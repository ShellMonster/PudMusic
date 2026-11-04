package ui

import (
	"image"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	mygoui "github.com/egoist/mygo/ui"
)

// 设置页各 Tab 的测试跑在真实的 Shell 上（和 settings_page_test.go 同一套外壳），断言的是
// **画面上有什么**和**设置里变成了什么**，不是「调了哪个函数」。每一个控件都有一条
// 「点它、读设置」的测试——「页画得挺好，只是某一格按了没反应」在界面上完全看不出来。

// TestPickingTheFontChangesWhatTheWholeWindowDrawsIn 验字体是真的换掉了：读的是**主题里的字体**，
// 不是设置里的字符串（设置存对了而主题没跟着换，界面上一个字都不会变）。
func TestPickingTheFontChangesWhatTheWholeWindowDrawsIn(t *testing.T) {
	tt, s, _, _ := shellOnAppearance(t, app.DefaultSettings(""))

	// 「系统默认」这一档要一个字节都不改主题：`t.Font` 的零值是系统自己的字体，而设置里默认是
	// `"system-ui"`——那是另一个字符串，设上去全应用的字宽都会变。
	if got := BuildTheme(s.app.SettingsSnapshot(), false).Font; got != "" {
		t.Fatalf("选了系统默认之后主题里的字体是 %q，应该是空的（= 系统自己的）", got)
	}
	options := fontOptionList()
	if len(options) < 2 {
		t.Skip("系统没有报任何字体")
	}
	// 列表一屏露 6 行，挑第二条（第一条是系统默认）。
	want := options[1]
	clickSetting(t, tt, s, want.label)
	tt.Frame()

	if got := s.app.SettingsSnapshot().FontFamily; got != want.family {
		t.Errorf("设置里的字体是 %q，want %q", got, want.family)
	}
	if got := BuildTheme(s.app.SettingsSnapshot(), false).Font; got != want.family {
		t.Errorf("这一帧主题里的字体是 %q，want %q（界面不会跟着变）", got, want.family)
	}
}

// TestTheFontListHidesTheBundledTitleFont 验随包的标题字体「PudMusic Serif」不在设置页的字体列表里
// （它是内部字体，不是用户能选来当界面字体的）。
func TestTheFontListHidesTheBundledTitleFont(t *testing.T) {
	for _, o := range fontOptionList() {
		if o.family == "PudMusic Serif" || o.label == "PudMusic Serif" {
			t.Fatalf("字体列表里不该有随包的标题字体：%+v", o)
		}
	}
}

// TestEveryFontOptionIsDrawnInItsOwnFont 验**每一条字体选项用它自己的字体画**。
//
// 直接比两行不同的字没有用（字本来就不一样，全用同一个字体画逐像素比也是「不一样」），所以拿参照比：
// 为每一条另起一个无头窗口，只画那一行字、写死它该用的那一族，再拿它的墨迹和列表里那一行比。
func TestEveryFontOptionIsDrawnInItsOwnFont(t *testing.T) {
	tt, s, _, _ := shellOnAppearance(t, app.DefaultSettings(""))
	options := fontOptionList()
	if len(options) < 4 {
		t.Fatalf("系统只报了 %d 条字体，这条测试没有可比的三条", len(options)-1)
	}
	for _, opt := range options[1:4] {
		text := opt.label + " · 晴天 夜航电台 0123"
		settingRect(t, tt, s, opt.label) // 先把列表和这一行滚进可视区
		got := inkShotOf(t, tt, text)
		want := renderedIn(t, text, opt.family)
		if !sameGlyphs(got.img, got.at, want.img, want.at) {
			t.Errorf("「%s」那一行不是用 %q 画出来的：列表里的墨迹和单独画一遍对不上。"+
				"列表 %.0f,%.0f %.0fx%.0f / 参照 %.0f,%.0f %.0fx%.0f",
				opt.label, opt.family,
				got.at.X, got.at.Y, got.at.W, got.at.H,
				want.at.X, want.at.Y, want.at.W, want.at.H)
		}
	}
}

