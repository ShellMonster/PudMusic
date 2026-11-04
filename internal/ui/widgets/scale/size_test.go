package scale

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// 这一组是字号刻度表的**护栏**：表的每一档值不许漂，DESIGN.md 的令牌不许
// 对不上，界面层也不许再出现裸的字号数字。

// repoRoot 是仓库根（从本文件的路径往上四级）。
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("拿不到本测试文件的路径")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", ".."))
}

// TestTheSizeScaleValuesAreLocked 把每一档的字号钉死。
//
// ⚠️ **这张表是「值只写一处」的地方**：改了 `Size13` 的值，全应用 **61 处**
// 「13 号字」会一起变——那是这张表存在的意义，也是它危险的方。
// 所以每一档的值都在这里写死一遍：谁想改一个数，得先改这里、并且想清楚
// 那 61 处会跟着变。
func TestTheSizeScaleValuesAreLocked(t *testing.T) {
	want := map[string]int{
		"Size10": 10, "Size11": 11, "Size12": 12, "Size13": 13,
		"Size14": 14, "Size15": 15, "Size16": 16, "Size18": 18,
		"Size20": 20, "Size24": 24, "Size28": 28, "Size30": 30,
	}
	got := map[string]int{
		"Size10": Size10, "Size11": Size11, "Size12": Size12, "Size13": Size13,
		"Size14": Size14, "Size15": Size15, "Size16": Size16, "Size18": Size18,
		"Size20": Size20, "Size24": Size24, "Size28": Size28, "Size30": Size30,
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("表里少了 %s", name)
			continue
		}
		if g != w {
			t.Errorf("%s = %d，want %d——这张表是全应用字号的唯一出处，改它等于改"+
				"所有用到这一档的地方", name, g, w)
		}
	}
}

// TestTheRoleAliasesPointAtTheRightSize 盯角色别名。
//
// ⚠️ **别名不产生第二份值**：它们就是刻度名的另一个写法。
// 有人把 `PageTitle` 指向 `Size14`，画面上歌名的层次就没了，而这一条会红。
func TestTheRoleAliasesPointAtTheRightSize(t *testing.T) {
	want := map[string]int{
		"PageTitle": 24, "CollectionTitle": 30, "NotFoundTitle": 28,
		"NewMusicTitle": 24, "BrandName": 24, "ListTitle": 16, "ListOwner": 14,
		"CollectionCopy": 14, "LyricsTranslation": 14, "EmptyTitle": 15,
		"EmptyHint": 12, "EmptyPageTitle": 20, "LyricValue": 10,
		"HeaderFont": 14, "ToastFont": 14, "FavPanelItem": 14,
		"RateMenuFont": 12, "VolPercentFont": 12,
	}
	got := map[string]int{
		"PageTitle": PageTitle, "CollectionTitle": CollectionTitle,
		"NotFoundTitle": NotFoundTitle, "NewMusicTitle": NewMusicTitle,
		"BrandName": BrandName, "ListTitle": ListTitle, "ListOwner": ListOwner,
		"CollectionCopy": CollectionCopy, "LyricsTranslation": LyricsTranslation,
		"EmptyTitle": EmptyTitle, "EmptyHint": EmptyHint,
		"EmptyPageTitle": EmptyPageTitle, "LyricValue": LyricValue,
		"HeaderFont": HeaderFont, "ToastFont": ToastFont,
		"FavPanelItem": FavPanelItem, "RateMenuFont": RateMenuFont,
		"VolPercentFont": VolPercentFont,
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("表里少了 %s", name)
			continue
		}
		if g != w {
			t.Errorf("%s = %d，want %d——别名指错档，画面上的层次就没了", name, g, w)
		}
	}
}

// TestTheSizeScaleMatchesTheDesignScale 把表里的每一档和 DESIGN.md §二
// 的「字号刻度」那张表逐条对上。
//
// ⚠️ **DESIGN.md 是「唯一目标值」**（它自己顶上那句话）：代码和它对不上时，
// 以它为准。所以这张表必须跟它一致——不一致的话，哪一边是错的就说不清了。
//
// 这一条直接读 DESIGN.md 那个 yaml 块，不另抄一份数：抄一份的话，两边各改
// 各的，这条测试还是绿的。
func TestTheSizeScaleMatchesTheDesignScale(t *testing.T) {
	tokens := parseDesignSizeScale(t, repoRoot(t))
	got := map[string]int{
		"Size10": Size10, "Size11": Size11, "Size12": Size12, "Size13": Size13,
		"Size14": Size14, "Size15": Size15, "Size16": Size16, "Size18": Size18,
		"Size20": Size20, "Size24": Size24, "Size28": Size28, "Size30": Size30,
	}
	for name, g := range got {
		w, ok := tokens[name]
		if !ok {
			t.Errorf("DESIGN.md 的字号刻度里没有 %s", name)
			continue
		}
		if g != w {
			t.Errorf("DESIGN.md 字号刻度 %s = %d，表里是 %d——两边对不上", name, w, g)
		}
	}
	// 反过来：DESIGN.md 里多出来的档也要有人认领。
	for name, v := range tokens {
		if _, ok := got[name]; !ok {
			t.Errorf("DESIGN.md 的字号刻度里 %s = %d，表里没有对应的名字", name, v)
		}
	}
}

