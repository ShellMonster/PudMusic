// 应用设置的落盘：schema 版本 + 原子写 + 异步合并写。
//
// 临时文件 + rename 只保证「写到一半被断电」不会留下半个文件；schema 版本和未来
// 版本只读保护则由设置文件信封负责。
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

	"github.com/ShellMonster/PudMusic/internal/service/audio"
)

const SchemaVersion = 1

// SettingsFileName 是设置文件名。
const SettingsFileName = "settings.json"

// 设置文件里几个约定值。
const (
	// DefaultFontFamily 是应用的默认字体。
	DefaultFontFamily = "system-ui"
	// DefaultPrimaryColor 是应用的默认主色。
	DefaultPrimaryColor = "#1ed760"
	// DefaultBorderRadius 是应用的默认圆角。
	DefaultBorderRadius = 8.0
	// MaxBorderRadius 是圆角的上限。
	MaxBorderRadius = 24.0
	// DefaultVolume 是「用户没设过音量」时给的默认音量（1 = 100%）。
	DefaultVolume = 1.0
	// MaxTrackFadeSeconds 是切歌淡入淡出能设的最长时间。
	MaxTrackFadeSeconds = 15
	// DefaultUnmuteVolume 是「压根没有音量可放回去」时取消静音放回去的值。
	//
	// 0.5 是一半——用户按了「取消静音」，听到的应该是一半声音，不是 0。
	DefaultUnmuteVolume = 0.5
	// DefaultMiniLyricsCurtain 是「迷你播放器点封面展开歌词卷帘」那一格的默认
	// 值。**默认开**（票 #536，2026-10-26 主人定的）。
	//
	// ⚠️ **它是 true 而不是 false**：主人 2026-10-26 先说「默认关」，后一天
	// 更正成「默认开」——**以票面为准**（票 #536 的正文写的是「默认开着」）。
	// 装上就有卷帘这个效果，是这一格存在的意义。
	DefaultMiniLyricsCurtain = true
)

// 侧栏宽度的三个数：
//
//	COLLAPSED_WIDTH = 64    折叠后多宽
//	MIN_WIDTH       = 180   展开时的下限，拖到它下面就折叠
//	MAX_WIDTH       = 300   展开时的上限
//
// 放在这一层而不是界面层，是因为 normalize 也要用：设置文件是用户能直接编辑的，
// 一个 `sideMenuWidth: 99999` 不该一路穿到画界面的那一行。
const (
	// 设计规范 v0.1 §4：展开 220（可拖 180 到 300），折叠 64 仅图标。
	CollapsedSideMenuWidth = 64.0
	MinSideMenuWidth       = 180.0
	MaxSideMenuWidth       = 300.0
	DefaultSideMenuWidth   = 220.0
)

// ClampSideMenuWidth 把一个宽度拉进 [MinSideMenuWidth, MaxSideMenuWidth]。
//
// 拖动过程中**不要**用它：拖到 100 时画出来的就该是 100（折叠中的样子），
// 只有落盘的值和重读设置时才钳。拖动过程中保持原始值，便于显示折叠状态。
func ClampSideMenuWidth(v float64) float64 {
	if v < MinSideMenuWidth {
		return MinSideMenuWidth
	}
	if v > MaxSideMenuWidth {
		return MaxSideMenuWidth
	}
	return v
}

// 主题档位。
type ThemeMode string

const (
	ThemeModeSystem ThemeMode = "system"
	ThemeModeLight  ThemeMode = "light"
	ThemeModeDark   ThemeMode = "dark"
	// ThemeModeSchedule 晚上（19:00 到次日 07:00）用深色，白天用浅色。
	ThemeModeSchedule ThemeMode = "schedule"
)

// ThemePreset 是主题方案：一套整体配色（底色、文字色、强调色和圆角，浅色深色
// 各一份）。暖纸 / 暮光 / 柔雾来自设计规范 v0.1 §2；青瓷 / 墨宣 / 紫檀 / 靛夜
// 是 2026-10-26 本仓库**原创**补的四套（补上冷色和水墨/紫/蓝几个色相，对比度
// 按 WCAG 现算过）——数值不取自任何现成的公开色板。
//
// 旧外观没有这一格，默认方案仍是暖纸。

