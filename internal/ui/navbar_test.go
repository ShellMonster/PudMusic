// 顶栏这一组跑的是 MyGo 的 Tester：和真窗口一模一样的 layout 与输入处理，只是画到
// 内存里。点得到、按得到键、找得到文字，所以「能不能用」能自动验。
//
// ⚠️ 每条都要 `tt.SetFocused(true)`：MyGo 的 `Element.Focused()` 判的是
// `rt.focused == e.id && rt.windowFocus()`（`ui/input.go:1048`），窗口没被
// 标成「在前台」的话它**恒为 false**，于是搜索框的失焦逻辑会每帧都成立。

package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	mygoui "github.com/egoist/mygo/ui"
)

// newShellWithSettings 建一个挂了设置的外壳。
//
// 设置里的「显示搜索历史」得先挂上才改得动：`app.SettingsStore` 没挂上时
// `Update` 直接返回 ErrSettingsClosed（见 internal/app/settings_mount.go）。
func newShellWithSettings(t *testing.T) (*Shell, *mygoui.Tester) {
	t.Helper()
	b := newTestShell(t, withSettings(app.DefaultSettings("")))
	return b.s, b.tt
}

// loggedInWith 是「造外壳之前先挂一份登录态」这一个选项。
//
// 先往凭据后端里存一份登录态，再 MountLogin——`MountLogin` 会把它读回来，所以走
// 的是生产那条路，不是往界面上硬塞一个「已登录」标志。
//
// 凭据后端用内存那一份（不碰真的钥匙串）；B 站客户端给一个空的：这一组只验
// 「菜单项接到了哪」，不联网，而 `SeedCookies` 只动 jar，不发请求。
//
// ⚠️ **要摆侧栏那三个入口的测试都得挂它**：「我的关注」「稍后再看」「历史记录」
// 要登录才摆（见 `Route.NeedsLogin`），没挂这一份的外壳上那三行压根不在
// （`TestTheSidebarHidesTheLoginOnlyEntriesUntilYouLogIn` 钉的就是那件事）。
// 那些测试只是**恰好**要用一个侧栏入口的话，换成不挑登录态的那几个更好——
// 它们钉的是别的东西，不该顺带把登录态也钉进去。
func loggedInWith(mid string) shellOpt {
	return mounted(func(t *testing.T, a *app.App) {
		store := app.NewMemoryStore("test")
		if err := store.Save(context.Background(), app.Session{SESSDATA: "sess", DedeUserID: mid}); err != nil {
			t.Fatal(err)
		}
		a.MountLogin(store, bili.NewClient(bili.Options{}))
	})
}

// newLoggedInShell 建一个**已经登录**的外壳。
func newLoggedInShell(t *testing.T, mid string) (*Shell, *mygoui.Tester) {
	t.Helper()
	b := newTestShell(t, loggedInWith(mid))
	return b.s, b.tt
}

// 扫码登录那一页的路由用 login.go 里那个 loginPath，这里不另立一份。

// Biu 的 `pollingInterval: 300000`（`navbar/user-feed/index.tsx:24`）。
//
// ⚠️ **这里写死 5 分钟，不引用 `dynamicFeedInterval`**：用那边常量算期望的话，
// 常量一改，步进和期望一起变，测试照样绿——「把间隔改成 4 分钟」这种改动就发现
// 不了了。这个仓库在封面那条（playbar 的 Biu 尺寸）上踩过同一个坑。
const wantFeedInterval = 5 * time.Minute

// fakeClockStart 是那只假表从哪儿起算。
//
// 挑一个整点：这一组断言里出现的时刻全都能手算出来，看日志时不用去猜
// 「0 基点是哪一刻」。
//
// 用的是界面层那一个假时钟（clock.go 的 fakeClock），不是另做一只——红点那个
// 节奏走的就是 `Shell.clock`，换上别的表等于没换（见 navbar_feed.go 文件头）。
var fakeClockStart = time.Date(2026, time.October, 16, 9, 0, 0, 0, time.UTC)

// fakeClockOn 把这只假表装到外壳上。
//
// `next` 拨回零值：造外壳的时候已经画过一帧，那一帧走的是真时钟，它已经把
// 「下一次该轮的时刻」推到 5 分钟之后了。拨回零值，下面第一帧才是干净的
// 「挂上就该先问一次」。
func fakeClockOn(s *Shell, t time.Time) *fakeClock {
	c := newFakeClock(t)
	s.clock = c
	s.feed.next = time.Time{}
	return c
}

