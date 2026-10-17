package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 快捷键和搜索历史的迁移入口，以及启动时把它们和设置、队列一起升的那一轮。

// ─────────────────────────────────────────────────────────────────────────────
// 快捷键
// ─────────────────────────────────────────────────────────────────────────────

// Biu 的 `shortcut-settings.json`（没有 schemaVersion，按 v0 读）迁到当前版本。
//
// 迁完的那一份**必须是我们自己的信封**（顶层有 schemaVersion、三个键摊平），
// 而且用户自己录的键原样保留。
func TestBiuShortcutsComeAcrossInOurOwnEnvelope(t *testing.T) {
	dir := t.TempDir()
	path := shortcutPath(dir)
	body := `{
      "enableGlobalShortcuts": false,
      "shortcuts": [
        { "id": "togglePlay", "name": "播放/暂停", "shortcut": "CommandOrControl+Shift+P" },
        { "id": "prev", "name": "上一首", "shortcut": "CommandOrControl+Left" },
        { "id": "next", "name": "下一首", "shortcut": "CommandOrControl+Right" },
        { "id": "volumeUp", "name": "音量加", "shortcut": "CommandOrControl+Up" },
        { "id": "volumeDown", "name": "音量减", "shortcut": "CommandOrControl+Down" },
        { "id": "toggleMiniMode", "name": "切换mini/完整模式", "shortcut": "CommandOrControl+M" }
      ],
      "globalShortcuts": [
        { "id": "togglePlay", "name": "播放/暂停", "shortcut": "CommandOrControl+Alt+P", "isConflict": false }
      ]
    }`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MigrateShortcutsFile(path); err != nil {
		t.Fatal(err)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(readFile(t, path)), &top); err != nil {
		t.Fatal(err)
	}
	if _, ok := top["schemaVersion"]; !ok {
		t.Fatalf("迁完的那一份没有 schemaVersion：%s", readFile(t, path))
	}
	// ⚠️ 三个键必须在**顶层**（匿名嵌入），埋进一层对象里 Biu 一个都取不到
	// （`shortcutFile` 的注释）。
	for _, k := range []string{"enableGlobalShortcuts", "shortcuts", "globalShortcuts"} {
		if _, ok := top[k]; !ok {
			t.Fatalf("迁完的那一份顶层没有 %q：%v", k, keysOfRaw(top))
		}
	}
	// 用户自己录的那个键原样保留。
	if !strings.Contains(string(top["shortcuts"]), "CommandOrControl+Shift+P") {
		t.Fatalf("用户在 Biu 里录的键丢了：%s", top["shortcuts"])
	}
	// `isConflict` 是 Biu 自己算出来再存下去的，我们不读也不带回
	// （`shortcutItem` 的注释）：存一份在文件里只会和下一次算出来的对不上。
	if strings.Contains(readFile(t, path), "isConflict") {
		t.Fatalf("Biu 那个算出来的字段被带回来了：%s", readFile(t, path))
	}
	// 挂上去读得回来，而且少了的那一条命令被补上了。
	k := newShortcutKeys(path)
	defer k.Close()
	got := k.snapshot()
	if got.EnableGlobalShortcuts {
		t.Errorf("「启用全局快捷键」没有迁过来")
	}
	if len(got.Shortcuts) != 6 {
		t.Errorf("迁过来 %d 条应用内快捷键，期望 6 条", len(got.Shortcuts))
	}
	if len(got.GlobalShortcuts) != 6 {
		t.Errorf("全局那列没有被补齐：%d 条", len(got.GlobalShortcuts))
	}
}

