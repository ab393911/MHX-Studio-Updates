package studio

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type TaskState string

const (
	Pending TaskState = "PENDING"
	Running TaskState = "RUNNING"
	Done    TaskState = "VERIFIED_DONE"
	Skip    TaskState = "CHECKED_SKIP"
	Failed  TaskState = "FAILED"
	Review  TaskState = "NEEDS_REVIEW"
)

// EnabledModules returns a copy so callers cannot mutate the active scope.
func EnabledModules() []string {
	return []string{"team", "eight_star", "fortune", "stronghold_prepare", "haidudu", "assets", "market", "battle_pass"}
}
func moduleEnabled(m string) bool {
	for _, v := range EnabledModules() {
		if m == v {
			return true
		}
	}
	return false
}
func legacyModules() []string {
	return []string{"team", "eight_star", "fortune", "stronghold_prepare", "haidudu", "assets", "market", "tower", "daily", "arena", "battle_pass"}
}

type Actor struct {
	Instance   int    `json:"instance"`
	Identity   string `json:"identity"`
	Profession string `json:"profession"`
}
type Batch struct {
	Actors  []Actor  `json:"actors"`
	Servers []string `json:"servers"`
}
type Plan struct {
	ID      string   `json:"id"`
	Day     string   `json:"day"`
	Batches []Batch  `json:"batches"`
	Modules []string `json:"modules"`
	// PhaseLayout preserves the original physical phase IDs of migrated plans.
	PhaseLayout       []string `json:"phase_layout,omitempty"`
	HistoricalBatches int      `json:"historical_batches,omitempty"`
}
type Phase struct {
	Number, Batch, Slot int
	Server, Module      string
	Actors              []Actor
	Tasks               []Task
}
type Task struct {
	Actor            string
	State            TaskState
	Attempt          int
	Evidence, Reason string
}

