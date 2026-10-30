package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// RevealServer uses the handoff's role-list back arrow, not logout or restart.
// It only runs on a twice verified role list; being in-game is never a reason
// to manufacture provenance by forcing the current character to log out.
func (g *ServerGuard) RevealServer(ctx context.Context, back Rect, minDelay, maxDelay int) error {
	if !back.Valid() || minDelay < 100 || maxDelay < minDelay || maxDelay > 3000 {
		return errors.New("返回区服页按钮或随机延迟无效")
	}
	if _, ok := g.Profile.Pages["tree"]; !ok {
		return errors.New("缺少返回后的登录页识别，不点击返回")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var state string
	for i := 0; i < 2; i++ {
		b, e := g.Device.Capture(ctx)
		if e != nil {
			return e
		}
		img, e := Decode(b)
		if e != nil {
			return e
		}
		now, e := g.Profile.Recognize(img)
		if e != nil {
			return e
		}
		if now != "roles" && !strings.HasPrefix(now, "selected_") {
			return errors.New("不是已确认角色列表，不点击返回、不退出当前角色")
		}
		if i > 0 && now != state {
			return errors.New("返回前角色列表发生变化，未点击")
		}
		state = now
		if i == 0 {
			if e = pause(ctx, Delay(minDelay, maxDelay)); e != nil {
				return e
			}
		}
	}
	x, y, e := Point(back)
	if e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if e = g.Device.Tap(ctx, x, y); e != nil {
		return e
	}
	g.Verified = false
	g.treeFrames = 0
	g.ServerScreenSHA = ""
	g.lastState = ""
	for {
		_, e = g.Capture(ctx)
		if e != nil && !errors.Is(e, ErrServerUnverified) {
			return e
		}
		if e == nil && g.Verified && g.lastState == "tree" {
			return nil
		}
		if e = pause(ctx, 250*time.Millisecond); e != nil {
			return fmt.Errorf("返回后未核验区服，不再点击：%w", e)
		}
	}
}
