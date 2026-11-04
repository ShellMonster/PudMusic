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
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/leading"
	"go/ast"
	"go/parser"
	"go/token"
	"image"
	"io/fs"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// textBoxPerSize 是「一行字画出来多高」与字号的比。
//
// 量出来的：30px 一行 35.3（`TestTheCollectionTitleIsTheBigOne`）、15px 一行
// 17.65（`TestEveryLoadingStateIsDrawnTheSameWay`），两者都是 1.1767。
const textBoxPerSize = 1.1767

// titleSizes 是**页标题**用的那几个字号：字重**一个字都不许写**。
//
// ⚠️ **页标题只有两个字号**：页标题 48（`scale.PageTitle`，宋体 700 走
// `typo.Display`）、空状态那一页的页面名 20（`scale.EmptyPageTitle`）。
//
// 此前这一条只钉了第一个——于是搜索页、收藏夹页、迷你播放器那几处的粗体一路漏到
// 了今天（UI 报告 §2 的 F-07：21 处 `.Bold()` 不在刻度上）。**这条测试的覆盖面
// 就是在这儿补上的**（空状态那一页是第二档）。
//
// ⚠️ **播放器那一页的歌名（`scale.PlayerTitle`）不在这张表里**：那一档是**歌名**，
// 不是页标题，而票 #169 的 `TestTheFullPlayerTitleIsNotBold` 钉的是**满屏那一处
// 要写 `.FontWeight(400)`**——两者的判据不是同一条（页标题一个字重都不写，
// 歌名那一处写出来）。迷你播放器那一处（`nowplaying.go`）由
// `TestOnlyTheScaleIsBold` 盖住——**任何一处 `.Bold()` 都不许有**，它管全局。
var titleSizes = []string{"PageTitle", "EmptyPageTitle"}

// stmtSpans 是这个文件里所有语句连同它们的范围。
//
// ⚠️ **元素是 `ast.Node` 而不是 `ast.Stmt`**：包级的常量声明（`ast.GenDecl`）
// 不是语句，而「一处粗体」的判据常常就在那种声明里。
type stmtSpans struct {
	stmts []ast.Node
	lo    []int
	hi    []int
}

// collectStmts 收下这个文件里所有的语句。
//
// ⚠️ **常量声明也算**：它不是 `ast.Stmt`（是 `ast.Decl`），而「一处粗体」的判据
// 常常就在那种声明里（`lyricsActiveWeight = 800`）——只收语句的话，那一处就
// 没有「它所在的那一句」可查，白名单认不出它。
func collectStmts(fset *token.FileSet, f *ast.File) stmtSpans {
	var sp stmtSpans
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case ast.Stmt:
			sp.stmts = append(sp.stmts, v)
		case *ast.GenDecl:
			sp.stmts = append(sp.stmts, v)
		default:
			return true
		}
		sp.lo = append(sp.lo, fset.Position(n.Pos()).Offset)
		sp.hi = append(sp.hi, fset.Position(n.End()).Offset)
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

// TestPageTitlesAreDisplayBold 钉住「深夜唱片行」的页标题口径（DESIGN.md §0′，
// 2026-11 用户拍板）：**48 号、宋体 700**，一律走 `typo.Display`。
//
// 两条判据，都是 AST：
//
//  1. 用**页标题字号**（`scale.PageTitle`）的那一句，必须是 `typo.Display(…)`
//     ——宋体和 700 都是它设的，别处各写各的就会漂；
//  2. 用页标题那几个字号的那一句里**不许再写** `Bold()` / `FontWeight(...)`：
//     字重由 `Display` 定，多写一处就是两处会各自漂开的地方。
//
// （旧口径「页标题 24/400、不加粗」随上游 Biu 那一套一起作废。）
func TestPageTitlesAreDisplayBold(t *testing.T) {
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
		ast.Inspect(f, func(n ast.Node) bool {
			stmt, ok := n.(ast.Stmt)
			if !ok {
				return true
			}
			if _, isExpr := stmt.(*ast.ExprStmt); !isExpr {
				if _, isAssign := stmt.(*ast.AssignStmt); !isAssign {
					return true
				}
			}
			usesPage, inDisplay := false, false
			ast.Inspect(stmt, func(m ast.Node) bool {
				c, ok := m.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch callName(c) {
				case "Display":
					if len(c.Args) == 2 && mentionsIdent(c.Args[1], "PageTitle") {
						usesPage, inDisplay = true, true
					}
				case "FontSize":
					if len(c.Args) == 1 && mentionsIdent(c.Args[0], "PageTitle") {
						usesPage = true
					}
				}
				return true
			})
			if usesPage && !inDisplay {
				where := fset.Position(stmt.Pos())
				t.Errorf("%s:%d 用了页标题字号但没走 `typo.Display`——页标题是 48 宋体 700",
					where.Filename, where.Line)
			}
			return true
		})
	}
}

