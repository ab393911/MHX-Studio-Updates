//go:build windows

package main

import (
	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/update"
)

func (a *app) updateLog(s string) { a.ui(func() { setText(a.controls[209], s); a.log(s) }) }
func (a *app) pollUpdateResult() {
	if a.updateResultDone || a.updateResultPath == "" {
		return
	}
	var r update.Result
	if filesafe.ReadJSON(a.updateResultPath, &r) != nil {
		return
	}
	var message string
	switch r.Status {
	case "committed":
		message = "更新成功：当前版本 " + version + "；旧EXE和配置备份已保留。"
	case "rolled_back":
		message = "更新失败，已恢复旧版本 " + version + "，配置已保留。"
	case "unchanged":
		message = "更新未安装，已恢复运行当前版本 " + version + "，文件保持原位。"
	case "recovery_required":
		message = "更新未完成，需要恢复；旧版和配置备份均已保留。"
	default:
		return
	}
	a.updateResultDone = true
	setText(a.controls[209], message)
	a.log(message)
	if r.Detail != "" && r.Status != "committed" {
		a.log("原因：" + r.Detail)
	}
	if r.Backup != "" {
		a.log("旧EXE备份：" + r.Backup)
	}
	if r.ConfigBackup != "" {
		a.log("配置备份：" + r.ConfigBackup)
	}
}
