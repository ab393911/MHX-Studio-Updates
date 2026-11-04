// Package fixtureprivacy prepares local previews for explicit review before a
// screenshot is used in public CI. It does not change application rules.
package fixtureprivacy

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

type Audit struct {
	Width, Height              int
	Format                     string
	ProtectedRegions           int
	MaskedPixels               int
	OutsideMasksUnchanged      bool
	ProtectedPixelsUnchanged   bool
	Opaque                     bool
	RulePagesChecked           int
	RecognitionScoresUnchanged bool
	OutputSHA256               string
}

// Force the encoder to retain RGBA8 even when all alpha values are opaque.
// The standard encoder otherwise changes opaque NRGBA input to RGB8.
type rgbaPNG struct{ *image.NRGBA }

func (rgbaPNG) Opaque() bool { return false }

func Redact(raw []byte, masks, protected []image.Rectangle) ([]byte, Audit, error) {
	var audit Audit
	if len(raw) < 33 || !bytes.Equal(raw[:8], []byte("\x89PNG\r\n\x1a\n")) || raw[24] != 8 || raw[25] != 6 || raw[28] != 0 {
		return nil, audit, fmt.Errorf("requires noninterlaced RGBA8 PNG; no format conversion allowed")
	}
	config, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, audit, err
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 {
		return nil, audit, fmt.Errorf("unsupported image bounds")
	}
	bounds := image.Rect(0, 0, config.Width, config.Height)
	if len(masks) == 0 || len(protected) == 0 {
		return nil, audit, fmt.Errorf("masks and protected regions are required")
	}
	for _, group := range [][]image.Rectangle{masks, protected} {
		for _, r := range group {
			if r.Empty() || !r.In(bounds) {
				return nil, audit, fmt.Errorf("invalid region %v", r)
			}
		}
	}
	for _, mask := range masks {
		for _, p := range protected {
			if mask.Overlaps(p) {
				return nil, audit, fmt.Errorf("mask %v intersects protected region %v", mask, p)
			}
		}
	}
	source, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, audit, err
	}
	dest := image.NewNRGBA(bounds)
	draw.Draw(dest, bounds, source, image.Point{}, draw.Src)
	black := image.NewUniform(color.NRGBA{A: 255})
	for _, mask := range masks {
		draw.Draw(dest, mask, black, image.Point{}, draw.Src)
	}
	var encoded bytes.Buffer
	if err = png.Encode(&encoded, rgbaPNG{dest}); err != nil {
		return nil, audit, err
	}
	// Retain numeric display metadata, never text/EXIF or arbitrary profiles.
	var safeChunks []byte
	for off := 8; off+12 <= len(raw); {
		n := int(binary.BigEndian.Uint32(raw[off : off+4]))
		if n > len(raw)-off-12 {
			return nil, audit, fmt.Errorf("invalid PNG chunk length")
		}
		kind := string(raw[off+4 : off+8])
		switch kind {
		case "sRGB", "sBIT", "gAMA", "cHRM":
			want := map[string]int{"sRGB": 1, "sBIT": 4, "gAMA": 4, "cHRM": 32}[kind]
			if n != want {
				return nil, audit, fmt.Errorf("invalid numeric display metadata %s", kind)
			}
			if kind == "sRGB" && raw[off+8] > 3 {
				return nil, audit, fmt.Errorf("invalid sRGB rendering intent")
			}
			if kind == "sBIT" {
				for _, v := range raw[off+8 : off+8+n] {
					if v < 1 || v > 8 {
						return nil, audit, fmt.Errorf("invalid significant-bit metadata")
					}
				}
			}
			safeChunks = append(safeChunks, raw[off:off+n+12]...)
		case "iCCP":
			return nil, audit, fmt.Errorf("embedded color profile requires separate privacy review")
		}
		off += n + 12
	}
	output := make([]byte, 0, encoded.Len()+len(safeChunks))
	output = append(output, encoded.Bytes()[:33]...)
	output = append(output, safeChunks...)
	output = append(output, encoded.Bytes()[33:]...)
	if !bytes.Equal(output[16:29], raw[16:29]) {
		return nil, audit, fmt.Errorf("PNG dimensions/format changed")
	}
	roundtrip, err := png.Decode(bytes.NewReader(output))
	if err != nil {
		return nil, audit, err
	}
	for y := 0; y < config.Height; y++ {
		for x := 0; x < config.Width; x++ {
			point := image.Pt(x, y)
			masked := false
			for _, r := range masks {
				if point.In(r) {
					masked = true
					break
				}
			}
			a := color.NRGBAModel.Convert(source.At(x, y)).(color.NRGBA)
			b := color.NRGBAModel.Convert(roundtrip.At(x, y)).(color.NRGBA)
			if a.A != 255 || b.A != 255 {
				return nil, audit, fmt.Errorf("requires opaque screenshot pixels")
			}
			if masked {
				audit.MaskedPixels++
				if b != (color.NRGBA{A: 255}) {
					return nil, audit, fmt.Errorf("redaction did not fully cover pixel")
				}
			} else if a != b {
				return nil, audit, fmt.Errorf("unmasked pixel changed at %v", point)
			}
		}
	}
	audit.Width, audit.Height = config.Width, config.Height
	audit.Format, audit.ProtectedRegions = "PNG RGBA8 noninterlaced", len(protected)
	audit.OutsideMasksUnchanged, audit.ProtectedPixelsUnchanged, audit.Opaque = true, true, true
	audit.OutputSHA256 = fmt.Sprintf("%x", sha256.Sum256(output))
	return output, audit, nil
}
