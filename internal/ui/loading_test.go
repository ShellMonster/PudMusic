package ui

import (
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// 这一票（#178）把加载态从「一行 15px 的淡字」换成「一枚居中的转圈」，而这一族
// 的错法全是**静默的**：画面上少了一枚转圈、或者它落在别的地方，**没有一样会
// 报错**——界面只是「不太对」。所以判据全是**画面上的东西**：转圈在不在、多大、
// 落在哪、以及按钮在忙的时候是不是真的在转。

// TestTheLoadingSpinnerSitsInTheTopFortyPercent 钉住「40vh 那一格」。
//
// # 判据是画面：转圈的中心落在哪
//
// 上游那几处是 `h-[40vh]` 那一格（`later/index.tsx:230`、`history/index.tsx:280`、
// `follow-list/index.tsx:306`、`music-recommend/index.tsx:319`），转圈在**那一格
// 的正中**——所以它离这一格的顶是 20vh（那一格的一半）。
//
// ⚠️ **这一条画的是那一格本身**（`withView` 绕开整窗），所以「这一格的顶」就是
// 屏幕的顶：转圈的中心必须在 20vh 的地方。整窗那一层（每一页的首屏）由
// `TestEveryFirstScreenIsACenteredSpinner` 钉。
func TestTheLoadingSpinnerSitsInTheTopFortyPercent(t *testing.T) {
	const winH = 800
	b := newTestShell(t, at(1200, winH), withView(func(s *Shell) func(*ui.Context) {
		return func(c *ui.Context) {
			s.applyFrameTheme(c)
			drawLoading(c, "test:loading", "正在加载…")
		}
	}))

	r, ok := b.tt.Find("正在加载…")
	if !ok {
		t.Fatalf("画面上没有那枚转圈，文字有 %q", b.tt.Texts())
	}
	if r.W != loadingSpinnerSize || r.H != loadingSpinnerSize {
		t.Errorf("转圈是 %.0f×%.0f，want %d×%d（HeroUI 的 md 那一档）",
			r.W, r.H, loadingSpinnerSize, loadingSpinnerSize)
	}
	// 中心在那一格（40vh）的正中：离这一格的顶 20vh。
	want := winH * (loadingShare / 2)
	if got := float64(r.Y + r.H/2); math.Abs(got-want) > 2 {
		t.Errorf("转圈的中心在 %.1f，want %.1f（那一格 %.0f 高，它的正中）",
			got, want, winH*loadingShare)
	}
}

// TestEveryFirstScreenIsACenteredSpinner 钉住「每一页的首屏在拉的时候是居中的
// 转圈」。
//
// # 判据是画面：那一枚转圈在不在、多大、落在哪
//
// ⚠️ **「转圈」是按「那个名字的元素是一枚 32×32 的方块」认的**：转圈没有可见的
// 字，它在无障碍树里的名字是那一句话（`Label`），而 Tester 认的就是名字——
// 所以 `Find` 拿回来的是**转圈那一格**，不是一行字（改之前那一行是 15px 的字，
// 高度 17.67）。
//
// ⚠️ **本地音乐那一页抓不住**：它扫的是硬盘不是网络，而扫一个空目录一帧之内就
// 完事（真机上的加载态也是一闪而过）。那一页由
// `TestTheLocalMusicLoadingComesFromTheSharedDrawer`（代码那一层）钉住。
func TestEveryFirstScreenIsACenteredSpinner(t *testing.T) {
	for _, page := range []struct {
		name    string
		loading string
		path    string
		// centered 报这一页的加载态是不是摆在**整个内容区**里（于是「居中」就是
		// 内容区的正中）。我的关注 / 用户动态那两页的加载态在右半边那一栏里，
		// 不是整个内容区——它们只验「有转圈」。
		centered bool
		// rows 报这一页的首屏是列表骨架屏（八行占位）而不是一枚转圈。
		rows bool
	}{
		{name: "搜索", loading: "正在加载…", path: searchPath + "?q=%E5%91%A8%E6%9D%B0%E4%BC%A6", centered: true, rows: true},
		{name: "收藏夹", loading: "正在加载…", path: collectionPathPrefix + "12345", centered: true, rows: true},
		{name: "稍后再看", loading: "正在加载…", path: laterPath, centered: true, rows: true},
		{name: "历史记录", loading: "正在加载…", path: historyPath, centered: true, rows: true},
		{name: "我的关注", loading: "正在加载关注的人…", path: followPath, rows: true},
		{name: "用户动态", loading: "正在加载动态…", path: dynamicFeedPath, rows: true},
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
				t.Fatalf("%s 的首屏上没有那枚转圈，文字有 %q", page.name, tt.Texts())
			}
			if page.rows {
				if r.H <= 0 || r.H > skeletonRows*skeletonRowH { // 矮窗口里会被视口截短
					t.Errorf("%s 的首屏骨架屏高 %.0f，want (0, %d]", page.name, r.H, skeletonRows*skeletonRowH)
				}
			} else if r.W != loadingSpinnerSize || r.H != loadingSpinnerSize {
				t.Errorf("%s 的首屏上那一枚是 %.0f×%.0f，want %d×%d——"+
					"「加载中」是一枚转圈，不是一行字", page.name, r.W, r.H,
					loadingSpinnerSize, loadingSpinnerSize)
			}
			if !page.centered {
				return
			}
			// 居中：转圈的中心在**内容区**的正中（侧栏右边到窗口右边那一段）。
			side := sideTargetWidth(s.app.SettingsSnapshot())
			want := (float32(1200) + float32(side)) / 2
			if got := r.X + r.W/2; math.Abs(float64(got-want)) > 2 {
				t.Errorf("%s 的转圈中心在 %.1f，want %.1f（内容区的正中，侧栏 %.0f 宽）",
					page.name, got, want, side)
			}
		})
	}
}

