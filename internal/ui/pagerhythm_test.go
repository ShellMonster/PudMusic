package ui

// 页面节奏那一族：页边距、页标题、主 Tab 与次级 Tab、搜索页的关键词、加载态、
// 未登录态、小字的行高（票 #138）。
//
// # 为什么大半的判据是像素
//
// 无头 Tester 的 `Find` 认的是「画着这句字**或者**标着这个名字」的元素，而
// **页标题和路由容器重名**：路由容器的无障碍名字就是页名（`Router.View` 结尾
// 那句 `pg.Label(e.title)`），所以 `Find("稍后再看")` 给回来的是整个路由容器
// （{200 64 1000 648}），不是那一行字。而侧栏里那几项的名字又和页标题重名
// （「本地音乐」「下载记录」），所以 `Find` 在这一族上认不准。
//
// 像素那一层没有这个问题：它量的是「画面上最靠左的那一笔」。而**字号**那一族
// 另有一处：字号量的是**墨迹的高度**（同一个字体同一个字号 ⇒ 同样的墨迹高度），
// 精确的那个数由 `TestEveryPageTitleUsesTheSharedSize`（AST）钉住。

import (
	"context"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	mygoui "github.com/egoist/mygo/ui"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"go/ast"
	"go/parser"
	"go/token"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 测试窗口的尺寸（基座默认 1200×800）。
const (
	testWinW = 1200
	testWinH = 800
)

// contentArea 是内容区（侧栏右边、顶栏下面、播放条上面）的那一块。
//
// ⚠️ **四边都让开**：侧栏右缘那条 1px 的分界线画在侧栏的最后那一列上，顶栏
// 下缘也有一道，播放条上缘还有一道渐变的影子——它们横贯整块内容区，扫到它们
// 的话每一页都报同一个数（第一版就是这么写的：七页一律报 204，那是侧栏那条
// 线的位置；第二版报 202，那是播放条那道影子的位置）。
func contentArea(sideW float64) image.Rectangle {
	return image.Rect(int(sideW)+2, navMinHeight+2, testWinW, testWinH-playbarHeight-24)
}

// pageBackground 是内容区里的底色。
//
// ⚠️ **取的是这一块里出现最多的那个颜色**，不是某一个采样点：顶栏下缘有��道
// 分隔线、侧栏右缘有另一道，而「内容区左上角那一个像素」正好落在其中一道上
// （第一版就是这么写的，七页一律报 204——那是那道线的位置）。
//
// 「最多」这个数就是底色：内容区绝大部分是空的，而画上去的东西（字、按钮、
// 表头）占的比例小得多。
func pageBackground(img *image.RGBA, area image.Rectangle) (r, g, b, a uint8) {
	count := map[uint32]int{}
	var best uint32
	bestN := -1
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			c := img.RGBAAt(x, y)
			k := uint32(c.R)<<24 | uint32(c.G)<<16 | uint32(c.B)<<8 | uint32(c.A)
			count[k]++
			if count[k] > bestN {
				bestN, best = count[k], k
			}
		}
	}
	return uint8(best >> 24), uint8(best >> 16), uint8(best >> 8), uint8(best)
}

// leftmostInk 是这一块里最靠左的「不是底色」的像素的 x。
//
// 没有就返回 -1。
func leftmostInk(img *image.RGBA, area image.Rectangle) int {
	br, bg, bb, ba := pageBackground(img, area)
	for x := area.Min.X; x < area.Max.X; x++ {
		for y := area.Min.Y; y < area.Max.Y; y++ {
			c := img.RGBAAt(x, y)
			if c.R != br || c.G != bg || c.B != bb || c.A != ba {
				return x
			}
		}
	}
	return -1
}

// inkRows 是这一块里「有墨的行」的集合（按行号）。
func inkRows(img *image.RGBA, area image.Rectangle) []int {
	br, bg, bb, ba := pageBackground(img, area)
	var rows []int
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if c.R != br || c.G != bg || c.B != bb || c.A != ba {
				rows = append(rows, y)
				break
			}
		}
	}
	return rows
}

// firstBand 是「第一段连续的有墨的行」。
//
// ⚠️ **为什么按行分段**：一页顶上第一段有墨的行就是标题那一行（下面那些内容
// 之间都隔着空行），而标题行里可能还摆着别的东西（本地音乐那一页的展示样式
// 切换、下载记录那一页的路径）——所以**行**给出的是「哪一段」，而**列**上的
// 那一段才是标题自己。
func firstBand(img *image.RGBA, area image.Rectangle) (lo, hi int, ok bool) {
	rows := inkRows(img, area)
	if len(rows) == 0 {
		return 0, 0, false
	}
	lo = rows[0]
	hi = rows[0]
	for _, y := range rows[1:] {
		if y > hi+1 {
			break
		}
		hi = y
	}
	return lo, hi, true
}

// inkColumns 是这一段行里「有墨的列」。
func inkColumns(img *image.RGBA, area image.Rectangle, lo, hi int) []bool {
	br, bg, bb, ba := pageBackground(img, area)
	cols := make([]bool, area.Max.X)
	for x := area.Min.X; x < area.Max.X; x++ {
		for y := lo; y <= hi; y++ {
			c := img.RGBAAt(x, y)
			if c.R != br || c.G != bg || c.B != bb || c.A != ba {
				cols[x] = true
				break
			}
		}
	}
	return cols
}

// firstRun 是「最左边那一段连续的墨迹」。
//
// ⚠️ **缝的门槛是 8 像素**：相邻两个汉字的墨迹之间只有 2~3 像素（20px 的字，
// advance 20 而墨迹约 17），而字与**下一个东西**之间差十几像素。所以按 8 像素
// 分段，第一段就是那行字自己。
//
// 返回的是那一段的 [起, 止]（含两端）。
func firstRun(cols []bool) (lo, hi int, ok bool) {
	x := -1
	for i, on := range cols {
		if on {
			x = i
			break
		}
	}
	if x < 0 {
		return 0, 0, false
	}
	lo = x
	hi = x
	gap := 0
	for i := x + 1; i < len(cols); i++ {
		if cols[i] {
			hi = i
			gap = 0
			continue
		}
		gap++
		if gap >= 8 {
			break
		}
	}
	return lo, hi, true
}

