package ui

// 「主窗口看不见的时候，迷你播放器照样跟着后台更新」的 E2E。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestTheMiniWindowKeepsUpWhileTheMainWindowIsUnseen
//
// # 这一条钉的是哪一件事
//
// 迷你播放器是**面板**（`Panel: true`，票 #162）：它浮在桌面上，用户看得见，
// 但点它**不激活应用**——于是它几乎永远没有焦点。拿焦点去判它看不看得见的话，
// 用户点一下播放键，进度条就再也不动了（见 presence.go 的 `freeWhileBlurred`）。
//
// 而主窗口失焦是**常态**：用户在别的应用里的时候，主窗口收在后台，迷你播放器
// 还在桌面上。这时候后台改了状态（音量、下载完成、换歌……），**迷你播放器
// 必须跟着更新**——不然用户眼前那一扇停在旧内容，而它正是用户唯一看着的那一扇。
//
// 这一条是 **E2E**：从「后台真的改了一份状态」开始，到「迷你窗的画面上真的
// 出现了新状态」为止。判据是**画面上的字**（无头 Tester 读的是控件的无障碍名
// 和可见文本），不是「有没有调过某个函数」。
//
// ⚠️ **一个字都不许打真 B 站**：这一条不碰网络（音量是纯本地状态）。
//
// # 为什么它和 window_seen_test.go 里那一条不是同一件事
//
// `window_seen_test.go` 的 `TestTheMiniWindowKeepsUpWhileTheMainWindowIsUnseen`
// 量的是**唤醒句柄那一层**（`wakeHandle.invalidateIf` 丢不丢唤醒）——它是
// 「逐窗口各算各的」那条铁律的单元证据。
//
// 而**这一条量的是结果**：后台改了状态之后，迷你窗的**画面**上有没有出现那个
// 新状态。单元那一层绿而画面没跟上，是完全可能的（接缝接错、忘了建那一帧、
// 画的是另一个外壳的那一份状态……）。所以两条都要。
//
// ⚠️ **两扇窗各画各的 Tester**：Tester 是拉式的（不 `Frame()` 一帧都不画），
// 而两个外壳各有各的 `wake` 句柄——用同一个 Tester 的话只有一扇窗被画到。

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// miniWakeFixture 是这一条用的夹具：主窗口 + 迷你播放器，各带一个 Tester。
type miniWakeFixture struct {
	main   *Shell
	mini   *Shell
	mainTT *mygoui.Tester
	miniTT *mygoui.Tester
	// app 是两扇窗**共享**的那一份应用状态（生产上就是同一个 `App`）。
	app *app.App
}

// newMiniWakeFixture 造「一个 App + 主窗口 + 迷你窗」，各带一个 Tester。
//
// ⚠️ **两扇窗各画各的**：它们各有各的 `wake` 句柄和 `presence`，而 Tester 是
// 拉式的——共用一个的话只有一扇窗会画到（`Shell.View` 里有 `s.wake.set(c)`，
// 两个外壳抢同一个句柄的话后画的那一个会把前一个的 `Services` 顶掉）。
func newMiniWakeFixture(t *testing.T) *miniWakeFixture {
	t.Helper()
	a := app.New()
	// `newTestShell` 的收尾会关掉 a；迷你那一条要先收（后注册先跑）。
	b := newTestShell(t, onApp(a), withoutTester())
	main := b.s
	mini := main.newMiniShell()
	t.Cleanup(mini.releaseMini)

	// 没有真窗口，`Attach` 那一步不会跑，于是主窗那条唤醒要手动登记——
	// 生产里它就在 `Attach` 里（铁律三：逐窗口）。迷你那条在
	// `newMiniShell` → `bindMiniWindow` 里已经登记好了。
	main.wakeRemove = a.AddWake(main.wake.invalidate)
	t.Cleanup(main.wakeRemove)

	f := &miniWakeFixture{
		main: main,
		mini: mini,
		app:  a,
	}
	f.mainTT = mygoui.NewTester(func(c *mygoui.Context) { main.View(c) }, 1200, 800)
	f.miniTT = mygoui.NewTester(func(c *mygoui.Context) { mini.View(c) }, miniWidth, miniHeight)
	// 先各画一帧：唤醒句柄的 `Services` 是**画的时候**登记的（`wake.set`），
	// 还没画过的那一帧 `invalidate` 会被丢掉（`held` 是假）。
	f.mainTT.Frame()
	f.miniTT.Frame()
	return f
}

