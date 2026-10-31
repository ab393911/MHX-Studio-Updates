package taskflow

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/vision"
)

type colorPatch struct {
	Rect      automation.Rect
	Pixels    []byte
	SourceID  int
	SourceSHA string
}

//go:embed purchase_currency.json
var goldPatchJSON []byte

func goldPurchaseCurrency(s snapshot) bool {
	var p colorPatch
	if json.Unmarshal(goldPatchJSON, &p) != nil || len(p.Pixels) != p.Rect.W*p.Rect.H*3 {
		return false
	}
	sum, bad := 0, 0
	for y := 0; y < p.Rect.H; y++ {
		for x := 0; x < p.Rect.W; x++ {
			r, g, b, _ := s.image.At(p.Rect.X+x, p.Rect.Y+y).RGBA()
			i := (y*p.Rect.W + x) * 3
			delta := abs(int(r>>8)-int(p.Pixels[i])) + abs(int(g>>8)-int(p.Pixels[i+1])) + abs(int(b>>8)-int(p.Pixels[i+2]))
			sum += delta
			if delta > 100 {
				bad++
			}
		}
	}
	return sum/(p.Rect.W*p.Rect.H*3) <= 18 && bad < 25
}
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
func getWays(target string) check {
	return func(s snapshot) error {
		if !has(s, "获取途径", automation.Rect{X: 300, Y: 28, W: 155, H: 45}) || !has(s, target+"幻形符", automation.Rect{X: 330, Y: 190, W: 310, H: 40}) || !has(s, "商行购买", automation.Rect{X: 340, Y: 240, W: 165, H: 45}) {
			return errors.New("幻形获取途径与目标不符")
		}
		return nil
	}
}
func shopTarget(target string) check {
	return func(s snapshot) error {
		if !has(s, "购买", automation.Rect{X: 165, Y: 45, W: 120, H: 45}) || !has(s, "交易列表", automation.Rect{X: 285, Y: 90, W: 150, H: 35}) || !has(s, target+"幻形符", automation.Rect{X: 625, Y: 100, W: 150, H: 60}) {
			return errors.New("购买页面或目标幻形符不符，未购买")
		}
		return nil
	}
}
func rewardPopup(s snapshot) error {
	if !has(s, "恭喜获得", automation.Rect{X: 330, Y: 70, W: 310, H: 100}) || !has(s, "点击空白处关闭", automation.Rect{X: 290, Y: 470, W: 400, H: 70}) {
		return errors.New("不是已确认获得物品提示")
	}
	return nil
}
func (c *controller) shopGrade(ctx context.Context, s snapshot) error {
	v, e := vision.ReadFields(ctx, c.Reader, s.png, []automation.Rect{{X: 630, Y: 173, W: 42, H: 28}})
	if e != nil {
		return e
	}
	if !regexp.MustCompile(`^8[★星*]?$`).MatchString(vision.Normal(v[0])) {
		return errors.New("交易项目不是已核验8★，不购买")
	}
	return nil
}

type shopValues struct {
	quantity, price, owned int64
	balance                vision.Amount
}

