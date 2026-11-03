//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type recognitionNative struct {
	Instance int
	Serial   string
	Report   struct {
		ProfileSHA    string `json:"profile_file_sha256"`
		ScreenshotSHA string `json:"screenshot_sha256"`
		CacheUsed     bool   `json:"cache_used"`
		Matches       []string
		Pages         []struct {
			Name    string
			Source  string
			Passed  bool
			Anchors []automation.AnchorScore
		}
	}
}

func recognitionGUI(exe, fixture, version, out string, displayProbe, screenPNG bool) map[string]any {
	user.NewProc("SetProcessDPIAware").Call()
	root, e := os.MkdirTemp("", "MHX recognition calibration ")
	fail(e)
	defer os.RemoveAll(root)
	install := filepath.Join(root, "LDPlayer")
	fail(os.MkdirAll(install, 0700))
	for _, name := range []string{"ldconsole.exe", "adb.exe", "dnplayer.exe"} {
		copyFile(fixture, filepath.Join(install, name))
	}
	fail(filesafe.JSON(filepath.Join(install, "emulator-fixture.json"), []map[string]any{{"Index": 1, "Running": true}}))
	p := gameFixtureProfile(install)
	// Reproduce the reported right-edge feature coordinates using synthetic pixels.
	// This verifies crop/display geometry, not the user's unavailable live image.
	rects := []automation.Rect{{X: 906, Y: 356, W: 36, H: 46}, {X: 908, Y: 411, W: 31, H: 44}, {X: 19, Y: 394, W: 30, H: 29}}
	framePath := filepath.Join(install, "screens", "game_3.png")
	frameBytes, e := os.ReadFile(framePath)
	fail(e)
	frame, e := png.Decode(bytes.NewReader(frameBytes))
	fail(e)
	textured := image.NewRGBA(frame.Bounds())
	for y := 0; y < 540; y++ {
		for x := 0; x < 960; x++ {
			textured.Set(x, y, frame.At(x, y))
		}
	}
	for _, r := range rects[:2] {
		for y := r.Y; y < r.Y+r.H; y++ {
			for x := r.X; x < r.X+r.W; x++ {
				textured.SetRGBA(x, y, color.RGBA{R: byte(30 + (x*11+y*3)%200), G: byte(20 + (x*5+y*7)%210), B: byte(40 + (x+y*13)%190), A: 255})
			}
		}
	}
	var encoded bytes.Buffer
	fail(png.Encode(&encoded, textured))
	fail(filesafe.Write(framePath, encoded.Bytes(), 0600))
	p.Pages["game"] = p.Pages["notice"] // stale features must be replaced; keep the reported threshold.
	gamePage := p.Pages["game"]
	gamePage.Threshold = 10
	p.Pages["game"] = gamePage
	state := gameFixtureState{Package: "com.pwrd.mzlite.laohu", Activity: ".Main", Foreground: "com.pwrd.mzlite.laohu", Page: "game_3", Running: true}
	statePath := filepath.Join(install, "game-fixture.json")
	fail(filesafe.JSON(statePath, state))
	data := filepath.Join(root, "data")
	profile := filepath.Join(data, "profiles", "1.json")
	fail(filesafe.JSON(profile, p))
	old, _ := os.ReadFile(profile)
	fail(filesafe.JSON(filepath.Join(data, "config.json"), map[string]any{"ldplayer_directory": install, "group_start_index": 1, "retries": 0, "min_delay_ms": 100, "max_delay_ms": 100, "game_bindings": map[string]any{"1": map[string]any{"package": state.Package, "activity": state.Activity, "android_id": "abcdef0123456789", "user_confirmed": true}}}))
	host := start(filepath.Join(install, "dnplayer.exe"), "--hold")
	defer func() { _ = host.Process.Kill(); _ = host.Wait() }()
	app := start(exe, "--data-dir", data)
	title := "知夏 · MHX Studio " + version
	fail(waitWindow(title))
	h := findWindow(title)
	until(h, "recognition fixture identity not verified", func() bool { return strings.Contains(readText(control(h, 208)), "ADB 已验证 1 台") })
	fmt.Println("recognition: identity verified; selecting PNG source")
	if screenPNG {
		// Actual screen pixels of this disposable Windows GUI, not generated shapes.
		fail(captureScreenRegion(image.Rect(0, 0, 960, 540), framePath))
		copyFile(framePath, filepath.Join(filepath.Dir(out), "source-desktop.png"))
		fmt.Println("recognition: real desktop PNG captured")
	}
	// Generic game is index 11, registered once; no nickname or login page is changed.
	send := user.NewProc("SendMessageW")
	send.Call(control(h, 206), 0x14e, 11, 0)
	click(h, 108)
	calTitle := "实例 1 标定 · 游戏主界面：两处稳定特征 + 菜单按钮"
	fail(waitWindow(calTitle))
	fmt.Println("recognition: calibration window opened")
	cal := findWindow(calTitle)
	dpi, _, _ := user.NewProc("GetDpiForWindow").Call(cal)
	if displayProbe {
		verifyDisplayLifecycle(cal, framePath, filepath.Dir(out), "calibration")
	}
	for _, r := range rects {
		a := uintptr(uint32(r.X+20) | uint32(r.Y+70)<<16)
		b := uintptr(uint32(r.X+r.W-1+20) | uint32(r.Y+r.H-1+70)<<16)
		send.Call(cal, 0x201, 1, a)
		send.Call(cal, 0x202, 0, b)
	}
	// Verify original-frame pixels are rendered at the exact client offset, not scaled/flipped.
	shot := filepath.Join(filepath.Dir(out), "calibration-roundtrip.png")
	fail(captureWindow(cal, shot))
	raw, e := os.ReadFile(shot)
	fail(e)
	rendered, e := png.Decode(bytes.NewReader(raw))
	fail(e)
	var origin struct{ X, Y int32 }
	user.NewProc("ClientToScreen").Call(cal, uintptr(unsafe.Pointer(&origin)))
	var wr screenRect
	user.NewProc("GetWindowRect").Call(cal, uintptr(unsafe.Pointer(&wr)))
	pngData, e := os.ReadFile(filepath.Join(install, "screens", "game_3.png"))
	fail(e)
	original, e := png.Decode(bytes.NewReader(pngData))
	fail(e)
	for _, r := range rects[:2] {
		x, y := r.X+r.W/2, r.Y+r.H/2
		rr, gg, bb, _ := original.At(x, y).RGBA()
		ar, ag, ab, _ := rendered.At(int(origin.X-wr.L)+20+x, int(origin.Y-wr.T)+70+y).RGBA()
		if rr != ar || gg != ag || bb != ab {
			fail(fmt.Errorf("display coordinate/pixel mismatch at %d,%d: %d,%d,%d != %d,%d,%d", x, y, rr, gg, bb, ar, ag, ab))
		}
	}
	fmt.Println("recognition: image lifecycle passed; saving calibration")
	click(cal, 302)
	fmt.Println("recognition: calibration save returned")
	until(h, "calibration not saved", func() bool {
		return findWindow(calTitle) == 0 && strings.Contains(readText(control(h, 900)), "已保存 游戏主界面")
	})
	gameIdle(h)
	history, _ := filepath.Glob(filepath.Join(data, "profiles", "history", "*.json"))
	if len(history) != 1 {
		fail(fmt.Errorf("missing original calibration backup %v", history))
	}
	kept, e := os.ReadFile(history[0])
	fail(e)
	if !bytes.Equal(old, kept) {
		fail(fmt.Errorf("original calibration backup changed"))
	}
	validCalibrationRaw, e := os.ReadFile(profile)
	fail(e)
	exported := false
	inspect := func(expect bool) recognitionNative {
		path := filepath.Join(data, "diagnostics", "recognition-latest-1.json")
		_ = os.Remove(path)
		fmt.Println("recognition: requesting fresh report")
		click(h, 109)
		var report recognitionNative
		until(h, "fresh recognition report not written", func() bool { return filesafe.ReadJSON(path, &report) == nil })
		gameIdle(h)
		fail(waitWindow("页面识别诊断"))
		diagnostic := findWindow("页面识别诊断")
		displayed := readText(control(diagnostic, 1300))
		if !strings.Contains(displayed, "fixture-1") || !strings.Contains(displayed, "平均误差") || !strings.Contains(displayed, "algorithm_version") {
			fail(fmt.Errorf("GUI diagnostic incomplete: %s", displayed))
		}
		fmt.Println("recognition: copying report")
		click(diagnostic, 1301)
		clipboardText := readClipboard()
		if strings.ReplaceAll(clipboardText, "\r\n", "\n") != strings.ReplaceAll(displayed, "\r\n", "\n") {
			fail(fmt.Errorf("clipboard report differs: %d vs %d", len(clipboardText), len(displayed)))
		}
		if !exported {
			fmt.Println("recognition: viewing original image")
			verifyOriginalView(diagnostic, pngData, filepath.Join(filepath.Dir(out), "diagnostic-original-view.png"), displayProbe, framePath)
			fmt.Println("recognition: exporting ZIP")
			destination, e := filepath.Abs(filepath.Join(filepath.Dir(out), "recognition-export.zip"))
			fail(e)
			_ = os.Remove(destination)
			user.NewProc("PostMessageW").Call(control(diagnostic, 1303), 0xF5, 0, 0)
			selectNativeDocument("导出诊断ZIP", destination)
			until(diagnostic, "GUI ZIP export missing", func() bool { _, e := os.Stat(destination); return e == nil })
			z, e := zip.OpenReader(destination)
			fail(e)
			names := map[string]bool{}
			for _, entry := range z.File {
				names[entry.Name] = true
				stream, e := entry.Open()
				fail(e)
				data, e := io.ReadAll(stream)
				fail(e)
				fail(stream.Close())
				if entry.Name == "screenshot.png" && !bytes.Equal(data, pngData) {
					fail(fmt.Errorf("ZIP changed original PNG bytes"))
				}
				if entry.Name == "report.json" {
					var copy recognitionNative
					fail(json.Unmarshal(data, &copy))
					if copy.Report.ScreenshotSHA != report.Report.ScreenshotSHA {
						fail(fmt.Errorf("ZIP report/frame SHA mismatch"))
					}
				}
			}
			fail(z.Close())
			if !names["screenshot.png"] || !names["report.json"] || !names["instance-profile.json"] || names["config.json"] {
				fail(fmt.Errorf("bad GUI ZIP export %v", names))
			}
			exported = true
		}
		closeTitle("页面识别诊断")
		found := false
		for _, s := range report.Report.Pages {
			if s.Name == "game" {
				found = true
				if s.Passed != expect || s.Source != "local" || len(s.Anchors) != 2 {
					fail(fmt.Errorf("game score unexpected %+v", s))
				}
				if expect {
					for _, a := range s.Anchors {
						if a.MeanAbsoluteError != 0 || a.MeanLimit != 10 || !a.Passed {
							fail(fmt.Errorf("wrong anchor metric: mean %.3f, limit %.3f, passed=%t; expected mean0/limit10", a.MeanAbsoluteError, a.MeanLimit, a.Passed))
						}
					}
				}
			}
		}
		if !found || report.Instance != 1 || report.Serial != "fixture-1" || report.Report.CacheUsed {
			fail(fmt.Errorf("wrong snapshot source %+v", report))
		}
		return report
	}
	fmt.Println("recognition: saved calibration verified")
	first := inspect(true)
	if first.Report.ScreenshotSHA != fmt.Sprintf("%x", sha256.Sum256(pngData)) {
		fail(fmt.Errorf("recognition did not use current source frame"))
	}
	closeTitle(title)
	fail(wait(app, 10*time.Second))
	app = start(exe, "--data-dir", data)
	fail(waitWindow(title))
	h = findWindow(title)
	until(h, "restart fixture identity not verified", func() bool { return strings.Contains(readText(control(h, 208)), "ADB 已验证 1 台") })
	again := inspect(true)
	if again.Report.ProfileSHA != first.Report.ProfileSHA {
		fail(fmt.Errorf("restart profile changed"))
	}
	// Edit and persist a custom rule through the actual GUI, never hand-edit production files.
	fmt.Println("recognition: restart passed; editing rules")
	click(h, 122)
	fail(waitWindow("页面规则管理"))
	manager := findWindow("页面规则管理")
	var edited automation.Profile
	fail(json.Unmarshal([]byte(readText(control(manager, 1409))), &edited))
	edited.Pages["custom_fixture"] = edited.Pages["game"]
	editorJSON, e := json.MarshalIndent(edited, "", "  ")
	fail(e)
	writeText(control(manager, 1409), string(editorJSON))
	user.NewProc("PostMessageW").Call(control(manager, 1402), 0xF5, 0, 0)
	fail(waitWindow("确认规则修改"))
	click(findWindow("确认规则修改"), 6)
	until(manager, "rule manager save not durable", func() bool {
		var p automation.Profile
		if filesafe.ReadJSON(profile, &p) != nil {
			return false
		}
		_, ok := p.Pages["custom_fixture"]
		return ok
	})
	// Restore the actual previous valid calibration using the history picker and GUI confirmation.
	history, e = filepath.Glob(filepath.Join(data, "profiles", "history", "*.json"))
	fail(e)
	prior := ""
	for _, path := range history {
		raw, e := os.ReadFile(path)
		fail(e)
		if bytes.Equal(raw, validCalibrationRaw) {
			prior = path
			break
		}
	}
	if prior == "" {
		fail(fmt.Errorf("previous valid rules missing from history"))
	}
	modifiedRaw, e := os.ReadFile(profile)
	fail(e)
	user.NewProc("PostMessageW").Call(control(manager, 1404), 0xF5, 0, 0)
	fmt.Println("recognition: restoring history preview")
	selectNativeDocument("选择规则文件：先预览，保存确认后生效", prior)
	until(manager, "history not loaded into preview", func() bool {
		return strings.ReplaceAll(readText(control(manager, 1409)), "\r\n", "\n") == strings.ReplaceAll(string(validCalibrationRaw), "\r\n", "\n")
	})
	unchanged, e := os.ReadFile(profile)
	fail(e)
	if !bytes.Equal(unchanged, modifiedRaw) {
		fail(fmt.Errorf("history preview changed active rules before confirmation"))
	}
	user.NewProc("PostMessageW").Call(control(manager, 1402), 0xF5, 0, 0)
	fail(waitWindow("确认规则修改"))
	click(findWindow("确认规则修改"), 6)
	until(manager, "history restore not durable", func() bool {
		var restored automation.Profile
		if filesafe.ReadJSON(profile, &restored) != nil {
			return false
		}
		_, found := restored.Pages["custom_fixture"]
		return !found && restored.Pages["game"].Threshold == 10
	})
	history, e = filepath.Glob(filepath.Join(data, "profiles", "history", "*.json"))
	fail(e)
	backedModified := false
	for _, path := range history {
		raw, e := os.ReadFile(path)
		fail(e)
		if bytes.Equal(raw, modifiedRaw) {
			backedModified = true
		}
	}
	if !backedModified {
		fail(fmt.Errorf("history restore lost replaced rules"))
	}
	closeTitle("页面规则管理")
	inspect(true)
	// Verify restored rules are also read after a fresh process restart.
	closeTitle(title)
	fail(wait(app, 10*time.Second))
	app = start(exe, "--data-dir", data)
	fail(waitWindow(title))
	h = findWindow(title)
	until(h, "restored identity unavailable after restart", func() bool { return strings.Contains(readText(control(h, 208)), "ADB 已验证 1 台") })
	inspect(true)
	fmt.Println("recognition: restored rules passed; rejecting unknown frame")
	state.Page = "unknown"
	fail(filesafe.JSON(statePath, state))
	unknown := inspect(false)
	if len(unknown.Report.Matches) != 0 {
		fail(fmt.Errorf("non-main frame unexpectedly recognized %v", unknown.Report.Matches))
	}
	fail(filesafe.ReadJSON(statePath, &state))
	if state.Taps != 0 || state.Starts != 0 {
		fail(fmt.Errorf("read-only recognition sent game actions %+v", state))
	}
	closeTitle(title)
	fail(wait(app, 10*time.Second))
	simulatedDPI := []int{}
	if displayProbe {
		simulatedDPI = []int{120, 144, 192}
	}
	return map[string]any{"source_png_sha256": fmt.Sprintf("%x", sha256.Sum256(pngData)), "host_window_dpi": dpi, "screen_rendering_without_printwindow": displayProbe, "actual_windows_desktop_png": screenPNG, "simulated_dpi_notifications": simulatedDPI, "passed": true, "native_windows": true, "fixture_only": true, "real_ldplayer_tested": false, "real_game_tested": false, "gui_original_image_view_exact_pixels": true, "zip_original_png_and_report_sha_verified": true, "gui_history_preview_is_read_only": true, "gui_history_restore_confirmed_and_backed_up": true, "restored_rules_work_after_restart": true, "gui_rule_edit_confirmed_and_persisted": true, "gui_zip_export_without_file_hunting": true, "gui_diagnostic_report_visible_and_clipboard_verified": true, "gui_saved_calibration_immediate_reload": true, "restart_preserves_calibration": true, "two_anchor_scores_verified": true, "thresholds_unchanged": true, "gdi_960x540_client_coordinate_roundtrip": true, "reported_right_edge_feature_rectangles_verified": true, "fresh_instance_identity_and_frame_sha_verified": true, "old_calibration_backup_preserved": true, "unknown_frame_rejected_without_click": true}
}