// 套了方案之后，方案的底色、文字、强调色、圆角**压过**设置里的 `primaryColor` /
// `backgroundColor` / `borderRadius`；用户在设置页改那三项时界面层会先把方案清成
// 经典，免得「改了没反应」。
type ThemePreset string

const (
	ThemePresetWarmPaper   ThemePreset = "warm-paper"
	ThemePresetDuskAmber   ThemePreset = "dusk-amber"
	ThemePresetSoftPeach   ThemePreset = "soft-peach"
	ThemePresetCeladon     ThemePreset = "celadon"      // 青瓷
	ThemePresetInkPaper    ThemePreset = "ink-paper"    // 墨宣
	ThemePresetSandalwood  ThemePreset = "sandalwood"   // 紫檀
	ThemePresetIndigoNight ThemePreset = "indigo-night" // 靛夜
	ThemePresetClassic     ThemePreset = "classic"
)

// DefaultThemePreset 是新设置的默认方案。
//
// 它是**生产默认**：应用还没发版，没有需要保住的旧外观，所以默认一律是暖纸
// （缺键、空串、不认识的值都落到这里，见 `pickEnum`）。
//
// 「经典」这个**用户选项**照旧保留（设置页方案卡里有它，摆在最后一行末尾），它自己那套配色有
// 专门的用例盯着（`internal/ui/themepreset_test.go` 及那几处按方案各跑一遍的
// 截图用例）。而「生产默认是暖纸」由本包里的测试盯着（`themepreset_test.go`）。
var DefaultThemePreset = ThemePresetWarmPaper

// 音质档位。默认是有损的高品质档（音乐库 320K / 投稿 192K），无损/杜比是显式选项——
// 拿不到时由取流那层自动退到最高有损，不报错。
type AudioQuality string

const (
	AudioQualityLossless AudioQuality = "lossless"
	AudioQualityDolby    AudioQuality = "dolby"
	AudioQualityHigh     AudioQuality = "high"
	AudioQualityMedium   AudioQuality = "medium"
	AudioQualityLow      AudioQuality = "low"
)

// 列表显示方式。
type DisplayMode string

const (
	DisplayModeCard    DisplayMode = "card"
	DisplayModeList    DisplayMode = "list"
	DisplayModeCompact DisplayMode = "compact"
)

// BackdropClarity 是**满屏播放器背景的清晰度**（票 #666）。
//
// 背景那一层本来是「封面放大四倍、明显模糊」（照 Biu 的 `glassmorphism.ts`），
// 所以它**清晰度天生就低**。这一格让用户自己挑：
//
//	流畅  画布 = 窗口的 1/4，σ=32 DIP（**默认**，也是从前的样子）
//	清晰  画布 = 窗口的 1/2，σ=16 DIP
//	高清  画布 = 窗口的 1/1，σ=8 DIP
//
// ⚠️ **画布越大，要的源图越大、GPU 模糊也越贵**——三档是按「像素量级」分的
// （27 万 / 103 万 / 397 万像素，见票面）。
type BackdropClarity string

const (
	BackdropClaritySmooth BackdropClarity = "smooth"
	BackdropClarityClear  BackdropClarity = "clear"
	BackdropClarityHD     BackdropClarity = "hd"
)

// 画布是窗口的几分之一（对应上面那三档）。
//
// ⚠️ **它是「分母」**：4 表示画布 = 窗口 / 4。值越小画布越大、越清晰。
func (c BackdropClarity) CanvasDivisor() float64 {
	switch c {
	case BackdropClarityClear:
		return 2
	case BackdropClarityHD:
		return 1
	}
	return 4
}

