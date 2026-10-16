// 顶栏这一组跑的是 MyGo 的 Tester：和真窗口一模一样的 layout 与输入处理，只是画到
// 内存里。点得到、按得到键、找得到文字，所以「能不能用」能自动验。
//
// ⚠️ 每条都要 `tt.SetFocused(true)`：MyGo 的 `Element.Focused()` 判的是
// `rt.focused == e.id && rt.windowFocus()`（`ui/input.go:1048`），窗口没被
// 标成「在前台」的话它**恒为 false**，于是搜索框的失焦逻辑会每帧都成立。

package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// newShellWithSettings 建一个挂了设置的外壳。
//
// 设置里的「显示搜索历史」得先挂上才改得动：`app.SettingsStore` 没挂上时
// `Update` 直接返回 ErrSettingsClosed（见 internal/app/settings_mount.go）。
func newShellWithSettings(t *testing.T) (*Shell, *mygoui.Tester) {
	t.Helper()
	a := app.New()
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})
	s := New(a)
	t.Cleanup(s.images.Close)
	t.Cleanup(func() { _ = s.hist.Close() })
	return s, mygoui.NewTester(s.View, 1200, 800)
}

// fakeClockStart 是那只假表从哪儿起算。
//
// 挑一个整点：这一组断言里出现的时刻全都能手算出来，看日志时不用去猜
// 「0 基点是哪一刻」。
var fakeClockStart = time.Date(2026, time.October, 16, 9, 0, 0, 0, time.UTC)

