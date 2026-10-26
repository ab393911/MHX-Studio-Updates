package automation

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"
)

func TestRandomClicksStayInsideButtons(t *testing.T) {
	r := Rect{X: 900, Y: 480, W: 18, H: 16}
	for i := 0; i < 1000; i++ {
		x, y, e := Point(r)
		if e != nil || x < r.X || x >= r.X+r.W || y < r.Y || y >= r.Y+r.H {
			t.Fatal(x, y, e)
		}
	}
}
func TestSkippedTaskBlocksServerChange(t *testing.T) {
	p := Progress{Entries: map[string]string{}}
	for i := 1; i <= 5; i++ {
		for r := 1; r <= 5; r++ {
			p.Entries[Key(i, "server-A", r, "daily")] = "verified_complete"
		}
	}
	ids := []int{1, 2, 3, 4, 5}
	if !p.CanChangeServer(ids, "server-A", []string{"daily"}) {
		t.Fatal("complete blocked")
	}
	p.Entries[Key(5, "server-A", 5, "daily")] = "skipped"
	if p.CanChangeServer(ids, "server-A", []string{"daily"}) {
		t.Fatal("skipped accepted as completed")
	}
	if p.CanChangeServer(nil, "", nil) {
		t.Fatal("empty gate opened")
	}
}
func TestCacheBounded(t *testing.T) {
	c := NewCache(2, 8<<20)
	for i := 0; i < 20; i++ {
		c.Add("a", make([]byte, 1<<20))
		c.Add("b", make([]byte, 1<<20))
		c.Add("c", make([]byte, 1<<20))
	}
	n, b := c.Size()
	if n > 6 || b > 8<<20 {
		t.Fatal(n, b)
	}
}
func TestAnchorsAndAmbiguity(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 960, 540))
	p := NewProfile()
	a, _ := CaptureAnchor(img, Rect{10, 10, 80, 30})
	b, _ := CaptureAnchor(img, Rect{300, 100, 60, 25})
	p.Pages["game_1"] = Page{[]Anchor{a, b}, 10}
	if e := p.Require(img, "game_1"); e != nil {
		t.Fatal(e)
	}
	p.Pages["game_2"] = p.Pages["game_1"]
	if e := p.Require(img, "game_1"); e == nil {
		t.Fatal("ambiguous roles accepted")
	}
	for y := 10; y < 40; y++ {
		for x := 10; x < 90; x++ {
			img.Set(x, y, color.White)
		}
	}
	if p.Match(img, "game_1") {
		t.Fatal("changed screen accepted")
	}
}

type unknownDevice struct {
	png  []byte
	taps int
}

func (d *unknownDevice) Capture(context.Context) ([]byte, error) { return d.png, nil }
func (d *unknownDevice) Tap(context.Context, int, int) error     { d.taps++; return nil }
func TestUncalibratedScreenNeverClicks(t *testing.T) {
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 960, 540)))
	d := &unknownDevice{png: b.Bytes()}
	e := Engine{Device: d, Profile: NewProfile(), Options: Options{Retries: 2, MinDelayMS: 100, MaxDelayMS: 150, StateTimeout: time.Millisecond}}
	if e.Cycle(context.Background(), []int{1}) == nil {
		t.Fatal("missing profile accepted")
	}
	if d.taps != 0 {
		t.Fatal("unknown page clicked")
	}
}
