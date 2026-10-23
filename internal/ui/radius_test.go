package ui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// 这一组是圆角的**护栏**：界面层里不许再出现裸的圆角数字。
//
// # 为什么要有这一条
//
// 审计（票 #212 的 F-11）数过：主题圆角是用户可调的（`app.Settings.BorderRadius`，
// 默认 8、上限 24），而界面代码里有 **27 处写死了圆角数字**——其中 `Radius(6)`
// 18 处。**用户把全局圆角调到 24 时，那 27 处一个都不跟着变。**
//
// 这一票把该跟主题的都改成了 `t.Radius`，剩下几处**刻意不跟**的（预览缩略图
// 里画出来的假方块、药丸、玻璃面板、颜色样本）都起了名字。
//
// 这一条让「又漏了一个」变成**一条红**：以后新写一处 `Radius(6)`，它会指出来。
//
// # 它拦两种写法
//
//  1. **实参本身是裸数字**：`Radius(6)`。
//
//  2. **实参是一个常量，而那个常量写着裸数字**：`const r = 6` 之后
//     `Radius(r)`。
//
//     ⚠️ **只查第 1 种是漏的**：实参是常量名时，`ast.BasicLit` 根本看不见它，
//     护栏照样绿——而那个数字**照样是写死的**。所以第 2 种也要拦。
//
//     ⚠️ **第 2 类只拦「该跟主题却写死了」的那些**：值是 6（上游 `rounded-md`
//     那一档）或者 8（全局圆角令牌的默认值）的时候，那个数是**跟着主题走
//     就该变的**。而药丸（999）、玻璃面板（12）、颜色样本（16）、预览缩略图
//     里画出来的假方块（1 / 2）那些是**形状**，不跟主题走——它们各有各的
//     理由，名字已经说明了（见各处注释）。
//
// 只扫**非测试**文件：测试里的数字是**判据**，拿表里的常量去写期望值的
// 话，被测的常量一改，期望值跟着改，那条测试就白写了。
func TestNoLiteralRadiusIsLeft(t *testing.T) {
	var bad []string
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
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
			if !ok || sel.Sel.Name != "Radius" {
				return true
			}
			switch v := call.Args[0].(type) {
			case *ast.BasicLit:
				if v.Kind != token.INT && v.Kind != token.FLOAT {
					return true
				}
				pos := fset.Position(v.Pos())
				bad = append(bad, fmt.Sprintf("%s:%d: Radius(%s) —— 实参是裸数字",
					pos.Filename, pos.Line, v.Value))
			case *ast.Ident:
				lit, ok := numbers[v.Name]
				if !ok {
					return true
				}
				// 只拦「该跟主题却写死了」的：6（上游 rounded-md 那一档）
				// 和 8（全局圆角令牌的默认值）。
				if lit != "6" && lit != "8" {
					return true
				}
				// ⚠️ **这两个是**刻意**不跟主题的**（改动前就有的具名常量）：
				//
				//	- `coverRadius` = 6：播放条那张封面。上游是 `radius="md"`
				//	  （Tailwind 的 md 就是 6），BMG-124 / 票 #220 定的：它要
				//	  和它悬停时那层黑罩的圆角**一样**，两边都是 6 才看得出来
				//	  是一张。
				//	- `popupRadius` = 8：浮层（弹窗 / 下拉）的圆角。浮层是浮在
				//	  别的内容**上面**的一层，它的圆角跟着下面的内容块走的话，
				//	  浮层和它盖着的那块会「长在一起」，看不出哪层浮在上面。
				if deliberateRadiusNames[v.Name] {
					return true
				}
				pos := fset.Position(v.Pos())
				bad = append(bad, fmt.Sprintf("%s:%d: Radius(%s) —— 实参是常量 %s = %s，那个数该跟主题走",
					pos.Filename, pos.Line, v.Name, v.Name, lit))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("扫源码失败：%v", err)
	}
	if len(bad) > 0 {
		t.Errorf("界面层里还有 %d 处写死的圆角：\n%s"+
			"（该跟主题的用 t.Radius；刻意不跟的给它起个名字，理由写在旁边）",
			len(bad), strings.Join(bad, "\n"))
	}
}

// deliberateRadiusNames 是**刻意不跟主题走**的那几个圆角常量。
//
// ⚠️ **它们是「形状」，不是「主题」**：药丸（999）、玻璃面板（12）、
// 颜色样本（16）、预览缩略图里画出来的假方块（1 / 2）——那些值在
// 上游那边也是写死的，跟着用户的圆角走的话，形状就变了。
//
// 而**这两个是改动前就有的**（这一票之前它们就在那儿，且都各有理由）：
// `coverRadius`（BMG-124 定的，播放条封面和它那层黑罩要一样）与
// `popupRadius`（浮层要浮在内容上面，圆角不能跟着下面的内容走）。
var deliberateRadiusNames = map[string]bool{
	"coverRadius": true,
	"popupRadius": true,
}

// numericNames 收一个文件里所有「名字 → 裸数字」的常量与变量。
//
// 包级 const/var、函数里的局部 const/var、`x := 6` 都算。
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
