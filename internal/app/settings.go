// 应用设置的落盘：schema 版本 + 原子写 + 异步合并写。
//
// ## JSON 键名必须和 Biu 完全一致
//
// Biu 用 electron-store，落盘的是 `<UserData>/app-settings.json`，内容形如
// `{"appSettings": {...}}`（见 `biu/electron/store.ts:12-21`，
// `biu/src/store/settings.ts:46-57`）。键名全部来自
// `biu/shared/settings/app-settings.ts` 的 `defaultAppSettings`，结构定义在
// `biu/shared/types/app-setting.d.ts`。
//
// 我们原样沿用这些键名，**包括这一阶段还不用的那几个**（`ffmpegPath`、
// `hiddenMenuKeys`、`displayMode`……）。理由不是"预留"，是为了用户来回切换
// Biu 和 MyGo 时不丢东西：这一版读得懂、写得回，用户换个应用打开还是原样。
// 丢字段是最难查的一类数据丢失。
//
// 本文件写出的 JSON 一定包含全部键；Biu 导出的文件可能省略 `proxySettings.port`
// 这类可选键，读的时候补默认值即可——两个方向的差别只在这里。
//
// ## 为什么原子写不够（Codex M9）
//
// 临时文件 + rename 只保证「写到一半被断电」不会留下半个文件。它不管：
//
//   - **schema 版本**：文件是哪个版本写的、能不能读，这一层完全管不了。
//   - **迁移失败怎么办**：升级到一半崩溃，留下的到底是新的还是旧的？
//   - **版本回退**：用户把应用换回旧版，旧版读到新版的文件会怎样？
//   - **多文件一致性**：这是本文件之外的事（播放队列、下载任务…），要靠
//     「一次升级要么全成、要么全不成」来保证，见下面 ErrSettingsFromNewer。
//
// 所以本文件做四件事，缺一不可：schema 号、迁移链、迁移前备份、
// 遇到更新的版本就**只读不写**。
package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SchemaVersion 是本包写出的设置文件格式版本。
//
// 改设置的**结构**（加键、删键、改类型）就要涨它，并往 migrations 里加一条
// v→v+1 的路。只改取值范围、只加新的可选键，不用涨。
//
// 这不是历史包袱：自动更新会把用户从旧版直接送到新版，中间不经过用户确认，
// 迁移链是唯一的兜底。
const SchemaVersion = 1

// SettingsFileName 是设置文件名。
//
// ⚠️ 和 Biu 的 `app-settings.json` 不一样。Biu 那个名字是 electron-store 的
// `StoreNameMap.AppSettings` 直接拼出来的，没有语义。我们按 `PLAN.md` §3.4
// 用 `settings.json`——**键名一致才需要兼容，文件名不需要**。要导入 Biu 的
// 备份，把文件内容贴进来即可，不用改文件名。
const SettingsFileName = "settings.json"

// 设置文件里几个约定值。
const (
	// DefaultFontFamily 是 Biu 的默认字体。Biu 自己还留过一个
	// `"system-default"` 的错误值（src/store/settings.ts:37-39 专门兼容它），
	// 迁移里一并改掉。
	DefaultFontFamily = "system-ui"
	// DefaultPrimaryColor 是 Biu 的绿色主色（Spotify 绿）。
	DefaultPrimaryColor = "#1ed760"
	// DefaultBorderRadius 是 Biu 的默认圆角。
	DefaultBorderRadius = 8.0
	// MaxBorderRadius 是圆角的上限，也是 Biu 设置页滑块的右端点。
	MaxBorderRadius = 24.0
)

// 侧栏宽度的三个数，抄 Biu `src/layout/side/index.tsx:15-17`：
//
//	COLLAPSED_WIDTH = 72    折叠后多宽
//	MIN_WIDTH       = 160   展开时的下限，拖到它下面就折叠
//	MAX_WIDTH       = 480   展开时的上限
//
// 放在这一层而不是界面层，是因为 normalize 也要用：设置文件是用户能直接编辑的，
// 一个 `sideMenuWidth: 99999` 不该一路穿到画界面的那一行。
const (
	CollapsedSideMenuWidth = 72.0
	MinSideMenuWidth       = 160.0
	MaxSideMenuWidth       = 480.0
	DefaultSideMenuWidth   = 200.0
)

