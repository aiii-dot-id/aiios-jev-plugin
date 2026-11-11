package main

import (
	"bytes"
	"fmt"
	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	pluginID = "id.aiii.jev"

	jevHost      = "net.outbound:api.typesafe.ai:443"
	jevName      = "api.typesafe.ai"
	netLocal     = "net.local"
	fsSandbox    = "fs.sandbox"
	jevBase      = "https://api.typesafe.ai"
	systemOne    = "/v1/systemone"
	modelsPath   = "/v1/models"
	defaultModel = "jev-latest"
	backendJev   = "jev"
	backendLocal = "local"

	maxRequestBytes = 256 << 10

	maxJudgeReplyBytes = 768 << 10

	defaultReplyBytes = 16 << 10
	minReplyBytes     = 1 << 10
	replyRoom         = 3616
	maxReplyBytes     = 32000 - replyRoom

	defaultTimeoutMS = 10000
	minTimeoutMS     = 1000
	maxTimeoutMS     = 60000
	maxChoices       = 255
	maxLevels        = 10
	maxModels        = 256
	maxModelName     = 128
	maxLabel         = 64

	modelsResultBytes = len(`{"judge":"","choices":[]}`) + 6*255 + maxModels*(len(`{"value":"","label":""},`)+maxModelName+6*maxLabel)
)

func init() {
	p := sdk.New(pluginID)
	p.Describe("ask", sdk.Descriptor{
		Summary: askSummary,
		Input:   "schemas/ask_in.json", Output: "schemas/ask_out.json", Effects: sdk.EffectsWriteExternal,
		Capabilities: []string{jevHost, netLocal, fsSandbox}, MaxResultBytes: maxReplyBytes, Family: "jev",
		Keywords: []string{"jev", "judgment", "judge", "classify", "screen", "evaluate", "score", "locate", "files"},
	})
	p.Handle("ask", func(c sdk.Call) (any, error) { return ask(c, sdkHost{}) })
	p.Describe("models", sdk.Descriptor{Summary: modelsSummary, Input: "schemas/models_in.json", Output: "schemas/models_out.json", Effects: sdk.EffectsReadExternal, Capabilities: []string{jevHost, netLocal}, MaxResultBytes: modelsResultBytes, Family: "jev", Keywords: []string{"jev", "models", "judge"}})
	p.Handle("models", func(c sdk.Call) (any, error) { return models(c, sdkHost{}) })
	p.Run()
}
func main() { sdk.MainDescribe() }

type settingsReader interface {
	String(key string) (string, bool)
	Handle(key string) (string, bool)
	Int(key string) (int64, bool)
}

type config struct {
	backend, model          string
	judge                   string
	systemOne, models, auth string
	timeoutMS, replyBytes   int
}

func settings(h host) (config, error) {
	v, err := h.settings()
	if err != nil {
		return config{}, hostError(err)
	}
	return configFrom(v)
}

func configFrom(v settingsReader) (config, error) {
	c := config{backend: backendJev, model: defaultModel, timeoutMS: defaultTimeoutMS, replyBytes: defaultReplyBytes}
	if b, ok := v.String("backend"); ok && b != "" {
		if b != backendJev && b != backendLocal {
			return c, sdk.Fail("JEV_CONFIGURATION_INVALID", fmt.Sprintf(configurationInvalid, "backend", "is neither jev nor local"))
		}
		c.backend = b
	}
	if m, ok := v.String("model"); ok && m != "" {
		c.model = m
	}
	for _, b := range []struct {
		key      string
		dst      *int
		min, max int64
	}{{"timeout_ms", &c.timeoutMS, minTimeoutMS, maxTimeoutMS}, {"max_reply_bytes", &c.replyBytes, minReplyBytes, maxReplyBytes}} {
		if n, ok := v.Int(b.key); ok {
			if n < b.min || n > b.max {
				return c, sdk.Fail("JEV_CONFIGURATION_INVALID", fmt.Sprintf(configurationInvalid, b.key, fmt.Sprintf("is %d, outside %d to %d", n, b.min, b.max)))
			}
			*b.dst = int(n)
		}
	}
	if c.backend == backendLocal {
		raw, _ := v.String("local_url")
		if strings.TrimSpace(raw) == "" {
			return c, sdk.Fail("JEV_UNCONFIGURED", unconfiguredLocal)
		}
		base, host, why := localBase(raw)
		if why != "" {
			return c, sdk.Fail("JEV_CONFIGURATION_INVALID", fmt.Sprintf(configurationInvalid, "local_url", why))
		}
		c.systemOne, c.models, c.judge = base+systemOne, base+modelsPath, host
		return c, nil
	}
	handle, _ := v.Handle("api_key")
	if handle == "" {
		return c, sdk.Fail("JEV_UNCONFIGURED", unconfiguredJev)
	}
	c.systemOne, c.models, c.auth, c.judge = jevBase+systemOne, jevBase+modelsPath, handle, jevName
	return c, nil
}

