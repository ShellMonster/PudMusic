// 「刷新数据」：重新拉一次侧栏那两组收藏夹（头像下拉里的那一项，见
// `ui/navbar_avatar.go`）。
//
// # 它替代的是一句「还没联过 B 站接口」
//
// 头像下拉那一项以前按下去只弹一句「没有数据可刷新」——而收藏夹接口是有的
// （`bili.CreatedFolders`，`created/list`，收藏夹的编辑 / 新建 / 拉全量都靠它）。
// 所以现在按下去是真的重新拉一次，用户在 B 站网页上改了收藏夹，切回来能看到。
//
// # 两组都刷，而那句话也提两组
//
// 「我创建的」走 `created/list`，「我收藏的」走 `collected/list`（见
// `bili.CollectedFolders`）。**两组的条目都是会变的**——用户在 B 站网页上
// 改了收藏夹、取消了收藏，这边就该跟着变——而只刷一组的话，另一组会一直
// 停在旧的那一份上。
//
// 所以出来的那句话也提两组：只提一组的话，用户会以为另一组没重新拉过。
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
	"context"
	"errors"
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
// 用户在头像下拉里按「刷新数据」走的是这一条（见 `ui/navbar_avatar.go`）。
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
	a.refreshFavorites(true)
}

// AutoRefreshFavorites 在**登录成功之后**和**开机时已经登录**这两处拉一次。
//
// # 为什么要自动拉
//
// 侧栏上那两组收藏夹是用户登录之后最想看见的东西之一，而「自己去头像下拉里
// 按「刷新数据」」是一个没人会自己想到的操作——于是那一栏长期是空的，而用户
// 以为是自己没有收藏夹。
//
// # 它和用户自己按下去的那一下有什么不一样
//
//   - **成功时不出声**：侧栏上多出几行就是反馈，而开机就弹一条「已刷新…」
//     是噪声（每次启动都来一次）。
//   - **失败时出声**：用户没要求过这一趟，不说的话他只会看到侧栏是空的，
//     分不清是「拉失败了」还是「真的一 个都没有」。
//   - **没登录时一个请求都不发**：未登录去试登录后的接口，B 站一律回 -101，
//     而那一片 401 噪声会把别的排查全带偏。
//
// ⚠️ **只在登录 / 开机各来一次**：这是网络往返，**不许每帧都拉**。所以触发点
// 是这两处明确的事件，不是界面每一帧问一次「要不要拉」。
func (a *App) AutoRefreshFavorites() {
	if !a.LoggedIn() {
		return
	}
	a.refreshFavorites(false)
}

// refreshFavorites 是真的那一趟（手动和自动走的是同一条路）。
//
// announce 说的是「结果要不要做成一条提示」：手动按的那一下要（用户按了按钮，
// 不说话就是「按下去没反应」），自动拉的那一下**成功不要、失败要**（理由见
// `AutoRefreshFavorites`）。
func (a *App) refreshFavorites(announce bool) {
	f := a.FavoritesOf()
	if !f.tryBeginRefresh() {
		// ⚠️ **这句话不许和侧栏那一行「正在刷新收藏夹…」一样**：它们是屏幕上的
		// 两个不同位置，而字一样的话，读画面的测试分不出「侧栏上那行还在」和
		// 「右下角那条提示还没过期」——等错一个就要多等两秒，而那两秒会把真正
		// 要看的那句「已刷新…」吃掉。
		//
		// 自动拉撞上「已经在刷」时不说话：那一趟是用户自己点的，它跑完会自己
		// 说话，用户不该收到两条。
		if announce {
			f.setRefreshResult("已经在刷了，等这一趟完", notice.Plain)
		}
		return
	}

	mid := a.SelfMid()
	if mid <= 0 {
		// ⚠️ **这一档不算「拉失败」**：没登录的时候压根没去拉，而侧栏上那句话
		// 说的是「我们没问过 B 站」。把它记成失败的话，侧栏会摆一句
		// 「拉不到收藏夹，可以重试」——而用户该做的是**登录**，不是重试。
		//
		// 所以这里只说一句话（手动按的那一下要说：用户按了按钮，不说话就是
		// 「按下去没反应」），**不动那一组的状态**。
		if announce {
			f.finishRefresh("还没登录，登录之后才能刷新收藏夹", notice.Warning)
		} else {
			f.finishQuietly()
		}
		return
	}
	c := a.collectionsSnapshot()
	if c == nil {
		// ⚠️ **不许说「接口」**（票 #242，UX 报告 §3-F 的 F2）：这句是弹给
		// 用户看的。
		a.refuseRefresh(f, "刷不了收藏夹：这一摊暂时加载不出来", notice.Danger, announce)
		return
	}
	a.refreshFavoriteGroups(f, c, mid, announce)
}

