package automation

import (
	"context"
	"errors"
	"image"
	"testing"
	"time"
)

func enterEngine(d *visualSimulator) Engine {
	return Engine{Device: d, Profile: d.profile, Options: Options{Retries: 0, MinDelayMS: 100, MaxDelayMS: 100, StateTimeout: 2 * time.Second}}
}
func TestSingleTargetEntersAndStaysWithoutLoopingOtherRoles(t *testing.T) {
	d := simulator(t)
	e := enterEngine(d)
	r, err := e.EnterRole(context.Background(), 3, EnterOptions{})
	if err != nil || !r.EnteredGame || !r.SelectedRoleVerified || !r.IdentityVerified || d.state != "game_3" || len(d.entered) != 1 || d.entered[0] != 3 {
		t.Fatal(r, err, d.state, d.entered)
	}
}
func TestOtherSelectedRoleFromLoginIsHandled(t *testing.T) {
	d := simulator(t)
	d.state = "tree"
	// The role list commonly has a preselected first character.
	d.images["roles"] = d.images["selected_1"]
	e := enterEngine(d)
	r, err := e.EnterRole(context.Background(), 4, EnterOptions{})
	if err != nil || !r.IdentityVerified || d.state != "game_4" {
		t.Fatal(r, err, d.state)
	}
}
func TestAlreadyTargetMakesNoTap(t *testing.T) {
	d := simulator(t)
	d.state = "game_2"
	e := enterEngine(d)
	r, err := e.EnterRole(context.Background(), 2, EnterOptions{})
	if err != nil || !r.IdentityVerified || d.roleTaps != 0 || len(d.entered) != 0 || d.state != "game_2" {
		t.Fatal(r, err, d)
	}
}

