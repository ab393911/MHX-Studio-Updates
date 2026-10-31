package taskflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
	"github.com/ab393911/MHX-Studio-Updates/internal/vision"
)

const (
	testTalisman = 7
	testBag      = 8
	testPhantom  = 9
	testWays     = 10
	testShop     = 11
	testReward   = 12
)

type eightState struct {
	Stock                          [4]int64
	Active, Cover, Strong          bool
	Rows                           [4]bool
	Points                         int64
	HasCard, Frozen, Insufficient  bool
	Grade                          int
	Quantity, Price, Owned         int64
	Balance, WrongCurrency, Reward bool
}
type eightDevice struct {
	base                                          *teamDevice
	mu                                            sync.Mutex
	p                                             eightState
	cache                                         map[string][]byte
	applyCalls, castCalls, buyCalls, castAttempts int
	onBuy                                         func()
}

func setField(im *image.NRGBA, r automation.Rect, kind int, n int64) {
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			im.SetNRGBA(x, y, color.NRGBA{uint8(n), uint8(200 + kind), uint8(n >> 8), 255})
		}
	}
}
func drawCheck(im *image.NRGBA, x, y int, checked bool) {
	if !checked {
		return
	}
	for yy := y - 8; yy < y+8; yy++ {
		for xx := x - 8; xx < x+8; xx++ {
			im.SetNRGBA(xx, yy, color.NRGBA{80, 180, 105, 255})
		}
	}
	for yy := y - 3; yy < y+3; yy++ {
		for xx := x - 2; xx < x+2; xx++ {
			im.SetNRGBA(xx, yy, color.NRGBA{255, 255, 255, 255})
		}
	}
}
func (d *eightDevice) Capture(ctx context.Context) ([]byte, error) {
	b, e := d.base.Capture(ctx)
	if e != nil {
		return nil, e
	}
	img, e := automation.Decode(b)
	if e != nil {
		return nil, e
	}
	r, _, _, _ := img.At(0, 0).RGBA()
	scene := int(r >> 8)
	if scene < testTalisman {
		return b, nil
	}
	d.mu.Lock()
	p := d.p
	key := fmt.Sprintf("%d:%+v", scene, p)
	cached := d.cache[key]
	d.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	im := image.NewNRGBA(img.Bounds())
	for y := 0; y < 540; y++ {
		for x := 0; x < 960; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			im.SetNRGBA(x, y, color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 255})
		}
	}
	if p.HasCard {
		im.SetNRGBA(8, 1, color.NRGBA{255, 0, 0, 255})
	}
	if p.Insufficient {
		im.SetNRGBA(10, 1, color.NRGBA{255, 0, 0, 255})
	}
	if p.Frozen {
		im.SetNRGBA(9, 1, color.NRGBA{255, 0, 0, 255})
	}
	switch scene {
	case testTalisman:
		for i, r := range materialAreas() {
			setField(im, r, 1, p.Stock[i])
		}
		for i := 0; i < 4; i++ {
			left := int64(0)
			if p.Active {
				left = 70
			}
			setField(im, automation.Rect{X: 532, Y: 93 + i*43, W: 58, H: 22}, 2, left)
			drawCheck(im, 642, 103+i*43, p.Rows[i])
		}
		drawCheck(im, 402, 428, p.Cover)
		drawCheck(im, 505, 428, p.Strong)
	case testPhantom:
		setField(im, automation.Rect{X: 566, Y: 386, W: 40, H: 26}, 3, int64(p.Grade))
		setField(im, automation.Rect{X: 570, Y: 413, W: 60, H: 23}, 7, p.Points)
	case testShop:
		setField(im, automation.Rect{X: 630, Y: 173, W: 42, H: 28}, 3, int64(p.Grade))
		setField(im, automation.Rect{X: 635, Y: 338, W: 32, H: 25}, 8, p.Quantity)
		setField(im, automation.Rect{X: 641, Y: 381, W: 112, H: 28}, 4, p.Price)
		balance := int64(0)
		if p.Balance {
			balance = 1
		}
		setField(im, automation.Rect{X: 874, Y: 7, W: 63, H: 28}, 5, balance)
		setField(im, automation.Rect{X: 750, Y: 310, W: 25, H: 28}, 6, p.Owned)
		var patch colorPatch
		_ = json.Unmarshal(goldPatchJSON, &patch)
		for y := 0; y < patch.Rect.H; y++ {
			for x := 0; x < patch.Rect.W; x++ {
				i := (y*patch.Rect.W + x) * 3
				co := color.NRGBA{patch.Pixels[i], patch.Pixels[i+1], patch.Pixels[i+2], 255}
				if p.WrongCurrency {
					co = color.NRGBA{20, 80, 200, 255}
				}
				im.SetNRGBA(patch.Rect.X+x, patch.Rect.Y+y, co)
			}
		}
	}
	var out bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if e = enc.Encode(&out, im); e != nil {
		return nil, e
	}
	d.mu.Lock()
	if d.cache == nil {
		d.cache = map[string][]byte{}
	}
	d.cache[key] = out.Bytes()
	d.mu.Unlock()
	return out.Bytes(), nil
}
func (d *eightDevice) Tap(ctx context.Context, x, y int) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	states, _, _ := d.base.world.copy()
	scene := states[d.base.id]
	set := func(s int) { d.base.world.mu.Lock(); d.base.world.state[d.base.id] = s; d.base.world.mu.Unlock() }
	if scene == testMain && x >= 900 && y >= 350 && y <= 410 {
		set(testBag)
		return nil
	}
	if scene == testParty && x >= 26 && x <= 66 && y >= 457 && y <= 503 {
		set(testTalisman)
		return nil
	}
	if scene < testTalisman {
		return d.base.Tap(ctx, x, y)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	switch scene {
	case testTalisman:
		if x >= 642 && y < 60 {
			set(testParty)
			return nil
		}
		for i := 0; i < 4; i++ {
			if x >= 632 && x <= 653 && y >= 93+i*43 && y <= 115+i*43 {
				d.p.Rows[i] = !d.p.Rows[i]
				return nil
			}
		}
		if y >= 418 && y <= 440 {
			if x >= 392 && x <= 413 {
				d.p.Cover = !d.p.Cover
				return nil
			}
			if x >= 495 && x <= 516 {
				d.p.Strong = !d.p.Strong
				return nil
			}
		}
		if x >= 410 && x <= 560 && y >= 450 && y <= 505 {
			for _, n := range d.p.Stock {
				if n < 1 {
					return errors.New("fixture material empty")
				}
			}
			for i := range d.p.Stock {
				d.p.Stock[i]--
			}
			d.p.Active = true
			d.applyCalls++
			return nil
		}
	case testBag:
		if x >= 875 && y >= 210 && y < 290 {
			return nil
		}
		if x >= 30 && x <= 66 && y >= 385 && y <= 481 {
			set(testPhantom)
			return nil
		}
	case testPhantom:
		if x >= 24 && x <= 56 && y >= 8 && y < 34 {
			set(testMain)
			return nil
		}
		if x >= 112 && x <= 177 && y >= 157 && y <= 517 {
			return nil
		}
		if x >= 700 && y >= 450 && y < 508 {
			set(testWays)
			return nil
		}
		if x >= 600 && x < 700 && y >= 450 && y < 508 {
			d.castAttempts++
			if d.p.Points < 50 {
				d.p.Insufficient = true
				return nil
			}
			if !d.p.HasCard || d.p.Grade != 8 {
				return errors.New("fixture invalid phantom cast")
			}
			d.p.Points -= 50
			d.p.Frozen = true
			d.castCalls++
			return nil
		}
	case testWays:
		if x >= 642 && y < 60 {
			set(testPhantom)
			return nil
		}
		if x >= 555 && x <= 660 && y >= 235 && y <= 290 {
			set(testShop)
			return nil
		}
	case testShop:
		if x >= 778 && y >= 51 && y <= 79 {
			set(testWays)
			return nil
		}
		if x >= 165 && x <= 497 && y >= 139 && y <= 438 {
			return nil
		}
		if x >= 585 && x <= 616 && y >= 335 && y <= 366 {
			d.p.Quantity--
			return nil
		}
		if x >= 670 && x <= 810 && y >= 430 && y <= 482 {
			if d.p.Quantity != 1 || d.p.Grade != 8 || d.p.WrongCurrency || !d.p.Balance {
				return errors.New("fixture refused unsafe buy")
			}
			d.buyCalls++
			d.p.Owned++
			d.p.HasCard = true
			d.p.Points = 100
			if d.p.Reward {
				set(testReward)
			}
			if d.onBuy != nil {
				d.onBuy()
			}
			return nil
		}
	case testReward:
		if x >= 440 && x <= 520 && y >= 478 && y <= 508 {
			set(testShop)
			return nil
		}
	}
	return fmt.Errorf("invalid eight fixture tap %d (%d,%d)", scene, x, y)
}

