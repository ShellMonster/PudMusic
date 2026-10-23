package leading

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

// 这一组是行高档位表的**护栏**：表的每一档值不许漂，界面层也不许再出现
// 裸的行高数字。

// repoRoot 是仓库根（从本文件的路径往上四级）。
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("拿不到本测试文件的路径")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", ".."))
}

// TestTheLeadingValuesAreLocked 把每一档的行高钉死。
//
// ⚠️ **这张表是「行高只写一处」的地方**：改了 `Copy` 的值，全应用所有
// 小字说明那一行的行高会一起变——那是这张表存在的意义，也是它危险的方。
func TestTheLeadingValuesAreLocked(t *testing.T) {
	want := map[string]float64{
		"Owner": 1.0, "Compact": 1.2, "Lyrics": 1.375, "Signature": 1.4,
		"Title": 1.5, "Copy": 1.5, "Result": 1.6, "Hint": 1.7,
	}
	got := map[string]float64{
		"Owner": Owner, "Compact": Compact, "Lyrics": Lyrics, "Signature": Signature,
		"Title": Title, "Copy": Copy, "Result": Result, "Hint": Hint,
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("表里少了 %s", name)
			continue
		}
		if g != w {
			t.Errorf("%s = %v，want %v——这张表是全应用行高的唯一出处，改它等于改"+
				"所有用到这一档的地方", name, g, w)
		}
	}
}

// TestNoLeadingLiteralIsLeft 把「行高字面量」这件事钉住：界面层里不许再
// 出现 `LineHeight(1.5)` 这种写法。
//
// # 为什么要有这一条
//
// 这一票把 10 处行高实参收进了这张表。**光做完不算是做完**——过两个月
// 新写的一处 `LineHeight(1.5)` 又是一条没进表的数，而那一处谁都看不出来。
// 这一条让「又漏了一个」变成**一条红**。
//
// # 它拦两种写法
//
//  1. **实参本身是裸数字**：`LineHeight(1.5)`。
//
//  2. **实参是一个常量，而那个常量写着裸数字**：`const foo = 1.5` 之后
//     `LineHeight(foo)`。
//
//     ⚠️ **只查第 1 种是漏的**：实参是常量名时，`ast.BasicLit` 根本看不见它，
//     护栏照样绿——而那个数字**照样在表外**。所以第 2 种也要拦。
//
// 只扫**非测试**文件：测试里的数字是**判据**，拿表里的常量去写期望值的
// 话，被测的常量一改，期望值跟着改，那条测试就白写了。
func TestNoLeadingLiteralIsLeft(t *testing.T) {
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
		numbers := numericNames(file)
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			// `LineHeight(倍数)` 是行高；`FixedLineHeight(像素)` 那一支
			// **不是倍数**（歌词译文的 20 像素），不管它。
			if !ok || sel.Sel.Name != "LineHeight" {
				return true
			}
			switch v := call.Args[0].(type) {
			case *ast.BasicLit:
				if v.Kind != token.INT && v.Kind != token.FLOAT {
					return true
				}
				pos := fset.Position(v.Pos())
				bad = append(bad, fmt.Sprintf("%s:%d: LineHeight(%s) —— 实参是裸数字",
					pos.Filename, pos.Line, v.Value))
			case *ast.Ident:
				if lit, ok := numbers[v.Name]; ok {
					pos := fset.Position(v.Pos())
					bad = append(bad, fmt.Sprintf("%s:%d: LineHeight(%s) —— 实参是常量 %s = %s，那个数写在表外",
						pos.Filename, pos.Line, v.Name, v.Name, lit))
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
		t.Errorf("界面层里还有 %d 处行高的数字没走 leading 那张表：\n%s",
			len(bad), strings.Join(bad, "\n"))
	}
}

// numericNames 收一个文件里所有「名字 → 裸数字」的常量与变量。
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
