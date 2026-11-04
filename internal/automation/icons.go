package automation

import (
	"fmt"
	"image"
	"math"
	"sort"
)

// IconRules is an opt-in draft library. Login profiles and Recognize do not load
// it. Each icon reuses the existing strict two-or-more-feature scorer.
type IconRules struct {
	Schema       int      `json:"schema"`
	Revision     int      `json:"revision"`
	Width        int      `json:"width"`
	Height       int      `json:"height"`
	Profile      Profile  `json:"profile"`
	MainIcons    []string `json:"main_icons"`
	MinIcons     int      `json:"min_icons"`
	StableFrames int      `json:"stable_frames"`
	Blockers     []string `json:"blockers,omitempty"`
}

func NewIconRules() IconRules {
	return IconRules{Schema: 1, Width: 960, Height: 540, Profile: NewProfile(), MinIcons: 2, StableFrames: 2}
}

type IconCandidate struct{ ID, Label string }

func IconCandidates() []IconCandidate {
	return []IconCandidate{{"ui_gameplay", "玩法（候选）"}, {"ui_market", "交易/交易行（候选，入口待核验）"}, {"ui_team", "队伍（候选，入口待核验）"}, {"ui_bag", "背包（候选）"}, {"ui_menu", "菜单（候选）"}, {"ui_realm", "境界（候选）"}, {"ui_common", "常用（候选）"}}
}

func informative(a Anchor) bool {
	if len(a.Pixels) != 512 {
		return false
	}
	hist := [256]int{}
	lo, hi := 255, 0
	for _, v := range a.Pixels {
		hist[v]++
		if int(v) < lo {
			lo = int(v)
		}
		if int(v) > hi {
			hi = int(v)
		}
	}
	cumulative, median := 0, 0
	for value, count := range hist {
		cumulative += count
		if cumulative >= 256 {
			median = value
			break
		}
	}
	absolute := 0
	for value, count := range hist {
		d := value - median
		if d < 0 {
			d = -d
		}
		absolute += d * count
	}
	// Minimum MAE to any constant image is the deviation about its median.
	// Keep it above 10 so a flat white/gray screen cannot match an icon core.
	return hi-lo >= 80 && float64(absolute)/512 >= 15 && IconContrast(a) >= 20
}

func insideRect(a, b Rect) bool {
	return a.X >= b.X && a.Y >= b.Y && a.X+a.W <= b.X+b.W && a.Y+a.H <= b.Y+b.H
}
func overlap(a, b Rect) bool { return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H }

func (r IconRules) Validate() error {
	if r.Schema != 1 || r.Width != 960 || r.Height != 540 || r.Revision < 0 || r.StableFrames < 2 || r.StableFrames > 3 || r.MinIcons < 2 {
		return fmt.Errorf("图标草稿需960×540、至少2帧和至少2个不同图标")
	}
	if err := r.Profile.Validate(); err != nil {
		return err
	}
	blockers := map[string]bool{}
	for _, id := range r.Blockers {
		if _, ok := r.Profile.Pages[id]; !ok || blockers[id] {
			return fmt.Errorf("无效遮挡规则 %s", id)
		}
		blockers[id] = true
	}
	buttonsUsed := map[string]bool{}
	for id, page := range r.Profile.Pages {
		if page.Threshold != 10 {
			return fmt.Errorf("图标/遮挡草稿阈值必须保持10: %s", id)
		}
		for _, limit := range page.FeatureThresholds {
			if limit != 10 {
				return fmt.Errorf("逐特征阈值必须保持10")
			}
		}
		if blockers[id] {
			continue
		}
		if page.ButtonID == "" || buttonsUsed[page.ButtonID] {
			return fmt.Errorf("图标点击区域ID必须独立: %s", id)
		}
		buttonsUsed[page.ButtonID] = true
		click, ok := r.Profile.Buttons[page.ButtonID]
		if !ok || page.RequiredMatches() != len(page.Anchors) {
			return fmt.Errorf("图标需独立点击区域且全部特征通过: %s", id)
		}
		seen := map[Rect]bool{}
		for _, a := range page.Anchors {
			if a.Rect.W > 24 || a.Rect.H > 24 || !insideRect(a.Rect, click) || !informative(a) {
				return fmt.Errorf("%s：选择6～24像素的不同高对比图标核心区域；不要包含背景或红点，点击范围须包含这些区域", id)
			}
			for previous := range seen {
				if overlap(previous, a.Rect) {
					return fmt.Errorf("%s：图标核心特征不得重叠", id)
				}
			}
			seen[a.Rect] = true
		}
	}
	if len(r.MainIcons) == 0 {
		return nil
	}
	if len(r.MainIcons) < 3 || r.MinIcons > len(r.MainIcons) {
		return fmt.Errorf("主界面候选至少配置3个图标，至少2个共同通过")
	}
	seen := map[string]bool{}
	for i, id := range r.MainIcons {
		page, ok := r.Profile.Pages[id]
		if !ok || seen[id] || blockers[id] {
			return fmt.Errorf("无效主界面图标 %s", id)
		}
		seen[id] = true
		for _, other := range r.MainIcons[:i] {
			if overlap(r.Profile.Buttons[page.ButtonID], r.Profile.Buttons[r.Profile.Pages[other].ButtonID]) {
				return fmt.Errorf("主界面图标点击范围重叠，不能作为不同图标证据")
			}
		}
	}
	return nil
}

