//go:build windows

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/update"
)

func liveUpdate(old, oldVersion, manifest, expected, out, fixture string, oneClick bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	m, newer, e := update.Check(ctx, update.Client(), manifest, oldVersion)
	fail(e)
	if !newer || m.Version != expected {
		fail(fmt.Errorf("unexpected live channel version %s", m.Version))
	}
	root, e := os.MkdirTemp("", "MHX live update 中文 ")
	fail(e)
	defer os.RemoveAll(root)
	data := filepath.Join(root, "AppData")
	fail(os.Setenv("LOCALAPPDATA", data))
	dataRoot := filepath.Join(data, "MHXStudioNext")
	install := ""
	if fixture != "" {
		install = filepath.Join(root, "已验收雷电基线")
		fail(os.MkdirAll(install, 0700))
		for _, name := range []string{"ldconsole.exe", "adb.exe", "dnplayer.exe"} {
			copyFile(fixture, filepath.Join(install, name))
		}
		type vm struct {
			Index            int
			Running, Offline bool
		}
		var all []vm
		for n := 1; n <= 24; n++ {
			all = append(all, vm{n, n <= 5, false})
		}
		fail(filesafe.JSON(filepath.Join(install, "emulator-fixture.json"), all))
		host := start(filepath.Join(install, "dnplayer.exe"), "--hold")
		defer func() { _ = host.Process.Kill(); _ = host.Wait() }()
		fail(filesafe.JSON(filepath.Join(dataRoot, "ldplayer-discovery.json"), map[string]string{"directory": install, "adb": filepath.Join(install, "adb.exe")}))
	}
	cfgPath := filepath.Join(dataRoot, "config.json")
	fail(filesafe.JSON(cfgPath, map[string]any{"ldplayer_directory": install, "group_start_index": 1, "manifest_url": manifest, "retries": 7, "min_delay_ms": 250, "max_delay_ms": 650, "jobs": map[string]any{}, "single_validations": map[string]any{}, "future_unknown_field": map[string]any{"keep": true}}))
	profile := filepath.Join(dataRoot, "profiles", "1.json")
	fail(filesafe.JSON(profile, map[string]any{"future_calibration_sentinel": "preserve bytes"}))
	before, e := os.ReadFile(cfgPath)
	fail(e)
	profileBefore, e := os.ReadFile(profile)
	fail(e)
	target := filepath.Join(root, "Program", "MHXStudio.exe")
	copyFile(old, target)
	oldHash, e := update.Hash(target)
	fail(e)
	parent := start(target)
	oldTitle := "知夏 · MHX Studio " + oldVersion
	fail(waitWindow(oldTitle))
	h := findWindow(oldTitle)
	if fixture != "" {
		until(h, "published baseline did not detect 24 instances", func() bool {
			return strings.Contains(readText(control(h, 208)), "检测到 24 台，已运行 5 台，ADB 已验证 5 台")
		})
	}
	time.Sleep(200 * time.Millisecond)
	prefix := "legacy"
	if oneClick {
		prefix = "one-click"
	}
	fail(captureWindow(h, filepath.Join(filepath.Dir(out), prefix+"-before.png")))
	button := control(h, 110)
	if button == 0 {
		fail(errors.New("update button not found"))
	}
	if oneClick {
		writeText(control(h, 205), "unfinished draft")
	}
	// This is the single initiating click. The legacy published .15 binary has
	// one immutable confirmation; the new updater baseline must have none.
	user.NewProc("SendMessageW").Call(button, 0xF5, 0, 0)
	done := make(chan error, 1)
	go func() { done <- parent.Wait() }()
	confirmations := 0
	finished := false
	end := time.Now().Add(100 * time.Second)
	for time.Now().Before(end) {
		dlg := findWindow("MHX Studio")
		if dlg != 0 {
			var pid uint32
			user.NewProc("GetWindowThreadProcessId").Call(dlg, uintptr(unsafe.Pointer(&pid)))
			if int(pid) == parent.Process.Pid {
				if oneClick {
					fail(errors.New("one-click updater displayed an unexpected confirmation dialog"))
				}
				yes := control(dlg, 6)
				if yes != 0 {
					user.NewProc("SendMessageW").Call(yes, 0xF5, 0, 0)
					confirmations++
				}
			}
		}
		select {
		case e := <-done:
			fail(e)
			finished = true
		default:
		}
		if finished {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !finished {
		_ = parent.Process.Kill()
		fail(errors.New("GUI update did not exit the original process"))
	}
	if !oneClick && confirmations != 1 {
		fail(fmt.Errorf("legacy preview.15 confirmation count %d", confirmations))
	}
	newTitle := "知夏 · MHX Studio " + expected
	fail(waitWindow(newTitle))
	h = findWindow(newTitle)
	until(h, "restarted GUI is hidden", func() bool { v, _, _ := user.NewProc("IsWindowVisible").Call(h); return v != 0 })
	var result update.Result
	var plan update.Plan
	committed := false
	end = time.Now().Add(45 * time.Second)
	for time.Now().Before(end) {
		plans, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".mhx-updates", "tx-*", "plan.json"))
		for _, path := range plans {
			if filesafe.ReadJSON(path, &plan) == nil && filesafe.ReadJSON(filepath.Join(filepath.Dir(path), "result.json"), &result) == nil && result.Status == "committed" {
				committed = true
			}
		}
		if committed {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !committed {
		fail(fmt.Errorf("GUI update did not commit: %+v", result))
	}
	fail(update.Verify(target, m.SHA256))
	fail(update.Verify(result.Backup, oldHash))
	_, e = update.ReadConfigBackup(plan.ConfigBackupPath(), dataRoot, plan.Token)
	fail(e)
	after, e := os.ReadFile(cfgPath)
	fail(e)
	if !bytes.Equal(before, after) {
		fail(errors.New("GUI update changed configuration bytes"))
	}
	profileAfter, e := os.ReadFile(profile)
	fail(e)
	if !bytes.Equal(profileBefore, profileAfter) {
		fail(errors.New("GUI update changed calibration bytes"))
	}
	until(h, "new GUI did not display explicit update success", func() bool {
		return strings.Contains(readText(control(h, 209)), "更新成功：当前版本 "+expected)
	})
	detectionVerified := false
	launchVerified := false
	if fixture != "" {
		until(h, "upgraded GUI no longer detects 24 instances", func() bool {
			return strings.Contains(readText(control(h, 208)), "检测到 24 台，已运行 5 台，ADB 已验证 5 台")
		})
		detectionVerified = true
		user.NewProc("SendMessageW").Call(control(h, 202), 0x14E, 5, 0)
		click(h, 114)
		until(h, "upgraded GUI could not start instance 6", func() bool {
			return strings.Contains(readText(control(h, 208)), "检测到 24 台，已运行 6 台，ADB 已验证 6 台")
		})
		launchVerified = true
	}
	time.Sleep(200 * time.Millisecond)
	click(h, 110)
	until(h, "latest-version check did not show the expected message", func() bool { return strings.Contains(readText(control(h, 209)), "当前已是最新版本") })
	fail(captureWindow(h, filepath.Join(filepath.Dir(out), prefix+"-after.png")))
	closeTitle(newTitle)
	resultMap := map[string]any{"native_windows": true, "previous_version": oldVersion, "baseline_published": !oneClick, "real_update_button_clicked": true, "update_button_clicks": 1, "confirmation_clicks": confirmations, "one_click_without_confirmation": oneClick, "anonymous_manifest_download": true, "sha256_verified": true, "old_exe_backup_retained": true, "configuration_snapshot_verified": true, "configuration_byte_identical": true, "calibration_byte_identical": true, "new_gui_restarted": true, "new_gui_visible": true, "explicit_success_message": true, "latest_version_message": true, "post_upgrade_24_instance_detection_fixture": detectionVerified, "post_upgrade_start_and_adb_fixture": launchVerified, "emulator_fixture_only": true, "real_ldplayer_tested": false, "version": expected, "source_commit": m.SourceCommit, "windows_game_tested": false, "passed": true, "completed_at": time.Now().UTC().Format(time.RFC3339)}
	if !oneClick {
		resultMap["previous_published_version"] = oldVersion
		resultMap["configuration_backup_timing"] = "legacy_new_gui_before_health"
	} else {
		resultMap["configuration_backup_timing"] = "before_executable_replacement"
	}
	fail(filesafe.JSON(out, resultMap))
	fmt.Println("Live GUI update passed:", expected, "one click:", oneClick, "legacy confirmations:", confirmations)
}

func captureWindow(h uintptr, path string) error {
	var r struct{ L, T, R, B int32 }
	if ok, _, _ := user.NewProc("GetWindowRect").Call(h, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return errors.New("GetWindowRect failed")
	}
	w, height := int(r.R-r.L), int(r.B-r.T)
	if w < 10 || height < 10 || w > 3000 || height > 3000 {
		return errors.New("unexpected window dimensions")
	}
	gdi := syscall.NewLazyDLL("gdi32.dll")
	dc, _, _ := user.NewProc("GetWindowDC").Call(h)
	if dc == 0 {
		return errors.New("GetDC failed")
	}
	defer user.NewProc("ReleaseDC").Call(h, dc)
	mem, _, _ := gdi.NewProc("CreateCompatibleDC").Call(dc)
	defer gdi.NewProc("DeleteDC").Call(mem)
	bitmap, _, _ := gdi.NewProc("CreateCompatibleBitmap").Call(dc, uintptr(w), uintptr(height))
	defer gdi.NewProc("DeleteObject").Call(bitmap)
	old, _, _ := gdi.NewProc("SelectObject").Call(mem, bitmap)
	ok, _, _ := user.NewProc("PrintWindow").Call(h, mem, 2)
	gdi.NewProc("SelectObject").Call(mem, old)
	if ok == 0 {
		return errors.New("PrintWindow failed")
	}
	header := struct {
		Size                   uint32
		Width, Height          int32
		Planes, Bits           uint16
		Compression, SizeImage uint32
		X, Y                   int32
		Used, Important        uint32
	}{Size: 40, Width: int32(w), Height: -int32(height), Planes: 1, Bits: 32}
	raw := make([]byte, w*height*4)
	n, _, _ := gdi.NewProc("GetDIBits").Call(dc, bitmap, 0, uintptr(height), uintptr(unsafe.Pointer(&raw[0])), uintptr(unsafe.Pointer(&header)), 0)
	if n == 0 {
		return errors.New("GetDIBits failed")
	}
	painted := false
	for i := 0; i < len(raw); i += 4 {
		if raw[i] != 0 || raw[i+1] != 0 || raw[i+2] != 0 {
			painted = true
			break
		}
	}
	if !painted {
		return errors.New("GUI capture is entirely black; visibility/rendering verification failed")
	}
	img := image.NewRGBA(image.Rect(0, 0, w, height))
	for y := 0; y < height; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			img.SetRGBA(x, y, color.RGBA{raw[i+2], raw[i+1], raw[i], 255})
		}
	}
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	e = png.Encode(f, img)
	return errors.Join(e, f.Close())
}
