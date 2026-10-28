package main

import (
	"encoding/json"
	"errors"
	"fmt"
	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
	"strings"
	"testing"
)

const mixedQuery = `{"state":{"evidence":"tests passed"},"questions":{"n":{"type":"noul","instructions":"Did tests pass?"},"c":{"type":"choice","instructions":"Which action?","criteria":{"inspect":null,"test":"Run tests"}},"s":{"type":"score","instructions":"How complete?","criteria":["none","partial","full"]}}}`
const mixedReply = `{"model":"jev-1.13.0","answers":{"n":{"type":"noul","noul":0.9},"c":{"type":"choice","choice":"inspect","probabilities":{"inspect":0.8,"test":0.2},"confidence":0.6},"s":{"type":"score","score":1.5,"legend":{"0":"none","1":"partial","2":"full"},"probabilities":{"0":0,"1":0.5,"2":0.5},"confidence":0.4}},"usage":{"input_tokens":100,"output_tokens":20},"future_field":{"kept":true}}`

func reason(t *testing.T, err error, want string) {
	t.Helper()
	var e *sdk.OperationError
	if !errors.As(err, &e) || e.ReasonCode != want {
		t.Fatalf("want %s, got %v", want, err)
	}
}
func TestMixedQuestionsAndLosslessReply(t *testing.T) {
	q, err := validateQuery([]byte(mixedQuery))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := response(sdk.HTTPResult{Status: 200, Body: json.RawMessage(mixedReply)}, nil, defaultReplyBytes)
	if err != nil || string(raw) != mixedReply {
		t.Fatalf("reply changed: %v %s", err, raw)
	}
	if err = validateAnswers(raw, q); err != nil {
		t.Fatal(err)
	}
	quoted, _ := json.Marshal(mixedReply)
	raw, err = response(sdk.HTTPResult{Status: 200, Body: quoted}, nil, defaultReplyBytes)
	if err != nil || string(raw) != mixedReply {
		t.Fatal("JSON text response lost", err)
	}
}
func TestInvalidQueriesBeforeAnySettingsOrNetwork(t *testing.T) {
	bad := []string{`{}`, `{"state":null,"questions":{}}`, `{"state":"x","questions":{}}`, strings.Replace(mixedQuery, `"type":"noul"`, `"type":"chat"`, 1), strings.Replace(mixedQuery, `"instructions":"Did tests pass?"`, `"instructions":false`, 1), strings.Replace(mixedQuery, `["none","partial","full"]`, `["alone"]`, 1), strings.Replace(mixedQuery, `"inspect":null`, `"inspect":42`, 1), strings.Replace(mixedQuery, `"state":`, `"state":0,"state":`, 1), strings.Replace(mixedQuery, `"state":`, `"url":"https://evil.test","state":`, 1)}
	for _, raw := range bad {
		_, err := validateQuery([]byte(raw))
		if err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if _, err := validateQuery([]byte(strings.Replace(mixedQuery, `"state":`, `"_host_now_ms":123,"state":`, 1))); err != nil {
		t.Fatal(err)
	}
}
func TestStructuredDescriptionsAndUnicode(t *testing.T) {
	raw := `{"state":["evidence"],"questions":{"\ud83d\ude00":{"type":"choice","instructions":{"question":"Pick","context":["a:b,c"]},"criteria":{"a":"first","b":["second"]}}}}`
	q, err := validateQuery([]byte(raw))
	if err != nil || q["😀"] == nil {
		t.Fatalf("structured/Unicode: %v %v", q, err)
	}
}
func TestMalformedAnswerNeverBecomesSuccess(t *testing.T) {
	q, _ := validateQuery([]byte(mixedQuery))
	pairs := [][2]string{{`"noul":0.9`, `"noul":1.1`}, {`"noul":0.9`, `"noul":"0.9"`}, {`"n":{"type":"noul"`, `"missing":{"type":"noul"`}, {`"choice":"inspect"`, `"choice":"invented"`}, {`"inspect":0.8`, `"inspect":0.1`}, {`"confidence":0.6`, `"confidence":null`}, {`"score":1.5`, `"score":3`}, {`"0":"none"`, `"9":"none"`}, {`"input_tokens":100`, `"input_tokens":-1`}, {`"output_tokens":20`, `"output_tokens":0.5`}, {`"model":"jev-1.13.0"`, `"model":null`}, {`"noul":0.9`, `"noul":0.9,"noul":0.1`}}
	for _, pair := range pairs {
		if err := validateAnswers([]byte(strings.Replace(mixedReply, pair[0], pair[1], 1)), q); err == nil {
			t.Errorf("accepted %s", pair[1])
		}
	}
}
func TestHTTPFailuresPreserveMeaning(t *testing.T) {
	for code, want := range map[int]string{0: "JEV_TRANSPORT_UNKNOWN", 401: "JEV_ACCESS_REFUSED", 403: "JEV_ACCESS_REFUSED", 422: "JEV_REQUEST_REFUSED", 429: "JEV_RATE_LIMITED", 529: "JEV_UNAVAILABLE"} {
		_, err := response(sdk.HTTPResult{Status: code}, nil, defaultReplyBytes)
		reason(t, err, want)
	}
	_, err := response(sdk.HTTPResult{}, sdk.Deny("NET_AUTH_PROFILE_NOT_ADMITTED", "no grant"), defaultReplyBytes)
	reason(t, err, "NET_AUTH_PROFILE_NOT_ADMITTED")
	_, err = response(sdk.HTTPResult{}, sdk.Fail("NET_EFFECT_UNKNOWN", "lost"), defaultReplyBytes)
	reason(t, err, "NET_EFFECT_UNKNOWN")
	_, err = response(sdk.HTTPResult{Status: 200, Body: json.RawMessage(mixedReply), Effect: "performed"}, fmt.Errorf("incomplete transport"), defaultReplyBytes)
	reason(t, err, "JEV_TRANSPORT_UNKNOWN")
	_, err = response(sdk.HTTPResult{Status: 200, Body: json.RawMessage(strings.Repeat(" ", defaultReplyBytes+1))}, nil, defaultReplyBytes)
	reason(t, err, "JEV_RESULT_TOO_LARGE")
}
func TestModelsRefuseIncompleteLists(t *testing.T) {
	for _, raw := range []string{`{"models":[]}`, `{"models":[{"name":"valid"},{}]}`, `{"models":[{"name":"valid"},null]}`, `{"models":[{"name":"valid","name":"other"}]}`} {
		if _, err := modelChoices([]byte(raw)); err == nil {
			t.Error("accepted", raw)
		}
	}
	rows := make([]string, maxModels)
	for i := range rows {
		rows[i] = fmt.Sprintf(`{"name":"model-%d"}`, i)
	}
	if _, err := modelChoices([]byte(`{"models":[` + strings.Join(rows, ",") + `]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := modelChoices([]byte(`{"models":[` + strings.Join(rows, ",") + `,{"name":"extra"}]}`)); err == nil {
		t.Fatal("trimmed oversized list")
	}
}
func FuzzSpanWalker(f *testing.F) {
	for _, s := range []string{mixedQuery, mixedReply, `{"a":[1,{"b":"x:y,z"}]}`, `{"\ud83d\ude00":"ok"}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > maxRequestBytes || sdk.ValidateStrict([]byte(s)) != nil {
			return
		}
		var m map[string]json.RawMessage
		if json.Unmarshal([]byte(s), &m) != nil {
			return
		}
		got := object([]byte(s))
		if len(got) != len(m) {
			t.Fatal("member count")
		}
		for k, v := range m {
			if string(v) != string(got[k]) {
				t.Fatalf("span mismatch for %q", k)
			}
		}
	})
}
