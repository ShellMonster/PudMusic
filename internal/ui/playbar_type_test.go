// 播放条的字号与图标尺寸（GitHub #173，规格 #163 的 G26）。
//
// # 这一族钉的是**画面上的尺寸**
//
// 这一票改的每一件事在界面上都是「看得见但不出错」的那种：字号小一档、字重
// 重一档、图标小一圈——**没有一样会报错，也没有任何一条功能测试会红**，用户只会
// 觉得「这个界面跟那边不像」。所以判据全是量出来的：
//
//	字号 → 文字盒高（字号 × 行高，行高在同一套字体里是常数）
//	字重 → 墨迹的多少（400 和 700 的盒高一样，只有笔画粗细不同）
//	图标 → 图形那一块的墨迹（框的大小是摆出来的，画出来的才说明它多大）
//
// # 四个字号（上游那一档，票面写的就是它）
//
//	歌名 16 / 400（原来 15 / 700）
//	UP 名 14（原来 12）
//	时间 14（原来 12）
//	倍速 16（原来 13/14——那一枚原来**没写字号**，跟着主题的默认值走，
//	          而那个默认值 macOS 是 13、别的平台是 14：**同一枚按钮在两个
//	          平台上是两个字号**）
//
// # 两个图标
//
//	封面 hover 的箭头 32（原来 20）
//	上一首 / 下一首 32/22（原来 32/18）
package ui

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// TestThePlayBarUsesTheBiuTypeScale 钉播放条上那四个字号。
//
// ⚠️ **判据是量出来的盒高，不是代码里的数**：`Find` 给回来的是文字元素的盒子，
// 而盒高 = 字号 × 行高（`textBoxPerSize` = 1.1767，同一套字体里是常数）。
// 写成「代码里那个 16」的话，改代码的人顺手把测试也改了，一点也红不了。
//
// ⚠️ **深浅两套都要量**：字号跟主题无关，但**盒子不一样**——浅色下那几行字压在
// 白底上，深色下压在深灰上，而盒高只跟字号有关（画的是同一套字）。两套都量是
// 为了「深色下没换一套字」这件事有个交代。
func TestThePlayBarUsesTheBiuTypeScale(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt, _ := playbarTester(t, barState(), 1200, 0, dark)
			for _, want := range []struct {
				text string
				size float32
				what string
			}{
				{text: "一首歌", size: 16, what: "歌名"},
				{text: "某个 UP 主", size: 14, what: "UP 名"},
				{text: "0:00", size: 14, what: "当前时间"},
				{text: "1:40", size: 14, what: "总时长"},
				{text: "1x", size: 16, what: "倍速"},
			} {
				r, ok := tt.Find(want.text)
				if !ok {
					t.Fatalf("播放条上找不到%s「%s」，画面上的字：%q",
						want.what, want.text, tt.Texts())
				}
				got := r.H / textBoxPerSize
				if math.Abs(float64(got-want.size)) > 0.8 {
					t.Errorf("播放条上的%s是 %.1fpx，want %.0f——"+
						"上游那一档是歌名 16 / UP 名 14 / 时间 14 / 倍速 16",
						want.what, got, want.size)
				}
			}
		})
	}
}

