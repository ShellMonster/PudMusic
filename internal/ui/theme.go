// 主题：把设置里的四件事（跟随系统/浅色/深色、主色、背景色、圆角）
// 变成 MyGo 每一帧真正用的那份主题。
//
// 🔴 铁律：Apply 必须在**每个窗口的每一帧开头**调一次。
//
// MyGo 的 ui/context.go:120 在每帧开头都执行
// `c.theme = c.rt.defaultTheme()`——把主题重置成跟随系统外观的那份。
// 上一帧 SetTheme 进去的东西**被静默丢掉**：不报错、不崩溃、不打日志，
// 就是主题悄悄跳回系统色。所以「在切换按钮的回调里 SetTheme 一次」这种
// 写法看起来能用，切完那一帧是对的，下一帧就没了。官方文档没写这个坑，
// `apply()` 下面那条测试就是为了钉住它。
//
// 接进 shell 的位置：`Shell.applyFrameTheme`，viewMain 和 viewCrash 的第一行
// 各调它一次。别的地方不许直接调 `c.SetTheme`——漏一处就有一帧用系统色。
package ui

import (
	"strconv"
	"strings"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// ApplyTheme 在一帧的开头装上当前设置对应的主题。
//
// systemDark 是「系统现在是不是深色」。传 `c.Theme().Dark` 即可——那是在
// ApplyTheme 之前读的、MyGo 刚重置好的那份跟随系统的主题，正好是系统外观。
// **必须先读再设**，反过来的话读到的是自己上一帧设的值。
//
// ⚠️ **这一份不缓存**：它是给测试和预览用的（`grep ApplyTheme` 只有定义和测试
// 文件），而它们**不是每帧调**。生产上每帧调的那一处是 `Shell.applyFrameTheme`，
// 那里有缓存（见 `themeCache`）。
func ApplyTheme(c *ui.Context, s app.Settings, systemDark bool) {
	c.SetTheme(buildTheme(s, systemDark, c.Preferences()))
}

// themeKey 是决定主题的那几样东西。
//
// ## ⚠️ **只放真正影响主题的字段**
//
// 设置里还有别的（下载目录、隐藏的菜单项、播放模式……），那些跟主题无关。
// 放进来会让「设置改了」变成「设置里任何一格变了都重算」——而重算是每帧一次
// 的事，于是缓存就白做了。
//
// 这一份是**可比较的**（几个字符串 ＋ 几个数），所以判「变了没有」就是一次
// `==`，不分配任何东西。
type themeKey struct {
	// mode 是设置里的那一档（跟随系统 / 浅色 / 深色）。
	mode app.ThemeMode
	// radius 是设置里的圆角。
	radius float64
	// font 是设置里的字体（空串或系统默认 = 不设）。
	font string
	// preset 是主题方案（空 = 经典）。
	preset app.ThemePreset
	// primary / background 是设置里的主色和底色（十六进制串）。
	primary    string
	background string
	// dark 是这份主题**实际要的那一档深浅**（见 `effectiveDark`）。
	//
	// ⚠️ **不是「系统是不是深色」**：档位是浅色/深色时，主题跟系统深浅无关，
	// 而 MyGo 一帧要跑两遍 view（第二遍读到的 `c.Theme()` 是**我们自己**装上去
	// 的那一份，见 `applyFrameTheme` 的注释）——于是按「系统深浅」做键的话，
	// 档位压过系统时两遍的键不一样，缓存每一帧都要重算，白做了。
	dark bool
	// contrast / textScale 是桌面偏好（高对比度、字号缩放）。
	contrast  bool
	textScale float32
}

var themeNow = time.Now

// 「定时」档：这个钟点之后用深色，早上这个钟点起用浅色。
const (
	scheduleDarkFrom  = 19
	scheduleLightFrom = 7
)

// effectiveDark 是这份设置实际要的那一档深浅。
//
// 档位是浅色/深色时由档位说了算；「跟随系统」时才看系统。
//
// ⚠️ **这是 `buildTheme` 判深浅的唯一出处**——缓存的键也用它，两边必须一致，
// 否则会出现「键变了但主题没变」（白重算）或「主题变了但键没变」（主题不更新）。
func effectiveDark(s app.Settings, systemDark bool) bool {
	switch s.ThemeMode {
	case app.ThemeModeLight:
		return false
	case app.ThemeModeDark:
		return true
	case app.ThemeModeSchedule:
		h := themeNow().Hour()
		return h >= scheduleDarkFrom || h < scheduleLightFrom
	}
	return systemDark
}

// themeCache 是主题的缓存：设置没变就复用同一份。
//
// ## 为什么能缓存
//
// 主题只在**设置变了 / 系统深浅变了 / 桌面偏好变了**的时候才变——而那三样
// 全在 `themeKey` 里，于是「键一样就复用」就等于「变了立刻生效」（下一帧
// 就是新的）。
//
// ## 为什么备两份（热 / 不热）
//
// 滚动条的悬停档是**装上去之前**改一个字段的（`scrollbarHot`，见
// `applyFrameTheme`），而缓存下来的那份是**跨帧共用**的——在它身上改字段的话，
// 改的是缓存本身，下一帧「不热」的那一档也变成热的了（滚动条一直是深的那一档，
// 而且没有症状：只是深了一档）。
//
// 所以热的那一档是**另建的一份**（值拷贝一份，字段改在那一份上）。
type themeCache struct {
	// key 是这一份是按哪几样拼出来的。
	key themeKey
	// base 是不热的那一份。
	base *ui.Theme
	// hotTheme 是指针压在滚动条上的那一档。
	hotTheme *ui.Theme
	// valid 是「这一份是真的」（零值的那一份是空的）。
	valid bool
}

// of 取出这一帧该用的那一份主题，必要时重算。
//
// ⚠️ **重算只发生在键变了的时候**：设置改了、系统深浅变了、桌面偏好变了——
// 三样都在键里。
func (c *themeCache) of(s app.Settings, systemDark bool, p ui.Preferences) *ui.Theme {
	key := themeKey{
		mode:       s.ThemeMode,
		preset:     s.ThemePreset,
		radius:     s.BorderRadius,
		font:       s.FontFamily,
		primary:    s.PrimaryColor,
		background: s.BackgroundColor,
		dark:       effectiveDark(s, systemDark),
		contrast:   p.HighContrast,
		textScale:  p.TextScale,
	}
	if !c.valid || c.key != key {
		base := buildTheme(s, systemDark, p)
		// 热的那一档是**值拷贝**一份：改的是拷贝，缓存里那一份不动。
		hot := *base
		hot.Scrollbar = scrollbarHot(&hot)
		c.key, c.base, c.hotTheme, c.valid = key, base, &hot, true
	}
	return c.base
}

// hot 取出「指针压在滚动条上」的那一档。
//
// ⚠️ **它必须是另建的一份**：见 `themeCache` 顶上那一段——在缓存的那一份上
// 改字段的话，改的是缓存本身。
func (c *themeCache) hot() *ui.Theme { return c.hotTheme }

// applyFrameTheme 是**每帧开头装主题**的那一处，生产上唯一会走到的入口。
//
// ⚠️ 它直接调 `themeCache.of` + `c.SetTheme`，不再转一手 `ApplyTheme`：滚动条的
// 悬停档要在装上去之前换一份（`scrollbarHot`），而那个状态只在 Shell 上。
// `ApplyTheme` 因此只剩测试和预览在用（`grep ApplyTheme` 只有定义和测试文件），
// **改 `ApplyTheme` 不会影响外壳**——要改生产行为，改这里和 `buildTheme`。
//
// ⚠️ **先读 `c.Theme().Dark` 再设**：读到的是 MyGo 刚在帧开头重置好的、跟随
// 系统的那份，也就是「系统现在是不是深色」。反过来的话读到的是我们上一帧
// 自己设的档位，「跟随系统」在系统浅色时会画出深色——深浅整个反过来，而且
// 不报错，只是界面和系统对不上。
//
// **一帧里调几次都对**，页面 view 因此可以在自己开头照惯例也调一次（SetTheme
// 只影响它之后创建的控件，页面 view 忘了装主题就会用系统色画出来，跟旁边的
// 界面对不上）。两次结果一样，是两件事各自兜住的：
//
//   - 跨 build pass：MyGo 每个 pass 开头的 `context.reset`
//     （ui/context.go:120）都把 `c.theme` 换回框架那份，而 `SetTheme` 只改
//     `c.theme`、不碰 `rt.theme`，所以下一次读到的仍然是系统那份。
//   - 同一 pass 内连着调：档位是浅色/深色时 `dark` 由档位说了算，读到的
//     `Dark` 是几都不影响；档位是「跟随系统」时第一次装完 `Dark` 就等于
//     系统深浅，第二次拿它当 `systemDark` 再走一遍，还是同一个值。
//
// （这两条曾经被写反过一次：一版注释说「一帧只能调一次，重复调会把跟随系统
// 搞反」，还为此加了个 `s.themed` 守卫。枚举了（系统深浅 × 三档）八种组合，
// 删掉守卫全绿——那个失败模式不存在。）
//
// ## ⚠️ 主题是**缓存**着的（见 `themeCache`）
//
// 原先这一帧要 `buildTheme` 一次：新建一份 `*ui.Theme`，并且**每帧重新解析**
// 用户设置里的颜色串（`parseHexColor` 每帧两个切片）。而主题只在设置变了、
// 系统深浅变了、桌面偏好变了的时候才变——于是那三样做成一个键，键一样就复用。
//
// 缓存的是**值**（一份 `*ui.Theme`），不是「装不装」——铁律一那条「每帧开头
// 都要装」照样成立：每帧还是调 `SetTheme`，只是装上去的是同一份。
func (s *Shell) applyFrameTheme(c *ui.Context) {
	systemDark := c.Theme().Dark
	t := s.theme.of(s.app.SettingsSnapshot(), systemDark, c.Preferences())
	// 指针压在滚动条上时深一档（上游 `--os-handle-bg-hover`）。框架的绘制
	// 只在**指针落在容器上**时画滚动条，所以这里只管「出现之后更明显」。
	//
	// ⚠️ 读的是**上一帧**量到的状态（`scrollbarHot`）：这一帧的容器还没建出来，
	// 问不到。指针一移动就会有一帧，所以看上去是「移上去就变深」，不会晚。
	if s.scrollbarHot {
		t = s.theme.hot()
	}
	c.SetTheme(t)
	s.publishCard(t)
}

// BuildTheme 按设置拼出这一帧要用的主题。
//
// 从 `ui.LightTheme()` / `ui.DarkTheme()` 起步而不是从零拼结构体：那两份
// 是 MyGo 自己的默认值，跟着框架升级一起变；自己写一个 `&ui.Theme{...}`
// 的话，MyGo 新加一个字段我们不会知道，控件就会用零值画。
//
// ⚠️ **这一份不带桌面偏好**（高对比度 / 字号缩放）。它是给测试和预览直接
// 拼主题用的；界面上每一帧走的是 `ApplyTheme` → `buildTheme`，那里会读
// `Context.Preferences()`。两处只差这一层，别的算法是同一份。
func BuildTheme(s app.Settings, systemDark bool) *ui.Theme {
	return buildTheme(s, systemDark, ui.Preferences{})
}

// 上游那三个语义色（DESIGN.md §四「颜色与状态」）。
//
// ⚠️ **深浅两套是同一个值**：上游 HeroUI 的 `danger` / `warning` / `success`
// 本身就不分深浅，这里也不分——之前那三档是框架自己的默认（浅色 #dc2626 /
// #d97706 / #16a34a，深色 #ef4444 / #f59e0b / #22c55e），红橙绿都是另一套色相。
//
// ⚠️ **`success` 和主色是两回事**：主色是 Biu 的绿 `#1ed760`（用户能在设置里
// 换），`success` 是 HeroUI 那个绿 `#17c964`（写死的）。两个挨得很近，但用户
// 换主色的时候不该把「下载完成」那一档一起换了。
//
// ⚠️ **这三个色当字色用时，浅色下的对比度是「上游那一档自带的代价」**——
// 按 WCAG 相对亮度算（对着各自主题的页面底色）：
//
//	                改之前        改之后（上游）
//	成功 success   浅 3.30:1  →  浅 2.19:1 ；深 7.78:1 → 深 8.08:1
//	警告 warning   浅 3.19:1  →  浅 2.04:1 ；深 8.25:1 → 深 8.68:1
//	危险 danger    浅 4.83:1  →  浅 4.14:1 ；深 4.71:1 → 深 4.27:1
//
// 也就是说：**浅色下三个都降了（成功和警告降得最狠），深色下成功和警告升了，
// 危险降了一点点**。这是上游 HeroUI 那三个色相自己的性质（上游也是拿它们当
// 字色用），不是实现走样——DESIGN.md §四 指定的就是这一档。
//
// ⚠️ **别拿「对比度不够」当理由把色值改回去**：改回去就是「自己发明了一个
// 上游没有的配色」，那一条是 #163 的整张对照表在盯着的。
//
// ✅ **当字色用时另取一档**（票 #241 / 报告 F-22）：**色相不变，只压深**到
// ≥4.5:1——和主色那一档（`widgets/ink` 的 `AccentOn`）是同一个路数。那一档在
// `internal/ui/semantic.go`（`dangerText` / `warningText` / `successText`），
// 而**主题里这三个值一个字节都没动**（`semantic_test.go` 盯着它们）。
// ⚠️ **当底色**（确认键、Toast）不走那一档——那一档要的是「底上的前景色」。
const (
	semanticDanger  = "#f31260"
	semanticWarning = "#f5a524"
	semanticSuccess = "#17c964"
)

// 分隔线（上游 HeroUI 的 `divider`）。
//
// ⚠️ **深色那档是叠出来的**：`rgba(255,255,255,0.15)`——15% 白叠在**当前的底色**
// 上。底色是默认的 `#18181b` 时叠出来是 `#3b3b3d`（DESIGN.md 注释里写的「约
// #3a3a3b」是同一个数的口头值）。
//
// ⚠️ **不能写成实色**：用户自己挑了底色之后，那道线要叠在**那个**底色上——
// 写死就是「按默认底色算的线压在用户挑的底上」。
//
// ⚠️ 框架默认那一份是 `#d9d9de`（浅）/ `#3f3f46`（深），两个都不是上游的值：
// 深色那个是**实色**（比 15% 白亮得多），浅色那个差两个通道。
const (
	// dividerLight 是浅色下的分隔线：#dbdbdb。
	dividerLight = "#dbdbdb"
	// dividerDarkAlpha 是深色下那道线的浓度：15% 白。
	dividerDarkAlpha = 0.15
)

// dividerColor 是当前这份主题的分隔线颜色。
func dividerColor(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.Background.Mix(ui.RGB(255, 255, 255), dividerDarkAlpha)
	}
	return ui.Hex(dividerLight)
}

