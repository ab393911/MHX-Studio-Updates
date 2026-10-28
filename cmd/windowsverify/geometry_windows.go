//go:build windows

package main

import "unsafe"

type screenRect struct{ L, T, R, B int32 }

func onScreen(h uintptr) bool {
	var r, work screenRect
	if ok, _, _ := user.NewProc("GetWindowRect").Call(h, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return false
	}
	if ok, _, _ := user.NewProc("SystemParametersInfoW").Call(0x30, 0, uintptr(unsafe.Pointer(&work)), 0); ok == 0 {
		return false
	}
	return r.R > r.L && r.B > r.T && r.L >= work.L && r.T >= work.T && r.R <= work.R && r.B <= work.B
}
