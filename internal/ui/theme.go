// 主题：把设置里的深浅档、强调色、封面取色变成 MyGo 每一帧真正用的那份
// 「深夜唱片行」主题（颜色令牌在 `widgets/tok`，见 DESIGN.md §0′）。
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

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/covertint"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
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
	// font 是设置里的字体（空串或系统默认 = 不设）。
	font string
	// accent 是设置里的强调色（朱砂 / 靛青 / 松绿 / 琥珀）。
	accent app.Accent
	// coverHue / coverSat 是封面取色那一对（只在 coverAccent 开着时有值）。
	coverHue, coverSat float64
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
	// fontScale 是设置页的「界面字号」档（紧凑 / 标准 / 大）。
	fontScale app.FontScale
}

func coverHueOf(s app.Settings) float64 {
	if s.CoverAccent {
		return s.CoverHue
	}
	return 0
}

func coverSatOf(s app.Settings) float64 {
	if s.CoverAccent {
		return s.CoverSat
	}
	return 0
}

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
	// card 是「卡片那一档」的三个值（底色 / 圆角 / 悬停底）。
	//
	// ⚠️ **它和主题是同一层的东西**（理由见 `cardPalette` 顶上那一段）：三个值
	// 只依赖**方案**和**深浅**，而那两样都在 `key` 里。于是它跟着主题一起算、
	// 一起缓存——各处去读那一份，而不是**每张卡各去读一遍设置**（票 #632）。
	card cardPalette
	// valid 是「这一份是真的」（零值的那一份是空的）。
	valid bool
}

