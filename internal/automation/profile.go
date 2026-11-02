package automation

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"time"
)

type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

func (r Rect) Valid() bool {
	return r.W >= 6 && r.H >= 6 && r.X >= 0 && r.Y >= 0 && r.X+r.W <= 960 && r.Y+r.H <= 540
}

type Anchor struct {
	Rect   Rect   `json:"rect"`
	Pixels []byte `json:"pixels"`
}

const RuleAlgorithmVersion = "gray32x16-mae-v1"

type Page struct {
	ButtonID            string    `json:"button_id,omitempty"`
	Label               string    `json:"label,omitempty"`
	Revision            int       `json:"revision,omitempty"`
	FeatureThresholds   []float64 `json:"feature_thresholds,omitempty"`
	MinMatches          int       `json:"min_matches,omitempty"`
	CalibrationImageSHA string    `json:"calibration_image_sha256,omitempty"`
	Anchors             []Anchor  `json:"anchors"`
	Threshold           float64   `json:"threshold"`
}
type Profile struct {
	Schema  int             `json:"schema"`
	Pages   map[string]Page `json:"pages"`
	Buttons map[string]Rect `json:"buttons"`
}

func NewProfile() Profile {
	return Profile{Schema: 1, Pages: map[string]Page{}, Buttons: map[string]Rect{}}
}
func Decode(b []byte) (image.Image, error) {
	c, e := png.DecodeConfig(bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	if c.Width != 960 || c.Height != 540 {
		return nil, fmt.Errorf("screen is %dx%d; this profile requires landscape 960x540", c.Width, c.Height)
	}
	return png.Decode(bytes.NewReader(b))
}
func sample(img image.Image, r Rect) []byte {
	p := make([]byte, 0, 32*16)
	for y := 0; y < 16; y++ {
		for x := 0; x < 32; x++ {
			rr, g, b, _ := img.At(r.X+x*(r.W-1)/31, r.Y+y*(r.H-1)/15).RGBA()
			p = append(p, byte((299*rr+587*g+114*b)/1000>>8))
		}
	}
	return p
}
func CaptureAnchor(img image.Image, r Rect) (Anchor, error) {
	if !r.Valid() {
		return Anchor{}, errors.New("anchor must be inside the 960x540 image and at least 6x6")
	}
	return Anchor{r, sample(img, r)}, nil
}
func (p Profile) Validate() error {
	if p.Schema != 1 || p.Pages == nil || p.Buttons == nil {
		return errors.New("invalid recognition profile")
	}
	for name, page := range p.Pages {
		if !page.valid() {
			return fmt.Errorf("%s needs 2..8 anchors, valid per-feature thresholds 1..20 and at least two required matches", name)
		}
		if page.ButtonID != "" {
			if _, ok := p.Buttons[page.ButtonID]; !ok {
				return fmt.Errorf("%s missing button %s", name, page.ButtonID)
			}
		}
		for _, a := range page.Anchors {
			if !a.Rect.Valid() || len(a.Pixels) != 512 {
				return errors.New("invalid image anchor")
			}
		}
	}
	for _, r := range p.Buttons {
		if !r.Valid() {
			return errors.New("invalid button bounds")
		}
	}
	return nil
}

// AnchorScore reports the existing grayscale metric without relaxing its limits.
type OffsetScore struct {
	DX                      int     `json:"dx"`
	DY                      int     `json:"dy"`
	MeanAbsoluteError       float64 `json:"mean_absolute_error"`
	ChangedPixels           int     `json:"changed_pixels"`
	WouldPassExistingLimits bool    `json:"would_pass_existing_limits"`
}
type AnchorScore struct {
	Rect                  Rect         `json:"rect"`
	MeanAbsoluteError     float64      `json:"mean_absolute_error"`
	MeanLimit             float64      `json:"mean_limit"`
	ChangedPixels         int          `json:"changed_pixels"`
	ChangedPixelLimit     int          `json:"changed_pixel_limit"`
	PixelDeltaLimit       int          `json:"pixel_delta_limit"`
	Passed                bool         `json:"passed"`
	Error                 string       `json:"error,omitempty"`
	ReferencePixels       []byte       `json:"reference_pixels,omitempty"`
	CurrentPixels         []byte       `json:"current_pixels,omitempty"`
	BorderMeanError       float64      `json:"border_mean_error,omitempty"`
	InteriorMeanError     float64      `json:"interior_mean_error,omitempty"`
	BorderChangedPixels   int          `json:"border_changed_pixels,omitempty"`
	InteriorChangedPixels int          `json:"interior_changed_pixels,omitempty"`
	BrightnessDelta       float64      `json:"brightness_delta,omitempty"`
	BestOffsetDiagnostic  *OffsetScore `json:"best_offset_diagnostic,omitempty"`
}

func (page Page) valid() bool {
	if len(page.Anchors) < 2 || len(page.Anchors) > 8 || math.IsNaN(page.Threshold) || math.IsInf(page.Threshold, 0) || page.Threshold < 1 || page.Threshold > 20 || page.Revision < 0 || len(page.Label) > 128 {
		return false
	}
	if page.MinMatches != 0 && (page.MinMatches < 2 || page.MinMatches > len(page.Anchors)) {
		return false
	}
	if len(page.FeatureThresholds) != 0 && len(page.FeatureThresholds) != len(page.Anchors) {
		return false
	}
	for _, t := range page.FeatureThresholds {
		if math.IsNaN(t) || math.IsInf(t, 0) || t < 1 || t > 20 {
			return false
		}
	}
	return true
}
func (page Page) RequiredMatches() int {
	if page.MinMatches == 0 {
		return len(page.Anchors)
	}
	return page.MinMatches
}

type PageScore struct {
	Label               string        `json:"label"`
	ButtonID            string        `json:"button_id,omitempty"`
	CalibrationImageSHA string        `json:"calibration_image_sha256,omitempty"`
	Revision            int           `json:"revision"`
	RequiredMatches     int           `json:"required_matches"`
	MatchedFeatures     int           `json:"matched_features"`
	Confidence          float64       `json:"feature_pass_ratio"`
	Name                string        `json:"name"`
	Anchors             []AnchorScore `json:"anchors"`
	Passed              bool          `json:"passed"`
	Error               string        `json:"error,omitempty"`
}

func (p Profile) Score(img image.Image, name string) PageScore {
	result := PageScore{Name: name}
	page, ok := p.Pages[name]
	if !ok || !page.valid() {
		result.Error = "page missing or invalid recognition rule"
		return result
	}
	if img == nil || img.Bounds() != image.Rect(0, 0, 960, 540) {
		result.Error = "requires original 960x540 image, origin 0,0; no scaling"
		return result
	}
	result.Label = page.Label
	if result.Label == "" {
		result.Label = name
	}
	result.ButtonID = page.ButtonID
	result.CalibrationImageSHA = page.CalibrationImageSHA
	result.Revision = page.Revision
	if result.Revision == 0 {
		result.Revision = 1
	}
	result.RequiredMatches = page.RequiredMatches()
	for featureIndex, a := range page.Anchors {
		score := AnchorScore{Rect: a.Rect, MeanLimit: page.Threshold, ChangedPixelLimit: 40, PixelDeltaLimit: 40}
		if len(page.FeatureThresholds) > 0 {
			score.MeanLimit = page.FeatureThresholds[featureIndex]
		}
		if !a.Rect.Valid() || len(a.Pixels) != 512 {
			score.Error = "invalid stored anchor"
			result.Passed = false
			result.Anchors = append(result.Anchors, score)
			continue
		}
		now := sample(img, a.Rect)
		sum, bad := 0, 0
		for i, v := range now {
			d := int(v) - int(a.Pixels[i])
			if d < 0 {
				d = -d
			}
			sum += d
			if d > 40 {
				bad++
			}
		}
		score.MeanAbsoluteError = float64(sum) / 512
		score.ChangedPixels = bad
		score.Passed = score.MeanAbsoluteError <= score.MeanLimit && bad <= 40
		if score.Passed {
			result.MatchedFeatures++
		} else {
			score.Error = fmt.Sprintf("mean %.3f/%.3f; changed pixels %d/40", score.MeanAbsoluteError, score.MeanLimit, bad)
		}
		result.Anchors = append(result.Anchors, score)
	}
	result.Confidence = float64(result.MatchedFeatures) / float64(len(page.Anchors))
	result.Passed = result.MatchedFeatures >= result.RequiredMatches
	for _, a := range result.Anchors {
		if a.Error == "invalid stored anchor" {
			result.Passed = false
		}
	}
	if !result.Passed {
		result.Error = fmt.Sprintf("matched %d/%d features; requires %d", result.MatchedFeatures, len(page.Anchors), result.RequiredMatches)
	}
	return result
}

// Explain adds read-only diagnostics. Offset comparisons NEVER change Passed,
// which always comes from the original fixed-coordinate, unchanged-limit test.
func (p Profile) Explain(img image.Image, name string) PageScore {
	report := p.Score(img, name)
	if name != "game" || img == nil || img.Bounds() != image.Rect(0, 0, 960, 540) {
		return report
	}
	page, ok := p.Pages[name]
	if !ok || !page.valid() {
		return report
	}
	for i := range report.Anchors {
		score := &report.Anchors[i]
		if score.Error == "invalid stored anchor" {
			continue
		}
		ref := page.Anchors[i].Pixels
		now := sample(img, score.Rect)
		score.ReferencePixels = append([]byte(nil), ref...)
		score.CurrentPixels = now
		borderSum, insideSum, borderN, insideN, signed := 0, 0, 0, 0, 0
		for n, v := range now {
			d := int(v) - int(ref[n])
			signed += d
			if d < 0 {
				d = -d
			}
			x, y := n%32, n/32
			if x < 2 || x >= 30 || y < 2 || y >= 14 {
				borderSum += d
				borderN++
				if d > 40 {
					score.BorderChangedPixels++
				}
			} else {
				insideSum += d
				insideN++
				if d > 40 {
					score.InteriorChangedPixels++
				}
			}
		}
		score.BorderMeanError = float64(borderSum) / float64(borderN)
		score.InteriorMeanError = float64(insideSum) / float64(insideN)
		score.BrightnessDelta = float64(signed) / 512
		best := OffsetScore{MeanAbsoluteError: 1e9}
		for dy := -2; dy <= 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
				r := score.Rect
				r.X += dx
				r.Y += dy
				if !r.Valid() {
					continue
				}
				pixels := sample(img, r)
				sum, bad := 0, 0
				for n, v := range pixels {
					d := int(v) - int(ref[n])
					if d < 0 {
						d = -d
					}
					sum += d
					if d > 40 {
						bad++
					}
				}
				candidate := OffsetScore{DX: dx, DY: dy, MeanAbsoluteError: float64(sum) / 512, ChangedPixels: bad}
				candidate.WouldPassExistingLimits = candidate.MeanAbsoluteError <= score.MeanLimit && bad <= score.ChangedPixelLimit
				if candidate.MeanAbsoluteError < best.MeanAbsoluteError || (candidate.MeanAbsoluteError == best.MeanAbsoluteError && candidate.ChangedPixels < best.ChangedPixels) {
					best = candidate
				}
			}
		}
		score.BestOffsetDiagnostic = &best
	}
	return report
}

