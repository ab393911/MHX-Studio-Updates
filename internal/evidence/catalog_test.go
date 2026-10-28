package evidence

import (
	"bytes"
	"context"
	"errors"
	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"
)

func canvas(p automation.Page) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 960, 540))
	for _, a := range p.Anchors {
		for y := 0; y < 16; y++ {
			for x := 0; x < 32; x++ {
				v := a.Pixels[y*32+x]
				img.SetRGBA(a.Rect.X+x*(a.Rect.W-1)/31, a.Rect.Y+y*(a.Rect.H-1)/15, color.RGBA{v, v, v, 255})
			}
		}
	}
	return img
}
func encoded(img image.Image) []byte { var b bytes.Buffer; _ = png.Encode(&b, img); return b.Bytes() }
func TestCatalogValidAndLocalCalibrationWins(t *testing.T) {
	c, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	if len(c.Pages) != 22 || len(c.Icons) != 4 {
		t.Fatal(c)
	}
	local := automation.NewProfile()
	local.Buttons["enter"] = automation.Rect{X: 100, Y: 100, W: 20, H: 20}
	local.Pages["game_3"] = c.Pages[0].Page
	p := c.Profile(local)
	if p.Buttons["enter"].X != 100 || p.Pages["game_3"].Anchors[0].Rect != c.Pages[0].Page.Anchors[0].Rect {
		t.Fatal("local profile overwritten")
	}
	if len(local.Pages) != 1 {
		t.Fatal("local profile mutated")
	}
}

type adDevice struct {
	ad, clear      []byte
	taps, captures int
	stick          bool
}

func (d *adDevice) Capture(ctx context.Context) ([]byte, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	d.captures++
	if d.taps == 0 || d.stick {
		return d.ad, nil
	}
	return d.clear, nil
}
func (d *adDevice) Tap(ctx context.Context, x, y int) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if x < 742 || x >= 758 || y < 56 || y >= 72 {
		return errors.New("tap outside evidenced close button")
	}
	d.taps++
	return nil
}
func TestAdUsesFrameNotArtworkAndRequiresDisappearance(t *testing.T) {
	c, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	var p automation.Page
	for _, v := range c.Pages {
		if v.Key == "advert" {
			p = v.Page
		}
	}
	img := canvas(p)
	for y := 140; y < 400; y++ {
		for x := 280; x < 650; x++ {
			img.SetRGBA(x, y, color.RGBA{200, 50, 100, 255})
		}
	}
	if !c.Match(img, "advert") {
		t.Fatal("changing artwork broke frame detection")
	}
	fake := &adDevice{ad: encoded(img), clear: encoded(image.NewRGBA(image.Rect(0, 0, 960, 540)))}
	d := LoginDevice{Device: fake, Catalog: c, Retries: 0, MinDelayMS: 100, MaxDelayMS: 100}
	if _, e = d.Capture(context.Background()); e != nil || fake.taps != 1 || fake.captures < 4 {
		t.Fatal(e, fake)
	}
	fake.taps = 0
	fake.stick = true
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, e = d.Capture(ctx); e == nil {
		t.Fatal("click acknowledgement counted as ad closed")
	}
}
func TestSolidColorCannotVerifyProfession(t *testing.T) {
	c, _ := Load()
	img := image.NewRGBA(image.Rect(0, 0, 30, 30))
	for y := 0; y < 30; y++ {
		for x := 0; x < 30; x++ {
			img.SetRGBA(x, y, color.RGBA{230, 80, 120, 255})
		}
	}
	if _, _, e := c.Profession(img, img.Bounds()); e == nil {
		t.Fatal("color alone accepted as profession")
	}
}