// runHeight 是那一段墨迹占了几个行。
//
// ⚠️ **只数那一段（lo..hi）里的行**：整块内容区扫一遍的话，标题下面那些内容
// （筛选条、输入框、表头）也数进来，量出来的是「内容有多高」而不是「字有多大」。
//
// **字号量的是它**：同一个字体、同一个字号 ⇒ 同样的墨迹高度；而不同的字号
// 差得很明显（20px 的汉字墨迹约 19 行，30px 的约 26 行）。
func runHeight(img *image.RGBA, area image.Rectangle, lo, hi, x0, x1 int) int {
	br, bg, bb, ba := pageBackground(img, area)
	n := 0
	for y := lo; y <= hi; y++ {
		for x := x0; x <= x1; x++ {
			c := img.RGBAAt(x, y)
			if c.R != br || c.G != bg || c.B != bb || c.A != ba {
				n++
				break
			}
		}
	}
	return n
}

// TestEveryPageHasTheSameHorizontalMargin 钉住「七页的横向内边距是同一个数」。
//
// # 判据是像素：内容区里最靠左的那一笔落在哪儿
//
// ⚠️ **为什么不用 `Find`**：页标题和路由容器重名（`Router.View` 结尾那句
// `pg.Label(e.title)`），`Find("稍再说看")` 给回来的是整个路由容器；而侧栏里
// 「本地音乐」「下载记录」那两项的名字又和页标题重名。所以 `Find` 在这一族上
// 认不准哪一个才是页面上那行字。
//
// 像素那一层量的是「画面上最靠左的那一笔」，而**每一页最靠左的那一样东西都
// 贴在内容区的左缘**：首页是那一排 Tab 里选中那一项的淡底，稍后再看 / 历史记录
// 是关键词输入框的左边框，本地音乐 / 下载记录 / 搜索是页标题那行字（字形自带
// 一两像素的边距，容忍两像素），收藏夹是头图区那一排里「播放全部」那颗按钮的
// 左缘。
//
// ⚠️ **容忍两像素**：字形墨迹的左边距（1~2 像素）随字体而异，而那一族要抓的
// 是 4~12 像素的差（横向页边距原来是 16 / 20 / 24 / 28 四种）。
func TestEveryPageHasTheSameHorizontalMargin(t *testing.T) {
	sideW := app.DefaultSideMenuWidth
	area := contentArea(sideW)
	// ⚠️ **期望值写的是字面的 16，不是 `pagePadX`**：用被测的同一个常量算期望值
	// 的话，把 `pagePadX` 改成 20 这一条照样绿——「取 16」这个决定就没有护栏
	// （票 #138 的评审用 `go test -overlay` 验过：改掉常量测试仍然 ok）。
	// 下面那一句把那个数本身也钉住。
	//
	// 16 是上游的 `px-4`（DESIGN.md 的 `spacing.page_horizontal`）；行里再 8，
	// 内容离内容区边缘 24。
	const wantPad = 16
	want := int(sideW) + wantPad
	if pagePadX != wantPad {
		t.Errorf("pagePadX = %d，want %d——七页的横向页边距取的是 16（上游的"+
			" `px-4`，理由见 pagerhythm.go 顶上那一段）", pagePadX, wantPad)
	}

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
	} {
		t.Run(page.name, func(t *testing.T) {
			b := newTestShell(t, page.opts...)
			s, tt := b.s, b.tt
			s.navigate(page.path)
			tt.Frame()

			got := leftmostInk(tt.Image(), area)
			if got < 0 {
				t.Fatalf("这一页上一点墨都没有，文字有 %q", tt.Texts())
			}
			if got < want-2 || got > want+2 {
				t.Errorf("%s 的内容从 x=%d 开始，want %d（侧栏 %d + 页边距 %d）——"+
					"七页的横向内边距必须是同一个数", page.name, got, want, int(sideW), wantPad)
			}
		})
	}

	// 收藏夹那一页**要等容器拉回来**：头图区第一帧是「正在加载…」，那一帧画面上
	// 最靠左的是居中的那一格（不是内容区的左缘）。等它加载完，头图区和下面
	// 那一排「播放全部」都在左缘上。
	t.Run("收藏夹", func(t *testing.T) {
		_, _, tt := collectionShellOpts(t, nil, "", "12345", mediaOf(1, "歌 一"))
		got := leftmostInk(tt.Image(), area)
		if got < 0 {
			t.Fatalf("这一页上一点墨都没有，文字有 %q", tt.Texts())
		}
		if got < want-2 || got > want+2 {
			t.Errorf("收藏夹的内容从 x=%d 开始，want %d（侧栏 %d + 页边距 %d）——"+
				"七页的横向内边距必须是同一个数", got, want, int(sideW), wantPad)
		}
	})
}

// libraryPageOpts 是「稍后再看」「历史记录」这两页要的那一摊零件：假 B 站 + 一份
// 登录态。
//
// ⚠️ **登录态是必须的**（票 #138）：这两页的内容要登录才有（`Route.RequiresLogin`），
// 没登录时页面画的是「先登录」那一支——而这一条要量的是**内容**从哪儿开始。
func libraryPageOpts(t *testing.T) []shellOpt {
	t.Helper()
	f := newLibFake(t)
	f.withLater(1, 2, "歌 一", "歌 二")
	f.withHistory(1, 0, 100, "歌 三", "歌 四")
	return []shellOpt{
		mounted(func(t *testing.T, a *app.App) {
			cli := biliForTest(t, f)
			store := app.NewMemoryStore("test")
			if err := store.Save(context.Background(), app.Session{SESSDATA: "sess"}); err != nil {
				t.Fatal(err)
			}
			a.MountLogin(store, cli)
			a.MountLibrary(cli)
		}),
		withClock(newFakeClock(now)),
	}
}

