package automation

import (
	"context"
	"errors"
	"fmt"
	"image"
	"strings"
	"time"
)

var ErrIdentityUnverified = errors.New("已进入游戏，但角色身份未核验；不能报告为指定角色登录成功")

type EnterResult struct {
	Role                 int  `json:"role"`
	EnteredGame          bool `json:"entered_game"`
	SelectedRoleVerified bool `json:"selected_role_verified"`
	IdentityVerified     bool `json:"identity_verified"`
}
type LoginStatus struct{ Page, Step string }
type EnterOptions struct {
	OnStatus    func(LoginStatus)
	InitialWait time.Duration
}

// Recognize uses one frame for all comparisons. A selected-row template can
// overlap its generic role-list page, and a named character can overlap the
// generic game page. Competing page types or character identities are unsafe.
func (p Profile) Recognize(img image.Image) (string, error) {
	if img == nil || img.Bounds() != image.Rect(0, 0, 960, 540) {
		return "", errors.New("页面识别只接受960×540横屏截图")
	}
	var base, selected, game []string
	for _, name := range []string{"notice", "tree", "roles", "menu", "settings", "logout_confirm", "game"} {
		if p.Match(img, name) {
			base = append(base, name)
		}
	}
	for i := 1; i <= 5; i++ {
		if n := fmt.Sprintf("selected_%d", i); p.Match(img, n) {
			selected = append(selected, n)
		}
		if n := fmt.Sprintf("game_%d", i); p.Match(img, n) {
			game = append(game, n)
		}
	}
	if len(selected) > 1 || len(game) > 1 {
		return "", errors.New("页面同时匹配多个角色，请重新标定昵称或选中高亮")
	}
	if len(selected) == 1 {
		for _, b := range base {
			if b != "roles" {
				return "", fmt.Errorf("页面识别有冲突：%s 与 %s", selected[0], b)
			}
		}
		if len(game) > 0 {
			return "", errors.New("角色选择与游戏内画面识别冲突")
		}
		return selected[0], nil
	}
	if len(game) == 1 {
		for _, b := range base {
			if b != "game" {
				return "", fmt.Errorf("页面识别有冲突：%s 与 %s", game[0], b)
			}
		}
		return game[0], nil
	}
	if len(base) == 1 {
		return base[0], nil
	}
	if len(base) > 1 {
		return "", fmt.Errorf("页面同时匹配 %v，未点击", base)
	}
	return "", errors.New("未知页面：请用截图标定稳定特征；未点击")
}

