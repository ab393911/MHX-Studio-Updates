//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/ldplayer"
)

func (a *app) detect() { a.detectInstances(true) }

func (a *app) detectInstances(manual bool) {
	if a.busy.Load() {
		return
	}
	if manual {
		if e := a.save(); e != nil {
			a.log(e.Error())
			return
		}
	}
	configured := strings.TrimSpace(text(a.controls[201]))
	a.nextScan = time.Now().Add(12 * time.Second)
	a.backgroundScan = !manual
	setText(a.controls[208], "正在检测雷电实例；列表发现和 ADB 就绪状态分别核对……")
	a.work(func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 70*time.Second)
		defer cancel()
		saved, loadErr := loadDiscovery(a.root)
		plan := ldplayer.BuildSearchPlan(configured, saved.Directory, saved.ADB)
		result := ldplayer.Detect(ctx, plan, nil, func(client ldplayer.Client, all []ldplayer.Instance) {
			a.ui(func() {
				a.showInstances(client, all, false)
				setText(a.controls[208], ldplayer.Summary(all)+"；正在核对ADB身份。")
			})
		})
		if loadErr != nil {
			result.Checks = append(result.Checks, ldplayer.Check{Kind: "发现记录", Result: loadErr.Error() + "；原文件保留，继续自动搜索"})
		}
		if ctx.Err() == nil {
			if e := rememberDiscovery(a.root, result); e != nil {
				result.Reason += "；保存有效路径失败：" + e.Error()
			}
		}
		if e := writeDetectionReport(a.root, version, result); e != nil {
			result.Reason += "；写入检测报告失败：" + e.Error()
		}
		a.ui(func() {
			before := a.lastInventory
			a.showInstances(result.Client, result.Instances, true)
			if before != a.lastInventory || manual {
				if result.Reason != "" {
					a.log(result.Reason)
				}
				if len(result.Instances) == 0 {
					shown := 0
					for _, c := range result.Checks {
						if c.Path != "" {
							a.log("已检查：" + c.Path + "；" + c.Result)
							shown++
							if shown >= 8 {
								break
							}
						}
					}
					a.log("完整搜索路径和错误请点“检测详情”；可继续使用“选择雷电程序”作为备用。")
					for _, c := range result.Checks {
						if c.Kind == "ADB本地服务" {
							a.log("ADB服务检查：" + c.Path + "；" + c.Result)
						}
					}
				}
			}
			if len(result.Instances) == 0 {
				setText(a.controls[208], "未发现实例；已尝试管理程序与ADB，具体原因见“检测详情”。")
			}
			a.nextScan = time.Now().Add(12 * time.Second)
		})
		return ctx.Err()
	})
}

func (a *app) showInstances(client ldplayer.Client, all []ldplayer.Instance, final bool) {
	previous := ""
	if strings.EqualFold(a.client.Console, client.Console) {
		i := comboIndex(a.controls[202])
		if i >= 0 && i < len(a.devices) {
			previous = a.devices[i].Key()
		}
	}
	client.OnConnection = func(event string) { a.ui(func() { a.connectionLog(event) }) }
	a.client, a.devices = client, all
	currentDir := strings.TrimSpace(text(a.controls[201]))
	if client.Console != "" && (currentDir == "" || strings.EqualFold(currentDir, a.cfg.InstallDir) || strings.EqualFold(currentDir, filepath.Dir(client.Console))) {
		setText(a.controls[201], filepath.Dir(client.Console))
	}
	send(a.controls[202], 0x14B, 0, 0)
	selected, firstRunning, firstReady := -1, -1, -1
	for i, d := range all {
		if d.Unmapped {
			comboAdd(a.controls[202], fmt.Sprintf("ADB设备 %s  %s", d.Serial, d.Status()))
		} else {
			comboAdd(a.controls[202], fmt.Sprintf("编号 %d  %s  %s  %s", d.Index, d.Name, d.Status(), d.Serial))
		}
		if d.Key() == previous {
			selected = i
		}
		if d.Running() && firstRunning < 0 {
			firstRunning = i
		}
		if d.Serial != "" && !d.Unmapped && firstReady < 0 {
			firstReady = i
		}
	}
	if selected < 0 {
		selected = firstReady
	}
	if selected < 0 {
		selected = firstRunning
	}
	if selected < 0 && len(all) > 0 {
		selected = 0
	}
	if selected >= 0 {
		send(a.controls[202], 0x14E, uintptr(selected), 0)
		if !a.groupSeeded && !all[selected].Unmapped {
			if strings.TrimSpace(text(a.controls[205])) == strconv.Itoa(a.cfg.GroupStart) {
				setText(a.controls[205], strconv.Itoa(all[selected].Index))
			}
			a.groupSeeded = true
		}
	}
	if final {
		for _, event := range a.connectionHistory.Observe(time.Now(), all) {
			a.connectionLog(event)
		}
		setText(a.controls[208], ldplayer.Summary(all)+"；扫描完成 "+time.Now().Format("15:04:05")+"；空闲12秒后再扫描。")
	} else {
		setText(a.controls[208], ldplayer.Summary(all)+"；扫描中，ADB身份待本轮核验，暂不能操作。")
	}
	if final {
		b, _ := json.Marshal(struct {
			Console string
			Devices []ldplayer.Instance
		}{client.Console, all})
		sig := string(b)
		if sig != a.lastInventory {
			location := client.Console
			if location == "" {
				location = "未定位；ADB设备按实际序列号显示"
			}
			a.log(ldplayer.Summary(all) + "。控制台：" + location)
			for _, d := range all {
				if d.ConnectionError != "" {
					if d.Unmapped {
						a.log("ADB设备 " + d.Serial + "：" + d.ConnectionError)
					} else {
						a.log(fmt.Sprintf("编号 %d：%s", d.Index, d.ConnectionError))
					}
				}
			}
			a.lastInventory = sig
		}
	}
}