// TestEveryPageTitleUsesTheSharedSize 钉住「页标题字号是同一个数」的那一半：
// **代码里每一个页标题都调同一个常量**。
//
// # 判据是 AST，不是像素
//
// 像素那一层量的是「那一行字的墨迹有多高」，而汉字墨迹的高度和字号之间隔着
// 一个随字体而异的系数（同一个字号换个字体差一两像素）——所以**精确的那个数**
// 只能由代码里的一个调用来钉（`FontSize(pageTitleSize)`），而画面那一层
// （`TestEveryPageTitleIsTheSameSizeOnScreen`）管的是「它们看起来一样」。
//
// ⚠️ **别把 `FontSize(22)` 抄回去**：那正是这一票要消掉的东西（22 不在
// Tailwind 的刻度上，是某一页自己起的数，七页里六页抄了它）。
func TestEveryPageTitleUsesTheSharedSize(t *testing.T) {
	// 这几行字就是各页的页标题（首页没有页标题，理由见 `homepage.go`；收藏夹
	// 那一页的大标题是 30，不走这个数）。
	//
	// ⚠️ **设置页和登录页也在名单里**：它们的**边距**归 #127（设置页要收进居中
	// 那一栏），而**字号**是这一票的事——从设置页翻到下载记录，标题小两号正是
	// 这一票要消掉的那一种。
	titles := []string{"稍后再看", "历史记录", "本地音乐", "下载记录", "搜索", "设置", "扫码登录"}
	for _, title := range titles {
		if !titleUsesSharedSize(t, title) {
			t.Errorf("页标题 %q 没有用 `FontSize(pageTitleSize)`——页标题的字号必须"+
				"是同一个数（pageTitleSize = %d）", title, pageTitleSize)
		}
	}
	// 那个数本身也钉住：24（上游 `h1` 的 `text-2xl`，见 pagerhythm.go 顶上那段）。
	if pageTitleSize != 24 {
		t.Errorf("pageTitleSize = %d，want 24——上游那一套里页标题取的是 `text-2xl`"+
			"（24，DESIGN.md 的 `typography.page_title`）", pageTitleSize)
	}
}

// titleUsesSharedSize 报这一行字是不是用 `FontSize(pageTitleSize)` 画的。
//
// 判据是**那一句话**（一个 `ExprStmt`）：它长成
// `ui.Text(c, "稍后再看").FontSize(pageTitleSize).Bold()`，所以在**同一句**里
// 找那个 `FontSize(pageTitleSize)` 就够了——不用顺着调用链往上爬（那样要自己
// 建父节点表，而链上还有 `.Bold()` / `.Grow(1)` 那些，爬错了很难看出来）。
func titleUsesSharedSize(t *testing.T, title string) bool {
	t.Helper()
	found := false
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
			t.Fatalf("解析 %s 失败，这一就没法查了：%v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			stmt, ok := n.(*ast.ExprStmt)
			if !ok {
				return true
			}
			hasTitle, hasShared := false, false
			ast.Inspect(stmt, func(m ast.Node) bool {
				call, ok := m.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch sel.Sel.Name {
				case "Text":
					if mentionsTitle(call, title) {
						hasTitle = true
					}
				case "FontSize":
					if len(call.Args) == 1 {
						switch arg := call.Args[0].(type) {
						case *ast.Ident:
							if arg.Name == "pageTitleSize" {
								hasShared = true
							}
						case *ast.CallExpr:
							// `scale.Font(c, pageTitleSize)`：系统字号缩放那一层
							//（票 #139）。里面那个数仍然必须是同一个常量——这一条
							// 管的就是「七页用的是同一个数」，不是「没套缩放」。
							if sel, ok := arg.Fun.(*ast.SelectorExpr); ok &&
								sel.Sel.Name == "Font" && len(arg.Args) == 2 {
								if id, ok := arg.Args[1].(*ast.Ident); ok && id.Name == "pageTitleSize" {
									hasShared = true
								}
							}
						}
					}
				}
				return true
			})
			if hasTitle && hasShared {
				found = true
			}
			return true
		})
	}
	return found
}

// mentionsTitle 报这一次 `ui.Text` 的参数里有没有那个字。
func mentionsTitle(call *ast.CallExpr, title string) bool {
	for _, arg := range call.Args {
		lit, ok := arg.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			continue
		}
		if strings.Contains(lit.Value, title) {
			return true
		}
	}
	return false
}

// TestEveryPageTitleIsTheSameSizeOnScreen 钉住「页标题字号是同一个数」的另一半：
// **画面上那几行字一样大**。
//
// # 判据是墨迹的宽度，不是高度
//
// 墨迹的**高度**差得太小：24px 的汉字墨迹 22~23 行，20px 的 18~19 行——而同一
// 个字号的不同字（稍 / 本 / 下 / 搜）自己就差一行（笔画多的字满一些），所以高度
// 这一层只能当粗筛。
//
// 墨迹的**宽度**准得多：一行 n 个字占的宽度是 `(n-1)×字号 + 最后一个字的墨迹
// 宽 − 第一个字的边距`，后头那两项在同一套字体里是**同一个数**——所以拿基准页
// 反推那个常数，别的页字号差多少就看得出来。
//
// ⚠️ **为什么不用 `Find`**：页标题和路由容器重名（路由容器的无障碍名字就是页
// 名），`Find("稍后再看")` 给回来的是整个路由容器——那一行字自己的盒子拿不到。
//
// ⚠️ **为什么先分「带」再分「段」**：一页顶上第一段有墨的行就是标题那一行（下面
// 那些内容之间都隔着空行），而标题行里可能还摆着别的东西（本地音乐那一页的
// 展示样式切换、下载记录那一页的路径）——所以**行**给出「哪一段」，**列**上被
// 空隙分开的那一段才是标题自己（空隙的门槛是 8 像素：相邻两字之间只有 2~3
// 像素，而字和下一个东西之间差十几像素）。
//
// 精确的那个数由 `TestEveryPageTitleUsesTheSharedSize`（AST）钉住。
func TestEveryPageTitleIsTheSameSizeOnScreen(t *testing.T) {
	// 深浅两套都量（票 #166 的验收）：深色下字形和底色挨得近，量出来的墨迹
	// 边界可能不同——只出浅色的话，「深色下标题是不是同一个数」没人管。
	for _, dark := range []bool{false, true} {
		theme := "浅色"
		if dark {
			theme = "深色"
		}
		t.Run(theme, func(t *testing.T) {
			area := contentArea(app.DefaultSideMenuWidth)
			// 基准页：本地音乐那一页（它的标题是四个字，最容易量准）。
			const ref = "本地音乐"
			type measured struct {
				span   int // 墨迹从头一个字到尾一个字的宽度
				chars  int
				height int // 墨迹占了几行
			}
			got := map[string]measured{}
			for _, page := range []struct {
				name  string
				path  string
				chars int
				opts  []shellOpt
			}{
				{name: "稍后再看", path: laterPath, chars: 4, opts: libraryPageOpts(t)},
				{name: "历史记录", path: historyPath, chars: 4, opts: libraryPageOpts(t)},
				{name: ref, path: localMusicPath, chars: 4},
				{name: "下载记录", path: downloadListPath, chars: 4},
				{name: "搜索", path: searchPath + "?q=%E5%A4%9C%E7%A9%BA", chars: 2},
				// 设置页和登录页也在这一族里：它们的页标题同样是 `pageTitleSize`
				// （票 #166——「所有页面的大标题 24/400」）。
				{name: "设置", path: settingsPath, chars: 2},
				{name: "扫码登录", path: loginPath, chars: 4},
			} {
				b := newTestShell(t, append(page.opts, systemIsDark(dark))...)
				s, tt := b.s, b.tt
				s.navigate(page.path)
				tt.Frame()

				img := tt.Image()
				lo, hi, ok := firstBand(img, area)
				if !ok {
					t.Fatalf("%s：内容区顶上一点墨都没有，文字有 %q", page.name, tt.Texts())
				}
				x0, x1, ok := firstRun(inkColumns(img, area, lo, hi))
				if !ok {
					t.Fatalf("%s：标题那一行里没有墨，文字有 %q", page.name, tt.Texts())
				}
				got[page.name] = measured{
					span:   x1 - x0 + 1,
					chars:  page.chars,
					height: runHeight(img, area, lo, hi, x0, x1),
				}
			}

			// 基准页的墨迹宽度折算成字号：`3×字号 + 常数`。
			refm := got[ref]
			const want = 24
			k := refm.span - (refm.chars-1)*want
			heights := map[string]int{}
			for name, m := range got {
				heights[name] = m.height
			}
			for name, m := range got {
				if m.chars < 3 {
					// ⚠️ **两个字的那一页量不准**：墨迹宽度里那个「常数」是**这一页
					// 的字自己的**（最后一个字的墨迹宽 − 第一个字的边距），除以 (n-1)
					// 之后那一页的误差就是常数本身（一两个字的时候是 ±1~2 像素，折算
					// 到字号上就是 2 像素）。它由下面那条「墨迹高度一样」的粗筛管着，
					// 而精确的那一半由 AST 那一条钉住。
					continue
				}
				size := float64(m.span-k) / float64(m.chars-1)
				if size < want-1 || size > want+1 {
					t.Errorf("%s：「%s」的标题折算出来是 %.1fpx（墨迹宽 %d，%d 个字），"+
						"want %d（±1）——页标题的字号必须是同一个数",
						theme, name, size, m.span, m.chars, want)
				}
			}

			// 粗筛：各页的标题墨迹高度一样（容忍一行——同一个字号的不同字自己就差一行）。
			for name, h := range heights {
				for other, oh := range heights {
					if name == other {
						continue
					}
					if oh != h && oh != h+1 && oh != h-1 {
						t.Errorf("%s：「%s」的标题墨迹 %d 行，「%s」是 %d 行——页标题的"+
							"字号必须是同一个数", theme, name, h, other, oh)
					}
				}
			}
		})
	}
}

