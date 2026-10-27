package ldplayer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Check struct{ Kind, Path, Source, Result string }
type SearchPlan struct {
	Installations []Installation
	ADBs          []string
	Address       string
	Checks        []Check
}
type Detection struct {
	Client                                           Client
	Instances                                        []Instance
	Source, Reason, ValidatedDirectory, ValidatedADB string
	Checks                                           []Check
}

// Report missing candidates too: D:\LDPlayer14 is a hint, never assumed valid.
func searchPlan(configured, savedDirectory, savedADB string, dirs []installDirectory, extras []string, address string) SearchPlan {
	all := []installDirectory{{configured, "当前设置"}, {savedDirectory, "上次核验的目录"}, {`D:\LDPlayer14`, "曾使用的候选路径"}, {`C:\LDPlayer14`, "常用安装目录"}, {`D:\leidian\LDPlayer14`, "常用安装目录"}, {`C:\leidian\LDPlayer14`, "常用安装目录"}}
	all = append(all, dirs...)
	p := SearchPlan{Address: address}
	seenDir, seenFile := map[string]bool{}, map[string]bool{}
	check := func(kind, path, source string) bool {
		path, e := filepath.Abs(path)
		if e != nil {
			return false
		}
		key := strings.ToLower(filepath.Clean(path))
		if seenFile[key] {
			return false
		}
		seenFile[key] = true
		st, e := os.Stat(path)
		result := "存在，待核验"
		if os.IsNotExist(e) {
			result = "不存在"
		} else if e != nil {
			result = "无法访问：" + e.Error()
		} else if st.IsDir() {
			result = "不是可执行文件"
		}
		p.Checks = append(p.Checks, Check{kind, path, source, result})
		return e == nil && !st.IsDir()
	}
	addADB := func(path, source string) {
		if path != "" && check("ADB程序", path, source) {
			absolute, _ := filepath.Abs(path)
			p.ADBs = append(p.ADBs, absolute)
		}
	}
	addADB(savedADB, "上次核验的ADB")
	for _, d := range all {
		path := strings.Trim(strings.TrimSpace(d.path), "\"")
		if path == "" {
			continue
		}
		if strings.EqualFold(filepath.Ext(path), ".exe") {
			path = filepath.Dir(path)
		}
		path, e := filepath.Abs(path)
		if e != nil {
			continue
		}
		key := strings.ToLower(filepath.Clean(path))
		if seenDir[key] {
			continue
		}
		seenDir[key] = true
		if len(seenDir) > 128 {
			p.Checks = append(p.Checks, Check{Kind: "搜索范围", Result: "目录候选超过128个，后续候选未检查"})
			break
		}
		adb := ""
		for _, a := range []string{filepath.Join(path, "adb.exe"), filepath.Join(path, "adb", "adb.exe")} {
			if check("ADB程序", a, d.source) {
				p.ADBs = append(p.ADBs, a)
				if adb == "" {
					adb = a
				}
			} else {
				for _, known := range p.ADBs {
					if strings.EqualFold(known, a) && adb == "" {
						adb = known
					}
				}
			}
		}
		for _, name := range []string{"ldconsole.exe", "dnconsole.exe"} {
			console := filepath.Join(path, name)
			if check("雷电管理程序", console, d.source) {
				source := d.source
				if d.path == configured && configured != "" {
					source = "已选择的目录"
				}
				p.Installations = append(p.Installations, Installation{console, adb, source})
			}
		}
	}
	for _, path := range extras {
		addADB(path, "PATH、SDK或主程序目录")
	}
	return p
}
func BuildSearchPlan(configured, savedDirectory, savedADB string) SearchPlan {
	var extras []string
	if p, e := exec.LookPath("adb.exe"); e == nil {
		extras = append(extras, p)
	}
	if p, e := os.Executable(); e == nil {
		extras = append(extras, filepath.Join(filepath.Dir(p), "adb.exe"))
	}
	for _, key := range []string{"ANDROID_SDK_ROOT", "ANDROID_HOME"} {
		if s := os.Getenv(key); s != "" {
			extras = append(extras, filepath.Join(s, "platform-tools", "adb.exe"))
		}
	}
	if s := os.Getenv("LOCALAPPDATA"); s != "" {
		extras = append(extras, filepath.Join(s, "Android", "Sdk", "platform-tools", "adb.exe"))
	}
	return searchPlan(configured, savedDirectory, savedADB, systemDirectories(), extras, DefaultADBAddress())
}

