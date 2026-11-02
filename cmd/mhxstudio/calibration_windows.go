//go:build windows

package main

import (
	"context"
	"fmt"
	"image"
	"strconv"
	"syscall"
	"unsafe"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
)

type calibrationName struct{ key, label, button string }

var calibrationNames = func() []calibrationName {
	out := []calibrationName{{"notice", "公告页：黄色踏入仙途", "notice_enter"}, {"tree", "大树登录页：踏入仙途（若存在）", "tree_enter"}, {"roles", "角色列表公共区域 + 进入按钮", "enter"}, {"menu", "菜单展开页 + 设置按钮", "settings"}, {"settings", "设置页 + 返回登录按钮", "logout"}, {"logout_confirm", "返回登录确认弹窗（若存在）", "confirm_logout"}}
	for i := 1; i <= 5; i++ {
		out = append(out, calibrationName{fmt.Sprintf("selected_%d", i), fmt.Sprintf("角色列表：第 %d 个已选中 + 对应角色行范围", i), fmt.Sprintf("role_%d", i)})
	}
	out = append(out, calibrationName{"game", "游戏主界面：两处稳定特征 + 菜单按钮", "menu"})
	return out
}()

type calibration struct {
	shared       bool
	featureCount int
	app          *app
	hwnd         uintptr
	index        int
	name         calibrationName
	profile      automation.Profile
	img          image.Image
	pixels       []byte
	rects        []automation.Rect
	start        point
	dragging     bool
	label        uintptr
}
type bitmapInfo struct {
	Size                   uint32
	Width, Height          int32
	Planes, BitCount       uint16
	Compression, SizeImage uint32
	XPels, YPels           int32
	ClrUsed, ClrImportant  uint32
}
type paintStruct struct {
	DC                 uintptr
	Erase              int32
	Rect               struct{ L, T, R, B int32 }
	Restore, IncUpdate int32
	Reserved           [32]byte
}