// ClampSideMenuWidth 把一个宽度拉进 [MinSideMenuWidth, MaxSideMenuWidth]。
//
// 拖动过程中**不要**用它：拖到 100 时画出来的就该是 100（折叠中的样子），
// 只有落盘的值和重读设置时才钳。Biu 也是这个分工（`computeWidth` 里的
// widthForView / widthToPersist 分开）。
func ClampSideMenuWidth(v float64) float64 {
	if v < MinSideMenuWidth {
		return MinSideMenuWidth
	}
	if v > MaxSideMenuWidth {
		return MaxSideMenuWidth
	}
	return v
}

// 主题档位。Biu 的 `ThemeMode = "system" | "light" | "dark"`。
type ThemeMode string

const (
	ThemeModeSystem ThemeMode = "system"
	ThemeModeLight  ThemeMode = "light"
	ThemeModeDark   ThemeMode = "dark"
)

// 音质档位。Biu 的 `AudioQuality`。
type AudioQuality string

const (
	AudioQualityAuto     AudioQuality = "auto"
	AudioQualityLossless AudioQuality = "lossless"
	AudioQualityHigh     AudioQuality = "high"
	AudioQualityMedium   AudioQuality = "medium"
	AudioQualityLow      AudioQuality = "low"
)

// 列表显示方式。Biu 的 `displayMode`。
type DisplayMode string

const (
	DisplayModeCard    DisplayMode = "card"
	DisplayModeList    DisplayMode = "list"
	DisplayModeCompact DisplayMode = "compact"
)

// 页面切换动画。Biu 的 `PageTransition`。
type PageTransition string

const (
	PageTransitionNone    PageTransition = "none"
	PageTransitionFade    PageTransition = "fade"
	PageTransitionSlide   PageTransition = "slide"
	PageTransitionScale   PageTransition = "scale"
	PageTransitionSlideUp PageTransition = "slideUp"
)

// 关闭窗口时怎么办。Biu 的 `closeWindowOption`。
type CloseWindowOption string

const (
	CloseWindowHide CloseWindowOption = "hide"
	CloseWindowExit CloseWindowOption = "exit"
)

// 代理协议。Biu 的 `ProxyType`。
type ProxyType string

const (
	ProxyNone   ProxyType = "none"
	ProxyHTTP   ProxyType = "http"
	ProxySOCKS4 ProxyType = "socks4"
	ProxySOCKS5 ProxyType = "socks5"
)

// ProxySettings 是代理配置。
//
// Port 用指针：Biu 的类型是 `port?: number`，默认值是 `undefined`，
// 用 int 表达不了「没设」和「设成 0」的区别。
type ProxySettings struct {
	Type     ProxyType `json:"type"`
	Host     string    `json:"host"`
	Port     *int      `json:"port"`
	Username string    `json:"username"`
	Password string    `json:"password"`
}

// SideMenuCollectionFolded 是侧栏两组收藏的折叠状态。键名照 Biu 的
// `sideMenuCollectionFolded`（`biu/shared/settings/app-settings.ts:25`）。
type SideMenuCollectionFolded struct {
	Created   bool `json:"created"`
	Collected bool `json:"collected"`
}

// SideMenuCollectionOrder 是侧栏两组收藏的**顺序**：每组一条 ID 序列。
//
// ## 这是唯一一个我们加的、Biu 没有的键
//
// Biu 把这个顺序单独存在另一个 store 里（zustand persist 的
// `"favorites-order"`，`{createdOrder, collectedOrder}`，
// `biu/src/store/favorite.ts:157-163`），不在 app-settings.json 里。
// 我们放进设置，是因为验收要求「顺序落盘」，而设置存储已经把 schema 版本、
// 原子写、退出前 flush、只读保护这几件都做好了（见 settings.go），
// 另开一份文件就等于把这些重造一遍——重造出来的那份还少了只读保护：
// 设置来自更新的版本时，这一格照样会被旧版本按旧语义改写。
//
// 分工照 Biu：**条目不存，顺序才存**。条目每次从接口拉，存一份本地副本
// 只会和接口对不上；顺序是用户拖出来的，接口不返回，只能存。
//
// 加键不用涨 SchemaVersion（见 SchemaVersion 的注释：只加新的可选键不用涨），
// 读不到就是空序列，等于「还没排过序」。
type SideMenuCollectionOrder struct {
	Created   []int64 `json:"created"`
	Collected []int64 `json:"collected"`
}

