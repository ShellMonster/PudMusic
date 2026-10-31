// 首页切 Tab 的**横向滑动**（票 #706）。
//
// # 这一票把票 #238 的裁决推翻了
//
// 此前切 Tab 是**串行两段**：旧的淡出 100ms → 新的淡进 200ms（票 #238）。
// 票 #238 把「同一时刻画面上有两层内容」判为**重影缺陷**（它的 F1）。
//
// **现在推翻那个裁决**（用户明确同意）。理由是：
//
//	票 #238 的判据把「两层同时在画面上」和「两层**叠在同一位置**」当成一件事。
//	淡入淡出时两层**确实叠在同一位置**（新的一层盖在旧的一层上面）——那是重影。
//	横向滑动时两层**在空间上不重叠**：旧的一层往左走，新的一层从右边进来，
//	两者各占一段，**中间没有重叠**——那不是重影，是 ViewPager。
//
// ⇒ **旧的先走、新的后进那一段被删掉了**：两档**同时**在画面上，**同时**动，
// **同起同落**。
//
// # 两档并存 = 两档内容都要建
//
// 旧的淡出那一段里**只建一档**（那是票 #238 的省法），而滑动这一段
// **两档都在树上**——旧的那一档滑出去、新的那一档滑进来。
//
// ⇒ **代价是分配**（见 `framebudget_test.go` 那一档）：多一档是 **+32 次 /
// +528 字节**（实测：一档的稳态是 134 次 / 4704 字节，滑动那一段每一帧是
// 166 次 / 5232 字节）。这不是翻倍——多出来的那部分是**第二档的壳**（表头 +
// 列表 + 它的滚动容器），不是它的全部行。
//
// # ⚠️ **动的是布局位置，不是 transform**
//
// 每一档的横向偏移是**布局位置**（`Left(x)`），不是一层 transform。理由是
// DESIGN.md §五《不做》那条：
//
//	**动画不要用 transform 动大子树**——软件渲染回退时会整棵子树逐像素重绘
//	（第三方实测差两个数量级）。要动就动**布局位置**。
//
// 内容那一块是**一整屏**（几十行歌），正是一棵「大子树」——所以它走布局位置。
//
// ⚠️ **`Left(x)` 是「相对布局位置的偏移」，不是绝对坐标**（票 #706 的 P0 修复
// 踩的就是这个坑）：框架对**流里**的元素是「从布局摆好的地方**再**往右挪」
// （`mygo/ui/layout.go` 的 `flexLayout`：`dx, dy := c.relative(cw, ch)`——它把
// `dx` **加**在流内位置上）。所以在一条**行**里，第二个子元素的流内位置**已经
// 是第一段的宽度**——再叠一个 `Left(width)`，它的最终 x 就是 **2 × width**。
//
// ⚠️ **代价是每帧重建**（布局位置被 view 读了 ⇒ 每一帧重新排版）。实测
// （探针量过，量法照 `framebudget_test.go` 那一档）：滑动那一段每一帧是
// **166 次 / 5232 字节**，而一档的稳态是 **134 次 / 4704 字节**——**+32 次 /
// +528 字节**（不是翻倍：多出来的是**第二档的壳**，不是它的全部行）。
//
// ⚠️ **「走 transform 更便宜」这条路在这个仓库里不成立**：仓库自己的判据是
// （`widgets/hover/hover.go` 的注释、`internal/ui/switch.go`）：**那个动画值
// 还会不会被 view 读**——会被读就还是每帧重建整棵界面，就白换。而内容块的
// 位移**必然被读**（要按它摆两档的位置）。
//
// # 指示器和内容**同起同落**
//
// 用户要的是「顶部与底部动画同步进行」。做法是**两边用同一套动画参数**：
//
//	曲线   motion.Unified（值只写一处，`internal/ui/widgets/motion`）
//	时长   homePagerDuration（200ms）
//
// ⇒ 两边的进度在**任何时刻**都相等（同一个曲线、同一个时长、同一个起点）。
//
// ⚠️ **框架没有跨元素共享的原语**（`mygo/ui/motion.go` 的 `channel` 是**每元素
// 私有**的），所以「同步」只能是「参数一样」，不是「共用一个时间源」。
//
// ⚠️ **指示器那一处从弹簧改成曲线了**（此前是 `motion.TabSlide` 那条弹簧）：
// 弹簧走 453ms 才收手，而曲线走 200ms——**两者不同起同落**。
// 而「弹簧保留速度、连点三个 Tab 时接着走」那条好处，在**曲线**这一处也
// 有（框架的曲线分支被打断时**从当前值接着走**，见 `mygo/ui/motion.go` 的
// `channel.retarget`）——只是它「重新起速」而不是「保留速度」。
// 理由与取舍见 DESIGN.md §五《首页切 Tab：横向滑动》那一节。
package ui

