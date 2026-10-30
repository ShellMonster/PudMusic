// 播放条上那枚「评论」键要显示的数字（票 #682）。
//
// # 它和 `CommentsAllCount()` 不是一回事
//
// `CommentsAllCount()` 是**评论页那一趟**带回来的总数——只有用户打开过评论页、
// 拿到过第一页之后才有值。而播放条上那一枚在**打开评论页之前**就要显示数字，
// 所以这个数有自己的一趟：`bili.ArchiveStats`（稿件详情的 `stat.reply`），
// 和收藏要的 avid 是**同一个回包**。
//
// # 这一组全走替身
//
// `commentsArchiveFetch` 是那一趟的替身，一个真请求都不发（AGENTS.md §6）。
package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// archiveTrips 数「稿件详情那一趟」被叫了几回、问的是哪个 bvid。
type archiveTrips struct {
	mu   sync.Mutex
	seen []string
}

func (c *archiveTrips) add(bvid string) {
	c.mu.Lock()
	c.seen = append(c.seen, bvid)
	c.mu.Unlock()
}

func (c *archiveTrips) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.seen)
}

// TestTheCommentCountArrivesOnItsOwn 钉住**播放条不等评论页**。
//
// 🔴 这一条是票 #682 的主体：没打开过评论页时 `CommentsAllCount()` 是 0，
// 而播放条上那枚键要显示的数字**必须**自己拿到——走稿件详情的 `stat.reply`。
//
// ⚠️ **在途的时候不许重复发**：界面每一帧都问一次，而每一帧发一趟就是每秒
// 60 个请求（和 `TestCommentsAreNotAskedTwice` 同一条理由）。
func TestTheCommentCountArrivesOnItsOwn(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	trips := &archiveTrips{}
	release := make(chan struct{})
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		trips.add(bvid)
		<-release
		return bili.ArchiveStats{Aid: 170001, Reply: 1286}, nil
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1xx411c7mD", Cid: "1"})
	a.queue.selectAt(0)

	// 第一帧问一次：那一趟发出去、还没回来 ⇒ 0（「还不知道」）。
	if got := a.CommentCount(); got != 0 {
		t.Errorf("还没拿到就报了 %d 条评论——那会让用户以为这一首就是这么多", got)
	}
	waitCommentsFor(t, commentsWaitTimeout, "稿件详情那一趟发出去了", func() bool {
		return trips.count() >= 1
	})
	trips.mu.Lock()
	got := trips.seen[0]
	trips.mu.Unlock()
	if got != "BV1xx411c7mD" {
		t.Errorf("那一趟问的是 %q，want bvid", got)
	}

	// 在途的时候问 50 次（界面每一帧问一次）：还是 1 趟。
	for range 50 {
		if n := a.CommentCount(); n != 0 {
			t.Errorf("在途的时候报了 %d 条评论", n)
		}
	}
	if n := trips.count(); n != 1 {
		t.Errorf("在途的时候发了 %d 趟，want 1", n)
	}

	close(release)
	waitCommentsFor(t, commentsWaitTimeout, "评论数到了", func() bool {
		return a.CommentCount() == 1286
	})
}

// TestTheCommentCountDoesNotCarryOverToTheNextSong 钉住**换歌不串数**。
//
// 🔴 判据和 `CommentsSlot` 那一套一致：上一首的评论数**不许**挂到下一首上。
// 换歌那一刻**立刻**是 0（「还不知道」），等下一首自己那一趟回来才有值。
func TestTheCommentCountDoesNotCarryOverToTheNextSong(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	trips := &archiveTrips{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		trips.add(bvid)
		switch bvid {
		case "BVfirst":
			return bili.ArchiveStats{Aid: 1, Reply: 111}, nil
		case "BVsecond":
			return bili.ArchiveStats{Aid: 2, Reply: 222}, nil
		}
		return bili.ArchiveStats{}, errors.New("哪一首？")
	}
	a.Queue().Append(
		Item{Source: "online", Type: "mv", Bvid: "BVfirst", Cid: "1"},
		Item{Source: "online", Type: "mv", Bvid: "BVsecond", Cid: "1"},
	)
	a.queue.selectAt(0)
	waitCommentsFor(t, commentsWaitTimeout, "第一首的评论数到了", func() bool {
		return a.CommentCount() == 111
	})

	a.queue.selectAt(1)
	if got := a.CommentCount(); got != 0 {
		t.Errorf("刚换歌就报了 %d 条评论——那是上一首的（换歌那一刻应当是「还不知道」）", got)
	}
	waitCommentsFor(t, commentsWaitTimeout, "第二首的评论数到了", func() bool {
		return a.CommentCount() == 222
	})

	// 换回去：第一首的答案在内存里，**不用再问一趟**。
	before := trips.count()
	a.queue.selectAt(0)
	waitCommentsFor(t, commentsWaitTimeout, "换回第一首", func() bool {
		return a.CommentCount() == 111
	})
	if got := trips.count(); got > before {
		t.Errorf("换回第一首又问了一趟（多出来 %d 趟）——同一首的答案该记在内存里", got-before)
	}
}