// TestThePlayBarTitleIsNotBold 钉歌名的字重是 **400**（不是 700）。
//
// # 为什么要钉字重
//
// 上游那一句是 `text-base`（16 / 400）——**Tailwind 的 Preflight 把字重重置成
// inherit**，所以它不是加粗的。我们原来是 15 / 700：字号小一档、字重重一档。
// 而播放条上的歌名是**整条里最大的一行字**，它一粗，整条播放条就「重」过了头
// （满屏播放器的标题是 20 / 400——更大的字、更轻的重量）。
//
// # 判据：AST + 墨迹，两样都要
//
// **AST 那一半**钉的是「代码里没有 `Bold()`／`FontWeight()`」——字重在画面上
// 量不到盒子（400 和 700 的行高一样，只有笔画粗细不同），所以「哪一句用哪一档
// 字重」只能由代码钉住（这是 `typography_test.go` 里页标题那一族的同一个道理）。
//
// **墨迹那一半**钉的是「画出来的那一行确实不是粗的那一版」：同一串字在同一个
// 进程里按 400 和 700 各画一次，数各自的墨迹像素，播放条上那一行的墨迹必须
// **更靠近 400 那一版**。
//
// ⚠️ **两个对照都是同一个进程里画的**（同一个字体、同一套抗锯齿），所以判据是
// 「比 700 那一版轻」——**不是**「等于某个数」：墨迹的绝对值跟系统字体有关，
// 写死一个数换个平台就会飘。
func TestThePlayBarTitleIsNotBold(t *testing.T) {
	// AST 那一半：歌名那一句不许调 `Bold()` / `FontWeight(...)`。
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "playbar.go", nil, 0)
	if err != nil {
		t.Fatalf("解析 playbar.go 失败：%v", err)
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "viewTrackText" {
			return true
		}
		ast.Inspect(fn, func(m ast.Node) bool {
			stmt, ok := m.(*ast.ExprStmt)
			if !ok {
				return true
			}
			chain := chainCalls(stmt.X)
			// 找那一句 `ui.Text(c, title)…`——歌名。
			if !chainHas(chain, "Text") || !chainTextArgIsTitle(chain) {
				return true
			}
			found = true
			for _, c := range chain {
				switch c.name {
				case "Bold":
					t.Error("playbar.go 的歌名调了 Bold()——歌名是 16/400，" +
						"层次靠大小不靠重量（上游的 `text-base` 被 Preflight 重置成 inherit）")
				case "FontWeight":
					t.Error("playbar.go 的歌名调了 FontWeight(...)——歌名是 16/400")
				case "FontSize":
					if len(c.call.Args) == 1 && !mentionsFloat(c.call.Args[0], 16) {
						t.Errorf("playbar.go 的歌名是 %s，want 16",
							goformat(fset, c.call.Args[0]))
					}
				}
			}
			return true
		})
		return false
	})
	if !found {
		t.Fatal("playbar.go 里找不到画歌名的那一句——`viewTrackText` 变了？")
	}

	// 墨迹那一半。
	tt, s := playbarTester(t, barState(), 1200, 0, false)
	title, ok := tt.Find("一首歌")
	if !ok {
		t.Fatalf("播放条上找不到歌名，画面上的字：%q", tt.Texts())
	}
	bg := themeColor(BuildTheme(s.app.SettingsSnapshot(), false).Background)
	onBar := inkPixels(tt.Image(), title, bg)

	normal := titleInk(t, false)
	bold := titleInk(t, true)
	if onBar >= (normal+bold)/2 {
		t.Errorf("播放条上的歌名有 %d 个墨迹像素，而 400 那一版是 %d、700 那一版是 %d——"+
			"它画出来的是**加粗**的那一版", onBar, normal, bold)
	}
}

// chainCall 是「一串调用」里的一节。
type chainCall struct {
	name string
	call *ast.CallExpr
}

// chainCalls 把 `ui.Text(c, title).FontSize(…).MaxLines(1)` 那样的一串调用拆开，
// **从外往里**。
//
// ⚠️ 框架的样式 API 全是这种「接在后面」的写法，所以一句里能同时有字号、字色、
// 截断几样——而 `ast.Inspect` 只能看见「这一句里有没有某个调用」，看不出
// 「哪一个调用是哪一个」。拆成链才能把「歌名那一句的字号」和「错误那一句的字号」
// 分开。
func chainCalls(e ast.Expr) []chainCall {
	var out []chainCall
	for {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return out
		}
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			out = append(out, chainCall{name: fn.Sel.Name, call: call})
			e = fn.X
		case *ast.Ident:
			out = append(out, chainCall{name: fn.Name, call: call})
			return out
		default:
			return out
		}
	}
}

// chainHas 报这一串里有没有那个名字。
func chainHas(chain []chainCall, name string) bool {
	for _, c := range chain {
		if c.name == name {
			return true
		}
	}
	return false
}

// chainTextArgIsTitle 报这一串里那个 `Text(...)` 画的是不是 `title`。
//
// ⚠️ **只认 `title` 这一个字**：播放条上画文字的地方有好几处（歌名、UP 名、
// 错误、音质徽章），认错了就把别处的字号当成了歌名的。
func chainTextArgIsTitle(chain []chainCall) bool {
	for _, c := range chain {
		if c.name != "Text" {
			continue
		}
		for _, a := range c.call.Args {
			if mentionsIdent(a, "title") {
				return true
			}
		}
	}
	return false
}

// goformat 把一段源码写出来（报错时给人看）。
func goformat(fset *token.FileSet, e ast.Expr) string {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, e); err != nil {
		return "（写不出来）"
	}
	return buf.String()
}

