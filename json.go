package main

// Walk spans only after sdk.ValidateStrict has checked the JSON domain (including
// duplicate names, Unicode and depth). Keep the guest free of reflective decoding.
import (
	"bytes"
	"encoding/json"
	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

func text(raw []byte) (string, bool) {
	return sdk.Object(append(append([]byte(`{"v":`), raw...), '}')).String("v")
}
func parts(raw []byte) []json.RawMessage {
	raw = bytes.TrimSpace(raw)
	if len(raw) < 2 {
		return nil
	}
	raw = raw[1 : len(raw)-1]
	var out []json.RawMessage
	start, depth, quoted, escaped := 0, 0, false, false
	for i, c := range raw {
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		switch c {
		case '"':
			quoted = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		case ',', ':':
			if depth == 0 {
				out = append(out, bytes.TrimSpace(raw[start:i]))
				start = i + 1
			}
		}
	}
	if tail := bytes.TrimSpace(raw[start:]); len(tail) > 0 {
		out = append(out, tail)
	}
	return out
}
func object(raw []byte) map[string]json.RawMessage {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	p := parts(raw)
	out := make(map[string]json.RawMessage, len(p)/2)
	for i := 0; i+1 < len(p); i += 2 {
		k, _ := text(p[i])
		out[k] = p[i+1]
	}
	return out
}
func shape(raw []byte, kinds string) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && bytes.IndexByte([]byte(kinds), raw[0]) >= 0
}
