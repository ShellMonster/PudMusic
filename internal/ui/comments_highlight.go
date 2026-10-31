// 评论正文里「命中歌名」那一段的高亮（票 #684）。
//
// # 为什么高亮只在这一页有
//
// 评论页的「只看提到歌名的评论」那一枚开关（票 #574）把没提到歌名的评论筛掉
// 了——而用户看着筛出来的这几条，会想知道**它们到底哪儿提到了这首歌**。尤其
// 是 `mv` 那一档：正文一大段，歌名藏在某个角落，光看文字要自己找。
//
// ⇒ 筛选开着的时候，正文里命中歌名的那些字染上主色 + 一层淡主色底（和搜索
// 结果那一种形状一致，`mygo/ui/richtext.go` 的 `Span.Background`）。
//
// # 一条判据：高亮是**装饰**
//
// 🔴 **它不许改正文的内容**：给辅助技术和无头 Tester 的那一份（`commentBody.plain`）
// 一个字符都不动——切 span 只是画法，`plain` 还是那一条评论的原文（表情占位符
// 换成「［表情］」，理由见 `commentSpans`）。
//
// # 配色：淡主色底 + 主色字，两样都从 `t.Accent` 现算
//
// 主色是用户可设的（设置页能改），所以这里**没有一个写死的绿**：
//
//	底   `t.Accent.Alpha(0.18)`——20% 是上游 HeroUI `bg-primary/20` 那个数
//	     （见 `playingRowBg`），这里取 18% 是让底色更轻一档：正文是**连续几行
//	     的字**，一整片 20% 的底色会把那一段读成一枚按钮。
//	字   `ink.Ensure(t.Accent, 底, 4.5)`——主色当字压在淡主色底上，浅色下
//	     那个原色只有 1.6:1 上下（同一个结论见 `playingRowText` 的注释）。
//
// ⚠️ **底是半透明的、字是实色的**：底压在半透明的底色上，用户换了深浅两套
// 之后它跟着变——所以字色得按**合出来的那个底**算，不能按 `t.Background`
// 算（理由同 `ink.AccentOn` 那一段）。
//
// ⚠️ **列表行悬停时底色换成 `t.SurfaceHover`**（`mygo/ui/list.go` 的
// `w.bg = t.SurfaceHover`）——同一段字于是有两个可能的底，字色按**更严的那
// 一个**算（`commentMarkOf` 里两个都量一遍、取对比度低的那个）。
package ui