func readClipboard() string {
	ok, _, err := user.NewProc("OpenClipboard").Call(0)
	if ok == 0 {
		fail(err)
	}
	defer user.NewProc("CloseClipboard").Call()
	mem, _, err := user.NewProc("GetClipboardData").Call(13)
	if mem == 0 {
		fail(err)
	}
	kernel := syscall.NewLazyDLL("kernel32.dll")
	size, _, _ := kernel.NewProc("GlobalSize").Call(mem)
	ptr, _, err := kernel.NewProc("GlobalLock").Call(mem)
	if ptr == 0 {
		fail(err)
	}
	defer kernel.NewProc("GlobalUnlock").Call(mem)
	values := make([]uint16, int(size)/2)
	kernel.NewProc("RtlMoveMemory").Call(uintptr(unsafe.Pointer(&values[0])), ptr, size)
	return syscall.UTF16ToString(values)
}

// Common dialogs can nest the filename edit under a DirectUI/ComboBox host.
// Inspect actual visible child controls after initialization, never assume one parent level.
var filenameCandidate uintptr
var filenameRank int32
var enumFilenameCallback = syscall.NewCallback(func(h, l uintptr) uintptr {
	var name [128]uint16
	user.NewProc("GetClassNameW").Call(h, uintptr(unsafe.Pointer(&name[0])), 128)
	if syscall.UTF16ToString(name[:]) != "Edit" {
		return 1
	}
	visible, _, _ := user.NewProc("IsWindowVisible").Call(h)
	enabled, _, _ := user.NewProc("IsWindowEnabled").Call(h)
	if visible == 0 || enabled == 0 {
		return 1
	}
	var rect screenRect
	user.NewProc("GetWindowRect").Call(h, uintptr(unsafe.Pointer(&rect)))
	id, _, _ := user.NewProc("GetDlgCtrlID").Call(h)
	rank := rect.T
	if id == 1152 {
		rank += 100000
	}
	if rank > filenameRank {
		filenameRank = rank
		filenameCandidate = h
	}
	return 1
})

