// 通知区域的托盘图标。
//
// ## 三个平台都有
//
// macOS 是菜单栏图标（NSStatusItem，点一下弹菜单，图标用黑色透明的模板图，系统
// 按菜单栏的明暗自己着色）；Windows / Linux 是通知区域的托盘，左键点图标显示/隐藏。
// 菜单项照 Biu 的 `electron/windows/tray.ts`：播放/暂停、上一曲、下一曲、
// 显示/隐藏界面、设置、退出程序。
package platform

import (
	"runtime"

	"github.com/egoist/mygo"
)

// TraySupported 报这个平台该不该有托盘 / 菜单栏图标。三个平台都有。
func TraySupported() bool { return true }

// TrayIcon 给这个平台该用的那张图，以及它是不是模板图。macOS 用黑色透明的音符
// 模板图（`TrayTemplatePNG`），其余平台用彩色的应用图标。
func TrayIcon() (png []byte, template bool) {
	if runtime.GOOS == "darwin" {
		return TrayTemplatePNG(), true
	}
	return TrayIconPNG(), false
}

// NewTray 建一个托盘图标，带上这个图标和菜单。
//
// **必须在 mygo.App.WhenReady 之后调**：框架建托盘要过事件循环，没 ready 就调
// 是直接 panic 的（`mygo/modules.go` 的 NewTray）。
func NewTray(icon []byte, template bool, menu *mygo.Menu) (*mygo.Tray, error) {
	return mygo.NewTray(mygo.TrayOptions{ID: "biugo", Icon: icon, IconIsTemplate: template, ToolTip: "BiuGo", Menu: menu})
}
