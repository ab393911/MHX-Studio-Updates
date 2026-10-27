package ldplayer

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type identity struct{ android, boot string }

func bootFrom(s string) string {
	for _, line := range strings.Fields(s) {
		v := strings.ToLower(line)
		if len(v) != 36 || v[8] != '-' || v[13] != '-' || v[18] != '-' || v[23] != '-' {
			continue
		}
		h := strings.ReplaceAll(v, "-", "")
		if _, e := strconv.ParseUint(h[:16], 16, 64); e != nil {
			continue
		}
		if _, e := strconv.ParseUint(h[16:], 16, 64); e == nil && h != strings.Repeat("0", 32) {
			return v
		}
	}
	return ""
}

// At most four vendor/ADB commands run concurrently. Offline instances cannot
// consume an unbounded number of subprocesses or block the complete inventory.
func parallel(ctx context.Context, n int, f func(int)) {
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() == nil {
					f(i)
				}
			}
		}()
	}
	for i := 0; i < n; i++ {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return
		}
	}
	close(jobs)
	wg.Wait()
}

// Bind never discards the inventory because one device is offline. Android IDs
// alone are insufficient for cloned VMs; independent boot IDs distinguish clones
// and also allow two ADB serial aliases for the same boot. Ambiguous identities
// remain visible, but are never allowed to receive clicks.
func (c Client) Bind(ctx context.Context, inventory []Instance) ([]Instance, error) {
	ctx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	all := append([]Instance(nil), inventory...)
	for i := range all {
		all[i].Serial, all[i].AndroidID, all[i].BootID, all[i].ConnectionError = "", "", "", ""
	}
	if c.ADB == "" && c.Server == "" {
		for i := range all {
			if all[i].Running() {
				all[i].ConnectionError = "未找到 adb.exe；可启动模拟器，暂不能控制游戏"
			}
		}
		return all, nil
	}
	ids := make([]identity, len(all))
	parallel(ctx, len(all), func(i int) {
		x := all[i]
		if !x.Android || x.PID <= 0 {
			return
		}
		b, e := c.run(ctx, c.Console, "adb", "--index", strconv.Itoa(x.Index), "--command", "shell settings get secure android_id")
		if e != nil {
			all[i].ConnectionError = "实例 ADB 未就绪：" + e.Error()
			return
		}
		ids[i].android = idFrom(string(b))
		b, e = c.run(ctx, c.Console, "adb", "--index", strconv.Itoa(x.Index), "--command", "shell cat /proc/sys/kernel/random/boot_id")
		if e == nil {
			ids[i].boot = bootFrom(string(b))
		}
		if ids[i].android == "" {
			all[i].ConnectionError = "未取得 Android 身份；请检查雷电 ADB 本地连接"
		}
	})
	ready := 0
	for _, id := range ids {
		if id.android != "" {
			ready++
		}
	}
	if ready == 0 {
		return all, ctx.Err()
	}
	var serials []string
	var e error
	if c.Server != "" {
		var ds []ADBDevice
		ds, e = (ADBServer{c.Server}).Devices(ctx)
		for _, d := range ds {
			if d.State == "device" {
				serials = append(serials, d.Serial)
			}
		}
	} else {
		var b []byte
		b, e = c.run(ctx, c.ADB, "devices", "-l")
		serials = ParseDevices(string(b))
	}
	if e != nil {
		for i, id := range ids {
			if id.android != "" {
				all[i].ConnectionError = "ADB 设备列表读取失败：" + e.Error()
			}
		}
		return all, ctx.Err()
	}
	if len(serials) > 256 {
		serials = serials[:256]
	}
	direct := make([]identity, len(serials))
	read := func(ctx context.Context, s, command string) (string, error) {
		if c.Server != "" {
			return (ADBServer{c.Server}).Identity(ctx, s, command)
		}
		b, e := c.adb(ctx, s, "shell", command)
		return string(b), e
	}
	parallel(ctx, len(serials), func(i int) {
		b, e := read(ctx, serials[i], "settings get secure android_id")
		if e != nil {
			return
		}
		direct[i].android = idFrom(string(b))
		b, e = read(ctx, serials[i], "cat /proc/sys/kernel/random/boot_id")
		if e == nil {
			direct[i].boot = bootFrom(string(b))
		}
	})
	byIdentity := map[identity][]string{}
	counts := map[identity]int{}
	for i, id := range direct {
		if id.android != "" {
			byIdentity[id] = append(byIdentity[id], serials[i])
		}
	}
	for _, id := range ids {
		if id.android != "" {
			counts[id]++
		}
	}
	for i, id := range ids {
		if id.android == "" {
			continue
		}
		matches := byIdentity[id]
		if counts[id] != 1 || len(matches) == 0 || (id.boot == "" && len(matches) != 1) {
			all[i].ConnectionError = "ADB 身份无法唯一核对；实例仍已检测到，游戏操作已停用"
			continue
		}
		sort.Strings(matches)
		all[i].Serial, all[i].AndroidID, all[i].BootID = matches[0], id.android, id.boot
		all[i].ConnectionError = ""
	}
	return all, ctx.Err()
}

func (d Instance) Status() string {
	if d.Unmapped {
		if d.ADBState != "device" {
			return "ADB " + d.ADBState + " · 编号待核对"
		}
		return "ADB 已发现 · 编号待核对"
	}
	if d.Serial != "" {
		return "已运行 · ADB 已验证"
	}
	if !d.Running() {
		return "未启动"
	}
	if !d.Android {
		return "已运行 · 安卓启动中"
	}
	return "已运行 · ADB 待就绪"
}

func Summary(all []Instance) string {
	running, ready, known, unmapped := 0, 0, 0, 0
	for _, d := range all {
		if d.Unmapped {
			unmapped++
			continue
		}
		known++
		if d.Running() {
			running++
		}
		if d.Serial != "" && !d.Unmapped {
			ready++
		}
	}
	if known == 0 && unmapped > 0 {
		return fmt.Sprintf("发现 %d 个ADB设备，雷电实例编号待匹配", unmapped)
	}
	s := fmt.Sprintf("检测到 %d 台，已运行 %d 台，ADB 已验证 %d 台", known, running, ready)
	if unmapped > 0 {
		s += fmt.Sprintf("；另有%d个ADB设备待匹配", unmapped)
	}
	return s
}
