package ldplayer

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

type recoveryRunner struct {
	mu sync.Mutex
	bindingRunner
	failures                        int
	reconnects                      int
	wrongDirectBoot, wrongIndexBoot bool
	commands                        []string
}

func (r *recoveryRunner) Run(ctx context.Context, exe string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := strings.Join(args, " ")
	r.commands = append(r.commands, s)
	if strings.Contains(s, "shell input tap") {
		return nil, nil
	}
	if strings.HasSuffix(s, " reconnect") {
		r.reconnects++
		return nil, nil
	}
	if strings.Contains(s, "-s odd-serial shell settings") && r.failures > 0 {
		r.failures--
		return nil, fmt.Errorf("device offline")
	}
	if strings.Contains(s, "boot_id") && ((r.wrongDirectBoot && strings.Contains(s, "-s odd-serial")) || (r.wrongIndexBoot && strings.Contains(s, "--index 6"))) {
		return []byte("aaaaaaaa-bbbb-4ccc-8ddd-111111111111"), nil
	}
	return r.bindingRunner.Run(ctx, exe, args...)
}
func recoveryDevice() Instance {
	return Instance{Index: 6, PID: 300, TopWindow: 60, BindWindow: 61, Android: true, Serial: "odd-serial", AndroidID: "abcdef0123456789", BootID: "aaaaaaaa-bbbb-4ccc-8ddd-000000000001"}
}
func TestOfflineReconnectRechecksBothIdentitiesBeforeTap(t *testing.T) {
	r := &recoveryRunner{failures: 1}
	var events []string
	c := Client{Console: "console", ADB: "adb", Runner: r, OnConnection: func(s string) { events = append(events, s) }}
	if err := c.Tap(context.Background(), recoveryDevice(), 10, 10); err != nil || r.reconnects != 1 {
		t.Fatal(err, r.reconnects)
	}
	calls := strings.Join(r.commands, "\n")
	if strings.Count(calls, "shell input tap") != 1 || len(events) != 2 || !strings.Contains(events[1], "恢复") {
		t.Fatal(calls, events)
	}
	if !strings.Contains(calls, "--index 6 --command shell cat /proc/sys/kernel/random/boot_id") {
		t.Fatal(calls)
	}
}
func TestOfflineRecoveryCannotTapChangedCloneOrRestartedInstance(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		r := &recoveryRunner{failures: 1, wrongDirectBoot: !indexed, wrongIndexBoot: indexed}
		c := Client{Console: "console", ADB: "adb", Runner: r}
		if err := c.Tap(context.Background(), recoveryDevice(), 10, 10); err == nil {
			t.Fatal("identity change accepted")
		}
		for _, s := range r.commands {
			if strings.Contains(s, "input tap") {
				t.Fatal(s)
			}
		}
	}
}
func TestOfflineRetriesBoundedAndCancellationStops(t *testing.T) {
	r := &recoveryRunner{failures: 10}
	c := Client{Console: "console", ADB: "adb", Runner: r}
	if err := c.Verify(context.Background(), recoveryDevice()); err == nil || r.reconnects != 2 {
		t.Fatal(err, r.reconnects)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n := r.reconnects
	if err := c.Verify(ctx, recoveryDevice()); err != context.Canceled || r.reconnects != n {
		t.Fatal(err)
	}
}

func TestOnDemandRecoveryRefreshesMappingInsteadOfTrustingCachedSerial(t *testing.T) {
	r := &recoveryRunner{failures: 1}
	c := Client{Console: "console", ADB: "adb", Runner: r}
	got, err := c.RecoverInstance(context.Background(), 6)
	if err != nil || got.Serial != "odd-serial" || got.BootID != recoveryDevice().BootID {
		t.Fatal(got, err)
	}
	for _, s := range r.commands {
		if strings.Contains(s, "input") || strings.Contains(s, "launch --index") {
			t.Fatal("recovery performed action", s)
		}
	}
}
func TestOfflineOutputAndScanningAreDifferentStates(t *testing.T) {
	if err := adbOutputError([]byte("error: device offline")); err == nil || !transientADB(err) {
		t.Fatal(err)
	}
	pending := Instance{Index: 4, PID: 400, Android: true}
	offline := pending
	offline.ConnectionError = "device offline"
	if strings.Contains(pending.Status(), "offline") || !strings.Contains(offline.Status(), "offline") {
		t.Fatal(pending.Status(), offline.Status())
	}
}