// TestTheLoadingStateIsNotTheEmptyState 钉住「加载中和空状态是两档，不混」。
//
// # 判据是画面：同一页上，两档长得不一样
//
// 改之前（票 #138）这两档是**同一个格子**：加载中是一行同样大小的淡字，空状态
// 是一枚插画 + 两行字。而这一票（#178）把它们分成两档：加载中是**一枚转圈**
// （上游那几页的首屏就是它），空状态是插画 + 两行字。
//
// 「不混」有三条：
//
//  1. 加载中那一档**没有**空状态的那枚插画、**没有**那两行字；
//  2. 空状态那一档**没有**转圈；
//  3. 两档都在这一块的正中（横向同一个位置）——「居中」是那一块的性质，不是
//     某一档的性质。
func TestTheLoadingStateIsNotTheEmptyState(t *testing.T) {
	st := newSearchStub(2, 0)
	st.hold("周杰伦", 1)
	_, tt := newSearchPage(t, "周杰伦", st)
	tt.Frame()

	loading, ok := tt.Find("正在加载…")
	if !ok {
		t.Fatalf("没有那枚转圈，文字有 %q", tt.Texts())
	}
	if tt.HasText("暂无内容") {
		t.Errorf("还在加载就说「暂无内容」——那是把「还没好」说成「没有」，文字有 %q", tt.Texts())
	}
	// 转圈不是一行字：15px 那一档的行高是 17.67，而它是 32 的一方块。
	if loading.H != skeletonRows*skeletonRowH {
		t.Errorf("加载中那一块高 %.2f，want %d——列表首屏是八行骨架屏，不是一行字",
			loading.H, skeletonRows*skeletonRowH)
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
	if _, ok := tt.Find("正在加载…"); ok {
		t.Error("结果回来了还转着——加载中和空状态混成了一档")
	}
	// 横向：两档都在这一块的正中。
	lc, ec := loading.X+loading.W/2, empty.X+empty.W/2
	if math.Abs(float64(lc-ec)) > 2 {
		t.Errorf("转圈居中在 %.1f，「暂无内容」居中在 %.1f——"+
			"「居中」是这一块的性质，两档得在同一个位置上", lc, ec)
	}
}

// TestTheBusyButtonTurnsAndGreysOut 钉住「按钮在忙的时候转起来 + 灰下去」。
//
// # 判据是画面：那一枚按钮
//
// 四件事：
//
//  1. **在忙的时候按钮里有转圈**（改之前只是把字换成「上传中…」）；
//  2. **它真的在转**（推钟之后画面变了；不忙的时候同样的推钟画面不动）；
//  3. **按钮灰下去**（框架对禁用的控件是整个乘 0.5 不透明度，
//     `mygo/ui/paint.go:209-212`；上游 HeroUI 的按钮在 `isLoading` 时也是禁用的）；
//  4. **字还在**（拿掉之后按钮缩成一枚小方块，状态一变宽度就跳一下——
//     `update_entry.go` 里记着那个实测值）。
//
// ⚠️ **转圈在无障碍树里也有那个名字**（`Label`），而按钮自己也有、那个字也有——
// 所以在忙的时候那个名字出现**三次**（按钮 / 转圈 / 字），不忙的时候只有**一次**
// （那个字）。少一次就是转圈不在。
func TestTheBusyButtonTurnsAndGreysOut(t *testing.T) {
	mk := func(busy bool) *testShell {
		return newTestShell(t, at(1200, 800), withView(func(s *Shell) func(*ui.Context) {
			return func(c *ui.Context) {
				s.applyFrameTheme(c)
				ui.Row(c).Gap(24).Padding(40).AlignItems(ui.Center).Children(func() {
					if busy {
						busyPrimaryButton(c, "上传中…")
					} else {
						ui.PrimaryButton(c, "上传中…")
					}
				})
			}
		}))
	}
	idle := mk(false)
	busy := mk(true)

	// ① 转圈在。
	if n := countText(idle.tt, "上传中…"); n != 1 {
		t.Errorf("不忙的时候「上传中…」出现 %d 次，want 1（只有那个字）", n)
	}
	if n := countText(busy.tt, "上传中…"); n != 3 {
		t.Errorf("在忙的时候「上传中…」出现 %d 次，want 3（按钮 + 它里面那枚转圈 + 那个字）——"+
			"按钮里没有转圈", n)
	}

	// 按钮那一枚在忙的时候**有名字**（`Label`），所以 `Find` 拿回来的是按钮本身；
	// 不忙的时候按钮没有名字，`Find` 拿回来的是那个字。⚠️ 两者的左边不是同一个地方
	// （在忙的时候左边多了一段转圈），所以下面的取样点用**忙的那一枚**算。
	btn, ok := busy.tt.Find("上传中…")
	if !ok {
		t.Fatalf("找不到那枚按钮，文字有 %q", busy.tt.Texts())
	}
	theme := BuildTheme(busy.s.app.SettingsSnapshot(), false)
	pad := theme.Space(3.5) // 按钮左右那圈内边距（`styleButton` 的 `t.Space(3.5)`）

	// ② 它真的在转：推钟之后，转圈所在的那一格的画面变了；不忙的时候同样的推钟
	// 画面不动（那个字是静的）。
	spinX := int(btn.X) + int(pad)
	spinY := int(btn.Y)
	if busy.spinPixelsChanged(spinX, spinY, busySpinnerSize, int(btn.H)) == 0 {
		t.Error("推了钟，转圈那一格却一点没变——它没有转起来")
	}
	if idle.spinPixelsChanged(spinX, spinY, busySpinnerSize, int(btn.H)) != 0 {
		t.Error("没有转圈的那一枚，推钟之后画面却变了")
	}

	// ③ 灰下去：同一处底色，不忙的是纯主色，在忙的是它和底色各一半。
	px, py := int(btn.X)+4, int(btn.Y+btn.H/2)
	normal := idle.tt.Image().RGBAAt(px, py)
	dim := busy.tt.Image().RGBAAt(px, py)
	if normal == dim {
		t.Errorf("在忙的时候按钮底色没变（%v）——它没有灰下去", dim)
	}
	page := busy.tt.Image().RGBAAt(10, 10)
	want := mixHalf(normal, page)
	if !closeRGBA(dim, want, 3) {
		t.Errorf("在忙的时候按钮底色是 %v，want %v（主色的一半）", dim, want)
	}

	// ④ 字还在：按钮不是一枚缩成小方块的空壳（实测空壳是 42 宽，而带着那句
	// 字的按钮是 77 上下——那句字本身就有 49）。
	if btn.W < 60 {
		t.Errorf("在忙的时候按钮只有 %.0f 宽——字被拿掉了（实测空壳是 42）", btn.W)
	}
}

// spinPixelsChanged 推钟再画一帧之后，返回**那一格里变了的像素**有几个。
//
// ⚠️ **推钟用 `Advance` + `Frame`，不数帧**：框架的钟站着不动，数帧那种等法会
// **静默失效**——不报错、不超时，直接放行（AGENTS §2）。
func (sh *testShell) spinPixelsChanged(x, y, w, h int) int {
	before := sh.tt.Image()
	sh.tt.Advance(120 * time.Millisecond)
	sh.tt.Frame()
	after := sh.tt.Image()
	n := 0
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			if before.RGBAAt(xx, yy) != after.RGBAAt(xx, yy) {
				n++
			}
		}
	}
	return n
}

// mixHalf 把 c 和底色各取一半——框架对禁用的控件就是整个乘 0.5 不透明度。
func mixHalf(c, bg color.RGBA) color.RGBA {
	return color.RGBA{
		R: uint8((int(c.R) + int(bg.R)) / 2),
		G: uint8((int(c.G) + int(bg.G)) / 2),
		B: uint8((int(c.B) + int(bg.B)) / 2),
		A: 255,
	}
}

// closeRGBA 报两个颜色差得近不近（逐通道，允许一点误差）。
func closeRGBA(got, want color.RGBA, tol int) bool {
	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	return d(got.R, want.R) <= tol && d(got.G, want.G) <= tol && d(got.B, want.B) <= tol
}
