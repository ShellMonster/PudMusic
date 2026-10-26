// 进度条上的章节点：画出来（票 #585）。
//
// # 点画在哪
//
// 每一章的**起点**（`Chapter.At`）换算成轨道上的一个分数位置，画一枚比轨道
// 粗一点的**背景色圆点**——它跨在轨道上，读起来是轨道在那里「断了一截」：
// B 站网页版播放器的章节就是这么画的（进度条按章节分段，分隔处留缺口），
// 而且一个「缺口」在**填过的和没填过的两段轨道上都读得出来**，不像一枚
// 灰点（轨道本来就是灰的）或者一枚主色点（填过的那一段本来就是主色的）。
//
// 上游 Biu 没有章节的界面（`web-player.ts` 只定义了 `view_points` 的类型，
// 没有渲染方），所以这里没有上游的数可抄：点的直径是**轨道粗细 + 3**
// （播放条 4 的轨道上 7，满屏 6 的轨道上 9，迷你窗 `Space(2)` = 8）——
// 比轨道粗一点才能盖出看得见的缺口，又不能粗到在 12 高的迷你窗里撞到
// 上下两行。出图对着浅 / 深两套看过（`chapters_test.go` 的
// `TestRenderChapterDotsShots`），三处进度条上的缺口都读得出来。
//
// # 画在哪一层
//
// `DrawOver` 追加在滑块**自己的画法之后**：框架那颗 `ui.Slider`（迷你窗那条）
// 的轨道 / 旋钮 / 玻璃一个都不动，自绘那两根（播放条、满屏）的轨道 / 旋钮
// 也一个都不动——点只是**再画一遍的最上面一层**。⚠️ 不能用 `Draw`：那是
// **替换**（`mygo/ui/element.go` 的 `paintFn`），会把滑块自己的画法整个顶掉。
//
// # 点击的吸附（只吸附「点」，拖动永远原样）
//
// 规则是用户 2026-10-26 口述的那一条：点在两个相邻的点之间、而当前就在其中
// 一个点上的时候，跳到**另一个**点上去——迷你窗那条进度条又矮又窄，点是冲
// 着它去的，点中了却落在两点之间是最难受的一档。拖动**不**吸附：章节内部
// 要能自由拖，吸附只属于「点」这一下。
//
// 点在所有点之外（开头段 / 末段）、正好点中一个点、或者这一首根本没有章节，
// 都按原样 seek——和没有这个功能一个样。
package ui

