package ui

import (
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 这一票改的是**画出来的像素**，而「描边没了」这件事在代码里看不出来：换成
// `ui.Button` 一样编译得过、一样全绿，只有画面上多了那圈 1px 的线。所以下面这几条
// 全部读像素。
//
// 三条分别钉三件事：**没有描边**、**没有常驻底色**（合起来是第一条，它们用同一个
// 探针：框内那一块必须和框外那一块是同一个颜色）、**反馈还在**、**选中是一层很淡
// 的底而不是实心主色**。
//
// ⚠️ **深浅两套主题都要跑**：调研明写了「深色下差距比浅色更明显」（报告 §2
// 基线 D）——深色下那圈描边（`t.Border`，票 #172 之后是 15% 白叠出来的
// `#3b3b3d`）压在 `#18181b` 上是一道亮线，浅色下它几乎看不出来。只跑浅色的话，
// 这一票最该修的那一面根本没被验过。

// probeGap 是探针离框线多远。
//
// ⚠️ **3 像素不是随手挑的**：框线内侧 1 像素是描边，外侧 2~3 像素是这一格背后
// 那一层。而探针取在这个距离上还顺带躲开了圆角——圆角那一段是底色和它身后那一层
// 的连续过渡，两三个像素宽（`icons_test.go` 的 `scanGlyph` 记着同一件事，它为此
// 内缩了 `inkInset`）。再往外就会量到邻居控件的边，再往里就量到描边本身。
const probeGap = 3

// probeTolerance 是「算同一个颜色」的容差。
//
// 3 是留给抗锯齿的，不是留给主题色差的：这一票要断的两处差别最小的那一个是顶栏
// 按钮的常驻底色（`t.Surface` #f4f4f5 压在窗口底色 #ffffff 上，通道差 11），
// 描边那处更大（票 #172 之后浅色分隔线是 #dbdbdb，对 #f4f4f5 通道差 25/26）。
// 所以 3 抓得住这两处，而不会把「差两三个色阶的那点抗锯齿」当成差距。
const probeTolerance = 3

// probeBand 取一块矩形里的**众数**颜色。
//
// ⚠️ **用众数而不是某一个点**：按钮框里本来就有字和图标，一个点落在笔画上是必然的
// （量在居中的文字上会得到一个 197/244 这种中间色），而众数在几百个像素里取的是
// 「这一格真正的底」。
func probeBand(img *image.RGBA, x0, y0, x1, y1 int) color.RGBA {
	var s []color.RGBA
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			s = append(s, img.RGBAAt(clampX(img, x), clampY(img, y)))
		}
	}
	if len(s) == 0 {
		return color.RGBA{}
	}
	return modeOf(s)
}

// probeFill 是这一格按钮**里面**那一大块的底色（内缩 probeGap，所以不含描边）。
func probeFill(img *image.RGBA, r mygoui.Rect) color.RGBA {
	x0, y0, w, h := round32(r.X), round32(r.Y), int(r.W), int(r.H)
	return probeBand(img, x0+probeGap, y0+probeGap, x0+w-1-probeGap, y0+h-1-probeGap)
}

// probeBehind 是这一格按钮**背后**那一块的底色（外扩 probeGap）。
//
// ⚠️ 它同时充当「有没有描边」和「有没有常驻底色」两处的参照——两处要判的正是
// 「框里的东西和框外的东西是不是同一个颜色」。
func probeBehind(img *image.RGBA, r mygoui.Rect) color.RGBA {
	x0, y0, w, h := round32(r.X), round32(r.Y), int(r.W), int(r.H)
	return probeBand(img, x0-probeGap, y0-probeGap, x0+w-1+probeGap, y0+h-1+probeGap)
}

// probeEdge 是贴着框**上边**那一行（含内外各一行）与众数的差别。
//
// ⚠️ **只取上下两条边、不取左右两条**：左右两边在按钮正中，而那里正是居中的字和
// 图标。上下两边按横向中点取——那里既没有角（圆角只吃掉左右两端）也没有内容
// （按钮上下留白 6 像素）。
//
// ⚠️ **上下各多看一行**：框是 `Tester` 给的小数坐标，四舍五入可能差一像素，只看
// 框线那一行的话会「往外偏了一像素 → 读到框外那一块 → 明明有描边却过关」。
// 内缩一行、外扩一行都看过，有描边就一定被抓住。
//
// 返回的是差值而不是颜色，好让报错那一行直接说「差了多少」。
func probeEdge(img *image.RGBA, r mygoui.Rect) int {
	x0, y0, w, h := round32(r.X), round32(r.Y), int(r.W), int(r.H)
	xc := x0 + w/2
	worst := 0
	for _, edge := range []int{y0, y0 + h - 1} {
		for _, row := range []int{edge - 1, edge, edge + 1} {
			// 参照是同一列上、离框线 probeGap+1 的那一行——同一个横坐标下唯一
			// 不可能被这一格的描边或底色碰到的地方。
			ref := probeBand(img, xc-3, edge-probeGap-1, xc+3, edge-probeGap-1)
			if d := maxDiff(probeBand(img, xc-3, row, xc+3, row), ref); d > worst {
				worst = d
			}
		}
	}
	return worst
}

