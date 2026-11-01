package ldplayer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func transientADB(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	if (strings.Contains(s, "device '") && strings.Contains(s, "' not found")) || (strings.Contains(s, "device \"") && strings.Contains(s, "\" not found")) {
		return true
	}
	for _, marker := range []string{"offline", "device not found", "no devices", "device still authorizing", "transport error", "connection reset", "connection refused", "closed"} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

func pauseRecovery(ctx context.Context) error {
	t := time.NewTimer(500 * time.Millisecond)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

var ErrActionNeedsRecheck = errors.New("ADB已恢复；原截图/前台依据可能过期，停止本次动作，需重新检查页面后再操作")

// Verify retries transport checks only. It never replays a click or launch and
// never retries identity changes. Reconnect is scoped to this serial, not the server.
func (c Client) Verify(ctx context.Context, d Instance) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.actionRecovery != nil && c.actionRecovery.Load() {
		return ErrActionNeedsRecheck
	}
	ctx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	started := time.Now()
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		last = c.verifyOnce(ctx, d)
		if last == nil && attempt > 0 {
			last = c.verifyIndexIdentity(ctx, d)
			if last == nil {
				last = c.verifyOnce(ctx, d)
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if last == nil {
			if attempt > 0 && c.actionRecovery != nil {
				c.actionRecovery.Store(true)
			}
			if attempt > 0 && c.OnConnection != nil {
				c.OnConnection(fmt.Sprintf("编号%d ADB恢复于%s；开始%s；重新核验实例、Android及boot身份", d.Index, time.Now().Format(time.RFC3339), started.Format(time.RFC3339)))
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if c.actionRecovery != nil && c.actionRecovery.Load() {
				return ErrActionNeedsRecheck
			}
			return nil
		}
		if transientADB(last) && c.actionRecovery != nil {
			c.actionRecovery.Store(true)
		}
		if !transientADB(last) || attempt == 2 {
			return last
		}
		if d.BootID == "" {
			return fmt.Errorf("ADB传输故障且缺少boot身份，禁止自动恢复操作；请重新检测：%w", last)
		}
		if attempt == 0 && c.OnConnection != nil {
			c.OnConnection(fmt.Sprintf("编号%d ADB短暂故障开始%s：%v；暂停操作并限次重试", d.Index, started.Format(time.RFC3339), last))
		}
		if c.ADB != "" {
			_, _ = c.adb(ctx, d.Serial, "reconnect")
		}
		if err := pauseRecovery(ctx); err != nil {
			return err
		}
	}
	return last
}

// RecoverInstance performs fresh inventory/identity mapping instead of using a
// stale serial or a guessed port. Other instances are never restarted.
func (c Client) RecoverInstance(ctx context.Context, index int) (Instance, error) {
	ctx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		all, err := c.Discover(ctx)
		last = err
		for _, d := range all {
			if d.Index != index || d.Unmapped {
				continue
			}
			if d.Serial != "" && d.BootID != "" {
				if err = c.Verify(ctx, d); err == nil {
					return d, nil
				}
				last = err
			} else if d.Serial != "" {
				last = fmt.Errorf("编号%d缺少boot身份，禁止自动恢复操作", index)
			} else {
				last = fmt.Errorf("编号%d ADB未就绪：%s", index, d.ConnectionError)
			}
		}
		if ctx.Err() != nil {
			return Instance{}, ctx.Err()
		}
		if attempt < 2 {
			if err = pauseRecovery(ctx); err != nil {
				return Instance{}, err
			}
		}
	}
	return Instance{}, fmt.Errorf("编号%d当前未核验在线（可能启动中、扫描延迟或离线），未执行游戏操作：%v", index, last)
}

// Some vendor consoles return exit status zero with an ADB transport error on stdout.
func adbOutputError(b []byte) error {
	text := strings.TrimSpace(string(b))
	if transientADB(fmt.Errorf("%s", text)) {
		return fmt.Errorf("ADB transport: %s", text)
	}
	return nil
}