// advanceUntil 推着钟等一个条件成立，**不自己调 `Frame()`**。
//
// # 为什么不用 `waitFrames`
//
// `waitFrames`（`shelltest_test.go`）每一轮都调 `tt.Frame()`——而 `Frame()` 会
// **自己置一个「有人要帧」的标记**（`mygo/ui/headless.go:448`：
// `func (t *Tester) Frame() { t.h.requested.Store(true); t.settle() }`）。
// 于是画面一定会更新——**哪怕根本没人叫醒过它**。
//
// 这一条要验的恰恰是「**有没有人叫醒它**」，所以那个标记不能由测试自己点着。
//
// # 为什么步长是 20ms
//
// Tester 的刷新率是 60Hz（`mygo/ui/headless.go` 的 `hz`），一帧是 16.67ms——
// 而 `Advance` 只画**落在窗口内**的那些帧（`due()` 按 `frameAt + 1/hz` 排期）。
// 推 16ms 会落在下一帧**之前**，于是这一轮什么也没画（实测过）。
//
// ⚠️ **它是「推够一帧」而不是「固定帧数」**：条件一成立就停，所以机器慢一点
// 也不会飘（AGENTS.md §2：不要用固定毫秒当判据）。
func advanceUntil(t *testing.T, tt *mygoui.Tester, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitBudget)
	for {
		tt.Advance(20 * time.Millisecond)
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了 %v，推着钟画了多少帧都还没：%s，文字有 %q", waitBudget, what, tt.Texts())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestTheMiniWindowShowsTheNewStateWhileTheMainWindowIsUnseen 是这一条的正文。
//
// 判据：**后台改了音量之后，迷你窗的画面上出现的是新音量**（静音那一枚的
// 无障碍名从「静音」变成「取消静音」）——而不是停在旧内容。
//
// ⚠️ **它和 `window_seen_test.go` 里那一条不是同一件事**：那一条量的是唤醒
// 句柄那一层（`FramePending`），这一条量的是**画面**。
func TestTheMiniWindowShowsTheNewStateWhileTheMainWindowIsUnseen(t *testing.T) {
	f := newMiniWakeFixture(t)

	// 起点：迷你窗画着「静音」那一枚（音量没被静音过）。
	if _, ok := f.miniTT.Find("静音"); !ok {
		t.Fatalf("起点就不对：迷你窗上没有「静音」那一枚，这一条就验不到东西（文字有 %q）",
			f.miniTT.Texts())
	}

	// 主窗口失焦（用户在别的应用里）。
	f.main.noteWindow(func(p *windowPresence) { p.focused = false })

	// ⚠️ **迷你窗也要失焦**——这才是生产上的常态：它是面板（`Panel: true`，
	// 票 #162），**点它不激活应用**，于是它几乎永远没有焦点（见 presence.go
	// 的 `freeWhileBlurred`）。
	//
	// ⚠️ **不把它弄失焦，这条就等于没验**：迷你窗**带着焦点**的时候，
	// 「看得见」是 `focused` 那一支说了算——`freeWhileBlurred` 那一支压根没被
	// 走到，于是「迷你窗失焦之后还看得见」这条**根本没被验**。
	// （实测：把 `seenLocked` 里的 `|| p.freeWhileBlurred` 去掉，这两条照样绿——
	// 那是这一条一开始的真实漏洞，靠变异测试发现的。）
	f.mini.noteWindow(func(p *windowPresence) { p.focused = false })

	// 后台改了一份状态：音量归零（于是静音了）。
	//
	// ⚠️ **这一条要的是「后台」那一支**：`App.SetVolume` 改完状态会叫醒
	// **每一个**登记过的窗口（铁律三，`wakeAll`）——而叫醒是**逐窗口**的：
	// 主窗看不见就丢掉，迷你窗看得见就接住。
	f.app.SetVolume(0)

	// ⚠️ **不要自己调 `Frame()`**：那会把「唤醒有没有接住」这一层绕过去——
	// Tester 是拉式的，自己 `Frame()` 一下画面就更新了，于是「迷你窗压根没
	// 被叫醒」这种坏法照样绿。
	//
	// 实测过这一条：把 `bindMiniWindow` 改成空操作（迷你窗压根不登记唤醒
	// 句柄），这条照样绿——因为自己 `Frame()` 把画面补上了。
	//
	// 所以这里**只推钟**：Tester 的 `Advance` 会画那些「有人要」的帧
	// （`mygo/ui/headless.go` 的 `Advance` → `t.asked()` 那一支）。
	if !f.miniTT.FramePending() {
		t.Fatalf("主窗口失焦、迷你窗口看得见，后台改了音量，迷你窗却没有要帧——" +
			"它会停在旧内容（铁律三：逐窗口叫醒）")
	}
	advanceUntil(t, f.miniTT, "迷你窗的画面上出现新状态", func() bool {
		_, ok := f.miniTT.Find("取消静音")
		return ok
	})

	if _, ok := f.miniTT.Find("取消静音"); !ok {
		t.Errorf("主窗口失焦之后，后台改了音量，迷你窗的画面上却没有跟着改（文字有 %q）——"+
			"迷你播放器是用户唯一看着的那一扇，它停在旧内容，用户看到的是过时的状态",
			f.miniTT.Texts())
	}

	// 而**主窗口那一扇**不应该因为这一下后台变化而要帧——它看不见（用户正
	// 在别的应用里），那些帧全是白烧的电（票 #156）。
	//
	// ⚠️ **主窗失焦时它会补一帧**（票 #468：那一帧是把常驻动画停下来的那一帧），
	// 所以这里比的是「**这一次后台变化之后**它有没有又要帧」，而不是「它现在
	// 有没有 pending」——后者在失焦那一刻就是真的。
	//
	// ⚠️ **得先把失焦那一下补的那一帧画掉**：`mainTT.Frame()` 画完它就停下
	// 了（那一帧之后它不再要下一帧）。
	f.mainTT.Frame()
	f.mainTT.Advance(time.Second)
	if f.mainTT.FramePending() {
		t.Error("主窗口失焦了，后台改了音量，它却又要起帧——" +
			"看不见的窗口那些帧全是白烧的电（票 #156）")
	}
}

// TestTheMiniWindowKeepsUpWhenOnlyTheMainWindowIsBlurred 断「迷你窗的唤醒
// **没有被丢掉**」这一层。
//
// ⚠️ **为什么单独一条**：上面那一条量的是**结果**（画面有没有跟上），而这一条
// 量的是**原因**（唤醒有没有被接住）。两条会给出同一个结果的情形有——
// 「叫醒被丢掉了，可画面恰好是对的」（比如上一帧之后状态没变）。分开量，
// 才知道坏在哪一层。
//
// ⚠️ **判据是 `invalidateIf` 的返回值**：那是 `wakeHandle` 那一层的原话
// （「有没有真的请求一帧」），主窗丢、迷你窗接住。
func TestTheMiniWindowKeepsUpWhenOnlyTheMainWindowIsBlurred(t *testing.T) {
	f := newMiniWakeFixture(t)

	// 主窗口失焦。
	f.main.noteWindow(func(p *windowPresence) { p.focused = false })

	// ⚠️ **迷你窗也失焦**（生产常态：它是面板，点它不激活应用，见上面那一条
	// 的同一段话）。少了这一句，`freeWhileBlurred` 那一支就没被走到。
	f.mini.noteWindow(func(p *windowPresence) { p.focused = false })

	// 主窗的可见性：看不见（唤醒该被丢掉）。
	if f.main.presence.seen() {
		t.Fatal("起点就不对：主窗口失焦了却还报「看得见」")
	}
	// 迷你窗的可见性：看得见（唤醒该被接住）。
	//
	// ⚠️ **这一条是迷你播放器那一族的身份**：它是面板，失焦也算看得见
	// （`freeWhileBlurred`）。要是这一条是假，症状是「用户在别的应用里
	// 的时候迷你窗的进度条不动了」。
	if !f.mini.presence.seen() {
		t.Fatal("起点就不对：迷你窗是面板，失焦也算看得见（`freeWhileBlurred`），" +
			"它不该报「看不见」")
	}

	// 唤醒：主窗丢掉、迷你窗接住。
	if f.main.wake.invalidateIf(true) {
		t.Error("主窗口失焦了，后台一叫它它还是接住了唤醒——" +
			"看不见的窗口那些帧全是白烧的电（票 #156）")
	}
	if !f.mini.wake.invalidateIf(true) {
		t.Error("主窗口失焦、迷你窗口看得见，后台却没能把迷你窗口叫醒——" +
			"它会停在旧内容（铁律三：逐窗口叫醒）")
	}
}
