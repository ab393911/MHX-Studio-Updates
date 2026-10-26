package ldplayer

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Instance struct {
	Index      int
	Name       string
	TopWindow  uint64
	BindWindow uint64
	Android    bool
	PID        int
	Serial     string
	AndroidID  string
}

func ParseList2(text string) ([]Instance, error) {
	text = strings.TrimPrefix(text, "\ufeff")
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	seen := map[int]bool{}
	var all []Instance
	for {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		if len(row) == 1 && strings.TrimSpace(row[0]) == "" {
			continue
		}
		if len(row) != 7 {
			return nil, fmt.Errorf("unexpected list2 row with %d fields", len(row))
		}
		n, e := strconv.Atoi(strings.TrimSpace(row[0]))
		if e != nil || n < 0 {
			return nil, errors.New("invalid LDPlayer instance index")
		}
		if seen[n] {
			return nil, errors.New("duplicate LDPlayer index")
		}
		seen[n] = true
		h, e := strconv.ParseUint(strings.TrimSpace(row[2]), 10, 64)
		if e != nil {
			return nil, e
		}
		b, e := strconv.ParseUint(strings.TrimSpace(row[3]), 10, 64)
		if e != nil {
			return nil, e
		}
		a, e := strconv.Atoi(strings.TrimSpace(row[4]))
		if e != nil || a < 0 || a > 1 {
			return nil, errors.New("invalid Android readiness")
		}
		pid, e := strconv.Atoi(strings.TrimSpace(row[5]))
		if e != nil {
			return nil, e
		}
		all = append(all, Instance{Index: n, Name: row[1], TopWindow: h, BindWindow: b, Android: a == 1, PID: pid})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Index < all[j].Index })
	return all, nil
}
func ParseDevices(text string) []string {
	set := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[1] == "device" {
			set[f[0]] = true
		}
	}
	var out []string
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}
type OSRunner struct{}

func (OSRunner) Run(ctx context.Context, exe string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	hide(cmd)
	cmd.WaitDelay = 2 * time.Second
	var stdout limitedBuffer
	cmd.Stdout = &stdout
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	if e := cmd.Run(); e != nil {
		return nil, fmt.Errorf("%s: %w (%s)", filepath.Base(exe), e, strings.TrimSpace(string(stderr.b)))
	}
	return stdout.b, nil
}

type limitedBuffer struct{ b []byte }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(b.b)+len(p) > 8<<20 {
		return 0, errors.New("command output exceeds 8 MiB")
	}
	b.b = append(b.b, p...)
	return len(p), nil
}

type Client struct {
	Console string
	ADB     string
	Runner  Runner
}

func (c Client) run(ctx context.Context, exe string, args ...string) ([]byte, error) {
	r := c.Runner
	if r == nil {
		r = OSRunner{}
	}
	sub, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return r.Run(sub, exe, args...)
}
func (c Client) adb(ctx context.Context, s string, args ...string) ([]byte, error) {
	return c.run(ctx, c.ADB, append([]string{"-s", s}, args...)...)
}
func idFrom(text string) string {
	for _, l := range strings.Split(text, "\n") {
		v := strings.TrimSpace(l)
		if len(v) == 16 {
			if _, e := strconv.ParseUint(v, 16, 64); e == nil && v != "0000000000000000" {
				return strings.ToLower(v)
			}
		}
	}
	return ""
}
func (c Client) Instances(ctx context.Context) ([]Instance, error) {
	b, e := c.run(ctx, c.Console, "list2")
	if e != nil {
		return nil, e
	}
	return ParseList2(decodeConsole(b))
}

