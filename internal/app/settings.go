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
// ## Biu 有**两种**文件，两种都得能读
//
// 「Biu 的设置文件」不是一个形状，是两个：
//
//   - `app-settings.json`（electron-store 自己落的盘）：外面包一层信封
//     `{"appSettings": {...}, "__internal__": {...}}`。
//   - `biu-settings-<时间戳>.json`（设置页上「导出配置」按钮下载下来的）：
//     **裸的 `{...}`，外面没有信封**——`export-import.tsx:19` 写的是
//     `JSON.stringify(settingStore?.appSettings ?? defaultAppSettings, null, 2)`，
//     写的是 `appSettings` 的值本身。Biu 自己的「导入配置」要的也是这个形状
//     （同文件 :46-51，逐个 `defaultAppSettings` 的键去取）。
//
// 用户真正会拿到手上、真正会拿来「导入」的是**第二种**——第一种躺在
// `~/Library/Application Support/Biu/` 里，用户一般不知道它在哪。所以认不出
// 裸的那份不是「少支持一种格式」，是**导入功能整个不工作**：整份文件被当成没有
// 任何已知键，退回全默认值，**一声不响**，用户只会觉得「导入了但什么都没变」。
//
// 判据见 `splitEnvelope`。
//
// 本文件写出的 JSON 一定包含全部键；Biu 的文件可能省略 `proxySettings.port`
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
// 用 `settings.json`——**键名一致才需要兼容，文件名不需要**。
//
// 导入 Biu 的配置就是把它的文件放到设置文件该在的位置上，**内容不用改**：
// Biu 的两种形状（electron-store 的 `app-settings.json`、以及设置页「导出
// 配置」下载的 `biu-settings-*.json`）都能直接读，见文件头的说明和
// `splitEnvelope`。
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
	// DefaultVolume 是「用户没设过音量」时给的默认音量（1 = 100%）。
	//
	// ⚠️ **它是 1 而不是 Biu 的 0.5**：Biu 那边 0.5 是 `play-list` store 的初值，
	// 而我们**老版本每一次启动都是 100%**（音量压根没落盘）——老用户升级上来
	// 看到的还是他一直习惯的那一档，不该在升级的那一次突然变成一半。
	//
	// 上游那 0.5 的出处见报告 §4.4（`biu/src/store/play-list.ts:369`）。
	DefaultVolume = 1.0
	// DefaultUnmuteVolume 是「压根没有音量可放回去」时取消静音放回去的值。
	//
	// 0.5 是一半——用户按了「取消静音」，听到的应该是一半声音，不是 0。
	DefaultUnmuteVolume = 0.5
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
	// 设计规范 v0.1 §4：展开 220（可拖 180 到 300），折叠 64 仅图标。上游 Biu 是 72 / 160~480 / 200。
	CollapsedSideMenuWidth = 64.0
	MinSideMenuWidth       = 180.0
	MaxSideMenuWidth       = 300.0
	DefaultSideMenuWidth   = 220.0
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

// ThemePreset 是主题方案：设计规范里的三套整体配色（暖纸 / 暮光 / 柔雾），
// 每套含浅色和深色各一份底色、文字色、强调色和圆角。
//
// ## 我们加的、Biu 没有的键
//
// Biu 只有 `themeMode`（浅 / 深 / 跟随系统）加一个主色一个底色，**没有「整套方案」
// 这个概念**，所以 `themePreset` 是我们自己的键（见 `ourOwnKeys`）。Biu 读到
// 我们的文件会把它当不认识的键，Biu 自己的设置一个键都没丢。
//
// ## 默认是「暖纸」，「经典」是显式的一档
//
// 「经典」就是原来的外观（Biu 绿加框架默认底色），不套任何方案，值是 `"classic"`。
// 默认（包括键缺失、空串、不认识的值）一律是暖纸：应用还没发版，没有需要保住的
// 旧外观；Biu 导出的配置里没有这个键，导入后也拿到暖纸。
// 套了方案之后，方案的底色、文字、强调色、圆角**压过**设置里的 `primaryColor` /
// `backgroundColor` / `borderRadius`；用户在设置页改那三项时界面层会先把方案清成
// 经典，免得「改了没反应」。
type ThemePreset string

const (
	ThemePresetWarmPaper ThemePreset = "warm-paper"
	ThemePresetDuskAmber ThemePreset = "dusk-amber"
	ThemePresetSoftPeach ThemePreset = "soft-peach"
	ThemePresetClassic   ThemePreset = "classic"
)

