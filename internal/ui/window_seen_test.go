package ui

// 这一组是票 #156 第二条（失焦/隐藏时不再逐帧画界面）的回归证据。
//
// # 为什么判据是「帧数」和 `FramePending`，不是画面上画了什么
//
// 「窗口看不见」在画面上什么都不该变——用户看不到它。能出问题的只有**帧还在
// 不在跑**：看得见时每秒几十帧，看不见时一帧都不该有。而 `Tester` 是拉式的
// （不 `Frame()` 一帧都不画），唯一能把「有人在要帧」和「没人要帧」分开的，
// 是 `FramePending()`——它报的正是 view 自己有没有在请求下一帧。
//
// # 看不见的那个状态怎么摆
//
// 无头 `Tester` 没有真窗口，系统不会给它推焦点/隐藏/最小化。所以这一组直接改
// `Shell.presence`（`noteWindow`）——那正是生产上窗口回调改的那一份状态
// （`watchWindow`），不是测试专用的旁路。三种原因各测一遍：失焦、最小化、隐藏。
//
// ⚠️ **不能用 `tt.Frame()` 的返回值数帧**：它调一次 `settle()`，而 settle 自己
// 可能画二十帧（波形每帧都要下一帧）。数「`Frame()` 调了几次」什么也说明不了。
// 所以每个 testShell 外面包一层计数的 view（`countingShell`）。

import (
	"math"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/hover"
	mygoui "github.com/egoist/mygo/ui"
)

// countingShell 造一个停在「稍后再看」的外壳，画面上有一首正在放的歌（于是有
// 那个每帧都要下一帧的跳动波形），并记下画了多少帧。
//
// ⚠️ **「哪一首在放」是换上去的接线给的**：判据认的是「**此刻有声音的那一首**」
// （`app.App.IsPlaying`），而这一层没法让一首在线的歌真的出声——所以换成一份
// 说「第一首在放」的接线。这一条要量的是**窗口看不见时还画不画**，波形只是
// 那一帧里「有东西在动」的凭据。
func countingShell(t *testing.T) (*Shell, *mygoui.Tester, *int) {
	t.Helper()
	f := newLibFake(t).withLater(1, 4, libTitles...)
	frames := 0
	tt, s := libraryShellOpts(t, f, laterPath, withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			frames++
			s.View(c)
		}
	}))
	waitFrames(t, tt, "稍后再看第一页", func() bool { return rowsShown(&s.laterPage) == 4 })

	rows := s.laterPage.rows
	items := rows.Rows()
	playing := items[0].Item.Identity()
	rows.acts.IsPlaying = func(it app.Item) bool { return it.Identity() == playing }
	tt.Frame()
	if _, ok := tt.Find(hover.PlayingLabel(libTitles[0])); !ok {
		t.Fatalf("起点就不对：正在放的那一行没有跳动波形，这一条就什么都验不到（文字有 %q）",
			tt.Texts())
	}
	return s, tt, &frames
}

