package ui

import (
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/hover"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
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
	want := float32(metricsOf(s.laterPage.rows.Layout()).height)
	if row.H != want {
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

// TestTheLaterHeaderSaysItsOwnColumns 钉住稍后再看那一行的表头列。
//
// 它比 `widgets/list` 那一族多两列：播放量 / 加入时间（上游是「投稿时间」，而这一页
// 的筛选器筛的是 `add_at`——跟着筛的那个时间画，见 `later.go` 文件头）。
func TestTheLaterHeaderSaysItsOwnColumns(t *testing.T) {
	tt, _ := laterHoverShell(t)

	for _, want := range []string{"#", "标题", "播放量", "加入时间", "时长"} {
		if !uiShows(tt, want) {
			t.Errorf("稍后再看的表头上没有「%s」，文字有 %q", want, tt.Texts())
		}
	}
}

// TestTheLaterHeaderLinesUpWithItsRows 钉住表头和那一页的行对得齐。
//
// ⚠️ 量的是**编号**那一列：它是唯一会被前面几块顶偏的一列，而错的那一列在画面上只是
// 「编号歪了一点」（这一页原来那份表头就是把「标题」摆在序号后面、忘了中间那个封面，
// 右边三列整整差 56）。
func TestTheLaterHeaderLinesUpWithItsRows(t *testing.T) {
	tt, _ := laterHoverShell(t)

	head, ok := tt.Find("#")
	if !ok {
		t.Fatalf("表头上没有「#」，文字有 %q", tt.Texts())
	}
	row, ok := tt.Find("1")
	if !ok {
		t.Fatalf("画面上找不到第一行的序号，文字有 %q", tt.Texts())
	}
	if head.X != row.X {
		t.Errorf("表头的「#」在 %.1f，行里那个序号在 %.1f——差 %.1f（封面那一列的空位"+
			"不在它和「标题」中间？）", head.X, row.X, head.X-row.X)
	}

	// 右端那一列也要对：那才是「数字列竖着扫得下来」的那一条。
	headClock, ok := tt.Find("时长")
	if !ok {
		t.Fatalf("表头上没有「时长」，文字有 %q", tt.Texts())
	}
	rowClock, ok := tt.Find(clockText(185 * time.Second))
	if !ok {
		t.Fatalf("画面上找不到那一行的时长 %q，文字有 %q",
			clockText(185*time.Second), tt.Texts())
	}
	if headClock.X != rowClock.X {
		t.Errorf("表头的「时长」在 %.1f，行里那一列在 %.1f——差 %.1f",
			headClock.X, rowClock.X, headClock.X-rowClock.X)
	}
}

// TestTheHistoryHeaderLinesUpWithItsRows 同一件事，历史记录那一页。
//
// 它右边那两列是**播放进度 / 观看时间**，不是播放量 / 加入时间——历史记录的接口不
// 给播放量（`libraryRow.View` 的注释）。表头跟着行走，不是跟着上一张票走。
func TestTheHistoryHeaderLinesUpWithItsRows(t *testing.T) {
	tt, _ := historyHoverShell(t)

	for _, want := range []string{"#", "标题", "播放进度", "观看时间"} {
		if !uiShows(tt, want) {
			t.Errorf("历史记录的表头上没有「%s」，文字有 %q", want, tt.Texts())
		}
	}
	head, ok := tt.Find("#")
	if !ok {
		t.Fatalf("表头上没有「#」，文字有 %q", tt.Texts())
	}
	row, ok := tt.Find("1")
	if !ok {
		t.Fatalf("画面上找不到第一行的序号，文字有 %q", tt.Texts())
	}
	if head.X != row.X {
		t.Errorf("表头的「#」在 %.1f，行里那个序号在 %.1f——差 %.1f",
			head.X, row.X, head.X-row.X)
	}
}

// TestHoveringALibraryRowPutsAPlayTriangleOnItsCover 钉住两页的行也有 hover 反馈。
//
// 这一页的行不是 `widgets/list` 画的（列多两列），所以那边验过的在这儿要自己再钉一遍
// ——「组件共用」不等于「每一处调用都接上了」。
func TestHoveringALibraryRowPutsAPlayTriangleOnItsCover(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shell func(*testing.T) (*ui.Tester, *Shell)
	}{
		{"稍后再看", laterHoverShell},
		{"历史记录", historyHoverShell},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tt, _ := tc.shell(t)

			if _, ok := tt.Find(hover.FromLabel(libTitles[1])); ok {
				t.Fatalf("指针还没移上去封面就有播放三角了：%q", tt.Texts())
			}
			hoverShellRow(t, tt, libTitles[1])

			got, ok := tt.Find(hover.FromLabel(libTitles[1]))
			if !ok {
				t.Fatalf("指针移上去之后封面上没有播放三角：%q", tt.Texts())
			}
			if got.W < 19 {
				t.Errorf("那个播放三角只有 %.1f 宽，want 20", got.W)
			}
			if _, ok := tt.Find(hover.FromLabel(libTitles[0])); ok {
				t.Errorf("指针在第二行，第一行的封面上却有播放三角：%q", tt.Texts())
			}
		})
	}
}

