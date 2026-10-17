// 歌词区本体：逐行高亮、平滑滚到中间、上下渐隐遮罩、翻译叠在原词下面。
//
// # 这一层只画画
//
// 解析、缓存、偏移与字号的夹界、切歌先重置再恢复，全在底下那一层：
// `internal/service/lyrics`（LRC 解析 + 一首歌一个文件的缓存）与
// `internal/app/lyrics.go`（切歌顺序、`Min/MaxLyricsOffsetMS`、
// `Min/MaxLyricsFontSize`、`SetLyricsOffset` / `SetLyricsFontSize` 自己夹界、
// 500ms 防抖落盘）。界面上调值一律走 `App.SetLyricsXxx`，**不自己再夹一遍**——
// 夹界只有一个出处，界面抄一遍的话，两边有一天会对不上，而对不上的那一边是界面。
//
// 这一层连「哪一句正在唱」都不自己算：`lyrics.ActiveAt` 报的是第几句，
// 它和 `lyrics.Row` 都是底下那一层的东西。
//
// # 每一个数都抄上游，出处标在行边
//
// 上游是 `src/components/lyrics/index.tsx`（React + DOM）。这一层照它抄的是
// **数**和**结构**，画法换成 MyGo 原生控件：
//
//   - 放大 1.5 倍：`index.tsx:335` 的 `fontSize * 1.5`
//   - 上下渐隐：`index.tsx:357-360` 那条 `maskImage`
//   - 逐行之间的缝 / 每句上下的留白：`index.tsx:365` 的 `space-y-2` 与
//     `index.tsx:331` 的 `py-2`
//   - 译文 14px、离原词 4px、80% 不透明：`index.tsx:344` 的
//     `mt-1 text-sm text-white/80`
//
// 抄不来的两样，各有各的理由，都写在下面那一项上：`font-extrabold`（MyGo 的
// `FontWeight` 能给 800，是有的——真正抄不来的是那层 `drop-shadow`）与
// `transition-all duration-300`（字号和透明度是每一帧重新算出来的值，MyGo
// 不会自动给它们补一段动画；自己接一段 `AnimateTo` 是能做的，但那会让
// 「当前这一句是哪一句」这件事本身带上时间，而这一版**不**做——见
// `lyricActiveScale` 那一条）。
//
// # 「滚到中间」这件事最容易错的地方
//
// 这一块难的不是功能，是三处一帧的滞后合起来之后**界面上看不出任何异样**：
//
//  1. `Element.Bounds()` 读的是**上一帧**量好的盒子（`mygo/ui/element.go:1322`
//     的注释就写着 "the previous frame's"），而弹簧是在**布局里**跑的
//     （`mygo/ui/scroll.go` 的 `adoptScroll` 在 `place` 当中）。
//  2. `ScrollState.MaxY` 同样是布局里才写的，而 `ScrollTo` 拿它夹目标。
//  3. 于是第一帧量不到任何东西，第二帧量到的是还没滚过去的框。
//
// 三条合起来的后果是「歌词一帧都不动」，而放大、渐隐、翻译全都对，只有
// 「没滚到中间」那一样看不出来。所以下面那三段各自都标了它防的是什么，
// **别把它们合掉**：目标是「加上」不是「减去」、只在换句时发指令、
// 量不到时要一帧但有预算。
package ui

import (
	"math"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
	"github.com/egoist/mygo/ui"
)