// TestNavBarSpansTheTopOfTheWindow 验顶栏真的横贯窗口顶上的一条。
//
// 「整条是拖动区」这件事本身无头**验不了**：MyGo 无头宿主的 `startDrag` 是空
// 实现（`ui/headless.go:88`），拖没拖窗口在画面上看不出来。这条能验的是拖动区
// 成立的前提——它得是**顶上那一条、并且横贯整个窗口宽度**，否则有一片窗口
// 拖不动。
func TestNavBarSpansTheTopOfTheWindow(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)

	back, ok := tt.Find(navBackLabel)
	if !ok {
		t.Fatalf("顶栏上没有后退，文字有 %q", tt.Texts())
	}
	avatar, ok := tt.Find(avatarLabel)
	if !ok {
		t.Fatalf("顶栏上没有头像，文字有 %q", tt.Texts())
	}
	// 最右边那一格是登录入口（login.go 的 viewLoginEntry），顶栏「顶满」验的是
	// 它，不是头像——头像右边还有别的控件，拿头像验会得出一个假的「没顶满」。
	login, ok := tt.Find("登录")
	if !ok {
		t.Fatalf("顶栏上没有登录入口，文字有 %q", tt.Texts())
	}

	if back.Y < 0 || back.Y+back.H > navMinHeight {
		t.Errorf("后退按钮在 %v，不在顶上那 %v 高里", back, navMinHeight)
	}
	if avatar.Y < 0 || avatar.Y+avatar.H > navMinHeight {
		t.Errorf("头像在 %v，不在顶上那 %v 高里", avatar, navMinHeight)
	}
	if login.Y < 0 || login.Y+login.H > navMinHeight {
		t.Errorf("登录入口在 %v，不在顶上那 %v 高里", login, navMinHeight)
	}
	// 左边贴着窗口左边、右边贴着窗口右边 = 这一条横贯整个宽度。
	//
	// 留出来的 slack 是：顶栏自己的右内边距（8，见 viewNav 的 Padding）
	// 加那个控件自己的内边距。**Tester 只能量到文字/名字那一小块**，
	// 量不到按钮本身——`Find` 给的是标签节点的框（`ui/runtime.go:964`）。
	// 所以这里量的是「顶栏那一组控件顶到了窗口右边附近」，不是「按钮的边缘
	// 正好等于窗口边缘」。
	const slack = 32
	if back.X < 8 {
		t.Errorf("后退按钮左边缘 %v，快贴到窗口外了", back.X)
	}
	if right := login.X + login.W; float32(1200)-right > slack {
		t.Errorf("登录入口右边缘 %v，离窗口右边还有 %v（容差 %v），顶栏没顶满",
			right, 1200-right, slack)
	}
	if s.router.Path() != "/" {
		t.Errorf("看了一顶栏，路径变成了 %q", s.router.Path())
	}
}

// TestNavBarControlsStillWorkInsideTheDragArea 验拖动区没把里面的控件吃掉。
//
// 这是「整条都是拖动区」真正会出事的地方：命中链上最内层带输入标志的元素才是
// 目标（`ui/input.go:262`），拖动区做得不对，按钮就按不动了。
func TestNavBarControlsStillWorkInsideTheDragArea(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)

	if err := tt.Click(navBackLabel); err != nil {
		t.Fatalf("顶栏上点不到后退：%v，文字有 %q", err, tt.Texts())
	}
	if err := tt.Click("动态"); err != nil {
		t.Fatalf("顶栏上点不到「动态」：%v，文字有 %q", err, tt.Texts())
	}
	if got := s.router.Path(); got != dynamicFeedPath {
		t.Errorf("点「动态」之后在 %q，want %q", got, dynamicFeedPath)
	}
}

// TestSearchSuggestsHistoryAfterTyping 验搜索框里敲字会弹出历史建议浮层。
func TestSearchSuggestsHistoryAfterTyping(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	s.hist.add("周杰伦")
	s.hist.add("林俊杰")
	tt.Frame()

	tt.Click(searchLabel)
	tt.Type("周")

	if !tt.HasText("搜索历史") {
		t.Fatalf("敲了字没有弹出搜索历史浮层，文字有 %q", tt.Texts())
	}
	if !tt.HasText("周杰伦") || !tt.HasText("林俊杰") {
		t.Errorf("浮层上没有那两条历史，文字有 %q", tt.Texts())
	}
	// 输入框里敲的字得在浮层外面，别被浮层盖掉。
	if r, ok := tt.Find(searchLabel); !ok || r.W > searchWidth {
		t.Errorf("搜索框宽度是 %v，宽过 %v 就不是它了", r, searchWidth)
	}
}

// TestSuggestCollapsesWhenSelectingOne 验「选中一条」会收起浮层。
// TestSuggestCollapsesWhenSelectingOne 也钉着面板上那个 KeepFocus。
//
// 少了它，点历史那一条会先把焦点抢到那个按钮上，「失焦就收」在按下那一帧就成立，
// 浮层在 Children 之前就没了，那一条根本点不到——实测下来「选中」变成了「停在
// 首页」。摘掉 KeepFocus，这一条立刻红。
func TestSuggestCollapsesWhenSelectingOne(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	s.hist.add("周杰伦")
	tt.Frame()

	tt.Click(searchLabel)
	if !tt.HasText("搜索历史") {
		t.Fatalf("浮层没开，文字有 %q", tt.Texts())
	}
	if err := tt.Click("周杰伦"); err != nil {
		t.Fatalf("浮层上点不到那条历史：%v，文字有 %q", err, tt.Texts())
	}

	if s.searchSuggest {
		t.Error("选中一条之后浮层还开着")
	}
	if got := s.router.Path(); got != searchPath {
		t.Errorf("选中一条之后在 %q，want %q", got, searchPath)
	}
	if tt.HasText("搜索历史") {
		t.Errorf("浮层还在画面上，文字有 %q", tt.Texts())
	}
}

