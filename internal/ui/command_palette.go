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
	"strings"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
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
	paletteWidth = 480
	// paletteTop 是面板顶边离窗口顶边多远：命令面板的惯例位置是「顶上
	// 约 1/6 处」（VS Code / Raycast 都在那一带），96 在 560 高的最小
	// 窗口上正好是那个比例。
	paletteTop = 96
	// paletteListMaxH 是命令列表的高度上限，超了滚。
	paletteListMaxH = 320

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
}

// paletteEntries 是这一帧面板上可摆的条目，按框里的词过滤过。
//
// ⚠️ **比对的是小写 + 去空白**：用户敲 "toggle play"、"TOGGLEPLAY" 都
// 是同一条意图；中文名原样比（`strings.Contains` 对中文本来就是逐字）。
func paletteEntries(query string) []paletteEntry {
	var all []paletteEntry
	for _, c := range commandSpecs {
		keys := c.id + " " + c.name
		if _, it, ok := findShortcutInDefaults(c.id); ok && it.Shortcut != "" {
			keys += " " + it.Shortcut + " " + formatAccel(accelerator(it.Shortcut))
		}
		all = append(all, paletteEntry{
			label: c.name,
			voice: "命令：" + c.name,
			id:    c.id,
			keys:  keys,
		})
	}
	for _, r := range routes {
		if !r.InMenu {
			continue
		}
		all = append(all, paletteEntry{
			label: "去" + r.Title,
			voice: "去" + r.Title,
			nav:   r.Match,
			// 用户敲的是「去设置」，而 keys 里也要有「去」——否则搜「去」
			// 一个字都搜不到页面类，只有命令类会剩下。
			keys: "去" + r.Title + " " + r.Title + " " + r.Match,
		})
	}
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

// runPaletteEntry 跑面板里选中的那一条。跑完收面板——命令面板的语义是
// 「发一条命令就退场」，跑完还开着的话用户要再多按一次 Esc。
func (s *Shell) runPaletteEntry(e paletteEntry) {
	s.paletteOpen = false
	if e.nav != "" {
		s.navigate(e.nav)
		return
	}
	s.onShortcut(e.id)
}

// viewCommandPalette 画命令面板。没开就一个字都不建。
//
// ⚠️ 排在满屏之后、Toast 之前（`viewMain` 里调）：它是模态的，要盖住整页，
// 而 Toast（右下角那句提示）要在最上面。
func (s *Shell) viewCommandPalette(c *ui.Context) {
	if !s.paletteOpen {
		// 面板没了：下一回它再出现的时候，那一下又是「进场」（理由同
		// `viewSuggest` 里那一支）。
		s.paletteShown = false
		return
	}
	t := c.Theme()
	_, winH := c.Size()

	justOpened := !s.paletteShown
	s.paletteShown = true

	ui.Overlay(c, func() {
		// ⚠️ **蒙版是模态的**（理由同 `queue_drawer.go` 那一段）：焦点关进
		// 面板、底下那一页点不动、底下的快捷键让路。半透明的那一层底让
		// 「底下还有一页」看得见——命令面板是叠上去的一小块，不是另一页。
		back := ui.Row(c).Key("palette-backdrop").
			Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Background(ui.RGBA(0, 0, 0, 0.32)).
			// ⚠️ **水平居中靠 Row 的主轴**，不是靠绝对定位的百分比 + 负边距
			//（那套在这个布局里没有「负边距把面板拉回中线」的语义——布局按
			// inset 摆，LeftPercent 只是左沿）。`Justify(Center)` 让面板中线
			// 落在窗口中线（VS Code 那一档的位置）。
			Justify(ui.Center).
			Modal()
		back.Children(func() {
			panel := popupPanelCard(s, t, ui.Column(c).Key("command-palette").
				Width(paletteWidth).
				// 离顶 96 DIP、横向由 Row 居中（见上面 Justify）。
				Margin(paletteTop, space.None, space.None, space.None))
			panel.Role(ui.RoleDialog).Label(paletteLabel)

			// 进场：淡入 + 升上来 10 DIP（和联想面板同一段，理由见那里）。
			if justOpened {
				panel.Jump(paletteEnterFadeKey, 0)
				panel.Jump(paletteEnterRiseKey, suggestEnterRise)
			}
			fade := panel.AnimateTo(paletteEnterFadeKey, 1, paletteEnterAnim)
			rise := panel.AnimateTo(paletteEnterRiseKey, 0, paletteEnterAnim)
			panel = panel.Opacity(fade).Margin(paletteTop+rise, space.None, space.None, space.None)

			entries := paletteEntries(s.paletteQuery)

			// 🔴 **选中态每帧校验**：词变了列表就换，`sel` 指的下标可能越界，
			// 越了就放回「还没选中」——回车跑第一条（见下面 Enter 那一支）。
			if s.paletteSel >= len(entries) {
				s.paletteSel = -1
			}

			panel.Children(func() {
				s.viewPaletteInput(c, entries)
				s.viewPaletteList(c, t, entries, winH)
			})
		})

		// 关面板的两条路：点底下那一层、按 Esc。理由同 `viewQueueDrawer`
		// 里那一段（用 `back.Clicked()` 不用 `panel.PressedOutside()`）。
		//
		// ⚠️ **Esc 走 OverlayShortcut，不走输入框的 OnTextKey**：输入框的
		// `OnTextKey` 在它自己不收这个键的时候才轮到（`TextInputBase` 对 Esc
		// 默认是清字，见 `navbar_search.go` 的文件头）——所以输入框那一支
		// 先问「面板开着没有」，开着就吃掉这次 Esc 关面板；而焦点不在输入框
		// 上（比如用户点了一行）时，这一支兜底。
		if back.Clicked() || back.OverlayShortcut(0, ui.KeyEscape) {
			s.paletteOpen = false
		}
	})
}

// viewPaletteInput 是面板顶上那个输入框。
//
// ⚠️ **开面板的那一帧把焦点按在它身上**：打开面板就是为了敲字，焦点不
// 在框上的话用户要先点一下才能敲——「按 ⌘K 再点一下框」就是把一个键
// 变成两下。
func (s *Shell) viewPaletteInput(c *ui.Context, entries []paletteEntry) {
	field := ui.TextInputBase(c, &s.paletteQuery).
		Label(paletteInputLabel).Placeholder("输入命令或页面名").
		FillWidth().Padding(space.Px8, space.Px8)
	field.Focus()
	field.OnTextKey(func(k ui.TextKey) bool {
		// Esc 收面板：开着就吃掉这一次（字留着——用户只是想关，不是想删
		// 刚敲的那几个字；真删字的工具是 Backspace）。
		if k.Key == ui.KeyEscape {
			if !s.paletteOpen {
				return false
			}
			s.paletteOpen = false
			return true
		}
		// 🔴 **带修饰键的方向键/回车还给输入框**（理由同 `navbar_search.go`
		// 那一支：`Shift+↓` 是扩选，`Cmd+↓` 是光标到末尾）。
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
			// 没选中时 ↑ 从底下最后一条开始（和联想面板同一个惯例）。
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
			// 没选中任何一条就默认第一条——「敲了词按回车」的语义是「跑
			// 最像的那一条」（Spotlight / VS Code 都是第一条）。
			sel := s.paletteSel
			if sel < 0 || sel >= n {
				sel = 0
			}
			s.runPaletteEntry(entries[sel])
			return true
		}
		return false
	})
	// ⚠️ **敲字就把键盘选中清掉**：词变了那一摊条目要换，留着那个高亮
	// 会指着一条已经不在列表里的命令（理由同 `navbar_search.go` 的
	// `field.Changed()` 那一支）。
	if field.Changed() {
		s.paletteSel = -1
	}
}

