//go:build windows

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/update"
)

func liveUpdate(old, oldVersion, manifest, expected, out string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	m, newer, e := update.Check(ctx, update.Client(), manifest, oldVersion)
	fail(e)
	if !newer || m.Version != expected {
		fail(fmt.Errorf("unexpected live channel version %s", m.Version))
	}
	root, e := os.MkdirTemp("", "MHX live update 中文 ")
	fail(e)
	defer os.RemoveAll(root)
	data := filepath.Join(root, "AppData")
	fail(os.Setenv("LOCALAPPDATA", data))
	cfgPath := filepath.Join(data, "MHXStudioNext", "config.json")
	fail(filesafe.JSON(cfgPath, map[string]any{"ldplayer_directory": "", "manifest_url": manifest, "retries": 7, "min_delay_ms": 250, "max_delay_ms": 650, "jobs": map[string]any{}, "single_validations": map[string]any{}, "future_unknown_field": map[string]any{"keep": true}}))
	before, e := os.ReadFile(cfgPath)
	fail(e)
	target := filepath.Join(root, "Program", "MHXStudio.exe")
	copyFile(old, target)
	oldHash, e := update.Hash(target)
	fail(e)
	parent := start(target)
	oldTitle := "知夏 · MHX Studio " + oldVersion
	fail(waitWindow(oldTitle))
	time.Sleep(500 * time.Millisecond)
	h := findWindow(oldTitle)
	if e = captureWindow(h, filepath.Join(filepath.Dir(out), "gui-preview.png")); e != nil {
		fmt.Println("GUI capture unavailable:", e)
	}
	button, _, _ := user.NewProc("GetDlgItem").Call(h, 110)
	if button == 0 {
		fail(errors.New("update button not found"))
	}
	user.NewProc("SendMessageW").Call(button, 0xF5, 0, 0)
	// Accept only the update confirmation owned by this test's original process.
	confirmed := false
	end := time.Now().Add(70 * time.Second)
	for time.Now().Before(end) {
		dlg := findWindow("MHX Studio")
		if dlg != 0 {
			var pid uint32
			user.NewProc("GetWindowThreadProcessId").Call(dlg, uintptr(unsafe.Pointer(&pid)))
			if int(pid) == parent.Process.Pid {
				yes, _, _ := user.NewProc("GetDlgItem").Call(dlg, 6)
				if yes != 0 {
					user.NewProc("SendMessageW").Call(yes, 0xF5, 0, 0)
					confirmed = true
					break
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	if !confirmed {
		fail(errors.New("real update confirmation was not displayed"))
	}
	fail(wait(parent, 100*time.Second))
	newTitle := "知夏 · MHX Studio " + expected
	fail(waitWindow(newTitle))
	var result update.Result
	committed := false
	end = time.Now().Add(40 * time.Second)
	for time.Now().Before(end) {
		plans, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".mhx-updates", "tx-*", "result.json"))
		for _, p := range plans {
			if filesafe.ReadJSON(p, &result) == nil && result.Status == "committed" {
				committed = true
			}
		}
		if committed {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if !committed {
		fail(fmt.Errorf("GUI update did not commit: %+v", result))
	}
	fail(update.Verify(target, m.SHA256))
	fail(update.Verify(result.Backup, oldHash))
	after, e := os.ReadFile(cfgPath)
	fail(e)
	if !bytes.Equal(before, after) {
		fail(errors.New("GUI update changed config bytes"))
	}
	closeTitle(newTitle)
	fail(filesafe.JSON(out, map[string]any{"native_windows": true, "previous_published_version": oldVersion, "real_update_button_clicked": true, "confirmation_clicked": true, "anonymous_manifest_download": true, "sha256_verified": true, "old_exe_backup_retained": true, "new_gui_restarted": true, "configuration_byte_identical": true, "version": expected, "source_commit": m.SourceCommit, "windows_game_tested": false, "passed": true, "completed_at": time.Now().UTC().Format(time.RFC3339)}))
	fmt.Println("Live public-channel GUI update passed:", expected)
}

func captureWindow(h uintptr, path string) error {
	var r struct{ L, T, R, B int32 }
	if ok, _, _ := user.NewProc("GetWindowRect").Call(h, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return errors.New("GetWindowRect failed")
	}
	w, height := int(r.R-r.L), int(r.B-r.T)
	if w < 10 || height < 10 || w > 3000 || height > 3000 {
		return errors.New("unexpected window dimensions")
	}
	gdi := syscall.NewLazyDLL("gdi32.dll")
	dc, _, _ := user.NewProc("GetDC").Call(h)
	if dc == 0 {
		return errors.New("GetDC failed")
	}
	defer user.NewProc("ReleaseDC").Call(h, dc)
	mem, _, _ := gdi.NewProc("CreateCompatibleDC").Call(dc)
	defer gdi.NewProc("DeleteDC").Call(mem)
	bitmap, _, _ := gdi.NewProc("CreateCompatibleBitmap").Call(dc, uintptr(w), uintptr(height))
	defer gdi.NewProc("DeleteObject").Call(bitmap)
	old, _, _ := gdi.NewProc("SelectObject").Call(mem, bitmap)
	ok, _, _ := user.NewProc("PrintWindow").Call(h, mem, 2)
	gdi.NewProc("SelectObject").Call(mem, old)
	if ok == 0 {
		return errors.New("PrintWindow failed")
	}
	header := struct {
		Size                   uint32
		Width, Height          int32
		Planes, Bits           uint16
		Compression, SizeImage uint32
		X, Y                   int32
		Used, Important        uint32
	}{Size: 40, Width: int32(w), Height: -int32(height), Planes: 1, Bits: 32}
	raw := make([]byte, w*height*4)
	n, _, _ := gdi.NewProc("GetDIBits").Call(dc, bitmap, 0, uintptr(height), uintptr(unsafe.Pointer(&raw[0])), uintptr(unsafe.Pointer(&header)), 0)
	if n == 0 {
		return errors.New("GetDIBits failed")
	}
	img := image.NewRGBA(image.Rect(0, 0, w, height))
	for y := 0; y < height; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			img.SetRGBA(x, y, color.RGBA{raw[i+2], raw[i+1], raw[i], 255})
		}
	}
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	e = png.Encode(f, img)
	return errors.Join(e, f.Close())
}
