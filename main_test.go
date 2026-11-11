package main

import (
	"encoding/json"
	"fmt"
	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
	"strings"
	"testing"
	"unicode/utf8"
)

const mixedQuery = `{"state":{"evidence":"tests passed"},"questions":{"n":{"type":"noul","instructions":"Did tests pass?"},"c":{"type":"choice","instructions":"Which action?","criteria":{"inspect":null,"test":"Run tests"}},"s":{"type":"score","instructions":"How complete?","criteria":["none","partial","full"]}}}`
const mixedReply = `{"model":"jev-1.13.0","answers":{"n":{"type":"noul","noul":0.9},"c":{"type":"choice","choice":"inspect","probabilities":{"inspect":0.8,"test":0.2},"confidence":0.6},"s":{"type":"score","score":1.5,"legend":{"0":"none","1":"partial","2":"full"},"probabilities":{"0":0,"1":0.5,"2":0.5},"confidence":0.4}},"usage":{"input_tokens":100,"output_tokens":20},"future_field":{"kept":true}}`

func reason(t *testing.T, err error, want string) {
	t.Helper()
	if code(err) != want {
		t.Fatalf("want %s, got %v", want, err)
	}
}

func checkAnswers(query, reply string) error {
	a, err := parseAsk([]byte(listed(query)))
	if err != nil {
		return err
	}
	if _, _, _, _, err := replyModel([]byte(reply)); err != nil {
		return err
	}
	answers := object(object([]byte(reply))["answers"])
	if len(answers) != len(a.qs) {
		return invalidReply("Answer IDs do not match the submitted questions")
	}
	for _, q := range a.qs {
		if err := q.check(answers[q.id], q.id); err != nil {
			return err
		}
	}
	return nil
}

func TestInvalidQueriesBeforeAnySettingsOrNetwork(t *testing.T) {
	bad := []string{`{}`, `{"state":null,"questions":{}}`, `{"state":"x","questions":{}}`, strings.Replace(mixedQuery, `"type":"noul"`, `"type":"chat"`, 1), strings.Replace(mixedQuery, `"instructions":"Did tests pass?"`, `"instructions":false`, 1), strings.Replace(mixedQuery, `["none","partial","full"]`, `["alone"]`, 1), strings.Replace(mixedQuery, `"inspect":null`, `"inspect":42`, 1), strings.Replace(mixedQuery, `"state":`, `"state":0,"state":`, 1), strings.Replace(mixedQuery, `"state":`, `"url":"https://evil.test","state":`, 1),
		`{"state":"x","questions":{"q":"  "}}`, `{"state":"x","questions":{"q":{"type":"box","instructions":"x","criteria":{}}}}`,
		`{"state":"x","questions":{"q":"Is it?"},"rank":"q"}`, `{"state":"x","questions":{"q":"Is it?"},"items":[]}`, `{"state":"x","questions":{"q":"Is it?"},"items":{"$files":"a/*","x":1}}`,
		`{"state":"x","questions":{"q":"Is it?"},"items":{"$files":"a/[*"}}`, `{"state":"x","questions":{"q":"Is it?"},"long_context":"yes"}`,
		`{"state":"x","questions":{"q":{"type":"choice","instructions":"x","criteria":{"$file":"c.txt"}}}}`,
		`{"state":"x","questions":{"q":"Is it?"},"items":[1],"from":{"after":null,"model":null}}`, `{"state":"x","questions":{"q":"Is it?"},"items":[1],"from":{"after":-1,"model":null,"screen":"` + strings.Repeat("0", 64) + `"}}`,
	}
	for _, raw := range bad {
		if _, err := parseAsk([]byte(listed(raw))); code(err) != "OPERATION_ARGUMENT_INVALID" {
			t.Errorf("accepted %s: %v", raw, err)
		}
	}
	for _, raw := range []string{strings.Replace(mixedQuery, `"state":`, `"_host_now_ms":123,"state":`, 1), `{"state":"x","questions":{"q":"Is it?"}}`,
		`{"state":"x","questions":{"$file":"q.json"}}`, `{"state":"x","questions":[{"id":"q","instructions":"Is it?"}]}`, `{"state":{"$file":"a.md","note":1},"questions":{"q":{"type":"box","instructions":{"$file":"i.txt"}}}}`,
		`{"state":"x","questions":{"q":"Is it?"},"items":{"$files":"*.eml"},"rank":"q","long_context":true,"from":{"after":"x.eml","model":"jev-1.14.0","screen":"` + strings.Repeat("a", 64) + `"}}`} {
		if _, err := parseAsk([]byte(listed(raw))); err != nil {
			t.Errorf("refused %s: %v", raw, err)
		}
	}
}

