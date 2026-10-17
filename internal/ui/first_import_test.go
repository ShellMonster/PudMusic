package ui

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
)

// 「从 Biu 切过来」的端到端：造一份 Biu 的数据目录，在一个**空**的本应用数据
// 目录上跑一次首次导入，然后照**真正的启动路径**把它们挂起来——设置挂上、
// 下载挂上——断言用户看到的就是 Biu 那边的那一份。
//
// 这一组是本票的正面对照。前面那些测的是零件（`ScanBiu`、`MergeIntoQueue`、
// `MigrateFile`），这一组测的是「四份零件凑起来之后用户拿到什么」。

// biuHomeDir 造一份 Biu 的数据目录，四样东西都用上。
func biuHomeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	put := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put(biuSettingsName, `{"appSettings":{
      "downloadPath":"/Users/x/Downloads","primaryColor":"#17c964",
      "fontFamily":"system-default","themeMode":"dark",
      "proxySettings":{"type":"none","host":"","username":"","password":""},
      "sideMenuWidth":280,"sideMenuCollapsed":true,
      "sideMenuCollectionFolded":{"created":true,"collected":false},
      "localMusicDirs":["/Users/x/Music"],"displayMode":"card"
    }}`)
	put(biuShortcutsName, `{
      "enableGlobalShortcuts": false,
      "shortcuts":[
        {"id":"togglePlay","name":"播放/暂停","shortcut":"CommandOrControl+Shift+P"},
        {"id":"prev","name":"上一首","shortcut":"CommandOrControl+Left"},
        {"id":"next","name":"下一首","shortcut":"CommandOrControl+Right"},
        {"id":"volumeUp","name":"音量加","shortcut":"CommandOrControl+Up"},
        {"id":"volumeDown","name":"音量减","shortcut":"CommandOrControl+Down"},
        {"id":"toggleMiniMode","name":"切换mini/完整模式","shortcut":"CommandOrControl+M"}
      ],
      "globalShortcuts":[
        {"id":"togglePlay","name":"播放/暂停","shortcut":"CommandOrControl+Alt+P"}
      ]
    }`)
	put(biuDownloadsName, `{"t1":{
      "id":"t1","status":"completed","outputFileType":"audio","title":"Biu 里下完的那首",
      "cover":"http://cover/x.jpg","bvid":"BV1aa411c7mD","cid":12345,
      "createdTime":1700000000000,"savePath":"/Users/x/Downloads/那首.flac",
      "totalBytes":31457280,"audioCodecs":"flac",
      "chunks":[{"type":"audio","start":0,"end":10485759,"done":true}]
    }}`)
	put(biuLyricsName, `{"BV1aa411c7mD-12345":{
      "fontSize":19,"offset":-250,"lyrics":"[00:00.00] 歌词在这"
    }}`)
	put(biuLoginName, "{t}\x00\x01密文\xff")
	return dir
}

// 这几个名字跟着 `internal/app/biuimport.go` 里的那几个常量（那边是小写、导不出
// 去）。这里抄一份是因为界面层要造 Biu 的目录。
const (
	biuSettingsName  = "app-settings.json"
	biuShortcutsName = "shortcut-settings.json"
	biuDownloadsName = "media-downloads.json"
	biuLyricsName    = "lyrics-cache.json"
	biuLoginName     = "user-login-info.json"
)

