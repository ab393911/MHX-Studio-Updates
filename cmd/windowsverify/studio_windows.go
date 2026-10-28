//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
)

func studioGUI(exe, fixture, version, out string) map[string]any {
	root, e := os.MkdirTemp("", "MHX studio 数据 中控 ")
	fail(e)
	defer os.RemoveAll(root)
	install := filepath.Join(root, "雷电")
	fail(os.MkdirAll(install, 0700))
	for _, n := range []string{"ldconsole.exe", "adb.exe", "dnplayer.exe"} {
		copyFile(fixture, filepath.Join(install, n))
	}
	var all []map[string]any
	for n := 1; n <= 24; n++ {
		all = append(all, map[string]any{"Index": n, "Running": n <= 5})
	}
	fail(filesafe.JSON(filepath.Join(install, "emulator-fixture.json"), all))
	ids := []int{2, 6, 9, 17, 24}
	jobs := []string{"合欢", "天音", "青云", "鬼王", "鬼王"}
	jobMap := map[string]any{}
	bindings := map[string]any{}
	for i, id := range ids {
		jobMap[fmt.Sprint(id)] = []string{jobs[i], jobs[i], jobs[i], jobs[i], jobs[i]}
		bindings[fmt.Sprint(id)] = map[string]any{"package": "com.example.game", "activity": ".Main", "android_id": "abcdef0123456789", "user_confirmed": true}
		g := gameFixtureState{Package: "com.example.game", Activity: ".Main", Foreground: "com.android.launcher", Page: "notice"}
		fail(filesafe.JSON(filepath.Join(install, fmt.Sprintf("game-fixture-%d.json", id)), g))
	}
	data := filepath.Join(root, "data")
	fail(filesafe.JSON(filepath.Join(data, "config.json"), map[string]any{"ldplayer_directory": install, "retries": 1, "group_start_index": 1, "jobs": jobMap, "game_bindings": bindings}))
	host := start(filepath.Join(install, "dnplayer.exe"), "--hold")
	defer func() { _ = host.Process.Kill(); _ = host.Wait() }()
	app := start(exe, "--data-dir", data)
	title := "知夏 · MHX Studio " + version
	fail(waitWindow(title))
	h := findWindow(title)
	until(h, "studio baseline inventory not ready", func() bool {
		return strings.Contains(readText(control(h, 208)), "检测到 24 台，已运行 5 台，ADB 已验证 5 台")
	})
	click(h, 106)
	panelTitle := "MHX Studio 工作台 · 五开计划与订单"
	fail(waitWindow(panelTitle))
	p := findWindow(panelTitle)
	until(h, "workbench exceeds work area", func() bool { return onScreen(p) && onScreen(control(p, 501)) && onScreen(control(p, 507)) })
	list := control(p, 510)
	for _, id := range ids {
		user.NewProc("SendMessageW").Call(list, 0x185, 1, uintptr(id-1))
	}
	click(p, 501)
	until(h, "arbitrary five-instance launch did not verify ADB", func() bool { return strings.Contains(readText(control(p, 512)), "五台ADB身份已验证") })
	gameIdle(h)
	launches, e := os.ReadFile(filepath.Join(install, "launches.log"))
	fail(e)
	if strings.Join(strings.Fields(string(launches)), ",") != "6,9,17,24" {
		fail(fmt.Errorf("wrong selected launch %s", launches))
	}
	click(p, 502)
	until(h, "selected five game launch failed", func() bool {
		return strings.Contains(readText(control(p, 512)), "五台已绑定游戏的前台均已核验")
	})
	gameIdle(h)
	for _, id := range ids {
		var g gameFixtureState
		fail(filesafe.ReadJSON(filepath.Join(install, fmt.Sprintf("game-fixture-%d.json", id)), &g))
		if g.Starts != 1 || g.Taps != 0 || g.Foreground != g.Package {
			fail(fmt.Errorf("unexpected game action %+v", g))
		}
	}
	writeText(control(p, 511), "一梦诛仙,另一服")
	click(p, 503)
	until(h, "plan not persisted/displayed", func() bool { return strings.Contains(readText(control(p, 515)), "角色1小组") })
	writeText(control(p, 513), "ci-buyer-7")
	writeText(control(p, 514), "7万")
	click(p, 507)
	until(h, "order not displayed", func() bool {
		return strings.Contains(readText(control(p, 515)), "总额70000 确认已交0 锁定0 剩余70000")
	})
	writeText(control(p, 516), "32.7万")
	writeText(control(p, 517), "345")
	click(p, 509)
	until(h, "per-character capacity calculator failed", func() bool { return strings.Contains(readText(control(p, 512)), "估算可交付30000银锭") })
	fail(captureWindow(p, filepath.Join(out, "studio-gui-preview.png")))
	closeTitle(panelTitle)
	gameIdle(h)
	closeTitle(title)
	fail(wait(app, 10*time.Second))
	s, e := studio.Open(data)
	fail(e)
	orders, e := s.Orders()
	fail(e)
	if len(orders) != 1 || orders[0].Total != 70000 || orders[0].Confirmed != 0 || orders[0].DeliveryEnabled {
		fail(fmt.Errorf("order changed %+v", orders))
	}
	plan, e := s.LatestPlan()
	fail(e)
	if len(plan.Batches) != 1 || len(plan.Batches[0].Servers) != 2 || plan.Batches[0].Actors[0].Instance != 2 {
		fail(fmt.Errorf("plan mismatch %+v", plan))
	}
	next, e := s.Next(plan.ID, time.Now())
	fail(e)
	for _, task := range next.Tasks {
		if task.State != studio.Pending {
			fail(fmt.Errorf("plan incorrectly marked game complete %+v", task))
		}
	}
	fail(s.Close())
	app = start(exe, "--data-dir", data)
	fail(waitWindow(title))
	h = findWindow(title)
	until(h, "restart inventory not ready", func() bool { return strings.Contains(readText(control(h, 208)), "检测到 24 台") })
	click(h, 106)
	fail(waitWindow(panelTitle))
	p = findWindow(panelTitle)
	until(h, "order not restored on restart", func() bool { return strings.Contains(readText(control(p, 515)), "ci-buyer-7") })
	closeTitle(panelTitle)
	gameIdle(h)
	closeTitle(title)
	fail(wait(app, 10*time.Second))
	return map[string]any{"passed": true, "fixture_only": true, "real_game_tested": false, "real_ldplayer_tested": false, "arbitrary_five_selection": true, "selected_start_and_adb": true, "selected_five_games_open_without_taps": true, "sqlite_plan_and_order_persisted": true, "no_fake_task_completion": true, "no_real_delivery": true, "capacity_327000_345_is_30000": true, "restart_preserves_plan_and_order": true, "workbench_on_screen": true}
}
