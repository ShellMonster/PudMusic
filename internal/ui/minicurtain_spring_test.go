package ui

// 迷你播放器那个「歌词卷帘」的**弹簧**和**两道轴**这一组（票 #307）。
//
// # 这一组量的是什么
//
// 票 #307 把卷帘从**定时长曲线**（240ms + `motion.Unified`）换成了**自己
// 积分的弹簧**（见 `minicurtain_spring.go`），而这一组盯的是四件事：
//
//  1. **展开是弹簧**：按 Issue 指定的 `w = 10.5` 展开，**回弹两三下、幅度递减**；
//  2. **收起不弹**（过阻尼 ζ = 1.1），一路收回 84；
//  3. **窗高全程 ≤ 84 + 236**，而**每一帧窗高 == 内容高**（过冲那一帧也不许
//     超过目标——透明的那一块照样吃点击，文件头第 1 条）；
//  4. **两道轴**：上轴在迷你窗顶边，下轴在卷帘下沿，**下沿到哪轴到哪**。
//
// # 为什么逐帧推钟
//
// ⚠️ **推钟不数帧**：这一段是**按时间积分**的（弹簧自己按 `c.Now()` 的差
// 积分），而无头 Tester 的钟站着不动。⇒ 每一步推 **20 毫秒**——无头那一档
// 的刷新是 60Hz（16.7 毫秒一帧），推 20 毫秒正好跑**一帧**（下一帧在 33
// 毫秒，落在窗口外）。
//
// ⚠️ **别在 `tt.Advance` 之后又 `tt.Frame()`**：那会在同一个时刻上再跑一帧
// （`dt = 0`，弹簧不动），而**第一帧**会把那 20 毫秒里剩下的零头也吃掉
// ——弹簧的 `dt` 于是不是 16.7 而是 20 毫秒，「到全开要几帧」就量不准了。

import (
	"image"
	"image/color"
	"testing"
	"time"

	mygoui "github.com/egoist/mygo/ui"
)

// miniCurtainFrame 是**一帧**量到的东西。
type miniCurtainFrame struct {
	// height 是这一帧画出来的卷帘高（像素）。
	height float32

	// window 是这一帧窗被改成的高度（像素）。
	window int
}

// miniCurtainFrames 推 n 帧，把每一帧的卷帘高和窗高记下来。
//
// ⚠️ **每一帧都是 16.7 毫秒**（60Hz）——见文件头那一段。
func miniCurtainFrames(t *testing.T, tt *mygoui.Tester, s *Shell, n int) []miniCurtainFrame {
	t.Helper()
	out := make([]miniCurtainFrame, 0, n)
	for range n {
		tt.Advance(time.Second / 60)
		tt.Frame()
		out = append(out, miniCurtainFrame{
			height: s.miniCurtain.height,
			window: s.miniCurtain.setHeight,
		})
	}
	return out
}

// miniCurtainBounces 报高度序列里的**回弹**：每一次「冲上去又缩回来」缩回
// 了多少像素。
//
// ⚠️ **判据是 0.5 像素**：比那更小的起伏是数值噪声（弹簧的停止条件就是
// `|v| ≤ 0.06`，而 0.06 × 16.7 毫秒 × 236 ≈ 2.4 像素——比那更小的抖动确实
// 存在）。
func miniCurtainBounces(heights []float32) []float32 {
	var out []float32
	if len(heights) == 0 {
		return out
	}
	peak, valley := heights[0], heights[0]
	up := true
	for _, h := range heights[1:] {
		switch {
		case up:
			if h > peak {
				peak = h
			} else if peak-h > 0.5 {
				// 冲上去了又缩回来 ⇒ 记一次回弹。
				out = append(out, peak-h)
				valley = h
				up = false
			}
		default:
			if h < valley {
				valley = h
			} else if h-valley > 0.5 {
				peak = h
				up = true
			}
		}
	}
	return out
}

