package main

import (
	"encoding/binary"
	"testing"
)

func exifSeg(order binary.ByteOrder, ifd uint32, body int) []byte {
	t := make([]byte, body)
	if order == binary.LittleEndian {
		copy(t, "II")
	} else {
		copy(t, "MM")
	}
	order.PutUint16(t[2:], 0x2a)
	order.PutUint32(t[4:], ifd)
	if body >= 8+2+12 {
		order.PutUint16(t[8:], 1)
		order.PutUint16(t[10:], 0x0112)
		order.PutUint16(t[12:], 3)
		order.PutUint32(t[14:], 1)
		order.PutUint16(t[18:], 6)
	}
	return append([]byte("Exif\x00\x00"), t...)
}

func TestAnEXIFOffsetPastItsSegmentIsNoOrientation(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		const body = 64
		for _, tc := range []struct {
			ifd    uint32
			want   int
			reason string
		}{
			{8, 6, "the ordinary IFD at 8"},
			{body - 2, 1, "the last offset whose entry count fits: zero entries read, no orientation"},
			{body - 1, 1, "one byte short of an entry count"},
			{body, 1, "the segment's end"},
			{0x7ffffffd, 1, "int32's last value that does not wrap when 2 is added"},
			{0x7ffffffe, 1, "wraps to int32's minimum when 2 is added"},
			{0x7fffffff, 1, "int32's maximum"},
			{0x80000000, 1, "negative as int32"},
			{0xffffffff, 1, "uint32's maximum"},
			{7, 1, "inside the TIFF header"},
		} {
			seg := exifSeg(order, tc.ifd, body)
			if tc.ifd == body-2 {
				order.PutUint16(seg[6+body-2:], 0)
			}
			o, exif := exifOrientation(seg)
			if o != tc.want || !exif {
				t.Errorf("%v offset %#x (%s): orientation %d, exif %v; want %d, true", order, tc.ifd, tc.reason, o, exif, tc.want)
			}
		}

		if o, exif := exifOrientation(exifSeg(order, 0x7fffffff, 8)); o != 1 || !exif {
			t.Errorf("%v truncated: orientation %d, exif %v", order, o, exif)
		}
	}

	seg := exifSeg(binary.BigEndian, 0x7ffffffe, 16)
	b := []byte{0xFF, 0xD8, 0xFF, 0xE1, byte((len(seg) + 2) >> 8), byte(len(seg) + 2)}
	b = append(b, seg...)
	b = append(b, 0xFF, 0xC0, 0, 17, 8, 0x02, 0x58, 0x03, 0x20, 3, 1, 0x22, 0, 2, 0x11, 1, 3, 0x11, 1, 0xFF, 0xD9)
	if w, h, o, ok := imageHeader(b); !ok || w != 800 || h != 600 || o != 1 {
		t.Fatalf("imageHeader: %d x %d, orientation %d, ok %v; want 800 x 600, 1, true", w, h, o, ok)
	}
}
