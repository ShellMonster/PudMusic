// 命令面板（⌘K）：一个浮在窗口顶上的可搜索命令列表。
//
// # 为什么要有这一层
//
// 命令表（`commands.go`）里的命令只有「键」和「菜单」两个入口：记住 ⌘P 是
// 暂停、⌘← 是上一首是用户的负担，而菜单要抬手去点。命令面板是第三个入口：
// 一个键（⌘K）打开一个框，敲几个字、回车——「切换播放模式」这种一年用几次
// 的命令从此不用记键。
//
// # 骨架照队列抽屉和联想浮层各取一半
//
//   - **浮层形状照 `queue_drawer.go`**：`ui.Overlay` + `Absolute`，蒙版
//     `Modal()` 把焦点关进面板、底下那页点不动；点外面 / Esc 关掉。
//   - **输入框 + ↑↓ 导航照 `navbar_search.go`**：`TextInputBase` +
//     `OnTextKey`——Esc 给浮层（收面板），↑↓ 在列表里走，回车跑选中的
//     那一条。`SearchField` 不能用：它的 Esc 被自己吃掉清字，浮层就收不到
//     （理由同 `navbar_search.go` 的文件头）。
//   - **进场照 `viewSuggest`**：淡入 + 升上来 10 DIP、200ms +
//     `motion.Unified`——只播进场，退场瞬消（理由同联想面板：那是
//     「用户正盯着它」的浮层，不是高频弹窗）。
//
// # 列表里摆什么
//
// 两类，摊平成一条：
//
//	命令类    `commandSpecs` 里那十几条（播放/暂停、收藏当前歌曲…）——
//	          动作走 `s.onShortcut(id)`，和菜单项、快捷键是同一份实现。
//	页面类    `routes` 里 `InMenu` 的那几条（去首页 / 去设置 / 去本地
//	          音乐…）——动作走 `s.navigate(match)`，和侧栏那一项同一条路。
//
// 两类各带前缀「命令：」/「去」写进读屏名，于是「播放/暂停」和「去设置」
// 在读屏软件里说得清是两件事。
package ui