// Bind compares Android IDs reported through the instance-index API and ADB.
// No assumed ADB port or fixed set of five serials is used.
func (c Client) Discover(ctx context.Context) ([]Instance, error) {
	all, e := c.Instances(ctx)
	if e != nil {
		return nil, e
	}
	for _, x := range all {
		if x.Android && x.PID > 0 {
			if _, e = c.run(ctx, c.Console, "adb", "--index", strconv.Itoa(x.Index), "--command", "shell settings get secure android_id"); e != nil {
				return nil, e
			}
		}
	}
	b, e := c.run(ctx, c.ADB, "devices", "-l")
	if e != nil {
		return nil, e
	}
	byID := map[string][]string{}
	for _, s := range ParseDevices(string(b)) {
		b, e = c.adb(ctx, s, "shell", "settings", "get", "secure", "android_id")
		if e != nil {
			continue
		}
		id := idFrom(string(b))
		if id != "" {
			byID[id] = append(byID[id], s)
		}
	}
	seen := map[string]bool{}
	for i, x := range all {
		if !x.Android || x.PID <= 0 {
			continue
		}
		b, e = c.run(ctx, c.Console, "adb", "--index", strconv.Itoa(x.Index), "--command", "shell settings get secure android_id")
		if e != nil {
			return nil, e
		}
		id := idFrom(string(b))
		s := byID[id]
		if id == "" || len(s) != 1 || seen[id] {
			return nil, fmt.Errorf("instance %d: cannot uniquely verify ADB identity (duplicate clones must be resolved)", x.Index)
		}
		seen[id] = true
		all[i].Serial = s[0]
		all[i].AndroidID = id
	}
	return all, nil
}
func (c Client) Verify(ctx context.Context, d Instance) error {
	all, e := c.Instances(ctx)
	if e != nil {
		return e
	}
	found := false
	for _, x := range all {
		if x.Index == d.Index && x.PID == d.PID && x.TopWindow == d.TopWindow && x.BindWindow == d.BindWindow && x.Android {
			found = true
		}
	}
	if !found {
		return errors.New("instance restarted or window binding changed; detect again")
	}
	b, e := c.adb(ctx, d.Serial, "shell", "settings", "get", "secure", "android_id")
	if e != nil {
		return e
	}
	if idFrom(string(b)) != d.AndroidID {
		return errors.New("ADB identity changed; stopped")
	}
	return nil
}
func (c Client) Screenshot(ctx context.Context, d Instance) ([]byte, error) {
	if e := c.Verify(ctx, d); e != nil {
		return nil, e
	}
	return c.adb(ctx, d.Serial, "exec-out", "screencap", "-p")
}
func (c Client) Tap(ctx context.Context, d Instance, x, y int) error {
	if x < 0 || x >= 960 || y < 0 || y >= 540 {
		return errors.New("tap outside 960x540")
	}
	if e := c.Verify(ctx, d); e != nil {
		return e
	}
	_, e := c.adb(ctx, d.Serial, "shell", "input", "tap", strconv.Itoa(x), strconv.Itoa(y))
	return e
}
func Find(configured string) (string, string) {
	var dirs []string
	if configured != "" {
		if strings.EqualFold(filepath.Ext(configured), ".exe") {
			dirs = append(dirs, filepath.Dir(configured))
		} else {
			dirs = append(dirs, configured)
		}
	}
	for _, drive := range []string{"C:", "D:", "E:", "F:", "G:"} {
		for _, s := range []string{`\LDPlayer*`, `\leidian\LDPlayer*`, `\Program Files\LDPlayer*`, `\Program Files\ldplayer*box`, `\雷电模拟器*`} {
			hits, _ := filepath.Glob(drive + s)
			dirs = append(dirs, hits...)
		}
	}
	for _, dir := range dirs {
		for _, n := range []string{"ldconsole.exe", "dnconsole.exe"} {
			p := filepath.Join(dir, n)
			a := filepath.Join(dir, "adb.exe")
			if st, e := os.Stat(p); e == nil && !st.IsDir() {
				if _, e = os.Stat(a); e == nil {
					return p, a
				}
			}
		}
	}
	return "", ""
}
