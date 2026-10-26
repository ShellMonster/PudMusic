package skeleton

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestPhaseIsClockOnlyAndWraps 钉住「相位只由钟决定、一个周期回到原处」：
// 同一屏的灰块靠这一条同步。
func TestPhaseIsClockOnlyAndWraps(t *testing.T) {
	t0 := time.Unix(100, 0)
	if Phase(t0) != Phase(t0.Add(Period)) {
		t.Fatal("一个周期之后相位应回到原处")
	}
	if p := Phase(t0.Add(Period / 2)); p <= 0 || p >= 1 {
		t.Fatalf("半周期处相位 %v 应在 (0,1)", p)
	}
}

// TestRowAndCardKeepTheirSize 钉住占位块的尺寸：数据一到换成真内容时版面不跳。
func TestRowAndCardKeepTheirSize(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Width(400).Children(func() {
			Row(c, "r", 56)
			Card(c, "k", 100)
		})
	}, 400, 400)
	tt.Frame()
	tt.Advance(Period / 3)
	tt.Frame()
}
