package scrim

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 这一组是遮罩令牌表的**护栏**：表的每一档值不许漂，界面层也不许再出现
// 裸的遮罩 alpha。

// repoRoot 是仓库根（从本文件的路径往上四级）。
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("拿不到本测试文件的路径")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", ".."))
}

// TestTheScrimValuesAreLocked 把每一档的 alpha 钉死。
//
// ⚠️ **这张表是「值只写一处」的地方**：改了 `Media` 的值，满屏播放器的背景
// 压暗和裁剪框外的压暗会**一起**变——那是这张表存在的意义，也是它危险的方。
func TestTheScrimValuesAreLocked(t *testing.T) {
	want := map[string]float64{
		"Media": 0.55, "Cover": 0.50, "Modal": 0.40, "Overlay": 0.35, "OnScrim": 0.92,
		// UP 空间头图区那两档（浅色 / 深色各一对）。
		"HeaderLight": 0.30, "HeaderDark": 0.50,
		"HeaderTextLight": 0.40, "HeaderTextDark": 0.15,
	}
	got := map[string]float64{
		"Media": Media, "Cover": Cover, "Modal": Modal, "Overlay": Overlay, "OnScrim": OnScrim,
		"HeaderLight": HeaderLight, "HeaderDark": HeaderDark,
		"HeaderTextLight": HeaderTextLight, "HeaderTextDark": HeaderTextDark,
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("表里少了 %s", name)
			continue
		}
		if g != w {
			t.Errorf("%s = %v，want %v——这张表是全应用遮罩的唯一出处，改它等于改"+
				"所有用到这一档的地方", name, g, w)
		}
	}
}

// TestNoScrimLiteralIsLeft 把「遮罩 alpha 字面量」这件事钉住：界面层里不许
// 再出现 `Background(ui.RGBA(0, 0, 0, 0.55))` 这种写法。
//
// # 为什么要有这一条
//
// 这一票把 6 处遮罩 alpha 收进了这张表。**光做完不算是做完**——过两个月
// 新写的一处 `ui.RGBA(0, 0, 0, 0.5)` 又是一条没进表的数，而那一处谁都
// 看不出来。这一条让「又漏了一个」变成**一条红**。
//
// # 它只盯「盖在内容上面的黑」
//
// ⚠️ **不是所有「纯黑 + 裸 alpha」都是遮罩**。这四族也是纯黑加裸 alpha，
// 但它们各管各的：
//
//   - 旋钮 / 开关的**影**（`knobslider.go`、`static_switch.go`）——那是画笔级的
//     浓度（0.14 / 0.22 / 0.25 那一堆），归 `widgets/shadow` 那一族。
//   - 渐变的**端点**（`hover.go` 的 `from-black/90`）——那是一层从透明到黑的
//     渐变，不是「盖上去的一层」。
//   - 满屏那两级**前景色**（`fullplayer.go` 的 0.92 / 0.62，浅底时改用黑字）——
//     那是字，不是罩。
//
// 所以判据是**这个 RGBA 被当作底色盖上去**：它是 `Background(...)` 或
// `Gradient(...)` 的实参。剩下的用法一律放行。
//
// 只扫**非测试**文件：测试里的数字是**判据**，拿表里的常量去写期望值的
// 话，被测的常量一改，期望值跟着改，那条测试就白写了。
func TestNoScrimLiteralIsLeft(t *testing.T) {
	uiDir := filepath.Join(repoRoot(t), "internal", "ui")
	var bad []string
	err := filepath.WalkDir(uiDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// `widgets/shadow` 那一族是**画笔级的影**，不是遮罩。
			if d.Name() == "shadow" {
				return filepath.SkipDir
			}
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
		// 先收「被当作底色盖上去」的那些 RGBA 调用。
		overlays := overlayColors(file)
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 4 {
				return true
			}
			if !overlays[call] {
				return true
			}
			// `ui.RGBA(r, g, b, alpha)` —— 前三格必须是 0（纯黑）。
			for _, arg := range call.Args[:3] {
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Value != "0" {
					return true
				}
			}
			alpha, ok := call.Args[3].(*ast.BasicLit)
			if !ok || (alpha.Kind != token.INT && alpha.Kind != token.FLOAT) {
				return true
			}
			pos := fset.Position(alpha.Pos())
			bad = append(bad, fmt.Sprintf("%s:%d: 遮罩的 alpha 写成了裸数字 %s——"+
				"去 scrim 那张表里取一档", pos.Filename, pos.Line, alpha.Value))
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("扫源码失败：%v", err)
	}
	if len(bad) > 0 {
		t.Errorf("界面层里还有 %d 处遮罩的 alpha 没走 scrim 那张表：\n%s",
			len(bad), strings.Join(bad, "\n"))
	}
}

// overlayColors 收这个文件里所有「被当作底色盖上去」的 RGBA 调用。
//
// 判据是**它出现在 `Background(...)` 或 `Gradient(...)` 的实参里**——
// 那两个是「拿这个颜色铺一层」的入口。中间变量（`dim := ui.RGBA(...)`
// 之后 `Background(dim)`）认不出来，那些不归这条管（值已经收进表了）。
func overlayColors(file *ast.File) map[*ast.CallExpr]bool {
	out := map[*ast.CallExpr]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// ⚠️ **只有 `Background` 算**：渐变的**端点**不是遮罩——
		// `Gradient(ui.RGBA(0,0,0,0), ui.RGBA(0,0,0,0.9), 180)` 那一头是
		// 透明、这一头是九成黑，它是「从封面底边往上淡出」的一层，
		// 不是「盖上去的一层黑」。
		if sel.Sel.Name != "Background" {
			return true
		}
		for _, arg := range call.Args {
			if rgba, ok := arg.(*ast.CallExpr); ok {
				out[rgba] = true
			}
		}
		return true
	})
	return out
}

// TestTheScrimAlphasAreDistinct 盯住「几档之间别撞成同一个数」。
//
// ⚠️ **两档撞成同一个数的时候，这一档就没有名字了**：调用处写
// `scrim.Media` 和 `scrim.Overlay` 却是同一个值，那「要哪一档」这句话
// 就没意义了——而那一处的语义差别（背景压暗 vs 小浮层底）在画面上只看
// 得出深浅，改错了看不出来。
func TestTheScrimAlphasAreDistinct(t *testing.T) {
	seen := map[float64]string{}
	for name, v := range map[string]float64{
		"Media": Media, "Cover": Cover, "Modal": Modal, "Overlay": Overlay,
	} {
		if other, ok := seen[v]; ok {
			t.Errorf("%s 和 %s 是同一个数 %v——两档撞了，其中一档就没有名字了",
				name, other, v)
			continue
		}
		seen[v] = name
	}
}
