// 切 Tab 的过渡：旧的那一块怎么退、新的那一块怎么进（票 #368）。
//
// # 这一票做两件事
//
//  1. **切 Tab 有过渡**（此前是硬切：`h.tab` 一变，`viewTabBody` 整块换内容）。
//  2. **「流行」切进去不闪**：那一档还在拉的时候，**旧的那一块留在画面上**，
//     不要先画「正在加载」再换一屏内容。
//
// # 过渡的形式：MyGo 文档的「Coming and going」
//
// 框架的 `ui.ElementTransition` 有两半：`Enter`（这一块怎么进来）和 `Exit`
// （这一块怎么出去）。文档里的例子（MyGo `docs/ui/transitions.md`）：
//
//	toast := ui.ElementTransition{
//		Enter: &ui.Motion{Y: 12},
//		Exit:  &ui.Motion{Opacity: 1, X: 300},
//	}
//
// ⚠️ **`Exit` 那一半不是「把旧内容留着不动」**：框架在旧元素不再被建的那一帧
// **复制一份它上一帧的样子**（`mygo/ui/transition.go` 的 `ghostOf`），让那一份
// 按 `Exit` 走完，然后扔掉。所以：
//
//   - **旧内容在退场那 200ms 里不重建**（省下的是一整份列表每帧的活）；
//   - 它「还在画面上」这件事由框架替我们留住——而它画的是**上一帧那张**，
//     不是重新画一遍。所以它退场的时候是「一张照片淡出」。
//
// # 时长 / 幅度 / 几个一起动
//
//   - **200ms**：抄 DESIGN.md §五《逐处》那张表里**「悬停淡入」那一档**
//     （`transition-opacity duration-200`）——那是一块内容淡入的时长，而
//     这里是「一屏内容换掉」，同一种「淡入淡出」，同一个数。
//   - **曲线走 `motion.Unified`**（DESIGN.md §五《一条统一曲线》）：全应用一条，
//     ease-out。不跟统一曲线的只有两处（hover 放大、主 Tab 滑块），这里不是。
//     ⚠️ 这条曲线是**前重后轻**的（前 50% 的时间走完 95.4%，实算），所以
//     200ms 的名义时长**摸起来只有 100ms 上下**——过渡「看得见」的那一段更短。
//     这是有意的：切 Tab 是点击触发的，要**立刻**给到新内容（DESIGN.md §五
//     《总纲》：时长按距离和尺寸定，不按一个上限定）。
//   - **幅度 8 DIP**：只是让新内容「落进来」而不是「凭空出现」。小到读不出
//     方向，只读得出「动了」——DESIGN.md §五《规则》第 5 条（有意识地减少
//     动画的元素数量）。
//   - **同时动两个**：旧的那一块退、新的那一块进。别的一律不动（Tab 那一排的
//     滑块是它自己的弹簧，那是另一件事）。
//
// # ⚠️ 动的是**布局位置**，不是 transform
//
// 进场那 8 DIP 是框架的 `Motion.Y`——它改的是元素在父元素里的**位置**
// （`mygo/ui/transition.go` 的 `transition.apply` 写的是 `e.x` / `e.y`），
// 不是一层 transform。DESIGN.md §五《不做》那条写着「动画不要用 transform
// 动大子树……要动就动布局位置」——**这一处正是照它做的**。
//
// # 和队列抽屉那一处不冲突
//
// 队列抽屉的进出场（DESIGN.md §五《抽屉进出场》）动的是面板的**右边距**
// （布局位置），这里动的是内容块的**位置 + 不透明度**：两处都是「动布局位置」，
// 形式一样；不同的只是抽屉是「一块面板滑进来」，这里是「一屏内容换掉」。
// 抽屉那一处的裁决**没有被推广成全局规矩**：本页不滑、不缩放，只淡入淡出。
//
// # ⚠️ 上游是硬切
//
// 上游 Biu 的 `src/pages/music-recommend/index.tsx` 的 `activeTab` 一换就换，
// 没有过渡。**这一票是「我们比上游多」，不是「对齐上游」**（票 #368 的原话）。
package ui

