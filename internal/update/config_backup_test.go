package update

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
)

func configFixture(t *testing.T) (Plan, []byte) {
	t.Helper()
	p, _ := planFixture(t)
	root := filepath.Join(filepath.Dir(p.Target), "用户配置")
	if e := os.MkdirAll(filepath.Join(root, "profiles"), 0700); e != nil {
		t.Fatal(e)
	}
	b := []byte("{\"future_unknown\":{\"keep\":true},\"retries\":7}\n")
	write(t, filepath.Join(root, "config.json"), b)
	write(t, filepath.Join(root, "ldplayer-discovery.json"), []byte("{\"directory\":\"old\"}\n"))
	write(t, filepath.Join(root, "profiles", "1.json"), []byte("{\"calibration\":\"keep\"}\n"))
	var e error
	p, e = WithConfig(p, root)
	if e != nil {
		t.Fatal(e)
	}
	return p, b
}
func TestUpdateMakesConfigurationSnapshotBeforeReplacingEXE(t *testing.T) {
	p, b := configFixture(t)
	rt := &fakeRuntime{p: p, health: true}
	i := Installer{Runtime: rt, StableFor: time.Millisecond, Rename: func(src, dst string) error {
		if src == p.Target {
			if _, e := ReadConfigBackup(p.ConfigBackup, p.DataRoot, p.Token); e != nil {
				t.Fatal("EXE was changed before configuration backup", e)
			}
		}
		return os.Rename(src, dst)
	}}
	if e := i.Install(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	backed, e := os.ReadFile(filepath.Join(p.ConfigBackup, "files", "config.json"))
	if e != nil || string(backed) != string(b) {
		t.Fatal(string(backed), e)
	}
	now, _ := os.ReadFile(filepath.Join(p.DataRoot, "config.json"))
	if string(now) != string(b) {
		t.Fatal("successful update changed configuration")
	}
}
func TestCrashAfterConfigMutationRestoresSettingsAndRetainsFailedFiles(t *testing.T) {
	p, b := configFixture(t)
	rt := &fakeRuntime{p: p, mutateConfig: true, earlyExit: true}
	if e := (Installer{Runtime: rt}).Install(context.Background(), p); e == nil {
		t.Fatal("failed migration committed")
	}
	now, e := os.ReadFile(filepath.Join(p.DataRoot, "config.json"))
	if e != nil || string(now) != string(b) {
		t.Fatal(string(now), e)
	}
	if _, e = os.Stat(filepath.Join(p.DataRoot, "profiles", "new.json")); !os.IsNotExist(e) {
		t.Fatal("partial migration remains active")
	}
	failed, _ := filepath.Glob(filepath.Join(p.DataRoot, ".mhx-rollback", "*", "profiles", "new.json"))
	if len(failed) != 1 {
		t.Fatal("failed configuration was not retained", failed)
	}
	if e = Verify(p.Backup, p.OldSHA); e != nil {
		t.Fatal(e)
	}
	var r Result
	filesafe.ReadJSON(filepath.Join(p.Dir(), "result.json"), &r)
	if r.Status != "rolled_back" || r.ConfigBackup == "" {
		t.Fatal(r)
	}
}
func TestHealthReceiptFollowedByImmediateExitStillRollsBack(t *testing.T) {
	p, b := configFixture(t)
	rt := &fakeRuntime{p: p, health: true, exitAfterReceipt: true}
	if e := (Installer{Runtime: rt}).Install(context.Background(), p); e == nil {
		t.Fatal("receipt-only crashed process committed")
	}
	if e := Verify(p.Target, p.OldSHA); e != nil {
		t.Fatal(e)
	}
	now, _ := os.ReadFile(filepath.Join(p.DataRoot, "config.json"))
	if string(now) != string(b) {
		t.Fatal("configuration changed")
	}
}
func TestTamperedSnapshotCannotOverwriteActiveConfiguration(t *testing.T) {
	p, b := configFixture(t)
	if e := BackupConfig(p.DataRoot, p.ConfigBackup, p.Token); e != nil {
		t.Fatal(e)
	}
	write(t, filepath.Join(p.ConfigBackup, "files", "config.json"), []byte("tampered"))
	if e := RestoreConfig(p.ConfigBackup, p.DataRoot, p.Token); e == nil {
		t.Fatal("tampered configuration backup accepted")
	}
	now, _ := os.ReadFile(filepath.Join(p.DataRoot, "config.json"))
	if string(now) != string(b) {
		t.Fatal("active configuration overwritten")
	}
}
func TestLegacyStartupSnapshotIsCreatedWithoutChangingConfiguration(t *testing.T) {
	p, b := configFixture(t)
	p.DataRoot = ""
	p.ConfigBackup = ""
	root := filepath.Join(filepath.Dir(p.Target), "用户配置")
	dir, e := EnsureStartupConfigBackup(p, root)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ReadConfigBackup(dir, root, p.Token); e != nil {
		t.Fatal(e)
	}
	now, _ := os.ReadFile(filepath.Join(root, "config.json"))
	if string(now) != string(b) {
		t.Fatal("legacy bootstrap changed configuration")
	}
}
