// UP 空间头图区那一层黑蒙版（票 #295 / 报告 L-13）。
//
// # 这一组断的是四件事
//
//  1. **蒙版在位**：头图区整块压着上游那一档黑（浅色 30% / 深色 50%）——
//     **压在真头图上面**，不是压在「没有头图时那块纯色」上面。
//  2. **文字那一带多压一档**：整块那一档是**平**的，而头图是**用户上传的
//     任意图片**——纯白头图是最坏情况，白字压在 30% 黑上只有 2.1:1。
//  3. **白字在两层主题下都读得出来**：最坏情况（纯白头图）下 ≥4.5:1。
//  4. **系统字号放大时也读得出来**：字号一放大，那一行（头像 + 名字 + 签名 +
//     统计）就往上长，名字的上沿跟着跑——那一档必须跟着它走。
//
// # 为什么用像素断而不用「调了哪个函数」断
//
// 蒙版这件事特别容易「代码在那儿、画面上没画」：多压一档的那一层是
// `Absolute` + 渐变背景，画歪了（位置错、渐变方向反、盖在文字底下、满档
// 落得太低）代码照样编译过。所以这里断的是**画面上那个像素是多少**。
//
// # 采样位置不能乱挪
//
//   - **头图区上沿**（y=74，头图区自己的 10）：那里**不蒙**，只有整块那一
//     档——「蒙版在位（浅 30% / 深 50%）」量的是这一处。
//   - **有字那一带**（y=256）：满档之后（渐变在头图区自己的 28 处满档）。
//
// 两处都在「名字和统计之间那块空地」（x = 页面左缘 + 300），那里什么都没有。
package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
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
// ① 头图区上沿 = 底色 × (1-蒙版)；② 有字那一带 = 再 × (1-再压那一档)。
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

// TestTheSpaceHeaderMaskCoversARealBanner 断「蒙版压在**真头图**上面」。
//
// ⚠️ **这一条是「有头图」那一档**：别的用例里那张头图的地址是 `https://f/…`
// （下不下来），于是头图区画的是「没有头图时那块纯色」——**蒙版压没压在图上，
// 那一档根本没验过**。这里让假 B 站真的回一张 PNG。
//
// ⚠️ **那张 PNG 是纯白的**：那是**最坏情况**——蒙版要是没压住，白字就化在
// 白底里（浅色下只有 2.1:1）。
func TestTheSpaceHeaderMaskCoversARealBanner(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newUIExplore(t)
			banner := serveWhitePNG(b, "/banner.png")
			uiSpaceBannerData(b, "真头图的 UP 主", banner)
			s, tt, _ := newSpaceShell(t, b, true, "77", systemIsDark(dark))
			openSpace(t, s, tt, 42)
			waitFor(t, "头图下下来了", func() bool {
				return s.images.Get(banner).Ready()
			})
			tt.Frame()

			img := tt.Image()
			x := int(float32(sideTargetWidth(s.app.SettingsSnapshot())) + 300)

			// ① 头图区上沿：**纯白头图** × (1-蒙版)——这就是上游那个
			// #b2b2b2（浅色）/ #7f7f7f（深色）。
			wantTop := overBlack(mygoui.RGB(255, 255, 255), upHeaderMask(dark))
			gotTop := img.RGBAAt(x, 74)
			if !nearRGB(gotTop, rgbOf(wantTop), 2) {
				t.Errorf("真头图上头图区上沿是 %v，want %v（纯白 × (1-%.2f)）",
					gotTop, rgbOf(wantTop), upHeaderMask(dark))
			}

			// ② 有字的那一带：再压一档。
			wantBand := overBlack(wantTop, upHeaderTextScrim(dark))
			gotBand := img.RGBAAt(x, 256)
			if !nearRGB(gotBand, rgbOf(wantBand), 2) {
				t.Errorf("真头图上有字的那一带是 %v，want %v", gotBand, rgbOf(wantBand))
			}

			// ③ 名字压在那一带上：白字 ≥4.5:1。
			nameBox, ok := tt.Find("真头图的 UP 主")
			if !ok {
				t.Fatalf("画面上找不到昵称，文字有 %q", tt.Texts())
			}
			bg := img.RGBAAt(int(nameBox.X)-4, int(nameBox.Y)+1)
			if r := ink.Ratio(mygoui.RGB(255, 255, 255), colorOf(bg)); r < 4.5 {
				t.Errorf("真头图上昵称那一行的对比度只有 %.2f:1（< 4.5），底色 %v", r, bg)
			}
		})
	}
}

