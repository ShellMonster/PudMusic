// 封面上的 hover 反馈：列表行和网格卡片那两套，外加「正在放」那个跳动波形。
//
// Biu 上游这三块是 `components/music-list-item/index.tsx:100-107`（列表行的压暗 +
// 播放三角 + 序号位置的跳动波形）、`components/music-card/index.tsx:39-47, :50-55`
// （网格卡片右下角的播放钮 + 封面底部的渐变条）。本项目两边都缺：列表行只有一个
// 「序号换成一个写着『播放』的按钮」，网格卡片一点 hover 反馈都没有，序号位置摆的是
// 一句静态的「正在播放」。
//
// # 尺寸都是抄上游的，一个没改
//
//	列表行：48×48 封面上盖 35% 黑 + 一个 20px 的白色三角，指针停在三角上时放大 110%
//	网格卡：右下角浮出 40px 主色圆播放钮，封面底部一条 black/90 渐变（左播放量右时长）
//	淡入淡出 200ms（上游两处都是 `transition-opacity duration-200`）
//
// ⚠️ **时长抄上游，曲线是全应用那条统一的**（`motion.Unified`，票 #292）。
//
// # ⚠️ 反馈是**每一帧都建**的，指针不在也建
//
// 这是这一包唯一一处要动脑的地方。淡入要跨帧：三角画出来那一帧不透明，它得从
// **上一帧**的值接着往下走。而 MyGo 的动画值记在**节点**上（`ui.AnimateTo` 的
// `st.motions`），节点的身份跨帧不变才有那份状态。于是这一块不能写成
//
//	if hover { 建三角 }
//
// 那样节点每帧都是新的，`Animate` 走的是 `channel(..., made=true)` 那一支、直接返回
// 目标值（`mygo/ui/motion.go` 的 AnimateTo），于是**淡入变成「啪」地一下出现**——不
// 报错、不闪红，只是没动效。
//
// 现在的写法是每帧都建、完全透明时用 `Invisible()` 收掉，理由逐条写在 `show` 那里。
package hover

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/iconset"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/motion"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scrim"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/shadow"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// frameVisible 是「画这一帧的那个窗口，用户现在看得到吗」那一个开关。
//
// ⚠️ **它是逐窗口一份的**（票 #392）：主窗口失焦不等于迷你播放器看不见
// （`internal/ui/presence.go`：「为什么每个窗口各有一份」）。所以这里放的是
// **一个指针**——每一扇窗装上自己那一份，而这一格波形在**建的时候**把指针
// 认下来（见 `Wave`）。
//
// ⚠️ **为什么是指针、而不是建的时候问一次「看得见吗」**：波形是**画的时候**
// 问的（那一帧走**局部重绘**，只重画、不重建），而可见性会变——窗口失焦的
// 那一刻答案就变了。一个**活的**开关（`atomic.Bool`）能被立刻改掉，而一个
// 建的时候问出来的布尔值要等下一帧重建时才会更新——那中间窗口还在按帧重画
// （票 #156 治的就是那个：失焦时白烧电）。
//
// 零值必须是「看得见」：这个包自己的测试直接调 `Wave`，没有窗口来装开关；
// 设成 false 的话那些测试里的波形会永远不动，而它们测的正是「动」。
//
// ⚠️ **这个开关只管「要不要下一帧」，不管「这一帧画不画」**：看不见的时候
// 能走到这里的那一帧照样画完整（也许是某条后台路径送进来的），只是画完不再
// 要下一帧。真正让帧停下来的那一半在 `wakeHandle.invalidate`（wake.go）。
var frameVisible = NewVisibleFlag()

// NewVisibleFlag 造一个「看得见」的开关（给 `SetVisibleFlag` 用，和
// `frameVisible` 那一个默认值一样）。
func NewVisibleFlag() *atomic.Bool {
	v := &atomic.Bool{}
	v.Store(true)
	return v
}

// SetVisibleFlag 装上「这一扇窗看不看得见」那一个开关。只由
// `internal/ui.Shell.View` 每帧调一次（装的是这一扇窗自己那一份）。
//
// ⚠️ **装的是指针，不是「看得见吗」**：这个包里的波形在**画的时候**问那个
// 开关（那一帧只重画、不重建），所以问出来的答案必须是**活的**——窗口失焦
// 的那一刻它就得变（理由见上面 `frameVisible` 那一段）。
func SetVisibleFlag(visible *atomic.Bool) {
	if visible == nil {
		visible = NewVisibleFlag()
	}
	frameVisible = visible
}

const (
	// Fade 是淡入淡出的时长。Biu 那两处都是 `duration-200`。
	Fade = 200 * time.Millisecond

	// dimAlpha 是列表行封面上那层压暗。**35% 黑**，逐字抄上游
	// （`music-list-item/index.tsx:100-107` 的 `bg-black/35`）。
	dimAlpha = scrim.Overlay

	// triSize 是列表行封面上那个播放三角的边长。上游是 20。
	triSize = 20

	// triScale 是指针停在三角上时它放大的倍数。**110%**，抄上游那一处。
	triScale = 1.1

	// btn 是网格卡片右下角那个圆播放钮的直径。上游是 40。
	btn = 40

	// btnInset 是那个播放钮离封面边留多少。
	btnInset = space.Px8

	// barH 是封面底部那一条渐变多高。
	barH = 22

	// barTextInset 是渐变上那几个字离右边多远。**播放钮的宽再加一个 btnInset**
	// （40 + 8）：按钮从右边往里让了 btnInset，所以「播放钮占的那一条 + 再空一个
	// btnInset」正好是字该让出的宽——只让 `btn` 的话字的右边缘和按钮的左边缘**贴
	// 着**，而那一列 11 号的字读起来是「顶着」按钮的。
	barTextInset = btn + 2*btnInset

	// triTint 是播放三角和封面底部那几个字的白。**带一点透明**：纯白压在一张浅色
	// 封面上会看不见，而封面是什么颜色我们不知道。
	triTint = scrim.OnScrim
)

