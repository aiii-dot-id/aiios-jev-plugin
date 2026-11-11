package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

type values map[string]any

func (v values) String(k string) (string, bool) { s, ok := v[k].(string); return s, ok }
func (v values) Handle(k string) (string, bool) { return v.String(k) }
func (v values) Int(k string) (int64, bool)     { n, ok := v[k].(int); return int64(n), ok }

func code(err error) string {
	var op *sdk.OperationError
	if errors.As(err, &op) {
		return op.ReasonCode
	}
	return ""
}

func TestOneWireTwoAddresses(t *testing.T) {
	c, err := configFrom(values{"api_key": "jev"})
	if err != nil || c.systemOne != "https://api.typesafe.ai/v1/systemone" || c.models != "https://api.typesafe.ai/v1/models" || c.auth != "jev" || c.judge != "api.typesafe.ai" {
		t.Fatalf("hosted: %+v %v", c, err)
	}
	c, err = configFrom(values{"backend": "local", "local_url": "http://judge.lan:8080/", "api_key": "jev"})
	if err != nil || c.systemOne != "http://judge.lan:8080/v1/systemone" || c.models != "http://judge.lan:8080/v1/models" || c.auth != "" || c.judge != "judge.lan:8080" {
		t.Fatalf("local: %+v %v", c, err)
	}
	if _, err := configFrom(values{}); code(err) != "JEV_UNCONFIGURED" {
		t.Fatalf("hosted without a key: %v", err)
	}
	if _, err := configFrom(values{"backend": "local", "api_key": "jev"}); code(err) != "JEV_UNCONFIGURED" {
		t.Fatalf("local without an address: %v", err)
	}
	for _, v := range []values{{"backend": "local", "local_url": "ftp://judge.lan"}, {"backend": "local", "local_url": "http://u:p@judge:8080"},
		{"backend": "both", "api_key": "jev"}, {"api_key": "jev", "timeout_ms": 10}, {"api_key": "jev", "max_reply_bytes": 1 << 20},
		{"api_key": "jev", "timeout_ms": 60001}, {"api_key": "jev", "max_reply_bytes": 28385}} {
		if _, err := configFrom(v); code(err) != "JEV_CONFIGURATION_INVALID" {
			t.Fatalf("%v accepted: %v", v, err)
		}
	}
}

func TestAnUpgradeKeepsEveryDestination(t *testing.T) {
	for _, tc := range []struct {
		v    values
		want string
		code string
	}{
		{values{"api_key": "k"}, "https://api.typesafe.ai/v1/systemone", ""},
		{values{"api_key": "k", "backend": "jev"}, "https://api.typesafe.ai/v1/systemone", ""},
		{values{"api_key": "k", "local_url": "http://old.lan:9000"}, "https://api.typesafe.ai/v1/systemone", ""},
		{values{"api_key": "k", "backend": "jev", "local_url": "http://old.lan:9000"}, "https://api.typesafe.ai/v1/systemone", ""},
		{values{"backend": "local", "local_url": "http://judge.lan:9000"}, "http://judge.lan:9000/v1/systemone", ""},
		{values{"api_key": "k", "backend": "local", "local_url": "http://judge.lan:9000"}, "http://judge.lan:9000/v1/systemone", ""},
		{values{"backend": "local"}, "", "JEV_UNCONFIGURED"},
		{values{"local_url": "http://judge.lan:9000"}, "", "JEV_UNCONFIGURED"},
	} {
		c, err := configFrom(tc.v)
		if code(err) != tc.code || c.systemOne != tc.want && tc.code == "" {
			t.Errorf("%v: %s %v", tc.v, c.systemOne, err)
		}
	}
}

func TestTheReplyBudgetIsASetting(t *testing.T) {
	c, _ := configFrom(values{"api_key": "jev"})
	if c.timeoutMS != 10000 || c.replyBytes != 16384 {
		t.Fatalf("defaults: %+v", c)
	}
	c, _ = configFrom(values{"api_key": "jev", "timeout_ms": 60000, "max_reply_bytes": 28384})
	if c.timeoutMS != 60000 || c.replyBytes != 28384 {
		t.Fatalf("operator's values: %+v", c)
	}
	if maxReplyBytes+replyRoom != 32000 || defaultReplyBytes+replyRoom != 20000 {
		t.Fatalf("the budget's bounds no longer fit the host's windows: %d, %d", maxReplyBytes, defaultReplyBytes)
	}
}

