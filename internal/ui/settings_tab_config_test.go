package ui

import (
	"encoding/json"
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
	body := `{"schemaVersion":` + itoa(app.SchemaVersion+5) + `,"appSettings":{"borderRadius":7}}`
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

// readFile 读一个文件，测试专用。
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s：%v", path, err)
	}
	return string(b)
}

// itoa 是 strconv.Itoa 的短写法，这一组测试用得上两三次。
func itoa(n int) string { return strconv.Itoa(n) }