// BlurDIP 是这一档在**屏幕上**的模糊半径（DIP）。
//
// ⚠️ **画布越大、σ 越小**：模糊的观感要跟着画布走——画布是 1/4 时 σ=32 正好
// 「把细节全糊掉」，画布放大四倍之后同样的 32 DIP 会糊得太狠（糊成一片纯色），
// 于是按同样的比例降到 8。
func (c BackdropClarity) BlurDIP() float64 {
	switch c {
	case BackdropClarityClear:
		return 16
	case BackdropClarityHD:
		return 8
	}
	return 32
}

// 页面切换动画。
type PageTransition string

const (
	PageTransitionNone    PageTransition = "none"
	PageTransitionFade    PageTransition = "fade"
	PageTransitionSlide   PageTransition = "slide"
	PageTransitionScale   PageTransition = "scale"
	PageTransitionSlideUp PageTransition = "slideUp"
)

// 关闭窗口时怎么办。
type CloseWindowOption string

const (
	CloseWindowHide CloseWindowOption = "hide"
	CloseWindowExit CloseWindowOption = "exit"
)

// 代理协议。支持 HTTP、HTTPS、SOCKS4 和 SOCKS5。
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
// Port 用指针表达「未设置」和「设成 0」的区别。
type ProxySettings struct {
	Type     ProxyType `json:"type"`
	Host     string    `json:"host"`
	Port     *int      `json:"port"`
	Username string    `json:"username"`
	Password string    `json:"password"`
}

// SideMenuCollectionFolded 是侧栏两组收藏的折叠状态。
type SideMenuCollectionFolded struct {
	Created   bool `json:"created"`
	Collected bool `json:"collected"`
}

// SideMenuCollectionOrder 是侧栏两组收藏的**顺序**：每组一条 ID 序列。
//
// 条目每次从接口拉取，顺序是用户拖出来的，因此只保存顺序。
// 设置存储统一提供 schema 版本、原子写、退出前 flush 和只读保护。
//
// 加键不用涨 SchemaVersion（只加新的可选键不用涨）；读不到就是空序列，等于「还没排过序」。
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

// DynamicSeenEntry 是一个账号的「看到哪儿了」。
type DynamicSeenEntry struct {
	// Mid 是这个账号的 mid。
	Mid int64 `json:"mid"`
	// Baseline 是**用户看过的那条最新动态的 id**（B 站的动态 id，18 位左右）。
	//
	// ⚠️ **它是动态 id，不是时间戳**：B 站「有没有新动态」那个接口
	// （/feed/all/update）要的就是它（社区文档：update_baseline 是 str、必填；
	// 真实的返回长这样："954659517801431040"）。传一个时间戳进去，那个接口要么
	// 回错、要么当成「比它新的都算」——而界面上只是「那个点不对」。
	//
	// 空串 = 一次都没看过 → 请求动态更新时传 0。
	Baseline string `json:"baseline"`
}

// DynamicSeen 是「动态红点看到哪儿了」——**按账号**记。
//
// ⚠️ **它按账号记**：动态流是「**我关注的人**在发什么」，那是**按账号**的
// （和 dynamicFeed 那一页同一个理由，见 upspace.go）。换账号之后那是另一份
// 「没看过的动态」，基准不能接着上一个账号的用。
//
// ⚠️ **它是一张表，不是一格**：账号来回切的时候，**每一个账号的基准都得留着**——
// 冲掉上一个账号那一格的话，切回去之后那个点又会按「全部没看过」亮起来，而用户
// 明明看过了（那张表是这一票的第二版；第一版只有一格，评审查出来的）。
//
// 加键不用涨 SchemaVersion（见 SchemaVersion 的注释：只加新的可选键不用涨）：
// 读不到就是空表，等于「每个账号都没看过」。
type DynamicSeen struct {
	// Baselines 是每一个账号的基准。
	//
	// ⚠️ **它是一个序列，不是一个映射**：映射的**空字面量要分配**（序列不会——
	// 空序列指向 `runtime.zerobase`），而 `DefaultSettings` 每帧都可能被调
	// （`SettingsSnapshot` 在没挂存储时给的就是它）——帧预算那一条盯着每帧的
	// 分配数（实测：映射那一版让播放条那一档从 14 次涨到 16 次，上限正好是 16）。
	// 一张表只有一两条，扫一遍就行。
	//
	// 0 = 一次都没看过 → 请求动态更新时传 0。
	Baselines []DynamicSeenEntry `json:"baselines"`
}