func Detect(ctx context.Context, plan SearchPlan, runner Runner, onInventory func(Client, []Instance)) Detection {
	out := Detection{Checks: append([]Check(nil), plan.Checks...)}
	client, inventory, source, e := SelectInstallation(ctx, plan.Installations, runner)
	if e == nil {
		out.Client, out.Source = client, source
		out.ValidatedDirectory = filepath.Dir(client.Console)
		out.Checks = append(out.Checks, Check{"实例列表", client.Console, source, fmt.Sprintf("读取成功：%d台", len(inventory))})
		if onInventory != nil {
			onInventory(client, inventory)
		}
		out.Instances, e = client.Bind(ctx, inventory)
		if e != nil {
			out.Reason = e.Error()
		}
		if client.ADB != "" {
			for _, d := range out.Instances {
				if d.Serial != "" {
					out.ValidatedADB = client.ADB
					break
				}
			}
		}
		// Unavailable ADB bindings may still be visible through the local server.
		needFallback := len(inventory) == 0
		for _, d := range out.Instances {
			if d.Running() && d.Serial == "" {
				needFallback = true
			}
		}
		if !needFallback {
			return out
		}
	} else {
		out.Reason = e.Error()
		out.Checks = append(out.Checks, Check{Kind: "实例列表", Result: e.Error()})
	}
	if ctx.Err() != nil {
		return out
	}
	server := ADBServer{plan.Address}
	sub, cancel := context.WithTimeout(ctx, 6*time.Second)
	devices, serverErr := server.Devices(sub)
	cancel()
	var identityRead func(context.Context, string, string) (string, error)
	if serverErr == nil {
		identityRead = server.Identity
		out.Checks = append(out.Checks, Check{"ADB本地服务", plan.Address, "ADB智能套接字", fmt.Sprintf("连接成功：%d个设备", len(devices))})
		if out.Source == "" {
			out.Source = "本机ADB服务 " + plan.Address
		}
	} else {
		out.Checks = append(out.Checks, Check{"ADB本地服务", plan.Address, "ADB智能套接字", serverErr.Error()})
		for _, adb := range plan.ADBs {
			if ctx.Err() != nil {
				break
			}
			c := Client{ADB: adb, Runner: runner}
			b, e := c.run(ctx, adb, "devices", "-l")
			if e == nil && !strings.Contains(string(b), "List of devices attached") {
				e = fmt.Errorf("没有返回有效的ADB设备列表")
			}
			if e != nil {
				out.Checks = append(out.Checks, Check{"ADB程序", adb, "直接ADB扫描", e.Error()})
				continue
			}
			devices = parseADBDevices(string(b))
			out.Checks = append(out.Checks, Check{"ADB程序", adb, "直接ADB扫描", fmt.Sprintf("读取成功：%d个设备", len(devices))})
			out.ValidatedADB = adb
			if out.Client.ADB == "" {
				out.Client.ADB = adb
			}
			if out.Source == "" {
				out.Source = "ADB程序 " + adb
			}
			identityRead = func(ctx context.Context, s, command string) (string, error) {
				b, e := c.adb(ctx, s, "shell", command)
				return string(b), e
			}
			break
		}
	}
	if identityRead == nil {
		out.Reason += "；没有可用ADB连接。已检查的程序路径与具体错误见“检测详情”。"
		return out
	}
	if out.Client.Console != "" {
		if serverErr == nil && out.Client.ADB == "" {
			out.Client.Server = plan.Address
		}
		if bound, e := out.Client.Bind(ctx, inventory); e == nil {
			// A supplemental server may be empty or unrelated to the working
			// executable's server. Never discard an already verified binding.
			for i := range out.Instances {
				if out.Instances[i].Serial != "" {
					continue
				}
				for _, d := range bound {
					if d.Index == out.Instances[i].Index && d.Serial != "" {
						out.Instances[i] = d
						break
					}
				}
			}
		}
	}
	used := map[string]bool{}
	usedIdentity := map[identity]bool{}
	for _, d := range out.Instances {
		if d.Serial != "" {
			used[d.Serial] = true
			if d.BootID != "" {
				usedIdentity[identity{d.AndroidID, d.BootID}] = true
			}
		}
	}
	fallback := make([]Instance, len(devices))
	parallel(ctx, len(devices), func(i int) {
		v := devices[i]
		d := Instance{Index: -1, Name: "ADB设备", Serial: v.Serial, Unmapped: true, ADBState: v.State, Android: v.State == "device"}
		if v.State == "device" {
			b, e := identityRead(ctx, v.Serial, "settings get secure android_id")
			if e == nil {
				d.AndroidID = idFrom(b)
			} else {
				d.ConnectionError = e.Error()
			}
			b, e = identityRead(ctx, v.Serial, "cat /proc/sys/kernel/random/boot_id")
			if e == nil {
				d.BootID = bootFrom(b)
			}
			if d.AndroidID == "" && d.ConnectionError == "" {
				d.ConnectionError = "无法读取ADB身份，游戏操作保持停止"
			}
		} else {
			d.ConnectionError = "ADB状态为 " + v.State + "，不执行游戏操作"
		}
		fallback[i] = d
	})
	added := 0
	for _, d := range fallback {
		key := identity{d.AndroidID, d.BootID}
		if used[d.Serial] || (d.BootID != "" && usedIdentity[key]) {
			continue
		}
		out.Instances = append(out.Instances, d)
		added++
		// Without a verified console index, equal identities do not prove two
		// serials are aliases. Keep both visible and prohibit game control.
	}
	if added > 0 {
		out.Reason = "已通过ADB发现设备；无法独立证明实例编号的设备按序列号显示，不猜测编号或执行游戏操作。"
	} else if out.Client.Console == "" {
		out.Reason = "ADB服务可用，但没有已连接设备；未找到管理程序，无法枚举未启动实例。"
	}
	return out
}
