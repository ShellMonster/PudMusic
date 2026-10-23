// 快捷键的写法：把一次按键变成 Biu/Electron 的 accelerator 串，再把那个串画成
// 人看得懂的符号。照 Biu 的 `src/common/utils/shortcut.ts` 两个函数逐条来。
//
// ## 为什么存的是 Electron 的 accelerator，而不是 MyGo 的 (Modifiers, Key)
//
// 因为要**和 Biu 双向兼容**：用户在 Biu 里录的 ⌘⇧P 换到这边得认得，反过来也是。
// MyGo 那一对在 macOS 上是 `Super`、别处是 `Ctrl`，写出去就成了 `Ctrl+Shift+P`，
// Biu 那边读不懂，`CommandOrControl` 才是两边都认的那个词。
//
// ## 三个规矩（Biu 的 handleKeyDown + PLAN C-49）
//
//   - **所有按键都吃掉**：回调返回 true，MyGo 就不会把这个键继续交给 Tab、方向键
//     滚动、菜单角色那一整套（`ui/input.go:679` 的 `rt.deliver` 之后直接
//     `return true`）。不这么做的话，在这一格里敲 A 会跳到下一个控件，敲 ← 会把
//     整页滚走——录的是 ⌘←，得到的是「焦点跑掉了」。
//   - **Backspace / Delete 清空**这一条。
//   - **Escape 失焦，而且不算录入**：这一格不再吃键，下一次按键走回全局的语义。
//     「不算录入」很重要：用户是想取消，不是想把 Esc 设成快捷键。
package ui

