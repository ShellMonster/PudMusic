package app_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// 「断点续播」的验收（spec docs/specs：退出时记住播到哪儿，下次打开接着放）。
//
// 和 `playback` 那一格（停在原位、**暂停**）是两件事：这一格记的是「上次
// 退出时在放哪一首、放到哪儿了」，下次启动**接着放**（往回退几秒接上下文）。

// resumeCopies 是「断点续播」的素材。
//
// ⚠️ **仓库里最长的素材也只有 4 秒**（stereo16/24 都是 4 秒），装不下
// 「拖进 5 秒、退 3 秒还剩 2 秒」那一段——所以主验收那条把 `positionMs`
// 手写成 5000（见 TestStartupResumeSeeksBackThreeSecondsAndPlays 的注释）。
// 素材够长是「真的放起来、真的出声」那一边；位置那一半靠手写设置。
func resumeCopies(t *testing.T, n int) []string {
	t.Helper()
	src := filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac")
	b, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	dir := t.TempDir()
	out := make([]string, n)
	for i := range n {
		out[i] = filepath.Join(dir, string(rune('a'+i))+".flac")
		if err := os.WriteFile(out[i], b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// resumeStored 读设置文件里 `playbackResume` 那一格（文件没落过就返回零值）。
func resumeStored(t *testing.T, path string) app.PlaybackResume {
	t.Helper()
	s, err := resumeStoredRaw(path)
	if err != nil {
		t.Fatalf("设置文件读不出来：%v", err)
	}
	return s
}

// resumeStoredRaw 和 resumeStored 同一个读法，但读不到的错一起给回来：
// 「不该写」的断言要分得清「文件还没落盘」和「文件里没有那一格」。
func resumeStoredRaw(path string) (app.PlaybackResume, error) {
	s, err := app.LoadSettings(path)
	if err != nil {
		return app.PlaybackResume{}, err
	}
	return s.PlaybackResume, nil
}

// TestTrackSwitchWritesThePreviousTrackToPlaybackResume 钉住「切歌那一刻」：
// 位置清零之前，上一首的 key 和位置要落进 `playbackResume`。
func TestTrackSwitchWritesThePreviousTrackToPlaybackResume(t *testing.T) {
	paths := resumeCopies(t, 2)
	a, path := playbackMount(t)

	if err := a.PlayFrom(items(paths[0], paths[1])...); err != nil {
		t.Fatal(err)
	}
	a.ScrubEnd(0.5)
	a.Toggle() // 暂停：位置才停得住，断言才不含糊
	at := a.Snapshot().Position
	if at <= 0 {
		t.Fatalf("拖完之后位置是 %v，want > 0", at)
	}

	if err := a.PlayAt(1); err != nil {
		t.Fatal(err)
	}
	if err := a.FlushSettings(); err != nil {
		t.Fatal(err)
	}

	r := resumeStored(t, path)
	wantKey := "local:" + paths[0]
	if r.TrackKey != wantKey {
		t.Errorf("切歌之后记的 key 是 %q，want %q（上一首）", r.TrackKey, wantKey)
	}
	// 记的是上一首停下的位置（毫秒），不是清零之后的 0。
	if r.PositionMs <= 0 || r.PositionMs > at.Milliseconds()+1000 {
		t.Errorf("切歌之后记的位置是 %d ms，want 约 %d ms", r.PositionMs, at.Milliseconds())
	}
}

// TestClosingWritesPlaybackResume 钉住「退出前」那一处：听了一会儿直接退出，
// `playbackResume` 里写的是最后一首和它的位置。
func TestClosingWritesPlaybackResume(t *testing.T) {
	paths := resumeCopies(t, 1)
	a, path := playbackMount(t)

	if err := a.PlayFrom(items(paths[0])...); err != nil {
		t.Fatal(err)
	}
	a.ScrubEnd(0.5)
	a.Toggle() // 暂停
	at := a.Snapshot().Position
	if at <= 0 {
		t.Fatalf("位置是 %v，want > 0", at)
	}

	// 退出那一趟（和界面关窗口时做的同一串，见 playbackRestart）。
	a.Close()
	if err := a.CloseSettings(); err != nil {
		t.Fatal(err)
	}

	r := resumeStored(t, path)
	if want := "local:" + paths[0]; r.TrackKey != want {
		t.Errorf("退出之后记的 key 是 %q，want %q", r.TrackKey, want)
	}
	if r.PositionMs <= 0 || r.PositionMs > at.Milliseconds()+1000 {
		t.Errorf("退出之后记的位置是 %d ms，want 约 %d ms", r.PositionMs, at.Milliseconds())
	}
}

// TestPlaybackResumeIsNotWrittenOnEveryTick 钉住「不每拍写」：
// 光放着不动（位置自己往前走）不许把 `playbackResume` 那一格写进设置文件。
//
// 50ms 一次的写盘是纯浪费（见 spec），而设置存储的防抖窗口被一直顶掉的后果
// 更糟：暂停期间用户改的任何设置都不落盘。
func TestPlaybackResumeIsNotWrittenOnEveryTick(t *testing.T) {
	paths := resumeCopies(t, 1)
	a, path := playbackMount(t)

	if err := a.PlayFrom(items(paths[0])...); err != nil {
		t.Fatal(err)
	}
	// 放几拍（位置自己往前走），不许写。
	time.Sleep(300 * time.Millisecond)
	if err := a.FlushSettings(); err != nil {
		t.Fatal(err)
	}
	if r, err := resumeStoredRaw(path); err != nil {
		t.Fatal(err)
	} else if r.TrackKey != "" {
		t.Fatalf("只放着不动也写了 playbackResume：%+v", r)
	}
}

// resumeWriteSettings 把「上次在放哪一首、放到哪儿了」手写成设置文件里那一格。
//
// ⚠️ **它走的是真设置那条路**（先写盘、再挂上去），和 `withSettings` /
// `playbackRestart` 是同一件事——不是往界面上硬塞一个值。
func resumeWriteSettings(t *testing.T, path string, items []app.PlaybackEntry, current string, positionMs int64) {
	t.Helper()
	store, err := app.OpenSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s := app.DefaultSettings("")
	s.Playback = app.PlaybackSnapshot{Items: items, Current: current}
	s.PlaybackResume = app.PlaybackResume{TrackKey: current, PositionMs: positionMs}
	if _, err := store.Update(func(cur *app.Settings) { *cur = s }); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

// resumeEntry 把一份队列手写进 `playback.items` 的形状（path 是本地文件）。
func resumeEntry(path string) app.PlaybackEntry {
	return app.PlaybackEntry{ID: "local:" + path, Title: filepath.Base(path)}
}

// TestStartupResumeSeeksBackThreeSecondsAndPlays 是这一票的主验收：
// 设置里有这一条且队列里找得到 → seek 到 positionMs-3s 然后**放起来**。
//
// ⚠️ **positionMs 手写成 5000**（见 resumeCopies 的注释）：素材只有 4 秒，
// 落盘那一格是「上次退出时放到哪儿了」——验的是「恢复时往回退 3 秒」
// 那一条换算，不是「真实放了 5 秒」。5 秒退 3 秒剩 2 秒，正好落在素材中间。
func TestStartupResumeSeeksBackThreeSecondsAndPlays(t *testing.T) {
	paths := resumeCopies(t, 2)
	_, path := playbackMount(t)
	resumeWriteSettings(t, path,
		[]app.PlaybackEntry{resumeEntry(paths[0]), resumeEntry(paths[1])},
		"local:"+paths[1], 5000)

	b := app.New()
	if err := b.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		b.Close()
		_ = b.CloseSettings()
	})
	// 断点续播排在「队列恢复」之后（和生产同一顺序，见 shell_window.go）。
	if err := b.RestorePlayback(); err != nil {
		t.Fatal(err)
	}
	if err := b.ResumePlayback(); err != nil {
		t.Fatal(err)
	}

	st := b.Snapshot()
	if !st.HasTrack {
		t.Fatal("恢复之后播放条上什么都没有")
	}
	if !st.Playing {
		t.Error("恢复之后没在放——「接着放」不是「停在原位」")
	}
	// 5 秒退 3 秒 = 2 秒，落在素材中间（4 秒）。
	want := 2 * time.Second
	if diff := st.Position - want; diff < -300*time.Millisecond || diff > 300*time.Millisecond {
		t.Errorf("恢复出来的位置是 %v，want 约 %v（5000ms 退 3 秒）", st.Position, want)
	}
	cur, ok := b.Queue().Current()
	if !ok {
		t.Fatal("恢复之后队列里没有当前项")
	}
	if cur.Path != paths[1] {
		t.Errorf("恢复出来的是 %q，want %q（上次在放的那一首）", cur.Path, paths[1])
	}
}

// TestStartupResumeClampsTheRewindAtZero 钉住「退 3 秒的下限是 0」：
// 上次只听了 1 秒就退出，恢复出来从头放，不是负位置。
func TestStartupResumeClampsTheRewindAtZero(t *testing.T) {
	paths := resumeCopies(t, 1)
	_, path := playbackMount(t)
	// positionMs=1000：退 3 秒会出界（-2 秒），下限是 0。
	resumeWriteSettings(t, path, []app.PlaybackEntry{resumeEntry(paths[0])}, "local:"+paths[0], 1000)

	b := app.New()
	if err := b.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		b.Close()
		_ = b.CloseSettings()
	})
	// 断点续播排在「队列恢复」之后（和生产同一顺序，见 shell_window.go）。
	if err := b.RestorePlayback(); err != nil {
		t.Fatal(err)
	}
	if err := b.ResumePlayback(); err != nil {
		t.Fatal(err)
	}
	st := b.Snapshot()
	if !st.Playing {
		t.Error("恢复之后没在放")
	}
	if st.Position < 0 {
		t.Errorf("恢复出来的位置是 %v，不许是负的", st.Position)
	}
	if st.Position > 500*time.Millisecond {
		t.Errorf("上次只听了 1 秒，恢复出来的位置是 %v，want 接近开头（退 3 秒出界钳到 0）", st.Position)
	}
}

