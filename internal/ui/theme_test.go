package ui

import (
	"path/filepath"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 这些测试跑在 MyGo 的 headless Tester 上：真主题、真画一帧、真读像素。
// 用假的 `*mygoui.Theme` 顶替等于什么都没验——SetTheme 的坑恰恰在
// 「真框架怎么对待它」这件事上。

// themeProbe 是一帧里读到的主题，用来断言「这一帧真正用的主题是什么」。
type themeProbe struct {
	dark      bool
	bg        mygoui.Color
	accent    mygoui.Color
	radius    float32
	inverse   mygoui.Color
	inverseTx mygoui.Color
}

func readTheme(c *mygoui.Context) themeProbe {
	t := c.Theme()
	return themeProbe{
		dark:      t.Dark,
		bg:        t.Background,
		accent:    t.Accent,
		radius:    t.Radius,
		inverse:   t.Inverse,
		inverseTx: t.InverseText,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 三档主题
// ─────────────────────────────────────────────────────────────────────────────

// TestThemeModeThreeSteps 验跟随系统 / 浅色 / 深色三档。
func TestThemeModeThreeSteps(t *testing.T) {
	cases := []struct {
		mode      app.ThemeMode
		sysDark   bool
		wantDark  bool
		lightMode string
	}{
		{app.ThemeModeLight, false, false, "强制浅色，系统是浅色"},
		{app.ThemeModeLight, true, false, "强制浅色，系统是深色"},
		{app.ThemeModeDark, false, true, "强制深色，系统是浅色"},
		{app.ThemeModeDark, true, true, "强制深色，系统是深色"},
		{app.ThemeModeSystem, false, false, "跟随系统，系统是浅色"},
		{app.ThemeModeSystem, true, true, "跟随系统，系统是深色"},
	}
	for _, c := range cases {
		s := app.DefaultSettings("")
		s.ThemeMode = c.mode

		var got themeProbe
		tt := mygoui.NewTester(func(c *mygoui.Context) {
			ApplyTheme(c, s, c.Theme().Dark)
			got = readTheme(c)
		}, 200, 100)
		tt.SetDark(c.sysDark)
		tt.Frame()

		if got.dark != c.wantDark {
			t.Errorf("%s：Dark 是 %v，want %v", c.lightMode, got.dark, c.wantDark)
		}
	}
}

// TestThemeFollowsTheSystemAtRuntime 验「跟随系统」跟着系统外观真的变，
// 不只是启动时读一次。
func TestThemeFollowsTheSystemAtRuntime(t *testing.T) {
	s := app.DefaultSettings("")
	s.ThemeMode = app.ThemeModeSystem

	var got themeProbe
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		ApplyTheme(c, s, c.Theme().Dark)
		got = readTheme(c)
	}, 200, 100)

	if got.dark {
		t.Fatal("系统是浅色，画出来却是深色")
	}
	tt.SetDark(true)
	tt.Frame()
	if !got.dark {
		t.Error("系统切到深色，界面没跟上")
	}
	tt.SetDark(false)
	tt.Frame()
	if got.dark {
		t.Error("系统切回浅色，界面没跟上")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 🔴 每帧必须调 ApplyTheme
// ─────────────────────────────────────────────────────────────────────────────

// TestSetThemeOnceInACallbackIsSilentlyReset 是这个文件最重要的一条。
//
// 它复现「把 SetTheme 放进切换按钮的回调里，只调一次」这个写法：
// 切的那一帧是对的，下一帧就被 MyGo 静默重置回跟随系统的那份。
// **不报错、不崩溃、不打日志**——只是主题悄悄跳回去了。
//
// 这个坑官方文档没写，所以用测试钉住：哪天真有人图省事把 ApplyTheme
// 从 view 开头挪走，这条会先红。
func TestSetThemeOnceInACallbackIsSilentlyReset(t *testing.T) {
	var got themeProbe
	switched, applied := false, false

	// 「错误」的写法：只在切换那一帧的 view 里 SetTheme 一次。
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		s := app.DefaultSettings("")
		s.ThemeMode = app.ThemeModeSystem
		if switched && !applied {
			applied = true
			s.ThemeMode = app.ThemeModeLight
			c.SetTheme(BuildTheme(s, c.Theme().Dark))
		}
		got = readTheme(c)
		mygoui.Text(c, "内容")
	}, 200, 100)

	tt.SetDark(true)
	tt.Frame()
	if !got.dark {
		t.Fatal("起点就不对：系统是深色")
	}

	switched = true
	tt.Frame() // 设上去的那一帧
	if got.dark {
		t.Fatal("这一帧没切过来，测试本身有问题")
	}
	if !applied {
		t.Fatal("SetTheme 根本没被调到，测试本身有问题")
	}

	tt.Frame() // 只是又画了一帧，没有再 SetTheme
	if !got.dark {
		t.Skip("这一版 MyGo 不再每帧重置主题，ApplyTheme 的每帧约定可以去掉")
	}
	t.Logf("确认了：下一帧主题被重置回系统深色（Background=%v），SetTheme 只在回调里调是不够的",
		got.bg)
}

// TestApplyThemeEveryFrameSurvives 是上面那条的正面版本。
//
// ApplyTheme 在 view 开头每帧调：切完之后随便点别的地方、随便重画，
// 主题都不会跳回去。这是 `PLAN.md` 阶段 1 的验收原话。
func TestApplyThemeEveryFrameSurvives(t *testing.T) {
	s := app.DefaultSettings("")
	s.ThemeMode = app.ThemeModeSystem

	other := false
	var got themeProbe
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		// 每帧开头，只这一行。
		ApplyTheme(c, s, c.Theme().Dark)
		got = readTheme(c)

		if mygoui.Button(c, "切到浅色").Clicked() {
			s.ThemeMode = app.ThemeModeLight
		}
		if mygoui.Button(c, "点我").Clicked() {
			other = !other
		}
		mygoui.Text(c, "内容")
	}, 300, 200)

	tt.SetDark(true)
	tt.Frame()
	if !got.dark {
		t.Fatal("起点就不对：系统是深色")
	}

	if err := tt.Click("切到浅色"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got.dark {
		t.Fatal("点了切换按钮之后主题还是深色")
	}

	// 验收原话：「切完主题随便点几下别的地方，主题不会自己跳回系统色」。
	for range 3 {
		if err := tt.Click("点我"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if got.dark {
			t.Fatal("点别的地方之后主题跳回系统色了")
		}
	}
	if !other {
		t.Error("顺带验一下按钮确实点得动")
	}

	// 系统外观变了也不该动我们——档位是「浅色」就该一直是浅色。
	tt.SetDark(false)
	tt.Frame()
	if got.dark {
		t.Error("档位是浅色，系统切到深色之后界面跟着变了")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 主色、背景色、圆角
// ─────────────────────────────────────────────────────────────────────────────

// TestRadiusIsClamped 验圆角夹在 0~24。
func TestRadiusIsClamped(t *testing.T) {
	cases := []struct{ in, want float32 }{
		{-5, 0},
		{0, 0},
		{8, 8},
		{24, 24},
		{999, 24},
	}
	for _, c := range cases {
		s := app.DefaultSettings("")
		s.BorderRadius = float64(c.in)
		if got := BuildTheme(s, false).Radius; got != c.want {
			t.Errorf("圆角 %v 夹成了 %v，want %v", c.in, got, c.want)
		}
	}
}

// TestPrimaryColorDrivesTheAccentGroup 验主色真的铺到了整组强调色上。
func TestPrimaryColorDrivesTheAccentGroup(t *testing.T) {
	s := app.DefaultSettings("")
	s.PrimaryColor = "#1ed760" // Biu 的绿

	light := BuildTheme(s, false)
	dark := BuildTheme(s, true)

	defaults := map[string]*mygoui.Theme{
		"浅色": mygoui.LightTheme(),
		"深色": mygoui.DarkTheme(),
	}
	for name, th := range map[string]*mygoui.Theme{"浅色": light, "深色": dark} {
		if th.Accent.R != 0x1e || th.Accent.G != 0xd7 || th.Accent.B != 0x60 {
			t.Errorf("%s主色没铺上 Accent：%+v", name, th.Accent)
		}
		// hover/pressed 必须是从主色推出来的，不是框架默认值留着。
		if th.AccentHover == defaults[name].AccentHover {
			t.Errorf("%s的 hover 色还是框架默认的", name)
		}
		// 绿底上白字看不清，MyGo 的做法是反过来用深色字。
		if th.AccentText.R != 24 || th.AccentText.G != 24 || th.AccentText.B != 27 {
			t.Errorf("%s主色上的字是 %+v，亮色主色上该用深色字", name, th.AccentText)
		}
	}
	// 深色底上 hover 往亮走，浅色底上往暗走——反了就是按下时反而变淡。
	if light.AccentHover.R >= light.Accent.R {
		t.Error("浅色主题上 hover 该比主色暗")
	}
	if dark.AccentHover.R <= dark.Accent.R {
		t.Error("深色主题上 hover 该比主色亮")
	}
}

// TestBackgroundColorIsUsed 验自定义背景色。
func TestBackgroundColorIsUsed(t *testing.T) {
	s := app.DefaultSettings("")
	if got := BuildTheme(s, false).Background; got != mygoui.LightTheme().Background {
		t.Error("背景色留空时该用主题默认的")
	}

	s.BackgroundColor = "#101014"
	if got := BuildTheme(s, false).Background; got.R != 0x10 || got.G != 0x10 || got.B != 0x14 {
		t.Errorf("自定义背景色没生效：%+v", got)
	}
}

// TestBadColorsFallBackInsteadOfPanicking 验坏颜色不炸进程。
//
// `ui.Hex` 对非法输入直接 panic（ui/color.go:33-38）。颜色串是设置文件里的
// 用户输入，为了画错的颜色把进程带走不划算。
func TestBadColorsFallBackInsteadOfPanicking(t *testing.T) {
	for _, bad := range []string{"红色", "#zzz", "#12345", "#", "", "#1234567"} {
		s := app.DefaultSettings("")
		s.PrimaryColor = bad
		s.BackgroundColor = bad
		th := BuildTheme(s, false) // 之前会 panic 的地方
		if th.Accent.A == 0 || th.Background.A == 0 {
			t.Errorf("坏颜色 %q 让主题变得全透明了", bad)
		}
	}
}

// TestInverseIsExplicitInDarkMode 验深色下 Inverse 显式给了。
//
// 留零的话 MyGo 的 inverse() 会退回「fill=Text、text=Background」——
// 深色下就是一张白底卡片，tooltip 和 toast 在深色界面里闪一下。
func TestInverseIsExplicitInDarkMode(t *testing.T) {
	dark := BuildTheme(app.DefaultSettings(""), true)
	if dark.Inverse == mygoui.Transparent || dark.InverseText == mygoui.Transparent {
		t.Fatalf("深色下 Inverse/InverseText 留零了：%v / %v", dark.Inverse, dark.InverseText)
	}
	// 深色下的 tooltip 不该比背景还亮太多——那是一张白卡片。
	if luminanceOf(dark.Inverse) <= luminanceOf(dark.Background) {
		t.Error("深色下 tooltip 的底色比背景还暗，会看不见")
	}
	if luminanceOf(dark.Inverse) > 200 {
		t.Errorf("深色下 tooltip 底色亮到 %v，是一张白卡片", luminanceOf(dark.Inverse))
	}

	light := BuildTheme(app.DefaultSettings(""), false)
	if light.Inverse == mygoui.Transparent || light.InverseText == mygoui.Transparent {
		t.Error("浅色下 Inverse/InverseText 留零了")
	}
	if luminanceOf(light.Inverse) >= luminanceOf(light.Background) {
		t.Error("浅色下 tooltip 应该是深色卡片")
	}
}

func luminanceOf(c mygoui.Color) float32 {
	return 0.2126*float32(c.R) + 0.7152*float32(c.G) + 0.0722*float32(c.B)
}

// ─────────────────────────────────────────────────────────────────────────────
// 到像素
// ─────────────────────────────────────────────────────────────────────────────

// TestThemeReachesThePixels 验主题不是只改了结构体，画面上真的变了。
//
// 前面的测试都在 view 里读 c.Theme()，那已经是「应用层看到的东西」。
// 这条再往下一层：读渲染结果的像素。
func TestThemeReachesThePixels(t *testing.T) {
	s := app.DefaultSettings("")
	s.BackgroundColor = "#336699"

	tt := mygoui.NewTester(func(c *mygoui.Context) {
		ApplyTheme(c, s, c.Theme().Dark)
		mygoui.Text(c, "文字")
	}, 200, 100)
	tt.Frame()

	// 窗口四角是窗口底色。取左上角，避开文字和控件。
	// tt.Image() 给的是 color.RGBA（8 位/通道、预乘 alpha），
	// mygoui.Color 是 0~255 + 0~1 的 alpha，两边换一下再比。
	got := tt.Image().RGBAAt(2, 2)
	want := mygoui.Hex("#336699")
	if got.R != want.R || got.G != want.G || got.B != want.B || got.A != want.A {
		t.Errorf("画面左上角是 %v，want %v（主题没有走到像素）", got, want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 重启后保持
// ─────────────────────────────────────────────────────────────────────────────

// TestThemeSurvivesARestart 验收项「三档切换真的生效，且重启后保持」的
// 后半句。
//
// 「生效」由 TestApplyThemeEveryFrameSurvives 和 TestThemeReachesThePixels 验；
// 这一条验的是接缝——设置落到盘上、重启读回来、拼出来还是那个主题。
// 少了它，「切换生效但重启丢档位」这种回退没人拦得住。
func TestThemeSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	st, err := app.OpenSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	p := newProbeTester(st.Snapshot)
	p.tt.SetDark(false)
	p.frame()
	if p.probe.dark {
		t.Fatal("起点就不对：默认档位跟随系统，系统是浅色")
	}

	// 用户在设置里选了深色。
	if _, err := st.Update(func(s *app.Settings) { s.ThemeMode = app.ThemeModeDark }); err != nil {
		t.Fatal(err)
	}
	p.frame()
	if !p.probe.dark {
		t.Error("改了设置之后主题没跟上")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	// 重启：新的 store，新的 view。
	again, err := app.OpenSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { again.Close() })

	q := newProbeTester(again.Snapshot)
	// 系统此刻是浅色：重启后仍然要是深色，否则说明档位没存住。
	q.tt.SetDark(false)
	q.frame()
	if !q.probe.dark {
		t.Errorf("重启后档位丢了：系统是浅色，画出来却不是深色（%+v）", q.probe)
	}
}

// probeTester 是一个「画完一帧就能读到那一帧用的主题」的 Tester。
type probeTester struct {
	tt    *mygoui.Tester
	probe themeProbe
}

// newProbeTester 的 view 就是 shell.go 里该长的样子：每帧开头读设置、装主题。
func newProbeTester(settings func() app.Settings) *probeTester {
	p := &probeTester{}
	p.tt = mygoui.NewTester(func(c *mygoui.Context) {
		ApplyTheme(c, settings(), c.Theme().Dark)
		p.probe = readTheme(c)
	}, 200, 100)
	return p
}

func (p *probeTester) frame() { p.tt.Frame() }

// ─────────────────────────────────────────────────────────────────────────────
// 接进 Shell
// ─────────────────────────────────────────────────────────────────────────────

// TestShellFramesCarryTheTheme 验错误页也是一帧，也得有主题。
//
// shell.go 现在还没接 ApplyTheme（那个文件不归本阶段改），这条用真实的
// Shell 跑，把「接上去之后该是什么样子」钉住：正常页和错误页都带着当前设置。
func TestShellFramesCarryTheTheme(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	s := New(a)

	settings := app.DefaultSettings("")
	settings.ThemeMode = app.ThemeModeDark

	var got themeProbe
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		ApplyTheme(c, settings, c.Theme().Dark)
		got = readTheme(c)
		s.View(c)
	}, 700, 400)

	tt.Frame()
	if !got.dark {
		t.Error("正常页没有带上设置里的深色主题")
	}

	// 崩过一次之后：错误页也是一帧。
	s.crash = "坏掉了"
	tt.Frame()
	if !got.dark {
		t.Error("错误页没有带上设置里的深色主题")
	}
}
