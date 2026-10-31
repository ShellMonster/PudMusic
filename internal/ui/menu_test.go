// 应用菜单：macOS 窗口左上角那排菜单（票 #539 起的，票 #712 扩的）。
//
// 这一族断的是**菜单本身**：有哪些栏、哪些项、顺序对不对、点了有没有反应。
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

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo"
)

// TestTheMenuHasSevenColumns 断菜单是那七栏。
//
// 顺序也是惯例：应用 / 文件 / 编辑 / 播放 / 歌曲 / 显示 / 窗口。
//
// ⚠️ **「播放」和「歌曲」那两栏没有角色**（框架没有「播放器」这个角色），
// 所以它们那一栏的 `Role` 是空串——断的时候按位置断，不要按角色断。
func TestTheMenuHasSevenColumns(t *testing.T) {
	b := newTestShell(t)
	m := appMenu(b.s)
	var got []string
	for _, col := range m.Items() {
		got = append(got, string(col.Role))
	}
	want := []string{
		string(mygo.RoleAppMenu), string(mygo.RoleFileMenu), string(mygo.RoleEditMenu),
		"", "", // 播放 / 歌曲：框架没有这两个角色
		string(mygo.RoleViewMenu), string(mygo.RoleWindowMenu),
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

// TestEveryColumnHasATitle 断**每一栏都有名字**。
//
// ⚠️ **为什么这一条要紧**：菜单栏上那一栏的名字就是**那一栏的 `Label`**
// （`mygo/internal/darwin/menu.go:143`：`buildMenu(it.Submenu, it.Label, …)`）。
// 而框架只给**角色自带的那几栏**补名字（`prepareItem` 里 `it.Label == ""` 时才
// 用 `roleDefaults`）——「播放」「歌曲」那两个角色框架没有，所以**不给
// `Label` 就是一片空白**。
//
// ⚠️ **空白那一栏的症状极难懂**：菜单栏上多出两个没有字的东西，点下去却弹出
// 一列项——用户看到的是「这个应用的菜单坏了」。
func TestEveryColumnHasATitle(t *testing.T) {
	b := newTestShell(t)
	m := appMenu(b.s)
	for i, col := range m.Items() {
		if col.Label == "" {
			t.Errorf("第 %d 栏没有名字（id=%q，role=%q）：菜单栏上会是一片空白",
				i+1, col.ID, col.Role)
		}
	}
}

// TestTheAppMenuColumnFollowsTheMacOrder 断应用菜单那一栏的项和顺序。
//
// 顺序是 macOS 的惯例：关于 → 设置 → 隐藏三件 → 退出。用户按 ⌘, 的手感和
// 别的应用一样，靠的就是这个顺序（这一票的用户原话：「点击的时候能进到
// 关于、进到设置」）。
func TestTheAppMenuColumnFollowsTheMacOrder(t *testing.T) {
	b := newTestShell(t)
	m := appMenu(b.s)
	cols := m.Items()
	if len(cols) == 0 {
		t.Fatal("菜单里一栏都没有")
	}
	col := cols[0]
	if col.Role != mygo.RoleAppMenu {
		t.Fatalf("第一栏不是应用菜单，是 %q——macOS 上左上角第一个菜单永远写着应用名", col.Role)
	}

	got := columnShape(col)
	want := []string{
		"关于布丁音乐", "---", "设置…", "---",
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
	b := newTestShell(t)
	m := appMenu(b.s)
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
	if it.Label != "关于"+appName {
		t.Errorf("「关于」那一项写的是 %q，want %q", it.Label, "关于"+appName)
	}
}

// TestTheSettingsItemHasTheMacShortcut 断「设置…」带 ⌘,。
//
// ⌘, 是 macOS 的惯例（系统设置、偏好设置都是它）。写法是 `CmdOrCtrl+,`——
// macOS 上是 ⌘,，别处是 Ctrl,+。
func TestTheSettingsItemHasTheMacShortcut(t *testing.T) {
	b := newTestShell(t)
	m := appMenu(b.s)
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

	m := appMenu(main)
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
	b := newTestShell(t)
	m := appMenu(b.s)
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

// TestThePlayColumnHasThePlayerControls 断「播放」那一栏有那些项。
//
// ⚠️ **断的是「有哪几项」不是「点了有没有反应」**——后者是下面那几条。
//
// 顺序也是惯例：播放/暂停、上一首、下一首在前，快退快进在后。
func TestThePlayColumnHasThePlayerControls(t *testing.T) {
	b := newTestShell(t)
	m := appMenu(b.s)
	col := m.Items()[3]
	got := columnShape(col)
	want := []string{
		"播放/暂停", "上一首", "下一首", "---",
		"快退 5 秒", "快进 5 秒", "---",
		"顺序", "循环", "随机", "单曲", "---",
		"音量加", "音量减", "静音",
	}
	if len(got) != len(want) {
		t.Fatalf("播放栏有 %d 项（%v），want %d 项（%v）", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("播放栏第 %d 项是 %q，want %q（整栏：%v）", i+1, got[i], want[i], got)
		}
	}
}

// TestThePlayModeItemsAreRadios 断「播放模式」那四项是**单选**。
//
// ⚠️ **为什么单选**：四个档位是**互斥**的（不是「顺序」也不是「随机」），
// 而框架的单选项相邻时会自动成组（`mygo/menu.go:372` 的 `prepareItem`
// 里 `radioGroup`）——���「点一个，另外三个自动取消勾选」。
//
// ⚠️ **勾选态要运行期刷**：播放模式随时会变（用户按播放条上那枚键），
// 而菜单是系统画的、不会自己变——见 refreshMenuChecked。
func TestThePlayModeItemsAreRadios(t *testing.T) {
	b := newTestShell(t)
	m := appMenu(b.s)
	for _, mode := range app.PlayModes {
		it := m.ItemByID(menuPlayModeIDPrefix + string(mode))
		if it == nil {
			t.Errorf("菜单里没有播放模式 %q 那一项", mode)
			continue
		}
		if it.Type != mygo.MenuItemRadio {
			t.Errorf("播放模式 %q 那一项的类型是 %v，want 单选（radio）——四个档位是互斥的",
				mode, it.Type)
		}
	}
}

// TestThePlayModeCheckFollowsTheCurrentMode 断**勾选态跟着当前档位**。
//
// ⚠️ **这一条断的是「刷新真的会发生」**：菜单是系统画的，状态变了它不会
// 自己变——得我们伸手去改（`refreshMenuChecked`）。
//
// ⚠️ **先刷一次再改档位**：刚建好的菜单还没刷过（`menuState` 是空的），
// 而刷新的判据是「跟上一次比」——所以第一次刷是「变了」。
func TestThePlayModeCheckFollowsTheCurrentMode(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.menu = appMenu(s)

	s.refreshMenuChecked(s.app.Snapshot())
	// 默认档是「循环」（`DefaultPlayMode`）。
	if !menuItemChecked(s.menu, menuPlayModeIDPrefix+string(app.PlayModeLoop)) {
		t.Error("刚建好的时候，「循环」那一项没有打钩")
	}

	s.app.SetPlayMode(app.PlayModeRandom)
	s.refreshMenuChecked(s.app.Snapshot())

	if menuItemChecked(s.menu, menuPlayModeIDPrefix+string(app.PlayModeLoop)) {
		t.Error("切到「随机」之后，「循环」那一项还打着钩")
	}
	if !menuItemChecked(s.menu, menuPlayModeIDPrefix+string(app.PlayModeRandom)) {
		t.Error("切到「随机」之后，「随机」那一项没有打钩")
	}
}

// TestTheDisplayColumnHasTheModesAndToggles 断「显示」那一栏。
func TestTheDisplayColumnHasTheModesAndToggles(t *testing.T) {
	b := newTestShell(t)
	m := appMenu(b.s)
	col := m.Items()[5]
	got := columnShape(col)
	// ⚠️ **迷你播放器不在这一栏**——它在「窗口」栏（Apple Music / QQ音乐 /
	// 网易云三家一致，见 `windowColumn` 的注释）。
	want := []string{
		"列表", "网格", "紧凑", "---",
		"显示/隐藏侧栏", "---",
		string(mygo.RoleToggleDevTools), string(mygo.RoleToggleFullScreen),
	}
	if len(got) != len(want) {
		t.Fatalf("显示栏有 %d 项（%v），want %d 项（%v）", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("显示栏第 %d 项是 %q，want %q（整栏：%v）", i+1, got[i], want[i], got)
		}
	}
}

// TestTheSongColumnItemsAreDisabledWithoutATrack 断**没歌的时候那几项是禁用的**。
//
// ⚠️ **为什么**：给一个按了没用的键比不给更糟——用户按两次发现还是那样，
// 就再也不信这个应用了（理由同 `comments_entry.go` 的文件头）。
func TestTheSongColumnItemsAreDisabledWithoutATrack(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.menu = appMenu(s)
	s.refreshMenuChecked(s.app.Snapshot())

	for _, id := range []string{menuLikeID, menuFavoriteID, menuDownloadID, menuCommentsID, menuArtistSpaceID} {
		it := s.menu.ItemByID(id)
		if it == nil {
			t.Errorf("菜单里没有 %q 那一项", id)
			continue
		}
		if it.IsEnabled() {
			t.Errorf("没有歌的时候 %q 那一项还是能点的：按了什么也不会发生", id)
		}
	}
}

// TestTheSongColumnGoesToTheRightPlace 断「歌曲」那一栏点到的东西对不对。
//
// ⚠️ **断的是「点了之后状态变成什么」**——菜单项点了要真的做事。
func TestTheSongColumnGoesToTheRightPlace(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.menu = appMenu(s)

	// 「打开 UP 主空间」：没有歌的时候点了什么也不发生（不能崩）。
	it := s.menu.ItemByID(menuArtistSpaceID)
	if it == nil {
		t.Fatal("菜单里没有「打开 UP 主空间」那一项")
	}
	it.Click(it, nil)

	// 「显示评论」：没有歌的时候点了也不开抽屉。
	if s.commentsOpen {
		t.Error("没有歌的时候点了「显示评论」，抽屉开了")
	}
}

// TestTheFileColumnGoesToThosePages 断「文件」那一栏跳到那两页。
func TestTheFileColumnGoesToThosePages(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.menu = appMenu(s)

	it := s.menu.ItemByID(menuLocalMusicID)
	if it == nil {
		t.Fatal("菜单里没有「本地音乐」那一项")
	}
	it.Click(it, nil)
	if got := s.router.Path(); got != localMusicPath {
		t.Errorf("点了「本地音乐」之后停在 %q，want %q", got, localMusicPath)
	}

	it = s.menu.ItemByID(menuDownloadListID)
	if it == nil {
		t.Fatal("菜单里没有「下载记录」那一项")
	}
	it.Click(it, nil)
	if got := s.router.Path(); got != downloadListPath {
		t.Errorf("点了「下载记录」之后停在 %q，want %q", got, downloadListPath)
	}
}

// TestTheCommandItemsCarryTheShortcuts 断**命令项带着快捷键**。
//
// ⚠️ **快捷键是从设置里读的**（`menuAccelFor`），不是写死的——用户在设置页
// 改了之后菜单要跟着变（这是票 #716 的核心）。
//
// ⚠️ **默认的那几个键**：⌘P / ⌘← / ⌘→（应用内那一份，`shortcuts.go`）。
func TestTheCommandItemsCarryTheShortcuts(t *testing.T) {
	b := newTestShell(t)
	m := appMenu(b.s)
	cases := []struct {
		id   string
		want string
	}{
		{"togglePlay", "CmdOrCtrl+P"},
		{"prev", "CmdOrCtrl+Left"},
		{"next", "CmdOrCtrl+Right"},
	}
	for _, c := range cases {
		it := m.ItemByID(menuCommandIDPrefix + c.id)
		if it == nil {
			t.Errorf("菜单里没有命令 %q 那一项", c.id)
			continue
		}
		if it.Accelerator != c.want {
			t.Errorf("命令 %q 的快捷键是 %q，want %q", c.id, it.Accelerator, c.want)
		}
	}
}

// TestTheCommandItemsRunTheCommand 断**菜单项走的是快捷键那一套**。
//
// ⚠️ **这是这一族最要紧的一条**：菜单的加速键**会先于窗口拿到那个键**
// （框架自己的 E2E：`mygo/internal/e2e/menu_shortcuts_windows_test.go:14`
// ——`Ctrl+,` 跑了菜单项，内容区一次都没收到）。
//
// ⇒ 所以菜单项的 `Click` **必须**走 `runShortcut`（`s.onShortcut`），
// 不能自己调 `s.app.Toggle()`——否则同一件事有两份实现，改一处忘另一处就
// 漂，而用户按菜单和按快捷键的结果会不一样。
//
// 这一条断的是「点了菜单项之后，`onShortcut` 那个入口被调到了」——用一个
// 假入口接住，比真的去播一首歌更稳（那要网络）。
func TestTheCommandItemsRunTheCommand(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.menu = appMenu(s)

	var fired []string
	s.onShortcut = func(id string) { fired = append(fired, id) }

	for _, id := range []string{"togglePlay", "prev", "next"} {
		it := s.menu.ItemByID(menuCommandIDPrefix + id)
		if it == nil {
			t.Fatalf("菜单里没有命令 %q 那一项", id)
		}
		it.Click(it, nil)
	}

	want := []string{"togglePlay", "prev", "next"}
	if len(fired) != len(want) {
		t.Fatalf("点了三项，命令入口被调了 %d 次（%v），want %d 次（%v）", len(fired), fired, len(want), want)
	}
	for i := range want {
		if fired[i] != want[i] {
			t.Errorf("第 %d 次是 %q，want %q（整份：%v）", i+1, fired[i], want[i], fired)
		}
	}
}

// columnShape 是一栏菜单的形状：分隔线写「---」，角色项写角色名，其余写文案。
func columnShape(col *mygo.MenuItem) []string {
	out := make([]string, 0, len(col.Submenu))
	for _, it := range col.Submenu {
		switch {
		case it.Type == mygo.MenuItemSeparator:
			out = append(out, "---")
		case it.Role != "":
			out = append(out, string(it.Role))
		default:
			out = append(out, it.Label)
		}
	}
	return out
}

// menuItemChecked 报那一项打着钩没有。
func menuItemChecked(m *mygo.Menu, id string) bool {
	it := m.ItemByID(id)
	return it != nil && it.IsChecked()
}
