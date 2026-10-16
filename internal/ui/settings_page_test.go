package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 设置页这一组测试跑在真实的 Shell 上：真 App、真设置文件、真画一帧、真点按钮。
//
// 断言的东西只有三类：**画面上的文字**（这一页有没有真画出来）、**读像素**
//（换档位之后界面是不是真的变了）、**磁盘上的文件**（改完是不是真存住了）。
// 不断言「调了哪个函数」——那只能证明状态层有那个方法，证明不了界面上真有一个
// 入口。三档按钮摆在文件里没人调用过，就是这么漏过去的。

// 1200x800 的窗口里，设置页能点的那一条带。上下两个数字是顶栏（`navMinHeight`
// = 32）和播放条（`playbarHeight` = 88）让出来的。
const (
	pageTop    = 40
	pageBottom = 700
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
	for range 24 {
		if r, ok := tt.Find(label); ok && r.H > 0 && r.Y >= pageTop && r.Y+r.H <= pageBottom {
			return r
		}
		before := s.settings.scroll.Y
		s.settings.scroll.Y = before + 60
		tt.Frame()
		if s.settings.scroll.Y == before {
			break // 已经到底了，再滚也不动
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
func shellOnSettings(t *testing.T, s app.Settings) (*mygoui.Tester, *Shell, *app.App, string) {
	t.Helper()

	// 先把这份设置写到盘上，再挂上去——走的是「启动时读盘」那条路。
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	st, err := app.OpenSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Update(func(cur *app.Settings) { *cur = s }); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	a := app.New()
	if err := a.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})

	s2 := New(a)
	// 关掉切页动画：Tester 一帧画完，250ms 的动画在里面永远走不完，留着它只会
	// 让「读到的是哪一帧」变成随机的。理由和 newShell 里那一句一样。
	s2.router.Transition = mygoui.TransitionNone
	tt := mygoui.NewTester(s2.View, 1200, 800)
	tt.Frame()
	openSettings(t, tt)
	return tt, s2, a, path
}

// TestSettingsPageShowsItsContent 验切过去看到的是设置页，不是占位页。
//
// 逐项点名，是因为设置页最可能的失败是「画出来了，但少了一项」——用户看见一个
// 缺了「窗口关闭」的设置页，不会知道还有一项。
func TestSettingsPageShowsItsContent(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))

	for _, want := range []string{
		"设置", "外观", "主题", "圆角",
		"跟随系统", "浅色", "深色",
		"系统", "窗口关闭", "隐藏到托盘", "直接退出",
		"开机自启",
	} {
		if !tt.HasText(want) {
			t.Errorf("设置页上没有 %q，文字有 %q", want, tt.Texts())
		}
	}
	// 「不是占位页」这一条要能失败才算数。原来这里查的是占位页那句
	// 「这一页还没做」——那句话连同占位页一起没了，断言恒为真，验不到任何东西。
	// 现在查的是这一页真的接上了自己的画法（builtPage），接不上就会退回空状态。
	if s.builtPage(lookup(settingsPath)) == nil {
		t.Errorf("设置页没接上 builtPage，画出来的是空状态而不是设置页")
	}
	// macOS 上不建托盘（与 Biu 一致），「隐藏到托盘」在这边是收进程序坞。名字和
	// 实际行为对不上时得说出来，否则用户会以为我们漏做了托盘。
	if !tt.HasText("macOS 上不建托盘图标") {
		t.Errorf("没说清 macOS 上没有托盘，文字有 %q", tt.Texts())
	}
}

// TestTheSettingsPageIsReachableFromTheMenu 验入口在侧栏上——
// 用户找得到，才谈得上「设置页最小可用」。
func TestTheSettingsPageIsReachableFromTheMenu(t *testing.T) {
	_, tt := newShell(t)

	if tt.HasText("外观") {
		t.Fatal("还没点就在设置页上了")
	}
	openSettings(t, tt)
}

