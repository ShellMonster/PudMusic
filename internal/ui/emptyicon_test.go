package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// 空状态**插画位**那一族，以及错误页。
//
// # 这一族断的是「画面上画出来了」，不是「哪个函数被调了」
//
// MyGo 的 Icon 没有元素树可以查（`mygo/ui/svg.go` 的文档：图标是装饰，辅助技术
// 看不见它），而无头 Tester 的 `Find` 只认画出来的文字和 `Label` 起的名字。所以
// 「那枚图标画出来了没有」只能从**像素**上看——元素在、位置对、文字也在，可屏
// 幕上什么也没有，那一类是查不出来的。
//
// ⚠️ **深浅两套主题都要断**：图标是单色的、跟着祖先的文字色走（`drawIcon` 收的
// 就是文字色），而浅色下文字是深灰、深色下是亮白。只断浅色的话，深色下整格
// 空掉照样全绿。

// TestEveryEmptyStateNamesItsOwnIllustration 是这一票的核心那一条：
// **七页各画各的**。
//
// 🔴 **这一条断的是「配的是哪一枚」，不是像素。** 试过像素：把每一页插画那一块
// 的颜色集合拿来两两比，而它红不起来——两次量到的集合里混着抗锯齿的灰阶，
// 同一个图标在两页上落在不同的亚像素位置上，集合就不同；而「那一页根本没配
// 图标」反倒让它更独特（少一枚 = 少一批颜色 = 和别的页不一样），于是照样全绿。
//
// 像素那一层留给下面那条（`TestTheIllustrationIsActuallyPainted`）：它断的是
// 「画出来了没有」，而「画的是不是这一枚」是数据层的断言，就用数据断。
//
// ⚠️ 上游七页用的是**同一枚** `RiCloseCircleLine`（`components/empty/index.tsx:16`），
// 所以「每页都配了图标」和「每页都一样」是同时成立的——上一版就是那个样子，
// 报告 §S1 点名的正是「七个页面共用一张」。
func TestEveryEmptyStateNamesItsOwnIllustration(t *testing.T) {
	// 七页就是 issue 126 点名的那七页（和 #124 的依赖顺序里那一份一致）。
	pages := []struct {
		what string
		e    emptyState
	}{
		{"推荐音乐", lookup("/").Empty},
		{"我的关注", lookup(followPath).Empty},
		{"稍后再看", lookup(laterPath).Empty},
		{"历史记录", lookup(historyPath).Empty},
		{"下载记录", lookup(downloadListPath).Empty},
		{"搜索", searchIdle},
		{"收藏夹", noFavorites},
	}

	seen := map[string]string{}
	for _, p := range pages {
		if p.e.Icon == "" {
			t.Errorf("%s 的空状态没有配插画（图标名是空串）——七页共用一张正是"+
				"这一票要消掉的那件事", p.what)
			continue
		}
		if prev, dup := seen[p.e.Icon]; dup {
			t.Errorf("%s 和 %s 配的是同一枚图标（%s）——"+
				"每一页要能一眼看出是哪个页面空着", p.what, prev, p.e.Icon)
			continue
		}
		seen[p.e.Icon] = p.what
	}
	if len(seen) < len(pages) {
		t.Errorf("七页里只有 %d 枚不同的图标，want %d", len(seen), len(pages))
	}
}

