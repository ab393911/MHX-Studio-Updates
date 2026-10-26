//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/update"
)

type windowsRuntime struct{}

func (windowsRuntime) WaitParent(ctx context.Context, pid int) error {
	h, e := syscall.OpenProcess(0x00100000, false, uint32(pid))
	if e != nil {
		if e == syscall.Errno(87) {
			return nil
		}
		return e
	}
	defer syscall.CloseHandle(h)
	for {
		result, e := syscall.WaitForSingleObject(h, 100)
		if e != nil {
			return e
		}
		if result == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

type windowsChild struct{ cmd *exec.Cmd }

func (c windowsChild) Stop() error {
	e := c.cmd.Process.Kill()
	if e != nil && !errors.Is(e, os.ErrProcessDone) {
		return e
	}
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case <-done:
		return nil
	case <-time.After(10 * time.Second):
		return errors.New("new EXE did not exit after termination")
	}
}
func (windowsRuntime) Launch(path string, args ...string) (update.Child, error) {
	cmd := exec.Command(path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if e := cmd.Start(); e != nil {
		return nil, e
	}
	return windowsChild{cmd}, nil
}
