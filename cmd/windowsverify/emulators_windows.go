//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
)

func control(h uintptr, id int) uintptr {
	v, _, _ := user.NewProc("GetDlgItem").Call(h, uintptr(id))
	return v
}
func readText(h uintptr) string {
	b := make([]uint16, 12000)
	// GetWindowTextW intentionally does not read another process's edit text.
	// WM_GETTEXT is a system-marshalled message and works for these controls.
	user.NewProc("SendMessageW").Call(h, 0xD, uintptr(len(b)), uintptr(unsafe.Pointer(&b[0])))
	end := 0
	for end < len(b) && b[end] != 0 {
		end++
	}
	return syscall.UTF16ToString(b[:end])
}
func writeText(h uintptr, s string) {
	p, e := syscall.UTF16PtrFromString(s)
	fail(e)
	user.NewProc("SendMessageW").Call(h, 0xC, 0, uintptr(unsafe.Pointer(p)))
}
func click(h uintptr, id int) {
	button := control(h, id)
	if button == 0 {
		fail(fmt.Errorf("control %d missing", id))
	}
	user.NewProc("SendMessageW").Call(button, 0xF5, 0, 0)
}
func until(h uintptr, description string, f func() bool) {
	end := time.Now().Add(35 * time.Second)
	for time.Now().Before(end) {
		if f() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	fail(fmt.Errorf("%s: status=%s; log=%s", description, readText(control(h, 208)), readText(control(h, 900))))
}

// These tests exercise the actual Windows GUI and child processes, but use a
// vendor-CLI fixture. Their result is deliberately not a real LDPlayer/game pass.
func emulatorGUI(exe, fixture, version, outDir string) map[string]any {
	root, e := os.MkdirTemp("", "MHX emulator fixture 中文 空格 ")
	fail(e)
	defer os.RemoveAll(root)
	install := filepath.Join(root, "任意安装路径", "雷电14")
	fail(os.MkdirAll(install, 0700))
	for _, name := range []string{"ldconsole.exe", "adb.exe", "dnplayer.exe"} {
		copyFile(fixture, filepath.Join(install, name))
	}
	type vm struct {
		Index            int
		Running, Offline bool
	}
	all := []vm{{6, true, false}, {7, false, false}, {8, false, false}, {9, true, true}, {10, false, false}}
	fail(filesafe.JSON(filepath.Join(install, "emulator-fixture.json"), all))
	host := start(filepath.Join(install, "dnplayer.exe"), "--hold")
	defer func() { _ = host.Process.Kill(); _ = host.Wait() }()
	data := filepath.Join(root, "fresh-data")
	app := start(exe, "--data-dir", data)
	title := "知夏 · MHX Studio " + version
	fail(waitWindow(title))
	h := findWindow(title)
	until(h, "startup did not discover custom process path and ten-column list", func() bool {
		return strings.Contains(readText(control(h, 208)), "检测到 5 台，已运行 2 台，ADB 已验证 1 台")
	})
	if _, e = os.Stat(filepath.Join(data, "config.json")); !os.IsNotExist(e) {
		fail(errors.New("automatic discovery must not write configuration"))
	}
	var remembered struct{ Directory, ADB string }
	fail(filesafe.ReadJSON(filepath.Join(data, "ldplayer-discovery.json"), &remembered))
	if remembered.Directory == "" || remembered.ADB == "" {
		fail(errors.New("validated installation was not automatically remembered"))
	}
	actual := readText(control(h, 201))
	wantDir, wantErr := os.Stat(install)
	gotDir, gotErr := os.Stat(actual)
	if wantErr != nil || gotErr != nil || !os.SameFile(wantDir, gotDir) {
		fail(fmt.Errorf("wrong installation selected: got %q, expected same directory as %q", actual, install))
	}
	count, _, _ := user.NewProc("SendMessageW").Call(control(h, 202), 0x146, 0, 0)
	if count != 5 {
		fail(fmt.Errorf("offline VM hid inventory: count=%d", count))
	}
	// Select stopped instance 7, then click the actual single-launch button.
	user.NewProc("SendMessageW").Call(control(h, 202), 0x14E, 1, 0)
	click(h, 114)
	until(h, "single launch or ADB verification failed", func() bool {
		return strings.Contains(readText(control(h, 208)), "已运行 3 台，ADB 已验证 2 台") && strings.Contains(readText(control(h, 900)), "启动状态检查完成")
	})
	// Start dynamic group 6..10, preserving already-running 6, 7 and 9.
	writeText(control(h, 205), "6")
	click(h, 115)
	until(h, "five-instance group launch failed", func() bool {
		return strings.Contains(readText(control(h, 208)), "已运行 5 台，ADB 已验证 4 台")
	})
	log, e := os.ReadFile(filepath.Join(install, "launches.log"))
	fail(e)
	if strings.Join(strings.Fields(string(log)), ",") != "7,8,10" {
		fail(fmt.Errorf("wrong or repeated instance launch: %s", log))
	}
	writeText(control(h, 205), "10")
	click(h, 115)
	until(h, "missing index must reject group before any launch", func() bool { return strings.Contains(readText(control(h, 900)), "编号 11 不存在") })
	after, e := os.ReadFile(filepath.Join(install, "launches.log"))
	fail(e)
	if string(after) != string(log) {
		fail(errors.New("invalid group launched a partial subset"))
	}
	// Simulate an externally stopped VM; the periodic scan must update the GUI.
	all = []vm{{6, false, false}, {7, true, false}, {8, true, false}, {9, true, true}, {10, true, false}}
	fail(filesafe.JSON(filepath.Join(install, "emulator-fixture.json"), all))
	until(h, "periodic refresh did not notice externally stopped instance", func() bool {
		return strings.Contains(readText(control(h, 208)), "已运行 4 台，ADB 已验证 3 台")
	})
	if strings.Contains(readText(control(h, 900)), "操作进行中，请先停止或等待完成") {
		fail(errors.New("internal control notifications were treated as user actions"))
	}
	fail(captureWindow(h, filepath.Join(outDir, "emulator-gui-preview.png")))
	closeTitle(title)
	fail(wait(app, 10*time.Second))
	_ = host.Process.Kill()
	_ = host.Wait()
	// Only the validated discovery record is copied; no config and no running
	// vendor process can supply the custom directory on this fresh launch.
	restoredData := filepath.Join(root, "restored-data")
	copyFile(filepath.Join(data, "ldplayer-discovery.json"), filepath.Join(restoredData, "ldplayer-discovery.json"))
	all = []vm{{6, false, false}, {7, false, false}, {8, false, false}, {9, false, false}, {10, false, false}}
	fail(filesafe.JSON(filepath.Join(install, "emulator-fixture.json"), all))
	restored := start(exe, "--data-dir", restoredData)
	fail(waitWindow(title))
	h = findWindow(title)
	until(h, "validated path was not restored for stopped instances", func() bool {
		return strings.Contains(readText(control(h, 208)), "检测到 5 台，已运行 0 台，ADB 已验证 0 台") && readText(control(h, 201)) != ""
	})
	if _, e = os.Stat(filepath.Join(restoredData, "config.json")); !os.IsNotExist(e) {
		fail(errors.New("restoration changed config"))
	}
	click(h, 115)
	until(h, "cold five-instance launch no longer verifies every ADB identity", func() bool {
		return strings.Contains(readText(control(h, 208)), "检测到 5 台，已运行 5 台，ADB 已验证 5 台")
	})
	closeTitle(title)
	fail(wait(restored, 10*time.Second))
	return map[string]any{"passed": true, "fixture_only": true, "real_ldplayer_tested": false, "startup_process_path_discovery": true, "ten_column_inventory": true, "offline_instance_retained": true, "automatic_scan_does_not_save_config": true, "validated_path_automatically_remembered": true, "restart_from_discovery_record_with_all_instances_stopped": true, "cold_five_launch_and_adb_verified": true, "single_launch_button": true, "dynamic_group_6_to_10": true, "running_instances_not_relaunched": true, "missing_index_rejected_before_launch": true, "periodic_refresh": true, "cloned_android_id_with_unique_boot_ids": true, "control_notifications_do_not_trigger_actions": true}
}

func adbFallbackGUI(exe, fixture, version, outDir string) map[string]any {
	root, e := os.MkdirTemp("", "MHX native ADB 中文 ")
	fail(e)
	defer os.RemoveAll(root)
	state := filepath.Join(root, "adb-state.json")
	ready := filepath.Join(root, "server-ready.json")
	type device struct {
		Index            int
		Running, Offline bool
		Serial           string
	}
	fail(filesafe.JSON(state, []device{{26, true, false, "emulator-19752"}, {87, true, true, "127.0.0.1:27991"}, {301, true, false, "unrelated-serial-Z"}}))
	server := start(fixture, "--serve-adb", state, ready)
	defer func() { _ = server.Process.Kill(); _ = server.Wait() }()
	var endpoint struct{ Address string }
	end := time.Now().Add(10 * time.Second)
	for time.Now().Before(end) {
		if filesafe.ReadJSON(ready, &endpoint) == nil && endpoint.Address != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if endpoint.Address == "" {
		fail(errors.New("native ADB fixture did not start"))
	}
	previous, hadPrevious := os.LookupEnv("ADB_SERVER_SOCKET")
	fail(os.Setenv("ADB_SERVER_SOCKET", "tcp:"+endpoint.Address))
	defer func() {
		if hadPrevious {
			_ = os.Setenv("ADB_SERVER_SOCKET", previous)
		} else {
			_ = os.Unsetenv("ADB_SERVER_SOCKET")
		}
	}()
	data := filepath.Join(root, "fresh-data")
	app := start(exe, "--data-dir", data)
	title := "知夏 · MHX Studio " + version
	fail(waitWindow(title))
	h := findWindow(title)
	until(h, "native ADB fallback did not populate the list", func() bool {
		return strings.Contains(readText(control(h, 208)), "发现 3 个ADB设备，雷电实例编号待匹配")
	})
	if !strings.Contains(readText(control(h, 202)), "emulator-19752") {
		fail(errors.New("fallback default selection did not prefer an online device"))
	}
	var report struct {
		Source, Reason string
		Checks         []struct{ Kind, Path, Result string }
		Instances      []struct {
			Index    int
			Serial   string
			Unmapped bool
		}
	}
	fail(filesafe.ReadJSON(filepath.Join(data, "diagnostics", "detection.json"), &report))
	if !strings.Contains(report.Source, "本机ADB服务") || len(report.Instances) != 3 {
		fail(fmt.Errorf("unexpected fallback: %+v", report))
	}
	seen := map[string]bool{}
	for _, d := range report.Instances {
		if !d.Unmapped || d.Index != -1 {
			fail(errors.New("ADB port was converted into a guessed instance index"))
		}
		seen[d.Serial] = true
	}
	if !seen["emulator-19752"] || !seen["127.0.0.1:27991"] || !seen["unrelated-serial-Z"] {
		fail(errors.New("device serials were hard-coded"))
	}
	hint, service := false, false
	for _, c := range report.Checks {
		if strings.EqualFold(c.Path, `D:\LDPlayer14\ldconsole.exe`) {
			hint = true
		}
		if c.Kind == "ADB本地服务" && c.Path == endpoint.Address {
			service = true
		}
	}
	if !hint || !service {
		fail(errors.New("checked directories or ADB address missing from report"))
	}
	if _, e = os.Stat(filepath.Join(data, "config.json")); !os.IsNotExist(e) {
		fail(errors.New("fallback scan changed user configuration"))
	}
	click(h, 114)
	until(h, "unmapped ADB device was not blocked from launch", func() bool {
		return strings.Contains(readText(control(h, 900)), "没有已核验的雷电管理程序")
	})
	fail(captureWindow(h, filepath.Join(outDir, "adb-fallback-gui-preview.png")))
	closeTitle(title)
	fail(wait(app, 10*time.Second))
	return map[string]any{"passed": true, "fixture_only": true, "real_ldplayer_tested": false, "automatic_native_adb_without_console": true, "arbitrary_serials_and_offline_devices_visible": true, "no_guessed_instance_numbers": true, "unmapped_control_blocked": true, "checked_paths_reported": true, "prior_D_LDPlayer14_candidate_checked": true, "configuration_unchanged_by_detection": true}
}
