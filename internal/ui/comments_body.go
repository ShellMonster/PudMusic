// 评论正文那一格：框架的富文本控件 + 表情图（票 #702）。
//
// # 这一格为什么改用框架的 `ui.RichText`
//
// 旧的做法是**自己排版、自己画**（`shapeCommentBody` + `ui.ShapeRichText` +
// `Painter.TextLayout` + `Painter.Image`）：正文切成一段一段的 span，表情占位符
// 换成一枚 U+FFFC（inline object），排出来的版自己量高度、自己二分截断，
// 表情图自己按 `layout.Objects()` 贴上去。
//
// 🔴 **当初为什么不用框架的文本控件**（旧文件头里那句话）：
//
//	框架的文本控件画不出 inline object 那一格：mygo/ui/paint.go 的
//	textForeground 跳过 g.Object 的字形——它只给那一格**留位置**，不画东西。
//
// **这条断言写在 2026-10-28（`f0735f1f`），那时依赖是 mygo v0.6.4。** 而
// `DrawObjects` 是 **v0.7.1 才引入的**（v0.6.4 里 0 个文件提到它，v0.7.1 起
// 6 个文件有）——项目之后在 #611 升到 v0.7.1、在 `d25206a2` 升到 v0.7.3，
// **两次都没把代码和注释一起跟上**。⇒ 那条理由**已经不成立**了。
//
// 框架现在**有**现成能力（`mygo/docs/ui/text.md`）：
//
//	A span with an ObjectWidth makes its object replacement characters
//	(U+FFFC) inline objects... `DrawObjects` draws them, after the glyphs,
//	in the text's clip: it gets each one's rune in the paragraph and its
//	box.
//
// ⇒ 正文改用 `ui.RichText(...).DrawObjects(...)`：排版、截断、高亮、选中
// **全都交给框架**，而表情图由 `DrawObjects` 贴上去（那一格的位置是框架给
// 的，贴得准）。
//
// # 手搓的那些为什么能删
//
//	shapeCommentBody   自排版 + 自截断（框架的 `MaxLines` + `Ellipsis` 接手）
//	commentBody        自己缓存 `*ui.TextLayout`（框架有自己的布局缓存，
//	                   `mygo/ui/layoutcache.go` 的 `sameLayout`）
//	safeCommentCut     自己算占位符区间（框架按 grapheme 截断，而占位符那一
//	                   格是**一个 rune**，切不到它里面去——见下面《切点落在占位
//	                   符那一格》）
//	commentEmoteRanges 同上
//	commentBodyOf      自己按 (rpid, w, lines, mark) 缓存
//
// ⚠️ **旧的那一档还有一个毛病**：它调的是 `text.Shared().Shape`，而那一趟
// **不缓存**（`internal/text/layout.go:845`「caches nothing」）——所以那一帧
// 每条评论都重排一次。框架的 `ui.RichText` 走的是**元素自己的布局缓存**
// （`mygo/ui/layoutcache.go`），同一份正文跨帧不再重排。
//
// # 三套坐标系（这一格最容易做坏的地方）
//
// 一条评论的正文有**三份长度不一样的串**：
//
//	原文        `[妙啊]`            5 个 rune
//	给读屏那份  `［表情］`            4 个字符
//	span 里     `\uFFFC`             1 个 rune
//
// 而「命中歌名」的高亮区间（`songname.Range`）用的是**原文的 rune 坐标**。
// ⇒ 切 span 的那一步必须**同时**知道「原文走到哪儿了」和「span 走到哪儿了」。
//
// ⚠️ **本文件里的做法**：切 span 时**逐 rune 走原文**（`appendMarkedText`），
// 而**表情占位符那一格整段让出去**（它画出来是一张图，不是一块有底色的字，
// 给它套一层底色的话画面上就是表情图四周多出一块方方的色块）。
//
// # 切点落在占位符那一格
//
// ⚠️ **框架的 `MaxLines` 按 grapheme 截断**，而占位符在 span 里是**一枚
// rune**（U+FFFC）——所以切点要么在它**前面**，要么在它**后面**，**不会落在它
// 里面**（那是旧的做法才有的毛病：旧的那一份按 rune 切**原文**，而原文里
// `[妙啊]` 是五个 rune，切点可以落在中间）。
//
// ⇒ 「切坏表情」那个形状（正文里剩下半个 `[妙啊`）**在新的做法里压根
// 不存在**：画出来的那一份里，占位符要么整枚在（一张图），要么整枚不在。
//
// # 🔴 省略号被「表情那一格」吞掉（票 #702 的审查）
//
// 框架的省略号是**加在最后一行末尾**的，而它的样式取自**最后一个到达切点的
// 那一段 span**（`internal/text/layout.go` 的 `ellipsize`：`tSpans[n-1].End
// += len(mark)`）——所以**切点正好落在表情占位符那一格末尾**时，省略号的
// rune 被**并进那一段**。
//
// ⚠️ **那一段带 `ObjectWidth`** ⇒ 它是 **inline object**：框架给 `ObjectWidth
// > 0` 的 span 的**每一个 rune**挂 run delegate（`internal/text/layout.go` 的
// `line()`：`g.Object, g.Advance, g.ID = true, sp.ObjectWidth, 0`）⇒ 那个
// rune 的**推进宽是 18**（表情那格的宽），而**不是**省略号自己的 11.12。
//
// ⇒ **实测（框架内部 trace，`internal/text/layout.go` 的 `ellipsize`）**：
//
//	切点在文字段末尾   省略号那一格 adv = **11.12**（正常）
//	切点在表情段末尾   省略号那一格 adv = **18.00**（被表情那格吞了）
//
// ⇒ 而**后果随平台而异**（这一段是**平台无关**的：`ellipsize` 在
// `internal/text/layout.go` 里，Core Text 和 DirectWrite 都走它）：
//
//	**macOS（Core Text）**  框架的 `line()` 按 **rune 下标**找那一格属于哪一段
//	                     （`spanAt(spans, g.Cluster)`）——省略号的 rune 下标是
//	                     **切点之后**，落在**下一段**（普通文字段）⇒ 不是
//	                     object ⇒ **省略号照常画出来**（实测：像素逐列相同，
//	                     末尾三个点都在）。
//	**Windows（DirectWrite）** `setObjects` 按 **code unit** 给那一段里**每个
//	                     rune**挂 inline object（`dwrite_objects_windows.go`）——
//	                     而省略号的 code unit **就在并进去的那一段里** ⇒ 那一格
//	                     **真的**是 inline object ⇒ **一个字都不画** ⇒ 用户
//	                     看到的是**一片空白**（不是「…」）。
//
// ⇒ **本文件的做法**：每枚表情占位符**后面接一段零宽的普通 span**
// （`commentObjectGuard`，见 `commentSpansOf`）——于是切点**落不到**表情那
// 一格的末尾（切点落在它**前面**或落在**守卫那一段**的末尾，而守卫那一段是
// **普通**的）⇒ 省略号**永远**继承普通文字的样式，**永远**是「…」。
//
// ⚠️ **守卫是零宽的**（U+200B ZERO WIDTH SPACE）：它**不占地方**（实测：加它
// 前后每一行的墨迹签名逐宽度相同，见 `comments_body_test.go` 的
// `TestTheEllipsisGuardDoesNotChangeTheLayout`）——所以它**不改变排版**。
package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/songname"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// commentObject 是正文里一枚表情占位符的尺寸（inline object 的盒子）。
//
// ⚠️ **它的高度是 `ObjectHeight`（基线以上），而下降是 `ObjectDescent`**
// （基线以下）——框架的约定（`mygo/docs/ui/text.md`）：
//
//	a box of that size in DIPs standing on the baseline, ObjectHeight above
//	the baseline and ObjectDescent below it
//
// ⇒ 那一格**站在基线上**，而它是 18×18 的正方形 ⇒ 高度 18、下降 0。
const (
	commentObjectW = float32(commentEmote)
	commentObjectH = float32(commentEmote)
)

