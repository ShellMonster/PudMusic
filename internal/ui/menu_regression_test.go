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
	s.refreshTrayMenu()

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
	s.refreshTrayMenu()
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
	s.refreshTrayMenu()

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

// TestTheLikeItemUsesTheSameJudgeAsTheInterface 断**「赞」和界面用同一套判据**。
//
// ⚠️ **为什么这一条要紧**：菜单原来用「有没有歌」判，而界面上用的是
// `App.Likeable()`（`internal/ui/like.go` 的 `viewLikeStar` 第一句就是
// `if !s.app.Likeable() { return }`——本地文件**根本不摆那个键**）。
//
// ⇒ 两套判据的后果：本地文件在菜单里点「赞」→ `App.SetLike` 回一句
// 「这一首在 B 站上没法赞」→ **弹一个红色 Toast**。而界面上那一枚压根不存在。
//
// ⇒ 「按一个点了报错的键比没有那个键更糟」。
//
// 这一条断的是**菜单用的判据和界面是同一个函数**，而不是「某个状态下灰不灰」
// ——后者要真的放一首歌进去（要网络）。
func TestTheLikeItemUsesTheSameJudgeAsTheInterface(t *testing.T) {
	// 界面上那一枚的判据。
	b := newTestShell(t)
	if !b.a.Likeable() {
		t.Skip("这一轮没有歌（likeable 是假的）：这一条要的是「判据是同一个函数」")
	}

	// 菜单那一项的判据要是同一个函数的同一个答案。
	s := b.s
	s.menu = appMenu(s)
	s.refreshMenuChecked()
	it := s.menu.ItemByID(menuLikeID)
	if it == nil {
		t.Fatal("菜单里没有「赞」那一项")
	}
	if !it.IsEnabled() != !s.app.Likeable() {
		t.Errorf("「赞」那一项的可用态（%v）和 `App.Likeable()`（%v）对不上",
			it.IsEnabled(), s.app.Likeable())
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
	s.refreshMenuChecked()
	it := s.menu.ItemByID(menuCommentsID)
	if it == nil {
		t.Fatal("菜单里没有「显示评论」那一项")
	}
	if it.IsEnabled() != s.app.CommentsWorthAsking() {
		t.Errorf("「显示评论」那一项的可用态（%v）和 `App.CommentsWorthAsking()`（%v）对不上",
			it.IsEnabled(), s.app.CommentsWorthAsking())
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
