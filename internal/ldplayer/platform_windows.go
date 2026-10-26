//go:build windows

package ldplayer

import (
	"os/exec"
	"syscall"
	"unicode/utf8"
	"unsafe"
)

func hide(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
func decodeConsole(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	if len(b) == 0 {
		return ""
	}
	p := syscall.NewLazyDLL("kernel32.dll").NewProc("MultiByteToWideChar")
	n, _, _ := p.Call(0, 0, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0, 0)
	if n == 0 {
		return string(b)
	}
	w := make([]uint16, n)
	p.Call(0, 0, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(unsafe.Pointer(&w[0])), n)
	return syscall.UTF16ToString(w)
}
