package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const dropReserve = 256

var envelope = map[string]bool{"judge": true, "model": true, "answers": true, "sent": true, "long_context": true,
	"images": true, "orientation": true, "usage": true, "rows": true, "bands": true, "visited": true, "dropped": true,
	"next": true}

const leftOwn, leftRoom, leftLegend = " (a name the reply uses)", " (no room)", " (longer than its criteria allow)"

const worstNum = -1.2345678901234567e-308

type answer struct {
	noul, score, conf float64
	choice            string
	probs             []prob
	box               [4]float64
	legend            []string
	extra             []member
}

type prob struct {
	key string
	p   float64
}

var known = map[string]bool{"type": true, "noul": true, "choice": true, "score": true, "probabilities": true, "confidence": true, "legend": true, "box": true}

func readAnswer(q *question, raw rawJSON) answer {
	var a answer
	m := object(raw)
	a.noul, _ = number(m["noul"], 0, 1)
	a.score, _ = number(m["score"], 0, 10)
	a.conf, _ = number(m["confidence"], 0, 1)
	a.choice, _ = text(m["choice"])
	for _, p := range members(m["probabilities"]) {
		v, _ := number(p.val, 0, 1)
		a.probs = append(a.probs, prob{p.name, v})
	}
	b := object(m["box"])
	for i, k := range []string{"top", "bottom", "left", "right"} {
		a.box[i], _ = number(b[k], 0, 1)
	}
	if q.legend {
		l := object(m["legend"])
		for i := 0; i < q.levels; i++ {
			s, _ := text(l[strconv.Itoa(i)])
			a.legend = append(a.legend, s)
		}
	}
	for _, x := range members(raw) {
		if !known[x.name] {
			a.extra = append(a.extra, x)
		}
	}
	return a
}

func worstAnswer(q *question) answer {
	a := answer{noul: worstNum, score: worstNum, conf: worstNum, box: [4]float64{worstNum, worstNum, worstNum, worstNum}}
	for _, o := range q.options {
		if len(quote(nil, o)) > len(quote(nil, a.choice)) {
			a.choice = o
		}
		a.probs = append(a.probs, prob{o, worstNum})
	}
	for i := 0; i < q.levels; i++ {
		a.probs = append(a.probs, prob{strconv.Itoa(i), worstNum})
		if q.legend {
			a.legend = append(a.legend, strings.Repeat("x", legendCap(q, i)-2))
		}
	}
	return a
}

func legendCap(q *question, level int) int {
	if l := parts(q.criteria); level < len(l) {
		return 2*len(l[level]) + 16
	}
	return 16
}

type keeper func(n int) bool

func keepNone(int) bool { return false }

func appendAnswer(out []byte, q *question, a answer, keep keeper, prefix string) ([]byte, []string) {
	switch q.kind {
	case "noul":
		out = num(append(out, `{"noul":`...), a.noul)
	case "box":
		out = append(out, `{"box":{"top":`...)
		out = num(out, a.box[0])
		out = num(append(out, `,"bottom":`...), a.box[1])
		out = num(append(out, `,"left":`...), a.box[2])
		out = num(append(out, `,"right":`...), a.box[3])
		out = num(append(out, `},"confidence":`...), a.conf)
	default:
		if q.kind == "choice" {
			out = quote(append(out, `{"choice":`...), a.choice)
		} else {
			out = num(append(out, `{"score":`...), a.score)
		}
		ps := append([]prob(nil), a.probs...)
		sort.SliceStable(ps, func(i, j int) bool { return ps[i].p > ps[j].p })
		out = append(out, `,"probabilities":{`...)
		for i, p := range ps {
			if i > 0 {
				out = append(out, ',')
			}
			out = num(append(quote(out, p.key), ':'), p.p)
		}
		out = num(append(out, `},"confidence":`...), a.conf)
	}
	var dropped []string
	if q.legend && q.kind == "score" {
		l := []byte(`,"legend":{`)
		fits := true
		for i, s := range a.legend {
			v := quote(nil, s)
			fits = fits && len(v) <= legendCap(q, i)
			if i > 0 {
				l = append(l, ',')
			}
			l = append(append(quote(l, strconv.Itoa(i)), ':'), v...)
		}
		if fits {
			out = append(append(out, l...), '}')
		} else {
			dropped = append(dropped, prefix+"legend"+leftLegend)
		}
	}
	out, more := appendExtras(out, a.extra, keep, prefix)
	return append(out, '}'), append(dropped, more...)
}

