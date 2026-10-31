//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/login"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
	"github.com/ab393911/MHX-Studio-Updates/internal/taskflow"
)

func (a *app) executeEight(ctx context.Context, actors []taskflow.Actor, spec login.Spec, leader int, planID string, ledger bool) (err error) {
	var phase studio.Phase
	var e error
	if ledger {
		phase, e = a.stateStore.Next(planID, time.Now())
		if errors.Is(e, studio.ErrPlanComplete) {
			return nil
		}
		if e != nil {
			return e
		}
		if phase.Module != "eight_star" || phase.Server != spec.Server || phase.Slot != spec.Role {
			return errors.New("计划当前阶段不是八星准备，不执行额外消耗")
		}
		if e = a.stateStore.StartPhase(planID, phase.Number, a.cfg.Retries); e != nil {
			return e
		}
		defer func() {
			if err != nil {
				for _, t := range phase.Tasks {
					if t.State != studio.Done && t.State != studio.Skip {
						_ = a.stateStore.SetTask(planID, phase.Number, t.Actor, studio.Review, "", err.Error(), a.cfg.Retries)
					}
				}
			}
		}()
	}
	a.ui(func() {
		setText(a.controls[211], "八星准备：读取材料与选项，验证打符；按职业核验8★幻形，缺卡最多购买一次。可随时停止。")
	})
	skip := map[int]bool{}
	if ledger {
		for _, actor := range phase.Actors {
			key := studio.ActorKey(actor.Identity, phase.Server, phase.Slot)
			for _, t := range phase.Tasks {
				if t.Actor == key && (t.State == studio.Done || t.State == studio.Skip) {
					skip[actor.Instance] = true
				}
			}
		}
	}
	report, runErr := taskflow.RunEight(ctx, actors, leader, skip)
	if e = filesafe.JSON(filepath.Join(a.root, "profiles", "eight-last.json"), report); e != nil {
		return errors.Join(runErr, e)
	}
	if ledger {
		seen := map[int]bool{}
		for _, r := range report.Actors {
			if seen[r.Instance] {
				return errors.New("八星结果实例重复，不写完成")
			}
			seen[r.Instance] = true
			if r.PreviouslyCompleted {
				continue
			}
			known := func(p taskflow.Part) bool { return p.State == studio.Done || p.State == studio.Skip }
			if !r.ReturnedMain || !known(r.Talisman) || !known(r.Phantom) {
				continue
			}
			var actor studio.Actor
			found := false
			for _, v := range phase.Actors {
				if v.Instance == r.Instance {
					actor = v
					found = true
					break
				}
			}
			if !found {
				return errors.New("八星结果不属于当前计划实例")
			}
			state, reason := studio.Done, "verified_talisman_and_target_phantom"
			if r.Talisman.Reason == "resource_insufficient" || r.Phantom.Reason == "resource_insufficient" {
				state, reason = studio.Skip, "resource_insufficient"
			}
			if r.Talisman.State == studio.Skip && r.Phantom.State == studio.Skip && r.Talisman.Reason == "already_done" && r.Phantom.Reason == "already_done" {
				state, reason = studio.Skip, "already_done"
			}
			key := studio.ActorKey(actor.Identity, phase.Server, phase.Slot)
			terminalAlready := false
			for _, t := range phase.Tasks {
				if t.Actor == key && (t.State == studio.Done || t.State == studio.Skip) {
					terminalAlready = true
				}
			}
			if !terminalAlready {
				if e = a.stateStore.SetTask(planID, phase.Number, key, state, r.Phantom.ScreenSHA, reason, a.cfg.Retries); e != nil {
					return errors.Join(runErr, e)
				}
			}
		}
	}
	if runErr != nil {
		return runErr
	}
	if !report.Verified {
		return errors.New("五台八星准备未全部核验结束")
	}
	a.ui(func() {
		setText(a.controls[211], fmt.Sprintf("%s角色%d：组队及八星准备已核验结束，资源不足按检查结果跳过。聚福、据点、交易等尚未执行，未自动切号。", spec.Server, spec.Role))
		if a.workbench != nil {
			a.workbench.refresh()
		}
	})
	return nil
}
