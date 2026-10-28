package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/hover"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// 票 #279：**历史记录页第一行误显示「正在播放」**。
//
// # 用户说的原话
//
// 「历史界面的，我点进去发现第一个左侧有那个播放的在动的，实际上我并没有在
// 播放这首歌，我只是刚刚点到历史记录里」。
//
// # 根因
//
// 「正在播放」那一档画的是**跳动的波形**，还会被读屏软件念成「正在播放」——所以
// 它答的必须是「**此刻有声音的是哪一首**」。而它以前认的是「**队列的指针停在
// 哪一项上**」。那两样在下面这几处不是同一件事：
//
//   - 暂停 / 停止（用户按的）：指针不动，声音没了；
//   - 启动时「恢复上次听到哪儿」：恢复出来是**停着的**（`openLocal` 的
//     `openRestore` 那一档明写「不出声」）；
//   - 取流失败：那一首压根没放起来；
//   - 换歌的取流窗口：队列已经换到新歌了，而播放器还在放上一首。
//
// 于是那一行在跳，而应用根本没在放它。
//
// # 这一组量的是「没在放就不标」
//
// ⚠️ **「真在放时仍然要标」**由两层量：判据本身在 `internal/app`
// （`queue_isplaying_test.go`），而「真在放时界面上真的有那个标记」在
// `internal/ui/widgets/list` 的端到端那一条（真播放器 + 真接线）。
//
// ⚠️ **数据是假的 B 站造的**（`libFake`），一个真 B 站接口都不打。

// markedPlaying 报 title 那一行有没有被标成「正在播放」。
//
// ⚠️ **认的是无障碍标签**（`歌名 正在播放`），不是画出来的那几个字：波形是一个
// 没有文字的盒子，它能被 `Find` 找到靠的就是这个标签。
func markedPlaying(tt *ui.Tester, title string) bool {
	_, ok := tt.Find(hover.PlayingLabel(title))
	return ok
}

// ─────────────────────────────────────────────────────────────────────────────
// 复现
// ─────────────────────────────────────────────────────────────────────────────

// TestTheHistoryPageDoesNotMarkASongThatIsNotPlaying 是这一票的回归。
//
// # 复现步骤（数据是假 B 站造的）
//
//  1. 假 B 站返回一页历史记录（4 条）。
//  2. 队列摆成「历史记录的第一首是**当前项**」——`Queue().Reset` 只摆不播。
//  3. 进历史记录页。
//  4. **修之前**：第一行的序号那一格是**跳动的波形**，标签是「甲曲 正在播放」，
//     而应用根本没在放它（`Reset` 没有起播，队列当前项只是指针）。
//  5. **修之后**：那一行没有波形，序号还是数字「1」。
//
// ⚠️ **为什么「队列当前项 = 它」就算「没在放」**：那只是指针。声音在不在是另一件事
// （暂停、停止、恢复出来没起播、取流失败、换歌的取流窗口——见文件头）。
func TestTheHistoryPageDoesNotMarkASongThatIsNotPlaying(t *testing.T) {
	f := newLibFake(t).withHistory(1, 30, 185, libTitles...)
	tt, s := libraryShell(t, f, historyPath)
	waitFrames(t, tt, "历史记录第一页", func() bool { return rowsShown(&s.historyPage) == 4 })

	items := s.historyPage.rows.Rows()
	if _, ok := s.app.Queue().Reset(items[0].Item); !ok {
		t.Fatal("队列摆不进去")
	}
	tt.Frame()

	if markedPlaying(tt, libTitles[0]) {
		t.Errorf("历史记录第一行被标成「正在播放」，而此刻没有声音在出：%q", tt.Texts())
	}
	// 序号还在（没被波形换掉）：那是「这一行没有那个标记」的样子。
	if _, ok := tt.Find("1"); !ok {
		t.Errorf("第一行的序号不见了（它被换成波形了？）：%q", tt.Texts())
	}
}

// TestTheLaterPageDoesNotMarkASongThatIsNotPlaying 同一条，稍后再看那一页。
//
// ⚠️ 两页的行是同一副骨架（`library_rows.go` 的 `pagedRows`），所以这一条和上面
// 那一条断的是同一件事的两次——**两页都断，才不会有人只修一页**。
func TestTheLaterPageDoesNotMarkASongThatIsNotPlaying(t *testing.T) {
	f := newLibFake(t).withLater(1, 4, libTitles...)
	tt, s := libraryShell(t, f, laterPath)
	waitFrames(t, tt, "稍后再看第一页", func() bool { return rowsShown(&s.laterPage) == 4 })

	items := s.laterPage.rows.Rows()
	if _, ok := s.app.Queue().Reset(items[0].Item); !ok {
		t.Fatal("队列摆不进去")
	}
	tt.Frame()

	if markedPlaying(tt, libTitles[0]) {
		t.Errorf("稍后再看第一行被标成「正在播放」，而此刻没有声音在出：%q", tt.Texts())
	}
}

