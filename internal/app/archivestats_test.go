// 播放条上那三枚键的读数（票 #701）。
//
// # 这一组断的是「那一趟回包里的三个数都到界面层了」
//
// 三个数在**同一个回包**里（`/x/web-interface/view` 的 `stat`），而播放条上
// 三枚键各要一个——所以零新增网络往返是这一票的第一条要求。
//
// ⚠️ **零新增往返**：那一趟（稿件详情）本来就要发（票 #682 的评论数就在里头），
// 而这一票只是**顺带**多读两栏 JSON。
//
// # 这一组全走替身
//
// `commentsArchiveFetch` 是那一趟的替身，一个真请求都不发（AGENTS.md §6）。
package app

import (
	"context"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// TestTheThreeCountsArriveOnTheSameTrip 钉住**三个数在**同一趟**里到齐**。
//
// 🔴 **这是「零新增网络往返」那一条的落点**：三个数分三次问就是「一次能拿到的
// 东西问了 B 站三次」——而那一趟本来就要发（收藏要 aid、票 #682 要评论数）。
func TestTheThreeCountsArriveOnTheSameTrip(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	trips := &archiveTrips{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		trips.add(bvid)
		return bili.ArchiveStats{
			Aid: 170001, Reply: 1286, Favorite: 2468, Like: 9999,
		}, nil
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1xx411c7mD", Cid: "1"})
	a.queue.selectAt(0)

	// 第一帧：那一趟刚发出去，还没回来 ⇒ 三个数都是「没有」。
	if got := a.ArchiveCounts(); got != (ArchiveCounts{}) {
		t.Errorf("还没拿到就报了 %+v——那会让用户以为这一首就是这么多", got)
	}
	waitCommentsFor(t, commentsWaitTimeout, "三个数到了", func() bool {
		c := a.ArchiveCounts()
		return c.Reply == 1286 && c.Favorite == 2468 && c.Like == 9999
	})

	// 🔴 **一趟**：三个数都在这一趟里，不许因为多要两个数就多发两趟。
	if n := trips.count(); n != 1 {
		t.Errorf("发了 %d 趟，want 1——三个数在同一个回包里", n)
	}
	// `CommentCount()` 是同一个数的另一个出口（票 #682 那一族）。
	if got := a.CommentCount(); got != 1286 {
		t.Errorf("CommentCount() 是 %d，want 1286", got)
	}
}

// TestTheThreeCountsDoNotCarryOverToTheNextSong 钉住**换歌不串数**。
//
// 🔴 **上一首的三个数不许挂到下一首上**：换歌那一刻**立刻**是「没有」（不是 0
// 也不是旧数），等下一首自己那一趟回来才有值。
//
// ⚠️ **判据是「立刻」而不是「最终」**：最终值对了，而中间那一帧挂着上一首的
// 数——用户会读到「这一首的赞数怎么和刚才那首一样」。
func TestTheThreeCountsDoNotCarryOverToTheNextSong(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	trips := &archiveTrips{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		trips.add(bvid)
		switch bvid {
		case "BVfirst000000000":
			return bili.ArchiveStats{Aid: 1, Reply: 111, Favorite: 222, Like: 333}, nil
		case "BVsecond00000000":
			return bili.ArchiveStats{Aid: 2, Reply: 444, Favorite: 555, Like: 666}, nil
		}
		return bili.ArchiveStats{}, nil
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BVfirst000000000", Cid: "1"})
	a.queue.selectAt(0)
	waitCommentsFor(t, commentsWaitTimeout, "第一首的数到了", func() bool {
		c := a.ArchiveCounts()
		return c.Reply == 111 && c.Favorite == 222 && c.Like == 333
	})

	// 换歌。
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BVsecond00000000", Cid: "1"})
	a.queue.selectAt(1)

	// ⚠️ **换歌那一刻**：立刻是「没有」。
	if got := a.ArchiveCounts(); got != (ArchiveCounts{}) {
		t.Errorf("换歌之后还挂着上一首的数 %+v", got)
	}
	waitCommentsFor(t, commentsWaitTimeout, "第二首的数到了", func() bool {
		c := a.ArchiveCounts()
		return c.Reply == 444 && c.Favorite == 555 && c.Like == 666
	})
}

// TestTheThreeCountsAreZeroForALocalFile 钉住**本地文件三个数都是「没有」**。
//
// 🔴 **本地文件在 B 站上没有对应的对象**：那一趟压根不该发——发出去是「问一个
// 认不出来的 oid」，而症状是「静默拿到空列表」。
//
// ⚠️ **判据分两层**：数是「没有」，而且**一个请求都不发**。
func TestTheThreeCountsAreZeroForALocalFile(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	trips := &archiveTrips{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		trips.add(bvid)
		return bili.ArchiveStats{Aid: 1, Reply: 1, Favorite: 1, Like: 1}, nil
	}
	a.Queue().Append(Item{Source: "local", Type: "local", Path: "/tmp/x.mp3", Title: "本地的一首"})
	a.queue.selectAt(0)

	if got := a.ArchiveCounts(); got != (ArchiveCounts{}) {
		t.Errorf("本地文件报出了 %+v", got)
	}
	if n := trips.count(); n != 0 {
		t.Errorf("本地文件发了 %d 趟请求——它该一个都不发", n)
	}
}

// TestTheThreeCountsStayZeroWhenTheTripFails 钉住**那一趟失败时三个数都是
// 「没有」**，而不是 0 冒充「零赞」。
//
// ⚠️ **失败之后隔 `commentsRetryAfter` 再问**：界面每一帧都问一次，不记退避的
// 话一次断网就是每秒一趟（`ensureCommentStats` 的注释写着这一条）。
func TestTheThreeCountsStayZeroWhenTheTripFails(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	trips := &archiveTrips{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		trips.add(bvid)
		return bili.ArchiveStats{}, errCommentsUnavailable
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1xx411c7mD", Cid: "1"})
	a.queue.selectAt(0)

	// ⚠️ **那一趟是界面问出来的**（`ensureCommentStats` 由 `ArchiveCounts()`
	// 触发——铁律三：view 里不发网络请求）。所以这里先问一次，那一趟才发出去。
	a.ArchiveCounts()
	waitCommentsFor(t, commentsWaitTimeout, "那一趟发出去了", func() bool {
		return trips.count() >= 1
	})
	if got := a.ArchiveCounts(); got != (ArchiveCounts{}) {
		t.Errorf("失败之后报出了 %+v——那是「零赞零收藏零评论」，不是「没有这个数」", got)
	}
}