// TestTheIllustrationIsActuallyPainted 断「那一枚真的画出来了」。
//
// 判据是**标题正上方那条带里有东西**：上游 `space-y-2` 是 8、插画 32，所以插画
// 落在 `[标题顶 - 40, 标题顶 - 8]`。底色取「标题左边那一点」——**不是窗口左上角**，
// 那里是侧栏，而侧栏和内容区的底色不是同一个数（第一版取的就是它，于是整块内容
// 区在指纹里变成了两百多种白）。
//
// 断的是**画上了东西**而不是「长什么样」：长什么样是上面那条断的（它比较的是图标
// 名）。拿掉 `drawEmpty` 里那一行 `ui.Icon` 之后这一条立刻红，而上面那条照样绿。
func TestTheIllustrationIsActuallyPainted(t *testing.T) {
	for _, p := range []struct {
		what, path, title string
	}{
		{"推荐音乐", "/", lookup("/").Empty.Title},
		{"稍后再看", laterPath, lookup(laterPath).Empty.Title},
		{"历史记录", historyPath, lookup(historyPath).Empty.Title},
		// ⚠️ **下载记录页画的是「没有下载目录」那一支**：这一组的外壳没挂下载
		// （`DownloadDir()` 是空串），而没有目录时那一格说的是「去设置里选一
		// 个」而不是「搜一首歌」（报告 F3）。两支的插画是同一枚。
		{"下载记录", downloadListPath, noDownloadDir.Title},
	} {
		for _, dark := range []bool{false, true} {
			name := "浅色"
			if dark {
				name = "深色"
			}
			t.Run(p.what+"/"+name, func(t *testing.T) {
				b := newTestShell(t, systemIsDark(dark))
				s, tt := b.s, b.tt
				s.navigate(p.path)
				waitFrames(t, tt, p.what+"那一页的空状态画出来了", func() bool {
					return tt.HasText(p.title)
				})
				if n := paintedInBand(t, tt, p.title); n < 20 {
					t.Errorf("%s 的插画那一带只画了 %d 个像素（want ≥ 20）——"+
						"要么没画，要么和底色是同一个数（深色下看的是「看得清」那一位）",
						p.what, n)
				}
			})
		}
	}
}

// TestTheIllustrationIsFollowedByTheCopy 断「插画在两行字**上面**」。
//
// 判据是**位置**：插画那一块的外接框底边要落在标题那一行的顶边之上（上游
// `space-y-2 py-20`，插画在最上面）。只看「有几枚图标」的话，把插画摆在文字
// 底下（或者和文字重叠）也照样过——而那在画面上是一团糊。
func TestTheIllustrationIsFollowedByTheCopy(t *testing.T) {
	s, tt := newShell(t)
	s.navigate("/download-list")
	tt.Frame()

	// ⚠️ **这一页有两种空**：没有下载目录（这一支画的是它）和真的没下载过。
	// 这一组的外壳没挂下载，所以画出来的是前者（报告 F3）。
	e := noDownloadDir
	title := e.Title
	if e.Icon == "" {
		t.Fatalf("下载记录的空状态没有配图标（%v）", e)
	}
	tr, ok := tt.Find(title)
	if !ok {
		t.Fatalf("空状态那两行字没画出来，文字有 %q", tt.Texts())
	}

	// 标题上面那一块：图标待在那儿的高度。
	above := glyphRows(t, tt, 0, int(tr.Y)-1, 400)
	if above == 0 {
		t.Errorf("标题 %q 的上面一行图形都没画——插画不在文字上面", title)
	}
	// 而图形不能和标题那一行重叠：插画的外接框底边落在标题顶边之上。
	iconBox := iconAbove(t, tt, int(tr.Y)-1)
	if iconBox.Max.Y >= int(tr.Y) {
		t.Errorf("插画一直画到 y=%d，而标题从 y=%v 开始——两样叠在一起了（插画盒 %v）",
			iconBox.Max.Y, tr.Y, iconBox)
	}
}

// emptyStateVars 列出这个包里所有**包级** `emptyState` 变量的名字。
//
// 为什么先要这张名单：判据是「有人自己画了某一条空状态的那两行」，而「某一条」
// 在 AST 上唯一能认出来的就是**这个变量是 `emptyState`**。只按 `.Title` /
// `.Hint` 认会误伤一大片（歌名的 `m.Title`、下载那一行的 `r.Title`、满屏歌名的
// `st.Title`……它们和空状态一点关系都没有）。
func emptyStateVars(t *testing.T) map[string]bool {
	t.Helper()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	out := map[string]bool{}
	for _, name := range matches {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败，这一就没法查了：%v", name, err)
		}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || !isEmptyStateValue(vs.Type, vs.Values) {
					continue
				}
				for _, n := range vs.Names {
					out[n.Name] = true
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("一个包级 emptyState 变量都没找到——这一条的判据整个失效了")
	}
	return out
}

