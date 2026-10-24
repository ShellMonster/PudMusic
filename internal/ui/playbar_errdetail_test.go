package ui

// 播放条上那一句错的「**详情层**」（票 #320）。
//
// # 这一组盯的是「用户能不能读到完整那一句」
//
// `bili.Friendly` 那张表里的话信息量很大（「B 站没有给这一条单独的音频流（歌单、
// 活动那一种多半是这样）。在 B 站里打开它听，或者换一首放」四十来个字），而
// 播放条上那一行只有 140 像素宽（1200 宽的窗口，实测）——于是它被压成一行
// 省略号，用户常常只读到半句话。
//
// 这一组断三件事：
//
//  1. **点得开**：点那一句红字，出来一块面板，里面是完整那一句（多行排开）。
//  2. **播放条一个像素都没动**：面板是浮层，那一行还是一行，播放条还是 76 高。
//  3. **错没了面板就收**：不然下一句错一上来，面板是开着的。

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// errLongSentence 是 `bili.Friendly` 那张表里那种**信息量很大**的话。
//
// 四十来个字——放在 140 像素宽的一行里只剩「打不开：B 站没有给这一…」这样的
// 碎片（票 #320 的现象）。
const errLongSentence = "B 站没有给这一条单独的音频流（歌单、活动那一种多半是这样）。在 B 站里打开它听，或者换一首放"

// errBarTester 造一个「播放条在窗口底下」的画面。
//
// ⚠️ **播放条要摆在底下**：浮层是往**上**弹的（底下是窗口边，没有位置），而
// 摆在顶上时它会被窗口裁掉半张——那样测的就不是「用户看得见的那块面板」了。
//
// ⚠️ **状态是外面那个指针**：测试要能改它（错没了那一档要看面板收不收）。
func errBarTester(t *testing.T, st *app.State, width, height int, dark bool) (*mygoui.Tester, *Shell) {
	t.Helper()
	var b *testShell
	b = newTestShell(t, at(width, height), systemIsDark(dark), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			mygoui.Column(c).Fill().Children(func() {
				mygoui.Row(c).Grow(1)
				s.viewPlayBar(c, *st)
			})
		}
	}))
	tt := b.tt
	tt.Frame()
	return tt, b.s
}

// openErrDetail 点开播放条上那一句错，返回点开之后那一帧。
//
// ⚠️ **点的是那一句红字的最左边**：那一句的盒子是整行那么宽（`Grow(1)`），
// 而画出来的字只有一百多像素——点盒子中间会落在字后面的空白上。
func openErrDetail(t *testing.T, tt *mygoui.Tester, msg string) {
	t.Helper()
	r, ok := tt.Find("打不开：" + msg)
	if !ok {
		t.Fatalf("画面上没有那一句错：%q", tt.Texts())
	}
	tt.ClickAt(r.X+4, r.Y+r.H/2)
	tt.Frame()
}

// TestClickingTheErrorOpensTheWholeSentence 盯「点得开，完整那一句拿得到」。
//
// ⚠️ **判据是「面板里那句话排开了不止一行」**，不是「那句话在画面上」：播放条
// 上那一句的**文本节点里也是完整那一句**（只是画的时候被截了），所以「它在
// 树上」证明不了任何事——要证明的是它**排开了、看得全**。
func TestClickingTheErrorOpensTheWholeSentence(t *testing.T) {
	st := app.State{Err: errLongSentence}
	tt, _ := errBarTester(t, &st, 1200, 320, false)

	// 还没点：面板不在。
	if _, ok := tt.Find("错误详情"); ok {
		t.Fatalf("还没点就有详情面板了：%q", tt.Texts())
	}

	openErrDetail(t, tt, errLongSentence)

	panel, ok := tt.Find("错误详情")
	if !ok {
		t.Fatalf("点开之后没有详情面板：%q", tt.Texts())
	}
	// 完整那一句在面板里。
	body, ok := tt.Find(errLongSentence)
	if !ok {
		t.Fatalf("面板里没有完整那一句：%q", tt.Texts())
	}
	// 🔴 **它排开了**：一行是 14 像素高（12 号字），两行就是 28。不止一行，
	// 才说明这句话是**排开的、看得全的**，而不是又被截了一次。
	if body.H <= 20 {
		t.Errorf("面板里那句话只有 %.1f 高——它被压成一行了（= 又截了一次）：%q",
			body.H, tt.Texts())
	}
	// 🔴 **它排在那一行上面**（不是压着它）：面板是锚在那一句上的一个气泡，
	// 底边在那一句的上沿——压住了它自己解释的那一行，就成了「说了有话看不见」。
	bar, ok := tt.Find("打不开：" + errLongSentence)
	if !ok {
		t.Fatalf("画面上没有那一句错：%q", tt.Texts())
	}
	if panel.Y+panel.H > bar.Y {
		t.Errorf("详情面板压着自己那一行：面板底 %.1f，那一行顶 %.1f",
			panel.Y+panel.H, bar.Y)
	}
}

