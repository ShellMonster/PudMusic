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
//
// 🔴 **它走的是产品代码那一条路**（`commentPlain`）——而不是自己
// `ReplaceAll`：而 `commentPlain` 现在**和画出来的那一份同源**（同一次
// 切分，见 `comments_body.go` 的《两份必须同源》）⇒ 用例断言的「want」就是
// **产品真的会读出来的那一句**。
//
// ⚠️ **自己写一份 `ReplaceAll` 会让这条用例断错东西**：那一份的判定口径
// （替换表情表里每一个 token、不看边界）和产品的**不一样**（见
// `comments_body.go` 的《两份必须同源》）——于是用例会在产品已经修好之后
// 仍然红。
func commentPlainOf(cm bili.Comment) string {
	return commentPlain(cm.Message, cm.Emotes)
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
// 🔴 **判据分两半（而不是一半）**：
//
//	**拖选真的生效了**  剪贴板里**有**那段正文。
//	**按过那一枚之后，正文还得能选**  那一枚的按**没有**把「正文可选中」
//	                                 **整个废掉**。
//
// ⚠️ **为什么「楼中楼展开了」那一半单独断不出东西**：那一枚是**第三行**的控件，
// 而它**不在**正文那一格的祖先链上（框架的 `prepareSelectable` 的祖先链规则），
// 所以它**本来就**点得动——**有没有 `Selectable()` 都一样**（实测：变异测试）。
// ⇒ 所以那一半是**守卫**（它无论如何都变）。
func TestTheSelectableCommentBodyDoesNotStealTheReplyButton(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	s, tt := newCommentsShell(t, b, audioItem(), 1200, 800)

	// 🔴 **先在正文上拖选**：这一步让「正文可选中」真正在生效（框架的
	// `pressTextSelection` 接走了那一下）——**而这一下才是这一条要问的**。
	//
	// ⚠️ **拖的是「第一行」**（`dragOverFirstLine`）：那才是正文真正在画的那一行。
	first := s.app.Comments()[0]
	body := commentPlainOf(first)
	waitFrames(t, tt, "正文画出来", func() bool {
		_, ok := tt.Find(body)
		return ok
	})
	dragOverFirstLine(t, tt, body)

	// 🔴 **判据分两半**：
	//
	//	**拖选真的生效了**  剪贴板里**有**那段正文（否则下面那些都是空谈——
	//	                   一个压根没选中的正文，「按那一枚之后会怎样」根本
	//	                   问不到那个问题）。
	//	**按那一枚之后，正文仍然能选**  那一枚的按**没有**把「正文可选中」
	//	                   **整个废掉**（比如它把选择清掉了、或它把正文的
	//	                   命中抢了）。
	//
	// ⚠️ **「楼中楼展开了」那一半**留着（那是这一条最早的判据）——但它
	// **单独**断不出东西：那一枚是**第三行**的控件，而它**不在**正文那一格的
	// 祖先链上（框架的 `prepareSelectable` 的祖先链规则），所以它**本来就**
	// 点得动——**有没有 `Selectable()` 都一样**（实测：变异测试）。
	selected := copySelection(tt)
	if selected == "" {
		t.Fatalf("在正文上拖选之后剪贴板里**什么也没有**——" +
			"「正文可选中」压根没生效，后面那些判据就都在问一个不存在的问题")
	}

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

	// 🔴 **按过那一枚之后，正文还得能选**：这一枚是**第三行**的，它跟正文
	// 那一格**不是同一个东西**——而它按下去**不该把正文的选择**整个清掉。
	//
	// ⚠️ **这一半是**变异敏感**的那一半**：把正文那一格的 `Selectable()` 拔掉，
	// 「拖选生效」这一半就红了（剪贴板是空的）——所以这一条**断的是「正文
	// 真的可选中，而且按那一枚之后还可选中」**，而不是「那一枚还能按」。
	dragOverFirstLine(t, tt, body)
	if got := copySelection(tt); got == "" {
		t.Errorf("按过「共 52 条回复」之后，在正文上**又**拖选了一次，" +
			"剪贴板里却什么也没有——那一枚把「正文可选中」**整个废掉**了")
	}
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
	s, tt := newCommentsShell(t, b, mentionItem(), 1400, 1000)

	// 🔴 **先在正文上拖选**（理由同上面那一条）：这一步让「正文可选中」真正在
	// 生效——**而这一下才是这一条要问的**。
	first := s.app.Comments()[0]
	body := commentPlainOf(first)
	waitFrames(t, tt, "正文画出来", func() bool {
		_, ok := tt.Find(body)
		return ok
	})
	dragOverFirstLine(t, tt, body)

	// 🔴 **判据的第一半**：**拖选真的生效了**（剪贴板里有那段正文）——否则
	// 后面那些都是在问一个不存在的问题。
	if selected := copySelection(tt); selected == "" {
		t.Fatalf("在正文上拖选之后剪贴板里**什么也没有**——" +
			"「正文可选中」压根没生效，后面那些判据就都在问一个不存在的问题")
	}

	if err := tt.Click("开关：只看提到歌名的评论"); err != nil {
		t.Fatalf("点不了那枚开关：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "筛选开着", func() bool {
		return tt.HasText("条提到这首歌")
	})

	// 🔴 **按过那枚开关之后，正文还得能选**（理由同上面那一条）：那枚开关在
	// **页头**，而它在**按下去**的时候不该把**别处**（评论正文）的选择清掉。
	dragOverFirstLine(t, tt, body)
	if got := copySelection(tt); got == "" {
		t.Errorf("按过「只看提到歌名的评论」那枚开关之后，在正文上**又**拖选了一次，" +
			"剪贴板里却什么也没有——那枚开关把「正文可选中」**整个废掉**了")
	}
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

// TestPressingTheReplyButtonClearsTheSelection 钉住**按过「共 N 条回复」之后，
// 正文里的选区没了**。
//
// 🔴 **这一条钉的是框架自己的行为**（不是 #702 引入的）：框架的
// `pressTextSelection`（`mygo/ui/input.go` 的 `pointerDown` 先问它）在**按下**的
// 那一刻就把选区**接管**了——而那一枚按钮的按**也会**让框架走那一趟
// （`textSelectionAt` 的「就近」兜底会按**距离**找最近的段落）⇒ 选区被清掉。
//
// ⚠️ **为什么现在要把它钉住**：票 #702 的审查用例（
// `TestTheSelectableCommentBodyDoesNotStealTheReplyButton`）**间接依赖**它——
// 那一条要断言「按过那一枚之后，正文**还得能选**」，而这中间隔着「按之前选区
// 还在」这一步。⇒ 它得是**明确的**（这条用例）而不是**碰巧**的。
//
// ⚠️ **而它不是缺陷**：按了按钮之后选区没了，是**正常的**（用户点了别处）——
// 框架的规矩。而**正文仍然可选**（再拖一次就有），那才是 #702 要保证的事。
func TestPressingTheReplyButtonClearsTheSelection(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	s, tt := newCommentsShell(t, b, audioItem(), 1200, 800)

	first := s.app.Comments()[0]
	body := commentPlainOf(first)
	waitFrames(t, tt, "正文画出来", func() bool {
		_, ok := tt.Find(body)
		return ok
	})

	// 🔴 **先选中一段**：不选中就没有「选区」可言。
	dragOverFirstLine(t, tt, body)
	if got := copySelection(tt); got == "" {
		t.Fatalf("在正文上拖选之后剪贴板里什么也没有——「正文可选中」压根没生效")
	}

	// 🔴 **按那一枚**：框架会把选区接管掉 ⇒ 选区没了。
	if err := tt.Click("共52条回复"); err != nil {
		t.Fatalf("点不动「共 52 条回复」：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	if got := copySelection(tt); got != "" {
		t.Errorf("按过「共 52 条回复」之后剪贴板里**还有** %q",
			got)
		t.Errorf("而框架的 `pressTextSelection` 是**接管**选区的" +
			"（按下的那一下就把选区拿走了）⇒ 这里应该是空的")
	}

	// 🔴 **而正文仍然可选**（再拖一次就有）——那才是 #702 要保证的事。
	dragOverFirstLine(t, tt, body)
	if got := copySelection(tt); got == "" {
		t.Errorf("按过「共 52 条回复」之后，在正文上**又**拖选了一次，" +
			"剪贴板里却什么也没有——「正文可选中」被那一枚**废掉**了")
	}
}
