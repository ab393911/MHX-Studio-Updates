//go:build windows

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"
	"unsafe"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
	"github.com/ab393911/MHX-Studio-Updates/internal/update"
)

var user = syscall.NewLazyDLL("user32.dll")
var partialReport map[string]any
var partialReportPath string

func fail(e error) {
	if e != nil {
		if partialReport != nil && partialReportPath != "" {
			partialReport["passed"] = false
			partialReport["error"] = e.Error()
			_ = filesafe.JSON(partialReportPath, partialReport)
		}
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
	// Clipboard locks and GDI DCs belong to their acquiring Windows thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	debug.SetTraceback("all")
	watchdog := time.AfterFunc(8*time.Minute, func() { panic("native verifier exceeded its time limit; refusing an unbounded or incomplete pass") })
	defer watchdog.Stop()
	old := flag.String("old", "", "")
	published19 := flag.String("published19", "", "")
	published21 := flag.String("published21", "", "")
	published40 := flag.String("published40", "", "")
	published41 := flag.String("published41", "", "")
	published412 := flag.String("published-issue2-2", "", "published issue2.2 baseline for GUI upgrade")
	updateOnly := flag.Bool("update-only", false, "")
	recognitionOnly := flag.Bool("recognition-only", false, "")
	iconDraftGUI := flag.Bool("icon-draft-gui", false, "exercise the opt-in icon draft GUI on synthetic fixture only")
	screenPNG := flag.Bool("screen-png", false, "use a real Windows desktop PNG as the fixture frame")
	framePNG := flag.String("png-frame", "", "optional authorized PNG for fixture-only GUI tests")
	frameKind := flag.String("png-frame-kind", "", "redacted or private-original; no live-device acceptance")
	replayDir := flag.String("image-replay-dir", "", "replay a recorded PNG/profile in isolated test data without saving calibration")
	displayProbe := flag.Bool("display-probe", false, "verify actual screen rendering, never force PrintWindow")
	interrupt := flag.String("interrupt-update", "", "")
	newExe := flag.String("new", "", "")
	exitExe := flag.String("exit-fixture", "", "")
	emulatorFixture := flag.String("emulator-fixture", "", "")
	version := flag.String("version", "", "")
	out := flag.String("out", "dist/windows-tests.json", "")
	live := flag.String("live-update", "", "")
	oldVersion := flag.String("old-version", "3.4.999", "")
	oneClick := flag.Bool("expect-one-click", false, "")
	transportFailures := flag.Bool("transport-failures-only", false, "test installed updater rejection of failed/corrupt downloads")
	flag.Parse()
	if *transportFailures {
		if *old == "" || *newExe == "" || *version == "" || *emulatorFixture == "" {
			fail(errors.New("download rejection test requires baseline, public candidate, fixture and version"))
		}
		fail(filesafe.JSON(*out, transportFailureGUI(*old, *oldVersion, *newExe, *version, *emulatorFixture)))
		return
	}
	if *interrupt != "" {
		var p update.Plan
		fail(filesafe.ReadJSON(*interrupt, &p))
		err := (update.Installer{Runtime: interruptionRuntime{}, Rename: func(a, b string) error {
			err := os.Rename(a, b)
			if err == nil && b == p.Backup {
				os.Exit(77)
			}
			return err
		}}).Install(context.Background(), p)
		fail(fmt.Errorf("interruption point not reached: %v", err))
		return
	}
	if *recognitionOnly {
		watchdog.Reset(2 * time.Minute)
		if *newExe == "" || *emulatorFixture == "" || *version == "" {
			fail(errors.New("recognition verifier requires new EXE, fixture and version"))
		}
		sources := 0
		for _, enabled := range []bool{*screenPNG, *replayDir != "", *framePNG != ""} {
			if enabled {
				sources++
			}
		}
		if sources > 1 {
			fail(errors.New("PNG input modes are mutually exclusive"))
		}
		if *iconDraftGUI && sources != 0 {
			fail(errors.New("icon draft GUI requires the isolated synthetic calibration fixture"))
		}
		if *framePNG != "" && *frameKind != "redacted" && *frameKind != "private-original" {
			fail(errors.New("PNG frame requires an explicit source kind"))
		}
		result := recognitionGUI(*newExe, *emulatorFixture, *version, *out, *displayProbe, *screenPNG, *replayDir, *framePNG, *frameKind, *iconDraftGUI)
		fail(filesafe.JSON(*out, result))
		return
	}
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
	partialReport, partialReportPath = results, *out
	if *emulatorFixture != "" && !*updateOnly {
		results["emulator_gui_fixture"] = emulatorGUI(*newExe, *emulatorFixture, *version, filepath.Dir(*out))
		results["adb_fallback_gui_fixture"] = adbFallbackGUI(*newExe, *emulatorFixture, *version, filepath.Dir(*out))
		results["single_game_gui_fixture"] = gameGUI(*newExe, *emulatorFixture, *version, filepath.Dir(*out))
		results["studio_gui_fixture"] = studioGUI(*newExe, *emulatorFixture, *version, filepath.Dir(*out))
		results["group_login_gui_fixture"] = groupLoginGUI(*newExe, *emulatorFixture, *version, filepath.Dir(*out))
	}
	scenarios := []string{"success", "startup_timeout", "sha_tamper", "sharing_violation", "interrupted_recovery"}
	ledger, e := studio.Open(filepath.Dir(cfg))
	fail(e)
	sentinelOrder, e := ledger.CreateOrder("native-preserved-buyer", 40000)
	fail(e)
	fail(ledger.Close())
	if *exitExe != "" {
		scenarios = append(scenarios, "immediate_exit", "config_migration_exit")
	}
	for _, scenario := range scenarios {
		results["active_scenario"] = scenario
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
		var helperErr error
		if scenario == "interrupted_recovery" {
			fail(wait(parent, 20*time.Second))
			self, e := os.Executable()
			fail(e)
			interrupted := start(self, "--interrupt-update", filepath.Join(tx, "plan.json"))
			if err := wait(interrupted, 20*time.Second); err == nil || interrupted.ProcessState == nil || interrupted.ProcessState.ExitCode() != 77 {
				fail(fmt.Errorf("did not exit at injected interruption: %v", err))
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				fail(errors.New("interruption did not leave missing EXE"))
			}
			fail(update.Verify(p.Backup, p.OldSHA))
			// Observe the terminated helper's persistent state before the harness
			// explicitly restarts it. This is recoverability, not unattended rollback.
			time.Sleep(2 * time.Second)
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				fail(errors.New("unexpected automatic recovery; reassess interruption classification"))
			}
			helperErr = wait(start(helper, "--apply-update", filepath.Join(tx, "plan.json")), 50*time.Second)
		} else {
			helperCmd := start(helper, "--apply-update", filepath.Join(tx, "plan.json"))
			fail(os.Unsetenv("MHX_TEST_MUTATE_CONFIG"))
			fail(wait(parent, 20*time.Second))
			helperErr = wait(helperCmd, 50*time.Second)
		}
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
		case "startup_timeout", "immediate_exit", "config_migration_exit", "interrupted_recovery":
			if result.Status != "rolled_back" {
				fail(fmt.Errorf("rollback failed: %+v", result))
			}
			fail(update.Verify(target, p.OldSHA))
			fail(update.Verify(p.Backup, p.OldSHA))
			fail(waitWindow("知夏 · MHX Studio " + *oldVersion))
			closeTitle("知夏 · MHX Studio " + *oldVersion)
		default:
			if helperErr == nil || result.Status != "unchanged" {
				fail(fmt.Errorf("preflight protection failed: %+v %v", result, helperErr))
			}
			fail(update.Verify(target, p.OldSHA))
			fail(waitWindow("知夏 · MHX Studio " + *oldVersion))
			closeTitle("知夏 · MHX Studio " + *oldVersion)
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
		results[scenario] = map[string]any{"passed": true, "status": result.Status, "config_unchanged": true, "configuration_snapshot_verified": configBacked, "backup_retained": scenario == "success" || scenario == "startup_timeout" || scenario == "immediate_exit" || scenario == "config_migration_exit" || scenario == "interrupted_recovery"}
		results[scenario].(map[string]any)["studio_order_database_preserved"] = true
		if scenario == "interrupted_recovery" {
			results[scenario].(map[string]any)["recovery_helper_restarted_by_test"] = true
			results[scenario].(map[string]any)["automatic_without_restarting_helper"] = false
		}
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
	if *published40 != "" {
		results["active_scenario"] = "preview40_direct_gui_upgrade"
		folder := filepath.Join(filepath.Dir(*out), "preview40-direct")
		fail(os.MkdirAll(folder, 0700))
		results["preview40_direct_gui_upgrade"] = localGUIUpgrade(*published40, "3.5.0-preview.40", *newExe, *version, *emulatorFixture, filepath.Join(folder, "upgrade.json"))
	}
	if *published41 != "" {
		results["active_scenario"] = "preview41_issue2_gui_upgrade"
		folder := filepath.Join(filepath.Dir(*out), "preview41-issue2")
		fail(os.MkdirAll(folder, 0700))
		results["preview41_issue2_gui_upgrade"] = localGUIUpgrade(*published41, "3.5.0-preview.41-issue2.1", *newExe, *version, *emulatorFixture, filepath.Join(folder, "upgrade.json"))
	}
	if *published412 != "" {
		results["active_scenario"] = "preview41_issue2_2_gui_upgrade"
		folder := filepath.Join(filepath.Dir(*out), "preview41-issue2-2")
		fail(os.MkdirAll(folder, 0700))
		results["preview41_issue2_2_gui_upgrade"] = localGUIUpgrade(*published412, "3.5.0-preview.41-issue2.2", *newExe, *version, *emulatorFixture, filepath.Join(folder, "upgrade.json"))
	}
	// Also prove settings saves preserve unknown fields (unit tests cover detail).
	results["publication_ready"] = false
	results["publication_blocker"] = *oldVersion + " helper termination between renames leaves target absent; recovery requires restarting retained helper, not unattended rollback"
	results["passed"] = true
	results["completed_at"] = time.Now().UTC().Format(time.RFC3339)
	fail(filesafe.JSON(*out, results))
	fmt.Println("Native Windows GUI/update checks passed; LDPlayer/game tests remain pending.")
}

// Test-only process: the parent EXE was already waited for by the harness.
// The injected rename exits before Launch, exactly after the retained backup.
type interruptionRuntime struct{}

func (interruptionRuntime) WaitParent(context.Context, int) error { return nil }
func (interruptionRuntime) Launch(string, ...string) (update.Child, error) {
	return nil, errors.New("unexpected launch before interruption")
}
