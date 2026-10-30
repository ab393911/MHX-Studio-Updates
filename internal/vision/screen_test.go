package vision

import (
	"image"
	"image/color"
	"testing"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
)

func word(text string, x, y, w, h int) Word {
	return Word{Text: text, Bounds: automation.Rect{X: x, Y: y, W: w, H: h}}
}
func TestExactTextJoiningAndRegionDisambiguation(t *testing.T) {
	s := Screen{Lines: []Line{{Words: []Word{word("发起", 700, 460, 30, 20), word("组队", 732, 460, 30, 20)}}, {Words: []Word{word("发起组队", 100, 50, 65, 20)}}}}
	r, e := s.Unique("发起组队", automation.Rect{X: 680, Y: 440, W: 150, H: 70})
	if e != nil || r.X != 700 || r.W != 62 {
		t.Fatal(r, e)
	}
	if _, e = s.Unique("发起组队", automation.Rect{W: 960, H: 540}); e == nil {
		t.Fatal("ambiguous label allowed")
	}
	if _, e = s.Unique("组", automation.Rect{W: 960, H: 540}); e == nil {
		t.Fatal("partial word allowed")
	}
	s.Lines[0].Words[0].Bounds.X = -1
	if s.Validate() == nil {
		t.Fatal("invalid OCR coordinates accepted")
	}
}
func TestRoleNicknameRowsExcludeLevelsAndRequireChineseEngine(t *testing.T) {
	s := Screen{Language: "zh-Hans-CN"}
	for i, y := range []int{95, 169, 240, 311, 382} {
		s.Lines = append(s.Lines, Line{Words: []Word{word([]string{"角色甲", "角色乙", "角色丙", "角色丁", "角色戊"}[i], 110, y-15, 80, 18)}})
		s.Lines = append(s.Lines, Line{Words: []Word{word("69级", 110, y+8, 40, 15)}})
	}
	names, e := RoleNames(s)
	if e != nil || names[2] != "角色丙" {
		t.Fatal(names, e)
	}
	s.Language = "en-US"
	if _, e = RoleNames(s); e == nil {
		t.Fatal("English engine claimed Chinese nicknames")
	}
	s.Language = "zh-Hans-CN"
	s.Lines[2].Words[0].Text = "角色甲"
	if _, e = RoleNames(s); e == nil {
		t.Fatal("duplicate names accepted")
	}
}
func TestInvitationMaskFindsOddPositionAndRejectsDuplicates(t *testing.T) {
	i := InvitationIcon()
	img := image.NewRGBA(image.Rect(0, 0, 960, 540))
	draw := func(x, y int) {
		for _, p := range i.Pixels {
			img.SetRGBA(x+p.X, y+p.Y, color.RGBA{p.R, p.G, p.B, 255})
		}
	}
	draw(401, 377)
	r, e := i.Find(img)
	if e != nil || r.X != 401 || r.Y != 377 {
		t.Fatal(r, e)
	}
	// Counter region is not in the mask and may change freely.
	for y := 0; y < 15; y++ {
		for x := 28; x < 48; x++ {
			img.SetRGBA(401+x, 377+y, color.RGBA{220, 0, 0, 255})
		}
	}
	if _, e = i.Find(img); e != nil {
		t.Fatal("dynamic counter rejected", e)
	}
	draw(701, 177)
	if _, e = i.Find(img); e == nil {
		t.Fatal("two invitations guessed")
	}
	if _, e = i.Find(image.NewRGBA(image.Rect(0, 0, 960, 540))); e == nil {
		t.Fatal("blank screen matched")
	}
}
