package taskflow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/vision"
)

const (
	testMain    = 1
	testParty   = 2
	testFixed   = 3
	testConfirm = 4
	testInvite  = 5
	testNotice  = 6
)

type teamWorld struct {
	mu              sync.Mutex
	state           [5]int
	accepted        [5]bool
	invited         bool
	wrongInvite     bool
	wrongFixed      bool
	multipleInvites bool
	taps, accepts   int
	names           [5]string
	images          map[string][]byte
	onTap           func(int)
}
type teamDevice struct {
	world *teamWorld
	id    int
}

func (w *teamWorld) copy() (state [5]int, accepted [5]bool, invited bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state, w.accepted, w.invited
}
func (d *teamDevice) Capture(ctx context.Context) ([]byte, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	state, accepted, invited := d.world.copy()
	s := state[d.id]
	if s == testMain && invited && !accepted[d.id] {
		s = testNotice
	}
	key := fmt.Sprintf("%d:%d:%v:%v", d.id, s, accepted, invited)
	d.world.mu.Lock()
	cached := d.world.images[key]
	d.world.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	im := image.NewRGBA(image.Rect(0, 0, 960, 540))
	im.SetRGBA(0, 0, color.RGBA{uint8(s), uint8(d.id), 0, 255})
	for i, v := range accepted {
		if v {
			im.SetRGBA(i, 1, color.RGBA{255, 0, 0, 255})
		}
	}
	if invited {
		im.SetRGBA(6, 1, color.RGBA{255, 0, 0, 255})
	}
	if s == testMain || s == testNotice {
		for _, r := range []automation.Rect{{X: 10, Y: 10, W: 80, H: 24}, {X: 120, Y: 10, W: 80, H: 24}} {
			for y := r.Y; y < r.Y+r.H; y++ {
				for x := r.X; x < r.X+r.W; x++ {
					v := uint8(100 + d.id*20)
					im.SetRGBA(x, y, color.RGBA{v, v, v, 255})
				}
			}
		}
	}
	if s == testNotice {
		for _, p := range vision.InvitationIcon().Pixels {
			im.SetRGBA(401+p.X, 377+p.Y, color.RGBA{p.R, p.G, p.B, 255})
		}
	}
	var b bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if e := encoder.Encode(&b, im); e != nil {
		return nil, e
	}
	d.world.mu.Lock()
	if d.world.images == nil {
		d.world.images = map[string][]byte{}
	}
	d.world.images[key] = b.Bytes()
	d.world.mu.Unlock()
	return b.Bytes(), nil
}
func (d *teamDevice) Tap(ctx context.Context, x, y int) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	w := d.world
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.state[d.id]
	w.taps++
	if w.onTap != nil {
		w.onTap(w.taps)
	}
	switch s {
	case testMain:
		if w.invited && !w.accepted[d.id] && x >= 401 && x < 449 && y >= 377 && y < 423 {
			w.state[d.id] = testInvite
			return nil
		}
		if x >= 920 && x < 944 && y >= 208 && y < 234 {
			w.state[d.id] = testParty
			return nil
		}
	case testParty:
		if x >= 24 && x < 56 && y >= 8 && y < 34 {
			w.state[d.id] = testMain
			return nil
		}
		if x >= 870 && y >= 140 && y < 215 {
			w.state[d.id] = testFixed
			return nil
		}
		if x >= 870 && y >= 65 && y < 140 {
			return nil
		}
	case testFixed:
		if x >= 700 && x < 855 && y >= 450 && y < 515 {
			w.state[d.id] = testConfirm
			return nil
		}
		if x >= 870 && y >= 65 && y < 140 {
			w.state[d.id] = testParty
			return nil
		}
	case testConfirm:
		if x >= 500 && x < 660 && y >= 350 && y < 405 {
			w.state[d.id] = testParty
			w.invited = true
			w.accepted[0] = true
			return nil
		}
	case testInvite:
		if x >= 580 && x < 656 && y >= 125 && y < 160 {
			w.state[d.id] = testMain
			w.accepted[d.id] = true
			w.accepts++
			return nil
		}
	}
	return fmt.Errorf("unexpected tap actor%d state%d at %d,%d", d.id, s, x, y)
}

