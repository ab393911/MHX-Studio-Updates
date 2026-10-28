package studio

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Order struct {
	DeliveryEnabled                              bool
	ID, Buyer                                    string
	Total, Confirmed, Held, Remaining, Completed int64
}
type Transfer struct {
	Requested                        int64
	ID, Request, Order, Actor, Epoch string
	Amount                           int64
	State, Receipt, Proof, SHA       string
}

func orderAt(d *nativeDB, id string) (Order, error) {
	r, _, e := d.query("SELECT id,buyer,total,completed,enabled FROM orders WHERE id=?", id)
	if e != nil {
		return Order{}, e
	}
	if len(r) != 1 {
		return Order{}, errors.New("order not found")
	}
	o := Order{ID: r[0][0], Buyer: r[0][1], Total: integer(r[0][2]), Completed: integer(r[0][3]), DeliveryEnabled: r[0][4] == "1"}
	r, _, e = d.query("SELECT COALESCE(SUM(CASE WHEN state='CONFIRMED' THEN amount ELSE 0 END),0),COALESCE(SUM(CASE WHEN state IN ('RESERVED','DISPATCHED','NEEDS_REVIEW') THEN amount ELSE 0 END),0) FROM transfers WHERE order_id=?", id)
	if e != nil {
		return o, e
	}
	o.Confirmed = integer(r[0][0])
	o.Held = integer(r[0][1])
	o.Remaining = o.Total - o.Confirmed
	if o.Held > o.Remaining || o.Remaining < 0 {
		return o, errors.New("order invariant failed")
	}
	return o, nil
}
func (s *Store) CreateOrder(buyer string, total int64) (Order, error) {
	var o Order
	if !validText(buyer) || total <= 0 || total > MaxMoney || total%10000 != 0 {
		return o, errors.New("买家ID不能为空，银锭订单须为正整数万")
	}
	id := ID()
	e := s.tx(func(d *nativeDB) error {
		_, _, e := d.query("INSERT INTO orders(id,buyer,total,created) VALUES(?,?,?,?)", id, buyer, num(total), num(time.Now().Unix()))
		if e != nil {
			return e
		}
		o, e = orderAt(d, id)
		return e
	})
	return o, e
}
func (s *Store) Order(id string) (Order, error) {
	var o Order
	e := s.tx(func(d *nativeDB) error { var e error; o, e = orderAt(d, id); return e })
	return o, e
}
func (s *Store) Orders() ([]Order, error) {
	r, e := s.read("SELECT id FROM orders ORDER BY created DESC,rowid DESC LIMIT 100")
	if e != nil {
		return nil, e
	}
	var out []Order
	for _, row := range r {
		o, e := s.Order(row[0])
		if e != nil {
			return nil, e
		}
		out = append(out, o)
	}
	return out, nil
}
func (s *Store) SetBudget(actor string, silver, merit int64, evidence string, observed time.Time) error {
	cap, e := Capacity(silver, merit)
	if e != nil {
		return e
	}
	if !validText(actor) || !hashOK(evidence) || observed.IsZero() || time.Since(observed) > 15*time.Minute || time.Until(observed) > time.Minute {
		return errors.New("额度须来自15分钟内实际核验的余额/功绩画面")
	}
	return s.tx(func(d *nativeDB) error {
		r, _, e := d.query("SELECT COUNT(*) FROM transfers WHERE actor=? AND state IN ('RESERVED','DISPATCHED','NEEDS_REVIEW')", actor)
		if e != nil {
			return e
		}
		if r[0][0] != "0" {
			return errors.New("角色存在锁定/不明交易，先对账，不覆盖额度")
		}
		r, _, e = d.query("SELECT evidence,observed FROM budgets WHERE actor=?", actor)
		if e != nil {
			return e
		}
		if len(r) > 0 && (r[0][0] == evidence || integer(r[0][1]) >= observed.UnixNano()) {
			return errors.New("旧余额证据不能重复刷新额度")
		}
		if len(r) > 0 {
			_, _, e = d.query("UPDATE budgets SET silver=?,merit=?,capacity=?,epoch=?,evidence=?,observed=? WHERE actor=?", num(silver), num(merit), num(cap), ID(), evidence, num(observed.UnixNano()), actor)
		} else {
			_, _, e = d.query("INSERT INTO budgets(actor,silver,merit,capacity,epoch,evidence,observed) VALUES(?,?,?,?,?,?,?)", actor, num(silver), num(merit), num(cap), ID(), evidence, num(observed.UnixNano()))
		}
		return e
	})
}
func transferRow(row []string) Transfer {
	return Transfer{ID: row[0], Request: row[1], Order: row[2], Actor: row[3], Epoch: row[4], Amount: integer(row[5]), State: row[6], Receipt: row[7], Proof: row[8], SHA: row[9], Requested: integer(row[10])}
}

const txColumns = "id,request,order_id,actor,epoch,amount,state,receipt,proof,proof_sha,requested"

