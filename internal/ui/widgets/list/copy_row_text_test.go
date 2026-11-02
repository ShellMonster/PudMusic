// 「拖选一行复制整行」这一族（票 #703 的 A）。
//
// # 这一族断的是**拖一下能复制出去什么**
//
// 判据是**剪贴板里的原文**：先 `SetClipboard("")` 清空，再拖选，再走一次
// 「复制」，最后读 `Clipboard()`。只断「元素上有没有 Selectable」是断实现，
// 不是断行为——而这一族要的正是行为。
//
// ⚠️ **剪贴板是框架那一份**（`tt.Clipboard()`）：框架自己的复制（可选中文字
// 的 ⌘C / 「编辑 › 拷贝」）写的就是它，而 `acts.Copy` 写的是**应用**那一份
// （真机上就是系统剪贴板）。这里断的是**用户按了 ⌘C 之后剪贴板里是什么**，
// 所以断框架那一份。
//
// # 三个手势要同时活着
//
// 这一族一次钉三件事（它们是同一块地上的三个手势，坏一个另两个还在）：
//
//	单击标题	那一行被选中（列表那一档）
//	双击标题	那一首开始播（列表那一档）
//	拖选标题	剪贴板里是**整行**（标题 + UP 名 + 时长）
//
// 🔴 **前两件事是这一列最要紧的两种手势**（票 #685 的注释）：加了 `Selectable`
// 之后按下会被框架的 `pressTextSelection` 接走（`mygo/ui/input.go` 的
// `pointerDown` 先问它），所以它们必须被单独钉住。
package list

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo/ui"
)

// rowTextSongs 是这一族用的两首歌：歌名 + UP 名 + 时长都有。
//
// ⚠️ **UP 名和时长都要有**：整行复制的那一段是「标题 + UP 名 + 时长」
// （Biu 上游那一行就是这几列），少一样就复制不出那一整段。
func rowTextSongs() []Music {
	return []Music{
		{ID: "a", Title: "第一首歌", Owner: "UP 甲", Duration: 185 * 1e9,
			Item: app.Item{Source: "online", Type: "mv", Bvid: "BV1", Title: "第一首歌"}},
		{ID: "b", Title: "第二首歌", Owner: "UP 乙", Duration: 245 * 1e9,
			Item: app.Item{Source: "online", Type: "mv", Bvid: "BV2", Title: "第二首歌"}},
	}
}

