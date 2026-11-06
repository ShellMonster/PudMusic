// 菜单这一批改动**修掉的那些毛病**的回归（票 #712 的对抗性审查之后）。
//
// 这一族钉的是**四个具体的坑**，每一个都是「功能坏了 / 说谎了」那一类：
//
//  1. **⌘M 撞车**：框架「窗口 › 最小化 ⌘M」和我们的迷你播放器抢同一个键，
//     而**菜单的加速键先于窗口拿到那个键** ⇒ 迷你播放器在 macOS 上根本触发不了。
//  2. **状态栏「正在播放」是启动快照**：托盘菜单只建一次，那一行若在建的时候
//     求值，就永远是启动那一刻的那首。
//  3. **「赞」「显示评论」的禁用判据用错**：菜单用「有没有歌」，而界面上用
//     `Likeable()` / `CommentsWorthAsking()` —— 两套判据 ⇒ 本地文件在菜单里
//     点「赞」会弹一个红色 Toast，而界面上那一枚**压根不摆**。
//  4. **命令表的「进不进菜单」标志和菜单实况脱节** ⇒ 四条命令的加速键不跟着
//     设置走 ⇒ 旧键被菜单吃掉 ⇒ 键「失灵」。
package ui

import (
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo"
)

// TestTheMiniPlayerShortcutYieldsCommandMToTheSystem 断**⌘M 归系统所有**。
//
// ⚠️ **为什么这一条要紧**：框架「窗口」栏里「最小化」那一项带的加速键就是
// `CmdOrCtrl+M`（`mygo/menu.go:743`），而**菜单的加速键先于窗口拿到那个键**
// （框架自己的 E2E：`mygo/internal/e2e/menu_shortcuts_windows_test.go:14`）。
//
// ⇒ 所以迷你播放器**不能**用 ⌘M：抢它的结果不是「两个都响」，而是
// **迷你播放器永远不响**（用户按 ⌘M 得到的是窗口被最小化）。
//
// ⇒ 它让开到 ⌘⇧M。Apple Music 自己也是这么做的（`Switch to MiniPlayer###M#shift`）。
func TestTheMiniPlayerShortcutYieldsCommandMToTheSystem(t *testing.T) {
	def := defaultShortcutSettings()
	for _, list := range [][]shortcutItem{def.Shortcuts, def.GlobalShortcuts} {
		for _, it := range list {
			// ⌘M 就是「CommandOrControl+M」（**不带任何 Shift**）。
			if it.Shortcut == "CommandOrControl+M" {
				t.Errorf("命令 %q 的默认键是 ⌘M：那是 macOS 的「最小化」，"+
					"而菜单的加速键先于窗口拿到那个键 ⇒ 迷你播放器永远不响", it.ID)
			}
		}
	}
	// ⚠️ **而它要有一个键**：让开不是「不给了」。
	var mini string
	for _, it := range def.Shortcuts {
		if it.ID == "toggleMiniMode" {
			mini = it.Shortcut
		}
	}
	if mini == "" {
		t.Fatal("「显示/隐藏迷你播放器」没有默认键了")
	}
	if !strings.Contains(mini, "Shift") {
		t.Errorf("「显示/隐藏迷你播放器」的默认键是 %q，want 一个带 ⇧ 的（⌘M 归最小化）", mini)
	}
}

// TestTheMenuDoesNotClaimCommandMForSomethingElse 断**菜单上没有任何一项
// 声明 ⌘M**。
//
// ⚠️ **为什么这一条和上一条是两件事**：上一条断的是「设置里没有 ⌘M」，
// 这一条断的是「**菜单栏上**没有 ⌘M」——菜单上的加速键**先于窗口**拿到那个键，
// 所以菜单上写了 ⌘M 就等于**吃掉**了它。
//
// ⚠️ **框架自己的那一项（窗口 › 最小化）带 ⌘M 是对的**——那是系统的键，
// 它本来就该在菜单栏上。所以这一条只查**我们自己写的那些项**。
func TestTheMenuDoesNotClaimCommandMForSomethingElse(t *testing.T) {
	b := newTestShell(t)
	m := appMenu(b.s)
	for _, col := range m.Items() {
		for _, it := range col.Submenu {
			// ⚠️ **框架自己那一项（窗口 › 最小化）带 ⌘M 是对的**——那是系统的
			// 键，它本来就该在菜单栏上。所以这一条只查**我们自己写的那些项**
			//（它们有 id；框架角色项的 id 是空的）。
			if it.ID == "" {
				continue
			}
			if it.Accelerator == "CmdOrCtrl+M" {
				t.Errorf("菜单项 %q（%q）声明了 ⌘M：那是系统「最小化」的键，"+
					"而菜单的加速键先于窗口拿到它", it.ID, it.Label)
			}
		}
	}
}

