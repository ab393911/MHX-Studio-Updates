package studio

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"
)

// This creates exactly the v1 schema/plan representation shipped in preview.21.
// It deliberately bypasses Open, which would migrate it before the test starts.
func legacyFixture(t *testing.T, root string, plans ...Plan) *Store {
	t.Helper()
	if e := os.MkdirAll(root, 0700); e != nil {
		t.Fatal(e)
	}
	d, e := openNative(filepath.Join(root, FileName), false)
	if e != nil {
		t.Fatal(e)
	}
	s := &Store{db: d, Root: root}
	t.Cleanup(func() { _ = s.Close() })
	for _, q := range append([]string{"PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON"}, schema...) {
		if _, _, e = d.query(q); e != nil {
			t.Fatal(e)
		}
	}
	for _, p := range plans {
		raw, e := json.Marshal(p)
		if e != nil {
			t.Fatal(e)
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		fields["unknown_preserved_setting"] = json.RawMessage(`{"keep":"exactly"}`)
		raw, _ = json.Marshal(fields)
		if _, _, e = d.query("INSERT INTO plans(id,day,config,created) VALUES(?,?,?,?)", p.ID, p.Day, string(raw), num(time.Now().Unix())); e != nil {
			t.Fatal(e)
		}
		n := 0
		for _, b := range p.Batches {
			for _, server := range b.Servers {
				for slot := 1; slot <= 5; slot++ {
					for _, m := range p.Modules {
						for _, a := range b.Actors {
							if _, _, e = d.query("INSERT INTO tasks(plan,phase,actor,module,state,attempt,evidence,reason,updated) VALUES(?,?,?,?,?,?,?,?,?)", p.ID, num(int64(n)), ActorKey(a.Identity, server, slot), m, string(Pending), "0", evidence(fmt.Sprint(p.ID, n, a.Identity)), "legacy", num(12345)); e != nil {
								t.Fatal(e)
							}
						}
						n++
					}
				}
			}
		}
	}
	return s
}
func legacyFullPlan(id string) Plan {
	p := plan()
	p.ID = id
	p.Modules = legacyModules()
	return p
}
func rows(t *testing.T, s *Store, q string, args ...string) [][]string {
	t.Helper()
	r, e := s.read(q, args...)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func oldTaskSnapshot(t *testing.T, s *Store) [][]string {
	return rows(t, s, "SELECT plan,phase,actor,module,state,attempt,evidence,reason,updated FROM tasks ORDER BY plan,phase,actor")
}
func moneySnapshot(t *testing.T, s *Store) [][][]string {
	t.Helper()
	var out [][][]string
	for _, table := range []string{"budgets", "orders", "transfers", "outbox", "events"} {
		out = append(out, rows(t, s, "SELECT * FROM "+table+" ORDER BY 1"))
	}
	return out
}
func seedMoney(t *testing.T, s *Store) (Order, Transfer, string) {
	t.Helper()
	o, e := enabledOrder(t, s, "migration-buyer", 70000)
	if e != nil {
		t.Fatal(e)
	}
	for _, actor := range []string{"money-a", "money-b"} {
		if e = s.SetBudget(actor, 327000, 345, evidence(actor), time.Now()); e != nil {
			t.Fatal(e)
		}
	}
	tr, e := s.Reserve(o.ID, "money-a", "confirmed-request", 10000)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkDispatched(tr.ID, o.Buyer, evidence("buyer")); e != nil {
		t.Fatal(e)
	}
	rc := receipt(t, s, o, tr)
	if e = s.Confirm(tr.ID, rc); e != nil {
		t.Fatal(e)
	}
	held, e := s.Reserve(o.ID, "money-b", "uncertain-request", 20000)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkDispatched(held.ID, o.Buyer, evidence("buyer")); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateOrder("record-only", 30000); e != nil {
		t.Fatal(e)
	}
	return o, held, rc.ProofPath
}
func reopen(t *testing.T, s *Store) *Store {
	t.Helper()
	root := s.Root
	if e := s.Close(); e != nil {
		t.Fatal(e)
	}
	v, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { v.Close() })
	return v
}
func TestLegacyMigrationPreservesEveryPhysicalTaskAndMoneyRecord(t *testing.T) {
	p := legacyFullPlan("legacy-all")
	s := legacyFixture(t, t.TempDir(), p)
	// Old removed tasks deliberately span all six states. Keep their evidence and
	// attempts intact even if an old task was interrupted or had failed.
	states := []TaskState{Pending, Running, Done, Skip, Failed, Review}
	for i, state := range states {
		_, _, e := s.db.query("UPDATE tasks SET state=?,attempt=2 WHERE plan=? AND phase=?", string(state), p.ID, num(int64(7+i*11)))
		if e != nil {
			t.Fatal(e)
		}
	}
	_, _, _ = s.db.query("UPDATE tasks SET state='VERIFIED_DONE' WHERE plan=? AND phase<7", p.ID)
	old := oldTaskSnapshot(t, s)
	moneyOrder, held, proof := seedMoney(t, s)
	beforeMoney := moneySnapshot(t, s)
	beforeProof, e := os.ReadFile(proof)
	if e != nil {
		t.Fatal(e)
	}
	original := rows(t, s, "SELECT config FROM plans WHERE id=?", p.ID)[0][0]
	if e = VerifyFile(s.Root); e != nil {
		t.Fatal("legacy read-only health", e)
	}
	s = reopen(t, s)
	if !reflect.DeepEqual(old, oldTaskSnapshot(t, s)) {
		t.Fatal("physical task progress/evidence changed")
	}
	if !reflect.DeepEqual(beforeMoney, moneySnapshot(t, s)) {
		t.Fatal("migration changed financial data")
	}
	b, _ := os.ReadFile(proof)
	if !slices.Equal(b, beforeProof) {
		t.Fatal("proof changed")
	}
	got, e := s.Plan(p.ID)
	if e != nil {
		t.Fatal(e)
	}
	if !slices.Equal(got.Modules, EnabledModules()) || !slices.Equal(got.PhaseLayout, legacyModules()) || got.HistoricalBatches != 2 {
		t.Fatal(got)
	}
	if rows(t, s, "SELECT original_config FROM plan_scope_history WHERE plan=?", p.ID)[0][0] != original {
		t.Fatal("original config not archived verbatim")
	}
	if !json.Valid([]byte(rows(t, s, "SELECT config FROM plans WHERE id=?", p.ID)[0][0])) {
		t.Fatal("invalid config")
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(rows(t, s, "SELECT config FROM plans WHERE id=?", p.ID)[0][0]), &fields)
	if string(fields["unknown_preserved_setting"]) != `{"keep":"exactly"}` {
		t.Fatal("unknown field lost")
	}
	ph, e := s.Next(p.ID, time.Now())
	if e != nil || ph.Number != 10 || ph.Module != "battle_pass" {
		t.Fatal("old phase 10 remapped", ph, e)
	}
	if e = s.SetTask(p.ID, 8, ActorKey(p.Batches[0].Actors[0].Identity, "一梦诛仙", 1), Running, "", "", 1); e == nil {
		t.Fatal("archived daily executed")
	}
	if e = s.RecoverInterrupted(); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(old, oldTaskSnapshot(t, s)) {
		t.Fatal("archived running task was rewritten during recovery")
	}
	order, e := s.Order(moneyOrder.ID)
	if e != nil || order.Confirmed != 10000 || order.Held != 20000 {
		t.Fatal(order, e)
	}
	if _, e = s.Reserve(order.ID, "money-b", "another-request", 10000); e == nil {
		t.Fatal("uncertain transfer resent")
	}
	if rows(t, s, "SELECT state FROM transfers WHERE id=?", held.ID)[0][0] != string(Review) {
		t.Fatal("dispatched transfer not held for reconciliation")
	}
	snapshot := oldTaskSnapshot(t, s)
	history := rows(t, s, "SELECT * FROM plan_scope_history ORDER BY plan")
	s = reopen(t, s)
	if !reflect.DeepEqual(snapshot, oldTaskSnapshot(t, s)) || !reflect.DeepEqual(history, rows(t, s, "SELECT * FROM plan_scope_history ORDER BY plan")) {
		t.Fatal("migration not idempotent")
	}
	if e = VerifyFile(s.Root); e != nil {
		t.Fatal(e)
	}
}
func TestRemovedPendingFailedAndReviewCannotBlockGroupOrReenterPipeline(t *testing.T) {
	p := legacyFullPlan("group")
	s := legacyFixture(t, t.TempDir(), p)
	_, _, _ = s.db.query("UPDATE tasks SET state='VERIFIED_DONE' WHERE plan=? AND phase<7", p.ID)
	_, _, _ = s.db.query("UPDATE tasks SET state='FAILED' WHERE plan=? AND phase=7", p.ID)
	_, _, _ = s.db.query("UPDATE tasks SET state='NEEDS_REVIEW' WHERE plan=? AND phase=8", p.ID)
	s = reopen(t, s)
	if done, e := s.GroupComplete(p.ID, 0, "一梦诛仙", 1); e != nil || done {
		t.Fatal("battle pass was skipped", done, e)
	}
	ph, e := s.Next(p.ID, time.Now())
	if e != nil || ph.Module != "battle_pass" {
		t.Fatal(ph, e)
	}
	for _, a := range ph.Actors {
		actor := ActorKey(a.Identity, ph.Server, ph.Slot)
		if e = s.SetTask(p.ID, ph.Number, actor, Running, "", "", 1); e != nil {
			t.Fatal(e)
		}
		if e = s.SetTask(p.ID, ph.Number, actor, Done, evidence(actor), "verified", 1); e != nil {
			t.Fatal(e)
		}
	}
	if done, e := s.GroupComplete(p.ID, 0, "一梦诛仙", 1); e != nil || !done {
		t.Fatal(done, e)
	}
	next, e := s.Next(p.ID, time.Now())
	if e != nil || next.Number != 11 || next.Slot != 2 || next.Module != "team" {
		t.Fatal("wrong role after layout gaps", next, e)
	}
	for _, ph := range sMustPlan(t, s, p.ID).Phases() {
		if !moduleEnabled(ph.Module) {
			t.Fatal("removed phase leaked", ph)
		}
	}
}
func sMustPlan(t *testing.T, s *Store, id string) Plan {
	t.Helper()
	p, e := s.Plan(id)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestMigrationFaultRollsBackDDLPlansTasksAndFinancialData(t *testing.T) {
	s := legacyFixture(t, t.TempDir(), legacyFullPlan("a"), legacyFullPlan("b"))
	_, _, proof := seedMoney(t, s)
	tasks := oldTaskSnapshot(t, s)
	money := moneySnapshot(t, s)
	proofBefore, _ := os.ReadFile(proof)
	configs := rows(t, s, "SELECT id,config FROM plans ORDER BY id")
	_, _, e := s.db.query("CREATE TRIGGER fail_second_plan BEFORE UPDATE ON plans WHEN OLD.id='b' BEGIN SELECT RAISE(ABORT,'injected migration failure'); END")
	if e != nil {
		t.Fatal(e)
	}
	root := s.Root
	s.Close()
	if _, e = Open(root); e == nil {
		t.Fatal("migration fault ignored")
	}
	d, e := openNative(filepath.Join(root, FileName), false)
	if e != nil {
		t.Fatal(e)
	}
	s = &Store{db: d, Root: root}
	defer s.Close()
	if rows(t, s, "SELECT version FROM schema_info")[0][0] != "1" {
		t.Fatal("schema version committed on failure")
	}
	if _, _, e = d.query("SELECT enabled FROM tasks LIMIT 1"); e == nil {
		t.Fatal("ALTER TABLE was not rolled back")
	}
	if rows(t, s, "SELECT COUNT(*) FROM sqlite_master WHERE name='plan_scope_history'")[0][0] != "0" {
		t.Fatal("history DDL committed on failure")
	}
	if !reflect.DeepEqual(tasks, oldTaskSnapshot(t, s)) || !reflect.DeepEqual(money, moneySnapshot(t, s)) || !reflect.DeepEqual(configs, rows(t, s, "SELECT id,config FROM plans ORDER BY id")) {
		t.Fatal("partial migration survived rollback")
	}
	b, _ := os.ReadFile(proof)
	if !slices.Equal(b, proofBefore) {
		t.Fatal("proof changed on failure")
	}
	_, _, e = d.query("DROP TRIGGER fail_second_plan")
	if e != nil {
		t.Fatal(e)
	}
	s = reopen(t, s)
	if _, e = s.Plan("b"); e != nil {
		t.Fatal("retry after fault", e)
	}
}
func TestMigrationRejectsCorruptOrUnknownLegacyLedger(t *testing.T) {
	for _, mode := range []string{"missing", "wrong_module", "wrong_actor", "unknown_module"} {
		t.Run(mode, func(t *testing.T) {
			p := legacyFullPlan("corrupt")
			s := legacyFixture(t, t.TempDir(), p)
			var q string
			switch mode {
			case "missing":
				q = "DELETE FROM tasks WHERE plan='corrupt' AND phase=0"
			case "wrong_module":
				q = "UPDATE tasks SET module='market' WHERE plan='corrupt' AND phase=0"
			case "wrong_actor":
				q = "UPDATE tasks SET actor=actor||'x' WHERE plan='corrupt' AND phase=0"
			case "unknown_module":
				q = "UPDATE plans SET config=replace(config,'tower','unknown_task') WHERE id='corrupt'"
			}
			if _, _, e := s.db.query(q); e != nil {
				t.Fatal(e)
			}
			root := s.Root
			s.Close()
			if _, e := Open(root); e == nil {
				t.Fatal("invalid ledger migrated", mode)
			}
			d, e := openNative(filepath.Join(root, FileName), true)
			if e != nil {
				t.Fatal(e)
			}
			defer d.close()
			r, _, _ := d.query("SELECT version FROM schema_info")
			if r[0][0] != "1" {
				t.Fatal("partial schema upgrade")
			}
		})
	}
}
func TestAppendToMigratedPlanUsesOnlyNewScopeWithoutRenumberingHistory(t *testing.T) {
	p := legacyFullPlan("append")
	p.Batches = p.Batches[:1]
	s := legacyFixture(t, t.TempDir(), p)
	// Archived activity does not make an otherwise pristine active plan busy.
	_, _, _ = s.db.query("UPDATE tasks SET state='RUNNING',attempt=1 WHERE module='daily'")
	before := oldTaskSnapshot(t, s)
	s = reopen(t, s)
	b := plan().Batches[1]
	if e := s.AppendBatch(p.ID, b); e != nil {
		t.Fatal(e)
	}
	got := sMustPlan(t, s, p.ID)
	if got.HistoricalBatches != 1 {
		t.Fatal("historical batch boundary extended")
	}
	if rows(t, s, "SELECT COUNT(*) FROM tasks WHERE plan=? AND phase>=110 AND enabled=0", p.ID)[0][0] != "0" {
		t.Fatal("new removed tasks generated")
	}
	after := oldTaskSnapshot(t, s)
	if !reflect.DeepEqual(before, after[:len(before)]) {
		t.Fatal("old phase identities changed on append")
	}
	if _, e := s.Next(p.ID, time.Now()); e != nil {
		t.Fatal(e)
	}
	var first *Phase
	for _, ph := range got.Phases() {
		if ph.Batch == 1 {
			copy := ph
			first = &copy
			break
		}
	}
	if first == nil || first.Number != 110 || first.Module != "team" {
		t.Fatal(first)
	}
	s = reopen(t, s)
	if _, e := s.Next(p.ID, time.Now()); e != nil {
		t.Fatal("reopen appended layout", e)
	}
}
func TestEmptyRemovedPlanIsArchivedAndActiveFailuresStillBlock(t *testing.T) {
	p := legacyFullPlan("only-removed")
	p.Modules = []string{"tower", "daily", "arena"}
	s := legacyFixture(t, t.TempDir(), p)
	s = reopen(t, s)
	if _, e := s.Next(p.ID, time.Now()); !errors.Is(e, ErrPlanComplete) {
		t.Fatal(e)
	}
	if done, e := s.GroupComplete(p.ID, 0, "一梦诛仙", 1); e == nil || done {
		t.Fatal("no active group reported as game success")
	}
	fresh := plan()
	if e := s.CreatePlan(fresh); e != nil {
		t.Fatal(e)
	}
	ph, _ := s.Next(fresh.ID, time.Now())
	actor := ActorKey(ph.Actors[0].Identity, ph.Server, ph.Slot)
	_ = s.SetTask(fresh.ID, ph.Number, actor, Running, "", "", 0)
	_ = s.SetTask(fresh.ID, ph.Number, actor, Failed, "", "failure", 0)
	if done, e := s.GroupComplete(fresh.ID, 0, ph.Server, ph.Slot); e != nil || done {
		t.Fatal("active failure counted complete")
	}
	if e := s.SetTask(fresh.ID, ph.Number, actor, Running, "", "", 0); e == nil {
		t.Fatal("retry exhaustion ignored")
	}
}
func TestRemovedInterruptedWorkCannotOpenMidnightBoundary(t *testing.T) {
	p := legacyFullPlan("midnight")
	s := legacyFixture(t, t.TempDir(), p)
	_, _, _ = s.db.query("UPDATE tasks SET state='RUNNING',attempt=1 WHERE module='daily'")
	s = reopen(t, s)
	if _, e := s.Next(p.ID, time.Now().Add(24*time.Hour)); e == nil {
		t.Fatal("archived activity began a new group after midnight")
	}
}
func TestCurrentScopeRejectsAllNineRemovedFeaturesAndHasNoMutableDefault(t *testing.T) {
	want := []string{"team", "eight_star", "fortune", "stronghold_prepare", "haidudu", "assets", "market", "battle_pass"}
	if !slices.Equal(EnabledModules(), want) {
		t.Fatal(EnabledModules())
	}
	copy := EnabledModules()
	copy[0] = "daily"
	if EnabledModules()[0] != "team" {
		t.Fatal("scope globally mutable")
	}
	for _, removed := range []string{"tower", "daily", "arena", "welfare", "main_quest", "power", "daily_like", "daily_life", "daily_merchant", "daily_rewards"} {
		p := plan()
		p.Modules = []string{removed}
		if p.Validate() == nil {
			t.Fatal("removed module allowed", removed)
		}
	}
	s := store(t)
	p := plan()
	p.Modules = want
	if e := s.CreatePlan(p); e != nil {
		t.Fatal(e)
	}
	for _, ph := range p.Phases() {
		if !moduleEnabled(ph.Module) {
			t.Fatal(ph)
		}
	}
	if rows(t, s, "SELECT COUNT(*) FROM tasks WHERE module IN ('tower','daily','arena')")[0][0] != "0" {
		t.Fatal("new removed tasks present")
	}
}
func TestProcessDeathDuringScopeMigrationRollsBack(t *testing.T) {
	p := legacyFullPlan("crash")
	s := legacyFixture(t, t.TempDir(), p)
	before := oldTaskSnapshot(t, s)
	root := s.Root
	s.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestScopeMigrationCrashChild$")
	cmd.Env = append(os.Environ(), "MHX_SCOPE_CRASH_DB="+root)
	if e := cmd.Run(); e != nil {
		t.Fatal(e)
	}
	d, e := openNative(filepath.Join(root, FileName), false)
	if e != nil {
		t.Fatal(e)
	}
	s = &Store{db: d, Root: root}
	if rows(t, s, "SELECT version FROM schema_info")[0][0] != "1" || !reflect.DeepEqual(before, oldTaskSnapshot(t, s)) {
		t.Fatal("uncommitted migration survived process death")
	}
	if _, _, e = d.query("SELECT enabled FROM tasks LIMIT 1"); e == nil {
		t.Fatal("uncommitted DDL survived")
	}
	s = reopen(t, s)
	if len(sMustPlan(t, s, p.ID).Modules) != 8 {
		t.Fatal("recovery migration failed")
	}
}
func TestScopeMigrationCrashChild(t *testing.T) {
	root := os.Getenv("MHX_SCOPE_CRASH_DB")
	if root == "" {
		return
	}
	d, e := openNative(filepath.Join(root, FileName), false)
	if e != nil {
		os.Exit(2)
	}
	if _, _, e = d.query("BEGIN IMMEDIATE"); e != nil {
		os.Exit(3)
	}
	if e = migrateScope(d); e != nil {
		os.Exit(4)
	}
	os.Exit(0) // no COMMIT, close, or deferred rollback
}

func TestMigratedBarriersRequireAllFiveAcrossEverySlotServerAndBatch(t *testing.T) {
	p := legacyFullPlan("barriers")
	s := legacyFixture(t, t.TempDir(), p)
	s = reopen(t, s)
	for _, expected := range sMustPlan(t, s, p.ID).Phases() {
		ph, e := s.Next(p.ID, time.Now())
		if e != nil || ph.Number != expected.Number || ph.Module != expected.Module {
			t.Fatal("barrier/layout mismatch", expected, ph, e)
		}
		for i, a := range ph.Actors {
			actor := ActorKey(a.Identity, ph.Server, ph.Slot)
			if e = s.SetTask(p.ID, ph.Number, actor, Running, "", "", 1); e != nil {
				t.Fatal(e)
			}
			if e = s.SetTask(p.ID, ph.Number, actor, Done, evidence(actor+ph.Module), "verified", 1); e != nil {
				t.Fatal(e)
			}
			if i == 3 {
				still, e := s.Next(p.ID, time.Now())
				if e != nil || still.Number != ph.Number {
					t.Fatal("one character advanced entire group", still, e)
				}
			}
		}
		if ph.Module == "market" || ph.Module == "battle_pass" {
			done, e := s.GroupComplete(p.ID, ph.Batch, ph.Server, ph.Slot)
			if e != nil || done != (ph.Module == "battle_pass") {
				t.Fatal("group completion ignored active module", ph, done, e)
			}
		}
	}
	if _, e := s.Next(p.ID, time.Now()); !errors.Is(e, ErrPlanComplete) {
		t.Fatal(e)
	}
}

func TestMissingFinancialTablesAreNeverSilentlyRecreatedByMigration(t *testing.T) {
	for _, table := range []string{"budgets", "orders", "transfers", "outbox"} {
		t.Run(table, func(t *testing.T) {
			s := legacyFixture(t, t.TempDir(), legacyFullPlan("missing-finance"))
			_, _, proof := seedMoney(t, s)
			originalProof, e := os.ReadFile(proof)
			if e != nil {
				t.Fatal(e)
			}
			_, _, e = s.db.query("PRAGMA foreign_keys=OFF")
			if e != nil {
				t.Fatal(e)
			}
			_, _, e = s.db.query("DROP TABLE " + table)
			if e != nil {
				t.Fatal(e)
			}
			root := s.Root
			s.Close()
			if e = VerifyFile(root); e == nil {
				t.Fatal("missing table passed startup health")
			}
			if _, e = Open(root); e == nil {
				t.Fatal("missing financial table recreated as empty")
			}
			d, e := openNative(filepath.Join(root, FileName), true)
			if e != nil {
				t.Fatal(e)
			}
			defer d.close()
			r, _, e := d.query("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table)
			if e != nil || r[0][0] != "0" {
				t.Fatal("missing table silently recreated", e)
			}
			r, _, e = d.query("SELECT version FROM schema_info")
			if e != nil || r[0][0] != "1" {
				t.Fatal("invalid financial ledger migrated", e)
			}
			b, e := os.ReadFile(proof)
			if e != nil || !slices.Equal(b, originalProof) {
				t.Fatal("proof changed", e)
			}
		})
	}
}
