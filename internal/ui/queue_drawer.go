// 播放列表抽屉：Biu 的 `components/music-playlist-drawer`。行高 64
// （`index.tsx:12` 的 `RowHeight`，也是 `scrollToPlayItem` 那个
// `targetIndex * 64` 的出处）、按判重标识去重（同一处的 `uniqBy`，键在
// `app.Item.Identity` 上）、行菜单五项（`menu.tsx` 的 `getMenus`，理由见
// `queueMenuItems`）。
//
// 它浮在主布局**外面**：`ui.Overlay` + `Absolute`（MyGo 文档
// `docs/ui/overlays.md` 里有一个抽屉的例子），不在 `viewMain` 那个 `Row` 里
// 留一列——留在那儿抽屉一开窗口就窄一块，页面被挤着走，那不是抽屉。
package ui

import (
	"strconv"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/rowkey"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/motion"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scrim"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/shadow"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	tclean "github.com/ShellMonster/Biu_Go/internal/ui/widgets/title"
	"github.com/egoist/mygo/ui"
)

// 尺寸：行高 64（`RowHeight`）、宽 320（设计规范 v0.1 §4 的右侧 Inspector；上游 Biu 是 384）、封面
// 48（`h-12 w-12`）、行内边距 8（`p-2`，48+8+8 正好 64）、顶上那条 52。
const (
	queueRowHeight    = 64
	queueDrawerWidth  = 320
	queueCoverSize    = 48
	queuePadY         = space.Px8
	queuePadX         = space.Px8
	queueHeaderHeight = 52
)

// ---- 进出场（票 #298） ----
//
// # 为什么大面板加、小浮层不加
//
// **大面板（队列抽屉）加进出场**：从右侧滑入，200ms + CubicBezier(0.16, 0.9,
// 0.21, 0.98)，**不做回弹**。理由见 DESIGN.md §九（2026-10-24 拍板）。
//
// **菜单 / 小浮层 / 确认框 / Toast 不加**：那些是高频操作，一秒好几次，
// 每一处都滑一遍就成噪音了。
//
// ⚠️ **上游不是「全站都关了动画」**：上游那 43 处 `disableAnimation` 是**一件一件
// 挂在具体组件上的**（Modal 10 处、Button 10 处、Popover 6 处、Switch 4 处、
// Tooltip 4 处、Checkbox 3 处、Dropdown 3 处、Select 1 处、Slider 1 处、
// Drawer 1 处），而**上游自己在满屏播放器里做了一个从右边滑进去的抽屉**
// （`src/components/full-screen-player/index.tsx:421-422`：`transition-all
// duration-300 ease-out` + `translate-x-full → translate-x-0`）。
// 那一段的出处与逐处清单见 DESIGN.md §五「上游那 43 处 `disableAnimation` 的
// 范围」；§九那一段记的是「小浮层不加、大面板加」这个拍板。

// queueSlideDuration 是抽屉滑入滑出要走多久：200ms。
//
// 取 200ms 是因为它是**大面板**：面板一开就占住半屏，滑得慢会显得「来得很重」；
// 而它又是高频开关（播放条上那枚），滑得慢会让人觉得「按了没反应」。
const queueSlideDuration = 200 * time.Millisecond

// queueSlideAnim 是抽屉进出场那一段动画。
//
// ⚠️ **曲线走全应用那一条统一曲线**（`motion.Unified`，值在
// `internal/ui/widgets/motion` 里）：`CubicBezier(0.16, 0.9, 0.21, 0.98)`——
// 全应用只写这一处，抽屉跟别处走的是同一条曲线。
//
// ⚠️ **它是包级变量，不能每帧现造**：CubicBezier 返回的是一个闭包，
// 每帧调一次就是**每帧一个分配**——而每帧分配是我们唯一的性能判据
// （`internal/ui/framebudget_test.go`）。
//
// ⚠️ **走曲线那一支，不做回弹**：回弹只留给「奖励动量」（甩动落位那种
// 「给了一下奖励」的动作），**点击触发的过渡不弹**（报告 164 §3.0 引的
// 《Web 流畅界面建议》：「小心地尝试带回弹效果的动画…Start simple, not springy」）。
//
// ⚠️ **Duration > 0 才是曲线那一支**：Duration == 0 会落进弹簧那一支，
// 而弹簧那一支的兜底值是 500ms（`mygo/ui/motion.go` 的 defaultSpringDuration）。
// 「零值 = 500ms 弹簧」不是「没动画」。
var queueSlideAnim = ui.Animation{Duration: queueSlideDuration, Ease: motion.Unified}

// queueSlideKey 是这一段动画在面板那个元素上的键。
//
// ⚠️ **键必须是常量字符串**：框架把动画的值记在元素的 state 里，键是 `any`，
// 而非常量字符串每帧装箱一次就是每帧一个分配（同上）。
const queueSlideKey = "queue-drawer:slide"

// drawerPhase 是播放列表抽屉现在处在进场的哪一段。
//
// # 为什么不是一个 bool
//
// 「开没开」和「在画面上的哪一段」是**两件事**：退场的那 200ms 里抽屉**还在树上**
// （它滑到窗口外面去了，但还没拆），所以「关掉」不是「不建」，是「建着，滑出去，
// 滑完了才拆」。这就是「开合的状态要和动画对齐」那一句。
//
// # 推它的是两样东西，只有两样
//
//	用户的意图（`drawerOpen`）	点那枚键、按 Esc、点外面
//	动画自己走没走完	面板到位了 / 滑到头了
//
// **动画不会反过来推它**——那是「重入」，而重入是这类状态机最容易出的 bug。
type drawerPhase uint8

const (
	// drawerGone 是「抽屉不在树上」：没开，也没在退场。
	drawerGone drawerPhase = iota
	// drawerIn 是「正在滑进来」：面板已经在树上，正从窗口外面往右沿走。
	drawerIn
	// drawerShown 是「开着，而且已经到位」。
	drawerShown
	// drawerOut 是「正在滑出去」：面板还在树上，正往窗口外面走。
	drawerOut
)

// queueRowState 是抽屉里那一行**处在哪一档**（票 #319，票 #352 补上一档）。
//
// 四档，因为「当前这一首」有三种状态，而用户要分得开：
//
//	正在放 / 暂停着 —— 高亮成「正在播放」（上游那一档）
//	用户按了停止 —— 说的是「已停止」，**不高亮**（见 `viewQueueList`）
//	队列自己走完了 —— 说的是「这一串放完了」，**不高亮**（同上）
type queueRowState int

const (
	// queueRowPlain 是普通的行：不是当前这一首。
	queueRowPlain queueRowState = iota
	// queueRowCurrent 是「当前这一首」：在放，或者暂停着。
	queueRowCurrent
	// queueRowStopped 是「当前这一首，**用户按了停止**」：这一首到此为止，
	// 而**队列里还有歌**。
	queueRowStopped
	// queueRowDrained 是「当前这一首，**队列自己走完了**」：顺序播放走到队尾，
	// **没有下一首了**（票 #352）。
	//
	// ⚠️ 它和 `queueRowStopped` **画面上一模一样**（都不高亮、字都取次要色），
	// 差的是**副题那一句**：前者说「已停止」，这一档说「这一串放完了」——
	// 同一屏上对同一件事不许说两句不同的话（播放条上那一句就是这么分的，
	// 判据是同一个 `app.State.QueueFinished`）。
	queueRowDrained
)

