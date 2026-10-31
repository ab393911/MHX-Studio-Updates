package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/update"
)

func fail(e error) {
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func main() {
	exe := flag.String("exe", "dist/MHXStudio.exe", "")
	ver := flag.String("version", "", "")
	commit := flag.String("commit", "", "")
	out := flag.String("out", "dist", "")
	verify := flag.String("verify", "", "")
	report := flag.String("report", "", "")
	flag.Parse()
	if *verify != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		m, _, e := update.Check(ctx, update.Client(), *verify, "0.0.0")
		fail(e)
		if *ver != "" && m.Version != *ver {
			fail(fmt.Errorf("unexpected version %s", m.Version))
		}
		if *commit != "" && m.SourceCommit != *commit {
			fail(fmt.Errorf("unexpected source commit %s", m.SourceCommit))
		}
		tmp, e := os.MkdirTemp("", "mhx-release-verify-")
		fail(e)
		defer os.RemoveAll(tmp)
		_, e = update.Download(ctx, update.Client(), m, tmp)
		fail(e)
		result := map[string]any{"anonymous_https_download": true, "sha256_verified": true, "windows_x64_pe_verified": true, "version": m.Version, "sha256": m.SHA256, "size": m.Size, "source_commit": m.SourceCommit, "verified_at": time.Now().UTC().Format(time.RFC3339), "windows_game_tested": false}
		if *report != "" {
			fail(filesafe.JSON(*report, result))
		}
		fail(json.NewEncoder(os.Stdout).Encode(result))
		return
	}
	if *ver == "" || len(*commit) != 40 {
		fail(fmt.Errorf("version and full commit SHA required"))
	}
	fail(update.ValidateEXE(*exe))
	hash, e := update.Hash(*exe)
	fail(e)
	st, e := os.Stat(*exe)
	fail(e)
	m := update.Manifest{Schema: 1, Version: *ver, Platform: "windows-amd64", URL: "https://github.com/ab393911/MHX-Studio-Updates/releases/download/v" + *ver + "/MHXStudio.exe", SHA256: hash, Size: st.Size(), Notes: "持续开发测试版：取消昵称核对，按实例、区服、角色槽位和职业核验；接通固定队组队与八星打符/幻形测试。移除订单、发货和消息界面及相关切角色限制，中控台分别显示四区资产汇总，逐角色按整万银锭及整百功绩取较小值，重复扫描覆盖。余额自动读取与后续任务链仍未接通，不代表实时余额或真实游戏验收通过。以preview.21直升及故障恢复为当前基线，保留preview.19直升验证。", SourceCommit: *commit, WindowsTested: false}
	fail(m.Validate())
	fail(os.MkdirAll(*out, 0700))
	fail(filesafe.JSON(filepath.Join(*out, "update.json"), m))
	fail(filesafe.Write(filepath.Join(*out, "SHA256SUMS.txt"), []byte(hash+"  MHXStudio.exe\n"), 0600))
	zpath := filepath.Join(*out, "MHXStudio-Windows-x64.zip")
	zf, e := os.OpenFile(zpath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	fail(e)
	z := zip.NewWriter(zf)
	add := func(src, name string) {
		f, e := os.Open(src)
		fail(e)
		defer f.Close()
		w, e := z.Create(name)
		fail(e)
		_, e = io.Copy(w, f)
		fail(e)
	}
	add(*exe, "MHXStudio.exe")
	add("docs/START_HERE.txt", "START_HERE.txt")
	add("docs/TESTING.md", "TESTING.md")
	add("docs/SOURCE_INVENTORY.md", "SOURCE_INVENTORY.md")
	add("docs/ONLINE_UPDATE_ACCEPTANCE.md", "ONLINE_UPDATE_ACCEPTANCE.md")
	add("docs/SINGLE_GAME_ACCEPTANCE.md", "SINGLE_GAME_ACCEPTANCE.md")
	add("docs/DELIVERY_RULES.md", "DELIVERY_RULES.md")
	add("docs/HANDOFF_ACCEPTANCE_20261029.md", "HANDOFF_ACCEPTANCE_20261029.md")
	add("docs/HANDOFF_EVIDENCE_REPLAY.json", "HANDOFF_EVIDENCE_REPLAY.json")
	add("docs/SCOPE_REMOVAL_20261029.md", "SCOPE_REMOVAL_20261029.md")
	add("docs/GROUP_LOGIN_ACCEPTANCE.md", "GROUP_LOGIN_ACCEPTANCE.md")
	add("docs/ON_OPEN_TEAM_ACCEPTANCE.md", "ON_OPEN_TEAM_ACCEPTANCE.md")
	add("docs/CURRENT_FLOW.md", "CURRENT_FLOW.md")
	add(filepath.Join(*out, "update.json"), "update.json")
	add(filepath.Join(*out, "SHA256SUMS.txt"), "SHA256SUMS.txt")
	if *report != "" {
		add(*report, "WINDOWS_AUTOMATED_TESTS.json")
	}
	fail(z.Close())
	fail(zf.Close())
}
