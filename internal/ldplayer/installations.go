package ldplayer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Installation struct{ Console, ADB, Source string }
type installDirectory struct{ path, source string }

func installations(configured string, dirs []installDirectory) []Installation {
	if configured != "" {
		dirs = append([]installDirectory{{configured, "已选择的目录"}}, dirs...)
	}
	seen := map[string]bool{}
	var out []Installation
	for _, d := range dirs {
		p := strings.Trim(strings.TrimSpace(d.path), "\"")
		if p == "" {
			continue
		}
		if strings.EqualFold(filepath.Ext(p), ".exe") {
			p = filepath.Dir(p)
		}
		p, e := filepath.Abs(p)
		if e != nil {
			continue
		}
		for _, name := range []string{"ldconsole.exe", "dnconsole.exe"} {
			console := filepath.Join(p, name)
			key := strings.ToLower(filepath.Clean(console))
			if seen[key] {
				continue
			}
			st, e := os.Stat(console)
			if e != nil || st.IsDir() {
				continue
			}
			seen[key] = true
			adb := ""
			for _, a := range []string{filepath.Join(p, "adb.exe"), filepath.Join(p, "adb", "adb.exe")} {
				if st, e := os.Stat(a); e == nil && !st.IsDir() {
					adb = a
					break
				}
			}
			out = append(out, Installation{console, adb, d.source})
		}
		if len(out) >= 64 {
			break
		}
	}
	return out
}

func FindInstallations(configured string) []Installation {
	return installations(configured, systemDirectories())
}
func Find(configured string) (string, string) {
	all := FindInstallations(configured)
	if len(all) == 0 {
		return "", ""
	}
	return all[0].Console, all[0].ADB
}

// SelectInstallation honors an existing explicit selection. Otherwise an
// installation with running instances takes priority over an idle installation.
// Separate installations are not merged: their raw indices may overlap.
func SelectInstallation(ctx context.Context, candidates []Installation, runner Runner) (Client, []Instance, string, error) {
	var first Client
	var inventory []Instance
	var source string
	var errs []error
	for _, p := range candidates {
		if ctx.Err() != nil {
			return Client{}, nil, "", ctx.Err()
		}
		c := Client{Console: p.Console, ADB: p.ADB, Runner: runner}
		all, e := c.Instances(ctx)
		if e != nil {
			errs = append(errs, fmt.Errorf("%s：%w", p.Console, e))
			continue
		}
		if first.Console == "" {
			first, inventory, source = c, all, p.Source
		}
		if p.Source == "已选择的目录" {
			return c, all, p.Source, nil
		}
		for _, d := range all {
			if d.Running() {
				return c, all, p.Source, nil
			}
		}
	}
	if first.Console != "" {
		return first, inventory, source, nil
	}
	if len(errs) == 0 {
		return Client{}, nil, "", errors.New("未找到雷电安装目录。请点“选择雷电程序”，选择 ldconsole.exe 或 dnconsole.exe；选好后会记住位置")
	}
	return Client{}, nil, "", fmt.Errorf("找到控制台但读取实例列表失败：%w", errors.Join(errs...))
}
