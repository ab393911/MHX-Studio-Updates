//go:build windows

package main

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"unsafe"
)

// The driver reads owned native DIB storage, not a temporary Go slice. Both
// image windows retain this bitmap until WM_DESTROY on the UI thread.
type imageBitmap struct{ handle, bits uintptr }

func newImageBitmap(img image.Image) (*imageBitmap, error) {
	if img == nil || img.Bounds() != image.Rect(0, 0, 960, 540) {
		return nil, errors.New("图片必须为原始960×540")
	}
	pixels := originalImagePixels(img)
	bi := bitmapInfo{Size: 40, Width: 960, Height: -540, Planes: 1, BitCount: 32, SizeImage: uint32(len(pixels))}
	var bits uintptr
	bitmap, _, err := gdi32.NewProc("CreateDIBSection").Call(0, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 || bits == 0 {
		if bitmap != 0 {
			gdi32.NewProc("DeleteObject").Call(bitmap)
		}
		return nil, fmt.Errorf("CreateDIBSection失败：%v", err)
	}
	kernel32.NewProc("RtlMoveMemory").Call(bits, uintptr(unsafe.Pointer(&pixels[0])), uintptr(len(pixels)))
	runtime.KeepAlive(pixels)
	return &imageBitmap{bitmap, bits}, nil
}
func (b *imageBitmap) close() {
	if b != nil && b.handle != 0 {
		gdi32.NewProc("DeleteObject").Call(b.handle)
		b.handle = 0
		b.bits = 0
	}
}
func (b *imageBitmap) draw(dc uintptr) error {
	if b == nil || b.handle == 0 || dc == 0 {
		return errors.New("图片位图或绘图DC尚未就绪")
	}
	mem, _, _ := gdi32.NewProc("CreateCompatibleDC").Call(dc)
	if mem != 0 {
		old, _, _ := gdi32.NewProc("SelectObject").Call(mem, b.handle)
		if old != 0 && old != ^uintptr(0) {
			ok, _, _ := gdi32.NewProc("BitBlt").Call(dc, 20, 70, 960, 540, mem, 0, 0, 0x00CC0020)
			gdi32.NewProc("SelectObject").Call(mem, old)
			gdi32.NewProc("DeleteDC").Call(mem)
			if ok != 0 {
				gdi32.NewProc("GdiFlush").Call()
				return nil
			}
		} else {
			gdi32.NewProc("DeleteDC").Call(mem)
		}
	}
	// Remote/heterogeneous DCs can reject BitBlt. Keep the same owned pixels for
	// a checked, original-size fallback; never silently leave an empty canvas.
	bi := bitmapInfo{Size: 40, Width: 960, Height: -540, Planes: 1, BitCount: 32, SizeImage: 960 * 540 * 4}
	lines, _, err := gdi32.NewProc("StretchDIBits").Call(dc, 20, 70, 960, 540, 0, 0, 960, 540, b.bits, uintptr(unsafe.Pointer(&bi)), 0, 0x00CC0020)
	if int32(lines) <= 0 {
		return fmt.Errorf("BitBlt及StretchDIBits绘图失败（lines=%d）：%v", int32(lines), err)
	}
	gdi32.NewProc("GdiFlush").Call()
	return nil
}

func (c *calibration) drawImage(dc uintptr) {
	var bounds windowRect
	user32.NewProc("GetClientRect").Call(c.hwnd, uintptr(unsafe.Pointer(&bounds)))
	user32.NewProc("FillRect").Call(dc, uintptr(unsafe.Pointer(&bounds)), 6)
	err := c.bitmap.draw(dc)
	c.rendered = err == nil
	if err != nil {
		if c.paintError != err.Error() && c.app != nil {
			c.app.log("图片显示失败（标定保持不变）：" + err.Error())
		}
		c.paintError = err.Error()
		setText(c.label, "图片显示失败，禁止保存；原标定未改动。"+err.Error())
		if save, _, _ := user32.NewProc("GetDlgItem").Call(c.hwnd, 302); save != 0 {
			user32.NewProc("EnableWindow").Call(save, 0)
		}
		return
	}
	if c.paintError != "" {
		c.paintError = ""
		if c.featureCount >= 2 {
			c.hint()
		} else {
			setText(c.label, "原始960×540截图，仅查看；不修改标定。")
		}
		if save, _, _ := user32.NewProc("GetDlgItem").Call(c.hwnd, 302); save != 0 {
			user32.NewProc("EnableWindow").Call(save, 1)
		}
	}
	pen, _, _ := gdi32.NewProc("CreatePen").Call(0, 2, 0x0000ff)
	oldPen, _, _ := gdi32.NewProc("SelectObject").Call(dc, pen)
	brush, _, _ := gdi32.NewProc("GetStockObject").Call(5)
	oldBrush, _, _ := gdi32.NewProc("SelectObject").Call(dc, brush)
	for _, r := range c.rects {
		gdi32.NewProc("Rectangle").Call(dc, uintptr(r.X+20), uintptr(r.Y+70), uintptr(r.X+r.W+20), uintptr(r.Y+r.H+70))
	}
	gdi32.NewProc("SelectObject").Call(dc, oldBrush)
	gdi32.NewProc("SelectObject").Call(dc, oldPen)
	gdi32.NewProc("DeleteObject").Call(pen)
}
func (c *calibration) paint() {
	var ps paintStruct
	dc, _, _ := user32.NewProc("BeginPaint").Call(c.hwnd, uintptr(unsafe.Pointer(&ps)))
	defer user32.NewProc("EndPaint").Call(c.hwnd, uintptr(unsafe.Pointer(&ps)))
	c.drawImage(dc)
}

// Use the DC supplied by erase/print messages; BeginPaint is only for WM_PAINT.
func (c *calibration) imageMessage(m uint32, w uintptr) (uintptr, bool) {
	switch m {
	case 0xF:
		c.paint()
		return 0, true
	case 0x14:
		if w != 0 {
			c.drawImage(w)
		}
		return 1, true
	case 0x318:
		if w != 0 {
			c.drawImage(w)
		}
		return 0, true
	case 5, 0x2E0:
		user32.NewProc("InvalidateRect").Call(c.hwnd, 0, 0)
	}
	return 0, false
}
func showImageWindow(h uintptr) {
	showWindow.Call(h, 1)
	user32.NewProc("InvalidateRect").Call(h, 0, 0)
	user32.NewProc("UpdateWindow").Call(h)
}
