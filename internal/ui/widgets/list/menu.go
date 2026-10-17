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
	"github.com/egoist/mygo/ui"
)

// menuItem 是右键菜单里的一项。sep 的是分隔那一行——它不是一项，MyGo 的分隔
// 由 `Menu.Separator` 画；把它放进项表，是为了让项表读起来和 Biu 的那份一样
// （Biu 的 ContextMenuItem 里也有一个只起分组作用的 `hidden` 项）。
type menuItem struct {
	key   string
	label string
	sep   bool
}

// menuSeparator 是分隔那一行。
var menuSeparator = menuItem{sep: true}

// menuItems 是这一行的菜单，项和顺序照 Biu 的
// `biu/src/pages/music-recommend/menu.tsx`（搜索结果那一套完全一样）。
//
// ⚠️ **「打开来处」按这一项有没有来处分**：本地文件没有 bvid，写「在 B 站打开」
// 只能打开一个打不开的网页，所以换成「在访达中显示」。那个判断在
// `actions.go` 的 OpenLabel 里，播放列表抽屉的行菜单也调它。
//
// ⚠️ 别看着 Biu 有图标就给每一项补一张图：MenuItem 没有图标字段，补不进去。
func menuItems(m Music) []menuItem {
	return []menuItem{
		{key: "play", label: "播放"},
		{key: "queue", label: "添加到队列"},
		menuSeparator,
		{key: "open", label: OpenLabel(m.Item)},
		menuSeparator,
		{key: "favorite", label: "收藏"},
		{key: "download-audio", label: "下载音频"},
	}
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
		if menu.Item(item.label).Chosen() {
			doAction(item, i, song, acts, playFrom)
			return
		}
	}
}

// doAction 是菜单项被选中之后做的事。i 是这一项的下标，song 是它那一首。
func doAction(item menuItem, i int, song Music, acts Actions, playFrom func(int)) {
	switch item.key {
	case "play":
		playFrom(i)
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
			// 说清楚比按了没反应好（见 download-audio 那一支的注释）。
			notify(acts, "「"+item.label+"」还没接 B 站接口")
			return
		}
		acts.FavSelect(song.Item)
	case "download-audio":
		// ⚠️ 这一项**还没接线**，而缺的不是那个服务：下载服务早就写好了
		//（`internal/service/download`，issue #29 已 CLOSED），抽屉那一支也早就
		// 能下载（`queue_drawer.go` 的 `startDownloadAudio`）。缺的是
		// `list.Actions` 上**根本没有 `Download` 这个字段**——列表这一侧压根没把
		// 下载递过来，所以任何一页的列表菜单都走不到它。
		//
		// 话里带上用户按的那个字：菜单有五项，只说「还没接」的话用户不知道说的
		// 是哪一项，得自己再点一遍试。
		notify(acts, "「"+item.label+"」还没接 B 站接口")
	}
}