// 用户第一次打开这个应用，而机器上有 Biu：四样都带过来，而且**照真正的启动
// 路径挂起来能用**。
func TestAFreshInstallBringsEverythingOverFromBiu(t *testing.T) {
	biu := biuHomeDir(t)
	dst := t.TempDir()

	r := firstBiuImport(dst, []string{biu}, nil)
	if !r.Any() {
		t.Fatalf("自动导入没带过来任何东西：%+v", r)
	}
	if !r.Settings || !r.Shortcuts || r.Downloads != 1 || r.Lyrics != 1 {
		t.Fatalf("四样没有全带过来：%+v", r)
	}

	t.Run("设置挂上来之后就是 Biu 那一份", func(t *testing.T) {
		a := app.New()
		defer func() {
			a.Close()
			_ = a.CloseSettings()
		}()
		if err := a.MountSettings(app.SettingsPath(dst)); err != nil {
			t.Fatalf("挂设置时报错了：%v", err)
		}
		got := a.SettingsSnapshot()
		if got.PrimaryColor != "#17c964" {
			t.Errorf("主色不是 Biu 那一份：%q", got.PrimaryColor)
		}
		if got.ThemeMode != app.ThemeModeDark {
			t.Errorf("主题不是 Biu 那一份：%q", got.ThemeMode)
		}
		if got.DisplayMode != app.DisplayModeCard {
			t.Errorf("显示样式不是 Biu 那一份：%q", got.DisplayMode)
		}
		if got.SideMenuWidth != 280 || !got.SideMenuCollapsed {
			t.Errorf("侧栏不是 Biu 那一份：%v %v", got.SideMenuWidth, got.SideMenuCollapsed)
		}
		if len(got.LocalMusicDirs) != 1 || got.LocalMusicDirs[0] != "/Users/x/Music" {
			t.Errorf("本地音乐目录不是 Biu 那一份：%v", got.LocalMusicDirs)
		}
		// Biu 那个已知的坏字体值在导入时就修掉了，而不是留在设置里等着画错字体。
		if got.FontFamily != app.DefaultFontFamily {
			t.Errorf("Biu 那个 \"system-default\" 没被修掉：%q", got.FontFamily)
		}
	})

	t.Run("下载队列挂上来之后那一条还在", func(t *testing.T) {
		m, err := download.New(download.Options{
			Client:  httpClientForTest(),
			Resolve: func(context.Context, download.Identity) (*download.Source, error) { return nil, nil },
		}, filepath.Join(dst, download.QueueFileName))
		if err != nil {
			t.Fatalf("挂下载时报错了：%v", err)
		}
		defer func() { _ = m.Close(filepath.Join(dst, download.QueueFileName)) }()
		got := m.Snapshots()
		if len(got) != 1 {
			t.Fatalf("队列里有 %d 条，期望 1 条", len(got))
		}
		if got[0].Title != "Biu 里下完的那首" {
			t.Errorf("标题不是 Biu 那一首：%q", got[0].Title)
		}
		// ID 是 `<bvid>|<cid>`——cid 是字符串形式（见 `TaskID` 的注释）。
		if got[0].ID != "BV1aa411c7mD|12345" {
			t.Errorf("身份不是 Biu 那一首：%q", got[0].ID)
		}
		if got[0].Dest != "/Users/x/Downloads/那首.flac" {
			t.Errorf("保存路径不是 Biu 那一首：%q", got[0].Dest)
		}
		if got[0].Status != download.StatusDone {
			t.Errorf("状态不是「已下完」：%q", got[0].Status)
		}
		if got[0].Label != "无损 FLAC" {
			t.Errorf("音质徽章没翻过来：%q", got[0].Label)
		}
	})

	t.Run("歌词落在设置页约定的那条路上", func(t *testing.T) {
		p := filepath.Join(dst, app.LyricsCacheDirName, app.LyricsKey("BV1aa411c7mD", 12345)+".json")
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("歌词没落到 %s：%v", p, err)
		}
		if !strings.Contains(string(body), "歌词在这") {
			t.Errorf("歌词正文不在：%s", body)
		}
		// 用户自己调过的偏移/字号也跟着过来——那是他在歌词页上拖出来的。
		if !strings.Contains(string(body), "-250") || !strings.Contains(string(body), "19") {
			t.Errorf("偏移/字号没带过来：%s", body)
		}
	})

	t.Run("登录态不迁，但要说清要重新登录", func(t *testing.T) {
		if !saysAbout(r.Notes, "重新登录") {
			t.Errorf("没有提示重新登录：%v", r.Notes)
		}
		// 一个字节的凭据都不许落到我们的目录里。
		var leaked []string
		_ = filepath.Walk(dst, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			body, _ := os.ReadFile(p)
			for _, secret := range []string{"密文", "SESSDATA", "bili_jct", "refresh_token"} {
				if strings.Contains(string(body), secret) {
					leaked = append(leaked, filepath.Base(p))
				}
			}
			return nil
		})
		if len(leaked) > 0 {
			t.Errorf("凭据漏进了我们的目录：%v", leaked)
		}
	})

	t.Run("导入的快捷键挂上来就是 Biu 那一套", func(t *testing.T) {
		k := newShortcutKeys(shortcutPath(dst))
		defer k.Close()
		got := k.snapshot()
		if got.EnableGlobalShortcuts {
			t.Errorf("「启用全局快捷键」还是默认的 true：Biu 那边是关的")
		}
		if len(got.Shortcuts) != 6 {
			t.Errorf("应用内那列 %d 条，期望 6 条", len(got.Shortcuts))
		}
		if len(got.GlobalShortcuts) != 6 {
			t.Errorf("全局那列没被补齐：%d 条", len(got.GlobalShortcuts))
		}
	})
}

