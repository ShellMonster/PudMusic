package ui

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	mygoui "github.com/egoist/mygo/ui"
)

// 这一组跑的是 MyGo 的 Tester：和真窗口一模一样的 layout、拖动和系统菜单，
// 只是画到内存里。所以「拖窄了会自动折叠」「右键菜单有几项」这些都能自动验，
// 不需要人守在窗口前面拖。

// sideShell 造一个挂了真设置文件的 Sidebar 场景，返回设置文件路径。
//
// 为什么不用 newShell：那个不挂设置，写进去的每一笔宽度和折叠态都会被
// `ErrSettingsNotMounted` 挡掉——这一组要验的正好是「存没存住」。
func sideShell(t *testing.T, opts ...shellOpt) (*Shell, *app.App, *mygoui.Tester, string) {
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
	b := newTestShell(t, append([]shellOpt{
		onApp(a),
		withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}),
	}, opts...)...)
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

// ─────────────────────────────────────────────────────────────────────────────
// 拖宽、拖窄、折叠
// ─────────────────────────────────────────────────────────────────────────────

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
func favShell(t *testing.T, created, collected int, opts ...shellOpt) (*Shell, *app.App, *mygoui.Tester, string) {
	t.Helper()
	s, a, tt, path := sideShell(t, opts...)
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
// **上边缘**（`mygo/ui/list.go:863` 的 dropAt、`list.go:886` 的 boundary）。
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
		if s == "布丁音乐" {
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

// TestTheCollapsedSidebarItemSaysItsName 钉住验收第 3 条的后半句：
// 折叠后每一项在悬停时给出名称提示。
//
// 折叠态那一格只有一枚图标（上游摆的也是图标），没有字可以让用户对照着认——
// 这一格完全靠记忆。
//
// ⚠️ **悬停提示要指针停够 0.6 秒才出**（`mygo/ui/tooltip.go` 的 tooltipDelay），
// 所以是 Move + Advance + Frame，不是 Move + Frame。
//
// ⚠️ **断的是「多了一个」而不是「有没有」**：折叠态那一项的**无障碍名字**就是
// 「下载记录」（见 `TestTheCollapsedSidebarItemHasItsFullName`），而提示是
// 浮层里的**另一个**元素——所以判据是**同一个字出现了两次**（名字 + 提示），
// 不是「画面上有这句话」。
func TestTheCollapsedSidebarItemSaysItsName(t *testing.T) {
	_, _, tt := collapsedShell(t)

	// 折叠态那一列摆的是图标，没有字——找的是那一项的**无障碍名字**。
	r, ok := tt.Find("下载记录")
	if !ok {
		t.Fatalf("折叠态的侧栏上没有「下载记录」那一格，文字有 %q", tt.Texts())
	}
	before := countText(tt, "下载记录")
	if before != 1 {
		t.Fatalf("悬停之前「下载记录」出现了 %d 次，want 1（那一项的无障碍名字），文字有 %q",
			before, tt.Texts())
	}

	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(700 * time.Millisecond)
	tt.Frame()

	if got := countText(tt, "下载记录"); got != before+1 {
		t.Errorf("悬停的时候没有多出一个「下载记录」（提示），出现了 %d 次，want %d，文字有 %q",
			got, before+1, tt.Texts())
	}
}

// TestTheCollapsedSidebarItemHasItsFullName 钉住折叠态那一项的**无障碍名字**。
//
// 折叠态画面上只有一枚图标，读屏软件念出来就是「图标」——这一格等于**完全没有
// 名字**。所以那一项要带一个 `Label`（全名）。
//
// ⚠️ **Label 不在画面上多画一个字**：折叠态画面上本来就没有那个全名（展开态
// 才有，而展开态**不**给 Label——那一档可见的字就是名字，见 `sidebar.go` 的
// 注释）。
func TestTheCollapsedSidebarItemHasItsFullName(t *testing.T) {
	_, _, tt := collapsedShell(t)

	for _, name := range []string{"推荐音乐", "本地音乐", "下载记录", "设置"} {
		if !tt.HasText(name) {
			t.Errorf("折叠态那一项没有全名 %q，文字有 %q", name, tt.Texts())
		}
	}
}

// TestTheCollapsedSidebarTipSitsToTheRightOfTheItem 钉住提示的**位置**。
//
// 上游折叠态那一项的提示是 `placement="right"`
// （`biu/src/components/menu/menu-item.tsx:78`）——贴在那一项的右边。
// 摆在上面的话提示会盖住上面那几项（侧栏只有 72 宽，摆不下别的东西），
// 而摆在左边会被窗口边钳住。
//
// # ⚠️ **为什么断的是像素而不是那个元素的盒子**
//
// 折叠态那一项的**无障碍名字**也是「下载记录」（见
// `TestTheCollapsedSidebarItemHasItsFullName`），而 `Tester.Find` 找的是**第一个**
// 同样的字——那是那一项自己，**不是提示**。所以提示在哪儿只能看**画面**：
// 提示的底色是**反色**（浅色下是深色，深色下是比背景略亮的 Surface），
// 而侧栏那一列的底色是 `t.Background`。
//
// 深浅两套都量：浅色下反色是**深**色（和底色差得开），深色下反色是**略亮**的
// Surface（只差一级），而那一格到底是不是提示，只有量了才知道。
func TestTheCollapsedSidebarTipSitsToTheRightOfTheItem(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			st := app.DefaultSettings("")
			st.SideMenuCollapsed = true
			s, tt := newShell(t, systemIsDark(dark), withSettings(st),
				withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}))
			tt.Frame()
			theme := BuildTheme(s.app.SettingsSnapshot(), dark)

			r, ok := tt.Find("下载记录")
			if !ok {
				t.Fatalf("折叠态的侧栏上没有「下载记录」那一格，文字有 %q", tt.Texts())
			}
			// 上面那一格（「本地音乐」）——提示摆在**上面**的话它会被盖住。
			above, ok := tt.Find("本地音乐")
			if !ok {
				t.Fatalf("折叠态的侧栏上没有「本地音乐」那一格，文字有 %q", tt.Texts())
			}

			tt.Move(r.X+r.W/2, r.Y+r.H/2)
			tt.Advance(700 * time.Millisecond)
			tt.Frame()

			inverse := color.RGBA{R: theme.Inverse.R, G: theme.Inverse.G,
				B: theme.Inverse.B, A: theme.Inverse.A}
			img := tt.Image()
			// ① 侧栏右边那一格是提示的底色 → 提示贴在那一项的右边。
			// x=58：提示从那一格右沿往右 6 处开始（侧栏现在只有 64 宽，提示的头几像素压在侧栏里）。
			at := img.RGBAAt(int(r.X+r.W)+10, int(r.Y+r.H/2))
			if at != inverse {
				t.Errorf("侧栏右边那一格是 %v，want %v（提示的底色）——"+
					"提示没有贴在那一项的右边", at, inverse)
			}
			// ② 上面那一格没有被盖住 → 提示不在上面。
			over := img.RGBAAt(int(above.X+above.W/2), int(above.Y+above.H/2))
			if over == inverse {
				t.Errorf("上面那一格被提示盖住了（%v）——提示摆在上面了", over)
			}
		})
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
	// 虚拟列表的行高要多画几帧才量全（第一帧后面的行还没测，高度是估的 / 0）。
	for range 4 {
		tt.Frame()
	}

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
// 已经包了一层 `Guard`（`shell_crash.go`），侧栏在里面，外层照样接得住——把
// viewMain 里那句 `Guard(s.sideBody, s.onPanic)(c)` 换成裸的 `s.sideBody(c)`，
// 这条照样绿。所以它证明的是**用户看得见的行为**（崩了不死、错误页、有记录），
// 不是哪一层接的。侧栏那层 Guard 是任务要求的写法，靠 code review 守着。
//
// 放在这里的理由：侧栏在外层布局里，它一空就是整个窗口空一大块，比某一页崩了
// 更显眼，所以要单独有一条盯着它崩了之后的样子。
func TestTheSidebarPanicIsCaught(t *testing.T) {
	s, _, tt, _ := sideShell(t)

	if !tt.HasText("布丁音乐") {
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

// ─────────────────────────────────────────────────────────────────────────────
// #164：菜单项的图标、行高，以及左上角那一条品牌
// ─────────────────────────────────────────────────────────────────────────────

// sideInkPixels 报这一块里「有多少像素和众数不一样」——也就是画上去的东西
// 有多少。众数当背景（那一块大部分是底色）。
//
// ⚠️ **不是「有没有非底色像素」而是「有多少个」**：抗锯齿的边缘像素也算，
// 而「图标被换成一个字母」那种改动会让墨迹的**位置**变掉、数量也变掉——
// 数量这一维足够把两种图形分开。
func sideInkPixels(img *image.RGBA, x0, y0, x1, y1 int) int {
	x0, x1 = clampX(img, x0), clampX(img, x1)
	y0, y1 = clampY(img, y0), clampY(img, y1)
	if x1 < x0 || y1 < y0 {
		return 0
	}
	var samples []color.RGBA
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			samples = append(samples, img.RGBAAt(x, y))
		}
	}
	if len(samples) == 0 {
		return 0
	}
	bg := modeOf(samples)
	n := 0
	for _, c := range samples {
		if maxDiff(c, bg) > probeTolerance {
			n++
		}
	}
	return n
}

