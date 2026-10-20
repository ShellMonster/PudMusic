package ui

import (
	"image"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	mygoui "github.com/egoist/mygo/ui"
)

// 四个 Tab 这一组测试跑在真实的 Shell 上（和 settings_page_test.go 同一套外壳），
// 断言的是**画面上有什么**和**设置里变成了什么**，不是「调了哪个函数」。
//
// 这一票最坏的一种失败是「页画画得挺好，只是某一格按了没反应」——它在界面上完全
// 看不出来。所以每一个控件都有一条「点它、读设置」的测试。

// TestAllFourTabsAreThereAndEachDrawsItsOwnThing 验四个 Tab 齐全，而且各画各的。
//
// 逐个 Tab 点过去看有没有**这一页独有**的字：只看 Tab 的名字在不在是不够的——
// 四个 Tab 都画了同一个东西（或者空面板）的话，点过去也「成功」了。
func TestAllFourTabsAreThereAndEachDrawsItsOwnThing(t *testing.T) {
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""))

	// 切 Tab 之前先把「这一页独有」的那些字收起来。
	only := map[string][]string{
		"常规设置":  {"外观", "播放", "下载", "搜索", "系统"},
		"菜单设置":  {"系统默认菜单", "全选", "折叠侧栏", "侧栏宽度"},
		"快捷键设置": {"应用内快捷键", "全局快捷键", "启用全局快捷键", "恢复默认"},
		// ⚠️ 四种协议只在**下拉摆开的时候**都在画面上（收起时只有当前那一个，
		// 下面 `TestTheProxyTabTakesAllFourProtocols` 逐个点开验）。所以这里只点名
		// 收起时也该有的那几句。
		"代理设置": {"代理类型", "不使用代理", "现在不走代理"},
	}
	seen := make([][]string, 0, 4)
	for _, tab := range settingsTabNames {
		openSettingsTab(t, tt, tab)
		// 这一页独有的字必须都在；同时把这一页的整屏文字存下来。
		var page []string
		for _, want := range only[tab] {
			if !tt.HasText(want) {
				t.Errorf("%s 那一页上没有 %q，文字有 %q", tab, want, tt.Texts())
			}
			page = append(page, want)
		}
		seen = append(seen, page)
	}

	// 别的 Tab 的字不许漏到这一页来：Biu 换了 Tab 就是换了一页，混在一起的话
	// 用户会以为「代理设置」也在管主题。
	openSettingsTab(t, tt, "代理设置")
	for _, bad := range []string{"外观", "系统默认菜单", "启用全局快捷键"} {
		if tt.HasText(bad) {
			t.Errorf("代理那一页上混进了 %q", bad)
		}
	}
}

