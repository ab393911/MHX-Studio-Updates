package ldplayer

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
)

type LaunchOptions struct {
	Timeout, PollInterval, LaunchInterval time.Duration
	OnLog                                 func(string)
}

// Start launches only existing, explicitly requested indices. Validate every
// index before the first mutation, skip running instances, and verify Android
// readiness rather than treating a successful command as a completed boot.
func (c Client) Start(ctx context.Context, indices []int, opt LaunchOptions) ([]Instance, error) {
	if len(indices) == 0 || len(indices) > 5 {
		return nil, errors.New("每次只能启动指定的 1～5 台模拟器")
	}
	if opt.Timeout <= 0 {
		opt.Timeout = 150 * time.Second
	}
	if opt.PollInterval <= 0 {
		opt.PollInterval = 2 * time.Second
	}
	if opt.LaunchInterval <= 0 {
		opt.LaunchInterval = time.Second
	}
	if opt.OnLog == nil {
		opt.OnLog = func(string) {}
	}
	ctx, cancel := context.WithTimeout(ctx, opt.Timeout)
	defer cancel()
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	all, e := c.Instances(ctx)
	if e != nil {
		return all, e
	}
	byIndex := map[int]Instance{}
	for _, d := range all {
		byIndex[d.Index] = d
	}
	seen := map[int]bool{}
	for _, n := range indices {
		if n < 0 || seen[n] {
			return all, errors.New("模拟器编号无效或重复")
		}
		seen[n] = true
		if _, ok := byIndex[n]; !ok {
			return all, fmt.Errorf("编号 %d 不存在，未启动任何新实例；请使用列表中的雷电原始编号", n)
		}
	}
	launched := false
	for _, n := range indices {
		if e := ctx.Err(); e != nil {
			return all, e
		}
		if byIndex[n].Running() {
			opt.OnLog(fmt.Sprintf("编号 %d 已运行，保留当前窗口，不重复启动。", n))
			continue
		}
		if launched {
			if e = pause(ctx, opt.LaunchInterval); e != nil {
				return all, e
			}
		}
		if _, e = c.run(ctx, c.Console, "launch", "--index", strconv.Itoa(n)); e != nil {
			return all, fmt.Errorf("编号 %d 启动命令失败，已打开的实例保留：%w", n, e)
		}
		launched = true
		opt.OnLog(fmt.Sprintf("已发送启动命令：编号 %d；正在等待安卓就绪。", n))
	}
	for {
		all, e = c.Instances(ctx)
		if e != nil {
			return all, fmt.Errorf("启动后状态检查失败，已打开的实例保留：%w", e)
		}
		ready := map[int]bool{}
		for _, d := range all {
			ready[d.Index] = d.Android && d.PID > 0
		}
		complete := true
		for _, n := range indices {
			if !ready[n] {
				complete = false
			}
		}
		if complete {
			opt.OnLog("指定模拟器的安卓启动状态已确认，正在核对 ADB 连接。")
			return all, nil
		}
		if e = pause(ctx, opt.PollInterval); e != nil {
			return all, fmt.Errorf("等待模拟器启动结束：%w；已打开的窗口保留，可点“检测实例”重查", e)
		}
	}
}

func pause(ctx context.Context, delay time.Duration) error {
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
