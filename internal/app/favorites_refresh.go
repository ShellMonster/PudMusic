// 「刷新数据」：重新拉一次侧栏那两组收藏夹（头像下拉里的那一项，见
// `ui/navbar_avatar.go`）。
//
// # 它替代的是一句「还没联过 B 站接口」
//
// 头像下拉那一项以前按下去只弹一句「没有数据可刷新」——而收藏夹接口是有的
// （`bili.CreatedFolders`，`created/list`，收藏夹的编辑 / 新建 / 拉全量都靠它）。
// 所以现在按下去是真的重新拉一次，用户在 B 站网页上改了收藏夹，切回来能看到。
//
// # 只刷「我创建的」这一组，而那句话也只说它
//
// 本项目只接了「某个人创建了哪些收藏夹」这一个列表接口。**「我收藏了别人的哪些
// 收藏夹」在这一层压根没有数据源**——侧栏上那一组至今只有 `SetFavorites` 能填，
// 而填它的只有测试。所以刷的是有源的那一组，说出来的那句话也只提它：写一句
// 「已刷新收藏夹」会让用户以为另一组也重新拉过了，而它没有。
//
// 那一组的几件事仍然照常（播放整个收藏夹、取消收藏、拖拽排序），它们动的是内存里
// 已经在的那一份，不依赖刷新。
//
// # 结果是一句话，走信箱而不是回调
//
// 和 `folderResult` 同一个形状（**取走**而不是读，见那个类型的注释）：界面每一帧
// 开头调一次 `TakeFavoritesRefresh`，取到就弹一条提示，取不到就什么也不做。
//
// 回调那一套也能用，但刷新这件事**不需要界面等着它**——侧栏那条「正在刷新…」
// 是从 `FavoritesRefreshing` 读的，和最终那句话是两回事：前者在飞的时候就有，
// 后者落定之后才有。
package app

