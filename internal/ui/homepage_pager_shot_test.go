// 切 Tab 滑动中那条**接缝分割线**的画面验收（本票）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderPagerSeamShots
//
// 出三张：静止（没有分割线）、滑动中途（接缝处一条竖线）、走完之后（分割线收掉）。
//
// ⚠️ **出图是材料，审查才是那道门**（AGENTS.md §1）：这一份图是拿来看「分割线
// 有没有盖住被竖切的卡片右缘、静止时确实不画」的——看完要把差在哪儿写进收尾
// 说明。
package ui

import (
	"os"
	"testing"
	"time"
)

// TestRenderPagerSeamShots 把滑动中途和静止时的 pager 各存一张 PNG。
func TestRenderPagerSeamShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	_, tt := newHome(t, fullSongs())
	save(t, dir, "pager-静止.png", tt)

	if err := tt.Click(homeTabs[1]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v", homeTabs[1], err)
	}
	// 推到动画的中途（旧的那一档已经走了一半，新的那一档刚进来一半）。
	tt.Advance(25 * time.Millisecond)
	tt.Frame()
	save(t, dir, "pager-滑动中途.png", tt)

	tt.Advance(homePagerDuration + 100*time.Millisecond)
	tt.Frame()
	save(t, dir, "pager-走完之后.png", tt)
}
