package automation

import (
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

// These are test-only crops of the approved redacted PNG, not shipped rules or
// evidence of camera-motion stability. No private profile is used here.
func redactedIconFixture(t *testing.T) (IconRules, image.Image) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "issue2-redacted", "screenshot.png"))
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != "e7e2e9436a79f2535e3955a3961e0317e5308672d9d8536ecab724aabc16444e" {
		t.Fatal("unapproved fixture")
	}
	img, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	r := NewIconRules()
	for _, c := range []struct {
		id       string
		features []Rect
		click    Rect
	}{
		{"ui_bag", []Rect{{912, 363, 10, 12}, {925, 375, 10, 12}}, Rect{904, 352, 44, 50}},
		{"ui_menu", []Rect{{23, 403, 12, 6}, {28, 412, 12, 6}}, Rect{19, 394, 30, 29}},
		{"ui_realm", []Rect{{910, 420, 10, 12}, {923, 431, 10, 12}}, Rect{908, 411, 31, 44}},
	} {
		r, err = r.Calibrate(img, c.id, c.id, c.id+"_click", c.features, c.click)
		if err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		for _, a := range r.Profile.Pages[c.id].Anchors {
			t.Logf("%s core=%+v contrast=%.3f", c.id, a.Rect, IconContrast(a))
		}
	}
	r.MainIcons = []string{"ui_bag", "ui_menu", "ui_realm"}
	return r, img
}

func TestApprovedPNGIconCoresAndNegativeFrames(t *testing.T) {
	r, img := redactedIconFixture(t)
	if got := r.Assess(iconFrames(img, img)); !got.CandidateMain || got.ActionAllowed {
		t.Fatal(got)
	}
	for _, gray := range []uint8{0, 64, 128, 192, 255} {
		unknown := image.NewRGBA(img.Bounds())
		for y := 0; y < 540; y++ {
			for x := 0; x < 960; x++ {
				unknown.SetRGBA(x, y, color.RGBA{gray, gray, gray, 255})
			}
		}
		if r.Assess(iconFrames(unknown, unknown)).CandidateMain {
			t.Fatalf("constant %d accepted", gray)
		}
	}
	// Synthetic state perturbation of a real PNG: one icon may fail, two must
	// remain consistently visible. This is not a second real game screenshot.
	changed := copyIconImage(img)
	eraseIcon(changed, r, "ui_realm")
	if !r.Assess(iconFrames(img, changed)).CandidateMain {
		t.Fatal("two remaining icons lost")
	}
	eraseIcon(changed, r, "ui_bag")
	if r.Assess(iconFrames(img, changed)).CandidateMain {
		t.Fatal("one remaining icon accepted")
	}
}

func TestPrivatePNGAdditionalIconCandidates(t *testing.T) {
	raw, _, _ := loadRecordedPNG(t)
	img, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	r := NewIconRules()
	// Candidate labels come from this static screenshot. Their destinations and
	// motion/red-dot stability remain unverified, and nothing is activated.
	for _, c := range []struct {
		id          string
		a, b, click Rect
	}{
		{"ui_gameplay", Rect{22, 139, 10, 10}, Rect{33, 147, 10, 10}, Rect{16, 130, 38, 38}},
		{"ui_market", Rect{636, 22, 10, 10}, Rect{647, 32, 10, 10}, Rect{625, 14, 40, 40}},
	} {
		r, err = r.Calibrate(img, c.id, c.id, c.id+"_click", []Rect{c.a, c.b}, c.click)
		if err != nil {
			t.Fatal(c.id, err)
		}
		if !r.Profile.Score(img, c.id).Passed {
			t.Fatal(c.id)
		}
	}
}
