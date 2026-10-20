package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 这一组跑的是 MyGo 的 Tester：和真窗口一模一样的 layout、拖动和系统菜单，
// 只是画到内存里。所以「拖窄了会自动折叠」「右键菜单有几项」这些都能自动验，
// 不需要人守在窗口前面拖。

// sideShell 造一个挂了真设置文件的 Sidebar 场景，返回设置文件路径。
//
// 为什么不用 newShell：那个不挂设置，写进去的每一笔宽度和折叠态都会被
// `ErrSettingsNotMounted` 挡掉——这一组要验的正好是「存没存住」。
func sideShell(t *testing.T) (*Shell, *app.App, *mygoui.Tester, string) {
	t.Helper()
	// 宽度过渡是 200ms 的动画，无头 Tester 一帧最多 settle 20 帧，走不完。
	// 开了 ReduceMotion，动画直接落到终点——这里要验的是**终点**，动的那段
	// 200ms 人眼看的，不是能自动断言的东西。
	//
	// `withSettings` 要一份 Settings 值，而这一条要的是「盘上一个**还不存在**
	// 的设置文件」（走的是真程序第一次启动那条路），所以路径自己给。
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	a := app.New()
	if err := a.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	b := newTestShell(t, onApp(a),
		withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}))
	tt := b.tt
	tt.Frame()
	return b.s, a, tt, path
}

// dragFrom 按下 (x, y)，分四步移到 (tx, ty)，在那里松手。
//
// 必须分步：MyGo 记的是**这一帧的位移**，一步跳到位和真实拖动不是同一件事。
func dragFrom(tt *mygoui.Tester, x, y, tx, ty float32) {
	tt.Press(x, y)
	for k := 1; k <= 4; k++ {
		f := float32(k) / 4
		tt.Move(x+(tx-x)*f, y+(ty-y)*f)
	}
	tt.Release(tx, ty)
	tt.Frame()
}

// gripX 是右缘把手当前中心在哪：侧栏宽减去把手那一半。
//
// 把手是侧栏那一行的最后一个子元素，宽 sideGrip，贴右边。侧栏宽多少这里
// 传多少——测试里那几个宽度都是我们自己给的确定值。
// gripX 是把手中心那一列的横坐标。
//
// ⚠️ **那条 1 像素的分隔线（`sideDividerW`）在里面**：它画在侧栏框的**内侧**
// （MyGo 的 `Border` 文档：「draws a border of width DIPs inside the element's
// edges … the padding and the children are inside it」），所以把手整个落在
// 分隔线左边一格。少减这 1 的话，按下去的点落在分隔线上而不是把手上——而
// 按在分隔线上的症状是「拖不动」，不会报「差了一个像素」。
func gripX(sidebarWidth float32) float32 { return sidebarWidth - sideDividerW - sideGrip/2 }

// sideWidthOnScreen 是这一帧画面上侧栏实际有多宽。
//
// 折叠按钮铺满侧栏正文（Column 把子元素拉满宽），所以它的宽度加回右边那条
// 把手和那条分隔线就是侧栏的宽。找的是按钮的**无障碍名字**而不是那个「«」的
// 字形——找字形拿到的是文字那一小块的盒子（8 宽），不是按钮的。
//
// 取的是**画面上的盒子**，不是设置里的数：设置对了但画出来不对，同样是没做。
func sideWidthOnScreen(t *testing.T, tt *mygoui.Tester) float32 {
	t.Helper()
	for _, name := range []string{"折叠侧栏", "展开侧栏"} {
		if r, ok := tt.Find(name); ok {
			return r.W + sideGrip + sideDividerW
		}
	}
	t.Fatalf("画面上找不到折叠按钮，文字有 %q", tt.Texts())
	return 0
}