// miniCurtainPeakFrame 报高度序列里**第一个峰**在第几帧（0 是第一帧）。
//
// 序列只有一个元素时没有可比较的峰；最后一个元素也不能拿 `i+1` 比较。
func miniCurtainPeakFrame(heights []float32) int {
	if len(heights) < 3 {
		return -1
	}
	for i := 1; i < len(heights)-1; i++ {
		if heights[i] >= heights[i-1] && heights[i] > heights[i+1] {
			return i
		}
	}
	return -1
}

func TestMiniCurtainPeakFrameDoesNotReadPastTheSequence(t *testing.T) {
	for _, heights := range [][]float32{{}, {1}, {1, 2}, {1, 2, 1}} {
		got := miniCurtainPeakFrame(heights)
		if len(heights) == 3 {
			if got != 1 {
				t.Errorf("heights=%v: peak=%d, want 1", heights, got)
			}
			continue
		}
		if got != -1 {
			t.Errorf("heights=%v: peak=%d, want -1", heights, got)
		}
	}
}

// TestTheMiniCurtainUnrollsWithASpring 盯「展开是弹簧」（票 #307）。
//
// ⚠️ **它量的是「放下去到全开要几帧」**：时间步固定为 `time.Second/60`，峰值帧
// 直接对应真实 60Hz 的时间量纲；Issue 指定 `w = 10.5` 的首峰应在第 13~14 帧。
//
// ⚠️ **回弹要数出「过冲几次、每次多高」**：这一条把每一帧的高度记下来，
// 数出「冲上去又缩回来」了几次、每次缩回多少像素——**幅度要递减**（票面：
// 「回弹两三下、幅度递减」）。
func TestTheMiniCurtainUnrollsWithASpring(t *testing.T) {
	s, a := miniCurtainShell(t)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle()
	a.PutLyricsFrom("", "[00:00.00]第一句\n[00:05.00]第二句\n", "")
	tt := miniCurtainTester(s)
	tt.Frame()

	clickCurtainCover(t, tt)
	if !s.miniCurtain.open {
		t.Fatalf("点封面之后卷帘没有展开")
	}

	frames := miniCurtainFrames(t, tt, s, 60)
	heights := make([]float32, len(frames))
	for i, f := range frames {
		heights[i] = f.height
	}

	// 🔴 **窗高全程 ≤ 84 + 236**，而**每一帧窗高 == 内容高**。
	//
	// ⚠️ **这两条是一件事的两头**：这一扇窗是透明的，而**透明的那一块照样
	// 吃点击**（`minicurtain.go` 文件头第 1 条）——窗比内容高 20 像素，底下
	// 那个应用的按钮就点不到那 20 像素。
	//
	// ⚠️ **「过冲那一帧」是这一条最要紧的一帧**：弹簧内部的 `p` 会超过 1，
	// 而画出来的高度被**镜像**翻成「缩回去一点」（见 `minicurtain_spring.go`
	// 的文件头）——所以窗高**从不超过** 84 + 236。
	for i, f := range frames {
		if f.window > miniHeight+miniCurtainH {
			t.Errorf("第 %d 帧窗高是 %d，want ≤ %d（84 + %d）："+
				"过冲那一帧窗比内容高，而透明的那一块照样吃点击",
				i, f.window, miniHeight+miniCurtainH, miniCurtainH)
		}
		want := miniHeight + int(f.height+0.5)
		if f.window != want {
			t.Errorf("第 %d 帧窗高是 %d，want %d（84 + 这一帧画出来的卷帘高 %.1f）："+
				"窗高必须每一帧都等于内容高", i, f.window, want, f.height)
		}
	}

	// 🔴 **回弹**：至少两下，幅度递减。
	bounces := miniCurtainBounces(heights)
	if len(bounces) < 2 {
		t.Errorf("展开只回弹了 %d 下（%v），want ≥ 2：票面要的是"+
			"「到底后回弹两三下、幅度递减」", len(bounces), bounces)
	}
	for i := 1; i < len(bounces); i++ {
		if bounces[i] >= bounces[i-1] {
			t.Errorf("回弹幅度没有递减：%v（第 %d 下 %.1f 像素 ≥ 第 %d 下 %.1f 像素）",
				bounces, i, bounces[i], i-1, bounces[i-1])
		}
	}
	if len(bounces) > 0 && bounces[0] < 5 {
		t.Errorf("头一下回弹只有 %.1f 像素（%v）：那一下得**看得出来**"+
			"（票面：「回弹两三下、幅度递减」）", bounces[0], bounces)
	}

	// 🔴 **放下去到全开**：Issue 指定 `w = 10.5`，第一个峰在第 13~14 帧。
	peak := miniCurtainPeakFrame(heights)
	if peak < 0 {
		t.Fatalf("高度序列里没有峰：%v", heights)
	}
	if peak < 13 || peak > 14 {
		t.Errorf("第一个峰在第 %d 帧（60fps 下 %.0fms），want 第 13~14 帧（约 217~233ms，Issue 指定 w = 10.5）：%v",
			peak, float64(peak+1)*float64(time.Second/60)/float64(time.Millisecond), heights)
	}
	// 峰上头要够高（≥ 90% 的目标高）——不然那不叫「放下去」。
	if heights[peak] < miniCurtainH*0.9 {
		t.Errorf("峰上只有 %.1f 像素（want ≥ %.0f）：那不叫「放下去」",
			heights[peak], miniCurtainH*0.9)
	}
}

