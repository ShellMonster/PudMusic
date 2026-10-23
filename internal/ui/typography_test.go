package ui

// 排版刻度那一族（票 #166）：页标题 24/400、设置页 20/400 + 16/500 + 14、
// 分组间距 24，以及 500 这一档字重本身画得出来。
//
// 数值的唯一目标值是仓库根的 DESIGN.md（`typography` 与 `spacing` 两组令牌）。
//
// # 判据为什么一半是 AST、一半是像素
//
// 字号在画面上量得准（文字盒子的高度是字号 × 行高，行高在同一套字体里是常数），
// 所以字号走像素。**字重在画面上量不到盒子**——400 和 500 的行高一样，只有笔画
// 粗细不同——所以「哪一句用哪一档字重」只能由代码钉住（AST）；而「500 这一档
// 真的画得比 400 粗」由下面那条墨量测试单独证明。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"image"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// textBoxPerSize 是「一行字画出来多高」与字号的比。
//
// 量出来的：30px 一行 35.3（`TestTheCollectionTitleIsTheBigOne`）、15px 一行
// 17.65（`TestEveryLoadingStateIsDrawnTheSameWay`），两者都是 1.1767。
const textBoxPerSize = 1.1767

// titleSizes 是**页标题**用的那几个字号：字重**一个字都不许写**。
//
// ⚠️ **页标题只有两个字号**：页标题 24（`pageTitleSize`）、空状态那一页的页面名
// 20（`emptyPageTitle`）。
//
// 此前这一条只钉了第一个——于是搜索页、收藏夹页、迷你播放器那几处的粗体一路漏到
// 了今天（UI 报告 §2 的 F-07：21 处 `.Bold()` 不在刻度上）。**这条测试的覆盖面
// 就是在这儿补上的**（空状态那一页是第二档）。
//
// ⚠️ **播放器那一页的歌名（`playerTitleSize`）不在这张表里**：那一档是**歌名**，
// 不是页标题，而票 #169 的 `TestTheFullPlayerTitleIsNotBold` 钉的是**满屏那一处
// 要写 `.FontWeight(400)`**——两者的判据不是同一条（页标题一个字重都不写，
// 歌名那一处写出来）。迷你播放器那一处（`nowplaying.go`）由
// `TestOnlyTheScaleIsBold` 盖住——**任何一处 `.Bold()` 都不许有**，它管全局。
var titleSizes = []string{"pageTitleSize", "emptyPageTitle"}

// stmtSpans 是这个文件里所有语句连同它们的范围。
type stmtSpans struct {
	stmts []ast.Stmt
	lo    []int
	hi    []int
}

// collectStmts 收下这个文件里所有的语句。
func collectStmts(fset *token.FileSet, f *ast.File) stmtSpans {
	var sp stmtSpans
	ast.Inspect(f, func(n ast.Node) bool {
		s, ok := n.(ast.Stmt)
		if !ok {
			return true
		}
		sp.stmts = append(sp.stmts, s)
		sp.lo = append(sp.lo, fset.Position(s.Pos()).Offset)
		sp.hi = append(sp.hi, fset.Position(s.End()).Offset)
		return true
	})
	return sp
}

// innermost 找出罩着 offset 那个位置的**最里层**的那一句（范围最小的那一句）。
//
// ⚠️ **为什么要「最里层」**：view 的函数体外面通常还包着一句
// `ui.Column(c)…Children(func(){…})`——而标题那句就在那个函数体里，于是外面
// 那一句也「提到了标题」。拿外面那一句当判据的话，同一处粗体会被报两次
// （一次报在函数体那一行，一次报在外层那一行）。
//
// 没有罩着它的句子就返回 -1。
func (sp stmtSpans) innermost(offset int) int {
	best := -1
	for i := range sp.stmts {
		if offset < sp.lo[i] || offset >= sp.hi[i] {
			continue
		}
		if best < 0 || sp.hi[i]-sp.lo[i] < sp.hi[best]-sp.lo[best] {
			best = i
		}
	}
	return best
}

