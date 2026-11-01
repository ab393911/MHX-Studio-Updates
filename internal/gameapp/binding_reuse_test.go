package gameapp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func TestReuseVerifiedGameFromDesktopAndRejectWrongTarget(t *testing.T) {
	ctx := context.Background()
	source := binding()
	saved := map[string]Binding{"1": source}
	a := &android{apps: source.String(), foreground: "com.android.launcher3"}
	got, err := Reuse(ctx, a, saved, "2", "target2")
	if err != nil || got.AndroidID != "target2" || got.Component != source.Component {
		t.Fatal(got, err)
	}
	if len(a.calls) == 0 {
		t.Fatal("not verified")
	}
	a.apps = "com.other.game/.Main"
	if _, err = Reuse(ctx, a, saved, "3", "target3"); err == nil {
		t.Fatal("missing target accepted")
	}
	saved["2"] = got
	if _, err = Reuse(ctx, a, saved, "2", "changed"); err == nil {
		t.Fatal("identity replaced")
	}
	for _, s := range a.calls {
		if len(s) >= 2 && s[:2] == "am" {
			t.Fatal("discovery launched game")
		}
	}
}
func TestLauncherCannotBeConfirmedOrReusedAndNewGameNeedsForeground(t *testing.T) {
	ctx := context.Background()
	launcher := Binding{Component: Component{"com.android.launcher3", ".Main"}, AndroidID: "123", Confirmed: true}
	a := &android{apps: launcher.String(), foreground: launcher.Package}
	if err := Confirm(ctx, a, launcher, "123"); err == nil {
		t.Fatal("launcher confirmed")
	}
	if _, err := Reuse(ctx, a, map[string]Binding{"1": launcher}, "2", "456"); err == nil {
		t.Fatal("launcher reused")
	}
	b := binding()
	a.apps = b.String()
	if err := Confirm(ctx, a, b, "123"); err == nil {
		t.Fatal("nonforeground game confirmed")
	}
	a.foreground = b.Package
	if err := Confirm(ctx, a, b, "123"); err != nil {
		t.Fatal(err)
	}
}
func TestConflictingConfirmedGamesRequireSelection(t *testing.T) {
	b := binding()
	other := b
	other.Package = "com.other.game"
	if _, err := Reuse(context.Background(), &android{apps: b.String()}, map[string]Binding{"1": b, "2": other}, "3", "target"); err == nil {
		t.Fatal("ambiguous game reused")
	}
}

func TestFiveBindingsReuseWithoutForegroundAfterRestart(t *testing.T) {
	ctx := context.Background()
	saved := map[string]Binding{"1": binding()}
	for i := 2; i <= 5; i++ {
		key := fmt.Sprint(i)
		a := &android{apps: binding().String(), foreground: "com.android.launcher3"}
		b, err := Reuse(ctx, a, saved, key, "id"+key)
		if err != nil {
			t.Fatal(i, err)
		}
		saved[key] = b
	}
	// Serialize/reload the same representation stored in config.json.
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var reopened map[string]Binding
	if err = json.Unmarshal(data, &reopened); err != nil {
		t.Fatal(err)
	}
	for key, b := range reopened {
		a := &android{apps: b.String(), foreground: "com.android.launcher3"}
		if got, err := Reuse(ctx, a, reopened, key, b.AndroidID); err != nil || got != b {
			t.Fatal(key, got, err)
		}
	}
}
