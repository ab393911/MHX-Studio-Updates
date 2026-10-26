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
	for i := 1; i <= 5; i++ {
		out = append(out, calibrationName{fmt.Sprintf("game_%d", i), fmt.Sprintf("游戏主界面：第 %d 个角色昵称 + 菜单按钮", i), "menu"})
	}
	return out
}()

type calibration struct {
	app      *app
	hwnd     uintptr
	index    int
	name     calibrationName
	profile  automation.Profile
	img      image.Image
	pixels   []byte
	rects    []automation.Rect
	start    point
	dragging bool
	label    uintptr
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
	d, e := a.selected()
	if e != nil {
		a.log(e.Error())
		return
	}
	i := comboIndex(a.controls[206])
	if i < 0 || i >= len(calibrationNames) {
		return
	}
	name := calibrationNames[i]
	profile, e := loadProfile(a.root, d.Index)
	if e != nil {
		a.log(e.Error())
		return
	}
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
		a.ui(func() {
			c := &calibration{app: a, index: d.Index, name: name, profile: profile, img: img}
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
	h, _, _ := createWindow.Call(0, ptr("MHXStudioNextWindow"), ptr(fmt.Sprintf("实例 %d 标定 · %s", c.index, c.name.label)), 0x00CA0000|wsVisible, 100, 50, 1015, 750, c.app.hwnd, 0, inst, 0)
	if h == 0 {
		c.app.cal = nil
		c.app.log("标定窗口创建失败")
		return
	}
	c.hwnd = h
	c.label = child(h, "STATIC", "第1框：稳定的页面特征（避开动画、人物和计时数字）。在截图上拖出矩形。", 20, 12, 950, 40, 0, 0)
	child(h, "BUTTON", "清除重画", 20, 640, 130, 34, 0, 301)
	child(h, "BUTTON", "保存这次标定", 165, 640, 165, 34, 0, 302)
	child(h, "BUTTON", "取消", 345, 640, 110, 34, 0, 303)
	child(h, "STATIC", "截图仅在内存中；只保存所框特征的低分辨率灰度数据和按钮范围。", 475, 644, 490, 40, 0, 0)
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
	hints := []string{"第1框：稳定的页面特征（避开动画、人物和计时数字）。在截图上拖出矩形。", "第2框：另一处独立特征。角色标定须包含昵称/选中高亮；不同角色必须有区别。", "第3框：按钮可点击范围（" + c.name.button + "）。框在按钮内部，点击只在中央小范围随机。", "三框已完成。确认页面和角色序号正确后点“保存这次标定”。"}
	setText(c.label, hints[len(c.rects)])
}
func (c *calibration) save() {
	if len(c.rects) != 3 {
		c.app.info("请依次框选两处识别特征和一个按钮范围。")
		return
	}
	a, e := automation.CaptureAnchor(c.img, c.rects[0])
	if e != nil {
		c.app.info(e.Error())
		return
	}
	b, e := automation.CaptureAnchor(c.img, c.rects[1])
	if e != nil {
		c.app.info(e.Error())
		return
	}
	c.profile.Pages[c.name.key] = automation.Page{Anchors: []automation.Anchor{a, b}, Threshold: 10}
	c.profile.Buttons[c.name.button] = c.rects[2]
	// An identical role identity calibration would allow the wrong character.
	if e = c.profile.Require(c.img, c.name.key); e != nil {
		c.app.info("标定有冲突：" + e.Error() + "。请将特征框放在昵称或该角色选中标记上。")
		return
	}
	if e = saveProfile(c.app.root, c.index, c.profile); e != nil {
		c.app.info(e.Error())
		return
	}
	delete(c.app.cfg.SingleValidations, strconv.Itoa(c.index))
	if e = c.app.cfg.Save(c.app.root); e != nil {
		c.app.log("单机验证记录保存失败：" + e.Error())
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
		if len(c.rects) >= 3 {
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
