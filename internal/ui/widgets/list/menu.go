// 每一行的系统原生右键菜单。
//
// 用 MyGo 的 `Element.ContextMenu`（系统菜单）而不是自绘浮层：验收要的是
// 「系统原生样式、键盘方向键能导航」，只有系统菜单免费给这两样。
//
// ⚠️ **菜单项不带图标**：MyGo 的 `MenuItem` 只有 label / disabled / sub 三个
// 字段，没有图标路径（Biu 的菜单项是带图标的 React 组件）。`PLAN.md` §6.7 把
// 「接受没有图标」还是「改用 Popover 自绘」记成一个待定项——这一轮按系统原生
// 菜单做，图标是往后再说的事，不是这一张的范围。
//
// ⚠️ **每一项都要 Item 一遍**：菜单是这趟全部建出来的，只有被 Chosen 的那一项
// 为 true。少建一项，那一项就永远点不到（和 `internal/ui/sidebar_favorites.go`
// 里的 favoriteMenu 同一个道理）。
package list

import (
	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/egoist/mygo/ui"
)

// menuItem 是右键菜单里的一项。sep 的是分隔那一行——它不是一项，MyGo 的分隔
// 由 `Menu.Separator` 画；把它放进项表，是为了让项表读起来和 Biu 的那份一样
// （Biu 的 ContextMenuItem 里也有一个只起分组作用的 `hidden` 项）。
type menuItem struct {
	key   string
	label string
	sep   bool
	// disabled 是这一项**按不动**。
	//
	// 它是「这一首压根放不了」那一类（没挂稿件的推荐条目、缺音频 id 的
	// 音乐库条目）的落地方式：**在按之前就不给入口**，而不是让人按了
	// 再听一句「不能放」。
	disabled bool
	// note 是这一项**不是动作**，是一句给用户看的说明。
	//
	// 它排在菜单末尾，写着「为什么按不动」和「还能怎么办」——按不动而不
	// 说为什么等于让用户猜，而用户猜不出来。
	note bool
}

// menuSeparator 是分隔那一行。
var menuSeparator = menuItem{sep: true}

// menuItems 是这一行的菜单，项和顺序照 Biu 的
// `biu/src/pages/music-recommend/menu.tsx`（搜索结果那一套完全一样）。
//
// 「下一首播放」的位置也是抄上游：Biu 把它摆在「播放」和「添加到队列」**之间**
// （`music-recommend/index.tsx:204-213`）——上游那一组是同一件事的几档（立刻听 /
// 听完这首就听 / 排到最后），连着摆用户才看得出这是个梯度；我们这一段是**四档**
// （比上游多一枚「立即播放」，见下面那一节）。
//
// ⚠️ **「打开来处」按这一项有没有来处分**：本地文件没有 bvid，写「在 B 站打开」
// 只能打开一个打不开的网页，所以换成「在访达中显示」。那个判断在
// `actions.go` 的 OpenLabel 里，播放列表抽屉的行菜单也调它。
//
// ⚠️ 别看着 Biu 有图标就给每一项补一张图：MenuItem 没有图标字段，补不进去。
//
// # 四档播放（票 #299，2026-10-24 拍板）
//
// 这一段菜单现在是**四档**，名字各说各的事：
//
//	立即播放        只放**这一首**（队列被换成这一首）
//	从这一首往后听  从这一首到这一页末尾，接进队列并播放
//	下一首播放      这一首排在正在放的那首后面
//	添加到队列      这一首排到队尾
//
// ⚠️ **「立即播放」和「从这一首往后听」只差几个字，做的事差着一个队列**——所以
// 原来那项叫「播放」的那一枚**改了名**：菜单里同时摆着「播放」和「立即播放」
// 两枚，用户按的是哪一枚只能靠试，而试错的那一下会把用户的队列换掉。
//
// 「立即播放」是**拍板定的语义**（票 #299）：只放当前这一首。它对应的是上游
// 那一枚（老版本歌单行菜单里的 `PlayList/index.tsx:68`「立即播放」）——「立刻
// 放这一首」，不是「把这一页都排上」。
//
// # 放不了的那一类：先别给入口
//
// 首页推荐、音乐榜、新歌速递、搜索结果**共用这一份菜单**，而它们里面
// 确实有放不了的条目（B 站给纯音乐条目不挂稿件，那种行画得出来而放不了）。
// 那一种在菜单里是**灰的**（`Disabled`），并在末尾补一句说明——
// 用户右键就能看到「为什么这一首没有播放」和「还能怎么办」。
//
// ⚠️ **别改成「按了才说」**：那就是把一句应用内部的判断甩给用户，而用户
// 分不清「真没有音频流」和「没取到」（这两件事要走的路正好相反，见
// `internal/app/unplayable.go`）。
func menuItems(m Music) []menuItem {
	// ⚠️ **「播放」那一项在用户行上是另一个动作**：搜索页「用户」那一档的行
	// 不是歌（`app.ItemTypeUser`），它那一项的「播放」是**进 UP 主空间**
	// （上游 `user-card.tsx:51`）。所以那一档的行仍旧写「播放」——写成
	// 「从这一首往后听」的话，用户会以为点下去是在放歌。
	playLabel := "从这一首往后听"
	if m.Item.Type == app.ItemTypeUser {
		playLabel = "播放"
	}
	items := []menuItem{
		{key: "play-now", label: "立即播放"},
		{key: "play", label: playLabel},
		{key: "play-next", label: "下一首播放"},
		{key: "queue", label: "添加到队列"},
		menuSeparator,
		{key: "open", label: OpenLabel(m.Item)},
		menuSeparator,
		{key: "favorite", label: "收藏"},
		{key: "download-audio", label: "下载音频"},
	}
	return markUnplayable(items, m.Item)
}

