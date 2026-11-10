package ui

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo/ui"
)

// TestHeatBucketsFollowTheDensity 钉住分桶和平方根归一：
// 同一桶里两条弹幕是峰值，隔壁桶一条是它的平方根。
func TestHeatBucketsFollowTheDensity(t *testing.T) {
	items := []app.DanmakuItem{
		{ID: "a", TimeMS: 0},
		{ID: "b", TimeMS: 1000},
		{ID: "c", TimeMS: 5000},
	}
	bars := heatBuckets(items, 20*time.Second)
	if len(bars) != 4 {
		t.Fatalf("20 秒按 5 秒分桶出了 %d 桶，want 4：%v", len(bars), bars)
	}
	if bars[0] != 1 {
		t.Errorf("最密的那一桶 = %v，want 1", bars[0])
	}
	if want := float32(0.7071); bars[1] < want-0.01 || bars[1] > want+0.01 {
		t.Errorf("一条弹幕的桶 = %v，want ≈ %v", bars[1], want)
	}
	if bars[2] != 0 || bars[3] != 0 {
		t.Errorf("空桶不是 0：%v", bars)
	}
}

// TestHeatBucketsCapLongSongs 钉住「长歌也压到 480 点以内」。
func TestHeatBucketsCapLongSongs(t *testing.T) {
	items := []app.DanmakuItem{{ID: "a", TimeMS: 0}}
	bars := heatBuckets(items, 6*time.Hour)
	if len(bars) != timelineHeatPoints {
		t.Errorf("6 小时的歌分了 %d 桶，want %d", len(bars), timelineHeatPoints)
	}
}

// TestHeatCacheOnlyRebuildsWhenTheBatchChanges 钉住缓存的身份：
// 同一批弹幕每帧返回同一份切片，条数一变才重算。
func TestHeatCacheOnlyRebuildsWhenTheBatchChanges(t *testing.T) {
	var h timelineHeat
	items := []app.DanmakuItem{{ID: "a", TimeMS: 0}, {ID: "b", TimeMS: 1000}}
	first := h.of(items, 20*time.Second)
	again := h.of(items, 20*time.Second)
	if len(first) == 0 || len(again) == 0 || &first[0] != &again[0] {
		t.Fatal("同一批弹幕没有复用上一次的分桶")
	}
	more := append(append([]app.DanmakuItem{}, items...), app.DanmakuItem{ID: "c", TimeMS: 2000})
	next := h.of(more, 20*time.Second)
	if len(next) == 0 || &next[0] == &first[0] {
		t.Fatal("弹幕条数变了还在用旧的分桶")
	}
}

// TestTimelineOverlaysDrawOutsideTheTrack 出图验几何：
// 已缓冲是轨道下面那条细线，热度是轨道上面的柱子，两者都不压在轨道上。
func TestTimelineOverlaysDrawOutsideTheTrack(t *testing.T) {
	const w, h = 200, 20
	green := ui.RGB(10, 200, 10)
	red := ui.RGB(200, 10, 10)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Width(w).Height(h).Draw(func(p *ui.Painter, r ui.Rect) {
			paintTimelineOverlays(p, r, timelineOverlay{
				buffered:    0.5,
				heat:        []float32{1, 0.5},
				pad:         8,
				trackW:      4,
				bufferedCol: green,
				heatCol:     red,
			})
		})
	}, w, h)

	img := tt.Image()
	near := func(x, y int, want ui.Color) bool {
		got := img.RGBAAt(x, y)
		return abs(int(got.R)-int(want.R)) <= 6 &&
			abs(int(got.G)-int(want.G)) <= 6 &&
			abs(int(got.B)-int(want.B)) <= 6 && got.A > 0
	}
	// 已缓冲：x 从 8 到 100，y 在轨道下面（14..15）。
	if !near(50, 15, green) {
		t.Errorf("已缓冲那一条没画在轨道下面：%v", img.RGBAAt(50, 15))
	}
	if near(150, 15, green) {
		t.Errorf("已缓冲画过了 50%%：%v", img.RGBAAt(150, 15))
	}
	// 热度：轨道上面（1..5），第一根最高。
	if !near(20, 3, red) {
		t.Errorf("热度柱没画在轨道上面：%v", img.RGBAAt(20, 3))
	}
	if near(20, 8, red) {
		t.Errorf("热度柱压到轨道上了：%v", img.RGBAAt(20, 8))
	}
}
