package main

import (
	"encoding/json"
	"errors"
	"fmt"
	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
	"strings"
)

const (
	pluginID     = "id.aiii.jev"
	jevHost      = "net.outbound:api.typesafe.ai:443"
	jevURL       = "https://api.typesafe.ai/v1/systemone"
	modelsURL    = "https://api.typesafe.ai/v1/models"
	defaultModel = "jev-latest"

	maxRequestBytes = 256 << 10

	maxResultBytes   = 16 << 10
	requestTimeoutMS = 10000
	maxChoices       = 255
	maxLevels        = 10
	maxModels        = 256
)

func init() {
	p := sdk.New(pluginID)
	p.Describe("query", sdk.Descriptor{
		Summary: "Ask Jev focused questions about supplied evidence: noul for probability of yes, choice for alternatives, score for descriptive levels. Write one judgment per question; supply relevant context and explicit criteria, including insufficient evidence when appropriate. Answers are evidence for your reasoning; confidence is not a guarantee. Split the query if its reply is too large. State and questions are sent to TypeSafe.",
		Input:   "schemas/query_in.json", Output: "schemas/query_out.json", Effects: sdk.EffectsWriteExternal,
		Capabilities: []string{jevHost}, MaxResultBytes: maxResultBytes, Family: "jev",
		Keywords: []string{"jev", "judgment", "classify", "evaluate", "score", "decision"},
		Examples: []string{`{"state":{"evidence":"Unit tests passed; integration tests were not run."},"questions":{"covered":{"type":"noul","instructions":"Does evidence say integration tests were run?"}}}`},
	})
	p.Handle("query", query)
	p.Describe("models", sdk.Descriptor{Summary: "List Jev model choices for the model setting", Input: "schemas/models_in.json", Output: "schemas/models_out.json", Effects: sdk.EffectsReadExternal, Capabilities: []string{jevHost}, MaxResultBytes: maxResultBytes, Family: "jev", Keywords: []string{"jev", "models"}})
	p.Handle("models", models)
	p.Run()
}
func main() { sdk.MainDescribe() }

func settings() (string, string, error) {
	v, err := sdk.Settings.Load()
	if err != nil {
		return "", "", hostError(err)
	}
	handle, _ := v.Handle("api_key")
	if handle == "" {
		return "", "", sdk.Fail("JEV_UNCONFIGURED", "Set the Jev API key on the plugin card")
	}
	model, _ := v.String("model")
	if model == "" {
		model = defaultModel
	}
	return handle, model, nil
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
func hostError(err error) error {
	if d, ok := sdk.AsDenied(err); ok {
		return sdk.Deny(d.ReasonCode, d.Message)
	}
	var op *sdk.OperationError
	if errors.As(err, &op) {
		return err
	}
	return sdk.Fail("JEV_TRANSPORT_FAILED", "The host could not complete the Jev operation; no retry was made")
}
func query(c sdk.Call) (any, error) {
	q, err := validateQuery(c.Args())
	if err != nil {
		return nil, err
	}
	handle, model, err := settings()
	if err != nil {
		return nil, err
	}
	if !modelName(model) {
		return nil, sdk.Fail("JEV_CONFIGURATION_INVALID", "model must be a nonblank ASCII model name, at most 128 bytes")
	}

	body := `{"model":"` + model + `","state":` + string(c.Args().Raw("state")) + `,"questions":` + string(c.Args().Raw("questions")) + `}`
	if len(body) > maxRequestBytes {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "Request exceeds 256 KiB; pass less context or fewer questions")
	}
	res, err := sdk.HTTP.Post(jevURL, &sdk.HTTPOptions{Body: body, ContentType: "application/json", AuthProfile: handle, TimeoutMS: requestTimeoutMS})
	raw, err := response(res, err)
	if err != nil {
		return nil, err
	}
	if err = validateAnswers(raw, q); err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}
func response(res sdk.HTTPResult, err error) ([]byte, error) {
	var op *sdk.OperationError
	_, denied := sdk.AsDenied(err)
	if denied || errors.As(err, &op) && op.Status == "denied" {
		return nil, hostError(err)
	}
	if err != nil && (res.Status == 0 || res.Status == 200) {
		return nil, hostError(err)
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
			reason = "JEV_TRANSPORT_FAILED"
		}
		return nil, sdk.Fail(reason, fmt.Sprintf("Jev HTTP status %d; no answers accepted and no retry made", res.Status))
	}
	raw := []byte(res.Body)

	if s, ok := text(raw); ok {
		raw = []byte(s)
	}
	if len(raw) > maxResultBytes {
		return nil, sdk.Fail("JEV_RESULT_TOO_LARGE", "Reply exceeds 16 KiB; submit fewer questions or shorter option descriptions")
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
	handle, _, err := settings()
	if err != nil {
		return nil, err
	}
	res, err := sdk.HTTP.Get(modelsURL, &sdk.HTTPOptions{AuthProfile: handle, TimeoutMS: requestTimeoutMS})
	raw, err := response(res, err)
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
