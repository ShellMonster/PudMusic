// 设置里的「数据显示样式」：各页怎么读它，以及设置页旁边那张预览。
//
// # 这一格以前是不工作的
//
// 「数据显示样式」三档从进项目起就摆在那儿、能存进设置文件，而**没有任何一页读
// 它**。症状是「改了没有任何页面跟着变」——比「设置没用」更让人火大的是
// 「改了只有搜索页变了」，所以本文件把读它这件事收在一个出口上（`displayView`），
// 由每一页在每一帧调一次，而不是各页自己去 `switch` 那三个字符串。
//
// # 三档的分工
//
//   - 列表：一行一首（`widgets/list` 的 `List`）
//   - 紧凑：还是一行一首，行高更矮（`List` 的紧凑档）
//   - 网格：一行几首（`widgets/list` 的 `Grid`）
//
// 枚举和对照在 `widgets/list/view.go`，那边只有一份来源。这边要做的只是**每帧
// 把它从设置里读出来交给页面**。
//
// # 本地音乐页为什么是另一回事
//
// 见 `localmusic.go` 顶上那段：那一页自己有「表格」这一档（文件大小 / 格式 /
// 创建时间这些列和封面没关系），全局设置管不到它，于是那一页的页内开关改成
// 「表格 / 歌曲」两档，选了「歌曲」之后由这一份设置说了算。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
)

// 设置页「外观 → 数据显示样式」那三张预览卡在 settings_tab_appearance.go（`dispCard`）；
// 这里只留各个内容页读「现在是哪一档」的那一个入口。

// displayView 是**这一帧**内容页该摆成什么样。
//
// 每帧调：设置随时会变，而这是唯一一处把设置翻译成画法的地方。认不出的档位由
// `list.ViewOf` 退回列表（设置那边也已经退回默认了）。
//
// 🔴 **别缓存**：设置页改了档之后是靠 `applySetting` → `invalidate` → 重画生效的，
// 而缓存下来的话「改了没变」——正是这一票要消灭的那种坏。
func (s *Shell) displayView() list.View {
	return list.ViewOf(s.app.SettingsSnapshot().DisplayMode)
}