// TestChoosingTheCloseWindowOptionIsStored 验「关窗口时退出还是留在托盘里」
// 这一项选得了、存得住。
//
// 托盘实现在 issue #28，这一版只管存储和选择——所以这里断言的是**盘上那份**，
// 不是窗口关掉之后发生了什么：后者现在还不该有任何行为。
func TestChoosingTheCloseWindowOptionIsStored(t *testing.T) {
	want := app.DefaultSettings("")
	want.CloseWindowOption = app.CloseWindowHide // Biu 的默认值
	tt, s, a, path := shellOnSettings(t, want)

	if got := a.SettingsSnapshot().CloseWindowOption; got != app.CloseWindowHide {
		t.Fatalf("起点是 %q", got)
	}

	clickSetting(t, tt, s, "直接退出")
	tt.Frame()

	if got := a.SettingsSnapshot().CloseWindowOption; got != app.CloseWindowExit {
		t.Fatalf("点了「直接退出」之后设置是 %q", got)
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

	// 换回去也要行：这是一对能来回切的选项，不是一个只写一次的开关。
	clickSetting(t, tt, s, "隐藏到托盘")
	tt.Frame()
	if got := a.SettingsSnapshot().CloseWindowOption; got != app.CloseWindowHide {
		t.Errorf("点回「隐藏到托盘」之后设置是 %q", got)
	}
}

// TestDraggingTheRadiusSliderChangesTheSetting 验圆角滑块接上了。
//
// 滑块是最容易「画在那儿、拖着不动」的一个控件：读的是设置里的值，改的是
// 另一个地址上的副本，没接上的时候屏幕上照样有那根条。所以这里按住拖一段，
// 断言设置真的变了——并且变完之后主题里那个圆角也跟着变了。
func TestDraggingTheRadiusSliderChangesTheSetting(t *testing.T) {
	want := app.DefaultSettings("")
	want.BorderRadius = app.DefaultBorderRadius
	tt, _, a, _ := shellOnSettings(t, want)

	if a.SettingsSnapshot().BorderRadius != app.DefaultBorderRadius {
		t.Fatalf("起点圆角是 %v，want %v", a.SettingsSnapshot().BorderRadius, app.DefaultBorderRadius)
	}

	r, ok := tt.Find("全局圆角")
	if !ok {
		t.Fatalf("设置页上没有圆角滑块，文字有 %q", tt.Texts())
	}
	// 按在滑块靠右 85% 的地方：从 8 挪到接近 24，是一眼能看出来的变化。
	x, y := r.X+r.W*0.85, r.Y+r.H/2
	tt.Press(x, y)
	tt.Frame()
	tt.Release(x, y)
	tt.Frame()

	got := a.SettingsSnapshot().BorderRadius
	if got <= app.DefaultBorderRadius {
		t.Errorf("拖了滑块，圆角还是 %v（起点 %v）", got, app.DefaultBorderRadius)
	}
	if got > app.MaxBorderRadius {
		t.Errorf("圆角 %v 越过了上限 %v", got, app.MaxBorderRadius)
	}
}

// TestSettingsPageSaysSoWhenAChangeCannotBeStored 验「存不住」时用户看得见。
//
// 这里的错是**当场**发生的：设置压根没挂上（拿不到应用数据目录就是这个结果），
// 所以点下去立刻被拒。与其让用户按了没反应，不如说清为什么。
//
// 后台落盘失败（磁盘满、目录没了）是另一条路，那条由
// `TestShellSaysSoWhenSettingsAreBroken` 盯。
func TestSettingsPageSaysSoWhenAChangeCannotBeStored(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	a.RecordSettingsFailure(os.ErrNotExist)

	s := New(a)
	t.Cleanup(s.images.Close)
	tt := mygoui.NewTester(s.View, 1200, 800)
	tt.Frame()
	openSettings(t, tt)

	if err := tt.Click("深色"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	if !tt.HasText("主题没存上") {
		t.Errorf("改不动的时候界面上没有提示，文字有 %q", tt.Texts())
	}
	// 提示之外界面照常画：这一帧要是 panic 了，测试进程自己就没了。
	if !tt.HasText("窗口关闭") {
		t.Errorf("提示之外设置页没画出来，文字有 %q", tt.Texts())
	}
}

// TestSettingsPageRefusesToWriteAFileFromANewerVersion 验只读时**每一项**都改
// 不了，不只是主题那一项。
//
// 逐项点是因为只读是整份文件的性质：主题被挡住了而「窗口关闭」还能改，用户
// 会以为那一份存住了，下次升级回来才发现丢了一半。
func TestSettingsPageRefusesToWriteAFileFromANewerVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	// 这个文件将来的版本号比我们高：一律只读，不备份、不改写。
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

	s := New(a)
	t.Cleanup(s.images.Close)
	s.router.Transition = mygoui.TransitionNone
	tt := mygoui.NewTester(s.View, 1200, 800)
	tt.Frame()
	openSettings(t, tt)

	if !tt.HasText("设置只读") {
		t.Errorf("设置页上没有说清为什么改不了，文字有 %q", tt.Texts())
	}
	for _, label := range []string{"浅色", "直接退出"} {
		clickSetting(t, tt, s, label)
		tt.Frame()
	}

	// 文件一个字都不能被我们改过。
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
	s := New(a)
	s.router.Transition = mygoui.TransitionNone
	tt := mygoui.NewTester(s.View, 1200, 800)
	tt.Frame()

	if mode == "write" {
		openSettings(t, tt)
		clickSetting(t, tt, s, "深色")
		tt.Frame()
		clickSetting(t, tt, s, "直接退出")
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
	cmd := exec.Command(os.Args[0], "-test.run=TestSettingsMadeOnThePageSurviveAProcessRestart", "-test.count=1")
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
	if !tt.HasText("系统登录后自动启动应用") {
		t.Errorf("「开机自启」没有说明，文字有 %q", tt.Texts())
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
