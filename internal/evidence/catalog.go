package evidence

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"math"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
)

//go:embed catalog.json
var raw []byte

type Page struct {
	Key, Label string
	SourceID   int    `json:"source_id"`
	SourceSHA  string `json:"source_sha256"`
	Page       automation.Page
	Button     string
	Bounds     *automation.Rect
	Login      bool
	Derived    string
}
type Icon struct {
	Profession    string
	Width, Height int
	Pixels        []byte
	SourceID      int    `json:"source_id"`
	SourceSHA     string `json:"source_sha256"`
}
type Catalog struct {
	Schema int
	ZipSHA string `json:"zip_sha256"`
	Pages  []Page
	Icons  []Icon
}

func Load() (Catalog, error) {
	var c Catalog
	e := json.Unmarshal(raw, &c)
	if e != nil {
		return c, e
	}
	if c.Schema != 1 {
		return c, errors.New("invalid evidence catalog")
	}
	p := automation.NewProfile()
	for _, v := range c.Pages {
		p.Pages[v.Key] = v.Page
		if v.Bounds != nil {
			p.Buttons[v.Button] = *v.Bounds
		}
	}
	return c, p.Validate()
}
func (c Catalog) Profile(local automation.Profile) automation.Profile {
	p := automation.NewProfile()
	for _, v := range c.Pages {
		if v.Login {
			p.Pages[v.Key] = v.Page
			if v.Bounds != nil {
				p.Buttons[v.Button] = *v.Bounds
			}
		}
	}
	for k, v := range local.Pages {
		p.Pages[k] = v
	}
	for k, v := range local.Buttons {
		p.Buttons[k] = v
	}
	return p
}
func (c Catalog) Matches(img image.Image) []Page {
	if img.Bounds() != image.Rect(0, 0, 960, 540) {
		return nil
	}
	p := automation.NewProfile()
	var out []Page
	for _, v := range c.Pages {
		p.Pages[v.Key] = v.Page
		if p.Match(img, v.Key) {
			out = append(out, v)
		}
	}
	return out
}
func (c Catalog) Match(img image.Image, key string) bool {
	for _, p := range c.Matches(img) {
		if p.Key == key {
			return true
		}
	}
	return false
}
func (c Catalog) Label(key string) string {
	for _, p := range c.Pages {
		if p.Key == key {
			return p.Label
		}
	}
	return key
}
func normalized(img image.Image, r image.Rectangle) ([]float64, error) {
	if !r.In(img.Bounds()) {
		return nil, errors.New("icon outside image")
	}
	minx, miny, maxx, maxy := r.Max.X, r.Max.Y, r.Min.X, r.Min.Y
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			rr, g, b, _ := img.At(x, y).RGBA()
			hi := max(rr, g, b) >> 8
			lo := min(rr, g, b) >> 8
			if hi > 150 && hi-lo > 55 {
				minx = min(minx, x)
				miny = min(miny, y)
				maxx = max(maxx, x)
				maxy = max(maxy, y)
			}
		}
	}
	if maxx-minx < 5 || maxy-miny < 5 {
		return nil, errors.New("profession shape not found")
	}
	var out []float64
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			r, g, b, _ := img.At(minx+x*(maxx-minx)/15, miny+y*(maxy-miny)/15).RGBA()
			out = append(out, float64(r>>8), float64(g>>8), float64(b>>8))
		}
	}
	return out, nil
}
func (c Catalog) Profession(img image.Image, r image.Rectangle) (string, float64, error) {
	sample, e := normalized(img, r)
	if e != nil {
		return "", 0, e
	}
	best, second := 1e9, 1e9
	job := ""
	for _, v := range c.Icons {
		ref := image.NewRGBA(image.Rect(0, 0, v.Width, v.Height))
		if len(v.Pixels) != v.Width*v.Height*3 {
			return "", 0, errors.New("invalid icon template")
		}
		for i := 0; i < v.Width*v.Height; i++ {
			copy(ref.Pix[i*4:i*4+3], v.Pixels[i*3:i*3+3])
			ref.Pix[i*4+3] = 255
		}
		s, e := normalized(ref, ref.Bounds())
		if e != nil {
			continue
		}
		sum := 0.0
		for i := range s {
			sum += math.Abs(s[i] - sample[i])
		}
		score := sum / float64(len(s))
		if score < best {
			second = best
			best = score
			job = v.Profession
		} else if score < second {
			second = score
		}
	}
	if best > 46 || second-best < 3 {
		return "", best, errors.New("职业形状/颜色证据不唯一")
	}
	if second-best < 7 {
		return job, best, errors.New("单行职业边界不够清晰，须联合其它四行核验")
	}

	return job, best, nil
}
func (c Catalog) FiveProfessions(img image.Image) (string, error) {
	if !c.Match(img, "roles") {
		return "", errors.New("角色页未确认")
	}
	job := ""
	weak := 0
	for _, cy := range []int{95, 169, 240, 311, 382} {
		j, score, e := c.Profession(img, image.Rect(207, cy-14, 234, cy+14))
		if e != nil {
			if j == "" || score > 35 {
				return "", e
			}
			weak++
		}
		if job != "" && j != job {
			return "", errors.New("同一实例五个角色职业不一致")
		}
		job = j
	}
	if weak > 1 {
		return "", errors.New("职业联合证据不足")
	}
	return job, nil
}

