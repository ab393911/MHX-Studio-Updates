package taskflow

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
	"github.com/ab393911/MHX-Studio-Updates/internal/vision"
)

type Part struct {
	State     studio.TaskState `json:"state"`
	Reason    string           `json:"reason"`
	ScreenSHA string           `json:"screen_sha256"`
}
type EightActor struct {
	Instance            int    `json:"instance"`
	Talisman            Part   `json:"talisman"`
	Phantom             Part   `json:"phantom"`
	ReturnedMain        bool   `json:"returned_main"`
	Error               string `json:"error,omitempty"`
	PreviouslyCompleted bool   `json:"previously_completed,omitempty"`
}
type EightReport struct {
	Server         string       `json:"server"`
	Role           int          `json:"role"`
	StartedAt      time.Time    `json:"started_at"`
	FinishedAt     time.Time    `json:"finished_at"`
	Actors         []EightActor `json:"actors"`
	Verified       bool         `json:"verified"`
	Error          string       `json:"error,omitempty"`
	RealGameTested bool         `json:"real_game_tested"`
}

var ErrResourceInsufficient = errors.New("已核验资源不足")
var gradeEightPattern = regexp.MustCompile(`^打符等级:8[★星*∧^△▲⌃]*$`)
var fractionPattern = regexp.MustCompile(`^([0-9]+)/([0-9]+)$`)
var phantomCostPattern = regexp.MustCompile(`消耗([0-9]+)点非专属点数`)