// sideIconCell 是「菜单项那一枚图标」在画面上占的那一格。
//
// 展开态那一行的结构是「[按钮内边距 8][40×40 的图标格][缝 8][名字]」，而
// 名字那一块的左边就是图标格的右边再往左 8 像素。
//
// ⚠️ **这里量的是「名字左边那一段」而不是「图标本身」**：图标对辅助技术
// 不可见（`mygo/ui/svg.go` 的 Icon 文档），无头 Tester 的 `Find` 只认文字和
// `Label`，图标元素根本不在那张表里——所以只能从画面上量。
func sideMenuIconCell(text mygoui.Rect) (x0, y0, x1, y1 int) {
	return int(text.X) - 52, int(text.Y) - 10, int(text.X) - 4, int(text.Y) + 10
}

// TestEverySideMenuEntryHasAnIcon 钉住验收第一条的前半句：
// 每一个菜单入口**前面都有一枚图标**。
//
// 判据是**画面**：每一项的**名字左边**那一格里必须有墨（图标格在那儿）。
//
// ⚠️ **为什么是「每一项」而不是「上游那六项」**：上游的默认菜单是六项
// （`biu/src/common/constants/menus.tsx`），而我们**多摆了一条「设置」**
// （DESIGN.md §九「侧栏多一条设置」）。七项都要有图标——漏一条，那一项在
// 侧栏里就**一个图标都没有**，而画面上只是「那一项比别人少点东西」。
//
// ⚠️ **登录了才全**：未登录时那三项（我的关注 / 稍后再看 / 历史记录）根本不摆
// （`viewSideMenu` 的抬头），不在这一条的检查范围里。
func TestEverySideMenuEntryHasAnIcon(t *testing.T) {
	s, tt := newShell(t, loggedInWith("7"))
	s.navigate("/")
	tt.Frame()
	tt.Move(600, 400)
	tt.Frame()

	img := tt.Image()
	checked := 0
	for _, rt := range routes {
		if !rt.InMenu {
			continue
		}
		text, ok := tt.Find(rt.Title)
		if !ok {
			t.Errorf("侧栏上没有「%s」那一项，文字有 %q", rt.Title, tt.Texts())
			continue
		}
		checked++
		x0, y0, x1, y1 := sideMenuIconCell(text)
		if n := sideInkPixels(img, x0, y0, x1, y1); n == 0 {
			t.Errorf("「%s」那一项**前面没有图标**（名字左边 %d..%d 那一格是空的）",
				rt.Title, x0, x1)
		}
	}
	if checked != 8 {
		t.Errorf("这一条只查了 %d 项，want 8（原型的八个入口）", checked)
	}
}

