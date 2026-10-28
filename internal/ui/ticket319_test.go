package ui

// 「暂停」与「停止」分得开 + 「这一串放完了」说出来（票 #319）。
//
// # 这一票要治的两处
//
//	**暂停 vs 停止**：暂停停在原地、停止回到开头，而界面上原来**分不出这两件事**
//	（唯一的差别是进度条那一格，而它在播放条上，不在状态栏里）。
//	**队列放完**：顺序播放走到队尾，界面上**一句话都没有**——分不出「这一串
//	放完了」和「暂停在开头」。
//
// # 判据都在**画面**上
//
// SPEC 的 Testing Decisions 说 UI 测试断渲染结果。所以下面那几条量的是画面上
// 有没有那句话、抽屉里那一行的底色变没变——不是「有没有调过某个函数」。
//
// # 为什么不走整窗断抽屉里那一行
//
// 停住的时候画面上有**两个**「已停止」：播放条上那一句，和抽屉里那一行的副题。
// 而 `Tester.Find` 只给**第一个**——播放条那个先建（`viewMain` 的次序：播放条
// 在 `drawUnderlay` 里，抽屉在它后面）。整窗里断不出「抽屉里那一行说的是什么」。
// 所以抽屉那几条**单独画抽屉**（见 `queueDrawerTester`）。

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// ---- 播放条上那一句 ----

// TestThePlayBarSaysTheTrackIsStopped 断**停住了的时候播放条上有一句「已停止」**。
//
// ⚠️ **判据是「画面上有这句话」**：停住和暂停在按钮上长得一模一样（都是「播放」），
// 而进度条那一格又不在这条线上——用户在这条线上读到的必须是**一句话**。
func TestThePlayBarSaysTheTrackIsStopped(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "一首歌", Stopped: true,
	}, 1200)
	if !tt.HasText("已停止") {
		t.Errorf("停住了，播放条上却没有「已停止」这一句，文字有 %q", tt.Texts())
	}
}

// TestThePlayBarSaysTheQueueIsDone 断**队列放完时播放条上有一句「这一串放完了」**。
//
// ⚠️ **而且说的不是「已停止」**：那是「用户按了停止」那一档的话。顺序播放走到
// 队尾的时候队列里没有下一首了——说「已停止」是在说另一件事。
func TestThePlayBarSaysTheQueueIsDone(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "最后一首", Stopped: true, QueueFinished: true,
	}, 1200)
	if !tt.HasText("这一串放完了") {
		t.Errorf("队列放完了，播放条上却没有「这一串放完了」，文字有 %q", tt.Texts())
	}
	if tt.HasText("已停止") {
		t.Errorf("队列放完时说成了「已停止」——那是「用户按了停止」那一档的话，"+
			"而这里没有下一首了，文字有 %q", tt.Texts())
	}
}

// TestAPausedTrackIsNotSaidToBeStopped 断**暂停不是停止**。
//
// 这一条是那一对的另一半：只断「停住了会说已停止」的话，把「暂停」也算成停住
// 照样绿——而那正是用户抱怨的那件事（按了暂停，界面说「已停止」）。
func TestAPausedTrackIsNotSaidToBeStopped(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "一首歌", Playing: false,
	}, 1200)
	for _, say := range []string{"已停止", "这一串放完了"} {
		if tt.HasText(say) {
			t.Errorf("暂停着的歌被说成了 %q，文字有 %q", say, tt.Texts())
		}
	}
}

// TestStoppingTheTrackSaysSoOnThePlayBar 断**真按了停止**之后界面上有那一句。
//
// 上面那几条喂的是手摆的状态，这一条走真的 App：按停止 → 快照 → 界面。
// 断的是**接线**（`State.Stopped` 有没有真的送到界面上），不是画法。
func TestStoppingTheTrackSaysSoOnThePlayBar(t *testing.T) {
	h := newHarness(t)
	enqueue(t, h, copiesOf(t, 1)...)

	h.app.Stop()
	h.tt.Frame()

	if !h.tt.HasText("已停止") {
		t.Errorf("按了停止，播放条上却没有「已停止」，文字有 %q", h.tt.Texts())
	}
}

// ---- 抽屉里那一行 ----

// TestTheQueueDrawerSaysTheTrackIsStopped 断**抽屉里那一行的副题是「已停止」**。
//
// 那一行的副题平常是它的来源（「本地音乐」/「B 站音乐」…）。停住的时候它说的
// 是「停住了」——因为用户盯着这一行看的是「为什么没声了」。
func TestTheQueueDrawerSaysTheTrackIsStopped(t *testing.T) {
	tt, s := queueDrawerTester(t, false)

	s.app.Stop()
	tt.Frame()

	if !tt.HasText("已停止") {
		t.Errorf("停住了，抽屉里那一行却没有说「已停止」，文字有 %q", tt.Texts())
	}
}

// TestTheStoppedRowIsNotHighlightedLikeAPlayingOne 断**停住的那一行不再高亮成
// 「正在播放」**。
//
// 这是这一票在抽屉上的那一条：停住之后那一行仍涂成主色淡底，和暂停**一模一样**
// ——用户在界面上分不出这两件事（票 #319 的现象，报告 §4.1.1 C10）。
//
// ⚠️ **判据是「底色」**：那一行的底在两种状态下必须不同（主色淡底 → 面板底）。
//
// ⚠️ **量的那个点在行内的留白上**（往右 200 像素、行底往上 4 像素），不在文字
// 上：两种状态那一行的副题不一样（来源 / 「已停止」），拿字形去比的话这一条会
// 在**画法完全没变**的情况下也绿——而那正是它要抓的那种坏。
func TestTheStoppedRowIsNotHighlightedLikeAPlayingOne(t *testing.T) {
	tt, s := queueDrawerTester(t, false)

	row, ok := tt.Find("a")
	if !ok {
		t.Fatalf("抽屉里找不到那一行，文字有 %q", tt.Texts())
	}
	playing := pixelAt(tt.Image(), row.X+200, row.Y+row.H-4)

	// 停住。
	s.app.Stop()
	tt.Frame()

	row, ok = tt.Find("a")
	if !ok {
		t.Fatalf("停住之后抽屉里找不到那一行，文字有 %q", tt.Texts())
	}
	stopped := pixelAt(tt.Image(), row.X+200, row.Y+row.H-4)

	if stopped == playing {
		t.Errorf("停住之后那一行的底色没变（%v）——它仍高亮成「正在播放」，"+
			"和暂停一模一样", stopped)
	}
}

// queueDrawerTester 只画抽屉那一块（不画整窗），摆一首在放的歌并把抽屉拉开。
//
// ⚠️ **为什么绕开整窗**：见文件头那一段——停住的时候画面上有两个「已停止」，
// 而 `Tester.Find` 只给第一个（播放条那个先建）。单独画抽屉的话，画面上只有
// 抽屉里那一个。
//
// ⚠️ **抽屉自己不装主题**（铁律二：它在 `viewMain` 的同一个 build pass 里建），
// 隔离出来画就得自己装——理由同 `framebudget_test.go` 里那一档。
//
// ⚠️ **深浅是建壳的时候定的**（`systemIsDark`）：「建好之后再 `SetDark`」那一手
// 在 Tester 里**不重画**（理由同 `playbar_state_shot_test.go`）。
func queueDrawerTester(t *testing.T, dark bool) (*mygoui.Tester, *Shell) {
	t.Helper()
	return queueDrawerTesterWith(t, dark, copiesOf(t, 2)...)
}
