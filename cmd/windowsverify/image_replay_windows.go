//go:build windows

package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
)

type imageReplayExpected struct {
	Kind       string    `json:"kind"`
	PNG        string    `json:"source_png_sha256"`
	Pixels     string    `json:"image_pixel_sha256"`
	Profile    string    `json:"profile_file_sha256"`
	MeanErrors []float64 `json:"mean_errors"`
	Changed    []int     `json:"changed_pixels"`
	Threshold  float64   `json:"threshold"`
}

func guiGDIHandles(h uintptr) uintptr {
	var pid uint32
	user.NewProc("GetWindowThreadProcessId").Call(h, uintptr(unsafe.Pointer(&pid)))
	kernel := syscall.NewLazyDLL("kernel32.dll")
	process, _, err := kernel.NewProc("OpenProcess").Call(0x400, 0, uintptr(pid))
	if process == 0 {
		fail(fmt.Errorf("GDI resource process query: %v", err))
	}
	defer kernel.NewProc("CloseHandle").Call(process)
	count, _, _ := user.NewProc("GetGuiResources").Call(process, 0)
	if count == 0 {
		fail(fmt.Errorf("no GDI resources in running GUI process"))
	}
	return count
}

// The screenshot is real; the emulator and data directory are disposable fixtures.
// No calibration is saved. Preserve the recorded failure instead of silently
// calibrating the same frame until it passes.
func replayGameImageGUI(exe, title, frame, profile, statePath, out, replayDir string, app *exec.Cmd, h uintptr) map[string]any {
	var expected imageReplayExpected
	fail(filesafe.ReadJSON(filepath.Join(replayDir, "expected.json"), &expected))
	if len(expected.MeanErrors) != 2 || len(expected.Changed) != 2 || expected.Threshold != 10 {
		fail(fmt.Errorf("invalid recorded feature expectations"))
	}
	rules, err := os.ReadFile(profile)
	fail(err)
	png, err := os.ReadFile(frame)
	fail(err)
	if fmt.Sprintf("%x", sha256.Sum256(png)) != expected.PNG || fmt.Sprintf("%x", sha256.Sum256(rules)) != expected.Profile {
		fail(fmt.Errorf("recorded PNG/profile SHA mismatch"))
	}
	data := filepath.Dir(filepath.Dir(profile))
	verifyRules := func() {
		now, err := os.ReadFile(profile)
		fail(err)
		if !bytes.Equal(now, rules) {
			fail(fmt.Errorf("preview changed recorded calibration"))
		}
	}
	inspect := func() uintptr {
		path := filepath.Join(data, "diagnostics", "recognition-latest-1.json")
		_ = os.Remove(path)
		click(h, 109)
		var report recognitionNative
		until(h, "recorded PNG diagnostic missing", func() bool { return filesafe.ReadJSON(path, &report) == nil })
		gameIdle(h)
		fail(waitWindow("页面识别诊断"))
		if report.Instance != 1 || report.Serial != "fixture-1" || report.Report.CacheUsed || report.Report.ProfileSHA != expected.Profile || report.Report.ScreenshotSHA != expected.PNG || report.Report.ImagePixelSHA != expected.Pixels || len(report.Report.Matches) != 0 {
			fail(fmt.Errorf("recorded frame provenance or safe failure changed"))
		}
		found := false
		for _, page := range report.Report.Pages {
			if page.Name != "game" {
				continue
			}
			found = true
			if page.Passed || page.Source != "local" || len(page.Anchors) != 2 {
				fail(fmt.Errorf("recorded game unexpectedly passes"))
			}
			for n, anchor := range page.Anchors {
				if anchor.Passed || anchor.MeanAbsoluteError != expected.MeanErrors[n] || anchor.ChangedPixels != expected.Changed[n] || anchor.MeanLimit != expected.Threshold {
					fail(fmt.Errorf("recorded feature %d score/threshold changed: %+v", n+1, anchor))
				}
			}
		}
		if !found {
			fail(fmt.Errorf("recorded game diagnostic missing"))
		}
		verifyRules()
		return findWindow("页面识别诊断")
	}
	const rounds = 3
	resourceCounts := []uintptr{}
	dpi := uintptr(0)
	for round := 0; round < rounds; round++ {
		fmt.Println("image replay: original/calibration round", round+1)
		dir := filepath.Join(filepath.Dir(out), fmt.Sprintf("round-%d", round+1))
		fail(os.MkdirAll(dir, 0700))
		diagnostic := inspect()
		// Reuse the full PNG byte/pixel verification and ordinary desktop readback.
		verifyOriginalView(diagnostic, png, filepath.Join(dir, "original.png"), true, frame)
		verifyRules()
		closeTitle("页面识别诊断")
		user.NewProc("SendMessageW").Call(control(h, 206), 0x14e, 11, 0)
		click(h, 108)
		calTitle := "实例 1 标定 · 游戏主界面：两处稳定特征 + 菜单按钮"
		fail(waitWindow(calTitle))
		cal := findWindow(calTitle)
		dpi, _, _ = user.NewProc("GetDpiForWindow").Call(cal)
		verifyDisplayLifecycle(cal, frame, dir, "calibration")
		// Exercise Cancel and the window close path. Never click Save or draw anchors.
		if round%2 == 0 {
			click(cal, 303)
		} else {
			closeTitle(calTitle)
		}
		until(h, "calibration preview did not close", func() bool { return findWindow(calTitle) == 0 })
		gameIdle(h)
		verifyRules()
		count := guiGDIHandles(h)
		resourceCounts = append(resourceCounts, count)
		if round > 0 && count > resourceCounts[0] {
			fail(fmt.Errorf("GDI resources grew after reopening: %v", resourceCounts))
		}
	}
	closeTitle(title)
	fail(wait(app, 10*time.Second))
	app = start(exe, "--data-dir", data)
	fail(waitWindow(title))
	h = findWindow(title)
	// The background scan must finish before requesting a fresh diagnostic.
	until(h, "recorded replay ADB fixture not verified", func() bool {
		return strings.Contains(readText(control(h, 208)), "ADB 已验证 1 台")
	})
	inspect()
	closeTitle("页面识别诊断")
	verifyRules()
	var state gameFixtureState
	fail(filesafe.ReadJSON(statePath, &state))
	if state.Starts != 0 || state.Taps != 0 {
		fail(fmt.Errorf("image replay sent game actions"))
	}
	closeTitle(title)
	fail(wait(app, 10*time.Second))
	return map[string]any{
		"passed": true, "native_windows": true, "recorded_png_replayed": true,
		"private_original_png_tested": expected.Kind == "private-original",
		"redacted_png_tested":         expected.Kind == "redacted",
		"image_source_kind":           expected.Kind,
		"real_ldplayer_connected":     false, "user_device_accepted": false,
		"source_png_sha256": expected.PNG, "profile_sha256_unchanged": expected.Profile,
		"image_pixel_sha256": expected.Pixels, "recorded_mean_errors": expected.MeanErrors,
		"threshold_unchanged": expected.Threshold, "recorded_recognition_still_rejected": true,
		"viewer_reopen_rounds": rounds, "calibration_cancel_rounds": rounds,
		"screen_rendering_without_printwindow": true, "full_960x540_pixel_comparison": true,
		"restart_preserves_original_rules": true, "gdi_handles_after_each_round": resourceCounts,
		"no_game_clicks_or_launches": true, "host_window_dpi": dpi,
		"simulated_dpi_notifications": []int{120, 144, 192},
	}
}

// Preview/cancel only: this helper never invokes calibration Save.
func previewCalibrationOnce(h uintptr, frame, profile, dir string, cancelButton bool) uintptr {
	before, err := os.ReadFile(profile)
	fail(err)
	user.NewProc("SendMessageW").Call(control(h, 206), 0x14e, 11, 0)
	click(h, 108)
	title := "实例 1 标定 · 游戏主界面：两处稳定特征 + 菜单按钮"
	fail(waitWindow(title))
	cal := findWindow(title)
	verifyDisplayLifecycle(cal, frame, dir, "calibration")
	if cancelButton {
		click(cal, 303)
	} else {
		closeTitle(title)
	}
	until(h, "calibration preview not closed", func() bool { return findWindow(title) == 0 })
	gameIdle(h)
	after, err := os.ReadFile(profile)
	fail(err)
	if !bytes.Equal(before, after) {
		fail(fmt.Errorf("preview changed calibration bytes"))
	}
	return guiGDIHandles(h)
}