func appendExtras(out []byte, extra []member, keep keeper, prefix string) ([]byte, []string) {
	var dropped []string
	for _, x := range extra {
		if keep(1 + len(x.key) + 1 + len(x.val)) {
			out = append(append(append(append(out, ','), x.key...), ':'), x.val...)
		} else {
			dropped = append(dropped, prefix+x.name+leftRoom)
		}
	}
	return out, dropped
}

type tokens struct {
	in, out int64
	extra   []member
}

var worstTokens = tokens{in: 9007199254740991, out: 9007199254740991}

func readTokens(raw rawJSON) tokens {
	u := object(raw)
	in, _ := number(u["input_tokens"], 0, 9007199254740991)
	o, _ := number(u["output_tokens"], 0, 9007199254740991)
	t := tokens{in: int64(in), out: int64(o)}
	for _, x := range members(raw) {
		if x.name != "input_tokens" && x.name != "output_tokens" {
			t.extra = append(t.extra, x)
		}
	}
	return t
}

func appendTokens(out []byte, t tokens, keep keeper) ([]byte, []string) {
	out = strconv.AppendInt(append(out, `,"usage":{"input_tokens":`...), t.in, 10)
	out = strconv.AppendInt(append(out, `,"output_tokens":`...), t.out, 10)
	out, dropped := appendExtras(out, t.extra, keep, "usage.")
	return append(out, '}'), dropped
}

func render(budget int, write func(keep keeper, note string) ([]byte, []string)) []byte {
	first, _ := write(keepNone, "")
	spare := budget - len(first) - dropReserve
	admit := func() keeper {
		left := spare
		return func(n int) bool {
			if n > left {
				return false
			}
			left -= n
			return true
		}
	}
	out, dropped := write(admit(), "")
	if len(dropped) == 0 {
		return out
	}
	note := fmt.Sprintf(droppedNote, strings.Join(dropped, ", "))
	for room := dropReserve - len(`,"dropped":`); len(quote(nil, note)) > room; {
		note = cut(note, len(note)-len("…")-1) + "…"
	}
	out, _ = write(admit(), note)
	return out
}

func appendNote(out []byte, note string) []byte {
	if note == "" {
		return out
	}
	return quote(append(out, `,"dropped":`...), note)
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

type oneReply struct {
	judge, model string
	qs           []*question
	answers      []answer
	sent, turned []sentFile
	long, images bool
	usage        tokens
	extra        []member
	collided     []string
}

func (o *oneReply) write(keep keeper, note string) ([]byte, []string) {
	out := quote([]byte(`{"judge":`), o.judge)
	out = quote(append(out, `,"model":`...), o.model)
	out = append(out, `,"answers":{`...)
	dropped := append([]string(nil), o.collided...)
	for i, q := range o.qs {
		if i > 0 {
			out = append(out, ',')
		}
		var d []string
		out, d = appendAnswer(append(quote(out, q.id), ':'), q, o.answers[i], keep, q.id+".")
		dropped = append(dropped, d...)
	}
	out = append(out, '}')
	if len(o.sent) > 0 {
		out = appendSent(append(out, `,"sent":`...), o.sent)
	}
	out = appendFlags(out, o.long, o.images, o.turned)
	out, d := appendTokens(out, o.usage, keep)
	dropped = append(dropped, d...)
	out, d = appendExtras(out, o.extra, keep, "")
	return append(appendNote(out, note), '}'), append(dropped, d...)
}

func (o *oneReply) bound() int {
	w := *o
	w.model, w.usage, w.extra = strings.Repeat("m", maxModelName), worstTokens, nil
	w.answers = make([]answer, len(o.qs))
	for i, q := range o.qs {
		w.answers[i] = worstAnswer(q)
	}
	b, _ := w.write(keepNone, "")
	return len(b) + dropReserve
}

func appendSent(out []byte, sent []sentFile) []byte {
	out = append(out, '{')
	for i, s := range sent {
		if i > 0 {
			out = append(out, ',')
		}
		out = quote(append(quote(out, s.path), ':'), s.digest)
	}
	return append(out, '}')
}

func appendFlags(out []byte, long, images bool, turned []sentFile) []byte {
	if long {
		out = quote(append(out, `,"long_context":`...), flagLongContext)
	}
	if images {
		out = quote(append(out, `,"images":`...), flagImages)
	}
	if len(turned) > 0 {
		out = append(out, `,"orientation":{`...)
		for i, t := range turned {
			if i > 0 {
				out = append(out, ',')
			}
			out = quote(append(quote(out, t.path), ':'), fmt.Sprintf(flagOrientation, atoi(t.digest)))
		}
		out = append(out, '}')
	}
	return out
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }
