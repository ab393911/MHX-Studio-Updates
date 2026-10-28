//go:build windows

package studio

import (
	"errors"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

var sqliteDLL = systemSQLite()

func systemSQLite() *syscall.LazyDLL {
	var dir [32768]uint16
	n, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemDirectoryW").Call(uintptr(unsafe.Pointer(&dir[0])), uintptr(len(dir)))
	if n == 0 || n >= uintptr(len(dir)) {
		return syscall.NewLazyDLL("MHX-System-SQLite-Unavailable.dll")
	}
	return syscall.NewLazyDLL(filepath.Join(syscall.UTF16ToString(dir[:]), "winsqlite3.dll"))
}

func sp(n string) *syscall.LazyProc { return sqliteDLL.NewProc(n) }

type nativeDB struct{ p uintptr }

func cString(v string) (*byte, error) { return syscall.BytePtrFromString(v) }
func nativeString(p uintptr, n int) string {
	if p == 0 || n == 0 {
		return ""
	}
	b := make([]byte, n)
	var read uintptr
	k := syscall.NewLazyDLL("kernel32.dll")
	process, _, _ := k.NewProc("GetCurrentProcess").Call()
	ok, _, _ := k.NewProc("ReadProcessMemory").Call(process, p, uintptr(unsafe.Pointer(&b[0])), uintptr(n), uintptr(unsafe.Pointer(&read)))
	if ok == 0 || read != uintptr(n) {
		return ""
	}
	return string(b)
}
func (h *nativeDB) err() error {
	p, _, _ := sp("sqlite3_errmsg").Call(h.p)
	if p == 0 {
		return errors.New("SQLite error")
	}
	n, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("lstrlenA").Call(p)
	if n > 4096 {
		n = 4096
	}
	return errors.New(nativeString(p, int(n)))
}
func openNative(path string, readonly bool) (*nativeDB, error) {
	if e := sqliteDLL.Load(); e != nil {
		return nil, e
	}
	p, e := cString(path)
	if e != nil {
		return nil, e
	}
	h := &nativeDB{}
	flags := uintptr(2 | 4 | 0x10000)
	if readonly {
		flags = 1 | 0x10000
	}
	rc, _, _ := sp("sqlite3_open_v2").Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&h.p)), flags, 0)
	runtime.KeepAlive(p)
	if rc != 0 {
		e = h.err()
		h.close()
		return nil, e
	}
	return h, nil
}
func (h *nativeDB) close() error {
	if h.p == 0 {
		return nil
	}
	rc, _, _ := sp("sqlite3_close_v2").Call(h.p)
	if rc != 0 {
		return h.err()
	}
	h.p = 0
	return nil
}
func (h *nativeDB) query(sql string, args ...string) ([][]string, int, error) {
	p, e := cString(sql)
	if e != nil {
		return nil, 0, e
	}
	var stmt uintptr
	rc, _, _ := sp("sqlite3_prepare_v2").Call(h.p, uintptr(unsafe.Pointer(p)), ^uintptr(0), uintptr(unsafe.Pointer(&stmt)), 0)
	runtime.KeepAlive(p)
	if rc != 0 {
		return nil, 0, h.err()
	}
	defer sp("sqlite3_finalize").Call(stmt)
	count, _, _ := sp("sqlite3_bind_parameter_count").Call(stmt)
	if int(count) != len(args) {
		return nil, 0, errors.New("SQLite argument count mismatch")
	}
	for i, v := range args {
		p, e := cString(v)
		if e != nil {
			return nil, 0, e
		}
		rc, _, _ := sp("sqlite3_bind_text").Call(stmt, uintptr(i+1), uintptr(unsafe.Pointer(p)), ^uintptr(0), ^uintptr(0))
		runtime.KeepAlive(p)
		if rc != 0 {
			return nil, 0, h.err()
		}
	}
	var out [][]string
	for {
		rc, _, _ := sp("sqlite3_step").Call(stmt)
		if rc == 101 {
			n, _, _ := sp("sqlite3_changes").Call(h.p)
			return out, int(n), nil
		}
		if rc != 100 {
			return nil, 0, h.err()
		}
		if len(out) >= 50000 {
			return nil, 0, errors.New("SQLite result limit exceeded")
		}
		cols, _, _ := sp("sqlite3_column_count").Call(stmt)
		row := make([]string, int(cols))
		for i := range row {
			n, _, _ := sp("sqlite3_column_bytes").Call(stmt, uintptr(i))
			if n > 8<<20 {
				return nil, 0, errors.New("SQLite value too large")
			}
			p, _, _ := sp("sqlite3_column_text").Call(stmt, uintptr(i))
			row[i] = nativeString(p, int(n))
		}
		out = append(out, row)
	}
}
