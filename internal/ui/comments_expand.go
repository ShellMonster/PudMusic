// 长正文的「展开 / 收起」那一枚（票 #706）。
//
// # 这一份在**新架构**上重做过了
//
// 原来的实现是**建在旧的自排版那套上**的：
//
//	shapeCommentBody(c, cm, w, lines, hl, mark)  自己排版、自己二分截断
//	commentBody.folded                            排版结果里那一位「折过了」
//	commentBodyOf(c, p, cm, lines, w)             按 (rpid, w, lines, mark) 缓存
//	commentBodyUnlimited = -1                     「不限行数」那一个负数
//
// 而票 #702 把正文换成了**框架的 `ui.RichText`**（理由见 `comments_body.go` 的
// 文件头）⇒ 排版、截断、高亮、选中**全都交给框架**了，而旧的那一套**在 main
// 上已经删干净**。
//
// ⇒ **「展开」这个功能在新架构上重新实现**（就是这一份）。
//
// # 🔴 新架构的关键差别（三条，都影响这一份）
//
//		① **框架的 `MaxLines` + `Ellipsis` 接手截断**
//		   而框架判的是 **`p.MaxLines > 0`**（`internal/text/layout.go` 的 `layout`：
//		   `if p.MaxLines > 0 { maxLines = p.MaxLines - len(l.Lines) }`）
//		   ⇒ **`MaxLines(0)` 就是「不设上限」，整个正文一行都不截**。
//		   ⇒ 旧的 `commentBodyUnlimited = -1`（那个负数是为了绕开旧实现里
//		   「`maxH = 0` 会让截断那一支恒真」的坑）**不再需要**——新架构里 0 就是
//		   「不限」（`commentBodyUnlimited = 0`）。
//
//		② **框架自己有布局缓存**
//		   （`mygo/ui/layoutcache.go` 的 `sameLayout`：按 span 的样式比对，样式没变
//		   就不重排）⇒ **不再需要 `p.bodies` 那份自缓存来存 `*ui.TextLayout`**。
//	  ⇒ 而展开/收起是**换一档 `MaxLines`**，两档的参数不同 ⇒ 框架的缓存**各
//		   命中自己那一份**，不需要手工清缓存。
//
//		③ **三套坐标系**（`comments_body.go` 的文件头那一节）：原文 / 给读屏那份 /
//		   span 里那份，长度都不一样，而「命中歌名」的高亮区间用的是**原文的 rune
//		   坐标**。
//	  ⇒ **展开/收起不碰任何 span** ⇒ 高亮区间**天然还对得上**：高亮是**烘进
//		   span 里**的（`ui.Span.Color` / `Background`），而换的只是 `MaxLines`
//		   ⇒ 同一批 span 在两档里画出来，高亮落在同样的那几个字上。
//
// # 「折没折」怎么判
//
// 🔴 **判据是「量出来的高度超过上限没有」**，而**不是**「框架截了没有」——
// 框架**不告诉**元素「我截断了」（`text.Layout.Truncated` 在 `internal/text`
// 里，而 `ui` 这一层没把它露出来）。
//
// ⇒ 所以这一份**自己量一次**：用 `c.MeasureText` 量那几段 span 在**正文那一列
// 的宽**上排出来有多高，而 `MeasureText` **不设 `MaxLines`** ⇒ 它量的是**完整
// 正文**的高度（而画出来的那一格是**截断之后**的）。
//
// ⚠️ **量的必须是「画出来的那一份」**：同一个宽、同一批 span、同一个字号
// （`scale.Font(c, scale.Size14)`）⇒ 量出来的高度和画出来的高度**是同一趟
// 排版**（框架的文本系统按参数缓存，`text.System.Layout` 认同一份 `Params`）。
//
// ⚠️ **而「上限」是 `lines × lineH`**：`lineH` 是字体的 ascent + descent +
// line gap（`ui.Font.Metrics()`），而框架那一格画出来的行高**就是它**。
//
// ⇒ **判据**：`measuredH > lines × lineH` ⇒ 正文太长 ⇒ 折起来了 ⇒ 该摆
// 「展开」那一枚。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// commentBodyFolded 断「这条正文**太长了吗**」（票 #706）。
//
// 🔴 **它量的和画的是同一趟排版**：`c.MeasureText(width, spans...)` 和
// `ui.RichText(c, spans...)` 都走框架的文本系统，而那一份按**参数**缓存
// （`text.System.Layout` 认同一份 `Params`）⇒ 量出来的行数和画出来的行数
// **是同一个**。
//
// ⚠️ **量的是「完整正文」**：`MeasureText` 不设 `MaxLines`（它量的是
// `Painter.RichText` 画出来的大小，而那一条**没有**行数上限）⇒ 它给的是
// **一个字都没截**时的高度。而画出来的那一格**是截断之后**的（`MaxLines` +
// `Ellipsis`）⇒ 两者一比就知道「截了没有」。
//
// ⚠️ **上限是 `lines × lineH`**：`lineH` 是字体的 ascent + descent + line gap
// （`ui.Font.Metrics()`），而框架画出来的那一行**就是它那么高**。
//
// ⚠️ **差一点点不算折**：框架的截断判的是「有没有第 `lines+1` 行」（`> 0` 的
// 那个 `maxLines`），而**一行的高度是有零头的**——所以判据给 **0.5 的余量**，
// 免得「刚好差一根头发」的时候多摆一枚「展开」。
//
// ⚠️ **「不限行数」那一档恒false**：`commentBodyUnlimited` 是 0，而 0 行的
// 上限是 0 ⇒ 量出来的高度**永远大于** 0 ⇒ 而那一档压根不该判「折了」——
// 它是**展开之后**的那一份。⇒ 调用方**先看展开态**（那一档直接返回 false）。
//
// # 🔴 **量一次就够，量出来的记在缓存的那一份里**
//
// ⚠️ **每帧每条量一遍是白烧的**：量一次是**一趟排版**（`c.MeasureText`），
// 而这一页每一帧都要画十来条评论的正文 ⇒ 每帧每条量一遍就是**每帧十来趟
// 排版**——而 `framebudget_test.go` 那一档（评论页 2300 字节 / 88 次）**会红**。
//
// ⇒ 所以量出来的那一位**记在 `commentBody` 里**（`folded` + `foldedW`），
// 而**缓存键里含宽**（宽度变了 ⇒ 折没折可能变了 ⇒ 要重量）。
//
// ⚠️ **而那一趟排版**（`c.MeasureText`）**自己也有缓存**（框架的
// `text.System.Layout` 按参数缓存）——所以「重量」那一趟**不重排**，它只是
// **查一遍缓存**。⇒ 而它**分配的那一份**（`meas` 那个切片）是**每帧新建**的
// ——⇒ 那才是这一档真正的开销，而**记下来之后连它也不用每帧建**。
func commentBodyFolded(c *ui.Context, body *commentBody, width float32, lines int) bool {
	// 🔴 **「不限」那一档不判**：它是**展开之后**的那一份，而展开之后一行都
	// 没截 ⇒ 「折过了」恒假。
	if lines <= 0 {
		return false
	}
	if width <= 0 {
		// 还没量到那一列的宽（第一帧）：**不判折**——而下一帧量到了就判了。
		// ⇒ 症状是「刚进来那一帧没有『展开』那一枚」，而下一帧就有了。
		return false
	}
	// 🔴 **缓存命中**：同一个宽上量过一次了 ⇒ 直接答。
	//
	// ⚠️ **宽是「上一帧」的**（`Bounds()` 报的是上一帧的盒子）⇒ 而它**变了**
	// 就是另一份（折没折可能变了）⇒ 所以宽是键的一部分。
	if body.foldedW == width {
		return body.folded
	}
	// 🔴 **量的字号要和画的一样**：画出来的那一格是
	// `FontSize(scale.Font(c, scale.Size14))`，而 `MeasureText` 量的字号是
	// **span 自己带的那个**（`spanStyle` 里 `sp.Size` 覆盖基础字号）——⇒
	// 这里给每一段补上那个字号。
	//
	// ⚠️ **而那个切片是每帧一次分配**（这一页一屏十来条评论）⇒ 所以它**记在
	// 缓存的那一份里**（`measSpans`），而不是每帧现建（理由见上面的
	// 《量一次就够》）。
	size := scale.Font(c, scale.Size14)
	if body.measSpans == nil || len(body.measSpans) != len(body.spans) {
		body.measSpans = make([]ui.Span, len(body.spans))
	}
	for i, sp := range body.spans {
		body.measSpans[i] = sp
		if body.measSpans[i].Size == 0 {
			body.measSpans[i].Size = size
		}
	}
	_, h := c.MeasureText(width, body.measSpans...)
	folded := false
	if h > 0 {
		m := ui.Font{Family: c.Theme().Font, Size: size}.Metrics()
		lineH := m.Ascent + m.Descent + m.LineGap
		if lineH > 0 {
			folded = h > float32(lines)*lineH+0.5
		}
	}
	body.folded, body.foldedW = folded, width
	return folded
}

