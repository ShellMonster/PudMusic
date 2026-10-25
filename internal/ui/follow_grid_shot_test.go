// 「我的关注」那个网格的**行间距**（票 #266）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFollowGridShots
//
// # 深浅两套都要出
//
// 卡片底和页面底在深色下只差一档，那 16 的缝在深色下更容易看漏——只出浅色的话
// 深色下有没有缝说不清。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染。
package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
)

// TestRenderFollowGridShots 把「我的关注」那个网格存成 PNG，深浅各一套。
//
// # 为什么摆九个 UP 主
//
// 窗口摆成 **1400 宽**（默认的 1200 只有两列），网格于是是**三列**，九个人排成
// **三行**——行与行之间那 16 的缝有**两处**可看，列与列之间那 16 的缝有**两处**
// 可看。
func TestRenderFollowGridShots(t *testing.T) {
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
			b := newUIExplore(t)
			uiFollowTags(b, map[int]any{0: "默认分组"})
			uiFollowList(b, 9, []any{
				uiFollowUser(11, "第一个 UP"), uiFollowUser(12, "第二个 UP"),
				uiFollowUser(13, "第三个 UP"), uiFollowUser(14, "第四个 UP"),
				uiFollowUser(15, "第五个 UP"), uiFollowUser(16, "第六个 UP"),
				uiFollowUser(17, "第七个 UP"), uiFollowUser(18, "第八个 UP"),
				uiFollowUser(19, "第九个 UP"),
			})
			uiFollowSearch(b, "", nil)
			uiFollowTag(b, 0, []any{})

			s, tt, _ := newSpaceShell(t, b, true, "77", systemIsDark(dark), at(1400, 900))
			s.navigate(followPath)
			tt.Frame()
			f := s.followPageOf()
			waitFrames(t, tt, "人到了", func() bool { return len(f.users) == 9 && f.tagStarted })
			// ⚠️ **列数要几帧才稳**：列数是按**上一帧**量出来的容器宽算的
			// （`followPage.width` → `list.GridColumnsFor`），头一帧按一个还没量准
			// 的宽排成 5 列，下一帧量到真的才排成 3 列（理由同 `follow.go` 里
			// `keepFollowAnchor` 那段）。不等它稳，拍下来的是**上一档列数**。
			waitFrames(t, tt, "列数稳了", func() bool { return f.cols == list.GridColumnsFor(f.width) })
			// 指针挪开，别让它压在某一张卡上（悬停会升起来一条操作条）。
			tt.Move(30, 30)
			homeFrame(tt)
			save(t, dir, "45-"+name+"-关注-网格行间距.png", tt)
		})
	}
}