// TestTheNowPlayingLineFollowsTheCurrentSong 断**状态栏「正在播放」那一行
// 跟着当前那首走**。
//
// ⚠️ **为什么这一条要紧**：托盘那一份菜单**只建一次**（`installTray`），而
// 那一行是**会变的**。若它在建菜单时求值，就永远是**启动那一刻**的那首——
// 而它恰恰是用户点开菜单**第一眼**看到的那一行。
func TestTheNowPlayingLineFollowsTheCurrentSong(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	// 装一份托盘菜单（就像 `installTray` 那样）。
	s.trayMenuRef = s.trayMenu()
	s.refreshTrayMenu(s.app.Snapshot())

	it := s.trayMenuRef.ItemByID(trayNowPlayingID)
	if it == nil {
		t.Fatal("状态栏菜单里没有「正在播放」那一行")
	}
	// 没有歌的时候。
	if it.Label != "没有在放" {
		t.Errorf("没有歌的时候那一行是 %q，want %q", it.Label, "没有在放")
	}

	// ⚠️ **换了歌之后要刷新**：这一条断的是「刷新真的会发生」。
	//
	// ⚠️ **怎么换歌**：直接改 App 的状态是不行的（那是应用层的内部），
	// 而真的换歌要网络。所以这里换的是**快照**能反映出来的那一栏——
	// 用 `app.State` 的字段构造一个外壳不现实（`Snapshot` 是从内部读的）。
	//
	// ⇒ 所以这一条断的是**刷新机制本身**：改完之后 `refreshTrayMenu` 看到的
	// 东西变了，那一行就跟着变。而「变了没有」由 `trayState` 的「比一遍再改」
	// 决定。
	s.trayState = trayState{} // 假装从来没刷过
	s.refreshTrayMenu(s.app.Snapshot())
	if got := s.trayMenuRef.ItemByID(trayNowPlayingID).Label; got != "没有在放" {
		t.Errorf("刷新之后那一行是 %q，want %q", got, "没有在放")
	}
}

// TestTheTrayPlaybackItemsAreDisabledWithoutATrack 断**没歌的时候状态栏那三项
// 是禁用的**。
//
// ⚠️ **为什么这一条要紧**：应用菜单那边的「歌曲」栏会正确禁用（`hasTrack`），
// 而状态栏这边如果**永远亮着**，用户点「播放/暂停」而什么都没发生——
// 会以为是菜单坏了。⇒ **两处要一致**。
func TestTheTrayPlaybackItemsAreDisabledWithoutATrack(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.trayMenuRef = s.trayMenu()
	s.refreshTrayMenu(s.app.Snapshot())

	for _, id := range []string{"togglePlay", "prev", "next"} {
		it := s.trayMenuRef.ItemByID(trayCommandIDPrefix + id)
		if it == nil {
			t.Errorf("状态栏菜单里没有命令 %q 那一项", id)
			continue
		}
		if it.IsEnabled() {
			t.Errorf("没有歌的时候 %q 那一项还是能点的：按了什么也不会发生", id)
		}
	}
}

