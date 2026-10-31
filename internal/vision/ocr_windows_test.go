//go:build windows

package vision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
)

// This exercises the actual hidden WinRT bridge on the Windows runner. It uses
// generated Latin text, not game screenshots or a simulated OCR response.
func TestNativeWindowsOCRReadsGeneratedTextWithoutSavingScreenshot(t *testing.T) {
	report := map[string]any{"capability_present": false, "generated_text_verified": false, "real_game_tested": false}
	defer func() {
		if path := os.Getenv("MHX_OCR_REPORT"); path != "" {
			if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
				t.Error(e)
				return
			}
			b, e := json.MarshalIndent(report, "", "  ")
			if e == nil {
				e = os.WriteFile(path, b, 0600)
			}
			if e != nil {
				t.Error(e)
			}
		}
	}()
	gdi := syscall.NewLazyDLL("gdi32.dll")
	dc, _, _ := gdi.NewProc("CreateCompatibleDC").Call(0)
	if dc == 0 {
		t.Fatal("CreateCompatibleDC failed")
	}
	defer gdi.NewProc("DeleteDC").Call(dc)
	type bitmapInfo struct {
		Size                   uint32
		Width, Height          int32
		Planes, BitCount       uint16
		Compression, SizeImage uint32
		XPels, YPels           int32
		Used, Important        uint32
	}
	bi := bitmapInfo{Size: 40, Width: 960, Height: -540, Planes: 1, BitCount: 32, SizeImage: 960 * 540 * 4}
	var ptr unsafe.Pointer
	bmp, _, _ := gdi.NewProc("CreateDIBSection").Call(dc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&ptr)), 0, 0)
	if bmp == 0 || ptr == nil {
		t.Fatal("CreateDIBSection failed")
	}
	defer gdi.NewProc("DeleteObject").Call(bmp)
	old, _, _ := gdi.NewProc("SelectObject").Call(dc, bmp)
	defer gdi.NewProc("SelectObject").Call(dc, old)
	face, _ := syscall.UTF16PtrFromString("Arial")
	font, _, _ := gdi.NewProc("CreateFontW").Call(uintptr(int32ToUint(-46)), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(face)))
	if font == 0 {
		t.Fatal("CreateFont failed")
	}
	defer gdi.NewProc("DeleteObject").Call(font)
	oldFont, _, _ := gdi.NewProc("SelectObject").Call(dc, font)
	defer gdi.NewProc("SelectObject").Call(dc, oldFont)
	pixels := unsafe.Slice((*byte)(ptr), 960*540*4)
	for i := range pixels {
		pixels[i] = 255
	}
	gdi.NewProc("SetTextColor").Call(dc, 0)
	gdi.NewProc("SetBkColor").Call(dc, 0xFFFFFF)
	text := "MHX Studio 123456"
	wide, _ := syscall.UTF16PtrFromString(text)
	ok, _, _ := gdi.NewProc("TextOutW").Call(dc, 80, 90, uintptr(unsafe.Pointer(wide)), uintptr(len(text)))
	if ok == 0 {
		t.Fatal("TextOut failed")
	}
	small, _, _ := gdi.NewProc("CreateFontW").Call(int32ToUint(-16), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(face)))
	if small == 0 {
		t.Fatal("small font creation failed")
	}
	previous, _, _ := gdi.NewProc("SelectObject").Call(dc, small)
	digits, _ := syscall.UTF16PtrFromString("654321")
	ok, _, _ = gdi.NewProc("TextOutW").Call(dc, 80, 200, uintptr(unsafe.Pointer(digits)), 6)
	gdi.NewProc("SelectObject").Call(dc, previous)
	gdi.NewProc("DeleteObject").Call(small)
	if ok == 0 {
		t.Fatal("numeric probe TextOut failed")
	}
	gdi.NewProc("GdiFlush").Call()
	im := image.NewNRGBA(image.Rect(0, 0, 960, 540))
	for i := 0; i < 960*540; i++ {
		im.Pix[i*4] = pixels[i*4+2]
		im.Pix[i*4+1] = pixels[i*4+1]
		im.Pix[i*4+2] = pixels[i*4]
		im.Pix[i*4+3] = 255
	}
	var b bytes.Buffer
	if e := png.Encode(&b, im); e != nil {
		t.Fatal(e)
	}
	r := NewNativeReader()
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	s, e := r.Read(ctx, b.Bytes())
	if e != nil {
		report["error"] = e.Error()
	}
	if errors.Is(e, ErrUnavailable) {
		t.Skip("Runner OCR capability missing; this is NOT an OCR pass:", e)
	}
	if e != nil {
		t.Fatal("WinRT bridge failed:", e)
	}
	report["capability_present"] = true
	report["language"] = s.Language
	if !strings.Contains(Normal(s.Text(automation.Rect{W: 960, H: 540})), "123456") {
		t.Fatal("OCR did not read generated digits", s)
	}
	// A second request must use the same live worker and retain valid coordinates.
	s, e = r.Read(ctx, b.Bytes())
	if e != nil || s.Validate() != nil {
		t.Fatal(s, e)
	}
	report["generated_text_verified"] = true
	trace := &numericProbeReader{Reader: r}
	fields, e := ReadFields(ctx, trace, b.Bytes(), []automation.Rect{{X: 80, Y: 200, W: 130, H: 28}})
	var amount Amount
	if e == nil && len(fields) == 1 {
		amount, e = ParseAmount(fields[0])
	}
	if e != nil || len(fields) != 1 || !amount.Exact || amount.Value != 654321 {
		if path := os.Getenv("MHX_OCR_REPORT"); path != "" {
			_ = os.WriteFile(filepath.Join(filepath.Dir(path), "numeric-ocr-failure.png"), trace.png, 0600)
		}
		t.Fatalf("isolated numeric OCR failed fields=%q amount=%+v err=%v screen=%+v", fields, amount, e, trace.screen)
	}
	report["packed_numeric_verified"] = true
	t.Log("native Windows OCR language", s.Language, "; no real game tested")
}
func int32ToUint(v int32) uintptr { return uintptr(uint32(v)) }

// Probe diagnostics contain only generated test digits, never a game frame.
type numericProbeReader struct {
	Reader
	png    []byte
	screen Screen
}

func (r *numericProbeReader) Read(ctx context.Context, b []byte) (Screen, error) {
	r.png = append([]byte(nil), b...)
	s, e := r.Reader.Read(ctx, b)
	r.screen = s
	return s, e
}
