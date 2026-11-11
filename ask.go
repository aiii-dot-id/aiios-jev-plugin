package main

import (
	"bytes"
	"fmt"
	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
	"strings"
)

const (
	invokeBudgetMS = 90_000
	replyReserveMS = 5_000
)

type askArgs struct {
	state     rawJSON
	questions rawJSON
	qs        []*question
	qfile     string
	items     rawJSON
	files     string
	rank      string
	long      bool
	from      *cursor
}

func parseAsk(raw []byte) (*askArgs, error) {
	if len(raw) > maxRequestBytes || sdk.ValidateStrict(raw) != nil {
		return nil, invalidArg("Arguments must be strict JSON within 256 KiB")
	}
	m := object(raw)
	if err := allowed(m, []string{"state", "questions", "items", "rank", "long_context", "from"}, true, ""); err != nil {
		return nil, err
	}
	a := &askArgs{state: m["state"], questions: m["questions"]}
	if !shape(a.state, `"{[`) {
		return nil, invalidArgAt("/state", "expected a string, object, or array")
	}
	if p, is, err := reference(a.questions); err != nil {
		return nil, err
	} else if is {
		a.qfile = p
	} else if a.qs, err = parseQuestions(a.questions, "/questions"); err != nil {
		return nil, err
	}
	if v := m["items"]; v != nil {
		a.items = v
		switch {
		case shape(v, "["):
			if len(parts(v)) == 0 {
				return nil, invalidArgAt("/items", "expected at least one item")
			}
		case shape(v, "{"):
			o := object(v)
			p, ok := text(o["$files"])
			if len(o) != 1 || !ok || strings.TrimSpace(p) == "" {
				return nil, invalidArgAt("/items", `expected an array of items, or {"$files": "<folder>/<pattern>"}`)
			}
			if err := filesPattern(p); err != nil {
				return nil, err
			}
			a.files = p
		default:
			return nil, invalidArgAt("/items", `expected an array of items, or {"$files": "<folder>/<pattern>"}`)
		}
	}
	if v := m["rank"]; v != nil {
		s, ok := text(v)
		if !ok || s == "" {
			return nil, invalidArgAt("/rank", "expected a question id, or id.option")
		}
		a.rank = s
	}
	if v := m["long_context"]; v != nil {
		if string(v) != "true" && string(v) != "false" {
			return nil, invalidArgAt("/long_context", "expected true or false")
		}
		a.long = string(v) == "true"
	}
	if v := m["from"]; v != nil {
		c, err := parseCursor(v)
		if err != nil {
			return nil, err
		}
		a.from = c
	}
	if a.items == nil && (a.rank != "" || a.from != nil) {
		return nil, invalidArg("rank and from apply only with items")
	}
	return a, nil
}

func ask(c sdk.Call, h host) (any, error) {
	a, err := parseAsk(c.Args())
	if err != nil {
		return nil, err
	}
	cfg, err := settings(h)
	if err != nil {
		return nil, err
	}
	model := cfg.model
	if a.from != nil && a.from.model != "" {
		model = a.from.model
	}
	if !modelName(model) {
		return nil, sdk.Fail("JEV_CONFIGURATION_INVALID", "model must be a nonblank ASCII model name, at most 128 bytes")
	}

	r := newResolver(h, maxRequestBytes-len(request(model, a.state, a.questions, nil)))
	if a.qfile != "" {
		if a.qs, err = r.questionsFile(a.qfile); err != nil {
			return nil, err
		}
	}
	for _, q := range a.qs {
		base := argPath("/questions", q.id)
		if q.instr, err = r.resolve(q.instr, base+"/instructions"); err != nil {
			return nil, err
		}
		if q.criteria != nil {
			q.legend = q.legend || refCount(q.criteria) > 0
			if q.criteria, err = r.resolve(q.criteria, base+"/criteria"); err != nil {
				return nil, err
			}
		}
	}
	state, err := r.resolve(a.state, "/state")
	if err != nil {
		return nil, err
	}
	if a.items != nil {
		return askItems(c, h, cfg, model, a, state, r)
	}
	return askOne(c, h, cfg, model, a, state, r)
}

func (r *resolver) questionsFile(path string) ([]*question, error) {
	if r.refs >= maxRefs {
		return nil, sdk.Fail("JEV_REFERENCES_TOO_MANY", fmt.Sprintf(referencesTooMany, path, maxRefs))
	}
	r.refs++
	f, err := r.load(path)
	if err != nil {
		return nil, err
	}
	if f.format != "" || sdk.ValidateStrict(f.data) != nil || !shape(f.data, "[") {
		return nil, invalidArg(fmt.Sprintf(questionsFileBad, path))
	}
	qs, err := parseQuestions(f.data, path)
	if err != nil {
		return nil, err
	}
	for _, q := range qs {
		q.legend = true
	}
	r.room -= len(f.data)
	r.note(path, f)
	return qs, nil
}

