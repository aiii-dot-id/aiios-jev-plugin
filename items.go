package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
	"path"
	"sort"
	"strconv"
	"strings"
)

const (
	tokensPerRequest, tokensPerQuestion, tokensLong = 64_000, 32_000, 262_000
	bytesPerToken                                   = 2.4

	maxNameBytes = 255

	refusalCap = 256
)

var rowKeys = map[string]bool{"ref": true, "i": true, "sha256": true, "refused": true, "long_context": true, "orientation": true}

type cursor struct {
	after  rawJSON
	model  string
	screen string
}

func parseCursor(raw rawJSON) (*cursor, error) {
	m := object(raw)
	if err := allowed(m, []string{"after", "model", "screen"}, false, "/from"); err != nil {
		return nil, err
	}
	c := &cursor{after: m["after"]}
	if _, isPath := text(c.after); !(isPath || string(c.after) == "null" || index(c.after) >= 0) {
		return nil, invalidArgAt("/from/after", "expected null, a path or an index: pass a reply's next whole")
	}
	if mv := m["model"]; string(mv) != "null" {
		s, ok := text(mv)
		if !ok || !modelName(s) {
			return nil, invalidArgAt("/from/model", "expected null or a model name: pass a reply's next whole")
		}
		c.model = s
	}
	s, _ := text(m["screen"])
	if _, err := hex.DecodeString(s); err != nil || len(s) != 64 {
		return nil, invalidArgAt("/from/screen", "expected a reply's screen digest: pass a reply's next whole")
	}
	c.screen = s
	return c, nil
}

func index(raw []byte) int {
	n, err := strconv.Atoi(string(trimmed(raw)))
	if err != nil || n < 0 {
		return -1
	}
	return n
}

func filesPattern(p string) error {
	folder, pattern := splitPattern(p)
	if pattern == "" || strings.ContainsRune(pattern, 0) {
		return invalidArgAt("/items/$files", "expected <folder>/<pattern>, the pattern naming files in that one folder")
	}
	if _, err := path.Match(pattern, ""); err != nil {
		return invalidArgAt("/items/$files", "the pattern is not one path.Match reads: "+err.Error())
	}
	if len(folder) > 512 {
		return invalidArgAt("/items/$files", "the folder is longer than the host's 512 bytes")
	}
	return nil
}

func splitPattern(p string) (folder, pattern string) {
	i := strings.LastIndex(p, "/")
	switch {
	case i < 0:
		return "", p
	case i == 0:
		return "/", p[1:]
	}
	return p[:i], p[i+1:]
}

func within(folder, name string) string {
	switch folder {
	case "":
		return name
	case "/":
		return "/" + name
	}
	return folder + "/" + name
}

func matches(pattern string, e sdk.FileEntry) bool {
	if e.Dir || strings.HasPrefix(e.Name, ".") && !strings.HasPrefix(pattern, ".") {
		return false
	}
	ok, _ := path.Match(pattern, e.Name)
	return ok
}

type rankBy struct {
	q      int
	kind   string
	option string
}

func parseRank(rank string, qs []*question) (rankBy, error) {
	if rank == "" {
		return rankBy{q: -1}, nil
	}
	for i, q := range qs {
		if q.id == rank && q.kind != "box" {
			return rankBy{q: i, kind: q.kind}, nil
		}
	}
	for i, q := range qs {
		if q.kind == "choice" && strings.HasPrefix(rank, q.id+".") && q.hasOption(rank[len(q.id)+1:]) {
			return rankBy{i, q.kind, rank[len(q.id)+1:]}, nil
		}
	}
	return rankBy{}, invalidArgAt("/rank", "names no noul, choice or score question, or no option of a choice")
}

func (r rankBy) key(w row) float64 {
	a := w.answers[r.q]
	switch r.kind {
	case "choice":
		want := a.choice
		if r.option != "" {
			want = r.option
		}
		for _, p := range a.probs {
			if p.key == want {
				return p.p
			}
		}
		return 0
	case "score":
		return a.score
	}
	return a.noul
}