const (
	// lyricsAreaKey 是歌词区那一块的键（铁律四）。
	lyricsAreaKey = "page:lyrics:area"
	// lyricsEmptyKey 是「暂无歌词」那一块的键，和歌名那一行分开。
	lyricsEmptyKey = "page:lyrics"

	// lyricsActiveScale 是正在唱的那一句放大到几倍，抄 `index.tsx:335` 的
	// `fontSize: isActive ? fontSize * 1.5 : fontSize`。
	lyricsActiveScale = 1.5

	// lyricsIdleOpacity / lyricsActiveOpacity 抄 `index.tsx:333` 的
	// `isActive ? "opacity-100" : "opacity-60"`。
	lyricsIdleOpacity    = 0.6
	lyricsActiveOpacity  = 1.0
	lyricsIdleWeight     = 400 // font-normal
	lyricsActiveWeight   = 800 // font-extrabold
	lyricsLineHeight     = 1.375
	lyricsLineGap        = 8   // `space-y-2`：0.5rem
	lyricsLinePadY       = 8   // `py-2`：0.5rem
	lyricsPadX           = 12  // 我们自己加的：上游那一句靠容器边，这里留一点
	lyricsMaxWidth       = 896 // `max-w-4xl`：56rem
	lyricsTranslationGap = 4   // `mt-1`：0.25rem

	// lyricsTranslationSize 是译文那一行的字号，抄 `index.tsx:344` 的 `text-sm`
	// （0.875rem = 14px）。**是死的**，不跟着原词那个字号走——上游的
	// `text-sm` 就是绝对值，12px 的歌词配 14px 的翻译是 Biu 自己的样子。
	lyricsTranslationSize = 14.0
	// lyricsTranslationLineHeight 是译文那一行的高度，抄 `text-sm` 自带的
	// `line-height: 1.25rem`（= 20px，绝对值）。原词那行用 `leading-snug`
	// （1.375 倍），译文那行没有 `leading-*`，所以是 20px 而不是 1.375 倍。
	lyricsTranslationLineHeight = 20.0
	// lyricsTranslationOpacity 抄 `index.tsx:344` 的 `text-white/80`。
	lyricsTranslationOpacity = 0.8

	// lyricsFadeTop / lyricsFadeBottom 是上下那两条遮罩各占歌词区多高，
	// 抄 `index.tsx:358` 那条 `maskImage`：`transparent 0% … black 24%` 是
	// 上沿，`black 76% … transparent 100%` 是下沿。
	lyricsFadeTop    = 0.24
	lyricsFadeBottom = 0.24

	// lyricMeasureFrames 是「量不到、再要一帧」的预算。头一帧必然量不到
	// （还没有上一帧），所以正常是 1；2 和 3 是给别的情况留的余量，而**必须有
	// 上限**：不加预算的话，只要歌词区这一帧压根没画出来（切页到一半、窗口
	// 还没摆好），它就会一帧一帧地要下去，而表现是「已经切走的那一页还在让
	// 窗口满速重画」。
	lyricMeasureFrames = 3

	// noLyric 是「还没有哪一句需要滚过去」（`want` 的初值）。
	noLyric = -1
)

// lyricScrollAnim 是滚到中间用的那一下动画。
//
// 抄上游 `index.tsx:314` 的 `scrollTo({ behavior: "smooth" })`——那里没有时长，
// 是平台自己那条缓动，而 MyGo 的 `ui.Smooth` 正是默认的那一支弹簧（500ms、
// 不回弹，`mygo/ui/motion.go:33`）。这里是在它上面**只**补一个 Precision。
//
// ⚠️ **必须给 Precision**，理由是这一层拿 `ScrollState.Scrolling()` 判
// 「弹簧还在飞、让它飞完」。框架的默认是 0.001 DIP（`motion.go:70`），到这个
// 精度之后弹簧还要再真跑两秒才收手——而这两秒里用户拿滚轮翻了歌词，下一帧
// 就会重新对一次目标。0.05 DIP 是一根头发丝的误差，肉眼看不出来，到点就收手。
var lyricScrollAnim = ui.Animation{
	Spring: ui.SpringOptions{Duration: 500 * time.Millisecond, Precision: 0.05},
}

// lyricLineKey 是每一句歌词的判重标识（铁律四）。
//
// 用**时间戳加行号**：时间戳单靠自己不唯一——两句完全同一时刻的话是有的，而
// 那正是该保留先后顺序的时候（见 `lyrics.Row` 的注释），把它们合成一句会丢掉
// 顺序。行号是位置，换一首歌整体换掉，跨歌撞不上。
type lyricLineKey struct {
	at time.Duration
	i  int
}