// 🔴 **为什么末尾不补一段空 span**（曾经想过，没用）：
//
// 框架的省略号继承**最后一个到达切点的那段 span** 的**排版**样式
// （`internal/text/layout.go` 的 `ellipsize`：`tSpans[n-1].End += len(mark)`），
// 而那一段**可能是表情占位符那一格**（它带 `ObjectWidth`）——于是省略号会被
// 按 inline object 排版（**一块空白**，不是「…」）。
//
// ⚠️ **而「末尾补一段空 span」压根没用**（实测）：**空**的那一段与前一段的
// `End` **相同**（它没有自己的 rune）⇒ 它**压根不是**「最后一个到达切点的
// span」⇒ 切点落在表情那一格末尾时，省略号**照样**并进表情那一格
// （实测：那一格的推进宽是 **18**，而省略号自己只有 **11.12**）。
//
// 🔴 **而这个思路本身是对的**——只是要补在**每一枚表情后面**（而不是正文
// 末尾），而且要**零宽**（否则占地方）。⇒ `commentObjectGuard`。

// commentObjectGuard 是**每枚表情占位符后面**跟的那一段零宽的普通 span。
//
// 🔴 **它挡的是「切点正好落在表情那一格的末尾」**（理由见文件头《省略号被
// 「表情那一格」吞掉》）：框架的 `ellipsize` 把省略号的 rune 并进**最后一个
// 到达切点的那一段**——而加了守卫之后，那一段**永远是守卫**（普通 span）⇒
// 省略号**永远**继承普通文字的样式。
//
// ⚠️ **为什么是 U+200B（ZERO WIDTH SPACE）而不是别的**：
//
//	**零宽**  它不占任何地方 ⇒ 排版不变（实测：加它前后每一行的墨迹签名
//	         逐宽度相同，见 `comments_body_test.go` 的
//	         `TestTheEllipsisGuardDoesNotChangeTheLayout`）。
//	**普通**  它**不带** `ObjectWidth` ⇒ 框架不会把它当 inline object。
//	**不成字**  它是**控制字符**（`unicode.Cf`）⇒ 复制出去对用户毫无意义
//	         （而 `commentPlain` 那一份**根本不带它**——它只活在画出来的那
//	         一份里）。
//
// ⚠️ **它为什么不能是 U+200C / U+200D（ZWNJ / ZWJ）**：那些是**连接符**，
// 而连接符会**把两边的字连成一个 grapheme** ⇒ 切点会落在**跨过守卫**的地方
// ⇒ 守卫那一段反而**不是**「最后到达切点的那一段」了（实测：ZWJ 那一档
// 省略号仍然被吞成 18 宽）。
const commentObjectGuard = "\u200B"

