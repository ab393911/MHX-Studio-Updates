//go:build windows

package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"runtime"
	"testing"
)

// Exercise the actual Win32 bitmap after the decoded PNG and temporary Go
// storage are collected. This checks ownership, BGR conversion and row order.
func TestNativeImageBitmapPNGLifetimeAndCleanup(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	img := image.NewNRGBA(image.Rect(0, 0, 960, 540))
	points := map[image.Point]color.NRGBA{
		{0, 0}: {243, 11, 29, 255}, {959, 0}: {17, 221, 37, 255},
		{0, 539}: {7, 31, 249, 255}, {959, 539}: {127, 61, 199, 255},
	}
	for p, c := range points {
		img.SetNRGBA(p.X, p.Y, c)
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, img); err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(raw.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	bitmap, err := newImageBitmap(decoded)
	if err != nil {
		t.Fatal(err)
	}
	defer bitmap.close()
	img, decoded = nil, nil
	raw.Reset()
	runtime.GC()
	runtime.GC()
	dc, _, _ := user32.NewProc("GetDC").Call(0)
	if dc == 0 {
		t.Fatal("no screen DC")
	}
	defer user32.NewProc("ReleaseDC").Call(0, dc)
	mem, _, _ := gdi32.NewProc("CreateCompatibleDC").Call(dc)
	if mem == 0 {
		t.Fatal("no memory DC")
	}
	defer gdi32.NewProc("DeleteDC").Call(mem)
	target, _, _ := gdi32.NewProc("CreateCompatibleBitmap").Call(dc, 1000, 640)
	if target == 0 {
		t.Fatal("no target bitmap")
	}
	defer gdi32.NewProc("DeleteObject").Call(target)
	old, _, _ := gdi32.NewProc("SelectObject").Call(mem, target)
	defer gdi32.NewProc("SelectObject").Call(mem, old)
	if err := bitmap.draw(mem); err != nil {
		t.Fatal(err)
	}
	for p, c := range points {
		got, _, _ := gdi32.NewProc("GetPixel").Call(mem, uintptr(20+p.X), uintptr(70+p.Y))
		want := uintptr(c.R) | uintptr(c.G)<<8 | uintptr(c.B)<<16
		if got != want {
			t.Fatalf("PNG pixel %v: %#x != %#x", p, got, want)
		}
	}
	bitmap.close()
	bitmap.close()
	if err := bitmap.draw(mem); err == nil {
		t.Fatal("closed bitmap accepted")
	}
	process, _, _ := kernel32.NewProc("GetCurrentProcess").Call()
	before, _, _ := user32.NewProc("GetGuiResources").Call(process, 0)
	for i := 0; i < 20; i++ {
		b, err := newImageBitmap(image.NewNRGBA(image.Rect(0, 0, 960, 540)))
		if err != nil {
			t.Fatal(err)
		}
		b.close()
	}
	after, _, _ := user32.NewProc("GetGuiResources").Call(process, 0)
	if after > before {
		t.Fatalf("GDI bitmap leak: %d -> %d", before, after)
	}
}
