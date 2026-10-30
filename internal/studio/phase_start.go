package studio

import (
	"errors"
	"time"
)

// StartPhase acquires every unfinished actor task in one SQLite transaction.
// A late actor's exhausted retry budget or a database error cannot leave only
// part of the five marked running before the executor is even invoked.
func (s *Store) StartPhase(id string, number, maxRetries int) error {
	if maxRetries < 0 || maxRetries > 20 {
		return errors.New("invalid retry limit")
	}
	return s.tx(func(d *nativeDB) error {
		p, e := planInDB(d, id)
		if e != nil {
			return e
		}
		if e = validateLedger(d, p); e != nil {
			return e
		}
		rows, _, e := d.query("SELECT MIN(phase) FROM tasks WHERE plan=? AND enabled=1 AND state NOT IN ('VERIFIED_DONE','CHECKED_SKIP')", id)
		if e != nil {
			return e
		}
		if len(rows) != 1 || rows[0][0] != num(int64(number)) {
			return errors.New("preceding phase/group/server/batch is unfinished or phase complete")
		}
		var phase Phase
		found := false
		for _, ph := range p.Phases() {
			if ph.Number == number {
				phase = ph
				found = true
				break
			}
		}
		if !found {
			return errors.New("unknown active phase")
		}
		if Day(time.Now()) != p.Day && phase.Module == p.Modules[0] {
			start, width := p.groupBounds(phase)
			r, _, e := d.query("SELECT COUNT(*) FROM tasks WHERE plan=? AND enabled=1 AND phase>=? AND phase<? AND (state<>'PENDING' OR attempt>0)", id, num(int64(start)), num(int64(start+width)))
			if e != nil {
				return e
			}
			if r[0][0] == "0" {
				return errors.New("NEW_DAY_PENDING：未确认跨日边界，不启动新小组")
			}
		}
		rows, _, e = d.query("SELECT state,attempt FROM tasks WHERE plan=? AND phase=? AND enabled=1", id, num(int64(number)))
		if e != nil {
			return e
		}
		if len(rows) != 5 {
			return errors.New("phase must contain all five actor tasks")
		}
		pending := 0
		for _, r := range rows {
			state := TaskState(r[0])
			if state == Done || state == Skip {
				continue
			}
			if state != Pending && state != Failed {
				return errors.New("task needs reconciliation before restart")
			}
			if integer(r[1]) >= int64(maxRetries+1) {
				return errors.New("retry limit reached; entire phase remains unchanged")
			}
			pending++
		}
		_, count, e := d.query("UPDATE tasks SET state='RUNNING',attempt=attempt+1,reason='verified_group_start',updated=? WHERE plan=? AND phase=? AND enabled=1 AND state IN ('PENDING','FAILED')", num(time.Now().Unix()), id, num(int64(number)))
		if e == nil && count != pending {
			return errors.New("phase ownership changed")
		}
		return e
	})
}
