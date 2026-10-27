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
	m := update.Manifest{Schema: 1, Version: *ver, Platform: "windows-amd64", URL: "https://github.com/ab393911/MHX-Studio-Updates/releases/download/v" + *ver + "/MHXStudio.exe", SHA256: hash, Size: st.Size(), Notes: "一键在线更新验收版：新版本取消更新二次确认，增加配置快照、回滚恢复及重启后成功提示；保留preview.15雷电检测和启动。首次从旧preview.15升级仍需其原确认框，配置快照由新程序在健康确认前补建；后续升级在替换前备份。", SourceCommit: *commit, WindowsTested: false}
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
	add(filepath.Join(*out, "update.json"), "update.json")
	add(filepath.Join(*out, "SHA256SUMS.txt"), "SHA256SUMS.txt")
	if *report != "" {
		add(*report, "WINDOWS_AUTOMATED_TESTS.json")
	}
	fail(z.Close())
	fail(zf.Close())
}
