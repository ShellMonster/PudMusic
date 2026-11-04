// 收藏夹 / 合集页的画面验收（T3，原型 `PAGES.collection`）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderCollectionPageShots
//
// 深浅两套都出：头部取色块、操作条、列表、多选批量条。图是材料，对照
// `ref/{dark,light}-collection*.png` 逐项过（见收尾报告）。
package ui

import (
	"os"
	"testing"
	"time"
)

// TestRenderCollectionPageShots 把收藏夹页存成 PNG（深 / 浅 × 普通 / 多选）。
func TestRenderCollectionPageShots(t *testing.T) {
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
			pics := &tintPicServer{}
			orig := tintClient
			t.Cleanup(func() { tintClient = orig })
			tintClient = newFakePicClient(pics)

			b := newCollectionBili(t)
			b.setMedias(mediaOf(1, "渺小"), mediaOf(2, "你就不要想起我"), mediaOf(3, "说到爱"), mediaOf(4, "言不由衷"))
			b.mu.Lock()
			b.infos["cover"] = coverURL
			b.mu.Unlock()
			_, s, tt := collectionShellFrom(t, b, []shellOpt{systemIsDark(dark)}, "", "12345")
			installRedImages(t, s)
			waitSongs(t, tt, s, 4)
			waitFrames(t, tt, "头图区取到色", func() bool {
				_, sat := s.tint.sync(s.app.Collection(s.collection.ref).Cover, func() {})
				return sat > 0
			})
			settleFrame(tt)
			save(t, dir, name+"-collection.png", tt)

			// 多选：开多选、勾两首，批量条贴在页底。
			s.toggleCollectionMulti(s.collection)
			settleFrame(tt)
			for _, id := range []string{"1", "2"} {
				if m, ok := s.collection.list.List().At(0); ok && id == "1" {
					s.collection.picked[m.ID] = true
				}
			}
			if m, ok := s.collection.list.List().At(1); ok {
				s.collection.picked[m.ID] = true
			}
			settleFrame(tt)
			// 行底色有 160ms 的淡入：推测试钟让它走完，再出图。
			for range 40 {
				if !tt.FramePending() {
					break
				}
				tt.Advance(16 * time.Millisecond)
			}
			save(t, dir, name+"-collection-multi.png", tt)
		})
	}
}
