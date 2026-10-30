package vision

import (
	_ "embed"
	"encoding/json"
	"errors"
	"image"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
)

type Pixel struct {
	X, Y    int
	R, G, B uint8
}
type Icon struct {
	Width, Height, SourceID int
	SourceSHA               string
	Pixels                  []Pixel
}

//go:embed invitation.json
var invitationJSON []byte

func InvitationIcon() Icon { var i Icon; _ = json.Unmarshal(invitationJSON, &i); return i }

// Find samples only the invariant bell and caption foreground. Background and
// the dynamic notification counter are deliberately absent from the template.
func (i Icon) Find(img image.Image) (automation.Rect, error) {
	if img == nil || img.Bounds() != image.Rect(0, 0, 960, 540) || i.Width < 6 || i.Height < 6 || i.Width > 100 || i.Height > 100 || len(i.Pixels) < 40 {
		return automation.Rect{}, errors.New("邀请图标识别模板无效")
	}
	for _, p := range i.Pixels {
		if p.X < 0 || p.Y < 0 || p.X >= i.Width || p.Y >= i.Height {
			return automation.Rect{}, errors.New("图标特征越界")
		}
	}
	type hit struct{ x, y, score int }
	var found []hit
	at := func(x, y int) (uint8, uint8, uint8) {
		r, g, b, _ := img.At(x, y).RGBA()
		return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
	}
	switch v := img.(type) {
	case *image.NRGBA:
		at = func(x, y int) (uint8, uint8, uint8) { n := v.PixOffset(x, y); return v.Pix[n], v.Pix[n+1], v.Pix[n+2] }
	case *image.RGBA:
		at = func(x, y int) (uint8, uint8, uint8) { n := v.PixOffset(x, y); return v.Pix[n], v.Pix[n+1], v.Pix[n+2] }
	}
	diff := func(x, y int, coarse bool) (int, bool) {
		sum, bad := 0, 0
		n := len(i.Pixels)
		if coarse {
			n = min(16, n)
		}
		for j := 0; j < n; j++ {
			p := i.Pixels[j]
			r, g, b := at(x+p.X, y+p.Y)
			delta := abs(int(r)-int(p.R)) + abs(int(g)-int(p.G)) + abs(int(b)-int(p.B))
			sum += delta
			if delta > 120 {
				bad++
			}
			if bad > max(1, n/10) {
				return 0, false
			}
		}
		return sum / n, sum/n <= 54
	}
	for y := 0; y <= 540-i.Height; y++ {
		for x := 0; x <= 960-i.Width; x++ {
			if _, ok := diff(x, y, true); !ok {
				continue
			}
			score, ok := diff(x, y, false)
			if !ok {
				continue
			}
			cluster := -1
			for j, h := range found {
				if abs(x-h.x) < i.Width/2 && abs(y-h.y) < i.Height/2 {
					cluster = j
					break
				}
			}
			if cluster < 0 {
				found = append(found, hit{x, y, score})
			} else if score < found[cluster].score {
				found[cluster] = hit{x, y, score}
			}
		}
	}
	if len(found) != 1 {
		return automation.Rect{}, errors.New("队伍邀请图标缺失或不唯一")
	}
	h := found[0]
	for y := max(0, h.y-1); y <= min(540-i.Height, h.y+1); y++ {
		for x := max(0, h.x-1); x <= min(960-i.Width, h.x+1); x++ {
			score, ok := diff(x, y, false)
			if ok && score < h.score {
				h = hit{x, y, score}
			}
		}
	}
	return automation.Rect{X: h.x, Y: h.y, W: i.Width, H: i.Height}, nil
}
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