// TestTheSidebarTransitionIsBiuTwoHundredMilliseconds 钉住验收里那个数。
//
// ⚠️ 「走的是过渡，不是一步到位」这件事在无头 Tester 里**钉不住**，别再试：
// MyGo 的动画按墙上时间推进，而 `Tester.Frame()` 一帧里 settle 最多跑 20 遍
// view（`ui/headless.go:215`）。20 遍在 -race 下足够走完 200ms，中间态一次
// 也抓不到——写成「折叠那一帧的宽既不是 300 也不是 72」，实测 -race 下
// 20 次错 4 次。那种测试比没有更糟：它会把「跑得慢」报成「功能没做」。
//
// 能钉住的是这个数：过渡时长就是 200ms（Biu 的 `duration-200`，
// `biu/src/layout/side/index.tsx:146`）。谁把它换成弹簧、或者随手写成
// 150ms，这条会红。过渡本身由 `viewSide` 里那句 `AnimateTo` 保证
// （拖动那一支用 `Jump`，Biu 的 `transition-none`）。
func TestTheSidebarTransitionIsBiuTwoHundredMilliseconds(t *testing.T) {
	if sideWidthTransition != 200*time.Millisecond {
		t.Errorf("侧栏过渡是 %v，want 200ms（Biu 的 duration-200）", sideWidthTransition)
	}
	if sideGrip != 8 {
		t.Errorf("把手宽是 %d，want 8（Biu 的 w-2）", sideGrip)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 拖宽、拖窄、折叠
// ─────────────────────────────────────────────────────────────────────────────

// TestDraggingTheGripBelowTheMinWidthCollapsesTheSidebar 是验收第 2 条：
// 侧栏拖到 160 像素以下自动折叠到 72。
func TestDraggingTheGripBelowTheMinWidthCollapsesTheSidebar(t *testing.T) {
	_, a, tt, _ := sideShell(t)

	if got := sideWidthOnScreen(t, tt); got != app.DefaultSideMenuWidth {
		t.Fatalf("起点侧栏是 %v 宽，want %v", got, app.DefaultSideMenuWidth)
	}

	// 200 → 往左拖 96 → 104。104 < 160，落进折叠区。
	start := gripX(app.DefaultSideMenuWidth)
	tt.Press(start, 400)
	for _, to := range []float32{-24, -48, -72, -96} {
		tt.Move(start+to, 400)
	}

	// 还没松手就该看着是折叠的：折叠的样子是**拖的过程中**就出现的，
	// 不是松手之后才跳过去。
	if got := sideWidthOnScreen(t, tt); got != app.CollapsedSideMenuWidth {
		t.Errorf("拖到 104 时画面上的侧栏是 %v 宽，want %v（还在拖就该折叠）",
			got, app.CollapsedSideMenuWidth)
	}

	tt.Release(start-96, 400)
	tt.Frame()

	st := a.SettingsSnapshot()
	if !st.SideMenuCollapsed {
		t.Error("拖到 160 以下松手，没有记成折叠")
	}
	// 存下去的是 160 不是 72：存 72 的话，拖回来时起点就是折叠宽度，
	// 怎么拖都展不开。
	if st.SideMenuWidth != app.MinSideMenuWidth {
		t.Errorf("存下去的宽度是 %v，want %v", st.SideMenuWidth, app.MinSideMenuWidth)
	}
	if got := sideWidthOnScreen(t, tt); got != app.CollapsedSideMenuWidth {
		t.Errorf("松手之后侧栏是 %v 宽，want %v", got, app.CollapsedSideMenuWidth)
	}
}

// TestDraggingBackOutExpandsTheSidebar 是同一条验收的另一半：拖回自动展开。
func TestDraggingBackOutExpandsTheSidebar(t *testing.T) {
	_, a, tt, _ := sideShell(t)

	// 先折叠：点按钮比拖一遍短。
	if err := tt.Click("«"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := sideWidthOnScreen(t, tt); got != app.CollapsedSideMenuWidth {
		t.Fatalf("折叠之后是 %v 宽，want %v", got, app.CollapsedSideMenuWidth)
	}

	// 从 72 拖到 172：+100 > 160，展开。
	dragFrom(tt, gripX(app.CollapsedSideMenuWidth), 400, gripX(app.CollapsedSideMenuWidth)+100, 400)

	st := a.SettingsSnapshot()
	if st.SideMenuCollapsed {
		t.Error("拖宽了还是记着折叠")
	}
	if st.SideMenuWidth != app.MinSideMenuWidth+12 {
		t.Errorf("存下去的宽度是 %v，want %v", st.SideMenuWidth, app.MinSideMenuWidth+12)
	}
	if got := sideWidthOnScreen(t, tt); got != app.MinSideMenuWidth+12 {
		t.Errorf("画面上的侧栏是 %v 宽，want %v", got, app.MinSideMenuWidth+12)
	}
}

// TestDraggingTheGripBeyondItsOwnBoxKeepsWidening 盯的是一个具体的坑。
//
// 把手只有 8 宽。`Pressed()` 多一条「指针必须在元素框内」的条件
// （`ui/input.go:1030`），所以指针往左一挪 9 个像素它就报 false——
// 侧栏最多只能拖动 16 像素宽就卡住。这条测试把「指针早就出框了还在跟着
// 手指走」钉住，换成 `Pressed()` 它会当场红。
func TestDraggingTheGripBeyondItsOwnBoxKeepsWidening(t *testing.T) {
	_, _, tt, _ := sideShell(t)

	start := gripX(app.DefaultSideMenuWidth)
	tt.Press(start, 400)
	// 一路往左拖 30：指针离开把手那 8 宽的框是第二步就开始的事。
	for _, to := range []float32{-10, -20, -25, -30} {
		tt.Move(start+to, 400)
	}
	tt.Release(start-30, 400)
	tt.Frame()

	// 200 - 30 = 170，还够着 160，所以看得到确切的宽度而不是折叠后的 72。
	if got := sideWidthOnScreen(t, tt); got != app.DefaultSideMenuWidth-30 {
		t.Errorf("拖到 -30 之后侧栏是 %v 宽，want %v：指针早就出了把手那 8 宽的框",
			got, app.DefaultSideMenuWidth-30)
	}
}

// TestDraggingWiderIsCappedAt480 验上限。
//
// 不钳的话拖到 600 松手就存 600，下次一打开侧栏把整个窗口吃掉，内容区一点
// 不剩。
func TestDraggingWiderIsCappedAt480(t *testing.T) {
	_, a, tt, _ := sideShell(t)

	dragFrom(tt, gripX(app.DefaultSideMenuWidth), 400, gripX(app.DefaultSideMenuWidth)+400, 400)

	if got := a.SettingsSnapshot().SideMenuWidth; got != app.MaxSideMenuWidth {
		t.Errorf("拖到 600 存下去的是 %v，want %v", got, app.MaxSideMenuWidth)
	}
	if got := sideWidthOnScreen(t, tt); got != app.MaxSideMenuWidth {
		t.Errorf("画面上的侧栏是 %v 宽，want %v", got, app.MaxSideMenuWidth)
	}
}

// TestTheSidebarWidthSurvivesARestart 验宽度也存住了。
//
// 和收藏夹顺序一样走真文件：拖 → flush → 关掉重开 → 画面上还是那个宽。
func TestTheSidebarWidthSurvivesARestart(t *testing.T) {
	_, a, tt, path := sideShell(t)

	dragFrom(tt, gripX(app.DefaultSideMenuWidth), 400, gripX(app.DefaultSideMenuWidth)+120, 400)
	if err := a.FlushSettings(); err != nil {
		t.Fatal(err)
	}
	if err := a.CloseSettings(); err != nil {
		t.Fatal(err)
	}

	_, _, tt2, _ := sideShellOn(t, path)
	if got := sideWidthOnScreen(t, tt2); got != app.DefaultSideMenuWidth+120 {
		t.Errorf("重开之后侧栏是 %v 宽，want %v", got, app.DefaultSideMenuWidth+120)
	}
}

// sideShellOn 是「已经有设置文件」的那一条：先写盘，再挂上去重开。
func sideShellOn(t *testing.T, path string) (*Shell, *app.App, *mygoui.Tester, string) {
	t.Helper()
	a := app.New()
	if err := a.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	b := newTestShell(t, onApp(a),
		withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}))
	tt := b.tt
	tt.Frame()
	return b.s, a, tt, path
}

// TestTheCollapseButtonRoundTrips 验底下那个折叠按钮：点了就折叠，再点就
// 展开，而且折叠态也存进设置（Biu 的 `sideMenuCollapsed`）。
func TestTheCollapseButtonRoundTrips(t *testing.T) {
	_, a, tt, _ := sideShell(t)

	if err := tt.Click("«"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.SettingsSnapshot().SideMenuCollapsed {
		t.Error("点了折叠按钮，设置里没记折叠")
	}
	if got := sideWidthOnScreen(t, tt); got != app.CollapsedSideMenuWidth {
		t.Errorf("折叠之后是 %v 宽，want %v", got, app.CollapsedSideMenuWidth)
	}
	if _, ok := tt.Find("»"); !ok {
		t.Errorf("折叠之后按钮没有换成展开的样子，文字有 %q", tt.Texts())
	}

	if err := tt.Click("»"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.SettingsSnapshot().SideMenuCollapsed {
		t.Error("再点一次还是折叠着")
	}
	// 按钮只翻折叠那一个开关，宽度没动过，所以展开回来还是原来那个宽
	// （Biu 也一样：`onToggleCollapsed` 只改 sideMenuCollapsed）。
	if got := sideWidthOnScreen(t, tt); got != app.DefaultSideMenuWidth {
		t.Errorf("展开之后是 %v 宽，want %v（按钮只翻折叠开关，不动宽度）",
			got, app.DefaultSideMenuWidth)
	}
}

// TestTheSidebarSaysSoWhenItsWidthCannotBeSaved 验设置只读时不悄悄失败。
//
// 拖完松手宽度没变，而界面上一个错都没有——用户只会以为程序坏了。
func TestTheSidebarSaysSoWhenItsWidthCannotBeSaved(t *testing.T) {
	_, a, tt, _ := sideShell(t)

	if err := a.MountSettings(writeReadOnlySettings(t)); err == nil {
		t.Fatal("来自更新版本的设置没有报错")
	}
	tt.Frame()

	dragFrom(tt, gripX(app.DefaultSideMenuWidth), 400, 196+60, 400)

	if !tt.HasText("侧栏宽度存不住") {
		t.Errorf("设置只读时没有告诉用户，文字有 %q", tt.Texts())
	}
}

// writeReadOnlySettings 造一份 schema 更高的设置文件：只能读不能写。
func writeReadOnlySettings(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	if err := os.WriteFile(path, []byte(`{"schemaVersion": 99, "appSettings": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// ─────────────────────────────────────────────────────────────────────────────
// 收藏夹两组
// ─────────────────────────────────────────────────────────────────────────────

// favShell 是种了几条收藏夹的场景。
//
// 两组的标题带前缀（「我建的 1」/「我收的 1」）——B 站那边完全可能有同一个
// 收藏夹既是自己创建的又是自己收藏的，两组同名的话测试根本分不清右键点到了
// 哪一行。
func favShell(t *testing.T, created, collected int) (*Shell, *app.App, *mygoui.Tester, string) {
	t.Helper()
	s, a, tt, path := sideShell(t)
	a.SetFavorites(app.FavoritesCreated, testFavorites("我建的 ", created))
	a.SetFavorites(app.FavoritesCollected, testFavorites("我收的 ", collected))
	tt.Frame()
	return s, a, tt, path
}

// testFavorites 造 n 条条目，标题是 prefix 加序号。
func testFavorites(prefix string, n int) []app.Favorite {
	out := make([]app.Favorite, n)
	for i := range out {
		out[i] = app.Favorite{
			ID:    int64(i + 1),
			Title: prefix + strconv.Itoa(i+1),
			Mid:   42,
			Type:  2,
		}
	}
	return out
}

// TestBothCollectionGroupsFold 验两组的折叠是各按各的。
//
// 两组折叠态存在设置的同一个结构体里（`{created, collected}`），写错格子的
// 表现是「折了一组、另一组的收藏夹凭空不见了」，而且界面上看不出是谁的问题。
func TestBothCollectionGroupsFold(t *testing.T) {
	_, a, tt, _ := favShell(t, 3, 2)

	if err := tt.Click("▾ 我创建的"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	if _, ok := tt.Find("▸ 我创建的"); !ok {
		t.Errorf("第一组没有折起来，文字有 %q", tt.Texts())
	}
	if _, ok := tt.Find("▾ 我收藏的"); !ok {
		t.Errorf("第二组跟着折了，文字有 %q", tt.Texts())
	}
	if !tt.HasText("我收的 2") {
		t.Errorf("第二组的收藏夹不见了，文字有 %q", tt.Texts())
	}

	folded := a.SettingsSnapshot().SideMenuCollectionFolded
	if !folded.Created || folded.Collected {
		t.Errorf("折叠状态存成了 %+v，want 只有 Created", folded)
	}

	// 展开回来。
	if err := tt.Click("▸ 我创建的"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("▾ 我创建的"); !ok {
		t.Errorf("没有展开回来，文字有 %q", tt.Texts())
	}
	if a.SettingsSnapshot().SideMenuCollectionFolded.Created {
		t.Error("展开之后设置里还是折叠着")
	}
}

// TestTheContextMenuHasBiuNumberOfItems 验右键菜单的项数和 Biu 一致：
// 我创建的 5 项、我收藏的 4 项。
//
// ⚠️ 用的是**系统原生菜单**（`Element.ContextMenu`），不是自绘浮层：验收
// 要的是「系统原生样式、方向键能导航」，只有系统菜单免费给这两样。
func TestTheContextMenuHasBiuNumberOfItems(t *testing.T) {
	_, _, tt, _ := favShell(t, 2, 2)

	want := []struct {
		row    string
		labels []string
	}{
		{"我建的 1", []string{"播放", "添加到播放列表", "修改", "隐藏", "删除"}},
		{"我收的 1", []string{"播放", "添加到播放列表", "隐藏", "取消收藏"}},
	}

	for _, w := range want {
		if err := tt.RightClick(w.row); err != nil {
			t.Fatalf("「%s」这一行点不到右键：%v", w.row, err)
		}
		if got := tt.Menu(); !slices.Equal(got, w.labels) {
			t.Errorf("「%s」的右键菜单是 %q，want %q", w.row, got, w.labels)
		}
		tt.CloseMenu()
		tt.Frame()
	}
}

// TestHidingAFavoriteTakesItOffTheSidebar 验「隐藏」这一项是真的。
//
// 其余几项都要 B 站接口，「隐藏」不用——Biu 就是把 ID 写进 `hiddenMenuKeys`
// （`collection/index.tsx:232-244`），那一格就在我们的设置里。
func TestHidingAFavoriteTakesItOffTheSidebar(t *testing.T) {
	_, a, tt, _ := favShell(t, 3, 0)

	if err := tt.RightClick("我建的 2"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("隐藏"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	// 认的是**侧栏那一列**，不是整个画面：提示里写着「已把「我建的 2」
	// 从侧栏藏起来」，按整个画面找会一直找到那句提示。
	if _, ok := tt.Find("我建的 2"); ok {
		t.Errorf("藏起来了还摆在侧栏里，文字有 %q", tt.Texts())
	}
	if !tt.HasText("我建的 1") || !tt.HasText("我建的 3") {
		t.Errorf("连别的收藏夹也一起藏了，文字有 %q", tt.Texts())
	}
	if got := a.SettingsSnapshot().HiddenMenuKeys; !slices.Equal(got, []string{"2"}) {
		t.Errorf("hiddenMenuKeys 是 %v，want [2]", got)
	}
}

// TestDraggingAFavoriteReordersItAndKeepsItAfterARestart 是这一张最核心的
// 验收：「收藏夹能拖拽排序，顺序落盘，重启还在」。
//
// 走真文件：种几条 → 在界面上拖着换位置 → 真的落盘 → 关掉重开 → 重新喂
// 同一批条目（接口每次都这么给）→ 顺序还在。
func TestDraggingAFavoriteReordersItAndKeepsItAfterARestart(t *testing.T) {
	s, a, tt, path := favShell(t, 4, 0)

	from, ok := tt.Find("我建的 1")
	if !ok {
		t.Fatalf("侧栏上没有第一条收藏夹，文字有 %q", tt.Texts())
	}
	to, ok := tt.Find("我建的 3")
	if !ok {
		t.Fatalf("侧栏上没有第三条收藏夹，文字有 %q", tt.Texts())
	}
	// 落在第三条那一行的上半边（MyGo 的 dropAt：落在某行的上半边 = 插到
	// 那一行**前面**，指示线就画在它的上边缘）。所以 1 号要停在「我建的 3」
	// 上方：得到 我建的 2、我建的 1、我建的 3、我建的 4。
	dragFrom(tt, from.X+from.W/2, from.Y+from.H/2, to.X+to.W/2, to.Y+to.H*0.25)

	want := []string{"我建的 2", "我建的 1", "我建的 3", "我建的 4"}
	if got := favTitles(a, app.FavoritesCreated); !slices.Equal(got, want) {
		t.Fatalf("拖完侧栏上是 %q，want %q", got, want)
	}

	// 铁律四：行 key 用的是收藏夹的 ID，不是序号。序号是「父 ID + 兄弟
	// 序号」那一套，顺序一变每一行的状态就串到别的收藏夹上——实测的
	// 症状就是「正在操作的那一首变成了旁边另一首」。
	keys := s.sideLists[app.FavoritesCreated].Key
	for i, wantID := range []int64{2, 1, 3, 4} {
		if got := keys(i); got != wantID {
			t.Errorf("第 %d 行的 key 是 %v，want %v（收藏夹的 ID，不是序号）", i, got, wantID)
		}
	}

	if err := a.FlushSettings(); err != nil {
		t.Fatal(err)
	}
	if err := a.CloseSettings(); err != nil {
		t.Fatal(err)
	}

	_, a2, tt2, _ := sideShellOn(t, path)
	a2.SetFavorites(app.FavoritesCreated, testFavorites("我建的 ", 4))
	tt2.Frame()

	if got := favTitles(a2, app.FavoritesCreated); !slices.Equal(got, want) {
		t.Errorf("重开之后侧栏上是 %q，顺序没存住", got)
	}
}

// TestDroppingOnARowPutsTheDraggedOneRightAboveIt 钉住**落点和结果**对得上。
//
// MyGo 给的落点是「插到第 to 行前面」，屏幕上那条指示线就画在第 to 行的
// **上边缘**（`mygo/ui/list.go:727` 的 dropAt、`list.go:744` 的 boundary）。
// 所以验法只有一句：拖走的那一条，最后必须**紧挨着落在那一行的上面**。
//
// 这条是拖拽排序的验收本体。之前的写法把「挪到某行的位置」当成了「插到某行
// 的前面」，往下拖的时候落点比指示线低一格，而错的那条顺序还会被
// ReorderFavorite 立刻写进设置文件——重开还是错的。
func TestDroppingOnARowPutsTheDraggedOneRightAboveIt(t *testing.T) {
	const n = 4

	for to := 1; to < n; to++ {
		t.Run(fmt.Sprintf("落在第 %d 行的上边缘", to+1), func(t *testing.T) {
			_, a, tt, _ := favShell(t, n, 0)

			from, ok := tt.Find("我建的 1")
			if !ok {
				t.Fatalf("侧栏上没有第一条，文字有 %q", tt.Texts())
			}
			row, ok := tt.Find(fmt.Sprintf("我建的 %d", to+1))
			if !ok {
				t.Fatalf("侧栏上没有第 %d 条，文字有 %q", to+1, tt.Texts())
			}
			dragFrom(tt, from.X+from.W/2, from.Y+from.H/2,
				row.X+row.W/2, row.Y+row.H*0.25)

			assertDroppedJustAbove(t, a, to, fmt.Sprintf("我建的 %d", to+1))
		})
	}

	t.Run("落在最后一条下面 = 排到最后面", func(t *testing.T) {
		_, a, tt, _ := favShell(t, n, 0)

		from, ok := tt.Find("我建的 1")
		if !ok {
			t.Fatalf("侧栏上没有第一条，文字有 %q", tt.Texts())
		}
		last, ok := tt.Find(fmt.Sprintf("我建的 %d", n))
		if !ok {
			t.Fatalf("侧栏上没有第 %d 条，文字有 %q", n, tt.Texts())
		}
		// 落在最后一条的**下半边**：MyGo 的落点这时是「列表的行数」。
		dragFrom(tt, from.X+from.W/2, from.Y+from.H/2,
			last.X+last.W/2, last.Y+last.H*0.9)

		got := favTitles(a, app.FavoritesCreated)
		if got[n-1] != "我建的 1" {
			t.Errorf("拖到最后面之后是 %q，最后一条不是「我建的 1」", got)
		}
	})
}

// assertDroppedJustAbove 断言「我建的 1」现在正好压在第 to 行那条的上面。
func assertDroppedJustAbove(t *testing.T, a *app.App, to int, target string) {
	t.Helper()
	got := favTitles(a, app.FavoritesCreated)
	if got[to] != target {
		t.Errorf("落点是「插到 %q 前面」，那一行应该在第 %d 位，实际在第 %d 位：%q",
			target, to, indexOf(got, target), got)
	}
	if got[to-1] != "我建的 1" {
		t.Errorf("落点是「插到 %q 前面」，它上面一条应该是被拖的那条，实际是 %q：%q",
			target, got[to-1], got)
	}
}

func indexOf(list []string, want string) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return -1
}

// favTitles 是一组当前摆出来的标题。
func favTitles(a *app.App, g app.FavoriteGroup) []string {
	items := a.SideFavorites(g)
	out := make([]string, len(items))
	for i, f := range items {
		out[i] = f.Title
	}
	return out
}

// TestDragReorderIsOffWhenTheGroupIsFolded 验折叠时拖不动（Biu 的 SD-09）。
//
// 折叠时那一列根本没建，拖不动是自然发生的——这条测试盯的是「别因为折叠而
// 漏了一帧又把它建出来」，所以除了断言画面上没有了，还要在折叠标题上真的
// 拖一次，再断言**顺序一个字节都没动**（`Reorder` 没被调用、设置里也没多出
// 任何东西）。
//
// ⚠️ 认标题要认对：fixture 的标题是「我建的 1/2/3」（`favShell` 里带前缀），
// 之前这里找的是「收藏 1」——那个串画面上从来没有过，所以折不折叠都通过，
// 整条测试等于空跑，SD-09 一条证据也没有。
func TestDragReorderIsOffWhenTheGroupIsFolded(t *testing.T) {
	_, a, tt, _ := favShell(t, 3, 0)

	// 起点：这一列确实在，而且标题是能找着的。
	if _, ok := tt.Find("我建的 1"); !ok {
		t.Fatalf("起点就没有「我建的 1」，这条测试什么也证明不了，文字有 %q", tt.Texts())
	}
	before := favTitles(a, app.FavoritesCreated)

	if err := tt.Click("▾ 我创建的"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("我建的 1"); ok {
		t.Fatalf("折叠之后收藏夹还摆着，文字有 %q", tt.Texts())
	}

	// 在折叠标题上拖一次：不该有任何反应，也不该有顺序变化。
	head, ok := tt.Find("▸ 我创建的")
	if !ok {
		t.Fatalf("画面上找不到折叠标题，文字有 %q", tt.Texts())
	}
	dragFrom(tt, head.X+head.W/2, head.Y+head.H/2, head.X, head.Y+120)

	if got := favTitles(a, app.FavoritesCreated); !slices.Equal(got, before) {
		t.Errorf("折叠时拖了一下，顺序从 %q 变成了 %q", before, got)
	}
	if got := a.SettingsSnapshot().SideMenuCollectionOrder.Created; len(got) != 0 {
		t.Errorf("折叠时拖了一下，设置里多出了一条顺序：%v", got)
	}

	// 展开回来顺序还在——证明刚才不是「碰巧一样」。
	if err := tt.Click("▸ 我创建的"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("我建的 1"); !ok {
		t.Fatalf("展开之后收藏夹不见了，文字有 %q", tt.Texts())
	}
	if got := favTitles(a, app.FavoritesCreated); !slices.Equal(got, before) {
		t.Errorf("展开之后顺序是 %q，want %q", got, before)
	}
}

// TestTheRowKeysAreUniqueAcrossTheTwoGroups 验两组的行 key 不撞。
//
// 两组完全可能有同一个收藏夹（B 站那边自己收藏的和自己创建的可以是同一个），
// 只给 ID 的话 Tester 里撞 key 直接 panic（`ui/diagnostics.go`）。
func TestTheRowKeysAreUniqueAcrossTheTwoGroups(t *testing.T) {
	a := favRowKey(app.FavoritesCreated, 7)
	b := favRowKey(app.FavoritesCollected, 7)
	if a == b {
		t.Errorf("两组的行 key 都是 %q", a)
	}
}

// TestTheSidebarShowsTheAppNameExactlyOnce 钉住「侧栏上只有一个应用名」。
//
// 2026-10 应用从 `Biu MyGo` 改名成 `BiuGo` 时，折叠按钮上方那行 11pt 的小字跟着
// 改了，**顶上那条 18pt 的品牌字没跟着改**——展开的侧栏上方一个「Biu」、下方一个
// 「BiuGo」，同一栏里两个产品名。两行都是正常的 Text，所以**没有任何一处会报错**，
// 只有人看得出不对劲。
//
// 修法是把应用名挪到顶上那一处、删掉下面的小字（`viewSideBrand`）。这一条钉住
// 修完的样子：整个界面上应用名只出现一次，而旧的「Biu」整行不许再回来。
func TestTheSidebarShowsTheAppNameExactlyOnce(t *testing.T) {
	_, _, tt, _ := sideShell(t)

	var names []string
	for _, s := range tt.Texts() {
		if s == "Biu" {
			t.Errorf("界面上还有一行整字「Biu」——那是被改名前的旧品牌字：%q", tt.Texts())
		}
		if strings.Contains(s, "BiuGo") {
			names = append(names, s)
		}
	}
	if len(names) != 1 {
		t.Errorf("界面上有 %d 处应用名（%q），want 1 处", len(names), names)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 折叠态的悬停提示、收藏夹名的截断
// ─────────────────────────────────────────────────────────────────────────────

// collapsedShell 造一个**折叠着**的侧栏。
//
// ⚠️ 折叠后的宽度过渡是 200ms 的动画，无头 Tester 一帧最多 settle 20 帧，
// 走不完——所以开了 ReduceMotion，动画直接落到终点（理由同 `sideShell`）。
func collapsedShell(t *testing.T) (*Shell, *app.App, *mygoui.Tester) {
	t.Helper()
	st := app.DefaultSettings("")
	st.SideMenuCollapsed = true
	s, tt := newShell(t, withSettings(st),
		withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}))
	tt.Frame()
	return s, s.app, tt
}

// TestTheCollapsedSidebarItemSaysItsName 钉住验收第 1 条：
// 折叠后每一项在悬停时给出名称提示。
//
// 折叠态那一格只有一个字（上游摆的是图标，而那一档还没换），没有字可以让用户
// 对照着认——这一格完全靠记忆。
//
// ⚠️ **悬停提示要指针停够 0.6 秒才出**（`mygo/ui/tooltip.go` 的 tooltipDelay），
// 所以是 Move + Advance + Frame，不是 Move + Frame。
//
// ⚠️ **认的是「下载记录」而不是「推荐音乐」**：后者是当前那一页的**路由容器**
// 的无障碍名字（`mygo/ui/router.go` 给路由容器 `Label(e.title)`），它本来就
// 在画面上——拿它当判据的话，这条测试折不折叠都通过，等于空跑。
func TestTheCollapsedSidebarItemSaysItsName(t *testing.T) {
	_, _, tt := collapsedShell(t)

	// 折叠态那一列摆的是每个标题的第一个字。
	r, ok := tt.Find("下")
	if !ok {
		t.Fatalf("折叠态的侧栏上没有「下载记录」那一个字，文字有 %q", tt.Texts())
	}
	if tt.HasText("下载记录") {
		t.Fatalf("还没悬停就把名字说出来了，文字有 %q", tt.Texts())
	}

	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(700 * time.Millisecond)
	tt.Frame()

	if !tt.HasText("下载记录") {
		t.Errorf("悬停的时候没有说出这一项是什么，文字有 %q", tt.Texts())
	}
}

// TestTheCollapsedSidebarTipSitsToTheRightOfTheItem 钉住提示的**位置**。
//
// 上游折叠态那一项的提示是 `placement="right"`
// （`biu/src/components/menu/menu-item.tsx:98-99`）——贴在那一项的右边。
// 摆在上面的话提示会盖住上面那几项（侧栏只有 72 宽，摆不下别的东西），
// 而摆在左边会被窗口边钳住。
func TestTheCollapsedSidebarTipSitsToTheRightOfTheItem(t *testing.T) {
	_, _, tt := collapsedShell(t)

	r, ok := tt.Find("下")
	if !ok {
		t.Fatalf("折叠态的侧栏上没有「下载记录」那一个字，文字有 %q", tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(700 * time.Millisecond)
	tt.Frame()

	tip, ok := tt.Find("下载记录")
	if !ok {
		t.Fatalf("悬停的时候没有说出这一项是什么，文字有 %q", tt.Texts())
	}
	// 侧栏折叠后是 72 宽（`app.CollapsedSideMenuWidth`），提示得在它右边。
	if tip.X < app.CollapsedSideMenuWidth {
		t.Errorf("提示摆在侧栏里面（x=%.1f < %.0f），文字有 %q",
			tip.X, app.CollapsedSideMenuWidth, tt.Texts())
	}
}

// TestALongFavoriteNameDoesNotMakeTheRowTaller 钉住验收第 2 条：
// 长收藏夹名**不换行**（断高度不变）。
//
// 收藏夹名一长（比如「【长期主义】2026 年度歌单合集·最终版」），侧栏那一行
// 会长高一截，把下面的收藏夹整体顶下去，侧栏跟着抖。侧栏只有 160~480 宽，
// 超过约 20 个汉字就换行。
//
// ⚠️ **断的是高度，不是「有没有省略号」**：画面上那行字被截断了没有，画面上
// 看不出来（省略号是画出来的，而截断的字和没截断的字**一样高**）——
// 能分出来的只有「这一行有没有变高」。
func TestALongFavoriteNameDoesNotMakeTheRowTaller(t *testing.T) {
	_, a, tt, _ := favShell(t, 0, 0)

	const longName = "【长期主义】2026 年度歌单合集·最终版"
	a.SetFavorites(app.FavoritesCreated, []app.Favorite{
		{ID: 1, Title: "短名字", Mid: 42, Type: 2},
		{ID: 2, Title: longName, Mid: 42, Type: 2},
		{ID: 3, Title: "第三首", Mid: 42, Type: 2},
	})
	tt.Frame()

	first, ok := tt.Find("短名字")
	if !ok {
		t.Fatalf("侧栏上没有第一条收藏夹，文字有 %q", tt.Texts())
	}
	middle, ok := tt.Find(longName)
	if !ok {
		t.Fatalf("侧栏上没有那条长名字的收藏夹，文字有 %q", tt.Texts())
	}
	last, ok := tt.Find("第三首")
	if !ok {
		t.Fatalf("侧栏上没有第三条收藏夹，文字有 %q", tt.Texts())
	}

	// ① 长名字那一行**一样高**：换行的话它会是两行。
	if middle.H != first.H {
		t.Errorf("长名字那一行是 %v 高，短的是 %v 高——它换行了",
			middle.H, first.H)
	}
	// ② 它**没有把下面那一行顶下去**：两条间距一样。
	gapBefore := middle.Y - first.Y
	gapAfter := last.Y - middle.Y
	if gapBefore != gapAfter {
		t.Errorf("长名字那一行前后两条的间距是 %v 和 %v——它把下面的顶下去了",
			gapBefore, gapAfter)
	}
}

// TestTheSidebarPanicIsCaught 验侧栏画崩了，**程序不崩**，而且切到错误页。
//
// ⚠️ 这条证明不了「是侧栏自己那层 Guard 接住的」。`New()` 里 viewMain 外面
// 已经包了一层 `Guard`（`shell.go`），侧栏在里面，外层照样接得住——把
// viewMain 里那句 `Guard(s.sideBody, s.onPanic)(c)` 换成裸的 `s.sideBody(c)`，
// 这条照样绿。所以它证明的是**用户看得见的行为**（崩了不死、错误页、有记录），
// 不是哪一层接的。侧栏那层 Guard 是任务要求的写法，靠 code review 守着。
//
// 放在这里的理由：侧栏在外层布局里，它一空就是整个窗口空一大块，比某一页崩了
// 更显眼，所以要单独有一条盯着它崩了之后的样子。
func TestTheSidebarPanicIsCaught(t *testing.T) {
	s, _, tt, _ := sideShell(t)

	if !tt.HasText("BiuGo") {
		t.Fatalf("侧栏没画出来，文字有 %q", tt.Texts())
	}
	s.sideBody = func(*mygoui.Context) { panic("侧栏画崩了") }
	tt.Frame()

	if s.crash == nil {
		t.Fatal("侧栏 panic 了，外壳没有记住")
	}
	if !strings.Contains(errString(s.crash), "侧栏画崩了") {
		t.Errorf("记住的 panic 是 %v", s.crash)
	}
	if !tt.HasText("界面出错了") {
		t.Errorf("没有切到错误页，文字有 %q", tt.Texts())
	}
	if !tt.HasText("侧栏画崩了") {
		t.Errorf("错误页上没有崩掉的原因，文字有 %q", tt.Texts())
	}
}
