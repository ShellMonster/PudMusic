// 顶栏这一组跑的是 MyGo 的 Tester：和真窗口一模一样的 layout 与输入处理，只是画到
// 内存里。点得到、按得到键、找得到文字，所以「能不能用」能自动验。
//
// ⚠️ 每条都要 `tt.SetFocused(true)`：MyGo 的 `Element.Focused()` 判的是
// `rt.focused == e.id && rt.windowFocus()`（`ui/input.go:1171`），窗口没被
// 标成「在前台」的话它**恒为 false**，于是搜索框的失焦逻辑会每帧都成立。

package ui

import (
	"context"
	"net/url"
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
// 要登录才摆（见 `Route.HideInMenuWhenLoggedOut`），没挂这一份的外壳上那三行压根不在
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

// TestTheTopBarCoversTheContentAreaOnly 验顶栏**只盖内容区那一段**。
//
// 「整条是拖动区」这件事本身无头**验不了**：MyGo 无头宿主的 `startDrag` 是空
// 实现（`ui/headless.go:90`），拖没拖窗口在画面上看不出来。这条能验的是拖动区
// 成立的前提——它得是**内容区顶上那一条**（x ≥ 侧栏宽），否则有一片窗口
// 拖不动；而它**不该**再盖住侧栏那一段（那是侧栏的 logo 和菜单的地方）。
//
// ⚠️ **这一条跟着票 #286 改过**：顶栏原来横贯**整个窗口宽度**（后退键贴在
// 窗口左边 x=8），而现在是上游那种「侧栏通栏 + 顶栏只盖内容区」（上游
// `biu/src/layout/index.tsx:36-47`，报告 C-01）。所以左边那一头量的是
// **内容区的左沿**（侧栏的右沿），不再是窗口的左沿。
func TestTheTopBarCoversTheContentAreaOnly(t *testing.T) {
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
	// 最右边那一格就是头像（它也是未登录时的登录入口），顶栏「顶满」验的是它。
	login := avatar

	if back.Y < 0 || back.Y+back.H > navMinHeight {
		t.Errorf("后退按钮在 %v，不在顶上那 %v 高里", back, navMinHeight)
	}
	if avatar.Y < 0 || avatar.Y+avatar.H > navMinHeight {
		t.Errorf("头像在 %v，不在顶上那 %v 高里", avatar, navMinHeight)
	}
	if login.Y < 0 || login.Y+login.H > navMinHeight {
		t.Errorf("登录入口在 %v，不在顶上那 %v 高里", login, navMinHeight)
	}
	// 左边：**内容区的左沿**（侧栏的右沿），右边：窗口的右边。
	//
	// ⚠️ **左边量的是「顶栏那一组控件顶到内容区左边附近」**，不是「按钮的边缘
	// 正好等于内容区的边缘」：**Tester 只能量到文字/名字那一小块**，量不到按钮
	// 本身（`Find` 给的是标签节点的框，`ui/runtime.go:1056`）。顶栏自己的左
	// 内边距是 8（`viewNav` 的 Padding），而框架那一支后退键自己还有 8 的
	// 内边距（`mygo/ui/widgets.go` 的 `Padding(Space(1.5), Space(2))`）——
	// 所以量到的是 200 + 8 + 8 那一带。
	//
	// 右边留出来的 slack 是：顶栏自己的右内边距（8）加那个控件自己的内边距。
	const slack = 32
	contentLeft := float32(app.DefaultSideMenuWidth)
	if back.X < contentLeft {
		t.Errorf("后退按钮左边缘 %v，在内容区左沿（%v）**左边**——顶栏又盖到侧栏上去了",
			back.X, contentLeft)
	}
	if back.X > contentLeft+slack {
		t.Errorf("后退按钮左边缘 %v，离内容区左沿（%v）有 %v（容差 %v）——它不在顶栏左端",
			back.X, contentLeft, back.X-contentLeft, slack)
	}
	if right := login.X + login.W; float32(1200)-right > slack {
		t.Errorf("登录入口右边缘 %v，离窗口右边还有 %v（容差 %v），顶栏没顶满",
			right, 1200-right, slack)
	}
	if s.router.Path() != "/" {
		t.Errorf("看了一顶栏，路径变成了 %q", s.router.Path())
	}
}

// TestTheTopBarDoesNotReserveRoomForTheWindowControls 钉住顶栏**左边不让出**
// `bar.Left`（票 #286 / 报告 C-01）。
//
// # 为什么单独立一条
//
// `TestTheTopBarCoversTheContentAreaOnly` 量的是「后退键左边缘离内容区左沿不
// 超过 32 像素」，而**无头 Tester 的 TitleBar 是零值**（`mygo/ui/headless.go`
// 那张表默认没填，`bar.Left` 是 0）——于是「让出 `bar.Left+8`」和「不让出」
// 两种写法量到的都是同一个数（X=208），**分不出来**。只有真窗口
// （`bar.Left` = 66）才差那 66 像素。
//
// 所以这里显式装一个 macOS 那样的标题栏进去（`tt.SetTitleBar`，做法照
// `fullplayer_review_test.go` 里那一条），量两遍：装之前和装之后，后退键的
// 左边缘**一个像素都不许动**。
//
// # 断言为什么写成「不许动」而不是「等于某个数」
//
// 「等于某个数」会把这一条变成「顶栏左内边距是 8」那一条的另一份——内边距
// 一改，断言跟着改。而这一条要钉的是**依赖关系**：顶栏左边那 8 像素**不随
// `bar.Left` 变**。「预留」和「不预留」两种写法在这一条上分别是「动了 66」和
// 「没动」，分得开。
func TestTheTopBarDoesNotReserveRoomForTheWindowControls(t *testing.T) {
	_, tt := newShell(t)
	tt.SetFocused(true)

	before, ok := tt.Find(navBackLabel)
	if !ok {
		t.Fatalf("顶栏上没有后退，文字有 %q", tt.Texts())
	}

	// macOS 那三枚窗口按钮在窗口左上角占掉的那一格：真窗口上 66 像素宽。
	//
	// ⚠️ **高度那一栏跟这一条无关**（顶栏高是 `max(bar.Height, navMinHeight)`，
	// 38 那个数够不着 64），写上只是为了让这个标题栏像真窗口的那样。
	const barLeft = float32(66)
	tt.SetTitleBar(mygoui.TitleBar{Height: 38, Left: barLeft})
	tt.Frame()

	after, ok := tt.Find(navBackLabel)
	if !ok {
		t.Fatalf("装上标题栏之后顶栏上没有后退，文字有 %q", tt.Texts())
	}
	if after.X != before.X {
		t.Errorf("装上 bar.Left=%v 的标题栏之后，后退键左边缘从 %v 挪到了 %v（+%v）——"+
			"顶栏**不让出**窗口左上角那一格（票 #286）：macOS 的红黄绿在侧栏那一带，"+
			"跟顶栏压根不在一块，让开它们的是侧栏的品牌块",
			barLeft, before.X, after.X, after.X-before.X)
	}
	// 顺带断一次**绝对位置**：顶栏只盖内容区那一段，后退键仍然贴着内容区的
	// 左沿（侧栏右沿），而不是被推到窗口按钮那一格后面。
	//
	// 这一句是给「整体挪位」兜底的：上面那句只说「没动」，而这一句说「动也不
	// 许多」，两者的差别就在「顶栏整体挪到别处去了」这种改法上。
	const slack = 32
	contentLeft := float32(app.DefaultSideMenuWidth)
	if after.X > contentLeft+slack {
		t.Errorf("装上 bar.Left=%v 之后后退键左边缘是 %v，离内容区左沿（%v）有 %v"+
			"（容差 %v）——它不在顶栏左端", barLeft, after.X, contentLeft,
			after.X-contentLeft, slack)
	}
}

// TestNavBarControlsStillWorkInsideTheDragArea 验拖动区没把里面的控件吃掉。
//
// 这是「整条都是拖动区」真正会出事的地方：命中链上最内层带输入标志的元素才是
// 目标（`ui/input.go:302`），拖动区做得不对，按钮就按不动了。
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
	// 「我的关注」「稍后再看」「历史记录」未登录时不摆（`Route.HideInMenuWhenLoggedOut`），
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
	settleText(t, tt, noSearchResults.Title)
	if !tt.HasText(noSearchResults.Title) {
		t.Errorf("页面上没有「没有结果」那一句，文字有 %q", tt.Texts())
	}
	if tt.HasText(searchIdle.Title) {
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

// TestTheFilledSearchBoxReplacesItsWordWhenYouType 钉住「填上去的那个词，敲字是
// **替换**，不是追加」。
//
// 页内那个输入框 2026-10-22 删掉之后（票 #234），顶栏那个框显示当前这一页搜的
// 词——有的时候那个词是**我们填上去的**（深链接进来的 /search?q=…、翻到另一条
// 历史），不是用户自己敲的。那时候他点进框里敲一个字，期望的是「就搜这个字」，
// 而实际得到的是「旧词 + 新字」（想搜「林」，搜出来的是「周杰伦林」）。
//
// 所以用户**刚拿到焦点**的时候整个词被选上：接着敲的字就把它盖掉了。
//
// ⚠️ **用户自己敲的词不许全选**（那是另一条）：他在框里敲到一半要改，替他全选
// 上等于把他敲的字吃掉。
func TestTheFilledSearchBoxReplacesItsWordWhenYouType(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	// 深链接进搜索页：框里那个词是填上去的。
	s.navigate(searchPath + "?" + searchQuery + "=" + url.QueryEscape("周杰伦"))
	tt.Frame()
	if s.searchKeyword != "周杰伦" {
		t.Fatalf("顶栏框里是 %q，want 周杰伦", s.searchKeyword)
	}

	// 点进框里 → 整个词被选上。
	tt.Click(searchLabel)
	tt.Frame()

	// 敲一个字 → 整个词被盖掉，不是追加。
	tt.Type("林")
	tt.Frame()
	if s.searchKeyword != "林" {
		t.Errorf("敲进去之后框里是 %q，want「林」——填上去的那个词要被**替换**，"+
			"不是追加（想搜「林」却搜出来「周杰伦林」）", s.searchKeyword)
	}
}

// TestClearingTheSearchBoxOnTheSearchPageClearsTheResults 钉住「在搜索页上按那枚
// 「清空」，这一场搜索就没了」。
//
// 页内那个输入框删掉之后（票 #234），「清空」只剩这一个入口。清完框而下面还是
// 「搜索【周杰伦】的结果」和一屏结果——那正是 F-01 要消掉的那种「框和结果对不上」。
func TestClearingTheSearchBoxOnTheSearchPageClearsTheResults(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	s.navigate(searchPath + "?" + searchQuery + "=" + url.QueryEscape("周杰伦"))
	tt.Frame()

	if err := tt.Click("清空"); err != nil {
		t.Fatalf("点不了那枚「清空」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()

	if got := s.router.Location(); strings.Contains(got, "q=") {
		t.Errorf("按了「清空」之后 URL 上还有词：%q——这一场搜索应该没了", got)
	}
	if !tt.HasText(searchIdle.Title) {
		t.Errorf("没有回到「还没输入」那一支，文字有 %q", tt.Texts())
	}
}

// TestAnEmptySearchGoesBackToTheIdleState 钉住「把框里的字删干净再回车 → 回到
// 「还没输入」那一支」。
//
// 页内那个框还在的时候就是这个行为（清空之后回车回到「没搜」）；那个框删掉之后
// 这条路就断了，用户回不到那一支。
func TestAnEmptySearchGoesBackToTheIdleState(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	s.navigate(searchPath + "?" + searchQuery + "=" + url.QueryEscape("周杰伦"))
	tt.Frame()

	tt.Click(searchLabel)
	tt.Key(mygoui.Cmd, mygoui.KeyA) // 全选
	tt.Key(0, mygoui.KeyBackspace)  // 删掉
	tt.Frame()
	if s.searchKeyword != "" {
		t.Fatalf("框里的字没删干净：%q", s.searchKeyword)
	}

	tt.TypeKey(0, mygoui.KeyEnter, "\r")
	tt.Frame()

	if got := s.router.Location(); strings.Contains(got, "q=") {
		t.Errorf("空词回车之后 URL 上还有词：%q", got)
	}
	if !tt.HasText(searchIdle.Title) {
		t.Errorf("空词回车之后没有回到「还没输入」那一支，文字有 %q", tt.Texts())
	}
}

// TestAvatarMenuHasBiuSixItems 验头像下拉就是 Biu 那六项，顺序也一样，
// **而且按登录态隐藏**也照 Biu 那一栏 `hidden`。
//
// ⚠️ **这一层现在是自绘的浮层**（原来是系统菜单），所以数菜单项不用
// `tt.Menu()`——那个读的是系统菜单真正摆出来的那一份，而现在压根没有系统
// 菜单。改成读浮层里的字：浮层自己的名字（`avatarMenuLabel`）之后就是那
// 几项，顺序即 Biu 的顺序。
//
// ⚠️ **Biu 是六项，我们按登录态挑着画**（票 #260，上游
// `user/index.tsx:88-158` 的 `hidden`）：
//
//   - 未登录 → 「登录 / 设置 / 刷新数据 / 问题反馈」（没有「个人资料」、
//     没有「退出登录」）。
//   - 已登录 → 「个人资料 / 设置 / 刷新数据 / 问题反馈 / 退出登录」
//     （没有「登录」）。
//
// ⚠️ **顺序仍然是 Biu 的顺序**：藏掉的那几项不改变其余各项的相对次序。
func TestAvatarMenuHasBiuSixItems(t *testing.T) {
	for _, tc := range []struct {
		name string
		mid  string
		want []string
	}{
		{
			name: "未登录",
			mid:  "",
			want: []string{"登录", "设置", "刷新数据", "问题反馈"},
		},
		{
			name: "已登录",
			mid:  "2",
			want: []string{"个人资料", "设置", "刷新数据", "问题反馈", "退出登录"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				s  *Shell
				tt *mygoui.Tester
			)
			if tc.mid == "" {
				s, tt = newShell(t)
			} else {
				s, tt = newLoggedInShell(t, tc.mid)
			}
			tt.SetFocused(true)

			openAvatarMenu(t, tt)
			got := avatarMenuLabels(tt)
			if len(got) != len(tc.want) {
				t.Fatalf("头像下拉有 %d 项 %q，want %d 项 %q", len(got), got, len(tc.want), tc.want)
			}
			for i, w := range tc.want {
				if got[i] != w {
					t.Errorf("第 %d 项是 %q，want %q（整个是 %q）", i+1, got[i], w, got)
				}
			}
			// ⚠️ **而且它不是系统菜单**：系统菜单是系统外观，跟设计规范不搭，
			// 而换掉它就是这一票要办的事。
			if m := tt.Menu(); len(m) != 0 {
				t.Errorf("头像点开之后弹的是系统菜单 %q——这一票要的是自绘浮层", m)
			}
			_ = s
		})
	}
}

// TestAvatarMenuGoesToItsRoute 验菜单项接到了对应的路由。
func TestAvatarMenuGoesToItsRoute(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)

	openAvatarMenu(t, tt)
	clickAvatarMenuItem(t, s, tt, "设置")
	if got := s.router.Path(); got != settingsPath {
		t.Errorf("选了「设置」之后在 %q，want %q", got, settingsPath)
	}

	// 「登录」接的是扫码登录页。
	openAvatarMenu(t, tt)
	clickAvatarMenuItem(t, s, tt, "登录")
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

	openAvatarMenu(t, tt)
	clickAvatarMenuItem(t, s, tt, "个人资料")
	if got := s.router.Path(); got != "/user/2" {
		t.Errorf("选了「个人资料」之后在 %q，want /user/2", got)
	}
}

// TestAvatarMenuWithoutALoginSaysSo 验没登录时**画出来的那些项**说清楚，
// 而不是带用户走进 404。
//
// ⚠️ **Biu 那两条是按登录态隐藏掉的，我们现在也照它隐藏**（票 #260，上游
// `user/index.tsx:88-158` 的 `hidden` 那一栏）：「个人资料」和「退出登录」
// 在没登录时**压根不画**，于是它们不会「说一句」——它们不在那儿。
//
// ⚠️ **而「刷新数据」两项都在**：它没登录也说得出「还没登录，登录之后才能
// 刷新收藏夹」（那一句在应用层说的，见 `app/favorites_refresh.go`）。
func TestAvatarMenuWithoutALoginSaysSo(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)

	// ⚠️ **先钉住「那两项不画」**：这是这一票改的那件事。
	openAvatarMenu(t, tt)
	for _, hidden := range []string{"个人资料", "退出登录"} {
		for _, label := range avatarMenuLabels(tt) {
			if label == hidden {
				t.Errorf("没登录时头像下拉里画着%q——上游那两项是按登录态藏起来的", hidden)
			}
		}
	}

	for _, item := range []string{"刷新数据"} {
		s.clearToasts()
		// ⚠️ **不要再 `openAvatarMenu` 一次**：浮层已经开着，而那一句是**再点
		// 一下头像**——于是它把浮层关掉了，后面那一下就点了个空处。
		clickAvatarMenuItem(t, s, tt, item)
		if got := toastTexts(s.visibleToasts()); len(got) != 1 {
			t.Fatalf("选了 %q 之后有 %d 条提示 %q，want 1 条", item, len(got), got)
		}
		if got := s.router.Path(); got != "/" {
			t.Errorf("选了 %q 之后却切到了 %q", item, got)
		}
		// ⚠️ **而且没有弹确认框**：没登录就没有可退的。
		if tt.HasText("确认退出登录？") {
			t.Errorf("没登录时选了 %q 也弹了确认框", item)
		}
	}
}

// TestAvatarMenuLogoutActuallyLogsOut 验「退出登录」是真退，不是弹一句话。
//
// ⚠️ **中间多了一步确认**（上游有，我们原来没有）：先问「确认退出登录？」，
// 答「确认」才真的退。
func TestAvatarMenuLogoutActuallyLogsOut(t *testing.T) {
	s, tt := newLoggedInShell(t, "2")
	tt.SetFocused(true)

	if !s.app.LoggedIn() {
		t.Fatal("装上去的登录态没生效")
	}
	openAvatarMenu(t, tt)
	clickAvatarMenuItem(t, s, tt, "退出登录")
	if !tt.HasText("确认退出登录？") {
		t.Fatalf("选了「退出登录」之后没有确认框，文字有 %q", tt.Texts())
	}
	if !s.app.LoggedIn() {
		t.Error("确认框还开着就已经退登了")
	}
	// 答「确认」。
	if err := tt.Click("确认退出登录"); err != nil {
		t.Fatalf("点不到确认键：%v，文字有 %q", err, tt.Texts())
	}
	if s.app.LoggedIn() {
		t.Error("确认之后还是登录态")
	}
}

// TestAvatarMenuOpensTheIssuePage 验「问题反馈」把地址交给系统。
func TestAvatarMenuOpensTheIssuePage(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)

	openAvatarMenu(t, tt)
	clickAvatarMenuItem(t, s, tt, "问题反馈")
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
// 的 `settle` 只在 `Frame()` / `Click()` 里跑（`ui/headless.go:249`），后台那个
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

// TestTheSearchMagnifierSearchesWhatIsInTheBox 验顶栏那枚放大镜**是能点的**
// （票 BMG-253）。
//
// # 为什么这一条重要
//
// 那枚放大镜原先是**纯装饰**：不可点、永远是同一个淡灰色。用户点它只是把光标
// 放进输入框——**结果碰巧对，但界面上没有任何东西说明它是个入口**。
//
// 现在它是**一枚按钮**：点它 = 拿框里的词去搜（和回车是同一件事）。
func TestTheSearchMagnifierSearchesWhatIsInTheBox(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	tt.Frame()

	// 在框里敲一个词（走的是真实输入）。
	box, ok := tt.Find(searchLabel)
	if !ok {
		t.Fatalf("顶栏上没有搜索框，文字有 %q", tt.Texts())
	}
	tt.ClickAt(box.X+box.W/2, box.Y+box.H/2)
	tt.Frame()
	tt.Type("周杰伦")
	tt.Frame()

	// 点那枚放大镜。
	mag, ok := tt.Find(searchActionLabel)
	if !ok {
		t.Fatalf("顶栏上没有那枚放大镜（%q），文字有 %q", searchActionLabel, tt.Texts())
	}
	tt.ClickAt(mag.X+mag.W/2, mag.Y+mag.H/2)
	tt.Frame()

	if got := s.router.Query(searchQuery); got != "周杰伦" {
		t.Errorf("点放大镜之后搜的是 %q，want 周杰伦", got)
	}
	if got := s.router.Path(); got != searchPath {
		t.Errorf("点放大镜之后在 %q，want %q", got, searchPath)
	}
	// 记进历史：它是「一次搜索」，不是「点了一下那个图标」。
	if got := searchValues(s.hist.list()); len(got) != 1 || got[0] != "周杰伦" {
		t.Errorf("点放大镜之后历史是 %q，want [周杰伦]", got)
	}
}

// TestTheClearAllButtonSaysWhatItDeletes 验「清除全部」**说清它删的是什么、能不能
// 撤**（票 BMG-253）。
//
// # 为什么这一条不是「加一句提示」那么轻
//
// 「清除全部」是这一块**唯一的不可逆动作**：它一次性删掉全部搜索历史，而 B 站
// 那边没有「撤销」——删掉的那一条用户自己再也搜不回来。它原先是 12 像素的淡灰
// 字、**一句提示都没有**，用户点了才知道自己删掉了什么。
//
// 判据是**提示的那句话本身**：它既要说清删的是什么（全部搜索历史），也要说清
// 能不能撤（不能）。
func TestTheClearAllButtonSaysWhatItDeletes(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	s.hist.add("周杰伦")
	tt.Frame()
	if err := tt.Click(searchLabel); err != nil {
		t.Fatalf("点不到搜索框：%v", err)
	}
	tt.Frame()

	clear, ok := tt.Find("清除全部")
	if !ok {
		t.Fatalf("浮层上没有「清除全部」，文字有 %q", tt.Texts())
	}
	// 指针停上去，那句提示就该出来（界面层那一份是 0ms 出）。
	tt.Move(clear.X+clear.W/2, clear.Y+clear.H/2)
	tt.Frame()
	if !tt.HasText(clearHistoryTip) {
		t.Errorf("指针停在「清除全部」上没有那句提示（%q），文字有 %q",
			clearHistoryTip, tt.Texts())
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
// （`mygo/ui/router.go:685`，`Context.Router` 的文档也写着 "or nil outside one"）。
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
// Biu 的顶栏那一格是 `h-16 flex-none`（`biu/src/layout/index.tsx:40`），也就是
// 64。我们原来是 32（MyGo 例子里的下限，`ui/context.go:357`）。
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
	login, ok := tt.Find(avatarLabel)
	if !ok {
		t.Fatalf("顶栏上没有头像，文字有 %q", tt.Texts())
	}
	// 无头 Tester 里 `c.TitleBar()` 是零（`ui/headless.go:46` 那张表默认没填），
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
// 词在屏幕上出现两次。上游的顶栏压根没有这一行（`layout/navbar/index.tsx:31-34`
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
// # 为什么这条必须**绕开 router**
//
// ⚠️ **走整窗的话这条断言恒绿**——`Router.View` 结尾有一句 `pg.Label(e.title)`
// （`mygo/ui/router.go:693`）：路由容器的**无障碍名字**就是页名，而
// `Tester.HasText` 认的正是「画着这句字**或者**标着这个名字」的元素。于是那一页
// 哪怕一个字都没画，`HasText("设置")` 也是真的。
//
// 实测（把 `settings_page.go` 里那个 `ui.Text(c, "设置")` 删掉再跑这一条）：
// **照样 PASS**。而首页的 `Find("推荐音乐")` 给回来的是 `{0 0 1200 800}`——
// **整个路由容器**，而首页画面上一个「推荐音乐」都没有。
//
// 所以这里**直接调那一页的 view**，绕开 router 那个容器。绕开之后画面上出现
// 这个名字，就只能是那一页自己画的。
//
// # 为什么用 `Find`（逐字相等）而不用 `HasText`（是子串）
//
// 设置页那四个 Tab 叫「常规设置」「菜单设置」……`HasText("设置")` 认的是子串，
// 删掉页面上那个「设置」它照样是真的——那样这条又是空的。
//
// # 为什么还要断一次高度
//
// 页标题是一行 24px 的字（实测高约 28），而路由容器是整块内容区（高 648）。
// 这一句让「量到的是标题而不是容器」成为断言的一部分，而不是靠「我绕开了
// router」这个前提。
//
// # 断言的到底是哪一个「名字」
//
// 每一页都**自己**把名字画在屏幕上：设置 / 下载记录 / 本地音乐 / 扫码登录是
// 那一行页标题；历史记录 / 稍后再看在没有挂上 B 站接口时画的是「还没接上」那一支
// （`viewLibraryMissing`），而那一支**也写着这一页的名字**。两种都算数——
// 这一条断的是「**这一页在屏幕上有没有名字**」，不是「页标题那一行在不在」。
// 两种都**不是**路由容器那个无障碍标签：那个已经绕开了。
func TestThePageStillShowsItsOwnTitle(t *testing.T) {
	// 每一页自己的画法。**直接调 view，不走 `viewRoutes`**——理由见文件头那一段。
	//
	// ⚠️ **「搜索」不在这一列**：它要从路由上读查询串（`viewSearch` 里的
	// `r.Query(searchQuery)`），而路由那一格正是这里要绕开的东西。
	pages := []struct {
		title string
		draw  func(*Shell) func(*mygoui.Context)
	}{
		{"设置", func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) { s.wake.set(c); s.viewSettings(c, nil) }
		}},
		{"下载记录", func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) { s.wake.set(c); s.viewDownloadList(c, nil) }
		}},
		{"历史记录", func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) { s.wake.set(c); s.viewHistory(c, nil) }
		}},
		{"稍后再看", func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) { s.wake.set(c); s.viewLater(c, nil) }
		}},
		{"本地音乐", func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) { s.wake.set(c); s.viewLocalMusic(c, nil) }
		}},
		{"扫码登录", func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) { s.wake.set(c); s.viewLogin(c, nil) }
		}},
	}

	// 一行标题的高度上限。24px 的字实测约 28 高（见上面那一段），
	// 而整块内容区是几百高——60 把这两者分得很开。
	const titleLineMaxH = 60

	for _, p := range pages {
		t.Run(p.title, func(t *testing.T) {
			b := newTestShell(t, withView(p.draw))
			tt := b.tt
			tt.Frame()
			homeFrame(tt)

			r, ok := tt.Find(p.title)
			if !ok {
				t.Fatalf("%q 这一页上没有页名 %q（画面上的字：%q）——顶栏那一处去掉之后，"+
					"这一页在屏幕上就没有名字了", p.title, p.title, tt.Texts())
			}
			if r.H > titleLineMaxH {
				t.Errorf("%q 这一页上的 %q 是 %.0f 高（y=%.0f）——那不是一行标题，"+
					"是装页面的容器；量到的不是页名", p.title, p.title, r.H, r.Y)
			}
		})
	}
}

