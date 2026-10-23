// 设置页里那些「从一个固定名单里选一个」的下拉。
//
// ## 为什么不用 `ui.Select`
//
// `ui.Select` 把**选中的那个值本身**画在收起状态里（`ui/widgets.go` 的 coreSelect：
// `coreText(c, *selected)`）。我们要的是画**人看的那几个字**——存的是
// `auto`，画出来得是「自动」。用 `ui.Select` 的结果是收起状态写着 `auto`，旁边
// 展开着「自动 / 无损 / 高品质」，用户得自己对一遍才知道选了哪个。
//
// 所以用 `ui.SelectBase` 自己拼一个：收起和展开都用 label，存的还是值。
package ui

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// enumItem 是下拉里的一项。
type enumItem[T comparable] struct {
	value T
	label string
}

// enumSelect 是一个下拉。收起时显示当前那项的 label，展开时每一项都用它自己的
// label，存进设置的是 value。
//
// ⚠️ `label` 是这个下拉**自己的**名字，要和左边那一行的标题错开：设置页里两样东西
// 写着同一个词的时候，读屏软件念出来是「音质偏好、音质偏好」，用户不知道第二个
// 是干什么的。
//
// ⚠️ 收起来和展开都摆 label，所以 `value` 在设置里是 `auto` 这样的词、界面上是
// 「自动」这样的字——两者之间的翻译只有这一份，`Changed()` 之后读回来的 value
// 直接就是设置里该有的东西，中间没有第二次转换可以出错。
func enumSelect[T comparable](c *ui.Context, value *T, items []enumItem[T], label string, width float32, ro bool) {
	t := c.Theme()
	labelOf := func(v T) string {
		for _, it := range items {
			if it.value == v {
				return it.label
			}
		}
		return fmt.Sprint(v) // 值不在名单里（手改的设置文件）：画那个值自己，别画空
	}

	sel := ui.SelectBase(c, value)
	trigger := sel.Trigger.Width(width).Radius(t.Radius).
		Background(t.Surface).Border(1, t.Border).Padding(0, 8).Disabled(ro).
		Label(label)
	trigger.Children(func() {
		ui.Text(c, labelOf(*value)).SingleLine().Grow(1)
		ui.Icon(c, chevronIcon()).TextColor(t.TextMuted)
	})

	sel.Popup(func(panel ui.Element) {
		popupPanel(t, panel.Grow(1).MaxWidth(width+24).Padding(4)).Children(func() {
			for _, it := range items {
				opt := sel.Item(it.value).Padding(6, 10).Radius(6)
				switch {
				case opt.Highlighted():
					// 高亮（指针 / 键盘游标在这一项）走「被选中的选项」那一个
					// 出口：实心主色底 + `t.AccentText` 字（票 #239，审计 F-14）。
					opt = flatSolid(c, opt)
				case it.value == *value:
					// ⚠️ **这一档不是「选中」**：它是**当前值**（指针不在这一项上时
					// 靠它认出选的是哪个），所以是**淡底、没有主色**。指针落在当前值
					// 上时两个态叠在一起——那时「高亮」那一档盖过它。
					opt = opt.Background(t.Surface)
				}
				opt.Children(func() { ui.Text(c, it.label).SingleLine().Grow(1) })
			}
		})
	})
}

// chevronIcon 是下拉右边那个小箭头。
//
// MyGo 自己的 `ui.Select` 里那一个没导出（`ui/widgets.go` 的 `chevron`），这里
// 自己画一个同样的，免得为了一个 10x10 的箭头去改框架。
func chevronIcon() *ui.SVG {
	const shape = `<path d="M2.5 4 L5 6.5 L7.5 4"/>`
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10" ` +
		`fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" ` +
		`stroke-linejoin="round">` + shape + `</svg>`))
}
