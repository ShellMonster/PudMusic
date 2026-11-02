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
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/egoist/mygo/ui"
)

// miniCurtainLyricsKey 是歌词板里歌词滚动区那一块的键（铁律四）。
//
// ⚠️ **它不是 `miniCurtainAreaKey`**：那一块是**歌词板本身**（`viewMiniCurtain`
// 那个 Box），而这一块是**歌词区**（板里的滚动容器）——两个不同的东西，
// 两个不同的键。
const miniCurtainLyricsKey = "mini:curtain:lyrics"

// viewMiniCurtainLyrics 画歌词板里的歌词：**当前句居中**。
//
// viewH 是**这一帧歌词区有多高**（= 歌词板高）。
//
// ⚠️ **它必须正好等于歌词区实际的高**：上下那两段居中留白就是按它算的，
// 而「当前句在正中」这件事**全靠「留白 == 歌词区的高一半」**那个等式。
// 差一个像素，当前句就差一个像素。
func (s *Shell) viewMiniCurtainLyrics(c *ui.Context, snap app.LyricsSnapshot, active int, t *ui.Theme, viewH float32) {
	lv := &s.miniLyric
	// ⚠️ **歌词板自己那一档字号**：满屏那一屏用的是**用户偏好里的字号**
	//（12~48），而 320 宽里放 20px 的字（当前行 ×1.5 = 30px）一行只有十个
	// 字。这里传一份**改了字号的快照副本**进去。
	cur := snap
	cur.FontSize = miniCurtainFontSize
	st := lyricStyle{
		color: t.Text,
		muted: t.TextMuted,
		// ⚠️ **渐隐盖到 `t.Surface`**：歌词板的底是 `Surface`（半透明那一层），
		// 盖到 `t.Background` 的话，那条渐隐带子会是**另一个色**——一眼就看
		// 出「这一块不是一张板」。
		fade: t.Surface,
		area: miniCurtainLyricsKey,
		ns:   miniCurtainNS,
		// ⚠️ **每一行居中**：歌词板只有 320 宽，而「正在唱的那一句」是放大的
		// ——居中才看得出它是当前句。
		center: true,
		padX:   miniCurtainPadX,
	}

	scroll := ui.Scroll(c).Key(miniCurtainLyricsKey).Grow(1).FillWidth().
		TrackScroll(&lv.scroll)
	scroll.Children(func() {
		// 🔴 **上下各留半屏**：当前句**居中**——上面那段留白把第一句顶到
		// 中间（头几句滚得到中间），下面那段留白把最后一句托到中间（末尾
		// 那几句也滚得到中间）。和满屏那一屏是**同一张账**。
		//
		// ⚠️ **留白必须正好是歌词区这一帧的高一半**：差一个像素，当前句就
		// 差一个像素——而歌词板的高每帧跟着弹簧走，所以留白也得跟着变。
		content := ui.Column(c).Gap(lyricsLineGap).
			Padding(viewH/2, miniCurtainPadX, viewH/2, miniCurtainPadX).
			AlignItems(ui.Center)
		content.Children(func() {
			for i, row := range cur.Rows {
				line := s.lyricLine(c, lv, i, active, row, cur.FontSize, st)
				if i == active {
					lv.line = line.Bounds()
				}
			}
		})
	})
	// ⚠️ **上下两条渐隐**：和满屏那一屏是**同一张表**（`lyricsFadeTopStops` /
	// `lyricsFadeBottomStops`）——历史句从上沿淡出去，未来句从下沿淡出去。
	scroll.DrawOver(func(p *ui.Painter, r ui.Rect) {
		paintLyricFade(p, r, st.fade)
	})
	// ⚠️ **量的是滚动容器自己的框**（`Bounds()` 读的是上一帧的那一份）：它
	// 就是「歌词区有多高」那个数——而上面那两段留白必须跟它**一样高**。
	lv.box = scroll.Bounds()
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
