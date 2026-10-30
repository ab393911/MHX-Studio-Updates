package studio

// Delivery executes one allocated gift, rather than treating a reservation as
// a game action. Ports must read the actual game UI; fixtures implement the same
// contract in tests. No production port is enabled until its UI readers exist.
import (
	"context"
	"errors"
	"fmt"
	"time"
)

type BuyerProof struct {
	Buyer, Actor, ScreenSHA string
	FriendAccepted          bool
	ObservedAt              time.Time
}

type BalanceProof struct {
	Actor, ScreenSHA string
	Silver, Merit    int64
	ObservedAt       time.Time
}

type GiftDraft struct {
	Buyer, Actor, ScreenSHA string
	Amount                  int64
	SilverCurrency          bool
	ObservedAt              time.Time
}

type GiftResult struct {
	Buyer, Actor, Reference, ResultSHA string
	Amount, AfterSilver, AfterMerit    int64
	SuccessVerified                    bool
	Frame                              []byte
	ObservedAt                         time.Time
}

type DeliveryPort interface {
	// FindOrAddFriend waits for actual acceptance. An invitation click is not acceptance.
	FindOrAddFriend(context.Context, string) (BuyerProof, error)
	ReadBalance(context.Context) (BalanceProof, error)
	PrepareGift(context.Context, string, int64) (GiftDraft, error)
	// SubmitGift must perform at most one irreversible submission; never retry it internally.
	SubmitGift(context.Context, GiftDraft) (GiftResult, error)
}

type DeliveryRequest struct {
	Order, Actor, RequestID string
	Maximum                 int64
}

func freshGameProof(at time.Time, sha string) bool {
	age := time.Since(at)
	return hashOK(sha) && !at.IsZero() && age >= -time.Second && age <= 15*time.Second
}