// lyricFadeStop 是渐变上的一个点：位置（在**那一条带子**里从 0 到 1）和盖住的
// 程度（1 = 完全盖住底下的歌词）。
//
// 🔴 **盖住的程度不是上游那条 mask 的 alpha，是 1 减它。**
// 上游 `index.tsx:357` 那条 `WebkitMaskImage` 的 alpha 0 是「藏起来」、1 是
// 「透出来」，而这一层画的是一块**盖板**。直接把 alpha 当成盖住的程度，两头
// 正好反过来，现象是：紧贴窗口上下边缘的歌词满色被硬切、条子内侧反而淡到
// 看不见，而两条带子各自在中间鼓一个包。
//
// 下面四行是上游那八个色标**逐段换算**过来的，换算过程：
//
//	上游（位置按整个歌词区算）  alpha  →  盖住   落在上/下沿哪一条里
//	transparent      0%          0   →  1.00    上沿   0/24   = 0.00
//	alpha 0.15       6%        0.15  →  0.85    上沿   6/24   = 0.25
//	alpha 0.5       12%        0.50  →  0.50    上沿  12/24   = 0.50
//	black           24%        1.00  →  0.00    上沿  24/24   = 1.00
//	black           76%        1.00  →  0.00    下沿  76/76…  = 0.00
//	alpha 0.5       88%        0.50  →  0.50    下沿 (88-76)/24 = 0.50
//	alpha 0.15      94%        0.15  →  0.85    下沿 (94-76)/24 = 0.75
//	transparent    100%         0   →  1.00    下沿 (100-76)/24 = 1.00
//
// 上下两段落在自己那条带子里的位置**不一样**（上沿 0.25/0.5，下沿 0.5/0.75），
// 而透明度正好相反——两段不能照抄成同一串数。
type lyricFadeStop struct {
	at    float32
	cover float32
}

// 上沿那条：盖住的程度从满到零。
var lyricFadeTopStops = []lyricFadeStop{
	{0, 1}, {0.25, 0.85}, {0.5, 0.5}, {1, 0},
}

// 下沿那条：盖住的程度从零到满。
var lyricFadeBottomStops = []lyricFadeStop{
	{0, 0}, {0.5, 0.5}, {0.75, 0.85}, {1, 1},
}

// lyricsView 是歌词区跨帧活着的那一摊。
//
// ⚠️ **只由 view 在主线程上读写**（铁律一）：下面那几样都不是锁保护的，因为
// 界面状态是主线程单点写入的，后台那一线只往 `App` 那边投递。
type lyricsView struct {
	// scroll 是歌词容器自己的滚动位置。**每个窗口一份**。
	scroll ui.ScrollState

	// box / line 是**上一帧**量到的：歌词容器、正在唱的那一句，各自在窗口里
	// 的哪个框。它们只在两个地方写：画完这一帧的元素之后读它的 `Bounds()`。
	//
	// ⚠️ 「上一帧」不是保守，是唯一能读到的：`Bounds()` 读的是框架在上一帧
	// 布局末尾提交的那一份盒子（`mygo/ui/element.go:1322`）。拿它算目标和拿
	// `scroll.Y` 是同一帧的那一份，两者自洽——见 `lyricsView.centre`。
	box  ui.Rect
	line ui.Rect

	// key 是上面那两个框是给**哪一首歌**量的。换歌就整份作废（见 onSong）。
	key string

	// want 是上一次我们让它滚向的是第几句。
	want int

	// pend 是「量不到、再要一帧」连着问了几次（见 lyricMeasureFrames）。
	pend int

	// offsetOpen / fontOpen 是右下角那两个浮层开着没有。只由 view 在主线程上
	// 读写，所以不需要锁（同 rateOpen、drawerOpen）。
	offsetOpen bool
	fontOpen   bool
}

// onSong 换歌时把量出来的那一摊整份作废。
//
// ⚠️ 顺序很重要：**先认出换歌了，再决定这一帧滚不滚**。认晚一帧的话，新歌的
// 头一帧会拿上一首的框去算目标，滚过去之后用户看到的是从中间开始的一段歌词。
func (lv *lyricsView) onSong(key string) {
	if lv.key == key {
		return
	}
	lv.key = key
	lv.forget()
	// 回到顶上。留着上一首的滚动量的话，新歌算出来的目标会是一个「在上一首
	// 那个位置上」的值——方向是对的（居中留白会把第一句往下带），但落点是错的。
	lv.scroll.X, lv.scroll.Y = 0, 0
}