// TestTheMiniCurtainRollsUpWithoutBouncing 盯「收起不弹」（票 #307）。
//
// ⚠️ **判据是「一路缩，没有一次涨回来」**：收起那一段是**过阻尼**（ζ = 1.1）
// ⇒ 高度单调下降，中间**没有过冲**（过冲就是「涨回来」）。
func TestTheMiniCurtainRollsUpWithoutBouncing(t *testing.T) {
	s, a := miniCurtainShell(t)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle()
	a.PutLyricsFrom("", "[00:00.00]第一句\n[00:05.00]第二句\n", "")
	tt := miniCurtainTester(s)
	tt.Frame()

	clickCurtainCover(t, tt)
	settleCurtain(t, tt, s, true)
	if !s.miniCurtain.open {
		t.Fatalf("起点就不对：卷帘没有展开")
	}

	s.miniCurtainCollapse(tt.Now())
	// ⚠️ **先画一帧**：弹簧是在**画视图**的时候推的，而 `tt.Advance` 只在
	// 「有人要帧」的时候才跑帧——刚收起的时候还没有人要帧（那一下是测试直接
	// 调的，不是点出来的）。
	tt.Frame()
	frames := miniCurtainFrames(t, tt, s, 60)

	// 🔴 **收起真的是「滑下去」而不是「一帧消失」**。
	//
	// ⚠️ **这一条是踩过坑才写下来的**：那一版的收起**一帧就没了**——因为
	// 「画出来的高度 = 比例 × 全开高」里的**全开高**取成了
	// `miniCurtainTargetHeight()`（它在收着的时候是 84 ⇒ 基数是 0），
	// 而**收起的一下 `open` 就变成 false 了**。⇒ **高度 = 比例 × 0 = 0**，
	// 窗于是**一帧就回到 84**。
	//
	// ⇒ 而**下面那个「一路缩」的判据**在那种情况下**照样通过**（全是 0，
	// 单调、不涨）——**判据太松，测不出这一条**。所以这里要断的是
	// 「**中间那一段是真的在 0 和全开之间的**」。
	middle := false
	for _, f := range frames {
		if f.height > 1 && f.height < miniCurtainH-1 {
			middle = true
			break
		}
	}
	if !middle {
		t.Errorf("收起的过程里**没有一帧**落在 1 和 %d 之间（拿到的高度：%v）："+
			"那是「一帧消失」，不是「像手卷上去一样」",
			miniCurtainH-1, miniCurtainHeights(frames))
	}

	for i := 1; i < len(frames); i++ {
		if frames[i].height > frames[i-1].height+0.01 {
			t.Errorf("第 %d 帧卷帘高 %.2f，比上一帧（%.2f）**涨回来了**："+
				"收起那一段是过阻尼（ζ = 1.1），不该有回弹",
				i, frames[i].height, frames[i-1].height)
		}
	}
	if got := frames[len(frames)-1].height; got > 0.01 {
		t.Errorf("收起之后卷帘高是 %.2f，want 0", got)
	}
	if got, want := frames[len(frames)-1].window, miniHeight; got != want {
		t.Errorf("收起之后窗高是 %d，want %d", got, want)
	}
	// ⚠️ **窗高全程不许超过 84 + 236**：收起那一段是从满高往回走的，而过冲
	// （如果有过冲）会是**先涨**——那正是「窗比内容高」那一条。
	for i, f := range frames {
		if f.window > miniHeight+miniCurtainH {
			t.Errorf("第 %d 帧窗高是 %d，want ≤ %d", i, f.window, miniHeight+miniCurtainH)
		}
	}
}

