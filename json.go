package main

import (
	"bytes"
	"encoding/json"
	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
	"strconv"
)

type rawJSON = json.RawMessage

func text(raw []byte) (string, bool) {
	return sdk.Object(append(append([]byte(`{"v":`), raw...), '}')).String("v")
}
func parts(raw []byte) []rawJSON {
	raw = bytes.TrimSpace(raw)
	if len(raw) < 2 {
		return nil
	}
	raw = raw[1 : len(raw)-1]
	var out []rawJSON
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
func object(raw []byte) map[string]rawJSON {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	p := parts(raw)
	out := make(map[string]rawJSON, len(p)/2)
	for i := 0; i+1 < len(p); i += 2 {
		k, _ := text(p[i])
		out[k] = p[i+1]
	}
	return out
}

type member struct {
	name string
	key  rawJSON
	val  rawJSON
}

func members(raw []byte) []member {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	p := parts(raw)
	out := make([]member, 0, len(p)/2)
	for i := 0; i+1 < len(p); i += 2 {
		k, _ := text(p[i])
		out = append(out, member{k, p[i], p[i+1]})
	}
	return out
}

func shape(raw []byte, kinds string) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && bytes.IndexByte([]byte(kinds), raw[0]) >= 0
}

const hexDigits = "0123456789abcdef"

func quote(out []byte, s string) []byte {
	out = append(out, '"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"' || c == '\\':
			out = append(out, '\\', c)
		case c < 0x20:
			out = append(out, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&15])
		default:
			out = append(out, c)
		}
	}
	return append(out, '"')
}

func quoted(n int) int { return 6*n + 2 }

const numBound = 24

func num(out []byte, f float64) []byte { return strconv.AppendFloat(out, f, 'g', -1, 64) }