type teamReader struct{ world *teamWorld }

func (r teamReader) Read(ctx context.Context, b []byte) (vision.Screen, error) {
	if e := ctx.Err(); e != nil {
		return vision.Screen{}, e
	}
	im, e := png.Decode(bytes.NewReader(b))
	if e != nil {
		return vision.Screen{}, e
	}
	rr, gg, _, _ := im.At(0, 0).RGBA()
	state, id := int(rr>>8), int(gg>>8)
	s := vision.Screen{Language: "zh-Hans-CN"}
	add := func(text string, x, y, w, h int) {
		s.Lines = append(s.Lines, vision.Line{Words: []vision.Word{{Text: text, Bounds: automation.Rect{X: x, Y: y, W: w, H: h}}}})
	}
	r.world.mu.Lock()
	names := r.world.names
	wrongFixed, wrongInvite := r.world.wrongFixed, r.world.wrongInvite
	ir, _, _, _ := im.At(6, 1).RGBA()
	invited := ir > 0
	r.world.mu.Unlock()
	switch state {
	case testParty, testFixed:
		add("队伍", 75, 10, 50, 24)
		add("固定", 890, 170, 35, 22)
		add("队伍", 890, 90, 35, 22)
		if state == testFixed {
			for i, n := range names {
				if wrongFixed && i == 4 {
					n = "其他角色"
				}
				add(n, 120+160*i, 263, 70, 18)
				add("在线", 120+160*i, 340, 40, 18)
			}
			add("发起组队", 715, 466, 100, 25)
		} else {
			add("队长", 50, 145, 20, 45)
			if !invited {
				add("没有队伍的时候", 20, 470, 140, 20)
				add("创建队伍", 580, 465, 110, 25)
				add(names[id], 90, 260, 75, 18)
			} else {
				add("离开队伍", 580, 465, 110, 25)
				for i, n := range names {
					r, _, _, _ := im.At(i, 1).RGBA()
					if r > 0 {
						add(n, 90+170*i, 260, 75, 18)
					}
				}
			}
		}
	case testConfirm:
		add("提醒", 290, 125, 60, 25)
		add("是否创建队伍并邀请固定队成员入队？", 340, 245, 290, 22)
		add("确定", 530, 370, 85, 24)
	case testInvite:
		add("邀请列表", 300, 35, 110, 25)
		add("队伍", 315, 80, 55, 23)
		n := names[0]
		if wrongInvite {
			n = "陌生队长"
		}
		add(n, 310, 115, 100, 20)
		add("接受", 590, 130, 55, 25)
		r.world.mu.Lock()
		multiple := r.world.multipleInvites
		r.world.mu.Unlock()
		if multiple {
			add("接受", 590, 190, 55, 25)
		}
	}
	return s, nil
}
func teamFixture(t *testing.T) ([]Actor, *teamWorld) {
	t.Helper()
	w := &teamWorld{names: [5]string{"角色甲", "角色乙", "角色丙", "角色丁", "角色戊"}}
	var actors []Actor
	for i, n := range []int{2, 6, 9, 17, 24} {
		w.state[i] = testMain
		d := &teamDevice{w, i}
		b, e := d.Capture(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		im, _ := automation.Decode(b)
		a, _ := automation.CaptureAnchor(im, automation.Rect{X: 10, Y: 10, W: 80, H: 24})
		c, _ := automation.CaptureAnchor(im, automation.Rect{X: 120, Y: 10, W: 80, H: 24})
		p := automation.NewProfile()
		p.Pages["game_3"] = automation.Page{Anchors: []automation.Anchor{a, c}, Threshold: 2}
		actors = append(actors, Actor{Instance: n, Identity: fmt.Sprint("bound-", n), Server: "一梦诛仙", Name: w.names[i], Profession: []string{"合欢", "天音", "青云", "鬼王", "鬼王"}[i], Role: 3, Device: d, Reader: teamReader{w}, Profile: p, MinDelayMS: 100, MaxDelayMS: 100})
	}
	return actors, w
}
func TestTeamInvitesAcceptsVerifiesActualFiveAndReturnsAllToMain(t *testing.T) {
	actors, w := teamFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	defer cancel()
	r, e := RunTeam(ctx, actors, 2)
	if e != nil || !r.Verified || len(r.Members) != 5 {
		t.Fatal(r, e)
	}
	state, accepted, _ := w.copy()
	for i := range actors {
		if state[i] != testMain || !accepted[i] {
			t.Fatal(state, accepted)
		}
	}
	w.mu.Lock()
	count := w.accepts
	w.mu.Unlock()
	if count != 4 {
		t.Fatal("not four invitations", count)
	}
}
func TestAlreadyCorrectPartyDoesNotInviteAgain(t *testing.T) {
	actors, w := teamFixture(t)
	w.invited = true
	for i := range w.accepted {
		w.accepted[i] = true
	}
	r, e := RunTeam(context.Background(), actors, 2)
	if e != nil || !r.Verified {
		t.Fatal(r, e)
	}
	if w.accepts != 0 {
		t.Fatal("already-correct party accepted again")
	}
}
func TestTeamRejectsCopiedOrMismatchedIdentityBeforeAnyTap(t *testing.T) {
	for _, kind := range []string{"identity", "role", "server", "leader"} {
		t.Run(kind, func(t *testing.T) {
			a, w := teamFixture(t)
			leader := 2
			switch kind {
			case "name":
				a[1].Name = a[0].Name
			case "identity":
				a[1].Identity = a[0].Identity
			case "role":
				a[1].Role = 2
			case "server":
				a[1].Server = "别服"
			case "leader":
				leader = 99
			}
			if _, e := RunTeam(context.Background(), a, leader); e == nil || w.taps != 0 {
				t.Fatal(kind, e, w.taps)
			}
		})
	}
}
func TestFixedPresetDoesNotRequireNicknameEquality(t *testing.T) {
	a, w := teamFixture(t)
	w.wrongFixed = true
	w.state[0] = testFixed
	c := &controller{Actor: a[0]}
	s, e := c.take(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if e = fixedTeam(a, "")(s); e != nil {
		t.Fatal(e)
	}
	for i := range a {
		a[i].Name = ""
	}
	if _, _, e := validateActors(a, a[0].Instance); e != nil {
		t.Fatal(e)
	}
}

func TestStopDuringNavigationPreventsAnyFurtherTapOrErrorPersistence(t *testing.T) {
	a, w := teamFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.onTap = func(n int) {
		if n == 1 {
			cancel()
		}
	}
	errorsSaved := 0
	for i := range a {
		a[i].OnError = func([]byte, error) { errorsSaved++ }
	}
	r, e := RunTeam(ctx, a, 2)
	if !errors.Is(e, context.Canceled) || r.Verified || w.taps != 1 || errorsSaved != 0 {
		t.Fatal(r, e, w.taps, errorsSaved)
	}
}
func TestInviteDoesNotRequireNicknameAndUserCancellationNeverClicks(t *testing.T) {
	a, w := teamFixture(t)
	w.wrongInvite = true
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, e := RunTeam(ctx, a, 2)
	if e != nil || !r.Verified || w.accepts != 4 {
		t.Fatal(r, e, w.accepts)
	}
	a, w = teamFixture(t)
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	_, e = RunTeam(ctx, a, 2)
	if !errors.Is(e, context.Canceled) || w.taps != 0 {
		t.Fatal(e, w.taps)
	}
}

func TestMultipleAcceptableInvitesStopBeforeAccepting(t *testing.T) {
	a, w := teamFixture(t)
	w.multipleInvites = true
	w.state[1] = testInvite
	c := &controller{Actor: a[1]}
	if e := c.accept(context.Background(), ""); e == nil || w.accepts != 0 || w.taps != 0 {
		t.Fatal(e, w.accepts, w.taps)
	}
}
func TestFiveOccupiedSlotsAreRequiredEvenWithoutNames(t *testing.T) {
	a, w := teamFixture(t)
	w.invited = true
	w.state[0] = testParty
	for i := 0; i < 4; i++ {
		w.accepted[i] = true
	}
	c := &controller{Actor: a[0]}
	s, e := c.take(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if e = fullTeam(a, "")(s); e == nil {
		t.Fatal("four members accepted as five")
	}
}