// queueRow 是抽屉里画出来的一行。
//
// `at` 是它在**队列**里的下标：去重之后行号和下标不再是一回事（同一个键的第一条
// 留在队列第 3 位、在抽屉里是第 1 行），点一行 / 放一首 / 删一行用的都是 `at`。
type queueRow struct {
	item app.Item
	at   int
	// key 是这一行的身份（铁律四）。见 queueRowKey。
	key string
	// id 是这一首的**判重标识**（Item.Identity）。
	//
	// ⚠️ **它和 key 不是一回事**：key 是「这一行」（铁律四），id 是「这一首」
	// （队列里可能有同一首的多份，抽屉只画一行）。
	//
	// 它在 queueRowsFor 里就**算好了**（那一处本来就要算，见去重那段）——存下来
	// 是为了别处不用再算一遍（现算一遍是要分配的：Identity 拼的是一个新字符串）。
	//
	// ⚠️ **而且它是记好的那一份**（票 #339）：`queueRowsFor` 拿
	// `s.queueRowIDs` 那个 memo 换来的，所以每帧每行不再现拼一次。
	id string
	// state 是这一行处在哪一档（见 queueRowState）。**每帧重算**：这些行是
	// `viewQueueDrawer` 每帧现造的（`queueRowsFor`），所以写在上面的状态不会
	// 留到下一帧。
	//
	// ⚠️ **状态写在行自己身上，不另开一个变量**：画这一列的那个闭包每帧建一次，
	// 而它捕获的每一个变量都占闭包结构体的一个位置——多一个变量就是多一个
	// 指针，尺寸类跳一级，**每帧多 16 个字节**（票 #319 的验收：每帧分配不许
	// 涨）。状态跟着行走，这一条就不用多捕获任何东西。
	state queueRowState
}

// queueRowKey 是这一行的 key。
//
// 🔴 铁律四：**行的身份是这一首歌的判重标识，不是行号**。默认的「父 ID +
// 兄弟序号」在前面插一行 / 删掉一行之后会让后面所有行的状态整体错配——
// 抽屉上最直接的表现是「正在播的那首」高亮到了旁边那一行。
//
// 认不出是哪一首的项（Identity 返回空串，见它的注释）退回用队列下标当身份：
// **不能给它们同一个空串**，那会让 MyGo 在 Tester 里当场 panic（重复的 key），
// 真窗口里则是两行串在一处、删一行连着删掉两首。
//
// ⚠️ 退回的那一支前缀用 `!` 而不是别的：`Identity` 造出来的键一律是
// `<local|mv|audio>:<值>`，冒号是它的一部分。所以「`queue-row:` + 标识」和
// 「`queue-row!` + 下标」这两串**永远不会相等**——写成 `queue-row:#3` 的话，
// 一首标识正好是 `#3` 的歌就会和队里第三行那个认不出的项撞上（虽然今天造不出
// 那种标识，但这里是唯一一处拿两样用户数据拼一个键的地方，别留这个口子）。
func queueRowKey(id string, at int) string {
	if id != "" {
		return "queue-row:" + id
	}
	return "queue-row!" + strconv.Itoa(at)
}

// queueRowIdentity 是抽屉里那一行的**两串身份**：判重标识和行身份。
//
// ⚠️ **两串都是每帧每行都要的**，而现拼一次就是一个新字符串（`Item.Identity()`
// 拼的是「local:」+ 路径，`queueRowKey` 拼的是「queue-row:」+ 标识）——
// 认得出身份的那些行每帧每行两次，二十首的队列就是四十来次（认不出的那一支
// 只有行身份那一次：判重标识是空串，不拼）。
type queueRowIdentity struct {
	// id 是这一首的判重标识（`Item.Identity()`）。
	id string
	// key 是这一行的身份（铁律四）。见 queueRowKey。
	key string
}

// queueRowRaw 是「那两串身份」的**原料**。
//
// ⚠️ **取得它不能分配**：所以它记的是 Item 本身（现成的字段）加上这一项在队列里
// 的位置，而**不是**那两串身份——那两串恰恰是要省掉的那一步（`rowkey.Memo` 的
// 文件头：「现拼的那一步本身就要分配，而它恰恰是要省掉的那一步」）。
//
// ⚠️ **Item 整个当原料**：它认得出身份靠的就是那几个字段（`Source` / `Type` /
// `Path` / `Bvid` / `Sid`），而比较一个 Item 不分配（字段全是现成的字符串，
// 同一个字符串的指针一样，比较是 O(1)）。
type queueRowRaw struct {
	item app.Item
	// at 是这一项在队列里的位置。认不出是哪一首时行身份退回按下标（见
	// queueRowKey），所以位置也是原料的一部分。
	at int
}

// packQueueRowIdentity 是「原料 → 那两串身份」的那一步。
//
// ⚠️ **纯函数**：同样的原料必须给出同样的结果（`rowkey.Memo` 的契约——它拿
// 原料当身份，身份变了才重算）。
//
// ⚠️ **它只在原料变了的时候才走**（见 `rowkey.Memo`）：那一档每帧每行问一次，
// 而快的那条路上零分配。
func packQueueRowIdentity(r queueRowRaw) queueRowIdentity {
	id := r.item.Identity()
	return queueRowIdentity{id: id, key: queueRowKey(id, r.at)}
}

// queueRowsFor 是抽屉这一帧要画的那几行：队列按判重标识去重之后的样子。
//
// **保留第一次出现的**（`uniqBy` 的语义）：那一行在队列里的位置（`at`）也还是
// 第一次入队那个，播放条和「下一首」走的是队列，抽屉跟着走同一个才不至于各说各话。
//
// ⚠️ **那两串身份是记好的那一份**（`ids`）：现拼的话每帧每行一到两次分配（票 #339）。
func queueRowsFor(items []app.Item, ids *rowkey.Memo[queueRowRaw, queueRowIdentity]) []queueRow {
	rows := make([]queueRow, 0, len(items))
	seen := make(map[string]bool, len(items))
	for i, it := range items {
		// ⚠️ **按「第几项」记**：这一格的原料是这一项自己（Item + 队列里的位置），
		// 所以队列一改（加歌、删歌、重排）它自己就看得出「数据换了」——调用方
		// 一处都不用记得重建（理由见 `rowkey.Memo` 的文件头）。
		ident := ids.At(i, queueRowRaw{item: it, at: i}, packQueueRowIdentity)
		if ident.id != "" {
			if seen[ident.id] {
				continue
			}
			seen[ident.id] = true
		}
		rows = append(rows, queueRow{item: it, at: i, key: ident.key, id: ident.id})
	}
	return rows
}

