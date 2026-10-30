package login

import (
	"errors"
	"path/filepath"
	"strings"
)

// StartupPlan is an explicitly saved application-open plan, not an OS boot
// registration. Indices select existing instances; Android IDs bind the installed
// accounts. Runtime ADB serials and boot IDs must be rediscovered every launch.
type StartupMember struct {
	Index     int    `json:"index"`
	AndroidID string `json:"android_id"`
	Package   string `json:"package"`
}
type StartupPlan struct {
	Enabled     bool            `json:"enabled"`
	Console     string          `json:"console"`
	Target      Spec            `json:"target"`
	Members     []StartupMember `json:"members"`
	RunTasks    bool            `json:"run_tasks,omitempty"`
	LeaderIndex int             `json:"leader_index,omitempty"`
}

func (p StartupPlan) Validate() error {
	if !p.Enabled {
		return nil
	}
	if strings.TrimSpace(p.Console) == "" || len(p.Console) > 2048 || strings.ContainsRune(p.Console, 0) || len(p.Members) != 5 || p.Target.Server == "" || len(p.Target.Server) > 128 || p.Target.Role < 1 || p.Target.Role > 5 {
		return errors.New("自动启动方案需要已确认的雷电程序、区服、角色和五台实例")
	}
	seen := map[int]bool{}
	for _, m := range p.Members {
		if m.Index < 0 || m.Index > 99999 || seen[m.Index] || strings.TrimSpace(m.AndroidID) == "" || len(m.AndroidID) > 128 || strings.ContainsRune(m.AndroidID, 0) || !validPackage(m.Package) {
			return errors.New("自动启动实例重复、设备身份缺失或游戏包名无效")
		}
		seen[m.Index] = true
	}
	if p.RunTasks && !seen[p.LeaderIndex] {
		return errors.New("队长不在保存的五台实例中")
	}
	return nil
}
func validPackage(s string) bool {
	if len(s) < 3 || len(s) > 255 || !strings.Contains(s, ".") {
		return false
	}
	for _, part := range strings.Split(s, ".") {
		if part == "" {
			return false
		}
		for i, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' || i > 0 && r >= '0' && r <= '9') {
				return false
			}
		}
	}
	return true
}
func (p StartupPlan) Indices() []int {
	out := make([]int, len(p.Members))
	for i, m := range p.Members {
		out[i] = m.Index
	}
	return out
}
func (p StartupPlan) MatchesConsole(console string) bool {
	return strings.EqualFold(filepath.Clean(p.Console), filepath.Clean(console))
}
