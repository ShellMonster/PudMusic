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

// TestCopyingASelectionStillBeatsTheSelectedRow 是「别抢」那一条：
// **拖选了头图区的字之后，⌘C 复制的是那段字**，不是列表里选中的那一行。
//
// 🔴 **这条是方案的关键判据**：列表行的 ⌘C 挂在列表的 `HandleInput` 上
// （`widgets/list/copy_title.go`），而框架只在**焦点那个元素**上派发按键 /
// 编辑命令。拖选文字之后焦点落在那一段字上（`pressTextSelection` 做的），
// 所以列表那一条**根本收不到**——这一条钉住的就是这个顺序。
//
// ⚠️ **先选中一行**（让「列表有一条可复制的行」这个前提成立）：不选的话
// 这条测试会因为「列表本来就没东西可复制」而绿，验不到抢不抢。
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

// TestClickingTheHeaderAroundItsTextStillChoosesTheFirstSong 钉住「文字可选中
// 之后，头图区**其余那一片**还是列表的第 0 行」。
//
// 🔴 **这条解决的是一个真实副作用**：头图区那几行字变成可选之后，**点在字上**
// 的那一下被文本选择接走了（框架的 `pressTextSelection` 先于行的 `clickAt`），
// 于是 `List.Selected` 不再被推到第一首歌。而头图区**其余的地方**（封面旁边、
// 字与字之间的留白、左右内边距）该照旧——那里的点击仍然落到第 0 行上，而
// `List.View` 会把第 0 行修正成第一首歌（见 `list.go` 的 `l.sel = leadRows`）。
//
// ⚠️ **为什么「点在字上不选第一首」是可接受的**：头图区是**布局**，不是一条
// 歌——用户点在标题上想的是「看看这个页头」或者「复制这个名字」，不是「我要
// 选中这一页的第一首」。而选中在票 #197 之后**画面上什么都没有**（不刷底色），
// 所以用户也看不到这一下丢了。
func TestClickingTheHeaderAroundItsTextStillChoosesTheFirstSong(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)
	waitFrames(t, tt, "头图区画出来", func() bool {
		_, ok := tt.Find("通勤路上")
		return ok
	})

	r, ok := tt.Find("通勤路上")
	if !ok {
		t.Fatalf("画面上找不到标题，文字有 %q", tt.Texts())
	}
	// 标题左边那一块留白（内边距里、不在任何字上）。
	tt.ClickAt(r.X-14, r.Y+r.H/2)
	tt.Frame()

	if got, ok := s.collection.list.Selected(); !ok || got.Title != "歌 一" {
		t.Errorf("点页头文字旁边的留白之后选中的是 (%q, %v)，want 歌 一（列表第 0 行）",
			got.Title, ok)
	}
}