// viewQueueDrawer 是播放列表抽屉。没开就一个字都不建。
//
// 🔴 铁律二：**这一层不调 SetTheme**——它在 `viewMain` 的同一个 build pass 里建
// （`viewMain` 第一行就 `applyFrameTheme` 过），读到的 `c.Theme()` 已经是用户选
// 的那一档。跟 `viewToasts` 一样。
//
// ⚠️ 外面那层 `Guard` 是 `viewMain` 加的（铁律三）：抽屉一崩，崩掉的是**整个窗口**。
func (s *Shell) viewQueueDrawer(c *ui.Context) {
	// 阶段先推，**建不建后说**：退场的那一段面板还在树上，所以「关掉」不是「不建」。
	justIn := s.syncDrawerPhase()
	if s.drawerPhase == drawerGone {
		return
	}
	// 记一笔「抽屉建了一遍」——给「一帧建了几遍」那条测试数（见 builds.go）。
	//
	// ⚠️ 这一句数的是**两个调用方**：满屏播放器开着的时候抽屉由满屏那一支
	// 建（`viewFullPlayer` 末尾那一句），而 `viewMain` 必须让开。两处都建
	// 的话这里会是 2——而画面上看着没差。
	s.builds.drawer++
	t := c.Theme()
	s.queueRows = queueRowsFor(s.app.Queue().Items(), &s.queueRowIDs)

	ui.Overlay(c, func() {
		// ⚠️ 底边留出底下那一条的高度（**上游** Biu 的
		// `data-[placement=right]:mb-22` 是 88，**我们**的是 `playbarHeight`
		// = 76）。这一层是吃点击的（见下面 back.Clicked 那段），不留出这一条
		// 播放条就**整个被它盖住**：抽屉开着时按播放、上一首、下一首都按不动。
		//
		// 满屏开着的时候底下那一条是满屏的底栏（进度 + 控制键，108 高，见
		// `fullControlsHeight`），不是播放条——让出的高度要跟着它，否则进度条
		// 上面那十几像素会被蒙版盖住、点一下就变成「关抽屉」。
		bottomInset := float32(playbarHeight)
		if s.full.open {
			bottomInset = fullControlsHeight
		}
		back := ui.Row(c).Key("queue-backdrop").
			Absolute().Left(0).Top(0).Right(0).Bottom(bottomInset)
		if s.drawerPhase == drawerOut {
			// ⚠️ **退场那 200ms 里，这片蒙版不吃点击**。
			//
			// 改动之前关掉抽屉是**整个浮层都不建**（`viewQueueDrawer` 第一句
			// `if !s.drawerOpen { return }`），蒙版立刻消失，下一次点击直接落在
			// 页面上。现在面板还要滑 200ms 才拆，而面板在 ~100ms 时就已经 95%
			// 滑出屏幕了——「看上去已经关好」的那段时间，底下那一页（侧栏、页面）
			// 却点不动：那一下点击被这片**看不见的**蒙版吃掉，只会把 drawerOpen
			// 又设成 false。
			//
			// 所以退场一开始，蒙版就**让开指针**（`PassThrough`）：它是透明的，
			// 用户看不见它，而它盖住整页——看不见的东西吃点击，就是「点不动」。
			back = back.PassThrough()
		} else {
			// 没有底色：Biu 的 `backdrop="transparent"`，底下那一层照旧看得见。
			//
			// ⚠️ **开着的时候它是模态**：Modal 把焦点关进抽屉、外面那些快捷键
			// 让路，而底下那一页在它开着的时候点不动（Biu 的透明蒙版也是这样，
			// 它只是看不见，不是空的）。
			back = back.Modal()
		}
		back.Children(func() {
			// ⚠️ **那一圈影是上游给的**：Biu 那一枚是 HeroUI 的
			// `<Drawer radius="md" shadow="md">`（`components/music-playlist-drawer/
			// index.tsx:116`），而 HeroUI 的 Drawer 是拿 Modal 画的，那一档影落在
			// **面板本身**上。抽屉是浮在页面右边的一整条，没有影的话它和底下那一页
			// 是「同一块底上的两块颜色」——深色下尤其如此（上游的卡片底和背景几乎
			// 同色，**靠阴影分层**，见 DESIGN.md §4）。
			//
			// ⚠️ **面板底色取 `t.Background`（= 上游的 `bg-background`）**：上游
			// 那一枚 Drawer 没有自己的底色类，走的是 HeroUI Drawer 的默认
			// `bg-background`，而上游主题把 `background` 这个色定义成了**页面底色**
			// （`src/common/constants/theme.ts`：深色 #17181a、浅色 #ffffff）。所以
			// 那一块和底下那一页是**同一个颜色**，分开的正是上面那圈影。
			//
			// 之前这里取 `t.Surface`（深色下 #27272a）——那正是 DESIGN.md §四说的
			// 「比背景亮一级的板」。
			//
			// ⚠️ **面板是绝对定位的**（原来它是在这一行 flex 里靠 `Justify(End)`
			// 贴右沿的）：进出场要动的是**布局位置**，而 flex 那一套摆不出
			// 「从右边滑进来」——`justify-content: flex-end` 溢出时是往**左**溢出
			// （`mygo/ui/layout.go` 的 flexLayout：`free := max(contentMain-ln.main, 0)`，
			// 剩下的地方被夹成 0，条目落在起点）。绝对定位按 **inset** 摆，
			// 那才是「从窗口右边挪进来」能用的那一套。
			panel := shadow.HeroMedium(ui.Column(c).Key("queue-drawer").
				Absolute().Top(0).Bottom(0).Width(queueDrawerWidth).
				Background(t.Background).BorderStartWidth(1).BorderColor(t.Border), t)
			// ⚠️ **面板在退场时照旧接点击**：它还在屏幕上（滑出去那一段看得见），
			// 而「看不见的东西吃点击」才是坏的（那是底下那片蒙版，见上面）。
			// 面板看得见，所以点它就是点它。
			//
			// ⚠️ **别用 `Disabled` 关它**：框架把禁用画成**半透明**
			// （`mygo/ui/paint.go`：`if e.flags&flagDisabled != 0 { p.opacity *= 0.5 }`），
			// 那会让面板一边滑一边淡——变成「淡出」而不是「滑出」了。
			panel.Role(ui.RoleDialog).Label("播放列表").
				Children(func() {
					s.viewQueueHeader(c, t)
					s.viewQueueBody(c, t)
				})

			// ---- 进出场 ----
			//
			// 动的是面板的**右边距**：0 = 贴住窗口右沿（到位了），
			// -queueDrawerWidth = 整个面板在窗口外面（还没进来）。
			//
			// ⚠️ **动的是布局位置，不是 transform**（DESIGN.md §五：「动画不要用
			// transform 动大子树……要动就动**布局位置**」）。软件渲染回退时
			// transform 会让整棵子树逐像素重绘。
			//
			// ⚠️ **第一次建要 Jump 一下**：框架的规矩是「一个值从它的第一个目标开始」
			// （`mygo/ui/motion.go` 的 channel：第一次 `AnimateTo` 直接把值给成
			// 目标），所以不先 Jump 的话面板会**直接出现在**右沿上，没有滑入。
			if justIn {
				panel.Jump(queueSlideKey, -queueDrawerWidth)
			}
			// 目标：开着就贴住右沿（0），关着就整个在窗口外面。
			target := float32(0)
			if s.drawerPhase == drawerOut {
				target = -queueDrawerWidth
			}
			slide := panel.AnimateTo(queueSlideKey, target, queueSlideAnim)
			panel = panel.Right(slide)

			// 到位了 / 滑到头了：换一段。
			//
			// ⚠️ **用 Settled 不用 Arrived**：Settled 是「停在那儿了」，退场这一段
			// 要的是「滑到头了就把面板拆掉」——拆之前那一帧面板还在树上
			//（它在窗口外面，看不见），下一帧才真的没了。
			//
			// ⚠️ **这一句不会重入**：它只在阶段是 in/out 时才改阶段，而阶段只被
			// 意图和动画推着走（见 drawerPhase 那一段）。
			if panel.Settled(queueSlideKey) {
				switch s.drawerPhase {
				case drawerIn:
					s.drawerPhase = drawerShown
				case drawerOut:
					s.drawerPhase = drawerGone
				}
			}
		})

		// 关抽屉的两条路：点底下那一层、按 Esc。少一条的表现是「我明明点了外面，
		// 它还在」，另一条少的是「按了 Esc 什么都没发生」。
		//
		// ⚠️ 用 `back.Clicked()`，**不要用 `panel.PressedOutside()`**：后者问的是
		// 「指针按在了不是这个元素的地方」，问的**不是这一层的框**——播放条上按
		// 一下也算「按在外面」，于是按播放会顺手把抽屉关了。而播放条是这一层
		// 特意让开的（Biu 的 `mb-22`）：抽屉开着时它照常用，关抽屉是另一件事。
		//
		// 副作用是底下那一层（侧栏、页面）在抽屉开着的时候点不动——Biu 的透明
		// 蒙版也是这样，它只是看不见，不是空的。这和上面那个 Modal 是同一件事。
		//
		// ⚠️ 按下 Esc 时焦点得在抽屉里才轮得到它（Modal 把焦点关进来了）。
		//
		// ⚠️ **这里是「=false」不是「= !drawerOpen」**：关的动作是幂等的——
		// 退场那 200ms 里用户再点一次外面，不该把抽屉又打开。
		if back.Clicked() || back.OverlayShortcut(0, ui.KeyEscape) {
			s.drawerOpen = false
		}
	})
}

