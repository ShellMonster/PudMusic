// 快捷键的另一半：录下来的键真的会让事情发生。
//
// # 这一票补的是哪两半
//
// `shortcuts.go` 那一票只做了「录下来存住」：设置页里能录、能存、冲突看得出来，
// 按下去什么也不发生。这一票接上缺的两半——**应用内分发**（按键时找到命中的那条
// 命令并真的跑它，见 viewShortcuts）和**全局注册**（应用不在前台时也要能响应，
// 见 shortcut_global.go）。
//
// # 为什么不自己写一套按键分发
//
// MyGo 有一层现成的：`Context.Shortcut(mods, key)` 登记「窗口这一层要处理这个组合
// 键」，引擎在下一帧告诉调用方这一下有没有送到（`mygo/ui/context.go:439`、
// `ui/input.go:856` 的 `engine.shortcut`）。它自带三条我们要的规矩：
//
//   - **修饰键要一个不差**：`routeKey` 比的是 `r.mods == k.mods`
//     （`ui/input.go:800`），不是「包含」。于是设了 ⌘P 之后按 ⌘⇧P 不会顺带响
//     一下播放/暂停。
//   - **焦点优先**：按键先交给焦点上的输入框，输入框说「这一下归我」的话快捷键
//     压根拿不到（`editor.wants`，`mygo/ui/editor_navigation.go:27`）。这一条就是
//     「在搜索框里打字时快捷键不许抢按键」，而且它由引擎保证，不靠我们记得判。
//   - **浮层优先**：`Context.Shortcut` 在对话框开着时直接返回 false
//     （`ui/context.go:440` 的 `insideModal`）——对话框在要键的时候键归它。
//
// # 有一处必须自己让：正在录的那一格
//
// 引擎在把按键交给焦点上的元素**之前**先问「有没有人认领这个组合键」
// （`rt.claimed`，`ui/input.go:564`），认领了就**不**交给那个元素。窗口登记了 ⌘P 的
// 下一帧，正在录的那一格就收不到 ⌘P——而用户想录的往往正是这个键（他把播放/暂停
// 改成别的，又想改回来）。于是 viewShortcuts 在**正在录**的那一帧一个都不登记，
// 全局那一份也从系统上撤下来（shortcut_global.go 里那一条，理由是 Carbon 热键
// 到不了焦点上的元素）。
//
// **代价是「让开」必须收得干净。** 一让开就是整份快捷键都不响，而界面上没有任何地方
// 能说出「为什么现在按什么键都没反应」。所以 `s.recording` 只在三种情况下成立：
// 刚点了那一格、正在录、以及**快捷键设置那一页正画着**——离开那一页就自动收掉
// （见 viewShortcuts）。这一票的头一版就是漏了这一半：录完一个键之后 `s.recording`
// 再也没被清过，于是用户改完任何一个快捷键、走回首页，六条命令当场全哑，
// 要切到别的设置 Tab 才碰运气救回来。
package ui

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// shortcutVolumeStep 是按一次音量键的步长。Biu 的是 `setVolume(Math.min(1, volume +
// 0.05))`（`src/app.tsx:75`），0.05 照抄。
const shortcutVolumeStep = 0.05