// PlayFrom 是「按下去从这一首往后听」这句话。
//
// ⚠️ 「按下去」和**双击那一行**是同一件事（`playFrom`），所以提示是这一句而不是
// 「播放」：说「播放」的话用户第一次按下去会发现整张歌单都换了队列——那正是它该做
// 的事，只是那句话没有提前说。
const PlayFrom = "从这一首往后听"

// PlayNow 是「按下去加这一首并立即播」这句话。
//
// ⚠️ 和 PlayFrom 分开：首页「新歌速递」那张卡片按下去保留原队列、追加并立即播这一首
// （上游 `new-music-top.tsx` 的 `handlePlay`），说「从这一首往后听」会提前许诺一
// 件那一下不做的事。
const PlayNow = "播放"

// PlayingMark 是「这一行正在放」那句话，无障碍标签和无头 Tester 都靠它认这一行。
//
// ⚠️ **它的唯一出处在这里**（`list.PlayingMark` 和 `internal/ui` 那个都指回来）。
// 同一个应用里两套列表写着两种说法，用户会以为是两个意思——那是 BMG-56 已经付过账
// 的一个结论。
const PlayingMark = "正在播放"

// FromLabel 是列表行封面上那个播放三角的标签。**带上歌名**：读屏软件读的是标签而不
// 是画出来的那几个字，光说「从这一首往后听」的话用户听到的是一句没头没尾的话，得自
// 己猜是哪一首。顺带它让这一行在 Tester 里可认（`Tester.Find` 认标签）。
func FromLabel(title string) string { return title + " " + PlayFrom }

// PlayLabel 是「加这一首并立即播」那枚播放钮的标签，理由同 FromLabel。
func PlayLabel(title string) string { return title + " " + PlayNow }

// ProfileTip 是「按下去进他的 UP 空间」那一句提示。
//
// # 为什么它和 `PlayFrom` 是两句话
//
// 搜索页「用户」那一档的行**不是歌**：按下去是跳到那个 UP 主的主页
// （`internal/ui/searchresults.go` 的 `openSearchUser` → `navigate("/user/" + mid)`）。
//
// 那一圈反馈原来**三样都在说「放歌」**（图形是一枚播放三角、提示是「从这一首
// 往后听」、无障碍名也是那一句）——用户读到的和发生的对不上：他想进他的主页，
// 屏幕上却写着「我要开始放歌」。三样里只改一样，另一样还在说「放歌」。
//
// ⚠️ **这一句不许写成「播放」**：菜单里那一项已经从「从这一首往后听」改成
// 「播放」了（`widgets/list/menu.go`），而「播放」说的是放歌——那是另一件事，
// 说了还是对不上。
const ProfileTip = "进他的 UP 空间"

// ProfileLabel 是那一档的无障碍名（带上名字），理由同 FromLabel。
func ProfileLabel(title string) string { return title + " " + ProfileTip }

// profileIcon 是「进这个人的空间」那一枚图形：一个人。
//
// ⚠️ **和 `internal/ui` 的 `iconUser` 是同一张图**（`User & Faces/user-line.svg`），
// 而这一处**不能** import `internal/ui`（那是个环：那一层 import 了这一包）——
// 所以走 `iconset`（叶子包，两边共用同一份资源和同一个解析器，见它的文件头）。
//
// ⚠️ **为什么是「一个人」而不是「一张名片」**：应用里另有一枚「个人资料」
// （`Business/profile-line.svg`，头像下拉里那一项），但它那一圈细线在 20 见方
// 的地方糊成一团；一个人头加肩膀在那个尺寸上仍然一眼认得出。而这一枚要画在
// 48 见方的头像正中间，只有 20 大。
var profileIcon = iconset.Must("User & Faces/user-line.svg")

// glyph 是封面那一圈上画的那个图形。
//
// ⚠️ **两种图形是两种事情**：播放三角说「放歌」，那一个人说「进他的空间」。
// 判据在 `widgets/list` 那一层算好（`app.Item.Type == app.ItemTypeUser`）再传进来
// ——这一包在 `widgets/list` 下面，够不着 `internal/app`（那是个环）。
type glyph uint8

const (
	// glyphPlay 是一枚播放三角（「从这一首往后听」）。
	glyphPlay glyph = iota
	// glyphProfile 是一枚「一个人」（「进他的 UP 空间」）。
	glyphProfile
)

// PlayingLabel 是那个跳动波形的标签，理由同 FromLabel。
func PlayingLabel(title string) string { return title + " " + PlayingMark }

// ─────────────────────────────────────────────────────────────────────────────
// 那几个身份：算一次，记下来
// ─────────────────────────────────────────────────────────────────────────────
//
// # 为什么要有这一节
//
// 那一圈反馈（压暗 / 三角 / 渐变条 / 播放钮）每一帧都要建，而它建出来的那几样
// 东西**带着身份**：节点键（`Key`）和动画键（`AnimateWith`）——淡入要跨帧，
// 节点的身份不变才有那份状态（见文件头那一段）。
//
// 现算的话，每一帧每一行要：
//
//   - 拼四个字符串（标签、`:dim`、`:fade`、`:tri`）：**四次分配**；
//   - 把其中三个**装箱进 `any`**（`Key` 和 `AnimateWith` 的参数是 `any`，而
//     `string → any` 的装箱每次都要在堆上分配一次）：**又三次**。
//
// 一屏十三行就是**九十一次**——实测那是首页那一屏每帧 137 次分配里的 91 次
// （见 `internal/ui/framebudget_test.go` 里「首页列表」那一档）。而那几个身份
// **只跟着歌名变**：歌名不变，它们就不变。
//
// # 所以：算一次，记下来，之后每帧拿现成的
//
// ⚠️ **记的是 `any`（已经装箱的那一份），不是字符串**：只把字符串记下来、
// 每帧再装箱一次，等于只省了「拼字符串」那几笔——**装箱那三次还在**。
//
// ⚠️ **键和值都和改之前一模一样**（同一个字符串、同一个后缀），所以节点的
// id（`keyedID` 对字符串走 `maphash.String`）和改之前**逐位相同**——淡入的
// 状态、跨帧的身份，一样都不差。这一节改的是「算几次」，不是「算什么」。

