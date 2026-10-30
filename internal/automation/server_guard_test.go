package automation

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"testing"
)

func scopedSimulator(t *testing.T) (*visualSimulator, *ServerGuard) {
	t.Helper()
	d := simulator(t)
	img, e := Decode(d.images["tree"])
	if e != nil {
		t.Fatal(e)
	}
	rgba := image.NewRGBA(img.Bounds())
	for y := 0; y < 540; y++ {
		for x := 0; x < 960; x++ {
			rgba.Set(x, y, img.At(x, y))
		}
	}
	r1, r2 := Rect{600, 150, 50, 18}, Rect{660, 150, 50, 18}
	fill(rgba, r1, 220)
	fill(rgba, r2, 230)
	a, _ := CaptureAnchor(rgba, r1)
	b, _ := CaptureAnchor(rgba, r2)
	d.images["tree"] = encodeImage(t, rgba)
	return d, &ServerGuard{Device: d, Profile: d.profile, ServerPage: Page{Anchors: []Anchor{a, b}, Threshold: 3}}
}
func encodeImage(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if e := png.Encode(&b, img); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestObservedNamedServerThenTargetRoleCanComplete(t *testing.T) {
	d, g := scopedSimulator(t)
	eng := enterEngine(d)
	eng.Device = g
	r, e := eng.EnterRole(context.Background(), 3, EnterOptions{})
	if e != nil || !r.IdentityVerified || !g.Verified || len(g.ServerScreenSHA) != 64 || d.state != "game_3" {
		t.Fatal(r, g, e)
	}
}
func TestRoleListWithoutServerProvenanceStopsBeforeRoleTap(t *testing.T) {
	d, g := scopedSimulator(t)
	d.state = "roles"
	eng := enterEngine(d)
	eng.Device = g
	if _, e := eng.EnterRole(context.Background(), 3, EnterOptions{}); !errors.Is(e, ErrServerUnverified) || d.roleTaps != 0 || len(d.entered) != 0 {
		t.Fatal(e, d.entered)
	}
}
func TestAlreadyCorrectRoleDoesNotInventServerProofOrLogout(t *testing.T) {
	d, g := scopedSimulator(t)
	d.state = "game_3"
	eng := enterEngine(d)
	eng.Device = g
	r, e := eng.EnterRole(context.Background(), 3, EnterOptions{})
	if e != nil || !r.IdentityVerified || g.Verified || len(d.entered) != 0 || d.state != "game_3" {
		t.Fatal(r, g, e)
	}
}
func TestWrongServerLabelInvalidatesScopeBeforeLoginTap(t *testing.T) {
	d, g := scopedSimulator(t)
	d.state = "tree"
	// Use the real synthetic tree page with its ordinary page anchors, but no label.
	fresh := simulator(t)
	d.images["tree"] = fresh.images["tree"]
	if _, e := g.Capture(context.Background()); e == nil || g.Verified {
		t.Fatal(e, g)
	}
	if e := g.Tap(context.Background(), 750, 405); e == nil || d.state != "tree" {
		t.Fatal(e, d.state)
	}
}
func TestLatePageChangeCannotReuseEarlierServerState(t *testing.T) {
	d, g := scopedSimulator(t)
	d.state = "tree"
	if _, e := g.Capture(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Capture(context.Background()); e != nil {
		t.Fatal(e)
	}
	d.state = "roles"
	if e := g.Tap(context.Background(), 750, 405); e == nil || d.roleTaps != 0 || d.state != "roles" {
		t.Fatal(e, d.state)
	}
}
func TestReturnToNoticeResetsPriorServerEvidence(t *testing.T) {
	d, g := scopedSimulator(t)
	d.state = "tree"
	_, _ = g.Capture(context.Background())
	_, _ = g.Capture(context.Background())
	if !g.Verified {
		t.Fatal("fixture missing scope")
	}
	d.state = "notice"
	_, _ = g.Capture(context.Background())
	d.state = "roles"
	if _, e := g.Capture(context.Background()); !errors.Is(e, ErrServerUnverified) || g.Verified {
		t.Fatal(e)
	}
}

func TestServerEvidenceRequiresConsecutiveVisibleFrames(t *testing.T) {
	d, g := scopedSimulator(t)
	d.state = "tree"
	_, _ = g.Capture(context.Background())
	d.images["unknown"] = encodeImage(t, image.NewRGBA(image.Rect(0, 0, 960, 540)))
	d.state = "unknown"
	_, _ = g.Capture(context.Background())
	d.state = "tree"
	_, _ = g.Capture(context.Background())
	if g.Verified {
		t.Fatal("nonconsecutive server frames accepted")
	}
	_, _ = g.Capture(context.Background())
	if !g.Verified {
		t.Fatal("consecutive server frames rejected")
	}
}
