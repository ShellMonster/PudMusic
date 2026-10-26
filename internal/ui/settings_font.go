// 设置页的字体选择：Biu 的 `src/components/font-select`。
//
// ## 每一条用它自己的字体画预览
//
// 这是这一票明写的一条验收（C-48）。做法是 `ui.Text(...).Font(family)`：MyGo 的
// `Font` 收一个**逗号分隔的族名列表**，系统有的那一个画字、没有的往后找
// （`ui/element.go:1044`），所以「这台机器上没有的字体」画出来是回退后的那个——
// 和 Biu 的 CSS `fontFamily` 一样，用户看到的就是他真会拿到的样子。
//
// 用 `ui.Select`（普通下拉）摆不了这个：它的选项是纯字符串，画出来一律是当前
// 字体。所以这里用 `ui.SelectBase` 自己拼一个面板，选项里画自己的字。
//
// ## 列表是问系统要来的
//
// Biu 那个下拉里装的是 `window.electron.getFonts()` 报上来的系统字体表；对应
// 的入口是 MyGo 的 `ui.FontFamilies()`（`mygo/ui/window.go:66`），列举这台机器
// 上文字能用的全部字体族。**不是一张挑好的名单**——早先那份注释写着「MyGo
// 没有这个 API」，那是错的（票 #139 改掉）。
//
// 系统默认那一条仍用 `system-ui`，那是 MyGo 认得的系统自己的字体
// （`Font` 的注释里点名的两个之一）。
//
// ⚠️ **枚举要缓存**：`FontFamilies()` 第一次会问系统要一遍（几百个字体的 Mac
// 上是几十毫秒），而且每次都复制一份切片。设置页每一帧都要这份名单，所以这里
// 用 `sync.Once` 只取一次；框架自己也会缓存，但复制那一层躲不掉。
package ui

