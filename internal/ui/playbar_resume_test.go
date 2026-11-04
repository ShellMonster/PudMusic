package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 「断点续播」的 UI 验收：启动后播放条上的位置读数不是 0:00（接着放出来了）。

// resumeShellOnLocal 造一份「上次在放本地文件、放到 positionMs」的设置，
// 挂上外壳（队列和那一首都真的开出来），返回 Tester 和 App。
func resumeShellOnLocal(t *testing.T, positionMs int64) (*mygoui.Tester, *app.App) {
	t.Helper()
	src := flac(t)
	b, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	dir := t.TempDir()
	flacPath := filepath.Join(dir, "上次那首.flac")
	if err := os.WriteFile(flacPath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	st := app.DefaultSettings("")
	st.Playback = app.PlaybackSnapshot{
		Items:   []app.PlaybackEntry{{ID: "local:" + flacPath, Title: "上次那首"}},
		Current: "local:" + flacPath,
	}
	st.PlaybackResume = app.PlaybackResume{TrackKey: "local:" + flacPath, PositionMs: positionMs}
	sh := newTestShell(t, withSettings(st))
	// 停在一个干净的页面上：首页的「本周已听 0:00」会和播放条的读数撞字。
	sh.s.navigate("/empty")
	sh.tt.Frame()
	return sh.tt, sh.a
}

// TestThePlaybarIsNotAtZeroAfterAResume 钉住「启动后播放条进度不是 0:00」：
// 上次放到 1 秒，退 3 秒出界钳到 0——而歌放起来之后位置自己往前走，
// 走过 1 秒之后读数该是「0:01」，不是「0:00」。
//
// ⚠️ **判据是「歌自己走过 1 秒之后还在不在 0:00」**：`clock` 把不到 1 秒的
// 全写成 0:00，所以等的是「真的放起来之后位置过了 1 秒」——断的是「接着放」
// 真的出声了，不是「起播那一帧就离开了 0」。
func TestThePlaybarIsNotAtZeroAfterAResume(t *testing.T) {
	tt, a := resumeShellOnLocal(t, 1000)
	if err := a.RestorePlayback(); err != nil {
		t.Fatal(err)
	}
	if err := a.ResumePlayback(); err != nil {
		t.Fatal(err)
	}
	// 歌真的放起来要一拍：等位置走过 1 秒再画（`clock` 那一格不到 1 秒
	// 还是写 0:00）。
	waitFrames(t, tt, "恢复出来的那首自己走过 1 秒", func() bool {
		return a.PlayPosition() > time.Second
	})
	if tt.HasText("0:00") {
		t.Errorf("接着放之后播放条还在 0:00，文字有 %q", tt.Texts())
	}
	if !a.Snapshot().Playing {
		t.Error("恢复之后没在放——「接着放」不是「停在原位」")
	}
}

// TestThePlaybarShowsARewoundPositionAfterAResume 钉住「退 3 秒」：
// positionMs 手写 5000 → 恢复出来的读数是 2 秒那一段（0:02），不是 0:00。
func TestThePlaybarShowsARewoundPositionAfterAResume(t *testing.T) {
	tt, a := resumeShellOnLocal(t, 5000)
	if err := a.RestorePlayback(); err != nil {
		t.Fatal(err)
	}
	if err := a.ResumePlayback(); err != nil {
		t.Fatal(err)
	}
	// 恢复出来停在 2 秒那一段（5000-3000），素材 4 秒装得下。
	waitFrames(t, tt, "恢复出来的那首自己往前走", func() bool {
		return a.PlayPosition() > 0
	})
	// 读数是「0:02」那一段（歌在往前走，读到 0:02 / 0:03 都算对——
	// 钉的是「不是 0:00」，不是某一拍的具体值）。
	if !tt.HasText("0:0") {
		t.Errorf("接着放之后播放条没有位置读数，文字有 %q", tt.Texts())
	}
	pos := a.Snapshot().Position
	if pos < time.Second || pos > 3*time.Second {
		t.Errorf("恢复出来的位置是 %v，want 约 2 秒（5000ms 退 3 秒）", pos)
	}
}
