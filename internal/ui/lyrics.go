// 歌词区：逐行高亮 + 居中滚动 + 上下渐隐 + 翻译叠在原词下面，外加偏移和字号两个
// 控件。逐条照 Biu 的 `src/components/lyrics/index.tsx`（405 行）——**每一个数字
// 底下都标了它在上游的哪一行**，那些数都是死的：自己挑一个「看着更协调」的数，用户
// 在 Biu 里习惯的那一档就在这儿对不上了。
//
// 偏移 ±5000ms 步长 50、字号 12~48px 在 `internal/app/lyrics.go`——放业务层是
// 因为设置文件也是那一层管（见 app 的 Settings），两边同一套夹界。
//
// 🔴 本文件里的 view **不许**调 `c.SetTheme`：主题每帧开头装一次就够（铁律二，
// 见 theme.go）。页面 view 在自己开头照惯例再调一次 `s.applyFrameTheme`。
package ui

import (
	"fmt"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
	"github.com/egoist/mygo/ui"
)

const (
	// lyricsLinePadY 是每一行上下的留白，抄 `py-2`（index.tsx:331）。
	lyricsLinePadY = 8
	// lyricsLineGap 是行与行之间，抄 `space-y-2`（index.tsx:336）。给的是滚动区。
	lyricsLineGap = 8
	// lyricsPadX 是歌词左右留白。上游那边是 `max-w-4xl` 居中（index.tsx:355），
	// 这一层没做「限宽居中」——歌词区本来就是窄的一条，限宽只会让它更窄。
	lyricsPadX = 24

	// lyricsActiveScale 是正在唱的那行放大到几倍，抄 index.tsx:335 的 `fontSize * 1.5`。
	lyricsActiveScale = 1.5
	// lyricsIdleOpacity 是其余各行的不透明度，抄 index.tsx:333 的 `opacity-60`。
	lyricsIdleOpacity = 0.6

	// lyricsTranslationSize 是翻译行的字号。抄 index.tsx:344 的 `text-sm`——Tailwind
	// 的 `text-sm` 是**固定的 14px**，不跟着 fontSize 控件走。
	lyricsTranslationSize = 14
	// lyricsTranslationGap 是翻译行离原词的距离，抄 index.tsx:344 的 `mt-1`。
	lyricsTranslationGap = 4

	// lyricsFadePercent 是上下渐隐各占歌词区高度的百分之多少，抄 index.tsx:357-360
	// 那个 maskImage 里 24% / 76% 那两处。
	lyricsFadePercent = 24

	// lyricsStuckTries 是「目标够不着」时再试几帧。理由见 followActiveLyric。
	lyricsStuckTries = 8

	// lyricsCenteredPx 是「这一行已经在中间了」的容差（像素）。
	//
	// 差一像素不算偏：滚动是弹簧，落定的那一帧本来就可能差零点几，而这个数要是
	// 取 0，那条「已经在中间」的判断就永远不会成立，于是每帧都重新发一次指令。
	lyricsCenteredPx = 1.5

	// lyricsSettleFrame 是「还没对齐时」再要一帧的间隔（一帧，60Hz）。要它是因为
	// `Bounds()` 和居中留白都取自**上一帧**：「量出来 → 算出来 → 滚过去」要跨两三帧，
	// 而暂停时 `App.tick` 不再叫醒界面（它只在 `live` 时叫，见 app.go 的 tick），
	// 那两三帧没人要就永远不来——歌词停在顶上不动，而界面上看不出「它本来该滚到中间」。
	lyricsSettleFrame = 16 * time.Millisecond
)

