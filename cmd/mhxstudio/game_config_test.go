package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ab393911/MHX-Studio-Updates/internal/gameapp"
)

func TestGameBindingAndTargetRoleSurviveAlongsideExistingSettings(t *testing.T) {
	root := t.TempDir()
	raw := []byte(`{"ldplayer_directory":"D:\\LDPlayer14","retries":7,"jobs":{"6":["合欢","合欢","合欢","合欢","合欢"]},"future":{"keep":true}}`)
	if e := os.WriteFile(filepath.Join(root, "config.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	c, e := loadSettings(root)
	if e != nil {
		t.Fatal(e)
	}
	c.Games["6"] = gameapp.Binding{Component: gameapp.Component{Package: "com.example.game", Activity: ".Main"}, AndroidID: "identity", Confirmed: true}
	c.TargetRoles["6"] = 3
	if e = c.Save(root); e != nil {
		t.Fatal(e)
	}
	got, e := loadSettings(root)
	if e != nil || got.TargetRoles["6"] != 3 || got.Games["6"].AndroidID != "identity" || got.InstallDir != `D:\LDPlayer14` || got.Jobs["6"][0] != "合欢" {
		t.Fatal(got, e)
	}
	var extra map[string]any
	if e = json.Unmarshal(got.raw["future"], &extra); e != nil || extra["keep"] != true {
		t.Fatal(extra, e)
	}
}

func TestGroupLoginPreferencesAndUnknownSettingsSurviveSaveAndReload(t *testing.T) {
	root := t.TempDir()
	if e := os.WriteFile(filepath.Join(root, "config.json"), []byte(`{"retries":7,"future":{"keep":true},"target_roles":{"6":4}}`), 0600); e != nil {
		t.Fatal(e)
	}
	c, e := loadSettings(root)
	if e != nil {
		t.Fatal(e)
	}
	c.GroupLoginServer = "一梦诛仙"
	c.GroupLoginRole = 3
	if e = c.Save(root); e != nil {
		t.Fatal(e)
	}
	got, e := loadSettings(root)
	if e != nil || got.GroupLoginServer != "一梦诛仙" || got.GroupLoginRole != 3 || got.TargetRoles["6"] != 4 || got.Retries != 7 || got.raw["future"] == nil {
		t.Fatal(got, e)
	}
	got.GroupLoginRole = 6
	if got.Validate() == nil {
		t.Fatal("invalid group role saved")
	}
}

func TestFiveInstanceBindingsSurviveRestartAndFailedSaveRollsBack(t *testing.T) {
	root := t.TempDir()
	c := defaults()
	c.TargetRoles["1"] = 5
	for i := 1; i <= 5; i++ {
		b := gameapp.Binding{Component: gameapp.Component{Package: "com.example.game", Activity: ".Main"}, AndroidID: fmt.Sprintf("id%d", i), Confirmed: true}
		if err := c.storeGame(root, i, b); err != nil {
			t.Fatal(err)
		}
	}
	got, err := loadSettings(root)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		b := got.Games[strconv.Itoa(i)]
		if !b.Confirmed || b.AndroidID != fmt.Sprintf("id%d", i) {
			t.Fatal(i, b)
		}
	}
	if got.TargetRoles["1"] != 5 {
		t.Fatal("role removed")
	}
	blocked := filepath.Join(t.TempDir(), "file")
	if err = os.WriteFile(blocked, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	old := got.Games["1"]
	if err = got.storeGame(blocked, 1, gameapp.Binding{}); err == nil || got.Games["1"] != old {
		t.Fatal("failed write changed existing binding", err)
	}
	if err = got.storeGame(blocked, 6, old); err == nil {
		t.Fatal("expected save error")
	}
	if _, exists := got.Games["6"]; exists {
		t.Fatal("failed new binding retained")
	}
}
