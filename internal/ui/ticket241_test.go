package ui

// 票 #241 的判据：UI 报告 F-16 / F-19 / F-20 / F-21 / F-22 / F-24 / F-26。
//
// # 这一族的判据都在**画面上**
//
// 这一票改的全是「看起来」：墨迹的重量、句子有没有被吃掉、框有多宽、什么颜色。
// 断函数调用断不出这些——一个把错误原因截得更狠的实现照样全绿，而用户看到的是
// 「这一条流有多…」。所以这里一律量像素。
//
// ⚠️ **深浅两套都要跑**：语义色压深那一档只在浅色下要压（深色下成功 / 警告本来
// 就够），头像那一圈描边在深色下才看得出是「一圈描边」而不是「一块圆盘」。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"image"
	"image/color"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// F-16　顶栏末两枚的视觉重量
// ─────────────────────────────────────────────────────────────────────────────

// TestTheTopBarEndsWithTwoLightControls 钉住「顶栏最右那两枚和左边三枚线框图标
// 同一档」。
//
// 判据是**墨迹像素数**：报告那一份就是这么量的（每枚取「图标 ±6 像素」的框，
// 背景采样在框左上，数和底色差 > 12 的像素）。
//
// ⚠️ **量的是图标键自己的框**（32×32），所以五枚是同一块面积上的墨迹——
// 可比。改之前：头像 608、登录方块 752，而那几枚是 140~152。
//
// ⚠️ **左边那三枚**（打开文件 / 迷你 / 动态）：原来有四枚，票 #289 拿掉了顶栏
// 那一枚「设置」（侧栏和头像下拉里都有同一个动作）。
func TestTheTopBarEndsWithTwoLightControls(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, systemIsDark(dark))
			s, tt := b.s, b.tt
			openATrack(t, s)
			s.navigate("/")
			waitFrames(t, tt, "首页画出来了", func() bool { return tt.HasText("播放") })
			tt.Move(1180, 300)
			homeFrame(tt)

			th := BuildTheme(app.DefaultSettings(""), dark)
			bg := rgb(th.Background)

			// 前面三枚线框图标，和末两枚（头像 / 登录键）。
			lineIcons := []string{"打开文件", "迷你", "动态"}
			tail := []string{avatarLabel, "登录"}

			inks := map[string]int{}
			rects := map[string]ui.Rect{}
			for _, label := range append(append([]string{}, lineIcons...), tail...) {
				r, ok := tt.Find(label)
				if !ok {
					t.Fatalf("顶栏上找不到 %q（文字有 %q）", label, tt.Texts())
				}
				inks[label] = inkInRect(tt.Image(), r, bg)
				rects[label] = r
			}

			// 那三枚线框图标之间没有差别（报告实测 140~152），取它们的最大值当
			// 「这一档」的尺子。
			tier := 0
			for _, label := range lineIcons {
				if inks[label] > tier {
					tier = inks[label]
				}
			}
			// 头像：和那三枚线框图标**同一档**。
			//
			// ⚠️ **上限给到 1.3 倍**：那一档不是「一模一样」（头像有一圈 1 像素的描
			// 边，那三枚线框图标没有），而是「同一档」——报告的原话。实测：头像
			// 152~160、那三枚 137~141，也就是 1.08~1.14 倍（那三枚彼此之间也有
			// 1.03 倍的差别）。改之前是 608（4.3 倍）。
			if inks[avatarLabel] > tier*13/10 {
				t.Errorf("头像那一格的墨迹是 %d 像素，而左边那三枚线框图标是 %d~%d——"+
					"它比邻居重，用户的眼睛先落在它身上（框 %v）",
					inks[avatarLabel], inks[lineIcons[0]], tier, rects[avatarLabel])
			}
			// 登录键：**和那三枚线框图标同一档**。
			//
			// ⚠️ **这一枚的图形是二维码**（票 #242：登录是扫码登录，而「圆圈里一支
			// 箭头」那一对在 18px 上分不清进出）。二维码**天生比线框图标密**：18px
			// 下它那几块方块糊成一块（墨迹 191，是邻居的 1.36 倍——整排最重的一枚），
			// 所以它画在 **16**（`iconLoginSize`）：那几块还是分得开的，墨迹 137，
			// 和邻居**同一档**。
			//
			// 顺带断一句「它不是一块实心主色的方块」：F-16 / F-05 说的就是那块底
			// （改之前 778 块主色像素），这一格现在一块主色像素都没有。
			if inks["登录"] > tier*13/10 {
				t.Errorf("登录键那一格的墨迹是 %d 像素，而左边那三枚线框图标是 %d~%d——"+
					"它比邻居重，用户的眼睛先落在它身上（框 %v）",
					inks["登录"], inks[lineIcons[0]], tier, rects["登录"])
			}
			loginRect := rects["登录"]
			accent := hexColorOf(t, "#1ed760")
			if n := countPixelsIn(t, tt, int(loginRect.X)-2, int(loginRect.Y)-2,
				int(loginRect.X+loginRect.W)+2, int(loginRect.Y+loginRect.H)+2, accent); n > 0 {
				t.Errorf("登录键那一格里有 %d 块主色像素——它不该再是一块实心主色的方块"+
					"（改之前是 778，是相邻线框图标的 5.4 倍）", n)
			}
			t.Logf("三枚线框图标 %v，末两枚 %v", inks, inks)
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// F-19　下载记录的错误原因不再被截断
// ─────────────────────────────────────────────────────────────────────────────