func TestStructuredDescriptionsAndUnicode(t *testing.T) {
	raw := `{"state":["evidence"],"questions":{"😀":{"type":"choice","instructions":{"question":"Pick","context":["a:b,c"]},"criteria":{"a":"first","b":["second"]}}}}`
	a, err := parseAsk([]byte(listed(raw)))
	if err != nil || len(a.qs) != 1 || a.qs[0].id != "😀" || strings.Join(a.qs[0].options, ",") != "a,b" {
		t.Fatalf("structured/Unicode: %v %v", a, err)
	}
}

func TestMalformedAnswerNeverBecomesSuccess(t *testing.T) {
	if err := checkAnswers(mixedQuery, mixedReply); err != nil {
		t.Fatal(err)
	}
	pairs := [][2]string{{`"noul":0.9`, `"noul":1.1`}, {`"noul":0.9`, `"noul":"0.9"`}, {`"n":{"type":"noul"`, `"missing":{"type":"noul"`}, {`"choice":"inspect"`, `"choice":"invented"`}, {`"inspect":0.8`, `"inspect":0.1`}, {`"confidence":0.6`, `"confidence":null`}, {`"score":1.5`, `"score":3`}, {`"0":"none"`, `"9":"none"`}, {`"input_tokens":100`, `"input_tokens":-1`}, {`"output_tokens":20`, `"output_tokens":0.5`}, {`"model":"jev-1.13.0"`, `"model":null`}}
	for _, pair := range pairs {
		if err := checkAnswers(mixedQuery, strings.Replace(mixedReply, pair[0], pair[1], 1)); err == nil {
			t.Errorf("accepted %s", pair[1])
		}
	}
	box := `{"state":"x","questions":{"b":{"type":"box","instructions":"the mug in ` + "`photo`" + `"}}}`
	good := `{"model":"jev-1.14.0","answers":{"b":{"type":"box","box":{"top":0.25,"bottom":0.82,"left":0.44,"right":0.84},"confidence":0.92}},"usage":{"input_tokens":1,"output_tokens":1}}`
	if err := checkAnswers(box, good); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{`"top":0.25`, `"top":0.9`}, {`"right":0.84`, `"right":1.2`}, {`"left":0.44,`, ``}, {`"confidence":0.92`, `"confidence":2`}} {
		if err := checkAnswers(box, strings.Replace(good, pair[0], pair[1], 1)); err == nil {
			t.Errorf("box accepted %s", pair[1])
		}
	}
}

func TestHTTPFailuresPreserveMeaning(t *testing.T) {
	for status, want := range map[int]string{0: "JEV_TRANSPORT_UNKNOWN", 401: "JEV_ACCESS_REFUSED", 403: "JEV_ACCESS_REFUSED", 422: "JEV_REQUEST_REFUSED", 429: "JEV_RATE_LIMITED", 529: "JEV_UNAVAILABLE"} {
		_, err := response(sdk.HTTPResult{Status: status}, nil, jevName, defaultReplyBytes, "", "")
		reason(t, err, want)
		if !strings.Contains(err.Error(), jevName) {
			t.Errorf("%d does not name the judge: %v", status, err)
		}
	}
	_, err := response(sdk.HTTPResult{}, sdk.Deny("NET_AUTH_PROFILE_NOT_ADMITTED", "no grant"), jevName, defaultReplyBytes, "", "")
	reason(t, err, "NET_AUTH_PROFILE_NOT_ADMITTED")
	_, err = response(sdk.HTTPResult{}, sdk.Fail("NET_EFFECT_UNKNOWN", "lost"), jevName, defaultReplyBytes, "", "")
	reason(t, err, "NET_EFFECT_UNKNOWN")
	_, err = response(sdk.HTTPResult{Status: 200, Body: json.RawMessage(mixedReply), Effect: "performed"}, fmt.Errorf("incomplete transport"), jevName, defaultReplyBytes, "", "")
	reason(t, err, "JEV_TRANSPORT_UNKNOWN")
	raw, err := response(sdk.HTTPResult{Status: 200, Body: json.RawMessage(mixedReply)}, nil, jevName, defaultReplyBytes, "", "")
	if err != nil || string(raw) != mixedReply {
		t.Fatalf("reply changed: %v %s", err, raw)
	}
	quoted, _ := json.Marshal(mixedReply)
	if raw, err = response(sdk.HTTPResult{Status: 200, Body: quoted}, nil, jevName, defaultReplyBytes, "", ""); err != nil || string(raw) != mixedReply {
		t.Fatal("JSON text response lost", err)
	}
	_, err = response(sdk.HTTPResult{Status: 200, Body: json.RawMessage(`"` + strings.Repeat(" ", maxJudgeReplyBytes+1) + `"`)}, nil, jevName, defaultReplyBytes, "", "")
	reason(t, err, "JEV_RESULT_TOO_LARGE")
}