func transferAt(d *nativeDB, id string) (Transfer, error) {
	r, _, e := d.query("SELECT "+txColumns+" FROM transfers WHERE id=?", id)
	if e != nil {
		return Transfer{}, e
	}
	if len(r) != 1 {
		return Transfer{}, errors.New("transfer not found")
	}
	return transferRow(r[0]), nil
}
func (s *Store) Reserve(order, actor, request string, want int64) (Transfer, error) {
	var t Transfer
	if !validText(request) || want <= 0 || want > MaxMoney || want%10000 != 0 {
		return t, errors.New("invalid reservation")
	}
	e := s.tx(func(d *nativeDB) error {
		r, _, e := d.query("SELECT "+txColumns+" FROM transfers WHERE request=?", request)
		if e != nil {
			return e
		}
		if len(r) > 0 {
			t = transferRow(r[0])
			if t.Order != order || t.Actor != actor || t.Requested != want {
				return errors.New("idempotency key conflicts with another request")
			}
			return nil
		}
		o, e := orderAt(d, order)
		if e != nil {
			return e
		}
		if !o.DeliveryEnabled {
			return errors.New("订单仅记账，未通过实际发货授权与身份验收")
		}
		if o.Completed > 0 || o.Remaining-o.Held < 10000 {
			return errors.New("订单已完成或额度全部锁定，停止发货")
		}
		r, _, e = d.query("SELECT epoch,capacity,observed FROM budgets WHERE actor=?", actor)
		if e != nil {
			return e
		}
		if len(r) != 1 || time.Now().UnixNano()-integer(r[0][2]) > int64(15*time.Minute) {
			return errors.New("角色没有新鲜已核验额度")
		}
		epoch, cap := r[0][0], integer(r[0][1])
		r, _, e = d.query("SELECT COUNT(*) FROM transfers WHERE actor=? AND state IN ('RESERVED','DISPATCHED','NEEDS_REVIEW')", actor)
		if e != nil {
			return e
		}
		if r[0][0] != "0" {
			return errors.New("角色存在未结清交易，不自动重发")
		}
		r, _, e = d.query("SELECT COALESCE(SUM(amount),0) FROM transfers WHERE actor=? AND epoch=? AND state<>'CANCELLED'", actor, epoch)
		if e != nil {
			return e
		}
		available := cap - integer(r[0][0])
		n := want
		if v := o.Remaining - o.Held; n > v {
			n = v
		}
		if n > available {
			n = available
		}
		if n < 10000 {
			return errors.New("角色或订单可分配额度不足")
		}
		t = Transfer{ID: ID(), Request: request, Order: order, Actor: actor, Epoch: epoch, Amount: n, Requested: want, State: "RESERVED"}
		_, _, e = d.query("INSERT INTO transfers(id,request,order_id,actor,epoch,amount,requested,state,updated) VALUES(?,?,?,?,?,?,?,?,?)", t.ID, request, order, actor, epoch, num(n), num(want), t.State, num(time.Now().Unix()))
		return e
	})
	return t, e
}
func (s *Store) MarkDispatched(id, buyer, identityEvidence string) error {
	if !hashOK(identityEvidence) {
		return errors.New("missing independently verified buyer identity")
	}
	return s.tx(func(d *nativeDB) error {
		t, e := transferAt(d, id)
		if e != nil {
			return e
		}
		o, e := orderAt(d, t.Order)
		if e != nil {
			return e
		}
		if !o.DeliveryEnabled || o.Completed > 0 || o.Buyer != buyer || t.State != "RESERVED" {
			return errors.New("买家不符或交易不处于可发送状态；未重发")
		}
		_, _, e = d.query("UPDATE transfers SET state='DISPATCHED',updated=? WHERE id=?", num(time.Now().Unix()), id)
		if e != nil {
			return e
		}
		_, _, e = d.query("INSERT INTO events(kind,object,detail,created) VALUES('dispatch',?,?,?)", id, identityEvidence, num(time.Now().Unix()))
		return e
	})
}
func (s *Store) MarkUncertain(id string) error {
	return s.tx(func(d *nativeDB) error {
		_, n, e := d.query("UPDATE transfers SET state='NEEDS_REVIEW',updated=? WHERE id=? AND state='DISPATCHED'", num(time.Now().Unix()), id)
		if e != nil {
			return e
		}
		if n != 1 {
			return errors.New("only a dispatched transfer can become uncertain")
		}
		return nil
	})
}
func (s *Store) CancelReservation(id string) error {
	return s.tx(func(d *nativeDB) error {
		_, n, e := d.query("UPDATE transfers SET state='CANCELLED',updated=? WHERE id=? AND state='RESERVED'", num(time.Now().Unix()), id)
		if e != nil {
			return e
		}
		if n != 1 {
			return errors.New("已发起或不明交易不能自动释放额度")
		}
		return nil
	})
}

type Receipt struct {
	Buyer, Reference, ProofPath, ProofSHA, ResultEvidence string
	Amount, AfterSilver, AfterMerit                       int64
}

