package automation

import (
	"image"
	"image/color"
	"testing"
)

func iconFixture(t *testing.T) (IconRules, *image.RGBA) {
	t.Helper()
	r := NewIconRules()
	img := image.NewRGBA(image.Rect(0, 0, 960, 540))
	for i, id := range []string{"ui_bag", "ui_menu", "ui_team"} {
		x := 40 + i*200
		for y := 100; y < 130; y++ {
			for xx := x; xx < x+40; xx++ {
				v := uint8(35 + (xx*17+y*23+i*41)%190)
				img.SetRGBA(xx, y, color.RGBA{v, v, v, 255})
			}
		}
		var err error
		r, err = r.Calibrate(img, id, id, id+"_click", []Rect{{x, 100, 10, 8}, {x + 14, 112, 10, 8}}, Rect{x, 100, 40, 30})
		if err != nil {
			t.Fatal(err)
		}
	}
	r.MainIcons = []string{"ui_bag", "ui_menu", "ui_team"}
	return r, img
}
func iconFrames(a, b image.Image) []IconFrame {
	return []IconFrame{{"instance-1", "capture-1", a}, {"instance-1", "capture-2", b}}
}
func copyIconImage(img image.Image) *image.RGBA {
	out := image.NewRGBA(img.Bounds())
	for y := 0; y < 540; y++ {
		for x := 0; x < 960; x++ {
			out.Set(x, y, img.At(x, y))
		}
	}
	return out
}
func eraseIcon(img *image.RGBA, r IconRules, id string) {
	for _, a := range r.Profile.Pages[id].Anchors {
		for y := a.Rect.Y; y < a.Rect.Y+a.Rect.H; y++ {
			for x := a.Rect.X; x < a.Rect.X+a.Rect.W; x++ {
				img.SetRGBA(x, y, color.RGBA{A: 255})
			}
		}
	}
}

func TestIconQuorumStableGroupsAndNoAction(t *testing.T) {
	r, img := iconFixture(t)
	next := copyIconImage(img)
	eraseIcon(next, r, "ui_bag")
	result := r.Assess(iconFrames(img, next))
	if !result.CandidateMain || result.ActionAllowed || len(result.StableIcons) != 2 {
		t.Fatal(result)
	}
	eraseIcon(next, r, "ui_menu")
	if r.Assess(iconFrames(img, next)).CandidateMain {
		t.Fatal("single icon accepted")
	}
	a, b := copyIconImage(img), copyIconImage(img)
	eraseIcon(a, r, "ui_bag")
	eraseIcon(b, r, "ui_menu")
	if r.Assess(iconFrames(a, b)).CandidateMain {
		t.Fatal("alternating icons counted as stable")
	}
}
func TestIconChangingBackgroundAndDotOutsideCore(t *testing.T) {
	r, img := iconFixture(t)
	next := copyIconImage(img)
	for y := 0; y < 540; y++ {
		for x := 0; x < 960; x++ {
			keep := false
			for _, p := range r.Profile.Pages {
				for _, a := range p.Anchors {
					if inside(a.Rect, x, y) {
						keep = true
					}
				}
			}
			if !keep {
				next.SetRGBA(x, y, color.RGBA{uint8(x % 255), uint8(y % 255), 80, 255})
			}
		}
	}
	if !r.Assess(iconFrames(img, next)).CandidateMain {
		t.Fatal("background-only change rejected")
	}
}
func TestIconIdentityDuplicateFramesUnknownAndBlocker(t *testing.T) {
	r, img := iconFixture(t)
	f := iconFrames(img, img)
	f[1].Source = "instance-2"
	if r.Assess(f).CandidateMain {
		t.Fatal("mixed instance accepted")
	}
	f = iconFrames(img, img)
	f[1].CaptureID = f[0].CaptureID
	if r.Assess(f).CandidateMain || r.Assess(f[:1]).CandidateMain {
		t.Fatal("duplicate/single capture accepted")
	}
	blank := image.NewRGBA(img.Bounds())
	if r.Assess(iconFrames(blank, blank)).CandidateMain {
		t.Fatal("unknown accepted")
	}
	r.Profile.Pages["blocked_dialog"] = r.Profile.Pages["ui_bag"]
	r.Blockers = []string{"blocked_dialog"}
	if r.Assess(iconFrames(img, img)).CandidateMain {
		t.Fatal("visible icons bypassed blocker")
	}
}
func TestIconCalibrationQualityBoundsAndThreshold(t *testing.T) {
	r, img := iconFixture(t)
	bad := r
	page := bad.Profile.Pages["ui_bag"]
	page.Threshold = 11
	bad.Profile.Pages["ui_bag"] = page
	if bad.Validate() == nil {
		t.Fatal("threshold increase accepted")
	}
	r, img = iconFixture(t)
	if _, err := r.Calibrate(nil, "x", "x", "x", nil, Rect{}); err == nil {
		t.Fatal("nil image accepted")
	}
	if _, err := r.Calibrate(image.NewRGBA(img.Bounds()), "x", "x", "x", []Rect{{10, 10, 8, 8}, {20, 10, 8, 8}}, Rect{10, 10, 30, 30}); err == nil {
		t.Fatal("flat patches accepted")
	}
	r.MainIcons = []string{"ui_bag", "ui_menu"}
	if r.Validate() == nil {
		t.Fatal("less than three candidate groups accepted")
	}
}
