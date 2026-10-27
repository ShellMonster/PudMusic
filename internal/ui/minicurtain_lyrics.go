package ui

// 迷你播放器那个「歌词卷帘」里的歌词：**贴着下沿排**（票 #307 §②）。
//
// # 票面 §② 的原话
//
// 「卷帘里的歌词**贴着下沿排**（下轴在上、歌词在下轴之上），这样窗高变了，
// 歌词跟着下沿走 ⇒ 窗高永远等于内容高。」
//
// # 为什么另起一摊，而不是复用满屏那一屏的 `viewLyricLines`
//
// 满屏那一屏的歌词是**当前句居中**（上游 `index.tsx:369` 那份居中留白），
// 而这一屏要的是**贴着下沿**：**当前句的下沿**就压在**下轴的上沿**上，历史
// 那几句往上长。
//
// 而「居中」和「贴下沿」在**滚动位置**上是两件相反的事：
//
//   - 居中：当前句的**中心**在歌词区的中心；
//   - 贴下沿：当前句的**下沿**在歌词区的下沿。
//
// ⇒ 「滚到哪儿」那个算法不一样 ⇒ 那一段得知道这一屏是**哪一种**。而这一
// 摊还要另外做三件满屏那一屏不需要的事：
//
//  1. **内容顶上留一段空白**（`topPad`）：没有它的话，一首歌的头几句（内容
//     比歌词区矮）**滚不到下沿**——框架把滚动位置夹在 `[0, MaxY]`，而
//     `MaxY` 是 0 ⇒ 当前句停在**顶上**，不是下沿。
//  2. **下沿那条渐隐遮罩要去掉**：当前句就在下沿上，而那条遮罩正是「越靠
//     下沿越淡」——留着它，当前句就永远是淡的。
//  3. **歌词区的高度每一帧都在变**（卷帘的弹簧）⇒ 那段留白的高度必须**跟
//     着变**（`topPad` = 歌词区这一帧的高）。
//
// ⇒ 所以这里另起一摊（`viewMiniCurtainLyrics`），**满屏那一屏一个字都不
// 改**（`lyrics.go` 里 `viewLyricLines` / `lyricsView.centre` 都没动）。
//
// # 「窗高变了，歌词跟着下沿走」是怎么做到的
//
// 歌词区是**一个滚动容器**，而滚动容器的内容顶上有一段**等于歌词区高的
// 空白**（`topPad`）。于是：
//
//   - 内容 = [空白（一整屏高）][第一句]…[当前句]…；
//   - 滚动位置 = 「当前句的下沿」减去「歌词区的高」。
//
// 而 `topPad` **恰好等于**歌词区的高 ⇒ 那个滚动位置**与歌词区的高无关**
// ⇒ **窗长高的时候，滚动位置一动不动**，而内容顶上那段空白跟着长高 ⇒ 当前
// 句**一直贴着下沿**（下沿往下走，它也往下走）。
//
// ⚠️ **这就是「窗高 == 内容高」那条的算法版本**：内容永远是「一屏空白 + 词」
// 那一块，空白那一块把内容撑到**至少一屏高**，于是歌词区**没有空出来的一
// 块**——窗高变了，歌词区跟着变，卷帘的下沿（= 下轴）永远压着最后一句。