// TestTheSideMenuRowsAreFortyHigh 钉住验收第二条：**菜单行高 40**。
//
// 上游是 HeroUI 的 Button **md**：`h-10` = 40。我们原来那一行是
// 6 + 15.3 + 6 ≈ **28**。
//
// 怎么量：选中那一行有一层很淡的底（上游的 `variant="flat"`），沿着侧栏
// 那一列扫下去，那一段连续的「和底色不一样」的像素就是**那一行的盒子**。
// 扫的列取 **x = 120**（按钮的中部）：两头的圆角在 x = 4 那一列上会切掉
// 两三个像素，量出来是 38 而不是 40。
func TestTheSideMenuRowsAreFortyHigh(t *testing.T) {
	s, tt := newShell(t, loggedInWith("7"))
	s.navigate("/")
	tt.Frame()
	tt.Move(600, 400)
	tt.Frame()

	text, ok := tt.Find("推荐音乐")
	if !ok {
		t.Fatalf("侧栏上没有「推荐音乐」那一项，文字有 %q", tt.Texts())
	}

	img := tt.Image()
	const col = 120
	// 那一行在画面上的纵坐标：名字是**居中**在那一行里的，所以行高是
	// 「名字的上下沿再各加一点」——真正的判据是下面扫出来的那一段。
	cy := int(text.Y + text.H/2)

	bg := img.RGBAAt(col, 40) // 侧栏的底色（y=40 在顶栏那一带，不是侧栏）
	// 底色取侧栏里那一列的众数：扫的这一段里大部分是底色。
	var col0 []color.RGBA
	for y := 60; y < 700; y++ {
		col0 = append(col0, img.RGBAAt(col, y))
	}
	bg = modeOf(col0)

	// 从行中心往上、往下找那一段的两个边（**走到第一个还是底色的那一格就停**，
	// 所以 up/down 落在那一段的两个端点上，而不是它外面那一格）。
	up := cy
	for up > 60 && maxDiff(img.RGBAAt(col, up-1), bg) > probeTolerance {
		up--
	}
	down := cy
	for down < 699 && maxDiff(img.RGBAAt(col, down+1), bg) > probeTolerance {
		down++
	}
	got := down - up + 1
	if got != int(tok.NavRowH) {
		t.Errorf("菜单那一行是 %d 高，want %d（上游 HeroUI 的 Button md 是 `h-10`）",
			got, int(tok.NavRowH))
	}
}