// TestTheLibraryRowIndexBecomesAWaveformWhilePlaying 钉住「正在播放的那一行，序号位置
// 换成跳动的波形，行尾那句『正在播放』没有了」。
//
// ⚠️ **认的是标签 `歌名 正在播放`**（`hover.PlayingLabel`），不是画出来的那几个字——
// 波形是一个没有文字的盒子，它能被 `Find` 找到靠的就是这个标签。
func TestTheLibraryRowIndexBecomesAWaveformWhilePlaying(t *testing.T) {
	f := newLibFake(t).withLater(1, 4, libTitles...)
	tt, s := libraryShell(t, f, laterPath)
	waitFrames(t, tt, "稍后再看第一页", func() bool { return rowsShown(&s.laterPage) == 4 })

	// 先钉住没在放的时候：序号还在，波形不在。
	if _, ok := tt.Find(hover.PlayingLabel(libTitles[1])); ok {
		t.Fatalf("还没有歌在放，那一行上就有波形了：%q", tt.Texts())
	}

	// ⚠️ **摆队列而不是 `playFrom`**：在线播放那一路在测试里没接上（`Play` 走真播放
	// 器），按下去只会说一句「放不了这一首」。而「哪一行在放」认的是**队列的当前项**
	// （`app.Item.Identity`），摆好队列它就成立了——和 `library_menu_test.go` 那一族
	// 同一个做法。
	rows := s.laterPage.rows
	items := rows.Rows()
	if _, ok := s.app.Queue().Reset(items[0].Item, items[1].Item); !ok {
		t.Fatal("队列摆不进去")
	}
	tt.Frame()

	if !rows.isPlaying(items[0]) {
		t.Fatalf("第一首没在放（接线认不出来），文字有 %q", tt.Texts())
	}
	wave, ok := tt.Find(hover.PlayingLabel(libTitles[0]))
	if !ok {
		t.Fatalf("正在放的那一行没有那个跳动波形：%q", tt.Texts())
	}
	// ⚠️ **拿第二行的序号当基准**：第一行的序号已经被波形换掉了，而「波形在序号那一列
	// 位置上」这句话量的正是同一个横坐标。序号那一列是 40 宽（`colIndex`），波形比它
	// 宽就说明它把封面顶走了。
	second, ok := tt.Find("2")
	if !ok {
		t.Fatalf("画面上找不到第二行的序号，文字有 %q", tt.Texts())
	}
	if diff := wave.X - second.X; diff > 2 || diff < -2 {
		t.Errorf("波形摆在 x=%.1f，第二行的序号在 %.1f——差 %.1f，它没有占在序号的位置上",
			wave.X, second.X, diff)
	}
	if wave.W > colIndex {
		t.Errorf("波形占了 %.1f 宽，比序号那一列（%d）还宽——它把封面顶走了", wave.W, colIndex)
	}
	// 行尾那句没有了：整个画面上「正在播放」只该剩下这一个标签。
	n := 0
	for _, s := range tt.Texts() {
		if s == playingMark || (len(s) > len(playingMark) &&
			s[len(s)-len(playingMark)-1:] == " "+playingMark) {
			n++
		}
	}
	if n != 1 {
		t.Errorf("画面上有 %d 处「正在播放」，want 1 处（波形那一个）：%q", n, tt.Texts())
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

// TestTheSearchUserTabHeaderHasNoClock 钉住「用户」那一档的表头**没有「时长」**。
//
// 那一档的行是「序号 / 头像 / 昵称 / 个性签名」（`searchUserMusic` 的注释），
// `list.Music.Duration` 是零值，于是行里那一列**没有字**——**位置是留着的**
// （`widgets/list` 的 `viewCard` 无论有没有时长都留着那么宽，因为行尾那枚「⋯」的
// 位置跟着这一列走）——但表头上还写着「时长」的话，用户读到的是一列空的表头。
//
// ⚠️ 断的是**规格里有没有那一列**而不是画面上有没有那个字：`CardHeaderSpec` 摆的是
// 列定义，画面上的字是它画出来的，两者对不上这件事只有前者能说清。
func TestTheSearchUserTabHeaderHasNoClock(t *testing.T) {
	video := searchHeaderSpec(0, list.ViewList, false, "k")
	user := searchHeaderSpec(searchTabUser, list.ViewList, false, "k")

	if last := video.Cols[len(video.Cols)-1]; last.Title != "时长" {
		t.Errorf("歌曲那一档的表头最后一列是「%s」，want 「时长」", last.Title)
	}
	for _, col := range user.Cols {
		if col.Title == "时长" {
			t.Errorf("「用户」那一档的表头上还摆着「时长」，而那一档的行里没有那一列")
		}
	}
	// 别的几列一个都不能少（封面那一列的空位也算）。
	if len(user.Cols) != len(video.Cols)-1 {
		t.Errorf("「用户」那一档的表头有 %d 列，want %d（只少「时长」那一列）",
			len(user.Cols), len(video.Cols)-1)
	}
}
