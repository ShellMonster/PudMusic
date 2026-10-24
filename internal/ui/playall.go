// 「全部播放」那一枚按钮：**应用里只有一种画法、一个出口**（票 #244 / UX 报告
// §2 的 B6）。
//
// # 为什么它是一个共用的出口
//
// 同一个动作（「把这一页的歌加进播放队列」）在两个页面上都有入口：
//
//	搜索结果页   排序那一行右边（`viewSearchToolbar`）
//	首页         那一排 Tab 的**右沿**（`viewTabs`）
//
// 此前**只有搜索结果页有**：首页**故意没有**——理由是队列语义（追加 / 从这首
// 开始）还没定下来（DESIGN.md §十第 3 条）。于是同一个动作在一个页面能按、在
// 另一个页面找不到，用户会以为是两个不同的功能（UX 报告 §2 的 B6：「用户在一个
// 页面能按它、在另一个页面找不到它」）。
//
// # 语义已裁决（票 #299，2026-10-24，人定的）
//
// 🔴 **「全部播放」= 把这一页的歌按顺序加进队列并播放**——加进队列（追加到
// 队尾）**和**开始播放（从这一页的第一首开始）两件事一起做。
//
// 此前那一支是**只追加、不换队列、不起播**（票 #60 的验收第 1 条，上游也是
// `addList`）：差别在界面上看得见——按了之后正在放的那首**不换**，新的一首
// 排在它后面。裁决之后改 `playAllSongs` **一处**，两个页面一起变——这正是把
// 两处收进一个出口的用处。
//
// 另一半的语义（「立即播放」= **只放当前这一首**）在 `widgets/list` 的菜单里：
// 每一行的右键菜单有一枚「立即播放」，它换掉整个队列、只放那一首。
package ui

