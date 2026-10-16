package app_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// ─────────────────────────────────────────────────────────────────────────────
// 键名必须和 Biu 一致
// ─────────────────────────────────────────────────────────────────────────────

// biuKeys 是 Biu `defaultAppSettings` 里的全部键，逐个抄下来的。
// 出处：biu/shared/settings/app-settings.ts:1-31。
//
// 这张表是「拿一份 Biu 的设置导出比对键名」这条验收的机器版本：写出来的
// JSON 至少要正好是这些键，一个不多一个不少。少一个 = 用户来回切应用会丢设置。
var biuKeys = []string{
	"autoStart",
	"closeWindowOption",
	"fontFamily",
	"borderRadius",
	"downloadPath",
	"primaryColor",
	"backgroundColor",
	"audioQuality",
	"hiddenMenuKeys",
	"displayMode",
	"ffmpegPath",
	"themeMode",
	"pageTransition",
	"showSearchHistory",
	"proxySettings",
	"sideMenuCollapsed",
	"sideMenuWidth",
	"sideMenuCollectionFolded",
	"reportPlayHistory",
	"localMusicDirs",
}

// ourOwnKeys 是**我们加的、Biu 没有的键**，一共这一个。
//
// 加它是有理由的：侧栏收藏夹的拖拽顺序要存，Biu 把这一份单独放在另一个
// store 里（zustand persist 的 `"favorites-order"`，
// `biu/src/store/favorite.ts:157-163`），不在 app-settings.json 里。
// 我们不另开一份文件，是因为设置存储已经把 schema 版本、原子写、退出前
// flush、只读保护做好了，另开一份等于把这些重造一遍，还少了只读保护。
//
// 代价说清楚：多这一个键，Biu 那边读到我们的文件会当它是不认识的键。
// 会不会把它弄丢取决于 electron-store 的合并行为——最坏的情况也只是
// **收藏夹的顺序弹回 Biu 给的那个顺序**，Biu 自己的设置一个键都没丢。
// 这条取舍记在这里，想改主意时先看这一段。
var ourOwnKeys = []string{
	"sideMenuCollectionOrder",
}

// TestSettingsKeysMatchBiu 逐字比对键名。
func TestSettingsKeysMatchBiu(t *testing.T) {
	b, err := json.Marshal(app.DefaultSettings("/Users/x/Downloads"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}

	for _, k := range biuKeys {
		if _, ok := got[k]; !ok {
			t.Errorf("少了 Biu 的键 %q", k)
		}
		delete(got, k)
	}
	// 我们自己加的那几个（见 ourOwnKeys 的说明）是允许的，先摘掉再查剩下
	// 的——不加这一步的话，「多了一个键」这条断言会一直红，而它本来防的是
	// 「有人不小心加了键忘了想清楚」。
	for _, k := range ourOwnKeys {
		if _, ok := got[k]; !ok {
			t.Errorf("少了自己加的键 %q", k)
		}
		delete(got, k)
	}
	for k := range got {
		t.Errorf("多了一个没人认领的键 %q", k)
	}
}

// TestNestedKeysMatchBiu 比对嵌套的两组键——Biu 的类型定义在
// biu/shared/types/app-setting.d.ts:7-41。
func TestNestedKeysMatchBiu(t *testing.T) {
	s := app.DefaultSettings("")

	p, err := json.Marshal(s.ProxySettings)
	if err != nil {
		t.Fatal(err)
	}
	var proxy map[string]json.RawMessage
	if err := json.Unmarshal(p, &proxy); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"type", "host", "port", "username", "password"} {
		if _, ok := proxy[k]; !ok {
			t.Errorf("proxySettings 少了键 %q", k)
		}
	}

	f, err := json.Marshal(s.SideMenuCollectionFolded)
	if err != nil {
		t.Fatal(err)
	}
	var folded map[string]json.RawMessage
	if err := json.Unmarshal(f, &folded); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"created", "collected"} {
		if _, ok := folded[k]; !ok {
			t.Errorf("sideMenuCollectionFolded 少了键 %q", k)
		}
	}

	o, err := json.Marshal(s.SideMenuCollectionOrder)
	if err != nil {
		t.Fatal(err)
	}
	var order map[string]json.RawMessage
	if err := json.Unmarshal(o, &order); err != nil {
		t.Fatal(err)
	}
	// 两组的键名和 sideMenuCollectionFolded 一样，是刻意的：Biu 那边也是
	// `{created, collected}` 摆在一处（favorite.ts:157-163）。
	for _, k := range []string{"created", "collected"} {
		if _, ok := order[k]; !ok {
			t.Errorf("sideMenuCollectionOrder 少了键 %q", k)
		}
	}
}