// TestSuggestCollapsesOnEscape 验「按 Esc」会收起浮层，**而且不连带清字**。
//
// 这一条是这个搜索框要自己拼、不用 ui.SearchField 的全部理由：
// SearchField 的 Esc 被它自己的清空按钮吃掉（`ui/combobox.go:437`），
// 浮层收不起来。
func TestSuggestCollapsesOnEscape(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	s.hist.add("周杰伦")
	tt.Frame()

	tt.Click(searchLabel)
	tt.Type("周")
	if !s.searchSuggest {
		t.Fatalf("敲了字浮层没开，文字有 %q", tt.Texts())
	}

	tt.Key(0, mygoui.KeyEscape)
	if s.searchSuggest {
		t.Errorf("按了 Esc 浮层还开着，文字有 %q", tt.Texts())
	}
	if tt.HasText("搜索历史") {
		t.Errorf("浮层还在画面上，文字有 %q", tt.Texts())
	}
	if got := s.searchKeyword; got != "周" {
		t.Errorf("按 Esc 把字也清了，搜索框里是 %q，want 周", got)
	}
}

// TestSuggestCollapsesOnBlur 验「失焦」会收起浮层。
func TestSuggestCollapsesOnBlur(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	s.hist.add("周杰伦")
	s.hist.add("林俊杰")
	tt.Frame()

	tt.Click(searchLabel)
	tt.Type("周")
	if !s.searchSuggest {
		t.Fatalf("敲了字浮层没开，文字有 %q", tt.Texts())
	}

	// 点到窗口里的别处去：焦点离开搜索框，浮层跟着收。
	//
	// 点侧栏的「本地音乐」：那是侧栏里**独一份**的文字（`Find` 只会认到先画
	// 的那一个）。别点「打开文件」——它会拉系统文件框。
	//
	// ⚠️ **这一段原来写着「顶栏的页名上也有一个『推荐音乐』」**——票 #127 把
	// 顶栏那一处页名去掉之后，理由跟着换了，而「挑一个不挑登录态的入口」那一条
	// 仍然是这一段真正要说的。
	//
	// ⚠️ 刻意挑一个**不挑登录态**的入口：这一条钉的是「点别处会失焦」，而
	// 「我的关注」「稍后再看」「历史记录」未登录时不摆（`Route.NeedsLogin`），
	// 拿它们当失焦的落点的话，这条测试还会顺带把登录态也钉进去。
	if err := tt.Click("本地音乐"); err != nil {
		t.Fatalf("点不到侧栏：%v，文字有 %q", err, tt.Texts())
	}

	if s.searchSuggest {
		t.Errorf("失焦之后浮层还开着，文字有 %q", tt.Texts())
	}
	if tt.HasText("搜索历史") {
		t.Errorf("浮层还在画面上，文字有 %q", tt.Texts())
	}
}

// TestSuggestHiddenWhenTheSettingIsOff 验设置里关掉「显示搜索历史」就不摆。
//
// 这条设置**只管显不显示，不管存不存**（Biu 同款，`search/index.tsx:121-124`）：
// 关掉之后文件里的历史一条都不能少。
func TestSuggestHiddenWhenTheSettingIsOff(t *testing.T) {
	s, tt := newShellWithSettings(t)
	tt.SetFocused(true)
	s.hist.add("周杰伦")
	if _, err := s.app.UpdateSettings(func(st *app.Settings) { st.ShowSearchHistory = false }); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	tt.Click(searchLabel)
	tt.Type("周")

	if tt.HasText("搜索历史") {
		t.Errorf("设置关了还摆历史浮层，文字有 %q", tt.Texts())
	}
	if got := len(s.hist.list()); got != 1 {
		t.Errorf("关掉设置之后历史只剩 %d 条，设置管了存", got)
	}
}

// TestSubmittingSearchRecordsHistory 验回车提交：记进历史、切到搜索页。
func TestSubmittingSearchRecordsHistory(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	tt.Frame()

	tt.Click(searchLabel)
	tt.Type("林俊杰")
	tt.Key(0, mygoui.KeyEnter)

	got := s.hist.list()
	if len(got) != 1 || got[0].Value != "林俊杰" {
		t.Fatalf("回车之后历史是 %v，want 林俊杰", searchValues(got))
	}
	if p := s.router.Path(); p != searchPath {
		t.Errorf("回车之后在 %q，want %q", p, searchPath)
	}
	if s.searchSuggest {
		t.Error("回车之后浮层还开着")
	}
}