// miniCurtainHeights 把一串帧的高度列出来（报错的时候要看得见）。
func miniCurtainHeights(frames []miniCurtainFrame) []float32 {
	out := make([]float32, len(frames))
	for i, f := range frames {
		out[i] = f.height
	}
	return out
}

// TestTheMiniCurtainRetriesTheActivation 盯「激活应用那一步失败了就再试」
// （票 #307 修 F1）。
//
// ⚠️ **它量的是「该不该再试」那个判据**（`miniCurtainFocusDue`）：真机上
// 「应用有没有真的被激活」要一个真窗口 + 一个在前台的应用（那是**真机那一
// 半**，记在 `miniplayer_test.go` 末尾「真窗口才量得到」里），而无头这一档
// 量得到的是**判据本身**。
//
// ⚠️ **判错的后果是两种**：**收手太晚** ⇒ 把用户从他自己那件事上拽走
// （还在重试 `activateIgnoringOtherApps:`）；**收手太早** ⇒ 一次都没试，
// Esc 到不了这一扇窗（F1 那个症状）。⇒ 四个条件一个一个断。
func TestTheMiniCurtainRetriesTheActivation(t *testing.T) {
	now := time.Now()
	// 「点封面那一刻应用**不是**前台」的那一份状态。
	f := miniCurtainFocusState{
		need:  true,
		until: now.Add(miniCurtainFocusWindow),
		next:  now.Add(miniCurtainFocusDelay),
	}

	// 该试：什么都还没发生（需要激活、还没激活过、窗口没过、时刻到了、要键盘）。
	if !miniCurtainFocusDue(f.next, true, f) {
		t.Errorf("「该试」的那些条件都满足了，却判成「不用试」")
	}
	// 不该试：已经激活过了（`OnDidBecomeActive` 到了）。
	fdone := f
	fdone.done = true
	if miniCurtainFocusDue(f.next, true, fdone) {
		t.Errorf("已经激活过了，还判成「要重试」：那会把用户从他自己那件事上拽走")
	}
	// 不该试：压根不需要激活（点封面那一刻应用**已经是**前台）。
	fno := f
	fno.need = false
	if miniCurtainFocusDue(f.next, true, fno) {
		t.Errorf("点封面那一刻应用已经是前台了，还判成「要重试」：" +
			"用户点别的应用之后我们会把用户拽回来")
	}
	// 不该试：窗口过了。
	if miniCurtainFocusDue(f.until.Add(time.Millisecond), true, f) {
		t.Errorf("重试的窗口已经过了，还判成「要重试」")
	}
	// 不该试：还没到下一次的时刻。
	if miniCurtainFocusDue(f.next.Add(-time.Millisecond), true, f) {
		t.Errorf("还没到下一次重试的时刻，还判成「要重试」")
	}
	// 不该试：那一扇已经不要键盘了（卷帘收起来了）。
	if miniCurtainFocusDue(f.next, false, f) {
		t.Errorf("那一扇已经不要键盘了，还判成「要重试」")
	}

	// 🔴 **重试的那两档之间要隔开**：`activateIgnoringOtherApps:` 是**异步**
	// 的（16~50ms 才落地），所以两次重试之间不能比那一下还短——不然第二次
	// 会撞在第一次还没落地的那一次上。
	if miniCurtainFocusDelay <= 50*time.Millisecond {
		t.Errorf("两次重试之间隔 %v，want > 50ms：激活那一句是异步的（16~50ms 才"+
			"落地），下一次重试不能撞在它还没落地的那一次上", miniCurtainFocusDelay)
	}
	// 🔴 **重试的窗口要够长**（十次机会），又要**短到不会在用户走开之后还把
	// 应用拽回前台**。
	if tries := miniCurtainFocusWindow / miniCurtainFocusDelay; tries < 5 {
		t.Errorf("重试窗口 %v ÷ 间隔 %v = %d 次机会，want ≥ 5", miniCurtainFocusWindow, miniCurtainFocusDelay, tries)
	}
	if miniCurtainFocusWindow > 2*time.Second {
		t.Errorf("重试窗口 %v 太长：用户已经走开之后还会把应用拽回前台", miniCurtainFocusWindow)
	}
}

