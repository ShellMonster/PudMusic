// 「评论正文 / 歌单标题 / 列表标题不能选中复制」这一票（#685）的界面验收。
//
// # 这一族断的是**拖一下能不能复制出去**
//
// 用户看到的症状是「拖选之后按 Cmd+C，剪贴板里什么都没有」——所以判据是
// **剪贴板的原文**：先 `SetClipboard("")` 清空，再拖选，再 `Command("copy")`，
// 最后读 `Clipboard()`。只断「元素上有没有 Selectable」是断实现，不是断行为。
//
// # 为什么歌单头图区是**容器级** Selectable
//
// 头图区那几行字（标题 / 简介 / 类型和条数）是一列，用户想复制的是**一整段**
// ——所以选择作用域给在包着它们的那一层上（`mygo/ui/api_gen.go` 的
// `Selectable`：「On a container, its Text and RichText descendants share one
// selection. Copy joins their selected text with newlines, in the order they
// were built.」）。这一条里那条「从标题拖到简介」就是钉这个的。
//
// # 可点的那些东西**不许**被 Selectable 抢走
//
// 头图区里有三样是能点的：创建者的名字（名字型链接）、收藏星标、⋯ 菜单。
// 前两样是**控件**（框架的规矩：「Text inside controls, such as buttons and
// text inputs, keeps the control's interaction」），第三样在操作条上、压根不在
// 这一块里。这一条钉的是**点了还有效**——这也是这一票最容易做坏的地方。
package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// dragOver 用指针把 from 那一块从左沿拖到 to 那一块的右沿。
//
// ⚠️ **不按行高算坐标、不猜字符宽度**：两端都取 `Find` 量出来的盒子，落在
//
//	起点：from 的左沿 +1（行内第一个字之前）
//	终点：to 的右沿 -1（行内最后一个字之后）
//
// ——这样「整段都选中」不依赖字体度量。
func dragOver(t *testing.T, tt *ui.Tester, from, to string) {
	t.Helper()
	a, ok := tt.Find(from)
	if !ok {
		t.Fatalf("画面上找不到 %q，文字有 %q", from, tt.Texts())
	}
	b, ok := tt.Find(to)
	if !ok {
		t.Fatalf("画面上找不到 %q，文字有 %q", to, tt.Texts())
	}
	if a.W <= 0 || a.H <= 0 || b.W <= 0 || b.H <= 0 {
		t.Fatalf("%q / %q 没量出大小（%v / %v）——按不中", from, to, a, b)
	}
	x0, y0 := a.X+1, a.Y+a.H/2
	x1, y1 := b.X+b.W-1, b.Y+b.H/2
	tt.Press(x0, y0)
	tt.Move(x1, y1)
	tt.Release(x1, y1)
	tt.Frame()
}

