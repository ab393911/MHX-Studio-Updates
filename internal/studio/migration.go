package studio

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const scopeRevision = "2026-10-29-remove-nine"

func orderedSubset(values, order []string) bool {
	last := -1
	for _, v := range values {
		n := slices.Index(order, v)
		if n < 0 || n <= last {
			return false
		}
		last = n
	}
	return true
}
func (p Plan) validateStored() error {
	// A legacy plan containing only removed modules is an archived, empty plan,
	// never a license to invent a new task or a successful game result.
	meta := p
	if len(p.Modules) == 0 && len(p.PhaseLayout) > 0 {
		meta.Modules = []string{"team"}
	}
	if e := meta.Validate(); e != nil {
		return e
	}
	if !orderedSubset(p.Modules, EnabledModules()) {
		return errors.New("enabled modules out of scope or order")
	}
	if len(p.PhaseLayout) == 0 {
		if p.HistoricalBatches != 0 {
			return errors.New("historical layout missing")
		}
		return nil
	}
	if p.HistoricalBatches < 1 || p.HistoricalBatches > len(p.Batches) {
		return errors.New("invalid historical batch count")
	}
	if !orderedSubset(p.PhaseLayout, legacyModules()) {
		return errors.New("invalid historical phase layout")
	}
	var active []string
	for _, m := range p.PhaseLayout {
		if moduleEnabled(m) {
			active = append(active, m)
		}
	}
	if !slices.Equal(active, p.Modules) {
		return errors.New("scope/layout mismatch")
	}
	return nil
}
func legacyPlan(raw, id string) (Plan, error) {
	var p Plan
	if e := json.Unmarshal([]byte(raw), &p); e != nil {
		return p, e
	}
	if p.ID != id || (len(p.PhaseLayout) != 0 || p.HistoricalBatches != 0) || len(p.Modules) == 0 || !orderedSubset(p.Modules, legacyModules()) {
		return p, errors.New("invalid legacy plan layout")
	}
	p.PhaseLayout = append([]string(nil), p.Modules...)
	p.HistoricalBatches = len(p.Batches)
	p.Modules = nil
	for _, m := range p.PhaseLayout {
		if moduleEnabled(m) {
			p.Modules = append(p.Modules, m)
		}
	}
	return p, p.validateStored()
}

// validateLedger compares identity/module/phase, not just row counts. Each batch
// is read separately to stay below the bounded SQLite result buffer.
func validateLedger(d *nativeDB, p Plan) error {
	phaseBase := 0
	for bi, batch := range p.Batches {
		expected := map[string]string{}
		start := phaseBase
		for _, server := range batch.Servers {
			for slot := 1; slot <= 5; slot++ {
				for _, module := range p.layoutForBatch(bi) {
					flag := "0"
					if moduleEnabled(module) {
						flag = "1"
					}
					for _, a := range batch.Actors {
						expected[num(int64(phaseBase))+"/"+ActorKey(a.Identity, server, slot)] = module + "/" + flag
					}
					phaseBase++
				}
			}
		}
		rows, _, e := d.query("SELECT phase,actor,module,enabled FROM tasks WHERE plan=? AND phase>=? AND phase<?", p.ID, num(int64(start)), num(int64(phaseBase)))
		if e != nil {
			return e
		}
		for _, r := range rows {
			key := r[0] + "/" + r[1]
			if want, ok := expected[key]; !ok || want != r[2]+"/"+r[3] {
				return errors.New("任务身份、模块或启用范围与原阶段不符")
			}
			delete(expected, key)
		}
		if len(expected) != 0 {
			return errors.New("任务账本不完整，暂停而非误报完成")
		}
	}
	rows, _, e := d.query("SELECT COUNT(*) FROM tasks WHERE plan=? AND (phase<0 OR phase>=?)", p.ID, num(int64(phaseBase)))
	if e != nil {
		return e
	}
	if rows[0][0] != "0" {
		return errors.New("unknown physical phase in task ledger")
	}
	return nil
}
func verifyLegacy(d *nativeDB) error {
	rows, _, e := d.query("SELECT id,config FROM plans ORDER BY id")
	if e != nil {
		return e
	}
	for _, r := range rows {
		if _, e = legacyPlan(r[1], r[0]); e != nil {
			return fmt.Errorf("legacy plan %s: %w", r[0], e)
		}
	}
	return nil
}
func verifyScope(d *nativeDB) error {
	if _, _, e := d.query("SELECT COUNT(*) FROM plan_scope_history"); e != nil {
		return e
	}
	fk, _, e := d.query("PRAGMA foreign_key_check")
	if e != nil {
		return e
	}
	if len(fk) != 0 {
		return errors.New("studio foreign key check failed")
	}
	rows, _, e := d.query("SELECT id,config FROM plans ORDER BY id")
	if e != nil {
		return e
	}
	for _, r := range rows {
		p, e := planInDB(d, r[0])
		if e != nil {
			return e
		}
		if e = validateLedger(d, p); e != nil {
			return fmt.Errorf("plan %s: %w", p.ID, e)
		}
	}
	return nil
}