// TestPageTitlesAreNotBold 钉住「标题的字重是 400」。
//
// 上游的 `h1` 只写了 `text-2xl`，字重被 Tailwind 的 Preflight 重置成 inherit，
// 也就是正文那一档——**不是加粗**（DESIGN.md 的 `typography.page_title`）。
// 我们此前八处页标题全是 `.Bold()`（20/700），层次靠重量不靠字号。
//
// 判据是 AST：凡是拿**标题那几个字号**当字号的那一句，里面不许出现 `Bold()` 或
// `FontWeight(...)`。字号的精确值由 `TestEveryPageTitleUsesTheSharedSize` 钉。
//
// ⚠️ **字重不许写，连 `FontWeight(400)` 也不许**：标题的层次靠字号拉开，
// 不靠重量——把 400 写出来只是多一处会漂的地方（它和「不写」画出来一样，
// 但两处写法会各自漂开）。
func TestPageTitlesAreNotBold(t *testing.T) {
	fset := token.NewFileSet()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range matches {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败：%v", name, err)
		}
		sp := collectStmts(fset, f)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			heavy := ""
			switch callName(call) {
			case "Bold":
				heavy = "Bold()"
			case "FontWeight":
				heavy = "FontWeight(...)"
			default:
				return true
			}
			pos := fset.Position(call.Pos())
			best := sp.innermost(pos.Offset)
			if best < 0 {
				return true
			}
			stmt := sp.stmts[best]
			hasTitle := false
			ast.Inspect(stmt, func(m ast.Node) bool {
				c, ok := m.(*ast.CallExpr)
				if !ok || callName(c) != "FontSize" || len(c.Args) != 1 {
					return true
				}
				for _, size := range titleSizes {
					if mentionsIdent(c.Args[0], size) {
						hasTitle = true
					}
				}
				return true
			})
			if hasTitle {
				where := fset.Position(stmt.Pos())
				t.Errorf("%s:%d 的标题调了 %s——标题是 400，字重被上游的"+
					" Preflight 重置成 inherit，不是加粗", where.Filename, where.Line, heavy)
			}
			return true
		})
	}
}

// boldOnScale 是**唯一**允许粗的那几处。
//
// DESIGN.md §二 的字重刻度里，每一条用途都标了字重（页标题 400、设置行标题
// 500、正文 400……），粗体只有三处写在表里（品牌字、收藏夹那一页头图区的大标题、
// 404 那个数字），另有一处是上游原样抄下来的（歌词浮层里那两个读数）。
//
// 每一条都写着**为什么它可以是粗的**——刻度上没写的事不许悄悄留着。
var boldOnScale = []struct {
	file string
	// mark 是认出它的那一句：文件里提到它的那一句才算这一处。
	mark string
	// count 是这一处有几处（歌词浮层那两个读数是两处）。
	count int
	why   string
}{
	{
		file: "collectionpage.go", mark: "collectionTitleSize", count: 1,
		why: "收藏夹 / 合集 / 系列那一页头图区的大标题：上游 `text-3xl`（30）就是粗体" +
			"（pagerhythm.go 里写着为什么）",
	},
	{
		file: "sidebar.go", mark: "sideBrandNameSize", count: 1,
		why: "品牌字（应用名）：DESIGN.md §二「品牌字 24 / 粗体 / 主色」",
	},
	{
		file: "notfound.go", mark: "404", count: 1,
		why: "404 那个数字：它是那一页上最大的一个字",
	},
	{
		file: "lyrics_knobs.go", mark: "lyricValueSize", count: 2,
		why: "歌词浮层里那两个读数：上游那两个 control 是 `text-[10px] font-bold`",
	},
}

// TestOnlyTheScaleIsBold 钉住「粗体只出现在刻度写明的那几处」。
//
// 此前代码里有 **21 处 `.Bold()`**（= 700），其中约 20 处落在「按 DESIGN 应该是
// 400/500」的位置上——页面标题、分组标题、弹窗标题、空状态标题、列表里的名字
// 全是 700（UI 报告 §2 的 F-07）。
//
// 判据是 AST：扫 `internal/ui` 下每一个非测试文件，找 `.Bold()` 和
// `FontWeight(700)`；每一处都必须落在 `boldOnScale` 那张表里，而表里的每一处
// 都写着**为什么它可以是粗的**。
//
// ⚠️ **这张表是「白名单」，不是「黑名单」**：新增一处粗体要往表里加一条并写清
// 理由——这样「为什么这一处可以粗」不会随着时间烂掉（注释说谎比没有注释更坏）。
func TestOnlyTheScaleIsBold(t *testing.T) {
	fset := token.NewFileSet()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	counts := make([]int, len(boldOnScale))
	for _, name := range matches {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败：%v", name, err)
		}

		sp := collectStmts(fset, f)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isHeavyWeight(call) {
				return true
			}
			pos := fset.Position(call.Pos())
			// 最里层的那一句 = 范围最小的那一句。
			best := sp.innermost(pos.Offset)
			hit := -1
			if best >= 0 {
				for i, a := range boldOnScale {
					if a.file != name {
						continue
					}
					if mentionsMark(sp.stmts[best], a.mark) {
						hit = i
						break
					}
				}
			}
			if hit < 0 {
				t.Errorf("%s:%d 调了重字重——粗体只出现在刻度写明的那几处"+
					"（DESIGN.md §二；UI 报告 §2 的 F-07）", pos.Filename, pos.Line)
				return true
			}
			counts[hit]++
			return true
		})
	}

	// 表里的每一处都还要在（别留下写空了却还占着位的条目）。
	for i, a := range boldOnScale {
		if counts[i] != a.count {
			t.Errorf("%s 里刻度写明的粗体有 %d 处，want %d——%s",
				a.file, counts[i], a.count, a.why)
		}
	}
}