// TestPageTitlesSetNoWeightOfTheirOwn 是上面那条的第 2 条判据：页标题那几个字号的
// 那一句里不许再写 `Bold()` / `FontWeight(...)`（字重归 `typo.Display`）。
func TestPageTitlesSetNoWeightOfTheirOwn(t *testing.T) {
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
				t.Errorf("%s:%d 的页标题自己写了 %s——字重由 typo.Display 定（宋体 700），"+
					"别再写第二处", where.Filename, where.Line, heavy)
			}
			return true
		})
	}
}

// prototypeWeights 是原型（`index.html` 的 `font-weight`、`app.js` 的行内样式）里实际出现的字重。
//
// 原型共用到 400 / 500 / 600 / 700 四档，没有 800。旧护栏把 600 当「无主」、把 700 限在七处白名单里，
// 和原型广泛使用 600 / 700 冲突，已经取消（DESIGN.md §0′）。
var prototypeWeights = map[int]bool{400: true, 500: true, 600: true, 700: true}

// TestOnlyThePrototypeWeightsAreWritten 钉住「代码里写的字重都在原型的那四档里」。
//
// 取代了 `TestNoWeightSixHundred`（禁 600）和 `TestOnlyTheScaleIsBold`（700 只许出现在白名单的七处）。
// 这两条钉的是旧刻度，和原型冲突：原型的 `.badge`、`.btn`、`.h-page` 等大量用 600 / 700。
//
// # 判据是 AST，扫 `internal/ui` 下每一个非测试文件（递归）
//
//	B. `FontWeight(<字面量>)`，字面量不在 {400, 500, 600, 700}
//	C. 调用点：字面量传给一个名字带 `weight` 的参数，不在那四档
//	D. 本包里声明、名字带 `weight` 的常量 / 变量，字面量不在那四档
//
// `.Bold()` 是 700，在集合里，所以放行。⚠️ 扫的字重数量为 0 说明判据没扫到东西，直接红。
func TestOnlyThePrototypeWeightsAreWritten(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := loadPackages(fset, ".")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]int{}
	for _, pkg := range pkgs {
		for _, f := range pkg.files {
			for _, site := range weightSites(f, pkg) {
				seen[site.value]++
				if !prototypeWeights[site.value] {
					t.Errorf("%s: %s——原型只用 400 / 500 / 600 / 700（DESIGN.md §0′）",
						fset.Position(site.pos), site.what)
				}
			}
		}
	}
	// 判据自己得扫得到东西：500 是设置行标题的字重，每一版都在用。
	if seen[500] == 0 {
		t.Fatalf("一处 500 都没扫到（扫到的字重 %v）——判据没有扫到字重写法，等于没钉", seen)
	}
}

// pkgFacts 是一个包（= `internal/ui` 下的一个目录）里，判据要用的那些事实。
type pkgFacts struct {
	// files 是这个包里那些非测试文件。
	files []*ast.File
	// paths 是那些文件相对 `internal/ui` 的路径（白名单按它索引）。
	paths []string
	// params 是这个包里那些函数 / 方法的参数名（按名字索引）。
	params map[string][]string
}

