// 弹幕这一摊的**模型**：那些数、那条轨道规则、还有跨帧记着的那点状态。
//
// # 数值都来自研究票 #538 的报告 §3.3
//
// 上游 Biu 没有弹幕可抄 ⇒ **参考就是那份报告**，不是上游：
//
//	区域      满屏左列的封面那一块（360×270）
//	轨道      8 条，每条 26 DIP（字 18 + 缝 8）
//	字号      18 DIP
//	颜色      保留 B 站的十进制 RGB，统一乘 alpha 0.85
//	速度      6 秒横穿那一块（360/6 = 60 px/s）
//	同屏      最多 12 条
//	密度      最多 8 行字（= 8 条轨道）
//	淡出      不淡出，直接出界
//
// ⚠️ **这些数的正本在 `DESIGN.md`**（零节那条「满屏封面上的弹幕」）——这里只是
// 把它写成了代码里的常量，两边要一起改。
//
// # 时间轴 1:1 对齐
//
// 弹幕的时间是**视频时间**，我们放的就是这个视频的音轨 ⇒ `app.State.Position`
// 就是弹幕的时间轴，**没有额外 offset**。
//
// ⇒ 「从第 3 分钟开始听」就从第 3 分钟那一条开始滚（**按当前位置滚**，不是按
// 「播放开始后的第几分钟」滚）——应用自己的「恢复上次听到哪儿」也一样（那时
// 位置就是一个非零数）。
//
// # 画在哪儿、怎么动
//
// 那两件事在 `fullplayer_danmaku.go`（画的地方和那层浮层），这一份只管
// 「哪几条该在屏幕上、各占哪条轨道」。
//
// # 位移有自己的时钟（票 #669）
//
// 整窗的唤醒被降到约 1Hz（`internal/app/tick_wake.go` 的 `readoutMoved`），
// 而弹幕要按显示器刷新率走 ⇒ 位置不能再只取「建树那一刻的 `State.Position`」。
//
// 这一份因此记一个**锚点**：建树那一刻把 `State.Position` 钉在
// `baseMS` / `baseAt` 上，绘制帧用 `Painter.Now()` 从锚点按 `rate` 往外推
// （`posAt`）。两次重建之间位置因此是**连续**的，整窗重建多快都无所谓。
//
// 锚点只在三种情况下重钉（`follow`）：
//
//	换歌        调用方 `reset` 之后重新钉
//	跳转        这一帧的读数和推出来的位置差过 `danmakuSeekSlop`
//	暂停/恢复   播放状态变了（冻结在**画到的那一格**，不回到读数上）
//
// 几十毫秒的采样误差**不重钉**：重钉就是跳一下，而那个误差是有界的
// （播放器 50ms 一拍）。
//
// # 增量：只有新进入窗口的条目才分配轨道（票 #670）
//
// #647 交付了按 `TimeMS` 排序的索引（`itemsByTime`）和窗口二分；改之前绘制帧
// 仍然每帧把窗口里那一段**重新过一遍**：每一条都查一次字宽缓存、可见列表
// 每帧重排一次、已经在 `slots` 里的条目每帧再查一次 map。密集弹幕（上千条）
// 下这些工作随「窗口里有多少条」线性增长，而窗口起点是按**最长那条弹幕**
// 估出来的上界——一条长弹幕就能把窗口拉出好几分钟。
//
// ⇒ 这一份现在记一个**只前进的游标** `cursor`：
//
//	第一次跨过当前位置的条目   进一次窗口：先看有没有轨道放得下，放得下才
//	                          量字宽、分配轨道、加入 `active`；放不下就丢掉
//	                          （不排队，也不为它量字宽）
//	已经在 active 里的条目     每帧只按自己的年龄算位置，不再查索引、不再
//	                          量字宽、不再写 map
//	整个出界的条目             从 active / slots / widths 里一起收掉
//
// 换歌（`count` / `first` 变了）、大幅回退、字号变化、封面宽度变化时，
// 游标和活动集合整体复位（见 `resetTracks`）——那些状态下旧的字宽、轨道
// 门槛和活动条目都不再成立。
//
// 时间索引和窗口二分仍然是 #647 交付的那一份：`rewind` 用 `sort.Search` 把
// 游标放到窗口起点（早已出界的条目直接跳过），改的只是**不再每帧都做一遍**。
package ui

