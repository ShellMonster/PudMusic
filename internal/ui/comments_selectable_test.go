// 「评论正文能拖选复制」（票 #702）的界面验收。
//
// # 这一族断的是**剪贴板里的原文**
//
// 用户看到的症状是「正文一个字都复制不出来」——而判据是**剪贴板**：
// 先 `SetClipboard("")` 清空，再拖选，再 `Command("copy")`，最后读
// `Clipboard()`。只断「元素上有没有 Selectable」是断实现，不是断行为。
//
// # 为什么正文和昵称是两条独立的路
//
// 昵称那一半在票 #685 就做了（`selectable_copy_test.go` 里的
// `TestDraggingACommentNameCopiesIt`），而正文那一半是这一票。正文比昵称难
// 三处，每一处都单独一条用例：
//
//	表情   正文里的 `[妙啊]` 是一枚 inline object（U+FFFC），**不是五个字**
//	       ——复制出来的是「［表情］」那一格（见 `commentPlain`），而画面上
//	       是那张图。
//	截断   超过几行的正文被折起来，末尾是省略号——**折起来之后那部分根本
//	       不在画面上**，复制出来的自然是折起来的那一段。
//	高亮   「只看提到歌名」开着的时候，正文里命中的那几段染着底色，而**高亮
//	       是装饰**：复制出来的还是那句话（不带任何底色标记）。
//
// # 一个真请求都不发
//
// 数据走一台**假 B 站**（`newCommentBili`），回的是**真接口录下来的 fixture**
// ——理由同 `comments_test.go` 的文件头。
package ui