import (
	"unicode/utf8"

	"github.com/ShellMonster/PudMusic/internal/ui/songname"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// commentMarkAlpha 是命中那一段底上主色的浓度（18%）。
//
// ⚠️ **和 `playingRowBgAlpha`（20%）不是同一个数，也不共用一个常量**：那一
// 档是**一整行**的底（一个独立的块），这一档是**几行字**的底——同样的浓度在
// 连续文字上看着重一档。两个数要各自调，共用一个会牵一发动全身。
const commentMarkAlpha = 0.18

// commentMark 是「命中的那一段」要怎么画：底色、字色、圆角。
//
// ⚠️ **零值 = 不高亮**（`Bg.A == 0`）：还没算出主题的那一档（比如筛选关着）
// 传零值进来，`commentSpans` 就按原来的样子拼 span；这比「再传一个 bool」少
// 一个可能自相矛盾的参数（`Bg` 是透明的却说要高亮）。
type commentMark struct {
	// Bg 是那一段的底色（**半透明**的主色，压在当前那一行的底上）。
	Bg ui.Color
	// Ink 是那一段的字色（按 `Bg` 合出来的那个底算过 4.5:1）。
	Ink ui.Color
	// Radius 是底色的圆角（`Span.Background` 会贴着字形的外沿画）。
	Radius float32
}

// commentMarkOf 算这一帧的高亮配色。
//
// ⚠️ **一帧算一次**（`viewComments` 开头），不是每一条评论算一次：`ink.Ensure`
// 是一趟二十步的二分（每步一次 WCAG 相对亮度换算），而这一页一屏十来条评论
// ——每条各算一遍是白花的。
func commentMarkOf(t *ui.Theme) commentMark {
	wash := t.Accent.Alpha(commentMarkAlpha)
	// 这层淡底合到两个可能的底板上的样子：页面底色，和列表行悬停那一档。
	base := opaqueBackground(t.Background)
	bg := wash.Over(base)
	if t.SurfaceHover.A == 255 {
		// ⚠️ **取对比度更低的那个**：字色要同时在这两个底上读得出来，而
		// `Ensure` 只保证对**给定的那个底**达标——按宽的那个算，严的那个就
		// 会差一截（同 `ink.SemanticOn` 挑 `SurfaceHover` 的理由）。
		hover := wash.Over(t.SurfaceHover)
		if ink.Ratio(t.Accent, hover) < ink.Ratio(t.Accent, bg) {
			bg = hover
		}
	}
	return commentMark{
		Bg:     wash,
		Ink:    ink.Ensure(t.Accent, bg, ink.TextContrast),
		Radius: space.Px2,
	}
}

// opaqueBackground 把带透明度的页面底色合到白底上（窗口底下没有第三层可依）。
//
// 理由同 `playingRowBase`：用户挑了一个带透明度的底色时，面板本身也是半透明
// 的，而「合出来的那个颜色」只有压在白底上才算得出来。
func opaqueBackground(c ui.Color) ui.Color {
	if c.A == 255 {
		return c
	}
	return c.Over(ui.Color{R: 255, G: 255, B: 255, A: 255})
}

// 🔴 **为什么没有「把命中区间裁进折起来的那一段」那一档**（旧的 `clipMarks`
// 干的）：
//
// 旧的实现里，**截断是这一页自己做的**（自己二分、自己量高度），所以**截断
// 点之后的那一截正文**要**从 span 里切掉**——留着它只会去切一段不存在的文字。
//
// 而现在**截断是框架做的**（`ui.RichText` 的 `MaxLines` + `Ellipsis`）：框架
// 自己知道切到哪儿，**画出来的**只有切点之前的那几行，而**高亮是按原文的
// rune 坐标**烘进 span 里的——切点之后的那几段**画不出来**（它们不在那一行
// 上），于是**不用裁**。
//
// ⚠️ **而「给读屏的那一份」是整段原文**（`.Label(...)` 给的）：它**没有**被
// 截断（框架的省略号是**画出来**的，不在文字里）——所以读屏软件读到的是**整
// 句**，而眼睛看到的是**折起来的那几行**。这两者**不一致**是这一票的**已知
// 取舍**（见 `comments_body.go` 的文件头）。

// appendMarkedText 把一段文字按命中区间切成几段 span：命中的那几段套上底色与
// 主色字，其余照旧。
//
// base 是这段文字的**第一个 rune** 在整条正文里的下标（`hl` 用的是那个坐标
// 系——见 `songname.Range`）。`hl` 必须是**排好序且合并过**的（`Matches` 的
// 返回值就是那样）。
//
// ⚠️ **按 rune 走，不按字节**：命中区间的坐标系是 rune，而字节长度在中文和
// emoji 上各不相同。
//
// ⚠️ **区间可能把一头一尾切在一个字的中间吗**：不会——`Matches` 映射回来的
// 区间落在字边界上（`normalize` 是逐 rune 做的），而截断那一档也是按 rune 切
// 的（`safeCommentCut`）。
func appendMarkedText(dst []ui.Span, text string, base int, hl []songname.Range, mark commentMark) []ui.Span {
	if text == "" {
		return dst
	}
	if mark.Bg.A == 0 || len(hl) == 0 {
		return append(dst, ui.Span{Text: text})
	}
	// segStart 是当前这一段在 text 里的字节起点，segMarked 是这一段有没有命中。
	pos := base
	segStart := 0
	segMarked := false
	ri := 0
	flush := func(end int) {
		if end <= segStart {
			return
		}
		sp := ui.Span{Text: text[segStart:end]}
		if segMarked {
			sp.Background, sp.Color, sp.Radius = mark.Bg, mark.Ink, mark.Radius
		}
		dst = append(dst, sp)
	}
	for i := 0; i < len(text); {
		_, size := utf8.DecodeRuneInString(text[i:])
		for ri < len(hl) && pos >= hl[ri].End {
			ri++
		}
		marked := ri < len(hl) && pos >= hl[ri].Start && pos < hl[ri].End
		if marked != segMarked {
			flush(i)
			segStart, segMarked = i, marked
		}
		pos++
		i += size
	}
	flush(len(text))
	return dst
}
