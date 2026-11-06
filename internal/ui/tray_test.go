// 状态栏图标（macOS 菜单栏图标）那一摊的菜单（票 #717）。
//
// 这一族断的是**菜单的形状**和**它上面那几项点了会做什么**。
//
// ⚠️ **判据是「菜单树里有什么」和「点了之后状态变成什么」**，不是「屏幕上
// 画出了什么」——菜单是系统画的（NSMenu），它不在窗口的画面里。
package ui

import (
	"testing"

	"github.com/egoist/mygo"
)

// TestTheTrayMenuHasThePlayerShape 断状态栏菜单的形状。
//
// ⚠️ **顺序就是那样**：先说「现在在放什么」，然后是播放控制，然后是窗口的
// 那些去处，最后是设置和退出。
func TestTheTrayMenuHasThePlayerShape(t *testing.T) {
	b := newTestShell(t)
	m := b.s.trayMenu()
	var got []string
	for _, it := range m.Items() {
		switch {
		case it.Type == mygo.MenuItemSeparator:
			got = append(got, "---")
		default:
			got = append(got, it.Label)
		}
	}
	want := []string{
		"没有在放", "---",
		"播放/暂停", "上一曲", "下一曲", "---",
		"显示/隐藏主窗口", "迷你播放器", "---",
		"设置", "退出程序",
	}
	if len(got) != len(want) {
		t.Fatalf("状态栏菜单有 %d 项（%v），want %d 项（%v）", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("状态栏菜单第 %d 项是 %q，want %q（整份：%v）", i+1, got[i], want[i], got)
		}
	}
}

// TestTheNowPlayingLineSaysWhatIsPlaying 断最上面那一行说的是「现在在放什么」。
//
// ⚠️ **没有歌的时候要说清楚**：留一条空的那一行，用户看到的是「这个应用什么都
// 没在放」——而那到底是「没歌」还是「菜单坏了」分不清。
func TestTheNowPlayingLineSaysWhatIsPlaying(t *testing.T) {
	b := newTestShell(t)
	s := b.s

	// 没有歌。
	if got := s.trayNowPlayingLabel(s.app.Snapshot()); got != "没有在放" {
		t.Errorf("没有歌的时候那一行是 %q，want %q", got, "没有在放")
	}
}

// TestTheNowPlayingLineIsNotClickable 断那一行**点不动**。
//
// ⚠️ **它只是给人看的**：一个能点的「正在播放：xxx」会让人以为点了能干什么
// （跳到那一首？打开那一页？），而它其实什么都不做。
//
// macOS 上一个禁用项是灰的——正好是「这一行不可点」的样子。
func TestTheNowPlayingLineIsNotClickable(t *testing.T) {
	b := newTestShell(t)
	m := b.s.trayMenu()
	it := m.ItemByID(trayNowPlayingID)
	if it == nil {
		t.Fatal("菜单里没有「正在播放」那一行")
	}
	if it.IsEnabled() {
		t.Error("「正在播放」那一行还是能点的：它只是给人看的，点了什么也不会发生")
	}
	if it.Click != nil {
		t.Error("「正在播放」那一行有 Click：它不该有任何动作")
	}
}

// TestTheTrayCommandsRunTheCommand 断**状态栏菜单也走同一个命令入口**。
//
// ⚠️ **这是这一族最要紧的一条**：菜单项的加速键**会先于窗口拿到那个键**
// （框架自己的 E2E：`mygo/internal/e2e/menu_shortcuts_windows_test.go:14`）。
// 而状态栏菜单和应用菜单**功能重复**（都有播放/上一首/下一首）——
// 重复是故意的（Spotify 就是两处都有），但**动作必须是同一份实现**，否则
// 用户按菜单和按快捷键的结果会不一样。
//
// 这一条断的是「点了状态栏菜单那一项，命令入口被调到了」。
func TestTheTrayCommandsRunTheCommand(t *testing.T) {
	b := newTestShell(t)
	s := b.s

	var fired []string
	s.onShortcut = func(id string) { fired = append(fired, id) }

	m := s.trayMenu()
	for _, id := range []string{"togglePlay", "prev", "next"} {
		it := m.ItemByID(trayCommandIDPrefix + id)
		if it == nil {
			t.Fatalf("状态栏菜单里没有命令 %q 那一项", id)
		}
		it.Click(it, nil)
	}

	want := []string{"togglePlay", "prev", "next"}
	if len(fired) != len(want) {
		t.Fatalf("点了三项，命令入口被调了 %d 次（%v），want %d 次（%v）", len(fired), fired, len(want), want)
	}
	for i := range want {
		if fired[i] != want[i] {
			t.Errorf("第 %d 次是 %q，want %q（整份：%v）", i+1, fired[i], want[i], fired)
		}
	}
}

