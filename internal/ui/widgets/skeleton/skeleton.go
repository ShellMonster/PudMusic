// Package skeleton 是「骨架屏」：内容还没到的时候，在它将来要出现的位置先画一块
// 灰色占位，并让一道高光从左往右扫过去。
//
// 设计稿：docs/design-mockups/skeleton.png。
//
// # 和转圈的分工
//
// 转圈（`internal/ui/loading.go`）是**整页首屏**那一档，说的是「还没好」；骨架屏
// 是**内容的形状已知**时用（列表行、封面网格、头图区），让用户在等的时候就看见
// 版面，数据一到只是把灰块换成真内容，版面不跳。
//
// # 画法
//
// 每一块是一个 `Draw` 回调：底色是前景色的 8%，高光是一道 35% 宽的竖带，带里是
// 背景色的 55%。**相位只由钟决定**（不看元素何时出现），所以同一屏上所有灰块的
// 高光是同步扫过的，看起来是一道光而不是一堆各自闪的块。
//
// ⚠️ **走局部重绘**（`p.AnimationFrame()`）而不是 `c.AnimationFrame()`：灰块是画出来
// 的，每帧重建整窗只为了挪一道高光不划算（同 `hover.Wave`）。
// ⚠️ **看不见的窗口不要帧**（票 #156）：开关是 `hover.VisibleFlag()`，建的时候认下
// 指针，画的时候读值。
package skeleton

import (
	"sync/atomic"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/hover"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	// Period 是高光扫过一整块的时长。
	Period = 1400 * time.Millisecond
	// bandShare 是高光带占一块宽度的几成。
	bandShare = 0.35
	// baseAlpha 是灰块底色（前景色）的不透明度。
	baseAlpha = 0.08
	// glowAlpha 是高光带中心（背景色）的不透明度。
	glowAlpha = 0.55
)

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
	base, glow := t.Text.Alpha(baseAlpha), t.Background.Alpha(glowAlpha)
	visible := hover.VisibleFlag()
	return b.Draw(func(p *ui.Painter, r ui.Rect) {
		paint(p, r, radius, base, glow, visible)
	})
}

// paint 画一块：底色 + 一道扫过的高光带。拆出来是为了测试能直接量。
func paint(p *ui.Painter, r ui.Rect, radius float32, base, glow ui.Color, visible *atomic.Bool) {
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
	if visible.Load() {
		p.AnimationFrame()
	}
}

func clamp01(v float32) float32 { return min(max(v, 0), 1) }

// Row 画一行歌曲占位：方形缩略图 + 两行字条 + 右侧时长条。总高 rowH。
func Row(c *ui.Context, key string, rowH float32) {
	pad := float32(6)
	thumb := rowH - 2*pad
	ui.Row(c).Key(key).FillWidth().Height(rowH).Padding(pad).Gap(space.Px12).AlignItems(ui.Center).Children(func() {
		Block(c, key+":t", thumb, thumb, 6)
		ui.Column(c).Grow(1).Gap(space.Px8).Children(func() {
			Block(c, key+":a", 0, 12, 4)
			Block(c, key+":b", 120, 10, 4)
		})
		Block(c, key+":d", 40, 10, 4)
	})
}

// Card 画一张封面卡片占位：方形封面（边长 side）+ 两条字条。
func Card(c *ui.Context, key string, side float32) {
	ui.Column(c).Key(key).Width(side).Gap(space.Px8).Children(func() {
		Block(c, key+":c", side, side, 8)
		Block(c, key+":a", side*0.8, 12, 4)
		Block(c, key+":b", side*0.5, 10, 4)
	})
}

// Post 画一条动态占位：头像 + 名字条，下面三行正文条（最后一行短）。
func Post(c *ui.Context, key string) {
	ui.Column(c).Key(key).FillWidth().Padding(space.Px12).Gap(space.Px12).Children(func() {
		ui.Row(c).Gap(space.Px12).AlignItems(ui.Center).Children(func() {
			Block(c, key+":av", 40, 40, 20)
			ui.Column(c).Gap(space.Px8).Children(func() {
				Block(c, key+":n", 120, 12, 4)
				Block(c, key+":t", 72, 10, 4)
			})
		})
		Block(c, key+":l1", 0, 12, 4)
		Block(c, key+":l2", 0, 12, 4)
		Block(c, key+":l3", 220, 12, 4)
	})
}
