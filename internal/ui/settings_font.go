// 设置页的字体选择：Biu 的 `src/components/font-select`。
//
// ## 每一条用它自己的字体画预览
//
// 这是这一票明写的一条验收（C-48）。做法是 `ui.Text(...).Font(family)`：MyGo 的
// `Font` 收一个**逗号分隔的族名列表**，系统有的那一个画字、没有的往后找
// （`ui/element.go:997`），所以「这台机器上没有的字体」画出来是回退后的那个——
// 和 Biu 的 CSS `fontFamily` 一样，用户看到的就是他真会拿到的样子。
//
// 用 `ui.Select`（普通下拉）摆不了这个：它的选项是纯字符串，画出来一律是当前
// 字体。所以这里用 `ui.SelectBase` 自己拼一个面板，选项里画自己的字。
//
// ## 列表是定死的，不是问系统要来的
//
// Biu 那个下拉里装的是 `window.electron.getFonts()` 报上来的系统字体表。MyGo
// v0.5.5 **没有**这个 API（`internal/text/` 底下只有排版引擎，没有枚举字体的
// 入口），所以这一份是一张挑好的名单。
//
// 名单怎么挑的：每一项都写成**族名列表**而不是单个族名，于是缺的那一个自动由
// 后面的顶上——「苹方」在 macOS 上是 PingFang SC，在 Windows 上落到
// Microsoft YaHei，两边都有字可看。系统默认那一条用 `system-ui`，那是 MyGo 认得
// 的系统自己的字体（`ui/keys.go` 旁边 `Font` 的注释里点名的两个之一）。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// fontOption 是一条字体选项。family 直接存进 `app.Settings.FontFamily`。
type fontOption struct {
	label  string
	family string
}

// fontOptions 是那张名单。第一项是 Biu 那个 `[{ name: "系统默认", ... }]`。
var fontOptions = []fontOption{
	{"系统默认", "system-ui"},
	{"苹方 / 微软雅黑", "PingFang SC, Microsoft YaHei, system-ui"},
	{"黑体", "Heiti SC, SimHei, sans-serif"},
	{"宋体", "Songti SC, SimSun, serif"},
	{"圆体", "Hiragino Maru Gothic ProN, Yuanti SC, sans-serif"},
	{"楷体", "Kaiti SC, KaiTi, serif"},
	{"衬线", "Georgia, Times New Roman, serif"},
	{"无衬线", "Helvetica Neue, Arial, sans-serif"},
	{"等宽", "Menlo, Consolas, monospace"},
	{"打字机", "Courier New, monospace"},
}

// systemFontFamily 是「系统默认」那一条的值。Biu 的默认设置里也是这个串
// （`app.DefaultFontFamily`）。
const systemFontFamily = app.DefaultFontFamily

// pickFont 是字体那一格。
func (s *Shell) pickFont(c *ui.Context, st app.Settings, ro bool) {
	families := make([]string, len(fontOptions))
	labels := make([]string, len(fontOptions))
	for i, f := range fontOptions {
		families[i], labels[i] = f.family, f.label
	}

	// 手改过的设置文件可能带一个不在名单里的族名。**不能当成系统默认**——那样
	// 用户刚打开这一页看到的字就是错的，而且他自己也不知道设置里存了什么。
	// 面板里加一条临时的「设置里的：xxx」，选中它就还是他原来那个。
	chosen := st.FontFamily
	if _, ok := findFont(chosen); !ok {
		families = append(families, chosen)
		labels = append(labels, "设置里的："+chosen)
	}

	idx := indexOfString(families, chosen)
	sel := ui.SelectBase(c, &idx)
	trigger := sel.Trigger.
		Grow(1).Radius(c.Theme().Radius).Border(1, c.Theme().Border).
		Background(c.Theme().Surface).Padding(0, 8).Disabled(ro).
		Label("选择字体")
	trigger.Children(func() {
		// ⚠️ 触发器上显示的那一行也用它自己的字体画，不然「我选了圆体」在收起
		// 的状态下看不出来。
		ui.Text(c, labels[idx]).Font(families[idx]).SingleLine().Grow(1)
	})

	sel.Popup(func(panel ui.Element) {
		t := c.Theme()
		panel.Grow(1).MaxWidth(fontMenuWidth).Padding(4).Radius(t.Radius+2).
			Background(t.Background).Border(1, t.Border).Children(func() {
			for i, family := range families {
				// ⚠️ **不给这一格加 Label**：上面画的那个字就是它的名字，再补一个
				// 只会多出一个同样文字的无障碍节点（见 sidebar.go 里那条）。
				item := sel.Item(i).Padding(6, 10).Radius(6)
				if item.Highlighted() {
					item.Background(t.Accent).TextColor(t.AccentText)
				} else if i == idx {
					item.Background(t.Surface)
				}
				item.Children(func() {
					// 🔴 这一句就是验收项：每一条选项用它自己的字体画。
					ui.Text(c, labels[i]).Font(family).SingleLine().Grow(1)
				})
			}
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

// findFont 在名单里找一条。
func findFont(family string) (fontOption, bool) {
	for _, f := range fontOptions {
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