func (s *Store) Confirm(id string, r Receipt) (err error) {
	defer func() {
		if err != nil {
			_ = s.MarkUncertain(id)
		}
	}()
	if !validText(r.Reference) || !hashOK(r.ProofSHA) || !hashOK(r.ResultEvidence) {
		return errors.New("确认需独立成功结果与本地截图，截图本身不算成功")
	}
	return s.tx(func(d *nativeDB) error {
		t, e := transferAt(d, id)
		if e != nil {
			return e
		}
		o, e := orderAt(d, t.Order)
		if e != nil {
			return e
		}
		if r.Buyer != o.Buyer || r.Amount != t.Amount {
			return errors.New("receipt buyer/amount mismatch")
		}
		if t.State == "CONFIRMED" {
			if t.Receipt == r.Reference && t.SHA == r.ProofSHA && t.Proof == r.ProofPath {
				return nil
			}
			return errors.New("conflicting duplicate receipt")
		}
		if t.State != "DISPATCHED" && t.State != "NEEDS_REVIEW" {
			return errors.New("transfer was not dispatched")
		}
		if e = s.verifyProof(o.Buyer, t.Order, t.ID, r.ProofPath, r.ProofSHA); e != nil {
			return e
		}
		rows, _, e := d.query("SELECT silver,merit FROM budgets WHERE actor=? AND epoch=?", t.Actor, t.Epoch)
		if e != nil {
			return e
		}
		if len(rows) != 1 {
			return errors.New("budget epoch changed unexpectedly")
		}
		prev, _, e := d.query("SELECT COALESCE(SUM(amount),0) FROM transfers WHERE actor=? AND epoch=? AND state='CONFIRMED'", t.Actor, t.Epoch)
		if e != nil {
			return e
		}
		used := integer(prev[0][0]) + t.Amount
		if r.AfterSilver < 0 || r.AfterMerit < 0 || r.AfterSilver != integer(rows[0][0])-used || r.AfterMerit != integer(rows[0][1])-(used/10000)*100 {
			return errors.New("发后余额/功绩变化无法对账，保持额度锁定")
		}
		existing, _, e := d.query("SELECT COUNT(*) FROM transfers WHERE receipt=? AND id<>?", r.Reference, id)
		if e != nil {
			return e
		}
		if existing[0][0] != "0" {
			return errors.New("receipt already used")
		}
		if _, _, e = d.query("UPDATE transfers SET state='CONFIRMED',receipt=?,proof=?,proof_sha=?,updated=? WHERE id=?", r.Reference, r.ProofPath, r.ProofSHA, num(time.Now().Unix()), id); e != nil {
			return e
		}
		o, e = orderAt(d, o.ID)
		if e != nil {
			return e
		}
		if o.Confirmed == o.Total {
			if _, _, e = d.query("UPDATE orders SET completed=? WHERE id=? AND completed=0", num(time.Now().Unix()), o.ID); e != nil {
				return e
			}
		}
		actorLabel := t.Actor
		var parts []json.RawMessage
		if json.Unmarshal([]byte(t.Actor), &parts) == nil && len(parts) == 3 {
			var server string
			var slot int
			if json.Unmarshal(parts[1], &server) == nil && json.Unmarshal(parts[2], &slot) == nil {
				actorLabel = fmt.Sprintf("%s第%d个", server, slot)
			}
		}
		message := fmt.Sprintf("角色%s，本次交付%d；订单总额%d；累计确认%d；剩余%d", actorLabel, t.Amount, o.Total, o.Confirmed, o.Remaining)
		if _, _, e = d.query("INSERT INTO outbox(transfer_id,message) VALUES(?,?)", id, message); e != nil {
			return e
		}
		_, _, e = d.query("INSERT INTO events(kind,object,detail,created) VALUES('confirm',?,?,?)", id, r.ResultEvidence, num(time.Now().Unix()))
		return e
	})
}
func (s *Store) Outbox() ([][]string, error) {
	return s.read("SELECT seq,transfer_id,message,delivered FROM outbox ORDER BY seq")
}

// Authorization is not exposed by the current GUI. A future verified delivery
// controller must obtain explicit authorization and actual buyer identity first.
func (s *Store) AuthorizeDelivery(id, buyer, identityEvidence string) error {
	if !hashOK(identityEvidence) {
		return errors.New("buyer identity evidence required")
	}
	return s.tx(func(d *nativeDB) error {
		o, e := orderAt(d, id)
		if e != nil {
			return e
		}
		if o.Buyer != buyer || o.Completed != 0 || o.Confirmed != 0 || o.Held != 0 {
			return errors.New("order authorization mismatch")
		}
		if _, _, e = d.query("UPDATE orders SET enabled=1 WHERE id=?", id); e != nil {
			return e
		}
		_, _, e = d.query("INSERT INTO events(kind,object,detail,created) VALUES('delivery_authorization',?,?,?)", id, identityEvidence, num(time.Now().Unix()))
		return e
	})
}
