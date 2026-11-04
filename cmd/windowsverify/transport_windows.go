//go:build windows

package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
	"github.com/ab393911/MHX-Studio-Updates/internal/update"
)

// Only verification tools are rebuilt. Both product EXEs are immutable public
// downloads; injected HTTP faults operate on a disposable loopback TLS server.
func transportFailureGUI(old, oldVersion, candidate, version, fixture string) map[string]any {
	binary, err := os.ReadFile(candidate)
	fail(err)
	oldSHA, err := update.Hash(old)
	fail(err)
	newSHA := fmt.Sprintf("%x", sha256.Sum256(binary))
	var meta struct {
		Version string `json:"version"`
		Source  string `json:"source_commit"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	raw, err := exec.CommandContext(ctx, candidate, "--self-test").Output()
	fail(err)
	fail(json.Unmarshal(raw, &meta))
	if meta.Version != version {
		fail(fmt.Errorf("wrong candidate version"))
	}
	result := map[string]any{"native_windows": true, "baseline_version": oldVersion, "baseline_sha256": oldSHA, "version": version, "source_commit": meta.Source, "sha256": newSHA, "temporary_ci_data_only": true, "cases": map[string]any{}}
	for _, mode := range []string{"http_503", "truncated", "sha_corruption"} {
		fmt.Println("Installed updater rejects injected download:", mode)
		root, err := os.MkdirTemp("", "MHX failed download ")
		fail(err)
		func() {
			defer os.RemoveAll(root)
			var manifest update.Manifest
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/update.json" {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(manifest)
					return
				}
				if r.URL.Path != "/package.exe" {
					http.NotFound(w, r)
					return
				}
				if mode == "http_503" {
					http.Error(w, "injected unavailable asset", 503)
					return
				}
				w.Header().Set("Content-Length", fmt.Sprint(len(binary)))
				if mode == "truncated" {
					_, _ = w.Write(binary[:1024])
					return
				}
				broken := append([]byte(nil), binary...)
				broken[len(broken)-1] ^= 1
				_, _ = w.Write(broken)
			}))
			defer server.Close()
			manifest = update.Manifest{Schema: 1, Version: version, Platform: "windows-amd64", URL: server.URL + "/package.exe", SHA256: newSHA, Size: int64(len(binary)), SourceCommit: meta.Source}
			// Same normal system trust path as the existing local GUI upgrade test;
			// no TLS validation override is passed to either product EXE.
			cert := server.Certificate()
			thumb := sha1.Sum(cert.Raw)
			thumbText := hex.EncodeToString(thumb[:])
			certPath := filepath.Join(root, "test-root.cer")
			fail(os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0600))
			if b, e := exec.CommandContext(ctx, "certutil", "-f", "-addstore", "Root", certPath).CombinedOutput(); e != nil {
				fail(fmt.Errorf("test trust import: %w %s", e, b))
			}
			defer func() {
				clean, stop := context.WithTimeout(context.Background(), 30*time.Second)
				defer stop()
				if b, e := exec.CommandContext(clean, "certutil", "-delstore", "Root", thumbText).CombinedOutput(); e != nil {
					fail(fmt.Errorf("test trust cleanup: %w %s", e, b))
				}
			}()
			install := filepath.Join(root, "LDPlayer")
			fail(os.MkdirAll(install, 0700))
			for _, name := range []string{"ldconsole.exe", "adb.exe", "dnplayer.exe"} {
				copyFile(fixture, filepath.Join(install, name))
			}
			var inventory []map[string]any
			for n := 1; n <= 5; n++ {
				inventory = append(inventory, map[string]any{"Index": n, "Running": true})
			}
			fail(filesafe.JSON(filepath.Join(install, "emulator-fixture.json"), inventory))
			host := start(filepath.Join(install, "dnplayer.exe"), "--hold")
			defer func() { _ = host.Process.Kill(); _ = host.Wait() }()
			data := filepath.Join(root, "data")
			cfg := filepath.Join(data, "config.json")
			bindings := map[string]any{}
			jobs := map[string]any{}
			for n := 1; n <= 5; n++ {
				bindings[fmt.Sprint(n)] = map[string]any{"package": "com.fixture.confirmed", "android_id": fmt.Sprintf("%016x", n), "user_confirmed": true}
				jobs[fmt.Sprint(n)] = []string{"合欢", "天音", "青云", "鬼王", "鬼王"}
			}
			fail(filesafe.JSON(cfg, map[string]any{"manifest_url": server.URL + "/update.json", "ldplayer_directory": install, "group_start_index": 1, "retries": 0, "game_bindings": bindings, "jobs": jobs, "target_roles": map[string]int{"1": 4}, "future_unknown": "keep"}))
			preserved := map[string][]byte{}
			for n := 1; n <= 5; n++ {
				p := filepath.Join(data, "profiles", fmt.Sprint(n)+".json")
				value := []byte("{\"synthetic_rule_sentinel\":true}\n")
				fail(filesafe.Write(p, value, 0600))
				preserved[p] = value
			}
			s, err := studio.Open(data)
			fail(err)
			order, err := s.CreateOrder("failure preservation fixture", 90000)
			fail(err)
			fail(s.Close())
			target := filepath.Join(root, "Program", "MHXStudio.exe")
			copyFile(old, target)
			priorBackup := filepath.Join(filepath.Dir(target), ".mhx-backups", "previous.exe")
			copyFile(old, priorBackup)
			app := start(target, "--data-dir", data)
			title := "知夏 · MHX Studio " + oldVersion
			fail(waitWindow(title))
			h := findWindow(title)
			until(h, "baseline did not finish detection", func() bool { return strings.Contains(readText(control(h, 208)), "ADB 已验证 5 台") })
			before, err := os.ReadFile(cfg)
			fail(err)
			click(h, 110)
			want := map[string]string{"http_503": "HTTP 503", "truncated": "unexpected EOF", "sha_corruption": "download SHA-256 mismatch"}[mode]
			until(h, "failed download was not rejected", func() bool {
				log := readText(control(h, 900))
				return strings.Contains(log, "操作停止：") && strings.Contains(log, want)
			})
			if findWindow(title) == 0 {
				fail(fmt.Errorf("download failure exited installed GUI"))
			}
			fail(update.Verify(target, oldSHA))
			fail(update.Verify(priorBackup, oldSHA))
			plans, err := filepath.Glob(filepath.Join(filepath.Dir(target), ".mhx-updates", "tx-*", "plan.json"))
			fail(err)
			if len(plans) != 0 {
				fail(fmt.Errorf("failed download created install transaction"))
			}
			after, err := os.ReadFile(cfg)
			fail(err)
			if !bytes.Equal(before, after) {
				fail(fmt.Errorf("download failure changed configuration"))
			}
			closeTitle(title)
			fail(wait(app, 10*time.Second))
			for p, want := range preserved {
				got, err := os.ReadFile(p)
				fail(err)
				if !bytes.Equal(got, want) {
					fail(fmt.Errorf("download failure changed calibration"))
				}
			}
			s, err = studio.Open(data)
			fail(err)
			kept, err := s.Order(order.ID)
			fail(err)
			if kept.Total != 90000 {
				fail(fmt.Errorf("download failure changed SQLite order"))
			}
			fail(s.Close())
			result["cases"].(map[string]any)[mode] = map[string]any{"passed": true, "installed_gui_remained": true, "old_exe_unchanged": true, "config_bindings_jobs_unchanged": true, "five_calibrations_preserved": true, "sqlite_order_preserved": true, "prior_backup_preserved": true, "no_install_transaction": true}
		}()
	}
	result["passed"] = true
	return result
}
