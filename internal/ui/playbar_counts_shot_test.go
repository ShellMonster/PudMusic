// 播放条左边第三段那三枚键带数字之后的样子（票 #701）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderPlayBarCountsShots
//
// # 这一族要看的是「三枚键各带一个数字」
//
//	分集 │ 收藏 99k+ │ 赞 99k+ │ 评论 99k+
//
// ⚠️ **深浅两套都要出**：数字是**次要色**（`TextMuted`），而次要色在深色底上
// 和浅色底上是**两档不同的灰**——只看一套的话，「数字读不读得出来」这件事
// 根本看不出来。
//
// ⚠️ **要有歌**：没歌的时候第三段一格都不画（`viewTrackActions` 第一行就
// return），而这一票要看的正是那三枚键。
//
// ⚠️ **要有数**：拿不到数的时候三枚键都是纯图标键（票 #701 的规矩：「拿不到就
// 一个数字都不画」），而这一族要看的是**带数字**的那一档。
package ui

import (
	"net/http"
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestRenderPlayBarCountsShots 存图：三枚键各带一个数字的样子，深浅各一套。
func TestRenderPlayBarCountsShots(t *testing.T) {
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
			b := newCommentBili(t, "reply-main.json")
			b.on("/x/web-interface/view", func(w http.ResponseWriter, r *http.Request) {
				uiExploreJSON(w, 0, map[string]any{
					"aid": 170001, "bvid": r.URL.Query().Get("bvid"),
					// ⚠️ **三个数都折成最宽的那一档**（上限那一格）：那是**最坏**
					// 的一档，宽度账就是按它算的。
					"stat": map[string]any{"reply": 99999, "favorite": 99999, "like": 99999},
				})
			})
			it := app.Item{
				Source: "online", Type: "mv", Bvid: "BVcountsShot", Cid: "1",
				Title:  "一首歌的名字很长很长很长很长",
				Artist: "一个非常非常非常非常非常非常长的 UP 主名字", ArtistMid: 1,
			}
			ts := commentCountShell(t, b, it, 1200)
			waitFrames(t, ts.tt, "三个数问回来了", func() bool {
				c := ts.s.app.ArchiveCounts()
				return c.Reply == 99999 && c.Favorite == 99999 && c.Like == 99999
			})
			ts.tt.SetDark(dark)
			ts.tt.Frame()
			// ⚠️ **这一份只画播放条**（`commentCountShell` 的 view 只调
			// `viewPlayBar`），所以**播放条就在窗口顶上**——裁的是**最上面**那
			// 76 高，不是底下那一段。
			saveCrop(t, dir, "03-播放条-三枚键带数字-"+name+".png", ts.tt, 0, playbarHeight)

			// 02 **最小窗口**（900）：那一档余量最小（实测 0.70）——「三段有没有
			// 撞上进度条」在这一档最看得出来。
			small := commentCountShell(t, b, it, minWindowW)
			waitFrames(t, small.tt, "三个数问回来了", func() bool {
				c := small.s.app.ArchiveCounts()
				return c.Reply == 99999 && c.Favorite == 99999 && c.Like == 99999
			})
			stt := small.tt
			stt.SetDark(dark)
			stt.Frame()
			saveCrop(t, dir, "04-播放条-三枚键带数字-最小窗口-"+name+".png", stt, 0, playbarHeight)
		})
	}
}