func (e *Engine) EnterRole(ctx context.Context, role int, opt EnterOptions) (result EnterResult, err error) {
	result.Role = role
	defer func() {
		if err != nil && !errors.Is(err, context.Canceled) && e.Options.OnError != nil {
			e.Options.OnError(e.last, err)
		}
	}()
	status := func(page, step string) {
		e.log(step)
		if opt.OnStatus != nil {
			opt.OnStatus(LoginStatus{page, step})
		}
	}
	if role < 1 || role > 5 {
		return result, errors.New("目标角色须为1～5")
	}
	if e.Options.Retries < 0 || e.Options.Retries > 20 || e.Options.MinDelayMS < 100 || e.Options.MaxDelayMS < e.Options.MinDelayMS || e.Options.MaxDelayMS > 3000 {
		return result, errors.New("重试或随机延迟设置无效")
	}
	if err = e.Profile.Validate(); err != nil {
		return result, err
	}
	selected := fmt.Sprintf("selected_%d", role)
	game := fmt.Sprintf("game_%d", role)
	for _, key := range []string{"roles", selected} {
		if !e.has(key) {
			return result, fmt.Errorf("缺少页面标定 %s；游戏可单独启动，但未进行登录点击", key)
		}
	}
	if !e.has(game) && !e.has("game") {
		return result, errors.New("缺少游戏主界面标定：请标定本角色昵称+稳定界面特征；未进行登录点击")
	}
	for _, key := range []string{fmt.Sprintf("role_%d", role), "enter"} {
		if _, ok := e.Profile.Buttons[key]; !ok {
			return result, fmt.Errorf("缺少按钮范围标定 %s", key)
		}
	}
	if opt.InitialWait > 0 {
		if opt.InitialWait > 45*time.Second {
			return result, errors.New("startup wait exceeds safety bound")
		}
		wait, cancel := context.WithTimeout(ctx, opt.InitialWait)
		status("loading", "等待游戏启动页面，按截图状态判断，不点击")
		for {
			img, er := e.image(wait)
			if er == nil {
				_, er = e.Profile.Recognize(img)
			}
			if er == nil {
				break
			}
			if er = pause(wait, 350*time.Millisecond); er != nil {
				cancel()
				return result, fmt.Errorf("游戏启动后页面未确认：%w", er)
			}
		}
		cancel()
	}
	state, err := e.loginObserve(ctx, status)
	if err != nil {
		return result, err
	}
	if state == game {
		result.EnteredGame = true
		result.IdentityVerified = true
		status(state, "当前已在目标角色内，昵称画面已核验；没有重新启动或退出")
		return result, nil
	}
	if state == "game" {
		result.EnteredGame = true
		return result, ErrIdentityUnverified
	}
	// An explicitly recognized other character may return to login. No server
	// switch, character creation, task progress or battle action exists here.
	for step := 0; step < 10; step++ {
		switch {
		case state == "roles" || strings.HasPrefix(state, "selected_"):
			if state != selected {
				status(state, fmt.Sprintf("正在选择第%d个角色，等待选中标记验证", role))
				state, err = e.loginTransition(ctx, state, fmt.Sprintf("role_%d", role), []string{selected}, status)
				if err != nil {
					return result, err
				}
			}
			result.SelectedRoleVerified = true
			status(state, "目标角色选中已验证，正在进入游戏")
			state, err = e.loginTransition(ctx, state, "enter", []string{game, "game"}, status)
			if err != nil {
				return result, err
			}
			result.EnteredGame = true
			result.IdentityVerified = state == game
			if !result.IdentityVerified {
				return result, ErrIdentityUnverified
			}
			status(state, fmt.Sprintf("已进入第%d个角色，游戏内身份画面已核验；已停止操作", role))
			return result, nil
		case state == "notice":
			state, err = e.loginTransition(ctx, state, "notice_enter", []string{"tree", "roles", selected}, status)
		case state == "tree":
			state, err = e.loginTransition(ctx, state, "tree_enter", []string{"roles", selected}, status)
		case strings.HasPrefix(state, "game_"):
			state, err = e.loginTransition(ctx, state, "menu", []string{"menu"}, status)
		case state == "menu":
			state, err = e.loginTransition(ctx, state, "settings", []string{"settings"}, status)
		case state == "settings":
			state, err = e.loginTransition(ctx, state, "logout", []string{"logout_confirm", "notice", "tree", "roles", selected}, status)
		case state == "logout_confirm":
			state, err = e.loginTransition(ctx, state, "confirm_logout", []string{"notice", "tree", "roles", selected}, status)
		default:
			return result, fmt.Errorf("无法从 %s 安全进入角色，已停止", state)
		}
		if err != nil {
			return result, err
		}
	}
	return result, errors.New("登录页面转换超过安全步数，已停止")
}
func (e *Engine) loginObserve(ctx context.Context, status func(string, string)) (string, error) {
	var last error
	for n := 0; n <= e.Options.Retries; n++ {
		img, err := e.image(ctx)
		if err != nil {
			return "", err
		}
		state, err := e.Profile.Recognize(img)
		if err == nil {
			return state, nil
		}
		last = err
		status("unknown", fmt.Sprintf("画面未确认，等待重试 %d/%d，不点击", n, e.Options.Retries))
		if n < e.Options.Retries {
			if err = pause(ctx, 500*time.Millisecond); err != nil {
				return "", err
			}
		}
	}
	return "", last
}
func containsState(all []string, s string) bool {
	for _, a := range all {
		if a == s || (a == "roles" && strings.HasPrefix(s, "selected_")) {
			return true
		}
	}
	return false
}
func (e *Engine) loginTransition(ctx context.Context, before, button string, after []string, status func(string, string)) (string, error) {
	r, ok := e.Profile.Buttons[button]
	if !ok || !r.Valid() {
		return "", fmt.Errorf("缺少按钮标定 %s；已停止", button)
	}
	var possible []string
	for _, a := range after {
		if e.has(a) {
			possible = append(possible, a)
		}
	}
	if len(possible) == 0 {
		return "", fmt.Errorf("缺少 %s 点击后的目标画面标定，未点击", button)
	}
	var last error
	for n := 0; n <= e.Options.Retries; n++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		img, err := e.image(ctx)
		if err != nil {
			return "", err
		}
		state, err := e.Profile.Recognize(img)
		if err != nil {
			return "", err
		}
		if containsState(possible, state) {
			return state, nil
		}
		// Role selection may remain on a different highlighted role after a failed
		// attempt. It is safe to retry only on a verified role-list state.
		allowed := state == before || (strings.HasPrefix(button, "role_") && (state == "roles" || strings.HasPrefix(state, "selected_")))
		if !allowed {
			return "", fmt.Errorf("页面变为 %s，期望 %s，停止点击", state, before)
		}
		if err = pause(ctx, Delay(e.Options.MinDelayMS, e.Options.MaxDelayMS)); err != nil {
			return "", err
		}
		img, err = e.image(ctx)
		if err != nil {
			return "", err
		}
		fresh, err := e.Profile.Recognize(img)
		if err != nil {
			return "", err
		}
		if fresh != state {
			return "", errors.New("随机等待期间页面变化，未点击")
		}
		x, y, err := Point(r)
		if err != nil {
			return "", err
		}
		status(state, fmt.Sprintf("%s：尝试 %d/%d，等待实际页面变化", button, n+1, e.Options.Retries+1))
		if err = e.Device.Tap(ctx, x, y); err != nil {
			return "", err
		}
		timeout := e.Options.StateTimeout
		if timeout <= 0 {
			timeout = 25 * time.Second
		}
		wait, cancel := context.WithTimeout(ctx, timeout)
		stable := ""
		for {
			img, er := e.image(wait)
			if er != nil {
				cancel()
				return "", er
			}
			current, er := e.Profile.Recognize(img)
			if er == nil && containsState(possible, current) {
				if stable == current {
					cancel()
					status(current, "页面变化已连续验证："+current)
					return current, nil
				}
				stable = current
			} else {
				stable = ""
			}
			last = er
			if er = pause(wait, 250*time.Millisecond); er != nil {
				last = errors.Join(last, er)
				break
			}
		}
		cancel()
	}
	return "", fmt.Errorf("%s 在 %d 次尝试后未验证目标页面，已停止：%w", button, e.Options.Retries+1, last)
}
