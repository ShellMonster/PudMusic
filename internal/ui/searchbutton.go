// 会展开的搜索：收起时是一枚放大镜图标键，点开才是一个输入框。
//
// 出处是上游的 `src/components/search-button/index.tsx`：收藏夹、本地音乐两页
// 的「按名字找」都是它，而不是一个常驻的输入框（常驻的那一版在最小窗口上要么
// 挤掉旁边的排序，要么自己被压成一条缝）。
//
// # 收起 / 展开与「点外面」
//
// 收起与展开是 `searchButtonState.open`。**外面的点击靠焦点判断**：输入框
// 展开时拿到焦点（`AutoFocus`），用户点别的控件时焦点就过去了，下一帧这里发现
// 它不再有焦点就把面板收起来。
//
// ⚠️ **不能只看「这一帧没有焦点就收」**：换一页再回来时，输入框那一帧是重画的
// （`Bounds` 是空的），焦点并没有「被别的控件拿走」——直接收的话，用户回来看到
// 的是一个收起按钮，而列表还按他上次的词筛着，界面上没有第二处说这件事。
// 所以判据是「上一帧它在画面上（`Bounds` 非空）**而且**上一帧有焦点、这一帧没
// 有了」——那才是「点到了别处」。
//
// 点到页面空白处不会移走焦点，所以那一下不收（上游的 `useClickAway` 会收）。
// 要判「点到了空白」得有一个盖住整页的透明层，而这一层会挡掉下面所有控件——
// 不值得为一个收起的时机做这件事。
//
// # 焦点不许抢
//
// 输入框只在**用户刚点了那枚放大镜**的那一次拿到焦点（`focusNext`）。别的时机
// （尤其是「切到别的页再回来」）输入框照样是展开的，但**不碰键盘焦点**。
//
// 理由：回来的时候那个输入框是**重画**出来的（元素重建 = 新生），而
// `AutoFocus` 认的正是「这一帧刚出生」——不挡一下的话，用户在别的页敲的字会
// 落进这个他看不见自己打开过的框里。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// searchFieldHeight 是展开后那个输入框的高。上游 `classNames.inputWrapper` 是
// `h-10`（40），和操作条上那枚主按钮同高。
const searchFieldHeight = 40

// searchButtonState 是一枚会展开的搜索此刻的状态。
//
// 它必须长在页面状态上（不是 view 的局部变量）：即时模式每一帧都重画，局部
// 变量下一帧就没了。
type searchButtonState struct {
	// open 是展开着没有。
	open bool
	// wasFocused 是上一帧那个输入框有没有键盘焦点。见文件头「点外面」那一段。
	wasFocused bool
	// focusNext 是「下一帧给那个输入框焦点」。
	//
	// ⚠️ **只有用户点了放大镜才置它**：切页回来时输入框会重画（新生），而
	// `AutoFocus` 正是按「这一帧刚出生」来抢焦点的——不挡这一下，用户在别的
	// 页敲的字就落进这个框里（评审第 5 条）。
	focusNext bool
}

// expandableSearch 画一枚会展开的搜索。
//
// text 是框里那个词（由调用方持有），label 同时是收起时那枚图标键的名字和展开
// 后输入框的名字——两态只有一个控件，名字不变，测试和读屏软件也就只认这一个名字。
//
// minW / maxW 是展开后那个输入框的宽度范围。给 `minW` 是为了挤的时候它先让位
// （旁边的排序下拉不能被压扁），给 `maxW` 是因为上游展开到 240 就停了。
//
// tip 是界面层那一份带延迟的悬停提示（tooltip.go）：收起时那枚图标键的提示
// 走它，和别的图标键一个时延（框架那枚固定 600ms，和全应用对不上）。为零时
// （单独造这一页的测试）退回框架那枚。
//
// justShown 是「这一帧是刚进这一页的第一帧」（`Shell.pageJustShown`）：那一帧
// 先不建输入框，好让框架别把焦点还给一个用户没打开过的框。
//
// 返回的是**展开时那个输入框**；收起时返回零值元素（它的 `Submitted` /
// `Focused` 一律是 false）。调用方靠这个读回车。
func expandableSearch(c *ui.Context, tip tipFunc, st *searchButtonState, text *string,
	label string, minW, maxW float32, justShown bool) ui.Element {
	// ⚠️ **刚进这一页的那一帧不建输入框**（`justShown`）：框架切页时会把焦点
	// 还给「离开这一页时有焦点的那个元素」，而那正是这个输入框——不建它就没有
	// 焦点可还，下一帧它照常展开（词还摆在那儿），焦点却不落在它身上。
	//
	// 详见 `Shell.pageJustShown`。
	if !st.open || justShown {
		if tipFrom(tip, c, flatIconButton(c, iconSearch, label, iconMd).
			Size(iconHitMd, iconMd).Padding(space.None, space.Px7), label, tipDefault).Clicked() {
			st.open, st.wasFocused, st.focusNext = true, false, true
		}
		return ui.Element{}
	}

	field := ui.TextInput(c, text).
		Width(minW).Grow(1).MaxWidth(maxW).Height(searchFieldHeight).
		Label(label).Placeholder(label)
	// 展开的那一帧把焦点给它——**但只在用户刚点了放大镜的时候**（见文件头
	// 「焦点不许抢」）：用户点的是放大镜，接下来要做的就是打字；而切页回来时
	// 那个框是重画出来的，不该抢走别处的键盘焦点。
	if st.focusNext {
		field = field.AutoFocus()
		st.focusNext = false
	}

	// 见文件头：只有「上一帧在画面上、上一帧有焦点、这一帧没了」才算点到了别处。
	onScreen := field.Bounds().W > 0
	focused := field.Focused()
	if onScreen && st.wasFocused && !focused {
		st.open = false
	}
	st.wasFocused = focused
	return field
}
