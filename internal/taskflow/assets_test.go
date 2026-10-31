package taskflow

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
	"github.com/ab393911/MHX-Studio-Updates/internal/vision"
)

type assetDevice struct {
	scene       int
	taps        [][2]int
	helpersOnly bool
	frames      map[int][]byte
}

func (d *assetDevice) frame(scene int) []byte {
	if b := d.frames[scene]; b != nil {
		return b
	}
	im := image.NewNRGBA(image.Rect(0, 0, 960, 540))
	for y := 0; y < 540; y++ {
		for x := 0; x < 960; x++ {
			im.SetNRGBA(x, y, color.NRGBA{uint8(scene * 35), uint8(scene * 35), uint8(scene * 35), 255})
		}
	}
	if scene == 4 {
		for i, r := range assetAreas {
			setField(im, r, 10+i, 100+int64(i))
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, im)
	if d.frames == nil {
		d.frames = map[int][]byte{}
	}
	d.frames[scene] = buf.Bytes()
	return buf.Bytes()
}
func (d *assetDevice) Capture(ctx context.Context) ([]byte, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	return d.frame(d.scene), nil
}
func (d *assetDevice) Tap(ctx context.Context, x, y int) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	d.taps = append(d.taps, [2]int{x, y})
	inside := func(r automation.Rect) bool { return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H }
	switch d.scene {
	case 1:
		if inside(automation.Rect{X: 350, Y: 480, W: 25, H: 25}) {
			d.scene = 2
			return nil
		}
	case 2:
		if inside(automation.Rect{X: 240, Y: 350, W: 160, H: 30}) {
			d.scene = 3
			return nil
		}
		if inside(automation.Rect{X: 552, Y: 355, W: 30, H: 28}) {
			d.scene = 1
			return nil
		}
	case 3:
		if inside(automation.Rect{X: 493, Y: 14, W: 24, H: 27}) {
			d.scene = 4
			return nil
		}
		if inside(automation.Rect{X: 45, Y: 22, W: 28, H: 23}) {
			d.scene = 2
			return nil
		}
	case 4:
		if inside(automation.Rect{X: 781, Y: 56, W: 22, H: 22}) {
			d.scene = 3
			return nil
		}
	}
	return errors.New("unexpected tap; no sending or assistant selection allowed")
}

type assetReader struct {
	device        *assetDevice
	silver, merit string
}