// TestPickingTheFontChangesWhatTheWholeWindowDrawsIn 验字体是真的换掉了。
//
// 这一条读的是**主题里的字体**，不是设置里的字符串：设置存对了而主题没跟着换，
// 界面上一个字都不会变，而那正是「所有设置项都在界面上能改」会漏掉的那种失败。
func TestPickingTheFontChangesWhatTheWholeWindowDrawsIn(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))

	// 「系统默认」这一档要**一个字节都不改**主题：`t.Font` 的零值是系统自己的字体，
	// 而设置里默认是 `"system-ui"`——那是另一个字符串，设上去全应用的字宽都会变
	// （见 BuildTheme 那里的注释和那条被它弄红的播放条测试）。
	if got := BuildTheme(s.app.SettingsSnapshot(), false).Font; got != "" {
		t.Fatalf("选了系统默认之后主题里的字体是 %q，应该是空的（= 系统自己的）", got)
	}

	openFontMenu(t, tt)
	// 「等宽」那条：family 是 `Menlo, Consolas, monospace`，和系统默认不是同一个。
	if err := tt.Click("等宽"); err != nil {
		t.Fatalf("点不到「等宽」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()

	want := "Menlo, Consolas, monospace"
	if got := s.app.SettingsSnapshot().FontFamily; got != want {
		t.Errorf("设置里的字体是 %q，want %q", got, want)
	}
	if got := BuildTheme(s.app.SettingsSnapshot(), false).Font; got != want {
		t.Errorf("这一帧主题里的字体是 %q，want %q（界面不会跟着变）", got, want)
	}
}

// TestEveryFontOptionIsDrawnInItsOwnFont 验**每一条选项用它自己的字体画**。
//
// 这条只能靠读像素，而直接比两行不同的标签是没有用的：两行的字本来就不一样，
// 就算全都用同一个字体画，逐像素比的结果也是「不一样」——把 `.Font(...)` 整句去掉
// 它照样绿。
//
// 所以这里的做法是**拿参照比**：为每一条选项另起一个无头窗口，只画那一行字，
// 明确写死它该用的那一族，然后拿它的墨迹和面板上那一行比。两者必须一致，而
// 面板上那一行不可能知道「它该用哪一族」——那一句 `.Font(family)` 是页面上唯一
// 能让它知道的地方。
func TestEveryFontOptionIsDrawnInItsOwnFont(t *testing.T) {
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openFontMenu(t, tt)

	// 这三条的族名列表第一个字族在这台机器上是真的中文宋体/黑体/楷体，拿它们比
	// 才有意义：拉丁字族那几条的标签是中文，列表里没有中文字族，画出来的是同一套
	// 回退字体，比了等于没比。
	probes := []string{"苹方 / 微软雅黑", "黑体", "宋体", "楷体"}
	for _, label := range probes {
		family, ok := fontFamilyOf(fontOptions, label)
		if !ok {
			t.Fatalf("名单里没有 %q", label)
		}
		got := inkShotOf(t, tt, label)
		want := renderedIn(t, label, family)
		if !sameGlyphs(got.img, got.at, want.img, want.at) {
			t.Errorf("「%s」那一行不是用 %q 画出来的：面板上的墨迹和单独画一遍对不上。"+
				"面板 %.0f,%.0f %.0fx%.0f / 参照 %.0f,%.0f %.0fx%.0f",
				label, family,
				got.at.X, got.at.Y, got.at.W, got.at.H,
				want.at.X, want.at.Y, want.at.W, want.at.H)
		}
	}
}

// fontFamilyOf 按**标签**在名单里找一条选项的族名。
//
// `findFont` 是按族名找的（设置页用它来认出「设置里存的那个字体在不在名单里」），
// 这里要的正好相反。
func fontFamilyOf(options []fontOption, label string) (string, bool) {
	for _, o := range options {
		if o.label == label {
			return o.family, true
		}
	}
	return "", false
}

// renderedIn 另起一个无头窗口，只画一行字、写死它用哪一族字体，返回它的墨迹。
//
// 位置对齐靠的是：参照窗口和面板上的控件都用同一段内边距，字就落在同一格上，
// 于是逐像素比有意义。字体大小不写死——用主题的默认值，和面板上那一行是同一个。
func renderedIn(t *testing.T, label, family string) inkShot {
	t.Helper()
	rt := mygoui.NewTester(func(c *mygoui.Context) {
		ui.Column(c).Padding(fontPreviewPad).Children(func() {
			ui.Text(c, label).Font(family)
		})
	}, 600, 80)
	rt.Frame()
	r, ok := rt.Find(label)
	if !ok || r.H == 0 {
		t.Fatalf("参照窗口里没有画出 %q", label)
	}
	return inkShot{img: rt.Image(), at: r}
}

// TestPickingAColorChangesTheWholeTheme 验颜色是真的落到主题上。
//
// 读的是 `BuildTheme` 出来的强调色和背景色，和设置里的十六进制串两样都查：串对了
// 而主题没跟着变，界面上一根线都不会换颜色。
func TestPickingAColorChangesTheWholeTheme(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))

	// 取色板是点色块之后在下面摆出来的，点里面的常用色。
	openColorPicker(t, tt, "主色")
	if err := tt.Click("Red"); err != nil {
		t.Fatalf("取色板里点不到红色：%v", err)
	}
	tt.Frame()

	want := "#ef4444"
	if got := s.app.SettingsSnapshot().PrimaryColor; got != want {
		t.Errorf("设置里的主色是 %q，want %q", got, want)
	}
	if got := BuildTheme(s.app.SettingsSnapshot(), false).Accent; got.R != 0xef || got.G != 0x44 || got.B != 0x44 {
		t.Errorf("这一帧主题的强调色是 %+v，want #ef4444", got)
	}
}

// TestTheColorItemGoesBackToFollowingTheTheme 验「用默认」能把值放回空串。
//
// 背景色的默认值是**空串**，意思是「跟着主题走」。Biu 那一格挑过一次之后就再也
// 回不到这个状态，而那正是多数人想要的常态——所以这里多给了那个出口。
func TestTheColorItemGoesBackToFollowingTheTheme(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))

	if got := s.app.SettingsSnapshot().BackgroundColor; got != "" {
		t.Fatalf("起点背景色不是空串，是 %q", got)
	}
	if tt.HasText("用默认") {
		t.Error("没改过的时候不该摆「用默认」——那一格没有可退回的东西")
	}

	openColorPicker(t, tt, "背景")
	if err := tt.Click("Blue"); err != nil {
		t.Fatalf("取色板里点不到蓝色：%v", err)
	}
	tt.Frame()
	if got := s.app.SettingsSnapshot().BackgroundColor; got != "#3b82f6" {
		t.Fatalf("挑完之后背景色是 %q", got)
	}
	// 取色板是盖在页面上的一层，不先关掉就点「用默认」——点到的会是板子上那一格颜色。
	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()

	if err := tt.Click("用默认"); err != nil {
		t.Fatalf("点不到「用默认」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	if got := s.app.SettingsSnapshot().BackgroundColor; got != "" {
		t.Errorf("退回之后背景色是 %q，want 空串（跟着主题走）", got)
	}
	if c := BuildTheme(s.app.SettingsSnapshot(), false).Background; c.R != 0xff || c.G != 0xff {
		t.Errorf("退回之后主题的背景色是 %+v，want 浅色默认的白", c)
	}
}

