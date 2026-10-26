// 应用菜单：macOS 窗口左上角那排菜单（票 #539）。
//
// 这一族断的是**菜单本身**：有哪些项、顺序对不对、点了有没有反应。
//
// ⚠️ **判据一律是「菜单树里有什么」和「点了之后状态变成什么」**，不是
// 「屏幕上画出了什么」——菜单是系统画的（AppKit 的 NSMenu），它不在窗口的
// 画面里，截不到也量不到（见 `internal/ui/menu.go` 顶上那一段）。
//
// ⚠️ **角色项的文案不断言**：那些文案是框架按**应用此刻显示的语言**贴的，
// 而语言要等事件循环起来之后才定下来（`startLocale` → `relabelMenus`）——
// 无头 Tester 里没有那个事件循环，所以在这儿量到的可能是英文。
// 角色项断的是**角色**和**顺序**，我们自己那两项断的是文案（它们是写死的）。
// 真机上那一份是中文的（见收尾说明）。
package ui

import (
	"testing"

	"github.com/egoist/mygo"
)

// TestTheAppMenuColumnFollowsTheMacOrder 断应用菜单那一栏的项和顺序。
//
// 顺序是 macOS 的惯例：关于 → 设置 → 隐藏三件 → 退出。用户按 ⌘, 的手感和
// 别的应用一样，靠的就是这个顺序（这一票的用户原话：「点击的时候能进到
// 关于、进到设置」）。
func TestTheAppMenuColumnFollowsTheMacOrder(t *testing.T) {
	m := appMenu(func() {}, func() {})
	cols := m.Items()
	if len(cols) == 0 {
		t.Fatal("菜单里一栏都没有")
	}
	col := cols[0]
	if col.Role != mygo.RoleAppMenu {
		t.Fatalf("第一栏不是应用菜单，是 %q——macOS 上左上角第一个菜单永远写着应用名", col.Role)
	}

	var got []string
	for _, it := range col.Submenu {
		switch {
		case it.Type == mygo.MenuItemSeparator:
			got = append(got, "---")
		case it.Role != "":
			got = append(got, string(it.Role))
		default:
			got = append(got, it.Label)
		}
	}
	want := []string{
		"关于 布丁音乐", "---", "设置…", "---",
		string(mygo.RoleHide), string(mygo.RoleHideOthers), string(mygo.RoleUnhide), "---",
		string(mygo.RoleQuit),
	}
	if len(got) != len(want) {
		t.Fatalf("应用菜单那一栏有 %d 项（%v），want %d 项（%v）", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("应用菜单第 %d 项是 %q，want %q（整栏：%v）", i+1, got[i], want[i], got)
		}
	}
}

// TestTheAboutItemIsOurOwn 断「关于」是**我们自己的**那一项。
//
// ⚠️ **不能用 `RoleAbout`**：那个角色在 macOS 上走系统的
// `orderFrontStandardAboutPanel:`，弹出来的是系统那个关于面板——它没有
// 仓库链接，也没有第三方许可的入口（见 internal/ui/menu.go 的注释）。
func TestTheAboutItemIsOurOwn(t *testing.T) {
	m := appMenu(func() {}, func() {})
	it := m.ItemByID(menuAboutID)
	if it == nil {
		t.Fatal("菜单里没有「关于」那一项")
	}
	if it.Role == mygo.RoleAbout {
		t.Error("「关于」用的是 RoleAbout：那会弹系统那个面板，没有仓库链接也没有第三方许可")
	}
	if it.Click == nil {
		t.Error("「关于」没有 Click：点了什么也不会发生")
	}
	if it.Label != "关于 "+appName {
		t.Errorf("「关于」那一项写的是 %q，want %q", it.Label, "关于 "+appName)
	}
}

// TestTheSettingsItemHasTheMacShortcut 断「设置…」带 ⌘,。
//
// ⌘, 是 macOS 的惯例（系统设置、偏好设置都是它）。写法是 `CmdOrCtrl+,`——
// macOS 上是 ⌘,，别处是 Ctrl,+。
func TestTheSettingsItemHasTheMacShortcut(t *testing.T) {
	m := appMenu(func() {}, func() {})
	it := m.ItemByID(menuSettingsID)
	if it == nil {
		t.Fatal("菜单里没有「设置…」那一项")
	}
	if it.Label != "设置…" {
		t.Errorf("「设置…」那一项写的是 %q，want %q（省略号是 macOS 的写法）", it.Label, "设置…")
	}
	if it.Accelerator != "CmdOrCtrl+," {
		t.Errorf("「设置…」的快捷键是 %q，want %q", it.Accelerator, "CmdOrCtrl+,")
	}
	if it.Click == nil {
		t.Error("「设置…」没有 Click：点了什么也不会发生")
	}
}