// commentBodyWidthOf 是正文那一列有多宽（**框架量出来的那一个**）。
//
// 🔴 **它量的不是「容器宽」而是「正文那一格自己的盒子」**：正文的宽度决定它
// 在哪几个字那里折行，而折行位置决定这一格多高——**宽度错了，整张列表的高度
// 就错了**。
//
// ⚠️ **它量的是上一帧的盒子**（`Bounds()` 报的是上一帧的）：这一帧还没排版，
// 拿不到这一帧的宽。差一帧的代价是刚进这一页时正文按一个猜的宽度量一次，
// 下一帧就对了。
//
// ⚠️ **宽度是 0 的时候返回 0**（不是猜一个）：而 `commentBodyFolded` 见到 0
// 就**不判折**（见它的注释）——⇒ 猜一个宽度会让「折没折」的判据**半个身位
// 落在猜测上**。
//
// 🔴 **为什么不再按「容器宽减去头像和缝」算**：那是旧的做法（旧的那一套自己
// 排版，所以要自己算宽度）。而新架构**框架自己按宽度折行**（`FillWidth` 的
// 那一格拿到的是框架量出来的宽）⇒ **我们自己算的那个数是另一份口径**，而
// 两份口径迟早分叉——而分叉出来的那种差异极难查（正文折在哪儿和画出来的那
// 一格对不上）。
//
// ⇒ 所以这里量的是**画出来的那一格自己的盒子**（`Bounds().W`）。
func commentBodyWidthOf(body ui.Element) float32 {
	return body.Bounds().W
}
