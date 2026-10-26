package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigRetainsUnknownFields(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	raw := []byte(`{"retries":5,"future":{"account_local_only":true},"manifest_url":"https://example.invalid/update.json"}`)
	if e := os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	c, e := loadSettings(root)
	if e != nil {
		t.Fatal(e)
	}
	c.Retries = 8
	if e = c.Save(root); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(path)
	var obj map[string]any
	if e = json.Unmarshal(b, &obj); e != nil {
		t.Fatal(e)
	}
	if obj["future"].(map[string]any)["account_local_only"] != true || obj["retries"] != float64(8) {
		t.Fatal(string(b))
	}
}
func TestCorruptConfigIsNotOverwritten(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	raw := []byte("not json")
	os.WriteFile(path, raw, 0600)
	if _, e := loadSettings(root); e == nil {
		t.Fatal("corrupt config accepted")
	}
	b, _ := os.ReadFile(path)
	if string(b) != string(raw) {
		t.Fatal("config overwritten")
	}
}
