package taskflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
	"github.com/ab393911/MHX-Studio-Updates/internal/vision"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"testing"
	"time"
)

type quoteReader struct{ calls int }

func (r *quoteReader) Read(ctx context.Context, b []byte) (vision.Screen, error) {
	im, e := automation.Decode(b)
	if e != nil {
		return vision.Screen{}, e
	}
	n := []int{4, 4, 1}[r.calls%3]
	r.calls++
	s := vision.Screen{}
	for i := 0; i < n; i++ {
		rr, _, bb, _ := im.At(20, 8+i*(540/n)).RGBA()
		n := int(rr>>8) + int(bb>>8)*256
		s.Lines = append(s.Lines, vision.Line{Words: []vision.Word{{Text: strconv.Itoa(n), Bounds: automation.Rect{X: 20, Y: 15 + i*(540/[]int{4, 4, 1}[(r.calls-1)%3]), W: 60, H: 24}}}})
	}
	return s, ctx.Err()
}
func quoteFixture(t *testing.T) (*controller, snapshot) {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 960, 540))
	var patch colorPatch
	if e := json.Unmarshal(marketSilverJSON, &patch); e != nil {
		t.Fatal(e)
	}
	for y := 0; y < patch.Rect.H; y++ {
		for x := 0; x < patch.Rect.W; x++ {
			i := (y*patch.Rect.W + x) * 3
			im.SetNRGBA(patch.Rect.X+x, patch.Rect.Y+y, color.NRGBA{patch.Pixels[i], patch.Pixels[i+1], patch.Pixels[i+2], 255})
		}
	}
	for i, r := range quoteAreas() {
		setField(im, r, 1, []int64{1500, 1501, 1506, 1510, 1519, 1524, 1716, 1, 11}[i])
	}
	var b bytes.Buffer
	_ = png.Encode(&b, im)
	s := snapshot{image: im, png: b.Bytes(), sha: "source", at: time.Now()}
	for _, w := range []struct {
		text string
		r    automation.Rect
	}{
		{"孔雀石", automation.Rect{X: 615, Y: 112, W: 90, H: 24}},
		{"上架", automation.Rect{X: 175, Y: 55, W: 60, H: 25}}, {"交易列表", automation.Rect{X: 300, Y: 100, W: 120, H: 24}},
		{"单价", automation.Rect{X: 531, Y: 343, W: 40, H: 20}}, {"数量", automation.Rect{X: 531, Y: 291, W: 40, H: 20}},
		{"上架3天", automation.Rect{X: 623, Y: 432, W: 78, H: 24}},
	} {
		s.ocr.Lines = append(s.ocr.Lines, vision.Line{Words: []vision.Word{{Text: w.text, Bounds: w.r}}})
	}
	return &controller{Actor: Actor{Reader: &quoteReader{}}}, s
}
func TestMarketQuoteReadsPricesWithoutListingCountsOrPurchase(t *testing.T) {
	c, s := quoteFixture(t)
	v, e := c.readMarketQuote(context.Background(), s)
	if e != nil || v.Lowest != 1500 || v.CurrentUnit != 1716 || v.Quantity != 1 || v.Owned != 11 || len(v.OfferPrices) != 6 {
		t.Fatal(v, e)
	}
}
func TestMarketQuoteRejectsWrongCurrencyAndAbsentHeader(t *testing.T) {
	c, s := quoteFixture(t)
	s.image = image.NewNRGBA(image.Rect(0, 0, 960, 540))
	if _, e := c.readMarketQuote(context.Background(), s); e == nil {
		t.Fatal("accepted wrong currency")
	}
	c, s = quoteFixture(t)
	s.ocr = vision.Screen{}
	if _, e := c.readMarketQuote(context.Background(), s); e == nil {
		t.Fatal("accepted unverified page")
	}
}

// The end-to-end fixture forbids every financial button: only navigation to a
// quote and returning to the game are implemented by the fake device.
type marketProbeDevice struct {
	base    *teamDevice
	scene   int
	images  map[int][]byte
	listing snapshot
	taps    int
}

