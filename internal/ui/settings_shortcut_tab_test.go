package ui

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 快捷键那一页
// ─────────────────────────────────────────────────────────────────────────────

// TestRecordingAShortcutStoresIt 验录进去存得住，而且画成符号。
//
// ⚠️ 必须**先点那一格**：录制的这一格靠焦点吃键（`HandleInput`），而焦点是被点
// 出来的。直接 `tt.Key` 不点，键会走全局的语义——症状是「测试里按了没反应」，
// 很容易被误判成「录制功能没做」。
func TestRecordingAShortcutStoresIt(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "快捷键设置")

	clickCell(t, tt, "应用内快捷键：播放/暂停")
	if got := s.recording; got != (shortcutSlot{id: "togglePlay"}) {
		t.Fatalf("点了那一格没有进入录制（recording=%+v）", got)
	}
	tt.Key(mygoui.Super, mygoui.KeyK)
	tt.Frame()

	if got := s.keys.snapshot().Shortcuts[0].Shortcut; got != "CommandOrControl+K" {
		t.Errorf("录成 %q，want CommandOrControl+K", got)
	}
	// 画面上摆的是符号（mac 上 ⌘），不是那一串英文——Biu 也是这么画的。
	if mac := macSymbols(); mac && !tt.HasText("⌘+K") {
		t.Errorf("画出来的不是 ⌘+K，文字有 %q", tt.Texts())
	}
	// 两列互不相干。
	if got := s.keys.snapshot().GlobalShortcuts[0].Shortcut; got != "CommandOrControl+Alt+P" {
		t.Errorf("全局那一列被连坐改成了 %q", got)
	}
}

// TestEscapeWhileRecordingBlursAndRecordsNothing 验 Escape：失焦，且不算录入。
//
// 两半都要验，因为它们是同一个键的两种错法：
//
//   - 算录入了 → 用户的 Cancel 变成了「把 Esc 设成播放/暂停的快捷键」。
//   - 没失焦 → 用户按 Esc 以为退出了，下一次按键又被这一格吃掉录进去。
func TestEscapeWhileRecordingBlursAndRecordsNothing(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "快捷键设置")

	clickCell(t, tt, "应用内快捷键：播放/暂停")
	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()

	if s.recording != (shortcutSlot{}) {
		t.Errorf("按了 Esc 之后还在录 %+v", s.recording)
	}
	if tt.Focused("应用内快捷键：播放/暂停") {
		t.Error("按了 Esc 之后焦点还留在那一格上——下一次按键又会被吃掉")
	}
	if tt.HasText(recordingHint) {
		t.Error("按了 Esc 之后那一格还摆着「按下新的快捷键」")
	}
	// 收起之后那一格照常显示它现在的键（照 Biu：录的时候字也不变）。
	if !tt.HasText(pick(macSymbols(), "⌘+P", "Ctrl+P")) {
		t.Errorf("Esc 之后那一格没有照常显示它现在的键，文字有 %q", tt.Texts())
	}
	if got := s.keys.snapshot().Shortcuts[0].Shortcut; got != "CommandOrControl+P" {
		t.Errorf("按了 Esc 反而把键改了：%q", got)
	}

	// 真的走了：接下来的键不再被吃掉。
	tt.Key(mygoui.Super, mygoui.KeyJ)
	tt.Frame()
	if got := s.keys.snapshot().Shortcuts[0].Shortcut; got != "CommandOrControl+P" {
		t.Errorf("Esc 之后按的键又被录进去了：%q", got)
	}
}

// TestBackspaceAndDeleteClearTheCell 验 Backspace / Delete 清空这一格。
func TestBackspaceAndDeleteClearTheCell(t *testing.T) {
	for _, key := range []mygoui.Key{mygoui.KeyBackspace, mygoui.KeyDelete} {
		tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
		openSettingsTab(t, tt, "快捷键设置")

		clickCell(t, tt, "应用内快捷键：播放/暂停")
		tt.Key(0, key)
		tt.Frame()

		if got := s.keys.snapshot().Shortcuts[0].Shortcut; got != "" {
			t.Errorf("%v 没有清空那一格，还是 %q", key, got)
		}
		// 正在录的那一格画的是提示（照 Biu：没绑键时那一格是空的，正在录才提示）；
		// 退出录制之后才看得出它画的是「未设置」。
		tt.Key(0, mygoui.KeyEscape)
		tt.Frame()
		if !hasExactText(tt, "未设置") {
			t.Errorf("%v 清空之后那一格画的不是「未设置」，文字有 %q", key, tt.Texts())
		}
		s.keys.Close()
	}
}

// TestAKeyThatCannotBeAShortcutIsNotStored 验非法键不存。
//
// 症状最好认的那一条是 Insert：按下去什么都没发生，而那一格**应该**描红一下
// 让用户知道「刚才那一下没录上」。所以这一条两样都验。
func TestAKeyThatCannotBeAShortcutIsNotStored(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "快捷键设置")

	clickCell(t, tt, "应用内快捷键：播放/暂停")
	tt.Key(0, mygoui.KeyInsert)
	tt.Frame()

	if got := s.keys.snapshot().Shortcuts[0].Shortcut; got != "CommandOrControl+P" {
		t.Errorf("录不进的那个键被存进去了：%q", got)
	}
	if s.rejected != (shortcutSlot{id: "togglePlay"}) {
		t.Errorf("录不进去的那一格没有标红，rejected=%+v", s.rejected)
	}

	// 下一个合法的键把红去掉。
	tt.Key(mygoui.Super, mygoui.KeyK)
	tt.Frame()
	if s.rejected != (shortcutSlot{}) {
		t.Errorf("录进了一个合法的键之后红还没消：%+v", s.rejected)
	}
}

