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
	"runtime"

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