// syncDrawerPhase 推一遍抽屉的阶段机，返回这一帧是不是「刚打开」。
//
// ⚠️ **它只被两样东西推着走**：用户的意图（`drawerOpen`）和动画走没走完
// （`viewQueueDrawer` 里那一句 Settled）。**动画不会反过来推它**。
//
// ⚠️ **「关」是幂等的**：`drawerOpen = false` 连按几次都是「关」，
// 而 `drawerOpen = !drawerOpen` 那样写在关的动作上就是「开」——
// 退场那 200ms 里用户再点一次外面，抽屉会弹回来。
//
// ⚠️ **退场途中又打开**：阶段退回 `drawerIn`，而面板**从当前位置接着滑回去**
// （框架的曲线分支被打断时是从当前值接着走的，`mygo/ui/motion.go` 的 retarget）。
func (s *Shell) syncDrawerPhase() bool {
	switch s.drawerPhase {
	case drawerGone:
		if !s.drawerOpen {
			return false
		}
		// 刚开：面板从窗口外面滑进来。
		s.drawerPhase = drawerIn
		return true
	case drawerIn, drawerShown:
		if !s.drawerOpen {
			s.drawerPhase = drawerOut
		}
	case drawerOut:
		if s.drawerOpen {
			s.drawerPhase = drawerIn
		}
	}
	return false
}

// viewQueueHeader 是顶上那一条：标题 + 条数 + 定位 + 清空。两个键只在队列非空时才摆
// （Biu 的 `Boolean(pureList?.length) && …`）。
//
// 字号照上游（`music-playlist-drawer/index.tsx:131-133`）：标题是 `h3`，也就是
// `text-lg` = 18 / 400（Preflight 把标题字重重置成 inherit，不是加粗）；条数是
// 同一处的 `text-sm` = 14。原来这里是 15 / 700 和 12——标题小一号还重两档，读起来
// 像一句标语。
//
// ⚠️ **条数要紧跟标题**（上游那是一个 `h3` 里的 `span`，中间没有空隙）：原来
// 它是标题旁边一个独立的 Text，而标题那一格 `.Grow(1)`——于是它被推到面板最右边，
// 离标题 192 px，读起来像「播放列表」和另一个控件各说各的。
func (s *Shell) viewQueueHeader(c *ui.Context, t *ui.Theme) {
	rows := s.queueRows

	ui.Row(c).Key("queue-header").Height(queueHeaderHeight).FillWidth().
		PaddingY(space.Px10).PaddingX(space.Px12).Gap(space.Px8).AlignItems(ui.Center).Children(func() {
		// 标题和条数是**一个**东西：上游是 `<h3>播放列表<span …>({n})</span></h3>`
		// （`music-playlist-drawer/index.tsx:131-133`），条数紧跟在标题后面、
		// 中间没有空隙，读起来是「播放列表(3)」。
		//
		// ⚠️ **原来条数是标题旁边一个独立的 Text，而标题那一格 `.Grow(1)`**——
		// 标题把剩下的地方吃掉，条数于是被推到面板最右边（实测离标题 192 px，
		// 紧挨着「定位 / 清空」两枚图标）。这里把两样摆进**同一格**：基线对齐
		// （上游那是同一行文字里的一个 span，基线本来就齐），字号还是 18 / 14。
		ui.Row(c).AlignItems(ui.Baseline).Gap(space.None).Children(func() {
			ui.Text(c, "播放列表").FontSize(scale.Font(c, scale.Size18)).TextColor(t.Text)
			if len(rows) == 0 {
				return
			}
			ui.Text(c, "("+strconv.Itoa(len(rows))+")").FontSize(scale.Font(c, scale.Size14)).TextColor(t.TextMuted)
		})
		if len(rows) == 0 {
			return
		}
		// 剩下的地方让给两枚图标——上游是 `justify-between` 的右半边。
		ui.Spacer(c)

		// 逐字照 Biu 那一段（`components/music-playlist-drawer/index.tsx:137-141`）：
		// 两个 16px 的 `IconButton`（`RiFocus3Line` / `RiDeleteBinLine`）各带
		// tooltip。**无障碍名字一个字没少**——原来那两个文字键念出来是「定位当前
		// 播放」「清空」，换成图标之后靠 `Label` 念的还是这两句，而它们同时也是
		// 无头 Tester 认这两个按钮的唯一凭据（`queue_drawer_test.go` 靠它点）。
		//
		// ⚠️ **走 `iconKey`（悬停时图形变主色）**，不是 `iconButton`（票 #249）：
		// 打开这个抽屉的那一枚（播放条上的「播放列表」）走的是 `iconKey`——同一个
		// 抽屉里，头上的键和开抽屉的键反馈档位不一样，用户会觉得「这一枚不亮」。
		if s.withTip(c, iconKey(c, iconLocate, "定位当前播放", iconSm, false).
			Size(iconHit, iconHit).Padding(space.None, space.Px9),
			"把队列滚到正在放的那一首").Clicked() {
			s.locateCurrent()
		}
		if s.withTip(c, iconKey(c, iconDelete, "清空", iconSm, false).
			Size(iconHit, iconHit).Padding(space.None, space.Px9), "清空播放列表").Clicked() {
			s.app.ClearQueue()
		}
	})
	// 头下面那条线用框架的 `ui.Divider`：1 DIP、无圆角、无内边距，取的就是
	// `t.Border`（`mygo/ui/widgets.go` 的 `coreDivider`）——和原来手搓的这一行
	// 逐字一样，只是换了个出处。它看自己在 `Row` 还是 `Column` 里决定给宽度还是
	// 高度，这里外面是 `Column`，所以画出来还是横着那一条。
	ui.Divider(c).Key("queue-header-rule")
}

// viewQueueBody 是抽屉下面那一块：有歌就是那一列，没有歌是一句空状态（分岔照
// Biu 的 `{list.length ? <VirtualList/> : <Empty/>}`）。
func (s *Shell) viewQueueBody(c *ui.Context, t *ui.Theme) {
	if len(s.queueRows) == 0 {
		// ⚠️ 那两行走 `emptyCopy`（empty.go），和七个页面的空状态同一份字号、
		// 同一份主次色。原先这里自己抄了一份 15 / 12——而抄一份就会漂开。
		ui.Column(c).Key("queue-empty").Grow(1).Center().Padding(space.Px20, space.Px16).Children(func() {
			emptyCopy(c, queueEmpty)
		})
		return
	}
	s.viewQueueList(c, t)
}