// buildTheme 是 BuildTheme 加桌面偏好。
//
// ## 只跟框架 `Theme.follow` 的两样，不跟主色
//
// MyGo 的默认主题在每帧开头会 `t.follow(rt.preferences())`
// （`mygo/ui/runtime.go:464`），里面三件事：系统主色、高对比度、字号缩放。
// 我们从 `LightTheme()` / `DarkTheme()` 起步，那一层就绕开了——所以高对比度
// 和字号缩放得自己接上。
//
// 🔴 **系统主色那一件不能接**（票面明写）。用户要的是 Biu 的绿，主色由她自己
// 在设置里挑；框架那一步会把系统的主色铺上来，用户在系统里换个强调色，我们的
// 按钮跟着变色——那不是我们的配色体系。
//
// 高对比度那几行照 `mygo/ui/preferences.go:69-74` 逐条来（Border / TextMuted /
// Focus / Scrollbar），字号缩放照 `:75-77`（`t.FontSize *= TextScale`）。
func buildTheme(s app.Settings, systemDark bool, p ui.Preferences) *ui.Theme {
	// ⚠️ **深浅由 `effectiveDark` 判**（档位压过系统）——而缓存的键也用它
	// （见 `themeKey`），两边必须一致。
	dark := effectiveDark(s, systemDark)

	var t *ui.Theme
	if dark {
		t = ui.DarkTheme()
	} else {
		t = ui.LightTheme()
	}

	t.Radius = radiusOf(s.BorderRadius)
	// 🔴 字体和圆角一样是**每帧**从设置里读的，所以它在这里装一次就够，别的地方
	// 不用再管。
	//
	// ⚠️ **「系统默认」这一档必须一个字节都不改**。`t.Font` 的零值是「系统自己的
	// 字体」（`ui/theme.go` 里 LightTheme/DarkTheme 都不设它），而设置里默认是
	// `"system-ui"`——那**是另一个字符串**：设上去之后 MyGo 按这个名字去解析字体，
	// 解析到的那一族和「不设」不一定同一个，于是**全应用的字宽都变了**。
	//
	// 实测：把 `t.Font = s.FontFamily` 无条件设上，`internal/ui` 的
	// `TestDragAACToEnd` 在 `-race` 下三次里红两次——播放条上「暂停」那个按钮的位置
	// 变了，`tt.Click("暂停")` 点了个空气（`playbar_aac_test.go:113`）。
	//
	// 「用户选了系统默认」本来就该等于「什么都没变」，所以这里判一下再设。
	if s.FontFamily != "" && s.FontFamily != app.DefaultFontFamily {
		t.Font = s.FontFamily
	}
	if c, ok := parseHexColor(s.BackgroundColor); ok {
		t.Background = c
		// 用户改了底色，**控件那一层要跟着走**：不跟的话白底上的按钮还是
		// 浅灰、深底上的还是深灰，两个主题的「层次」在自定义底色上就断了。
		applyBackground(t)
	}
	// 分隔线：上游 HeroUI 的 `divider`（深色 15% 白叠出、浅色 #dbdbdb）。
	//
	// ⚠️ **放在底色之后**：深色那档是叠在**当前底色**上的，用户挑了底色，
	// 那道线要跟着重算。
	t.Border = dividerColor(t)
	if c, ok := parseHexColor(s.PrimaryColor); ok {
		applyAccent(t, c)
	}

	// 语义色换成上游那三档（DESIGN.md §四）。深浅两套同一个值。
	t.Danger = ui.Hex(semanticDanger)
	t.Warning = ui.Hex(semanticWarning)
	t.Success = ui.Hex(semanticSuccess)

	// 主题方案盖在最后：选了方案就压过上面的底色 / 主色 / 圆角；经典不动。
	// ⚠️ 放在 Inverse 之前——Inverse 取的是 Text / Background / Surface，要取方案的。
	applyPreset(t, s.ThemePreset)

	// 🔴 Inverse / InverseText 留零的话，MyGo 的 inverse() 会退回
	// 「fill=Text、text=Background」——深色下就是**一张白底卡片**，
	// tooltip 和 toast 直接在深色界面里闪一下。默认值不是我们要的，
	// 所以显式给：深色下用比背景略亮的 Surface，浅色下用深色卡片。
	if dark {
		t.Inverse, t.InverseText = t.Surface, t.Text
	} else {
		t.Inverse, t.InverseText = t.Text, t.Background
	}

	// 高对比度（系统「增加对比度」）：照 MyGo `Theme.follow` 那四行。
	// ⚠️ 放在主色和底色之后：它调的是**最终**的 Border / TextMuted。
	if p.HighContrast {
		t.Border = t.Border.Mix(t.Text, 0.45)
		t.TextMuted = t.TextMuted.Mix(t.Text, 0.4)
		t.Focus.A = 255
		t.Scrollbar = t.Scrollbar.Alpha(1.6)
	}

	// 字号缩放（Windows / GNOME 的「文本大小」）：框架那一步就是乘主题字号。
	//
	// ⚠️ 只影响**没显式设 FontSize** 的文字——显式字号是固定 DIP，框架不会
	// 替你乘。我们页面里显式字号走的是 `widgets/scale` 的 `scale.Font`，那一处
	// 乘的是同一个比例。
	if p.TextScale > 0 && p.TextScale != 1 {
		t.FontSize *= p.TextScale
	}

	// 滚动条宽度照上游 `--os-size: 10px`（`biu/src/app.css:56`）。框架默认 6。
	t.ScrollbarWidth = scrollbarWidth
	return t
}