func (c *controller) shopRead(ctx context.Context, s snapshot) (shopValues, error) {
	var out shopValues
	v, e := vision.ReadAmounts(ctx, c.Reader, s.png, []automation.Rect{{X: 635, Y: 338, W: 32, H: 25}, {X: 641, Y: 381, W: 112, H: 28}, {X: 874, Y: 7, W: 63, H: 28}, {X: 750, Y: 310, W: 25, H: 28}})
	if e != nil {
		return out, e
	}
	out.quantity, e = v[0].ExactInteger()
	if e != nil {
		return out, e
	}
	out.price, e = v[1].ExactInteger()
	if e != nil {
		return out, e
	}
	out.owned, e = v[3].ExactInteger()
	out.balance = v[2]
	if e != nil || out.quantity <= 0 || out.price <= 0 {
		return out, errors.New("购买数量、总价或拥有数量未读清")
	}
	return out, nil
}
func (c *controller) closeShopToPhantom(ctx context.Context, target string) error {
	for step := 0; step < 3; step++ {
		s, e := c.take(ctx)
		if e != nil {
			return e
		}
		if phantomTarget(target)(s) == nil {
			return nil
		}
		after := func(s snapshot) error {
			if phantomTarget(target)(s) == nil || getWays(target)(s) == nil || c.game(s) == nil {
				return nil
			}
			return errors.New("关闭购买窗口后页面未知")
		}
		if shopTarget(target)(s) == nil {
			if _, e = c.tap(ctx, shopTarget(target), fixedButton(automation.Rect{X: 778, Y: 51, W: 28, H: 28}), after); e != nil {
				return e
			}
			continue
		}
		if getWays(target)(s) == nil {
			if _, e = c.tap(ctx, getWays(target), fixedButton(automation.Rect{X: 642, Y: 32, W: 26, H: 26}), phantomTarget(target)); e != nil {
				return e
			}
			continue
		}
		if c.game(s) == nil {
			if e = c.openPhantom(ctx); e != nil {
				return e
			}
			return c.selectPhantom(ctx, target, phantomCategory(c.Profession))
		}
		return errors.New("购买后未确认已知返回页面，不盲点关闭")
	}
	return errors.New("购买后返回幻形超过安全步数")
}
func phantomCategory(job string) string {
	if job == "合欢" || job == "天音" {
		return "辅助"
	}
	return "进攻"
}
func (c *controller) buyPhantom(ctx context.Context, target string) error {
	if _, e := c.tap(ctx, phantomTarget(target), textButton("获取", automation.Rect{X: 650, Y: 450, W: 150, H: 58}), getWays(target)); e != nil {
		return e
	}
	if _, e := c.tap(ctx, getWays(target), textButton("前往", automation.Rect{X: 555, Y: 235, W: 105, H: 55}), shopTarget(target)); e != nil {
		return e
	}
	s, e := c.take(ctx)
	if e != nil {
		return e
	}
	if c.shopGrade(ctx, s) != nil {
		found := false
		for row := 0; row < 4 && !found; row++ {
			for col := 0; col < 2; col++ {
				if _, e = c.tap(ctx, shopTarget(target), fixedButton(automation.Rect{X: 165 + col*177, Y: 139 + row*79, W: 155, H: 62}), shopTarget(target)); e != nil {
					return e
				}
				s, e = c.take(ctx)
				if e != nil {
					return e
				}
				if c.shopGrade(ctx, s) == nil {
					found = true
					break
				}
			}
		}
		if !found {
			return errors.New("未在已见交易项中核验8★，不购买默认3★项目")
		}
	}
	var values shopValues
	for changes := 0; changes <= 20; changes++ {
		s, e = c.take(ctx)
		if e != nil {
			return e
		}
		values, e = c.shopRead(ctx, s)
		if e != nil {
			return e
		}
		if values.quantity == 1 {
			break
		}
		if changes == 20 {
			return errors.New("购买数量无法安全调为1")
		}
		previous := values.quantity
		before := func(s snapshot) error {
			if e := shopTarget(target)(s); e != nil {
				return e
			}
			if e := c.shopGrade(ctx, s); e != nil {
				return e
			}
			v, e := c.shopRead(ctx, s)
			if e != nil {
				return e
			}
			if v.quantity != previous {
				return errors.New("购买数量变化")
			}
			return nil
		}
		after := func(s snapshot) error {
			if e := shopTarget(target)(s); e != nil {
				return e
			}
			v, e := c.shopRead(ctx, s)
			if e != nil {
				return e
			}
			if v.quantity != previous-1 {
				return errors.New("数量减1未核验")
			}
			return nil
		}
		if _, e = c.tap(ctx, before, fixedButton(automation.Rect{X: 585, Y: 335, W: 31, H: 31}), after); e != nil {
			return e
		}
	}
	if values.owned > 0 {
		return errors.New("商行显示已拥有目标卡，先核对库存，不重复购买")
	}
	if values.balance.Upper < values.price {
		if e = c.closeShopToPhantom(ctx, target); e != nil {
			return e
		}
		return ErrResourceInsufficient
	}
	if values.balance.Lower < values.price {
		return errors.New("铜钱万缩写精度不足以确认购买余额，不消费")
	}
	before := func(s snapshot) error {
		if e := shopTarget(target)(s); e != nil {
			return e
		}
		if e := c.shopGrade(ctx, s); e != nil {
			return e
		}
		if !goldPurchaseCurrency(s) {
			return errors.New("购买币种图标不是已核验铜钱")
		}
		v, e := c.shopRead(ctx, s)
		if e != nil {
			return e
		}
		if v.quantity != 1 || v.price != values.price || v.owned != 0 || v.balance.Lower < v.price {
			return errors.New("购买前价格、数量、库存或余额变化，未消费")
		}
		return nil
	}
	// Never click “自动购买”. One explicit purchase only; an ambiguous result
	// leaves the phase for review rather than buying another card.
	if _, e = c.press(ctx, before, textButton("购买", automation.Rect{X: 670, Y: 430, W: 140, H: 52})); e != nil {
		return e
	}
	resultCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	stable := 0
	for {
		s, e = c.take(resultCtx)
		if e != nil {
			return e
		}
		if rewardPopup(s) == nil {
			if _, e = c.tap(resultCtx, rewardPopup, fixedButton(automation.Rect{X: 440, Y: 478, W: 80, H: 30}), shopTarget(target)); e != nil {
				return e
			}
			stable = 0
			continue
		}
		if shopTarget(target)(s) == nil {
			v, readErr := c.shopRead(resultCtx, s)
			if readErr == nil && v.quantity == 1 && v.owned == 1 {
				stable++
			} else {
				stable = 0
			}
			if stable >= 2 {
				break
			}
		}
		if e = wait(resultCtx, 300*time.Millisecond); e != nil {
			return errors.New("购买结果不明，不重新购买；保留待核查")
		}
	}
	return c.closeShopToPhantom(ctx, target)
}
