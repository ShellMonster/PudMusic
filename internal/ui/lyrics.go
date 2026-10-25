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
// 抄不来的那样，理由写在下面那一项上：`font-extrabold`（MyGo 的 `FontWeight`
// 能给 800，是有的——真正抄不来的是那层 `drop-shadow`）。
//
// `transition-all duration-300 ease-out` 是抄了的，走 `AnimateTo`（见
// `lyricLineAnim`）：每一行自己带一个 0/1 的「在唱」值，字号和透明度按它插值，
// 换句时两端各用 300ms 走完。**「正在唱的是第几句」仍然不带时间**——句号照旧
// 由 `lyrics.ActiveAt` 每一帧算出来，滚动、渐隐、译文、居中的依据全部还是那个
// 离散的句号；带上时间（过渡到了九成几）的只有这一行的**字号和透明度**。
//
// # 「滚到中间」这件事最容易错的地方
//
// 这一块难的不是功能，是三处一帧的滞后合起来之后**界面上看不出任何异样**：
//
//  1. `Element.Bounds()` 读的是**上一帧**量好的盒子（`mygo/ui/element.go:1382`
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
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/leading"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/motion"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	// lyricsAreaKey 是歌词区那一块的键（铁律四）。
	lyricsAreaKey = "page:lyrics:area"
	// lyricsEmptyKey 是「暂无歌词」那一块的键，和歌名那一行分开。
	lyricsEmptyKey = "page:lyrics"

	// lyricsActiveScale 是正在唱的那一句放大到几倍，抄 `index.tsx:335` 的
	// `fontSize: isActive ? fontSize * 1.5 : fontSize`。
	//
	// 过渡期间这个倍数是**插出来的**（见 `lyricLineAnim`）：换句那一刻并不是
	// 从 1 直接跳到 1.5，而是 300ms 走完中间那些倍。
	lyricsActiveScale = 1.5

	// lyricsIdleOpacity / lyricsActiveOpacity 抄 `index.tsx:333` 的
	// `isActive ? "opacity-100" : "opacity-60"`。
	lyricsIdleOpacity   = 0.6
	lyricsActiveOpacity = 1.0
	lyricsIdleWeight    = 400 // font-normal
	lyricsActiveWeight  = 800 // font-extrabold

	lyricsLineGap        = space.Px8  // `space-y-2`：0.5rem
	lyricsLinePadY       = space.Px8  // `py-2`：0.5rem
	lyricsPadX           = space.Px12 // 我们自己加的：上游那一句靠容器边，这里留一点
	lyricsMaxWidth       = 896        // `max-w-4xl`：56rem
	lyricsTranslationGap = space.Px4  // `mt-1`：0.25rem

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
// 「弹簧还在飞、让它飞完」。框架的默认是 0.001 DIP（`motion.go:75`），到这个
// 精度之后弹簧还要再真跑两秒才收手——而这两秒里用户拿滚轮翻了歌词，下一帧
// 就会重新对一次目标。0.05 DIP 是一根头发丝的误差，肉眼看不出来，到点就收手。
var lyricScrollAnim = ui.Animation{
	Spring: ui.SpringOptions{Duration: 500 * time.Millisecond, Precision: 0.05},
}

// 「窗口刚重新看得见」那一帧用的那一下：**一次到位**。
//
// ⚠️ **不能用 `ui.Animation{}`**：它的零值是 `Smooth` 那条 500ms 的弹簧
// （`mygo/ui/motion.go:33`），而那正好是票面不要的「慢慢追」。一个 1ns 的
// 曲线在下一帧就落到目标上（`channel.step` 里 `t >= 1` 直接取 target），
// 用户看到的是一次跳到位。
//
// ⚠️ **那条 1ns 的曲线是 `motion.Snap`**（值只写在 `widgets/motion`，见
// DESIGN.md §五《动效的值表》）——原先这里另写了一份一模一样的，票 #468
// 把它并回去了。

// lyricLineTransition 是「这一句从没在唱变成正在唱」（以及反过来）走多久，
// 抄上游 `index.tsx:330` 的 `transition-all duration-300 ease-out`。
const lyricLineTransition = 300 * time.Millisecond

// lyricLineAnim 就是那段过渡。
//
// ⚠️ **Ease 留空 = `ui.EaseOut`**（`mygo/ui/motion.go:17` 与 `widgets.go:98`），
// 那是框架那条三次曲线 `1-(1-t)³`。写成 `ui.Smooth`（零值）会变成 500ms
// 的弹簧——那是滚到中间那段用的东西，不是行样式这段。
//
// ⚠️ **它也不是上游那个 `ease-out`**：上游（CSS）那个是
// `cubic-bezier(0, 0, 0.58, 1)`，实算 t=0.25 时一条走 57.8%、另一条走 37.8%
// ——**两条不是一个东西**（框架里 `ui.EaseInOut` 与上游那条
// `cubic-bezier(0.4,0,0.2,1)` 的差别更大，见 `hoverzoom.go` 那儿记着）。
//
// ⚠️ **这里显式写了曲线**（票 #292）：全应用走**一条统一曲线**
// （`motion.Unified`），上游那个 `ease-out` 只是上游的默认值，不是这一处的
// 出处。时长 300ms 仍然抄上游。
//
// 「减少动态效果」不用在这里判：`AnimateTo` 自己读 `Preferences.ReduceMotion`，
// 为真时第一帧就落到目标上（`mygo/ui/motion.go:322-330`）。DESIGN.md 的
// 「减少动态效果」那一节要的就是这个。
var lyricLineAnim = ui.Animation{Duration: lyricLineTransition, Ease: motion.Unified}

