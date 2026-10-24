package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 这一族把侧栏里那两组收藏夹存成 PNG，留给人眼看。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderSidebarFavoritesStyleShots
//
// # 为什么要出这么多套
//
// 这一片是 2026-10-24 那次界面重设计之后**没跟上的一处**（票 #371），而这一片
// 上的每一个数都**跟着主题走**：侧栏底色走方案的 `sidebar`、选中那一行的淡底
// 走 `accentSoft`（深浅两档不同）、折叠态那层浮层走 `surface1` / `radiusLg`。
//
// 只看一套的话，下面这几样一件都验不到：
//
//   - 暖纸那一套的淡底还看不看得出来（赤陶色的淡底和绿色的淡底不是一回事）；
//   - 深色下那一行是不是浮起来的（深色下淡底和底色的差比浅色下小）；
//   - 折叠态那层浮层是不是一块**卡片**（经典是控件层 + 1.5 倍圆角，方案里是
//     surface1 + radiusLg）。
//
// ⚠️ 一个字都不许打真 B 站接口：全部是内存夹具 + 无头渲染。
func TestRenderSidebarFavoritesStyleShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, preset := range []app.ThemePreset{
		app.ThemePresetClassic, app.ThemePresetWarmPaper,
		app.ThemePresetDuskAmber, app.ThemePresetSoftPeach,
	} {
		for _, dark := range []bool{false, true} {
			shade := "浅色"
			if dark {
				shade = "深色"
			}
			name := string(preset) + "-" + shade
			t.Run(name, func(t *testing.T) {
				st := app.DefaultSettings("")
				st.ThemePreset = preset
				st.ThemeMode = app.ThemeModeLight
				if dark {
					st.ThemeMode = app.ThemeModeDark
				}

				// 一 **两组都有条目**：这一片的主体（分组标题 + 收藏夹名）。
				{
					f := newFolderBili(t, nil)
					f.pages = [][]map[string]any{folderPage(1, 2)}
					f.setCollected([]map[string]any{
						{"id": 900, "title": "入站必刷", "mid": 77, "type": 11, "state": 0},
						{"id": 902, "title": "鬼畜小课堂", "mid": 79, "type": 11, "state": 0},
					})
					s, tt := refreshShellOpts(t, f, "7", withSettings(st), systemIsDark(dark))
					s.app.RefreshFavorites()
					waitFrames(t, tt, "两组都拉回来了", func() bool {
						return len(s.app.SideFavorites(app.FavoritesCreated)) == 2 &&
							len(s.app.SideFavorites(app.FavoritesCollected)) == 2
					})
					for range 3 {
						tt.Frame()
					}
					saveRect(t, dir, "侧栏-收藏夹-两组都有-"+name+".png", tt, 0, 420, 220, 220)

					// 二 **当前就在其中一个收藏夹页上**：那一行是 `accentSoft`
					// 淡底 + 按那层底现算的字——这一张就是看它和旁边那些行
					// 是不是一套。
					s.navigate(collectionPath(1))
					for range 3 {
						tt.Frame()
					}
					saveRect(t, dir, "侧栏-收藏夹-当前页-"+name+".png", tt, 0, 420, 220, 220)
				}

				// 三 **折叠态**：侧栏折起来，收藏夹换成一枚图标；点开是一层浮层。
				{
					f := newFolderBili(t, nil)
					f.pages = [][]map[string]any{folderPage(1, 2)}
					f.setCollected([]map[string]any{
						{"id": 900, "title": "入站必刷", "mid": 77, "type": 11, "state": 0},
					})
					folded := st
					folded.SideMenuCollapsed = true
					s, tt := refreshShellOpts(t, f, "7", withSettings(folded), systemIsDark(dark))
					s.app.RefreshFavorites()
					waitFrames(t, tt, "两组都拉回来了", func() bool {
						return len(s.app.SideFavorites(app.FavoritesCreated)) == 2 &&
							len(s.app.SideFavorites(app.FavoritesCollected)) == 1
					})
					for range 3 {
						tt.Frame()
					}
					if err := tt.Click(favCollapsedName); err != nil {
						t.Fatalf("点那枚「收藏夹」图标：%v", err)
					}
					for range 3 {
						tt.Frame()
					}
					saveRect(t, dir, "侧栏-收藏夹-折叠态浮层-"+name+".png", tt, 0, 120, 300, 420)
				}
			})
		}
	}
}
