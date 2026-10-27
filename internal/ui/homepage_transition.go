// 切 Tab 的过渡：旧的那一块怎么退、新的那一块怎么进（票 #368、票 #238）。
//
// # #368 做两件事
//
//  1. **切 Tab 有过渡**（此前是硬切：`h.tab` 一变，`viewTabBody` 整块换内容）。
//  2. **「流行」切进去不闪**：那一档还在拉的时候，**旧的那一块留在画面上**，
//     不要先画「正在加载」再换一屏内容。
//
// # #238 收尾：旧的先淡出、新的后淡进（中间不同时画两层）
//
// 此前那一段是**交叉淡入淡出**：旧的那一块一边淡出、新的那一块一边淡进，
// **同一时刻画面上有两层内容**——那就是重影（票 #238 的 F1，评审亲眼看见的）。
//
// 现在按 DESIGN.md §五《规则》第 10 条（**删除时透明度先变、添加时透明度后
// 变**）改成**两段**：
//
//	退场（homeFadeOut）	旧的那一块从满亮到全灭，**新的那一块还不建**
//	进场（homeFadeIn）	旧的那一块已经没了，**新的那一块**从全灭到满亮
//
// ⚠️ **交接那一下不空**：旧的那一块在那一帧**已经不建了**（它是被换掉的，不
// 是被淡掉的），而**新的那一块在那一帧已经有四成**（进场那一段提前一帧起算，
// 见 `homeFadeInLead`）——所以**任何一帧上只有一层，而内容那一块始终有东西**。
//
// # ⚠️ **退场那一段自己淡出，不靠框架的 `Exit`
//
// 框架的 `Exit` 那一半是**复制一份上一帧的样子**再让它走完（`mygo/ui/
// transition.go` 的 `ghostOf`）——于是：
//
//   - 那一份复制品要**重新排一遍旧列表的文字**（框架的 `layoutBox`），切 Tab
//     之后的**第一帧**因此一次性多分配 **185 KB**（票 #238 的 F4）；
//   - 它和新的那一块**同时在画面上**——那就是重影。
//
// 所以这里**不写 `Exit`**：退场那一段里旧的那一块**照常建**（它就是屏幕上
// 那一块），淡出是**它自己的不透明度**（`Opacity`），不走框架那份复制品。
//
// # 进场那一段还是框架的 `Enter`（位置）
//
// 新的那一块**从下面 8 DIP 处升上来**（`Motion.Y`，动的是**布局位置**不是
// transform，见下面《⚠️ 动的是布局位置》那一段）。不透明度是**我们自己**
// 算的（`homeFadeInCurve`），所以两件事**各管一样**，不叠乘。
//
// # 时长 / 幅度
//
//   - **进场 200ms**（`homeBodyDuration`）：抄 DESIGN.md §五《逐处》那张表里
//     **「悬停淡入」那一档**（`transition-opacity duration-200`）——那是一块
//     内容淡入的时长，而这里是「一屏内容换掉」，同一种「淡入淡出」。
//   - **退场 100ms**（`homeFadeOut`）：比进场短，因为切 Tab 是点击触发的，
//     **要立刻给到新内容**（DESIGN.md §五《总纲》：时长按距离和尺寸定，不按
//     一个上限定）——而旧的那一块**一直亮到最后一刻**（见 `homeFadeOutCurve`），
//     所以这 100ms 里用户看到的内容**没有变暗**。
//   - **幅度 8 DIP**：只是让新内容「落进来」而不是「凭空出现」。小到读不出
//     方向，只读得出「动了」——DESIGN.md §五《规则》第 5 条（有意识地减少
//     动画的元素数量）。
//   - **只动内容那一块**：Tab 那一排的滑块是它自己的弹簧，那是另一件事。
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

// homeBodyDuration 是切 Tab 时内容那一块的**进场**时长。
//
// ⚠️ **和「悬停淡入」那一档一样是 200ms**（DESIGN.md §五《逐处》那张表）：那是
// 本仓库「一块内容淡入」的时长，而这里是「一屏内容换掉」——同一种淡入淡出，
// 同一个数。
const homeBodyDuration = 200 * time.Millisecond