import (
	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/motion"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// miniCurtainLyricsKey 是卷帘里歌词滚动区那一块的键（铁律四）。
//
// ⚠️ **它不是 `miniCurtainAreaKey`**：那一块是**卷帘本身**（`viewMiniCurtain`
// 那个 Box），而这一块是**歌词区**（卷帘里的滚动容器）——两个不同的东西，
// 两个不同的键。
const miniCurtainLyricsKey = "mini:curtain:lyrics"

// viewMiniCurtainLyrics 画卷帘里的歌词：**贴着下沿排**（票 #307 §②）。
//
// viewH 是**这一帧歌词区有多高**（= 卷帘高 − 下轴那一道 14）。
//
// ⚠️ **它必须正好等于歌词区实际的高**：那段顶上留白（`topPad`）就是按它
// 算的，而「当前句一直贴着下沿」这件事**全靠「留白 == 歌词区的高」**那个
// 等式（见文件头那一节）。差一个像素，当前句就差一个像素。
func (s *Shell) viewMiniCurtainLyrics(c *ui.Context, snap app.LyricsSnapshot, active int, t *ui.Theme, viewH float32) {
	lv := &s.miniLyric
	// ⚠️ **卷帘自己那一档字号**：满屏那一屏用的是**用户偏好里的字号**
	//（12~48），而 320 宽里放 20px 的字（当前行 ×1.5 = 30px）一行只有十个
	// 字。这里传一份**改了字号的快照副本**进去。
	cur := snap
	cur.FontSize = miniCurtainFontSize
	st := lyricStyle{
		color: t.Text,
		muted: t.TextMuted,
		// ⚠️ **渐隐盖到 `t.Surface`**：卷帘的底是 `Surface`，盖到
		// `t.Background` 的话，那条渐隐带子会是**另一个色**——一眼就看出
		// 「这一块不是一张卡片」。
		fade: t.Surface,
		area: miniCurtainLyricsKey,
		ns:   miniCurtainNS,
		// ⚠️ **每一行居中**：卷帘只有 320 宽，而「正在唱的那一句」是放大的
		// ——居中才看得出它是当前句。
		center: true,
		padX:   miniCurtainPadX,
	}

	scroll := ui.Scroll(c).Key(miniCurtainLyricsKey).Grow(1).FillWidth().
		TrackScroll(&lv.scroll)
	scroll.Children(func() {
		// 🔴 **顶上留一段空白**（`topPad` = 歌词区这一帧的高）。
		//
		// ⚠️ **没有它，当前句就贴不到下沿**：一首歌的头几句（内容比歌词区
		// 矮）滚不下去——框架把滚动位置夹在 `[0, MaxY]`，而 `MaxY` 是 0。
		//
		// ⚠️ **它必须是「一整屏高」而不是「差一点」**：留白比歌词区矮的话，
		// 当前句还是差一截才够得着下沿。
		content := ui.Column(c).Gap(lyricsLineGap).
			Padding(viewH, miniCurtainPadX, space.None, miniCurtainPadX).
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
	// ⚠️ **只有上沿那条渐隐**（满屏那一屏是上下两条）：当前句就在**下沿**
	// 上，而下沿那条遮罩正是「越靠下沿越淡」——留着它，**当前句永远是淡的**。
	scroll.DrawOver(func(p *ui.Painter, r ui.Rect) {
		paintLyricFadeTop(p, r, st.fade)
	})
	// ⚠️ **量的是滚动容器自己的框**（`Bounds()` 读的是上一帧的那一份）：它
	// 就是「歌词区有多高」那个数——而上面那个 `topPad` 必须跟它**一样高**。
	lv.box = scroll.Bounds()
}

// miniCurtainLyricPlace 把「当前句贴着下沿」那个滚动位置算出来并下发
// （票 #307 §②）。
//
// ⚠️ **目标位置是「当前句的**下沿**」对「歌词区的下沿」**：
//
//	要滚的那一截 = 当前句离歌词区顶边多远 + 当前句有多高 − 歌词区有多高
//
// （而满屏那一屏是「当前句的**中心**」对「歌词区的中心」，见
// `lyricsView.centre`——那是**两件相反的事**。）
//
// ⚠️ **它与「歌词区有多高」无关**：顶上那段留白正好是**一整屏高**，于是那
// 一截算出来是「前头那几句有多高 + 当前句有多高」——窗长高，滚动位置**一
// 动不动**，当前句**一直贴着下沿**（见文件头那一节）。
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

	// 🔴 **当前句的**下沿**贴着歌词区的下沿**（票面 §②）。
	off := (lv.line.Y - lv.box.Y) + lv.line.H - lv.box.H
	aim := lv.aimOf(active, content, miniCurtainFontSize)

	if s.miniCurtain.lyricSnap {
		// ⚠️ **刚展开的那一下要「一次到位」**：展开之前歌词区压根没画过，
		// 滚动位置是**上一首歌**留下的（或 0）——从那儿慢慢滚过来，用户看到
		// 的是「当前句从屏幕外边爬上来」，而不是「卷帘放开，当前句就在下沿」。
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

// paintLyricFadeTop 在歌词区**上沿**盖一条渐隐遮罩。
//
// ⚠️ **它只有上沿那一条**：卷帘里的歌词**贴着下沿**（票 #307 §②），而当前
// 句就在**下沿**上——下沿那条遮罩（`paintLyricFade`）是「越靠下沿越淡」，
// 留着它**当前句永远是淡的**。
//
// ⚠️ **盖的是歌词区自己的底色**（`t.Surface`）：渐隐是「让歌词淡进背景里」。
func paintLyricFadeTop(p *ui.Painter, r ui.Rect, bg ui.Color) {
	top := r.H * lyricsFadeTop
	p.FillGradient(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: top}, lyricFade(bg, lyricFadeTopStops), 0)
}