// TestThePlayBarDoesNotMoveWhenTheDetailOpens 盯「播放条还是一行、还是 76 高」。
//
// ⚠️ **判据是「一个像素都没动」**：面板是浮层，它挂在窗口的覆盖层上——它要是
// 挤进了播放条的布局里，播放条就会被撑成两行，而**症状是静默的**：最后一行
// 被 76 高裁掉，界面上看不出坏了（文本节点还在，测试也还找得到它）。
func TestThePlayBarDoesNotMoveWhenTheDetailOpens(t *testing.T) {
	st := app.State{Err: errLongSentence}
	tt, _ := errBarTester(t, &st, 1200, 320, false)

	before, ok := tt.Find("打不开：" + errLongSentence)
	if !ok {
		t.Fatalf("画面上没有那一句错：%q", tt.Texts())
	}
	openErrDetail(t, tt, errLongSentence)
	after, ok := tt.Find("打不开：" + errLongSentence)
	if !ok {
		t.Fatalf("点开之后那一句错不见了：%q", tt.Texts())
	}
	if before != after {
		t.Errorf("打开详情之后播放条上那一句动了：%v → %v", before, after)
	}
	// 🔴 **还是一行**：一行 12 号字是 14 像素高。两行（28）就说明它换行了，
	// 而 76 高的播放条装不下第二行。
	if after.H > 20 {
		t.Errorf("播放条上那一句错高 %.1f——它换行了（76 高的播放条装不下两行）", after.H)
	}
}

// TestTheDetailClosesWhenTheErrorIsGone 盯「错没了，面板就收」。
//
// ⚠️ **不收的话**：用户重试成功了，播放条上那一行没了，而**面板还开着**——
// 它上面写着「打不开」，而其实能播了。下一句错一上来，面板也是开着的。
func TestTheDetailClosesWhenTheErrorIsGone(t *testing.T) {
	st := app.State{Err: errLongSentence}
	tt, _ := errBarTester(t, &st, 1200, 320, false)

	openErrDetail(t, tt, errLongSentence)
	if _, ok := tt.Find("错误详情"); !ok {
		t.Fatalf("点开之后没有详情面板：%q", tt.Texts())
	}

	// 错没了（重试成功了）。
	st.Err = ""
	tt.Frame()

	if _, ok := tt.Find("错误详情"); ok {
		t.Errorf("错没了，详情面板还开着：%q", tt.Texts())
	}
}

// TestTheDetailClosesWhenTheUserPressesEscape 盯「按 Esc 能关掉那块面板」。
//
// ⚠️ **判据是「面板不在画面上了」**：浮层是框架那个 `ui.Popover` 建的，而
// 「点外面 / 按 Esc 关掉」是它自己处理的（`mygo/ui/base.go` 的 `popover`）——
// 代码里这一层只管开关本身。
//
// ⚠️ **Esc 那一条有条件**：框架把键**先给焦点链**（焦点上的控件和它周围那一圈），
// 它们都不处理才轮到浮层（`mygo/ui/input.go` 的 `routeKeys`）。所以焦点在一个
// **自己吃 Esc** 的控件里时（`ui.SearchField` 就是）这一块关不掉——那条限制
// 写在 `playbar.go` 里 `viewTrackError` 的注释上。
//
// 这一条钉的是「焦点在别处时关得掉」这一档：点开之后焦点在锚点那一行上。
func TestTheDetailClosesWhenTheUserPressesEscape(t *testing.T) {
	st := app.State{Err: errLongSentence}
	tt, _ := errBarTester(t, &st, 1200, 320, false)

	openErrDetail(t, tt, errLongSentence)
	if _, ok := tt.Find("错误详情"); !ok {
		t.Fatalf("点开之后没有详情面板：%q", tt.Texts())
	}

	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()

	if _, ok := tt.Find("错误详情"); ok {
		t.Errorf("按了 Esc 之后详情面板还开着：%q", tt.Texts())
	}
}

