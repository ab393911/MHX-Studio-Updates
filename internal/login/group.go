// Package login coordinates login preparation only. It never changes task,
// server, order or battle completion states.
package login

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type Spec struct {
	Server string `json:"server"`
	Role   int    `json:"role"`
}
type Proof struct {
	Identity              string    `json:"identity"`
	Server                string    `json:"server"`
	Role                  int       `json:"role"`
	EnteredGame           bool      `json:"entered_game"`
	IdentityVerified      bool      `json:"identity_verified"`
	ServerVerified        bool      `json:"server_verified"`
	ScreenSHA             string    `json:"screen_sha256"`
	ServerScreenSHA       string    `json:"server_screen_sha256"`
	ObservedAt            time.Time `json:"observed_at"`
	CharacterName         string    `json:"character_name,omitempty"`
	CharacterNameVerified bool      `json:"character_name_verified,omitempty"`
}
type Member struct {
	Instance             int
	Identity, Profession string
	CharacterSignature   string
	Preflight            func(context.Context) error // read-only, no launch or tap
	Enter                func(context.Context) (Proof, error)
	Recheck              func(context.Context) (Proof, error)
}
type Status struct {
	Instance int    `json:"instance"`
	Identity string `json:"identity"`
	State    string `json:"state"`
	Step     string `json:"step"`
	Error    string `json:"error,omitempty"`
	Proof    Proof  `json:"proof"`
}
type Report struct {
	Spec       Spec      `json:"target"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Ready      bool      `json:"ready"`
	Members    []Status  `json:"members"`
	Error      string    `json:"error,omitempty"`
}
type Options struct {
	Timeout     time.Duration
	ProofMaxAge time.Duration
	OnStatus    func(Status) // called serially by the coordinator, never by workers
}

func validHash(s string) bool { b, e := hex.DecodeString(s); return e == nil && len(b) == 32 }
func Validate(spec Spec, members []Member) error {
	if spec.Server == "" || len(spec.Server) > 128 || spec.Role < 1 || spec.Role > 5 || len(members) != 5 {
		return errors.New("须指定同一区服、1～5角色槽位和恰好五台实例")
	}
	ids := map[string]bool{}
	indexes := map[int]bool{}
	jobs := map[string]int{}
	signatures := map[string]bool{}
	for _, m := range members {
		if m.Instance < 0 || m.Identity == "" || ids[m.Identity] || indexes[m.Instance] || m.Preflight == nil || m.Enter == nil || m.Recheck == nil {
			return errors.New("实例身份重复、缺失或登录接口不完整")
		}
		if !validHash(m.CharacterSignature) || signatures[m.CharacterSignature] {
			return errors.New("五名角色缺少独立身份标定，不能复制通用主界面作为五个账号身份")
		}
		signatures[m.CharacterSignature] = true
		ids[m.Identity] = true
		indexes[m.Instance] = true
		jobs[m.Profession]++
	}
	if jobs["合欢"] != 1 || jobs["天音"] != 1 || jobs["青云"] != 1 || jobs["鬼王"] != 2 {
		return errors.New("本组职业须为合欢、天音、青云、鬼王、鬼王")
	}
	return nil
}
func verifyProof(p Proof, m Member, s Spec, now time.Time, maxAge time.Duration) error {
	if p.Identity != m.Identity || p.Server != s.Server || p.Role != s.Role {
		return errors.New("登录证据与目标实例、区服或角色不符")
	}
	if !p.EnteredGame || !p.IdentityVerified {
		return errors.New("目标角色身份未核验")
	}
	if !p.ServerVerified || !validHash(p.ServerScreenSHA) {
		return errors.New("本次登录区服依据不足，不能报告五台同服就位")
	}
	if !validHash(p.ScreenSHA) || p.ObservedAt.IsZero() || p.ObservedAt.After(now.Add(time.Second)) || now.Sub(p.ObservedAt) > maxAge {
		return errors.New("最终身份画面缺失或已过期，须重新核验")
	}
	return nil
}
func Run(ctx context.Context, spec Spec, members []Member, opt Options) (report Report, err error) {
	report.Spec = spec
	report.StartedAt = time.Now().UTC()
	defer func() {
		report.FinishedAt = time.Now().UTC()
		if err != nil {
			report.Error = err.Error()
		}
	}()
	if err = Validate(spec, members); err != nil {
		return
	}
	if opt.Timeout == 0 {
		opt.Timeout = 3 * time.Minute
	}
	if opt.ProofMaxAge == 0 {
		opt.ProofMaxAge = 5 * time.Second
	}
	if opt.Timeout < 0 || opt.Timeout > 5*time.Minute || opt.ProofMaxAge <= 0 || opt.ProofMaxAge > 15*time.Second {
		err = errors.New("登录时限或证据有效期无效")
		return
	}
	work, cancel := context.WithTimeout(ctx, opt.Timeout)
	defer cancel()
	report.Members = make([]Status, len(members))
	publish := func(i int, state, step string, p Proof, e error) {
		st := Status{Instance: members[i].Instance, Identity: members[i].Identity, State: state, Step: step, Proof: p}
		if e != nil {
			st.Error = e.Error()
		}
		report.Members[i] = st
		if opt.OnStatus != nil {
			opt.OnStatus(st)
		}
	}
	for i := range members {
		publish(i, "PENDING", "等待全组只读预检", Proof{}, nil)
	}
	type answer struct {
		i int
		p Proof
		e error
	}
	phase := func(name string, call func(context.Context, Member) (Proof, error), check bool) error {
		if e := work.Err(); e != nil {
			return e
		}
		ch := make(chan answer, len(members))
		for i, m := range members {
			publish(i, "RUNNING", name, report.Members[i].Proof, nil)
			go func(i int, m Member) {
				a := answer{i: i}
				defer func() {
					if v := recover(); v != nil {
						a.e = fmt.Errorf("登录接口异常：%v", v)
					}
					ch <- a
				}()
				if e := work.Err(); e != nil {
					a.e = e
					return
				}
				a.p, a.e = call(work, m)
				if a.e == nil && check {
					a.e = verifyProof(a.p, m, spec, time.Now(), opt.ProofMaxAge)
				}
			}(i, m)
		}
		var cause error
		for range members {
			a := <-ch
			if a.e != nil {
				state := "FAILED"
				if a.p.EnteredGame && (!a.p.IdentityVerified || !a.p.ServerVerified) {
					state = "NEEDS_REVIEW"
				}
				if errors.Is(a.e, context.Canceled) || (errors.Is(a.e, context.DeadlineExceeded) && work.Err() != nil) {
					state = "STOPPED"
				}
				publish(a.i, state, name, a.p, a.e)
				if cause == nil {
					cause = fmt.Errorf("实例%d：%w", members[a.i].Instance, a.e)
					cancel()
				}
			} else {
				publish(a.i, "VERIFIED", name, a.p, nil)
			}
		}
		return cause
	}
	if err = phase("只读预检（尚未启动游戏或点击）", func(c context.Context, m Member) (Proof, error) { return Proof{}, m.Preflight(c) }, false); err != nil {
		return
	}
	if err = phase("启动并登录指定角色", func(c context.Context, m Member) (Proof, error) { return m.Enter(c) }, true); err != nil {
		return
	}
	if err = phase("全组结束后的最终截图复核", func(c context.Context, m Member) (Proof, error) { return m.Recheck(c) }, true); err != nil {
		return
	}
	for i, m := range members {
		if err = verifyProof(report.Members[i].Proof, m, spec, time.Now(), opt.ProofMaxAge); err != nil {
			publish(i, "FAILED", "最终证据已过期", report.Members[i].Proof, err)
			return
		}
	}
	if err = work.Err(); err != nil {
		return
	}
	report.Ready = true
	return
}