// commentPlain 是「给辅助技术和无头 Tester 的那一份正文」。
//
// 🔴 **它和画出来的那一份必须是同一句话**（理由同旧 `commentBody.plain`
// 的注释）：不然屏幕阅读器读到的和眼睛看到的不一样。
//
// ⚠️ **表情占位符换成「［表情］」**：那一格在文字里是 U+FFFC——那是个不可见
// 的字符，复制出去对用户毫无意义；而「［表情］」至少说清了那里有个表情。
//
// ⚠️ **占位符不认死一个**：B 站给的那张表是**每条评论自己一份**
// （`Comment.Emotes`），而占位符本身长得五花八门（`[妙啊]` / `[大哭]` /
// `[间奏时光表情包_哭]`）。
//
// 🔴 **它必须和 `commentSpansOf` **共用同一次切分**（理由见
// `commentSpansOf` 的注释《两份必须同源》）。
func commentPlain(msg string, emotes map[string]bili.CommentEmote) string {
	_, _, plain := commentSpansOf(msg, emotes, nil, commentMark{})
	return plain
}

// commentPlainSpan 是正文里**一枚表情占位符**在「给读屏的那一份」里的写法。
//
// ⚠️ **它和画出来的那一份不一样**：画出来的那一份里那是**一枚 U+FFFC**
// （inline object，框架给那一格留位置并把它交给我们画），而给读屏的那一份
// 是「［表情］」四个字。
//
// ⇒ **这两份的长度不一样**（1 个 rune vs 4 个字符），所以「选中的那几个
// rune」**不能**直接拿去切给读屏的那一份——见 `commentPlainOfSelection`。
const commentPlainSpan = "［表情］"

