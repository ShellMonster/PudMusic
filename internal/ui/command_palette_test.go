// 命令面板（⌘K）这一组：开没开、搜得到搜不到、↑↓ 和回车跑的是哪一条。
//
// # 这一组量的是哪几件事
//
//	⌘K 真的把面板开出来        它是 commandSpecs 里的一条命令，走的是
//	                          应用内分发那一条路——不验的话，「表里加了
//	                          一条而按下去什么也没发生」又是那种静默 bug
//	搜词会把列表收窄           paletteEntries 的过滤，包括「敲命令 id /
//	                          快捷键也找得到」
//	↑↓ + 回车跑选中的那条      键盘导航是这一层里最像状态机的一段：
//	                          没选中时回车跑第一条，选中之后跑选中的
//	页面类条目真的会换页        去设置 = s.navigate(settingsPath)
//	Esc / 点外面都收得掉       模态浮层的两条退路
package ui

import (
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// TestCmdKOpensThePalette 验 ⌘K 真的把面板开出来。
//
// 它走的是「设置里那条默认键 → 应用内分发 → runShortcut → toggleCommandPalette」
// 整条路：任何一环断了（默认键没登记、命令表里没这条、派发没接）这条都红。
func TestCmdKOpensThePalette(t *testing.T) {
	b := newTestShell(t)
	s, tt := b.s, b.tt
	tt.Frame() // 先画一帧：这一帧才登记上 ⌘K，下一帧才有得命中

	tt.Key(mygoui.Cmd, mygoui.KeyK)
	tt.Frame()

	if !s.paletteOpen {
		t.Fatal("按了 ⌘K 面板没开")
	}
	if !tt.HasText(paletteInputLabel) {
		t.Fatalf("面板开了但输入框没在画面上，文字有 %q", tt.Texts())
	}
	// 命令和页面都在：命令类按名字验（播放/暂停），页面类按「去」前缀验。
	for _, want := range []string{"播放/暂停", "去设置", "去本地音乐"} {
		if !tt.HasText(want) {
			t.Errorf("面板里没有「%s」，文字有 %q", want, tt.Texts())
		}
	}

	// Esc 收得掉（焦点在输入框上，走的是 OnTextKey 那一支）。
	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()
	if s.paletteOpen {
		t.Error("按了 Esc 面板还开着")
	}
}

// TestPaletteEntriesFilterByNameIDAndShortcut 验搜词收窄：敲命令名、命令 id、
// 快捷键都能找到对应的那一条。
func TestPaletteEntriesFilterByNameIDAndShortcut(t *testing.T) {
	if got := len(paletteEntries("")); got != len(commandSpecs)+countMenuRoutes() {
		t.Fatalf("不敲词时是 %d 条，want 命令 %d + 页面 %d", got, len(commandSpecs), countMenuRoutes())
	}
	// 按中文名搜。
	if got := paletteEntries("暂停"); len(got) != 1 || got[0].id != "togglePlay" {
		t.Errorf("搜「暂停」得到 %v，want 只有 togglePlay", got)
	}
	// 按命令 id 搜（大小写、空白都不认）。
	if got := paletteEntries("cycle play mode"); len(got) != 1 || got[0].id != "cyclePlayMode" {
		t.Errorf("搜「cycle play mode」得到 %v，want 只有 cyclePlayMode", got)
	}
	// 按快捷键搜：⌘P → 播放/暂停。
	got := paletteEntries("CommandOrControl+P")
	if len(got) != 1 || got[0].id != "togglePlay" {
		t.Errorf("搜「CommandOrControl+P」得到 %v，want 只有 togglePlay", got)
	}
	// 搜不到的说一句，不是空着。
	if got := paletteEntries("不存在的命令"); len(got) != 0 {
		t.Errorf("搜「不存在的命令」居然还有 %v", got)
	}
}

// countMenuRoutes 是 routes 里 InMenu 的条数——面板里「去…」那几条的数。
func countMenuRoutes() int {
	n := 0
	for _, r := range routes {
		if r.InMenu {
			n++
		}
	}
	return n
}

// TestPaletteArrowsAndEnterRunThePickedCommand 验 ↑↓ + 回车。
//
// 没选中时回车跑**第一条**（Spotlight / VS Code 的口径）；↓ 武装之后跑
// 选中的那一条。
func TestPaletteArrowsAndEnterRunThePickedCommand(t *testing.T) {
	b := newTestShell(t)
	s, tt := b.s, b.tt
	var fired []string
	s.onShortcut = func(id string) { fired = append(fired, id) }
	s.paletteOpen = true
	tt.Frame()

	// 没选中直接回车 → 第一条（commandSpecs 的第一条是 togglePlay）。
	tt.Key(0, mygoui.KeyEnter)
	tt.Frame()
	if len(fired) != 1 || fired[0] != "togglePlay" {
		t.Fatalf("没选中时回车跑的是 %v，want [togglePlay]", fired)
	}
	if s.paletteOpen {
		t.Fatal("跑完一条面板还开着")
	}

	// 再开，↓ 武装到第一条、再 ↓ 到第二条，回车 → prev。
	s.toggleCommandPalette()
	tt.Frame()
	tt.Key(0, mygoui.KeyDown)
	tt.Key(0, mygoui.KeyDown)
	tt.Key(0, mygoui.KeyEnter)
	tt.Frame()
	if len(fired) != 2 || fired[1] != "prev" {
		t.Fatalf("↓ 两次回车跑的是 %v，want 第二次是 prev", fired)
	}
}

// TestPaletteNavEntryGoesToThatPage 验页面类条目：敲「设置」回车换到设置页。
func TestPaletteNavEntryGoesToThatPage(t *testing.T) {
	b := newTestShell(t)
	s, tt := b.s, b.tt
	s.paletteOpen = true
	s.paletteQuery = "去设置"
	tt.Frame()
	tt.Key(0, mygoui.KeyEnter)
	tt.Frame()
	if got := s.router.Path(); got != settingsPath {
		t.Fatalf("回车「去设置」之后停在 %q，want %q", got, settingsPath)
	}
}

// TestPaletteClickingOutsideClosesIt 验点外面收面板。
func TestPaletteClickingOutsideClosesIt(t *testing.T) {
	b := newTestShell(t)
	s, tt := b.s, b.tt
	s.paletteOpen = true
	tt.Frame()

	// 点在蒙版上：面板那一块之外的窗口底（左上角离面板 480 宽的中线很远）。
	tt.ClickAt(10, 10)
	tt.Frame()
	if s.paletteOpen {
		t.Error("点了外面面板还开着")
	}
}

// TestPaletteQueryResetsBetweenSessions 验上一场敲过的词不带进下一场
// （理由见 `toggleCommandPalette` 那段注释）。
func TestPaletteQueryResetsBetweenSessions(t *testing.T) {
	b := newTestShell(t)
	s, tt := b.s, b.tt
	s.paletteOpen = true
	s.paletteQuery = "暂停"
	tt.Frame()
	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()

	s.toggleCommandPalette()
	tt.Frame()
	if s.paletteQuery != "" {
		t.Errorf("重开面板时词是 %q，want 空", s.paletteQuery)
	}
}

// TestPaletteRunsTheNewCommandsThroughTheSameDispatch 验新加的四条命令
// 走的也是 `s.onShortcut`——命令表、派发、面板是同一份实现。
func TestPaletteRunsTheNewCommandsThroughTheSameDispatch(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	var fired []string
	s.onShortcut = func(id string) { fired = append(fired, id) }

	for _, id := range []string{"favoriteCurrent", "cyclePlayMode", "toggleMainWindow", "toggleQueueDrawer"} {
		if _, ok := commandByID(id); !ok {
			t.Fatalf("命令表里没有 %q", id)
		}
		s.runPaletteEntry(paletteEntry{label: id, id: id})
	}
	if strings.Join(fired, ",") != "favoriteCurrent,cyclePlayMode,toggleMainWindow,toggleQueueDrawer" {
		t.Errorf("跑出去的是 %v", fired)
	}
}

// TestNewCommandsAppearInBothShortcutColumns 验新命令在设置页两列都有格可录
// （「全局快捷键」那一列，需求第三条）。
func TestNewCommandsAppearInBothShortcutColumns(t *testing.T) {
	def := defaultShortcutSettings()
	for _, id := range []string{"commandPalette", "favoriteCurrent", "cyclePlayMode", "toggleMainWindow"} {
		if _, ok := findShortcut(id, def.Shortcuts); !ok {
			t.Errorf("应用内那一列没有 %q", id)
		}
		if _, ok := findShortcut(id, def.GlobalShortcuts); !ok {
			t.Errorf("全局那一列没有 %q", id)
		}
	}
	// 队列抽屉只在应用内（理由见 shortcuts.go 的 inAppOnlyShortcuts）。
	if _, ok := findShortcut("toggleQueueDrawer", def.Shortcuts); !ok {
		t.Error("应用内那一列没有 toggleQueueDrawer")
	}
	if _, ok := findShortcut("toggleQueueDrawer", def.GlobalShortcuts); ok {
		t.Error("toggleQueueDrawer 不该有全局那一半")
	}
}

// TestCyclePlayModeRunsThroughRunShortcut 验「切换播放模式」真的调
// `App.CyclePlayMode`——派发那一支接到的是 App 层的方法，不是空转。
func TestCyclePlayModeRunsThroughRunShortcut(t *testing.T) {
	b := newTestShell(t)
	s, a := b.s, b.a
	if got := a.Queue().PlayMode(); got != app.DefaultPlayMode {
		t.Fatalf("开局的播放模式是 %v，want %v", got, app.DefaultPlayMode)
	}
	s.runShortcut("cyclePlayMode")
	if got, want := a.Queue().PlayMode(), app.DefaultPlayMode.Next(); got != want {
		t.Errorf("按了一次之后是 %v，want %v", got, want)
	}
}

// TestToggleQueueDrawerFlipsTheIntentNotThePhase 验队列抽屉那条命令翻转的是
// `drawerOpen`（意图），不是 `drawerPhase`（动画阶段）——理由见派发那里
// 的注释。
func TestToggleQueueDrawerFlipsTheIntentNotThePhase(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.runShortcut("toggleQueueDrawer")
	if !s.drawerOpen {
		t.Error("按了一次 drawerOpen 还是 false")
	}
	s.runShortcut("toggleQueueDrawer")
	if s.drawerOpen {
		t.Error("按了两次 drawerOpen 还是 true")
	}
}