// homeFadeOut 是**退场**那一段的时长：旧的那一块从满亮到全灭。
//
// ⚠️ **比进场短**：切 Tab 是点击触发的，**要立刻给到新内容**（DESIGN.md §五
// 《总纲》：时长按距离和尺寸定）。而旧的那一块**一直亮到最后一刻**（见
// `homeFadeOutCurve`），所以这 100ms 里用户看到的内容**没有变暗**。
//
// ⚠️ **这一段不走框架的 `Exit`**（理由见文件头《⚠️ 退场那一段自己淡出》）。
const homeFadeOut = 100 * time.Millisecond

// homeFadeIn 是**进场**那一段的时长：新的那一块从全灭到满亮。
//
// ⚠️ **和 `homeBodyDuration` 是同一个数**（200ms）：那一段是内容淡入的时长，
// 这里也是。位置那一半（升上来 8 DIP）是框架的 `Enter` 走的，时长同一个。
const homeFadeIn = homeBodyDuration

// homeFadeInLead 是**进场那一段提前多久开始算**。
//
// ⚠️ **它是为了不留空帧**：旧的那一块淡到 0 的那一帧，屏幕上如果**什么都没有**，
// 用户看见的是一下「闪」（实测那一帧只剩页面底色——票 #368 当初要消掉的正是
// 这种「闪」）。提前**一帧**开始算，新的一块在那一帧已经有四成（统一曲线前重
// 后轻，实算 16ms 是 42%），于是**内容那一块始终有东西**。
//
// ⚠️ **它不违反「中间不同时画两层」**：旧的那一块在交接那一帧**已经不建了**
// （它是被换掉的，不是被淡掉的），而新的一块在**交接之前**根本没有建——所以
// 任何一帧上**只有一层**（§五《规则》第 10 条）。
//
// ⚠️ **这正是交叉淡入淡出时代的样子**：那一版里新的一块从**第一帧**就在（只是
// 淡入），16ms 之后也是四成左右。改的是「旧的那一块还在不在」，不是「新的
// 那一块多快露面」。
const homeFadeInLead = 16 * time.Millisecond

// homeBodyRise 是新内容进场时从下面升上来的幅度（DIP）。
//
// ⚠️ **8 是「读得出动了、读不出方向」的那个数**：再大就成了一次「滑动」
// （DESIGN.md §五《规则》第 5 条：元素自身的变化不应太夸张），再小就只是
// 「凭空出现」。
const homeBodyRise = 8

// homeBodyTransition 是切 Tab 时内容那一块的进出场。
//
// ⚠️ **只写 `Enter`，不写 `Exit`**（票 #238）：写了 `Exit`，框架就会**复制一份
// 上一帧的样子**让它走完（`mygo/ui/transition.go` 的 `ghostOf`）——那份复制品
// 要**重新排一遍旧列表的文字**（切 Tab 之后的第一帧因此一次性多分配 185 KB），
// 而且它**和新的那一块同时在画面上**（重影）。不写 `Exit`，旧的那一块在退场
// 那一段照常建，淡出是它自己的不透明度。
//
// ⚠️ **`Enter` 的 `Opacity` 是 1，不是 0**：不透明度是**我们自己**算的
// （`homeFadeInCurve`），而框架这一半只管**位置**（升上来 8 DIP）。写成 0 的
// 的话两边的淡入会**叠乘**（新的那一块会淡入两遍——出来得比预期慢一倍）。
//
// ⚠️ **`Opacity: 1` 的另一个后果**：框架判断「要不要动」看的是 `Motion.fades`
// （`mygo/ui/transition.go`），而它是「不透明度小于 1」——所以系统开着「减少
// 动态效果」时这一半**整个不跑**（新内容直接到位，不升上来）。那正是「减少
// 动态效果」要的样子。
var homeBodyTransition = ui.ElementTransition{
	Duration: homeBodyDuration,
	Ease:     motion.Unified,
	// 进来的那一块：从下面 8 DIP 处升上来（不透明度是 1——淡入是我们自己算的）。
	Enter: &ui.Motion{Opacity: 1, Y: homeBodyRise},
}