// TestTheCommentCountIsZeroForALocalFile 钉住**本地文件不显示数字、也不发请求**。
//
// ⚠️ 本地音乐在 B 站上没有对应的对象（`CommentsWorthAsking` 也是这一条），
// 一个数都问不出来——而问一个不存在的 bvid 的代价是后台白跑一趟。
func TestTheCommentCountIsZeroForALocalFile(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	asked := make(chan struct{}, 1)
	a.commentsArchiveFetch = func(_ context.Context, _ string) (bili.ArchiveStats, error) {
		select {
		case asked <- struct{}{}:
		default:
		}
		return bili.ArchiveStats{Aid: 1, Reply: 999}, nil
	}
	a.Queue().Append(LocalItem("/tmp/本地的一首.mp3"))
	a.queue.selectAt(0)

	if got := a.CommentCount(); got != 0 {
		t.Errorf("本地文件报了 %d 条评论", got)
	}
	select {
	case <-asked:
		t.Error("本地文件问了一趟稿件详情——它连 bvid 都没有")
	case <-time.After(50 * time.Millisecond):
	}
}

// TestTheCommentCountSharesTheArchiveTripWithTheCommentsPage 钉住**两边不各发一趟**。
//
// 🔴 评论页要的是同一个回包里的 `aid`（`commentArchiveID`），而播放条要的是
// `stat.reply`——两件事在**同一趟**里。谁先问都行，后一个必须吃前一个的结果
// （内存缓存），而不是再问一遍 B 站。
//
// ⚠️ **两档都要断**：先播放条后评论页（下面这一条），以及反过来——
// 反过来那一半在 `TestTheCommentsPageTripFeedsTheCommentCount`。
func TestTheCommentCountSharesTheArchiveTripWithTheCommentsPage(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	trips := &archiveTrips{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		trips.add(bvid)
		return bili.ArchiveStats{Aid: 4242, Reply: 77}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		return commentPage(nil, "", true), nil
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)

	// 先让播放条那一趟拿到数（它顺带把 aid 记进了内存）。
	waitCommentsFor(t, commentsWaitTimeout, "评论数到了", func() bool {
		return a.CommentCount() == 77
	})
	if got := trips.count(); got != 1 {
		t.Fatalf("稿件详情发了 %d 趟，want 1", got)
	}

	// 现在进评论页那一趟：它该直接用内存里那个 aid。
	a.EnsureComments()
	waitCommentsFor(t, commentsWaitTimeout, "评论页那一趟走完", func() bool {
		return a.CommentsLoaded()
	})
	if got := trips.count(); got != 1 {
		t.Errorf("评论页又发了一趟稿件详情（总共 %d 趟）——它该吃缓存", got)
	}
}

// TestTheCommentsPageTripFeedsTheCommentCount 是上一条的**反过来**那一半：
// 评论页先问，播放条该白拿那个数字。
//
// ⚠️ **这一半不能省**：两边各自记一份「问到过没有」的话，先问的那一边
// （评论页）不会把数字交给播放条，而播放条会**再发一趟**——用户只是打开了一下
// 评论页，同一首的稿件详情就被问了两次。
func TestTheCommentsPageTripFeedsTheCommentCount(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	trips := &archiveTrips{}
	release := make(chan struct{})
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		trips.add(bvid)
		<-release
		return bili.ArchiveStats{Aid: 4242, Reply: 55}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		return commentPage(nil, "", true), nil
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)

	a.EnsureComments()
	waitCommentsFor(t, commentsWaitTimeout, "评论页那一趟发出去了", func() bool {
		return trips.count() >= 1
	})

	// 那一趟还在路上的时候播放条也来问：它该**等**那一趟，而不是自己再发一趟。
	for range 20 {
		a.CommentCount()
	}
	close(release)
	waitCommentsFor(t, commentsWaitTimeout, "评论数到了", func() bool {
		return a.CommentCount() == 55
	})
	if got := trips.count(); got != 1 {
		t.Errorf("稿件详情发了 %d 趟，want 1——播放条该吃评论页那一趟的结果", got)
	}
}

// TestTheAIDIndexDoesNotStopTheCommentCount 钉住**那份只有 aid 的索引不挡数字**。
//
// 🔴 盘上的缓存只存了 aid（格式不动，见 `commentArchiveID` 的注释），**没有**
// 评论数——它命中之后 `commentsAvids` 里就有 aid 了。评论页那一半认它（它只要
// aid），播放条那一半**不能认**（它要评论数）：判据必须是 `commentsStatsKnown`
// 而不是「`commentsAvids` 里有这个 bvid」。
//
// ⚠️ **这里直接摆内存里那一格，不走真盘**：走 `MountComments` + `SaveAVID`
// 的话，写盘是那一趟 goroutine 的后半段，测试收尾（删临时目录）会和它抢
// ——红的会是「目录删不掉」，而产品一点问题都没有。这一条要验的是**判据**，
// 盘那一层由 `service/comments` 自己的测试守着。
func TestTheAIDIndexDoesNotStopTheCommentCount(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	trips := &archiveTrips{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		trips.add(bvid)
		return bili.ArchiveStats{Aid: 4242, Reply: 33}, nil
	}
	// 摆出「盘上那份索引命中过」的样子：aid 有、评论数没有。
	a.commentsMu.Lock()
	a.commentsAvids = map[string]int64{"BV1": 4242}
	a.commentsMu.Unlock()

	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		return commentPage(nil, "", true), nil
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)

	// 播放条那条路：那份索引不该让它空手而归。
	waitCommentsFor(t, commentsWaitTimeout, "评论数到了", func() bool {
		return a.CommentCount() == 33
	})
	if got := trips.count(); got != 1 {
		t.Errorf("稿件详情发了 %d 趟，want 1——播放条那一半不该被那份只有 aid 的索引挡回去", got)
	}
}