import (
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// commentPlainOf 是「这一条评论给读屏软件的那一份正文」。
//
// ⚠️ **它跟画出来的那一份不是同一串字**：表情占位符在画出来的那一份里是
// 一枚 U+FFFC（inline object），而给读屏的那一份是「［表情］」四个字
// （`commentPlain`）。
//
// ⚠️ **占位符不认死一个**：B 站给的那张表是**每条评论自己一份**
// （`Comment.Emotes`），而占位符本身长得五花八门（`[妙啊]` / `[大哭]` /
// `[间奏时光表情包_哭]`）——照抄某一个字面量的话，认不出的那几枚就留在正文
// 里（而它们在画面上是图）。
func commentPlainOf(cm bili.Comment) string {
	out := cm.Message
	for token := range cm.Emotes {
		out = strings.ReplaceAll(out, token, "［表情］")
	}
	return out
}

// TestDraggingACommentBodyCopiesIt 断**评论正文能拖选复制**。
//
// 🔴 **这是这一票的主判据**：正文那一格是**可拖选的**，而复制出来的是**正文
// 原文**（不是昵称、不是空串）。
//
// ⚠️ **判据是「剪贴板里正是那句正文」**：拖选的起止两端都取 `Find` 量出来的
// 盒子（不按行高算坐标、不猜字符宽度——理由同 `dragOver`）。
func TestDraggingACommentBodyCopiesIt(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	s, tt := newCommentsShell(t, b, audioItem(), 1200, 800)

	first := s.app.Comments()[0]
	body := commentPlainOf(first)
	if body == "" {
		t.Fatalf("夹具里第一条评论没有正文")
	}
	waitFrames(t, tt, "正文画出来", func() bool {
		_, ok := tt.Find(body)
		return ok
	})

	dragOverFirstLine(t, tt, body)
	if got := copySelection(tt); got != body {
		t.Errorf("拖选评论正文之后剪贴板里是 %q，want %q", got, body)
	}
}

// TestDraggingACommentBodyWithAnEmoteCopiesThePlainText 断**带表情的正文
// 也能复制**——而复制出来的是**给读屏的那一份**（表情占位符换成「［表情］」）。
//
// 🔴 **为什么判据是「［表情］」而不是「[妙啊]」**：表情在画面上是一张图
// （inline object），而那一格在**文字里**是 U+FFFC——那是个不可见的字符，
// 复制出去对用户毫无意义。给读屏的那一份把它写成「［表情］」，用户复制出去
// 至少知道这里有个表情。
//
// ⚠️ **占位符是**这一条自己的**：B 站给的那张表情表是每条评论一份，而占位符
// 长得五花八门——所以这里从 `Comment.Emotes` 里取它自己那一个。
func TestDraggingACommentBodyWithAnEmoteCopiesThePlainText(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	s, tt := newCommentsShell(t, b, audioItem(), 1200, 800)

	// 找一条带表情的评论（夹具里主评论和楼中楼都有）。
	var want, token string
	for _, cm := range s.app.Comments() {
		for tk := range cm.Emotes {
			want, token = commentPlainOf(cm), tk
			break
		}
		if want != "" {
			break
		}
	}
	if want == "" {
		t.Fatalf("夹具里一条带表情的评论都没有")
	}
	waitFrames(t, tt, "带表情的正文画出来", func() bool {
		_, ok := tt.Find(want)
		return ok
	})

	dragOverFirstLine(t, tt, want)
	if got := copySelection(tt); got != want {
		t.Errorf("拖选带表情的正文之后剪贴板里是 %q，want %q", got, want)
	}
	// 🔴 **字面量不许出现在剪贴板里**：那是「表情成图」那条验收直接破掉的
	// 形状（用户复制出去看到的是「[大哭]」那几个字，而画面上是一张图）。
	if got := copySelection(tt); strings.Contains(got, token) {
		t.Errorf("剪贴板里留着表情的字面量 %q（剪贴板是 %q）", token, got)
	}
}

// TestDraggingAReplyBodyCopiesIt 断**楼中楼那一档同样能选**。
//
// ⚠️ **它走的是另一个调用点**（`viewCommentReplyRow` → `viewCommentBodyEl`，
// 只是行数不同：主评论 6 行、楼中楼 4 行）。漏掉楼中楼的症状是「主评论能选，
// 展开的回复一个字都复制不出来」。
func TestDraggingAReplyBodyCopiesIt(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	s, tt := newCommentsShell(t, b, audioItem(), 1200, 800)

	// 展开第一梯楼中楼。
	if err := tt.Click("共52条回复"); err != nil {
		t.Fatalf("点不动「共 52 条回复」：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "楼中楼画出来了", func() bool {
		for _, row := range s.comments.rows {
			if row.reply && row.cm.Message != "" {
				return true
			}
		}
		return false
	})

	// ⚠️ **正文从**行**上取**（不是从应用层那份取）：行上的那一份才是画出来的
	// 那一句，而画出来的那一句已经把表情占位符换成「［表情］」了。
	var want string
	for _, row := range s.comments.rows {
		if row.reply && row.cm.Message != "" {
			want = commentPlainOf(row.cm)
			break
		}
	}
	if want == "" {
		t.Fatalf("楼中楼一条正文都没有")
	}
	waitFrames(t, tt, "楼中楼的正文画出来", func() bool {
		_, ok := tt.Find(want)
		return ok
	})

	dragOverFirstLine(t, tt, want)
	if got := copySelection(tt); got != want {
		t.Errorf("拖选楼中楼的正文之后剪贴板里是 %q，want %q", got, want)
	}
}

// TestTheSelectableCommentBodyDoesNotStealTheReplyButton 断**加了 Selectable
// 之后，」共 N 条回复」那一枚还点得动**。
//
// 🔴 **这是这一票最容易做坏的地方**：框架的 `pressTextSelection`
// （`mygo/ui/input.go` 的 `pointerDown` 先问它）会把**按下**先接走——而评论
// **行不是可点的**（外面是裸 `ui.List`，行画法自己写），所以正文那一格加上
// `Selectable` 之后，按在正文上的那一下有可能被「选字」吃掉。
//
// ⇒ 框架的 `prepareSelectable`（`mygo/ui/textselection.go:45-54`）有一条精确
// 规则：祖先链上遇到 `flagClickable|flagFocusable|flagEditable|flagUnselectable`
// 或 `inputFn != nil` 就停——**控件内部的文字不会被抢走**。而「共 N 条回复」
// 那一枚是 `flat.ButtonContent`（一个可点的控件），它**不在**正文那一格的祖先
// 链上，所以正文可选中不该影响它。
//
// ⚠️ **判据是「点了之后楼中楼真的展开了」**（不是「按钮还在」）。
func TestTheSelectableCommentBodyDoesNotStealTheReplyButton(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	s, tt := newCommentsShell(t, b, audioItem(), 1200, 800)

	if err := tt.Click("共52条回复"); err != nil {
		t.Fatalf("点不动「共 52 条回复」：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "楼中楼展开了", func() bool {
		for _, row := range s.comments.rows {
			if row.reply {
				return true
			}
		}
		return false
	})
}

// TestTheSelectableCommentBodyDoesNotStealTheMentionSwitch 断**加了 Selectable
// 之后，页头那一枚「只看提到歌名的」还按得动**。
//
// ⚠️ **它在页头，不在正文那一格里**——但它是一个**开关**（`springSwitch`），
// 而开关是控件（框架的规矩：「Text inside controls keeps the control's
// interaction」）。这一条是这一票的**另一半**防线。
//
// ⚠️ **判据是「筛选真的开着」**（页头那句话变成「N 条提到这首歌」）。
func TestTheSelectableCommentBodyDoesNotStealTheMentionSwitch(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	b.mainRaw = commentFixtureMentionNoMore(t, "晴天")
	_, tt := newCommentsShell(t, b, mentionItem(), 1400, 1000)

	if err := tt.Click("开关：只看提到歌名的评论"); err != nil {
		t.Fatalf("点不了那枚开关：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "筛选开着", func() bool {
		return tt.HasText("条提到这首歌")
	})
}

// TestCopyingACommentBodyBeatsTheSelectedRow 钉住「拖选正文之后 ⌘C 复制的是
// 那段正文，不是别的东西」。
//
// ⚠️ **这一条防的是「焦点被抢走」**：拖选之后焦点落在正文那一段上（框架的
// `pressTextSelection` 干的），而评论页自己不发任何写接口，所以这一页上
// 没有别的东西会跟它抢 ⌘C——但**页头那一枚开关**和**楼中楼那一枚按钮**是
// 控件，控件的按键归控件自己。
//
// ⚠️ **判据是剪贴板的内容**：清空 → 拖选 → 复制 → 读。
func TestCopyingACommentBodyBeatsTheSelectedRow(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	s, tt := newCommentsShell(t, b, audioItem(), 1200, 800)

	first := s.app.Comments()[0]
	body := commentPlainOf(first)
	waitFrames(t, tt, "正文画出来", func() bool {
		_, ok := tt.Find(body)
		return ok
	})

	dragOverFirstLine(t, tt, body)
	if got := copySelection(tt); got != body {
		t.Errorf("拖选正文之后复制出来的是 %q，want %q", got, body)
	}
}
