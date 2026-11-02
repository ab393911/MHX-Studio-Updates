package automation

import (
	"context"
	"errors"
	"image"
	"image/color"
	"testing"
	"time"
)

func TestScoreBothFeaturesWithoutRelaxingEitherThreshold(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 960, 540))
	a, _ := CaptureAnchor(img, Rect{10, 10, 32, 16})
	b, _ := CaptureAnchor(img, Rect{100, 10, 32, 16})
	p := NewProfile()
	p.Pages["game"] = Page{[]Anchor{a, b}, 10}
	for y := 10; y < 26; y++ {
		for x := 10; x < 42; x++ {
			img.Set(x, y, color.Gray{11})
		}
	}
	s := p.Score(img, "game")
	if s.Passed || len(s.Anchors) != 2 || s.Anchors[0].MeanAbsoluteError != 11 || s.Anchors[1].MeanAbsoluteError != 0 || s.Anchors[0].MeanLimit != 10 || s.Anchors[0].ChangedPixels != 0 {
		t.Fatal(s)
	}
	if p.Match(img, "game") {
		t.Fatal("mean limit relaxed")
	}
}
func TestChangedPixelGateCannotBeHiddenByLowMean(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 960, 540))
	r := Rect{10, 10, 32, 16}
	a, _ := CaptureAnchor(img, r)
	b, _ := CaptureAnchor(img, Rect{100, 10, 32, 16})
	p := NewProfile()
	p.Pages["game"] = Page{[]Anchor{a, b}, 10}
	for i := 0; i < 41; i++ {
		img.Set(r.X+i%32, r.Y+i/32, color.Gray{50})
	}
	s := p.Score(img, "game")
	if s.Passed || s.Anchors[0].MeanAbsoluteError >= 10 || s.Anchors[0].ChangedPixels != 41 || s.Anchors[0].ChangedPixelLimit != 40 {
		t.Fatal(s)
	}
}
func TestAnchorPixelCoordinatesAndOriginalImageDimensions(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 960, 540))
	for y := 0; y < 540; y++ {
		for x := 0; x < 960; x++ {
			v := uint8((x + y) % 256)
			img.SetRGBA(x, y, color.RGBA{v, v, v, 255})
		}
	}
	r := Rect{700, 300, 64, 32}
	a, e := CaptureAnchor(img, r)
	if e != nil {
		t.Fatal(e)
	}
	if a.Pixels[0] != uint8((r.X+r.Y)%256) || a.Pixels[511] != uint8((r.X+r.W-1+r.Y+r.H-1)%256) {
		t.Fatal("sampling shifted")
	}
	p := NewProfile()
	p.Pages["game"] = Page{[]Anchor{a, a}, 10}
	if !p.Match(img, "game") || p.Match(image.NewRGBA(image.Rect(0, 0, 1920, 1080)), "game") || p.Match(image.NewRGBA(image.Rect(1, 1, 961, 541)), "game") {
		t.Fatal("invalid frame dimensions accepted")
	}
}
func TestUnknownFrameStillCannotTriggerLoginClick(t *testing.T) {
	s := simulator(t)
	d := &uncertainTap{visualSimulator: s, err: errors.New("must not click")}
	s.images["unknown"] = s.images["notice"]
	// Make all template comparisons reject the current frame without changing state machine.
	p := NewProfile()
	for key, page := range s.profile.Pages {
		for j := range page.Anchors {
			page.Anchors[j].Pixels = make([]byte, 512)
		}
		p.Pages[key] = page
	}
	p.Buttons = s.profile.Buttons
	e := Engine{Device: d, Profile: p, Options: Options{Retries: 0, MinDelayMS: 100, MaxDelayMS: 100, StateTimeout: time.Millisecond}}
	if _, err := e.EnterRole(context.Background(), 1, EnterOptions{SkipNicknameCheck: true}); err == nil || d.calls != 0 {
		t.Fatal(err, d.calls)
	}
}

func TestDiagnosticOffsetNeverLetsShiftedUnknownFramePass(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 960, 540))
	r1, r2 := Rect{100, 100, 32, 16}, Rect{300, 100, 32, 16}
	for _, r := range []Rect{r1, r2} {
		for y := 0; y < r.H; y++ {
			for x := 0; x < r.W; x++ {
				v := uint8(20 + ((x*73 + y*41) % 230))
				img.SetRGBA(r.X+x, r.Y+y, color.RGBA{v, v, v, 255})
			}
		}
	}
	a, _ := CaptureAnchor(img, r1)
	b, _ := CaptureAnchor(img, r2)
	p := NewProfile()
	p.Pages["game"] = Page{[]Anchor{a, b}, 10}
	shifted := image.NewRGBA(image.Rect(0, 0, 960, 540))
	for y := 0; y < 540; y++ {
		for x := 1; x < 960; x++ {
			shifted.Set(x, y, img.At(x-1, y))
		}
	}
	s := p.Explain(shifted, "game")
	if s.Passed || p.Match(shifted, "game") {
		t.Fatal("diagnostic offset relaxed recognition")
	}
	for _, score := range s.Anchors {
		if score.BestOffsetDiagnostic == nil || score.BestOffsetDiagnostic.DX != 1 || score.BestOffsetDiagnostic.DY != 0 || score.BestOffsetDiagnostic.MeanAbsoluteError != 0 || len(score.CurrentPixels) != 512 || len(score.ReferencePixels) != 512 {
			t.Fatal(score)
		}
	}
}