// list 报 g 这一组存下的顺序。
func (o SideMenuCollectionOrder) list(g FavoriteGroup) []int64 {
	if g == FavoritesCollected {
		return o.Collected
	}
	return o.Created
}

// setList 把 g 这一组的顺序写成 v，返回新的整份。
func (o SideMenuCollectionOrder) setList(g FavoriteGroup, v []int64) SideMenuCollectionOrder {
	if g == FavoritesCollected {
		o.Collected = v
	} else {
		o.Created = v
	}
	return o
}

// Settings 是一份完整的应用设置，键名与 Biu 一一对应。
//
// 字段顺序按 Biu `defaultAppSettings` 的书写顺序排，方便两边对着看。
type Settings struct {
	AutoStart                bool                     `json:"autoStart"`
	CloseWindowOption        CloseWindowOption        `json:"closeWindowOption"`
	FontFamily               string                   `json:"fontFamily"`
	BorderRadius             float64                  `json:"borderRadius"`
	DownloadPath             string                   `json:"downloadPath"`
	PrimaryColor             string                   `json:"primaryColor"`
	BackgroundColor          string                   `json:"backgroundColor"`
	AudioQuality             AudioQuality             `json:"audioQuality"`
	HiddenMenuKeys           []string                 `json:"hiddenMenuKeys"`
	DisplayMode              DisplayMode              `json:"displayMode"`
	FFmpegPath               string                   `json:"ffmpegPath"`
	ThemeMode                ThemeMode                `json:"themeMode"`
	PageTransition           PageTransition           `json:"pageTransition"`
	ShowSearchHistory        bool                     `json:"showSearchHistory"`
	ProxySettings            ProxySettings            `json:"proxySettings"`
	SideMenuCollapsed        bool                     `json:"sideMenuCollapsed"`
	SideMenuWidth            float64                  `json:"sideMenuWidth"`
	SideMenuCollectionFolded SideMenuCollectionFolded `json:"sideMenuCollectionFolded"`
	SideMenuCollectionOrder  SideMenuCollectionOrder  `json:"sideMenuCollectionOrder"`
	ReportPlayHistory        bool                     `json:"reportPlayHistory"`
	LocalMusicDirs           []string                 `json:"localMusicDirs"`

	// extra 是这个版本的代码不认识的键，原样带着走。
	//
	// 两个方向都要保：更新的 Biu 加了新键，或者未来的我们加了键而用户换回了
	// 旧版——直接丢弃就是数据丢失。读进来时留下，写出去时原样带上，
	// 我们认识的键以我们的值为准。
	extra map[string]json.RawMessage
}

// knownSettingsKeys 是 Settings 里我们自己认识的键，从结构体标签反射出来。
// 加字段不用改这里，也就不会出现「加了字段忘了加白名单」。
var knownSettingsKeys = func() map[string]bool {
	t := reflect.TypeOf(Settings{})
	m := make(map[string]bool, t.NumField())
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if name, _, _ := strings.Cut(f.Tag.Get("json"), ","); name != "" && name != "-" {
			m[name] = true
		}
	}
	return m
}()

// UnmarshalJSON 读一个 Settings：已知的键按结构体读，剩下的存进 extra。
func (s *Settings) UnmarshalJSON(b []byte) error {
	// plain 去掉方法的那一份类型，防止 UnmarshalJSON 自己调自己。
	type plain Settings
	var v plain
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		return err
	}
	extra := make(map[string]json.RawMessage, len(all))
	for k, raw := range all {
		if !knownSettingsKeys[k] {
			extra[k] = raw
		}
	}

	*s = Settings(v)
	s.extra = extra
	return nil
}

