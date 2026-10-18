package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 验收项 1 的后半段：系统解码器解出来的歌，播放条也要认。
//
// 走 MyGo 的 Tester 而不是真窗口：控件、状态、解码链路都是真的，只是不往
// 屏幕上画。这样 CI 上也能跑，不用人盯着。

func TestAACPlaysThroughTheSystemDecoder(t *testing.T) {
	if _, err := exec.LookPath("afconvert"); err != nil {
		t.Skip("这台机器上没有 afconvert，造不出 AAC 素材")
	}
	dir := t.TempDir()
	wav := filepath.Join(dir, "tone.wav")
	if err := exec.Command("afconvert", "-f", "WAVE", "-d", "LEI16@44100", "-c", "2",
		"/System/Library/Sounds/Ping.aiff", wav).Run(); err != nil {
		t.Skipf("系统里没有 Ping.aiff：%v", err)
	}
	m4a := filepath.Join(dir, "tone.m4a")
	if err := exec.Command("afconvert", "-f", "m4af", "-d", "aac", "-b", "192000", wav, m4a).Run(); err != nil {
		t.Skipf("造不出 AAC 素材：%v", err)
	}

	h := newHarness(t)
	if err := h.app.Enqueue(app.LocalItem(m4a)); err != nil {
		t.Fatalf("加进队列：%v", err)
	}
	waitFor(t, "AAC 开始播", func() bool {
		h.tt.Frame()
		return h.app.Snapshot().HasTrack
	})

	st := h.app.Snapshot()
	if !st.Playing {
		t.Error("打开 .m4a 之后没有在播")
	}
	if st.Duration <= 0 {
		t.Error("打开 .m4a 之后没有时长，进度条画不出来")
	}
	if st.Title != "tone" {
		t.Errorf("歌名是 %q，want tone", st.Title)
	}
	if st.Err != "" {
		t.Errorf("播放条上有错误：%s", st.Err)
	}

	// 画面上要真的画出歌名和总时长。
	if !h.tt.HasText("tone") {
		t.Errorf("画面上没有歌名，文字有 %q", h.tt.Texts())
	}
	if _, ok := h.tt.Find("下一首"); !ok {
		t.Errorf("画面上找不到「下一首」，文字有 %q", h.tt.Texts())
	}

	// 进度真的在走：等一会儿看位置有没有往前。
	before := h.app.Snapshot().Position
	waitFor(t, "AAC 的播放位置往前走", func() bool {
		h.tt.Frame()
		return h.app.Snapshot().Position > before
	})
}

// TestDragAACToEnd 盯的是 play-list.ts 那套行为在系统解码器上也成立：
// 拖到末尾不崩、不假死，往回拖还能接着播。
//
// ⚠️ 拖到末尾之后**不再是「停下来」**了：队列里那一首放完，播放模式说「循环」
// （这是默认档，Biu 的初始值也是它），于是同一首从头再来——Biu 的 `next()`
// 在队列只有一首时做的正是这件事（`list.length === 1` → `currentTime = 0` +
// 重放）。这一条原来写的是「等它停下来」，那是**还没有播放模式的时候**的样子：
// 那一票要治的症状（BMG-47 A5「用户听到一首歌结束就停住，必须手动点下一首」）
// 就长在那个「停下来」上。
//
// 「不崩、不假死」那一半没有丢：下面照旧断**终态**（从头重放、拖回中间能接着
// 播、界面还点得动），而拖完之后位置是末尾还是开头要**看那一帧和那个 50ms 的
// tick 谁先**，所以不去断它（理由见 `TestDragToEndDoesNotCrash` 里那段）。顺带另一条（`TestDragToEndDoesNotCrash`）钉的是**队列是空
// 的**那种情形——那才是真的停下来。
func TestDragAACToEnd(t *testing.T) {
	if _, err := exec.LookPath("afconvert"); err != nil {
		t.Skip("这台机器上没有 afconvert")
	}
	dir := t.TempDir()
	wav := filepath.Join(dir, "tone.wav")
	if err := exec.Command("afconvert", "-f", "WAVE", "-d", "LEI16@44100", "-c", "2",
		"/System/Library/Sounds/Ping.aiff", wav).Run(); err != nil {
		t.Skipf("系统里没有 Ping.aiff：%v", err)
	}
	m4a := filepath.Join(dir, "tone.m4a")
	if err := exec.Command("afconvert", "-f", "m4af", "-d", "aac", "-b", "192000", wav, m4a).Run(); err != nil {
		t.Skipf("造不出 AAC 素材：%v", err)
	}

	h := newHarness(t)
	if err := h.app.Enqueue(app.LocalItem(m4a)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "AAC 开始播", func() bool {
		h.tt.Frame()
		return h.app.Snapshot().HasTrack
	})
	dragTo(t, h.tt, 1) // 一路拖到最右端

	// 循环这一档 + 队列里只有这一首 → 同一首从头再放。位置要真的回到开头，
	// 而且要在放（否则就是「卡在末尾」，也正是这一条要防的假死）。
	waitFor(t, "拖到末尾之后没有从头重放", func() bool {
		h.tt.Frame()
		st := h.app.Snapshot()
		return st.Playing && st.Position < 40*time.Millisecond
	})

	dragTo(t, h.tt, 0.2) // 往回拖，必须能接着播
	if !h.app.Snapshot().Playing {
		t.Error("往回拖之后没有接着播")
	}
	if err := h.tt.Click("暂停"); err != nil {
		t.Fatalf("拖到末尾之后界面点不动了：%v", err)
	}
}

// TestScreenshotsWithAAC 存一张放 AAC 时的画面，给人眼对照。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestScreenshotsWithAAC
func TestScreenshotsWithAAC(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	if _, err := exec.LookPath("afconvert"); err != nil {
		t.Skip("这台机器上没有 afconvert")
	}
	tmp := t.TempDir()
	wav := filepath.Join(tmp, "tone.wav")
	if err := exec.Command("afconvert", "-f", "WAVE", "-d", "LEI16@44100", "-c", "2",
		"/System/Library/Sounds/Ping.aiff", wav).Run(); err != nil {
		t.Skipf("系统里没有 Ping.aiff：%v", err)
	}
	// 三首不同名字的歌，截图上才看得出队列在走。
	names := []string{"第一首", "第二首", "第三首"}
	var items []app.Item
	for _, name := range names {
		m4a := filepath.Join(tmp, name+".m4a")
		if err := exec.Command("afconvert", "-f", "m4af", "-d", "aac", "-b", "192000", wav, m4a).Run(); err != nil {
			t.Skipf("造不出 AAC 素材：%v", err)
		}
		items = append(items, app.LocalItem(m4a))
	}

	h := newHarness(t)
	if err := h.app.Enqueue(items...); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "AAC 开始播", func() bool {
		h.tt.Frame()
		return h.app.Snapshot().HasTrack
	})
	if err := h.tt.Click("下一首"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "切到第二首", func() bool {
		h.tt.Frame()
		return h.app.Snapshot().Title == "第二首"
	})
	dragTo(t, h.tt, 0.4)
	save(t, dir, "06-aac-queue.png", h.tt)
}
