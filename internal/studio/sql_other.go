//go:build !windows && (!linux || !cgo)

package studio

import "errors"

type nativeDB struct{}

func openNative(string, bool) (*nativeDB, error) {
	return nil, errors.New("SQLite tests require Linux cgo; shipped Windows EXE uses system WinSQLite without cgo")
}
func (*nativeDB) query(string, ...string) ([][]string, int, error) {
	return nil, 0, errors.New("SQLite unavailable")
}
func (*nativeDB) close() error { return nil }
