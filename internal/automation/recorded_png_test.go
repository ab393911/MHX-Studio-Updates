package automation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type recordedExpectation struct {
	PNG        string    `json:"source_png_sha256"`
	Pixels     string    `json:"image_pixel_sha256"`
	Profile    string    `json:"profile_file_sha256"`
	MeanErrors []float64 `json:"mean_errors"`
	Changed    []int     `json:"changed_pixels"`
	Threshold  float64   `json:"threshold"`
}

// Private fixtures are opt-in and remain outside the repository. No synthetic
// fallback is counted as a recorded-image test.
func loadRecordedPNG(t *testing.T) ([]byte, Profile, recordedExpectation) {
	t.Helper()
	dir := os.Getenv("MHX_REPLAY_FIXTURE_DIR")
	if dir == "" {
		t.Skip("recorded PNG fixture not supplied; no private-image validation performed")
	}
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var expected recordedExpectation
	if err := json.Unmarshal(read("expected.json"), &expected); err != nil {
		t.Fatal(err)
	}
	if len(expected.MeanErrors) != 2 || len(expected.Changed) != 2 || expected.Threshold != 10 {
		t.Fatal("invalid fixture expectations")
	}
	raw, rules := read("screenshot.png"), read("recorded-profile.json")
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != expected.PNG || fmt.Sprintf("%x", sha256.Sum256(rules)) != expected.Profile {
		t.Fatal("recorded bytes changed")
	}
	p := NewProfile()
	if err := json.Unmarshal(rules, &p); err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	return raw, p, expected
}

func TestRecordedPNGPreservesFailureAndThreshold(t *testing.T) {
	raw, p, want := loadRecordedPNG(t)
	img, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if FrameSHA(img) != want.Pixels {
		t.Fatal("decoded pixels changed")
	}
	for y := 0; y < 540; y++ {
		for x := 0; x < 960; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a != 65535 {
				t.Fatal("unexpected transparent pixels")
			}
		}
	}
	score := p.Explain(img, "game")
	if score.Passed || len(score.Anchors) != 2 || p.Pages["game"].Threshold != 10 {
		t.Fatal(score)
	}
	for n, a := range score.Anchors {
		if a.Passed || a.MeanLimit != want.Threshold || a.MeanAbsoluteError != want.MeanErrors[n] || a.ChangedPixels != want.Changed[n] {
			t.Fatal(a)
		}
	}
	if _, err := p.Recognize(img); err == nil {
		t.Fatal("unconfirmed frame accepted")
	}
}

type recordedFrameDevice struct {
	raw            []byte
	captures, taps int
}

func (d *recordedFrameDevice) Capture(ctx context.Context) ([]byte, error) {
	d.captures++
	return d.raw, ctx.Err()
}
func (d *recordedFrameDevice) Tap(context.Context, int, int) error { d.taps++; return nil }

func TestRecordedPNGFailureStopsBeforeTap(t *testing.T) {
	raw, recorded, _ := loadRecordedPNG(t)
	p := simulator(t).profile
	for name, page := range recorded.Pages {
		p.Pages[name] = page
	}
	for name, rect := range recorded.Buttons {
		p.Buttons[name] = rect
	}
	img, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Recognize(img); err == nil {
		t.Fatal("fixture unexpectedly matches")
	}
	d := &recordedFrameDevice{raw: raw}
	e := Engine{Device: d, Profile: p, Options: Options{Retries: 0, MinDelayMS: 100, MaxDelayMS: 100, StateTimeout: time.Second}}
	_, err = e.EnterRole(context.Background(), 1, EnterOptions{})
	if err == nil || d.captures == 0 || d.taps != 0 {
		t.Fatalf("err=%v captures=%d taps=%d", err, d.captures, d.taps)
	}
}
