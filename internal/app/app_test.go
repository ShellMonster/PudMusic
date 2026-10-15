package app_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 这些测试用真解码器真播放器跑：阶段 1 要验收的就是「打开本地 FLAC 能出声、
// 进度条能跳」，用假的替身测等于没测。
//
// ⚠️ 播放走的是系统音频输出，CI 上没有声卡时 MyGo 会静音播放（照常走时间轴），
// 断言的都是时间和位置，不依赖「听得见」。

func flac(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	return p
}

// waitFor 轮询到 cond 成立，最多等 d。异步的东西（播放器 goroutine、解码 goroutine）
// 只能这样等，给死时间会 flaky。
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等了 %v，%s 还没发生", d, what)
}

func newApp(t *testing.T) *app.App {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)
	return a
}

func TestOpenPlaysAndReportsLength(t *testing.T) {
	a := newApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}

	st := a.Snapshot()
	if !st.HasTrack {
		t.Fatal("打开之后 HasTrack 还是 false")
	}
	if !st.Playing {
		t.Error("打开之后没有在播")
	}
	if st.Title != "stereo16" {
		t.Errorf("歌名是 %q，want stereo16", st.Title)
	}
	if st.Duration <= 0 {
		t.Fatalf("时长是 %v，解码器没报出长度", st.Duration)
	}
	if st.Err != "" {
		t.Errorf("状态里有错误：%s", st.Err)
	}

	// 位置会自己往前走——这就是进度条时钟在做的事。
	waitFor(t, 3*time.Second, "播放位置往前走", func() bool {
		return a.Snapshot().Position > 0
	})
}

func TestOpenUnknownFileKeepsTheError(t *testing.T) {
	a := newApp(t)
	if err := a.Open(filepath.Join(t.TempDir(), "nope.flac")); err == nil {
		t.Fatal("打开一个不存在的文件居然成功了")
	}
	if st := a.Snapshot(); st.HasTrack || st.Err == "" {
		t.Errorf("打开失败之后 HasTrack=%v Err=%q，应该是有歌=false 且有错误", st.HasTrack, st.Err)
	}
}

func TestTogglePausesAndResumes(t *testing.T) {
	a := newApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}

	a.Toggle()
	if a.Snapshot().Playing {
		t.Fatal("Toggle 之后还在播")
	}
	paused := a.Snapshot().Position

	time.Sleep(150 * time.Millisecond)
	if got := a.Snapshot().Position; got > paused+40*time.Millisecond {
		t.Errorf("暂停之后位置还在走：%v → %v", paused, got)
	}

	a.Toggle()
	if !a.Snapshot().Playing {
		t.Fatal("再 Toggle 一次没接着播")
	}
	waitFor(t, 3*time.Second, "恢复播放后位置往前走", func() bool {
		return a.Snapshot().Position > paused
	})
}

// TestScrubToEndAndBack 是阶段 1 的验收项 5：拖到文件末尾不崩、不假死，
// 往回拖还能接着播。
func TestScrubToEndAndBack(t *testing.T) {
	a := newApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	dur := a.Snapshot().Duration
	if dur <= 0 {
		t.Fatal("没有时长，没法验拖动")
	}

	a.ScrubStart()
	a.ScrubEnd(1) // 拖到最右端 = 文件末尾
	st := a.Snapshot()
	if st.Position < dur-30*time.Millisecond {
		t.Errorf("拖到末尾后位置是 %v，时长 %v", st.Position, dur)
	}
	// 末尾没有声音可放，播放机会自己停下来；不能卡住不动。
	waitFor(t, 3*time.Second, "拖到末尾后播放器停下来", func() bool {
		return !a.Snapshot().Playing
	})

	// 往回拖：用户意图仍然是「在放」，必须能接着播，而不是停在原地。
	a.ScrubStart()
	a.ScrubEnd(0.25)
	if got := a.Snapshot().Position; got > dur/2 {
		t.Errorf("往回拖之后位置是 %v，应该在 %v 附近", got, dur/4)
	}
	if !a.Snapshot().Playing {
		t.Error("往回拖之后没有接着播")
	}
	waitFor(t, 3*time.Second, "往回拖之后又开始走", func() bool {
		return a.Snapshot().Position > dur/4
	})
}

func TestScrubWhilePausedStaysPaused(t *testing.T) {
	a := newApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停

	a.ScrubStart()
	a.ScrubEnd(0.5)
	if a.Snapshot().Playing {
		t.Error("暂停状态下拖进度条，顺手把它播起来了")
	}
	if a.Snapshot().Position <= 0 {
		t.Error("暂停状态下拖进度条没有生效")
	}
}

func TestVolumeIsClamped(t *testing.T) {
	a := newApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	a.SetVolume(3)
	if got := a.Snapshot().Volume; got != 1 {
		t.Errorf("音量 %v，want 1（超范围要夹住）", got)
	}
	a.SetVolume(-1)
	if got := a.Snapshot().Volume; got != 0 {
		t.Errorf("音量 %v，want 0（负数要夹住）", got)
	}
}

// TestCloseReleasesFileHandles 是验收项 6：换歌、关应用都不该留下文件句柄。
//
// 这里连着开关 40 次，看 /dev/fd 里的条目数有没有涨。真漏一个句柄的话，
// 40 次之后 fd 表会明显变长。
func TestCloseReleasesFileHandles(t *testing.T) {
	a := newApp(t)
	path := flac(t)

	before := openFDs(t)
	for i := 0; i < 40; i++ {
		if err := a.Open(path); err != nil {
			t.Fatal(err)
		}
	}
	a.Close()

	waitFor(t, 5*time.Second, "文件句柄被释放", func() bool {
		return openFDs(t) <= before+2
	})
	if after := openFDs(t); after > before+2 {
		t.Errorf("开关 40 次之后打开的文件描述符从 %d 涨到 %d，句柄漏了", before, after)
	}
}

func openFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Skipf("读不到 /dev/fd，跳过句柄检查：%v", err)
	}
	return len(entries)
}
