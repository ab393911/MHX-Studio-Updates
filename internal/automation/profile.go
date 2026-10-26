package automation

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"time"
)

type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

func (r Rect) Valid() bool {
	return r.W >= 6 && r.H >= 6 && r.X >= 0 && r.Y >= 0 && r.X+r.W <= 960 && r.Y+r.H <= 540
}

type Anchor struct {
	Rect   Rect   `json:"rect"`
	Pixels []byte `json:"pixels"`
}
type Page struct {
	Anchors   []Anchor `json:"anchors"`
	Threshold float64  `json:"threshold"`
}
type Profile struct {
	Schema  int             `json:"schema"`
	Pages   map[string]Page `json:"pages"`
	Buttons map[string]Rect `json:"buttons"`
}

func NewProfile() Profile {
	return Profile{Schema: 1, Pages: map[string]Page{}, Buttons: map[string]Rect{}}
}
func Decode(b []byte) (image.Image, error) {
	c, e := png.DecodeConfig(bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	if c.Width != 960 || c.Height != 540 {
		return nil, fmt.Errorf("screen is %dx%d; this profile requires landscape 960x540", c.Width, c.Height)
	}
	return png.Decode(bytes.NewReader(b))
}
func sample(img image.Image, r Rect) []byte {
	p := make([]byte, 0, 32*16)
	for y := 0; y < 16; y++ {
		for x := 0; x < 32; x++ {
			rr, g, b, _ := img.At(r.X+x*(r.W-1)/31, r.Y+y*(r.H-1)/15).RGBA()
			p = append(p, byte((299*rr+587*g+114*b)/1000>>8))
		}
	}
	return p
}
func CaptureAnchor(img image.Image, r Rect) (Anchor, error) {
	if !r.Valid() {
		return Anchor{}, errors.New("anchor must be inside the 960x540 image and at least 6x6")
	}
	return Anchor{r, sample(img, r)}, nil
}
func (p Profile) Validate() error {
	if p.Schema != 1 || p.Pages == nil || p.Buttons == nil {
		return errors.New("invalid recognition profile")
	}
	for name, page := range p.Pages {
		if len(page.Anchors) != 2 || page.Threshold < 1 || page.Threshold > 20 {
			return fmt.Errorf("%s needs two anchors and threshold 1..20", name)
		}
		for _, a := range page.Anchors {
			if !a.Rect.Valid() || len(a.Pixels) != 512 {
				return errors.New("invalid image anchor")
			}
		}
	}
	for _, r := range p.Buttons {
		if !r.Valid() {
			return errors.New("invalid button bounds")
		}
	}
	return nil
}
func (p Profile) Match(img image.Image, name string) bool {
	page, ok := p.Pages[name]
	if !ok || len(page.Anchors) != 2 {
		return false
	}
	for _, a := range page.Anchors {
		if !a.Rect.Valid() || len(a.Pixels) != 512 {
			return false
		}
		now := sample(img, a.Rect)
		sum, bad := 0, 0
		for i, v := range now {
			d := int(v) - int(a.Pixels[i])
			if d < 0 {
				d = -d
			}
			sum += d
			if d > 40 {
				bad++
			}
		}
		if float64(sum)/512 > page.Threshold || bad > 40 {
			return false
		}
	}
	return true
}

// Require rejects competing character identities, even when a generic page matches.
func (p Profile) Require(img image.Image, name string) error {
	if !p.Match(img, name) {
		return fmt.Errorf("state %s not verified", name)
	}
	var prefix string
	if len(name) > 5 && name[:5] == "game_" {
		prefix = "game_"
	}
	if len(name) > 9 && name[:9] == "selected_" {
		prefix = "selected_"
	}
	if prefix != "" {
		for i := 1; i <= 5; i++ {
			other := fmt.Sprintf("%s%d", prefix, i)
			if other != name && p.Match(img, other) {
				return errors.New("ambiguous character identity; recalibrate name/highlight anchors")
			}
		}
	}
	return nil
}
func bounded(n int) int {
	if n <= 1 {
		return 0
	}
	var b [8]byte
	if _, e := rand.Read(b[:]); e != nil {
		return n / 2
	}
	return int(binary.LittleEndian.Uint64(b[:]) % uint64(n))
}
func Point(r Rect) (int, int, error) {
	if !r.Valid() {
		return 0, 0, errors.New("button bounds not calibrated")
	}
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	rx := int(math.Min(8, float64((r.W-2)/4)))
	ry := int(math.Min(6, float64((r.H-2)/4)))
	return cx + bounded(rx*2+1) - rx, cy + bounded(ry*2+1) - ry, nil
}
func Delay(min, max int) time.Duration {
	return time.Duration(min+bounded(max-min+1)) * time.Millisecond
}