// TestRecordingMovesToWhicheverCellWasClicked 验录制的位置跟着点走。
//
// 这一条钉的是一个很难查的 bug：焦点在**建元素的过程中**被按回上一格，于是「点了
// 音量加、开始录的还是播放/暂停」，界面上没有任何报错。
func TestRecordingMovesToWhicheverCellWasClicked(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "快捷键设置")

	clickCell(t, tt, "应用内快捷键：播放/暂停")
	clickCell(t, tt, "应用内快捷键：下一首")
	if got := s.recording; got != (shortcutSlot{id: "next"}) {
		t.Fatalf("点了「下一首」之后在录 %+v", got)
	}
	tt.Key(mygoui.Super, mygoui.KeyN)
	tt.Frame()
	got := s.keys.snapshot()
	checkShortcut(t, "应用内", got.Shortcuts[0], "togglePlay", "播放/暂停", "CommandOrControl+P")
	checkShortcut(t, "应用内", got.Shortcuts[2], "next", "下一首", "CommandOrControl+N")
}

// TestTheGlobalColumnIsRecordedSeparately 验全局那一列也录得进去。
//
// 两列是**两份**设置。全局那列录不进去的话，用户会发现「Biu 里全局能用，这边不能」，
// 而界面上那一格和另一列长得一模一样。
func TestTheGlobalColumnIsRecordedSeparately(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "快捷键设置")

	clickCell(t, tt, "全局快捷键：音量加")
	tt.Key(mygoui.Super|mygoui.Alt, mygoui.KeyI)
	tt.Frame()

	got := s.keys.snapshot()
	checkShortcut(t, "全局", got.GlobalShortcuts[3], "volumeUp", "音量加", "CommandOrControl+Alt+I")
	checkShortcut(t, "应用内", got.Shortcuts[3], "volumeUp", "音量加", "CommandOrControl+Up")
}

// TestTwoCellsWithTheSameKeyAreMarkedAsConflicting 验冲突。
//
// 判据照 Biu：同一列之内比（`shortcut-settings.tsx:23-39`），空键不算冲突。
// 画出来的是红描边 + 一句「与「音量加」冲突」。
func TestTwoCellsWithTheSameKeyAreMarkedAsConflicting(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "快捷键设置")

	if tt.HasText("与「") {
		t.Fatalf("默认那六条里就有冲突，文字有 %q", tt.Texts())
	}

	clickCell(t, tt, "应用内快捷键：上一首")
	tt.Key(mygoui.Super, mygoui.KeyP)
	tt.Frame()

	if got := s.keys.snapshot().Shortcuts[1].Shortcut; got != "CommandOrControl+P" {
		t.Fatalf("没录进去：%q", got)
	}
	if !tt.HasText("与「播放/暂停」冲突") {
		t.Errorf("撞了却没说出来，文字有 %q", tt.Texts())
	}

	// 把它改成不撞的，红字就没了。
	clickCell(t, tt, "应用内快捷键：上一首")
	tt.Key(mygoui.Super, mygoui.KeyL)
	tt.Frame()
	if tt.HasText("与「") {
		t.Errorf("改成不撞的了还留着冲突提示，文字有 %q", tt.Texts())
	}
}

// TestTheShortcutCellSaysItOnlyRecords 验这一页把「这一版只录不生效」说出来了。
//
// 一个按了没反应的快捷键设置页，用户只会以为应用坏了。把没做的那一半说出来，
// 比让人自己撞上去便宜得多。
func TestTheShortcutCellSaysItOnlyRecords(t *testing.T) {
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "快捷键设置")

	if !tt.HasText("这一版先把键录下来存住") {
		t.Errorf("这一页没说清「改了不会立刻生效」，文字有 %q", tt.Texts())
	}
}

// TestAShortcutFileFromANewerVersionIsRefusedOnThePage 验只读时页面上也动不了。
//
// 逐项点是因为只读是整份文件的性质：某一格被挡住了而另一格还能改，用户会以为那
// 一次存住了，下次升级回来才发现丢了一半。
func TestAShortcutFileFromANewerVersionIsRefusedOnThePage(t *testing.T) {
	tt, s, a, _ := shellOnSettings(t, app.DefaultSettings(""))
	// 造一份「来自更新版本」的快捷键文件。
	s.keys = newShortcutKeys(writeShortcutFileFromNewer(t))
	openSettingsTab(t, tt, "快捷键设置")

	if !tt.HasText("快捷键只读") {
		t.Fatalf("这一页没说清为什么改不了，文字有 %q", tt.Texts())
	}
	clickCell(t, tt, "应用内快捷键：播放/暂停")
	tt.Key(mygoui.Super, mygoui.KeyK)
	tt.Frame()

	if got := s.keys.snapshot().Shortcuts[0].Shortcut; got == "CommandOrControl+K" {
		t.Error("只读的时候居然录进去了")
	}
	_ = a
}
