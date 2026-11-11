package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
	"strings"
	"unicode/utf8"
)

const (
	maxRefs = 64

	headPage = 64 << 10
)

func reference(raw []byte) (path string, is bool, err error) {
	if !shape(raw, "{") {
		return "", false, nil
	}
	m := object(raw)
	v, has := m["$file"]
	if !has || len(m) != 1 {
		return "", false, nil
	}
	p, ok := text(v)
	if !ok || strings.TrimSpace(p) == "" {
		return "", true, invalidArg(`a reference is {"$file": "<a path the identity could read>"}`)
	}
	return p, true, nil
}

func refCount(raw []byte) int {
	if _, is, _ := reference(raw); is {
		return 1
	}
	raw = bytes.TrimSpace(raw)
	if !shape(raw, "{[") {
		return 0
	}
	n, ps := 0, parts(raw)
	for i := range ps {
		if raw[0] == '[' || i%2 == 1 {
			n += refCount(ps[i])
		}
	}
	return n
}

type tooLarge struct {
	*sdk.OperationError
	size int64
}

func (e *tooLarge) Unwrap() error { return e.OperationError }

func fileTooLargeErr(path string, size int64, room int) error {
	return &tooLarge{sdk.Fail("JEV_FILE_TOO_LARGE", fmt.Sprintf(fileTooLarge, path, size, room)), size}
}

func refusedFile(path string, err error) error {
	if d, ok := sdk.AsDenied(err); ok {
		return sdk.Deny(d.ReasonCode, path+": "+d.Message)
	}
	op, ok := sdk.AsOperationError(err)
	if !ok {
		return sdk.Fail("JEV_FILE_UNREADABLE", fmt.Sprintf(fileUnreadable, path, "a fault", err.Error()))
	}
	if op.Status == "denied" {
		return &sdk.OperationError{Status: "denied", ReasonCode: op.ReasonCode, Reason: path + ": " + op.Reason}
	}
	if strings.HasPrefix(op.ReasonCode, "FS_") {
		return sdk.Fail("JEV_FILE_UNREADABLE", fmt.Sprintf(fileUnreadable, path, op.ReasonCode, op.Reason))
	}
	return op
}

func aboutFile(err error) bool {
	op, ok := sdk.AsOperationError(err)
	if !ok {
		return false
	}
	switch op.ReasonCode {
	case "JEV_FILE_UNREADABLE", "JEV_FILE_NOT_TEXT", "JEV_FILE_TOO_LARGE", "JEV_IMAGE_OUT_OF_BOUNDS":
		return true
	}
	return strings.HasPrefix(op.ReasonCode, "FS_")
}

type file struct {
	data   []byte
	format string
	orient int
}

func digest(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:8])
}

type resolver struct {
	h      host
	refs   int
	room   int
	files  map[string]file
	base   map[string]file
	assets []byte
	sent   []sentFile
	turned []sentFile
	images int
}

type sentFile struct{ path, digest string }

func newResolver(h host, room int) *resolver {
	return &resolver{h: h, room: room, files: map[string]file{}}
}