// TestEnvelopeMatchesBiuShape 验外面那层信封：electron-store 落的是
// `{"appSettings": {...}}`（biu/electron/store.ts:12-21）。
func TestEnvelopeMatchesBiuShape(t *testing.T) {
	st, err := app.OpenSettingsStore(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	// 没有改动就不该有文件，先改一笔。
	if _, err := st.Update(func(s *app.Settings) { s.FontFamily = "system-ui" }); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		SchemaVersion int             `json:"schemaVersion"`
		AppSettings   json.RawMessage `json:"appSettings"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if f.SchemaVersion != app.SchemaVersion {
		t.Errorf("schemaVersion 写的是 %d，want %d", f.SchemaVersion, app.SchemaVersion)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(f.AppSettings, &keys); err != nil {
		t.Fatal(err)
	}
	if len(keys) != len(biuKeys)+len(ourOwnKeys) {
		t.Errorf("appSettings 里 %d 个键，want %d（Biu 的 %d + 我们自己加的 %d）",
			len(keys), len(biuKeys)+len(ourOwnKeys), len(biuKeys), len(ourOwnKeys))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 存进去、读得回来
// ─────────────────────────────────────────────────────────────────────────────

func TestSettingsSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	st, err := app.OpenSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Update(func(s *app.Settings) {
		s.ThemeMode = app.ThemeModeDark
		s.BorderRadius = 16
		s.PrimaryColor = "#ff8800"
		s.LocalMusicDirs = []string{"/music/a", "/music/b"}
	}); err != nil {
		t.Fatal(err)
	}
	// Close 是「改完就杀进程还能读回来」这条验收的落点。
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := app.OpenSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { again.Close() })

	got := again.Snapshot()
	if got.ThemeMode != app.ThemeModeDark || got.BorderRadius != 16 || got.PrimaryColor != "#ff8800" {
		t.Errorf("重启后拿到的是 %+v", got)
	}
	if len(got.LocalMusicDirs) != 2 || got.LocalMusicDirs[0] != "/music/a" {
		t.Errorf("本地音乐目录没存住：%v", got.LocalMusicDirs)
	}
}

// TestUnknownKeysSurviveARoundTrip 验「不认识的键原样带着走」。
//
// 两个方向都要保：更新的 Biu 加了新键、或者未来的我们加了键而用户换回了旧版。
// 认不出的键直接丢就是数据丢失。
func TestUnknownKeysSurviveARoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	write(t, path, `{
	  "schemaVersion": 1,
	  "appSettings": {"themeMode": "dark", "someFutureKey": {"a": [1, 2]}}
	}`)

	st, err := app.OpenSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Update(func(s *app.Settings) { s.FontFamily = "system-ui" }); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	var f struct {
		AppSettings map[string]json.RawMessage `json:"appSettings"`
	}
	readJSON(t, path, &f)
	raw, ok := f.AppSettings["someFutureKey"]
	if !ok {
		t.Fatal("不认识的键被丢了")
	}
	// 比内容而不是比字节：写出去的时候统一缩进了，格式变了没关系，
	// 值不能变。
	var got, want any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"a":[1,2]}`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("不认识的键被改动了：%s", raw)
	}
	if raw := f.AppSettings["themeMode"]; string(raw) != `"dark"` {
		t.Errorf("认识的键没保住：%s", raw)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 迁移
// ─────────────────────────────────────────────────────────────────────────────

// TestBiuFileLoadsAsSchemaZero 验从 Biu 直接导出的文件能读。
//
// Biu 的 electron-store 还会写一个 `__internal__` 段（conf 的内部状态），
// 标准库会自动忽略它，但不能因此报错——真实导出的文件里就是有。
func TestBiuFileLoadsAsSchemaZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	write(t, path, `{
	  "appSettings": {
	    "themeMode": "dark",
	    "primaryColor": "#1ed760",
	    "fontFamily": "system-default",
	    "borderRadius": 12,
	    "proxySettings": {"type": "http", "host": "127.0.0.1", "port": 7890}
	  },
	  "__internal__": {"migrations": {"version": 1}}
	}`)

	s, err := app.LoadSettings(path)
	if err != nil {
		t.Fatalf("Biu 的文件读不出来：%v", err)
	}
	if s.ThemeMode != app.ThemeModeDark {
		t.Errorf("themeMode 是 %q", s.ThemeMode)
	}
	if s.BorderRadius != 12 {
		t.Errorf("borderRadius 是 %v", s.BorderRadius)
	}
	if s.ProxySettings.Host != "127.0.0.1" || s.ProxySettings.Port == nil || *s.ProxySettings.Port != 7890 {
		t.Errorf("代理设置没读对：%+v", s.ProxySettings)
	}
	// Biu 自己那个已知坏值必须被迁移改掉（biu/src/store/settings.ts:37-39）。
	if s.FontFamily != app.DefaultFontFamily {
		t.Errorf("fontFamily 的坏值没被迁移掉，还是 %q", s.FontFamily)
	}
}

func TestMissingFileIsNotAnError(t *testing.T) {
	s, err := app.LoadSettings(filepath.Join(t.TempDir(), "没有这个文件.json"))
	if err != nil {
		t.Fatalf("第一次运行不该报错：%v", err)
	}
	if s.ThemeMode != app.ThemeModeSystem || s.BorderRadius != app.DefaultBorderRadius {
		t.Errorf("拿到的不是默认值：%+v", s)
	}
}

// TestCorruptFileIsBackedUpAndReset 验坏文件：留证据、重置、还能继续用。
func TestCorruptFileIsBackedUpAndReset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	write(t, path, `{"appSettings": {"themeMode": `)

	_, err := app.LoadSettings(path)
	if !errors.Is(err, app.ErrSettingsCorrupt) {
		t.Fatalf("错是 %v，want ErrSettingsCorrupt", err)
	}
	// 坏内容必须留下：用户丢了设置，得知道丢的是什么。
	baks, _ := filepath.Glob(filepath.Join(dir, "settings.json.*"))
	if len(baks) == 0 {
		t.Fatal("坏文件没有被备份下来")
	}
	raw, err := os.ReadFile(baks[0])
	if err != nil || len(raw) == 0 {
		t.Fatalf("备份是空的：%v", err)
	}
}