// TestNavBarSpansTheTopOfTheWindow 验顶栏真的横贯窗口顶上的一条。
//
// 「整条是拖动区」这件事本身无头**验不了**：MyGo 无头宿主的 `startDrag` 是空
// 实现（`ui/headless.go:88`），拖没拖窗口在画面上看不出来。这条能验的是拖动区
// 成立的前提——它得是**顶上那一条、并且横贯整个窗口宽度**，否则有一片窗口
// 拖不动。
func TestNavBarSpansTheTopOfTheWindow(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)

	back, ok := tt.Find("Back")
	if !ok {
		t.Fatalf("顶栏上没有后退，文字有 %q", tt.Texts())
	}
	avatar, ok := tt.Find(avatarLabel)
	if !ok {
		t.Fatalf("顶栏上没有头像，文字有 %q", tt.Texts())
	}

	if back.Y < 0 || back.Y+back.H > navMinHeight {
		t.Errorf("后退按钮在 %v，不在顶上那 %v 高里", back, navMinHeight)
	}
	if avatar.Y < 0 || avatar.Y+avatar.H > navMinHeight {
		t.Errorf("头像在 %v，不在顶上那 %v 高里", avatar, navMinHeight)
	}
	// 左边贴着窗口左边、右边贴着窗口右边 = 这一条横贯整个宽度。
	if back.X < 8 {
		t.Errorf("后退按钮左边缘 %v，快贴到窗口外了", back.X)
	}
	if right := avatar.X + avatar.W; float32(1200)-right > 16 {
		t.Errorf("头像右边缘 %v，离窗口右边还有 %v，顶栏没顶满", right, 1200-right)
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

	if err := tt.Click("Back"); err != nil {
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
	if got := s.searchQuery; got != "周" {
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
	// 点侧栏的「我的关注」：那是侧栏里**独一份**的文字（顶栏的页名上也有一个
	// 「推荐音乐」，`Find` 只会认到先画的那一个）。别点「打开文件」——它会拉
	// 系统文件框。
	if err := tt.Click("我的关注"); err != nil {
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

	// 个人资料要认得出登录态：没登录就没有 mid，给一条 `/user/` 等于把用户
	// 送进 404，所以那一支该说「还没登录」。
	s.user = navUser{loggedIn: true, mid: "2"}
	tt.Frame()
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

// TestAvatarMenuSaysWhatIsNotDoneYet 验做不了的菜单项说清楚，而不是没反应。
//
// 「问题反馈」是唯一真能做的：它把地址交给系统（`Context.OpenURL`）。
func TestAvatarMenuSaysWhatIsNotDoneYet(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)

	for _, want := range []string{"刷新数据", "退出登录", "登录"} {
		s.clearToasts()
		if err := tt.Click(avatarLabel); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem(want); err != nil {
			t.Fatalf("选 %q 失败：%v", want, err)
		}
		if got := toastTexts(s.visibleToasts()); len(got) != 1 {
			t.Fatalf("选了 %q 之后有 %d 条提示 %q，want 1 条", want, len(got), got)
		}
	}

	s.clearToasts()
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

	now := fakeClockStart
	polls := 0
	s.feed.now = func() time.Time { return now }
	s.feed.fetch = func() int { polls++; return 0 }
	// newShell 建出来的时候已经画过一帧了，那一帧用的是真时钟。把「下一次该
	// 轮的时刻」拨回零值，下面第一帧才是干净的「挂上就该先问一次」。
	s.feed.next = time.Time{}

	tt.Frame()
	if polls != 1 {
		t.Fatalf("第一帧轮了 %d 次，want 1（挂上就该先问一次，不是干等 5 分钟）", polls)
	}

	// 还没到点：走多少帧都不该轮。
	now = fakeClockStart.Add(dynamicFeedInterval - time.Millisecond)
	tt.Frame()
	tt.Frame()
	if polls != 1 {
		t.Fatalf("离 5 分钟还差 1ms 就已经轮了 %d 次，want 1", polls)
	}

	// 到点：轮一次。
	now = fakeClockStart.Add(dynamicFeedInterval)
	tt.Frame()
	if polls != 2 {
		t.Fatalf("到 5 分钟轮了 %d 次，want 2", polls)
	}

	// 再一个周期：又一次，而且只有一次。
	now = fakeClockStart.Add(2 * dynamicFeedInterval)
	tt.Frame()
	tt.Frame()
	if polls != 3 {
		t.Fatalf("到 10 分钟一共轮了 %d 次，want 3", polls)
	}
}

// TestDynamicDotShowsAndClears 验有更新时红点出来，点进去就清掉。
func TestDynamicDotShowsAndClears(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)

	now := fakeClockStart
	s.feed.now = func() time.Time { return now }
	s.feed.fetch = func() int { return 3 }
	s.feed.next = time.Time{}

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
	now = now.Add(dynamicFeedInterval)
	tt.Frame()
	if !tt.HasText("3 条新动态") {
		t.Errorf("下一轮之后红点没回来，文字有 %q", tt.Texts())
	}
}

// TestSuggestCollapsesWhenSelectingOne 也是 KeepFocus 那一条的回归证据：
// 少了面板上的 KeepFocus，点历史那一条会先把焦点抢到按钮上，「失焦就收」
// 那一支在按下那一帧就成立，浮层在 Children 之前就没了，那一条根本点不到
// （实测：选中变成了「停在首页」）。摘掉 KeepFocus 这一条就会红。
func TestSearchClearButtonEmptiesTheField(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	s.hist.add("周杰伦")
	tt.Frame()

	tt.Click(searchLabel)
	tt.Type("周")
	if s.searchQuery != "周" {
		t.Fatalf("没敲进去，搜索框里是 %q", s.searchQuery)
	}
	if _, ok := tt.Find("清空"); !ok {
		t.Fatalf("敲了字却没有「清空」，文字有 %q", tt.Texts())
	}

	if err := tt.Click("清空"); err != nil {
		t.Fatalf("点不到「清空」：%v，文字有 %q", err, tt.Texts())
	}
	if s.searchQuery != "" {
		t.Errorf("清空之后搜索框里还有 %q", s.searchQuery)
	}
	// 「清空」这个按钮只在框里有字的时候才画（`viewSearch` 里那一支）。它没了
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
	if !tt.HasText("界面出错了") {
		t.Errorf("没有切到错误页，文字有 %q", tt.Texts())
	}
	if !tt.HasText("顶栏画崩了") {
		t.Errorf("错误页上没有崩掉的原因，文字有 %q", tt.Texts())
	}
}
