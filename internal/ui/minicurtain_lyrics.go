package ui

// 迷你播放器那块「歌词板」里的歌词：**当前句居中**。
//
// # 为什么另起一摊，而不是复用满屏那一屏的 `viewLyricLines`
//
// 满屏那一屏的歌词也是**当前句居中**（上游 `index.tsx:369` 那份居中留白），
// 而这一屏要的也是居中——那为什么不直接复用：
//
//  1. **字号**：满屏那一屏用**用户偏好里的字号**（12~48），而 320 宽里放
//     20px 的字一行只有十个字——这里传一份**改了字号的快照副本**；
//  2. **渐隐盖到的颜色**：满屏那一屏盖到**满屏自己的底**（糊过的封面），
//     这一块盖到 `t.Surface`（歌词板的底）；
//  3. **行键命名空间**：三处画的是**同一首歌的同一份歌词**，行键撞了跨帧
//     认人会挑错一个（`miniCurtainNS`，见 `minicurtain.go`）。
//
// ⇒ 所以这里另起一摊（`viewMiniCurtainLyrics`），**满屏那一屏一个字都不
// 改**（`lyrics.go` 里 `viewLyricLines` / `lyricsView.centre` 都没动）。
//
// # 历史：它曾经是「贴着下沿排」（票 #307 §②）
//
// 旧版要的是「当前句的下沿压在下轴的上沿上」——那是「卷轴」设计里的地轴
// 之上。换「独立歌词板」之后，那一套（顶上留一整屏的空白、只留上沿一条
// 渐隐、「下沿对下沿」的滚动算法）整个撤了：**当前句在歌词区正中**，上面
// 那几句是历史、下面那几句是未来，上下两条渐隐和满屏那一屏同一张表。

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/lyrics"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// miniCurtainLyricsKey 是歌词板里歌词滚动区那一块的键（铁律四）。
//
// ⚠️ **它不是 `miniCurtainAreaKey`**：那一块是**歌词板本身**（`viewMiniCurtain`
// 那个 Box），而这一块是**歌词区**（板里的滚动容器）——两个不同的东西，
// 两个不同的键。
const miniCurtainLyricsKey = "mini:curtain:lyrics"

// miniCurtainActiveWeight 是当前句的字重（原型 `.ly.cur` 是 700；系统字体里 600 与 700
// 同一档，且粗体只在刻度写明的几处——见 `TestOnlyTheScaleIsBold`）。
const miniCurtainActiveWeight = 600

// viewMiniCurtainLyrics 画卷帘里的歌词：**当前句居中**（原型 `.curtain .lyrics`）。
//
// viewH 是**这一帧歌词区有多高**（= 卷帘高 − 顶上那条 1 像素的线）。
//
// ⚠️ **它必须正好等于歌词区实际的高**：上下那两段居中留白就是按它算的，
// 而「当前句在正中」这件事**全靠「留白 == 歌词区的高一半」**那个等式。
// 差一个像素，当前句就差一个像素。
//
// # 样式（原型 `.curtain .ly`）
//
// 16 号、左对齐、每句上下各 6 的内边距；平时 `text-3`，当前句 `text`、放大
// 1.08 倍（以左边缘为原点）、加粗。上下两条渐隐盖到卡片的底。
//
// ⚠️ **不复用满屏那一屏的 `lyricLine`**：那一行是 1.5 倍放大 + 不透明度插值 +
// 800 字重（上游的样子，`lyrics.go` 归全屏播放器那一票），而这里要的是原型的
// 「变色 + 放大 1.08」。⇒ 另起一个 `miniCurtainLine`，`lyrics.go` 一个字都不动。
// 字体用系统正文字体：随包的宋体只有 Bold 一档，整屏歌词都用它会变成一屏粗体。
func (s *Shell) viewMiniCurtainLyrics(c *ui.Context, snap app.LyricsSnapshot, active int, t *ui.Theme, p tok.Palette, viewH float32) {
	lv := &s.miniLyric
	fade := s.coverWash(t, p.Surface)

	scroll := ui.Scroll(c).Key(miniCurtainLyricsKey).Grow(1).FillWidth().
		TrackScroll(&lv.scroll)
	scroll.Children(func() {
		// 🔴 **上下各留半屏**：当前句**居中**——上面那段留白把第一句顶到
		// 中间（头几句滚得到中间），下面那段留白把最后一句托到中间（末尾
		// 那几句也滚得到中间）。和满屏那一屏是**同一张账**。
		//
		// ⚠️ **留白必须正好是歌词区这一帧的高一半**：差一个像素，当前句就
		// 差一个像素——而卷帘的高每帧跟着弹簧走，所以留白也得跟着变。
		content := ui.Column(c).
			Padding(viewH/2, miniCurtainPadX, viewH/2, miniCurtainPadX).
			AlignItems(ui.Start)
		content.Children(func() {
			for i, row := range snap.Rows {
				line := s.miniCurtainLine(c, i, active, row, p)
				if i == active {
					lv.line = line.Bounds()
				}
			}
		})
	})
	// ⚠️ **上下两条渐隐**：和满屏那一屏是**同一张表**（`lyricsFadeTopStops` /
	// `lyricsFadeBottomStops`，原型 `.lyrics` 的 mask 18% / 82%）。
	scroll.DrawOver(func(pt *ui.Painter, r ui.Rect) {
		paintLyricFade(pt, r, fade)
	})
	// ⚠️ **量的是滚动容器自己的框**（`Bounds()` 读的是上一帧的那一份）：它
	// 就是「歌词区有多高」那个数——而上面那两段留白必须跟它**一样高**。
	lv.box = scroll.Bounds()
}

