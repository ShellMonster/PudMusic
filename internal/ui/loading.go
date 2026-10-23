// 加载态：页面首屏在拉的时候画的那一枚转圈，和按钮「在忙」时的那一档。
//
// # 这一票（#178）把加载态从「一行淡字」换成「一枚转圈」
//
// 改之前每一页的首屏是「正在加载…」那一行 15px 的淡字（票 #138 把它统一成了
// 居中带插画位的那一套的**无插画**版本），而上游是**一枚转圈**：
//
//	稍后再看    src/pages/later/index.tsx:230            h-[40vh] 里居中的 <Spinner size="lg" />
//	历史记录    src/pages/history/index.tsx:280           h-[40vh] 里居中的 <Spinner />
//	我的关注    src/pages/follow-list/index.tsx:306       h-[40vh] 里居中的 <Spinner label="加载中" />
//	推荐音乐    src/pages/music-recommend/index.tsx:319   h-[40vh] 里居中的 <Spinner size="lg" />
//	搜索        src/pages/search/video-list/index.tsx:177 min-h-[280px] 里居中的 <Spinner label="加载中" />
//	收藏夹      src/pages/video-collection/collections/list.tsx:43  h-[280px] 里居中的 <Spinner label="加载中" />
//
// **「加载中」和「空」是两档，不是一档**：空状态说的是「这里没有东西」（一个
// 结论），加载中说的是「还没好」（一个过程）。所以加载中那一档**不摆插画、
// 不摆提示句**，只有一枚转圈；而转圈的**无障碍名字**是那一句话——读屏软件读到
// 的仍然是「正在加载…」。
//
// # 「40vh」是什么
//
// 上游那几处是 `h-[40vh]`：**一格 40vh 高，转圈在它中间**，而不是整个页面的
// 正中。我们照抄那一格的形状（`loadingShare`），于是转圈落在**这一格的正中**，
// 也就是从这一格的顶往下 20vh 的地方。
//
// ⚠️ **40vh 是「视口高度的四成」，不是「这一块高的四成」**：上游那几处的
// `40vh` 量的是视口（浏览器里是 `vh`，Electron 里是窗口的内容区），我们这里
// 同样量窗口（`c.Size()` 的第二个数）。
//
// 代价是**窄窗口下那一格可能比它摆的那一块还高**：最小窗口 900×560 的内容区
// 只有 408，而收藏夹那一页的头图区就吃掉 281，列表区只剩 126——那一格 224 高，
// 于是转圈的最下面一个像素被播放条切掉（实测 30.5 而不是 32）。
//
// ⚠️ **上游是同样的形状**（那一格也是 `40vh`，不管摆得下摆不下），而要「绝不
// 溢出」就得知道摆它的那一块有多高——列表里那一格拿不到（框架给列表里那些不是
// 行的孩子的是**无限高**，见 `mygo/ui/list.go` 的 `layoutList`）。
//
// 顺带一提：**列表会把这一格算成内容高度**，所以多出来的那几像素是能滚的
// （框架的滚动条只在指针落在容器上时才画，于是平时看不见）。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// loadingShare 是「加载中」那一格占视口高度的几成。上游是 `h-[40vh]`。
//
// ⚠️ **它量的是视口，不是这一块**：见文件头那一段。
const loadingShare = 0.4

// loadingSpinnerSize 是页面级那一枚转圈的直径。**32**。
//
// 上游那几处用的是 HeroUI 的 `<Spinner />`（默认档 = `w-8` = 32）或
// `<Spinner size="lg" />`（`w-10` = 40）——见
// `@heroui/theme` 的 `spinner`（`packages/core/theme/src/components/spinner.ts`）。
// 我们取**默认那一档**：两处上游用 lg、两处用默认，而这一枚是页面级的，
// 不必抢过内容（按钮里那一枚是 14，两处差得开才分得出是「按钮在忙」）。
//
// ⚠️ **不是主题字号那一档**：框架的转圈默认是 `theme.FontSize × 1.25`（13 ×
// 1.25 ≈ 16），那一枚在页面中央几乎看不见。
const loadingSpinnerSize = 32

// busySpinnerSize 是按钮里那一枚转圈的直径。**14**。
//
// ⚠️ 按钮只有 26~28 高（`ui.Button` 的内边距 6 上下 + 一行 13px 的字），20 会
// 顶满整个按钮（上游 HeroUI 的按钮里那一枚是 `sm` = 20，那是因为它的按钮高 40）。
const busySpinnerSize = 14

// busyGap 是按钮里转圈和字之间的缝。
//
// 上游 HeroUI 的按钮里那两样东西之间的缝是 `gap-2`（8）；这里取 6，因为按钮里
// 的字是 13（上游是 14），而 6 是 `ui.Button` 自己的 `Gap`（`t.Space(1.5)`）。
const busyGap = space.Px6