// lyricPresenceKey 是每一行那个 0/1 的「在唱」值在节点上的键。
//
// 行元素本身已经有 `lyricLineKey`（跨帧稳定），所以这个键只要在**同一行**里
// 唯一就够了。用字符串而不是 `lyricLineKey` 那样的结构：行与行之间互不相干，
// 各自的动画值各记各的。
const lyricPresenceKey = "lyrics:line:presence"

// lyricLineKey 是每一句歌词的判重标识（铁律四）。
//
// 用**时间戳加行号**：时间戳单靠自己不唯一——两句完全同一时刻的话是有的，而
// 那正是该保留先后顺序的时候（见 `lyrics.Row` 的注释），把它们合成一句会丢掉
// 顺序。行号是位置，换一首歌整体换掉，跨歌撞不上。
//
// ⚠️ **ns 是「哪一处在画」**：迷你播放器那一页和满屏播放器那一屏画的是同一首歌的
// 同一份歌词，两边的行键要分开——不然同一个键在两棵树上各有一个节点，跨帧认人的
// 那一套会挑错一个（症状是「一首歌在两个地方显示，滚动的却是同一份」）。
type lyricLineKey struct {
	ns string
	at time.Duration
	i  int
}

// 两处歌词区各自的行键前缀。
const (
	// lyricNSPage 是迷你播放器那一页（`nowplaying.go`）。
	lyricNSPage = "page"
	// lyricNSFull 是满屏播放器那一屏（`fullplayer_lyrics.go`）。
	lyricNSFull = "full"
)