// TestTheTopBarHasNoSettingsKey 钉住「顶栏上没有『设置』这一枚」。
//
// 票 #289 / 报告 C-02：上游的设置在**侧栏菜单**和**头像下拉**里
// （`biu/src/layout/navbar/user/index.tsx:107`），**顶栏没有**。我们原来三处
// 都有：侧栏一条、头像下拉一条、顶栏一条——同一个动作三个入口，而它们**会不
// 一致**（侧栏那一条能被「菜单设置」里的「隐藏菜单项」藏掉，另外两处不能）。
//
// 断的是**画面**：顶栏那一块上不得有叫「设置」的东西。
//
// ⚠️ **用 `navBarTester` 而不是整窗**：它 `withView` 只画顶栏那一块，所以
// `Texts()` 里**只有**顶栏上的字。整窗上量的话，侧栏那一项和设置页自己的
// 标题都混在里面，「顶栏有没有」这件事就量不出来了。
func TestTheTopBarHasNoSettingsKey(t *testing.T) {
	tt := navBarTester(t, false)
	if _, ok := tt.Find("设置"); ok {
		t.Errorf("顶栏上还有一枚「设置」——侧栏里有一条、头像下拉里也有第三条，"+
			"同一个动作三个入口，而它们会不一致（顶栏上的字：%q）", tt.Texts())
	}
}