// MarshalJSON 写一个 Settings：自己的键 + 带回来的陌生键。
func (s Settings) MarshalJSON() ([]byte, error) {
	type plain Settings
	b, err := json.Marshal(plain(s))
	if err != nil || len(s.extra) == 0 {
		return b, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for k, raw := range s.extra {
		if _, known := m[k]; !known {
			m[k] = raw
		}
	}
	return json.Marshal(m)
}

// settingsFile 是落盘的整体。信封是 Biu 的 `{"appSettings": {...}}`，
// 外面多一个 schemaVersion。
//
// Biu 的 electron-store 还会写一个 `"__internal__"` 段（conf 的内部状态）。
// 读的时候标准库会自动忽略它，我们也不写回去。
type settingsFile struct {
	SchemaVersion int      `json:"schemaVersion"`
	AppSettings   Settings `json:"appSettings"`
}

// DefaultSettings 返回 Biu 的默认设置。downloadPath 传系统下载目录，
// 对应 Biu 的 `electron/store.ts:18`（那里注入 `app.getPath("downloads")`）。
func DefaultSettings(downloadPath string) Settings {
	return Settings{
		AutoStart:         false,
		CloseWindowOption: CloseWindowHide,
		FontFamily:        DefaultFontFamily,
		BorderRadius:      DefaultBorderRadius,
		DownloadPath:      downloadPath,
		PrimaryColor:      DefaultPrimaryColor,
		BackgroundColor:   "",
		AudioQuality:      AudioQualityAuto,
		HiddenMenuKeys:    []string{},
		DisplayMode:       DisplayModeList,
		FFmpegPath:        "",
		ThemeMode:         ThemeModeSystem,
		PageTransition:    PageTransitionNone,
		ShowSearchHistory: true,
		ProxySettings:     ProxySettings{Type: ProxyNone},
		SideMenuCollapsed: false,
		SideMenuWidth:     DefaultSideMenuWidth,
		SideMenuCollectionOrder: SideMenuCollectionOrder{
			Created:   []int64{},
			Collected: []int64{},
		},
		ReportPlayHistory: true,
		LocalMusicDirs:    []string{},
	}
}

// 设置文件的三种坏法。三种要分开，因为要分开处理。
var (
	// ErrSettingsCorrupt：文件在，但不是我们（或 Biu）写出来的 JSON。
	// 处理：备份走，重置成默认值，让用户继续用。
	ErrSettingsCorrupt = errors.New("app: 设置文件已损坏，已重置为默认值")

	// ErrSettingsFromNewer：文件来自一个 schema 更高的版本。
	// 处理：**一个字都不许写回去**（store 转成只读），否则用户的设置会被一个
	// 旧版本按旧语义改写，等升级回来已经面目全非。这是 M9 说的
	// 「旧版本回退兼容」。
	ErrSettingsFromNewer = errors.New("app: 设置文件来自更新的版本，本次只读不写")

	// ErrSettingsClosed：进程已经在收尾，不接受改动了。
	ErrSettingsClosed = errors.New("app: 设置存储已关闭")
)

// LoadSettings 读一份设置。
//
// 没有这个文件**不是错误**，返回默认值——第一次运行就是这样。
//
// 返回的 Settings 在出错时也总是可用的（要么是默认值，要么是从坏文件里
// 尽力恢复出来的），调用方只需要看 error 决定要不要告诉用户。
func LoadSettings(path string) (Settings, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultSettings(""), nil
		}
		return DefaultSettings(""), fmt.Errorf("app: 读设置 %s: %w", path, err)
	}

	var f settingsFile
	if err := json.Unmarshal(b, &f); err != nil {
		// 先把坏文件留下来再重置。没有它，用户丢了设置都不知道丢了什么，
		// 也没法把里面没坏的部分捞回来。
		backupSettings(path)
		return DefaultSettings(""), fmt.Errorf("%w: %s (%v)", ErrSettingsCorrupt, path, err)
	}

	// 文件里没有 schemaVersion 键时，解出来就是 0，也就是 v0——
	// 正好是 Biu 直接导出的那份。两边用同一个数字，不需要再猜。
	from := f.SchemaVersion
	if from > SchemaVersion {
		// 不备份、不动它。这是别人（更新的版本）写的，我们无权处置。
		return DefaultSettings(""), fmt.Errorf("%w: 文件是 v%d，我们只到 v%d（%s）",
			ErrSettingsFromNewer, from, SchemaVersion, path)
	}

	s := f.AppSettings
	if err := applyMigrations(&s, from); err != nil {
		backupSettings(path)
		return DefaultSettings(""), fmt.Errorf("app: 升级设置 %s 失败，已保留原文件: %w", path, err)
	}
	s.normalize()
	return s, nil
}