// TestPickingAnAccentChangesTheWholeTheme 验点一枚强调色，设置和整份主题一起换。
func TestPickingAnAccentChangesTheWholeTheme(t *testing.T) {
	tt, s, _, _ := shellOnAppearance(t, app.DefaultSettings(""))
	clickSetting(t, tt, s, "松绿")
	if got := s.app.SettingsSnapshot().Accent; got != app.AccentPine {
		t.Errorf("设置里的强调色是 %q，want pine", got)
	}
	if got, want := BuildTheme(s.app.SettingsSnapshot(), false).Accent, tok.AccentColor("pine", false); got != want {
		t.Errorf("这一帧主题的强调色是 %v，want %v", got, want)
	}
}

// TestTheAccentRowHidesWhenCoverAccentIsOn 验开了「封面取色」就隐藏强调色那一行（原型 `!s.coverAccent`）。
func TestTheAccentRowHidesWhenCoverAccentIsOn(t *testing.T) {
	tt, s, a, _ := shellOnAppearance(t, app.DefaultSettings(""))
	if !hasExactText(tt, "强调色") {
		t.Fatalf("起点没有强调色一行：%q", tt.Texts())
	}
	clickSetting(t, tt, s, "开关：封面取色")
	if !a.SettingsSnapshot().CoverAccent {
		t.Fatal("封面取色没有打开")
	}
	if hasExactText(tt, "强调色") {
		t.Errorf("开了封面取色强调色那一行还在：%q", tt.Texts())
	}
}