// TestEveryPageThatShowsThePlayingMarkUsesTheSameCriterion 断「同族页面」那一半。
//
// 「正在播放」那个标记不只这两页有：首页的新歌速递、搜索结果、收藏夹、合集、
// UP 空间的投稿——凡是用 `widgets/list` 那个列表的页面都有。⚠️ **它们共用同一份
// 接线**（`Shell.listActions()` → `list.AppActions` → `app.App.IsPlaying`），
// 所以这一条断言「那份接线在没声音的时候对**哪一首**都说不是」，就覆盖了所有
// 页面——不用每一页各写一条。
//
// ⚠️ **本地音乐和下载记录那两页没有这个标记**：本地音乐页自己画表格（它只有
// 「正在放的文件不给删除按钮」那一处判据，那一处认的是「当前项」，不是
// 「正在放」）；下载记录页压根没有这个标记。所以票面说的「四页」里，那两页
// 不适用——**说清楚比装作验过好**。
func TestEveryPageThatShowsThePlayingMarkUsesTheSameCriterion(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	// 一条队列，指针指着队列当前项，而**此刻没有声音**。
	song := app.Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1", Title: "队列指着它"}
	if _, ok := a.Queue().Reset(song); !ok {
		t.Fatal("队列摆不进去")
	}

	acts := list.AppActions(a, nil)
	for _, it := range []app.Item{
		song,
		{Source: "online", Type: "mv", Bvid: "BV2", Cid: "1", Title: "队列外的那首"},
		app.LocalItem("/music/a.flac"),
	} {
		if acts.IsPlaying(it) {
			t.Errorf("没有声音在出，而 IsPlaying(%q) 说了 true——"+
				"用这份接线的每一个页面都会把这一行标成「正在播放」", it.Title)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 「选中」和「正在放」是两件事
// ─────────────────────────────────────────────────────────────────────────────

// TestTheSelectionAndThePlayingMarkAreDifferentThings 断「画面上分得开」。
//
// 这两件事在界面上是**两个地方**：
//
//   - **正在放**：序号那一格是**跳动的波形**，歌名是**主色**（跟着声音走）；
//   - **选中**：序号那一格还是**数字**，歌名是**普通色**（跟着光标走）；键盘走到
//     它上面时另有一圈**焦点环**（票 #197 之后选中不再整行刷实心主色底）。
//
// 所以「正在放」的那一行和「选中」的那一行**各自认得出来**，而选中**不会**把
// 序号换成波形。
func TestTheSelectionAndThePlayingMarkAreDifferentThings(t *testing.T) {
	// ⚠️ **用稍后再看那一页**：历史记录那一页的假数据是**同一首歌**的 4 条记录
	// （B 站就是这样的，见 `historyBody`）——它们的判重标识一样，于是「在放的是
	// 第一首」这件事会把**四行**都标上，序号那一格也就全是波形了。稍后再看那
	// 一页的假数据每首一首不同的 bvid（见 `laterBody`），才量得出「这一行是
	// 波形、那一行是数字」。
	f := newLibFake(t).withLater(1, 4, libTitles...)
	tt, s := libraryShell(t, f, laterPath)
	waitFrames(t, tt, "稍后再看第一页", func() bool { return rowsShown(&s.laterPage) == 4 })

	items := s.laterPage.rows.Rows()
	// 「第一首在放」：换成一份说「第一首在放」的接线（这一层放不了在线的歌，
	// 理由见文件头）。
	playing := items[0].Item.Identity()
	s.laterPage.rows.acts.IsPlaying = func(it app.Item) bool { return it.Identity() == playing }
	// 「第二行被选中」：光标停在第二行。
	s.laterPage.rows.sel = 1
	tt.Frame()

	wave, ok := tt.Find(hover.PlayingLabel(libTitles[0]))
	if !ok {
		t.Fatalf("正在放的那一行没有那个波形：%q", tt.Texts())
	}
	// 选中那一行的序号还是数字（没被波形换掉）。
	num, ok := tt.Find("2")
	if !ok {
		t.Fatalf("选中的那一行没有序号数字：%q", tt.Texts())
	}
	// 两者不在同一行上：波形在第一行，数字在第二行。
	first, ok := tt.Find(libTitles[0])
	if !ok {
		t.Fatalf("画面上找不到第一行：%q", tt.Texts())
	}
	if wave.Y < first.Y || wave.Y+wave.H > first.Y+first.H {
		t.Errorf("那个波形不在第一行里 {%.1f,%.1f}，而在 {%.1f,%.1f}",
			wave.X, wave.Y, first.X, first.Y)
	}
	if num.Y < first.Y {
		t.Errorf("第二行的序号跑到了第一行上面（%.1f < %.1f）", num.Y, first.Y)
	}
}
