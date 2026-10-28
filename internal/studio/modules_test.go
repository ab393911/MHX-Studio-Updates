package studio

import (
	"testing"
	"time"
)

func TestTeamBlocksWrongLeaderStrangersAndIncompleteFive(t *testing.T) {
	v := TeamObservation{Verified: true, Leader: "a", ExpectedLeader: "a", ExpectedMembers: []string{"a", "b", "c", "d", "e"}, Members: []string{"a", "b", "c", "d"}}
	if TeamDecision(v).State == Done {
		t.Fatal("incomplete team ready")
	}
	v.InviteLeader = "stranger"
	if TeamDecision(v).State != Review {
		t.Fatal("wrong invitation accepted")
	}
	v.InviteLeader = ""
	v.Members = append(v.Members, "e")
	if TeamDecision(v).State != Done {
		t.Fatal("valid five rejected")
	}
}
func TestTalismanAndFortuneUseObservedResourcesNotGreenCheckboxesOrFixedCosts(t *testing.T) {
	v := TalismanObservation{Verified: true, SelectedGrade: 8, Materials: [4]int64{1, 1, 1, 1}}
	if TalismanDecision(v).State != Review {
		t.Fatal("unread material counted as enough")
	}
	v.MaterialCountsVerified = true
	v.Materials[2] = 0
	if TalismanDecision(v).State != Skip {
		t.Fatal("zero material ignored")
	}
	f := FortuneObservation{Verified: true, Active: map[string]bool{"stronghold": false, "demon_king": true}, Cost: map[string]int64{"stronghold": 125}, Fortune: 30, BindJade: 100, ExchangeRemaining: 5, FortunePerUnit: 25, JadePerUnit: 20, ExchangeVerified: true}
	if FortuneDecision(f).State != Pending {
		t.Fatal("valid exact observed exchange rejected")
	}
	f.ExchangeRemaining = 3
	if FortuneDecision(f).State != Skip {
		t.Fatal("weekly capacity ignored")
	}
}
func TestHaiduduRequiresBothCharacterAndPetAndHonorsDeadlineAndMissingFallbacks(t *testing.T) {
	now := time.Now()
	actors := []CombatActor{{"a", "合欢", "pet", false, false}, {"b", "天音", "pet", true, true}, {"c", "青云", "pet", true, true}, {"d", "鬼王", "pet", true, true}, {"e", "鬼王", "pet", true, true}}
	f := BattleFacts{Boss: "海嘟嘟", Round: 1, Actors: actors, AAlive: true, AVerified: true, BCVerified: true, TeamVerified: true, SkillsVerified: true, TurnStart: now, Now: now}
	cmd, e := Haidudu(f)
	if e != nil || len(cmd) != 10 {
		t.Fatal(cmd, e)
	}
	for _, c := range cmd {
		if c.Unit == "pet" && c.Target != "B" {
			t.Fatal(c)
		}
	}
	f.Round = 2
	cmd, e = Haidudu(f)
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range cmd {
		if c.Unit == "pet" && c.Target != "C" {
			t.Fatal(c)
		}
		if c.Actor == "b" && c.Unit == "character" && c.Target != "self" {
			t.Fatal(c)
		}
	}
	f.Round = 3
	f.Actors[2].UltimateKnown = false
	if _, e = Haidudu(f); e == nil {
		t.Fatal("unavailable ultimate guessed")
	}
	f.Round = 1
	f.Now = now.Add(28 * time.Second)
	if _, e = Haidudu(f); e == nil {
		t.Fatal("deadline ignored")
	}
	f.Now = now
	f.Boss = "unknown"
	if _, e = Haidudu(f); e == nil {
		t.Fatal("strategy applied to another boss")
	}
}
func TestArenaLossIsNeverRecordedAsVictoryAndPassZeroVitalitySkips(t *testing.T) {
	win, e := ArenaResult(8, 9, false, true)
	if e != nil || win {
		t.Fatal(win, e)
	}
	if ArenaDecision(9, true, nil).State != Skip {
		t.Fatal("10th challenge scheduled")
	}
	if PassDecision(true, false, false, 0, 0, 200, 100).State != Skip {
		t.Fatal("zero vitality worked")
	}
	if TowerDecision(false, false).State != Review {
		t.Fatal("unknown red dot skipped")
	}
}