// maxDiff 是两个颜色差得最多的那一道通道。
func maxDiff(a, b color.RGBA) int {
	d := func(x, y uint8) int {
		if x > y {
			return int(x) - int(y)
		}
		return int(y) - int(x)
	}
	return max(d(a.R, b.R), max(d(a.G, b.G), d(a.B, b.B)))
}

// assertFlat 断「这一格按钮和自己的背景是同一块颜色」——没有描边，也没有常驻底色。
//
// 两条判据缺一不可：**只断框内的众数**抓不到描边（一圈 1 像素在几百个像素里不占
// 众数），**只断框线那一行**抓不到常驻底色。
func assertFlat(t *testing.T, img *image.RGBA, r mygoui.Rect, what string) {
	t.Helper()
	behind := probeBehind(img, r)
	if d := maxDiff(probeFill(img, r), behind); d > probeTolerance {
		t.Errorf("%s 那一格有常驻底色：框内 %v、框外 %v，差 %d（> %d）",
			what, probeFill(img, r), behind, d, probeTolerance)
	}
	if d := probeEdge(img, r); d > probeTolerance {
		t.Errorf("%s 那一圈还有描边：框线那一行和它外面的差 %d（> %d）",
			what, d, probeTolerance)
	}
}

// flatSideShell 建一个「登录过、停在**不在侧栏菜单里**的一页、指针不在任何按钮上」
// 的整窗。
//
// ⚠️ **两处都得显式写出来**：登录是为了菜单摆满六项（未登录时那三个带 `HideInMenuWhenLoggedOut`
// 的不摆，见 `viewSideMenu`）；`systemIsDark` 是「假装系统是深色」，而设置里那一档
// 默认是「跟随系统」，所以它就是深浅的开关——`newLoggedInShell` 不接这一项，用它
// 会让两个子测试画出同一张图，**深色那一档看着全绿，其实根本没跑过**。
//
// ⚠️ **停在 `/search`**：那一页不在侧栏菜单里，于是**没有任何一行是选中的**——
// 量「平时的那些行」时不能有选中态混进来（它是 `TestTheCurrentRowIsAFaintTintNot
// ASolidBlock` 的事，那一条自己切到 `/local-music`）。
func flatSideShell(t *testing.T, dark bool) (*Shell, *mygoui.Tester) {
	t.Helper()
	b := newTestShell(t, systemIsDark(dark), loggedInWith("7"))
	s, tt := b.s, b.tt
	s.navigate("/search")
	tt.Frame()
	tt.Move(600, 780) // 指针挪到播放条那片空地上，别停在任何一格按钮上
	tt.Frame()
	return s, tt
}