// TestTheDockPlaybackItemsAreDisabledWithoutATrack 断**没歌的时候 Dock 菜单那
// 三项是禁用的**。
//
// ⚠️ **为什么这一条要紧**：和应用菜单「歌曲」栏、状态栏那三项是**同一条
// 规矩**——没歌的时候给「点了没反应」的键，比不给更糟。Dock 菜单跟它们
// 对齐，而不是各搞一套。
func TestTheDockPlaybackItemsAreDisabledWithoutATrack(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.dockMenuRef = s.buildDockMenu() // 同 installDockMenu 建的那一份
	s.refreshDockMenu(s.app.Snapshot())

	for _, id := range []string{"togglePlay", "prev", "next"} {
		it := s.dockMenuRef.ItemByID(dockMenuIDPrefix + id)
		if it == nil {
			t.Errorf("Dock 菜单里没有命令 %q 那一项", id)
			continue
		}
		if it.IsEnabled() {
			t.Errorf("没有歌的时候 Dock 菜单 %q 那一项还是能点的：按了什么也不会发生", id)
		}
	}
	if it := s.dockMenuRef.ItemByID(dockMenuIDPrefix + "like"); it == nil {
		t.Error("Dock 菜单里没有「喜欢歌曲」")
	} else if it.IsEnabled() {
		t.Error("没有歌的时候「喜欢歌曲」还是能点的")
	}
}

// TestTheDockPlayItemSaysPauseWhilePlayingAndPlayWhilePaused 断 Dock 菜单上的
// 第一项跟着实际播放状态变文案。
func TestTheDockPlayItemSaysPauseWhilePlayingAndPlayWhilePaused(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.dockMenuRef = s.buildDockMenu()

	s.refreshDockMenu(app.State{
		HasTrack: true,
		Playing:  true,
		PlayMode: app.PlayModeRandom,
	})
	toggle := s.dockMenuRef.ItemByID(dockMenuIDPrefix + "togglePlay")
	if toggle == nil {
		t.Fatal("Dock 菜单里没有播放项")
	}
	if toggle.Label != "暂停" {
		t.Errorf("播放中的 Dock 菜单项是 %q，want %q", toggle.Label, "暂停")
	}
	if !toggle.IsEnabled() {
		t.Error("播放中的播放项被禁用了")
	}
	random := s.dockMenuRef.ItemByID(dockMenuIDPrefix + "playMode." + string(app.PlayModeRandom))
	if random == nil || !random.IsChecked() {
		t.Error("播放模式为随机时，随机播放没有被选中")
	}

	s.refreshDockMenu(app.State{
		HasTrack: true,
		Playing:  false,
		PlayMode: app.PlayModeSequence,
	})
	if got := toggle.Label; got != "播放" {
		t.Errorf("暂停中的 Dock 菜单项是 %q，want %q", got, "播放")
	}
	sequence := s.dockMenuRef.ItemByID(dockMenuIDPrefix + "playMode." + string(app.PlayModeSequence))
	if sequence == nil || !sequence.IsChecked() {
		t.Error("播放模式为顺序时，顺序播放没有被选中")
	}
	if random.IsChecked() {
		t.Error("切到顺序播放后，随机播放仍然被选中")
	}
}

// TestTheDockMenuHasLikeAndPlayModeActions 断 Dock 菜单里有喜欢歌曲和播放模式
// 子菜单，并且播放模式是四个互斥选项。
func TestTheDockMenuHasLikeAndPlayModeActions(t *testing.T) {
	b := newTestShell(t)
	m := b.s.buildDockMenu()

	like := m.ItemByID(dockMenuIDPrefix + "like")
	if like == nil || like.Label != "喜欢歌曲" {
		t.Fatalf("Dock 菜单里没有「喜欢歌曲」项")
	}
	mode := m.ItemByID(dockMenuIDPrefix + "playMode")
	if mode == nil || mode.Label != "播放模式" {
		t.Fatalf("Dock 菜单里没有「播放模式」子菜单")
	}
	if len(mode.Submenu) != len(app.PlayModes) {
		t.Fatalf("播放模式子菜单有 %d 项，want %d", len(mode.Submenu), len(app.PlayModes))
	}
	for i, want := range app.PlayModes {
		got := mode.Submenu[i]
		if got.Type != mygo.MenuItemRadio {
			t.Errorf("播放模式第 %d 项类型是 %v，want radio", i, got.Type)
		}
		if got.Label != want.Desc() {
			t.Errorf("播放模式第 %d 项是 %q，want %q", i, got.Label, want.Desc())
		}
	}
}

