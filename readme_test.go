package main

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"
)

func TestREADMEExamplesMatchTheContract(t *testing.T) {
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile("(?s)```json\n(.*?)\n```").FindAllSubmatch(raw, -1)
	if len(blocks) != 4 {
		t.Fatalf("expected two request/reply pairs, got %d blocks", len(blocks))
	}
	for i := 0; i < len(blocks); i += 2 {
		a, err := parseAsk(blocks[i][1])
		if err != nil {
			t.Fatalf("README request %d: %v", i/2, err)
		}
		var reply struct {
			Judge   string                     `json:"judge"`
			Answers map[string]json.RawMessage `json:"answers"`
			Rows    []map[string]json.RawMessage
			Next    json.RawMessage
		}
		if err := json.Unmarshal(blocks[i+1][1], &reply); err != nil || reply.Judge == "" {
			t.Fatalf("README reply %d: %v", i/2, err)
		}
		for _, q := range a.qs {
			if a.items == nil && reply.Answers[q.id] == nil {
				t.Fatalf("README reply %d lacks %s", i/2, q.id)
			}
			for _, r := range reply.Rows {
				if r["refused"] == nil && r[q.id] == nil {
					t.Fatalf("README reply %d: a row lacks %s", i/2, q.id)
				}
			}
		}
	}
}
