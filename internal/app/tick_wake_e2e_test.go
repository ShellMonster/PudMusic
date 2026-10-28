package app_test

// 这一条是「放歌的时候界面不该每 50ms 重建一次」的**端到端**证据。
//
// # 它量的是什么
//
// 真的开一首本地 FLAC 在放，真的跑 1.2 秒，数**唤醒回调被调了几次**：
//
//	改之前：每一拍叫一次（20Hz）→ 1.2 秒里二十几次
//	改之后：只在进度条那个**读数**（秒数）变的时候叫 → 1 秒 1 次上下
//
// 唤醒回调就是 `App.SetWake` / `AddWake` 装进去的那一个——生产里它接的是
// `Shell` 的 `wake.invalidate`，最终走 `Services.Invalidate()` 让框架重画一帧。
// 所以「叫了几次」= 「界面被请求重画几次」。
//
// # 为什么不拿「帧数」当判据
//
// 帧数还要经过框架（合并、可见性、局部重绘），那是另一层的事；这一条要钉的
// 是**应用这一层有没有把播放器的时钟直接接成界面的时钟**。两者分开测，红了
// 才知道是哪一层。
//
// # 为什么允许 4 次而不是 1 次
//
//   - 秒边界落在 50ms 网格之间，一秒里跨过 1~2 个读数；
//   - 起播那一下有一串状态变化（加载完、开始放）各自会叫一次。
//
// 上限 4 远低于 20，拦得住「有人把唤醒接回每一拍」，又不至于因为起播噪声红。

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestAPlayingSongWakesTheViewOnceASecondNotTwentyTimes 钉「界面跟着读数走」。
func TestAPlayingSongWakesTheViewOnceASecondNotTwentyTimes(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	if err := a.Open(flac(t)); err != nil {
		t.Fatalf("开不了测试素材：%v", err)
	}
	// 等它真的在放（音频线程起来、位置过了零点几秒）。
	waitFor(t, 10*time.Second, "这首歌开始往前走", func() bool {
		return a.Snapshot().Position > 300*time.Millisecond
	})

	// 从这里开始数：**先清一次**，把起播那一串状态变化的唤醒排除掉。
	var wakes atomic.Int32
	a.SetWake(func() { wakes.Add(1) })
	time.Sleep(30 * time.Millisecond) // 让上面那次 SetWake 自己的 publish 落下去
	wakes.Store(0)

	// 量 1.2 秒：跨过一个或两个秒边界。
	const window = 1200 * time.Millisecond
	time.Sleep(window)
	if !a.Snapshot().Playing {
		t.Skip("这首歌在这台机器上没放起来（没有音频设备？），这一条量不到")
	}

	// 1.2 秒里最多 4 次：两个秒边界 + 起播/状态噪声的余量。
	//
	// ⚠️ **上界写 4、不是 2**：`tick` 是 50ms 网格，跨秒那一下可能被两次采样
	// 各判一次；而这条测试要拦的是「20Hz」，不是「精确的 1Hz」。
	if got := wakes.Load(); got > 4 {
		t.Errorf("1.2 秒里界面被叫醒 %d 次，want ≤ 4——"+
			"这是把播放器的 50ms 时钟直接接成了界面的刷新时钟"+
			"（改之前是 20Hz，二十几次）；进度条的读数一秒才变一次，"+
			"那根线自己的连续感由滑块局部重绘负责（`p.AnimationFrame()`）", got)
	}
	// 也不能一次都不叫：那样进度条就定住了。
	if got := wakes.Load(); got == 0 {
		t.Error("1.2 秒里界面一次都没被叫醒——进度条的读数不会更新，" +
			"用户看到的是卡住的进度")
	}
}

// TestAPausedSongWakesTheViewAtMostOnce 是上一条的反面：暂停之后位置不动，
// 界面就不该再被叫醒。
//
// 这一档以前是**白刷**：`live` 在暂停之后是假，所以本来就不叫——但那是
// 「碰巧对」。这条把它钉成契约：暂停 = 位置不动 = 读数不变 = 不要帧。
func TestAPausedSongWakesTheViewAtMostOnce(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	if err := a.Open(flac(t)); err != nil {
		t.Fatalf("开不了测试素材：%v", err)
	}
	waitFor(t, 10*time.Second, "这首歌开始往前走", func() bool {
		return a.Snapshot().Position > 200*time.Millisecond
	})
	a.Pause()

	var wakes atomic.Int32
	a.SetWake(func() { wakes.Add(1) })
	time.Sleep(30 * time.Millisecond)
	wakes.Store(0)

	time.Sleep(500 * time.Millisecond)
	if got := wakes.Load(); got > 1 {
		t.Errorf("暂停之后半秒里界面被叫醒 %d 次，want ≤ 1——"+
			"位置不动，读数就不变，那几帧全是白烧的电", got)
	}
}