// TestTheSettingsMenuItemGoesToTheMainWindowEvenWithTheMiniInFront 断
// 「设置…」走的是**主窗口**。
//
// ⚠️ **菜单是应用级的**（`SetMenu` 设在 `mygo.App` 上），迷你播放器那一扇
// 和主窗口共用同一列菜单。迷你开着的时候主窗口是收起来的（两个窗口互斥，
// 见 miniplayer.go）——于是这一项得先把迷你让开，再在主窗口上换路由。
//
// ⚠️ **回调拿到的 `win` 在这里传 nil**（最狠的一种）：菜单是应用级的，
// 真实情况下那个 `win` **可能是迷你那一扇**，而一个会去用它的实现当场就崩。
func TestTheSettingsMenuItemGoesToTheMainWindowEvenWithTheMiniInFront(t *testing.T) {
	b := newTestShell(t)
	main := b.s
	mini := main.newMiniShell()
	t.Cleanup(mini.releaseMini)

	m := appMenu(main.openAbout, main.openSettingsFromMenu)
	it := m.ItemByID(menuSettingsID)
	if it == nil {
		t.Fatal("菜单里没有「设置…」那一项")
	}
	it.Click(it, nil)

	if main.mini != nil {
		t.Error("迷你播放器还在前面：主窗口是收着的，设置页开在收起来的窗口上等于没开")
	}
	if got := main.router.Path(); got != settingsPath {
		t.Errorf("主窗口停在 %q，want %q", got, settingsPath)
	}
	if got := mini.router.Path(); got == settingsPath {
		t.Error("迷你那一扇自己去开设置页了：设置页是主窗口的那一页")
	}
}

// TestTheOtherColumnsAreStillThere 断编辑 / 显示 / 窗口那几栏没丢。
//
// ⚠️ **设了自己的菜单，默认菜单是被整个换掉的，不是合并**（框架的
// `handleReady`：`if !a.menuSet { a.menu = def }`）——所以那几栏得自己写回来。
// 少了的话，⌘C / ⌘V / ⌘W / ⌘Q 就都没了。
func TestTheOtherColumnsAreStillThere(t *testing.T) {
	m := appMenu(func() {}, func() {})
	roles := menuRoles(m)
	for _, r := range []mygo.MenuRole{
		mygo.RoleQuit,  // ⌘Q
		mygo.RoleClose, // ⌘W
		mygo.RoleCopy,  // ⌘C
		mygo.RolePaste, // ⌘V
		mygo.RoleCut,   // ⌘X
		mygo.RoleUndo,  // ⌘Z
		mygo.RoleSelectAll,
		mygo.RoleMinimize,
		mygo.RoleZoom,
		mygo.RoleToggleFullScreen,
		mygo.RoleToggleDevTools,
	} {
		if !roles[r] {
			t.Errorf("菜单里没有 %q 了：⌘C / ⌘V / ⌘W / ⌘Q 这些就没了", r)
		}
	}
}

// TestTheMenuHasFiveColumns 断菜单还是那五栏。
//
// 顺序也是惯例：应用 / 文件 / 编辑 / 显示 / 窗口。
func TestTheMenuHasFiveColumns(t *testing.T) {
	m := appMenu(func() {}, func() {})
	var got []mygo.MenuRole
	for _, col := range m.Items() {
		got = append(got, col.Role)
	}
	want := []mygo.MenuRole{
		mygo.RoleAppMenu, mygo.RoleFileMenu, mygo.RoleEditMenu,
		mygo.RoleViewMenu, mygo.RoleWindowMenu,
	}
	if len(got) != len(want) {
		t.Fatalf("菜单有 %d 栏（%v），want %d 栏（%v）", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 栏是 %q，want %q", i+1, got[i], want[i])
		}
	}
}

// TestTheAppIsWrittenInChinese 断「我们告诉框架这个应用是简体中文写的」。
//
// ⚠️ **断的是这句话本身，不是菜单上那几行字**：菜单上那些角色项的文案是
// 框架按**应用的语言**贴的，而语言要等事件循环起来之后才定下来
// （`startLocale` → `relabelMenus`）——无头 Tester 里没有那个事件循环，
// 所以菜单上的文案在这儿量不到（真机上量到了，是中文的，见收尾说明）。
//
// 而这一句是**因**：不声明语言，框架就以为这个应用是英语的，菜单全是英文
// （"Hide 布丁音乐" / "Quit 布丁音乐"）。所以断它就够了。
//
// ⚠️ **这里只调 `installLanguage`（声明），不调 `resolveLanguageNow`**：后者
// 要在**主线程**上算语言（`App.Locale()` 走 `onMainValue`），而测试跑在
// 别的 goroutine 上——那一调会**一直等下去**（无头环境里没有事件循环），
// 测试会挂到超时。
func TestTheAppIsWrittenInChinese(t *testing.T) {
	installLanguage()
	langs := mygo.App.Languages()
	if len(langs) == 0 || langs[0] != "zh-Hans" {
		t.Errorf("应用的语言是 %v，want 第一个是 zh-Hans", langs)
	}
}
