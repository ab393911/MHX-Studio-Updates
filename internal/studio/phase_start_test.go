package studio

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGroupPhaseStartIsAllOrNothingAndCannotDoubleAcquire(t *testing.T) {
	s := store(t)
	p := plan()
	if e := s.CreatePlan(p); e != nil {
		t.Fatal(e)
	}
	phase, e := s.Next(p.ID, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if e = s.StartPhase(p.ID, phase.Number+1, 3); e == nil {
		t.Fatal("started later phase")
	}
	if e = s.StartPhase(p.ID, phase.Number, 3); e != nil {
		t.Fatal(e)
	}
	got, e := s.Next(p.ID, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	for _, task := range got.Tasks {
		if task.State != Running || task.Attempt != 1 {
			t.Fatal(task)
		}
	}
	if e = s.StartPhase(p.ID, phase.Number, 3); e == nil {
		t.Fatal("two executors acquired phase")
	}
}
func TestRetryExhaustionAndDatabaseFailureRollBackEveryActor(t *testing.T) {
	s := store(t)
	p := plan()
	if e := s.CreatePlan(p); e != nil {
		t.Fatal(e)
	}
	ph, e := s.Next(p.ID, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	actor := ph.Tasks[4].Actor
	if e = s.SetTask(p.ID, ph.Number, actor, Running, "", "test", 0); e != nil {
		t.Fatal(e)
	}
	if e = s.SetTask(p.ID, ph.Number, actor, Failed, "", "test", 0); e != nil {
		t.Fatal(e)
	}
	before, e := s.Next(p.ID, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if e = s.StartPhase(p.ID, ph.Number, 0); e == nil {
		t.Fatal("exhausted actor accepted")
	}
	after, _ := s.Next(p.ID, time.Now())
	if !reflect.DeepEqual(before.Tasks, after.Tasks) {
		t.Fatal("partial start on retry failure", before.Tasks, after.Tasks)
	}
	trigger := "CREATE TRIGGER fail_group_start BEFORE UPDATE OF state ON tasks WHEN NEW.state='RUNNING' AND NEW.actor='" + strings.ReplaceAll(actor, "'", "''") + "' BEGIN SELECT RAISE(ABORT,'group-start-fault'); END"
	if e = s.tx(func(d *nativeDB) error { _, _, e := d.query(trigger); return e }); e != nil {
		t.Fatal(e)
	}
	if e = s.StartPhase(p.ID, ph.Number, 3); e == nil {
		t.Fatal("database fault did not fail group start")
	}
	after, _ = s.Next(p.ID, time.Now())
	if !reflect.DeepEqual(before.Tasks, after.Tasks) {
		t.Fatal("partial start after database fault", before.Tasks, after.Tasks)
	}
}