func TestARefusalCarriesTheJudgesWords(t *testing.T) {
	quoted, _ := json.Marshal("Bad Gateway\n<html>")
	for _, tc := range []struct {
		status    int
		body      string
		want, not string
	}{
		{400, `{"detail":"max_tokens_exceeded: 70210 tokens, the limit is 65536"}`, `the judge said: "max_tokens_exceeded: 70210 tokens, the limit is 65536"; the judge's limits were exceeded: asking again with long_context true`, ""},
		{400, `{"detail":"questions: unknown type"}`, `the judge said: "questions: unknown type"`, "long_context"},
		{422, `{"detail":[{"loc":["body","questions","urgency","criteria"],"msg":"Field required","type":"missing"}]}`, `the judge said: [{"loc":["body","questions","urgency","criteria"],"msg":"Field required","type":"missing"}]`, ""},
		{429, `{"error":"busy"}`, `the judge said: {"error":"busy"}`, ""},
		{502, string(quoted), `the judge said: "Bad Gateway\n<html>"`, "\n"},
		{500, "null", "", "the judge said"},
		{500, "", "", "the judge said"},
		{400, `{"detail":"` + strings.Repeat("é", 2000) + `"}`, "… (cut at the 1024-byte reply budget; the rest is not kept)", ""},
	} {
		_, err := response(sdk.HTTPResult{Status: tc.status, Body: json.RawMessage(tc.body)}, nil, jevName, 1024, "", "")
		if err == nil || !strings.Contains(err.Error(), tc.want) || tc.not != "" && strings.Contains(err.Error(), tc.not) {
			t.Errorf("%d %.40q: %v", tc.status, tc.body, err)
			continue
		}
		if msg := err.Error(); !utf8.ValidString(msg) || len(msg) > 1024+512 {
			t.Errorf("%d: %d bytes, valid UTF-8 %v", tc.status, len(msg), utf8.ValidString(msg))
		}
	}
}

func TestRoundedProbabilitiesAreAccepted(t *testing.T) {
	query := `{"state":"x","questions":{"c":{"type":"choice","instructions":"Which?","criteria":{"a":null,"b":null,"c":null}},"s":{"type":"score","instructions":"How much?","criteria":["none","some","all"]}}}`
	reply := func(p string) string {
		return `{"model":"jev-1.13.0","answers":{"c":{"type":"choice","choice":"a","probabilities":{"a":` + p + `,"b":` + p + `,"c":` + p + `},"confidence":0.3},` +
			`"s":{"type":"score","score":1,"legend":{"0":"none","1":"some","2":"all"},"probabilities":{"0":` + p + `,"1":` + p + `,"2":` + p + `},"confidence":0.3}},"usage":{"input_tokens":1,"output_tokens":1}}`
	}

	for _, p := range []string{"0.33", "0.3333333333333333"} {
		if err := checkAnswers(query, reply(p)); err != nil {
			t.Errorf("three even options at %s: %v", p, err)
		}
	}
	for _, p := range []string{"0.32", "0.34", "0.4"} {
		if err := checkAnswers(query, reply(p)); code(err) != "JEV_RESPONSE_INVALID" {
			t.Errorf("three options at %s, beyond rounding: %v", p, err)
		}
	}
}

