//go:build linux && cgo

package studio

/*
#cgo LDFLAGS: -l:libsqlite3.so.0
#include <stdlib.h>
typedef struct sqlite3 sqlite3;
typedef struct sqlite3_stmt sqlite3_stmt;
int sqlite3_open_v2(const char*,sqlite3**,int,const char*);
int sqlite3_close_v2(sqlite3*);
const char *sqlite3_errmsg(sqlite3*);
int sqlite3_prepare_v2(sqlite3*,const char*,int,sqlite3_stmt**,const char**);
int sqlite3_finalize(sqlite3_stmt*);
int sqlite3_bind_parameter_count(sqlite3_stmt*);
int sqlite3_bind_text(sqlite3_stmt*,int,const char*,int,void(*)(void*));
int sqlite3_step(sqlite3_stmt*);
int sqlite3_column_count(sqlite3_stmt*);
const unsigned char *sqlite3_column_text(sqlite3_stmt*,int);
int sqlite3_column_bytes(sqlite3_stmt*,int);
int sqlite3_changes(sqlite3*);
static int bind_copy(sqlite3_stmt *s,int i,const char *v){return sqlite3_bind_text(s,i,v,-1,(void(*)(void*))-1);}
*/
import "C"
import (
	"errors"
	"unsafe"
)

type nativeDB struct{ p *C.sqlite3 }

func openNative(path string, readonly bool) (*nativeDB, error) {
	c := C.CString(path)
	defer C.free(unsafe.Pointer(c))
	h := &nativeDB{}
	flags := C.int(2 | 4 | 0x10000)
	if readonly {
		flags = 1 | 0x10000
	}
	if C.sqlite3_open_v2(c, &h.p, flags, nil) != 0 {
		e := errors.New(C.GoString(C.sqlite3_errmsg(h.p)))
		h.close()
		return nil, e
	}
	return h, nil
}
func (h *nativeDB) close() error {
	if h.p == nil {
		return nil
	}
	if C.sqlite3_close_v2(h.p) != 0 {
		return errors.New("SQLite close failed")
	}
	h.p = nil
	return nil
}
func (h *nativeDB) query(sql string, args ...string) ([][]string, int, error) {
	c := C.CString(sql)
	defer C.free(unsafe.Pointer(c))
	var s *C.sqlite3_stmt
	if C.sqlite3_prepare_v2(h.p, c, -1, &s, nil) != 0 {
		return nil, 0, errors.New(C.GoString(C.sqlite3_errmsg(h.p)))
	}
	defer C.sqlite3_finalize(s)
	if int(C.sqlite3_bind_parameter_count(s)) != len(args) {
		return nil, 0, errors.New("SQLite argument count mismatch")
	}
	for i, v := range args {
		p := C.CString(v)
		rc := C.bind_copy(s, C.int(i+1), p)
		C.free(unsafe.Pointer(p))
		if rc != 0 {
			return nil, 0, errors.New("SQLite bind failed")
		}
	}
	var out [][]string
	for {
		rc := C.sqlite3_step(s)
		if rc == 101 {
			return out, int(C.sqlite3_changes(h.p)), nil
		}
		if rc != 100 {
			return nil, 0, errors.New(C.GoString(C.sqlite3_errmsg(h.p)))
		}
		if len(out) >= 50000 {
			return nil, 0, errors.New("SQLite result limit exceeded")
		}
		row := make([]string, int(C.sqlite3_column_count(s)))
		for i := range row {
			n := int(C.sqlite3_column_bytes(s, C.int(i)))
			if n > 8<<20 {
				return nil, 0, errors.New("SQLite value too large")
			}
			p := C.sqlite3_column_text(s, C.int(i))
			if p != nil {
				row[i] = C.GoStringN((*C.char)(unsafe.Pointer(p)), C.int(n))
			}
		}
		out = append(out, row)
	}
}