// TestTheTopBarKeepsItsOtherEntries 钉住「这一票只拿掉了『设置』那一枚」。
//
// 报告 C-02 说我们右边比上游多几枚（上游未登录时只有一枚头像）。**多出来的那
// 几枚各有各的理由**，挪不挪是另一票的事（挪入口不是纯对齐，要人拍板）——这一
// 票只动**纯冗余**的那一枚（设置），所以剩下这几枚必须**原样还在**。
//
// ⚠️ **断的是「还在」，不是「只有这些」**：顶栏右边到底该摆几枚，那是人拍板的
// 事；这一条只保证这一票没有顺手把别的也拿掉。
func TestTheTopBarKeepsItsOtherEntries(t *testing.T) {
	tt := navBarTester(t, false)
	for _, label := range []string{"打开文件", "迷你", "动态", avatarLabel} {
		if _, ok := tt.Find(label); !ok {
			t.Errorf("顶栏上找不到 %q（顶栏上的字：%q）——这一票只该拿掉「设置」"+
				"那一枚，别的入口没动", label, tt.Texts())
		}
	}
}

// TestTheSettingsIsStillReachableFromTheSidebarAndTheAvatar 钉住「拿掉顶栏那一枚
// 之后，设置还够得着」。
//
// 顶栏那一枚是**第三个**入口，前两个都还在：侧栏菜单里那一条（我们自己的，
// DESIGN.md §九「侧栏多一条设置」）、头像下拉里那第三条（上游自己的位置）。
//
// ⚠️ **侧栏那一条要认得出是侧栏的**：整窗上「设置」这两个字可能出现在好几个
// 地方，所以这里断的是**位置**——侧栏那一列在左边 200 像素之内。
func TestTheSettingsIsStillReachableFromTheSidebarAndTheAvatar(t *testing.T) {
	_, tt := newShell(t)
	tt.SetFocused(true)

	r, ok := tt.Find("设置")
	if !ok {
		t.Fatalf("画面上找不到「设置」，文字有 %q——设置进不去了", tt.Texts())
	}
	if r.X >= 200 {
		t.Errorf("那个「设置」在 x=%.0f，不在侧栏那一列（200 宽）里——"+
			"顶栏那一枚拿掉之后，侧栏那一条得还在", r.X)
	}

	// 头像下拉里那第三条。
	openAvatarMenu(t, tt)
	got := avatarMenuLabels(tt)
	found := false
	for _, item := range got {
		if item == "设置" {
			found = true
		}
	}
	if !found {
		t.Errorf("头像下拉里没有「设置」了，下拉里是 %q", got)
	}
}