// viewQueueList 是抽屉里那一列队列。行身份是判重标识（铁律四），行高定死 64——
// 「定位当前播放」滚到的 `targetIndex * 64` 靠的就是这个定值。
func (s *Shell) viewQueueList(c *ui.Context, t *ui.Theme) {
	rows := s.queueRows
	// ⚠️ **这一帧只取一次快照**（`viewMain` 那条规矩：两次取的话两者之间状态
	// 可能变了，而画出来的是两份不同的状态）。「当前这一首」和「停住了」都从
	// 这一份里读。
	st := s.app.Snapshot()
	playing := s.playingIdentity(st)
	// 「正在放的那一行」在**建列表之前**找出来，不要在每一行的回调里找：那是一帧
	// 里 len(rows) 次全表扫，队列长一点就是几万次判重标识的比较，而每帧都来一遍。
	cur := currentRow(rows, playing)
	// ⚠️ **状态写在那一行自己身上**（`queueRow.state`），不另开一个变量：这个
	// 闭包每帧建一次，而它捕获的每一个变量都占闭包结构体的一个位置——多一个
	// 变量就是多一个指针，尺寸类跳一级，**每帧多 16 个字节**（票 #319 的验收：
	// 每帧分配不许涨）。
	//
	// ⚠️ **「停住了」压过「当前」**：当前那一行停住之后，它要说的话是「停住了」，
	// 不是「正在放」——那正是这一票要治的那一处（停住之后那一行仍高亮成
	// 「正在播放」，和暂停**一模一样**，用户在界面上分不出这两件事）。
	//
	// ⚠️ **别的行谈不上停住**：它们压根没在放，所以只有当前那一行会被改。
	if cur >= 0 {
		rows[cur].state = queueRowCurrent
		if st.Stopped {
			rows[cur].state = queueRowStopped
			// ⚠️ **两种停住要分得开**（票 #352）：「用户按了停止」和「队列自己
			// 走完了」在播放器那一边落在**同一档**上，而用户要分的是两件事——
			// 队列里还有歌，还是没有下一首了。
			//
			// 判据和播放条上那一句是**同一个**（`app.State.QueueFinished`），
			// 所以两处不会对同一件事说两句不同的话。
			if st.QueueFinished {
				rows[cur].state = queueRowDrained
			}
		}
	}

	// 🔴 铁律四：Key 每一帧都重装，装的是这一首自己的判重标识。
	// ⚠️ 别改成行号：那正是实测踩过的坑（前面删掉一行，后面所有行的状态整体
	// 错配，抽屉上表现为「正在播的那首」高亮到了旁边那一行）。
	// ⚠️ **身份是预先装箱好的那一份**（`s.queueRowKeys`）：理由见
	// `shell.go` 里那个字段的注释（票 #313）。
	s.queueList.Key = func(i int) any {
		return s.queueRowKeys.At(i, rows[i].key, rowkey.Identity)
	}
	// Label 是这一行给无障碍和「打字选中」用的名字。
	s.queueList.Label = func(i int) string { return rows[i].item.Title }

	// 落点先清掉：**每一行自己记**（见下面 viewQueueRow 里那几句
	// PointerPosition），指针不在任何一行上时它就该是「没有落点」。
	s.queueDropAt = -1

	e := ui.List(c, &s.queueList, len(rows), func(i int) {
		s.viewQueueRow(c, rows[i], i, t)
	}).
		// 滚动位置写进自己的句柄，「定位当前播放」改的就是它（ScrollTo 带动画）。
		TrackScroll(&s.queueScroll).
		Grow(1).FillWidth()
	s.trackScrollbar(e)

	// 这一列是**拖动的落点**（框架把落点交给指针底下最里层那个肯收拖动值的
	// 元素——行里的封面、字、⋯ 都不收，所以收到的是这一列）。
	//
	// ⚠️ **问一次，不要每行问一次**：框架那两样（Drop / DragOver）每问一次都
	// 要新建一个闭包（那个闭包要抓住类型），而队列抽屉一屏有十来行——每行每帧
	// 问一次就是每帧二十几次分配（见 framebudget_test.go，那一档的上限是 50
	// 次）。问一次是两次（这里两行），问每一行就是二十六次。
	//
	// ⚠️ **Drop 读到的是「刚刚落下」那一次**：这一句在列表建完之后才问，而框架
	// 是在建列表的过程中把落点交给这一列的——问得晚一帧没关系，落下那一次
	// 记在元素的状态里，下一帧还在。
	if v, ok := ui.Drop[*queueDragValue](e); ok {
		s.queueDrop(v)
	}
	// 有东西正被拖着经过这一列：拖着的时候要画那条插入线。
	s.queueDragging = false
	if _, ok := ui.DragOver[*queueDragValue](e); ok {
		s.queueDragging = true
	}
}

// currentRow 是「正在放的那一首」在抽屉里的行号，没有是 -1。
//
// 按**判重标识**认，不按下标认（同一首排了两遍时，正在放的可能不是抽屉留下的
// 那一条）。这也是 Biu 的 `isSame(playItem, item)`。
func currentRow(rows []queueRow, playing string) int {
	if playing == "" {
		return -1
	}
	for i, r := range rows {
		// ⚠️ **用行上记好的那一份**（`r.id`）：现算的话每帧每一行一次字符串拼接
		// （`Item.Identity()` 拼的是「local:」+ 路径），而这一份在 `queueRowsFor`
		// 里已经算好、而且是记好的（票 #339）。
		if id := r.id; id != "" && id == playing {
			return i
		}
	}
	return -1
}

// playingIdentity 是「正在放的那一首」的判重标识。没有歌在放、或者认不出是哪一首
// 时都是空串——这两个情形必须一样：认不出的项之间身份都是空串，拿它去比会让抽屉
// 一开就有一行莫名高亮。
//
// ⚠️ 判据是**队列的当前项 **加**「确实有歌在放」**（`State.HasTrack`），
// 不是光看队列的当前项。光看当前项的话：删掉正在放的那首、而下一首打不开时
// （`App.playNextAfterCurrent` 会停掉播放，但队列的当前项已经落到下一首了），抽屉
// 会把**一首放不出来、也没在放**的歌涂成正在放，播放条上却是空的——界面说的和它做
// 的不一致，「定位当前播放」还会把用户滚到那一行中间。
//
// 队列的**下标**不动是有意的：停在那儿之后按「下一首」能从它后面接着走，点那一行
// 也能重试（`App.PlayAt`）。丢的是「它在放」这件事，不是「我们走到哪儿了」。
//
// ⚠️ **它认的是「当前这一首」，不是「此刻在响」**：暂停着的那一首照样认得出
// （上游 `isSame(playItem, item)` 也是这么认的）。「停住了」是另一档，见
// `queueRowState`。
func (s *Shell) playingIdentity(st app.State) string {
	if !st.HasTrack {
		return ""
	}
	cur, ok := s.app.Queue().Current()
	if !ok {
		return ""
	}
	return cur.Identity()
}

