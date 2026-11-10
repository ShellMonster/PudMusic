// 播放列表抽屉（「深夜唱片行」§0′.T4，原型 `refreshDrawer` 的 queue 一支）：宽 400、surface 底、
// 左边一条 `line`、`--shadow-pop`；头是 24 号宋体「播放列表」+「清空播放列表」+ 关闭键，头下面一行
// `N 首 … 播放模式`；列表按「已播放 / 正在播放 / 接下来」分段，每行是拖动把手 + 40 的封面 +
// 歌名（13 / 500）和「歌手 · 时长」+ 上移 / 移除（正在放的那一行是跳动的三根柱）。保留队列中的
// 每一项，行菜单仍是上游那五项加末尾一项「复制歌名」（票 #685，理由见 `queueMenuItems`）。
//
// 它浮在主布局**外面**：`ui.Overlay` + `Absolute`（MyGo 文档
// `docs/ui/overlays.md` 里有一个抽屉的例子），不在 `viewMain` 那个 `Row` 里
// 留一列——留在那儿抽屉一开窗口就窄一块，页面被挤着走，那不是抽屉。
package ui

import (
	"strconv"
	"strings"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/rowkey"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scrim"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shadow"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	tclean "github.com/ShellMonster/PudMusic/internal/ui/widgets/title"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// 尺寸（原型 `.qrow` / `.drawer`）：宽 400（窄窗铺满）、行是 `20px 40px 1fr auto` 四列、缝 12、
// 内边距 8（40 + 8 + 8 = 56 高）、封面 40、圆角 4。分段标签 `.q-label`：12 号 text-3、
// 内边距 12 / 8 / 4。
const (
	queueDrawerWidth = 400 // = tok.DrawerW（无类型常量，测试里拿它做整数运算）
	queueCoverSize   = 40  // = tok.CoverRow
	queueGripW       = 20
	queueRowPad      = space.Px8
	queueRowGap      = space.Px12

	queueChildCoverSize = 32
	queueChildGripW     = 16
	queueChildRowPad    = space.Px6
	queueChildRowGap    = space.Px8
	queueChildIndent    = space.Px12
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

// queueSlideDuration 是抽屉滑入要走多久：原型 `drawer-in var(--t-slow) var(--ease-out)` = 360ms。
const queueSlideDuration = motion.Slow

// queueSlideAnim 是抽屉**进场**那一段动画（360ms ease-out）；`queueSlideOutAnim` 是**出场**
// （原型 `drawer-out 220ms var(--ease-in)`）。一个过渡只有一个时长，所以进出场各一份，按方向挑。
//
// ⚠️ **它们是包级变量，不能每帧现造**：每帧一个分配而每帧分配是我们唯一的性能判据
// （`internal/ui/framebudget_test.go`）。**走曲线那一支，不做回弹**（Duration > 0 才是曲线那一支，
// Duration == 0 会落进 500ms 的弹簧）。
var (
	queueSlideAnim    = ui.Animation{Duration: queueSlideDuration, Ease: motion.EaseOut}
	queueSlideOutAnim = ui.Animation{Duration: motion.ExitDrawer, Ease: motion.EaseIn}
)

// drawerSlide 是抽屉往 target 滑时该用的动画：出场（target ≠ 0）用 `queueSlideOutAnim`。
func drawerSlide(target float32) ui.Animation {
	if target != 0 {
		return queueSlideOutAnim
	}
	return queueSlideAnim
}

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
// at 是它在**队列**里的下标：抽屉逐项展示队列，所以行号和队列下标一致；点一行、
// 放一首、删一行都使用这个位置。
type queueRow struct {
	item app.Item
	at   int
	// group 是「这一行属于哪个合集」那一份，**只有合集行有**。
	//
	// 🔴 **它不是 `item.GroupKey`**：那一栏是队列项自己带着的「我出自哪个
	// 合集」；这里是**这一帧画出来的行**上的组信息——同一个 `Item` 在
	// 队列里出现两份时，两份属于同一合集；如果它们不连续，则分属不同的
	// UI 分段组头。
	//
	// ⚠️ **散歌是 nil**：不是合集的行没有它，画了也不该有那一根竖线。
	group *queueGroup
	// key 是这一行的身份（铁律四）。见 queueRowKey。
	key string
	// id 是这一首的**判重标识**（Item.Identity），仅用于判断当前播放项；同一首歌的
	// 多份队列项各自保留自己的行。
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

// queueGroup 是抽屉里**一个分组**的那一份：组头 + 它带的那些集。
//
// ⚠️ **它是「这一帧画出来的组」，不是「队列里的组」**：队列里没有「组」这
// 个实体——合集只是几行带着同一个 `GroupKey` 的 `Item`，「它们是一组」是
// 抽屉画的时候**按 `GroupKey` 归拢**出来的。所以这一个结构只活在这一帧
// （`queueRowsForSnapshot` 顺手归拢），下一帧重新归。
//
// # 为什么归在行上而不是归在队列上
//
// 「这几行是一个分组」是**界面**的事（队列只管「下一首是谁」，那一层认
// `GroupKey` 一个字段就够）。把「组」做成队列实体的话，删一行、拖一行都
// 要顺手维护组的边界——而那一份在队列里用不上，白扛一份不变量。
type queueGroup struct {
	// Key 是这一组的键（`Item.GroupKey`，形如 "season:<id>"）。
	Key string
	// RowKey 是这一段组头的 UI key。同一合集在队列里分成多段时，每段都要独立。
	RowKey string
	// Title 是组头那一行要写的合集名（`Item.GroupTitle`，空串时界面那一格留空）。
	Title string
	// Count 是这一组一共几集（`Item.GroupCount`，第一集带着的就是总数）。
	Count int
	// Cover 是组头那一小枚封面（**第一集的封面**，和集行同一份来源）。
	Cover string
	// Owner 是组头副题那一段「UP 主」（第一集的 Artist——同一个合集里
	// 各集的 UP 多半是同一位）。
	Owner string
	// FirstAt 是这一组第一集在**队列**里的下标（组头要点开时按它定位）。
	FirstAt int
	// Limited 是「仅播第 1 首」此刻开没开（**单组**那一道，读的是快照）。
	Limited bool
	// PageGroup 表示这是多 P 视频组，而不是合集/系列组。
	PageGroup bool
	// CurrentAt 是当前播放项在队列里的下标；收起时用于提示当前第几集。
	CurrentAt int
}

// queueRowKey 是这一行的 key。
//
// 同一首歌的重复项用它在该歌曲副本中的序号区分；不同歌曲的插入不会改变已有行的
// key，因此悬停、焦点和菜单状态可以跟着原来的队列项保留。
func queueRowKey(id string, occurrence int, at int) string {
	if id != "" {
		return "queue-row:" + id + "!" + strconv.Itoa(occurrence)
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
	id  string
	key string
}

// queueRowRaw 是「那两串身份」的原料。
type queueRowRaw struct {
	item       app.Item
	occurrence int
	at         int
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
	return queueRowIdentity{id: id, key: queueRowKey(id, r.occurrence, r.at)}
}

// queueRowsFor 是抽屉这一帧要画的那几行：队列里的每一项都保留一行。
//
// 队列允许同一首歌重复入队，抽屉必须把每一份都展示出来，才能让可见计数、
// 行位置和下一首播放的实际顺序一致。行身份由判重标识和队列下标组成，
// 所以重复项仍有稳定且唯一的 key。
//
// ⚠️ **那两串身份是记好的那一份**（票 #339）：现拼的话每帧每行一到两次分配。
// queueRowsFor 保留纯函数测试和旧调用方的整列接缝；绘制路径使用 revision 快照。
func queueRowsFor(items []app.Item, ids *rowkey.Memo[queueRowRaw, queueRowIdentity]) []queueRow {
	q := app.NewQueue()
	q.Reset(items...)
	return queueRowsForSnapshot(q.Snapshot(), ids)
}

// queueRowsForSnapshot 是抽屉这一帧要画的那几行：**队列里的每一项都保留一行**，
// 分组项外加按 `GroupKey` 归拢出组头。
//
// ⚠️ **组是「按 GroupKey 归拢」出来的，不是「队列里有组」**：同一个合集的行
// 在队列里可以分开（用户拖过、手动加过），归拢认的是**身份**不是「连续几行」——
// 所以同一个合集的两段也会画成**两个组头**（它们是两段，不是一个组）。
func queueRowsForSnapshot(snapshot app.QueueSnapshot, ids *rowkey.Memo[queueRowRaw, queueRowIdentity]) []queueRow {
	rows := make([]queueRow, 0, snapshot.Len())
	// groups 是「这一帧归出来的组」：按 GroupKey 记，**第一集出现的位置**
	// 插组头，后面的集**插在它自己那行**（行序 = 队列序），不挪到一起。
	//
	// ⚠️ **记的是 `*queueGroup`**：同一个合集的两段各自一份（它们的行不
	// 连着），但**同一 GroupKey 只能归出一个组**——同一个合集在队列里出现
	// 两段时，第二段会归进第一段那个组头底下，而「这一段从哪儿开始」按
	// `FirstAt` 算不出来。所以这里用**「一段连续的同 key 行」当一个组**：
	// key 变了就结一个组，同 key 续上。
	var cur *queueGroup
	seen := make(map[string]int, snapshot.Len())
	for i := 0; i < snapshot.Len(); i++ {
		it, ok := snapshot.At(i)
		if !ok {
			continue
		}
		raw := queueRowRaw{item: it}
		ident := ids.At(i, raw, packQueueRowIdentity)
		if ident.id == "" {
			raw.at = i
			ident = ids.At(i, raw, packQueueRowIdentity)
			rows = append(rows, queueRow{item: it, at: i, key: ident.key, id: ident.id})
			cur = nil
			continue
		}
		occurrence := seen[ident.id]
		if occurrence > 0 {
			ident = ids.At(i, queueRowRaw{item: it, occurrence: occurrence}, packQueueRowIdentity)
			seen[ident.id] = occurrence + 1
		} else if ident.id != "" {
			seen[ident.id] = 1
		}
		row := queueRow{item: it, at: i, key: ident.key, id: ident.id}
		// 不是合集行：散歌直接追加，顺手把「当前组」断掉——下一个同 key 的集
		// 进来要新起一个组（它是一段，不接在前一段后面）。
		if it.GroupKey == "" {
			rows = append(rows, row)
			cur = nil
			continue
		}
		// 同组续上：cur 还是同一个 key 就并进它；换 key（或者刚断掉）就新起一个。
		if cur == nil || cur.Key != it.GroupKey {
			cur = &queueGroup{
				Key:       it.GroupKey,
				RowKey:    "queue-group:" + it.GroupKey + "!" + strconv.Itoa(i),
				Title:     it.GroupTitle,
				Count:     it.GroupCount,
				Cover:     it.Cover,
				Owner:     it.Artist,
				FirstAt:   i,
				Limited:   snapshot.GroupLimited(it.GroupKey),
				PageGroup: isPagesGroupKey(it.GroupKey),
				CurrentAt: -1,
			}
			// 组头行：`at` 用组里第一集的队列下标，key 加 `!group` 前缀和集行分开。
			rows = append(rows, queueRow{
				at:    i,
				key:   cur.RowKey,
				group: cur,
			})
		}
		row.group = cur
		if i == snapshot.CurrentIndex() {
			cur.CurrentAt = i
		}
		rows = append(rows, row)
	}
	return rows
}

func queueRowsSameShape(a, b []queueRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].at != b[i].at || a[i].id != b[i].id {
			return false
		}
		var aGroupKey, bGroupKey string
		if a[i].group != nil {
			aGroupKey = a[i].group.RowKey
		}
		if b[i].group != nil {
			bGroupKey = b[i].group.RowKey
		}
		if aGroupKey != bGroupKey {
			return false
		}
	}
	return true
}

// queueGroupRows 是这一帧要画的**组头那一行**（Open 时它后面跟着各集）。
//
// ⚠️ **行数会跟着开合变**（收着的组头后面不画集行）：所以这一份是「行 →
// 画什么」的**索引**——`queueRows` 装的是全部，`groupRows` 装的是「这一帧
// 看得见的那几行」。
func queueGroupRows(rows []queueRow, open map[string]bool) []queueRow {
	out := make([]queueRow, 0, len(rows))
	for i, r := range rows {
		if r.group != nil && r.item.GroupKey != "" {
			// 是某一集：组收着就不画。
			if !open[r.group.RowKey] {
				continue
			}
			out = append(out, r)
			continue
		}
		// 是组头行：它自己画（后面的集归 `open` 管，不用在这儿记）。
		if r.group != nil {
			out = append(out, r)
			continue
		}
		out = append(out, rows[i])
	}
	return out
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
		// 抽屉不在了，那道确认也不该留着——留着的话下一次开抽屉时它自己弹出来
		// （而用户上一次明明已经取消过了）。
		s.queueClearOpen = false
		return
	}
	// 记一笔「抽屉建了一遍」——给「一帧建了几遍」那条测试数（见 builds.go）。
	//
	// ⚠️ 这一句数的是**两个调用方**：满屏播放器开着的时候抽屉由满屏那一支
	// 建（`viewFullPlayer` 末尾那一句），而 `viewMain` 必须让开。两处都建
	// 的话这里会是 2——而画面上看着没差。
	s.builds.drawer++
	qSnapshot := s.app.Queue().Snapshot()
	if s.queueRowsRevision != qSnapshot.Revision() {
		nextRows := queueRowsForSnapshot(qSnapshot, &s.queueRowIDs)
		if !queueRowsSameShape(s.queueRows, nextRows) {
			s.queueGroupOpen = map[string]bool{}
		}
		s.queueRows = nextRows
		s.queueRowsRevision = qSnapshot.Revision()
	}
	t := c.Theme()
	p := tok.Of(t)
	ui.Overlay(c, func() {
		// ⚠️ 底边留出底下那一条的高度（原型 `.scrim { inset: 0 0 var(--bar-h) 0 }` 和
		// `.drawer { bottom: var(--bar-h) }`）：这一层是吃点击的，不留出播放条那一截，播放条就
		// **整个被它盖住**——抽屉开着时按播放、上一首、下一首都按不动。满屏开着的时候播放条
		// 被满屏盖着，抽屉直接铺到底（原型：满屏开着时 scrim / drawer 的 bottom 归 0）。
		bottomInset := float32(playbarHeight)
		if s.full.open {
			bottomInset = 0
		}
		winW, _ := c.Size()
		width := min(float32(queueDrawerWidth), winW)

		// 遮罩的**可见**那一层：32% 黑、淡入；退场开始就往透明走（220ms）。它不接指针
		// （`PassThrough`）——吃点击的是下面那个透明的 `back`。
		fade := float32(0)
		if s.drawerPhase == drawerOut {
			fade = 1
		}
		ui.Box(c).Key("queue-scrim").Absolute().Left(0).Top(0).Right(0).Bottom(bottomInset).
			Background(ui.RGBA(0, 0, 0, scrim.Drawer)).PassThrough().Transition(motion.ScrimIn).
			AnimatePose(ui.Pose{Fade: fade}, queueSlideOutAnim)

		back := ui.Row(c).Key("queue-backdrop").
			Absolute().Left(0).Top(0).Right(0).Bottom(bottomInset)
		if s.drawerPhase == drawerOut {
			// ⚠️ **退场那 220ms 里，这片蒙版不吃点击**：它是透明的，用户看不见它，而它盖住整页——
			// 看不见的东西吃点击，就是「点不动」。
			back = back.PassThrough()
		} else {
			// ⚠️ **开着的时候它是模态**：Modal 把焦点关进抽屉、外面那些快捷键让路。
			back = back.Modal()
		}
		back.Children(func() {
			// ⚠️ **面板是绝对定位的**：进出场要动的是**布局位置**（`Right(slide)`），flex 那一套
			// 摆不出「从右边滑进来」。
			panel := shadow.Pop(ui.Column(c).Key("queue-drawer").
				Absolute().Top(0).Bottom(0).Width(width).
				Background(p.Surface).BorderStartWidth(1).BorderColor(p.Line), t)
			// ⚠️ **面板在退场时照旧接点击**：它还在屏幕上，而「看不见的东西吃点击」才是坏的。
			// ⚠️ **别用 `Disabled` 关它**：框架把禁用画成半透明，那会让面板一边滑一边淡。
			panel.Role(ui.RoleDialog).Label("播放列表").
				Children(func() {
					s.viewQueueHeader(c, t)
					s.viewQueueBody(c, t, qSnapshot)
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
				panel.Jump(queueSlideKey, -width)
			}
			// 目标：开着就贴住右沿（0），关着就整个在窗口外面。
			target := float32(0)
			if s.drawerPhase == drawerOut {
				target = -width
			}
			slide := panel.AnimateTo(queueSlideKey, target, drawerSlide(target))
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
			s.closeQueueDrawer()
		}
	})

	// 「清空播放列表」那道二次确认。
	//
	// ⚠️ **排在抽屉之后**：它是浮在抽屉**上面**的（框架的浮层都挂在同一个
	// overlay root 上，后建的在上），而它自己是模态的——在它上面点任何别的控件
	// 都不该生效。
	//
	// ⚠️ **放在这里而不是 `viewMain` 里**：满屏播放器开着的时候抽屉由满屏那一支
	// 建（`viewFullPlayer` 末尾那一句），而 `viewMain` 让开——放在这儿两处都
	// 盖得住。
	s.viewQueueClearConfirm(c)
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

// viewQueueHeader 是顶上那两条：头（宋体 24「播放列表」、「清空播放列表」、关闭键）和头下面一行
// `N 首 … 播放模式`（原型 `.drawer-head` / `.drawer-sub`）。
//
// ⚠️ **「清空」要先问一次**（票 #454）：这一下是一键销毁整个队列 + 停掉播放，这里只记下「要问」
// （`queueClearOpen`），真正那一下在 `viewQueueClearConfirm` 里。空队列时「清空播放列表」禁用，
// 抽屉始终有一个可见的出口（关闭键）。
//
// ⚠️ **原型的头里没有「定位当前播放」**，这个界面入口已删；`app` 层的队列能力不动。
//
// ⚠️ 原型的头下面还有一段总时长（`41 分钟`）和每一行的曲长；队列里的 `app.Item` 没有时长这一栏
// （只有正在放的那一首知道），所以这里只有条数和播放模式，曲长只给正在放的那一行。
func (s *Shell) viewQueueHeader(c *ui.Context, t *ui.Theme) {
	// 歌数按**队列项**算（`queueSongCount`），不是按画出来的行——组头行也
	// 在 `s.queueRows` 里，数它会把「N 首」写成「N + 组头数」。
	n := queueSongCount(s.queueRows)
	p := tok.Of(t)
	pud.DrawerHead(c, "播放列表", func() {
		clear := pud.ButtonDisabled(c, "queue:clear", pud.Quiet, pud.Sm, "清空播放列表", "", n == 0)
		if clear.Clicked() {
			s.queueClearOpen = true
		}
		if s.withTip(c, pud.IconBtn(c, "queue:close", "x", "关闭播放列表", pud.IconOpts{}), "关闭播放列表").Clicked() {
			s.closeQueueDrawer()
		}
	})
	pud.DrawerSub(c, func() {
		typo.Mono(ui.Text(c, s.queueCountText(n))).FontSize(scale.Font(c, scale.Size12)).
			TextColor(p.Text3).SingleLine()
		ui.Spacer(c)
		ui.Text(c, s.app.Snapshot().PlayMode.Desc()).FontSize(scale.Font(c, scale.Size12)).
			TextColor(p.Text3).SingleLine()
	})
}

// queueCountText 是头下面那一行的「N 首」。条数不变就原样返回（现拼是每帧一次分配）。
func (s *Shell) queueCountText(n int) string {
	if s.queueCountStr == "" || s.queueCountN != n {
		s.queueCountN, s.queueCountStr = n, strconv.Itoa(n)+" 首"
	}
	return s.queueCountStr
}

// viewQueueClearConfirm 是「清空播放列表」之前那道二次确认。
//
// ⚠️ **用仓库现成的那一道**（`confirmModal`，见 confirmmodal.go）：删收藏夹、
// 删下载记录、删本地文件、清除已看完——**全走它**。同一类破坏性动作两套反应是
// 不行的（报告 §四 的第 43 条点名的就是这一处）。
//
// ⚠️ **danger（红）**：这一下毁掉的是**整个队列**，而且撤不回来（B 站那边没有
// 「撤销队列」这回事），连正在放的那首也停了。它和「清除失效内容」那一种不是
// 一回事——那一个只从本地记录里拿掉几行。
//
// ⚠️ **确认之后给一句事后消息**：确认框问的是「要不要」，而「已经清掉了」是另一
// 句话——本仓库对做完的破坏性动作都有一句（见 `downloadlist_ops.go` 的
// `downloadDoneText` 那一套）。
func (s *Shell) viewQueueClearConfirm(c *ui.Context) {
	if !s.queueClearOpen {
		return
	}
	switch confirmModal(c, &s.queueClearOpen, confirmSpec{
		tone:    toneDanger,
		title:   "清空播放列表吗？",
		body:    queueClearBody(queueSongCount(s.queueRows), s.app.Snapshot().HasTrack),
		confirm: "清空",
		cancel:  "取消",
	}) {
	case confirmAccepted:
		// 先关弹窗再做那件事：做的时候会弹提示（addToast），而弹窗还开着的话
		// 用户看到的是提示压在弹窗底下（同一件事见 `later.go` 的
		// `viewLaterClearConfirm`）。
		s.queueClearOpen = false
		s.app.ClearQueue()
		s.addToast("已清空播放列表", notice.Success)
	case confirmCancelled:
		// 取消 / Esc / 点外面：**什么都不做**。
		s.queueClearOpen = false
	}
}

// queueClearBody 是那道确认的正文：说清毁掉的是什么、有几首。
//
// 数的是队列里实际会被清掉的项数，和抽屉标题显示的行数一致。
func queueClearBody(n int, playing bool) string {
	what := "队列里的 " + strconv.Itoa(n) + " 首会清掉"
	if playing {
		what += "，正在放的那首也会停下来"
	}
	return what + "。"
}

// queueSongCount 是「队列里有几首歌」——**只数歌行，不数组头行**。
//
// ⚠️ **它和 `len(rows)` 不一样**：`s.queueRows` 里有组头行（它不是一首歌，
// 是「这一摞是什么」），而「N 首」「队列里 N 首会清掉」数的都是**歌**。
// 数错的症状是「队列里 9 首」画成「9 + 2 组头 = 11 首」。
func queueSongCount(rows []queueRow) int {
	n := 0
	for _, r := range rows {
		if r.item.GroupKey != "" || r.group == nil {
			// 是歌行：要么带着组标记（是某一集），要么压根没组（散歌）。
			// 组头行是「有 group、没 GroupKey」的那一行，跳过它。
			n++
		}
	}
	return n
}

// viewQueueBody 是抽屉下面那一块：有歌就是那一列，没有歌是一句空状态（分岔照
// Biu 的 `{list.length ? <VirtualList/> : <Empty/>}`）。
func (s *Shell) viewQueueBody(c *ui.Context, t *ui.Theme, snapshot app.QueueSnapshot) {
	if snapshot.Len() == 0 {
		ui.Column(c).Key("queue-empty").Grow(1).Center().Children(func() {
			pud.Empty(c, pud.ArtPlain, queueEmpty.Title, queueEmpty.Hint, nil)
		})
		return
	}
	s.viewQueueList(c, t, snapshot)
}

// viewQueueList 是播放列表抽屉里那一列队列。每个队列项都是一行，行高定死 64——
// 「定位当前播放」滚到的 `targetIndex * 64` 靠的就是这个定值。
func (s *Shell) viewQueueList(c *ui.Context, t *ui.Theme, snapshot app.QueueSnapshot) {
	// rows 是**这一帧画出来的行**：合集收着的时候它的集行不画（组头还在），
	// 所以这一份比 `s.queueRows`（队列里的全部）少那几行。
	//
	// ⚠️ **`s.queueRows` 不动**：`queueDrop` 的落点是「队列里的下标」，按全部
	// 那份算才和队列对得上；这里另算一份「画出来的是哪几行」给它用。
	all := s.queueRows
	rows := queueGroupRows(all, s.queueGroupOpen)
	// ⚠️ **这一帧只取一次快照**（`viewMain` 那条规矩：两次取的话两者之间状态
	// 可能变了，而画出来的是两份不同的状态）。「当前这一首」和「停住了」都从
	// 这一份里读。
	st := s.app.Snapshot()
	playing := st.HasTrack
	current := s.app.Queue().CurrentIndex()
	cur := currentRow(rows, current, playing)
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
		if rows[cur].group != nil {
			rows[cur].group.CurrentAt = rows[cur].at
		}
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

	// 这一帧「当前行是第几行、在不在响、多长」交给每一行读（分段标签和正在放那一行用）。
	s.queueCurrent, s.queuePlaying, s.queueDur = cur, st.Playing, st.Duration

	// 行 key 包含判重标识和队列位置，重复项因此各自稳定且唯一；组头行
	// 带着 `queue-group:` 前缀，和集行互不相撞。
	s.queueList.Key = func(i int) any {
		return s.queueRowKeys.At(i, rows[i].key, rowkey.Identity)
	}
	// Label 是这一行给无障碍和「打字选中」用的名字；组头行说合集名。
	s.queueList.Label = func(i int) string {
		if g := rows[i].group; g != nil && rows[i].item.GroupKey == "" {
			return g.Title
		}
		return rows[i].item.Title
	}

	// 落点先清掉：**每一行自己记**（见下面 viewQueueRow 里那几句
	// PointerPosition），指针不在任何一行上时它就该是「没有落点」。
	s.queueDropAt = -1
	// 「有东西正被拖着」这一帧的结论也先清掉：**每一行自己报**（见下面
	// viewQueueRow 里那一句 Dragging）——它是「这一行是不是拖动源」。
	s.queueDraggingNow = false

	e := ui.List(c, &s.queueList, len(rows), func(i int) {
		s.viewQueueRow(c, rows[i], i, t)
	}).
		// 滚动位置写进自己的句柄，「定位当前播放」改的就是它（ScrollTo 带动画）。
		TrackScroll(&s.queueScroll).
		// 抽屉正文内边距 8 / 12（原型 index.html:614 `.drawer-body { padding: 8px 12px }`）：
		// 行的悬停底、拖动落点线收在这一圈里面，不铺到抽屉边。
		PaddingY(space.Px8).PaddingX(space.Px12).
		Grow(1).FillWidth()
	s.trackScrollbar(e)

	// 这一列是**拖动的落点**（框架把落点交给指针底下最里层那个肯收拖动值的
	// 元素——行里的封面、字、⋯ 都不收，所以收到的是这一列）。
	//
	// ⚠️ **只问 Drop，不问 DragOver**：框架那两样（Drop / DragOver）每问一次
	// 都要新建一个闭包（那个闭包要抓住类型，见 mygo/ui/dragdrop.go 的
	// coreDrop），而队列抽屉一屏有十来行——每行每帧问一次就是每帧二十几次
	// 分配（见 framebudget_test.go，那一档的上限是 43 次）。
	//
	// ⚠️ **问一次，不要每行问一次**：问这一列是两次（这里两行），问每一行就是
	// 二十六次。
	//
	// ⚠️ **Drop 读到的是「刚刚落下」那一次**：这一句在列表建完之后才问，而框架
	// 是在建列表的过程中把落点交给这一列的——问得晚一帧没关系，落下那一次
	// 记在元素的状态里，下一帧还在。
	//
	// ⚠️ **Drop 不能省**：问它的那一句同时把这一列**登记成收拖动值的元素**
	// （`accepts` + `flagValueDrop`）——框架是照着这个登记找落点的，不登记的话
	// 松手时压根找不到这一列。
	if v, ok := ui.Drop[*queueDragValue](e); ok {
		s.queueDrop(v)
	}
	// 有东西正被拖着经过这一列：拖着的时候要画那条插入线。
	//
	// ⚠️ **这一句是「上一帧」的结论**（插入线在建行的时候画，而这一帧的结论
	// 要等列表建完才知道）——差一帧看不出来，理由见 shell.go 里
	// queueDragging 那一条。
	s.queueDragging = s.queueDraggingNow
}

// currentRow 是「正在放的那一首」在抽屉里的行号，没有是 -1。
//
// 当前播放项由队列下标定位；同一首歌重复出现时，只有当前项对应的那一行高亮。
func currentRow(rows []queueRow, current int, playing bool) int {
	if !playing || current < 0 {
		return -1
	}
	// 当前项在**队列**里的下标；这一帧画出来的行里，第 i 行的 `at` 记的就
	// 是队列下标——收着的组里那一行没画出来，它的 `at` 照样能认出来。
	for i := range rows {
		if rows[i].group != nil && rows[i].item.GroupKey == "" {
			continue
		}
		if rows[i].at == current {
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

// queueRowInnerKey 是每一行里那个「行本体」的身份（常量：行的外壳已经带着稳定的 key，里面只有一个）。
const queueRowInnerKey = "queue-row-body"

// queueSectionLabel 是这一行上面要不要有分段标签（原型 `refreshDrawer`）：
// 当前这一行上面写「正在播放」，它下面那一行写「接下来」，当前不在头一行时第 0 行上面写「已播放」。
func queueSectionLabel(at, current int) string {
	switch {
	case current < 0:
		return ""
	case at == current:
		return "正在播放"
	case at == current+1:
		return "接下来"
	case at == 0 && current > 0:
		return "已播放"
	}
	return ""
}

// viewQueueRow 是抽屉里的一行：散歌使用标准行；合集/分集子行使用紧凑行，
// 通过更小封面、缩进和左侧轨道表现父子层级。
//
// 行本体是 `20px 40px 1fr auto` 四列、缝 12、内边距 8、圆角 8；悬停 `hover` 底；正在放的歌名是
// accent-text；拖着的那一行 40% 不透明，落点处一条 2 像素的主色线。点这一行 = 从这儿听。
// 分段标签（「正在播放」等）在行本体**外面**——悬停、拖动、点击都只算行本体。
func (s *Shell) viewQueueRow(c *ui.Context, row queueRow, at int, t *ui.Theme) {
	p := tok.Of(t)
	// 组头行：这一行不是歌，是「这一组合集」的开合把和它的那一枚钮。
	// 它不播、不拖、不进分段标签（那一套认的是「这一首」）。
	if row.group != nil && row.item.GroupKey == "" {
		s.viewQueueGroupRow(c, row, t)
		return
	}
	state := row.state
	label := queueSectionLabel(at, s.queueCurrent)
	child := row.group != nil
	rowPad, rowGap := float32(queueRowPad), float32(queueRowGap)
	coverSize := float32(queueCoverSize)
	gripWidth := float32(queueGripW)
	titleSize, subtitleSize := float32(scale.Size13), float32(scale.Size12)
	if child {
		rowPad, rowGap = float32(queueChildRowPad), float32(queueChildRowGap)
		coverSize = queueChildCoverSize
		gripWidth = float32(queueChildGripW)
		titleSize, subtitleSize = float32(scale.Size12), float32(scale.Size11)
	}
	wrap := ui.Column(c).Key(row.key).FillWidth()
	wrap.Children(func() {
		if label != "" {
			ui.Text(c, label).FontSize(scale.Font(c, scale.Size12)).TextColor(p.Text3).
				Padding(space.Px12, space.Px8, space.Px4, space.Px8).SingleLine()
		}
		rowEl := ui.Row(c).Key(queueRowInnerKey).FillWidth().Padding(rowPad).Gap(rowGap).
			AlignItems(ui.Center).Radius(tok.R8)
		// 悬停要读在 Children 之前：指针是在布局那一步才知道落在哪儿的。
		hovered := rowEl.Hovered()
		if hovered {
			rowEl = rowEl.Background(p.Hover)
		}
		// 这一行是**拖动源**：拖它就是重排队列（见 queue_drag.go）。拖动值是指针（装指针不分配）。
		rowEl = rowEl.Drag(s.queueDragFor(at, row))
		if rowEl.Dragging() {
			rowEl = rowEl.Opacity(0.4)
			// 这一行就是拖动源 → 「有东西正被拖着」（用这一句顶掉框架的 DragOver，它每问一次
			// 都要新建一个闭包）。
			s.queueDraggingNow = true
		}
		// 落点就记在这一行：指针在这一行的上半还是下半。落点认**队列下标**
		//（`row.at`），不是这一帧画出来的行号——收起的组不画行，行号和队列
		// 下标已经对不上（`queueDrop` 索引的是 `s.queueRows`）。
		if _, py, over := rowEl.PointerPosition(); over {
			s.queueDropAt = row.at
			s.queueDropUpper = py < rowEl.Bounds().H/2
		}
		// 拖着的时候在落点那一行画一条插入线：挪到哪儿得看得见（原型 `.qrow.drag-over`）。
		if s.queueDragging && s.queueDropAt == row.at {
			s.queueDropLine(at, rowEl, p.Accent)
		}
		// 合集里的一集：行最左缘一条 accent 竖线（原型那条贯穿整组的
		// `border-left`），行内容在竖线右边缩进——把手 ⠿ 还在（和散歌同一列），
		// 竖线不在把手列里，它贴的是**行**的左缘。
		if child {
			accent := p.Accent
			if row.group.Limited {
				accent = p.LineStrong
			}
			ui.Box(c).Absolute().Left(0).Top(0).Bottom(0).Width(2).Background(accent)
			rowEl = rowEl.Margin(space.None, space.None, space.None, queueChildIndent)
		}
		rowEl.Children(func() {
			// 把手（散歌和集行都有）：标准行 20 宽，紧凑子行 16 宽。
			ui.Box(c).Width(gripWidth).Shrink(0).Center().Children(func() {
				gripSize := float32(14)
				if child {
					gripSize = 12
				}
				ui.Icon(c, pud.Icon("grip")).FontSize(gripSize).TextColor(p.Text3)
			})
			// 封面保持圆角 4 和 `surface-2` 占位底；父行 40，子行 32。
			ui.Box(c).Size(coverSize, coverSize).Shrink(0).Radius(tok.R4).Clip().
				Background(p.Surface2).Children(func() {
				imgcache.ViewFlat(c, s.images, row.item.Cover, coverSize, coverSize, tok.R4)
			})
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				cleaned := tclean.Clean(row.item.Title)
				ui.Text(c, cleaned.Title).FontSize(scale.Font(c, titleSize)).
					FontWeight(500).TextColor(playlistTextColor(t, state)).SingleLine().Ellipsis("…")
				// 合集行的副题：「第 N 集 · UP 主」——「这是第几集」比「这是谁」
				// 更要紧（用户在这一摞里认的是集数）。
				sub := s.queueRowSub(row, state)
				if row.group != nil && row.item.GroupIndex > 0 {
					sub = "第 " + strconv.Itoa(row.item.GroupIndex) + " 集"
					// 歌名提取出的那一段（`Cleaned.Extra`：从方括号 / 破折号
					// 里挪出来的歌手 / 专辑）优先于 `item.Artist`——合集里每集
					// 的标题常带「合集名 · 歌手」，B 站接口的 owner 是整个合集
					// 的 UP 主，不是这一集的歌手。
					who := cleaned.Extra
					if who == "" {
						who = row.item.Artist
					}
					if who != "" {
						sub += " · " + who
					}
					// 「仅播第 1 首」开着：第 2 集起要说「这一集不会被接着放」。
					if row.group.Limited && row.item.GroupIndex > 1 {
						sub += " · 播完第 1 集就停"
					}
					if state == queueRowCurrent && s.queueDur > 0 {
						sub += " · " + clock(s.queueDur)
					}
				}
				subText := ui.Text(c, sub).FontSize(scale.Font(c, subtitleSize)).SingleLine().Ellipsis("…")
				// 「仅播第 1 首」开着的组：第 2 集起整行变暗（原型 `.limited .qrow` 半透明）。
				if row.group != nil && row.group.Limited && row.item.GroupIndex > 1 {
					subText = subText.Opacity(0.5)
				}
				subText.TextColor(p.Text2)
			})
			// 操作：正在放的那一行是三根跳动的柱（右边空 12），别的行是上移 + 移除（32 的键）。
			if state == queueRowCurrent {
				ui.Row(c).Margin(space.None, space.Px12, space.None, space.None).Label("正在播放 " + row.item.Title).Children(func() {
					pud.EqBars(c, "queue:eq", s.queuePlaying)
				})
				return
			}
			ui.Row(c).Shrink(0).Children(func() {
				up := pud.IconBtn(c, "queue:up", "up", s.queueRowOps.At(row.at, row.item.Title, queueUpLabel),
					pud.IconOpts{Row: true, Size: queueActionIconSize(child), Disabled: row.at == 0})
				if up.Clicked() && s.app.Queue().MoveAtBefore(row.at, row.at-1) {
					s.wake.invalidate()
				}
				if pud.IconBtn(c, "queue:remove", "x", s.queueRowRm.At(row.at, row.item.Title, queueRemoveLabel),
					pud.IconOpts{Row: true, Size: queueActionIconSize(child)}).Clicked() {
					s.removeQueueRow(row)
				}
			})
		})

		// 点这一行 = 从这儿听。⚠️ 点正在放的那一行不重头播（见 app.PlayAt）。
		if rowEl.Clicked() {
			if err := s.app.PlayAt(row.at); err != nil {
				s.addToast("放不了这一首："+err.Error(), notice.Danger)
			}
		}
	})
	// 右键菜单挂在**整行**（含上面的分段标签）上：MyGo 取的是最里层那个带菜单的元素，整行这一层足够里面，
	// 而在分段标签上按右键也出同一份菜单（行的无障碍名 / 测试按歌名找到的是整行的中心）。
	wrap.ContextMenu(func(m *ui.Menu) { s.queueMenu(m, row) })
}

func queueActionIconSize(child bool) float32 {
	if child {
		return tok.IconSm
	}
	return tok.Icon
}

// viewQueueGroupRow 是抽屉里一个分组的**组头行**（原型 `.chead`）。
//
// 它不是歌：不播、不拖、不进分段标签。点它开合（收着的组里那些集不画）；
// 行尾那一枚「仅播第 1 首 / 仅播第 1 集 / 恢复连播」是这个分组自己的临时开关，不动设置。
//
// ⚠️ **点它播的是这一组的第一集**（`row.at` 记的就是第一集的队列下标），
// 和点集行里第 1 集是同一个动作——合集行不画「▶」，但点它照样从这儿听。
func (s *Shell) viewQueueGroupRow(c *ui.Context, row queueRow, t *ui.Theme) {
	p := tok.Of(t)
	g := row.group
	open := s.queueGroupOpen[g.RowKey]
	wrap := ui.Column(c).Key(row.key).FillWidth()
	wrap.Children(func() {
		rowEl := ui.Row(c).Key(queueRowInnerKey).FillWidth().Padding(queueRowPad).Gap(queueRowGap).
			AlignItems(ui.Center).Radius(tok.R8)
		if rowEl.Hovered() {
			rowEl = rowEl.Background(p.Hover)
		}
		rowEl.Children(func() {
			// 开合箭头（原型 `.carat`）：开着是 ▸ 转 90°，收着是 ▸。它不是
			// 图标，是「这一组收着还是开着」的那一个记号。
			//
			// ⚠️ **占 `queueGripW` 那一列**：散歌行第一列是 20 的把手，集行
			// 第一列是 20 的竖线——组头不给 ▸ 同一列宽的话，它的封面和
			// 名字比别的行左移一截，左侧三档对不齐。
			ui.Box(c).Width(queueGripW).Shrink(0).Center().Children(func() {
				carat := ui.Text(c, "▸").FontSize(scale.Font(c, scale.Size11)).TextColor(p.Text3).SingleLine()
				if open {
					carat.TextColor(p.Text)
				}
			})
			// 封面 + 集数角标：和集行同一枚 40 封面，右下压一个「N」的小块
			// （原型 `.g-cov .gcnt`），让用户一眼认出「这是一摞，不是一首」。
			ui.Box(c).Size(queueCoverSize, queueCoverSize).Shrink(0).Radius(tok.R4).Clip().
				Background(p.Surface2).Children(func() {
				imgcache.ViewFlat(c, s.images, g.Cover, queueCoverSize, queueCoverSize, tok.R4)
				// 集数角标：右下，mono 10，半透明黑底（原型 `.gcnt`）——
				// 原型就是这个黑块，不是主色。
				ui.Box(c).Absolute().Right(0).Bottom(0).Padding(space.Px2, space.Px4).
					Background(ui.RGBA(0, 0, 0, scrim.Badge)).Radius(tok.R4).Children(func() {
					typo.Mono(ui.Text(c, strconv.Itoa(g.Count))).FontSize(scale.Font(c, scale.Size11)).
						TextColor(ui.RGBA(255, 255, 255, 0.92)).SingleLine()
				})
			})
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, tclean.Clean(g.Title).Title).FontSize(scale.Font(c, scale.Size13)).
					FontWeight(500).TextColor(p.Text).SingleLine().Ellipsis("…")
				ui.Text(c, queueGroupSub(g)).FontSize(scale.Font(c, scale.Size11)).
					TextColor(p.Text2).SingleLine().Ellipsis("…")
			})
			// 「仅播第 1 首 / 恢复连播」这一枚：**单组**的临时开关，不动设置。
			// 开着时它是 accent 描边 + 淡底（原型 `.sw.off`）。
			label := "仅播第 1 首"
			if g.PageGroup {
				label = "仅播第 1 集"
			}
			if g.Limited {
				label = "恢复连播"
			}
			btn := pud.Button(c, "queue:grouplimit", pud.Quiet, pud.Sm, label, "")
			if g.Limited {
				btn = btn.BorderColor(p.Accent).TextColor(p.AccentText).Background(p.AccentSoft)
			}
			if btn.Clicked() {
				s.app.SetCollectionGroupLimited(g.Key, !g.Limited)
			}
		})
		// 点这一行 = 开合（收着的组不画集行）；⚠️ **不是播第一集**——开合是
		// 这个组头自己的事，播放要走「点这一行 = 从这儿听」那一套（那要按
		// 到第一集的行上）。
		if rowEl.Clicked() {
			s.queueGroupOpen[g.RowKey] = !open
			s.wake.invalidate()
		}
	})
	// 右键菜单挂在**整行**上：和集行同一份（那一行 `FirstAt` 就是这一组的
	// 第一集，菜单里的动作按它办）。
	wrap.ContextMenu(func(m *ui.Menu) { s.queueMenu(m, row) })
}

// queueGroupSub 是组头副题那一行：「合集/分集 · UP 主 · N 集」。
//
// ⚠️ **「合集」是一个小 tag**（原型 `.tag`）：它不是字，是「这一行不是一首
// 歌」的那一个记号——没有它，组头和普通行只在「右上角多一枚钮」上差。
func queueGroupSub(g *queueGroup) string {
	sub := "合集"
	if g.PageGroup {
		sub = "分集"
		if g.CurrentAt >= 0 && g.Count > 0 && g.CurrentAt >= g.FirstAt {
			page := g.CurrentAt - g.FirstAt + 1
			if page > 0 && page <= g.Count {
				sub += " · 当前第 " + strconv.Itoa(page) + " 集"
			}
		}
	}
	if g.Owner != "" {
		sub += " · " + g.Owner
	}
	if g.Count > 0 {
		sub += " · " + strconv.Itoa(g.Count) + " 集"
	}
	return sub
}

func isPagesGroupKey(key string) bool {
	return strings.HasPrefix(key, "pages:")
}

// queueUpLabel / queueRemoveLabel 是两枚行内键的无障碍名字（原型 `上移 {歌名}`、
// `从播放列表移除 {歌名}`，带上歌名好让一屏 N 枚分得出是哪一行）。
func queueUpLabel(title string) string { return "上移 " + title }

func queueRemoveLabel(title string) string { return "从播放列表移除 " + title }

// queueRowSub 是歌名下面那一行：「歌手 · 时长」（原型 `small`）。
//
// 停住的那一行说的是「停住了」，不是它的来源（理由见 `queueRowSubtitle`）；时长只有正在放的那一首
// 知道，其余的行没有这一段。
func (s *Shell) queueRowSub(row queueRow, state queueRowState) string {
	switch state {
	case queueRowStopped, queueRowDrained:
		return queueRowSubtitle(row.item, state)
	}
	who := row.item.Artist
	if who == "" {
		who = itemSubtitle(row.item)
	}
	if state == queueRowCurrent && s.queueDur > 0 {
		return who + " · " + clock(s.queueDur)
	}
	return who
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
	p := tok.Of(t)
	switch state {
	case queueRowCurrent:
		return p.AccentText
	case queueRowStopped, queueRowDrained:
		return p.Text2
	}
	return p.Text
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
	Title: "暂无播放内容",
	Hint:  "在歌曲上选择「添加到播放列表」或直接播放。",
}

// ---- 那一行的菜单（上游五项 + 「复制歌名」） ----

// queueMenuItem 是行菜单里的一项。
//
// sep 的是**分隔那一行**——它不是一项，框架那一层由 `Menu.Separator` 画；放进
// 项表是为了让项序读起来和菜单上看到的顺序一致（和 `widgets/list/menu.go` 的
// `menuItem.sep` 同一个写法）。
type queueMenuItem struct {
	key   string
	label string
	sep   bool
}

// queueMenuSeparator 是分隔那一行。
var queueMenuSeparator = queueMenuItem{sep: true}

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
//
// # 末尾那一段「复制歌名」（票 #685）
//
// 上游那五项之后**另起一段**加一项只读动作：用户要把歌名复制出去（发给别人、
// 去搜这一首），而抽屉是唯一能看「这一串还要放什么」的地方——那里原先一个字
// 都复制不出来（内容列表那一套「复制歌曲信息」只有列表 / 网格上有）。
//
// ⚠️ **排在最后、还隔一条分隔线**：前面五项有现成的断言盯着（
// `queue_drawer_test.go` 那两条逐项比项序，它们盯的是「上游那五项照抄」），
// 插在中间会把那两条变成「改文案就得改测试」的麻烦。而它在语义上也确实是
// 另一类：前五项都动东西（收藏 / 下载 / 打开 / 删），这一项一个字节都不动。
func queueMenuItems(item app.Item) []queueMenuItem {
	return []queueMenuItem{
		{key: "favorite", label: "收藏"},
		{key: "download-audio", label: "下载音频"},
		{key: "download-video", label: "下载视频"},
		{key: "bililink", label: list.OpenLabel(item)},
		{key: "del", label: "从列表删除"},
		queueMenuSeparator,
		{key: "copy-title", label: queueCopyLabel},
	}
}

// queueCopyLabel 是抽屉行菜单里「复制歌名」那一项的字。
//
// ⚠️ **和内容列表的「复制歌曲信息」不同名**：那个复制的是「歌名 - UP 名」加
// 地址（`list.SongInfoText`），这个只复制歌名——同一句字用在两处的话，用户
// 粘出来才发现多了一半东西。
const queueCopyLabel = "复制歌名"

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
		if item.sep {
			m.Separator()
			continue
		}
		if m.Item(item.label).Chosen() {
			s.queueAction(row, item)
			return
		}
	}
}