// TestTheMiniCurtainReverseNeverGrows 盯「展开到一半就收起」不许先长高
// （票 #307 修评审第 2 轮那条 major）。
//
// 🔴 **它量的是生产的那一个弹簧**（`miniCurtainSpring`，不是测试自己写的
// 一份）：评审的探针就是直接驱动它——60fps，第 6 帧换目标，然后看着高度。
//
// ⚠️ **那一版会先长高约 7.9 像素再缩回去**（149.674 → 157.554）：因为换
// 目标的时候**速度接着**（参考页那一套），而展开那一段的速度是**向上**的。
// ⇒ 票面 §② 明令不许出现「窗先变大再缩回去」那种中间态（那一帧窗比内容
// 高，透明的那一块在吃点击）。
//
// ⇒ **换目标 ⇒ 速度归零**（`miniCurtainSpring.start`）。
func TestTheMiniCurtainReverseNeverGrows(t *testing.T) {
	sp := &miniCurtainSpring{}
	t0 := time.Now()
	sp.start(1, t0)

	// 🔴 **先放开 6 帧**（60fps ⇒ 100 毫秒）：评审的探针就是在这个时刻换
	// 目标的——那一刻卷帘正长到一半多。
	//
	// ⚠️ **推的是 60fps 的整数帧**（`time.Second/60`）：无头那一档的刷新
	// 就是 60Hz，而弹簧是**按时间积分**的——推 20 毫秒会让 `dt` 与真实帧
	// 不一致。
	const fps = 60
	frame := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Second / fps) }
	height := func(n int) float64 { return sp.step(frame(n)) }

	var prev float64
	for i := 1; i <= 6; i++ {
		prev = height(i)
	}
	if prev <= 0 || prev >= 1 {
		t.Fatalf("换目标那一刻的高度是 %.3f（want 0~1 之间）："+
			"起点就不对（展开到一半才换目标）", prev)
	}

	// 🔴 **换目标：收起**。
	sp.start(0, frame(6))

	// 🔴 **此后每一帧都不许比换目标那一刻高**，而且**一路往下**。
	//
	// ⚠️ **判据是「不许涨」**（不是「不许超过目标」）：票面 §② 要的是「不许
	// 再出现『窗先变大再缩回去』这种中间态」——而这一段的高度是**从换目标
	// 那一刻的高度**开始的（不突然跳一下）。
	limit := prev
	cur := limit
	for i := 7; i <= 60; i++ {
		h := height(i)
		if h > limit+1e-6 {
			t.Errorf("第 %d 帧高度 %.3f，比换目标那一刻（%.3f）还高："+
				"「展开到一半就收起」先长高了 %.3f ——票面 §② 不许出现"+
				"「窗先变大再缩回去」", i, h, limit, h-limit)
		}
		if h > cur+1e-6 {
			t.Errorf("第 %d 帧高度 %.3f，比上一帧（%.3f）**涨回来了**："+
				"收起那一段不许有回弹", i, h, cur)
		}
		cur = h
	}
	if got := sp.pos; got > 0.001 {
		t.Errorf("收起之后高度是 %.4f，want 0", got)
	}
}

