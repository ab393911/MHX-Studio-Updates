// Package vision contains bounded, in-memory OCR results. A text match is a
// candidate observation; controllers must combine it with page/identity checks
// and a second independent capture before a critical action.
package vision

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
)

var ErrUnavailable = errors.New("本机Windows文字识别不可用")

type Word struct {
	Text   string          `json:"text"`
	Bounds automation.Rect `json:"bounds"`
}
type Line struct {
	Words []Word `json:"words"`
}
type Screen struct {
	Language string `json:"language"`
	Lines    []Line `json:"lines"`
}
type Reader interface {
	Read(context.Context, []byte) (Screen, error)
}

func Normal(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		switch r {
		case '＊', '✩', '☆':
			return '★'
		case '：':
			return ':'
		}
		return r
	}, s)
}
func inside(r, area automation.Rect) bool {
	return r.W > 0 && r.H > 0 && r.X >= area.X && r.Y >= area.Y && r.X+r.W <= area.X+area.W && r.Y+r.H <= area.Y+area.H
}
func union(a, b automation.Rect) automation.Rect {
	x := min(a.X, b.X)
	y := min(a.Y, b.Y)
	return automation.Rect{X: x, Y: y, W: max(a.X+a.W, b.X+b.W) - x, H: max(a.Y+a.H, b.Y+b.H) - y}
}
func (s Screen) Validate() error {
	if len(s.Lines) > 200 {
		return errors.New("OCR行数超限")
	}
	n := 0
	for _, l := range s.Lines {
		for _, w := range l.Words {
			n++
			if n > 3000 || len(w.Text) > 1024 || !inside(w.Bounds, automation.Rect{W: 960, H: 540}) {
				return errors.New("OCR文字或坐标无效")
			}
		}
	}
	return nil
}
func (s Screen) Text(area automation.Rect) string {
	var out []string
	for _, l := range s.Lines {
		var parts []string
		for _, w := range l.Words {
			if inside(w.Bounds, area) {
				parts = append(parts, w.Text)
			}
		}
		if len(parts) > 0 {
			out = append(out, strings.Join(parts, ""))
		}
	}
	return strings.Join(out, "\n")
}

// Unique requires an exact phrase made from contiguous OCR words inside the
// supplied region; an unrelated same-name control elsewhere cannot be clicked.
func (s Screen) Unique(text string, area automation.Rect) (automation.Rect, error) {
	if err := s.Validate(); err != nil {
		return automation.Rect{}, err
	}
	needle := Normal(text)
	if needle == "" {
		return automation.Rect{}, errors.New("空识别目标")
	}
	var found []automation.Rect
	for _, l := range s.Lines {
		for i, w := range l.Words {
			if !inside(w.Bounds, area) {
				continue
			}
			combined := ""
			box := w.Bounds
			for j := i; j < len(l.Words) && j < i+24; j++ {
				v := l.Words[j]
				if !inside(v.Bounds, area) {
					break
				}
				combined += Normal(v.Text)
				box = union(box, v.Bounds)
				if combined == needle {
					duplicate := false
					for _, r := range found {
						if r == box {
							duplicate = true
						}
					}
					if !duplicate {
						found = append(found, box)
					}
					break
				}
				if !strings.HasPrefix(needle, combined) {
					break
				}
			}
		}
	}
	if len(found) != 1 {
		return automation.Rect{}, errors.New("目标文字缺失或不唯一：" + text)
	}
	if !found[0].Valid() {
		return automation.Rect{}, errors.New("目标文字可点击范围过小：" + text)
	}
	return found[0], nil
}