func (r assetReader) Read(ctx context.Context, b []byte) (vision.Screen, error) {
	if e := ctx.Err(); e != nil {
		return vision.Screen{}, e
	}
	im, e := automation.Decode(b)
	if e != nil {
		return vision.Screen{}, e
	}
	screen := vision.Screen{}
	add := func(text string, x, y, w, h int) {
		screen.Lines = append(screen.Lines, vision.Line{Words: []vision.Word{{Text: text, Bounds: automation.Rect{X: x, Y: y, W: w, H: h}}}})
	}
	red, _, _, _ := im.At(0, 0).RGBA()
	if red>>8 == 30 {
		add(r.silver, 20, 8, 200, 45)
		add(r.merit, 20, 278, 200, 45)
		return screen, nil
	}
	switch int(red>>8) / 35 {
	case 2:
		add("好友", 10, 155, 75, 25)
		add("邮箱", 10, 205, 75, 25)
		add("好友上限39/75", 140, 490, 160, 20)
		add("群聊", 345, 20, 140, 25)
		for i := 0; i < 3; i++ {
			add("小助手", 220, 75+i*90, 160, 35)
			add("助", 195, 72+i*90, 15, 15)
			add("手", 195, 90+i*90, 15, 15)
		}
		name := "普通联系人"
		if r.device.helpersOnly {
			name = "小助手"
		}
		add(name, 220, 345, 160, 35)
	case 3:
		add("系统提示", 150, 55, 120, 20)
		add("发给", 190, 493, 70, 25)
	case 4:
		add("赠送礼物", 170, 55, 180, 25)
		add("鲜花", 440, 108, 70, 25)
		add("银锭", 710, 108, 70, 25)
	}
	return screen, nil
}
func newAssetActor(t *testing.T) (Actor, *assetDevice) {
	t.Helper()
	d := &assetDevice{scene: 1}
	im, e := automation.Decode(d.frame(1))
	if e != nil {
		t.Fatal(e)
	}
	p := automation.NewProfile()
	var anchors []automation.Anchor
	for _, r := range []automation.Rect{{X: 10, Y: 10, W: 40, H: 25}, {X: 100, Y: 10, W: 40, H: 25}} {
		a, e := automation.CaptureAnchor(im, r)
		if e != nil {
			t.Fatal(e)
		}
		anchors = append(anchors, a)
	}
	p.Pages["game"] = automation.Page{Anchors: anchors, Threshold: 1}
	return Actor{Instance: 2, Identity: "bound-instance", Server: "一梦诛仙", Role: 3, Device: d, Profile: p, Reader: assetReader{device: d, silver: "32.7万", merit: "345"}}, d
}
func TestAssetReadNavigatesWithoutSendingAndStoresConservativeUnits(t *testing.T) {
	a, d := newAssetActor(t)
	r, e := ReadAssets(context.Background(), a)
	if e != nil {
		t.Fatal(e)
	}
	if d.scene != 1 || len(d.taps) != 6 || !r.ReturnedMain || r.Available != 30000 || r.Silver.Exact || r.Silver.Value != 327000 {
		t.Fatalf("%+v taps=%v", r, d.taps)
	}
	snapshot, e := r.Snapshot()
	if e != nil {
		t.Fatal(e)
	}
	if snapshot.Silver != 320000 || snapshot.Merit != 300 || snapshot.Instance != 2 || snapshot.Slot != 3 || len(snapshot.Evidence) != 64 {
		t.Fatal(snapshot)
	}
	s, e := studio.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for i := 0; i < 2; i++ {
		if e = s.RecordCharacterAsset(snapshot); e != nil {
			t.Fatal(e)
		}
	}
	sum, e := s.ServerAssetSummary([]string{a.Server}, []int{2}, time.Now(), 15*time.Minute)
	if e != nil || sum[0].Available != 30000 || sum[0].Scanned != 1 {
		t.Fatal(sum, e)
	}
}
func TestAssetReadRejectsHelpersAndUnknownPageWithoutGuessing(t *testing.T) {
	for _, test := range []string{"helpers", "unknown"} {
		t.Run(test, func(t *testing.T) {
			a, d := newAssetActor(t)
			if test == "helpers" {
				d.helpersOnly = true
			} else {
				d.scene = 5
			}
			_, e := ReadAssets(context.Background(), a)
			if e == nil {
				t.Fatal("accepted unknown contact/page")
			}
			want := 0
			if test == "helpers" {
				want = 1
			}
			if len(d.taps) != want {
				t.Fatal(d.taps)
			}
		})
	}
}
func TestAssetAbbreviationBoundariesNeverProduceFalseCapacity(t *testing.T) {
	for _, s := range []string{"32万", "32.0万", "bad", "-1"} {
		t.Run(s, func(t *testing.T) {
			a, d := newAssetActor(t)
			a.Reader = assetReader{device: d, silver: s, merit: "345"}
			ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
			defer cancel()
			r, e := ReadAssets(ctx, a)
			if e == nil || r.ReturnedMain {
				t.Fatal(r, e)
			}
			if len(d.taps) != 3 {
				t.Fatal("uncertain read navigated further", d.taps)
			}
		})
	}
}
func TestAssetStopDoesNotClick(t *testing.T) {
	a, d := newAssetActor(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := ReadAssets(ctx, a)
	if !errors.Is(e, context.Canceled) || len(d.taps) != 0 {
		t.Fatal(e, d.taps)
	}
}
func TestUnreturnedAssetCannotBecomeSnapshot(t *testing.T) {
	_, e := (AssetReading{}).Snapshot()
	if e == nil {
		t.Fatal("missing query accepted")
	}
}
func TestWorkObservationRejectsUnknownCostAndVitality(t *testing.T) {
	s := snapshot{ocr: vision.Screen{Lines: []vision.Line{{Words: []vision.Word{{Text: "710/1000", Bounds: automation.Rect{X: 754, Y: 5, W: 63, H: 30}}}}}}}
	n, e := vitality(s)
	if e != nil || n != 710 {
		t.Fatal(n, e)
	}
	if _, e = workCost(s); e == nil {
		t.Fatal("invented work cost")
	}
}
func TestUnsupportedModuleCannotBeCompleted(t *testing.T) {
	r, e := RunModule(context.Background(), nil, 0, "market", nil)
	if e == nil || r.Verified || !strings.Contains(e.Error(), "执行器") {
		t.Fatal(r, e)
	}
}