// migration 把 v 升到 v+1。
type migration func(*Settings) error

// migrations 是迁移链。key 是**升级前**的版本号。
var migrations = map[int]migration{
	// v0：Biu 直接导出的文件（没有 schemaVersion）。
	// 键名我们原样沿用，所以结构上不用搬东西，只要修掉 Biu 自己那个已知坏值。
	0: func(s *Settings) error {
		if s.FontFamily == "system-default" {
			s.FontFamily = DefaultFontFamily
		}
		return nil
	},
}

// applyMigrations 一路往上走。每一步失败就把错误交出去，绝不半途停下留个
// 「一半新一半旧」的结果。
func applyMigrations(s *Settings, from int) error {
	for v := from; v < SchemaVersion; v++ {
		m, ok := migrations[v]
		if !ok {
			return fmt.Errorf("没有从 schema v%d 升到 v%d 的路", v, v+1)
		}
		if err := m(s); err != nil {
			return fmt.Errorf("v%d → v%d: %w", v, v+1, err)
		}
	}
	return nil
}

// hexColor 是合法的颜色写法，和 MyGo 的 ui.Hex 接受的一样。
// 这里自己写一份：本包不认识界面，不能 import mygo/ui。
var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

// normalize 把明显不合理的值拉回合法区间。
//
// 这一步不是可选的洁癖：设置文件是用户能直接编辑的，也可能从 Biu、备份、
// 手改的导入里来，一个 `borderRadius: 9999` 不该一路穿到画界面的那一行。
func (s *Settings) normalize() {
	s.BorderRadius = min(max(s.BorderRadius, 0), MaxBorderRadius)
	if s.FontFamily == "" {
		s.FontFamily = DefaultFontFamily
	}
	s.SideMenuWidth = ClampSideMenuWidth(s.SideMenuWidth)

	s.ThemeMode = pickEnum(s.ThemeMode, ThemeModeSystem,
		ThemeModeSystem, ThemeModeLight, ThemeModeDark)
	s.AudioQuality = pickEnum(s.AudioQuality, AudioQualityAuto,
		AudioQualityAuto, AudioQualityLossless, AudioQualityHigh,
		AudioQualityMedium, AudioQualityLow)
	s.DisplayMode = pickEnum(s.DisplayMode, DisplayModeList,
		DisplayModeCard, DisplayModeList, DisplayModeCompact)
	s.PageTransition = pickEnum(s.PageTransition, PageTransitionNone,
		PageTransitionNone, PageTransitionFade, PageTransitionSlide,
		PageTransitionScale, PageTransitionSlideUp)
	s.CloseWindowOption = pickEnum(s.CloseWindowOption, CloseWindowHide,
		CloseWindowHide, CloseWindowExit)
	s.ProxySettings.Type = pickEnum(s.ProxySettings.Type, ProxyNone,
		ProxyNone, ProxyHTTP, ProxySOCKS4, ProxySOCKS5)

	if s.PrimaryColor == "" {
		s.PrimaryColor = DefaultPrimaryColor
	}
	if !hexColor.MatchString(s.PrimaryColor) {
		s.PrimaryColor = DefaultPrimaryColor
	}
	if s.BackgroundColor != "" && !hexColor.MatchString(s.BackgroundColor) {
		s.BackgroundColor = ""
	}

	if s.HiddenMenuKeys == nil {
		s.HiddenMenuKeys = []string{}
	}
	if s.LocalMusicDirs == nil {
		s.LocalMusicDirs = []string{}
	}
	// nil 和空序列在 JSON 里差一个 `null`，写出去会让「还没排过序」和
	// 「排过序，排出来是空的」看起来是两回事。统一成空序列。
	if s.SideMenuCollectionOrder.Created == nil {
		s.SideMenuCollectionOrder.Created = []int64{}
	}
	if s.SideMenuCollectionOrder.Collected == nil {
		s.SideMenuCollectionOrder.Collected = []int64{}
	}
}

