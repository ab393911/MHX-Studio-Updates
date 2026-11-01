package taskflow

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
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
