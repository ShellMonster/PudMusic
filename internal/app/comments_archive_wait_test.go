// 「bvid → 稿件详情」那一趟的等待者（票 #682 的对抗性审查）。
//
// # 两条路会同时要 aid
//
//	播放条那一半（`ensureCommentStats`）  要 `stat.reply`（评论数），不认盘
//	评论页那一半（`commentArchiveID`）    要 aid，**盘上那份只有 aid 的索引算答案**
//
// 两半走同一张在途表（同一首歌只问 B 站一次），所以后到的那一半会挂在前一半上
// 等。🔴 **评论页那一半不该为一个本地就有答案的东西去等网络那一趟**：盘上有 aid
// 的时候它压根不必排队，而排队之后醒来只判 `commentsStatsKnown`（两栏都问过）
// 会让它跟着播放条那一趟一起报「评论暂时打不开」。
//
// ⚠️ 这一组全走替身 + 本地临时目录，一个真请求都不发（AGENTS.md §6）。
package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/service/comments"
)

// TestCommentsPageOpensFromDiskWhileThePlaybarTripFails 钉住**评论页不被播放条
// 那一趟拖死**。
//
// 🔴 场景（审查者复现的那一条）：盘上明明有 aid 索引，而播放条那一趟（不认盘）
// 正在途——它断网失败了。评论页挂到它身上等，醒来只判 `commentsStatsKnown`，于是
// 跟着报「评论暂时打不开」，尽管本地就有开页需要的那个 aid。
//
// ⚠️ **确定性靠「不放行」**：播放条那一趟在自己的替身里发一个「已经在途」的
// 信号（`started`），测试等到那个信号之后才点开评论页，而**那一趟在评论请求
// 发出来之前一直不返回**。修复之前评论页注定挂在那张在途表上（坑是在起 goroutine
// 之前就占下的，`started` 一到就说明它在了）⇒ 评论请求永远发不出去 ⇒ 必红；
// 修复之后评论页读盘就拿到 aid、不进等待 ⇒ 必绿。**不靠 sleep 撞时机**。
func TestCommentsPageOpensFromDiskWhileThePlaybarTripFails(t *testing.T) {
	dir := t.TempDir()
	c := comments.NewCache(dir)
	if err := c.SaveAVID("BVdisk", 4242); err != nil {
		t.Fatal(err)
	}
	a := New()
	t.Cleanup(a.Close)
	a.MountComments(c)

	started := make(chan struct{})
	// 永远不放行：播放条那一趟卡在途上（它的失败方式不重要——评论页根本不该
	// 受它影响）。收尾时再放，好让那条 goroutine 干净退出。
	release := make(chan struct{})
	var once sync.Once
	a.commentsArchiveFetch = func(_ context.Context, _ string) (bili.ArchiveStats, error) {
		once.Do(func() { close(started) })
		<-release
		return bili.ArchiveStats{}, errors.New("网络没通")
	}
	listOIDs := make(chan int64, 4)
	a.commentsListFetch = func(_ context.Context, _ int64, oid int64, _ int, _ string) (bili.CommentPage, error) {
		listOIDs <- oid
		return commentPage(nil, "", true), nil
	}
	t.Cleanup(func() { close(release) })
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BVdisk", Cid: "1"})
	a.queue.selectAt(0)

	// 播放条那一趟先发出去（用户在听歌，播放条每一帧都在问）——它卡在途上。
	a.CommentCount()
	select {
	case <-started:
	case <-time.After(commentsWaitTimeout):
		t.Fatal("播放条那一趟没发出去")
	}

	// 用户这时候点开评论页。
	a.EnsureComments()

	select {
	case oid := <-listOIDs:
		if oid != 4242 {
			t.Errorf("评论请求的 oid 是 %d，want 4242（盘上那个 aid）", oid)
		}
	case <-time.After(commentsWaitTimeout):
		t.Fatalf("盘上有 aid 索引、播放条那一趟还没回来，评论页却打不开"+
			"（CommentsErr=%q）——它挂在那张在途表上等网络，而答案在本地", a.CommentsErr())
	}
	waitCommentsFor(t, commentsWaitTimeout, "评论页那一趟走完", func() bool {
		return a.CommentsLoaded()
	})
	if err := a.CommentsErr(); err != nil {
		t.Errorf("评论页报错了：%v", err)
	}
}

// TestBothCommentPageWaitersGetTheAIDFromTheDisk 钉住**两个评论页等待者都拿到
// aid**（谁先谁后都一样）。
//
// 🔴 审查者说的第二条路：等待者醒来只判 `commentsStatsKnown`。那一格是「两栏都
// 真的问过」——盘命中（只写 aid）和播放条那一趟失败都不会置它，于是**明明 aid
// 就在手边**，等待者却报「评论暂时打不开」。
//
// ⚠️ **确定性**：播放条那一趟永远不返回（不靠 sleep），两个等待者同时向**同一个
// bvid** 要 aid；修复前两条都卡死（红），修复后两条都立刻从盘上拿到 4242（绿）。
func TestBothCommentPageWaitersGetTheAIDFromTheDisk(t *testing.T) {
	dir := t.TempDir()
	c := comments.NewCache(dir)
	if err := c.SaveAVID("BVdisk", 4242); err != nil {
		t.Fatal(err)
	}
	a := New()
	t.Cleanup(a.Close)
	a.MountComments(c)

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var trips int32
	a.commentsArchiveFetch = func(_ context.Context, _ string) (bili.ArchiveStats, error) {
		atomic.AddInt32(&trips, 1)
		once.Do(func() { close(started) })
		<-release
		return bili.ArchiveStats{}, errors.New("网络没通")
	}
	t.Cleanup(func() { close(release) })

	// 播放条那一趟先发出去，卡在途上。
	a.ensureCommentStats("BVdisk")
	select {
	case <-started:
	case <-time.After(commentsWaitTimeout):
		t.Fatal("播放条那一趟没发出去")
	}

	type answer struct {
		st  bili.ArchiveStats
		err error
	}
	results := make(chan answer, 2)
	for range 2 {
		go func() {
			st, err := a.archiveStats(context.Background(), "BVdisk", nil, true)
			results <- answer{st, err}
		}()
	}

	for i := range 2 {
		select {
		case got := <-results:
			if got.err != nil {
				t.Errorf("第 %d 个等待者报错了：%v——盘上就有 aid", i+1, got.err)
			}
			if got.st.Aid != 4242 {
				t.Errorf("第 %d 个等待者拿到的 aid 是 %d，want 4242", i+1, got.st.Aid)
			}
		case <-time.After(commentsWaitTimeout):
			t.Fatalf("第 %d 个等待者没醒过来——它在为一个本地就有答案的东西等网络", i+1)
		}
	}
	// 两条评论页的路都不该另发一趟：B 站那一趟只有播放条那条（它卡着）。
	if got := atomic.LoadInt32(&trips); got != 1 {
		t.Errorf("稿件详情发了 %d 趟，want 1（评论页的两条都读盘）", got)
	}
}

