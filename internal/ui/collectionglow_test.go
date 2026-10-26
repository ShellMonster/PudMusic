package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestCollectionGlowIsOffWithoutColorAndFadesOutWithIt 钉住头图区光晕：取不到色时
// 不画；取到了是「封面色 → 同色透明」，深色底比浅色底更浓。
func TestCollectionGlowIsOffWithoutColorAndFadesOutWithIt(t *testing.T) {
	light, dark := &ui.Theme{Background: ui.RGB(250, 250, 250)}, &ui.Theme{Background: ui.RGB(20, 20, 20)}
	if g := collectionGlow(light, 120, 0); g.From.A != 0 || g.To.A != 0 {
		t.Fatalf("没取到色时不该有光晕: %+v", g)
	}
	gl, gd := collectionGlow(light, 120, 0.6), collectionGlow(dark, 120, 0.6)
	if gl.From.A == 0 || gl.To.A != 0 {
		t.Fatalf("光晕应自上而下淡到透明: %+v", gl)
	}
	if gd.From.A <= gl.From.A {
		t.Fatalf("深色底光晕 %d 应比浅色底 %d 更浓", gd.From.A, gl.From.A)
	}
}
