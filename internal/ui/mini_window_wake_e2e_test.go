package ui

// 「主窗口看不见的时候，迷你播放器照样跟着后台更新」的 E2E。
//
//	go test ./internal/ui/ -run 'TestTheMiniWindow'
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
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/hover"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shimmer"
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
//
// ⚠️ **收尾把「看得见」那个开关还原**：`hover.SetVisibleFlag` /
// `shimmer.SetVisibleFlag` 装的是**包级全局变量**，而 `Shell.View` 每帧都把它
// 换成**自己那一份**（`shell_view.go` 的 `hover.SetVisibleFlag(visible)`）——
// 于是这一组测完，那个全局指着**最后一扇画过的窗**，而最后一扇画的是**失焦的
// 主窗**（值是假）。
//
// 而基座（`shelltest_test.go` 的 `newTestShell`）本来就管这件事：它每条测试
// 都装一个新的、看得见的开关，为的就是「上一条测试留下的『看不见』会漏进
// 这一条」。这一组**绕过**了那个纪律（两个外壳轮流覆盖全局），所以得自己
// 还原——否则后面某条测试读到这个全局时读到的是「看不见」。
func newMiniWakeFixture(t *testing.T) *miniWakeFixture {
	t.Helper()
	a := app.New()
	// `newTestShell` 的收尾会关掉 a；迷你那一条要先收（后注册先跑）。
	b := newTestShell(t, onApp(a), withoutTester())
	main := b.s
	mini := main.newMiniShell()
	t.Cleanup(mini.releaseMini)

	// 没有真窗口，`Attach` 那一步不会跑，于是主窗那条唤醒要手动登记——
	// 生产里它就在 `Attach` 里（铁律三：逐窗口）。
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

	// ⚠️ **还原「看得见」那个包级全局**（理由见上面那段话）。
	//
	// ⚠️ **不能直接还原成基座装的那一个**：那个已经被两扇窗轮流覆盖过了，
	// 而它现在指的值是「最后一扇画过的窗」的。装一个**新的、看得见的**开关
	// 才是「每条测试都从看得见开始」那个纪律的意思。
	base := hover.NewVisibleFlag()
	hover.SetVisibleFlag(base)
	shimmer.SetVisibleFlag(base)
	t.Cleanup(func() {
		restored := hover.NewVisibleFlag()
		hover.SetVisibleFlag(restored)
		shimmer.SetVisibleFlag(restored)
	})
	return f
}