// homeBodyKeys 是内容那一块三档的 key。
//
// ⚠️ **存成 `any` 是为了一个分配都不多**：key 是 `any`，而把一个字符串装进
// `any` 要一次堆分配（`runtime.convTstring`）——每帧一次就是首页那一档每帧
// 多一次分配，而那一档的预算是有数的余量（`framebudget_test.go`：实测 46 次
// / 上限 54 次）。初始化的时候装好，每帧只是取一个。
//
// ⚠️ **别改回 `strconv.Itoa` 拼出来的那种**：那一下是两次分配（拼字符串 +
// 装箱），首页那一档每一帧都多两次——而那一档的余量只有八次。
var homeBodyKeys = [homeTabCount]any{
	"home:body:0",
	"home:body:1",
	"home:body:2",
	"home:body:3",
	"home:body:4",
	"home:body:5",
}

// homeSwapPhase 是切 Tab 那一段现在处在哪一段。
//
// # 为什么不是一个 bool
//
// 「在不在换」和「换到哪一段了」是**两件事**：退场那一段里**旧的那一块还在
// 画面上**（它在淡出），而进场那一段里**旧的那一块已经没了**（新的正在淡入）。
// 「换没换」一句话说不清——那一段的两头画的是**不同的东西**。
type homeSwapPhase uint8

const (
	// homeSwapIdle 是「没在换」：画面上那一档就是选中那一档。
	homeSwapIdle homeSwapPhase = iota
	// homeSwapOut 是「旧的正在退」：旧的那一块在淡出，**新的那一块还不建**。
	homeSwapOut
	// homeSwapIn 是「新的正在进」：旧的那一块已经没了，新的在淡入。
	homeSwapIn
)

// homeSwap 是切 Tab 那一段的状态。
//
// ⚠️ **它是这一段唯一的记忆**：画面上画的是哪一档（`homePage.shown`）和这一
// 段处在哪一段，两样都在这儿。退场那一段里 `shown` 还是旧的那一档（它在淡
// 出），进了 `homeSwapIn` 才换成新的那一档。
type homeSwap struct {
	// phase 是现在处在哪一段。
	phase homeSwapPhase
	// to 是要换过去的那一档。
	to int
	// at 是这一段开始的时刻（框架的钟，见 `syncSwap`）。
	at time.Time
	// from 是**退场这一段开始时**的不透明度：被打断时从当前值接着走（§五《规则》
	// 第 3 条：动画要可打断）。
	from float32
	// alpha 是此刻的不透明度（1 = 满亮）。
	alpha float32
}

// homeFadeOutCurve 是退场那一段此刻的不透明度。
//
// ⚠️ **走的是同一条曲线的「剩下的时间」**（`motion.Unified(1 - t/时长)`），
// **不是**正着走。正着走（`motion.Unified(t/时长)`）的话旧内容在**前 10%**
// 的时间里就只剩一半了——这条曲线前重后轻（DESIGN.md §五《一条统一曲线》，
// 前 50% 的时间走完 95.4%）——而新的那一块要等它**淡完**才进（§五《规则》
// 第 10 条），于是中间会**空出**一大截（实算：200ms 的退场会空出 **120ms**，
// 屏幕上只剩底色）。
//
// 反过来走的话旧内容**一直亮到最后一刻**：100ms 的退场里，前 75ms 都在 80%
// 以上（实算），而新的那一块紧接着就顶上——退场的最后 25ms 从 80% 掉到 0，
// 进场 16ms 就到 42%（这条曲线前重后轻；见 `homeFadeInLead`）。
//
// ⚠️ **这是同一条曲线**（`motion.Unified`），不是另立一条：差别只在**喂进去
// 的那个数**是「走过的」还是「剩下的」。
func homeFadeOutCurve(elapsed time.Duration) float32 {
	if elapsed >= homeFadeOut {
		return 0
	}
	return motion.Unified(1 - float32(elapsed)/float32(homeFadeOut))
}

// homeFadeInCurve 是进场那一段此刻的不透明度。
//
// ⚠️ **正着走**（`motion.Unified(t/时长)`）：新内容要**尽快露面**——这条曲线
// 前 50% 的时间走完 95.4%，所以 200ms 的进场在 16ms 之后就有四成（实算）。
//
// ⚠️ **那 16ms 是提前的**（`homeFadeInLead`）：交接那一帧新的一块已经有四成，
// 于是内容那一块**不留空帧**。
func homeFadeInCurve(elapsed time.Duration) float32 {
	t := elapsed + homeFadeInLead
	if t >= homeFadeIn {
		return 1
	}
	return motion.Unified(float32(t) / float32(homeFadeIn))
}

