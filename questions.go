package main

import (
	"fmt"
	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
	"math"
	"strconv"
	"strings"
)

func invalidArg(msg string) error   { return sdk.Fail("OPERATION_ARGUMENT_INVALID", msg) }
func invalidReply(msg string) error { return sdk.Fail("JEV_RESPONSE_INVALID", msg) }

func argPath(parent, key string) string {
	return parent + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}
func invalidArgAt(path, msg string) error {
	if path == "" {
		return invalidArg("arguments: " + msg)
	}
	return invalidArg(strconv.Quote(path) + ": " + msg)
}
func allowed(m map[string]rawJSON, keys []string, host bool, path string) error {
	if m == nil {
		return invalidArgAt(path, "expected a JSON object")
	}
	for k := range m {
		if host && strings.HasPrefix(k, "_host") {
			continue
		}
		found := false
		for _, want := range keys {
			if k == want {
				found = true
				break
			}
		}
		if !found {
			return invalidArgAt(argPath(path, k), "unknown field")
		}
	}
	return nil
}
func nameOK(s string) bool { return strings.TrimSpace(s) != "" && len(s) <= 128 }

func description(raw []byte) bool {
	if !shape(raw, `"{[`) {
		return false
	}
	if s, ok := text(raw); ok {
		return strings.TrimSpace(s) != ""
	}
	return true
}

type question struct {
	id       string
	kind     string
	instr    rawJSON
	criteria rawJSON
	options  []string
	levels   int
	legend   bool
}

func (q *question) wire(out []byte, item rawJSON) []byte {
	out = append(append(append(out, `{"type":"`...), q.kind...), `","instructions":`...)
	if item == nil {
		out = append(out, q.instr...)
	} else {
		out = append(append(append(append(append(out, `{"question":`...), q.instr...), `,"item":`...), item...), '}')
	}
	if q.criteria != nil {
		out = append(append(out, `,"criteria":`...), q.criteria...)
	}
	return append(out, '}')
}

func parseQuestions(raw []byte, path string) ([]*question, error) {
	if !shape(raw, "[") {
		return nil, invalidArgAt(path, `expected a list of questions, or {"$file": "<a JSON file of questions>"}`)
	}
	list := parts(raw)
	if len(list) == 0 {
		return nil, invalidArgAt(path, "expected at least one question")
	}
	qs := make([]*question, 0, len(list))
	seen := map[string]bool{}
	for i, v := range list {
		p := argPath(path, strconv.Itoa(i))
		m := object(v)
		if err := allowed(m, []string{"id", "type", "instructions", "criteria"}, false, p); err != nil {
			return nil, err
		}
		id, ok := text(m["id"])
		if !ok || !nameOK(id) {
			return nil, invalidArgAt(p+"/id", "question ids must be nonblank and at most 128 bytes")
		}
		if seen[id] {
			return nil, invalidArgAt(p+"/id", "this id names an earlier question too")
		}
		seen[id] = true
		q := &question{id: id}
		if err := q.parse(m, p); err != nil {
			return nil, err
		}
		qs = append(qs, q)
	}
	return qs, nil
}