// blockedBiliOpts 是一台**永远不回**的假 B 站：除了 nav 以外，所有请求都卡住。
//
// 用来造「还在加载」那一档：页面发出去的那一趟没回来，它就一直停在加载态。
//
// ⚠️ **nav 必须回**：WBI 的密钥是从它那儿拿的，而后面每一个请求都要签名——nav
// 也卡住的话，第一个请求根本发不出去。
func blockedBiliOpts(t *testing.T, loggedIn bool) []shellOpt {
	t.Helper()
	hold := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/x/web-interface/nav" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"0","ttl":1,"data":{"isLogin":true,
			  "wbi_img":{"img_url":"https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png",
			             "sub_url":"https://i0.hdslb.com/bfs/wbi/4932caff0ff746aeb6f01bf08b70ac45.png"}}}`))
			return
		}
		<-hold
	}))
	t.Cleanup(func() { close(hold); srv.Close() })

	transport := &app.CredentialTransport{
		Base:   srv.Client().Transport,
		Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOfFixture(srv.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport})
	cli.TestOnlyPointHostsAtFixture(srv.URL)
	return []shellOpt{
		mounted(func(t *testing.T, a *app.App) {
			store := app.NewMemoryStore("test")
			if loggedIn {
				if err := store.Save(context.Background(), app.Session{SESSDATA: "sess"}); err != nil {
					t.Fatal(err)
				}
			}
			a.MountLogin(store, cli)
			if loggedIn {
				a.TestOnlyAdoptLogin("77")
			}
			a.MountLibrary(cli)
			a.MountExplore(cli)
			a.MountCollections(app.CollectionRefs{Client: cli, Settle: 0})
		}),
	}
}

// TestEveryLoadingStateIsDrawnTheSameWay 钉住「八个页面的「加载中」走同一个出口」。
//
// # 判据是画面：那一行字的字号
//
// ⚠️ **不是断函数名**：这一条量的是「加载中」那一行字**画出来是什么形状**——
// 同一个字号（15px 那一档，和「没有结果」那一行字一样）。八个页面都是同一个数，
// 所以它们是同一处构造出来的。
//
// # 怎么进到加载态
//
// **一台永远不回的假 B 站**（`blockedBiliOpts`）：页面发出去的那一趟没回来，它
// 就一直停在加载态——比「只画一帧」可靠得多（那一帧赌的是「后台还没回来」，
// 机器一忙就输了）。
//
// ⚠️ **本地音乐那一页不在这一族里**：它扫的是硬盘不是网络，而扫一个空目录
// 一帧之内就完事——**真机上的加载态也是一闪而过**（库大了才看得见）。那一页
// 的加载态由 `TestTheLocalMusicLoadingComesFromTheSharedDrawer`（代码那一层）
// 钉住。
func TestEveryLoadingStateIsDrawnTheSameWay(t *testing.T) {
	for _, page := range []struct {
		name    string
		loading string
		path    string
	}{
		{name: "搜索", loading: "正在加载…", path: searchPath + "?q=%E5%91%A8%E6%9D%B0%E4%BC%A6"},
		{name: "收藏夹", loading: "正在加载…", path: collectionPathPrefix + "12345"},
		{name: "稍后再看", loading: "正在加载…", path: laterPath},
		{name: "历史记录", loading: "正在加载…", path: historyPath},
		{name: "我的关注", loading: "正在加载关注的人…", path: followPath},
		{name: "用户动态", loading: "正在加载动态…", path: dynamicFeedPath},
		{name: "UP 主空间", loading: "正在加载动态…", path: "/user/77"},
	} {
		t.Run(page.name, func(t *testing.T) {
			b := newTestShell(t, blockedBiliOpts(t, true)...)
			s, tt := b.s, b.tt
			s.navigate(page.path)
			tt.Frame()
			// 多画两帧，让后台那一趟真的发出去（页面是在第一帧发的）。
			tt.Frame()
			tt.Frame()

			r, ok := tt.Find(page.loading)
			if !ok {
				t.Fatalf("%s 没有说「%s」，文字有 %q", page.name, page.loading, tt.Texts())
			}
			// 字号：15px 那一档的行高是 17.67（`emptyTitleSize` × 字体行高）。
			if r.H < 16 || r.H > 19 {
				t.Errorf("%s 的「%s」那一行高 %.2f，want 17.7（15px 那一档）——"+
					"「加载中」和「没有结果」必须走同一个出口", page.name, page.loading, r.H)
			}
		})
	}
}

// TestTheLoadingStateLooksLikeTheEmptyState 断「同一页上，「加载中」和「没有
// 结果」是同一个格子」。
//
// 判据是画面：两行字**一样高**（同一个字号）、**居中的位置一样**（同一个格子）。
func TestTheLoadingStateLooksLikeTheEmptyState(t *testing.T) {
	st := newSearchStub(2, 0)
	st.hold("周杰伦", 1)
	_, tt := newSearchPage(t, "周杰伦", st)
	tt.Frame()

	loading, ok := tt.Find("正在加载…")
	if !ok {
		t.Fatalf("没有说「正在加载…」，文字有 %q", tt.Texts())
	}
	// 放行，让它变成「没有结果」。
	st.release("周杰伦", 1)
	waitFrames(t, tt, "结果回来了（没有结果那一支）", func() bool {
		return tt.HasText("暂无内容")
	})
	empty, ok := tt.Find("暂无内容")
	if !ok {
		t.Fatalf("没有说「暂无内容」，文字有 %q", tt.Texts())
	}

	if loading.H != empty.H {
		t.Errorf("「正在加载…」高 %.2f，「暂无内容」高 %.2f——同一页上这两句话必须"+
			"是同一个格子（同一个出口）", loading.H, empty.H)
	}
	lc, ec := loading.X+loading.W/2, empty.X+empty.W/2
	if lc < ec-2 || lc > ec+2 {
		t.Errorf("「正在加载…」居中在 %.1f，「暂无内容」居中在 %.1f——"+
			"同一页上这两句话得在同一个格子里", lc, ec)
	}
}

// TestTheMainTabsAndTheSortTabsAreNotTheSame 钉住「主 Tab 与次级 Tab 不是同一个
// 样子」。
//
// # 判据是像素：那一排下面有没有那条主色的横条
//
// 框架的 `ui.Tabs` 是**下划线**（选中那一项底下画一条主色的横条，
// `mygo@v0.5.5/ui/tabs.go` 的 `coreTabs`），而主 Tab 用的是我们自己那一套
// **扁平**样式（`tabs.go` 的 `flatTabs`）——那条横条**没有**。
//
// 搜索页正好两种都有：上面那一排是主 Tab（歌曲 / 视频 / 用户），下面那一排是
// 次级排序（综合排序 / 最多播放 / …）。所以这一条在**同一页上**比。
//
// ⚠️ **只看「有没有主色的横条」不看别的**：主 Tab 那一项选中时有一层淡底
// （浅色下是主色很淡的一层，深色下是中性灰），而它是**一整块**、不是一条
// 2 像素的横条——而且它的颜色和主色**不一样**（淡底是主色掺出来的），
// 所以按「逐字等于主色」去扫不会把它算进去。
func TestTheMainTabsAndTheSortTabsAreNotTheSame(t *testing.T) {
	st := newSearchStub(2, 0)
	_, tt := newSearchPage(t, "周杰伦", st)
	tt.Frame()

	accent := BuildTheme(app.DefaultSettings(""), false).Accent
	img := tt.Image()

	main, ok := tt.Find("歌曲")
	if !ok {
		t.Fatalf("画面上找不到主 Tab「歌曲」，文字有 %q", tt.Texts())
	}
	sort, ok := tt.Find("综合排序")
	if !ok {
		t.Fatalf("画面上找不到次级排序「综合排序」，文字有 %q", tt.Texts())
	}

	if hasAccentBar(img, main, accent) {
		t.Errorf("主 Tab 那一排下面有那条主色的横条——主 Tab 要的是扁平那一种" +
			"（见 tabs.go 的 flatTabs），不是下划线")
	}
	if !hasAccentBar(img, sort, accent) {
		t.Errorf("次级排序那一排下面没有那条主色的横条——次级排序保持框架那一套" +
			"下划线（`ui.Tabs`），和主 Tab 分成两种样子")
	}
}

// hasAccentBar 报这个文字下面有没有一条主色的横条。
//
// 扫的是**文字那一行下面十像素**：框架把横条画在 Tab 那个盒子（文字 + 内边距）
// 的最下面，也就是文字下面 8 像素左右。
func hasAccentBar(img *image.RGBA, label mygoui.Rect, accent mygoui.Color) bool {
	want := rgbaOf(accent)
	for y := int(label.Y + label.H); y <= int(label.Y+label.H)+10; y++ {
		for x := int(label.X); x < int(label.X+label.W); x++ {
			if y < 0 || x < 0 || y >= img.Bounds().Dy() || x >= img.Bounds().Dx() {
				continue
			}
			if img.RGBAAt(x, y) == want {
				return true
			}
		}
	}
	return false
}

// TestTheSearchPageShowsTheCurrentKeyword 钉住「搜索页的输入框显示当前关键词」。
//
// # 三件事
//
//  1. 从顶栏搜完点进来，框里是**那个词**（原来框是空的，而下面已经显示着
//     「搜索【夜空中最亮的星】的结果」和一屏结果行——框里的词和它下面正在显示
//     的那批歌对不上）。
//  2. 画面上框里画的是那个词（不是那句占位「歌名或 UP 主」）。
//  3. **切页回来不覆盖用户正在敲的字**。
func TestTheSearchPageShowsTheCurrentKeyword(t *testing.T) {
	st := newSearchStub(2, 0)
	s, tt := newSearchPage(t, "周杰伦", st)
	tt.Frame()

	if s.searchDraft != "周杰伦" {
		t.Errorf("搜索页的输入框里是 %q，want「周杰伦」——框里要显示当前关键词",
			s.searchDraft)
	}
	// 画面上：框里画的是那个词（正文色），不是占位那句（次要色）。
	box, ok := tt.Find("搜索歌名或 UP 主")
	if !ok {
		t.Fatalf("画面上找不到那个输入框，文字有 %q", tt.Texts())
	}
	if !boxHasTextColor(tt, box) {
		t.Errorf("那个输入框里没有正文色的字——框里画的是占位那句，而不是当前关键词")
	}

	// ③ 切页回来不覆盖用户正在敲的字。
	tt.Click("搜索歌名或 UP 主")
	tt.Type("伦")
	tt.Frame()
	typed := s.searchDraft
	if !strings.Contains(typed, "伦") {
		t.Fatalf("敲进去的字没进框里：%q", typed)
	}
	s.navigate(homePath)
	tt.Frame()
	s.navigate(searchPath + "?" + searchQuery + "=" + "周杰伦")
	tt.Frame()
	if s.searchDraft != typed {
		t.Errorf("切页回来之后框里是 %q，want %q——用户正在敲的字不许被覆盖",
			s.searchDraft, typed)
	}

	// ④ 🔴 **框被清空的时候不许弹回来**（票 #138 的评审抓出来的那一条）。
	//
	// 判据里多一条「框空着也写」的话，用户**还在这一页、正在删字**的时候就撞上
	// 了：他把框里的字删干净 → 那一帧草稿是空串 → 关键词被立刻写回去 → 框**清
	// 不掉**（自己弹回来了），而他接着敲的字被**粘在旧词后面**（「林周杰伦」，
	// 回车就按这个搜）。
	//
	// 删着改是更常见的做法（实测：全选 + 直接重打能绕过去，而删着改绕不过去）。
	tt.Click("搜索歌名或 UP 主")
	tt.Key(mygoui.Cmd, mygoui.KeyA) // 全选
	tt.Key(0, mygoui.KeyBackspace)  // 删掉
	tt.Frame()
	if s.searchDraft != "" {
		t.Errorf("把框里的字删干净之后，框里又变回 %q——框清不掉，而用户接着敲的"+
			"字会被粘在旧词后面", s.searchDraft)
	}
	// 接着敲的那个字**只**是他敲的那个。
	tt.Type("林")
	tt.Frame()
	if s.searchDraft != "林" {
		t.Errorf("删干净之后接着敲「林」，框里是 %q，want「林」", s.searchDraft)
	}
}

// boxHasTextColor 报这个盒子里有没有正文色的字。
//
// 输入框里**有字**的时候画的是正文色（`t.Text`），**空着**的时候画的是那句
// 占位，而占位是次要色（`t.TextMuted`）——所以按颜色分得清。
func boxHasTextColor(tt *mygoui.Tester, box mygoui.Rect) bool {
	want := rgbaOf(BuildTheme(app.DefaultSettings(""), false).Text)
	img := tt.Image()
	for y := int(box.Y); y < int(box.Y+box.H); y++ {
		for x := int(box.X); x < int(box.X+box.W); x++ {
			if y < 0 || x < 0 || y >= img.Bounds().Dy() || x >= img.Bounds().Dx() {
				continue
			}
			if img.RGBAAt(x, y) == want {
				return true
			}
		}
	}
	return false
}

// TestThePagesThatNeedLoginSaySo 钉住「未登录时出现登录提示，且不出现空列表」。
//
// 三页：我的关注 / 稍后再看 / 历史记录。**一个请求都不发**（那几个接口全都要
// Cookie，没登录时它们一律回 -101，而 -101 和「网络断了」一模一样）。
func TestThePagesThatNeedLoginSaySo(t *testing.T) {
	for _, page := range []struct {
		name string
		path string
	}{
		{name: "我的关注", path: followPath},
		{name: "稍后再看", path: laterPath},
		{name: "历史记录", path: historyPath},
	} {
		t.Run(page.name, func(t *testing.T) {
			// ⚠️ **B 站客户端要挂着、但不登录**：没挂上的时候页面画的是「还没接上
			// B 站接口」那一支（那一句更具体，见 `later.go` 的注释），而这里要
			// 验的是「挂了接口、但没登录」那一种。
			b := newTestShell(t, blockedBiliOpts(t, false)...)
			s, tt := b.s, b.tt
			s.navigate(page.path)
			tt.Frame()
			tt.Frame()

			if !tt.HasText("还没登录") {
				t.Errorf("%s 没登录时没说「还没登录」，文字有 %q", page.name, tt.Texts())
			}
			if !tt.HasText("去登录") {
				t.Errorf("%s 没登录时没给「去登录」那一步，文字有 %q", page.name, tt.Texts())
			}
			// 空列表那一句**不许**出现：用户要的是「先登录」，不是「这里没有」。
			if empty := lookup(page.path).Empty.Title; tt.HasText(empty) {
				t.Errorf("%s 没登录时说的是「%s」——用户登录完发现还是空的，"+
					"会以为是登录坏了", page.name, empty)
			}
			// 那一页的表头 / 筛选 / 关键词框也都不该在：内容那一块整个不摆。
			if tt.HasText("搜索标题/UP主名称") {
				t.Errorf("%s 没登录时还摆着关键词框——内容那一块整个不该出现", page.name)
			}
		})
	}
}

// TestTheCollectionTitleIsTheBigOne 钉住「收藏夹页大标题 22 → 30」。
//
// # 两件事
//
//  1. **那个数是 30**（上游 `text-3xl`），而页标题是 20——两处不是同一个数。
//  2. **两行标题放得下**：封面高度写死 168，而头图区那一列是「两行标题 + 简介 +
//     类型和条数 + 创建者」。标题一大，这一列就可能高过封面——而那一列是**靠左**
//     的，高过封面的话它会从封面底下探出来，头图区也就不是 168 那一高了。
//
// 判据是**画面**：头图区那一列的最后一行（创建者）的底边不许越过封面的底边。
func TestTheCollectionTitleIsTheBigOne(t *testing.T) {
	if collectionTitleSize != 30 {
		t.Errorf("collectionTitleSize = %d，want 30（上游 `text-3xl`）", collectionTitleSize)
	}
	// 一个长到要折两行的名字。
	long := "这是一个很长很长很长很长很长很长很长很长很长很长的收藏夹名字"
	_, s, tt := collectionShellWithTitle(t, long)
	_ = s

	// 先确认那个名字**真的折成了两行**（否则下面那条「放得下」验的是一行）——
	// 30px 的一行是 35.3 高，两行是 70.6。
	title, ok := tt.Find(long)
	if !ok {
		t.Fatalf("头图区里找不到那个大标题，文字有 %q", tt.Texts())
	}
	if title.H < 60 {
		t.Fatalf("那个名字没有折成两行（高 %.1f）——下面那条「放得下」就成了一句空话", title.H)
	}

	owner, ok := tt.Find("UP 小明")
	if !ok {
		t.Fatalf("头图区里找不到创建者那一行，文字有 %q", tt.Texts())
	}
	// 封面的底边：头图区从内容区顶上往下 20（`viewCollectionHeader` 的上内边距），
	// 封面 168 高。
	coverBottom := float32(navMinHeight) + 20 + collectionCoverH
	if owner.Y+owner.H > coverBottom {
		t.Errorf("两行标题把头图区那一列撑到 %.0f，越过了封面的底边 %.0f——"+
			"标题一大，这一列就从封面底下探出来了", owner.Y+owner.H, coverBottom)
	}
}

// collectionShellWithTitle 造一个收藏夹外壳，收藏夹的名字是 title。
func collectionShellWithTitle(t *testing.T, title string, extra ...shellOpt) (*favBili, *Shell, *mygoui.Tester) {
	t.Helper()
	b := newCollectionBili(t)
	b.setMedias(mediaOf(1, "歌 一"))
	b.infos["title"] = title
	opts := []shellOpt{withSettings(app.DefaultSettings(""))}
	opts = append(opts, extra...)
	transport := &app.CredentialTransport{
		Base:   b.Server.Client().Transport,
		Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOfFixture(b.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport, CSRF: func() string { return "jct" }})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	opts = append(opts, mounted(func(_ *testing.T, a *app.App) {
		a.MountCollections(app.CollectionRefs{Client: cli, Settle: 0})
	}))
	s, tt := newShell(t, opts...)
	s.app.SetWake(s.wake.invalidate)
	s.navigate(collectionPathPrefix + "12345")
	waitFrames(t, tt, "容器元信息加载完", func() bool {
		return s.collection != nil && s.app.Collection(s.collection.ref).Loaded
	})
	return b, s, tt
}

// TestTheSuggestTermsShowWhichCharsWereHit 钉住「搜索联想浮层里把已命中的那几个字
// 标出来」。
//
// # 判据是画面：命中那两个字是上游那个粉
//
// 上游把 B 站回包里命中的部分染成粉色（回包里的 `name` 把命中的字包在 `<em>`
// 里，上游渲染成粉色，`src/app.css` 的 `.suggest_high_light`），而我们取的是
// 干净的 `term`——十行全是同一种灰，用户得逐行读完才知道哪条对得上自己敲的那
// 几个字。
//
// ⚠️ **命中段是自己算的**（拿用户已经敲的词在 `term` 里找），而**服务层取
// `name` 那一条不许改**：拿带标签的 `name` 去搜，等于把 `<em>` 当关键词发出去，
// 真会搜错。
func TestTheSuggestTermsShowWhichCharsWereHit(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setSuggestTerms("周杰", "周杰伦", "周杰伦 晴天")
	s, tt := newSuggestShell(t, f)
	tt.Frame()
	typeInSearch(t, tt, "周杰")
	settleSuggestDebounce(s, tt)
	waitFrames(t, tt, "联想词出来了", func() bool {
		return tt.HasText("周杰伦")
	})

	// 命中段那两个字是上游那个粉（浅色底下压暗一档，见 `suggestHitColor`）。
	hit := suggestHitColor(BuildTheme(app.DefaultSettings(""), false))
	want := rgbaOf(hit)
	img := tt.Image()
	n := 0
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if img.RGBAAt(x, y) == want {
				n++
			}
		}
	}
	if n < 20 {
		t.Errorf("浮层里那个粉一个像素都没有（want ≥ 20）——「周杰」那两个字"+
			"没有被标出来，文字有 %q", tt.Texts())
	}
}

// TestTheSmallCopyHasAFixedLineHeight 钉住那九处小字的行高是**定好的**。
//
// # 判据是 AST，不是像素
//
// 像素那一层量的是「这一行有多高」，而不写行高时这一行有多高是**字体决定**的
// （同一个字号在不同字体下差几个像素）——所以要钉的是「**行高是写死的那个数**」，
// 而那是代码里的一个调用（`LineHeight(...)`）。
//
// # 那九处
//
// 空状态第二句 / 收藏夹的简介 / 收藏夹的「公开收藏夹 • N 条视频」 / 收藏夹的
// 创建者 / 下载记录那一行的路径 / 本地音乐的「有目录读不到」/ 本地音乐的
// 「这个目录里有我们还放不出的格式」/ 日期筛选的摘要 / 封面那一格下面那句提示。
//
// ⚠️ **11px 的那一句行高给大一点**（`smallCopyHintLeading` = 1.7）：11 × 1.5
// = 16.5，而旁边 12px 的那些是 18——一行字的高度差两像素，弹窗里那几行看着就
// 错开了。
func TestTheSmallCopyHasAFixedLineHeight(t *testing.T) {
	for _, place := range []struct {
		file string
		// key 是那一行字在源码里的样子（`ui.Text` 的第一个参数）。
		key string
		// want 是那个 `LineHeight(...)` 该调用的常量。
		want string
	}{
		{file: "empty.go", key: "e.Hint", want: "smallCopyLeading"},
		{file: "collectionpage.go", key: "info.Intro", want: "smallCopyLeading"},
		{file: "collectionpage.go", key: "collectionKindText()", want: "smallCopyLeading"},
		{file: "collectionpage.go", key: "info.OwnerName", want: "smallCopyLeading"},
		{file: "downloadlist_ops.go", key: "path", want: "smallCopyLeading"},
		{file: "localmusic.go", key: "有目录读不到：", want: "smallCopyLeading"},
		{file: "localmusic.go", key: "这个目录里有我们还放不出的格式（", want: "smallCopyLeading"},
		{file: "daterange.go", key: "f.summary()", want: "smallCopyLeading"},
		{file: "covermodal.go", key: "coverHint", want: "smallCopyHintLeading"},
	} {
		if !smallCopyHasLineHeight(t, place.file, place.key, place.want) {
			t.Errorf("%s 的 %q 那一行字没有调 `LineHeight(%s)`——小字的行高必须是"+
				"定好的", place.file, place.key, place.want)
		}
	}
}

// smallCopyHasLineHeight 报那个文件里那一句字有没有调 `LineHeight(want)`。
//
// 找法：**一句**（一个 `ExprStmt`）里同时有 `ui.Text(...)`（认得出是那一行字）
// 和 `LineHeight(...)`（认得出调的是哪个数）——所以不用顺着调用链往上爬。
func smallCopyHasLineHeight(t *testing.T, file, key, want string) bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("解析 %s 失败：%v", file, err)
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		stmt, ok := n.(*ast.ExprStmt)
		if !ok {
			return true
		}
		hasText, hasLeading := false, false
		ast.Inspect(stmt, func(m ast.Node) bool {
			call, ok := m.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "Text":
				// ⚠️ **看的是所有参数**：第一个是 `c`（那个上下文），字在后面。
				for _, arg := range call.Args {
					if exprKey(arg) == key {
						hasText = true
					}
				}
			case "LineHeight":
				if len(call.Args) == 1 {
					if id, ok := call.Args[0].(*ast.Ident); ok && id.Name == want {
						hasLeading = true
					}
				}
			}
			return true
		})
		if hasText && hasLeading {
			found = true
		}
		return true
	})
	return found
}

// exprKey 把一个表达式变成一个能认出来的键。
func exprKey(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.BasicLit:
		// 字面量带上那对引号不好认（源码里写的是 `"有目录读不到："`），去掉。
		return strings.Trim(v.Value, `"`)
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprKey(v.X) + "." + v.Sel.Name
	case *ast.CallExpr:
		return exprKey(v.Fun) + "()"
	case *ast.BinaryExpr:
		return exprKey(v.X)
	}
	return ""
}

