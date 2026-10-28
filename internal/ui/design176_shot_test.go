// 票 #176 的画面验收：自绘确认框、双月日期选择器、下载状态（蓝条 + 绿勾）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderDesign176Shots
//
// **深浅两套都出**（DESIGN §8）：深色下主色当底的那几处差别更明显，而这一票
// 改的三样里有两样（确认键、进度条）都是「一块颜色」。
package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/service/download"
)

// TestRenderDesign176Shots 把这三处存成 PNG，留给人眼和上游并排看。
func TestRenderDesign176Shots(t *testing.T) {
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
			// ── 确认框：危险那一档（红图标 + 红键）────────────────────
			_, s, tt := collectionShell(t, "12345",
				mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
			waitSongs(t, tt, s, 3)
			tt.SetDark(dark)
			enterMulti(t, tt)
			for _, title := range []string{"歌 一", "歌 二"} {
				if err := tt.Click("选择 " + title); err != nil {
					t.Fatal(err)
				}
				settleFrame(tt)
			}
			if err := tt.Click("批量删除"); err != nil {
				t.Fatal(err)
			}
			settleFrame(tt)
			tt.Move(1180, 770)
			tt.Frame()
			save(t, dir, "96-确认框-危险-"+name+".png", tt)

			// ── 双月日期选择器：两块并排 ────────────────────────────
			lt, ls := libraryShell(t, newLibFake(t).withLater(1, 1, "甲"), laterPath)
			waitFrames(t, lt, "把第一页拉回来", func() bool {
				return rowsShown(ls.pageOf(laterPath)) > 0
			})
			lt.SetDark(dark)
			if err := lt.Click(dateRangeLabel); err != nil {
				t.Fatal(err)
			}
			lt.Frame()
			lt.Move(1180, 770)
			lt.Frame()
			save(t, dir, "97-双月日期选择器-"+name+".png", lt)

			// ── 下载状态：蓝条 + 绿勾 ────────────────────────────────
			_, dt := newRows(t,
				dlSnapshot("歌 甲", download.StatusRunning, dlProgress, dlChunk),
				dlSnapshot("歌 乙", download.StatusDone, dlChunk, dlChunk),
				dlSnapshot("歌 丙", download.StatusError, dlProgress, dlChunk))
			dt.SetDark(dark)
			dt.Move(1180, 770)
			dlFrame(dt)
			save(t, dir, "98-下载状态-蓝条与绿勾-"+name+".png", dt)
		})
	}
}
