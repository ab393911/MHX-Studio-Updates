package ldplayer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

type recoveryRunner struct {
	mu sync.Mutex
	bindingRunner
	failures                               int
	bootFailures                           int
	actionOutput                           []byte
	actionError                            error
	changeWindowOnIndexBoot, windowChanged bool
	reconnects                             int
	wrongDirectBoot, wrongIndexBoot        bool
	commands                               []string
}

func (r *recoveryRunner) Run(ctx context.Context, exe string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := strings.Join(args, " ")
	r.commands = append(r.commands, s)
	if s == "list2" && r.windowChanged {
		return []byte("6,six,60,999,1,300,301\n10,ten,100,101,1,400,401"), nil
	}
	if strings.Contains(s, "boot_id") && strings.Contains(s, "-s odd-serial") && r.bootFailures > 0 {
		r.bootFailures--
		return []byte("error: device offline"), nil
	}
	if strings.Contains(s, "boot_id") && strings.Contains(s, "--index 6") && r.changeWindowOnIndexBoot {
		r.windowChanged = true
	}

	if strings.Contains(s, "shell input") {
		return r.actionOutput, r.actionError
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
func TestOfflineRecoveryRequiresFreshActionAndNeverReplaysTap(t *testing.T) {
	r := &recoveryRunner{failures: 1}
	var events []string
	c := Client{Console: "console", ADB: "adb", Runner: r, OnConnection: func(s string) { events = append(events, s) }}
	if err := c.Tap(context.Background(), recoveryDevice(), 10, 10); !errors.Is(err, ErrActionNeedsRecheck) || r.reconnects != 1 {
		t.Fatal(err, r.reconnects)
	}
	if strings.Contains(strings.Join(r.commands, "\n"), "shell input tap") {
		t.Fatal("old screenshot click sent after recovery")
	}
	if err := c.Tap(context.Background(), recoveryDevice(), 10, 10); err != nil {
		t.Fatal(err)
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

func TestActionBarrierCannotBeBypassedByForegroundFallback(t *testing.T) {
	r := &recoveryRunner{failures: 1}
	base := Client{Console: "console", ADB: "adb", Runner: r}
	action := base.ForAction()
	if _, err := action.Shell(context.Background(), recoveryDevice(), "dumpsys", "activity", "activities"); !errors.Is(err, ErrActionNeedsRecheck) {
		t.Fatal(err)
	}
	count := len(r.commands)
	if _, err := action.Shell(context.Background(), recoveryDevice(), "dumpsys", "window", "windows"); !errors.Is(err, ErrActionNeedsRecheck) {
		t.Fatal(err)
	}
	if err := action.Tap(context.Background(), recoveryDevice(), 10, 10); !errors.Is(err, ErrActionNeedsRecheck) || len(r.commands) != count {
		t.Fatal("fallback dispatched after fault", err, r.commands)
	}
}
func TestRecoveryWithoutBootIdentityCannotAuthorizeAction(t *testing.T) {
	r := &recoveryRunner{failures: 1}
	c := Client{Console: "console", ADB: "adb", Runner: r}
	d := recoveryDevice()
	d.BootID = ""
	if err := c.Tap(context.Background(), d, 10, 10); err == nil || r.reconnects != 0 {
		t.Fatal(err, r.reconnects)
	}
	if strings.Contains(strings.Join(r.commands, "\n"), "input tap") {
		t.Fatal("unverified recovery clicked")
	}
}
func TestStopDuringRecoveryNotificationPreventsDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &recoveryRunner{failures: 1}
	c := Client{Console: "console", ADB: "adb", Runner: r, OnConnection: func(event string) {
		if strings.Contains(event, "恢复于") {
			cancel()
		}
	}}
	if err := c.Tap(ctx, recoveryDevice(), 10, 10); err != context.Canceled {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(r.commands, "\n"), "input tap") {
		t.Fatal("canceled action dispatched")
	}
	count := len(r.commands)
	if _, err := c.run(ctx, "adb", "devices"); err != context.Canceled || len(r.commands) != count {
		t.Fatal("runner called with canceled context", err)
	}
}

func TestBootQueryOfflineOutputIsRetriedAndReverified(t *testing.T) {
	r := &recoveryRunner{bootFailures: 1}
	c := Client{Console: "console", ADB: "adb", Runner: r}
	if err := c.Verify(context.Background(), recoveryDevice()); err != nil || r.reconnects != 1 {
		t.Fatal(err, r.reconnects)
	}
}
func TestWindowChangeDuringRecoveryStopsBeforeClick(t *testing.T) {
	r := &recoveryRunner{failures: 1, changeWindowOnIndexBoot: true}
	c := Client{Console: "console", ADB: "adb", Runner: r}
	if err := c.Tap(context.Background(), recoveryDevice(), 10, 10); err == nil {
		t.Fatal("restarted window accepted")
	}
	if strings.Contains(strings.Join(r.commands, "\n"), "input tap") {
		t.Fatal("clicked changed window")
	}
}
func TestBindCannotPublishReadyWhenBootQueryReportsOffline(t *testing.T) {
	r := &recoveryRunner{bootFailures: 10}
	c := Client{Console: "console", ADB: "adb", Runner: r}
	all, err := c.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range all {
		if d.Index == 6 && d.Serial != "" {
			t.Fatal("offline boot query published as ready", d)
		}
	}
}

func TestAmbiguousActionResultNeverReconnectsOrReplays(t *testing.T) {
	for _, exitError := range []bool{false, true} {
		r := &recoveryRunner{actionOutput: []byte("error: device offline")}
		if exitError {
			r.actionOutput = nil
			r.actionError = fmt.Errorf("device offline")
		}
		c := Client{Console: "console", ADB: "adb", Runner: r}
		if err := c.Tap(context.Background(), recoveryDevice(), 10, 10); !errors.Is(err, ErrActionOutcomeUnknown) {
			t.Fatal(err)
		}
		if count := strings.Count(strings.Join(r.commands, "\n"), "shell input tap"); count != 1 || r.reconnects != 0 {
			t.Fatal("ambiguous tap replayed", r.commands)
		}
		r.commands = nil
		if _, err := c.Shell(context.Background(), recoveryDevice(), "input", "touchscreen", "swipe", "1", "1", "2", "2", "100"); !errors.Is(err, ErrActionOutcomeUnknown) {
			t.Fatal(err)
		}
		if count := strings.Count(strings.Join(r.commands, "\n"), "shell input touchscreen swipe"); count != 1 || r.reconnects != 0 {
			t.Fatal("ambiguous swipe replayed", r.commands)
		}
	}
}

func TestQuotedSerialNotFoundIsTransportErrorButIdentityChangeIsNot(t *testing.T) {
	for _, message := range []string{"error: device 'emulator-5556' not found", `error: device "emulator-5556" not found`} {
		if !transientADB(fmt.Errorf("%s", message)) {
			t.Fatal(message)
		}
	}
	for _, message := range []string{"ADB identity changed; stopped", "instance boot identity changed; detect again", "device unauthorized"} {
		if transientADB(fmt.Errorf("%s", message)) {
			t.Fatal("nontransient error retried", message)
		}
	}
}