// commentPlainOfSelection 把「框架选中的一段」（那一段是**画出来的那一份**
// 的文字）翻译成**给读屏的那一份**的对应的一段。
//
// 🔴 **为什么要翻译**：画出来的那一份里，表情占位符是**一枚** U+FFFC；而
// 给读屏的那一份里它是**四个字**（「［表情］」）。⇒ 那一枚 U+FFFC 复制出去
// 对用户毫无意义（那是个不可见的字符），而「［表情］」至少说清了那里有个
// 表情。
//
// ⚠️ **它只换 U+FFFC，不动别的**：所以选中的那段普通文字**一个字都不改**
// （高亮是装饰，复制出来的那句话必须和画出来的一样）。
//
// 🔴 **坐标是哪一份的（这一条必须写清楚）**：框架给的是**画出来的那一份**的坐标。
//
// ⇒ 技术上的出处：`mygo/ui/textselection.go:419`：
//
//	parts = append(parts, SelectedText{ID: p.id, Text: p.editor.source, Start: from, End: to})
//
// 而 `editor.source` 是**画出来的那一份**（`mygo/ui/textselection.go:85`：`ed.source = e.text`，
// 而 `e.text` 是**那一格的文字**）——**不是** `Label` 那一份（那一份是「给读屏的」，
// 而它是**另一份字符串**（表情占位符写成「［表情］」。
//
// ⚠️ **为什么这个差别很要紧**：两份的**长度不一样**（表情那一格是 **1 个 rune**，
// 而给读屏那一份是 **4 个字符**）⇒ 若果坐标是**给读屏那一份**的，整个选区就**全错位**了。
//
// ⇒ 而这一条依赖的**前提**（框架给的是画出来那一份）在**代码里**没有任何地方写下来
// ——所以 `comments_body_test.go` 里有一条用例把它钉住（见那里的
// `TestSelectingAfterTheEmoteStillCopiesTheRightSentence`）。
//
// 🔴 **它还去掉守卫**（`commentObjectGuard`）：守卫是**画出来的那一份**里的
// 零宽字符（U+200B）——它**只**是为了让省略号不落在表情那一格（理由见文件
// 头），而它复制出去对用户毫无意义（一个看不见的控制字符）⇒ 复制出来的那句
// 话必须和 `commentPlain` **一模一样**。
func commentPlainOfSelection(text string, start, end int) string {
	// ⚠️ **按 rune 切**（不是按字节）：框架给的是**rune 下标**
	// （`SelectedText.Start` / `End`），而中文一个字三个字节——按字节切会切坏。
	runes := []rune(text)
	if start < 0 {
		start = 0
	}
	if end > len(runes) {
		end = len(runes)
	}
	if start >= end {
		return ""
	}
	out := strings.ReplaceAll(string(runes[start:end]), "\uFFFC", commentPlainSpan)
	return strings.ReplaceAll(out, commentObjectGuard, "")
}