// of 取出这一帧该用的那一份主题，必要时重算。
//
// ⚠️ **重算只发生在键变了的时候**：设置改了、系统深浅变了、桌面偏好变了——
// 三样都在键里。
func (c *themeCache) of(s app.Settings, systemDark bool, p ui.Preferences) *ui.Theme {
	key := themeKey{
		mode:      s.ThemeMode,
		font:      s.FontFamily,
		accent:    s.Accent,
		coverHue:  coverHueOf(s),
		coverSat:  coverSatOf(s),
		dark:      effectiveDark(s, systemDark),
		contrast:  p.HighContrast,
		textScale: p.TextScale,
		fontScale: s.FontScale,
	}
	if !c.valid || c.key != key {
		base := buildTheme(s, systemDark, p)
		// 热的那一档是**值拷贝**一份：改的是拷贝，缓存里那一份不动。
		hot := *base
		hot.Scrollbar = scrollbarHot(&hot)
		// 卡片那三个值跟主题一起算（理由见 `themeCache.card`）。
		c.card = cardPaletteOf(base)
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
	s.applyFrameThemeMode(c, "")
}

// applyFrameThemeMode 是 `applyFrameTheme` 加一档「这扇窗的深浅固定」：
// force 非空时用它替掉设置里的深浅档（主色、字号、取色照旧跟设置走）。
// 只有刘海窗用（固定黑底，DESIGN §0′.T5）；那扇窗有自己的 `themeCache`，
// 每帧键一样，不会跟主窗互相挤掉缓存。
func (s *Shell) applyFrameThemeMode(c *ui.Context, force app.ThemeMode) {
	systemDark := c.Theme().Dark
	st := s.app.SettingsSnapshot()
	if force != "" {
		st.ThemeMode = force
	}
	if st.CoverAccent {
		st.CoverHue, st.CoverSat = s.tint.sync(s.app.Snapshot().CoverURL, s.wake.invalidate)
	}
	scale.SetUser(st.FontScale.Factor())
	t := s.theme.of(st, systemDark, c.Preferences())
	// 指针压在滚动条上时深一档（上游 `--os-handle-bg-hover`）。框架的绘制
	// 只在**指针落在容器上**时画滚动条，所以这里只管「出现之后更明显」。
	//
	// ⚠️ 读的是**上一帧**量到的状态（`scrollbarHot`）：这一帧的容器还没建出来，
	// 问不到。指针一移动就会有一帧，所以看上去是「移上去就变深」，不会晚。
	if s.scrollbarHot {
		t = s.theme.hot()
	}
	c.SetTheme(t)
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

// buildTheme 是 BuildTheme 加桌面偏好。
//
// ## 从哪儿来
//
// 从 `ui.LightTheme()` / `ui.DarkTheme()` 起步（MyGo 新加的字段跟着框架的默认值走），
// 再把「深夜唱片行」的令牌（`tok.Base`）逐项盖上去：
//
//	Background = bg            Surface = surface-2（控件那一层：搜索框、Chip、输入框）
//	SurfaceHover / Pressed = surface-2 叠 hover / press
//	Border = line              Text / TextMuted = text / text-2
//	Accent = accent            AccentText = accent-ink（主色底上的字）
//	AccentHover = accent 往 text 推 12%（原型 `color-mix(accent 88%, text)`）
//	Focus = focus              Danger / Warning / Success = err / warn / ok
//	Inverse = text、InverseText = bg（Toast 反色胶囊，§0′ 冲突 7 按原型）
//	Radius = 8                 Scrollbar = line-strong
//
// 设置里的 `borderRadius` 不再进主题：原型的圆角是固定的 4 / 8 / 12 / 胶囊。
//
// ## 只跟框架 `Theme.follow` 的两样，不跟主色
//
// 高对比度和字号缩放照 `mygo/ui/preferences.go` 接上；🔴 **系统主色那一件不接**：
// 强调色由用户在四种里挑，不跟系统的强调色走。
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

	// ⚠️ **「系统默认」这一档必须一个字节都不改**：`t.Font` 的零值是「系统自己的
	// 字体」，而设置里默认是 `"system-ui"`——那是另一个字符串，设上去之后全应用的
	// 字宽都会变（`TestDragAACToEnd` 实测踩过）。
	if s.FontFamily != "" && s.FontFamily != app.DefaultFontFamily {
		t.Font = s.FontFamily
	}

	pal := tok.Base(dark, string(s.Accent))
	t.Background = pal.Bg
	t.Surface = pal.Surface2
	t.SurfaceHover = composite(pal.Surface2, pal.Hover)
	t.SurfacePressed = composite(pal.Surface2, pal.Press)
	t.Border = pal.Line
	t.Text, t.TextMuted = pal.Text, pal.Text2
	t.Danger, t.Warning, t.Success = pal.Err, pal.Warn, pal.Ok
	t.Radius = radiusMd
	t.Scrollbar = pal.LineStrong

	applyAccent(t, pal.Accent)
	t.AccentText = pal.AccentInk
	t.Focus = pal.Focus

	// 封面取色盖在强调色之后：开着且取到了颜色，主色换成封面的色相（亮度按深浅定），
	// 底上字按亮度挑黑白（`applyAccent`），焦点环跟着主色走。
	if s.CoverAccent && s.CoverSat > 0 {
		if c, ok := parseHexColor(covertint.Hue{H: s.CoverHue, S: s.CoverSat}.Color(dark)); ok {
			applyAccent(t, c)
			t.Focus = c
		}
	}

	// Toast / 提示是反色胶囊（原型 `.toast { background: var(--text); color: var(--bg) }`）。
	// 🔴 必须显式给：留零的话 MyGo 会自己推一份。
	t.Inverse, t.InverseText = pal.Text, pal.Bg

	// 高对比度（系统「增加对比度」）：照 MyGo `Theme.follow` 那四行。
	if p.HighContrast {
		t.Border = t.Border.Mix(t.Text, 0.45)
		t.TextMuted = t.TextMuted.Mix(t.Text, 0.4)
		t.Focus.A = 255
		t.Scrollbar = t.Scrollbar.Alpha(1.6)
	}

	// 字号缩放（Windows / GNOME 的「文本大小」）：只影响没显式设字号的文字；显式
	// 字号走 `widgets/scale` 的 `scale.Font`，乘的是同一个比例。
	if p.TextScale > 0 && p.TextScale != 1 {
		t.FontSize *= p.TextScale
	}
	// 设置页的「界面字号」档（和系统缩放相乘；显式字号走 `scale.Font`，同一个乘子）。
	if f := s.FontScale.Factor(); f != 1 {
		t.FontSize *= f
	}

	// 滚动条宽度照上游 `--os-size: 10px`。框架默认 6。
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
// 「深夜唱片行」的滚动条是不透明的 `--line-strong`，提不透明度不起作用，所以
// 悬停那一档往次要字色推一半：深色下更亮、浅色下更深，都是「更显眼」。
func scrollbarHot(t *ui.Theme) ui.Color {
	return t.Scrollbar.Mix(t.TextMuted, 0.5)
}

// applyAccent 把主色铺到整份主题的强调色那一组上。
//
// 做法和 MyGo 自己的 `Theme.follow` 一致（ui/preferences.go:50-78）：主色
// 之外那几个色都是从它推出来的。自己另推一套，两处就会给出不一样的 hover 色。
func applyAccent(t *ui.Theme, a ui.Color) {
	black := ui.Color{A: 255}
	white := ui.Color{R: 255, G: 255, B: 255, A: 255}

	t.Accent = a
	// MyGo fades Accent when the window is inactive unless this is set explicitly.
	// Keep the product accent stable across focus changes.
	t.InactiveAccent = a
	// 主按钮悬停：主色 88% 混正文色（原型 index.html:283
	// `color-mix(in srgb, var(--accent) 88%, var(--text))`），深浅一样，和 `pud.Button` 的 Primary
	// 同一个算法——以前深色下是混白 20%，框架控件和 pud 主键悬停时两种颜色。
	// ⚠️ 调用前 `t.Text` 必须已经是这一套的正文色（`BuildTheme` 先铺令牌再调这里）。
	t.AccentHover = a.Mix(t.Text, 0.12)
	if t.Dark {
		t.AccentPressed = a.Mix(black, 0.15)
		t.Selection, t.Focus = a.Alpha(0.4), a.Mix(white, 0.2).Alpha(0.6)
	} else {
		t.AccentPressed = a.Mix(black, 0.25)
		t.Selection, t.Focus = a.Alpha(0.25), a.Alpha(0.55)
	}

	// 主色上的字：挑黑或白里**对比更高的那一个**（WCAG 相对亮度，pivot 0.179
	// 是「黑白两个字对比度相等」的那个点）。Biu 的绿 (#1ed760) 正好是要走深字
	// 那一支的。
	//
	// 🔴 **判据必须是 WCAG 亮度，不是 sRGB 加权和**（评审抓到的）：加权和在
	// 中间调的饱和色上会挑错——`#10c8ff`（亮青）加权值 152 ≤ 165 走白字，而
	// 白字压上去只有 **1.96:1**；WCAG 亮度 0.44 > 0.179 走深字。这一档是
	// 「主色底上的字」，它的职责就是**在主色底上读得出来**（用户在设置里可以
	// 把主色换成任何颜色，写死的阈值保证不了这件事）。
	t.AccentText = white
	if ink.Luminance(a) > ink.PivotLuminance {
		t.AccentText = ui.Color{R: 24, G: 24, B: 27, A: 255}
	}
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
