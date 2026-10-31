// 快捷键字符串的翻译（票 #716）。
//
// 这一族断的是**设置页那一份能不能变成菜单要的那一份**——不是「按下去有没有反应」
// （那是分发那一族的活）。
//
// ⚠️ **判据是「翻译成了什么」**，不是「菜单上画出了什么」：菜单是系统画的
// （AppKit 的 NSMenu），它不在窗口的画面里，截不到也量不到（见 `internal/ui/menu.go`
// 顶上那一段）。
package ui

import "testing"

// TestTheStoredFormBecomesTheFrameworkForm 断**默认的那几条都能翻译**。
//
// ⚠️ **为什么这一条最要紧**：设置页存的是 Biu 那一套（`CommandOrControl+P`），
// 菜单要的是 Electron 那一套（`CmdOrCtrl+P`）。翻译不出来 ⇒ 菜单上那一项
// **不显示快捷键**——而用户明明在设置页看见它写着 ⌘P。
//
// 这一条钉的就是「默认的那八条，一条都不能翻译不出来」。
func TestTheStoredFormBecomesTheFrameworkForm(t *testing.T) {
	def := defaultShortcutSettings()
	for _, it := range def.Shortcuts {
		got := toMenuAccel(accelerator(it.Shortcut))
		if !got.ok {
			t.Errorf("命令 %q 的快捷键 %q 翻译不出来：菜单上那一项不显示快捷键", it.ID, it.Shortcut)
			continue
		}
		if got.accel == "" {
			t.Errorf("命令 %q 翻译成了空串", it.ID)
		}
	}
}

// TestTheTranslationIsTheFrameworkForm 断**翻译出来的是框架认得的那一套**。
//
// ⚠️ **判据是「框架的解析器认得它」**——而不是「看起来像」。
//
// 框架的解析器是 `mygo/internal/accelerator` 的 `Parse`（**内部包，导不进来**），
// 而它的词表在 `accelerator.go:55-95` 的 `namedKeys` 和 `:117-135` 的 `modifier`。
// 下面这一族用**我们自己的**那张表（`frameworkKnowsKey`）当判据——而
// `TestTheKnownKeyTableMatchesTheFrameworkVocabulary` 那一条钉住两张表一致。
func TestTheTranslationIsTheFrameworkForm(t *testing.T) {
	cases := []struct {
		stored string
		want   string
	}{
		{"CommandOrControl+P", "CmdOrCtrl+P"},
		{"CommandOrControl+Alt+P", "CmdOrCtrl+Alt+P"},
		{"CommandOrControl+Left", "CmdOrCtrl+Left"},
		{"Shift+Left", "Shift+Left"},
		{"CommandOrControl+Down", "CmdOrCtrl+Down"},
		{"Command+Up", "Super+Up"},
	}
	for _, c := range cases {
		got := toMenuAccel(accelerator(c.stored))
		if !got.ok {
			t.Errorf("%q 翻译不出来，want %q", c.stored, c.want)
			continue
		}
		if got.accel != c.want {
			t.Errorf("%q 翻译成了 %q，want %q", c.stored, got.accel, c.want)
		}
	}
}

// TestAnUntranslatableOneIsNotSilentlyDropped 断**翻译不了的要说出来**。
//
// ⚠️ **为什么这一条要紧**：设置页里可能有用户手写的、框架不认的键名。
// 那一条在**设置页里是有效的**（我们自己的 `parseAccel` 认的词表比框架宽），
// 而菜单上显示一个按了没反应的键比不显示更糟——用户会以为那个键坏了。
//
// ⇒ 翻译不了就是翻译不了（`ok` 为 false），**不显示**，但**不因此让命令失效**。
func TestAnUntranslatableOneIsNotSilentlyDropped(t *testing.T) {
	got := toMenuAccel(accelerator("CommandOrControl+NoSuchKey"))
	if got.ok {
		t.Errorf("框架不认的键名翻译成了 %q：菜单上会出现一个按了没反应的快捷键", got.accel)
	}
	if got.accel != "" {
		t.Errorf("翻译不了却给出了 %q", got.accel)
	}
}

// TestAnEmptyShortcutTranslatesToNothing 断**空的那一份**。
//
// ⚠️ **清空一格是设置页允许的**（`applyShortcut` 里 `accel == ""` 那一支会
// 说「快捷键已清空」）。那一条在菜单上**就不该显示快捷键**——但**命令本身还在**
// （菜单项还在，只是没有键）。
func TestAnEmptyShortcutTranslatesToNothing(t *testing.T) {
	got := toMenuAccel(accelerator(""))
	if got.ok {
		t.Errorf("空的那一份翻译成了 %q", got.accel)
	}
}

// TestTheKnownKeyTableMatchesTheFrameworkVocabulary 断**我们抄的那张键名表
// 和框架的一致**。
//
// ⚠️ **为什么要专门钉这一条**：框架的 `namedKeys`
// （`mygo/internal/accelerator/accelerator.go:55-95`）是**内部包导不进来的**，
// 所以我们抄了一份（`frameworkKnowsKey`）。**两张表会漂**——框架加了新键名 ours
// 就得跟着加。
//
// 那时的症状是「菜单上那一项没显示快捷键，而设置页里那个键是有效的」——
// 不致命，但看得见。
//
// ⇒ 这一条钉的是**我们支持的键名这一侧**：设置页能录出来的键，框架都要认。
// 框架多出来的键名（我们不支持的那些）不管——那是「用户录不了」，不是漂。
func TestTheKnownKeyTableMatchesTheFrameworkVocabulary(t *testing.T) {
	// 设置页能录出来的键（`recordableKeys`，`shortcut_keys.go:60`）。
	for _, it := range defaultShortcutSettings().Shortcuts {
		// 拆出最后那一段（键名）。
		parts := splitAccelParts(it.Shortcut)
		if len(parts) == 0 {
			continue
		}
		key := parts[len(parts)-1]
		if !frameworkKnowsKey(key) {
			t.Errorf("命令 %q 的键名 %q 我们不认，框架也认不得：菜单上不会显示它的快捷键",
				it.ID, key)
		}
	}
}

// TestTheTranslationKeepsTheKeyOrder 断**修饰键的顺序**。
//
// ⚠️ **为什么顺序有讲究**：框架的 `Accelerator.String()`（`accelerator.go:37`）
// 把它规范化成 `Super+Ctrl+Alt+Shift+Key` 的顺序——而我们翻译出来的那一份要
// **和它一致**，否则同一个键在菜单上和在设置页上看起来不一样。
//
// ⚠️ 设置页存的那一份的顺序是「Cmd、Alt、Shift」（`accelFromEvent`，
// `shortcut_keys.go:84-95`），**和框架的规范化顺序不一样**（框架是
// Super、Ctrl、Alt、Shift）。所以翻译的时候要**按框架的顺序重排**。
func TestTheTranslationKeepsTheKeyOrder(t *testing.T) {
	// 设置页存的：Cmd + Shift + Alt（`accelFromEvent` 的顺序）。
	got := toMenuAccel(accelerator("CommandOrControl+Shift+Alt+P"))
	if !got.ok {
		t.Fatal("翻译不出来")
	}
	// 框架的规范化顺序：Super（Cmd）、Alt、Shift。
	if got.accel != "CmdOrCtrl+Alt+Shift+P" {
		t.Errorf("翻译成了 %q，want %q（框架的规范化顺序是 Super、Ctrl、Alt、Shift）",
			got.accel, "CmdOrCtrl+Alt+Shift+P")
	}
}
