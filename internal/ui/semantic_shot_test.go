// #172 的出图：语义色、深色层次、分隔线，深浅两套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderSemanticShots
//
// ⚠️ **深浅两套都要出**：这一票改的三件事在深浅两套下长得完全不一样——
// 语义色两套同值，但**卡片与背景的层次**（深色靠影、浅色靠那一圈描边）和
// **分隔线**（深色是 15% 白叠出来、浅色是 #dbdbdb）各是一套。
//
// ⚠️ **图是给人看的，判据在 `semantic_test.go` 里**：这里只负责把画面摆出来，
// 免得「像素对了但画面难看得没人要」。
package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/service/download"
)

// TestRenderSemanticShots 把这一票动过的地方各存一张 PNG，深浅各一套。
func TestRenderSemanticShots(t *testing.T) {
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
			// ① 下载记录：那张卡的底（一行「下载完成」= 成功色，一行出错 =
			// 危险色——那条红条是画面上唯一一块**实心**的语义色）。
			rows := []download.Snapshot{
				dlSnapshot("第一首", download.StatusDone, 100, 100),
				dlSnapshot("第二首", download.StatusError, 200, 100),
			}
			_, tt := shotDownloadShell(t, dark, rows)
			save(t, dir, "70-"+name+"-下载记录-卡片与语义色.png", tt)

			// ② 队列抽屉：左缘那条分隔线，和面板那一层底色。
			s, dt := shotSpaceShell(t, newUIExplore(t), dark, true, "77")
			s.drawerOpen = true
			// ⚠️ **要让抽屉滑到位**：Tester 的钟站着不动，不推它的话面板还在
			// 窗口外面（它从右边滑进来），存下来的图是空的右半边。
			settleDrawer(t, s, dt)
			save(t, dir, "71-"+name+"-队列抽屉-分隔线.png", dt)

			// ③ 关注那一页：一张**卡**（上游的 `bg-content1` + `shadow="md"`）——
			// 深色下它和页面底色几乎同色，分开的正是那圈影。
			b := newUIExplore(t)
			uiFollowTags(b, map[int]any{0: "默认分组"})
			uiFollowList(b, 4, []any{
				uiFollowUser(11, "第一个 UP"), uiFollowUser(12, "第二个 UP"),
				uiFollowUser(13, "第三个 UP"), uiFollowUser(14, "第四个 UP"),
			})
			uiFollowSearch(b, "", nil)
			uiFollowTag(b, 0, []any{})
			fs, ft := shotSpaceShell(t, b, dark, true, "77")
			fs.navigate(followPath)
			ft.Frame()
			f := fs.followPageOf()
			waitFrames(t, ft, "人到了", func() bool { return len(f.users) == 4 && f.tagStarted })
			ft.Frame()
			save(t, dir, "72-"+name+"-关注卡-卡片与影.png", ft)
		})
	}
}
