package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// 快捷键存储测试使用真实文件，覆盖默认值、持久化和当前格式保护。

// TestTheShortcutDefaultsAreStable 逐条钉住 Biu 的默认值。
//
// 抄错一条的症状是「Biu 里按 ⌘⇧P，我们这边按 ⌘⇧P 没反应」，而两边的界面都
// 画得好好的，所以这一条只能靠把值逐条写出来比。
func TestTheShortcutDefaultsAreStable(t *testing.T) {
	got := defaultShortcutSettings()

	if !got.EnableGlobalShortcuts {
		t.Error("默认是开着全局快捷键的")
	}
	want := []struct{ id, name, in, global string }{
		{"togglePlay", "播放/暂停", "CommandOrControl+P", "CommandOrControl+Alt+P"},
		{"prev", "上一首", "CommandOrControl+Left", "CommandOrControl+Alt+Left"},
		{"next", "下一首", "CommandOrControl+Right", "CommandOrControl+Alt+Right"},
		{"volumeUp", "音量加", "CommandOrControl+Up", "CommandOrControl+Alt+Up"},
		{"volumeDown", "音量减", "CommandOrControl+Down", "CommandOrControl+Alt+Down"},
		{"toggleMiniMode", "切换mini/完整模式", "CommandOrControl+M", "CommandOrControl+Alt+M"},
	}
	// 我们自己补的两条（快退 / 快进）只在应用内那一列，排在 Biu 那六条后面；
	// 全局那一列仍然只有 Biu 的六条。
	if len(got.Shortcuts) != len(want)+2 || len(got.GlobalShortcuts) != len(want) {
		t.Fatalf("命令条数是 %d / %d，want %d / %d", len(got.Shortcuts), len(got.GlobalShortcuts), len(want)+2, len(want))
	}
	for i, w := range want {
		checkShortcut(t, "应用内", got.Shortcuts[i], w.id, w.name, w.in)
		checkShortcut(t, "全局", got.GlobalShortcuts[i], w.id, w.name, w.global)
	}
	checkShortcut(t, "应用内", got.Shortcuts[6], "seekBack", "快退 5 秒", "Shift+Left")
	checkShortcut(t, "应用内", got.Shortcuts[7], "seekForward", "快进 5 秒", "Shift+Right")
	for _, g := range got.GlobalShortcuts {
		if g.ID == "seekBack" || g.ID == "seekForward" {
			t.Errorf("「%s」不该有全局那一半（⌘⌥ 加方向键要留给别的应用）", g.ID)
		}
	}
}

func checkShortcut(t *testing.T, col string, got shortcutItem, id, name, want string) {
	t.Helper()
	if got.ID != id || got.Name != name || got.Shortcut != want {
		t.Errorf("%s那一条是 %+v，want {%s %s %s}", col, got, id, name, want)
	}
}

// TestTheDefaultsAreAlsoWhatAMissingFileGets 验「没有这个文件」不是错误。
func TestTheDefaultsAreAlsoWhatAMissingFileGets(t *testing.T) {
	got, ro := readShortcuts(filepath.Join(t.TempDir(), "nope.json"))
	if ro {
		t.Error("没有这个文件不该是只读")
	}
	checkShortcut(t, "应用内", got.Shortcuts[0], "togglePlay", "播放/暂停", "CommandOrControl+P")
}

// TestARecordedShortcutLandsOnDiskAndComesBack 验存住。
//
// ⚠️ 特意不等后台那 120ms 的合并写窗口：能存住就只能是 `Close` 那一次同步落盘
// 的功劳（和设置那边「杀掉进程重启还在」是同一条验收）。
func TestARecordedShortcutLandsOnDiskAndComesBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), shortcutFileName)
	k := newShortcutKeys(path)

	if err := k.set(shortcutSlot{id: "togglePlay"}, "CommandOrControl+Shift+K"); err != nil {
		t.Fatal(err)
	}
	if err := k.setEnableGlobal(false); err != nil {
		t.Fatal(err)
	}
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}

	back := newShortcutKeys(path)
	defer back.Close()
	got := back.snapshot()
	checkShortcut(t, "应用内", got.Shortcuts[0], "togglePlay", "播放/暂停", "CommandOrControl+Shift+K")
	if got.EnableGlobalShortcuts {
		t.Error("「启用全局快捷键」没存上")
	}
	// 两列互不相干：改应用内那一格，全局那一格还是原样。
	checkShortcut(t, "全局", got.GlobalShortcuts[0], "togglePlay", "播放/暂停", "CommandOrControl+Alt+P")
}

// TestAFileFromANewerVersionIsReadOnly 验只读保护。
//
// 只读时**一个字都不许写回去**：按旧语义改一遍就把用户的快捷键毁了。这一格是
// 这一份 store 唯一的「会丢数据」的地方，所以单独立一条。
func TestAFileFromANewerVersionIsReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), shortcutFileName)
	writeJSON(t, path, map[string]any{
		"schemaVersion": 99,
		"shortcuts":     []map[string]string{{"id": "togglePlay", "name": "播放/暂停", "shortcut": "CommandOrControl+J"}},
	})

	k := newShortcutKeys(path)
	defer k.Close()
	if !k.readOnly() {
		t.Fatal("来自更新版本的文件没有被判成只读")
	}
	before, _ := os.ReadFile(path)
	if err := k.set(shortcutSlot{id: "togglePlay"}, "CommandOrControl+X"); err == nil {
		t.Error("只读时竟然存成功了")
	}
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Errorf("只读的文件被改写了：\n前 %s\n后 %s", before, after)
	}
}