func greenCheck(s snapshot, x, y int) bool {
	green, white := 0, 0
	for yy := y - 8; yy < y+8; yy++ {
		for xx := x - 8; xx < x+8; xx++ {
			r, g, b, _ := s.image.At(xx, yy).RGBA()
			if g > r*12/10 && g > b*11/10 && g > 30000 {
				green++
			}
			if r > 50000 && g > 50000 && b > 50000 {
				white++
			}
		}
	}
	return green > 25 && white > 10
}
func talismanPage(s snapshot) error {
	if !has(s, "打符", automation.Rect{X: 300, Y: 28, W: 95, H: 38}) || !has(s, "打符等级", automation.Rect{X: 310, Y: 270, W: 145, H: 42}) || !has(s, "覆盖", automation.Rect{X: 410, Y: 411, W: 60, H: 35}) {
		return errors.New("打符标题、等级字段和覆盖选项未核验")
	}
	return nil
}
func talismanEight(s snapshot) error {
	if e := talismanPage(s); e != nil {
		return e
	}
	if !gradeEightPattern.MatchString(vision.Normal(s.ocr.Text(automation.Rect{X: 310, Y: 270, W: 145, H: 42}))) {
		return errors.New("打符等级不是已确认8★，不猜测下拉选项")
	}
	return nil
}
func materialAreas() []automation.Rect {
	var out []automation.Rect
	for i := 0; i < 4; i++ {
		out = append(out, automation.Rect{X: 364 + i*69, Y: 360, W: 23, H: 25})
	}
	return out
}
func (c *controller) materials(ctx context.Context, s snapshot) ([4]int64, error) {
	var out [4]int64
	v, e := vision.ReadAmounts(ctx, c.Reader, s.png, materialAreas())
	if e != nil {
		return out, e
	}
	for i, n := range v {
		v, e := n.ExactInteger()
		if e != nil {
			return out, e
		}
		out[i] = v
	}
	return out, nil
}
func (c *controller) effectsActive(ctx context.Context, s snapshot) error {
	var areas []automation.Rect
	for i := 0; i < 4; i++ {
		areas = append(areas, automation.Rect{X: 532, Y: 93 + i*43, W: 58, H: 22})
	}
	fields, e := vision.ReadFields(ctx, c.Reader, s.png, areas)
	if e != nil {
		return e
	}
	for _, v := range fields {
		m := fractionPattern.FindStringSubmatch(vision.Normal(v))
		if m == nil {
			return errors.New("四项打符属性增益值未读清")
		}
		left, e := strconv.ParseInt(m[1], 10, 64)
		if e != nil || left <= 0 {
			return errors.New("仍有未生效的打符效果")
		}
		total, e := strconv.ParseInt(m[2], 10, 64)
		if e != nil || left > total || total <= 0 {
			return errors.New("打符属性增益值无效")
		}
	}
	return nil
}
func (c *controller) talisman(ctx context.Context, actors []Actor, leaderName string) (part Part, err error) {
	part.State = studio.Review
	if e := c.openTeam(ctx); e != nil {
		return part, e
	}
	if _, e := c.tap(ctx, fullTeam(actors, leaderName), fixedButton(automation.Rect{X: 26, Y: 457, W: 40, H: 46}), talismanPage); e != nil {
		return part, e
	}
	s, e := c.await(ctx, talismanEight, 8*time.Second)
	if e != nil {
		return part, e
	}
	before, e := c.materials(ctx, s)
	if e != nil {
		return part, e
	}
	decision := studio.TalismanDecision(studio.TalismanObservation{Verified: true, SelectedGrade: 8, Materials: before, MaterialCountsVerified: true})
	if decision.State == studio.Skip {
		verify := func(s snapshot) error {
			if e := talismanEight(s); e != nil {
				return e
			}
			now, e := c.materials(ctx, s)
			if e != nil {
				return e
			}
			if now != before {
				return errors.New("材料数变化，未核验资源不足")
			}
			return nil
		}
		s, e = c.await(ctx, verify, 10*time.Second)
		if e != nil {
			return part, e
		}
		part = Part{State: studio.Skip, Reason: "resource_insufficient", ScreenSHA: s.sha}
	} else if decision.State != studio.Pending {
		return part, errors.New(decision.Reason)
	} else {
		for i := 0; i < 4; i++ {
			i := i
			s, e = c.take(ctx)
			if e != nil {
				return part, e
			}
			if !greenCheck(s, 642, 103+i*43) {
				after := func(s snapshot) error {
					if e := talismanEight(s); e != nil {
						return e
					}
					if !greenCheck(s, 642, 103+i*43) {
						return errors.New("打符项目未勾选")
					}
					return nil
				}
				if _, e = c.tap(ctx, talismanEight, fixedButton(automation.Rect{X: 632, Y: 93 + i*43, W: 21, H: 22}), after); e != nil {
					return part, e
				}
			}
		}
		for _, opt := range []struct {
			x    int
			want bool
		}{{402, true}, {505, false}} {
			opt := opt
			s, e = c.take(ctx)
			if e != nil {
				return part, e
			}
			if greenCheck(s, opt.x, 428) != opt.want {
				after := func(s snapshot) error {
					if e := talismanEight(s); e != nil {
						return e
					}
					if greenCheck(s, opt.x, 428) != opt.want {
						return errors.New("覆盖/强化选项状态未确认")
					}
					return nil
				}
				if _, e = c.tap(ctx, talismanEight, fixedButton(automation.Rect{X: opt.x - 10, Y: 418, W: 21, H: 22}), after); e != nil {
					return part, e
				}
			}
		}
		ready := func(s snapshot) error {
			if e := talismanEight(s); e != nil {
				return e
			}
			for i := 0; i < 4; i++ {
				if !greenCheck(s, 642, 103+i*43) {
					return errors.New("四项打符未全部勾选")
				}
			}
			if !greenCheck(s, 402, 428) || greenCheck(s, 505, 428) {
				return errors.New("必须覆盖且不强化")
			}
			v, e := c.materials(ctx, s)
			if e != nil {
				return e
			}
			if v != before {
				return errors.New("打符提交前材料数变化，不消费")
			}
			return nil
		}
		verified := func(s snapshot) error {
			if e := talismanEight(s); e != nil {
				return e
			}
			v, e := c.materials(ctx, s)
			if e != nil {
				return e
			}
			for i, n := range v {
				if n != before[i]-1 {
					return errors.New("材料变化不对应本次四项打符")
				}
			}
			return c.effectsActive(ctx, s)
		}
		s, e = c.tap(ctx, ready, textButton("一键打符", automation.Rect{X: 410, Y: 450, W: 150, H: 55}), verified)
		if e != nil {
			return part, e
		}
		part = Part{State: studio.Done, Reason: "verified_four_effects_and_material_consumption", ScreenSHA: s.sha}
	}
	if _, e = c.tap(ctx, talismanEight, fixedButton(automation.Rect{X: 642, Y: 32, W: 26, H: 26}), partyPage); e != nil {
		return part, e
	}
	_, e = c.tap(ctx, fullTeam(actors, leaderName), fixedButton(automation.Rect{X: 24, Y: 8, W: 32, H: 26}), c.game)
	return part, e
}

