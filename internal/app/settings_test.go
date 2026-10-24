package app_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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

// ourOwnKeys 是**我们加的、Biu 没有的键**。
//
// 加它们是有理由的：Biu 把这两样东西各自单独放在另一个 store 里
// （zustand persist 的 `"favorites-order"`，`biu/src/store/favorite.ts:157-163`；
// 以及 `play-list` store 自己的 partialize，`biu/src/store/play-list.ts:978`），
// **不在 app-settings.json 里**。我们不另开一份文件，是因为设置存储已经把
// schema 版本、原子写、退出前 flush、只读保护做好了，另开一份等于把这些
// 重造一遍，还少了只读保护。
//
// 代价说清楚：多这几个键，Biu 那边读到我们的文件会把它们当不认识的键。
// 会不会把它们弄丢取决于 electron-store 的合并行为——最坏的情况也只是
// **收藏夹的顺序弹回 Biu 给的那个顺序**、以及**播放模式回到 Biu 那边记的
// 那一档**，Biu 自己的设置一个键都没丢。
// 这条取舍记在这里，想改主意时先看这一段。
//
// ⚠️ **音量和静音（`volume` / `isMuted`）也在这一栏里**，理由同上：它们是 Biu
// 那个 `play-list` store 里的字段（`biu/src/store/play-list.ts:70,369`），
// **不在 app-settings.json 里**。而我们**必须**落盘它——不落盘的话，用户为了
// 不吵把音量调到 20%，重启之后又是 100%（耳机上那是真的惊吓）。
// 键名照 Biu 的字段名逐字抄（`volume` / `isMuted`）。
//
// ⚠️ **「上次听到哪儿」（`playback`）也在这一栏里**，理由同上（它在 Biu 那边是
// `play-list` store 的 `list` / `playId` 和 `play-progress` 的
// `localStorage["play-current-time"]`）。而它**尤其**该跟着设置走：它和音量、
// 播放模式在**同一次原子写**里落盘，三者之间才没有「谁先谁后」的竞态
// （见 `internal/app/playback_state.go` 文件头那张表）。键名是我们自己的。
var ourOwnKeys = []string{
	"sideMenuCollectionOrder",
	"playMode",
	"shouldKeepPagesOrderInRandomPlayMode",
	"volume",
	"isMuted",
	"playback",
	"themePreset",
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

// ─────────────────────────────────────────────────────────────────────────────
// Biu 那两种文件，两种都得读得进来
// ─────────────────────────────────────────────────────────────────────────────

// biuAppSettings 是 Biu 一份设置的全部 20 个键。
//
// 值**全部用非默认值**，这是故意的：任何一项没被带过去，断言都会炸出来，而不是
// 被默认值悄悄填上、看起来一切正常。默认值的设置文件读错了也是「全对」——
// 那才是这类 bug 最难查的原因。
//
// 键名和取值照抄 `biu/shared/settings/app-settings.ts` 的 `defaultAppSettings`，
// 结构见 `biu/shared/types/app-setting.d.ts`。
const biuAppSettings = `{
  "autoStart": true,
  "closeWindowOption": "exit",
  "fontFamily": "PingFang SC",
  "borderRadius": 16,
  "downloadPath": "/Users/x/Downloads",
  "primaryColor": "#1ed760",
  "backgroundColor": "#101014",
  "audioQuality": "lossless",
  "hiddenMenuKeys": ["首页", "动态"],
  "displayMode": "compact",
  "ffmpegPath": "/opt/ffmpeg",
  "themeMode": "dark",
  "pageTransition": "fade",
  "showSearchHistory": false,
  "proxySettings": {"type": "socks5", "host": "127.0.0.1", "port": 7890, "username": "u", "password": "p"},
  "sideMenuCollapsed": true,
  "sideMenuWidth": 240,
  "sideMenuCollectionFolded": {"created": true, "collected": false},
  "reportPlayHistory": false,
  "localMusicDirs": ["/music/a", "/music/b"]
}`

// Biu 的两种文件形状。
//
// ⚠️ 这两种**都是用户真的会拿到的文件**，必须都能直接读：
//
//   - 带信封的：electron-store 自己落在 `~/Library/Application Support/Biu/
//     app-settings.json` 的那份（`biu/electron/store.ts:12-21`）。里面还会
//     多一段 `__internal__`（conf 的内部状态）。
//   - 裸的：设置页上「导出配置」按钮**下载给用户**的那份，文件名
//     `biu-settings-<时间戳>.json`。`biu/src/pages/settings/export-import.tsx:19`
//     写的是 `JSON.stringify(settingStore?.appSettings ?? defaultAppSettings, null, 2)`
//     ——**`appSettings` 的值本身，外面没有那层信封**。
//
// 用户要导入配置时手里拿的是第二种。第一种躺在 Biu 的数据目录里，一般人根本
// 不知道它在哪。所以认不出裸的那份不是「少支持一种格式」，是导入整个不工作。
var biuFiles = map[string]string{
	"「导出配置」下载的（裸的，没有信封）": biuAppSettings,
	"electron-store 落盘的（带信封）": `{"appSettings": ` + biuAppSettings +
		`, "__internal__": {"migrations": {"version": 1}}}`,
}

// TestABiuFilesAreReadable 是「Biu 的文件读进来还是原样」。
//
// 这一条盯的是一个**静默**的失败：认不出形状时不会报错，整份文件被当成一个
// 键都不认识的 JSON，退回全默认值——用户只会觉得「导入了但什么都没变」。
// 所以这里逐键比，值也一样：fixture 里全是非默认值，读成默认值就等于全丢。
func TestABiuFilesAreReadable(t *testing.T) {
	for name, body := range biuFiles {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), app.SettingsFileName)
			write(t, path, body)

			got, err := app.LoadSettings(path)
			if err != nil {
				t.Fatalf("Biu 的文件读不出来：%v", err)
			}
			// 读出来的那份要原样等于 Biu 写下去的那 20 个键。
			// normalize 不动这里的任何一个值（每项都在合法区间内），所以
			// 这一比就是在验「有没有被丢掉」。比的是 `biuAppSettings` 而不是
			// 文件正文：带信封的那种文件正文外面还包着 `appSettings` 和
			// `__internal__` 两个键，那两个不属于「设置」。
			// （`wantSettings` 另外加上了 ourOwnKeys 里我们自己的那一个键，
			// 理由见那个函数。）
			assertSameKeys(t, wantSettings(t), decodeObject(t, mustMarshal(t, got)), "读进来之后")
		})
	}
}