func (q *question) parse(m map[string]rawJSON, path string) error {
	if !description(m["instructions"]) {
		return invalidArgAt(path+"/instructions", "expected nonblank text, an object, or an array")
	}
	q.kind = "noul"
	if t := m["type"]; t != nil {
		q.kind, _ = text(t)
	}
	q.instr = m["instructions"]
	c := m["criteria"]
	cp := path + "/criteria"
	if _, isRef, _ := reference(c); isRef {

		return invalidArgAt(cp, "criteria are written out; a file may stand for one description, not the whole")
	}
	switch q.kind {
	case "noul":
		if c == nil {
			return nil
		}
		cm := object(c)
		if err := allowed(cm, []string{"true", "false"}, false, cp); err != nil {
			return err
		}
		for k, v := range cm {
			if !description(v) {
				return invalidArgAt(argPath(cp, k), "expected nonblank text, an object, or an array")
			}
		}
	case "choice":
		cm := members(c)
		if len(cm) < 1 || len(cm) > maxChoices {
			return invalidArgAt(cp, "expected an object with 1 to 255 options")
		}
		for _, o := range cm {
			if !nameOK(o.name) {
				return invalidArgAt(argPath(cp, o.name), "option names must be nonblank and at most 128 bytes")
			}
			if !(string(o.val) == "null" || description(o.val)) {
				return invalidArgAt(argPath(cp, o.name), "expected null, nonblank text, an object, or an array")
			}
			q.options = append(q.options, o.name)
		}
	case "score":
		if !shape(c, "[") {
			return invalidArgAt(cp, "expected an ordered array")
		}
		levels := parts(c)
		if len(levels) < 2 || len(levels) > maxLevels {
			return invalidArgAt(cp, fmt.Sprintf("expected 2 to 10 descriptive levels; got %d", len(levels)))
		}
		for i, v := range levels {
			if !description(v) {
				return invalidArgAt(argPath(cp, strconv.Itoa(i)), "expected nonblank text, an object, or an array")
			}
		}
		q.levels = len(levels)
	case "box":
		if c != nil {
			return invalidArgAt(cp, "a box takes no criteria")
		}
	default:
		return invalidArgAt(path+"/type", "expected noul, choice, score, or box")
	}
	q.criteria = c
	return nil
}

const (
	roundingSlack = 0.005
	floatSlack    = 1e-9
)

func number(raw []byte, lo, hi float64) (float64, bool) {
	n, e := strconv.ParseFloat(string(raw), 64)
	return n, e == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= lo && n <= hi
}

func usage(raw []byte) bool {
	u := object(raw)
	for _, k := range []string{"input_tokens", "output_tokens"} {
		n, ok := number(u[k], 0, 9007199254740991)
		if !ok || math.Trunc(n) != n {
			return false
		}
	}
	return true
}

func (q *question) check(raw rawJSON, id string) error {
	a := object(raw)
	if got, _ := text(a["type"]); got != q.kind {
		return invalidReply("Answer missing or wrong type for " + id)
	}
	switch q.kind {
	case "noul":
		if _, ok := number(a["noul"], 0, 1); !ok {
			return invalidReply("Invalid noul probability for " + id)
		}
		return nil
	case "box":
		b := object(a["box"])
		top, ok1 := number(b["top"], 0, 1)
		bottom, ok2 := number(b["bottom"], 0, 1)
		left, ok3 := number(b["left"], 0, 1)
		right, ok4 := number(b["right"], 0, 1)
		if !ok1 || !ok2 || !ok3 || !ok4 || len(b) != 4 || top >= bottom || left >= right {
			return invalidReply("Invalid box for " + id)
		}
	}
	if _, ok := number(a["confidence"], 0, 1); !ok {
		return invalidReply("Invalid confidence for " + id)
	}
	options := q.options
	switch q.kind {
	case "box":
		return nil
	case "score":
		options = make([]string, q.levels)
		for i := range options {
			options[i] = strconv.Itoa(i)
		}
		legend := object(a["legend"])
		if len(legend) != q.levels {
			return invalidReply("Incomplete score legend for " + id)
		}
		for _, k := range options {
			if _, ok := text(legend[k]); !ok {
				return invalidReply("Invalid score legend for " + id)
			}
		}
		if _, ok := number(a["score"], 0, float64(q.levels-1)); !ok {
			return invalidReply("Score outside the submitted levels for " + id)
		}
	case "choice":
		choice, ok := text(a["choice"])
		if !ok || !q.hasOption(choice) {
			return invalidReply("Choice outside the submitted options for " + id)
		}
	}
	probs := object(a["probabilities"])
	if len(probs) != len(options) {
		return invalidReply("Incomplete probability distribution for " + id)
	}
	sum := 0.0
	for _, k := range options {
		p, ok := number(probs[k], 0, 1)
		if !ok {
			return invalidReply("Invalid option probability for " + id)
		}
		sum += p
	}

	if math.Abs(sum-1) > roundingSlack*float64(len(options))+floatSlack {
		return invalidReply(fmt.Sprintf("Probabilities for %s do not sum to one", id))
	}
	return nil
}

func (q *question) hasOption(name string) bool {
	for _, o := range q.options {
		if o == name {
			return true
		}
	}
	return false
}
