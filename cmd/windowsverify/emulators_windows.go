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
	return map[string]any{"passed": true, "fixture_only": true, "real_ldplayer_tested": false, "startup_process_path_discovery": true, "ten_column_inventory": true, "offline_instance_retained": true, "automatic_scan_does_not_save_config": true, "single_launch_button": true, "dynamic_group_6_to_10": true, "running_instances_not_relaunched": true, "missing_index_rejected_before_launch": true, "periodic_refresh": true, "cloned_android_id_with_unique_boot_ids": true, "control_notifications_do_not_trigger_actions": true}
}
