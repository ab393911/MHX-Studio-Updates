package login

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func hash(v string) string { b := sha256.Sum256([]byte(v)); return hex.EncodeToString(b[:]) }
func fixture() (Spec, []Member) {
	spec := Spec{"一梦诛仙", 3}
	var members []Member
	for i, index := range []int{2, 6, 9, 17, 24} {
		identity := fmt.Sprintf("actor-%d", index)
		result := func(context.Context) (Proof, error) {
			return Proof{Identity: identity, Server: spec.Server, Role: spec.Role, EnteredGame: true, IdentityVerified: true, ServerVerified: true, ScreenSHA: hash(identity), ServerScreenSHA: hash("server"), ObservedAt: time.Now().UTC()}, nil
		}
		members = append(members, Member{Instance: index, Identity: identity, Profession: []string{"合欢", "天音", "青云", "鬼王", "鬼王"}[i], CharacterSignature: hash("nickname-" + identity), Preflight: func(context.Context) error { return nil }, Enter: result, Recheck: result})
	}
	return spec, members
}
func TestAllFivePreflightBeforeAnyConcurrentLoginThenFreshRecheck(t *testing.T) {
	spec, m := fixture()
	var preflight, entered, finished, rechecked atomic.Int32
	allEntered := make(chan struct{})
	var once sync.Once
	for i := range m {
		base := m[i].Enter
		m[i].Preflight = func(context.Context) error { preflight.Add(1); return nil }
		m[i].Enter = func(ctx context.Context) (Proof, error) {
			if preflight.Load() != 5 {
				return Proof{}, errors.New("launch before whole-group preflight")
			}
			if entered.Add(1) == 5 {
				once.Do(func() { close(allEntered) })
			}
			select {
			case <-ctx.Done():
				return Proof{}, ctx.Err()
			case <-allEntered:
			}
			finished.Add(1)
			return base(ctx)
		}
		m[i].Recheck = func(ctx context.Context) (Proof, error) {
			if finished.Load() != 5 {
				return Proof{}, errors.New("recheck before all logins finished")
			}
			rechecked.Add(1)
			return base(ctx)
		}
	}
	report, e := Run(context.Background(), spec, m, Options{Timeout: time.Second})
	if e != nil || !report.Ready || rechecked.Load() != 5 || len(report.Members) != 5 {
		t.Fatal(report, e)
	}
	for i, status := range report.Members {
		if status.Instance != m[i].Instance || status.Proof.Role != 3 || status.State != "VERIFIED" {
			t.Fatal(status)
		}
	}
}
func TestOnePreflightFailureProducesNoLaunchOrTap(t *testing.T) {
	spec, m := fixture()
	var launches atomic.Int32
	m[2].Preflight = func(context.Context) error { return errors.New("ADB identity changed") }
	for i := range m {
		m[i].Enter = func(context.Context) (Proof, error) { launches.Add(1); return Proof{}, nil }
	}
	r, e := Run(context.Background(), spec, m, Options{Timeout: time.Second})
	if e == nil || r.Ready || launches.Load() != 0 {
		t.Fatal(r, e, launches.Load())
	}
}
func TestOneLoginFailureCancelsPeersAndNeverRunsFinalStage(t *testing.T) {
	spec, m := fixture()
	var final atomic.Int32
	m[0].Enter = func(context.Context) (Proof, error) { return Proof{}, errors.New("unknown overlay; no tap") }
	for i := 1; i < len(m); i++ {
		m[i].Enter = func(ctx context.Context) (Proof, error) { <-ctx.Done(); return Proof{}, ctx.Err() }
	}
	for i := range m {
		m[i].Recheck = func(context.Context) (Proof, error) { final.Add(1); return Proof{}, nil }
	}
	r, e := Run(context.Background(), spec, m, Options{Timeout: time.Second})
	if e == nil || r.Ready || final.Load() != 0 {
		t.Fatal(r, e)
	}
}
func TestEnteredGameWithoutServerEvidenceIsReviewNotSuccess(t *testing.T) {
	spec, m := fixture()
	base := m[0].Enter
	m[0].Enter = func(ctx context.Context) (Proof, error) { p, e := base(ctx); p.ServerVerified = false; return p, e }
	r, e := Run(context.Background(), spec, m, Options{Timeout: time.Second})
	if e == nil || r.Ready || r.Members[0].State != "NEEDS_REVIEW" {
		t.Fatal(r, e)
	}
}
func TestFreshGroupBarrierRejectsRoleChangeAfterEarlierSuccess(t *testing.T) {
	spec, m := fixture()
	base := m[4].Recheck
	m[4].Recheck = func(ctx context.Context) (Proof, error) { p, e := base(ctx); p.Role = 4; return p, e }
	r, e := Run(context.Background(), spec, m, Options{Timeout: time.Second})
	if e == nil || r.Ready {
		t.Fatal(r, e)
	}
}
func TestFinalEvidenceCannotAgeOutWhileOtherMembersWait(t *testing.T) {
	spec, m := fixture()
	base := m[4].Recheck
	m[4].Recheck = func(ctx context.Context) (Proof, error) {
		select {
		case <-ctx.Done():
			return Proof{}, ctx.Err()
		case <-time.After(80 * time.Millisecond):
			return base(ctx)
		}
	}
	r, e := Run(context.Background(), spec, m, Options{Timeout: time.Second, ProofMaxAge: 20 * time.Millisecond})
	if e == nil || r.Ready {
		t.Fatal(r, e)
	}
}
func TestStopCancelsAllWorkersAndDoesNotContinue(t *testing.T) {
	spec, m := fixture()
	started := make(chan struct{}, 5)
	var final atomic.Int32
	for i := range m {
		m[i].Enter = func(ctx context.Context) (Proof, error) {
			started <- struct{}{}
			<-ctx.Done()
			return Proof{}, ctx.Err()
		}
		m[i].Recheck = func(context.Context) (Proof, error) { final.Add(1); return Proof{}, nil }
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		r, e := Run(ctx, spec, m, Options{Timeout: time.Second})
		if r.Ready {
			e = errors.New("cancelled run ready")
		}
		done <- e
	}()
	<-started
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) || final.Load() != 0 {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("stop did not join workers")
	}
}
func TestDuplicateIdentityCalibrationOrProfessionRejectedBeforeQueries(t *testing.T) {
	for _, mode := range []string{"duplicate_instance", "duplicate_device", "bad_profession", "unsupported_role"} {
		t.Run(mode, func(t *testing.T) {
			spec, m := fixture()
			var queries atomic.Int32
			for i := range m {
				m[i].Preflight = func(context.Context) error { queries.Add(1); return nil }
			}
			switch mode {
			case "duplicate_instance":
				m[4].Instance = m[0].Instance
			case "duplicate_device":
				m[4].Identity = m[0].Identity
			case "copied_nickname":
				m[4].CharacterSignature = m[0].CharacterSignature
			case "bad_profession":
				m[4].Profession = "青云"
			case "unsupported_role":
				spec.Role = 6
			}
			r, e := Run(context.Background(), spec, m, Options{})
			if e == nil || r.Ready || queries.Load() != 0 {
				t.Fatal(r, e)
			}
		})
	}
}
func TestMalformedProofCannotClaimReadiness(t *testing.T) {
	for _, mode := range []string{"identity", "server", "hash", "future", "unverified_role"} {
		t.Run(mode, func(t *testing.T) {
			spec, m := fixture()
			base := m[1].Recheck
			m[1].Recheck = func(ctx context.Context) (Proof, error) {
				p, e := base(ctx)
				switch mode {
				case "identity":
					p.Identity = "wrong"
				case "server":
					p.Server = "another"
				case "hash":
					p.ScreenSHA = "bad"
				case "future":
					p.ObservedAt = time.Now().Add(time.Minute)
				case "unverified_role":
					p.IdentityVerified = false
				}
				return p, e
			}
			r, e := Run(context.Background(), spec, m, Options{Timeout: time.Second})
			if e == nil || r.Ready {
				t.Fatal(r, e)
			}
		})
	}
}

func TestNoNicknameSignatureNeeded(t *testing.T) {
	s, m := fixture()
	for i := range m {
		m[i].CharacterSignature = ""
	}
	r, e := Run(context.Background(), s, m, Options{})
	if e != nil || !r.Ready {
		t.Fatal(r, e)
	}
}
