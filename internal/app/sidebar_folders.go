// 侧栏收藏夹右键菜单里的那四件事：播放、添加到播放列表、删除、取消收藏。
//
// # 为什么放在这一层，而不是界面层
//
// 「播放整个收藏夹」和「把整个收藏夹加进队列」都要**先把里面的内容拉回来**
// （网络往返，界面线程上一次都不许有，铁律三），而界面层不认识 B 站。
// 「删除」和「取消收藏」更直接：它们动的是用户真实的数据，而写操作在整个项目
// 里**各只有这一处**——第二处发出去的那次没人挡得住。
//
// # 结果是一个字符串，不是 error
//
// 四个方法的回调是 `func(string)`，**成功和失败都调**，而那句话是已经说全了的
// 一整句（「已播放 200 首，这个收藏夹里还有更多没放进来」/「播放不了这个收藏夹：
// 被 B 站风控拦下了」）。
//
// 这么定是因为这四件事里有一半**成功得并不圆满**：截断、跳过放不了的条目。
// 用 error 报就得在每一处判断「这个错是失败还是『成功但是少放了』」，而那句
// 话的措辞只有这一层知道（见 `folderItems` 里那个 200 的截断）。界面那边
// 一律 `s.addToast(msg)`。
//
// # 「整个收藏夹」是有限的
//
// 🔴 `folderItemLimit`：翻页翻到上限就停，**并且把这件事说出来**。
//
// 一个几千首的收藏夹真存在（UP 主的投稿合集就是）。全放进去会占住队列好几
// 小时，而用户点的是侧栏里一行收藏夹的名字。所以截断是对的，**闷头截断是错的**
// ——表现是「收藏夹明明有 1200 首，我只放了 200 首，界面上一个字都没说」。
package app