import (
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/motion"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// homeBodyDuration 是切 Tab 时内容那一块的进出场时长。
//
// ⚠️ **和「悬停淡入」那一档一样是 200ms**（DESIGN.md §五《逐处》那张表）：那是
// 本仓库「一块内容淡入」的时长，而这里是「一屏内容换掉」——同一种淡入淡出，
// 同一个数。
const homeBodyDuration = 200 * time.Millisecond

// homeBodyRise 是新内容进场时从下面升上来的幅度（DIP）。
//
// ⚠️ **8 是「读得出动了、读不出方向」的那个数**：再大就成了一次「滑动」
// （DESIGN.md §五《规则》第 5 条：元素自身的变化不应太夸张），再小就只是
// 「凭空出现」。
const homeBodyRise = 8

// homeBodyTransition 是切 Tab 时内容那一块的进出场。
//
// ⚠️ **`Enter` 和 `Exit` 都要有 `Opacity`**：只写 `Y` 的话框架按「位移」处理
// （`Motion.fades`：不透明度小于 1 才算淡入淡出），而这里要的是**淡入淡出**
// 加一点位移——两样一起，才不是「硬切」。
var homeBodyTransition = ui.ElementTransition{
	Duration: homeBodyDuration,
	Ease:     motion.Unified,
	// 进来的那一块：从下面 8 DIP 处升上来，同时淡入。
	Enter: &ui.Motion{Opacity: 0, Y: homeBodyRise},
	// 出去的那一块：原地淡出。
	//
	// ⚠️ **不位移**：退场那一半是框架复制出来的**一张快照**（不是重建的内容），
	// 把它推走，推走的是一张照片。
	Exit: &ui.Motion{Opacity: 0},
}

// homeBodyKeys 是内容那一块三档的 key。
//
// ⚠️ **存成 `any` 是为了一个分配都不多**：key 是 `any`，而把一个字符串装进
// `any` 要一次堆分配（`runtime.convTstring`）——每帧一次就是首页那一档每帧
// 多一次分配，而那一档的预算**只剩一次**（`framebudget_test.go`：实测 137 次
// / 上限 138 次）。初始化的时候装好，每帧只是取一个。
//
// ⚠️ **别改回 `strconv.Itoa` 拼出来的那种**：那一下是两次分配（拼字符串 +
// 装箱），首页那一档立刻超预算。
var homeBodyKeys = [homeTabCount]any{
	"home:body:0",
	"home:body:1",
	"home:body:2",
}

// viewBody 画「内容那一块」。
//
// ⚠️ **这一块带 Key**：切 Tab 时它整个换掉（key 跟着「画的是哪一档」走），
// 框架靠这个认出「旧的那一块没了、新的这一块来了」，于是旧的按 `Exit` 退、
// 新的按 `Enter` 进（见 `homeBodyTransition`）。
//
// ⚠️ **key 里带的是「画的是哪一档」，不是「选中的是哪一档」**：选中那一档还在
// 拉的时候，画面上画的是**上一档**（见 `syncShown`）——那一块不该换，也不该动。
func (h *homePage) viewBody(c *ui.Context, s *Shell) {
	tab := h.shown
	ui.Column(c).Key(homeBodyKeys[tab]).Grow(1).Gap(space.Px12).
		Transition(homeBodyTransition).Children(func() {
		// 新歌速递只画在「流行」那一档的顶部，别的地方一律不画（上游
		// index.tsx:295）。它自己那三态（拉取中 / 拉不到 / 正常）是它内部的
		// 事，这里只把这一摊的状态递过去。
		//
		// ⚠️ **这一块本身不是在这里画的**（票 #369）：它是「流行」那一档**列表的
		// 第 0 行**（页内头部，见 `list.Lead`），所以它在列表的滚动区里——跟着内容
		// 一起滚走（上游那一页的顶图和下面的列表在**同一个滚动容器**里）。画它
		// 的地方是 `viewPopLead`（那一档有列表的时候）和 `viewPopTop`（那一档
		// 一首歌都没有、没有列表可挂的时候）。
		//
		// ⚠️ **状态要在画之前递过去**：它那三态是它**画的时候**读的，而它在
		// 布局那一遍才画（列表的行是布局时建的）——递晚了那一帧画出来的是旧状态。
		if tab == homePopTab {
			h.newGrid.SetStatus(h.feed.loadingOf(homeNewMusic), h.feed.newTab.err)
		}

		h.viewTabBody(c, s, tab)
	})
}

// syncShown 算「此刻画面上画的是哪一档」。
//
// ⚠️ **它和 tab（选中的那一档）不是同一件事**，而这正是「流行切进去不闪」那
// 一条：切到一档而它还在拉的时候，画面上留着**上一档**的内容——用户看到的是
// 「内容还在换」，而不是「屏幕空了一下」。
//
// ⚠️ **它在 `syncHome` 那一层跑，不在 view 里**：它是「每帧都要算一次的副作用」
// （跟 `adopt` / `pull` 同一类），而 view 是「画成什么样」（票 #161 把这两件事
// 拆开的）。
func (h *homePage) syncShown() {
	// 选中的那一档已经有着落了（歌 / 失败 / 真的空）→ 画它。
	if !h.tabPending(h.tab) {
		h.shown = h.tab
		return
	}
	// 还在拉：留着上一档的内容——但**只有那一档真的有歌才留得住**。
	// 留着的是一个「拉不到」或者「空着」的话，留着的是一句跟当前这一档无关的话
	//（切到「流行」还拉着，屏幕上却写着「音乐推荐加载失败」）。
	if len(h.songs.tabs[h.shown]) == 0 {
		h.shown = h.tab
	}
}

// tabPending 是「这一档的内容还在路上」。
//
// ⚠️ **「还在拉」和「拉到了但是空的」是两件事**（见 `viewTabBody` 那四种状态）：
// 只有「还在拉」才留得住旧内容；「拉到了但是空的」要立刻把空状态画出来。
func (h *homePage) tabPending(tab int) bool {
	// 这一档的歌还在拉，而手上还没有歌。
	if h.feed.loadingOf(tab) && len(h.songs.tabs[tab]) == 0 {
		return true
	}
	// 「流行」那一档还多一块「新歌速递」：它也在拉的时候，这一档**不算齐**。
	// ⚠️ 不等它的话，新歌速递会先画一个转圈、然后自己换成一屏卡片——那一跳
	// 就是这一票要消掉的那一种（只是发生在那一块内部，而不是整屏）。
	if tab == homePopTab && h.feed.loadingOf(homeNewMusic) && len(h.songs.new) == 0 {
		return true
	}
	return false
}