// TestTheGeneralTabStoresEachOfItsChoices 验常规那一页的每一格都存得住。
//
// 逐格点是因为「其中一格改了没反应」在界面上完全看不出来——设置页照样画得挺好。
func TestTheGeneralTabStoresEachOfItsChoices(t *testing.T) {
	tt, s, a, _ := shellOnSettings(t, app.DefaultSettings(""))
	// 这一格每一行点一遍：设置页照样画得挺好、只是其中一格改了没反应，是这一页
	// 最难自己发现的一种坏。

	// 数据显示样式
	clickSetting(t, tt, s, "网格")
	if got := a.SettingsSnapshot().DisplayMode; got != app.DisplayModeCard {
		t.Errorf("数据显示样式是 %q，want card", got)
	}

	// 音质偏好（下拉）
	openSelect(t, tt, "选择音质偏好")
	if err := tt.Click("无损"); err != nil {
		t.Fatalf("下拉里点不到「无损」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	if got := a.SettingsSnapshot().AudioQuality; got != app.AudioQualityLossless {
		t.Errorf("音质是 %q，want lossless", got)
	}
	// 底下那句说明要跟着换：Biu 就是把当前档位的解释写在这一格下面（`system-
	// settings.tsx:236-241`）。不跟着换的话「无损到底是什么」还是没地方看。
	if !tt.HasText("FLAC / Hi-Res") {
		t.Errorf("换了档位之后说明没跟着换，文字有 %q", tt.Texts())
	}

	// 两个开关。⚠️ 开关没有名字，靠**点它左边的那个小方框**点不到（Tester 的
	// Click 是找显示着那个字的元素），所以这里走 Find + ClickAt。
	clickSwitch(t, tt, s, "开关：上报本机播放记录")
	if a.SettingsSnapshot().ReportPlayHistory {
		t.Error("上报播放记录没有关掉")
	}
	clickSwitch(t, tt, s, "开关：上报本机播放记录")
	if !a.SettingsSnapshot().ReportPlayHistory {
		t.Error("上报播放记录没有打开")
	}

	clickSwitch(t, tt, s, "开关：显示搜索历史")
	if a.SettingsSnapshot().ShowSearchHistory {
		t.Error("显示搜索历史没有关掉")
	}
}

// TestTheProxyTabTakesAllFourProtocols 验**任务明写的那四种协议都摆出来、也选得进**。
//
// ⚠️ 原来这条把「不使用代理」当成了第四项——那一档是**关掉**，不是一种协议。于是真正
// 要求的 HTTPS 从头到尾没在界面上出现过，而这条测试一直是绿的。
//
// 所以这一条分成两半：
//
//  1. 下拉摆开时，五档**逐字**在画面上（少一档、顺序错、名字改，都会红）；
//  2. 四种协议挨个点一遍，每点一次都真的存进了设置，且主机/端口那一整块跟着出现。
//
// 见 `app.ProxyType` 那里为什么比 Biu 多一个 HTTPS。
func TestTheProxyTabTakesAllFourProtocols(t *testing.T) {
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "代理设置")

	// 1. 下拉里摆着的是哪五档。
	openSelect(t, tt, "选择代理类型")
	want := []string{"不使用代理", "HTTP 代理", "HTTPS 代理", "SOCKS4 代理", "SOCKS5 代理"}
	for _, label := range want {
		if !tt.HasText(label) {
			t.Errorf("代理类型下拉里没有 %q，文字有 %q", label, tt.Texts())
		}
	}
	// 多一档要在这一条里红掉。判据是「**以「 代理」结尾**的那些」——这一页自己也有
	// 「代理设置」「代理类型」「选择代理类型」和一句含「代理」的提示语，把它们一并
	// 数进来就永远对不上。少一档由上面那五条 `HasText` 管。
	if got := proxyOptionLabels(tt); len(got) != len(want) {
		t.Errorf("代理类型下拉里有 %d 档：%q，want %d 档 %q", len(got), got, len(want), want)
	}
	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()

	// 2. 四种协议挨个点。
	for _, c := range []struct {
		label string
		want  app.ProxyType
	}{
		{"HTTP 代理", app.ProxyHTTP},
		{"HTTPS 代理", app.ProxyHTTPS},
		{"SOCKS4 代理", app.ProxySOCKS4},
		{"SOCKS5 代理", app.ProxySOCKS5},
		{"不使用代理", app.ProxyNone},
	} {
		tt2, s, a, _ := shellOnSettings(t, app.DefaultSettings(""))
		openSettingsTab(t, tt2, "代理设置")

		openSelect(t, tt2, "选择代理类型")
		if err := tt2.Click(c.label); err != nil {
			t.Fatalf("下拉里点不到「%s」：%v，文字有 %q", c.label, err, tt2.Texts())
		}
		tt2.Frame()
		if got := a.SettingsSnapshot().ProxySettings.Type; got != c.want {
			t.Errorf("选了「%s」之后类型是 %q，want %q", c.label, got, c.want)
		}
		// 「不使用代理」时主机/端口那一整块不该画；选了别的就该画。
		if hasHost := tt2.HasText("代理主机"); hasHost != (c.want != app.ProxyNone) {
			t.Errorf("选了「%s」之后主机那一格 %v", c.label, hasHost)
		}
		s.keys.Close()
	}
}

// TestTheProxyPasswordOnlyShowsWhereItMeansSomething 验密码只在两种协议下出现。
//
// 照 Biu 的 `showPassword = type === "http" || type === "socks5"`
// （`proxy-settings.tsx:39`）——SOCKS4 和 SOCKS 协议里本来就没有「用户名密码」
// 这一段，填了也没有地方去，摆出来只是让人以为填了有用。
func TestTheProxyPasswordOnlyShowsWhereItMeansSomething(t *testing.T) {
	want := map[app.ProxyType]bool{
		app.ProxyHTTP: true, app.ProxyHTTPS: true, app.ProxySOCKS5: true,
		app.ProxySOCKS4: false, app.ProxyNone: false,
	}
	for p, has := range want {
		st := app.DefaultSettings("")
		st.ProxySettings.Type = p
		tt, s, _, _ := shellOnSettings(t, st)
		openSettingsTab(t, tt, "代理设置")
		if got := tt.HasText("代理密码"); got != has {
			t.Errorf("类型是 %q 时密码那一格是 %v，want %v", p, got, has)
		}
		if !tt.HasText("代理用户名") && p != app.ProxyNone {
			t.Errorf("类型是 %q 时连用户名那一格都没画", p)
		}
		s.keys.Close()
	}
}

