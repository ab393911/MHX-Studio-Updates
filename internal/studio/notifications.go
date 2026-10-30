package studio

import (
	"context"
	"errors"
	"time"
)

// The legacy INTEGER column remains backward readable: 0=pending, 1=verified,
// -1=claimed/in flight, -2=ambiguous. No schema or financial row is rewritten.
type Notification struct {
	Sequence                        int64
	Transfer, Buyer, Actor, Message string
}
type NotificationStatus struct {
	Notification
	State string
}

func (s *Store) PendingNotifications() ([]NotificationStatus, error) {
	rows, err := s.read("SELECT o.seq,o.transfer_id,o.message,o.delivered,t.actor,r.buyer FROM outbox o JOIN transfers t ON t.id=o.transfer_id JOIN orders r ON r.id=t.order_id WHERE o.delivered<>1 ORDER BY o.seq")
	if err != nil {
		return nil, err
	}
	var out []NotificationStatus
	for _, r := range rows {
		state := map[string]string{"0": "待发送", "-1": "发送中", "-2": "待核查"}[r[3]]
		if state == "" {
			return nil, errors.New("未知买家消息状态，先核查")
		}
		out = append(out, NotificationStatus{Notification: Notification{Sequence: integer(r[0]), Transfer: r[1], Message: r[2], Actor: r[4], Buyer: r[5]}, State: state})
	}
	return out, nil
}

type ChatReceipt struct {
	Buyer, Actor, Message, ScreenSHA string
	VisibleInHistory                 bool
	ObservedAt                       time.Time
}
type ChatPort interface {
	VerifyChat(context.Context, string, string) (BuyerProof, error)
	// SendChat submits once and verifies the exact outgoing message in history.
	SendChat(context.Context, string, string) (ChatReceipt, error)
}

// claimNotification serializes messages by confirmed-ledger snapshot sequence.
// An unresolved earlier message blocks newer messages, including on another
// emulator, so old totals cannot appear after newer totals in the buyer chat.
func (s *Store) claimNotification() (n Notification, err error) {
	err = s.tx(func(d *nativeDB) error {
		rows, _, e := d.query("SELECT o.seq,o.transfer_id,o.message,o.delivered,t.actor,t.state,r.buyer FROM outbox o JOIN transfers t ON t.id=o.transfer_id JOIN orders r ON r.id=t.order_id WHERE o.delivered<>1 ORDER BY o.seq LIMIT 1")
		if e != nil {
			return e
		}
		if len(rows) == 0 {
			return nil
		}
		r := rows[0]
		if r[3] != "0" || r[5] != "CONFIRMED" {
			return errors.New("前一条买家消息正在发送或待核查，不重复发送、不越序")
		}
		n = Notification{Sequence: integer(r[0]), Transfer: r[1], Message: r[2], Actor: r[4], Buyer: r[6]}
		_, count, e := d.query("UPDATE outbox SET delivered=-1 WHERE seq=? AND delivered=0", r[0])
		if e == nil && count != 1 {
			return errors.New("买家消息已由其他进程接管")
		}
		return e
	})
	return
}

// NotifyNext uses the actual sending character's port, never the leader or a
// convenient currently active account. Messaging failure never rolls back or
// resubmits a confirmed gift. At-most-once submission is deliberate: uncertain
// message delivery needs reconciliation rather than an automatic duplicate.
func (s *Store) NotifyNext(ctx context.Context, ports map[string]ChatPort) (n Notification, err error) {
	if err = ctx.Err(); err != nil {
		return n, err
	}
	n, err = s.claimNotification()
	if err != nil || n.Sequence == 0 {
		return n, err
	}
	complete := false
	defer func() {
		if !complete {
			err = errors.Join(err, s.tx(func(d *nativeDB) error {
				_, _, e := d.query("UPDATE outbox SET delivered=-2 WHERE seq=? AND delivered=-1", num(n.Sequence))
				return e
			}))
		}
	}()
	p := ports[n.Actor]
	if p == nil {
		return n, errors.New("实际发货角色不在线，消息保留待核对，不切换其他角色发送")
	}
	identity, err := p.VerifyChat(ctx, n.Buyer, n.Actor)
	if err != nil {
		return n, err
	}
	if identity.Actor != n.Actor || identity.Buyer != n.Buyer || !identity.FriendAccepted || !freshGameProof(identity.ObservedAt, identity.ScreenSHA) {
		return n, errors.New("游戏对话框买家ID或当前发货角色身份未核验")
	}
	if err = ctx.Err(); err != nil {
		return n, err
	}
	r, err := p.SendChat(ctx, n.Buyer, n.Message)
	if err != nil {
		return n, err
	}
	if r.Buyer != n.Buyer || r.Actor != n.Actor || r.Message != n.Message || !r.VisibleInHistory || !freshGameProof(r.ObservedAt, r.ScreenSHA) {
		return n, errors.New("消息未在当前角色与买家对话记录中核验，不重发")
	}
	err = s.tx(func(d *nativeDB) error {
		_, count, e := d.query("UPDATE outbox SET delivered=1 WHERE seq=? AND delivered=-1", num(n.Sequence))
		if e != nil {
			return e
		}
		if count != 1 {
			return errors.New("消息状态变化，保持人工核查")
		}
		_, _, e = d.query("INSERT INTO events(kind,object,detail,created) VALUES('buyer_message',?,?,?)", n.Transfer, r.ScreenSHA, num(time.Now().Unix()))
		return e
	})
	complete = err == nil
	return n, err
}

// ReconcileNotification only acknowledges an exact message already visible in
// the correct chat. It never sends again or releases a gift allocation.
func (s *Store) ReconcileNotification(seq int64, r ChatReceipt) error {
	if seq <= 0 || !r.VisibleInHistory || !freshGameProof(r.ObservedAt, r.ScreenSHA) {
		return errors.New("核查需要当前对话中的完整消息及新鲜截图证据")
	}
	return s.tx(func(d *nativeDB) error {
		rows, _, e := d.query("SELECT o.message,o.delivered,t.actor,r.buyer,t.state FROM outbox o JOIN transfers t ON t.id=o.transfer_id JOIN orders r ON r.id=t.order_id WHERE o.seq=?", num(seq))
		if e != nil {
			return e
		}
		if len(rows) != 1 || rows[0][0] != r.Message || rows[0][2] != r.Actor || rows[0][3] != r.Buyer || rows[0][4] != "CONFIRMED" {
			return errors.New("消息内容、发货角色、买家或已确认交易不符")
		}
		if rows[0][1] == "1" {
			return nil
		}
		if rows[0][1] != "-2" {
			return errors.New("消息不处于待核查状态，不能抢占正在发送的工作进程")
		}
		if _, _, e = d.query("UPDATE outbox SET delivered=1 WHERE seq=? AND delivered=-2", num(seq)); e != nil {
			return e
		}
		_, _, e = d.query("INSERT INTO events(kind,object,detail,created) VALUES('buyer_message_reconciled',?,?,?)", num(seq), r.ScreenSHA, num(time.Now().Unix()))
		return e
	})
}