// TestTheAppearanceTabStoresEachOfItsChoices 验外观 Tab 里每一格都存得住：深浅三张卡、界面字号、
// 收起侧栏、数据显示样式三张卡。
func TestTheAppearanceTabStoresEachOfItsChoices(t *testing.T) {
	tt, s, a, _ := shellOnAppearance(t, app.DefaultSettings(""))

	for _, c := range []struct {
		label string
		want  app.ThemeMode
	}{{"深色", app.ThemeModeDark}, {"浅色", app.ThemeModeLight}, {"跟随系统", app.ThemeModeSystem}} {
		clickTheme(t, tt, s, c.label)
		if got := a.SettingsSnapshot().ThemeMode; got != c.want {
			t.Errorf("点了「%s」之后主题档位是 %q，want %q", c.label, got, c.want)
		}
	}

	// 界面字号（下拉）。
	settingRect(t, tt, s, "选择界面字号")
	openSelect(t, tt, "选择界面字号")
	if err := tt.Click("大"); err != nil {
		t.Fatalf("下拉里点不到「大」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	if got := a.SettingsSnapshot().FontScale; got != app.FontScaleLarge {
		t.Errorf("界面字号是 %q，want lg", got)
	}

	clickSetting(t, tt, s, "开关：收起侧栏")
	if !a.SettingsSnapshot().SideMenuCollapsed {
		t.Error("收起侧栏没有打开")
	}

	for _, c := range []struct {
		label string
		want  app.DisplayMode
	}{{"卡片", app.DisplayModeCard}, {"紧凑", app.DisplayModeCompact}, {"列表", app.DisplayModeList}} {
		clickSetting(t, tt, s, c.label)
		if got := a.SettingsSnapshot().DisplayMode; got != c.want {
			t.Errorf("点了「%s」之后数据显示样式是 %q，want %q", c.label, got, c.want)
		}
	}
}

// TestTheInterfaceFontScaleScalesTheText 验「界面字号」真的改了文字大小：同一行字在「大」档比「紧凑」档高。
func TestTheInterfaceFontScaleScalesTheText(t *testing.T) {
	size := func(fs app.FontScale) float32 {
		st := app.DefaultSettings("")
		st.FontScale = fs
		tt, _, _, _ := shellOnSettings(t, st)
		r, ok := tt.Find("默认音质")
		if !ok {
			t.Fatalf("找不到「默认音质」：%q", tt.Texts())
		}
		return r.H
	}
	small, large := size(app.FontScaleSmall), size(app.FontScaleLarge)
	if !(large > small) {
		t.Errorf("「大」档的行标题高 %.1f，「紧凑」档 %.1f——字号没有跟着档位变", large, small)
	}
}

// TestTheGeneralTabStoresEachOfItsChoices 验常规那一页的每一格都存得住。
func TestTheGeneralTabStoresEachOfItsChoices(t *testing.T) {
	tt, s, a, _ := shellOnSettings(t, app.DefaultSettings(""))

	// 默认音质（下拉）。
	openSelect(t, tt, "选择默认音质")
	if err := tt.Click("无损"); err != nil {
		t.Fatalf("下拉里点不到「无损」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	if got := a.SettingsSnapshot().AudioQuality; got != app.AudioQualityLossless {
		t.Errorf("音质是 %q，want lossless", got)
	}

	// 开关：没有名字，靠 Label 找（`dropdown` / 开关都配了「开关：标题」）。
	clickSwitch(t, tt, s, "开关：上报本机播放记录")
	if a.SettingsSnapshot().ReportPlayHistory {
		t.Error("上报播放记录没有关掉")
	}
	clickSwitch(t, tt, s, "开关：上报本机播放记录")
	if !a.SettingsSnapshot().ReportPlayHistory {
		t.Error("上报播放记录没有打开")
	}
	clickSwitch(t, tt, s, "开关：更新提醒")
	if a.SettingsSnapshot().UpdateNotificationEnabled() {
		t.Error("更新提醒没有关掉")
	}
	clickSwitch(t, tt, s, "开关：开启刘海播放器")
	if a.SettingsSnapshot().NotchPlayerOn() == app.DefaultSettings("").NotchPlayerOn() {
		t.Error("刘海播放器开关没有生效")
	}
}

// TestTheProxyTabMapsItsThreeModesToTheSettings 验代理 Tab 的三档落到设置上：
// 不使用代理 → direct，使用系统代理 → none（照环境变量走），自定义 → 保存之后才是 http / socks…
func TestTheProxyTabMapsItsThreeModesToTheSettings(t *testing.T) {
	tt, s, a, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "代理")

	typ := func() app.ProxyType { return a.SettingsSnapshot().ProxySettings.Type }
	if typ() != app.ProxyNone {
		t.Fatalf("起点是 %q，want none（系统代理）", typ())
	}
	clickSetting(t, tt, s, "代理方式：不使用代理")
	if typ() != app.ProxyDirect {
		t.Errorf("点了「不使用代理」之后是 %q，want direct", typ())
	}
	clickSetting(t, tt, s, "代理方式：使用系统代理")
	if typ() != app.ProxyNone {
		t.Errorf("点了「使用系统代理」之后是 %q，want none", typ())
	}
	// 选「自定义」只开表单，不写设置：http 而主机空着的话每条请求都会失败。
	clickSetting(t, tt, s, "代理方式：自定义代理")
	if typ() != app.ProxyNone {
		t.Errorf("点了「自定义代理」就把设置改成了 %q（半填的表单不该落盘）", typ())
	}
	for _, want := range []string{"代理类型", "代理主机", "代理端口", "代理用户名", "代理密码", "保存并测试"} {
		if !tt.HasText(want) {
			t.Errorf("自定义表单上没有 %q，文字有 %q", want, tt.Texts())
		}
	}
}

// TestTheCustomProxyFormValidatesBeforeItSaves 验「保存并测试」先校验：主机空、端口不是数字都不落盘。
func TestTheCustomProxyFormValidatesBeforeItSaves(t *testing.T) {
	tt, s, a, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "代理")
	clickSetting(t, tt, s, "代理方式：自定义代理")

	clickSetting(t, tt, s, "保存并测试")
	if got := a.SettingsSnapshot().ProxySettings.Type; got != app.ProxyNone {
		t.Fatalf("什么都没填就落盘了：%q", got)
	}
	if !tt.HasText("请填写代理主机") || !tt.HasText("端口是 1–65535 的数字") {
		t.Errorf("校验提示没出来：%q", tt.Texts())
	}
}

