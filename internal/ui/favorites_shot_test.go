// 票 #362 的画面验收：侧栏那两组收藏夹的**三种空状态**和**有内容**那一档。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFavoritesShots
//
// # 为什么深浅两套都要出
//
// 这一票改的是**一句话**，而那句话是灰字（`TextMuted`）——浅色和深色下的
// 灰不是同一个灰。只出浅色的话深色下那一句读不读得出来根本看不出来。
//
// # 这一组要看的正是「三句话不一样」
//
// 摆一句「还没有收藏夹」在「没拉到」的时候是撒谎（issue #362）。而**撒谎
// 在截图上是看不出来的**——所以这一组出图是为了让人**读**那一句，
// 而不是为了让像素比对过。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染。
package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestRenderFavoritesShots 把侧栏收藏夹那几档存成 PNG。
func TestRenderFavoritesShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// 80 **没登录**：侧栏上那一句是「登录后能看到收藏夹」——
			// 不是「还没有收藏夹」（那时候我们压根没去问 B 站）。
			{
				f := newFolderBili(t, nil)
				_, tt := refreshShellOpts(t, f, "", systemIsDark(dark))
				for range 3 {
					tt.Frame()
				}
				save(t, dir, "80-侧栏-收藏夹-没登录-"+name+".png", tt)
			}

			// 81 **拉到了，而且真的一个都没有**：那一句是「还没有收藏夹」。
			{
				f := newFolderBili(t, nil)
				f.pages = [][]map[string]any{{}}
				s, tt := refreshShellOpts(t, f, "7", systemIsDark(dark))
				s.app.RefreshFavorites()
				// ⚠️ **两组都要等到**：空状态那句话是按**两组**的状态算出来的。
				waitFrames(t, tt, "收藏夹拉完了（真的一个都没有）", func() bool {
					return s.app.FavoriteState(app.FavoritesCreated).Loaded &&
						s.app.FavoriteState(app.FavoritesCollected).Loaded
				})
				for range 3 {
					tt.Frame()
				}
				save(t, dir, "81-侧栏-收藏夹-空-"+name+".png", tt)
			}

			// 82 **拉失败**：那一句是「拉不到收藏夹，可以重试」——
			// 这一档**不许**摆「还没有收藏夹」。
			{
				f := newFolderBili(t, nil)
				f.failTheList()
				s, tt := refreshShellOpts(t, f, "7", systemIsDark(dark))
				s.app.RefreshFavorites()
				waitFrames(t, tt, "拉失败被记下来了", func() bool {
					return s.app.FavoriteState(app.FavoritesCreated).Err != ""
				})
				for range 3 {
					tt.Frame()
				}
				save(t, dir, "82-侧栏-收藏夹-拉失败-"+name+".png", tt)
			}

			// 83 **两组都有条目**：自动拉回来的那一份直接摆在那儿。
			//
			// ⚠️ **「我收藏的」那一组只有收藏夹**：合集 / 系列被
			// `app.collectedFavorites` 滤掉了（它们的内容列表是另一套接口，
			// 本项目没接，摆进去是死的路）。
			{
				f := newFolderBili(t, nil)
				f.pages = [][]map[string]any{folderPage(1, 2)}
				f.setCollected([]map[string]any{
					{"id": 900, "title": "入站必刷", "mid": 77, "type": 11, "state": 0},
					{"id": 902, "title": "鬼畜小课堂", "mid": 79, "type": 11, "state": 0},
				})
				s, tt := refreshShellOpts(t, f, "7", systemIsDark(dark))
				s.app.RefreshFavorites()
				waitFrames(t, tt, "两组都拉回来了", func() bool {
					return len(s.app.SideFavorites(app.FavoritesCreated)) == 2 &&
						len(s.app.SideFavorites(app.FavoritesCollected)) == 2
				})
				for range 3 {
					tt.Frame()
				}
				save(t, dir, "83-侧栏-收藏夹-两组都有-"+name+".png", tt)
			}
		})
	}
}