// TestTheSpaceHeaderTextStaysReadableAtEveryTextScale 断「系统字号放大时，
// 名字仍然落在满档里」。
//
// 🔴 **这是这一票的评审查出来的那一条**：名字的上沿是按「头像 140 高」推
// 出来的，而**系统字号一放大，那一行就往上长**——名字的上沿跟着跑（实测：
// 字号 1 倍时在头图区里 82 的地方，1.75 倍 → 60，2 倍 → 47，**2.25 倍
// （Windows 文本大小的上限）→ 31**）。满档要是落得比那更低，名字的上沿就
// 落进渐变的淡区，白字压着的底色从 #6b6b6b 变回 #a8a8a8，**对比度从
// 5.33:1 掉到 2.4:1**——而那正是这张票要防的那件事。
//
// ⚠️ **量的是名字上沿那一行的底色**（不是名字中间那一行）：名字的**上沿**
// 才是先落进淡区的那一处。
func TestTheSpaceHeaderTextStaysReadableAtEveryTextScale(t *testing.T) {
	for _, sc := range []float32{1, 1.5, 1.75, 2, 2.25} {
		t.Run(fmt.Sprintf("字号%.2f", sc), func(t *testing.T) {
			b := newUIExplore(t)
			banner := serveWhitePNG(b, "/banner.png")
			// **最坏情况**：长签名（折两行）+ 认证那一行——那一列最高，
			// 名字被顶得最高。
			uiSpaceBannerData(b, "名字很长很长的 UP 主", banner)
			s, tt, _ := newSpaceShell(t, b, true, "77", systemIsDark(false),
				withPrefs(mygoui.Preferences{TextScale: sc}))
			openSpace(t, s, tt, 42)
			waitFor(t, "头图下下来了", func() bool {
				return s.images.Get(banner).Ready()
			})
			tt.Frame()

			img := tt.Image()
			nameBox, ok := tt.Find("名字很长很长的 UP 主")
			if !ok {
				t.Fatalf("画面上找不到昵称，文字有 %q", tt.Texts())
			}
			// 名字**上沿**那一行的底色（名字左边 4 像素 = 头像和名字之间的空地）。
			bg := img.RGBAAt(int(nameBox.X)-4, int(nameBox.Y)+1)
			if r := ink.Ratio(mygoui.RGB(255, 255, 255), colorOf(bg)); r < 4.5 {
				t.Errorf("字号 %.2f 时昵称上沿的对比度只有 %.2f:1（< 4.5），底色 %v"+
					"——名字被顶进渐变的淡区了", sc, r, bg)
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

// ─────────────────────────────────────────────────────────────────────────────
// 夹具
// ─────────────────────────────────────────────────────────────────────────────

// serveWhitePNG 让假 B 站回一张**纯白**的 PNG，返回它的地址。
//
// ⚠️ **纯白是最坏情况**：头图是用户上传的任意图片，而蒙版要保的就是
// 「白底头图 + 白字」那一档读得出来。
func serveWhitePNG(b *uiExplore, path string) string {
	b.on(path, func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 400, 200))
		for y := 0; y < 200; y++ {
			for x := 0; x < 400; x++ {
				img.Set(x, y, color.RGBA{255, 255, 255, 255})
			}
		}
		var buf bytes.Buffer
		_ = png.Encode(&buf, img)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(buf.Bytes())
	})
	return b.URL + path
}

// uiSpaceBannerData 是 `uiSpaceData` 加两个东西：**真头图**和一个**最高的那
// 一列**（长签名折两行 + 认证那一行）。
//
// ⚠️ **「最高的那一列」是故意的**：那一列越高，头图区里那一行就往上长得越
// 厉害，名字被顶得越高——而名字的上沿落进渐变的淡区时，白字压着的底色就
// 从 #6b6b6b 变回 #a8a8a8（对比度 5.33:1 → 2.4:1）。所以验「字号放大时
// 读得出来」必须用**这一份**，不是随便一份。
func uiSpaceBannerData(b *uiExplore, name, banner string) {
	uiSpaceData(b, name, 0, 123456)
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"mid": 42, "name": name, "level": 4,
			"sign": "这是一个很长的签名，签名的内容长到要折成两行才放得下，" +
				"所以它一定折行了，一定折行了，一定折行了。",
			"face":         "https://f/face-42.jpg",
			"top_photo_v2": map[string]string{"l_200h_img": banner},
			// ⚠️ **role 不能是 0**：role 是 0 时 B 站那一档是「没认证」，那一行
			// 压根不画（`bili/space.go` 的 `officialOf`）——而那一行正是把
			// 名字顶得最高的那一行。
			"official": map[string]any{"role": 1, "title": "认证信息"},
		})
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 小工具
// ─────────────────────────────────────────────────────────────────────────────

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
