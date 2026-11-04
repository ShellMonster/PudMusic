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

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
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
		// `system-ui` 是「默认」那一条存的值；随包的标题字体「PudMusic Serif」是内部字体，
		// 不是用户能选来当界面字体的（DESIGN.md §0′），都不进名单。
		if strings.EqualFold(name, systemFontFamily) || name == typo.DisplayFamily {
			continue
		}
		opts = append(opts, fontOption{label: name, family: name})
	}
	return opts
}

// systemFontFamily 是「系统默认」那一条的值。Biu 的默认设置里也是这个串
// （`app.DefaultFontFamily`）。
const systemFontFamily = app.DefaultFontFamily

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