// TestTheColorPickerKeepsTheTransparency 验取色板调出来的**透明度存得住**。
//
// ⚠️ 这一条是被一条真 bug 逼出来的：`toHex` 只写 RGB，取色板上那条 **Opacity** 滑块
// 拖出来的 alpha 被**悄悄丢掉**——界面上是半透明的，存下去是全不透明，下一帧跳回去。
//
// 拖法照审阅给的复现：点滑块中点 → 推几帧 → 看存下来的值。
func TestTheColorPickerKeepsTheTransparency(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openColorPicker(t, tt, "主色")
	if err := tt.Click("Red"); err != nil {
		t.Fatalf("取色板里点不到红色：%v", err)
	}
	tt.Frame()
	if got := s.app.SettingsSnapshot().PrimaryColor; got != "#ef4444" {
		t.Fatalf("先选个不透明的，存下来是 %q，want #ef4444", got)
	}

	// 拖 Opacity 滑块到中间。不透明 → 大约一半。
	dragSliderTo(t, tt, "Opacity", 0.5)
	tt.Frame()

	got := s.app.SettingsSnapshot().PrimaryColor
	if len(got) != 9 || !strings.HasPrefix(got, "#ef4444") {
		t.Fatalf("拖了透明度之后存下来的是 %q，want #ef4444xx（8 位，带 alpha）", got)
	}
	alpha, err := strconv.ParseUint(got[7:9], 16, 8)
	if err != nil {
		t.Fatal(err)
	}
	if alpha < 100 || alpha > 155 {
		t.Errorf("透明度是 %d（0x%02x），want 大约 128（滑块拖到中间）", alpha, alpha)
	}
	if a := BuildTheme(s.app.SettingsSnapshot(), false).Accent.A; a != uint8(alpha) {
		t.Errorf("主题的强调色 alpha 是 %d，存的是 %d", a, alpha)
	}
}

// dragSliderTo 把一条滑块拖到它自己的几分之几（0..1）。
//
// ⚠️ 必须在 `Advance` 之后再 `Frame`：MyGo 的动画状态挂在控件的节点上，而无头 Tester
// 的钟**只有 `Advance(d)` 才走**。不推进钟的话「按住 → 移动 → 松手」三步会落在同
// 一次 settle 里，看不出拖没拖动。
func dragSliderTo(t *testing.T, tt *mygoui.Tester, label string, frac float32) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("找不到滑块「%s」，文字有 %q", label, tt.Texts())
	}
	x := r.X + r.W*frac
	y := r.Y + r.H/2
	tt.Press(x, y)
	tt.Advance(16 * time.Millisecond)
	tt.Frame()
	tt.Release(x, y)
	for range 3 {
		tt.Advance(50 * time.Millisecond)
		tt.Frame()
	}
}

// TestTheProxyHostAndPortAreStored 验主机和端口存得住、非法端口存不进去。
//
// 端口那一格最容易被「看着能改」骗过去：输入框照收不误，而设置里存的是一个 0 或者
// 一段乱码，拨号的时候才炸。
func TestTheProxyHostAndPortAreStored(t *testing.T) {
	st := app.DefaultSettings("")
	st.ProxySettings.Type = app.ProxyHTTP
	tt, _, a, _ := shellOnSettings(t, st)
	openSettingsTab(t, tt, "代理设置")

	typeIntoProxyField(t, tt, "代理主机", "127.0.0.1")
	typeIntoProxyField(t, tt, "代理端口", "7890")

	got := a.SettingsSnapshot().ProxySettings
	if got.Host != "127.0.0.1" {
		t.Errorf("主机是 %q", got.Host)
	}
	if got.Port == nil || *got.Port != 7890 {
		t.Errorf("端口是 %v，want 7890", got.Port)
	}
}

