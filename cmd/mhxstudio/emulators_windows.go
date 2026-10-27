//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		client, all, source, e := ldplayer.SelectInstallation(ctx, ldplayer.FindInstallations(configured), nil)
		if e != nil {
			a.ui(func() {
				setText(a.controls[208], "自动检测未完成："+e.Error())
				if manual || a.lastInventory != "not-found" {
					a.log(e.Error())
					a.lastInventory = "not-found"
				}
				a.nextScan = time.Now().Add(12 * time.Second)
			})
			return nil
		}
		a.ui(func() {
			a.showInstances(client, all, false)
			setText(a.controls[208], ldplayer.Summary(all)+"；正在逐台核对 ADB。来源："+source)
		})
		bound, bindErr := client.Bind(ctx, all)
		a.ui(func() { a.showInstances(client, bound, true); a.nextScan = time.Now().Add(12 * time.Second) })
		return bindErr
	})
}

func (a *app) showInstances(client ldplayer.Client, all []ldplayer.Instance, final bool) {
	previous := -1
	if strings.EqualFold(a.client.Console, client.Console) {
		i := comboIndex(a.controls[202])
		if i >= 0 && i < len(a.devices) {
			previous = a.devices[i].Index
		}
	}
	a.client, a.devices = client, all
	currentDir := strings.TrimSpace(text(a.controls[201]))
	if currentDir == "" || strings.EqualFold(currentDir, a.cfg.InstallDir) || strings.EqualFold(currentDir, filepath.Dir(client.Console)) {
		setText(a.controls[201], filepath.Dir(client.Console))
	}
	send(a.controls[202], 0x14B, 0, 0)
	selected, firstRunning, firstReady := -1, -1, -1
	for i, d := range all {
		comboAdd(a.controls[202], fmt.Sprintf("编号 %d  %s  %s", d.Index, d.Name, d.Status()))
		if d.Index == previous {
			selected = i
		}
		if d.Running() && firstRunning < 0 {
			firstRunning = i
		}
		if d.Serial != "" && firstReady < 0 {
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
		if !a.groupSeeded {
			if strings.TrimSpace(text(a.controls[205])) == strconv.Itoa(a.cfg.GroupStart) {
				setText(a.controls[205], strconv.Itoa(all[selected].Index))
			}
			a.groupSeeded = true
		}
	}
	setText(a.controls[208], ldplayer.Summary(all)+"；空闲时每12秒自动刷新。")
	if final {
		b, _ := json.Marshal(struct {
			Console string
			Devices []ldplayer.Instance
		}{client.Console, all})
		sig := string(b)
		if sig != a.lastInventory {
			a.log(ldplayer.Summary(all) + "。控制台：" + client.Console)
			for _, d := range all {
				if d.ConnectionError != "" {
					a.log(fmt.Sprintf("编号 %d：%s", d.Index, d.ConnectionError))
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
		a.log("先自动检测实例列表，找到列表后再启动指定实例。")
		a.detectInstances(false)
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