// DefaultThemePreset 是新设置的默认方案。
//
// 它是**生产默认**：应用还没发版，没有需要保住的旧外观，所以默认一律是暖纸
// （缺键、空串、不认识的值都落到这里，见 `pickEnum`）。
//
// ⚠️ **这是一个变量而不是常量，是为了留一扇门**：老界面那一批「和 Biu 逐像素
// 对照」的用例曾经在这里被按成经典，好让它们继续绿。票 #406 把那一层去掉
// （`internal/ui` 的 `TestMain` 不再改它），界面那一包的测试现在量的是**暖纸**
// ——那才是用户看见的那一版。
//
// 「经典」这个**用户选项**照旧保留（设置页四张预览卡里有它），它自己那套配色有
// 专门的用例盯着（`internal/ui/themepreset_test.go` 及那几处按方案各跑一遍的
// 截图用例）。而「生产默认是暖纸」由本包里的测试盯着（`themepreset_test.go`）。
var DefaultThemePreset = ThemePresetWarmPaper

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

// 代理协议。Biu 的 `ProxyType` 是 `"none" | "http" | "socks4" | "socks5"`，
// **没有 HTTPS**（`biu/src/pages/settings/proxy-settings.tsx:10-15` 的
// `PROXY_TYPE_OPTIONS`）。
//
// 🔴 这里比 Biu 多一个 `https`。加它破的不是「键名与 Biu 一致」那条（那条讲的是
// `app-settings.json` 里的**键**，一个都没动），而是多了一个 **值**：
//
//   - Biu 读到 `{"proxySettings":{"type":"https"}}` 时，TypeScript 的那个联合类型
//     在运行时拦不住它，值原样留在 store 里——**Biu 那边一个字节都不丢**。
//   - 我们这边它是合法值，`normalize()` 认它、拨号认它。
//
// 为什么必须有：任务与 `SPEC.md` 的网络一节明写「代理支持 HTTP / HTTPS / SOCKS5 /
// SOCKS4 四种协议」。`SPEC.md` 那行字照的是 Biu 的四个协议名里的前两个，但**以任务
// 明文为准**。HTTPS 走的也是 HTTP 那一套拨号（Go 的 `http.Transport` 对
// `http://` 和 `https://` 的代理是同一条路），所以多一档不需要新的实现，只是把
// 用户要的那一档摆出来。
type ProxyType string

