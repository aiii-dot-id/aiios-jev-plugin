package main

import (
	"os"
	"regexp"
	"testing"
)

func TestREADMEExampleMatchesNativeContract(t *testing.T) {
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile("(?s)```json\n(.*?)\n```").FindAllSubmatch(raw, -1)
	if len(blocks) != 2 {
		t.Fatalf("expected one JSON request/response pair, got %d blocks", len(blocks))
	}
	questions, err := validateQuery(blocks[0][1])
	if err != nil {
		t.Fatalf("README request: %v", err)
	}
	if err := validateAnswers(blocks[1][1], questions); err != nil {
		t.Fatalf("README response: %v", err)
	}
}
