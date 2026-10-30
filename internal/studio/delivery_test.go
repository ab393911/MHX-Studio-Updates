package studio

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type giftFixture struct {
	actor, buyer                                string
	want                                        int64
	wrongDraft, ambiguous, badBalance, noFriend bool
	submits                                     atomic.Int32
	find                                        func()
}

func (p *giftFixture) FindOrAddFriend(ctx context.Context, buyer string) (BuyerProof, error) {
	if p.find != nil {
		p.find()
	}
	return BuyerProof{Buyer: p.buyer, Actor: p.actor, FriendAccepted: !p.noFriend, ScreenSHA: evidence("buyer"), ObservedAt: time.Now()}, ctx.Err()
}
func (p *giftFixture) ReadBalance(ctx context.Context) (BalanceProof, error) {
	return BalanceProof{Actor: p.actor, Silver: 327000, Merit: 345, ScreenSHA: evidence("balance:" + p.actor), ObservedAt: time.Now()}, ctx.Err()
}
func (p *giftFixture) PrepareGift(ctx context.Context, buyer string, n int64) (GiftDraft, error) {
	p.want = n
	if p.wrongDraft {
		buyer = "different buyer"
	}
	return GiftDraft{Buyer: buyer, Actor: p.actor, Amount: n, SilverCurrency: true, ScreenSHA: evidence("draft"), ObservedAt: time.Now()}, ctx.Err()
}
func (p *giftFixture) SubmitGift(ctx context.Context, d GiftDraft) (GiftResult, error) {
	p.submits.Add(1)
	r := GiftResult{Buyer: p.buyer, Actor: p.actor, Reference: ID(), Amount: d.Amount, AfterSilver: 327000 - d.Amount, AfterMerit: 345 - d.Amount/10000*100, ResultSHA: evidence("result"), SuccessVerified: true, Frame: frame(), ObservedAt: time.Now()}
	if p.ambiguous {
		return r, errors.New("network after click")
	}
	if p.badBalance {
		r.AfterSilver++
	}
	return r, ctx.Err()
}
func TestDeliveryWorkerSendsExactAllocationAndQueuesCurrentActorMessage(t *testing.T) {
	s := store(t)
	o, e := s.CreateOrder("123456", 3000000)
	if e != nil {
		t.Fatal(e)
	}
	a := ActorKey("device2", "一梦诛仙", 3)
	p := &giftFixture{actor: a, buyer: o.Buyer}
	r, e := s.DeliverOne(context.Background(), p, DeliveryRequest{Order: o.ID, Actor: a, RequestID: "one", Maximum: o.Total})
	if e != nil || r.State != "CONFIRMED" || r.Amount != 30000 || p.submits.Load() != 1 {
		t.Fatal(r, e)
	}
	got, _ := s.Order(o.ID)
	if got.Confirmed != 30000 || got.Remaining != 2970000 || got.Held != 0 {
		t.Fatal(got)
	}
	q, _ := s.Outbox()
	if len(q) != 1 || !strings.Contains(q[0][2], "一梦诛仙第3个") || !strings.Contains(q[0][2], "本次交付30000") || !strings.Contains(q[0][2], "剩余2970000") {
		t.Fatal(q)
	}
	stats, e := s.Assets()
	if e != nil || len(stats) != 1 || stats[0].EffectiveMerit != 300 || stats[0].Capacity != 30000 || stats[0].Confirmed != 30000 {
		t.Fatal(stats, e)
	}
	_, e = s.DeliverOne(context.Background(), p, DeliveryRequest{Order: o.ID, Actor: a, RequestID: "one", Maximum: o.Total})
	if e == nil || p.submits.Load() != 1 {
		t.Fatal("repeated worker submitted", e)
	}
}
func TestDeliveryWorkerFailuresNeverResendOrReleaseUncertainQuota(t *testing.T) {
	for _, scenario := range []string{"wrong buyer", "not accepted", "wrong draft", "ambiguous", "wrong balance"} {
		t.Run(scenario, func(t *testing.T) {
			s := store(t)
			o, _ := s.CreateOrder("123456", 3000000)
			p := &giftFixture{actor: "actor", buyer: o.Buyer}
			switch scenario {
			case "wrong buyer":
				p.buyer = "other"
			case "not accepted":
				p.noFriend = true
			case "wrong draft":
				p.wrongDraft = true
			case "ambiguous":
				p.ambiguous = true
			case "wrong balance":
				p.badBalance = true
			}
			tr, e := s.DeliverOne(context.Background(), p, DeliveryRequest{o.ID, "actor", "one", 30000})
			if e == nil {
				t.Fatal("failure accepted")
			}
			got, _ := s.Order(o.ID)
			if got.Confirmed != 0 {
				t.Fatal(got)
			}
			if scenario == "ambiguous" || scenario == "wrong balance" {
				if got.Held != 30000 || tr.State != "NEEDS_REVIEW" || p.submits.Load() != 1 {
					t.Fatal(got, tr)
				}
			} else if got.Held != 0 || p.submits.Load() != 0 {
				t.Fatal(got, tr)
			}
			q, _ := s.Outbox()
			if len(q) != 0 {
				t.Fatal("failure queued success message")
			}
		})
	}
}
func TestConcurrentWorkersWithSameRequestCanOnlySubmitOnce(t *testing.T) {
	s := store(t)
	o, _ := s.CreateOrder("123456", 10000)
	peer, e := Open(s.Root)
	if e != nil {
		t.Fatal(e)
	}
	defer peer.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	p := &giftFixture{actor: "a", buyer: o.Buyer, find: func() { close(entered); <-release }}
	done := make(chan error, 1)
	go func() {
		_, e := s.DeliverOne(context.Background(), p, DeliveryRequest{o.ID, "a", "same", 10000})
		done <- e
	}()
	<-entered
	other := &giftFixture{actor: "a", buyer: o.Buyer}
	if _, e = peer.DeliverOne(context.Background(), other, DeliveryRequest{o.ID, "a", "same", 10000}); e == nil {
		t.Fatal("two processes acquired one request")
	}
	close(release)
	if e = <-done; e != nil || p.submits.Load() != 1 || other.submits.Load() != 0 {
		t.Fatal(e)
	}
}
func TestFiveDeliveryWorkersDoNotExceedOrderAndKeepFiveProofs(t *testing.T) {
	s := store(t)
	o, _ := s.CreateOrder("123456", 150000)
	// Authorization occurs once; production also permits the workers to race,
	// with a loser safely stopping rather than weakening the authorization gate.
	if e := s.AuthorizeDelivery(o.ID, o.Buyer, evidence("buyer")); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a := fmt.Sprint("actor", i)
			p := &giftFixture{actor: a, buyer: o.Buyer}
			_, e := s.DeliverOne(context.Background(), p, DeliveryRequest{o.ID, a, fmt.Sprint("request", i), 30000})
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	got, _ := s.Order(o.ID)
	if got.Confirmed != 150000 || got.Held != 0 || got.Completed == 0 {
		t.Fatal(got)
	}
	q, _ := s.Outbox()
	if len(q) != 5 {
		t.Fatal(q)
	}
	if _, e := s.DeliverOne(context.Background(), &giftFixture{actor: "six", buyer: o.Buyer}, DeliveryRequest{o.ID, "six", "six", 30000}); e == nil {
		t.Fatal("completed order still executed")
	}
}