func TestAFailedCallSaysWhetherItWasSent(t *testing.T) {
	lost := fmt.Errorf("transport")
	for _, tc := range []struct {
		res  sdk.HTTPResult
		err  error
		want string
	}{
		{sdk.HTTPResult{Effect: "not-performed"}, lost, "JEV_TRANSPORT_NOT_SENT"},
		{sdk.HTTPResult{Effect: "unknown"}, lost, "JEV_TRANSPORT_UNKNOWN"},
		{sdk.HTTPResult{}, lost, "JEV_TRANSPORT_UNKNOWN"},
		{sdk.HTTPResult{Status: 200, Effect: "performed"}, lost, "JEV_TRANSPORT_UNKNOWN"},
		{sdk.HTTPResult{Effect: "not-performed"}, nil, "JEV_TRANSPORT_NOT_SENT"},
		{sdk.HTTPResult{Effect: "not-performed"}, sdk.Fail("NET_REMOTE_OUTCOME_FAILED", "transport: refused"), "JEV_TRANSPORT_NOT_SENT"},
		{sdk.HTTPResult{Effect: "unknown"}, sdk.Fail("NET_EFFECT_UNKNOWN", "lost"), "JEV_TRANSPORT_UNKNOWN"},
		{sdk.HTTPResult{}, sdk.Fail("NET_EFFECT_UNKNOWN", "lost"), "NET_EFFECT_UNKNOWN"},
		{sdk.HTTPResult{Effect: "not-performed"}, sdk.Deny("POLICY_DENY", "egress"), "POLICY_DENY"},
	} {
		_, err := response(tc.res, tc.err, jevName, defaultReplyBytes, "", "")
		if code(err) != tc.want {
			t.Errorf("%+v %v: %v, want %s", tc.res, tc.err, err, tc.want)
		}
	}
	_, err := response(sdk.HTTPResult{Effect: "not-performed"}, sdk.Fail("NET_REMOTE_OUTCOME_FAILED", "transport: refused"), jevName, defaultReplyBytes, "", "")
	if !strings.Contains(err.Error(), "Not sent") || !strings.Contains(err.Error(), "safe") || !strings.Contains(err.Error(), "NET_REMOTE_OUTCOME_FAILED, transport: refused") {
		t.Fatalf("not sent says so: %v", err)
	}
	_, err = response(sdk.HTTPResult{Effect: "unknown"}, lost, jevName, defaultReplyBytes, "", "")
	if !strings.Contains(err.Error(), "may have been charged") || !strings.Contains(err.Error(), "charged again") {
		t.Fatalf("sent says so: %v", err)
	}
	_, err = response(sdk.HTTPResult{Effect: "unknown"}, lost, jevName, defaultReplyBytes, "", againItems)
	if !strings.Contains(err.Error(), "the same from asks these items again") {
		t.Fatalf("items say what asking again does: %v", err)
	}
}

func TestARefusalCarriesTheRetryTime(t *testing.T) {
	thirty, five := int64(30), int64(5)
	_, err := response(sdk.HTTPResult{Status: 429, RetryAfterS: &thirty}, nil, jevName, defaultReplyBytes, "", "")
	if code(err) != "JEV_RATE_LIMITED" || !strings.Contains(err.Error(), "retry_after_s: 30") {
		t.Fatalf("429 with a time: %v", err)
	}
	_, err = response(sdk.HTTPResult{Status: 429}, nil, jevName, defaultReplyBytes, "", "")
	if code(err) != "JEV_RATE_LIMITED" || !strings.Contains(err.Error(), "no time to wait") {
		t.Fatalf("429 without a time: %v", err)
	}
	_, err = response(sdk.HTTPResult{Status: 503, RetryAfterS: &five}, nil, jevName, defaultReplyBytes, "", "")
	if code(err) != "JEV_UNAVAILABLE" || !strings.Contains(err.Error(), "retry_after_s: 5") {
		t.Fatalf("503 with a time: %v", err)
	}
}

type manifest struct {
	Version   string `json:"version"`
	Interface struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
	} `json:"interface"`
	Envelope []string `json:"capability_envelope"`
	Variants []struct {
		Caps []string `json:"variant_capabilities"`
	} `json:"variants"`
	Settings []aiiospkg.SettingDecl `json:"settings"`
}