// Settings 是一份完整的应用设置。
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

	// PlayMode 是播放模式（顺序 / 循环 / 随机 / 单曲）。
	PlayMode PlayMode `json:"playMode"`
	// BackdropClarity 是满屏播放器背景的清晰度（票 #666）。空串 = 流畅。
	BackdropClarity BackdropClarity `json:"backdropClarity"`
	// ShouldKeepPagesOrderInRandomPlayMode 是「随机时保持分集顺序」。
	ShouldKeepPagesOrderInRandomPlayMode bool `json:"shouldKeepPagesOrderInRandomPlayMode"`

	// Volume 是用户设的音量（0..1），1 是解码出来的原样。
	//
	// ⚠️ **用指针**：「这份设置里没有这一格」（老版本写的文件）和「用户把音量
	// 拉到 0」必须分得开——前者该给默认值（100%），后者该给 0（真的没声）。
	// 用 float64 的话两者读出来都是 0，而「启动时突然没声」和「用户自己拉到 0」
	// 是两回事：前者是 bug，后者是用户要的。
	//
	// 用指针表达「未设置」和「设成 0」的区别。
	//
	// 键名保持稳定，位置与播放相关设置放在一起。
	Volume *float64 `json:"volume"`

	// PlaybackRate 是用户设的播放倍率。0 表示没设过（倍率的合法范围从 0.5 起，
	// 所以 0 不会和真值混淆），读出来按原速。重启之后还在。
	PlaybackRate float64 `json:"playbackRate"`

	// NormalizeVolume 是「音量均衡」开关：把特别响的歌压到统一的响度，只压不抬
	// （见 internal/service/audio/normalize.go）。默认关。
	NormalizeVolume bool `json:"normalizeVolume"`

	// MiniLyricsCurtain 是「迷你播放器点封面展开歌词卷帘」那个开关。**默认开**
	// （票 #536，2026-10-26 主人定的）。
	//
	// ⚠️ **用指针**，和上面那个 `Volume` 同一路数：「这份设置里没有这一格」
	// （老版本写的文件）和「用户把卷帘关了」必须分得开——前者该给默认的
	// **开**，后者才是关。
	//
	// ⚠️ **为什么不能是 `bool`**：布尔值表达不了「没设过」，而这一格的默认
	// 恰恰是**开**——用 `bool` 的话，一份老设置文件读出来就是 false（= 关），
	// 于是「默认开着」这件事对老用户**不成立**（而票面要的是「装上就有
	// 卷帘」）。这一格是**新增**的键，所以 nil = 没设过 = 默认开。
	//
	// 这一格使用指针，区分「未设置」和「用户关闭」。缺少字段时由 normalize 填入默认值。
	MiniLyricsCurtain *bool `json:"miniLyricsCurtain"`

	// CoverAccent 是「封面取色」开关：主色跟着当前封面的色相走。默认关。
	CoverAccent bool `json:"coverAccent"`

	// UpdateNotification 是「有更新了弹系统通知」开关（票 #559）。默认**开**。
	//
	// ⚠️ **它默认开，而它的零值是 false**：所以「这一格不存在」（老版本写的
	// 设置文件）和「用户把它关了」在读回来之后分不开——两者的读回来都是 false，
	// 而老用户升级上来之后**不该**平白多一条系统通知的打扰。
	//
	// ⚠️ **所以这一格是指针**（`*bool`），跟 `Volume` 同一个理由：
	// `*bool` 能把「没设过」（nil → 默认开）和「用户关了」（false）分开。
	// 用裸 bool 的话，要么老用户升级上来就被静默关掉（那也是错的方向），要么
	// 拿不到「用户关了」这一档。
	//
	// 键名是应用自己的设置字段。
	UpdateNotification *bool `json:"updateNotification"`

	// CoverHue / CoverSat 是界面从当前封面取出来的色相（0~360）和饱和度（0~1），只在
	// 内存里传给主题，不落盘。CoverSat 为 0 表示还没取到（或封面没颜色）。
	CoverHue, CoverSat float64 `json:"-"`

	// TrackFadeSeconds 是切歌淡入淡出的时长（秒，0..15），0 = 关（默认）。
	// 一首快放完时音量渐渐降到 0，下一首（自动接上的）从 0 渐渐升回来。
	TrackFadeSeconds int `json:"trackFadeSeconds"`

	// VideoQuality 是看视频时**优先**的画质上限（B 站的 qn：16/32/64/80/112/120），
	// 0 = 默认（720P）。这一集没有那一档时（比如没开会员）自动降到最接近的低档。
	VideoQuality int `json:"videoQuality"`

	// EQPreset 是均衡器预设的 ID（见 audio.EQPresets），空或 "flat" = 关（默认）。
	EQPreset string `json:"eqPreset"`

	// Muted 是静音标志。**它和音量分开存**（票 #311 / 报告 §3.3 的 B2）。
	//
	// ⚠️ **别再用「音量 0」顶替静音**：落盘之后那就是个坑——静音之后退出再
	// 启动，音量变成 0（Clementine 的老毛病）。静音是它自己的一格，而音量那一
	// 格记的是「用户设的是多少」。
	//
	// Muted 是静音标志，和音量分开存。
	Muted bool `json:"isMuted"`

	// Playback 是「上次听到哪儿」那一格：队列、当前项、播放位置。
	//
	// 它和音量、播放模式在同一个文件里，是有意的：同一次原子写里落盘，
	// 它们之间就没有「谁先谁后」的竞态（见 playback_state.go 文件头那张表）。
	Playback PlaybackSnapshot `json:"playback"`

	// DynamicSeen 是「动态红点看到哪儿了」。
	DynamicSeen DynamicSeen `json:"dynamicSeen"`
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

