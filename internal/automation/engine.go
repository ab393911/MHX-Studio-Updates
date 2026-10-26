package automation

import (
	"context"
	"errors"
	"fmt"
	"image"
	"sync"
	"time"
)

type Device interface {
	Capture(context.Context) ([]byte, error)
	Tap(context.Context, int, int) error
}
type Options struct {
	Retries      int
	MinDelayMS   int
	MaxDelayMS   int
	StateTimeout time.Duration
	OnLog        func(string)
	OnError      func([]byte, error)
}
type Engine struct {
	Device  Device
	Profile Profile
	Options Options
	last    []byte
}

func (e *Engine) log(s string) {
	if e.Options.OnLog != nil {
		e.Options.OnLog(s)
	}
}
func (e *Engine) image(ctx context.Context) (image.Image, error) {
	b, err := e.Device.Capture(ctx)
	if err != nil {
		return nil, err
	}
	e.last = b
	return Decode(b)
}
func pause(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func (e *Engine) wait(ctx context.Context, want string) error {
	timeout := e.Options.StateTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	sub, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var last error
	for {
		img, err := e.image(sub)
		if err == nil {
			err = e.Profile.Require(img, want)
		}
		if err == nil {
			return nil
		}
		last = err
		if err = pause(sub, 350*time.Millisecond); err != nil {
			return fmt.Errorf("waiting for %s: %w", want, errors.Join(last, err))
		}
	}
}
func (e *Engine) click(ctx context.Context, before, button, after string) error {
	if _, ok := e.Profile.Pages[before]; !ok {
		return fmt.Errorf("missing calibration: %s", before)
	}
	if _, ok := e.Profile.Pages[after]; !ok {
		return fmt.Errorf("missing calibration: %s", after)
	}
	r, ok := e.Profile.Buttons[button]
	if !ok {
		return fmt.Errorf("missing button calibration: %s", button)
	}
	var last error
	for attempt := 0; attempt <= e.Options.Retries; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		img, err := e.image(ctx)
		if err != nil {
			last = err
			continue
		}
		if err = e.Profile.Require(img, after); err == nil {
			return nil
		}
		if err = e.Profile.Require(img, before); err != nil {
			last = err
			e.log(fmt.Sprintf("%s 未验证，重试 %d/%d", before, attempt, e.Options.Retries))
			if err = pause(ctx, 500*time.Millisecond); err != nil {
				return err
			}
			continue
		}
		min, max := e.Options.MinDelayMS, e.Options.MaxDelayMS
		if min < 100 || max < min || max > 3000 {
			return errors.New("invalid click delay settings")
		}
		if err = pause(ctx, Delay(min, max)); err != nil {
			return err
		}
		// Capture again after the random delay; a late dialog cannot reuse a stale state.
		img, err = e.image(ctx)
		if err != nil {
			last = err
			continue
		}
		if err = e.Profile.Require(img, before); err != nil {
			last = err
			continue
		}
		x, y, err := Point(r)
		if err != nil {
			return err
		}
		e.log(fmt.Sprintf("%s → %s（尝试 %d/%d）", before, after, attempt+1, e.Options.Retries+1))
		if err = e.Device.Tap(ctx, x, y); err != nil {
			last = err
			continue
		}
		if err = e.wait(ctx, after); err == nil {
			return nil
		}
		last = err
	}
	return fmt.Errorf("%s failed after %d attempts: %w", button, e.Options.Retries+1, last)
}