// viewShortcuts 是应用内那一份分发，**每帧一次**，由 viewMain 在装完主题之后调。
//
// 它同时是「录制状态」这件事的**兜底**：录完一个键、或者那一页不再画了，都要把
// `s.recording` 收干净——理由见下面两段。
//
// 登记的同时也就知道了这一帧有没有键命中：引擎把上一帧登记过的组合键交给对应的
// 调用方（`engine.shortcut` 命中 `rt.delivered` 就返回 true）。所以这一段既是
// 「告诉引擎窗口认领哪些键」，也是「这一帧有没有键打中」。
func (s *Shell) viewShortcuts(c *ui.Context) {
	// ⚠️ **「正在录」必须在这一帧之前就成立，否则它会永远成立下去。**
	//
	// `s.recording` 非零 = 这一段不登记任何组合键（理由见文件头），于是快捷键从
	// 那一刻起全部不响。而清掉它的只有三处：点 ×、按 Esc、以及录完一个键之后
	// （见 shortcut_keys.go）。漏了任何一处，用户改完一个快捷键、走回首页，六条
	// 命令当场全哑，**界面上一个字都不提**——那正是这一票要消灭的那种症状，
	// 只不过换了个原因。
	//
	// 所以这里有两道兜底：
	//   - 录成功时直接清（主路，在 takeRecordedKey 里）。
	//   - **这一帧没画录制格就清**（兜底）。「正在录」只在快捷键设置那一页上才
	//     有意义：那一页不画了，`keepRecordingFocus` 不跑，焦点迟早会离开那一格，
	//     而 `FocusBind` 并**不**保证离开时把变量清零（实测：点一格再点侧栏回首页，
	//     它还留着）。一帧的延迟无妨——走开之后那一帧本来就没有按键会来。
	//
	// ⚠️ **满屏开着的时候这一页也不画**（票 #161：底层那一层在满屏开着的时候
	// 一个控件都不建），所以「正在录」在那一段也会被这一句收掉。这跟「切走」的
	// 那一支是同一条规矩，而它正是这一条兜底要防的事：录制期间窗口一个组合键
	// 都不登记（这一句提前返回），用户看不见「为什么现在按什么都不响」。
	drawn := s.recorderShown
	s.recorderShown = false
	if s.recording != (shortcutSlot{}) && !drawn {
		s.recording = shortcutSlot{}
		s.rejected = shortcutSlot{}
	}

	// 全局那一列也得跟着让开（见 shortcut_global.go）：macOS 的 Carbon 热键压根
	// 到不了焦点上的元素，不从系统上撤下来，「在全局那一格录 ⌘⌥P」按下的是那条
	// 命令、不是这一格。两半一起让开、一起回来。
	if s.recording != s.registeredFor {
		s.syncGlobalShortcuts()
	}

	if s.recording != (shortcutSlot{}) {
		return
	}
	// 同一个组合键撞了两条命令时**第一条赢**：Biu 是 `shortcuts.find(...)`（先找到
	// 的那条），而冲突在设置页上是看得见的红字，用户改完就好了，不必另立一条规矩。
	// 全部照常登记、只跑第一个命中的：漏登记会让「把第一条清掉之后第二条接手」
	// 要等到下一帧才生效。
	fired := ""
	for _, it := range s.keys.snapshot().Shortcuts {
		mods, key, ok := parseAccel(accelerator(it.Shortcut))
		if !ok {
			continue
		}
		if c.Shortcut(mods, key) && fired == "" {
			fired = it.ID
		}
	}
	if fired != "" {
		s.onShortcut(fired)
	}
}

// runShortcut 是一条命令该做什么。**界面线程上**：应用内那一份在帧里调
// （viewShortcuts），全局那一份在系统热键的回调里调（shortcut_global.go；
// macOS 那边 Carbon 的事件处理器就装在主线程的事件循环上）。
//
// 认不出来的 id 什么都不做：Biu 的 switch 末尾也是 `default: break`
// （`src/app.tsx:82`）。设置文件里手写一条 Biu 有而我们没有的命令时，按下去不该
// 把界面弄坏。
func (s *Shell) runShortcut(id string) {
	switch id {
	case "togglePlay":
		s.app.Toggle()
	case "prev":
		s.app.Prev()
	case "next":
		s.app.Next()
	case "volumeUp":
		s.nudgeVolume(shortcutVolumeStep)
	case "volumeDown":
		s.nudgeVolume(-shortcutVolumeStep)
	case "toggleMiniMode":
		s.toggleMiniPlayer()
	}
}

// nudgeVolume 加减一次音量。
//
// 钳在 0..1：Biu 是 `Math.min(1, volume + 0.05)` / `Math.max(0, volume - 0.05)`
// （`src/app.tsx:75-76`），而 `App.SetVolume` 自己也会钳一遍——两边都钳是因为
// **越界的那一下不能变成「声音突然大到最大」**：钳在读回来之后、送给播放器之前。
//
// ⚠️ **走 `setVolume` 而不是直接 `SetVolume`**：那一条顺带记住「最后一个不是 0
// 的音量」，于是「按了静音之后按音量加」再「取消静音」，放回去的是用户自己调过
// 的那个值，而不是一个记不着的旧值。
func (s *Shell) nudgeVolume(delta float64) {
	s.setVolume(s.app.Snapshot().Volume + delta)
}

