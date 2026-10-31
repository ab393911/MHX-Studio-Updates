package taskflow

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
	"github.com/ab393911/MHX-Studio-Updates/internal/vision"
)

func title(text string) check {
	return func(s snapshot) error {
		if !has(s, text, automation.Rect{X: 20, Y: 0, W: 310, H: 85}) {
			return fmt.Errorf("%s页面标题未核验", text)
		}
		return nil
	}
}
func dailyPage(s snapshot) error {
	if e := title("日常")(s); e != nil {
		return e
	}
	if !has(s, "聚福宝匣", automation.Rect{X: 80, Y: 420, W: 170, H: 85}) || !has(s, "挑战", automation.Rect{X: 865, Y: 140, W: 85, H: 70}) {
		return errors.New("日常页独立入口未核验")
	}
	return nil
}
func fortunePage(s snapshot) error {
	if e := title("聚福宝匣")(s); e != nil {
		return e
	}
	if !has(s, "挑战加速", automation.Rect{X: 155, Y: 95, W: 50, H: 155}) {
		return errors.New("聚福挑战加速标签未核验")
	}
	return nil
}
func (c *controller) openDaily(ctx context.Context) error {
	s, e := c.take(ctx)
	if e != nil {
		return e
	}
	if dailyPage(s) == nil {
		return nil
	}
	_, e = c.tap(ctx, c.game, textButton("玩法", automation.Rect{X: 0, Y: 110, W: 90, H: 75}), dailyPage)
	return e
}
func (c *controller) scroll(ctx context.Context, before check, up bool) error {
	d, ok := c.Device.(interface {
		Swipe(context.Context, int, int, int, int, time.Duration) error
	})
	if !ok {
		return errors.New("当前设备不支持列表滚动")
	}
	s, e := c.take(ctx)
	if e != nil {
		return e
	}
	if e = before(s); e != nil {
		return e
	}
	y1, y2 := 420, 160
	if !up {
		y1, y2 = y2, y1
	}
	if e = wait(ctx, automation.Delay(c.MinDelayMS, c.MaxDelayMS)); e != nil {
		return e
	}
	s, e = c.take(ctx)
	if e != nil {
		return e
	}
	if e = before(s); e != nil {
		return e
	}
	if e = d.Swipe(ctx, 430, y1, 430, y2, 450*time.Millisecond); e != nil {
		return e
	}
	return wait(ctx, 350*time.Millisecond)
}
func (c *controller) closeDaily(ctx context.Context) error {
	_, e := c.tap(ctx, dailyPage, fixedButton(automation.Rect{X: 25, Y: 8, W: 30, H: 26}), c.game)
	return e
}
func (c *controller) fortune(ctx context.Context) (Part, error) {
	out := Part{State: studio.Review}
	if e := c.openDaily(ctx); e != nil {
		return out, e
	}
	if _, e := c.tap(ctx, dailyPage, textButton("聚福宝匣", automation.Rect{X: 80, Y: 420, W: 170, H: 85}), fortunePage); e != nil {
		return out, e
	}
	changed := false
	for _, name := range []string{"妖灵据点", "魔宗妖王"} {
		found := false
		for n := 0; n < 5; n++ {
			s, e := c.take(ctx)
			if e != nil {
				return out, e
			}
			if e = fortunePage(s); e != nil {
				return out, e
			}
			if _, e = s.ocr.Unique(name, automation.Rect{X: 205, Y: 95, W: 345, H: 375}); e == nil {
				found = true
				break
			}
			if n < 4 {
				if e = c.scroll(ctx, fortunePage, true); e != nil {
					return out, e
				}
			}
		}
		if !found {
			return out, fmt.Errorf("聚福列表未找到%s，没有选择其它玩法", name)
		}
		selected := func(s snapshot) error {
			if e := fortunePage(s); e != nil {
				return e
			}
			if !has(s, name, automation.Rect{X: 585, Y: 105, W: 210, H: 45}) {
				return errors.New("聚福右侧目标未核验")
			}
			return nil
		}
		if _, e := c.tap(ctx, fortunePage, textButton(name, automation.Rect{X: 205, Y: 95, W: 345, H: 375}), selected); e != nil {
			return out, e
		}
		s, e := c.take(ctx)
		if e != nil {
			return out, e
		}
		activated := func(s snapshot) error {
			if e := selected(s); e != nil {
				return e
			}
			if !has(s, "已激活", automation.Rect{X: 590, Y: 410, W: 190, H: 60}) {
				return errors.New("未核验目标已激活，不重复扣福气")
			}
			return nil
		}
		if activated(s) == nil {
			out.ScreenSHA = s.sha
			continue
		}
		v, e := vision.ReadAmounts(ctx, c.Reader, s.png, []automation.Rect{{X: 878, Y: 5, W: 43, H: 30}, {X: 634, Y: 424, W: 37, H: 28}})
		if e != nil {
			return out, e
		}
		balance, e := v[0].ExactInteger()
		if e != nil {
			return out, e
		}
		cost, e := v[1].ExactInteger()
		if e != nil || cost <= 0 {
			return out, errors.New("聚福实际激活消耗未读清")
		}
		if balance < cost {
			// The handoff's exchange page does not show the remaining weekly cap.
			// Do not manufacture a cap or spend bound jade with missing observations.
			return out, errors.New("福气不足，绑玉兑换的本周剩余额度尚无可核验读数，未兑换或标完成")
		}
		before := func(s snapshot) error {
			if e := selected(s); e != nil {
				return e
			}
			now, e := vision.ReadAmounts(ctx, c.Reader, s.png, []automation.Rect{{X: 878, Y: 5, W: 43, H: 30}, {X: 634, Y: 424, W: 37, H: 28}})
			if e != nil {
				return e
			}
			if len(now) != 2 || now[0] != v[0] || now[1] != v[1] {
				return errors.New("聚福余额或费用变化，未激活")
			}
			return nil
		}
		after := func(s snapshot) error {
			if e := activated(s); e != nil {
				return e
			}
			v, e := vision.ReadAmounts(ctx, c.Reader, s.png, []automation.Rect{{X: 878, Y: 5, W: 43, H: 30}})
			if e != nil {
				return e
			}
			n, e := v[0].ExactInteger()
			if e != nil || n != balance-cost {
				return errors.New("聚福扣费与激活结果未同时核验")
			}
			return nil
		}
		s, e = c.tap(ctx, before, textButton("一键激活", automation.Rect{X: 595, Y: 410, W: 175, H: 60}), after)
		if e != nil {
			return out, e
		}
		changed = true
		out.ScreenSHA = s.sha
	}
	if _, e := c.tap(ctx, fortunePage, fixedButton(automation.Rect{X: 781, Y: 56, W: 22, H: 22}), dailyPage); e != nil {
		return out, e
	}
	if e := c.closeDaily(ctx); e != nil {
		return out, e
	}
	out.State, out.Reason = studio.Skip, "already_done"
	if changed {
		out.State, out.Reason = studio.Done, "verified_two_accelerations_and_cost"
	}
	return out, nil
}