import (
	"strconv"
	"strings"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scrim"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shadow"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

const (
	// paletteLabel 是整个面板给读屏软件和无头 Tester 的名字。
	paletteLabel = "命令面板"
	// paletteInputLabel 是顶上那个输入框的名字。
	paletteInputLabel = "搜索命令"
	// paletteEmptyNote 是一个命令都没搜到的时候说的那句话。
	paletteEmptyNote = "没有这条命令"

	// paletteWidth 是面板的宽。命令名最长也就「显示/隐藏迷你播放器」十个字，
	// 480 摆得开、在 900 的最小窗口上也不顶边。
	paletteWidth = 600
	// paletteTop 是面板顶边离窗口顶边多远：命令面板的惯例位置是「顶上
	// 约 1/6 处」（VS Code / Raycast 都在那一带），Px96（96 DIP）在 560
	// 高的最小窗口上正好是那个比例——值在 space 那张表上，这里不另写。
	paletteTop = space.Px96
	// paletteListMaxH 是命令列表的高度上限，超了滚。
	paletteListMaxH = 360

	// paletteEnterFadeKey / paletteEnterRiseKey 是进场动画的两个通道（常量
	// 字符串，理由同 queueSlideKey）。
	paletteEnterFadeKey = "palette:enter-fade"
	paletteEnterRiseKey = "palette:enter-rise"
)

// paletteEnterAnim 是进场那一段用的动画。**包级变量**（理由同
// `suggestEnterAnim`：`motion.Unified` 是闭包，每帧现造等于每帧一个分配）。
var paletteEnterAnim = ui.Animation{Duration: suggestEnterDuration, Ease: motion.Unified}

// paletteEntry 是面板里的一条：显示名 + 读屏名 + 要跑的命令 id。
//
// ⚠️ **动作不在这一层**：页面类记 `nav` 前缀 + 目标路由，命令类记命令 id；
// 跑的时候统一走 `runPaletteEntry`——那一条知道「页面类 navigate、命令类
// onShortcut」。
type paletteEntry struct {
	// label 是界面上那一行显示的字。
	label string
	// voice 是读屏软件念的那一句（带「命令：」/「去」前缀，和显示名分开
	// 两件事：显示名是给眼睛扫的，念出来要听得清是哪一类）。
	voice string
	// id 是命令类条目的命令 id（进 `s.onShortcut`），nav 是页面类条目的
	// 目标路由（进 `s.navigate`）。
	id, nav string
	// keys 是搜的时候也比对的字：命令的 id 和快捷键。用户敲 "toggleplay"
	// 或者 "⌘P" 都该找得到那一条。
	keys string
	// group 是分组名（原型 `PAL_ACTIONS` 的 `g`：「前往」「操作」），icon 是行首 16 号的图标名，
	// run 非空时选中就执行它（不走 nav / id）。
	group, icon string
	run         func(*Shell)
}

func paletteEntries(s *Shell, query string) []paletteEntry {
	var nav, ops []paletteEntry
	loggedIn := s != nil && s.app.LoggedIn()
	for _, r := range routes {
		if !r.InMenu || (r.HideInMenuWhenLoggedOut && !loggedIn) {
			continue
		}
		nav = append(nav, paletteEntry{
			label: r.Title, voice: "去" + r.Title, nav: r.Match,
			keys:  "去" + r.Title + " " + r.Title + " " + r.Match,
			group: "前往", icon: strings.TrimSuffix(strings.TrimPrefix(r.Icon, "Pud/"), ".svg"),
		})
	}
	// 原型：登录后多一条「用户动态」，没登录多一条「扫码登录」。
	if loggedIn {
		nav = append(nav, paletteEntry{label: "用户动态", voice: "去用户动态", nav: dynamicFeedPath,
			keys: "用户动态 动态 " + dynamicFeedPath, group: "前往", icon: "bell"})
	} else {
		nav = append(nav, paletteEntry{label: "扫码登录", voice: "去扫码登录", nav: loginPath,
			keys: "扫码登录 登录 " + loginPath, group: "前往", icon: "user"})
	}
	for _, c := range commandSpecs {
		keys := c.id + " " + c.name
		if _, it, ok := findShortcutInDefaults(c.id); ok && it.Shortcut != "" {
			keys += " " + it.Shortcut + " " + formatAccel(accelerator(it.Shortcut))
		}
		ops = append(ops, paletteEntry{
			label: c.name, voice: "命令：" + c.name, id: c.id, keys: keys,
			group: "操作", icon: paletteCommandIcon(c.id),
		})
	}
	dark := s != nil && s.theme.base != nil && s.theme.base.Dark
	themeLabel, themeIcon := "切换到深色主题", "moon"
	if dark {
		themeLabel, themeIcon = "切换到浅色主题", "sun"
	}
	ops = append(ops, paletteEntry{
		label: themeLabel, voice: "命令：" + themeLabel, keys: "主题 深色 浅色 theme",
		group: "操作", icon: themeIcon,
		run: func(s *Shell) { s.toggleDarkLight(dark) },
	})
	all := append(nav, ops...)
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return all
	}
	q = strings.ReplaceAll(q, " ", "")
	var out []paletteEntry
	for _, e := range all {
		hay := strings.ToLower(strings.ReplaceAll(e.keys, " ", ""))
		if strings.Contains(hay, q) {
			out = append(out, e)
		}
	}
	return out
}

// paletteCommandIcon 是命令类条目行首的图标（取原型 `PAL_ACTIONS` 里同类动作的图标）。
func paletteCommandIcon(id string) string {
	switch id {
	case "togglePlay":
		return "play"
	case "prev":
		return "prev"
	case "next":
		return "next"
	case "toggleMiniMode", "toggleNotchMode", "toggleMainWindow":
		return "mini"
	case "toggleQueueDrawer":
		return "queue"
	case "favoriteCurrent":
		return "star"
	case "cyclePlayMode":
		return "repeat"
	case "volumeUp", "volumeDown":
		return "vol"
	case "commandPalette":
		return "cmd"
	}
	return "play"
}