// TestSubmittingSearchCarriesTheWordToThePage 验回车之后那个词真的到了搜索页。
//
// 这一条盯的是两半接起来：顶栏把词放在 URL 的 `q` 上（navbar_search.go 的
// submitSearch），搜索页从 URL 上取 `q`（searchpage.go 的 viewSearch）。少任何
// 一半，用户看到的都是「刚敲的词还在顶栏框里，正文却说请输入搜索词」——那句话
// 在屏幕上摆着，一眼就是坏了。
func TestSubmittingSearchCarriesTheWordToThePage(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	tt.Frame()

	tt.Click(searchLabel)
	tt.Type("周杰伦")
	tt.Key(0, mygoui.KeyEnter)

	if got := s.router.Query(searchQuery); got != "周杰伦" {
		t.Fatalf("URL 上的 q 是 %q，want 周杰伦", got)
	}
	if got := s.router.Path(); got != searchPath {
		t.Errorf("提交之后在 %q，want %q", got, searchPath)
	}
	// 页面上画的是「搜了没有结果」那一支，不是「还没输入」那一支——
	// 后者的正文写着「在上面的搜索框里输入歌名或 UP 主」，正是这条要防的。
	// 结果是异步回来的，那一句比提交晚几帧才画出来（理由见 settleText）。
	settleText(t, tt, noSearchResults.Hint)
	if !tt.HasText(noSearchResults.Hint) {
		t.Errorf("页面上没有「没有结果」那一句，文字有 %q", tt.Texts())
	}
	if tt.HasText(searchIdle.Hint) {
		t.Errorf("页面上还是「还没输入」那一支，文字有 %q", tt.Texts())
	}
}

// TestPickingAHistoryFillsTheBoxBack 验点浮层里的一条历史会把词填回顶栏框。
//
// Biu 上游是 `setValue(item.value)`（`search/index.tsx:154`）。不填的话框里留着
// 用户敲的半截字，而页面搜的是这一条——两处对不上。
func TestPickingAHistoryFillsTheBoxBack(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	s.hist.add("林俊杰")
	tt.Frame()

	tt.Click(searchLabel)
	tt.Type("杰")
	if err := tt.Click("林俊杰"); err != nil {
		t.Fatalf("点不到那条历史：%v，文字有 %q", err, tt.Texts())
	}

	if got := s.searchKeyword; got != "林俊杰" {
		t.Errorf("点完之后顶栏框里是 %q，want 林俊杰", got)
	}
	if got := s.router.Query(searchQuery); got != "林俊杰" {
		t.Errorf("URL 上的 q 是 %q，want 林俊杰", got)
	}
}

// TestAvatarMenuHasBiuSixItems 验头像下拉就是 Biu 那六项，顺序也一样。
//
// ⚠️ 菜单是**开的时候才建**的，`Chosen()` 又是在选完之后的下一帧才报
// （`ui/menu.go:76-99`），两次都不在 view 里。想数菜单项，用无头 Tester 的
// `tt.Menu()`——它读的是系统菜单真正摆出来的那一份。
func TestAvatarMenuHasBiuSixItems(t *testing.T) {
	_, tt := newShell(t)
	tt.SetFocused(true)

	if err := tt.Click(avatarLabel); err != nil {
		t.Fatalf("点不到头像：%v，文字有 %q", err, tt.Texts())
	}
	want := []string{"登录", "个人资料", "设置", "刷新数据", "问题反馈", "退出登录"}
	got := tt.Menu()
	if len(got) != len(want) {
		t.Fatalf("头像下拉有 %d 项 %q，want %d 项 %q", len(got), got, len(want), want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("第 %d 项是 %q，want %q（整个是 %q）", i+1, got[i], w, got)
		}
	}
}

// TestAvatarMenuGoesToItsRoute 验菜单项接到了对应的路由。
func TestAvatarMenuGoesToItsRoute(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)

	if err := tt.Click(avatarLabel); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("设置"); err != nil {
		t.Fatal(err)
	}
	if got := s.router.Path(); got != settingsPath {
		t.Errorf("选了「设置」之后在 %q，want %q", got, settingsPath)
	}

	// 「登录」接的是扫码登录页。
	if err := tt.Click(avatarLabel); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("登录"); err != nil {
		t.Fatal(err)
	}
	if got := s.router.Path(); got != loginPath {
		t.Errorf("选了「登录」之后在 %q，want %q", got, loginPath)
	}
}

// TestAvatarMenuProfileUsesTheLoggedInMid 验「个人资料」认的是登录态里那个 mid。
//
// 这一条钉的是「登录态的来源只有 app」：头像这一层不存第二份，mid 一律现去问
// `app.Session()`。存两份的后果是退登之后头像还显示着上一个人的样子，而界面上
// 没有任何一个地方会提示「没退干净」。
func TestAvatarMenuProfileUsesTheLoggedInMid(t *testing.T) {
	s, tt := newLoggedInShell(t, "2")
	tt.SetFocused(true)

	if err := tt.Click(avatarLabel); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("个人资料"); err != nil {
		t.Fatal(err)
	}
	if got := s.router.Path(); got != "/user/2" {
		t.Errorf("选了「个人资料」之后在 %q，want /user/2", got)
	}
}

// TestAvatarMenuWithoutALoginSaysSo 验没登录时那两项说清楚，而不是带用户走进 404。
//
// Biu 那两条是按登录态隐藏掉的。我们六项一直都在（见 navbar_avatar.go 文件头），
// 于是「个人资料」和「退出登录」在没登录时必须有话说。
func TestAvatarMenuWithoutALoginSaysSo(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)

	for _, item := range []string{"个人资料", "退出登录", "刷新数据"} {
		s.clearToasts()
		if err := tt.Click(avatarLabel); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem(item); err != nil {
			t.Fatalf("选 %q 失败：%v", item, err)
		}
		if got := toastTexts(s.visibleToasts()); len(got) != 1 {
			t.Fatalf("选了 %q 之后有 %d 条提示 %q，want 1 条", item, len(got), got)
		}
		if got := s.router.Path(); got != "/" {
			t.Errorf("选了 %q 之后却切到了 %q", item, got)
		}
	}
}

