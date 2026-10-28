package update

import (
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStudioOrderDatabaseIsBackedUpRestoredAndFailedCopyQuarantined(t *testing.T) {
	root := t.TempDir()
	s, e := studio.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder("buyer", 30000)
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	dest := filepath.Join(t.TempDir(), "snapshot")
	token := strings.Repeat("a", 48)
	if e = BackupConfig(root, dest, token); e != nil {
		t.Fatal(e)
	}
	m, e := ReadConfigBackup(dest, root, token)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, f := range m.Files {
		if f.Path == studio.FileName {
			found = true
		}
	}
	if !found {
		t.Fatal("database omitted from snapshot")
	}
	if e = os.WriteFile(filepath.Join(root, studio.FileName), []byte("failed migration corrupted data"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = RestoreConfig(dest, root, token); e != nil {
		t.Fatal(e)
	}
	s, e = studio.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	got, e := s.Order(o.ID)
	if e != nil || got.Total != 30000 {
		t.Fatal(got, e)
	}
	failed, _ := filepath.Glob(filepath.Join(root, ".mhx-rollback", "*", studio.FileName))
	if len(failed) != 1 {
		t.Fatal("failed DB not retained", failed)
	}
}
