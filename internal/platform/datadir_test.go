package platform

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

// TestTheDataDirectoryNameTracksTheDisplayName 钉住「目录名跟着应用定名走」。
//
// 数据目录名是 `PudMusic`，和应用定名（`PudMusic`）一致——这是**迁过来之后**
// 的样子：老用户的 `Biu MyGo` 被 `os.Rename` 搬过来，新装的也直接落在这里。
// 目录里装的是设置、搜索历史、快捷键、歌词缓存、下载记录和登录态——**名字
// 本身不许「顺手」再改**，下一次要改就得再走一次 `os.Rename` 迁移，理由和
// 做法见 `datadir.go` 顶上那一节。
func TestTheDataDirectoryNameTracksTheDisplayName(t *testing.T) {
	if LegacyUserDataDirName != "PudMusic" {
		t.Errorf("应用数据目录名是 %q，want PudMusic——目录名不跟着定名走，"+
			"老用户升级之后 Finder 里找不到「布丁音乐」那一摞数据", LegacyUserDataDirName)
	}
}

// TestAnOldUsersDirectoryIsMigratedOntoTheNewName 验收项：旧目录被整个搬到新名字上。
//
// 老用户的样子：`…/Biu MyGo` 还在、`…/PudMusic` 还没有。这一版开起来之后，
// 旧目录必须**原地消失**、内容**整个**出现在 `…/PudMusic` 里——`os.Rename`
// 是同分区原子的，要么整个过去、要么原地不动，没有「搬一半」的中间态。
// 搬过去之后数据目录钉在新的那个上。
func TestAnOldUsersDirectoryIsMigratedOntoTheNewName(t *testing.T) {
	base := fakeUserConfigDir(t)
	legacy := filepath.Join(base, oldUserDataDirName)
	dir := filepath.Join(base, LegacyUserDataDirName)

	// 老用户升级前的样子：旧目录里有一份设置。
	want := `{"schemaVersion":1,"appSettings":{"volume":42}}`
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "settings.json"), []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := PinUserDataDir(); err != nil {
		t.Fatalf("钉数据目录：%v", err)
	}
	got, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		t.Fatalf("问钉住之后的数据目录：%v", err)
	}
	if got != dir {
		t.Errorf("数据目录是 %q，want %q（旧目录要整个迁到新名字上）", got, dir)
	}
	if _, err := os.Stat(legacy); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("旧目录 %q 迁完之后还在（err=%v）", legacy, err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("新目录里读不到 settings.json：%v", err)
	}
	if string(body) != want {
		t.Errorf("settings.json 的内容变成了 %q，want %q", body, want)
	}
}

// TestTheMigrationLeavesEverythingInTheOldDirectoryInPlace 验收项：迁过来的
// 东西一个不少、内容不变。
//
// 挑的这些是应用真放在用户数据目录里的东西（文件名抄自各自那一份源码）：
// 设置（`app.SettingsFileName`）、搜索历史（`search-history.json`）、快捷键
// （`shortcuts.json`）、歌词缓存目录（`lyrics/`）、升级快照（`data-snapshots/`）。
// 少一样，这一票就只是「目录被改了名」而不是「数据没丢」。
func TestTheMigrationLeavesEverythingInTheOldDirectoryInPlace(t *testing.T) {
	base := fakeUserConfigDir(t)
	legacy := filepath.Join(base, oldUserDataDirName)
	dir := filepath.Join(base, LegacyUserDataDirName)

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
	if got, err := mygo.App.Path(mygo.PathUserData); err != nil || got != dir {
		t.Fatalf("数据目录是 %q（err=%v），want %q", got, err, dir)
	}
	for rel, body := range want {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("迁完之后在新目录里读不到 %s：%v", rel, err)
			continue
		}
		if string(got) != body {
			t.Errorf("%s 的内容变成了 %q，want %q", rel, got, body)
		}
	}
}