func TestOtherCharacterUsesExistingMenuSettingsLogoutRoute(t *testing.T) {
	d := simulator(t)
	d.state = "game_1"
	e := enterEngine(d)
	r, err := e.EnterRole(context.Background(), 2, EnterOptions{})
	if err != nil || !r.IdentityVerified || d.state != "game_2" || len(d.entered) != 1 || d.entered[0] != 2 {
		t.Fatal(r, err, d.state, d.entered)
	}
}
func TestUnknownOrMissingCalibrationCannotTap(t *testing.T) {
	for _, missing := range []bool{false, true} {
		d := simulator(t)
		if missing {
			delete(d.profile.Pages, "selected_2")
		} else {
			d.images["notice"] = d.images["menu"]
			delete(d.profile.Pages, "menu")
		}
		e := enterEngine(d)
		_, err := e.EnterRole(context.Background(), 2, EnterOptions{})
		if err == nil || d.roleTaps != 0 || len(d.entered) != 0 {
			t.Fatal(err, d.entered)
		}
	}
}
func TestWrongSelectedRoleNeverPressesEnter(t *testing.T) {
	d := simulator(t)
	d.state = "roles"
	d.wrongRole = true
	e := enterEngine(d)
	e.Options.Retries = 1
	e.Options.StateTimeout = 20 * time.Millisecond
	_, err := e.EnterRole(context.Background(), 1, EnterOptions{})
	if err == nil || len(d.entered) != 0 || d.roleTaps != 2 {
		t.Fatal(err, d.roleTaps, d.entered)
	}
}
func TestGenericGameOnlyIsExplicitlyPartial(t *testing.T) {
	d := simulator(t)
	d.state = "roles"
	p := d.profile.Pages["game_2"]
	delete(d.profile.Pages, "game_2")
	d.profile.Pages["game"] = p
	e := enterEngine(d)
	r, err := e.EnterRole(context.Background(), 2, EnterOptions{})
	if !errors.Is(err, ErrIdentityUnverified) || !r.EnteredGame || r.IdentityVerified {
		t.Fatal(r, err)
	}
}
func TestAmbiguousPageAndIdentityAreRejected(t *testing.T) {
	d := simulator(t)
	img, _ := Decode(d.images["notice"])
	d.profile.Pages["tree"] = d.profile.Pages["notice"]
	if _, err := d.profile.Recognize(img); err == nil {
		t.Fatal("ambiguous login accepted")
	}
	d = simulator(t)
	img, _ = Decode(d.images["selected_1"])
	d.profile.Pages["selected_2"] = d.profile.Pages["selected_1"]
	if _, err := d.profile.Recognize(img); err == nil {
		t.Fatal("ambiguous identity accepted")
	}
}
func TestCanceledSingleLoginDoesNotTapOrSaveFailure(t *testing.T) {
	d := simulator(t)
	e := enterEngine(d)
	calls := 0
	e.Options.OnError = func([]byte, error) { calls++ }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.EnterRole(ctx, 1, EnterOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls != 0 || d.roleTaps != 0 {
		t.Fatal(calls, d)
	}
}

type changingDevice struct {
	d              *visualSimulator
	captures, taps int
}

func (d *changingDevice) Capture(ctx context.Context) ([]byte, error) {
	d.captures++
	if d.captures >= 3 {
		return d.d.images["settings"], nil
	}
	return d.d.Capture(ctx)
}
func (d *changingDevice) Tap(ctx context.Context, x, y int) error {
	d.taps++
	return d.d.Tap(ctx, x, y)
}
func TestLateDialogAfterRandomDelayStopsBeforeTap(t *testing.T) {
	d := simulator(t)
	c := &changingDevice{d: d}
	e := enterEngine(d)
	e.Device = c
	if _, err := e.EnterRole(context.Background(), 1, EnterOptions{}); err == nil || c.taps != 0 {
		t.Fatal(err, c.taps)
	}
}
func TestWrongResolutionRejected(t *testing.T) {
	p := NewProfile()
	if _, err := p.Recognize(image.NewRGBA(image.Rect(0, 0, 1, 1))); err == nil {
		t.Fatal("unknown image accepted")
	}
}

type startupDevice struct {
	d                   *visualSimulator
	captures, earlyTaps int
}

func (d *startupDevice) Capture(ctx context.Context) ([]byte, error) {
	d.captures++
	if d.captures < 3 {
		return d.d.images["settings"], ctx.Err()
	}
	return d.d.Capture(ctx)
}
func (d *startupDevice) Tap(ctx context.Context, x, y int) error {
	if d.captures < 3 {
		d.earlyTaps++
	}
	return d.d.Tap(ctx, x, y)
}
func TestColdStartWaitsForKnownPageWithoutClickingLoadingScreen(t *testing.T) {
	d := simulator(t)
	delete(d.profile.Pages, "settings")
	device := &startupDevice{d: d}
	eng := enterEngine(d)
	eng.Device = device
	r, e := eng.EnterRole(context.Background(), 1, EnterOptions{InitialWait: 2 * time.Second})
	if e != nil || !r.IdentityVerified || device.earlyTaps != 0 {
		t.Fatal(r, e, device)
	}
}

func TestSlotEntryWithoutNicknameUsesSelectedRowAndGenericMain(t *testing.T) {
	d := simulator(t)
	d.profile.Pages["game"] = d.profile.Pages["game_3"]
	e := enterEngine(d)
	r, err := e.EnterRole(context.Background(), 3, EnterOptions{SkipNicknameCheck: true})
	if err != nil || !r.SelectedRoleVerified || !r.IdentityVerified || d.state != "game_3" {
		t.Fatal(r, err, d.state)
	}
	if _, ok := d.profile.Pages["game_3"]; !ok {
		t.Fatal("saved nickname profile was mutated")
	}
	// Starting with a generic game image cannot prove its slot; follow the full
	// logout/selection path and do not accept the old main screen as identity.
	d.entered = nil
	r, err = e.EnterRole(context.Background(), 3, EnterOptions{SkipNicknameCheck: true})
	if err != nil || !r.SelectedRoleVerified || len(d.entered) != 1 || d.entered[0] != 3 {
		t.Fatal(r, err, d.entered)
	}
}
func TestSlotEntryWithoutNicknameStillRejectsWrongSelectedRow(t *testing.T) {
	d := simulator(t)
	d.profile.Pages["game"] = d.profile.Pages["game_1"]
	d.state = "roles"
	d.wrongRole = true
	e := enterEngine(d)
	e.Options.StateTimeout = 20 * time.Millisecond
	if _, err := e.EnterRole(context.Background(), 1, EnterOptions{SkipNicknameCheck: true}); err == nil || len(d.entered) > 0 {
		t.Fatal(err, d.entered)
	}
}
