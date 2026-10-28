// 「数据显示样式」那三档，和它在两个控件之间的分发。
//
// 设置里那一格（`internal/ui/settings_tab_general.go` 的 `displayModes`）摆的是
// `app.DisplayMode`，这里是它落到画法上的样子。**两套枚举各管一段**：设置那边管
// 「存进设置文件的那个字符串」，这里管「画的时候走哪一支」，中间的对照只写一遍
// （ViewOf）。别让页面各自去 `switch` 那三个字符串——那是三处会对不上的来源。
//
// 三个值和 `displayModes` 那三行**顺序一致**，因为设置页那个预览要靠同一个下标
// 把「选中的那一档」和「预览的那一张」对上（见 `previewOf`）。
package list

import "github.com/ShellMonster/PudMusic/internal/app"

// View 是一份歌此刻该摆成什么样。
//
// 零值是列表：它是最老的那一档，也是设置默认值（`app.DefaultSettings` 的
// `DisplayModeList`），所以「没装过 / 认不出 / 忘了设」全都落在这一档上——
// 认不出的档位由 `app.pickEnum` 退回默认（`internal/app/settings.go`）。
type View uint8

const (
	// ViewList 一行一首：封面 + 歌名 + UP 名 + 时长。
	ViewList View = iota
	// ViewCard 网格：一行几首，列数按容器宽分档（见 grid.go）。
	ViewCard
	// ViewCompact 紧凑：还是一行一首，但封面小一号、歌名只占一行、上下内边距
	// 收窄——一屏能多看将近一半的歌。
	ViewCompact
)

// ViewCount 是档数。加一档要改这一处（设置页的预览按它排三张图）。
const ViewCount = 3

// ViewOf 把设置里那三档换成画法。
//
// **认不出的档位一律当列表**：设置那边已经退回默认了（`app.pickEnum`），而一个
// 拼错的值落到界面上应该是「和默认一样」而不是「什么都不画」。
func ViewOf(m app.DisplayMode) View {
	switch m {
	case app.DisplayModeCard:
		return ViewCard
	case app.DisplayModeCompact:
		return ViewCompact
	default:
		return ViewList
	}
}

// ModeOf 是 ViewOf 的反过来：这一档存进设置文件里写哪句话。
//
// 存在的理由是设置页的预览（`internal/ui/displaypreview.go`）：预览要能只凭一个
// `View` 就知道「我现在画的是哪一档」，而反过来再算一次会让「档位」有两个来源。
func ModeOf(v View) app.DisplayMode {
	switch v {
	case ViewCard:
		return app.DisplayModeCard
	case ViewCompact:
		return app.DisplayModeCompact
	default:
		return app.DisplayModeList
	}
}

// String 是这一档的名字。测试报错时用它，读日志时也用它。
func (v View) String() string {
	switch v {
	case ViewCard:
		return "网格"
	case ViewCompact:
		return "紧凑"
	default:
		return "列表"
	}
}
