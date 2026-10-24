// 「正在放的那一行」这一档的配方：**主色 20% 那一层淡底 + 主色当字**。
//
// # 两个抽屉共用这一份
//
//   - 队列抽屉的「正在播放」那一行（`queue_drawer.go` 的 `viewQueueRow`）；
//   - 播放条上那个分集抽屉的「正在放」那一行（`partdrawer.go` 的 `viewPartRow`）。
//
// ⚠️ **它们是同一件事，所以配方只有一份**：上游那两处都是 HeroUI 的
// `variant="flat" color="primary"`，展开成 `bg-primary/20 text-primary-600`
// ——主色 20% 那一层淡底。票 #294 修了队列抽屉那一处，票 #323 修分集抽屉这一处。
//
// 各写一份的话，迟早有一份被改歪而另一边看不出来——**这正是票 #323 的来由**：
// #294 只修了队列抽屉那一处，分集抽屉那一处一直还是实心主色底，两边于是差着
// 一个上游的数。
//
// ⚠️ **它和 `flatCurrent` 是两件事**（见 `flatbutton.go`）：`flatCurrent` 是
// 「**当前在哪**」——侧栏菜单当前项、左栏那三处共用的**中性灰底 + 主色的字**。
// 改这一份不会把那边改掉，反过来也一样。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/egoist/mygo/ui"
)

// playingRowBgAlpha 是「正在放」那一行底上主色的浓度。
//
// 20% 是上游那个数：HeroUI 的 `bg-primary/20`
// （`music-playlist-drawer/list-item.tsx:32` 的 `variant="flat" color="primary"`
// 展开成 `bg-primary/20 text-primary-600`；分集抽屉那一行是同一个配方）。
const playingRowBgAlpha = 0.2

// playingRowBg 是「正在放」那一行的**底色**：主色 20% 那一层淡底。
//
// 浅色下落在抽屉面板底（`t.Background` = #ffffff）上是 **#d2f7df**——上游实图
// 那个数（票 #294 / 报告 §1 的 S-11）。深色下落在 #18181b 上是 **#193e29**。
//
// ⚠️ **主色是用户可设的**：所以这里是「当前主色的 20%」，没有一个写死的绿。
//
// ⚠️ **这一格不走 `flatCurrent`**：那个是「当前在哪」（侧栏菜单当前项、左栏那
// 三处）共用的中性灰底——改它会把侧栏一起改掉。
func playingRowBg(t *ui.Theme) ui.Color {
	return t.Accent.Alpha(playingRowBgAlpha).Over(playingRowBase(t))
}

// playingRowBase 是那一层淡底**身下那块底板**（不透明的那一份）。
//
// 两个抽屉的面板底都是 `t.Background`（队列抽屉那一枚走 `bg-background`，分集
// 那个浮层是框架的 `stylePanel`），而用户可以挑一个带透明度的底色——那时候面板
// 本身也是半透明的。取它压在白底上的那个数（窗口底色是白的），免得这一格算出
// 来的数和画出来的不一样。
func playingRowBase(t *ui.Theme) ui.Color {
	if t.Background.A == 255 {
		return t.Background
	}
	return t.Background.Over(ui.Color{R: 255, G: 255, B: 255, A: 255})
}

// playingRowText 是「正在放」那一行里**标题**的颜色。
//
// 主色当字，**按那一层淡底算**：浅色下主色（#1ed760）压淡绿底只有 1.65:1，
// 直接用等于把这一行读不出来——所以从主色现算一个压深的变体（`ink.Ensure`，
// ≥4.5:1；默认主色下算出 #127d38）。深色下主色压淡底是 6.21:1，原样就够。
//
// ⚠️ **深浅两套都走同一个算法**：默认主色下深色那一支算出来就是主色本身
// （`ink.Ensure` 达标时原样返回），而用户在深色下挑了一个偏暗的主色时，这一档
// 会把它往白里拉一点——总比那一行读不出来强。
func playingRowText(t *ui.Theme) ui.Color {
	return ink.Ensure(t.Accent, playingRowBg(t), 4.5)
}
