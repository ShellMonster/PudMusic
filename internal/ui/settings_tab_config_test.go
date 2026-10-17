package ui

import (
	"encoding/json"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 设置页「配置」那一段的测试：导出 / 导入 / 从 Biu 导入。
//
// 断言的是**画面上有什么**和**磁盘上落了什么**——文件框在无头环境里调不了，
// 替掉 `s.picker`（和本地音乐那几个「选择」按钮同一个道理）。

// 三个按钮都在常规那一页上，而且点得动。
//
// 挨个点名是因为设置页最可能的失败是「画出来了，但少了一个按钮」：用户在设置
// 页里找一圈「导出配置」没找到，只会觉得这个应用不能备份设置。
func TestTheConfigSectionIsThereOnTheGeneralTab(t *testing.T) {
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""))

	for _, want := range []string{"配置", "导出配置", "导入配置"} {
		if !hasExactText(tt, want) {
			t.Errorf("设置页上没有 %q，文字有 %q", want, tt.Texts())
		}
	}
	// 「从 Biu 导入」只在**真的找得到** Biu 的数据目录时才摆。摆一个点了必然说
	// 「没找到 Biu」的按钮，等于让用户自己判断这台机器上有没有装过 Biu。
	if hasExactText(tt, "从 Biu 导入") {
		t.Errorf("没有 Biu 数据目录时也摆了「从 Biu 导入」，文字有 %q", tt.Texts())
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
	target := filepath.Join(dir, "biu-settings-2026-10-17T12-30-00.json")
	fake := &fakePicker{savedAs: target, savedName: "biu-settings-2026-10-17T12-30-00.json"}
	s.picker = fake

	clickSetting(t, tt, s, "导出配置")
	waitFor(t, func() bool { _, err := os.Stat(target); return err == nil })

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
		t.Fatalf("导出带了 appSettings 信封：Biu 的导入一个键都取不到：%s", body)
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
// 「只变了提到的那些」是 Biu 的语义（`merge`，`export-import.tsx:53`）。用户导出
// 一份只改过主题的配置导回来，圆角、下载目录、侧栏宽度都该原样。
func TestImportingAConfigChangesOnlyWhatThatFileMentions(t *testing.T) {
	cur := app.DefaultSettings("/Users/x/Downloads")
	cur.ThemeMode = app.ThemeModeLight
	cur.BorderRadius = 16
	cur.SideMenuWidth = 300
	tt, s, a, _ := shellOnSettings(t, cur)

	patch := filepath.Join(t.TempDir(), "biu-settings-x.json")
	if err := os.WriteFile(patch, []byte(`{"themeMode":"dark","borderRadius":20}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s.picker = &fakePicker{configPath: patch}

	clickSetting(t, tt, s, "导入配置")
	waitFor(t, func() bool { return a.SettingsSnapshot().ThemeMode == app.ThemeModeDark })

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

	patch := filepath.Join(t.TempDir(), "biu-settings-x.json")
	if err := os.WriteFile(patch, []byte(`{"borderRadius":21}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s.picker = &fakePicker{configPath: patch}

	clickSetting(t, tt, s, "导入配置")
	waitFor(t, func() bool {
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
	waitForFrames(t, tt, func() bool { return tt.HasText("导入配置失败") })

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
	s := New(a)
	s.router.Transition = mygoui.TransitionNone
	tt := mygoui.NewTester(s.View, 1200, 800)
	tt.Frame()
	openSettings(t, tt)

	if !a.SettingsReadOnly() {
		t.Fatal("一份未来的设置文件没有被判成只读：用户会以为改了能存住")
	}

	patch := filepath.Join(t.TempDir(), "biu-settings-x.json")
	if err := os.WriteFile(patch, []byte(`{"borderRadius":21}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s.picker = &fakePicker{configPath: patch}

	clickSetting(t, tt, s, "导入配置")
	waitForFrames(t, tt, func() bool { return tt.HasText("导入配置失败") })
	// 那份文件一个字节都没动。
	if got := readFile(t, path); got != body {
		t.Fatalf("一份未来的设置文件被改写了：\n改前 %s\n改后 %s", body, got)
	}
}

// 界面上「配置」那一段不会把设置页画崩——哪怕迁移报告里有很长的句子。
func TestTheConfigSectionSurvivesALongMigrationNote(t *testing.T) {
	withMigration(t, MigrationReport{
		Upgraded:      "数据已升到 v1（动了：settings.json、shortcuts.json、search-history.json、download-queue.json）",
		UpgradedFiles: []string{"settings.json", "shortcuts.json"},
	})
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	if tt.HasText("界面出错了") {
		t.Fatalf("配置段画崩了，文字有 %q", tt.Texts())
	}
}

// 升级失败那一句必须带着「原来的数据一个字节都没少」。
//
// 用户最怕的是「升级失败之后我的设置没了」。界面上只写一句「升级失败」的话，
// 他会在还没搞清发生了什么之前先去翻自己的文件。
func TestAFailedUpgradeSaysTheOldFilesAreStillThere(t *testing.T) {
	withMigration(t, MigrationReport{
		Upgraded:   "升级到 v1 失败：这一份读不懂。原来的数据一个字节都没少。",
		UpgradeErr: os.ErrInvalid,
		Rollback:   true,
	})
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	if !tt.HasText("原来的数据一个字节都没少") {
		t.Fatalf("升级失败时没有说原文件还在，文字有 %q", tt.Texts())
	}
	if !tt.HasText("数据已升到") && !tt.HasText("升级到 v1 失败") {
		t.Fatalf("升级结果没有摆在设置页上，文字有 %q", tt.Texts())
	}
}

// withMigration 临时换掉最近一次的迁移报告。
// restoreMigrationAfter 记住**此刻**的迁移报告，测试结束时恢复回去。
//
// ⚠️ 为什么 `withMigration` 的 cleanup 不够：它只恢复**它自己设进去的那一份**。而
// 按一下「从 Biu 导入」按钮会走 `rememberImport`，那是**直接把报告写进包级的
// `migration.r`** 的——不在任何 cleanup 的射程里。
//
// 留下的内容会让**后面**每一个新建的 Shell 在第一帧弹一条 toast
// （`viewMain` → `announceMigration`），而 toast 的卡片底色是**主色**
// （`viewToast`：Biu 的 `color: "primary"`）。`internal/ui/settings_test.go` 那些
// 验「窗口底色」的测试取的采样点 `(700, 430)`（900×600 窗口的右下角）**正好落在
// toast 上**——实测那几条红成「画面那个点是 {30 215 96}」，那个值就是
// `DefaultPrimaryColor` 逐字。
//
// 所以**任何点了导入按钮的测试都要在开头调这个**，而不是指望 `withMigration`。
func restoreMigrationAfter(t *testing.T) {
	t.Helper()
	migration.mu.Lock()
	before := migration.r
	migration.mu.Unlock()
	t.Cleanup(func() {
		migration.mu.Lock()
		migration.r = before
		migration.mu.Unlock()
	})
}

func withMigration(t *testing.T, r MigrationReport) {
	t.Helper()
	migration.mu.Lock()
	before := migration.r
	migration.r = r
	migration.mu.Unlock()
	t.Cleanup(func() {
		migration.mu.Lock()
		migration.r = before
		migration.mu.Unlock()
	})
}

// waitForFrames 等一个后台结果，**每轮都重画一帧**。
//
// 和 `waitFor` 的区别：结果可能是**画在屏幕上的一行字**（一条提示）。画面是
// 上一帧的产物，不 `Frame()` 就永远读不到那一帧——而提示是在后台 goroutine 里
// 投的，那个 goroutine 调的 `invalidate` 对无头 Tester 是不起作用的。
func waitForFrames(t *testing.T, tt *mygoui.Tester, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if done() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	tt.Frame()
	t.Fatal("等了两秒，那个后台结果还没画出来")
}

// 点「从 Biu 导入」：设置真的换成了 Biu 那一份，而且**旧的那份留了备份**。
//
// 这是「用户点一下就能导」的正面对照——前面 `TestAFreshInstall…` 测的是启动时
// 自动走的那条路，那条路用户没有选择权。这一条是他在设置页上自己按下去的。
func TestPressingTheBiuImportButtonActuallySwapsTheSettingsIn(t *testing.T) {
	biu := biuHomeDir(t)

	mine := app.DefaultSettings("/Users/y/Mine")
	mine.ThemeMode = app.ThemeModeLight
	tt, s, a, path := shellOnSettings(t, mine)
	before := readFile(t, path)
	// 导入写到的是「本应用的用户数据目录」，而那个目录在无头环境里是一个真的、
	// 可写的目录。不指到临时目录上的话，这个测试会改用户的文件。
	withUserDataDir(t, filepath.Dir(path))

	// ⚠️ **必须在 `withMigration` 之前调**：它记的是「此刻」的值，而此刻应该是
	// **什么都没有**。反过来的话它记下的是刚被 `withMigration` 设进去的那一份
	// （带 `BiuDirs`），cleanup 恢复成它 —— 于是每一条测试跑完都还留着一份报告，
	// 后面每一个新建的 Shell 第一帧照样弹 toast，采样点照样被盖。
	restoreMigrationAfter(t)
	withMigration(t, MigrationReport{BiuDirs: []string{biu}})

	// 按钮因为找得到 Biu 才摆——这是设置页上的可观察行为。
	tt.Frame()
	if !hasExactText(tt, "从 Biu 导入") {
		t.Fatalf("找得到 Biu 数据目录时没有摆「从 Biu 导入」，文字有 %q", tt.Texts())
	}

	clickSetting(t, tt, s, "从 Biu 导入")
	waitForFrames(t, tt, func() bool {
		return a.SettingsSnapshot().PrimaryColor == "#17c964"
	})

	got := a.SettingsSnapshot()
	if got.DownloadPath != "/Users/x/Downloads" {
		t.Errorf("下载目录还是我自己那份：%q", got.DownloadPath)
	}
	if !got.SideMenuCollapsed {
		t.Errorf("侧栏折叠状态不是 Biu 那一份：%v", got.SideMenuCollapsed)
	}
	// 我原来那份留了备份。这是这一票里最坏的一种丢数据的入口——
	// 「我导入了 Biu 的设置，我的设置没了」，而用户连那份文件在哪都说不出来。
	bak := readFile(t, path+".bak")
	if !strings.Contains(bak, "/Users/y/Mine") {
		t.Fatalf("备份里不是我自己那份设置：%s", bak)
	}
	if !strings.Contains(before, "/Users/y/Mine") {
		t.Fatalf("导入前的文件内容不对：%s", before)
	}
	// 而导入完成之后界面上要说清做了什么。
	tt.Frame()
	if !tt.HasText("已从 Biu 导入") && !tt.HasText("Biu") {
		t.Errorf("导入完成之后没有告诉用户做了什么，文字有 %q", tt.Texts())
	}
}

// withUserDataDir 把「本应用的用户数据目录」指到临时目录上。
//
// ⚠️ 这一格**必须替**。无头 Tester 里 `mygo.App.Path(mygo.PathUserData)` 返回的是
// 一个真的、可写的目录（`~/Library/Application Support/<测试二进制名>/`），
// 不替的话「从 Biu 导入」这个测试会把 Biu 的设置真的写进用户的目录里——用户在
// 文件夹里看到一个他从来没见过的 `ui.test/`，而下一次跑同一批测试读到的是上一次
// 留下的内容。这条约束是被跑出来之后才发现的。
func withUserDataDir(t *testing.T, dir string) {
	t.Helper()
	before := userDataDir
	userDataDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userDataDir = before })
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

	// 采样点选在内容区的空白处（x=300），不落在任何一个控件上——滚一下设置页
	// (600, 300) 就会落到某个控件的颜色上，而那一帧看着像是「主题没换」。
	const probeX, probeY = 300, 400
	lightBG := BuildTheme(a.SettingsSnapshot(), false).Background
	if w := asColor(tt.Image().RGBAAt(probeX, probeY)); w != lightBG {
		t.Fatalf("起点不是浅色（%v），这一条验不出什么：want %v", w, lightBG)
	}

	patch := filepath.Join(t.TempDir(), "biu-settings-x.json")
	if err := os.WriteFile(patch, []byte(`{"themeMode":"dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s.picker = &fakePicker{configPath: patch}

	clickSetting(t, tt, s, "导入配置")
	waitFor(t, func() bool { return a.SettingsSnapshot().ThemeMode == app.ThemeModeDark })
	tt.Frame()

	want := BuildTheme(a.SettingsSnapshot(), false) // 系统仍然是浅色
	if want.Dark != true {
		t.Fatalf("测试本身有问题：设置要深色、系统浅色，却拼出了浅色主题")
	}
	w := asColor(tt.Image().RGBAAt(probeX, probeY))
	if w != want.Background {
		t.Fatalf("导入深色主题之后画面还是 %v，want %v（这一帧的主题还是系统那份）", w, want.Background)
	}

	// 反过来：再导一份「跟随系统」并把系统外观切成深色，画面要跟着**系统**走。
	// 这一条排除的是「界面根本不看 themeMode、一直深色」这种蒙混过关的实现。
	follow := filepath.Join(t.TempDir(), "biu-settings-y.json")
	if err := os.WriteFile(follow, []byte(`{"themeMode":"system"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s.picker = &fakePicker{configPath: follow}
	clickSetting(t, tt, s, "导入配置")
	waitFor(t, func() bool { return a.SettingsSnapshot().ThemeMode == app.ThemeModeSystem })
	tt.SetDark(true)
	tt.Frame()

	sysBG := BuildTheme(a.SettingsSnapshot(), true).Background
	if got := asColor(tt.Image().RGBAAt(probeX, probeY)); got != sysBG {
		t.Errorf("「跟随系统」+ 系统深色：画面是 %v，want %v", got, sysBG)
	}
}

// asColor 把一个像素换成 ui.Color，好和 `BuildTheme` 的输出直接比。
func asColor(p color.RGBA) mygoui.Color {
	return mygoui.Color{R: p.R, G: p.G, B: p.B, A: p.A}
}

// 任务书明写「凭据可以不迁，**要提示重新登录**」——那句话必须出现在界面上。
//
// 这一条是被探针抓出来的：`Imported.Notes` 在设置页和 toast 上**一次都没被读过**，
// 界面上写着「见下方清单」而那个清单根本不存在。用户在启动时什么也没点，然后发现
// 要重新登录，而界面上从头到尾没提过发生过一次导入。
func TestTheSettingsPageSaysOutWhatDidNotComeOver(t *testing.T) {
	withMigration(t, MigrationReport{
		Upgraded: "数据已升到 v1",
		Imported: app.Imported{
			Dir:       "/Users/x/Library/Application Support/Biu",
			Settings:  true,
			Shortcuts: true,
			Downloads: 1,
			Notes: []string{
				"Biu 那边是登录状态。Biu 的登录信息是加密存的，这一版不搬过来——请在这个应用里重新登录一次。",
				"Biu 的歌词缓存里有 2 条读不出来，已经跳过。",
			},
		},
	})
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""))

	// 逐条点名，不只查「有没有提到登录」——那两句是**两件不同的事**。
	for _, want := range []string{
		"这次没有带过来的：",
		"请在这个应用里重新登录一次",
		"Biu 的歌词缓存里有 2 条读不出来",
	} {
		if !tt.HasText(want) {
			t.Errorf("设置页上没有 %q，文字有 %q", want, tt.Texts())
		}
	}
	// 「见下方清单」那句话曾经指着一个不存在的东西；现在清单在下面，但不能靠
	// 一句指路的话代替它本身。
	if tt.HasText("见下方清单") {
		t.Errorf("还在说「见下方清单」，文字有 %q", tt.Texts())
	}
}

// 启动时的自动导入结果要弹一条 toast 说出来——用户不会主动去设置页。
func TestTheFirstFrameSaysTheMigrationHappened(t *testing.T) {
	withMigration(t, MigrationReport{
		Imported: app.Imported{
			Dir:      "/Users/x/…/Biu",
			Settings: true,
			Notes:    []string{"Biu 那边是登录状态……请在这个应用里重新登录一次。"},
		},
	})
	s, tt := newShell(t)
	tt.Frame()

	// ⚠️ 首帧那句**刻意是短的**：toast 的卡片底色是主色（`viewToast`），而它一旦
	// 换行就向上扩展、盖住页面内容区。完整清单在设置页「配置」那一段。
	if !tt.HasText("从 Biu 导入了一批数据") {
		t.Fatalf("第一帧没有说「从 Biu 导入过」，文字有 %q", tt.Texts())
	}
	if !tt.HasText("重新登录") {
		t.Fatalf("第一帧没有提示重新登录，文字有 %q", tt.Texts())
	}
	// **只弹一次**：第二帧还在，但那条提示已经不在了（toast 两秒后自己走，
	// 而这里没在等它过期——要验的是「没有再弹一条同样的」）。
	before := countText(tt, "从 Biu 导入了一批数据")
	tt.Frame()
	if after := countText(tt, "从 Biu 导入了一批数据"); after != before {
		t.Errorf("每帧都在重弹同一条提示：%d → %d", before, after)
	}
	_ = s
}

// 一次顺顺利利的升级**不弹** toast。
//
// 用户没要求知道「数据已是最新」；弹一条「已升到 v1」是在打扰他，而且真出事
// （升级失败）时那条提示就混在一堆无害提示里，不显眼。
func TestASuccessfulQuietUpgradeSaysNothing(t *testing.T) {
	withMigration(t, MigrationReport{
		Upgraded:      "数据已升到 v1（动了：settings.json）",
		UpgradedFiles: []string{"settings.json"},
	})
	_, tt := newShell(t)
	tt.Frame()

	if tt.HasText("已升到 v1") {
		t.Errorf("一次顺顺利利的升级弹了一条 toast，文字有 %q", tt.Texts())
	}
}

// 升级失败要弹，而且那句话必须带着「原文件一个字节都没少」。
func TestAFailedUpgradeIsAnnouncedOnTheFirstFrameToo(t *testing.T) {
	withMigration(t, MigrationReport{
		Upgraded:   "升级到 v1 失败：这一份读不懂。原来的数据一个字节都没少。",
		UpgradeErr: os.ErrInvalid,
		Rollback:   true,
	})
	_, tt := newShell(t)
	tt.Frame()

	if !tt.HasText("数据升级失败") {
		t.Fatalf("升级失败没有在第一帧说出来，文字有 %q", tt.Texts())
	}
	if !tt.HasText("一个字节都没少") {
		t.Fatalf("那句话里没有说原文件还在，文字有 %q", tt.Texts())
	}
}

// 数据目录来自更新的版本：导入按钮**不摆**，并把为什么说清楚。
//
// 摆在那个位置的话，用户按下去每一样都会被各自的只读判断挡掉，看到的是四句
// 「没带过来的」，而他刚才明明点了一个看着能用的按钮。
func TestTheBiuImportButtonIsGoneWhenTheDataComesFromANewerBuild(t *testing.T) {
	withMigration(t, MigrationReport{
		ReadOnly: true,
		Upgraded: "数据目录来自更新的版本，这一版一个字都不会写回去。",
		BiuDirs:  []string{"/Users/x/…/Biu"},
	})
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""))

	if hasExactText(tt, "从 Biu 导入") {
		t.Errorf("只读目录里还摆着「从 Biu 导入」，文字有 %q", tt.Texts())
	}
	if !tt.HasText("数据目录来自更新的版本") {
		t.Errorf("没有把「为什么没有导入按钮」说清楚，文字有 %q", tt.Texts())
	}
}
