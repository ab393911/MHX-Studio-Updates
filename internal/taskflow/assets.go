package taskflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
	"github.com/ab393911/MHX-Studio-Updates/internal/vision"
)

// AssetReading retains the displayed precision. Stored amounts are rounded only
// after the entire OCR interval agrees on the same per-character whole unit.
type AssetReading struct {
	Instance     int           `json:"instance"`
	Server       string        `json:"server"`
	Slot         int           `json:"slot"`
	Silver       vision.Amount `json:"silver"`
	Merit        vision.Amount `json:"merit"`
	Available    int64         `json:"available"`
	Evidence     string        `json:"evidence"`
	ObservedAt   time.Time     `json:"observed_at"`
	ReturnedMain bool          `json:"returned_main"`
}

func giftPage(s snapshot) error {
	if !has(s, "赠送礼物", automation.Rect{X: 150, Y: 40, W: 200, H: 50}) || !has(s, "银锭", automation.Rect{X: 680, Y: 94, W: 110, H: 50}) || !has(s, "鲜花", automation.Rect{X: 410, Y: 94, W: 130, H: 50}) {
		return errors.New("礼物查询页标题和独立标签未核验")
	}
	return nil
}
func friendPanel(s snapshot) error {
	if !has(s, "好友", automation.Rect{X: 0, Y: 135, W: 110, H: 60}) || !has(s, "邮箱", automation.Rect{X: 0, Y: 190, W: 110, H: 60}) {
		return errors.New("好友侧栏未核验")
	}
	return nil
}
func friendsPage(s snapshot) error {
	if e := friendPanel(s); e != nil {
		return e
	}
	if !has(s, "好友上限", automation.Rect{X: 120, Y: 475, W: 230, H: 43}) || !has(s, "群聊", automation.Rect{X: 330, Y: 10, W: 175, H: 45}) {
		return errors.New("不是好友列表，不选择联系人")
	}
	return nil
}
func chatPage(s snapshot) error {
	if !has(s, "以上为历史消息", automation.Rect{X: 215, Y: 190, W: 270, H: 45}) && !has(s, "系统提示", automation.Rect{X: 120, Y: 43, W: 380, H: 40}) {
		return errors.New("好友聊天页未核验")
	}
	if !has(s, "发给", automation.Rect{X: 180, Y: 480, W: 160, H: 44}) {
		return errors.New("聊天输入栏未核验")
	}
	return nil
}

// A helper row is rejected using the entire row, including the vertical 助/手
// badge. A contact's nickname is not calibrated or used as role identity.
func normalFriend(s snapshot) (automation.Rect, error) {
	if e := friendsPage(s); e != nil {
		return automation.Rect{}, e
	}
	for i := 0; i < 5; i++ {
		r := automation.Rect{X: 130, Y: 60 + i*90, W: 350, H: 90}
		if r.Y+r.H > 475 {
			r.H = 475 - r.Y
		}
		if r.H < 20 {
			continue
		}
		text := vision.Normal(s.ocr.Text(r))
		if text == "" || strings.Contains(text, "助手") || strings.Contains(text, "助") || strings.Contains(text, "手") {
			continue
		}
		// Require a name in the actual name column, not just an avatar level.
		if vision.Normal(s.ocr.Text(automation.Rect{X: 220, Y: r.Y + 15, W: 240, H: min(45, r.H-15)})) == "" {
			continue
		}
		return automation.Rect{X: 240, Y: r.Y + 20, W: 160, H: 30}, nil
	}
	return automation.Rect{}, errors.New("当前可见好友均为助手或未读清，没有选择联系人")
}
func (c *controller) openGift(ctx context.Context) error {
	s, e := c.take(ctx)
	if e != nil {
		return e
	}
	if giftPage(s) == nil {
		return nil
	}
	if c.game(s) == nil {
		// Screenshot 146: contacts icon, below chat; never the send-chat field.
		_, e = c.tap(ctx, c.game, fixedButton(automation.Rect{X: 350, Y: 480, W: 25, H: 25}), friendPanel)
		if e != nil {
			return e
		}
		s, e = c.take(ctx)
		if e != nil {
			return e
		}
	}
	if friendPanel(s) == nil {
		if friendsPage(s) != nil {
			if _, e = c.tap(ctx, friendPanel, textButton("好友", automation.Rect{X: 0, Y: 135, W: 110, H: 60}), friendsPage); e != nil {
				return e
			}
		}
		if _, e = c.tap(ctx, friendsPage, normalFriend, chatPage); e != nil {
			return e
		}
	} else if chatPage(s) != nil {
		return errors.New("余额查询起始页面未知")
	}
	// Screenshot 85: gift icon; only opens the read-only query page.
	_, e = c.tap(ctx, chatPage, fixedButton(automation.Rect{X: 493, Y: 14, W: 24, H: 27}), giftPage)
	return e
}
func assetAmounts(v []vision.Amount) (silver, merit, available int64, err error) {
	if len(v) != 2 {
		err = errors.New("银锭/功绩字段缺失")
		return
	}
	if silver, err = v[0].RoundDown(10000); err != nil {
		return
	}
	if merit, err = v[1].RoundDown(100); err != nil {
		return
	}
	available, err = studio.Capacity(silver, merit)
	return
}

