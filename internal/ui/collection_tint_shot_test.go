// 歌单页头图区那层「封面主色光晕」的画面验收（票 #623）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderCollectionGlowShots
//
// **两张图**：一张有封面（光晕在）、一张没有（光晕不在）——
// 单看第一张的话，「光晕压根没画出来」和「光晕画对了」长得一样。
//
// ⚠️ **出图是材料，审查才是那道门**（AGENTS.md §1）：这一份图是拿来对着
// 上游 Biu `src/pages/video-collection/header/index.tsx` 与设计稿
// `docs/design-mockups/collection_header.png` 核那五项的
// （布局 / 间距 / 层级 / 密度 / 对齐），核完要把差在哪儿一条一条写进收尾说明。
//
// ⚠️ **这一票只改「颜色算得快一点」**，颜色值本身应与从前一致——所以视觉
// 审查要确认的是光晕**还在、还在同一个位置、强度没变**（色相有几度误差属
// 正常，不算缺陷）。
package ui

import (
	"os"
	"testing"
)

// TestRenderCollectionGlowShots 把歌单页头图区存成 PNG。
func TestRenderCollectionGlowShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	t.Run("有封面", func(t *testing.T) {
		pics := &tintPicServer{}
		orig := tintClient
		t.Cleanup(func() { tintClient = orig })
		tintClient = newFakePicClient(pics)

		_, s, tt := collectionShellWithCover(t, "12345", coverURL, mediaOf(1, "歌 一"))
		installRedImages(t, s)
		waitSongs(t, tt, s, 1)
		// 等取色那一趟回来，再画一帧。
		waitFrames(t, tt, "头图区取到色", func() bool {
			hue, sat := s.tint.sync(s.app.Collection(s.collection.ref).Cover, func() {})
			return sat > 0 && (hue < 12 || hue > 348)
		})
		settleFrame(tt)
		save(t, dir, "歌单头图-有封面.png", tt)
	})

	t.Run("没封面", func(t *testing.T) {
		// 没有封面的收藏夹：那一档画的是「正在加载」/「没有封面」，
		// **光晕不画**（取不到色就是没有光晕）。
		_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"))
		waitSongs(t, tt, s, 1)
		settleFrame(tt)
		save(t, dir, "歌单头图-没封面.png", tt)
	})
}
