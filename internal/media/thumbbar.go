// 任务栏缩略图悬停时出现的那一条按钮：长什么样、什么时候可按、按了等于哪条命令。
//
// 这一半**不认识 Windows**：它只说「三个按钮、顺序是这个、播放中中间那个
// 是暂停」，把这份描述递给平台那一半（thumbbar_windows.go）去贴到窗口上。
// 能这么切是因为这一半的规则全是我们自己定的，不查系统任何一个头文件——
// 而规则错掉的后果（按了没反应、按钮在没歌的时候还能点）是那种**界面上完全
// 看不出来**的错，只能靠这里断。
//
// 它和 SMTC（`Windows 媒体会话`）是两套东西、两个位置：
//
//	SMTC      → 浮窗、锁屏、键盘媒体键、耳机
//	缩略图按钮 → 鼠标悬停任务栏按钮时，缩略图**下面**那一小条
//
// 两者都把命令交给同一个 handler（Session.dispatch），所以这一票里
// 「哪条路过来的」在业务层分不出来，也不需要分。
package media

// thumbbarCommandCount 是缩略图上一共有几个按钮。
//
// ⚠️ **别加。** Windows 的缩略图工具栏最多放 7 个，再多会被系统丢掉而不是
// 排第二行；真正能用的宽度也就 3 个（16px 一个，两侧还要留边）。第四个要来的
// 时候先想清楚是合并成「上一首/下一首」还是干脆放到 SMTC 那边。
const thumbbarCommandCount = 3

// thumbbarFirstID 是第一个按钮的命令 ID。
//
// 按钮 ID 是**固定分配**的：同一个位置永远是同一个数字，播放状态变了也不换。
// 系统回给我们的 `WM_COMMAND` 里带的就是这个数字（放在低位），拿它反查是哪
// 一个按钮——ID 一旦跟着状态变，就分不清那次按的是「暂停」还是「播放」了。
//
// 取 0x0200 起：低位 16 位是命令 ID，而 0x0001..0x0004 是 WM_COMMAND 里
// `HIWORD` 那个「通知码」的常用取值（BN_CLICKED 之类）。从 0x0200 起避开整片
// 区域，肉眼也更容易在日志里认出这是我们自己的。
const thumbbarFirstID = 0x0200

// thumbbarCommand 是「第几个按钮」——不是命令，是**位置**。
type thumbbarCommand int

// 三个位置。顺序就是它在缩略图上从左到右的顺序。
const (
	thumbbarPrev thumbbarCommand = iota
	thumbbarPlayPause
	thumbbarNext
)

// id 是这个位置的命令 ID。
func (c thumbbarCommand) id() uint32 {
	return uint32(thumbbarFirstID + int(c))
}

// commandFor 翻成业务层的命令。
//
// ⚠️ 中间那个按钮**按当前状态发 Play 或 Pause，不发 Toggle**：系统给回来的
// 就是「用户看到的那一个键被按了」，按钮上画着播放三角就该发 Play。发 Toggle
// 的话，状态和画面对不上的一瞬间（刚播完、还没刷新）会把歌按停——
// 那是「按了播放反而停了」，比按了没反应糟得多。
//
// at 只在 seek 上有意义，而缩略图上**没有 seek 键**（见 smtcValues.go）。
// 位置这条路上的时间轴是 SMTC 的事。
func (c thumbbarCommand) commandFor(playing bool) Command {
	switch c {
	case thumbbarPrev:
		return CommandPrev
	case thumbbarNext:
		return CommandNext
	}
	if playing {
		return CommandPause
	}
	return CommandPlay
}

// thumbbarCommandOfID 从系统回传的命令 ID 认出是哪个按钮。
//
// 认不出时给 (0, false)：那一段数字可能根本不是我们发出去的按钮（同一个
// 窗口上还有别的东西在用 WM_COMMAND），这时候回一条 CommandPlay 会让歌莫名其妙
// 开始放。
func thumbbarCommandOfID(id uint32) (thumbbarCommand, bool) {
	if id < thumbbarFirstID || id >= thumbbarFirstID+thumbbarCommandCount {
		return 0, false
	}
	return thumbbarCommand(id - thumbbarFirstID), true
}

// thumbbarButton 是某一个按钮在某一刻的样子。平台那一半照着它把按钮贴上去，
// 再照着它改（`ThumbBarUpdateButtons`）。整个结构是纯数据，能直接比较。
type thumbbarButton struct {
	// ID 是命令 ID，跟位置绑死（见 thumbbarCommand.id）。
	ID uint32
	// Command 是按下去等于哪条命令。
	Command Command
	// Glyph 是画哪个图标。播放/暂停那个位置的图标**跟着状态变**。
	Glyph thumbbarGlyph
	// Enabled 是能不能按。没歌的时候三个全灭。
	Enabled bool
}

// thumbbarButtons 是「现在这一刻」这一条按钮栏。
//
// 顺序固定为 上一首 / 播放·暂停 / 下一首，**不随状态重排**——系统按位置记住
// 按钮，用户看惯了一眼的顺序也在这里，重排会让三个图标在两次刷新之间跳位。
//
// hasTrack 是「有没有歌」。没有歌的时候不是「藏掉这一条」，而是**留在那里但是
// 全灭**：按钮凭空消失的话，用户把鼠标悬停过去什么都没出来，会以为是程序没反应。
func thumbbarButtons(hasTrack, playing bool) [thumbbarCommandCount]thumbbarButton {
	playGlyph := glyphPlay
	playCmd := CommandPlay
	if playing {
		playGlyph = glyphPause
		playCmd = CommandPause
	}
	return [thumbbarCommandCount]thumbbarButton{
		{ID: thumbbarPrev.id(), Command: CommandPrev, Glyph: glyphPrev, Enabled: hasTrack},
		{ID: thumbbarPlayPause.id(), Command: playCmd, Glyph: playGlyph, Enabled: hasTrack},
		{ID: thumbbarNext.id(), Command: CommandNext, Glyph: glyphNext, Enabled: hasTrack},
	}
}

// thumbbarGlyphOfID 是给「图标也换了」的路径准备的：同一个 ID 在播放中/暂停中
// 是两个图标，平台那一半更新按钮时要知道换哪一个。
func thumbbarGlyphOfID(id uint32, hasTrack, playing bool) (thumbbarGlyph, bool) {
	pos, ok := thumbbarCommandOfID(id)
	if !ok {
		return 0, false
	}
	return thumbbarButtons(hasTrack, playing)[pos].Glyph, true
}
