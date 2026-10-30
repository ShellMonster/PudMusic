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
// # 画法在 `widgets/shimmer`
//
// 这一层只组合形状（行 / 卡片 / 动态）。「底色多深、高光多宽、多快」在
// `widgets/shimmer`——因为**封面缓存也要那条扫光**，而它不能 import 这个包
// （`skeleton` → `hover` → `imgcache`，反过来就是环）。见那个包的文件头。
//
// ⚠️ **看不见的窗口不要帧**（票 #156）：开关在 `shimmer` 里，建的时候认下
// 指针，画的时候读值。
package skeleton

import (
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shimmer"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// Period 是高光扫过一整块的时长（转发 `shimmer.Period`，老调用点还在用它）。
const Period = shimmer.Period

// Phase 是此刻高光的位置（0~1，整条带走完一趟是 1）。只由钟决定。
//
// ⚠️ 留着这一层是为了 `skeleton_test.go` 那条「相位只由钟决定」还在这边量
// （它验的是**这个包用的**画法）；实际算在 `shimmer.Phase`。
func Phase(now time.Time) float32 { return shimmer.Phase(now) }

// Block 画一块宽 w、高 h、圆角 radius 的灰色占位，带扫光。key 必须稳定且唯一
// （铁律四）。w 为 0 表示铺满父级的宽。
func Block(c *ui.Context, key string, w, h, radius float32) ui.Element {
	return shimmer.Block(c, key, w, h, radius)
}

// Row 画一行歌曲占位：方形缩略图 + 两行字条 + 右侧时长条。总高 rowH。
func Row(c *ui.Context, key string, rowH float32) {
	pad := (rowH - 48) / 2
	if pad < 0 {
		pad = 0
	}
	thumb := rowH - 2*pad
	ui.Row(c).Key(key).FillWidth().Height(rowH).Padding(pad).Gap(space.Px12).AlignItems(ui.Center).Children(func() {
		Block(c, key+":t", thumb, thumb, 6)
		ui.Column(c).Grow(1).Gap(space.Px8).Children(func() {
			Block(c, key+":a", 180, 12, 4)
			Block(c, key+":b", 120, 10, 4)
		})
		Block(c, key+":d", 40, 10, 4)
	})
}

// Card 画一张封面卡片占位：方形封面（边长 side）+ 两条字条。
func Card(c *ui.Context, key string, side float32) {
	t := c.Theme()
	ui.Column(c).Key(key).Width(side).Gap(space.Px8).Children(func() {
		Block(c, key+":c", side, side, t.Radius)
		Block(c, key+":a", side*0.8, 12, t.Radius)
		Block(c, key+":b", side*0.5, 10, t.Radius)
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
		Block(c, key+":l1", 300, 12, 4)
		Block(c, key+":l2", 260, 12, 4)
		Block(c, key+":l3", 220, 12, 4)
	})
}
