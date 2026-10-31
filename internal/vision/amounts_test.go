package vision

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
)

func TestAbbreviatedBalanceIsNotReportedAsExactMoney(t *testing.T) {
	a, e := ParseAmount("32.7万")
	if e != nil || a.Exact || a.Lower != 326000 || a.Upper != 328000 {
		t.Fatal(a, e)
	}
	if _, e = a.ExactInteger(); e == nil {
		t.Fatal("abbreviated balance invented exact digits")
	}
	if n, e := a.RoundDown(10000); e != nil || n != 320000 {
		t.Fatal(n, e)
	}
	a, _ = ParseAmount("1.0万")
	if _, e = a.RoundDown(10000); e == nil {
		t.Fatal("rounding crossed a delivery boundary")
	}
	for _, s := range []string{"-1", "12O0", "1.2", "1万2", "10000在售", "999999999999999999999"} {
		if _, e = ParseAmount(s); e == nil {
			t.Fatal("uncertain field accepted", s)
		}
	}
	a, e = ParseAmount("3.0001万")
	if e != nil || !a.Exact || a.Value != 30001 {
		t.Fatal(a, e)
	}
}

type amountReader struct{ t *testing.T }

func (r amountReader) Read(ctx context.Context, b []byte) (Screen, error) {
	im, e := automation.Decode(b)
	if e != nil {
		return Screen{}, e
	}
	var s Screen
	for i, text := range []string{"0", "1", "0", "1"} {
		v, _, _, _ := im.At(20, 8+135*i).RGBA()
		if uint8(v>>8) != uint8(80+i*20) {
			r.t.Fatal("wrong source field packed", i, v)
		}
		s.Lines = append(s.Lines, Line{Words: []Word{{Text: text, Bounds: automation.Rect{X: 16, Y: 14 + 135*i, W: 30, H: 40}}}})
	}
	return s, ctx.Err()
}
func TestFourNumericFieldsKeepIsolationAndAllowOCRBoxOverhang(t *testing.T) {
	im := image.NewRGBA(image.Rect(0, 0, 960, 540))
	var areas []automation.Rect
	for i := 0; i < 4; i++ {
		r := automation.Rect{X: 350 + i*70, Y: 360, W: 20, H: 20}
		areas = append(areas, r)
		for y := r.Y; y < r.Y+r.H; y++ {
			for x := r.X; x < r.X+r.W; x++ {
				im.SetRGBA(x, y, color.RGBA{uint8(80 + i*20), 0, 0, 255})
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, im)
	got, e := ReadAmounts(context.Background(), amountReader{t}, b.Bytes(), areas)
	if e != nil || len(got) != 4 {
		t.Fatal(got, e)
	}
	for i, n := range got {
		if !n.Exact || n.Value != int64(i%2) {
			t.Fatal(got)
		}
	}
}
