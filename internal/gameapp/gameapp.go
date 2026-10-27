// Package gameapp discovers Android launcher components without guessing a game
// package. A human-confirmed binding is required before any launch command.
package gameapp

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Shell interface {
	Shell(context.Context, ...string) ([]byte, error)
}
type Component struct {
	Package  string `json:"package"`
	Activity string `json:"activity"`
}
type Binding struct {
	Component
	AndroidID string `json:"android_id"`
	Confirmed bool   `json:"user_confirmed"`
}
type Inventory struct {
	Apps       []Component
	Foreground string
}

var pkgRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)
var activityRE = regexp.MustCompile(`^\.?[A-Za-z_][A-Za-z0-9_.$]*$`)
var errAmbiguousForeground = errors.New("前台记录冲突")

func (c Component) String() string { return c.Package + "/" + c.Activity }
func (c Component) Valid() bool {
	return len(c.String()) <= 512 && pkgRE.MatchString(c.Package) && activityRE.MatchString(c.Activity)
}
func (c Component) key() string {
	a := c.Activity
	if strings.HasPrefix(a, ".") {
		a = c.Package + a
	}
	return c.Package + "/" + a
}
func ParseComponents(s string) []Component {
	seen := map[string]bool{}
	var out []Component
	for _, line := range strings.Split(s, "\n") {
		f := strings.Split(strings.TrimSpace(line), "/")
		if len(f) != 2 {
			continue
		}
		c := Component{f[0], f[1]}
		if c.Valid() && !seen[c.key()] {
			out = append(out, c)
			seen[c.key()] = true
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key() < out[j].key() })
	return out
}

// ForegroundPackage accepts only explicit resumed/focused records. Conflicting
// records are rejected; historical activity/back-stack entries are not evidence.
func ForegroundPackage(s string) (string, error) {
	seen := map[string]bool{}
	for _, line := range strings.Split(s, "\n") {
		if !strings.Contains(line, "mResumedActivity:") && !strings.Contains(line, "topResumedActivity=") && !strings.Contains(line, "mCurrentFocus=") {
			continue
		}
		for _, field := range strings.Fields(line) {
			for _, c := range ParseComponents(strings.Trim(field, "{}")) {
				seen[c.Package] = true
			}
		}
	}
	if len(seen) != 1 {
		if len(seen) > 1 {
			return "", fmt.Errorf("%w：%d个应用，未点击", errAmbiguousForeground, len(seen))
		}
		return "", fmt.Errorf("无法唯一确认前台应用（%d 个记录），没有进行游戏点击", len(seen))
	}
	for p := range seen {
		return p, nil
	}
	panic("unreachable")
}
func ReadForeground(ctx context.Context, s Shell) (string, error) {
	b, e := s.Shell(ctx, "dumpsys", "activity", "activities")
	var p string
	if e == nil {
		p, e = ForegroundPackage(string(b))
	}
	b, we := s.Shell(ctx, "dumpsys", "window", "windows")
	var w string
	if we == nil {
		w, we = ForegroundPackage(string(b))
	}
	if errors.Is(e, errAmbiguousForeground) || errors.Is(we, errAmbiguousForeground) {
		return "", errors.Join(e, we)
	}
	if e == nil && we == nil && p != w {
		return "", fmt.Errorf("游戏前台与焦点窗口不一致（%s / %s），可能有系统弹窗，未点击", p, w)
	}
	if we == nil {
		return w, nil
	}
	if e == nil {
		return p, nil
	}
	return "", errors.Join(e, we)
}
func Discover(ctx context.Context, s Shell) (Inventory, error) {
	args := []string{"package", "query-activities", "--components", "-a", "android.intent.action.MAIN", "-c", "android.intent.category.LAUNCHER"}
	b, e := s.Shell(ctx, append([]string{"cmd"}, args...)...)
	apps := ParseComponents(string(b))
	if e != nil || len(apps) == 0 {
		b, e = s.Shell(ctx, append([]string{"pm"}, args[1:]...)...)
		apps = ParseComponents(string(b))
	}
	if e != nil {
		return Inventory{}, e
	}
	if len(apps) == 0 {
		return Inventory{}, errors.New("未获得可启动应用列表；可能未安装游戏，或此 Android 不支持入口查询。未启动任何应用")
	}
	p, _ := ReadForeground(ctx, s)
	return Inventory{Apps: apps, Foreground: p}, nil
}
func VerifyBinding(ctx context.Context, s Shell, b Binding, androidID string) error {
	if !b.Confirmed || b.AndroidID == "" || b.AndroidID != androidID || !b.Component.Valid() {
		return errors.New("游戏尚未绑定或设备身份已变化，请重新发现并确认游戏")
	}
	inv, e := Discover(ctx, s)
	if e != nil {
		return e
	}
	for _, a := range inv.Apps {
		if a.key() == b.Component.key() {
			return nil
		}
	}
	return errors.New("已绑定游戏的启动入口不再存在，请重新选择；没有启动其他应用")
}
func RequireForeground(ctx context.Context, s Shell, pkg string) error {
	p, e := ReadForeground(ctx, s)
	if e != nil {
		return e
	}
	if p != pkg {
		return fmt.Errorf("前台应用已变为 %s；期望 %s，停止截图识别及点击", p, pkg)
	}
	return nil
}

type LaunchResult struct{ AlreadyForeground, ProcessWasRunning bool }

// Open never uses force-stop or am -S. A background process is brought forward
// once through its verified launcher; an already foreground game is not relaunched.
func Open(ctx context.Context, s Shell, b Binding, androidID string) (LaunchResult, error) {
	var r LaunchResult
	if e := VerifyBinding(ctx, s, b, androidID); e != nil {
		return r, e
	}
	if p, e := ReadForeground(ctx, s); e == nil && p == b.Package {
		r.AlreadyForeground = true
		r.ProcessWasRunning = true
		return r, nil
	}
	pid, e := s.Shell(ctx, "pidof", b.Package)
	r.ProcessWasRunning = e == nil && strings.TrimSpace(string(pid)) != ""
	if e = ctx.Err(); e != nil {
		return r, e
	}
	// Pass one single-quoted component argument through adb's remote shell.
	// Its grammar excludes quotes and shell operators (activity '$' is valid).
	bout, e := s.Shell(ctx, "am", "start", "-W", "-a", "android.intent.action.MAIN", "-c", "android.intent.category.LAUNCHER", "-n", "'"+b.Component.String()+"'")
	if e != nil {
		return r, e
	}
	text := strings.ToLower(string(bout))
	if strings.Contains(text, "error:") || strings.Contains(text, "exception") || strings.Contains(text, "status: timeout") {
		return r, fmt.Errorf("Android 拒绝启动已绑定游戏：%s", strings.TrimSpace(string(bout)))
	}
	wait, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	for {
		if e = RequireForeground(wait, s, b.Package); e == nil {
			return r, nil
		}
		t := time.NewTimer(350 * time.Millisecond)
		select {
		case <-wait.Done():
			t.Stop()
			return r, fmt.Errorf("启动后未验证游戏前台：%w", errors.Join(e, wait.Err()))
		case <-t.C:
		}
	}
}
