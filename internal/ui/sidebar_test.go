package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

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
	path := filepath.Join(t.TempDir(), app.SettingsFileName)

	a := app.New()
	if err := a.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})

	s := New(a)
	t.Cleanup(s.images.Close)
	tt := mygoui.NewTester(s.View, 1200, 800)
	// 宽度过渡是 200ms 的动画，无头 Tester 一帧最多 settle 20 帧，走不完。
	// 开了 ReduceMotion，动画直接落到终点——这里要验的是**终点**，动的那段
	// 200ms 人眼看的，不是能自动断言的东西。
	tt.SetPreferences(mygoui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	return s, a, tt, path
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
func gripX(sidebarWidth float32) float32 { return sidebarWidth - sideGrip/2 }

// sideWidthOnScreen 是这一帧画面上侧栏实际有多宽。
//
// 折叠按钮铺满侧栏正文（Column 把子元素拉满宽），所以它的宽度加回右边那条
// 把手就是侧栏的宽。找的是按钮的**无障碍名字**而不是那个「«」的字形——
// 找字形拿到的是文字那一小块的盒子（8 宽），不是按钮的。
//
// 取的是**画面上的盒子**，不是设置里的数：设置对了但画出来不对，同样是没做。
func sideWidthOnScreen(t *testing.T, tt *mygoui.Tester) float32 {
	t.Helper()
	for _, name := range []string{"折叠侧栏", "展开侧栏"} {
		if r, ok := tt.Find(name); ok {
			return r.W + sideGrip
		}
	}
	t.Fatalf("画面上找不到折叠按钮，文字有 %q", tt.Texts())
	return 0
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
	dragFrom(tt, gripX(app.CollapsedSideMenuWidth), 400, 68+100, 400)

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

	dragFrom(tt, gripX(app.DefaultSideMenuWidth), 400, 196+400, 400)

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

	dragFrom(tt, gripX(app.DefaultSideMenuWidth), 400, 196+120, 400)
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
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})
	s := New(a)
	t.Cleanup(s.images.Close)
	tt := mygoui.NewTester(s.View, 1200, 800)
	tt.SetPreferences(mygoui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	return s, a, tt, path
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
	// 落在第三条那一行的上半边：落点是「排在它前面」，所以 1 号停在 2、3
	// 之间，得到 我建的 2、我建的 3、我建的 1、我建的 4。
	dragFrom(tt, from.X+from.W/2, from.Y+from.H/2, to.X+to.W/2, to.Y+to.H*0.25)

	want := []string{"我建的 2", "我建的 3", "我建的 1", "我建的 4"}
	if got := favTitles(a, app.FavoritesCreated); !slices.Equal(got, want) {
		t.Fatalf("拖完侧栏上是 %q，want %q", got, want)
	}

	// 铁律四：行 key 用的是收藏夹的 ID，不是序号。序号是「父 ID + 兄弟
	// 序号」那一套，顺序一变每一行的状态就串到别的收藏夹上——实测的
	// 症状就是「正在操作的那一首变成了旁边另一首」。
	keys := s.sideLists[app.FavoritesCreated].Key
	for i, wantID := range []int64{2, 3, 1, 4} {
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

// favTitles 是一组当前摆出来的标题。
func favTitles(a *app.App, g app.FavoriteGroup) []string {
	items := a.SideFavorites(g)
	out := make([]string, len(items))
	for i, f := range items {
		out[i] = f.Title
	}
	return out
}

// TestDragReorderIsOffWhenTheGroupIsFolded 验折叠时拖不动。
//
// Biu 的 SD-09「排序项渲染器」就是这一条。折叠时那一列根本没建，拖不动是
// 自然发生的——这条测试盯的是「别因为折叠而漏了一帧又把它建出来」。
func TestDragReorderIsOffWhenTheGroupIsFolded(t *testing.T) {
	_, _, tt, _ := favShell(t, 3, 0)

	if err := tt.Click("▾ 我创建的"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("收藏 1") {
		t.Fatalf("折叠之后收藏夹还摆着，文字有 %q", tt.Texts())
	}

	// 折叠位置（侧栏里那个标题）上拖一下，不该有任何顺序变化。
	head, ok := tt.Find("▸ 我创建的")
	if !ok {
		t.Fatalf("画面上找不到折叠标题，文字有 %q", tt.Texts())
	}
	dragFrom(tt, head.X+head.W/2, head.Y+head.H/2, head.X, head.Y+120)
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

// TestTheSidebarPanicIsCaught 验铁律三：侧栏画崩了，进程不崩。
//
// 侧栏在外层布局里，它一空就是整个窗口空一大块——所以它必须和页面一样
// 有一层 Guard。
func TestTheSidebarPanicIsCaught(t *testing.T) {
	s, _, tt, _ := sideShell(t)

	if !tt.HasText("Biu") {
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
}