// toggleMiniPlayer 切到迷你播放器 / 从它切回来。
//
// Biu 的 mini 模式是「主窗口 hide() + 开一个 mini 窗口」（`electron/ipc/window.ts:41-47`），
// 两个窗口互斥。我们现在是这么做的（见 `miniplayer.go`），而 `/mini-player` 那一条
// **路由仍然留着**：它是主窗口里的一页（歌名 + 歌词），两件事不是一件。
//
// ⚠️ **判据是「迷你窗口开着没有」，不是「当前路由是不是 `/mini-player`」**：
// 迷你窗口是另一个进程里的另一个窗口，主窗口的路由停在哪儿和它没关系。
func (s *Shell) toggleMiniPlayer() {
	// ⚠️ **迷你窗口那一侧要绕回主窗口。** 那一侧的 `s.mini` 是 nil（迷你窗口不会
	// 开着第二个自己），照常走下去就是 `openMiniPlayer()` 建在**迷你外壳**上——
	// 于是按一次「切换mini/完整模式」开出两扇迷你窗口，各放一首歌。
	//
	// 这个坑是登记应用内快捷键那一手带进来的（⌘M 在小窗里也归这一扇管，见
	// `viewMini` 里那句 `viewShortcuts`）。绕法是现成的：`toggleMini` 接的就是
	// 主窗口的 `toggleMiniPlayer`。
	if s.main != nil {
		if s.toggleMini != nil {
			s.toggleMini()
		}
		return
	}
	if s.mini != nil {
		s.closeMiniPlayer()
		return
	}
	s.openMiniPlayer()
}

// navigateBack 退回上一页。
//
// 顶栏那个后退按钮（navbar.go 的 `ui.BackButton`）退回去时顺手把提示和崩溃状态
// 一起清掉，这里照同样的一遍：换页之后上一页的 Toast 不该留着、崩过的状态同样
// 该复位——否则「按了快捷键退回去了，右下角还挂着上一页那句提示」。
func (s *Shell) navigateBack() {
	if !s.router.CanGoBack() {
		return
	}
	s.clearToasts()
	s.crash = nil
	s.router.Back()
}

// ─────────────────────────────────────────────────────────────────────────────
// accelerator 串 → (修饰键, 键)
// ─────────────────────────────────────────────────────────────────────────────

// parseAccel 是 accelFromEvent 的**反函数**：存下来的那一串变回引擎认得的
// (修饰键, 键)。
//
// 双向都得有，是因为存的是 Biu 的写法而引擎要的是自己那一对：两边各写一遍的话，
// 一边加了新键而另一边没加，症状是「设置页里能录，界面上显示也正常，按下去什么
// 也不发生」——所以这一对由 `TestEveryRecordedKeyComesBackAsItself` 钉着，
// 每个能录的键 × 每一种修饰键都得原样回来。
//
// 认不出来的串返回 false，调用方跳过那一条，而不是硬猜一个键——硬猜出来的结果是
// 用户按 A 结果另一个动作响了，而界面上没有任何提示。
func parseAccel(a accelerator) (ui.Modifiers, ui.Key, bool) {
	parts := accelParts(string(a))
	if len(parts) < 2 {
		// 只有一段：那一段是键，没有修饰键（「P」「Left」都是合法的快捷键）。
		key, ok := accelKeyFromName(parts[0])
		return 0, key, ok
	}
	key, ok := accelKeyFromName(parts[len(parts)-1])
	if !ok {
		return 0, 0, false
	}
	var mods ui.Modifiers
	for _, p := range parts[:len(parts)-1] {
		m, ok := accelModFromName(p)
		if !ok {
			return 0, 0, false
		}
		mods |= m
	}
	return mods, key, true
}

// accelParts 按 `+` 拆开 accelerator。
//
// ⚠️ 末尾是 `++` 的那一种（键本身就是加号）要特别处理：`strings.Split` 会把最后
// 两段都拆成空的，于是「键」变成空串、修饰键多出一个空的，最后解析失败——
// 「我按 ⌘+ 想换歌，结果录进去了按下去没反应」。Biu 那边用 `+` 本身当键的写法是
// `CmdOrCtrl++`（`src/common/utils/shortcut.ts` 的 `mapKeyToElectronAccelerator`）。
//
// 逐字照 MyGo 自己的拆法（`mygo/internal/accelerator` 的 `splitParts`）：同一份
// accelerator 字符串在这两边必须拆成同一件事，拆法不一样的话症状是「全局注册上
// 了但应用内按不动」，或者反过来。
func accelParts(s string) []string {
	parts := strings.Split(s, "+")
	switch {
	case strings.HasSuffix(s, "++"):
		return append(parts[:len(parts)-2], "+")
	case s == "+":
		return []string{"+"}
	}
	return parts
}

