//go:build windows

package vision

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
)

//go:embed windowsocr.ps1
var script string

// Windows' built-in OCR is reached through a hidden, fixed WinRT bridge. No
// user commands, installs, saved normal screenshots, network service or shell
// interpolation are involved. One worker serializes five devices' OCR requests.
type NativeReader struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	in          io.WriteCloser
	scan        *bufio.Scanner
	unavailable error
	stderr      *stageBuffer
}
type stageBuffer struct{ b []byte }

func (b *stageBuffer) Write(p []byte) (int, error) {
	n := len(p)
	b.b = append(b.b, p...)
	if len(b.b) > 8192 {
		b.b = b.b[len(b.b)-8192:]
	}
	return n, nil
}
func (r *NativeReader) diagnostic(e error) error {
	if r.stderr != nil && len(r.stderr.b) > 0 {
		return fmt.Errorf("%w; Windows OCR: %s", e, string(r.stderr.b))
	}
	return e
}

func NewNativeReader() *NativeReader { return &NativeReader{} }
func encodedScript() string {
	var b bytes.Buffer
	for _, v := range utf16.Encode([]rune(script)) {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	return base64.StdEncoding.EncodeToString(b.Bytes())
}
func (r *NativeReader) stop() {
	if r.cmd != nil {
		_ = r.in.Close()
		_ = r.cmd.Process.Kill()
		_ = r.cmd.Wait()
		r.cmd = nil
		r.in = nil
		r.scan = nil
	}
}
func (r *NativeReader) Close() error { r.mu.Lock(); defer r.mu.Unlock(); r.stop(); return nil }
func (r *NativeReader) Read(ctx context.Context, png []byte) (Screen, error) {
	if len(png) > 8<<20 {
		return Screen{}, errors.New("OCR截图超出8MiB")
	}
	if _, e := automation.Decode(png); e != nil {
		return Screen{}, e
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.unavailable != nil {
		return Screen{}, r.unavailable
	}
	if e := ctx.Err(); e != nil {
		return Screen{}, e
	}
	starting := r.cmd == nil
	if starting {
		root := os.Getenv("SystemRoot")
		if root == "" {
			return Screen{}, ErrUnavailable
		}
		cmd := exec.Command(filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoLogo", "-NoProfile", "-NonInteractive", "-MTA", "-EncodedCommand", encodedScript())
		r.stderr = &stageBuffer{}
		cmd.Stderr = r.stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		in, e := cmd.StdinPipe()
		if e != nil {
			return Screen{}, e
		}
		out, e := cmd.StdoutPipe()
		if e != nil {
			_ = in.Close()
			return Screen{}, e
		}
		if e = cmd.Start(); e != nil {
			_ = in.Close()
			_ = out.Close()
			return Screen{}, fmt.Errorf("%w: %v", ErrUnavailable, e)
		}
		r.cmd, r.in = cmd, in
		r.scan = bufio.NewScanner(out)
		r.scan.Buffer(make([]byte, 4096), 1<<20)
	}
	limit := 15 * time.Second
	if starting {
		limit = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	type reply struct {
		Ready       bool   `json:"ready"`
		Screen      Screen `json:"screen"`
		Error       string `json:"error"`
		Unavailable bool   `json:"unavailable"`
	}
	type response struct {
		r reply
		e error
	}
	done := make(chan response, 1)
	in, scan := r.in, r.scan
	// Both pipe write and read are covered by cancellation; neither may freeze
	// the GUI if the WinRT bridge stops consuming stdin or replying on stdout.
	go func() {
		if starting {
			var initial reply
			if !scan.Scan() {
				e := scan.Err()
				if e == nil {
					e = io.ErrUnexpectedEOF
				}
				done <- response{initial, e}
				return
			}
			if e := json.Unmarshal(scan.Bytes(), &initial); e != nil {
				done <- response{initial, e}
				return
			}
			if initial.Error != "" {
				done <- response{initial, nil}
				return
			}
			if !initial.Ready {
				done <- response{initial, errors.New("OCR桥接未确认就绪")}
				return
			}
		}
		e := json.NewEncoder(in).Encode(map[string]string{"png": base64.StdEncoding.EncodeToString(png)})
		var v reply
		if e == nil {
			if !scan.Scan() {
				e = scan.Err()
				if e == nil {
					e = io.ErrUnexpectedEOF
				}
			} else {
				e = json.Unmarshal(scan.Bytes(), &v)
			}
		}
		done <- response{v, e}
	}()
	select {
	case <-ctx.Done():
		r.stop()
		<-done
		return Screen{}, r.diagnostic(ctx.Err())
	case v := <-done:
		if v.e != nil {
			r.stop()
			return Screen{}, r.diagnostic(v.e)
		}
		if v.r.Error != "" {
			r.stop()
			e := errors.New(v.r.Error)
			if v.r.Unavailable {
				r.unavailable = fmt.Errorf("%w: %v", ErrUnavailable, e)
				return Screen{}, r.unavailable
			}
			return Screen{}, e
		}
		if e := v.r.Screen.Validate(); e != nil {
			r.stop()
			return Screen{}, e
		}
		return v.r.Screen, nil
	}
}