// TestTheCommentsWaitersReadTheDiskThatAppearedWhileTheTripWasInFlight 钉住
// **等待者进门时读盘**（而不是排到在途那一趟后面）。
//
// 🔴 这一条和上一条是同一件事的两个时点：盘上的索引**在那一趟发出去之后**才有
// （比如另一个进程/上一次运行刚写下它）。修复之前「内存里没有 aid」就直接排到
// 在途表上，而那一趟是播放条那一趟（它要等网络、断了就失败）；修复之后进门先读
// 盘，读到就当场返回。
//
// ⚠️ **确定性**：缓存是在「那一趟已经在途」之后挂上的（`started` 已到），所以
// 两个等待者进门时**盘上确实有 aid**，而内存里**确实没有**——修复前必红。
func TestTheCommentsWaitersReadTheDiskThatAppearedWhileTheTripWasInFlight(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	a.commentsArchiveFetch = func(_ context.Context, _ string) (bili.ArchiveStats, error) {
		once.Do(func() { close(started) })
		<-release
		return bili.ArchiveStats{}, errors.New("网络没通")
	}
	t.Cleanup(func() { close(release) })

	// 播放条那一趟先发出去（它不读盘），卡在途上。
	a.ensureCommentStats("BVdisk")
	select {
	case <-started:
	case <-time.After(commentsWaitTimeout):
		t.Fatal("播放条那一趟没发出去")
	}

	// 现在盘上有了那一份索引（在途的那一趟还什么都没记）。
	dir := t.TempDir()
	c := comments.NewCache(dir)
	if err := c.SaveAVID("BVdisk", 4242); err != nil {
		t.Fatal(err)
	}
	a.MountComments(c)

	type answer struct {
		st  bili.ArchiveStats
		err error
	}
	results := make(chan answer, 2)
	for range 2 {
		go func() {
			st, err := a.archiveStats(context.Background(), "BVdisk", nil, true)
			results <- answer{st, err}
		}()
	}
	for i := range 2 {
		select {
		case got := <-results:
			if got.err != nil {
				t.Errorf("第 %d 个等待者报错了：%v——盘上就有 aid", i+1, got.err)
			}
			if got.st.Aid != 4242 {
				t.Errorf("第 %d 个等待者拿到的 aid 是 %d，want 4242", i+1, got.st.Aid)
			}
		case <-time.After(commentsWaitTimeout):
			t.Fatalf("第 %d 个等待者没回来——它排在播放条那一趟后面等网络，"+
				"而盘上就有答案", i+1)
		}
	}
}

// TestBothWaitersGetTheAIDWhenTheTripSucceeds 钉住**一趟成功回来、两个等待者都
// 拿到 aid**：这条是上一条的对照——那一趟真的问回来了（两栏都有），等待者不该
// 各发一趟，也不该拿不到东西。
func TestBothWaitersGetTheAIDWhenTheTripSucceeds(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var trips int32
	a.commentsArchiveFetch = func(_ context.Context, _ string) (bili.ArchiveStats, error) {
		atomic.AddInt32(&trips, 1)
		once.Do(func() { close(started) })
		<-release
		return bili.ArchiveStats{Aid: 4242, Reply: 55}, nil
	}

	// 播放条那一趟在途（它不认盘，盘也没有）。
	a.ensureCommentStats("BV1")
	select {
	case <-started:
	case <-time.After(commentsWaitTimeout):
		t.Fatal("播放条那一趟没发出去")
	}

	type answer struct {
		st  bili.ArchiveStats
		err error
	}
	results := make(chan answer, 2)
	for range 2 {
		go func() {
			st, err := a.archiveStats(context.Background(), "BV1", nil, true)
			results <- answer{st, err}
		}()
	}

	close(release)
	for i := range 2 {
		select {
		case got := <-results:
			if got.err != nil {
				t.Errorf("第 %d 个等待者报错了：%v——那一趟是成功回来的", i+1, got.err)
			}
			if got.st.Aid != 4242 {
				t.Errorf("第 %d 个等待者拿到的 aid 是 %d，want 4242", i+1, got.st.Aid)
			}
		case <-time.After(commentsWaitTimeout):
			t.Fatalf("第 %d 个等待者没醒过来", i+1)
		}
	}
	if got := atomic.LoadInt32(&trips); got != 1 {
		t.Errorf("稿件详情发了 %d 趟，want 1——三个调用者共用同一趟", got)
	}
}