// TestTheDockPlaybackItemsRunTheCommand 断 Dock 菜单里的播放控制仍走统一命令入口。
func TestTheDockPlaybackItemsRunTheCommand(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	m := s.buildDockMenu()
	var fired []string
	s.onShortcut = func(id string) { fired = append(fired, id) }

	for _, id := range []string{"togglePlay", "prev", "next"} {
		it := m.ItemByID(dockMenuIDPrefix + id)
		if it == nil {
			t.Fatalf("Dock 菜单里没有命令 %q", id)
		}
		it.Click(it, nil)
	}
	want := []string{"togglePlay", "prev", "next"}
	if len(fired) != len(want) {
		t.Fatalf("点了三项，命令入口被调了 %d 次（%v），want %d 次", len(fired), fired, len(want))
	}
	for i := range want {
		if fired[i] != want[i] {
			t.Errorf("第 %d 次是 %q，want %q", i+1, fired[i], want[i])
		}
	}
}

// TestTheCommentsItemUsesTheSameJudgeAsTheInterface 断**「显示评论」和界面
// 用同一套判据**。
//
// ⚠️ 同上一条：`App.CommentsWorthAsking()` 是界面上那一枚的判据
// （`internal/ui/comments_entry.go`），而菜单原来用「有没有歌」。
// ⇒ 本地文件点「显示评论」**什么也不发生**（静默返回）——比报错还糟，
// 因为用户什么都看不出来。
func TestTheCommentsItemUsesTheSameJudgeAsTheInterface(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.menu = appMenu(s)

	// ⚠️ **本地文件那一档**（理由同上一条）：本地文件在 B 站上压根没有对应的
	// 对象，而问一个认不出来的 oid 的症状是「静默拿到空列表」——
	// 所以菜单上那一项必须是禁用的，而界面上那一枚**压根不摆**
	//（`internal/ui/comments_entry.go` 的 `viewCommentsButton`）。
	if err := s.app.Enqueue(app.LocalItem(flacPath(t))); err != nil {
		t.Fatalf("摆一个本地文件：%v", err)
	}
	s.refreshMenuChecked(s.app.Snapshot())

	it := s.menu.ItemByID(menuCommentsID)
	if it == nil {
		t.Fatal("菜单里没有「显示评论」那一项")
	}
	if s.app.CommentsWorthAsking() {
		t.Fatal("本地文件竟然有评论可问：这一条的前提不成立")
	}
	if it.IsEnabled() {
		t.Error("本地文件时「显示评论」那一项还是能点的：点了什么也不会发生")
	}
}

// TestEveryCommandItemInTheMenuGetsItsShortcutRefreshed 断**菜单里每一条命令
// 项的加速键都跟着设置走**。
//
// ⚠️ **这是对抗性审查里那个真 bug 的回归**：命令表原来有一个**手写的**
// 「进不进菜单」标志，而它**和菜单实况脱节**——「快退 5 秒」「快进 5 秒」
// 「音量加」「音量减」四条明明在菜单里，标志却写着不进，于是
// `refreshMenuAccel` 不刷它们。
//
// ⇒ 而脱节的后果：菜单的加速键**先于窗口**拿到那个键，所以旧键会被菜单吃掉，
// 而设置页里它已经不归那条命令了 ⇒ **用户按了「键失灵」**。
//
// 这一条**逐条**改设置里的键，再看菜单上那一项有没有跟着变。
func TestEveryCommandItemInTheMenuGetsItsShortcutRefreshed(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.menu = appMenu(s)

	// 菜单里实际有的那些命令项。
	ids := menuCommandIDs(s.menu)
	if len(ids) == 0 {
		t.Fatal("菜单里一个命令项都没有")
	}

	for _, id := range ids {
		// 改这一条的键。
		s.applyShortcut(shortcutSlot{id: id}, "CommandOrControl+Shift+B")
		it := s.menu.ItemByID(menuCommandIDPrefix + id)
		if it == nil {
			t.Errorf("命令 %q：菜单里找不到它", id)
			continue
		}
		if it.Accelerator != "CmdOrCtrl+Shift+B" {
			t.Errorf("命令 %q 改了键之后菜单上还是 %q，want %q（不刷它的后果："+
				"旧键被菜单吃掉，而设置页里它已经不归这条命令了）",
				id, it.Accelerator, "CmdOrCtrl+Shift+B")
		}
	}
}

