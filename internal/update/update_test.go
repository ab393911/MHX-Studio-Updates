package update

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
)

func fixture(extra string) []byte {
	b := make([]byte, 2048)
	copy(b, "MZ")
	binary.LittleEndian.PutUint32(b[60:], 128)
	copy(b[128:], "PE\x00\x00")
	h := b[132:]
	binary.LittleEndian.PutUint16(h, 0x8664)
	binary.LittleEndian.PutUint16(h[2:], 1)
	binary.LittleEndian.PutUint16(h[16:], 240)
	o := h[20:]
	binary.LittleEndian.PutUint16(o, 0x20b)
	binary.LittleEndian.PutUint32(o[108:], 16)
	s := h[260:]
	copy(s, ".text")
	binary.LittleEndian.PutUint32(s[16:], 512)
	binary.LittleEndian.PutUint32(s[20:], 512)
	copy(b[1024:], extra)
	return b
}
func write(t *testing.T, path string, b []byte) {
	t.Helper()
	if e := os.WriteFile(path, b, 0700); e != nil {
		t.Fatal(e)
	}
}
func planFixture(t *testing.T) (Plan, []byte) {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "MHXStudio.exe")
	old := fixture("old version")
	write(t, target, old)
	tx := filepath.Join(dir, ".mhx-updates", "tx-test")
	if e := os.MkdirAll(tx, 0700); e != nil {
		t.Fatal(e)
	}
	stage := filepath.Join(tx, "package.exe")
	write(t, stage, fixture("new version"))
	hash, _ := Hash(stage)
	p, e := NewPlan(target, stage, "3.5.0-preview.2", hash)
	if e != nil {
		t.Fatal(e)
	}
	return p, old
}

type fakeChild struct {
	stopped bool
	exited  <-chan struct{}
}

func (c *fakeChild) Stop() error             { c.stopped = true; return nil }
func (c *fakeChild) Exited() <-chan struct{} { return c.exited }

type fakeRuntime struct {
	p          Plan
	health     bool
	launchFail bool
	parentFail bool
	earlyExit  bool
	launches   int
	child      fakeChild
}