// scrollbarHot 是「指针压在滚动条上」那一档的颜色。
//
// 上游那里是 `--os-handle-bg-hover`（比默认那档深一级，`app.css:58-59`），
// 提示用户「这根能拖」。框架没有第二档颜色，只有 `Theme.Scrollbar` 一个字段
// ——所以由界面层在**每帧开头**按上一帧量到的悬停状态选这一档（见
// `scrollbar.go` 与 `Shell.applyFrameTheme`）。
//
// 做法是把同一份颜色的不透明度提上去（而不是换成另一个灰）：深色下本来就
// 是白色半透明，提不透明度只会更亮/更实，和上游「深一级」是同一个方向。
func scrollbarHot(t *ui.Theme) ui.Color {
	return t.Scrollbar.Alpha(1.6)
}

// applyBackground 按用户挑的底色重推控件那一层：Surface 三档。
//
// ## 比例是从框架那两套默认值反推的
//
// 浅色默认底色 `#ffffff` → Surface `#f4f4f5`（往黑 4.3%）、Hover `#e9e9ec`
// （8.6%）、Pressed `#dddde1`（13.7%）。
// 深色默认 `#18181b` → Surface `#27272a`（往白 6.5%）、Hover `#323236`
// （11.3%）、Pressed `#3c3c41`（15.6%）。
//
// 于是这三档在**默认底色上**推出来的值和框架那套几乎逐字节相同（差 1 个通道
// 的地方是 `Mix` 的舍入），换个底色就整体跟着平移——「控件比底色亮/暗多少」
// 这件事不随底色变。
//
// ⚠️ **描边不在这儿**：分隔线是上游的 `divider`（深色 15% 白叠出、浅色
// #dbdbdb），它自己有一套规矩，见 `dividerColor`。
//
// ⚠️ `Text` / `TextMuted` **不跟着推**：那是票面范围之外（只说「控件的底色和
// 描边」），而且正文色的对比度是用户挑底色时自己要看的事。主色当字色的那一档
// 倒是会跟着算——它每次都是从**当前底色**现推的（`widgets/ink`）。
func applyBackground(t *ui.Theme) {
	black := ui.Color{A: 255}
	white := ui.Color{R: 255, G: 255, B: 255, A: 255}
	mix := white
	if !t.Dark {
		mix = black
	}
	surface, hover, pressed := surfaceStepLight, hoverStepLight, pressedStepLight
	if t.Dark {
		surface, hover, pressed = surfaceStepDark, hoverStepDark, pressedStepDark
	}
	t.Surface = t.Background.Mix(mix, surface)
	t.SurfaceHover = t.Background.Mix(mix, hover)
	t.SurfacePressed = t.Background.Mix(mix, pressed)
}

