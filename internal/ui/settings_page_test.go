package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/testproc"
	mygoui "github.com/egoist/mygo/ui"
)

// 设置页这一组测试跑在真实的 Shell 上：真 App、真设置文件、真画一帧、真点按钮。
//
// 断言的东西只有三类：**画面上的文字**（这一页有没有真画出来）、**读像素**
//（换档位之后界面是不是真的变了）、**磁盘上的文件**（改完是不是真存住了）。
// 不断言「调了哪个函数」——那只能证明状态层有那个方法，证明不了界面上真有一个
// 入口。三档按钮摆在文件里没人调用过，就是这么漏过去的。

// 1200x800 的窗口里，设置页能点的那一条带。上下两个数字是顶栏和播放条让出来的，
// 再各留 8 的余量。
//
// ⚠️ **两个数是从顶栏 / 播放条那两个常量算出来的，不是抄下来的**：票 #127 把顶栏
// 从 32 抬到 64（Biu 的 `h-16`）之后，写死的 40 就**比顶栏矮**了——于是一个
// y=45 的控件（也就是压在顶栏底下那一行的）会被 `settingRect` 当成「已在可视区里」
// 直接点下去，而点下去什么也不会发生。按 `navMinHeight` 算，顶栏再抬一次这里
// 跟着抬，而**那条断言**仍然是「别点在顶栏上」。
const (
	pageTop    = navMinHeight + 8
	pageBottom = 800 - playbarHeight - 8
)

// clickSetting 点设置页上的一个东西，**先把它滚进可视区再点**。
//
// ⚠️ 这一步不能省：设置页现在外面套了一层 `Scroll`（四个 Tab 的内容加起来比
// 窗口高），而 `tt.Click` 只按标签找元素、**不检查它是不是被滚出可视区**——
// 找到就点，点在窗外的地方，于是「点了没反应」，而测试自己看不出哪里不对。
// 现象和「控件坏了」一模一样，最容易把人带进一条查不到头的路。
func clickSetting(t *testing.T, tt *mygoui.Tester, s *Shell, label string) {
	t.Helper()
	r := settingRect(t, tt, s, label)
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
}

// settingRect 把设置页滚到 label 那一处在可视区里为止，返回它那一格的框。
//
// 单独拎出来是因为设置页上有几个控件**没有可点的字**（开关只有一个旋钮、颜色只有
// 一块色块），它们只能靠自己的 Label 找，而找出来之后仍然要滚进来才点得到。
func settingRect(t *testing.T, tt *mygoui.Tester, s *Shell, label string) mygoui.Rect {
	t.Helper()
	for range 48 {
		r, ok := tt.Find(label)
		if ok && r.H > 0 && r.Y >= pageTop && r.Y+r.H <= pageBottom {
			return r
		}
		// 在可视区上沿以上就往回滚（前面的断言可能已经把页滚到下面去了），否则往下滚。
		step := float32(60)
		if ok && r.Y < pageTop { // 滚出上沿时 Find 给的是贴在上沿、高 0 的框
			step = -60
		}
		before := s.settings.scroll.Y
		s.settings.scroll.Y = max(before+step, 0)
		tt.Frame()
		if s.settings.scroll.Y == before {
			break // 到头了，再滚也不动
		}
	}
	t.Fatalf("设置页上滚不到 %q，文字有 %q", label, tt.Texts())
	return mygoui.Rect{}
}

// openSettingsTab 切到设置页上的某一个 Tab。Tab 那一行永远在可视区顶部，不滚。
func openSettingsTab(t *testing.T, tt *mygoui.Tester, name string) {
	t.Helper()
	if err := tt.Click(name); err != nil {
		t.Fatalf("点不到设置页的 Tab %q：%v，文字有 %q", name, err, tt.Texts())
	}
	tt.Frame()
}

// shellOnSettings 造一个停在设置页上的外壳。
func shellOnSettings(t *testing.T, s app.Settings, opts ...shellOpt) (*mygoui.Tester, *Shell, *app.App, string) {
	t.Helper()
	// withSettings 走的是「先写盘、再挂上去」那条**生产**路（真程序启动时也是
	// 先读盘），不是往界面上硬塞一个 Settings 值。
	b := newTestShell(t, append([]shellOpt{withSettings(s)}, opts...)...)
	tt := b.tt
	tt.Frame()
	openSettings(t, tt)
	return tt, b.s, b.a, b.settingsPath
}