func localBase(raw string) (base, host, why string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	switch {
	case err != nil:
		return "", "", "is not an address"
	case u.Scheme != "http" && u.Scheme != "https":
		return "", "", "must begin http:// or https://"
	case u.Host == "" || u.Hostname() == "":
		return "", "", "names no host"
	case u.User != nil:
		return "", "", "must not carry a user or password"
	case u.RawQuery != "" || u.Fragment != "" || u.ForceQuery:
		return "", "", "must not carry a query or fragment"
	}
	p := strings.TrimSuffix(strings.TrimRight(u.Path, "/"), systemOne)
	return u.Scheme + "://" + u.Host + p, u.Host, ""
}

func modelName(s string) bool {
	if len(s) == 0 || len(s) > maxModelName {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-:/", r)) {
			return false
		}
	}
	return true
}

func hostError(err error) error {
	if d, ok := sdk.AsDenied(err); ok {
		return sdk.Deny(d.ReasonCode, d.Message)
	}
	if op, ok := sdk.AsOperationError(err); ok {
		return op
	}
	return sdk.Fail("JEV_TRANSPORT_UNKNOWN", err.Error())
}

func transportFailure(res sdk.HTTPResult, err error, judge, again string) error {
	host := ""
	if op, ok := sdk.AsOperationError(err); ok {
		host = " (the host: " + op.ReasonCode
		if op.Reason != "" && op.Reason != op.ReasonCode {
			host += ", " + op.Reason
		}
		host += ")"
	}
	if res.Effect == "not-performed" {
		if again == "" {
			again = againOne
		}
		return sdk.Fail("JEV_TRANSPORT_NOT_SENT", fmt.Sprintf(notSent, judge)+host+again)
	}
	if again == "" {
		again = againOneMaybe
	}
	return sdk.Fail("JEV_TRANSPORT_UNKNOWN", fmt.Sprintf(outcomeUnknown, judge)+host+again)
}