// TestBothDirectoriesExistKeepsBothAndUsesTheNewOne 钉住「新旧都在」那一支。
//
// 可能是迁过一次之后又有东西写回了旧目录，也可能是用户手动放的——无论哪种，
// **旧的那份留着不动**，数据目录直接用新的那个。不替用户做主去删或合并：
// 那种「帮你收拾一下」的代码错起来是静默的。
func TestBothDirectoriesExistKeepsBothAndUsesTheNewOne(t *testing.T) {
	base := fakeUserConfigDir(t)
	legacy := filepath.Join(base, oldUserDataDirName)
	dir := filepath.Join(base, LegacyUserDataDirName)

	for _, d := range []string{legacy, dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	oldMarker := filepath.Join(legacy, "settings.json")
	newMarker := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(oldMarker, []byte(`{"old":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newMarker, []byte(`{"new":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := PinUserDataDir(); err != nil {
		t.Fatalf("钉数据目录：%v", err)
	}
	got, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		t.Fatalf("问数据目录：%v", err)
	}
	if got != dir {
		t.Errorf("数据目录是 %q，want %q（新旧都在时用新的）", got, dir)
	}
	// 旧的那份留着：不删、不合并、不提示。
	if _, err := os.Stat(oldMarker); err != nil {
		t.Errorf("旧目录里的 settings.json 被动了：%v", err)
	}
	body, err := os.ReadFile(newMarker)
	if err != nil || string(body) != `{"new":true}` {
		t.Errorf("新目录里的 settings.json 被旧目录里的串了：%q（err=%v）", body, err)
	}
}

// TestTheMigrationFallsBackToTheOldDirectoryWhenRenameFails 钉住失败路径。
//
// `os.Rename` 会败（跨设备、权限不够、目录被占）。败了**不能**用空的新目录
// 开起来——那一下的代价是「设置安静地回到出厂」，用户以为数据丢了
// （`internal/app/migrate.go` 犯过那个错）。所以败了**退回旧目录**：照样钉、
// 照样开，但把错误**返回**给 bootstrap 记日志。
//
// 触发 rename 失败的方式：把旧目录放进**另一个**文件系统根下的目录里——
// `t.TempDir()` 在同一个分区上 rename 不过去，所以这里把「旧目录」建在
// 一个**不同的**父目录下，再让 `os.Rename` 跨不过去。同一分区上这个测试
// 只能断「rename 之后的行为对不对」，断不了「rename 本身会不会败」——
// 所以这里直接用一个**不存在的目标父目录**让 rename 必败（目标父目录不在，
// rename 返回 ENOENT），这是不挑平台的败法。
func TestTheMigrationFallsBackToTheOldDirectoryWhenRenameFails(t *testing.T) {
	base := fakeUserConfigDir(t)
	legacy := filepath.Join(base, oldUserDataDirName)
	dir := filepath.Join(base, LegacyUserDataDirName)

	// 让 rename 必败：换掉 `renameFunc`。跨设备（EXDEV）那种坏法没法用
	// 临时目录稳定地造出来——`t.TempDir()` 给的都在同一个分区上。
	t.Cleanup(func() { renameFunc = os.Rename })
	renameFunc = func(oldpath, newpath string) error {
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath,
			Err: errors.New("cross-device link")}
	}

	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(legacy, "settings.json")
	if err := os.WriteFile(marker, []byte(`{"schemaVersion":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := PinUserDataDir()
	if err == nil {
		t.Fatal("rename 必败时 PinUserDataDir 应该把错误报出来，不该吞掉")
	}
	got, gerr := mygo.App.Path(mygo.PathUserData)
	if gerr != nil {
		t.Fatalf("问退回之后的数据目录：%v", gerr)
	}
	if got != legacy {
		t.Errorf("rename 失败之后数据目录是 %q，want 退回旧目录 %q", got, legacy)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("旧目录里的 settings.json 不在了：%v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("新目录 %q 被建出来了（err=%v）——失败路径不该留下半成品", dir, err)
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