type chatFixture struct {
	actor, buyer string
	sends        int
	fail, wrong  bool
	sent         []string
}

func (p *chatFixture) VerifyChat(ctx context.Context, buyer, actor string) (BuyerProof, error) {
	if p.wrong {
		actor = "other actor"
	}
	return BuyerProof{Actor: actor, Buyer: buyer, FriendAccepted: true, ScreenSHA: evidence("chat identity"), ObservedAt: time.Now()}, ctx.Err()
}
func (p *chatFixture) SendChat(ctx context.Context, buyer, message string) (ChatReceipt, error) {
	p.sends++
	p.sent = append(p.sent, message)
	r := ChatReceipt{Actor: p.actor, Buyer: p.buyer, Message: message, VisibleInHistory: true, ScreenSHA: evidence(message), ObservedAt: time.Now()}
	if p.fail {
		return r, errors.New("unknown message result")
	}
	return r, ctx.Err()
}
func confirmedFixture(t *testing.T, s *Store, actor string) (Order, Transfer) {
	t.Helper()
	o, e := s.CreateOrder("123456", 30000)
	if e != nil {
		t.Fatal(e)
	}
	tr, e := s.DeliverOne(context.Background(), &giftFixture{actor: actor, buyer: o.Buyer}, DeliveryRequest{o.ID, actor, ID(), 30000})
	if e != nil {
		t.Fatal(e)
	}
	return o, tr
}
func TestBuyerMessagesUseActualSendingCharacterAndCannotDuplicateOnRestart(t *testing.T) {
	s := store(t)
	o, tr := confirmedFixture(t, s, "actual sender")
	p := &chatFixture{actor: tr.Actor, buyer: o.Buyer}
	n, e := s.NotifyNext(context.Background(), map[string]ChatPort{tr.Actor: p})
	if e != nil || n.Actor != tr.Actor || p.sends != 1 {
		t.Fatal(n, e)
	}
	n, e = s.NotifyNext(context.Background(), map[string]ChatPort{tr.Actor: p})
	if e != nil || n.Sequence != 0 || p.sends != 1 {
		t.Fatal(n, e)
	}
	root := s.Root
	s.Close()
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.RecoverInterrupted(); e != nil {
		t.Fatal(e)
	}
	n, e = s.NotifyNext(context.Background(), map[string]ChatPort{tr.Actor: p})
	if e != nil || n.Sequence != 0 || p.sends != 1 {
		t.Fatal(n, e)
	}
}
func TestUnknownMessageDoesNotResendGiftOrAllowLaterSnapshotToOvertake(t *testing.T) {
	s := store(t)
	o, tr := confirmedFixture(t, s, "sender1")
	_, _ = confirmedFixture(t, s, "sender2")
	p := &chatFixture{actor: tr.Actor, buyer: o.Buyer, fail: true}
	_, e := s.NotifyNext(context.Background(), map[string]ChatPort{tr.Actor: p})
	if e == nil || p.sends != 1 {
		t.Fatal(e)
	}
	if e = s.RecoverInterrupted(); e != nil {
		t.Fatal(e)
	}
	p.fail = false
	_, e = s.NotifyNext(context.Background(), map[string]ChatPort{tr.Actor: p})
	if e == nil || p.sends != 1 {
		t.Fatal("ambiguous message resent", e)
	}
	got, _ := s.Order(o.ID)
	if got.Confirmed != 30000 || got.Held != 0 || got.Completed == 0 {
		t.Fatal("message failure modified gift", got)
	}
}
func TestChatWrongActorCannotSendAndInterruptedClaimBecomesReview(t *testing.T) {
	s := store(t)
	o, tr := confirmedFixture(t, s, "sender")
	p := &chatFixture{actor: tr.Actor, buyer: o.Buyer, wrong: true}
	if _, e := s.NotifyNext(context.Background(), map[string]ChatPort{tr.Actor: p}); e == nil || p.sends != 0 {
		t.Fatal(e)
	}
	q, _ := s.Outbox()
	if q[0][3] != "-2" {
		t.Fatal(q)
	}
	other := store(t)
	confirmedFixture(t, other, "sender")
	if _, e := other.claimNotification(); e != nil {
		t.Fatal(e)
	}
	if e := other.RecoverInterrupted(); e != nil {
		t.Fatal(e)
	}
	q, _ = other.Outbox()
	if q[0][3] != "-2" {
		t.Fatal(q)
	}
}