// TestTheErrorReasonIsNotCutOff 钉住「错误原因整句都在画面上」。
//
// 判据是**末行右端的笔画**：省略号（…）是**基线上三个点**（实测：只有两行像素），
// 而一个汉字的笔画铺满整个字身（实测：十一行）。所以末行最右那几列只有两行墨
// = 那一行末尾是省略号 = 那句话被截断了。
//
// ⚠️ **改之前这一条是红的**：那一版末行是「这一条流有多…」，右端那八列只有基线
// 上那两个点。
func TestTheErrorReasonIsNotCutOff(t *testing.T) {
	_, tt := newRows(t, dlSnapshot("歌 甲", download.StatusError, dlProgress, dlChunk))
	if !shows(tt, dlErrText) {
		t.Fatalf("那一格没有画出错误原因，文字有 %q", tt.Texts())
	}
	rows := inkTailRows(t, tt, dlErrText, rgb(BuildTheme(app.DefaultSettings(""), false).Background))
	if rows < 4 {
		t.Errorf("错误原因末行最右那八列只有 %d 行墨——那是省略号（基线上三个点），"+
			"说明这句话被截断了", rows)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// F-20　确认弹窗不再是一条
// ─────────────────────────────────────────────────────────────────────────────

// TestTheConfirmPanelIsWideEnough 钉住「确认框不是 228 宽的一条」。
//
// 报告实测：改之前那个面板是 **228 宽**（标题几乎顶到边），上游的 `max-w-lg`
// 是 512。报告给的建议是 320~360——现在取 340。
//
// ⚠️ **量的是面板而不是标题那行字**：`Find(标题)` 给的是**面板**（面板是标题的
// 父，父先被遍历）。
func TestTheConfirmPanelIsWideEnough(t *testing.T) {
	_, s, tt := collectionShell(t, "12345",
		mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
	waitSongs(t, tt, s, 3)
	openBatchConfirm(t, tt, "批量删除")

	panel, ok := tt.Find("确认删除选中的 2 首吗？")
	if !ok {
		t.Fatalf("确认框没画出来（找不到标题），文字有 %q", tt.Texts())
	}
	if panel.W < 320 {
		t.Errorf("确认框是 %.0f 宽，want ≥320——它现在是一条，"+
			"标题几乎顶到边（改之前是 228）", panel.W)
	}
	if panel.W > 460 {
		t.Errorf("确认框是 %.0f 宽，want ≤460——它不该比内容宽出一大截", panel.W)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// F-21　迷你窗的音量滑块不再用主色
// ─────────────────────────────────────────────────────────────────────────────

// TestTheMiniVolumeIsNotTheAccent 钉住「迷你窗里那根音量滑块不是主色绿的」。
//
// 迷你播放器只有 320×100，那条音量滑块原来是**实心主色填充**——纯 #1ed760 占
// 那扇窗的 0.97%，是整扇窗里最大的一块颜色，而它是**次要**的一档（音量），
// 同窗那条播放位置（主要的）反倒不是彩色的。
//
// 判据：**音量那一格里一块主色像素都没有**。
func TestTheMiniVolumeIsNotTheAccent(t *testing.T) {
	requireTestAudio(t)
	a := app.New()
	t.Cleanup(a.Close)
	main := newTestShell(t, onApp(a)).s
	s := NewMini(a, main, nil)
	t.Cleanup(s.closePerWindow)

	if err := enqueueTwoWithCover(a, t); err != nil {
		t.Fatal(err)
	}
	a.SetVolume(0.6)

	tt := miniTester(s)
	waitFor(t, "封面画上来了", func() bool {
		tt.Frame()
		return !miniCoverBox(tt).Empty()
	})

	vol, ok := tt.Find("音量")
	if !ok {
		t.Fatalf("迷你窗里找不到那根音量滑块，文字有 %q", tt.Texts())
	}
	accent := hexColorOf(t, "#1ed760")
	x0, y0 := int(vol.X)-2, int(vol.Y)-2
	x1, y1 := int(vol.X+vol.W)+2, int(vol.Y+vol.H)+2
	n := countPixelsIn(t, tt, x0, y0, x1, y1, accent)
	if n > 0 {
		t.Errorf("音量滑块那一格里有 %d 块主色像素——那一扇窗只有 320×100，"+
			"主色绿占那么大一块，比同窗那条播放位置（主要的）还响", n)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// F-22　语义色当字色要压深
// ─────────────────────────────────────────────────────────────────────────────

// TestTheSemanticColorsAsTextAreReadable 钉住「语义色当字色用时 ≥4.5:1」。
//
// 上游 HeroUI 那三个色相浅色下当字色是：成功 2.19:1、警告 2.04:1、危险 4.14:1
// （数在 `theme.go` 的语义色那一段）。**色值不动**（动了就是自己发明配色），
// 而是**当字色用时另取一档**：色相不变，只压深。
func TestTheSemanticColorsAsTextAreReadable(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			th := BuildTheme(app.DefaultSettings(""), dark)
			for _, c := range []struct {
				what string
				got  ui.Color
				raw  ui.Color
			}{
				{"危险", dangerText(th), th.Danger},
				{"警告", warningText(th), th.Warning},
				{"成功", successText(th), th.Success},
			} {
				// ⚠️ **三个底都要够**：页面底色、控件那一层（`t.Surface`）、以及它
				// hover 时的样子（`t.SurfaceHover`）——语义色当字色用的地方不止页面
				// 底色（快捷键那一格就画在 `t.Surface` 上，评审查出来过：那一处只有
				// 3.77:1）。这一档取的是**最严的那个底**（`SurfaceHover`），所以三个
				// 一起达标。
				for _, bg := range []struct {
					what string
					c    ui.Color
				}{
					{"页面底色", th.Background},
					{"Surface", th.Surface},
					{"SurfaceHover", th.SurfaceHover},
				} {
					if r := ink.Ratio(c.got, bg.c); r < 4.5 {
						t.Errorf("%s 当字色是 %v，压在%s上只有 %.2f:1，want ≥4.5"+
							"（色相不变，只压深）", c.what, c.got, bg.what, r)
					}
				}
				// ⚠️ **主题里那三个值一个字节都没动**——这一档是「当字色用时另取
				// 一档」，不是改主题（改主题就是「自己发明了一个上游没有的配色」）。
				if c.got != c.raw && c.got == th.Accent {
					t.Errorf("%s 当字色那一档和主色撞了（%v）", c.what, c.got)
				}
			}
		})
	}
}

// TestTheDownloadDoneTextIsNotTheRawSuccess 钉住「「下载完成」那几个字是压深过
// 的那一档，不是上游那个 2.19:1 的绿」。
//
// ⚠️ **只看那几个字**：那枚绿勾是**图形**，它用主题里那一个原值（图形不受 4.5:1
// 约束，3:1 即可）。所以这里量的是文字那一块，勾在它左边，不在框里。
func TestTheDownloadDoneTextIsNotTheRawSuccess(t *testing.T) {
	_, tt := newRows(t, dlSnapshot("歌 甲", download.StatusDone, dlChunk, dlChunk))
	if !shows(tt, "下载完成") {
		t.Fatalf("那一格没有写出「下载完成」，文字有 %q", tt.Texts())
	}
	txt, ok := tt.Find("下载完成")
	if !ok {
		t.Fatalf("找不到「下载完成」那几个字，文字有 %q", tt.Texts())
	}
	raw := hexColorOf(t, "#17c964")
	n := countPixelsIn(t, tt, int(txt.X)-2, int(txt.Y)-2,
		int(txt.X+txt.W)+2, int(txt.Y+txt.H)+2, raw)
	if n > 0 {
		t.Errorf("「下载完成」那几个字里有 %d 块是上游那个绿（#17c964，浅色下压在白底上"+
			"只有 2.19:1）——当字色要取压深过的那一档", n)
	}
}

// TestTheShortcutConflictTextIsReadable 断「撞了的那一格」里的字读得出来。
//
// ⚠️ **这一格是画在 `Background(t.Surface)` 上的**，而语义色那一档（压深到
// ≥4.5:1）原先只对着**页面底色**保证——于是浅色下原值 `#f31260` 压在那上面
// 只有 **3.77:1**（评审查出来过：那一格里的字是原值，一个字节都没压深）。
//
// 判据是**画面上那一格里的字**：量出它最重的那一块像素（字的芯，就是它本身的
// 颜色），再和那一格的底（`t.Surface`）算对比度。
func TestTheShortcutConflictTextIsReadable(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""), systemIsDark(dark))
			openSettingsTab(t, tt, "快捷键设置")

			// 造一个冲突：把「上一首」录成 ⌘P（和「播放/暂停」撞了）。
			clickCell(t, tt, "应用内快捷键：上一首")
			tt.Key(ui.Super, ui.KeyP)
			tt.Frame()
			if !tt.HasText("与「播放/暂停」冲突") {
				t.Fatalf("没造出冲突，文字有 %q", tt.Texts())
			}

			cell, ok := tt.Find("应用内快捷键：上一首")
			if !ok {
				t.Fatalf("找不到那一格，文字有 %q", tt.Texts())
			}
			th := BuildTheme(app.DefaultSettings(""), dark)
			// 那一格的底是 `t.Surface`（`shortcut_keys.go` 的 recorderCell）。
			bg := rgb(th.Surface)
			// 字在格的左边（右边那颗是「×」键，它的颜色不是危险色）。
			x0, x1 := int(cell.X)+9, int(cell.X)+9+45
			y0, y1 := int(cell.Y)+2, int(cell.Y+cell.H)-2
			got, ok := darkestIn(tt, x0, y0, x1, y1, bg)
			if !ok {
				t.Fatalf("那一格左边没有找到字（框 %v）", cell)
			}
			if r := ink.Ratio(toUIColor(got), toUIColor(bg)); r < 4.5 {
				t.Errorf("撞了的那一格里的字是 %v，压在格子的底 %v 上只有 %.2f:1，want ≥4.5——"+
					"「这一格和别的键撞了」是最该读出来的一句话", got, bg, r)
			}
		})
	}
}

