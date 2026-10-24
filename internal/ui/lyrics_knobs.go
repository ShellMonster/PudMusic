// 右下角浮着的那一列旋钮：歌词偏移、字号，和「找歌词」（上游 `index.tsx:380-398`
// 那三个图标）。
//
// # 两条路各画一份
//
// 这一列现在画在**两处**：
//
//	主窗口「正在播放」那一页   `lyrics.go` 的 `viewLyrics`
//	满屏播放器那一屏           `fullplayer_lyrics.go` 的 `viewFullLyrics`
//
// 上游是同一份组件挂两处（`components/full-screen-player/index.tsx:361` 把
// `<Lyrics showControls>` 挂上去），所以满屏那一屏**本来就有**这三枚——
// 之前我们满屏这一屏一枚都没有（票 #166 / 报告 S-19）。
//
// ⚠️ **两处各记各的浮层状态**（`s.lyric` / `s.fullLyric`）和**各用各的键**
// （`lyricKnobPage` / `lyricKnobFull`）：满屏是浮在主布局上面的一层，底下那一层
// 照旧建，两处的旋钮会在同一帧里同时存在。
//
// # 第三个旋钮（找歌词）是 BMG-66 加进来的
//
// 上游那儿的第三个是一个放大镜，按下去开「歌词搜索弹窗」（`index.tsx:389-397`）。
// 它那一票（#21 的界面那一半）拆成了 BMG-66，两个弹窗在 `lyricssearch.go`。
// 这一票之前它是**故意不摆**的（歌词只从缓存来，按下去什么也不会发生）——现在
// 它有地方可去了。
//
// ⚠️ **它和上面那两个的可见条件不一样**：那两个画在没有歌词的时候不摆（无处可
// 附），它两种时候都摆——用户点它的时机恰恰是**没有歌词**的时候。
//
// # 夹界一个都不在这里
//
// 范围、步长、默认值全从底下那一层取（`app.MinLyricsOffsetMS` 等等），
// 而真正写进去的是 `app.SetLyricsOffset` / `app.SetLyricsFontSize`，**由它们
// 自己夹**。这一层算一遍、底下再夹一遍，两边有一天会对不上，而界面上看不出
// 是哪一边错了。
//
// # 为什么用 `SliderBase` 自己画，而不是 `Slider`
//
// 上游那两个都是竖着的滑块（`offset-control.tsx` / `font-size-control.tsx` 里
// 的 `orientation="vertical"`），而 **MyGo 的 `ui.Slider` 只会横着**——
// `.Vertical()` 对它是一个空操作（`mygo/ui/base.go:148` 的注释："Slider, drawn
// across, stays so"）。所以轨道和滑块自己画，`docs/ui/slider.md` 给的就是
// 这么画的例子。
//
// 拖动的手感、键盘（左右箭头、Home/End）、步长、无障碍，全是 `SliderBase` 自己
// 的：这一层只画那两块颜色。
package ui