func TestMessageReconciliationRequiresExactVisibleHistoryAndNeverSends(t *testing.T) {
	s := store(t)
	o, tr := confirmedFixture(t, s, "sender")
	p := &chatFixture{actor: tr.Actor, buyer: o.Buyer, fail: true}
	n, err := s.NotifyNext(context.Background(), map[string]ChatPort{tr.Actor: p})
	if err == nil {
		t.Fatal("expected ambiguous message")
	}
	r := ChatReceipt{Actor: tr.Actor, Buyer: o.Buyer, Message: n.Message, VisibleInHistory: true, ScreenSHA: evidence("observed history"), ObservedAt: time.Now()}
	wrong := r
	wrong.Buyer = "wrong"
	if err = s.ReconcileNotification(n.Sequence, wrong); err == nil {
		t.Fatal("wrong buyer reconciled")
	}
	wrong = r
	wrong.Message += "different"
	if err = s.ReconcileNotification(n.Sequence, wrong); err == nil {
		t.Fatal("wrong message reconciled")
	}
	wrong = r
	wrong.ObservedAt = time.Now().Add(-time.Minute)
	if err = s.ReconcileNotification(n.Sequence, wrong); err == nil {
		t.Fatal("stale history reconciled")
	}
	if err = s.ReconcileNotification(n.Sequence, r); err != nil {
		t.Fatal(err)
	}
	if err = s.ReconcileNotification(n.Sequence, r); err != nil {
		t.Fatal("not idempotent", err)
	}
	n, err = s.NotifyNext(context.Background(), map[string]ChatPort{tr.Actor: p})
	if err != nil || n.Sequence != 0 || p.sends != 1 {
		t.Fatal("message resent during reconciliation", n, err)
	}
	pending, err := s.PendingNotifications()
	if err != nil || len(pending) != 0 {
		t.Fatal(pending, err)
	}
}