func (r *fakeRuntime) WaitParent(context.Context, int) error {
	if r.parentFail {
		return errors.New("parent still running")
	}
	return nil
}
func (r *fakeRuntime) Launch(_ string, args ...string) (Child, error) {
	r.launches++
	if r.earlyExit && r.launches == 1 {
		ch := make(chan struct{})
		close(ch)
		r.child.exited = ch
	}
	if r.launchFail && r.launches == 1 {
		return nil, errors.New("launch denied")
	}
	if r.health && r.launches == 1 {
		filesafe.JSON(args[1], Receipt{r.p.Version, r.p.Token, r.p.NewSHA})
	}
	return &r.child, nil
}
func TestSemver(t *testing.T) {
	for _, x := range []struct {
		a, b string
		cmp  int
	}{{"3.5.0-preview.10", "3.5.0-preview.9", 1}, {"3.5.0", "3.5.0-preview.99", 1}, {"3.10.0", "3.9.9", 1}, {"3.5.0", "3.5.0", 0}, {"3.5.0-a", "3.5.0-b", -1}} {
		c, e := Compare(x.a, x.b)
		if e != nil || c != x.cmp {
			t.Fatalf("%+v => %d %v", x, c, e)
		}
	}
	for _, v := range []string{"3.5", "latest", "3.5.0-01", "03.5.0"} {
		if _, e := Compare(v, "3.5.0"); e == nil {
			t.Fatalf("accepted %s", v)
		}
	}
}
func TestDownloadAndHashRejection(t *testing.T) {
	b := fixture("network")
	dir := t.TempDir()
	p := filepath.Join(dir, "source.exe")
	write(t, p, b)
	hash, _ := Hash(p)
	var m Manifest
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/update.json" {
			json.NewEncoder(w).Encode(m)
			return
		}
		w.Write(b)
	}))
	defer server.Close()
	m = Manifest{Schema: 1, Version: "3.5.0", Platform: "windows-amd64", URL: server.URL + "/file.exe", SHA256: hash, Size: int64(len(b))}
	checked, newer, e := Check(context.Background(), server.Client(), server.URL+"/update.json", "3.4.0")
	if e != nil || !newer {
		t.Fatal(e)
	}
	dl := t.TempDir()
	if _, e = Download(context.Background(), server.Client(), checked, dl); e != nil {
		t.Fatal(e)
	}
	m.SHA256 = strings.Repeat("0", 64)
	dl = t.TempDir()
	if _, e = Download(context.Background(), server.Client(), m, dl); e == nil {
		t.Fatal("bad hash accepted")
	}
	files, _ := os.ReadDir(dl)
	if len(files) != 0 {
		t.Fatal("failed temporary download retained")
	}
	m.SHA256 = hash
	m.Size += 1
	if _, e = Download(context.Background(), server.Client(), m, t.TempDir()); e == nil {
		t.Fatal("truncated download accepted")
	}
}
func TestRejectBadManifestAndHTTPRedirect(t *testing.T) {
	for _, raw := range []string{"http://a/b", "https://user:pass@a/b", "file:///tmp/a", "https://a/b#x"} {
		if HTTPS(raw) == nil {
			t.Fatal(raw)
		}
	}
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://example.invalid/file", 302) }))
	defer s.Close()
	c := s.Client()
	c.CheckRedirect = Client().CheckRedirect
	if _, _, e := Check(context.Background(), c, s.URL, "3.0.0"); e == nil {
		t.Fatal("redirect downgrade accepted")
	}
}
func TestInstallCommitPreservesConfigAndAllBackups(t *testing.T) {
	p, old := planFixture(t)
	cfg := filepath.Join(filepath.Dir(p.Target), "config.json")
	sentinel := []byte(`{"future_unknown_field":123,"retry":7}`)
	write(t, cfg, sentinel)
	older := filepath.Join(filepath.Dir(p.Backup), "older.exe")
	write(t, older, old)
	rt := &fakeRuntime{p: p, health: true}
	if e := (Installer{Runtime: rt}).Install(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{p.Backup, older} {
		if e := Verify(path, p.OldSHA); e != nil {
			t.Fatal(e)
		}
	}
	if e := Verify(p.Target, p.NewSHA); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(cfg)
	if string(b) != string(sentinel) {
		t.Fatal("config changed")
	}
	var result Result
	filesafe.ReadJSON(filepath.Join(p.Dir(), "result.json"), &result)
	if result.Status != "committed" {
		t.Fatal(result)
	}
}
func TestInstallRollbackScenarios(t *testing.T) {
	for _, kind := range []string{"replace", "launch", "health"} {
		t.Run(kind, func(t *testing.T) {
			p, _ := planFixture(t)
			rt := &fakeRuntime{p: p, launchFail: kind == "launch"}
			i := Installer{Runtime: rt, HealthTimeout: 15 * time.Millisecond}
			if kind == "replace" {
				i.Rename = func(a, b string) error {
					if a == p.Stage {
						return errors.New("injected disk error")
					}
					return os.Rename(a, b)
				}
			}
			if e := i.Install(context.Background(), p); e == nil {
				t.Fatal("failure not reported")
			}
			if e := Verify(p.Target, p.OldSHA); e != nil {
				t.Fatal(e)
			}
			if e := Verify(p.Backup, p.OldSHA); e != nil {
				t.Fatal(e)
			}
			var r Result
			filesafe.ReadJSON(filepath.Join(p.Dir(), "result.json"), &r)
			if r.Status != "rolled_back" {
				t.Fatal(r)
			}
			if kind == "health" && !rt.child.stopped {
				t.Fatal("failed child was not stopped")
			}
		})
	}
}
func TestNoMutationOnPreflightFailure(t *testing.T) {
	for _, kind := range []string{"parent", "hash", "backup"} {
		t.Run(kind, func(t *testing.T) {
			p, old := planFixture(t)
			rt := &fakeRuntime{p: p, parentFail: kind == "parent"}
			if kind == "hash" {
				write(t, p.Stage, fixture("tampered"))
			}
			if kind == "backup" {
				write(t, p.Backup, old)
			}
			if e := (Installer{Runtime: rt}).Install(context.Background(), p); e == nil {
				t.Fatal("failure accepted")
			}
			if e := Verify(p.Target, p.OldSHA); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestMissingTargetRecovery(t *testing.T) {
	p, _ := planFixture(t)
	if e := os.Rename(p.Target, p.Backup); e != nil {
		t.Fatal(e)
	}
	i := Installer{}
	if e := i.Recover(p); e != nil {
		t.Fatal(e)
	}
	if e := Verify(p.Target, p.OldSHA); e != nil {
		t.Fatal(e)
	}
	if e := Verify(p.Backup, p.OldSHA); e != nil {
		t.Fatal(e)
	}
	if e := i.Recover(p); e == nil {
		t.Fatal("recovery overwrote existing target")
	}
}

func TestImmediateProcessExitRollsBackWithoutTryingToKillDeadProcess(t *testing.T) {
	p, _ := planFixture(t)
	rt := &fakeRuntime{p: p, earlyExit: true}
	if e := (Installer{Runtime: rt, HealthTimeout: time.Hour}).Install(context.Background(), p); e == nil || !strings.Contains(e.Error(), "exited before") {
		t.Fatal(e)
	}
	if rt.child.stopped {
		t.Fatal("attempted to kill a process that had already exited")
	}
	if e := Verify(p.Target, p.OldSHA); e != nil {
		t.Fatal(e)
	}
	if e := Verify(p.Backup, p.OldSHA); e != nil {
		t.Fatal(e)
	}
	var r Result
	filesafe.ReadJSON(filepath.Join(p.Dir(), "result.json"), &r)
	if r.Status != "rolled_back" {
		t.Fatal(r)
	}
}