// TestAnUnseenWindowStopsAskingForFrames 是这一票的核心：窗口看不见时，那一帧
// 画完之后**不再要下一帧**。
//
// 三种「看不见」各测一遍，因为它们走的是三个不同的窗口回调（`OnBlur` /
// `OnMinimize` / `OnHide`），而漏掉任何一个的症状都一样：那一扇窗的用户界面上
// 看不出异样，只是笔记本在包里慢慢变热。
func TestAnUnseenWindowStopsAskingForFrames(t *testing.T) {
	for _, tc := range []struct {
		name   string
		unseen func(*windowPresence)
	}{
		{"失焦", func(p *windowPresence) { p.focused = false }},
		{"最小化", func(p *windowPresence) { p.minimized = true }},
		{"隐藏", func(p *windowPresence) { p.shown = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, tt, frames := countingShell(t)

			// 先钉住**看得见的时候真的在逐帧画**：不然下面那条「看不见」可能只是
			// 因为根本没有动画在跑，绿得毫无意义。
			//
			// ⚠️ 那一次 `invalidate` 是模拟 `App.tick`：在放歌的时候它每 50ms 叫醒
			// 每个窗口一次。Tester 的时钟要有人先要帧，`Advance` 才会按刷新率画。
			//
			// ⚠️ **判据是 `FramePending`，不是「重建了几次」**：波形要的那些帧走的是
			// **局部重绘**（票 #392），不重建——数重建是数不出来的。而这一档里
			// **没有别的人要帧**（`countingShell` 只摆队列不起播，`App` 的 `live`
			// 为假，于是 `App.tick` 从不叫醒窗口），所以 `FramePending` 为真就是
			// 波形要的。
			if !s.wake.invalidateIf(true) {
				t.Fatal("看得见的窗口却把唤醒丢掉了")
			}
			before := *frames
			tt.Advance(500 * time.Millisecond)
			if !tt.FramePending() {
				t.Fatalf("看得见、有歌在放，推进半秒画面却不再要下一帧——" +
					"跳动波形没有在逐帧动画，下面那条「看不见时停帧」就验不到东西")
			}

			// 看不见。
			s.noteWindow(tc.unseen)
			// 这一帧是「最后那一下」：正在飞的动画要把它收掉（波形在这里看到
			// 窗口看不见了，就不再要下一帧）。它画几帧都行。
			tt.Frame()
			if tt.FramePending() {
				t.Errorf("窗口看不见了，画面还在请求下一帧")
			}

			// 此后一秒里 `App.tick` 叫了二十次（在放歌的时候它每 50ms 一次）。
			// 哪些帧一次都不该画：唤醒被窗口丢掉，而画面自己也不要下一帧。
			before = *frames
			for range 20 {
				if s.wake.invalidateIf(true) {
					t.Error("看不见的窗口把唤醒画进去了——`wakeHandle.invalidate` 没有挡")
				}
			}
			tt.Advance(time.Second)
			if got := *frames - before; got != 0 {
				t.Errorf("窗口看不见的一秒里重建了 %d 次整棵元素树，want 0——"+
					"用户看不到这一扇窗，那些帧全是白烧的电", got)
			}

			// ⚠️ **还要断「画面自己不再要下一帧」**：上面那条数的是**重建**，
			// 而波形要的那些帧走**局部重绘**（不重建）——数重建是数不出来的，
			// 于是「窗口看不见了可它还在按帧重画」那种回归会从它底下溜过去
			//（票 #392 把它从「每一帧重建」改成了「每一帧只重画」）。
			//
			// 所以这里**再送一帧进去**（`invalidateNow` 绕过「看不看得见」那道
			// 闸门，模拟「外面来的」一帧：输入、后台结果……），然后断**那一帧
			// 之后画面不再要下一帧**。
			s.wake.invalidateNow()
			tt.Advance(time.Second)
			if tt.FramePending() {
				t.Errorf("窗口看不见了，送进去一帧之后画面还在要下一帧——" +
					"它会一直按帧重画，用户看不到这一扇窗，那些帧全是白烧的电")
			}

			// 而「看不见」不是永久的：窗口回来之后必须接着画（下面那条
			// TestAWokenWindowCatchesUp 是它更完整的一半，这里只确认这一条
			// 不是靠「把窗口弄坏了」才绿的）。
			s.noteWindow(func(p *windowPresence) {
				p.focused, p.minimized, p.shown = true, false, true
			})
			if !tt.FramePending() {
				t.Error("窗口重新看得见了，却没有请求那一帧——界面会停在看不见之前的那一刻")
			}
			tt.Frame()
		})
	}
}

// TestAWokenWindowCatchesUp 钉住「回来之后立刻恢复，而不是一帧一帧追」。
//
// 进度和波形本来就是按**当前**状态/时钟算的（`Snapshot` 与 `c.Now()`），所以
// 「对齐」这一半主要落在歌词的滚动上（见 TestTheLyricsJumpBackWhenTheWindowIsBack）。
// 这条管的是另一半：回来的那一帧真的被要了，之后逐帧动画也真的续上了。
func TestAWokenWindowCatchesUp(t *testing.T) {
	s, tt, frames := countingShell(t)

	// 看不见，并让画面停在「已经停手」的状态。
	s.noteWindow(func(p *windowPresence) { p.focused = false })
	tt.Frame()
	if tt.FramePending() {
		t.Fatal("起点就不对：窗口看不见了，画面还在要帧")
	}

	// 回来。
	before := *frames
	s.noteWindow(func(p *windowPresence) { p.focused = true })
	if !tt.FramePending() {
		t.Fatal("窗口回来的那一刻没有请求一帧——界面会停在看不见之前的那一刻，" +
			"用户切回来看到的是旧进度、旧歌名")
	}
	tt.Frame()
	if got := *frames - before; got == 0 {
		t.Error("窗口回来后那一帧没有画出来")
	}

	// 后面照常逐帧：再叫醒一次、推半秒，波形应当继续跳。
	before = *frames
	if !s.wake.invalidateIf(true) {
		t.Fatal("窗口已经回来了，唤醒却又被丢掉了")
	}
	tt.Advance(500 * time.Millisecond)
	if *frames <= before {
		t.Error("窗口回来后动画没有续上——波形停了，而用户正看着它")
	}
}