// LoginDevice closes only the evidenced ad frame/X. Variable artwork is not
// sampled. Unknown guides/modals are returned to the guarded engine and stop it.
type LoginDevice struct {
	Device                          automation.Device
	Catalog                         Catalog
	Retries, MinDelayMS, MaxDelayMS int
	OnLog                           func(string)
}

func (d *LoginDevice) Tap(ctx context.Context, x, y int) error { return d.Device.Tap(ctx, x, y) }
func sleep(ctx context.Context, t time.Duration) error {
	timer := time.NewTimer(t)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func (d *LoginDevice) Capture(ctx context.Context) ([]byte, error) {
	for n := 0; n <= d.Retries; n++ {
		b, e := d.Device.Capture(ctx)
		if e != nil {
			return nil, e
		}
		img, e := automation.Decode(b)
		if e != nil {
			return nil, e
		}
		if !d.Catalog.Match(img, "advert") {
			return b, nil
		}
		var box automation.Rect
		for _, p := range d.Catalog.Pages {
			if p.Key == "advert" && p.Bounds != nil {
				box = *p.Bounds
			}
		}
		if d.OnLog != nil {
			d.OnLog("已识别广告外框与关闭按钮，准备关闭；未使用广告画面内容")
		}
		if e = sleep(ctx, automation.Delay(d.MinDelayMS, d.MaxDelayMS)); e != nil {
			return nil, e
		}
		fresh, e := d.Device.Capture(ctx)
		if e != nil {
			return nil, e
		}
		im, e := automation.Decode(fresh)
		if e != nil {
			return nil, e
		}
		if !d.Catalog.Match(im, "advert") {
			return nil, errors.New("广告状态变化，未点击")
		}
		x, y, e := automation.Point(box)
		if e != nil {
			return nil, e
		}
		if e = d.Device.Tap(ctx, x, y); e != nil {
			return nil, e
		}
		// Two consecutive absent frames are required before proceeding. A click
		// acknowledgement is deliberately not an ad-close acknowledgement.
		wait, cancel := context.WithTimeout(ctx, 8*time.Second)
		stable := 0
		for {
			b, e = d.Device.Capture(wait)
			if e != nil {
				cancel()
				return nil, e
			}
			img, e = automation.Decode(b)
			if e != nil {
				cancel()
				return nil, e
			}
			if !d.Catalog.Match(img, "advert") {
				stable++
			} else {
				stable = 0
			}
			if stable == 2 {
				cancel()
				if d.OnLog != nil {
					d.OnLog("广告消失已连续两帧核验")
				}
				return b, nil
			}
			if e = sleep(wait, 250*time.Millisecond); e != nil {
				cancel()
				break
			}
		}
	}
	return nil, fmt.Errorf("advertisement did not close")
}