// TestTheIconButtonsAreTransparentAndUnbordered 断**图标键**：顶栏那几枚、播放条
// 那一排、队列抽屉头那两枚，画出来都得和背后那一层是同一个颜色。
//
// ⚠️ **只探图标键是有原因的**：`Tester.Find` 找的是「显示着这个字或者被 `Label`
// 命名的那一个元素」，而纯文字按钮上**没有 `Label`**（`flatbutton.go` 里那条：不许
// 补，补上去画面上会出现两份同样的字）。于是 `Find("导出配置")` 给回来的是**那几个
// 字自己的框**，按它去量框线，量到的是笔画而不是按钮。
//
// 纯文字按钮（侧栏菜单行、设置页那几个小按钮）改用另外两条：侧栏用
// `TestTheSidebarColumnHasOnlyOneColour`，选中态用
// `TestTheCurrentRowIsAFaintTintNotASolidBlock`。三处缺一不可。
//
// ⚠️ **退化回 `ui.Button` 的话这一条立刻红**，而别的全绿：签名一个字没变、事件也
// 照旧触发，只有画面上多了一圈线。
func TestTheIconButtonsAreTransparentAndUnbordered(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			_, tt := flatSideShell(t, dark)
			// ⚠️ **「播放列表」从顶栏搬到了播放条右边那一组**（票 #127，照 Biu
			// 的 `playbar/right/index.tsx:16`），所以下面那一句「哪一格」得跟着
			// 变——写成「顶栏」会让人以为它在顶栏上，而它不在了。
			//
			// ⚠️ **「设置」不在这一列里**（票 #289）：顶栏那一枚拿掉了（侧栏和
			// 头像下拉里都有同一个动作），而**框架那一支后退/前进不是图标键**——
			// 它们是 `ui.Button`，有自己的底色和（原来那圈）描边，不归这一条管。
			for _, what := range []struct{ label, where string }{
				{"打开文件", "顶栏"},
				// 它原来是文字键（「迷你」两个字），票 #134 的第七单元接上了
				// `iconPiP`——现在它也走这一条探针。
				{"迷你", "顶栏"},
				{"播放列表", "播放条右边"},
			} {
				r, ok := tt.Find(what.label)
				if !ok {
					t.Fatalf("%s上找不到 %q，文字有 %q", what.where, what.label, tt.Texts())
				}
				assertFlat(t, tt.Image(), r, what.where+" "+what.label)
			}
		})
	}
}

// sideRun 是侧栏左边那一列上「和侧栏底色不一样」的一段。
type sideRun struct{ y0, y1 int }

// sideScanY 是侧栏那一列从哪儿扫到哪儿。
//
// ⚠️ **上下都留了富余**（品牌那一行到折叠按钮那一行）：扫的范围里除了菜单行，还有
// 收藏夹那一片（两个分组标题 + 那一句空状态），而它们也都在左边 8 像素处起头——范围
// 收窄到菜单那几行，就得靠 `Tester.Find` 去量它们的框，而 `Find` 认不出「侧栏里
// 那个和内容区标题同名的那一个」（见下面那个 ⚠️）。宁可多扫：多出来的那些像素上
// 同样只该有侧栏底色，一并断掉。
const (
	sideScanY0 = 55
	sideScanY1 = 700
)

// sideScanX 是扫侧栏的第几像素。
//
// ⚠️ **4，不是 8**：菜单行的描边在 x=0、按钮通栏，所以 4 那一列既在按钮之内、又能
// 吃到上下那两条横描边。而「Biu」那个品牌字、下面收藏夹那一句空状态都是
// `Padding(…, 8, …)` 从第 8 像素起头的——扫 8 会扫到它们身上去。
const sideScanX = 4

// sideScans 扫侧栏那一列，返回「和底色不一样」的那些段。
//
// 底色取那一列的**众数**（不是最上面那个像素）：侧栏底色占了这一列的绝大部分，
// 拿众数才对；拿「最上面」的话，一行描边飘在上面就会把底色认错。
//
// ⚠️ 这一列上画的每一件事——菜单行的描边、选中那一行的淡底、描边药丸的常驻底色
// ——在画面上**全**是「一段和底色不一样的像素」，所以这一个探针同时断得住这三件。
func sideScans(img *image.RGBA) (bg color.RGBA, runs []sideRun) {
	var col []color.RGBA
	for y := sideScanY0; y <= sideScanY1; y++ {
		col = append(col, img.RGBAAt(clampX(img, sideScanX), clampY(img, y)))
	}
	bg = modeOf(col)
	start := -1
	for i, p := range col {
		if d := maxDiff(p, bg); d > probeTolerance {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			runs = append(runs, sideRun{sideScanY0 + start, sideScanY0 + i - 1})
			start = -1
		}
	}
	if start >= 0 {
		runs = append(runs, sideRun{sideScanY0 + start, sideScanY1})
	}
	return bg, runs
}