import (
	"runtime"
	"strconv"
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// accelerator 是 Biu（= Electron）写快捷键的格式：`CommandOrControl+Alt+Shift+P`。
//
// 存的是**一串**而不是结构体，为的是它原样就是 Biu 文件里的值，读写都不用翻译。
type accelerator string

// 修饰键在串里的那三个词。顺序照 Biu：CommandOrControl、Alt、Shift。
const (
	accelCmd    = "CommandOrControl"
	accelCmdRaw = "Command"
	accelCtrl   = "Control"
	accelAlt    = "Alt"
	accelShift  = "Shift"
)

// recordableKeys 是能当快捷键的键。别的键（比如 ⌘ 本身、F13、Insert）按下去只会
// 记成非法并把这一格描红，不会存进去。
//
// 收录范围照 PLAN 的那条备注：「`ui.Key` 只有 F1~F12，没有媒体键、没有数字键盘 |
// 应用内快捷键只绑字母/方向键/F 键，媒体键走阶段 7」。字母、数字、F 键、方向键
// 之外，把编辑键也收进来（空格、回车、Tab 和那排标点）——它们都在 `ui.Key` 里，
// 不是什么贵重资源，删掉只会让「我想用 ⌘\ 换歌」这种正常要求录不进去。
//
// ⚠️ **Backspace / Delete / Escape 不在里面**，它们有别的意思（清空 / 取消）。
// 另外 Tab 也不在：它同时是「焦点往下一个」的意思，而这一格吃掉所有键，Tab 就
// 没法用来走开——所以取消只留 Escape 一条路，这一页里也说得出。
var recordableKeys = func() map[ui.Key]bool {
	m := map[ui.Key]bool{
		ui.KeySpace: true, ui.KeyEnter: true,
		ui.KeyLeft: true, ui.KeyRight: true, ui.KeyUp: true, ui.KeyDown: true,
		ui.KeyMinus: true, ui.KeyEqual: true, ui.KeyComma: true, ui.KeyPeriod: true,
		ui.KeySlash: true, ui.KeySemicolon: true, ui.KeyQuote: true,
		ui.KeyBracketLeft: true, ui.KeyBracketRight: true, ui.KeyBackslash: true,
		ui.KeyBackquote: true,
	}
	// 字母、数字、F1~F12 这三段在 `ui.Key` 里是**连续的**（keys.go 就是按这个
	// 顺序排的），所以按范围加。逐个列 46 个字母和数字看上去更「完整」，但漏一
	// 个的症状是「我明明按了 ⌘Q 它没反应」——最难查的那一种；范围不会漏。
	for k := ui.KeyA; k <= ui.KeyZ; k++ {
		m[k] = true
	}
	for k := ui.Key0; k <= ui.Key9; k++ {
		m[k] = true
	}
	for k := ui.KeyF1; k <= ui.KeyF12; k++ {
		m[k] = true
	}
	return m
}()

// accelFromEvent 把一次按键变成 accelerator。ok=false 表示这一下不该被录进去
// （修饰键本身、不能当快捷键的键）。
func accelFromEvent(ev ui.InputEvent) (accelerator, bool) {
	// 布局认不出的键（西里尔布局上的 ы）报 KeyUnknown，用它在美国键盘上的位置
	// 顶上——`ui.InputEvent.PhysicalKey` 的文档就是这么说的。没有位置信息就
	// 真的认不出来，只能当非法键。
	key := ev.Key
	if key == ui.KeyUnknown {
		key = ev.PhysicalKey
	}
	if !recordableKeys[key] {
		return "", false
	}

	var parts []string
	// `ui.Cmd` 是 Command（macOS）/ Ctrl（别处），两边合成 Biu 的那一个词。
	if ev.Mods&(ui.Super|ui.Ctrl) != 0 {
		parts = append(parts, accelCmd)
	}
	if ev.Mods&ui.Alt != 0 {
		parts = append(parts, accelAlt)
	}
	if ev.Mods&ui.Shift != 0 {
		parts = append(parts, accelShift)
	}
	return accelerator(strings.Join(append(parts, accelKeyName(key)), "+")), true
}

// accelKeyName 是一个键在 accelerator 里的写法，照 Biu 的那几行。
func accelKeyName(k ui.Key) string {
	switch {
	case k >= ui.KeyA && k <= ui.KeyZ:
		return string(rune('A' + (k - ui.KeyA)))
	case k >= ui.Key0 && k <= ui.Key9:
		return string(rune('0' + (k - ui.Key0)))
	case k >= ui.KeyF1 && k <= ui.KeyF12:
		return "F" + strconv.Itoa(int(k-ui.KeyF1)+1)
	}
	switch k {
	case ui.KeySpace:
		return "Space"
	case ui.KeyEnter:
		return "Enter"
	case ui.KeyUp:
		return "Up"
	case ui.KeyDown:
		return "Down"
	case ui.KeyLeft:
		return "Left"
	case ui.KeyRight:
		return "Right"
	case ui.KeyMinus:
		return "-"
	case ui.KeyEqual:
		return "="
	case ui.KeyComma:
		return ","
	case ui.KeyPeriod:
		return "."
	case ui.KeySlash:
		return "/"
	case ui.KeySemicolon:
		return ";"
	case ui.KeyQuote:
		return "'"
	case ui.KeyBracketLeft:
		return "["
	case ui.KeyBracketRight:
		return "]"
	case ui.KeyBackslash:
		return "\\"
	case ui.KeyBackquote:
		return "`"
	}
	return ""
}

// formatAccel 把 accelerator 画成人看得懂的符号。逐条照 Biu 的
// `formatElectronAcceleratorForDisplay`：macOS 上 CommandOrControl 是 ⌘、Alt 是 ⌥、
// Shift 是 ⇧，方向键是箭头，空格是 ␣；别的平台和 mac 一样用 Ctrl/Alt/Shift。
func formatAccel(a accelerator) string {
	if a == "" {
		return ""
	}
	mac := runtime.GOOS == "darwin"
	parts := strings.Split(string(a), "+")
	for i, p := range parts {
		switch p {
		case accelCmd:
			parts[i] = pick(mac, "⌘", "Ctrl")
		case accelCmdRaw:
			parts[i] = "⌘"
		case accelCtrl:
			parts[i] = "Ctrl"
		case accelAlt:
			parts[i] = pick(mac, "⌥", "Alt")
		case accelShift:
			parts[i] = pick(mac, "⇧", "Shift")
		case "Up":
			parts[i] = "↑"
		case "Down":
			parts[i] = "↓"
		case "Left":
			parts[i] = "←"
		case "Right":
			parts[i] = "→"
		case "Space":
			parts[i] = pick(mac, "␣", "Space")
		}
	}
	return strings.Join(parts, "+")
}

// ─────────────────────────────────────────────────────────────────────────────

// 录制控件
// ─────────────────────────────────────────────────────────────────────────────

// 录制时那一格显示的字。
const (
	// recordingHint 是「正在录、而这一格现在还没有键」时那一格里的字。已经设过键的
	// 那一格在录的时候照常显示它现在的键（Biu 就是这么做的，见 shortcutRecorder）。
	recordingHint = "按下新的快捷键"

	// shortcutFieldWidth / shortcutFieldHeight 是那一格多大。Biu 那一列是
	// `200px` 宽（`shortcut-settings.tsx:102`），高度按一行字加内边距取。
	shortcutFieldWidth  = 200
	shortcutFieldHeight = 30

	// clearButtonSize 是那个 × 的边长。Biu 摆的是 16px 的圆圈图标（`RiCloseCircleFill
	// size={16}`），这里取 18 上下：格子只有 30 高，按钮默认的内边距会让它撑到 29.3
	// 高（几乎占满整格），钉死之后它才是「一个 ×」而不是「一整条按钮」。
	clearButtonSize = 18
)

// shortcutRecorder 画快捷键那一格：没在录的时候显示它现在的键，点了开始录。
//
// 返回**正在录的那一格**（没有在录时是零值 Element）。调用方（`settingsShortcuts`）
// 拿它决定要不要把焦点按回来，见那里。
//
// ## 焦点和「正在录」为什么绑成同一件事
//
// `FocusBind(&s.recording, slot)` 把这一格和 `s.recording` 绑在一起：点了它焦点
// 过去，下一帧 `s.recording` 就等于这一格；而 `s.recording` 被清成零值时焦点也跟着
// 离开（Escape 失焦走的就是这条）。于是「正在录哪一格」不需要另记一份状态——
// 记两份就得自己同步它们不同步时的样子，而那种样子的表现是「有一格在闪，可是按
// 什么都没反应」。
//
// ⚠️ `s.recording` 必须是**可比较**的类型，`FocusBind` 只收可比较值的指针。
// 它的零值 `shortcutSlot{}` 表示「没有在录」，给 FocusBind 的正是「清成零值等于
// 让焦点走开」这条语义（`ui/focus_binding.go`）。
func (s *Shell) shortcutRecorder(c *ui.Context, item shortcutItem, slot shortcutSlot, conflict string, ro bool) (rec ui.Element) {
	t := c.Theme()
	recording := s.recording == slot

	// 画出来的一直是**它现在的键**（Biu 的 `<Input value={format(value)}>` 就是这样，
	// 录的时候那一格里的字不会变成一句提示）：录完立刻看得见自己刚按了什么，是
	// 这一格最要紧的一件事。「正在录」由主色描边和焦点环表示。
	//
	// ⚠️ 一格没设键的时候正在录也要说清「现在按下去就会录上」——空框配主色描边，
	// 用户会以为那是一个空的按钮。
	border := t.Border
	text := formatAccel(accelerator(item.Shortcut))
	switch {
	case item.Shortcut == "" && recording:
		border = t.Accent
		text = recordingHint
	case item.Shortcut == "":
		text = "未设置"
	case recording:
		border = t.Accent
	}
	// 描红的两件事：和别的键撞了（Biu 的 isConflict），和刚才按的那个键根本不
	// 能录成快捷键。撞了还带一句是谁撞的，只有描红的话用户不知道自己撞了谁。
	invalid := conflict != "" || s.rejected == slot
	if invalid {
		border = t.Danger
	}
	color := t.Text
	switch {
	case recording:
		// 「正在录」这几个字是主色当**字**：浅色下换成与底色 ≥4.5:1 的
		// 变体（理由见 `widgets/ink`），否则录到一半的话读不出来。
		color = ink.AccentOn(t)
	case invalid:
		// ⚠️ **取「危险色当**字色**」的那一档**（票 #241 / 报告 F-22）：这一格是
		// 画在 `Background(t.Surface)` 上的，而浅色下原值 `#f31260` 压在那上面
		// 只有 **3.77:1**——「这一格和别的键撞了」是最该读出来的一句话。
		//
		// 那一档（`dangerText` → `ink.SemanticOn`）对的是**最严的那个底**
		// （`t.SurfaceHover`：浅色下最暗、深色下最亮），而 `t.Surface` 夹在中间
		// ——所以它在 `t.Surface` 上也够 4.5:1。
		color = dangerText(t)
	}

	// 🔴 **这一格必须是 `ui.Row`，不是 `ui.Box`。**
	//
	// `ui.Box` 就是**列**（`ui/context.go` 的 coreBox → coreColumn）：子元素从上往下
	// 排，而且**每一个都拉满列的宽度**。所以「键 + ×」在 Box 里是**上下两层**：键占满
	// 184 宽的那一层，× 同样占满 184 宽、而高度有 29，于是它整个掉到框的**外面**去
	// ——点不到（Biu 那个 × 变成死按钮），而且从画面上看是「两层框、× 压着下一行」。
	// 键也在上面那一层贴着上边框，没有上下居中。
	//
	// 实测（`TestEveryRecorderCellKeepsItsClearButtonInside` 的注释里有整张表）：
	//
	//	Box(Text + Button) → 键 184x15.3 @ y=0    × 184x29.3 @ y=15.3   ← 掉出框外
	//	Row(Text.Grow + Button) → 键 145.9x15.3 @ y=7.3  × 38.1x29.3 @ y=0.3 ← 框内、居中
	field := ui.Row(c).Key(shortcutFieldKey(slot)).
		Width(shortcutFieldWidth).Height(shortcutFieldHeight).
		Radius(t.Radius).Background(t.Surface).Border(1, border).
		Padding(space.None, space.Px8).AlignItems(ui.Center).
		FocusBind(&s.recording, slot).Label(recorderLabel(item, slot))
	if ro {
		// 只读时这一格既不该吃键也不该抢焦点：Disabled 的元素不取焦点、不响应
		// 点击、不处理输入（`ui/element.go:1299`）。FocusBind 已经把它标成可
		// 聚焦了，所以这里必须补上这一句，否则用户一点就进到「正在录」那一支，
		// 而按什么键都存不住。
		field = field.Disabled(true)
	} else {
		s.takeRecordedKey(field, slot)
	}

	field.Children(func() {
		// `Grow(1)`：把 × 右边剩下的宽度全吃掉，于是 × 永远贴着右边，超长的键被
		// `SingleLine` 截断而不是把 × 顶出框外（手改过的设置文件里可能有很长的值）。
		ui.Text(c, text).FontSize(scale.Font(c, scale.Size13)).SingleLine().Grow(1).Shrink(0).TextColor(color)
		// 清空。Biu 的 `endContent` 那个 ×（`shortcut-key-input/index.tsx:46`），
		// 它的显示条件是「不是只读**且**有值」——**不是**「没在录」。
		//
		// 🔴 「没在录时才摆」会让这个 × 变成**永远点不到的死按钮**：点它的那一下，
		// 指针事件是在帧之前落到引擎上的（`ui/input.go` 的 pointerDown），我们的
		// `InputPointerDown` 立刻把 `s.recording` 设成这一格；紧接着 view 才跑，
		// 于是那一帧「正在录」成立，× **根本没被建出来**，`Clicked()` 自然读不到。
		// 用户看到的是一个点下去没反应的图标——整格扫 60 个点，一个都清不掉。
		//
		// 所以照 Biu：只要有值就摆着。点它 = 清空**并且**退出录制（清空是一个收尾
		// 动作，不该顺手把这一格留在「等着录下一个键」的状态里）。
		//
		// 这一格没设键的时候不摆：没东西可清的时候摆一个叉只是让人多按一次空的。
		//
		// ⚠️ 尺寸是钉死的：不钉的话 Button 默认的内边距让它有 29.3 高，在 30 高的
		// 格里上下各只让出 0.35 DIP，看上去就是「贴边的一个大方块」而不是一个 ×。
		if !ro && item.Shortcut != "" &&
			ui.Button(c, "×").Label("清空「"+item.Name+"」的快捷键").
				FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).
				Size(clearButtonSize, clearButtonSize).Padding(space.None).Radius(clearButtonSize/2).
				Clicked() {
			s.rejected = shortcutSlot{}
			s.recording = shortcutSlot{}
			s.applyShortcut(slot, "")
		}
	})

	if recording && !ro {
		rec = field
	}
	return rec
}

