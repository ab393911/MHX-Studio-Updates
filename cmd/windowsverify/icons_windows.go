//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
)

// Exercise the new opt-in GUI against the disposable textured fixture. The
// separate real-PNG run still verifies both full image windows independently.
func verifyIconDraftGUI(h uintptr, data, profile, framePath, out string) {
	fmt.Println("recognition: icon draft GUI; no active-profile changes")
	before, err := os.ReadFile(profile)
	fail(err)
	open := func() uintptr {
		click(h, 122)
		fail(waitWindow("页面规则管理"))
		click(findWindow("页面规则管理"), 1440)
		fail(waitWindow("图标标定与多锚点草稿"))
		m := findWindow("图标标定与多锚点草稿")
		// Change scope exactly as a user would; notify the selected shared scope.
		user.NewProc("SendMessageW").Call(control(m, 1400), 0x14e, 1, 0)
		user.NewProc("SendMessageW").Call(m, 0x111, 1400|1<<16, control(m, 1400))
		return m
	}
	draftPath := filepath.Join(data, "profiles", "icon-drafts", "shared.json")
	calTitle := "实例 1 标定 · 背包（候选）"
	// Cancel must not create a library or alter the existing profile.
	m := open()
	click(m, 1405)
	fail(waitWindow(calTitle))
	cal := findWindow(calTitle)
	verifyDisplayLifecycle(cal, framePath, out, "icon-calibration")
	click(cal, 303)
	if _, err := os.Stat(draftPath); !os.IsNotExist(err) {
		fail(fmt.Errorf("cancel created icon rules: %v", err))
	}
	m = open()
	click(m, 1405)
	fail(waitWindow(calTitle))
	cal = findWindow(calTitle)
	for _, r := range []automation.Rect{{X: 906, Y: 356, W: 10, H: 10}, {X: 922, Y: 379, W: 10, H: 10}, {X: 906, Y: 356, W: 36, H: 46}} {
		a := uintptr(uint32(r.X+20) | uint32(r.Y+70)<<16)
		b := uintptr(uint32(r.X+r.W-1+20) | uint32(r.Y+r.H-1+70)<<16)
		user.NewProc("SendMessageW").Call(cal, 0x201, 1, a)
		user.NewProc("SendMessageW").Call(cal, 0x202, 0, b)
	}
	click(cal, 302)
	until(h, "icon calibration not saved", func() bool { _, err := os.Stat(draftPath); return err == nil })
	var first automation.IconRules
	fail(filesafe.ReadJSON(draftPath, &first))
	fail(first.Validate())
	if len(first.Profile.Pages) != 1 || first.Profile.Pages["ui_bag"].Threshold != 10 {
		fail(fmt.Errorf("unexpected icon calibration"))
	}
	firstRaw, err := os.ReadFile(draftPath)
	fail(err)
	m = open()
	var edited automation.IconRules
	fail(json.Unmarshal([]byte(readText(control(m, 1409))), &edited))
	p := edited.Profile.Pages["ui_bag"]
	p.Label = "edited icon fixture"
	edited.Profile.Pages["ui_bag"] = p
	raw, err := json.MarshalIndent(edited, "", "  ")
	fail(err)
	writeText(control(m, 1409), string(raw))
	save := func() {
		user.NewProc("PostMessageW").Call(control(m, 1402), 0xF5, 0, 0)
		fail(waitWindow("确认图标草稿"))
		click(findWindow("确认图标草稿"), 6)
	}
	save()
	until(m, "icon edit not durable", func() bool {
		var r automation.IconRules
		return filesafe.ReadJSON(draftPath, &r) == nil && r.Profile.Pages["ui_bag"].Label == "edited icon fixture"
	})
	history, err := filepath.Glob(filepath.Join(data, "profiles", "icon-drafts", "history", "*.json"))
	fail(err)
	prior := ""
	for _, path := range history {
		b, err := os.ReadFile(path)
		fail(err)
		if bytes.Equal(b, firstRaw) {
			prior = path
		}
	}
	if prior == "" {
		fail(fmt.Errorf("original icon draft not backed up"))
	}
	modified, err := os.ReadFile(draftPath)
	fail(err)
	user.NewProc("PostMessageW").Call(control(m, 1404), 0xF5, 0, 0)
	selectNativeDocument("选择图标草稿JSON：仅预览", prior)
	until(m, "icon history preview not loaded", func() bool {
		return strings.ReplaceAll(readText(control(m, 1409)), "\r\n", "\n") == strings.ReplaceAll(string(firstRaw), "\r\n", "\n")
	})
	preview, err := os.ReadFile(draftPath)
	fail(err)
	if !bytes.Equal(modified, preview) {
		fail(fmt.Errorf("history preview modified icon draft"))
	}
	save()
	until(m, "icon history restore not durable", func() bool {
		var r automation.IconRules
		return filesafe.ReadJSON(draftPath, &r) == nil && r.Revision == 3 && r.Profile.Pages["ui_bag"].Label == first.Profile.Pages["ui_bag"].Label
	})
	click(m, 1441)
	fail(waitWindow("页面识别诊断"))
	d := findWindow("页面识别诊断")
	report := readText(control(d, 1300))
	for _, s := range []string{"图标草稿诊断", "ui_bag", "\"action_allowed\": false", "effective_rule_sha256", "capture_id"} {
		if !strings.Contains(report, s) {
			fail(fmt.Errorf("incomplete icon report: missing %s", s))
		}
	}
	click(d, 1301)
	if readClipboard() != strings.ReplaceAll(report, "\r\n", "\n") && readClipboard() != report {
		fail(fmt.Errorf("icon report copy mismatch"))
	}
	closeTitle("页面识别诊断")
	after, err := os.ReadFile(profile)
	fail(err)
	if !bytes.Equal(before, after) {
		fail(fmt.Errorf("icon GUI changed active login profile"))
	}
	fmt.Println("recognition: icon cancel/save/shared reload/history/two-frame report passed")
}
