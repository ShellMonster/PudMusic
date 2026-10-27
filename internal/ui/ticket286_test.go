package ui

// 票 #286（板子任务 BMG-165）的画面验收：**外壳布局——侧栏从窗口顶端贯到底，顶栏只盖内容区**。
//
// ⚠️ **文件名按 GitHub issue 号走**（这一族都是 `ticket<N>_test.go`，N = issue 号）：
// 这一票是 **#286**。「165」在这个包里指的是**另一张票**——GitHub #165（顶栏搜索：
// 输入区形态与历史 Chip，`navbar_search_form_test.go` / `tooltip_delay_test.go`
// 那两处说的就是它），而 BMG-165 是**板子任务号**，不是 issue 号。
//
// 上游是 `biu/src/layout/index.tsx:36-47` 那个形状：侧栏（`SideNav`）和
// 「顶栏 + 内容区」那一列是**兄弟**，顶栏在右边那一列里——于是侧栏从窗口最上头
// 一直贯到底（红黄绿浮在它左上角，logo 在它们下面），顶栏只画在内容列上。
//
// 我们原来是「顶栏盖满整窗 / 侧栏从顶栏下面才开始」那三层（`underlay.go` 的
// `Column{顶栏 / Row{侧栏, 路由页} / 播放条}`）：左上角那一块（上游放 Logo 的
// 地方）放的是后退 / 前进 / 搜索框，而侧栏从 y=64 才开始。报告 C-01 量过的差：
// 上游侧栏右沿那条 1px 分隔线从 y=0 一直到 y=712，我们从 y=64 才开始；
// 上游的后退键在 x=216，我们贴在窗口左边 x=8。
//
// 这一族断三件事，每一件都是**逐像素**量的（读源码量不出「顶栏有没有盖到
// 侧栏上」）：

import (
	"image"
	"image/color"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// shellShotShell 造一个停在家页的外壳，**深浅两套都要跑**。
//
// ⚠️ **指针要挪开**：停在任何一格按钮上会叠一层悬停底，而这一族量的是
// 「底色对不对」「线在不在」——悬停底会把它读成别的东西。
func shellShotShell(t *testing.T, dark bool) (*Shell, *mygoui.Tester, *image.RGBA) {
	t.Helper()
	b := newTestShell(t, systemIsDark(dark), withSettings(app.DefaultSettings("")))
	s, tt := b.s, b.tt
	s.navigate("/")
	tt.Frame()
	tt.Move(600, 400)
	tt.Frame()
	return s, tt, tt.Image()
}

// sideDividerColumn 找出侧栏右缘那条 1px 分隔线在第几列。
//
// 做法：在**侧栏里一片空处**的那一行上，从左边扫过去，找第一个「和侧栏底色
// 不一样」的像素——那就是那条线。
//
// ⚠️ **挑的行要空**：菜单项、选中那一行的淡底都会挡在前面。y=300 那一行在
// 侧栏上是空的（菜单只到 y=250 左右，下面是收藏夹那一片，都在
// x=8..170，不在 x=190 那一列上——但扫的是整行，所以挑的是**侧栏那一列**）。
func sideDividerColumn(t *testing.T, img *image.RGBA, y int) int {
	t.Helper()
	// 这一行左侧的众数 = 侧栏的底色。
	var left []color.RGBA
	for x := 0; x < 150; x++ {
		left = append(left, img.RGBAAt(x, y))
	}
	bg := modeOf(left)
	for x := 150; x < 400; x++ {
		if maxDiff(img.RGBAAt(x, y), bg) > 8 {
			return x
		}
	}
	t.Fatalf("侧栏右缘那条线在 y=%d 那一行上找不到了", y)
	return -1
}

// TestTheSidebarRunsFromTheTopOfTheWindow 断**侧栏从窗口顶端贯到底**。
//
// 判据是那条 1px 分隔线：它在**第一行**（y=0）就在，说明侧栏画到了窗口最
// 上头；改之前它在 y=64 才开始（那一段是顶栏）。
//
// ⚠️ **量线而不是量底色**：侧栏和内容区的底色**是同一个**（`t.Background`，
// 票 #127 统一过的），所以「侧栏那一块是什么颜色」分不出两件事；那条分隔线
// 才分得出——它在侧栏的右缘，从侧栏的上沿一直到下沿。
func TestTheSidebarRunsFromTheTopOfTheWindow(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			_, _, img := shellShotShell(t, dark)

			divider := sideDividerColumn(t, img, 300)

			// ① 线的上沿：y=0 那一行上，它就在。
			//
			// ⚠️ **拿 y=300 那一行的颜色当参照**：同一个像素在两个地方应该是
			// 同一个颜色（分隔线是纯色，不随高度变）。
			want := img.RGBAAt(divider, 300)
			if got := img.RGBAAt(divider, 0); maxDiff(got, want) > 8 {
				t.Errorf("侧栏右缘那条线在 y=0 是 %v，y=300 是 %v——"+
					"侧栏没有画到窗口最上头（顶栏还盖在它上面）", got, want)
			}
			// ② 顶栏那一段（y=0..63）里，它**一路都在**：中间任何一行断了，
			// 就是有一块不是侧栏。
			for y := 0; y < navMinHeight; y++ {
				if got := img.RGBAAt(divider, y); maxDiff(got, want) > 8 {
					t.Errorf("侧栏右缘那条线在 y=%d 断了（%v，want %v）", y, got, want)
					break
				}
			}
			// ③ 它在侧栏的**右缘**，不在别处：侧栏宽（`DefaultSideMenuWidth`）的
			// 最后一列（线本身），或者侧栏底色和内容区底色不同时的下一列。
			if want := int(app.DefaultSideMenuWidth); divider != want-1 && divider != want {
				t.Errorf("侧栏右缘那条线在 x=%d，want %d（侧栏宽 %v）",
					divider, want, app.DefaultSideMenuWidth)
			}
		})
	}
}

