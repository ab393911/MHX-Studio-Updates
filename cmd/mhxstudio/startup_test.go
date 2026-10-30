package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ab393911/MHX-Studio-Updates/internal/login"
)

func TestOnOpenPlanSavedWithoutErasingOldSettingsAndCanBeDisabled(t *testing.T) {
	root := t.TempDir()
	raw := []byte(`{"ldplayer_directory":"D:\\LDPlayer14","future_unknown":{"keep":true},"jobs":{"2":["合欢","合欢","合欢","合欢","合欢"]}}`)
	if e := os.WriteFile(filepath.Join(root, "config.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	c, e := loadSettings(root)
	if e != nil {
		t.Fatal(e)
	}
	if c.Startup != nil {
		t.Fatal("old config implicitly enabled startup")
	}
	p := login.StartupPlan{Enabled: true, Console: `D:\LDPlayer14\ldconsole.exe`, Target: login.Spec{Server: "一梦诛仙", Role: 3}}
	for _, n := range []int{2, 6, 9, 17, 24} {
		p.Members = append(p.Members, login.StartupMember{Index: n, AndroidID: "verified", Package: "com.example.game"})
	}
	c.Startup = &p
	if e = c.Save(root); e != nil {
		t.Fatal(e)
	}
	c, e = loadSettings(root)
	if e != nil || c.Startup == nil || !c.Startup.Enabled || c.Startup.Target.Role != 3 || c.InstallDir != `D:\LDPlayer14` || c.Jobs["2"][4] != "合欢" {
		t.Fatal(c, e)
	}
	c.Startup = &login.StartupPlan{Enabled: false}
	if e = c.Save(root); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(root, "config.json"))
	var obj map[string]any
	_ = json.Unmarshal(b, &obj)
	if obj["future_unknown"] == nil || obj["on_open_plan"].(map[string]any)["enabled"] != false {
		t.Fatal(string(b))
	}
	c, e = loadSettings(root)
	if e != nil || c.Startup.Enabled {
		t.Fatal(c, e)
	}
}
