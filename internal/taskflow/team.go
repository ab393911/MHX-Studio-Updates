// Package taskflow executes screenshot-verified game operations. It does not
// infer completion from an ADB command or write simulated results into SQLite.
package taskflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"strings"
	"sync"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/vision"
)

type Actor struct {
	Instance                   int
	Identity, Name, Profession string
	Server                     string
	Role                       int
	Device                     automation.Device
	Profile                    automation.Profile
	Reader                     vision.Reader
	MinDelayMS, MaxDelayMS     int
	OnError                    func([]byte, error)
}
type Proof struct {
	Instance   int       `json:"instance"`
	Name       string    `json:"name"`
	ScreenSHA  string    `json:"screen_sha256"`
	ObservedAt time.Time `json:"observed_at"`
}
type TeamReport struct {
	Leader         int     `json:"leader"`
	Members        []Proof `json:"members"`
	Verified       bool    `json:"verified"`
	Error          string  `json:"error,omitempty"`
	RealGameTested bool    `json:"real_game_tested"`
}
type snapshot struct {
	image image.Image
	ocr   vision.Screen
	png   []byte
	sha   string
	at    time.Time
}
type controller struct {
	Actor
	last []byte
}
type check func(snapshot) error
type button func(snapshot) (automation.Rect, error)

func wait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func (c *controller) take(ctx context.Context) (snapshot, error) {
	var s snapshot
	if e := ctx.Err(); e != nil {
		return s, e
	}
	s.at = time.Now().UTC()
	b, e := c.Device.Capture(ctx)
	if e != nil {
		return s, e
	}
	c.last = b
	s.png = b
	s.image, e = automation.Decode(b)
	if e != nil {
		return s, e
	}
	s.ocr, e = c.Reader.Read(ctx, b)
	if e != nil {
		return s, e
	}
	if e = s.ocr.Validate(); e != nil {
		return s, e
	}
	h := sha256.Sum256(b)
	s.sha = hex.EncodeToString(h[:])
	return s, nil
}
func (c *controller) game(s snapshot) error {
	return c.Profile.Require(s.image, fmt.Sprintf("game_%d", c.Role))
}
func has(s snapshot, text string, r automation.Rect) bool {
	return strings.Contains(vision.Normal(s.ocr.Text(r)), vision.Normal(text))
}

var whole = automation.Rect{W: 960, H: 540}

func teamPage(s snapshot) error {
	if !has(s, "队伍", automation.Rect{X: 65, W: 115, H: 42}) || !has(s, "固定", automation.Rect{X: 870, Y: 140, W: 80, H: 75}) {
		return errors.New("队伍页标题和独立侧栏未核验")
	}
	return nil
}
func partyPage(s snapshot) error {
	if e := teamPage(s); e != nil {
		return e
	}
	if !has(s, "离开队伍", automation.Rect{Y: 440, W: 960, H: 80}) && !has(s, "创建队伍", automation.Rect{Y: 440, W: 960, H: 80}) {
		return errors.New("实际队伍标签未核验，固定队预设不算组队")
	}
	return nil
}
func invitePage(s snapshot) error {
	if !has(s, "邀请列表", automation.Rect{X: 290, Y: 25, W: 160, H: 50}) || !has(s, "队伍", automation.Rect{X: 300, Y: 72, W: 100, H: 32}) {
		return errors.New("不是已确认队伍邀请列表")
	}
	return nil
}
func confirmPage(s snapshot) error {
	if !has(s, "提醒", automation.Rect{X: 280, Y: 115, W: 90, H: 45}) || !has(s, "是否创建队伍并邀请固定队成员入队", automation.Rect{X: 320, Y: 230, W: 330, H: 65}) {
		return errors.New("不是创建固定队伍的确认框，不点击确认")
	}
	return nil
}
func textButton(text string, r automation.Rect) button {
	return func(s snapshot) (automation.Rect, error) { return s.ocr.Unique(text, r) }
}
func fixedButton(r automation.Rect) button {
	return func(snapshot) (automation.Rect, error) { return r, nil }
}