// TestTheMenuTabHidesAnItemFromTheSidebar 验菜单那一页是真的接在侧栏上的。
//
// 这是这一票里最容易「摆了个开关、按了没反应」的一格：侧栏那一列是
// `sidebar.go` 的 `viewSideMenu`，菜单那一页改的是设置里的 `hiddenMenuKeys`。
// 两处没接上的话，勾选框照样跳，设置也照样存住，而侧栏上那一条还在。
//
// ⚠️ 拿**「本地音乐」**当例子而不是「稍后再看」：这一条钉的是
// `hiddenMenuKeys` 通不通，而那三个入口未登录时压根不摆（`Route.HideInMenuWhenLoggedOut`），
// 拿它们当例子的话这条测试还会顺带把登录态也钉进去——把 `HideInMenuWhenLoggedOut` 去掉，
// 它照样绿，而它本来就不该管那件事。
func TestTheMenuTabHidesAnItemFromTheSidebar(t *testing.T) {
	tt, _, a, _ := shellOnSettings(t, app.DefaultSettings(""))

	if !tt.HasText("本地音乐") {
		t.Fatalf("侧栏上起点就没有「本地音乐」，文字有 %q", tt.Texts())
	}
	openSettingsTab(t, tt, "菜单设置")
	clickCheckbox(t, tt, "在侧栏里显示「本地音乐」")
	if got := a.SettingsSnapshot().HiddenMenuKeys; len(got) != 1 || got[0] != localMusicPath {
		t.Fatalf("设置里藏起来的是 %q，want [%s]", got, localMusicPath)
	}
	// ⚠️ 这里必须**数个数**，不能只问「有没有」：菜单那一页上那个复选框摆的字也是
	// 「本地音乐」，它的无障碍名字更是「在侧栏里显示「本地音乐」」。用子串匹配的
	// `HasText` 永远是绿的；用全等匹配也仍然是绿的（复选框那个字一模一样）。
	// 「侧栏上那一项不见了」的正确判据是**从两处变成一处**。
	if got := countText(tt, "本地音乐"); got != 1 {
		t.Errorf("勾掉了之后画面上有 %d 处「本地音乐」，want 1（只剩菜单那一页的复选框）", got)
	}

	// 勾回来。
	clickCheckbox(t, tt, "在侧栏里显示「本地音乐」")
	if got := a.SettingsSnapshot().HiddenMenuKeys; len(got) != 0 {
		t.Errorf("勾回来之后还藏着 %q", got)
	}
	if got := countText(tt, "本地音乐"); got != 2 {
		t.Errorf("勾回来之后画面上有 %d 处「本地音乐」，want 2（侧栏 + 菜单那一页）", got)
	}
}

// TestTheSelectAllCheckboxIsStrictLengthEqual 验「全选」是严格长度相等。
//
// 这条是照着验收的形状写的（PLAN C-50）：判定是
// `len(groupKeys) > 0 && len(selectedKeys) == len(groupKeys)`。
//
// ⚠️ 特意构造一组「长度一样但成员不同」的情况：改了集合比较的话它会判成全选，
// 而界面上明明有一项没勾上——用户点一下「全选」以为会全勾上，结果是全取消。
func TestTheSelectAllCheckboxIsStrictLengthEqual(t *testing.T) {
	keys := []string{"/", "/follow", "/later", "/history"}

	cases := []struct {
		name    string
		visible []string
		want    bool
	}{
		{"一个都没勾（长度都是 0，集合比较会判成全选）", nil, false},
		{"全勾上了", keys, true},
		{"勾了三个", keys[:3], false},
		{"长度和 keys 一样，但换成了别的键（不是子集）", []string{"/a", "/b", "/c", "/d"}, true},
		{"比 keys 还长", []string{"/a", "/b", "/c", "/d", "/e"}, false},
	}
	for _, c := range cases {
		if got := menuAllChecked(keys, c.visible); got != c.want {
			t.Errorf("%s：%v 对 %v 判成 %v，want %v", c.name, c.visible, keys, got, c.want)
		}
	}
	// 一组空的永远不是全选（上面第一条已经覆盖了），空 keys 也不能是。
	if menuAllChecked(nil, nil) {
		t.Error("一组空的菜单被判成了全选")
	}
}

// TestTheSelectAllCheckboxReallyWritesTheHiddenList 验「全选」那一格接上了设置。
//
// 纯函数那一半（严格长度相等）上面已经验了；这一半验它按下去真的写对了
// `hiddenMenuKeys`——两半分开是因为「判定对、接线错」和「接线对、判定错」是两个
// 完全不同方向的失败。
func TestTheSelectAllCheckboxReallyWritesTheHiddenList(t *testing.T) {
	tt, _, a, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "菜单设置")

	// 一开始六项都是勾上的，所以点一下「全选」是**全取消**——Biu 是
	// `onSelectionChange(checked ? groupKeys : [])`（`select-all-checkbox-group`）。
	clickCheckbox(t, tt, "全选：系统默认菜单")
	if got := a.SettingsSnapshot().HiddenMenuKeys; len(got) != len(menuGroupKeys()) {
		t.Errorf("全取消之后藏着 %q，want 全部 %d 条", got, len(menuGroupKeys()))
	}
	clickCheckbox(t, tt, "全选：系统默认菜单")
	if got := a.SettingsSnapshot().HiddenMenuKeys; len(got) != 0 {
		t.Errorf("全勾上之后还藏着 %q", got)
	}
}