// toUIColor 把画面上的颜色翻成 `ui.Color`（`ink.Ratio` 收那一种）。
func toUIColor(c color.RGBA) ui.Color {
	return ui.Color{R: c.R, G: c.G, B: c.B, A: c.A}
}

// darkestIn 找一块矩形里「离底色最远」的那个像素（字的芯）。
//
// ⚠️ **要的是最远的那一块，不是最暗的那一块**：字的笔画中间是它自己的颜色，
// 边上那一圈抗锯齿是它和底的混合——所以「离底最远」才是它本身。
func darkestIn(tt *ui.Tester, x0, y0, x1, y1 int, bg color.RGBA) (color.RGBA, bool) {
	img := tt.Image()
	best := bg
	dist := -1
	found := false
	for y := max(y0, 0); y < min(y1, img.Rect.Max.Y); y++ {
		for x := max(x0, 0); x < min(x1, img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			d := maxDiff(p, bg)
			if d > dist {
				dist, best, found = d, p, true
			}
		}
	}
	return best, found
}

// ─────────────────────────────────────────────────────────────────────────────
// F-24　抽屉空状态提示的末行不是孤字
// ─────────────────────────────────────────────────────────────────────────────

// TestTheQueueEmptyHintHasNoOrphan 钉住「队列抽屉那句提示的末行不是一个字」。
//
// 那一句在抽屉那 352 宽的内容区里折成两行，原来那一版是 31 个字——折完末行只剩
// 「来」一个字（中文孤字：行尾单字）。
//
// 判据：**末行的墨迹宽度 ≥ 两个字的宽度**（12px 的汉字约 11.5 像素宽）。
func TestTheQueueEmptyHintHasNoOrphan(t *testing.T) {
	b := newTestShell(t)
	b.s.drawerOpen = true
	// ⚠️ **要让抽屉滑到位**：Tester 的钟站着不动，不推它的话面板还在窗口外面
	//（它从右边滑进来），量墨迹量的是空的右半边。
	settleDrawer(t, b.s, b.tt)
	homeFrame(b.tt)

	lines := inkLines(t, b.tt, queueEmpty.Hint, rgb(BuildTheme(app.DefaultSettings(""), false).Background))
	if len(lines) < 2 {
		t.Fatalf("那句话没有折成两行（只有 %d 行墨），判据量不了末行", len(lines))
	}
	last := lines[len(lines)-1]
	if w := last.right - last.left; w < 20 {
		t.Errorf("那句话末行只有 %d 像素宽（约 %d 个字）——中文孤字，行尾单字应回避",
			w, w/11)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// F-26　三处 600 字重
// ─────────────────────────────────────────────────────────────────────────────

// TestNoWeightSixHundred 钉住「代码里没有 600 字重」。
//
// `DESIGN.md` §二 的刻度表里有 600 那一档，而**用途表里没有它的位置**——
// 600 是「无主」的：没有人能说出那三个数字为什么该是 600。500（`weightMedium`）
// 才是仓库里唯一被命名的字重。
//
// ⚠️ **扫源码而不是画面**：字重不是颜色，量像素量不出「这一行是 600 还是 500」
// （两者在 12px 下几乎一样）。这一族有先例（`playbar_type_test.go` 那几条）。
func TestNoWeightSixHundred(t *testing.T) {
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range matches {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败，这一就没法查了：%v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "FontWeight" || len(call.Args) != 1 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok {
				return true
			}
			if lit.Value == "600" {
				t.Errorf("%s: 字重 600 不在刻度的用途表里——用 `weightMedium`（500）",
					fset.Position(call.Pos()))
			}
			return true
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 量墨迹的那几个小工具
// ─────────────────────────────────────────────────────────────────────────────

// inkInRect 数一个矩形里「和底色差得够远」的像素。
//
// ⚠️ **阈值 12 是报告那一份量墨迹用的同一个数**：浅色下的 `t.Surface`
// （#f4f4f5）和页面底色（#ffffff）只差 11，按 12 算就当它们一样——而那一格
// 圆盘（头像原来那一枚）就是靠这一点才被算成「墨迹」。
func inkInRect(img *image.RGBA, r ui.Rect, bg color.RGBA) int {
	n := 0
	for y := max(int(r.Y), 0); y < min(int(r.Y+r.H), img.Rect.Max.Y); y++ {
		for x := max(int(r.X), 0); x < min(int(r.X+r.W), img.Rect.Max.X); x++ {
			if maxDiff(img.RGBAAt(x, y), bg) > 12 {
				n++
			}
		}
	}
	return n
}

// inkLine 是一行墨迹的左右边界和它占的行号。
type inkLine struct {
	left, right int
	y0, y1      int
}

// inkLines 把一段文字的墨迹按行分开。
//
// ⚠️ **行是按「竖着分开」的**：中间隔了两行以上的空白就是新的一行（行与行之间
// 本来就有半个字身的空，而同一行里汉字的笔画是连着的）。
//
// ⚠️ **不能按左右分**：折行之后第二行是**从左端重新开始**的，按左右分会把
// 两行并成一行（那是第一版栽的地方）。
func inkLines(t *testing.T, tt *ui.Tester, text string, bg color.RGBA) []inkLine {
	t.Helper()
	r, ok := tt.Find(text)
	if !ok {
		t.Fatalf("画面上找不到 %q，文字有 %q", text, tt.Texts())
	}
	img := tt.Image()
	// ⚠️ **就按元素自己的框扫，不往外扩**：往外扩两像素会把**上面那一句**
	// （空状态那个标题）的最后一行扫进来——那一行是居中的，边界和正文不一样，
	// 混进来以后「末行」就不是末行了。
	x0, x1 := max(int(r.X), 0), min(int(r.X+r.W), img.Rect.Max.X)
	y0, y1 := max(int(r.Y), 0), min(int(r.Y+r.H), img.Rect.Max.Y)

	// 每一行墨迹的左右边界。
	type row struct {
		y, left, right int
	}
	var rows []row
	for y := y0; y < y1; y++ {
		left, right := -1, -1
		for x := x0; x < x1; x++ {
			if maxDiff(img.RGBAAt(x, y), bg) > 12 {
				if left < 0 {
					left = x
				}
				right = x
			}
		}
		if left >= 0 {
			rows = append(rows, row{y: y, left: left, right: right})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	var out []inkLine
	cur := inkLine{left: rows[0].left, right: rows[0].right, y0: rows[0].y, y1: rows[0].y}
	prevY := rows[0].y
	for _, r := range rows[1:] {
		if r.y-prevY > 2 {
			out = append(out, cur)
			cur = inkLine{left: r.left, right: r.right, y0: r.y, y1: r.y}
		} else {
			if r.left < cur.left {
				cur.left = r.left
			}
			if r.right > cur.right {
				cur.right = r.right
			}
			cur.y1 = r.y
		}
		prevY = r.y
	}
	out = append(out, cur)
	return out
}

// inkTailRows 数「最后一行最右那八列」上有几行墨。
//
// ⚠️ **数的是「行」，不是「有几行墨迹延伸到这里」**：省略号（…）是**基线上三个
// 点**（实测：只有两行像素），而一个汉字的笔画铺满整个字身（实测：十一行）。
// 所以那一列只有两行墨 = 那一行末尾是省略号 = 那句话被截断了。
func inkTailRows(t *testing.T, tt *ui.Tester, text string, bg color.RGBA) int {
	t.Helper()
	lines := inkLines(t, tt, text, bg)
	if len(lines) == 0 {
		t.Fatalf("画面上找不到 %q 的墨迹，文字有 %q", text, tt.Texts())
	}
	last := lines[len(lines)-1]
	r, ok := tt.Find(text)
	if !ok {
		t.Fatalf("画面上找不到 %q，文字有 %q", text, tt.Texts())
	}
	img := tt.Image()
	x0 := max(int(r.X), last.right-7)
	x1 := min(last.right+1, int(r.X+r.W))
	rows := 0
	for y := last.y0; y <= last.y1; y++ {
		for x := x0; x < x1; x++ {
			if maxDiff(img.RGBAAt(x, y), bg) > 12 {
				rows++
				break
			}
		}
	}
	return rows
}
