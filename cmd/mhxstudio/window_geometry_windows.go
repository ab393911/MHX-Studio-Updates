//go:build windows

package main

import "unsafe"

type windowRect struct{ L, T, R, B int32 }

func workArea() windowRect {
	r := windowRect{R: 1024, B: 768}
	if ok, _, _ := user32.NewProc("SystemParametersInfoW").Call(0x30, 0, uintptr(unsafe.Pointer(&r)), 0); ok == 0 || r.R-r.L < 300 || r.B-r.T < 300 {
		w, _, _ := user32.NewProc("GetSystemMetrics").Call(0)
		h, _, _ := user32.NewProc("GetSystemMetrics").Call(1)
		r = windowRect{R: int32(w), B: int32(h)}
	}
	return r
}
func fittedBounds(width, height int) (int, int, int, int) {
	r := workArea()
	w, h := int(r.R-r.L), int(r.B-r.T)
	if width > w {
		width = w
	}
	if height > h {
		height = h
	}
	return int(r.L) + (w-width)/2, int(r.T) + (h-height)/2, width, height
}
func fitPopup(h uintptr, width, height int) {
	x, y, w, height := fittedBounds(width, height)
	user32.NewProc("MoveWindow").Call(h, uintptr(x), uintptr(y), uintptr(w), uintptr(height), 1)
}
func (a *app) fitClientControls() {
	if a.controls[900] == 0 {
		return
	}
	var r windowRect
	if ok, _, _ := user32.NewProc("GetClientRect").Call(a.hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return
	}
	w, h := int(r.R)-40, int(r.B)-515
	if w < 300 {
		w = 300
	}
	if h < 60 {
		h = 60
	}
	user32.NewProc("MoveWindow").Call(a.controls[900], 20, 495, uintptr(w), uintptr(h), 1)
	for _, v := range []struct{ id, y, height int }{{208, 373, 25}, {209, 282, 20}, {211, 448, 40}} {
		user32.NewProc("MoveWindow").Call(a.controls[v.id], 20, uintptr(v.y), uintptr(w), uintptr(v.height), 1)
	}
}
func (a *app) fitOpenWindows() {
	if a.cal != nil && a.cal.hwnd != 0 && a.placedCalibration != a.cal.hwnd {
		a.placedCalibration = a.cal.hwnd
		fitPopup(a.cal.hwnd, 1015, 750)
	}
	if a.gameSelector != nil && a.gameSelector.hwnd != 0 && a.placedSelector != a.gameSelector.hwnd {
		a.placedSelector = a.gameSelector.hwnd
		fitPopup(a.gameSelector.hwnd, 950, 330)
	}
}