// 控件那三档的比例（浅色往黑推、深色往白推；推的量是 `applyBackground` 里那
// 一份）。
//
// ⚠️ **别在别处另抄一份**：卡片那一档的悬停要从**卡片自己的底**出发推同样
// 的一档（`themepreset.go` 的 `cardHoverBackground`），推的量就是这里的
// `surfaceStep`——写死颜色、或者另抄一个看起来差不多的数，两处就会对不上，
// 而对不上的症状是「悬停那一档比别处的悬停深一点/浅一点」，没人查得出原因。
const (
	// surfaceStep 是「控件那一层」的比例：浅色 4.3%、深色 6.5%。
	//
	// ⚠️ **这一档就是上游 `content1 → content2` 的差**（浅色 zinc-0 → zinc-100、
	// 深色 zinc-900 → zinc-800，见 `themepreset.go` 的 `cardHoverBackground`）。
	surfaceStepLight float32 = 0.043
	surfaceStepDark  float32 = 0.065
	// hoverStep 是「悬停那一档」的比例：浅色 8.6%、深色 11.3%。
	hoverStepLight float32 = 0.086
	hoverStepDark  float32 = 0.113
	// pressedStep 是「按下那一档」的比例：浅色 13.7%、深色 15.6%。
	pressedStepLight float32 = 0.137
	pressedStepDark  float32 = 0.156
)