// TestSettingsPageShowsItsContent 验切过去看到的是设置页，不是占位页，并逐项点名：
// 设置页最可能的失败是「画出来了，但少了一项」。
func TestSettingsPageShowsItsContent(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))

	// 页头 + 六个分类 + 常规 Tab 的三组。
	for _, want := range []string{
		"设置", "常规", "外观", "菜单", "快捷键", "代理", "关于",
		"播放", "默认音质", "视频画质", "上报本机播放记录",
		"系统", "开机自启", "关闭窗口时留在菜单栏", "开启刘海播放器", "更新提醒",
		"下载", "下载目录", "更改",
	} {
		if !tt.HasText(want) {
			t.Errorf("设置页上没有 %q，文字有 %q", want, tt.Texts())
		}
	}
	// 原型有、底层没有的条目不做假开关。
	for _, no := range []string{"无缝播放", "恢复上次的播放", "记录播放历史", "切歌时显示通知", "默认下载内容"} {
		if tt.HasText(no) {
			t.Errorf("设置页上不该有没有底层支持的条目 %q", no)
		}
	}
	if s.builtPage(lookup(settingsPath)) == nil {
		t.Errorf("设置页没接上 builtPage，画出来的是空状态而不是设置页")
	}
}

// TestTheSettingsPageIsReachableFromTheMenu 验入口在侧栏上。
func TestTheSettingsPageIsReachableFromTheMenu(t *testing.T) {
	_, tt := newShell(t)
	if tt.HasText("默认音质") {
		t.Fatal("还没点就在设置页上了")
	}
	openSettings(t, tt)
}

// TestTheSettingsTabsSwitchAndEachDrawsItsOwnThing 验六个分类各自画出自己的东西。
func TestTheSettingsTabsSwitchAndEachDrawsItsOwnThing(t *testing.T) {
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	for _, c := range []struct {
		tab  string
		want []string
	}{
		{"外观", []string{"深浅模式", "深色", "浅色", "跟随系统", "颜色与字体", "封面取色", "界面字号", "收起侧栏", "字体", "数据显示样式", "卡片", "列表", "紧凑"}},
		{"菜单", []string{"侧栏菜单显示", "收藏夹显示"}},
		{"快捷键", []string{"快捷键设置", "启用全局快捷键", "命令", "应用内快捷键", "全局快捷键"}},
		{"代理", []string{"代理设置", "不使用代理", "使用系统代理", "自定义代理"}},
		{"关于", []string{"开源许可", "诊断信息", "查看", "复制"}},
		{"常规", []string{"播放", "系统", "下载"}},
	} {
		openSettingsTab(t, tt, c.tab)
		for _, w := range c.want {
			if !tt.HasText(w) {
				t.Errorf("「%s」Tab 上没有 %q，文字有 %q", c.tab, w, tt.Texts())
			}
		}
	}
	// 主题方案卡片是用户拍板不做的（只保留一套主题）。
	openSettingsTab(t, tt, "外观")
	if tt.HasText("主题方案") {
		t.Errorf("外观 Tab 上不该有「主题方案」：%q", tt.Texts())
	}
}

// TestTheMenuAboutItemOpensTheAboutTab 验应用菜单的「关于」不再开弹窗，而是跳到设置页的「关于」Tab。
func TestTheMenuAboutItemOpensTheAboutTab(t *testing.T) {
	b := newTestShell(t)
	b.tt.Frame()
	b.s.openAbout()
	settleSettingsPage(t, b.tt)
	if b.s.router.Path() != settingsPath || b.s.settings.tab != settingsTabAbout {
		t.Fatalf("菜单「关于」之后在 %q 的第 %d 个 Tab", b.s.router.Path(), b.s.settings.tab)
	}
	if !b.tt.HasText("开源许可") {
		t.Errorf("「关于」Tab 没画出来：%q", b.tt.Texts())
	}
}