type eightReader struct {
	base   teamReader
	target string
}

func (r eightReader) Read(ctx context.Context, b []byte) (vision.Screen, error) {
	im, e := automation.Decode(b)
	if e != nil {
		return vision.Screen{}, e
	}
	rr, gg, _, _ := im.At(0, 0).RGBA()
	var s vision.Screen
	s.Language = "zh-Hans-CN"
	add := func(text string, x, y, w, h int) {
		s.Lines = append(s.Lines, vision.Line{Words: []vision.Word{{Text: text, Bounds: automation.Rect{X: x, Y: y, W: w, H: h}}}})
	}
	if rr>>8 == 30 && gg>>8 == 30 {
		n := 1
		marker := func(y int) bool { _, g, _, _ := im.At(20, y).RGBA(); return g>>8 > 200 }
		if marker(143) && !marker(135) {
			n = 4
		} else {
			if marker(188) && !marker(180) {
				n = 3
			} else {
				if marker(278) && !marker(270) {
					n = 2
				}
			}
		}
		for i := 0; i < n; i++ {
			r, g, b, _ := im.At(20, 8+i*(540/n)).RGBA()
			value := int64(r>>8) + int64(b>>8)*256
			kind := int(g>>8) - 200
			text := ""
			switch kind {
			case 1, 6, 7, 8:
				text = fmt.Sprint(value)
			case 2:
				text = fmt.Sprintf("%d/70", value)
			case 3:
				text = fmt.Sprintf("%d★", value)
			case 4:
				text = fmt.Sprint(value)
			case 5:
				if value == 0 {
					text = "0"
				} else {
					text = "898.5万"
				}
			default:
				return s, fmt.Errorf("unrecognized numeric ROI kind%d", kind)
			}
			add(text, 24, 15+i*(540/n), 80, 20)
		}
		return s, ctx.Err()
	}
	scene := int(rr >> 8)
	if scene < testTalisman {
		s, e = r.base.Read(ctx, b)
		if scene == testMain {
			add("背包", 905, 374, 44, 22)
		}
		return s, e
	}
	switch scene {
	case testTalisman:
		add("打符", 310, 35, 60, 25)
		add("打符等级:8★", 315, 281, 120, 22)
		add("覆盖", 420, 418, 40, 22)
		add("强化", 520, 418, 40, 22)
		add("一键打符", 430, 465, 100, 25)
	case testBag, testPhantom:
		add("属性", 890, 90, 40, 24)
		add("仓库", 890, 320, 40, 24)
		add("灵匣", 890, 245, 40, 24)
		if scene == testBag {
			add("背包", 75, 10, 70, 25)
		} else {
			add("灵匣", 75, 10, 70, 25)
			add("幻形", 38, 410, 22, 45)
			add(r.target, 570, 88, 140, 24)
			hasCard, _, _, _ := im.At(8, 1).RGBA()
			frozen, _, _, _ := im.At(9, 1).RGBA()
			insufficient, _, _, _ := im.At(10, 1).RGBA()
			if insufficient > 0 {
				add("点数不足", 350, 250, 100, 24)
			}
			if hasCard == 0 {
				add("暂无幻形卡可使用", 575, 390, 230, 24)
				add("获取", 700, 466, 60, 24)
			} else {
				add("消耗50点非专属点数可使此1张幻形卡", 570, 434, 280, 22)
				if frozen > 0 {
					add("冻结", 625, 466, 60, 24)
				} else {
					add("幻形", 625, 466, 60, 24)
				}
				add("获取", 720, 466, 60, 24)
			}
		}
	case testWays:
		add("获取途径", 310, 35, 120, 25)
		add(r.target+"幻形符获取途径", 350, 200, 270, 24)
		add("商行购买", 350, 250, 130, 24)
		add("前往", 575, 250, 60, 25)
	case testShop:
		add("购买", 180, 55, 60, 25)
		add("交易列表", 300, 100, 120, 24)
		add(r.target+"幻形符", 635, 110, 140, 45)
		add("购买", 705, 440, 75, 25)
	case testReward:
		add("恭喜获得", 400, 100, 170, 40)
		add("点击空白处关闭", 330, 500, 250, 25)
	}
	return s, ctx.Err()
}
func eightFixture(t *testing.T) ([]Actor, *eightDevice) {
	t.Helper()
	a, w := teamFixture(t)
	w.invited = true
	for i := range w.accepted {
		w.accepted[i] = true
	}
	d := &eightDevice{base: a[0].Device.(*teamDevice), p: eightState{Stock: [4]int64{2, 2, 2, 2}, Rows: [4]bool{true, true, true, true}, Cover: true, Points: 100, HasCard: true, Grade: 8, Quantity: 1, Price: 60000, Balance: true, Reward: true}}
	target, _, _ := studio.Phantom(a[0].Profession)
	a[0].Device = d
	a[0].Reader = eightReader{teamReader{w}, target}
	return a, d
}
func TestTalismanVerifiesFourMaterialChangesAndResourceSkipNeverClicksApply(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprint(empty), func(t *testing.T) {
			a, d := eightFixture(t)
			if empty {
				d.p.Stock[0] = 0
			}
			c := &controller{Actor: a[0]}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			part, e := c.talisman(ctx, a, a[0].Name)
			if e != nil {
				t.Fatal(part, e)
			}
			if empty {
				if part.State != studio.Skip || d.applyCalls != 0 {
					t.Fatal(part, d.applyCalls)
				}
			} else if part.State != studio.Done || d.applyCalls != 1 || d.p.Stock[0] != 1 {
				t.Fatal(part, d.p)
			}
			states, _, _ := d.base.world.copy()
			if states[0] != testMain {
				t.Fatal("did not return main")
			}
		})
	}
}
func TestOwnedPhantomVerifiesFreezeAndAlreadyFrozenDoesNotSpend(t *testing.T) {
	for _, frozen := range []bool{false, true} {
		t.Run(fmt.Sprint(frozen), func(t *testing.T) {
			a, d := eightFixture(t)
			d.p.Frozen = frozen
			c := &controller{Actor: a[0]}
			part, e := c.phantom(context.Background())
			if e != nil {
				t.Fatal(part, e)
			}
			if frozen {
				if part.State != studio.Skip || d.castCalls != 0 {
					t.Fatal(part)
				}
			} else if part.State != studio.Done || d.castCalls != 1 || !d.p.Frozen {
				t.Fatal(part, d.p)
			}
			if d.buyCalls != 0 {
				t.Fatal("bought despite owned card")
			}
		})
	}
}
func TestMissingPhantomBuysExactlyOneAndRejectsWrongCurrency(t *testing.T) {
	for _, wrong := range []bool{false, true} {
		t.Run(fmt.Sprint(wrong), func(t *testing.T) {
			a, d := eightFixture(t)
			d.p.HasCard = false
			d.p.Points = 0
			d.p.WrongCurrency = wrong
			c := &controller{Actor: a[0]}
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			part, e := c.phantom(ctx)
			if wrong {
				if e == nil || d.buyCalls != 0 || d.castCalls != 0 {
					t.Fatal(part, e, d.buyCalls)
				}
			} else if e != nil || part.State != studio.Done || d.buyCalls != 1 || d.castCalls != 1 {
				t.Fatal(part, e, d.buyCalls, d.castCalls)
			}
		})
	}
}
func TestStopAfterPhantomBuyCannotResubmitOrClaimFrozen(t *testing.T) {
	a, d := eightFixture(t)
	d.p.HasCard = false
	d.p.Points = 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.onBuy = cancel
	c := &controller{Actor: a[0]}
	part, e := c.phantom(ctx)
	if !errors.Is(e, context.Canceled) || part.State == studio.Done || d.buyCalls != 1 || d.castCalls != 0 {
		t.Fatal(part, e, d.buyCalls, d.castCalls)
	}
}

