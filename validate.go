package main

import (
	"encoding/json"
	"fmt"
	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
	"math"
	"strconv"
	"strings"
)

func invalidArg(msg string) error   { return sdk.Fail("OPERATION_ARGUMENT_INVALID", msg) }
func invalidReply(msg string) error { return sdk.Fail("JEV_RESPONSE_INVALID", msg) }

// argPath uses JSON Pointer escaping so names containing '/' or '~' stay
// unambiguous. Diagnostics quote paths to escape caller-supplied control chars.
func argPath(parent, key string) string {
	return parent + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}
func invalidArgAt(path, msg string) error {
	if path == "" {
		return invalidArg("arguments: " + msg)
	}
	return invalidArg(strconv.Quote(path) + ": " + msg)
}
func allowed(m map[string]json.RawMessage, keys []string, host bool, path string) error {
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
func validateQuery(raw []byte) (map[string]json.RawMessage, error) {
	if len(raw) > maxRequestBytes || sdk.ValidateStrict(raw) != nil {
		return nil, invalidArg("Arguments must be strict JSON within 256 KiB")
	}
	m := object(raw)
	if err := allowed(m, []string{"state", "questions"}, true, ""); err != nil {
		return nil, err
	}
	if !shape(m["state"], `"{[`) {
		return nil, invalidArgAt("/state", "expected a string, object, or array")
	}
	qs := object(m["questions"])
	if len(qs) == 0 {
		return nil, invalidArgAt("/questions", "expected a nonempty object of named questions")
	}
	for id, rawQ := range qs {
		path := argPath("/questions", id)
		if !nameOK(id) {
			return nil, invalidArgAt(path, "question names must be nonblank and at most 128 bytes")
		}
		q := object(rawQ)
		if err := allowed(q, []string{"type", "instructions", "criteria"}, false, path); err != nil {
			return nil, err
		}
		if !description(q["instructions"]) {
			return nil, invalidArgAt(path+"/instructions", "expected nonblank text, an object, or an array")
		}
		kind, _ := text(q["type"])
		c := q["criteria"]
		cp := path + "/criteria"
		switch kind {
		case "noul":
			if c == nil {
				continue
			}
			cm := object(c)
			if err := allowed(cm, []string{"true", "false"}, false, cp); err != nil {
				return nil, err
			}
			for k, v := range cm {
				if !description(v) {
					return nil, invalidArgAt(argPath(cp, k), "expected nonblank text, an object, or an array")
				}
			}
		case "choice":
			cm := object(c)
			if len(cm) < 1 || len(cm) > maxChoices {
				return nil, invalidArgAt(cp, "expected an object with 1 to 255 options")
			}
			for k, v := range cm {
				if !nameOK(k) {
					return nil, invalidArgAt(argPath(cp, k), "option names must be nonblank and at most 128 bytes")
				}
				if !(string(v) == "null" || description(v)) {
					return nil, invalidArgAt(argPath(cp, k), "expected null, nonblank text, an object, or an array")
				}
			}
		case "score":
			if !shape(c, "[") {
				return nil, invalidArgAt(cp, "expected an ordered array")
			}
			levels := parts(c)
			if len(levels) < 2 || len(levels) > maxLevels {
				return nil, invalidArgAt(cp, fmt.Sprintf("expected 2 to 10 descriptive levels; got %d", len(levels)))
			}
			for i, v := range levels {
				if !description(v) {
					return nil, invalidArgAt(argPath(cp, strconv.Itoa(i)), "expected nonblank text, an object, or an array")
				}
			}
		default:
			return nil, invalidArgAt(path+"/type", "expected noul, choice, or score")
		}
	}
	return qs, nil
}
func number(raw []byte, lo, hi float64) (float64, bool) {
	n, e := strconv.ParseFloat(string(raw), 64)
	return n, e == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= lo && n <= hi
}
func validateAnswers(raw []byte, questions map[string]json.RawMessage) error {
	if sdk.ValidateStrict(raw) != nil {
		return invalidReply("Reply is outside the strict JSON domain")
	}
	r := object(raw)
	model, ok := text(r["model"])
	if !ok || !modelName(model) {
		return invalidReply("Reply lacks a valid model identity")
	}
	usage := object(r["usage"])
	for _, k := range []string{"input_tokens", "output_tokens"} {
		n, ok := number(usage[k], 0, 9007199254740991)
		if !ok || math.Trunc(n) != n {
			return invalidReply("Reply lacks nonnegative integer token usage")
		}
	}
	answers := object(r["answers"])
	if len(answers) != len(questions) {
		return invalidReply("Answer IDs do not match the submitted questions")
	}
	for id, rawQ := range questions {
		a := object(answers[id])
		q := object(rawQ)
		kind, _ := text(q["type"])
		got, _ := text(a["type"])
		if got != kind {
			return invalidReply("Answer missing or wrong type for " + id)
		}
		if kind == "noul" {
			if _, ok := number(a["noul"], 0, 1); !ok {
				return invalidReply("Invalid noul probability for " + id)
			}
			continue
		}
		if _, ok := number(a["confidence"], 0, 1); !ok {
			return invalidReply("Invalid confidence for " + id)
		}
		options := object(q["criteria"])
		if kind == "score" {
			levels := parts(q["criteria"])
			options = map[string]json.RawMessage{}
			for i := range levels {
				options[strconv.Itoa(i)] = nil
			}
			legend := object(a["legend"])
			if len(legend) != len(levels) {
				return invalidReply("Incomplete score legend for " + id)
			}
			for k := range options {
				if _, ok := text(legend[k]); !ok {
					return invalidReply("Invalid score legend for " + id)
				}
			}
			if _, ok := number(a["score"], 0, float64(len(levels)-1)); !ok {
				return invalidReply("Score outside the submitted levels for " + id)
			}
		} else {
			choice, ok := text(a["choice"])
			if _, exists := options[choice]; !ok || !exists {
				return invalidReply("Choice outside the submitted options for " + id)
			}
		}
		probs := object(a["probabilities"])
		if len(probs) != len(options) {
			return invalidReply("Incomplete probability distribution for " + id)
		}
		sum := 0.0
		for k := range options {
			p, ok := number(probs[k], 0, 1)
			if !ok {
				return invalidReply("Invalid option probability for " + id)
			}
			sum += p
		}
		// Allow only rounding noise, never renormalize a service response.
		if math.Abs(sum-1) > 0.001 {
			return invalidReply(fmt.Sprintf("Probabilities for %s do not sum to one", id))
		}
	}
	return nil
}