// isHeavyWeight 报这一次调用是不是「把字重调到 700」。
func isHeavyWeight(call *ast.CallExpr) bool {
	switch callName(call) {
	case "Bold":
		return true
	case "FontWeight":
		return len(call.Args) == 1 && mentionsFloat(call.Args[0], 700)
	}
	return false
}

// mentionsMark 报这一句里有没有那个标识符（或是那个字符串字面量）。
//
// ⚠️ **字符串字面量也算**：404 那个数字是 `ui.Text(c, "404")`——它没有名字，
// 只有那句话本身。
func mentionsMark(stmt ast.Stmt, mark string) bool {
	found := false
	ast.Inspect(stmt, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Ident:
			if v.Name == mark {
				found = true
			}
		case *ast.BasicLit:
			if v.Kind == token.STRING && strings.Trim(v.Value, `"`) == mark {
				found = true
			}
		}
		return true
	})
	return found
}

// mentionsIdent 报这个表达式里有没有那个标识符（往调用里找一层，
// `scale.Font(c, pageTitleSize)` 中间还隔着一个调用）。
func mentionsIdent(e ast.Expr, name string) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name == name
	case *ast.CallExpr:
		for _, a := range v.Args {
			if mentionsIdent(a, name) {
				return true
			}
		}
	}
	return false
}

// TestTheSettingsPageUsesTheTypographicScale 钉住设置页那四档字号。
//
// 上游那一页的刻度（DESIGN.md 的 `typography`）：
//
//	分组标题 20/400（`h2` = `text-xl`）
//	行标题   16/500（`font-medium`）
//	说明     14/400（`text-sm`）
//
// 此前这里整页是 16/700、14/700、12——每一档都比上游小两号、粗两档。
//
// 判据是画面上的文字盒高（字号 × `textBoxPerSize`），深浅两套都量：深色下这
// 三行字离得更近，字号差一档看得出来，字重差一档却看不出来——字重由
// `TestTheSettingsRowTitleUsesTheMediumWeight`（AST）钉。
func TestTheSettingsPageUsesTheTypographicScale(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, systemIsDark(dark))
			b.s.navigate(settingsPath)
			b.tt.Frame()

			for _, want := range []struct {
				text string
				size float32
				what string
				// padBottom 是这一行字自己带的下内边距——`Find` 给回来的是整个
				// 元素盒子，分组标题自己垫了 4（标题与第一行之间的那一点空）。
				padBottom float32
			}{
				{text: "外观", size: 20, what: "分组标题", padBottom: 4},
				{text: "主题", size: 16, what: "行标题"},
				{text: "选择浅色或深色主题", size: 14, what: "说明"},
			} {
				r, ok := b.tt.Find(want.text)
				if !ok {
					t.Fatalf("设置页上找不到%s「%s」，文字有 %q", want.what, want.text, b.tt.Texts())
				}
				got := (r.H - want.padBottom) / textBoxPerSize
				if math.Abs(float64(got-want.size)) > 0.8 {
					t.Errorf("设置页%s「%s」折算出来是 %.1fpx，want %.0f——"+
						"设置页的三档字号是 20 / 16 / 14（DESIGN.md 的 typography）",
						want.what, want.text, got, want.size)
				}
			}
		})
	}
}

// TestTheMenuGroupNameFitsOnOneLine 钉住「菜单那一页的组名不折行」。
//
// 那一格是「左边一列 100 宽的组名 + 右边一排勾选框」（上游 `space-x-[100px]`）。
// 组名「系统默认菜单」六个汉字在 16px 下是 96 宽，正好放得下；跟着分组标题
// 改成 20px 的话是 120 宽，会折成「系统默认菜 / 单」（出图量过）。**它是一列
// 的组标签，不是整页的 `h2`**——上游那里给的是 `text-medium`。
func TestTheMenuGroupNameFitsOnOneLine(t *testing.T) {
	b := newTestShell(t)
	s, tt := b.s, b.tt
	s.navigate(settingsPath)
	tt.Frame()
	openSettingsTab(t, tt, "菜单设置")

	r, ok := tt.Find(menuGroupName)
	if !ok {
		t.Fatalf("菜单那一页上找不到组名「%s」，文字有 %q", menuGroupName, tt.Texts())
	}
	// 一行 16px 约 18.8 高；折成两行就是 37.7。
	if oneLine := float32(16 * textBoxPerSize); r.H > oneLine+2 {
		t.Errorf("组名「%s」画出来 %.1f 高（一行约 %.1f）——折行了：100 的位宽"+
			"放不下 20px 的六个汉字", menuGroupName, r.H, oneLine)
	}
}