func TestEightRunsFiveAndSkipsPreviouslyCompletedActorWithoutConsuming(t *testing.T) {
	a, d := eightFixture(t)
	devices := []*eightDevice{d}
	world := d.base.world
	for i := 1; i < 5; i++ {
		v := &eightDevice{base: a[i].Device.(*teamDevice), p: d.p}
		target, _, _ := studio.Phantom(a[i].Profession)
		a[i].Device = v
		a[i].Reader = eightReader{teamReader{world}, target}
		devices = append(devices, v)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	report, e := RunEight(ctx, a, a[0].Instance, map[int]bool{a[0].Instance: true})
	if e != nil || !report.Verified || len(report.Actors) != 5 {
		t.Fatal(report, e)
	}
	for i, r := range report.Actors {
		if i == 0 {
			if !r.PreviouslyCompleted || devices[i].applyCalls != 0 || devices[i].castCalls != 0 {
				t.Fatal("repeated completed actor", r)
			}
		} else if !r.ReturnedMain || r.Talisman.State != studio.Done || r.Phantom.State != studio.Done || devices[i].applyCalls != 1 || devices[i].castCalls != 1 {
			t.Fatal(r, devices[i].p)
		}
	}
}

func TestPhantomExplicitShortageDoesNotInferCardNumberOrRepeat(t *testing.T) {
	a, d := eightFixture(t)
	d.p.Points = 0
	c := &controller{Actor: a[0]}
	part, e := c.phantom(context.Background())
	if e != nil || part.State != studio.Skip || part.Reason != "resource_insufficient" || d.castAttempts != 1 || d.castCalls != 0 || d.buyCalls != 0 {
		t.Fatal(part, e, d.castAttempts, d.castCalls, d.buyCalls)
	}
}

func TestMissingPhantomRejectsThreeStarAndVerifiedCopperShortage(t *testing.T) {
	for _, grade := range []int{3, 8} {
		t.Run(fmt.Sprint(grade), func(t *testing.T) {
			a, d := eightFixture(t)
			d.p.HasCard = false
			d.p.Grade = grade
			d.p.Balance = false
			c := &controller{Actor: a[0]}
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			part, e := c.phantom(ctx)
			if grade == 3 && e == nil {
				t.Fatal("default 3-star accepted", part)
			}
			if grade == 8 && (e != nil || part.State != studio.Skip || part.Reason != "resource_insufficient") {
				t.Fatal(part, e)
			}
			if d.buyCalls != 0 || d.castCalls != 0 {
				t.Fatal("unsafe spend", d.buyCalls, d.castCalls)
			}
		})
	}
}
