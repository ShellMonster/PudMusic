package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestWashWithLeavesBaseAloneWithoutColor 钉住「没取到色就原样」和「取到了偏向主色、
// 深色比浅色染得多」。
func TestWashWithLeavesBaseAloneWithoutColor(t *testing.T) {
	base, acc := ui.RGB(250, 250, 250), ui.RGB(200, 40, 40)
	if washWith(base, acc, 0, false) != base {
		t.Fatal("没取到色时底色不该变")
	}
	w := washWith(base, acc, 0.6, false)
	if w.G >= base.G || w.R >= base.R {
		t.Fatalf("应向主色偏: %+v", w)
	}
	d := ui.RGB(20, 20, 20)
	if wd := washWith(d, acc, 0.6, true); int(wd.R)-20 <= int(washWith(d, acc, 0.6, false).R)-20 {
		t.Fatal("深色应比浅色染得更多")
	}
}