const (
	ProxyNone   ProxyType = "none"
	ProxyHTTP   ProxyType = "http"
	ProxyHTTPS  ProxyType = "https"
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
// ## 我们加的、Biu 没有的键（两个地方，原因一样）
//
// `SideMenuCollectionOrder` 和 `Settings.PlayMode` /
// `Settings.ShouldKeepPagesOrderInRandomPlayMode` 在 Biu 那边**都不在
// app-settings.json 里**：前者是 zustand persist 的 `"favorites-order"`
// （`{createdOrder, collectedOrder}`，`biu/src/store/favorite.ts:157-163`），
// 后两者是 `play-list` store 里的字段（`biu/src/store/play-list.ts:74,86`，
// 靠它自己的 partialize 落盘）。
//
// 键名照抄、位置换了，理由和 Biu 把它们分开存一样：它们是**队列/侧栏的状态**，
// 不是应用的配置。但我们仍然放进设置，因为设置存储已经把 schema 版本、
// 原子写、退出前 flush、只读保护这几件都做好了（见 settings.go），
// 另开一份文件就等于把这些重造一遍——重造出来的那份还少了只读保护：
// 设置来自更新的版本时，这一格照样会被旧版本按旧语义改写。
//
// 「顺序」这一格还多一条理由：验收要求「顺序落盘」，而设置是唯一一个已经
// 做对了写入那一半的地方。
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
	ThemePreset              ThemePreset              `json:"themePreset"`
	PageTransition           PageTransition           `json:"pageTransition"`
	ShowSearchHistory        bool                     `json:"showSearchHistory"`
	ProxySettings            ProxySettings            `json:"proxySettings"`
	SideMenuCollapsed        bool                     `json:"sideMenuCollapsed"`
	SideMenuWidth            float64                  `json:"sideMenuWidth"`
	SideMenuCollectionFolded SideMenuCollectionFolded `json:"sideMenuCollectionFolded"`
	SideMenuCollectionOrder  SideMenuCollectionOrder  `json:"sideMenuCollectionOrder"`
	ReportPlayHistory        bool                     `json:"reportPlayHistory"`
	LocalMusicDirs           []string                 `json:"localMusicDirs"`

	// PlayMode 是播放模式（顺序 / 循环 / 随机 / 单曲）。键名照 Biu 的
	// `play-list.ts:74` 那个 zustand 字段，见本类型上面「我们加的键」那一段。
	PlayMode PlayMode `json:"playMode"`
	// ShouldKeepPagesOrderInRandomPlayMode 是「随机时保持分集顺序」。键名照 Biu
	// 的 `play-list.ts:86`，同一段理由。
	//
	// ⚠️ **默认值是 true（照 Biu 的 `play-list.ts:373`），而它的零值是 false，
	// 所以「这份设置里没有这一格」和「用户把它关了」在读回来之后分不开。**
	// 于是：新装的是开的（走 DefaultSettings），从一份旧设置升上来的是关的。
	//
	// 这不是这一格独有的：`ShowSearchHistory` 和 `ReportPlayHistory` 的默认值
	// 也是 true，缺了它们同样是关的（见 DefaultSettings）。Go 的 bool 表达不了
	// 「没设过」，要修就得给这几格一起换成 `*bool` 或者涨一次 SchemaVersion 再
	// 迁移——那是另一票的事，不该混在「加四档播放模式」里顺手做掉。
	//
	// 今天没人在意：这个开关只在**队列里已经有分集**的时候才改变任何事，而
	// 队列里现在**没有**分集——分集抽屉（BMG-47 的 A12）是换集时**就地改队列里
	// 那一项的 Cid**，不插新的项（见 `App.PlayPart` 与 `Item.PageIndex` 那段
	// 注释）。要让这个开关真的有话可说，得让每一集各占一项，那是另一件事。
	ShouldKeepPagesOrderInRandomPlayMode bool `json:"shouldKeepPagesOrderInRandomPlayMode"`

	// Volume 是用户设的音量（0..1），1 是解码出来的原样。
	//
	// ⚠️ **用指针**：「这份设置里没有这一格」（老版本写的文件）和「用户把音量
	// 拉到 0」必须分得开——前者该给默认值（100%），后者该给 0（真的没声）。
	// 用 float64 的话两者读出来都是 0，而「启动时突然没声」和「用户自己拉到 0」
	// 是两回事：前者是 bug，后者是用户要的。
	//
	// （和 `ProxySettings.Port` 同一个理由——那份也是 Biu 那边用 `?:` 表达的
	// 「没设过」。）
	//
	// 键名照 Biu 的 `play-list` 那个 zustand 字段（`volume`），位置换了——
	// 和 `PlayMode` / `ShouldKeepPagesOrderInRandomPlayMode` 同一段理由。
	Volume *float64 `json:"volume"`

	// Muted 是静音标志。**它和音量分开存**（票 #311 / 报告 §3.3 的 B2）。
	//
	// ⚠️ **别再用「音量 0」顶替静音**：落盘之后那就是个坑——静音之后退出再
	// 启动，音量变成 0（Clementine 的老毛病）。静音是它自己的一格，而音量那一
	// 格记的是「用户设的是多少」。
	//
	// 键名照 Biu 的 `play-list` 那个 zustand 字段（`isMuted`）。
	Muted bool `json:"isMuted"`

	// Playback 是「上次听到哪儿」那一格：队列、当前项、播放位置。
	//
	// ⚠️ **它和音量、播放模式在同一个文件里，是有意的**：同一次原子写里落盘，
	// 它们之间就没有「谁先谁后」的竞态（见 playback_state.go 文件头那张表）。
	//
	// 键名是我们自己的（Biu 那边没有这一个键，它把队列放在 `play-list` store
	// 里）。Biu 读到它会当不认识的键——最坏的结果是**它自己的队列不受影响**
	//（那是另一个文件里的事）。
	//
	// 加键不用涨 SchemaVersion（见 SchemaVersion 的注释：只加新的可选键不用
	// 涨）：读不到就是空的，等于「没有上次听到哪儿」。
	Playback PlaybackSnapshot `json:"playback"`

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

// DefaultSettings 返回 Biu 的默认设置。downloadPath 传默认下载目录，
// 对应 Biu 的 `electron/store.ts:18`（那里注入 `app.getPath("downloads")`）。
//
// ⚠️ **我们这边传的是空串**：默认落点由 `platform.DefaultDownloadsDir` 算出来
// （「音乐」目录下的应用子目录），不是系统下载目录——见
// `platform/downloadsdir.go` 顶上那一节。
func DefaultSettings(downloadPath string) Settings {
	// 音量那一格是**指针**，所以每一次调用都要新的一份——共用同一个指针的话，
	// 有人改了 `*s.Volume` 就把全局的默认值改了。
	vol := DefaultVolume
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
		ThemePreset:       DefaultThemePreset,
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

		// 音量：默认 100%（老版本每一次启动都是这一档，见 DefaultVolume）。
		Volume: &vol,
		Muted:  false,

		// 「上次听到哪儿」那一格：默认是空的（= 没有上次听到哪儿）。
		//
		// ⚠️ **`Items` 给空序列而不是 nil**：nil 和空序列在 JSON 里差一个
		// `null`，写出去会让「还没存过」和「存过、当时队列是空的」看起来是两
		// 回事。统一成空序列。
		Playback: PlaybackSnapshot{Items: []PlaybackEntry{}},

		// 播放模式的两个默认值都照 Biu 的初始状态（`play-list.ts:370,373`）：
		// 默认循环播放（不是顺序播放——「放完就停住」正是这一票要治的症状，
		// 见 PlayMode 的注释），随机时保持分集顺序默认开着。
		PlayMode:                             DefaultPlayMode,
		ShouldKeepPagesOrderInRandomPlayMode: true,
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
// 读得懂三种形状（自己写的、Biu 的 electron-store 落盘文件、Biu「导出配置」
// 下载的裸文件），见文件头和 `splitEnvelope`。
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

	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		// 先把坏文件留下来再重置。没有它，用户丢了设置都不知道丢了什么，
		// 也没法把里面没坏的部分捞回来。
		backupSettings(path)
		return DefaultSettings(""), fmt.Errorf("%w: %s (%v)", ErrSettingsCorrupt, path, err)
	}

	s, from, err := splitEnvelope(b, top)
	if err != nil {
		backupSettings(path)
		return DefaultSettings(""), fmt.Errorf("%w: %s (%v)", ErrSettingsCorrupt, path, err)
	}

	if from > SchemaVersion {
		// 不备份、不动它。这是别人（更新的版本）写的，我们无权处置。
		return DefaultSettings(""), fmt.Errorf("%w: 文件是 v%d，我们只到 v%d（%s）",
			ErrSettingsFromNewer, from, SchemaVersion, path)
	}

	if err := applyMigrations(&s, from); err != nil {
		backupSettings(path)
		return DefaultSettings(""), fmt.Errorf("app: 升级设置 %s 失败，已保留原文件: %w", path, err)
	}
	s.normalize()
	return s, nil
}

