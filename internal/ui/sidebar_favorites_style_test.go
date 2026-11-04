package ui

// 这一族盯的是**侧栏里那两组收藏夹的样式**（票 #371）。
//
// 这一片是 2026-10-24 那次界面重设计（PR #357）之后**没跟上的一处**：重设计改
// 了侧栏本身（展开 220 / 折叠 64、底色用方案的 `sidebar`、选中项底是
// `accentSoft` 淡底），而这一片还是老样子——它跟同一根侧栏里的菜单项放在一起
// 是两套风格。
//
// # 判据是「和同一根侧栏里的参照物对齐」，不是「某个具体的数」
//
// 这一片要跟的是侧栏那几样东西：分组标题（「发现」「我的音乐」）、它们的左沿、
// 菜单项的选中态。拿它们当参照物，而不是写死像素值——方案换了、主题的间距变
// 了，那些数会跟着变，而「是不是一套」这件事不会变。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染。

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"image"
	"image/color"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	"github.com/egoist/mygo/ui"
	mygoui "github.com/egoist/mygo/ui"
)

// TestTheFavoritesTextIsReadableOnTheSidebar 断这一片的字在侧栏底上
// **≥ 4.5:1**。
//
// 这一片有三种字，各压在不同的一层底上：
//
//   - **分组标题 / 空状态 / 在飞状态**：次要色，压侧栏底；
//   - **收藏夹名**：正文色，压侧栏底；
//   - **当前那一行**：按那层 `accentSoft` 淡底现算的字（`flatCurrentText`）。
//
// ⚠️ **四个方案 × 深浅两套都要过**：方案换了，字色和底色一起换——而它们的对
// 比度不是同一个数（实测最低的是经典浅色 4.83:1，暖纸浅色 5.00:1）。
func TestTheFavoritesTextIsReadableOnTheSidebar(t *testing.T) {
	for _, preset := range []app.Accent{
		app.AccentIndigo, app.AccentCinnabar,
		app.AccentAmber, app.AccentPine,
	} {
		for _, dark := range []bool{false, true} {
			shade := "浅色"
			if dark {
				shade = "深色"
			}
			st := app.DefaultSettings("")
			st.Accent = preset
			th := BuildTheme(st, dark)
			sidebar := tok.Of(th).Side
			for _, c := range []struct {
				what string
				from ui.Color
				onto ui.Color
			}{
				{"分组标题 / 空状态 / 在飞状态（次要色）", th.TextMuted, sidebar},
				{"收藏夹名（正文色）", th.Text, sidebar},
				{"当前那一行（按淡底现算）", flatCurrentText(th, true), accentSoft(th)},
			} {
				if r := ink.Ratio(c.from, c.onto); r < 4.5 {
					t.Errorf("%s / %s：%v 压 %v 只有 %.2f:1（< 4.5）",
						preset, shade, c.from, c.onto, r)
				}
			}
		}
	}
}

// inkOfRect 是那一块里的**字**色：底色取众数，字取离底最远的那一个像素
// （字是抗锯齿画的，最远的那一头就是它本身）。
func inkOfRect(img *image.RGBA, r mygoui.Rect) color.RGBA {
	bg := modeOf(bandPixels(img, r))
	return farthestFrom(img, r, bg)
}

// inkLeft 是那一条带里第一个「有墨」的 x —— **文字的左边缘**。
//
// 从 x0 开始往右扫，撞到第一个和底色差得够远的像素就停。
//
// ⚠️ **为什么不用 Tester 给的 rect**：MyGo 把**列表行**里的文字按 inline 段落
// 排版，而那个段落节点的 rect 是**行盒**的左沿（不是文字的）——量出来会差 20
// 个像素，而那 20 个正是这一片要钉的东西（收藏夹名的字该和分组标题的字同列）。
//
// ⚠️ **底色取那一带里的众数**：字只占一条带的一小部分，众数才是底。
func inkLeft(img *image.RGBA, r mygoui.Rect, x0 int) int {
	bg := modeOf(bandPixels(img, r))
	for x := x0; x < int(r.X+r.W); x++ {
		for y := int(r.Y); y < int(r.Y+r.H); y++ {
			if maxDiff(img.RGBAAt(x, y), bg) > 40 {
				return x
			}
		}
	}
	return -1
}

// samplePixel 取那一个像素。越界返回全透明——调用方拿它和期望值比，
// 不一样就是「那儿没有那个底色」。
func samplePixel(img *image.RGBA, x, y int) color.RGBA {
	if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
		return color.RGBA{}
	}
	return img.RGBAAt(x, y)
}
