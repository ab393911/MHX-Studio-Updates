package studio

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ab393911/MHX-Studio-Updates/internal/update"
)

// The same snapshot/restore code used by the independent updater must restore
// the original v1 database, not an already-migrated replacement.
func TestUpdaterSnapshotRestoresLegacyScopeAndAllMoneyAfterMigration(t *testing.T) {
	p := legacyFullPlan("update-original")
	s := legacyFixture(t, t.TempDir(), p)
	_, _, proof := seedMoney(t, s)
	root := s.Root
	beforeTasks := oldTaskSnapshot(t, s)
	beforeMoney := moneySnapshot(t, s)
	beforeProof, _ := os.ReadFile(proof)
	s.Close()
	originalHash, e := update.Hash(filepath.Join(root, FileName))
	if e != nil {
		t.Fatal(e)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	token := strings.Repeat("a", 48)
	if e = update.BackupConfig(root, snapshot, token); e != nil {
		t.Fatal(e)
	}
	if e = MigrateExisting(root); e != nil {
		t.Fatal(e)
	}
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	if len(sMustPlan(t, s, p.ID).Modules) != 8 {
		t.Fatal("new scope not applied")
	}
	_, _, _ = s.db.query("UPDATE tasks SET reason='failed new version' WHERE enabled=1")
	s.Close()
	if e = update.RestoreConfig(snapshot, root, token); e != nil {
		t.Fatal(e)
	}
	restoredHash, e := update.Hash(filepath.Join(root, FileName))
	if e != nil || restoredHash != originalHash {
		t.Fatal("original DB bytes not restored", e)
	}
	d, e := openNative(filepath.Join(root, FileName), true)
	if e != nil {
		t.Fatal(e)
	}
	old := &Store{db: d, Root: root}
	if rows(t, old, "SELECT version FROM schema_info")[0][0] != "1" || !reflect.DeepEqual(beforeTasks, oldTaskSnapshot(t, old)) || !reflect.DeepEqual(beforeMoney, moneySnapshot(t, old)) {
		t.Fatal("legacy progress/financial data not restored")
	}
	d.close()
	old.db = nil
	b, e := os.ReadFile(proof)
	if e != nil || string(b) != string(beforeProof) {
		t.Fatal("proof changed by updater", e)
	}
	failed, _ := filepath.Glob(filepath.Join(root, ".mhx-rollback", "*", FileName))
	if len(failed) != 1 {
		t.Fatal("failed migrated DB not retained", failed)
	}
	if e = VerifyFile(root); e != nil {
		t.Fatal("legacy DB cannot pass rollback health", e)
	}
	if e = MigrateExisting(root); e != nil {
		t.Fatal("retry migration after rollback", e)
	}
}