// markUnplayable 把放不了 / 下不了的那些项按不动，并补上说明。
//
// 两类分开判：**放不了**（没有可播的稿件身份）动的是四个播放入口
// （立即播放 / 从这一首往后听 / 下一首播放 / 添加到队列）；
// **下不了**（音乐库条目：放得了但没有下载通道）动的是「下载音频」。
// 合成一句会错——「下载音频」对放不了的条目也是对的（它压根下不了，
// 但原因不同）。
func markUnplayable(items []menuItem, it app.Item) []menuItem {
	// ⚠️ **判据是 `app.CanPlay`**（应用层那一个），说明是
	// `app.UnplayableMenuNote`（同一判据的另一面）：判据和话分开两处，
	// 免得话说的是一种、判的是另一种。
	if !app.CanPlay(it) {
		for i := range items {
			switch items[i].key {
			case "play-now", "play", "play-next", "queue", "download-audio":
				items[i].disabled = true
			}
		}
		// ⚠️ **只说一段**：放不了的条目必然也下不了（下载按 bvid 取流），
		// 两段说明摆在一起是把同一件事说了两遍，而菜单末尾那一段会顶掉
		// 前一段（用户只看见最后一句）。
		items = append(items, menuItem{key: "why-unplayable",
			label: app.UnplayableMenuNote(it), disabled: true, note: true})
		return items
	}
	if note := app.UndownloadableMenuNote(it); note != "" {
		for i := range items {
			if items[i].key == "download-audio" {
				items[i].disabled = true
			}
		}
		items = append(items, menuItem{key: "why-undownloadable", label: note, disabled: true, note: true})
	}
	return items
}

// buildMenu 建第 i 行的菜单。被选中那一项时按它的 key 做对应的事。
//
// ⚠️ i 什么时候是真的：菜单被点中的那一帧，框架会用**新的一帧重跑一遍**每一行
// 的 row 回调（`ui/menu.go` 里 `rt.consumed = true` 那一句），闭包里的 i 因此
// 总是用户按下去那一刻那一行的下标。行的身份是 ID、框架按 ID 重新对位，所以
// 哪怕「自动续拉」在这中间往头上插了行，l.songs[i] 仍然是用户按的那一首。
//
// 真正会错的是拿 i 当**身份**：序号在插一行之后就指另一首了（铁律四）。
// 这里 i 只用来读当行，身份由 Music.ID 担着。
func (l *List) buildMenu(menu *ui.Menu, i int) {
	buildSongMenu(menu, l.songs, l.acts, i, l.playFrom)
}