// 渐隐条上四个色标的位置（占**这一条自己的高度**的几分之几）与**盖住的程度**
// （0 = 全透，1 = 完全遮住）。
//
// 上游那一条 maskImage 管上下两头，色标是按整个歌词区的高度给的：
// 0% 全透明 → 6% 0.15 → 12% 0.5 → 24% 全黑 → 76% 全黑 → 88% 0.5 → 94% 0.15 →
// 100% 全透明。
//
// 🔴 **CSS mask 的 alpha 0 是「藏起来」，alpha 1 是「原样透出来」**——不是反过来。
// 所以上游那条 mask 的意思是：**紧贴窗口上下边缘的歌词被藏起来，中间那段原样显示**。
// 而本文件画的是一块**盖板**（背景色的渐变条压在歌词上面），于是盖住的程度 =
// 1 - mask 的 alpha：
//
//	0% → 盖住 1.0（藏）    6% → 0.85    12% → 0.5    24% → 0（透）
//	76% → 0（透）         88% → 0.5    94% → 0.85  100% → 1.0（藏）
//
// **写反过一次**：当初把 mask 的 alpha 直接当成了盖板的 alpha，于是**紧贴窗口边缘的
// 歌词满色被硬切、条子内侧反而淡到看不见**——现象是「76% 处消失、99% 处又恢复满色」。
// 也就是说「上下有渐隐遮罩」这一条当时根本没做成，而那条测试还把倒过来的方向写成了
// 期望值（见 TestTheFadeFadesWhatScrollsUnderIt）。
//
// 上下两段的**位置不一样**：上沿的 6%/12% 落在自己那条的 0.25/0.5，下沿的
// 88%/94% 落在 0.5/0.75。透明度则正好相反——上沿从 1 降到 0，下沿从 0 升到 1，
// 因为两条的「贴窗口边缘那一头」分别在自己条子的**上**头和**下**头。
var (
	lyricsFadeTopAt    = []float32{0, 0.25, 0.5, 1}
	lyricsFadeTopAlpha = []float32{1, 0.85, 0.5, 0}

	lyricsFadeBottomAt    = []float32{0, 0.5, 0.75, 1}
	lyricsFadeBottomAlpha = []float32{0, 0.5, 0.85, 1}
)

// viewLyrics 是迷你播放器里那一块歌词。
//
// 🔴 外面包一层 Guard（铁律三）：view 里 panic 会一路穿出去**杀死整个进程**。
// 页面本身也包了一层（`Shell.page`），这里再包一层不是重复保护，是「这一块以后被
// 挪到别处也不会漏掉」——它读的是切片，索引一旦错位就是一次越界 panic。
func (s *Shell) viewLyrics(c *ui.Context, st app.State) {
	// 🔴 铁律二：页面 view 的头一件正事就是装主题（见 theme.go）。
	s.applyFrameTheme(c)

	Guard(func(c *ui.Context) { s.drawLyrics(c, st) }, s.onPanic)(c)
}

// drawLyrics 画歌词本体：没有歌词就是那一句空状态，有歌词就是滚动的那一列行
// 加上下渐隐，右下角浮着偏移和字号两个控件。
func (s *Shell) drawLyrics(c *ui.Context, st app.State) {
	ls := s.app.Lyrics()

	// ⚠️ 没有歌词时**只**画那一句空状态，别的一概不画。控件摆在没有歌词的屏幕上
	// 是两个按下去什么也不会发生的东西：用户点了没反应，只会以为自己点错了地方。
	// 上游那边控件是无条件摆的（`index.tsx:380` 的 `showControls`），可它的歌词
	// 是当场取回来的，不存在「这一屏取不到」这一种状态。
	if !ls.HasLyrics {
		// 一句都不说是不行的：「没有歌词」是一个**有前提**的状态，得先有歌在放，
		// 才谈得上「这首歌没有歌词」（见 empty.go 的 noLyrics）。
		s.viewEmpty(c, "page:lyrics", noLyrics)
		return
	}

	ui.Column(c).Key("lyrics").Grow(1).FillHeight().Children(func() {
		// 正在唱的是哪一行：播放位置**加上偏移**。正偏移 = 歌词整体往后挪。
		active := lyrics.ActiveAt(ls.Rows, st.Position+time.Duration(ls.OffsetMS)*time.Millisecond)

		// ⚠️ 换歌时把滚动位置和那一台跟随状态整个换掉：上一首的滚动量在新歌上
		// 毫无意义，而歌词区一上来就停在歌的中间看着像是「唱过了」——用户会以为
		// 自己拖过进度条。量出来的容器高/行高也一起扔掉（那是上一首歌的框）。
		if s.lyricKey != ls.Key {
			s.resetLyricFollow(ls.Key)
		}

		// box 要在自己的 Children 之后才拿得到，所以先声明再赋值——写成
		// `box := ui.Scroll(...).Children(func(){ ... box ... })` 在 Go 里
		// 编译不过（短变量声明的作用域从这一句**之后**才开始）。
		var box ui.Element
		box = ui.Scroll(c).Key("lyrics-scroll").TrackScroll(&s.lyricScroll).
			Fill().Padding(s.lyricPad(), lyricsPadX).
			// 行间距放在**滚动区**上而不是每一行上：每一行自己的 Gap 是原词和译文
			// 之间那 4px（`mt-1`），行与行之间那 8px（`space-y-2`）得由容器给。
			Gap(lyricsLineGap).Children(func() {
			s.drawLyricsLines(c, ls, active)
		})

		// Children 里已经把 lyricActiveEl 记成了正在唱的那一行。读它的盒子算
		// 「滚到哪儿正好居中」。
		s.lyricBox = box
		s.followActiveLyric(c, active)
		// 量下来的框和行高留给**下一帧**用（见 lyricPad 和测试）。
		// ⚠️ 存成几个数而不是留住那个元素句柄：`ui.Element` 一帧结束就作废了
		// （`ui/frame.go:100`），界面上没有哪一帧能再读它。
		if b := box.Bounds(); b.H > 0 {
			s.lyricBoxX, s.lyricBoxY = b.X, b.Y
			s.lyricBoxW, s.lyricBoxH = b.W, b.H
		}
		if h := s.lyricActiveEl.Bounds().H; h > 0 {
			s.lyricLineH = h
		}

		// 渐隐两条。**必须画在滚动区之后**：即时模式是按画出来的顺序盖上去的，
		// 画在前面的话歌词直接从遮罩底下透出来，渐隐就反了。
		s.drawLyricsFade(c, true)
		s.drawLyricsFade(c, false)
		// 控件**也在这一列里面**，而且**在渐隐之后**：它要浮在歌词上面（上游是
		// `absolute right-6 bottom-6`，index.tsx:381），画在渐隐之前的话自己先
		// 被盖住一半。
		s.drawLyricsControls(c, ls)
	})
}