var assetAreas = []automation.Rect{{X: 758, Y: 6, W: 56, H: 29}, {X: 881, Y: 6, W: 43, H: 29}}

func (c *controller) readAssets(ctx context.Context) (reading AssetReading, err error) {
	reading.Instance, reading.Server, reading.Slot = c.Instance, c.Server, c.Role
	if err = c.openGift(ctx); err != nil {
		return
	}
	var amounts []vision.Amount
	var prior string
	stable := func(s snapshot) error {
		if e := giftPage(s); e != nil {
			return e
		}
		v, e := vision.ReadAmounts(ctx, c.Reader, s.png, assetAreas)
		if e != nil {
			return e
		}
		if _, _, _, e = assetAmounts(v); e != nil {
			return e
		}
		key := fmt.Sprintf("%+v", v)
		if key != prior {
			prior = key
			return errors.New("等待两次银锭/功绩读数一致")
		}
		amounts = v
		return nil
	}
	s, e := c.await(ctx, stable, 15*time.Second)
	if e != nil {
		return reading, e
	}
	reading.Silver, reading.Merit = amounts[0], amounts[1]
	_, _, reading.Available, err = assetAmounts(amounts)
	if err != nil {
		return
	}
	reading.Evidence, reading.ObservedAt = s.sha, s.at
	if _, err = c.tap(ctx, giftPage, fixedButton(automation.Rect{X: 781, Y: 56, W: 22, H: 22}), chatPage); err != nil {
		return
	}
	if _, err = c.tap(ctx, chatPage, fixedButton(automation.Rect{X: 45, Y: 22, W: 28, H: 23}), friendPanel); err != nil {
		return
	}
	if _, err = c.tap(ctx, friendPanel, fixedButton(automation.Rect{X: 552, Y: 355, W: 30, H: 28}), c.game); err != nil {
		return
	}
	reading.ReturnedMain = true
	return
}
func (r AssetReading) Snapshot() (studio.CharacterAsset, error) {
	silver, merit, available, e := assetAmounts([]vision.Amount{r.Silver, r.Merit})
	if e != nil {
		return studio.CharacterAsset{}, e
	}
	if !r.ReturnedMain || available != r.Available {
		return studio.CharacterAsset{}, errors.New("资产读取或返回主界面未完成")
	}
	return studio.CharacterAsset{Instance: r.Instance, Slot: r.Slot, Server: r.Server, Silver: silver, Merit: merit, Evidence: r.Evidence, ObservedAt: r.ObservedAt}, nil
}
func ReadAssets(ctx context.Context, actor Actor) (r AssetReading, err error) {
	if actor.Device == nil || actor.Reader == nil || actor.Instance < 0 || actor.Identity == "" || actor.Server == "" || actor.Role < 1 || actor.Role > 5 {
		return r, errors.New("资产读取缺少本次登录角色依据")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	c := &controller{Actor: actor}
	defer func() {
		if err != nil && c.OnError != nil && ctx.Err() != context.Canceled {
			c.OnError(c.last, err)
		}
	}()
	return c.readAssets(ctx)
}