// TestAvatarMenuLogoutActuallyLogsOut 验「退出登录」是真退，不是弹一句话。
func TestAvatarMenuLogoutActuallyLogsOut(t *testing.T) {
	s, tt := newLoggedInShell(t, "2")
	tt.SetFocused(true)

	if !s.app.LoggedIn() {
		t.Fatal("装上去的登录态没生效")
	}
	if err := tt.Click(avatarLabel); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("退出登录"); err != nil {
		t.Fatal(err)
	}
	if s.app.LoggedIn() {
		t.Error("选了「退出登录」之后还是登录态")
	}
}

// TestAvatarMenuOpensTheIssuePage 验「问题反馈」把地址交给系统。
func TestAvatarMenuOpensTheIssuePage(t *testing.T) {
	_, tt := newShell(t)
	tt.SetFocused(true)

	if err := tt.Click(avatarLabel); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("问题反馈"); err != nil {
		t.Fatal(err)
	}
	if got := tt.OpenedURLs(); len(got) != 1 || got[0] != feedbackURL {
		t.Errorf("「问题反馈」打开的是 %q，want [%s]", got, feedbackURL)
	}
}

// TestDynamicDotPollsEveryFiveMinutes 用**假时钟**验那 5 分钟的节奏。
//
// ⚠️ 这里不能 `time.Sleep` 等真的 5 分钟——SPEC 的 Testing Decisions 把假时钟
// 列成每一阶段都要留下的回归证据之一。假表在 navbar_feed.go 的
// `feedPoller.now` 上换。
func TestDynamicDotPollsEveryFiveMinutes(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)

	clock := fakeClockOn(s, fakeClockStart)
	polls := 0
	s.feed.fetch = func() int { polls++; return 0 }

	tt.Frame()
	if polls != 1 {
		t.Fatalf("第一帧轮了 %d 次，want 1（挂上就该先问一次，不是干等 5 分钟）", polls)
	}

	// 还没到点：走多少帧都不该轮。
	clock.Advance(wantFeedInterval - time.Millisecond)
	tt.Frame()
	tt.Frame()
	if polls != 1 {
		t.Fatalf("离 5 分钟还差 1ms 就已经轮了 %d 次，want 1", polls)
	}
	if got := dynamicFeedInterval; got != wantFeedInterval {
		t.Fatalf("轮询间隔是 %v，Biu 的 pollingInterval: 300000 是 %v", got, wantFeedInterval)
	}

	// 到点：轮一次。
	clock.Advance(time.Millisecond)
	tt.Frame()
	if polls != 2 {
		t.Fatalf("到 5 分钟轮了 %d 次，want 2", polls)
	}

	// 再一个周期：又一次，而且只有一次。
	clock.Advance(wantFeedInterval)
	tt.Frame()
	tt.Frame()
	if polls != 3 {
		t.Fatalf("到 10 分钟一共轮了 %d 次，want 3", polls)
	}
}

// TestTheFeedPollerWakesTheWindowWhenTheIntervalElapses 验那一路「到点叫醒界面」
// 的定时器真的在跑，而且 disarm 之后就不响了。
//
// 为什么单独拆一条：这一条验的是**定时器本身**（到点醒一次、每 interval 一次、
// 停了就不响）。它不能写成「窗口静止着，推进时钟，红点自己就亮了」——无头 Tester
// 的 `settle` 只在 `Frame()` / `Click()` 里跑（`ui/headless.go:214`），后台那个
// 定时器调 `Invalidate` 只是把 `requested` 置上，没人驱动就没有帧。所以「定时器
// 醒了 → 那一帧被画出来 → view 问了 tick」这半截在无头里验不了，能验的是前两截。
// 接线那一截（view 每帧都调了 arm）由下面那条盯着。
func TestTheFeedPollerWakesTheWindowWhenTheIntervalElapses(t *testing.T) {
	clock := newFakeClock(fakeClockStart)
	p := newFeedPoller(wantFeedInterval, nil)
	woke := 0
	p.arm(clock, func() { woke++ })

	clock.Advance(wantFeedInterval - time.Millisecond)
	if woke != 0 {
		t.Fatalf("离 5 分钟还差 1ms 就已经叫醒了 %d 次", woke)
	}
	clock.Advance(time.Millisecond)
	if woke != 1 {
		t.Fatalf("到 5 分钟叫醒了 %d 次，want 1", woke)
	}
	clock.Advance(wantFeedInterval)
	if woke != 2 {
		t.Fatalf("到 10 分钟叫醒了 %d 次，want 2", woke)
	}

	p.disarm()
	clock.Advance(wantFeedInterval)
	if woke != 2 {
		t.Errorf("disarm 之后还叫醒了，总数 %d，want 2", woke)
	}
	// 重复 disarm 是安全的（关窗收尾那条路可能走到两次）。
	p.disarm()
}