// lyricStyle 是歌词区那一屏的**画法**。
//
// # 它管的是「同一份歌词画在两个地方」的差别
//
// 迷你播放器那一页和满屏播放器那一屏画的是同一首歌的同一份歌词
// （`app.Lyrics()`），而两处的**字色**与**渐隐盖到的颜色**不一样：
//
//	迷你播放器那一页   字色是主题的正文色；渐隐盖到主题的底色（那一页底下
//	                   就是它自己的底）
//	满屏播放器那一屏   字色是满屏自己的前景色（白 92% / 黑 92%，跟着底走，
//	                   见 `fullForeground`）；渐隐盖到满屏的底（糊过的封面
//	                   压着 55% 的黑）
//
// 而画法本身（放大 1.5 倍、别的句 60% 的不透明度、译文叠在原词下面、上下渐隐、
// 正在唱的那一句滚到中间）两边**共用同一份代码**（`viewLyricLines`）——
// 抄一份的地方迟早会漂开，而漂开的那一处界面上看不出来。
type lyricStyle struct {
	// color 是歌词的字色。正在唱的那一句也是它，只是字号和不透明度不同。
	color ui.Color
	// muted 是**说明性文字**的字色（那一句「歌词来自 B 站」）。
	//
	// ⚠️ **它不是 `color` 的淡版**：满屏那一屏的字色是满屏自己的前景色
	// （白 92% / 黑 92%，跟着底走），而淡一档的那个是 `fullForeground` 给的
	// 第二档（见 `fullplayer_lyrics.go`）。用正文色画说明句的话，满屏那一屏
	// 上它是**最亮**的一行——比正在唱的那一句还抢眼。
	muted ui.Color
	// fade 是上下那两条遮罩渐隐到的颜色。
	fade ui.Color
	// area 是歌词区那一块的键（铁律四）。
	area string
	// ns 是这一块的行键前缀（见 `lyricLineKey`）。
	ns string
	// center 是「每一行**居中**」还是「每一行左对齐」。
	//
	// ⚠️ **迷你播放器那一页是左对齐**（一页那么宽，左对齐读起来是一张歌词纸），
	// **满屏那一屏跟着上游**：封面开着的时候左对齐（上游 `centered={!showCover}`），
	// 封面关掉的时候居中（那一栏是整窗宽，居中才不空）。
	center bool
	// padX 是歌词那一列左右那一份内边距（内容盒与每一行各一份）。
	//
	// ⚠️ **满屏那一屏给 0**：上游的歌词容器是 `px-12 py-24`，行本身**没有**左右
	// 内边距（`components/lyrics/index.tsx:331` 只有 `py-2`），于是左对齐的那些
	// 句**左沿就咬在内容盒的左沿**（1200 宽的窗口上是 x=648）。
	// 迷你播放器那一页给 `lyricsPadX`（12）——那一页是我们自己的排版（歌词纸）。
	padX float32
	// mask 是「按上游那条遮罩的语义，**逐行**淡出」那一档。
	//
	// ⚠️ **上游那条 `maskImage` 是按容器的上下沿淡的**（`index.tsx:357-360`：
	// `transparent 0% / black 24% / black 76% / transparent 100%`）——内容滑到
	// 边缘时被淡掉，滑到中间时是全亮的。
	//
	// ⚠️ **满屏那一屏不走「盖一层底色」那一支**（`lyrics.go` 的 `paintLyricFade`）：
	// 那一支是**盖**一层底色，而满屏的底是糊过的封面——盖出来的是背景上一个
	// 看得见的方块。框架没有遮罩那一层，于是这一档改成**逐行**按它在容器里的
	// 位置算不透明度（`lyricMaskAlpha`）：效果与上游那条遮罩一样，而背景一个
	// 像素都不动。
	//
	// ⚠️ **不是「离正在唱那一句多远」**：按那句算的话，滚过去以后**滚进视野的
	// 正是离它最远的那几句**，而它们会被算成「远」而全灭——用户看到的是一整列
	// 空白（评审抓到的那一条）。
	mask bool
	// bar 是正在唱那一句左边的强调色竖线（设计规范 v0.1 §7.5），只在左对齐时画。
	bar bool
	// overlay 是画在**滚动容器里面**的那一层（满屏那一屏右下角那三枚旋钮）。
	//
	// ⚠️ **它必须在滚动容器里面，不能是它的兄弟**：框架认「滚轮滚哪一层」是
	// 顺着**命中链**（指针底下的那个元素，再一层往上）找第一个能滚的——链上没有
	// 滚动容器，滚轮就什么也不做。挂在兄弟上时，链上是「旋钮列 → 歌词区」，
	// 滚轮落在旋钮列上（包括两枚之间的缝）就**滚不动歌词**了。
	//
	// ⚠️ **绝对定位的孩子不跟着内容滚**（`mygo/ui/layout.go` 的 `layoutScroll`：
	// 绝对定位的孩子按父节点的原点摆，不加滚动偏移），所以它挂在滚动容器里面
	// 也不会被歌词带着走。
	overlay func(c *ui.Context)
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

// lyricAim 是「滚到哪儿」这件事的**全部依据**：上一次我们让容器滚向的那个位置，
// 是由这几样一起定下来的。
//
// 🔴 **一样都不能少，而且少了哪一样都看不出来。** 只认 `active`（当前唱到第几句）
// 是不够的：用户拖一下字号滑块，句号没变、也不产生任何动画，于是这一层根本不发
// 指令，而内容高度与那份居中留白（`box.H/2 − 该句高/2`）已经整个变了——正在唱的
// 那一句被甩出歌词区，48px 时偏四百多像素、整句看不见，要等换到下一句才回来。
//
//	active   当前唱到第几句
//	content  这一份歌词的形状指纹（见 `lyricShapeOf`）
//	font     用户调过的字号
//	w        歌词容器有多宽
//
// 前两样加上 `font`、`w` 就是上游那个滚动 effect 的依赖表
// （`index.tsx:316` 的 `[activeIndex, centerPadding]`，而那份居中留白由
// `index.tsx:289` 的 `updateCenterPadding` 按字号、句数与窗口算出）。第五样
// `content` 是这一票自己加的：换一份歌词源可以**句数一样、句子长短不一样**
// （#21 的 `App.PutLyrics`），而句子长短就是那一行的高度。
//
// ⚠️ **容器高 `box.H` 不在里面，这是算出来的，不是漏的**：这一句的中心离容器
// 顶边是 `pad + Σ(前面各行) + 该行一半`，而 `pad = box.H/2 − 该行一半`，两处
// 抵消掉之后剩下 `box.H/2 + Σ` —— 那一项正好就是**容器自己的中心**。所以容器
// 变矮变矮都不改变「这一句偏不偏」，变的是 `MaxY` 能滚到哪儿，而那是框架自己在
// 夹（`mygo/ui/scroll.go:130`）。宽度不一样：它决定句子折几行，而折几行决定
// `Σ`，也就决定偏不偏。
type lyricAim struct {
	active  int
	content uint32
	font    int32
	w       int32
}

// aimOf 把这一帧的「该滚向哪儿」凑成一份。容器和字号取整再比：
//
// ⚠️ 取整是为了**不因为一根头发丝的抖动就每帧重发一次指令**。而不每帧重发是
// 「用户翻了歌词不会被拽回去」那一条的前提（见 `lyricsView.centre`）。
// 布局是确定的，同一个窗口大小下 `box.W` 每一帧同一个数——取整只是保险。
func (lv *lyricsView) aimOf(active int, content uint32, fontSize float32) lyricAim {
	return lyricAim{
		active:  active,
		content: content,
		font:    int32(math.Round(float64(fontSize))),
		w:       int32(math.Round(float64(lv.box.W))),
	}
}

// lyricShapeOf 是这一份歌词的「形状指纹」：每句多长、有没有译文、什么时候唱。
//
// 变了就说明**内容的高度**变了，目标就得重算——哪怕句号、句数、字号都没变。
// 换一份歌词源正是这一种：同一首歌，二十句还是二十句，可新那份的句子更长，
// 于是每一行都更高，正在唱的那一句被顶到了下面去。
//
// 代价是一首歌几百行、每行五个字的常数时间（`len` 是 O(1)），而它换来的是
// 「换了源之后正在唱的那一句还在中间」。
func lyricShapeOf(rows []lyrics.Row) uint32 {
	fp := uint32(2166136261) // FNV-1a 的起始值
	for _, r := range rows {
		fp = (fp ^ uint32(len(r.Text))) * 16777619
		fp = (fp ^ uint32(len(r.Translation))) * 16777619
		fp = (fp ^ uint32(r.At/time.Millisecond)) * 16777619
	}
	return fp
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
	// 布局末尾提交的那一份盒子（`mygo/ui/element.go:1382`）。拿它算目标和拿
	// `scroll.Y` 是同一帧的那一份，两者自洽——见 `lyricsView.centre`。
	box  ui.Rect
	line ui.Rect

	// key 是上面那两个框是给**哪一首歌**量的。换歌就整份作废（见 onSong）。
	key string

	// aim 是上一次我们让它滚向哪儿的那一组依据，见 `lyricAim`。
	aim lyricAim

	// aiming 是「刚刚因为依据变了要对一次，那一次的目标是拿旧框算的，还在等几何
	// 跟上」。**只用来补一下**（见 `lyricsView.centre` 里那一支），补完就清掉。
	aiming bool

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
	// `active: noLyric`（-1）永远不等于任何一次真的瞄过的依据，于是新的一首歌
	// 一画出来就会重新对一次目标。
	lv.aim, lv.aiming, lv.pend = lyricAim{active: noLyric}, false, 0
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
func (lv *lyricsView) centre(c *ui.Context, active int, content uint32, fontSize float32, snap bool) {
	if !lv.ready() {
		// 头一帧还没有框可量。这一帧不滚，但要一帧再来量。
		//
		// ⚠️ `pend` 是有预算的：不加的话「量不到」这个分支会一直要帧，而它要
		// 的那个东西（这一帧画出来的框）可能压根不会有。
		lv.pend++
		if lv.pend <= lyricMeasureFrames {
			// ⚠️ **这一句不能改成 `p.AnimationFrame()`**（票 #392 的那一批里，
			// 别处都改了，就这一处没改——**理由不是「忘了」，是它要的东西不同**）：
			// 局部重绘（`mygo/ui/runtime.go` 的 `repaintFrame`）**只重画、不
			// 重建**，于是**不重排**——而这一句要的是「**再排一遍**好量到歌词
			// 区和那一行的框」（那两个框是布局量出来的，见 `viewLyricLines`）。
			//
			// 局部重绘那一帧里量到的还是**上一帧**的框（空的），于是这一句要的
			// 那一帧永远不会来，歌词就一直滚不动。
			println("DBG AnimationFrame from lyrics.go")
			//
			// 而它**也不值得**改：这一句是**每次换歌最多三帧**（`pend` 有预算），
			// 省下来的三次整窗重建。
			c.AnimationFrame()
		}
		return
	}
	lv.pend = 0

	// 这一句离容器顶边多远（上一帧量到的），减去它该待的位置（容器高减去它自己
	// 那一半，上下各让一半），就是要多滚的那一截。
	off := (lv.line.Y - lv.box.Y) - (lv.box.H-lv.line.H)/2
	aim := lv.aimOf(active, content, fontSize)

	if snap {
		// 窗口刚从看不见变成看得见（票 #156）：**一次跳到位**，不重放那段滚过去
		// 的动画。不处理的话，看不见那段时间里唱到第几句都记着，恢复之后弹簧
		// 从旧位置一帧一帧往新目标追——而用户其实只是把窗口切回来，看到的应该
		// 是「现在唱到哪一句」，不是一段补放的滚动。
		//
		// 目标算法和下面那一支完全一样：几何是上一帧量到的，`ScrollTo` 的
		// `MaxY` 夹取也是同一套（见 `motion.Snap`）。
		lv.aim = aim
		lv.aiming = false
		lv.scroll.ScrollTo(0, lv.scroll.Y+off, motion.Snap)
		return
	}

	switch {
	case lv.scroll.Scrolling():
		// 弹簧还在飞：每一帧按**当下**的几何重新对一次。
		//
		// 这一支修两件都慢一帧的事：`ScrollTo` 拿**上一帧**的 `MaxY` 夹目标
		// （`mygo/ui/scroll.go:130`），刚换歌那一刻内容还没撑开，夹出来的目标会
		// 短一截；那份居中留白也是按上一帧的框算的。

	case aim != lv.aim:
		// 依据变了：换了一句、拖了字号、换了歌词源、或者改了窗口大小。
		//
		// ⚠️ **只认句号是不够的**，而且不够到看不出来：拖一下字号滑块既不改句号、
		// 也不产生任何动画，于是这一支压根不会走，而内容高度与那份居中留白已经
		// 整个变了——正在唱的那一句被甩出歌词区，48px 时偏四百多像素、整句看不见，
		// 要等换到下一句才回来。依据是哪几样，见 `lyricAim`。
		lv.aiming = true

	case lv.aiming:
		// 上一支刚因为依据变了动过，可那一下的 `off` 是拿**上一帧**的框算的——
		// 改动要再过一帧才反映到框上。于是补这一下：对完就不再追了。
		//
		// ⚠️ **必须是补一下而不是一直追**，两件事：
		//
		//  1. 补的那一下常常是个**空操作**（目标就等于当前位置），弹簧当场收手、
		//     `Scrolling()` 跟着变 false。不补这一下的话就没有「弹簧在飞」这一支
		//     可走，而几何还在慢一帧——于是拖完字号，正在唱的那一句就停在偏出去
		//     四百多像素的地方，直到换句。
		//  2. 只补一下，于是「用户翻了歌词会不会被拽回去」那一问的代价只有一帧。
		//     一直追的话，用户在拖完旋钮的那一两帧里滑一下滚轮，又会被拽回去。
		lv.aiming = false

	default:
		// 落定了，而且没有人在动它——什么都不做。
		//
		// ⚠️ **用户拿滚轮翻歌词走的也是这一支**：翻的时候依据一个字都没变，于是
		// 这里返回，歌词就停在用户翻到的地方。框架那边一收到滚轮就把弹簧收走
		// （`mygo/ui/input.go:555` 的 `s.track.glide = nil // the user takes over
		// from ScrollTo`），`Scrolling()` 跟着变 false，不用自己再记一份
		// 「用户是不是翻过了」。
		return
	}

	lv.aim = aim
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
		// ⚠️ **「找歌词」那个旋钮这一支也要画**：上游那三个旋钮不受「有没有歌词」
		// 影响（`index.tsx:380-398` 在歌词区的外层），而用户点它的时机恰恰是
		// **没有歌词**的时候。少画它的话 `noLyrics.Hint` 那句「在歌词搜索里换个源
		// 再找一次」就又变回一句兑现不了的承诺——而这一支正是它兑现的地方。
		s.viewLyricKnobColumn(c, snap, false)
		return
	}

	t := c.Theme()
	s.viewLyricLines(c, lv, snap, active, lyricStyle{
		color: t.Text,
		muted: t.TextMuted,
		fade:  t.Background,
		area:  lyricsAreaKey,
		ns:    lyricNSPage,
		padX:  lyricsPadX,
	})

	// 右下角浮着的那一列旋钮。⚠️ **偏移与字号画在没有歌词的时候不画**（传下去的
	// 那个 `false`）：没有歌词时它们无处可附，落盘那边也不写（见
	// `app.flushLyrics`），摆着两个按下去什么都不发生的旋钮比不摆更糟。而「找
	// 歌词」那一个两种时候都画（见 `viewLyricKnobColumn`）。
	s.viewLyricKnobColumn(c, snap, true)

	// ⚠️ 放在整棵树建完之后：它要用容器和正在唱那一句的框，而那两个是上面读到的。
	// snap = 「这一帧是窗口刚恢复可见的第一帧」，一次到位（见 centre 里那一支）。
	lv.centre(c, active, lyricShapeOf(snap.Rows), snap.FontSize, s.snapScroll)
}

// viewLyricLines 画歌词的本体：一个可滚动的列，逐行高亮，正在唱的那一句在中间。
//
// ⚠️ **它只画本体**：空状态（没有歌词时）和右下角那三个旋钮都**不画**——
// 那两样在迷你播放器那一页和满屏播放器那一屏不一样（前者是「暂无歌词」+ 三个
// 旋钮，后者是「暂无歌词」+ 一个「找歌词」），所以留给调用方。
//
// ⚠️ **这一块必须是一个 `Grow(1)` 的 Column，不能是 Box 里套一个 Row。**
//
// 绕进 Row 里的话，Row 的 `Grow(1)` 走的是**横**那条轴（Row 的主轴是横的），
// 它的高度就变成「由内容撑着」——而内容的高度里有那两份居中留白，留白又是
// 按容器高度算的：`pad = 高/2 - 一句/2`，于是 高 → pad → 内容更高 → 高更大，
// 一帧大一截，最后歌词区比窗口还高、顶上半截在屏幕外，而界面上看不出异样
// （放大、渐隐、译文全对，只有滚不到中间）。
//
// 直接 Column + `Grow(1)` 就对：主轴是竖的，高度由外层分给它，而框架**不会**
// 给 `Grow` 的那一个套上「不许比内容矮」这条下限（`mygo/ui/layout.go:720` 的
// `c.grow == 0`），所以内容再长也只是一个可滚动的容器。
func (s *Shell) viewLyricLines(c *ui.Context, lv *lyricsView, snap app.LyricsSnapshot, active int, st lyricStyle) {
	pad := lv.centerPad()

	ui.Column(c).Key(st.area).Grow(1).FillWidth().AlignItems(ui.Center).
		Children(func() {
			// 这一份歌词是**自动来的**还是**搜来的**：一句说明（见 lyricOriginText）。
			s.viewLyricOrigin(c, snap, st)
			// 上游那一条 `max-w-4xl` + 居中（`index.tsx:352-355`）：歌词不贴着
			// 窗口边，中间空出一段。
			scroll := ui.Scroll(c).Grow(1).FillWidth().MaxWidth(lyricsMaxWidth).
				TrackScroll(&lv.scroll)

			// ⚠️ `Bounds()` 读的是**上一帧**的盒子，而且必须在元素刚建好、
			// 还没进下一帧的时候读（元素本身过了这一帧就不再有效）。
			lv.box = scroll.Bounds()

			// 渐隐遮罩画在内容**之上**：`DrawOver` 在框架的绘制次序里排在子元素
			// 和滚动条之后（`mygo/ui/paint.go:328`），而画成绝对定位的兄弟元素
			// 会被容器的裁剪切掉。
			//
			// ⚠️ **满屏那一屏不画它**（它走 `mask` 那一档，理由见 `lyricStyle`）：
			// 这一层是**盖一层底色**，而满屏的底是糊过的封面——盖出来的是背景上一个
			// 看得见的方块。满屏那一屏改走**逐行按位置**算的不透明度（上游那条遮罩
			// 的语义），背景一个像素都不动。
			if !st.mask {
				scroll.DrawOver(func(p *ui.Painter, r ui.Rect) {
					paintLyricFade(p, r, st.fade)
				})
			}

			scroll.Children(func() {
				// ⚠️ `Padding` 四个值是「上、右、下、左」（`mygo/ui/element.go:722`
				// 的 CSS 那一套）。写成 `Padding(0, padX, pad, pad)` 的话上面那份
				// 居中留白跑到**左**边去了：第一句贴在顶上，而且越往下越往左缩。
				//
				// ⚠️ **每一行都收成字那么宽**（`AlignItems(ui.Start)`）：不收的话
				// 那一行永远是整列那么宽——**框**铺满，于是「量这一行有多亮」量到的
				// 是框里的一切（浮在上面的旋钮、别的控件…），而不是那一句字。满屏
				// 那一屏右下角浮着三枚旋钮（白的，236），框一铺满就量进那一行里去了。
				//
				// ⚠️ **`AlignItems(ui.Center)` 只在居中那一支给**：迷你播放器那一页
				// 的歌词是**左对齐**的（一页那么宽，左对齐读起来是一张歌词纸），
				// 而满屏那一屏的歌词**跟着上游**（封面开着时左对齐、关掉时居中）。
				// 居中的是**每一行**（不是那一列）：正在唱的那一句放大 1.5 倍，
				// 它在中间那一条上。
				content := ui.Column(c).Gap(lyricsLineGap).
					Padding(pad, st.padX, pad, st.padX).
					AlignItems(ui.Start)
				if st.center {
					content = content.AlignItems(ui.Center)
				}
				content.Children(func() {
					for i, row := range snap.Rows {
						line := s.lyricLine(c, lv, i, active, row, snap.FontSize, st)
						if i == active {
							lv.line = line.Bounds()
						}
					}
				})

				// 浮在歌词上面、但**在滚动容器里面**的那一层（满屏那一屏右下角
				// 那三枚旋钮）。理由见 `lyricStyle.overlay`。
				if st.overlay != nil {
					st.overlay(c)
				}
			})
			s.trackScrollbar(scroll)
		})
}

// lyricBarW 是当前句左竖线的宽度。
const lyricBarW = 3

// lyricOriginText 是那一句「这份歌词是**怎么来的**」。
//
// # 为什么要有这一句
//
// 歌词有两处来路：B 站自己带出来的（取流那一趟顺带回来的）和用户在「找歌词」
// 里搜来的。用户分不清这两者时，看到一份词会以为「这是应用找来的」——而他下一
// 次换一首歌时就会去点「找歌词」，尽管这一首**根本不用点**。
//
// ⚠️ **空串 = 不知道来路**（老缓存里没有那一栏，见 `lyrics.Entry`），那就不画
// 这一句——**不知道不该被说成「搜索来的」**，那句话是错的。
func lyricOriginText(origin string) string {
	switch origin {
	case lyrics.OriginBili:
		return "歌词来自 B 站"
	case lyrics.OriginSearch:
		return "歌词来自搜索"
	}
	return ""
}

// viewLyricOrigin 画那一句「这份歌词是怎么来的」。
//
// ⚠️ **它画在歌词区的顶上，在滚动容器外面**：放进去的话它会跟着词滚走，而用户
// 要看的是「这一份」（换歌之后它才变）。
//
// ⚠️ **它的宽度和左右留白都跟着歌词那一列走**（`lyricsMaxWidth` 与 `st.padX`）：
// 左对齐的那一份词是从那一列的左沿开始的，而居中的那一份是从中间开始的——
// 说明句要跟着一边儿，不然它会飘在词的上面正中，而词在左边。
//
// 🔴 **左右留白是歌词那一份的**两倍**：歌词的行有**两层**横向留白——内容列
// 自己的（`viewLyricLines` 里那个 `Padding(pad, st.padX, pad, st.padX)`）和每一行
// 自己的（`lyricLine` 的 `Padding(lyricsLinePadY, st.padX)`）——所以**字**是从
// 两倍留白之后开始的。说明句只加一层的话，它会比歌词的字偏左**一个 `padX`**
// （迷你播放器那页 12px、满屏那一屏 8px），而这一处的注释说的是「跟着一边儿」。
func (s *Shell) viewLyricOrigin(c *ui.Context, snap app.LyricsSnapshot, st lyricStyle) {
	text := lyricOriginText(snap.Origin)
	if text == "" {
		return
	}
	col := ui.Column(c).FillWidth().MaxWidth(lyricsMaxWidth).
		Padding(space.None, st.padX*2, space.None, st.padX*2).
		// 说明句与词之间留一道缝：它们是两件事，贴在一起看着像同一段。
		Margin(space.None, space.None, space.Px8, space.None)
	if st.center {
		col = col.AlignItems(ui.Center)
	} else {
		col = col.AlignItems(ui.Start)
	}
	col.Children(func() {
		ui.Text(c, text).FontSize(scale.Font(c, scale.Size12)).TextColor(st.muted)
	})
}

// lyricLine 画一句歌词，和叠在它下面的那句翻译。
//
// 返回这一句的元素：调用方拿它的 `Bounds()` 量出这一句有多高、在哪儿
// （`Bounds()` 是上一帧的，见 lyricsView.box 那一段）。
//
// active 是**正在唱的是第几句**（`-1` = 还没有哪一句到点）。它不只用来判「这一句
// 是不是在唱」：满屏那一屏还按「离它多远」把两头的行压暗（见 `lyricRamp`）。
//
// st 是这一屏的画法（字色、行键前缀、居中还是左对齐），见 `lyricStyle`。
func (s *Shell) lyricLine(c *ui.Context, lv *lyricsView, i, active int, row lyrics.Row, fontSize float32, st lyricStyle) ui.Element {
	line := ui.Column(c).
		Key(lyricLineKey{ns: st.ns, at: row.At, i: i}).
		Padding(lyricsLinePadY, st.padX)
	// ⚠️ **这一行不铺满**：铺满了那一行就永远是整列那么宽——**框**里除了那一句
	// 字还有别的东西（浮在上面的旋钮…），量「这一行有多亮」就量到了它们。
	// 内容那一列用 `AlignItems(ui.Start)`（左对齐）或 `AlignItems(ui.Center)`
	//（居中）摆这些行，于是每一行都收成字那么宽（见 `viewLyricLines`）。

	// 「这一句正在唱」这件事在这一行上是一个 0/1 的值，换句时由 300ms 的
	// ease-out 推过去（`lyricLineAnim`）。
	//
	// ⚠️ **第一次出现的那一行不会「长出来」**：`AnimateTo` 的通道是那一帧才建
	// 的，建的时候值就在目标上。所以换歌、第一次找到歌词时，整屏歌词一上来就是
	// 对的；会过渡的只有「已经画着的那一行被换成了唱/不唱」。
	//
	// ⚠️ `s.snapScroll`（窗口刚从看不见恢复过来的那一帧）**一次到位**：和滚到中间那
	// 一支是同一个理由（见 `centre` 里那一支）。窗口看不见那段时间里歌照常在
	// 走，恢复那一刻用户要看的是「现在唱到哪一句」，不是一段补放的放大/缩小。
	// 用 `Jump` 而不是一个 1ns 的 `AnimateTo`：`AnimateTo` 第一帧返回的还是旧值，
	// 而这一帧的布局正是要用新值——晚一帧的话，`centre` 拿到的框还是旧大小，
	// 那一下「一次到位」会停在差一截的位置上。
	//
	// ⚠️ 这里的 `active` 仍然是**离散**的那一位（`lyrics.ActiveAt` 的答案）：
	// 句号不带时间，滚动、居中、渐隐的依据也都不带。带时间的只有下面的字号和
	// 透明度——这一点是这一票刻意守的边界（见文件头）。
	target := float32(0)
	if i == active {
		target = 1
	}
	var presence float32
	if s.snapScroll {
		presence = line.Jump(lyricPresenceKey, target)
	} else {
		presence = line.AnimateTo(lyricPresenceKey, target, lyricLineAnim)
	}

	// 字号和透明度按 presence 插值。**字重不插**：上游的 `transition-all` 对
	// font-weight 也是离散的（字重没有中间值），类一换就跳过去；这里照它，
	// 用 `active` 直接取端点，而不是拿 presence 去四舍五入。
	size := fontSize * (1 + (lyricsActiveScale-1)*presence)
	opacity := float32(lyricsIdleOpacity) +
		(lyricsActiveOpacity-lyricsIdleOpacity)*presence
	weight := lyricsIdleWeight
	if i == active {
		weight = lyricsActiveWeight
	}
	// 满屏那一屏：这一行**在歌词区里的位置**决定它有多不透明（上游那条遮罩的
	// 语义，理由见 `lyricStyle.mask` 与 `lyricMaskAlpha`）。
	//
	// ⚠️ **读的是上一帧的位置**（`Bounds()` 就是上一帧的）：淡出跟着「这一行现在
	// 在容器的哪儿」走——行滑到边缘时被淡掉，滑到中间时全亮。
	if st.mask {
		opacity *= lyricMaskAlpha(lyricRelPos(lv.box, line.Bounds()))
	}
	if st.bar && presence > 0 {
		acc := c.Theme().Accent.Alpha(presence)
		line.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X, Y: r.Y + lyricsLinePadY, W: lyricBarW, H: r.H - 2*lyricsLinePadY}, acc, 1.5)
		})
	}
	return line.Opacity(opacity).Children(func() {
		ui.Text(c, row.Text).
			FontSize(scale.Font(c, size)).FontWeight(weight).LineHeight(leading.Lyrics).
			TextColor(st.color)
			// 译文不是每行都有（`lyrics.Rows` 里 Translation 为空的那种，见那个
			// 类型的注释），空的时候一个字都不画——留一个空行的话，上下两行
			// 译文之间的缝会突然比别处大一倍。
		if row.Translation != "" {
			ui.Text(c, row.Translation).
				Margin(lyricsTranslationGap, space.None, space.None, space.None).
				FontSize(scale.Font(c, scale.LyricsTranslation)).
				FixedLineHeight(lyricsTranslationLineHeight).
				Opacity(lyricsTranslationOpacity).
				TextColor(st.color)
		}
	})
}

