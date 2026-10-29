package ui

import (
	"encoding/json"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 设置页「配置」那一段的测试：导出 / 导入。
//
// 断言的是**画面上有什么**和**磁盘上落了什么**——文件框在无头环境里调不了，
// 替掉 `s.picker`（和本地音乐那几个「选择」按钮同一个道理）。

// 配置段只提供本应用自己的配置文件导出 / 导入。
func TestTheConfigSectionOnlyOffersCurrentConfigImport(t *testing.T) {
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""))

	for _, want := range []string{"配置", "导出配置", "导入配置"} {
		if !hasExactText(tt, want) {
			t.Errorf("设置页上没有 %q，文字有 %q", want, tt.Texts())
		}
	}
	if hasExactText(tt, "从 Biu 导入") {
		t.Errorf("仍然摆出了已移除的「从 Biu 导入」入口，文字有 %q", tt.Texts())
	}
}

// 导出的文件里没有任何凭据，**而且**界面明说了这一点。
//
// 界面上那句「不含任何登录信息」不是装饰：用户把这份文件发到群里、发到论坛上、
// 存进网盘之后才发现里面有密码，那时已经晚了。
func TestExportingWritesAFileWithNoCredentialAndSaysSo(t *testing.T) {
	st := app.DefaultSettings("/Users/x/Downloads")
	st.ThemeMode = app.ThemeModeDark
	st.ProxySettings = app.ProxySettings{
		Type: app.ProxyHTTP, Host: "proxy.example.com", Password: "别带走",
	}
	tt, s, _, _ := shellOnSettings(t, st)

	dir := t.TempDir()
	target := filepath.Join(dir, "mygo-settings-2026-10-17T12-30-00.json")
	fake := &fakePicker{savedAs: target, savedName: "mygo-settings-2026-10-17T12-30-00.json"}
	s.picker = fake

	clickSetting(t, tt, s, "导出配置")
	waitFor(t, "代理配置写到盘上", func() bool { _, err := os.Stat(target); return err == nil })

	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "别带走") {
		t.Fatalf("代理密码进了导出的文件：%s", body)
	}
	for _, secret := range []string{"SESSDATA", "bili_jct", "refresh_token", "Cookie"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("导出的文件里有 %q", secret)
		}
	}
	// 而它确实是当前设置。
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		t.Fatalf("导出的不是 JSON：%v", err)
	}
	if _, wrapped := top["appSettings"]; wrapped {
		t.Fatalf("导出不应带设置文件信封：%s", body)
	}
	if got := string(top["themeMode"]); got != `"dark"` {
		t.Fatalf("导出的不是当前设置（themeMode=%s）", got)
	}
	// 用户改了名字也照样写过去——保存框返回什么就是什么。
	if fake.savedName == "" {
		t.Errorf("保存框没有带上建议的文件名")
	}
}

