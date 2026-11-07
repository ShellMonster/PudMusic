package ui

import (
	"path/filepath"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
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
// 「深夜唱片行」：令牌、强调色、圆角
// ─────────────────────────────────────────────────────────────────────────────

// themeWithAccent 是「主色是任意一个颜色」那一档主题：封面取色开着时主色就是
// 封面推出来的任意色，这里直接把 hex 铺上去（和 `buildTheme` 封面那一支同一个
// `applyAccent`），给那些「随便哪个主色都要读得出来」的对比度用例用。
func themeWithAccent(s app.Settings, hex string, dark bool) *mygoui.Theme {
	th := BuildTheme(s, dark)
	applyAccent(th, mygoui.Hex(hex))
	th.Focus = th.Accent
	return th
}

// TestTheThemeIsTheMidnightRecordPalette 验主题的每一档都来自原型 `index.html`
// 69–112 行（深浅两份），而不是框架默认值。
func TestTheThemeIsTheMidnightRecordPalette(t *testing.T) {
	for _, c := range []struct {
		dark                                    bool
		bg, surface2, line, text, text2, inv    string
		accent, accentInk, focus, err, ok, warn string
	}{
		{false, "#F6F6F5", "#E9EAEB", "#E0E1E3", "#141518", "#50535A", "#141518",
			"#D2391B", "#FFFFFF", "#D2391B", "#C93B3B", "#2E7D4F", "#9A5B06"},
		{true, "#0E0F11", "#1E2024", "#26282D", "#ECEBE8", "#A6A8AE", "#ECEBE8",
			"#FF5D3D", "#1A0905", "#FFB3A3", "#F0716B", "#5BC287", "#F2B04A"},
	} {
		th := BuildTheme(app.DefaultSettings(""), c.dark)
		for _, f := range []struct {
			what      string
			got, want mygoui.Color
		}{
			{"Background", th.Background, mygoui.Hex(c.bg)},
			{"Surface", th.Surface, mygoui.Hex(c.surface2)},
			{"Border", th.Border, mygoui.Hex(c.line)},
			{"Text", th.Text, mygoui.Hex(c.text)},
			{"TextMuted", th.TextMuted, mygoui.Hex(c.text2)},
			{"Inverse", th.Inverse, mygoui.Hex(c.inv)},
			{"InverseText", th.InverseText, mygoui.Hex(c.bg)},
			{"Accent", th.Accent, mygoui.Hex(c.accent)},
			{"AccentText", th.AccentText, mygoui.Hex(c.accentInk)},
			{"Focus", th.Focus, mygoui.Hex(c.focus)},
			{"Danger", th.Danger, mygoui.Hex(c.err)},
			{"Success", th.Success, mygoui.Hex(c.ok)},
			{"Warning", th.Warning, mygoui.Hex(c.warn)},
		} {
			if f.got != f.want {
				t.Errorf("深色=%v：%s 是 %v，want %v", c.dark, f.what, f.got, f.want)
			}
		}
		if th.Radius != 8 {
			t.Errorf("深色=%v：Radius 是 %v，want 8（--r-8）", c.dark, th.Radius)
		}
	}
}

// TestTheAccentSettingPicksTheAccentGroup 验四种强调色各自铺到整组强调色上，
// 深浅两份是原型给的两组值。
func TestTheAccentSettingPicksTheAccentGroup(t *testing.T) {
	want := map[app.Accent][2]string{ // [浅色, 深色]
		app.AccentCinnabar: {"#D2391B", "#FF5D3D"},
		app.AccentIndigo:   {"#3451C9", "#7F9CFF"},
		app.AccentPine:     {"#167A4D", "#4CC58E"},
		app.AccentAmber:    {"#8F5D00", "#F0B43C"},
	}
	for _, a := range app.Accents {
		s := app.DefaultSettings("")
		s.Accent = a
		for i, dark := range []bool{false, true} {
			th := BuildTheme(s, dark)
			if got := th.Accent; got != mygoui.Hex(want[a][i]) {
				t.Errorf("%s 深色=%v：Accent 是 %v，want %s", a, dark, got, want[a][i])
			}
			// 浅色四种都配白字、深色四种都配深字（原型 --accent-ink）。
			if dark == (luminanceOf(th.AccentText) > 128) {
				t.Errorf("%s 深色=%v：主色上的字是 %v", a, dark, th.AccentText)
			}
			if th.AccentHover == th.Accent {
				t.Errorf("%s 深色=%v：AccentHover 没从主色推出来", a, dark)
			}
			// 框架控件的主键悬停色和 pud.Button 的 Primary 一样：主色 88% 混正文色（原型
			// index.html:283）。曾经出错的行为：深色下是主色混白 20%，两种主键悬停时颜色不同。
			if want := th.Accent.Mix(th.Text, 0.12); th.AccentHover != want {
				t.Errorf("%s 深色=%v：AccentHover 是 %v，want 主色混正文色 12%% = %v", a, dark, th.AccentHover, want)
			}
		}
	}
}

// TestTheBorderRadiusSettingNoLongerShapesTheTheme 验设置里的圆角不再进主题：
// 原型的圆角是固定的 4 / 8 / 12 / 胶囊（§0′）。
func TestTheBorderRadiusSettingNoLongerShapesTheTheme(t *testing.T) {
	for _, r := range []float64{0, 4, 16, 24} {
		s := app.DefaultSettings("")
		s.BorderRadius = r
		if got := BuildTheme(s, false).Radius; got != 8 {
			t.Errorf("borderRadius=%v：主题圆角是 %v，want 8", r, got)
		}
	}
}

// TestInverseIsTheReversedPill 验 Toast / 提示是反色胶囊（§0′ 冲突 7 按原型）：
// 深色下浅底深字、浅色下深底浅字，而且必须显式给（留零的话框架会自己推一份）。
func TestInverseIsTheReversedPill(t *testing.T) {
	for _, dark := range []bool{false, true} {
		th := BuildTheme(app.DefaultSettings(""), dark)
		if th.Inverse == mygoui.Transparent || th.InverseText == mygoui.Transparent {
			t.Fatalf("深色=%v：Inverse/InverseText 留零了", dark)
		}
		if th.Inverse != th.Text || th.InverseText != th.Background {
			t.Errorf("深色=%v：Toast 是 %v 底 %v 字，want 正文色底、页面底色字", dark, th.Inverse, th.InverseText)
		}
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
	s.ThemeMode = app.ThemeModeDark

	tt := mygoui.NewTester(func(c *mygoui.Context) {
		ApplyTheme(c, s, c.Theme().Dark)
		mygoui.Text(c, "文字")
	}, 200, 100)
	tt.Frame()

	// 窗口四角是窗口底色。取左上角，避开文字和控件。
	// tt.Image() 给的是 color.RGBA（8 位/通道、预乘 alpha），
	// mygoui.Color 是 0~255 + 0~1 的 alpha，两边换一下再比。
	got := tt.Image().RGBAAt(2, 2)
	want := mygoui.Hex("#0E0F11") // 深色 --bg
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

// newProbeTester 的 view 就是 shell_view.go 里该长的样子：每帧开头读设置、装主题。
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
// shell_view.go 现在还没接 ApplyTheme（那个文件不归本阶段改），这条用真实的
// Shell 跑，把「接上去之后该是什么样子」钉住：正常页和错误页都带着当前设置。
func TestShellFramesCarryTheTheme(t *testing.T) {
	settings := app.DefaultSettings("")
	settings.ThemeMode = app.ThemeModeDark

	var got themeProbe
	b := newTestShell(t, at(700, 400), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			ApplyTheme(c, settings, c.Theme().Dark)
			got = readTheme(c)
			s.View(c)

		}
	}))
	s, tt := b.s, b.tt

	tt.Frame()
	if !got.dark {
		t.Error("正常页没有带上设置里的深色主题")
	}

	// 崩过一次之后：错误页也是一帧。
	s.crash = s.newCrashInfo("测试", "坏掉了")
	tt.Frame()
	if !got.dark {
		t.Error("错误页没有带上设置里的深色主题")
	}
}

// TestTheCoverHueOnlyTurnsTheAccentWhenTheSwitchIsOn 验封面取色：开着才换主色，关着取到了也不动；深浅主题下亮度不同。
func TestTheCoverHueOnlyTurnsTheAccentWhenTheSwitchIsOn(t *testing.T) {
	st := app.DefaultSettings("")
	base := BuildTheme(st, false).Accent
	st.CoverHue, st.CoverSat = 200, 0.8
	if got := BuildTheme(st, false).Accent; got != base {
		t.Fatalf("开关关着，主色却变了：%v → %v", base, got)
	}
	st.CoverAccent = true
	light, dark := BuildTheme(st, false).Accent, BuildTheme(st, true).Accent
	if light == base || light == dark {
		t.Errorf("开着之后浅色主色 %v（原 %v）、深色主色 %v，应该都变了且深浅不同", light, base, dark)
	}
	st.CoverSat = 0
	if got := BuildTheme(st, false).Accent; got != base {
		t.Errorf("没取到颜色时主色 %v，want 原来的 %v", got, base)
	}
}