// TestRenderPageRhythmShots 把这一票改过的那几页存成 PNG。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderPageRhythmShots
//
// # 深浅两套都要出
//
// 这一族看的全是**底色上的深浅**（主 Tab 那一块淡底、下划线那一道横条、
// 那个粉），只出浅色的话深色下糊成一片看不出来。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染。
func TestRenderPageRhythmShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// 60 搜索页：框里是当前关键词（不是空框），上面那一排是扁平的主 Tab，
			// 下面那一排是下划线的次级排序——两排不是同一个样子。
			st := newSearchStub(2, 4)
			b := newTestShell(t, systemIsDark(dark))
			s, tt := b.s, b.tt
			st.install(s)
			s.navigate(searchPath + "?" + searchQuery + "=" + "周杰伦")
			waitFrames(t, tt, "搜索结果出来了", func() bool {
				return tt.HasText("搜索【周杰伦】的结果")
			})
			tt.Move(1180, 300)
			homeFrame(tt)
			save(t, dir, "60-搜索页-"+name+".png", tt)

			// 61 收藏夹：头图区那个 30px 的大标题（下面那一排是扁平的主 Tab）。
			_, s2, tt2 := collectionShellWithTitle(t, "通勤路上", systemIsDark(dark))
			_ = s2
			tt2.Move(1180, 300)
			homeFrame(tt2)
			save(t, dir, "61-收藏夹-"+name+".png", tt2)

			// 62 设置页：分组标题 20/400、行标题 16/500、说明 14、组间 24
			// （票 #166）。深浅两套都出——这三档字号在深色下离得更近。
			b3 := newTestShell(t, systemIsDark(dark))
			s3, tt3 := b3.s, b3.tt
			s3.navigate(settingsPath)
			tt3.Frame()
			tt3.Move(1180, 300)
			homeFrame(tt3)
			save(t, dir, "62-设置页-"+name+".png", tt3)
		})
	}
}