// viewPaletteList 是输入框下面那一列命令。
//
// ⚠️ **能滚**：12 条命令 + 7 条页面在小窗口上可能超 `paletteListMaxH`，
// 和联想面板同一条规矩（`navbar_search.go` 的 suggestScroll）。
func (s *Shell) viewPaletteList(c *ui.Context, t *ui.Theme, entries []paletteEntry, winH float32) {
	maxH := float32(paletteListMaxH)
	if winH > 0 && maxH > winH-paletteTop-160 {
		// 面板底下还要留输入框和边距：列表不许把面板顶出窗口底。
		maxH = winH - paletteTop - 160
	}
	scroll := ui.Scroll(c).Key("palette-list").FillWidth().MaxHeight(maxH).
		TrackScroll(&s.paletteScroll)
	scroll.Children(func() {
		if len(entries) == 0 {
			ui.Text(c, paletteEmptyNote).FontSize(scale.Font(c, scale.Size13)).
				TextColor(t.TextMuted).Padding(space.Px8, space.Px8)
			return
		}
		ui.Column(c).FillWidth().Gap(space.Px2).Children(func() {
			for i, e := range entries {
				s.viewPaletteRow(c, t, e, i)
			}
		})
	})
}

// viewPaletteRow 是列表里的一行。
//
// ⚠️ **选中底和悬停底是同一片**（理由同 `viewSuggestTerm`）：选中态不是
// 新颜色，用户从「指针扫过会亮」已经知道这一片亮是「这一条是焦点」。
func (s *Shell) viewPaletteRow(c *ui.Context, t *ui.Theme, e paletteEntry, idx int) {
	entry := e
	row := ui.ButtonBase(c).Label(entry.voice).
		FillWidth().Radius(t.Radius).
		Padding(space.Px8, space.Px8).AlignItems(ui.Center)
	picked := s.paletteSel == idx
	if row.Hovered() || picked {
		row = row.Background(t.SurfaceHover)
	}
	if picked {
		row.ScrollIntoView()
	}
	row.Children(func() {
		ui.Text(c, entry.label).FontSize(scale.Font(c, scale.Size14)).Grow(1).MaxLines(1).Ellipsis("…")
		// 命令类的右边摆它现在的键：「这条命令对应哪个键」是面板上用户
		// 最常问的问题，顺手答了省得再去设置页翻。
		if entry.id != "" {
			if _, it, ok := findShortcutInDefaults(entry.id); ok && it.Shortcut != "" {
				ui.Text(c, formatAccel(accelerator(it.Shortcut))).
					FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)
			}
		}
	})
	if row.Clicked() {
		s.runPaletteEntry(entry)
	}
}