func bagPage(s snapshot) error {
	if !has(s, "属性", automation.Rect{X: 875, Y: 65, W: 75, H: 75}) || !has(s, "仓库", automation.Rect{X: 875, Y: 290, W: 75, H: 75}) {
		return errors.New("背包/灵匣公共侧栏未核验")
	}
	return nil
}
func phantomPage(s snapshot) error {
	if e := bagPage(s); e != nil {
		return e
	}
	if !has(s, "灵匣", automation.Rect{X: 65, W: 115, H: 44}) || !has(s, "幻形", automation.Rect{X: 25, Y: 365, W: 50, H: 150}) {
		return errors.New("幻形页面未核验")
	}
	return nil
}
func phantomTarget(target string) check {
	return func(s snapshot) error {
		if e := phantomPage(s); e != nil {
			return e
		}
		if !has(s, target, automation.Rect{X: 560, Y: 75, W: 240, H: 45}) {
			return errors.New("当前幻形目标不是" + target)
		}
		return nil
	}
}
func (c *controller) phantomGrade(ctx context.Context, s snapshot) error {
	v, e := vision.ReadFields(ctx, c.Reader, s.png, []automation.Rect{{X: 566, Y: 386, W: 40, H: 26}})
	if e != nil {
		return e
	}
	if !regexp.MustCompile(`^8[★星*]?$`).MatchString(vision.Normal(v[0])) {
		return errors.New("所选幻形卡未核验8★")
	}
	return nil
}
func (c *controller) openPhantom(ctx context.Context) error {
	if _, e := c.tap(ctx, c.game, textButton("背包", whole), bagPage); e != nil {
		return e
	}
	if _, e := c.tap(ctx, bagPage, textButton("灵匣", automation.Rect{X: 875, Y: 210, W: 75, H: 80}), bagPage); e != nil {
		return e
	}
	_, e := c.tap(ctx, bagPage, fixedButton(automation.Rect{X: 30, Y: 385, W: 36, H: 96}), phantomPage)
	return e
}
func (c *controller) selectPhantom(ctx context.Context, target, category string) error {
	s, e := c.take(ctx)
	if e != nil {
		return e
	}
	if phantomTarget(target)(s) == nil {
		return nil
	}
	r, e := s.ocr.Unique(category, automation.Rect{X: 82, Y: 55, W: 140, H: 115})
	if e != nil {
		return errors.New("幻形类别未在已知区域显示，需要核对滚动/展开状态：" + category)
	}
	// Expanded category has a light heading bar in the provided screenshots.
	rr, g, b, _ := s.image.At(88, r.Y+r.H/2).RGBA()
	if rr < 38000 || g < 42000 || b < 42000 {
		if _, e = c.tap(ctx, phantomPage, textButton(category, automation.Rect{X: 82, Y: 55, W: 140, H: 115}), phantomPage); e != nil {
			return e
		}
	}
	for i := 0; i < 4; i++ {
		if _, e = c.tap(ctx, phantomPage, fixedButton(automation.Rect{X: 112, Y: 157 + i*100, W: 65, H: 60}), phantomPage); e != nil {
			return e
		}
		s, e = c.take(ctx)
		if e != nil {
			return e
		}
		if phantomTarget(target)(s) == nil {
			return nil
		}
	}
	return errors.New("已见幻形列表中没有核验目标，不购买其它卡：" + target)
}
func (c *controller) phantom(ctx context.Context) (part Part, err error) {
	part.State = studio.Review
	target, category, e := studio.Phantom(c.Profession)
	if e != nil {
		return part, e
	}
	if e = c.openPhantom(ctx); e != nil {
		return part, e
	}
	if e = c.selectPhantom(ctx, target, category); e != nil {
		return part, e
	}
	frozen := func(s snapshot) error {
		if e := phantomTarget(target)(s); e != nil {
			return e
		}
		_, e := s.ocr.Unique("冻结", automation.Rect{X: 580, Y: 450, W: 140, H: 58})
		return e
	}
	s, e := c.take(ctx)
	if e != nil {
		return part, e
	}
	if frozen(s) == nil {
		frozenEight := func(s snapshot) error {
			if e := frozen(s); e != nil {
				return e
			}
			return c.phantomGrade(ctx, s)
		}
		s, e = c.await(ctx, frozenEight, 10*time.Second)
		if e != nil {
			return part, e
		}
		part = Part{State: studio.Skip, Reason: "already_done", ScreenSHA: s.sha}
	} else {
		if has(s, "暂无幻形卡可使用", automation.Rect{X: 560, Y: 370, W: 280, H: 60}) {
			if e = c.buyPhantom(ctx, target); errors.Is(e, ErrResourceInsufficient) {
				part = Part{State: studio.Skip, Reason: "resource_insufficient", ScreenSHA: s.sha}
			} else if e != nil {
				return part, e
			}
		}
		if part.State != studio.Skip {
			ready := func(s snapshot) error {
				if e := phantomTarget(target)(s); e != nil {
					return e
				}
				if e := c.phantomGrade(ctx, s); e != nil {
					return e
				}
				_, e := s.ocr.Unique("幻形", automation.Rect{X: 600, Y: 450, W: 110, H: 58})
				return e
			}
			s, e = c.await(ctx, ready, 8*time.Second)
			if e != nil {
				return part, e
			}
			costMatch := phantomCostPattern.FindStringSubmatch(vision.Normal(s.ocr.Text(automation.Rect{X: 560, Y: 430, W: 305, H: 30})))
			if costMatch == nil {
				return part, errors.New("幻形实际消耗未核验")
			}
			cost, e := strconv.ParseInt(costMatch[1], 10, 64)
			if e != nil || cost <= 0 {
				return part, errors.New("幻形消耗无效")
			}
			before := func(s snapshot) error {
				if e := ready(s); e != nil {
					return e
				}
				currentCost := phantomCostPattern.FindStringSubmatch(vision.Normal(s.ocr.Text(automation.Rect{X: 560, Y: 430, W: 305, H: 30})))
				if currentCost == nil || currentCost[1] != costMatch[1] {
					return errors.New("幻形实际消耗变化，不消费")
				}
				return nil
			}
			// The number below the card has no established resource meaning. Never
			// infer available points from it or retry an uncertain consumption.
			_, e = c.press(ctx, before, textButton("幻形", automation.Rect{X: 600, Y: 450, W: 110, H: 58}))
			if e != nil {
				return part, e
			}
			outcome := ""
			result := func(s snapshot) error {
				next := ""
				if frozen(s) == nil && c.phantomGrade(ctx, s) == nil {
					next = "frozen"
				} else if phantomTarget(target)(s) == nil {
					for _, message := range []string{"点数不足", "幻形卡不足", "材料不足", "资源不足"} {
						if has(s, message, automation.Rect{X: 240, Y: 120, W: 590, H: 330}) {
							next = "insufficient"
							break
						}
					}
				}
				if next == "" {
					outcome = ""
					return errors.New("幻形后既未核验冻结，也未核验资源不足；不重复点击")
				}
				if outcome != "" && outcome != next {
					outcome = next
					return errors.New("幻形结果变化，重新等待稳定")
				}
				outcome = next
				return nil
			}
			s, e = c.await(ctx, result, 18*time.Second)
			if e != nil {
				return part, e
			}
			if outcome == "insufficient" {
				// No supplement/confirm action is authorized. A visible cancel is
				// permitted only with the same verified shortage context.
				if _, cancelErr := s.ocr.Unique("取消", automation.Rect{X: 240, Y: 240, W: 480, H: 220}); cancelErr == nil {
					_, e = c.tap(ctx, func(s snapshot) error {
						if e := result(s); e != nil {
							return e
						}
						if outcome != "insufficient" {
							return errors.New("资源不足窗口已变化")
						}
						return nil
					}, textButton("取消", automation.Rect{X: 240, Y: 240, W: 480, H: 220}), ready)
					if e != nil {
						return part, e
					}
				} else if has(s, "确认", automation.Rect{X: 240, Y: 240, W: 480, H: 220}) || has(s, "补充", automation.Rect{X: 240, Y: 240, W: 480, H: 220}) {
					return part, errors.New("资源不足窗口没有已核验取消按钮，停止且不补充购买")
				}
				part = Part{State: studio.Skip, Reason: "resource_insufficient", ScreenSHA: s.sha}
			} else {
				part = Part{State: studio.Done, Reason: "verified_target_eight_star_frozen", ScreenSHA: s.sha}
			}
		}
	}
	_, e = c.tap(ctx, phantomTarget(target), fixedButton(automation.Rect{X: 24, Y: 8, W: 32, H: 26}), c.game)
	return part, e
}