// TestTheSettingsRowTitleUsesTheMediumWeight 钉住「行标题用的是 500 那一档」。
//
// 字重画不出盒子的差别（`Find` 拿到的矩形只跟字号和行高有关），所以这一条只能
// 钉代码：`settingRow` 画标题的那一句同时要有 16 的字号和 `weightMedium`。
func TestTheSettingsRowTitleUsesTheMediumWeight(t *testing.T) {
	if weightMedium != 500 {
		t.Errorf("weightMedium = %d，want 500——上游的 `font-medium` 就是 500，"+
			"它是一档正经的字重，不是「400 和 600 之间随便选」", weightMedium)
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "settings_page.go", nil, 0)
	if err != nil {
		t.Fatalf("解析 settings_page.go 失败：%v", err)
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		stmt, ok := n.(*ast.ExprStmt)
		if !ok {
			return true
		}
		has16, hasMedium := false, false
		ast.Inspect(stmt, func(m ast.Node) bool {
			call, ok := m.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch callName(call) {
			case "FontSize":
				if len(call.Args) == 1 && mentionsFloat(call.Args[0], 16) {
					has16 = true
				}
			case "FontWeight":
				if len(call.Args) == 1 && mentionsIdent(call.Args[0], "weightMedium") {
					hasMedium = true
				}
			}
			return true
		})
		if has16 && hasMedium {
			found = true
		}
		return true
	})
	if !found {
		t.Error("settings_page.go 里没有一句同时写着 `FontSize(…16…)` 和 " +
			"`FontWeight(weightMedium)`——设置页的行标题必须是 16/500")
	}
}

// mentionsFloat 报这个表达式里有没有那个数字（`scale.Font(c, 16)` 的 16）。
func mentionsFloat(e ast.Expr, v float64) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.INT {
			if got, err := strconv.Atoi(lit.Value); err == nil && float64(got) == v {
				found = true
			}
		}
		return true
	})
	return found
}

// TestTheSettingsGroupsAre24Apart 钉住「设置分组之间是 24」。
//
// 票 #166 之前是两个数：Tab 内容那一层 18、页头那一层 16。DESIGN.md 只有一个
// `spacing.settings_group_gap` = 24。判据是代码：两处 `Gap` 用的都是同一个常量，
// 常量本身是 24——写成字面量的话，下一票改一处、漏一处，两个数又漂开。
func TestTheSettingsGroupsAre24Apart(t *testing.T) {
	if settingsGroupGap != 24 {
		t.Errorf("settingsGroupGap = %d，want 24（DESIGN.md 的 "+
			"`spacing.settings_group_gap`）", settingsGroupGap)
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "settings_page.go", nil, 0)
	if err != nil {
		t.Fatalf("解析 settings_page.go 失败：%v", err)
	}
	n := 0
	ast.Inspect(f, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if callName(call) == "Gap" && len(call.Args) == 1 &&
			mentionsIdent(call.Args[0], "settingsGroupGap") {
			n++
		}
		return true
	})
	if n < 2 {
		t.Errorf("设置页只有 %d 处 `Gap(settingsGroupGap)`，want 2（Tab 内容那一层"+
			"和页头那一层）——分组间距是两个数的话，改一处、漏一处就又漂开了", n)
	}
}

