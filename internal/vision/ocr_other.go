//go:build !windows

package vision

import "context"

type NativeReader struct{}

func NewNativeReader() *NativeReader                               { return &NativeReader{} }
func (*NativeReader) Read(context.Context, []byte) (Screen, error) { return Screen{}, ErrUnavailable }
func (*NativeReader) Close() error                                 { return nil }