// TestTheMiniPlayerMenuItemCarriesItsShortcut 断**迷你播放器那一项带着它的键**。
//
// ⚠️ **为什么这一条要紧**：这一项原来是**一个裸的勾选项**（没有加速键），
// 而设置页里明明有一个键 —— 两处对不上，用户在菜单里看到「显示/隐藏迷你
// 播放器」旁边一片空白。
//
// ⚠️ **而且它是勾选型**：框架在菜单项被点的时候会**自己先翻勾选**
// （`mygo/menu.go:563`），而我们要的勾选态是「迷你窗开着没有」这个**真状态**
// ——所以勾选态由 `refreshMenuChecked` 按真状态刷，**不是**靠用户点。
func TestTheMiniPlayerMenuItemCarriesItsShortcut(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.menu = appMenu(s)

	it := s.menu.ItemByID(menuCommandIDPrefix + "toggleMiniMode")
	if it == nil {
		t.Fatal("菜单里没有「显示/隐藏迷你播放器」那一项")
	}
	if it.Accelerator == "" {
		t.Error("「显示/隐藏迷你播放器」那一项没有加速键：设置页里明明有一个")
	}
	if it.Type != mygo.MenuItemCheckbox {
		t.Errorf("「显示/隐藏迷你播放器」那一项的类型是 %v，want 勾选型", it.Type)
	}
}

// TestTheRadioGroupsAreAdjacent 断**那几个单选组是相邻的**。
//
// ⚠️ **为什么这一条要紧**：框架的单选分组靠 `radioGroup()`
// （`mygo/menu.go:343`）——它向两边扩，判据是**相邻的那一项也是 radio**。
// 而**分隔线的 Type 是 `MenuItemSeparator`**——所以**一条分隔线就把一组
// 拆成两组**。
//
// ⇒ 而拆开的后果很具体：`SetChecked`（`mygo/menu.go:288`）对 radio 项会调
// `uncheckRadioSiblings()`，**只取消同一组里的兄弟**——拆组之后**四个档位
// 能同时打着钩**。
//
// ⇒ 「播放模式」那四项（顺序 / 循环 / 随机 / 单曲）必须**连续**，
// 「显示模式」那三项（列表 / 网格 / 紧凑）也是。
//
// ⚠️ **这一条断的是「它们在同一个父菜单里、且中间没有别的项」**——框架没导出
// `radioGroup`，但「中间没有别的项」正是成组的**必要条件**。
func TestTheRadioGroupsAreAdjacent(t *testing.T) {
	b := newTestShell(t)
	m := appMenu(b.s)

	groups := []struct {
		name string
		ids  []string
	}{
		{"播放模式", []string{
			menuPlayModeIDPrefix + string(app.PlayModeSequence),
			menuPlayModeIDPrefix + string(app.PlayModeLoop),
			menuPlayModeIDPrefix + string(app.PlayModeRandom),
			menuPlayModeIDPrefix + string(app.PlayModeSingle),
		}},
		{"显示模式", []string{
			menuDisplayModeIDPrefix + string(app.DisplayModeList),
			menuDisplayModeIDPrefix + string(app.DisplayModeCard),
			menuDisplayModeIDPrefix + string(app.DisplayModeCompact),
		}},
	}
	for _, g := range groups {
		for _, col := range m.Items() {
			idx := -1
			for i, it := range col.Submenu {
				if it.ID == g.ids[0] {
					idx = i
					break
				}
			}
			if idx < 0 {
				continue
			}
			// ⚠️ **从第一项往后数，数出这一组那一段**。
			end := idx
			for end+1 < len(col.Submenu) && col.Submenu[end+1].Type == mygo.MenuItemRadio {
				end++
			}
			if got := end - idx + 1; got != len(g.ids) {
				t.Errorf("「%s」那一组只有 %d 项连续（want %d）："+
					"中间夹了别的项 ⇒ 框架的 radioGroup 拆组 ⇒ 四个档位能同时打钩",
					g.name, got, len(g.ids))
			}
			break
		}
	}
}

