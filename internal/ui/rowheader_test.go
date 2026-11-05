package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/hover"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// 这一组验「稍后再看」「历史记录」两页的表头和 hover 反馈（票面「一」「三」）。
//
// 组件和断言都在 `widgets/list` 那一族里验过了（`header_test.go` / `hover_test.go`）。
// 这里再验一遍是因为**这两页的行不是那个包画的**：它们的列多两列（播放量 / 加入时间，
// 播放进度 / 观看时间），表头跟着多两列——而「表头跟着多」这件事每一页都得自己写一
// 次，是最容易漏的地方。

// libTitles 是这一族夹具里那一页的歌名。
var libTitles = []string{"甲曲", "乙曲", "丙曲", "丁曲"}

// laterHoverShell 造一个停在稍后再看、外壳接上假 B 站的那一页。
func laterHoverShell(t *testing.T) (*ui.Tester, *Shell) {
	t.Helper()
	f := newLibFake(t).withLater(1, 4, libTitles...)
	tt, s := libraryShell(t, f, laterPath)
	waitFrames(t, tt, "稍后再看第一页", func() bool { return rowsShown(&s.laterPage) == 4 })
	return tt, s
}

// historyHoverShell 造一个停在历史记录的那一页。
func historyHoverShell(t *testing.T) (*ui.Tester, *Shell) {
	t.Helper()
	f := newLibFake(t).withHistory(1, 30, 185, libTitles...)
	tt, s := libraryShell(t, f, historyPath)
	waitFrames(t, tt, "历史记录第一页", func() bool { return rowsShown(&s.historyPage) == 4 })
	return tt, s
}

// TestTheLibraryRowsAreTheSameHeightNoMatterHowLongTheTitle 钉住「稍后再看 /
// 历史记录」那两页的行高是**定死的一个值**。
//
// 理由和 `widgets/list` 的 `TestEveryRowIsTheSameHeightNoMatterHowLongTheTitle` 是
// 同一个：上游那一行的歌名是 `truncate`（**一行**），折成两行的话整张列表的行高就
// 跟着歌名长短跳，一屏能看几首歌会变来变去。
//
// ⚠️ **这两页的行不是 `widgets/list` 画的**（它们多两列），所以那边钉过的那一条在
// 这边不成立——得自己再钉一遍。
func TestTheLibraryRowsAreTheSameHeightNoMatterHowLongTheTitle(t *testing.T) {
	tt, s := laterHoverShell(t)

	row, ok := tt.Find(libTitles[0])
	if !ok {
		t.Fatalf("画面上找不到第一行，文字有 %q", tt.Texts())
	}
	_ = s
	want := float32(60) // 原型 `.trow` 高 60（DESIGN.md §0′.T7）
	// 布局是 float32，60 会算成 60.000015；0.01 远小于任何一次折行（≥ 一行字高）。
	if row.H < want-0.01 || row.H > want+0.01 {
		t.Errorf("稍后再看那一行高 %v，want %v（行高是定死的一个值）", row.H, want)
	}

	// 相邻两行隔的也是行高：歌名折行不能把这一行顶高。
	next, ok := tt.Find(libTitles[1])
	if !ok {
		t.Fatalf("画面上找不到第二行，文字有 %q", tt.Texts())
	}
	if got := next.Y - row.Y; got < want-0.5 || got > want+0.5 {
		t.Errorf("第一行到第二行隔了 %v，want %v（行高是定死的一个值）", got, want)
	}
}

// hoverShellRow 把指针移到 title 那一行的中间，并把淡入推满。
//
// ⚠️ **推钟 + 补一帧**两件都要：MyGo 的 Tester 的钟站着不动（`mygo/ui/headless.go`），
// 不推的话那 200ms 淡入一步也不走；而 `Advance` 只画它那一刻之前到点的那些帧，推到的
// 那一刻上画面还是旧的那一帧。
func hoverShellRow(t *testing.T, tt *ui.Tester, title string) {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", title, tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	tt.Frame()
	tt.Advance(hover.Fade)
	tt.Frame()
}

// uiShows 报这一帧上有没有一个**文字正好是** s 的控件。
//
// ⚠️ 不能用 `tt.HasText`：那是子串匹配（`mygo/ui/headless.go` 的 `strings.Contains`），
// 「#」在别处、「时长」在歌名里都可能命中——判据一虚，断言就永远为真。
func uiShows(tt *ui.Tester, s string) bool {
	for _, got := range tt.Texts() {
		if got == s {
			return true
		}
	}
	return false
}

// TestTheSearchUserTabHeaderHasNoClock 钉住「用户」那一档的表头**没有时长、
// 播放量、投稿时间**。
//
// 那一档的行是「序号 / 头像 / 昵称 / 个性签名」（`searchUserMusic` 的注释），
// 时长 / 播放量 / 投稿时间三样它一样都没有——后两样的列定义是 `searchColumns`
// 给的（用户那一档是零值），时长那一列则在规格末尾被切掉。
//
// ⚠️ 断的是**规格里有没有那一列**而不是画面上有没有那个字：`CardHeaderSpec` 摆的是
// 列定义，画面上的字是它画出来的，两者对不上这件事只有前者能说清。
func TestTheSearchUserTabHeaderHasNoClock(t *testing.T) {
	video := searchHeaderSpec(0, list.ViewList, list.Columns{PlayCount: true, PubTime: true}, false, "k")
	user := searchHeaderSpec(searchTabUser, list.ViewList, list.Columns{}, false, "k")

	if last := video.Cols[len(video.Cols)-1]; last.Title != "时长" {
		t.Errorf("歌曲那一档的表头最后一列是「%s」，want 「时长」", last.Title)
	}
	for _, col := range user.Cols {
		switch col.Title {
		case "时长", "播放量", "投稿时间":
			t.Errorf("「用户」那一档的表头上还摆着「%s」，而那一档的行里没有那一列",
				col.Title)
		}
	}
	// 剩下的两列一个都不能少：序号、标题。
	//
	// ⚠️ **封面不在表头里**（DESIGN.md：表头的「标题」从封面左缘起）——所以是两列
	// 而不是三列。多一列封面空位的话「标题」要往右让 64，而那在画面上只是「表头比
	// 下面那几列靠右一点」。
	if len(user.Cols) != 2 {
		t.Errorf("「用户」那一档的表头有 %d 列，want 2（# / 标题）", len(user.Cols))
	}
}