func screenOf(state, questions, items []byte) string {
	h := sha256.New()
	for _, p := range [][]byte{[]byte("jev screen 1"), state, questions, trimmed(items)} {
		h.Write(p)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

type row struct {
	ref     string
	i       int
	digest  string
	answers []answer
	refused string
	long    bool
	orient  int
}

func appendRow(out []byte, w row, qs []*question, keep keeper) ([]byte, []string) {
	if w.ref != "" {
		out = quote(append(out, `{"ref":`...), w.ref)
	} else {
		out = strconv.AppendInt(append(out, `{"i":`...), int64(w.i), 10)
	}
	if w.refused != "" {
		return append(quote(append(out, `,"refused":`...), w.refused), '}'), nil
	}
	out = quote(append(out, `,"sha256":`...), w.digest)
	var dropped []string
	for j, q := range qs {
		var d []string
		out, d = appendAnswer(append(quote(append(out, ','), q.id), ':'), q, w.answers[j], keep, q.id+".")
		dropped = append(dropped, d...)
	}
	if w.long {
		out = quote(append(out, `,"long_context":`...), flagLongContext)
	}
	if w.orient > 1 {
		out = quote(append(out, `,"orientation":`...), fmt.Sprintf(flagOrientation, w.orient))
	}
	return append(out, '}'), dropped
}

type itemsReply struct {
	judge, model string
	qs           []*question
	rows         []row
	images       bool
	turned       []sentFile
	sent         bool
	usage        tokens
	files        bool
	visited      int
	extra        []member
	collided     []string
	next         *cursor
}

const bandYes, bandUnsettled, bandNo = "yes, above 0.7", "unsettled", "no, below 0.3"

func (x *itemsReply) write(keep keeper, note string) ([]byte, []string) {
	out := quote([]byte(`{"judge":`), x.judge)
	out = appendModel(append(out, `,"model":`...), x.model)
	out = append(out, `,"rows":[`...)
	dropped := append([]string(nil), x.collided...)
	for i, w := range x.rows {
		if i > 0 {
			out = append(out, ',')
		}
		var d []string
		out, d = appendRow(out, w, x.qs, keep)
		dropped = append(dropped, d...)
	}
	out = append(out, ']')
	out = x.appendBands(out, false)
	out = appendFlags(out, false, x.images, x.turned)
	if x.sent {
		var d []string
		out, d = appendTokens(out, x.usage, keep)
		dropped = append(dropped, d...)
	}
	if x.files {
		out = strconv.AppendInt(append(out, `,"visited":`...), int64(x.visited), 10)
	}
	out, d := appendExtras(out, x.extra, keep, "")
	dropped = append(dropped, d...)
	out = appendNote(out, note)
	out = append(out, `,"next":`...)
	if x.next == nil {
		out = append(out, "null"...)
	} else {
		out = append(append(out, `{"after":`...), x.next.after...)
		out = appendModel(append(out, `,"model":`...), x.next.model)
		out = append(quote(append(out, `,"screen":`...), x.next.screen), '}')
	}
	return append(out, '}'), dropped
}

func appendModel(out []byte, model string) []byte {
	if model == "" {
		return append(out, "null"...)
	}
	return quote(out, model)
}

func (x *itemsReply) appendBands(out []byte, worst bool) []byte {
	first := true
	for j, q := range x.qs {
		if q.kind != "noul" {
			continue
		}
		var yes, unsettled, no int
		for _, w := range x.rows {
			switch {
			case w.refused != "":
			case w.answers[j].noul > 0.7:
				yes++
			case w.answers[j].noul < 0.3:
				no++
			default:
				unsettled++
			}
		}
		if worst {
			yes, unsettled, no = 999999, 999999, 999999
		}
		if first {
			out, first = append(out, `,"bands":{`...), false
		} else {
			out = append(out, ',')
		}
		out = append(quote(out, q.id), `:{`...)
		out = strconv.AppendInt(append(quote(out, bandYes), ':'), int64(yes), 10)
		out = strconv.AppendInt(append(quote(append(out, ','), bandUnsettled), ':'), int64(unsettled), 10)
		out = strconv.AppendInt(append(quote(append(out, ','), bandNo), ':'), int64(no), 10)
		out = append(out, '}')
	}
	if !first {
		out = append(out, '}')
	}
	return out
}

func (x *itemsReply) fixedBound(worstAfter rawJSON) int {
	w := *x
	w.rows, w.extra = nil, nil
	w.model, w.sent, w.usage, w.images, w.visited = strings.Repeat("m", maxModelName), true, worstTokens, true, 9999999
	w.next = &cursor{after: worstAfter, model: w.model, screen: strings.Repeat("0", 64)}
	b, _ := w.write(keepNone, "")
	return len(b) + len(w.appendBands(nil, true)) - len(w.appendBands(nil, false)) + dropReserve
}

func rowBound(qs []*question, ref string, i int, long bool, image bool) int {
	w := row{ref: ref, i: i, digest: strings.Repeat("0", 16), long: long}
	if image {
		w.orient = 8
	}
	for _, q := range qs {
		w.answers = append(w.answers, worstAnswer(q))
	}
	b, _ := appendRow(nil, w, qs, keepNone)
	return len(b) + 1
}

func refusedRow(ref string, i int, code, why string) (row, int) {
	words := cut(code+": "+why, refusalCap)
	w := row{ref: ref, i: i, refused: words}
	b, _ := appendRow(nil, w, nil, keepNone)
	return w, len(b) + 1
}

type candidate struct {
	after rawJSON
	ref   string
	i     int
	value rawJSON
	match bool
}

func walk(h host, a *askArgs) (cands []candidate, truncated bool, err error) {
	after := rawJSON("null")
	if a.from != nil {
		after = a.from.after
	}
	if a.files == "" {
		items := parts(a.items)
		start := 0
		if string(after) != "null" {
			n := index(after)
			if n < 0 || n >= len(items) {
				return nil, false, invalidArgAt("/from/after", "an index of these items: pass a reply's next whole")
			}
			start = n + 1
		}
		for i := start; i < len(items); i++ {
			c := candidate{after: rawJSON(strconv.Itoa(i)), i: i, value: items[i], match: true}
			if p, is, _ := reference(items[i]); is {
				c.ref = p
			}
			cands = append(cands, c)
		}
		return cands, false, nil
	}
	folder, pattern := splitPattern(a.files)
	name := ""
	if string(after) != "null" {
		p, ok := text(after)
		prefix := within(folder, "")
		if !ok || !strings.HasPrefix(p, prefix) || p == prefix || strings.Contains(p[len(prefix):], "/") {
			return nil, false, invalidArgAt("/from/after", "a file in this folder: pass a reply's next whole")
		}
		name = p[len(prefix):]
	}
	entries, truncated, err := h.list(folder)
	if err != nil {
		return nil, false, hostError(err)
	}
	for _, e := range entries {
		if e.Name <= name {
			continue
		}
		p := within(folder, e.Name)
		cands = append(cands, candidate{after: quote(nil, p), ref: p, match: matches(pattern, e)})
	}
	return cands, truncated, nil
}

func itemFrag(qs []*question, k int, item rawJSON) []byte {
	var out []byte
	for j, q := range qs {
		out = q.wire(append(quote(append(out, ','), fmt.Sprintf("q%d_%d", j, k)), ':'), item)
	}
	return out
}

func askItems(c sdk.Call, h host, cfg config, model string, a *askArgs, state rawJSON, r *resolver) (any, error) {
	for _, q := range a.qs {
		if rowKeys[q.id] {
			return nil, invalidArgAt(argPath("/questions", q.id), "with items, a question's id cannot be a row's own member (ref, i, sha256, refused, long_context, orientation)")
		}
	}
	rank, err := parseRank(a.rank, a.qs)
	if err != nil {
		return nil, err
	}
	screen := screenOf(state, wireQuestions(a.qs), a.items)
	if a.from != nil && a.from.screen != screen {
		return nil, sdk.Fail("JEV_SCREEN_CHANGED", screenChanged)
	}
	if r.refs >= maxRefs {
		return nil, sdk.Fail("JEV_REFERENCES_TOO_MANY", fmt.Sprintf(sharedTooManyRef, r.refs, maxRefs))
	}
	shared := request(model, state, []byte("{}"), r.assets)
	if least := len(shared) + len(itemFrag(a.qs, 0, rawJSON("null"))); least > maxRequestBytes {
		return nil, invalidArg(fmt.Sprintf(sharedTooLarge, len(shared), maxRequestBytes))
	}
	x := &itemsReply{judge: cfg.judge, qs: a.qs, turned: r.turned, files: a.files != ""}
	if a.from != nil {
		x.model = a.from.model
	}

	worstRef, worstI := "", 0
	if x.files {
		folder, _ := splitPattern(a.files)
		worstRef = within(folder, "") + strings.Repeat("\x00", maxNameBytes)
	} else {
		items := parts(a.items)
		worstI = len(items) - 1
		for _, it := range items {
			if p, is, _ := reference(it); is && len(quote(nil, p)) > len(quote(nil, worstRef)) {
				worstRef = p
			}
		}
	}
	worstAfter := quote(nil, worstRef)
	if !x.files {
		worstAfter = rawJSON(strconv.Itoa(worstI))
	}
	fixed := x.fixedBound(worstAfter)
	worstRow := max(rowBound(a.qs, worstRef, worstI, a.long, true), rowBound(a.qs, "", worstI, a.long, true))
	if _, n := refusedRow(worstRef, worstI, "", strings.Repeat("x", refusalCap)); n > worstRow {
		worstRow = n
	}
	if fixed+worstRow > cfg.replyBytes {
		fit := len(a.qs)
		for ; fit > 0 && fixed+rowBound(a.qs[:fit], worstRef, worstI, a.long, true) > cfg.replyBytes; fit-- {
		}
		return nil, sdk.Fail("JEV_REPLY_WOULD_NOT_FIT", fmt.Sprintf(rowWouldNotFit, worstRow, cfg.replyBytes-fixed, cfg.replyBytes, fit, len(a.qs)))
	}

	cands, truncated, err := walk(h, a)
	if err != nil {
		return nil, err
	}
	limit := tokensPerRequest
	perQuestion := tokensPerQuestion
	if a.long {
		limit, perQuestion = tokensLong, tokensLong
	}
	type pending struct {
		row  int
		frag []byte
	}
	var (
		batch    []pending
		frags    []byte
		assets   = append([]byte(nil), r.assets...)
		size     = len(shared)
		textSize = len(shared) - len(r.assets)

		spent  = r.refs
		images = r.images > 0
		closed bool
		used   = fixed
		last   rawJSON
		walked int
	)
	consume := func(c candidate) {
		last = c.after
		walked++
	}
	for _, cand := range cands {
		x.visited++
		if !cand.match {
			consume(cand)
			continue
		}
		place := "/items/" + strconv.Itoa(cand.i)
		if x.files {
			place = "/items/" + cand.ref
		}
		refuse := func(code, why string) bool {
			w, n := refusedRow(cand.ref, cand.i, code, why)
			if used+n > cfg.replyBytes {
				return false
			}
			used += n
			x.rows = append(x.rows, w)
			consume(cand)
			return true
		}
		if closed {
			break
		}
		n := 1
		if cand.value != nil && cand.ref == "" {
			n = refCount(cand.value)
		}
		if n > maxRefs-r.refs {
			if !refuse("JEV_ITEM_TOO_MANY_REFERENCES", fmt.Sprintf(itemTooMany, n, maxRefs-r.refs, maxRefs)) {
				break
			}
			continue
		}
		if spent+n > maxRefs {
			break
		}

		ir := newResolver(h, maxRequestBytes-len(shared))
		ir.base, ir.refs = r.files, spent
		var item rawJSON
		if cand.ref != "" {
			item, err = ir.place(cand.ref, place)
		} else {
			item, err = ir.resolve(cand.value, place)
		}
		spent = ir.refs
		if err != nil {
			if t, ok := err.(*tooLarge); ok {
				if !refuse("JEV_ITEM_TOO_LARGE", fmt.Sprintf(itemTooLarge, t.size, maxRequestBytes, maxRequestBytes-len(shared))) {
					break
				}
				continue
			}
			if !aboutFile(err) {
				return nil, err
			}
			op, _ := sdk.AsOperationError(err)
			if !refuse(op.ReasonCode, strings.TrimPrefix(op.Reason, cand.ref+": ")) {
				break
			}
			continue
		}
		alone := len(shared) + len(itemFrag(a.qs, 0, item)) + len(ir.assets)
		if alone > maxRequestBytes {
			if !refuse("JEV_ITEM_TOO_LARGE", fmt.Sprintf(itemTooLargeQ, len(item), len(a.qs), alone, maxRequestBytes)) {
				break
			}
			continue
		}
		frag := itemFrag(a.qs, len(batch), item)
		over := float64(textSize+len(frag)) > float64(limit)*bytesPerToken
		for _, q := range a.qs {
			over = over || float64(len(state)+len(q.wire(nil, item))) > float64(perQuestion)*bytesPerToken
		}
		image := ir.images > 0
		if len(batch) > 0 && (image || over || size+len(frag)+len(ir.assets) > maxRequestBytes) {
			break
		}
		rb := rowBound(a.qs, cand.ref, cand.i, a.long, image)
		if used+rb > cfg.replyBytes {
			break
		}
		used += rb
		w := row{ref: cand.ref, i: cand.i, long: a.long}
		if cand.ref != "" {
			w.digest = ir.sent[0].digest
			if len(ir.turned) > 0 {
				w.orient = atoi(ir.turned[0].digest)
			}
		} else {
			w.digest = digest(item)
		}
		x.rows = append(x.rows, w)
		batch = append(batch, pending{len(x.rows) - 1, frag})
		frags = append(frags, frag...)
		assets = append(assets, ir.assets...)
		size += len(frag) + len(ir.assets)
		textSize += len(frag)
		for p, f := range ir.files {
			r.files[p] = f
		}
		images = images || image
		closed = image || over
		consume(cand)
	}
	ended := walked == len(cands) && !truncated
	if walked == len(cands) && truncated && len(x.rows) == 0 {
		return nil, sdk.Fail("JEV_LISTING_INCOMPLETE", fmt.Sprintf(listingIncomplete, a.files, 1024))
	}
	if len(batch) > 0 {
		questions := append(append([]byte{'{'}, frags[1:]...), '}')
		body := request(model, state, questions, assets)
		if len(body) > maxRequestBytes {
			return nil, invalidArg(fmt.Sprintf(requestTooLarge, len(body), maxRequestBytes))
		}
		tooLong := tooLongItems
		if a.long {
			tooLong = tooLongAlready
		}
		raw, err := post(c, h, cfg, body, headers(images, a.qs, a.long), tooLong, againItems)
		if err != nil {
			return nil, err
		}
		var answered string
		if answered, x.usage, x.extra, x.collided, err = replyModel(raw); err != nil {
			return nil, err
		}
		answers := object(object(raw)["answers"])
		if len(answers) != len(a.qs)*len(batch) {
			return nil, invalidReply("Answer IDs do not match the submitted questions")
		}
		for k, p := range batch {
			for j, q := range a.qs {
				ans := answers[fmt.Sprintf("q%d_%d", j, k)]
				if err := q.check(ans, q.id); err != nil {
					return nil, err
				}
				x.rows[p.row].answers = append(x.rows[p.row].answers, readAnswer(q, ans))
			}
		}
		x.model, x.sent, x.images = answered, true, images
	}
	if rank.q >= 0 {
		sort.SliceStable(x.rows, func(i, j int) bool {
			ri, rj := x.rows[i].refused == "", x.rows[j].refused == ""
			if ri != rj {
				return ri
			}
			return ri && rank.key(x.rows[i]) > rank.key(x.rows[j])
		})
	}
	if !ended {
		x.next = &cursor{after: last, model: x.model, screen: screen}
		if a.from != nil && a.from.model != "" {
			x.next.model = a.from.model
		}
	}
	return rawJSON(render(cfg.replyBytes, x.write)), nil
}