// loadPackages 收下 `internal/ui` 下所有的包（按目录分组）。
//
// ⚠️ **递归**：子包（`widgets/**`、`guard/**`、`iconset/**`）也在内——粗体漏到
// 子包里的话，界面上一样看得出来，而顶层扫不到它。
func loadPackages(fset *token.FileSet, root string) ([]*pkgFacts, error) {
	byDir := map[string]*pkgFacts{}
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		dir := filepath.Dir(path)
		pkg, ok := byDir[dir]
		if !ok {
			pkg = &pkgFacts{params: map[string][]string{}}
			byDir[dir] = pkg
			dirs = append(dirs, dir)
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		pkg.files = append(pkg.files, f)
		pkg.paths = append(pkg.paths, filepath.ToSlash(path))
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 收每个包那些函数 / 方法的参数名。
	for _, dir := range dirs {
		pkg := byDir[dir]
		for _, f := range pkg.files {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				pkg.params[fn.Name.Name] = paramNames(fn.Type)
			}
		}
	}

	out := make([]*pkgFacts, 0, len(dirs))
	for _, dir := range dirs {
		out = append(out, byDir[dir])
	}
	return out, nil
}

// calleeParams 报这一次调用对应的是哪个本地函数 / 方法，以及它的参数名。
//
// 返回 (nil, nil) 表示「不是本包的函数 / 方法」（比如框架的 `ui.Text`）。
func (p *pkgFacts) calleeParams(call *ast.CallExpr) ([]ast.Expr, []string) {
	var name string
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		name = fn.Name
	case *ast.SelectorExpr:
		name = fn.Sel.Name
	default:
		return nil, nil
	}
	params, ok := p.params[name]
	if !ok {
		return nil, nil
	}
	return call.Args, params
}

// paramNames 报一个函数的参数名（方法的接收者不算）。
func paramNames(ft *ast.FuncType) []string {
	var out []string
	if ft == nil || ft.Params == nil {
		return out
	}
	for _, f := range ft.Params.List {
		for _, n := range f.Names {
			out = append(out, n.Name)
		}
	}
	return out
}

// weightSite 是「一处写了字重」的地方。
type weightSite struct {
	// pos 是它的位置。
	pos token.Pos
	// value 是字面量写的字重。
	value int
	// what 是它是什么（写进报错里的那句话）。
	what string
}

// weightSites 找出这个文件里所有写了字重字面量的地方（判据的三样见
// `TestOnlyThePrototypeWeightsAreWritten`）。
func weightSites(f *ast.File, pkg *pkgFacts) []weightSite {
	var out []weightSite
	intLit := func(e ast.Expr) (int, string, bool) {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.INT {
			return 0, "", false
		}
		n, err := strconv.Atoi(lit.Value)
		return n, lit.Value, err == nil
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.CallExpr:
			// B：`FontWeight(<字面量>)`
			if callName(v) == "FontWeight" && len(v.Args) == 1 {
				if n, raw, ok := intLit(v.Args[0]); ok {
					out = append(out, weightSite{pos: v.Pos(), value: n, what: "把字重调成了 " + raw})
				}
			}
			// C：调用点——字面量传给一个名字带 weight 的参数
			if args, params := pkg.calleeParams(v); args != nil {
				for i, a := range args {
					n, raw, ok := intLit(a)
					if !ok || i >= len(params) || !strings.Contains(strings.ToLower(params[i]), "weight") {
						continue
					}
					out = append(out, weightSite{pos: v.Pos(), value: n, what: "把 " + raw + " 传给了一个字重参数"})
				}
			}
		case *ast.GenDecl:
			// D：名字带 weight 的常量 / 变量
			if v.Tok != token.CONST && v.Tok != token.VAR {
				return true
			}
			for _, spec := range v.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Values) != 1 || len(vs.Names) != 1 {
					continue
				}
				n, raw, ok := intLit(vs.Values[0])
				if !ok || !strings.Contains(strings.ToLower(vs.Names[0].Name), "weight") {
					continue
				}
				out = append(out, weightSite{pos: vs.Pos(), value: n,
					what: "声明了字重常量 " + vs.Names[0].Name + " = " + raw})
			}
		}
		return true
	})
	return out
}