// pickEnum 是就返回它，不认识（或者是空）就返回兜底值。
func pickEnum[T ~string](v T, def T, allowed ...T) T {
	if slices.Contains(allowed, v) {
		return v
	}
	return def
}

// backupSettings 把出问题的那份文件挪成 `<path>.bak` 留着。
//
// 用固定的后缀而不是时间戳：用户找得到、CI 断言得了，
// 而且只留最近一份——留一串历史副本对用户没有额外用处。
//
// 挪不动也不能报错——已经出过一次问题了，不能因为留个证据再报一次，
// 那会把真正的原因淹掉。
func backupSettings(path string) {
	_ = os.Rename(path, path+".bak")
}

// writeDebounce 是把连续改动合并成一次落盘的窗口。
//
// 没有它，拖圆角滑块就是每帧重写一遍整个设置文件。
const writeDebounce = 120 * time.Millisecond

// SettingsStore 是设置在内存里和磁盘上的那一份。
//
// 写盘永远是后台的：view 里改一个值就返回，不等 IO。改动攒够 writeDebounce
// 才落一次盘。
//
// 零值不可用，用 OpenSettingsStore。
type SettingsStore struct {
	path string
	// wake 在落盘完成后调。界面层装一个 Invalidate——不唤醒的话，
	// 界面永远停在「还没存」的那一帧。
	wake func()

	// wmu 只护着真正的写文件动作。写要串行：两次 writeSettingsAtomic
	// 撞在一起，后 rename 的可能先落地。
	wmu sync.Mutex

	mu       sync.Mutex
	cur      Settings
	dirty    bool
	readOnly bool
	closed   bool

	// lastErr 存最近一次落盘失败。后台循环和同步 Flush 都写它，LastError 读它，
	// 所以用 atomic.Pointer 而不是那把 mu——不能为了读一个错就把写盘堵住。
	lastErr atomic.Pointer[error]

	req     chan struct{} // 有改动要落盘，带缓冲，满了就丢弃（旧的请求没区别）
	quit    chan struct{}
	stopped chan struct{}
}

// OpenSettingsStore 打开（或建）一份设置。
//
// **要在开窗口之前调**，或者在一个 goroutine 里调完再要一帧：读盘是阻塞 IO。
//
// 返回的 store 一定可用。err 非 nil 时 store 仍然工作——
// ErrSettingsCorrupt 是「你的设置没了，已重置」，ErrSettingsFromNewer 是
// 「这份文件是新版写的，我不碰它」，这两种都要让用户看得见，但都不该
// 让程序起不来。
func OpenSettingsStore(path string) (*SettingsStore, error) {
	s, err := LoadSettings(path)
	return newSettingsStore(path, s, errors.Is(err, ErrSettingsFromNewer)), err
}

func newSettingsStore(path string, s Settings, readOnly bool) *SettingsStore {
	st := &SettingsStore{
		path:     path,
		cur:      s,
		readOnly: readOnly,
		req:      make(chan struct{}, 1),
		quit:     make(chan struct{}),
		stopped:  make(chan struct{}),
	}
	go st.loop()
	return st
}

// Path 是设置文件在哪。诊断信息、导入导出要用。
func (s *SettingsStore) Path() string { return s.path }

// SetWake 装「落盘完成了」的回调，由界面层装一次。
//
// ⚠️ 会从后台 goroutine 调，必须能跨 goroutine 调用（`ui.Services.Invalidate`
// 就是）。合并安全也必须有——一次落盘只调一次，但还是按合并安全的对待。
func (s *SettingsStore) SetWake(fn func()) { s.wake = fn }

// ReadOnly 报这个 store 是不是只读的（设置文件来自更新的版本）。
func (s *SettingsStore) ReadOnly() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readOnly
}

// Snapshot 返回当前设置的一份拷贝。
//
// 调用者拿到的只是值拷贝；里面的两个切片和 extra 映射是与 store 共享的，
// 别去改。要改走 Update。
func (s *SettingsStore) Snapshot() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