// localEmptyStateVars 找出「局部的那一条空状态」——`e := lookup(path).Empty`。
//
// ⚠️ **为什么单独一条**：局部变量的类型是推断出来的，AST 里看不到它是
// `emptyState`，所以上面那张包级名单里没有它。而这个包里取路由那一条空状态
// **只有这一个写法**（`routes` 表的 `Empty` 字段），而它恰好是漂开过两次的
// 那一处（`librarypage.go` 和 `follow.go` 各抄过一份 15 / 12）。
//
// 局限性说在前面：这不是类型检查。`e := 随便什么表达式` 只要结果碰巧是
// `emptyState` 就认不出来——而这个包里没有那种写法。
func localEmptyStateVars(t *testing.T) map[string]bool {
	t.Helper()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	out := map[string]bool{}
	for _, name := range matches {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || as.Tok != token.DEFINE || len(as.Rhs) == 0 {
				return true
			}
			sel, ok := as.Rhs[0].(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Empty" {
				return true
			}
			if _, ok := sel.X.(*ast.CallExpr); !ok { // lookup(path).Empty
				return true
			}
			for _, lhs := range as.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					out[id.Name] = true
				}
			}
			return true
		})
	}
	return out
}

// isEmptyStateValue 报这一个声明是不是一个 `emptyState`。
//
// 认两种写法：`var x emptyState`（有类型）和 `var x = emptyState{…}`（没类型、
// 值是复合字面量）——这个包里两种都有。
func isEmptyStateValue(typ ast.Expr, values []ast.Expr) bool {
	if id, ok := typ.(*ast.Ident); ok && id.Name == "emptyState" {
		return true
	}
	if len(values) == 0 {
		return false
	}
	cl, ok := values[0].(*ast.CompositeLit)
	if !ok {
		return false
	}
	if t, ok := cl.Type.(*ast.Ident); ok {
		return t.Name == "emptyState"
	}
	// `emptyState{…}` 省略类型时是 `[]T{…}` 那一类的 SelectorExpr/ArrayType
	return false
}