// TestTheLocalMusicLoadingComesFromTheSharedDrawer 钉住「本地音乐那一页的「正在
// 扫描…」也走共享出口」。
//
// # 为什么这一条是代码那一层的，不是画面那一层
//
// 那一页扫的是**硬盘**不是网络，而扫一个空目录**一帧之内就完事**——真机上的
// 加载态也是一闪而过（库大了才看得见）。所以画面那一层抓不住它（第一帧画出来
// 的时候它可能已经扫完了）。
//
// 所以这一条钉的是**代码**：那一行字必须由 `drawEmpty` 画（和这一页的「没有
// 结果」是同一个出口），而且它必须用 `loadingState` 那一档（**不是**一个空状态
// ——「还没好」和「没有」是两件事，票 #138 的两票边界就在这个标志上）。
func TestTheLocalMusicLoadingComesFromTheSharedDrawer(t *testing.T) {
	if !statementHasCall(t, "localmusic.go", "正在扫描…", "drawEmpty") {
		t.Errorf("本地音乐那一页的「正在扫描…」不是由 `drawEmpty` 画的——" +
			"「加载中」和「没有结果」必须走同一个出口")
	}
	if !statementHasCall(t, "localmusic.go", "正在扫描…", "loadingState") {
		t.Errorf("本地音乐那一页的「正在扫描…」没有用 `loadingState` 那一档——" +
			"「还没好」和「没有」是两件事")
	}
	// 同一页的「没有结果」也走那一个出口（这一条是「同一个出口」那一半）。
	if !statementHasCall(t, "localmusic.go", "local:empty", "drawEmpty") {
		t.Errorf("本地音乐那一页的「没有结果」不是由 `drawEmpty` 画的——" +
			"「加载中」和「没有结果」必须走同一个出口")
	}
}

