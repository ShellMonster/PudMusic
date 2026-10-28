// 「一批 B 站内容怎么变成队列里的几项」——**队列入口的唯一实现**。
//
// # 为什么它住在 app 层
//
// 这一件事有**两个入口**，而它们是同一件事：
//
//	收藏夹页的「播放全部」/「添加到播放列表」	界面层（`internal/ui`）
//	侧栏收藏夹的右键「播放」/「添加到播放列表」	app 层（`internal/app`）
//
// 它们原来**各写了一份**（界面层的 `itemsOfMedias` 和 app 层的
// `playableItems`），两份各判一次「这一条能不能放」。而「能不能放」这件事
// 在这个项目里有**三档**（见 `bili.Media`）：
//
//	有没有音频流	合集 / 电影没有（`Playable`）
//	还在不在	失效的稿件（`Invalid`）
//	有没有来源	本地文件 / 在线条目
//
// 各判一次的话，改判据时会漏掉一处——而漏掉的那一处症状很安静：
// **失效的歌照旧进队列，照旧白跑一趟取流**（每一首都等一次超时）。
//
// ⚠️ **所以判据只有一处**：这里是队列入口的唯一实现，界面层和 app 层都调它。
package app

import "github.com/ShellMonster/PudMusic/internal/service/bili"

// QueueItemsOfMedias 把一批 B 站内容翻成队列项，**放不了的那种滤掉**。
//
// # 为什么必须「滤掉」而不是「放进去再报错」
//
// 一个收藏夹里躺着两个合集就让「播放整个收藏夹」整条失败，是用户没法接受的。
// 同理：一个失效的条目（稿件被删了）放进去之后，应用要为它白跑一趟带
// WBI 签名的网络往返（实测 200~400ms），而那一趟**注定失败**——用户等的
// 就是这一串串超时。上游是**取流之前就滤掉**的
// （`_research/biu/src/common/utils/fav.ts:55` 的
// `.filter(item => [2, 12].includes(item.type) && item.attr === 0)`）。
//
// # 滤掉的是两档，各有各的理由
//
//	没有音频流（合集 / 电影）	`bili.Media.Playable`
//	已经失效（稿件没了）	`bili.Media.Invalid`
//
// ⚠️ **两者都要滤，但理由不同**——所以是两个判据，不是一个。合成一个的话，
// 界面上的说明只能取一句，而另一句就成了假话（用户照着做的下一步不一样）。
//
// # 它不排重
//
// 排重是**另一件事**（B 站翻页会重叠，同一个 bvid 落进 loaded 两次），
// 而它在界面层那一处（`ui.playAllItems`）——那里还要报「跳过了几首」。
// 这一层是「能不能放」这一件事的唯一出处。
func QueueItemsOfMedias(medias []bili.Media) []Item {
	items, _ := QueueItemsOfMediasCounting(medias)
	return items
}

// QueueItemsOfMediasCounting 是上面那一个，**外加跳过了几首**。
//
// # 为什么要有这个变体
//
// ⚠️ **跳过了不说出来，用户数不出**：「这一页有 30 首，而队列里只换了 25 首」
// ——那 5 首去哪了？界面上一个字的解释都没有。而这一层滤掉的**每一首**都是
// 用户看得见的（列表里画着，标着「已失效」），所以用户一定会数。
//
// 界面层那边本来就有这个机制（`ui.playAllItems` 的第二个返回值），而这一层
// 滤掉的和它滤掉的**不是同一批**（那边滤的是「排过重的」和「放不了的」，
// 这边滤的是「放不了的」和「失效的」）——所以**这一层也要报自己那一份**，
// 两边相加才是用户看到的真相。
//
// ⚠️ **报的是「这一层滤掉几首」**，不是「这一页有几首放不了」——两个口径
// 不一样（那一层还排重），混成一句会数错。
func QueueItemsOfMediasCounting(medias []bili.Media) (items []Item, skipped int) {
	items = make([]Item, 0, len(medias))
	for _, m := range medias {
		if !m.Playable() || m.Invalid() {
			skipped++
			continue
		}
		items = append(items, ItemOfMedia(m))
	}
	return items, skipped
}