// mentionsIdent 报这个表达式里有没有那个标识符（往调用里找一层，
// `scale.Font(c, scale.PageTitle)` 中间还隔着一个调用）。
//
// ⚠️ **包名限定的那种写法也算**（`scale.PageTitle`）：字号的出处现在是
// `widgets/scale` 那张表，代码里写的是 `scale.PageTitle`——那是一个
// `*ast.SelectorExpr`，不是 `*ast.Ident`。只认 `*ast.Ident` 的话，
// 这个判据会**静默地永远不成立**（它匹配不到任何东西，于是「没有加粗」
// 这一条对每一句都成立）。
func mentionsIdent(e ast.Expr, name string) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name == name
	case *ast.SelectorExpr:
		// `scale.PageTitle` → 认它的末段名（`PageTitle`）。
		return v.Sel.Name == name
	case *ast.CallExpr:
		for _, a := range v.Args {
			if mentionsIdent(a, name) {
				return true
			}
		}
	}
	return false
}

// TestTheSettingsPageUsesTheTypographicScale 钉住设置页的字号刻度（原型 CSS，index.html 498–504 行）：
//
//	分组标题 宋体 20（`.set-group h3`）
//	行标题   14 / 500（`.set-row .od-field strong`）
//	说明     13（`.set-row .od-field small`）
//
// 判据是画面上的文字盒高（字号 × `textBoxPerSize`），深浅两套都量。字重由
// `TestTheSettingsRowTitleUsesTheMediumWeight`（AST）钉。
func TestTheSettingsPageUsesTheTypographicScale(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, systemIsDark(dark))
			// 这几个字在常规页上各自唯一（「播放」「默认音质」「切换后会重新取流…」），
			// `Tester.Find` 拿的是第一个匹配。
			b.s.navigate(settingsPath)
			b.tt.Frame()
			for _, m := range []struct {
				text string
				size float32
				what string
			}{
				{"默认音质", 14, "行标题"},
				{"切换后会重新取流，并从当前位置继续播放", 13, "说明"},
			} {
				r, ok := b.tt.Find(m.text)
				if !ok {
					t.Fatalf("设置页上找不到%s「%s」，文字有 %q", m.what, m.text, b.tt.Texts())
				}
				// 行高是原型正文的 1.55（`leading.Body`），文字盒高 = 字号 × 1.55。
				got := r.H / leading.Body
				if math.Abs(float64(got-m.size)) > 0.8 {
					t.Errorf("%s「%s」折算出来是 %.1fpx，want %.0f——原型的刻度是 20 / 14 / 13",
						m.what, m.text, got, m.size)
				}
			}
			// 分组标题是宋体（Display），盒高随字体的行高走，不拿 `textBoxPerSize` 折算，
			// 只断言它比行标题大一档、而且不是 14。
			g, ok := b.tt.Find("播放")
			if !ok {
				t.Fatalf("设置页上找不到分组标题「播放」，文字有 %q", b.tt.Texts())
			}
			row, _ := b.tt.Find("默认音质")
			if g.H <= row.H+2 {
				t.Errorf("分组标题「播放」盒高 %.1f 不比行标题 %.1f 大——它该是 20 号宋体", g.H, row.H)
			}
		})
	}
}

