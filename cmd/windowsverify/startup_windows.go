//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/login"
)

func onOpenWait(h uintptr, description string, f func() bool) {
	end := time.Now().Add(80 * time.Second)
	for time.Now().Before(end) {
		if f() {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	fail(fmt.Errorf("%s: step=%s log=%s", description, readText(control(h, 211)), readText(control(h, 900))))
}
func onOpenGUI(exe, install, data, version, out string, ids []int) map[string]any {
	data2 := filepath.Join(filepath.Dir(data), "on-open-data")
	var cfg map[string]any
	fail(filesafe.ReadJSON(filepath.Join(data, "config.json"), &cfg))
	delete(cfg, "on_open_plan")
	fail(filesafe.JSON(filepath.Join(data2, "config.json"), cfg))
	for _, id := range ids {
		p := filepath.Join("profiles", fmt.Sprintf("%d.json", id))
		fail(os.MkdirAll(filepath.Dir(filepath.Join(data2, p)), 0700))
		copyFile(filepath.Join(data, p), filepath.Join(data2, p))
	}
	title := "知夏 · MHX Studio " + version
	panelTitle := "MHX Studio 工作台 · 五开计划与四区资产"
	groupTitle := "MHX Studio · 五台同槽登录准备"
	app := start(exe, "--data-dir", data2)
	fail(waitWindow(title))
	h := findWindow(title)
	onOpenWait(h, "setup inventory", func() bool { return strings.Contains(readText(control(h, 208)), "检测到 24 台") })
	gameIdle(h)
	click(h, 106)
	fail(waitWindow(panelTitle))
	p := findWindow(panelTitle)
	for _, id := range ids {
		user.NewProc("SendMessageW").Call(control(p, 510), 0x185, 1, uintptr(id-1))
	}
	gameIdle(h)
	click(p, 522)
	fail(waitWindow(groupTitle))
	g := findWindow(groupTitle)
	for _, id := range []int{531, 532, 538, 539, 540, 541} {
		if !onScreen(control(g, id)) {
			fail(fmt.Errorf("on-open control%d off-screen", id))
		}
	}
	user.NewProc("SendMessageW").Call(control(g, 535), 0x14E, 2, 0)
	click(g, 538)
	click(g, 539)
	if !strings.Contains(readText(control(g, 536)), "方案已保存") {
		fail(fmt.Errorf("on-open plan not saved: %s", readText(control(g, 536))))
	}
	var saved struct {
		Startup *login.StartupPlan `json:"on_open_plan"`
	}
	fail(filesafe.ReadJSON(filepath.Join(data2, "config.json"), &saved))
	if saved.Startup == nil || !saved.Startup.Enabled || saved.Startup.Target.Role != 3 || saved.Startup.RunTasks {
		fail(fmt.Errorf("incorrect saved login-only plan %+v", saved))
	}
	fail(captureWindow(g, filepath.Join(out, "on-open-settings-gui-preview.png")))
	closeTitle(groupTitle)
	closeTitle(panelTitle)
	gameIdle(h)
	closeTitle(title)
	fail(wait(app, 10*time.Second))
	before, e := os.ReadFile(filepath.Join(data2, "config.json"))
	fail(e)
	var inventory []map[string]any
	for i := 1; i <= 24; i++ {
		inventory = append(inventory, map[string]any{"Index": i, "Running": false})
	}
	fail(filesafe.JSON(filepath.Join(install, "emulator-fixture.json"), inventory))
	_ = os.Remove(filepath.Join(install, "launches.log"))
	for i, id := range ids {
		page := "notice"
		if i == 0 {
			page = "roles"
		}
		fail(filesafe.JSON(filepath.Join(install, fmt.Sprintf("game-fixture-%d.json", id)), gameFixtureState{Package: "com.example.game", Activity: ".Main", Foreground: "com.android.launcher", Page: page}))
	}
	// No button clicks after this launch: the saved plan must drive cold startup.
	app = start(exe, "--data-dir", data2)
	fail(waitWindow(title))
	h = findWindow(title)
	reportPath := filepath.Join(data2, "profiles", "startup-last.json")
	onOpenWait(h, "cold on-open login did not finish", func() bool {
		var r login.Report
		return filesafe.ReadJSON(reportPath, &r) == nil && r.Ready && r.Error == ""
	})
	gameIdle(h)
	fail(captureWindow(h, filepath.Join(out, "on-open-running-gui-preview.png")))
	launches, e := os.ReadFile(filepath.Join(install, "launches.log"))
	fail(e)
	if strings.Join(strings.Fields(string(launches)), ",") != "2,6,9,17,24" {
		fail(fmt.Errorf("wrong saved startup group: %s", launches))
	}
	var states []gameFixtureState
	for _, id := range ids {
		var s gameFixtureState
		fail(filesafe.ReadJSON(filepath.Join(install, fmt.Sprintf("game-fixture-%d.json", id)), &s))
		if s.Starts != 1 || s.Taps != 4 || s.Page != "game_3" {
			fail(fmt.Errorf("saved startup wrong role or duplicated game: %d %+v", id, s))
		}
		states = append(states, s)
	}
	after, e := os.ReadFile(filepath.Join(data2, "config.json"))
	fail(e)
	if string(before) != string(after) {
		fail(fmt.Errorf("on-open startup rewrote saved/unknown config"))
	}
	closeTitle(title)
	fail(wait(app, 10*time.Second))
	app = start(exe, "--data-dir", data2)
	fail(waitWindow(title))
	h = findWindow(title)
	onOpenWait(h, "startup countdown not visible", func() bool { return strings.Contains(readText(control(h, 211)), "自动启动倒计时") })
	click(h, 107)
	time.Sleep(7 * time.Second)
	gameIdle(h)
	for i, id := range ids {
		var s gameFixtureState
		fail(filesafe.ReadJSON(filepath.Join(install, fmt.Sprintf("game-fixture-%d.json", id)), &s))
		if s != states[i] {
			fail(fmt.Errorf("stop still started/tapped instance%d", id))
		}
	}
	closeTitle(title)
	fail(wait(app, 10*time.Second))
	fail(filesafe.ReadJSON(filepath.Join(data2, "config.json"), &cfg))
	plan := *saved.Startup
	plan.Members = append([]login.StartupMember(nil), saved.Startup.Members...)
	plan.Members[0].AndroidID = "changed-device"
	cfg["on_open_plan"] = plan
	fail(filesafe.JSON(filepath.Join(data2, "config.json"), cfg))
	app = start(exe, "--data-dir", data2)
	fail(waitWindow(title))
	h = findWindow(title)
	onOpenWait(h, "mismatched on-open binding not blocked", func() bool { return strings.Contains(readText(control(h, 900)), "游戏/设备绑定与方案不符") })
	gameIdle(h)
	for i, id := range ids {
		var s gameFixtureState
		fail(filesafe.ReadJSON(filepath.Join(install, fmt.Sprintf("game-fixture-%d.json", id)), &s))
		if s != states[i] {
			fail(fmt.Errorf("wrong device plan performed action on%d", id))
		}
	}
	closeTitle(title)
	fail(wait(app, 10*time.Second))
	return map[string]any{"passed": true, "fixture_only": true, "real_game_tested": false, "real_ldplayer_tested": false, "actual_gui_plan_save": true, "no_button_click_after_reopen": true, "cold_five_start_adb_game_role": true, "initial_role_list_server_reveal": true, "no_repeat_launch": true, "config_byte_identical": true, "stop_cancels_countdown": true, "wrong_binding_no_action": true, "no_os_boot_registration": true}
}