// paintLyricFade 在歌词区上下各盖一条渐隐遮罩。
//
// 盖的是歌词区自己的底色（`t.Background`）：渐隐是「让歌词淡进背景里」，不是
// 「让歌词变透明露出底下的东西」——迷你播放器这一页底下没有别的东西。
//
// ⚠️ **满屏那一屏不走这里**（它走 `lyricRamp` 那一档）：那底下是糊过的封面，
// 盖一层底色会在背景上留下一个看得见的方块。
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
// ⚠️ 角度是 180：MyGo 的角度是**从向上顺时针**数的（`mygo/ui/element.go:546`），
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

// lyricMaskAlpha 是上游那条 maskImage 在歌词区里的某一处的 alpha。
//
// ⚠️ **它是「行在容器里的位置」→ 「那一行有多不透明」**：上游那条渐变是
// `transparent 0% / black 24% / black 76% / transparent 100%`（`index.tsx:357-360`），
// **内容滑到边缘时被淡掉，滑到中间时是全亮的**。
//
// ⚠️ **不是「离正在唱那一句多远」**：按那句算的话，用户滚过去以后**滚进视野的
// 正是离它最远的那几句**，而它们会被算成「远」而全灭——用户看到的是一整列空白
// （评审抓到的那一条）。
//
// 那两张「盖住的程度」的表（`lyricFadeTopStops` / `lyricFadeBottomStops`）就是上游
// 那条渐变换算过来的（见它们上面的注释：上游那八个色标逐段换成四个点），所以这里
// 取它的补——上游的 alpha 0 是透明、1 是不透明，我们的 cover 1 是盖住、0 是盖不住。
func lyricMaskAlpha(rel float32) float32 {
	switch {
	case rel <= 0 || rel >= 1:
		return 0
	case rel < lyricsFadeTop:
		return 1 - lyricCoverAt(lyricFadeTopStops, rel/lyricsFadeTop)
	case rel > 1-lyricsFadeBottom:
		return 1 - lyricCoverAt(lyricFadeBottomStops,
			(rel-(1-lyricsFadeBottom))/lyricsFadeBottom)
	default:
		return 1
	}
}