// TestTheMiniWindowKeepsUpWhileTheMainWindowIsUnseen 钉住「多窗口各算各的」。
//
// 主窗口失焦不等于迷你播放器看不见：两扇窗各有各的 `presence`、各有各的唤醒
// 句柄，而 `App.wakeAll` 仍然挨个叫醒（铁律三不变）。这一条量的是「叫醒之后的
// 那一扇窗自己做的决定」——主窗把唤醒丢掉，迷你窗照常接住。
func TestTheMiniWindowKeepsUpWhileTheMainWindowIsUnseen(t *testing.T) {
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

	mainFrames, miniFrames := 0, 0
	mainTT := mygoui.NewTester(func(c *mygoui.Context) { mainFrames++; main.View(c) }, 1200, 800)
	miniTT := mygoui.NewTester(func(c *mygoui.Context) { miniFrames++; mini.View(c) }, miniWidth, miniHeight)

	// 主窗失焦（用户在看别的应用），迷你窗还浮在桌面上。
	main.noteWindow(func(p *windowPresence) { p.focused = false })

	// 一次普通的后台状态变化：`App` 挨个叫醒两扇窗。
	a.SetVolume(0.42)

	if mainTT.FramePending() {
		t.Error("主窗口失焦了，后台一叫它还是请求了一帧")
	}
	if !miniTT.FramePending() {
		t.Error("主窗口失焦、迷你窗口看得见，后台却没能把迷你窗口叫醒——" +
			"它会停在旧内容（铁律三：逐窗口叫醒）")
	}

	// 两扇窗真画一帧：主窗照画（看不见不等于不能画，只是没人叫它），
	// 迷你窗照常。
	beforeMain, beforeMini := mainFrames, miniFrames
	mainTT.Frame()
	miniTT.Frame()
	if mainFrames == beforeMain || miniFrames == beforeMini {
		t.Errorf("帧数没有增长（main %d→%d，mini %d→%d）",
			beforeMain, mainFrames, beforeMini, miniFrames)
	}
}

// TestTheSpectrumStopsAndResumesWithTheWindow 钉住满屏那一条：**看得见的时候
// 动画照常**（省电不该省掉该有的效果），看不见的时候同样停下来。
//
// 满屏的频谱是第二处「只要在放就每帧要下一帧」的地方（另一处是列表里的波形）。
// 两个都得门控：只挡住 `App.tick` 的唤醒是不够的——这一点上框架的持有帧
// （`host.occluded`）也帮不上忙：**它挡的是「窗口不可见」**（含被别的窗口盖住、
// 最小化、隐藏），而**失焦**的窗口在框架眼里仍是可见的（`NSWindow.occlusionState`
// 还在 visible 位上——见 presence.go 里 `watchWindow` 那一段）。
func TestTheSpectrumStopsAndResumesWithTheWindow(t *testing.T) {
	s, tt, _ := fullTester(t)
	s.app.SetFullPlayerPrefs(app.FullPlayerPrefs{ShowSpectrum: true})
	openFull(t, tt)
	if _, ok := tt.Find("频谱"); !ok {
		t.Fatalf("满屏里没有频谱，这一条就没东西可验（文字有 %q）", tt.Texts())
	}
	// 把开场那段共享元素转场走完（假时钟推 1.5 秒）。不推完的话，下面「看得见
	// 时在要帧」可能是转场要的，而不是频谱——那一条就红得/绿得都不是因为它。
	for range 5 {
		tt.Advance(300 * time.Millisecond)
	}

	// 看得见：频谱每一帧都要下一帧（settle 会一直画到 20 帧上限）。
	tt.Frame()
	if !tt.FramePending() {
		t.Fatal("窗口看得见、歌在放、频谱开着，画面却不要下一帧了——动画停了")
	}

	// 看不见。
	s.noteWindow(func(p *windowPresence) { p.focused = false })
	tt.Frame()
	if tt.FramePending() {
		t.Error("窗口看不见了，频谱还在要下一帧")
	}

	// 回来。
	s.noteWindow(func(p *windowPresence) { p.focused = true })
	tt.Frame()
	if !tt.FramePending() {
		t.Error("窗口回来了，频谱没有接着动")
	}
}

