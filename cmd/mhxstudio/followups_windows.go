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

func (a *app) executeModule(ctx context.Context, actors []taskflow.Actor, leader int, module, planID string, ledger bool) (err error) {
	var phase studio.Phase
	skip := map[int]bool{}
	if ledger {
		var e error
		phase, e = a.stateStore.Next(planID, time.Now())
		if e != nil {
			return e
		}
		if phase.Module != module || phase.Server != actors[0].Server || phase.Slot != actors[0].Role {
			return errors.New("后续任务与当前计划角色/阶段不符")
		}
		for _, p := range phase.Actors {
			key := studio.ActorKey(p.Identity, phase.Server, phase.Slot)
			for _, t := range phase.Tasks {
				if t.Actor == key && (t.State == studio.Done || t.State == studio.Skip) {
					skip[p.Instance] = true
				}
			}
		}
		if e = a.stateStore.StartPhase(planID, phase.Number, a.cfg.Retries); e != nil {
			return e
		}
		defer func() {
			if err != nil {
				for _, p := range phase.Actors {
					if !skip[p.Instance] {
						_ = a.stateStore.SetTask(planID, phase.Number, studio.ActorKey(p.Identity, phase.Server, phase.Slot), studio.Review, "", err.Error(), a.cfg.Retries)
					}
				}
			}
		}()
	}
	a.ui(func() {
		setText(a.controls[211], "正在执行："+studio.ModuleLabel(module)+"；逐项核验游戏结果，可随时停止。")
	})
	report, runErr := taskflow.RunModule(ctx, actors, leader, module, skip)
	if e := filesafe.JSON(filepath.Join(a.root, "profiles", module+"-last.json"), report); e != nil {
		return errors.Join(runErr, e)
	}
	for _, r := range report.Actors {
		if module == "assets" && r.Error != "" && a.stateStore != nil {
			if e := a.stateStore.RecordCharacterAssetFailure(r.Instance, actors[0].Role, actors[0].Server, time.Now(), r.Error); e != nil {
				return errors.Join(runErr, e)
			}
		}

		if r.Assets != nil && r.Error == "" {
			snapshot, e := r.Assets.Snapshot()
			if e != nil {
				return errors.Join(runErr, e)
			}
			if a.stateStore == nil {
				return errors.New("资产数据库尚未打开，未保存读数")
			}
			if e = a.stateStore.RecordCharacterAsset(snapshot); e != nil {
				return errors.Join(runErr, e)
			}
		}
		if ledger && r.Error == "" && (r.Part.State == studio.Done || r.Part.State == studio.Skip) {
			found := false
			for _, p := range phase.Actors {
				if p.Instance == r.Instance {
					found = true
					if e := a.stateStore.SetTask(planID, phase.Number, studio.ActorKey(p.Identity, phase.Server, phase.Slot), r.Part.State, r.Part.ScreenSHA, r.Part.Reason, a.cfg.Retries); e != nil {
						return errors.Join(runErr, e)
					}
				}
			}
			if !found {
				return errors.New("后续任务返回了计划以外的实例")
			}
		}
	}
	a.ui(func() {
		if a.workbench != nil {
			a.workbench.refresh()
		}
	})
	if runErr != nil {
		return runErr
	}
	if !report.Verified {
		return errors.New("后续任务结果尚未全部核验")
	}
	return nil
}
func (a *app) executeFollowups(ctx context.Context, actors []taskflow.Actor, spec login.Spec, leader int, planID string, ledger bool) error {
	// Observational snapshots do not complete an out-of-order scheduler task.
	// Read them immediately so balances remain testable even if combat is blocked.
	if e := a.executeModule(ctx, actors, leader, "assets", "", false); e != nil {
		return e
	}
	if !ledger {
		return nil
	}
	for {
		phase, e := a.stateStore.Next(planID, time.Now())
		if errors.Is(e, studio.ErrPlanComplete) {
			return nil
		}
		if e != nil {
			return e
		}
		if phase.Server != spec.Server || phase.Slot != spec.Role {
			a.ui(func() { setText(a.controls[211], "本组已核验结束；下一组登录尚未启动。") })
			return nil
		}
		switch phase.Module {
		case "assets", "fortune", "battle_pass":
			if e = a.executeModule(ctx, actors, leader, phase.Module, planID, true); e != nil {
				return e
			}
		case "stronghold_prepare", "haidudu":
			return errors.New("据点战斗仍缺目标A、宝宝技能及第三回合备用的完整核验，不启动战斗或跳过该阶段；可单独测试资产、聚福和战令")
		default:
			return fmt.Errorf("%s完整执行器尚未接通，阶段保持待执行", studio.ModuleLabel(phase.Module))
		}
	}
}