// MiniLyricsCurtainOn 报「迷你播放器点封面展开歌词卷帘」那一格是开是关。
//
// ⚠️ **nil 算「开」**：这一格的默认是开（票 #536），而 nil 是「这份设置里
// 没有这一格」——老设置文件读出来就是 nil。界面层问这一格时走这一个方法，
// 于是「默认开着」这件事只有一处定义（`DefaultMiniLyricsCurtain`）。
//
// ⚠️ 正常路径上 `normalize()` 已经把 nil 填成默认了（见 `Settings.normalize`），
// 而这里再兜一次是因为**零值 `Settings{}`** 也可能出现（界面上手工拼的那几处）。
func (s Settings) MiniLyricsCurtainOn() bool {
	if s.MiniLyricsCurtain == nil {
		return DefaultMiniLyricsCurtain
	}
	return *s.MiniLyricsCurtain
}

// settingsFile 是当前版本的设置文件信封。
type settingsFile struct {
	SchemaVersion int      `json:"schemaVersion"`
	AppSettings   Settings `json:"appSettings"`
}

// DefaultSettings 返回应用的默认设置。downloadPath 传默认下载目录。
//
// 默认落点由 `platform.DefaultDownloadsDir` 算出来（「音乐」目录下的应用子目录），
// 不是系统下载目录——见 `platform/downloadsdir.go` 顶上那一节。
func DefaultSettings(downloadPath string) Settings {
	// 音量那一格是**指针**，所以每一次调用都要新的一份——共用同一个指针的话，
	// 有人改了 `*s.Volume` 就把全局的默认值改了。
	vol := DefaultVolume
	// 「有更新了弹系统通知」那一格也是**指针**（理由见 `Settings.UpdateNotification`
	// 那一格），而且它默认**开**——系统通知是低频的好事，而新装的用户还没说过
	// 「不要」。
	updateNotif := true
	// 「迷你播放器点封面展开歌词卷帘」那一格同样是**指针**（理由见那个字段的
	// 注释），而它**默认是开的**（票 #536）。
	curtain := DefaultMiniLyricsCurtain
	return Settings{
		AutoStart:         false,
		CloseWindowOption: CloseWindowHide,
		FontFamily:        DefaultFontFamily,
		BorderRadius:      DefaultBorderRadius,
		DownloadPath:      downloadPath,
		PrimaryColor:      DefaultPrimaryColor,
		BackgroundColor:   "",
		AudioQuality:      AudioQualityHigh,
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

		// 空序列表示每个账号都还没有已读基准；请求动态更新时传 0。
		//
		// ⚠️ **空序列不分配**（它指向 `runtime.zerobase`），而空映射要分配——
		// 理由见 DynamicSeen.Baselines 那一格。
		DynamicSeen: DynamicSeen{Baselines: []DynamicSeenEntry{}},

		// 音量：默认 100%（老版本每一次启动都是这一档，见 DefaultVolume）。
		Volume: &vol,
		Muted:  false,
		// 「有更新了弹系统通知」：默认开。
		UpdateNotification: &updateNotif,

		// 迷你播放器的歌词卷帘：**默认开**（票 #536）。
		//
		// ⚠️ 它是**指针**，所以每一次调用都要新的一份——共用同一个指针的话，
		// 有人改了 `*s.MiniLyricsCurtain` 就把全局的默认值改了（和上面那个
		// `vol` 同一路数）。
		MiniLyricsCurtain: &curtain,

		// 「上次听到哪儿」那一格：默认是空的（= 没有上次听到哪儿）。
		//
		// ⚠️ **`Items` 给空序列而不是 nil**：nil 和空序列在 JSON 里差一个
		// `null`，写出去会让「还没存过」和「存过、当时队列是空的」看起来是两
		// 回事。统一成空序列。
		Playback: PlaybackSnapshot{Items: []PlaybackEntry{}},

		// 播放模式的两个默认值：默认循环播放，随机时保持分集顺序。
		PlayMode:                             DefaultPlayMode,
		ShouldKeepPagesOrderInRandomPlayMode: true,
	}
}

