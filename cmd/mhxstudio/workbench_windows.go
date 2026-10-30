//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/evidence"
	"github.com/ab393911/MHX-Studio-Updates/internal/gameapp"
	"github.com/ab393911/MHX-Studio-Updates/internal/ldplayer"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
)

type workbench struct {
	app                                                *app
	hwnd, list, servers, buyer, amount, status, report uintptr
	inventory                                          []ldplayer.Instance
	balance, merit                                     uintptr
}

func (a *app) openWorkbench() {
	if (a.healthPath != "" && !a.healthDone) || (a.updateResultPath != "" && !a.updateResultDone) {
		a.log("更新健康检查或提交确认尚未完成，请等待。")
		return
	}
	if a.workbench != nil {
		showWindow.Call(a.workbench.hwnd, 9)
		return
	}
	if a.stateStore == nil {
		s, e := studio.Open(a.root)
		if e != nil {
			a.log("本地进度数据打开失败，原文件保留：" + e.Error())
			return
		}
		a.stateStore = s
		if e = s.RecoverInterrupted(); e != nil {
			a.log(e.Error())
			return
		}
	}
	p := &workbench{app: a}
	a.workbench = p
	inst, _, _ := moduleHandle.Call(0)
	x, y, w, h := fittedBounds(1000, 730)
	hwnd, _, _ := createWindow.Call(0, uintptr(unsafe.Pointer(utf("MHXStudioNextWindow"))), uintptr(unsafe.Pointer(utf("MHX Studio 工作台 · 五开计划与订单"))), 0x00CA0000|wsVisible, uintptr(x), uintptr(y), uintptr(w), uintptr(h), a.hwnd, 0, inst, 0)
	if hwnd == 0 {
		a.workbench = nil
		a.log("工作台窗口创建失败")
		return
	}
	p.hwnd = hwnd
	child(hwnd, "STATIC", "本地进度与订单；游戏任务尚未开放执行，订单不会自动发货。点击列表可勾选/取消。", 20, 12, 945, 25, 0, 0)
	child(hwnd, "STATIC", "勾选任意五台真实实例", 20, 42, 270, 24, 0, 0)
	p.list = child(hwnd, "LISTBOX", "", 20, 69, 285, 215, wsBorder|0x200000|8|1, 510)
	child(hwnd, "STATIC", "区服顺序（逗号分隔；仅计划，不自动换服）", 325, 42, 620, 24, 0, 0)
	p.servers = child(hwnd, "EDIT", "一梦诛仙", 325, 69, 620, 28, wsBorder, 511)
	child(hwnd, "BUTTON", "启动所选五台", 325, 110, 180, 32, 0, 501)
	child(hwnd, "BUTTON", "打开所选五台游戏", 520, 110, 205, 32, 0, 502)
	child(hwnd, "BUTTON", "建立五开计划", 740, 110, 205, 32, 0, 503)
	child(hwnd, "BUTTON", "刷新进度", 325, 155, 150, 32, 0, 504)
	child(hwnd, "BUTTON", "海嘟嘟预检", 490, 155, 170, 32, 0, 505)
	child(hwnd, "BUTTON", "停止", 675, 155, 115, 32, 0, 506)
	child(hwnd, "BUTTON", "关闭", 805, 155, 140, 32, 0, 508)
	child(hwnd, "BUTTON", "追加所选五台到当前计划", 325, 204, 220, 32, 0, 521)
	p.status = child(hwnd, "STATIC", "", 560, 204, 385, 80, 0, 512)
	child(hwnd, "STATIC", "买家游戏ID", 20, 299, 120, 24, 0, 0)
	p.buyer = child(hwnd, "EDIT", "", 145, 294, 270, 28, wsBorder, 513)
	child(hwnd, "STATIC", "订单银锭（整万）", 435, 299, 160, 24, 0, 0)
	p.amount = child(hwnd, "EDIT", "", 600, 294, 170, 28, wsBorder, 514)
	child(hwnd, "BUTTON", "建立记账订单", 790, 292, 155, 32, 0, 507)
	child(hwnd, "STATIC", "余额", 20, 343, 55, 24, 0, 0)
	p.balance = child(hwnd, "EDIT", "", 75, 337, 145, 28, wsBorder, 516)
	child(hwnd, "STATIC", "功绩", 235, 343, 55, 24, 0, 0)
	p.merit = child(hwnd, "EDIT", "", 290, 337, 115, 28, wsBorder, 517)
	child(hwnd, "BUTTON", "计算可交付（手动核对）", 425, 335, 245, 32, 0, 509)
	child(hwnd, "BUTTON", "五台同槽登录准备…", 690, 335, 255, 32, 0, 522)
	p.report = child(hwnd, "EDIT", "", 20, 385, 925, 260, wsBorder|0x200000|4|0x40|0x800, 515)
	p.instances()
	p.refresh()
}
func (p *workbench) indices() ([]int, error) {
	n := int(send(p.list, 0x190, 0, 0))
	if n != 5 {
		return nil, fmt.Errorf("请恰好勾选五台，目前%d台", n)
	}
	indexes := make([]int32, 5)
	if int(send(p.list, 0x191, 5, uintptr(unsafe.Pointer(&indexes[0])))) != 5 {
		return nil, fmt.Errorf("无法读取五台选择")
	}
	var out []int
	for _, i := range indexes {
		if i < 0 || int(i) >= len(p.inventory) {
			return nil, fmt.Errorf("实例清单已变化，请重新选择")
		}
		out = append(out, p.inventory[i].Index)
	}
	return out, nil
}
func (p *workbench) instances() {
	chosen := map[int]bool{}
	n := int(send(p.list, 0x18B, 0, 0))
	for i := 0; i < n && i < len(p.inventory); i++ {
		if send(p.list, 0x187, uintptr(i), 0) == 1 {
			chosen[p.inventory[i].Index] = true
		}
	}
	p.inventory = nil
	for _, d := range p.app.devices {
		if !d.Unmapped {
			p.inventory = append(p.inventory, d)
		}
	}
	send(p.list, 0x184, 0, 0)
	for i, d := range p.inventory {
		j := p.app.cfg.Jobs[strconv.Itoa(d.Index)][0]
		if j == "" {
			j = "职业待绑定"
		}
		line := fmt.Sprintf("编号%d  %s  %s", d.Index, j, d.Status())
		sendMessage.Call(p.list, 0x180, 0, uintptr(unsafe.Pointer(utf(line))))
		if chosen[d.Index] {
			send(p.list, 0x185, 1, uintptr(i))
		}
	}
}
func (p *workbench) chosen() ([]ldplayer.Instance, error) {
	ids, e := p.indices()
	if e != nil {
		return nil, e
	}
	var out []ldplayer.Instance
	for _, id := range ids {
		found := false
		for _, d := range p.app.devices {
			if d.Index == id && !d.Unmapped {
				out = append(out, d)
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("编号%d不在当前已核验清单", id)
		}
	}
	return out, nil
}
func (p *workbench) refresh() {
	if p.app.workbench != p {
		return
	}
	p.instances()
	lines := []string{"任务状态：仅保存计划与核验进度，不把点击或模拟测试写为游戏完成。"}
	plan, e := p.app.stateStore.LatestPlan()
	if e == nil {
		phase, pe := p.app.stateStore.Next(plan.ID, time.Now())
		lines = append(lines, "计划 "+plan.ID)
		if pe == nil {
			lines = append(lines, "下一阶段："+studio.PhaseLabel(phase))
		}
		if pe == studio.ErrPlanComplete {
			if len(plan.Modules) == 0 {
				lines = append(lines, "该旧计划仅含已停用任务，已归档；没有执行或核验游戏任务。")
			} else {
				lines = append(lines, "本计划启用任务均已核验结束。")
			}
		}
		if pe != nil && pe != studio.ErrPlanComplete {
			lines = append(lines, "保护状态："+pe.Error())
		}
		for _, t := range phase.Tasks {
			for _, actor := range phase.Actors {
				if t.Actor == studio.ActorKey(actor.Identity, phase.Server, phase.Slot) {
					lines = append(lines, fmt.Sprintf("模拟器%d / %s / 角色%d：%s", actor.Instance, phase.Server, phase.Slot, studio.StateLabel(t.State)))
				}
			}
		}
	} else {
		lines = append(lines, e.Error())
	}
	orders, e := p.app.stateStore.Orders()
	if e != nil {
		lines = append(lines, e.Error())
	}
	lines = append(lines, "", "订单仅记账；实际赠送、买家核验和消息发送尚未接入。")
	for _, o := range orders {
		status := "待分配"
		if !o.DeliveryEnabled {
			status = "仅记账"
		}
		if o.Held > 0 {
			status = "额度锁定/待核验"
		}
		if o.Completed > 0 {
			status = "已完成"
		}
		lines = append(lines, fmt.Sprintf("订单%s 买家%s 总额%d 确认已交%d 锁定%d 剩余%d 状态%s", o.ID, o.Buyer, o.Total, o.Confirmed, o.Held, o.Remaining, status))
	}
	setText(p.report, strings.Join(lines, "\r\n"))
	setText(p.status, fmt.Sprintf("本地订单%d笔。顺序：同服角色1→5；该批全部指定区服完成后才换下一批。\r\n失败/不明结果不算完成，跨日边界未确认时安全暂停。", len(orders)))
}
func (p *workbench) startSelected(games bool) {
	chosen, e := p.chosen()
	if e != nil {
		setText(p.status, e.Error())
		return
	}
	client, cfg := p.app.client, p.app.cfg
	p.app.work(func(ctx context.Context) (err error) {
		defer func() {
			if err != nil && ctx.Err() != nil {
				err = ctx.Err()
			}
			if err != nil {
				p.app.ui(func() {
					if p.app.workbench == p {
						setText(p.status, err.Error())
					}
				})
			}
		}()
		if !games {
			ids := make([]int, len(chosen))
			for i, d := range chosen {
				ids[i] = d.Index
			}
			all, e := client.Start(ctx, ids, ldplayer.LaunchOptions{OnLog: p.app.logAsync})
			if e != nil {
				return e
			}
			all, e = client.Bind(ctx, all)
			p.app.ui(func() {
				p.app.showInstances(client, all, true)
				if p.app.workbench == p {
					p.instances()
				}
			})
			if e != nil {
				return e
			}
			for _, id := range ids {
				ready := false
				for _, d := range all {
					if d.Index == id && d.Serial != "" && !d.Unmapped {
						ready = true
					}
				}
				if !ready {
					return fmt.Errorf("编号%d尚未通过ADB核验", id)
				}
			}
			p.app.ui(func() {
				if p.app.workbench == p {
					setText(p.status, "所选五台已启动，五台ADB身份已验证；没有启动游戏任务。")
				}
			})
			return nil
		}
		for _, d := range chosen {
			b := cfg.Games[strconv.Itoa(d.Index)]
			if e := gameapp.VerifyBinding(ctx, gameShell{client, d}, b, d.AndroidID); e != nil {
				return fmt.Errorf("编号%d：%w；五台游戏启动未开始", d.Index, e)
			}
		}
		var wg sync.WaitGroup
		errs := make(chan error, 5)
		for _, d := range chosen {
			wg.Add(1)
			go func(d ldplayer.Instance) {
				defer wg.Done()
				_, e := gameapp.Open(ctx, gameShell{client, d}, cfg.Games[strconv.Itoa(d.Index)], d.AndroidID)
				if e != nil {
					errs <- fmt.Errorf("编号%d：%w", d.Index, e)
				}
			}(d)
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			return e
		}
		p.app.ui(func() {
			if p.app.workbench == p {
				setText(p.status, "五台已绑定游戏的前台均已核验；未报告角色登录或任务成功。")
			}
		})
		return nil
	})
}
func (p *workbench) createPlan(appendBatch bool) {
	chosen, e := p.chosen()
	if e != nil {
		setText(p.status, e.Error())
		return
	}
	var actors []studio.Actor
	for _, d := range chosen {
		if d.Serial == "" || d.AndroidID == "" {
			setText(p.status, fmt.Sprintf("编号%d请先启动并通过ADB核验", d.Index))
			return
		}
		jobs := p.app.cfg.Jobs[strconv.Itoa(d.Index)]
		if jobs[0] == "" {
			setText(p.status, fmt.Sprintf("编号%d未绑定职业：请在主窗口选择该实例与职业，再点绑定五个同职业角色。", d.Index))
			return
		}
		for _, j := range jobs {
			if j != jobs[0] {
				setText(p.status, "同实例五个角色职业绑定不一致")
				return
			}
		}
		identity := sha256.Sum256([]byte(strings.ToLower(p.app.client.Console) + "|" + strconv.Itoa(d.Index) + "|" + d.AndroidID))
		actors = append(actors, studio.Actor{Instance: d.Index, Identity: hex.EncodeToString(identity[:]), Profession: jobs[0]})
	}
	raw := strings.NewReplacer("，", ",", "\n", ",").Replace(text(p.servers))
	var servers []string
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			servers = append(servers, s)
		}
	}
	plan := studio.Plan{ID: studio.ID(), Day: studio.Day(time.Now()), Batches: []studio.Batch{{Actors: actors, Servers: servers}}, Modules: studio.EnabledModules()}
	if appendBatch {
		old, er := p.app.stateStore.LatestPlan()
		e = er
		if e == nil {
			e = p.app.stateStore.AppendBatch(old.ID, plan.Batches[0])
		}
	} else {
		e = p.app.stateStore.CreatePlan(plan)
	}
	if e != nil {
		setText(p.status, e.Error())
		return
	}
	p.refresh()
}
func (p *workbench) proc(h uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case wmCommand:
		if w>>16 != 0 {
			return 0
		}
		id := int(w & 0xffff)
		if id == 506 {
			p.app.handle(107)
			return 0
		}
		if p.app.busy.Load() {
			if p.app.backgroundScan {
				p.app.pendingStudioAction = id
				if p.app.cancel != nil {
					p.app.cancel()
				}
				return 0
			}
			setText(p.status, "操作进行中，可点停止；完成后再操作工作台。")
			return 0
		}
		switch id {
		case 501:
			p.startSelected(false)
		case 502:
			p.startSelected(true)
		case 503:
			p.createPlan(false)
		case 521:
			p.createPlan(true)
		case 522:
			p.openGroupLogin()
		case 504:
			p.refresh()
		case 505:
			setText(p.status, "海嘟嘟战术规划代码已接入；实战未开放。缺目标A可靠绑定、宝宝技能与第三回合备用，以及失败后重入验证。30秒回合保护按角色+宝宝双指令设计。")
		case 507:
			n, e := studio.ParseAmount(text(p.amount))
			if e == nil {
				_, e = p.app.stateStore.CreateOrder(strings.TrimSpace(text(p.buyer)), n)
			}
			if e != nil {
				setText(p.status, e.Error())
			} else {
				p.refresh()
			}
		case 508:
			destroyWindow.Call(h)
		case 509:
			b, e := studio.ParseAmount(text(p.balance))
			if e != nil {
				setText(p.status, e.Error())
				return 0
			}
			m, e := studio.ParseAmount(text(p.merit))
			if e != nil {
				setText(p.status, e.Error())
				return 0
			}
			n, e := studio.Capacity(b, m)
			if e != nil {
				setText(p.status, e.Error())
			} else {
				setText(p.status, fmt.Sprintf("当前角色估算可交付%d银锭；有效功绩%d。每角色单独取整，不混合零头。仅手动核对，不是已读取游戏余额。", n, m-m%100))
			}
		}
		return 0
	case wmClose:
		destroyWindow.Call(h)
		return 0
	case wmDestroy:
		p.app.workbench = nil
		return 0
	}
	v, _, _ := defWindow.Call(h, uintptr(m), w, l)
	return v
}
func effectiveProfile(p automation.Profile) (automation.Profile, error) {
	c, e := evidence.Load()
	if e != nil {
		return p, e
	}
	return c.Profile(p), nil
}
func (a *app) maintainStudio() bool {
	if a.busy.Load() || a.cal != nil || a.gameSelector != nil || a.closing {
		return false
	}
	if a.stateStore == nil && !a.stateOpenFailed {
		if _, e := os.Stat(filepath.Join(a.root, studio.FileName)); e == nil {
			s, e := studio.Open(a.root)
			if e == nil {
				e = s.RecoverInterrupted()
			}
			if e != nil {
				if s != nil {
					_ = s.Close()
				}
				a.stateOpenFailed = true
				a.log("进度库检查失败，基础检测和更新仍可用：" + e.Error())
				return false
			}
			a.stateStore = s
		}
	}
	day := studio.Day(time.Now())
	if a.stateStore == nil || a.lastProofCleanupDay == day {
		return false
	}
	a.lastProofCleanupDay = day
	s := a.stateStore
	a.work(func(ctx context.Context) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		n, e := s.CleanupProofs(time.Now())
		if e == nil && n > 0 {
			a.logAsync(fmt.Sprintf("已清理%d个完成超过10天订单的截图；订单和流水保留。", n))
		}
		return e
	})
	return true
}
func (a *app) evidenceCheck() {
	d, e := a.selected()
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
		im, e := automation.Decode(b)
		if e != nil {
			return e
		}
		c, e := evidence.Load()
		if e != nil {
			return e
		}
		matches := c.Matches(im)
		if len(matches) == 0 {
			return fmt.Errorf("交接截图候选模板未命中，未点击；可使用已有本机标定")
		}
		for _, m := range matches {
			a.logAsync(fmt.Sprintf("候选页面：%s（截图%d；只读检查，不代表实机验收）", m.Label, m.SourceID))
		}
		if j, e := c.FiveProfessions(im); e == nil {
			a.logAsync("形状与颜色联合识别：五个角色均为" + j + "。职业绑定未自动修改。")
		} else if c.Match(im, "roles") {
			a.logAsync("职业候选未确认：" + e.Error() + "；保留已有人工职业绑定。")
		}
		return nil
	})
}