func validJob(j string) bool { return j == "合欢" || j == "天音" || j == "青云" || j == "鬼王" }
func (p Plan) Validate() error {
	if !orderedSubset(p.Modules, EnabledModules()) {
		return errors.New("enabled modules out of scope or order")
	}
	if !validText(p.ID) || len(p.Batches) == 0 || len(p.Batches) > 50 || len(p.Modules) == 0 || len(p.Modules) > len(EnabledModules()) {
		return errors.New("invalid plan")
	}
	if _, e := time.Parse("2006-01-02", p.Day); e != nil {
		return e
	}
	known := map[string]bool{}
	for _, m := range EnabledModules() {
		known[m] = true
	}
	seenM := map[string]bool{}
	for _, m := range p.Modules {
		if !known[m] || seenM[m] {
			return errors.New("unknown/duplicate module; removed tasks cannot be scheduled")
		}
		seenM[m] = true
	}
	seenActors := map[string]bool{}
	for _, b := range p.Batches {
		if len(b.Actors) != 5 || len(b.Servers) == 0 || len(b.Servers) > 30 {
			return errors.New("a batch requires exactly five actors and explicit servers")
		}
		seenIndex := map[int]bool{}
		jobs := map[string]int{}
		servers := map[string]bool{}
		for _, a := range b.Actors {
			if a.Instance < 0 || seenIndex[a.Instance] || !validText(a.Identity) || seenActors[a.Identity] || !validJob(a.Profession) {
				return errors.New("invalid, duplicate or unbound emulator identity/profession")
			}
			seenIndex[a.Instance] = true
			seenActors[a.Identity] = true
			jobs[a.Profession]++
		}
		if jobs["合欢"] != 1 || jobs["天音"] != 1 || jobs["青云"] != 1 || jobs["鬼王"] != 2 {
			return errors.New("five professions must be 合欢、天音、青云、鬼王、鬼王")
		}
		for _, v := range b.Servers {
			if !validText(v) || servers[v] {
				return errors.New("invalid/duplicate server")
			}
			servers[v] = true
		}
	}
	return nil
}
func (p Plan) layoutForBatch(batch int) []string {
	if batch < p.HistoricalBatches && len(p.PhaseLayout) != 0 {
		return p.PhaseLayout
	}
	return p.Modules
}
func (p Plan) Phases() []Phase {
	var out []Phase
	number := 0
	for bi, b := range p.Batches {
		for _, server := range b.Servers {
			for slot := 1; slot <= 5; slot++ {
				for _, m := range p.layoutForBatch(bi) {
					if moduleEnabled(m) {
						out = append(out, Phase{Number: number, Batch: bi, Slot: slot, Server: server, Module: m, Actors: b.Actors})
					}
					number++
				}
			}
		}
	}
	return out
}
func ActorKey(identity, server string, slot int) string {
	b, _ := json.Marshal([]any{identity, server, slot})
	return string(b)
}
func (s *Store) CreatePlan(p Plan) error {
	if len(p.PhaseLayout) != 0 || p.HistoricalBatches != 0 {
		return errors.New("only migration may assign a historical phase layout")
	}
	if e := p.Validate(); e != nil {
		return e
	}
	b, e := json.Marshal(p)
	if e != nil {
		return e
	}
	return s.tx(func(d *nativeDB) error {
		if _, _, e := d.query("INSERT INTO plans(id,day,config,created) VALUES(?,?,?,?)", p.ID, p.Day, string(b), num(time.Now().Unix())); e != nil {
			return e
		}
		for _, phase := range p.Phases() {
			for _, a := range phase.Actors {
				if _, _, e := d.query("INSERT INTO tasks(plan,phase,actor,module,updated) VALUES(?,?,?,?,?)", p.ID, num(int64(phase.Number)), ActorKey(a.Identity, phase.Server, phase.Slot), phase.Module, num(time.Now().Unix())); e != nil {
					return e
				}
			}
		}
		return nil
	})
}
func (s *Store) Plan(id string) (Plan, error) {
	var p Plan
	r, e := s.read("SELECT config FROM plans WHERE id=?", id)
	if e != nil {
		return p, e
	}
	if len(r) != 1 {
		return p, errors.New("plan not found")
	}
	e = json.Unmarshal([]byte(r[0][0]), &p)
	if e == nil && p.ID != id {
		e = errors.New("stored plan identity mismatch")
	}
	if e == nil {
		e = p.validateStored()
	}
	return p, e
}
func (s *Store) AppendBatch(id string, b Batch) error {
	return s.tx(func(d *nativeDB) error {
		rows, _, e := d.query("SELECT config FROM plans WHERE id=?", id)
		if e != nil {
			return e
		}
		if len(rows) != 1 {
			return errors.New("plan not found")
		}
		var p Plan
		if e = json.Unmarshal([]byte(rows[0][0]), &p); e != nil {
			return e
		}
		if len(p.Modules) == 0 {
			return errors.New("该计划已全部归档，请建立新的启用任务计划")
		}
		busy, _, e := d.query("SELECT COUNT(*) FROM tasks WHERE plan=? AND enabled=1 AND (state<>'PENDING' OR attempt>0)", id)
		if e != nil {
			return e
		}
		if busy[0][0] != "0" {
			return errors.New("计划已执行，不能变更批次结构")
		}
		before := len(p.Batches)
		p.Batches = append(p.Batches, b)
		if e = p.validateStored(); e != nil {
			return e
		}
		var fields map[string]json.RawMessage
		if e = json.Unmarshal([]byte(rows[0][0]), &fields); e != nil {
			return e
		}
		fields["batches"], e = json.Marshal(p.Batches)
		if e != nil {
			return e
		}
		raw, e := json.Marshal(fields)
		if e != nil {
			return e
		}
		if _, _, e = d.query("UPDATE plans SET config=? WHERE id=?", string(raw), id); e != nil {
			return e
		}
		for _, ph := range p.Phases() {
			if ph.Batch < before {
				continue
			}
			for _, a := range ph.Actors {
				if _, _, e = d.query("INSERT INTO tasks(plan,phase,actor,module,updated) VALUES(?,?,?,?,?)", id, num(int64(ph.Number)), ActorKey(a.Identity, ph.Server, ph.Slot), ph.Module, num(time.Now().Unix())); e != nil {
					return e
				}
			}
		}
		return nil
	})
}
func (s *Store) LatestPlan() (Plan, error) {
	r, e := s.read("SELECT id FROM plans ORDER BY created DESC,rowid DESC LIMIT 1")
	if e != nil {
		return Plan{}, e
	}
	if len(r) == 0 {
		return Plan{}, errors.New("尚无五开计划")
	}
	return s.Plan(r[0][0])
}

