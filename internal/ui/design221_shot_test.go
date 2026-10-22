// 票 #221 的画面验收：播放条那道影（上游两层——外层 div 的 `shadow-2xl`
// + 里面那张 Card 的 `shadow="sm"`）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderDesign221Shots
//
// **深浅两套都出**（DESIGN §8）：HeroUI 那一档深浅两套的第三层**不一样**——
// 深色下是条**里面**的 1 像素白边，浅色下是条**外面**的 1 像素黑边。
// 只看浅色的话深色下那道线没人看得到，而那道线正是「播放条浮起来」的全部
// 理由（深底上黑边看不见）。
package ui

import (
	"os"
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// TestRenderDesign221Shots 把播放条连同它那道影存成 PNG。
func TestRenderDesign221Shots(t *testing.T) {
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
			// 整窗：播放条贴在窗口底边，影落在内容区那一侧。
			//
			// ⚠️ **不放歌**（放歌要读本地文件，慢且与影无关）：没有歌时播放条
			// 照样画，影也照样在——要看的正是那条分界线。
			b := newTestShell(t, at(1200, 800), systemIsDark(dark))
			b.tt.Frame()
			save(t, dir, "96-播放条-整窗-"+name+".png", b.tt)

			// 单独画播放条：底是一整块纯色，那道影看得最清楚。
			tt := shadowScene(t, dark, func(s *Shell, c *mygoui.Context) {
				s.viewPlayBar(c, barState())
			})
			save(t, dir, "97-播放条-单独画-"+name+".png", tt)
		})
	}
}