// dragOverFirstLine 用指针把 from 那一块的**整段**从左沿拖到右沿。
//
// 🔴 **为什么要有这一支**（而不用 `dragOver`）：`dragOver` 落点是
// **盒子正中**（`a.Y + a.H/2`），而一段**折成两行**的正文，它的盒子里**两行
// 各占一半**——于是起点落在**第二行**上，而终点落在第二行的末尾。
// ⇒ 选出来的是**第二行**，而不是整段（实测：86 个字的正文选出来只有 20 个）。
//
// ⚠️ **落点是「盒子的上沿往下几像素」**：上半格一定是第一行——所以取
// `a.Y + a.H/4`，它落在第一行的行内，而**不依赖**字体度量。
//
// 🔴 **而「a.H/4」那个判据只对**两行**的正文成立**（票 #706 改了这件事）：
//
//	两行  第一行占上半格 ⇒ `a.H/4` 落在第一行里 ✓
//	四行  第一行只占**四分之一** ⇒ `a.H/4` 落在**第二行**里 ✗
//
// ⇒ 而评论从**独立页**搬进**右侧抽屉**（400 宽）之后，正文那一列从 904 宽
// 变成 323 宽 ⇒ 同一段 86 个字的正文**从两行折成四行** ⇒ 起点落在第二行 ⇒
// 选出来的是**第二行往后**（症状：「复制出来的那一句**开头没了**」）。
//
// ⚠️ **所以起点改成「上沿往下固定几个像素」**：一行正文有十几像素高（14 号
// 字），而盒子顶上那几像素**一定**在第一行里 ⇒ **和行数无关**。
//
// ⚠️ **终点落在盒子的**下面**一行**：一段折行的文字，它的**盒子右沿**是**最长
// 那一行**的右沿（而那一行可能是**第二行**）——所以「拖到右沿」选到的是
// **第二行的末尾**，不是整段。
//
// ⇒ 终点取「盒子的下沿再往下 2 像素、起点那一列」：框架的命中测试把**下面
// 那一行**算进去（`textHit` 的 `ly >= ed.originY+ed.layout.Height` ⇒ 末尾），
// 于是选出来的是**整段**（实测：86 个字，一个不差）。
func dragOverFirstLine(t *testing.T, tt *ui.Tester, from string) {
	t.Helper()
	a, ok := tt.Find(from)
	if !ok {
		t.Fatalf("画面上找不到 %q，文字有 %q", from, tt.Texts())
	}
	if a.W <= 0 || a.H <= 0 {
		t.Fatalf("%q 没量出大小（%v）——按不中", from, a)
	}
	// 🔴 **起点落在第一行里**（理由见上面那一段）：上沿往下 2 像素。
	const firstLineInset = 2
	y0 := a.Y + firstLineInset
	if y0 >= a.Y+a.H {
		y0 = a.Y + a.H/2
	}
	x0, y1 := a.X+1, a.Y+a.H+2
	tt.Press(x0, y0)
	tt.Move(x0, y1)
	tt.Release(x0, y1)
	tt.Frame()
}

// copySelection 清空剪贴板、走一次应用的「复制」命令，返回剪贴板里的字。
func copySelection(tt *ui.Tester) string {
	tt.SetClipboard("")
	tt.Command("copy")
	return tt.Clipboard()
}

// TestDraggingTheCollectionHeaderCopiesTheTitle 断**歌单页头的大标题能拖选复制**。
func TestDraggingTheCollectionHeaderCopiesTheTitle(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)
	waitFrames(t, tt, "头图区画出来", func() bool {
		_, ok := tt.Find("通勤路上")
		return ok
	})

	dragOver(t, tt, "通勤路上", "通勤路上")
	if got := copySelection(tt); got != "通勤路上" {
		t.Errorf("拖选歌单标题之后剪贴板里是 %q，want %q", got, "通勤路上")
	}
}

// TestDraggingAcrossTheCollectionHeaderCopiesEveryLine 断**从头拖到尾一次复制
// 多行**——这是「容器级 Selectable」那个选择本身的验收。
//
// 逐行各自 Selectable 的话，拖到第二行时第一行会掉出去（作用域不同），
// 用户得复制三次。
func TestDraggingAcrossTheCollectionHeaderCopiesEveryLine(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)
	waitFrames(t, tt, "头图区画出来", func() bool {
		_, ok := tt.Find("早上听")
		return ok
	})

	dragOver(t, tt, "通勤路上", "早上听")
	want := "通勤路上\n早上听"
	if got := copySelection(tt); got != want {
		t.Errorf("从标题拖到简介之后剪贴板里是 %q，want %q", got, want)
	}
}

// TestDraggingACommentNameCopiesIt 断**评论昵称能拖选复制**。
//
// ⚠️ **只做昵称，不做正文**：正文那一半（`shapeCommentBody`）与
// `work/684-highlight` 在同一个函数上加「歌名命中高亮」，两个 PR 会硬冲突——
// 正文的改造单独一票（见这一票的收尾报告）。
func TestDraggingACommentNameCopiesIt(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	s, tt := newCommentsShell(t, b, audioItem(), 1200, 800)

	first := s.app.Comments()[0]
	if first.Uname == "" {
		t.Fatalf("夹具里第一条评论没有昵称")
	}
	waitFrames(t, tt, "昵称画出来", func() bool {
		_, ok := tt.Find(first.Uname)
		return ok
	})

	dragOver(t, tt, first.Uname, first.Uname)
	if got := copySelection(tt); got != first.Uname {
		t.Errorf("拖选评论昵称之后剪贴板里是 %q，want %q", got, first.Uname)
	}
}

