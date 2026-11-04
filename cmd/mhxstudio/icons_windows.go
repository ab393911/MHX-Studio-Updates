//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
	"unsafe"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
)

func (a *app) openIconManager() {
	d, err := a.selected()
	if err != nil {
		a.info(err.Error())
		return
	}
	p := a.newUtility("图标标定与多锚点草稿", "icon-rules")
	if p == nil {
		return
	}
	p.index = d.Index
	child(p.hwnd, "STATIC", fmt.Sprintf("实例%d：仅诊断草稿，未接入登录或游戏操作。共享仅适用于同分辨率/布局，仍需逐实例验证。", d.Index), 15, 10, 940, 30, 0, 0)
	p.scope = child(p.hwnd, "COMBOBOX", "", 15, 44, 320, 120, 3, 1400)
	comboAdd(p.scope, "本实例图标草稿")
	comboAdd(p.scope, "五开共享图标草稿（仅本机）")
	send(p.scope, 0x14e, 0, 0)
	for _, b := range []struct {
		label string
		x, id int
	}{{"载入JSON", 350, 1401}, {"保存草稿", 490, 1402}, {"导出JSON", 630, 1403}, {"恢复历史…", 770, 1404}} {
		child(p.hwnd, "BUTTON", b.label, b.x, 44, 130, 30, 0, b.id)
	}
	p.editor = child(p.hwnd, "EDIT", "", 15, 84, 940, 415, wsBorder|0x200000|0x100000|4|0x40, 1409)
	send(p.editor, 0xC5, 2<<20, 0)
	child(p.hwnd, "STATIC", "框选基于已保存草稿：先选不同的6～24像素图标核心特征，最后单独选点击范围；避开红点、数字及背景。", 15, 505, 940, 32, 0, 0)
	p.id = child(p.hwnd, "EDIT", "ui_bag", 15, 542, 180, 28, wsBorder, 1406)
	p.name = child(p.hwnd, "EDIT", "背包（候选）", 205, 542, 230, 28, wsBorder, 1407)
	p.button = child(p.hwnd, "EDIT", "ui_bag_click", 445, 542, 170, 28, wsBorder, 1408)
	p.count = child(p.hwnd, "COMBOBOX", "", 630, 542, 90, 160, 3, 1410)
	for i := 2; i <= 8; i++ {
		comboAdd(p.count, strconv.Itoa(i))
	}
	send(p.count, 0x14e, 0, 0)
	child(p.hwnd, "BUTTON", "实时截图并框选", 735, 542, 200, 30, 0, 1405)
	p.iconChoice = child(p.hwnd, "COMBOBOX", "", 15, 585, 290, 150, 3, 1445)
	for _, c := range automation.IconCandidates() {
		comboAdd(p.iconChoice, c.Label)
	}
	send(p.iconChoice, 0x14e, 3, 0)
	child(p.hwnd, "BUTTON", "组合主界面候选", 320, 585, 190, 32, 0, 1442)
	child(p.hwnd, "BUTTON", "连续采样诊断（不点击）", 525, 585, 230, 32, 0, 1441)
	child(p.hwnd, "BUTTON", "关闭", 770, 585, 165, 32, 0, 1304)
	child(p.hwnd, "STATIC", "主界面候选至少配置3个不同图标，连续两帧至少同2个通过；弹窗可能保留图标，须补充遮挡页/负例规则。", 15, 633, 940, 36, 0, 0)
	p.loadIconEditor()
}
func (p *utilityPanel) loadIconEditor() {
	r, sha, err := readIconScope(p.app.root, p.index, p.shared())
	p.iconExpected = sha
	if err != nil {
		p.app.info("草稿无法读取；可从历史载入预览后恢复：" + err.Error())
		return
	}
	raw, _ := json.MarshalIndent(r, "", "  ")
	setText(p.editor, string(raw))
}
func (p *utilityPanel) editedIcons() (automation.IconRules, error) {
	var r automation.IconRules
	raw, err := readRuleEditor(p.editor)
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal([]byte(raw), &r); err != nil {
		return r, err
	}
	return r, r.Validate()
}
func (p *utilityPanel) iconCommand(h uintptr, id int, notification uintptr) uintptr {
	if id == 1400 && notification == 1 {
		p.loadIconEditor()
		return 0
	}
	if id == 1445 && notification == 1 {
		i := comboIndex(p.iconChoice)
		candidates := automation.IconCandidates()
		if i >= 0 && i < len(candidates) {
			c := candidates[i]
			setText(p.id, c.ID)
			setText(p.name, c.Label)
			setText(p.button, c.ID+"_click")
		}
		return 0
	}
	if notification != 0 {
		return 0
	}
	switch id {
	case 1304:
		destroyWindow.Call(h)
	case 1401, 1404:
		initial := ""
		if id == 1404 {
			initial = filepath.Join(p.app.root, "profiles", "icon-drafts", "history")
		}
		if path := chooseDocument(h, false, "选择图标草稿JSON：仅预览", initial, "*.json"); path != "" {
			raw, err := os.ReadFile(path)
			var r automation.IconRules
			if err == nil {
				err = json.Unmarshal(raw, &r)
			}
			if err == nil {
				err = r.Validate()
			}
			if err != nil {
				p.app.info("未载入草稿：" + err.Error())
			} else {
				setText(p.editor, string(raw))
			}
		}
	case 1402:
		r, err := p.editedIcons()
		if err != nil {
			p.app.info(err.Error())
			return 0
		}
		yes, _, _ := messageBox.Call(h, uintptr(unsafe.Pointer(utf("仅保存所选范围的图标草稿，旧草稿先备份；不会替换原页面标定，也不会执行游戏操作。"))), uintptr(unsafe.Pointer(utf("确认图标草稿"))), 4|0x30|0x100)
		if yes != 6 {
			return 0
		}
		if err = saveIconScope(p.app.root, p.index, p.shared(), r, p.iconExpected); err != nil {
			p.app.info(err.Error())
			return 0
		}
		p.loadIconEditor()
		p.app.log("图标草稿已保存；未启用游戏操作。")
	case 1403:
		r, err := p.editedIcons()
		if err != nil {
			p.app.info(err.Error())
			return 0
		}
		if path := chooseDocument(h, true, "导出图标草稿JSON", "", "*.json"); path != "" {
			if err = validateExportDestination(p.app.root, path); err == nil {
				err = filesafe.JSON(path, r)
			}
			if err != nil {
				p.app.info(err.Error())
			}
		}
	case 1405:
		valid := regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
		key, label, button := text(p.id), text(p.name), text(p.button)
		if !valid.MatchString(key) || !valid.MatchString(button) || label == "" {
			p.app.info("图标ID/按钮ID使用小写字母、数字和下划线；名称不能为空。")
			return 0
		}
		d, err := p.app.selected()
		if err != nil || d.Index != p.index {
			p.app.info("实例已变化，请重新打开图标草稿。")
			return 0
		}
		shared, count := p.shared(), comboIndex(p.count)+2
		destroyWindow.Call(h)
		p.app.startCalibrationStore(calibrationName{key, label, button}, shared, count, true, p.index)
	case 1442:
		r, err := p.editedIcons()
		if err == nil {
			r, err = composeIconMain(r)
		}
		if err != nil {
			p.app.info(err.Error())
			return 0
		}
		raw, _ := json.MarshalIndent(r, "", "  ")
		setText(p.editor, string(raw))
		p.app.log("主界面组合仅载入预览，请保存后再诊断；未改变原识别流程。")
	case 1441:
		d, err := p.app.selected()
		if err != nil || d.Index != p.index {
			p.app.info("实例已变化，请重新打开。")
			return 0
		}
		destroyWindow.Call(h)
		p.app.inspectIcons()
	}
	return 0
}