// TestTheRedDotIsArmedWhileTheWindowIsUp 盯住接线那一截：只要顶栏画着，这一路
// 定时器就得在。
//
// 没有它的话「arm 写了但没人调」就是一个永远绿的测试——而症状（窗口静止时红点不
// 亮）要等五分钟才看得见一次。
func TestTheRedDotIsArmedWhileTheWindowIsUp(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	tt.Frame()

	if s.feed.stop == nil {
		t.Fatal("顶栏画过了，那一路「到点叫醒」的定时器却没装上：" +
			"窗口静止时没有人画帧，于是到点了也没人去轮询")
	}
}

// TestDynamicDotShowsAndClears 验有更新时红点出来，点进去就清掉。// TestDynamicDotShowsAndClears 验有更新时红点出来，点进去就清掉。
func TestDynamicDotShowsAndClears(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)

	clock := fakeClockOn(s, fakeClockStart)
	s.feed.fetch = func() int { return 3 }

	tt.Frame()
	if !tt.HasText("3 条新动态") {
		t.Fatalf("拉到 3 条却没有红点，文字有 %q", tt.Texts())
	}

	if err := tt.Click("动态"); err != nil {
		t.Fatal(err)
	}
	if got := s.router.Path(); got != dynamicFeedPath {
		t.Errorf("点「动态」之后在 %q，want %q", got, dynamicFeedPath)
	}
	if tt.HasText("3 条新动态") {
		t.Errorf("看完动态红点还在，文字有 %q", tt.Texts())
	}

	// 下一个 5 分钟再拉一次，还是 3 条：红点会再出来。
	clock.Advance(wantFeedInterval)
	tt.Frame()
	if !tt.HasText("3 条新动态") {
		t.Errorf("下一轮之后红点没回来，文字有 %q", tt.Texts())
	}
}

func TestSearchClearButtonEmptiesTheField(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	s.hist.add("周杰伦")
	tt.Frame()

	tt.Click(searchLabel)
	tt.Type("周")
	if s.searchKeyword != "周" {
		t.Fatalf("没敲进去，搜索框里是 %q", s.searchKeyword)
	}
	if _, ok := tt.Find("清空"); !ok {
		t.Fatalf("敲了字却没有「清空」，文字有 %q", tt.Texts())
	}

	if err := tt.Click("清空"); err != nil {
		t.Fatalf("点不到「清空」：%v，文字有 %q", err, tt.Texts())
	}
	if s.searchKeyword != "" {
		t.Errorf("清空之后搜索框里还有 %q", s.searchKeyword)
	}
	// 「清空」这个按钮只在框里有字的时候才画（`viewSearchBox` 里那一支）。它没了
	// 就是框里空了——输入框画出来的字是 bound value 驱动的（`ui/textinput.go:53`：
	// 每帧拿 bound value 对一次，不一致就重设编辑器），所以值一空画面跟着空。
	// 这一条断的就是「只清了变量、画面上的字还在」。
	if _, ok := tt.Find("清空"); ok {
		t.Errorf("清空之后「清空」还在画面上，框里的字没清掉，文字有 %q", tt.Texts())
	}
}

// TestSuggestChipDeleteRemovesOnlyThatOne 验浮层上那个 × 只删自己那一条。
//
// Biu 的 Chip 是 `isCloseable`（`search/index.tsx:142-166`），关掉一条和清空
// 全部是两回事：删错了用户要找回来的就是刚搜过的东西。
func TestSuggestChipDeleteRemovesOnlyThatOne(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	s.hist.add("周杰伦")
	s.hist.add("林俊杰")
	tt.Frame()

	tt.Click(searchLabel)
	if !tt.HasText("搜索历史") {
		t.Fatalf("浮层没开，文字有 %q", tt.Texts())
	}
	// 每条 chip 的 × 有自己的无障碍名字（「删除「周杰伦」」），所以点得准。
	if err := tt.Click("删除「周杰伦」"); err != nil {
		t.Fatalf("点不到那一条的删除：%v，文字有 %q", err, tt.Texts())
	}

	got := searchValues(s.hist.list())
	if len(got) != 1 || got[0] != "林俊杰" {
		t.Errorf("删了一条之后剩下 %q，want [林俊杰]", got)
	}
}

// TestSuggestClearAllEmptiesTheHistory 验「清除全部」。
func TestSuggestClearAllEmptiesTheHistory(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	s.hist.add("周杰伦")
	s.hist.add("林俊杰")
	tt.Frame()

	tt.Click(searchLabel)
	if err := tt.Click("清除全部"); err != nil {
		t.Fatalf("点不到「清除全部」：%v，文字有 %q", err, tt.Texts())
	}
	if got := s.hist.list(); len(got) != 0 {
		t.Errorf("清除全部之后还剩 %q", searchValues(got))
	}
	// 一条都没有了，浮层就不该再摆一块空板子。
	if tt.HasText("搜索历史") {
		t.Errorf("清空之后浮层还摆着，文字有 %q", tt.Texts())
	}
}

