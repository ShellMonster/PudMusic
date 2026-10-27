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
package ui

import (
	"cmp"
	"slices"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
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

	// danmakuMaxAgeMS 是「一条弹幕最多在屏幕上待多久」的**粗筛**（毫秒）。
	//
	// 真正的那一档按**字宽**算（一条字走到出界要 (封面宽 + 字宽) / 速度），
	// 而这里只是一个上界，用来**别去量那些早该出界的字**（量字宽要排一次版）。
	//
	// 15 秒对应「字宽 540 DIP」——比封面上能摆下的最长的一行还长。
	danmakuMaxAgeMS = 15000
)

// danmakuAnim 是弹幕那一个 pose 的动画。
//
// ⚠️ **时长 100ms、线性**（报告 §3.1 实测）：
//
//   - **时长 50~200ms 都一样平滑**（每帧 1 像素）；**500ms 反而慢一半**——
//     目标是每 50 毫秒往前挪一点，时长太长时插出来的值一直追不上目标。
//   - **线性**：匀速横穿本来就该是匀速的，缓动会让它一顿一顿。
//
// ⚠️ **「减少动态效果」开着的时候不用另写一套**：框架在那个偏好下让 pose 的
// X/Y/Scale/Rotate **直接到终点** ⇒ 弹幕变成 20fps 跳着走（实测与「位置算在
// view 里」那一档一模一样，见 `mygo/ui/pose.go` 的 `still && i < 4`）。
var danmakuAnim = ui.Animation{Duration: 100 * time.Millisecond, Ease: ui.Linear}

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

// danmakuLayer 记着弹幕这一摊跨帧的状态。
//
// ⚠️ **为什么要有跨帧的状态**：轨道是**入场那一刻**定的（B 站那样），不是
// 每帧重算的——每帧重算的话，一条弹幕会在屏幕上「换轨道」，或者被挤掉之后又
// 半路冒出来。
type danmakuLayer struct {
	// freeAt 是每条轨道「下一条最早能进」的视频时间（毫秒）。
	//
	// ⚠️ **它是按 key 记的判据**（报告 §3.3）：前一条滚够了 30% 的宽度，
	// 这一条轨道才让下一条进来。
	freeAt [danmakuTracks]int64

	// slots 记着正在滚的那些弹幕：弹幕的 rowid → 它占的那条轨道。
	//
	// ⚠️ **键是弹幕自己的 rowid**（稳定且唯一），不是下标——下标会随着
	// 「前面插了一条」整体错配（铁律四第 4 条）。
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

	// lastPos 是上一帧的位置（毫秒）。
	//
	// ⚠️ **它只用来认「位置往回跳了」**：用户拖动进度条回到前面时，那八条
	// 轨道上记着的「下一条最早能进」还是**将来**的时刻，于是新弹幕一条都放
	// 不进去（屏幕上会一条弹幕都没有）。
	//
	// ⇒ 位置往回跳的时候把轨道那一摊清掉重来。
	lastPos int64
}

// reset 把这一摊清空（换歌、或者压根没有弹幕）。
func (d *danmakuLayer) reset() {
	d.freeAt = [danmakuTracks]int64{}
	d.slots = nil
	d.widths = nil
	d.live = 0
	d.count = 0
	d.first = ""
	d.lastPos = 0
}

// danmakuVisible 是一条此刻该在屏幕上的弹幕。
type danmakuVisible struct {
	// item 是这一条自己（时间、正文、颜色、rowid）。
	item app.DanmakuItem
	// ageMS 是它滚了多久（毫秒）：`当前位置 - 它自己的时间`。
	ageMS int64
	// width 是它的字宽（DIP）。
	width float32
	// lane 是它占的轨道号（-1 = 被丢掉了）。
	lane int
}

// danmakuSortByTime 按视频时间排（稳定的）。
//
// ⚠️ **轨道是按「谁先入场」定的**（B 站那样），所以顺序不能乱。
//
// ⚠️ **排序要稳定**：同一时刻的两条（B 站的数据里有）得保持 B 站给的
// 先后，否则它们的轨道每帧可能换（⇒ 抖）。
func danmakuSortByTime(vis []danmakuVisible) {
	slices.SortStableFunc(vis, func(a, b danmakuVisible) int {
		return cmp.Compare(a.item.TimeMS, b.item.TimeMS)
	})
}

// assign 给一条新出现的弹幕找一条轨道。
//
// # 规则（报告 §3.3）
//
// 每条弹幕找第一条「前一条已经走出 30% 宽度」的轨道；都没有就**丢掉这一条**
// （不排队）。
//
// ⚠️ **轨道是入场那一刻定的**（B 站那样），不是每帧重算的——每帧重算的话，
// 一条弹幕会在屏幕上换轨道，或者被挤掉之后又半路冒出来。
func (d *danmakuLayer) assign(it app.DanmakuItem, width float32, spacingMS int64, coverW float32) danmakuSlot {
	slot := danmakuSlot{
		lane:     -1,
		timeMS:   it.TimeMS,
		maxAgeMS: danmakuMaxAgeMS,
	}
	if it.ID != "" {
		// 一条字走到**整个出界**（右沿也出去了）要多久：(封面宽 + 字宽) / 速度。
		slot.maxAgeMS = int64((float64(coverW) + float64(width)) * 1000 / danmakuSpeed)
	}
	if d.live < danmakuMaxOnScreen {
		for t := range danmakuTracks {
			if d.freeAt[t] <= it.TimeMS {
				slot.lane = t
				d.freeAt[t] = it.TimeMS + spacingMS
				d.live++
				break
			}
		}
	}
	if d.slots == nil {
		d.slots = map[string]danmakuSlot{}
	}
	d.slots[it.ID] = slot
	return slot
}

// expire 收掉已经整个出界的那些（它们占的轨道要让给后来的）。
func (d *danmakuLayer) expire(posMS int64) {
	for id, slot := range d.slots {
		if posMS-slot.timeMS > slot.maxAgeMS {
			delete(d.slots, id)
			if slot.lane >= 0 {
				d.live--
			}
		}
	}
}

// widthOf 报一条弹幕的字宽（DIP）。**量一次就够**。
func (d *danmakuLayer) widthOf(c *ui.Context, id, text string, fontSize float32) float32 {
	if w, ok := d.widths[id]; ok {
		return w
	}
	w, _ := c.MeasureText(0, ui.Span{Text: text, Size: fontSize})
	if d.widths == nil {
		d.widths = map[string]float32{}
	}
	d.widths[id] = w
	return w
}

// danmakuColorOf 把 B 站的十进制 RGB 变成画界面用的颜色。
//
// ⚠️ **统一乘 alpha 0.85**（报告 §3.3）：弹幕挡脸是 B 站的老毛病，半透明是
// 它自己的默认做法。
func danmakuColorOf(rgb uint32) ui.Color {
	return ui.RGBA(uint8(rgb>>16), uint8(rgb>>8), uint8(rgb), danmakuAlpha)
}
