//go:build !windows

package ldplayer

import "os/exec"

func hide(cmd *exec.Cmd)                    {}
func decodeConsole(b []byte) string         { return string(b) }
func systemDirectories() []installDirectory { return nil }