// TestTheSettingsRowTitleUsesTheMediumWeight 钉住「行标题用的是 500 那一档」：字重画不出盒子的
// 差别，所以只能钉代码——`settings_widgets.go` 里画行标题的那一句同时要有 14 的字号和 500 的字重。
func TestTheSettingsRowTitleUsesTheMediumWeight(t *testing.T) {
	if weightMedium != 500 {
		t.Errorf("weightMedium = %d，want 500——`font-medium` 就是 500", weightMedium)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "settings_widgets.go", nil, 0)
	if err != nil {
		t.Fatalf("解析 settings_widgets.go 失败：%v", err)
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		stmt, ok := n.(*ast.ExprStmt)
		if !ok {
			return true
		}
		has14, hasMedium := false, false
		ast.Inspect(stmt, func(m ast.Node) bool {
			call, ok := m.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch callName(call) {
			case "FontSize":
				if len(call.Args) == 1 && mentionsIdent(call.Args[0], "Size14") {
					has14 = true
				}
			case "FontWeight":
				if len(call.Args) == 1 {
					if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Value == "500" {
						hasMedium = true
					}
				}
			}
			return true
		})
		if has14 && hasMedium {
			found = true
		}
		return true
	})
	if !found {
		t.Error("settings_widgets.go 里没有一句同时写着 `FontSize(…Size14…)` 和 `FontWeight(500)`——" +
			"设置页的行标题必须是 14/500")
	}
}

// TestTheSettingsGroupsAre40Apart 钉住「设置分组之间是 40」（原型 `.set-group { margin-bottom: 40px }`）：
// 常量本身是 40，而且 `setGroup` 里用的就是它——写成字面量的话，改一处漏一处两个数又漂开。
func TestTheSettingsGroupsAre40Apart(t *testing.T) {
	if setGroupGap != 40 {
		t.Errorf("setGroupGap = %d，want 40", setGroupGap)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "settings_widgets.go", nil, 0)
	if err != nil {
		t.Fatalf("解析 settings_widgets.go 失败：%v", err)
	}
	n := 0
	ast.Inspect(f, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if callName(call) == "Margin" {
			for _, a := range call.Args {
				if mentionsIdent(a, "setGroupGap") {
					n++
				}
			}
		}
		return true
	})
	if n < 1 {
		t.Errorf("`setGroup` 里没有用 `setGroupGap` 做下边距")
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

// TestPagesStartAtTheTop 钉住「页面顶上留白不多于原型的那一档」。
//
// 原型 `.page { padding: 8px 40px 64px }`、`.page-head { padding: 24px 0 8px }`（index.html
// 262 / 274 行）：内容顶上到标题块是 8 + 24 = 32，再加字形自己的上沿留白（Eyebrow 11 号约 3，
// 48 号标题约 14）。票 #166 的 G40 当年按上游「页面根没有 py」把它卡在 16，和原型冲突，改成原型的数。
//
// 判据是画面：内容区里**第一段墨**（含页头上面那行 Eyebrow）从顶栏下缘往下多远，≤ 40。
// ⚠️ **首页不在这一张表里**：原型首页顶上是 `padding-top: 8` + 问候区的 `padding-top: 24`，
// 由 `homepage_test.go` 的 `TestTheHomePageUsesThePrototypePageMargins` 钉。
func TestPagesStartAtTheTop(t *testing.T) {
	sideW := app.DefaultSideMenuWidth
	area := contentArea(sideW)
	for _, page := range []struct {
		name string
		path string
		opts []shellOpt
	}{
		{name: "稍后再看", path: laterPath, opts: libraryPageOpts(t)},
		{name: "历史记录", path: historyPath, opts: libraryPageOpts(t)},
		{name: "本地音乐", path: localMusicPath},
		{name: "下载记录", path: downloadListPath},
		{name: "搜索", path: searchPath + "?q=%E5%A4%9C%E7%A9%BA"},
		{name: "设置", path: settingsPath},
	} {
		// 登录页不在这里：原型的 `.login` 是整页居中的卡片（上下 48，垂直居中），不是页头那一族。
		t.Run(page.name, func(t *testing.T) {
			b := newTestShell(t, page.opts...)
			s, tt := b.s, b.tt
			s.navigate(page.path)
			tt.Frame()

			rows := inkRows(tt.Image(), area)
			if len(rows) == 0 {
				t.Fatalf("%s：内容区顶上一点墨都没有，文字有 %q", page.name, tt.Texts())
			}
			if top := rows[0] - navMinHeight; top > 40 {
				t.Errorf("%s 的第一段墨从顶栏下缘往下 %d 像素开始——原型的页面顶上是 8 + 24 加字形留白，"+
					"want ≤ 40", page.name, top)
			}
		})
	}
}