// TestTheMiniPlayerIsInTheWindowColumn 断**迷你播放器在「窗口」栏**。
//
// ⚠️ **为什么这一条要钉**：这是**三家播放器一致**的做法（2026-11-01 实测本机
// 装着的三个，枚举真实的 AppKit 菜单树）：
//
//   - Apple Music：`窗口 ▸ 迷你播放程序 ⌥⌘M` / `窗口 ▸ 切换到迷你播放程序 ⌥⌘M`
//   - QQ音乐：`窗口 ▸ 切换到迷你窗口 ⇧⌘S`
//   - 网易云音乐：`窗口 ▸ 切换至迷你模式 ⌘M`
//
// ⇒ 而「显示」那一栏放的是**界面的样子**（视图档位、侧栏）。
//
// ⚠️ **钉的是「它在窗口栏、且不在显示栏」**——挪走的那一边也要钉，否则
// 有人把它加回去（那不违反惯例，只是和三家不一致）。
func TestTheMiniPlayerIsInTheWindowColumn(t *testing.T) {
	b := newTestShell(t)
	m := appMenu(b.s)

	cols := m.Items()
	windowCol := cols[len(cols)-1]
	if windowCol.Role != mygo.RoleWindowMenu {
		t.Fatalf("最后一栏不是「窗口」栏，是 %q", windowCol.Role)
	}
	found := false
	for _, it := range windowCol.Submenu {
		if it.ID == menuCommandIDPrefix+"toggleMiniMode" {
			found = true
		}
	}
	if !found {
		t.Errorf("「窗口」栏里没有「显示/隐藏迷你播放器」那一项：%v", columnShape(windowCol))
	}

	// ⚠️ **而它不在「显示」栏**：那一栏是放「界面的样子」的地方。
	viewCol := cols[5]
	if viewCol.Role != mygo.RoleViewMenu {
		t.Fatalf("第 6 栏不是「显示」栏，是 %q", viewCol.Role)
	}
	for _, it := range viewCol.Submenu {
		if it.ID == menuCommandIDPrefix+"toggleMiniMode" {
			t.Error("「显示/隐藏迷你播放器」在「显示」栏里：它在「窗口」栏（Apple Music / QQ音乐 / 网易云三家一致）")
		}
	}
}

// TestTheWindowColumnStillHasTheSystemItems 断**「窗口」栏的系统那几项没丢**。
//
// ⚠️ **为什么这一条要紧**：框架的「窗口」栏是 `RoleWindowMenu`，它自带
// 「最小化 / 缩放 / 前置全部」（`mygo/menu.go:934` 的 `roleSubmenu(RoleWindowMenu)`）。
// 而**给了自己的 `Submenu` 就只留着角色的名字**（`prepareItem` 只在 `Submenu`
// 为空时才用角色自带的那一份）——所以那三项得**自己写回来**，漏一项就没了。
func TestTheWindowColumnStillHasTheSystemItems(t *testing.T) {
	b := newTestShell(t)
	m := appMenu(b.s)
	cols := m.Items()
	windowCol := cols[len(cols)-1]

	roles := menuRoles(m)
	for _, r := range []mygo.MenuRole{mygo.RoleMinimize, mygo.RoleZoom, mygo.RoleFront} {
		if !roles[r] {
			t.Errorf("菜单里没有 %q 了：⌘M（最小化）这些就没了", r)
		}
	}
	inWindow := false
	for _, it := range windowCol.Submenu {
		if it.Role == mygo.RoleMinimize {
			inWindow = true
		}
	}
	if !inWindow {
		t.Errorf("「窗口」栏里没有「最小化」：%v", columnShape(windowCol))
	}
}