import (
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// homePagerDuration 是切 Tab 时**指示器和内容一起**走完要多久。
//
// ⚠️ **两边用同一个数**：用户要的是「顶部与底部动画同步进行」（同起同落），
// 而框架的动画通道是**每元素私有**的（`mygo/ui/motion.go`），没有跨元素共享
// 的时间源——所以「同步」只能靠「参数一样」实现：
//
//	曲线   motion.Unified（两边同一个）
//	时长   homePagerDuration（两边同一个）
//
// ⇒ 两边的进度在**任何时刻**都相等。
//
// ⚠️ **200ms 是抄「悬停淡入」那一档**（DESIGN.md §五《逐处》那张表）：那是
// 本仓库「一块内容换掉」的时长。而**指示器那一侧从弹簧改成曲线**了——弹簧走
// 453ms（实测），和内容的 200ms 不同起同落。
const homePagerDuration = 200 * time.Millisecond

// homePagerAnim 是切 Tab 那一段的动画参数。**指示器和内容用同一个。**
//
// ⚠️ **它是包级变量，不能每帧现造**：`motion.Unified` 是一个闭包，每帧调一次
// 就是每帧一个分配——而每帧分配是本仓库唯一的性能判据（`framebudget_test.go`）。
//
// ⚠️ **走曲线那一支**：`Duration > 0` 才是曲线；`Duration == 0` 落进弹簧那一支，
// 而弹簧那一支的兜底是 500ms（`mygo/ui/motion.go` 的 `defaultSpringDuration`）。
// 「零值 = 500ms 弹簧」不是「没动画」。
var homePagerAnim = ui.Animation{Duration: homePagerDuration, Ease: motion.Unified}

// homeBodyKeys 是内容块**每一档**的 key。
//
// ⚠️ **存成 `any` 是为了一个分配都不多**：key 是 `any`，而把一个字符串装进
// `any` 要一次堆分配（`runtime.convTstring`）——每帧一次就是首页那一档每帧
// 多一次分配，而那一档的预算是有数的余量（`framebudget_test.go`）。初始化
// 的时候装好，每帧只是取一个。
//
// ⚠️ **别改回 `strconv.Itoa` 拼出来的那种**：那一下是两次分配（拼字符串 +
// 装箱），首页那一档每一帧都多两次——而那一档的余量只有几次。
//
// ⚠️ **每一档一个 key（而不是整块一个）**：滑动那一段**两档同时在树上**，而
// 框架靠 key 认出「哪一档是哪一档」——三档的内容各有各的滚动位置
// （`list.PagedList` 各自记着），key 相同的话它们会**抢同一份**。
var homeBodyKeys = [homeTabCount]any{
	"home:body:0",
	"home:body:1",
	"home:body:2",
	"home:body:3",
	"home:body:4",
	"home:body:5",
}

// homePagerGeom 是切 Tab 那一段**此刻**两档的几何位置。
//
// ⚠️ **它是给测试看的**（`homepage_pager_test.go`）：画面上量不到「两档各在
// 哪儿」——`tt.Find` 报的是**裁切之后**的盒子（框架的 `commitElement` 把标签
// 的盒子和祖先的裁切区求交，见 `mygo/ui/runtime.go`），所以滑出去的那一档
// 报出来是**被裁过的**，量不出它滑了多远。
//
// ⇒ 所以这一段把算出来的位置**记一份**，测试照它断。
//
// ⚠️ **它是「这一帧算出来的那个数」，不是「画面上量出来的那个数」**——而
// 那两者**必须一致**：位置是 `homePagerOffsets` 算的，而它算完就照着摆。
type homePagerGeom struct {
	// oldX / newX 是两档各自的横向位置（相对内容块的左边沿）。
	oldX, newX float32
	// oldTab / newTab 是两档各自是哪一档（-1 表示「那一档没画」）。
	oldTab, newTab int
	// width 是内容块有多宽（一档就多宽）。
	width float32
	// progress 是这一段此刻的进度（0 = 刚开始，1 = 走完）。
	progress float32
}

// homePagerView 是切 Tab 那一段的**可观测状态**。
//
// ⚠️ **它只是「这一帧算出来的那些数」的快照**，不参与画法——画法照
// `homePagerOffsets`。
//
// ⚠️ **它在画完之后才更新**（`viewPager` 的最后一句）：所以它报的是**这一帧
// 画出来的那一帧**的数。
var homePagerView homePagerGeom

// # 为什么不是一个 bool
//
// 「在不在换」和「换到哪一段了」是**两件事**：滑出去的那一段里**两档都在画面
// 上**（旧的一档往左走、新的那一档从右边进来），而**两档各占一段、空间上不
// 重叠**。而「不换」那一段里**只有一档**（画着的那一档）。
type homePagerPhase uint8

const (
	// homePagerIdle 是「没在换」：画面上只有画着的那一档。
	homePagerIdle homePagerPhase = iota
	// homePagerSlide 是「在滑」：两档都在画面上，旧的往左走，新的从右边进来。
	homePagerSlide
)

// homePager 是切 Tab 那一段的状态。
//
// ⚠️ **它是这一段唯一的记忆**：画面上画的是哪一档（`homePage.shown`）、条带
// **右段**那一档是哪一档（`to`）、这一段从什么时候开始（`at`）、这一段从
// 哪个进度起算（`p0`）、以及这一段是往前走还是往回走（`back`）。
type homePager struct {
	// phase 是现在处在哪一段。
	phase homePagerPhase
	// to 是**条带右段**（第二段）那一档——**正滑进来的那一档**。
	//
	// ⚠️ **条带左段那一档不是它**：左段那一档是 `homePage.shown`（滑之前就在
	// 画面上的那一档），而它在整个这一段里**不变**（滑完了才换成新的那一档）。
	//
	// ⚠️ **「滑回去」（反悔）那一段里 `to` 也不变**：那时往回走的是**左段**
	// 那一档（它滑回原地），而 `to` 那一档**往右退出去**。
	to int
	// at 是这一段开始的时刻（框架的钟，见 `syncPager`）。
	at time.Time
	// back 说这一段是**滑回去**（用户反悔了，见 `syncPager` 那一支）：
	// 进度从 p0 往回走到 **0**（左段那一档回到原地）。
	back bool
	// p0 是**这一段**开始时的进度（0 = 左段那一档在原地，1 = 右段那一档在原
	// 地）。
	//
	// ⚠️ **它是「这一段」的起点，不是「这一次切换」的起点**：改主意（换一档
	// 去，或者反悔）会**另起一段**，而那一段**从当前进度接着走**（不跳）。
	p0 float32
}

// homePagerOffsets 是切 Tab 那一段**此刻**两档各自的横向位置。
//
// 返回的顺序是「左段（滑走的那一档）、右段（滑进来的那一档）」。
//
// ⚠️ **它拿的是「过了多久」，不是「此刻几点」**：位置因此**只由「这一段过了多久」
// 决定**，而**与「这一段什么时候开始的」无关**——那是「位置只由曲线算、不由上一
// 帧是多少算」的**结构性**保证（框架的 `Exit` 那一半要复制一份上一帧的样子，见
// `mygo/ui/transition.go` 的 `ghostOf`，实测 204 KB）。
//
// ⚠️ **它一律由曲线算出来**，不是「上一帧是多少」：阶段刚换的那一帧如果**忘
// 了算**，位置就是**零值**（0），而 0 的意思是「两档都还在原地」——于是那一帧
// 两档**叠在同一位置**（那正是票 #238 说的重影，只不过是瞬时的）。
//
// ⚠️ **两档是紧挨着的**：右段那一档的左边沿**正好**在左段那一档的右边沿右边
// 一个容器宽的地方——即 `newX == oldX + width`。既不重叠，也不留缝（ViewPager
// 的样子）。
//
// ⚠️ **q 是「条带挪出去多远」**（0 = 左段那一档在原地，1 = 右段那一档在原地）：
// 左段那一档在 `-width·q`、右段那一档在 `width·(1-q)`——两者的差恒等于容器宽。
func homePagerOffsets(s homePager, elapsed time.Duration, width float32) (oldX, newX float32) {
	if s.phase != homePagerSlide {
		return 0, 0
	}
	q := homePagerSegmentProgress(s, elapsed)
	return -width * q, width * (1 - q)
}

// homePagerSegmentProgress 是切 Tab 那一段**此刻**的进度 q（0 = 左段那一档在
// 原地，1 = 右段那一档在原地）。
//
// ⚠️ **它是纯函数**：不碰界面状态，不看钟——调用方把时刻传进来。
//
// ⚠️ **它算的是「这一段」**（`s.p0` → 目标），不是「这一次切换」：改主意会**另
// 起一段**，而那一段**从当前进度接着走**（不跳）。
func homePagerSegmentProgress(s homePager, elapsed time.Duration) float32 {
	// u 是这一段的归一化进度（0 = 刚开始，1 = 走完），走的是那条统一曲线。
	u := homePagerProgress(elapsed)
	if s.back {
		// ⚠️ **往回走**（用户反悔了）：进度从 `p0` 退回到 **0**（左段那一档回到
		// 原地）。
		//
		// ⚠️ **它和指示器同起同落**：指示器那一条在目标变了的时候**从当前值接着
		// 走**（框架的曲线分支：见 `mygo/ui/motion.go` 的 `channel.retarget`），
		// 而它的当前值和这里的 `p0` 是同一个数——于是两边的归一化进度**逐帧
		// 相等**（见 `homepage_pager_test.go` 的那条）。
		return s.p0 * (1 - u)
	}
	// 往前走：从 `p0` 走到 **1**（右段那一档到原地）。
	return s.p0 + (1-s.p0)*u
}

// homePagerProgress 是切 Tab 那一段此刻的进度（0 = 刚开始，1 = 走完）。
//
// ⚠️ **它是纯函数**：不碰界面状态，不看钟——调用方把时刻传进来。
// 测试用它断「指示器和内容同起同落」（见 `homepage_pager_test.go`）。
func homePagerProgress(elapsed time.Duration) float32 {
	if elapsed >= homePagerDuration {
		return 1
	}
	if elapsed <= 0 {
		return 0
	}
	return motion.Unified(float32(elapsed) / float32(homePagerDuration))
}

// syncPager 推切 Tab 那一段的阶段机，返回**这一帧**两档各自该在哪儿。
//
// 返回的两个下标是「旧的那一档」和「新的那一档」（-1 表示「那一档不画」）。
//
// ⚠️ **阶段机在 view 里跑**（同队列抽屉的 `syncDrawerPhase`，见
// queue_drawer.go 的 `drawerPhase` 那一段）：这一段是**画面上的**事——什么时候
// 换、这一刻两档在哪儿——而它要看的是**这一帧**的时刻（`c.Now()`，框架的
// 钟）。放在 sync 那一层就得另带一个钟（`s.clock`），而两个钟差一帧（测试里
// 推一个不推另一个，那一帧的画面就对不上）。
//
// ⚠️ **「换到哪一档」和「它到了没有」是 sync 层算的**（`h.want` / `h.wantReady`，
// 见 homepage.go 的 `syncHome`）——**不是这里算的**：这里（view 层）看到的
// `h.tab` 是**这一帧**才写下的（框架把点中的下标写在画 Tab 那一排的时候），
// 而那一档的请求是**下一帧**才发出去的（`pull()` 在 sync 层里跑）——所以在
// 这里判断「它到了没有」，那一刻 `loadingOf` 还是假，于是会以为「它已经有着落
// 了」而立刻换过去。
//
// ⚠️ **系统开着「减少动态效果」时整段跳过**（`c.Preferences().ReduceMotion`）：
// 那时候新内容**直接到位**，不滑。
func (h *homePage) syncPager(c *ui.Context, width float32) (g homePagerGeom) {
	now := c.Now()
	reduce := c.Preferences().ReduceMotion
	// ⚠️ **「那一档没画」是 -1**（不是零值）——零值是「第 0 档」。
	g.oldTab, g.newTab = -1, -1

	switch h.swap.phase {
	case homePagerIdle:
		if h.want != h.shown {
			if reduce {
				// 减少动态效果：直接换，不滑。
				h.shown = h.want
				return g
			}
			if width <= 0 {
				// ⚠️ **容器宽还没量到**（第一帧）：滑不了——两档要错开多远，
				// 靠的就是这个宽。这一帧先不动，下一帧量到了再滑。
				c.AnimationFrame()
				return g
			}
			// ⚠️ **不等数据到货**：点了就滑。
			//
			// 此前（票 #238 那一套）要等那一档**有着落了**才换（`wantReady`），
			// 于是「切到一档而它还在拉」的时候屏幕上留着**上一档**的内容。
			// 而这一票的横向滑动**不那样**：新的那一档**滑进来**，画的是**骨架
			// 屏**（内容还没到，但**形状已知**——一屏歌曲行）。
			//
			// ⇒ **「不闪」那一条是靠骨架屏兑现的**，不是靠「留着旧内容」。
			h.swap = homePager{phase: homePagerSlide, to: h.want, at: now}
		}
	case homePagerSlide:
		if h.want == h.shown {
			// 用户又点回**原来那一档**：**反悔了**。
			//
			// ⚠️ **不瞬时归位**：那一段**接着往回走**（DESIGN.md §五《规则》第 12
			// 条：「动画必须可打断、**可反悔**——中途改变主意时，界面随用户调度」）。
			//
			// 而**指示器那一条本来就在往回走**（框架的曲线分支被打断时**从当前值接着
			// 走**，见 `mygo/ui/motion.go` 的 `channel.retarget`）——所以内容这一段
			// **必须跟着走**：否则两段**不同起同落**（指示器慢慢滑回去、内容啪地一下
			// 跳回原地）。
			//
			// ⚠️ **从当前进度接着往回走**（不跳回起点）：新起一段，那一段的起点
			// `p0` 就是此刻的进度。
			if reduce {
				// 减少动态效果：直接归位（指示器那一条框架自己也会直接给到目标）。
				h.swap = homePager{}
				break
			}
			q := homePagerSegmentProgress(h.swap, now.Sub(h.swap.at))
			if q <= 0 {
				// 已经在原地了（没什么可滑回去的）：收工。
				h.swap = homePager{}
				break
			}
			h.swap.back = true
			h.swap.p0 = q
			h.swap.at = now
		} else if h.want != h.swap.to {
			// 改主意了：还是滑，只是**滑到另一档**去（`to` 换了）。
			//
			// ⚠️ **旧的那一档从当前进度接着走**（位置一律由曲线算，而曲线的
			// 起点没有变）——那是「动画可打断」（DESIGN.md §五《规则》第 3 条）。
			//
			// ⚠️ **刚才是「往回走」而现在又改主意去另一档**：方向要翻回来（`back`
			// 去掉），而**起点还是此刻的进度**（不跳）。
			if h.swap.back {
				q := homePagerSegmentProgress(h.swap, now.Sub(h.swap.at))
				h.swap.back = false
				h.swap.p0 = q
				h.swap.at = now
			}
			h.swap.to = h.want
		}
	}

	if h.swap.phase == homePagerIdle {
		return g
	}

	elapsed := now.Sub(h.swap.at)
	if elapsed >= homePagerDuration {
		if h.swap.back {
			// 滑回原地的这一段走完了：回到「只有画着的那一档」。
			h.swap = homePager{}
			return g
		}
		// 走完了：换到新的那一档。
		//
		// ⚠️ **这一帧只画新的那一档**：旧的那一档在走完的那一刻整个在容器
		// 外面（进度是 1，位置是 -width），而**两档的 key 相同的话框架会
		// 报错**（两个同 key 的元素共用一份状态，见 `mygo/ui/context.go` 的
		// `rekey`）——`shown` 和 `to` 在这一刻是同一档，所以只画一档。
		h.shown = h.swap.to
		h.swap = homePager{}
		return g
	}
	// 🔴 **这一段要一帧一帧地重画**：框架不知道我们在动（内容块的位移
	// **不是**框架的 `AnimateTo`——那是我们自己按曲线算的），不主动要
	// 下一帧的话画面就**停在这一帧**。
	c.AnimationFrame()

	q := homePagerSegmentProgress(h.swap, elapsed)
	ox, nx := homePagerOffsets(h.swap, elapsed, width)
	return homePagerGeom{
		oldX: ox,
		newX: nx,
		// ⚠️ **左段那一档是 `shown`**（滑之前就在画面上的那一档），**右段那一档是
		// `to`**（正滑进来的那一档）——而「滑回去」那一段里位置反过来：左段那一
		// 档**滑回原地**，右段那一档**往右退出去**。
		oldTab: h.shown,
		newTab: h.swap.to,
		width:  width,
		// ⚠️ **progress 是「条带挪出去多远」**（0 = 左段那一档在原地，1 = 右段
		// 那一档在原地）——指示器那一条的进度和它**同一个数**（同起同落）。
		progress: q,
	}
}

// viewPager 画「内容那一块」——**横向滑动**的那一版。
//
// # 结构：一个裁切的容器，里面是**一条**两档宽的**条带**，整条挪动
//
//	┌─────────────────────────┐  ← 容器（裁切，宽度 = 一档）
//	│  [旧的一档][新的那一档] │  ← 条带（宽度 = 两档），位置由曲线算
//	└─────────────────────────┘
//
// ⚠️ **两档在空间上不重叠**（旧的在左、新的在右），所以它们**不重影**——
// 这正是推翻票 #238 那个裁决的理由（见文件头）。
//
// ⚠️ **横向位置是布局位置**（`Left(x)`），不是 transform——见文件头
// 《⚠️ 动的是布局位置，不是 transform》。
//
// ⚠️ **整条挪，而不是各挪各的**：两档是**紧挨着的两段**（ViewPager 的样子），
// 而它们的位置**由同一个数算出来**（`homePagerOffsets`：旧的在 `-width·t`、
// 新的在 `width·(1-t)`）——那两条式子的差恒等于容器宽。
//
// 而框架的 `Left()` 挪的是**一个元素**——所以这里挪的是**装着两档的那条
// 带子**（`ui.Row`），两档在带子里**并排**（各占一段）。
//
// ⚠️ **带子比容器宽**（两档 = 两个容器宽）：所以它**溢出**容器，而容器
// `Clip()` 把它裁掉——滑出去的那一档因此看不见。
//
// ⚠️ **带子里的两档各占一段，各有各的 key**（`homeBodyKeys[tab]`）：框架靠 key
// 认出「哪一档是哪一档」，而三档的内容各有各的滚动位置（`list.PagedList` 各自
// 记着），key 相同的话它们会**抢同一份**。
func (h *homePage) viewPager(c *ui.Context, s *Shell) {
	// ⚠️ **宽度是**上一帧**量到的**（`LayoutBounds` 报的是上一帧的盒子）：
	// 两档横向错开的距离就是容器宽（它们紧挨着，各占一段）。
	//
	// ⚠️ **第一帧量不到**（那是零值）——而那时**还没在滑**（`want` 那时候还没
	// 算出来，见 `syncHome`），所以 0 只影响「还没量到」的那一帧。
	width := h.pagerBox.W

	g := h.syncPager(c, width)
	// ⚠️ **只取 `oldX`**：条带**整体**挪 `oldX`，而两档在带子里**并排**——
	// 所以右段那一档的位置是 `width + oldX`（= `newX`），而它**不用单独摆**。
	oldX, oldTab, newTab := g.oldX, g.oldTab, g.newTab

	// 容器：裁切（带子滑出容器时看不见）。
	//
	// ⚠️ **它不是一个滚动容器**（`ui.ScrollHorizontal`）：那一个的内容宽是
	// **自动算**的，而这里带子的位置是**我们自己算**的（要按曲线插值）——
	// 滚动容器的偏移不由我们说了算。
	//
	// ⚠️ **它是一个**行**：带子（也是一行）是它的子元素，而**行里的子元素在
	// 交叉轴上默认居中**（框架的 `Row()` 把 `align` 设成 Center）——所以这里
	// 显式 `AlignItems(ui.Stretch)`，让带子**占满容器的高**。
	//
	// ⚠️ **带子要占满容器高**：容器（一个**列**）的主轴是**竖的**，而列的子
	// 元素默认是**按内容高的**——不给它高度的话，它的高度就是**内容的高度**
	//（一屏几十行歌），而里面那一档的列表就**没有视口**，滚不动。
	box := ui.Row(c).Key("home:pager").Grow(1).FillWidth().AlignItems(ui.Stretch).Clip()
	box.Children(func() {
		// ⚠️ **带子永远在**：不滑的时候它里面**只有一档**（画着的那一档），滑的
		// 时候它里面**有两档**。
		//
		// ⚠️ **为什么不能「不滑的时候那一档直接挂在容器上」**：那样的话那一档的
		// **父元素**在切换的那一刻变了（从带子变成容器）——而框架按**父元素 +
		// key** 算元素的身份（`mygo/ui/context.go` 的 `keyedID`）——于是那一档
		// 的**身份变了**，它的行（列表的行是布局时建的）就要**重建**一遍。
		//
		// 实测：那一下是 **371 次 / 92 KB**（一帧）——而滑动那一段每一帧只有
		// **166 次 / 5232 字节**。⇒ **那一下比整段动画都贵**。
		//
		// ⚠️ **带子的宽度跟着档数变**（一档时一档宽、两档时两档宽）：那会改带子
		// 的 `layoutStyle.width`（框架的布局缓存拿它当判据，见
		// `mygo/ui/layoutcache.go`）——所以**每次切档都有一帧要重排**。而那一帧
		// 的代价是**一档**的（不是两档的），实测和稳态一样（134 次 / 4704 字节）。
		stripW := width
		if oldTab >= 0 {
			stripW = 2 * width
		}
		strip := ui.Row(c).Key("home:pager:strip").Width(stripW).FillHeight().
			AlignItems(ui.Stretch).Shrink(0)
		if oldTab < 0 {
			// 不在滑的时候：只画**画着的那一档**（`h.shown`）。
			//
			// ⚠️ **画着的那一档不一定是选中那一档**（票 #368）：选中那一档还在
			// 拉的时候，画面上留着上一档的内容。
			strip.Children(func() {
				h.viewPagerPage(c, s, h.shown, width)
			})
			return
		}
		// 在滑：两档都在，整条带子往左挪。
		//
		// ⚠️ **带子的位置是 `oldX`**：左段那一档在带子的第一段（从 0 开始），
		// 右段那一档在第二段（**流内位置已经是 width**）——所以带子往左挪
		// `oldX` 之后，左段那一档在 `oldX`、右段那一档在 `width + oldX`。
		//
		// ⚠️ **`newX == oldX + width`**（两档紧挨着），而 `oldX` 是负的——所以
		// 带子往左挪，旧的往左走、新的从右边进来。
		//
		// ⚠️ **带子里面那一列要「拉满」**（`AlignItems(ui.Stretch)`）：一行里的
		// 子元素**默认是居中**的（框架的 `Row()` 把 `align` 设成 Center），而
		// 那一档要**占满带子的高**（否则它的列表同样没有视口）。
		//
		// ⚠️ **带子必须是两档那么宽，而且不许缩**（`Width(2*width)` +
		// `Shrink(0)`）：框架的行**默认会缩**（`shrink = 1`，见
		// `mygo/ui/context.go`），而带子在一个 948 宽的行里放着两个 948 宽的
		// 子元素——**缩了它们就被挤成 474 一半**，于是每一帧都要按新宽度重排
		// 一遍旧列表的文字。
		//
		// 而带子**比容器宽**（两档 = 两个容器宽）——所以它**溢出**容器，而容器
		// `Clip()` 把它裁掉——滑出去的那一档因此看不见。
		if oldX != 0 {
			strip = strip.Left(oldX)
		}
		// ⚠️ **右段那一档的流内偏移是 0**——**不是 `width`**。
		//
		// ⚠️ **这一条踩过坑**（票 #706 的 P0 修复）：框架的 `Left(x)` 是**相对
		// 布局位置的偏移**，不是绝对坐标（`mygo/ui/layout.go` 的 `flexLayout`：
		// `dx, dy := c.relative(cw, ch)`——它把 `dx` **加**在流内摆好的位置上）。
		// 而带子是个**行**：左段那一档占满第一段（`width`），**右段那一档的流内
		// 位置已经是 `width`**——再叠一个 `Left(width)`，它的最终 x 就是
		// **2 × width**。
		//
		// ⇒ 那一档**整个在容器外面**，而容器裁着它 ⇒ **可见宽度恒为 0**——画面
		// 上看到的是「旧的一档缩成一条 → 一整片空白 → 新的一档啪地出现」，而**横
		// 向滑动根本没有发生**。
		//
		// ⇒ 所以这里**不再给右段那一档加 `Left`**：它在流内的第二段（x = width），
		// 正是「紧挨着左段那一档」该在的位置。
		strip.Children(func() {
			h.viewPagerPage(c, s, oldTab, width)
			h.viewPagerPage(c, s, newTab, width)
		})
	})
	// ⚠️ **量的是容器的盒子**（上一帧的）：两档横向错开的距离就是它。
	h.pagerBox = box.LayoutBounds()

	// ⚠️ **记下这一帧的几何**（给测试看）：画面上量不到「两档各在哪儿」——
	// `tt.Find` 报的是**裁切之后**的盒子（框架把标签的盒子和祖先的裁切区求
	// 交，见 `mygo/ui/runtime.go`），所以滑出去的那一档报出来是**被裁过的**。
	homePagerView = g
}

// viewPagerPage 画内容块里的**一档**。
//
// ⚠️ **它在带子里的位置是**流内位置**：左段那一档是第一段（x=0），右段那一档
// 是第二段（x=width）——**流**摆出来的，不是 `Left()` 摆的。
//
// ⚠️ **别再给它加 `Left(width)`**（票 #706 的 P0 修复）：框架的 `Left(x)` 是
// **相对布局位置的偏移**（`mygo/ui/layout.go` 的 `flexLayout`：
// `dx, dy := c.relative(cw, ch)`——它把 `dx` **加**在流内摆好的位置上），不是
// 绝对坐标。而这一档的流内位置**已经是 `width`**——再叠一个 `Left(width)`，它
// 的最终 x 就是 2 × width ⇒ **整个在容器外面** ⇒ 可见宽度恒为 0 ⇒ 横向滑动
// 根本没发生。
//
// width 是**一档的宽**（= 容器宽）：每一档都占这么宽，所以两档紧挨着的时候
// 它们的边界正好在带子的 width 处。
//
// ⚠️ **位置是布局位置**（不是 transform）——DESIGN.md §五《不做》那条。
func (h *homePage) viewPagerPage(c *ui.Context, s *Shell, tab int, width float32) {
	// ⚠️ **「流行」那一档的状态要在画之前递过去**：它那三态是它**画的时候**
	// 读的，而它在布局那一遍才画（列表的行是布局时建的）——递晚了那一帧画出
	// 来的是旧状态。
	if tab == homePopTab {
		h.newGrid.SetStatus(h.feed.newTab.loading, h.feed.newTab.err)
	}

	// ⚠️ **那一列要占满带子的高**（`FillHeight`）：一行里的子元素**默认是居
	// 中**的（框架的 `Row()` 把 `align` 设成 Center），而**按内容高**的话它
	// 就没有**视口**——里面那一档的列表滚不动。
	//
	// ⚠️ **宽度是**一档的宽**（`Width(width)`）**：两档紧挨着的时候它们的边界
	// 正好在带子的 width 处。
	// ⚠️ **那一列不许缩**（`Shrink(0)`）：框架的行**默认会缩**（`shrink = 1`，
	// 见 `mygo/ui/context.go`），而两档并排在一条两档宽的带子里——**缩了它们
	// 就不是一档那么宽了**，于是每一帧都要按新宽度重排一遍旧列表的文字。
	//
	// ⚠️ **`MinWidth(0)` 是省那一大笔的关键**：框架算一行里每个子元素的**最小
	// 主轴尺寸**时，默认要**量它的最小内容宽**（`mygo/ui/layout.go` 的
	// `flexLayout`：`it.minMain = min(intrinsic(c, false), it.maxMain)`）——而
	// 那一档里面是一个**列表**（`ui.List`），量它的最小内容宽就要**把行建一遍**
	// （列表的行是量的时候建的）。而带子的 `Left()` **每一帧都变**，所以带子每
	// 一帧都重排一次 ⇒ 那个测量每一帧都做一次 ⇒ 实测切 Tab 那一段每帧
	// **114 KB**（稳态是 4.7 KB，**差二十四倍**）。
	//
	// ⚠️ **写死 `MinWidth(0)` 就绕开了那个测量**：框架知道「它最窄也是 0」，不
	// 用去量。
	col := ui.Column(c).Key(homeBodyKeys[tab]).Width(width).MinWidth(0).FillHeight().
		Shrink(0).Gap(space.Px12)
	col.Children(func() {
		h.viewTabBody(c, s, tab)
	})
}

// tabPending 是「这一档的内容还在路上」。
//
// ⚠️ **「还在拉」和「拉到了但是空的」是两件事**（见 `viewTabBody` 那四种状态）：
// 只有「还在拉」才留得住旧内容；「拉到了但是空的」要立刻把空状态画出来。
//
// ⚠️ **状态是翻页列表自己说的**（`list.PagedList.Paged()`）：三档的内容在
// 各自的翻页列表里（票 #370），而「还在拉」和「拉到了但是空的」在那一层
// 里分得最清。
func (h *homePage) tabPending(tab int) bool {
	pg := h.lists[tab].Paged()
	return pg.Loading && pg.Loaded == 0
}
