// 投稿的章节：进度条上那些点的数据（票 #585）。
//
// 数据来自 `/x/player/wbi/v2` 的 `view_points`（`bili.Client.Chapters`），只有
// 投稿条目有（`Item.Bvid` 和 `Item.Cid` 都不空的那一路）；音乐库音频的时长和
// 背后投稿对不上，本地文件更没有——都不问。
//
// 这一套的形状和分集（`fullplayer.go` 的 `ensureParts`）是同一个：**界面每一帧
// 调 getter，getter 顺手把没问过的那一趟问出去**；问回来了叫醒界面（铁律三），
// 换一首歌就忘掉。差别只有两个：
//
//   - 键是 **bvid+cid** 而不是 bvid：章节是按集给的，多 P 的两集各是各的。
//   - **失败是静默的**：分集问不出界面上有一句「问不出分集」，章节问不出就
//     只是没有点——点不点是锦上添花的事，为它弹一句错、闪一下提示，比
//     「这一条没有章节」吵得多。失败照样退避（`chaptersRetryAfter`），隔一会儿
//     界面下一帧再问一次。
package app

import (
	"context"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// Chapter 是进度条上的一个章节点。
type Chapter struct {
	// At 是起点（点画的地方，也是吸附落下去的地方）。
	At time.Duration
	// End 是终点。下一章的 At 和它通常是同一个时刻；界面上现在用不到它，
	// 留着是因为「这一章多长」迟早要在悬停里说（悬停提示那一票）。
	End time.Duration
	// Title 是章节名（B 站叫 content），不保证给。
	Title string
}

// chaptersRetryAfter 是章节问失败之后隔多久再问一次。和分集那边的
// `partsRetryAfter` 是同一个量级（网络抖动能缓过来、用户还盯着的那一会儿）。
const chaptersRetryAfter = 15 * time.Second

// chaptersForSnapshotLocked 是 `Snapshot` 里那一步（**在 a.mu 里调**）：把当前
// 这一首的章节放进 State，顺手把没问过的那一趟问出去。
//
// **返回的是缓存**：第一次的那一趟还没回来时给的是 nil（界面上就是没有点），
// 问回来了会叫醒界面，下一帧的 Snapshot 就有了。按起点排好序。
//
// ⚠️ **问章节的 client 由调用方递进来，不能在这里拿 `streamingSnapshot`**：那把
// 锁是 `a.mu`，而此刻它已经在手里（sync.Mutex 不可重入，照直写是当场死锁——
// 第一次跑整条链就锁死在 `Snapshot` 上了）。
//
// 空列表 = 没有章节（不是错误，也不重试——问过就是没有）。
func (a *App) chaptersForSnapshotLocked() []Chapter {
	it, ok := a.queue.Current()
	if !ok || it.Bvid == "" || it.Cid == "" {
		return nil
	}
	a.ensureChapters(it.Bvid, it.Cid, a.chapterFetchLocked())
	a.chaptersMu.Lock()
	defer a.chaptersMu.Unlock()
	if len(a.chapters) == 0 {
		return nil
	}
	return append([]Chapter(nil), a.chapters...)
}

// chapterFetchLocked 把问章节的那一趟解析出来（**在 a.mu 里调**）。nil = 取流
// 零件还没挂上（`MountStreaming` 还没跑）。
func (a *App) chapterFetchLocked() func(context.Context, string, string) ([]bili.Chapter, error) {
	if a.chaptersFetch != nil {
		return a.chaptersFetch
	}
	if a.streaming == nil || a.streaming.Client == nil {
		return nil
	}
	return a.streaming.Client.Chapters
}

// ensureChapters 保证正在问 / 已经问过 bvid+cid 的章节。
//
// 换一集就重置：`chaptersKey` 是这份缓存属于谁，迟到的结果回来发现对不上
// 直接丢（和分集那边「换稿丢弃」是同一个道理）。
//
// 🔴 **只有成功才算问过**（`chaptersLoaded`）：失败放下 `chaptersWanted`、
// 记下时刻，隔 `chaptersRetryAfter` 之后界面下一帧再问一次——不然一次网络
// 抖动之后这一集到播完都没有点。**空结果也是成功**：问过了就是没有章节，
// 不该再来第二趟。
//
// ⚠️ **fetch 是调用方递进来的**（在 a.mu 里解析的，见 chaptersForSnapshotLocked）。
// fetch 为 nil 的时候**不算问过**：零件没挂上是暂时的，记下时刻、隔退避窗口
// 再来——但**不能在这里走 finishChapters**，它要拿 a.mu（死锁，同上）。
func (a *App) ensureChapters(bvid, cid string, fetch func(context.Context, string, string) ([]bili.Chapter, error)) {
	key := bvid + "\x1f" + cid
	a.chaptersMu.Lock()
	if a.chaptersKey != key {
		a.chaptersKey, a.chapters = key, nil
		a.chaptersLoaded, a.chaptersWanted = false, false
		a.chaptersTriedAt = time.Time{}
	}
	if a.chaptersLoaded || a.chaptersWanted {
		a.chaptersMu.Unlock()
		return
	}
	// 退避：界面每一帧都进 Snapshot，不等一下的话失败一次就是每秒一发。
	if !a.chaptersTriedAt.IsZero() && time.Since(a.chaptersTriedAt) < chaptersRetryAfter {
		a.chaptersMu.Unlock()
		return
	}
	a.chaptersWanted = true
	a.chaptersTriedAt = time.Now()
	a.chaptersMu.Unlock()

	if fetch == nil {
		return
	}
	go func() {
		items, err := fetch(context.Background(), bvid, cid)
		a.finishChapters(bvid, cid, items, err)
	}()
}

// finishChapters 记下一次取章节的结果并叫醒界面。
//
// ⚠️ **只有成功才 `chaptersLoaded = true`**（理由见 ensureChapters）。
func (a *App) finishChapters(bvid, cid string, items []bili.Chapter, err error) {
	a.chaptersMu.Lock()
	if key := bvid + "\x1f" + cid; a.chaptersKey == key {
		if err != nil {
			a.chapters = nil
		} else {
			a.chapters = make([]Chapter, 0, len(items))
			for _, ch := range items {
				a.chapters = append(a.chapters, Chapter{At: ch.From, End: ch.To, Title: ch.Title})
			}
		}
		a.chaptersLoaded = err == nil
		a.chaptersWanted = false
	}
	a.chaptersMu.Unlock()

	a.mu.Lock()
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
	}
}