// forget 把量到的几何擦掉。**没有歌词的时候每帧都调**（幂等）。
//
// 留着它们的话，下一首歌的头一帧会拿上一首的框去算目标，而那正是「切歌要清掉
// 上一首的歌词，不能残留」在界面上唯一可能残留的地方：歌词文本每一帧都是从
// 快照读的（残留不了），而**滚动到了哪儿**是要自己记着的。
func (lv *lyricsView) forget() {
	lv.box, lv.line = ui.Rect{}, ui.Rect{}
	lv.want, lv.pend = noLyric, 0
}

// centerPad 是内容上下那一份留白：半个歌词区减去正在唱那一句的一半高。
//
// 抄上游 `index.tsx:369-372` 的 `paddingTop/Bottom: centerPadding`。没有它的话
// 第一句永远在顶上（内容不够一屏时），而用户看着歌还没唱到第一句词就已经看到
// 了它——那一屏应该是空的。
func (lv *lyricsView) centerPad() float32 {
	if lv.box.H <= 0 || lv.line.H <= 0 {
		return 0
	}
	return max(0, lv.box.H/2-lv.line.H/2)
}

// ready 说的是「这一帧量得到那两个框」。量不到就是头一帧，或者那一帧根本没画。
func (lv *lyricsView) ready() bool { return lv.box.H > 0 && lv.line.H > 0 }

// centre 让正在唱的那一句滚到容器中间。
//
// # 目标是「现在加上多出来的那一截」，不是减去
//
// 往下滚是内容往上走：这一句现在在中间偏**下**，就要往下滚那么多，把它带上来。
// 写成减号的话目标越算越靠上、越算越够不着，最后被夹在 `MaxY` 那个端点上不动
// ——而放大、渐隐、翻译全对，只有「没滚到中间」那一样看不出来。
func (lv *lyricsView) centre(c *ui.Context, active int) {
	if !lv.ready() {
		// 头一帧还没有框可量。这一帧不滚，但要一帧再来量。
		//
		// ⚠️ `pend` 是有预算的：不加的话「量不到」这个分支会一直要帧，而它要
		// 的那个东西（这一帧画出来的框）可能压根不会有。
		lv.pend++
		if lv.pend <= lyricMeasureFrames {
			c.AnimationFrame()
		}
		return
	}
	lv.pend = 0

	// 这一句离容器顶边多远（上一帧量到的），减去它该待的位置（容器高减去它自己
	// 那一半，上下各让一半），就是要多滚的那一截。
	off := (lv.line.Y - lv.box.Y) - (lv.box.H-lv.line.H)/2

	// ⚠️ **只在换了一句、或者上一次那一下还在飞的时候才发指令。** 每帧都发的话，
	// 用户拿滚轮翻了歌词，下一帧就被拽回中间——歌词就翻不动了，而那一眼看过去
	// 像是「滚不动」。
	//
	// 用户一滚轮，框架就把那个弹簧收走了（`mygo/ui/input.go:490` 的
	// `s.track.glide = nil // the user takes over from ScrollTo`），
	// `Scrolling()` 跟着变 false，于是这里自动停手——不用自己再记一份
	// 「用户是不是翻过了」。
	//
	// 弹簧还在飞的时候再对一次，是修 MaxY 那一帧的滞后：`ScrollTo` 拿
	// **上一帧**的 `MaxY` 夹目标（`mygo/ui/scroll.go:129`），刚换歌那一刻内容
	// 还没撑开，夹出来的目标会短一截。每帧对一次，两帧之内就到了。
	if active == lv.want && !lv.scroll.Scrolling() {
		return
	}
	lv.want = active
	lv.scroll.ScrollTo(0, lv.scroll.Y+off, lyricScrollAnim)
}

// lyricNow 是「唱到哪一句了」的时刻：播放位置加上用户调过的偏移。
//
// 抄上游 `index.tsx:46` 的 `currentMs = currentTime * 1000 + offset`。偏移是加的：
// 正数 = 歌词整体往后挪，于是同一个播放位置落在**更早**的一句上（那一帧还没到，
// 高亮停在前一句），而那正是「我把歌词往后调一点」想要的。
func lyricNow(pos time.Duration, offsetMS int) time.Duration {
	return pos + time.Duration(offsetMS)*time.Millisecond
}