import (
	"fmt"
	"strconv"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	// lyricKnobInset 是那两个旋钮离歌词区右边和下边多远，抄 `index.tsx:381` 的
	// `absolute right-6 bottom-6`（1.5rem = 24px）。
	lyricKnobInset = space.Px24

	// lyricKnobGap 是两个旋钮之间的缝，抄 `index.tsx:381` 的 `space-y-3`
	// （0.75rem = 12px）。
	lyricKnobGap = space.Px12

	// lyricKnobSize 是每一枚旋钮**多高**。
	//
	// ⚠️ **32 是上游那一枚圆钮的直径**（上游那三枚是 `rounded-full` 的 ⌀32 圆键，
	// 报告 S-19 实测）。我们这三枚是**带字的**（钮上写着当前值，见
	// `viewLyricKnobs` 顶上那一条），所以形状不是圆的——但**高度照上游那个数**：
	// 那一列于是和上游占一样的地方（顶沿 560、底沿 680、中心间距 44 =
	// 32 + `space-y-3` 的 12）。
	lyricKnobSize = 32.0

	// lyricPopoverGap 是浮层和它那个旋钮之间的缝，抄 `offset-control.tsx` /
	// `font-size-control.tsx` 里的 `offset={8}`。
	lyricPopoverGap = space.Px8

	// lyricSliderW 是竖滑块的宽（含留白）。上游那两个是 `w-1` 的轨道加一个圆钮，
	// 我们把整块做成钮那么宽。
	lyricSliderW = 24.0
	// lyricSliderThumb 是滑块那个圆钮的直径，`SliderBase` 要求「上下各留半个钮」
	// 才能把钮整个留在自己的框里（`mygo/ui/base.go` 里 `Vertical` 那段注释）。
	lyricSliderThumb = 20.0
	// lyricSliderTrack 是轨道那一条的宽。
	lyricSliderTrack = 4.0

	// lyricFontSliderH 是字号那条滑块的高，抄 `font-size-control.tsx` 里
	// `className="h-28"`（7rem = 112px）。
	lyricFontSliderH = 112.0
	// lyricOffsetSliderH 是偏移那条滑块的高，抄 `offset-control.tsx` 里
	// `className="h-32"`（8rem = 128px）。
	lyricOffsetSliderH = 128.0
)

// 这两个名字是给无障碍和测试用的：界面上摆的是「20px」这样的短标签，而
// `Tester.Find` 找的是这个（`mygo/ui/headless.go` 的 Find 找「画着这句文字
// 或者标着这个名字」的元素）。
const (
	lyricFontKnobName   = "歌词字号"
	lyricOffsetKnobName = "歌词偏移"
	// lyricKnobColumnName 是**那一列**的名字。
	//
	// ⚠️ **它给的是那一列（不是某一枚）**：无头 Tester 要量的是这一列在画面上
	// 的框——它挂在歌词区右下角，右沿 / 底沿都是上游那两个数（票 #166）。
	lyricKnobColumnName = "歌词旋钮"
)

// 浮层里那两行的键，和旋钮分开，免得两个元素抢同一个身份。
const (
	lyricFontSliderKey   = "lyrics:knob:font"
	lyricOffsetSliderKey = "lyrics:knob:offset"
	// lyricKnobColumnKey 是右下角那一列的键（铁律四）。
	//
	// ⚠️ **两条路上都要有**：有歌词时这一列摆三个旋钮、没有歌词时只摆一个，
	// 身份跟着换会让它「换了一列」而不是「少了一个旋钮」。
	lyricKnobColumnKey = "lyrics:knob:column"
)

// lyricKnobIDs 是「右下角那一列旋钮」这一族控件的**身份**。
//
// # 为什么满屏那一处要自己的一份
//
// 满屏播放器是**浮在主布局上面的一层**，底下那一层照旧建——于是「正在播放」
// 那一页的旋钮和满屏那一屏的旋钮会在**同一帧里同时存在**。键是按**父节点的
// id**算的（`mygo/ui/context.go` 的 `keyedID`），所以同一个键挂在两个不同的
// 父节点下面本来也不会撞；但浮层（字号 / 偏移那两条滑块）是在**浮层根**底下
// 建的，两处各建各的、靠「父节点不同」兜底——那件事太脆了。
//
// 所以两处各给一份：主窗口那一页一份，满屏那一屏一份（见 `lyricKnobPage` /
// `lyricKnobFull`）。
type lyricKnobIDs struct {
	// column 是那一列自己的键。
	column string
	// font / offset 是两个浮层里那两条滑块的键。
	font   string
	offset string
}

