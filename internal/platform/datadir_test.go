package platform

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

// 用假家目录盖住这一组测试，免得真的去动维护者本机的应用数据目录。
//
// `os.UserConfigDir()` 是**每次调用现读环境变量**的（darwin 读 `HOME`，Linux 读
// `XDG_CONFIG_HOME` 退回 `HOME/.config`，Windows 读 `AppData`），所以这里改完
// 立刻生效、而且 `t.Setenv` 会在测试结束时还原。
//
// 返回的是**配置目录本身**（不是家目录）：三个平台上它和家目录差的都不止一级
// （macOS 是 `~/Library/Application Support`），拿家目录去拼期望路径必然对不上。
func fakeUserConfigDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	switch runtime.GOOS {
	case "darwin", "ios":
		t.Setenv("HOME", home)
	case "windows":
		t.Setenv("AppData", home)
	default:
		t.Setenv("XDG_CONFIG_HOME", home)
	}
	// `PinUserDataDir` 动的是 `mygo.App` 这一个全局量，测完必须还回去，
	// 否则同一包里别的测试会拿到一个指向临时目录的数据目录。
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("假配置目录拿不到：%v", err)
	}
	return dir
}

// unpin 之后 `mygo.App.Path` 回到框架自己的默认值。单独拆出来是因为下面那条
// 要在**钉之前**看一眼那个默认值长什么样。
func unpin() { mygo.App.SetPath(mygo.PathUserData, "") }

// TestTheDataDirectoryNameIsNotTheDisplayName 钉住「显示名改了，目录名没跟着改」。
//
// 这一条是这个包里**最贵**的一条不变式：目录名一旦跟上显示名，老用户升级之后
// 设置、搜索历史、快捷键、歌词缓存、下载记录和界面状态会一起「消失」——
// 数据一个字节没坏，只是没人再去那个目录找。症状是静默的，没有任何一处报错。
//
// 所以这里读的是 `mygo.json` 里的**显示名**，断言它和目录名**不一样**：不是断言
// 「目录名是某个字面量」（那会在下次改名时无声地一起变），而是断言「这两件事从此
// 各走各的」。有人哪天把它们统一了，这条立刻红，而且红得正是时候。
func TestTheDataDirectoryNameIsNotTheDisplayName(t *testing.T) {
	name := appDisplayName(t)
	if name == LegacyUserDataDirName {
		t.Fatalf("mygo.json 里的应用名还是 %q——显示名和用户数据目录同名，"+
			"老用户升级之后数据目录会整个搬走（见本文件顶上那一节）", name)
	}
	// ⚠️ **不断言显示名的字面量**：显示名是可以改的（2026-10 改过一次，
	// 2026-10-26 又改成「布丁音乐」），而这条断的是**「显示名与目录名各走各的」**
	// 那个不变式——上面那句「显示名 == 目录名」才是它要抓的坏法。
	// 拿某个具体名字来断（「必须叫 BiuGo」）是**把显示名焊死在测试里**：
	// 改一次名就红一次，红完还得回来改测试——而那时人会开始想「是不是这条不许改」。
	if strings.Contains(name, "Biu My Go") {
		t.Errorf("mygo.json 里的应用名是 %q，和用户数据目录同名了——"+
			"老用户升级之后数据目录会整个搬走", name)
	}
}