// drawLyricsLines 画那一行行歌词。正在唱的那行放大到 1.5 倍并加粗，翻译叠在
// 原词下面。
func (s *Shell) drawLyricsLines(c *ui.Context, ls app.LyricsSnapshot, active int) {
	// 每一帧重来：这次没有歌词（或者没有那一句）时留着的必须是零值 Element，
	// 拿上一帧的句柄去 Bounds() 读到的是**上一首歌**的坐标。
	s.lyricActiveEl = ui.Element{}

	for i, row := range ls.Rows {
		on := i == active
		size := ls.FontSize
		color := c.Theme().Text
		if on {
			size *= lyricsActiveScale
		} else {
			color = color.Alpha(lyricsIdleOpacity)
		}

		// 🔴 铁律四：行的身份是「时间戳 + 这一行在原文里的序号」，不是序号本身。
		// 照抄上游的 key（`${line.time}-${index}`，index.tsx:326）：时间戳是歌词
		// 自带的，同一首歌里两次出现的副歌是**两句**，光用时间戳会撞成一行，
		// 而撞了的行在 Tester 里当场 panic、在真窗口里是两行状态串在一起。
		//
		// 序号补在后面是为了「换了一首歌而时间戳恰好撞上」这一种——那是另一个
		// 元素（换歌时整个滚动区换了），不会和这里撞。
		ui.Column(c).Key(lyricLineKey(i, row)).Grow(1).Padding(lyricsLinePadY, 0).
			Gap(lyricsTranslationGap).Children(func() {
			// 正在唱的那行加粗（`font-extrabold`，index.tsx:321）。
			line := ui.Text(c, row.Text).FontSize(size).TextColor(color)
			if on {
				line = line.Bold()
				s.lyricActiveEl = line
			}

			// 译文叠在原词下面。**只在有译文时画**：空译文那一行占着 4px 的缝，
			// 于是原词和下一句之间多出一条看不见的空隙。
			if row.Translation != "" {
				ui.Text(c, row.Translation).FontSize(lyricsTranslationSize).
					TextColor(c.Theme().TextMuted)
			}
		})
	}
}

// lyricLineKey 是第 i 行的行身份。见 drawLyricsLines 里铁律四那一段。
func lyricLineKey(i int, row lyrics.Row) string {
	return fmt.Sprintf("lyric:%d:%d", row.At.Milliseconds(), i)
}