// TestTheMiniCurtainKeepsAFrameWhileTheActivationRetryIsOpen 盯「激活应用那个
// 重试窗口开着的时候，迷你窗还要得出帧」（票 #307 修评审第 2 轮那条
// major）。
//
// 🔴 **为什么这一条是必须的**：迷你窗是**失焦照画**的，而**帧是「谁要」才
// 出的**——弹簧停稳之后就没有人要帧了，于是那个 1200ms 的重试窗口**还剩
// 大半**，而后面那几次重试**一次都没跑**（F1 那个症状还在）。
//
// ⚠️ **无头这一档正好量得到**：无头 Tester 里**压根没有应用**，于是
// `OnDidBecomeActive` 那一档**永远不触发**——而那正是「激活成功了就收手」
// 的那个信号。⇒ 重试窗口在无头这一档**全程开着**，这一条量得到「窗口开着
// 的时候还有没有帧源」。
func TestTheMiniCurtainKeepsAFrameWhileTheActivationRetryIsOpen(t *testing.T) {
	s, a := miniCurtainShell(t)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle()
	a.PutLyricsFrom("", "[00:00.00]第一句\n[00:05.00]第二句\n", "")
	tt := miniCurtainTester(s)
	tt.Frame()

	clickCurtainCover(t, tt)
	settleCurtain(t, tt, s, true)

	// 🔴 **弹簧停稳之后**：重试窗口还开着 ⇒ **必须还要帧**。
	//
	// ⚠️ **`settleCurtain` 那个判据是「弹簧停稳了」**（不是「没有帧要
	// 出」）——见那个函数的注释。
	if s.miniCurtain.spring.moving {
		t.Fatalf("弹簧还没停稳就往下走了")
	}
	if !tt.FramePending() {
		t.Errorf("弹簧停稳之后没有帧要出了，而「激活应用」那个重试窗口"+
			"还开着（还剩 %v）：后面那几次重试**一次都不会跑**"+
			"（迷你窗是失焦照画的，帧是「谁要」才出的）",
			time.Until(s.miniCurtainFocus.until))
	}

	// 🔴 **窗口过了之后就不该再要帧**：不然迷你窗会**一直**重画下去
	//（失焦照画的每一帧都是一次整窗重建）。
	tt.Advance(miniCurtainFocusWindow + 100*time.Millisecond)
	tt.Frame()
	if tt.FramePending() {
		t.Errorf("重试窗口过了之后还在要帧：迷你窗会一直重画下去")
	}
}

// miniCurtainLum 报一个颜色的亮度。
func miniCurtainLum(c color.RGBA) float64 {
	return 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
}

// miniCurtainAxisRows 报画面底部那一段「轴」占着几行（从下往上数）。
//
// ⚠️ **判据是「和纸身差得够多」**：那一段是从画面最底下往上数、直到某一行的
// 颜色和纸身（画面中间那一行）差得不多为止。⇒ 量的是**轴那一段的行数**，
// 而**轴的下沿永远是画面最底下那一行**。
func miniCurtainAxisRows(img *image.RGBA) int {
	b := img.Bounds()
	x := b.Max.X - 20
	paper := miniCurtainLum(img.RGBAAt(x, b.Max.Y/2))
	rows := 0
	for y := b.Max.Y - 1; y >= b.Min.Y; y-- {
		if d := miniCurtainLum(img.RGBAAt(x, y)) - paper; d > 12 || d < -12 {
			rows++
			continue
		}
		break
	}
	return rows
}

