//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/ldplayer"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
	"github.com/ab393911/MHX-Studio-Updates/internal/update"
)

var version = "3.5.0-preview.0"
var sourceCommit = "development"
var (
	user32           = syscall.NewLazyDLL("user32.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	gdi32            = syscall.NewLazyDLL("gdi32.dll")
	registerClass    = user32.NewProc("RegisterClassExW")
	createWindow     = user32.NewProc("CreateWindowExW")
	defWindow        = user32.NewProc("DefWindowProcW")
	getMessage       = user32.NewProc("GetMessageW")
	translateMessage = user32.NewProc("TranslateMessage")
	dispatchMessage  = user32.NewProc("DispatchMessageW")
	setWindowText    = user32.NewProc("SetWindowTextW")
	getWindowText    = user32.NewProc("GetWindowTextW")
	sendMessage      = user32.NewProc("SendMessageW")
	postMessage      = user32.NewProc("PostMessageW")
	messageBox       = user32.NewProc("MessageBoxW")
	destroyWindow    = user32.NewProc("DestroyWindow")
	postQuit         = user32.NewProc("PostQuitMessage")
	setTimer         = user32.NewProc("SetTimer")
	moduleHandle     = kernel32.NewProc("GetModuleHandleW")
	loadCursor       = user32.NewProc("LoadCursorW")
	showWindow       = user32.NewProc("ShowWindow")
)

const (
	wsChild   = 0x40000000
	wsVisible = 0x10000000
	wsBorder  = 0x00800000
	wmCommand = 0x111
	wmApp     = 0x8001
	wmDestroy = 2
	wmClose   = 0x10
	wmTimer   = 0x113
)

type point struct{ X, Y int32 }
type msg struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             point
	Private        uint32
}
type wndclass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Name                         *uint16
	SmallIcon                          uintptr
}
type app struct {
	hwnd                                              uintptr
	controls                                          map[int]uintptr
	actions                                           chan func()
	logs                                              []string
	root                                              string
	cfg                                               settings
	devices                                           []ldplayer.Instance
	client                                            ldplayer.Client
	busy                                              atomic.Bool
	cancel                                            context.CancelFunc
	closing                                           bool
	cache                                             *automation.Cache
	cal                                               *calibration
	healthPath, healthToken, healthVersion, smokePath string
	healthDone                                        bool
	nextScan                                          time.Time
	lastInventory                                     string
	groupSeeded                                       bool
	backgroundScan                                    bool
	pendingAction                                     int
	updateResultPath                                  string
	updateResultDone                                  bool
	updating                                          bool
	startupBackupDone                                 bool
	gameSelector                                      *gameSelector
	roleInstanceKey                                   string
	placedCalibration, placedSelector                 uintptr
	workbench                                         *workbench
	stateStore                                        *studio.Store
	pendingStudioAction                               int
	lastProofCleanupDay                               string
	stateOpenFailed                                   bool
}

var active *app

func utf(s string) *uint16 { return syscall.StringToUTF16Ptr(s) }

