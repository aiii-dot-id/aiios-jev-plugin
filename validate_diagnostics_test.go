package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

func TestQueryDiagnosticPaths(t *testing.T) {
	for _, tc := range []struct{ name, question, path, message string }{
		{"question_object", `null`, "/questions/1", "expected a JSON object"},
		{"unknown_field", `{"id":"check","type":"noul","instructions":"x","extra":true}`, "/questions/1/extra", "unknown field"},
		{"instructions", `{"id":"check","type":"noul","instructions":false}`, "/questions/1/instructions", "expected nonblank text, an object, or an array"},
		{"type", `{"id":"check","type":"chat","instructions":"x"}`, "/questions/1/type", "expected noul, choice, score, or box"},
		{"noul_object", `{"id":"check","type":"noul","instructions":"x","criteria":[]}`, "/questions/1/criteria", "expected a JSON object"},
		{"noul_key", `{"id":"check","type":"noul","instructions":"x","criteria":{"maybe":"x"}}`, "/questions/1/criteria/maybe", "unknown field"},
		{"noul_value", `{"id":"check","type":"noul","instructions":"x","criteria":{"true":false}}`, "/questions/1/criteria/true", "expected nonblank text, an object, or an array"},
		{"choice_size", `{"id":"check","type":"choice","instructions":"x","criteria":{}}`, "/questions/1/criteria", "expected an object with 1 to 255 options"},
		{"choice_name", `{"id":"check","type":"choice","instructions":"x","criteria":{" ":null}}`, "/questions/1/criteria/ ", "option names must be nonblank and at most 128 bytes"},
		{"choice_value", `{"id":"check","type":"choice","instructions":"x","criteria":{"retry":42}}`, "/questions/1/criteria/retry", "expected null, nonblank text, an object, or an array"},
		{"score_shape", `{"id":"check","type":"score","instructions":"x","criteria":{}}`, "/questions/1/criteria", "expected an ordered array"},
		{"score_size", `{"id":"check","type":"score","instructions":"x","criteria":["alone"]}`, "/questions/1/criteria", "expected 2 to 10 descriptive levels; got 1"},
		{"score_level", `{"id":"check","type":"score","instructions":"x","criteria":["none",false]}`, "/questions/1/criteria/1", "expected nonblank text, an object, or an array"},
		{"box_criteria", `{"id":"check","type":"box","instructions":"x","criteria":{}}`, "/questions/1/criteria", "a box takes no criteria"},
		{"repeated_id", `{"id":"other","instructions":"y"}`, "/questions/1/id", "this id names an earlier question too"},
		{"no_id", `{"instructions":"y"}`, "/questions/1/id", "question ids must be nonblank and at most 128 bytes"},
		{"whole_criteria_file", `{"id":"check","type":"choice","instructions":"x","criteria":{"$file":"c.json"}}`, "/questions/1/criteria", "criteria are written out; a file may stand for one description, not the whole"},
	} {
		t.Run(tc.name, func(t *testing.T) {

			raw := `{"state":"x","questions":[{"id":"other","type":"noul","instructions":"x"},` + tc.question + `]}`
			_, err := parseAsk([]byte(raw))
			reason(t, err, "OPERATION_ARGUMENT_INVALID")
			var op *sdk.OperationError
			if !errors.As(err, &op) || op.Reason != strconv.Quote(tc.path)+": "+tc.message {
				t.Fatalf("diagnostic: %v", err)
			}
		})
	}
}

func TestAQuestionWithoutATypeIsANoul(t *testing.T) {
	a, err := parseAsk([]byte(`{"state":"x","questions":[{"id":"z","instructions":"Is it?"},{"id":"a","type":"choice","instructions":"Which?","criteria":{"b":null,"a":null}}]}`))
	if err != nil || a.qs[0].id != "z" || a.qs[0].kind != "noul" || a.qs[1].id != "a" {
		t.Fatalf("%v %v", a, err)
	}
	if got := string(wireQuestions(a.qs)); got != `{"z":{"type":"noul","instructions":"Is it?"},"a":{"type":"choice","instructions":"Which?","criteria":{"b":null,"a":null}}}` {
		t.Fatal(got)
	}
}

