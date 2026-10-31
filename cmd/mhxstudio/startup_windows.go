//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/ldplayer"
	"github.com/ab393911/MHX-Studio-Updates/internal/login"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
)

func (a *app) cancelOnOpen() {
	if !a.startupAttempted && a.cfg.Startup != nil && a.cfg.Startup.Enabled {
		a.startupAttempted = true
		a.log("本次打开软件的自动启动已取消；保存的下次启动方案不变。")
	}
}

func (g *groupLoginPanel) saveOnOpen() {
	if g.app.busy.Load() {
		setText(g.status, "操作进行中，先停止或等待完成。")
		return
	}
	if send(controlOf(g.hwnd, 538), 0xF0, 0, 0) != 1 {
		next := g.app.cfg
		next.Startup = &login.StartupPlan{Enabled: false}
		if e := next.Save(g.app.root); e != nil {
			setText(g.status, e.Error())
			return
		}
		g.app.cfg = next
		g.app.startupAttempted = true
		setText(g.status, "已关闭打开软件后自动运行；模拟器检测与手动启动继续保留。")
		return
	}
	spec := login.Spec{Server: strings.TrimSpace(text(g.server)), Role: comboIndex(g.role) + 1}
	p := login.StartupPlan{Enabled: true, Console: g.app.client.Console, Target: spec}
	p.RunTasks = send(controlOf(g.hwnd, 540), 0xF0, 0, 0) == 1
	if p.RunTasks {
		if _, e := g.app.stateStore.LatestPlan(); e != nil {
			setText(g.status, "先建立持久化五开计划，避免每次打开重复消耗；"+e.Error())
			return
		}
	}
	row := comboIndex(g.leader)
	if row < 0 || row >= len(g.devices) {
		setText(g.status, "请选择本组队长")
		return
	}
	p.LeaderIndex = g.devices[row].Index
	configured := append([]ldplayer.Instance(nil), g.devices...)
	for i, d := range configured {
		b := g.app.cfg.Games[strconv.Itoa(d.Index)]
		if !b.Confirmed || !b.Component.Valid() || b.AndroidID == "" {
			setText(g.status, fmt.Sprintf("实例%d尚无已确认游戏与设备绑定", d.Index))
			return
		}
		if d.AndroidID != "" && d.AndroidID != b.AndroidID {
			setText(g.status, fmt.Sprintf("实例%d设备身份已变化，先重新核对绑定", d.Index))
			return
		}
		p.Members = append(p.Members, login.StartupMember{Index: d.Index, AndroidID: b.AndroidID, Package: b.Package})
		configured[i].AndroidID = b.AndroidID // expected identity only; never a live ADB proof
	}
	if e := p.Validate(); e != nil {
		setText(g.status, e.Error())
		return
	}
	if _, e := g.members(spec, configured, false); e != nil {
		setText(g.status, e.Error())
		return
	}
	gate := &groupLoginPanel{app: g.app, devices: configured}
	if e := gate.planGate(spec); e != nil {
		setText(g.status, e.Error())
		return
	}
	next := g.app.cfg
	next.Startup = &p
	next.GroupLoginServer = spec.Server
	next.GroupLoginRole = spec.Role
	if e := next.Save(g.app.root); e != nil {
		setText(g.status, e.Error())
		return
	}
	g.app.cfg = next
	g.app.startupAttempted = true
	setText(g.status, "方案已保存。下次双击软件后，先自动启动这五台并登录目标角色；可在倒计时或运行中点停止。没有设置Windows开机启动。")
}

