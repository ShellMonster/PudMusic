// 满屏播放器打开时底层那一层不画（票 #161）这一组。
//
// # 判据是**构建次数**，不是「看起来更快」
//
// 「快了多少」在这台机器上量不出来（负载一变就飘，而 AGENTS §2 早说了测试结果
// 只在机器不忙的时候算数）。而「满屏开着的时候底层那一页一帧都没被建」是
// **确定的**：探针数的是 `Shell.page` 那一次分派（`Shell.pageBuilds`）。
//
// # 为什么用下载记录这一页当底下的那一页
//
// 它的数据是**每帧**从 `rowsFn` 取的（`syncDownloads`），改一下那个切片就是改
// 了这一页此刻的内容——不用等一趟 HTTP，也就没有「结果到底回来了没有」那种
// 时序上的不确定。而「用户看不见的那一页，数据还在往前走」正是这一票要的。
package ui

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
	mygoui "github.com/egoist/mygo/ui"
)

// underlayShell 建一个停在下载记录页上的外壳，这一页的数据源换成一份**随时可以
// 改**的切片，另外放一首歌在队列里（满屏要有一首歌在放才开得出来）。
//
// 返回的那份切片是这一页此刻的内容的**唯一来源**：改它就是改这一页。
func underlayShell(t *testing.T) (*Shell, *mygoui.Tester, *[]download.Snapshot) {
	t.Helper()

	a := app.New()
	t.Cleanup(a.Close)
	s := newDownloadShell(t, a)

	rows := &[]download.Snapshot{}
	s.downloads.rowsFn = func() []download.Snapshot { return *rows }

	if err := a.Enqueue(app.LocalItem(flacPath(t))); err != nil {
		t.Fatalf("把歌放进队列：%v", err)
	}

	tt := mygoui.NewTester(s.View, dlWide, 800)
	s.navigate(downloadListPath)
	dlFrame(tt)
	if !s.app.Snapshot().HasTrack {
		t.Fatalf("歌没开起来：%q", s.app.Snapshot().Err)
	}
	return s, tt, rows
}

// TestTheUnderlyingPageIsNotBuiltWhileTheFullPlayerIsOpen 钉住「满屏开着的时候
// 底下那一页一帧都不被建」。
//
// ⚠️ **探针数的是 `Shell.page` 那一次分派**（`Shell.pageBuilds`），不是「画面上
// 有没有那个页面的字」——满屏那层是不透明的，底下画没画在画面上根本看不出来。
//
// 拿掉这一票的改动（`drawUnderlay` 里那个提前返回）之后这一条立刻红：满屏开着
// 的时候那一页照样每帧被建一次。
func TestTheUnderlyingPageIsNotBuiltWhileTheFullPlayerIsOpen(t *testing.T) {
	s, tt, _ := underlayShell(t)

	// 先让它画几帧，确认底下的页**真的在画**（不然这一条测的是「本来就没建」）。
	dlFrame(tt)
	before := s.pageBuilds
	if before == 0 {
		t.Fatal("起点就不对：这一页一帧都没建过")
	}

	s.openFullPlayer()
	tt.Frame()
	opened := s.pageBuilds

	for range 10 {
		tt.Frame()
	}
	if got := s.pageBuilds - opened; got != 0 {
		t.Errorf("满屏开着的时候底下那一页被建了 %d 次（10 帧里），want 0——"+
			"它是满屏那层不透明浮层底下的一页，用户看不见，机器不该替它干活", got)
	}
}

// TestBackgroundResultsStillLandWhileTheFullPlayerIsOpen 钉住「跳过绘制 ≠ 跳过
// 逻辑」。
//
// 这一条是这一票最容易做坏的地方：把底下那一页整个不建之后，它**每帧的副作用**
// （收后台结果、起该起的那一拉）也跟着一起停了——症状是「关掉满屏之后那一页还
// 停在打开那一刻的样子，要再等一趟才更新」。
//
// 判据是**这一页手上的数据**：满屏开着的时候数据源变了，这一页手上的那份也要
// 跟着变——而这一页一个控件都没被建过。
func TestBackgroundResultsStillLandWhileTheFullPlayerIsOpen(t *testing.T) {
	s, tt, rows := underlayShell(t)

	*rows = append(*rows, dlSnapshot("第一首", download.StatusRunning, dlProgress, dlChunk))
	dlFrame(tt)
	if len(s.downloads.rows) != 1 {
		t.Fatalf("起点就不对：这一页手上是 %d 条，want 1", len(s.downloads.rows))
	}

	// 满屏开着。
	s.openFullPlayer()
	tt.Frame()

	// 满屏期间数据源变了。
	*rows = append(*rows, dlSnapshot("第二首", download.StatusRunning, dlProgress, dlChunk))

	// 这一帧里：副作用照跑（数据取到了），而页面一个控件都没建。
	tt.Frame()
	if got := len(s.downloads.rows); got != 2 {
		t.Errorf("满屏期间这一页的手上还是 %d 条，want 2——"+
			"跳过绘制不能连带跳过每帧的副作用", got)
	}
}