// 已经用过一阵子的应用：不自动抢它的数据。
//
// 判据是「本应用一份数据文件都没有」。用户已经调好的主题、他下过的歌，都不该
// 因为他机器上还留着 Biu 的目录而被覆盖掉。
func TestAnInstallThatAlreadyHasDataIsNotOverwrittenByAnAutomaticImport(t *testing.T) {
	biu := biuHomeDir(t)
	dst := t.TempDir()
	// 我自己的设置。
	mine := app.DefaultSettings("/Users/y/Mine")
	mine.ThemeMode = app.ThemeModeLight
	if err := app.ExportSettings(app.SettingsPath(dst), mine); err != nil {
		t.Fatal(err)
	}
	// 换成我们自己的信封（ExportSettings 导的是裸的那一份）。
	if err := os.WriteFile(app.SettingsPath(dst),
		[]byte(`{"schemaVersion":1,"appSettings":{"themeMode":"light","primaryColor":"#ff0000"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	r := firstBiuImport(dst, []string{biu}, nil)
	if r.Any() {
		t.Fatalf("已经有数据了却还是自动导入了：%+v", r)
	}
	got, err := app.LoadSettings(app.SettingsPath(dst))
	if err != nil {
		t.Fatal(err)
	}
	if got.ThemeMode != app.ThemeModeLight || got.PrimaryColor != "#ff0000" {
		t.Fatalf("我自己的设置被改了：%+v", got)
	}
}

// 升级失败时不导入。
//
// 那时的目录可能处于「回滚了但标记说不清」的状态，在上面写东西是给一个说不清的
// 状态再加一层说不清。
func TestNoAutomaticImportWhenTheUpgradeFailed(t *testing.T) {
	biu := biuHomeDir(t)
	dst := t.TempDir()

	r := firstBiuImport(dst, []string{biu}, os.ErrInvalid)
	if r.Any() {
		t.Fatalf("升级失败了却还是导入了：%+v", r)
	}
	if _, err := os.Stat(app.SettingsPath(dst)); err == nil {
		t.Fatal("升级失败时仍然写了设置文件")
	}
}

// 机器上没有 Biu：什么都不做，也不报错。
func TestNoBiuOnThisMachineIsNotAnError(t *testing.T) {
	dst := t.TempDir()
	if r := firstBiuImport(dst, nil, nil); r.Any() {
		t.Fatalf("机器上没有 Biu 却导出了东西：%+v", r)
	}
	// 而目录里一个文件都不该多出来。
	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("没有 Biu 却往目录里写了：%v", names)
	}
}

// 导进来之后，界面上的「配置」那一段要说得清做了什么。
func TestTheImportReportReadsLikeASentence(t *testing.T) {
	got := biuImportToast(app.Imported{
		Settings:  true,
		Shortcuts: true,
		Downloads: 1,
		Lyrics:    1,
		Notes:     []string{"Biu 那边是登录状态，请重新登录一次。"},
	})
	for _, want := range []string{"设置", "快捷键", "1 条下载记录", "1 首歌词", "重新登录"} {
		if !strings.Contains(got, want) {
			t.Errorf("那句话里没有 %q：%s", want, got)
		}
	}

	// 一个都没带过来时要说「没有」，而不是甩一份四项全 0 的报告。
	empty := biuImportToast(app.Imported{})
	if !strings.Contains(empty, "没有") {
		t.Errorf("空报告的话是 %q", empty)
	}
}

// saysAbout 报一批话里有没有提到某个词。
func saysAbout(notes []string, word string) bool {
	for _, n := range notes {
		if strings.Contains(n, word) {
			return true
		}
	}
	return false
}

func httpClientForTest() *http.Client { return &http.Client{} }

// 先升级、再自动导入——这个顺序下第一次运行**照样**要导。
//
// 顺序不是随意的：`MountMigration` 就是先 `runUpgrade` 再 `firstBiuImport`，而
// 第一次运行时那一轮升级会写下总标记 `data-version.json`（哪怕四份数据文件一个
// 都不存在）。把标记算成「用户已有数据」的话，自动导入在第一次运行时永远被跳过
// ——而那恰恰是它唯一该跑的场景。
//
// 这一条是被真代码带出来的：写完 `MountMigration` 之后才注意到 `runUpgrade`
// 成功时一定会写标记。
func TestTheFirstRunUpgradesAndThenStillImportsFromBiu(t *testing.T) {
	biu := biuHomeDir(t)
	dst := t.TempDir()

	var rep MigrationReport
	if err := runUpgrade(dst, &rep); err != nil {
		t.Fatalf("第一次运行的升级失败了：%v", err)
	}
	// 标记确实被写下来了（哪怕目录是空的）。
	if _, err := os.Stat(filepath.Join(dst, app.UpgradeFileName)); err != nil {
		t.Fatalf("第一次运行没有写下总控标记：%v", err)
	}

	r := firstBiuImport(dst, []string{biu}, nil)
	if !r.Any() {
		t.Fatalf("第一次运行升级之后没有自动导入：%+v", r)
	}
	if !r.Settings || !r.Shortcuts || r.Downloads != 1 || r.Lyrics != 1 {
		t.Fatalf("四样没有全带过来：%+v", r)
	}
}