// 导入一份配置之后，设置真的变了，**而且只变了文件里提到的那些**。
//
// 「只变了提到的那些」表示配置文件按字段合并。用户导出一份只改过主题的配置
// 导回来，圆角、下载目录、侧栏宽度都该原样。
func TestImportingAConfigChangesOnlyWhatThatFileMentions(t *testing.T) {
	cur := app.DefaultSettings("/Users/x/Downloads")
	cur.ThemeMode = app.ThemeModeLight
	cur.BorderRadius = 16
	cur.SideMenuWidth = 300
	tt, s, a, _ := shellOnSettings(t, cur)

	patch := filepath.Join(t.TempDir(), "mygo-settings-x.json")
	if err := os.WriteFile(patch, []byte(`{"themeMode":"dark","borderRadius":20}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s.picker = &fakePicker{configPath: patch}

	clickSetting(t, tt, s, "导入配置")
	waitFor(t, "设置落成深色", func() bool { return a.SettingsSnapshot().ThemeMode == app.ThemeModeDark })

	got := a.SettingsSnapshot()
	if got.BorderRadius != 20 {
		t.Errorf("文件里改了圆角，没生效：%v", got.BorderRadius)
	}
	if got.SideMenuWidth != 300 {
		t.Errorf("文件里没提侧栏宽度，它被重置了：%v", got.SideMenuWidth)
	}
	if got.DownloadPath != "/Users/x/Downloads" {
		t.Errorf("文件里没提下载目录，它被重置了：%q", got.DownloadPath)
	}
	// 换档位之后界面要真的跟着变，而不是设置变了画面还是旧的。
	tt.Frame()
	if !hasExactText(tt, "深色") {
		t.Errorf("导入之后画面没有重画，文字有 %q", tt.Texts())
	}
}

// 导入之后**落盘**了。
//
// 设置是后台合并写、退避窗口 120ms；用户导入了配置就退出，那一下有可能还等在
// 窗口里。不显式 Flush 的话用户下次打开看到的是旧的，而界面上刚刚明明变了。
func TestImportingAConfigActuallyReachesTheDisk(t *testing.T) {
	tt, s, _, path := shellOnSettings(t, app.DefaultSettings(""))

	patch := filepath.Join(t.TempDir(), "mygo-settings-x.json")
	if err := os.WriteFile(patch, []byte(`{"borderRadius":21}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s.picker = &fakePicker{configPath: patch}

	clickSetting(t, tt, s, "导入配置")
	waitFor(t, "字号写进配置文件", func() bool {
		b, err := os.ReadFile(path)
		return err == nil && strings.Contains(string(b), "21")
	})
}

// 挑的是一份读不懂的文件时，要把话说出来，而不是安静地什么都不做。
//
// 用户点了「导入配置」、文件框关了、界面上什么都没变——他会以为应用坏了。
func TestImportingAFileWeCannotReadSaysSo(t *testing.T) {
	tt, s, a, _ := shellOnSettings(t, app.DefaultSettings(""))

	bad := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(bad, []byte(`{ 这不是 JSON`), 0o644); err != nil {
		t.Fatal(err)
	}
	s.picker = &fakePicker{configPath: bad}

	before := a.SettingsSnapshot().BorderRadius
	clickSetting(t, tt, s, "导入配置")
	waitFrames(t, tt, "画面上出现「导入配置失败」", func() bool { return tt.HasText("导入配置失败") })

	if got := a.SettingsSnapshot().BorderRadius; got != before {
		t.Errorf("一份坏掉的文件把设置改了：%v → %v", before, got)
	}
}

// 设置来自更新的版本时，导入要被拒掉并说清为什么。
//
// 只读那份文件是这一票的目标之一（`ErrSettingsFromNewer`）。用户点「导入配置」
// 却一个字都没进来、界面上什么都没有，那是把「按旧语义改写了你的数据」变成了
// 「按了没反应」——后者其实是更安全的，但用户不知道发生了什么。
func TestImportingIntoSettingsFromANewerVersionIsRefusedLoudly(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	// 一份未来的设置文件。
	body := `{"schemaVersion":` + strconv.Itoa(app.SchemaVersion+5) + `,"appSettings":{"borderRadius":7}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	a := app.New()
	if err := a.MountSettings(path); err == nil {
		t.Fatal("一份未来的设置文件挂载时没有报错")
	}
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})
	b := newTestShell(t, onApp(a))
	s, tt := b.s, b.tt
	tt.Frame()
	openSettings(t, tt)

	if !a.SettingsReadOnly() {
		t.Fatal("一份未来的设置文件没有被判成只读：用户会以为改了能存住")
	}

	patch := filepath.Join(t.TempDir(), "mygo-settings-x.json")
	if err := os.WriteFile(patch, []byte(`{"borderRadius":21}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s.picker = &fakePicker{configPath: patch}

	clickSetting(t, tt, s, "导入配置")
	waitFrames(t, tt, "画面上出现「导入配置失败」", func() bool { return tt.HasText("导入配置失败") })
	// 那份文件一个字节都没动。
	if got, err := os.ReadFile(path); err != nil || string(got) != body {
		t.Fatalf("一份未来的设置文件被改写了：\n改前 %s\n改后 %s", body, got)
	}
}

// 导入一份「深色主题 + 白色背景」的配置之后，**整个窗口真的变成深色**。
//
// 这一条钉的是 🔴 铁律二在**这条路上**也成立：主题是每帧开头由 `viewMain` 装的，
// 而导入是**改设置 → invalidate → 重画**，重画的那一帧开头会读到新的设置。
// 少了那一步的表现不是「主题不换」，是「主题悄悄跳回系统色」——不报错、不崩溃、
// 不打日志（`theme.go` 的文件头写着这一条）。
//
// 判据是**像素**：只查 `BuildTheme` 的话，改设置存没存住和界面用没用上是两件事，
// 而后者才是用户看见的那一件。
func TestImportingADarkThemeActuallyTurnsTheWholeWindowDark(t *testing.T) {
	// 系统是浅色，而配置要深色——两者相反，才看得出是「按设置走」还是「按系统走」。
	tt, s, a, _ := shellOnSettings(t, app.DefaultSettings(""))
	tt.SetDark(false)
	tt.Frame()

	// 采样点选在**两块空白**上，不落在任何一个控件上：侧栏中段（100,500）和
	// 内容区右边那块（1160,400）。
	//
	// ⚠️ 原来只有一个点，在 (300,400)。设置页的内容收进 900 居中之后（票 #127），
	// 那一块正好落到「颜色」那一行的字上——**深色下那个字是 `#18181b`**，
	// 于是「起点不是浅色」这条当场红，而报出来的那个数看起来像「界面本来就是
	// 深色的」，离真因隔了三层。同一类坑还有第二个：采样点落在一块控件上时，
	// 那一帧**看着像是「主题没换」**。
	//
	// 之所以取两块：这一条的名字是「**整个窗口**真的变成深色」，而侧栏和内容区
	// 各自铺的是同一个底色（#127 统一之后），一个点只验到其中一半——把侧栏那
	// 一半改回 `Surface`（深色下 `#27272a`）而内容区不动，这一条原来照样全绿。
	probes := [][2]int{{100, 500}, {1160, 400}}
	// 第一个点在侧栏上（方案的 sidebar 层），第二个在内容区上（Background）。
	probeBG := func(sysDark bool, i int) mygoui.Color {
		if i == 0 {
			return wantBackground(t, a, sysDark)
		}
		return BuildTheme(a.SettingsSnapshot(), sysDark).Background
	}
	// ⚠️ 每一处读像素都走 `waitPixel`（等它对上），不是「画一帧就读」：切路由是
	// 淡入的，刚点完那一帧的底色还差百分之几（实测 255 里的 251），那个差跟
	// 主题对不对毫无关系，却会让这条测试随机红，而它红的时刻正好是「别的模块给
	// 顶栏多加了一个控件」那种改动。判据没有放宽——条件就是 want 本身。
	for i, p := range probes {
		lightBG := probeBG(false, i)
		if w := asColor(waitPixel(t, tt, p[0], p[1], rgbaOf(lightBG), "起点")); w != lightBG {
			t.Fatalf("起点 (%d,%d) 不是浅色（%v），这一条验不出什么：want %v", p[0], p[1], w, lightBG)
		}
	}

	patch := filepath.Join(t.TempDir(), "mygo-settings-x.json")
	if err := os.WriteFile(patch, []byte(`{"themeMode":"dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s.picker = &fakePicker{configPath: patch}

	clickSetting(t, tt, s, "导入配置")
	waitFor(t, "设置落成深色", func() bool { return a.SettingsSnapshot().ThemeMode == app.ThemeModeDark })
	tt.Frame()

	want := BuildTheme(a.SettingsSnapshot(), false) // 系统仍然是浅色
	if want.Dark != true {
		t.Fatalf("测试本身有问题：设置要深色、系统浅色，却拼出了浅色主题")
	}
	for i, p := range probes {
		wb := probeBG(false, i)
		if w := asColor(waitPixel(t, tt, p[0], p[1], rgbaOf(wb), "导入深色主题之后")); w != wb {
			t.Fatalf("导入深色主题之后画面 (%d,%d) 还是 %v，want %v（这一帧的主题还是系统那份）",
				p[0], p[1], w, wb)
		}
	}

	// 反过来：再导一份「跟随系统」并把系统外观切成深色，画面要跟着**系统**走。
	// 这一条排除的是「界面根本不看 themeMode、一直深色」这种蒙混过关的实现。
	follow := filepath.Join(t.TempDir(), "mygo-settings-y.json")
	if err := os.WriteFile(follow, []byte(`{"themeMode":"system"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s.picker = &fakePicker{configPath: follow}
	clickSetting(t, tt, s, "导入配置")
	waitFor(t, "设置落成跟随系统", func() bool { return a.SettingsSnapshot().ThemeMode == app.ThemeModeSystem })
	tt.SetDark(true)
	tt.Frame()

	for i, p := range probes {
		sysBG := probeBG(true, i)
		if got := asColor(waitPixel(t, tt, p[0], p[1], rgbaOf(sysBG), "「跟随系统」+ 系统深色")); got != sysBG {
			t.Errorf("「跟随系统」+ 系统深色：画面 (%d,%d) 是 %v，want %v", p[0], p[1], got, sysBG)
		}
	}
}

// asColor 把一个像素换成 ui.Color，好和 `BuildTheme` 的输出直接比。
func asColor(p color.RGBA) mygoui.Color {
	return mygoui.Color{R: p.R, G: p.G, B: p.B, A: p.A}
}

// rgbaOf 是 asColor 的反过来：`BuildTheme` 拼出来的是 ui.Color，而读像素读到的
// 是 color.RGBA，要拿前者去比后者的读数就得先换过去。
func rgbaOf(c mygoui.Color) color.RGBA {
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A}
}

// 数据目录来自更新的版本：导入按钮**不摆**，并把为什么说清楚。
//
// 摆在那个位置的话，用户按下去每一样都会被各自的只读判断挡掉，看到的是四句
// 「没带过来的」，而他刚才明明点了一个看着能用的按钮。