// findShortcutInDefaults 按 id 查默认的应用内键。面板把默认键写进可搜的
// 字里（⌘P → 播放/暂停）；用户改过的键不在这里——面板是「现在有什么命令」，
// 不是「键是什么」那一层（那是设置页的事）。
func findShortcutInDefaults(id string) (int, shortcutItem, bool) {
	for i, it := range defaultShortcutSettings().Shortcuts {
		if it.ID == id {
			return i, it, true
		}
	}
	return 0, shortcutItem{}, false
}

// toggleCommandPalette 是「命令面板」那条命令做的事：开 / 关。
//
// 开的时候把上一场收干净：词清掉、选中复位——上一场留下来的词在下一场
// 是「我上次敲到一半」的残留，而命令面板是「我现在要做一件事」的入口，
// 上一场的事和这一场无关（Spotlight / VS Code 的 ⌘⇧P 也都是这么收的）。
func (s *Shell) toggleCommandPalette() {
	if !s.paletteOpen {
		s.paletteQuery = ""
		s.paletteSel = -1
		s.paletteScroll = ui.ScrollState{}
	}
	s.paletteOpen = !s.paletteOpen
}

func (s *Shell) runPaletteEntry(e paletteEntry) {
	s.paletteOpen = false
	switch {
	case e.run != nil:
		e.run(s)
	case e.nav != "":
		s.navigate(e.nav)
	default:
		s.onShortcut(e.id)
	}
}

func (s *Shell) viewCommandPalette(c *ui.Context) {
	if !s.paletteOpen {
		s.paletteShown = false
		return
	}
	_, winH := c.Size()
	winW, _ := c.Size()
	s.paletteShown = true

	ui.Overlay(c, func() {
		// 原型 `dialog::backdrop`：50% 黑的遮罩，面板离顶 12vh、水平居中。
		back := ui.Row(c).Key("palette-backdrop").
			Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Background(ui.RGBA(0, 0, 0, scrim.Dialog)).
			Justify(ui.Center).
			Modal().Transition(motion.ScrimIn)
		back.Children(func() {
			p := tok.Of(c.Theme())
			w := min(paletteWidth, winW-32)
			panel := shadow.Pop(ui.Column(c).Key("command-palette").Width(w).
				Margin(winH*0.12, space.None, space.None, space.None).
				Background(p.Surface).Border(1, p.Line).Radius(tok.R12).
				Transition(motion.PopIn), c.Theme())
			panel.Role(ui.RoleDialog).Label(paletteLabel)

			entries := paletteEntries(s, s.paletteQuery)
			if s.paletteSel >= len(entries) {
				s.paletteSel = -1
			}
			panel.Children(func() {
				s.viewPaletteInput(c, entries)
				s.viewPaletteList(c, entries, winH)
			})
		})

		if back.Clicked() || back.OverlayShortcut(0, ui.KeyEscape) {
			s.paletteOpen = false
		}
	})
}

// viewPaletteInput 是面板顶上的输入行（原型 `.pal-input`：内边距 16/20、缝 12、放大镜 18 text-3、
// 输入 16 号，底下一条 `line`）。键盘：↑↓ 选、回车执行、Esc 关。
func (s *Shell) viewPaletteInput(c *ui.Context, entries []paletteEntry) {
	p := tok.Of(c.Theme())
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(space.Px12).
		Padding(space.Px16, space.Px20).
		Draw(func(pp *ui.Painter, r ui.Rect) {
			pp.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 1, W: r.W, H: 1}, p.Line, 0)
		}).Children(func() {
		ui.Icon(c, pud.Icon("search")).FontSize(tok.Icon).TextColor(p.Text3).Shrink(0)
		s.viewPaletteField(c, entries)
	})
}