func TestTheRoundingBoundIsExact(t *testing.T) {
	for _, tc := range []struct {
		probs []string
		ok    bool
	}{
		{[]string{"0.34", "0.67"}, true},
		{[]string{"0.33", "0.66"}, true},
		{[]string{"0.34", "0.68"}, false},
		{[]string{"0.32", "0.66"}, false},
		{[]string{"0.26", "0.26", "0.25", "0.25"}, true},
		{[]string{"0.24", "0.24", "0.25", "0.25"}, true},
		{[]string{"0.27", "0.26", "0.25", "0.25"}, false},
		{[]string{"0.23", "0.24", "0.25", "0.25"}, false},
	} {
		names, members := make([]string, len(tc.probs)), make([]string, len(tc.probs))
		for i, p := range tc.probs {
			names[i] = fmt.Sprintf(`"o%d":null`, i)
			members[i] = fmt.Sprintf(`"o%d":%s`, i, p)
		}
		query := `{"state":"x","questions":{"c":{"type":"choice","instructions":"Which?","criteria":{` + strings.Join(names, ",") + `}}}}`
		reply := `{"model":"jev-1.13.0","answers":{"c":{"type":"choice","choice":"o0","probabilities":{` + strings.Join(members, ",") +
			`},"confidence":0.1}},"usage":{"input_tokens":1,"output_tokens":1}}`
		for range 64 {
			if err := checkAnswers(query, reply); (err == nil) != tc.ok {
				t.Fatalf("%v: accepted %v, want %v (%v)", tc.probs, err == nil, tc.ok, err)
			}
		}
	}
}

func TestTheModelsNameTheJudgeAndFitTheirBound(t *testing.T) {
	out, err := modelChoices([]byte(`{"models":[{"name":"jev-latest","description":"General-purpose system one model.","release_date":"2026-09-15"},{"name":"jev-1.13.0"}]}`), jevName)
	if err != nil || string(out) != `{"judge":"api.typesafe.ai","choices":[{"value":"jev-latest","label":"2026-09-15 General-purpose system one model."},{"value":"jev-1.13.0"}]}` {
		t.Fatalf("%s %v", out, err)
	}
	rows := make([]string, maxModels)
	for i := range rows {
		rows[i] = fmt.Sprintf(`{"name":"%03d%s","description":"%s","release_date":"2026-11-10"}`, i, strings.Repeat("m", maxModelName-3), strings.Repeat("\\u0001", 200))
	}
	out, err = modelChoices([]byte(`{"models":[`+strings.Join(rows, ",")+`]}`), strings.Repeat("\x01", 255))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > modelsResultBytes {
		t.Fatalf("%d bytes over the declared %d", len(out), modelsResultBytes)
	}
	var parsed struct {
		Choices []struct{ Value, Label string }
	}
	if err := json.Unmarshal(out, &parsed); err != nil || len(parsed.Choices) != maxModels || len(parsed.Choices[0].Label) > maxLabel {
		t.Fatalf("%v %d", err, len(parsed.Choices))
	}
}

func TestModelsRefuseIncompleteLists(t *testing.T) {
	for _, raw := range []string{`{"models":[]}`, `{"models":[{"name":"valid"},{}]}`, `{"models":[{"name":"valid"},null]}`, `{"models":[{"name":"valid","name":"other"}]}`} {
		if _, err := modelChoices([]byte(raw), jevName); err == nil {
			t.Error("accepted", raw)
		}
	}
	rows := make([]string, maxModels)
	for i := range rows {
		rows[i] = fmt.Sprintf(`{"name":"model-%d"}`, i)
	}
	if _, err := modelChoices([]byte(`{"models":[`+strings.Join(rows, ",")+`]}`), jevName); err != nil {
		t.Fatal(err)
	}
	if _, err := modelChoices([]byte(`{"models":[`+strings.Join(rows, ",")+`,{"name":"extra"}]}`), jevName); err == nil {
		t.Fatal("trimmed oversized list")
	}
}

func FuzzSpanWalker(f *testing.F) {
	for _, s := range []string{mixedQuery, mixedReply, `{"a":[1,{"b":"x:y,z"}]}`, `{"😀":"ok"}`} {
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