// TestTheSelectableCollectionHeaderStillTakesItsClicks 是这一票的回归大项：
// **头图区加了 Selectable 之后，里面那几样能点的东西还能点**。
//
// 🔴 **按逐样本拆子测试**：名字一点就离开这一页（跳到 `/user/{mid}`），
// 所以同一副外壳里点不了第二样。三样各造一副，判据都是「点了之后那件事
// 真的发生了」——不是「元素还在」。
func TestTheSelectableCollectionHeaderStillTakesItsClicks(t *testing.T) {
	t.Run("创建者的名字跳到他的空间", func(t *testing.T) {
		b := newCollectionBili(t)
		b.setMedias(mediaOf(1, "歌 一"))
		b.infos["upper"] = map[string]any{"mid": 7, "name": "UP 小明"}
		_, s, tt := collectionShellFrom(t, b, nil, "", "12345")
		waitSongs(t, tt, s, 1)
		tt.Frame()

		if err := tt.Click("UP 小明"); err != nil {
			t.Fatalf("点不到创建者的名字：%v，文字有 %q", err, tt.Texts())
		}
		tt.Frame()
		if got := s.router.Path(); got != "/user/7" {
			t.Errorf("点了创建者的名字之后在 %q，want /user/7", got)
		}
	})

	t.Run("收藏星标把收藏夹收藏了", func(t *testing.T) {
		_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"))
		waitSongs(t, tt, s, 1)
		tt.Frame()

		if err := tt.Click("收藏这个收藏夹"); err != nil {
			t.Fatalf("点不到星标：%v，文字有 %q", err, tt.Texts())
		}
		waitFrames(t, tt, "收藏成功", func() bool {
			return s.app.Collection(s.collection.ref).Faved && tt.HasText("已收藏这个收藏夹")
		})
	})

	t.Run("⋯ 菜单开得出来", func(t *testing.T) {
		_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"))
		waitSongs(t, tt, s, 1)
		tt.Frame()

		if err := tt.Click("更多操作"); err != nil {
			t.Fatalf("点不到 ⋯：%v，文字有 %q", err, tt.Texts())
		}
		settleFrame(tt)
		got := tt.Menu()
		if !slicesContains(got, "下载全部音频") {
			t.Errorf("⋯ 菜单里没有「下载全部音频」：%q", got)
		}
		tt.CloseMenu()
	})
}

// TestTheCollectionHeaderDoesNotSelectItsOwnerLink 是上一条的另一面：创建者
// 那个名字是**控件**，拖它不该变成选字——框架的规矩是「Text inside controls
// keeps the control's interaction」。
//
// ⚠️ **判据是「拖完按复制，剪贴板里不是那个名字」**：选中了的话剪贴板里就是它。
// 只断「点得动」的话，一个「既能选中又还能点」的半坏实现照样绿。
func TestTheCollectionHeaderDoesNotSelectItsOwnerLink(t *testing.T) {
	b := newCollectionBili(t)
	b.setMedias(mediaOf(1, "歌 一"))
	b.infos["upper"] = map[string]any{"mid": 7, "name": "UP 小明"}
	_, s, tt := collectionShellFrom(t, b, nil, "", "12345")
	waitSongs(t, tt, s, 1)
	tt.Frame()

	dragOver(t, tt, "UP 小明", "UP 小明")
	if got := copySelection(tt); got == "UP 小明" {
		t.Errorf("创建者的名字被拖成了可选中文本（剪贴板里是 %q）——它是链接，拖它不该选字", got)
	}
}

