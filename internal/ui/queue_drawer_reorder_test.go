package ui

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 队列抽屉的**拖拽重排**（票 #322 的 B5）。
//
// 这一组盯三件事：
//
//   - 拖得动，顺序真的变了；
//   - **松手才提交**：拖动的过程中队列一个字节都不动；
//   - **正在放的那首不错位**：重排之后指针还指着同一首。
//
// 判据看的是**队列本身**（`app.Queue().Items()`），不是画面：画面上那一列是
// 去重之后的（见 queueRowsFor），而队列里可以有同一首的多份。

// drawerSongs 造几首**歌名认得出来**的队列项。
//
// ⚠️ **歌名必须认得出来**：`Tester.Find` 拿到的是**第一个**匹配的，而播放条上
// 正在放的那首也在画面上——名字一样的话 Find 量到的是播放条上那一份（它在屏幕
// 下面，不在抽屉里），按下去按的就不是那一行。
func drawerSongs(t *testing.T, titles ...string) []app.Item {
	t.Helper()
	items := copiesOf(t, len(titles))
	for i := range items {
		items[i].Title = titles[i]
	}
	return items
}

// queueOrderTitles 是队列里此刻的歌名，按顺序。
//
// ⚠️ 名字和 `library_menu_test.go` 的 `queueTitles` 差一个词：那边收的是
// `[]app.Item`，这边收的是 `*app.App`（它自己从队列里取）。
func queueOrderTitles(a *app.App) []string {
	items := a.Queue().Items()
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Title
	}
	return out
}