func RunEight(ctx context.Context, actors []Actor, leader int, skip map[int]bool) (report EightReport, err error) {
	report.StartedAt = time.Now().UTC()
	defer func() {
		report.FinishedAt = time.Now().UTC()
		if err != nil {
			report.Error = err.Error()
		}
	}()
	controls, lead, e := validateActors(actors, leader)
	if e != nil {
		return report, e
	}
	report.Server, report.Role = actors[0].Server, actors[0].Role
	userCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for _, c := range controls {
		if _, e = c.await(ctx, c.game, 8*time.Second); e != nil {
			return report, e
		}
	}
	report.Actors = make([]EightActor, 5)
	errs := make(chan error, 5)
	var wg sync.WaitGroup
	for i, c := range controls {
		if skip[c.Instance] {
			report.Actors[i] = EightActor{Instance: c.Instance, ReturnedMain: true, PreviouslyCompleted: true}
			continue
		}
		wg.Add(1)
		go func(i int, c *controller) {
			defer wg.Done()
			r := EightActor{Instance: c.Instance, Phantom: Part{State: studio.Review, Reason: "not_started"}}
			first, e := c.talisman(ctx, actors, actors[lead].Name)
			r.Talisman = first
			if e == nil {
				r.Phantom, e = c.phantom(ctx)
			}
			if e == nil {
				_, e = c.await(ctx, c.game, 8*time.Second)
				r.ReturnedMain = e == nil
			}
			if e != nil {
				r.Error = e.Error()
				errs <- fmt.Errorf("实例%d八星准备：%w", c.Instance, e)
				cancel()
				if !errors.Is(userCtx.Err(), context.Canceled) && c.OnError != nil {
					c.OnError(c.last, e)
				}
			}
			report.Actors[i] = r
		}(i, c)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		err = errors.Join(err, e)
	}
	if err == nil {
		for _, r := range report.Actors {
			if r.PreviouslyCompleted {
				continue
			}
			known := func(p Part) bool { return p.State == studio.Done || p.State == studio.Skip }
			if !r.ReturnedMain || !known(r.Talisman) || !known(r.Phantom) {
				err = errors.New("八星子步骤结果不完整，不标记阶段结束")
				break
			}
		}
	}
	report.Verified = err == nil
	return report, err
}