// TestTheSharedDrawingEntryIsTheOnlyOne 钉住「**一套**绘制入口」。
//
// 🔴 机械查 AST，不靠自觉：空状态这一族有十几处调用点，每一处自己写一遍
// `FontSize(15)` + `FontSize(12)` 的话，七页的字号和主次色就会慢慢漂开——而
// 每一次都是无意的（抄一下上一处的），也看不出来。
//
// ## 判据为什么是 AST，以及走过两次弯路
//
// 第一版查「同一行里同时出现 `FontSize(15)` 和 `FontSize(12)`」，而它**第一次
// 跑就被 `gofmt` 打败**：变异写成一行，`gofmt` 把它拆成两行，判据失效。
//
// 第二版查 AST 找「`ui.Text(…, X.Title)`」，而它**误伤一大片**：歌名的
// `m.Title`、下载那一行的 `r.Title`、满屏歌名的 `st.Title` 都叫 `.Title`，
// 和空状态一点关系都没有。
//
// 现在这一版先把**包级 `emptyState` 变量**的名字收成一张名单（`emptyStateVars`），
// 再找「谁在画名单上某个变量的 `.Title` / `.Hint`」——那是「把空文案抄了一份」
// 的确切形状。
//
// ⚠️ **必须真的解析生产文件**：第一版用的是 `filesInThisPackage`，而它只 glob
// `*_test.go`——于是这一条解析的全是测试文件，恒绿，还绿得很有自信（两次变异
// 都从它底下过去了）。
//
// allowlist 上那两处都不是「抄了一份」，形状本来就和共享入口不同：满屏那一屏在
// 有底色的背板上（主次色是背板的 on / dim），而「还没登录」那一支的 `Title` 是
// 一句 22px 的标题、下面还跟着一枚按钮。理由都写在 allowlist 那一行上。
func TestTheSharedDrawingEntryIsTheOnlyOne(t *testing.T) {
	vars := emptyStateVars(t)
	// 局部的那一条（`e := lookup(path).Empty`）也进来，见 localEmptyStateVars。
	for k, v := range localEmptyStateVars(t) {
		vars[k] = v
	}
	allow := map[string]bool{
		// 满屏那一屏压在**有底色的背板**上，主次色是背板的 on / dim 而不是
		// 主题的 Text / TextMuted（浅色封面铺满时主题那两档会糊进去），
		// 而且它还要在下面挂一枚「去登录」。见 fullplayer.go 那一处的注释。
		"viewFullEmpty": true,
		// 「还没登录」那一支（我的关注 / 稍后再看 / 历史记录共用）：它的
		// `Title` 是一句**页标题那个字号和字重**（24 / 400，不是共享入口里
		// 那个 15px 的「这里为什么空」），下面还跟着一个「去登录」按钮。
		// 形状不同就是另一支，不是抄了一份。见 loginprompt.go。
		"viewNeedsLogin": true,
		// 满屏那一屏的歌词空状态（票 #236）：和 `viewFullEmpty` 同一个理由——
		// 满屏压在**糊过的封面**上，主次色是满屏自己的前景色（白 92% /
		// 黑 92%，跟着底走），而共享出口 `emptyCopy` 用的是主题的 Text /
		// TextMuted（浅色主题下是黑的，压在浅色封面上会糊进去）。
		// 见 fullplayer_lyrics.go 那一处的注释。
		"viewFullLyricsEmpty": true,
	}

	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range matches {
		// `empty.go` 就是那一份本身，放过；测试文件里出现 `.Title` 是断言。
		if name == "empty.go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败，这一就没法查了：%v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || allow[fn.Name.Name] || fn.Body == nil {
				return true
			}
			ast.Inspect(fn.Body, func(m ast.Node) bool {
				call, ok := m.(*ast.CallExpr)
				if !ok {
					return true
				}
				// ⚠️ 两层类型断言都要先判 `ok` 再往下走：第一层失败时 `sel`
				// 是 nil，接着问 `sel.X` 就是当场空指针——而崩在测试里看起来
				// 像「这一条坏了」，和它要查的那件事一点关系都没有。
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Text" {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || (pkg.Name != "ui" && pkg.Name != "mygoui") {
					return true
				}
				for _, arg := range call.Args {
					as, ok := arg.(*ast.SelectorExpr)
					if !ok {
						continue
					}
					// ⚠️ 接收者**不一定是变量**：`lookup(followPath).Empty.Title`
					// 的接收者是一个调用。这里必须先判再取，否则当场类型断言
					// 崩——而崩在测试里看起来像「这一条坏了」。
					id, ok := as.X.(*ast.Ident)
					if !ok || !vars[id.Name] {
						continue
					}
					if as.Sel.Name == "Title" || as.Sel.Name == "Hint" {
						t.Errorf("%s:%d 的 %s 自己画了 %s.%s——空文案走 `emptyCopy`"+
							"（empty.go），抄一份改一次字号只改一处、另一处不动"+
							"（真要例外的话，加进这一条的 allowlist 并写明理由）",
							name, fset.Position(call.Pos()).Line, fn.Name.Name,
							id.Name, as.Sel.Name)
					}
				}
				return true
			})
			return true
		})
	}
}