// parseSettingsFile 从一段字节里认出一份设置和它是第几版。
//
// 它和 `LoadSettings` 的区别只有一处：**不碰磁盘**。导入（`biuimport.go` 那边）
// 拿到的是 Biu 的一份 JSON 字节、要认的形状和设置文件完全一样（两种，见文件头），
// 让它先落盘再走 `LoadSettings` 是「为了读一份文件而写一份文件」——会覆盖用户
// 现在的设置，而且报出来的错会指向一个临时路径。
//
// 返回的 from 是文件**自己说的**版本（没有 `schemaVersion` 键就是 0，见
// `splitEnvelope`），调用方自己决定要不要走迁移链。
func parseSettingsFile(b []byte) (Settings, int, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		return Settings{}, 0, err
	}
	return splitEnvelope(b, top)
}

// marshalSettingsFile 按我们自己的 schema 把一份设置写成字节。
//
// 信封是 `{"schemaVersion": N, "appSettings": {...}}`（`settingsFile`），
// 和 `SettingsStore.Flush` 写出去的是同一份形状——导出的那份配置**不带**这个
// 信封，那是另一条路（见 `export.go`），因为 Biu 设置页「导出配置」下载的
// 就是裸的 `appSettings`。
func marshalSettingsFile(s Settings) ([]byte, error) {
	return json.MarshalIndent(settingsFile{
		SchemaVersion: SchemaVersion,
		AppSettings:   s,
	}, "", "  ")
}