// commentSpansOf 把正文切成一段一段的 span：**文字是文字，表情占位符换成一枚
// inline object**。
//
// 返回的第二样是那些表情图的地址，**与 inline object 一一对应**（按出现的
// 顺序）——`DrawObjects` 回调里的 `at`（那枚 U+FFFC 在段落里的 rune 位置）
// 就是找它们的钥匙。
//
// 🔴 **返回的第三样是「给读屏的那一份」**（表情占位符写成「［表情］」）——
// **它和前两样必须来自同一次切分**（理由见下面《两份必须同源》）。
//
// 🔴 **认占位符的口径只有这一处**（理由同旧 `commentSpans`）：「`[` 到下一个
// `]` 之间的那一段，**在表情表里**」才算占位符。
//
// ⚠️ **高亮是这一支的另一档**（`commentSpansMarked`）：`hl` 为空、`mark` 是
// 零值的时候，两处的 span 逐段相同——所以「认占位符」仍然只有这一处。
//
// # 🔴 两份必须同源（票 #702 的审查）
//
// 旧的做法（基线）里 `commentSpansMarked` **一个函数**同时产出 `spans` 和
// `plain` ⇒ 那两份**必然一致**。
//
// 而 #702 把它们拆成两个独立函数之后，**判定的口径就分叉了**：
//
//	`commentSpansOf`  从左往右扫，找 `[` 到**下一个** `]` 之间的那一段，
//	                  **整段在表情表里**才算占位符。
//	`commentPlain`    `strings.ReplaceAll` 替换**表情表里每一个 token**，
//	                  **不看边界**。
//
// ⇒ 两者在两种形状上分叉：
//
//	**嵌套方括号**  正文 "前面有字[a[b]]后面有字"，表情表 {"[b]"}
//	                `commentSpansOf` 认出的是 `[b]`（从 `[` 到下一个 `]`）
//	                ⇒ 画出来的是 U+FFFC；而 `commentPlain` 的 ReplaceAll
//	                把 `[b]` 也换成「［表情］」⇒ **读屏的那一份与画出来的那
//	                一份不一样**。
//	**相邻 token**  正文 "前面有字[a][a]后面有字"，表情表 {"[a]", "[a][a]"}
//	                ⇒ ReplaceAll 按 **map 的迭代顺序**替换 ⇒ **同一帧里
//	                读到的内容可能每次都不同**（`commentPlain` 在
//	                `viewCommentBodyEl` 里**每帧**调一次）。
//
// ⇒ **本文件的做法**：**一次切分，两份结果**——`commentSpansOf` 在切分的
// 同一趟里顺手把「给读屏的那一份」也拼出来（`plain`），而 `commentPlain`
// **只是它的第三个返回值**（不再自己 `ReplaceAll`）。
func commentSpansOf(msg string, emotes map[string]bili.CommentEmote, hl []songname.Range, mark commentMark) ([]ui.Span, []string, string) {
	var spans []ui.Span
	var urls []string
	// plain 是「给读屏的那一份」——**在同一次切分里拼出来**（理由见上面的
	// 《两份必须同源》）。
	var plain strings.Builder
	// base 是 rest 之前已经过去了多少个**原文的** rune。
	base := 0
	text := func(s string) {
		if s == "" {
			return
		}
		spans = appendMarkedText(spans, s, base, hl, mark)
		plain.WriteString(s)
		base += utf8.RuneCountInString(s)
	}
	rest := msg
	for len(rest) > 0 {
		i := strings.IndexByte(rest, '[')
		if i < 0 {
			text(rest)
			break
		}
		if i > 0 {
			text(rest[:i])
		}
		j := strings.IndexByte(rest[i:], ']')
		if j < 0 {
			// 没有收尾的 ']'：后面整段都是普通文字。
			text(rest[i:])
			break
		}
		token := rest[i : i+j+1]
		e, ok := emotes[token]
		if !ok {
			// 不是表情：这一段就是普通文字。
			text(token)
			rest = rest[i+j+1:]
			continue
		}
		// 🔴 **占位符那一格整段让出去**（不着色）：它画出来是一张图，不是一块
		// 有底色的字——给它套底色的话，画面上就是表情图四周多出一块方方的
		// 色块（`Span.Background` 贴着字形外沿画，而 U+FFFC 的「字形」就是
		// 那一枚 18×18 的 object）。
		spans = append(spans, ui.Span{
			Text:         "\uFFFC",
			ObjectWidth:  commentObjectW,
			ObjectHeight: commentObjectH,
		})
		// 🔴 **守卫**：表情后面接一段零宽的普通 span（理由见文件头《省略号被
		// 「表情那一格」吞掉》）。
		spans = append(spans, ui.Span{Text: commentObjectGuard})
		urls = append(urls, commentImageURL(e.URL))
		// ⚠️ **给读屏的那一份**：表情占位符写成「［表情］」——**不含守卫**
		// （那是个零宽的控制字符，复制出去对用户毫无意义）。
		plain.WriteString(commentPlainSpan)
		base += utf8.RuneCountInString(token)
		rest = rest[i+j+1:]
	}
	return spans, urls, plain.String()
}

