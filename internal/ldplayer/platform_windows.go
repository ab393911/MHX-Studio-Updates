//go:build windows

package ldplayer

import (
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"
	"unsafe"
)

var kernel = syscall.NewLazyDLL("kernel32.dll")

func systemDirectories() []installDirectory {
	var out []installDirectory
	// Process paths discover portable/custom installations on any drive, without
	// guessing a window title, port formula, or a fixed installation directory.
	snapshot, e := syscall.CreateToolhelp32Snapshot(2, 0)
	if e == nil {
		var p syscall.ProcessEntry32
		p.Size = uint32(unsafe.Sizeof(p))
		for e = syscall.Process32First(snapshot, &p); e == nil; e = syscall.Process32Next(snapshot, &p) {
			name := strings.ToLower(syscall.UTF16ToString(p.ExeFile[:]))
			switch name {
			case "dnplayer.exe", "ldplayer.exe", "ldconsole.exe", "dnconsole.exe", "dnmultiplayer.exe", "ldmultiplayer.exe", "ldvboxheadless.exe", "adb.exe":
				h, e := syscall.OpenProcess(0x1000, false, p.ProcessID)
				if e != nil {
					continue
				}
				b, n := make([]uint16, 32768), uint32(32768)
				ok, _, _ := kernel.NewProc("QueryFullProcessImageNameW").Call(uintptr(h), 0, uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&n)))
				syscall.CloseHandle(h)
				if ok != 0 {
					dir := filepath.Dir(syscall.UTF16ToString(b[:n]))
					out = append(out, installDirectory{dir, "正在运行的雷电进程"}, installDirectory{filepath.Dir(dir), "正在运行的雷电进程"})
				}
			}
		}
		syscall.CloseHandle(snapshot)
	}
	for _, root := range []syscall.Handle{syscall.HKEY_CURRENT_USER, syscall.HKEY_LOCAL_MACHINE} {
		for _, view := range []uint32{syscall.KEY_WOW64_64KEY, syscall.KEY_WOW64_32KEY} {
			for _, name := range []string{"dnplayer.exe", "ldplayer.exe", "ldconsole.exe", "dnconsole.exe"} {
				key, e := registryOpen(root, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\`+name, view)
				if e == nil {
					out = append(out, installDirectory{programPath(registryString(key, "")), "Windows 安装记录"}, installDirectory{registryString(key, "Path"), "Windows 安装记录"})
					syscall.RegCloseKey(key)
				}
			}
			uninstall, e := registryOpen(root, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`, view)
			if e != nil {
				continue
			}
			for i := uint32(0); i < 8192; i++ {
				b, n := make([]uint16, 512), uint32(512)
				if e = syscall.RegEnumKeyEx(uninstall, i, &b[0], &n, nil, nil, nil, nil); e != nil {
					break
				}
				key, e := registryOpen(uninstall, syscall.UTF16ToString(b[:n]), view)
				if e != nil {
					continue
				}
				name := strings.ToLower(registryString(key, "DisplayName"))
				if strings.Contains(name, "雷电") || strings.Contains(name, "ldplayer") || strings.Contains(name, "leidian") {
					for _, value := range []string{"InstallLocation", "DisplayIcon", "UninstallString"} {
						out = append(out, installDirectory{programPath(registryString(key, value)), "Windows 安装记录"})
					}
				}
				syscall.RegCloseKey(key)
			}
			syscall.RegCloseKey(uninstall)
		}
	}
	mask, _, _ := kernel.NewProc("GetLogicalDrives").Call()
	for drive := 0; drive < 26; drive++ {
		if mask&(1<<drive) == 0 {
			continue
		}
		base := string(rune('A'+drive)) + `:\`
		root, _ := syscall.UTF16PtrFromString(base)
		kind, _, _ := kernel.NewProc("GetDriveTypeW").Call(uintptr(unsafe.Pointer(root)))
		if kind != 2 && kind != 3 {
			continue
		}
		for _, pattern := range []string{`LDPlayer*`, `leidian\LDPlayer*`, `leidian\*`, `Changzhi\*`, `雷电模拟器*`, `Program Files\LDPlayer*`, `Program Files\leidian\*`, `Program Files\dnplayer*`, `Program Files (x86)\LDPlayer*`, `Program Files (x86)\leidian\*`, `Program Files (x86)\dnplayer*`} {
			hits, _ := filepath.Glob(filepath.Join(base, pattern))
			for _, p := range hits {
				out = append(out, installDirectory{p, "常用安装目录"})
			}
		}
	}
	return out
}

func registryOpen(root syscall.Handle, name string, view uint32) (syscall.Handle, error) {
	p, e := syscall.UTF16PtrFromString(name)
	if e != nil {
		return 0, e
	}
	var key syscall.Handle
	e = syscall.RegOpenKeyEx(root, p, 0, syscall.KEY_READ|view, &key)
	return key, e
}
func registryString(key syscall.Handle, name string) string {
	p, e := syscall.UTF16PtrFromString(name)
	if e != nil {
		return ""
	}
	var kind, n uint32
	if syscall.RegQueryValueEx(key, p, nil, &kind, nil, &n) != nil || n < 2 || n > 65536 || (kind != syscall.REG_SZ && kind != syscall.REG_EXPAND_SZ) {
		return ""
	}
	b := make([]uint16, (n+1)/2)
	if syscall.RegQueryValueEx(key, p, nil, &kind, (*byte)(unsafe.Pointer(&b[0])), &n) != nil {
		return ""
	}
	s := syscall.UTF16ToString(b)
	if kind == syscall.REG_EXPAND_SZ {
		q, e := syscall.UTF16PtrFromString(s)
		if e == nil {
			dst := make([]uint16, 32768)
			r, _, _ := kernel.NewProc("ExpandEnvironmentStringsW").Call(uintptr(unsafe.Pointer(q)), uintptr(unsafe.Pointer(&dst[0])), uintptr(len(dst)))
			if r > 0 && r <= uintptr(len(dst)) {
				s = syscall.UTF16ToString(dst)
			}
		}
	}
	return s
}
func programPath(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "\"") {
		if i := strings.Index(s[1:], "\""); i >= 0 {
			return s[1 : 1+i]
		}
	}
	if i := strings.Index(strings.ToLower(s), ".exe"); i >= 0 {
		return s[:i+4]
	}
	return s
}

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
