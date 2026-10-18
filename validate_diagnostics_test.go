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
		{"question_object", `null`, "/questions/check", "expected a JSON object"},
		{"unknown_field", `{"type":"noul","instructions":"x","extra":true}`, "/questions/check/extra", "unknown field"},
		{"instructions", `{"type":"noul","instructions":false}`, "/questions/check/instructions", "expected nonblank text, an object, or an array"},
		{"type", `{"type":"chat","instructions":"x"}`, "/questions/check/type", "expected noul, choice, or score"},
		{"noul_object", `{"type":"noul","instructions":"x","criteria":[]}`, "/questions/check/criteria", "expected a JSON object"},
		{"noul_key", `{"type":"noul","instructions":"x","criteria":{"maybe":"x"}}`, "/questions/check/criteria/maybe", "unknown field"},
		{"noul_value", `{"type":"noul","instructions":"x","criteria":{"true":false}}`, "/questions/check/criteria/true", "expected nonblank text, an object, or an array"},
		{"choice_size", `{"type":"choice","instructions":"x","criteria":{}}`, "/questions/check/criteria", "expected an object with 1 to 255 options"},
		{"choice_name", `{"type":"choice","instructions":"x","criteria":{" ":null}}`, "/questions/check/criteria/ ", "option names must be nonblank and at most 128 bytes"},
		{"choice_value", `{"type":"choice","instructions":"x","criteria":{"retry":42}}`, "/questions/check/criteria/retry", "expected null, nonblank text, an object, or an array"},
		{"score_shape", `{"type":"score","instructions":"x","criteria":{}}`, "/questions/check/criteria", "expected an ordered array"},
		{"score_size", `{"type":"score","instructions":"x","criteria":["alone"]}`, "/questions/check/criteria", "expected 2 to 10 descriptive levels; got 1"},
		{"score_level", `{"type":"score","instructions":"x","criteria":["none",false]}`, "/questions/check/criteria/1", "expected nonblank text, an object, or an array"},
	} {
		t.Run(tc.name, func(t *testing.T) {

			raw := `{"state":"x","questions":{"other":{"type":"noul","instructions":"x"},"check":` + tc.question + `}}`
			_, err := validateQuery([]byte(raw))
			reason(t, err, "OPERATION_ARGUMENT_INVALID")
			var op *sdk.OperationError
			if !errors.As(err, &op) || op.Reason != strconv.Quote(tc.path)+": "+tc.message {
				t.Fatalf("diagnostic: %v", err)
			}
		})
	}
}

func TestDiagnosticEscapesCallerNames(t *testing.T) {
	raw := `{"state":"x","questions":{"a/b~c\n":{"type":"choice","instructions":"x","criteria":{"retry/now~":42}}}}`
	_, err := validateQuery([]byte(raw))
	var op *sdk.OperationError
	if !errors.As(err, &op) || !strings.HasPrefix(op.Reason, `"/questions/a~1b~0c\n/criteria/retry~1now~0": `) || strings.Contains(op.Reason, "\n") {
		t.Fatalf("ambiguous or multiline path: %v", err)
	}
}

func TestQueryValidationBoundariesUnchanged(t *testing.T) {
	for _, n := range []int{0, 1, 255, 256} {
		options := make([]string, n)
		for i := range options {
			options[i] = fmt.Sprintf(`"k%d":null`, i)
		}
		raw := `{"state":"x","questions":{"q":{"type":"choice","instructions":"x","criteria":{` + strings.Join(options, ",") + `}}}}`
		_, err := validateQuery([]byte(raw))
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
		raw := `{"state":"x","questions":{"q":{"type":"score","instructions":"x","criteria":` + string(c) + `}}}`
		_, err := validateQuery([]byte(raw))
		if (err == nil) != (n >= 2 && n <= 10) {
			t.Fatalf("score count %d: %v", n, err)
		}
	}
	for _, n := range []int{128, 129} {
		raw := `{"state":"x","questions":{"` + strings.Repeat("q", n) + `":{"type":"noul","instructions":"x"}}}`
		_, err := validateQuery([]byte(raw))
		if (err == nil) != (n == 128) {
			t.Fatalf("name bytes %d: %v", n, err)
		}
	}
}

func TestQueryRootAndNameDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"root", `[]`, "arguments: expected a JSON object"},
		{"state", `{"state":false,"questions":{}}`, `"/state": expected a string, object, or array`},
		{"questions", `{"state":"x"}`, `"/questions": expected a nonempty object of named questions`},
		{"blank_question", `{"state":"x","questions":{" \t":{"type":"noul","instructions":"x"},"other":{"type":"noul","instructions":"x"}}}`, `"/questions/ \t": question names must be nonblank and at most 128 bytes`},
		{"empty_question", `{"state":"x","questions":{"":{"type":"noul","instructions":"x"}}}`, `"/questions/": question names must be nonblank and at most 128 bytes`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateQuery([]byte(tc.raw))
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
			raw := `{"state":"x","questions":{` + string(key) + `:{"type":"noul","instructions":"x"}}}`
			path, message := argPath("/questions", name), "question names must be nonblank and at most 128 bytes"
			if option {
				raw = `{"state":"x","questions":{"q":{"type":"choice","instructions":"x","criteria":{` + string(key) + `:null}}}}`
				path, message = argPath("/questions/q/criteria", name), "option names must be nonblank and at most 128 bytes"
			}
			_, err := validateQuery([]byte(raw))
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