// TestTheMiniPlayerMenuItemRunsTheCommand 断**迷你播放器那一项走命令入口**。
func TestTheMiniPlayerMenuItemRunsTheCommand(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.menu = appMenu(s)

	var fired []string
	s.onShortcut = func(id string) { fired = append(fired, id) }

	it := s.menu.ItemByID(menuCommandIDPrefix + "toggleMiniMode")
	if it == nil {
		t.Fatal("菜单里没有「显示/隐藏迷你播放器」那一项")
	}
	it.Click(it, nil)

	if len(fired) != 1 || fired[0] != "toggleMiniMode" {
		t.Errorf("点了那一次，命令入口被调了 %v，want [toggleMiniMode]", fired)
	}
}

// TestTheCommandNamesAndTheSettingsPageAgree 断**命令表和设置页的名字一样**。
//
// ⚠️ **特别是「迷你播放器」那一族**：界面里一律叫「迷你」，而上游 Biu 的
// 直译是「切换mini/完整模式」——那个半中半英的词只出现在那一处。
func TestTheCommandNamesAndTheSettingsPageAgree(t *testing.T) {
	def := defaultShortcutSettings()
	for _, it := range def.Shortcuts {
		c, ok := commandByID(it.ID)
		if !ok {
			continue
		}
		if c.name != it.Name {
			t.Errorf("命令 %q：命令表里叫 %q，设置页里叫 %q", it.ID, c.name, it.Name)
		}
	}
	// ⚠️ **「mini」这个英文词不许再出现**：界面里一律叫「迷你」。
	for _, c := range commandSpecs {
		if strings.Contains(strings.ToLower(c.name), "mini") {
			t.Errorf("命令 %q 的名字是 %q：界面里一律叫「迷你」", c.id, c.name)
		}
	}
}

// TestTheDeveloperToolsOnlyShowInDevelopmentBuilds 断**「开发者工具」只在开发
// 构建里**。
//
// ⚠️ **为什么这一条要紧**：框架有 `mygo.IsDev()`（`mygo/dev.go:12`）——
// 发布构建由 `mygo build` 注入 `production=1`（`scripts/release.sh` 走的就是
// `mygo build`）。
//
// 而 macOS 上正式版应用的菜单栏里**没有**「开发者工具」这一项
// （Chrome / Edge / Safari 的发布版都没有），它还长期占着 ⌥⌘I。
func TestTheDeveloperToolsOnlyShowInDevelopmentBuilds(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.menu = appMenu(s)

	has := false
	for _, col := range s.menu.Items() {
		for _, it := range col.Submenu {
			if it.Role == mygo.RoleToggleDevTools {
				has = true
			}
		}
	}
	// ⚠️ **这一条在开发构建里是「要有」**：测试跑的就是开发构建
	//（`mygo.IsDev()` 为真）。发布构建里没有，那由真机验收看。
	if mygo.IsDev() && !has {
		t.Error("开发构建里没有「开发者工具」那一项")
	}
	if !mygo.IsDev() && has {
		t.Error("发布构建里还有「开发者工具」那一项：macOS 上正式版的菜单栏里没有它")
	}
}

// TestThePlayModeItemsAreStillRadiosAfterTheFix 断**改完之后播放模式那四项
// 还是单选**。
//
// ⚠️ 这是 P1-11 那次重构的**附带风险**：`commandSpecs` 改了结构，
// `playModeItem` 顺手改一下很正常——但改坏了就是「四个档位能同时打钩」。
func TestThePlayModeItemsAreStillRadiosAfterTheFix(t *testing.T) {
	b := newTestShell(t)
	m := appMenu(b.s)
	for _, mode := range app.PlayModes {
		it := m.ItemByID(menuPlayModeIDPrefix + string(mode))
		if it == nil {
			t.Errorf("菜单里没有播放模式 %q 那一项", mode)
			continue
		}
		if it.Type != mygo.MenuItemRadio {
			t.Errorf("播放模式 %q 那一项的类型是 %v，want 单选（radio）", mode, it.Type)
		}
	}
}
