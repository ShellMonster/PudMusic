package ui

// 榜单横幅上那几句字的对比度。
//
// # 为什么要有这一条（2026-10-29 的视觉走查）
//
// 横幅是**固定的深色渐变 + 白字**（`homeTiles`：那些颜色不跟主题走，磁贴本身
// 就是「封面」）。DESIGN.md 对正文文字要求 ≥4.5:1（§四「语义色当字色」那一条
// 用的就是这个口径，且点名「最严的那个底」）。
//
// 副题那一句是「第 3 期（周榜）· 1970-01-01 更新」——用户判断「这张榜是哪一期」
// 全靠它，不是装饰。而它原来写的是**白 82%**：压在热榜那个橙红渐变亮端
// `(196,82,44)` 上合成出来是 `(244,224,217)`，实测只有 **3.60:1**，不达标。
//
// # 判据
//
// 按**实际用的那个颜色**（不是「白」）做 alpha 合成，再按 WCAG 算对比度；
// 渐变两端都量——哪一端更暗、对比度就更差。

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"
)

// boardTitleColor 是横幅大标题用的颜色（纯白）。
func boardTitleColor() ui.Color { return ui.RGBA(255, 255, 255, 1) }

// TestTheBoardBannerTextIsReadable 钉住「两张榜的横幅，标题和副题都读得清」。
//
// ⚠️ **两张榜都要**：热榜是橙红、原创榜是紫，两套渐变的亮度不一样——热榜那一端
// 更暗，正是原来掉到 3.60:1 的那一侧。只量原创榜的话会漏掉。
func TestTheBoardBannerTextIsReadable(t *testing.T) {
	for _, tab := range []int{homeHotTab, homeOriginalTab} {
		from, to, ok := boardTileColors(tab)
		if !ok {
			t.Fatalf("tab %d 在 homeTiles 里没有对应的磁贴", tab)
		}
		for name, bg := range map[string]ui.Color{"from": from, "to": to} {
			bgRGBA := color.RGBA{R: bg.R, G: bg.G, B: bg.B, A: 255}
			for label, fg := range map[string]ui.Color{
				"标题": boardTitleColor(),
				"副题": boardCaptionColor,
			} {
				got := contrastRatio(over(fg, bg), bgRGBA)
				if got < 4.5 {
					t.Errorf("%s 那一档的横幅（%s 端）%s对比度是 %.2f:1，要求 ≥4.5:1",
						homeTabs[tab], name, label, got)
				}
			}
		}
	}
}

// boardTileColors 取某张榜那张磁贴的渐变两端。
func boardTileColors(tab int) (from, to ui.Color, ok bool) {
	for _, tile := range homeTiles {
		if tile.tab == tab {
			return tile.from, tile.to, true
		}
	}
	return ui.Color{}, ui.Color{}, false
}
