//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/evidence"
	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/gameapp"
	"github.com/ab393911/MHX-Studio-Updates/internal/ldplayer"
	"github.com/ab393911/MHX-Studio-Updates/internal/login"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
	"github.com/ab393911/MHX-Studio-Updates/internal/vision"
)

type groupLoginPanel struct {
	app                                *app
	hwnd, server, role, report, status uintptr
	leader                             uintptr
	devices                            []ldplayer.Instance
	states                             map[int]login.Status
}

func (p *workbench) openGroupLogin() {
	if p.app.loginPanel != nil {
		showWindow.Call(p.app.loginPanel.hwnd, 9)
		return
	}
	chosen, e := p.chosen()
	if e != nil {
		setText(p.status, e.Error())
		return
	}
	g := &groupLoginPanel{app: p.app, devices: chosen, states: map[int]login.Status{}}
	server := p.app.cfg.GroupLoginServer
	if server == "" {
		server = "一梦诛仙"
	}
	role := p.app.cfg.GroupLoginRole
	if role < 1 || role > 5 {
		role = 1
	}
	if plan, e := p.app.stateStore.LatestPlan(); e == nil {
		if phase, e := p.app.stateStore.Next(plan.ID, time.Now()); e == nil {
			server, role = phase.Server, phase.Slot
		}
	}
	p.app.loginPanel = g
	inst, _, _ := moduleHandle.Call(0)
	x, y, w, h := fittedBounds(800, 590)
	hwnd, _, _ := createWindow.Call(0, uintptr(unsafe.Pointer(utf("MHXStudioNextWindow"))), uintptr(unsafe.Pointer(utf("MHX Studio · 五台同槽登录准备"))), 0x00CA0000|wsVisible, uintptr(x), uintptr(y), uintptr(w), uintptr(h), p.app.hwnd, 0, inst, 0)
	if hwnd == 0 {
		p.app.loginPanel = nil
		p.app.log("五台登录窗口创建失败")
		return
	}
	g.hwnd = hwnd
	child(hwnd, "STATIC", "全组预检后登录；可勾选登录后组队及八星准备测试。不会自动换区、战斗或真实发货。", 18, 15, 750, 28, 0, 0)
	child(hwnd, "STATIC", "目标区服", 18, 55, 85, 25, 0, 0)
	g.server = child(hwnd, "EDIT", server, 105, 50, 240, 28, wsBorder, 534)
	child(hwnd, "STATIC", "共同角色槽位", 365, 55, 125, 25, 0, 0)
	g.role = child(hwnd, "COMBOBOX", "", 495, 50, 70, 180, 3, 535)
	for i := 1; i <= 5; i++ {
		comboAdd(g.role, strconv.Itoa(i))
	}
	send(g.role, 0x14E, uintptr(role-1), 0)
	child(hwnd, "BUTTON", "预检并登录所选五台", 18, 96, 250, 34, 0, 531)
	child(hwnd, "BUTTON", "停止", 285, 96, 130, 34, 0, 532)
	child(hwnd, "BUTTON", "关闭", 432, 96, 130, 34, 0, 533)
	child(hwnd, "BUTTON", "打开软件后自动\n启动本组并登录", 580, 47, 180, 45, 3|0x2000, 538)
	if p.app.cfg.Startup != nil && p.app.cfg.Startup.Enabled {
		send(controlOf(hwnd, 538), 0xF1, 1, 0)
	}
	child(hwnd, "BUTTON", "保存启动方案", 580, 96, 180, 34, 0, 539)
	g.status = child(hwnd, "STATIC", "当前只内置“一梦诛仙”区服文字特征；其它区服不猜测。按实例、区服及选中槽位核验，不需要昵称标定。", 18, 145, 745, 55, 0, 536)
	g.report = child(hwnd, "EDIT", "", 18, 210, 745, 260, wsBorder|0x200000|4|0x40|0x800, 537)
	child(hwnd, "BUTTON", "登录后继续组队及八星准备（测试）", 18, 485, 285, 28, 3, 540)
	child(hwnd, "STATIC", "队长", 350, 489, 60, 25, 0, 0)
	g.leader = child(hwnd, "COMBOBOX", "", 410, 482, 350, 180, 3, 541)
	leaderIndex := 0
	for i, d := range chosen {
		comboAdd(g.leader, fmt.Sprintf("实例%d %s", d.Index, d.Name))
		if p.app.cfg.Startup != nil && p.app.cfg.Startup.LeaderIndex == d.Index {
			leaderIndex = i
		}
	}
	send(g.leader, 0x14E, uintptr(leaderIndex), 0)
	if p.app.cfg.Startup != nil && p.app.cfg.Startup.RunTasks {
		send(controlOf(hwnd, 540), 0xF1, 1, 0)
	}
	for _, d := range chosen {
		g.states[d.Index] = login.Status{Instance: d.Index, State: "PENDING", Step: "等待启动；不会把历史记录当本次核验"}
	}
	g.render()
}
func (g *groupLoginPanel) render() {
	var lines []string
	for _, d := range g.devices {
		s := g.states[d.Index]
		label := map[string]string{"PENDING": "待预检", "RUNNING": "进行中", "VERIFIED": "已核验", "FAILED": "失败", "STOPPED": "已停止", "NEEDS_REVIEW": "待核实"}[s.State]
		lines = append(lines, fmt.Sprintf("实例%d：%s；%s", d.Index, label, s.Step))
		if s.Error != "" {
			lines = append(lines, "  "+s.Error)
		}
	}
	setText(g.report, strings.Join(lines, "\r\n"))
}
func (g *groupLoginPanel) planGate(spec login.Spec) error {
	plan, e := g.app.stateStore.LatestPlan()
	if e != nil {
		if e.Error() == "尚无五开计划" {
			return nil
		}
		return e
	}
	phase, e := g.app.stateStore.Next(plan.ID, time.Now())
	if errors.Is(e, studio.ErrPlanComplete) {
		return nil
	}
	if e != nil {
		return e
	}
	if phase.Server != spec.Server || phase.Slot != spec.Role {
		return fmt.Errorf("当前计划仍在%s角色%d，不能跳过未完成小组", phase.Server, phase.Slot)
	}
	wanted := map[int]string{}
	for _, a := range phase.Actors {
		wanted[a.Instance] = a.Identity
	}
	for _, d := range g.devices {
		id := sha256.Sum256([]byte(strings.ToLower(g.app.client.Console) + "|" + strconv.Itoa(d.Index) + "|" + d.AndroidID))
		if wanted[d.Index] != hex.EncodeToString(id[:]) {
			return errors.New("所选五台不是计划当前批次，不自动跳批")
		}
	}
	for _, t := range phase.Tasks {
		if t.State == studio.Running || t.State == studio.Review {
			return errors.New("当前任务正在执行或待核查，先核对状态，不自动退出角色")
		}
	}
	return nil
}
func (g *groupLoginPanel) start() {
	g.app.cancelOnOpen()
	if g.app.busy.Load() {
		setText(g.status, "已有操作运行，请等待或先点停止。")
		return
	}
	spec := login.Spec{Server: strings.TrimSpace(text(g.server)), Role: comboIndex(g.role) + 1}
	if e := g.planGate(spec); e != nil {
		setText(g.status, e.Error())
		return
	}
	needTasks := send(controlOf(g.hwnd, 540), 0xF0, 0, 0) == 1
	if needTasks {
		if _, e := g.app.stateStore.LatestPlan(); e != nil {
			setText(g.status, "先在工作台建立五开计划，防止重复消耗材料；"+e.Error())
			return
		}
	}
	leaderRow := comboIndex(g.leader)
	if leaderRow < 0 || leaderRow >= len(g.devices) {
		setText(g.status, "请选择本组队长")
		return
	}
	leaderIndex := g.devices[leaderRow].Index
	members, e := g.members(spec, g.devices, needTasks)
	if e != nil {
		setText(g.status, e.Error())
		return
	}
	next := g.app.cfg
	next.GroupLoginServer = spec.Server
	next.GroupLoginRole = spec.Role
	if e = next.Save(g.app.root); e != nil {
		setText(g.status, "登录设置未能保存，未启动："+e.Error())
		return
	}
	g.app.cfg = next
	for _, id := range []int{531, 534, 535, 538, 539, 540, 541} {
		user32.NewProc("EnableWindow").Call(controlOf(g.hwnd, id), 0)
	}
	setText(g.status, "开始全组只读预检；任何一台失败就取消本次其余操作，不执行任务或战斗。")
	g.app.work(func(ctx context.Context) error {
		report, e := login.Run(ctx, spec, members, login.Options{OnStatus: func(s login.Status) {
			g.app.ui(func() {
				if g.app.loginPanel == g {
					g.states[s.Instance] = s
					g.render()
				}
			})
		}})
		if e == nil && report.Ready && needTasks {
			e = g.app.runTeam(ctx, report, g.devices, leaderIndex)
		}
		// This is a last-run diagnostic, not durable game-task completion or a
		// reusable permission to act. It is covered by existing profiles/*.json backup.
		saved := filesafe.JSON(filepath.Join(g.app.root, "profiles", "group-login-last.json"), report)
		if saved != nil {
			e = errors.Join(e, fmt.Errorf("登录结果保存失败：%w", saved))
		}
		g.app.ui(func() {
			if g.app.loginPanel != g {
				return
			}
			for _, id := range []int{531, 534, 535, 538, 539, 540, 541} {
				user32.NewProc("EnableWindow").Call(controlOf(g.hwnd, id), 1)
			}
			if report.Ready && e == nil {
				setText(g.status, "五台同槽位角色及本次区服图像依据已复核；仅登录准备完成，未组队、未完成任务。")
				if needTasks {
					setText(g.status, "组队及八星准备已核验结束（可能含资源不足跳过）；未执行据点战斗、交易行整理或银锭发货。")
				}
			} else {
				message := "本组未通过就位核验；不开始组队或下一任务。"
				if report.Ready && needTasks {
					message = "角色登录已核验，后续任务未完成。"
				}
				if e != nil {
					message += " " + e.Error()
				}
				setText(g.status, message)
			}
		})
		return e
	})
}

