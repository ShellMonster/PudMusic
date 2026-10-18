package ui

import (
	"image"
	"image/color"
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
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
// 基线 D）——深色下那圈 `#3f3f46` 压在 `#18181b` 上是一道亮线，浅色下它几乎看不出
// 来。只跑浅色的话，这一票最该修的那一面根本没被验过。

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
// 描边那处更大（#d9d9de 对 #f4f4f5，通道差 27）。所以 3 抓得住这两处，而不会把
// 「差两三个色阶的那点抗锯齿」当成差距。
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
// ⚠️ **两处都得显式写出来**：登录是为了菜单摆满六项（未登录时那三个带 `NeedsLogin`
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
// 补，补上去画面上会出现两份同样的字）。于是 `Find("迷你")` 给回来的是**那两个字
// 自己的框**，按它去量框线，量到的是笔画而不是按钮。
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
			for _, what := range []string{"打开文件", "播放列表", "设置"} {
				r, ok := tt.Find(what)
				if !ok {
					t.Fatalf("顶栏上找不到 %q，文字有 %q", what, tt.Texts())
				}
				assertFlat(t, tt.Image(), r, "顶栏 "+what)
			}
		})
	}
}

// sideRun 是侧栏左边那一列上「和侧栏底色不一样」的一段。
type sideRun struct{ y0, y1 int }

// sideScanY 是侧栏那一列从哪儿扫到哪儿。
//
// ⚠️ **上下都留了富余**（品牌那一行到折叠按钮那一行）：扫的范围里除了菜单行，还有
// 「还没有收藏夹」那一句提示和两个分组标题，而它们也都在左边 8 像素处起头——范围
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
// 吃到上下那两条横描边。而「Biu」那个品牌字、下面「还没有收藏夹」那一句提示都是
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
// （`#d9d9de` / `#3f3f46`）或者一块常驻底色。
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

// TestThePlayBarAndRateKeysAreFlatAndNotDead 是播放条那一半：换上去的同时，
// **悬停反馈还在**。
//
// 三条判据互相兜着：
//
//  1. 平时和框外同一块颜色（没有描边、没有常驻底色）。
//  2. 指针落上去**颜色要变**——扁平不等于没反馈，而「没反馈」是不报错的坏。
//  3. 变的那点**要淡**：通道差落在 6~64 之间。下限是「变了」，上限是「变的是一层
//     很薄的底，而不是换成了另一块实色」。
//
// ⚠️ 只画播放条（`playBarTester`）而不是整窗：整窗里这一格背后是播放条自己的
// `t.Surface`，而外侧那一圈要采到**播放条之内**才算是同一个参照。整窗的写法会在
// 播放条的最外沿（加歌那一枚）采到窗外，报出来的是「框外是窗口底色」——离真因
// 隔了两层。
func TestThePlayBarAndRateKeysAreFlatAndNotDead(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt := playBarTester(t, fullBarState(), 1200, dark)
			tt.Move(600, 20) // 播放条之外，窗口顶上那条空地
			tt.Frame()

			for _, what := range []string{"上一首", "下一首", "播放模式", "播放速率", "加歌"} {
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

// TestTheCurrentRowIsAFaintTintNotASolidBlock 钉住选中态。
//
// 上一条断的是「平时没有底」，这一条断的是「选中那一格有底，但不是实心主色」。
//
// 判据是**和侧栏底色的通道差**：
//
//   - 0        → 选中态根本没画，用户看不出自己在哪一页
//   - 6~64     → 一层很淡的底（对的形状）
//   - ~214     → 实心主色（Biu `#1ed760` 压在 `#f4f4f5` 上最大的那一道），
//     这就是原来那一版 `Background(t.Accent)`，也是调研点名的那一处
//
// 64 是从「看得出在哪一页」倒推的绝对数，**不是照抄任何一处实现里的数**：
// 浅色那一档淡底是 30、深色那一档是 39，而实心那一版最小也有 149。两边都过，两边
// 都拦得住。
//
// ⚠️ **探针还是侧栏那一列**（和上一条同一个），不是 `Tester.Find`：「本地音乐」在
// 内容区还有一个同名页标题，`Find` 给回来的是那个 (92, 7)——第一版就栽在这儿，
// 它对着内容区的白底去量侧栏，怎么量都不会红。侧栏那一列只认「和侧栏底色不同的
// 那一段」，而这一页里这样的段**只有一段**：选中的那一行。
func TestTheCurrentRowIsAFaintTintNotASolidBlock(t *testing.T) {
	const (
		wantMin = 6
		wantMax = 64
	)
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
			switch {
			case d < wantMin:
				t.Errorf("选中的那一行和侧栏底色只差 %d（< %d，%v 对 %v）——"+
					"看不出当前在哪一页", d, wantMin, lit, bg)
			case d > wantMax:
				t.Errorf("选中的那一行和侧栏底色差 %d（> %d，%v 对 %v）——那是实心主色，"+
					"Biu 那边是主色很淡的一层（上游 HeroUI 的 variant=\"flat\"）",
					d, wantMax, lit, bg)
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
	for range 3 {
		tt.Frame()
	}
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
	if err := tt.Click("清空"); err != nil {
		t.Fatal(err)
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
			for range 3 {
				tt.Frame()
			}
			tt.Move(away[0], away[1])
			tt.Frame()
			save(t, dir, "21-"+name+"-队列抽屉.png", tt)
		})
	}
}