// TestTheFlatAreasNeverReachForTheStockButton 钉住「票面点名的五处里不许再出现
// `ui.Button`」。
//
// # 为什么断源码而不是画面
//
// 评审查出来过一次：设置页有**六枚**描边药丸漏了（`settings_tab_config.go` 的
// 导出 / 导入、`settings_tab_menu.go` 的「添加文件夹」和「×」、
// `settings_tab_shortcut.go` 的「恢复默认」），而代码和**全部测试都绿**——
// `ui.Button(c, "导出配置")` 和 `flatButton(c, "导出配置")` 签名一模一样、事件照旧
// 触发，只有画面上多了一圈线。
//
// 而逐处配画面探针做不到：纯文字按钮**没有一个能直接用的探针**（`Tester.Find`
// 给的是那行字的框，见 `TestTheIconButtonsAreTransparentAndUnbordered` 顶上那一段），
// 这也是当初那六枚漏掉还没人发现的同一个原因。
//
// 覆盖面用**文件名前缀**而不是逐个列文件：以后新加一个 `sidebar_xxx.go` 自动在里
// 头，不用回来改这张单子。
func TestTheFlatAreasNeverReachForTheStockButton(t *testing.T) {
	// 票面点名的五处 + 它们的附属文件。
	prefixes := []string{"sidebar", "navbar", "playbar", "settings", "queue_drawer", "partdrawer"}
	// 这几个是**故意**留着 `ui.Button` 的，每一条都有理由；新加例外必须先在这里
	// 写一行，评审才看得见。
	allowed := map[string]string{
		"ui.Toolbar": "框架自己的横向工具栏，它本来就是无描边那一支" +
			"（`styleButton` 的 toolbarButtons），不是描边药丸",
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var checked int
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if !slices.ContainsFunc(prefixes, func(p string) bool {
			return name == p+".go" || strings.HasPrefix(name, p+"_")
		}) {
			continue
		}
		checked++
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if !strings.Contains(code, "ui.Button(") {
				continue
			}
			why, ok := allowed[code]
			if !ok {
				why = "这一处不在例外名单里——要么换成 flatButton，要么在 allowed 里写一行理由"
			}
			t.Errorf("%s:%d 还在用 ui.Button：%s\n	%s", name, i+1,
				strings.TrimSpace(code), why)
		}
	}
	if checked == 0 {
		t.Fatal("一个文件都没查到——前缀写错了，这条测试会一直绿着")
	}
}

// TestTheSidebarRowsHaveNoBorderAndNoFill 断**侧栏那一列纯文字按钮**。
//
// 换了个探针：`Tester.Find` 在纯文字按钮上给的是**那行字的框**（见上面那一条），
// 而按钮是通栏的、字是居中的，所以从「字的框」往四周探，探到的是笔画。
//
// 而且它还认错人：「本地音乐」在侧栏里有一行、在内容区还有一个页标题，而 `Find`
// 给的是**先画的那个**——实测给回来的是内容区那个 (92, 7)，按它去量侧栏，量的
// 是标题区的白底。第一版这条就是这么写的：它一直绿，而把 `flatButton` 退回
// `ui.Button` 之后它**照样绿**（扫的范围因为上面那个认错人反而是空的）。
//
// 判据：停在**不在侧栏菜单里**的一页（`/search`），于是没有任何一行是选中的，
// 整条侧栏那一列上**只该有侧栏底色一种颜色**。出现第二段就是有一圈描边
// （浅色 `#dbdbdb` / 深色 15% 白叠出来的 `#3b3b3d`，票 #172 之后）或者一块
// 常驻底色。
func TestTheSidebarRowsHaveNoBorderAndNoFill(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			_, tt := flatSideShell(t, dark)
			bg, runs := sideScans(tt.Image())
			if len(runs) != 0 {
				t.Errorf("侧栏左边第 %d 像素那一列上有 %d 段和底色 %v 不同的像素（%v）——"+
					"菜单行还有一圈描边或者一块常驻底色", sideScanX, len(runs), bg, runs)
			}
		})
	}
}

