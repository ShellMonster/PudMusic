// 我的关注页的画面验收（T3，原型 `PAGES.follow`）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFollowShots
//
// 深浅两套：已登录（分组栏 + 搜索 + 两列卡片）和未登录。图是假接口给的合成数据。
// 对照 `ref/{dark,light}-follow.png`、`*-loggedout-follow.png`，见收尾报告。
package ui

import (
	"os"
	"testing"
)

// TestRenderFollowShots 把我的关注页存成 PNG。
func TestRenderFollowShots(t *testing.T) {
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
			uiFollowTags(b, map[int]any{1: "音乐人", 2: "电台"})
			uiFollowList(b, 6, []any{
				uiFollowUser(11, "夜航电台"), uiFollowUser(12, "一只耳朵"),
				uiFollowUser(13, "老唱片研究所"), uiFollowUser(14, "雨后街道"),
				uiFollowUser(15, "深夜食堂 BGM"), uiFollowUser(16, "钢琴与咖啡"),
			})
			uiFollowSearch(b, "", nil)
			uiFollowTag(b, 1, []any{})

			s, tt, _ := newSpaceShell(t, b, true, "77", systemIsDark(dark))
			s.navigate(followPath)
			tt.Frame()
			f := s.followPageOf()
			waitFrames(t, tt, "人到了", func() bool { return len(f.users) == 6 && f.tagStarted })
			tt.Move(30, 30)
			homeFrame(tt)
			save(t, dir, name+"-follow.png", tt)
		})
		t.Run(name+"-loggedout", func(t *testing.T) {
			b := newUIExplore(t)
			s, tt, _ := newSpaceShell(t, b, false, "", systemIsDark(dark))
			s.navigate(followPath)
			tt.Frame()
			homeFrame(tt)
			save(t, dir, name+"-loggedout-follow.png", tt)
		})
		t.Run(name+"-skeleton", func(t *testing.T) {
			b := newUIExplore(t)
			uiFollowTags(b, map[int]any{1: "音乐人", 2: "电台"})
			followHoldGrid(t, b)
			s, tt, _ := newSpaceShell(t, b, true, "77", systemIsDark(dark))
			s.navigate(followPath)
			tt.Frame()
			f := s.followPageOf()
			if !f.pager.loading {
				t.Fatal("右边那一格没在拉——骨架屏出图量的是加载态")
			}
			tt.Frame()
			homeFrame(tt)
			save(t, dir, name+"-follow-skeleton.png", tt)
		})
	}
}
