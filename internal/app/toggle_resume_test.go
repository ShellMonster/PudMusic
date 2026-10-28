package app_test

// 「按过一次暂停之后队列卡住」（票 #352）。
//
// # 现象
//
// 顺序播放两首短歌 → 在播放条上按一次暂停、再按一次恢复 → 第一首放完 →
// **队列不往下走**（还是第一首、进度停在末尾、不在放），而整窗画面上**一句话
// 都没有**——既不是「已停止」，也不是「这一串放完了」。
//
// 对照：不暂停就正常走完（队尾写「这一串放完了」）；走 `App.Pause()` /
// `App.Play()`（快捷键那条路）也正常。
//
// # 根因
//
// `wantPlay` 答的是「用户的意图（按过播放）」，而 `App.Toggle` 的**恢复**那一半
// 没把它记回 true——它一直 false。`onTrackFinished` 拿它当「要不要往下走」的
// 判据（那一行早退），于是队列停在那儿；队列不往下走，`QueueFinished` 就永远
// 是 false，「这一串放完了」也就永远不出现。
//
// ⚠️ **对照的那条路不许改坏**：`App.Pause()` / `App.Play()` 两个方向都记
// `wantPlay`，所以它一直是对的——下面有一条测试专门守着它。

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestTogglingPauseDoesNotStallTheQueue 盯**按一次暂停再恢复之后队列照常往下走**。
//
// ⚠️ **素材要短**：这一条真的在等它放完（0.25 秒一首），而四秒的素材会让这条
// 测试跑成一条慢测试。
func TestTogglingPauseDoesNotStallTheQueue(t *testing.T) {
	paths := shorts(t, 2)

	a := newApp(t)
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	a.SetPlayMode(app.PlayModeSequence)
	if err := a.PlayFrom(items(paths...)...); err != nil {
		t.Fatal(err)
	}
	if got := a.Snapshot().Title; got != "a" {
		t.Fatalf("起播的歌是 %q，want a", got)
	}

	// 按一次暂停（想听 → 不想听），再按一次恢复（不想听 → 想听）。
	a.Toggle()
	if st := a.Snapshot(); st.Playing {
		t.Fatal("按了一次之后还在 Playing——那是播放键，不是暂停键")
	}
	a.Toggle()

	// 第一首放完之后要接第二首。
	waitTitle(t, a, "b", "按过一次暂停再恢复之后，队列没有往下走")
}

// TestTheQueueFinishesAfterAToggleSaysSo 盯**这条路上「这一串放完了」也要出现**。
//
// 这是同一件事的另一半：队列往下走了还不算完——队尾那一句也得说出来（界面上
// 靠 `State.QueueFinished` 说，判据见 `App.Snapshot`）。
//
// ⚠️ **别只断「走到了队尾」**：那个组合（走到了队尾、但界面上一句话都没有）正是
// 修好之前的样子——那一条的另一半是 `QueueFinished`。
func TestTheQueueFinishesAfterAToggleSaysSo(t *testing.T) {
	paths := shorts(t, 2)

	a := newApp(t)
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	a.SetPlayMode(app.PlayModeSequence)
	if err := a.PlayFrom(items(paths...)...); err != nil {
		t.Fatal(err)
	}

	a.Toggle()
	a.Toggle()

	// 队列走完：停在开头、不在放，而且这一句要说出来。
	waitFor(t, 8*time.Second, "队列走完之后停下来", func() bool {
		st := a.Snapshot()
		return st.Title == "b" && !st.Playing && st.Stopped
	})
	st := a.Snapshot()
	if !st.QueueFinished {
		t.Error("队列走完了，QueueFinished 还是 false——" +
			"界面上不会说「这一串放完了」，用户只看到卡住")
	}
}

// TestPauseAndPlayStillMoveTheQueueOn 盯**快捷键那条路（`App.Pause` / `App.Play`）
// 行为不变**。
//
// 这一条是**对照组**：它走的是另一条路（两个方向都记 `wantPlay`），修 `Toggle`
// 不许把它改坏——症状会一模一样（队列不往下走），而用户分不出是哪条路坏了。
func TestPauseAndPlayStillMoveTheQueueOn(t *testing.T) {
	paths := shorts(t, 2)

	a := newApp(t)
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	a.SetPlayMode(app.PlayModeSequence)
	if err := a.PlayFrom(items(paths...)...); err != nil {
		t.Fatal(err)
	}
	if got := a.Snapshot().Title; got != "a" {
		t.Fatalf("起播的歌是 %q，want a", got)
	}

	a.Pause()
	if st := a.Snapshot(); st.Playing {
		t.Fatal("按了暂停之后还在 Playing")
	}
	a.Play()

	waitTitle(t, a, "b", "走 App.Pause()/App.Play() 那条路，队列没有往下走")
}

// TestAPausedSongStaysPutAfterAToggle 盯**「按了暂停就别往下走」这件事没被改坏**。
//
// ⚠️ **和上面那一条是一对**：恢复播放之后队列要往下走，而**还停着的时候**就不
// 该往下走。只钉前半条的话，把 `onTrackFinished` 的早退条件整个删掉也照样绿——
// 而那正是「用户按了暂停，下一首歌自己放了起来」（`onTrackFinished` 顶上那段
// 注释记的就是它）。
func TestAPausedSongStaysPutAfterAToggle(t *testing.T) {
	paths := shorts(t, 3)

	a := newApp(t)
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	a.SetPlayMode(app.PlayModeLoop)
	if err := a.PlayFrom(items(paths...)...); err != nil {
		t.Fatal(err)
	}

	a.Toggle()
	// 一首 0.25 秒的歌：静置一秒多，早就够放完两次了。
	time.Sleep(1200 * time.Millisecond)
	if got := a.Snapshot(); got.Title != "a" || got.Playing {
		t.Errorf("暂停之后播放条变成了 %q/Playing=%v，want 还是 a 且没在放", got.Title, got.Playing)
	}
}
