package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStandaloneDataIsolationAndExistingDataUntouched(t *testing.T) {
	old := isolatedTestID
	defer func() { isolatedTestID = old }()
	base := t.TempDir()
	existing := filepath.Join(base, "MHXStudioNext")
	if err := os.MkdirAll(existing, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "studio.sqlite"} {
		if err := os.WriteFile(filepath.Join(existing, name), []byte("existing-user-data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	isolatedTestID = "Issue2-c8411b6"
	expected := filepath.Join(base, "MHXStudioTests", isolatedTestID)
	for _, requested := range []string{"", expected} {
		got, err := dataDirectory(base, requested)
		if err != nil || got != expected {
			t.Fatal(got, err)
		}
	}
	for _, requested := range []string{existing, filepath.Join(base, "other"), filepath.Join(expected, "..", "..", "MHXStudioNext")} {
		if got, err := dataDirectory(base, requested); err == nil || got != "" {
			t.Fatal("existing/shared data accepted", got, err)
		}
	}
	for _, name := range []string{"config.json", "studio.sqlite"} {
		got, err := os.ReadFile(filepath.Join(existing, name))
		if err != nil || string(got) != "existing-user-data" {
			t.Fatal(name, err)
		}
	}
	if _, err := os.Stat(expected); !os.IsNotExist(err) {
		t.Fatal("resolver unexpectedly wrote data", err)
	}
}
func TestNormalBuildDataDirectoryBehaviorPreserved(t *testing.T) {
	old := isolatedTestID
	defer func() { isolatedTestID = old }()
	isolatedTestID = ""
	base := t.TempDir()
	got, err := dataDirectory(base, "")
	if err != nil || got != filepath.Join(base, "MHXStudioNext") {
		t.Fatal(got, err)
	}
	custom := filepath.Join(base, "custom")
	if got, err = dataDirectory(base, custom); err != nil || got != custom {
		t.Fatal(got, err)
	}
}
func TestInvalidTestBuildIdentifiersFailClosed(t *testing.T) {
	old := isolatedTestID
	defer func() { isolatedTestID = old }()
	for _, id := range []string{"..", ".", "../MHXStudioNext", `..\MHXStudioNext`, "C:shared"} {
		isolatedTestID = id
		if _, err := dataDirectory(t.TempDir(), ""); err == nil {
			t.Fatal(id)
		}
	}
}
