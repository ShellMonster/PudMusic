package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// accelerator 的写法（录进去存下来 / 存下来显示出来）这一组。
//
// 两条规矩最容易错，而且错得很安静：存出去的串 Biu 读不懂（用户切过去发现
// 快捷键全没了），显示出来的符号不对（用户以为按错了键）。

func keyEvent(mods ui.Modifiers, key ui.Key) ui.InputEvent {
	return ui.InputEvent{Kind: ui.InputKeyDown, Key: key, Mods: mods}
}

// TestAKeyBecomesTheAcceleratorBiuExpects 验录进去的串。
//
// 用**修饰键那一侧**的取值而不是 `ui.Cmd`：Biu 那边把 Ctrl 和 Command 合成一个词
// （CommandOrControl），而 `ui.Cmd` 在 mac 上是 Super、在别处是 Ctrl——
// 只测 `ui.Cmd` 的话 mac 上就会误以为该写 Super。
func TestAKeyBecomesTheAcceleratorBiuExpects(t *testing.T) {
	cases := []struct {
		name string
		mods ui.Modifiers
		key  ui.Key
		want accelerator
	}{
		{"光按一个字母没有修饰键", 0, ui.KeyK, "K"},
		{"Command", ui.Super, ui.KeyK, "CommandOrControl+K"},
		{"Ctrl", ui.Ctrl, ui.KeyK, "CommandOrControl+K"},
		{"Command+Alt", ui.Super | ui.Alt, ui.KeyK, "CommandOrControl+Alt+K"},
		{"顺序固定：Command、Alt、Shift", ui.Shift | ui.Alt | ui.Super, ui.KeyK,
			"CommandOrControl+Alt+Shift+K"},
		{"空格", ui.Super, ui.KeySpace, "CommandOrControl+Space"},
		{"方向键", ui.Super, ui.KeyLeft, "CommandOrControl+Left"},
		{"F 键", 0, ui.KeyF5, "F5"},
		{"数字", ui.Super, ui.Key7, "CommandOrControl+7"},
		{"标点", ui.Super, ui.KeySlash, "CommandOrControl+/"},
		{"没有修饰键的方向键也认（Mac 的 Ctrl+方向是别的语义，这里不管）", 0, ui.KeyUp, "Up"},
	}
	for _, c := range cases {
		got, ok := accelFromEvent(keyEvent(c.mods, c.key))
		if !ok {
			t.Errorf("%s：没录进去", c.name)
			continue
		}
		if got != c.want {
			t.Errorf("%s：录成 %q，want %q", c.name, got, c.want)
		}
	}
}

// TestTheKeysThatCannotBeAShortcutAreNotRecorded 验录不进去的那些键。
//
// 少一条记录里的键，症状是「我按了 Insert 它什么都不显示，也不出错」——用户
// 只能一遍遍重按。这一条把不能录的键一条条列出来。
func TestTheKeysThatCannotBeAShortcutAreNotRecorded(t *testing.T) {
	for _, key := range []ui.Key{
		ui.KeyEscape,    // 取消，不算录入
		ui.KeyBackspace, // 清空，不算录入
		ui.KeyDelete,    // 清空，不算录入
		ui.KeyTab,       // 这一格吃掉所有键，Tab 没法用来走开
		ui.KeyHome, ui.KeyEnd, ui.KeyPageUp, ui.KeyPageDown, ui.KeyInsert,
		ui.KeyContextMenu, ui.KeyBack, ui.KeyForward,
		ui.KeyUnknown, // 修饰键本身 / 认不出来的键
	} {
		if recordableKeys[key] {
			t.Errorf("%v 被算成能录的键了", key)
		}
	}
	// 而字母 / 数字 / F 键必须能录——它们是最常用的那批，漏一个就是
	// 「我明明按了 ⌘Q 它没反应」。
	for key := ui.KeyA; key <= ui.KeyZ; key++ {
		if !recordableKeys[key] {
			t.Fatalf("字母 %v 录不进去", key)
		}
	}
	for key := ui.Key0; key <= ui.Key9; key++ {
		if !recordableKeys[key] {
			t.Fatalf("数字 %v 录不进去", key)
		}
	}
	for key := ui.KeyF1; key <= ui.KeyF12; key++ {
		if !recordableKeys[key] {
			t.Fatalf("F 键 %v 录不进去", key)
		}
	}
}

// TestAStrangeLayoutStillRecordsByPosition 验布局认不出的键按位置顶替。
//
// 西里尔布局上的 ы 报 KeyUnknown，它的 accelerator 在 Biu 那边是 `CommandOrControl+
// Y`。照 `ev.Key` 写就存成 `CommandOrControl+` ——一个残缺的串，Biu 那边认不出来，
// 而我们这边看着「有键」。
func TestAStrangeLayoutStillRecordsByPosition(t *testing.T) {
	ev := ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyUnknown, PhysicalKey: ui.KeyY, Mods: ui.Super}
	got, ok := accelFromEvent(ev)
	if !ok {
		t.Fatal("认不出来的键没录进去")
	}
	if got != "CommandOrControl+Y" {
		t.Errorf("录成 %q，want CommandOrControl+Y", got)
	}
	// 位置也拿不到就只能当非法键：宁可不录，也不要存一个残缺的串。
	if _, ok := accelFromEvent(ui.InputEvent{Kind: ui.InputKeyDown, Mods: ui.Super}); ok {
		t.Error("连位置都没有却录进去了")
	}
}

// TestTheSymbolsOnScreenAreTheOnesBiuUses 验画出来的那一串。
//
// mac 上是 ⌘/⌥/⇧，别处是 Ctrl/Alt/Shift——照 Biu 的
// `formatElectronAcceleratorForDisplay`。写错的后果是用户以为自己记错了键。
func TestTheSymbolsOnScreenAreTheOnesBiuUses(t *testing.T) {
	cmd, alt, shift := "Ctrl", "Alt", "Shift"
	if macSymbols() {
		cmd, alt, shift = "⌘", "⌥", "⇧"
	}
	cases := []struct {
		in   accelerator
		want string
	}{
		{"CommandOrControl+P", cmd + "+P"},
		{"CommandOrControl+Alt+Shift+K", cmd + "+" + alt + "+" + shift + "+K"},
		{"CommandOrControl+Left", cmd + "+←"},
		{"CommandOrControl+Up", cmd + "+↑"},
		{"CommandOrControl+Space", cmd + "+" + pick(macSymbols(), "␣", "Space")},
		// 用户自己的旧配置里可能有 `Command`（不是 CommandOrControl），照样认。
		{"Command+Q", "⌘+Q"},
		{"Control+Q", "Ctrl+Q"},
		{"", ""},
	}
	for _, c := range cases {
		if got := formatAccel(c.in); got != c.want {
			t.Errorf("%q 画成 %q，want %q", c.in, got, c.want)
		}
	}
}

// TestEveryStoredDefaultShowsUpAsSomethingShortcuts 钉住一条很实的事：默认那六条
// 每一行画出来的都不能是空的。
//
// 摆一格空的录制框，用户看到的就是「这一格什么都没有」——分不清是没绑键还是界面
// 坏了。
func TestEveryStoredDefaultShowsUpAsSomethingShortcuts(t *testing.T) {
	for _, it := range defaultShortcutSettings().Shortcuts {
		if got := formatAccel(accelerator(it.Shortcut)); got == "" {
			t.Errorf("%q 这一格画出来是空的", it.Name)
		}
	}
}