import (
	"log"
	"strconv"

	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// favRefresh 是「正在刷」那一趟的状态，连同跑完之后要说的那句话。
//
// ⚠️ **不另起一把锁**：它挂在 `Favorites` 里，跟着 `Favorites.mu` 走。这两件事是
// 同一份侧栏收藏夹的两种状态（条目 + 「正在重新拉它」），而 `FavoritesOf` 已经
// 是这个 App 挂这一摊的那张表了（见 favorites.go 的 `mountedFavorites`）——再加
// 一张包级的表挂同一件事，只会让「一个 App 的收藏夹状态到底存哪儿」有两个答案。
type favRefresh struct {
	// busy 是「那一趟还在飞」。
	busy bool
	// have / text / tone 是后台攒下的话。**取走过就没有了**。
	//
	// ⚠️ **tone 跟着话一起攒**（票 #180）：界面把这句话弹成一条提示，而提示
	// 要按语义分色（成了绿、没成红）。话在这一层生成，**成没成也只有这一层
	// 知道**——界面那边按字猜的话，改一次文案就错一次颜色。
	have bool
	text string
	tone notice.Tone
}

// RefreshFavorites 后台重新拉一次侧栏那些收藏夹。**立刻返回**。
//
// 一次只会有一趟在飞：已经在刷的时候这一句返回 false（`tryBeginRefresh`），
// 而**那句话也会说给用户听**（「正在刷新收藏夹…」）——按下去没反应是最容易被
// 当成程序坏了的那一种。
//
// 说得出口的拒绝只有两档，都在这一层判，界面不用自己复述一遍：
//
//   - 没登录：这一组数据要的是「我的收藏夹」，未登录时 B 站一律回 -101。
//   - 收藏夹那一摊没挂上：那就没有能发请求的零件（见 `CollectionsMounted`）。
//
// ⚠️ **没登录排在前面**：那是用户自己能解决的那一档，而「接口没挂上」他看见也
// 办不了。先说后者的话，一个只是没登录的人会读到一句跟他无关的话。
func (a *App) RefreshFavorites() {
	f := a.FavoritesOf()
	if !f.tryBeginRefresh() {
		// ⚠️ **这句话不许和侧栏那一行「正在刷新收藏夹…」一样**：它们是屏幕上的
		// 两个不同位置，而字一样的话，读画面的测试分不出「侧栏上那行还在」和
		// 「右下角那条提示还没过期」——等错一个就要多等两秒，而那两秒会把真正
		// 要看的那句「已刷新…」吃掉。
		f.setRefreshResult("已经在刷了，等这一趟完", notice.Plain)
		return
	}

	mid := a.SelfMid()
	if mid <= 0 {
		f.finishRefresh("还没登录，登录之后才能刷新收藏夹", notice.Warning)
		return
	}
	c := a.collectionsSnapshot()
	if c == nil {
		f.finishRefresh("刷不了收藏夹：这一摊还没接上 B 站接口", notice.Danger)
		return
	}
	a.refreshCreatedFolders(f, c, mid)
}

// refreshCreatedFolders 是真的那一趟：翻完 B 站那些页，换掉侧栏那一组。
//
// ⚠️ **翻页的循环复用 `fetchCreatedFolders`**，不是自己再写一遍：那一处已经踩过
// 「B 站在最后一页上说 has_more」的坑（见它的注释），而刷新和收藏夹选择弹窗问的
// 是同一个接口、终止条件必须一模一样——两处各写一遍的话，哪天只改了一处，症状是
// 「有时候刷新出来多一行空白收藏夹」。
//
// rid 传 0：不问「某一条内容在不在里面」——侧栏只需要有哪些格子。
func (a *App) refreshCreatedFolders(f *Favorites, c *collections, mid int64) {
	go func() {
		list, err := a.fetchCreatedFolders(c, 0)
		if err != nil {
			log.Printf("biu: 刷新收藏夹失败：%v", err)
			f.finishRefresh(errTextFolder(err), notice.Danger)
			a.wakeUI()
			return
		}
		// 走 `SetFavorites`（不是 `FavoritesOf().Replace`）：它顺手按设置里存的
		// 顺序排好，而**顺序不是接口给的，是用户拖出来的**（见 favorites.go 文件头）。
		// 少走这一步的话每刷一次，用户整理好的顺序就被接口的顺序顶掉。
		a.SetFavorites(FavoritesCreated, createdFavorites(list, mid))
		f.finishRefresh(refreshDoneText(len(list)), notice.Success)
		a.wakeUI()
	}()
}

// createdFavorites 把 B 站那份翻成侧栏要的那一种。
//
// Type 一律填「收藏夹」（11）：这个接口给的**只有收藏夹**，而 `folderRef` 认不出
// 类型时会按收藏夹处理（见 sidebar_folders.go），填对了那一处的兜底就不用救了。
func createdFavorites(list []bili.CreatedFolder, mid int64) []Favorite {
	out := make([]Favorite, 0, len(list))
	for _, it := range list {
		out = append(out, Favorite{
			ID: it.ID, Title: it.Title, Mid: mid, Type: int(bili.CollectionFavorite),
		})
	}
	return out
}

// refreshDoneText 是刷完之后说的那句话。
//
// 一个都没有时说的是「你还没有创建收藏夹」而不是「已刷新 0 个」：后者读起来像
// 「刷了但白刷」，而真相是「B 站那边一个都没有」——用户接下来要做的动作不一样。
func refreshDoneText(n int) string {
	if n == 0 {
		return "已刷新，你还没有创建收藏夹"
	}
	return "已刷新「我创建的」：" + strconv.Itoa(n) + " 个"
}

// FavoritesRefreshing 报此刻有没有那一趟在飞。侧栏那条「正在刷新…」读的是它。
func (a *App) FavoritesRefreshing() bool { return a.FavoritesOf().refreshBusy() }

// TakeFavoritesRefresh 取走后台攒下的那一句话，取走之后就轮到下一句。
//
// 和 `TakeFolderResult` 同一个道理——**取走而不是读**：读而不取的话同一句会在
// 每一帧都发一次提示，而用户看到的是一条永远消不掉的提示。
func (a *App) TakeFavoritesRefresh() (string, notice.Tone, bool) {
	return a.FavoritesOf().takeRefresh()
}

// ── 那一小份状态的方法（都在 `Favorites.mu` 里） ─────────────────────────────

// tryBeginRefresh 立起「正在刷」，已经有一趟在飞时返回 false。
func (f *Favorites) tryBeginRefresh() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refresh.busy {
		return false
	}
	f.refresh.busy = true
	return true
}

// finishRefresh 收掉「正在刷」，并攒下要说的那句话。
//
// ⚠️ **busy 一定要在说话之前清掉**：界面拿到那句话弹提示的那一帧，正好也是它
// 要把侧栏上那行「正在刷新…」收掉的那一帧。顺序反了的话那一帧里两句话对着干，
// 而留下的是「刷新完了，界面上还在转」的那一种。
func (f *Favorites) finishRefresh(text string, tone notice.Tone) {
	f.mu.Lock()
	f.refresh.busy, f.refresh.have, f.refresh.text, f.refresh.tone = false, true, text, tone
	f.mu.Unlock()
}

// setRefreshResult 只攒下那句话，不碰 busy。
//
// 给「按了第二次」那一档用：那一趟本来就还在飞，**不能**因为这一句就把界面上
// 那行「正在刷新…」收了——用户会以为刷新已经结束了。
func (f *Favorites) setRefreshResult(text string, tone notice.Tone) {
	f.mu.Lock()
	f.refresh.have, f.refresh.text, f.refresh.tone = true, text, tone
	f.mu.Unlock()
}

func (f *Favorites) refreshBusy() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refresh.busy
}

func (f *Favorites) takeRefresh() (string, notice.Tone, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.refresh.have {
		return "", notice.Plain, false
	}
	f.refresh.have = false
	return f.refresh.text, f.refresh.tone, true
}