import (
	"strings"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// fontOption 是一条字体选项。family 直接存进 `app.Settings.FontFamily`。
type fontOption struct {
	label  string
	family string
}

// systemFontFamilies 是系统字体族的名字，只问一次。
//
// 枚举失败（或者这一版框架在不支持的平台上给空）时就是空表：下面只剩「系统
// 默认」一条，设置仍然能用——比一页空白好。
var (
	fontFamiliesOnce sync.Once
	fontFamilyNames  []string
)

func systemFontFamilies() []string {
	fontFamiliesOnce.Do(func() {
		for _, f := range ui.FontFamilies() {
			name := strings.TrimSpace(f.Name)
			if name == "" {
				continue
			}
			fontFamilyNames = append(fontFamilyNames, name)
		}
	})
	return fontFamilyNames
}

// fontOptionList 是下拉里那一份名单：先是「系统默认」，再是系统字体。
//
// ⚠️ **`system-ui` 要排掉**：它是「默认」那一条存的值，系统枚举里也可能出现
// 同名的一项——同一个值出现两次的话 `indexOfString` 只认第一个，用户点第二条
// 得到的还是第一条，看上去像「点了没反应」。
func fontOptionList() []fontOption {
	opts := []fontOption{{label: "系统默认", family: systemFontFamily}}
	for _, name := range systemFontFamilies() {
		if strings.EqualFold(name, systemFontFamily) {
			continue
		}
		opts = append(opts, fontOption{label: name, family: name})
	}
	return opts
}

// systemFontFamily 是「系统默认」那一条的值。Biu 的默认设置里也是这个串
// （`app.DefaultFontFamily`）。
const systemFontFamily = app.DefaultFontFamily

// pickFont 是字体那一格。
func (s *Shell) pickFont(c *ui.Context, st app.Settings, ro bool) {
	options := fontOptionList()
	families := make([]string, len(options))
	labels := make([]string, len(options))
	for i, f := range options {
		families[i], labels[i] = f.family, f.label
	}

	// 手改过的设置文件可能带一个不在名单里的族名（比如旧版写进去的
	// 「PingFang SC, Microsoft YaHei, system-ui」那种族名列表）。**不能当成系统
	// 默认**——那样用户刚打开这一页看到的字就是错的，而且他自己也不知道设置里
	// 存了什么。面板里加一条临时的「设置里的：xxx」，选中它就还是他原来那个。
	chosen := st.FontFamily
	if _, ok := findFont(options, chosen); !ok {
		families = append(families, chosen)
		labels = append(labels, "设置里的："+chosen)
	}

	idx := indexOfString(families, chosen)
	sel := ui.SelectBase(c, &idx)
	// 🔴 **定宽，不写 `Grow(1)`**。宽是控件列的基准（`selectWidth` =
	// `settingsControlW`），走 `settingsControlFitW` 钳——窄窗口那一档不钳的话
	// 会溢出分组卡片（900 宽实测溢出 14 像素）。
	//
	// 原来是 `Grow(1)`，而这一行的左边（标题 + 说明）也是 `Grow(1)`
	//（`settingsRowEl`），于是两边平分那一行——设置页收进 900 之后，这一格
	// 有 410 宽，一个「系统默认」四个字摆在一条 410 的框里，而它右边没有
	// 任何东西了。那不是「点哪儿都行」的好心，是控件在替一整行的空间负责：
	// 用户要按准那个 410 宽框的右下角才能展开它。
	trigger := sel.Trigger.
		Width(s.settingsControlFitW(c, selectWidth)).
		Radius(c.Theme().Radius).Border(1, c.Theme().Border).
		Background(c.Theme().Surface).Padding(space.None, space.Px10).Height(settingsSelectH).AlignItems(ui.Center).Disabled(ro).
		Label("选择字体")
	trigger.Children(func() {
		// ⚠️ 触发器上显示的那一行也用它自己的字体画，不然「我选了圆体」在收起
		// 的状态下看不出来。
		ui.Text(c, labels[idx]).Font(families[idx]).SingleLine().Grow(1)
	})

	sel.Popup(func(panel ui.Element) {
		t := c.Theme()
		popupPanel(t, panel.MaxWidth(fontMenuWidth).Padding(space.Px4)).Children(func() {
			// 🔴 **这份名单现在是系统报上来的，可能有两百多条**（这台 Mac 上
			// 245 条）。不套滚动容器、不限高的话，面板会比窗口还高：超出窗口的
			// 那些既看不见也点不到（和联想浮层当年那个坑一样，见
			// `navbar_search.go` 的 `viewSuggest`）。
			//
			// 上限 256 = HeroUI 下拉默认那一档（`max-h-64`），Biu 用的就是它。
			menu := ui.Scroll(c).Key("font-menu").FillWidth().MaxHeight(fontMenuMaxH)
			menu.Children(func() {
				ui.Column(c).FillWidth().Children(func() {
					for i, family := range families {
						// ⚠️ **不给这一格加 Label**：上面画的那个字就是它的名字，再补一个
						// 只会多出一个同样文字的无障碍节点（见 sidebar.go 里那条）。
						item := sel.Item(i).Padding(space.Px6, space.Px10).Radius(t.Radius)
						if item.Highlighted() {
							// 高亮走「被选中的选项」那一个出口（实心主色底 +
							// `t.AccentText` 字，票 #239，审计 F-14）。
							item = flatSolid(c, item)
						} else if i == idx {
							// 当前值（不是「选中」）：淡底、没有主色。
							item = item.Background(t.Surface)
						}
						item.Children(func() {
							// 🔴 这一句就是验收项：每一条选项用它自己的字体画。
							//
							// ⚠️ `Ellipsis` 不能省：族名的长短差得很远，而每一条用的是
							// **它自己的字体**、宽度没有上限——长名字会画到旁边去。
							ui.Text(c, labels[i]).Font(family).SingleLine().
								Ellipsis("…").Grow(1)
						})
					}
				})
			})
			s.trackScrollbar(menu)
		})
	})

	if trigger.Changed() {
		s.applySetting("字体", func(cur *app.Settings) { cur.FontFamily = families[idx] })
	}
}

// fontMenuWidth 是那个面板多宽。Biu 那一格是 `w-[180px]`（`system-settings.tsx:164`），
// 面板比它宽一点是必须的：这些字体名里最长的一个比 180 宽（无衬线 / Helvetica
// Neue），按 180 画就会被裁成「Helvetica N…」，用户挑不了。
const fontMenuWidth = 220

// fontMenuMaxH 是字体面板最高多高。256 = HeroUI 下拉默认的 `max-h-64`。
const fontMenuMaxH = 256

// findFont 在名单里找一条。
func findFont(options []fontOption, family string) (fontOption, bool) {
	for _, f := range options {
		if f.family == family {
			return f, true
		}
	}
	return fontOption{}, false
}

// indexOfString 找一个字符串在名单里的下标，没有就是 -1。
func indexOfString(list []string, v string) int {
	for i, s := range list {
		if s == v {
			return i
		}
	}
	return -1
}