func text(h uintptr) string {
	b := make([]uint16, 4096)
	getWindowText.Call(h, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return syscall.UTF16ToString(b)
}
func setText(h uintptr, s string) { setWindowText.Call(h, uintptr(unsafe.Pointer(utf(s)))) }
func send(h uintptr, m uint32, a, b uintptr) uintptr {
	r, _, _ := sendMessage.Call(h, uintptr(m), a, b)
	return r
}
func (a *app) ask(s string) bool {
	r, _, _ := messageBox.Call(a.hwnd, uintptr(unsafe.Pointer(utf(s))), uintptr(unsafe.Pointer(utf("MHX Studio"))), 4|0x30)
	return r == 6
}
func (a *app) info(s string) {
	messageBox.Call(a.hwnd, uintptr(unsafe.Pointer(utf(s))), uintptr(unsafe.Pointer(utf("MHX Studio"))), 0x40)
}
func (a *app) ui(f func()) { a.actions <- f; postMessage.Call(a.hwnd, wmApp, 0, 0) }
func (a *app) log(s string) {
	a.logs = append(a.logs, time.Now().Format("15:04:05")+"  "+s)
	if len(a.logs) > 180 {
		a.logs = a.logs[len(a.logs)-180:]
	}
	setText(a.controls[900], strings.Join(a.logs, "\r\n"))
	send(a.controls[900], 0xB1, ^uintptr(0), ^uintptr(0))
	send(a.controls[900], 0xB7, 0, 0)
}
func (a *app) logAsync(s string) { a.ui(func() { a.log(s) }) }
func child(parent uintptr, class, title string, x, y, w, h int, style uintptr, id int) uintptr {
	inst, _, _ := moduleHandle.Call(0)
	out, _, _ := createWindow.Call(0, uintptr(unsafe.Pointer(utf(class))), uintptr(unsafe.Pointer(utf(title))), wsChild|wsVisible|style, uintptr(x), uintptr(y), uintptr(w), uintptr(h), parent, uintptr(id), inst, 0)
	return out
}
func (a *app) control(class, title string, x, y, w, h int, style uintptr, id int) uintptr {
	out := child(a.hwnd, class, title, x, y, w, h, style, id)
	if id > 0 {
		a.controls[id] = out
	}
	font, _, _ := gdi32.NewProc("GetStockObject").Call(17)
	send(out, 0x30, font, 1)
	return out
}
func (a *app) label(s string, x, y, w int)      { a.control("STATIC", s, x, y, w, 25, 0, 0) }
func (a *app) button(s string, x, y, w, id int) { a.control("BUTTON", s, x, y, w, 32, 0, id) }
func comboAdd(h uintptr, s string)              { sendMessage.Call(h, 0x143, 0, uintptr(unsafe.Pointer(utf(s)))) }
func comboIndex(h uintptr) int                  { return int(int32(send(h, 0x147, 0, 0))) }
func (a *app) build() {
	a.label("MHX Studio "+version+"  |  截图识别、五开计划与本地订单测试版", 20, 15, 1000)
	a.label("保留已验收更新、雷电与游戏包名启动；新游戏功能仍待实机验收，战斗与真实发货未开放。", 20, 44, 1000)
	a.label("雷电目录", 20, 83, 90)
	a.control("EDIT", a.cfg.InstallDir, 115, 78, 610, 28, wsBorder, 201)
	a.button("选择雷电程序", 740, 77, 140, 101)
	a.button("检测实例", 895, 77, 110, 102)
	a.label("当前实例", 20, 126, 90)
	a.control("COMBOBOX", "", 115, 120, 420, 300, 3|0x200000, 202)
	send(a.controls[202], 0x160, 870, 0)
	a.label("职业（人工绑定）", 550, 126, 155)
	job := a.control("COMBOBOX", "", 710, 120, 140, 200, 3, 203)
	for _, s := range []string{"合欢", "天音", "青云", "鬼王"} {
		comboAdd(job, s)
	}
	send(job, 0x14E, 0, 0)
	a.button("绑定五个同职业角色", 865, 119, 140, 103)
	a.label("异常重试次数", 20, 168, 125)
	a.control("EDIT", strconv.Itoa(a.cfg.Retries), 150, 162, 60, 28, wsBorder, 204)
	a.label("五开首编号", 230, 168, 115)
	a.control("EDIT", strconv.Itoa(a.cfg.GroupStart), 350, 162, 70, 28, wsBorder, 205)
	a.button("保存设置", 440, 160, 110, 104)
	a.button("截图/职业预检", 670, 160, 170, 105)
	a.button("五开工作台", 855, 160, 150, 106)
	a.button("停止", 565, 160, 90, 107)
	a.label("标定页面", 20, 213, 90)
	cal := a.control("COMBOBOX", "", 115, 205, 500, 380, 3|0x200000, 206)
	for _, s := range calibrationNames {
		comboAdd(cal, s.label)
	}
	send(cal, 0x14E, 0, 0)
	a.button("截图并框选标定", 630, 204, 180, 108)
	a.button("只检查当前识别", 825, 204, 180, 109)
	a.label("更新清单", 20, 256, 90)
	a.control("EDIT", a.cfg.ManifestURL, 115, 250, 710, 28, wsBorder, 207)
	a.button("一键检查更新", 840, 248, 165, 110)
	a.control("STATIC", "点击一次即可检查并自动完成可用更新，旧EXE及配置会备份。", 20, 282, 990, 20, 0, 209)
	a.control("STATIC", "正常截图仅存内存，最多2帧/设备、总计32 MiB；异常截图在本机保留最多20组。", 20, 305, 990, 18, 0, 0)
	a.button("打开数据目录", 20, 326, 140, 111)
	a.button("查看验收说明", 175, 326, 140, 112)
	a.button("检查未完成更新", 330, 326, 170, 113)
	a.button("启动当前模拟器", 515, 326, 190, 114)
	a.button("启动本组5台", 720, 326, 190, 115)
	a.button("检测详情", 925, 326, 80, 116)
	a.control("STATIC", "正在自动查找雷电；已启动但 ADB 未就绪的实例也会显示。", 20, 373, 990, 25, 0, 208)
	a.label("目标角色", 20, 411, 80)
	role := a.control("COMBOBOX", "", 100, 405, 65, 160, 3, 210)
	for n := 1; n <= 5; n++ {
		comboAdd(role, strconv.Itoa(n))
	}
	send(role, 0x14E, 0, 0)
	a.button("启动游戏并进入角色", 180, 405, 215, 117)
	a.button("仅启动游戏", 410, 405, 125, 118)
	a.button("发现/绑定游戏", 550, 405, 145, 119)
	a.button("查看异常截图", 710, 405, 140, 120)
	a.button("标定指引", 865, 405, 140, 121)
	a.control("STATIC", "游戏状态：未检查；选择实例和目标角色后开始。", 20, 448, 985, 40, 0, 211)
	a.control("EDIT", "", 20, 495, 985, 210, wsBorder|0x200000|4|0x40|0x800, 900)
	a.log("preview.19及此前的在线更新、雷电发现、单台/五台启动、ADB和游戏包名启动已由用户实机验收；新增功能另行验收。")
	a.log("当前版本 " + version + "：启动后自动搜索管理程序和ADB，空闲时每12秒刷新；已核验路径自动记住。具体路径与原因见“检测详情”。")
	a.log("配置位置：" + a.root)
}
func (a *app) work(f func(context.Context) error) {
	if !a.busy.CompareAndSwap(false, true) {
		a.log("已有操作运行，请先停止或等待完成。")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	background := a.backgroundScan
	if !background {
		for _, id := range []int{117, 118, 119, 121} {
			user32.NewProc("EnableWindow").Call(a.controls[id], 0)
		}
	}
	go func() {
		err := f(ctx)
		if err != nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		cancel()
		a.ui(func() {
			a.busy.Store(false)
			a.cancel = nil
			a.backgroundScan = false
			for _, id := range []int{117, 118, 119, 121} {
				user32.NewProc("EnableWindow").Call(a.controls[id], 1)
			}
			a.restoreTargetRole()
			if err != nil && !(background && errors.Is(err, context.Canceled)) {
				a.log("操作停止：" + err.Error())
				if a.updating {
					setText(a.controls[209], "更新未完成："+err.Error())
				}
			}
			a.updating = false
			if a.closing {
				destroyWindow.Call(a.hwnd)
				return
			}
			if a.pendingAction != 0 {
				id := a.pendingAction
				a.pendingAction = 0
				a.handle(id)
				return
			}
			if id := a.pendingStudioAction; id != 0 && a.workbench != nil {
				a.pendingStudioAction = 0
				a.workbench.proc(a.workbench.hwnd, wmCommand, uintptr(id), 0)
			}
		})
	}()
}
func (a *app) save() error {
	c := a.cfg
	c.InstallDir = strings.TrimSpace(text(a.controls[201]))
	c.ManifestURL = strings.TrimSpace(text(a.controls[207]))
	n, e := strconv.Atoi(text(a.controls[204]))
	if e != nil {
		return errors.New("重试次数必须是整数")
	}
	c.Retries = n
	if d, e := a.selected(); e == nil {
		c.TargetRoles[strconv.Itoa(d.Index)] = comboIndex(a.controls[210]) + 1
	}
	c.GroupStart, e = strconv.Atoi(strings.TrimSpace(text(a.controls[205])))
	if e != nil {
		return errors.New("五开首编号必须是整数")
	}
	if c.GroupStart != a.cfg.GroupStart {
		a.groupSeeded = true
	}
	if e = c.Save(a.root); e != nil {
		return e
	}
	a.cfg = c
	return nil
}
func (a *app) selected() (ldplayer.Instance, error) {
	i := comboIndex(a.controls[202])
	if i < 0 || i >= len(a.devices) {
		return ldplayer.Instance{}, errors.New("请先检测并选择实例")
	}
	d := a.devices[i]
	if d.Unmapped {
		return d, errors.New("已发现ADB设备，但雷电实例编号尚未核对；没有执行游戏操作")
	}
	if a.client.ADB == "" {
		return d, errors.New("未定位游戏控制用adb.exe；检测结果保留，游戏操作未开始")
	}
	if d.Serial == "" {
		return d, errors.New("该实例未就绪或尚未验证 ADB 身份")
	}
	return d, nil
}
func (a *app) bindJob() {
	d, e := a.selected()
	if e != nil {
		a.log(e.Error())
		return
	}
	jobs := []string{"合欢", "天音", "青云", "鬼王"}
	j := comboIndex(a.controls[203])
	if j < 0 || j >= len(jobs) {
		return
	}
	a.cfg.Jobs[strconv.Itoa(d.Index)] = [5]string{jobs[j], jobs[j], jobs[j], jobs[j], jobs[j]}
	delete(a.cfg.SingleValidations, strconv.Itoa(d.Index))
	if e = a.save(); e != nil {
		a.log(e.Error())
		return
	}
	a.log(fmt.Sprintf("实例 %d 的五个角色已人工绑定为 %s；职业尚非自动识别。", d.Index, jobs[j]))
}

type boundDevice struct {
	client ldplayer.Client
	device ldplayer.Instance
	cache  *automation.Cache
}

func (d boundDevice) Capture(ctx context.Context) ([]byte, error) {
	b, e := d.client.Screenshot(ctx, d.device)
	if e == nil {
		d.cache.Add(strconv.Itoa(d.device.Index), b)
	}
	return b, e
}
func (d boundDevice) Tap(ctx context.Context, x, y int) error {
	return d.client.Tap(ctx, d.device, x, y)
}
func (a *app) errorCapture(d ldplayer.Instance, b []byte, err error) {
	dir := filepath.Join(a.root, "diagnostics")
	if e := os.MkdirAll(dir, 0700); e != nil {
		a.logAsync("异常资料保存失败：" + e.Error())
		return
	}
	base := fmt.Sprintf("error-%020d-%d", time.Now().UnixNano(), d.Index)
	if len(b) > 0 && len(b) <= 8<<20 {
		if _, e := automation.Decode(b); e == nil {
			_ = filesafe.Write(filepath.Join(dir, base+".png"), b, 0600)
		}
	}
	_ = filesafe.JSON(filepath.Join(dir, base+".json"), map[string]any{"instance": d.Index, "instance_name": d.Name, "adb_serial": d.Serial, "android_id": d.AndroidID, "boot_id": d.BootID, "screenshot_sha256": fmt.Sprintf("%x", sha256.Sum256(b)), "resources": "not_read", "counters": "not_read", "captured_at": time.Now().UTC().Format(time.RFC3339Nano), "error": err.Error(), "version": version})
	entries, _ := filepath.Glob(filepath.Join(dir, "error-*.json"))
	if len(entries) > 20 {
		for _, p := range entries[:len(entries)-20] {
			_ = os.Remove(p)
			_ = os.Remove(strings.TrimSuffix(p, ".json") + ".png")
		}
	}
}
func (a *app) cycle(five bool) {
	if e := a.save(); e != nil {
		a.log(e.Error())
		return
	}
	d, e := a.selected()
	if e != nil {
		a.log(e.Error())
		return
	}
	chosen := []ldplayer.Instance{d}
	if five {
		start, e := strconv.Atoi(text(a.controls[205]))
		if e != nil {
			a.log("五开首编号必须是整数")
			return
		}
		chosen = nil
		for n := start; n < start+5; n++ {
			found := false
			for _, x := range a.devices {
				if x.Index == n && x.Serial != "" {
					chosen = append(chosen, x)
					found = true
				}
			}
			if !found {
				a.log(fmt.Sprintf("编号 %d 未就绪，五开未开始。", n))
				return
			}
		}
	}
	profiles := make([]automation.Profile, len(chosen))
	counts := map[string]int{}
	for i, x := range chosen {
		p, e := loadProfile(a.root, x.Index)
		if e != nil {
			a.log(e.Error())
			return
		}
		profiles[i] = p
		jobs := a.cfg.Jobs[strconv.Itoa(x.Index)]
		if jobs[0] == "" {
			a.log(fmt.Sprintf("实例 %d 未绑定职业", x.Index))
			return
		}
		for _, j := range jobs {
			if j != jobs[0] {
				a.log("同一实例五角色职业不一致")
				return
			}
		}
		counts[jobs[0]]++
		if five {
			proof := a.cfg.SingleValidations[strconv.Itoa(x.Index)]
			if proof.ProfileSHA != profileHash(p) || proof.AndroidID != x.AndroidID {
				a.log(fmt.Sprintf("实例 %d 尚未通过当前标定下的单机五角色闭环，五开未开始。", x.Index))
				return
			}
		}
	}
	if five && (counts["合欢"] != 1 || counts["天音"] != 1 || counts["青云"] != 1 || counts["鬼王"] != 2) {
		a.log("五开职业绑定须为合欢、天音、青云、鬼王、鬼王。")
		return
	}
	if !a.ask("将进行登录/退出测试，依次验证五个角色，再返回登录。\n不记录任务完成，不切换区服。请确认模拟器没有其他控制程序同时点击。\n开始？") {
		return
	}
	cfg := a.cfg
	client := a.client
	a.work(func(ctx context.Context) error {
		group, cancel := context.WithCancel(ctx)
		defer cancel()
		var wg sync.WaitGroup
		errs := make(chan error, len(chosen))
		for i, x := range chosen {
			wg.Add(1)
			go func(i int, x ldplayer.Instance) {
				defer wg.Done()
				eng := automation.Engine{Device: boundDevice{client, x, a.cache}, Profile: profiles[i], Options: automation.Options{Retries: cfg.Retries, MinDelayMS: cfg.MinDelayMS, MaxDelayMS: cfg.MaxDelayMS, StateTimeout: 18 * time.Second, OnLog: func(s string) { a.logAsync(fmt.Sprintf("[%d] %s", x.Index, s)) }, OnError: func(b []byte, e error) { a.errorCapture(x, b, e) }}}
				if e := eng.Cycle(group, []int{1, 2, 3, 4, 5}); e != nil {
					errs <- fmt.Errorf("实例 %d: %w", x.Index, e)
					cancel()
				}
			}(i, x)
		}
		wg.Wait()
		close(errs)
		var all []error
		for e := range errs {
			all = append(all, e)
		}
		if len(all) > 0 {
			return errors.Join(all...)
		}
		a.ui(func() {
			if !five {
				a.cfg.SingleValidations[strconv.Itoa(d.Index)] = proof{profileHash(profiles[0]), d.AndroidID, time.Now().Format(time.RFC3339)}
				if e := a.cfg.Save(a.root); e != nil {
					a.log("测试通过但验证记录保存失败：" + e.Error())
					return
				}
			}
			a.log("本次五角色登录/返回登录闭环已通过画面验证；任务进度没有改变。")
		})
		return nil
	})
}
func (a *app) checkRecognition() {
	d, e := a.selected()
	if e != nil {
		a.log(e.Error())
		return
	}
	p, e := loadProfile(a.root, d.Index)
	if e != nil {
		a.log(e.Error())
		return
	}
	p, e = effectiveProfile(p)
	if e != nil {
		a.log(e.Error())
		return
	}
	client := a.client
	a.work(func(ctx context.Context) error {
		b, e := client.Screenshot(ctx, d)
		if e != nil {
			return e
		}
		a.cache.Add(strconv.Itoa(d.Index), b)
		img, e := automation.Decode(b)
		if e != nil {
			return e
		}
		var matches []string
		for _, s := range calibrationNames {
			if p.Require(img, s.key) == nil {
				matches = append(matches, s.label)
			}
		}
		if len(matches) == 0 {
			return errors.New("当前画面未识别；没有进行点击")
		}
		a.logAsync("当前识别：" + strings.Join(matches, "；"))
		return nil
	})
}
func (a *app) onlineUpdate() {
	// Updating preserves the saved configuration verbatim and must not depend
	// on unfinished edits in unrelated game/emulator controls.
	raw := strings.TrimSpace(text(a.controls[207]))
	if raw == "" {
		raw = a.cfg.ManifestURL
	}
	a.updating = true
	a.work(func(ctx context.Context) error {
		a.updateLog("正在检查新版本……")
		m, newer, e := update.Check(ctx, update.Client(), raw, version)
		if e != nil {
			return fmt.Errorf("更新检查失败，当前程序未改动：%w", e)
		}
		if !newer {
			a.updateLog("当前已是最新版本（" + version + "）。")
			return nil
		}
		a.updateLog("发现新版本 " + m.Version + "，正在自动下载并校验SHA-256……")
		target, e := os.Executable()
		if e != nil {
			return e
		}
		parent := filepath.Join(filepath.Dir(target), ".mhx-updates")
		if e = os.MkdirAll(parent, 0700); e != nil {
			return fmt.Errorf("程序目录不可写，请将新版本放到可写的新文件夹：%w", e)
		}
		tx, e := os.MkdirTemp(parent, "tx-")
		if e != nil {
			return e
		}
		stage, e := update.Download(ctx, update.Client(), m, tx)
		if e != nil {
			return e
		}
		plan, e := update.NewPlan(target, stage, m.Version, m.SHA256)
		if e != nil {
			return e
		}
		plan, e = update.WithConfig(plan, a.root)
		if e != nil {
			return e
		}
		helper := filepath.Join(tx, "updater.exe")
		if e = filesafe.CopyNew(target, helper); e != nil {
			return e
		}
		if e = update.Verify(helper, plan.OldSHA); e != nil {
			return e
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		cmd := exec.Command(helper, "--apply-update", filepath.Join(tx, "plan.json"))
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if e = cmd.Start(); e != nil {
			return e
		}
		a.ui(func() {
			a.log("校验通过，正在安全退出，由独立更新器备份EXE和配置、替换并重启。备份：" + plan.Backup)
			destroyWindow.Call(a.hwnd)
		})
		return nil
	})
}
func (a *app) pendingUpdates() {
	target, e := os.Executable()
	if e != nil {
		a.log(e.Error())
		return
	}
	plans, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".mhx-updates", "tx-*", "plan.json"))
	if len(plans) == 0 {
		a.log("没有更新事务记录。")
		return
	}
	for _, p := range plans {
		var r update.Result
		if e = filesafe.ReadJSON(filepath.Join(filepath.Dir(p), "result.json"), &r); e != nil {
			a.log("未完成事务：" + p + "；文件均保留，请勿手动覆盖旧版。 ")
			continue
		}
		a.log(fmt.Sprintf("更新 %s：%s %s；备份：%s", r.Version, r.Status, r.Detail, r.Backup))
	}
}
func (a *app) handle(id int) {
	if a.updateResultPath != "" && !a.updateResultDone && id != 111 && id != 116 {
		a.log("正在确认升级结果，请稍候。")
		return
	}
	if a.busy.Load() && id != 107 && id != 111 && id != 116 {
		if (a.backgroundScan && id >= 101 && id <= 121) || id == 110 {
			a.pendingAction = id
			if a.cancel != nil {
				a.cancel()
			}
			if id == 110 {
				a.log("正在安全结束当前操作，随后自动检查更新。")
			} else {
				a.log("正在结束后台检测，随后执行所选操作。")
			}
			return
		}
		a.log("操作进行中，请先停止或等待完成。")
		return
	}
	switch id {
	case 101:
		if p := chooseConsole(a.hwnd); p != "" {
			setText(a.controls[201], filepath.Dir(p))
			if e := a.save(); e != nil {
				a.log(e.Error())
			} else {
				a.detectInstances(false)
			}
		}
	case 102:
		a.detect()
	case 103:
		a.bindJob()
	case 104:
		if e := a.save(); e != nil {
			a.log(e.Error())
		} else {
			a.log("设置已保存，未知配置字段保留。")
		}
	case 105:
		a.evidenceCheck()
	case 106:
		a.openWorkbench()
	case 107:
		a.pendingAction = 0
		a.pendingStudioAction = 0
		if a.cancel != nil {
			a.cancel()
			a.log("正在停止；不会再开始下一次点击。")
			setText(a.controls[211], "已请求停止，等待当前ADB调用安全退出；不会开始下一步点击。")
		}
	case 108:
		a.placedCalibration = 0
		a.startCalibration()
	case 109:
		a.checkRecognition()
	case 110:
		a.onlineUpdate()
	case 111:
		_ = exec.Command("explorer.exe", a.root).Start()
	case 112:
		a.info("preview.19及此前在线更新、雷电24实例、单台/五台启动、ADB和包名启动已由用户实机通过。\n新版新增内置候选识别、广告外框关闭和五开工作台；已有本机标定优先。\n工作台可自选五台、打开已绑定游戏、保存区服/批次计划及记账订单，不自动执行任务、战斗或真实发货。\n角色昵称未核验时只报告部分成功；职业候选不确定时保留人工绑定。\n所有升级继续只点“一键检查更新”，原设置、标定和进度/订单数据保留。真实机器故障回滚仍未验收。")
	case 113:
		a.pendingUpdates()
	case 114:
		a.startEmulators(false)
	case 115:
		a.startEmulators(true)
	case 116:
		a.detectionDetails()
	case 117:
		a.startGame(true)
	case 118:
		a.startGame(false)
	case 119:
		a.discoverGame(0)
	case 120:
		dir := filepath.Join(a.root, "diagnostics")
		_ = os.MkdirAll(dir, 0700)
		_ = exec.Command("explorer.exe", dir).Start()
	case 121:
		a.loginCalibrationGuide()
	}
}
func windowProc(h uintptr, m uint32, w, l uintptr) uintptr {
	a := active
	if a != nil && a.cal != nil && h == a.cal.hwnd {
		return a.cal.proc(h, m, w, l)
	}
	if a != nil && a.gameSelector != nil && h == a.gameSelector.hwnd {
		return a.gameSelector.proc(h, m, w, l)
	}
	if a != nil && a.workbench != nil && h == a.workbench.hwnd {
		return a.workbench.proc(h, m, w, l)
	}
	if a != nil && h == a.hwnd {
		switch m {
		case wmCommand:
			// EDIT/COMBOBOX notifications also arrive as WM_COMMAND. Only user
			// button clicks are actions; internal field refreshes must not log or queue work.
			id := int(w & 0xffff)
			if id == 202 && w>>16 == 1 {
				a.restoreTargetRole()
			}
			if id >= 101 && id <= 121 && w>>16 == 0 {
				a.handle(id)
			}
			return 0
		case wmApp:
			for {
				select {
				case f := <-a.actions:
					f()
					a.fitOpenWindows()
				default:
					return 0
				}
			}
		case 5:
			a.fitClientControls()
			return 0
		case wmTimer:
			a.tick()
			return 0
		case wmClose:
			if a.cal != nil {
				a.info("请先关闭标定窗口。")
				return 0
			}
			if a.busy.Load() {
				if a.backgroundScan {
					a.closing = true
					if a.cancel != nil {
						a.cancel()
					}
					return 0
				}
				if a.ask("操作正在运行。停止后退出？") {
					a.closing = true
					if a.cancel != nil {
						a.cancel()
					}
				}
				return 0
			}
			destroyWindow.Call(h)
			return 0
		case wmDestroy:
			postQuit.Call(0)
			return 0
		}
	}
	r, _, _ := defWindow.Call(h, uintptr(m), w, l)
	return r
}
func (a *app) tick() {
	if !a.healthDone && a.healthPath != "" {
		if e := studio.VerifyFile(a.root); e != nil {
			a.log("进度/订单数据库健康检查失败，拒绝确认升级：" + e.Error())
			return
		}
		visible, _, _ := user32.NewProc("IsWindowVisible").Call(a.hwnd)
		if visible == 0 {
			showWindow.Call(a.hwnd, 9)
			user32.NewProc("UpdateWindow").Call(a.hwnd)
			return
		}
		if a.healthVersion != version {
			a.log("健康检查版本不符，拒绝确认。")
			return
		}
		exe, e := os.Executable()
		if e != nil {
			return
		}
		hash, e := update.Hash(exe)
		if e != nil {
			return
		}
		if !a.startupBackupDone {
			var plan update.Plan
			if e = filesafe.ReadJSON(filepath.Join(filepath.Dir(a.healthPath), "plan.json"), &plan); e != nil {
				a.log("无法读取更新事务：" + e.Error())
				return
			}
			if plan.Token != a.healthToken || plan.Version != version || !strings.EqualFold(plan.NewSHA, hash) || !strings.EqualFold(plan.Target, exe) {
				a.log("更新事务与当前程序不匹配，未确认健康状态。")
				return
			}
			backup, e := update.EnsureStartupConfigBackup(plan, a.root)
			if e != nil {
				a.log("配置备份核对失败：" + e.Error())
				return
			}
			a.startupBackupDone = true
			a.log("配置快照已核对：" + backup)
		}
		if e = filesafe.JSON(a.healthPath, update.Receipt{Version: version, Token: a.healthToken, SHA256: hash}); e != nil {
			a.log("健康确认写入失败：" + e.Error())
		}
	}
	a.healthDone = true
	a.pollUpdateResult()
	if a.smokePath != "" {
		visible, _, _ := user32.NewProc("IsWindowVisible").Call(a.hwnd)
		_ = filesafe.JSON(a.smokePath, map[string]any{"version": version, "source_commit": sourceCommit, "window_created": a.hwnd != 0, "window_visible": visible != 0, "message_loop": true, "controls": len(a.controls), "windows_game_tested": false})
		destroyWindow.Call(a.hwnd)
		return
	}
	if a.updateResultPath != "" && !a.updateResultDone {
		return
	}
	if a.maintainStudio() {
		return
	}
	if !a.busy.Load() && a.cal == nil && a.gameSelector == nil && !a.closing && time.Now().After(a.nextScan) {
		a.detectInstances(false)
	}
}
func main() {
	runtime.LockOSThread()
	var apply, receipt, token, healthVersion, smoke, data, result string
	var self bool
	flag.StringVar(&apply, "apply-update", "", "")
	flag.StringVar(&receipt, "health-receipt", "", "")
	flag.StringVar(&token, "health-token", "", "")
	flag.StringVar(&healthVersion, "health-version", "", "")
	flag.StringVar(&smoke, "smoke-test", "", "")
	flag.StringVar(&data, "data-dir", "", "")
	flag.StringVar(&result, "update-result", "", "")
	flag.BoolVar(&self, "self-test", false, "")
	flag.Parse()
	if self {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"version": version, "source_commit": sourceCommit, "os": runtime.GOOS, "arch": runtime.GOARCH, "windows_game_tested": false})
		return
	}
	if apply == "" {
		exe, _ := os.Executable()
		if strings.EqualFold(filepath.Base(exe), "updater.exe") {
			planPath := filepath.Join(filepath.Dir(exe), "plan.json")
			var p update.Plan
			if filesafe.ReadJSON(planPath, &p) == nil {
				r, _, _ := messageBox.Call(0, uintptr(unsafe.Pointer(utf("检测到更新恢复助手。仅在主程序缺失时从已校验备份恢复，不覆盖现有 EXE。继续？"))), uintptr(unsafe.Pointer(utf("MHX Studio 恢复"))), 4|0x30)
				if r == 6 {
					e := (update.Installer{}).Recover(p)
					if e != nil {
						messageBox.Call(0, uintptr(unsafe.Pointer(utf(e.Error()))), uintptr(unsafe.Pointer(utf("未执行恢复，文件保留"))), 0x10)
					} else {
						_, _ = (windowsRuntime{}).Launch(p.Target, "--update-result", filepath.Join(p.Dir(), "result.json"))
					}
				}
				return
			}
		}
	}
	if apply != "" {
		var p update.Plan
		if e := filesafe.ReadJSON(apply, &p); e != nil {
			os.Exit(2)
		}
		i := update.Installer{Runtime: windowsRuntime{}}
		if _, e := os.Stat(p.Target); os.IsNotExist(e) {
			if e = i.Recover(p); e == nil {
				_, _ = (windowsRuntime{}).Launch(p.Target, "--update-result", filepath.Join(p.Dir(), "result.json"))
				return
			}
		}
		if e := i.Install(context.Background(), p); e != nil {
			os.Exit(3)
		}
		return
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = os.Getenv("APPDATA")
	}
	if base == "" {
		base = os.TempDir()
	}
	if data == "" {
		data = filepath.Join(base, "MHXStudioNext")
	}
	if e := os.MkdirAll(data, 0700); e != nil {
		messageBox.Call(0, uintptr(unsafe.Pointer(utf(e.Error()))), uintptr(unsafe.Pointer(utf("MHX Studio 启动失败"))), 0x10)
		os.Exit(1)
	}
	defer func() {
		if r := recover(); r != nil {
			_ = filesafe.Write(filepath.Join(data, "startup-error.txt"), []byte(fmt.Sprint(r)), 0600)
			messageBox.Call(0, uintptr(unsafe.Pointer(utf(fmt.Sprint(r)))), uintptr(unsafe.Pointer(utf("MHX Studio 错误，详情已保留"))), 0x10)
		}
	}()
	mutexName := fmt.Sprintf("Local\\MHXStudioNext-%x", sha256.Sum256([]byte(strings.ToLower(filepath.Clean(data)))))
	mutex, _, mutexErr := kernel32.NewProc("CreateMutexW").Call(0, 0, uintptr(unsafe.Pointer(utf(mutexName))))
	if mutex == 0 {
		messageBox.Call(0, uintptr(unsafe.Pointer(utf(mutexErr.Error()))), uintptr(unsafe.Pointer(utf("无法创建运行锁"))), 0x10)
		return
	}
	defer kernel32.NewProc("CloseHandle").Call(mutex)
	if mutexErr == syscall.Errno(183) {
		messageBox.Call(0, uintptr(unsafe.Pointer(utf("已有 MHX Studio 使用此数据目录，请先关闭该窗口。"))), uintptr(unsafe.Pointer(utf("MHX Studio"))), 0x40)
		return
	}
	cfg, e := loadSettings(data)
	if e != nil {
		messageBox.Call(0, uintptr(unsafe.Pointer(utf(e.Error()))), uintptr(unsafe.Pointer(utf("配置保留，启动已停止"))), 0x10)
		return
	}
	a := &app{controls: map[int]uintptr{}, actions: make(chan func(), 512), root: data, cfg: cfg, cache: automation.NewCache(2, 32<<20), healthPath: receipt, healthToken: token, healthVersion: healthVersion, smokePath: smoke}
	defer func() {
		if a.stateStore != nil {
			_ = a.stateStore.Close()
		}
	}()
	if result == "" && receipt != "" {
		result = filepath.Join(filepath.Dir(receipt), "result.json")
	}
	a.updateResultPath = result
	if receipt != "" {
		a.nextScan = time.Now().Add(4 * time.Second)
	}
	_, a.groupSeeded = cfg.raw["group_start_index"]
	active = a
	dpi := user32.NewProc("SetProcessDPIAware")
	_, _, _ = dpi.Call()
	inst, _, _ := moduleHandle.Call(0)
	cursor, _, _ := loadCursor.Call(0, 32512)
	name := utf("MHXStudioNextWindow")
	wc := wndclass{Size: uint32(unsafe.Sizeof(wndclass{})), Proc: syscall.NewCallback(windowProc), Instance: inst, Cursor: cursor, Background: 6, Name: name}
	if r, _, err := registerClass.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		messageBox.Call(0, uintptr(unsafe.Pointer(utf(err.Error()))), uintptr(unsafe.Pointer(utf("窗口注册失败"))), 0x10)
		return
	}
	x, y, width, height := fittedBounds(1050, 780)
	h, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(utf("知夏 · MHX Studio "+version))), 0x00CF0000|wsVisible, uintptr(x), uintptr(y), uintptr(width), uintptr(height), 0, 0, inst, 0)
	if h == 0 {
		messageBox.Call(0, uintptr(unsafe.Pointer(utf(err.Error()))), uintptr(unsafe.Pointer(utf("窗口创建失败"))), 0x10)
		return
	}
	a.hwnd = h
	a.build()
	a.fitClientControls()
	// The already-published preview.15 helper launches with SW_HIDE. The
	// first ShowWindow may honor STARTUPINFO, so restore explicitly again.
	showWindow.Call(h, 1)
	showWindow.Call(h, 9)
	user32.NewProc("UpdateWindow").Call(h)
	user32.NewProc("RedrawWindow").Call(h, 0, 0, 0x185)
	if result != "" {
		setText(a.controls[209], "新版本已启动，正在完成健康检查并确认升级结果……")
		a.pollUpdateResult()
	}
	setTimer.Call(h, 1, 1200, 0)
	var m msg
	for {
		r, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&m)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}