func response(res sdk.HTTPResult, err error, judge string, words int, tooLong, again string) ([]byte, error) {
	if _, denied := sdk.AsDenied(err); denied {
		return nil, hostError(err)
	}
	if op, ok := sdk.AsOperationError(err); ok && op.Status == "denied" {
		return nil, op
	}
	if err != nil && (res.Status == 0 || res.Status == 200) {
		if res.Effect == "" {
			if _, ok := sdk.AsOperationError(err); ok {
				return nil, hostError(err)
			}
		}
		return nil, transportFailure(res, err, judge, again)
	}
	if res.Status != 200 {
		reason := "JEV_REQUEST_REFUSED"
		switch {
		case res.Status == 401 || res.Status == 403:
			reason = "JEV_ACCESS_REFUSED"
		case res.Status == 429:
			reason = "JEV_RATE_LIMITED"
		case res.Status >= 500:
			reason = "JEV_UNAVAILABLE"
		case res.Status == 0:
			return nil, transportFailure(res, err, judge, again)
		}
		msg := fmt.Sprintf(statusRefused, judge, res.Status)
		switch {
		case res.RetryAfterS != nil:
			msg += fmt.Sprintf(retryAfter, *res.RetryAfterS)
		case res.Status == 429:
			msg += noRetryAfter
		}
		said, detail := judgeSaid(res.Body, words)
		if said != "" {
			msg += fmt.Sprintf(judgeReason, said)
		}

		if res.Status == 400 && strings.Contains(detail, "max_tokens_exceeded") {
			if tooLong == "" {
				tooLong = tooLongOne
			}
			msg += tooLong
		}
		return nil, sdk.Fail(reason, msg)
	}
	raw := []byte(res.Body)

	if s, ok := text(raw); ok {
		raw = []byte(s)
	}
	if len(raw) > maxJudgeReplyBytes {
		return nil, sdk.Fail("JEV_RESULT_TOO_LARGE", fmt.Sprintf(replyTooLarge, maxJudgeReplyBytes))
	}
	if sdk.ValidateStrict(raw) != nil || object(raw) == nil {
		return nil, sdk.Fail("JEV_RESPONSE_INVALID", "Jev reply must be a strict JSON object")
	}
	return raw, nil
}

func judgeSaid(body []byte, limit int) (said, detail string) {
	raw := bytes.TrimSpace(body)
	s := strconv.Quote(string(raw))
	if sdk.ValidateStrict(raw) == nil {
		if d := object(raw)["detail"]; d != nil {
			raw = d
		}
		s = string(raw)
		if t, ok := text(raw); ok {
			s = strconv.Quote(t)
		}
	}
	detail = string(raw)
	if t, ok := text(raw); ok {
		detail = t
	}
	if len(raw) == 0 || s == "null" || s == `""` {
		return "", detail
	}
	if len(s) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + fmt.Sprintf(judgeReasonCut, limit)
	}
	return s, detail
}

func models(c sdk.Call, h host) (any, error) {
	if err := allowed(object(c.Args()), nil, true, ""); err != nil {
		return nil, err
	}
	cfg, err := settings(h)
	if err != nil {
		return nil, err
	}
	res, err := h.get(cfg.models, &sdk.HTTPOptions{AuthProfile: cfg.auth, TimeoutMS: cfg.timeoutMS})
	raw, err := response(res, err, cfg.judge, cfg.replyBytes, "", "")
	if err != nil {
		return nil, err
	}
	return modelChoices(raw, cfg.judge)
}

func modelChoices(raw []byte, judge string) (rawJSON, error) {
	if sdk.ValidateStrict(raw) != nil {
		return nil, invalidReply("invalid models JSON")
	}
	rows := object(raw)["models"]
	if !shape(rows, "[") {
		return nil, invalidReply("models must be an array")
	}
	out := quote([]byte(`{"judge":`), judge)
	out = append(out, `,"choices":[`...)
	seen := map[string]bool{}
	for _, row := range parts(rows) {
		m := object(row)
		name, ok := text(m["name"])
		if !ok || !modelName(name) {
			return nil, invalidReply("model name is missing or invalid")
		}
		if seen[name] {
			continue
		}
		if len(seen) > 0 {
			out = append(out, ',')
		}
		seen[name] = true
		out = quote(append(out, `{"value":`...), name)
		if label := modelLabel(m); label != "" {
			out = quote(append(out, `,"label":`...), label)
		}
		out = append(out, '}')
	}
	if len(seen) == 0 || len(seen) > maxModels {
		return nil, invalidReply("expected 1 to 256 distinct models")
	}
	return append(out, "]}"...), nil
}

func modelLabel(m map[string]rawJSON) string {
	date, _ := text(m["release_date"])
	desc, _ := text(m["description"])
	label := strings.Join(strings.Fields(date+" "+desc), " ")
	if len(label) <= maxLabel {
		return label
	}
	cut := maxLabel - len("…")
	for cut > 0 && !utf8.RuneStart(label[cut]) {
		cut--
	}
	return label[:cut] + "…"
}