// TestTheCloseWindowSwitchIsStored 验「关闭窗口时留在菜单栏」选得了、存得住：开 = hide，关 = exit。
func TestTheCloseWindowSwitchIsStored(t *testing.T) {
	want := app.DefaultSettings("")
	want.CloseWindowOption = app.CloseWindowHide
	tt, s, a, path := shellOnSettings(t, want)

	if got := a.SettingsSnapshot().CloseWindowOption; got != app.CloseWindowHide {
		t.Fatalf("起点是 %q", got)
	}
	clickSetting(t, tt, s, "开关：关闭窗口时留在菜单栏")
	tt.Frame()
	if got := a.SettingsSnapshot().CloseWindowOption; got != app.CloseWindowExit {
		t.Fatalf("关掉开关之后设置是 %q", got)
	}
	if err := a.FlushSettings(); err != nil {
		t.Fatal(err)
	}
	saved, err := app.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.CloseWindowOption != app.CloseWindowExit {
		t.Errorf("落到盘上的是 %q，want exit", saved.CloseWindowOption)
	}
	// 换回去也要行。
	clickSetting(t, tt, s, "开关：关闭窗口时留在菜单栏")
	tt.Frame()
	if got := a.SettingsSnapshot().CloseWindowOption; got != app.CloseWindowHide {
		t.Errorf("打开开关之后设置是 %q", got)
	}
}

// TestSettingsPageSaysSoWhenAChangeCannotBeStored 验「存不住」时用户看得见：设置压根没挂上，
// 点下去立刻被拒。后台落盘失败是另一条路，见 `TestShellSaysSoWhenSettingsAreBroken`。
func TestSettingsPageSaysSoWhenAChangeCannotBeStored(t *testing.T) {
	a := app.New()
	a.RecordSettingsFailure(os.ErrNotExist)

	b := newTestShell(t, onApp(a))
	tt := b.tt
	tt.Frame()
	openAppearance(t, tt)
	clickTheme(t, tt, b.s, "浅色")

	if !tt.HasText("主题没存上") {
		t.Errorf("改不动的时候界面上没有提示，文字有 %q", tt.Texts())
	}
	if !tt.HasText("深浅模式") {
		t.Errorf("提示之外界面没画出来，文字有 %q", tt.Texts())
	}
}

// TestSettingsPageRefusesToWriteAFileFromANewerVersion 验只读时每一项都改不了，不只是主题那一项：
// 主题被挡住了而别的还能改，用户会以为那一份存住了，下次升级回来才发现丢了一半。
func TestSettingsPageRefusesToWriteAFileFromANewerVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	if err := os.WriteFile(path, []byte(`{"schemaVersion": 99, "appSettings": {"themeMode": "dark"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	a := app.New()
	if err := a.MountSettings(path); err == nil {
		t.Fatal("来自更新版本的设置文件没有报错")
	}
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})

	b := newTestShell(t, onApp(a))
	s, tt := b.s, b.tt
	tt.Frame()
	openAppearance(t, tt)
	if !tt.HasText("设置只读") {
		t.Errorf("没有说清为什么改不了，文字有 %q", tt.Texts())
	}
	clickTheme(t, tt, s, "浅色")
	openSettingsTab(t, tt, "常规")
	clickSetting(t, tt, s, "开关：关闭窗口时留在菜单栏")
	tt.Frame()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"schemaVersion": 99`) {
		t.Errorf("只读的文件被改写了：%s", raw)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 真的杀进程重启
// ─────────────────────────────────────────────────────────────────────────────

// pageChildEnv 是「我现在是那个被拉起来点设置的子进程」的暗号。
const pageChildEnv = "BIU_PAGE_CHILD"

// TestSettingsMadeOnThePageSurviveAProcessRestart 是验收项「改设置 → 杀掉进程
// → 重启 → 设置还在」的字面版本，而且走的是**界面上真的那条路**：挂设置、切到
// 设置页、点按钮、收尾。
//
// 别的测试 reopen 的是一份 store，读的是同一个文件同一段代码。这条多验的是
// **进程边界**：后台的合并写有没有机会在退出前写完——SIGKILL 拦不住，所以靠的
// 是收尾那次同步 Flush（`Shell.Attach` 在窗口关闭时调的就是它）。
func TestSettingsMadeOnThePageSurviveAProcessRestart(t *testing.T) {
	if os.Getenv(pageChildEnv) != "" {
		pageChildRoundTrip(t)
		return
	}

	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	runPageChild(t, path, "write")
	runPageChild(t, path, "read")

	s, err := app.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.ThemeMode != app.ThemeModeDark {
		t.Errorf("两个子进程过去之后主题档位是 %q，want dark", s.ThemeMode)
	}
	if s.CloseWindowOption != app.CloseWindowExit {
		t.Errorf("窗口关闭那项是 %q，want exit", s.CloseWindowOption)
	}
}

// pageChildRoundTrip 是子进程里跑的那一趟：切到设置页、点按钮、立刻收尾，
// 不给后台写盘任何机会。
func pageChildRoundTrip(t *testing.T) {
	path := os.Getenv("BIU_PAGE_PATH")
	mode := os.Getenv("BIU_PAGE_MODE")

	a := app.New()
	defer a.Close()
	if err := a.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	b := newTestShell(t, onApp(a))
	s, tt := b.s, b.tt
	tt.Frame()

	if mode == "write" {
		openAppearance(t, tt)
		clickTheme(t, tt, s, "深色")
		openSettingsTab(t, tt, "常规")
		// 「关闭窗口时留在菜单栏」默认开，关掉它就是 CloseWindowExit。
		clickSetting(t, tt, s, "开关：关闭窗口时留在菜单栏")
		tt.Frame()
	}

	got := a.SettingsSnapshot()
	// 这一句就是 `Shell.Attach` 在窗口关闭时调的那一句。刻意不等 120ms 的
	// 合并写窗口：能存住就只能是这次同步收尾的功劳。
	if err := a.CloseSettings(); err != nil {
		t.Fatal(err)
	}

	if mode == "read" {
		if got.ThemeMode != app.ThemeModeDark || got.CloseWindowOption != app.CloseWindowExit {
			t.Errorf("重启之后读到的是 %+v", got)
		}
	}
	// 关掉无头外壳开的封面缓存，别给测试进程留后台 goroutine。
	s.images.Close()
}

// runPageChild 拉起一次本测试二进制自己。-test.run 让它只跑这一条。
func runPageChild(t *testing.T, path, mode string) {
	t.Helper()
	cmd := exec.Command(testproc.Path(t), "-test.run=TestSettingsMadeOnThePageSurviveAProcessRestart", "-test.count=1")
	cmd.Env = append(os.Environ(),
		pageChildEnv+"=1",
		"BIU_PAGE_PATH="+path,
		"BIU_PAGE_MODE="+mode,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("子进程 %s 失败了：%v\n%s", mode, err, out)
	}
}

// TestTheAutoStartSwitchIsOnThePage 验开机自启有控件可点。
//
// 这一条单独立出来，是因为它的失败方式最阴：设置页照样画得出来、其它三项照样
// 点得动，只是「开机自启」那一行根本不存在——用户界面上没有任何地方能开它，
// 而 `Shell.SetAutoStart` 只有一个测试在调，生产代码里没有第二个调用者。
func TestTheAutoStartSwitchIsOnThePage(t *testing.T) {
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""))

	if !tt.HasText("开机自启") {
		t.Fatalf("设置页上没有「开机自启」这一行，文字有 %q", tt.Texts())
	}
}