// tap revalidates both the context and the button after the random delay.
// Exactly one tap is submitted; an unconfirmed response never causes a resend.
func (c *controller) tap(ctx context.Context, before check, find button, after check) (snapshot, error) {
	s, e := c.take(ctx)
	if e != nil {
		return s, e
	}
	if e = before(s); e != nil {
		return s, e
	}
	r, e := find(s)
	if e != nil {
		return s, e
	}
	if e = wait(ctx, automation.Delay(c.MinDelayMS, c.MaxDelayMS)); e != nil {
		return s, e
	}
	fresh, e := c.take(ctx)
	if e != nil {
		return fresh, e
	}
	if e = before(fresh); e != nil {
		return fresh, e
	}
	r2, e := find(fresh)
	if e != nil {
		return fresh, e
	}
	left, top := max(r.X, r2.X), max(r.Y, r2.Y)
	right, bottom := min(r.X+r.W, r2.X+r2.W), min(r.Y+r.H, r2.Y+r2.H)
	r = automation.Rect{X: left, Y: top, W: right - left, H: bottom - top}
	x, y, e := automation.Point(r)
	if e != nil {
		return fresh, e
	}
	if e = ctx.Err(); e != nil {
		return fresh, e
	}
	if e = c.Device.Tap(ctx, x, y); e != nil {
		return fresh, e
	}
	return c.await(ctx, after, 18*time.Second)
}
func (c *controller) await(ctx context.Context, condition check, timeout time.Duration) (snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stable := 0
	var last error
	var s snapshot
	for {
		var e error
		s, e = c.take(ctx)
		if e != nil {
			return s, e
		}
		last = condition(s)
		if last == nil {
			stable++
		} else {
			stable = 0
		}
		if stable >= 2 {
			return s, nil
		}
		if e = wait(ctx, 250*time.Millisecond); e != nil {
			return s, errors.Join(last, e)
		}
	}
}
func (c *controller) openTeam(ctx context.Context) error {
	s, e := c.take(ctx)
	if e != nil {
		return e
	}
	if partyPage(s) == nil {
		return nil
	}
	if teamPage(s) == nil {
		_, e = c.tap(ctx, teamPage, textButton("队伍", automation.Rect{X: 870, Y: 65, W: 80, H: 75}), partyPage)
		return e
	}
	if e = c.game(s); e != nil {
		return e
	}
	// Handoff screenshot 146: the team flag is below the quest panel, not
	// the expand-menu button and not a coordinate used as page evidence.
	_, e = c.tap(ctx, c.game, fixedButton(automation.Rect{X: 920, Y: 208, W: 24, H: 26}), teamPage)
	if e != nil {
		return e
	}
	s, e = c.take(ctx)
	if e != nil {
		return e
	}
	if partyPage(s) == nil {
		return nil
	}
	_, e = c.tap(ctx, teamPage, textButton("队伍", automation.Rect{X: 870, Y: 65, W: 80, H: 75}), partyPage)
	return e
}
func sameNames(got []string, wanted []Actor) bool {
	if len(got) != 5 || len(wanted) != 5 {
		return false
	}
	seen := map[string]bool{}
	for _, a := range wanted {
		seen[vision.Normal(a.Name)] = true
	}
	for _, n := range got {
		n = vision.Normal(n)
		if !seen[n] {
			return false
		}
		delete(seen, n)
	}
	return len(seen) == 0
}
func readNames(s snapshot, fixed bool) []string {
	var names []string
	for i := 0; i < 5; i++ {
		r := automation.Rect{X: 80 + 170*i, Y: 256, W: 120, H: 25}
		if fixed {
			r = automation.Rect{X: 100 + 160*i, Y: 253, W: 140, H: 34}
		}
		names = append(names, vision.Normal(s.ocr.Text(r)))
	}
	return names
}
func fullTeam(actors []Actor, leader string) check {
	return func(s snapshot) error {
		if e := partyPage(s); e != nil {
			return e
		}
		names := readNames(s, false)
		if !sameNames(names, actors) || names[0] != vision.Normal(leader) || !has(s, "队长", automation.Rect{X: 45, Y: 130, W: 35, H: 85}) {
			return errors.New("五名实际成员或队长身份尚未核验")
		}
		return nil
	}
}
func fixedTeam(actors []Actor, leader string) check {
	return func(s snapshot) error {
		if e := teamPage(s); e != nil {
			return e
		}
		names := readNames(s, true)
		if !sameNames(names, actors) || names[0] != vision.Normal(leader) {
			return errors.New("固定队成员与当前五个角色不一致，不发送邀请")
		}
		for i := 0; i < 5; i++ {
			if !has(s, "在线", automation.Rect{X: 100 + 160*i, Y: 330, W: 140, H: 35}) || has(s, "离线", automation.Rect{X: 100 + 160*i, Y: 330, W: 140, H: 35}) {
				return errors.New("固定队五人未全部在线，不使用离线托管")
			}
		}
		// Only the explicit unchecked box from screenshot 77 is accepted. Checking
		// the caption text alone says nothing about whether offline hosting is enabled.
		greens := 0
		for y := 414; y < 430; y++ {
			for x := 714; x < 730; x++ {
				r, g, b, _ := s.image.At(x, y).RGBA()
				if g > r*12/10 && g > b*11/10 && g > 30000 {
					greens++
				}
			}
		}
		if greens > 12 {
			return errors.New("离线托管已开启，停止自动邀请")
		}
		return nil
	}
}
func (c *controller) leaderInvite(ctx context.Context, actors []Actor) error {
	if e := c.openTeam(ctx); e != nil {
		return e
	}
	s, e := c.take(ctx)
	if e != nil {
		return e
	}
	if fullTeam(actors, c.Name)(s) == nil {
		return nil
	}
	// A populated, unrelated party is not dissolved or overwritten.
	if !has(s, "没有队伍的时候", automation.Rect{Y: 450, W: 550, H: 65}) {
		wanted := map[string]bool{}
		for _, a := range actors {
			wanted[vision.Normal(a.Name)] = true
		}
		for _, n := range readNames(s, false) {
			if n != "" && !wanted[n] {
				return errors.New("当前队伍存在未核验成员，不自动改队")
			}
		}
	}
	if _, e = c.tap(ctx, teamPage, textButton("固定", automation.Rect{X: 870, Y: 140, W: 80, H: 75}), fixedTeam(actors, c.Name)); e != nil {
		return e
	}
	if _, e = c.tap(ctx, fixedTeam(actors, c.Name), textButton("发起组队", automation.Rect{X: 700, Y: 450, W: 155, H: 65}), confirmPage); e != nil {
		return e
	}
	if _, e = c.tap(ctx, confirmPage, textButton("确定", automation.Rect{X: 500, Y: 350, W: 160, H: 55}), teamPage); e != nil {
		return e
	}
	// Force the actual party tab: fixed-team presets are not proof of 5/5.
	_, e = c.tap(ctx, teamPage, textButton("队伍", automation.Rect{X: 870, Y: 65, W: 80, H: 75}), partyPage)
	return e
}
func (c *controller) accept(ctx context.Context, leader string) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	for {
		s, e := c.take(ctx)
		if e != nil {
			return e
		}
		if invitePage(s) == nil {
			before := func(s snapshot) error {
				if e := invitePage(s); e != nil {
					return e
				}
				if vision.Normal(s.ocr.Text(automation.Rect{X: 305, Y: 110, W: 180, H: 25})) != vision.Normal(leader) {
					return errors.New("邀请队长不符，不接受")
				}
				return nil
			}
			if e = before(s); e != nil {
				return e
			}
			// Wait out the game's countdown; never parse a changing label as an
			// already accepted invitation. Unknown extra rows are not selected.
			if _, e = s.ocr.Unique("接受", automation.Rect{X: 580, Y: 125, W: 76, H: 35}); e == nil {
				_, e = c.tap(ctx, before, textButton("接受", automation.Rect{X: 580, Y: 125, W: 76, H: 35}), c.game)
				if e != nil {
					return e
				}
				return c.openTeam(ctx)
			}
		} else if c.game(s) == nil {
			icon := vision.InvitationIcon()
			if _, e = icon.Find(s.image); e == nil {
				_, e = c.tap(ctx, c.game, func(s snapshot) (automation.Rect, error) { return icon.Find(s.image) }, invitePage)
				if e != nil {
					return e
				}
			}
		} else {
			return errors.New("等待邀请时出现未知页面，不点击")
		}
		if e = wait(ctx, 300*time.Millisecond); e != nil {
			return e
		}
	}
}

