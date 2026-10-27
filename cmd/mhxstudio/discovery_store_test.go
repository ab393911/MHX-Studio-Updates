package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ab393911/MHX-Studio-Updates/internal/ldplayer"
)

func TestAutomaticDiscoveryRemembersOnlyValidatedPathsAndPreservesConfig(t *testing.T) {
	root := t.TempDir()
	config := []byte("{\"future\":{\"keep\":true},\"retries\":7}\n")
	path := filepath.Join(root, "config.json")
	if e := os.WriteFile(path, config, 0600); e != nil {
		t.Fatal(e)
	}
	if e := rememberDiscovery(root, ldplayer.Detection{Source: "unverified candidate"}); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(root, "ldplayer-discovery.json")); !os.IsNotExist(e) {
		t.Fatal("candidate cached before validation")
	}
	d := ldplayer.Detection{ValidatedDirectory: `D:\custom LDPlayer`, ValidatedADB: `D:\custom LDPlayer\adb.exe`}
	if e := rememberDiscovery(root, d); e != nil {
		t.Fatal(e)
	}
	r, e := loadDiscovery(root)
	if e != nil || r.Directory != d.ValidatedDirectory || r.ADB != d.ValidatedADB {
		t.Fatal(r, e)
	}
	d.ValidatedDirectory = `E:\other LDPlayer`
	if e = rememberDiscovery(root, d); e != nil {
		t.Fatal(e)
	}
	history, _ := filepath.Glob(filepath.Join(root, "discovery-history", "*.json"))
	if len(history) != 1 {
		t.Fatal("previous discovery not retained", history)
	}
	b, _ := os.ReadFile(path)
	if string(b) != string(config) {
		t.Fatal("configuration was overwritten")
	}
}
func TestCorruptDiscoveryIsPreservedAndReportShowsEveryCheckedPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "ldplayer-discovery.json")
	raw := []byte("bad-json")
	if e := os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	if e := rememberDiscovery(root, ldplayer.Detection{ValidatedDirectory: "valid"}); e == nil {
		t.Fatal("corrupt original overwritten")
	}
	b, _ := os.ReadFile(path)
	if string(b) != string(raw) {
		t.Fatal("original changed")
	}
	d := ldplayer.Detection{Reason: "ADB connection refused", Checks: []ldplayer.Check{{Kind: "雷电管理程序", Path: `D:\LDPlayer14\ldconsole.exe`, Result: "不存在"}, {Kind: "ADB本地服务", Path: "127.0.0.1:5037", Result: "connection refused"}}}
	if e := writeDetectionReport(root, "3.5-test", d); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(root, "diagnostics", "detection.txt"))
	if e != nil || !strings.Contains(string(b), `D:\LDPlayer14\ldconsole.exe`) || !strings.Contains(string(b), "127.0.0.1:5037") || !strings.Contains(string(b), "3.5-test") {
		t.Fatal(string(b), e)
	}
}
