// 通知区域的托盘图标。
//
// ## macOS 上没有托盘
//
// `TraySupported` 在 darwin 上返回 false，Biu 也是这么做的：`biu/electron/main.ts`
// 那一行 `if (process.platform !== "darwin") createTray(...)` 把整段托盘包在了
// 非 darwin 里。macOS 上收进后台是「窗口不见了，Dock 图标还在」，点一下图标
// 就回来——不是托盘。
//
// Windows / Linux 上有托盘，「隐藏到托盘」这一档才名副其实：窗口收起来之后，
// 用户还有一个能点回来的入口。Biu 的托盘菜单也是这几个项（`electron/windows/
// tray.ts`：播放/暂停、上一曲、下一曲、显示/隐藏界面、设置、退出程序）。
package platform

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"runtime"
	"sync"

	"github.com/egoist/mygo"
)

// TraySupported 报这个平台该不该有托盘。macOS 上永远没有（见上面的说明）。
func TraySupported() bool { return runtime.GOOS != "darwin" }

// NewTray 建一个托盘图标，带上这个图标和菜单。
//
// **必须在 mygo.App.WhenReady 之后调**：框架建托盘要过事件循环，没 ready 就调
// 是直接 panic 的（`mygo/modules.go` 的 NewTray）。
func NewTray(icon []byte, menu *mygo.Menu) (*mygo.Tray, error) {
	return mygo.NewTray(mygo.TrayOptions{Icon: icon, ToolTip: "Biu MyGo", Menu: menu})
}

// 托盘图标。
//
// 现画一张，不带二进制资源进来：Biu 的托盘图标是 `tray.ico`（Windows 原生
// 格式），而框架的 `TrayOptions.Icon` 要的是 **PNG**，两者对不上；与其为了这
// 一个图标去引一个转换依赖，不如照着 Biu 的主色画一张 32×32 的 PNG（那是打包
// 时会替换掉的占位图）。
var (
	trayIconOnce sync.Once
	trayIconPNG  []byte
)

// TrayIcon 是托盘那个 PNG，32×32（Retina 下正好是菜单栏 16pt）。
//
// 图案：主色（`app.DefaultPrimaryColor`，Biu 的 Spotify 绿）铺满，中间一个白色
// 的播放三角。画在一张 PNG 上而不是塞进 resources 目录：托盘图标要求的是
// **模板图**（macOS）／小尺寸（Windows），走资源那一套反而要处理两种格式。
func TrayIcon() []byte {
	trayIconOnce.Do(func() {
		const n = 32
		img := image.NewRGBA(image.Rect(0, 0, n, n))
		green := color.RGBA{0x1e, 0xd7, 0x60, 0xff}
		white := color.RGBA{0xff, 0xff, 0xff, 0xff}

		// 圆角方块。
		r := 6
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				if inRoundedRect(x, y, n, r) {
					img.Set(x, y, green)
				}
			}
		}
		// 播放三角：顶点 (12,8) (12,24) (23,16)。
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				if inTriangle(x, y, 12, 8, 12, 24, 23, 16) {
					img.Set(x, y, white)
				}
			}
		}

		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return
		}
		trayIconPNG = buf.Bytes()
	})
	return trayIconPNG
}

// inRoundedRect 报 (x,y) 在不在一个 n×n 的圆角方块里（角半径 r）。
func inRoundedRect(x, y, n, r int) bool {
	cx := min(max(x, r-1), n-r)
	cy := min(max(y, r-1), n-r)
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

// inTriangle 报点落在不在这三个点围成的三角形里（标准叉积判据）。
func inTriangle(px, py, x1, y1, x2, y2, x3, y3 int) bool {
	d := func(ax, ay, bx, by int) int { return (px-bx)*(ay-by) - (ax-bx)*(py-by) }
	a, b, c := d(x1, y1, x2, y2), d(x2, y2, x3, y3), d(x3, y3, x1, y1)
	neg := a < 0 || b < 0 || c < 0
	pos := a > 0 || b > 0 || c > 0
	return !(neg && pos)
}