import (
	"cmp"
	"slices"
	"sort"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

const (
	// danmakuTracks 是轨道数（报告 §3.3）：8 条。
	//
	// ⚠️ **8 × 26 = 208 ≤ 270**（封面那一块的高）——所以八条轨道摆得下。
	danmakuTracks = 8

	// danmakuTrackHeight 是轨道高（报告 §3.3）：26 DIP = 字 18 + 缝 8。
	//
	// ⚠️ **系统字号调大的时候这一档跟着长**：轨道高至少是「字高 + 缝」，
	// 否则两行字会压在一起（字 18 的时候正好是 26）。
	danmakuTrackHeight float32 = 26

	// danmakuTrackGap 是字底下那道缝（报告 §3.3）：8 DIP。
	danmakuTrackGap float32 = 8

	// danmakuFontSize 是字号（报告 §3.3）：18 DIP。
	//
	// ⚠️ **走 `scale.Font`**：系统文本大小要跟着变（票 #139 的规矩）。
	danmakuFontSize = scale.Size18

	// danmakuAlpha 是统一乘上的透明度（报告 §3.3）：0.85。
	//
	// 理由是 B 站自己的做法：**弹幕挡脸**是老毛病，半透明是它的默认。
	danmakuAlpha = 0.85

	// danmakuSpeed 是横穿的速度（DIP/秒，报告 §3.3）：360 / 6 = 60。
	//
	// 比 B 站默认慢，音乐场景下更好读。
	danmakuSpeed = 60

	// danmakuMaxOnScreen 是同屏上限（报告 §3.3）：12 条。
	//
	// ⚠️ **它同时是帧预算的根**：满屏那一档每帧的分配是「基线 + N 条弹幕」
	// （N=12 ⇒ 43 次，见 `framebudget_test.go` 那一档）。上不封顶的话六十条
	// 就是基线 + 60，直接把那一档的预算顶穿两倍。
	danmakuMaxOnScreen = 12

	// danmakuTrackSpacingRatio 是「前一条走出这么宽才放下一条」（报告 §3.3）。
	//
	// ⚠️ **按封面的宽算**（不是按字宽）：30% × 360 = 108 DIP ⇒ 1.8 秒。
	// 弹幕稀的时候稀疏、密的时候自动变稀，不会堆成一坨。
	danmakuTrackSpacingRatio = 0.3
)

const (
	// danmakuSeekSlop 是「这一下是跳转，不是采样误差」的门槛。
	//
	// # 为什么需要它
	//
	// 绘制帧按 `Painter.Now()` 自己往前推位置，而整窗重建时拿到的
	// `State.Position` 是播放器 50ms 一拍采回来的。两者差几十毫秒是**正常**的，
	// 拿那个差去重钉锚点就是让弹幕在每次整窗重建时抖一下。
	//
	// ⇒ 超过这个门槛的差才算「用户拖了进度条 / 跳转」：那时必须重钉，而且
	// 往回跳还要把轨道整份复位（见 `follow`）。
	//
	// ⚠️ **不能太小**：正常播放时两次相邻的整窗重建差 1~2 秒（整窗约 1Hz），
	// 门槛太小的话每一次重建都被当成跳转 ⇒ 轨道整份复位 ⇒ 弹幕每次重建都换
	// 轨道（抖）。
	danmakuSeekSlop = 2 * time.Second
)

// danmakuPosAt 报「锚点之后过了 d」时弹幕该在的位置（毫秒）。
//
// rate 是那一份弹幕的时间轴每秒走多少（正常是 1；倍速播放时跟着变）。
func danmakuPosAt(baseMS int64, rate float64, d time.Duration) int64 {
	return baseMS + int64(float64(d.Milliseconds())*rate)
}

// danmakuRate 把 `State.Rate` 归到「一个合理的倍速」上。
//
// ⚠️ **零值和负数不是「停在原地」**：手摆的状态（测试、还没装播放器的那几帧）
// 里 `Rate` 是 0，直接乘出来位置就永远不动——弹幕停在右边沿上不动，而那看
// 上去像「它坏了」。所以 0 和负数都按 1 处理。
func danmakuRate(rate float64) float64 {
	if rate <= 0 {
		return 1
	}
	return rate
}

// danmakuLane 是一条轨道上**上一条**弹幕的那些数。
//
// ⚠️ **为什么记的是「上一条」而不是「轨道上还空着没有」**：下一条能不能进
// 这条轨道，取决于**上一条的尾巴走到哪儿了**——同一条轨道上两条弹幕**不能
// 叠字**（叠在一起就没法读了）。
type danmakuLane struct {
	// used 是这条轨道用过没有（空轨道什么都不挡）。
	used bool
	// timeMS 是上一条入场的视频时间（毫秒）。
	timeMS int64
	// width 是上一条的字宽（DIP）——**它要走出多远，下一条才进得来**。
	width float32
}

// danmakuSlot 是一条正在滚的弹幕占的那条轨道。
type danmakuSlot struct {
	// lane 是它占的轨道号。**-1 = 被丢掉了**（没有轨道可放，见
	// `danmakuLayer.assign`）。
	lane int
	// timeMS 是它自己的视频时间（毫秒）——用来算它滚了多远。
	timeMS int64
	// maxAgeMS 是它最多在屏幕上待多久（按字宽算出来的）。
	maxAgeMS int64
}

// danmakuActive 是活动集合里的一条：已经进了窗口、占着轨道的那条弹幕。
//
// ⚠️ **它是「已入场」的唯一事实**（票 #670）：绘制帧只遍历这一份，不再回
// 时间索引里找「谁该出现」——那是 `cursor` 在一帧里做一次的事。
type danmakuActive struct {
	// item 是这条弹幕自己（正文、颜色、rowid 都在里面）。
	item app.DanmakuItem
	// slot 是它占的那条轨道（`lane` 一定 >= 0：被丢掉的不进活动集合）。
	slot danmakuSlot
}

// danmakuLayer 记着弹幕这一摊跨帧的状态。
//
// ⚠️ **为什么要有跨帧的状态**：轨道是**入场那一刻**定的（B 站那样），不是
// 每帧重算的——每帧重算的话，一条弹幕会在屏幕上「换轨道」，或者被挤掉之后又
// 半路冒出来。
type danmakuLayer struct {
	// lanes 记着每条轨道上**上一条**弹幕的那些数（入场时间 + 字宽）。
	//
	// ⚠️ **下一条能不能进这条轨道，判据是两条**：
	//
	//	一、时间（报告 §3.3）：前一条走出 30% 的宽度（360 × 30% ÷ 60 = 1.8 秒）。
	//	二、几何：**前一条的尾巴必须已经过了右边沿那条入场线**——
	//	   两条同速从右边沿进来，后一条的左边沿永远比前一条的右边沿靠右
	//	   60 × Δt 像素 ⇒ 不叠字当且仅当 Δt × 60 ≥ 前一条的字宽。
	//
	// ⚠️ **第二条是评审提出来的**（critical：同轨叠字）：报告 §3.3 那条
	// 「走出 30%」只当**时间门槛**用——它对**长字不够**：一条 196 像素宽的
	// 字滚了 1.8 秒（走了 108 像素）之后它的尾巴还在封面里，而下一条已经
	// 从右边沿进来了 ⇒ **重叠 88 像素**，两句话叠在一起没法读。
	//
	// ⚠️ **两条都过才进**（取大的那个）：时间门槛管「稀疏度」，几何门槛管
	// 「可读性」——后者优先。
	lanes [danmakuTracks]danmakuLane

	// slots 记着正在滚的那些弹幕：弹幕的 rowid → 它占的那条轨道。
	//
	// ⚠️ **键是弹幕自己的 rowid**（稳定且唯一），不是下标——下标会随着
	// 「前面插了一条」整体错配（铁律四第 4 条）。
	//
	// ⚠️ **被丢掉的不进这张表**（票 #670）：没有槽位就没有活动状态，下一帧
	// 也不该再为它做任何事。
	slots map[string]danmakuSlot

	// widths 记着每条弹幕的字宽（量一次就够）。
	//
	// ⚠️ **量字宽要排一次版**（`c.MeasureText`），而每帧量一次就是每条
	// 弹幕一次分配 ⇒ 记下来。
	widths map[string]float32

	// live 是现在占着轨道的有几条（同屏上限 12 就是它）。
	live int

	// count/first 是这一份弹幕的「身份」——变了就是换歌了（要重置）。
	count int
	first string

	// itemsByTime 是当前弹幕批次按 TimeMS 排序的索引（#647 交付的那一份）。
	// 绘制帧只在这个索引上做时间窗口二分，不再扫描和排序完整列表。
	itemsByTime  []app.DanmakuItem
	maxTextBytes int

	// cursor 是时间索引上「还没被游标吞掉」的第一个位置（票 #670）。
	//
	// ⚠️ **它只前进**（大幅回退除外）：每帧从它往后扫到「时间 > 当前位置」
	// 为止，跨过去的条目**只处理一次**——分轨道、量字宽、加入 `active`。
	// 之后每帧只遍历活动集合，不再碰索引，也不再查字宽。
	cursor int

	// active 是已经入场、还在屏幕上的那些条目（按入场时间排序）。
	//
	// ⚠️ **它是复用的缓冲**：收掉出界的那几条走原地压缩，不重新分配。
	active []danmakuActive

	// lastPosMS 是上一次推进窗口时的位置，只用来认「这一帧往回走了」。
	//
	// ⚠️ **往回走要把游标倒回去**（小范围抖动 / 采样误差）：不倒的话，
	// 刚被吞掉的那几条会被当成「已经处理过」，而它们的年龄变小之后本该
	// 重新出现在画面上。
	lastPosMS int64
	// posValid 是「lastPosMS 已经是这一份弹幕上的一个位置」（复位 / 换歌 /
	// 跳转之后为假，第一帧要走一次二分）。
	posValid bool

	// coverW 是这一份活动状态按哪个封面宽算的（变了要整份复位）。
	coverW float32

	// baseMS 是锚点那一刻的视频位置（毫秒），baseAt 是那一刻的**钟**
	// （建树时 `Context.Now()` 的读数）。绘制帧用
	// `danmakuPosAt(baseMS, rate, p.Now().Sub(baseAt))` 往外推。
	//
	// ⚠️ **它们是两个不同的钟**：baseMS 是播放器的位置（50ms 采一次），
	// baseAt 是界面的墙钟。两者只在锚点这一刻对齐，之后各走各的——这正是
	// 「弹幕自己按 60fps 往前走」的意思。
	baseMS int64
	baseAt time.Time
	// rate 是这一份弹幕的时间轴每秒走多少（倍速播放时跟着变）。
	rate float64
	// playing 是锚点之后弹幕动不动。
	//
	// ⚠️ **暂停时它不许动，即使墙钟还在走**：暂停的那一刻把锚点重钉到
	// 「画到的那一格」上（不是播放器的读数），于是恢复之后从原地接着走。
	playing bool

	// lastBaseMS 是上一次**建树**时读到（或推出来）的位置，只用来认跳转。
	//
	// ⚠️ **判据是「读数 vs 自己推出来的位置」**，不是「和上一次读数比」：
	// 正常播放时上一次读数可能是 1 秒前（整窗约 1Hz），拿它比会把每一次
	// 重建都当成跳转。
	lastBaseMS int64

	// ready 是「这一帧有一份弹幕可画」。盒子、绘制回调都看它。
	ready bool
	// animate 是「播放中，而且用户没要求减少动态效果」：只有它为真时才逐帧
	// 重绘。为假时位置只在整窗重建那一帧更新（`posAt` 返回锚点本身）。
	animate bool
	// fontSize / trackH 是这一份弹幕用的字号与轨道高（建树时按系统字号和
	// 封面高度算好，绘制帧直接用）。
	fontSize float32
	trackH   float32

	// paints / requests 是**给测试数**的两个计数（照 `builds.go` 那条规矩：
	// 「谁画的谁才数得出来」，所以它住在生产代码里）。
	//
	//	paints    这一层画了几帧
	//	requests  这一层要了几次下一帧
	//
	// ⚠️ **不能拿 `Tester.FramePending()` 判「弹幕要不要帧」**：满屏开着的时候
	// 进度条那根线（`viewFullProgress` 的 `live`）在播放中也一直要帧，两者从
	// 「有没有帧」上看不出区别。
	paints, requests int

	// entered / assigned / measures 是增量窗口的**可测计数**（票 #670），
	// 全是普通整数，不参与绘制、不做任何额外分配：
	//
	//	entered   过粗筛之后「进了窗口」的条目数（含没有轨道被丢掉的；
	//	          回退重扫时同一条会被再数一次——那正是回退的成本）
	//	assigned  其中真的拿到轨道的条目数（= 活动集合一共收过多少条）
	//	measures  真的量过字宽的次数（每条拿到轨道的只量一次）
	//
	// ⚠️ **它们钉住的是「分配次数只和新进入的条数相关」**：密集弹幕下
	// `assigned` 不该随总条数线性增长，稳态帧里 `measures` 一次都不该涨。
	entered, assigned, measures int
}

// reset 把这一摊清空（换歌、或者压根没有弹幕）。
func (d *danmakuLayer) reset() {
	d.resetTracks()
	d.count = 0
	d.first = ""
	d.itemsByTime = nil
	d.maxTextBytes = 0
	d.baseMS, d.lastBaseMS = 0, 0
	d.baseAt = time.Time{}
	d.rate = 0
	d.playing = false
	d.ready = false
	d.animate = false
	d.fontSize, d.trackH = 0, 0
	d.entered, d.assigned, d.measures = 0, 0, 0
}

// resetTracks 把轨道、字宽、游标和活动集合整份复位，**保留**这一份弹幕的
// 身份（`count` / `first`）、时间索引和时钟锚点。
//
// 它用在四种「旧状态不再成立」的时候（票 #670）：换歌（走 `reset`）、大幅
// 回退、字号变化、封面宽度变化。之后游标从时间索引的窗口起点重新开始，
// 新位置上的条目按空轨道重新分配。
func (d *danmakuLayer) resetTracks() {
	d.lanes = [danmakuTracks]danmakuLane{}
	d.slots = nil
	d.widths = nil
	d.live = 0
	d.cursor = 0
	// ⚠️ **active 只截断、不置 nil**：它是复用的缓冲（复位之后照样复用它）。
	d.active = d.active[:0]
	d.lastPosMS = 0
	d.posValid = false
	d.coverW = 0
}

// posAt 报「钟走到 now」时弹幕该在的位置（毫秒）。
//
// ⚠️ **暂停（或不做连续动画）时它给的就是锚点那一格**：那时位置只在整窗
// 重建那一帧更新（见 `follow`）。
func (d *danmakuLayer) posAt(now time.Time) int64 {
	if !d.playing || !d.animate || d.baseAt.IsZero() {
		return d.baseMS
	}
	return danmakuPosAt(d.baseMS, d.rate, now.Sub(d.baseAt))
}

// follow 让锚点跟着这一帧的读数走（建树时调）。
//
// pos 是这一帧读到的 `State.Position`，now 是这一帧的钟，playing 是
// 「这一帧在不在放」，rate 是倍速，animate 是「要不要走连续动画」。
//
// # 什么时候重钉
//
//	还没锚过 / 播放状态变了 / 动画开关变了   钉到读数上
//	不做连续动画（暂停、减少动态效果）      读数变了就钉
//	在做连续动画、读数比推出来的差过阈值    钉（跳转；阈值见 danmakuSeekSlop）
//	其余                                    不动
//
// ⚠️ **最后那条「其余」是关键**：在放时读数和推出来的位置差几十毫秒是采样
// 误差，重钉就是让弹幕在每次整窗重建时抖一下。
func (d *danmakuLayer) follow(pos int64, now time.Time, playing, animate bool, rate float64) {
	if d.baseAt.IsZero() {
		d.pin(pos, now, playing, animate, rate)
		return
	}
	if d.playing != playing || d.animate != animate || d.rate != danmakuRate(rate) {
		// 播放状态变了（暂停 / 恢复 / 减动效开关翻面）、或者用户改了倍速。
		//
		// ⚠️ **暂停冻在「画到的那一格」上**，不是播放器的读数：读数比画出来
		// 的晚几十毫秒（50ms 一拍），拿它冻会让弹幕在按下暂停时往回跳一两个
		// 像素。恢复则用读数——音频那边的事实，差的那一点正好补上。
		if playing {
			d.pin(pos, now, playing, animate, rate)
		} else {
			d.freeze(d.posAt(now), now, pos)
		}
		return
	}
	if !playing || !animate {
		// 同一个状态里读数变了 ⇒ 用户拖了进度条（暂停时播放器自己不动；
		// 减动效时位置只在整窗重建这一帧更新）。
		if pos != d.lastBaseMS {
			d.pin(pos, now, playing, animate, rate)
		}
		return
	}
	if drift := pos - d.posAt(now); drift > danmakuSeekSlop.Milliseconds() || -drift > danmakuSeekSlop.Milliseconds() {
		d.pin(pos, now, playing, animate, rate)
		return
	}
	d.lastBaseMS = pos
}

// pin 把锚点钉在 pos / now 上。
func (d *danmakuLayer) pin(pos int64, now time.Time, playing, animate bool, rate float64) {
	d.baseMS, d.baseAt = pos, now
	d.lastBaseMS = pos
	d.playing, d.animate = playing, animate
	d.rate = danmakuRate(rate)
}

// freeze 把锚点冻在 painted 那一格上（暂停），而 lastBaseMS 记着播放器的读数
// ——用它认「暂停期间用户拖了进度条」。
func (d *danmakuLayer) freeze(painted int64, now time.Time, sampled int64) {
	d.baseMS, d.baseAt = painted, now
	d.lastBaseMS = sampled
	d.playing, d.animate = false, false
}

// danmakuSortItemsByTime 建立当前批次的稳定时间索引。
func danmakuSortItemsByTime(items []app.DanmakuItem) {
	slices.SortStableFunc(items, func(a, b app.DanmakuItem) int {
		return cmp.Compare(a.TimeMS, b.TimeMS)
	})
}

// pickLane 给一条**新进入窗口**的弹幕找第一条放得下的轨道（-1 = 放不下）。
//
// # 规则
//
// 每条弹幕找第一条**放得下**的轨道（见 `danmakuLayer.lanes` 那一段的两条
// 判据：时间门槛 + 几何门槛）；八条轨道都放不下、或者同屏已经到上限，就
// **丢掉这一条**（不排队）。
//
// ⚠️ **它不量字宽**（票 #670）：放不下的条目连一次 `MeasureText` 都不该付
// ——密集弹幕里被丢掉的是绝大多数，而那正是「首次打开」最贵的那一下。
//
// ⚠️ **轨道是入场那一刻定的**（B 站那样），不是每帧重算的——每帧重算的话，
// 一条弹幕会在屏幕上换轨道，或者被挤掉之后又半路冒出来。
func (d *danmakuLayer) pickLane(timeMS, spacingMS int64) int {
	if d.live >= danmakuMaxOnScreen {
		return -1
	}
	for t := range danmakuTracks {
		lane := d.lanes[t]
		if !lane.used {
			// 空轨道 ⇒ 什么都不挡。
			return t
		}
		// 门槛一（报告 §3.3）：前一条走出 30% 的宽度。
		need := spacingMS
		// 门槛二（几何）：前一条的**尾巴**必须已经过了右边沿那条入场线，
		// 否则两条的字会叠在一起（评审提的 critical）。
		if geom := int64(float64(lane.width) * 1000 / danmakuSpeed); geom > need {
			need = geom
		}
		if timeMS-lane.timeMS < need {
			continue
		}
		return t
	}
	return -1
}

// commit 把一条拿到轨道的弹幕记进轨道状态和槽位，返回它的槽位。
//
// ⚠️ **`lane >= 0` 才调它**：被丢掉的不进槽位（票 #670）——没有槽位就没有
// 活动状态，下一帧也不该再为它做任何事。
func (d *danmakuLayer) commit(lane int, it app.DanmakuItem, width float32, coverW float32) danmakuSlot {
	slot := danmakuSlot{
		lane:   lane,
		timeMS: it.TimeMS,
		// 一条字走到**整个出界**（右沿也出去了）要多久：(封面宽 + 字宽) / 速度。
		maxAgeMS: int64((float64(coverW) + float64(width)) * 1000 / danmakuSpeed),
	}
	d.lanes[lane] = danmakuLane{
		used:   true,
		timeMS: it.TimeMS,
		width:  width,
	}
	if d.slots == nil {
		d.slots = map[string]danmakuSlot{}
	}
	d.slots[it.ID] = slot
	// ⚠️ **`live` 是活动集合条数的镜像**：`expire` 收掉出界的那些时重设，
	// 这里每收一条加一——两个地方各加各减容易漏，所以就这两个口子。
	d.live++
	return slot
}

// assign 给一条弹幕找轨道并记账（挑轨道 + 写入槽位两步的合并入口）。
//
// ⚠️ **绘制路径不走它**（`advance` 先挑轨道、量完字宽才 `commit`，为的是
// 「放不下的不量字宽」）；它留给只验轨道规则的测试用。
func (d *danmakuLayer) assign(it app.DanmakuItem, width float32, spacingMS int64, coverW float32) danmakuSlot {
	lane := d.pickLane(it.TimeMS, spacingMS)
	if lane < 0 {
		return danmakuSlot{lane: -1, timeMS: it.TimeMS}
	}
	return d.commit(lane, it, width, coverW)
}

// danmakuAgeBoundMS 报「一条弹幕早就该出界了」的时间上界（毫秒）。
//
// # 它是**粗筛**，不是判据
//
// 真正的判据是量出来的字宽（(封面宽 + 字宽) ÷ 速度）——而量字宽要排一次版，
// 所以先用这个上界把早该出界的那些滤掉，别白排一次版。
//
// # 为什么用**字节数**而不是字数
//
// 一个字节最宽一个字宽（汉字是**三个**字节一个字宽）⇒ 「字节数 × 字号」
// 一定 ≥ 真字宽 ⇒ 这个上界**不会**把还在画面上的那条滤掉。
//
// ⚠️ **滤掉了就是「提前消失」**：本票评审提过一个 major——40 字的长弹幕在
// 第 17 秒整条不见了（一个 15 秒的硬上限在按字宽判断出界之前先把它滤掉了，
// 而按 60 px/s 它的右缘还在封面里）。所以这一档**只做上界，不做上限**。
//
// ⚠️ **汉字那三倍是白给的**：粗筛窗口比真窗口宽三倍 ⇒ 量字宽的次数多一些
// （每条弹幕拿到轨道时量一次，量完记在 `d.widths` 里）。
func danmakuAgeBoundMS(text string, fontSize, coverW float32) int64 {
	wBound := float64(len(text)) * float64(fontSize)
	return int64((float64(coverW) + wBound) * 1000 / danmakuSpeed)
}

// windowStart 是「从当前位置往前推多远的条目还可能挂在屏幕上」的时间点。
//
// 它用的是**最长那条**弹幕的字节数上界（`maxTextBytes`）——所以窗口一定
// 盖住所有还在画面上的条目（`danmakuAgeBoundMS` 只粗筛、不砍）。
func (d *danmakuLayer) windowStart(posMS int64, coverW float32) int64 {
	bound := float64(coverW) + float64(d.maxTextBytes)*float64(d.fontSize)
	return posMS - int64(bound*1000/danmakuSpeed)
}

// rewind 把游标挪到 `posMS` 那一格的窗口起点（二分，不扫描）。
//
// ⚠️ **只在「第一次」和「跳转 / 回退」时调**（票 #670）：普通播放帧从游标
// 往后走，一次 `sort.Search` 都不做——窗口二分仍然是 #647 那份索引上的，
// 只是不再每帧都二分一遍。
func (d *danmakuLayer) rewind(posMS int64, coverW float32) {
	startMS := d.windowStart(posMS, coverW)
	d.cursor = sort.Search(len(d.itemsByTime), func(i int) bool {
		return d.itemsByTime[i].TimeMS >= startMS
	})
}

// expire 收掉已经整个出界的那些（它们占的轨道要让给后来的）。
//
// ⚠️ **活动集合是原地压缩的**（票 #670）：不新建切片、不重新排序。同屏
// 上限 12 条 ⇒ 这一步每帧最多过 12 个元素。
//
// ⚠️ **字宽跟着槽位一起收**：`widths` 与 `slots` 是同一份活动集合的两个
// 视图，只留**在屏**那几条的字宽——不然一首长歌放下来它会一直涨。回退时
// 重新扫到同一条会再量一次，那是回退自己的成本，正常播放不付。
func (d *danmakuLayer) expire(posMS int64) {
	kept := d.active[:0]
	for _, a := range d.active {
		if posMS-a.slot.timeMS > a.slot.maxAgeMS {
			delete(d.slots, a.item.ID)
			delete(d.widths, a.item.ID)
			continue
		}
		kept = append(kept, a)
	}
	d.active = kept
	d.live = len(kept)
}

// widthOf 报一条弹幕的字宽（DIP）。**量一次就够**。
//
// ⚠️ **权衡器是 `Painter` 而不是 `Context`**：量字宽现在发生在**绘制帧**里
// （那一帧只有 `p`），而 `Painter.MeasureText` 和 `Context.MeasureText` 落到
// 同一个排版缓存上（`richParams`）——同一个字号量出来的数一样。
//
// ⚠️ **只有拿到轨道的条目才调它**（票 #670）：`measures` 数的是真的排了几次版。
func (d *danmakuLayer) widthOf(p *ui.Painter, id, text string, fontSize float32) float32 {
	if w, ok := d.widths[id]; ok {
		return w
	}
	w, _ := p.MeasureText(0, ui.Span{Text: text, Size: fontSize})
	if d.widths == nil {
		d.widths = map[string]float32{}
	}
	d.widths[id] = w
	d.measures++
	return w
}

// advance 把时间窗口推到 `posMS`，并把**新进入窗口**的条目录入活动集合。
//
// # 每帧做什么
//
//	要跳就二分一次游标（第一次 / 跳转 / 回退）
//	收掉出界的（原地压缩活动集合）
//	从游标往后，把「时间已经不晚于 posMS」的条目**过一遍就不再回头**：
//	    没有 rowid / 早就出界        跳过（不量字宽）
//	    已经在活动集合里            跳过（回退时才会碰到）
//	    有轨道放得下                量一次字宽、分配、加入活动集合
//	    没有轨道                   丢掉（不量字宽、不进活动集合）
//
// ⚠️ **工作量和「新进入几条」成正比**，不跟「这份弹幕一共几条」成正比：
// 稳态帧里游标不动，只有 `expire` 过一遍那顶多 12 条活动条目。
func (d *danmakuLayer) advance(p *ui.Painter, posMS int64, coverW float32, spacingMS int64) {
	// ⚠️ **往回走就把游标倒回去**（票 #670）：往回走一点（暂停恢复时锚点重钉、
	// 倍速切换的采样误差）时，刚刚收掉的那几条可能又该在屏幕上了——不倒游标
	// 它们永远不会再入场。这不是普通播放帧的成本：在放时 `posAt` 单调，只有
	// 「跳转 / 暂停 / 恢复 / 换倍速」那几下会往回走。
	if !d.posValid || posMS < d.lastPosMS ||
		posMS-d.lastPosMS > danmakuSeekSlop.Milliseconds() {
		d.rewind(posMS, coverW)
	}
	d.expire(posMS)
	for d.cursor < len(d.itemsByTime) {
		it := d.itemsByTime[d.cursor]
		if it.TimeMS > posMS {
			break
		}
		d.cursor++
		if it.ID == "" {
			// 没有 rowid ⇒ 认不出它 ⇒ 不画。
			//
			// ⚠️ **不是「兜底给它一个 key」**：槽位是按 rowid 记的，没有身份
			// 就没法稳定（生产上 rowid 一定有，见 `internal/service/bili` 的
			// `Danmaku.ID`）。
			continue
		}
		if _, ok := d.slots[it.ID]; ok {
			// 回退时重新扫到的条目——它已经在活动集合里，状态直接复用。
			continue
		}
		if posMS-it.TimeMS > danmakuAgeBoundMS(it.Text, d.fontSize, coverW) {
			// 粗筛上界：早就出界的那些不排版（量字宽要排一次版）。
			continue
		}
		d.entered++
		lane := d.pickLane(it.TimeMS, spacingMS)
		if lane < 0 {
			// 没有轨道可放 ⇒ **丢掉这一条**（报告 §3.3：不排队）。
			continue
		}
		width := d.widthOf(p, it.ID, it.Text, d.fontSize)
		if posMS-it.TimeMS > int64((float64(coverW)+float64(width))*1000/danmakuSpeed) {
			// 精确判据（照旧是「(封面宽 + 字宽) ÷ 速度」）：回退重扫时才会
			// 走到这里——那条早就整个出界了，不该再挂回轨道上。刚量的那一格
			// 也一起收掉，让 `widths` 与 `slots` 保持一一对应。
			delete(d.widths, it.ID)
			continue
		}
		slot := d.commit(lane, it, width, coverW)
		d.active = append(d.active, danmakuActive{item: it, slot: slot})
		d.assigned++
	}
	d.lastPosMS = posMS
	d.posValid = true
}

// sync 是**建树那一帧**要做的三件事：认这份弹幕的身份、把字号与轨道高定下来、
// 把时钟锚点对上这一帧的位置。
//
// ⚠️ **它只调一次（建树那一遍）**：绘制帧拿到的 `Painter` 里没有 `Context`，
// 而这三件事都需要 `Context`（设置、系统字号、`Now`）。
func (d *danmakuLayer) sync(c *ui.Context, st app.State, items []app.DanmakuItem, area ui.Rect) {
	if len(items) == 0 {
		// 没有弹幕（或者还在问）⇒ 这一帧没有可画的。
		d.reset()
		return
	}
	if d.count != len(items) || d.first != items[0].ID {
		// **换了一份**（换歌了）⇒ 上一份的轨道、字宽、锚点全部作废。
		//
		// ⚠️ **判据是「条数 + 第一条的 rowid」：这一份弹幕的顺序是 B 站给的
		// 顺序（不变），所以这两样变了就是换了一份。
		d.reset()
		d.count, d.first = len(items), items[0].ID
		d.itemsByTime = append(d.itemsByTime[:0], items...)
		d.maxTextBytes = 0
		for _, it := range d.itemsByTime {
			if n := len(it.Text); n > d.maxTextBytes {
				d.maxTextBytes = n
			}
		}
		danmakuSortItemsByTime(d.itemsByTime)
	}

	fontSize := scale.Font(c, danmakuFontSize)
	trackH := danmakuTrackHeight
	if h := fontSize + danmakuTrackGap; h > trackH {
		// 系统字号调大了 ⇒ 轨道跟着长（否则两行字压在一起）。
		trackH = h
	}
	if maxH := area.H / float32(danmakuTracks); trackH > maxH {
		// 而轨道总数是死的（8 条）⇒ 太高的话只能压回去。
		trackH = maxH
	}
	// ⚠️ **字号变了要把轨道整份复位**（票 #670）：字宽是按字号量的，旧的
	// 那一份宽度、轨道门槛和「整个出界」的时长全部不再成立。
	if d.ready && d.fontSize != fontSize {
		d.resetTracks()
	}
	// ⚠️ **封面宽度变了也要复位**：轨道门槛（30% 封面宽）和出界时长
	// （封面宽 + 字宽）都按它算，而窗口粗筛的上界也用它。
	if d.coverW != 0 && d.coverW != area.W {
		d.resetTracks()
	}
	d.fontSize, d.trackH = fontSize, trackH
	d.coverW = area.W
	d.ready = true

	posMS := st.Position.Milliseconds()
	if d.baseAt.IsZero() {
		// 第一帧（或者刚 reset）⇒ 锚点直接落在这儿。
		d.pin(posMS, c.Now(), st.Playing, st.Playing && !c.Preferences().ReduceMotion, st.Rate)
		return
	}
	// ⚠️ **回退要把轨道整份复位**：轨道上记着的是「上一条入场时刻」，回退
	// 之后那些时刻还在**将来**，于是新位置上的弹幕一条都放不进去（屏幕上
	// 会一条弹幕都没有）。
	//
	// 判据是「这一帧的读数比**画到的**位置还早」——那是用户把进度条拖回去了，
	// 不是采样误差（差值是整段整段的）。
	//
	// ⚠️ **比的是 `posAt` 不是 `baseMS`**：在放时锚点是**上一次建树**那一刻
	// （可能一秒前），拿它比的话「从 60 秒拖回 30 秒」会被漏掉（30 > anchor-2s），
	// 而轨道上记着的是 60 秒附近的入场时刻。
	if posMS < d.posAt(c.Now())-danmakuSeekSlop.Milliseconds() {
		d.resetTracks()
		// ⚠️ **复位会把 `coverW` 清成 0（「还没按哪个宽度算过」）**：这一帧
		// 已经知道宽度了，补回去——否则下一帧的 `sync` 会以为封面宽度刚变。
		d.coverW = area.W
	}
	d.follow(posMS, c.Now(), st.Playing, st.Playing && !c.Preferences().ReduceMotion, st.Rate)
}

// danmakuColorOf 把 B 站的十进制 RGB 变成画界面用的颜色。
//
// ⚠️ **统一乘 alpha 0.85**（报告 §3.3）：弹幕挡脸是 B 站的老毛病，半透明是
// 它自己的默认做法。
func danmakuColorOf(rgb uint32) ui.Color {
	return ui.RGBA(uint8(rgb>>16), uint8(rgb>>8), uint8(rgb), danmakuAlpha)
}