// shortcutFieldKey 是那一格的 key。用命令 id 而不是序号（🔴 铁律四）：录完一个键
// 那一格的字会变、点叉清空之后叉还会消失，序号不是稳定的身份，身份一变
// `FocusBind` 记在这一格上的状态就落到了旁边那一格上。
func shortcutFieldKey(slot shortcutSlot) string {
	if slot.global {
		return "shortcut-field:global:" + slot.id
	}
	return "shortcut-field:app:" + slot.id
}

// keepRecordingFocus 把焦点按在正在录的那一格上，返回值是「能不能按」。
//
// ## 为什么要每帧按一次
//
// `ui.Router` 在焦点掉到 0 的时候会把焦点收回到**页面根上**
// （`mygoui/ui/router.go` 的 `focusPage`）。而在带路由的页面里，焦点每隔一帧就
// 掉一次（实测：committed 的焦点是「在、掉、在、掉」地摆）。掉下去的那一帧里，
// 按键既不在这一格的 `HandleInput`，也不在窗口的快捷键上——**按什么键都没人接**。
//
// ⚠️ 焦点要在**循环之外**、这一格建完之后再按，不能在建它的那一行里按。
// 指针事件是在帧之前就落到引擎上的（`Tester.ClickAt` 和真窗口的 Cocoa 事件都是
// 这样，`ui/input.go` 的 pointerDown），而 view 在那之后才跑：建 A 的那一行里按
// 焦点，等于把用户刚点的 B 上的焦点又抢回 A——症状是「点了音量加，开始录的还是
// 播放/暂停」，而且没有任何报错。
//
// 元素在同一个 build pass 里是有效的（`Element` 只在**下一个** pass 之前过期），
// 所以「建完之后再按」做得到：`settingsShortcuts` 就是把正在录的那一格一路带出
// 循环，最后按一次。
func (s *Shell) keepRecordingFocus(rec ui.Element) {
	if rec.Valid() {
		rec.Focus()
	}
}