// radiusOf 是全局圆角，夹在 0~24。
//
// 上限 24 是 Biu 设置页滑块的右端点，也是圆角再大控件就开始变形的点。
// 上限在这里兜一道，不只靠设置文件那边夹——BuildTheme 也被测试和将来的
// 预览直接调用，那边给的是手搓的结构体。
func radiusOf(v float64) float32 {
	switch {
	case v <= 0:
		return 0
	case v >= app.MaxBorderRadius:
		return float32(app.MaxBorderRadius)
	}
	return float32(v)
}

// applyAccent 把主色铺到整份主题的强调色那一组上。
//
// 做法和 MyGo 自己的 `Theme.follow` 一致（ui/preferences.go:50-78）：主色
// 之外那几个色都是从它推出来的。自己另推一套，两处就会给出不一样的 hover 色。
func applyAccent(t *ui.Theme, a ui.Color) {
	black := ui.Color{A: 255}
	white := ui.Color{R: 255, G: 255, B: 255, A: 255}

	t.Accent = a
	if t.Dark {
		// 深色底上主色偏暗，hover 要往亮走才对得出来。
		t.AccentHover, t.AccentPressed = a.Mix(white, 0.2), a.Mix(black, 0.15)
		t.Selection, t.Focus = a.Alpha(0.4), a.Mix(white, 0.2).Alpha(0.6)
	} else {
		t.AccentHover, t.AccentPressed = a.Mix(black, 0.12), a.Mix(black, 0.25)
		t.Selection, t.Focus = a.Alpha(0.25), a.Alpha(0.55)
	}

	// 主色上的字：白底太亮看不清就反过来用深色。Biu 的绿 (#1ed760)
	// 正好是要走这一支的。
	t.AccentText = white
	if luminance(a) > 165 {
		t.AccentText = ui.Color{R: 24, G: 24, B: 27, A: 255}
	}
}