// TestTheMediumWeightIsDrawnHeavierThanNormal 证明 500 这一档**画得出来**。
//
// # 为什么要单独画一遍
//
// 设置页的 500 只出现在行标题上，而同页上没有同字号的 400 可以比——所以「行标题
// 用了 500」在画面上看不出来。这一条把同一句话按 400 / 500 / 700 画三遍，数每
// 一行的墨量：**墨量随字重单调变多**，500 比 400 多出可辨的一截。
//
// 若框架把 500 悄悄吸到 400（比如只在离散的几档里挑最近的一个），400 和 500 的
// 墨量会一模一样，这一条就红——而那正是「补 500 字重这一档」要防的事。
//
// ⚠️ 量的不是 `Find` 的盒子：三行字一样大，盒子一样高；差别只在笔画上。
func TestTheMediumWeightIsDrawnHeavierThanNormal(t *testing.T) {
	const winW, winH = 520, 400
	b := newTestShell(t, at(winW, winH), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			ApplyTheme(c, app.DefaultSettings(""), false)
			mygoui.Column(c).Padding(20).Gap(24).Children(func() {
				mygoui.Text(c, "肥瘦对比").FontSize(48).FontWeight(400)
				mygoui.Text(c, "肥瘦对比").FontSize(48).FontWeight(weightMedium)
				mygoui.Text(c, "肥瘦对比").FontSize(48).FontWeight(700)
			})
		}
	}))
	tt := b.tt
	tt.Frame()

	img := tt.Image()
	area := image.Rect(0, 0, winW, winH)
	br, bg, bb, ba := pageBackground(img, area)

	// 三行字之间隔着 Gap 24，按连续有墨的行切得开。
	var bands [][2]int
	for _, y := range inkRows(img, area) {
		if n := len(bands); n > 0 && y <= bands[n-1][1]+1 {
			bands[n-1][1] = y
			continue
		}
		bands = append(bands, [2]int{y, y})
	}
	if len(bands) != 3 {
		t.Fatalf("画面上有 %d 段墨，want 3（400 / 500 / 700 三行）——文字有 %q",
			len(bands), tt.Texts())
	}

	ink := func(band [2]int) int {
		n := 0
		for y := band[0]; y <= band[1]; y++ {
			for x := area.Min.X; x < area.Max.X; x++ {
				c := img.RGBAAt(x, y)
				if c.R != br || c.G != bg || c.B != bb || c.A != ba {
					n++
				}
			}
		}
		return n
	}
	normal, medium, heavy := ink(bands[0]), ink(bands[1]), ink(bands[2])
	t.Logf("墨量：400 = %d，500 = %d，700 = %d", normal, medium, heavy)

	if medium <= normal {
		t.Errorf("500 的墨量 %d 不比 400 的 %d 多——框架把 500 画成了 400，"+
			"这一档字重是假的", medium, normal)
	}
	if medium*100 < normal*105 {
		t.Errorf("500 的墨量只比 400 多 %.1f%%（%d vs %d）——两档几乎画成同一个"+
			"粗细，看不出「中等字重」", float64(medium-normal)*100/float64(normal),
			medium, normal)
	}
	if heavy <= medium {
		t.Errorf("700 的墨量 %d 不比 500 的 %d 多——500 已经是图表里最粗的那一档，"+
			"「补一档 500」就没有意义", heavy, medium)
	}
}

// TestPagesStartAtTheTop 钉住「页面根没有上内边距」（票 #166 的 G40）。
//
// 上游的页容器只有 `px-4`（16），**没有 `py`**。我们此前每一页顶上多留了
// 16~28（首页 / 下载记录 20、稍后再看 / 历史记录 / 本地音乐 16、搜索 28、
// 设置 28、登录 20），于是整块内容比上游低一截。
//
// 判据是画面：内容区里**第一段墨**从顶栏下缘往下多远。上边距收成 0 之后，那
// 只有字形自己的上沿留白（2~5 像素）；留 16 或 20 的话，这一段会明显低下去。
func TestPagesStartAtTheTop(t *testing.T) {
	sideW := app.DefaultSideMenuWidth
	area := contentArea(sideW)
	for _, page := range []struct {
		name string
		path string
		opts []shellOpt
	}{
		{name: "首页", path: homePath},
		{name: "稍后再看", path: laterPath, opts: libraryPageOpts(t)},
		{name: "历史记录", path: historyPath, opts: libraryPageOpts(t)},
		{name: "本地音乐", path: localMusicPath},
		{name: "下载记录", path: downloadListPath},
		{name: "搜索", path: searchPath + "?q=%E5%A4%9C%E7%A9%BA"},
		{name: "设置", path: settingsPath},
		{name: "扫码登录", path: loginPath},
	} {
		t.Run(page.name, func(t *testing.T) {
			b := newTestShell(t, page.opts...)
			s, tt := b.s, b.tt
			s.navigate(page.path)
			tt.Frame()

			lo, _, ok := firstBand(tt.Image(), area)
			if !ok {
				t.Fatalf("%s：内容区顶上一点墨都没有，文字有 %q", page.name, tt.Texts())
			}
			if top := lo - navMinHeight; top > 8 {
				t.Errorf("%s 的第一段墨从顶栏下缘往下 %d 像素开始——页面根不许有"+
					"上内边距（上游只有 `px-4`、没有 `py`），want ≤ 8", page.name, top)
			}
		})
	}
}
