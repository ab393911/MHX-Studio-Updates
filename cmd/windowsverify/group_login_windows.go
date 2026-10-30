//go:build windows

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/evidence"
	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/login"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
)

func writeGroupImage(path string, img image.Image) {
	var b bytes.Buffer
	fail(png.Encode(&b, img))
	fail(filesafe.Write(path, b.Bytes(), 0600))
}
func readGroupImage(path string) *image.RGBA {
	b, e := os.ReadFile(path)
	fail(e)
	img, e := automation.Decode(b)
	fail(e)
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), img, image.Point{}, draw.Src)
	return out
}
func drawGroupAnchor(img *image.RGBA, a automation.Anchor) {
	for y := 0; y < 16; y++ {
		for x := 0; x < 32; x++ {
			v := a.Pixels[y*32+x]
			img.SetRGBA(a.Rect.X+x*(a.Rect.W-1)/31, a.Rect.Y+y*(a.Rect.H-1)/15, color.RGBA{v, v, v, 255})
		}
	}
}
func groupLoginGUI(exe, fixture, version, out string) map[string]any {
	root, e := os.MkdirTemp("", "MHX five login 中文 ")
	fail(e)
	defer os.RemoveAll(root)
	install := filepath.Join(root, "雷电")
	fail(os.MkdirAll(install, 0700))
	for _, n := range []string{"ldconsole.exe", "adb.exe", "dnplayer.exe"} {
		copyFile(fixture, filepath.Join(install, n))
	}
	ids := []int{2, 6, 9, 17, 24}
	selected := map[int]bool{}
	for _, id := range ids {
		selected[id] = true
	}
	var inventory []map[string]any
	for n := 1; n <= 24; n++ {
		inventory = append(inventory, map[string]any{"Index": n, "Running": selected[n]})
	}
	fail(filesafe.JSON(filepath.Join(install, "emulator-fixture.json"), inventory))
	data := filepath.Join(root, "data")
	jobs := map[string]any{}
	bindings := map[string]any{}
	catalog, e := evidence.Load()
	fail(e)
	serverPage, e := catalog.ServerPage("一梦诛仙")
	fail(e)
	for i, id := range ids {
		memberRoot := filepath.Join(install, fmt.Sprintf("seed-%d", id))
		p := gameFixtureProfile(memberRoot)
		dest := filepath.Join(install, "screens", fmt.Sprint(id))
		fail(os.MkdirAll(filepath.Dir(dest), 0700))
		fail(os.Rename(filepath.Join(memberRoot, "screens"), dest))
		game := readGroupImage(filepath.Join(dest, "game_3.png"))
		v := byte(160 + i*18)
		for _, a := range p.Pages["game_3"].Anchors {
			draw.Draw(game, image.Rect(a.Rect.X, a.Rect.Y, a.Rect.X+a.Rect.W, a.Rect.Y+a.Rect.H), &image.Uniform{color.RGBA{v, v, v, 255}}, image.Point{}, draw.Src)
		}
		page := p.Pages["game_3"]
		for j, a := range page.Anchors {
			page.Anchors[j], e = automation.CaptureAnchor(game, a.Rect)
			fail(e)
		}
		p.Pages["game_3"] = page
		writeGroupImage(filepath.Join(dest, "game_3.png"), game)
		tree := readGroupImage(filepath.Join(dest, "tree.png"))
		for _, a := range serverPage.Anchors {
			drawGroupAnchor(tree, a)
		}
		writeGroupImage(filepath.Join(dest, "tree.png"), tree)
		fail(filesafe.JSON(filepath.Join(data, "profiles", fmt.Sprintf("%d.json", id)), p))
		job := []string{"合欢", "天音", "青云", "鬼王", "鬼王"}[i]
		jobs[fmt.Sprint(id)] = []string{job, job, job, job, job}
		bindings[fmt.Sprint(id)] = map[string]any{"package": "com.example.game", "activity": ".Main", "android_id": "abcdef0123456789", "user_confirmed": true}
		fail(filesafe.JSON(filepath.Join(install, fmt.Sprintf("game-fixture-%d.json", id)), gameFixtureState{Package: "com.example.game", Activity: ".Main", Foreground: "com.android.launcher", Page: "notice"}))
	}
	fail(filesafe.JSON(filepath.Join(data, "config.json"), map[string]any{"ldplayer_directory": install, "retries": 0, "min_delay_ms": 100, "max_delay_ms": 100, "group_start_index": 1, "jobs": jobs, "game_bindings": bindings, "future_unknown": map[string]any{"keep": true}}))
	host := start(filepath.Join(install, "dnplayer.exe"), "--hold")
	defer func() { _ = host.Process.Kill(); _ = host.Wait() }()
	app := start(exe, "--data-dir", data)
	title := "知夏 · MHX Studio " + version
	fail(waitWindow(title))
	h := findWindow(title)
	until(h, "five-login inventory not ready", func() bool {
		return strings.Contains(readText(control(h, 208)), "检测到 24 台，已运行 5 台，ADB 已验证 5 台")
	})
	gameIdle(h)
	click(h, 106)
	panelTitle := "MHX Studio 工作台 · 五开计划与订单"
	fail(waitWindow(panelTitle))
	p := findWindow(panelTitle)
	for _, id := range ids {
		user.NewProc("SendMessageW").Call(control(p, 510), 0x185, 1, uintptr(id-1))
	}
	gameIdle(h)
	click(p, 522)
	groupTitle := "MHX Studio · 五台同槽登录准备"
	fail(waitWindow(groupTitle))
	g := findWindow(groupTitle)
	if !onScreen(g) || !onScreen(control(g, 531)) || !onScreen(control(g, 532)) {
		fail(fmt.Errorf("five-login controls exceed desktop"))
	}
	user.NewProc("SendMessageW").Call(control(g, 535), 0x14E, 2, 0)
	click(g, 531)
	until(h, "five-login readiness did not verify", func() bool { return strings.Contains(readText(control(g, 536)), "仅登录准备完成") })
	gameIdle(h)
	fail(captureWindow(g, filepath.Join(out, "group-login-gui-preview.png")))
	var report login.Report
	reportPath := filepath.Join(data, "profiles", "group-login-last.json")
	fail(filesafe.ReadJSON(reportPath, &report))
	if !report.Ready || len(report.Members) != 5 || report.Spec.Role != 3 {
		fail(fmt.Errorf("wrong group readiness report %+v", report))
	}
	for _, id := range ids {
		var state gameFixtureState
		fail(filesafe.ReadJSON(filepath.Join(install, fmt.Sprintf("game-fixture-%d.json", id)), &state))
		if state.Starts != 1 || state.Taps != 4 || state.Page != "game_3" {
			fail(fmt.Errorf("wrong group actions %d %+v", id, state))
		}
	}
	// Already-correct characters must not be logged out just to manufacture a
	// server proof. This second attempt is explicitly partial, with no new taps.
	click(g, 531)
	until(h, "missing server provenance was not reported", func() bool { return strings.Contains(readText(control(g, 536)), "区服依据不足") })
	gameIdle(h)
	fail(filesafe.ReadJSON(reportPath, &report))
	if report.Ready {
		fail(fmt.Errorf("already-in-game invented server evidence"))
	}
	for _, id := range ids {
		var state gameFixtureState
		fail(filesafe.ReadJSON(filepath.Join(install, fmt.Sprintf("game-fixture-%d.json", id)), &state))
		if state.Starts != 1 || state.Taps != 4 {
			fail(fmt.Errorf("already-target character was restarted or tapped"))
		}
	}
	// A wrong visible server label must stop its instance before any login tap.
	treePath := filepath.Join(install, "screens", fmt.Sprint(ids[0]), "tree.png")
	correctTree, e := os.ReadFile(treePath)
	fail(e)
	wrong := readGroupImage(treePath)
	for _, a := range serverPage.Anchors {
		draw.Draw(wrong, image.Rect(a.Rect.X, a.Rect.Y, a.Rect.X+a.Rect.W, a.Rect.Y+a.Rect.H), image.Black, image.Point{}, draw.Src)
	}
	writeGroupImage(treePath, wrong)
	for _, id := range ids {
		fail(filesafe.JSON(filepath.Join(install, fmt.Sprintf("game-fixture-%d.json", id)), gameFixtureState{Package: "com.example.game", Activity: ".Main", Foreground: "com.example.game", Page: "tree", Running: true}))
	}
	click(g, 531)
	until(h, "wrong server was not stopped", func() bool { return strings.Contains(readText(control(g, 536)), "区服文字特征与目标不符") })
	gameIdle(h)
	var wrongState gameFixtureState
	fail(filesafe.ReadJSON(filepath.Join(install, fmt.Sprintf("game-fixture-%d.json", ids[0])), &wrongState))
	if wrongState.Taps != 0 {
		fail(fmt.Errorf("wrong server was tapped"))
	}
	fail(filesafe.Write(treePath, correctTree, 0600))
	// Stop during a real wait. All fixture instances stay on the notice page.
	for _, id := range ids {
		fail(filesafe.JSON(filepath.Join(install, fmt.Sprintf("game-fixture-%d.json", id)), gameFixtureState{Package: "com.example.game", Activity: ".Main", Foreground: "com.example.game", Page: "notice", Running: true, Stall: true}))
	}
	beforeShots, _ := filepath.Glob(filepath.Join(data, "diagnostics", "*.png"))
	click(g, 531)
	until(h, "stalled group did not start", func() bool {
		for _, id := range ids {
			var state gameFixtureState
			if filesafe.ReadJSON(filepath.Join(install, fmt.Sprintf("game-fixture-%d.json", id)), &state) == nil && state.Taps > 0 {
				return true
			}
		}
		return false
	})
	click(g, 532)
	until(h, "group stop did not finish", func() bool { return strings.Contains(readText(control(g, 536)), "context canceled") })
	gameIdle(h)
	afterShots, _ := filepath.Glob(filepath.Join(data, "diagnostics", "*.png"))
	if len(beforeShots) != len(afterShots) {
		fail(fmt.Errorf("user cancellation saved error screenshots"))
	}
	var beforeStates []gameFixtureState
	for _, id := range ids {
		var state gameFixtureState
		fail(filesafe.ReadJSON(filepath.Join(install, fmt.Sprintf("game-fixture-%d.json", id)), &state))
		beforeStates = append(beforeStates, state)
	}
	time.Sleep(300 * time.Millisecond)
	for i, id := range ids {
		var state gameFixtureState
		fail(filesafe.ReadJSON(filepath.Join(install, fmt.Sprintf("game-fixture-%d.json", id)), &state))
		if state.Taps != beforeStates[i].Taps {
			fail(fmt.Errorf("tap continued after stop"))
		}
	}
	writeText(control(g, 534), "另一区服")
	click(g, 531)
	if !strings.Contains(readText(control(g, 536)), "不会猜测") {
		fail(fmt.Errorf("unknown server accepted"))
	}
	closeTitle(groupTitle)
	gameIdle(h)
	// Login preparation does not invent or complete any scheduled task.
	click(p, 503)
	until(h, "plan not created after login", func() bool { return strings.Contains(readText(control(p, 515)), "角色1小组") })
	click(p, 522)
	fail(waitWindow(groupTitle))
	g = findWindow(groupTitle)
	user.NewProc("SendMessageW").Call(control(g, 535), 0x14E, 2, 0)
	click(g, 531)
	if !strings.Contains(readText(control(g, 536)), "不能跳过未完成小组") {
		fail(fmt.Errorf("plan role barrier bypassed"))
	}
	closeTitle(groupTitle)
	closeTitle(panelTitle)
	gameIdle(h)
	closeTitle(title)
	fail(wait(app, 10*time.Second))
	ledger, e := studio.Open(data)
	fail(e)
	defer ledger.Close()
	plan, e := ledger.LatestPlan()
	fail(e)
	phase, e := ledger.Next(plan.ID, time.Now())
	fail(e)
	for _, task := range phase.Tasks {
		if task.State != studio.Pending {
			fail(fmt.Errorf("login wrote fake task completion"))
		}
	}
	var cfg map[string]any
	fail(filesafe.ReadJSON(filepath.Join(data, "config.json"), &cfg))
	if cfg["group_login_role"] != float64(3) || cfg["group_login_server"] != "一梦诛仙" || cfg["future_unknown"] == nil {
		fail(fmt.Errorf("group preferences/unknown fields lost"))
	}
	return map[string]any{"passed": true, "fixture_only": true, "real_ldplayer_tested": false, "real_game_tested": false, "arbitrary_five_same_slot_login": true, "visible_server_label_required": true, "independent_role_templates": true, "final_five_recheck": true, "already_correct_no_logout_or_restart": true, "wrong_server_no_tap": true, "stop_cancels_all_and_no_new_taps": true, "user_stop_no_error_screenshot": true, "current_plan_role_barrier": true, "no_fake_task_completion": true, "settings_preserved": true, "dialog_on_screen": true}
}
