// 全局快捷键：应用不在前台时也要能响应。
//
// # 为什么要分两列、为什么两列默认差一个 ⌥
//
// 设置页上是两列：应用内快捷键（本文件的同伴 shortcut_dispatch.go 走的那条）和
// 全局快捷键（这一份）。Biu 也是这么分的（`shared/settings/shortcut-settings.ts`）：
// 全局那六条比应用内的**多一个 ⌥**。不差这一个的话，在别的应用里按 ⌘P 会
// 抢走那个应用自己的 ⌘P。
//
// # 登记用的是存下来的那一串，一个字都不翻译
//
// `mygo.GlobalShortcut.Register` 自己会用 `mygo/internal/accelerator` 解析
// （`modules.go:551`），而那个解析器认得 `CommandOrControl`、`Alt`、`Shift`、
// 方向键、空格、F 键和单个字符——也就是说**我们存给 Biu 的那一串可以原样喂给它**。
// 自己再翻一遍的话，两套词表迟早会走岔，而症状是「设置页显示 ⌘⇧P，别的应用里
// 按 ⌘⇧P 什么也没有」。
//
// # 必须在应用 ready 之后登记
//
// `Register` 头上有 `needsApp`（`mygo/app.go` 的那条守卫），应用没跑起来时调它会
// 直接 panic。所以同步登记的时机只有两处：`Run` 的 `WhenReady` 和设置页改动之后，
// 两处都已经在事件循环里。
//
// # 正在录的时候一个都不登记
//
// 这一条和应用内那一份（`shortcut_dispatch.go` 的 viewShortcuts）是**同一件事的
// 两半**：用户点中一个录制格、准备按下一个新键时，两个机制都得让开，否则这个键压根
// 录不进去（详见 syncGlobalShortcuts 里那段）。
//
// # 注册不上要降级，不能瘫
//
// 全局快捷键是**加分项**：别的应用先占了那个键、Wayland 上没有桌面 portal、
// Linux 上没有 X11 会话——这些都会让 `Register` 返回错误。这时候应用内那一列
// 照常用（它压根不碰系统登记），而界面上弹一句说清楚是哪几条没登记上。
// 反过来做——因为全局这一列失败就把整个快捷键功能一起关掉——才是这里真正要防的。
//
// # 和 #28 那套（单实例、托盘）不打架
//
// 托盘/单实例管的是「窗口关掉之后进程还在不在」，这一份管的是「系统上登记了哪些
// 键」，两条只在**退进程**那一步相交：`OnWillQuit` 里撤掉登记。而关窗口选「隐藏」
// 的那一次**不撤**——应用还活着、音乐还在放，这时候撤了等于「按 ⌘⌥P 在别的应用里
// 没反应，而本应用的窗口明明还开着」。
package ui

import (
	"log"
	"strings"

	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/egoist/mygo"
)

// globalShortcutService 是「向系统登记一个全局快捷键」的那一半。
//
// 做成接口是为了能替掉它，理由和 `autoStartService`（lifecycle.go）一模一样：
// 真的那一份要走事件循环（框架的 `needsApp`），无头 Tester 里没有事件循环，
// 不替掉就只能等上真系统才验得到「设置改了 → 重新登记」这一条。
type globalShortcutService interface {
	// register 向系统登记一个 accelerator（存下来的那一串原样），按下时调 fn。
	// 注册不上返回错误，调用方**要能继续往下走**。
	register(acc string, fn func()) error
	// unregisterAll 把本应用登记过的全部撤掉。反复调是安全的。
	unregisterAll()
}

// systemGlobalShortcuts 是真的那一份，也就是 mygo.GlobalShortcut。
//
// ⚠️ **它只能在 `Run` 里装上，不能在 `New` 里。** `Register` 头上有 `needsApp`
// （`mygo/app.go:128`）：应用没跑起来时在主线程上调它直接 panic。而 `New` 的调用方
// 除了 `Run` 还有一整套测试（无头 Tester 没有事件循环），任何一次「录一个快捷键」
// 都会走到 `syncGlobalShortcuts`。
type systemGlobalShortcuts struct{}

func (systemGlobalShortcuts) register(acc string, fn func()) error {
	return mygo.GlobalShortcut.Register(acc, fn)
}

func (systemGlobalShortcuts) unregisterAll() { mygo.GlobalShortcut.UnregisterAll() }

// nilGlobalShortcuts 是**什么都不做**的那一份，也是 `New` 默认装上的那一份。
//
// 有了它，「还没装上真的那一份」是安静地什么都不做，而不是 panic——而 panic 的
// 症状是「录一个快捷键整个应用崩掉」，比什么都不做糟得多。真的那一份由 `Run`
// 装上（见上面 systemGlobalShortcuts 那段），而 `Run` 是唯一一个真的会有事件循环
// 的地方。
type nilGlobalShortcuts struct{}

func (nilGlobalShortcuts) register(string, func()) error { return nil }
func (nilGlobalShortcuts) unregisterAll()                {}