// TestAnUnrecognizableFileIsLeftAlone 验认不出的文件只读、不备份、不改写。
func TestAnUnrecognizableFileIsLeftAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), shortcutFileName)
	writeJSON(t, path, map[string]any{"someOtherApp": 1})

	k := newShortcutKeys(path)
	defer k.Close()
	if !k.readOnly() {
		t.Error("认不出的文件没有被判成只读")
	}
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != `{"someOtherApp":1}` {
		t.Errorf("别人写的文件被改写了：%s", raw)
	}
}

// TestACorruptFileIsBackedUpAndDefaultsComeBack 验坏文件不吞设置。
func TestACorruptFileIsBackedUpAndDefaultsComeBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), shortcutFileName)
	if err := os.WriteFile(path, []byte("{不是 JSON"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ro := readShortcuts(path)
	if ro {
		t.Error("坏文件该退回默认值接着用，不该是只读")
	}
	checkShortcut(t, "应用内", got.Shortcuts[0], "togglePlay", "播放/暂停", "CommandOrControl+P")
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf("坏的那份没有留成 .bak：%v", err)
	}
}

// TestRestoreDefaultsBringsTheDefaultKeysBack 验「恢复默认」。
//
// 特意先改坏三格再恢复，并且**用户自己多出来的那一条要留着**——Biu 是整份替换，
// 那一版会把用户加的键一起抹掉。
func TestRestoreDefaultsBringsTheDefaultKeysBack(t *testing.T) {
	k := newShortcutKeys("")
	defer k.Close()

	for _, id := range []string{"togglePlay", "prev", "next"} {
		if err := k.set(shortcutSlot{id: id}, "CommandOrControl+Q"); err != nil {
			t.Fatal(err)
		}
	}
	// 用户自己加的一条：不在默认那六条里。
	k.mu.Lock()
	k.cur.Shortcuts = append(k.cur.Shortcuts, shortcutItem{ID: "mine", Name: "我加的", Shortcut: "CommandOrControl+9"})
	k.mu.Unlock()

	if err := k.reset(); err != nil {
		t.Fatal(err)
	}
	got := k.snapshot()
	checkShortcut(t, "应用内", got.Shortcuts[0], "togglePlay", "播放/暂停", "CommandOrControl+P")
	checkShortcut(t, "全局", got.GlobalShortcuts[0], "togglePlay", "播放/暂停", "CommandOrControl+Alt+P")
	if len(got.Shortcuts) != len(defaultShortcutSettings().Shortcuts)+1 {
		t.Errorf("恢复默认之后是 %d 条，want 默认快捷键数 + 用户自己那条", len(got.Shortcuts))
	}
	checkShortcut(t, "用户自己那条", got.Shortcuts[len(got.Shortcuts)-1], "mine", "我加的", "CommandOrControl+9")
}

// TestConflictsAreOnlyLookedForInsideOneColumn 验冲突只在同一列里判。
//
// 应用内的 ⌘P 和全局的 ⌘P 可以同时存在——全局那个是在别的应用里按的。把它判成
// 冲突的后果是用户「明明把两个键都设对了，界面上却一直说冲突」。
func TestConflictsAreOnlyLookedForInsideOneColumn(t *testing.T) {
	list := []shortcutItem{
		{ID: "togglePlay", Shortcut: "CommandOrControl+P"},
		{ID: "prev", Shortcut: "CommandOrControl+Left"},
	}
	// 全局那列里 `CommandOrControl+Alt+P` 就是 prev 的键（默认就是）。它不在这一列
	// 里，所以**不算冲突**——两列各按各的判，判错了用户会看着「冲突」去改一个本来
	// 没问题的键。
	if other, bad := conflictOf(list, "prev", "CommandOrControl+Alt+P"); bad {
		t.Errorf("和别的列撞了也算冲突：%+v", other)
	}
	other, bad := conflictOf(list, "prev", "CommandOrControl+P")
	if !bad {
		t.Fatal("真的和同列的另一条撞上了却没报")
	}
	if other.ID != "togglePlay" {
		t.Errorf("报出来的是 %q，want togglePlay", other.ID)
	}
	if _, bad := conflictOf(list, "prev", ""); bad {
		t.Error("空键不该算冲突（Biu 同一段里明写着「空快捷键不会冲突」）")
	}
	if _, bad := conflictOf(list, "togglePlay", "CommandOrControl+P"); bad {
		t.Error("自己跟自己不算冲突")
	}
}

// TestKeysAreWrittenInTheCurrentFormat 验当前快捷键文件的顶层结构。
func TestKeysAreWrittenInTheCurrentFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), shortcutFileName)
	k := newShortcutKeys(path)
	// ⚠️ 得先改一处才会落盘：这一份和设置那边一样，**没改过就不写**（第一次运行
	// 不该凭空多出一个文件）。所以这一条顺便验的是「改一处之后写出去的形状」。
	if err := k.set(shortcutSlot{id: "togglePlay"}, "CommandOrControl+P"); err != nil {
		t.Fatal(err)
	}
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schemaVersion", "shortcuts", "globalShortcuts", "enableGlobalShortcuts"} {
		if _, ok := got[key]; !ok {
			t.Errorf("少了键 %q，写出去的是 %s", key, raw)
		}
	}
	// 三个快捷键字段必须保持顶层结构。
	for _, key := range []string{"shortcuts", "globalShortcuts", "enableGlobalShortcuts"} {
		inner := got[key]
		if len(inner) > 0 && inner[0] == '{' {
			t.Errorf("键 %q 被写成了嵌套的对象，Biu 那边取不到", key)
		}
	}
}

// writeJSON 写一份文件，测试里省得每次都 marshal 一遍。
func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// macSymbols 这台机器上显示成符号的是哪一套。测试里按同一个条件算期望值，
// 免得在 mac 上写死 ⌘、在别处又写不对。
func macSymbols() bool { return runtime.GOOS == "darwin" }