// TestStartupResumeSkipsWhenTheKeyIsNotInTheQueue 钉住「key 找不到」：
// 设置里有这一条、但恢复出来的队列里没有那一条 → 不恢复、不报错。
//
// ⚠️ **key 手写一个队列里没有的**（`local:` 指向一个压根不在队列里的路径）：
// 「用户删了那一条」「文件被挪走了」都是这一种——设置里还留着，但队列里
// 没有它。不能拿「恢复出来的队列少一条」来凑：`playback` 那一格的守卫
// 会把认不出来的条目先跳过，key 找不找得到要的是**队列里压根没有**。
func TestStartupResumeSkipsWhenTheKeyIsNotInTheQueue(t *testing.T) {
	paths := resumeCopies(t, 1)
	_, path := playbackMount(t)

	// 队列里只有 paths[0]；resume 的 key 指向一个不存在的本地路径。
	resumeWriteSettings(t, path,
		[]app.PlaybackEntry{resumeEntry(paths[0])},
		"local:/tmp/这一条压根不在队列里.flac", 2000)

	b := app.New()
	if err := b.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		b.Close()
		_ = b.CloseSettings()
	})
	if err := b.RestorePlayback(); err != nil {
		t.Fatal(err)
	}
	if err := b.ResumePlayback(); err != nil {
		t.Errorf("key 找不到时报了错：%v（该安静跳过）", err)
	}
	st := b.Snapshot()
	if st.Playing {
		t.Error("key 找不到还自己放起来了")
	}
	if st.Err != "" {
		t.Errorf("key 找不到还冒了一句红字：%q", st.Err)
	}
	// 队列照旧停在 `playback` 那一格恢复出来的样子（那一首、暂停、原位）。
	cur, ok := b.Queue().Current()
	if !ok {
		t.Fatal("key 找不到连队列都空了")
	}
	if cur.Path != paths[0] {
		t.Errorf("队列被错动了：当前是 %q，want %q", cur.Path, paths[0])
	}
}

// TestPlaybackResumeKeyMatchesTheLyricKeyShape 钉住身份口径：
// `playbackResume.trackKey` 用 `lyricKeyOf` 那同一套（bvid / sid / 本地路径），
// 和 `playback.items` 的 `id`（`mv:` / `audio:` / `local:`）能互相认出来。
func TestPlaybackResumeKeyMatchesTheLyricKeyShape(t *testing.T) {
	paths := resumeCopies(t, 1)
	a, path := playbackMount(t)

	if err := a.PlayFrom(items(paths[0])...); err != nil {
		t.Fatal(err)
	}
	a.Close()
	if err := a.CloseSettings(); err != nil {
		t.Fatal(err)
	}
	r := resumeStored(t, path)
	if want := "local:" + paths[0]; r.TrackKey != want {
		t.Fatalf("本地那一条的 key 是 %q，want %q", r.TrackKey, want)
	}
}
