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

	"github.com/ab393911/MHX-Studio-Updates/internal/evidence"
	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/ldplayer"
	"github.com/ab393911/MHX-Studio-Updates/internal/login"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
	"github.com/ab393911/MHX-Studio-Updates/internal/taskflow"
)

func (a *app) runTeam(ctx context.Context, ready login.Report, devices []ldplayer.Instance, leader int) (err error) {
	defer func() {
		if err != nil {
			a.ui(func() { setText(a.controls[211], "后续组队已停止："+err.Error()+"；未标记完成。") })
		}
	}()
	if !ready.Ready || len(ready.Members) != 5 || len(devices) != 5 || a.ocr == nil {
		return errors.New("缺少本次五台登录与文字识别依据，不开始组队")
	}
	proofs := map[int]login.Proof{}
	for _, m := range ready.Members {
		if m.State != "VERIFIED" || !m.Proof.IdentityVerified || !m.Proof.ServerVerified || !m.Proof.CharacterNameVerified || m.Proof.CharacterName == "" {
			return fmt.Errorf("实例%d虽可完成昵称标定核验，但尚未得到本次中文昵称读数，未发送组队邀请", m.Instance)
		}
		if m.Proof.Server != ready.Spec.Server || m.Proof.Role != ready.Spec.Role || time.Since(m.Proof.ObservedAt) > 15*time.Second {
			return errors.New("登录就位依据过期或目标不符，未开始组队")
		}
		proofs[m.Instance] = m.Proof
	}
	catalog, e := evidence.Load()
	if e != nil {
		return e
	}
	var actors []taskflow.Actor
	for _, d := range devices {
		p, ok := proofs[d.Index]
		if !ok {
			return errors.New("当前实例和就位记录不对应")
		}
		key := strconv.Itoa(d.Index)
		binding := a.cfg.Games[key]
		h := sha256.Sum256([]byte(a.client.Console + "|" + key + "|" + d.AndroidID + "|" + d.BootID + "|" + binding.Package))
		if p.Identity != hex.EncodeToString(h[:]) {
			return errors.New("就位后设备/游戏身份变化，未发送组队邀请")
		}
		raw, e := loadProfile(a.root, d.Index)
		if e != nil {
			return e
		}
		shell := gameShell{a.client, d}
		device := &gameDevice{boundDevice: boundDevice{a.client, d, a.cache}, shell: shell, pkg: binding.Package}
		actors = append(actors, taskflow.Actor{Instance: d.Index, Identity: p.Identity, Name: p.CharacterName, Profession: a.cfg.Jobs[key][0], Server: ready.Spec.Server, Role: ready.Spec.Role, Device: device, Profile: catalog.Profile(raw), Reader: a.ocr, MinDelayMS: a.cfg.MinDelayMS, MaxDelayMS: a.cfg.MaxDelayMS, OnError: func(b []byte, e error) { a.errorCapture(d, b, e) }})
	}
	var plan studio.Plan
	var phase studio.Phase
	ledger := false
	if a.stateStore != nil {
		plan, e = a.stateStore.LatestPlan()
		if e == nil {
			phase, e = a.stateStore.Next(plan.ID, time.Now())
			if e != nil {
				return e
			}
			if phase.Module != "team" || phase.Server != ready.Spec.Server || phase.Slot != ready.Spec.Role {
				return errors.New("当前计划已进入其它阶段，不重做组队；对应后续执行器仍待接通")
			}
			wanted := map[string]bool{}
			for _, t := range phase.Tasks {
				wanted[t.Actor] = true
			}
			for _, d := range devices {
				h := sha256.Sum256([]byte(strings.ToLower(a.client.Console) + "|" + strconv.Itoa(d.Index) + "|" + d.AndroidID))
				if !wanted[studio.ActorKey(hex.EncodeToString(h[:]), phase.Server, phase.Slot)] {
					return errors.New("组队实例不属于计划当前批次")
				}
			}
			ledger = true
		} else if e.Error() != "尚无五开计划" {
			return e
		}
	}
	if ledger {
		if e = a.stateStore.StartPhase(plan.ID, phase.Number, a.cfg.Retries); e != nil {
			return e
		}
		defer func() {
			if err != nil {
				for _, t := range phase.Tasks {
					if t.State != studio.Done && t.State != studio.Skip {
						_ = a.stateStore.SetTask(plan.ID, phase.Number, t.Actor, studio.Review, "", err.Error(), a.cfg.Retries)
					}
				}
			}
		}()
	}
	a.ui(func() {
		setText(a.controls[211], fmt.Sprintf("自动组队：队长实例%d，核对固定成员、发邀请、核对五人实际队伍；不执行战斗。", leader))
	})
	report, e := taskflow.RunTeam(ctx, actors, leader)
	if saved := filesafe.JSON(filepath.Join(a.root, "profiles", "team-last.json"), report); saved != nil {
		e = errors.Join(e, saved)
	}
	if e != nil {
		return e
	}
	if !report.Verified {
		return errors.New("未核验五人实际队伍")
	}
	if ledger {
		byInstance := map[int]string{}
		for _, p := range report.Members {
			byInstance[p.Instance] = p.ScreenSHA
		}
		for _, actor := range phase.Actors {
			key := studio.ActorKey(actor.Identity, phase.Server, phase.Slot)
			for _, t := range phase.Tasks {
				if t.Actor == key && t.State != studio.Done && t.State != studio.Skip {
					if e = a.stateStore.SetTask(plan.ID, phase.Number, key, studio.Done, byInstance[actor.Instance], "verified_five_members_and_returned_main", a.cfg.Retries); e != nil {
						return e
					}
				}
			}
		}
	}
	a.ui(func() {
		setText(a.controls[211], "五个角色的实际队伍、队长和返回主界面均已核验；未执行战斗、交易或自动切号。")
	})
	return nil
}