// viewCommentBodyEl 画一条评论的正文。
//
// 🔴 **正文是框架的 `ui.RichText`**（理由见文件头）：它负责排版、按行数截断
// 加省略号、把「命中歌名」的那几段染上底色，而**可选中**是它自带的能力
// （`Selectable`）。
//
// ⚠️ **`.Label(...)` 把正文交给框架**：屏幕阅读器读得到它，无头 Tester 也
// 找得到它——不然这一页的正文对它们是不存在的。
//
// ⚠️ **那一格给的是 `plain`（表情占位符写成「［表情］」的那一份）**：那一格
// 是**给读屏的那一份**，而画出来的那一份里表情是图（见 `DrawObjects`）。
//
// 🔴 **高亮只在筛选开着的时候做**（票 #684）：关着的时候这一条走的是**原来
// 那条路**——不跑那次匹配、不带任何底色（正文是**连续几行的字**，常驻的色块
// 会让用户以为自己是拿鼠标选中了几段）。
func (s *Shell) viewCommentBodyEl(c *ui.Context, p *commentsPage, cm bili.Comment, lines int) {
	body := s.commentBodyOf(p, cm)
	if body == nil || len(body.spans) == 0 {
		return
	}
	spans := body.spans
	// ⚠️ **这两个闭包每帧新建一次就是每帧两次分配**：而这一格每一帧都要画
	// （一屏十来行就是二十来次）⇒ **闭包记在缓存的那一份里**
	// （`commentBodySpans`），不是每帧现建。
	//
	// ⚠️ **理由同旧 `commentBody.paint` 的注释**（那一档记的是「画这一格的那
	// 一支」）：闭包每帧新建一次就是一次分配，而这一格每一帧都要画。
	//
	// ⚠️ **颜色在里面现取**（`pr.Theme()`）：主题是用户能改的，而这一份缓存
	// 跨帧活着——写死颜色的话，用户换了深浅两套之后正文还是旧的颜色。
	draw := body.draw
	copyFn := body.copyFn
	if draw == nil {
		// 🔴 **第一次画这一格**：建那两个闭包，记在缓存的那一份里。
		cache := s.pageImages()
		spans, urls := body.spans, body.urls
		draw = func(p *ui.Painter, at int, r ui.Rect) {
			// 🔴 **`at` 是那枚 U+FFFC 在段落里的 rune 位置**（框架给的），而
			// `urls` 是**按出现顺序**的图——两者的对应关系靠**数**（`at` 每多一
			// 个，就是下一枚表情）。
			//
			// ⚠️ **数的是「这一枚之前有几枚」**：框架的 `at` 是段落里的 rune 下
			// 标，而占位符在段落里各占一个 rune——所以「第几个占位符」= 数一遍
			// 段落里排在它前面、同样是占位符的 rune 有几个。
			i := commentObjectIndex(at, spans)
			if i < 0 || i >= len(urls) {
				return
			}
			st := cache.Get(urls[i])
			if st.Bitmap == nil {
				// 图还没下来：那一格先空着（框架的 inline object 本来就不画任
				// 何东西），图好了缓存会叫醒这一扇窗。
				return
			}
			p.Image(st.Bitmap, r, ui.Contain)
		}
		copyFn = func(parts []ui.SelectedText) transfer.Data {
			var out strings.Builder
			for i, part := range parts {
				if i > 0 {
					out.WriteByte('\n')
				}
				out.WriteString(commentPlainOfSelection(part.Text, part.Start, part.End))
			}
			return transfer.TextData(out.String())
		}
		body.draw, body.copyFn = draw, copyFn
	}
	// 🔴 **选择的作用域给在「包着正文的那一格」上**（而不是正文自己）：框架的
	// `CopyData` 是**按作用域**找的（`mygo/ui/textselection.go` 的
	// `textSelectionCommand`：`sc := rt.states[s.textScope]`）——而正文自己
	// 带着 `Selectable` 时，作用域是**它自己**，`CopyData` 挂在它身上**不生效**
	// （框架走的是编辑���那一路，复制出来的是原文）。
	//
	// ⚠️ **包着的那一格必须是 `kindBox`**：框架找作用域时判的是
	// `p.kind == kindBox && p.flags&flagSelectable != 0`（`prepareSelectable`）。
	ui.Box(c).FillWidth().Selectable().
		// 🔴 **复制出去的是「给读屏的那一份」**（理由见
		// `commentPlainOfSelection`）：框架默认复制的是**画出来的那一份**，
		// 而那一枚 U+FFFC 复制出去是个**看不见的字符**。
		CopyData(copyFn).
		Children(func() {
			ui.RichText(c, spans...).
				FontSize(scale.Font(c, scale.Size14)).
				TextColor(c.Theme().Text).
				MaxLines(lines).
				Ellipsis("…").
				// ⚠️ **`.Label(...)` 给的是「给读屏的那一份」**（表情占位符写成
				// 「［表情］」）：屏幕阅读器读得到它，无头 Tester 也找得到它——
				// 而画出来的那一份里表情是图（`DrawObjects` 那一支）。
				//
				// 🔴 **它用的是缓存的那一份**（`body.plain`），不是现算的
				// `commentPlain(...)`：现算的话**每一帧**都要把正文再切一遍
				// （而这一页一屏十来条评论，每帧十来趟切分就是白烧 CPU）。
				//
				// ⚠️ **而它和画出来的那一份必然一致**（理由见 `commentSpansOf`
				// 的《两份必须同源》）：两者来自**同一次切分**。
				Label(body.plain).
				DrawObjects(draw)
		})
}