// refuseRefresh 是「这一趟压根发不出去」的那一档。
//
// ⚠️ **失败要记进状态里**（`markFailed`）：不然侧栏上那一句话仍然是「还没有
// 收藏夹」，而真相是「我们压根没拉成」——那是撒谎（AGENTS.md §4）。
//
// ⚠️ **「没登录」那一档不走这里**（见调用处）：那是「没去拉」，不是「拉失败」。
func (a *App) refuseRefresh(f *Favorites, text string, tone notice.Tone, announce bool) {
	f.markFailed(FavoritesCreated, text)
	if announce {
		f.finishRefresh(text, tone)
		return
	}
	f.finishQuietly()
}

// refreshFavoriteGroups 是真的那一趟：翻完 B 站那些页，换掉侧栏那两组。
//
// ⚠️ **翻页的循环复用 `fetchCreatedFolders`**，不是自己再写一遍：那一处已经踩过
// 「B 站在最后一页上说 has_more」的坑（见它的注释），而刷新和收藏夹选择弹窗问的
// 是同一个接口、终止条件必须一模一样——两处各写一遍的话，哪天只改了一处，症状是
// 「有时候刷新出来多一行空白收藏夹」。
//
// rid 传 0：不问「某一条内容在不在里面」——侧栏只需要有哪些格子。
//
// # ⚠️ **两组的条目和状态是一起换的**
//
// 这一趟拉两个接口，而界面上的空状态那句话是**按两组的状态算出来的**
// （见 `ui.favEmptyText`）。所以：
//
//   - **两组都拉**，即使第一组已经失败了——另一组的数据是好的，而「因为一个
//     失败就都不拉」的话，用户要等下一次刷新才看得见它。
//   - **条目先摆、状态后记，中间不叫醒界面**：有一帧是「有条目、状态还停在
//     没拉过」的话，界面上那句话会从「还没拉收藏夹」跳到「还没有收藏夹」——
//     而这一跳用户看得见。
func (a *App) refreshFavoriteGroups(f *Favorites, c *collections, mid int64, announce bool) {
	go func() {
		created, errCreated := a.fetchCreatedFolders(c, 0)
		collected, errCollected := a.fetchCollectedFolders(c, mid)

		// 走 `SetFavorites`（不是 `FavoritesOf().Replace`）：它顺手按设置里存的
		// 顺序排好，而**顺序不是接口给的，是用户拖出来的**（见 favorites.go 文件头）。
		// 少走这一步的话每刷一次，用户整理好的顺序就被接口的顺序顶掉。
		if errCreated == nil {
			a.SetFavorites(FavoritesCreated, createdFavorites(created, mid))
		}
		if errCollected == nil {
			a.SetFavorites(FavoritesCollected, collectedFavorites(collected))
		}
		// ⚠️ **一个都没有也是「拉到了」**（见 `markLoaded` 的注释）：那正是
		// 「B 站那边确实没有」这个结论本身，界面上该说的是「还没有收藏夹」。
		if errCreated == nil {
			f.markLoaded(FavoritesCreated)
		} else {
			log.Printf("biu: 刷新「我创建的」失败：%v", errCreated)
			f.markFailed(FavoritesCreated, favErrText(FavoritesCreated, errCreated))
		}
		if errCollected == nil {
			f.markLoaded(FavoritesCollected)
		} else {
			log.Printf("biu: 刷新「我收藏的」失败：%v", errCollected)
			f.markFailed(FavoritesCollected, favErrText(FavoritesCollected, errCollected))
		}

		switch {
		case errCreated != nil:
			f.finishRefresh(favErrText(FavoritesCreated, errCreated), notice.Danger)
		case errCollected != nil:
			f.finishRefresh(favErrText(FavoritesCollected, errCollected), notice.Danger)
		case announce:
			f.finishRefresh(refreshDoneText(len(created), len(collected)), notice.Success)
		default:
			f.finishQuietly()
		}
		a.wakeUI()
	}()
}