// followActiveLyric 把正在唱的那一行滚到中间。
//
// 目标位置是从**上一帧的盒子**算的（MyGo 的 `Bounds()` 给的是上一帧的框，见
// `api_gen.go:2991`）：这一行离容器中心有多远（`off`），滚动量就往那边走多远。四条
// 规则：量不出来（第一帧）就再要一帧；`off` 已经在容差内就不动；弹簧还在飞就让它飞
// 完；都不满足才 `ScrollTo(当前滚动量 + off)`。
//
// ## 为什么只能拿 `off` 判断「到了没有」
//
// 滚动量 `Y` 和 `MaxY` 都靠不住，而**两处一帧的滞后（都实测踩过）**合起来的后果
// 是整块歌词一帧都不动、界面上看不出任何异样（放大、渐隐、翻译全对）：
//
//   - `off` 是上一帧量出来的，而那一帧的滚动量未必和现在这个是同一个数（差值本身
//     正是要滚的那个量）。
//   - 弹簧是在**布局里**跑的（`layout.go:130` 的 `place` 里调 `adoptScroll`），而
//     `MaxY` 是那之后才写的（`layout.go:142`）——夹取用的是**上一帧**的 `MaxY`。
//     拿当前这一帧的 `MaxY` 判「目标够不够得着」，会把「刚被夹在端点上」看成
//     「够得着却没到」。
//
// ⚠️ 目标是 `Y + off` 不是 `Y - off`：往下滚内容是往上走的，那一行在中间偏**下**
// （`off > 0`）时要往下滚那么多才能把它拉到中间。写成减号的话目标越算越靠上、越算
// 越够不着，最后被夹在端点上不动。
//
// ## 两种「别再跟着滚」
//
// 用户拿滚轮翻歌词（`lyricHeld`）时别抢回来，抢回来用户根本翻不动歌词；上游也是
// `useEffect(…, [activeIndex, centerPadding])`（`index.tsx:291`），那一句之间不再动。
// 发出去的指令**够不着**（`lyricStuck`）时也不能一遍遍重发：滚不动不说，还会把帧率
// 一直拖在 60 上，而画面上什么都看不出来。
//
// 认「是用户动的」只有一个办法：**弹簧没在飞、而滚动量停在一个既不是我们要的、也
// 不是任何一个夹取端点上的地方**。拿「上一帧的滚动量」比是不行的：容器采纳新滚动量
// 的那一帧两者是同一个数，下一帧才看得出变了，而那时上一帧的数已经被记成新的了。
func (s *Shell) followActiveLyric(c *ui.Context, active int) {
	line, box := s.lyricActiveEl.Bounds(), s.lyricBox.Bounds()
	if line.H <= 0 || box.H <= 0 {
		// 还没量出来。要下一帧的量，而这一帧未必还有下一帧——暂停时
		// `App.tick` 不再叫醒界面（它只在 `live` 时叫，见 app.go 的 tick），
		// 于是歌词就停在顶上不动。所以这里自己再要一帧。
		c.After(lyricsSettleFrame)
		return
	}

	off := line.Y + line.H/2 - (box.Y + box.H/2)
	y, maxY := s.lyricScroll.Y, s.lyricScroll.MaxY
	pad := s.lyricPad()

	// 换行了、留白变了（换歌 / 改字号 / 改窗口宽度），就重新从头试一遍。
	//
	// 内容变高（`MaxY` 变了）也要重新试：**够不着**常常只是因为内容还没量出来——
	// 头几帧的滚动范围比后来小得多，而「滚到那儿去」当时压根不是越界，是量少了。
	// 不重试的话歌词就停在半路上，而界面上看不出任何异样。
	//
	// ⚠️ 判「换行了」用的是 `lyricSeen` 而**不是** `lyricActive`：后者只在真的发出
	// ScrollTo 时才更新，而「够不着」那条路压根不会走到那儿。于是拿它判的话，
	// 那一支每帧都被清零再 +1，预算永远停在 1，`c.After` 每帧都发——**这道保险
	// 正好在它要防的那个分支上失效**（而失效的表现就是它本来要防的那个：帧率一直
	// 掉在 60 上，画面上什么都看不出来）。
	if active != s.lyricSeen || pad != s.lyricPadH || maxY != s.lyricMaxY {
		s.lyricStuck = 0
	}
	s.lyricSeen, s.lyricMaxY = active, maxY
	// 用户抢过去了吗。见函数头。弹簧自己在飞的时候不算——那是我们让它去的。
	if !s.lyricScroll.Scrolling() && y != s.lyricExpect && !atLyricEdge(y, maxY) {
		s.lyricHeld = true
	}

	switch {
	case s.lyricHeld && active == s.lyricActive:
		return // 用户在翻，不抢。唱到下一句再跟回去。
	case absf(off) <= lyricsCenteredPx:
		return // 已经在中间了。
	case s.lyricScroll.Scrolling():
		return // 弹簧在飞，让它飞完；下一帧重新量。
	}

	target := y + off
	if target < 0 || target > maxY {
		// 够不着，滚过去也只是被夹回来。还是要再试几帧——见函数头。
		if s.lyricStuck < lyricsStuckTries {
			s.lyricStuck++
			c.After(lyricsSettleFrame)
		}
		return
	}
	s.lyricScroll.ScrollTo(0, target, lyricsScrollAnim)
	s.lyricActive, s.lyricPadH = active, pad
	s.lyricExpect = target

	c.After(lyricsSettleFrame) // 没到位就再要一帧，见函数头
}