var (
	// lyricKnobPage 是主窗口「正在播放」那一页那一列旋钮的身份。
	lyricKnobPage = lyricKnobIDs{
		column: lyricKnobColumnKey,
		font:   lyricFontSliderKey,
		offset: lyricOffsetSliderKey,
	}
	// lyricKnobFull 是**满屏播放器**那一屏那一列旋钮的身份。
	//
	// ⚠️ **前缀是 `full:`**：满屏那一列是满屏自己的一族控件，和主窗口那一列
	// 不是同一件事（满屏的旋钮跟着满屏的歌词状态走，见 `s.fullLyric`）。
	lyricKnobFull = lyricKnobIDs{
		column: "full:lyrics:knob:column",
		font:   "full:lyrics:knob:font",
		offset: "full:lyrics:knob:offset",
	}
)

// viewLyricKnobColumn 画右下角浮着的那一列旋钮（主窗口「正在播放」那一页）。
//
// ⚠️ 它们是**绝对定位**的兄弟元素，不是 `ui.Overlay`：要贴着的是**歌词区**的
// 右下角，不是窗口的右下角——迷你播放器那一页底下还压着播放条，按窗口摆的话
// 旋钮会掉进播放条里。摆它们的那个壳在 `viewLyrics` 里（同一个 Column 的
// 绝对定位孩子）。
//
// adjustable 只管**上面那两个**（字号与偏移）。「找歌词」那一个两种时候都画：
// 用户点它的时机恰恰是**没有歌词**的时候，而没有歌词时那两个旋钮无处可附
// （见 lyrics.go 上面那一支）。
func (s *Shell) viewLyricKnobColumn(c *ui.Context, snap app.LyricsSnapshot, adjustable bool) {
	s.viewLyricKnobColumnWith(c, snap, adjustable, &s.lyric, lyricKnobPage)
}

// viewLyricKnobColumnWith 是那一列旋钮的**通用画法**（两处共用）。
//
// lv 是这一列跟着走的那份歌词状态（浮层开着没有记在它里面）：主窗口那一页是
// `s.lyric`，满屏那一屏是 `s.fullLyric`——两处各记各的，浮层才不会互相抢。
//
// ids 是这一列的身份（见 `lyricKnobIDs`）。
//
// 返回的是那一列本身：满屏那一屏要给它**自己的字色**（满屏的前景色，见
// `fullForeground`）——那一列压在糊过的封面上，主题的字色在浅色主题下是黑的。
func (s *Shell) viewLyricKnobColumnWith(c *ui.Context, snap app.LyricsSnapshot, adjustable bool, lv *lyricsView, ids lyricKnobIDs) ui.Element {
	col := ui.Column(c).Key(ids.column).Absolute().Right(lyricKnobInset).
		Bottom(lyricKnobInset).Gap(lyricKnobGap).AlignItems(ui.End).
		// 名字：这一组是「歌词旋钮」，读屏软件念得出；无头 Tester 也靠它量这一列
		// 在画面上的框（票 #166 要钉住它在歌词区右下角）。
		Label(lyricKnobColumnName)
	col.Children(func() {
		if adjustable {
			s.viewLyricKnobs(c, snap, lv, ids)
		}
		// 上游那一枚是 `isIconOnly` 的圆钮（`components/lyrics/index.tsx:389-397`），
		// 我们这一枚带着「找歌词」三个字——图标化会把这个动作的唯一说明
		// 一起拿掉，而它恰恰是**没有歌词时**唯一摆着的那个键。
		if flatButton(c, lyricSearchKnobLabel).Height(lyricKnobSize).Clicked() {
			s.openLyricSearch()
		}
	})
	return col
}