// TestTheLogoSitsWhereUpstreamPutsIt 断**logo 在上游那一带**。
//
// 上游的 logo 盒在 y=32..72（侧栏的上沿 + `pt-8` 的 32 + 40 高的盒子），实测
// 那一块墨在 **y=36..66**（报告 C-01 量的是「logo 的绿在 CSS y=40..60」——
// 那是纯绿的那几行，抗锯齿的边还往外一点）。
//
// 我们改完之后也该在这个位置：改之前 logo 盒在 y=96..136（顶栏 64 + 32）。
//
// ⚠️ **量的是墨迹的包围盒，不是元素盒子**：`Tester` 量不到元素盒子（`Find`
// 只给有名字/有字的那些），而墨迹的包围盒是「40×40 的图标减去图标自己的
// 留白」——两边同一张图标的同一批像素，位置一样就说明摆对了。
func TestTheLogoSitsWhereUpstreamPutsIt(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			_, _, img := shellShotShell(t, dark)

			// 侧栏的底色：侧栏右缘左边那一列（x=190）的众数。
			var col []color.RGBA
			for y := 0; y < 700; y++ {
				col = append(col, img.RGBAAt(190, y))
			}
			bg := modeOf(col)

			// 墨迹的包围盒：logo 那一块（x < 62，y < 80）。
			minX, minY, maxX, maxY := 9999, 9999, -1, -1
			for y := 0; y < 80; y++ {
				for x := 0; x < 62; x++ {
					if maxDiff(img.RGBAAt(x, y), bg) <= 12 {
						continue
					}
					minX, minY = min(minX, x), min(minY, y)
					maxX, maxY = max(maxX, x), max(maxY, y)
				}
			}
			if maxX < 0 {
				t.Fatalf("左上角那块没有图")
			}
			// 上游那一块墨在 y=36..66、x=19..52。留 2 像素的容差（抗锯齿那一圈
			// 和图标的留白在不同平台上差一两个像素）。
			if minY < 34 || maxY > 68 {
				t.Errorf("logo 那一块墨在 y=%d..%d，want ≈ 36..66"+
					"（上游 logo 盒 y=32..72）", minY, maxY)
			}
			if minX < 17 || maxX > 54 {
				t.Errorf("logo 那一块墨在 x=%d..%d，want ≈ 19..52", minX, maxX)
			}
		})
	}
}

// TestThePlayBarStillSpansTheWholeWindow 断**播放条仍然横贯整窗**。
//
// ⚠️ **这一条是防「顺手把播放条塞进内容区」的**：外壳改成「侧栏 | 内容区」
// 两列之后，播放条要是跟着内容区走，它就从 x=216 开始了——而上游的播放条是
// 横贯整窗的（`biu/src/layout/index.tsx`：它在最底下那一层，不在内容列里），
// 三枚键也才落在**窗口**的竖直中轴上（`playbar.go` 的 1:3:1 三列）。
//
// 判据用的是播放条上那个歌名的位置：它在播放条的**左段**，所以它的 x 一定
// 小于侧栏的右沿。
func TestThePlayBarStillSpansTheWholeWindow(t *testing.T) {
	s, tt, _ := shellShotShell(t, false)
	openATrack(t, s)
	tt.Frame()

	title, ok := tt.Find("stereo16")
	if !ok {
		t.Fatalf("播放条上没有那一首歌，文字有 %q", tt.Texts())
	}
	if title.X >= app.DefaultSideMenuWidth {
		t.Errorf("播放条上那一首歌在 x=%v——播放条没有横贯整窗（它跟着内容区走了）", title.X)
	}
}

// TestTheTopBarKeepsItsButtonsInsideTheWindow 断**顶栏那一排在最窄的那一档
// 也不越出窗口**。
//
// 窗口 900（`minWindowW`，最小）+ 侧栏 480（`app.MaxSideMenuWidth`，最大）
// → 内容区只剩 420，而顶栏上那一排（两个箭头 + 搜索框 280 + 右边五枚键）要
// 560 多。**搜索框先让位**（`navbar_search.go` 那一处的 `MinWidth(0)`），右边
// 那几枚键留在窗口里。
//
// ⚠️ **不让的后果画面上看不出来**：搜索框写死了 280，而 flex 的默认最小宽度
// 就是那个写死的宽——它一步都不肯让，于是右边那几枚键被推到 x=904 之后
// （窗口只有 900 宽），**整个在窗口外**：用户点不到它们，而截图上只是「右边
// 空了一块」。
func TestTheTopBarKeepsItsButtonsInsideTheWindow(t *testing.T) {
	st := app.DefaultSettings("")
	st.SideMenuWidth = 480
	b := newTestShell(t, withSettings(st), at(minWindowW, minWindowH),
		withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}))
	tt := b.tt
	tt.Frame()

	for _, what := range []string{"打开文件", "迷你", "动态", avatarLabel} {
		r, ok := tt.Find(what)
		if !ok {
			t.Fatalf("顶栏上找不到 %q，文字有 %q", what, tt.Texts())
		}
		if r.W <= 0 {
			t.Errorf("顶栏上的 %q 宽 %.1f——它整个被挤没了", what, r.W)
			continue
		}
		if r.X+r.W > float32(minWindowW) {
			t.Errorf("顶栏上的 %q 右缘是 %.1f，越出窗口了（want ≤ %d）",
				what, r.X+r.W, minWindowW)
		}
	}
}