func (s *Shell) viewPaletteField(c *ui.Context, entries []paletteEntry) {
	field := ui.TextInputBase(c, &s.paletteQuery).
		Label(paletteInputLabel).Placeholder("输入命令或页面名").
		Grow(1).FontSize(scale.Font(c, scale.Size16))
	field.Focus()
	field.OnTextKey(func(k ui.TextKey) bool {
		if k.Key == ui.KeyEscape {
			if !s.paletteOpen {
				return false
			}
			s.paletteOpen = false
			return true
		}
		if k.Mods != 0 {
			return false
		}
		n := len(entries)
		switch k.Key {
		case ui.KeyDown:
			if n == 0 {
				return false
			}
			s.paletteSel = (s.paletteSel + 1 + n) % n
			return true
		case ui.KeyUp:
			if n == 0 {
				return false
			}
			if s.paletteSel < 0 {
				s.paletteSel = n - 1
			} else {
				s.paletteSel = (s.paletteSel - 1 + n) % n
			}
			return true
		case ui.KeyEnter:
			if n == 0 {
				return true // 面板是模态的，回车不许漏给底下那一页
			}
			sel := s.paletteSel
			if sel < 0 || sel >= n {
				sel = 0
			}
			s.runPaletteEntry(entries[sel])
			return true
		}
		return false
	})
	if field.Changed() {
		s.paletteSel = -1
	}
}

// viewPaletteList 是命令列表（原型 `.pal-list`：最高 360、内边距 8、分组小标题 12 号 text-3）。
func (s *Shell) viewPaletteList(c *ui.Context, entries []paletteEntry, winH float32) {
	p := tok.Of(c.Theme())
	maxH := float32(paletteListMaxH)
	if winH > 0 && maxH > winH-winH*0.12-120 {
		maxH = winH - winH*0.12 - 120
	}
	scroll := ui.Scroll(c).Key("palette-list").FillWidth().MaxHeight(maxH).
		TrackScroll(&s.paletteScroll)
	scroll.Children(func() {
		ui.Column(c).FillWidth().Padding(space.Px8).Children(func() {
			if len(entries) == 0 {
				ui.Text(c, paletteEmptyNote).FontSize(scale.Font(c, scale.Size14)).
					TextColor(p.Text2).TextAlign(ui.Center).FillWidth().Padding(space.Px24)
				return
			}
			last := ""
			for i, e := range entries {
				if e.group != last {
					last = e.group
					ui.Text(c, e.group).FontSize(scale.Font(c, scale.Size12)).TextColor(p.Text3).
						Padding(space.Px8, space.Px12, space.Px4, space.Px12)
				}
				s.viewPaletteRow(c, e, i)
			}
		})
	})
}

// viewPaletteRow 是一行（原型 `.pal-item`：内边距 10/12、缝 12、圆角 8、图标 16 text-2，右边 kbd；
// 选中行 `accent-soft` 底 + 图标 accent-text）。
func (s *Shell) viewPaletteRow(c *ui.Context, e paletteEntry, idx int) {
	p := tok.Of(c.Theme())
	entry := e
	row := ui.ButtonBase(c).Key("pal:"+strconv.Itoa(idx)).Label(entry.voice).
		FillWidth().Radius(tok.R8).Padding(space.Px10, space.Px12).Gap(space.Px12).AlignItems(ui.Center)
	picked := s.paletteSel == idx
	iconCol := p.Text2
	switch {
	case picked:
		row = row.Background(p.AccentSoft)
		iconCol = p.AccentText
		row.ScrollIntoView()
	case row.Hovered():
		row = row.Background(p.Hover)
	}
	row.Children(func() {
		ui.Icon(c, pud.Icon(entry.icon)).FontSize(tok.IconSm).TextColor(iconCol).Shrink(0)
		ui.Text(c, entry.label).FontSize(scale.Font(c, scale.Size14)).Grow(1).MaxLines(1).Ellipsis("…")
		if entry.id != "" {
			if _, it, ok := findShortcutInDefaults(entry.id); ok && it.Shortcut != "" {
				pud.Kbd(c, formatAccel(accelerator(it.Shortcut)))
			}
		}
	})
	if row.Clicked() {
		s.runPaletteEntry(entry)
	}
}
