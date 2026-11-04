package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/update"
)

func storedIconFixture(t *testing.T, invert bool) automation.IconRules {
	t.Helper()
	r := automation.NewIconRules()
	img := image.NewRGBA(image.Rect(0, 0, 960, 540))
	for i, id := range []string{"ui_bag", "ui_menu", "ui_team"} {
		x := 40 + i*200
		for y := 100; y < 130; y++ {
			for xx := x; xx < x+40; xx++ {
				v := uint8(35 + (xx*17+y*23+i*41)%190)
				if invert {
					v = 255 - v
				}
				img.SetRGBA(xx, y, color.RGBA{v, v, v, 255})
			}
		}
		var err error
		r, err = r.Calibrate(img, id, id, id+"_click", []automation.Rect{{X: x, Y: 100, W: 10, H: 8}, {X: x + 14, Y: 112, W: 10, H: 8}}, automation.Rect{X: x, Y: 100, W: 40, H: 30})
		if err != nil {
			t.Fatal(err)
		}
	}
	r, err := composeIconMain(r)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestIconDraftUsesExistingUpdateBackupAndRollback(t *testing.T) {
	root := t.TempDir()
	rules := storedIconFixture(t, false)
	if err := saveIconScope(root, 1, true, rules, ""); err != nil {
		t.Fatal(err)
	}
	path := iconScopePath(root, 1, true)
	before, _ := os.ReadFile(path)
	backup := filepath.Join(t.TempDir(), "snapshot")
	token := strings.Repeat("b", 48)
	if err := update.BackupConfig(root, backup, token); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("interrupted future update"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := update.RestoreConfig(backup, root, token); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("existing update snapshot omitted icon drafts")
	}
}
func TestIconSharedReuseOverrideRestorePreservesExistingData(t *testing.T) {
	root := t.TempDir()
	sentinels := []string{"config.json", "studio.sqlite", "profiles/1.json"}
	for _, name := range sentinels {
		if err := filesafe.Write(filepath.Join(root, name), []byte("existing-user-data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, b := storedIconFixture(t, false), storedIconFixture(t, true)
	if err := saveIconScope(root, 1, true, a, ""); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		got, err := loadIcons(root, i)
		if err != nil || len(got.Profile.Pages) != 3 || got.Profile.Pages["ui_bag"].Anchors[0].Pixels[0] != a.Profile.Pages["ui_bag"].Anchors[0].Pixels[0] {
			t.Fatal(i, err)
		}
	}
	if err := saveIconScope(root, 1, false, b, ""); err != nil {
		t.Fatal(err)
	}
	one, _ := loadIcons(root, 1)
	two, _ := loadIcons(root, 2)
	if bytes.Equal(one.Profile.Pages["ui_bag"].Anchors[0].Pixels, two.Profile.Pages["ui_bag"].Anchors[0].Pixels) {
		t.Fatal("instance override lost")
	}
	_, localSHA, _ := readIconScope(root, 1, false)
	if err := saveIconScope(root, 1, false, b, localSHA); err != nil {
		t.Fatal(err)
	}
	one, _ = loadIcons(root, 1)
	two, _ = loadIcons(root, 2)
	if one.Revision != 2 || two.Revision != 1 {
		t.Fatal("effective revision ignored local scope", one.Revision, two.Revision)
	}
	_, sha, err := readIconScope(root, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = saveIconScope(root, 1, true, b, sha); err != nil {
		t.Fatal(err)
	}
	if saveIconScope(root, 1, true, a, sha) == nil {
		t.Fatal("stale writer accepted")
	}
	history, _ := filepath.Glob(filepath.Join(root, "profiles", "icon-drafts", "history", "*.json"))
	if len(history) != 2 {
		t.Fatal(history)
	}
	var restore automation.IconRules
	sharedHistory, _ := filepath.Glob(filepath.Join(root, "profiles", "icon-drafts", "history", "shared.json-*.json"))
	if len(sharedHistory) != 1 {
		t.Fatal(sharedHistory)
	}
	if err = filesafe.ReadJSON(sharedHistory[0], &restore); err != nil {
		t.Fatal(err)
	}
	_, current, _ := readIconScope(root, 1, true)
	if err = saveIconScope(root, 1, true, restore, current); err != nil {
		t.Fatal(err)
	}
	got, _, err := readIconScope(root, 1, true)
	if err != nil || got.Revision != 3 || !bytes.Equal(got.Profile.Pages["ui_bag"].Anchors[0].Pixels, a.Profile.Pages["ui_bag"].Anchors[0].Pixels) {
		t.Fatal(got.Revision, err)
	}
	for _, name := range sentinels {
		raw, _ := os.ReadFile(filepath.Join(root, name))
		if string(raw) != "existing-user-data" {
			t.Fatal("existing data changed", name)
		}
	}
}
func TestIconFailedAtomicWriteAndCorruptRecovery(t *testing.T) {
	root := t.TempDir()
	a, b := storedIconFixture(t, false), storedIconFixture(t, true)
	if err := saveIconScope(root, 1, true, a, ""); err != nil {
		t.Fatal(err)
	}
	path := iconScopePath(root, 1, true)
	before, _ := os.ReadFile(path)
	_, sha, _ := readIconScope(root, 1, true)
	err := writeIconScope(root, 1, true, b, sha, func(p string, raw []byte, mode os.FileMode) error {
		if p == path {
			return errors.New("injected write interruption")
		}
		return filesafe.Write(p, raw, mode)
	})
	after, _ := os.ReadFile(path)
	if err == nil || !bytes.Equal(before, after) {
		t.Fatal("failed write replaced active draft", err)
	}
	if err = os.WriteFile(path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = loadIcons(root, 2); err == nil {
		t.Fatal("corrupt rules silently ignored")
	}
	_, brokenSHA, _ := readIconScope(root, 1, true)
	var valid automation.IconRules
	if err = json.Unmarshal(before, &valid); err != nil {
		t.Fatal(err)
	}
	if err = saveIconScope(root, 1, true, valid, brokenSHA); err != nil {
		t.Fatal(err)
	}
	if _, err = loadIcons(root, 2); err != nil {
		t.Fatal("valid previous draft not restored", err)
	}
}