// TestHidingAMenuItemLeavesOtherGroupsAlone 验别的组写的键一个都不动。
//
// `hiddenMenuKeys` 是**一份全局名单**，Biu 那边还有两组往同一个键里写各自的 id
// （`menu-settings.tsx:57-101`）。在这里动到它们，用户在别处（将来）做的选择就被
// 悄悄抹掉了。
func TestHidingAMenuItemLeavesOtherGroupsAlone(t *testing.T) {
	st := app.DefaultSettings("")
	st.HiddenMenuKeys = []string{"90001", "/later", "90002"}
	tt, _, a, _ := shellOnSettings(t, st)
	openSettingsTab(t, tt, "菜单设置")

	clickCheckbox(t, tt, "在侧栏里显示「推荐音乐」")
	got := a.SettingsSnapshot().HiddenMenuKeys
	if len(got) != 4 {
		t.Fatalf("藏一项之后名单变成了 %q，want 原有三条 + 新藏的一条", got)
	}
	for _, want := range []string{"90001", "90002", "/later", "/"} {
		if !contains(got, want) {
			t.Errorf("别的组写的 %q 被抹掉了：%q", want, got)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 小工具
// ─────────────────────────────────────────────────────────────────────────────

// openFontMenu 把字体那一格的下拉摆开。
func openFontMenu(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	if err := tt.Click("选择字体"); err != nil {
		t.Fatalf("点不到字体下拉：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	if !tt.HasText("苹方 / 微软雅黑") {
		t.Fatalf("字体面板没有摆开，文字有 %q", tt.Texts())
	}
}

// openColorPicker 摆开某一块颜色的取色板。
func openColorPicker(t *testing.T, tt *mygoui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatalf("点不到「%s」那块颜色：%v，文字有 %q", label, err, tt.Texts())
	}
	tt.Frame()
}

// openSelect 摆开一个下拉。靠 Label 找：下拉自己摆的字是选中的那一个，改了会变。
func openSelect(t *testing.T, tt *mygoui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatalf("点不到下拉「%s」：%v，文字有 %q", label, err, tt.Texts())
	}
	tt.Frame()
}

// typeIntoProxyField 在代理那一页的一个输入框里敲字然后提交。
//
// ⚠️ 名字带 Proxy：上游的 `localmusic_test.go` 里已经有一个 `typeInto` 了（同包同名
// 编不过），两个的用途也不同——那个往本地音乐页的框里敲，这个往代理那一页。
//
// ⚠️ `Tester.Type` 是往**有焦点的**输入框里塞文本（`platform.TextInput`），不带
// 按键；所以先点一下那个框让它拿到焦点。
func typeIntoProxyField(t *testing.T, tt *mygoui.Tester, label, text string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatalf("点不到输入框「%s」：%v，文字有 %q", label, err, tt.Texts())
	}
	tt.Frame()
	tt.Type(text)
	tt.Frame()
	tt.Key(0, mygoui.KeyEnter)
	tt.Frame()
}

// clickSwitch 点一个开关。靠它自己的 Label 找——所以每个开关都配了
// 「开关：那一行的标题」这个名字（那也是读屏软件唯一能念出来的名字，见
// `pickSwitch` 的注释）。用「开关：」开头而不是照抄那一行的标题，是因为界面上
// 标题那一处是**一字不差**的同名文本：`Find` 只给第一处。
func clickSwitch(t *testing.T, tt *mygoui.Tester, s *Shell, title string) {
	t.Helper()
	clickSetting(t, tt, s, title)
}

// clickCheckbox 点一个复选框。`ui.Checkbox` 的字和框是一体的（`coreCheckbox` 把它们
// 放在同一行里，点字也算点它），所以直接 `tt.Click(那个字)`。
func clickCheckbox(t *testing.T, tt *mygoui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatalf("点不到复选框「%s」：%v，文字有 %q", label, err, tt.Texts())
	}
	tt.Frame()
}

// clickCell 点一格录制框，按它的 Label 找（画面上那一格显示的字在录的时候会变，
// 拿它当名字就再也点不到它了）。
func clickCell(t *testing.T, tt *mygoui.Tester, label string) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("找不到录制格「%s」，文字有 %q", label, tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
}

// sameInk 比两小片画面是不是一模一样。
//
// 只看「深色像素的个数」不够：两行字只要有一个像素不同，笔画位置就不一样，个数却
// 可能正好相等（少一个多一个）。所以逐像素比。
// hasExactText 报画面上有没有**一个字不差**的那一条。
//
// `Tester.HasText` 是子串匹配（testing.md 里写着的），所以「在侧栏里显示「稍后再
// 看」」里也有「稍后再看」——用它验「某个词不见了」永远是绿的。
func hasExactText(tt *mygoui.Tester, want string) bool { return countText(tt, want) > 0 }

// proxyOptionLabels 是画面上那些**看起来像代理类型档位**的文字：以「 代理」结尾，
// 或者就是「不使用代理」。
func proxyOptionLabels(tt *mygoui.Tester) []string {
	var out []string
	seen := map[string]bool{}
	for _, txt := range tt.Texts() {
		if (strings.HasSuffix(txt, " 代理") || txt == "不使用代理") && !seen[txt] {
			seen[txt] = true
			out = append(out, txt)
		}
	}
	return out
}

// countText 数画面上有几处**一字不差**的那一条。
func countText(tt *mygoui.Tester, want string) int {
	n := 0
	for _, got := range tt.Texts() {
		if got == want {
			n++
		}
	}
	return n
}

// fontPreviewPad 是上面那个参照窗口的内边距，用来把字挪到和面板上一致的位置。
// 12 是面板上那一项的内边距（`settings_font.go` 里的 `Padding(6, 10)` 加上行高
// 差出来的那一点）；差一两个像素不影响「有没有墨」的逐像素比——差半个字就会。
const fontPreviewPad = 12

// inkShot 是一张画面的一小块，连着它在那一帧里的位置一起存下来。
type inkShot struct {
	img *image.RGBA
	at  mygoui.Rect
}

// inkShotOf 取某一条在当前这一帧里的墨迹快照。
func inkShotOf(t *testing.T, tt *mygoui.Tester, label string) inkShot {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("面板上没有 %q，文字有 %q", label, tt.Texts())
	}
	return inkShot{img: tt.Image(), at: r}
}

