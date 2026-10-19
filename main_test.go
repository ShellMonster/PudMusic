package main

// 「`main()` 真的把用户数据目录钉住了」这件事，本身得是**钉子**。
//
// # 为什么这一条不能省
//
// 2026-10 应用从 `Biu MyGo` 改名成 `BiuGo`，而 `mygo.App.Path(mygo.PathUserData)`
// 默认是 `os.UserConfigDir()` **拼应用名**。跟着改名走的话，老用户升级之后设置、
// 搜索历史、快捷键、歌词缓存、下载记录和界面状态会一起「消失」——数据一个字节
// 没坏，只是不再有人去那个目录找，而**没有任何一处会报错**。
//
// 补救是 `platform.PinUserDataDir()`（理由与取舍见 `internal/platform/datadir.go`），
// 而 `internal/platform/datadir_test.go` 那几条把它本身钉得很死：函数行为、常量、
// 「显示名不许退回」、「没搬任何东西过去」。
//
// **但那几条全是直接调函数的。** 把 `main()` 里那一句删掉——或者换成别的什么
// 调用——24 个包照样全绿，而老用户的数据正在静默地消失。变异实验做过：
// 把 `main()` 第一句换成 `_ = platform.LegacyUserDataDirName`，验收命令退出码 0、
// 无任何 FAIL。
//
// 缺口只能从这里补：`main()` **在测试里是调不到的**（它最后一句是 `ui.Run`，
// 那会把事件循环转起来），所以没有办法从行为上跑一遍启动流程。能钉的只有它的
// **源码形状**——这一条就是钉那个形状的。
//
// # 为什么用 go/parser 而不是 grep
//
// 一是 `main.go` 的注释里满是「必须排在所有 Mount* 之前」这类字样，正则会在
// 注释里先撞上；二是格式化一动（换行、缩进）正则就断，而 AST 不受这些影响。
// 这一份本来就只是一层兜底，不该比它要守的东西还脆。
//
// # 它证明不了什么
//
// 它证明不了「钉住之后数据真的还在」——那是 `internal/platform/datadir_test.go`
// 的 `TestEverythingInTheOldDirectoryIsStillThere` 干的。两条合起来才是一条完整
// 的链：这里证明「`main()` 调了它，且调在读盘之前」，那里证明「调了它之后老数据
// 还在原地」。哪一头断了这条链都断。

import (
	"cmp"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"
)

// pinCall 是 `main()` 必须调的那一句。
//
// ⚠️ 它连**参数个数**一起钉：不带参数才是「钉目录」，带参数说明有人换了个别的
// 调用，而那种事同样要红。
const pinCall = "platform.PinUserDataDir"

// TestMainPinsTheUserDataDirectoryBeforeAnythingReadsIt 钉住 `main()` 里的那一句。
//
// 断两件事：
//
//  1. **它在**——`func main()` 里确实调了 `platform.PinUserDataDir()`，且没带参数；
//  2. **它调在前面**——`app.New()` 和每一个 `ui.*`（`ui.MountMigration` 读盘、
//     `ui.MountSettings` 读设置、`ui.Run` 里建窗口与单实例锁也都读这个目录）都排在
//     它**后面**。
//
// 第 2 条不是洁癖：这一句晚一步的症状是**「读不到设置就安静地按第一次运行开起来」**，
// 用户只发现「改了主题，下次打开又变回来了」，而日志里一句异常都没有。
func TestMainPinsTheUserDataDirectoryBeforeAnythingReadsIt(t *testing.T) {
	calls := callsInMain(t)

	at, args := -1, -1
	for _, c := range calls {
		if c.name != pinCall {
			continue
		}
		at = c.pos
		args = c.args
	}
	if at < 0 {
		t.Fatalf("`main()` 里没有 %s()——用户数据目录于是会跟着显示名走，"+
			"老用户升级之后设置、搜索历史、快捷键、歌词缓存、下载记录和界面状态"+
			"会一起消失，而**没有任何一处会报错**（理由见本文件顶上那一节）。"+
			"第一句就得是它：%v", pinCall, callNames(calls))
	}
	if args != 0 {
		t.Errorf("`main()` 里那一句带了 %d 个参数——认不出那还是不是「钉目录」", args)
	}

	for _, c := range calls {
		if c.pos >= at {
			break
		}
		if strings.HasPrefix(c.name, "ui.") || c.name == "app.New" {
			t.Errorf("%s() 排在 %s() **前面**（第 %d 位 vs 第 %d 位）——"+
				"它读的就是这个目录，而晚一步的默认处理是安静地按第一次运行开起来",
				c.name, pinCall, c.pos, at)
		}
	}
}

// mainCall 是 `func main()` 里的一次包级调用。
type mainCall struct {
	name string // "ui.MountSettings" 这样的「包名.函数名」
	pos  int    // 在源码里的出现次序（0 起）
	args int    // 实参个数；-1 表示不是一次普通调用
}

// callsInMain 按**源码顺序**列出 `func main()` 里的那些「包名.函数名」调用。
//
// 只认 `包名.名字` 这种形状（`ui.MountSettings`），不认 `a.MountExplore` 那种
// 方法调用和裸函数——要守的是「入口点里对界面层与平台层的调用顺序」，方法调用
// 拼不出包名，认了反而会误报。
func callsInMain(t *testing.T) []mainCall {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("解 main.go：%v", err)
	}
	var fn *ast.FuncDecl
	for _, d := range f.Decls {
		if d, ok := d.(*ast.FuncDecl); ok && d.Name.Name == "main" {
			fn = d
		}
	}
	if fn == nil {
		t.Fatal("main.go 里没有 func main")
	}

	type hit struct {
		name string
		args int
		off  int
	}
	var hits []hit
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		hits = append(hits, hit{
			name: id.Name + "." + sel.Sel.Name,
			args: len(call.Args),
			off:  fset.Position(sel.Pos()).Offset,
		})
		return true
	})
	// `ast.Inspect` 对一棵正常的语法树本来就是源码顺序，但排一次更稳：这一条
	// 测试的全部价值就在「谁在谁前面」，顺序算错它就白跑了。
	slices.SortFunc(hits, func(a, b hit) int { return cmp.Compare(a.off, b.off) })

	out := make([]mainCall, len(hits))
	for i, h := range hits {
		out[i] = mainCall{name: h.name, pos: i, args: h.args}
	}
	return out
}

// callNames 把那串调用写成一行，好塞进报错里（`main()` 读起来比报错好懂）。
func callNames(calls []mainCall) string {
	names := make([]string, len(calls))
	for i, c := range calls {
		names[i] = c.name
	}
	return "[" + strings.Join(names, " → ") + "]"
}
