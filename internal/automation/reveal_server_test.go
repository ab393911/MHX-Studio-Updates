package automation

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type backSimulator struct {
	*visualSimulator
	backs       int
	directRoles bool
}

func (d *backSimulator) Tap(ctx context.Context, x, y int) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if (d.state == "roles" || strings.HasPrefix(d.state, "selected_")) && inside(Rect{X: 24, Y: 8, W: 32, H: 26}, x, y) {
		d.state = "tree"
		d.backs++
		return nil
	}
	if d.state == "notice" && d.directRoles {
		d.state = "roles"
		return nil
	}
	return d.visualSimulator.Tap(ctx, x, y)
}
func TestRevealServerOnlyNavigatesFromRoleListThenVerifiesNamedLabel(t *testing.T) {
	d, g := scopedSimulator(t)
	d.state = "roles"
	back := &backSimulator{visualSimulator: d}
	g.Device = back
	if _, e := g.Capture(context.Background()); !errors.Is(e, ErrServerUnverified) {
		t.Fatal(e)
	}
	if e := g.RevealServer(context.Background(), Rect{X: 24, Y: 8, W: 32, H: 26}, 100, 100); e != nil || !g.Verified || back.backs != 1 || d.state != "tree" {
		t.Fatal(e, back.backs, g)
	}
	eng := enterEngine(d)
	eng.Device = g
	r, e := eng.EnterRole(context.Background(), 3, EnterOptions{})
	if e != nil || !r.IdentityVerified || d.state != "game_3" {
		t.Fatal(r, e)
	}
}
func TestAnnouncementDirectlyToRolesCanRecoverWithoutRestartingGame(t *testing.T) {
	d, g := scopedSimulator(t)
	back := &backSimulator{visualSimulator: d, directRoles: true}
	g.Device = back
	eng := enterEngine(d)
	eng.Device = g
	if _, e := eng.EnterRole(context.Background(), 3, EnterOptions{}); !errors.Is(e, ErrServerUnverified) {
		t.Fatal(e)
	}
	if e := g.RevealServer(context.Background(), Rect{X: 24, Y: 8, W: 32, H: 26}, 100, 100); e != nil {
		t.Fatal(e)
	}
	r, e := eng.EnterRole(context.Background(), 3, EnterOptions{})
	if e != nil || !r.IdentityVerified || back.backs != 1 || len(d.entered) != 1 {
		t.Fatal(r, e)
	}
}
func TestRevealDoesNotLogoutAlreadyCorrectGameOrAcceptWrongServer(t *testing.T) {
	d, g := scopedSimulator(t)
	d.state = "game_3"
	back := &backSimulator{visualSimulator: d}
	g.Device = back
	if e := g.RevealServer(context.Background(), Rect{X: 24, Y: 8, W: 32, H: 26}, 100, 100); e == nil || back.backs != 0 || d.state != "game_3" {
		t.Fatal(e)
	}
	d.state = "roles"
	d.images["tree"] = simulator(t).images["tree"]
	if e := g.RevealServer(context.Background(), Rect{X: 24, Y: 8, W: 32, H: 26}, 100, 100); e == nil || g.Verified || back.backs != 1 || d.roleTaps != 0 {
		t.Fatal(e)
	}
}