// lyricCoverAt 是那张「盖住的程度」的表在 at（0..1）处的值（线性插值）。
//
// ⚠️ **名字带 `lyric` 前缀**：`coverAt` 那个名字在 `likefixture_test.go` 里已经
// 有一个（播放条上那张封面的框），两个同名的东西在一个包里会撞。
func lyricCoverAt(stops []lyricFadeStop, at float32) float32 {
	if len(stops) == 0 {
		return 0
	}
	if at <= stops[0].at {
		return stops[0].cover
	}
	for i := 1; i < len(stops); i++ {
		if at <= stops[i].at {
			a, b := stops[i-1], stops[i]
			if b.at == a.at {
				return b.cover
			}
			return a.cover + (b.cover-a.cover)*(at-a.at)/(b.at-a.at)
		}
	}
	return stops[len(stops)-1].cover
}

// lyricRelPos 是这一行在歌词区里的相对位置（0 = 顶沿，1 = 底沿）。
//
// ⚠️ **读的是上一帧的盒子**（`Bounds()` 就是上一帧的），而这正是要的：淡出跟着
// 「这一行现在在容器的哪儿」走——行滑到边缘时被淡掉，滑到中间时全亮。
//
// ⚠️ **容器框也是上一帧的**（`lv.box`，见 `viewLyricLines`）——两者是同一帧的
// 同一份数据，自洽。
//
// ⚠️ **量不到的时候按「在中间」算**：第一帧那些行还没有盒子（`Bounds()` 是空的），
// 按「在顶沿」算的话它们一上来就是全透明的——于是歌词刚出现的那一帧是一片空白。
func lyricRelPos(box, line ui.Rect) float32 {
	if box.H <= 0 || line.H <= 0 {
		return 0.5
	}
	return (line.Y - box.Y) / box.H
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