// favErrText 是某一组拉失败时说的话。
//
// ⚠️ **说的是哪一组**：两组各拉各的，而「拉不到收藏夹」不说是哪一组的话，
// 用户会以为是两组全都没有了。
func favErrText(g FavoriteGroup, err error) string {
	if errors.Is(err, errNotLoggedInForFolders) {
		return "还没登录，登录之后才能刷新收藏夹"
	}
	name := "「我创建的」"
	if g == FavoritesCollected {
		name = "「我收藏的」"
	}
	return "拉不到" + name + "：" + bili.Friendly(err)
}

// fetchCollectedFolders 一页一页翻「我收藏的收藏夹」到完成为止。
//
// ⚠️ **终止条件是「B 站说没有」**（`CollectedFolders` 拿 count 算出来的那一位），
// **不是「这一页不满」**：这一页里可能有被滤掉的失效条目（`state != 0`），
// 按条数判的话「一页 50 条里有 3 条失效」就会把翻页提前掐掉——
// 而用户收藏夹列表的后几页就这么没了。
//
// 🔴 **collectedPageLimit 是保险丝，不是条件**：count 撒谎时不会一直翻下去。
// 一个收藏了上千个收藏夹的用户到不了这个数（50 一页，200 页是一万个）。
const collectedPageLimit = 200

func (a *App) fetchCollectedFolders(c *collections, mid int64) ([]bili.CollectedFolder, error) {
	cli := c.refs.Client
	// ⚠️ **mid 从 cookie 里读**，和 `fetchCreatedFolders` 同一处（理由见它的
	// 注释）：那一层问的是 `up_mid`，而 `up_mid` 是 cookie 里那个 mid。
	// 读错的话这一趟会说「还没登录」，而真相是登录态好好的。
	if m := cli.SelfMid(); m > 0 {
		mid = m
	}
	if mid <= 0 {
		return nil, errNotLoggedInForFolders
	}
	var all []bili.CollectedFolder
	for page := 1; page <= collectedPageLimit; page++ {
		batch, hasMore, err := c.refs.Client.CollectedFolders(context.Background(), mid, page)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if !hasMore {
			return all, nil
		}
	}
	return all, nil
}

// collectedFavorites 把 B 站那份翻成侧栏要的那一种。
//
// ⚠️ **Type 原样带过去**（11 收藏夹 / 21 合集 / 31 系列）：「我收藏的」那一组
// 三种都有，而类型决定点进去是哪一个页面（见 `ui.collectionPathOf`）。
func collectedFavorites(list []bili.CollectedFolder) []Favorite {
	out := make([]Favorite, 0, len(list))
	for _, it := range list {
		out = append(out, Favorite{
			ID: it.ID, Title: it.Title, Mid: it.Mid, Type: it.Type,
		})
	}
	return out
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
// ⚠️ **两组都提**：这一趟拉的是两组，而只提一组的话，用户会以为另一组
// 没重新拉过——而「我收藏的」那一组的条目是**会变**的（用户在 B 站网页上
// 取消了收藏，这边就该少一行）。
//
// 一个都没有时说的是「B 站那边一个都没有」而不是「已刷新 0 个」：后者读起来
// 像「刷了但白刷」，而真相是「B 站那边确实没有」——用户接下来要做的动作不一样。
func refreshDoneText(created, collected int) string {
	if created == 0 && collected == 0 {
		return "已刷新收藏夹：B 站那边一个都没有"
	}
	return "已刷新收藏夹：「我创建的」" + strconv.Itoa(created) +
		" 个，「我收藏的」" + strconv.Itoa(collected) + " 个"
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

// finishQuietly 收掉「正在刷」，**不攒下任何话**。
//
// 给「自动拉」那一档用（理由见 `AutoRefreshFavorites`）：成功时侧栏上多出
// 几行就是反馈，而开机就弹一条「已刷新…」是噪声。
//
// ⚠️ **它不清 `have`**：信箱里要是还攒着一句别的话（用户自己按的那一下刚
// 攒下的），那句话得留着给界面取走。
func (f *Favorites) finishQuietly() {
	f.mu.Lock()
	f.refresh.busy = false
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