// queueAction 是菜单里那一项被选中之后做的事。
//
// **每一项都真的在做那件事**，这一层只有「点了 → 调服务 → 把结果说给用户」：
//
//	收藏        App.FavoriteSong —— 接口调用、把 bvid 换成 avid、挑哪一格、判重复，
//	            全在 `internal/service/bili/favorite.go` 里（这里一句 B 站的知识都没有）
//	下载音频    App.Download
//	下载视频    App.DownloadVideo —— 取视频流 + 交织成一个 mp4 都在下载服务里
//	打开来处    list.Actions.Open
//	从列表删除  App.RemoveSongFromList
//	复制歌名    剪贴板接缝（票 #685，见下面那一支）
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
	case "copy-title":
		s.copyQueueTitle(row)
	}
}

// copyQueueTitle 把这一行的歌名放进剪贴板（票 #685）。
//
// ⚠️ **复制的是界面上显示的那个名字**（`tclean.Clean` 清洗过的，和
// `viewQueueRow` 里画的是同一句）：界面上写着「晴天」而复制出来是「【4K修复】晴天」，
// 用户会以为复制错了行。这一条和 `widgets/list` 的 `TestCmdCCopiesTheCleanedTitle`
// 是同一个口径。
//
// ⚠️ **走 `s.copyText`，不是裸的 `s.wake.writeClipboard`**（票 #685 的对抗性
// 审查）：后者**不取号**，于是在途的封面那一趟（后台编 PNG，慢）跑完时问
// `isLatest(gen)` 仍然是 true，它会照写——用户最后按的「复制歌名」被一张旧
// 封面盖掉。`copyText` 走 `commitNow`，取号 + 写在同一段里（理由见
// `clipboard.go` 的 `copyClipboard`）。
//
// ⚠️ **无头 Tester 里 `mygo.Clipboard.Write*` 会 panic**（`needsApp`）——
// `copyText` 内部走的是 `Shell.writeClipboardText` 那个接缝，测试把它换成
// 记下内容的桩。
func (s *Shell) copyQueueTitle(row queueRow) {
	if err := s.copyText(tclean.Clean(row.item.Title).Title); err != nil {
		s.addToast("复制不了歌名："+err.Error(), notice.Danger)
		return
	}
	s.addToast("已复制歌名", notice.Success)
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
	s.sayQueueErr(s.app.RemoveAt(row.at))
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