func passPage(s snapshot) error {
	if e := title("仙盟战令")(s); e != nil {
		return e
	}
	if !has(s, "战令奖励", automation.Rect{X: 875, Y: 135, W: 50, H: 145}) && !has(s, "战令任务", automation.Rect{X: 875, Y: 295, W: 50, H: 160}) {
		return errors.New("战令独立侧栏未核验")
	}
	return nil
}
func passTasks(s snapshot) error {
	if e := passPage(s); e != nil {
		return e
	}
	if !has(s, "消耗活力200点", whole) {
		return errors.New("战令消耗活力任务未读清")
	}
	return nil
}
func workPage(s snapshot) error {
	if !has(s, "打工", automation.Rect{X: 235, Y: 170, W: 95, H: 160}) || !has(s, "开工", automation.Rect{X: 500, Y: 315, W: 140, H: 50}) || !has(s, "消耗活力", automation.Rect{X: 450, Y: 195, W: 200, H: 40}) {
		return errors.New("打工簿工作窗口未核验")
	}
	return nil
}

var vitalityRE = regexp.MustCompile(`^([0-9]+)/([0-9]+)$`)
var workCostRE = regexp.MustCompile(`消耗活力([0-9]+)`)

func (c *controller) openPass(ctx context.Context) error {
	s, e := c.take(ctx)
	if e != nil {
		return e
	}
	if passPage(s) == nil {
		return nil
	}
	welfare := func(s snapshot) error {
		if !has(s, "仙盟战令", automation.Rect{X: 0, Y: 135, W: 160, H: 110}) {
			return errors.New("福利仙盟战令入口未核验")
		}
		return nil
	}
	if _, e = c.tap(ctx, c.game, textButton("福利", automation.Rect{X: 140, Y: 5, W: 100, H: 70}), welfare); e != nil {
		return e
	}
	_, e = c.tap(ctx, welfare, textButton("仙盟战令", automation.Rect{X: 0, Y: 135, W: 160, H: 110}), passPage)
	return e
}
func vitality(s snapshot) (int64, error) {
	m := vitalityRE.FindStringSubmatch(vision.Normal(s.ocr.Text(automation.Rect{X: 754, Y: 5, W: 63, H: 30})))
	if m == nil {
		return 0, errors.New("活力当前值/上限未读清")
	}
	n, e := strconv.ParseInt(m[1], 10, 64)
	if e != nil {
		return 0, e
	}
	top, e := strconv.ParseInt(m[2], 10, 64)
	if e != nil || n > top || top <= 0 {
		return 0, errors.New("活力数值无效")
	}
	return n, nil
}
func workCost(s snapshot) (int64, error) {
	m := workCostRE.FindStringSubmatch(vision.Normal(s.ocr.Text(automation.Rect{X: 450, Y: 195, W: 200, H: 40})))
	if m == nil {
		return 0, errors.New("打工实际活力消耗未读清")
	}
	n, e := strconv.ParseInt(m[1], 10, 64)
	if e != nil || n <= 0 {
		return 0, errors.New("打工费用无效")
	}
	return n, nil
}

