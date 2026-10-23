package space

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

// 这一组是这张表的**护栏**：表里的值不许漂，DESIGN.md 的令牌不许对不上，
// 代码里也不许再出现裸的间距数字。

// repoRoot 是仓库根（从本文件的路径往上三层）。
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("拿不到本测试文件的路径")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", ".."))
}

// TestTheScaleValuesAreLocked 把每一档的像素值钉死。
//
// ⚠️ **这张表是「值只写一处」的地方**：改了 `Px8` 的值，全应用 **109 处**
// 「8 像素的缝」会一起变——那是这张表存在的意义，也是它危险的方。
// 所以每一档的值都在这里写死一遍：谁想改一个数，得先改这里、并且想清楚
// 那 109 处会跟着变。
//
// 碎步（1/3/5/7/9/90）也在里面：**它们不是设计刻度**，但代码里在用，
// 所以一样钉死——归一是另一票的事（那会改画面）。
func TestTheScaleValuesAreLocked(t *testing.T) {
	want := map[string]int{
		// 刻度。
		"None": 0,
		"Px2":  2,
		"Px4":  4,
		"Px6":  6,
		"Px8":  8,
		"Px10": 10,
		"Px12": 12,
		"Px16": 16,
		"Px20": 20,
		"Px24": 24,
		"Px28": 28,
		"Px32": 32,
		"Px48": 48,
		// 碎步（不在刻度上）。
		"Px1":  1,
		"Px3":  3,
		"Px5":  5,
		"Px7":  7,
		"Px9":  9,
		"Px90": 90,
	}
	got := map[string]int{
		"None": None, "Px2": Px2, "Px4": Px4, "Px6": Px6, "Px8": Px8,
		"Px10": Px10, "Px12": Px12, "Px16": Px16, "Px20": Px20,
		"Px24": Px24, "Px28": Px28, "Px32": Px32, "Px48": Px48,
		"Px1": Px1, "Px3": Px3, "Px5": Px5, "Px7": Px7, "Px9": Px9,
		"Px90": Px90,
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("表里少了 %s", name)
			continue
		}
		if g != w {
			t.Errorf("%s = %d，want %d——这张表是全应用间距的唯一出处，改它等于改"+
				"所有用到这一档的地方", name, g, w)
		}
	}
}

// TestTheTableMatchesTheDesignTokens 把表里的别名和仓库根 DESIGN.md 的
// `spacing` 一节逐条对上。
//
// ⚠️ **DESIGN.md 是「唯一目标值」**（它自己顶上那句话）：代码和它对不上时，
// 以它为准。所以这张表必须跟它一致——不一致的话，哪一边是错的就说不清了。
//
// 这一条直接读 DESIGN.md 那个 yaml 块，不另抄一份数：抄一份的话，两边各改
// 各的，这条测试还是绿的。
func TestTheTableMatchesTheDesignTokens(t *testing.T) {
	tokens := parseDesignSpacing(t, repoRoot(t))
	alias := map[string]int{
		"unit":               Unit,
		"page_horizontal":    PageHorizontal,
		"page_vertical":      PageVertical,
		"list_gap":           ListGap,
		"settings_group_gap": SettingsGroupGap,
	}
	for name, want := range alias {
		got, ok := tokens[name]
		if !ok {
			t.Errorf("DESIGN.md 的 spacing 一节里没有 %s 令牌", name)
			continue
		}
		if got != want {
			t.Errorf("DESIGN.md spacing.%s = %d，表里是 %d——两边对不上", name, got, want)
		}
	}
	// 反过来：DESIGN.md 里多出来的令牌也要有人认领。
	for name, v := range tokens {
		if _, ok := alias[name]; !ok {
			t.Errorf("DESIGN.md 的 spacing.%s = %d，表里没有对应的名字", name, v)
		}
	}
}

// parseDesignSpacing 读 DESIGN.md 里 `spacing:` 那一段，返回令牌表。
func parseDesignSpacing(t *testing.T, root string) map[string]int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "DESIGN.md"))
	if err != nil {
		t.Fatalf("读 DESIGN.md 失败：%v", err)
	}
	out := map[string]int{}
	inBlock := false
	inSpacing := false
	for line := range strings.SplitSeq(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inBlock = !inBlock
			continue
		}
		if !inBlock {
			continue
		}
		if !inSpacing {
			if trimmed == "spacing:" {
				inSpacing = true
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
			t.Fatalf("DESIGN.md 的 spacing.%s 不是整数：%q", key, val)
		}
		out[strings.TrimSpace(key)] = n
	}
	if len(out) == 0 {
		t.Fatal("DESIGN.md 里没读到 spacing 一节——是不是改了格式？")
	}
	return out
}

