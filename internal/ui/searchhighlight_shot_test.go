package ui

import (
	"os"
	"testing"
)

func TestRenderSearchHighlightShot(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	st := newSearchStub(10, 10)
	s, tt := newSearchPage(t, "第", st)
	waitFrames(t, tt, "结果", loadedTo(s, searchTabSong, 1))
	save(t, dir, "150-搜索高亮.png", tt)
}