// TestTheCustomProxyFormSavesAndProbesTheProxy 验填好主机和端口之后「保存并测试」写进设置，
// 并只拨一下代理自己的地址（不向真实站点发请求）。
func TestTheCustomProxyFormSavesAndProbesTheProxy(t *testing.T) {
	probed := make(chan string, 1)
	old := proxyProbe
	proxyProbe = func(host, port string) error { probed <- host + ":" + port; return nil }
	t.Cleanup(func() { proxyProbe = old })

	tt, s, a, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "代理")
	clickSetting(t, tt, s, "代理方式：自定义代理")
	typeIntoProxyField(t, tt, "127.0.0.1输入框", "127.0.0.1")
	typeIntoProxyField(t, tt, "7890输入框", "7890")
	clickSetting(t, tt, s, "保存并测试")

	got := a.SettingsSnapshot().ProxySettings
	if got.Type != app.ProxyHTTP || got.Host != "127.0.0.1" || got.Port == nil || *got.Port != 7890 {
		t.Fatalf("保存下来的是 %+v", got)
	}
	select {
	case addr := <-probed:
		if addr != "127.0.0.1:7890" {
			t.Errorf("测试拨的是 %q", addr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("没有拨代理")
	}
}

// TestTheMenuTabHidesAnItemFromTheSidebar 验菜单 Tab 的开关把入口从侧栏藏掉、再放回来。
func TestTheMenuTabHidesAnItemFromTheSidebar(t *testing.T) {
	tt, s, a, _ := shellOnSettings(t, app.DefaultSettings(""))

	if !tt.HasText("本地音乐") {
		t.Fatalf("侧栏上起点就没有「本地音乐」，文字有 %q", tt.Texts())
	}
	openSettingsTab(t, tt, "菜单")
	clickSetting(t, tt, s, "开关：在侧栏里显示「本地音乐」")
	if got := a.SettingsSnapshot().HiddenMenuKeys; len(got) != 1 || got[0] != localMusicPath {
		t.Fatalf("设置里藏起来的是 %q，want [%s]", got, localMusicPath)
	}
	// 「侧栏上那一项不见了」的判据是**从两处变成一处**（菜单页那一行的标题也叫「本地音乐」）。
	if got := countText(tt, "本地音乐"); got != 1 {
		t.Errorf("关掉之后画面上有 %d 处「本地音乐」，want 1（只剩菜单那一页的行标题）", got)
	}
	clickSetting(t, tt, s, "开关：在侧栏里显示「本地音乐」")
	if got := a.SettingsSnapshot().HiddenMenuKeys; len(got) != 0 {
		t.Errorf("打开之后还藏着 %q", got)
	}
	if got := countText(tt, "本地音乐"); got != 2 {
		t.Errorf("打开之后画面上有 %d 处「本地音乐」，want 2（侧栏 + 菜单那一页）", got)
	}
}

// TestHidingAMenuItemLeavesOtherGroupsAlone 验别的组写的键一个都不动：`hiddenMenuKeys` 是一份全局名单，
// 收藏夹的 id 和别的菜单项都写在里面。
func TestHidingAMenuItemLeavesOtherGroupsAlone(t *testing.T) {
	st := app.DefaultSettings("")
	st.HiddenMenuKeys = []string{"90001", "90002", "/later"}
	tt, s, a, _ := shellOnSettings(t, st)
	openSettingsTab(t, tt, "菜单")
	clickSetting(t, tt, s, "开关：在侧栏里显示「本地音乐」")
	got := a.SettingsSnapshot().HiddenMenuKeys
	if len(got) != 4 {
		t.Fatalf("藏一项之后名单变成了 %q，want 原有三条 + 新藏的一条", got)
	}
	for _, want := range []string{"90001", "90002", "/later", localMusicPath} {
		if !contains(got, want) {
			t.Errorf("别的组写的 %q 被抹掉了：%q", want, got)
		}
	}
}

// TestTheDownloadDirButtonAsksForAFolder 验下载目录那一行的「更改」弹的是文件夹对话框，
// 挑完存进 `downloadPath`。
func TestTheDownloadDirButtonAsksForAFolder(t *testing.T) {
	tt, s, a, _ := shellOnSettings(t, app.DefaultSettings(""))
	fake := &fakePicker{path: "/somewhere/songs"}
	s.picker = fake
	clickSetting(t, tt, s, "更改")
	waitFor(t, "文件框弹出来", func() bool { tt.Frame(); return fake.gotText[0] != "" })
	if !fake.gotDir {
		t.Error("「更改」弹的不是文件夹对话框")
	}
	waitFor(t, "下载目录存进设置", func() bool { tt.Frame(); return a.SettingsSnapshot().DownloadPath == "/somewhere/songs" })
}

// ─────────────────────────────────────────────────────────────────────────────
// 小工具
// ─────────────────────────────────────────────────────────────────────────────

// typeIntoProxyField 在代理表单的一个输入框里敲字然后提交（输入框按 Label 找，Label 是
// 「占位字 + 输入框」）。`Tester.Type` 往**有焦点的**输入框里塞文本，所以先点一下让它拿到焦点。
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

func firstMonospacedFamily(t *testing.T) string {
	t.Helper()
	for _, f := range ui.FontFamilies() {
		if f.Monospaced && f.Name != systemFontFamily {
			return f.Name
		}
	}
	t.Fatal("系统没有报任何等宽字体，这条测试没法挑一条来点")
	return ""
}

func renderedIn(t *testing.T, label, family string) inkShot {
	t.Helper()
	rt := mygoui.NewTester(func(c *mygoui.Context) {
		ui.Column(c).Padding(fontPreviewPad).Children(func() {
			// 文字色要和列表里那一行一样（--text）：抗锯齿边缘像素在"<200 算墨迹"的阈值上会随颜色不同而判得不一样。
			ui.Text(c, label).Font(family).FontSize(16).TextColor(tok.Of(BuildTheme(app.DefaultSettings(""), false)).Text)
		})
	}, 600, 80)
	rt.Frame()
	r, ok := rt.Find(label)
	if !ok || r.H == 0 {
		t.Fatalf("参照窗口里没有画出 %q", label)
	}
	return inkShot{img: rt.Image(), at: r}
}

func openSelect(t *testing.T, tt *mygoui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatalf("点不到下拉「%s」：%v，文字有 %q", label, err, tt.Texts())
	}
	tt.Frame()
}

