//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
	"unsafe"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/gameapp"
	"github.com/ab393911/MHX-Studio-Updates/internal/ldplayer"
)

// The accepted LDPlayer implementation is left intact. Every added Android
// query/launch goes through its original instance + Android/boot identity gate.
type gameShell struct {
	client ldplayer.Client
	device ldplayer.Instance
}

func (s gameShell) Shell(ctx context.Context, args ...string) ([]byte, error) {
	if e := s.client.Verify(ctx, s.device); e != nil {
		return nil, e
	}
	sub, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	r := s.client.Runner
	if r == nil {
		r = ldplayer.OSRunner{}
	}
	return r.Run(sub, s.client.ADB, append([]string{"-s", s.device.Serial, "shell"}, args...)...)
}

type gameDevice struct {
	boundDevice
	shell gameShell
	pkg   string
	last  []byte
}

func (d *gameDevice) Capture(ctx context.Context) ([]byte, error) {
	if e := gameapp.RequireForeground(ctx, d.shell, d.pkg); e != nil {
		return nil, e
	}
	b, e := d.boundDevice.Capture(ctx)
	if e == nil {
		d.last = b
	}
	return b, e
}
func (d *gameDevice) Tap(ctx context.Context, x, y int) error {
	if e := gameapp.RequireForeground(ctx, d.shell, d.pkg); e != nil {
		return e
	}
	return d.boundDevice.Tap(ctx, x, y)
}
func (a *app) restoreTargetRole() {
	d, e := a.selected()
	if e != nil {
		return
	}
	key := strconv.Itoa(d.Index)
	if key == a.roleInstanceKey {
		return
	}
	a.roleInstanceKey = key
	r := a.cfg.TargetRoles[key]
	if r < 1 || r > 5 {
		r = 1
	}
	send(a.controls[210], 0x14E, uintptr(r-1), 0)
	bound := "尚未绑定游戏"
	if b := a.cfg.Games[key]; b.Confirmed {
		bound = "已绑定 " + b.Package
	}
	setText(a.controls[211], fmt.Sprintf("当前模拟器：%d；目标角色：%d；%s；页面与步骤：待检查", d.Index, r, bound))
}
func (a *app) gameStatus(d ldplayer.Instance, role int, page, step string) {
	a.ui(func() {
		setText(a.controls[211], fmt.Sprintf("当前模拟器：%d；目标角色：%d；页面：%s；步骤：%s", d.Index, role, page, step))
	})
}
func (a *app) startGame(enter bool) {
	if e := a.save(); e != nil {
		a.log(e.Error())
		return
	}
	d, e := a.selected()
	if e != nil {
		a.log(e.Error())
		return
	}
	if !d.Running() || d.Serial == "" || d.AndroidID == "" {
		a.log("请先使用已验收的模拟器启动按钮，等待ADB验证后再启动游戏。")
		return
	}
	key := strconv.Itoa(d.Index)
	b := a.cfg.Games[key]
	if !b.Confirmed || b.AndroidID != d.AndroidID {
		id := 118
		if enter {
			id = 117
		}
		a.discoverGame(id)
		return
	}
	role := comboIndex(a.controls[210]) + 1
	profile, e := loadProfile(a.root, d.Index)
	if e != nil {
		a.log(e.Error())
		return
	}
	client, cfg := a.client, a.cfg
	a.work(func(ctx context.Context) (err error) {
		shell := gameShell{client, d}
		device := &gameDevice{boundDevice: boundDevice{client, d, a.cache}, shell: shell, pkg: b.Package}
		defer func() {
			if err != nil {
				if errors.Is(err, context.Canceled) {
					a.gameStatus(d, role, "已停止", "用户中止，未开始下一步动作")
					return
				}
				a.gameStatus(d, role, "已停止", err.Error())
				if len(device.last) == 0 && ctx.Err() == nil {
					shot, er := client.Screenshot(ctx, d)
					if er == nil {
						device.last = shot
					}
				}
				a.errorCapture(d, device.last, fmt.Errorf("package=%s target_role=%d: %w", b.Package, role, err))
			}
		}()
		a.gameStatus(d, role, "待检查", "核验已安装游戏与启动入口")
		launch, err := gameapp.Open(ctx, shell, b, d.AndroidID)
		if err != nil {
			return err
		}
		step := "已启动游戏并验证前台"
		if launch.AlreadyForeground {
			step = "游戏已在前台，直接识别当前页"
		} else if launch.ProcessWasRunning {
			step = "已恢复正在运行的游戏，没有强制重启"
		}
		a.logAsync(fmt.Sprintf("[%d] %s：%s", d.Index, step, b.Package))
		shot, err := device.Capture(ctx)
		if err != nil {
			return err
		}
		img, err := automation.Decode(shot)
		if err != nil {
			return err
		}
		page := "未知/未标定"
		if p, er := profile.Recognize(img); er == nil {
			page = p
		}
		a.gameStatus(d, role, page, step)
		if !enter {
			a.logAsync("仅启动游戏完成：ADB身份及游戏前台已核验；未声称登录或角色成功。")
			return nil
		}
		eng := automation.Engine{Device: device, Profile: profile, Options: automation.Options{Retries: cfg.Retries, MinDelayMS: cfg.MinDelayMS, MaxDelayMS: cfg.MaxDelayMS, StateTimeout: 30 * time.Second, OnLog: func(s string) { a.logAsync(fmt.Sprintf("[%d] %s", d.Index, s)) }}}
		result, err := eng.EnterRole(ctx, role, automation.EnterOptions{OnStatus: func(s automation.LoginStatus) { a.gameStatus(d, role, s.Page, s.Step) }})
		if err != nil {
			return err
		}
		if !result.IdentityVerified {
			return automation.ErrIdentityUnverified
		}
		a.logAsync(fmt.Sprintf("实例%d第%d个角色登录已由已标定身份特征验证；已停留游戏内，不执行任务。", d.Index, role))
		return nil
	})
}