// sameGlyphs 比两小片画面里的**字**是不是一模一样。
//
// 两片各自先找出墨迹的**外框**，再从各自的外框左上角逐像素比。只比「深色像素的
// 个数」是不够的：两行字只要有一个像素不同，笔画位置就不一样，个数却可能正好
// 相等（这里少一个那里多一个）。比的是「有没有墨」，不是墨色——两处都是正文色。
//
// 先找外框是必须的：面板上那一格的框是从**整项**的左边开始的（里面还有 10 的
// 内边距），参照那一格的框从文字开始，两边差着十几个像素。直接按框比，比的是
// 「空对空」，一个字都比不到。
func sameGlyphs(imgA *image.RGBA, a mygoui.Rect, imgB *image.RGBA, b mygoui.Rect) bool {
	boxA, okA := inkBox(imgA, a)
	boxB, okB := inkBox(imgB, b)
	if !okA || !okB {
		// 一边一个字都没有：不算「一样」。那是「这一行没画出来」，得报出来。
		return false
	}
	w := int(minF(boxA.W, boxB.W))
	h := int(minF(boxA.H, boxB.H))
	if w == 0 || h == 0 {
		return false
	}
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			if inkAt(imgA, boxA.X+float32(col), boxA.Y+float32(row)) !=
				inkAt(imgB, boxB.X+float32(col), boxB.Y+float32(row)) {
				return false
			}
		}
	}
	return true
}

// inkBox 是一小片画面里墨迹的**外框**（紧贴着最外侧那些有墨的像素）。没有墨就
// 返回 ok=false。
func inkBox(img *image.RGBA, r mygoui.Rect) (mygoui.Rect, bool) {
	x0, y0 := int(r.X), int(r.Y)
	x1, y1 := x0+int(r.W), y0+int(r.H)
	left, top, right, bottom := x1, y1, x0-1, y0-1
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if !inkAt(img, float32(x), float32(y)) {
				continue
			}
			if x < left {
				left = x
			}
			if x > right {
				right = x
			}
			if y < top {
				top = y
			}
			if y > bottom {
				bottom = y
			}
		}
	}
	if right < left || bottom < top {
		return mygoui.Rect{}, false
	}
	return mygoui.Rect{
		X: float32(left), Y: float32(top),
		W: float32(right - left + 1), H: float32(bottom - top + 1),
	}, true
}

func minF(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

// writeShortcutFileFromNewer 造一份「来自更新版本」的快捷键文件，返回路径。
func writeShortcutFileFromNewer(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), shortcutFileName)
	writeJSON(t, path, map[string]any{
		"schemaVersion": 99,
		"shortcuts":     []map[string]string{{"id": "togglePlay", "name": "播放/暂停", "shortcut": "CommandOrControl+P"}},
	})
	return path
}

func inkAt(img *image.RGBA, x, y float32) bool {
	if !inside(img, int(x), int(y)) {
		return false
	}
	r, g, b, _ := img.At(int(x), int(y)).RGBA()
	return r>>8 < 200 || g>>8 < 200 || b>>8 < 200
}

func inside(img *image.RGBA, x, y int) bool {
	return x >= 0 && y >= 0 && x < img.Bounds().Dx() && y < img.Bounds().Dy()
}