// Cycle performs only login and return-to-login checks; no task completion is recorded.
// Starting on a game screen logs out first. Each selected role must be verified by
// its own selected-row AND in-game identity anchors before proceeding.
func (e *Engine) Cycle(ctx context.Context, roles []int) (err error) {
	defer func() {
		if err != nil && !errors.Is(err, context.Canceled) && e.Options.OnError != nil {
			e.Options.OnError(e.last, err)
		}
	}()
	if e.Options.Retries < 0 || e.Options.Retries > 20 {
		return errors.New("retries must be 0..20")
	}
	if err = e.Profile.Validate(); err != nil {
		return err
	}
	for _, role := range roles {
		if role < 1 || role > 5 {
			return errors.New("role must be 1..5")
		}
	}
	if len(roles) == 0 {
		return errors.New("no target roles")
	}
	// Preflight all needed templates before the first tap.
	for _, r := range roles {
		for _, key := range []string{fmt.Sprintf("selected_%d", r), fmt.Sprintf("game_%d", r), "roles", "menu", "settings"} {
			if _, ok := e.Profile.Pages[key]; !ok {
				return fmt.Errorf("missing calibration %s", key)
			}
		}
		for _, key := range []string{fmt.Sprintf("role_%d", r), "enter", "menu", "settings", "logout"} {
			if _, ok := e.Profile.Buttons[key]; !ok {
				return fmt.Errorf("missing button %s", key)
			}
		}
	}
	if !e.has("notice") && !e.has("tree") {
		return errors.New("calibrate at least one actual login page")
	}
	for _, r := range roles {
		if err = e.toRoles(ctx); err != nil {
			return err
		}
		selected := fmt.Sprintf("selected_%d", r)
		game := fmt.Sprintf("game_%d", r)
		if err = e.click(ctx, "roles", fmt.Sprintf("role_%d", r), selected); err != nil {
			return err
		}
		if err = e.click(ctx, selected, "enter", game); err != nil {
			return err
		}
		e.log(fmt.Sprintf("第 %d 个角色身份已验证（仅登录测试，任务未完成）", r))
		if err = e.logout(ctx, game); err != nil {
			return err
		}
	}
	return nil
}
func (e *Engine) has(s string) bool { _, ok := e.Profile.Pages[s]; return ok }
func (e *Engine) loginPage(ctx context.Context) (string, error) {
	for _, name := range []string{"notice", "tree", "roles"} {
		if !e.has(name) {
			continue
		}
		img, err := e.image(ctx)
		if err != nil {
			return "", err
		}
		if e.Profile.Require(img, name) == nil {
			return name, nil
		}
	}
	return "", errors.New("no calibrated login page recognized")
}
func (e *Engine) logout(ctx context.Context, game string) error {
	if err := e.click(ctx, game, "menu", "menu"); err != nil {
		return err
	}
	if err := e.click(ctx, "menu", "settings", "settings"); err != nil {
		return err
	}
	// Logout can lead to a confirmation, notice, or tree page; verify the actual one.
	if err := e.clickAny(ctx, "settings", "logout", []string{"logout_confirm", "notice", "tree"}); err != nil {
		return err
	}
	img, err := e.image(ctx)
	if err != nil {
		return err
	}
	if e.Profile.Match(img, "logout_confirm") {
		if err = e.clickAny(ctx, "logout_confirm", "confirm_logout", []string{"notice", "tree"}); err != nil {
			return err
		}
	}
	page, err := e.loginPage(ctx)
	if err != nil {
		return err
	}
	if page != "notice" && page != "tree" {
		return errors.New("logout did not reach a verified login page")
	}
	return nil
}
func (e *Engine) clickAny(ctx context.Context, before, button string, after []string) error {
	var candidates []string
	for _, s := range after {
		if e.has(s) {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 0 {
		return errors.New("no destination page calibrated")
	}
	// Use a virtual page containing no anchors is prohibited. Instead one tap and
	// poll all valid destinations, repeating only if the original page is verified.
	var last error
	for a := 0; a <= e.Options.Retries; a++ {
		img, er := e.image(ctx)
		if er != nil {
			return er
		}
		for _, s := range candidates {
			if e.Profile.Require(img, s) == nil {
				return nil
			}
		}
		if er = e.Profile.Require(img, before); er != nil {
			last = er
			continue
		}
		if er = pause(ctx, Delay(e.Options.MinDelayMS, e.Options.MaxDelayMS)); er != nil {
			return er
		}
		img, er = e.image(ctx)
		if er != nil {
			return er
		}
		if er = e.Profile.Require(img, before); er != nil {
			last = er
			continue
		}
		r, ok := e.Profile.Buttons[button]
		if !ok {
			return fmt.Errorf("missing button %s", button)
		}
		x, y, er := Point(r)
		if er != nil {
			return er
		}
		if er = e.Device.Tap(ctx, x, y); er != nil {
			return er
		}
		timeout := e.Options.StateTimeout
		if timeout <= 0 {
			timeout = 15 * time.Second
		}
		end := time.Now().Add(timeout)
		for time.Now().Before(end) {
			img, er = e.image(ctx)
			if er == nil {
				for _, s := range candidates {
					if e.Profile.Require(img, s) == nil {
						return nil
					}
				}
			}
			if er = pause(ctx, 350*time.Millisecond); er != nil {
				return er
			}
		}
		last = errors.New("logout transition was not verified")
	}
	return last
}
func (e *Engine) toRoles(ctx context.Context) error {
	unknown := 0
	for step := 0; step < 8; step++ {
		img, err := e.image(ctx)
		if err != nil {
			return err
		}
		if e.Profile.Require(img, "roles") == nil {
			return nil
		}
		found := false
		for n := 1; n <= 5; n++ {
			g := fmt.Sprintf("game_%d", n)
			if e.Profile.Require(img, g) == nil {
				if err = e.logout(ctx, g); err != nil {
					return err
				}
				found = true
				break
			}
		}
		if found {
			continue
		}
		if e.Profile.Match(img, "logout_confirm") {
			if err = e.clickAny(ctx, "logout_confirm", "confirm_logout", []string{"notice", "tree"}); err != nil {
				return err
			}
			continue
		}
		if e.Profile.Match(img, "notice") {
			if err = e.clickAny(ctx, "notice", "notice_enter", []string{"tree", "roles"}); err != nil {
				return err
			}
			continue
		}
		if e.Profile.Match(img, "tree") {
			if err = e.click(ctx, "tree", "tree_enter", "roles"); err != nil {
				return err
			}
			continue
		}
		if unknown >= e.Options.Retries {
			return errors.New("unknown page: retry limit reached; no click performed")
		}
		unknown++
		e.log(fmt.Sprintf("未知页面，等待重试 %d/%d，不点击", unknown, e.Options.Retries))
		if err = pause(ctx, 500*time.Millisecond); err != nil {
			return err
		}
		step--
	}
	return errors.New("login transition limit exceeded")
}

// Gate keys include the real instance, server, character and task.
// Retry exhaustion is recorded as skipped, never as completed.
type Progress struct {
	Entries map[string]string `json:"entries"`
}

func Key(instance int, server string, role int, task string) string {
	return fmt.Sprintf("%d|%s|%d|%s", instance, server, role, task)
}
func (p Progress) CanChangeServer(instances []int, server string, tasks []string) bool {
	if len(instances) == 0 || server == "" || len(tasks) == 0 {
		return false
	}
	for _, i := range instances {
		for r := 1; r <= 5; r++ {
			for _, t := range tasks {
				if t == "" || p.Entries[Key(i, server, r, t)] != "verified_complete" {
					return false
				}
			}
		}
	}
	return true
}

// Cache is bounded in memory. Normal frames are never written to disk.
type Cache struct {
	mu        sync.Mutex
	frames    map[string][][]byte
	perDevice int
	maxBytes  int
	bytes     int
}

func NewCache(perDevice, maxBytes int) *Cache {
	if perDevice < 1 {
		perDevice = 1
	}
	if perDevice > 3 {
		perDevice = 3
	}
	if maxBytes < 8<<20 {
		maxBytes = 8 << 20
	}
	return &Cache{frames: map[string][][]byte{}, perDevice: perDevice, maxBytes: maxBytes}
}
func (c *Cache) Add(key string, b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(b) > c.maxBytes {
		return
	}
	q := c.frames[key]
	for len(q) >= c.perDevice {
		c.bytes -= len(q[0])
		q = q[1:]
	}
	for c.bytes+len(b) > c.maxBytes {
		removed := false
		for k, v := range c.frames {
			if k != key && len(v) > 0 {
				c.bytes -= len(v[0])
				c.frames[k] = v[1:]
				removed = true
				break
			}
		}
		if !removed {
			if len(q) == 0 {
				break
			}
			c.bytes -= len(q[0])
			q = q[1:]
		}
	}
	copyB := append([]byte(nil), b...)
	c.frames[key] = append(q, copyB)
	c.bytes += len(b)
}
func (c *Cache) Size() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, q := range c.frames {
		n += len(q)
	}
	return n, c.bytes
}