// splitEnvelope 把一份设置文件拆成「设置」和「它是第几版」。
//
// 三种形状，认错任何一种的后果都是**静默丢设置**：
//
//  1. 我们自己写的：{"schemaVersion": 1, "appSettings": {...}}
//  2. Biu 的 electron-store 落盘文件：{"appSettings": {...}, "__internal__": {...}}
//     （`__internal__` 是 conf 的内部状态，标准库自动忽略，我们也不写回去）
//  3. **Biu 设置页上「导出配置」下载的那份：裸的 `{...}`，外面没有信封。**
//     `biu/src/pages/settings/export-import.tsx:19` 写的是
//     `JSON.stringify(settingStore?.appSettings ?? defaultAppSettings, null, 2)`。
//     用户真正拿得着的导入文件就是这一种。
//
// 判据只有一条：**顶层有没有 `appSettings` 这个键**。裸的那份里根本没有它
// （那 20 个键是 autoStart、closeWindowOption……），两种形状不会认错。
//
// 裸的那份按 **v0** 读：它没有 schemaVersion 键，那就是 0，也就是「Biu 直接
// 导出的那份」——和它带信封时走的是同一条迁移链，v0 那一步会把 Biu 自己那个
// `"system-default"` 坏字体值修掉（`biu/src/store/settings.ts:37-39` 专门兼容
// 它，旧版本导出的文件里就是会有）。
func splitEnvelope(b []byte, top map[string]json.RawMessage) (Settings, int, error) {
	if _, ok := top["appSettings"]; ok {
		var f settingsFile
		if err := json.Unmarshal(b, &f); err != nil {
			return Settings{}, 0, err
		}
		return f.AppSettings, f.SchemaVersion, nil
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		return Settings{}, 0, err
	}
	return s, 0, nil
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
	// 「没设过」和「被钳到区间里」是两件事，兜底值必须分开：
	//
	//   - 文件里没有这一格，或者写的是 0 → 给**默认的 200**，不是下限 160。
	//     Biu 那边是 `sideMenuWidth ?? 200`（`biu/src/layout/side/index.tsx:49`），
	//     兜底也是 200；而且下一次写盘就会把这个 200 永久写回去。少了这一条，
	//     Biu 的旧导出、手写的设置文件第一次打开就变成 160，Biu 那边是 200。
	//   - 写了但是越界（比如手改成 99999）→ 钳进 [160, 480]。
	if s.SideMenuWidth <= 0 {
		s.SideMenuWidth = DefaultSideMenuWidth
	} else {
		s.SideMenuWidth = ClampSideMenuWidth(s.SideMenuWidth)
	}

	s.ThemeMode = pickEnum(s.ThemeMode, ThemeModeSystem,
		ThemeModeSystem, ThemeModeLight, ThemeModeDark)
	s.ThemePreset = pickEnum(s.ThemePreset, DefaultThemePreset,
		ThemePresetWarmPaper, ThemePresetDuskAmber, ThemePresetSoftPeach, ThemePresetClassic)
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
	// 设置文件里没有 playMode 这一格时它是空串，normalize 会把它变成默认档，
	// 于是「老版本升上来」和「新装」走的是同一条路。
	s.PlayMode = pickEnum(s.PlayMode, DefaultPlayMode, PlayModes...)
	s.ProxySettings.Type = pickEnum(s.ProxySettings.Type, ProxyNone,
		ProxyNone, ProxyHTTP, ProxyHTTPS, ProxySOCKS4, ProxySOCKS5)

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
	// 音量：钳进 [0, 1]。
	//
	// ⚠️ **「这一格不存在」和「这一格是 0」必须分开**：前者是老版本写的文件
	// （那时候音量压根不落盘，每一次启动都是 100%），该给**默认值**；后者是用户
	// 自己把滑块拉到最下（= 静音），该给 0。用 float64 的话两者读出来都是 0，
	// 而「启动时突然没声」是最难查的一类回归——所以这一格是指针（见
	// `Settings.Volume` 的注释）。
	//
	// 而 0 本身是合法的：用户把滑块拉到最下就是 0（= 静音，见 `App.SetVolume`）。
	if s.Volume == nil {
		v := DefaultVolume
		s.Volume = &v
	} else {
		v := min(max(*s.Volume, 0), 1)
		s.Volume = &v
	}
	// nil 和空序列在 JSON 里差一个 `null`，写出去会让「还没排过序」和
	// 「排过序，排出来是空的」看起来是两回事。统一成空序列。
	if s.SideMenuCollectionOrder.Created == nil {
		s.SideMenuCollectionOrder.Created = []int64{}
	}
	if s.SideMenuCollectionOrder.Collected == nil {
		s.SideMenuCollectionOrder.Collected = []int64{}
	}
	// 「上次听到哪儿」那一格，同一个理由（见 DefaultSettings 里那一句）。
	if s.Playback.Items == nil {
		s.Playback.Items = []PlaybackEntry{}
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
		err = writeFileAtomic(s.path, b)
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

// writeFileAtomic 原子写：临时文件 → fsync → rename。
//
// 任何一步失败都把临时文件删掉，不留垃圾。
//
// 这一份服务的是**这一包里的任何一份文件**（设置、总控标记、回滚时写回去的备份），
// 不是只服务设置——名字带 settings 是历史，改叫通用名是为了让 `upgrade.go` 那边的
// 调用读起来诚实（它写的是标记和备份，不是设置）。
func writeFileAtomic(path string, b []byte) (err error) {
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