// TestTheTrayMenuKeepsEveryItemBiuHas 断**上游 Biu 那几项一项不少**。
//
// ⚠️ **这一条是「别改丢」**：上游的托盘菜单是
// `_research/biu/electron/windows/tray.ts` 的那份，而我们这一份是**照它**来的
// （只是加了「正在播放」那一行和几个去处）。少一项就是「菜单项不齐」——
// 用户把窗口收进托盘之后，托盘那几项就是唯一的入口。
//
// ⚠️ **「设置」那一项在 Biu 那边叫「设置」**（不带省略号），我们也是。
//
// ⚠️ **「显示主窗口」那一项被删了**：它和「显示/隐藏主窗口」**在迷你开着
// 的时候指向同一件事**（`openWindow` 在迷你开着时是「把迷你叫到前面」），
// 两个都摆着，用户会以为它们不一样。
//
// ⚠️ **「显示/隐藏界面」这一项的文案和上游不同**：上游写「显示/隐藏界面」
// （`tray.ts`），我们写「显示/隐藏主窗口」——「界面」是 Windows 上的说法，
// macOS 上说「窗口」。而**上游 Biu 在 macOS 上压根不建这个图标**
// （`_research/biu/electron/main.ts:161` 是 `if (process.platform !== "darwin")`），
// 所以这一栏在 macOS 上**没有上游可对齐**，得按 macOS 的词汇来。
func TestTheTrayMenuKeepsEveryItemBiuHas(t *testing.T) {
	b := newTestShell(t)
	m := b.s.trayMenu()
	labels := map[string]bool{}
	for _, it := range m.Items() {
		labels[it.Label] = true
	}
	for _, want := range []string{"播放/暂停", "上一曲", "下一曲", "显示/隐藏主窗口", "设置", "退出程序"} {
		if !labels[want] {
			t.Errorf("状态栏菜单里没有 %q 这一项：上游 Biu 有（`tray.ts`）", want)
		}
	}
}

// TestTheTrayQuitItemQuitsTheApp 断「退出程序」那一项点了走 `App.Quit()`。
//
// ⚠️ **为什么单独断这一条**：Linux 上窗口没有菜单栏（`installMenu` 只装
// macOS），托盘右键菜单的「退出程序」是**唯一**的图形化退出入口——而
// AppIndicator 的菜单由面板进程画，Xvfb 下 automation 点不到它（见
// `internal/e2e/linux_lifecycle_test.go` 的文件头）。能验的就是「菜单里
// 有这一项、它的 Click 真的落在 `App.Quit()`」——后者走 OnBeforeQuit →
// 关窗口 → OnWillQuit 存设置那一整条，绕过它设置就丢。
//
// ⚠️ **不真调 `mygo.App.Quit()`**：测试进程没有事件循环，真调会把整个
// 测试 binary 拉进退出流程。所以退出入口走包级变量 `quitFunc`，这一档
// 把它换成假的、断言「Click 调到它了」——真机上它是 `mygo.App.Quit`。
func TestTheTrayQuitItemQuitsTheApp(t *testing.T) {
	b := newTestShell(t)
	m := b.s.trayMenu()
	it := m.ItemByID(trayQuitID)
	if it == nil {
		t.Fatal("状态栏菜单里没有「退出程序」那一项")
	}
	if !it.IsEnabled() {
		t.Error("「退出程序」那一项是灰的：它得能点")
	}
	if it.Click == nil {
		t.Fatal("「退出程序」那一项没有 Click：点了什么也不会发生")
	}

	called := 0
	old := quitFunc
	quitFunc = func() { called++ }
	defer func() { quitFunc = old }()

	it.Click(it, nil)
	if called != 1 {
		t.Errorf("点「退出程序」调了 %d 次退出入口，want 1 次（它该落在 App.Quit 上）", called)
	}
}