func (a *app) inspectIcons() {
	d, err := a.selected()
	if err != nil {
		a.log(err.Error())
		return
	}
	rules, err := loadIcons(a.root, d.Index)
	if err != nil {
		a.log("图标诊断停止：" + err.Error())
		return
	}
	request := a.currentGameTarget()
	client := a.client
	a.work(func(ctx context.Context) error {
		var frames []automation.IconFrame
		var last []byte
		for n := 0; n < rules.StableFrames; n++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			png, err := client.Screenshot(ctx, d)
			if err != nil {
				return err
			}
			// Screenshot verifies before reading. Verify again so an emulator
			// restart/rebind during screencap cannot become accepted evidence.
			if err = client.Verify(ctx, d); err != nil {
				return err
			}
			img, err := automation.Decode(png)
			if err != nil {
				return err
			}
			frames = append(frames, automation.IconFrame{Source: fmt.Sprintf("%d|%s|%s|%s", d.Index, d.Serial, d.AndroidID, d.BootID), CaptureID: fmt.Sprintf("%d-%d", time.Now().UnixNano(), n), Image: img})
			last = png
			if n+1 < rules.StableFrames {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(250 * time.Millisecond):
				}
			}
		}
		assessment := rules.Assess(frames)
		ruleJSON, err := json.Marshal(rules)
		if err != nil {
			return err
		}
		captures := []map[string]string{}
		for _, f := range frames {
			captures = append(captures, map[string]string{"source": f.Source, "capture_id": f.CaptureID, "pixel_sha256": automation.FrameSHA(f.Image)})
		}
		a.gameUI(ctx, request, func() {
			dir := filepath.Join(a.root, "diagnostics", fmt.Sprintf("icons-%d-%d", d.Index, time.Now().UnixNano()))
			doc := map[string]any{"instance": d.Index, "serial": d.Serial, "assessment": assessment, "rules_revision": rules.Revision, "effective_rule_sha256": fmt.Sprintf("%x", sha256.Sum256(ruleJSON)), "captures": captures, "source_verified_by_client": true, "live_device_acceptance": false}
			raw, err := json.MarshalIndent(doc, "", "  ")
			if err == nil {
				err = filesafe.Write(filepath.Join(dir, "report.json"), raw, 0600)
			}
			if err == nil {
				err = filesafe.Write(filepath.Join(dir, "screenshot.png"), last, 0600)
			}
			if err != nil {
				a.log("图标诊断记录失败：" + err.Error())
				return
			}
			summary := "图标草稿诊断：无游戏点击；候选不证明角色身份或无遮挡页面。\r\n" + assessment.Reason + "\r\n" + string(raw)
			if err = filesafe.Write(filepath.Join(dir, "summary.txt"), []byte(summary), 0600); err != nil {
				a.log(err.Error())
				return
			}
			a.openDiagnostic(diagnosticBundle{Directory: dir, Summary: summary})
		})
		return nil
	})
}