// TestTheMiniCurtainHasTwoAxes 盯「两道轴」（票 #307）。
//
// ⚠️ **判据是画面**：轴是**看得见**的东西，而无头 Tester 量得到画面上的
// 像素（`tt.Image()`）。⇒ 这一条量的是「**画面最底下那一行是轴**」和
// 「**画面最顶上那几行是轴**」。
//
// ⚠️ **而且要跟着下沿走**：无头 Tester 的窗高是自己那个（不跟真窗口走），
// 所以这一条**自己改 Tester 的尺寸**（`tt.SetSize`）来模拟「窗在长高」——
// 改完之后**轴仍然贴在最底下那一行**（票面：「下沿到哪轴到哪」）。
func TestTheMiniCurtainHasTwoAxes(t *testing.T) {
	s, a := miniCurtainShell(t)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle()
	a.PutLyricsFrom("", "[00:00.00]第一句\n[00:05.00]第二句\n", "")
	tt := miniCurtainTester(s)
	tt.Frame()

	// 🔴 **上轴**：迷你窗顶边那一道（一直都在，收着的时候也在）。
	//
	// ⚠️ **量的是「顶上那几行和纸身不一样」**：上轴只有 6 高（封面从 y=7
	// 起，再高就压到封面了——见 `minicurtain_axis.go` 那个常量）。
	top := miniCurtainLum(tt.Image().RGBAAt(160, 5))
	paper := miniCurtainLum(tt.Image().RGBAAt(160, 12))
	if d := top - paper; d > -8 && d < 8 {
		t.Errorf("迷你窗顶边那一行（y=5）的亮度是 %.0f，而下面纸身（y=12）是 %.0f："+
			"**上轴不在顶边**（票面：「上轴 = 迷你窗顶边那一道」）", top, paper)
	}
	// ⚠️ **而顶边下面那一行是纸身**：上轴只有 6 高（y=0..5）。
	if d := miniCurtainLum(tt.Image().RGBAAt(160, 6)) - paper; d > 4 || d < -4 {
		t.Errorf("y=6 那一行的亮度是 %.0f，而纸身是 %.0f：上轴比 6 高了"+
			"（它会压到封面的第一行）", miniCurtainLum(tt.Image().RGBAAt(160, 6)), paper)
	}

	// 展开。
	clickCurtainCover(t, tt)
	settleCurtain(t, tt, s, true)

	// 🔴 **下轴**：贴着卷帘下沿那一道，**画面最底下那一行就是它**。
	img := tt.Image()
	rows := miniCurtainAxisRows(img)
	if rows < 8 {
		t.Errorf("画面最底下那一段只有 %d 行是轴，want ≥ 8：下轴（%d 高）"+
			"没画出来，或者没贴在窗的下沿", rows, miniCurtainAxisH)
	}
	if rows > miniCurtainAxisH {
		t.Errorf("画面最底下那一段有 %d 行是轴，want ≤ %d：下轴是 %d 高",
			rows, miniCurtainAxisH, miniCurtainAxisH)
	}

	// 🔴 **跟着下沿走**：把窗改矮（模拟真窗在长高 / 变矮），轴仍然贴在最底下。
	//
	// ⚠️ **这一条是「下沿到哪轴到哪」**：轴是**根**的绝对定位孩子（不是卷帘
	// 那一段的），而根的高度就是窗的高度——理由见 `minicurtain_axis.go` 的
	// 文件头。
	for _, h := range []int{miniHeight + 100, miniHeight + 200, miniHeight + miniCurtainH} {
		tt.SetSize(miniWidth, h)
		tt.Frame()
		if got := miniCurtainAxisRows(tt.Image()); got < 8 {
			t.Errorf("窗改成 %d 高之后，画面最底下那一段只有 %d 行是轴："+
				"下轴没有跟着下沿走", h, got)
		}
	}
}