// dragOverRow 用指针把 from 那一块的**整段**从左沿拖到右沿。
//
// ⚠️ **不按行高算坐标、不猜字符宽度**：两端都取 `Find` 量出来的盒子，落在
//
//	起点：from 的左沿 +1（行内第一个字之前）
//	终点：to 的右沿 -1（行内最后一个字之后）
//
// ——这样「整段都选中」不依赖字体度量（和 `internal/ui/selectable_copy_test.go`
// 的 `dragOver` 同一个规矩）。
func dragOverRow(t *testing.T, tt *ui.Tester, from, to string) {
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

// copyRow 清空剪贴板、走一次应用的「复制」命令，返回剪贴板里的字。
func copyRow(tt *ui.Tester) string {
	tt.SetClipboard("")
	tt.Command("copy")
	return tt.Clipboard()
}

// TestDraggingARowCopiesTheWholeRow 断**拖选一行复制的是整行**。
//
// ⚠️ **判据是剪贴板里的那段原文**：它该是这一行显示的那几样——歌名、UP 名、
// 时长——用换行连着。
//
// 🔴 **不是「歌名那一段」**：那一段是**容器级 `Selectable`** 的意义——「它的
// Text 后代共用一个选择，复制时用换行连着」（`mygo/ui/api_gen.go` 的
// `Selectable`）。而逐行各自 `Selectable` 的话，拖到下一行时上一行会掉出去。
//
// ⚠️ **拖的是 UP 名那一块**：它是**可选中那一列里**的一块（歌名那一块在列表
// 这一档量不到——`ListState.Label` 装的是歌名，于是 `tt.Find` 命中的是**整行**
// 的盒子，按下去落在序号那一列上，不在可选中的范围里）。而复制出去的是
// **整行**（歌名 + UP 名 + 时长），所以拖哪一块都一样——这正是「容器级
// `Selectable`」要的那件事。
func TestDraggingARowCopiesTheWholeRow(t *testing.T) {
	_, tt := newList(t, Actions{}, rowTextSongs())

	dragOverRow(t, tt, "UP 乙", "UP 乙")

	want := "第二首歌\nUP 乙\n4:05"
	if got := copyRow(tt); got != want {
		t.Errorf("拖选第二行之后剪贴板里是 %q，want %q", got, want)
	}
}

// TestDraggingARowCopiesTheWholeRowInTheCompactView 断**紧凑那一档一样**。
//
// ⚠️ **紧凑那一档是另一段画法**（`card.go` 的 `viewCompactCard`），不是上面
// 那一段少画几样——所以「上面那条绿了就当两档都对」是不成立的。
func TestDraggingARowCopiesTheWholeRowInTheCompactView(t *testing.T) {
	l := New(newCache(t), Actions{})
	l.SetView(ViewCompact)
	l.SetSongs(rowTextSongs())
	tt := ui.NewTester(l.View, testerW, testerH)
	tt.Frame()

	dragOverRow(t, tt, "UP 乙", "UP 乙")

	want := "第二首歌\nUP 乙\n4:05"
	if got := copyRow(tt); got != want {
		t.Errorf("紧凑档拖选第二行之后剪贴板里是 %q，want %q", got, want)
	}
}

// TestDraggingARowInTheGridCopiesTheWholeCell 断**网格那一格也一样**。
//
// ⚠️ **网格那一档也是另一段画法**（`grid.go` 的 `viewCell`）：封面、封面底上
// 那条渐变（播放量 / 时长）、歌名、UP 名 / 时长——画的东西不一样，而拖选复制
// 的那一段也不一样。
//
// ⇒ 而网格那一格的**时长在 UP 名那一行右边**（不是单独一列），所以整格复制
// 出来是「歌名 + UP 名 + 时长」这三样（封面上的播放量 / 时长不是文字，不在
// 选择里）。
//
// ⚠️ **网格这一档 `Find` 给的是歌名那一小块**（格子没有 `Label`）——所以这里
// 拖的就是**歌名**本身。
func TestDraggingARowInTheGridCopiesTheWholeCell(t *testing.T) {
	_, tt := newGrid(t, Actions{}, rowTextSongs(), 900, gridH)

	dragOverRow(t, tt, "第二首歌", "第二首歌")

	want := "第二首歌\nUP 乙\n4:05"
	if got := copyRow(tt); got != want {
		t.Errorf("网格里拖选第二格之后剪贴板里是 %q，want %q", got, want)
	}
}

// TestClickingARowTitleStillSelectsThatRow 断**点标题仍然选中那一行**。
//
// 🔴 **这一条是「能不能给行标题加 Selectable」的答案**：加了之后框架的
// `pressTextSelection` 把这一下按下接走，行的 `listRowValueInput` 收不到
// `Clicked()`，于是「单击选中」就没了——所以这一列自己把它接回来
// （`row_select.go` 的 `rowTextInput`）。
//
// ⚠️ **判据是 `Selected()`**：只断「没崩」的话，一个「点一下什么都不发生」的
// 实现照样绿。
func TestClickingARowTitleStillSelectsThatRow(t *testing.T) {
	l, tt := newList(t, Actions{}, rowTextSongs())

	r, ok := tt.Find("UP 乙")
	if !ok {
		t.Fatalf("画面上找不到第二行的 UP 名，文字有 %q", tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()

	if got, ok := l.Selected(); !ok || got.ID != "b" {
		t.Errorf("点第二行之后 Selected() = (%q, %v)，want (b, true)", got.ID, ok)
	}
}

// TestDoubleClickingARowTitleStillPlaysThatSong 断**双击标题仍然播那一首**。
//
// 🔴 **和上面那一条是同一个坑的两头**：按下被 `pressTextSelection` 接走之后，
// 双击那一首也不会开始播（框架的 `pointerUp` 只给**被按下那个元素**的
// `clicks++`，而 `pressTextSelection` 把 `rt.pressed` 设成了那几段字）。
//
// ⚠️ **判据是「真的放了那���首」**，不是「某个回调被调了」。
func TestDoubleClickingARowTitleStillPlaysThatSong(t *testing.T) {
	var played []app.Item
	_, tt := newList(t, Actions{PlayNow: func(item app.Item) { played = append(played, item) }}, rowTextSongs())

	r, ok := tt.Find("UP 乙")
	if !ok {
		t.Fatalf("画面上找不到第二行的 UP 名，文字有 %q", tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if len(played) != 0 {
		t.Errorf("单击就播放了 %d 次——单击只该选中", len(played))
	}

	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if len(played) != 1 || played[0].Bvid != "BV2" {
		t.Errorf("双击之后「立即播放」收到 %v，want 只有 BV2 那一首", played)
	}
}

// TestCmdCWithNoTextSelectedStillCopiesTheRowTitle 断**⌘C 的兜底还在**。
//
// 🔴 **这一条是票 #685 的老判据**：选中一行之后按 ⌘C，复制的是**那一行的歌名**
// ——而它现在多了一个竞争者（那一列可选中了，框架自己也会复制那一段）。
//
// ⚠️ **判据是「那一段文字一个都没选」**：所以**先拖一下再取消**是做不到的
// （框架的选中取消不了），这里走的是「**点一下那一行**」——点的是**可选中
// 那一列外面**的地方（序号那一列），那一列没被选中，焦点也还在列表上，于是
// 列表这一层的兜底出手（理由见 `copy_title.go`）。
//
// ⚠️ **两条路都走一遍**（`Key` 与 `Command`）：macOS 上「编辑 › 拷贝」走的是菜单
// 命令（`mygo/ui/input.go` 的 `editEvent`），键盘走的是按键；只认一条的话另一条
// 在真机上就是「按了没反应」。
func TestCmdCWithNoTextSelectedStillCopiesTheRowTitle(t *testing.T) {
	for _, route := range []struct {
		name  string
		press func(*ui.Tester)
	}{
		{"按键", func(tt *ui.Tester) { tt.Key(ui.Cmd, ui.KeyC) }},
		{"编辑菜单", func(tt *ui.Tester) { tt.Command("copy") }},
	} {
		t.Run(route.name, func(t *testing.T) {
			var copied []string
			acts := Actions{Copy: func(text string) { copied = append(copied, text) }}
			l, tt := newList(t, acts, rowTextSongs())

			r, ok := tt.Find("2")
			if !ok {
				t.Fatalf("画面上找不到序号，文字有 %q", tt.Texts())
			}
			tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
			tt.Frame()
			if got, ok := l.Selected(); !ok || got.ID != "b" {
				t.Fatalf("点第二行之后 Selected() = (%q, %v)，want (b, true)", got.ID, ok)
			}

			route.press(tt)
			tt.Frame()

			if len(copied) != 1 || copied[0] != "第二首歌" {
				t.Errorf("%s之后复制出来的是 %q，want 一条「第二首歌」", route.name, copied)
			}
		})
	}
}