// TestTheDataDirectoryIsNotMovedOntoTheDisplayName 钉住「钉住的那一步真的推翻了默认值」。
//
// 这条不比对字面量，只比对**行为**：框架本来会算出来的那个目录（跟着显示名走的
// `…/BiuGo`）和实际用的那个（`…/Biu MyGo`）必须是两个不同的目录，老目录原地
// 不动、跟着显示名走的那个压根没被建出来。这样下次再改名时它照样成立
// （而 `TestTheDataDirectoryNameIsNotTheDisplayName` 管的是「显示名不许退回去」）。
//
// ⚠️ 这里得先 `SetName`：**`go test` 里 `mygo.App.Name()` 拿不到打包时的应用名**
// （`packageInfo()` 读的是 app bundle 的 Info.plist，测试二进制没有），它退回的是
// 测试二进制的文件名。不手动设上，`App.Path` 算出来的默认目录就压根不是
// 「跟着显示名走的那个」，而这条测试要断的那件事就整个落空、还会绿。
func TestTheDataDirectoryIsNotMovedOntoTheDisplayName(t *testing.T) {
	base := fakeUserConfigDir(t)
	display := appDisplayName(t)
	// 还原成打包时的样子：应用名 = mygo.json 里那个。
	mygo.App.SetName(display)
	t.Cleanup(func() { mygo.App.SetName("") })

	legacy := filepath.Join(base, LegacyUserDataDirName)
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	// 老用户留下的一件东西。不放的话下面那条「没搬东西」断的是个空目录，
	// 搬不搬都绿。
	marker := filepath.Join(legacy, "settings.json")
	if err := os.WriteFile(marker, []byte(`{"schemaVersion":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	unpin()
	defaultDir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		t.Fatalf("问框架要默认的数据目录：%v", err)
	}
	if want := filepath.Join(base, display); defaultDir != want {
		t.Fatalf("框架的默认数据目录是 %q，want %q——"+
			"「默认跟着应用名走」这个前提不成立了，`PinUserDataDir` 存在的理由也就没了",
			defaultDir, want)
	}

	if err := PinUserDataDir(); err != nil {
		t.Fatalf("钉数据目录：%v", err)
	}
	got, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		t.Fatalf("问钉住之后的数据目录：%v", err)
	}
	if got != legacy {
		t.Errorf("数据目录是 %q，want %q（老目录要原地不动）", got, legacy)
	}
	// 关键的一条：跟着显示名走的那个目录里**一个文件都不该有**。搬过去了就说明
	// 哪天有人把「不迁移」改成了「迁移」，而搬目录这件事的失败模式是静默的。
	//
	// 断的是「里面有没有东西」而不是「目录在不在」：上面那次 `App.Path` 自己就
	// 会把默认目录建出来（框架的正常行为），所以那个目录多半**是**存在的——
	// 存在不等于搬了东西。
	if entries, err := os.ReadDir(defaultDir); err == nil {
		if len(entries) != 0 {
			t.Errorf("%s 里有 %d 个东西——这一版不该搬任何东西过去", defaultDir, len(entries))
		}
	}
	// 老用户那件东西也要还在原地（上面那条断的是「没往新目录搬」，这条断的是
	// 「没把老的搬空」——两件事凑一起才是「数据没丢」）。
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("老目录里的 settings.json 不在了：%v", err)
	}
}

// TestEverythingInTheOldDirectoryIsStillThere 验收项：改名之后老用户的数据还在。
//
// 这条不验「目录名对不对」（上一条已经钉了），只验**用户真正在意的后果**：
// 老目录里那些文件，改名之后**一个都还在原地、还读得到、内容没变**。
//
// 挑的这些是应用真放在用户数据目录里的东西（文件名抄自各自那一份源码）：
// 设置（`app.SettingsFileName`）、搜索历史（`search-history.json`）、快捷键
// （`shortcuts.json`）、歌词缓存目录（`lyrics/`）、升级快照（`data-snapshots/`）。
// 少一样，这一票就只是「目录没被改名」而不是「数据没丢」。
func TestEverythingInTheOldDirectoryIsStillThere(t *testing.T) {
	base := fakeUserConfigDir(t)
	legacy := filepath.Join(base, LegacyUserDataDirName)

	// 一个老用户升级前的样子。内容随便写，但**要各不相同**——只断「文件在」
	// 的话，一份被清空或串了内容的目录照样能过。
	want := map[string]string{
		"settings.json":                `{"schemaVersion":1,"appSettings":{"volume":42}}`,
		"search-history.json":          `["周杰伦","五月天"]`,
		"shortcuts.json":               `{"playPause":"Space"}`,
		"lyrics/song-lyrics.json":      `{"lyrics":"我是一只小小鸟"}`,
		"data-snapshots/readme-marker": "上一版的备份",
	}
	for rel, body := range want {
		p := filepath.Join(legacy, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := PinUserDataDir(); err != nil {
		t.Fatalf("钉数据目录：%v", err)
	}
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		t.Fatalf("问数据目录：%v", err)
	}
	if dir != legacy {
		t.Fatalf("数据目录是 %q，want %q", dir, legacy)
	}
	for rel, body := range want {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("读不到 %s：%v——改名之后老用户的数据「消失」就是这个样子", rel, err)
			continue
		}
		if string(got) != body {
			t.Errorf("%s 的内容变成了 %q，want %q", rel, got, body)
		}
	}
}

// TestAFreshInstallGetsThatDirectoryCreated 钉住「全新安装也用同一个目录」。
//
// 钉住之后 `mygo.App.Path` 走的是覆盖值那条分支，而**那条分支不建目录**
// （`paths.go:57-60`），所以 `PinUserDataDir` 自己得建。不建的话第一次写设置
// 会失败，而失败又被上层当成「拿不到目录」，界面提示的是另一件事——用户只会
// 发现「改了主题，下次打开又变回来了」。
func TestAFreshInstallGetsThatDirectoryCreated(t *testing.T) {
	base := fakeUserConfigDir(t)
	want := filepath.Join(base, LegacyUserDataDirName)

	if err := PinUserDataDir(); err != nil {
		t.Fatalf("钉数据目录：%v", err)
	}
	info, err := os.Stat(want)
	if err != nil {
		t.Fatalf("全新安装时 %s 没有被建出来：%v", want, err)
	}
	if !info.IsDir() {
		t.Errorf("%s 不是目录", want)
	}
	// 还要能从它里面真的写进去：存在但写不了（权限、配额）是同一类症状。
	probe := filepath.Join(want, ".probe")
	if err := os.WriteFile(probe, []byte("x"), 0o644); err != nil {
		t.Errorf("新建的目录写不进去：%v", err)
	}
}

// appDisplayName 读 `mygo.json` 里的应用名（显示名）。
//
// ⚠️ **不调 `mygo.App.Name()`**：那个函数在没有打包应用时退回**可执行文件名**
// （`mygo@v0.6.3/app.go:275-287`），在 `go test` 里那就是测试二进制的名字，
// 拿它去比较等于什么都没比。这里要的是**界面上写着的那个名字**，所以直接读文件。
func appDisplayName(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "mygo.json"))
	if err != nil {
		t.Fatalf("读不到 mygo.json：%v", err)
	}
	var cfg struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("解 mygo.json：%v", err)
	}
	if cfg.Name == "" {
		t.Fatalf("mygo.json 里的 \"name\" 是空的")
	}
	return cfg.Name
}