// TestABiuFilesSurviveARoundTripUnchanged 是「键名与 Biu 完全一致（**双向无
// 差集**）」：Biu 的文件读一遍、我方改一笔、写回去，逐键双向比对。
//
// `TestSettingsKeysMatchBiu` 只比了**我们写出去的**键名；少的那一半是**读进来
// 的**：一份完整的 Biu 文件，经过我们读一遍、写一遍之后，是不是还和原来一样。
// 少一个键 = 用户从 Biu 切过来丢一项设置，而丢的那一项在界面上没有任何提示。
func TestABiuFilesSurviveARoundTripUnchanged(t *testing.T) {
	for name, body := range biuFiles {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), app.SettingsFileName)
			write(t, path, body)

			st, err := app.OpenSettingsStore(path)
			if err != nil {
				t.Fatalf("Biu 的文件读不出来：%v", err)
			}
			// 改一笔再写回去：写出去的是「我们改过之后的」那份。只读不写的
			// 路径这条就验不到任何东西。
			if _, err := st.Update(func(s *app.Settings) { s.BorderRadius = 20 }); err != nil {
				t.Fatal(err)
			}
			if err := st.Close(); err != nil {
				t.Fatal(err)
			}

			var file struct {
				AppSettings map[string]any `json:"appSettings"`
			}
			readJSON(t, path, &file)

			// 两个方向都要查：少了的是丢设置，多了的是我们自造了没人认的键。
			want := wantSettings(t)
			got := file.AppSettings
			for k := range want {
				if _, ok := got[k]; !ok {
					t.Errorf("来回一趟之后丢了 Biu 的键 %q", k)
				}
			}
			for k := range got {
				if _, ok := want[k]; !ok {
					t.Errorf("来回一趟之后多了一个 Biu 没有的键 %q", k)
				}
			}
			// 值也要一样。Biu 的键名和取值都原样带走，用户换回 Biu 打开还是原样。
			for k, wantVal := range want {
				gotVal, ok := got[k]
				if !ok {
					continue // 上面已经报过了。
				}
				if k == "borderRadius" {
					// 这一条是我们**故意**改的（上面 Update 改的就是它）。
					if gotVal != float64(20) {
						t.Errorf("borderRadius 改成了 %v，want 20", gotVal)
					}
					continue
				}
				if !reflect.DeepEqual(gotVal, wantVal) {
					t.Errorf("%s：来回一趟之后是 %#v，Biu 那边是 %#v", k, gotVal, wantVal)
				}
			}
		})
	}
}

// TestABiuExportWithTheBadFontValueStillGetsFixed 验裸的那份也会走 v0 迁移。
//
// Biu 自己留过一个 `"system-default"` 的错误默认值，读的时候专门兼容它
// （`biu/src/store/settings.ts:37-39`）。**旧版本导出的文件里就是会有这个值**，
// 而导出写的就是 appSettings 本身——所以走「导入」这条路时，那份文件是没有
// schemaVersion 的裸 JSON，和「Biu 直接导出的」是同一件事，必须照样修掉。
func TestABiuExportWithTheBadFontValueStillGetsFixed(t *testing.T) {
	body := strings.Replace(biuAppSettings, `"PingFang SC"`, `"system-default"`, 1)
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	write(t, path, body)

	s, err := app.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.FontFamily != app.DefaultFontFamily {
		t.Errorf("裸的那份没走 v0 迁移，fontFamily 还是 %q", s.FontFamily)
	}
}