// atLyricEdge 报滚动量是不是停在夹取端点上（0 或 MaxY）。
//
// 「停在端点上」是**够不着**而不是「用户抢了」：容器把滚动量夹在 `[0, MaxY]`，
// 夹取的结果一定是这两个数之一，而用户滚去的地方通常在中间。
func atLyricEdge(y, maxY float32) bool {
	return y <= lyricsCenteredPx || y >= maxY-lyricsCenteredPx
}

// lyricsScrollAnim 是「滚到正在唱的那一行」用的那条动画。写成 `var` 而不是常量：
// `ui.Animation` 里是 `SpringOptions`，编译器不认它是常量表达式（实测 `const` 编译不过）。
var lyricsScrollAnim = ui.Animation{Spring: ui.SpringOptions{
	Duration: 500 * time.Millisecond, // ui.Smooth 的那个时长（`motion.go:38`）
	// 0.05 DIP：比上面那个「已经在中间」的容差（1.5）小三十倍，肉眼一个子像素都
	// 看不出来，而弹簧会在到达那一刻就收手。
	Precision: 0.05,
}}

// absf 是 |v|。差值比较用得到。
func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// resetLyricFollow 把那一台跟随状态整个换掉，换歌时调。
//
// 量出来的框（`lyricBoxY/H`、`lyricLineH`）也一起清：那是上一首歌的容器高，
// 留着的话新歌的头一帧会按旧高度算居中留白，等于又慢一拍。
func (s *Shell) resetLyricFollow(key string) {
	s.lyricKey = key
	s.lyricScroll = ui.ScrollState{}
	s.lyricActive, s.lyricSeen = -1, -1
	s.lyricExpect = 0
	s.lyricHeld, s.lyricStuck = false, 0
	s.lyricBoxX, s.lyricBoxY, s.lyricBoxW, s.lyricBoxH, s.lyricLineH = 0, 0, 0, 0, 0
}

// lyricPad 是滚动区上下留的那块空，用来让第一句和最后一句**也能滚到中间**。
//
// 没有它的话内容比容器短，滚动量被夹在 0，于是第一句永远贴着顶——「唱到第一句时
// 它没居中」看着就是歌词坏了。算式照上游 `updateCenterPadding`（index.tsx:251）：
// `(容器高 - 行高) / 2`；容器高和行高都是**上一帧**量到的，第一帧是 0、第二帧就对上
// 了。
//
// ⚠️ `lyricPadForced` 是给测试用的「替掉」入口，见下面那段。它默认是 nil，界面上一帧
// 都不看它。
func (s *Shell) lyricPad() float32 {
	if s.lyricPadForced != nil {
		return *s.lyricPadForced
	}
	return max(0, s.lyricBoxH/2-s.lyricLineH/2)
}