// luminance 是 sRGB 相对亮度，和 ui.Color 里那个未导出的 gray() 同算法。
//
// 这里重写一份是因为它没导出。本包不能靠反射去戳 ui 的内部状态。
func luminance(c ui.Color) float32 {
	return 0.2126*float32(c.R) + 0.7152*float32(c.G) + 0.0722*float32(c.B)
}

// parseHexColor 解析 #rgb / #rgba / #rrggbb / #rrggbbaa。
//
// 不用 `ui.Hex`：它对不合法的输入**直接 panic**（ui/color.go:33-38）。
// 颜色串是设置文件里的用户输入，为了一个画错的颜色把整个进程带走不划算，
// 这里返回 ok=false 让调用方退回默认值。
func parseHexColor(s string) (ui.Color, bool) {
	h, ok := strings.CutPrefix(strings.TrimSpace(s), "#")
	if !ok {
		return ui.Color{}, false
	}

	// 先拆成 0~15 的单个 hex 位（nibble），再按需要拼。
	// ⚠️ 这一步不能省：直接两位一组 ParseUint 拿到的是**字节**，
	// 再拿它 `v[0]<<4|v[1]` 就成了字节左移 4 位——#1ed760 会变成
	// R=0x1e<<4|0xd7 = 0xf7，一整片红。两种拼法混用是这个坑的全部。
	d, ok := hexDigits(h)
	if !ok {
		return ui.Color{}, false
	}
	switch len(d) {
	case 3, 4: // #rgb / #rgba：每一位翻倍
		c := ui.Color{
			R: d[0]*16 + d[0],
			G: d[1]*16 + d[1],
			B: d[2]*16 + d[2],
			A: 255,
		}
		if len(d) == 4 {
			c.A = d[3]*16 + d[3]
		}
		return c, true
	case 6, 8: // #rrggbb / #rrggbbaa
		c := ui.Color{
			R: d[0]<<4 | d[1],
			G: d[2]<<4 | d[3],
			B: d[4]<<4 | d[5],
			A: 255,
		}
		if len(d) == 8 {
			c.A = d[6]<<4 | d[7]
		}
		return c, true
	}
	return ui.Color{}, false
}

// hexDigits 把 hex 串拆成单个 hex 位（0~15）。位数不对或者有非 hex 字符就 false。
func hexDigits(h string) ([]uint8, bool) {
	if len(h) != 3 && len(h) != 4 && len(h) != 6 && len(h) != 8 {
		return nil, false
	}
	d := make([]uint8, len(h))
	for i := range h {
		n, err := strconv.ParseUint(h[i:i+1], 16, 8)
		if err != nil {
			return nil, false
		}
		d[i] = uint8(n)
	}
	return d, true
}