// viewLyrics 画歌词区。**只有真的有歌词才画本体**；没有的那一支走
// `viewEmpty` + `noLyrics`（那是 SPEC 点名的空状态，见 empty.go）。
//
// pos 是播放位置（`app.State.Position`）。
func (s *Shell) viewLyrics(c *ui.Context, snap app.LyricsSnapshot, pos time.Duration) {
	lv := &s.lyric
	// 🔴 认换歌在这一帧的最开头，早于任何一句决定。
	lv.onSong(snap.Key)

	active := lyrics.ActiveAt(snap.Rows, lyricNow(pos, snap.OffsetMS))
	if active < 0 {
		lv.forget()
		s.viewEmpty(c, lyricsEmptyKey, noLyrics)
		return
	}

	t := c.Theme()
	pad := lv.centerPad()

	// ⚠️ **这一块必须是一个 `Grow(1)` 的 Column，不能是 Box 里套一个 Row。**
	//
	// 绕进 Row 里的话，Row 的 `Grow(1)` 走的是**横**那条轴（Row 的主轴是横的），
	// 它的高度就变成「由内容撑着」——而内容的高度里有那两份居中留白，留白又是
	// 按容器高度算的：`pad = 高/2 - 一句/2`，于是 高 → pad → 内容更高 → 高更大，
	// 一帧大一截，最后歌词区比窗口还高、顶上半截在屏幕外，而界面上看不出异样
	// （放大、渐隐、译文全对，只有滚不到中间）。
	//
	// 直接 Column + `Grow(1)` 就对：主轴是竖的，高度由外层分给它，而框架**不会**
	// 给 `Grow` 的那一个套上「不许比内容矮」这条下限（`mygo/ui/layout.go:605` 的
	// `c.grow == 0`），所以内容再长也只是一个可滚动的容器。
	ui.Column(c).Key(lyricsAreaKey).Grow(1).FillWidth().AlignItems(ui.Center).
		Children(func() {
			// 上游那一条 `max-w-4xl` + 居中（`index.tsx:352-355`）：歌词不贴着
			// 窗口边，中间空出一段。
			scroll := ui.Scroll(c).Grow(1).FillWidth().MaxWidth(lyricsMaxWidth).
				TrackScroll(&lv.scroll)

			// ⚠️ `Bounds()` 读的是**上一帧**的盒子，而且必须在元素刚建好、
			// 还没进下一帧的时候读（元素本身过了这一帧就不再有效）。
			lv.box = scroll.Bounds()

			// 渐隐遮罩画在内容**之上**：`DrawOver` 在框架的绘制次序里排在子元素
			// 和滚动条之后（`mygo/ui/paint.go:278`），而画成绝对定位的兄弟元素
			// 会被容器的裁剪切掉。
			scroll.DrawOver(func(p *ui.Painter, r ui.Rect) {
				paintLyricFade(p, r, t.Background)
			})

			scroll.Children(func() {
				// ⚠️ `Padding` 四个值是「上、右、下、左」（`mygo/ui/element.go:675`
				// 的 CSS 那一套）。写成 `Padding(0, padX, pad, pad)` 的话上面那份
				// 居中留白跑到**左**边去了：第一句贴在顶上，而且越往下越往左缩。
				ui.Column(c).Gap(lyricsLineGap).Padding(pad, lyricsPadX, pad, lyricsPadX).
					Children(func() {
						for i, row := range snap.Rows {
							line := s.lyricLine(c, i, row, i == active, snap.FontSize)
							if i == active {
								lv.line = line.Bounds()
							}
						}
					})
			})
		})

	// 右下角浮着的两个旋钮。**画在没有歌词的时候不画**（`viewLyrics` 上面那一支
	// 直接 return 了）：没有歌词时偏移和字号无处可附，落盘那边也不写（见
	// `app.flushLyrics`），摆着两个按下去什么都不发生的旋钮比不摆更糟。
	ui.Column(c).Absolute().Right(lyricKnobInset).Bottom(lyricKnobInset).
		Gap(lyricKnobGap).AlignItems(ui.End).
		Children(func() {
			s.viewLyricKnobs(c, snap)
		})

	// ⚠️ 放在整棵树建完之后：它要用容器和正在唱那一句的框，而那两个是上面读到的。
	lv.centre(c, active)
}