// DeliverOne does not retry an irreversible submission. Even cancellation just
// after durable dispatch holds the allocation for reconciliation. Reservation
// keys remain idempotent across a worker restart and cannot trigger another gift.
func (s *Store) DeliverOne(ctx context.Context, p DeliveryPort, req DeliveryRequest) (tr Transfer, err error) {
	if p == nil || !validText(req.Actor) || !validText(req.RequestID) || req.Maximum <= 0 || req.Maximum > MaxMoney || req.Maximum%10000 != 0 {
		return tr, errors.New("invalid delivery request/port")
	}
	if err = ctx.Err(); err != nil {
		return tr, err
	}
	o, err := s.Order(req.Order)
	if err != nil {
		return tr, err
	}
	if o.Completed != 0 || o.Remaining-o.Held < 10000 {
		return tr, errors.New("订单已完成或额度锁定，不再发货")
	}
	// A resumed request must be inspected before UI operations or budget refresh.
	rows, err := s.read("SELECT "+txColumns+" FROM transfers WHERE request=?", req.RequestID)
	if err != nil {
		return tr, err
	}
	if len(rows) != 0 {
		tr = transferRow(rows[0])
		if tr.Actor != req.Actor || tr.Order != req.Order || tr.Requested != req.Maximum {
			return tr, errors.New("idempotency key conflicts with another request")
		}
		return tr, errors.New("该请求已有持久流水，不重复执行；请核对原交易")
	}
	// Reserve is intentionally idempotent for callers that inspect a ledger. A
	// game worker needs exclusive ownership in addition, or two processes could
	// both receive the same RESERVED row and submit it twice.
	err = s.tx(func(d *nativeDB) error {
		rows, _, e := d.query("SELECT COUNT(*) FROM events WHERE kind='delivery_worker' AND object=?", req.RequestID)
		if e != nil {
			return e
		}
		if rows[0][0] != "0" {
			return errors.New("该发货请求已有工作进程接管，不重复执行")
		}
		_, _, e = d.query("INSERT INTO events(kind,object,detail,created) VALUES('delivery_worker',?,?,?)", req.RequestID, req.Order+"|"+req.Actor, num(time.Now().Unix()))
		return e
	})
	if err != nil {
		return tr, err
	}
	buyer, err := p.FindOrAddFriend(ctx, o.Buyer)
	if err != nil {
		return tr, err
	}
	if !buyer.FriendAccepted || buyer.Buyer != o.Buyer || buyer.Actor != req.Actor || !freshGameProof(buyer.ObservedAt, buyer.ScreenSHA) {
		return tr, errors.New("买家ID、当前角色或好友通过状态未核验")
	}
	if !o.DeliveryEnabled {
		if err = s.AuthorizeDelivery(o.ID, o.Buyer, buyer.ScreenSHA); err != nil {
			return tr, err
		}
	}
	balance, err := p.ReadBalance(ctx)
	if err != nil {
		return tr, err
	}
	if balance.Actor != req.Actor || !freshGameProof(balance.ObservedAt, balance.ScreenSHA) {
		return tr, errors.New("当前角色余额/功绩依据不新鲜或身份不符")
	}
	if err = s.SetBudget(req.Actor, balance.Silver, balance.Merit, balance.ScreenSHA, balance.ObservedAt); err != nil {
		return tr, err
	}
	tr, err = s.Reserve(o.ID, req.Actor, req.RequestID, req.Maximum)
	if err != nil {
		return tr, err
	}
	if tr.State != "RESERVED" {
		return tr, errors.New("原请求已执行或待核查，不重新提交")
	}
	// Only untouched reservations can be released. Once dispatched, all failures
	// including screenshot/database failures preserve the occupied allocation.
	dispatched, confirmed := false, false
	defer func() {
		if confirmed {
			return
		}
		if dispatched {
			err = errors.Join(err, s.MarkUncertain(tr.ID))
			tr.State = "NEEDS_REVIEW"
		} else {
			err = errors.Join(err, s.CancelReservation(tr.ID))
			tr.State = "CANCELLED"
		}
	}()
	draft, err := p.PrepareGift(ctx, o.Buyer, tr.Amount)
	if err != nil {
		return tr, err
	}
	if draft.Buyer != o.Buyer || draft.Actor != req.Actor || draft.Amount != tr.Amount || !draft.SilverCurrency || !freshGameProof(draft.ObservedAt, draft.ScreenSHA) {
		return tr, errors.New("赠送确认页买家、金额、银锭币种或角色未核验，未发送")
	}
	if err = ctx.Err(); err != nil {
		return tr, err
	}
	if err = s.MarkDispatched(tr.ID, o.Buyer, draft.ScreenSHA); err != nil {
		return tr, err
	}
	dispatched = true
	if err = ctx.Err(); err != nil {
		return tr, err
	}
	result, err := p.SubmitGift(ctx, draft)
	if err != nil {
		return tr, err
	}
	if !result.SuccessVerified || result.Buyer != o.Buyer || result.Actor != req.Actor || result.Amount != tr.Amount || !freshGameProof(result.ObservedAt, result.ResultSHA) || !validText(result.Reference) {
		return tr, errors.New("赠送结果不明：保持配额锁定，不自动重发")
	}
	path, sha, err := s.SaveProof(o.Buyer, o.ID, tr.ID, result.Frame, result.ObservedAt)
	if err != nil {
		return tr, fmt.Errorf("成功画面凭证未保存，需人工核对：%w", err)
	}
	if err = s.Confirm(tr.ID, Receipt{Buyer: result.Buyer, Reference: result.Reference, ProofPath: path, ProofSHA: sha, ResultEvidence: result.ResultSHA, Amount: result.Amount, AfterSilver: result.AfterSilver, AfterMerit: result.AfterMerit}); err != nil {
		return tr, err
	}
	confirmed = true
	tr.State, tr.Proof, tr.SHA, tr.Receipt = "CONFIRMED", path, sha, result.Reference
	return tr, nil
}

// AssetStats is read-only. Per-character rounding happens before aggregation.
type AssetStats struct {
	Actor                         string
	Silver, Merit, EffectiveMerit int64
	Capacity, Confirmed, Held     int64
	Evidence                      string
	ObservedAt                    time.Time
}

func (s *Store) Assets() ([]AssetStats, error) {
	rows, err := s.read("SELECT b.actor,b.silver,b.merit,b.capacity,b.evidence,b.observed,COALESCE(SUM(CASE WHEN t.state='CONFIRMED' THEN t.amount ELSE 0 END),0),COALESCE(SUM(CASE WHEN t.state IN ('RESERVED','DISPATCHED','NEEDS_REVIEW') THEN t.amount ELSE 0 END),0) FROM budgets b LEFT JOIN transfers t ON t.actor=b.actor AND t.epoch=b.epoch GROUP BY b.actor ORDER BY b.actor")
	if err != nil {
		return nil, err
	}
	var out []AssetStats
	for _, row := range rows {
		m := integer(row[2])
		out = append(out, AssetStats{Actor: row[0], Silver: integer(row[1]), Merit: m, EffectiveMerit: m / 100 * 100, Capacity: integer(row[3]), Evidence: row[4], ObservedAt: time.Unix(0, integer(row[5])), Confirmed: integer(row[6]), Held: integer(row[7])})
	}
	return out, nil
}
