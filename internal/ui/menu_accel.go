// 快捷键字符串的两副写法：设置页存的那一份，和菜单要的那一份。
//
// # 为什么要有这一层
//
// **两边的词表不一样**，这是这一层存在的全部理由：
//
//   - 设置页存的是 **Biu 那一套**：`CommandOrControl+P`、`Shift+Left`、
//     `CommandOrControl+Alt+P`（`internal/ui/shortcuts.go` 的
//     `defaultShortcutSettings`，上游是
//     `_research/biu/shared/settings/shortcut-settings.ts`）。
//   - 框架的菜单要的是 **Electron 那一套**：`CmdOrCtrl+P`、`Shift+Left`、
//     `CmdOrCtrl+Alt+P`（`mygo/internal/accelerator/accelerator.go:98` 的
//     `Parse`）。
//
// 看着只差一个词（`CommandOrControl` vs `CmdOrCtrl`），但**不能直接互喂**：
//
//   - 框架的解析器认得 `CommandOrControl`（`accelerator.go:143` 的 `modifier`
//     里 `commandorcontrol` 那一条），**所以设置页那一份其实也能直接喂给它**；
//   - 但**方向键要小心**：`Shift+Left` 两边都认（框架的 `namedKeys` 里有
//     `left`，`accelerator.go:60`），而**框架的 `keyEquivalent` 在 macOS 上
//     对方向键有专门的一套编码**（`mygo/internal/darwin/menu.go:297` 的
//     `named` 表：`Left` → `0xF702`）——那一套是**给菜单用的**，不是给引擎用的。
//
// ⇒ 所以这一层做的是：**把设置页那一份翻译成菜单能用的那一份**，而不是
// 「两边凑巧一样就不用管」。
//
// # ⚠️ 翻译不了的那些不能悄悄吞掉
//
// 设置页里可能有用户手写的、框架的解析器认不出来的组合（比如某个框架不认的
// 键名）。那一条：
//
//   - **菜单上不显示那个键**（显示一个按了没反应的键比不显示更糟）；
//   - **但那个键在设置页里依然有效**——快捷键派发那一路走的是我们自己的
//     `parseAccel`（`shortcut_dispatch.go:259`），它认的词表比框架宽。
//
// 这两件事不矛盾：菜单上不显示 ≠ 那个键不生效。
package ui

import "strings"

// menuAccel 是「设置页存的那一份」→「菜单要的那一份」的翻译结果。
//
// ⚠️ **空字符串是有意义的**：它表示「这一条翻译不了」，而调用方据此**不显示
// 快捷键**，但**不因此让那条命令失效**。
type menuAccel struct {
	// accel 是菜单要的 accelerator（`CmdOrCtrl+P` 那一套）。
	// 空串 = 翻译不了。
	accel string
	// ok 报这一条翻译得了翻译不了。
	//
	// ⚠️ **为什么不用空串当判据**：理论上可以，但留一个显式的判据让读代码的
	// 人不必去查「空串到底是没有还是翻译不了」。
	ok bool
}