// drawLoading 画「正在加载」那一档：一枚居中的转圈。
//
// ⚠️ **它和「没有结果」不是同一档东西**：空状态是一枚插画 + 两行字（一个结论），
// 加载中只有一枚转圈（一个过程）。摆在一起的话，用户会把「还没好」读成「没有」。
//
// ⚠️ **它不装主题**：这一格大多画在页面里面，而页面那一层已经装过了（铁律二）。
// `applyFrameTheme` 是幂等的，多调一次没有副作用——而漏掉它的症状是「这一格用的是
// 系统色」，和旁边的界面对不上。
//
// key 给这一块的根元素（铁律四）。空串 = 不给。
func drawLoading(c *ui.Context, key, text string) {
	col := ui.Column(c)
	if key != "" {
		// ⚠️ Key 写在构造**之前**（铁律四的坑）：构造之后再 Key 等于没给。
		col = col.Key(key)
	}
	col.Grow(1).Children(func() {
		loadingSpinner(c, text)
	})
}

// loadingSpinner 是「加载中」那一格：40vh 高，转圈在它中间。
//
// ⚠️ **这一格的高度是写死的**（`Height`），不是 `Grow(0.4)`：列表的空状态那一格
// 是按**内容高**摆的（框架给列表里那些不是行的孩子的是无限高，见
// `mygo/ui/list.go` 的 `layoutList`），`Grow` 在那里一点用都没有——转圈会缩到
// 贴着列表的顶上。写死高度的话，页面级和列表里那两处落点就一样了。
func loadingSpinner(c *ui.Context, text string) {
	_, winH := c.Size()
	ui.Column(c).Height(winH * loadingShare).Center().Children(func() {
		// ⚠️ **Label 是这一枚的无障碍名字**（框架的转圈没有可见的字）：读屏软件
		// 读到的是「正在加载…」，而无头 Tester 也靠它认出这一枚。
		ui.Spinner(c).Size(loadingSpinnerSize, loadingSpinnerSize).Label(text)
	})
}

// busyButton 给一枚按钮装上「在忙」的样子：**转起来 + 灰下去**。
//
// ⚠️ **字不许拿掉**：拿掉之后按钮从「带字的那一枚」缩成一枚小方块（实测
// {x=1130, y=663, w=42, h=26}），状态一变宽度就跳一下——而这一枚的无障碍名字
// 正是那句话（`Label`）。转圈的那一枚是**加**上去的，不是替掉字的。
//
// ⚠️ **「灰」就是 `Disabled(true)`**：框架对禁用的控件是整个乘 0.5 不透明度
// （`mygo/ui/paint.go:209-212`），而上游 HeroUI 的按钮在 `isLoading` 时也是
// 禁用的（`use-button.ts`：`const isDisabled = isDisabledProp || isLoading`）。
// 所以这一档**不用自己调色**——调了反而和框架那一档对不上。
//
// ⚠️ **调用方要用空字建按钮**（`ui.PrimaryButton(c, "")`）：这一枚的字是这里
// 给的（忙的时候那句话和平时那句不一样），而 `Children` 是**加**上去的，不是
// 换掉原来那一份。
//
// ⚠️ **转圈也有那个名字**（`Label`）：上游 HeroUI 按钮里那一枚转圈是**没有**
// 名字的（`button.tsx`：`spinner = <Spinner color="current" size={spinnerSize} />`），
// 而我们给它名字有两个理由：读屏软件读到的是「上传中…」而不是一个没有名字的
// 指示器；而无头 Tester 只认「字」和「名字」——没有名字的那一枚在测试里
// **抓不住**（`tt.Find` 找的就是那两样）。
//
// b 是已经建好的那一枚按钮；fg 是它那一份字色（转圈要跟着按钮自己的颜色——
// 框架的转圈默认是暗色，不跟着走）。
func busyButton(c *ui.Context, b ui.Element, label string, fg ui.Color) ui.Element {
	b = b.Disabled(true).Label(label)
	b.Children(func() {
		ui.Row(c).Gap(busyGap).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).Size(busySpinnerSize, busySpinnerSize).TextColor(fg).Label(label)
			ui.Text(c, label).FontSize(c.Theme().FontSize)
		})
	})
	return b
}

// busyPrimaryButton 画一枚在忙的**主按钮**（实心底）。
//
// 这是「主按钮在忙」那一档的入口：调用方给的是**忙的时候那句话**（比如
// 「上传中…」），不忙的时候照旧用 `ui.PrimaryButton` 建那一枚。
func busyPrimaryButton(c *ui.Context, label string) ui.Element {
	t := c.Theme()
	return busyButton(c, ui.PrimaryButton(c, ""), label, t.AccentText)
}