// TestLeavingTheSearchPageMidTransitionDoesNotCrashTheWindow 验从 /search 离开时
// 不崩。
//
// 这一条是顶栏搜索框**回车之后最顺手的一步**就会踩到的：搜完随手点返回，或者点
// 侧栏第一项。崩起来是整个窗口变成「界面出错了」——不是某一页坏了，是窗口坏了。
//
// 崩在搜索页的 `c.Router().Query(...)`：换页动画还没走完时，**正在离场
// 的那一页**会被重新建一遍，而 MyGo 建离场页时故意不设 `c.router`
// （`mygo/router.go:685`，`Context.Router` 的文档也写着 "or nil outside one"）。
//
// ⚠️ 这一条必须把切页动画**打开**（生产默认就是自动滑动）。`newShell` 为了让
// 截图可复现把它关了，关着的时候离场页根本不重建，这条就永远是绿的——**测试
// 绿了不算验过**。
func TestLeavingTheSearchPageMidTransitionDoesNotCrashTheWindow(t *testing.T) {
	s, tt := newShell(t)
	s.router.Transition = mygoui.TransitionAuto
	tt.SetFocused(true)

	tt.Click(searchLabel)
	tt.Type("周杰伦")
	tt.Key(0, mygoui.KeyEnter)
	if got := s.router.Path(); got != searchPath {
		t.Fatalf("提交之后在 %q，want %q", got, searchPath)
	}

	// 返回按钮：走的是「回到上一页」那条路。
	back, ok := tt.Find(navBackLabel)
	if !ok {
		t.Fatalf("没有后退，文字有 %q", tt.Texts())
	}
	tt.ClickAt(back.X+back.W/2, back.Y+back.H/2)
	if s.crash != nil {
		t.Errorf("点返回之后崩了：%v", s.crash)
	}

	// 侧栏第一项：换的是**另一条**路由，而搜索页仍在历史里躺着，一样要被重建。
	if got := s.router.Path(); got != "/" {
		t.Fatalf("点返回之后在 %q，want /", got)
	}
	if err := tt.Click(searchLabel); err != nil {
		t.Fatal(err)
	}
	tt.Type("林俊杰")
	tt.Key(0, mygoui.KeyEnter)
	// 落点挑的是不挑登录态的那一项，理由见 TestSuggestCollapsesOnBlur。
	if err := tt.Click("本地音乐"); err != nil {
		t.Fatalf("点不到侧栏：%v，文字有 %q", err, tt.Texts())
	}
	if s.crash != nil {
		t.Errorf("点侧栏之后崩了：%v", s.crash)
	}
	if got := s.router.Path(); got != localMusicPath {
		t.Errorf("点侧栏之后在 %q，want %q", got, localMusicPath)
	}
}

// TestTheNavBarPanicIsCaught 验顶栏画崩了，**程序不崩**，而且切到错误页。
//
// ⚠️ 和侧栏那一条一样，这条证明的是**用户看得见的行为**（崩了不死、错误页、
// 有记录），不是哪一层 Guard 接住的——`New()` 里 viewMain 外面已经包了一层。
// navBody 上那层 Guard 是任务要求的写法，靠 code review 守着。
//
// 单独立一条是因为顶栏坏了坏得最显眼：整条顶栏没了，用户先发现的是「窗口
// 拖不动了」。
func TestTheNavBarPanicIsCaught(t *testing.T) {
	s, tt := newShell(t)

	if _, ok := tt.Find(avatarLabel); !ok {
		t.Fatalf("顶栏没画出来，文字有 %q", tt.Texts())
	}
	s.navBody = func(*mygoui.Context) { panic("顶栏画崩了") }
	tt.Frame()

	if s.crash == nil {
		t.Fatal("顶栏 panic 了，外壳没有记住")
	}
	if !strings.Contains(errString(s.crash), "顶栏画崩了") {
		t.Errorf("记住的 panic 是 %v", s.crash)
	}
	// ⚠️ **崩在界面的哪一块也要记住**（这一票要的「出错在哪一步」）：光记住
	// 「顶栏画崩了」分不出是顶栏坏了还是别处坏了把顶栏带崩的。
	if s.crash.where != "顶栏" {
		t.Errorf("记住的崩溃现场是 %q，want「顶栏」", s.crash.where)
	}
	if !tt.HasText("界面出错了") {
		t.Errorf("没有切到错误页，文字有 %q", tt.Texts())
	}
	if !tt.HasText("顶栏画崩了") {
		t.Errorf("错误页上没有崩掉的原因，文字有 %q", tt.Texts())
	}
}