// recorderLabel 是这一格给读屏软件和无头 Tester 的名字。
//
// ⚠️ 必须是**稳定**的：画面上那一格显示的字在录的时候会变（「按下新的快捷键」、
// 录完之后变成新的键），拿它当名字的话，录完那一帧就再也点不到它了。
func recorderLabel(item shortcutItem, slot shortcutSlot) string {
	if slot.global {
		return "全局快捷键：" + item.Name
	}
	return "应用内快捷键：" + item.Name
}

// takeRecordedKey 把这一格上按下的键读出来。
//
// 回调跑在两帧之间（`ui/handler.go`），那一刻元素已经过期了，所以这里只改
// Shell 的字段，**一个控件都不再碰**。
func (s *Shell) takeRecordedKey(field ui.Element, slot shortcutSlot) {
	field.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind == ui.InputPointerDown {
			// 点这一格就开始录。
			//
			// ⚠️ 返回 false 是刻意的：自己不去吃这个指针事件，引擎照常按命中链
			// 给这一格焦点（`ui/input.go` 的 pointerDown）。焦点和「正在录」是
			// 同一件事——`FocusBind` 把 `s.recording` 跟着焦点一起改，所以这一格
			// 一被点到，两边同时就位。
			s.recording = slot
			s.rejected = shortcutSlot{}
			return false
		}
		if ev.Kind != ui.InputKeyDown || ev.Repeat {
			return true // 按住不放不重复录：录一次就够了
		}
		switch ev.Key {
		case ui.KeyBackspace, ui.KeyDelete:
			// 清空这一格。Biu：`if (e.key === "Backspace" || e.key === "Delete")`。
			s.rejected = shortcutSlot{}
			s.applyShortcut(slot, "")
			return true
		case ui.KeyEscape:
			// 失焦且**不算录入**。清零就是让焦点走开（见 shortcutRecorder 的说明），
			// 下一帧焦点离开这一格，下一次按键回到全局的语义。
			s.recording = shortcutSlot{}
			s.rejected = shortcutSlot{}
			return true
		}
		a, ok := accelFromEvent(ev)
		if !ok {
			// 非法键：描红这一格，但什么都不存。Biu 是「mapKeyToElectronAccelerator
			// 返回 null 就不改」，我们多描一次红，好让用户知道「刚才那一下没录上」
			// 而不是「按了没反应」。
			s.rejected = slot
			return true
		}
		s.rejected = shortcutSlot{}
		// 🔴 **录成功也是一个收尾动作**：这一格不再等着录下一个键。
		//
		// 和上面那个 × 是同一个道理（见它那里的注释），而漏掉它的后果比 × 大得多：
		// `s.recording` 非零 = 窗口这一帧一个组合键都不登记（viewShortcuts），于是
		// **本次运行剩下的时间里六条命令全部失效**，而界面上没有任何一个字说得清
		// 为什么——那一格画着新键、带一圈主色描边，看着一切正常。
		//
		// 实测过的那条路：点「播放/暂停」那一格 → 按 ⌘⇧V → 走回首页 → 按 ⌘M 不响、
		// 按 ⌘P 不响、点进设置页也不响，要切到「常规设置」才碰运气恢复。
		s.recording = shortcutSlot{}
		s.applyShortcut(slot, string(a))
		return true
	})
}

// applyShortcut 存一个键；存不住就弹提示。
//
// 和 `applySetting` 一个道理：静默丢掉是这一页最坏的失败方式，用户以为改了、
// 下次打开又变回去，中间屏幕上什么都没发生过。
//
// 存住之后重新登记全局那一列：录的正是全局那一格时（`slot.global`）不登记的话，
// 用户在设置页上录的键在别的应用里压根不响——而录完就退出录制、界面上一切正常。
func (s *Shell) applyShortcut(slot shortcutSlot, accel string) {
	if err := s.keys.set(slot, accel); err != nil {
		s.addToast("快捷键没存上："+strings.TrimPrefix(err.Error(), "app: "), notice.Danger)
		return
	}
	s.syncGlobalShortcuts()
}
