//go:build windows

package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"image/png"
	"os"
	"path/filepath"
	"strings"
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

func recognitionGUI(exe, fixture, version, out string) map[string]any {
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
	p.Pages["game"] = p.Pages["notice"] // stale calibration must actually be replaced by GUI save
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
	// Generic game is index 11, registered once; no nickname or login page is changed.
	send := user.NewProc("SendMessageW")
	send.Call(control(h, 206), 0x14e, 11, 0)
	click(h, 108)
	calTitle := "实例 1 标定 · 游戏主界面：两处稳定特征 + 菜单按钮"
	fail(waitWindow(calTitle))
	cal := findWindow(calTitle)
	rects := []automation.Rect{{X: 10, Y: 10, W: 80, H: 24}, {X: 120, Y: 10, W: 80, H: 24}, {X: 20, Y: 480, W: 30, H: 24}}
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
	click(cal, 302)
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
	inspect := func(expect bool) recognitionNative {
		path := filepath.Join(data, "diagnostics", "recognition-latest-1.json")
		_ = os.Remove(path)
		click(h, 109)
		var report recognitionNative
		until(h, "fresh recognition report not written", func() bool { return filesafe.ReadJSON(path, &report) == nil })
		gameIdle(h)
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
							fail(fmt.Errorf("wrong anchor metric %+v", a))
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
	return map[string]any{"passed": true, "native_windows": true, "fixture_only": true, "real_ldplayer_tested": false, "real_game_tested": false, "gui_saved_calibration_immediate_reload": true, "restart_preserves_calibration": true, "two_anchor_scores_verified": true, "thresholds_unchanged": true, "gdi_960x540_client_coordinate_roundtrip": true, "fresh_instance_identity_and_frame_sha_verified": true, "old_calibration_backup_preserved": true, "unknown_frame_rejected_without_click": true}
}
