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

type windowsChild struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func (c *windowsChild) Exited() <-chan struct{} { return c.done }

func (c *windowsChild) Stop() error {
	select {
	case <-c.done:
		return nil
	default:
	}
	e := c.cmd.Process.Kill()
	if e != nil && !errors.Is(e, os.ErrProcessDone) {
		select {
		case <-c.done:
			return nil
		case <-time.After(300 * time.Millisecond):
			return e
		}
	}
	select {
	case <-c.done:
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
	c := &windowsChild{cmd: cmd, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(c.done) }()
	return c, nil
}