// buiMenuIcons 是「哪个入口配哪两枚图标」的**外部依据**：逐条抄自 Biu 的
// `src/common/constants/menus.tsx`，写的是**上游的字面文件名**，不是这里的
// 常量。
//
// ⚠️ 抄的是上游的**文件名**（`@remixicon/react` 引进来的那几个）：
// `RiDiscLine` → `Media/disc-line.svg`、`RiUserFollowLine` →
// `User & Faces/user-follow-line.svg`、`RiCalendarScheduleLine` →
// `Business/calendar-schedule-line.svg`、`RiHistoryLine` →
// `System/history-line.svg`、`RiFolderMusicLine` →
// `Document/folder-music-line.svg`、`RiFileDownloadLine` →
// `Document/file-download-line.svg`。
//
// 「设置」不在这张表里：上游的设置在头像下拉里，**不进侧栏菜单**——是我们
// 自己多摆的一条（DESIGN.md §九）。
var buiMenuIcons = []struct {
	title      string
	line, fill string
}{
	// 2026-11「深夜唱片行」：照原型 `app.js` 的 `NAV`（disc / users / later /
	// history / folder / download），选中项不换实心图形，只把图标染成
	// accent-text（原型 `.nav-item[aria-current] .od-icon`），所以两枚是同一张。
	{"推荐音乐", "Pud/disc.svg", "Pud/disc.svg"},
	{"我的关注", "Pud/users.svg", "Pud/users.svg"},
	{"稍后再看", "Pud/later.svg", "Pud/later.svg"},
	{"历史记录", "Pud/history.svg", "Pud/history.svg"},
	{"本地音乐", "Pud/folder.svg", "Pud/folder.svg"},
	{"下载记录", "Pud/download.svg", "Pud/download.svg"},
}
