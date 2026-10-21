// 票 #175 的画面验收：收藏夹头图区、下载记录卡片底、本地音乐工具条与行 hover。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderDesign175Shots
//
// **深浅两套都出**（DESIGN §8）：深色下卡片底和页面底色的差比浅色小得多，
// 只看浅色的话「卡片底到底有没有」这一条在深色下没人验。
package ui

import (
	"os"
	"testing"
)

// TestRenderDesign175Shots 把这三页的几个状态存成 PNG，留给人眼和上游并排看。
func TestRenderDesign175Shots(t *testing.T) {
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
			// ── 收藏夹：头图区四项 + 操作条 ────────────────────────────────
			_, s, tt := collectionShell(t, "12345",
				mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
			waitSongs(t, tt, s, 3)
			tt.SetDark(dark)
			tt.Move(1180, 770)
			tt.Frame()
			save(t, dir, "90-收藏夹-头图与操作条-"+name+".png", tt)

			// 搜索展开：输入框 40 高、排序下拉还在。
			tt.SetFocused(true)
			if err := tt.Click(collectionSearchLabel); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			save(t, dir, "91-收藏夹-搜索展开-"+name+".png", tt)

			// ── 下载记录：卡片底 + 单选筛选 + 文件名 hover ─────────────────
			_, dt := newRows(t, dlThreeShots()...)
			dt.SetDark(dark)
			dt.Move(1180, 770)
			dlFrame(dt)
			save(t, dir, "92-下载记录-卡片与筛选-"+name+".png", dt)

			if r, ok := dt.Find("歌 甲"); ok {
				dt.Move(r.X+r.W/2, r.Y+r.H/2)
				dt.Frame()
				dt.Frame()
			}
			save(t, dir, "93-下载记录-文件名hover-"+name+".png", dt)

			// ── 本地音乐：工具条 + 行 hover ────────────────────────────────
			root := t.TempDir()
			libSong(t, root, "第一首.mp3")
			libSong(t, root, "第二首.mp3")
			lt, _, _, _ := shellOnLocalMusic(t, root)
			lt.SetDark(dark)
			lt.Move(1180, 770)
			lt.Frame()
			save(t, dir, "94-本地音乐-工具条-"+name+".png", lt)

			if r, ok := lt.Find("第一首"); ok {
				lt.Move(r.X+r.W/2, r.Y+r.H/2)
				lt.Frame()
				lt.Frame()
			}
			save(t, dir, "95-本地音乐-行hover-"+name+".png", lt)
		})
	}
}