var ErrPlanComplete = errors.New("all specified servers in all batches are complete")

func planInDB(d *nativeDB, id string) (Plan, error) {
	var p Plan
	rows, _, e := d.query("SELECT config FROM plans WHERE id=?", id)
	if e != nil {
		return p, e
	}
	if len(rows) != 1 {
		return p, errors.New("plan not found")
	}
	if e = json.Unmarshal([]byte(rows[0][0]), &p); e != nil {
		return p, e
	}
	if p.ID != id {
		return p, errors.New("stored plan identity mismatch")
	}
	return p, p.validateStored()
}
func (s *Store) Next(id string, now time.Time) (phase Phase, err error) {
	err = s.tx(func(d *nativeDB) error {
		p, e := planInDB(d, id)
		if e != nil {
			return e
		}
		if e = validateLedger(d, p); e != nil {
			return e
		}
		r, _, e := d.query("SELECT MIN(phase) FROM tasks WHERE plan=? AND enabled=1 AND state NOT IN ('VERIFIED_DONE','CHECKED_SKIP')", id)
		if e != nil {
			return e
		}
		if len(r) == 0 || r[0][0] == "" {
			return ErrPlanComplete
		}
		n := int(integer(r[0][0]))
		found := false
		for _, ph := range p.Phases() {
			if ph.Number == n {
				phase = ph
				found = true
				break
			}
		}
		if !found {
			return errors.New("invalid stored phase")
		}
		start, width := p.groupBounds(phase)
		started, _, e := d.query("SELECT COUNT(*) FROM tasks WHERE plan=? AND enabled=1 AND phase>=? AND phase<? AND (state<>'PENDING' OR attempt>0)", id, num(int64(start)), num(int64(start+width)))
		if e != nil {
			return e
		}
		if Day(now) != p.Day && phase.Module == p.Modules[0] && started[0][0] == "0" {
			return errors.New("NEW_DAY_PENDING：当前小组已安全收尾，跨日边界待确认；不开始新小组")
		}
		rows, _, e := d.query("SELECT actor,state,attempt,evidence,reason FROM tasks WHERE plan=? AND phase=? AND enabled=1 ORDER BY actor", id, num(int64(n)))
		if e != nil {
			return e
		}
		for _, v := range rows {
			phase.Tasks = append(phase.Tasks, Task{Actor: v[0], State: TaskState(v[1]), Attempt: int(integer(v[2])), Evidence: v[3], Reason: v[4]})
		}
		return nil
	})
	return
}