// TestStoppingFramesDoesNotStopPlayback 钉住「停帧不等于停播」。
//
// 这一条不是在验 `App`（这一票没碰它），而是把两件事摆在一起量：窗口**看不见**
// 的时候一帧都不画，而**播放位置照样往前走**。反过来写错过（比如把唤醒门控挪进
// `App.tick`）的话，停下来的会是播放本身——那是这一票最不能付的代价。
func TestStoppingFramesDoesNotStopPlayback(t *testing.T) {
	s, tt := lyricShell(t)

	// 窗口看不见，并且画面已经停手。
	s.noteWindow(func(p *windowPresence) { p.focused = false })
	tt.Frame()
	if tt.FramePending() {
		t.Fatal("起点就不对：窗口看不见了，画面还在要帧")
	}

	// 让这一首真的放起来（`lyricShell` 为了保证位置可重复，把它暂停了）。
	s.app.Play()
	if !s.app.Snapshot().Playing {
		t.Skip("这台机器上放不了真音频（没有可用的音频设备），「停帧不等于停播」这一条验不到")
	}
	before := s.app.Snapshot().Position

	// 过程中**不画帧**：真窗口在这个状态下本来就没有帧。`App.tick` 那个每 50ms
	// 的钟仍然在跑，位置是它更新的。
	waitFor(t, "看不见的窗口里歌还在往前走", func() bool {
		st := s.app.Snapshot()
		return st.Playing && st.Position > before
	})

	if tt.FramePending() {
		t.Error("看不见的窗口在播放期间又开始要帧了")
	}
}

// TestTheLyricsJumpBackWhenTheWindowIsBack 钉住「回来之后一次跳到正确的位置」。
//
// # 为什么只有歌词需要这一条
//
// 进度条和波形是按**当前状态/时钟**算出来的（`App.Snapshot()` 与 `c.Now()`），
// 窗口一回来画出的就是此刻该有的样子。歌词的滚动量却是一段**弹簧动画的状态**：
// 看不见的那段时间里唱到第几句它不知道，回来之后若还按普通那一路走
// （`ScrollTo(…, lyricScrollAnim)`），就会从旧位置一帧一帧滚过去——用户看到的
// 是一段补放，而他要的是「现在唱到哪一句」。
//
// # 怎么量
//
// 先在同一扇窗上量出「第 10 句居中时的滚动量」当作基准，再回到第 2 句、把窗口
// 弄看不见、在看不见的时候唱到第 10 句，最后让窗口回来。回来之后**只画两帧**，
// 滚动量就应该已经是那个基准——弹簧动画在两帧里走不完，所以这条抓得住
// 「慢慢追」。
func TestTheLyricsJumpBackWhenTheWindowIsBack(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)

	// 基准：第 10 句居中时的滚动量。
	seekTo(t, s, tt, 10)
	want := s.lyric.scroll.Y

	// 退到第 2 句，让「看不见」之前的位置和回来之后差得足够远。
	seekTo(t, s, tt, 2)
	if math.Abs(float64(s.lyric.scroll.Y-want)) < 20 {
		t.Fatalf("起点就不对：第 2 句和第 10 句的滚动量只差 %.2f，这一条分不出追没追",
			math.Abs(float64(s.lyric.scroll.Y-want)))
	}

	// 看不见。
	s.noteWindow(func(p *windowPresence) { p.focused = false })
	tt.Frame()

	// 看不见的那段时间里，歌自己唱到了第 10 句。**故意不画帧**：真窗口在这个
	// 状态下本来就没有帧，画了就等于替产品把「不可见」这个条件撤掉了。
	s.app.SeekTo(10 * lyricStepMS * time.Millisecond)

	// 回来。回来那一帧算目标（`ScrollTo` 在树建完之后才设，所以目标要下一帧
	// 才摆进 glide），再推 1ms 让那一段 1ns 的动画到期，然后落地——**两帧
	// 加 1ms**，这是框架的次序，不是慢慢追。
	//
	// ⚠️ 中间那次 `Advance` 不能省：Tester 的钟站着不动，而 glide 是按时钟
	// 推进的（`dt <= 0` 时一动不动），只 `Frame()` 几次它永远停在原地。
	s.noteWindow(func(p *windowPresence) { p.focused = true })
	tt.Frame()
	tt.Advance(time.Millisecond)
	tt.Frame()

	got := s.lyric.scroll.Y
	if math.Abs(float64(got-want)) > 2 {
		t.Errorf("窗口回来之后歌词停在第 2 句那一带（Y=%.2f），want %.2f（第 10 句居中）——"+
			"这一段是慢慢追过去的，不是一次到位", got, want)
	}
}