// UpdateNotificationEnabled 报「有更新了要不要弹系统通知」。
//
// ⚠️ **它把「没设过」和「用户关了」分开**（理由见 `Settings.UpdateNotification`
// 那一格）：`nil`（老版本写的文件）按**默认开**算，而 `false`（用户明确关了）
// 是关。
//
// ⚠️ **裸 bool 的零值做不到这件事**：false 既是「没设过」又是「用户关了」。
func (s Settings) UpdateNotificationEnabled() bool {
	if s.UpdateNotification == nil {
		return true
	}
	return *s.UpdateNotification
}

// 当前版本设置文件的坏法分为损坏、版本不匹配和存储已关闭三类。
var (
	// ErrSettingsCorrupt：文件在，但不是当前版本写出的有效 JSON。
	// 处理：备份走，重置成默认值，让用户继续用。
	ErrSettingsCorrupt = errors.New("app: 设置文件已损坏，已重置为默认值")

	// ErrSettingsFromNewer：文件来自一个 schema 更高的版本。
	// 处理：一个字都不许写回去（store 转成只读），避免旧版本改写新版本数据。
	ErrSettingsFromNewer = errors.New("app: 设置文件来自更新的版本，本次只读不写")

	// ErrSettingsClosed：进程已经在收尾，不接受改动了。
	ErrSettingsClosed = errors.New("app: 设置存储已关闭")
)

// LoadSettings 读取当前版本的设置文件；缺失文件使用默认值，旧版和未来版本不写回。
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
		backupSettings(path)
		return DefaultSettings(""), fmt.Errorf("%w: %s (%v)", ErrSettingsCorrupt, path, err)
	}
	if f.SchemaVersion != SchemaVersion {
		return DefaultSettings(""), fmt.Errorf("%w: 文件是 v%d，我们只到 v%d（%s）",
			ErrSettingsFromNewer, f.SchemaVersion, SchemaVersion, path)
	}
	f.AppSettings.normalize()
	return f.AppSettings, nil
}

// marshalSettingsFile 按当前版本的信封写出设置。
func marshalSettingsFile(s Settings) ([]byte, error) {
	return json.MarshalIndent(settingsFile{
		SchemaVersion: SchemaVersion,
		AppSettings:   s,
	}, "", "  ")
}