// workOnce consumes one observed unit, never an estimated max. An unknown
// result cannot trigger a second click or a terminal scheduler record.
func (c *controller) workOnce(ctx context.Context) (Part, error) {
	out := Part{State: studio.Review}
	s, e := c.take(ctx)
	if e != nil {
		return out, e
	}
	if e = workPage(s); e != nil {
		return out, e
	}
	before, e := vitality(s)
	if e != nil {
		return out, e
	}
	cost, e := workCost(s)
	if e != nil {
		return out, e
	}
	if before < cost {
		return Part{State: studio.Skip, Reason: "resource_insufficient", ScreenSHA: s.sha}, nil
	}
	qty := vision.Normal(s.ocr.Text(automation.Rect{X: 480, Y: 273, W: 105, H: 36}))
	if qty != "1" {
		return out, errors.New("打工次数不是1，未开工")
	}
	guard := func(s snapshot) error {
		if e := workPage(s); e != nil {
			return e
		}
		n, e := vitality(s)
		if e != nil || n != before {
			return errors.New("开工前活力变化")
		}
		k, e := workCost(s)
		if e != nil || k != cost || vision.Normal(s.ocr.Text(automation.Rect{X: 480, Y: 273, W: 105, H: 36})) != "1" {
			return errors.New("开工费用或次数变化")
		}
		return nil
	}
	// Animation completion is proven by the same work window plus exact debit.
	after := func(s snapshot) error {
		if e := workPage(s); e != nil {
			return e
		}
		n, e := vitality(s)
		if e != nil || n != before-cost {
			return errors.New("打工结果未核验，不重复开工")
		}
		return nil
	}
	s, e = c.tap(ctx, guard, textButton("开工", automation.Rect{X: 500, Y: 315, W: 140, H: 50}), after)
	if e != nil {
		return out, e
	}
	return Part{State: studio.Done, Reason: "verified_one_work_debit", ScreenSHA: s.sha}, nil
}