// 一份来自更新版本的快捷键文件：不碰，并报错。
//
// 迁移这里返回错（而不是像读盘那样安静地只读）是有意的：调用方是**一次性的
// 事务**，它需要知道「这一份没升成」，否则会把总标记推到最新版，从此再也不会碰
// 这一份文件。
func TestShortcutsFromANewerVersionAreNeitherMigratedNorTouched(t *testing.T) {
	dir := t.TempDir()
	path := shortcutPath(dir)
	body := `{"schemaVersion":` + itoa(shortcutSchema+3) + `,"shortcuts":[],"globalShortcuts":[]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	err := MigrateShortcutsFile(path)
	if err == nil {
		t.Fatal("一份未来的快捷键文件没有被拦下")
	}
	if got := readFile(t, path); got != body {
		t.Fatalf("那份文件被改写了：\n改前 %s\n改后 %s", body, got)
	}
	if !strings.Contains(err.Error(), "没有动它") {
		t.Errorf("错误里没有说清「没有动它」：%v", err)
	}
}

// 认不出是什么文件：不动，并报错。
func TestAFileThatIsNotShortcutsAtAllIsNeitherMigratedNorTouched(t *testing.T) {
	dir := t.TempDir()
	path := shortcutPath(dir)
	body := `{"somethingElse": [1,2,3]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MigrateShortcutsFile(path); err == nil {
		t.Fatal("一份不是快捷键的文件没有被拦下")
	}
	if got := readFile(t, path); got != body {
		t.Fatalf("那份文件被改写了：%s", got)
	}
}

// 文件不存在不是错误（第一次运行）。
func TestAMissingFileIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	if err := MigrateShortcutsFile(shortcutPath(dir)); err != nil {
		t.Errorf("没有快捷键文件时迁移报了错：%v", err)
	}
	if err := MigrateSearchHistoryFile(searchHistoryPath(dir)); err != nil {
		t.Errorf("没有搜索历史时迁移报了错：%v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 搜索历史
// ─────────────────────────────────────────────────────────────────────────────

// 搜索历史 v0 → 当前版本，写回带 schemaVersion 的形状，条目原样保留。
func TestSearchHistoryIsMigratedAndItsItemsAreKept(t *testing.T) {
	dir := t.TempDir()
	path := searchHistoryPath(dir)
	if err := os.WriteFile(path, []byte(`{"items":[{"value":"甲","time":1},{"value":"乙","time":2}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSearchHistoryFile(path); err != nil {
		t.Fatal(err)
	}
	body := readFile(t, path)
	if !strings.Contains(body, `"schemaVersion"`) || !strings.Contains(body, "甲") || !strings.Contains(body, "乙") {
		t.Fatalf("迁完的搜索历史不对：%s", body)
	}

	h := newSearchHistory(path)
	defer h.Close()
	items := h.list()
	if len(items) != 2 || items[0].Value != "甲" {
		t.Fatalf("迁完之后读出来是 %v", items)
	}
}

// 一份未来的搜索历史：不碰，并报错。
func TestSearchHistoryFromANewerVersionIsNotTouched(t *testing.T) {
	dir := t.TempDir()
	path := searchHistoryPath(dir)
	body := `{"schemaVersion":` + itoa(searchHistorySchema+2) + `,"items":[]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSearchHistoryFile(path); err == nil {
		t.Fatal("一份未来的搜索历史没有被拦下")
	}
	if got := readFile(t, path); got != body {
		t.Fatalf("那份文件被改写了：%s", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 启动那一轮：四份文件一起升
// ─────────────────────────────────────────────────────────────────────────────

// 启动时四份文件一起升；其中一份坏了，整轮回滚，原文件全回来。
//
// 这是这一票最核心的那条断言：升级失败的表现**不是**「没升成」，是「升了一半」
// ——设置已是新版、队列还是旧版，而没有任何地方记着这件事。
func TestTheStartupUpgradeMovesAllFourFilesOrNone(t *testing.T) {
	dir := t.TempDir()
	settings := app.SettingsPath(dir)
	shortcuts := shortcutPath(dir)
	history := searchHistoryPath(dir)
	queue := filepath.Join(dir, "download-queue.json")

	// 设置是 Biu 导出的裸文件（v0，要走迁移链）；其余三份是当前版本。
	writeFile(t, settings, `{"appSettings":{"primaryColor":"#17c964","fontFamily":"system-default"}}`)
	writeFile(t, shortcuts, mustShortcutsJSON(t))
	writeFile(t, history, `{"schemaVersion":1,"items":[{"value":"甲","time":1}]}`)
	writeFile(t, queue, `{"version":1,"tasks":[]}`)

	var rep MigrationReport
	if err := runUpgrade(dir, &rep); err != nil {
		t.Fatalf("升级失败了：%v", err)
	}
	if rep.UpgradeErr != nil {
		t.Fatalf("升级报了错：%v", rep.UpgradeErr)
	}
	// 设置从 v0 升到 v1：坏字体值被修掉，并写上了 schemaVersion。
	got, err := app.LoadSettings(settings)
	if err != nil {
		t.Fatalf("升完的设置读不懂：%v", err)
	}
	if got.FontFamily != app.DefaultFontFamily {
		t.Errorf("Biu 那个 \"system-default\" 没被修掉：%q", got.FontFamily)
	}
	if got.PrimaryColor != "#17c964" {
		t.Errorf("主色没带过去：%q", got.PrimaryColor)
	}
	// 其余三份一个字都不该被动。
	for _, p := range []string{shortcuts, history, queue} {
		if migratingLeft(t, dir, p) {
			t.Errorf("%s 旁边还留着迁移备份", filepath.Base(p))
		}
	}
	if m := markerVersion(t, dir); m != app.CurrentDataVersion {
		t.Errorf("总标记停在 v%d，不是 v%d", m, app.CurrentDataVersion)
	}
}

// 队列文件版本对不上：整轮回滚，四份文件原样。
func TestTheStartupUpgradeRollsEverythingBackWhenOneFileIsFromTheFuture(t *testing.T) {
	dir := t.TempDir()
	settings := app.SettingsPath(dir)
	queue := filepath.Join(dir, "download-queue.json")

	settingsBefore := `{"appSettings":{"primaryColor":"#17c964"}}`
	queueBefore := `{"version":99,"tasks":[]}`
	writeFile(t, settings, settingsBefore)
	writeFile(t, queue, queueBefore)

	var rep MigrationReport
	err := runUpgrade(dir, &rep)
	if err == nil {
		t.Fatal("一份 v99 的队列没有被拦下")
	}
	if got := readFile(t, settings); got != settingsBefore {
		t.Fatalf("设置没有被恢复：\n改前 %s\n改后 %s", settingsBefore, got)
	}
	if got := readFile(t, queue); got != queueBefore {
		t.Fatalf("队列被改写了：\n改前 %s\n改后 %s", queueBefore, got)
	}
	if !rep.Rollback {
		t.Errorf("报告里没有说「回滚了」：%+v", rep)
	}
	if !strings.Contains(rep.Upgraded, "原来的数据一个字节都没少") {
		t.Errorf("升级失败那句话里没有说原文件还在：%q", rep.Upgraded)
	}
	// 备份也不能留在用户目录里当垃圾。
	for _, p := range []string{settings, queue} {
		if migratingLeft(t, dir, p) {
			t.Errorf("%s 旁边还留着迁移备份", filepath.Base(p))
		}
	}
}

// 已经是最新的一轮：不写任何东西，也不留标记。
//
// 每次启动都重写一遍文件的话，用户目录的 mtime 全在变，而有些备份工具按 mtime
// 判断「改过了」——用户会收到一堆「你的设置变了」的提示，而什么也没变。
func TestASecondLaunchWithNothingToDoWritesNothing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, app.SettingsPath(dir), mustSettingsJSON(t))
	writeFile(t, shortcutPath(dir), mustShortcutsJSON(t))

	// 第一轮把标记立起来。
	var first MigrationReport
	if err := runUpgrade(dir, &first); err != nil {
		t.Fatal(err)
	}
	before := statModTime(t, app.SettingsPath(dir))

	var second MigrationReport
	if err := runUpgrade(dir, &second); err != nil {
		t.Fatal(err)
	}
	if second.Upgraded != "" {
		t.Errorf("没有要升的，却说 %q", second.Upgraded)
	}
	if len(second.UpgradedFiles) != 0 {
		t.Errorf("没有要升的，却说动了 %v", second.UpgradedFiles)
	}
	if got := statModTime(t, app.SettingsPath(dir)); !got.Equal(before) {
		t.Errorf("没有要升的，设置文件却被重写了：%v → %v", before, got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 小工具
// ─────────────────────────────────────────────────────────────────────────────

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func statModTime(t *testing.T, path string) time.Time {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.ModTime()
}

func markerVersion(t *testing.T, dir string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, app.UpgradeFileName))
	if err != nil {
		t.Fatalf("读总控标记：%v", err)
	}
	var m struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("总控标记读不懂：%v", err)
	}
	return m.Version
}

func migratingLeft(t *testing.T, dir, file string) bool {
	t.Helper()
	p := filepath.Join(filepath.Dir(file), "."+filepath.Base(file)+".migrating")
	_, err := os.Stat(p)
	return err == nil
}

func keysOfRaw(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// mustShortcutsJSON 造一份「当前版本」的快捷键文件。
func mustShortcutsJSON(t *testing.T) string {
	t.Helper()
	s := defaultShortcutSettings()
	b, err := json.Marshal(shortcutFile{SchemaVersion: shortcutSchema, shortcutSettings: s})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// mustSettingsJSON 造一份「当前版本」的设置文件。
func mustSettingsJSON(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"schemaVersion": app.SchemaVersion,
		"appSettings":   app.DefaultSettings(""),
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
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
