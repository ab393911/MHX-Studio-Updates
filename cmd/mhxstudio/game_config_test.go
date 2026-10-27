package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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