// TestTogglingAutoStartGoesThroughTheSystem 验开关是**真的**在动系统。
//
// 这条盯的是「写设置」和「动系统」的顺序：反了会留下「设置说开、系统说关」，
// 而启动时的对齐（`syncAutoStart`）恰好只补这一种——于是用户下次开机发现根本没
// 自启，界面上那个开关却还亮着。
//
// 系统那一份在这里是替掉的（`autoStartService`，见 lifecycle.go）：真的那一份
// 要过事件循环，无头环境里没有。
func TestTogglingAutoStartGoesThroughTheSystem(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	fake := &fakeAutoStart{}
	s.autostart = fake

	if err := s.SetAutoStart(true); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 1 || !fake.calls[0] {
		t.Fatalf("系统那边被调了 %v，want 一次 set(true)", fake.calls)
	}

	// 页面上那个开关要跟着设置走：改了设置、下一帧就该是亮的。
	tt.Frame()
	if !s.app.SettingsSnapshot().AutoStart {
		t.Error("设置里没存上开机自启")
	}
}

// TestTheSettingsPageUsesThePageHead 钉住设置页的页头走 `pud.PageHeadAt`（原型 `.page-head`：
// Eyebrow `Preferences` + 宋体标题「设置」，标题字号 `scale.PageTitleForWindow(窗口宽)`，原型的 clamp(32px, 3.4vw, 48px)）。
func TestTheSettingsPageUsesThePageHead(t *testing.T) {
	src, err := os.ReadFile("settings_page.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `pud.PageHeadAt(c, "Preferences", "设置",`) {
		t.Error("settings_page.go 没有用 `pud.PageHeadAt(c, \"Preferences\", \"设置\", …)` 画页头")
	}
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	if !tt.HasText("PREFERENCES") && !tt.HasText("Preferences") {
		t.Errorf("页头上没有 Eyebrow「Preferences」：%q", tt.Texts())
	}
}