// sameTitles 断两列歌名是不是一模一样。
func sameTitles(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// queueRow 是抽屉里第 i 行（0 起）的框。
//
// ⚠️ **只找第一行，其余的行按行高推**：播放条上正在放的那首也在画面上，
// `Tester.Find` 拿到的是第一个匹配的——找「正在放的那首」会量到播放条上那一份
// （它在屏幕下面，不在抽屉里）。行高是定死的 64（`queueRowHeight`，理由见
// queue_drawer.go），所以推出来的框和真的一模一样。
func queueRowBox(first mygoui.Rect, i int) mygoui.Rect {
	r := first
	r.Y = first.Y + float32(i)*queueRowHeight
	return r
}

// dragQueueRow 把抽屉里 from 那一行拖到 targetY 那个高度松手。
//
// ⚠️ **中间不画帧**：框架的拖动是「按下之后指针移动超过 4 个 DIP 才算拖动」，
// 一步跳到位的话中间那几帧的坐标对不上，插入线会落在别的地方。写法照框架自己
// 那个测试（`mygo/ui/dragdrop_test.go` 的 drag）。
//
// ⚠️ **松手的高度决定「挪到哪一行之前」**：框架判的是松手的位置和每一行**中间**
// 谁在上（`mygo/ui/list.go` 的 dropAt）。落在目标那一行的**下半**就变成「挪到
// 下一行之前」，落在**上半**才是「挪到这一行之前」。
func dragQueueRow(t *testing.T, tt *mygoui.Tester, from mygoui.Rect, targetY float32) {
	t.Helper()
	x := from.X + from.W/2
	y := from.Y + from.H/2
	tt.Press(x, y)
	for k := 1; k <= 4; k++ {
		f := float32(k) / 4
		tt.Move(x, y+(targetY-y)*f)
	}
	tt.Release(x, targetY)
	tt.Frame()
}

// TestTheQueueRowsCanBeDraggedToReorder 盯「拖得动」那一条。
//
// 四首（甲乙丙丁），把第一行拖到第二行**上面**：队列变成 乙 甲 丙 丁。
func TestTheQueueRowsCanBeDraggedToReorder(t *testing.T) {
	h := newHarness(t)
	enqueue(t, h, drawerSongs(t, "甲", "乙", "丙", "丁")...)
	openDrawer(t, h)

	first, ok := h.tt.Find("甲")
	if !ok {
		t.Fatalf("抽屉里找不到甲，文字有 %q", h.tt.Texts())
	}
	second := queueRowBox(first, 1)

	// 落在第二行的**下半**：挪到第三行之前（第一行往下挪一格）。
	dragQueueRow(t, h.tt, first, second.Y+second.H-4)

	got := queueOrderTitles(h.app)
	want := []string{"乙", "甲", "丙", "丁"}
	if !sameTitles(got, want) {
		t.Errorf("拖完队列是 %q，want %q", got, want)
	}
}

// TestTheReorderIsCommittedOnlyOnRelease 盯「**松手才提交**」那一条
// （foobar2000 的规矩）。
//
// 拖动的过程中队列**一个字节都不动**——拖一下就改一次队列的话，用户每帧都在
// 被改，而「正在放的那首」会跟着乱跳（那个跳动是静默的：播放条上写着甲，抽屉
// 里高亮的是乙，响着的还是甲）。
func TestTheReorderIsCommittedOnlyOnRelease(t *testing.T) {
	h := newHarness(t)
	enqueue(t, h, drawerSongs(t, "甲", "乙", "丙", "丁")...)
	openDrawer(t, h)

	before := queueOrderTitles(h.app)
	first, ok := h.tt.Find("甲")
	if !ok {
		t.Fatalf("抽屉里找不到甲，文字有 %q", h.tt.Texts())
	}
	second := queueRowBox(first, 1)

	// 按下 + 拖过去，**还没松手**。
	x := first.X + first.W/2
	y := first.Y + first.H/2
	drop := second.Y + second.H - 4
	h.tt.Press(x, y)
	for k := 1; k <= 4; k++ {
		f := float32(k) / 4
		h.tt.Move(x, y+(drop-y)*f)
		h.tt.Frame()
	}

	if got := queueOrderTitles(h.app); !sameTitles(got, before) {
		t.Errorf("还没松手队列就变成了 %q，want %q（松手才提交）", got, before)
	}

	h.tt.Release(x, drop)
	h.tt.Frame()
	if got := queueOrderTitles(h.app); sameTitles(got, before) {
		t.Errorf("松手之后队列还是 %q——没提交", got)
	}
}

// TestTheReorderKeepsThePlayingSongInPlace 盯「**正在放的那首不错位**」
// （Nuclear 的 currentIndex 三分支修正）。
//
// 现在在放的是**乙**（第二行），把它拖到最前面：队列变成 乙 甲 丙 丁，而指针
// 还得指着乙（它的新下标是 0）。
//
// 错了的症状是**静默**的：播放条上写着乙，抽屉里高亮的是甲，而响着的还是乙。
func TestTheReorderKeepsThePlayingSongInPlace(t *testing.T) {
	h := newHarness(t)
	enqueue(t, h, drawerSongs(t, "甲", "乙", "丙", "丁")...)
	openDrawer(t, h)

	// 换成放第二首（乙）。
	if err := h.app.PlayAt(1); err != nil {
		t.Fatalf("放第二首失败：%v", err)
	}
	h.tt.Frame()
	playing, ok := h.app.Queue().Current()
	if !ok || playing.Title != "乙" {
		t.Fatalf("现在在放的是 %q，want 乙", playing.Title)
	}

	first, ok := h.tt.Find("甲")
	if !ok {
		t.Fatalf("抽屉里找不到甲，文字有 %q", h.tt.Texts())
	}
	second := queueRowBox(first, 1)

	// 把正在放的那首（乙）拖到最前面。
	dragQueueRow(t, h.tt, second, first.Y+4)

	got := queueOrderTitles(h.app)
	want := []string{"乙", "甲", "丙", "丁"}
	if !sameTitles(got, want) {
		t.Errorf("拖完队列是 %q，want %q", got, want)
	}
	now, ok := h.app.Queue().Current()
	if !ok || now.Title != "乙" {
		t.Errorf("重排之后正在放的是 %q，want 乙（正在放的那首不能错位）", now.Title)
	}
	// 抽屉里高亮的那一行也是同一首：界面上的高亮认的是判重标识，不是下标。
	if got := h.s.playingIdentity(h.app.Snapshot()); got != playing.Identity() {
		t.Errorf("高亮的判重标识是 %q，want %q", got, playing.Identity())
	}
}
