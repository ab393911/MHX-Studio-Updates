package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type diagnosticBundle struct{ Directory, Summary string }

func saveRecognitionBundle(root string, index int, png []byte, document map[string]any, report recognitionReport) (diagnosticBundle, error) {
	if _, err := automation.Decode(png); len(png) > 0 && err != nil {
		return diagnosticBundle{}, err
	}
	dir := filepath.Join(root, "diagnostics", fmt.Sprintf("recognition-%d-%d", index, time.Now().UnixNano()))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return diagnosticBundle{}, err
	}
	var s strings.Builder
	if len(png) == 0 {
		s.WriteString("本次未取得有效截图；没有匹配分数，不代表识别通过。\r\n")
	}
	fmt.Fprintf(&s, "MHX Studio 页面识别诊断（只读，未点击）\r\n实例：%d；ADB：%v\r\n算法：%s；规则SHA：%s\r\n截图SHA：%s；要求分辨率：960×540\r\n前台：%v；绑定包：%v\r\n", index, document["serial"], report.AlgorithmVersion, report.EffectiveSHA, report.ScreenshotSHA, document["foreground"], document["expected_package"])
	for _, page := range report.Pages {
		if page.Source == "built-in" && !page.Passed {
			continue
		}
		fmt.Fprintf(&s, "\r\n页面 %s，来源 %s，版本%d：通过=%t，特征%d/%d（至少%d）\r\n", page.Label+" ("+page.Name+")", page.Source, page.Revision, page.Passed, page.MatchedFeatures, len(page.Anchors), page.RequiredMatches)
		for n, a := range page.Anchors {
			fmt.Fprintf(&s, "  特征%d：平均误差 %.3f ≤ %.3f；差值>%d的点 %d ≤ %d；通过=%t；%s\r\n", n+1, a.MeanAbsoluteError, a.MeanLimit, a.PixelDeltaLimit, a.ChangedPixels, a.ChangedPixelLimit, a.Passed, a.Error)
		}
	}
	if report.FailureReason != "" {
		fmt.Fprintf(&s, "\r\n失败原因：%s\r\n", report.FailureReason)
	} else {
		fmt.Fprintf(&s, "\r\n匹配页面：%s；页面共同特征不证明角色身份。\r\n", strings.Join(report.Matches, "、"))
	}
	s.WriteString("\r\n模板与当前采样见下方JSON；未自动修改规则。此报告只反映本次截图。\r\n")
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return diagnosticBundle{}, err
	}
	summary := s.String() + "\r\n" + string(raw)
	for name, data := range map[string][]byte{"screenshot.png": png, "report.json": raw, "summary.txt": []byte(summary)} {
		if len(data) == 0 {
			continue
		}
		if err = filesafe.Write(filepath.Join(dir, name), data, 0600); err != nil {
			return diagnosticBundle{}, err
		}
	}
	// Snapshot only recognition rules, never config.json, accounts, orders or SQLite.
	for name, data := range map[string][]byte{"instance-profile.json": report.InstanceSnapshot, "shared-profile.json": report.SharedSnapshot} {
		if len(data) > 0 {
			if err = filesafe.Write(filepath.Join(dir, name), data, 0600); err != nil {
				return diagnosticBundle{}, err
			}
		}
	}
	return diagnosticBundle{dir, summary}, nil
}
func exportRecognitionBundle(bundle diagnosticBundle, destination string) error {
	// Fixed allowlist keeps unrelated data and path traversal out of exports.
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, name := range []string{"screenshot.png", "report.json", "summary.txt", "instance-profile.json", "shared-profile.json"} {
		raw, err := os.ReadFile(filepath.Join(bundle.Directory, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		f, err := w.Create(name)
		if err != nil {
			return err
		}
		if _, err = io.Copy(f, bytes.NewReader(raw)); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	return filesafe.Write(destination, out.Bytes(), 0600)
}
