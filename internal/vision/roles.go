package vision

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
)

// RoleNames reads only the five nickname rows of the evidenced role page.
func RoleNames(s Screen) ([5]string, error) {
	var names [5]string
	if e := s.Validate(); e != nil {
		return names, e
	}
	if !strings.HasPrefix(strings.ToLower(s.Language), "zh") {
		return names, errors.New("当前Windows未提供中文OCR，不能据此自动核对组队昵称")
	}
	seen := map[string]bool{}
	for i, cy := range []int{95, 169, 240, 311, 382} {
		raw := s.Text(automation.Rect{X: 104, Y: cy - 23, W: 100, H: 30})
		if strings.Contains(raw, "\n") {
			return [5]string{}, errors.New("昵称区域包含多行文字，未确认")
		}
		name := Normal(raw)
		if len([]rune(name)) < 2 || len([]rune(name)) > 12 || strings.Contains(name, "级") || strings.Contains(name, "\n") || seen[name] {
			return [5]string{}, errors.New("角色昵称缺失、重复或混入等级，不按顺序猜测")
		}
		for _, r := range name {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '·' && r != '_' {
				return [5]string{}, errors.New("昵称存在未核对字符")
			}
		}
		names[i] = name
		seen[name] = true
	}
	return names, nil
}

// NamedRoleDevice augments the existing nickname calibration, never replaces it.
type NamedRoleDevice struct {
	Device      automation.Device
	Profile     automation.Profile
	Reader      Reader
	Names       [5]string
	Verified    bool
	Error       error
	consecutive int
	disabled    bool
}

func (d *NamedRoleDevice) Tap(ctx context.Context, x, y int) error { return d.Device.Tap(ctx, x, y) }
func (d *NamedRoleDevice) Capture(ctx context.Context) ([]byte, error) {
	b, e := d.Device.Capture(ctx)
	if e != nil {
		return nil, e
	}
	if d.disabled {
		return b, nil
	}
	img, e := automation.Decode(b)
	if e != nil {
		return nil, e
	}
	state, e := d.Profile.Recognize(img)
	if e != nil {
		return b, nil
	}
	if state == "notice" || state == "tree" {
		d.consecutive = 0
		d.Verified = false
		d.Names = [5]string{}
		return b, nil
	}
	if (state != "roles" && !strings.HasPrefix(state, "selected_")) || d.Verified {
		return b, nil
	}
	if d.Reader == nil {
		d.Error = ErrUnavailable
		d.disabled = true
		return b, nil
	}
	s, e := d.Reader.Read(ctx, b)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if e != nil {
		d.Error = e
		d.disabled = errors.Is(e, ErrUnavailable)
		return b, nil
	}
	names, e := RoleNames(s)
	if e != nil {
		d.Error = e
		d.consecutive = 0
		d.disabled = !strings.HasPrefix(strings.ToLower(s.Language), "zh")
		return b, nil
	}
	if names == d.Names {
		d.consecutive++
	} else {
		d.Names = names
		d.consecutive = 1
	}
	d.Verified = d.consecutive >= 2
	if d.Verified {
		d.Error = nil
	}
	return b, nil
}
