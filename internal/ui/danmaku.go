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
	d.lanes = [danmakuTracks]danmakuLane{}
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
// # 规则
//
// 每条弹幕找第一条**放得下**的轨道（见 `danmakuLayer.lanes` 那一段的两条
// 判据：时间门槛 + 几何门槛）；八条轨道都放不下就**丢掉这一条**（不排队）。
//
// ⚠️ **轨道是入场那一刻定的**（B 站那样），不是每帧重算的——每帧重算的话，
// 一条弹幕会在屏幕上换轨道，或者被挤掉之后又半路冒出来。
func (d *danmakuLayer) assign(it app.DanmakuItem, width float32, spacingMS int64, coverW float32) danmakuSlot {
	slot := danmakuSlot{
		lane:   -1,
		timeMS: it.TimeMS,
		// 一条字走到**整个出界**（右沿也出去了）要多久：(封面宽 + 字宽) / 速度。
		maxAgeMS: int64((float64(coverW) + float64(width)) * 1000 / danmakuSpeed),
	}
	if d.live < danmakuMaxOnScreen {
		for t := range danmakuTracks {
			lane := d.lanes[t]
			if !lane.used {
				// 空轨道 ⇒ 什么都不挡。
				slot.lane = t
				break
			}
			// 门槛一（报告 §3.3）：前一条走出 30% 的宽度。
			need := spacingMS
			// 门槛二（几何）：前一条的**尾巴**必须已经过了右边沿那条入场线，
			// 否则两条的字会叠在一起（评审提的 critical）。
			if geom := int64(float64(lane.width) * 1000 / danmakuSpeed); geom > need {
				need = geom
			}
			if it.TimeMS-lane.timeMS < need {
				continue
			}
			slot.lane = t
			break
		}
		if slot.lane >= 0 {
			d.lanes[slot.lane] = danmakuLane{
				used:   true,
				timeMS: it.TimeMS,
				width:  width,
			}
			d.live++
		}
	}
	if d.slots == nil {
		d.slots = map[string]danmakuSlot{}
	}
	d.slots[it.ID] = slot
	return slot
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
// （每条弹幕只量一次，量完记在 `d.widths` 里）。
func danmakuAgeBoundMS(text string, fontSize, coverW float32) int64 {
	wBound := float64(len(text)) * float64(fontSize)
	return int64((float64(coverW) + wBound) * 1000 / danmakuSpeed)
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
