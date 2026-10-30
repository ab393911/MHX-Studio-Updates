//go:build windows

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"
	"unsafe"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
	"github.com/ab393911/MHX-Studio-Updates/internal/update"
)

var user = syscall.NewLazyDLL("user32.dll")

func fail(e error) {
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func copyFile(a, b string) { fail(os.MkdirAll(filepath.Dir(b), 0700)); fail(filesafe.CopyNew(a, b)) }
func start(path string, args ...string) *exec.Cmd {
	cmd := exec.Command(path, args...)
	fail(cmd.Start())
	return cmd
}
func wait(cmd *exec.Cmd, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case e := <-done:
		return e
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		return errors.New("Windows EXE timed out")
	}
}
func findWindow(title string) uintptr {
	p, _ := syscall.UTF16PtrFromString(title)
	h, _, _ := user.NewProc("FindWindowW").Call(0, uintptr(unsafe.Pointer(p)))
	return h
}
func closeTitle(title string) {
	h := findWindow(title)
	if h != 0 {
		user.NewProc("PostMessageW").Call(h, 0x10, 0, 0)
		end := time.Now().Add(8 * time.Second)
		for time.Now().Before(end) {
			if dlg := findWindow("MHX Studio"); dlg != 0 {
				var owner, windowPID uint32
				user.NewProc("GetWindowThreadProcessId").Call(h, uintptr(unsafe.Pointer(&windowPID)))
				user.NewProc("GetWindowThreadProcessId").Call(dlg, uintptr(unsafe.Pointer(&owner)))
				if owner != 0 && owner == windowPID {
					if yes := control(dlg, 6); yes != 0 {
						user.NewProc("SendMessageW").Call(yes, 0xF5, 0, 0)
					}
				}
			}
			if findWindow(title) == 0 {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		fail(fmt.Errorf("window did not close: %s", title))
	}
}
func waitWindow(title string) error {
	end := time.Now().Add(15 * time.Second)
	for time.Now().Before(end) {
		if findWindow(title) != 0 {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("GUI not found: %s", title)
}
func main() {
	debug.SetTraceback("all")
	watchdog := time.AfterFunc(8*time.Minute, func() { panic("native verifier exceeded 8 minutes; refusing an unbounded or incomplete pass") })
	defer watchdog.Stop()
	old := flag.String("old", "", "")
	published19 := flag.String("published19", "", "")
	published21 := flag.String("published21", "", "")
	newExe := flag.String("new", "", "")
	exitExe := flag.String("exit-fixture", "", "")
	emulatorFixture := flag.String("emulator-fixture", "", "")
	version := flag.String("version", "", "")
	out := flag.String("out", "dist/windows-tests.json", "")
	live := flag.String("live-update", "", "")
	oldVersion := flag.String("old-version", "3.4.999", "")
	oneClick := flag.Bool("expect-one-click", false, "")
	flag.Parse()
	if *live != "" {
		if *old == "" || *version == "" {
			fail(errors.New("old EXE and expected version required"))
		}
		liveUpdate(*old, *oldVersion, *live, *version, *out, *emulatorFixture, *oneClick)
		return
	}
	if *old == "" || *newExe == "" || *version == "" {
		fail(errors.New("old/new EXEs and new version required"))
	}
	root, e := os.MkdirTemp("", "MHX Windows 中文 空格 ")
	fail(e)
	defer os.RemoveAll(root)
	// All GUI and update tests use a fresh Windows data directory.
	data := filepath.Join(root, "AppData")
	fail(os.Setenv("LOCALAPPDATA", data))
	fail(os.MkdirAll(filepath.Join(data, "MHXStudioNext"), 0700))
	sentinel := []byte("{\"retries\":7,\"future_unknown_field\":{\"keep\":true}}\n")
	cfg := filepath.Join(data, "MHXStudioNext", "config.json")
	fail(os.WriteFile(cfg, sentinel, 0600))
	smoke := filepath.Join(root, "smoke.json")
	cmd := start(*newExe, "--smoke-test", smoke)
	fail(wait(cmd, 20*time.Second))
	var sm map[string]any
	fail(filesafe.ReadJSON(smoke, &sm))
	if sm["window_created"] != true || sm["message_loop"] != true {
		fail(errors.New("native GUI smoke test failed"))
	}
	if sm["window_visible"] != true {
		fail(errors.New("native GUI is hidden"))
	}
	results := map[string]any{"native_windows": true, "arch": "amd64", "gui_smoke": sm, "windows_game_tested": false, "version": *version}
	if *emulatorFixture != "" {
		results["emulator_gui_fixture"] = emulatorGUI(*newExe, *emulatorFixture, *version, filepath.Dir(*out))
		results["adb_fallback_gui_fixture"] = adbFallbackGUI(*newExe, *emulatorFixture, *version, filepath.Dir(*out))
		results["single_game_gui_fixture"] = gameGUI(*newExe, *emulatorFixture, *version, filepath.Dir(*out))
		results["studio_gui_fixture"] = studioGUI(*newExe, *emulatorFixture, *version, filepath.Dir(*out))
		results["group_login_gui_fixture"] = groupLoginGUI(*newExe, *emulatorFixture, *version, filepath.Dir(*out))
	}
	scenarios := []string{"success", "startup_timeout", "sha_tamper", "sharing_violation"}
	ledger, e := studio.Open(filepath.Dir(cfg))
	fail(e)
	sentinelOrder, e := ledger.CreateOrder("native-preserved-buyer", 40000)
	fail(e)
	fail(ledger.Close())
	if *exitExe != "" {
		scenarios = append(scenarios, "immediate_exit", "config_migration_exit")
	}
	for _, scenario := range scenarios {
		fmt.Println("Native Windows update scenario:", scenario)
		folder := filepath.Join(root, scenario)
		fail(os.MkdirAll(folder, 0700))
		target := filepath.Join(folder, "MHXStudio.exe")
		copyFile(*old, target)
		tx := filepath.Join(folder, ".mhx-updates", "tx-native")
		fail(os.MkdirAll(tx, 0700))
		stage := filepath.Join(tx, "package.exe")
		if scenario == "immediate_exit" || scenario == "config_migration_exit" {
			copyFile(*exitExe, stage)
		} else {
			copyFile(*newExe, stage)
		}
		hash, e := update.Hash(stage)
		fail(e)
		p, e := update.NewPlan(target, stage, *version, hash)
		fail(e)
		p, e = update.WithConfig(p, filepath.Dir(cfg))
		fail(e)
		parentReport := filepath.Join(folder, "parent-smoke.json")
		parent := start(target, "--smoke-test", parentReport)
		p.ParentPID = parent.Process.Pid
		if scenario == "startup_timeout" {
			p.Version = "99.0.0"
		}
		fail(filesafe.JSON(filepath.Join(tx, "plan.json"), p))
		helper := filepath.Join(tx, "updater.exe")
		copyFile(*old, helper)
		var lock syscall.Handle
		if scenario == "sharing_violation" {
			name, _ := syscall.UTF16PtrFromString(target)
			lock, e = syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, 0, 0)
			fail(e)
		}
		if scenario == "sha_tamper" {
			f, e := os.OpenFile(stage, os.O_APPEND|os.O_WRONLY, 0600)
			fail(e)
			_, e = f.Write([]byte("tampered"))
			fail(e)
			fail(f.Close())
		}
		if scenario == "config_migration_exit" {
			fail(os.Setenv("MHX_TEST_MUTATE_CONFIG", "1"))
		}
		helperCmd := start(helper, "--apply-update", filepath.Join(tx, "plan.json"))
		fail(os.Unsetenv("MHX_TEST_MUTATE_CONFIG"))
		fail(wait(parent, 20*time.Second))
		helperErr := wait(helperCmd, 50*time.Second)
		if lock != 0 {
			syscall.CloseHandle(lock)
		}
		var result update.Result
		fail(filesafe.ReadJSON(filepath.Join(tx, "result.json"), &result))
		switch scenario {
		case "success":
			if helperErr != nil || result.Status != "committed" {
				fail(fmt.Errorf("commit failed: %v %+v", helperErr, result))
			}
			fail(update.Verify(target, p.NewSHA))
			fail(update.Verify(p.Backup, p.OldSHA))
			fail(waitWindow("知夏 · MHX Studio " + *version))
			closeTitle("知夏 · MHX Studio " + *version)
		case "startup_timeout", "immediate_exit", "config_migration_exit":
			if result.Status != "rolled_back" {
				fail(fmt.Errorf("rollback failed: %+v", result))
			}
			fail(update.Verify(target, p.OldSHA))
			fail(update.Verify(p.Backup, p.OldSHA))
			fail(waitWindow("知夏 · MHX Studio 3.4.999"))
			closeTitle("知夏 · MHX Studio 3.4.999")
		default:
			if helperErr == nil || result.Status != "unchanged" {
				fail(fmt.Errorf("preflight protection failed: %+v %v", result, helperErr))
			}
			fail(update.Verify(target, p.OldSHA))
			fail(waitWindow("知夏 · MHX Studio 3.4.999"))
			closeTitle("知夏 · MHX Studio 3.4.999")
		}
		b, e := os.ReadFile(cfg)
		fail(e)
		if string(b) != string(sentinel) {
			fail(errors.New("configuration bytes changed during update"))
		}
		configBacked := false
		if scenario != "sha_tamper" {
			_, e = update.ReadConfigBackup(p.ConfigBackup, p.DataRoot, p.Token)
			fail(e)
			configBacked = true
		}
		if scenario == "config_migration_exit" {
			if _, e = os.Stat(filepath.Join(filepath.Dir(cfg), "profiles", "partial-new.json")); !os.IsNotExist(e) {
				fail(errors.New("failed migration remains active"))
			}
		}
		ledger, e = studio.Open(filepath.Dir(cfg))
		fail(e)
		kept, e := ledger.Order(sentinelOrder.ID)
		fail(e)
		if kept.Total != 40000 || kept.Confirmed != 0 || kept.Held != 0 {
			fail(errors.New("progress/order database changed during native update/rollback"))
		}
		fail(ledger.Close())
		results[scenario] = map[string]any{"passed": true, "status": result.Status, "config_unchanged": true, "configuration_snapshot_verified": configBacked, "backup_retained": scenario == "success" || scenario == "startup_timeout" || scenario == "immediate_exit" || scenario == "config_migration_exit"}
		results[scenario].(map[string]any)["studio_order_database_preserved"] = true
	}
	if *published19 != "" {
		folder := filepath.Join(filepath.Dir(*out), "preview19-direct")
		fail(os.MkdirAll(folder, 0700))
		results["preview19_direct_gui_upgrade"] = localGUIUpgrade(*published19, "3.5.0-preview.19", *newExe, *version, *emulatorFixture, filepath.Join(folder, "upgrade.json"))
	}
	if *published21 != "" {
		folder := filepath.Join(filepath.Dir(*out), "preview21-migration")
		fail(os.MkdirAll(folder, 0700))
		results["preview21_scope_gui_upgrade"] = localGUIUpgrade(*published21, "3.5.0-preview.21", *newExe, *version, *emulatorFixture, filepath.Join(folder, "upgrade.json"))
	}
	// Also prove settings saves preserve unknown fields (unit tests cover detail).
	results["passed"] = true
	results["completed_at"] = time.Now().UTC().Format(time.RFC3339)
	fail(filesafe.JSON(*out, results))
	fmt.Println("Native Windows GUI/update checks passed; LDPlayer/game tests remain pending.")
}
