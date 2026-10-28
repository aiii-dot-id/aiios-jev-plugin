package main

import (
	"encoding/json"
	"errors"
	"fmt"
	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
	"net/url"
	"strings"
)

const (
	pluginID = "id.aiii.jev"

	jevHost      = "net.outbound:api.typesafe.ai:443"
	netLocal     = "net.local"
	jevBase      = "https://api.typesafe.ai"
	systemOne    = "/v1/systemone"
	modelsPath   = "/v1/models"
	defaultModel = "jev-latest"
	backendJev   = "jev"
	backendLocal = "local"

	maxRequestBytes = 256 << 10

	defaultReplyBytes = 16 << 10
	minReplyBytes     = 1 << 10
	maxReplyBytes     = 64 << 10

	defaultTimeoutMS = 10000
	minTimeoutMS     = 1000
	maxTimeoutMS     = 85000
	maxChoices       = 255
	maxLevels        = 10
	maxModels        = 256
)

func init() {
	p := sdk.New(pluginID)
	p.Describe("query", sdk.Descriptor{
		Summary: querySummary,
		Input:   "schemas/query_in.json", Output: "schemas/query_out.json", Effects: sdk.EffectsWriteExternal,
		Capabilities: []string{jevHost, netLocal}, MaxResultBytes: maxReplyBytes, Family: "jev",
		Keywords: []string{"jev", "judgment", "classify", "evaluate", "score", "decision"},
		Examples: []string{`{"state":{"evidence":"Unit tests passed; integration tests were not run."},"questions":{"covered":{"type":"noul","instructions":"Does evidence say integration tests were run?"}}}`},
	})
	p.Handle("query", query)
	p.Describe("models", sdk.Descriptor{Summary: modelsSummary, Input: "schemas/models_in.json", Output: "schemas/models_out.json", Effects: sdk.EffectsReadExternal, Capabilities: []string{jevHost, netLocal}, MaxResultBytes: maxReplyBytes, Family: "jev", Keywords: []string{"jev", "models"}})
	p.Handle("models", models)
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
	systemOne, models, auth string
	timeoutMS, replyBytes   int
}

func settings() (config, error) {
	v, err := sdk.Settings.Load()
	if err != nil {
		return config{}, hostError(err, sdk.HTTPResult{})
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
		base, why := localBase(raw)
		if why != "" {
			return c, sdk.Fail("JEV_CONFIGURATION_INVALID", fmt.Sprintf(configurationInvalid, "local_url", why))
		}
		c.systemOne, c.models = base+systemOne, base+modelsPath
		return c, nil
	}
	handle, _ := v.Handle("api_key")
	if handle == "" {
		return c, sdk.Fail("JEV_UNCONFIGURED", unconfiguredJev)
	}
	c.systemOne, c.models, c.auth = jevBase+systemOne, jevBase+modelsPath, handle
	return c, nil
}

func localBase(raw string) (base, why string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	switch {
	case err != nil:
		return "", "is not an address"
	case u.Scheme != "http" && u.Scheme != "https":
		return "", "must begin http:// or https://"
	case u.Host == "" || u.Hostname() == "":
		return "", "names no host"
	case u.User != nil:
		return "", "must not carry a user or password"
	case u.RawQuery != "" || u.Fragment != "" || u.ForceQuery:
		return "", "must not carry a query or fragment"
	}
	p := strings.TrimSuffix(strings.TrimRight(u.Path, "/"), systemOne)
	return u.Scheme + "://" + u.Host + p, ""
}

func modelName(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-:/", r)) {
			return false
		}
	}
	return true
}

func hostError(err error, res sdk.HTTPResult) error {
	if d, ok := sdk.AsDenied(err); ok {
		return sdk.Deny(d.ReasonCode, d.Message)
	}
	var op *sdk.OperationError
	if errors.As(err, &op) {
		return err
	}
	return transportFailure(res)
}

func transportFailure(res sdk.HTTPResult) error {
	if res.Effect == "not-performed" {
		return sdk.Fail("JEV_TRANSPORT_NOT_SENT", notSent)
	}
	return sdk.Fail("JEV_TRANSPORT_UNKNOWN", outcomeUnknown)
}

func query(c sdk.Call) (any, error) {
	q, err := validateQuery(c.Args())
	if err != nil {
		return nil, err
	}
	cfg, err := settings()
	if err != nil {
		return nil, err
	}
	if !modelName(cfg.model) {
		return nil, sdk.Fail("JEV_CONFIGURATION_INVALID", "model must be a nonblank ASCII model name, at most 128 bytes")
	}

	body := `{"model":"` + cfg.model + `","state":` + string(c.Args().Raw("state")) + `,"questions":` + string(c.Args().Raw("questions")) + `}`
	if len(body) > maxRequestBytes {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "Request exceeds 256 KiB; pass less context or fewer questions")
	}
	res, err := sdk.HTTP.Post(cfg.systemOne, &sdk.HTTPOptions{Body: body, ContentType: "application/json", AuthProfile: cfg.auth, TimeoutMS: cfg.timeoutMS})
	raw, err := response(res, err, cfg.replyBytes)
	if err != nil {
		return nil, err
	}
	if err = validateAnswers(raw, q); err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func response(res sdk.HTTPResult, err error, limit int) ([]byte, error) {
	var op *sdk.OperationError
	_, denied := sdk.AsDenied(err)
	if denied || errors.As(err, &op) && op.Status == "denied" {
		return nil, hostError(err, res)
	}
	if err != nil && (res.Status == 0 || res.Status == 200) {
		return nil, hostError(err, res)
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
			return nil, transportFailure(res)
		}
		msg := fmt.Sprintf(statusRefused, res.Status)
		switch {
		case res.RetryAfterS != nil:
			msg += fmt.Sprintf(retryAfter, *res.RetryAfterS)
		case res.Status == 429:
			msg += noRetryAfter
		}
		return nil, sdk.Fail(reason, msg)
	}
	raw := []byte(res.Body)

	if s, ok := text(raw); ok {
		raw = []byte(s)
	}
	if len(raw) > limit {
		return nil, sdk.Fail("JEV_RESULT_TOO_LARGE", fmt.Sprintf(replyTooLarge, limit))
	}
	if sdk.ValidateStrict(raw) != nil || object(raw) == nil {
		return nil, sdk.Fail("JEV_RESPONSE_INVALID", "Jev reply must be a strict JSON object")
	}
	return raw, nil
}
func models(c sdk.Call) (any, error) {
	if err := allowed(object(c.Args()), nil, true, ""); err != nil {
		return nil, err
	}
	cfg, err := settings()
	if err != nil {
		return nil, err
	}
	res, err := sdk.HTTP.Get(cfg.models, &sdk.HTTPOptions{AuthProfile: cfg.auth, TimeoutMS: cfg.timeoutMS})
	raw, err := response(res, err, cfg.replyBytes)
	if err != nil {
		return nil, err
	}
	return modelChoices(raw)
}
func modelChoices(raw []byte) (any, error) {
	if sdk.ValidateStrict(raw) != nil {
		return nil, invalidReply("invalid models JSON")
	}
	rows := object(raw)["models"]
	if !shape(rows, "[") {
		return nil, invalidReply("models must be an array")
	}
	choices := []any{}
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
		seen[name] = true
		choices = append(choices, map[string]any{"value": name})
	}
	if len(choices) == 0 || len(choices) > maxModels {
		return nil, invalidReply("expected 1 to 256 distinct models")
	}
	return map[string]any{"choices": choices}, nil
}