// buildSongMenu 是**两种视图共用**的一份菜单：列表的一行和网格的一格都是它。
//
// 网格复用它，不是「照着再写一份」——菜单项、顺序、「在 B 站打开」还是「在访达
// 中显示」这些判断只在这一处，列表和网格不可能对不上。
//
// playFrom 是从这一首往后放队列，各视图自己接（列表按行，网格按格）。
func buildSongMenu(menu *ui.Menu, songs []Music, acts Actions, i int, playFrom func(int)) {
	if i < 0 || i >= len(songs) {
		return
	}
	song := songs[i]

	for _, item := range menuItems(song) {
		if item.sep {
			menu.Separator()
			continue
		}
		// ⚠️ 每一项都要 Item 一遍：只有 Chosen 的那一项为 true。
		//
		// 灰的那几项（放不了 / 下不了）也要 Item —— 少了它们菜单里就没有
		// 那些项，而**说明那一项**也必须 Item，否则它压根不在菜单里。
		it := menu.Item(item.label).Disabled(item.disabled)
		if it.Chosen() {
			// 说明那一项不是动作：它摆在那儿就是给人看的。
			if item.note {
				return
			}
			doAction(item, i, song, acts, playFrom)
			return
		}
	}
}

// doAction 是菜单项被选中之后做的事。i 是这一项的下标，song 是它那一首。
func doAction(item menuItem, i int, song Music, acts Actions, playFrom func(int)) {
	switch item.key {
	case "play-now":
		// 「立即播放」= **只放当前这一首**（票 #299）：队列被换成这一首。
		if acts.PlayNow == nil {
			// 说清楚比按了没反应好（和下面几项同一个道理）。
			notify(acts, "「"+item.label+"」现在还不能用", notice.Warning)
			return
		}
		acts.PlayNow(song.Item)
	case "play":
		playFrom(i)
	case "play-next":
		if acts.PlayNext == nil {
			// 说清楚比按了没反应好（和下面两项同一个道理）。
			//
			// ⚠️ **不许说「还没接上」**（票 #242，UX 报告 §3-F 的 F2）：那是把
			// 实现细节说给用户听——用户不知道应用里哪一块还没接上。
			notify(acts, "「"+item.label+"」现在还不能用", notice.Warning)
			return
		}
		acts.PlayNext(song.Item)
	case "queue":
		if acts.Add != nil {
			acts.Add([]app.Item{song.Item})
		}
	case "open":
		if acts.Open != nil {
			acts.Open(song.Item)
		}
	case "favorite":
		if acts.FavSelect == nil {
			// 说清楚比按了没反应好。**话里带上用户按的那个字**：菜单有五项，
			// 只说一句「现在还不能用」的话用户不知道说的是哪一项，得自己再点
			// 一遍试。
			//
			// ⚠️ **不许说「还没接接口」**（票 #242，UX 报告 §3-F 的 F2）。
			notify(acts, "「"+item.label+"」现在还不能用", notice.Warning)
			return
		}
		acts.FavSelect(song.Item)
	case "download-audio":
		if acts.Download == nil {
			// 同上。Biu 对本地项和「用户不是歌」那几种是把这一项藏起来的，
			// 我们不藏（见 `queue_drawer.go` 的 queueMenuItems 那一段），
			// 所以这里要有一句能说清「为什么这一项按不动」的话。
			//
			// ⚠️ **不许说「还没接接口」**（票 #242，UX 报告 §3-F 的 F2）。
			notify(acts, "「"+item.label+"」现在还不能用", notice.Warning)
			return
		}
		acts.Download(song.Item)
	}
}
