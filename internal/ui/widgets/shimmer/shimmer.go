// Package shimmer 是「一块灰底 + 一道高光从左扫到右」那一层**画法本身**。
//
// # 为什么单开一个包
//
// 两处要用同一条画法，而它们**不能互相 import**：
//
//	骨架屏（`widgets/skeleton`）  列表首屏那一格
//	封面缓存（`widgets/imgcache`）  每一张网络图的「正在加载」那一档
//
// `skeleton` 现在 import `hover`（为了那个「这一扇窗看不看得见」的开关），而
// `hover` import `imgcache`（列表行封面上的悬停反馈要画那张图）——于是
// `imgcache` 回头 import `skeleton` 就是一个环，编译期直接过不去。
//
// 把画法抽到这一层（它只 import 框架），两边都能用，而「高光多宽、多快、多亮」
// 仍然只有一份。
//
// # 画法
//
// 底色是前景色的 8%，高光是一道 35% 宽的竖带，带里是背景色的 55%。
// **相位只由钟决定**（不看元素何时出现），所以同一屏上所有灰块的高光是同步
// 扫过的，看起来是一道光而不是一堆各自闪的块。
//
// ⚠️ **走局部重绘**（`p.AnimationFrame()`）而不是 `c.AnimationFrame()`：灰块是
// 画出来的，每帧重建整窗只为了挪一道高光不划算（同 `hover.Wave`）。
// ⚠️ **看不见的窗口不要帧**（票 #156）：开关是 `VisibleFlag()`，建的时候认下
// 指针，画的时候读值。
package shimmer

import (
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
)

const (
	// Period 是高光扫过一整块的时长。
	Period = 1400 * time.Millisecond
	// bandShare 是高光带占一块宽度的几成。
	bandShare = 0.35
	// BaseAlpha 是灰块底色（前景色）的不透明度。
	//
	// ⚠️ **它是「看得见」那一档的下限**：0.08 的前景色在浅色下是 #ebebeb 上下，
	// 压在 #ffffff 的页面上一眼能看出是一块占位；再淡（0.04）就只剩「比白稍暗」，
	// 一屏几十块看起来仍然是一片空白。
	BaseAlpha float32 = 0.08
	// GlowAlpha 是高光带中心（背景色）的不透明度。
	GlowAlpha float32 = 0.55
)

// frameVisible 是「画这一帧的那个窗口，用户现在看得到吗」那一个开关。
//
// ⚠️ **它是逐窗口一份的**（票 #392）：主窗口失焦不等于迷你播放器看不见
// （`internal/ui/presence.go`：「为什么每个窗口各有一份」）。所以这里放的是
// **一个指针**——每一扇窗装上自己那一份，而用到它的画法在**建的时候**把指针
// 认下来（见 `Block`）。
//
// 零值必须是「看得见」：库自己的测试直接调 `Block`，没有窗口来装开关。
var frameVisible = NewVisibleFlag()

// NewVisibleFlag 造一个「看得见」的开关。
func NewVisibleFlag() *atomic.Bool {
	v := &atomic.Bool{}
	v.Store(true)
	return v
}

// SetVisibleFlag 装上「这一扇窗看不看得见」那一个开关。
//
// ⚠️ **装的是指针，不是「看得见吗」**：灰块在**画的时候**问那个开关（那一帧
// 只重画、不重建），所以问出来的答案必须是**活的**。
func SetVisibleFlag(visible *atomic.Bool) {
	if visible == nil {
		visible = NewVisibleFlag()
	}
	frameVisible = visible
}

// VisibleFlag 返回当前装着的「看得见」开关。
//
// ⚠️ **要在建的时候取、画的时候读**：局部重绘那一帧不重建。
func VisibleFlag() *atomic.Bool { return frameVisible }

// Phase 是此刻高光的位置（0~1，整条带走完一趟是 1）。只由钟决定。
func Phase(now time.Time) float32 {
	return float32(now.UnixNano()%int64(Period)) / float32(Period)
}

// Block 画一块宽 w、高 h、圆角 radius 的灰色占位，带扫光。key 必须稳定且唯一
// （铁律四）。w 为 0 表示铺满父级的宽。
func Block(c *ui.Context, key string, w, h, radius float32) ui.Element {
	b := ui.Box(c).Key(key).Height(h)
	if w > 0 {
		b = b.Width(w)
	} else {
		b = b.FillWidth()
	}
	t := c.Theme()
	baseAlpha, glowAlpha := BaseAlpha, GlowAlpha
	if t.Dark {
		baseAlpha = 0.12
		glowAlpha = 0.72
	}
	base, glow := t.Text.Alpha(baseAlpha), t.Background.Alpha(glowAlpha)
	visible := frameVisible
	return b.Draw(func(p *ui.Painter, r ui.Rect) {
		Paint(p, r, radius, base, glow, visible)
	})
}

// Paint 画一块：底色 + 一道扫过的高光带。拆出来是为了让**不建元素的画法**
// （比如封面那格自己是一个 `ui.Box`）也能用同一条。
//
// base / glow 由调用方给：底色要跟着主题走，而这一层不认识主题。
func Paint(p *ui.Painter, r ui.Rect, radius float32, base, glow ui.Color, visible *atomic.Bool) {
	p.Fill(r, base, radius)
	ph := Phase(p.Now())
	// 带的中心从 -band/2 走到 1+band/2，进出都在块外，头尾不会「啪」地出现。
	centre := -bandShare/2 + ph*(1+bandShare)
	lo, hi := centre-bandShare/2, centre+bandShare/2
	if hi > 0 && lo < 1 {
		p.FillGradient(r, ui.LinearGradient{Angle: 90, Stops: []ui.GradientStop{
			{Color: glow.Alpha(0), At: clamp01(lo)},
			{Color: glow, At: clamp01(centre)},
			{Color: glow.Alpha(0), At: clamp01(hi)},
		}}, radius)
	}
	if visible != nil && visible.Load() {
		p.AnimationFrame()
	}
}

func clamp01(v float32) float32 { return min(max(v, 0), 1) }