// viewQueueRow 是抽屉里的一行：封面 + 歌名 + 来源 + 右端那个「⋯」。
//
// ⚠️ 行高定在 64（Biu 的 RowHeight），**内容按封面 48 + 上下各 8 摆进去**。
// 别在这里改内边距：改了的话行高和 `targetIndex*64` 就不一样了，「定位当前
// 播放」会停在差几像素的地方——不难看出来，但用户每次按都差一点。
func (s *Shell) viewQueueRow(c *ui.Context, row queueRow, at int, t *ui.Theme) {
	// 那一行处在哪一档（正在放 / 停住了 / 普通）——写在行自己身上，见
	// `queueRow.state`。
	state := row.state
	rowEl := ui.Column(c).Key(row.key).Grow(1).Height(queueRowHeight).
		Padding(queuePadY, queuePadX).Radius(t.Radius).AlignItems(ui.Stretch)
	// 悬停要读在 Children 之前：指针是在布局那一步才知道落在哪儿的。
	hovered := rowEl.Hovered()
	switch {
	case state == queueRowCurrent:
		// 上游那一行走的是 `variant="flat" color="primary"`
		// （`music-playlist-drawer/list-item.tsx:32`）——HeroUI 的 flat+primary
		// 展开是 `bg-primary/20 text-primary-600`，也就是**主色 20% 那一层淡底**
		// （浅色下落在面板底 #ffffff 上是 #d2f7df，上游实图那个数）。
		//
		// 那一层淡底是**状态识别**：一眼看出哪一首在放，比中性灰好认（票 #294 /
		// 报告 §1 的 S-11，2026-10-24 拍板按上游改）。DESIGN §七保护的是「**主色
		// 当字色**的对比度」，不是「主色当底色」——§七 不该被扩大解释。
		//
		// ⚠️ **不是实心主色底**：实心那一档是「被选中的选项」（`flatSolid`），
		// 票 #197 刚把列表选中行的实心主色底去掉。这一档说的是「这一首在放」。
		//
		// ⚠️ **这一格不走 `flatCurrent`**：那个是「当前在哪」（侧栏菜单当前项、
		// 左栏那三处）共用的中性灰底，改它会把侧栏一起改掉。
		//
		// 配方在 `playingrow.go`（**两个抽屉共用一份**：分集抽屉的「正在放」那一
		// 行是同一档，见 `partdrawer.go` 的 `viewPartRow`）。
		rowEl = rowEl.Background(playingRowBg(t)).TextColor(playingRowText(t))
	case hovered:
		// 悬停要有反应：这一行点得动（跳过去放它），没有反馈用户不知道它是能点的。
		rowEl = rowEl.Background(t.SurfaceHover)
	}
	// 这一行是**拖动源**：拖它就是重排队列（见 queue_drag.go）。
	//
	// ⚠️ **拖动值是指针**：框架把拖动值装进 `any`，装指针不分配。
	rowEl = rowEl.Drag(s.queueDragFor(at, row))
	// 拖着的那一行半透明——框架那一套也是这么画的（见 mygo/ui/list.go 的
	// reorder）：行还在原处，而跟着指针走的是另一份拷贝。
	if rowEl.Dragging() {
		rowEl = rowEl.Opacity(0.4)
	}
	// 指针在这一行上吗？落点就记在这儿。
	//
	// ⚠️ **用 PointerPosition 而不是框架的 DragOver**：DragOver 每问一次都要
	// 新建一个闭包，而每一行每帧都要问一次（见上面那段）。
	//
	// ⚠️ **记的是「指针在这一行的哪一半」**：上半是「挪到这一行之前」，下半是
	// 「挪到下一行之前」（最后一行就是队尾）。
	if _, py, over := rowEl.PointerPosition(); over {
		s.queueDropAt = at
		s.queueDropUpper = py < rowEl.Bounds().H/2
	}
	// 拖着的时候在落点那一行画一条插入线：挪到哪儿得看得见。
	//
	// ⚠️ **只在拖着的时候画**：那个闭包是每帧一次分配（它要抓住「上半还是
	// 下半」），而没在拖的时候它没有用。
	if s.queueDragging && s.queueDropAt == at {
		s.queueDropLine(at, rowEl, t.Accent)
	}

	rowEl.Children(func() {
		ui.Row(c).Grow(1).FillHeight().Gap(space.Px8).AlignItems(ui.Center).Children(func() {
			s.viewQueueCover(c, row.item, state == queueRowCurrent, hovered)
			// 歌名和来源两行（Biu 的 `space-y-1`，这里 Gap(2)）竖着堆，居中放在
			// 48 那张封面旁边。
			//
			// 字号照上游 `music-playlist-drawer/list-item.tsx:68-80`：歌名
			// `text-base` = 16、来源 `text-sm` = 14——也就是 DESIGN.md 里「列表」
			// 那一档（歌名 16 / UP 14）。这里原来是 15 / 12，副题比列表行的 UP 名
			// 还小一档，同一列两种刻度。
			ui.Column(c).Grow(1).Gap(space.Px2).Children(func() {
				ui.Text(c, tclean.Clean(row.item.Title).Title).FontSize(scale.Font(c, scale.Size16)).
					TextColor(playlistTextColor(t, state)).SingleLine()
				// 副题两态不一样：正在放那一行按**它自己那层淡底**重算（见
				// `playlistSubtitleColor`），别的行是次要色。
				//
				// ⚠️ **正在放那一行不跟着染主色**：主色当字是**歌名**那一档的活
				// （上游 `text-primary-600`），副题再染一遍就成了两处都在喊。
				ui.Text(c, queueRowSubtitle(row.item, state)).FontSize(scale.Font(c, scale.Size14)).
					TextColor(playlistSubtitleColor(t, state)).SingleLine()
			})
			// Biu 的那一颗「⋯」是个 DropdownTrigger。这里给它挂系统的菜单
			// （Element.Menu），点它和右键这一行是同一份菜单——`queueMenu` 只
			// 写一遍，两条路不可能点出不同的东西来。
			//
			// 扁平：调研点名的描边药丸里就有这一颗（报告 §1.1 的 F1，证据那一段
			// 点名的是 `queue_drawer.go` 里「行尾的 ⋯」）。旁边的行本身没有边框，
			// 唯独行尾飘一个描边药丸，混在一列无边框的行里更显眼。
			//
			// 图标是上游那一枚（`music-list-item/operation.tsx:22` 的
			// `RiMore2Line`，16px）：这一颗画出来就是三个点，而「⋯」那几个
			// 字符跟着系统字体走——同一个意思在两台机器上是两个不同的宽高。
			//
			// ⚠️ **配方在 `widgets/list` 里**（票 #249）：同一个「⋯」在仓库里
			// 曾经有**三个尺寸**——列表那一档是 32×32、这里这一枚是 44×28（`flat`
			// 默认内边距 6/14）。44 宽在 320 的抽屉里吃掉 14% 行宽，歌名那一列
			// 因此只有 196 而不是 208，**标题比需要早 12px 截断**。现在两处共用
			// 一份（`list.OperationAccent`），尺寸是 16 + 每边 8 = 32×32。
			//
			// ⚠️ **无障碍名带上歌名**（`list.OperationLabel`）：原来这一枚叫
			// 「这一首的操作」——一屏 N 枚全都叫这一句，读屏念出来分不出是哪一行，
			// Tester 也认不出是哪一行。现在是「「歌名」的操作」，和列表那一档
			// 一样。
			//
			// ⚠️ **悬停时图形变主色**（`OperationAccent`）：指针落在它上面时整行
			// 也亮着（`rowEl.Hovered()` 对祖先为真），两处反馈叠在一起，而原来
			// 两者都是极淡的中性色——**看不出「我现在指着的是哪一枚」**。变主色
			// 那一档是上游 `hover:text-primary`（票 #249）。
			list.OperationAccent(c, list.OperationLabel(row.item.Title),
				func(m *ui.Menu) { s.queueMenu(m, row) })
		})
	})

	// 点这一行 = 从这儿听（Biu 的 `onPress` → `playListItem`）。
	//
	// ⚠️ **点正在放的那一行不重头播**（见 app.PlayAt）：Biu 的 playListItem 开头
	// 就是一句 `if (playId === id) return`。重头播是播放条上「播放」那个键的活。
	if rowEl.Clicked() {
		if err := s.app.PlayAt(row.at); err != nil {
			s.addToast("放不了这一首："+err.Error(), notice.Danger)
		}
	}
	// 右键菜单挂在**行**上（不是外面那个 List，也不是 ⋯ 那个按钮）：MyGo 取的是
	// 最里层那个带菜单的元素（`ui/menu.go` 的 menuTarget），挂错了整列共用一份，
	// 而且 ⋯ 上按右键会开成一份一模一样的、看不出是哪一行的菜单。
	rowEl.ContextMenu(func(m *ui.Menu) { s.queueMenu(m, row) })
}

// viewQueueCover 是行里那张封面。四种状态（没有地址 / 载入中 / 就绪 / 加载失败）
// 都由 imgcache.View 画，这里不重复它的判断。悬停时上面浮一枚 20 的播放三角
// （Biu 那层蒙版 + `RiPlayFill`）；正在放的那首不浮——它已经在放了。
func (s *Shell) viewQueueCover(c *ui.Context, item app.Item, playing, hovered bool) {
	t := c.Theme()
	ui.Row(c).Width(queueCoverSize).FillHeight().Children(func() {
		imgcache.ViewTitled(c, s.images, item.Cover, item.Title, queueCoverSize, queueCoverSize)
		if playing || !hovered {
			return
		}
		// 半透明黑底 + 一个播放三角，就是 Biu 那层 `bg-[rgba(0,0,0,0.35)]`。
		//
		// 三角是**图标**不是字符（上游 `list-item.tsx:49` 的 `<RiPlayFill
		// size={20}/>`）。原来这里写的是「▶」14 号字：字形跟着系统字体走，同一个
		// 意思在两台机器上是两个不同的宽高，而且 14 号字在 48 的封面上太小。
		// 图标不走 `scale.Font`：上游的 `size={20}` 是固定像素，图标不跟系统字号
		// 缩放（见 widgets/scale 的文件头）。
		//
		// PassThrough：**不让这一层吃掉点击**。它盖在封面上面，不放行的话
		// 点它就不是「点这一行」了（那一行跳过去放歌正是要点封面附近的）。
		ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Radius(t.Radius).
			PassThrough().Center().Background(ui.RGBA(0, 0, 0, scrim.Overlay)).Children(func() {
			ui.Icon(c, icon(iconPlayFill)).FontSize(20).
				TextColor(ui.RGBA(255, 255, 255, scrim.OnScrim))
		})
	})
}