// lyricPadForced 让测试把居中留白强行按成某一个值。nil = 用真值。
//
// 它存在的理由只有一个：把「目标够不着」那个分支**稳定地**逼出来。居中留白平时是拿
// 上一帧的容器高和行高算的，于是只有第一帧才可能是 0——而那一帧过去就没了。想用界面
// 驱动去撞「够不着」，得同时猜对窗口尺寸、字号、歌词长度和滚动位置，实测撞不出来
// （试过 17 句歌词的首句/中段/末句、700x400 / 420x300 / 300x160 / 160x90、字号 12 与
// 48，全都没逼进去）。而那道「够不着就别一直要帧」的保险正好需要被测到。
//
// 和 `Shell.pageBody` / `autostart` / `picker` 是同一个形状：**做成字段是为了能替掉它**。
// drawLyricsFade 画上下两条渐隐。top 为真画上面那条，为假画下面那条。
//
// 上游是在滚动容器上挂一个 CSS mask（index.tsx:357-360），一条 maskImage 管到
// 上下两头。MyGo 没有 mask，就用两条盖在上面的渐变条——
// **渐变的终点色必须是歌词区的背景色**，否则「渐隐」渐隐到了一个窗口上没有的
// 颜色，那不是渐隐，是盖了一层。
func (s *Shell) drawLyricsFade(c *ui.Context, top bool) {
	bg := c.Theme().Background
	at, alpha := lyricsFadeBottomAt, lyricsFadeBottomAlpha
	if top {
		at, alpha = lyricsFadeTopAt, lyricsFadeTopAlpha
	}
	stops := make([]ui.GradientStop, 0, len(at))
	for i, pos := range at {
		stops = append(stops, ui.GradientStop{Color: bg.Alpha(alpha[i]), At: pos})
	}

	// 🔴 **必须 `PassThrough`**：即时模式的命中链是按画出来的顺序排的，这两条盖板
	// 画在滚动区**之后**，于是它们排在命中链的前面——歌词区上下各 24%（合计约一半
	// 面积）拿滚轮翻不动，只有中间那条有反应（实测：分界线正好落在 0.24 和 0.76）。
	// 自动跟随不受影响（那是程序自己调 ScrollTo），受影响的是用户手动翻歌词找词。
	bar := ui.Box(c).Absolute().Left(0).Right(0).HeightPercent(lyricsFadePercent).
		PassThrough().
		LinearGradient(ui.LinearGradient{Angle: 180, Stops: stops})
	if top {
		bar = bar.Top(0)
	} else {
		bar = bar.Bottom(0)
	}
}

// drawLyricsControls 是右下角浮着的偏移和字号两个控件。
//
// 上游是右下角一列（`absolute right-6 bottom-6`，index.tsx:381），两个都是点开一个
// 弹层、里面一根竖滑块。这里没有弹层，直接摆出来：控件一共两个，藏起来一次反而多点
// 一次。没歌词时它们**根本不画**（见 drawLyrics）。
func (s *Shell) drawLyricsControls(c *ui.Context, ls app.LyricsSnapshot) {
	t := c.Theme()

	ui.Column(c).Key("lyrics-controls").Absolute().Right(16).Bottom(16).
		Gap(4).Children(func() {
		offset := float64(ls.OffsetMS)
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "偏移").FontSize(11).TextColor(t.TextMuted).Width(28)
			in := ui.NumberInput(c, &offset,
				app.MinLyricsOffsetMS, app.MaxLyricsOffsetMS, app.LyricsOffsetStepMS)
			// ⚠️ Label 给的是**控件自己的名字**（无障碍念的），旁边那个「偏移」
			// 是给人看的字。两者不能是同一句，否则画面上出现两份。
			//
			// ⚠️ **不许给它定宽**。`ui.NumberInput` 是 `Shrink(0)` 的：定一个比内容
			// 窄的宽度，子控件（那个加号/减号按钮）会排到框外面去，按钮跑到页面的
			// 留白之外就点不到了——而画上它还在那儿，看着好好的（实测：点十一下
			// 偏移纹丝不动）。
			if in.Label("歌词偏移").Changed() {
				s.app.SetLyricsOffset(int(offset))
			}
			// 上游的 `formatLabel`（offset-control.tsx:16）：正数带个 + 号。
			ui.Text(c, lyricsOffsetLabel(int(offset))).FontSize(11).TextColor(t.TextMuted)
		})

		size := float64(ls.FontSize)
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "字号").FontSize(11).TextColor(t.TextMuted).Width(28)
			in := ui.NumberInput(c, &size,
				app.MinLyricsFontSize, app.MaxLyricsFontSize, 1)
			if in.Label("歌词字号").Changed() {
				s.app.SetLyricsFontSize(float32(size))
			}
			ui.Text(c, lyricsFontLabel(size)).FontSize(11).TextColor(t.TextMuted)
		})
	})
}

// lyricsOffsetLabel 是偏移那一格旁边的字，抄 `offset-control.tsx:68` 的
// “ `${ms >= 0 ? "+" + ms : ms}` ms “。
func lyricsOffsetLabel(ms int) string {
	if ms >= 0 {
		return fmt.Sprintf("+%d ms", ms)
	}
	return fmt.Sprintf("%d ms", ms)
}

// lyricsFontLabel 是字号那一格旁边的字，抄 `font-size-control.tsx:66` 的
// `Math.round(value)` + `px`。
func lyricsFontLabel(px float64) string { return fmt.Sprintf("%dpx", int(px+0.5)) }
