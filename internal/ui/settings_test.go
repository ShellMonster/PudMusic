package ui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo"
	mygoui "github.com/egoist/mygo/ui"
)

// 这些测试跑在真实的 Shell 上：真 App、真设置文件、真画一帧、读像素。
//
// theme_test.go 验的是主题函数本身，这里验的是**接线**：设置 → 每帧 → 像素。
// 断言的是画面上那个点的颜色，不是「调了哪个函数」——SPEC 的 Testing Decisions
// 就是这么要求的：用假的 `*mygoui.Theme` 顶替等于什么都没验，SetTheme 的坑恰恰
// 在「真框架怎么对待它」这件事上。

// sampleAt 是页面区域里一个点的坐标：侧栏（200 宽）右边、导航栏下面、播放条
// 上面。那里画的是窗口底色，也就是当前主题的 Background。
//
// ⚠️ 取的是页面区域里**空着**的那一块，不是页面中间。页面中间是空状态那两行
// 文字（y≈265~315，居中），而文字是逐帧排的：采到它上面，这一帧读到的是
// 抗锯齿的灰，下一帧可能读到白，同一条测试时而红时而绿。y=430 在那块下面、
// 播放条上面，哪一页都不会画到。
const (
	sampleX = 700
	sampleY = 430
)

// shellWithSettings 造一个「真 App + 盘上真有那份设置 + 真 Shell」，返回设置
// 文件的路径（持久化那几条断言要看它）。
func shellWithSettings(t *testing.T, mode app.ThemeMode, systemDark bool) (*mygoui.Tester, *Shell, *app.App, string) {
	t.Helper()

	// 先把设置写到盘上，再挂上去——走的是「启动时读盘」那条路。
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	st, err := app.OpenSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Update(func(s *app.Settings) { s.ThemeMode = mode }); err != nil {
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

	s := New(a)
	// 🔴 关掉切页动画，否则这个文件里的像素断言是随机的。
	//
	// Router 默认 TransitionAuto：切页是 250ms 的滑动，而 Tester 一帧就画完，
	// 那一帧永远停在半路——上一页正往左挪、下一页正往右进，中间那道竖直的
	// 分界线正好扫过页面区域。于是「点一下再读 (700,430) 那个点」这一次读到
	// 底色、下一次读到分界线，实测八次里挂两次。
	//
	// 这个文件验的是主题，不是切页动画，动画在这里只会添乱。
	s.router.Transition = mygoui.TransitionNone
	tt := mygoui.NewTester(s.View, 900, 600)
	tt.SetDark(systemDark)
	tt.Frame()
	return tt, s, a, path
}

// wantBackground 是「设置 + 系统外观」这一对该画出来的窗口底色。
func wantBackground(t *testing.T, a *app.App, systemDark bool) mygoui.Color {
	t.Helper()
	return BuildTheme(a.SettingsSnapshot(), systemDark).Background
}

// assertBackground 断言画面上那个采样点就是主题的底色。
func assertBackground(t *testing.T, tt *mygoui.Tester, want mygoui.Color, what string) {
	t.Helper()
	c := tt.Image().RGBAAt(sampleX, sampleY)
	if c.R != want.R || c.G != want.G || c.B != want.B || c.A != want.A {
		t.Errorf("%s：画面那个点是 %v，want %v", what,
			mygoui.Color{R: c.R, G: c.G, B: c.B, A: c.A}, want)
	}
}

// TestShellPaintsTheThemeThatTheSettingsSay 验「切主题后渲染出来的主题真的
// 变了」——读的是像素，不是「调了哪个函数」。
//
// 断言的是像素，理由同文件开头：主题这件事的坑全在「真框架怎么对待它」，
// 在 view 里读 `c.Theme()` 读到的是应用层已经看到的东西，比像素浅一层。
func TestShellPaintsTheThemeThatTheSettingsSay(t *testing.T) {
	tt, _, a, _ := shellWithSettings(t, app.ThemeModeDark, true)
	assertBackground(t, tt, wantBackground(t, a, true), "设置是深色、系统也是深色")
	dark := tt.Image().RGBAAt(sampleX, sampleY)

	// 用户选了浅色。系统还是深色——如果这一帧画出来是深色，就说明设置根本没
	// 进到这一帧的主题里。
	if err := a.SetThemeMode(app.ThemeModeLight); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	assertBackground(t, tt, wantBackground(t, a, true), "设置切成浅色、系统还是深色")

	if light := tt.Image().RGBAAt(sampleX, sampleY); light == dark {
		t.Errorf("切了档位之后画面一点没变：还是 %v", dark)
	}

	// 切回深色也跟得上：三档不是只有两头能用。
	if err := a.SetThemeMode(app.ThemeModeDark); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	assertBackground(t, tt, wantBackground(t, a, true), "切回深色")
}

// TestShellKeepsTheThemeAfterClickingAround 是 PLAN 阶段 1 的验收原话：
// 「切完主题随便点几下别的地方，主题不会自己跳回系统色」。
//
// 系统是深色、设置选了浅色，差得越远越说明问题。这条在 Tester 里是**稳定**
// 复现的，不是碰运气：MyGo 每一遍 pass 开头都把主题重置回跟随系统的那份
// （`ui/context.go:119`），而实测**一次 `tt.Frame()` 就会跑两遍 view**，点击
// 被消费时还会再多一遍。只在「档位变了」的那一遍里 SetTheme 的写法，第一遍
// 画对了，第二遍就被悄悄打回系统色——不报错、不崩溃、屏幕上一闪而过。
func TestShellKeepsTheThemeAfterClickingAround(t *testing.T) {
	tt, _, a, _ := shellWithSettings(t, app.ThemeModeLight, true)
	if BuildTheme(a.SettingsSnapshot(), true).Dark {
		t.Fatal("起点就不对：设置是浅色，却拼出了深色主题")
	}
	assertBackground(t, tt, wantBackground(t, a, true), "起点")

	// 真的去点别的地方：侧栏那一条是这一版唯一的导航入口。
	// tt.Click 内部是 PointerDown / PointerUp 各跑一整帧，返回时已经走过
	// 「点击被消费」的那一遍；再补一次 Frame 是把再下一遍也走完。
	for i := range 3 {
		if err := tt.Click("稍后再看"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		assertBackground(t, tt, wantBackground(t, a, true), "点别的地方之后（第 "+string(rune('1'+i))+" 次）")
	}
	if !tt.HasText(lookup("/later").Empty.Title) {
		t.Errorf("顺带验一下按钮确实点得动，文字有 %q", tt.Texts())
	}

	// 系统外观变了也不该动我们——档位是「浅色」就该一直是浅色。
	tt.SetDark(false)
	tt.Frame()
	assertBackground(t, tt, wantBackground(t, a, false), "系统切回浅色之后")
}

// TestShellCrashPageCarriesTheTheme 验错误页也是一帧，也得有主题。
//
// 崩溃后的错误页如果漏了 ApplyTheme，会用系统色画出来：用户选深色时崩了，
// 看到一张亮色的错误页，像换了个应用。
func TestShellCrashPageCarriesTheTheme(t *testing.T) {
	tt, s, a, _ := shellWithSettings(t, app.ThemeModeLight, true)
	assertBackground(t, tt, wantBackground(t, a, true), "正常页")

	s.crash = "坏掉了"
	tt.Frame()
	if !tt.HasText("界面出错了") {
		t.Fatalf("没有画错误页，文字有 %q", tt.Texts())
	}
	assertBackground(t, tt, wantBackground(t, a, true), "错误页")
}

// TestShellSaysSoWhenSettingsAreBroken 验设置读写失败不让界面崩，而且用户
// 看得见。
//
// 静默地按默认值开下去是最坏的一种：用户改了半天的设置，下次打开发现全没了，
// 而且从来没人告诉过他。
func TestShellSaysSoWhenSettingsAreBroken(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	if err := os.WriteFile(path, []byte("这不是 JSON"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := app.New()
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})
	if err := a.MountSettings(path); err == nil {
		t.Fatal("坏掉的设置文件没有报错")
	}

	tt := mygoui.NewTester(New(a).View, 900, 600)
	tt.Frame() // 这一帧要是 panic 了，测试进程自己就没了

	if !tt.HasText("设置文件已损坏") {
		t.Errorf("界面上没有提示设置出了问题，文字有 %q", tt.Texts())
	}
	// 提示归提示，界面照常画。
	if !tt.HasText("Biu") {
		t.Errorf("提示之外界面没画出来，文字有 %q", tt.Texts())
	}

	// 同一个错不重复弹：每帧都问一次，不去重的话屏幕上会一直闪同样的话。
	before := len(tt.Texts())
	for range 5 {
		tt.Frame()
	}
	if after := len(tt.Texts()); after > before+1 {
		t.Errorf("同一个设置错误弹了 %d 次提示", after-before)
	}
}

// TestMountSettingsUsesTheMyGoAppDataDirectory 验设置落在 MyGo 自己的应用
// 数据目录里。
//
// 这条是防「有人后来把它换成自己拼的 ~/Library/Application Support/...」
// 用的：拼错的后果不是报错，是「读不到文件」，而读不到文件按第一次运行处理
// ——用户只会发现设置一直存不住，换台机器再丢一次。
//
// `SetPath` 是框架给的测试入口（`paths.go:72`）。顺带说一句：`App.Name()` 在
// `go test` 下会退化成可执行文件名（打包时的 ldflags 注入不生效），所以单测
// 里不能靠 `App.Path` 的默认值写死目录。
func TestMountSettingsUsesTheMyGoAppDataDirectory(t *testing.T) {
	dir := t.TempDir()
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })

	a := app.New()
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})
	if err := MountSettings(a); err != nil {
		t.Fatal(err)
	}
	if err := a.SetThemeMode(app.ThemeModeDark); err != nil {
		t.Fatal(err)
	}
	if err := a.CloseSettings(); err != nil {
		t.Fatal(err)
	}

	// 真文件、真名字，不是内存里转一圈。
	got, err := app.LoadSettings(filepath.Join(dir, app.SettingsFileName))
	if err != nil {
		t.Fatalf("设置没落在 %s 里：%v", dir, err)
	}
	if got.ThemeMode != app.ThemeModeDark {
		t.Errorf("落到盘上的档位是 %q，want dark", got.ThemeMode)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 用户真的能切
// ─────────────────────────────────────────────────────────────────────────────

// clickTheme 点主题那三个按钮之一。
func clickTheme(t *testing.T, tt *mygoui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatalf("点「%s」失败：%v（界面上还有切换主题的入口吗）", label, err)
	}
	tt.Frame()
}