// spacingMethods 是「间距」那一族方法名。
//
// ⚠️ **这份名单必须和框架对得上**（mygo v0.6.2）：框架里真有的是
// `Gap` / `GapX` / `GapY` / `Padding` / `PaddingX` / `PaddingY` /
// `PaddingStart` / `PaddingEnd` / `Margin` / `MarginX` / `MarginY` /
// `MarginStart` / `MarginEnd`。
//
// **框架里没有** `PaddingTop` / `PaddingBottom` / `PaddingLeft` /
// `PaddingRight` 和 `MarginTop` / `MarginBottom` / `MarginLeft` /
// `MarginRight`——那几个名字写出来**根本编译不过**，所以把它们列进护栏名单
// 等于给一份假名单壮胆：名字看着齐全，实际上一处也拦不住。
//
// 名单写窄了同样是漏：以后谁写一处 `.GapX(8)`，护栏不红，那条数就悄悄
// 出了表。所以这份名单要**照着框架抄**，不是照着想象抄。
var spacingMethods = map[string]bool{
	"Gap": true, "GapX": true, "GapY": true,
	"Padding": true, "PaddingX": true, "PaddingY": true,
	"PaddingStart": true, "PaddingEnd": true,
	"Margin": true, "MarginX": true, "MarginY": true,
	"MarginStart": true, "MarginEnd": true,
}

// TestNoSpacingLiteralIsLeft 把「间距字面量」这件事钉住：界面层里不许再
// 出现 `.Gap(8)` / `.Padding(0, 16)` 这种写法。
//
// # 为什么要有这一条
//
// 这一票把 310 个调用点、80 个本地常量收进了 `space` 那张表。**光做完不算是
// 做完**——过两个月新写的一处 `.Padding(0, 8)` 又是一条没进表的数，而那一处
// 谁都看不出来。这一条让「又漏了一个」变成**一条红**。
//
// # 它拦两种写法
//
//  1. **实参本身是裸数字**：`.Gap(8)`。
//
//  2. **实参是一个常量，而那个常量写着裸数字**：`const toastRight = 8` 之后
//     `.Padding(0, toastRight)`。
//
//     ⚠️ **只查第 1 种是漏的**：实参是常量名时，`ast.BasicLit` 根本看不见它，
//     护栏照样绿——而那个数字**照样在表外**。仓库里一度就摆着四个这样的反例
//     （`playbarClearance` / `toastRight` / `suggestTermPadY` / `suggestTermPadX`
//     / `favPanelGap`），报告却写着「0 个」。所以第 2 种也要拦。
//
// 只扫**非测试**文件：测试里的数字是**判据**（「这一行的右沿在 884」），
// 拿表里的常量去写期望值的话，被测的常量一改，期望值跟着改，那条测试就白
// 写了——`pagerhythm_test.go` 里那条就是这么故意写死 16 的。
func TestNoSpacingLiteralIsLeft(t *testing.T) {
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
		// const/var、还有 `x := 8` 这种。
		numbers := numericNames(file)
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !spacingMethods[sel.Sel.Name] {
				return true
			}
			for _, arg := range call.Args {
				switch v := arg.(type) {
				case *ast.BasicLit:
					if v.Kind != token.INT && v.Kind != token.FLOAT {
						continue
					}
					pos := fset.Position(v.Pos())
					bad = append(bad, fmt.Sprintf("%s:%d: .%s(%s) —— 实参是裸数字",
						pos.Filename, pos.Line, sel.Sel.Name, v.Value))
				case *ast.Ident:
					if lit, ok := numbers[v.Name]; ok {
						pos := fset.Position(v.Pos())
						bad = append(bad, fmt.Sprintf("%s:%d: .%s(%s) —— 实参是常量 %s = %s，那个数写在表外",
							pos.Filename, pos.Line, sel.Sel.Name, v.Name, v.Name, lit))
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
		t.Errorf("界面层里还有 %d 处间距的数字没走 space 那张表：\n%s",
			len(bad), strings.Join(bad, "\n"))
	}
}

// numericNames 收一个文件里所有「名字 → 裸数字」的常量与变量。
//
// 包级 const/var、函数里的局部 const/var、`x := 8` 都算。
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
