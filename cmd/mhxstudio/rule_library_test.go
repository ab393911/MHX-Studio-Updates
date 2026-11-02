package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestSharedRulesReloadInstanceOverrideAndExplicitHistoryRestore(t *testing.T) {
	root := t.TempDir()
	a, b := mainFrame(40), mainFrame(180)
	shared := localMain(a)
	if err := saveRuleLibrary(root, 0, shared, true); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		p, e := loadProfile(root, i)
		if e != nil || !p.Match(a, "game") {
			t.Fatal(i, e)
		}
	}
	if err := saveProfile(root, 1, localMain(b)); err != nil {
		t.Fatal(err)
	}
	p, _ := loadProfile(root, 1)
	if !p.Match(b, "game") || p.Match(a, "game") {
		t.Fatal("local priority lost")
	}
	if err := saveRuleLibrary(root, 0, localMain(b), true); err != nil {
		t.Fatal(err)
	}
	p, _ = loadProfile(root, 2)
	if !p.Match(b, "game") {
		t.Fatal("shared file was cached")
	}
	history, _ := filepath.Glob(filepath.Join(root, "profiles", "history", "shared-*.json"))
	if len(history) != 1 {
		t.Fatal(history)
	}
	if err := restoreRuleLibrary(root, 0, history[0], true); err != nil {
		t.Fatal(err)
	}
	p, _ = loadProfile(root, 2)
	if !p.Match(a, "game") {
		t.Fatal("previous valid shared version not restored")
	}
	p, _ = loadProfile(root, 1)
	if !p.Match(b, "game") {
		t.Fatal("restore altered instance override")
	}
	before, _ := os.ReadFile(sharedProfilePath(root))
	bad := filepath.Join(root, "bad.json")
	_ = os.WriteFile(bad, []byte(`{"schema":1,"pages":{"game":{}},"buttons":{}}`), 0600)
	if restoreRuleLibrary(root, 0, bad, true) == nil {
		t.Fatal("invalid restore accepted")
	}
	after, _ := os.ReadFile(sharedProfilePath(root))
	if !bytes.Equal(before, after) {
		t.Fatal("invalid restore changed library")
	}
}
func TestDiagnosticBundleExportsExactFrameAndSnapshotsNotPrivateData(t *testing.T) {
	root := t.TempDir()
	img := mainFrame(40)
	if err := saveProfile(root, 1, localMain(img)); err != nil {
		t.Fatal(err)
	}
	report, err := inspectRecognition(root, 1, img)
	if err != nil {
		t.Fatal(err)
	}
	raw := append([]byte(nil), report.InstanceSnapshot...)
	// Changing the file after inspection must not change the exported evidence.
	if err = saveProfile(root, 1, localMain(mainFrame(180))); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	_ = png.Encode(&encoded, img)
	doc := map[string]any{"instance": 1, "serial": "fixture-1", "report": report}
	bundle, err := saveRecognitionBundle(root, 1, encoded.Bytes(), doc, report)
	if err != nil {
		t.Fatal(err)
	}
	_ = filesafe.Write(filepath.Join(bundle.Directory, "config.json"), []byte("secret"), 0600)
	target := filepath.Join(t.TempDir(), "report.zip")
	if err = exportRecognitionBundle(bundle, target); err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(target)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	names := map[string]bool{}
	for _, f := range z.File {
		names[f.Name] = true
		if f.Name == "config.json" {
			t.Fatal("configuration exported")
		}
	}
	if !names["screenshot.png"] || !names["report.json"] || !names["instance-profile.json"] {
		t.Fatal(names)
	}
	kept, _ := os.ReadFile(filepath.Join(bundle.Directory, "instance-profile.json"))
	if !bytes.Equal(kept, raw) {
		t.Fatal("template snapshot changed after inspection")
	}
	frame, _ := os.ReadFile(filepath.Join(bundle.Directory, "screenshot.png"))
	if !bytes.Equal(frame, encoded.Bytes()) {
		t.Fatal("raw frame resized/reencoded")
	}
	snapshot, _ := os.ReadFile(filepath.Join(bundle.Directory, "report.json"))
	var v map[string]any
	if json.Unmarshal(snapshot, &v) != nil {
		t.Fatal("invalid JSON")
	}
	other, err := saveRecognitionBundle(root, 2, encoded.Bytes(), map[string]any{"instance": 2}, report)
	if err != nil || other.Directory == bundle.Directory {
		t.Fatal("instance diagnostics not isolated", err)
	}
}
func TestUnavailableScreenshotExportsFailureWithoutFakeImage(t *testing.T) {
	root := t.TempDir()
	r := recognitionReport{FailureReason: "ADB offline"}
	bundle, err := saveRecognitionBundle(root, 1, nil, map[string]any{"report": r}, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(bundle.Directory, "screenshot.png")); !os.IsNotExist(err) {
		t.Fatal("fake image present")
	}
	if !bytes.Contains([]byte(bundle.Summary), []byte("ADB offline")) {
		t.Fatal("failure lost")
	}
}
func TestRuleEditRevisionAndExportCannotOverwriteDataDirectory(t *testing.T) {
	root := t.TempDir()
	p := localMain(mainFrame(40))
	if err := saveRuleLibrary(root, 1, p, false); err != nil {
		t.Fatal(err)
	}
	first, _ := loadLocalProfile(root, 1)
	page := first.Pages["game"]
	page.Threshold = 9
	first.Pages["game"] = page
	if err := saveRuleLibrary(root, 1, first, false); err != nil {
		t.Fatal(err)
	}
	second, _ := loadLocalProfile(root, 1)
	if second.Pages["game"].Revision != first.Pages["game"].Revision+1 {
		t.Fatal("rule edit did not increment revision")
	}
	if validateExportDestination(root, filepath.Join(root, "config.json")) == nil {
		t.Fatal("export can overwrite app data")
	}
	if err := validateExportDestination(root, filepath.Join(t.TempDir(), "report.zip")); err != nil {
		t.Fatal(err)
	}
}