// commentObjectIndex 算第 `at` 个 rune 是**第几枚**表情占位符（-1 = 不是）。
//
// 🔴 **它数的是「占位符那一格的个数」**：框架给的 `at` 是**段落里的 rune
// 下标**，而正文里的占位符在 span 里各占**一个** rune——所以「第几个占位符」
// 就是数一遍**排在它前面、而自己也是一个占位符**的 rune 有几个。
//
// ⚠️ **它按 span 走**（不是按原文走）：占位符在 span 里是 U+FFFC，而普通
// 文字在 span 里是它自己——**只有占位符那一种 span 带 `ObjectWidth`**。
func commentObjectIndex(at int, spans []ui.Span) int {
	// 段落的第几个 rune 对应第几段 span。
	rune := 0
	obj := 0
	for _, sp := range spans {
		n := utf8.RuneCountInString(sp.Text)
		if rune+n > at {
			if sp.ObjectWidth > 0 {
				return obj
			}
			return -1
		}
		rune += n
		if sp.ObjectWidth > 0 {
			obj++
		}
	}
	return -1
}

// commentBodySpans 拿一条评论正文的那些 span（**排版之前**的那一份）。
//
// 🔴 **命中区间也在这一次算掉**（票 #684）：`songname.Table.Matches` 每条评论
// 跑一次就够，而这里是**唯一**需要它的地方——所以它是**算的时候**跑一趟，
// 不是每帧跑。筛选关着的时候**根本不跑**（高亮是筛选的一部分）。
//
// ⚠️ **它按 (rpid, 配色) 记一份**（`p.bodies`）：同一帧里一条评论只算一次
// span，而**跨帧**靠的是框架自己的布局缓存（`sameLayout`）——框架按 span 的
// **样式**（`text.EncodeSpans`）比对，样式没变就不重排。
func (s *Shell) commentBodyOf(p *commentsPage, cm bili.Comment) *commentBody {
	// 🔴 **高亮的配色一帧算一次**（`viewComments` 开头）：这里只是取过来用。
	mark := p.mark
	// 🔴 **缓存先查、匹配后跑**：`Matches` 每条评论分配十几次（它要拼一份归一化
	// 串 + 一张映射表），而这一页每一帧都要问一遍每一条评论——先查缓存的话，
	// 稳态下（正文排好了）它**一次都不跑**。
	if b, ok := p.bodies[cm.Rpid]; ok && b.mark == mark {
		return b
	}
	var hl []songname.Range
	if mark.Bg.A != 0 && !p.names.Empty() {
		hl = p.names.Matches(cm.Message)
	}
	spans, urls, plain := commentSpansOf(cm.Message, cm.Emotes, hl, mark)
	if p.bodies == nil {
		p.bodies = map[int64]*commentBody{}
	}
	body := &commentBody{spans: spans, urls: urls, plain: plain, mark: mark}
	p.bodies[cm.Rpid] = body
	return body
}