// TestTheDetailClosesWhenTheUserClicksOutside 盯「点外面能关掉那块面板」。
//
// 这一条和上面那条是**两个手势**（框架的 `popover` 分开判：`PressedOutside()`
// 和 `OverlayShortcut(0, KeyEscape)`），而「点外面」那一条**没有**焦点上的
// 前提——指针点在哪就是哪。
func TestTheDetailClosesWhenTheUserClicksOutside(t *testing.T) {
	st := app.State{Err: errLongSentence}
	tt, _ := errBarTester(t, &st, 1200, 320, false)

	openErrDetail(t, tt, errLongSentence)
	if _, ok := tt.Find("错误详情"); !ok {
		t.Fatalf("点开之后没有详情面板：%q", tt.Texts())
	}

	// 点面板外面那一块空白（播放条上面那一大片）。
	tt.ClickAt(600, 40)
	tt.Frame()

	if _, ok := tt.Find("错误详情"); ok {
		t.Errorf("点了外面之后详情面板还开着：%q", tt.Texts())
	}
}

// TestHoveringTheErrorStillShowsTheWholeSentence 盯**悬停那一条路没被弄坏**。
//
// 这一层是「点开」之前就有的（票 #282 的评审加的），而它更省事——指针停上去就
// 有全文。这一条钉住它还在。
//
// ⚠️ **判据是「同一句话在画面上出现了两次」**：一次是播放条上那句（画出来是
// 截断的），一次是提示框里那句（完整的）。
func TestHoveringTheErrorStillShowsTheWholeSentence(t *testing.T) {
	st := app.State{Err: errLongSentence}
	tt, _ := errBarTester(t, &st, 1200, 320, false)

	r, ok := tt.Find("打不开：" + errLongSentence)
	if !ok {
		t.Fatalf("画面上没有那一句错：%q", tt.Texts())
	}
	tt.Move(r.X+4, r.Y+r.H/2)
	tt.Frame()

	n := 0
	for _, s := range tt.Texts() {
		if s == "打不开："+errLongSentence {
			n++
		}
	}
	if n < 2 {
		t.Errorf("悬停之后提示框里没有完整那一句（画面上只出现过一次）：%q", tt.Texts())
	}
}

// TestTheTooltipDoesNotStackOnTopOfTheDetail 盯「提示框不和面板叠在一起」。
//
// ⚠️ **这两块摆在同一个位置**（都在那一行的上面），而它们说的是同一句话：
// 面板开着的时候指针移开又移回来，提示框就会盖在那块面板上——屏幕上是一块
// 小提示压着一块面板，读起来像坏了。
func TestTheTooltipDoesNotStackOnTopOfTheDetail(t *testing.T) {
	st := app.State{Err: errLongSentence}
	tt, _ := errBarTester(t, &st, 1200, 320, false)

	openErrDetail(t, tt, errLongSentence)

	// 指针移开，再移回来（移开是为了让提示框那一档「按过它了就收起」复位）。
	tt.Move(600, 120)
	tt.Frame()
	r, ok := tt.Find("打不开：" + errLongSentence)
	if !ok {
		t.Fatalf("画面上没有那一句错：%q", tt.Texts())
	}
	tt.Move(r.X+4, r.Y+r.H/2)
	tt.Frame()

	n := 0
	for _, s := range tt.Texts() {
		if s == "打不开："+errLongSentence {
			n++
		}
	}
	if n != 1 {
		t.Errorf("面板开着的时候提示框还挂着（同一句话出现了 %d 次）：%q", n, tt.Texts())
	}
}
