// 用户动态页的画面验收（T3，原型 `PAGES.feed`）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFeedShots
//
// 深浅两套：已登录（chip 横排 + 动态卡片）和未登录。对照 `ref/{dark,light}-feed.png`。
package ui

import (
	"image/color"
	"net/http"
	"os"
	"testing"
)

// TestRenderFeedShots 把用户动态页存成 PNG。
func TestRenderFeedShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{true, false} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			b := newUIExplore(t)
			b.on("/shot/cover.png", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "image/png")
				_, _ = w.Write(upspaceShotPNG(480, 480, color.RGBA{70, 130, 160, 255}, color.RGBA{200, 220, 230, 255}))
			})
			uiFollowList(b, 3, []any{})
			b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
				uiExploreJSON(w, 0, map[string]any{"total": 3, "list": []any{
					map[string]any{"mid": 11, "uname": "夜航电台"},
					map[string]any{"mid": 12, "uname": "一只耳朵"},
					map[string]any{"mid": 13, "uname": "老唱片研究所"},
				}})
			})
			b.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, r *http.Request) {
				uiExploreJSON(w, 0, map[string]any{
					"items": []any{
						upspaceShotArchive(b.URL+"/shot/cover.png", "v1", "BV1feedA"),
						upspaceShotText("t1", "今晚的第一首。外面下着雨，适合把音量再调小一点。"),
					},
					"offset": "", "has_more": false,
				})
			})
			s, tt, _ := newSpaceShell(t, b, true, "77", systemIsDark(dark))
			s.navigate(dynamicFeedPath)
			tt.Frame()
			f := s.dynamicFeedOf()
			waitFrames(t, tt, "动态和作者到了", func() bool { return len(f.items) >= 2 && len(f.authors) >= 3 })
			waitFrames(t, tt, "图下完", func() bool { return s.images.Get(b.URL + "/shot/cover.png").Ready() })
			tt.Move(30, 30)
			homeFrame(tt)
			save(t, dir, name+"-feed.png", tt)
		})
	}
}