// TestCmdCCopiesTheSelectedRowTitleInTheCollectionPage 是列表那一档的**端到端**
// 一条：在真的歌单页里点一行、按 ⌘C，剪贴板里就该是那一行的歌名。
//
// ⚠️ **和 `widgets/list` 的 `TestCmdCCopiesTheSelectedRowTitle` 不重复**：那一条
// 验的是控件自己的回调，这一条验的是**整条链**——控件的 `Actions.Copy` 接的是
// `Shell.wake.writeClipboard`（真窗口那一档是系统剪贴板），中间还隔着
// `listActions` 那一层接线。少接一根线，两条里只有这一条会红。
func TestCmdCCopiesTheSelectedRowTitleInTheCollectionPage(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)
	waitFrames(t, tt, "第二首画出来", func() bool {
		_, ok := tt.Find("歌 二")
		return ok
	})

	r, ok := tt.Find("歌 二")
	if !ok {
		t.Fatalf("画面上找不到第二首，文字有 %q", tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if got, ok := s.collection.list.Selected(); !ok || got.Title != "歌 二" {
		t.Fatalf("点第二首之后选中的是 (%q, %v)，want 歌 二", got.Title, ok)
	}

	tt.SetClipboard("")
	tt.Key(ui.Cmd, ui.KeyC)
	tt.Frame()

	if got := tt.Clipboard(); got != "歌 二" {
		t.Errorf("选中一行按 ⌘C 之后剪贴板里是 %q，want %q", got, "歌 二")
	}
}

// TestCopyingASelectionStillBeatsTheSelectedRow 钉住「拖选之后 ⌘C 复制的是那段
// 字，不是列表里选中的那一行」。
//
// ⚠️ **这条测的不是「优先级」，而是「框架只把按键派给焦点元素」**：
// `keyDownPhysical` 和 `editEvent` 都走 `focusHandler()`——拖选之后焦点落在
// 那一段字上（`pressTextSelection` 干的），列表那一层的 `HandleInput` 根本
// 收不到这条命令。实现里**没有**任何「谁优先」的判断；页面级 `OnShortcut` 抢、
// 列表自己挂 `OnShortcut` 直接写，这条测试**都照样绿**（对抗性审查实测过）。
//
// 🔴 **真正守住「列表那一行还能被 ⌘C 复制」的是
// `TestCmdCCopiesTheSelectedRowTitleInTheCollectionPage`**：它点一行、按 ⌘C，
// 断剪贴板里是那一行的歌名。少了那一半，「列表的 ⌘C 从未接上」也会让这一条绿。
//
// ⚠️ **先选中一行**（让「列表有一条可复制的行」这个前提成立）：不选的话
// 这条测试会因为「列表本来就没东西可复制」而绿，验不到那一半。
func TestCopyingASelectionStillBeatsTheSelectedRow(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)
	waitFrames(t, tt, "头图区画出来", func() bool {
		_, ok := tt.Find("通勤路上")
		return ok
	})

	r, ok := tt.Find("歌 二")
	if !ok {
		t.Fatalf("画面上找不到第二首，文字有 %q", tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if got, ok := s.collection.list.Selected(); !ok || got.Title != "歌 二" {
		t.Fatalf("先摆好的前提没成立：选中的是 (%q, %v)", got.Title, ok)
	}

	dragOver(t, tt, "通勤路上", "通勤路上")
	if got := copySelection(tt); got != "通勤路上" {
		t.Errorf("拖选了标题之后 ⌘C 复制出来的是 %q，want %q（列表那一行的 ⌘C 抢了）",
			got, "通勤路上")
	}
}

// TestDoubleClickingInsideTheHeaderColumnStillPlaysTheFirstSong 钉住「文字可选中
// 之后，页头那一列里**双击仍然播第一首**」。
//
// 🔴 **这是对抗性审查抓到的真实回归**：文字那一列加了 `Selectable` 之后，框架的
// `pressTextSelection` 把这**整列**上的按下先接走（`mygo/ui/input.go` 的
// `pointerDown` 先问它，而 `textSelectionAt` 对列内的空白会**就近吸到最近那一段
// 字**上）——于是行的 `OnSubmit` 永远不触发，双击播放**整列都死了**（1200 宽的
// 窗口下实测死区是 x≈472..1180、y≈66..245 的矩形，含标题右侧空白、行距、简介
// 下方、类型行）。基线那一整片是「双击播第一首」。
//
// ⚠️ **判据是队列真的长了一首、而且那一首是第一首**（不是「某个回调被调了」）。
func TestDoubleClickingInsideTheHeaderColumnStillPlaysTheFirstSong(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)
	waitFrames(t, tt, "头图区画出来", func() bool {
		_, ok := tt.Find("通勤路上")
		return ok
	})

	title, ok := tt.Find("通勤路上")
	if !ok {
		t.Fatalf("画面上找不到标题，文字有 %q", tt.Texts())
	}
	// 列内的空白：标题右边 200 像素（同一行上、不在任何字上）。
	x, y := title.X+title.W+200, title.Y+title.H/2

	before := len(s.app.Queue().Items())
	tt.ClickAt(x, y)
	tt.ClickAt(x, y)
	for range 8 {
		tt.Frame()
	}
	got := s.app.Queue().Items()
	if len(got) != before+1 {
		t.Fatalf("页头列内双击之后队列有 %d 首，want %d（双击该播第一首）：%v",
			len(got), before+1, titlesOf(got))
	}
	if got[len(got)-1].Title != "歌 一" {
		t.Errorf("双击页头列内放的是 %q，want 第一首 %q（全部：%v）",
			got[len(got)-1].Title, "歌 一", titlesOf(got))
	}
}

// TestClickingInsideTheHeaderColumnSelectsTheFirstSong 钉住「列内单击**仍然
// 选中第 0 行**」。
//
// ⚠️ **这条原来是点在标题左边 14 像素**（可选中那一列**外面**），于是去掉
// `.Selectable()` 它照样绿——恒真。现在点在列内（标题右边 200，同一行上、
// 不在任何字上），而基线那一整片都是「点一下选中第 0 行」。
//
// ⚠️ **判据是 `Selected`**：选中在票 #197 之后画面上什么都没有，但它决定
// 「⌘C 复制哪一行」（`TestCmdCCopiesTheSelectedRowTitleInTheCollectionPage`），
// 也决定方向键从哪儿开始走。
//
// ⚠️ **先选第二首**：不先弄脏的话，第 0 行本来就可能是选中的（第一帧的默认），
// 那这一条就验不到「点这一下把它挪回去了」。
func TestClickingInsideTheHeaderColumnSelectsTheFirstSong(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)
	waitFrames(t, tt, "头图区画出来", func() bool {
		_, ok := tt.Find("通勤路上")
		return ok
	})

	second, ok := tt.Find("歌 二")
	if !ok {
		t.Fatalf("画面上找不到第二首，文字有 %q", tt.Texts())
	}
	tt.ClickAt(second.X+second.W/2, second.Y+second.H/2)
	tt.Frame()
	if got, ok := s.collection.list.Selected(); !ok || got.Title != "歌 二" {
		t.Fatalf("先摆好的前提没成立：选中的是 (%q, %v)", got.Title, ok)
	}

	title, ok := tt.Find("通勤路上")
	if !ok {
		t.Fatalf("画面上找不到标题，文字有 %q", tt.Texts())
	}
	// 列内、不在任何字上：标题右边 200 像素。
	tt.ClickAt(title.X+title.W+200, title.Y+title.H/2)
	tt.Frame()

	if got, ok := s.collection.list.Selected(); !ok || got.Title != "歌 一" {
		t.Errorf("点页头列内之后选中的是 (%q, %v)，want 歌 一（列表第 0 行）",
			got.Title, ok)
	}
}

// TestDraggingInsideTheHeaderColumnStillCopiesIt 钉住「加了双击那一路之后，
// 列内的拖选复制**没有被弄坏**」。
//
// 🔴 **两条手势要同时活着**：只修双击（比如把整列改成不可选）会让拖选复制
// 回到「一个字都复制不出来」；只留 Selectable 则双击播不了。这一条和上面那条
// 一起把那一列钉死。
func TestDraggingInsideTheHeaderColumnStillCopiesIt(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)
	waitFrames(t, tt, "头图区画出来", func() bool {
		_, ok := tt.Find("通勤路上")
		return ok
	})

	dragOver(t, tt, "通勤路上", "早上听")
	want := "通勤路上\n早上听"
	if got := copySelection(tt); got != want {
		t.Errorf("列内从标题拖到简介之后剪贴板里是 %q，want %q", got, want)
	}
}
