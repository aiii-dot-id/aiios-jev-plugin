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
	if err != nil || c.systemOne != "https://api.typesafe.ai/v1/systemone" || c.models != "https://api.typesafe.ai/v1/models" || c.auth != "jev" {
		t.Fatalf("hosted: %+v %v", c, err)
	}
	c, err = configFrom(values{"backend": "local", "local_url": "http://judge.lan:8080/", "api_key": "jev"})
	if err != nil || c.systemOne != "http://judge.lan:8080/v1/systemone" || c.models != "http://judge.lan:8080/v1/models" || c.auth != "" {
		t.Fatalf("local: %+v %v", c, err)
	}
	if _, err := configFrom(values{}); code(err) != "JEV_UNCONFIGURED" {
		t.Fatalf("hosted without a key: %v", err)
	}
	if _, err := configFrom(values{"backend": "local", "api_key": "jev"}); code(err) != "JEV_UNCONFIGURED" {
		t.Fatalf("local without an address: %v", err)
	}
	for _, v := range []values{{"backend": "local", "local_url": "ftp://judge.lan"}, {"backend": "local", "local_url": "http://u:p@judge:8080"},
		{"backend": "both", "api_key": "jev"}, {"api_key": "jev", "timeout_ms": 10}, {"api_key": "jev", "max_reply_bytes": 1 << 20}} {
		if _, err := configFrom(v); code(err) != "JEV_CONFIGURATION_INVALID" {
			t.Fatalf("%v accepted: %v", v, err)
		}
	}
}

func TestTheReplyCapIsASetting(t *testing.T) {
	c, _ := configFrom(values{"api_key": "jev"})
	if c.timeoutMS != 10000 || c.replyBytes != 16384 {
		t.Fatalf("defaults: %+v", c)
	}
	c, _ = configFrom(values{"api_key": "jev", "timeout_ms": 30000, "max_reply_bytes": 32768})
	if c.timeoutMS != 30000 || c.replyBytes != 32768 {
		t.Fatalf("operator's values: %+v", c)
	}
	big := `{"model":"m","answers":{},"padding":"` + strings.Repeat("x", 20000) + `"}`
	if _, err := response(sdk.HTTPResult{Status: 200, Body: json.RawMessage(big)}, nil, 16384); code(err) != "JEV_RESULT_TOO_LARGE" ||
		!strings.Contains(err.Error(), "16384") {
		t.Fatalf("over the default cap: %v", err)
	}
	if raw, err := response(sdk.HTTPResult{Status: 200, Body: json.RawMessage(big)}, nil, 32768); err != nil || string(raw) != big {
		t.Fatalf("inside a raised cap: %v", err)
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
		{sdk.HTTPResult{Effect: "not-performed"}, sdk.Fail("NET_EFFECT_UNKNOWN", "lost"), "NET_EFFECT_UNKNOWN"},
	} {
		_, err := response(tc.res, tc.err, defaultReplyBytes)
		if code(err) != tc.want {
			t.Errorf("%+v %v: %v, want %s", tc.res, tc.err, err, tc.want)
		}
	}
	_, err := response(sdk.HTTPResult{Effect: "not-performed"}, lost, defaultReplyBytes)
	if !strings.Contains(err.Error(), "not sent") || !strings.Contains(err.Error(), "safe") {
		t.Fatalf("not sent says so: %v", err)
	}
	_, err = response(sdk.HTTPResult{Effect: "unknown"}, lost, defaultReplyBytes)
	if !strings.Contains(err.Error(), "was sent") || !strings.Contains(err.Error(), "charge again") {
		t.Fatalf("sent says so: %v", err)
	}
}

func TestARefusalCarriesTheRetryTime(t *testing.T) {
	thirty, five := int64(30), int64(5)
	_, err := response(sdk.HTTPResult{Status: 429, RetryAfterS: &thirty}, nil, defaultReplyBytes)
	if code(err) != "JEV_RATE_LIMITED" || !strings.Contains(err.Error(), "30 seconds") {
		t.Fatalf("429 with a time: %v", err)
	}
	_, err = response(sdk.HTTPResult{Status: 429}, nil, defaultReplyBytes)
	if code(err) != "JEV_RATE_LIMITED" || !strings.Contains(err.Error(), "no time to wait") {
		t.Fatalf("429 without a time: %v", err)
	}
	_, err = response(sdk.HTTPResult{Status: 503, RetryAfterS: &five}, nil, defaultReplyBytes)
	if code(err) != "JEV_UNAVAILABLE" || !strings.Contains(err.Error(), "5 seconds") {
		t.Fatalf("503 with a time: %v", err)
	}
}

type manifest struct {
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
	if !has(m.Envelope, netLocal) || !has(m.Envelope, jevHost) {
		t.Fatalf("envelope: %v", m.Envelope)
	}
	for _, v := range m.Variants {
		if !has(v.Caps, netLocal) {
			t.Fatalf("variant: %v", v.Caps)
		}
	}
}

func TestTheQuerySaysWhereItsTextGoes(t *testing.T) {
	if len(querySummary) > 256 || !strings.Contains(querySummary, "sent to") || !strings.Contains(querySummary, "api.typesafe.ai") && !strings.Contains(querySummary, "TypeSafe") || !strings.Contains(querySummary, "operator's network") {
		t.Fatalf("%d bytes: %q", len(querySummary), querySummary)
	}
	if len(modelsSummary) > 256 {
		t.Fatal(modelsSummary)
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
		if c.Operation == "query" && c.Why != "" {
			stance = c.Why
		}
	}
	if !strings.Contains(stance, "api.typesafe.ai") || !strings.Contains(stance, "local") {
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
		if got, why := localBase(raw); got != want || (want == "") != (why != "") {
			t.Errorf("%q: %q %q", raw, got, why)
		}
	}
}
