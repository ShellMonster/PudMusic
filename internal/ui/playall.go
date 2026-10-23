// 「全部播放」那一枚按钮：**应用里只有一种画法、一个出口**（票 #244 / UX 报告
// §2 的 B6）。
//
// # 为什么它是一个共用的出口
//
// 同一个动作（「把这一页的歌加进播放队列」）在两个页面上都有入口：
//
//	搜索结果页   排序那一行右边（`viewSearchToolbar`）
//	首页         那一排 Tab 右边（`viewTabs`）
//
// 此前**只有搜索结果页有**：首页**故意没有**——理由是队列语义（追加 / 从这首
// 开始）还没定下来（DESIGN.md §十第 3 条）。于是同一个动作在一个页面能按、在
// 另一个页面找不到，用户会以为是两个不同的功能（UX 报告 §2 的 B6：「用户在一个
// 页面能按它、在另一个页面找不到它」）。
//
// # 这一票只统一**画法**，语义等裁决
//
// 🔴 **语义沿用搜索页那一支，没有重新定**：**只追加，不换队列、不起播**——那是
// 票 #60 的验收第 1 条已经定下的（上游也是 `addList`，不是 `playList`，见
// `pages/search/video-list/index.tsx:89-101`）。差别在界面上看得见：按了之后
// 正在放的那首**不换**，新的一首排在它后面。
//
// 「从这首开始」那一档**仍然待人裁决**（DESIGN.md §十第 3 条）。裁决之后改
// `playAllSongs` **一处**，两个页面一起变——这正是把两处收进一个出口的用处。
package ui

import (
	"strconv"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
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

// playAllButton 是「全部播放」那一枚按钮。
//
// ⚠️ **两个页面走同一个出口**：搜索结果页和首页各摆一枚，而它们必须是**同一枚**
// ——同一个字、同一颗实心主色、同一个禁用条件。分开画的话，两处迟早会长成两个
// 样子（票 #235 消灭掉的「左边缘落在四个位置上」那类毛病就是这么来的）。
//
// disabled 是「现在按不了」：正在装第一页，或者这一页一首都没有。
func playAllButton(c *ui.Context, disabled bool) ui.Element {
	return ui.PrimaryButton(c, playAllLabel).Disabled(disabled)
}

// playAllSongs 把这一页的歌**追加**到播放队列末尾（不换队列、不起播）。
//
// ⚠️ **只追加**（理由见文件头）：上游那一行调的是 `addList`（`addList`，不是
// `playList`），差别在界面上看得见——按了之后正在放的那首**不换**，新的一首
// 排在它后面。
//
// 🔴 **说一句话**：按下去之后屏幕上本来什么都不会变（队列在播放条那儿，用户
// 未必看得到），不说一声的话那枚按钮像是死的。
//
// 🔴 **放不了的不塞进队列**（票 #244 / 评审）：同一页每一行的右键菜单对那种行
// 是**灰**的（「播放 / 下一首播放 / 添加到队列」三个入口都按不动，末尾还补一句
// 为什么）。而这一枚是**整页一起加**——它要是把放不了的也加进去，用户就会在
// 队列里撞上几首按不动的歌，而界面上一个字的解释都没有。
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
	if err := s.app.Enqueue(items...); err != nil {
		s.addToast("加不进队列："+err.Error(), notice.Danger)
		return
	}
	// 话照抄上游（`video-list/index.tsx:100`）。
	msg := "已添加 " + strconv.Itoa(len(items)) + " 首到播放列表"
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
// 🔴 **放不了的（`app.CanPlay` 判的）要去掉**：判据和右键菜单里那三个入口
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