// TestTheErrorPageSaysWhereItBroke 是错误页那一族的核心：出错在哪一步。
//
// 用户要拿这句话去跟开发者说，而只显示 panic 的值（「index out of range
// [5] with length 3」）谁也复现不出来——那句话说明了**是什么**，一点也没说
// **在哪儿**。所以错误页上必须同时有：崩在界面的哪一块、当时在看哪一页。
func TestTheErrorPageSaysWhereItBroke(t *testing.T) {
	b := newTestShell(t, at(900, 600),
		stubbed(func(_ *testing.T, s *Shell) {
			s.navigate("/download-list")
			s.crash = s.newCrashInfo("页面「下载记录」", "index out of range [5] with length 3")
		}))
	tt := b.tt

	for _, want := range []string{
		"出错的地方：页面「下载记录」",     // 界面的哪一块
		"当时在看的页面：下载记录",       // 当时在看哪一页
		"index out of range", // 原始错误
		"反馈",                 // 走得出去的路之一
		"回到首页",
		"重新载入这一页",
	} {
		if !tt.HasText(want) {
			t.Errorf("错误页上没有 %q，文字有 %q", want, tt.Texts())
		}
	}

	// 而「哪一页」这一句要能**换**：同一个 panic 落在不同页上，错误页得说出
	// 不同的页名——否则这一句只是个装饰。
	b2 := newTestShell(t, at(900, 600),
		stubbed(func(_ *testing.T, s *Shell) {
			s.navigate(laterPath)
			s.crash = s.newCrashInfo("侧栏", "坏掉了")
		}))
	if !b2.tt.HasText("当时在看的页面：稍后再看") {
		t.Errorf("换一页崩之后错误页还是原来那页的名字，文字有 %q", b2.tt.Texts())
	}
	if !b2.tt.HasText("出错的地方：侧栏") {
		t.Errorf("崩在别的块上，错误页没跟着换，文字有 %q", b2.tt.Texts())
	}
}

// TestTheErrorPageHasAnIllustration 断「错误页不是一片白」。
//
// ⚠️ **判据是像素**：光断言「有文字」的话，那张插画被撤掉之后这一条照样绿——
// 而「界面出错了」四行字摆在正中，用户看到的就是「一片白上面有几行字」。
// 断言的是插画那一块**和它上下的背景不是一个颜色**。
func TestTheErrorPageHasAnIllustration(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, at(900, 600), systemIsDark(dark),
				stubbed(func(_ *testing.T, s *Shell) {
					s.crash = s.newCrashInfo("侧栏", "坏掉了")
				}))
			img := b.tt.Image()
			// 插画在标题「界面出错了…」**上面**。量出标题那一行，再扫它上面
			// 那条带里有没有和窗口底色不一样的像素。
			//
			// ⚠️ **扫的是「有没有」而不是「某一个点是不是」**：那张图是线稿，
			// 线条之间留着大片的底色，取中心那一个点十次里有九次落在空处——
			// 而那种测试红起来的样子是「插画没画出来」，真因却是取错了点。
			tr, ok := b.tt.Find("界面出错了，播放器已经停在这一页")
			if !ok {
				t.Fatalf("错误页没画出来，文字有 %q", b.tt.Texts())
			}
			window := img.RGBAAt(4, 4)
			band := 0
			for y := 0; y < int(tr.Y); y++ {
				for x := 0; x < 900; x++ {
					if img.RGBAAt(x, y) != window {
						band++
					}
				}
			}
			if band == 0 {
				t.Errorf("错误页上标题上面那条带（%d × %d）全是窗口底色（%v）——"+
					"插画没画出来，撤掉它之后用户看到的就是一片白",
					900, int(tr.Y), window)
			}
		})
	}
}