func taskRow(s snapshot) (automation.Rect, error) {
	if e := passTasks(s); e != nil {
		return automation.Rect{}, e
	}
	r, e := s.ocr.Unique("消耗活力200点", whole)
	if e != nil {
		return automation.Rect{}, e
	}
	return automation.Rect{X: 730, Y: max(0, r.Y-20), W: 210, H: min(85, 540-max(0, r.Y-20))}, nil
}
func taskGo(s snapshot) (automation.Rect, error) {
	r, e := taskRow(s)
	if e != nil {
		return automation.Rect{}, e
	}
	return s.ocr.Unique("前往", r)
}
func lifePage(s snapshot) error {
	if e := title("生活")(s); e != nil {
		return e
	}
	if !has(s, "打工簿", whole) {
		return errors.New("生活打工簿入口未核验")
	}
	return nil
}
func rewardPage(s snapshot) error {
	if !has(s, "获得奖励", automation.Rect{X: 280, Y: 60, W: 400, H: 130}) && !has(s, "恭喜获得", automation.Rect{X: 280, Y: 60, W: 400, H: 130}) {
		return errors.New("不是已确认奖励结果页")
	}
	if !has(s, "点击空白处关闭", automation.Rect{X: 250, Y: 430, W: 460, H: 100}) {
		return errors.New("奖励关闭提示未核验")
	}
	return nil
}
func (c *controller) closeReward(ctx context.Context, after check) error {
	_, e := c.tap(ctx, rewardPage, fixedButton(automation.Rect{X: 430, Y: 480, W: 100, H: 25}), after)
	return e
}
func (c *controller) toPassTasks(ctx context.Context) error {
	if e := c.openPass(ctx); e != nil {
		return e
	}
	_, e := c.tap(ctx, passPage, fixedButton(automation.Rect{X: 890, Y: 340, W: 20, H: 40}), passTasks)
	return e
}
func (c *controller) returnFromPass(ctx context.Context) error {
	_, e := c.tap(ctx, passPage, fixedButton(automation.Rect{X: 25, Y: 8, W: 30, H: 26}), c.game)
	return e
}
func (c *controller) battlePass(ctx context.Context) (Part, error) {
	out := Part{State: studio.Review}
	if e := c.toPassTasks(ctx); e != nil {
		return out, e
	}
	// The task repeats by the game's visible weekly rounds. Re-read it after
	// each single work action; never assume that two clicks complete the task.
	worked := false
	for attempt := 0; attempt < 12; attempt++ {
		s, e := c.take(ctx)
		if e != nil {
			return out, e
		}
		if e = passTasks(s); e != nil {
			return out, e
		}
		row, e := taskRow(s)
		if e != nil {
			return out, e
		}
		if _, e = s.ocr.Unique("领取", row); e == nil {
			if _, e = c.tap(ctx, passTasks, textButton("领取", row), rewardPage); e != nil {
				return out, e
			}
			if e = c.closeReward(ctx, passTasks); e != nil {
				return out, e
			}
			continue
		}
		if has(s, "已完成", row) || has(s, "本周已完成", automation.Rect{X: 300, Y: 90, W: 540, H: 350}) {
			out.ScreenSHA = s.sha
			out.State, out.Reason = studio.Done, "verified_pass_task_finished"
			if !worked {
				out.State, out.Reason = studio.Skip, "already_done"
			}
			break
		}
		// This action belongs to the same observed vitality task row.
		destination := func(s snapshot) error {
			if workPage(s) == nil || lifePage(s) == nil {
				return nil
			}
			return errors.New("任务前往未进入生活/打工簿")
		}
		s, e = c.tap(ctx, passTasks, taskGo, destination)
		if e != nil {
			return out, e
		}
		if lifePage(s) == nil {
			if _, e = c.tap(ctx, lifePage, textButton("打工簿", whole), workPage); e != nil {
				return out, e
			}
		}
		p, e := c.workOnce(ctx)
		if e != nil {
			return out, e
		}
		worked = worked || p.State == studio.Done
		if _, e = c.tap(ctx, workPage, fixedButton(automation.Rect{X: 696, Y: 112, W: 22, H: 22}), lifePage); e != nil {
			return out, e
		}
		if _, e = c.tap(ctx, lifePage, fixedButton(automation.Rect{X: 25, Y: 8, W: 30, H: 26}), c.game); e != nil {
			return out, e
		}
		if e = c.toPassTasks(ctx); e != nil {
			return out, e
		}
		if p.State == studio.Skip {
			out = p
			break
		}
	}
	if out.State == studio.Review {
		return out, errors.New("战令达到本轮动作上限，保留未完成状态")
	}
	// Enter only pass rewards. Never sweep other welfare categories or paid offers.
	if _, e := c.tap(ctx, passTasks, fixedButton(automation.Rect{X: 890, Y: 180, W: 20, H: 40}), passPage); e != nil {
		return Part{State: studio.Review}, e
	}
	for n := 0; n < 10; n++ {
		s, e := c.take(ctx)
		if e != nil {
			return Part{State: studio.Review}, e
		}
		if e = passPage(s); e != nil {
			return Part{State: studio.Review}, e
		}
		if _, e = s.ocr.Unique("一键领取", automation.Rect{X: 690, Y: 445, W: 200, H: 60}); e != nil {
			// A different visible claim control needs its own result contract.
			if has(s, "领取", automation.Rect{X: 690, Y: 445, W: 200, H: 60}) {
				return Part{State: studio.Review}, errors.New("战令奖励按钮与已核验一键领取不同，未点击或记全部完成")
			}
			out.ScreenSHA = s.sha
			if e = c.returnFromPass(ctx); e != nil {
				return Part{State: studio.Review}, e
			}
			return out, nil
		}
		if _, e = c.tap(ctx, passPage, textButton("一键领取", automation.Rect{X: 690, Y: 445, W: 200, H: 60}), rewardPage); e != nil {
			return Part{State: studio.Review}, e
		}
		if e = c.closeReward(ctx, passPage); e != nil {
			return Part{State: studio.Review}, e
		}
	}
	return Part{State: studio.Review}, errors.New("战令奖励弹窗达到上限，未记完成")
}