// toMenuAccel 把设置页存的那一份翻译成菜单要的 accelerator。
//
// ⚠️ **这一层是单向的**（设置页 → 菜单）。反方向不需要：菜单上的 accelerator
// 是**我们写进去的**，不是用户录的。
//
// ⚠️ **翻译不了的返回空串**：见 `menuAccel` 的注释——调用方要据此**不显示**
// 快捷键，而不是让那条命令失效。
func toMenuAccel(stored accelerator) menuAccel {
	s := strings.TrimSpace(string(stored))
	if s == "" {
		// 没有快捷键 = 用户清空了它（设置页能清空一格）。菜单上就不显示键。
		return menuAccel{}
	}
	parts := splitAccelParts(s)

	// ⚠️ **修饰键要按框架的规范化顺序重排**，不能照设置页存的那一份原样带过去。
	//
	// 设置页存的是「Cmd、Alt、Shift」（`accelFromEvent`，`shortcut_keys.go:84-95`），
	// 而框架的 `Accelerator.String()`（`accelerator.go:37`）是
	// **Super、Ctrl、Alt、Shift**。⇒ 同一个键在菜单上和在设置页上看起来不一样，
	// 而菜单上的键是要给用户照着按的——两份不一样就是「我按了菜单上写的那个键，
	// 没反应」。
	var cmdOrCtrl, super, ctrl, alt, shift bool
	var key string
	for _, p := range parts {
		switch strings.ToLower(p) {
		case "commandorcontrol", "cmdorctrl":
			cmdOrCtrl = true
		case "command", "cmd", "super", "meta", "win":
			// ⚠️ **`Command` 是「真的 Command」，不是「Command 或 Ctrl」**。
			// 设置页里 `accelCmdRaw = "Command"` 那个词只在 macOS 上有意义
			//（`shortcut_keys.go:31`）；在别处它指的是 Windows 键。
			//
			// 菜单上照原样给 `Super`——框架在 macOS 上把它翻译成 ⌘
			//（`mygo/internal/darwin/menu.go:285`），在别处是 Windows 键。
			//
			// ⚠️ **它和 `CommandOrControl` 是两回事**，不能合并成一个标志：
			// 框架的解析器把 `CmdOrCtrl` 在 macOS 上解成 Super、在别处解成
			// Ctrl（`accelerator.go:143`），而 `Super` **在哪儿都是 Super**。
			// 合并了就等于把「这一条在 Windows 上是 Ctrl」改成「是 Windows 键」。
			super = true
		case "control", "ctrl":
			ctrl = true
		case "alt", "option":
			alt = true
		case "shift":
			shift = true
		default:
			// 键名那一段：原样带过去，但要确认框架认得它。
			//
			// ⚠️ **认不得就要说「翻译不了」**，不能原样丢过去——那样菜单上会
			// 出现一个按了没反应的键（而设置页里那个键是有效的，那更让人困惑：
			// 「设置页说这个键管用，菜单上写着它，点了却没反应」）。
			if !frameworkKnowsKey(p) {
				return menuAccel{}
			}
			key = p
		}
	}
	if key == "" {
		// 只有修饰键没有键——那不是一条快捷键。
		return menuAccel{}
	}
	out := make([]string, 0, 5)
	if cmdOrCtrl {
		out = append(out, "CmdOrCtrl")
	}
	if super {
		out = append(out, "Super")
	}
	if ctrl {
		out = append(out, "Ctrl")
	}
	if alt {
		out = append(out, "Alt")
	}
	if shift {
		out = append(out, "Shift")
	}
	out = append(out, key)
	return menuAccel{accel: strings.Join(out, "+"), ok: true}
}

// splitAccelParts 把 accelerator 拆成几段。
//
// ⚠️ **`+` 本身可以是键**（框架的 `splitParts` 就是这么处理的，
// `accelerator.go:126`：`CmdOrCtrl++` 表示「加号那个键」）。而设置页的
// `accelKeyName` 把加号写成 `=`（`shortcut_keys.go:127`）——它用的是「=
// 那个键 + Shift」的记法，不是 `+`。所以这里**不用**处理结尾的 `++`：
// 设置页存的那一份里 `+` 只会作为分隔符出现。
func splitAccelParts(s string) []string {
	return strings.Split(s, "+")
}

// frameworkKnowsKey 报框架的 accelerator 解析器认不认这个键名。
//
// ⚠️ **这张表是照着框架的 `namedKeys` 抄的**
// （`mygo/internal/accelerator/accelerator.go:55-95`）**加上单字符**。
//
// 抄它而不是调用框架的解析器，是因为 `mygo/internal/accelerator` 是**内部
// 包**，导不进来（`internal/` 的规矩）。
//
// ⚠️ **这张表和框架的那一份会漂**：框架加了新键名 ours 就得跟着加。
// 那时的症状是「菜单上那一项没显示快捷键，而设置页里那个键是有效的」——
// 不致命，但看得见。所以下面那条测试钉住**我们支持的键名**这一侧。
func frameworkKnowsKey(name string) bool {
	// 单个字符（字母、数字、符号）一律认。
	//
	// ⚠️ **按字节判长度**（照 `accelKeyFromName` 的做法，
	// `shortcut_dispatch.go:329`）：「é」在 UTF-8 里是两个字节，当成两个字符
	// 去查会落到下面那张表里去，而那里没有它。
	if len(name) == 1 {
		return true
	}
	switch strings.ToLower(name) {
	case "plus", "space", "tab", "capslock", "numlock", "scrolllock",
		"backspace", "delete", "del", "insert", "ins",
		"return", "enter",
		"up", "down", "left", "right", "home", "end", "pageup", "pagedown",
		"escape", "esc",
		"volumeup", "volumedown", "volumemute",
		"medianexttrack", "mediaprevioustrack", "mediastop", "mediaplaypause",
		"printscreen",
		"numdec", "numadd", "numsub", "nummult", "numdiv":
		return true
	}
	// F1~F24（框架认 F1~F24，我们只认 F1~F12——见 `accelKeyFromName`）。
	if lower := strings.ToLower(name); len(lower) >= 2 && lower[0] == 'f' {
		n := 0
		for i := 1; i < len(lower); i++ {
			if lower[i] < '0' || lower[i] > '9' {
				return false
			}
			n = n*10 + int(lower[i]-'0')
		}
		return n >= 1 && n <= 12
	}
	return false
}
