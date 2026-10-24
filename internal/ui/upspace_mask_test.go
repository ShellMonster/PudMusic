// UP 空间头图区那一层黑蒙版（票 #295 / 报告 L-13）。
//
// # 这一组断的是三件事
//
//  1. **蒙版在位**：头图区整块压着上游那一档黑（浅色 30% / 深色 50%）。
//  2. **文字那一带多压一档**：整块那一档是**平**的，而头图是**用户上传的
//     任意图片**——纯白头图是最坏情况，白字压在 30% 黑上只有 2.1:1。
//  3. **白字在两层主题下都读得出来**：最坏情况（纯白头图）下 ≥4.5:1。
//
// # 为什么用像素断而不用「调了哪个函数」断
//
// 蒙版这件事特别容易「代码在那儿、画面上没画」：多压一档的那一层是
// `Absolute` + 渐变背景，画歪了（位置错、渐变方向反、盖在文字底下）代码
// 照样编译过。所以这里断的是**画面上那个像素是多少**。
//
// # 采样位置不能乱挪
//
//   - **头图区上沿**（y=74）：那里**没有字**，所以只有整块那一档——
//     「蒙版在位（浅 30% / 深 50%）」量的是这一处。
//   - **有字那一带的下沿**（y=256）：在渐变**满档**之后（渐变在那一带的
//     上沿 15% 处满档），所以那里是「整块 × 再压一档」。
//
// 两处都在「名字和统计之间那块空地」（x = 页面左缘 + 300），那里什么都没有。
package ui

import (
	"image/color"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	mygoui "github.com/egoist/mygo/ui"
)

// TestTheSpaceHeaderMaskIsTheUpstreamOne 钉住头图区**整块**那一层黑蒙版的
// alpha。
//
// ⚠️ **这两个数是票面上的验收标准**（上游 `space-info.tsx:83-86` 的
// `overlayOpacity = isDarkTheme ? 0.5 : 0.3`），不是「差不多就行」。
func TestTheSpaceHeaderMaskIsTheUpstreamOne(t *testing.T) {
	if got := upHeaderMask(false); got != 0.30 {
		t.Errorf("浅色下头图区的蒙版是 %.2f，want 0.30（上游 `space-info.tsx:83-86`）", got)
	}
	if got := upHeaderMask(true); got != 0.50 {
		t.Errorf("深色下头图区的蒙版是 %.2f，want 0.50（上游同上）", got)
	}
}

// TestTheSpaceHeaderIsMaskedInBothThemes 断「蒙版真的画在头图区上」。
//
// ① 头图区上沿 = 底色 × (1-蒙版)；② 有字那一带 = 再 × (1-那一档)。
//
// ⚠️ **底色是 `t.Surface`**：没有头图时头图区铺的就是它（上游那一层也是
// 「没有图时是白底 × 30% 黑」，我们没有头图时是 `t.Surface` × 同一档）。
func TestTheSpaceHeaderIsMaskedInBothThemes(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newUIExplore(t)
			uiSpaceData(b, "量蒙版的 UP 主", 0, 123456)
			s, tt, _ := newSpaceShell(t, b, true, "77", systemIsDark(dark))
			openSpace(t, s, tt, 42)

			base := BuildTheme(s.app.SettingsSnapshot(), dark).Surface
			img := tt.Image()
			x := int(float32(sideTargetWidth(s.app.SettingsSnapshot())) + 300)

			// ① 头图区上沿：整块 × (1-蒙版)。
			wantTop := overBlack(base, upHeaderMask(dark))
			gotTop := img.RGBAAt(x, 74)
			if !nearRGB(gotTop, rgbOf(wantTop), 2) {
				t.Errorf("头图区上沿是 %v，want %v（底色 %v × (1-%.2f)）",
					gotTop, rgbOf(wantTop), base, upHeaderMask(dark))
			}

			// ② 有字的那一带：整块 × (1-蒙版) × (1-再压那一档)。
			wantBand := overBlack(wantTop, upHeaderTextScrim(dark))
			gotBand := img.RGBAAt(x, 256)
			if !nearRGB(gotBand, rgbOf(wantBand), 2) {
				t.Errorf("有字的那一带是 %v，want %v（再压 %.2f 之后）",
					gotBand, rgbOf(wantBand), upHeaderTextScrim(dark))
			}

			// ③ 多压的那一档必须**真的比整块那一档深**——两处相等就是
			// 「那一档没画出来」（而画面上仍然看着「有一层蒙版」）。
			if band := int(gotBand.R); band >= int(gotTop.R) {
				t.Errorf("有字的那一带（%v）没有比头图区上沿（%v）更深——"+
					"多压的那一档没画出来", gotBand, gotTop)
			}
		})
	}
}