// idsKind 是「哪一种反馈」——每一种要一套自己的身份（同一个盒子里的节点键不能重）。
type idsKind uint8

const (
	// idsRow 是列表行封面那一圈。
	idsRow idsKind = iota
	// idsCard 是网格卡片封面那一圈，「从这一首往后听」。
	idsCard
	// idsCardOne 是网格卡片封面那一圈，「只放这一首」。
	idsCardOne
	// idsRowProfile 是列表行封面那一圈，「进他的 UP 空间」。
	idsRowProfile
	// idsCardProfile 是网格卡片封面那一圈，「进他的 UP 空间」。
	idsCardProfile
)

// idsKey 是缓存的键：歌名 + 哪一种反馈。
//
// ⚠️ **它是结构体而不是字符串**：结构体键进 map **不装箱**（零分配），而
// `string` 键进 `sync.Map` 要装箱（每次一次分配）。
type idsKey struct {
	title string
	kind  idsKind
}

// ids 是「算好的那一份身份」。
type ids struct {
	// label 是无障碍名字（「歌名 从这一首往后听」那种）。
	//
	// ⚠️ **它是字符串而不是 `any`**：`Label` 的参数是 `string`（`mygo/ui` 的
	// `node.Label(s string)`），传字符串不装箱。
	label string
	// dim / fade / tri 是列表行那一圈的三样：压暗那一层的节点键、淡入动画的键、
	// 三角那一层的节点键。
	dim, fade, tri any
	// bar / btn 是网格卡片那一圈的两样：封面底部那一条渐变的节点键、右下角那枚
	// 播放钮的节点键。
	bar, btn any
}

// idsCache 记着「算好的那一份身份」。
//
// ⚠️ **它是全局的，而键里有歌名**：那套身份只跟着**歌名**变（歌名不变，那几个
// 键就不变），所以按歌名记一份就够——按「哪一行」记就得在四个地方各挂一份备忘
// （列表 / 网格 / 稍后再看 / 历史记录），而那四处算的是同一串东西。
var idsCache = struct {
	mu sync.Mutex
	m  map[idsKey]ids
}{m: make(map[idsKey]ids)}

// idsMax 是缓存里最多记多少份。
//
// 满了就整个清掉重来（和 `widgets/title` 那个缓存同一个做法）：够用，也不会
// 无限长。
//
// ⚠️ **这一份缓存的量级算进票 #364 的账**（「调研：怎么降低 MyGo 运行时的
// 内存占用」）：上限一万条，每条一个标签加四个装箱好的字符串；键是**歌名**，
// 所以用户听过多少首歌就留多少条。而它换来的是**每帧少九十一次分配**——
// 原来那九十一个对象是**每帧现造的垃圾**，记一份反而更省。
const idsMax = 10000

// idsOf 返回「这一首歌 + 这一种反馈」要用的那几个身份。
//
// ⚠️ **歌名不变时零分配**：查表不分配（结构体键不进 `any`），返回的是记好的
// 那一份（几个 `any` 是现成的，不用再装箱）。
func idsOf(title string, kind idsKind) ids {
	key := idsKey{title: title, kind: kind}
	idsCache.mu.Lock()
	defer idsCache.mu.Unlock()
	if got, ok := idsCache.m[key]; ok {
		return got
	}
	got := packIDs(title, kind)
	if len(idsCache.m) >= idsMax {
		clear(idsCache.m)
	}
	idsCache.m[key] = got
	return got
}

