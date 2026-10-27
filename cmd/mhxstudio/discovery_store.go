package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/ldplayer"
)

type discoveryRecord struct {
	Directory string `json:"directory"`
	ADB       string `json:"adb"`
}

func loadDiscovery(root string) (discoveryRecord, error) {
	var r discoveryRecord
	e := filesafe.ReadJSON(filepath.Join(root, "ldplayer-discovery.json"), &r)
	if os.IsNotExist(e) {
		return r, nil
	}
	return r, e
}
func rememberDiscovery(root string, d ldplayer.Detection) error {
	if d.ValidatedDirectory == "" && d.ValidatedADB == "" {
		return nil
	}
	r, e := loadDiscovery(root)
	if e != nil {
		return fmt.Errorf("上次发现记录无法读取，原文件保留：%w", e)
	}
	next := r
	if d.ValidatedDirectory != "" {
		next.Directory = d.ValidatedDirectory
	}
	if d.ValidatedADB != "" {
		next.ADB = d.ValidatedADB
	}
	if next == r {
		return nil
	}
	path := filepath.Join(root, "ldplayer-discovery.json")
	if _, e := os.Stat(path); e == nil {
		history := filepath.Join(root, "discovery-history")
		if e = os.MkdirAll(history, 0700); e != nil {
			return e
		}
		if e = filesafe.CopyNew(path, filepath.Join(history, fmt.Sprintf("discovery-%d.json", time.Now().UnixNano()))); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	return filesafe.JSON(path, next)
}
func writeDetectionReport(root, version string, d ldplayer.Detection) error {
	dir := filepath.Join(root, "diagnostics")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	obj := struct {
		Version, At, Source, Reason string
		Checks                      []ldplayer.Check
		Instances                   []ldplayer.Instance
	}{version, time.Now().Format(time.RFC3339), d.Source, d.Reason, d.Checks, d.Instances}
	if e := filesafe.JSON(filepath.Join(dir, "detection.json"), obj); e != nil {
		return e
	}
	var b strings.Builder
	fmt.Fprintf(&b, "MHX Studio %s 检测报告\r\n时间：%s\r\n来源：%s\r\n%s\r\n%s\r\n\r\n已检查的路径与连接：\r\n", version, obj.At, d.Source, ldplayer.Summary(d.Instances), d.Reason)
	for _, c := range d.Checks {
		fmt.Fprintf(&b, "[%s] %s\r\n来源：%s；结果：%s\r\n", c.Kind, c.Path, c.Source, c.Result)
	}
	fmt.Fprintln(&b, "\r\n实例与ADB映射：")
	for _, i := range d.Instances {
		fmt.Fprintf(&b, "编号=%d；编号未匹配=%t；名称=%s；ADB=%s；状态=%s；原因=%s\r\n", i.Index, i.Unmapped, i.Name, i.Serial, i.Status(), i.ConnectionError)
	}
	return filesafe.Write(filepath.Join(dir, "detection.txt"), []byte("\ufeff"+b.String()), 0600)
}
