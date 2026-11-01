package vision

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"regexp"
	"strconv"
	"strings"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
)

const MaxAmount int64 = 9_000_000_000_000_000

type Amount struct {
	Raw                 string
	Value, Lower, Upper int64
	Exact               bool
}

var amountPattern = regexp.MustCompile(`^([0-9]+)(?:\.([0-9]{1,4}))?(万)?$`)

// Abbreviated UI values are intervals, not invented exact balances. The range
// covers truncation and rounding by one displayed unit in either direction.
func ParseAmount(raw string) (Amount, error) {
	s := Normal(raw)
	m := amountPattern.FindStringSubmatch(s)
	var out Amount
	out.Raw = raw
	if m == nil {
		return out, errors.New("数字格式未确认")
	}
	n, e := strconv.ParseInt(m[1], 10, 64)
	if e != nil || n > MaxAmount {
		return out, errors.New("数字超出范围")
	}
	if m[3] == "" {
		if m[2] != "" {
			return out, errors.New("数量不得含小数")
		}
		return Amount{Raw: raw, Value: n, Lower: n, Upper: n, Exact: true}, nil
	}
	if n > MaxAmount/10000 {
		return out, errors.New("数字超出范围")
	}
	value := n * 10000
	step := int64(10000)
	if m[2] != "" {
		for range len(m[2]) {
			step /= 10
		}
		frac, _ := strconv.ParseInt(m[2], 10, 64)
		value += frac * step
	}
	if value > MaxAmount {
		return out, errors.New("数字超出范围")
	}
	if step == 1 {
		return Amount{Raw: raw, Value: value, Lower: value, Upper: value, Exact: true}, nil
	}
	return Amount{Raw: raw, Value: value, Lower: max(0, value-step), Upper: min(MaxAmount, value+step), Exact: false}, nil
}
func (a Amount) ExactInteger() (int64, error) {
	if !a.Exact {
		return 0, errors.New("界面使用万缩写，不能当作精确余额")
	}
	return a.Value, nil
}
func (a Amount) RoundDown(unit int64) (int64, error) {
	if unit <= 0 || a.Lower/unit != a.Upper/unit {
		return 0, errors.New("显示精度不足以确认取整额度")
	}
	return a.Lower / unit * unit, nil
}

// ReadAmounts enlarges up to four small fields onto separate rows in a single
// in-memory image. This avoids treating a material count as its adjacent 8★
// grade, or a market unit price as the listing's quantity. No screenshot file
// or temporary external OCR service is involved.
func ReadFields(ctx context.Context, reader Reader, frame []byte, areas []automation.Rect) ([]string, error) {
	return readFields(ctx, reader, frame, areas, 0)
}
func readFields(ctx context.Context, reader Reader, frame []byte, areas []automation.Rect, threshold uint8) ([]string, error) {
	if reader == nil || len(areas) == 0 || len(areas) > 4 {
		return nil, errors.New("数字识别需要1到4个独立字段")
	}
	img, e := automation.Decode(frame)
	if e != nil {
		return nil, e
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, 960, 540))
	for i := 0; i < len(canvas.Pix); i += 4 {
		background := uint8(30)
		if threshold != 0 {
			background = 255
		}
		canvas.Pix[i], canvas.Pix[i+1], canvas.Pix[i+2], canvas.Pix[i+3] = background, background, background, 255
	}
	var reads []automation.Rect
	rowH := 540 / len(areas)
	for i, r := range areas {
		if !inside(r, automation.Rect{W: 960, H: 540}) {
			return nil, errors.New("数字字段越界")
		}
		scale := min(6, 920/r.W, (rowH-16)/r.H)
		if scale < 2 {
			return nil, errors.New("数字字段过大，未与相邻信息隔离")
		}
		dest := automation.Rect{X: 20, Y: i*rowH + 8, W: r.W * scale, H: r.H * scale}
		// Windows OCR can extend a word box several pixels beyond the crop
		// (e.g. X=16 for content starting at X=20). This entire packed row
		// belongs exclusively to one field; never relax source UI boundaries.
		reads = append(reads, automation.Rect{X: 0, Y: i * rowH, W: 960, H: rowH})
		for y := 0; y < dest.H; y++ {
			for x := 0; x < dest.W; x++ {
				rr, g, b, _ := img.At(r.X+x/scale, r.Y+y/scale).RGBA()
				if threshold == 0 {
					canvas.SetNRGBA(dest.X+x, dest.Y+y, color.NRGBA{uint8(rr >> 8), uint8(g >> 8), uint8(b >> 8), 255})
				} else {
					// Isolate the white UI lettering from the coloured HUD background.
					// No character replacements or numeric inference are performed.
					v := uint8(255)
					if min(rr, g, b)>>8 >= uint32(threshold) {
						v = 0
					}
					canvas.SetNRGBA(dest.X+x, dest.Y+y, color.NRGBA{v, v, v, 255})
				}
			}
		}
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	var pngBytes bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if e = encoder.Encode(&pngBytes, canvas); e != nil {
		return nil, e
	}
	s, e := reader.Read(ctx, pngBytes.Bytes())
	if e != nil {
		return nil, e
	}
	if e = s.Validate(); e != nil {
		return nil, e
	}
	var out []string
	for _, r := range reads {
		raw := s.Text(r)
		if strings.Contains(raw, "\n") {
			return nil, errors.New("数字区域出现多行信息，未确认")
		}
		out = append(out, raw)
	}
	return out, nil
}

func parseAmounts(raw []string) ([]Amount, error) {
	var out []Amount
	for _, text := range raw {
		n, e := ParseAmount(text)
		if e != nil {
			return nil, e
		}
		out = append(out, n)
	}
	return out, nil
}
func ReadAmounts(ctx context.Context, reader Reader, frame []byte, areas []automation.Rect) ([]Amount, error) {
	raw, e := ReadFields(ctx, reader, frame, areas)
	if e != nil {
		return nil, e
	}
	direct, e := parseAmounts(raw)
	if e == nil {
		return direct, nil
	}
	// An invalid glyph is never silently changed to a digit. Two independently
	// thresholded images must produce identical valid values and precision.
	var confirmed []Amount
	for _, threshold := range []uint8{180, 210} {
		alternate, err := readFields(ctx, reader, frame, areas, threshold)
		if err != nil {
			return nil, err
		}
		amounts, err := parseAmounts(alternate)
		if err != nil {
			return nil, errors.New("数字原图和二值图均未读清，不猜测余额")
		}
		if confirmed == nil {
			confirmed = amounts
			continue
		}
		for i, n := range amounts {
			p := confirmed[i]
			if p.Value != n.Value || p.Exact != n.Exact || p.Lower != n.Lower || p.Upper != n.Upper {
				return nil, errors.New("两种数字图像读数不一致，不写余额")
			}
		}
	}
	// Even a valid field from the original must agree with fallback results.
	for i, text := range raw {
		if p, err := ParseAmount(text); err == nil {
			n := confirmed[i]
			if p.Value != n.Value || p.Exact != n.Exact || p.Lower != n.Lower || p.Upper != n.Upper {
				return nil, errors.New("数字原图与二值图冲突，不写余额")
			}
		}
	}
	return confirmed, nil
}
