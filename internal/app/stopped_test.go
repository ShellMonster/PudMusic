package app_test

// 「停住了」在快照里的那两栏（票 #319）。
//
// # 为什么是**两栏**而不是一栏
//
// 界面上要分的是**两件不同的事**：
//
//	用户按了停止 —— 这一首到此为止，**队列里还有歌**
//	顺序播放走到队尾 —— **没有下一首了**
//
// 而它们在播放器那一边落在**同一档**上（进度回到开头、不在放）。所以那一档
// 单独拿出来是分不出这两件事的——而分不出的话，界面上就只有一句「已停止」，
// 于是用户按了停止也被告知「这一串放完了」。
//
// 这两栏是 `State.Stopped`（「停住了」）和 `State.QueueFinished`（「这一串放
// 完了」），判据见 `App.Snapshot` 里那一行。

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestPausingIsNotStopping 盯**暂停不是停止**。
//
// 这是这一票最要紧的那一半：暂停停在原地、停止回到开头，而界面上原来分不出
// 这两件事（进度条那一格是唯一能看出差别的地方，而它在播放条上，不是状态栏）。
func TestPausingIsNotStopping(t *testing.T) {
	a := newApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}

	a.Pause()
	st := a.Snapshot()
	if st.Stopped {
		t.Error("暂停之后 Stopped 成了 true——暂停停在原地，而停止才回到开头")
	}
	if st.QueueFinished {
		t.Error("暂停之后 QueueFinished 成了 true——队列还剩好几首")
	}
}

// TestStoppingIsNotTheQueueRunningOut 盯**用户按了停止**不是「这一串放完了」。
//
// 这一条是那个区分的**要紧处**：两件事在播放器那一边是同一档，而用户按停止
// 的时候队列里通常还有歌——说「这一串放完了」是在骗他。
func TestStoppingIsNotTheQueueRunningOut(t *testing.T) {
	a := newApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}

	a.Stop()
	st := a.Snapshot()
	if !st.Stopped {
		t.Error("按了停止之后 Stopped 还是 false")
	}
	if st.QueueFinished {
		t.Error("按了停止之后 QueueFinished 成了 true——队列里还有歌，" +
			"说「这一串放完了」是在骗用户")
	}
}

// TestTheQueueRunningOutSaysSo 盯**顺序播放走到队尾**是「这一串放完了」。
//
// 素材是 0.25 秒的歌（`shorts`），两首——所以这一条是真的等它放完，不是摆一个
// 假状态。
//
// ⚠️ **别改 `Advance` 的语义**：Biu 上游就是 `audio.currentTime = 0;
// audio.pause()`（停在开头、不动队列指针），而这一票要的是**界面上把这件事
// 说出来**，不是改它停在哪。
func TestTheQueueRunningOutSaysSo(t *testing.T) {
	paths := shorts(t, 2)

	a := newApp(t)
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	a.SetPlayMode(app.PlayModeSequence)
	if err := a.PlayFrom(items(paths...)...); err != nil {
		t.Fatal(err)
	}

	// 等最后一首放完、停下来。
	waitFor(t, 6*time.Second, "最后一首放完之后停下来", func() bool {
		st := a.Snapshot()
		return st.Title != "" && !st.Playing && st.Stopped
	})
	st := a.Snapshot()
	if !st.Stopped {
		t.Errorf("队尾停下来之后 Stopped 是 false，want true（停在开头就是「停住了」）")
	}
	if !st.QueueFinished {
		t.Error("队尾停下来之后 QueueFinished 还是 false——" +
			"界面上分不出「这一串放完了」和「暂停在开头」")
	}
}

// TestPlayingAgainClearsTheStoppedFlag 盯**再按播放**把「停住了」这件事收掉。
//
// 停在开头之后用户按播放，那一首从头又放起来了——这时界面上不该还挂着
// 「已停止」（那是上一刻的事）。
func TestPlayingAgainClearsTheStoppedFlag(t *testing.T) {
	a := newApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}

	a.Stop()
	a.Play()
	waitFor(t, 5*time.Second, "重新放起来", func() bool { return a.Snapshot().Playing })
	st := a.Snapshot()
	if st.Stopped {
		t.Error("重新播放之后 Stopped 还是 true")
	}
	if st.QueueFinished {
		t.Error("重新播放之后 QueueFinished 还是 true")
	}
}

// TestScrubbingClearsTheStoppedFlag 盯**拖动进度条**把「停住了」收掉。
//
// 停在开头之后把进度条拖到中间，那一首接着放——这时界面上不该还挂着「已停止」。
func TestScrubbingClearsTheStoppedFlag(t *testing.T) {
	a := newApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}

	a.Stop()
	a.ScrubEnd(0.5)
	st := a.Snapshot()
	if st.Stopped {
		t.Error("拖了进度条之后 Stopped 还是 true——歌已经不在开头停着了")
	}
	if st.QueueFinished {
		t.Error("拖了进度条之后 QueueFinished 还是 true")
	}
}