func (r *resolver) resolve(raw rawJSON, place string) (rawJSON, error) {
	raw = bytes.TrimSpace(raw)
	if path, is, err := reference(raw); err != nil {
		return nil, err
	} else if is {
		return r.place(path, place)
	}
	if !shape(raw, "{[") || refCount(raw) == 0 {
		return raw, nil
	}
	ps := parts(raw)
	out := []byte{raw[0]}
	for i := range ps {
		if i > 0 {
			out = append(out, ",:"[boolInt(raw[0] == '{' && i%2 == 1)])
		}
		v := ps[i]
		if raw[0] == '[' || i%2 == 1 {
			key := fmt.Sprint(i)
			if raw[0] == '{' {
				k, _ := text(ps[i-1])
				key, v = strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1"), ps[i]
			}
			var err error
			if v, err = r.resolve(v, place+"/"+key); err != nil {
				return nil, err
			}
		}
		out = append(out, v...)
	}
	return append(out, raw[len(raw)-1]), nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (r *resolver) place(path, place string) (rawJSON, error) {
	if r.refs >= maxRefs {
		return nil, sdk.Fail("JEV_REFERENCES_TOO_MANY", fmt.Sprintf(referencesTooMany, path, maxRefs))
	}
	r.refs++
	f, err := r.load(path)
	if err != nil {
		return nil, err
	}
	if f.format == "" {
		v := quote(nil, string(f.data))
		if len(v) > r.room {
			return nil, fileTooLargeErr(path, int64(len(v)), r.room)
		}
		r.room -= len(v)
		r.note(path, f)
		return v, nil
	}

	h := sha256.New()
	h.Write(f.data)
	h.Write([]byte{0})
	h.Write([]byte(path))
	h.Write([]byte{0})
	h.Write([]byte(place))
	id := "img-" + hex.EncodeToString(h.Sum(nil))
	entry := quote([]byte{','}, id)
	entry = append(append(append(entry, `:{"type":"image","format":"`...), f.format...), `","data_base64":"`...)
	entry = append(base64.StdEncoding.AppendEncode(entry, f.data), `"}`...)
	v := quote(nil, id)
	if len(entry)+len(v) > r.room {
		return nil, fileTooLargeErr(path, int64(len(f.data)), r.room)
	}
	r.room -= len(entry) + len(v)
	r.assets = append(r.assets, entry...)
	r.images++
	r.note(path, f)
	return v, nil
}

func (r *resolver) note(path string, f file) {
	for _, s := range r.sent {
		if s.path == path {
			return
		}
	}
	r.sent = append(r.sent, sentFile{path, digest(f.data)})
	if f.orient > 1 {
		r.turned = append(r.turned, sentFile{path, fmt.Sprint(f.orient)})
	}
}

func (r *resolver) load(path string) (file, error) {
	if f, ok := r.files[path]; ok {
		return f, nil
	}
	if f, ok := r.base[path]; ok {
		return f, nil
	}
	data, eof, size, err := r.h.read(path, 0, min(max(r.room+1, 1), headPage))
	if err != nil {
		return file{}, refusedFile(path, err)
	}
	f := file{format: imageFormat(data), orient: 1}
	limit := r.room
	if f.format != "" {
		limit = r.room / 4 * 3
	}
	if size > int64(limit) {
		return file{}, fileTooLargeErr(path, size, r.room)
	}
	checked := false
	if f.format != "" {
		if w, h, o, ok := imageHeader(data); ok {
			if err := imageBounds(path, w, h); err != nil {
				return file{}, err
			}
			f.orient, checked = o, true
		}
	}
	for !eof {
		more, end, _, err := r.h.read(path, int64(len(data)), max(limit+1-len(data), 1))
		if err != nil {
			return file{}, refusedFile(path, err)
		}
		if len(more) == 0 && !end {
			return file{}, sdk.Fail("JEV_FILE_UNREADABLE", fmt.Sprintf(fileUnreadable, path, "FS_IO_FAILED", "a read returned nothing before the end"))
		}
		data, eof = append(data, more...), end
		if len(data) > limit {
			return file{}, fileTooLargeErr(path, int64(len(data)), r.room)
		}
	}
	switch {
	case f.format != "" && !checked:
		w, h, o, ok := imageHeader(data)
		if !ok {
			return file{}, sdk.Fail("JEV_IMAGE_OUT_OF_BOUNDS", fmt.Sprintf(imageUnreadable, path, f.format))
		}
		if err := imageBounds(path, w, h); err != nil {
			return file{}, err
		}
		f.orient = o
	case f.format == "" && (!utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0):
		return file{}, sdk.Fail("JEV_FILE_NOT_TEXT", fmt.Sprintf(fileNotText, path))
	}
	f.data = data
	r.files[path] = f
	return f, nil
}

const minSide, maxSide = 280, 1568

func imageBounds(path string, w, h int) error {
	if w < minSide || w > maxSide || h < minSide || h > maxSide {
		return sdk.Fail("JEV_IMAGE_OUT_OF_BOUNDS", fmt.Sprintf(imageOutOfBounds, path, w, h))
	}
	return nil
}

func imageFormat(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "png"
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		return "jpeg"
	}
	return ""
}

func imageHeader(b []byte) (w, h, orientation int, ok bool) {
	be16 := func(p []byte) int { return int(p[0])<<8 | int(p[1]) }
	if imageFormat(b) == "png" {
		if len(b) < 24 || string(b[12:16]) != "IHDR" {
			return 0, 0, 0, false
		}
		be32 := func(p []byte) int { return int(p[0])<<24 | int(p[1])<<16 | int(p[2])<<8 | int(p[3]) }
		return be32(b[16:]), be32(b[20:]), 1, true
	}
	orientation = 1
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xFF {
			return 0, 0, 0, false
		}
		m := b[i+1]
		if m == 0xFF {
			i++
			continue
		}
		if m == 0x01 || m >= 0xD0 && m <= 0xD7 {
			i += 2
			continue
		}
		n := be16(b[i+2:])
		seg := b[i+4:]
		if n < 2 {
			return 0, 0, 0, false
		}
		switch {
		case m >= 0xC0 && m <= 0xCF && m != 0xC4 && m != 0xC8 && m != 0xCC:
			if len(seg) < 5 {
				return 0, 0, 0, false
			}
			return be16(seg[3:]), be16(seg[1:]), orientation, true
		case m == 0xE1 && len(seg) >= n-2:
			if o, exif := exifOrientation(seg[:n-2]); exif {
				orientation = o
			}
		case m == 0xDA || m == 0xD9:
			return 0, 0, 0, false
		}
		i += 2 + n
	}
	return 0, 0, 0, false
}

func exifOrientation(seg []byte) (o int, exif bool) {
	if len(seg) < 14 || string(seg[:6]) != "Exif\x00\x00" {
		return 1, false
	}
	t := seg[6:]
	var u16 func([]byte) int
	var u32 func([]byte) int
	switch string(t[:2]) {
	case "II":
		u16 = func(p []byte) int { return int(p[0]) | int(p[1])<<8 }
		u32 = func(p []byte) int { return int(p[0]) | int(p[1])<<8 | int(p[2])<<16 | int(p[3])<<24 }
	case "MM":
		u16 = func(p []byte) int { return int(p[0])<<8 | int(p[1]) }
		u32 = func(p []byte) int { return int(p[0])<<24 | int(p[1])<<16 | int(p[2])<<8 | int(p[3]) }
	default:
		return 1, true
	}

	ifd := u32(t[4:])
	if ifd < 8 || ifd > len(t)-2 {
		return 1, true
	}
	for i, n := 0, u16(t[ifd:]); i < n; i++ {
		e := ifd + 2 + 12*i
		if e+12 > len(t) {
			return 1, true
		}
		if u16(t[e:]) == 0x0112 {
			if o := u16(t[e+8:]); o >= 1 && o <= 8 {
				return o, true
			}
			return 1, true
		}
	}
	return 1, true
}
