//go:build windows

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

// Read physical desktop pixels. No PrintWindow, WM_PRINT or forced WM_PAINT.
func captureScreenRegion(rect image.Rectangle, path string) error {
	width, height := rect.Dx(), rect.Dy()
	if width < 1 || height < 1 || width > 3000 || height > 3000 {
		return fmt.Errorf("invalid screen capture rectangle %v", rect)
	}
	gdi := syscall.NewLazyDLL("gdi32.dll")
	dc, _, e := user.NewProc("GetDC").Call(0)
	if dc == 0 {
		return e
	}
	defer user.NewProc("ReleaseDC").Call(0, dc)
	mem, _, e := gdi.NewProc("CreateCompatibleDC").Call(dc)
	if mem == 0 {
		return e
	}
	defer gdi.NewProc("DeleteDC").Call(mem)
	bitmap, _, e := gdi.NewProc("CreateCompatibleBitmap").Call(dc, uintptr(width), uintptr(height))
	if bitmap == 0 {
		return e
	}
	defer gdi.NewProc("DeleteObject").Call(bitmap)
	old, _, _ := gdi.NewProc("SelectObject").Call(mem, bitmap)
	ok, _, e := gdi.NewProc("BitBlt").Call(mem, 0, 0, uintptr(width), uintptr(height), dc, uintptr(rect.Min.X), uintptr(rect.Min.Y), 0x40CC0020)
	gdi.NewProc("SelectObject").Call(mem, old)
	if ok == 0 {
		return e
	}
	header := struct {
		Size                   uint32
		Width, Height          int32
		Planes, Bits           uint16
		Compression, SizeImage uint32
		X, Y                   int32
		Used, Important        uint32
	}{Size: 40, Width: int32(width), Height: -int32(height), Planes: 1, Bits: 32}
	raw := make([]byte, width*height*4)
	n, _, e := gdi.NewProc("GetDIBits").Call(dc, bitmap, 0, uintptr(height), uintptr(unsafe.Pointer(&raw[0])), uintptr(unsafe.Pointer(&header)), 0)
	if n == 0 {
		return e
	}
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < width*height; i++ {
		img.SetNRGBA(i%width, i/width, color.NRGBA{raw[i*4+2], raw[i*4+1], raw[i*4], 255})
	}
	file, e := os.Create(path)
	if e != nil {
		return e
	}
	e = png.Encode(file, img)
	closeErr := file.Close()
	if e != nil {
		return e
	}
	return closeErr
}
func captureWindowScreen(h uintptr, path string) error {
	var r screenRect
	ok, _, e := user.NewProc("GetWindowRect").Call(h, uintptr(unsafe.Pointer(&r)))
	if ok == 0 {
		return e
	}
	return captureScreenRegion(image.Rect(int(r.L), int(r.T), int(r.R), int(r.B)), path)
}
func screenImageMatches(h uintptr, source image.Image, path string) error {
	if e := captureWindowScreen(h, path); e != nil {
		return e
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	rendered, e := png.Decode(bytes.NewReader(raw))
	if e != nil {
		return e
	}
	var origin struct{ X, Y int32 }
	var r screenRect
	user.NewProc("ClientToScreen").Call(h, uintptr(unsafe.Pointer(&origin)))
	user.NewProc("GetWindowRect").Call(h, uintptr(unsafe.Pointer(&r)))
	for y := 0; y < 540; y++ {
		for x := 0; x < 960; x++ {
			a, b, c, _ := source.At(x, y).RGBA()
			d, e, f, _ := rendered.At(int(origin.X-r.L)+20+x, int(origin.Y-r.T)+70+y).RGBA()
			if a != d || b != e || c != f {
				return fmt.Errorf("screen pixel mismatch at %d,%d", x, y)
			}
		}
	}
	return nil
}
func verifyDisplayLifecycle(h uintptr, framePath, outDir, prefix string) {
	raw, e := os.ReadFile(framePath)
	fail(e)
	source, e := png.Decode(bytes.NewReader(raw))
	fail(e)
	// Keep the test window uncovered; no message that renders the image is sent.
	user.NewProc("SetWindowPos").Call(h, ^uintptr(0), 0, 0, 0, 0, 0x53)
	verify := func(stage string) {
		path := filepath.Join(outDir, prefix+"-screen-"+stage+".png")
		var err error
		for attempt := 0; attempt < 12; attempt++ {
			time.Sleep(100 * time.Millisecond)
			err = screenImageMatches(h, source, path)
			if err == nil {
				return
			}
		}
		fail(fmt.Errorf("%s %s: %w", prefix, stage, err))
	}
	verify("initial")
	// Exercise background erasure and normal message-loop recovery. The old
	// renderer may recover via a pending WM_PAINT; record that honestly.
	dc, _, _ := user.NewProc("GetDC").Call(h)
	user.NewProc("SendMessageW").Call(h, 0x14, dc, 0)
	user.NewProc("ReleaseDC").Call(h, dc)
	verify("erase")
	user.NewProc("ShowWindow").Call(h, 6)
	time.Sleep(100 * time.Millisecond)
	user.NewProc("ShowWindow").Call(h, 9)
	verify("restored")
	var r screenRect
	user.NewProc("GetWindowRect").Call(h, uintptr(unsafe.Pointer(&r)))
	user.NewProc("MoveWindow").Call(h, uintptr(r.L), uintptr(r.T), uintptr(r.R-r.L-4), uintptr(r.B-r.T-4), 1)
	user.NewProc("MoveWindow").Call(h, uintptr(r.L), uintptr(r.T), uintptr(r.R-r.L), uintptr(r.B-r.T), 1)
	verify("resized")
	for _, dpi := range []uintptr{120, 144, 192} {
		user.NewProc("SendMessageW").Call(h, 0x2E0, dpi|(dpi<<16), uintptr(unsafe.Pointer(&r)))
		verify(fmt.Sprintf("dpi-%d", dpi))
	}
}