import (
	"context"
	"fmt"
	"log"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// folderItemLimit 是「从侧栏菜单播放 / 加入整个收藏夹」最多取几首。
//
// 20 一页（B 站的上限，见 `bili.pageSizeLimit`），所以 200 是十页。
//
// 为什么不是「翻到完」：见文件头。更多会让一个中等收藏夹等很久，更少会少得
// 让人以为坏了——200 是这两头之间那个不偏不倚的数。
const folderItemLimit = 200

// PlayFolder 播放这个容器里的内容。**立刻返回**，跑完了在后台调 done。
//
// 从第一首起，把翻出来的那几首接进队列并开始播——和收藏夹页的「播放全部」是
// 同一件事，那边读到的是用户已经滚出来的那几页，这边是现拉的。
func (a *App) PlayFolder(f Favorite, done func(string)) {
	a.withFolderItems(f, "播放", "播放", done, func(items []Item) error {
		return a.PlayFrom(items...)
	})
}

// EnqueueFolder 把这个容器里的内容接进队列末尾。**立刻返回**，跑完了调 done。
//
// 不打断正在放的那首——和「播放」的分界就在这儿（见 `Enqueue` 的注释）。
func (a *App) EnqueueFolder(f Favorite, done func(string)) {
	a.withFolderItems(f, "加入队列", "加进队列", done, func(items []Item) error {
		return a.Enqueue(items...)
	})
}

// withFolderItems 是上面两条背后那一个：拉内容 → 交给 act → 说一句话。
//
// verb 是「已经做了」那句话里的动词（已播放 N 首），able 是「还没有」那一句里
// 的那一个（还没有能加进队列的歌）——两个不一样，因为「加入队列」和「加进
// 队列」读起来不是一回事。
//
// ⚠️ `done` 一定被调，**成功和失败都调**：界面那边是一次性的接缝，不调就是
// 用户按下去之后什么也没发生（比说错一句话更难查）。
func (a *App) withFolderItems(f Favorite, verb, able string, done func(string), act func(items []Item) error) {
	if a.collectionsSnapshot() == nil {
		sayFolder(done, verb+"不了这个收藏夹：歌单接口还没挂上")
		return
	}
	ref := folderRef(f)
	go func() {
		items, left, err := a.folderItems(context.Background(), ref)
		if err != nil {
			sayFolder(done, verb+"不了这个收藏夹："+bili.Friendly(err))
			return
		}
		if len(items) == 0 {
			sayFolder(done, "这个收藏夹里还没有能"+able+"的歌")
			return
		}
		if err := act(items); err != nil {
			sayFolder(done, verb+"不了："+bili.Friendly(err))
			return
		}
		// 🔴 截断的那一句：**成功也要说**（理由见文件头）。
		if left > 0 {
			sayFolder(done, fmt.Sprintf("已%s %d 首，这个收藏夹里还有更多没放进来",
				verb, len(items)))
			return
		}
		sayFolder(done, fmt.Sprintf("已%s %d 首", verb, len(items)))
	}()
}

// folderPageLimit 是「翻到第几页就认输」。
//
// 🔴 **不能只靠 `items` 那个上限收口**：一个收藏夹里如果全是合集 / 剧集（那几类
// 放不了，`playableItems` 会跳过它们），`items` **一个都不会长**，而 B 站那边
// `has_more` 一直是 true——于是那一趟会一直翻页，直到天荒地老。表现是「点了
// 播放，侧栏那一行转圈转到应用退出」。
//
// 20 一页（B 站的上限），200 页是四千条：一个正常人的收藏夹到不了，而真到了
// 的话用户早就等不下去了。
const folderPageLimit = 200

// folderItems 把容器里的内容翻完（最多 folderItemLimit 首）并翻成队列项。
//
// 返回的第二项是**还有多少没取到**（0 表示全都在里面了）——截断的那些。
//
// 翻页的循环在这一层而不是 `bili`：那一层的规矩是「发一次请求就返回」
// （同一个理由见 `foldermanage.go` 的 `fetchCreatedFolders`）。
//
// ⚠️ **三个终止条件缺一不可**：`has_more` 说没有、`items` 到了上限、页数到了
// `folderPageLimit`（理由见那个常量的注释）。只认前两个的话，遇到全放不了的
// 收藏夹会一直翻页。
func (a *App) folderItems(ctx context.Context, ref CollectionRef) ([]Item, int, error) {
	var items []Item
	for page := 1; page <= folderPageLimit; page++ {
		medias, hasMore, err := a.LoadCollectionPage(ctx, ref, bili.SortByFavTime, page)
		if err != nil {
			return items, 0, err
		}
		items = append(items, playableItems(medias)...)
		if !hasMore || len(medias) == 0 {
			return items, 0, nil
		}
		if len(items) >= folderItemLimit {
			return items, 1, nil
		}
	}
	return items, 1, nil
}

// playableItems 把一批内容翻成队列项，放不了的那种跳过。
//
// 和界面层 `itemsOfMedias` 是同一件事，**必须跳过而不是放进去再报错**：一个
// 收藏夹里躺着两个合集就让「播放整个收藏夹」整条失败，是用户没法接受的。
func playableItems(medias []bili.Media) []Item {
	items := make([]Item, 0, len(medias))
	for _, m := range medias {
		if !m.Playable() {
			continue
		}
		items = append(items, ItemOfMedia(m))
	}
	return items
}

// DeleteFavoriteFolder 删掉自己创建的这个收藏夹（连它里面的内容）。**立刻返回**。
//
// 🔴 **不可撤销**（里面的歌一起没了），所以界面上有二次确认。这一层不重复
// 问一遍——那个确认框得画在界面线程上。
//
// 成了之后把这一行从侧栏里拿掉并落一次设置（见 `forgetFavorite`）。
//
// ⚠️ **只对「我创建的」那一组有意义**：删除别人的收藏夹是 B 站不允许的，
// 而菜单上「删除」这一项本来就只出现在我创建的那一组（见 `favMenuItems`）。
func (a *App) DeleteFavoriteFolder(f Favorite, done func(string)) {
	c := a.collectionsSnapshot()
	if c == nil {
		sayFolder(done, "删不掉这个收藏夹：歌单接口还没挂上")
		return
	}
	go func() {
		if err := c.refs.Client.DeleteFolder(context.Background(), f.ID); err != nil {
			log.Printf("biu: 删收藏夹 %d 失败：%v", f.ID, err)
			sayFolder(done, "删不掉这个收藏夹："+bili.Friendly(err))
			a.wakeUI()
			return
		}
		a.forgetFavorite(FavoritesCreated, f.ID)
		sayFolder(done, "已删掉「"+f.Title+"」")
		a.wakeUI()
	}()
}

// UnfavoriteFolder 取消收藏这个收藏夹（**别人的**）。**立刻返回**。
//
// 和删除的分界：删除的是「我创建的、连同里面的歌一起没了」，这个只是把我对
// 它的那份收藏摘掉——歌还在 UP 主那里，UP 主那边一个字都不会少。所以它
// **不要二次确认**：撤回来只要再点一次，而点错了之后还弹一个框只会让人更烦。
func (a *App) UnfavoriteFolder(f Favorite, done func(string)) {
	c := a.collectionsSnapshot()
	if c == nil {
		sayFolder(done, "取消不了收藏：歌单接口还没挂上")
		return
	}
	go func() {
		if err := c.refs.Client.UnfavoriteCollection(context.Background(), f.ID); err != nil {
			log.Printf("biu: 取消收藏收藏夹 %d 失败：%v", f.ID, err)
			sayFolder(done, "取消不了收藏："+bili.Friendly(err))
			a.wakeUI()
			return
		}
		a.forgetFavorite(FavoritesCollected, f.ID)
		sayFolder(done, "已取消收藏「"+f.Title+"」")
		a.wakeUI()
	}()
}

// forgetFavorite 把这一行从侧栏里拿掉，并把落盘的顺序一起改掉。
//
// ⚠️ **两样都要**：只从内存里拿掉的话，`SideMenuCollectionOrder` 里还留着
// 那个 id，下次拉全量（`Replace` → `applySavedOrder`）它又会冒回原位——
// 用户刚删掉的东西自己长回来了。
func (a *App) forgetFavorite(g FavoriteGroup, id int64) {
	a.FavoritesOf().Remove(g, id)
	if _, err := a.UpdateSettings(func(s *Settings) {
		s.SideMenuCollectionOrder = s.SideMenuCollectionOrder.setList(g, a.FavoritesOf().IDs(g))
	}); err != nil {
		log.Printf("biu: 侧栏顺序里去掉收藏夹 %d 失败：%v", id, err)
	}
}

// folderRef 把侧栏那一行翻成容器引用。
//
// ⚠️ **类型认不出时按收藏夹处理**（和 URL 上的 `type` 一样，见
// `bili.CollectionType.Valid`）：侧栏那一行的 Type 是别的页面填的，而
// 「按收藏夹处理」最多是打不开，「整条失败」会让用户以为自己点的不是收藏夹。
func folderRef(f Favorite) CollectionRef {
	t := bili.CollectionType(f.Type)
	if !t.Valid() {
		t = bili.CollectionFavorite
	}
	return CollectionRef{ID: f.ID, Type: t}
}

// sayFolder 是「后台那一趟做完了，说一句」那一步。
//
// done 是 nil 的调用方（测试里那些不想听结果的）什么都不用做。
func sayFolder(done func(string), msg string) {
	if done != nil {
		done(msg)
	}
}
