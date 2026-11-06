//go:build e2e

package ui

// E2E 钩子：让真进程里的测试驱动**托盘菜单那一条退出路径**。
//
// # 为什么要有它
//
// Linux 上窗口没有菜单栏（`installMenu` 只装 macOS），托盘 AppIndicator
// 菜单里的「退出程序」是**唯一**的图形化退出入口——但那份菜单是**面板
// 进程**画的（StatusNotifierItem 协议），Xvfb 下没有真面板，automation
// 的 `menu` 方法只走窗口的 `w.menubar`（`linux.TestActivateMenuItem`），
// 够不到托盘。
//
// ⇒ 这里留一条**和面板等效**的缝：E2E 进程内按菜单项 id 直接调那一项的
// `Click`——和用户点开托盘菜单点「退出程序」走的是**同一个 Click、同一个
// `quitFunc`、同一条 `App.Quit()`**。验不到的是「面板画出了菜单」那一半
// （它在发行说明里写明不覆盖）；验到的是「这项存在、点了真退」。
//
// ⚠️ **只在 `e2e` 标签下编进来**：正常构建里没有这条路，`E2EShell` 也不
// 会被登记——这条缝只对带 `-tags e2e` 编出来的测试二进制存在。

import (
	"sync"

	"github.com/egoist/mygo"
)

// e2eShell 是 `Run` 起来的那一份外壳，`e2eRegisterShell` 记下它。
var (
	e2eShellMu sync.Mutex
	e2eShell   *Shell
)

// e2eRegisterShell 把主窗口那份外壳挂上钩子。在 `Run` 里 `New(a)` 之后调。
func e2eRegisterShell(s *Shell) {
	e2eShellMu.Lock()
	e2eShell = s
	e2eShellMu.Unlock()
}

// E2EActivateTrayItem 在主线程上点状态栏菜单里 id 那一项，报菜单或那一项
// 在不在。
//
// ⚠️ **走 `mygo.RunOnMain`**：菜单项的 Click（包括 `quitFunc()` 那条
// `App.Quit()`）必须和事件循环同线程——和真面板上点一下是同一个执行环境。
// 菜单还没建出来时（托盘建失败、或还没到 `installTray`）报 false，让
// 调用方自己去等/去失败。
func E2EActivateTrayItem(id string) bool {
	e2eShellMu.Lock()
	s := e2eShell
	e2eShellMu.Unlock()
	if s == nil {
		return false
	}
	var ok bool
	mygo.RunOnMain(func() {
		m := s.trayMenuRef
		if m == nil {
			return
		}
		it := m.ItemByID(id)
		if it == nil || it.Click == nil {
			return
		}
		ok = true
		it.Click(it, nil)
	})
	return ok
}

// E2ETrayItemEnabled 报状态栏菜单里 id 那一项在不在、能不能点——让 E2E
// 在「激活之前」先断「菜单里真有这一项」，把「没这项」和「点了没退」分开。
func E2ETrayItemEnabled(id string) (present, enabled bool) {
	e2eShellMu.Lock()
	s := e2eShell
	e2eShellMu.Unlock()
	if s == nil {
		return false, false
	}
	mygo.RunOnMain(func() {
		m := s.trayMenuRef
		if m == nil {
			return
		}
		if it := m.ItemByID(id); it != nil {
			present, enabled = true, !it.Disabled
		}
	})
	return present, enabled
}

// ---- 第二实例 handoff 探针 ----
//
// 「第二个实例转交文件」那条路（`platform.Lock` → framework socket →
// `acceptSecondInstances` → `OnSecondInstance` → `handleLaunchArgs` →
// `deliverArgs`）曾经红过一整族 E2E 而**第一实例一个字都没说**——它是
// 静默断的，断点在第二进程、framework 锁、还是我们这一半，界面上分不出来。
// 这里留一条只给 e2e 的缝：`handleLaunchArgs` 每被调到一次，就把收到的
// args 记下来；`handoff.args` 那条 tray-hook 命令把它读出去——测试就能断
// 「命令行真的进了第一实例的回调」。
var (
	e2eHandoffMu   sync.Mutex
	e2eHandoffArgs []string
)

// e2eNoteLaunchArgs 把一次 `handleLaunchArgs` 的调用记下来。生产代码里
// 是 `handleLaunchArgs` 入口的第一行（那条路是「系统交给本应用的命令行」
// 的最后一站）。
func e2eNoteLaunchArgs(args []string) {
	e2eHandoffMu.Lock()
	e2eHandoffArgs = append([]string(nil), args...)
	e2eHandoffMu.Unlock()
}

// E2ELaunchArgs 报最近一次 `handleLaunchArgs` 收到的 args（副本）。
func E2ELaunchArgs() []string {
	e2eHandoffMu.Lock()
	defer e2eHandoffMu.Unlock()
	return append([]string(nil), e2eHandoffArgs...)
}