// playlistTextColor 是那一行里**歌名**的颜色。
//
// 正在放的那一行是**淡主色底 + 主色当字**（配方见 `playingrow.go`），所以歌名
// 取的是 `playingRowText` 那一档——**按那一格的底算出来的那一档**。
//
// ⚠️ **停住的那一行取次要色**（票 #319）：它已经不高亮了（不高亮才成其为
// 「停住了」），而歌名再是主色的话，那一行看上去仍像在放——那正是这一票要
// 治的那一处。
//
// ⚠️ **和列表行 / 网格卡片那一档不是同一个数**（`widgets/list` 的 `rowColors`
// 那一档是 `ink.AccentOn`：它按**页面底色**算，浅色下更浅一点）。两处都是
// 「主色当字，不是实心底」，但压深时按的底不同——这一行的底是**淡主色**，
// 比页面底色暗，所以按页面底色算出来的那个变体压在上面差一点（4.03:1）。
//
// 差这一点不会有用户看得出来，但**判据要按各自的底算**：这一行的字是**自己
// 画的**（不继承父元素），底换了字不跟着换就是「底换了、字没换」。
func playlistTextColor(t *ui.Theme, state queueRowState) ui.Color {
	switch state {
	case queueRowCurrent:
		return playingRowText(t)
	case queueRowStopped, queueRowDrained:
		return t.TextMuted
	}
	return t.Text
}

// playlistSubtitleColor 是歌名下面那一行（来源）的颜色。
//
// 不是正在放的那一行：次要色（`t.TextMuted`）。
//
// 正在放的那一行：**按它自己那层淡底重算**——淡绿底比中性灰亮，次要色原样
// 压上去只有 4.17:1（不到 WCAG 正文的 4.5:1；改之前那块中性灰底上更只有
// 3.53:1）。重算之后是 **4.55:1**（浅色 #6b6b74），深色下原样就够（4.65:1）。
//
// ⚠️ **不是主色**：主色当字是**歌名**那一档的活（上游 `text-primary-600` 只
// 管那一个），副题再染一遍就成了两处都在喊。
func playlistSubtitleColor(t *ui.Theme, state queueRowState) ui.Color {
	if state == queueRowCurrent {
		return ink.Ensure(t.TextMuted, playingRowBg(t), 4.5)
	}
	return t.TextMuted
}

// queueRowSubtitle 是歌名下面那一行（票 #319，票 #352 补上「两种停住」）。
//
// ⚠️ **停住的那一行说的是「停住了」，不是它的来源**：来源是这一首的属性，
// 而「停住了」是**此刻的状态**——而这一行此刻正被用户盯着看「为什么没声了」。
// 来源在别的行上还在，而这一刻用户要的是状态。
//
// ⚠️ **普通那一行仍然是来源**：那一行没在放，「停住了」对它没有意义。
//
// ⚠️ **两种停住说得不一样**（票 #352）：用户按了停止说「已停止」，队列自己走完
// 了说「这一串放完了」——**和播放条上那一句是同一套判据**
// （`app.State.QueueFinished`）。原来这一档一律说「已停止」，于是队列走完时
// 同一屏上两句不同的话：播放条说「这一串放完了」，抽屉里说「已停止」。
func queueRowSubtitle(item app.Item, state queueRowState) string {
	switch state {
	case queueRowStopped:
		return "已停止"
	case queueRowDrained:
		return "这一串放完了"
	}
	return itemSubtitle(item)
}

// itemSubtitle 是歌名下面那一行。Biu 摆的是 UP 主名，我们这一层还没有那个字段，
// 而拿类型去冒充它是编一句话——用户会以为那是个人名。所以这里摆的是能从这一项
// **真的读出来**的来源。
func itemSubtitle(item app.Item) string {
	switch item.Identity() {
	case "":
		// ⚠️ **不许说「还没接上」**（票 #242，UX 报告 §3-F 的 F2）：那一项确实
		// 什么身份都没有，而用户要知道的只是「它的来源读不出来」。
		return "来源未知"
	}
	switch {
	case item.Source == "local" || item.Type == "local":
		return "本地音乐"
	case item.Type == "audio":
		return "B 站音乐"
	}
	return "B 站视频"
}

// locateCurrent 是顶上那个「定位当前播放」。
//
// 滚到 `targetIndex * 64`（Biu 的 `scrollToPlayItem` 那个乘法）：列表定高 64，
// 第 i 行的顶边就在 i*64 处，所以偏移是**算出来的**，这也是行高必须定死的原因。
//
// ⚠️ `ScrollState.ScrollTo(..., ui.Smooth)`，**别换成 `ListState.ScrollTo`**——
// 那个是一跳到位（`mygo/ui/list.go`），按下去「啪」地一下就过去了，而验收要的是
// 「平滑滚过去」。末尾几行会按 MaxY 钳住，和 Biu 那个 `Math.min(targetTop, maxTop)`
// 同一件事，这里不自己算一遍。
//
// 两种「滚不了」都要说话（Biu 的两个 addToast）：默默不动的话，用户分不清是按钮
// 坏了还是自己记错了。
func (s *Shell) locateCurrent() {
	// ⚠️ **先看有没有歌在放，再看队列的当前项**（理由同 playingIdentity）：只有队列
	// 那一项的话，接歌失败停下来之后它还指着下一首，按这一下会把用户滚到一首
	// 放不出来、也没在放的歌上面去——而屏幕上播放条是空的。
	if !s.app.Snapshot().HasTrack {
		s.addToast("当前没有正在播放的歌曲", notice.Warning)
		return
	}
	cur, ok := s.app.Queue().Current()
	if !ok {
		s.addToast("当前没有正在播放的歌曲", notice.Warning)
		return
	}
	id := cur.Identity()
	if id == "" {
		s.addToast("当前这一首认不出来，没法定位", notice.Warning)
		return
	}
	idx := -1
	for i, r := range s.queueRows {
		if r.item.Identity() == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.addToast("播放列表里没有当前播放的歌曲", notice.Warning)
		return
	}
	// 末尾那几行：目标偏移会超出内容，框架会把 glide 的目标钳在 MaxY 上
	// （`mygo/ui/scroll.go` 的 glide），和 Biu 那个 `Math.min(targetTop, maxTop)`
	// 同一件事，这里不自己算一遍。
	s.queueScroll.ScrollTo(0, float32(idx)*queueRowHeight, ui.Smooth)
}

// queueEmpty 是抽屉里没有歌时那一句。
//
// 标题用共享的「暂无内容」（和 `internal/ui/empty.go` 里那几条一样：上游的
// `<Empty />` 没有专属文案，落到的就是共享默认那句），Hint 写的是**用户能做
// 的下一步**，不是「这个功能还没做」。
//
// ⚠️ **末行不许只剩一个字**（票 #241 / 报告 F-24）：这一句在抽屉那 352 宽的
// 内容区里折成两行，原来那一版是 31 个字——折完末行只剩「来」一个字（中文
// 孤字，行尾单字应回避）。现在 33 个字：折点无论落在哪一格，末行都有 2~5 个字。
var queueEmpty = emptyState{
	Title: "暂无内容",
	Hint:  "点歌右边的「加入队列」，或者用播放条上的「加歌」把本地的文件放进来",
}

// ---- 那一行的菜单（五项） ----

