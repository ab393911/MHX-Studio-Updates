package main

import (
	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func mainFrame(value uint8) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 960, 540))
	for _, r := range []image.Rectangle{image.Rect(15, 30, 79, 62), image.Rect(800, 400, 864, 432)} {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				v := uint8(int(value) + (x+y)%20)
				img.SetRGBA(x, y, color.RGBA{v, v, v, 255})
			}
		}
	}
	return img
}
func localMain(img image.Image) automation.Profile {
	p := automation.NewProfile()
	a, _ := automation.CaptureAnchor(img, automation.Rect{X: 15, Y: 30, W: 64, H: 32})
	b, _ := automation.CaptureAnchor(img, automation.Rect{X: 800, Y: 400, W: 64, H: 32})
	p.Pages["game"] = automation.Page{Anchors: []automation.Anchor{a, b}, Threshold: 10}
	p.Buttons["menu"] = automation.Rect{X: 20, Y: 480, W: 30, H: 24}
	return p
}
func mainScore(t *testing.T, r recognitionReport) recognitionPage {
	t.Helper()
	for _, p := range r.Pages {
		if p.Name == "game" {
			return p
		}
	}
	t.Fatal("game missing")
	return recognitionPage{}
}
func TestSavedCalibrationImmediateReloadRestartAndLocalPriority(t *testing.T) {
	root := t.TempDir()
	a, b := mainFrame(40), mainFrame(180)
	if err := saveProfile(root, 1, localMain(a)); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(profilePath(root, 1))
	if err != nil {
		t.Fatal(err)
	}
	first, err := inspectRecognition(root, 1, a)
	if err != nil || !mainScore(t, first).Passed {
		t.Fatal(first, err)
	}
	if err = saveProfile(root, 1, localMain(b)); err != nil {
		t.Fatal(err)
	}
	second, err := inspectRecognition(root, 1, b)
	if err != nil || !mainScore(t, second).Passed || mainScore(t, second).Source != "local" || second.ProfileSHA == first.ProfileSHA || second.CacheUsed {
		t.Fatal(second, err)
	}
	restart, err := loadProfile(root, 1)
	if err != nil || !restart.Match(b, "game") || restart.Match(a, "game") {
		t.Fatal(err)
	}
	again, err := inspectRecognition(root, 1, b)
	if err != nil || again.ProfileSHA != second.ProfileSHA || !mainScore(t, again).Passed {
		t.Fatal(err)
	}
	history, _ := filepath.Glob(filepath.Join(root, "profiles", "history", "*.json"))
	if len(history) != 1 {
		t.Fatal(history)
	}
	kept, _ := os.ReadFile(history[0])
	if string(kept) != string(old) {
		t.Fatal("old calibration not preserved")
	}
	changed, err := inspectRecognition(root, 1, a)
	if err != nil || mainScore(t, changed).Passed {
		t.Fatal("old frame falsely accepted", err)
	}
}
func TestInstanceProfilesCannotLeakAcrossIndicesAndChecksAreReadOnly(t *testing.T) {
	root := t.TempDir()
	a, b := mainFrame(40), mainFrame(180)
	if err := saveProfile(root, 1, localMain(a)); err != nil {
		t.Fatal(err)
	}
	if err := saveProfile(root, 2, localMain(b)); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(root, "studio.sqlite3")
	if err := os.WriteFile(db, []byte("untouched-existing-database"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(profilePath(root, 1))
	r, err := inspectRecognition(root, 1, b)
	if err != nil || mainScore(t, r).Passed {
		t.Fatal("cross-instance profile accepted", err)
	}
	r, err = inspectRecognition(root, 2, b)
	if err != nil || !mainScore(t, r).Passed {
		t.Fatal(err)
	}
	other := image.NewRGBA(image.Rect(0, 0, 960, 540))
	r, err = inspectRecognition(root, 1, other)
	if err != nil || mainScore(t, r).Passed {
		t.Fatal("non-main accepted", err)
	}
	after, _ := os.ReadFile(profilePath(root, 1))
	data, _ := os.ReadFile(db)
	if string(after) != string(raw) || string(data) != "untouched-existing-database" {
		t.Fatal("read-only inspection wrote profile/database")
	}
}