func (r IconRules) Calibrate(img image.Image, id, label, button string, features []Rect, click Rect) (IconRules, error) {
	next := r
	next.Profile = NewProfile()
	for k, v := range r.Profile.Pages {
		next.Profile.Pages[k] = v
	}
	for k, v := range r.Profile.Buttons {
		next.Profile.Buttons[k] = v
	}
	if img == nil || img.Bounds() != image.Rect(0, 0, 960, 540) || id == "" || button == "" {
		return r, fmt.Errorf("需要有效ID和原始960×540截图")
	}
	page := Page{ButtonID: button, Label: label, Revision: r.Profile.Pages[id].Revision + 1, Threshold: 10, CalibrationImageSHA: FrameSHA(img)}
	for _, rect := range features {
		a, err := CaptureAnchor(img, rect)
		if err != nil {
			return r, err
		}
		page.Anchors = append(page.Anchors, a)
	}
	next.Profile.Pages[id] = page
	next.Profile.Buttons[button] = click
	if err := next.Validate(); err != nil {
		return r, err
	}
	return next, nil
}

type IconFrame struct {
	Source, CaptureID string
	Image             image.Image
}
type IconScore struct {
	ID     string      `json:"id"`
	Stable bool        `json:"stable"`
	Frames []PageScore `json:"frames"`
}
type IconAssessment struct {
	CandidateMain bool        `json:"candidate_main"`
	ActionAllowed bool        `json:"action_allowed"`
	StableIcons   []string    `json:"stable_icons"`
	Icons         []IconScore `json:"icons"`
	Reason        string      `json:"reason"`
	MinIcons      int         `json:"min_icons"`
	Frames        int         `json:"frames"`
}

func (r IconRules) Assess(frames []IconFrame) IconAssessment {
	result := IconAssessment{MinIcons: r.MinIcons, Frames: len(frames)}
	if err := r.Validate(); err != nil {
		result.Reason = err.Error()
		return result
	}
	if len(frames) < r.StableFrames || len(frames) > 3 {
		result.Reason = "需要2～3次独立截图，不能用单帧确认稳定性"
		return result
	}
	seen := map[string]bool{}
	for _, f := range frames {
		if f.Source == "" || f.Source != frames[0].Source || f.CaptureID == "" || seen[f.CaptureID] || f.Image == nil || f.Image.Bounds() != image.Rect(0, 0, 960, 540) {
			result.Reason = "实例身份、截图序号或分辨率不一致；停止"
			return result
		}
		seen[f.CaptureID] = true
	}
	blocked := false
	blockerSet := map[string]bool{}
	for _, id := range r.Blockers {
		blockerSet[id] = true
		for _, f := range frames {
			if r.Profile.Score(f.Image, id).Passed {
				blocked = true
			}
		}
	}
	keys := []string{}
	for id := range r.Profile.Pages {
		if !blockerSet[id] {
			keys = append(keys, id)
		}
	}
	sort.Strings(keys)
	stable := map[string]bool{}
	for _, id := range keys {
		s := IconScore{ID: id, Stable: true}
		for _, f := range frames {
			score := r.Profile.Score(f.Image, id)
			s.Frames = append(s.Frames, score)
			s.Stable = s.Stable && score.Passed
		}
		result.Icons = append(result.Icons, s)
		stable[id] = s.Stable
	}
	for _, id := range r.MainIcons {
		if stable[id] {
			result.StableIcons = append(result.StableIcons, id)
		}
	}
	switch {
	case blocked:
		result.Reason = "命中已配置遮挡页面，停止"
	case len(r.MainIcons) == 0:
		result.Reason = "尚未组合主界面图标，当前仅记录单图标评分"
	case len(result.StableIcons) < r.MinIcons:
		result.Reason = fmt.Sprintf("稳定图标%d/%d，至少需要%d；停止", len(result.StableIcons), len(r.MainIcons), r.MinIcons)
	default:
		result.CandidateMain = true
		result.Reason = "多图标连续采样一致；仅诊断候选，未授权游戏操作，仍需真实动态画面及其他页面验收"
	}
	return result
}

// IconContrast reports signal quality, not recognition accuracy.
func IconContrast(a Anchor) float64 {
	if len(a.Pixels) != 512 {
		return 0
	}
	sum, sq := 0., 0.
	for _, v := range a.Pixels {
		sum += float64(v)
		sq += float64(v) * float64(v)
	}
	return math.Sqrt(math.Max(0, sq/512-(sum/512)*(sum/512)))
}
