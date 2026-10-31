//go:build windows

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
)

type gameFixtureState struct {
	Package, Activity, Foreground, Page string
	Role                                int
	Running, Stall, WrongRole           bool
	Starts, Taps                        int
}

func gameIdle(h uintptr) {
	until(h, "game worker did not finish", func() bool { v, _, _ := user.NewProc("IsWindowEnabled").Call(control(h, 117)); return v != 0 })
}
func gameFixtureProfile(root string) automation.Profile {
	p := automation.NewProfile()
	names := []string{"notice", "tree", "roles", "menu", "settings", "logout_confirm"}
	for n := 1; n <= 5; n++ {
		names = append(names, fmt.Sprintf("selected_%d", n), fmt.Sprintf("game_%d", n))
	}
	r1, r2 := automation.Rect{X: 10, Y: 10, W: 80, H: 24}, automation.Rect{X: 120, Y: 10, W: 80, H: 24}
	s1, s2 := automation.Rect{X: 300, Y: 100, W: 80, H: 24}, automation.Rect{X: 400, Y: 100, W: 80, H: 24}
	fill := func(img *image.RGBA, r automation.Rect, v byte) {
		for y := r.Y; y < r.Y+r.H; y++ {
			for x := r.X; x < r.X+r.W; x++ {
				img.SetRGBA(x, y, color.RGBA{R: v, G: v, B: v, A: 255})
			}
		}
	}
	for i, name := range names {
		img := image.NewRGBA(image.Rect(0, 0, 960, 540))
		v := byte(25 + i*16)
		fill(img, r1, v)
		fill(img, r2, v)
		rects := []automation.Rect{r1, r2}
		if strings.HasPrefix(name, "selected_") {
			fill(img, r1, 57)
			fill(img, r2, 57)
			fill(img, s1, v)
			fill(img, s2, v)
			rects = []automation.Rect{s1, s2}
		}
		a, e := automation.CaptureAnchor(img, rects[0])
		fail(e)
		b, e := automation.CaptureAnchor(img, rects[1])
		fail(e)
		p.Pages[name] = automation.Page{Anchors: []automation.Anchor{a, b}, Threshold: 2}
		var out bytes.Buffer
		fail(png.Encode(&out, img))
		fail(filesafe.Write(filepath.Join(root, "screens", name+".png"), out.Bytes(), 0600))
	}
	var unknown bytes.Buffer
	fail(png.Encode(&unknown, image.NewRGBA(image.Rect(0, 0, 960, 540))))
	fail(filesafe.Write(filepath.Join(root, "screens", "unknown.png"), unknown.Bytes(), 0600))
	p.Buttons["notice_enter"] = automation.Rect{X: 465, Y: 470, W: 30, H: 20}
	p.Buttons["tree_enter"] = automation.Rect{X: 735, Y: 395, W: 30, H: 20}
	p.Buttons["enter"] = automation.Rect{X: 835, Y: 478, W: 30, H: 20}
	for n, y := range []int{95, 169, 240, 311, 382} {
		p.Buttons[fmt.Sprintf("role_%d", n+1)] = automation.Rect{X: 125, Y: y - 12, W: 30, H: 24}
	}
	p.Pages["game"] = p.Pages["game_3"]
	p.Buttons["menu"] = automation.Rect{X: 900, Y: 310, W: 30, H: 24}
	p.Buttons["settings"] = automation.Rect{X: 800, Y: 400, W: 30, H: 24}
	p.Buttons["logout"] = automation.Rect{X: 600, Y: 420, W: 30, H: 24}
	p.Buttons["confirm_logout"] = automation.Rect{X: 500, Y: 350, W: 30, H: 24}
	return p
}
func gameGUI(exe, fixture, version, outDir string) map[string]any {
	root, e := os.MkdirTemp("", "MHX single game 中文 ")
	fail(e)
	defer os.RemoveAll(root)
	install := filepath.Join(root, "雷电游戏测试")
	fail(os.MkdirAll(install, 0700))
	for _, name := range []string{"ldconsole.exe", "adb.exe", "dnplayer.exe"} {
		copyFile(fixture, filepath.Join(install, name))
	}
	fail(filesafe.JSON(filepath.Join(install, "emulator-fixture.json"), []map[string]any{{"Index": 6, "Running": true}}))
	statePath := filepath.Join(install, "game-fixture.json")
	g := gameFixtureState{Package: "com.example.game", Activity: ".Main", Foreground: "com.android.launcher", Page: "notice"}
	fail(filesafe.JSON(statePath, g))
	p := gameFixtureProfile(install)
	data := filepath.Join(root, "data")
	profilePath := filepath.Join(data, "profiles", "6.json")
	fail(filesafe.JSON(profilePath, p))
	calibrationBefore, e := os.ReadFile(profilePath)
	fail(e)
	configPath := filepath.Join(data, "config.json")
	cfg := map[string]any{"ldplayer_directory": install, "retries": 0, "min_delay_ms": 100, "max_delay_ms": 100, "group_start_index": 6, "future_unknown": map[string]any{"keep": true}, "game_bindings": map[string]any{"6": map[string]any{"package": g.Package, "activity": g.Activity, "android_id": "abcdef0123456789", "user_confirmed": true}}, "target_roles": map[string]int{"6": 3}}
	fail(filesafe.JSON(configPath, cfg))
	host := start(filepath.Join(install, "dnplayer.exe"), "--hold")
	defer func() { _ = host.Process.Kill(); _ = host.Wait() }()
	app := start(exe, "--data-dir", data)
	title := "知夏 · MHX Studio " + version
	fail(waitWindow(title))
	h := findWindow(title)
	until(h, "main window exceeds desktop work area", func() bool {
		return onScreen(h) && onScreen(control(h, 107)) && onScreen(control(h, 110)) && onScreen(control(h, 117)) && onScreen(control(h, 118))
	})
	until(h, "game test did not discover bound instance", func() bool { return strings.Contains(readText(control(h, 208)), "ADB 已验证 1 台") })
	until(h, "target role setting was not restored", func() bool { return readText(control(h, 210)) == "3" })
	click(h, 118)
	until(h, "game launch-only button did not verify foreground", func() bool { return strings.Contains(readText(control(h, 900)), "仅启动游戏完成") })
	gameIdle(h)
	fail(filesafe.ReadJSON(statePath, &g))
	if g.Starts != 1 || g.Taps != 0 || g.Foreground != g.Package {
		fail(fmt.Errorf("launch-only state %+v", g))
	}
	click(h, 117)
	until(h, "single target did not enter and stay in game", func() bool { return strings.Contains(readText(control(h, 211)), "已停止操作") })
	gameIdle(h)
	fail(filesafe.ReadJSON(statePath, &g))
	if g.Starts != 1 || g.Page != "game_3" || g.Taps != 4 {
		fail(fmt.Errorf("wrong target or extra launch %+v", g))
	}
	fail(captureWindow(h, filepath.Join(outDir, "single-game-gui-preview.png")))
	click(h, 108)
	calTitle := "实例 6 标定 · 公告页：黄色踏入仙途"
	fail(waitWindow(calTitle))
	cal := findWindow(calTitle)
	until(h, "calibration window or save button exceeds desktop", func() bool { return onScreen(cal) && onScreen(control(cal, 302)) })
	fail(captureWindow(cal, filepath.Join(outDir, "calibration-gui-preview.png")))
	closeTitle(calTitle)
	gameIdle(h)
	click(h, 117)
	until(h, "slot was not reverified without nickname", func() bool { return strings.Contains(readText(control(h, 900)), "已进入第3个角色") })
	gameIdle(h)
	fail(filesafe.ReadJSON(statePath, &g))
	if g.Starts != 1 || g.Taps != 12 {
		fail(fmt.Errorf("already-running game was restarted/tapped %+v", g))
	}
	g.Page = "unknown"
	fail(filesafe.JSON(statePath, g))
	click(h, 117)
	until(h, "unknown screenshot did not stop safely", func() bool { return strings.Contains(readText(control(h, 211)), "未知页面") })
	until(h, "unknown screen diagnostic missing", func() bool {
		paths, _ := filepath.Glob(filepath.Join(data, "diagnostics", "error-*.png"))
		return len(paths) > 0
	})
	gameIdle(h)
	fail(filesafe.ReadJSON(statePath, &g))
	if g.Taps != 12 {
		fail(fmt.Errorf("unknown screen tapped %+v", g))
	}
	g.Page = "roles"
	g.Stall = true
	fail(filesafe.JSON(statePath, g))
	click(h, 117)
	until(h, "stall test never reached first controlled tap", func() bool { var s gameFixtureState; return filesafe.ReadJSON(statePath, &s) == nil && s.Taps == 13 })
	click(h, 107)
	until(h, "stop button did not cancel login", func() bool { return strings.Contains(readText(control(h, 211)), "用户中止") })
	time.Sleep(400 * time.Millisecond)
	gameIdle(h)
	fail(filesafe.ReadJSON(statePath, &g))
	if g.Taps != 13 {
		fail(fmt.Errorf("tap after stop %+v", g))
	}
	calibrationAfter, e := os.ReadFile(profilePath)
	fail(e)
	if !bytes.Equal(calibrationBefore, calibrationAfter) {
		fail(fmt.Errorf("login modified calibration"))
	}
	closeTitle(title)
	fail(wait(app, 10*time.Second))
	// Remove only the binding from this isolated CI configuration, then exercise
	// the real discovery/confirmation dialog. No candidates may be trial-launched.
	delete(cfg, "game_bindings")
	fail(filesafe.JSON(configPath, cfg))
	g.Stall = false
	g.Page = "game_3"
	fail(filesafe.JSON(statePath, g))
	app = start(exe, "--data-dir", data)
	fail(waitWindow(title))
	h = findWindow(title)
	until(h, "binding test inventory not ready", func() bool { return strings.Contains(readText(control(h, 208)), "ADB 已验证 1 台") })
	click(h, 119)
	selector := fmt.Sprintf("确认轻享游戏 · 实例%d", 6)
	fail(waitWindow(selector))
	dlg := findWindow(selector)
	until(h, "game selector exceeds desktop work area", func() bool { return onScreen(dlg) && onScreen(control(dlg, 401)) })
	click(dlg, 401)
	until(h, "human-confirmed game binding not saved", func() bool {
		return findWindow(selector) == 0 && strings.Contains(readText(control(h, 900)), "已由用户确认游戏")
	})
	gameIdle(h)
	var saved struct {
		Games map[string]struct {
			Package, Activity, AndroidID string
			Confirmed                    bool `json:"user_confirmed"`
		} `json:"game_bindings"`
		Roles map[string]int `json:"target_roles"`
	}
	fail(filesafe.ReadJSON(configPath, &saved))
	if !saved.Games["6"].Confirmed || saved.Games["6"].Package != g.Package || saved.Roles["6"] != 3 {
		fail(fmt.Errorf("binding/role not persisted %+v", saved))
	}
	gameIdle(h)
	fail(filesafe.ReadJSON(statePath, &g))
	if g.Starts != 1 || g.Taps != 13 {
		fail(fmt.Errorf("unconfirmed discovery performed launch/tap %+v", g))
	}
	closeTitle(title)
	fail(wait(app, 10*time.Second))
	return map[string]any{"passed": true, "fixture_only": true, "real_ldplayer_tested": false, "real_game_tested": false, "main_and_critical_buttons_on_screen": true, "calibration_window_on_screen": true, "game_selector_on_screen": true, "synthetic_960x540_screens": true, "actual_launch_and_enter_buttons": true, "package_selection_and_binding": true, "unknown_candidates_never_trial_launched": true, "existing_running_game_not_restarted": true, "target_role_selected_and_stays_in_game": true, "selected_slot_and_generic_main_verified": true, "unknown_page_stops_without_tap": true, "exception_screenshot_only": true, "stop_button_cancels_wait": true, "target_role_and_binding_persisted": true, "calibration_byte_identical": true}
}