import (
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// chapterDots 把章节起点换算成轨道上的分数位置（0..1），按起点排好序给。
//
// 少于两个点不画：一个孤零零的点说不出「这里有章节」，只像一个坏点。
// 时长不知道（没歌）也不画。
func chapterDots(chapters []app.Chapter, dur time.Duration) []float32 {
	if dur <= 0 || len(chapters) < 2 {
		return nil
	}
	fracs := make([]float32, 0, len(chapters))
	for _, ch := range chapters {
		// 超出时长的点画上去也指不到地方（数据坏的那一档），丢掉。
		if ch.At < 0 || ch.At > dur {
			continue
		}
		fracs = append(fracs, float32(ch.At)/float32(dur))
	}
	if len(fracs) < 2 {
		return nil
	}
	return fracs
}

// chapterClickTarget 决定一次**点击**（t 是点中的时刻，pos 是当前的播放位置）
// 该落在哪里；ok=false 表示按原样 seek 到 t。
//
// chapters 要**按起点排好序**（`App.Chapters` 给的就是排好的）。规则见文件头：
//
//	t 严格落在相邻两点 (A, B) 之间时——
//	  当前在 B 那一章（含正好在 B 上）→ 跳 A
//	  当前在 A 那一章（含正好在 A 上）→ 跳 B
//	  当前在别处                      → 跳离 t 更近的那个（打平算左边的）
//	t 在任何空档之外（正好点上 / 开头段 / 末段）→ 原样 seek 到 t
func chapterClickTarget(t, pos time.Duration, chapters []app.Chapter) (time.Duration, bool) {
	if len(chapters) < 2 {
		return 0, false
	}
	// 当前在哪一章：pos 左边（含压线）最近的章节起点。压线算它自己那一章
	// ——「正好在 A 上」就是 A 那一章。
	cur := -1
	for i, ch := range chapters {
		if ch.At <= pos {
			cur = i
		}
	}
	for i := 0; i+1 < len(chapters); i++ {
		a, b := chapters[i].At, chapters[i+1].At
		if t <= a || t >= b {
			continue
		}
		switch cur {
		case i + 1:
			return a, true
		case i:
			return b, true
		}
		if t-a <= b-t {
			return a, true
		}
		return b, true
	}
	return 0, false
}

// chapterClickSlack 是「按下去之后指针挪了几像素以内还算点」的余量：
// 手抖两三像素的一下还是点，划出去的才是拖。
const chapterClickSlack = 4

// snappedScrub 把一次松手的进度值按章节点吸附。
//
// pressX / pressPos 是**按下去那一帧**记下的指针位置和播放位置（三个 view
// 各自记账，见 Shell 的 scrubPressX 那几对字段）。松手时指针挪出了余量就是
// 拖——原样返回；是点就按 `chapterClickTarget` 吸附。
func snappedScrub(sl ui.Element, val float64, pressX float32, pressPos time.Duration,
	dur time.Duration, chapters []app.Chapter) float64 {
	if dur <= 0 || len(chapters) < 2 {
		return val
	}
	x, _, _ := sl.PointerPosition()
	if d := x - pressX; d > chapterClickSlack || d < -chapterClickSlack {
		return val
	}
	at, ok := chapterClickTarget(time.Duration(val*float64(dur)), pressPos, chapters)
	if !ok {
		return val
	}
	return float64(at) / float64(dur)
}

// withChapterDots 给**自绘那套滑块**（`selfSlider`：播放条、满屏）画章节点。
//
// pad 是旋钮留在轨道里的那半个宽（`selfSlider` 的取值映射
// `r.X + knobW/2 + (r.W-knobW)*frac` 里那一份），和点落的位置用的是同一个映射
// ——点落在哪、旋钮走到哪，两处必须是同一个算法。col 是「缺口」的颜色
// （播放条给主题底色，满屏给那一行坐着的底）。
func withChapterDots(sl ui.Element, chapters []app.Chapter, dur time.Duration, pad, d float32, col ui.Color) ui.Element {
	fracs := chapterDots(chapters, dur)
	if fracs == nil {
		return sl
	}
	return sl.DrawOver(func(p *ui.Painter, r ui.Rect) {
		cy := r.Y + r.H/2
		drawChapterDots(p, fracs, r.X+pad, r.W-2*pad, cy, d, col)
	})
}

// withChapterDotsCore 给**框架那颗 `ui.Slider`**（迷你窗那条进度）画章节点。
//
// 几何照 `coreSlider` 的画法**镜像**（`mygo/ui/widgets.go`）：旋钮宽是
// `Space(5)`、轨道中线在 `r.Y + Space(2.5)`——点落的位置必须和旋钮走到的地方
// 是同一个算法，差半格就会「点在缝上、歌在点上」。缺口用主题底色。
func withChapterDotsCore(sl ui.Element, chapters []app.Chapter, dur time.Duration) ui.Element {
	fracs := chapterDots(chapters, dur)
	if fracs == nil {
		return sl
	}
	return sl.DrawOver(func(p *ui.Painter, r ui.Rect) {
		t := p.Theme()
		kw := t.Space(5)
		cy := r.Y + t.Space(2.5)
		drawChapterDots(p, fracs, r.X+kw/2, r.W-kw, cy, t.Space(2), t.Background)
	})
}

// drawChapterDots 把那一串分数位置画成跨在轨道上的背景色圆点。
//
// x0 + dx*f 是值→像素的映射（调用方各按自己那套滑块的算法给）；d 是点的直径。
func drawChapterDots(p *ui.Painter, fracs []float32, x0, dx, cy, d float32, col ui.Color) {
	for _, f := range fracs {
		x := x0 + dx*f
		p.Fill(ui.Rect{X: x - d/2, Y: cy - d/2, W: d, H: d}, col, d/2)
	}
}