// TestUserCanSwitchTheThemeFromTheInterface 是「主题三档能切」这条验收。
//
// ⚠️ 这里点的是**界面上的按钮**，不是直接调 `a.SetThemeMode`。后者只能证明
// 状态层有那个方法，证明不了界面上真有一个入口——三档摆在 `theme.go` 里没人
// 调用过，就是这么漏过去的。
//
// 每一档都挑「和系统外观相反」的时刻去断言：颜色差得越远，越说明画出来的是
// 设置里的档位而不是系统色。
func TestUserCanSwitchTheThemeFromTheInterface(t *testing.T) {
	// 系统是深色，起点是「跟随系统」。
	tt, _, a, path := shellWithSettings(t, app.ThemeModeSystem, true)
	assertBackground(t, tt, wantBackground(t, a, true), "起点：跟随系统，系统是深色")

	clickTheme(t, tt, "浅色")
	if got := a.SettingsSnapshot().ThemeMode; got != app.ThemeModeLight {
		t.Fatalf("点了「浅色」之后设置是 %q", got)
	}
	assertBackground(t, tt, wantBackground(t, a, true), "点「浅色」之后")

	// 系统外观变了也不该跟着动——档位是「浅色」就该一直是浅色。
	tt.SetDark(false)
	tt.Frame()
	assertBackground(t, tt, wantBackground(t, a, false), "系统切回浅色之后")

	// 点「深色」，并且**真的落到了盘上**（切换和持久化是同一条路）。
	clickTheme(t, tt, "深色")
	assertBackground(t, tt, wantBackground(t, a, false), "点「深色」之后，系统此时是浅色")
	if err := a.FlushSettings(); err != nil {
		t.Fatal(err)
	}
	saved, err := app.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ThemeMode != app.ThemeModeDark {
		t.Errorf("点完落到盘上的档位是 %q，want dark", saved.ThemeMode)
	}

	// 点回「跟随系统」：系统此刻是浅色，就该跟着变回浅色。
	clickTheme(t, tt, "跟随系统")
	if got := a.SettingsSnapshot().ThemeMode; got != app.ThemeModeSystem {
		t.Fatalf("点回「跟随系统」之后设置是 %q", got)
	}
	assertBackground(t, tt, wantBackground(t, a, false), "点回「跟随系统」之后，系统是浅色")
}