// hexColor 是合法的颜色写法，和 MyGo 的 ui.Hex 接受的一样。
// 这里自己写一份：本包不认识界面，不能 import mygo/ui。
var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

func (s *Settings) normalize() {
	s.BorderRadius = min(max(s.BorderRadius, 0), MaxBorderRadius)
	if s.FontFamily == "" {
		s.FontFamily = DefaultFontFamily
	}
	// 「没设过」和「被钳到区间里」是两件事，兜底值必须分开：
	// 文件里没有这一格，或者写的是 0 → 给默认的 220；写了但是越界则钳进 [180, 300]。
	if s.SideMenuWidth <= 0 {
		s.SideMenuWidth = DefaultSideMenuWidth
	} else {
		s.SideMenuWidth = ClampSideMenuWidth(s.SideMenuWidth)
	}

	s.ThemeMode = pickEnum(s.ThemeMode, ThemeModeSystem,
		ThemeModeSystem, ThemeModeLight, ThemeModeDark, ThemeModeSchedule)
	s.ThemePreset = pickEnum(s.ThemePreset, DefaultThemePreset,
		ThemePresetWarmPaper, ThemePresetDuskAmber, ThemePresetSoftPeach,
		ThemePresetCeladon, ThemePresetInkPaper, ThemePresetSandalwood,
		ThemePresetIndigoNight, ThemePresetClassic)
	// 认不出的值归一成默认的有损档；无损和杜比拿不到时由取流层退档。
	s.AudioQuality = pickEnum(s.AudioQuality, AudioQualityHigh,
		AudioQualityLossless, AudioQualityDolby, AudioQualityHigh,
		AudioQualityMedium, AudioQualityLow)
	s.DisplayMode = pickEnum(s.DisplayMode, DisplayModeList,
		DisplayModeCard, DisplayModeList, DisplayModeCompact)
	s.PageTransition = pickEnum(s.PageTransition, PageTransitionNone,
		PageTransitionNone, PageTransitionFade, PageTransitionSlide,
		PageTransitionScale, PageTransitionSlideUp)
	s.CloseWindowOption = pickEnum(s.CloseWindowOption, CloseWindowHide,
		CloseWindowHide, CloseWindowExit)
	// 设置文件里没有 playMode 这一格时它是空串，normalize 会把它变成默认档。
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
	// 「每个账号看到哪儿了」那张表：nil 和空表在 JSON 里差一个 `null`，而它写出去
	// 的形状该是稳定的（空表 = 每个账号都没看过）。
	//
	// ⚠️ **空序列不分配**（它指向 `runtime.zerobase`）——见 DynamicSeen.Baselines。
	if s.DynamicSeen.Baselines == nil {
		s.DynamicSeen.Baselines = []DynamicSeenEntry{}
	}
	// 音量：钳进 [0, 1]。
	//
	// 音量缺少时使用默认值，显式设置为 0 时保留静音值。
	if s.Volume == nil {
		v := DefaultVolume
		s.Volume = &v
	} else {
		v := min(max(*s.Volume, 0), 1)
		s.Volume = &v
	}
	// 有更新通知缺少时默认开启。
	if s.UpdateNotification == nil {
		v := true
		s.UpdateNotification = &v
	}
	// 迷你播放器歌词卷帘缺少时默认开启，用户明确关闭的值保持不变。
	if s.MiniLyricsCurtain == nil {
		v := DefaultMiniLyricsCurtain
		s.MiniLyricsCurtain = &v
	}
	if audio.EQPresetByID(s.EQPreset).ID == "flat" {
		s.EQPreset = ""
	}
	s.TrackFadeSeconds = min(max(s.TrackFadeSeconds, 0), MaxTrackFadeSeconds)
	if !slices.Contains(VideoQualitySteps, s.VideoQuality) {
		s.VideoQuality = 0
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

// VideoQualitySteps 是设置里「视频画质」能选的档（B 站的 qn，0 = 默认）。
var VideoQualitySteps = []int{0, 16, 32, 64, 80, 120}