// members builds guarded workers for either the manual or saved-on-open path.
func (g *groupLoginPanel) members(spec login.Spec, devices []ldplayer.Instance, readNames bool) ([]login.Member, error) {
	if readNames && g.app.ocr == nil {
		g.app.ocr = vision.NewNativeReader()
	}
	catalog, e := evidence.Load()
	if e != nil {
		return nil, e
	}
	serverPage, e := catalog.ServerPage(spec.Server)
	if e != nil {
		return nil, e
	}
	cfg, client := g.app.cfg, g.app.client
	var members []login.Member
	for _, d := range devices {
		key := strconv.Itoa(d.Index)
		binding := cfg.Games[key]
		raw, e := loadProfile(g.app.root, d.Index)
		if e != nil {
			return nil, fmt.Errorf("实例%d：%w", d.Index, e)
		}
		profile := catalog.Profile(raw).WithoutNicknamePages()
		if e = profile.Validate(); e != nil {
			return nil, e
		}
		enteredSlot := false

		identity := sha256.Sum256([]byte(client.Console + "|" + key + "|" + d.AndroidID + "|" + d.BootID + "|" + binding.Package))
		memberID := hex.EncodeToString(identity[:])
		shell := gameShell{client, d}
		device := &gameDevice{boundDevice: boundDevice{client, d, g.app.cache}, shell: shell, pkg: binding.Package}
		ad := &evidence.LoginDevice{Device: device, Catalog: catalog, Retries: cfg.Retries, MinDelayMS: cfg.MinDelayMS, MaxDelayMS: cfg.MaxDelayMS, OnLog: g.app.logAsync}
		guard := &automation.ServerGuard{Device: ad, Profile: profile, ServerPage: serverPage}
		var loginDevice automation.Device = guard
		named := &vision.NamedRoleDevice{Device: guard, Profile: profile, Reader: g.app.ocr, ExpectedProfession: cfg.Jobs[key][0], VerifyProfession: catalog.FiveProfessions, MaxFailures: cfg.Retries, SkipNicknameCheck: true}
		if readNames {
			loginDevice = named
		}
		jobs := cfg.Jobs[key]
		job := jobs[0]
		for _, j := range jobs {
			if j != job {
				return nil, fmt.Errorf("实例%d五个角色的职业绑定不一致", d.Index)
			}
		}
		proof := func(ctx context.Context) (login.Proof, error) {
			result := login.Proof{Identity: memberID, Server: spec.Server, Role: spec.Role, ServerVerified: guard.Verified, ServerScreenSHA: guard.ServerScreenSHA}
			if named.Verified {
				result.CharacterName = named.Names[spec.Role-1]
				result.CharacterNameVerified = true
			}
			result.ProfessionVerified = named.ProfessionVerified
			if readNames && named.Error != nil {
				result.CharacterNameError = named.Error.Error()
			}
			for n := 0; n < 2; n++ {
				shot, e := loginDevice.Capture(ctx)
				if e != nil {
					return result, e
				}
				img, e := automation.Decode(shot)
				if e != nil {
					return result, e
				}
				state, e := profile.Recognize(img)
				if e != nil {
					return result, e
				}
				if state != "game" || !enteredSlot {
					return result, fmt.Errorf("最终页面为%s，本次未核验目标槽位进入游戏", state)
				}
				if e = profile.Require(img, state); e != nil {
					return result, e
				}
				result.EnteredGame = true
				result.IdentityVerified = true
				h := sha256.Sum256(shot)
				result.ScreenSHA = hex.EncodeToString(h[:])
				result.ObservedAt = time.Now().UTC()
				if n == 0 {
					if e = groupWait(ctx, 250*time.Millisecond); e != nil {
						return result, e
					}
				}
			}
			result.ServerVerified = guard.Verified
			result.ServerScreenSHA = guard.ServerScreenSHA
			if !result.ServerVerified {
				return result, fmt.Errorf("区服依据不足：%w", automation.ErrServerUnverified)
			}
			return result, nil
		}
		diagnose := func(ctx context.Context, p login.Proof, err error) (login.Proof, error) {
			if err != nil && !errors.Is(err, context.Canceled) {
				g.app.errorCapture(d, device.last, fmt.Errorf("五台登录准备 server=%s role=%d：%w", spec.Server, spec.Role, err))
			}
			return p, err
		}
		m := login.Member{Instance: d.Index, Identity: memberID, Profession: job}
		m.Preflight = func(ctx context.Context) error {
			if !d.Running() || d.Serial == "" || d.AndroidID == "" || d.BootID == "" {
				return errors.New("请先启动所选五台并完成ADB验证")
			}
			if e := client.Verify(ctx, d); e != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return e
			}
			if e := gameapp.VerifyBinding(ctx, shell, binding, d.AndroidID); e != nil {
				return e
			}
			if e := profile.Validate(); e != nil {
				return e
			}
			for _, k := range []string{"roles", fmt.Sprintf("selected_%d", spec.Role), "game"} {
				if _, ok := profile.Pages[k]; !ok {
					return fmt.Errorf("缺少%s页面标定", k)
				}
			}
			for _, k := range []string{fmt.Sprintf("role_%d", spec.Role), "enter"} {
				if _, ok := profile.Buttons[k]; !ok {
					return fmt.Errorf("缺少%s按钮标定", k)
				}
			}
			return ctx.Err()
		}
		m.Enter = func(ctx context.Context) (login.Proof, error) {
			launched, e := gameapp.Open(ctx, shell, binding, d.AndroidID)
			if e != nil {
				return diagnose(ctx, login.Proof{}, e)
			}
			// Detect explicit scope failures immediately, instead of waiting through the
			// cold-start allowance on a known role page without server provenance.
			if _, e = guard.Capture(ctx); errors.Is(e, automation.ErrServerUnverified) {
				e = guard.RevealServer(ctx, catalog.RoleBackButton(), cfg.MinDelayMS, cfg.MaxDelayMS)
			}
			if e != nil {
				return diagnose(ctx, login.Proof{}, e)
			}
			engine := automation.Engine{Device: loginDevice, Profile: profile, Options: automation.Options{Retries: cfg.Retries, MinDelayMS: cfg.MinDelayMS, MaxDelayMS: cfg.MaxDelayMS, StateTimeout: 30 * time.Second, OnLog: func(s string) { g.app.logAsync(fmt.Sprintf("[%d] %s", d.Index, s)) }}}
			wait := time.Duration(0)
			if !launched.AlreadyForeground {
				wait = 40 * time.Second
			}
			r, e := engine.EnterRole(ctx, spec.Role, automation.EnterOptions{InitialWait: wait, SkipNicknameCheck: true})
			if errors.Is(e, automation.ErrServerUnverified) {
				if e = guard.RevealServer(ctx, catalog.RoleBackButton(), cfg.MinDelayMS, cfg.MaxDelayMS); e == nil {
					r, e = engine.EnterRole(ctx, spec.Role, automation.EnterOptions{SkipNicknameCheck: true})
				}
			}
			if e != nil {
				return diagnose(ctx, login.Proof{EnteredGame: r.EnteredGame, IdentityVerified: r.IdentityVerified}, e)
			}
			enteredSlot = r.SelectedRoleVerified && r.IdentityVerified
			p, e := proof(ctx)
			return diagnose(ctx, p, e)
		}
		m.Recheck = func(ctx context.Context) (login.Proof, error) { p, e := proof(ctx); return diagnose(ctx, p, e) }
		members = append(members, m)
	}
	return members, login.Validate(spec, members)
}
func groupWait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func (g *groupLoginPanel) proc(h uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case wmCommand:
		if w>>16 != 0 {
			return 0
		}
		switch int(w & 0xffff) {
		case 531:
			g.start()
		case 532:
			g.app.handle(107)
		case 533:
			if g.app.busy.Load() {
				g.app.handle(107)
			}
			destroyWindow.Call(h)
		case 539:
			g.saveOnOpen()
		}
		return 0
	case wmClose:
		if g.app.busy.Load() {
			g.app.handle(107)
		}
		destroyWindow.Call(h)
		return 0
	case wmDestroy:
		g.app.loginPanel = nil
		return 0
	}
	r, _, _ := defWindow.Call(h, uintptr(m), w, l)
	return r
}

func controlOf(hwnd uintptr, id int) uintptr {
	h, _, _ := user32.NewProc("GetDlgItem").Call(hwnd, uintptr(id))
	return h
}
