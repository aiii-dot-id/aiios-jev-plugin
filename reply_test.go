package main

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
)

func TestNoAnswerPassesItsBound(t *testing.T) {
	rng := rand.New(rand.NewPCG(20261110, 2))
	nums := []float64{0, 1, 0.5, 0.33, 1e-05, 0.1234567890123456, 2.2250738585072014e-308, 9.999999999999999e-05}
	pick := func() float64 { return nums[rng.IntN(len(nums))] }
	name := func() string {
		n := 1 + rng.IntN(12)
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(1 + rng.IntN(126))
		}
		return string(b)
	}
	for trial := 0; trial < 500; trial++ {
		var qs []*question
		var as []answer
		for j := 0; j < 1+rng.IntN(5); j++ {
			q := &question{id: fmt.Sprintf("q%d", j), kind: []string{"noul", "choice", "score", "box"}[rng.IntN(4)], legend: rng.IntN(2) == 0}
			a := answer{noul: pick(), score: pick(), conf: pick(), box: [4]float64{pick(), pick(), pick(), pick()}}
			switch q.kind {
			case "choice":
				for k := 0; k < 1+rng.IntN(6); k++ {
					q.options = append(q.options, name())
					a.probs = append(a.probs, prob{q.options[k], pick()})
				}
				a.choice = q.options[rng.IntN(len(q.options))]
			case "score":
				q.levels = 2 + rng.IntN(9)
				levels := make([]string, q.levels)
				for k := range levels {
					levels[k] = strconv.Quote(name())
					a.probs = append(a.probs, prob{strconv.Itoa(k), pick()})
					a.legend = append(a.legend, name())
				}
				q.criteria = rawJSON("[" + strings.Join(levels, ",") + "]")
			}
			qs, as = append(qs, q), append(as, a)
		}
		o := &oneReply{judge: jevName, model: "jev-1.14.0", qs: qs, answers: as, sent: []sentFile{{"notes/a.md", "0123456789abcdef"}}, long: true, images: true, usage: tokens{in: 123, out: 4}}
		if out, _ := o.write(keepNone, ""); len(out)+dropReserve > o.bound() {
			t.Fatalf("one state: %d bytes over the bound %d", len(out)+dropReserve, o.bound())
		}
		w := row{ref: "inbox/" + name(), digest: "0123456789abcdef", answers: as, long: true, orient: 6}
		if out, _ := appendRow(nil, w, qs, keepNone); len(out)+1 > rowBound(qs, w.ref, 0, true, true) {
			t.Fatalf("a row: %d bytes over the bound %d", len(out)+1, rowBound(qs, w.ref, 0, true, true))
		}
	}
}
