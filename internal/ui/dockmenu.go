// macOS Dock 图标的右键菜单，和 Windows 任务栏跳转列表（JumpList）。
//
// # 为什么有这一份
//
// 「Dock 栏 icon 右键」在两个平台上是两套机制：
//
//	macOS   → `App.Dock.SetMenu`，菜单叠在系统标准项（选项/隐藏/退出）上方
//	Windows → `App.JumpList.SetTasks`，「Tasks」那一栏是 IShellLink，只能起进程
//
// 它们**形状不一样、能力也不一样**，但**做的事是同一件**——「不打开窗口也
// 能控制播放」——所以放在同一个文件里对照着看。
//
// # 这一份菜单**只放动作**
//
// 不放「正在播放」那一行——那是托盘菜单存在的理由（`tray.go`），而 Dock
// 菜单没有「灰字信息行」的位置。用户右键 Dock 想的是「做点事」，不是「看点
// 东西」。
//
// 也不放「显示主窗口」「退出」——系统会自动在菜单下方追加那几项，重复加
// 一遍就是两行一样的字。
//
// # 每一项都走 `s.onShortcut`
//
// 理由同 `commands.go` 文件头那段：菜单项的 `Click` 必须和快捷键是**同一份
// 实现**——Dock 菜单也是菜单，它吃掉的那个键不会再传到窗口。自己写一份
// 「调 `s.app.Toggle()`」就是第三份实现，改一处忘一处。
package ui

import (
	"log"
	"runtime"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo"
)

// installDockUI 装「Dock 右键能控制播放」这一半：
//
//   - macOS：给 `App.Dock` 一份三项菜单（播放/暂停、上一首、下一首），
//     右键 Dock icon 就能看见；
//   - Windows：给 `App.JumpList` 一份三个 task（`pudmusic://cmd/toggle-play` 等），
//     右键任务栏图标就能看见。
//
// 失败只记日志不阻拦启动：Dock 菜单是「不打开窗口也能控制」的入口，但它
// 不是「能不能起来」——跟 `installTray` 是同一个取舍。
//
// **必须在 `mygo.App.WhenReady` 之后调**（`App.Dock.SetMenu` 要 `needsApp`，
// `App.JumpList.SetTasks` 同理）。
func (s *Shell) installDockUI() {
	if runtime.GOOS == "darwin" {
		s.installDockMenu()
		return
	}
	if runtime.GOOS == "windows" {
		s.installJumpList()
	}
}

// dockMenuIDPrefix 是 Dock 菜单项 id 的前缀（跟托盘菜单的 `trayCommandIDPrefix`
// 错开——`Menu.ItemByID` 是按整棵树找的，撞了 id 会改错那一项）。
const dockMenuIDPrefix = "dock.cmd."

// installDockMenu 给 macOS 的 Dock icon 装一份三项菜单。
//
// ⚠️ **这一份菜单只建一次**——`App.Dock.SetMenu` 是「换掉整份菜单」
// （`mygo/app.go:569`），运行期不能往里塞东西也不能往外拿。所以「能不能点」
// 由 `refreshDockMenu` 跟着 `HasTrack` 刷 `SetEnabled` 来改，**不重建**。
func (s *Shell) installDockMenu() {
	menu := s.buildDockMenu()
	mygo.App.Dock.SetMenu(menu)
	// 留着引用给 `refreshDockMenu` 用：菜单只建一次，可用态要运行期刷。
	s.dockMenuRef = menu
}

// buildDockMenu 建 Dock 菜单那一份内容。**不是装**，装是 `installDockMenu`
// 那一步把它递给 `App.Dock.SetMenu`；拆开是方便测试直接拿着它查 id。
func (s *Shell) buildDockMenu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{ID: dockMenuIDPrefix + "togglePlay", Label: commandName("togglePlay"),
			Click: func(*mygo.MenuItem, *mygo.Window) { s.onShortcut("togglePlay") }},
		{ID: dockMenuIDPrefix + "prev", Label: commandName("prev"),
			Click: func(*mygo.MenuItem, *mygo.Window) { s.onShortcut("prev") }},
		{ID: dockMenuIDPrefix + "next", Label: commandName("next"),
			Click: func(*mygo.MenuItem, *mygo.Window) { s.onShortcut("next") }},
	})
}

// installJumpList 给 Windows 的任务栏跳转列表装三个 task。
//
// ⚠️ **Task 只能起进程 + 传 URL**（`IShellLink` 的限制，见 `mygo/jumplist.go`），
// 不能挂回调。所以每个 task 的 Args 都是一条 `pudmusic://cmd/…` 的 URL——
// 单实例锁会把它转给已经跑着的那个实例，那边的 `OnOpenURL` 走
// `CommandFromURL` 翻成命令 id 再 `s.onShortcut`。
//
// ⚠️ **只设一次**：Windows 把 JumpList 持久化到用户配置里（`mygo/jumplist.go`
// 的官方建议「apps usually set it once at startup」），**不刷**——任务栏
// 右键里那三项永远一样，这跟「托盘菜单只建一次」是两种思路：托盘是「我们
// 自己的 NSMenu，能改 label」；JumpList 是「Windows 管的清单，改一处就是
// 换一整份」。
func (s *Shell) installJumpList() {
	tasks := []mygo.JumpListTask{
		{Title: commandName("togglePlay"), Args: []string{"pudmusic://cmd/toggle-play"}},
		{Title: commandName("prev"), Args: []string{"pudmusic://cmd/prev"}},
		{Title: commandName("next"), Args: []string{"pudmusic://cmd/next"}},
	}
	// 失败只记日志：JumpList 设不上只是「右键看不到这三项」，不影响应用起来。
	if err := mygo.App.JumpList.SetTasks(tasks); err != nil {
		log.Printf("pudmusic: 装不上任务栏跳转列表，右键任务栏图标看不到播放控制：%v", err)
	}
}

// refreshDockMenu 刷 Dock 菜单上那三项的可用态。
//
// ⚠️ **比一遍再改**：`SetEnabled` 每次都要往主线程投一个任务（同
// `refreshTrayMenu` 的理由），而刷新是每帧调一次的——不比一遍就是每帧三次
// 无谓的跨线程往返。
//
// ⚠️ **「从来没刷过」要单独认**：`dockHasTrack` 的零值是 false，跟「没歌」
// 是同一个值——第一次进来时如果不区分「是 false 因为没刷过」和「是 false
// 因为没歌」，那一趟就会原样 return，菜单上的三项**永远停在初始的可用态**
// （默认是亮的）——`TestTheDockPlaybackItemsAreDisabledWithoutATrack` 就是
// 来抓这个的。所以 `dockState` 是个三态：没刷过 / 没歌 / 有歌。
func (s *Shell) refreshDockMenu(st app.State) {
	if s.dockMenuRef == nil {
		return
	}
	cur := dockStateNoTrack
	if st.HasTrack {
		cur = dockStateHasTrack
	}
	if s.dockState == cur {
		return
	}
	s.dockState = cur
	for _, id := range []string{"togglePlay", "prev", "next"} {
		if it := s.dockMenuRef.ItemByID(dockMenuIDPrefix + id); it != nil {
			it.SetEnabled(st.HasTrack)
		}
	}
}

// dockState 是 Dock 菜单上「三项能不能点」在**上一次刷新时**的样子。
// 三态：没刷过（默认）/ 没歌 / 有歌——理由见 `refreshDockMenu` 顶上那段。
type dockState int

const (
	dockStateNeverRefreshed dockState = iota
	dockStateNoTrack
	dockStateHasTrack
)
