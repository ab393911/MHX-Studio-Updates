package gameapp

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type android struct {
	apps, foreground string
	running          bool
	calls            []string
	launchError      string
	window           string
}

func (a *android) Shell(ctx context.Context, args ...string) ([]byte, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	c := strings.Join(args, " ")
	a.calls = append(a.calls, c)
	switch {
	case strings.Contains(c, "query-activities"):
		return []byte(a.apps), nil
	case strings.HasPrefix(c, "dumpsys"):
		if a.window != "" && strings.Contains(c, "window") {
			return []byte("mCurrentFocus=Window{a u0 " + a.window + "/.Main}"), nil
		}
		return []byte("mResumedActivity: ActivityRecord{abc u0 " + a.foreground + "/.Main t1}"), nil
	case strings.HasPrefix(c, "pidof"):
		if a.running {
			return []byte("313"), nil
		}
		return nil, fmt.Errorf("not running")
	case strings.HasPrefix(c, "am start"):
		if a.launchError != "" {
			return []byte(a.launchError), nil
		}
		a.foreground = "com.example.game"
		return []byte("Status: ok"), nil
	}
	return nil, fmt.Errorf("unexpected: %s", c)
}
func binding() Binding {
	return Binding{Component: Component{"com.example.game", ".Main"}, AndroidID: "123", Confirmed: true}
}
func TestDiscoveryDoesNotLaunchOrGuessTarget(t *testing.T) {
	a := &android{apps: "2 activities found:\ncom.example.other/.Other\ncom.example.game/.Main\n", foreground: "com.example.other"}
	inv, e := Discover(context.Background(), a)
	if e != nil || len(inv.Apps) != 2 || inv.Foreground != "com.example.other" {
		t.Fatal(inv, e)
	}
	for _, c := range a.calls {
		if strings.HasPrefix(c, "am") {
			t.Fatal(c)
		}
	}
}
func TestAlreadyForegroundNeverRelaunches(t *testing.T) {
	a := &android{apps: binding().String(), foreground: "com.example.game"}
	r, e := Open(context.Background(), a, binding(), "123")
	if e != nil || !r.AlreadyForeground {
		t.Fatal(r, e)
	}
	for _, c := range a.calls {
		if strings.HasPrefix(c, "am") {
			t.Fatal(c)
		}
	}
}
func TestOpenVerifiesForegroundAndDoesNotForceStop(t *testing.T) {
	for _, running := range []bool{false, true} {
		a := &android{apps: binding().String(), foreground: "com.android.launcher", running: running}
		r, e := Open(context.Background(), a, binding(), "123")
		if e != nil || r.ProcessWasRunning != running {
			t.Fatal(r, e)
		}
		launches := 0
		for _, c := range a.calls {
			if strings.Contains(c, "force-stop") || strings.Contains(c, " -S") {
				t.Fatal(c)
			}
			if strings.HasPrefix(c, "am start") {
				launches++
			}
		}
		if launches != 1 {
			t.Fatal(a.calls)
		}
	}
}
func TestInvalidOrChangedBindingsCannotLaunch(t *testing.T) {
	for _, tc := range []struct {
		b        Binding
		id, apps string
	}{{Binding{}, "123", binding().String()}, {binding(), "changed", binding().String()}, {binding(), "123", "com.example.other/.Main"}} {
		a := &android{apps: tc.apps, foreground: "com.android.launcher"}
		if _, e := Open(context.Background(), a, tc.b, tc.id); e == nil {
			t.Fatal(tc)
		}
		for _, c := range a.calls {
			if strings.HasPrefix(c, "am") {
				t.Fatal(c)
			}
		}
	}
}
func TestComponentsAndForegroundRejectInjectedOrHistoricalValues(t *testing.T) {
	got := ParseComponents("com.example.game/.Main\ncom.example.game/com.example.game.Main\ncom.evil.app/.Main;reboot\ncom.example.game/.Main$Nested\n")
	if len(got) != 2 {
		t.Fatal(got)
	}
	if _, e := ForegroundPackage("Hist #0 ActivityRecord{abc u0 com.example.game/.Main}"); e == nil {
		t.Fatal("historical record accepted")
	}
	if _, e := ForegroundPackage("mResumedActivity: A{com.one.app/.Main}\nmResumedActivity: A{com.two.app/.Main}"); e == nil {
		t.Fatal("conflicting foreground accepted")
	}
	if p, e := ForegroundPackage("mCurrentFocus=Window{a u0 com.example.game/.Main}"); e != nil || p != "com.example.game" {
		t.Fatal(p, e)
	}
	if !reflect.DeepEqual(ParseComponents("No activities found"), []Component(nil)) {
		t.Fatal("invented component")
	}
}
func TestAndroidLaunchErrorDoesNotCountAsSuccess(t *testing.T) {
	a := &android{apps: binding().String(), foreground: "com.android.launcher", launchError: "Error: Activity class does not exist."}
	if _, e := Open(context.Background(), a, binding(), "123"); e == nil {
		t.Fatal("error accepted")
	}
}
func TestStopBeforeLaunch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := &android{apps: binding().String()}
	if _, e := Open(ctx, a, binding(), "123"); e == nil {
		t.Fatal("canceled launch")
	}
	if len(a.calls) != 0 {
		t.Fatal(a.calls)
	}
}

func TestSystemOverlayStopsClicksEvenWithGameResumed(t *testing.T) {
	a := &android{foreground: "com.example.game", window: "com.android.permissioncontroller"}
	if e := RequireForeground(context.Background(), a, "com.example.game"); e == nil {
		t.Fatal("overlay accepted")
	}
}