func TestDiagnosticEscapesCallerNames(t *testing.T) {
	raw := `{"state":"x","questions":[{"id":"a/b~c\n","type":"choice","instructions":"x","criteria":{"retry/now~":42}}]}`
	_, err := parseAsk([]byte(raw))
	var op *sdk.OperationError
	if !errors.As(err, &op) || !strings.HasPrefix(op.Reason, `"/questions/0/criteria/retry~1now~0": `) || strings.Contains(op.Reason, "\n") {
		t.Fatalf("ambiguous or multiline path: %v", err)
	}
}

func TestQueryValidationBoundariesUnchanged(t *testing.T) {
	for _, n := range []int{0, 1, 255, 256} {
		options := make([]string, n)
		for i := range options {
			options[i] = fmt.Sprintf(`"k%d":null`, i)
		}
		raw := `{"state":"x","questions":[{"id":"q","type":"choice","instructions":"x","criteria":{` + strings.Join(options, ",") + `}}]}`
		_, err := parseAsk([]byte(raw))
		if (err == nil) != (n >= 1 && n <= 255) {
			t.Fatalf("choice count %d: %v", n, err)
		}
	}
	for _, n := range []int{1, 2, 10, 11} {
		levels := make([]string, n)
		for i := range levels {
			levels[i] = "level"
		}
		c, _ := json.Marshal(levels)
		raw := `{"state":"x","questions":[{"id":"q","type":"score","instructions":"x","criteria":` + string(c) + `}]}`
		_, err := parseAsk([]byte(raw))
		if (err == nil) != (n >= 2 && n <= 10) {
			t.Fatalf("score count %d: %v", n, err)
		}
	}
	for _, n := range []int{128, 129} {
		raw := `{"state":"x","questions":[{"id":"` + strings.Repeat("q", n) + `","instructions":"x"}]}`
		_, err := parseAsk([]byte(raw))
		if (err == nil) != (n == 128) {
			t.Fatalf("name bytes %d: %v", n, err)
		}
	}
}

func TestQueryRootAndNameDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"root", `[]`, "arguments: expected a JSON object"},
		{"state", `{"state":false,"questions":[]}`, `"/state": expected a string, object, or array`},
		{"questions", `{"state":"x"}`, `"/questions": expected a list of questions, or {"$file": "<a JSON file of questions>"}`},
		{"a_map", `{"state":"x","questions":{"q":"Is it?"}}`, `"/questions": expected a list of questions, or {"$file": "<a JSON file of questions>"}`},
		{"empty", `{"state":"x","questions":[]}`, `"/questions": expected at least one question`},
		{"blank_id", `{"state":"x","questions":[{"id":" \t","instructions":"x"}]}`, `"/questions/0/id": question ids must be nonblank and at most 128 bytes`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseAsk([]byte(tc.raw))
			reason(t, err, "OPERATION_ARGUMENT_INVALID")
			var op *sdk.OperationError
			if !errors.As(err, &op) || op.Reason != tc.want {
				t.Fatalf("diagnostic: %v; want %q", err, tc.want)
			}
		})
	}
}

func TestQuestionAndOptionNameByteBoundaries(t *testing.T) {
	for _, name := range []string{
		strings.Repeat("n", 128), strings.Repeat("n", 129),
		strings.Repeat("é", 64), strings.Repeat("é", 64) + "x",
		"/~" + strings.Repeat("n", 127),
	} {
		for _, option := range []bool{false, true} {
			key, _ := json.Marshal(name)
			raw := `{"state":"x","questions":[{"id":` + string(key) + `,"instructions":"x"}]}`
			path, message := "/questions/0/id", "question ids must be nonblank and at most 128 bytes"
			if option {
				raw = `{"state":"x","questions":[{"id":"q","type":"choice","instructions":"x","criteria":{` + string(key) + `:null}}]}`
				path, message = argPath("/questions/0/criteria", name), "option names must be nonblank and at most 128 bytes"
			}
			_, err := parseAsk([]byte(raw))
			if len(name) <= 128 {
				if err != nil {
					t.Fatalf("valid name, bytes=%d option=%v: %v", len(name), option, err)
				}
				continue
			}
			reason(t, err, "OPERATION_ARGUMENT_INVALID")
			var op *sdk.OperationError
			if !errors.As(err, &op) || op.Reason != strconv.Quote(path)+": "+message {
				t.Fatalf("invalid name, bytes=%d option=%v: %v", len(name), option, err)
			}
		}
	}
}