// titleInk 量同一串字在 16px 下按 400 / 700 画出来各有多少墨迹像素。
//
// ⚠️ **字号和颜色都用播放条上那一份**（同一个主题、同一个字号）——否则量到的
// 墨迹差是别的东西带来的。
func titleInk(t *testing.T, bold bool) int {
	t.Helper()
	var bg color.RGBA
	b := newTestShell(t, at(400, 100), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.applyFrameTheme(c)
			bg = themeColor(c.Theme().Background)
			el := mygoui.Text(c, "一首歌").FontSize(16).TextColor(c.Theme().Text)
			if bold {
				el = el.Bold()
			}
			el.MaxLines(1)
		}
	}))
	tt := b.tt
	tt.Frame()
	r, ok := tt.Find("一首歌")
	if !ok {
		t.Fatalf("连对照的那一行都画不出来：%q", tt.Texts())
	}
	return inkPixels(tt.Image(), r, bg)
}

// TestTheCoverHoverArrowIsBigEnough 钉封面 hover 时那枚箭头是 **32**。
//
// ⚠️ **判据是画面上那枚箭头的墨迹，不是代码里的字号**：图标是 SVG，框的大小是
// 摆出来的，而「画出来多大」才是用户看见的。
//
// ⚠️ **量的是「亮的那一片」**：悬停时封面盖上一层 50% 的黑，而箭头是 92% 的白
// ——这一块里只有箭头是亮的。（封面的占位框自己有一圈亮边，所以那一块要**往里
// 缩 8 像素**再量。）
//
// 32 的那枚箭头有 60 个亮像素（16 宽 × 10 高）；20 的那枚只有 22 个（10 × 6）——
// 门槛放在 40，两头都离得开。
func TestTheCoverHoverArrowIsBigEnough(t *testing.T) {
	tt, _ := playbarTester(t, barState(), 1200, 0, false)
	cover, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("播放条上找不到封面，画面上的字：%q", tt.Texts())
	}
	// 指针停在封面上：那一层浮层是**悬停才画**的。
	tt.Move(cover.X+cover.W/2, cover.Y+cover.H/2)
	tt.Frame()

	inner := mygoui.Rect{
		X: cover.X + 8, Y: cover.Y + 8,
		W: cover.W - 16, H: cover.H - 16,
	}
	n, w, h := brightExtent(tt.Image(), inner)
	if n == 0 {
		t.Fatalf("悬停之后封面正中一块亮像素都没有——那一层浮层（和它那枚箭头）没画出来")
	}
	const wantPixels = 40
	if n < wantPixels {
		t.Errorf("封面 hover 的箭头只有 %d 个亮像素（%.0f×%.0f），want ≥ %d——"+
			"它太小了（上游那一枚是 32，原来是 20）", n, w, h, wantPixels)
	}
}

// TestTheSkipKeysDrawBiggerGlyphs 钉「上一首 / 下一首」是 **32/22**。
//
// 两半都要：
//
//	点击区 32 —— 跟上游的 `isIconOnly` 那一档一样（`iconHitMd`）
//	图形   22 —— 上游 `MusicPlayControl` 那两枚是 `size={22}`
//
// ⚠️ **图形的大小只能量墨迹**：那两枚是 SVG，元素框恒是 32，而 22 的图形画在
// 32 的框里。22 的图标有 142 个墨迹像素（13 宽 × 16 高），18 的那枚只有 94
// （11 × 12）——门槛放在 120，两头都离得开。
//
// ⚠️ **量的是「不是底色的那些像素」**：播放条的底是 `t.Background`，而那两枚
// 图形是 `t.Text`——深浅两套都分得开。
func TestTheSkipKeysDrawBiggerGlyphs(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt, s := playbarTester(t, barState(), 1200, 0, dark)
			bg := themeColor(BuildTheme(s.app.SettingsSnapshot(), dark).Background)
			for _, label := range []string{"上一首", "下一首"} {
				r, ok := tt.Find(label)
				if !ok {
					t.Fatalf("播放条上找不到「%s」，画面上的字：%q", label, tt.Texts())
				}
				if r.W != 32 || r.H != 32 {
					t.Errorf("「%s」的点击区是 %.1f×%.1f，want 32×32——"+
						"上游的图标键恒是 `w-[2rem] h-[2rem]`", label, r.W, r.H)
				}
				n, w, h := inkExtent2(tt.Image(), r, bg)
				const wantPixels = 120
				if n < wantPixels {
					t.Errorf("「%s」画出来的图形只有 %d 个墨迹像素（%.0f×%.0f），"+
						"want ≥ %d——图标是 18（上游那一枚是 22）", label, n, w, h, wantPixels)
				}
			}
		})
	}
}