// TestThemeSwitchSaysWhyItCannotSwitch 验设置只读（来自更新的版本）时按钮是
// 关着的，并且说清了原因。
//
// 与其让用户按了没反应，不如把按钮关掉并说明——这一版的设置一个字都不能写
// 回去，硬按只会让人以为是程序坏了。
func TestThemeSwitchSaysWhyItCannotSwitch(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	if err := os.WriteFile(path, []byte(`{"schemaVersion": 99, "appSettings": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	a := app.New()
	if err := a.MountSettings(path); !errors.Is(err, app.ErrSettingsFromNewer) {
		t.Fatalf("拿到的是 %v，want ErrSettingsFromNewer", err)
	}
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})

	tt := mygoui.NewTester(New(a).View, 900, 600)
	tt.SetDark(false)
	tt.Frame()

	if !tt.HasText("设置只读") {
		t.Errorf("界面上没有说清为什么切不了，文字有 %q", tt.Texts())
	}
	clickTheme(t, tt, "深色")
	if got := a.SettingsSnapshot().ThemeMode; got != app.ThemeModeSystem {
		t.Errorf("只读的设置被改了：%q", got)
	}
}

// TestShellSaysSoWhenSettingsCannotBeStored 验「设置存不住」时用户看得见。
//
// 这一条盯的是一个具体的坑：拿不到应用数据目录时没有 store 可挂，界面上什么
// 都不会出现，只有日志里有一行——而日志只有排障的人看得到。用户看到的现象是
// 「改了主题，下次打开又变回来了」，中间什么提示都没有。
func TestShellSaysSoWhenSettingsCannotBeStored(t *testing.T) {
	t.Run("记下失败之后界面有提示", func(t *testing.T) {
		a := app.New()
		t.Cleanup(a.Close)
		a.RecordSettingsFailure(errors.New("拿不到应用数据目录，这次的改动关掉应用就没了"))

		tt := mygoui.NewTester(New(a).View, 900, 600)
		tt.Frame()
		if !tt.HasText("这次的改动关掉应用就没了") {
			t.Errorf("界面上没有提示设置存不住，文字有 %q", tt.Texts())
		}
		// 提示归提示，界面照常画：主题还是能用默认的那份。
		assertBackground(t, tt, wantBackground(t, a, false), "存不住时")
	})

	t.Run("拿不到应用数据目录", func(t *testing.T) {
		// os.UserConfigDir 只认 XDG_CONFIG_HOME 和 HOME，两个都清掉它就只能
		// 报错——这是「拿不到目录」唯一的真实成因。
		t.Setenv("HOME", "")
		t.Setenv("XDG_CONFIG_HOME", "")

		a := app.New()
		t.Cleanup(a.Close)
		if err := MountSettings(a); err == nil {
			t.Skip("这个环境上 os.UserConfigDir 不靠 HOME/XDG_CONFIG_HOME，跳过")
		}

		tt := mygoui.NewTester(New(a).View, 900, 600)
		tt.Frame()
		if !tt.HasText("拿不到应用数据目录") {
			t.Errorf("拿不到目录这件事界面上没有提示，文字有 %q", tt.Texts())
		}
	})
}