func (p Profile) Match(img image.Image, name string) bool { return p.Score(img, name).Passed }

// Require rejects competing character identities, even when a generic page matches.
func (p Profile) Require(img image.Image, name string) error {
	if !p.Match(img, name) {
		return p.failure(img, fmt.Sprintf("state %s not verified", name))
	}
	var prefix string
	if len(name) > 5 && name[:5] == "game_" {
		prefix = "game_"
	}
	if len(name) > 9 && name[:9] == "selected_" {
		prefix = "selected_"
	}
	if prefix != "" {
		for i := 1; i <= 5; i++ {
			other := fmt.Sprintf("%s%d", prefix, i)
			if other != name && p.Match(img, other) {
				return errors.New("ambiguous character identity; recalibrate name/highlight anchors")
			}
		}
	}
	return nil
}
func bounded(n int) int {
	if n <= 1 {
		return 0
	}
	var b [8]byte
	if _, e := rand.Read(b[:]); e != nil {
		return n / 2
	}
	return int(binary.LittleEndian.Uint64(b[:]) % uint64(n))
}
func Point(r Rect) (int, int, error) {
	if !r.Valid() {
		return 0, 0, errors.New("button bounds not calibrated")
	}
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	rx := int(math.Min(8, float64((r.W-2)/4)))
	ry := int(math.Min(6, float64((r.H-2)/4)))
	return cx + bounded(rx*2+1) - rx, cy + bounded(ry*2+1) - ry, nil
}
func Delay(min, max int) time.Duration {
	return time.Duration(min+bounded(max-min+1)) * time.Millisecond
}

// FrameSHA is independent of PNG encoding and compares full original pixels.
// It is diagnostic evidence, never a requirement that a dynamic game frame stay identical.
func FrameSHA(img image.Image) string {
	if img == nil || img.Bounds() != image.Rect(0, 0, 960, 540) {
		return ""
	}
	h := sha256.New()
	row := make([]byte, 960*3)
	for y := 0; y < 540; y++ {
		for x := 0; x < 960; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			row[x*3] = byte(r >> 8)
			row[x*3+1] = byte(g >> 8)
			row[x*3+2] = byte(b >> 8)
		}
		_, _ = h.Write(row)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
