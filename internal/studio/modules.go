package studio

import "errors"

// Decisions consume verified observations, never raw click acknowledgements.
// UI readers/executors are intentionally separate and unavailable where the
// handoff does not supply an unambiguous success/identity/resource observation.
type Decision struct {
	State   TaskState
	Actions []string
	Reason  string
}

func uncertain(why string) Decision { return Decision{State: Review, Reason: why} }

type TeamObservation struct {
	Verified                 bool
	Leader, ExpectedLeader   string
	Members, ExpectedMembers []string
	InviteLeader             string
}

func TeamDecision(v TeamObservation) Decision {
	if !v.Verified || len(v.ExpectedMembers) != 5 || v.ExpectedLeader == "" {
		return uncertain("队长及五个角色身份缺少核验")
	}
	wanted := map[string]bool{}
	for _, id := range v.ExpectedMembers {
		if id == "" || wanted[id] {
			return uncertain("预期队伍身份重复/缺失")
		}
		wanted[id] = true
	}
	if !wanted[v.ExpectedLeader] {
		return uncertain("指定队长不在本小组")
	}
	if v.InviteLeader != "" {
		if v.InviteLeader != v.ExpectedLeader {
			return uncertain("邀请来自其他队长，不接受")
		}
		return Decision{State: Pending, Actions: []string{"accept_verified_invitation"}}
	}
	if v.Leader != "" && v.Leader != v.ExpectedLeader {
		return uncertain("实际队长不符")
	}
	seen := map[string]bool{}
	for _, m := range v.Members {
		if !wanted[m] || seen[m] {
			return uncertain("队伍存在陌生或重复成员")
		}
		seen[m] = true
	}
	if len(seen) == 5 && v.Leader == v.ExpectedLeader {
		return Decision{State: Done, Reason: "verified_five_identities"}
	}
	return Decision{State: Pending, Actions: []string{"invite_missing_members"}, Reason: "not_ready_for_battle"}
}

type TalismanObservation struct {
	Verified               bool
	SelectedGrade          int
	ActiveGrade            [4]int
	Remaining              [4]int64
	Materials              [4]int64
	MaterialCountsVerified bool
	SuccessResultVerified  bool
}

func TalismanDecision(v TalismanObservation) Decision {
	if !v.Verified || v.SelectedGrade != 8 {
		return uncertain("未核验8星")
	}
	allActive := true
	for i := 0; i < 4; i++ {
		if v.ActiveGrade[i] != 8 || v.Remaining[i] <= 0 {
			allActive = false
		}
	}
	if allActive {
		return Decision{State: Skip, Reason: "already_done"}
	}
	if !v.MaterialCountsVerified {
		return uncertain("材料数量未读清，绿色勾选不等于材料充足")
	}
	for _, n := range v.Materials {
		if n <= 0 {
			return Decision{State: Skip, Reason: "resource_insufficient"}
		}
	}
	if v.SuccessResultVerified {
		return uncertain("成功提示缺少对应四项8星状态，不记完成")
	}
	return Decision{State: Pending, Actions: []string{"apply_eight_star_once", "verify_four_effects_and_material_change"}}
}

type FortuneObservation struct {
	Verified                                                          bool
	Active                                                            map[string]bool
	Cost                                                              map[string]int64
	Fortune, BindJade, ExchangeRemaining, FortunePerUnit, JadePerUnit int64
	ExchangeVerified                                                  bool
}

func FortuneDecision(v FortuneObservation) Decision {
	if !v.Verified {
		return uncertain("福气/激活状态未确认")
	}
	var actions []string
	var cost int64
	for _, id := range []string{"stronghold", "demon_king"} {
		active, ok := v.Active[id]
		if !ok {
			return uncertain("目标激活状态缺失")
		}
		if active {
			continue
		}
		n, ok := v.Cost[id]
		if !ok || n <= 0 || n > MaxMoney-cost {
			return uncertain("实际激活消耗缺失")
		}
		cost += n
		actions = append(actions, "activate_"+id)
	}
	if len(actions) == 0 {
		return Decision{State: Skip, Reason: "already_done"}
	}
	if v.Fortune < 0 || v.Fortune > MaxMoney {
		return uncertain("福气余额无效")
	}
	if v.Fortune >= cost {
		return Decision{State: Pending, Actions: actions}
	}
	if !v.ExchangeVerified || v.FortunePerUnit <= 0 || v.JadePerUnit <= 0 || v.BindJade < 0 || v.ExchangeRemaining < 0 {
		return uncertain("绑玉、兑换比例或本周剩余额度未核验")
	}
	need := cost - v.Fortune
	units := need / v.FortunePerUnit
	if need%v.FortunePerUnit > 0 {
		units++
	}
	if units > v.ExchangeRemaining || units > v.BindJade/v.JadePerUnit {
		return Decision{State: Skip, Reason: "resource_insufficient"}
	}
	return Decision{State: Pending, Actions: append([]string{"exchange_exact_observed_deficit"}, actions...)}
}
func TowerDecision(redKnown, red bool) Decision {
	if !redKnown {
		return uncertain("锁灵塔红点状态不确定")
	}
	if !red {
		return Decision{State: Skip, Reason: "no_red_dot"}
	}
	return Decision{State: Pending, Actions: []string{"open_tower", "locate_chest_across_frames", "verify_reward", "return_to_verified_main"}}
}

type Opponent struct {
	ID       string
	Bonus    int
	Verified bool
}

func ArenaDecision(counter int, counterKnown bool, opponents []Opponent) Decision {
	if !counterKnown || counter < 0 || counter > 9 {
		return uncertain("今日挑战次数未核验")
	}
	if counter == 9 {
		return Decision{State: Skip, Reason: "no_attempts"}
	}
	for _, o := range opponents {
		if o.Verified && o.Bonus == 10 && o.ID != "" {
			return Decision{State: Pending, Actions: []string{"challenge_verified_" + o.ID, "enable_auto", "verify_result_and_counter_increment"}}
		}
	}
	return uncertain("没有已核验+10对手，不按位置盲点")
}
func ArenaResult(before, after int, win bool, resultKnown bool) (bool, error) {
	if !resultKnown || before < 0 || before >= 9 || after != before+1 {
		return false, errors.New("结算/次数变化未核验")
	}
	return win, nil
}
func PassDecision(verified, done, claimRed bool, vitality, progress, goal, costPerWork int64) Decision {
	if !verified || vitality < 0 || progress < 0 || goal <= 0 || progress > goal {
		return uncertain("战令进度/活力未核验")
	}
	if done {
		if claimRed {
			return Decision{State: Pending, Actions: []string{"claim_verified_task_reward", "claim_red_pass_rewards", "return_to_main"}}
		}
		return Decision{State: Skip, Reason: "already_done"}
	}
	if vitality == 0 {
		return Decision{State: Skip, Reason: "resource_insufficient"}
	}
	if costPerWork <= 0 {
		return uncertain("打工实际耗活力未核验")
	}
	if vitality < costPerWork {
		return Decision{State: Skip, Reason: "resource_insufficient"}
	}
	return Decision{State: Pending, Actions: []string{"work_one_observed_unit", "verify_animation_finished", "reread_pass_task"}}
}