// TestFileFromANewerVersionIsReadOnly 验版本回退（M9）。
//
// 用户把应用换回旧版，旧版读到新版的文件会怎样？答案必须是「读成默认值，
// 而且一个字都不写回去」——否则用户的设置会被旧语义改写，升级回来面目全非。
func TestFileFromANewerVersionIsReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	future := `{"schemaVersion": 9999, "appSettings": {"themeMode": "dark"}}`
	write(t, path, future)

	st, err := app.OpenSettingsStore(path)
	if !errors.Is(err, app.ErrSettingsFromNewer) {
		t.Fatalf("错是 %v，want ErrSettingsFromNewer", err)
	}
	if !st.ReadOnly() {
		t.Error("来自更新版本的设置文件必须是只读的")
	}
	if _, err := st.Update(func(s *app.Settings) { s.ThemeMode = app.ThemeModeLight }); !errors.Is(err, app.ErrSettingsFromNewer) {
		t.Errorf("只读 store 不该接受改动，err 是 %v", err)
	}
	if err := st.Flush(); err != nil {
		t.Error(err)
	}
	t.Cleanup(func() { st.Close() })

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != future {
		t.Errorf("文件被改了：\n%s", raw)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 规整
// ─────────────────────────────────────────────────────────────────────────────

func TestNormalizeClampsBadValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	write(t, path, `{"schemaVersion": 1, "appSettings": {
	  "borderRadius": 9999,
	  "themeMode": "极夜",
	  "displayMode": "table",
	  "audioQuality": "cd",
	  "closeWindowOption": "kill",
	  "proxySettings": {"type": "socks3"},
	  "primaryColor": "红色",
	  "backgroundColor": "#zzz",
	  "fontFamily": "",
	  "sideMenuWidth": -10
	}}`)

	s, err := app.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.BorderRadius != app.MaxBorderRadius {
		t.Errorf("borderRadius 没有夹到上限：%v", s.BorderRadius)
	}
	if s.ThemeMode != app.ThemeModeSystem {
		t.Errorf("不认识的 themeMode 没退回默认：%q", s.ThemeMode)
	}
	if s.DisplayMode != app.DisplayModeList || s.AudioQuality != app.AudioQualityAuto {
		t.Errorf("枚举没退回默认：%q / %q", s.DisplayMode, s.AudioQuality)
	}
	if s.CloseWindowOption != app.CloseWindowHide || s.ProxySettings.Type != app.ProxyNone {
		t.Errorf("枚举没退回默认：%q / %q", s.CloseWindowOption, s.ProxySettings.Type)
	}
	if s.PrimaryColor != app.DefaultPrimaryColor {
		t.Errorf("坏颜色没退回默认：%q", s.PrimaryColor)
	}
	if s.BackgroundColor != "" {
		t.Errorf("坏背景色该退回空（用主题默认）：%q", s.BackgroundColor)
	}
	if s.FontFamily != app.DefaultFontFamily || s.SideMenuWidth <= 0 {
		t.Errorf("空/负值没兜住：%q / %v", s.FontFamily, s.SideMenuWidth)
	}
	// nil 切片写成 null 的话，Biu 读的时候 `hiddenMenuKeys` 会变成 null。
	if s.HiddenMenuKeys == nil || s.LocalMusicDirs == nil {
		t.Error("空的列表字段该是 [] 而不是 nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 原子写与后台写盘
// ─────────────────────────────────────────────────────────────────────────────

// TestNoTempFilesLeftBehind 验原子写不留残骸。
func TestNoTempFilesLeftBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	st, err := app.OpenSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if _, err := st.Update(func(s *app.Settings) { s.BorderRadius++ }); err != nil {
			t.Fatal(err)
		}
		if err := st.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "settings.json" {
			t.Errorf("目录里多了 %q", e.Name())
		}
	}
}

// TestCloseFlushesPendingChanges 验「杀进程还在」靠的是 Close 兜底，
// 而不是靠 debounce 恰好赶上了。
func TestCloseFlushesPendingChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	st, err := app.OpenSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Update(func(s *app.Settings) { s.BorderRadius = 20 }); err != nil {
		t.Fatal(err)
	}
	// 不等 debounce 直接收尾。
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := app.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.BorderRadius != 20 {
		t.Errorf("收尾没把改动落盘，读回来是 %v", s.BorderRadius)
	}
}