func findDialogFilename(dialog uintptr) uintptr {
	filenameCandidate = 0
	filenameRank = -1000000
	user.NewProc("EnumChildWindows").Call(dialog, enumFilenameCallback, 0)
	return filenameCandidate
}

func selectNativeDocument(title, path string) {
	fail(waitWindow(title))
	dialog := findWindow(title)
	var filename uintptr
	until(dialog, "native document filename missing", func() bool { filename = findDialogFilename(dialog); return filename != 0 && control(dialog, 1) != 0 })
	writeText(filename, path)
	click(dialog, 1)
}
func verifyOriginalView(diagnostic uintptr, expected []byte, out string, displayProbe bool, framePath string) {
	click(diagnostic, 1302)
	fail(waitWindow("诊断原图 · 960×540"))
	viewer := findWindow("诊断原图 · 960×540")
	if !onScreen(viewer) || !onScreen(control(viewer, 1310)) {
		fail(fmt.Errorf("original image viewer exceeds desktop"))
	}
	if displayProbe {
		verifyDisplayLifecycle(viewer, framePath, filepath.Dir(out), "original")
		fail(captureWindowScreen(viewer, out))
	} else {
		fail(captureWindow(viewer, out))
	}
	raw, e := os.ReadFile(out)
	fail(e)
	rendered, e := png.Decode(bytes.NewReader(raw))
	fail(e)
	original, e := png.Decode(bytes.NewReader(expected))
	fail(e)
	var origin struct{ X, Y int32 }
	var wr screenRect
	user.NewProc("ClientToScreen").Call(viewer, uintptr(unsafe.Pointer(&origin)))
	user.NewProc("GetWindowRect").Call(viewer, uintptr(unsafe.Pointer(&wr)))
	for y := 0; y < 540; y++ {
		for x := 0; x < 960; x++ {
			r, g, b, _ := original.At(x, y).RGBA()
			a, c, d, _ := rendered.At(int(origin.X-wr.L)+20+x, int(origin.Y-wr.T)+70+y).RGBA()
			if r != a || g != c || b != d {
				fail(fmt.Errorf("original image pixel changed at %d,%d", x, y))
			}
		}
	}
	closeTitle("诊断原图 · 960×540")
	enabled, _, _ := user.NewProc("IsWindowEnabled").Call(diagnostic)
	if enabled == 0 {
		fail(fmt.Errorf("closing original view did not restore diagnostic window"))
	}
}