// GroupComplete counts only enabled modules, but verifies the entire ledger so
// a missing task cannot turn an incomplete character/group into a completed one.
func (s *Store) GroupComplete(id string, batch int, server string, slot int) (done bool, err error) {
	err = s.tx(func(d *nativeDB) error {
		p, e := planInDB(d, id)
		if e != nil {
			return e
		}
		if e = validateLedger(d, p); e != nil {
			return e
		}
		found := false
		done = true
		for _, ph := range p.Phases() {
			if ph.Batch != batch || ph.Server != server || ph.Slot != slot {
				continue
			}
			found = true
			rows, _, e := d.query("SELECT COUNT(*) FROM tasks WHERE plan=? AND phase=? AND enabled=1 AND state NOT IN ('VERIFIED_DONE','CHECKED_SKIP')", id, num(int64(ph.Number)))
			if e != nil {
				return e
			}
			if rows[0][0] != "0" {
				done = false
			}
		}
		if !found {
			done = false
			return errors.New("unknown group")
		}
		return nil
	})
	return
}
func (s *Store) SetTask(id string, phase int, actor string, to TaskState, evidence, reason string, maxRetries int) error {
	if maxRetries < 0 || maxRetries > 20 {
		return errors.New("invalid retry limit")
	}
	if (to == Done || to == Skip) && !hashOK(evidence) {
		return errors.New("terminal task requires a verified screen hash")
	}
	if to == Skip && reason != "already_done" && reason != "no_red_dot" && reason != "no_attempts" && reason != "resource_insufficient" {
		return errors.New("failure or retry exhaustion is not a checked skip")
	}
	return s.tx(func(d *nativeDB) error {
		r, _, e := d.query("SELECT state,attempt,module,enabled FROM tasks WHERE plan=? AND phase=? AND actor=?", id, num(int64(phase)), actor)
		if e != nil {
			return e
		}
		if len(r) != 1 {
			return errors.New("task not found")
		}
		if r[0][3] != "1" || !moduleEnabled(r[0][2]) {
			return errors.New("archived task is read-only and cannot execute")
		}
		p, e := planInDB(d, id)
		if e != nil {
			return e
		}
		matched := false
		for _, ph := range p.Phases() {
			if ph.Number == phase && ph.Module == r[0][2] {
				for _, a := range ph.Actors {
					if ActorKey(a.Identity, ph.Server, ph.Slot) == actor {
						matched = true
					}
				}
			}
		}
		if !matched {
			return errors.New("task identity does not match its immutable phase")
		}
		from := TaskState(r[0][0])
		attempt := integer(r[0][1])
		if from == Done || from == Skip {
			if from == to && evidence != "" {
				return nil
			}
			return errors.New("verified terminal task cannot be rewritten")
		}
		if to == Running {
			earliest, _, e := d.query("SELECT MIN(phase) FROM tasks WHERE plan=? AND enabled=1 AND state NOT IN ('VERIFIED_DONE','CHECKED_SKIP')", id)
			if e != nil {
				return e
			}
			if earliest[0][0] != num(int64(phase)) {
				return errors.New("preceding phase/group/server/batch is unfinished")
			}
			if from != Pending && from != Failed {
				return errors.New("task needs reconciliation before restart")
			}
			if attempt >= int64(maxRetries+1) {
				return errors.New("retry limit reached; task stays failed")
			}
			attempt++
		} else if to == Pending {
			if from != Review {
				return errors.New("only reviewed interrupted work can be reset")
			}
			if !hashOK(evidence) || reason != "reconciled_no_action" {
				return errors.New("reset needs actual state reconciliation")
			}
		} else if to == Done || to == Skip || to == Failed || to == Review {
			if from != Running && !(to == Skip && from == Pending) {
				return errors.New("invalid task state transition")
			}
		} else {
			return errors.New("unknown task state")
		}
		_, _, e = d.query("UPDATE tasks SET state=?,attempt=?,evidence=?,reason=?,updated=? WHERE plan=? AND phase=? AND actor=?", string(to), num(attempt), evidence, reason, num(time.Now().Unix()), id, num(int64(phase)), actor)
		return e
	})
}
func (s *Store) RecoverInterrupted() error {
	return s.tx(func(d *nativeDB) error {
		for _, q := range []string{"UPDATE tasks SET state='NEEDS_REVIEW',reason='interrupted_reconcile_game' WHERE enabled=1 AND state='RUNNING'", "UPDATE transfers SET state='NEEDS_REVIEW' WHERE state='DISPATCHED'"} {
			if _, _, e := d.query(q); e != nil {
				return e
			}
		}
		_, _, e := d.query("INSERT INTO events(kind,object,detail,created) VALUES('recovery','studio','interrupted work held; no automatic resend',?)", num(time.Now().Unix()))
		return e
	})
}
func PhaseLabel(p Phase) string {
	return fmt.Sprintf("第%d批 / %s / 角色%d小组 / %s", p.Batch+1, p.Server, p.Slot, ModuleLabel(p.Module))
}

func ModuleLabel(m string) string {
	v := map[string]string{"team": "组队", "eight_star": "八星打符与幻形", "fortune": "聚福激活", "stronghold_prepare": "据点准备", "haidudu": "海嘟嘟", "assets": "银锭与功绩统计", "market": "交易行", "battle_pass": "仙盟战令"}
	if s, ok := v[m]; ok {
		return s
	}
	return m
}
func StateLabel(s TaskState) string {
	v := map[TaskState]string{Pending: "待执行", Running: "执行中", Done: "已核验完成", Skip: "已核验条件跳过", Failed: "失败", Review: "待核查"}
	return v[s]
}