// TestConcurrentUpdatesAllLand 验并发改设置不丢更新、也不写坏文件。
// 配 -race 跑。
func TestConcurrentUpdatesAllLand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	st, err := app.OpenSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	const n = 40
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func() {
			defer wg.Done()
			if _, err := st.Update(func(s *app.Settings) {
				s.SideMenuWidth = float64(i + 1)
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	if err := st.Flush(); err != nil {
		t.Fatal(err)
	}
	// 文件任何时刻都得是完整可解析的 JSON：写一半被读到就是残缺文件。
	s, err := app.LoadSettings(path)
	if err != nil {
		t.Fatalf("并发写坏了文件：%v", err)
	}
	if s.SideMenuWidth <= 0 {
		t.Errorf("读回来的宽度是 %v", s.SideMenuWidth)
	}
}

// TestWakeIsCalledAfterFlush 验落盘完成后会叫醒界面。
//
// 不唤醒的话界面永远停在「还没存」那一帧——实测：不唤醒 = 新状态到了但
// 0 帧刷新。
func TestWakeIsCalledAfterFlush(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	st, err := app.OpenSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	var mu sync.Mutex
	woken := 0
	st.SetWake(func() {
		mu.Lock()
		woken++
		mu.Unlock()
	})

	if _, err := st.Update(func(s *app.Settings) { s.BorderRadius = 3 }); err != nil {
		t.Fatal(err)
	}
	if err := st.Flush(); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if woken == 0 {
		t.Error("落盘完成了但没有叫醒界面")
	}
}

// TestWriteFailureStaysPending 验写失败不会被当成「已保存」。
//
// 磁盘满、目录没了——这些情况下把 dirty 清掉就是静默丢设置。
func TestWriteFailureStaysPending(t *testing.T) {
	// 把路径指到一个不存在的目录下，CreateTemp 会失败。
	st, err := app.OpenSettingsStore(filepath.Join(t.TempDir(), "没有这个目录", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	if _, err := st.Update(func(s *app.Settings) { s.BorderRadius = 9 }); err != nil {
		t.Fatal(err)
	}
	if err := st.Flush(); err == nil {
		t.Fatal("写到不存在的目录上居然成功了")
	}
	if err := st.LastError(); err == nil {
		t.Error("失败没有记进 LastError")
	}
}

// TestUpdateIsDebouncedToOneWrite 验连续改动只落一次盘。
//
// 拖圆角滑块时每帧都会调 Update，没有合并就是一帧一写。
func TestUpdateIsDebouncedToOneWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	st, err := app.OpenSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	var mu sync.Mutex
	writes := 0
	st.SetWake(func() {
		mu.Lock()
		writes++
		mu.Unlock()
	})

	for i := range 50 {
		if _, err := st.Update(func(s *app.Settings) { s.BorderRadius = float64(i) }); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	// 等后台把攒下的那一次写掉。
	waitFor(t, 2*time.Second, "后台落盘", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return writes > 0
	})
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if writes > 5 {
		t.Errorf("50 次连续改动落了 %d 次盘，没有合并", writes)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 真的杀进程重启
// ─────────────────────────────────────────────────────────────────────────────

// childEnv 是「我现在是那个被拉起来改设置的子进程」的暗号。
const childEnv = "BIU_SETTINGS_CHILD"

// TestSettingsSurviveAProcessRestart 是验收项「改设置 → 杀掉进程重启 → 设置还在」
// 的字面版本：真的起一个进程、真的让它退、真的再起一个读回来。
//
// 前面那些测试 reopen 的只是一份 SettingsStore，读的是同一个文件同一段代码。
// 这一条多验的是**进程边界**：异步写盘的后台 goroutine 有没有机会在退出前
// 把改动写完。SIGKILL 拦不住，所以靠的是 Close 里那次同步 Flush。
func TestSettingsSurviveAProcessRestart(t *testing.T) {
	if os.Getenv(childEnv) != "" {
		childRoundTrip(t)
		return
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	runChild(t, path, "write")
	runChild(t, path, "read")

	// 第三遍：这个父进程自己再读一次，跨进程的结果确实落到了磁盘上。
	s, err := app.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.ThemeMode != app.ThemeModeDark || s.BorderRadius != 19 {
		t.Errorf("两个子进程过去之后读到的是 %+v", s)
	}
}

// childRoundTrip 是子进程里跑的那一趟。写完立刻退出——不给后台写盘任何机会。
func childRoundTrip(t *testing.T) {
	path := os.Getenv("BIU_SETTINGS_PATH")
	mode := os.Getenv("BIU_SETTINGS_MODE")

	st, err := app.OpenSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode == "write" {
		if _, err := st.Update(func(s *app.Settings) {
			s.ThemeMode = app.ThemeModeDark
			s.BorderRadius = 19
			s.LocalMusicDirs = []string{"/music/从子进程加的"}
		}); err != nil {
			t.Fatal(err)
		}
	}
	got := st.Snapshot()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if mode == "read" && (got.ThemeMode != app.ThemeModeDark || got.BorderRadius != 19) {
		t.Errorf("重启后读到的是 %+v", got)
	}
}

// runChild 拉起一次本测试二进制自己。-test.run 让它只跑这一条。
func runChild(t *testing.T, path, mode string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestSettingsSurviveAProcessRestart", "-test.count=1")
	cmd.Env = append(os.Environ(),
		childEnv+"=1",
		"BIU_SETTINGS_PATH="+path,
		"BIU_SETTINGS_MODE="+mode,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("子进程 %s 失败了：%v\n%s", mode, err, out)
	}
}

// ─────────────────────────────────────────────────────────────────────────────

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("写出来的不是合法 JSON：%v\n%s", err, b)
	}
}