// alphaAt 是切 Tab 那一段**此刻**的不透明度（1 = 满亮）。
//
// ⚠️ **它一律由曲线算出来**，不是「上一帧是多少」——这一点是这一票踩过的坑：
// 阶段刚换的那一帧如果**忘了算**，不透明度就是**零值**，而 0 的意思是「整棵
// 子树不画」（框架的 `mygo/ui/paint.go`：`opacity<=0.001` 就跳过）——于是那一帧
// 屏幕上只剩页面底色，用户看见的是一下「闪」（真窗口里每次切 Tab 都有）。
//
// ⚠️ **退场那一段的第一帧是满亮**（`from * homeFadeOutCurve(0)` = 1）：旧的那一
// 块在退场的第一帧**还是满亮的**，它从第二帧开始淡。
func (s homeSwap) alphaAt(now time.Time) float32 {
	switch s.phase {
	case homeSwapOut:
		return s.from * homeFadeOutCurve(now.Sub(s.at))
	case homeSwapIn:
		return homeFadeInCurve(now.Sub(s.at))
	}
	return 1
}

// syncSwap 推切 Tab 那一段的阶段机，返回**这一帧**内容那一块的不透明度。
//
// ⚠️ **阶段机在 view 里跑**（同队列抽屉的 `syncDrawerPhase`，见 queue_drawer.go 的
// `drawerPhase` 那一段）：这一段是**画面上的**事——什么时候换、这一刻有多透
// 明——而它要看的是**这一帧**的时刻（`c.Now()`，框架的钟）。放在 sync 那一层
// 就得另带一个钟（`s.clock`），而两个钟差一帧（测试里推一个不推另一个，那一
// 帧的画面就对不上）。
//
// ⚠️ **「换到哪一档」和「它到了没有」是 sync 层算的**（`h.want` / `h.wantReady`，
// 见 homepage.go 的 `syncHome`）——**不是这里算的**：这里（view 层）看到的
// `h.tab` 是**这一帧**才写下的（框架把点中的下标写在画 Tab 那一排的时候），
// 而那一档的请求是**下一帧**才发出去的（`pull()` 在 sync 层里跑）——所以在
// 这里判断「它到了没有」，那一刻 `loadingOf` 还是假，于是会以为「它已经有着落
// 了」而立刻换过去，换过去之后屏幕上就是「正在加载 XXX 推荐」那一格（票 #368
// 明令不许出现的）。
//
// ⚠️ **它推着三样东西**：用户的意图（`h.want`）、数据到没到（`h.wantReady`）、
// 动画走没走完。**动画不会反过来推它**（那是「重入」，见 queue_drawer.go 的
// `drawerPhase` 那一段）。
//
// ⚠️ **系统开着「减少动态效果」时整段跳过**（`c.Preferences().ReduceMotion`）：
// 那时候新内容**直接到位**，不淡入也不升上来。框架那一半也听系统的（见
// `homeBodyTransition`），两半都听。
func (h *homePage) syncSwap(c *ui.Context) float32 {
	now := c.Now()

	switch h.swap.phase {
	case homeSwapIdle:
		if h.wantReady && h.want != h.shown {
			if c.Preferences().ReduceMotion {
				// 减少动态效果：直接换，不淡。
				h.shown = h.want
				return 1
			}
			// 开始退场：旧的那一块从这一刻起淡出去（新的那一块还不建）。
			h.swap = homeSwap{phase: homeSwapOut, to: h.want, at: now, from: 1}
		}
	case homeSwapOut:
		if h.wantReady && h.want == h.shown {
			// 用户又点回**原来那一档**：取消这一段。旧的那一块还在屏幕上（它
			// 只是淡到一半），不该「淡出去又淡回来」——那是一下闪。
			h.swap = homeSwap{}
			break
		}
		if h.wantReady && h.want != h.swap.to {
			// 改主意了：还是退场，只是**退到另一档**去（`to` 换了，那一段接着走）。
			h.swap.to = h.want
		}
		if now.Sub(h.swap.at) >= homeFadeOut {
			// 旧的那一块淡完了 → 换到新的那一块，开始进场。
			h.shown = h.swap.to
			h.swap = homeSwap{phase: homeSwapIn, at: now}
		} else {
			// 🔴 **这一段要一帧一帧地重画**：框架不知道我们在动（没有 `Exit`、
			// 没有 `Enter` 在跑），不主动要下一帧的话画面就**停在这一帧**——
			// 症状是「切 Tab 之后旧的那一块卡在那儿不动了」。
			c.AnimationFrame()
		}
	case homeSwapIn:
		if h.wantReady && h.want != h.shown {
			// 用户又切了：**打断**这一段（§五《规则》第 3 条：动画要可打断），
			// 从当前的不透明度接着退（`from` 是当前值，不是 1）。
			h.swap = homeSwap{phase: homeSwapOut, to: h.want, at: now, from: h.swap.alpha}
			break
		}
		if now.Sub(h.swap.at) >= homeFadeIn {
			h.swap = homeSwap{}
		} else {
			c.AnimationFrame()
		}
	}
	// ⚠️ **不透明度一律由曲线算**（见 `alphaAt`）：阶段刚换的那一帧**忘了算**
	// 就是零值，而 0 的意思是「整棵子树不画」——屏幕上只剩页面底色。
	h.swap.alpha = h.swap.alphaAt(now)
	if h.swap.phase == homeSwapIdle {
		return 1
	}
	return h.swap.alpha
}