func (a *app) startEmulators(five bool) {
	if e := a.save(); e != nil {
		a.log(e.Error())
		return
	}
	if a.client.Console == "" || len(a.devices) == 0 {
		a.log("没有已核验的雷电管理程序，不能启动未运行的实例。已运行设备仍可通过ADB发现；具体已检查路径见“检测详情”。")
		return
	}
	var indices []int
	if five {
		for n := a.cfg.GroupStart; n < a.cfg.GroupStart+5; n++ {
			indices = append(indices, n)
		}
	} else {
		i := comboIndex(a.controls[202])
		if i < 0 || i >= len(a.devices) {
			a.log("请从当前实例列表选择要启动的模拟器。")
			return
		}
		if a.devices[i].Unmapped {
			a.log("该ADB设备的雷电编号尚未核对，未发送启动命令；请查看“检测详情”。")
			return
		}
		indices = []int{a.devices[i].Index}
	}
	client := a.client
	a.log(fmt.Sprintf("准备启动指定雷电编号 %v；已运行的实例会保留。", indices))
	a.work(func(ctx context.Context) error {
		all, e := client.Start(ctx, indices, ldplayer.LaunchOptions{OnLog: a.logAsync})
		if len(all) > 0 {
			if ctx.Err() == nil {
				bound, bindErr := client.Bind(ctx, all)
				all = bound
				e = errors.Join(e, bindErr)
			}
			final := append([]ldplayer.Instance(nil), all...)
			a.ui(func() { a.showInstances(client, final, true); a.nextScan = time.Now().Add(12 * time.Second) })
		}
		if e == nil {
			a.logAsync("启动状态检查完成；ADB 待就绪的实例会继续显示具体原因。")
		}
		return e
	})
}

func (a *app) detectionDetails() {
	path := filepath.Join(a.root, "diagnostics", "detection.txt")
	if _, e := os.Stat(path); e != nil {
		a.log("检测报告尚未生成，请等待本轮检测完成。")
		return
	}
	if e := exec.Command("notepad.exe", path).Start(); e != nil {
		a.log("打开检测报告失败：" + e.Error() + "；路径：" + path)
	}
}

// Keep connection evidence beyond the bounded on-screen log and software restart.
func (a *app) connectionLog(event string) {
	a.log(event)
	dir := filepath.Join(a.root, "diagnostics")
	if e := os.MkdirAll(dir, 0700); e != nil {
		a.log("连接记录保存失败：" + e.Error())
		return
	}
	f, e := os.OpenFile(filepath.Join(dir, "adb-connections.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		a.log("连接记录保存失败：" + e.Error())
		return
	}
	_, writeErr := fmt.Fprintln(f, time.Now().Format(time.RFC3339)+" "+event)
	if e = errors.Join(writeErr, f.Close()); e != nil {
		a.log("连接记录保存失败：" + e.Error())
	}
}