// TestTheBarIsWiderWithATrackThanWithout 钉宽度账的**前提**：右边那一组是
// 「有歌时」的那一档。
//
// ⚠️ **「下载」那一枚是有歌时才摆的**（上游也是那个条件），所以没有歌时右边
// 那一组窄 40（一枚 32 的键加一道 8 的缝），而中间那一列（进度条）多拿一半：
// 20。
//
// 这一条钉的是**那个差**（40 / 20），不是某个绝对数——绝对数会跟着「右边那一组
// 以后多一枚键」这种正经改动一起变，而差值是「有没有歌」这件事本身。
func TestTheBarIsWiderWithATrackThanWithout(t *testing.T) {
	withTrack, _ := playbarTester(t, barState(), 1200, 0, false)
	withoutTrack, _ := playbarTester(t, app.State{Rate: 1, PlayMode: app.PlayModeLoop}, 1200, 0, false)

	with := rightGroupWidth(t, withTrack)
	without := rightGroupWidth(t, withoutTrack)
	if diff := with - without; math.Abs(float64(diff-40)) > 0.5 {
		t.Errorf("有歌时右边那一组是 %.1f，没歌时是 %.1f（差 %.1f），want 差 40——"+
			"「下载」那一枚是有歌时才摆的（32 的键加一道 8 的缝）",
			with, without, diff)
	}

	withBar, _ := withTrack.Find("播放进度")
	withoutBar, _ := withoutTrack.Find("播放进度")
	if diff := withoutBar.W - withBar.W; math.Abs(float64(diff-20)) > 0.5 {
		t.Errorf("没歌时进度条是 %.1f，有歌时是 %.1f（差 %.1f），want 差 20——"+
			"右边那一组多占的 40 是左边和中间各分一半", withoutBar.W, withBar.W, diff)
	}
}

// rightGroupWidth 量播放条右边那一组有多宽（第一枚的左缘到最后一枚的右缘）。
func rightGroupWidth(t *testing.T, tt *mygoui.Tester) float32 {
	t.Helper()
	first, ok := tt.Find("播放模式")
	if !ok {
		t.Fatalf("播放条上找不到「播放模式」，画面上的字：%q", tt.Texts())
	}
	last, ok := tt.Find("加歌")
	if !ok {
		t.Fatalf("播放条上找不到「加歌」，画面上的字：%q", tt.Texts())
	}
	return last.X + last.W - first.X
}

// inkPixels 数 box 里「不是底色」的像素有多少——一个字有多重，就看它有多少墨。
func inkPixels(img *image.RGBA, box mygoui.Rect, bg color.RGBA) int {
	n := 0
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			if distinctPixel(img.RGBAAt(x, y), bg) {
				n++
			}
		}
	}
	return n
}

// inkExtent2 数 box 里「不是底色」的像素有多少，并给出它们的外接框。
func inkExtent2(img *image.RGBA, box mygoui.Rect, bg color.RGBA) (n int, w, h float32) {
	x0, y0 := int(box.X+box.W), int(box.Y+box.H)
	x1, y1 := -1, -1
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			if !distinctPixel(img.RGBAAt(x, y), bg) {
				continue
			}
			n++
			if x < x0 {
				x0 = x
			}
			if x > x1 {
				x1 = x
			}
			if y < y0 {
				y0 = y
			}
			if y > y1 {
				y1 = y
			}
		}
	}
	if x1 < 0 {
		return 0, 0, 0
	}
	return n, float32(x1 - x0 + 1), float32(y1 - y0 + 1)
}

// brightExtent 数 box 里「亮」的像素（三个通道都大于 180）有多少，并给出外接框。
//
// 用于封面那一层浮层：罩子是 50% 的黑，箭头是 92% 的白——这一块里只有箭头亮。
func brightExtent(img *image.RGBA, box mygoui.Rect) (n int, w, h float32) {
	x0, y0 := int(box.X+box.W), int(box.Y+box.H)
	x1, y1 := -1, -1
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			c := img.RGBAAt(x, y)
			if c.R <= 180 || c.G <= 180 || c.B <= 180 {
				continue
			}
			n++
			if x < x0 {
				x0 = x
			}
			if x > x1 {
				x1 = x
			}
			if y < y0 {
				y0 = y
			}
			if y > y1 {
				y1 = y
			}
		}
	}
	if x1 < 0 {
		return 0, 0, 0
	}
	return n, float32(x1 - x0 + 1), float32(y1 - y0 + 1)
}

// themeColor 把框架的颜色换成标准库的 RGBA。
func themeColor(c mygoui.Color) color.RGBA {
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A}
}