// viewBody 画「内容那一块」。
//
// ⚠️ **这一块带 Key**：切 Tab 时它整个换掉（key 跟着「画的是哪一档」走），
// 框架靠这个认出「旧的那一块没了、新的这一块来了」，于是新的按 `Enter` 走
// （升上来 8 DIP，见 `homeBodyTransition`）。
//
// ⚠️ **key 里带的是「画的是哪一档」，不是「选中的是哪一档」**：选中那一档还
// 在拉的时候，画面上画的是**上一档**（见 `syncSwap` 的 `ready`）——那一块不该
// 换，也不该动。
//
// ⚠️ **不透明度是我们自己算的**（`syncSwap` 返回的那个数）：退场那一段是旧
// 的那一块**自己的**不透明度（框架的 `Exit` 会复制一份上一帧的样子，那一份
// 要重排旧列表的文字——见文件头《⚠️ 退场那一段自己淡出》）。
func (h *homePage) viewBody(c *ui.Context, s *Shell) {
	// ⚠️ **先推阶段机**：它决定这一刻画面上画的是哪一档、有多透明。
	alpha := h.syncSwap(c)
	tab := h.shown
	ui.Column(c).Key(homeBodyKeys[tab]).Grow(1).Gap(space.Px12).
		Transition(homeBodyTransition).Opacity(alpha).Children(func() {
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
			h.newGrid.SetStatus(h.feed.newTab.loading, h.feed.newTab.err)
		}

		h.viewTabBody(c, s, tab)
	})
}

// tabPending 是「这一档的内容还在路上」。
//
// ⚠️ **「还在拉」和「拉到了但是空的」是两件事**（见 `viewTabBody` 那四种状态）：
// 只有「还在拉」才留得住旧内容；「拉到了但是空的」要立刻把空状态画出来。
//
// ⚠️ **「流行」那一档不再等「新歌速递」**（票 #238）：此前这一档**多**了一块
// 「新歌速递」，它也在拉的时候整档就不出现——于是「音乐榜到了、流行自己的歌
// 也到了」的用户看到的仍然是**上一档的内容**，而**全程没有任何反馈**（那一块
// 自己的「正在加载」永远轮不上，因为整档都没画出来）。
//
// 现在这一档**自己的歌**到了就换过去，而**新歌速递那一块自己显示它的「正在
// 加载」**（`homepage_newmusic.go` 的三态）——用户看得见**哪一块在等**。
func (h *homePage) tabPending(tab int) bool {
	// 这一档的歌还在拉，而手上还没有歌。
	//
	// ⚠️ **状态是翻页列表自己说的**（`list.PagedList.Paged()`）：三档的内容在
	// 各自的翻页列表里（票 #370），而「还在拉」和「拉到了但是空的」在那一层
	// 里分得最清。
	pg := h.lists[tab].Paged()
	return pg.Loading && pg.Loaded == 0
}