// viewLyricKnobs 画右下角浮着的字号与偏移两个旋钮。
func (s *Shell) viewLyricKnobs(c *ui.Context, snap app.LyricsSnapshot, lv *lyricsView, ids lyricKnobIDs) {
	// 字号在上面、偏移在下面（`index.tsx:382-388` 的次序）。
	//
	// ⚠️ **这一枚上留着数字**（「20px」「+50ms」）：上游那两个圆钮只画一枚图标
	// （`font-size-control.tsx:39-48`），当前值要打开浮层才看得到；我们把它写在
	// 钮上，用户不点开就知道现在调到了多少。要改的只是那层描边和常驻底。
	font := flatButton(c, lyricFontLabel(snap.FontSize)).Height(lyricKnobSize).
		Label(lyricFontKnobName)
	if font.Clicked() {
		lv.fontOpen = !lv.fontOpen
	}
	s.lyricFontPopover(c, font, snap, lv, ids)

	offset := flatButton(c, lyricOffsetLabel(snap.OffsetMS)).Height(lyricKnobSize).
		Label(lyricOffsetKnobName)
	if offset.Clicked() {
		lv.offsetOpen = !lv.offsetOpen
	}
	s.lyricOffsetPopover(c, offset, snap, lv, ids)
}

// lyricFontPopover 画字号那个浮层：一条竖滑块加一个当前值。
func (s *Shell) lyricFontPopover(c *ui.Context, anchor ui.Element, snap app.LyricsSnapshot, lv *lyricsView, ids lyricKnobIDs) {
	// ⚠️ **这里收的是存下来的那个字号，不是缩放后画出来的那个**：滑块的值、
	// `SetLyricsFontSize` 写回去的值、底下的标签都是同一个数。混进 TextScale 的话
	// 一次拖动会把 20 存成 26，下一帧实际画 32.5、拇指跳过去，而标签还写着 26
	//（评审在 1.25 倍下量到过）。
	v := float64(app.ClampLyricsFontSize(snap.FontSize))
	ui.PopoverBase(c, anchor, &lv.fontOpen, func(panel ui.Element) {
		lyricPanelStyle(c, panel, anchor)
		panel.Children(func() {
			sl := lyricSlider(c, ids.font, &v,
				app.MinLyricsFontSize, app.MaxLyricsFontSize, lyricFontStep,
				lyricSliderW, lyricFontSliderH)
			if sl.Changed() {
				// ⚠️ 不在这里夹：底下那一层自己夹（见本文件顶上那一句）。
				s.app.SetLyricsFontSize(float32(v))
			}
			ui.Text(c, lyricFontValueLabel(snap.FontSize)).
				FontSize(scale.Font(c, scale.LyricValue)).FontWeight(700).TextColor(c.Theme().TextMuted)
		})
	})
}

// lyricOffsetPopover 画偏移那个浮层。同上，一个竖滑块加一个当前值。
func (s *Shell) lyricOffsetPopover(c *ui.Context, anchor ui.Element, snap app.LyricsSnapshot, lv *lyricsView, ids lyricKnobIDs) {
	v := float64(snap.OffsetMS)
	ui.PopoverBase(c, anchor, &lv.offsetOpen, func(panel ui.Element) {
		lyricPanelStyle(c, panel, anchor)
		panel.Children(func() {
			sl := lyricSlider(c, ids.offset, &v,
				float64(app.MinLyricsOffsetMS), float64(app.MaxLyricsOffsetMS),
				float64(app.LyricsOffsetStepMS),
				lyricSliderW, lyricOffsetSliderH)
			if sl.Changed() {
				s.app.SetLyricsOffset(int(v))
			}
			ui.Text(c, lyricOffsetValueLabel(snap.OffsetMS)).
				FontSize(scale.Font(c, scale.LyricValue)).FontWeight(700).TextColor(c.Theme().TextMuted)
		})
	})
}