// TestTheSpaceHeaderTextIsReadableOnAnyBanner 钉住「头图上的字在两层主题下
// 都读得出来」。
//
// ⚠️ **量的是最坏情况**：一张**纯白**的头图。
//
// 头图是**用户上传的任意图片**——浅色主题下没有蒙版的话，「白底头图 +
// 白字」是看不清的（对比度全看用户传了什么）。所以这一条量的不是「今天
// 这一张头图」，而是「**最亮的那一张**」。
//
// ⚠️ **两档字都要量**：名字是纯白，签名 / 统计的名字是 92% 的白
// （`upHeaderTextDim`）——后者压在同一个底上，对比度低一档。
func TestTheSpaceHeaderTextIsReadableOnAnyBanner(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			mask := upHeaderMask(dark)
			extra := upHeaderTextScrim(dark)

			// 最坏情况：纯白的头图（255,255,255）。
			worst := overBlack(overBlack(mygoui.RGB(255, 255, 255), mask), extra)
			// 另一头：没有头图时铺的 `t.Surface`（浅色 #f4f4f5 / 深色 #27272a）。
			plain := overBlack(overBlack(defaultSurface(dark), mask), extra)

			for _, bg := range []struct {
				what string
				c    mygoui.Color
			}{
				{"纯白头图", worst},
				{"没有头图", plain},
			} {
				for _, fg := range []struct {
					what string
					c    mygoui.Color
				}{
					{"白字", upHeaderText()},
					{"92% 的白", upHeaderTextDim()},
				} {
					if r := ink.Ratio(fg.c, bg.c); r < 4.5 {
						t.Errorf("%s上那%s的对比度只有 %.2f:1（< 4.5）——"+
							"头图是用户上传的任意图片，这一张是最坏情况",
							bg.what, fg.what, r)
					}
				}
			}
		})
	}
}

// TestTheSpaceHeaderNameIsWhiteOnThePicture 钉住「头图上的字是白字」。
//
// ⚠️ **断的是画面上那个字的颜色**：字要是跟着主题翻（浅色下深字），压在
// 压暗过的照片上就是「深字压深底」——那是这层蒙版要防的那件事。
//
// 断言用「笔画最深处那个像素」：字有抗锯齿，边缘是一圈过渡色，笔画中心
// 那一块才是字色本身（`deepestIn` 的注释写着这件事）。
func TestTheSpaceHeaderNameIsWhiteOnThePicture(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newUIExplore(t)
			uiSpaceData(b, "白色的 UP 主", 0, 123456)
			s, tt, _ := newSpaceShell(t, b, true, "77", systemIsDark(dark))
			openSpace(t, s, tt, 42)

			nameBox, ok := tt.Find("白色的 UP 主")
			if !ok {
				t.Fatalf("画面上找不到昵称，文字有 %q", tt.Texts())
			}
			img := tt.Image()
			// 有字那一带的底色（名字和统计之间那块空地）。
			x := int(float32(sideTargetWidth(s.app.SettingsSnapshot())) + 300)
			bg := img.RGBAAt(x, 256)

			glyph := deepestIn(img, nameBox, bg)
			if glyph.R < 240 || glyph.G < 240 || glyph.B < 240 {
				t.Errorf("昵称那一行的字是 %v，want 接近纯白（头图上的字是白字）", glyph)
			}
			if r := ink.Ratio(colorOf(glyph), colorOf(bg)); r < 4.5 {
				t.Errorf("昵称压在头图区那一片底上的对比度只有 %.2f:1（< 4.5）", r)
			}
		})
	}
}

// overBlack 是「一层 alpha 的黑压在这个底色上」的结果。
//
// ⚠️ **和画面上那一层是同一个算法**：`ui.Color.Over`（`mygo/ui/color.go`）
// 就是框架画半透明背景时用的那一个（`Over` = c 盖在 base 上）。
func overBlack(base mygoui.Color, alpha float32) mygoui.Color {
	return mygoui.RGBA(0, 0, 0, alpha).Over(base)
}

// rgbOf 把一个颜色变成不透明的 RGBA（拿来和画面上的像素比）。
func rgbOf(c mygoui.Color) color.RGBA {
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: 255}
}

// colorOf 是 rgbOf 的反过来那一个（画面上的像素 → 能算对比度的颜色）。
func colorOf(c color.RGBA) mygoui.Color {
	return mygoui.Color{R: c.R, G: c.G, B: c.B, A: 255}
}

// nearRGB 是两个像素差不超过 tol 就是「一样」。
//
// ⚠️ **tol 只给 2**：画面上那个值是**算出来的那一档**（底色 × 0.7 × 0.6），
// 差得多了就是某一档画歪了。仓库里那个 `sameColor` 给的是 8——那一档是给
// 「渐变 / 抗锯齿 / 主题推导出来的颜色」用的，**钉死一个 alpha 的时候
// 太松**：蒙版差 8 个通道（30% 和 34% 之间）它照样绿。
func nearRGB(got, want color.RGBA, tol int) bool {
	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	return d(got.R, want.R) <= tol && d(got.G, want.G) <= tol && d(got.B, want.B) <= tol
}

// defaultSurface 是默认设置下那一档 `t.Surface`。
//
// 没有头图时头图区铺的就是它（`spaceFill` 的那一支），而它是「没有头图」
// 那一档底色。
func defaultSurface(dark bool) mygoui.Color {
	if dark {
		return mygoui.RGB(0x27, 0x27, 0x2a)
	}
	return mygoui.RGB(0xf4, 0xf4, 0xf5)
}
