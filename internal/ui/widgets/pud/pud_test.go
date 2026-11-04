package pud

import "testing"

// TestGridColsStepDownWithTheContentArea：6 / 5 / 4 / 2 列（原型 `.card-grid` 的媒体查询，
// 窗口宽减去侧栏 232 换成内容区宽）。
func TestGridColsStepDownWithTheContentArea(t *testing.T) {
	for _, c := range []struct {
		w    float32
		want int
	}{{1400, 6}, {1048, 6}, {1047, 5}, {948, 5}, {947, 4}, {760, 4}, {759, 2}, {0, 2}} {
		if got := GridCols(c.w); got != c.want {
			t.Errorf("内容区 %.0f：%d 列，want %d", c.w, got, c.want)
		}
	}
	for _, c := range []struct {
		w    float32
		want int
	}{{1100, 4}, {948, 4}, {900, 3}, {760, 3}, {700, 1}} {
		if got := VideoGridCols(c.w); got != c.want {
			t.Errorf("内容区 %.0f：视频 %d 列，want %d", c.w, got, c.want)
		}
	}
	if got := CellWidth(1020, 6, 24); got != 150 {
		t.Errorf("CellWidth(1020, 6, 24) = %v，want 150", got)
	}
}

// TestTrackColumnsFillTheRow：三列弹性列加固定列（40 / 88 / 56 / 96）和 16 的缝刚好铺满
// 列表宽；窄内容区去掉「专辑」那一列。
func TestTrackColumnsFillTheRow(t *testing.T) {
	for _, w := range []float32{1020, 1100, 980, 700} {
		main, album, owner := trackCols(w)
		n := 7
		if album == 0 {
			n = 6
		}
		sum := 2*rowPadX + colIdx + main + album + owner + colPlays + colDur + colAct + float32(n-1)*colGap
		if w >= 700 && (sum < w-1 || sum > w+1) {
			t.Errorf("列表宽 %.0f：各列加起来 %.1f", w, sum)
		}
	}
	if _, album, _ := trackCols(800); album != 0 {
		t.Errorf("列表宽 800 + 页边距（< %d）还画着专辑列", HideAlbumBelow)
	}
	if _, album, _ := trackCols(1020); album == 0 {
		t.Error("列表宽 1020 却没有专辑列")
	}
}