// TestThePlayBarKeysAreFlatAndKeepTheirHover 是播放条那一半：换上去的同时，
// **悬停反馈还在**。
//
// 四条判据互相兜着：
//
//  1. 平时和框外同一块颜色（没有描边、没有常驻底色）。
//  2. **能按的**键指针落上去颜色要变——扁平不等于没反馈，而「没反馈」是不报错的坏。
//  3. 变的那点**要淡**：通道差落在 6~64 之间。下限是「变了」，上限是「变的是一层
//     很薄的底，而不是换成了另一块实色」。
//  4. 指针挪走要真的撤回去。
//
// ⚠️ **「播放 / 暂停」也不在**：那一枚现在**也是扁平的**（票 #217 把它改成上游
// 那一枚：48×48 的盒子、底下什么都没有、图形是前景色），但它**量不了这一族的
// 判据**——`probeFill` 取的是框内像素的**众数**，而那一枚的图形（直径 40 的圆）
// 占了框的一大半，于是众数是**图形本身**而不是底色。它自己的那几条在
// `playbar_layout_test.go`（`TestThePlayKeyHasNoSolidBackground` /
// `TestThePlayKeyTurnsAccentOnHover`）。
// **「上一首 / 下一首」同样不在**：它们此刻是**禁用的**
// （`CanPrev` 要 `player != nil`，无头外壳里没有播放器，`app/queue.go:1210`），
// 而禁用键本来就不该有悬停反馈——那是 `TestADisabledFlatButtonDoesNotLightUpOnHover`
// 的事。拿它们去断「悬停要亮」只会逼着代码给禁用的键也铺一层底，
// **等于告诉用户这里点得动**（这正是评审查出来的那一条）。
//
// ⚠️ 只画播放条（`playBarTester`）而不是整窗：整窗里这一格背后是**内容区**的底色
// （票 #127 之后播放条和内容区同源，都是 `t.Background`），而外侧那一圈要采到
// **播放条之内**才算是同一个参照。整窗的写法会在播放条的最外沿（加歌那一枚）
// 采到窗外，报出来的是「框外是窗口底色」——离真因隔了两层。
func TestThePlayBarKeysAreFlatAndKeepTheirHover(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt := playBarTester(t, fullBarState(), 1200, dark)
			tt.Move(600, 20) // 播放条之外，窗口顶上那条空地
			tt.Frame()

			for _, what := range []string{"播放模式", "播放速率", "加歌"} {
				r, ok := tt.Find(what)
				if !ok {
					t.Fatalf("播放条上找不到 %q，文字有 %q", what, tt.Texts())
				}
				img := tt.Image()
				assertFlat(t, img, r, "播放条 "+what)

				// 悬停：指针落到这一格的中心。
				tt.Move(r.X+r.W/2, r.Y+r.H/2)
				tt.Frame()
				idle := probeFill(img, r)
				lit := probeFill(tt.Image(), r)
				if d := maxDiff(lit, idle); d < 6 {
					t.Errorf("%s 悬停之后一点没变（%v）——扁平不是没有反馈，"+
						"用户就不知道哪里点得动", what, idle)
				} else if d > 64 {
					t.Errorf("%s 悬停之后换了 %d 个色阶（%v → %v）——换太狠了，"+
						"那还是一格药丸，只是不描边了", what, d, idle, lit)
				}

				// 挪走要真的挪回去：留下的一层底比没有更坏。
				tt.Move(600, 20)
				tt.Frame()
				if back := probeFill(tt.Image(), r); maxDiff(back, idle) > probeTolerance {
					t.Errorf("%s 指针挪走之后还留着 %v（原来 %v）——那一层底没撤",
						what, back, idle)
				}
			}
		})
	}
}

// TestADisabledFlatButtonDoesNotLightUpOnHover 钉住「禁用键不许亮」。
//
// 这是评审查出来的第二条：`flatFeel` 原先在**建元素时**问 `IsDisabled()`，而调用方
// 是在 `flatButton` 返回**之后**才 `.Disabled(...)` 的——那一刻问出来恒是 false，
// 于是只读设置里的禁用按钮，指针落上去照样铺一层悬停底（实测那层底是正常键的一半
// 深，通道差 9，正常键 18）。**等于告诉用户这里点得动。**
//
// 两条判据缺一不可：
//
//  1. **同一屏上能写的那一档，指针落上去要亮**——不然这一条会因为「根本没反馈」
//     而白白地绿。
//  2. **只读设置里的每一档，指针落上去一个像素都不能变**。
//
// ⚠️ 判据用**同一格**的「指针在外 / 指针在内」两个采样点，不拿别的控件当参照：
// 别的控件换了主题、换了位置，差值就不是同一个数了。
func TestADisabledFlatButtonDoesNotLightUpOnHover(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// ① 能写的那一版：同样的按钮，指针落上去要亮。
			tt := openSettingsFor(t, dark, false)
			if d := hoverDelta(t, tt, "浅色"); d < 6 {
				t.Fatalf("能写的那一档指针落上去只变了 %d（< 6）——"+
					"下面「禁用键不亮」那条就没有对照，会白白地绿", d)
			}

			// ② 只读的那一版（设置来自更新的版本）：每一档都得一点不变。
			ro := openSettingsFor(t, dark, true)
			if !ro.HasText("设置只读") {
				t.Fatalf("没有进只读那一版，文字有 %q", ro.Texts())
			}
			for _, label := range []string{"浅色", "直接退出"} {
				if d := hoverDelta(t, ro, label); d > probeTolerance {
					t.Errorf("只读设置里的「%s」指针落上去变了 %d（> %d）——"+
						"禁用键亮了，等于告诉用户这里点得动", label, d, probeTolerance)
				}
			}
		})
	}
}