func wireQuestions(qs []*question) []byte {
	out := []byte{'{'}
	for i, q := range qs {
		if i > 0 {
			out = append(out, ',')
		}
		out = q.wire(append(quote(out, q.id), ':'), nil)
	}
	return append(out, '}')
}

func request(model string, state, questions, assets []byte) []byte {
	out := quote([]byte(`{"model":`), model)
	out = append(append(append(append(out, `,"state":`...), state...), `,"questions":`...), questions...)
	if len(assets) > 0 {
		out = append(append(append(out, `,"assets":{`...), assets[1:]...), '}')
	}
	return append(out, '}')
}

func headers(images bool, qs []*question, long bool) map[string]string {
	hs := map[string]string{}
	for _, q := range qs {
		images = images || q.kind == "box"
	}
	if images {
		hs["TypeSafe-Danger-Zone-Images"] = "true"
	}
	if long {
		hs["TypeSafe-Danger-Zone-Long-Context"] = "true"
	}
	return hs
}

func attempt(c sdk.Call, h host, setting int, judge string) (int, error) {
	start, ok := c.HostNowMillis()
	if !ok {
		return 0, sdk.Fail("JEV_NOT_SENT_OUT_OF_TIME", fmt.Sprintf(noHostTime, judge))
	}
	now := start
	if t, ok := h.last(); ok && t.UnixMilli() > now {
		now = t.UnixMilli()
	}
	left := invokeBudgetMS - (now - start) - replyReserveMS
	if left < minTimeoutMS {
		return 0, sdk.Fail("JEV_NOT_SENT_OUT_OF_TIME", fmt.Sprintf(outOfTime, max(left, 0), invokeBudgetMS/1000, minTimeoutMS, judge))
	}
	return int(min(int64(setting), left)), nil
}

func post(c sdk.Call, h host, cfg config, body []byte, hs map[string]string, tooLong, again string) (rawJSON, error) {
	timeout, err := attempt(c, h, cfg.timeoutMS, cfg.judge)
	if err != nil {
		return nil, err
	}
	res, err := h.post(cfg.systemOne, &sdk.HTTPOptions{Body: string(body), ContentType: "application/json", AuthProfile: cfg.auth, TimeoutMS: timeout, Headers: hs})
	return response(res, err, cfg.judge, cfg.replyBytes, tooLong, again)
}

func replyModel(raw rawJSON) (model string, use tokens, extra []member, collided []string, err error) {
	r := object(raw)
	model, ok := text(r["model"])
	if !ok || !modelName(model) {
		return "", tokens{}, nil, nil, invalidReply("Reply lacks a valid model identity")
	}
	if !usage(r["usage"]) {
		return "", tokens{}, nil, nil, invalidReply("Reply lacks nonnegative integer token usage")
	}
	for _, x := range members(raw) {
		switch {
		case x.name == "model" || x.name == "answers" || x.name == "usage":
		case envelope[x.name]:
			collided = append(collided, x.name+leftOwn)
		default:
			extra = append(extra, x)
		}
	}
	return model, readTokens(r["usage"]), extra, collided, nil
}

func askOne(c sdk.Call, h host, cfg config, model string, a *askArgs, state rawJSON, r *resolver) (any, error) {
	o := &oneReply{judge: cfg.judge, qs: a.qs, sent: r.sent, turned: r.turned, long: a.long, images: r.images > 0}
	if b := o.bound(); b > cfg.replyBytes {
		fit := len(a.qs)
		for ; fit > 0 && b > cfg.replyBytes; fit-- {
			w := *o
			w.qs = a.qs[:fit-1]
			b = w.bound()
		}
		return nil, sdk.Fail("JEV_REPLY_WOULD_NOT_FIT", fmt.Sprintf(replyWouldNotFit, o.bound(), cfg.replyBytes, fit, len(a.qs)))
	}
	body := request(model, state, wireQuestions(a.qs), r.assets)
	if len(body) > maxRequestBytes {
		return nil, invalidArg(fmt.Sprintf(requestTooLarge, len(body), maxRequestBytes))
	}
	tooLong := ""
	if a.long {
		tooLong = tooLongAlready
	}
	raw, err := post(c, h, cfg, body, headers(r.images > 0, a.qs, a.long), tooLong, "")
	if err != nil {
		return nil, err
	}
	if o.model, o.usage, o.extra, o.collided, err = replyModel(raw); err != nil {
		return nil, err
	}
	answers := object(object(raw)["answers"])
	if len(answers) != len(a.qs) {
		return nil, invalidReply("Answer IDs do not match the submitted questions")
	}
	for _, q := range a.qs {
		if err := q.check(answers[q.id], q.id); err != nil {
			return nil, err
		}
		o.answers = append(o.answers, readAnswer(q, answers[q.id]))
	}
	return rawJSON(render(cfg.replyBytes, o.write)), nil
}

func trimmed(raw []byte) []byte { return bytes.TrimSpace(raw) }