// TestAFileThatIsNotASettingsObjectIsCorrupt 验「既不是信封也不是设置」的文件
// 会被当成坏文件：留证据、重置、还能继续用。
//
// 认形状的判据是顶层有没有 `appSettings`。这里守的是判据的另一头：顶层是个
// **数组**或字符串时，不能因为「顶层没有 appSettings 键」就把它当成裸设置读
// 进来——那会读出一个全零的设置，然后 normalize 出一份默认值，同样是静默的。
func TestAFileThatIsNotASettingsObjectIsCorrupt(t *testing.T) {
	for _, body := range []string{`[1, 2, 3]`, `"设置"`, `42`} {
		path := filepath.Join(t.TempDir(), app.SettingsFileName)
		write(t, path, body)

		if _, err := app.LoadSettings(path); !errors.Is(err, app.ErrSettingsCorrupt) {
			t.Errorf("%s 没有被当成坏文件，err 是 %v", body, err)
		}
		if _, err := os.Stat(path + ".bak"); err != nil {
			t.Errorf("%s 没有被备份下来：%v", body, err)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────

// decodeObject 把一段 JSON 对象解成 map，解不了就说明 fixture 写错了。
func decodeObject(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("fixture 不是合法的 JSON 对象：%v\n%s", err, body)
	}
	return m
}

// mustMarshal 是给测试用的 json.Marshal：序列化一份设置失败是不能继续的。
func mustMarshal(t *testing.T, s app.Settings) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ourOwnKeysJSON 是 ourOwnKeys 里那几个键在「Biu 的文件里没有它们」时的样子
// ——读进来是零值（`playMode` 是空串，normalize 把它拉成 loop；另一个布尔是
// false，而 false 就是它的零值），读完之后写回去还是这个。
//
// 写成 JSON 而不是 Go 值，是为了让 `wantSettings` 能和 `ourOwnKeys` 交叉核对：
// 谁改了一边忘了另一边，那条核对会直接报出来。
const ourOwnKeysJSON = `{
	"sideMenuCollectionOrder": {"created": [], "collected": []},
	"playMode": "loop",
	"shouldKeepPagesOrderInRandomPlayMode": false,
	"volume": 1,
	"isMuted": false,
	"playback": {"items": [], "current": "", "positionMs": 0},
	"themePreset": "warm-paper"
}`

// wantSettings 是「读完一份 Biu 的文件之后应该拿到的那份设置」。
//
// Biu 的 20 个键（biuAppSettings，一个不多一个不少），**加上** ourOwnKeys 里
// 我们自己的那三个（见它在上面那段说明：收藏夹的顺序和播放模式，Biu 放在另外
// 的 store 里，我们放进设置是为了复用 schema 版本 / 原子写 / 退出前 flush /
// 只读保护）。
//
// 之所以要在每条「Biu 的文件」测试里都加上它们：Biu 的文件里当然没有这几个键，
// 读进来是零值、normalize 拉成默认值，写出去还是那个默认值。**它们本来就该
// 在**——真正要盯的是「Biu 的那 20 个键有没有原样带过去」，以及「有没有多出
// 别的键来」。
func wantSettings(t *testing.T) map[string]any {
	t.Helper()
	m := decodeObject(t, biuAppSettings)
	extras := decodeObject(t, ourOwnKeysJSON)
	if len(extras) != len(ourOwnKeys) {
		t.Fatalf("ourOwnKeys 有 %d 个，ourOwnKeysJSON 有 %d 个——加键/删键时两边都要改",
			len(ourOwnKeys), len(extras))
	}
	for _, k := range ourOwnKeys {
		v, ok := extras[k]
		if !ok {
			t.Fatalf("ourOwnKeys 里的 %q 在 ourOwnKeysJSON 里没有——加键时两边都要改", k)
		}
		if _, dup := m[k]; dup {
			t.Fatalf("%q Biu 也有，不该列在 ourOwnKeys 里", k)
		}
		m[k] = v
	}
	return m
}

// assertSameKeys 断言两份 JSON 的键**双向无差集**，值也一样。
//
// 键要多一个少一个都报（少 = 丢设置，多 = 自造了没人认的键），值不一样也报。
// `what` 只是让失败信息说清是哪一步坏的。
func assertSameKeys(t *testing.T, want, got map[string]any, what string) {
	t.Helper()
	for k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("%s：丢了键 %q", what, k)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("%s：多了一个键 %q", what, k)
		}
	}
	for k, wantVal := range want {
		if gotVal, ok := got[k]; ok && !reflect.DeepEqual(gotVal, wantVal) {
			t.Errorf("%s：%s 是 %#v，want %#v", what, k, gotVal, wantVal)
		}
	}
}