// openSettingsFor 开设置页；readOnly 为真时挂一份来自更新版本的设置（只读）。
//
// 只读那一档靠的是「文件里的 schemaVersion 比我们高」
// （`TestSettingsPageRefusesToWriteAFileFromANewerVersion` 用的同一条路）。
func openSettingsFor(t *testing.T, dark, readOnly bool) *mygoui.Tester {
	t.Helper()
	if !readOnly {
		b := newTestShell(t, systemIsDark(dark))
		b.s.navigate(settingsPath)
		b.tt.Frame()
		return b.tt
	}
	path := filepath.Join(writableTempDir(t, "settings"), app.SettingsFileName)
	// themeMode 要显式给：空字符串不是三档里的任何一个，三个主题按钮就都
	// 不选中——而「选中那一档 DOM 色是主色」正是禁用态那条测试要量的东西。
	if err := os.WriteFile(path, []byte(`{"schemaVersion": 99, "appSettings": {"themeMode": "system"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a := app.New()
	if err := a.MountSettings(path); err == nil {
		t.Fatal("来自更新版本的设置文件没有报错")
	}
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})
	// ⚠️ **`systemIsDark(dark)` 不能漏**：漏了的话这个分支永远画浅色，而
	// 「禁用态在深色下读不读得出来」恰恰是这一处最坏的那一档（绿字乘完 50%
	// 只剩 2.6:1）。数据那一半不改：只改 Tester 报给框架的系统外观。
	b := newTestShell(t, onApp(a), systemIsDark(dark))
	b.s.navigate(settingsPath)
	b.tt.Frame()
	return b.tt
}

// hoverDelta 是「指针落在这一格上」减「指针在别处」时，这一格的底色变了多少。
//
// ⚠️ 指针先挪到**同一格**的外面再取第一个采样点：同一个按钮、同一个背景、同一帧
// 的主题，三个条件只差「指针在不在框里」。
func hoverDelta(t *testing.T, tt *mygoui.Tester, label string) int {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("画面上找不到 %q，文字有 %q", label, tt.Texts())
	}
	away := [2]float32{600, 20}
	tt.Move(away[0], away[1])
	tt.Frame()
	before := tt.Image()
	idle := probeFill(before, r)

	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	return maxDiff(probeFill(tt.Image(), r), idle)
}

// TestTheSelectedRowKeepsItsContrast 钉住「选中那一行的字读得出来」。
//
// 这是评审查出来的第一条：选中态把底从实心主色换成淡底的同时**把字也染成主色**，
// 浅色默认主题下那一行从 **9.2:1 掉到 1.55:1**——低于 WCAG AA 的 4.5:1。
// 绿字压绿底是同一支颜色压自己。
//
// 判据是**画面上那一格的两个色**：底取众数、字取离底最远的那一个（落在实心笔画上），
// 再算 WCAG 2.x 的对比度。深浅两套主题都跑——深色那一档本来就过，而浅色那一档
// 曾经不是。
//
// 4.5 是 WCAG AA 对**普通正文**的门槛（侧栏菜单项是 13px 的正文，不是大字）。
func TestTheSelectedRowKeepsItsContrast(t *testing.T) {
	const wantAA = 4.5
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, systemIsDark(dark), loggedInWith("7"))
			s, tt := b.s, b.tt
			s.navigate("/local-music")
			tt.Frame()
			tt.Move(600, 780)
			tt.Frame()

			img := tt.Image()
			_, runs := sideScans(img)
			if len(runs) != 1 {
				t.Fatalf("侧栏那一列上有 %d 段和底色不同的像素（%v）——"+
					"选出当前那一行要靠「只有一段」", len(runs), runs)
			}
			bg, ink := rowInk(img, runs[0])
			got := wcagContrast(bg, ink)
			t.Logf("%s：选中那一行 底 %v / 字 %v = %.2f:1", name, bg, ink, got)
			if got < wantAA {
				t.Errorf("选中的那一行对比度只有 %.2f:1（< %.1f:1）——底 %v、字 %v。",
					got, wantAA, bg, ink)
			}
		})
	}
}

// rowInk 是一行里「底色」和「字色」：底取那一段的众数，字取离底最远的那一个像素。
//
// ⚠️ **字取的是最远的那一个、不是平均**：字是抗锯齿画的，一行里大部分是过渡色，
// 平均出来是一个两边都不是的中间值，拿它算对比度既不像底也不像字。落在实心笔画
// 上的那些像素才是字本身的颜色。
func rowInk(img *image.RGBA, r sideRun) (bg, ink color.RGBA) {
	var px []color.RGBA
	for y := r.y0; y <= r.y1; y++ {
		for x := sideScanX; x < sideScanX+probeFillW; x++ {
			px = append(px, img.RGBAAt(clampX(img, x), clampY(img, y)))
		}
	}
	bg = modeOf(px)
	worst := probeTolerance
	for _, p := range px {
		if d := maxDiff(p, bg); d > worst {
			worst, ink = d, p
		}
	}
	return bg, ink
}

// probeFillW 是 rowInk 横向取多宽：从侧栏左缘那一列往右，够盖住那几个字就行。
const probeFillW = 120

// wcagContrast 是 WCAG 2.x 的对比度：(L1+0.05)/(L2+0.05)，L1 取亮的那一份。
//
// 相对亮度按 sRGB 官方那一段：先把每个通道线性化（c≤0.04045 就直接除 12.92，
// 否则 ((c+0.055)/1.055)^2.4），再按 0.2126 / 0.7152 / 0.0722 加权。
func wcagContrast(a, b color.RGBA) float64 {
	la, lb := wcagLum(a), wcagLum(b)
	if lb > la {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func wcagLum(c color.RGBA) float64 {
	lin := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

// TestTheCurrentRowIsASolidBlock 钉住选中态。
//
// 上一条断的是「平时没有底」，这一条断的是「选中那一格有底」。
//
// 🔴 **这一档从「淡底」改成「实心主色底」**（票：用户要「深底色 + 白字」）：
// 淡底（`accentSoft`）和侧栏底色的对比度只有 **1.09:1 ~ 1.29:1**（实测）——
// **几乎分不出来**，而「当前在哪」是侧栏里唯一的方向标。
//
// 判据是**和侧栏底色的通道差**：
//
//   - 0        → 选中态根本没画，用户看不出自己在哪一页
//   - < 100    → 还是太淡，看不出当前在哪一页
//   - ≥ 100    → 一块实心主色底（对的形状）
//
// ⚠️ **探针还是侧栏那一列**（和上一条同一个），不是 `Tester.Find`：「本地音乐」在
// 内容区还有一个同名页标题，`Find` 给回来的是那个 (92, 7)——第一版就栽在这儿，
// 它对着内容区的白底去量侧栏，怎么量都不会红。侧栏那一列只认「和侧栏底色不同的
// 那一段」，而这一页里这样的段**只有一段**：选中的那一行。
func TestTheCurrentRowIsASolidBlock(t *testing.T) {
	// 🔴 **下限从 64 提到 100**：淡底那一档和侧栏底色只差 30~39 个通道，而实心
	// 主色底差着 100+（实测暖纸浅色 4.34:1、深色 6.38:1）。100 是把两档分开的
	// 那条线——它拦得住「改回淡底」，也拦得住「什么底都没画」。
	const wantMin = 100
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, systemIsDark(dark), loggedInWith("7"))
			s, tt := b.s, b.tt
			s.navigate("/local-music")
			tt.Frame()
			tt.Move(600, 780)
			tt.Frame()

			img := tt.Image()
			bg, runs := sideScans(img)
			if len(runs) != 1 {
				t.Fatalf("侧栏那一列上有 %d 段和底色不同的像素（%v）——应该只有选中的"+
					"那一行这一段。多出来的段是描边，常驻底色也会多出好几段",
					len(runs), runs)
			}
			// 取这一段**中间**那一块：两头是圆角和抗锯齿，颜色偏淡，取到它们就
			// 把差距量小了。
			r := runs[0]
			y0 := r.y0 + (r.y1-r.y0)/4 + 1
			y1 := r.y1 - (r.y1-r.y0)/4 - 1
			lit := probeBand(img, sideScanX, y0, sideScanX, y1)
			d := maxDiff(lit, bg)
			if d < wantMin {
				t.Errorf("选中的那一行和侧栏底色只差 %d（< %d，%v 对 %v）——"+
					"看不出当前在哪一页（这一档是实心主色底）", d, wantMin, lit, bg)
			}
		})
	}
}

// TestTheQueueHeaderKeysAreIconButtonsWithNames 钉住队列抽屉头那两枚。
//
// 三条：画着图标（不是两个字）、**无障碍名字一个字没少**、点了还是原来那个反应。
// 前两条是这一票换控件时最容易丢的（换成图标之后画面上再没有字，名字就只挂在
// `Label` 上，而 `Label` 少一个的话读屏念不出来、`Tester.Find` 也认不到）。
func TestTheQueueHeaderKeysAreIconButtonsWithNames(t *testing.T) {
	s, tt := newShell(t)
	s.app.Queue().Reset(app.Item{Source: "local", Type: "audio", Title: "第一首"})
	s.drawerOpen = true
	// ⚠️ **要让抽屉滑到位**：Tester 的钟站着不动，不推它的话面板还在窗口外面，
	// 按名字找「清空」会找不到（它在窗口外）。
	settleDrawer(t, s, tt)
	for _, name := range []string{"定位当前播放", "清空"} {
		r, ok := tt.Find(name)
		if !ok {
			t.Fatalf("抽屉头上找不到 %q，文字有 %q", name, tt.Texts())
		}
		if r.W > iconHit {
			t.Errorf("%q 那一格是 %.0f 宽——图标键的点击区是 %d，说明画的还是两个汉字",
				name, r.W, iconHit)
		}
	}
	// 还能点：换控件最容易把事件弄丢，而那也不报错。
	//
	// ⚠️ **「清空」要先问一次**（票 #454）：点它弹的是那道确认框，队列要等
	// 「确认清空」按下去才真的清空——所以这一条要连着点两步（那一道确认框长什么样，
	// 见 `ticket454_test.go` 的 `TestClearingTheQueueAsksFirst`）。
	if err := tt.Click("清空"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("确认清空"); err != nil {
		t.Fatalf("点不了确认键：%v（文字：%q）", err, tt.Texts())
	}
	if n := len(s.app.Queue().Items()); n != 0 {
		t.Errorf("按了「清空」队列里还剩 %d 首", n)
	}
}

// TestRenderFlatShots 出这一票的**对照图**：改前改后各跑一遍，同一份代码、同一批
// 内存夹具，只差「按钮换没换成 flatButton」。
//
// ⚠️ **为什么不并进 `TestRenderShots`**：那一族（`router_test.go:435`）的首页是
// **空的**——它没挂数据源，于是首页那张图上只有一片空状态，看不出按钮长什么样。
// 而这一票改的每一处都在**有内容**的时候才看得出（侧栏选中的那一行、播放条上那一
// 排图标键、抽屉头那两枚），所以另起一族，只用仓库现成的夹具：
// `homeSongs`（`homepage_test.go`）、`newTestShell`（`shelltest_test.go`）。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染，钥匙串里那份真登录
// 态一次都没被读（`loggedInWith` 挂的是 `app.NewMemoryStore`）。
func TestRenderFlatShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	// 指针停在窗口右下那片空地上：悬停那一层会跟着指针走，而存图要的是「平时」。
	away := [2]float32{1180, 770}

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// 首页：网格档、有内容。网格那一档才是上游 `home.png` 能对上的那一档。
			//
			// ⚠️ **不直接用 `newHome`**：它走的是 `newShell(t)`，不接
			// `systemIsDark`，于是「深色」那一档存出来的和「浅色」一模一样
			// （第一版就这样，而调研说的恰恰是**深色下差距更明显**）。
			// 这一处照 `newHome` 的三行写一遍，只是把「壳从哪儿来」交出来。
			b := newTestShell(t, systemIsDark(dark))
			s, tt := b.s, b.tt
			s.home.SetSongs(fullSongs())
			homeFrame(tt)
			mountDisplay(t, s.app)
			setDisplay(t, s.app, tt, app.DisplayModeCard)
			tt.Move(away[0], away[1])
			tt.Frame()
			save(t, dir, "20-"+name+"-首页-网格.png", tt)

			// 队列抽屉 + 有歌的播放条：抽屉头那两枚（图标化之后）和播放条那一排。
			// 队列里得有东西——那两个键「只在队列非空时才摆」
			//（`queue_drawer.go` 的 viewQueueHeader）。
			s.app.Queue().Reset(
				app.Item{Source: "local", Type: "audio", Title: "夜空中最亮的星", Artist: "逃跑计划"},
				app.Item{Source: "local", Type: "audio", Title: "起风了", Artist: "买辣椒也用券"},
				app.Item{Source: "local", Type: "audio", Title: "晴天", Artist: "周杰伦"},
			)
			s.drawerOpen = true
			// ⚠️ **要让抽屉滑到位**：Tester 的钟站着不动，不推它的话面板还在
			// 窗口外面（它从右边滑进来），存下来的图是空的右半边。
			settleDrawer(t, s, tt)
			tt.Move(away[0], away[1])
			tt.Frame()
			save(t, dir, "21-"+name+"-队列抽屉.png", tt)
		})
	}
}