// TestTheTopBarIsBiuHeight 钉住顶栏是 Biu 那个高度。
//
// Biu 的顶栏那一格是 `h-16 flex-none`（`biu/src/layout/index.tsx:32`），也就是
// 64。我们原来是 32（MyGo 例子里的下限，`ui/context.go:334`）。
//
// ⚠️ **为什么这条不只是「好看一点」**：顶栏是**内容区上面那一行**，它矮 32，内容区
// 就少 32——而设置页那一族测试量得着的可视区上下两个数（`settings_page_test.go`
// 的 `pageTop` / `pageBottom`）就是从它推出来的。所以拿 `navMinHeight` 当判据是
// 循环的（常量一改断言跟着改，等于没断言），这里断的是**画面上的高度**。
func TestTheTopBarIsBiuHeight(t *testing.T) {
	tt := navBarTester(t, false)
	// 后退按钮在顶栏最左边，它和右边登录入口之间就是顶栏本身那一行。
	back, ok := tt.Find(navBackLabel)
	if !ok {
		t.Fatalf("顶栏上没有后退，文字有 %q", tt.Texts())
	}
	login, ok := tt.Find("登录")
	if !ok {
		t.Fatalf("顶栏上没有登录入口，文字有 %q", tt.Texts())
	}
	// 无头 Tester 里 `c.TitleBar()` 是零（`ui/headless.go:44` 那张表默认没填），
	// 所以这一帧顶栏画的就是 `navMinHeight` 本身。64 是量出来的，见 navMinHeight。
	const want = 64
	if navMinHeight != want {
		t.Errorf("navMinHeight 是 %d，want %d（Biu 的 h-16）", navMinHeight, want)
	}
	for _, e := range []struct {
		what string
		r    mygoui.Rect
	}{{"后退", back}, {"登录入口", login}} {
		if e.r.Y < 0 || e.r.Y+e.r.H > navMinHeight {
			t.Errorf("%s在 %v，不在顶上那 %d 高里", e.what, e.r, navMinHeight)
		}
	}
	// 那一行要真的占满：登录入口贴着窗口右边。
	if right := login.X + login.W; float32(1200)-right > 32 {
		t.Errorf("登录入口右边缘 %v，离窗口右边还有 %v——顶栏没顶满", right, float32(1200)-right)
	}
}

// TestTheTopBarShowsNoPageTitle 钉住页名不出现两次。
//
// 每一页自己都写着标题（设置 / 下载记录 / 历史记录…），顶栏再写一遍就是同一个
// 词在屏幕上出现两次。上游的顶栏压根没有这一行（`layout/navbar/index.tsx:26-42`
// 的左边只有 `Navigation` + `Search`）——「我在哪」是侧栏高亮那一项的事。
//
// ⚠️ **用 `navBarTester` 而不是整窗**：它 `withView` 只画顶栏那一块，所以
// `Texts()` 里**只有**顶栏上的字。整窗上量的话，侧栏菜单项和页面自己的标题都
// 混在里面，「顶栏有没有」这件事就量不出来了。
func TestTheTopBarShowsNoPageTitle(t *testing.T) {
	// ⚠️ **设置页和搜索页不在这一列里**：顶栏上本来就有一个「设置」键和一个
	// 「搜索」框，它们的无障碍名字和那两页的页名**一模一样**——判据
	// 「顶栏上没有这个名字」在它们身上恒假，于是只能靠人肉分辨「那个是键、
	// 那个是页名」，而那正是这条断言不该干的事。其余的几条都和顶栏上任何一个
	// 键的名字不重名。
	//
	// 另外刻意收了「用户动态」那一页：它的页名**既不在侧栏菜单里、也不在顶栏
	// 任何一个键上**（`Route.InMenu` 是 false），所以改动之前它在顶栏上只有
	// 那一处——它变没变是单值的。
	paths := []string{downloadListPath, historyPath, laterPath, followPath, dynamicFeedPath, collectionMatch}

	b := newTestShell(t, withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.viewNav(c)
		}
	}))
	tt := b.tt
	tt.SetFocused(true)

	for _, p := range paths {
		title := lookup(p).Title
		b.s.navigate(p)
		tt.Frame()
		if tt.HasText(title) {
			t.Errorf("切到 %q 之后顶栏上写着页名 %q——顶栏的文字是 %q。"+
				"页名由那一页自己写，顶栏再写一遍就是屏幕上同一个词出现两次",
				p, title, tt.Texts())
		}
	}
}

// TestThePageStillShowsItsOwnTitle 钉住去掉顶栏那一处之后，标题还在页面上。
//
// 这是上一条**互为反面**的那一半。只断「顶栏上没有」的话，最省事的实现是「谁都
// 别画标题」——上一条会全绿，而用户进到「下载记录」看到的是一片没有名字的页面，
// 和他刚从侧栏点过来的那一项对不上。
//
// ⚠️ **分开画两次，各数一次**：`withView` 把整窗换成只画顶栏 / 只画内容区，
// 于是两边的文字互不掺。「只出现一次」这件事就是这么断的：**顶栏那一侧是 0，
// 内容区那一侧至少是 1**——重复只可能来自顶栏，它归零了，页面上剩下的那份就
// 是唯一的一份。
//
// 侧栏的菜单项**不在这两条数里**，所以「用户动态」那一页也算数：它的页名既不在
// 侧栏菜单里（`InMenu` 是 false），也不和顶栏任何一个键重名。
func TestThePageStillShowsItsOwnTitle(t *testing.T) {
	paths := []string{settingsPath, downloadListPath, historyPath, laterPath, dynamicFeedPath}

	b := newTestShell(t, withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.viewRoutes(c)
		}
	}))
	tt := b.tt

	for _, p := range paths {
		title := lookup(p).Title
		b.s.navigate(p)
		waitFrames(t, tt, p+" 的内容画出来了", func() bool { return b.s.router.Path() == p })
		if !tt.HasText(title) {
			t.Errorf("%q 这一页的内容区里没有页名 %q，文字有 %q", p, title, tt.Texts())
		}
	}
}