// migrateScope runs only inside Store.tx: schema DDL, scope flags, original
// configs and version commit together. It never rewrites task phase/state,
// attempts, evidence, timestamps, budgets, orders, transfers, outbox or proofs.
func migrateScope(d *nativeDB) error {
	if e := verifyLegacy(d); e != nil {
		return e
	}
	for _, q := range []string{
		"ALTER TABLE tasks ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1))",
		"CREATE TABLE plan_scope_history(plan TEXT PRIMARY KEY REFERENCES plans(id),original_config TEXT NOT NULL,revision TEXT NOT NULL,archived INTEGER NOT NULL)",
		"CREATE INDEX tasks_enabled_phase ON tasks(plan,enabled,phase)",
	} {
		if _, _, e := d.query(q); e != nil {
			return e
		}
	}
	rows, _, e := d.query("SELECT id,config FROM plans ORDER BY id")
	if e != nil {
		return e
	}
	for _, r := range rows {
		p, e := legacyPlan(r[1], r[0])
		if e != nil {
			return e
		}
		// Flag only the removed scope; any subsequent validation failure rolls
		// this back together with the ALTER TABLE and all earlier plans.
		if _, _, e = d.query("UPDATE tasks SET enabled=0 WHERE plan=? AND module IN ('tower','daily','arena')", p.ID); e != nil {
			return e
		}
		if e = validateLedger(d, p); e != nil {
			return fmt.Errorf("legacy plan %s: %w", p.ID, e)
		}
		if slices.Equal(p.Modules, p.PhaseLayout) {
			continue
		}
		var fields map[string]json.RawMessage
		if e = json.Unmarshal([]byte(r[1]), &fields); e != nil {
			return e
		}
		fields["modules"], e = json.Marshal(p.Modules)
		if e != nil {
			return e
		}
		fields["historical_batches"], e = json.Marshal(p.HistoricalBatches)
		if e != nil {
			return e
		}
		fields["phase_layout"], e = json.Marshal(p.PhaseLayout)
		if e != nil {
			return e
		}
		raw, e := json.Marshal(fields)
		if e != nil {
			return e
		}
		if _, _, e = d.query("INSERT INTO plan_scope_history(plan,original_config,revision,archived) VALUES(?,?,?,?)", p.ID, r[1], scopeRevision, num(time.Now().Unix())); e != nil {
			return e
		}
		if _, _, e = d.query("UPDATE plans SET config=? WHERE id=?", string(raw), p.ID); e != nil {
			return e
		}
	}
	if _, _, e = d.query("UPDATE schema_info SET version=2"); e != nil {
		return e
	}
	return verifyScope(d)
}

// MigrateExisting is used only after the updater has verified its configuration
// snapshot, before startup health acknowledgement. No database means no work.
func MigrateExisting(root string) error {
	if _, e := os.Stat(filepath.Join(root, FileName)); os.IsNotExist(e) {
		return nil
	} else if e != nil {
		return e
	}
	s, e := Open(root)
	if e != nil {
		return e
	}
	return s.Close()
}

func (p Plan) groupBounds(ph Phase) (start, width int) {
	for bi, b := range p.Batches {
		w := len(p.layoutForBatch(bi))
		if bi == ph.Batch {
			return start + (slices.Index(b.Servers, ph.Server)*5+ph.Slot-1)*w, w
		}
		start += len(b.Servers) * 5 * w
	}
	return 0, 0
}

// Missing financial tables must never be silently recreated as empty ledgers.
// Initialization is allowed only when the database file did not exist at Open.
func validateBaseTables(d *nativeDB) error {
	for _, name := range []string{"schema_info", "plans", "tasks", "budgets", "orders", "transfers", "outbox", "events"} {
		r, _, e := d.query("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name)
		if e != nil {
			return e
		}
		if len(r) != 1 || r[0][0] != "1" {
			return fmt.Errorf("existing database is missing %s; original retained, no empty replacement created", name)
		}
	}
	return nil
}