// lyricLine 画一句歌词，和叠在它下面的那句翻译。
//
// 返回这一句的元素：调用方拿它的 `Bounds()` 量出这一句有多高、在哪儿
// （`Bounds()` 是上一帧的，见 lyricsView.box 那一段）。
func (s *Shell) lyricLine(c *ui.Context, i int, row lyrics.Row, active bool, fontSize float32) ui.Element {
	t := c.Theme()
	size := fontSize
	weight, opacity := lyricsIdleWeight, float32(lyricsIdleOpacity)
	if active {
		size *= lyricsActiveScale
		weight, opacity = lyricsActiveWeight, lyricsActiveOpacity
	}
	return ui.Column(c).
		Key(lyricLineKey{at: row.At, i: i}).
		FillWidth().Padding(lyricsLinePadY, lyricsPadX).Opacity(opacity).
		Children(func() {
			ui.Text(c, row.Text).
				FontSize(size).FontWeight(weight).LineHeight(lyricsLineHeight).
				TextColor(t.Text)
			// 译文不是每行都有（`lyrics.Rows` 里 Translation 为空的那种，见那个
			// 类型的注释），空的时候一个字都不画——留一个空行的话，上下两行
			// 译文之间的缝会突然比别处大一倍。
			if row.Translation != "" {
				ui.Text(c, row.Translation).
					Margin(lyricsTranslationGap, 0, 0, 0).
					FontSize(lyricsTranslationSize).
					FixedLineHeight(lyricsTranslationLineHeight).
					Opacity(lyricsTranslationOpacity).
					TextColor(t.Text)
			}
		})
}

// paintLyricFade 在歌词区上下各盖一条渐隐遮罩。
//
// 盖的是歌词区自己的底色（`t.Background`）：渐隐是「让歌词淡进背景里」，不是
// 「让歌词变透明露出底下的东西」——迷你播放器这一页底下没有别的东西。
func paintLyricFade(p *ui.Painter, r ui.Rect, bg ui.Color) {
	top := r.H * lyricsFadeTop
	bottom := r.H * lyricsFadeBottom
	p.FillGradient(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: top}, lyricFade(bg, lyricFadeTopStops), 0)
	p.FillGradient(
		ui.Rect{X: r.X, Y: r.Y + r.H - bottom, W: r.W, H: bottom},
		lyricFade(bg, lyricFadeBottomStops), 0)
}

// lyricFade 把一张「盖住的程度」表变成渐变。
//
// ⚠️ 角度是 180：MyGo 的角度是**从向上顺时针**数的（`mygo/ui/element.go:499`），
// 180 才是从上往下。写成 90 会横过来盖，而那一眼看过去是「歌词左右两头淡掉」。
func lyricFade(bg ui.Color, stops []lyricFadeStop) ui.LinearGradient {
	g := ui.LinearGradient{Angle: 180}
	for _, s := range stops {
		c := ui.Transparent
		if s.cover > 0 {
			c = bg.Alpha(s.cover)
		}
		g.Stops = append(g.Stops, ui.GradientStop{Color: c, At: s.at})
	}
	return g
}

// lyricFontStep 是字号滑块的步长，抄 `font-size-control.tsx:16` 那一段的
// `step = 1`。
//
// ⚠️ 偏移的步长**不在这里**：它是 `app.LyricsOffsetStepMS`，和另外三个端点一样
// 从底下那一层取（见 viewLyricKnobs）。这一条在上游是组件内部的一个局部常量，
// 底下那一层没有把它提上来，所以这里照抄并写死，测试钉住它。
const lyricFontStep = 1.0

// roundPx 是界面上显示字号用的那个取整。上游是 `Math.round(value)`（
// `font-size-control.tsx:72`），而 MyGo 的字号是 float32，直接印出来是
// 「20.000001px」。
func roundPx(px float32) int { return int(math.Round(float64(px))) }
