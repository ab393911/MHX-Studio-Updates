package ldplayer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDynamicList(t *testing.T) {
	all, e := ParseList2("10,ten,100,101,1,400,401\r\n6,six,60,61,1,300,301\r\n1,one,0,0,0,-1,-1\r\n")
	if e != nil {
		t.Fatal(e)
	}
	if len(all) != 3 || all[0].Index != 1 || all[1].Index != 6 || all[2].Index != 10 {
		t.Fatal(all)
	}
	if len(ParseDevices("List of devices attached\nemulator-7777 device model:x\n127.0.0.1:21999 device\nx offline\ny unauthorized")) != 2 {
		t.Fatal("devices parse")
	}
}
func TestRejectAmbiguousList(t *testing.T) {
	for _, s := range []string{"1,a,0,0,1,4,5\n1,b,0,0,1,6,7", "1,a,b,c,1,2,3", "garbage"} {
		if _, e := ParseList2(s); e == nil {
			t.Fatal(s)
		}
	}
}

type fakeRunner struct{ duplicate bool }

func (f fakeRunner) Run(_ context.Context, exe string, args ...string) ([]byte, error) {
	s := strings.Join(args, " ")
	if s == "list2" {
		return []byte("6,six,60,61,1,300,301\n10,ten,100,101,1,400,401"), nil
	}
	if strings.HasPrefix(s, "devices") {
		return []byte("List of devices attached\nodd-serial device\n127.0.0.1:21999 device\n"), nil
	}
	if strings.Contains(s, "android_id") {
		id := "abcdef0123456789"
		if strings.Contains(s, "--index 10") || strings.Contains(s, "127.0.0.1:21999") {
			id = "fedcba9876543210"
		}
		if f.duplicate {
			id = "abcdef0123456789"
		}
		return []byte(id), nil
	}
	return nil, fmt.Errorf("unexpected %s %s", exe, s)
}
func TestBindingUsesIdentityNotPortFormula(t *testing.T) {
	c := Client{Console: "console", ADB: "adb", Runner: fakeRunner{}}
	all, e := c.Discover(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if all[0].Index != 6 || all[0].Serial != "odd-serial" || all[1].Index != 10 || all[1].Serial != "127.0.0.1:21999" {
		t.Fatal(all)
	}
	c.Runner = fakeRunner{duplicate: true}
	all, e = c.Discover(context.Background())
	if e != nil || len(all) != 2 || all[0].Serial != "" || all[1].Serial != "" || all[0].ConnectionError == "" {
		t.Fatal("ambiguous devices must remain visible but unbound", all, e)
	}
}

func TestLDPlayer14TenColumnList(t *testing.T) {
	all, e := ParseList2("\ufeff6,雷电模拟器-6,60,61,1,300,301,960,540,160\r\n10,雷电模拟器-10,0,0,0,-1,-1,960,540,160\r\n")
	if e != nil || len(all) != 2 || !all[0].Running() || all[1].Running() || all[0].Index != 6 {
		t.Fatal(all, e)
	}
	if _, e = ParseList2("99999,电脑桌面,0,0,0,-1,-1,960,540,160"); e == nil {
		t.Fatal("desktop sentinel accepted")
	}
}

type bindingRunner struct{ offline, aliases, clones, sameBoot bool }

func (r bindingRunner) Run(ctx context.Context, exe string, args ...string) ([]byte, error) {
	s := strings.Join(args, " ")
	if s == "list2" {
		return []byte("6,six,60,61,1,300,301,960,540,160\n10,ten,100,101,1,400,401,960,540,160"), nil
	}
	if strings.HasPrefix(s, "devices") {
		out := "List of devices attached\nodd-serial device\n127.0.0.1:21999 device\n"
		if r.aliases {
			out += "emulator-alias device\n"
		}
		return []byte(out), nil
	}
	second := strings.Contains(s, "--index 10") || strings.Contains(s, "127.0.0.1:21999")
	if r.offline && second {
		return nil, fmt.Errorf("device offline")
	}
	if strings.Contains(s, "android_id") {
		if second && !r.clones {
			return []byte("fedcba9876543210"), nil
		}
		return []byte("abcdef0123456789"), nil
	}
	if strings.Contains(s, "boot_id") {
		if second && !r.sameBoot {
			return []byte("aaaaaaaa-bbbb-4ccc-8ddd-111111111111"), nil
		}
		return []byte("aaaaaaaa-bbbb-4ccc-8ddd-000000000001"), nil
	}
	return nil, fmt.Errorf("unexpected command %s", s)
}
func TestOfflineDoesNotHideOtherInstances(t *testing.T) {
	c := Client{Console: "console", ADB: "adb", Runner: bindingRunner{offline: true}}
	all, e := c.Discover(context.Background())
	if e != nil || len(all) != 2 || all[0].Serial == "" || all[1].Serial != "" || all[1].ConnectionError == "" {
		t.Fatal(all, e)
	}
}
func TestClonesAndSerialAliasesUseBootIdentity(t *testing.T) {
	c := Client{Console: "console", ADB: "adb", Runner: bindingRunner{clones: true, aliases: true}}
	all, e := c.Discover(context.Background())
	if e != nil || len(all) != 2 || all[0].Serial == "" || all[1].Serial == "" || all[0].Serial == all[1].Serial || all[0].BootID == all[1].BootID {
		t.Fatal(all, e)
	}
	if e = c.Verify(context.Background(), all[0]); e != nil {
		t.Fatal(e)
	}
	c.Runner = bindingRunner{clones: true, sameBoot: true}
	if e = c.Verify(context.Background(), all[1]); e == nil {
		t.Fatal("changed boot identity accepted")
	}
	all, e = c.Discover(context.Background())
	if e != nil || all[0].Serial != "" || all[1].Serial != "" {
		t.Fatal("ambiguous cloned boot must not receive clicks", all, e)
	}
}
func TestMissingADBStillEnumerates(t *testing.T) {
	c := Client{Console: "console", Runner: fakeRunner{}}
	all, e := c.Discover(context.Background())
	if e != nil || len(all) != 2 || !all[0].Running() || all[0].ConnectionError == "" {
		t.Fatal(all, e)
	}
}
func TestInstallationCustomPathAndMissingADB(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "自定义 雷电安装")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(dir, "dnconsole.exe")
	if e := os.WriteFile(p, []byte("fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	all := installations(p, []installDirectory{{dir, "运行中的进程"}, {dir, "重复"}})
	if len(all) != 1 || all[0].Console != p || all[0].ADB != "" || all[0].Source != "已选择的目录" {
		t.Fatal(all)
	}
}

type launchRunner struct {
	mu         sync.Mutex
	running    map[int]bool
	launches   []int
	neverReady bool
}

func (r *launchRunner) Run(ctx context.Context, exe string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(args) == 1 && args[0] == "list2" {
		var out string
		for n := 6; n <= 10; n++ {
			if r.running[n] {
				android := 1
				if r.neverReady {
					android = 0
				}
				out += fmt.Sprintf("%d,vm%d,100,101,%d,%d,301,960,540,160\n", n, n, android, 300+n)
			} else {
				out += fmt.Sprintf("%d,vm%d,0,0,0,-1,-1,960,540,160\n", n, n)
			}
		}
		return []byte(out), nil
	}
	if len(args) == 3 && args[0] == "launch" && args[1] == "--index" {
		var n int
		_, _ = fmt.Sscan(args[2], &n)
		r.launches = append(r.launches, n)
		r.running[n] = true
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected command %v", args)
}
func TestStartOnlySpecifiedGroupAndSkipRunning(t *testing.T) {
	r := &launchRunner{running: map[int]bool{6: true, 9: true}}
	c := Client{Console: "console", Runner: r}
	all, e := c.Start(context.Background(), []int{6, 7, 8, 9, 10}, LaunchOptions{LaunchInterval: time.Millisecond})
	if e != nil || len(all) != 5 || !reflect.DeepEqual(r.launches, []int{7, 8, 10}) {
		t.Fatal(all, r.launches, e)
	}
	_, e = c.Start(context.Background(), []int{6, 7, 8, 9, 11}, LaunchOptions{})
	if e == nil || len(r.launches) != 3 {
		t.Fatal("missing index must reject the complete group before launch", r.launches, e)
	}
}
func TestStartCanLaunchClosedInstanceWithoutADB(t *testing.T) {
	r := &launchRunner{running: map[int]bool{}}
	c := Client{Console: "console", Runner: r}
	all, e := c.Start(context.Background(), []int{10}, LaunchOptions{})
	if e != nil || !all[4].Android || !reflect.DeepEqual(r.launches, []int{10}) {
		t.Fatal(all, e)
	}
}
func TestStartTimeoutRetainsStartedInstances(t *testing.T) {
	r := &launchRunner{running: map[int]bool{}, neverReady: true}
	c := Client{Console: "console", Runner: r}
	_, e := c.Start(context.Background(), []int{6}, LaunchOptions{Timeout: 10 * time.Millisecond, PollInterval: time.Millisecond})
	if e == nil || !r.running[6] || len(r.launches) != 1 {
		t.Fatal("timeout must retain window and must not relaunch blindly", r.launches, e)
	}
}

func TestCancelledStartDoesNotLaunch(t *testing.T) {
	r := &launchRunner{running: map[int]bool{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := (Client{Console: "console", Runner: r}).Start(ctx, []int{6}, LaunchOptions{})
	if e == nil || len(r.launches) != 0 {
		t.Fatal("cancelled operation launched emulator", r.launches, e)
	}
}

type selectionRunner struct{}

func (selectionRunner) Run(ctx context.Context, exe string, args ...string) ([]byte, error) {
	if strings.Contains(exe, "broken") {
		return nil, fmt.Errorf("unavailable")
	}
	if strings.Contains(exe, "idle") {
		return []byte("0,idle,0,0,0,-1,-1"), nil
	}
	return []byte("6,running,60,61,1,300,301,960,540,160"), nil
}
func TestInstallationSelectionPrefersRunningAndHonorsExplicitPath(t *testing.T) {
	candidates := []Installation{{Console: "broken", Source: "Windows 安装记录"}, {Console: "idle", Source: "Windows 安装记录"}, {Console: "active", Source: "正在运行的雷电进程"}}
	c, all, _, e := SelectInstallation(context.Background(), candidates, selectionRunner{})
	if e != nil || c.Console != "active" || all[0].Index != 6 {
		t.Fatal(c, all, e)
	}
	candidates[1].Source = "已选择的目录"
	c, _, _, e = SelectInstallation(context.Background(), candidates, selectionRunner{})
	if e != nil || c.Console != "idle" {
		t.Fatal(c, e)
	}
}