// drain 把一扇窗**欠着的那些帧**画掉，并确认它画完之后不再要下一帧。
//
// # 为什么非要有它
//
// 窗口**刚变得看不见**的时候，`noteWindow` 会**补一帧**（票 #468）——那一帧
// 是「把常驻动画停下来」的那一帧。于是「这扇窗现在 pending」这件事，
// **说的不是「有没有人叫醒过它」**，而是「它还欠着 #468 补的那一帧」。
//
// ⇒ **任何要断「后台叫醒有没有被接住」的判据，都得先把那一帧排掉**——
// 否则两个原因共用一个判据，分不开（而这正是本 PR 修的那一条旧测试的病）。
//
// # 它断的是「画完之后不再要下一帧」
//
// 不是「画了几帧」——那不是判据（「看不见」之后它本就该停）。
func drain(t *testing.T, tt *mygoui.Tester, what string) {
	t.Helper()
	tt.Frame()
	tt.Advance(50 * time.Millisecond)
	if tt.FramePending() {
		t.Fatalf("起点就不对：%s 排掉那些欠着的帧之后，它还在要帧——"+
			"下面「后台叫醒它」就分不出是这一次叫醒的了", what)
	}
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
// ⚠️ **`waitFramesClock` 也不行**：它是 `Advance` **加** `Frame()`（同样会自己
// 置那个标记）。
//
// # 为什么步长是 20ms
//
// Tester 的刷新率按 60Hz 算（`mygo/ui/headless.go` 的 `due()`：���值是 0 时
// 兜底成 60），一帧是 16.67ms——而 `Advance` 只画**落在窗口内**的那些帧
// （`due()` 按 `frameAt + 1/hz` 排期）。推 16ms 会落在下一帧**之前**，于是
// 这一轮什么也没画（实测过）。
//
// ⚠️ **它是「推够一帧」而不是「固定帧数」**：条件一成立就停，所以机器慢一点
// 也不会飘（AGENTS.md §2：不要用固定毫秒当判据）。那个 deadline 用的是
// 共用的 `waitBudget`（10 秒保险丝），和 `waitFrames` 一样。
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
	f.mini.noteWindow(func(p *windowPresence) { p.focused = false })

	// ⚠️ **把两扇窗欠着的帧都排掉**（票 #468 补的那一帧，见 `drain` 的注释）：
	// 不排掉的话，「画面上出现了新状态」这件事可能只是那一帧的功劳——
	// **它和「后台叫醒了迷你窗」是两种原因**，而这一条要量的是后者。
	//
	// ⚠️ 排掉之后画面上是**旧的**（音量还没改），于是下面「画面变了」只可能
	// 是后台叫醒带来的。
	drain(t, f.mainTT, "主窗")
	drain(t, f.miniTT, "迷你窗")

	// 后台改了一份状态：音量归零（于是静音了）。
	//
	// ⚠️ **这一条要的是「后台」那一支**：`App.SetVolume` 改完状态会叫醒
	// **每一个**登记过的窗口（铁律三，`wakeAll`）——而叫醒是**逐窗口**的：
	// 主窗看不见就丢掉，迷你窗看得见就接住。
	f.app.SetVolume(0)

	// ⚠️ **迷你窗得自己「要帧」**：那正是「后台叫醒了它」的样子（唤醒 =
	// 要一帧）。
	//
	// ⚠️ **不要自己调 `Frame()`**：那会把「唤醒有没有接住」这一层绕过去——
	// Tester 是拉式的，自己 `Frame()` 一下画面就更新了，于是「迷你窗压根没
	// 被叫醒」这种坏法照样绿。
	if !f.miniTT.FramePending() {
		t.Fatalf("主窗口失焦、迷你窗口看得见，后台改了音量，迷你窗却没有要帧——" +
			"它会停在旧内容（铁律三：逐窗口叫醒）")
	}

	// 🔴 **主窗口不该要帧**：它看不见（用户正在别的应用里），那些帧全是白烧
	// 的电（票 #156）。
	//
	// ⚠️ **这一条要在 `Frame()` 之前断**：`Tester.Frame()` 会把挂着的请求吃掉
	// （它自己置 `requested=true` 然后 settle），于是先 `Frame()` 再断的话，
	// 断的是「排掉之后的状态」——**信号被自己抹掉了**（实测：把
	// `invalidateIf` 改成不问 `seen`，这一条照样绿）。
	if f.mainTT.FramePending() {
		t.Error("主窗口失焦了，后台改了音量，它却又要起帧——" +
			"看不见的窗口那些帧全是白烧的电（票 #156）")
	}

	// 而迷你窗画出来的那一帧上，静音那一枚的名字变了。
	advanceUntil(t, f.miniTT, "迷你窗的画面上出现新状态", func() bool {
		_, ok := f.miniTT.Find("取消静音")
		return ok
	})
	if _, ok := f.miniTT.Find("取消静音"); !ok {
		t.Errorf("主窗口失焦之后，后台改了音量，迷你窗的画面上却没有跟着改（文字有 %q）——"+
			"迷你播放器是用户唯一看着的那一扇，它停在旧内容，用户看到的是过时的状态",
			f.miniTT.Texts())
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

// TestTheMiniWindowStopsWhenItIsHiddenOrMinimized 钉住迷你窗**自己**看不见的
// 那两档。
//
// # 为什么单独一条
//
// `freeWhileBlurred` 是「**不聚焦**也算看得见」——而它**不该**盖过「藏起来了」
// 和「最小化了」：迷你窗被藏起来之后，它当然不该在后台继续画。
//
// ⚠️ **这一档现在钉不住**：现有的 `TestAnUnseenWindowStopsAskingForFrames`
// 量的是**单窗**的失焦/最小化/隐藏，而 `freeWhileBlurred` 只在迷你窗上有——
// 于是「两扇窗都在」的场景里，迷你窗那两档没人管。
//
// ⚠️ **要防的那一种改法**：「迷你窗是面板，它永远看得见」——听起来很合理，
// 而它会让迷你窗藏起来之后还在烧电。
func TestTheMiniWindowStopsWhenItIsHiddenOrMinimized(t *testing.T) {
	for _, tc := range []struct {
		name   string
		unseen func(*windowPresence)
	}{
		{"藏起来", func(p *windowPresence) { p.shown = false }},
		{"最小化", func(p *windowPresence) { p.minimized = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMiniWakeFixture(t)

			// 迷你窗失焦（生产常态：它是面板）。
			f.mini.noteWindow(func(p *windowPresence) { p.focused = false })

			// 然后它自己看不见了。
			f.mini.noteWindow(tc.unseen)

			if f.mini.presence.seen() {
				t.Fatalf("起点就不对：迷你窗%s之后还报「看得见」——"+
					"它藏在后台，却还在按帧画（白烧的电）", tc.name)
			}

			// 排掉欠着的帧，然后让后台改一次状态。
			drain(t, f.miniTT, "迷你窗")
			f.app.SetVolume(0.42)

			// 它不该要帧。
			if f.miniTT.FramePending() {
				t.Errorf("迷你窗%s之后，后台改了状态，它却还要帧——"+
					"用户看不到这一扇，那些帧全是白烧的电（票 #156）", tc.name)
			}
		})
	}
}