func clickSwitch(t *testing.T, tt *mygoui.Tester, s *Shell, title string) {
	t.Helper()
	clickSetting(t, tt, s, title)
}

func clickCheckbox(t *testing.T, tt *mygoui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatalf("点不到复选框「%s」：%v，文字有 %q", label, err, tt.Texts())
	}
	tt.Frame()
}

func clickCell(t *testing.T, tt *mygoui.Tester, label string) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("找不到录制格「%s」，文字有 %q", label, tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
}

func hasExactText(tt *mygoui.Tester, want string) bool { return countText(tt, want) > 0 }

func countText(tt *mygoui.Tester, want string) int {
	n := 0
	for _, got := range tt.Texts() {
		if got == want {
			n++
		}
	}
	return n
}

type inkShot struct {
	img *image.RGBA
	at  mygoui.Rect
}

func inkShotOf(t *testing.T, tt *mygoui.Tester, label string) inkShot {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("面板上没有 %q，文字有 %q", label, tt.Texts())
	}
	return inkShot{img: tt.Image(), at: r}
}

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

type fakePicker struct {
	path    string
	err     error
	gotDir  bool
	gotText [2]string
}

func (f *fakePicker) pick(_ *mygo.Window, dir bool, title, message string) (string, error) {
	f.gotDir, f.gotText = dir, [2]string{title, message}
	return f.path, f.err
}

// fontPreviewPad 是 `renderedIn` 参照窗口的内边距。
const fontPreviewPad = 12