// accelModFromName 是 accelFromEvent 那三个词的反面。
//
// `CommandOrControl` → `ui.Cmd`（mac 上是 ⌘、别处是 Ctrl），而**不是** `ui.Super`：
// 存下来的那个词在 mac 上指 ⌘、在 Windows 上指 Ctrl，`ui.Cmd` 才是同一件事。
// 手改过的文件里写 `Command` / `Cmd` / `Super` / `Meta` 都收下——它们在
// `mygo/internal/accelerator` 的词表里也是同一个意思。
func accelModFromName(name string) (ui.Modifiers, bool) {
	switch strings.ToLower(name) {
	case "commandorcontrol", "command", "cmd", "super", "meta", "win":
		return ui.Cmd, true
	case "control", "ctrl":
		return ui.Ctrl, true
	case "alt", "option":
		return ui.Alt, true
	case "shift":
		return ui.Shift, true
	}
	return 0, false
}

// accelKeyFromName 是 accelKeyName 的反函数。
//
// 大小写都收：Biu 写出来的是大写（`CommandOrControl+P`），而手改过的设置文件里
// 小写的写法很常见，MyGo 自己的 accelerator 解析器也是小写之后比
// （`mygo/internal/accelerator` 的 `parseKey`）。
func accelKeyFromName(name string) (ui.Key, bool) {
	// 单个字符：字母、数字。**先按字节判长度**——「é」在 UTF-8 里是两个字节，
	// 当成两个字符去查会落到下面那张表里去，而那里没有它，于是老老实实返回 false。
	if len(name) == 1 {
		switch c := name[0]; {
		case c >= 'A' && c <= 'Z':
			return ui.KeyA + ui.Key(c-'A'), true
		case c >= 'a' && c <= 'z':
			return ui.KeyA + ui.Key(c-'a'), true
		case c >= '0' && c <= '9':
			return ui.Key0 + ui.Key(c-'0'), true
		}
	}
	switch strings.ToLower(name) {
	case "space":
		return ui.KeySpace, true
	case "enter", "return":
		return ui.KeyEnter, true
	case "up":
		return ui.KeyUp, true
	case "down":
		return ui.KeyDown, true
	case "left":
		return ui.KeyLeft, true
	case "right":
		return ui.KeyRight, true
	case "-", "minus":
		return ui.KeyMinus, true
	case "=", "equal", "plus", "+":
		// `+` 落到 `KeyEqual` 是照 MyGo 在 macOS 上那一半的处理：Carbon 要的是
		// 「= 那个键 + 按住 Shift」（`mygo/internal/darwin/hotkey.go:70`），而
		// 引擎这一侧「加号」就是 `KeyEqual`。两边指同一个物理键。
		return ui.KeyEqual, true
	case ",", "comma":
		return ui.KeyComma, true
	case ".", "period":
		return ui.KeyPeriod, true
	case "/", "slash":
		return ui.KeySlash, true
	case ";", "semicolon":
		return ui.KeySemicolon, true
	case "'", "quote":
		return ui.KeyQuote, true
	case "[", "bracketleft":
		return ui.KeyBracketLeft, true
	case "]", "bracketright":
		return ui.KeyBracketRight, true
	case `\`, "backslash":
		return ui.KeyBackslash, true
	case "`", "backquote", "grave":
		return ui.KeyBackquote, true
	}
	// F1~F12。Biu 的默认键里没有 F 键（它们只是「能录」），而手改过的文件里
	// 会有——解析不了的话「我设了 F5」就成了又一条录得进按不动的键。
	if len(name) >= 2 && (name[0] == 'F' || name[0] == 'f') {
		n := 0
		for i := 1; i < len(name); i++ {
			if name[i] < '0' || name[i] > '9' {
				return ui.KeyUnknown, false
			}
			n = n*10 + int(name[i]-'0')
		}
		if n >= 1 && n <= 12 {
			return ui.KeyF1 + ui.Key(n-1), true
		}
	}
	return ui.KeyUnknown, false
}