// TestTheNavArrowsAreBiuSizedAndUnbordered 钉住顶栏左边那两个箭头的**盒子**和
// **描边**。
//
// 票 #289 / 报告 C-03：上游那一枚后退是 **32×40、无描边**
// （`biu/src/layout/navbar/navigation/index.tsx:15-24`：`isIconOnly
// variant="flat" radius="md" className="w-8 min-w-8"`；实图逐像素量出来
// x=216..247、y=12..51、底色 `#f7f7f7`、**无描边**）。
//
// 我们原来是 **34×30、带一圈 1 像素描边**——那 2 像素是框架 `ui.Button` 的默认
// 描边（`mygo/ui/widgets.go` 的 `styleButton` 给的是 `t.Border`），而「描边药
// 丸」正是这一轮要收掉的毛病（`flatbutton.go:8-14` 记的就是这个）。
//
// # 为什么描边要断在**像素**上
//
// 「盒子 32×40」量得到（`Tester.Find` 给的是按钮自己的框），「有没有描边」量不
// 到——除非去看**最左边那一列**：
//
//   - **有描边**：最左边那一列是描边色（浅色下 #ebebeb），紧挨着里面那一列是
//     按钮的底色（#f9f9fa）——两个颜色**不一样**。
//   - **无描边**：最左边那一列就是底色，和里面那一列**一样**。
//
// 所以这一条比的是这两列的颜色相不相等。取**中间那一行**（y = 顶 + 一半高）：
// 那一行是直的，不受圆角影响。
//
// ⚠️ **深浅两套都要试**：描边在深色下是「15% 白叠在底色上」那一档，和底色的
// 差比浅色下小（浅色下差 14，深色下差 10），但仍然是两个不同的颜色。
func TestTheNavArrowsAreBiuSizedAndUnbordered(t *testing.T) {
	// 上游那一枚的尺寸：**写死**，不拿常量当判据（常量一改断言跟着改，等于没
	// 断言）。32×40 是逐像素量出来的，见上面那段。
	const wantW, wantH = 32, 40

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt := navBarTester(t, dark)
			img := tt.Image()
			for _, label := range []string{navBackLabel, navForwardLabel} {
				r, ok := tt.Find(label)
				if !ok {
					t.Fatalf("顶栏上没有 %q，文字有 %q", label, tt.Texts())
				}
				if r.W != wantW || r.H != wantH {
					t.Errorf("%q 那一枚是 %.0f×%.0f，want %d×%d（上游那枚是 32×40，"+
						"无描边）", label, r.W, r.H, wantW, wantH)
				}
				// 最左边那一列 vs 紧挨着里面那一列。
				x := round32(r.X)
				y := round32(r.Y + r.H/2)
				edge := img.RGBAAt(clampX(img, x), clampY(img, y))
				inside := img.RGBAAt(clampX(img, x+1), clampY(img, y))
				if edge != inside {
					t.Errorf("%q 那一枚最左边那一列是 %v、紧挨着里面那一列是 %v——"+
						"那一圈 1 像素的描边还在（上游那枚无描边，框 %v）",
						label, edge, inside, r)
				}
			}
		})
	}
}
