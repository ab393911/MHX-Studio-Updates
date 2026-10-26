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
}

var active *app

func utf(s string) *uint16 { return syscall.StringToUTF16Ptr(s) }
func ptr(s string) uintptr { return uintptr(unsafe.Pointer(utf(s))) }
func text(h uintptr) string {
	b := make([]uint16, 4096)
	getWindowText.Call(h, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return syscall.UTF16ToString(b)
}
func setText(h uintptr, s string) { setWindowText.Call(h, ptr(s)) }
func send(h uintptr, m uint32, a, b uintptr) uintptr {
	r, _, _ := sendMessage.Call(h, uintptr(m), a, b)
	return r
}
func (a *app) ask(s string) bool {
	r, _, _ := messageBox.Call(a.hwnd, ptr(s), ptr("MHX Studio"), 4|0x30)
	return r == 6
}
func (a *app) info(s string) { messageBox.Call(a.hwnd, ptr(s), ptr("MHX Studio"), 0x40) }
func (a *app) ui(f func())   { a.actions <- f; postMessage.Call(a.hwnd, wmApp, 0, 0) }
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
	out, _, _ := createWindow.Call(0, ptr(class), ptr(title), wsChild|wsVisible|style, uintptr(x), uintptr(y), uintptr(w), uintptr(h), parent, uintptr(id), inst, 0)
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
func comboAdd(h uintptr, s string)              { send(h, 0x143, 0, ptr(s)) }
func comboIndex(h uintptr) int                  { return int(int32(send(h, 0x147, 0, 0))) }
func (a *app) build() {
	a.label("MHX Studio "+version+"  |  更新与登录验证版", 20, 15, 1000)
	a.label("战斗未开发；登录流程需标定和实机验收；测试不会记录任务完成或自动换区。", 20, 44, 1000)
	a.label("雷电目录", 20, 83, 90)
	a.control("EDIT", a.cfg.InstallDir, 115, 78, 610, 28, wsBorder, 201)
	a.button("选择雷电程序", 740, 77, 140, 101)
	a.button("检测实例", 895, 77, 110, 102)
	a.label("当前实例", 20, 126, 90)
	a.control("COMBOBOX", "", 115, 120, 420, 300, 3|0x200000, 202)
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
	a.control("EDIT", "1", 350, 162, 70, 28, wsBorder, 205)
	a.button("保存设置", 440, 160, 110, 104)
	a.button("单机闭环：5角色", 565, 160, 180, 105)
	a.button("五开闭环测试", 760, 160, 150, 106)
	a.button("停止", 925, 160, 80, 107)
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
	a.button("一键检查并更新", 840, 248, 165, 110)
	a.label("正常截图仅存内存，最多2帧/设备、总计32 MiB；异常截图在本机保留最多20组。", 20, 295, 990)
	a.button("打开数据目录", 20, 326, 150, 111)
	a.button("查看验收说明", 185, 326, 150, 112)
	a.button("检查未完成更新", 350, 326, 180, 113)
	a.label("识别标定：每页框选两处稳定特征和按钮范围；角色页特征须包含昵称/选中标记。", 20, 373, 1000)
	a.control("EDIT", "", 20, 405, 985, 300, wsBorder|0x200000|4|0x40|0x800, 900)
	a.log("程序已启动。V3.3/V3.4 文件和数据未改动。当前版本未做用户 Windows + 雷电实机验收。")
	a.log("请先选择 ldconsole.exe 或 dnconsole.exe，检测实例，再标定当前实例。")
	a.log("配置位置：" + a.root)
}
func (a *app) work(f func(context.Context) error) {
	if !a.busy.CompareAndSwap(false, true) {
		a.log("已有操作运行，请先停止或等待完成。")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	go func() {
		err := f(ctx)
		cancel()
		a.ui(func() {
			a.busy.Store(false)
			a.cancel = nil
			if err != nil {
				a.log("操作停止：" + err.Error())
			}
			if a.closing {
				destroyWindow.Call(a.hwnd)
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
	if d.Serial == "" {
		return d, errors.New("该实例未就绪或尚未验证 ADB 身份")
	}
	return d, nil
}
func (a *app) detect() {
	if e := a.save(); e != nil {
		a.log(e.Error())
		return
	}
	console, adb := ldplayer.Find(a.cfg.InstallDir)
	if console == "" {
		a.log("未找到雷电。点“选择雷电程序”，选择安装目录内 ldconsole.exe 或 dnconsole.exe。")
		return
	}
	client := ldplayer.Client{Console: console, ADB: adb}
	a.work(func(ctx context.Context) error {
		all, e := client.Discover(ctx)
		if e != nil {
			return e
		}
		a.ui(func() {
			a.client = client
			a.devices = all
			send(a.controls[202], 0x14B, 0, 0)
			first := -1
			ready := 0
			for i, d := range all {
				state := "未运行"
				if d.Serial != "" {
					state = "已验证 " + d.Serial
					ready++
					if first < 0 {
						first = i
					}
				}
				comboAdd(a.controls[202], fmt.Sprintf("编号 %d  %s  %s", d.Index, d.Name, state))
			}
			if first >= 0 {
				send(a.controls[202], 0x14E, uintptr(first), 0)
				setText(a.controls[205], strconv.Itoa(all[first].Index))
			}
			a.log(fmt.Sprintf("检测到 %d 个实例，%d 个已运行且身份验证通过。编号直接采用雷电 list2 返回值。", len(all), ready))
		})
		return nil
	})
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
	_ = filesafe.JSON(filepath.Join(dir, base+".json"), map[string]any{"instance": d.Index, "error": err.Error(), "version": version})
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
	if e := a.save(); e != nil {
		a.log(e.Error())
		return
	}
	raw := a.cfg.ManifestURL
	a.work(func(ctx context.Context) error {
		a.logAsync("正在获取更新清单……")
		m, newer, e := update.Check(ctx, update.Client(), raw, version)
		if e != nil {
			return fmt.Errorf("更新检查失败，当前程序未改动：%w", e)
		}
		if !newer {
			a.logAsync("已核对清单：当前版本 " + version + "，服务器版本 " + m.Version + "，无需更新。")
			return nil
		}
		reply := make(chan bool, 1)
		a.ui(func() {
			reply <- a.ask(fmt.Sprintf("发现版本 %s\n%s\n\n下载并校验后，将保留当前 EXE 备份，再替换本程序；配置保持原位。\n新程序未通过启动检查时会自动回滚。\n确认更新？", m.Version, m.Notes))
		})
		select {
		case ok := <-reply:
			if !ok {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
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
		a.logAsync("正在下载；下载中断或 SHA-256 不符时不会替换主程序。")
		stage, e := update.Download(ctx, update.Client(), m, tx)
		if e != nil {
			return e
		}
		plan, e := update.NewPlan(target, stage, m.Version, m.SHA256)
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
			a.log("SHA-256 和 Windows x64 文件结构检查通过，正在退出并备份替换。备份：" + plan.Backup)
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
	if a.busy.Load() && id != 107 && id != 111 {
		a.log("操作进行中，请先停止或等待完成。")
		return
	}
	switch id {
	case 101:
		if p := chooseConsole(a.hwnd); p != "" {
			setText(a.controls[201], filepath.Dir(p))
			if e := a.save(); e != nil {
				a.log(e.Error())
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
		a.cycle(false)
	case 106:
		a.cycle(true)
	case 107:
		if a.cancel != nil {
			a.cancel()
			a.log("正在停止；不会再开始下一次点击。")
		}
	case 108:
		a.startCalibration()
	case 109:
		a.checkRecognition()
	case 110:
		a.onlineUpdate()
	case 111:
		_ = exec.Command("explorer.exe", a.root).Start()
	case 112:
		a.info("测试顺序：\n1. 新文件夹运行，保留 V3.3/V3.4。\n2. 选择雷电控制台程序，检测实例。\n3. 人工绑定职业，按每台实例分别标定两处特征与按钮。\n4. 先用“只检查当前识别”核对，再测单机五角色闭环。\n5. 五个实例全部单机通过后，再测五开。\n6. 一键检查更新：确认版本和备份提示后才替换。\n\n本版不执行战斗、银锭交付或自动换区；跳过任务不算完成。\n详细记录见发行包内 TESTING.md。")
	case 113:
		a.pendingUpdates()
	}
}
func windowProc(h uintptr, m uint32, w, l uintptr) uintptr {
	a := active
	if a != nil && a.cal != nil && h == a.cal.hwnd {
		return a.cal.proc(h, m, w, l)
	}
	if a != nil && h == a.hwnd {
		switch m {
		case wmCommand:
			a.handle(int(w & 0xffff))
			return 0
		case wmApp:
			for {
				select {
				case f := <-a.actions:
					f()
				default:
					return 0
				}
			}
		case wmTimer:
			a.tick()
			return 0
		case wmClose:
			if a.cal != nil {
				a.info("请先关闭标定窗口。")
				return 0
			}
			if a.busy.Load() {
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
	if a.healthDone {
		return
	}
	a.healthDone = true
	if a.healthPath != "" {
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
		if e = filesafe.JSON(a.healthPath, update.Receipt{Version: version, Token: a.healthToken, SHA256: hash}); e != nil {
			a.log("健康确认写入失败：" + e.Error())
		}
	}
	if a.smokePath != "" {
		_ = filesafe.JSON(a.smokePath, map[string]any{"version": version, "source_commit": sourceCommit, "window_created": a.hwnd != 0, "message_loop": true, "controls": len(a.controls), "windows_game_tested": false})
		destroyWindow.Call(a.hwnd)
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
				r, _, _ := messageBox.Call(0, ptr("检测到更新恢复助手。仅在主程序缺失时从已校验备份恢复，不覆盖现有 EXE。继续？"), ptr("MHX Studio 恢复"), 4|0x30)
				if r == 6 {
					e := (update.Installer{}).Recover(p)
					if e != nil {
						messageBox.Call(0, ptr(e.Error()), ptr("未执行恢复，文件保留"), 0x10)
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
		messageBox.Call(0, ptr(e.Error()), ptr("MHX Studio 启动失败"), 0x10)
		os.Exit(1)
	}
	defer func() {
		if r := recover(); r != nil {
			_ = filesafe.Write(filepath.Join(data, "startup-error.txt"), []byte(fmt.Sprint(r)), 0600)
			messageBox.Call(0, ptr(fmt.Sprint(r)), ptr("MHX Studio 错误，详情已保留"), 0x10)
		}
	}()
	mutexName := fmt.Sprintf("Local\\MHXStudioNext-%x", sha256.Sum256([]byte(strings.ToLower(filepath.Clean(data)))))
	mutex, _, mutexErr := kernel32.NewProc("CreateMutexW").Call(0, 0, ptr(mutexName))
	if mutex == 0 {
		messageBox.Call(0, ptr(mutexErr.Error()), ptr("无法创建运行锁"), 0x10)
		return
	}
	defer kernel32.NewProc("CloseHandle").Call(mutex)
	if mutexErr == syscall.Errno(183) {
		messageBox.Call(0, ptr("已有 MHX Studio 使用此数据目录，请先关闭该窗口。"), ptr("MHX Studio"), 0x40)
		return
	}
	cfg, e := loadSettings(data)
	if e != nil {
		messageBox.Call(0, ptr(e.Error()), ptr("配置保留，启动已停止"), 0x10)
		return
	}
	a := &app{controls: map[int]uintptr{}, actions: make(chan func(), 512), root: data, cfg: cfg, cache: automation.NewCache(2, 32<<20), healthPath: receipt, healthToken: token, healthVersion: healthVersion, smokePath: smoke}
	active = a
	dpi := user32.NewProc("SetProcessDPIAware")
	_, _, _ = dpi.Call()
	inst, _, _ := moduleHandle.Call(0)
	cursor, _, _ := loadCursor.Call(0, 32512)
	name := utf("MHXStudioNextWindow")
	wc := wndclass{Size: uint32(unsafe.Sizeof(wndclass{})), Proc: syscall.NewCallback(windowProc), Instance: inst, Cursor: cursor, Background: 6, Name: name}
	if r, _, err := registerClass.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		messageBox.Call(0, ptr(err.Error()), ptr("窗口注册失败"), 0x10)
		return
	}
	h, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(name)), ptr("知夏 · MHX Studio "+version), 0x00CF0000|wsVisible, 80, 40, 1050, 780, 0, 0, inst, 0)
	if h == 0 {
		messageBox.Call(0, ptr(err.Error()), ptr("窗口创建失败"), 0x10)
		return
	}
	a.hwnd = h
	a.build()
	if result != "" {
		var r update.Result
		if filesafe.ReadJSON(result, &r) == nil {
			a.log("上次更新：" + r.Status + "；" + r.Detail + "；备份 " + r.Backup)
		}
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