// packIDs 算出那一份身份。**只在歌名变了的时候走一次**。
func packIDs(title string, kind idsKind) ids {
	var out ids
	switch kind {
	case idsCardOne:
		out.label = PlayLabel(title)
	case idsRowProfile, idsCardProfile:
		out.label = ProfileLabel(title)
	default:
		out.label = FromLabel(title)
	}
	switch kind {
	case idsCard, idsCardOne, idsCardProfile:
		out.bar = out.label + ":bar"
		out.btn = out.label + ":btn"
	default:
		out.dim = out.label + ":dim"
		out.tri = out.label + ":tri"
	}
	// ⚠️ **淡入动画的键两种共用一个字段**：动画状态记在**节点自己**那份里
	// （`mygo/ui` 的 `st.motions`，而 `st` 是按节点 id 找的），所以同一个盒子
	// 里的压暗（列表行）和渐变条（卡片）各用各的节点，互不干扰。
	out.fade = out.label + ":fade"
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 淡入
// ─────────────────────────────────────────────────────────────────────────────

// fade 问 e 在 Fade 这段时间里走到 on 那个值没有，返回此刻的值（0~1）。
//
// ⚠️ **动画只装在 dim 这一个元素上**，别的层用返回值——两份各跑一份动画的话它们会
// 差一帧，而那种差在画面上是「压暗已经上来了、三角还差一丝」。
//
// ⚠️ MyGo 的 `Animate` 在系统开着「减少动态效果」时直接给目标值（`motion.go` 里
// `rt.preferences().ReduceMotion`），那是系统设置而不是我们这一层的判断，别在这里
// 自己绕过去。
//
// ⚠️ **曲线是全应用那条统一的**（`motion.Unified`），不是 `Animate` 的默认
// `EaseOut`——`Animate` 就是 `AnimateWith(..., EaseOut)`，所以这里要显式写
// `AnimateWith`。理由见 DESIGN.md §五。
//
// ⚠️ **key 是 `any` 而不是 `string`**：它是**已经装好的那一份**（见上面「那几个
// 身份」那一节）。`AnimateWith` 的参数本来就是 `any`，而 `string → any` 的装箱
// 每次都要在堆上分配一次——传一个现成的 `any` 就是零分配。
func fade(e ui.Element, key any, on bool) float32 {
	if on {
		return e.AnimateWith(key, 1, Fade, motion.Unified)
	}
	return e.AnimateWith(key, 0, Fade, motion.Unified)
}

// show 收掉一层：完全透明时把这一层**藏掉**。
//
// ## 为什么是 `Invisible()` 而不是「不建」
//
// 见文件头：不建就没有动画状态，淡入变成「啪」地一下。`Invisible()` 的原话是
// 「keeps their room in the layout but draws nothing and **takes neither the
// pointer nor the focus**, and **has no text to find**」（`mygo/ui/api_gen.go`），三件
// 事正好都是这里要的：
//
//   - 房间还在 → 行高不变，三角淡进来的那一帧封面不会跳一下；
//   - 不吃指针 → 指针不在的时候那个播放三角点不到（不然它会盖掉封面）；
//   - **找不到它的字** → 无头 Tester 里 `Find`/`HasText` 找不到它，于是「指针没移
//     上去时没有播放三角」这句话仍然是一句能断言的话。
//
// ⚠️ 判据写 `a <= 0` 而不是 `!on`：`on` 是指针在不在，`a` 是**画出来有多浓**。淡出
// 的那 200ms 里 `on` 已经是 false 了，而那一层还得再画 200ms 才收掉。
func show(e ui.Element, a float32) ui.Element {
	if a <= 0 {
		return e.Invisible()
	}
	return e.Opacity(a)
}

// drawTri 画一个播放三角，r 是它要待的那个盒子，c 是颜色。
//
// ⚠️ **左边多留 15%、上下各留 12%**：一个正三角形放在正方形的正中间，视觉上是偏左的
// （三角形的重心不在几何中心）。不挪的话用户会觉得那个三角没对准封面的中心。
func drawTri(p *ui.Painter, r ui.Rect, c ui.Color) {
	var path ui.Path
	path.MoveTo(r.X+r.W*0.15, r.Y+r.H*0.12).
		LineTo(r.X+r.W*0.15, r.Y+r.H*0.88).
		LineTo(r.X+r.W*0.88, r.Y+r.H*0.5).
		Close()
	p.FillPath(&path, c)
}

// ─────────────────────────────────────────────────────────────────────────────
// 列表行
// ─────────────────────────────────────────────────────────────────────────────

// RowCover 是「列表行的封面 + 它上面那一圈 hover 反馈」，按下去是**从这一首往后听**。
//
// 代替直接调 `imgcache.View`：反馈是**贴在封面上**的，它得知道封面有多大、又摆在
// 哪儿。调用方手里只有「一个 48×48 的封面」，所以连盒子一起给了。
//
// on 是**指针在这一行上**。play 是按下去做的事，为 nil 时那一块不可按。
//
// ⚠️ 这里的封面是方的（列表 48、紧凑 32），所以只收一个 size；网格卡片那边的封面
// 是 140 高跟着格子宽走，那一边收两个（见 CardCover）。
func RowCover(c *ui.Context, cache *imgcache.Cache, url string, size float32,
	on bool, title string, play func()) {
	rowCover(c, cache, url, size, on, title, idsOf(title, idsRow), PlayFrom, glyphPlay, play)
}

// RowCoverProfile 是同一圈反馈，但按下去是**进他的 UP 空间**。
//
// 搜索页「用户」那一档的行走的这一条（`internal/ui/searchresults.go` 的
// `openSearchUser`）：那一行不是歌，是**一个人**。图形、提示、无障碍名三样都
// 跟着换——三样里只换一样，另一样还在说「放歌」（理由见 `ProfileTip`）。
func RowCoverProfile(c *ui.Context, cache *imgcache.Cache, url string, size float32,
	on bool, title string, play func()) {
	rowCover(c, cache, url, size, on, title, idsOf(title, idsRowProfile), ProfileTip, glyphProfile, play)
}

// rowCover 是 RowCover / RowCoverProfile 共用的那一层。
//
// ids 是「这一行的身份」（算好的那一份，见上面「那几个身份」那一节），tip 是
// 指针停在上面时的那句提示，g 是画在封面上的那个图形。
//
// ⚠️ **封面那一图的标签是 `title`（光名字），不是 `ids.label`**：那一图说的是
// 「这是什么」，而上面那一圈反馈说的是「按下去会怎样」——两件事。
//
// （网格那一档的封面图拿的是 `ids.label`，那是那边一直的样子，见 `cardCover`；
// 这一处没跟着改——改它就是改「歌」那一档的读屏效果，而那一档这一票不许动。）
func rowCover(c *ui.Context, cache *imgcache.Cache, url string, size float32,
	on bool, title string, ids ids, tip string, g glyph, play func()) {
	t := c.Theme()
	// ⚠️ **那几个身份是记好的那一份**（见上面「那几个身份」那一节）：现算的话
	// 每帧每行要拼四个字符串、再装箱三次——一屏十三行就是九十一次。
	// 外面那一层盒子是必需的：压暗和那个三角用 `Attach` 贴在**父盒子的内边距
	// 框**上（`mygo/ui/api_gen.go` 的 Attach），而调用方给的父是那一行的 `ui.Row`
	//——贴上去就成了「浮在这一行中间」，横竖都不对。裹一层和封面一样大的盒子，贴的
	// 是它。
	//
	// `Clip()` 不能省：封面是圆角的，压暗那一层不裁就会在四个角上盖出方的黑边。
	ui.Box(c).Size(size, size).Radius(t.Radius).Clip().Children(func() {
		imgcache.ViewTitled(c, cache, url, title, size, size)

		// ⚠️ **压暗和三角是这一层的两个兄弟，不是一个套一个**：`Attach` 贴在**直接
		// 那个父**的盒子上，而套一层时内层贴到的是**外层自己那个盒子**——而外层刚
		// 被 `Attach` 拿掉了布局，量不出盒子来。实测那样画出来的三角浮在封面的左上
		// 角外面（而不是正中），而屏幕上只是「三角歪了」，一眼看不出是哪儿错了。
		//
		// 顺序不能换：后画的盖在先画的上面（绘制和命中都走树序，`mygo/ui/runtime.go`
		// 的 `rt.hits`），三角要压在压暗之上。
		//
		// ⚠️ 这两层都**不占布局**（`Attach` 把它们从父的布局里拿出来了），所以指针
		// 移上去的时候那一行不会横向跳一下。
		dim := ui.Box(c).Key(ids.dim).Size(size, size).Radius(t.Radius).
			Background(ui.RGBA(0, 0, 0, dimAlpha)).
			Attach(ui.AnchorCenter, ui.AnchorCenter)
		// **动画只装在压暗这一层上**，三角用同一个返回值（理由见 fade）。
		a := fade(dim, ids.fade, on)
		show(dim, a)

		tri := ui.Box(c).Key(ids.tri).Size(triSize, triSize).
			Label(ids.label).Tooltip(tip).
			Attach(ui.AnchorCenter, ui.AnchorCenter)
		if g == glyphProfile {
			// 「一个人」：一枚图标（`iconset` 里那张 `user-line.svg`）——图标那一套
			// 自带矢量绘制，比手画一个人头可靠（理由见 `profileIcon`）。
			//
			// ⚠️ **20 见方**：和原来那枚三角**一样大**——反馈的重量不变，只是换了
			// 说的是哪件事。
			tri.Children(func() {
				ui.Icon(c, profileIcon).FontSize(triSize).
					TextColor(ui.RGBA(255, 255, 255, triTint))
			})
		} else {
			// 播放三角。
			//
			// 🔴 **这个闭包不许捕获任何变量**：它每一帧每一行建一次，而**捕获了变量
			// 的闭包会逃到堆上**（每行每帧多一次分配）。`triTint` 是**常量**，
			// 所以这一支编译出来是一个**静态函数值**、一个分配都不占。
			//
			// ⚠️ **别把它挪进一个带参数的函数里**（票 #455 的评审实测）：那样闭包
			// 捕获的就是那个参数，于是每一行每一帧多一次堆分配——首页列表那一档的
			// 分配从 46 涨到 59（上限 54），整条帧预算因此变红
			// （`internal/ui/framebudget_test.go`）。
			tri.Draw(func(p *ui.Painter, r ui.Rect) {
				drawTri(p, r, ui.RGBA(255, 255, 255, triTint))
			})
		}
		show(tri, a)

		// ⚠️ **`Hovered` / `Clicked` 要在它建完之后立刻问**（同 `list/card.go` 里那一
		// 处）：它们报的是这一帧指针在这一格上发生了什么，而那一格得先被画出来才问
		// 得到。
		if play != nil && tri.Clicked() {
			play()
		}
		if tri.Hovered() {
			// 上游是 `hover:scale-110`。`Scale` 的原点默认是盒子中心
			//（`mygo/ui/transform.go` 的 transformAt），所以是往外涨而不是往一边偏。
			_ = tri.Scale(triScale, triScale)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 网格卡片
// ─────────────────────────────────────────────────────────────────────────────

// CardCover 是「网格卡片的封面 + 它上面那一圈 hover 反馈」。
//
// playCount 是封面底部那一行左边的播放量、clock 是右边的时长。**任一个是空串就不画
// 那半边**（本地音乐没有播放量，见 `list/grid.go` 里那一段注释）——画一列空白比不画
// 更难看，而用户看见半行字会以为这一格「数据不全」。
//
// ⚠️ 网格那一档**不给卡片加底板**：上游的 `music-card/index.tsx:31` 上没有任何背景
// 类，两边都是「封面 + 两行字浮在页面上」，那不是差距（票面「不要做」里点名了）。
func CardCover(c *ui.Context, cache *imgcache.Cache, url string, w, h float32,
	on bool, playCount, clock, title string, play func()) {
	cardCover(c, cache, url, w, h, on, playCount, clock, idsOf(title, idsCard), PlayFrom, glyphPlay, play, false)
}

// CardCoverProfile 是同一圈反馈，但按下去是**进他的 UP 空间**。
//
// 搜索页「用户」那一档的格子走的这一条：那一格不是歌，是**一个人**。图形、
// 提示、无障碍名三样都跟着换（理由见 `ProfileTip`）。
func CardCoverProfile(c *ui.Context, cache *imgcache.Cache, url string, w, h float32,
	on bool, playCount, clock, title string, play func()) {
	cardCover(c, cache, url, w, h, on, playCount, clock, idsOf(title, idsCardProfile), ProfileTip, glyphProfile, play, false)
}

// CardCoverOne 是同一圈反馈，但按下去**加这一首并立即播**（`PlayNow`）。
//
// 首页「新歌速递」那一块用它：那张卡片单击就追加并立即播这一首（上游
// `new-music-top.tsx` 的 `handlePlay`），和网格卡片「从这一首往后接」不是一件事。
// 两处的视觉是同一套——同一个应用里同一个手势不该长出两个样子。
//
// ⚠️ **只有这一处的封面自己带一圈影**（`shadow-md`，上游
// `new-music-top.tsx:234` 的 `rounded-medium shadow-md`）。网格卡片那张封面
// **没有**：上游 `music-card/index.tsx:32` 的 `<Image>` 上没有任何 shadow 类，
// 它只有底下那条渐变和右下角那个带影的播放钮。
func CardCoverOne(c *ui.Context, cache *imgcache.Cache, url string, w, h float32,
	on bool, title string, play func()) {
	cardCover(c, cache, url, w, h, on, "", "", idsOf(title, idsCardOne), PlayNow, glyphPlay, play, true)
}

// cardCover 是 CardCover / CardCoverOne / CardCoverProfile 共用的那一层：
// 封面 + 上面那一圈反馈。
//
// coverShadow 说**封面自己**要不要带一圈影（见 CardCoverOne 顶上那一段）。
//
// ⚠️ **影加在封面这一格上，不加在里面的图上**：影是**盒子外面**那一圈，而这一格
// 是裁着的（`Clip`）——影于是画在封面的四周，正好是上游那个 `shadow-md` 的样子。
// 加在里面的图上会变成「图在框里放大」，那是另一回事（hover 放大才那么做）。
func cardCover(c *ui.Context, cache *imgcache.Cache, url string, w, h float32,
	on bool, playCount, clock string, ids ids, tip string, g glyph, play func(), coverShadow bool) {
	t := c.Theme()

	cover := ui.Box(c).Size(w, h).Radius(t.Radius).Clip()
	if coverShadow {
		// `shadow-md` = `0 4px 6px -1px rgb(0 0 0/0.1), 0 2px 4px -2px rgb(0 0 0/0.1)`。
		cover = shadow.TailwindMD(cover)
	}
	cover.Children(func() {
		imgcache.ViewTitled(c, cache, url, ids.label, w, h)
		cardOverlay(c, w, h, on, playCount, clock, ids, tip, g, play)
	})
}

// cardOverlay 画网格卡片封面**上面**那一圈。
func cardOverlay(c *ui.Context, w, h float32, on bool, playCount, clock string, ids ids, tip string, g glyph, play func()) {
	t := c.Theme()

	bar := ui.Box(c).Key(ids.bar).Size(w, barH).
		// 上游是 `bg-gradient-to-t from-black/90`：`from-` 那一头在下边，也就是封面的
		// 底边，往上淡出。`Gradient` 的角度是「从竖直方向顺时针起算，180 是从上到
		// 下」，所以不透明那一头要放在第二个参数上。
		Gradient(ui.RGBA(0, 0, 0, 0), ui.RGBA(0, 0, 0, 0.9), 180).
		Attach(ui.AnchorBottomLeft, ui.AnchorBottomLeft)
	a := fade(bar, ids.fade, on)

	show(bar, a).Children(func() {
		// ⚠️ **`Padding` 的四个值是 上 / 右 / 下 / 左**（CSS 那一套）：这一层**右边**
		// 留 56（`barTextInset`）——那个播放钮就浮在右下角（按钮宽 40 + 离右边 8），
		// 字让得不够就有一半压在按钮底下，用户读到的是「播放量 1.2 万」和一个「时长
		// 没画完的 3:0」；**左边**留 8，播放量贴着封面左边正是上游的样子。
		//
		// 左右写反的话时长压在播放钮底下，而那种错在画面上只是「时长看着偏了一点」。
		ui.Row(c).FillWidth().FillHeight().
			Padding(space.None, barTextInset, space.None, btnInset).Children(func() {
			// ⚠️ **时长那一块是 Grow + 右对齐，播放量不是**：Row 默认把子元素**从左边一
			// 个挨一个**摆，而时长是这一行里最后一块，它要贴着那个播放钮的左边。给它
			// `Grow(1)` + 右对齐，两种情形都对：左边有播放量时它被推到右边，只有时长
			// 时它自己占满整行、仍然贴着右边。（第一版把它放在左边，于是没有播放量的
			// 那一半——本地音乐和首页——时长浮在封面左下角，看着像另一个控件。）
			if playCount != "" {
				ui.Text(c, playCount).FontSize(scale.Font(c, scale.Size11)).
					TextColor(ui.RGBA(255, 255, 255, triTint)).MaxLines(1)
			}
			if clock != "" {
				ui.Text(c, clock).Grow(1).TextAlign(ui.End).FontSize(scale.Font(c, scale.Size11)).
					TextColor(ui.RGBA(255, 255, 255, triTint)).MaxLines(1)
			}
		})
	})

	// 右下角那个主色圆播放钮。上游 `music-card/index.tsx:39-47`。
	circle := ui.Box(c).Key(ids.btn).Size(btn, btn).Radius(btn/2).
		Background(t.Accent).
		Attach(ui.AnchorBottomRight, ui.AnchorBottomRight).Right(btnInset).Bottom(btnInset)
	// ⚠️ **那一圈影是 `shadow-2xl`**（上游 `music-card/index.tsx:51` 的
	// `<div className="bg-primary rounded-full shadow-2xl">`）：
	// `0 25px 50px -12px rgb(0 0 0/0.25)`——**只有一层，颜色是 25% 黑**。
	//
	// 影落在**盒子外面**，而这一枚是贴在封面里的（`Attach`）——所以右边和下边那
	// 两半影会被封面的圆角裁掉，**看得见的是左边和上边那一圈**。上游也一样：
	// 那一枚在 `overflow-hidden` 的封面里（`music-card/index.tsx:33`）。
	shadow.Tailwind2XL(circle)

	show(circle, a).Children(func() {
		e := ui.Box(c).Size(btn, btn).Label(ids.label).Tooltip(tip).Center()
		if g == glyphProfile {
			// 「一个人」：图标那一枚是「和字号一样高、保持长宽比」，所以给它 24
			// （圆的六成，和下面那个三角一样大）就正好。
			e.Children(func() {
				ui.Icon(c, profileIcon).FontSize(btn * 0.6).TextColor(t.AccentText)
			})
		} else {
			// 播放三角。
			//
			// ⚠️ **这个闭包捕获了 `t`（主题）**，所以它会逃到堆上——**而这一处
			// 一直就是这样**（网格那一档的帧预算里一直算着这一次分配）。别为了
			// 「列表行那一支不捕获」把这一支也改了：两处的分配数各自算各自的，
			// 动了哪一头都会动那一条预算。
			e.Draw(func(p *ui.Painter, r ui.Rect) {
				// 三角缩到圆的六成：整个圆那么大的三角会顶出圆外。
				drawTri(p, shrink(r, 0.4), t.AccentText)
			})
		}
		if play != nil && e.Clicked() {
			play()
		}
	})
}

// shrink 把 r 往里收掉 f（0~1），按比例。
func shrink(r ui.Rect, f float32) ui.Rect {
	dx, dy := r.W*f/2, r.H*f/2
	return ui.Rect{X: r.X + dx, Y: r.Y + dy, W: r.W - 2*dx, H: r.H - 2*dy}
}

// ─────────────────────────────────────────────────────────────────────────────
// 正在放
// ─────────────────────────────────────────────────────────────────────────────

const (
	// waveBars 是那个波形有几根竖条。上游那一段是四根。
	waveBars = 4

	// waveBarW 是每根竖条多宽，waveGap 是两根之间空多少。4×2 + 3×2 = 14 宽，摆在序号
	// 那一列（40）里正好居中。
	waveBarW = 2
	waveGap  = space.Px2

	// waveMin / waveMax 是竖条最短和最长时多高，waveH 是这一块盒子多高
	// （12 号字的行高，和原来那一句「正在播放」占的一样高）。
	waveMin = 3
	waveMax = 11
	waveH   = 14

	// wavePeriod 是跳一轮要多久。上游那一段是 800ms 一轮，四根条子各错开四分之一
	// 轮，于是四根永远不同相——同一轮里四根一起落下去的话看着像闪了一下。
	wavePeriod = 800 * time.Millisecond
)

// Wave 是「这一行正在放」那个标记：序号位置上的一段跳动波形。
//
// ## 为什么把它做成会动的
//
// 早先那一版是一句 11px 的「正在播放」，理由写在 `list.PlayingMark` 顶上（「跳要一帧一
// 变，而快速切歌时帧率本来就紧」）。**这一版换掉了那个理由**：界面骨架已经和上游对
// 上了，而这一句字和上游的波形是两回事——用户扫一眼列表时，眼睛先被动的那个抓住。
//
// ⚠️ **窗口看得见的时候它每一帧都要一帧**（`AnimationFrame`）。而这一帧走的是
// **局部重绘**（`p.AnimationFrame()`，不是 `c.AnimationFrame()`）：波形是**画**
// 出来的（四个圆角竖条），而局部重绘那一帧**只重画、不重建整棵元素树**
// （`mygo/ui/runtime.go` 的 `repaintFrame`）——于是「放歌时整窗每秒重建 60 次」
// 变成「每秒重建 20 次（后台 tick 那条链）+ 重画 40 次」，CPU 和电都省一大截
// （票 #392）。
//
// ⚠️ **正因为走的是局部重绘，相位只能在**画**的时候算**：`repaintFrame` 那一帧
// **不重建**，所以建的时候算出来的相位会被**冻结**（闭包捕获的是建那一刻的值）。
// 相位是拿钟算的，于是它改成在 `Draw` 回调里算——那一层拿得到这一帧的时刻
// （`p.Now()`）。
//
// ## color 是**调用方给的**
//
// 波形摆的那一格在选中时是**主色底**（`ui.List` 自己刷的），而主色底上画一条主色波形等
// 于没画。所以这一格颜色是行自己判的（和它的歌名同一套判据），这里不猜。
//
// ⚠️ **标签必须带歌名**（`PlayingLabel`）：理由同文件头那一段——读屏软件读的是标签。
func Wave(c *ui.Context, key string, width float32, color ui.Color, title string) {
	wave := ui.Box(c).Key(key).Size(width, waveH).Label(PlayingLabel(title))
	// 跨帧要记的那一份状态在**建的时候**取一次（`ui.Local` 查表是建的时候的事，
	// 而局部重绘那一帧不重建）。指针在闭包里，跨帧指的是**同一个**那一份
	// （`ui.Local` 按 key 认元素，元素在，那一份就在）。
	st := ui.Local(wave, waveStateKey{}, func() waveState { return waveState{} })
	// ⚠️ **开关在**建的时候**认下来**（指针），而它的值在**画的时候**读：
	// 那个开关是**逐窗口**的（见 `frameVisible` 那一段），而局部重绘那一帧
	// **不重建**——那一刻包级那个指针还是**别的窗口**留下的。
	visible := frameVisible
	wave.Draw(func(p *ui.Painter, r ui.Rect) {
		phase := wavePhase(p, st, visible.Load())
		// 竖条整体居中：4×2 + 3×2 = 14 宽，在 32 宽的序号那一列里两边各空 9。
		x0 := r.X + (r.W-float32(waveBars*waveBarW+(waveBars-1)*waveGap))/2
		for i := range waveBars {
			// 每根条子错开 1/4 轮，四根于是永远不同相。
			h := waveHeight(phase + float32(i)/waveBars)
			p.Fill(ui.Rect{
				X: x0 + float32(i)*(waveBarW+waveGap),
				Y: r.Y + (r.H-h)/2,
				W: waveBarW, H: h,
			}, color, waveBarW/2)
		}
	})
}

// waveState 是波形那一格跨帧要留的两样东西。
//
// ⚠️ **合成一个结构体，而不是两个 `ui.Local`**：本地存储是**每个元素一份**的
// map，而问一次相位就要查一次 map——两份就是两次查、两个键。
type waveState struct {
	// started 是「相位的零点定下了没有」。
	started bool
	// start 是这一格**第一次出现**的那一刻（相位的零点）。
	start time.Time
	// seen 是上一次要帧时那一刻的时刻。零值 = 从来没要过，于是第一帧一定会要。
	seen time.Time
}

// waveStateKey 是 waveState 在元素的本地存储（`ui.Local`）里的键。
//
// ## ⚠️ 它**不带歌名**
//
// 本地存储是**每个元素一份**的，而波形那一格本身就是**按歌名分开的元素**
// （调用方给它的元素键是「wave:」加歌名）——所以这个键只要在这一格里唯一
// 就够了。原先那两个键是 `歌名+":start"` / `歌名+":seen"`，于是**每一帧**都要
// 拼两次字符串、再装箱两次。
//
// ## ⚠️ 为什么是**零大小的结构体**而不是字符串
//
// `ui.Local` 的键是 `any`，而 `string → any` 的装箱每次都要在堆上分配一次
// （`runtime.convTstring`）——这一格每一帧都要问一次相位，于是那一次分配
// 每一帧都付一遍。零大小的结构体装箱走 `runtime.zerobase`，**一次都不分配**。
type waveStateKey struct{}

// wavePhase 是此刻的相位（0~1），并且在**钟走了、窗口也看得见的时候**要下一帧。
//
// ⚠️ **它是在**画的时候**调的**（`Wave` 的 `Draw` 回调里），不是建的时候：
// 波形走的是**局部重绘**（`p.AnimationFrame()`），而局部重绘那一帧**不重建**
// ——建的时候算出来的相位会被冻结在闭包里，波形于是不动了。
//
// 而 `p.Now()` 拿得到这一帧的时刻（`mygo/ui/paint.go`：`p.rt.c.now`），
// 所以相位在画的时候照样算得出来。
//
// ⚠️ **看不见时 `seen` 照样更新，只是不要帧**：恢复可见之后那一次比较仍然
// 「钟走了」，于是会重新要帧，波形也从当前时刻接着跳。要是不更新 `seen`，
// 恢复后那一次也一样会要帧——两者在行为上一样，但更新让「这个字段是上一次
// 看表的时间」这条意思保持诚实，而不是变成「上一次要帧的时间」。
//
// ## 为什么不直接用 `ui.Loop`
//
// `Loop` 每一帧都调 `AnimationFrame()`（`mygo/ui/widgets.go`），而 Tester 的
// `settle()` 只要画面要了帧就一直画到 20 帧为止（`mygo/ui/headless.go`）——于是**只要
// 屏幕上有一行在放，每一次 `tt.Frame()` 都贵二十倍**。实测那一条把 `internal/ui` 整包从
// 27 秒拉到 57 秒，而每一张票的评审都要付这 30 秒。
//
// ## 判据是「钟有没有走」，不是「相位有没有变」
//
// 两者在真窗口里是一回事，而**在无头 Tester 里差很远**：它的钟站着不动
// （`headless.go`：「Its clock stands still unless the test moves it with Advance」）。
//
//   - 真窗口：`runFrame` 每一帧都取一次墙钟（`runtime.go` 的 `now := rt.now()`，然后
//     `rt.c.reset(now, …)`），一帧一个时刻，两帧不会相同 → 每帧都要一帧（窗口看得见
//     时）→ 波形照跳。
//   - Tester：`settle()` 里那 20 帧**共用一个时刻** → 只有第一帧要 → `settle()` 画两
//     帧就收工。测试要看哪一刻的样子自己 `Advance` 多少（见 `hover_test.go` 的
//     `advance`：`Advance` 之后还要补一帧 `Frame()`——`Advance` 只画它那一刻之前到点的
//     那些帧，刻意的，免得在同一个时刻上白画）。
//
// ⚠️ **判据不是「相位有没有变」**：那样它在 Tester 里会**彻底不动**——因为没有下一帧
// 就没人问「相位变没变」，而没有下一帧就没人看（死锁）。问钟不会：那一次 `Advance` 推了
// 钟，就有一帧跑起来，帧里钟和上一帧不一样，于是它又能要下一帧。
//
// ## ⚠️ **那个 init 闭包不捕获任何东西**
//
// 而「起始时刻」并不需要靠 init 带进来——下面那句「零点还没定过」就能定它，
// 于是这个闭包是**静态**的（编译成一个常量符号），每帧零分配。
//
// ⚠️ **「捕获了变量」本身不是分配的原因**（这一条实测出来的，别再写回去）：
// 决定分配的是**闭包有没有逃出去**。`ui.Local` 的 `init` 参数**只被调用一次**
// （`mygo/ui/context.go` 的 `coreLocal`：`v := new(T); *v = init()`），于是
// 编译器可以把它**内联掉**——逃逸分析实测：`wavePhase.func1` 是
// 「can inline … with cost 2」，而**捕获了变量**的同一形状的闭包，分配数与
// 不捕获的**一模一样**（都是 0 次）。
//
// 真正让那一票之前每帧付钱的是**键**（`key+":start"` 那个字符串）：它既要在
// 运行期**拼出来**，又要**装箱进 `any`**（`ui.Local` 的键是 `any`），两次
// 分配。闭包只是顺带被算在了头上。
//
// ## ⚠️ **`ui.Local` 现在是**建的时候**问的，状态是**画的时候**改的
//
// 查表（`ui.Local`）必须在建的时候做——它是「这个元素跨帧的那一份」。
// 而相位是钟算的，所以它在画的时候算。两者分开之后，`st` 这个指针是从建
// 的那一刻起就**指着同一份**的（`ui.Local` 按 key 认元素，元素在，那一份就在）。
func wavePhase(p *ui.Painter, st *waveState, visible bool) float32 {
	now := p.Now()
	if !st.started {
		// 第一次看见这一格：相位的零点就是此刻。
		//
		// ⚠️ **判据是 `started` 而不是「seen 是不是零值」**：无头 Tester 的钟
		// 起点可能正好是零时刻（`time.Time{}`），那时「seen 是零值」就分不出
		// 「第一次」和「钟没走过」。
		st.started, st.start = true, now
	}
	if !now.Equal(st.seen) {
		st.seen = now
		// 看不见的窗口不要再要帧（票 #156）：它每帧都请求下一帧，而
		// `App.tick` 又在放歌时每 50ms 叫醒每个窗口一次——两条链合起来，
		// 一扇收在后台的窗口会一直按帧重画，用户能感到的是风扇和电池。
		//
		// ⚠️ **这里要的是「局部重绘」那一帧**（`p.AnimationFrame()`），不是
		// 「整棵元素树重建一遍」（`c.AnimationFrame()`）：波形是画出来的，而
		// 重建那一帧要把整窗重排一遍——放歌时那是每秒 60 次（票 #392）。
		if visible {
			p.AnimationFrame()
		}
	}
	return float32(now.Sub(st.start)%wavePeriod) / float32(wavePeriod)
}

// waveHeight 是某一根竖条此刻多高。
func waveHeight(phase float32) float32 {
	// 一个正弦：phase 走 0~1 是一轮，条子从最短走到最长再走回来。
	s := math.Sin(2 * math.Pi * float64(phase))
	return waveMin + (waveMax-waveMin)*float32((s+1)/2)
}