type gameSelector struct {
	app               *app
	hwnd, combo, hint uintptr
	d                 ldplayer.Instance
	inv               gameapp.Inventory
	next              int
	shell             gameShell
}

func (a *app) discoverGame(next int) {
	d, e := a.selected()
	if e != nil {
		a.log(e.Error())
		return
	}
	if a.gameSelector != nil {
		return
	}
	shell := gameShell{a.client, d}
	a.work(func(ctx context.Context) error {
		inv, e := gameapp.Discover(ctx, shell)
		if e != nil {
			return e
		}
		a.ui(func() {
			c := &gameSelector{app: a, d: d, inv: inv, next: next, shell: shell}
			a.gameSelector = c
			c.open()
		})
		return nil
	})
}
func (c *gameSelector) open() {
	inst, _, _ := moduleHandle.Call(0)
	h, _, _ := createWindow.Call(0, uintptr(unsafe.Pointer(utf("MHXStudioNextWindow"))), uintptr(unsafe.Pointer(utf(fmt.Sprintf("确认轻享游戏 · 实例%d", c.d.Index)))), 0x00CA0000|wsVisible, 60, 120, 950, 330, c.app.hwnd, 0, inst, 0)
	if h == 0 {
		c.app.gameSelector = nil
		c.app.log("游戏选择窗口创建失败")
		return
	}
	c.hwnd = h
	user32.NewProc("EnableWindow").Call(c.app.hwnd, 0)
	child(h, "STATIC", "无法只凭包名确认轻享。请先在该模拟器中打开游戏，再读取前台并明确确认。", 20, 15, 900, 35, 0, 0)
	c.hint = child(h, "STATIC", "", 20, 55, 900, 45, 0, 0)
	c.combo = child(h, "COMBOBOX", "", 20, 110, 895, 350, 3|0x200000, 410)
	send(c.combo, 0x160, 1300, 0)
	child(h, "BUTTON", "确认所选是轻享并绑定", 20, 166, 260, 36, 0, 401)
	child(h, "BUTTON", "重新读取已打开的游戏", 300, 166, 260, 36, 0, 402)
	child(h, "BUTTON", "取消", 580, 166, 120, 36, 0, 403)
	child(h, "STATIC", "只读取安装应用和启动入口；确认前不会尝试启动任何候选程序。绑定仅保存在本机。", 20, 218, 895, 40, 0, 0)
	c.populate()
}
func (c *gameSelector) populate() {
	setText(c.hint, "当前前台包名："+c.inv.Foreground+"。请选择对应游戏入口；不认识时先打开游戏，再点“重新读取”。")
	send(c.combo, 0x14B, 0, 0)
	chosen, count := -1, 0
	for i, a := range c.inv.Apps {
		label := a.String()
		if a.Package == c.inv.Foreground {
			label = "[当前前台] " + label
			chosen = i
			count++
		}
		comboAdd(c.combo, label)
	}
	if count == 1 {
		send(c.combo, 0x14E, uintptr(chosen), 0)
	}
}
func (c *gameSelector) proc(h uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case wmCommand:
		if w>>16 != 0 {
			return 0
		}
		switch int(w & 0xffff) {
		case 401:
			i := comboIndex(c.combo)
			if i < 0 || i >= len(c.inv.Apps) {
				c.app.info("请先选择游戏入口，或在模拟器内打开游戏后重新读取。")
				return 0
			}
			b := gameapp.Binding{Component: c.inv.Apps[i], AndroidID: c.d.AndroidID, Confirmed: true}
			c.app.work(func(ctx context.Context) error {
				if e := gameapp.VerifyBinding(ctx, c.shell, b, c.d.AndroidID); e != nil {
					return e
				}
				c.app.ui(func() {
					if c.app.gameSelector != c {
						return
					}
					key := strconv.Itoa(c.d.Index)
					old, exists := c.app.cfg.Games[key]
					c.app.cfg.Games[key] = b
					if e := c.app.cfg.Save(c.app.root); e != nil {
						if exists {
							c.app.cfg.Games[key] = old
						} else {
							delete(c.app.cfg.Games, key)
						}
						c.app.log(e.Error())
						return
					}
					c.app.log(fmt.Sprintf("实例%d 已由用户确认游戏：%s；设备和启动入口已核验。", c.d.Index, b.Component.String()))
					c.app.roleInstanceKey = ""
					c.app.pendingAction = c.next
					destroyWindow.Call(c.hwnd)
				})
				return nil
			})
		case 402:
			c.app.work(func(ctx context.Context) error {
				inv, e := gameapp.Discover(ctx, c.shell)
				if e != nil {
					return e
				}
				c.app.ui(func() {
					if c.app.gameSelector == c {
						c.inv = inv
						c.populate()
					}
				})
				return nil
			})
		case 403:
			postMessage.Call(h, wmClose, 0, 0)
		}
		return 0
	case wmClose:
		if c.app.cancel != nil {
			c.app.cancel()
		}
		c.app.pendingAction = 0
		destroyWindow.Call(h)
		return 0
	case wmDestroy:
		c.app.gameSelector = nil
		user32.NewProc("EnableWindow").Call(c.app.hwnd, 1)
		return 0
	}
	r, _, _ := defWindow.Call(h, uintptr(m), w, l)
	return r
}