func (a *app) tryOnOpen() bool {
	p := a.cfg.Startup
	if p == nil || !p.Enabled || a.startupAttempted || a.busy.Load() || a.closing || a.updating || a.cal != nil || a.gameSelector != nil || a.workbench != nil || a.loginPanel != nil {
		return false
	}
	if !a.healthDone || a.updateResultPath != "" && !a.updateResultDone || a.lastInventory == "" {
		return false
	}
	if a.startupAt.IsZero() {
		a.startupAt = time.Now().Add(6 * time.Second)
		a.log("已保存自动启动方案：6秒后启动指定五台并登录；点击停止可取消本次。")
		return true
	}
	if time.Now().Before(a.startupAt) {
		setText(a.controls[211], fmt.Sprintf("自动启动倒计时%d秒：指定五台 → ADB → 已绑定游戏 → %s角色%d。可随时点停止。", int(time.Until(a.startupAt).Seconds())+1, p.Target.Server, p.Target.Role))
		return true
	}
	a.startupAttempted = true
	if e := p.Validate(); e != nil {
		a.log("自动启动停止：" + e.Error())
		return true
	}
	if !p.MatchesConsole(a.client.Console) {
		a.log("自动启动停止：当前雷电管理程序与保存方案不同，请重新核对方案。")
		return true
	}
	if a.stateStore == nil {
		s, e := studio.Open(a.root)
		if e == nil {
			e = s.RecoverInterrupted()
		}
		if e != nil {
			if s != nil {
				_ = s.Close()
			}
			a.log("自动启动停止，原数据保留：" + e.Error())
			return true
		}
		a.stateStore = s
	}
	plan := *p
	plan.Members = append([]login.StartupMember(nil), p.Members...)
	client := a.client
	var configured []ldplayer.Instance
	for _, m := range plan.Members {
		b := a.cfg.Games[strconv.Itoa(m.Index)]
		if !b.Confirmed || !b.Component.Valid() || b.Package != m.Package || b.AndroidID != m.AndroidID {
			a.log(fmt.Sprintf("自动启动停止：实例%d的游戏/设备绑定与方案不符。", m.Index))
			return true
		}
		found := false
		for _, d := range a.devices {
			if !d.Unmapped && d.Index == m.Index {
				if d.AndroidID != "" && d.AndroidID != m.AndroidID {
					a.log(fmt.Sprintf("自动启动停止：实例%d实际设备身份变化。", m.Index))
					return true
				}
				d.AndroidID = m.AndroidID
				configured = append(configured, d)
				found = true
				break
			}
		}
		if !found {
			a.log(fmt.Sprintf("自动启动停止：实例%d不在真实雷电清单中，不猜测其它实例。", m.Index))
			return true
		}
	}
	gate := &groupLoginPanel{app: a, devices: configured}
	if e := gate.planGate(plan.Target); e != nil {
		a.log("自动启动停止：" + e.Error())
		return true
	}
	if _, e := gate.members(plan.Target, configured, plan.RunTasks); e != nil {
		a.log("自动启动停止：" + e.Error())
		return true
	}
	a.work(func(ctx context.Context) (err error) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		step := func(s string) { a.ui(func() { setText(a.controls[211], "自动流程："+s); a.log(s) }) }
		report := login.Report{Spec: plan.Target, StartedAt: time.Now().UTC()}
		defer func() {
			report.FinishedAt = time.Now().UTC()
			if err != nil {
				report.Error = err.Error()
			}
			err = errors.Join(err, filesafe.JSON(filepath.Join(a.root, "profiles", "startup-last.json"), report))
		}()
		step("正在启动方案指定的五台；已运行实例保持原状。")
		all, e := client.Start(ctx, plan.Indices(), ldplayer.LaunchOptions{OnLog: a.logAsync})
		if e != nil {
			return e
		}
		all, e = client.Bind(ctx, all)
		a.ui(func() { a.showInstances(client, all, true) })
		if e != nil {
			return e
		}
		var chosen []ldplayer.Instance
		for _, m := range plan.Members {
			found := false
			for _, d := range all {
				if d.Index == m.Index && !d.Unmapped && d.Serial != "" && d.BootID != "" && d.AndroidID == m.AndroidID {
					chosen = append(chosen, d)
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("实例%d启动后ADB/设备身份核验失败，不启动任何游戏", m.Index)
			}
		}
		live := &groupLoginPanel{app: a, devices: chosen}
		if e = live.planGate(plan.Target); e != nil {
			return e
		}
		members, e := live.members(plan.Target, chosen, plan.RunTasks)
		if e != nil {
			return e
		}
		step("五台已启动；正在全组核对ADB、游戏绑定、页面与角色身份。")
		report, e = login.Run(ctx, plan.Target, members, login.Options{OnStatus: func(s login.Status) { step(fmt.Sprintf("实例%d %s：%s %s", s.Instance, s.State, s.Step, s.Error)) }})
		if e != nil {
			return e
		}
		if !report.Ready {
			return errors.New("五台角色未全部就位，不开始后续任务")
		}
		step("五台目标角色已就位并复核。本次只完成启动登录；未组队、未完成任务。")
		if plan.RunTasks {
			return a.runTeam(ctx, report, chosen, plan.LeaderIndex)
		}
		return nil
	})
	return true
}
