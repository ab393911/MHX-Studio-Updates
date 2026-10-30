package login

import "testing"

func startupFixture() StartupPlan {
	p := StartupPlan{Enabled: true, Console: "/verified/ldconsole.exe", Target: Spec{Server: "一梦诛仙", Role: 3}, RunTasks: true, LeaderIndex: 9}
	for _, n := range []int{2, 6, 9, 17, 24} {
		p.Members = append(p.Members, StartupMember{Index: n, AndroidID: "verified-device", Package: "com.example.game"})
	}
	return p
}
func TestStartupRequiresExplicitBoundFiveAndLeader(t *testing.T) {
	p := startupFixture()
	if e := p.Validate(); e != nil {
		t.Fatal(e)
	}
	if !p.MatchesConsole("/verified/ldconsole.exe") || p.MatchesConsole("/other/ldconsole.exe") {
		t.Fatal("console identity bypass")
	}
	ids := p.Indices()
	ids[0] = 99
	if p.Members[0].Index != 2 {
		t.Fatal("indices mutated saved members")
	}
	for _, mutate := range []func(*StartupPlan){func(p *StartupPlan) { p.Members = p.Members[:4] }, func(p *StartupPlan) { p.Members[1].Index = p.Members[0].Index }, func(p *StartupPlan) { p.Members[0].AndroidID = "" }, func(p *StartupPlan) { p.Members[0].Package = "com.example;bad" }, func(p *StartupPlan) { p.LeaderIndex = 999 }, func(p *StartupPlan) { p.Target.Role = 6 }, func(p *StartupPlan) { p.Target.Server = "" }, func(p *StartupPlan) { p.Console = "" }} {
		q := startupFixture()
		mutate(&q)
		if q.Validate() == nil {
			t.Fatal("invalid startup accepted", q)
		}
	}
	if (StartupPlan{Enabled: false}).Validate() != nil {
		t.Fatal("disabled plan blocked normal controller")
	}
}
