package ui

// 这一票的画面验收（票 #624 / #625）：
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderCollectionInvalidShots
//
// 出两套图（深浅各一套）：
//
//	① 失效条目的那一行	标签、灰掉的歌名
//	② 歌单页滚下去之后	头图区滚走了、右下角回顶按钮
//
// ⚠️ **一个字都不许打真 B 站接口**：内容是内存夹具（`favBili`）。

import (
	"os"
	"strconv"
	"testing"
)

// TestRenderCollectionInvalidShots 把这两票改过的那几处存成 PNG。
func TestRenderCollectionInvalidShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	medias := []map[string]any{
		mediaOf(1, "正常的一首"),
		invalidMediaOf(2, "失效的那首"),
		mediaOf(3, "也正常的一首"),
	}

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			_, s, tt := collectionShellOpts(t, []shellOpt{systemIsDark(dark)},
				"", "12345", medias...)
			waitSongs(t, tt, s, 3)
			tt.Frame()
			save(t, dir, "170-收藏夹页-失效条目-"+name+".png", tt)

			// 滚下去：头图区跟着滚走，右下角出现回顶按钮。
			more := make([]map[string]any, 0, 40)
			for i := range 40 {
				more = append(more, mediaOf(100+i, "歌曲 "+strconv.Itoa(i+1)))
			}
			_, s2, tt2 := collectionShellOpts(t, []shellOpt{systemIsDark(dark)},
				"", "12345", more...)
			waitSongs(t, tt2, s2, 40)
			tt2.Frame()
			tt2.Scroll(600, 500, 0, 1500)
			for range 40 {
				tt2.Frame()
			}
			save(t, dir, "171-收藏夹页-滚下去之后-"+name+".png", tt2)
		})
	}
}
