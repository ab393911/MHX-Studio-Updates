package main

import (
	"bytes"
	"context"
	"github.com/ab393911/MHX-Studio-Updates/internal/gameapp"
	"github.com/ab393911/MHX-Studio-Updates/internal/ldplayer"
	"os"
	"path/filepath"
	"testing"
)

func TestGameContinuationCancellationAndSingleConsumption(t *testing.T) {
	target := targetForGame(ldplayer.Client{Console: "console", ADB: "adb"}, ldplayer.Instance{Index: 4, PID: 400, Serial: "serial", AndroidID: "id", BootID: "boot"}, 3)
	ctx, cancel := context.WithCancel(context.Background())
	g := &gameContinuation{Context: ctx, Target: target, Action: 118}
	// Model a finished worker whose result is still queued when Stop arrives.
	cancel()
	if g.Take(target) {
		t.Fatal("canceled result resumed launch or binding")
	}
	g = &gameContinuation{Context: context.Background(), Target: target, Action: 118}
	if !g.Take(target) || g.Take(target) {
		t.Fatal("continuation did not execute exactly once")
	}
	var absent *gameContinuation
	if absent.Take(target) {
		t.Fatal("nil continuation executed")
	}
}
func TestGameContinuationRejectsChangedIdentityRoleAndInstallation(t *testing.T) {
	base := targetForGame(ldplayer.Client{Console: "console", ADB: "adb"}, ldplayer.Instance{Index: 4, PID: 400, TopWindow: 40, BindWindow: 41, Serial: "serial", AndroidID: "id", BootID: "boot"}, 3)
	for _, edit := range []func(*gameTarget){
		func(g *gameTarget) { g.Index++ }, func(g *gameTarget) { g.PID++ }, func(g *gameTarget) { g.TopWindow++ }, func(g *gameTarget) { g.BindWindow++ },
		func(g *gameTarget) { g.Serial = "other" }, func(g *gameTarget) { g.AndroidID = "clone" }, func(g *gameTarget) { g.BootID = "restarted" },
		func(g *gameTarget) { g.Role++ }, func(g *gameTarget) { g.Console = "other-installation" }, func(g *gameTarget) { g.ADB = "other-adb" },
	} {
		changed := base
		edit(&changed)
		g := &gameContinuation{Context: context.Background(), Target: base}
		if g.Take(changed) {
			t.Fatal("stale result accepted", changed)
		}
	}
}
func TestCanceledQueuedBindingCannotModifyDurableConfiguration(t *testing.T) {
	root := t.TempDir()
	cfg := defaults()
	if err := cfg.Save(root); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	target := gameTarget{Index: 4}
	gate := &gameContinuation{Context: ctx, Target: target}
	called := false
	queued := func() {
		gate.Apply(target, func() {
			called = true
			b := gameapp.Binding{Component: gameapp.Component{Package: "com.example.game", Activity: ".Main"}, AndroidID: "id", Confirmed: true}
			if err := cfg.storeGame(root, 4, b); err != nil {
				t.Fatal(err)
			}
		})
	}
	cancel()
	queued()
	after, err := os.ReadFile(filepath.Join(root, "config.json"))
	if err != nil || called || !bytes.Equal(before, after) || len(cfg.Games) != 0 {
		t.Fatal("canceled callback changed memory/file", err)
	}
}