// lyricPanelStyle 给浮层上样子，并摆到旋钮的**左边**。
//
// ⚠️ 摆在左边而不是上游的右边（那两个 control 里的 `placement="right"`）：
// 旋钮本来就贴着歌词区的右边，往右摆会摆出窗口。框架自己在放不下的时候
// 会翻到另一边（`mygo/ui/element.go` 的 `AttachTo`），所以窗口窄了也摆得下。
//
// 上面的样式是抄框架 `ui.Popover` 自己那一份（`mygo/ui/widgets.go` 的
// `stylePanel`）：用 `PopoverBase` 是为了能改摆的位置，而 `Popover` 把位置写死
// 在下面。
func lyricPanelStyle(c *ui.Context, panel, anchor ui.Element) {
	t := c.Theme()
	// 外观走共享出口（`popupPanel`），只有位置和间距是这一处自己的。
	popupPanel(t, panel.
		AttachTo(anchor, ui.AnchorLeft, ui.AnchorRight).
		MarginEnd(lyricPopoverGap).
		Padding(t.Space(1.5)))
}

// lyricSlider 画一条竖着的滑块，值落在 [*lo, hi]、按 step 跳。
//
// `.Vertical()` 是**从下往上**的：lo 在底下、hi 在顶上（`mygo/ui/base.go` 里
// `Vertical` 那段注释）。上面那个圆钮画的位置跟着它走，所以 hi 在顶上这件事
// 对界面来说是对的方向（上游那两个也是往上加）。
func lyricSlider(c *ui.Context, key string, v *float64, lo, hi, step float64, w, h float32) ui.Element {
	t := c.Theme()
	thumb := float32(lyricSliderThumb)
	// ⚠️ 身份要在**建之前**给（`Context.Key`，不是 `Element.Key`）：滑块是个控件，
	// 它的状态在构造那一刻就初始化了，事后再 `.Key(...)` 当场 panic（框架那句
	// 报错写着 "use Context.Key before construction"）。
	s := ui.SliderBase(c.Key(key), v, lo, hi).
		Label(key).Step(step).Vertical().
		Size(w, h).PaddingY(thumb / 2)
	s.Draw(func(p *ui.Painter, r ui.Rect) {
		track := ui.Rect{
			X: r.X + (r.W-lyricSliderTrack)/2,
			Y: r.Y + thumb/2,
			W: lyricSliderTrack,
			H: r.H - thumb,
		}
		p.Fill(track, t.Border, lyricSliderTrack/2)
		at := float32((*v - lo) / (hi - lo))
		at = max(0, min(1, at))
		// lo 在底下：值越大，圆钮越靠上。
		cy := track.Y + track.H*(1-at)
		p.Fill(ui.Rect{X: r.X, Y: cy - thumb/2, W: r.W, H: thumb}, t.Accent, thumb/2)
	})
	return s
}

// lyricFontLabel 是旋钮上那一句，比如「20px」。
func lyricFontLabel(px float32) string {
	return strconv.Itoa(roundPx(px)) + "px"
}

// lyricOffsetLabel 是偏移旋钮上那一句，比如「+50ms」。
//
// ⚠️ 抄 `offset-control.tsx:15` 的 `formatLabel`：正数带一个 `+`，负数就是它自己
// （负号已经在那儿了）。少了那个加号，+50 和 −50 在这个小圆钮上读起来一样。
func lyricOffsetLabel(ms int) string {
	return lyricOffsetValue(ms) + "ms"
}

// lyricOffsetValue 是偏移那个数本身，带符号。
func lyricOffsetValue(ms int) string {
	if ms >= 0 {
		return "+" + strconv.Itoa(ms)
	}
	return strconv.Itoa(ms)
}

// lyricFontValueLabel 是字号浮层里那一句，比如「20 px」。
//
// ⚠️ 中间那个空格是**加的**：旋钮上写的是「20px」，这里要是也写「20px」，同一个
// 窗口里就有两句一模一样的话，测试按文字找元素会撞车，人看着也别扭。
// 上游不会撞车是因为它那个旋钮上只有一个图标。
func lyricFontValueLabel(px float32) string {
	return fmt.Sprintf("字号 %d px", roundPx(px))
}

// lyricOffsetValueLabel 是偏移浮层里那一句，比如「+50 ms」。
func lyricOffsetValueLabel(ms int) string {
	return lyricOffsetValue(ms) + " ms"
}