// TestTheFirstFrameAfterClosingTheFullPlayerShowsTheCurrentState 钉住「关掉满屏
// 之后的**第一帧**就是当前状态」。
//
// ⚠️ **判据是画面上的字**：这一页画出来的是那几行记录，而数据是**每帧**从
// `rowsFn` 取的（`syncDownloads`）。满屏期间加的那一条，在关掉满屏之后的第
// 一帧就要在画面上——不能停在「打开那一刻」的样子。
//
// 拿掉这一票的改动之后这一条会红：那一页整个不建 → 它的副作用（取数据）也停了
// → 关掉满屏之后画出来的还是打开那一刻的那一份。
func TestTheFirstFrameAfterClosingTheFullPlayerShowsTheCurrentState(t *testing.T) {
	s, tt, rows := underlayShell(t)

	*rows = append(*rows, dlSnapshot("第一首", download.StatusRunning, dlProgress, dlChunk))
	dlFrame(tt)

	s.openFullPlayer()
	tt.Frame()

	// 满屏期间数据源变了。
	*rows = append(*rows, dlSnapshot("第二首", download.StatusRunning, dlProgress, dlChunk))
	tt.Frame()

	// 关掉满屏之后的**第一帧**。
	s.closeFullPlayer()
	tt.Frame()
	if !tt.HasText("第二首") {
		t.Errorf("关掉满屏之后的第一帧上没有满屏期间加的那一条，文字有 %q", tt.Texts())
	}
	if !tt.HasText("第一首") {
		t.Errorf("关掉满屏之后的第一帧上原来那一条也不见了，文字有 %q", tt.Texts())
	}
	// 而那一页确实被重新建了一次（不是「压根没画」所以才没有旧内容）。
	if s.pageBuilds == 0 {
		t.Error("这一页一帧都没建过——那上面那两条断言验的就不是「关掉满屏之后的第一帧」")
	}
}

// TestEscapeReturnsFromTheFullPlayerAtOnce 钉住「用快捷键从满屏返回时没有可
// 感知的延迟」。
//
// 判据是**按一次 Esc 之后的下一帧**：底下那一页已经画回来了（它的字在画面上），
// 而且它是被重新建过的（探针涨了）——不是「还要再等几帧才画回来」。
func TestEscapeReturnsFromTheFullPlayerAtOnce(t *testing.T) {
	s, tt, rows := underlayShell(t)

	*rows = append(*rows, dlSnapshot("第一首", download.StatusRunning, dlProgress, dlChunk))
	dlFrame(tt)

	s.openFullPlayer()
	tt.Frame()
	if tt.HasText("第一首") {
		t.Fatalf("满屏开着的时候底下那一页的字还在画面上，文字有 %q", tt.Texts())
	}
	before := s.pageBuilds

	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()

	if s.full.open {
		t.Fatalf("按了 Esc 全屏还开着，文字有 %q", tt.Texts())
	}
	if !tt.HasText("第一首") {
		t.Errorf("按了 Esc 之后的下一帧上，底下那一页还没画回来，文字有 %q", tt.Texts())
	}
	if s.pageBuilds <= before {
		t.Error("按了 Esc 之后底下那一页没有被重新建过——它停在打开满屏之前的样子了")
	}
}

// TestEveryPageWithContentHasASync 钉住「`pageSyncs` 那张表和 `builtPage` 是同一
// 件事的两面」。
//
// 漏一边的症状是**静默**的：那一页在满屏开着的时候副作用停更了，而界面上一个
// 字都不提（它被满屏那层盖着）。所以这里把它写成断言：加了新页而忘了往那张表
// 里加一行，这一票立刻红。
func TestEveryPageWithContentHasASync(t *testing.T) {
	s, tt := newShell(t)
	defer tt.Frame()

	for _, rt := range routes {
		hasBody := s.builtPage(rt) != nil
		_, hasSync := pageSyncs[rt.Match]
		if hasBody != hasSync {
			t.Errorf("路由 %q（%s）：有画法=%v，有每帧副作用=%v——"+
				"这两边必须同时有（见 underlay.go 的 pageSyncs）",
				rt.Match, rt.Title, hasBody, hasSync)
		}
	}
}
