//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

type utilityPanel struct {
	app                                          *app
	hwnd, editor, scope, id, name, button, count uintptr
	kind                                         string
	index                                        int
	bundle                                       diagnosticBundle
}

func (a *app) newUtility(title, kind string) *utilityPanel {
	if a.panel != nil {
		user32.NewProc("SetForegroundWindow").Call(a.panel.hwnd)
		return nil
	}
	inst, _, _ := moduleHandle.Call(0)
	h, _, _ := createWindow.Call(0, uintptr(unsafe.Pointer(utf("MHXStudioNextWindow"))), uintptr(unsafe.Pointer(utf(title))), 0x00CA0000|wsVisible, 50, 30, 990, 720, a.hwnd, 0, inst, 0)
	if h == 0 {
		a.log("窗口打开失败")
		return nil
	}
	p := &utilityPanel{app: a, hwnd: h, kind: kind}
	a.panel = p
	fitPopup(h, 990, 720)
	user32.NewProc("EnableWindow").Call(a.hwnd, 0)
	return p
}
func (a *app) openDiagnostic(bundle diagnosticBundle) {
	p := a.newUtility("页面识别诊断", "diagnostic")
	if p == nil {
		return
	}
	p.bundle = bundle
	child(p.hwnd, "STATIC", "本次只读采集，无游戏点击。导出含当前截图，请确认后再分享。", 15, 12, 940, 24, 0, 0)
	p.editor = child(p.hwnd, "EDIT", bundle.Summary, 15, 45, 940, 565, wsBorder|0x200000|0x100000|4|0x40|0x800, 1300)
	send(p.editor, 0xC5, 2<<20, 0)
	setText(p.editor, bundle.Summary)
	child(p.hwnd, "BUTTON", "复制完整报告", 15, 625, 170, 32, 0, 1301)
	imageButton := child(p.hwnd, "BUTTON", "查看960×540原图", 200, 625, 190, 32, 0, 1302)
	if _, err := os.Stat(filepath.Join(bundle.Directory, "screenshot.png")); err != nil {
		user32.NewProc("EnableWindow").Call(imageButton, 0)
	}
	child(p.hwnd, "BUTTON", "导出诊断ZIP", 405, 625, 170, 32, 0, 1303)
	child(p.hwnd, "BUTTON", "关闭", 590, 625, 100, 32, 0, 1304)
}
func chooseDocument(owner uintptr, save bool, title, initialDir, pattern string) string {
	file := make([]uint16, 32768)
	filter := []uint16{}
	for _, s := range []string{"文件", pattern, "所有文件", "*.*"} {
		v, _ := syscall.UTF16FromString(s)
		filter = append(filter, v...)
	}
	filter = append(filter, 0)
	of := openFileName{Owner: owner, File: &file[0], MaxFile: uint32(len(file)), Filter: &filter[0], Title: utf(title), Flags: 0x80000 | 0x800 | 0x8}
	if initialDir != "" {
		of.InitialDir = utf(initialDir)
	}
	proc := "GetOpenFileNameW"
	if save {
		proc = "GetSaveFileNameW"
		of.Flags |= 2
		of.DefaultExt = utf(strings.TrimPrefix(pattern, "*."))
	} else {
		of.Flags |= 0x1000
	}
	of.Size = uint32(unsafe.Sizeof(of))
	ok, _, _ := syscall.NewLazyDLL("comdlg32.dll").NewProc(proc).Call(uintptr(unsafe.Pointer(&of)))
	if ok == 0 {
		return ""
	}
	return syscall.UTF16ToString(file)
}
func copyReport(owner uintptr, s string) error {
	ok, _, e := user32.NewProc("OpenClipboard").Call(owner)
	if ok == 0 {
		return e
	}
	defer user32.NewProc("CloseClipboard").Call()
	values, err := syscall.UTF16FromString(s)
	if err != nil {
		return err
	}
	mem, _, e := kernel32.NewProc("GlobalAlloc").Call(2, uintptr(len(values)*2))
	if mem == 0 {
		return e
	}
	transferred := false
	defer func() {
		if !transferred {
			kernel32.NewProc("GlobalFree").Call(mem)
		}
	}()
	ptr, _, e := kernel32.NewProc("GlobalLock").Call(mem)
	if ptr == 0 {
		return e
	}
	kernel32.NewProc("RtlMoveMemory").Call(ptr, uintptr(unsafe.Pointer(&values[0])), uintptr(len(values)*2))
	kernel32.NewProc("GlobalUnlock").Call(mem)
	ok, _, e = user32.NewProc("EmptyClipboard").Call()
	if ok == 0 {
		return e
	}
	ok, _, e = user32.NewProc("SetClipboardData").Call(13, mem)
	if ok == 0 {
		return e
	}
	transferred = true
	return nil
}
func (a *app) openRuleManager() {
	d, err := a.selected()
	if err != nil {
		a.info(err.Error())
		return
	}
	p := a.newUtility("页面规则管理", "rules")
	if p == nil {
		return
	}
	p.index = d.Index
	child(p.hwnd, "STATIC", fmt.Sprintf("实例%d：本地优先于公共规则。导入/历史恢复先预览，点击保存并确认后才生效。", d.Index), 15, 10, 940, 32, 0, 0)
	p.scope = child(p.hwnd, "COMBOBOX", "", 15, 44, 320, 120, 3, 1400)
	comboAdd(p.scope, "本实例规则（优先）")
	comboAdd(p.scope, "公共页面规则（共享）")
	send(p.scope, 0x14e, 0, 0)
	child(p.hwnd, "BUTTON", "载入JSON", 350, 44, 130, 30, 0, 1401)
	child(p.hwnd, "BUTTON", "保存规则", 490, 44, 130, 30, 0, 1402)
	child(p.hwnd, "BUTTON", "导出JSON", 630, 44, 130, 30, 0, 1403)
	child(p.hwnd, "BUTTON", "恢复历史…", 770, 44, 160, 30, 0, 1404)
	p.editor = child(p.hwnd, "EDIT", "", 15, 84, 940, 435, wsBorder|0x200000|0x100000|4|0x40, 1409)
	send(p.editor, 0xC5, 2<<20, 0)
	child(p.hwnd, "STATIC", "框选新增/修改页面：页面ID、名称、按钮ID、稳定特征数量；点击后实时取图，不点击游戏。", 15, 530, 940, 26, 0, 0)
	p.id = child(p.hwnd, "EDIT", "custom_page", 15, 562, 180, 28, wsBorder, 1406)
	p.name = child(p.hwnd, "EDIT", "自定义页面", 205, 562, 230, 28, wsBorder, 1407)
	p.button = child(p.hwnd, "EDIT", "custom_button", 445, 562, 170, 28, wsBorder, 1408)
	p.count = child(p.hwnd, "COMBOBOX", "", 630, 562, 90, 160, 3, 1410)
	for i := 2; i <= 8; i++ {
		comboAdd(p.count, strconv.Itoa(i))
	}
	send(p.count, 0x14e, 0, 0)
	child(p.hwnd, "BUTTON", "实时截图并框选", 735, 562, 200, 30, 0, 1405)
	child(p.hwnd, "BUTTON", "关闭", 15, 615, 130, 32, 0, 1304)
	p.loadRules()
}
func (p *utilityPanel) shared() bool { return comboIndex(p.scope) == 1 }
func (p *utilityPanel) loadRules() {
	var rules automation.Profile
	var err error
	if p.shared() {
		rules, err = readProfile(sharedProfilePath(p.app.root))
	} else {
		rules, err = loadLocalProfile(p.app.root, p.index)
	}
	if err != nil {
		p.app.info(err.Error())
		return
	}
	raw, _ := json.MarshalIndent(rules, "", "  ")
	setText(p.editor, string(raw))
}
func (p *utilityPanel) rules() (automation.Profile, error) {
	v := automation.NewProfile()
	raw, err := readRuleEditor(p.editor)
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	if err != nil {
		return v, err
	}
	return v, v.Validate()
}
func (p *utilityPanel) proc(h uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case wmCommand:
		id := int(w & 0xffff)
		if id == 1400 && w>>16 == 1 {
			p.loadRules()
			return 0
		}
		if w>>16 != 0 {
			return 0
		}
		switch id {
		case 1301:
			if err := copyReport(h, p.bundle.Summary); err != nil {
				p.app.info("复制失败：" + err.Error())
			} else {
				p.app.log("完整识别诊断已复制。")
			}
		case 1302:
			_ = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", filepath.Join(p.bundle.Directory, "screenshot.png")).Start()
		case 1303:
			if path := chooseDocument(h, true, "导出诊断ZIP", "", "*.zip"); path != "" {
				if !strings.EqualFold(filepath.Ext(path), ".zip") {
					p.app.info("请选择.zip文件名；未写入文件。")
					return 0
				}
				if err := validateExportDestination(p.app.root, path); err != nil {
					p.app.info(err.Error())
					return 0
				}
				if err := exportRecognitionBundle(p.bundle, path); err != nil {
					p.app.info(err.Error())
				} else {
					p.app.log("诊断ZIP已导出：" + path)
				}
			}
		case 1304:
			destroyWindow.Call(h)
		case 1401, 1404:
			initial := ""
			if id == 1404 {
				initial = filepath.Join(p.app.root, "profiles", "history")
			}
			if path := chooseDocument(h, false, "选择规则文件：先预览，保存确认后生效", initial, "*.json"); path != "" {
				raw, err := os.ReadFile(path)
				if err != nil {
					p.app.info(err.Error())
					return 0
				}
				v := automation.NewProfile()
				if err = json.Unmarshal(raw, &v); err == nil {
					err = v.Validate()
				}
				if err != nil {
					p.app.info("规则无效，原规则保持：" + err.Error())
					return 0
				}
				setText(p.editor, string(raw))
			}
		case 1402:
			rules, err := p.rules()
			if err != nil {
				p.app.info(err.Error())
				return 0
			}
			yes, _, _ := messageBox.Call(h, uintptr(unsafe.Pointer(utf("确认替换所选范围的规则？原规则将备份，所有匹配阈值按预览内容保存。不会执行游戏操作。"))), uintptr(unsafe.Pointer(utf("确认规则修改"))), 4|0x30|0x100)
			if yes != 6 {
				return 0
			}
			if err = saveRuleLibrary(p.app.root, p.index, rules, p.shared()); err != nil {
				p.app.info(err.Error())
				return 0
			}
			if p.shared() {
				p.app.cfg.SingleValidations = map[string]proof{}
			} else {
				delete(p.app.cfg.SingleValidations, strconv.Itoa(p.index))
			}
			if err = p.app.cfg.Save(p.app.root); err != nil {
				p.app.info("规则已保存；验证凭据保存失败：" + err.Error())
			}
			p.app.log("页面规则已保存并留存历史；下次识别重新读取，需重新验证。")
		case 1403:
			rules, err := p.rules()
			if err != nil {
				p.app.info(err.Error())
				return 0
			}
			if path := chooseDocument(h, true, "导出页面规则JSON", "", "*.json"); path != "" {
				if !strings.EqualFold(filepath.Ext(path), ".json") {
					p.app.info("请选择.json文件名；未写入文件。")
					return 0
				}
				if err := validateExportDestination(p.app.root, path); err != nil {
					p.app.info(err.Error())
					return 0
				}
				if err = filesafe.JSON(path, rules); err != nil {
					p.app.info(err.Error())
				}
			}
		case 1405:
			key, label, button := text(p.id), text(p.name), text(p.button)
			valid := regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
			if !valid.MatchString(key) || !valid.MatchString(button) || label == "" || len(label) > 128 {
				p.app.info("页面和按钮ID使用小写字母、数字、下划线，名称不能为空。 ")
				return 0
			}
			shared, n := p.shared(), comboIndex(p.count)+2
			name := calibrationName{key, label, button}
			destroyWindow.Call(h)
			p.app.startRuleCalibration(name, shared, n)
		}
		return 0
	case wmClose:
		destroyWindow.Call(h)
		return 0
	case wmDestroy:
		p.app.panel = nil
		user32.NewProc("EnableWindow").Call(p.app.hwnd, 1)
		return 0
	}
	v, _, _ := defWindow.Call(h, uintptr(m), w, l)
	return v
}

func readRuleEditor(h uintptr) (string, error) {
	n, _, _ := user32.NewProc("GetWindowTextLengthW").Call(h)
	if n > 2<<20 {
		return "", fmt.Errorf("规则内容超过2MiB，请拆分页面规则")
	}
	b := make([]uint16, int(n)+1)
	getWindowText.Call(h, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return syscall.UTF16ToString(b), nil
}