import (
	"strconv"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// playAllLabel 是「全部播放」那一枚按钮上的字。
//
// ⚠️ **两个字都别改**：本地音乐页和收藏夹页那一枚叫「播放全部」，而这两个
// 页面上这一枚叫「全部播放」——同一个动作在应用里有两种叫法，是另一种
// 「两处不一致」。这一票统一的是**这两个页面之间**的画法（同一个动作、同一处
// 入口），不是把应用里所有「放一批歌」的入口都改成同一个词（那是另一件事，
// 而且那两处的语义确实不同：本地音乐那枚是**从第一首开始播**）。
const playAllLabel = "全部播放"

// 那一枚按钮的尺寸。**照上游那一枚**（票 #288 / UX 报告 C-05）。
//
// 上游是 HeroUI 的 `Button size="md"`，实图逐像素量出来是 **114×40**：
// 左右内边距 16（`px-4`）、图标 18（`<RiPlayFill size={18} />`）、图标和字之间
// 的缝 8（`gap-2`）、字 14（`text-sm`，四个汉字正好 56）——
//
//	16 + 18 + 8 + 56 + 16 = 114
//
// ⚠️ **改之前那一枚是 79×28 的纯文字**（`ui.PrimaryButton` 的默认样子：
// 内边距 6/14、字 13）。它矮一截（28 < 40）、窄一截（79 < 114），又没有那枚
// ▶ ——而它是**首页和每一页列表上唯一的主色实心块**，用户第一眼先看它。
//
// ⚠️ **高不跟 Tab 那一排齐**：上游首页那一排 Tab 是 44 高（`size="lg"`），
// 按钮是 40，两者在**中线**上对齐——上游那一行是 `items-center`。
const (
	// playAllH 是那一枚的高：上游 `h-10` = **40**。
	//
	// ⚠️ **和 `collectionOpsButtonH` 是同一个数**（合集那一页操作条上的主按钮），
	// 两处都是上游 HeroUI 按钮的 md 档。
	playAllH = 40
	// playAllPadX 是左右内边距：上游 `px-4` = **16**。
	playAllPadX = space.Px16
	// playAllGap 是图标和字之间的缝：上游 `gap-2` = **8**。
	playAllGap = space.Px8
	// playAllIconSize 是那枚 ▶ 的边长：上游 `<RiPlayFill size={18} />` = **18**。
	//
	// ⚠️ **它不跟 `iconMd` 走**：那一档是「图标键」的边长（播放条、顶栏那些），
	// 而这一枚是**按钮里的图标**，上游给的是 18，两件事各说各的。
	playAllIconSize = 18
)

// playAllButton 是「全部播放」那一枚按钮。
//
// ⚠️ **两个页面走同一个出口**：搜索结果页和首页各摆一枚，而它们必须是**同一枚**
// ——同一个字、同一颗实心主色、同一枚图标、同一个盒子、同一个禁用条件。分开画的
// 话，两处迟早会长成两个样子（票 #235 消灭掉的「左边缘落在四个位置上」那类毛病
// 就是这么来的）。
//
// disabled 是「现在按不了」：正在装第一页，或者这一页一首都没有。
//
// # 那枚 ▶ 图标
//
// 上游是 `<RiPlayFill size={18} />`（`startContent`），我们照抄：
// `iconPlayFill` 就是那张 `RiPlayFill`（`icons.go` 里有出处）。
//
// ⚠️ **图标在字前面**（上游的 `startContent` 是**起头**那一块）：所以按钮要用
// **空字**建（`ui.PrimaryButton(c, "")`），自己按「图标 → 字」的顺序摆孩子——
// `ui.PrimaryButton(c, label)` 建出来的那一份字是**第一个**孩子，往后再加只会
// 排在它后面（`mygo/ui/element.go` 的 `Children` 是**加**上去的，不是换掉原来
// 那一份）。这和 `loading.go` 的 `busyPrimaryButton` 是同一个用法。
//
// ⚠️ **图标继承按钮的字色**（`mygo/ui/svg.go` 的 `coreIcon`：「its shapes in the
// color of the text, which it takes from its ancestors」），而主色按钮的字色是
// `t.AccentText`——主色是 `#1ed760` 那一档亮绿时它是**黑**，于是图标和字都是黑
// 的，和上游一样（上游那枚也是 `dark:text-black`）。
func playAllButton(c *ui.Context, disabled bool) ui.Element {
	// ⚠️ **宽是内容给的，不是写死的 114**：16 + 18 + 8 + 56 + 16 = 114，
	// 而这几个数各自都有出处（上面那一段）。写死一个 114 的话，用户把字号调大
	// 之后字会挤出那个盒子——上游那枚是跟着内容长的。
	b := ui.PrimaryButton(c, "").
		Height(playAllH).
		Padding(space.None, playAllPadX).
		Gap(playAllGap)
	b.Children(func() {
		ui.Icon(c, icon(iconPlayFill)).FontSize(playAllIconSize)
		// ⚠️ **字是 14**（上游的 `text-sm`）：`scale.Size14` 是「正文 / 说明」
		// 那一档（DESIGN.md §二）。此前这一枚用的是框架的默认字号（13），
		// 于是它比上游窄 4 个像素。
		ui.Text(c, playAllLabel).FontSize(scale.Font(c, scale.Size14)).SingleLine()
	})
	return b.Disabled(disabled)
}

// playAllSongs 把这一页的歌**追加**到播放队列末尾，并**从这一页的第一首开始播**。
//
// # 语义（票 #299，2026-10-24 人定的）
//
// **「全部播放」= 把这一页的歌按顺序加进队列并播放**。
//
// ⚠️ **两件事一起做**：加进队列（追加到队尾）**和**开始播放（从这一页的第一首
// 开始）。此前只有前一半——按了之后正在放的那首不换，新的一首排在它后面，
// 而界面上一个字的提示都没有（用户会以为那枚按钮是死的）。
//
// 🔴 **放不了的不塞进队列**（票 #244 / 评审）：同一页每一行的右键菜单对那种行
// 是**灰**的（「立即播放 / 从这一首往后听 / 下一首播放 / 添加到队列」四个入口都
// 按不动，末尾还补一句为什么）。而这一枚是**整页一起加**——它要是把放不了的
// 也加进去，用户就会在队列里撞上几首按不动的歌，而界面上一个字的解释都没有。
//
// ⚠️ **跳过了要说一声**：不然用户数不出「这一页有 30 首，而队列里只多了 25 首」。
func (s *Shell) playAllSongs(songs []list.Music) {
	items, skipped := playAllItems(songs)
	if len(items) == 0 {
		if skipped > 0 {
			s.addToast("这一页的歌都放不了（没有可播的稿件）", notice.Warning)
		}
		return
	}
	// ⚠️ **起播的那一首是这一页的第一首**（`EnqueueAndPlay`）：队列里原来有的
	// 那几首排在前面，可正在放的是这一页的第一首。
	if err := s.app.EnqueueAndPlay(items...); err != nil {
		// ⚠️ **说清楚「歌已经进队列了」**：这一枚是**追加**，所以队列里已经多了
		// 那一批——只说「起播失败」的话，用户会以为按下去什么也没发生。
		s.addToast("这一页起播失败："+err.Error()+"（歌已经加进队列了）", notice.Danger)
		return
	}
	msg := "已添加 " + strconv.Itoa(len(items)) + " 首到播放列表，开始播放"
	if skipped > 0 {
		msg += "（有 " + strconv.Itoa(skipped) + " 首放不了，没加进去）"
	}
	s.addToast(msg, notice.Success)
}

// playAllItems 把这一页的行变成队列要的条目：去重，**并去掉放不了的那些**。
//
// ⚠️ **去重是必须的**：B 站翻页会重叠，同一个 bvid 落进 loaded 两次就是同一个
// 条目进队列两次，用户按下一首会连着听到同一首。
//
// 🔴 **放不了的（`app.CanPlay` 判的）要去掉**：判据和右键菜单里那几个播放入口
// **是同一个**（`app.CanPlay`，见 `widgets/list` 的 `markUnplayable`）——
// 菜单说「这一首按不动」，而「全部播放」把它加进队列，两边对不上。
//
// 第二个返回值是**跳过了几首**（去重之后、放不了的那种），用来把话说明白。
func playAllItems(songs []list.Music) ([]app.Item, int) {
	items := make([]app.Item, 0, len(songs))
	seen := make(map[string]bool, len(songs))
	skipped := 0
	for _, m := range songs {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		if !app.CanPlay(m.Item) {
			skipped++
			continue
		}
		items = append(items, m.Item)
	}
	return items, skipped
}
