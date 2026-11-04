package fixtureprivacy

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func syntheticPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 12, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 12; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(40 + x*9), uint8(20 + y*13), uint8(30 + x + y), 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, rgbaPNG{img}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestRedactPreservesPixelsFormatAndInput(t *testing.T) {
	source := syntheticPNG(t)
	before := append([]byte(nil), source...)
	out, audit, err := Redact(source, []image.Rectangle{image.Rect(0, 0, 3, 4)}, []image.Rectangle{image.Rect(8, 0, 12, 8)})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, source) || !bytes.Equal(source[16:29], out[16:29]) {
		t.Fatal("input or PNG format changed")
	}
	if audit.MaskedPixels != 12 || !audit.ProtectedPixelsUnchanged || !audit.OutsideMasksUnchanged || !audit.Opaque {
		t.Fatal(audit)
	}
	a, _ := png.Decode(bytes.NewReader(source))
	b, _ := png.Decode(bytes.NewReader(out))
	for y := 0; y < 8; y++ {
		for x := 0; x < 12; x++ {
			got := color.NRGBAModel.Convert(b.At(x, y)).(color.NRGBA)
			if x < 3 && y < 4 {
				if got != (color.NRGBA{A: 255}) {
					t.Fatal("mask not opaque")
				}
			} else if got != color.NRGBAModel.Convert(a.At(x, y)).(color.NRGBA) {
				t.Fatal("unmasked content changed")
			}
		}
	}
}

func TestRedactRejectsInvalidOrOverlappingRegions(t *testing.T) {
	for _, r := range []image.Rectangle{image.Rect(10, 0, 12, 2), image.Rect(-1, 0, 2, 2), image.Rect(0, 0, 13, 8), {}} {
		if _, _, err := Redact(syntheticPNG(t), []image.Rectangle{r}, []image.Rectangle{image.Rect(8, 0, 12, 8)}); err == nil {
			t.Fatalf("unsafe region accepted: %v", r)
		}
	}
}

func TestRedactDropsTextAndRetainsDisplayMetadata(t *testing.T) {
	source := syntheticPNG(t)
	chunk := func(kind string, payload []byte) []byte {
		var b bytes.Buffer
		_ = binary.Write(&b, binary.BigEndian, uint32(len(payload)))
		b.WriteString(kind)
		b.Write(payload)
		_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(append([]byte(kind), payload...)))
		return b.Bytes()
	}
	input := append([]byte(nil), source[:33]...)
	input = append(input, chunk("sRGB", []byte{0})...)
	input = append(input, chunk("tEXt", []byte("Owner\x00synthetic-private-name"))...)
	input = append(input, source[33:]...)
	out, _, err := Redact(input, []image.Rectangle{image.Rect(0, 0, 3, 4)}, []image.Rectangle{image.Rect(8, 0, 12, 8)})
	if err != nil {
		t.Fatal(err)
	}
	bad := append([]byte(nil), source[:33]...)
	bad = append(bad, chunk("sRGB", []byte("not-a-numeric-profile"))...)
	bad = append(bad, source[33:]...)
	if _, _, err := Redact(bad, []image.Rectangle{image.Rect(0, 0, 3, 4)}, []image.Rectangle{image.Rect(8, 0, 12, 8)}); err == nil {
		t.Fatal("malformed display chunk accepted")
	}
	if bytes.Contains(out, []byte("tEXt")) || bytes.Contains(out, []byte("synthetic-private-name")) || !bytes.Contains(out, chunk("sRGB", []byte{0})) {
		t.Fatal("metadata privacy/display contract violated")
	}
}