// TestTheErrorPageHasAWayOutInBothThemes 断「走得出去」在深色下也在。
//
// 这一条很土，但它挡的是一个真实发生过的坏：按钮摆在**深色下看不见的底色**上，
// 于是「点得动」在浅色下验得过、在深色下点不动，而两条断言都只跑浅色。
func TestTheErrorPageHasAWayOutInBothThemes(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// ⚠️ **一枚一个外壳**：三枚里任何一枚点下去都会离开错误页
			//（「重新载入」清掉 crash、「回到首页」换路由），而一个外壳上连点
			// 第二枚时屏幕上早就是正常画面了——报出来的是「第二枚不存在」，
			// 真因是第一枚点得太有效。
			for _, label := range []string{"反馈", "回到首页", "重新载入这一页"} {
				b := newTestShell(t, at(900, 600), systemIsDark(dark),
					stubbed(func(_ *testing.T, s *Shell) {
						s.crash = s.newCrashInfo("侧栏", "坏掉了")
					}))
				if _, ok := b.tt.Find(label); !ok {
					t.Errorf("错误页上没有 %q，文字有 %q", label, b.tt.Texts())
					b.Close()
					continue
				}
				if err := b.tt.Click(label); err != nil {
					t.Errorf("点不了 %q：%v", label, err)
				}
				b.Close()
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 量画面的工具
// ─────────────────────────────────────────────────────────────────────────────

// paintedInBand 数「标题正上方那条带里，有多少个像素和底色不一样」。
//
// ⚠️ 底色取「标题左边那一点」（`标题顶 - 6`），**不是窗口左上角**：那里是侧栏，
// 而侧栏和内容区的底色不是同一个数。拿侧栏那个数当底色的话，整块内容区每一点
// 纯白都「不是底色」，于是数出来的是整屏的白而不是那枚插画——而判据是
// 「≥ 20 个像素」，第一版因此永远绿（它量到的是几千）。
//
// 带宽和高度照 `drawEmpty` 的那几个数：插画 32、间距 8，所以落在
// `[标题顶 - 40, 标题顶 - 8]`。横着取标题中点左右各 24——空状态是居中的
// （上游 `items-center justify-center`），标题居中，所以它的中点就是那一列的中点。
func paintedInBand(t *testing.T, tt *mygoui.Tester, title string) int {
	t.Helper()
	tr, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到标题 %q，文字有 %q", title, tt.Texts())
	}
	img := tt.Image()
	bg := img.RGBAAt(int(tr.X)-6, int(tr.Y)-6)
	cx := int(tr.X + tr.W/2)
	n := 0
	for y := int(tr.Y) - 40; y <= int(tr.Y)-8; y++ {
		for x := cx - 24; x <= cx+24; x++ {
			if y < 0 || x < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
				continue
			}
			if img.RGBAAt(x, y) != bg {
				n++
			}
		}
	}
	return n
}

// glyphRows 数那一行里「不是底色」的像素有几个——0 就是那一行什么都没画。
func glyphRows(t *testing.T, tt *mygoui.Tester, x0, y, w int) int {
	t.Helper()
	if y < 0 {
		return 0
	}
	img := tt.Image()
	bg := img.At(2, 2) // 窗口左上角 = 底色
	n := 0
	for x := x0; x < x0+w; x++ {
		if img.At(x, y) != bg {
			n++
		}
	}
	return n
}

// iconAbove 量 `y` 那一行往上、标题之前那一块里的图形外接框。
//
// 找不到就返回一个落在标题之上的空框（`Max.Y` 小于标题 y），调用点据此报错。
func iconAbove(t *testing.T, tt *mygoui.Tester, y int) (r image.Rectangle) {
	t.Helper()
	img := tt.Image()
	bg := img.At(2, 2)
	minX, minY, maxX, maxY := 1<<30, 1<<30, -1, -1
	for yy := 0; yy <= y; yy++ {
		for x := 0; x < 1200; x++ {
			if img.At(x, yy) == bg {
				continue
			}
			if x < minX {
				minX = x
			}
			if yy < minY {
				minY = yy
			}
			if x > maxX {
				maxX = x
			}
			if yy > maxY {
				maxY = yy
			}
		}
	}
	return image.Rect(minX, minY, maxX, maxY)
}

// readFileOf 读这个包里的一份文件。
//
// 测试进程的当前目录就是包目录（`go test` 的约定），所以 `filesInThisPackage`
// 给的那些相对名字直接能用。
func readFileOf(t *testing.T, name string) ([]byte, error) {
	t.Helper()
	return os.ReadFile(name)
}