func (a *app) startCalibration() {
	if a.cal != nil {
		a.info("已有标定窗口，请先保存或取消。")
		return
	}
	i := comboIndex(a.controls[206])
	if i < 0 || i >= len(calibrationNames) {
		return
	}
	a.startRuleCalibration(calibrationNames[i], false, 2)
}
func (a *app) startRuleCalibration(name calibrationName, shared bool, count int) {
	if a.cal != nil || count < 2 || count > 8 {
		return
	}
	d, e := a.selected()
	if e != nil {
		a.log(e.Error())
		return
	}
	profile, e := loadLocalProfile(a.root, d.Index)
	if shared {
		profile, e = readProfile(sharedProfilePath(a.root))
	}
	if e != nil {
		a.log(e.Error())
		return
	}
	selection := comboIndex(a.controls[206])
	request := a.currentGameTarget()
	client := a.client
	a.work(func(ctx context.Context) error {
		b, e := client.Screenshot(ctx, d)
		if e != nil {
			return e
		}
		a.cache.Add(strconv.Itoa(d.Index), b)
		img, e := automation.Decode(b)
		if e != nil {
			return e
		}
		a.gameUI(ctx, request, func() {
			if comboIndex(a.controls[206]) != selection {
				a.log("标定页面选择已变化，旧截图不再打开标定窗口。")
				return
			}
			a.log(fmt.Sprintf("标定取图：实例%d serial=%s AndroidID=%s boot=%s；实时960×540截图；无缓存", d.Index, d.Serial, d.AndroidID, d.BootID))
			c := &calibration{shared: shared, featureCount: count, app: a, index: d.Index, name: name, profile: profile, img: img}
			a.cal = c
			c.open()
		})
		return nil
	})
}
func (c *calibration) open() {
	c.pixels = make([]byte, 960*540*4)
	for y := 0; y < 540; y++ {
		for x := 0; x < 960; x++ {
			r, g, b, _ := c.img.At(x, y).RGBA()
			i := (y*960 + x) * 4
			c.pixels[i] = byte(b >> 8)
			c.pixels[i+1] = byte(g >> 8)
			c.pixels[i+2] = byte(r >> 8)
		}
	}
	inst, _, _ := moduleHandle.Call(0)
	h, _, _ := createWindow.Call(0, uintptr(unsafe.Pointer(utf("MHXStudioNextWindow"))), uintptr(unsafe.Pointer(utf(fmt.Sprintf("实例 %d 标定 · %s", c.index, c.name.label)))), 0x00CA0000|wsVisible, 100, 50, 1015, 750, c.app.hwnd, 0, inst, 0)
	if h == 0 {
		c.app.cal = nil
		c.app.log("标定窗口创建失败")
		return
	}
	c.hwnd = h
	user32.NewProc("EnableWindow").Call(c.app.hwnd, 0)
	c.label = child(h, "STATIC", "第1框：稳定的页面特征（避开动画、人物和计时数字）。在截图上拖出矩形。", 20, 12, 950, 40, 0, 0)
	child(h, "BUTTON", "清除重画", 20, 640, 130, 34, 0, 301)
	child(h, "BUTTON", "保存这次标定", 165, 640, 165, 34, 0, 302)
	child(h, "BUTTON", "取消", 345, 640, 110, 34, 0, 303)
	c.hint()
	child(h, "STATIC", "仅保存特征灰度数据和按钮范围；已保存规则先备份，不自动修改其他页面。", 475, 644, 490, 40, 0, 0)
}
func (c *calibration) paint() {
	var ps paintStruct
	dc, _, _ := user32.NewProc("BeginPaint").Call(c.hwnd, uintptr(unsafe.Pointer(&ps)))
	defer user32.NewProc("EndPaint").Call(c.hwnd, uintptr(unsafe.Pointer(&ps)))
	bi := bitmapInfo{Size: 40, Width: 960, Height: -540, Planes: 1, BitCount: 32, SizeImage: uint32(len(c.pixels))}
	gdi32.NewProc("StretchDIBits").Call(dc, 20, 70, 960, 540, 0, 0, 960, 540, uintptr(unsafe.Pointer(&c.pixels[0])), uintptr(unsafe.Pointer(&bi)), 0, 0x00CC0020)
	pen, _, _ := gdi32.NewProc("CreatePen").Call(0, 2, 0x0000ff)
	oldPen, _, _ := gdi32.NewProc("SelectObject").Call(dc, pen)
	brush, _, _ := gdi32.NewProc("GetStockObject").Call(5)
	oldBrush, _, _ := gdi32.NewProc("SelectObject").Call(dc, brush)
	for _, r := range c.rects {
		gdi32.NewProc("Rectangle").Call(dc, uintptr(r.X+20), uintptr(r.Y+70), uintptr(r.X+r.W+20), uintptr(r.Y+r.H+70))
	}
	gdi32.NewProc("SelectObject").Call(dc, oldBrush)
	gdi32.NewProc("SelectObject").Call(dc, oldPen)
	gdi32.NewProc("DeleteObject").Call(pen)
}
func (c *calibration) hint() {
	label := fmt.Sprintf("第%d框：稳定特征（共%d处，避开动画、人物、背景和变化数字）。", len(c.rects)+1, c.featureCount)
	if len(c.rects) == c.featureCount {
		label = "最后一框：按钮可点击范围（" + c.name.button + "）。"
	}
	if len(c.rects) == c.featureCount+1 {
		label = "框选已完成，点击保存后立即重读自检。"
	}
	setText(c.label, label)
}
func (c *calibration) save() {
	if len(c.rects) != c.featureCount+1 {
		c.app.info("请依次框选全部稳定特征和最后的按钮范围。")
		return
	}
	anchors := []automation.Anchor{}
	for _, r := range c.rects[:c.featureCount] {
		a, err := automation.CaptureAnchor(c.img, r)
		if err != nil {
			c.app.info(err.Error())
			return
		}
		anchors = append(anchors, a)
	}
	old := c.profile.Pages[c.name.key]
	threshold := old.Threshold
	if threshold == 0 {
		threshold = 10
	}
	revision := old.Revision + 1
	if revision < 1 {
		revision = 1
	}
	page := automation.Page{ButtonID: c.name.button, CalibrationImageSHA: automation.FrameSHA(c.img), Label: c.name.label, Revision: revision, Anchors: anchors, Threshold: threshold}
	if len(old.FeatureThresholds) == len(anchors) {
		page.FeatureThresholds = old.FeatureThresholds
	}
	c.profile.Pages[c.name.key] = page
	c.profile.Buttons[c.name.button] = c.rects[c.featureCount]
	var e error
	// An identical role identity calibration would allow the wrong character.
	if e = c.profile.Require(c.img, c.name.key); e != nil {
		c.app.info("标定有冲突：" + e.Error() + "。请使用独立页面特征或该角色行选中标记。")
		return
	}
	if e = saveRuleLibrary(c.app.root, c.index, c.profile, c.shared); e != nil {
		c.app.info(e.Error())
		return
	}
	// Invalidate the previous proof as soon as the calibration changes, even if
	// the subsequent disk-read self-check fails.
	if c.shared {
		c.app.cfg.SingleValidations = map[string]proof{}
	} else {
		delete(c.app.cfg.SingleValidations, strconv.Itoa(c.index))
	}
	if e = c.app.cfg.Save(c.app.root); e != nil {
		c.app.log("单机验证记录保存失败：" + e.Error())
	}
	reloaded, loadErr := loadLocalProfile(c.app.root, c.index)
	if c.shared {
		reloaded, loadErr = readProfile(sharedProfilePath(c.app.root))
	}
	if loadErr != nil || !reloaded.Match(c.img, c.name.key) {
		c.app.log(fmt.Sprintf("标定已写入但重读自检未通过：%v；原文件与历史保留。", loadErr))
		return
	}
	for i, score := range reloaded.Score(c.img, c.name.key).Anchors {
		c.app.log(fmt.Sprintf("保存后重读自检（本次标定截图）：%s特征%d误差%.3f/阈值%.3f，超差像素%d/上限%d；通过=%t", c.name.key, i+1, score.MeanAbsoluteError, score.MeanLimit, score.ChangedPixels, score.ChangedPixelLimit, score.Passed))
	}
	c.app.log(fmt.Sprintf("实例 %d 已保存 %s；原标定已留存备份，需重新通过单机测试。", c.index, c.name.label))
	destroyWindow.Call(c.hwnd)
}
func (c *calibration) proc(h uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case 0xF:
		c.paint()
		return 0
	case 0x201:
		if len(c.rects) >= c.featureCount+1 {
			return 0
		}
		x, y := int32(int16(l&0xffff))-20, int32(int16((l>>16)&0xffff))-70
		if x >= 0 && x < 960 && y >= 0 && y < 540 {
			c.start = point{x, y}
			c.dragging = true
			user32.NewProc("SetCapture").Call(h)
		}
		return 0
	case 0x202:
		if !c.dragging {
			return 0
		}
		c.dragging = false
		user32.NewProc("ReleaseCapture").Call()
		x, y := int(int16(l&0xffff))-20, int(int16((l>>16)&0xffff))-70
		if x < 0 {
			x = 0
		}
		if x > 959 {
			x = 959
		}
		if y < 0 {
			y = 0
		}
		if y > 539 {
			y = 539
		}
		sx, sy := int(c.start.X), int(c.start.Y)
		if x < sx {
			x, sx = sx, x
		}
		if y < sy {
			y, sy = sy, y
		}
		r := automation.Rect{X: sx, Y: sy, W: x - sx + 1, H: y - sy + 1}
		if r.Valid() {
			c.rects = append(c.rects, r)
			c.hint()
			user32.NewProc("InvalidateRect").Call(h, 0, 1)
		} else {
			c.app.info("框选范围至少 6×6 像素。")
		}
		return 0
	case wmCommand:
		switch int(w & 0xffff) {
		case 301:
			c.rects = nil
			c.hint()
			user32.NewProc("InvalidateRect").Call(h, 0, 1)
		case 302:
			c.save()
		case 303:
			destroyWindow.Call(h)
		}
		return 0
	case wmClose:
		destroyWindow.Call(h)
		return 0
	case wmDestroy:
		c.app.cal = nil
		user32.NewProc("EnableWindow").Call(c.app.hwnd, 1)
		return 0
	}
	r, _, _ := defWindow.Call(h, uintptr(m), w, l)
	return r
}

