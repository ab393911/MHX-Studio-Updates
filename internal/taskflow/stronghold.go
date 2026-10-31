package taskflow

import (
	"context"
	"errors"
	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/vision"
	"time"
)

func strongholdPage(s snapshot) error {
	if e := title("妖灵据点")(s); e != nil {
		return e
	}
	if !has(s, "挑战方式", automation.Rect{X: 590, Y: 405, W: 115, H: 40}) || !has(s, "前往挑战", automation.Rect{X: 775, Y: 450, W: 175, H: 50}) {
		return errors.New("据点挑战方式和入口未核验")
	}
	return nil
}
func (c *controller) inspectStronghold(ctx context.Context) (string, string, error) {
	if e := c.openDaily(ctx); e != nil {
		return "", "", e
	}
	// Screenshot 107: vertical 妖灵据点 tile. Never substitute a text match
	// against the entire screen for this bounded, page-verified navigation.
	if _, e := c.tap(ctx, dailyPage, fixedButton(automation.Rect{X: 715, Y: 180, W: 80, H: 130}), strongholdPage); e != nil {
		return "", "", e
	}
	seasonArea := automation.Rect{X: 0, Y: 60, W: 125, H: 460}
	found := false
	for i := 0; i < 6; i++ {
		s, e := c.take(ctx)
		if e != nil {
			return "", "", e
		}
		if e = strongholdPage(s); e != nil {
			return "", "", e
		}
		if _, e = s.ocr.Unique("赛季", seasonArea); e == nil {
			found = true
			break
		}
		if i == 5 {
			break
		}
		d, ok := c.Device.(interface {
			Swipe(context.Context, int, int, int, int, time.Duration) error
		})
		if !ok {
			return "", "", errors.New("当前设备不支持据点分类滚动")
		}
		if e = wait(ctx, automation.Delay(c.MinDelayMS, c.MaxDelayMS)); e != nil {
			return "", "", e
		}
		s, e = c.take(ctx)
		if e != nil {
			return "", "", e
		}
		if e = strongholdPage(s); e != nil {
			return "", "", e
		}
		if e = d.Swipe(ctx, 75, 425, 75, 145, 450*time.Millisecond); e != nil {
			return "", "", e
		}
		if e = wait(ctx, 350*time.Millisecond); e != nil {
			return "", "", e
		}
	}
	if !found {
		return "", "", errors.New("据点分类未找到赛季，没有误用一阶首领")
	}
	var boss, previous string
	selected := func(s snapshot) error {
		if e := strongholdPage(s); e != nil {
			return e
		}
		name := vision.Normal(s.ocr.Text(automation.Rect{X: 580, Y: 65, W: 260, H: 40}))
		if name == "" {
			return errors.New("本周赛季首领名称未读清")
		}
		if name != previous {
			previous = name
			return errors.New("等待本周首领名称稳定")
		}
		boss = name
		return nil
	}
	s, e := c.tap(ctx, strongholdPage, textButton("赛季", seasonArea), selected)
	if e != nil {
		return "", "", e
	}
	proof := s.sha
	// The reviewed handoff still lacks the complete battle target/pet/fallback
	// bindings. Inspect the week and return; do not press 前往挑战 or 组队挑战.
	if _, e = c.tap(ctx, strongholdPage, fixedButton(automation.Rect{X: 25, Y: 8, W: 30, H: 26}), dailyPage); e != nil {
		return boss, proof, e
	}
	if e = c.closeDaily(ctx); e != nil {
		return boss, proof, e
	}
	return boss, proof, nil
}
