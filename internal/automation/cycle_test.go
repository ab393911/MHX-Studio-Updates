package automation

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"reflect"
	"strings"
	"testing"
	"time"
)

// A visual simulator exercises both login buttons, confirmation, all five
// identities, and the final logout. These are synthetic screens, not a game test.
type visualSimulator struct {
	state     string
	role      int
	images    map[string][]byte
	profile   Profile
	entered   []int
	roleTaps  int
	wrongRole bool
}

func (s *visualSimulator) Capture(ctx context.Context) ([]byte, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	return s.images[s.state], nil
}
func inside(r Rect, x, y int) bool { return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H }
func (s *visualSimulator) Tap(_ context.Context, x, y int) error {
	button := ""
	for k, r := range s.profile.Buttons {
		if inside(r, x, y) {
			button = k
			break
		}
	}
	switch s.state {
	case "notice":
		if button == "notice_enter" {
			s.state = "tree"
			return nil
		}
	case "tree":
		if button == "tree_enter" {
			s.state = "roles"
			return nil
		}
	case "menu":
		if button == "settings" {
			s.state = "settings"
			return nil
		}
	case "settings":
		if button == "logout" {
			s.state = "logout_confirm"
			return nil
		}
	case "logout_confirm":
		if button == "confirm_logout" {
			s.state = "notice"
			return nil
		}
	}
	if s.state == "roles" || strings.HasPrefix(s.state, "selected_") {
		for n := 1; n <= 5; n++ {
			if button == fmt.Sprintf("role_%d", n) {
				s.roleTaps++
				s.role = n
				if s.wrongRole {
					s.role = n%5 + 1
				}
				s.state = fmt.Sprintf("selected_%d", s.role)
				return nil
			}
		}
		if button == "enter" && s.role > 0 {
			s.state = fmt.Sprintf("game_%d", s.role)
			s.entered = append(s.entered, s.role)
			return nil
		}
	}
	if strings.HasPrefix(s.state, "game_") && button == "menu" {
		s.state = "menu"
		return nil
	}
	return fmt.Errorf("invalid simulated tap %s in %s", button, s.state)
}
func fill(img *image.RGBA, r Rect, v byte) {
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			img.SetRGBA(x, y, color.RGBA{v, v, v, 255})
		}
	}
}
func simulator(t *testing.T) *visualSimulator {
	t.Helper()
	p := NewProfile()
	s := &visualSimulator{state: "notice", images: map[string][]byte{}, profile: p}
	names := []string{"notice", "tree", "roles", "menu", "settings", "logout_confirm"}
	for n := 1; n <= 5; n++ {
		names = append(names, fmt.Sprintf("selected_%d", n), fmt.Sprintf("game_%d", n))
	}
	r1, r2 := Rect{10, 10, 80, 24}, Rect{120, 10, 80, 24}
	specific1, specific2 := Rect{300, 100, 60, 24}, Rect{400, 100, 60, 24}
	for i, name := range names {
		img := image.NewRGBA(image.Rect(0, 0, 960, 540))
		v := byte(20 + i*13)
		fill(img, r1, v)
		fill(img, r2, v)
		if strings.HasPrefix(name, "selected_") {
			fill(img, r1, 46)
			fill(img, r2, 46)
			fill(img, specific1, v)
			fill(img, specific2, v)
		}
		rects := []Rect{r1, r2}
		if strings.HasPrefix(name, "selected_") {
			rects = []Rect{specific1, specific2}
		}
		a, _ := CaptureAnchor(img, rects[0])
		b, _ := CaptureAnchor(img, rects[1])
		p.Pages[name] = Page{[]Anchor{a, b}, 5}
		var out bytes.Buffer
		png.Encode(&out, img)
		s.images[name] = out.Bytes()
	}
	keys := []string{"notice_enter", "tree_enter", "enter", "menu", "settings", "logout", "confirm_logout", "role_1", "role_2", "role_3", "role_4", "role_5"}
	for i, key := range keys {
		p.Buttons[key] = Rect{20 + i*70, 460, 30, 24}
	}
	s.profile = p
	return s
}
func TestFiveRolesCompleteFullVisualLoginLogoutLoop(t *testing.T) {
	d := simulator(t)
	eng := Engine{Device: d, Profile: d.profile, Options: Options{Retries: 0, MinDelayMS: 100, MaxDelayMS: 100, StateTimeout: 10 * time.Millisecond}}
	if e := eng.Cycle(context.Background(), []int{1, 2, 3, 4, 5}); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(d.entered, []int{1, 2, 3, 4, 5}) || d.state != "notice" || d.roleTaps != 5 {
		t.Fatal(d.entered, d.state, d.roleTaps)
	}
}
func TestWrongSelectedRoleNeverEntersGameAndUsesConfiguredRetries(t *testing.T) {
	d := simulator(t)
	d.state = "roles"
	d.wrongRole = true
	diagnostics := 0
	eng := Engine{Device: d, Profile: d.profile, Options: Options{Retries: 2, MinDelayMS: 100, MaxDelayMS: 100, StateTimeout: time.Millisecond, OnError: func([]byte, error) { diagnostics++ }}}
	if e := eng.Cycle(context.Background(), []int{1}); e == nil {
		t.Fatal("wrong role accepted")
	}
	if len(d.entered) != 0 || d.roleTaps != 3 || diagnostics != 1 {
		t.Fatal(d.entered, d.roleTaps, diagnostics)
	}
}