// queueMenuItem 是行菜单里的一项。
type queueMenuItem struct {
	key   string
	label string
}

// queueMenuItems 是这一行的菜单：Biu `music-playlist-drawer/menu.tsx` 的
// `getMenus` 那一套，一项不多一项不少：
//
//	收藏 / 下载音频 / 下载视频 / 在 B 站打开 / 从列表删除
//
// Biu 对本地项把前四项藏起来（`getMenus({isLogin, isLocal})` 里的 `hidden`）。
// **这里不藏**：今天唯一把项放进队列的入口是 `enqueuePicked`（本地文件），藏起来的
// 话普通用户看到的行菜单**永远只有「从列表删除」一项**，五项一条都见不到。摆出来
// 按不动的那两项（见 `queueAction`）各自说清为什么，不是一句笼统的
// 「现在还不能用」。
//
// 第四项的字按这一项的来处换（本地项写「在访达中显示」），判断和内容列表那一套
// 菜单共用一份（`list.OpenLabel`）。
func queueMenuItems(item app.Item) []queueMenuItem {
	return []queueMenuItem{
		{key: "favorite", label: "收藏"},
		{key: "download-audio", label: "下载音频"},
		{key: "download-video", label: "下载视频"},
		{key: "bililink", label: list.OpenLabel(item)},
		{key: "del", label: "从列表删除"},
	}
}

// queueMenu 建这一行的菜单。
//
// ⚠️ **每一项都要 Item 一遍**：菜单是这趟全部建出来的，只有 Chosen 的那一项为
// true。少建一项，那一项就永远点不到（和 `sidebar_favorites.go` 的 favoriteMenu
// 同一个道理）。
//
// ⚠️ row 什么时候是真的：菜单被点中的那一帧，框架会用**新的一帧重跑一遍**每一行
// 的 row 回调（`ui/menu.go` 里 `rt.consumed = true` 那一句），闭包里的 row 因此
// 总是用户按下去那一刻的那一行。行的身份是判重标识、框架按标识重新对位，所以
// 哪怕「上一首 / 下一首」在这中间挪了 cur，拿到的仍然是用户按的那一行。
func (s *Shell) queueMenu(m *ui.Menu, row queueRow) {
	for _, item := range queueMenuItems(row.item) {
		if m.Item(item.label).Chosen() {
			s.queueAction(row, item)
			return
		}
	}
}

// queueAction 是菜单里那一项被选中之后做的事。
//
// **五项全都真的在做那件事**，这一层只有「点了 → 调服务 → 把结果说给用户」：
//
//	收藏        App.FavoriteSong —— 接口调用、把 bvid 换成 avid、挑哪一格、判重复，
//	            全在 `internal/service/bili/favorite.go` 里（这里一句 B 站的知识都没有）
//	下载音频    App.Download
//	下载视频    App.DownloadVideo —— 取视频流 + 交织成一个 mp4 都在下载服务里
//	打开来处    list.Actions.Open
//	从列表删除  App.RemoveSongFromList
//
// 收藏要问 B 站四到五次，所以走**回调**（见 App.FavoriteSong）；下载是「加进队列、
// 立刻返回」。
func (s *Shell) queueAction(row queueRow, item queueMenuItem) {
	switch item.key {
	case "favorite":
		s.favoriteQueueRow(row)
	case "download-audio":
		s.startDownload(row.item)
	case "download-video":
		s.startDownloadVideo(row.item)
	case "bililink":
		// 内容列表那套 `list.Actions.Open`：本地项先判访达、再判 B 站地址。
		// 自己照着再写一遍的话，抽屉和列表会对同一项给出两种说法。
		list.AppActions(s.app, s.addToast).Open(row.item)
	case "del":
		s.removeQueueRow(row)
	}
}

// removeQueueRow 是「从列表删除」。
//
// ⚠️ 删的是**全部同身份项**，不是这一行对应的那一份：这一行是按判重标识画的，同一
// 首排了两遍只画一行，只删一份的话它画完还在（顶上的计数也不变），用户会以为按了
// 没反应。Biu 的 `play-list.ts` 里 `del` 用的就是 `isSame` 删全部。
//
// 认不出是哪一项的（`Identity` 是空串）走按位置删那一份——它没有身份可循，而这一行
// 在队列里的位置是准的。
func (s *Shell) removeQueueRow(row queueRow) {
	if row.item.Identity() == "" {
		s.sayQueueErr(s.app.RemoveAt(row.at))
		return
	}
	if _, err := s.app.RemoveSongFromList(row.item); err != nil {
		s.addToast(err.Error(), notice.Danger)
	}
}

// startDownload 是「下载音频」：加进下载队列（`app.Download`，下载那一票的实现）。
func (s *Shell) startDownload(item app.Item) {
	if !s.canDownload(item) {
		s.addToast("这一首已经在你磁盘上了，没有东西可下载", notice.Warning)
		return
	}
	if _, err := s.app.Download(item); err != nil {
		s.addToast("下不了这一首："+err.Error(), notice.Danger)
		return
	}
	s.addToast("已加进下载队列", notice.Success)
}

// startDownloadVideo 是「下载视频」：取视频流、交织成一个 mp4，落在下载目录里。
//
// **画质那一档传 `bili.VideoQualityUnknown`**（让 B 站按它的默认给）：设置页还没接
// 「视频下哪一档」，这里写死一个高画质只会让用户在流量不多的网络下下几百兆，
// 却不知道为什么这么大。
func (s *Shell) startDownloadVideo(item app.Item) {
	if !s.canDownload(item) {
		s.addToast("本地文件没有视频可下载", notice.Warning)
		return
	}
	if _, err := s.app.DownloadVideo(item, bili.VideoQualityUnknown); err != nil {
		s.addToast("下不了这个视频："+err.Error(), notice.Danger)
		return
	}
	s.addToast("已加进下载队列", notice.Success)
}

// canDownload 报这一项有没有东西可下载。
//
// 本地文件没有：B 站上还没有它，而「下载」下的是 B 站上还没下过的那一段。
func (s *Shell) canDownload(item app.Item) bool {
	return !isLocalItem(item) && item.Bvid != ""
}

// isLocalItem 报这一项是不是本地文件。
//
// 判断在 `app.Item.IsLocal` 那边——**别在这里再写一份**：这个项目里已经因为
// 「两处各写一份本地文件长什么样」付过一次代价（少判一个字段的话，手写的
// Item 会被当成本地文件，而症状是「在线的歌在访达里显示」）。
func isLocalItem(item app.Item) bool { return item.IsLocal() }

// favoriteQueueRow 是「收藏」。
//
// 结果走**回调**（`App.FavoriteSong` 要问 B 站四到五次，不该在界面线程上等），回调
// 里弹的那句已经由 app 层用 `bili.Friendly` 说好了——这一层不判断成功失败，它只知道
// 「成了 / 没成，理由是什么」。
func (s *Shell) favoriteQueueRow(row queueRow) {
	if isLocalItem(row.item) || row.item.Bvid == "" {
		s.addToast("这一首是本地文件，B 站上没有对应的东西", notice.Warning)
		return
	}
	s.app.FavoriteSong(row.item, func(folder bili.FavFolder, err error) {
		if err != nil {
			s.addToast("收藏不了："+err.Error(), notice.Danger)
			return
		}
		name := folder.Title
		if name == "" {
			name = "收藏夹"
		}
		s.addToast("已收藏到「"+name+"」", notice.Success)
	})
}

// sayQueueErr 把队列那一层的话原样说给用户听，**不加工任何前缀**：话是 app 那边
// 写的（`RemoveAt` 说的是「已经删掉了，但接不上下一首：…」，不是「删不掉」——
// 删是真的删掉了）。界面再套一句就是在当着用户的面说假话。
func (s *Shell) sayQueueErr(err error) {
	if err != nil {
		s.addToast(err.Error(), notice.Danger)
	}
}