// Update 改设置，改完立刻返回，**不等落盘**。
//
// fn 拿到的是当前值的拷贝，改它就行。返回值是改完之后的完整设置，
// 界面可以直接拿去画下一帧。
func (s *SettingsStore) Update(fn func(*Settings)) (Settings, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Settings{}, ErrSettingsClosed
	}
	if s.readOnly {
		cur := s.cur
		s.mu.Unlock()
		return cur, ErrSettingsFromNewer
	}
	fn(&s.cur)
	s.cur.normalize()
	s.dirty = true
	cur := s.cur
	s.mu.Unlock()

	s.signal()
	return cur, nil
}

// signal 告诉后台去落盘。缓冲区满了就说明已经有一个请求在排队了，
// 内容是「最新的设置」而不是「一次改动」，所以丢一个没有影响。
func (s *SettingsStore) signal() {
	select {
	case s.req <- struct{}{}:
	default:
	}
}

// Flush 立刻落盘并等它完成。测试、导出、以及不想等退避窗口的地方用。
func (s *SettingsStore) Flush() error {
	s.wmu.Lock()
	defer s.wmu.Unlock()

	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return nil
	}
	b, err := json.MarshalIndent(settingsFile{
		SchemaVersion: SchemaVersion,
		AppSettings:   s.cur,
	}, "", "  ")
	// 先清 dirty 再写：写失败要放回去（下面），写成功就不用再放。
	s.dirty = false
	s.mu.Unlock()

	if err == nil {
		err = writeSettingsAtomic(s.path, b)
	}
	if err != nil {
		// 放回去，否则这次改动就永远丢了——内存里有、磁盘上没有，
		// 关掉就没了，而且没有第二次机会。
		s.mu.Lock()
		s.dirty = true
		s.mu.Unlock()
		err = fmt.Errorf("app: 落盘设置 %s: %w", s.path, err)
		s.lastErr.Store(&err)
		return err
	}
	s.lastErr.Store(nil)
	s.wakeUp()
	return nil
}

// Close 收尾：停掉后台写、把还欠着的改动落完。可以重复调。
//
// ⚠️ 「改设置 → 杀进程 → 重启 → 设置还在」这条验收靠的就是这里。
// SIGKILL 拦不住，任何异步写盘都得配一个同步收尾才敢说这句话。
func (s *SettingsStore) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	close(s.quit)
	<-s.stopped
	return s.Flush()
}

func (s *SettingsStore) wakeUp() {
	if s.wake != nil {
		s.wake()
	}
}

// loop 是后台落盘的循环：收到请求后等一个 writeDebounce，把这段时间里的
// 连续改动一起写掉，然后睡到下一次请求。
func (s *SettingsStore) loop() {
	defer close(s.stopped)

	var timer *time.Timer
	for {
		var tick <-chan time.Time
		if timer != nil {
			tick = timer.C
		}
		select {
		case <-s.quit:
			if timer != nil {
				timer.Stop()
			}
			return
		case <-s.req:
			if timer == nil {
				timer = time.NewTimer(writeDebounce)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(writeDebounce)
			}
		case <-tick:
			timer = nil
			// 落盘失败不重试：磁盘满、目录没了这类问题重试也是白转，
			// 而且会把错误刷屏。下一次 Update 会自动再试一次。
			// Flush 自己记 LastError 并把 dirty 放回去。
			_ = s.Flush()
		}
	}
}

// LastError 是最近一次落盘失败的错。之后成功一次就清掉。
func (s *SettingsStore) LastError() error {
	if p := s.lastErr.Load(); p != nil {
		return *p
	}
	return nil
}

// writeSettingsAtomic 原子写：临时文件 → fsync → rename。
//
// 任何一步失败都把临时文件删掉，不留垃圾。
func writeSettingsAtomic(path string, b []byte) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".settings-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(name)
		}
	}()

	if _, err = tmp.Write(b); err != nil {
		return err
	}
	// fsync 的是数据。数据落盘了但目录项没有的话，断电之后可能还是旧文件名。
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	// CreateTemp 建出来的是 0600。设置文件本身不是秘密，但里面有下载目录、
	// 代理地址这些隐私，给成普通用户配置同样的权限。
	if err = os.Chmod(name, 0o644); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	return syncDir(dir)
}

// syncDir 把目录项本身刷到盘上。
//
// Windows 上打不开目录句柄，直接跳过——那边 rename 本身就已经是原子的。
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