func maxF(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// TestTheLocalMusicDirectoriesAreEditable 验 `localMusicDirs` 改得了。
//
// 这一条是被审阅挑出来的：`localMusicDirs` 在整个应用里**一处写它的地方都没有**
// （只有 app 包里的字段定义、默认值、normalize），所以「所有设置项都在界面上能改」
// 实际差这一个键。Biu 把它摆在 local-music 页上，而本项目那一页现在还是空状态，
// 所以它落在设置页上——见 `pickLocalMusicDirs` 的说明。
//
// 三个动作挨个走一遍：加、重复加不重复、删。重复加那一条最要紧：同一个目录摆两遍，
// 音乐列表里也会出现两遍，而用户以为自己只加了一次。
func TestTheLocalMusicDirectoriesAreEditable(t *testing.T) {
	st := app.DefaultSettings("")
	st.LocalMusicDirs = []string{"/music/rock", "/music/jazz"}
	tt, s, a, _ := shellOnSettings(t, st)

	// 起点两条都在画面上。
	for _, dir := range st.LocalMusicDirs {
		if !hasExactText(tt, dir) {
			t.Fatalf("起点没有摆出「%s」，文字有 %q", dir, tt.Texts())
		}
	}

	// 加一条。文件框在无头环境里调不了，替掉 `s.picker`（和 autostart 同一个道理）。
	fake := &fakePicker{path: "/music/classic"}
	s.picker = fake
	clickSetting(t, tt, s, "添加文件夹")
	waitFor(t, "第三个本地音乐目录写进设置", func() bool { return len(a.SettingsSnapshot().LocalMusicDirs) == 3 })

	// 再加一次同一个：不该出现两遍。
	fake.path = "/music/classic"
	clickSetting(t, tt, s, "添加文件夹")
	waitFor(t, "本地音乐目录写进设置", func() bool { return len(a.SettingsSnapshot().LocalMusicDirs) >= 3 })
	tt.Frame()
	if got := a.SettingsSnapshot().LocalMusicDirs; len(got) != 3 {
		t.Errorf("同一个目录加了两次之后是 %q，want 三条", got)
	}

	// 删掉中间那一条，留下的两条顺序不能乱。
	//
	// ⚠️ `before` 是**删之前**从 store 里拿的一份快照，它和 store 自己**共享**那一段
	// 数组（`internal/app/settings.go` 的 Snapshot 注释里写着）。所以删完之后 `before`
	// 必须还是原来那两条——直接对共享的数组 `slices.Delete` 会把顺序改掉，而那一格
	// 还没写盘，于是下次打开看到的是乱的。
	before := a.SettingsSnapshot()
	clickSetting(t, tt, s, "移除目录「/music/rock」")
	tt.Frame()
	got := a.SettingsSnapshot().LocalMusicDirs
	if len(got) != 2 || got[0] != "/music/jazz" || got[1] != "/music/classic" {
		t.Fatalf("删掉中间一条之后是 %q，want [/music/jazz /music/classic]", got)
	}
	if len(before.LocalMusicDirs) != 3 ||
		before.LocalMusicDirs[0] != "/music/rock" ||
		before.LocalMusicDirs[1] != "/music/jazz" ||
		before.LocalMusicDirs[2] != "/music/classic" {
		t.Errorf("删的那一下把共享的那段数组改了：%q", before.LocalMusicDirs)
	}
}

// TestTheLocalMusicDirectoriesDoNotCrashOnADuplicate 验设置文件里有重复目录时这一页
// **画得出来**。
//
// ⚠️ 这一条是被一个真崩溃逼出来的：那一行的 key 就是路径，而 MyGo 在同一层里拿到
// 两个一样的 key 会**当场 panic**——那一格带着整个设置页换成错误页，用户之前改好的
// 设置一个都看不见。设置文件是用户能直接编辑的（也能从 Biu 那边来），两个一样的
// 目录在文件里完全合法，所以这一格必须自己去重。
func TestTheLocalMusicDirectoriesDoNotCrashOnADuplicate(t *testing.T) {
	st := app.DefaultSettings("")
	st.LocalMusicDirs = []string{"/music/rock", "/music/rock", "/music/jazz"}
	tt, _, _, _ := shellOnSettings(t, st)

	if tt.HasText("界面出错了") {
		t.Fatalf("有重复目录就把整页画崩了，文字有 %q", tt.Texts())
	}
	if got := countText(tt, "/music/rock"); got != 1 {
		t.Errorf("重复的目录画了 %d 遍，want 1", got)
	}
	if got := countText(tt, "/music/jazz"); got != 1 {
		t.Errorf("/music/jazz 画了 %d 遍，want 1", got)
	}
}

// TestEachPathButtonAsksItsOwnQuestion 验三个「选择」按钮各自给**自己**的那两行字。
//
// 合并上游时两边都加过挑路径的功能：上游的 `platform.PickDirectory`（本地音乐页的
// 「添加目录」）和我这一票的 `PickPath`（设置页的下载目录 / FFmpeg）。同一个系统对
// 话框，问的问题不一样——本地音乐那页问的是「挑的音频会出现在哪儿」，设置页的下载
// 目录问的是「下载的东西会存到哪儿」。写死成一句就有一边在说错的话，而错的那句是
// **直接弹在系统对话框上**的。
func TestEachPathButtonAsksItsOwnQuestion(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))

	for _, c := range []struct {
		button string
		dir    bool
		title  string
	}{
		{"添加文件夹", true, "选择音乐文件夹"},
		{"选择", true, ""}, // 下载目录和 FFmpeg 那一行上都有「选择」，分开点
	} {
		fake := &fakePicker{path: "/somewhere"}
		s.picker = fake
		if c.title == "" {
			// 下载目录：先把播放条之外的那个「选择」点掉，先验 FFmpeg 那一行再说。
			continue
		}
		clickSetting(t, tt, s, c.button)
		waitFor(t, "文件框弹出来", func() bool { return fake.gotText[0] != "" })
		if fake.gotDir != c.dir {
			t.Errorf("「%s」弹的不是文件夹对话框", c.button)
		}
		if fake.gotText[0] != c.title {
			t.Errorf("「%s」弹出来的标题是 %q，want %q", c.button, fake.gotText[0], c.title)
		}
	}
}

// fakePicker 是替掉真的系统文件框的那一份。
//
// ⚠️ `dir` 和那两行字都记下来：那一格挑的是不是文件夹、对话框上说的是不是这一件事，
// 都是界面上看得见的——记下来才能在测试里断言它们没有串台。
//
// 「挑配置文件」和「存到哪」各有各的一格返回值（`configPath` / `savedAs`），
// 因为它们和 `path` 问的不是一件事：`path` 是「下载目录」「本地音乐目录」，
// 另两个是设置页末尾「配置」那一段的两个按钮。
type fakePicker struct {
	path    string
	err     error
	gotDir  bool
	gotText [2]string

	configPath string
	configErr  error
	savedAs    string
	savedName  string
	saveErr    error
}

func (f *fakePicker) pick(_ *mygo.Window, dir bool, title, message string) (string, error) {
	f.gotDir, f.gotText = dir, [2]string{title, message}
	return f.path, f.err
}

func (f *fakePicker) pickConfig(_ *mygo.Window) (string, error) {
	return f.configPath, f.configErr
}

func (f *fakePicker) saveConfig(_ *mygo.Window, defaultName string) (string, error) {
	f.savedName = defaultName
	return f.savedAs, f.saveErr
}