func init() {
	calibrationNames = append(calibrationNames, calibrationName{"game", "游戏主界面公共特征（仅确认进入，不证明角色身份）", "menu"})
}
func (a *app) loginCalibrationGuide() {
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
	r := comboIndex(a.controls[210]) + 1
	keys := []string{"notice", "tree", "roles", fmt.Sprintf("selected_%d", r), fmt.Sprintf("game_%d", r)}
	next := -1
	missing := ""
	for _, key := range keys {
		if _, ok := p.Pages[key]; !ok {
			for i, n := range calibrationNames {
				if n.key == key {
					if next < 0 {
						next = i
					}
					missing += "\n• " + n.label
				}
			}
		}
	}
	if next >= 0 {
		send(a.controls[206], 0x14E, uintptr(next), 0)
	}
	if missing == "" {
		missing = "\n本角色所需基础页面已有标定，请先点“只检查当前识别”验证。"
	}
	rows := []int{95, 169, 240, 311, 382}
	a.info(fmt.Sprintf("实例%d，目标角色%d。每个实际页面点“截图并框选标定”：两处稳定特征 + 一个按钮范围。\n待标定：%s\n\n参考中心：公告(480,480)、动画页(750,405)、本角色行(140,%d)、角色进入(850,488)、游戏菜单(930,320)。仅作找按钮参考，未经截图核验不点击。\n\n角色选中标定须包含本行高亮/昵称；游戏内标定须包含本角色昵称。没有实际出现的动画页可略过。\n缺少模板时会停止，不会按参考坐标盲点。", d.Index, r, missing, rows[r-1]))
}