type openFileName struct {
	Size                         uint32
	Owner, Instance              uintptr
	Filter, CustomFilter         *uint16
	MaxCustomFilter, FilterIndex uint32
	File                         *uint16
	MaxFile                      uint32
	FileTitle                    *uint16
	MaxFileTitle                 uint32
	InitialDir, Title            *uint16
	Flags                        uint32
	FileOffset, FileExtension    uint16
	DefaultExt                   *uint16
	CustomData                   uintptr
	Hook                         uintptr
	TemplateName                 *uint16
	Reserved                     uintptr
	Reserved2, FlagsEx           uint32
}

func chooseConsole(owner uintptr) string {
	file := make([]uint16, 32768)
	filter := append([]uint16{}, []uint16{'L', 'D', 'P', 'l', 'a', 'y', 'e', 'r', ' ', 'c', 'o', 'n', 's', 'o', 'l', 'e', 0}...)
	for _, s := range []string{"ldconsole.exe;dnconsole.exe", "EXE", "*.exe"} {
		w, _ := syscall.UTF16FromString(s)
		filter = append(filter, w...)
	}
	filter = append(filter, 0)
	of := openFileName{Owner: owner, File: &file[0], MaxFile: uint32(len(file)), Filter: &filter[0], Title: utf("选择雷电目录内 ldconsole.exe 或 dnconsole.exe"), Flags: 0x80000 | 0x1000 | 0x800}
	of.Size = uint32(unsafe.Sizeof(of))
	r, _, _ := syscall.NewLazyDLL("comdlg32.dll").NewProc("GetOpenFileNameW").Call(uintptr(unsafe.Pointer(&of)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(file)
}
