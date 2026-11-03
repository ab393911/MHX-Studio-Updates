//go:build windows

package main

import (
	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"os"
	"path/filepath"
	"unsafe"
)

// Reuse calibration's original-size GDI renderer without calibration handlers.
// Viewing never registers anchors, saves rules or sends game input.
type diagnosticImage struct {
	parent *utilityPanel
	canvas calibration
}

func (p *utilityPanel) openOriginalImage() {
	if p.image != nil {
		user32.NewProc("SetForegroundWindow").Call(p.image.canvas.hwnd)
		return
	}
	raw, err := os.ReadFile(filepath.Join(p.bundle.Directory, "screenshot.png"))
	if err != nil {
		p.app.info(err.Error())
		return
	}
	img, err := automation.Decode(raw)
	if err != nil {
		p.app.info(err.Error())
		return
	}
	inst, _, _ := moduleHandle.Call(0)
	h, _, _ := createWindow.Call(0, uintptr(unsafe.Pointer(utf("MHXStudioNextWindow"))), uintptr(unsafe.Pointer(utf("诊断原图 · 960×540"))), 0x00CA0000|wsVisible, 100, 50, 1015, 750, p.hwnd, 0, inst, 0)
	if h == 0 {
		p.app.info("原图窗口创建失败")
		return
	}
	p.image = &diagnosticImage{parent: p, canvas: calibration{hwnd: h, img: img, pixels: originalImagePixels(img)}}
	child(h, "STATIC", "原始960×540截图，仅查看；不缩放、不修改标定、不点击游戏。", 20, 12, 950, 40, 0, 0)
	child(h, "BUTTON", "关闭原图", 20, 640, 150, 34, 0, 1310)
	user32.NewProc("EnableWindow").Call(p.hwnd, 0)
	fitPopup(h, 1015, 750)
	user32.NewProc("InvalidateRect").Call(h, 0, 1)
}
func (d *diagnosticImage) proc(h uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case 0xf:
		d.canvas.paint()
		return 0
	case wmCommand:
		if int(w&0xffff) == 1310 && w>>16 == 0 {
			destroyWindow.Call(h)
		}
		return 0
	case wmClose:
		destroyWindow.Call(h)
		return 0
	case wmDestroy:
		d.parent.image = nil
		user32.NewProc("EnableWindow").Call(d.parent.hwnd, 1)
		return 0
	}
	v, _, _ := defWindow.Call(h, uintptr(m), w, l)
	return v
}
