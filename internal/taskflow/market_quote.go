package taskflow

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
	"github.com/ab393911/MHX-Studio-Updates/internal/vision"
)

// MarketQuote is an observation, never permission to list an item. The selling
// controller must separately verify item identity, exclusion, stock, legal price
// step, minimum, final fields and the actual result before recording success.
type MarketQuote struct {
	Item                                 string
	Lowest, CurrentUnit, Quantity, Owned int64
	OfferPrices                          []int64
	Currency                             string
	ScreenSHA                            string
	ObservedAt                           time.Time
}

//go:embed market_silver.json
var marketSilverJSON []byte

func listingPage(s snapshot) error {
	if !has(s, "上架", automation.Rect{X: 155, Y: 40, W: 160, H: 50}) || !has(s, "交易列表", automation.Rect{X: 285, Y: 85, W: 165, H: 45}) || !has(s, "单价", automation.Rect{X: 525, Y: 335, W: 55, H: 38}) || !has(s, "数量", automation.Rect{X: 525, Y: 285, W: 55, H: 35}) || !has(s, "上架3天", automation.Rect{X: 590, Y: 420, W: 140, H: 45}) {
		return errors.New("上架页标题、单价/数量与3天按钮未核验")
	}
	return nil
}
func marketSilver(s snapshot) bool {
	var p colorPatch
	if json.Unmarshal(marketSilverJSON, &p) != nil || p.Rect.W <= 0 || p.Rect.H <= 0 || len(p.Pixels) != p.Rect.W*p.Rect.H*3 {
		return false
	}
	sum, bad := 0, 0
	for y := 0; y < p.Rect.H; y++ {
		for x := 0; x < p.Rect.W; x++ {
			r, g, b, _ := s.image.At(p.Rect.X+x, p.Rect.Y+y).RGBA()
			i := (y*p.Rect.W + x) * 3
			d := abs(int(r>>8)-int(p.Pixels[i])) + abs(int(g>>8)-int(p.Pixels[i+1])) + abs(int(b>>8)-int(p.Pixels[i+2]))
			sum += d
			if d > 100 {
				bad++
			}
		}
	}
	return sum/(p.Rect.W*p.Rect.H*3) <= 18 && bad < (p.Rect.W*p.Rect.H)/10
}
func quoteAreas() []automation.Rect {
	// Left side: WHITE unit-price row under the GREEN 在售 quantity. These
	// regions exclude the coin and the stock count above it (source image110).
	var a []automation.Rect
	for row := 0; row < 3; row++ {
		for col := 0; col < 2; col++ {
			a = append(a, automation.Rect{X: 263 + col*179, Y: 178 + row*86, W: 58, H: 26})
		}
	}
	return append(a, automation.Rect{X: 630, Y: 337, W: 52, H: 28}, automation.Rect{X: 624, Y: 285, W: 46, H: 30}, automation.Rect{X: 753, Y: 257, W: 50, H: 26})
}
func (c *controller) readMarketQuote(ctx context.Context, s snapshot) (MarketQuote, error) {
	out := MarketQuote{Currency: "silver_ingot", ScreenSHA: s.sha, ObservedAt: s.at}
	if e := listingPage(s); e != nil {
		return out, e
	}
	name := strings.Split(s.ocr.Text(automation.Rect{X: 610, Y: 102, W: 150, H: 39}), "\n")[0]
	out.Item = vision.Normal(name)
	if out.Item == "" || len([]rune(out.Item)) > 32 {
		return out, errors.New("当前上架物品名未读清")
	}
	if !marketSilver(s) {
		return out, errors.New("单价旁币种不是截图核验的蓝白银锭；不读取为卖价")
	}
	areas := quoteAreas()
	var numbers []int64
	for begin := 0; begin < len(areas); begin += 4 {
		v, e := vision.ReadAmounts(ctx, c.Reader, s.png, areas[begin:min(begin+4, len(areas))])
		if e != nil {
			return out, e
		}
		for _, amount := range v {
			n, e := amount.ExactInteger()
			if e != nil {
				return out, e
			}
			numbers = append(numbers, n)
		}
	}
	if len(numbers) != 9 {
		return out, errors.New("交易字段不完整")
	}
	out.OfferPrices = append([]int64(nil), numbers[:6]...)
	for _, n := range out.OfferPrices {
		if n <= 0 {
			return out, errors.New("可见市场单价缺失或为零，不能据此压价")
		}
		if out.Lowest == 0 || n < out.Lowest {
			out.Lowest = n
		}
	}
	out.CurrentUnit, out.Quantity, out.Owned = numbers[6], numbers[7], numbers[8]
	if out.CurrentUnit <= 0 || out.Quantity <= 0 || out.Owned <= 0 || out.Quantity > out.Owned {
		return out, errors.New("上架单价、数量或库存无效")
	}
	return out, nil
}

