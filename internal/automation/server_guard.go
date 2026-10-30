package automation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

var ErrServerUnverified = errors.New("本次未观察到已标定的区服画面；停留当前页面，不猜测区服或自动换服")

// ServerGuard is local to one login attempt. A previous run's evidence is never
// reused. Only the visible, named login-server label can establish the scope.
// Profiles for role identity remain the operator's existing local calibrations.
type ServerGuard struct {
	Device          Device
	Profile         Profile
	ServerPage      Page
	Verified        bool
	ServerScreenSHA string
	treeFrames      int
	lastState       string
}

func (d *ServerGuard) Capture(ctx context.Context) ([]byte, error) {
	b, e := d.Device.Capture(ctx)
	if e != nil {
		return nil, e
	}
	img, e := Decode(b)
	if e != nil {
		return nil, e
	}
	state, recognition := d.Profile.Recognize(img)
	before := d.lastState
	d.lastState = state
	if recognition != nil {
		d.treeFrames = 0
		return b, nil
	} // the engine handles unknown/ambiguous frames
	switch {
	case state == "notice":
		d.Verified = false
		d.treeFrames = 0
		d.ServerScreenSHA = ""
	case state == "tree":
		if before != "tree" {
			d.Verified = false
			d.ServerScreenSHA = ""
			d.treeFrames = 0
		}
		p := NewProfile()
		p.Pages["expected_server"] = d.ServerPage
		if !p.Match(img, "expected_server") {
			d.Verified = false
			d.ServerScreenSHA = ""
			return nil, errors.New("登录区服文字特征与目标不符，未点击踏入仙途")
		}
		d.treeFrames++
		if d.treeFrames >= 2 {
			d.Verified = true
			h := sha256.Sum256(b)
			d.ServerScreenSHA = hex.EncodeToString(h[:])
		}
	case state == "roles" || strings.HasPrefix(state, "selected_"):
		if !d.Verified {
			return nil, ErrServerUnverified
		}
	}
	return b, nil
}
func (d *ServerGuard) Tap(ctx context.Context, x, y int) error {
	before := d.lastState
	if before == "" {
		return errors.New("点击前页面未确认")
	}
	if _, e := d.Capture(ctx); e != nil {
		return e
	}
	if before != d.lastState {
		return errors.New("提交点击前页面已变化，未点击")
	}
	if (before == "tree" || before == "roles" || strings.HasPrefix(before, "selected_")) && !d.Verified {
		return ErrServerUnverified
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := d.Device.Tap(ctx, x, y); e != nil {
		return fmt.Errorf("登录点击：%w", e)
	}
	return nil
}