// syncGlobalShortcuts 把全局那一列按**当前**的设置重新登记一遍。
//
// **先全撤再全建**，和 Biu 的 `registerAllShortcuts` 同一个形状（`electron/shortcut.ts:8`
// 第一行就是 `globalShortcut.unregisterAll()`）。全建而不是逐条改，是因为「哪几条
// 还在系统里」这件事只有框架自己知道：某一条注册失败之后它压根没在那儿，
// 而我们那份设置文件仍然写着它——下次同步要是只补差集，那一条就永远补不上去了。
//
// **界面线程上**调（`WhenReady` 的回调、设置改完之后）。三条改动出口——录一个键、
// 「恢复默认」、那个总开关——都在**改成功之后**才调它：存不住（只读那份来自更新版
// 的文件）的时候系统上的登记一个字都不该动。漏掉哪一条，用户看到的都是同一个
// 样子——设置页上写着新键，别的应用里按的还是旧的。
func (s *Shell) syncGlobalShortcuts() {
	// 记下「这一份是按什么录制状态登记的」，viewShortcuts 靠它知道要不要再来一次
	// （见 Shell 上那个字段的注释）。
	s.registeredFor = s.recording

	s.hotkeys.unregisterAll()

	cur := s.keys.snapshot()
	if !cur.EnableGlobalShortcuts {
		return
	}
	// ⚠️ **正在录的时候一个都不登记**，理由和文件头那一段是同一件事的一半：
	// 应用内那一份要撤（引擎的 `rt.claimed` 会让录制格收不到键），而这一份撤的理由
	// 更硬——**macOS 的 Carbon 热键压根到不了焦点上的元素**。不撤的话，用户在
	// 「全局快捷键：播放/暂停」那一格里按 ⌘⌥P，跑的是那条命令、不是这一格，
	// 于是那六个默认全局键**在任何一格都录不进去**（真机上量过：⌥⌘N 录制格收得到，
	// ⌥⌘K 只看到全局热键触发、录制格一行都没有）。
	//
	// 录完/退出录制之后 viewShortcuts 会再来一次同步，它们就回来了。
	if s.recording != (shortcutSlot{}) {
		return
	}
	var skipped []string
	for _, it := range cur.GlobalShortcuts {
		acc := strings.TrimSpace(it.Shortcut)
		if acc == "" {
			continue
		}
		// ⚠️ **和应用内那一条是同一个键的，全局这一份不登记。**
		//
		// 冲突只在同一列之内判（设置页明写着「两列互不相干」），所以这一种状态
		// 是允许存在的：应用内 ⌘P、全局也 ⌘P。而两套机制在不同平台上的重叠方式
		// 并不一样——macOS 的 Carbon 热键会吃掉这个组合键、X11 是 root 上抓的
		// 不管焦点在哪都响——两种都会在「窗口在前台」时把它响两次。
		// 「暂停之后立刻又接着播」就是它的样子，而用户完全不知道发生了什么。
		//
		// 不登记那一半是有代价的（这个键在别的应用里不响了），所以要**说给用户
		// 听**，见下面那一句提示。两条默认的键本来差一个 ⌥，所以正常用户看不到它。
		if containsShortcut(cur.Shortcuts, it.Shortcut) {
			log.Printf("biu: 全局快捷键 %q（%s）和应用内那一条是同一个键，全局这一份没登记",
				acc, it.Name)
			skipped = append(skipped, it.Name+"（和应用内那条重了）")
			continue
		}
		if err := s.hotkeys.register(acc, func() { s.onShortcut(it.ID) }); err != nil {
			log.Printf("biu: 全局快捷键 %q（%s）注册不上：%v", acc, it.Name, err)
			skipped = append(skipped, it.Name+"（多半是别的应用先占了）")
		}
	}
	if len(skipped) > 0 {
		// ⚠️ **橙色而不是红色**（票 #180）：应用内那一列照常用，只有全局那一列
		// 几个组合没登记上——是「有一件事没成」，不是「整件事失败了」。
		s.addToast("有几个全局快捷键没登记上："+strings.Join(skipped, "、")+
			"。应用内那一列照常用。", notice.Warning)
	}
}

// containsShortcut 报应用内那一列里有没有**同一个组合键**（写法可以不一样）。
//
// 比的是**解析出来的 (修饰键, 键)**，不是串：同一个键在文件里可以有好几种写法
// （`CommandOrControl+P`、`Cmd+P`、`commandorcontrol+p` 都是同一个，而它们逐字不相等）。
// 只比串的话这两种写法会各自登记一遍，于是**两套机制同时认领同一个组合键**——
// 正好落进这段防护要防的那件事：窗口在前台时它响两次（macOS 的 Carbon 热键吃掉
// 这一下、X11 的 root 抓键不管焦点在哪都响），而用户完全不知道发生了什么。
//
// 拿解析结果比**不会**把 `CommandOrControl+P` 和 `Ctrl+P` 错当成同一个：前者在 mac 上
// 是 `ui.Super`、在别处是 `ui.Ctrl`（`accelModFromName` 给的是 `ui.Cmd`），后者固定是
// `ui.Ctrl`——mac 上两者不同、别处相同，而那正是它们实际是不是同一个键。
func containsShortcut(list []shortcutItem, acc string) bool {
	mods, key, ok := parseAccel(accelerator(acc))
	if !ok {
		return false
	}
	for _, it := range list {
		m, k, ok := parseAccel(accelerator(strings.TrimSpace(it.Shortcut)))
		if ok && m == mods && k == key {
			return true
		}
	}
	return false
}

// uninstallGlobalShortcuts 把本应用登记过的全局快捷键全撤掉。退出流程的最后一步。
//
// 进程退出时系统本来就会放掉这些登记，而 `OnWillQuit` 之后紧接着就是退出——所以
// 这一句真正管的是另一件事：**托盘「退出程序」到进程真死之间的那几帧**。
// 那几帧里别的应用要是抢同一个键抢赢了，用户刚退出去的东西可能立刻又被别的一个
// 进程接住。先撤干净就没有那个窗口。
func (s *Shell) uninstallGlobalShortcuts() {
	s.hotkeys.unregisterAll()
}
