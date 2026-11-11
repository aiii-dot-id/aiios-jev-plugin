package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
	"sort"
	"strings"
	"testing"
	"time"
)

type fake struct {
	vals   values
	files  map[string][]byte
	denied map[string]error
	start  int64
	clock  int64
	reads  []string
	lists  int
	posts  [][]byte
	opts   []*sdk.HTTPOptions
	model  string
	judge  func(f *fake, body []byte) (sdk.HTTPResult, error)
	noul   func(judged string) float64
}

func newFake() *fake {
	return &fake{vals: values{"api_key": "jev"}, files: map[string][]byte{}, denied: map[string]error{}, start: 1_000_000, model: "jev-1.14.0"}
}

func (f *fake) settings() (settingsReader, error) { return f.vals, nil }
func (f *fake) read(p string, off int64, n int) ([]byte, bool, int64, error) {
	f.reads = append(f.reads, p)
	if e := f.denied[p]; e != nil {
		return nil, false, 0, e
	}
	b, ok := f.files[p]
	if !ok {
		return nil, false, 0, &sdk.OperationError{Status: "failed", ReasonCode: "FS_NOT_FOUND", Reason: "no such file"}
	}
	end := min(int(off)+n, len(b))
	return b[off:end], end == len(b), int64(len(b)), nil
}
func (f *fake) list(folder string) ([]sdk.FileEntry, bool, error) {
	f.lists++
	prefix := within(folder, "")
	seen := map[string]bool{}
	var out []sdk.FileEntry
	for p, b := range f.files {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		name, rest, dir := strings.Cut(p[len(prefix):], "/")
		_ = rest
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, sdk.FileEntry{Name: name, Dir: dir, Size: int64(len(b))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if len(out) > 1024 {
		return out[:1024], true, nil
	}
	return out, false, nil
}
func (f *fake) get(string, *sdk.HTTPOptions) (sdk.HTTPResult, error) { return sdk.HTTPResult{}, nil }
func (f *fake) post(url string, o *sdk.HTTPOptions) (sdk.HTTPResult, error) {
	f.posts, f.opts = append(f.posts, []byte(o.Body)), append(f.opts, o)
	if f.judge != nil {
		return f.judge(f, []byte(o.Body))
	}
	return f.answer([]byte(o.Body))
}
func (f *fake) last() (time.Time, bool) {
	if f.clock == 0 {
		return time.Time{}, false
	}
	return time.UnixMilli(f.clock), true
}

type sent struct {
	Model     string
	State     json.RawMessage
	Questions map[string]struct {
		Type         string
		Instructions json.RawMessage
		Criteria     json.RawMessage
	}
	Assets map[string]struct{ Type, Format, Data_base64 string }
}

func readSent(t *testing.T, body []byte) sent {
	t.Helper()
	var s sent
	if err := json.Unmarshal(body, &s); err != nil {
		t.Fatalf("the request is not JSON: %v", err)
	}
	return s
}

func (f *fake) answer(body []byte) (sdk.HTTPResult, error) {
	var s sent
	if err := json.Unmarshal(body, &s); err != nil {
		return sdk.HTTPResult{Status: 422, Body: json.RawMessage(`{"detail":"not JSON"}`)}, nil
	}
	answers := map[string]any{}
	for name, q := range s.Questions {
		switch q.Type {
		case "noul":
			var in struct{ Item json.RawMessage }
			judged := q.Instructions
			if json.Unmarshal(q.Instructions, &in) == nil && in.Item != nil {
				judged = in.Item
			}
			p := 0.1
			if f.noul != nil {
				p = f.noul(string(judged))
			} else if bytes.Contains(judged, []byte("MATCH")) {
				p = 0.9
			}
			answers[name] = map[string]any{"type": "noul", "noul": p}
		case "choice":
			var c map[string]json.RawMessage
			json.Unmarshal(q.Criteria, &c)
			names := make([]string, 0, len(c))
			for k := range c {
				names = append(names, k)
			}
			sort.Strings(names)
			probs := map[string]float64{}
			for i, k := range names {
				probs[k] = 0.1 / float64(max(len(names)-2, 1))
				if i == 0 {
					probs[k] = 0.6
				} else if i == 1 {
					probs[k] = 0.3
				}
			}
			if len(names) == 1 {
				probs[names[0]] = 1
			} else if len(names) == 2 {
				probs[names[0]], probs[names[1]] = 0.7, 0.3
			}
			answers[name] = map[string]any{"type": "choice", "choice": names[0], "probabilities": probs, "confidence": 0.4}
		case "score":
			var levels []json.RawMessage
			json.Unmarshal(q.Criteria, &levels)
			probs, legend := map[string]float64{}, map[string]string{}
			for i, l := range levels {
				probs[fmt.Sprint(i)] = 1 / float64(len(levels))
				var s string
				if json.Unmarshal(l, &s) != nil {
					s = string(l)
				}
				legend[fmt.Sprint(i)] = s
			}
			answers[name] = map[string]any{"type": "score", "score": float64(len(levels)-1) / 2, "probabilities": probs, "legend": legend, "confidence": 0.5}
		case "box":
			answers[name] = map[string]any{"type": "box", "box": map[string]float64{"top": 0.25, "bottom": 0.82, "left": 0.44, "right": 0.84}, "confidence": 0.92}
		}
	}
	b, _ := json.Marshal(map[string]any{"model": f.model, "answers": answers, "usage": map[string]int{"input_tokens": len(body) / 3, "output_tokens": 5 * len(answers)}})
	return sdk.HTTPResult{Status: 200, Body: b, Effect: "performed"}, nil
}

func listed(args string) string {
	ms := members([]byte(args))
	if ms == nil {
		return args
	}
	out := []byte{'{'}
	for i, m := range ms {
		if i > 0 {
			out = append(out, ',')
		}
		v := m.val
		if _, is, _ := reference(v); m.name == "questions" && shape(v, "{") && !is {
			list := []byte{'['}
			for j, q := range members(v) {
				if j > 0 {
					list = append(list, ',')
				}
				list = quote(append(list, `{"id":`...), q.name)
				if _, ok := text(q.val); ok {
					list = append(append(list, `,"instructions":`...), q.val...)
				}
				for _, x := range members(q.val) {
					list = append(append(append(append(list, ','), x.key...), ':'), x.val...)
				}
				list = append(list, '}')
			}
			v = append(list, ']')
		}
		out = append(append(append(out, m.key...), ':'), v...)
	}
	return string(append(out, '}'))
}

func (f *fake) call(args string) sdk.Call {
	args = listed(args)
	if f.start != 0 {
		args = fmt.Sprintf(`{"_host_now_ms":%d,`, f.start) + strings.TrimPrefix(args, "{")
	}
	return sdk.Call{Operation: "ask", Arguments: json.RawMessage(args)}
}

func (f *fake) ask(t *testing.T, args string) (map[string]json.RawMessage, error) {
	t.Helper()
	out, err := ask(f.call(args), f)
	if err != nil {
		return nil, err
	}
	raw := out.(rawJSON)
	budget := defaultReplyBytes
	if n, ok := f.vals.Int("max_reply_bytes"); ok {
		budget = int(n)
	}
	if len(raw) > budget {
		t.Fatalf("a %d-byte reply over the %d-byte budget", len(raw), budget)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || sdk.ValidateStrict(raw) != nil {
		t.Fatalf("the reply is not strict JSON: %v\n%s", err, raw)
	}
	if !strings.HasPrefix(string(raw), `{"judge":`) {
		t.Fatalf("the reply does not name its judge first: %.80s", raw)
	}
	return m, nil
}

func (f *fake) must(t *testing.T, args string) map[string]json.RawMessage {
	t.Helper()
	m, err := f.ask(t, args)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

type rowView struct {
	Ref     string          `json:"ref"`
	I       *int            `json:"i"`
	SHA256  string          `json:"sha256"`
	Refused string          `json:"refused"`
	Long    string          `json:"long_context"`
	Orient  string          `json:"orientation"`
	Raw     json.RawMessage `json:"-"`
}

func rows(t *testing.T, m map[string]json.RawMessage) []rowView {
	t.Helper()
	var raws []json.RawMessage
	if err := json.Unmarshal(m["rows"], &raws); err != nil {
		t.Fatal(err)
	}
	out := make([]rowView, len(raws))
	for i, r := range raws {
		json.Unmarshal(r, &out[i])
		out[i].Raw = r
	}
	return out
}

func next(t *testing.T, m map[string]json.RawMessage) string {
	t.Helper()
	return string(m["next"])
}

func sum16(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:8]) }

func pngOf(w, h, pad int) []byte {
	b := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR")
	b = binary.BigEndian.AppendUint32(b, uint32(w))
	b = binary.BigEndian.AppendUint32(b, uint32(h))
	b = append(b, 8, 6, 0, 0, 0, 0, 0, 0, 0)
	return append(b, bytes.Repeat([]byte{0x5A}, pad)...)
}

func jpegOf(w, h, orient, pad int) []byte {
	b := []byte{0xFF, 0xD8}
	if orient > 0 {
		x := []byte("Exif\x00\x00MM\x00\x2a\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01")
		x = append(x, byte(orient>>8), byte(orient), 0, 0, 0, 0, 0, 0)
		b = append(append(b, 0xFF, 0xE1), byte((len(x)+2)>>8), byte(len(x)+2))
		b = append(b, x...)
		xmp := []byte("http://ns.adobe.com/xap/1.0/\x00<x/>")
		b = append(append(b, 0xFF, 0xE1, 0, byte(len(xmp)+2)), xmp...)
	}
	b = append(b, 0xFF, 0xC0, 0, 17, 8, byte(h>>8), byte(h), byte(w>>8), byte(w), 3, 1, 0x22, 0, 2, 0x11, 1, 3, 0x11, 1)
	b = append(b, bytes.Repeat([]byte{0x5A}, pad)...)
	return append(b, 0xFF, 0xD9)
}

func TestATextAndAnImageReferenceGoInPlace(t *testing.T) {
	f := newFake()
	note, photo := []byte("Refunds within 30 days.\n"), jpegOf(800, 600, 6, 2000)
	f.files["notes/policy.md"], f.files["photos/kitchen.jpg"] = note, photo
	m := f.must(t, `{"state":{"rule":{"$file":"notes/policy.md"},"photo":{"$file":"photos/kitchen.jpg"}},"questions":{"mug":{"type":"box","instructions":"the coffee mug in `+"`photo`"+`"},"covered":"Does `+"`rule`"+` cover it?"}}`)
	s := readSent(t, f.posts[0])
	var state map[string]string
	json.Unmarshal(s.State, &state)
	a, ok := s.Assets[state["photo"]]
	if state["rule"] != string(note) || !strings.HasPrefix(state["photo"], "img-") || len(state["photo"]) != 68 || !ok || a.Type != "image" || a.Format != "jpeg" || a.Data_base64 != base64.StdEncoding.EncodeToString(photo) {
		t.Fatalf("state %v, assets %v", state, s.Assets)
	}
	if f.opts[0].Headers["TypeSafe-Danger-Zone-Images"] != "true" || f.opts[0].Headers["TypeSafe-Danger-Zone-Long-Context"] != "" {
		t.Fatalf("headers %v", f.opts[0].Headers)
	}
	var sentFiles map[string]string
	json.Unmarshal(m["sent"], &sentFiles)
	if sentFiles["notes/policy.md"] != sum16(note) || sentFiles["photos/kitchen.jpg"] != sum16(photo) || len(sentFiles) != 2 {
		t.Fatalf("sent %v", sentFiles)
	}
	if !strings.Contains(string(m["orientation"]), `"photos/kitchen.jpg":"pixels stored rotated (EXIF 6); boxes refer to the stored pixels"`) || string(m["images"]) != `"TypeSafe: not yet production-ready"` {
		t.Fatalf("flags %s %s", m["orientation"], m["images"])
	}
	if string(m["answers"]) != `{"mug":{"box":{"top":0.25,"bottom":0.82,"left":0.44,"right":0.84},"confidence":0.92},"covered":{"noul":0.1}}` {
		t.Fatalf("answers %s", m["answers"])
	}

	f.files["photos/wide.png"], f.files["photos/tiny.jpg"] = pngOf(2000, 800, 100), jpegOf(100, 100, 0, 10)
	for p, want := range map[string]string{"photos/wide.png": "2000 × 800", "photos/tiny.jpg": "100 × 100"} {
		f.reads = nil
		_, err := f.ask(t, `{"state":{"$file":"`+p+`"},"questions":{"q":"Is it?"}}`)
		if code(err) != "JEV_IMAGE_OUT_OF_BOUNDS" || !strings.Contains(err.Error(), want) || len(f.reads) != 1 {
			t.Fatalf("%s: %v after %d reads", p, err, len(f.reads))
		}
	}
	f.files["bin/blob"] = []byte{0, 1, 2, 0xFF, 0xFE}
	if _, err := f.ask(t, `{"state":{"$file":"bin/blob"},"questions":{"q":"Is it?"}}`); code(err) != "JEV_FILE_NOT_TEXT" {
		t.Fatal(err)
	}
	if _, err := f.ask(t, `{"state":{"$file":"no/such.md"},"questions":{"q":"Is it?"}}`); code(err) != "JEV_FILE_UNREADABLE" || !strings.Contains(err.Error(), "FS_NOT_FOUND") {
		t.Fatal(err)
	}
	f.denied["secrets/x"] = &sdk.OperationError{Status: "denied", ReasonCode: "POLICY_DENY", Reason: "the operator has not granted this plugin files"}
	if _, err := f.ask(t, `{"state":{"$file":"secrets/x"},"questions":{"q":"Is it?"}}`); code(err) != "POLICY_DENY" || !strings.Contains(err.Error(), "secrets/x: the operator") {
		t.Fatal(err)
	}
	if len(f.posts) != 1 {
		t.Fatalf("%d requests", len(f.posts))
	}
}

func TestALegendIsKeptForCriteriaFromFiles(t *testing.T) {
	f := newFake()
	f.files["l/0.md"], f.files["l/1.md"] = []byte("no refund asked"), []byte("a refund asked")
	m := f.must(t, `{"state":"x","questions":{"read":{"type":"score","instructions":"How?","criteria":[{"$file":"l/0.md"},{"$file":"l/1.md"}]},"wrote":{"type":"score","instructions":"How?","criteria":["no","yes"]}}}`)
	if !strings.Contains(string(m["answers"]), `"read":{"score":0.5,"probabilities":{"0":0.5,"1":0.5},"confidence":0.5,"legend":{"0":"no refund asked","1":"a refund asked"}}`) ||
		!strings.Contains(string(m["answers"]), `"wrote":{"score":0.5,"probabilities":{"0":0.5,"1":0.5},"confidence":0.5}`) {
		t.Fatalf("%s", m["answers"])
	}
}

func TestASmallReferenceToALargeFileCostsOnePage(t *testing.T) {
	f := newFake()
	f.files["big.log"] = bytes.Repeat([]byte("x"), 10<<20)
	_, err := f.ask(t, `{"state":{"$file":"big.log"},"questions":{"q":"Is it?"}}`)
	if code(err) != "JEV_FILE_TOO_LARGE" || len(f.reads) != 1 || len(f.posts) != 0 || !strings.Contains(err.Error(), "10485760 bytes") {
		t.Fatalf("%v after %d reads, %d requests", err, len(f.reads), len(f.posts))
	}
}

func TestSixtyFiveReferencesAreRefused(t *testing.T) {
	f := newFake()
	refs := make([]string, 65)
	for i := range refs {
		p := fmt.Sprintf("r/%02d.txt", i)
		f.files[p] = []byte("r")
		refs[i] = `{"$file":"` + p + `"}`
	}
	_, err := f.ask(t, `{"state":[`+strings.Join(refs, ",")+`],"questions":{"q":"Is it?"}}`)
	if code(err) != "JEV_REFERENCES_TOO_MANY" || len(f.posts) != 0 {
		t.Fatal(err)
	}
	f.must(t, `{"state":[`+strings.Join(refs[:64], ",")+`],"questions":{"q":"Is it?"}}`)
	if len(f.posts) != 1 {
		t.Fatal("64 references not sent")
	}
}

func depth(v any) int {
	switch x := v.(type) {
	case map[string]any:
		d := 0
		for _, e := range x {
			d = max(d, depth(e))
		}
		return d + 1
	case []any:
		d := 0
		for _, e := range x {
			d = max(d, depth(e))
		}
		return d + 1
	}
	return 0
}

func TestADeepStructureReachesTheJudgeWhole(t *testing.T) {
	f := newFake()
	f.files["deep.txt"] = []byte("bottom")
	deep := func(n int, leaf string) string { return strings.Repeat("[", n) + leaf + strings.Repeat("]", n) }
	m := f.must(t, `{"state":`+deep(62, `{"$file":"deep.txt"}`)+`,"questions":{"q":{"type":"noul","instructions":`+deep(61, `"Is it?"`)+`}},"items":[`+deep(61, `"MATCH"`)+`]}`)
	if len(rows(t, m)) != 1 {
		t.Fatal("not judged")
	}
	var s map[string]any
	json.Unmarshal(f.posts[0], &s)
	if d := depth(s["state"]); d != 62 || !strings.Contains(string(f.posts[0]), `[["bottom"]]`) {
		t.Fatalf("state depth %d", d)
	}
	for name, q := range s["questions"].(map[string]any) {
		if d := depth(q.(map[string]any)["instructions"]); d > 64 {
			t.Fatalf("%s instructions %d deep", name, d)
		}
	}
}

func TestADotfileIsNotMatchedByStar(t *testing.T) {
	f := newFake()
	for _, p := range []string{"inbox/.hidden.eml", "inbox/a.eml", "inbox/b.txt", "inbox/sub.eml/x"} {
		f.files[p] = []byte("MATCH " + p)
	}
	m := f.must(t, `{"state":"x","questions":{"q":"Is it?"},"items":{"$files":"inbox/*.eml"}}`)
	rs := rows(t, m)
	if len(rs) != 1 || rs[0].Ref != "inbox/a.eml" || next(t, m) != "null" {
		t.Fatalf("%s next %s", m["rows"], m["next"])
	}
	m = f.must(t, `{"state":"x","questions":{"q":"Is it?"},"items":{"$files":"inbox/.*"}}`)
	if rs := rows(t, m); len(rs) != 1 || rs[0].Ref != "inbox/.hidden.eml" {
		t.Fatalf("%s", m["rows"])
	}
}

func fill(f *fake, folder string, n int, name func(i int) string, body func(i int) []byte) {
	for i := 0; i < n; i++ {
		f.files[within(folder, name(i))] = body(i)
	}
}

func TestAFolderPastTheListingIsRefused(t *testing.T) {
	f := newFake()
	fill(f, "big", 1100, func(i int) string { return fmt.Sprintf("n%04d.txt", i) }, func(int) []byte { return []byte("x") })
	_, err := f.ask(t, `{"state":"x","questions":{"q":"Is it?"},"items":{"$files":"big/*.eml"}}`)
	if code(err) != "JEV_LISTING_INCOMPLETE" || len(f.posts) != 0 || !strings.Contains(err.Error(), "big/*.eml") {
		t.Fatal(err)
	}
	fill(f, "big", 3, func(i int) string { return fmt.Sprintf("a%d.eml", i) }, func(int) []byte { return []byte("MATCH") })
	m := f.must(t, `{"state":"x","questions":{"q":"Is it?"},"items":{"$files":"big/*.eml"}}`)
	if len(rows(t, m)) != 3 || !strings.Contains(next(t, m), `"after":"big/n1020.txt"`) {
		t.Fatalf("%s %s", m["rows"], m["next"])
	}
	_, err = f.ask(t, `{"state":"x","questions":{"q":"Is it?"},"items":{"$files":"big/*.eml"},"from":`+next(t, m)+`}`)
	if code(err) != "JEV_LISTING_INCOMPLETE" || len(f.posts) != 1 {
		t.Fatal(err)
	}
}

func itemsCall(items, from string, extra string) string {
	s := `{"state":"Judge each item.","questions":{"q":"Does ` + "`item`" + ` say MATCH?"},"items":` + items + extra
	if from != "" {
		s += `,"from":` + from
	}
	return s + "}"
}

func TestWholeItemsArePackedAndNextCarriesTheModel(t *testing.T) {
	f := newFake()
	fill(f, "docs", 10, func(i int) string { return fmt.Sprintf("%02d.txt", i) }, func(i int) []byte { return bytes.Repeat([]byte("MATCH "), 10_000) })
	m := f.must(t, itemsCall(`{"$files":"docs/*.txt"}`, "", ""))
	if n := len(rows(t, m)); n != 2 || !strings.Contains(next(t, m), `"after":"docs/01.txt","model":"jev-1.14.0"`) || len(f.posts[0]) > maxRequestBytes {
		t.Fatalf("%d rows, next %s", n, m["next"])
	}
	f.model = "jev-1.15.0"
	m = f.must(t, itemsCall(`{"$files":"docs/*.txt"}`, next(t, m), `,"long_context":true`))
	if s := readSent(t, f.posts[1]); s.Model != "jev-1.14.0" || len(s.Questions) != 4 || len(f.posts[1]) > maxRequestBytes {
		t.Fatalf("model %s, %d questions", s.Model, len(s.Questions))
	}
	if rs := rows(t, m); len(rs) != 4 || rs[0].Ref != "docs/02.txt" || rs[0].Long != flagLongContext || !strings.Contains(next(t, m), `"model":"jev-1.14.0"`) {
		t.Fatalf("%v %s", rs, m["next"])
	}
}

func TestTooLongIsTheJudgesWordAndLongContextJudgesThatBatch(t *testing.T) {
	f := newFake()
	f.files["d/a.txt"] = []byte("MATCH a")
	f.files["d/b.txt"] = bytes.Repeat([]byte("MATCH "), 15_000)
	f.files["d/c.txt"] = []byte("MATCH c")
	tooLong := func(f *fake, body []byte) (sdk.HTTPResult, error) {
		if f.opts[len(f.opts)-1].Headers["TypeSafe-Danger-Zone-Long-Context"] == "" && len(body) > 80_000 {
			return sdk.HTTPResult{Status: 400, Body: json.RawMessage(`{"detail":"max_tokens_exceeded: 37504 tokens with one question; the limit is 32768"}`), Effect: "performed"}, nil
		}
		return f.answer(body)
	}
	f.judge = tooLong
	m := f.must(t, itemsCall(`{"$files":"d/*.txt"}`, "", ""))
	if rs := rows(t, m); len(rs) != 1 || rs[0].Ref != "d/a.txt" {
		t.Fatalf("the item over the estimate was packed with another: %s", m["rows"])
	}
	from := next(t, m)
	_, err := f.ask(t, itemsCall(`{"$files":"d/*.txt"}`, from, ""))
	if code(err) != "JEV_REQUEST_REFUSED" || !strings.Contains(err.Error(), "the same from with long_context true judges this batch onward") || len(readSent(t, f.posts[1]).Questions) != 1 {
		t.Fatal(err)
	}
	m = f.must(t, itemsCall(`{"$files":"d/*.txt"}`, from, `,"long_context":true`))
	if rs := rows(t, m); len(rs) != 2 || rs[0].Ref != "d/b.txt" || rs[0].Long == "" || rs[1].Ref != "d/c.txt" || rs[1].Long == "" || next(t, m) != "null" {
		t.Fatalf("%s %s", m["rows"], m["next"])
	}

	f.judge = func(f *fake, body []byte) (sdk.HTTPResult, error) {
		return sdk.HTTPResult{Status: 400, Body: json.RawMessage(`{"detail":"max_tokens_exceeded"}`), Effect: "performed"}, nil
	}
	if _, err := f.ask(t, `{"state":"x","questions":{"q":"Is it?"}}`); !strings.Contains(err.Error(), "asking again with long_context true") {
		t.Fatal(err)
	}
	if _, err := f.ask(t, `{"state":"x","questions":{"q":"Is it?"},"long_context":true}`); !strings.Contains(err.Error(), "even with long_context") || f.opts[len(f.opts)-1].Headers["TypeSafe-Danger-Zone-Long-Context"] != "true" {
		t.Fatal(err)
	}
}

func TestAnItemThatCannotFitAloneIsRefusedInItsRow(t *testing.T) {
	f := newFake()
	f.files["d/a.txt"] = bytes.Repeat([]byte("x"), 300_000)
	f.files["d/b.txt"] = []byte("MATCH")
	m := f.must(t, itemsCall(`{"$files":"d/*.txt"}`, "", ""))
	rs := rows(t, m)
	if len(rs) != 2 || rs[0].Ref != "d/a.txt" || !strings.HasPrefix(rs[0].Refused, "JEV_ITEM_TOO_LARGE: 300000 bytes") || rs[1].Ref != "d/b.txt" || rs[1].Refused != "" || next(t, m) != "null" {
		t.Fatalf("%s %s", m["rows"], m["next"])
	}
	refs := func(n int, tag string) string {
		s := make([]string, n)
		for i := range s {
			p := fmt.Sprintf("r/%s%02d", tag, i)
			f.files[p] = []byte("r")
			s[i] = `{"$file":"` + p + `"}`
		}
		return "[" + strings.Join(s, ",") + "]"
	}
	state := refs(60, "s")
	m = f.must(t, `{"state":`+state+`,"questions":{"q":"Is it?"},"items":[`+refs(5, "a")+`,`+refs(4, "b")+`]}`)
	rs = rows(t, m)
	if len(rs) != 2 || !strings.HasPrefix(rs[0].Refused, "JEV_ITEM_TOO_MANY_REFERENCES: names 5 files; the state and questions leave 4") || rs[1].Refused != "" || next(t, m) != "null" {
		t.Fatalf("%s %s", m["rows"], m["next"])
	}
}

func TestASharedPartWithNoRoomForAnItemIsRefusedUnsent(t *testing.T) {
	f := newFake()
	s := make([]string, 64)
	for i := range s {
		p := fmt.Sprintf("r/%02d", i)
		f.files[p] = []byte("r")
		s[i] = `{"$file":"` + p + `"}`
	}
	if _, err := f.ask(t, `{"state":[`+strings.Join(s, ",")+`],"questions":{"q":"Is it?"},"items":[1]}`); code(err) != "JEV_REFERENCES_TOO_MANY" {
		t.Fatal(err)
	}
	f.files["big.txt"] = bytes.Repeat([]byte("y"), 262_028)
	if _, err := f.ask(t, `{"state":{"$file":"big.txt"},"questions":{"q":"Is it?"},"items":[1]}`); code(err) != "OPERATION_ARGUMENT_INVALID" || !strings.Contains(err.Error(), "leave no room for an item") {
		t.Fatal(err)
	}
	if len(f.posts) != 0 {
		t.Fatal("sent")
	}

	f.files["state.txt"] = bytes.Repeat([]byte("s"), 150_000)
	item := `"` + strings.Repeat("i", 50_000) + `"`
	m := f.must(t, `{"state":{"$file":"state.txt"},"questions":{"q":"Is it?"},"items":[`+item+`,`+item+`,`+item+`]}`)
	if len(rows(t, m)) != 1 || len(f.posts) != 1 {
		t.Fatalf("%d rows", len(rows(t, m)))
	}
}

func TestTheScreenIsItsQuestionNotItsOrderOrMode(t *testing.T) {
	f := newFake()
	f.files["q.json"] = []byte(`[{"id":"q","instructions":"Does ` + "`item`" + ` say MATCH?"},{"id":"s","type":"score","instructions":"How much?","criteria":["none","some","all"]}]`)
	f.files["rule.md"] = []byte("Judge each item.")
	call := func(from, extra string) string {
		s := `{"state":{"$file":"rule.md"},"questions":{"$file":"q.json"},"items":["MATCH 0","x 1","MATCH 2","x 3"]` + extra
		if from != "" {
			s += `,"from":` + from
		}
		return s + "}"
	}
	f.vals["max_reply_bytes"] = 1800
	m := f.must(t, call("", `,"rank":"q"`))
	first := rows(t, m)
	if len(first) == 0 || len(first) == 4 {
		t.Fatalf("the budget did not split the screen: %d rows", len(first))
	}

	if !strings.Contains(string(first[0].Raw), `"legend":{"0":"none","1":"some","2":"all"}`) {
		t.Fatalf("legend: %s", first[0].Raw)
	}
	from := next(t, m)
	m = f.must(t, call(from, `,"rank":"s","long_context":true`))
	if len(rows(t, m)) == 0 {
		t.Fatal("rank and long_context changed the screen")
	}
	f.files["q.json"] = []byte(`[{"id":"q","instructions":"Does ` + "`item`" + ` say MATCH, now?"}]`)
	posts := len(f.posts)
	if _, err := f.ask(t, call(from, "")); code(err) != "JEV_SCREEN_CHANGED" {
		t.Fatal(err)
	}
	f.files["q.json"] = []byte(`[{"id":"q","instructions":"Does ` + "`item`" + ` say MATCH?"},{"id":"s","type":"score","instructions":"How much?","criteria":["none","some","all"]}]`)
	f.files["rule.md"] = []byte("Judge each item strictly.")
	if _, err := f.ask(t, call(from, "")); code(err) != "JEV_SCREEN_CHANGED" || len(f.posts) != posts {
		t.Fatal(err)
	}
	f.files["rule.md"] = []byte("Judge each item.")
	if _, err := f.ask(t, strings.Replace(call(from, ""), `"x 3"`, `"x 3","x 4"`, 1)); code(err) != "JEV_SCREEN_CHANGED" {
		t.Fatal(err)
	}
}

func TestAQuestionSetThatCouldNotFitIsRefusedUnsent(t *testing.T) {
	f := newFake()
	opts := make([]string, 120)
	for i := range opts {
		opts[i] = fmt.Sprintf(`"option number %03d":null`, i)
	}
	choice := `{"type":"choice","instructions":"Which?","criteria":{` + strings.Join(opts, ",") + `}}`
	_, err := f.ask(t, `{"state":"x","questions":{"a":"Is it?","b":`+choice+`,"c":`+choice+`,"d":`+choice+`}}`)
	if code(err) != "JEV_REPLY_WOULD_NOT_FIT" || !strings.Contains(err.Error(), "3 of the 4 questions would fit") || len(f.posts) != 0 {
		t.Fatal(err)
	}
	_, err = f.ask(t, `{"state":"x","questions":{"a":"Is it?","b":`+choice+`,"c":`+choice+`,"d":`+choice+`},"items":[1,2]}`)
	if code(err) != "JEV_REPLY_WOULD_NOT_FIT" || !strings.Contains(err.Error(), "One item's row") || len(f.posts) != 0 {
		t.Fatal(err)
	}
	f.must(t, `{"state":"x","questions":{"a":"Is it?","b":`+choice+`,"c":`+choice+`}}`)
}

func TestEveryOutcomeIsReturnedAtOnceAndNeverSentTwice(t *testing.T) {
	thirty := int64(30)
	for _, tc := range []struct {
		res  sdk.HTTPResult
		err  error
		code string
		says string
	}{
		{sdk.HTTPResult{Status: 429, RetryAfterS: &thirty, Body: json.RawMessage(`{"detail":"slow down"}`), Effect: "performed"}, &sdk.OperationError{Status: "failed", ReasonCode: "NET_REMOTE_OUTCOME_FAILED"}, "JEV_RATE_LIMITED", "retry_after_s: 30"},
		{sdk.HTTPResult{Effect: "not-performed"}, &sdk.OperationError{Status: "failed", ReasonCode: "NET_REMOTE_OUTCOME_FAILED", Reason: "transport: connection refused"}, "JEV_TRANSPORT_NOT_SENT", "nothing was answered or charged"},
		{sdk.HTTPResult{Effect: "unknown"}, &sdk.OperationError{Status: "failed", ReasonCode: "NET_EFFECT_UNKNOWN", Reason: "transport: EOF"}, "JEV_TRANSPORT_UNKNOWN", "may have been charged"},
	} {
		f := newFake()
		f.judge = func(*fake, []byte) (sdk.HTTPResult, error) { return tc.res, tc.err }
		_, err := f.ask(t, `{"state":"x","questions":{"q":"Is it?"}}`)
		if code(err) != tc.code || !strings.Contains(err.Error(), tc.says) || !strings.Contains(err.Error(), jevName) || len(f.posts) != 1 {
			t.Errorf("one state: %v after %d requests", err, len(f.posts))
		}
		f.files["d/a.txt"], f.files["d/b.txt"] = []byte("x"), bytes.Repeat([]byte("y"), 300_000)
		_, err = f.ask(t, itemsCall(`["MATCH",{"$file":"d/b.txt"}]`, "", ""))
		if code(err) != tc.code || len(f.posts) != 2 || tc.code != "JEV_RATE_LIMITED" && !strings.Contains(err.Error(), "the same from asks these items again") {
			t.Errorf("items: %v after %d requests", err, len(f.posts))
		}
	}
}

func TestTheAttemptIsSizedToTheTimeLeft(t *testing.T) {
	f := newFake()
	f.vals["timeout_ms"] = 60000
	f.clock = f.start + 20_000
	f.must(t, `{"state":"x","questions":{"q":"Is it?"}}`)
	f.clock = f.start + 80_000
	f.must(t, `{"state":"x","questions":{"q":"Is it?"}}`)
	f.clock = f.start - 50_000
	f.must(t, `{"state":"x","questions":{"q":"Is it?"}}`)
	if f.opts[0].TimeoutMS != 60000 || f.opts[1].TimeoutMS != 5000 || f.opts[2].TimeoutMS != 60000 {
		t.Fatalf("timeouts %d %d %d", f.opts[0].TimeoutMS, f.opts[1].TimeoutMS, f.opts[2].TimeoutMS)
	}
	f.clock = f.start + 84_500
	_, err := f.ask(t, `{"state":"x","questions":{"q":"Is it?"}}`)
	if code(err) != "JEV_NOT_SENT_OUT_OF_TIME" || len(f.posts) != 3 || !strings.Contains(err.Error(), "500 ms") {
		t.Fatal(err)
	}
	f.start, f.clock = 0, 0
	if _, err = f.ask(t, `{"state":"x","questions":{"q":"Is it?"}}`); code(err) != "JEV_NOT_SENT_OUT_OF_TIME" || len(f.posts) != 3 {
		t.Fatal(err)
	}
}

func screen(t *testing.T, f *fake, first string, more func(from string) string) ([]rowView, []map[string]json.RawMessage) {
	t.Helper()
	var all []rowView
	var replies []map[string]json.RawMessage
	call := first
	for i := 0; ; i++ {
		if i > 200 {
			t.Fatal("the screen does not end")
		}
		m := f.must(t, call)
		replies = append(replies, m)
		all = append(all, rows(t, m)...)
		if next(t, m) == "null" {
			return all, replies
		}
		call = more(next(t, m))
	}
}

func TestEveryItemIsAnsweredOnceAcrossCalls(t *testing.T) {
	f := newFake()
	fill(f, "m", 900, func(i int) string { return fmt.Sprintf("a%04d.txt", i) }, func(int) []byte { return []byte("no") })
	long := strings.Repeat("ł", 120)
	fill(f, "m", 70, func(i int) string { return fmt.Sprintf("b%03d-%s.eml", i, long) }, func(i int) []byte { return []byte(fmt.Sprintf("MATCH %d", i)) })
	fill(f, "m", 3, func(i int) string { return fmt.Sprintf("c%d.eml", i) }, func(int) []byte { return bytes.Repeat([]byte("MATCH "), 25_000) })
	call := func(from string) string { return itemsCall(`{"$files":"m/*.eml"}`, from, `,"long_context":true`) }
	var calls int
	var all []rowView
	cur := ""
	for {
		m := f.must(t, call(cur))
		calls++
		if calls == 1 && !strings.Contains(string(m["visited"]), "9") {
			t.Fatalf("visited %s", m["visited"])
		}
		rs := rows(t, m)
		all = append(all, rs...)
		if calls == 2 {
			f.files["m/zz-ahead.eml"] = []byte("MATCH ahead")
			f.files["m/a0000-behind.eml"] = []byte("MATCH behind")
			delete(f.files, "m/c2.eml")
		}
		if next(t, m) == "null" {
			break
		}
		cur = next(t, m)
		if calls > 50 {
			t.Fatal("no end")
		}
	}
	seen := map[string]int{}
	for _, r := range all {
		seen[r.Ref]++
		if r.Refused != "" {
			t.Errorf("%s refused: %s", r.Ref, r.Refused)
		}
	}
	for p := range f.files {
		if strings.HasPrefix(p, "m/b") || strings.HasPrefix(p, "m/c") || strings.HasPrefix(p, "m/z") {
			if seen[p] != 1 {
				t.Errorf("%s answered %d times", p, seen[p])
			}
		}
	}
	if seen["m/a0000-behind.eml"] != 0 || seen["m/c2.eml"] != 0 || len(seen) != 70+2+1 {
		t.Fatalf("%d answered: behind %d, removed %d", len(seen), seen["m/a0000-behind.eml"], seen["m/c2.eml"])
	}
	for i, body := range f.posts {
		s := readSent(t, body)
		if len(s.Questions) > 64 || len(body) > maxRequestBytes {
			t.Fatalf("request %d: %d questions, %d bytes", i, len(s.Questions), len(body))
		}
	}

	for _, body := range f.posts {
		if n := bytes.Count(body, []byte("MATCH MATCH MATCH")); n > 0 && len(readSent(t, body).Questions) > 1 && len(body) > 200_000 {
			t.Fatalf("two large items in one request (%d bytes)", len(body))
		}
	}
}

func TestReferencesAndImagesBoundEachBatch(t *testing.T) {
	f := newFake()
	fill(f, "r", 70, func(i int) string { return fmt.Sprintf("%02d.txt", i) }, func(i int) []byte { return []byte("MATCH") })
	all, replies := screen(t, f, itemsCall(`{"$files":"r/*.txt"}`, "", ""), func(from string) string { return itemsCall(`{"$files":"r/*.txt"}`, from, "") })
	if len(all) != 70 || len(rows(t, replies[0])) != 64 || len(replies) != 2 {
		t.Fatalf("%d rows in %d calls, first %d", len(all), len(replies), len(rows(t, replies[0])))
	}
	g := newFake()
	g.files["rule.md"] = []byte("A mug counts when its handle shows.")
	g.files["ref.png"] = pngOf(400, 400, 50)
	g.files["img/a.jpg"], g.files["img/b.png"] = jpegOf(640, 480, 0, 100), pngOf(320, 320, 100)
	call := func(from string) string {
		s := `{"state":{"rule":{"$file":"rule.md"},"reference":{"$file":"ref.png"}},"questions":{"same":"Is the mug in ` + "`item`" + ` the one in ` + "`reference`" + `?","where":{"type":"box","instructions":"the mug in ` + "`item`" + `"}},"items":{"$files":"img/*"}`
		if from != "" {
			s += `,"from":` + from
		}
		return s + "}"
	}

	h := newFake()
	h.files["img/a.jpg"] = jpegOf(640, 480, 0, 100)
	all, replies = screen(t, h, itemsCall(`["MATCH 0","MATCH 1",{"$file":"img/a.jpg"}]`, "", ""), func(from string) string { return itemsCall(`["MATCH 0","MATCH 1",{"$file":"img/a.jpg"}]`, from, "") })
	if len(all) != 3 || len(replies) != 2 || len(rows(t, replies[0])) != 2 || len(readSent(t, h.posts[0]).Assets) != 0 || len(readSent(t, h.posts[1]).Assets) != 1 {
		t.Fatalf("%d rows in %d calls", len(all), len(replies))
	}
	all, replies = screen(t, g, call(""), call)
	if len(all) != 2 || len(replies) != 2 || len(g.posts) != 2 {
		t.Fatalf("%d rows in %d calls", len(all), len(replies))
	}
	for i, body := range g.posts {
		s := readSent(t, body)
		var state map[string]string
		json.Unmarshal(s.State, &state)
		if state["rule"] != "A mug counts when its handle shows." || len(s.Assets) != 2 || g.opts[i].Headers["TypeSafe-Danger-Zone-Images"] != "true" {
			t.Fatalf("request %d: state %v, %d assets", i, state, len(s.Assets))
		}
		for _, q := range s.Questions {
			var in struct{ Question, Item string }
			if json.Unmarshal(q.Instructions, &in) != nil || !strings.HasPrefix(in.Item, "img-") || in.Item == state["reference"] {
				t.Fatalf("request %d: %s", i, q.Instructions)
			}
			if _, ok := s.Assets[in.Item]; !ok {
				t.Fatalf("request %d: the item's image is not in assets", i)
			}
		}
	}
}

func TestTheModelIsBoundByTheFirstAnswer(t *testing.T) {
	f := newFake()
	fill(f, "d", 1000, func(i int) string { return fmt.Sprintf("a%04d.txt", i) }, func(int) []byte { return []byte("no") })
	fill(f, "d", 20, func(i int) string { return fmt.Sprintf("z%02d.eml", i) }, func(int) []byte { return bytes.Repeat([]byte("x"), 300_000) })
	f.files["d/zz.eml"] = []byte("MATCH")
	f.vals["max_reply_bytes"] = 4800
	call := func(from string) string { return itemsCall(`{"$files":"d/*.eml"}`, from, "") }
	m := f.must(t, call(""))
	rs := rows(t, m)
	if string(m["model"]) != "null" || len(f.posts) != 0 || !strings.Contains(next(t, m), `"model":null`) || len(rs) == 0 || len(rs) == 21 || rs[len(rs)-1].Refused == "" {
		t.Fatalf("model %s, next %s, rows %s", m["model"], m["next"], m["rows"])
	}
	all := len(rs)
	from := next(t, m)
	for _, failed := range []sdk.HTTPResult{{Effect: "not-performed"}, {Effect: "unknown"}} {
		f.judge = func(*fake, []byte) (sdk.HTTPResult, error) {
			return failed, &sdk.OperationError{Status: "failed", ReasonCode: "NET_X"}
		}
		if _, err := f.ask(t, call(from)); err == nil {
			t.Fatal("a failed request answered")
		}
	}
	f.judge = nil
	f.model = "jev-1.14.1"
	m = f.must(t, call(from))
	rs = rows(t, m)
	all += len(rs)
	if all != 21 || rs[len(rs)-1].Ref != "d/zz.eml" || string(m["model"]) != `"jev-1.14.1"` || next(t, m) != "null" || len(f.posts) != 3 {
		t.Fatalf("%d rows; %s %s %s", all, m["rows"], m["model"], m["next"])
	}
	if s := readSent(t, f.posts[0]); s.Model != "jev-latest" {
		t.Fatalf("the first request named %s", s.Model)
	}
}

func TestRowsAreWholeAnswersRankedOnAnyOption(t *testing.T) {
	f := newFake()
	f.judge = func(f *fake, body []byte) (sdk.HTTPResult, error) {
		s := readSent(t, body)
		answers := map[string]any{}
		for name, q := range s.Questions {
			p := 0.05
			if bytes.Contains(q.Instructions, []byte("high")) {
				p = 0.15
			}
			if q.Type == "score" {
				answers[name] = map[string]any{"type": "score", "score": 1, "legend": map[string]string{"0": "a", "1": "b"}, "probabilities": map[string]float64{"0": 0.5, "1": 0.5}, "confidence": 0.1, "future": "kept"}
				continue
			}
			answers[name] = map[string]any{"type": "choice", "choice": "refund", "probabilities": map[string]float64{"refund": 0.5, "rebook": 0.35, "info": p, "other": 0.15 - p}, "confidence": 0.2}
		}
		b, _ := json.Marshal(map[string]any{"model": "jev-1.14.0", "answers": answers, "usage": map[string]int{"input_tokens": 9, "output_tokens": 9}})
		return sdk.HTTPResult{Status: 200, Body: b}, nil
	}
	m := f.must(t, `{"state":"x","questions":{"route":{"type":"choice","instructions":"Which?","criteria":{"refund":null,"rebook":null,"info":null,"other":null}},"s":{"type":"score","instructions":"How?","criteria":["a","b"]}},"items":["low 0","high 1","low 2","high 3"],"rank":"route.info"}`)
	rs := rows(t, m)
	order := []int{}
	for _, r := range rs {
		order = append(order, *r.I)
	}
	if fmt.Sprint(order) != "[1 3 0 2]" {
		t.Fatalf("order %v", order)
	}
	if !strings.Contains(string(rs[0].Raw), `"route":{"choice":"refund","probabilities":{"refund":0.5,"rebook":0.35,"info":0.15,"other":0},"confidence":0.2}`) ||
		!strings.Contains(string(rs[0].Raw), `"s":{"score":1,"probabilities":{"0":0.5,"1":0.5},"confidence":0.1,"future":"kept"}`) {
		t.Fatalf("%s", rs[0].Raw)
	}
}

func TestBandsAndAddedMembers(t *testing.T) {
	f := newFake()
	f.noul = func(in string) float64 {
		switch {
		case strings.Contains(in, "MATCH"):
			return 0.9
		case strings.Contains(in, "maybe"):
			return 0.5
		}
		return 0.1
	}
	m := f.must(t, itemsCall(`["MATCH","maybe","no","MATCH","x",{"$file":"none.txt"}]`, "", ""))
	if string(m["bands"]) != `{"q":{"yes, above 0.7":2,"unsettled":1,"no, below 0.3":2}}` {
		t.Fatalf("bands %s", m["bands"])
	}
	if rs := rows(t, m); !strings.HasPrefix(rs[5].Refused, "JEV_FILE_UNREADABLE") || rs[5].Ref != "none.txt" {
		t.Fatalf("%s", m["rows"])
	}
	big := strings.Repeat("z", 30_000)
	f.judge = func(f *fake, body []byte) (sdk.HTTPResult, error) {
		r, _ := f.answer(body)
		var v map[string]any
		json.Unmarshal(r.Body, &v)
		v["small"], v["huge"] = "kept", big
		r.Body, _ = json.Marshal(v)
		return r, nil
	}
	for _, args := range []string{`{"state":"x","questions":{"q":"Is it?"}}`, itemsCall(`["MATCH"]`, "", "")} {
		m = f.must(t, args)

		if string(m["small"]) != `"kept"` || m["huge"] != nil || strings.Count(string(m["dropped"]), "huge (no room)") != 1 {
			t.Fatalf("%v", m)
		}
	}
}

func (f *fake) readsOf() map[string]int {
	n := map[string]int{}
	for _, p := range f.reads {
		n[p]++
	}
	return n
}

func TestTheCallsReferenceBudgetCountsRefusedItems(t *testing.T) {
	f := newFake()
	items := make([]string, 65)
	for i := range items {
		items[i] = fmt.Sprintf(`{"$file":"missing/%02d"}`, i)
	}
	call := func(from string) string { return itemsCall("["+strings.Join(items, ",")+"]", from, "") }
	m := f.must(t, call(""))
	rs := rows(t, m)
	if len(rs) != 64 || len(f.reads) != 64 || !strings.Contains(next(t, m), `"after":63,`) {
		t.Fatalf("%d rows after %d reads; next %s", len(rs), len(f.reads), m["next"])
	}
	for _, r := range rs {
		if !strings.HasPrefix(r.Refused, "JEV_FILE_UNREADABLE") {
			t.Fatalf("%+v", r)
		}
	}
	m = f.must(t, call(next(t, m)))
	if rs = rows(t, m); len(rs) != 1 || rs[0].Ref != "missing/64" || next(t, m) != "null" || len(f.reads) != 65 || len(f.posts) != 0 {
		t.Fatalf("%s; next %s; %d reads, %d requests", m["rows"], m["next"], len(f.reads), len(f.posts))
	}
}

func TestAPartlyReadItemSpendsTheCallsReferences(t *testing.T) {
	f := newFake()
	state := make([]string, 10)
	for i := range state {
		p := fmt.Sprintf("shared/%02d", i)
		f.files[p] = []byte("s")
		state[i] = `{"$file":"` + p + `"}`
	}
	items := make([]string, 30)
	for i := range items {
		f.files[fmt.Sprintf("ok/%02d", i)] = []byte("MATCH")
		items[i] = fmt.Sprintf(`{"a":{"$file":"ok/%02d"},"b":{"$file":"gone/%02d"}}`, i, i)
	}
	call := func(from string) string {
		s := `{"state":[` + strings.Join(state, ",") + `],"questions":{"q":"Is it?"},"items":[` + strings.Join(items, ",") + `]`
		if from != "" {
			s += `,"from":` + from
		}
		return s + "}"
	}
	m := f.must(t, call(""))
	first := rows(t, m)
	if len(first) != 27 || len(f.reads) != 64 || !strings.Contains(next(t, m), `"after":26,`) {
		t.Fatalf("%d rows after %d reads; next %s", len(first), len(f.reads), m["next"])
	}
	m = f.must(t, call(next(t, m)))
	second := rows(t, m)
	if len(second) != 3 || len(f.reads) != 64+10+6 || next(t, m) != "null" || len(f.posts) != 0 {
		t.Fatalf("%d rows after %d reads; next %s", len(second), len(f.reads), m["next"])
	}
	seen := map[int]bool{}
	for _, r := range append(first, second...) {
		if r.I == nil || seen[*r.I] || !strings.HasPrefix(r.Refused, "JEV_FILE_UNREADABLE") {
			t.Fatalf("%+v", r)
		}
		seen[*r.I] = true
	}
	if len(seen) != 30 {
		t.Fatalf("%d items covered", len(seen))
	}
}

func TestAReferenceCountsEachTimeItIsNamed(t *testing.T) {
	f := newFake()
	f.files["shared/S"], f.files["big/P"], f.files["ok/X"] = []byte("rule"), bytes.Repeat([]byte("p"), 70_000), []byte("MATCH")
	state := []string{`{"$file":"big/P"}`}
	for i := 0; i < 9; i++ {
		state = append(state, `{"$file":"shared/S"}`)
	}
	items := make([]string, 60)
	for i := range items {
		items[i] = `{"$file":"ok/X"}`
	}
	call := func(from string) string {
		s := `{"state":[` + strings.Join(state, ",") + `],"questions":{"q":"Is it?"},"items":[` + strings.Join(items, ",") + `]`
		if from != "" {
			s += `,"from":` + from
		}
		return s + "}"
	}
	m := f.must(t, call(""))
	first := rows(t, m)
	if n := f.readsOf(); len(first) != 54 || len(f.reads) != 4 || n["big/P"] != 2 || n["shared/S"] != 1 || n["ok/X"] != 1 || !strings.Contains(next(t, m), `"after":53,`) {
		t.Fatalf("%d rows after reads %v; next %s", len(first), f.readsOf(), m["next"])
	}
	m = f.must(t, call(next(t, m)))
	second := rows(t, m)
	if len(second) != 6 || len(f.reads) != 8 || next(t, m) != "null" || len(f.posts) != 2 {
		t.Fatalf("%d rows after %d reads, %d requests; next %s", len(second), len(f.reads), len(f.posts), m["next"])
	}
	for _, r := range append(first, second...) {
		if r.Refused != "" || r.Ref != "ok/X" {
			t.Fatalf("%+v", r)
		}
	}
}

func TestARefusedItemLeavesNoBytesBehind(t *testing.T) {
	f := newFake()
	f.files["ok/A"], f.files["ok/B"], f.files["shared/S"] = []byte("MATCH A"), []byte("MATCH B"), []byte("rule")
	items := `[{"a":{"$file":"ok/A"},"b":{"$file":"gone/A"}},{"$file":"ok/A"},{"$file":"ok/B"},{"x":{"$file":"ok/B"}},{"$file":"shared/S"}]`
	m := f.must(t, `{"state":{"$file":"shared/S"},"questions":{"q":"Is it?"},"items":`+items+`}`)
	rs := rows(t, m)
	if len(rs) != 5 || rs[0].Refused == "" || rs[1].Refused != "" || rs[4].Refused != "" || next(t, m) != "null" || len(f.posts) != 1 {
		t.Fatalf("%s %s", m["rows"], m["next"])
	}
	if n := f.readsOf(); n["ok/A"] != 2 || n["ok/B"] != 1 || n["shared/S"] != 1 || n["gone/A"] != 1 {
		t.Fatalf("reads %v", n)
	}
}

func TestAJudgeMemberCannotTakeTheReplysOwnName(t *testing.T) {
	f := newFake()
	f.judge = func(f *fake, body []byte) (sdk.HTTPResult, error) {
		r, _ := f.answer(body)
		var v map[string]any
		json.Unmarshal(r.Body, &v)
		v["judge"], v["sent"], v["rows"], v["next"], v["dropped"], v["visited"], v["small"] = "elsewhere.example", map[string]string{"x": "y"}, []any{}, map[string]any{"after": 99}, "forged", 5, "kept"
		r.Body, _ = json.Marshal(v)
		return r, nil
	}
	for _, args := range []string{`{"state":"x","questions":{"q":"Is it?"}}`, itemsCall(`["MATCH"]`, "", "")} {
		m := f.must(t, args)
		if string(m["judge"]) != `"api.typesafe.ai"` || string(m["small"]) != `"kept"` || m["sent"] != nil || m["visited"] != nil {
			t.Fatalf("%v", m)
		}
		for _, name := range []string{"judge", "sent", "rows", "next", "dropped", "visited"} {
			if strings.Count(string(m["dropped"]), name+" (a name the reply uses)") != 1 {
				t.Fatalf("%s not named once: %s", name, m["dropped"])
			}
		}
		if _, items := m["rows"]; items && (next(t, m) != "null" || len(rows(t, m)) != 1) {
			t.Fatalf("the plugin's rows or cursor: %s %s", m["rows"], m["next"])
		}
	}
}