// parseDesignSizeScale 读 DESIGN.md 里 `size_scale:` 那一段，返回档位表。
func parseDesignSizeScale(t *testing.T, root string) map[string]int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "DESIGN.md"))
	if err != nil {
		t.Fatalf("读 DESIGN.md 失败：%v", err)
	}
	out := map[string]int{}
	inBlock := false
	inSection := false
	for line := range strings.SplitSeq(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inBlock = !inBlock
			continue
		}
		if !inBlock {
			continue
		}
		if !inSection {
			if trimmed == "size_scale:" {
				inSection = true
			}
			continue
		}
		// 这一段里的令牌都是缩进两格的；顶格的行就是下一段了。
		if trimmed == "" || line == trimmed {
			break
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, val, ok := strings.Cut(trimmed, ":")
		if !ok {
			break
		}
		// 去掉行尾注释（DESIGN.md 里那几个令牌后面都跟着一句注释）。
		if i := strings.Index(val, "#"); i >= 0 {
			val = val[:i]
		}
		val = strings.TrimSpace(val)
		n, err := strconv.Atoi(val)
		if err != nil {
			t.Fatalf("DESIGN.md 的 size_scale.%s 不是整数：%q", key, val)
		}
		out[strings.TrimSpace(key)] = n
	}
	if len(out) == 0 {
		t.Fatal("DESIGN.md 里没读到 size_scale 一节——是不是改了格式？")
	}
	return out
}

// TestNoFontSizeLiteralIsLeft 把「字号字面量」这件事钉住：界面层里不许再
// 出现 `scale.Font(c, 13)` 这种写法。
//
// # 为什么要有这一条
//
// 这一票把 274 个字号实参收进了这张表。**光做完不算是做完**——过两个月
// 新写的一处 `scale.Font(c, 13)` 又是一条没进表的数，而那一处谁都看不出来。
// 这一条让「又漏了一个」变成**一条红**。
//
// # 它拦两种写法
//
//  1. **实参本身是裸数字**：`scale.Font(c, 13)`。
//
//  2. **实参是一个常量，而那个常量写着裸数字**：`const foo = 13` 之后
//     `scale.Font(c, foo)`。
//
//     ⚠️ **只查第 1 种是漏的**：实参是常量名时，`ast.BasicLit` 根本看不见它，
//     护栏照样绿——而那个数字**照样在表外**。所以第 2 种也要拦。
//
// 只扫**非测试**文件：测试里的数字是**判据**（「这一行的右沿在 884」），
// 拿表里的常量去写期望值的话，被测的常量一改，期望值跟着改，那条测试就白
// 写了。
func TestNoFontSizeLiteralIsLeft(t *testing.T) {
	uiDir := filepath.Join(repoRoot(t), "internal", "ui")
	var bad []string
	err := filepath.WalkDir(uiDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		// 这个文件里「名字 → 裸数字」的表：包级 const/var、函数里的局部
		// const/var、还有 `x := 13` 这种。
		numbers := numericNames(file)
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			// 只盯 `FontSize(scale.Font(c, N))` —— 那是**字号**的用法。
			//
			// ⚠️ **别把 `Width(scale.Font(c, N))` 也算进来**：那几处是**宽度**
			// 跟着系统字号缩放（设置页那一排分段控件、排序下拉的最小宽），
			// 它们的数不是字号，不在这张表里。
			outer, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || outer.Sel.Name != "FontSize" {
				return true
			}
			inner, ok := call.Args[0].(*ast.CallExpr)
			if !ok || len(inner.Args) < 2 {
				return true
			}
			sel, ok := inner.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Font" {
				return true
			}
			// `scale.Font(c, N)` 的第二个实参才是字号；第一个是 context。
			for _, arg := range inner.Args[1:] {
				switch v := arg.(type) {
				case *ast.BasicLit:
					if v.Kind != token.INT && v.Kind != token.FLOAT {
						continue
					}
					pos := fset.Position(v.Pos())
					bad = append(bad, fmt.Sprintf("%s:%d: scale.Font(..., %s) —— 实参是裸数字",
						pos.Filename, pos.Line, v.Value))
				case *ast.Ident:
					if lit, ok := numbers[v.Name]; ok {
						pos := fset.Position(v.Pos())
						bad = append(bad, fmt.Sprintf("%s:%d: scale.Font(..., %s) —— 实参是常量 %s = %s，那个数写在表外",
							pos.Filename, pos.Line, v.Name, v.Name, lit))
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("扫源码失败：%v", err)
	}
	if len(bad) > 0 {
		t.Errorf("界面层里还有 %d 处字号的数字没走 scale 那张刻度表：\n%s",
			len(bad), strings.Join(bad, "\n"))
	}
}

// numericNames 收一个文件里所有「名字 → 裸数字」的常量与变量。
//
// 包级 const/var、函数里的局部 const/var、`x := 13` 都算。
func numericNames(file *ast.File) map[string]string {
	out := map[string]string{}
	add := func(name string, lit *ast.BasicLit) {
		if _, err := strconv.ParseFloat(lit.Value, 64); err != nil {
			return
		}
		out[name] = lit.Value
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.GenDecl:
			if v.Tok != token.CONST && v.Tok != token.VAR {
				return true
			}
			for _, spec := range v.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok {
						add(name.Name, lit)
					}
				}
			}
		case *ast.AssignStmt:
			if v.Tok != token.DEFINE {
				return true
			}
			for i, lhs := range v.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || i >= len(v.Rhs) {
					continue
				}
				if lit, ok := v.Rhs[i].(*ast.BasicLit); ok {
					add(id.Name, lit)
				}
			}
		}
		return true
	})
	return out
}
