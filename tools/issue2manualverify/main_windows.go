//go:build windows

// CI-only verification of the user's double-click + Yes recovery procedure.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
	"github.com/ab393911/MHX-Studio-Updates/internal/update"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

var baselineHashes = map[string]string{
	"3.5.0-preview.40":          "e58eaef41b4e37361f79cc500872bc89f1e7ebe37d810785740b294eb397daa2",
	"3.5.0-preview.41-issue2.1": "b7ac42b0800b96c4dd60a4245e31ebf054373675ae23702af4bb7ba978bf3cb7",
	"3.5.0-preview.41-issue2.2": "28a5f525f9df3c04d53818dc561cde21765c3850f7f3c5aea9182566daebd9a7",
}

var user = syscall.NewLazyDLL("user32.dll")

func must(e error) {
	if e != nil {
		panic(e)
	}
}
func utf(s string) *uint16 { v, e := syscall.UTF16PtrFromString(s); must(e); return v }
func window(title string) uintptr {
	h, _, _ := user.NewProc("FindWindowW").Call(0, uintptr(unsafe.Pointer(utf(title))))
	return h
}
func waitWindow(title string) uintptr {
	end := time.Now().Add(25 * time.Second)
	for time.Now().Before(end) {
		if h := window(title); h != 0 {
			return h
		}
		time.Sleep(100 * time.Millisecond)
	}
	panic("GUI not found: " + title)
}
func wait(cmd *exec.Cmd) {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case e := <-done:
		must(e)
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		panic("recovery GUI timeout")
	}
}
func main() {
	old := flag.String("old", "", "")
	oldVersion := flag.String("old-version", "3.5.0-preview.40", "")
	out := flag.String("out", "manual-recovery.json", "")
	flag.Parse()
	defer func() {
		if r := recover(); r != nil {
			_ = filesafe.JSON(*out, map[string]any{"passed": false, "error": fmt.Sprint(r)})
			fmt.Fprintln(os.Stderr, r)
			os.Exit(1)
		}
	}()
	baselineSHA, ok := baselineHashes[*oldVersion]
	if !ok {
		panic("unsupported immutable baseline")
	}
	must(update.Verify(*old, baselineSHA))
	root, e := os.MkdirTemp("", "MHX manual recovery GUI ")
	must(e)
	defer os.RemoveAll(root)
	must(os.Setenv("LOCALAPPDATA", filepath.Join(root, "AppData")))
	data := filepath.Join(root, "AppData", "MHXStudioNext")
	must(os.MkdirAll(data, 0700))
	original := []byte("{\"retries\":7,\"unknown_existing\":true}\n")
	must(os.WriteFile(filepath.Join(data, "config.json"), original, 0600))
	s, e := studio.Open(data)
	must(e)
	order, e := s.CreateOrder("GUI recovery sentinel", 90000)
	must(e)
	must(s.Close())
	target := filepath.Join(root, "Program", "MHXStudio.exe")
	stage := filepath.Join(root, "Program", ".mhx-updates", "tx-gui", "package.exe")
	must(os.MkdirAll(filepath.Dir(stage), 0700))
	must(filesafe.CopyNew(*old, target))
	must(filesafe.CopyNew(*old, stage))
	p, e := update.NewPlan(target, stage, "3.5.0-preview.41-issue2.3", baselineSHA)
	must(e)
	p, e = update.WithConfig(p, data)
	must(e)
	helper := filepath.Join(p.Dir(), "updater.exe")
	must(filesafe.CopyNew(*old, helper))
	// Persistent state already proved reachable by the real-helper termination probe.
	must(update.BackupConfig(data, p.ConfigBackup, p.Token))
	must(os.Rename(target, p.Backup))
	cmd := exec.Command(helper) // no arguments: same entry point as Explorer double-click
	must(cmd.Start())
	dialog := waitWindow("MHX Studio 恢复")
	yes, _, _ := user.NewProc("GetDlgItem").Call(dialog, 6)
	if yes == 0 {
		panic("Yes control missing")
	}
	user.NewProc("SendMessageW").Call(yes, 0xF5, 0, 0)
	wait(cmd)
	gui := waitWindow("知夏 · MHX Studio " + *oldVersion)
	visible, _, _ := user.NewProc("IsWindowVisible").Call(gui)
	if visible == 0 {
		panic("restored GUI hidden")
	}
	must(update.Verify(target, baselineSHA))
	must(update.Verify(p.Backup, baselineSHA))
	must(update.Verify(helper, baselineSHA))
	user.NewProc("PostMessageW").Call(gui, 0x10, 0, 0)
	end := time.Now().Add(15 * time.Second)
	for window("知夏 · MHX Studio "+*oldVersion) != 0 {
		if time.Now().After(end) {
			panic("GUI failed to close")
		}
		time.Sleep(100 * time.Millisecond)
	}
	got, e := os.ReadFile(filepath.Join(data, "config.json"))
	must(e)
	if !bytes.Equal(got, original) {
		panic("config bytes changed")
	}
	s, e = studio.Open(data)
	must(e)
	kept, e := s.Order(order.ID)
	must(e)
	if kept.Total != 90000 {
		panic("order not restored")
	}
	must(s.Close())
	must(filesafe.JSON(*out, map[string]any{"passed": true, "native_windows": true, "baseline_version": *oldVersion, "baseline_sha256": baselineSHA, "actual_published_preview40": *oldVersion == "3.5.0-preview.40", "actual_published_preview41_issue2": *oldVersion == "3.5.0-preview.41-issue2.1", "actual_published_preview41_issue2_2": *oldVersion == "3.5.0-preview.41-issue2.2", "explorer_equivalent_no_arguments": true, "confirmation_clicked": true, "no_cmd_bat_python_or_manual_exe_copy": true, "old_exe_restored": true, "old_backup_retained": true, "config_bytes_preserved": true, "sqlite_order_preserved": true, "restored_gui_visible": true, "temporary_ci_data_only": true, "custom_data_directory_case_tested": false}))
}