// inspectMarketQuote requires two fresh observations, including all individual
// offer prices. A changed listing book or stock does not authorize a sale.
func (c *controller) inspectMarketQuote(ctx context.Context) (MarketQuote, error) {
	first, e := c.take(ctx)
	if e != nil {
		return MarketQuote{}, e
	}
	a, e := c.readMarketQuote(ctx, first)
	if e != nil {
		return a, e
	}
	if e = wait(ctx, 250*time.Millisecond); e != nil {
		return a, e
	}
	second, e := c.take(ctx)
	if e != nil {
		return a, e
	}
	b, e := c.readMarketQuote(ctx, second)
	if e != nil {
		return b, e
	}
	if a.Item != b.Item {
		return b, errors.New("两次报价物品不同，不生成报价")
	}
	if a.CurrentUnit != b.CurrentUnit || a.Quantity != b.Quantity || a.Owned != b.Owned {
		return b, errors.New("上架字段在两次观察间变化，不生成报价")
	}
	for i, n := range a.OfferPrices {
		if n != b.OfferPrices[i] {
			return b, errors.New("市场单价变化，不生成报价")
		}
	}
	return b, nil
}

func marketShelf(s snapshot) error {
	if e := title("商行")(s); e != nil {
		return e
	}
	if !has(s, "出售", automation.Rect{X: 175, Y: 48, W: 135, H: 45}) || !has(s, "我的货架", automation.Rect{X: 525, Y: 105, W: 160, H: 40}) || !has(s, "我的订单", automation.Rect{X: 850, Y: 450, W: 100, H: 65}) {
		return errors.New("商行出售货架未核验")
	}
	return nil
}
func marketHome(s snapshot) error {
	if e := title("商行")(s); e != nil {
		return e
	}
	if !has(s, "购买", automation.Rect{X: 40, Y: 48, W: 130, H: 45}) || !has(s, "出售", automation.Rect{X: 175, Y: 48, W: 135, H: 45}) {
		return errors.New("不是商行购买/出售页面")
	}
	return nil
}

// InspectMarket opens only a quote draft for the first visible item, then
// returns to the main screen. It never submits a listing, cancels an order,
// collects proceeds, or completes the market task in SQLite.
func InspectMarket(ctx context.Context, actors []Actor, leader int) (report ModuleReport, err error) {
	report.Module = "market_inspect"
	controls, lead, e := validateActors(actors, leader)
	if e != nil {
		return report, e
	}
	c := controls[lead]
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result := ModuleActor{Instance: c.Instance, Part: Part{State: studio.Review, Reason: "quote_only_not_market_completion"}}
	defer func() {
		if err != nil {
			result.Error = err.Error()
			if c.OnError != nil && ctx.Err() != context.Canceled {
				c.OnError(c.last, err)
			}
		}
		report.Actors = append(report.Actors, result)
		report.Verified = err == nil
	}()
	if _, err = c.await(ctx, c.game, 8*time.Second); err != nil {
		return report, err
	}
	if _, err = c.tap(ctx, c.game, textButton("交易", whole), marketHome); err != nil {
		return report, err
	}
	if _, err = c.tap(ctx, marketHome, textButton("出售", automation.Rect{X: 175, Y: 48, W: 135, H: 45}), marketShelf); err != nil {
		return report, err
	}
	if _, err = c.tap(ctx, marketShelf, fixedButton(automation.Rect{X: 181, Y: 117, W: 46, H: 42}), listingPage); err != nil {
		return report, err
	}
	quote, e := c.inspectMarketQuote(ctx)
	if e != nil {
		return report, e
	}
	result.Quote = &quote
	result.Target = quote.Item
	if _, err = c.tap(ctx, listingPage, fixedButton(automation.Rect{X: 778, Y: 51, W: 28, H: 28}), marketShelf); err != nil {
		return report, err
	}
	if _, err = c.tap(ctx, marketShelf, fixedButton(automation.Rect{X: 25, Y: 8, W: 30, H: 26}), c.game); err != nil {
		return report, err
	}
	result.Part.ScreenSHA = quote.ScreenSHA
	return report, nil
}
