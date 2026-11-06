// 「本周单曲循环」点一首歌时，把统计里的聚合键**拆回**一项。
//
// 统计按 `Item.Identity()` 那份口径聚合（`mv:<bvid>|<cid>` / `audio:<sid>` /
// `local:<路径>`，见 `statsIdentity.key`），所以这里的拆法就是 `trackKey`
// 那一份口径的**逆**——和 `itemOfPlaybackEntry`（恢复队列那一趟）是同一件
// 事在另一个入口。
//
// ⚠️ **拆不出来的键不许放进队列**：那种项连身份都认不出（`Identity()` 是
// 空串），入队就是一条永远放不了的死项——不如当场说「这一条放不了」。
package app

import "strings"

// errStatsKeyUnreadable 是「这条听歌记录认不出是哪一首」。
//
// ⚠️ **它是一个 `Unplayable`**（`AsUnplayable` 认得出）：界面那一层不用
// 区分「拆键失败」和「这一条本来就不能放」，按下去都是同一件事——
// 说一句放不了。
var errStatsKeyUnreadable = &Unplayable{
	Kind: UnplayableNoArchive,
	Why:  "这条听歌记录认不出是哪一首歌",
}

// ItemOfStatsKey 把「本周单曲循环」那一行的聚合键变回一项。
//
// `snap` 是这一首的快照（`ListeningStats.Tracks[key]`，可能为 nil——按天
// 存的明细里有键、全局聚合里没有是合法的）：歌名 / UP 名照快照填，
// `LocalItem` 那份文件名只是兜底。
//
// ⚠️ **它不管这一项现在放不放得了**（失效 / 网络断都是按下之后才判得出
// 的事），那道闸在 `EnqueueAndPlayNow` 里——这里只保证「拆出来的项身份
// 和键说的是同一首」。
func ItemOfStatsKey(key string, snap *StatsTrackStat) (Item, error) {
	var it Item
	switch {
	case strings.HasPrefix(key, playbackIDMV):
		bvid, cid := splitMVIdentity(strings.TrimPrefix(key, playbackIDMV))
		if bvid == "" {
			return Item{}, errStatsKeyUnreadable
		}
		it = Item{Source: SourceOnline, Type: "mv", Bvid: bvid, Cid: cid}
	case strings.HasPrefix(key, playbackIDAudio):
		sid := strings.TrimPrefix(key, playbackIDAudio)
		if sid == "" {
			return Item{}, errStatsKeyUnreadable
		}
		it = Item{Source: SourceOnline, Type: "audio", Sid: sid}
	case strings.HasPrefix(key, playbackIDLocal):
		path := strings.TrimPrefix(key, playbackIDLocal)
		if path == "" {
			return Item{}, errStatsKeyUnreadable
		}
		it = LocalItem(path)
	default:
		return Item{}, errStatsKeyUnreadable
	}
	if snap != nil {
		// ⚠️ **歌名优先用快照**：那一格写的是「听的时候」的名字
		// （见 `StatsTrackStat.Title` 的注释），用户认的也是它。
		if snap.Title != "" {
			it.Title = snap.Title
		}
		it.Artist = snap.Artist
		it.ArtistMid = snap.ArtistMid
	}
	return it, nil
}