// statementHasCall 报那个文件里「提到 key 的那一句」有没有调 funcName。
//
// 判据是**一句**（一个 `ExprStmt`）：本地音乐那一页的「正在扫描…」和它的
// 「没有结果」各自是一句，两句都调 `drawEmpty` 才叫「同一个出口」。
func statementHasCall(t *testing.T, file, key, funcName string) bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("解析 %s 失败：%v", file, err)
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		stmt, ok := n.(*ast.ExprStmt)
		if !ok {
			return true
		}
		hasKey, hasCall := false, false
		ast.Inspect(stmt, func(m ast.Node) bool {
			call, ok := m.(*ast.CallExpr)
			if !ok {
				return true
			}
			if callName(call) == funcName {
				hasCall = true
			}
			for _, arg := range call.Args {
				if mentionsKey(arg, key) {
					hasKey = true
				}
			}
			return true
		})
		if hasKey && hasCall {
			found = true
		}
		return true
	})
	return found
}

// callName 是这一次调用调的是谁（`drawEmpty` / `ui.Text` 都认）。
func callName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

// mentionsKey 报这个表达式里有没有那个字（字符串字面量）。
//
// ⚠️ **往里找**：本地音乐那一页的「正在扫描…」是 `loadingState("正在扫描…")` 的
// 参数，而 `loadingState(...)` 本身是一个调用——只看最外层那一层的参数就找不到它。
func mentionsKey(e ast.Expr, key string) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if ok && lit.Kind == token.STRING && strings.Trim(lit.Value, `"`) == key {
			found = true
		}
		return true
	})
	return found
}
