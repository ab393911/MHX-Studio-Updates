package vision

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"testing"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
)

type namesDevice struct {
	png  []byte
	taps int
}

func (d *namesDevice) Capture(context.Context) ([]byte, error) { return d.png, nil }
func (d *namesDevice) Tap(context.Context, int, int) error     { d.taps++; return nil }

type namesReader struct {
	calls    int
	language string
	fail     bool
}

func (r *namesReader) Read(context.Context, []byte) (Screen, error) {
	r.calls++
	if r.fail {
		return Screen{}, errors.New("unreadable")
	}
	s := Screen{Language: r.language}
	for i, y := range []int{95, 169, 240, 311, 382} {
		s.Lines = append(s.Lines, Line{Words: []Word{word([]string{"角色甲", "角色乙", "角色丙", "角色丁", "角色戊"}[i], 110, y-15, 80, 18)}})
	}
	return s, nil
}
func namesFixture(t *testing.T) (*NamedRoleDevice, *namesReader, *namesDevice) {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 960, 540))
	var b bytes.Buffer
	if e := png.Encode(&b, im); e != nil {
		t.Fatal(e)
	}
	a, e := automation.CaptureAnchor(im, automation.Rect{X: 10, Y: 10, W: 20, H: 20})
	if e != nil {
		t.Fatal(e)
	}
	p := automation.NewProfile()
	p.Pages["roles"] = automation.Page{Anchors: []automation.Anchor{a, a}, Threshold: 1}
	base := &namesDevice{png: b.Bytes()}
	r := &namesReader{language: "zh-Hans-CN"}
	return &NamedRoleDevice{Device: base, Profile: p, Reader: r, ExpectedProfession: "合欢", VerifyProfession: func(image.Image) (string, error) { return "合欢", nil }, MaxFailures: 1}, r, base
}
func TestNamesAndProfessionNeedTwoFreshFrames(t *testing.T) {
	d, r, _ := namesFixture(t)
	for i := 0; i < 2; i++ {
		if _, e := d.Capture(context.Background()); e != nil {
			t.Fatal(e)
		}
		if d.Verified != (i == 1) || d.ProfessionVerified != (i == 1) {
			t.Fatal(i, d.Verified, d.ProfessionVerified)
		}
	}
	if r.calls != 2 || d.Names[2] != "角色丙" || d.Error != nil {
		t.Fatal(r.calls, d.Names, d.Error)
	}
}
func TestWrongBoundProfessionStopsBeforeNicknameReadOrTap(t *testing.T) {
	d, r, base := namesFixture(t)
	d.VerifyProfession = func(image.Image) (string, error) { return "天音", nil }
	if _, e := d.Capture(context.Background()); e == nil {
		t.Fatal("wrong job accepted")
	}
	if r.calls != 0 || base.taps != 0 || d.Verified || d.ProfessionVerified {
		t.Fatal("wrong actor used")
	}
}
func TestNicknameCapabilityAndRetryFailuresStayUnverified(t *testing.T) {
	for _, mode := range []string{"missing", "language", "retry", "profession"} {
		t.Run(mode, func(t *testing.T) {
			d, r, _ := namesFixture(t)
			switch mode {
			case "missing":
				d.Reader = nil
			case "language":
				r.language = "en-US"
			case "retry":
				r.fail = true
			case "profession":
				d.VerifyProfession = func(image.Image) (string, error) { return "", errors.New("icon unclear") }
			}
			for i := 0; i < 5; i++ {
				if _, e := d.Capture(context.Background()); e != nil {
					t.Fatal(e)
				}
			}
			if d.Verified || d.Error == nil || !d.disabled {
				t.Fatal(d.Verified, d.Error, d.disabled)
			}
			if r.calls > 2 {
				t.Fatal("unbounded OCR retries", r.calls)
			}
		})
	}
}

func TestSkipNicknamesStillConfirmsProfessionWithoutOCR(t *testing.T) {
	d, r, _ := namesFixture(t)
	d.SkipNicknameCheck = true
	r.fail = true
	for range 2 {
		if _, e := d.Capture(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	if r.calls != 0 || !d.ProfessionVerified || d.Verified {
		t.Fatal(r.calls, d.ProfessionVerified, d.Verified)
	}
}