func RunTeam(ctx context.Context, actors []Actor, leader int) (report TeamReport, err error) {
	report.Leader = leader
	defer func() {
		if err != nil {
			report.Error = err.Error()
		}
	}()
	if len(actors) != 5 {
		return report, errors.New("组队需要恰好五个已核验角色")
	}
	seen := map[string]bool{}
	indices := map[int]bool{}
	identities := map[string]bool{}
	jobs := map[string]int{}
	lead := -1
	controls := make([]*controller, 5)
	for i, a := range actors {
		if a.Identity == "" || identities[a.Identity] || a.Server == "" || a.Server != actors[0].Server || a.Role != actors[0].Role || vision.Normal(a.Name) == "" || seen[vision.Normal(a.Name)] || indices[a.Instance] || a.Role < 1 || a.Role > 5 || a.Device == nil || a.Reader == nil || a.MinDelayMS < 100 || a.MaxDelayMS < a.MinDelayMS || a.MaxDelayMS > 3000 {
			return report, errors.New("角色身份、独立昵称、实例或点击设置不足，不发送邀请")
		}
		seen[vision.Normal(a.Name)] = true
		identities[a.Identity] = true
		indices[a.Instance] = true
		jobs[a.Profession]++
		if a.Instance == leader {
			lead = i
		}
		controls[i] = &controller{Actor: a}
	}
	if lead < 0 || jobs["合欢"] != 1 || jobs["天音"] != 1 || jobs["青云"] != 1 || jobs["鬼王"] != 2 {
		return report, errors.New("队长或五人职业配置不符")
	}
	userCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	defer func() {
		if err != nil && !errors.Is(userCtx.Err(), context.Canceled) {
			for _, c := range controls {
				if c.OnError != nil {
					c.OnError(c.last, err)
				}
			}
		}
	}()
	// All five must still be in their verified game role before the first
	// navigation/invitation. No old report is accepted as this preflight.
	for _, c := range controls {
		if _, e := c.await(ctx, c.game, 8*time.Second); e != nil {
			return report, e
		}
	}
	// Read-only fast path on an already-correct team cannot manufacture proof
	// from fixed presets: final checks always inspect the actual party tab.
	if e := controls[lead].leaderInvite(ctx, actors); e != nil {
		return report, e
	}
	leaderView, e := controls[lead].take(ctx)
	if e != nil {
		return report, e
	}
	alreadyFull := fullTeam(actors, actors[lead].Name)(leaderView) == nil
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for i, c := range controls {
		if i == lead {
			continue
		}
		wg.Add(1)
		go func(c *controller) {
			defer wg.Done()
			s, e := c.take(ctx)
			if e == nil && alreadyFull {
				e = c.openTeam(ctx)
			} else if e == nil && partyPage(s) == nil {
				e = nil
			} else if e == nil {
				e = c.accept(ctx, actors[lead].Name)
			}
			if e != nil {
				errs <- fmt.Errorf("实例%d：%w", c.Instance, e)
				cancel()
			}
		}(c)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		err = errors.Join(err, e)
	}
	if err != nil {
		return report, err
	}
	for _, c := range controls {
		s, e := c.await(ctx, fullTeam(actors, actors[lead].Name), 25*time.Second)
		if e != nil {
			return report, e
		}
		report.Members = append(report.Members, Proof{Instance: c.Instance, Name: c.Name, ScreenSHA: s.sha, ObservedAt: s.at})
	}
	for _, p := range report.Members {
		if time.Since(p.ObservedAt) > 15*time.Second {
			return report, errors.New("五台最终队伍证据过期，未标记完成")
		}
	}
	// Every actor returns through the evidenced page back arrow. This must be
	// verified too; a successful team read does not imply the main UI is ready.
	for _, c := range controls {
		if _, e := c.tap(ctx, fullTeam(actors, actors[lead].Name), fixedButton(automation.Rect{X: 24, Y: 8, W: 32, H: 26}), c.game); e != nil {
			return report, e
		}
	}
	report.Verified = true
	return report, nil
}
