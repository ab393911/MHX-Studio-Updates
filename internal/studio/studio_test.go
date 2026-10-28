package studio

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func evidence(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func store(t *testing.T) *Store {
	t.Helper()
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func frame() []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 16, 16)))
	return b.Bytes()
}
func receipt(t *testing.T, s *Store, o Order, tr Transfer) Receipt {
	t.Helper()
	p, h, e := s.SaveProof(o.Buyer, o.ID, tr.ID, frame(), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	return Receipt{Buyer: o.Buyer, Reference: ID(), ProofPath: p, ProofSHA: h, ResultEvidence: evidence("independent UI receipt"), Amount: tr.Amount, AfterSilver: 327000 - tr.Amount, AfterMerit: 345 - tr.Amount/10000*100}
}
func TestCapacityIsRoundedPerCharacterAndUsesIntegers(t *testing.T) {
	for _, tc := range [][3]int64{{327000, 345, 30000}, {9999, 900, 0}, {50000, 90, 0}, {50000, 120, 10000}} {
		n, e := Capacity(tc[0], tc[1])
		if e != nil || n != tc[2] {
			t.Fatal(n, e)
		}
	}
	n, e := SumCapacity([][2]int64{{9999, 90}, {9999, 90}})
	if e != nil || n != 0 {
		t.Fatal("pooled remainders", n, e)
	}
	for in, want := range map[string]int64{"32.7万": 327000, "0.0001万": 1, "10000": 10000} {
		n, e := ParseAmount(in)
		if e != nil || n != want {
			t.Fatal(in, n, e)
		}
	}
	if _, e := ParseAmount("1.5"); e == nil {
		t.Fatal("fractional silver accepted")
	}
}
func TestFiveDatabaseConnectionsCannotOverallocateAndConfirmTwice(t *testing.T) {
	s := store(t)
	o, e := enabledOrder(t, s, "buyer/../真实ID", 70000)
	if e != nil {
		t.Fatal(e)
	}
	var peers []*Store
	for i := 0; i < 5; i++ {
		p, e := Open(s.Root)
		if e != nil {
			t.Fatal(e)
		}
		peers = append(peers, p)
		defer p.Close()
		if e = p.SetBudget(fmt.Sprint(i), 327000, 345, evidence(fmt.Sprint(i)), time.Now()); e != nil {
			t.Fatal(e)
		}
	}
	var wg sync.WaitGroup
	transfers := make(chan Transfer, 5)
	for i, p := range peers {
		wg.Add(1)
		go func(i int, p *Store) {
			defer wg.Done()
			v, e := p.Reserve(o.ID, fmt.Sprint(i), fmt.Sprint("request", i), 30000)
			if e == nil {
				transfers <- v
			}
		}(i, p)
	}
	wg.Wait()
	close(transfers)
	held, e := s.Order(o.ID)
	if e != nil || held.Held != 70000 || held.Confirmed != 0 {
		t.Fatal(held, e)
	}
	for tr := range transfers {
		if e = s.MarkDispatched(tr.ID, o.Buyer, evidence("buyer identity")); e != nil {
			t.Fatal(e)
		}
		r := receipt(t, s, o, tr)
		if e = s.Confirm(tr.ID, r); e != nil {
			t.Fatal(e)
		}
		if e = s.Confirm(tr.ID, r); e != nil {
			t.Fatal("idempotent confirm", e)
		}
	}
	got, e := s.Order(o.ID)
	if e != nil || got.Confirmed != 70000 || got.Held != 0 || got.Completed == 0 {
		t.Fatal(got, e)
	}
	if _, e = s.Reserve(o.ID, "0", "another", 10000); e == nil {
		t.Fatal("overfilled order")
	}
	out, e := s.Outbox()
	if e != nil || len(out) != 3 {
		t.Fatal(out, e)
	}
	if e = VerifyFile(s.Root); e != nil {
		t.Fatal(e)
	}
}
func TestUncertainTransferHoldsQuotaAcrossRestartAndRejectsWrongBuyer(t *testing.T) {
	s := store(t)
	root := s.Root
	o, _ := enabledOrder(t, s, "buyer", 50000)
	if e := s.SetBudget("actor", 327000, 345, evidence("balance"), time.Now()); e != nil {
		t.Fatal(e)
	}
	tr, e := s.Reserve(o.ID, "actor", "request", 30000)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkDispatched(tr.ID, "wrong", evidence("id")); e == nil {
		t.Fatal("wrong buyer accepted")
	}
	if e = s.MarkDispatched(tr.ID, o.Buyer, evidence("id")); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.RecoverInterrupted(); e != nil {
		t.Fatal(e)
	}
	got, _ := s.Order(o.ID)
	if got.Confirmed != 0 || got.Held != 30000 {
		t.Fatal(got)
	}
	if _, e = s.Reserve(o.ID, "actor", "new", 10000); e == nil {
		t.Fatal("uncertain transfer resent")
	}
	if e = s.CancelReservation(tr.ID); e == nil {
		t.Fatal("uncertain amount released")
	}
	if e = s.SetBudget("actor", 327000, 345, evidence("new balance"), time.Now()); e == nil {
		t.Fatal("held budget overwritten")
	}
	again, e := s.Reserve(o.ID, "actor", "request", 30000)
	if e != nil || again.ID != tr.ID {
		t.Fatal(again, e)
	}
	if _, e = s.Reserve(o.ID, "actor", "request", 20000); e == nil {
		t.Fatal("idempotency payload conflict")
	}
}
func TestProofIsNotEnoughWithoutActualBalanceAndIdentityResult(t *testing.T) {
	s := store(t)
	o, _ := enabledOrder(t, s, "buyer", 10000)
	_ = s.SetBudget("actor", 327000, 345, evidence("b"), time.Now())
	tr, e := s.Reserve(o.ID, "actor", "r", 10000)
	if e != nil {
		t.Fatal(e)
	}
	_ = s.MarkDispatched(tr.ID, o.Buyer, evidence("buyer"))
	r := receipt(t, s, o, tr)
	r.AfterSilver++
	if e = s.Confirm(tr.ID, r); e == nil {
		t.Fatal("unreconciled receipt accepted")
	}
	got, _ := s.Order(o.ID)
	if got.Confirmed != 0 || got.Held != 10000 {
		t.Fatal(got)
	}
}
func TestProofRetentionOnlyAfterCompletionAndNeverDeletesLedger(t *testing.T) {
	s := store(t)
	o, _ := enabledOrder(t, s, "buyer", 10000)
	_ = s.SetBudget("actor", 327000, 345, evidence("b"), time.Now())
	tr, _ := s.Reserve(o.ID, "actor", "r", 10000)
	_ = s.MarkDispatched(tr.ID, o.Buyer, evidence("buyer"))
	r := receipt(t, s, o, tr)
	if e := s.Confirm(tr.ID, r); e != nil {
		t.Fatal(e)
	}
	pending, _ := s.CreateOrder("buyer", 10000)
	p, _, e := s.SaveProof(pending.Buyer, pending.ID, ID(), frame(), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	n, e := s.CleanupProofs(time.Now().Add(9 * 24 * time.Hour))
	if e != nil || n != 0 {
		t.Fatal(n, e)
	}
	n, e = s.CleanupProofs(time.Now().Add(11 * 24 * time.Hour))
	if e != nil || n != 1 {
		t.Fatal(n, e)
	}
	if _, e = os.Stat(p); e != nil {
		t.Fatal("unfinished proof deleted", e)
	}
	if _, e = os.Stat(r.ProofPath); !os.IsNotExist(e) {
		t.Fatal("old completed proof retained", e)
	}
	got, e := s.Order(o.ID)
	if e != nil || got.Confirmed != 10000 {
		t.Fatal("ledger deleted", got, e)
	}
}
func plan() Plan {
	actors := []Actor{{6, "a", "合欢"}, {17, "b", "天音"}, {29, "c", "青云"}, {42, "d", "鬼王"}, {77, "e", "鬼王"}}
	more := []Actor{{8, "f", "合欢"}, {18, "g", "天音"}, {30, "h", "青云"}, {43, "i", "鬼王"}, {78, "j", "鬼王"}}
	return Plan{ID: ID(), Day: Day(time.Now()), Batches: []Batch{{actors, []string{"一梦诛仙", "另一服"}}, {more, []string{"一梦诛仙"}}}, Modules: []string{"team", "eight_star"}}
}
func TestAllFiveSlotsThenServersThenNextBatchAndNoFailureSkip(t *testing.T) {
	s := store(t)
	p := plan()
	if e := s.CreatePlan(p); e != nil {
		t.Fatal(e)
	}
	phases := p.Phases()
	if phases[0].Slot != 1 || phases[8].Slot != 5 || phases[10].Server != "另一服" || phases[20].Batch != 1 {
		t.Fatal("bad schedule order", phases)
	}
	for n := 0; n < 20; n++ {
		next, e := s.Next(p.ID, time.Now())
		if e != nil || next.Number != n {
			t.Fatal(n, next, e)
		}
		for i, a := range next.Actors {
			k := ActorKey(a.Identity, next.Server, next.Slot)
			if e = s.SetTask(p.ID, n, k, Running, "", "", 1); e != nil {
				t.Fatal(e)
			}
			if n == 0 && i == 0 {
				if e = s.SetTask(p.ID, n, k, Skip, evidence("bad"), "retry_exhausted", 1); e == nil {
					t.Fatal("failed work skipped")
				}
			}
			if e = s.SetTask(p.ID, n, k, Done, evidence(fmt.Sprint(n, i)), "", 1); e != nil {
				t.Fatal(e)
			}
		}
	}
	next, e := s.Next(p.ID, time.Now())
	if e != nil || next.Batch != 1 || next.Slot != 1 {
		t.Fatal(next, e)
	}
}
func TestInterruptedTaskNeedsReconciliationAndMidnightStopsNewGroup(t *testing.T) {
	s := store(t)
	p := plan()
	_ = s.CreatePlan(p)
	ph, _ := s.Next(p.ID, time.Now())
	k := ActorKey(ph.Actors[0].Identity, ph.Server, ph.Slot)
	_ = s.SetTask(p.ID, 0, k, Running, "", "", 1)
	if e := s.RecoverInterrupted(); e != nil {
		t.Fatal(e)
	}
	if e := s.SetTask(p.ID, 0, k, Running, "", "", 1); e == nil {
		t.Fatal("interrupted task auto repeated")
	}
	if e := s.SetTask(p.ID, 0, k, Pending, evidence("actual reconciled UI"), "reconciled_no_action", 1); e != nil {
		t.Fatal(e)
	}
	fresh := plan()
	_ = s.CreatePlan(fresh)
	if _, e := s.Next(fresh.ID, time.Now().Add(24*time.Hour)); e == nil {
		t.Fatal("midnight started new group")
	}
}
func TestProcessDeathRollsBackUncommittedSQLiteTransaction(t *testing.T) {
	s := store(t)
	root := s.Root
	s.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashWriter$")
	cmd.Env = append(os.Environ(), "MHX_CRASH_DB="+root)
	if e := cmd.Run(); e != nil {
		t.Fatal(e)
	}
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	orders, e := s.Orders()
	if e != nil || len(orders) != 0 {
		t.Fatal(orders, e)
	}
}
func TestCrashWriter(t *testing.T) {
	root := os.Getenv("MHX_CRASH_DB")
	if root == "" {
		return
	}
	s, e := Open(root)
	if e != nil {
		os.Exit(2)
	}
	_, _, e = s.db.query("BEGIN IMMEDIATE")
	if e != nil {
		os.Exit(3)
	}
	_, _, e = s.db.query("INSERT INTO orders(id,buyer,total,created) VALUES('uncommitted','buyer',10000,1)")
	if e != nil {
		os.Exit(4)
	}
	os.Exit(0)
}
func TestPathTraversalAndSymlinkCannotDeleteOtherFolders(t *testing.T) {
	s := store(t)
	if _, _, e := s.SaveProof("b", "../../outside", ID(), frame(), time.Now()); e == nil {
		t.Fatal("path traversal accepted")
	}
	outside := t.TempDir()
	link := filepath.Join(s.Root, "proofs")
	if e := os.Symlink(outside, link); e != nil {
		t.Skip("symlink unavailable")
	}
	if _, _, e := s.SaveProof("buyer", ID(), ID(), frame(), time.Now()); e == nil {
		t.Fatal("symlink followed")
	}
}
func TestMarketAndDailyPoliciesDoNotConfusePricePositionOrCurrency(t *testing.T) {
	base := Listing{ItemID: "safe", Currency: "silver_ingot", PriceSource: "price", Quantity: 188, MarketLowest: 100, PriceUnit: 1, GameMin: 1, UserFloor: 50, Verified: true, ExcludedID: "excluded_specific_item"}
	n, e := ListingPrice(base)
	if e != nil || n != 99 {
		t.Fatal(n, e)
	}
	base.PriceSource = "listed_quantity"
	if _, e = ListingPrice(base); e == nil {
		t.Fatal("quantity used as price")
	}
	base.PriceSource = "price"
	base.Currency = "gold"
	if _, e = ListingPrice(base); e == nil {
		t.Fatal("wrong currency")
	}
	ids, e := DailyRefresh([]Card{{ID: "r", Title: "随机任务"}, {ID: "l", Title: "仙闻", Content: "在仙闻中点赞"}, {ID: "m", Title: "生活", Content: "使用一次生活技能", Done: true}}, 0)
	if e != nil || len(ids) != 1 || ids[0] != "r" {
		t.Fatal(ids, e)
	}
	if _, e = DailyRefresh(make([]Card, 3), 50); e == nil {
		t.Fatal("refresh limit ignored")
	}
}

func TestAppendBatchIsAtomicAndCannotRewriteExecutedPlan(t *testing.T) {
	s := store(t)
	p := plan()
	second := p.Batches[1]
	p.Batches = p.Batches[:1]
	if e := s.CreatePlan(p); e != nil {
		t.Fatal(e)
	}
	if e := s.AppendBatch(p.ID, second); e != nil {
		t.Fatal(e)
	}
	got, e := s.Plan(p.ID)
	if e != nil || len(got.Batches) != 2 {
		t.Fatal(got, e)
	}
	ph, _ := s.Next(p.ID, time.Now())
	key := ActorKey(ph.Actors[0].Identity, ph.Server, ph.Slot)
	_ = s.SetTask(p.ID, 0, key, Running, "", "", 1)
	if e = s.AppendBatch(p.ID, second); e == nil {
		t.Fatal("executed schedule changed")
	}
}

func enabledOrder(t *testing.T, s *Store, buyer string, n int64) (Order, error) {
	t.Helper()
	o, e := s.CreateOrder(buyer, n)
	if e != nil {
		return o, e
	}
	if e = s.AuthorizeDelivery(o.ID, buyer, evidence("simulated authorized buyer identity")); e != nil {
		t.Fatal(e)
	}
	return o, nil
}
func TestGuiRecordOnlyOrdersCannotBeReservedForDelivery(t *testing.T) {
	s := store(t)
	o, e := s.CreateOrder("buyer", 10000)
	if e != nil {
		t.Fatal(e)
	}
	_ = s.SetBudget("actor", 327000, 345, evidence("b"), time.Now())
	if o.DeliveryEnabled {
		t.Fatal("record-only order enabled")
	}
	if _, e = s.Reserve(o.ID, "actor", "x", 10000); e == nil {
		t.Fatal("record-only order dispatched")
	}
}
