package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// 皮肤页（/appearance）存成 PNG，留给人眼和设置页并排看：
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderAppearanceShots
//
// ⚠️ **深浅两套都要出**：皮肤页管的就是「长什么样」，深浅各一屏才算看过。
// 附带一张设置页「常规」那一屏——搬走之后那一组只剩「数据显示样式」，
// 两张并排才看得出「搬干净了没」。
func TestRenderAppearanceShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, withSettings(app.DefaultSettings("")), systemIsDark(dark))
			b.s.navigate(appearancePath)
			b.tt.Frame()
			save(t, dir, "90-"+name+"-皮肤.png", b.tt)

			b2 := newTestShell(t, withSettings(app.DefaultSettings("")), systemIsDark(dark))
			b2.s.navigate(settingsPath)
			b2.tt.Frame()
			save(t, dir, "91-"+name+"-设置-常规.png", b2.tt)
		})
	}
}
