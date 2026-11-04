// 下载记录页的画面验收（T7）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderDownloadsShots
package ui

import (
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/download"
)

func shotDownloads() []download.Snapshot {
	return []download.Snapshot{
		{ID: "a|1", Title: "夜航星", Label: "音频 · 无损", Status: download.StatusRunning, Percent: 64, Total: 31_000_000, CanRetry: true},
		{ID: "b|1", Title: "富士山下", Label: "音频 · 高品质", Status: download.StatusQueued, Percent: -1, CanRetry: true},
		{ID: "c|1", Title: "十年（现场版）", Label: "视频 · 1080P", Status: download.StatusPaused, Percent: 38, Total: 220_000_000, CanRetry: true},
		{ID: "d|1", Title: "浮夸", Label: "音频 · 无损", Status: download.StatusDone, Percent: 100, Total: 28_400_000, CanRetry: true},
		{ID: "e|1", Title: "孤勇者", Label: "音频 · 高品质", Status: download.StatusError, Err: "网络中断", Percent: 12, CanRetry: true},
	}
}

func TestRenderDownloadsShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{true, false} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		t.Run(mode, func(t *testing.T) {
			s, tt := newShell(t, systemIsDark(dark))
			s.downloads.rowsFn = shotDownloads
			s.navigate(downloadListPath)
			for i := 0; i < 10; i++ {
				tt.Advance(50 * time.Millisecond)
				tt.Frame()
			}
			save(t, dir, mode+"-downloads.png", tt)
		})
	}
}
