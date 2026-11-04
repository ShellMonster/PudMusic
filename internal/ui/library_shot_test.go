// 稍后再看 / 历史记录的画面验收（T7）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderLibraryShots
//
// 跑的是真外壳加一台假 B 站（`libFake`），数据全是合成的，封面为空（画「首字 + 渐变」）。
package ui

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func shotLaterBody() string {
	type row struct {
		title, owner string
		dur, prog    int
		view         int64
	}
	rs := []row{
		{"夜航星", "夜航电台", 227, -1, 8_230_000},
		{"十年", "一只耳朵", 231, 95, 5_300_000},
		{"富士山下", "夜航电台", 259, 0, 920_000},
		{"孤勇者", "音乐搬运组", 256, 0, 12_100_000},
	}
	items := make([]map[string]any, 0, len(rs))
	for i, r := range rs {
		items = append(items, map[string]any{
			"aid": int64(170001 + i), "bvid": fmt.Sprintf("BVshot%02d", i), "cid": int64(3001 + i),
			"title": r.title, "pic": "", "duration": r.dur, "progress": r.prog,
			"pubdate": int64(1700000000), "add_at": now.AddDate(0, 0, -i).Unix(), "is_pgc": false,
			"owner": map[string]any{"mid": 11 + i, "name": r.owner},
			"stat":  map[string]any{"view": r.view},
		})
	}
	return biliOK(map[string]any{"count": len(rs), "list": items})
}

func shotHistoryBody() string {
	type row struct {
		title, owner string
		ago          time.Duration
		dur          int
	}
	rs := []row{
		{"夜航星", "夜航电台", 20 * time.Minute, 227},
		{"十年", "一只耳朵", 3 * time.Hour, 231},
		{"富士山下", "夜航电台", 26 * time.Hour, 259},
		{"浮夸", "一只耳朵", 27 * time.Hour, 283},
		{"孤勇者", "音乐搬运组", 80 * time.Hour, 256},
	}
	items := make([]map[string]any, 0, len(rs))
	for i, r := range rs {
		items = append(items, map[string]any{
			"title": r.title, "cover": "", "author_name": r.owner, "author_mid": 11 + i,
			"view_at":  now.Add(-r.ago).Unix(),
			"progress": 60, "duration": r.dur,
			"kid":     float64(170001 + i),
			"history": map[string]any{"bvid": fmt.Sprintf("BVhist%02d", i), "cid": 4001 + i, "business": "archive"},
		})
	}
	return biliOK(map[string]any{"has_more": false, "page": map[string]any{"pn": 1}, "list": items})
}

func TestRenderLibraryShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{true, false} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		for _, c := range []struct{ name, path string }{
			{"later", laterPath}, {"history", historyPath},
		} {
			t.Run(mode+"-"+c.name, func(t *testing.T) {
				f := newLibFake(t)
				f.later[1] = shotLaterBody()
				f.history[1] = shotHistoryBody()
				tt, s := libraryShellOpts(t, f, c.path, systemIsDark(dark))
				for i := 0; i < 30; i++ {
					tt.Advance(50 * time.Millisecond)
					tt.Frame()
				}
				if rowsShown(s.pageOf(c.path)) == 0 {
					t.Fatalf("没有拿到行，文字有 %q", tt.Texts())
				}
				save(t, dir, mode+"-"+c.name+".png", tt)
			})
		}
	}
}
