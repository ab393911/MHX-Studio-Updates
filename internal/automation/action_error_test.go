package automation

import (
	"context"
	"errors"
	"testing"
)

type uncertainTap struct {
	*visualSimulator
	calls int
	err   error
}

func (d *uncertainTap) Tap(context.Context, int, int) error { d.calls++; return d.err }
func TestClickDoesNotReplayDeviceErrorThroughOuterRetryLoop(t *testing.T) {
	s := simulator(t)
	sentinel := errors.New("action outcome unknown")
	d := &uncertainTap{visualSimulator: s, err: sentinel}
	e := Engine{Device: d, Profile: s.profile, Options: Options{Retries: 3, MinDelayMS: 100, MaxDelayMS: 100}}
	if err := e.click(context.Background(), "notice", "notice_enter", "tree"); !errors.Is(err, sentinel) || d.calls != 1 {
		t.Fatal(err, d.calls)
	}
}