// miniCurtainLine 画一句歌词。
//
// 「这一句正在唱」在这一行上是一个 0/1 的值，换句时由 240ms 的 ease-out 推过去
// （原型 `.ly` 的 `transition: color var(--t-med), transform var(--t-slow)`）。
func (s *Shell) miniCurtainLine(c *ui.Context, i, active int, row lyrics.Row, p tok.Palette) ui.Element {
	line := ui.Column(c).Key(lyricLineKey{ns: miniCurtainNS, at: row.At, i: i}).
		Padding(miniCurtainLinePadY, space.None)
	target := float32(0)
	if i == active {
		target = 1
	}
	var presence float32
	if s.snapScroll {
		// 窗口刚从看不见恢复过来的那一帧**一次到位**：用户要看的是「现在唱到哪一句」，
		// 不是一段补放的变色 / 放大。
		presence = line.Jump(lyricPresenceKey, target)
	} else {
		presence = line.AnimateTo(lyricPresenceKey, target, ui.Animation{Duration: motion.Med, Ease: motion.EaseOut})
	}
	col := p.Text3.Mix(p.Text, presence)
	// ⚠️ **放大走字号（布局），不走 pose**：pose 的缩放以元素中心为原点，而这些行是
	// 左对齐的——放大会把左边缘往外推；原型是 `transform-origin: left center`。字号
	// 随 presence 插值，居中算法本来就是按当下的框算的。
	size := miniCurtainFontSize * (1 + (miniCurtainActiveScale-1)*presence)
	return line.Children(func() {
		t := ui.Text(c, row.Text).FontSize(scale.Font(c, size)).
			FixedLineHeight(miniCurtainLineH).TextColor(col)
		if i == active {
			t.FontWeight(miniCurtainActiveWeight)
		}
	})
}

// miniCurtainLyricPlace 把「当前句居中」那个滚动位置算出来并下发。
//
// ⚠️ **目标位置是「当前句的**中心**」对「歌词区的中心」**：
//
//	要滚的那一截 = 当前句离歌词区顶边多远 + 当前句有多高的一半 − 歌词区有多高的一半
//
// （和满屏那一屏的 `lyricsView.centre` 是**同一个算法**——只是这里直接
// 算 `off`，不走那一层结构体。）
func (s *Shell) miniCurtainLyricPlace(c *ui.Context, active int, content uint32) {
	lv := &s.miniLyric
	if !lv.ready() {
		// 量不到（头一帧 / 刚展开）：不滚，但要一帧再来量。
		//
		// ⚠️ **有预算**：不加的话「量不到」这一支会一直要帧，而它要的东西
		// 可能压根不会有（见 `lyricMeasureFrames`）。
		lv.pend++
		if lv.pend <= lyricMeasureFrames {
			c.AnimationFrame()
		}
		return
	}
	lv.pend = 0

	// 🔴 **当前句的中心对歌词区的中心**（票面：居中）。
	off := (lv.line.Y - lv.box.Y) + lv.line.H/2 - lv.box.H/2
	aim := lv.aimOf(active, content, miniCurtainFontSize)

	if s.miniCurtain.lyricSnap {
		// ⚠️ **刚展开的那一下要「一次到位」**：展开之前歌词区压根没画过，
		// 滚动位置是**上一首歌**留下的（或 0）——从那儿慢慢滚过来，用户看到
		// 的是「当前句从屏幕外边爬上来」，而不是「歌词板放开，当前句就在正中」。
		s.miniCurtain.lyricSnap = false
		lv.aim = aim
		lv.aiming = false
		lv.scroll.ScrollTo(0, lv.scroll.Y+off, motion.Snap)
		return
	}

	switch {
	case lv.scroll.Scrolling():
		// 滚动弹簧还在飞：每一帧按**当下**的几何重新对一次。

	case aim != lv.aim:
		// 依据变了：换了一句、换了歌词源、或者改了窗口大小。
		lv.aiming = true

	case lv.aiming:
		// 上一支刚因为依据变了动过，可那一下的 `off` 是拿**上一帧**的框算的
		// ——改动要再过一帧才反映到框上。于是补这一下：对完就不再追了。
		lv.aiming = false

	default:
		// 落定了，而且没有人在动它——什么都不做。
		//
		// ⚠️ **用户拿滚轮翻歌词走的也是这一支**：翻的时候依据一个字都没变，
		// 于是这里返回，歌词就停在用户翻到的地方（框架那边一收到滚轮就把
		// 弹簧收走）。
		return
	}
	lv.aim = aim
	lv.scroll.ScrollTo(0, lv.scroll.Y+off, lyricScrollAnim)
}