func (d *marketProbeDevice) Capture(ctx context.Context) ([]byte, error) {
	if d.scene == 0 {
		return d.base.Capture(ctx)
	}
	if b := d.images[d.scene]; b != nil {
		return b, ctx.Err()
	}
	im, _ := automation.Decode(d.listing.png)
	v := image.NewNRGBA(im.Bounds())
	for y := 0; y < 540; y++ {
		for x := 0; x < 960; x++ {
			v.Set(x, y, im.At(x, y))
		}
	}
	v.SetNRGBA(0, 0, color.NRGBA{uint8(d.scene), 0, 0, 255})
	var b bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if e := enc.Encode(&b, v); e != nil {
		return nil, e
	}
	d.images[d.scene] = b.Bytes()
	return b.Bytes(), ctx.Err()
}
func (d *marketProbeDevice) Tap(ctx context.Context, x, y int) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	d.taps++
	switch d.scene {
	case 0:
		if x >= 890 && y >= 210 && y < 250 {
			d.scene = 50
			return nil
		}
	case 50:
		if x >= 175 && x < 310 && y >= 48 && y < 93 {
			d.scene = 51
			return nil
		}
	case 51:
		if x >= 181 && x < 227 && y >= 117 && y < 159 {
			d.scene = 52
			return nil
		}
		if x >= 25 && x < 55 && y >= 8 && y < 34 {
			d.scene = 0
			return nil
		}
	case 52:
		if x >= 778 && x < 806 && y >= 51 && y < 79 {
			d.scene = 51
			return nil
		}
	}
	return fmt.Errorf("forbidden market probe tap scene%d (%d,%d)", d.scene, x, y)
}

type marketProbeReader struct {
	base    teamReader
	listing vision.Screen
	amounts quoteReader
}

func (r *marketProbeReader) Read(ctx context.Context, b []byte) (vision.Screen, error) {
	im, e := automation.Decode(b)
	if e != nil {
		return vision.Screen{}, e
	}
	v, _, _, _ := im.At(0, 0).RGBA()
	scene := int(v >> 8)
	if scene == 30 {
		return r.amounts.Read(ctx, b)
	}
	if scene < 50 {
		s, e := r.base.Read(ctx, b)
		s.Lines = append(s.Lines, vision.Line{Words: []vision.Word{{Text: "交易", Bounds: automation.Rect{X: 900, Y: 220, W: 40, H: 24}}}})
		return s, e
	}
	if scene == 52 {
		return r.listing, ctx.Err()
	}
	s := vision.Screen{}
	for _, w := range []struct {
		text string
		r    automation.Rect
	}{
		{"商行", automation.Rect{X: 70, Y: 8, W: 65, H: 25}}, {"购买", automation.Rect{X: 75, Y: 57, W: 65, H: 25}}, {"出售", automation.Rect{X: 210, Y: 57, W: 65, H: 25}},
	} {
		s.Lines = append(s.Lines, vision.Line{Words: []vision.Word{{Text: w.text, Bounds: w.r}}})
	}
	if scene == 51 {
		for _, w := range []struct {
			text string
			r    automation.Rect
		}{{"我的货架", automation.Rect{X: 550, Y: 115, W: 100, H: 24}}, {"我的订单", automation.Rect{X: 870, Y: 485, W: 70, H: 22}}} {
			s.Lines = append(s.Lines, vision.Line{Words: []vision.Word{{Text: w.text, Bounds: w.r}}})
		}
	}
	return s, ctx.Err()
}
func TestMarketProbeNavigatesReadsAndReturnsWithoutFinancialActions(t *testing.T) {
	actors, world := teamFixture(t)
	_, s := quoteFixture(t)
	d := &marketProbeDevice{base: actors[0].Device.(*teamDevice), images: map[int][]byte{}, listing: s}
	actors[0].Device = d
	actors[0].Reader = &marketProbeReader{base: teamReader{world}, listing: s.ocr}
	report, e := InspectMarket(context.Background(), actors, actors[0].Instance)
	if e != nil || !report.Verified || len(report.Actors) != 1 || report.Actors[0].Quote == nil || report.Actors[0].Quote.Lowest != 1500 || d.scene != 0 || d.taps != 5 {
		t.Fatal(report, e, d.scene, d.taps)
	}
	if report.Actors[0].Part.State != studio.Review {
		t.Fatal("inspection pretended market task complete", report)
	}
}