func TestThePackageDeclaresWhatTheCodeReads(t *testing.T) {
	b, err := os.ReadFile("plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.Version != "0.2.0" || m.Interface.ID != "id.aiii.jev.core" || m.Interface.Version != 2 {
		t.Fatalf("version %s, interface %+v", m.Version, m.Interface)
	}
	if err := aiiospkg.ValidateSettings(m.Settings); err != nil {
		t.Fatal(err)
	}
	d := map[string]aiiospkg.SettingDecl{}
	for _, s := range m.Settings {
		d[s.Key] = s
	}
	for key, want := range map[string][3]int64{"timeout_ms": {defaultTimeoutMS, minTimeoutMS, maxTimeoutMS}, "max_reply_bytes": {defaultReplyBytes, minReplyBytes, maxReplyBytes}} {
		s := d[key]
		def, _ := s.Default.(float64)
		if s.Type != "integer" || s.Minimum == nil || s.Maximum == nil || int64(def) != want[0] || int64(*s.Minimum) != want[1] || int64(*s.Maximum) != want[2] {
			t.Fatalf("%s: %+v", key, s)
		}
	}
	if s := d["backend"]; s.Type != "enum" || strings.Join(s.Values, ",") != "jev,local" || s.Default != "jev" {
		t.Fatalf("backend: %+v", s)
	}
	if d["local_url"].Type != "string" || d["api_key"].Required || d["model"].ChoicesFrom != "models" {
		t.Fatal("local_url, an optional key and the model picker")
	}
	has := func(xs []string, x string) bool {
		for _, v := range xs {
			if v == x {
				return true
			}
		}
		return false
	}
	for _, c := range []string{jevHost, netLocal, fsSandbox} {
		if !has(m.Envelope, c) {
			t.Fatalf("envelope: %v", m.Envelope)
		}
		for _, v := range m.Variants {
			if !has(v.Caps, c) {
				t.Fatalf("variant: %v", v.Caps)
			}
		}
	}
}

func TestTheWordsAreTheApprovedWords(t *testing.T) {
	for _, w := range []struct {
		text  string
		bytes int
	}{{askSummary, 235}, {stateWords, 227}, {questionsWords, 251}, {instructionsWords, 200}, {criteriaWords, 200}, {itemsWords, 192}, {rankWords, 158}, {longContextWords, 142}, {fromWords, 161}} {
		if len(w.text) != w.bytes {
			t.Errorf("%d bytes, approved at %d: %q", len(w.text), w.bytes, w.text)
		}
	}
	if len(modelsSummary) > 256 {
		t.Fatal(modelsSummary)
	}
	type described struct {
		Description string `json:"description"`
	}
	var schema struct {
		Properties map[string]struct {
			Description string `json:"description"`
			Items       struct {
				Properties map[string]described `json:"properties"`
			} `json:"items"`
		} `json:"properties"`
	}
	b, err := os.ReadFile("schemas/ask_in.json")
	if err != nil || json.Unmarshal(b, &schema) != nil {
		t.Fatalf("schema: %v", err)
	}
	for k, want := range map[string]string{"state": stateWords, "questions": questionsWords, "items": itemsWords, "rank": rankWords, "long_context": longContextWords, "from": fromWords} {
		if got := schema.Properties[k].Description; got != want {
			t.Errorf("%s: the schema says %q", k, got)
		}
	}
	for k, want := range map[string]string{"instructions": instructionsWords, "criteria": criteriaWords} {
		if got := schema.Properties["questions"].Items.Properties[k].Description; got != want {
			t.Errorf("questions' %s: the schema says %q", k, got)
		}
	}
	files, _ := filepath.Glob("tests/*.json")
	stance := ""
	for _, f := range files {
		b, _ := os.ReadFile(f)
		var c struct {
			Operation string `json:"operation"`
			Why       string `json:"unconfirmed_by_design"`
		}
		if err := json.Unmarshal(b, &c); err != nil {
			t.Fatal(err)
		}
		if c.Operation == "ask" && c.Why != "" {
			stance = c.Why
		}
	}
	if !strings.Contains(stance, "api.typesafe.ai") || !strings.Contains(stance, "local") || !strings.Contains(stance, "files") {
		t.Fatalf("stance: %q", stance)
	}
}

func TestALocalJudgeAddressIsHeldToItsShape(t *testing.T) {
	for raw, want := range map[string]string{
		"http://judge.lan:8080":              "http://judge.lan:8080",
		"https://judge.lan/":                 "https://judge.lan",
		"http://judge.lan:8080/v1/systemone": "http://judge.lan:8080",
		"http://judge.lan/?q=1":              "",
		"judge.lan:8080":                     "",
		"http://judge.lan/#x":                "",
	} {
		if got, _, why := localBase(raw); got != want || (want == "") != (why != "") {
			t.Errorf("%q: %q %q", raw, got, why)
		}
	}
}
