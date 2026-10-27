package ui

// 列表首屏骨架屏出图：设 BIU_SHOT_DIR 才存。看稍后再看、历史、关注、动态在拉时的八行占位。

import (
	"os"
	"testing"
)

func TestRenderSkeletonPageShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, page := range []struct{ name, path string }{
		{"140-骨架屏-稍后再看.png", laterPath},
		{"141-骨架屏-历史记录.png", historyPath},
		{"142-骨架屏-我的关注.png", followPath},
		{"143-骨架屏-用户动态.png", dynamicFeedPath},
	} {
		b := newTestShell(t, append(blockedBiliOpts(t, true), at(1200, 800))...)
		b.s.navigate(page.path)
		b.tt.Frame()
		b.tt.Frame()
		b.tt.Frame()
		save(t, dir, page.name, b.tt)
		b.Close()
	}
}
