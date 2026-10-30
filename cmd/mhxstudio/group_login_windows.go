//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
)

type groupLoginPanel struct {
	app                                *app
	hwnd, server, role, report, status uintptr
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
	x, y, w, h := fittedBounds(800, 550)
	hwnd, _, _ := createWindow.Call(0, uintptr(unsafe.Pointer(utf("MHXStudioNextWindow"))), uintptr(unsafe.Pointer(utf("MHX Studio · 五台同槽登录准备"))), 0x00CA0000|wsVisible, uintptr(x), uintptr(y), uintptr(w), uintptr(h), p.app.hwnd, 0, inst, 0)
	if hwnd == 0 {
		p.app.loginPanel = nil
		p.app.log("五台登录窗口创建失败")
		return
	}
	g.hwnd = hwnd
	child(hwnd, "STATIC", "只做登录准备：全组预检后执行，最后重新核验五台；不会组队、换区或执行任务。", 18, 15, 750, 28, 0, 0)
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
	g.status = child(hwnd, "STATIC", "当前只内置“一梦诛仙”区服文字特征；其它区服不猜测。请先完成每台目标角色昵称标定。", 18, 145, 745, 55, 0, 536)
	g.report = child(hwnd, "EDIT", "", 18, 210, 745, 275, wsBorder|0x200000|4|0x40|0x800, 537)
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
	orders, e := g.app.stateStore.Orders()
	if e != nil {
		return e
	}
	for _, o := range orders {
		if o.Held > 0 {
			return errors.New("订单仍有锁定额度，先核查交付；暂不自动切换角色")
		}
	}
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
	if g.app.busy.Load() {
		setText(g.status, "已有操作运行，请等待或先点停止。")
		return
	}
	spec := login.Spec{Server: strings.TrimSpace(text(g.server)), Role: comboIndex(g.role) + 1}
	if e := g.planGate(spec); e != nil {
		setText(g.status, e.Error())
		return
	}
	catalog, e := evidence.Load()
	if e != nil {
		setText(g.status, e.Error())
		return
	}
	serverPage, e := catalog.ServerPage(spec.Server)
	if e != nil {
		setText(g.status, e.Error())
		return
	}
	cfg, client := g.app.cfg, g.app.client
	var members []login.Member
	for _, d := range g.devices {
		key := strconv.Itoa(d.Index)
		binding := cfg.Games[key]
		raw, e := loadProfile(g.app.root, d.Index)
		if e != nil {
			setText(g.status, fmt.Sprintf("实例%d：%v", d.Index, e))
			return
		}
		identityPage, hasIdentity := raw.Pages[fmt.Sprintf("game_%d", spec.Role)]
		if !hasIdentity {
			setText(g.status, fmt.Sprintf("实例%d缺少第%d个角色的昵称标定；未启动本组", d.Index, spec.Role))
			return
		}
		profile := catalog.Profile(raw)
		pageBytes, _ := json.Marshal(identityPage)
		signature := sha256.Sum256(pageBytes)
		identity := sha256.Sum256([]byte(client.Console + "|" + key + "|" + d.AndroidID + "|" + d.BootID + "|" + binding.Package))
		memberID := hex.EncodeToString(identity[:])
		shell := gameShell{client, d}
		device := &gameDevice{boundDevice: boundDevice{client, d, g.app.cache}, shell: shell, pkg: binding.Package}
		ad := &evidence.LoginDevice{Device: device, Catalog: catalog, Retries: cfg.Retries, MinDelayMS: cfg.MinDelayMS, MaxDelayMS: cfg.MaxDelayMS, OnLog: g.app.logAsync}
		guard := &automation.ServerGuard{Device: ad, Profile: profile, ServerPage: serverPage}
		jobs := cfg.Jobs[key]
		job := jobs[0]
		for _, j := range jobs {
			if j != job {
				setText(g.status, fmt.Sprintf("实例%d五个角色的职业绑定不一致", d.Index))
				return
			}
		}
		proof := func(ctx context.Context) (login.Proof, error) {
			result := login.Proof{Identity: memberID, Server: spec.Server, Role: spec.Role, ServerVerified: guard.Verified, ServerScreenSHA: guard.ServerScreenSHA}
			for n := 0; n < 2; n++ {
				shot, e := guard.Capture(ctx)
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
				if state != fmt.Sprintf("game_%d", spec.Role) {
					return result, fmt.Errorf("最终页面为%s，不是已标定目标角色", state)
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
		m := login.Member{Instance: d.Index, Identity: memberID, Profession: job, CharacterSignature: hex.EncodeToString(signature[:])}
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
			for _, k := range []string{"roles", fmt.Sprintf("selected_%d", spec.Role), fmt.Sprintf("game_%d", spec.Role)} {
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
			if _, e = guard.Capture(ctx); e != nil {
				return diagnose(ctx, login.Proof{}, e)
			}
			engine := automation.Engine{Device: guard, Profile: profile, Options: automation.Options{Retries: cfg.Retries, MinDelayMS: cfg.MinDelayMS, MaxDelayMS: cfg.MaxDelayMS, StateTimeout: 30 * time.Second, OnLog: func(s string) { g.app.logAsync(fmt.Sprintf("[%d] %s", d.Index, s)) }}}
			wait := time.Duration(0)
			if !launched.AlreadyForeground {
				wait = 40 * time.Second
			}
			r, e := engine.EnterRole(ctx, spec.Role, automation.EnterOptions{InitialWait: wait})
			if e != nil {
				return diagnose(ctx, login.Proof{EnteredGame: r.EnteredGame, IdentityVerified: r.IdentityVerified}, e)
			}
			p, e := proof(ctx)
			return diagnose(ctx, p, e)
		}
		m.Recheck = func(ctx context.Context) (login.Proof, error) { p, e := proof(ctx); return diagnose(ctx, p, e) }
		members = append(members, m)
	}
	if e = login.Validate(spec, members); e != nil {
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
	for _, id := range []int{531, 534, 535} {
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
			for _, id := range []int{531, 534, 535} {
				user32.NewProc("EnableWindow").Call(controlOf(g.hwnd, id), 1)
			}
			if report.Ready && e == nil {
				setText(g.status, "五台同槽位角色及本次区服图像依据已复核；仅登录准备完成，未组队、未完成任务。")
			} else {
				message := "本组未通过就位核验；不开始组队或下一任务。"
				if e != nil {
					message += " " + e.Error()
				}
				setText(g.status, message)
			}
		})
		return e
	})
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
