package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/service/comments"
)

// 评论这一摊：后台拉、缓存、退避、换歌就重置、以及「mv 要先换 avid」。
//
// 这一组**全走替身**（`commentsListFetch` / `commentsArchiveFetch` / …），
// 一个真请求都不发——不碰真 B 站（AGENTS.md §6）。

// commentCalls 记着问评论那一趟被叫了几回、参数是什么。
type commentCalls struct {
	mu sync.Mutex
	// list 是主列表那几趟的参数：typ / oid / next。
	list []commentCall
	// archive 是 bvid → avid 那几趟。
	archive []string
	// replies 是楼中楼那几趟。
	replies []commentReplyCall
}

type commentCall struct {
	typ int64
	oid int64
	// next 是**偏移量**（空串 = 第一页），不是 `cursor.next`——理由见
	// `bili.CommentPage.NextOffset`。
	next string
}

type commentReplyCall struct {
	typ  int64
	oid  int64
	root int64
}

func (c *commentCalls) addList(typ, oid int64, next string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.list = append(c.list, commentCall{typ: typ, oid: oid, next: next})
}

func (c *commentCalls) addArchive(bvid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.archive = append(c.archive, bvid)
}

func (c *commentCalls) addReply(typ, oid, root int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.replies = append(c.replies, commentReplyCall{typ: typ, oid: oid, root: root})
}

func (c *commentCalls) listCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.list)
}

func (c *commentCalls) archiveCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.archive)
}

func (c *commentCalls) replyCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.replies)
}

func (c *commentCalls) firstList() commentCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.list) == 0 {
		return commentCall{}
	}
	return c.list[0]
}

func (c *commentCalls) firstReply() commentReplyCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.replies) == 0 {
		return commentReplyCall{}
	}
	return c.replies[0]
}

// commentPage 造一页评论。
//
// next 是**偏移量**（空串 = 到底了 / 没有下一页），不是 `cursor.next`。
func commentPage(rs []bili.Comment, next string, isEnd bool) bili.CommentPage {
	return bili.CommentPage{
		Replies:    rs,
		NextOffset: next,
		IsEnd:      isEnd,
		AllCount:   int64(len(rs)),
		UpperMid:   42,
		Mode:       int(bili.CommentSortHot),
		Name:       "热门评论",
	}
}

// waitComments 轮询到问到了 want 条评论。
func waitComments(t *testing.T, a *App, want int, what string) []bili.Comment {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := a.Comments(); len(got) == want {
			return got
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等了 %d 条评论，want %d（%s）", len(a.Comments()), want, what)
	return nil
}

// TestCommentsAreNotAskedTwice 钉住「界面每帧调 `Comments()` 不会发出第二个请求」。
//
// 这是这一摊最要紧的一条：界面每一帧都调它，而每一帧都发一次请求的话，B 站会
// 以为我们在刷——症状是「评论页转圈 forever，而请求数是每秒 60」。
func TestCommentsAreNotAskedTwice(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := &commentCalls{}
	release := make(chan struct{})
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		calls.addArchive(bvid)
		return bili.ArchiveStats{Aid: 12345}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		// ⚠️ **这一趟卡在途**：防重入要防的正是**这一段**——
		// 「问已经发出去了、结果还没回来」的时候界面已经画了好几帧。
		<-release
		return commentPage([]bili.Comment{{Rpid: 1, Message: "第一条"}}, "", true), nil
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)

	// 先等那一趟**真的发出去了**（它卡在途上）。这一步不能省：那一趟是在
	// 后台 goroutine 里跑的，界面那一帧调 `Comments()` 时它可能还没开始——
	// 那时候量到的「0 趟」说明不了任何事。
	a.Comments()
	waitCommentsFor(t, commentsWaitTimeout, "那一趟发出去了", func() bool {
		return calls.listCount() >= 1
	})

	// **在途的时候**问 50 次——**一帧一次**，而界面就是每一帧都问。
	for range 50 {
		a.Comments()
	}
	if got := calls.listCount(); got != 1 {
		t.Errorf("在途的时候问了 50 次，发了 %d 趟，want 1 —— 每帧一次请求", got)
	}

	// 放行，问回来之后**再**问 50 次：还是 1 趟（已经问过了就别再问）。
	close(release)
	waitComments(t, a, 1, "第一趟")
	for range 50 {
		a.Comments()
	}
	if got := calls.listCount(); got != 1 {
		t.Errorf("问回来之后又问了 50 次，总共发了 %d 趟，want 1", got)
	}
}

// TestTheMVCommentOIDIsTheNumericAVID 钉住「mv 那一路的 oid 是数字 avid」。
//
// 🔴 **漏了换 avid ⇒ 静默拿到空列表**：B 站把一个认不出来的 oid 判成「这个对象
// 没有评论」，而界面上只是一个空列表——没有报错、没有提示。
func TestTheMVCommentOIDIsTheNumericAVID(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := &commentCalls{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		calls.addArchive(bvid)
		return bili.ArchiveStats{Aid: 987654321}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		return commentPage([]bili.Comment{{Rpid: 1}}, "", true), nil
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1xx411c7mD", Cid: "1"})
	a.queue.selectAt(0)

	waitComments(t, a, 1, "第一趟")

	if got := calls.archiveCount(); got != 1 {
		t.Fatalf("bvid → avid 那一趟走了 %d 回，want 1", got)
	}
	if got := calls.archive[0]; got != "BV1xx411c7mD" {
		t.Errorf("换 avid 那一趟问的是 %q", got)
	}
	got := calls.firstList()
	if got.typ != bili.CommentTypeVideo {
		t.Errorf("type=%d want %d（视频）", got.typ, bili.CommentTypeVideo)
	}
	if got.oid != 987654321 {
		t.Errorf("oid=%d want 987654321（数字 avid，不是 bvid 串）", got.oid)
	}
}

// TestTheAudioCommentOIDIsTheAUID 钉住「audio 那一路的 oid 是 auid、type=14」。
func TestTheAudioCommentOIDIsTheAUID(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := &commentCalls{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		calls.addArchive(bvid)
		return bili.ArchiveStats{Aid: 111}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		return commentPage([]bili.Comment{{Rpid: 1}}, "", true), nil
	}
	a.Queue().Append(Item{Source: "online", Type: "audio", Sid: "130191"})
	a.queue.selectAt(0)

	waitComments(t, a, 1, "第一趟")

	// ⚠️ **audio 那一路不许换 avid**：oid 就是 auid，多换一次就是白跑一趟。
	if got := calls.archiveCount(); got != 0 {
		t.Errorf("audio 条目也去换 avid 了（%d 回）——它的 oid 本来就是 auid", got)
	}
	got := calls.firstList()
	if got.typ != bili.CommentTypeAudio {
		t.Errorf("type=%d want %d（音乐库音频）", got.typ, bili.CommentTypeAudio)
	}
	if got.oid != 130191 {
		t.Errorf("oid=%d want 130191（auid）", got.oid)
	}
}

// TestALocalItemIsNotWorthAsking 钉住「local 条目不摆入口，且一个请求都不发」。
func TestALocalItemIsNotWorthAsking(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := &commentCalls{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		calls.addArchive(bvid)
		return bili.ArchiveStats{Aid: 1}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		return commentPage(nil, "", true), nil
	}
	a.Queue().Append(LocalItem("/tmp/歌.mp3"))
	a.queue.selectAt(0)

	if a.CommentsWorthAsking() {
		t.Error("本地文件也摆评论入口了")
	}
	for range 20 {
		a.Comments()
	}
	if got := calls.listCount(); got != 0 {
		t.Errorf("本地文件发了 %d 趟评论请求，want 0", got)
	}
	if got := calls.archiveCount(); got != 0 {
		t.Errorf("本地文件发了 %d 趟 bvid→avid，want 0", got)
	}
}

// TestTheCommentsOfThePreviousSongDoNotShowUp 钉住「换歌之后旧的结果作废」。
//
// 迟到的结果回来时发现槽位对不上就丢——不然用户会看到「刚换的歌上面挂着上一首
// 的评论」。
func TestTheCommentsOfThePreviousSongDoNotShowUp(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	release := make(chan struct{})
	var n atomic.Int32
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		return bili.ArchiveStats{Aid: 1}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		if oid == 111 {
			// 第一首那一趟：在途，等测试放行。
			<-release
			return commentPage([]bili.Comment{{Rpid: 1, Message: "上一首的"}}, "", true), nil
		}
		n.Add(1)
		return commentPage([]bili.Comment{{Rpid: 2, Message: "这一首的"}}, "", true), nil
	}
	a.Queue().Append(
		Item{Source: "online", Type: "audio", Sid: "111"},
		Item{Source: "online", Type: "audio", Sid: "222"},
	)
	a.queue.selectAt(0)

	// 触发第一首那一趟（它卡在途）。
	a.Comments()
	waitCommentsFor(t, commentsWaitTimeout, "第一首那一趟发出去了", func() bool {
		a.commentsMu.Lock()
		defer a.commentsMu.Unlock()
		return a.commentsWanted
	})

	// 换歌。
	a.queue.selectAt(1)

	// 第二首自己问出来了。
	waitComments(t, a, 1, "第二首")
	if got := a.Comments()[0].Message; got != "这一首的" {
		t.Errorf("界面上摆的是 %q，want 这一首的", got)
	}

	// 放行第一首那一趟：它回来的时候槽位已经对不上了 ⇒ 丢掉。
	close(release)
	time.Sleep(50 * time.Millisecond)
	if got := a.Comments()[0].Message; got != "这一首的" {
		t.Errorf("上一首那一趟回来之后，界面上摆的是 %q —— 迟到的结果没被丢掉", got)
	}
	if n.Load() != 1 {
		t.Errorf("发了 %d 趟，want 1", n.Load())
	}
}

// TestCommentsAreAskedAgainAfterAFailure 钉住「失败之后能再问」。
//
// 原来失败和成功一样把「已问过」置成 true，于是一次 403 风控 / 一次网络抖动
// 之后，这一首在本次播放期间**再也不会出现评论**——而界面上还没有任何地方能
// 说出「问不出评论」。
func TestCommentsAreAskedAgainAfterAFailure(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := &commentCalls{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		calls.addArchive(bvid)
		return bili.ArchiveStats{Aid: 1}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		return bili.CommentPage{}, errors.New("风控")
	}
	a.Queue().Append(Item{Source: "online", Type: "audio", Sid: "130191"})
	a.queue.selectAt(0)

	// 第一趟：失败。
	if got := a.Comments(); got != nil {
		t.Fatalf("第一次就问出了 %d 条", len(got))
	}
	waitCommentsFor(t, commentsWaitTimeout, "第一趟", func() bool { return calls.listCount() >= 1 })
	if a.CommentsErr() == nil {
		t.Fatal("问失败了却没有记下原因")
	}

	// 退避之内再问：不发。
	//
	// ⚠️ **判据是 `commentsTriedAt` 有没有变**：那是「发起那一问的时刻」，
	// 而它是在**界面那一侧同步**写下去的。所以这一次量的是「有没有真的再发
	// 一趟」，不是「后台那个 goroutine 跑没跑」——后者要等，而等就 flaky 了。
	a.commentsMu.Lock()
	triedAt := a.commentsTriedAt
	a.commentsMu.Unlock()
	for range 20 {
		a.Comments()
	}
	a.commentsMu.Lock()
	triedAfter := a.commentsTriedAt
	a.commentsMu.Unlock()
	if !triedAfter.Equal(triedAt) {
		t.Errorf("退避之内又发起了一问（发起时刻从 %v 变成了 %v）", triedAt, triedAfter)
	}
	if got := calls.listCount(); got != 1 {
		t.Fatalf("退避之内又发了 %d 趟，want 还是 1", got)
	}

	// 把「上一次发起」的时刻拨到退避窗口之前：下一趟之后界面会再问一次。
	a.commentsMu.Lock()
	a.commentsTriedAt = a.commentsTriedAt.Add(-2 * commentsRetryAfter)
	a.commentsMu.Unlock()

	a.Comments()
	waitCommentsFor(t, commentsWaitTimeout, "退避之后那一趟", func() bool { return calls.listCount() >= 2 })
	if got := calls.listCount(); got != 2 {
		t.Fatalf("过了退避窗口发了 %d 趟，want 2 —— 失败之后就再也不问了", got)
	}
}

// TestTheClosedSectionIsSaidInPlainWords 钉住「评论区被关了」在界面上有一句人话。
//
// 🔴 票 #592：B 站在评论区关闭时回的是 **12002**，而仓库原来**只认 12061**
// ⇒ 这一档基本触发不到，界面上摆的是「B 站返回了一个看不懂的错误（12002）」。
//
// 这一条量的是**界面上真的那句话**：应用层把错翻成人话（`bili.CommentFriendly`），
// 用户看到的是「这个评论区被 UP 主关掉了」，**不是**一句「出错了」。
func TestTheClosedSectionIsSaidInPlainWords(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		return bili.ArchiveStats{Aid: 1}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		// 这一档就是 B 站回 12002 时仓库给出的那个错（`bili` 那一层已经把它
		// 翻成了哨兵，票 #592）。**照 `bili` 那一层翻出来的样子包**：带接口
		// 地址——漏到界面上就是一句行话了。
		return bili.CommentPage{}, fmt.Errorf("%w: %s", bili.ErrCommentSectionClosed, "/x/v2/reply/main")
	}
	a.Queue().Append(Item{Source: "online", Type: "audio", Sid: "130191"})
	a.queue.selectAt(0)

	a.Comments()
	waitCommentsFor(t, commentsWaitTimeout, "那一趟回来", func() bool { return a.CommentsErr() != nil })

	err := a.CommentsErr()
	if err == nil {
		t.Fatal("问失败了却没有记下原因")
	}
	msg := err.Error()
	if !strings.Contains(msg, "关掉") {
		t.Errorf("界面上那句话是 %q，没有说评论区被关掉了", msg)
	}
	if strings.Contains(msg, "看不懂") {
		t.Errorf("界面上摆的是兜底那句：%q", msg)
	}
	if strings.Contains(msg, "12002") || strings.Contains(msg, "/x/v2/") {
		t.Errorf("话里漏出了原始的码或接口地址：%q", msg)
	}
}

// TestTheSecondVisitIsInstant 钉住「同一首歌第二次进是瞬时的（不发请求）」。
func TestTheSecondVisitIsInstant(t *testing.T) {
	dir := t.TempDir()
	a := New()
	t.Cleanup(a.Close)
	a.MountComments(comments.NewCache(dir))

	calls := &commentCalls{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		calls.addArchive(bvid)
		return bili.ArchiveStats{Aid: 12345}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		return commentPage([]bili.Comment{{Rpid: 7, Message: "缓存里的"}}, "", true), nil
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)

	waitComments(t, a, 1, "第一趟")
	if got := calls.listCount(); got != 1 {
		t.Fatalf("第一趟发了 %d 趟，want 1", got)
	}
	// ⚠️ **等缓存写完再换歌**：那一趟还在后台往盘上写，而测试目录等会儿会被删
	// ——边写边删的表现是「测试临时目录删不掉」（目录不是空的）。
	waitCommentsFor(t, commentsWaitTimeout, "第一趟写进缓存了", func() bool {
		_, ok := comments.NewCache(dir).Load(bili.CommentTypeVideo, 12345)
		return ok
	})

	// 换歌再换回来。
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV2", Cid: "1"})
	a.queue.selectAt(1)
	a.queue.selectAt(0)

	// 这一趟**不发请求**：缓存里有。
	got := waitComments(t, a, 1, "第二次进来")
	if got[0].Message != "缓存里的" {
		t.Errorf("读到的是 %q，want 缓存里的", got[0].Message)
	}
	if n := calls.listCount(); n != 1 {
		t.Errorf("第二次进来发了 %d 趟请求，want 还是 1 —— 缓存没生效", n)
	}
}

// TestTheRefreshBypassesTheCache 钉住「有一枚『刷新』能绕过缓存」。
func TestTheRefreshBypassesTheCache(t *testing.T) {
	dir := t.TempDir()
	a := New()
	t.Cleanup(a.Close)
	a.MountComments(comments.NewCache(dir))

	calls := &commentCalls{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		calls.addArchive(bvid)
		return bili.ArchiveStats{Aid: 12345}, nil
	}
	var n atomic.Int32
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		// ⚠️ **第一趟和刷新那一趟给的字不一样**：判据要能认出「刷新那一份已经
		// 写进盘上了」。两趟给同一句话的话，判据会被第一趟满足掉，而刷新那一趟
		// 还在后台写——测试临时目录先被删，那一趟写进去就是「目录删不掉」。
		if n.Add(1) == 1 {
			return commentPage([]bili.Comment{{Rpid: 7, Message: "缓存里的"}}, "", true), nil
		}
		return commentPage([]bili.Comment{{Rpid: 7, Message: "新拉的"}}, "", true), nil
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)

	waitComments(t, a, 1, "第一趟")

	a.RefreshComments()
	waitCommentsFor(t, commentsWaitTimeout, "刷新那一趟", func() bool { return calls.listCount() >= 2 })
	waitCommentsFor(t, commentsWaitTimeout, "刷新之后界面上是新的一份", func() bool {
		got := a.Comments()
		return len(got) == 1 && got[0].Message == "新拉的"
	})
	if got := a.Comments(); len(got) != 1 || got[0].Message != "新拉的" {
		t.Errorf("刷新之后拿到的是 %+v", got)
	}
	// ⚠️ **等缓存写完再收工**：后台那一趟还在往盘上写，而测试目录会被删——
	// 边写边删的表现是「测试临时目录删不掉」（目录不是空的）。
	waitCommentsFor(t, commentsWaitTimeout, "刷新那一份写进缓存了", func() bool {
		b, err := os.ReadFile(filepath.Join(dir, comments.Key(bili.CommentTypeVideo, 12345)+".json"))
		return err == nil && strings.Contains(string(b), "新拉的")
	})
}

// TestTheAVIDIsRememberedAcrossRestarts 钉住「换过 avid 那一趟要记住」。
//
// ⚠️ **不只是内存里记住**：用户重启应用、回到刚才那首歌，不该再付一次往返。
// 而往返是**一次网络请求**——验收要求「第二次进是瞬时的（不发请求）」。
func TestTheAVIDIsRememberedAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	a := New()
	t.Cleanup(a.Close)
	a.MountComments(comments.NewCache(dir))

	calls := &commentCalls{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		calls.addArchive(bvid)
		return bili.ArchiveStats{Aid: 12345}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		return commentPage([]bili.Comment{{Rpid: 7}}, "", true), nil
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)

	waitComments(t, a, 1, "第一趟")
	if got := calls.archiveCount(); got != 1 {
		t.Fatalf("换了 %d 回 avid，want 1", got)
	}
	// 等索引写进盘上。
	waitCommentsFor(t, commentsWaitTimeout, "索引写进盘上了", func() bool {
		_, ok := comments.NewCache(dir).LoadAVID("BV1")
		return ok
	})
	// 等评论缓存也写完（不然它还在后台写，而测试目录等会儿会被删）。
	waitCommentsFor(t, commentsWaitTimeout, "评论缓存写进盘上了", func() bool {
		_, ok := comments.NewCache(dir).Load(bili.CommentTypeVideo, 12345)
		return ok
	})

	// 换一个 App（= 重启应用）：那一个也不该再换一次 avid。
	b := New()
	t.Cleanup(b.Close)
	b.MountComments(comments.NewCache(dir))
	b.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		calls.addArchive(bvid)
		return bili.ArchiveStats{Aid: 999}, nil
	}
	b.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		return commentPage([]bili.Comment{{Rpid: 7}}, "", true), nil
	}
	b.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	b.queue.selectAt(0)

	waitComments(t, b, 1, "重启之后那一趟")
	if got := calls.archiveCount(); got != 1 {
		t.Errorf("重启之后又换了 %d 回 avid，want 还是 1 —— 索引没记住", got)
	}
}

// TestARepeatedCommentIsNotAddedTwice 钉住「翻页重叠不会把同一条评论摆两遍」。
//
// 🔴 **翻页重叠是常事**：B 站的分页是按热度切的，不是按 rpid 切的，所以同一条
// 评论挪了名次就会在两页里各出现一次。
//
// ⚠️ **不去重的坏处是「整页变成错误页」**：这一页的行身份就是 rpid（界面层
// `internal/ui/comments.go` 的 `commentRowKey`），而框架见同一个父节点下有两个
// 一样的 key 就 panic（`ui: two elements under one parent have the key ...`）。
func TestARepeatedCommentIsNotAddedTwice(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := &commentCalls{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		return bili.ArchiveStats{Aid: 1}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		if next == "" {
			return commentPage([]bili.Comment{
				{Rpid: 1, Message: "第一条"},
				{Rpid: 2, Message: "第二条"},
			}, "OFFSET-2", false), nil
		}
		// 🔴 **第二页把第一条原样再来一次**（翻页重叠），外加一条新的。
		return commentPage([]bili.Comment{
			{Rpid: 1, Message: "第一条"},
			{Rpid: 3, Message: "第三条"},
		}, "", true), nil
	}
	a.Queue().Append(Item{Source: "online", Type: "audio", Sid: "130191"})
	a.queue.selectAt(0)

	waitComments(t, a, 2, "第一页")
	if !a.LoadMoreComments() {
		t.Fatal("翻不了第二页")
	}
	waitComments(t, a, 3, "第二页")

	got := a.Comments()
	seen := map[int64]int{}
	for _, c := range got {
		seen[c.Rpid]++
	}
	if seen[1] != 1 {
		t.Errorf("rpid 1 出现了 %d 次，want 1——翻页重叠没有去重", seen[1])
	}
	if seen[2] != 1 || seen[3] != 1 {
		t.Errorf("翻页之后的内容不对：%v", got)
	}
}

// TestAPageThatBringsNothingNewIsTheEnd 钉住「一页没有带来任何新东西就是到底
// 了」。
//
// 🔴 **那是「无进展保护」**：接口在重复给同一批（或者这一页是空的），而
// `cursor.is_end` 还说「还有」——于是自动补屏会一直拉下去，每拉一次一个往返，
// 而用户看着末尾那一行转圈转到天荒地老。
//
// ⚠️ **判据是「不再有下一页」**：翻页那一趟回来之后 `CommentsHasMore()` 要是
// false，而且**再翻也翻不动**。
func TestAPageThatBringsNothingNewIsTheEnd(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := &commentCalls{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		return bili.ArchiveStats{Aid: 1}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		if next == "" {
			return commentPage([]bili.Comment{{Rpid: 1, Message: "第一条"}}, "OFFSET-2", false), nil
		}
		// 🔴 **第二页全是重样的**，而游标还说「还有下一页」。
		return commentPage([]bili.Comment{{Rpid: 1, Message: "第一条"}}, "OFFSET-3", false), nil
	}
	a.Queue().Append(Item{Source: "online", Type: "audio", Sid: "130191"})
	a.queue.selectAt(0)

	waitComments(t, a, 1, "第一页")
	if !a.LoadMoreComments() {
		t.Fatal("翻不了第二页")
	}
	// 等第二页那一趟回来（条数不变，所以只能等「问了几趟」）。
	waitCommentListCalls(t, calls, 2)
	// 等它被记下来（去重 + 无进展保护）。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && a.CommentsHasMore() {
		time.Sleep(time.Millisecond)
	}
	if a.CommentsHasMore() {
		t.Errorf("一页全是重样的之后还说「还有下一页」——自动补屏会一直空转拉页")
	}
	if got := len(a.Comments()); got != 1 {
		t.Errorf("翻页之后有 %d 条，want 1（重复的那一条只算一次）", got)
	}
	if a.LoadMoreComments() {
		t.Error("已经到底了还能翻——那是空转（每次都要一个往返）")
	}
}

// waitCommentListCalls 等主列表被问到了 want 趟。
func waitCommentListCalls(t *testing.T, c *commentCalls, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.listCount() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("主列表被问了 %d 趟，want %d", c.listCount(), want)
}

// TestLoadMoreUsesTheCursor 钉住「按游标翻下一页」。
func TestLoadMoreUsesTheCursor(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := &commentCalls{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		return bili.ArchiveStats{Aid: 1}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		if next == "" {
			return commentPage([]bili.Comment{{Rpid: 1, Message: "第一页"}}, "OFFSET-2", false), nil
		}
		return commentPage([]bili.Comment{{Rpid: 2, Message: "第二页"}}, "", true), nil
	}
	a.Queue().Append(Item{Source: "online", Type: "audio", Sid: "130191"})
	a.queue.selectAt(0)

	waitComments(t, a, 1, "第一页")
	if !a.LoadMoreComments() {
		t.Fatal("翻不了第二页")
	}
	waitComments(t, a, 2, "第二页")
	got := a.Comments()
	if got[0].Message != "第一页" || got[1].Message != "第二页" {
		t.Errorf("翻页之后的内容不对：%v", got)
	}
	// ⚠️ **游标要原样带回去**（B 站给的是不透明游标，含义随排序变）。
	if c := calls.firstList(); c.next != "" {
		t.Errorf("第一页带了游标 %q，want 空串（第一页不带）", c.next)
	}
	// ⚠️ **翻页那一趟带的是上一页给的偏移量**，而且**一个字都不改**——
	// 改一个字符 B 站不报错，它回的是第一页（实测）。
	if got := calls.list[1].next; got != "OFFSET-2" {
		t.Errorf("翻页带的游标 = %q，want OFFSET-2（原样带回去）", got)
	}
	a.commentsMu.Lock()
	second := a.commentsTriedAt
	a.commentsMu.Unlock()
	_ = second
	if got := calls.listCount(); got != 2 {
		t.Errorf("翻页发了 %d 趟，want 2", got)
	}
	// 已经到底了 ⇒ 不再翻。
	if a.LoadMoreComments() {
		t.Error("到底了还能翻")
	}
}

// TestLoadMoreCarriesTheOffsetThroughTheCache 钉住「翻页用的偏移量连着缓存
// 一起活下来」。
//
// 🔴 **那一栏是「加载更多」能不能用的唯一依据**：偏移量是**空串**时翻不了
// （拿它去翻第二页会把第一页再要一遍，界面上出现一份重复的评论）。
//
// ⚠️ **而它要穿过缓存**：翻页用的偏移量是从**第一页**那份里拿的，而第一页
// 落盘时用的是**同一个** `NextOffset`——缓存那一栏漏了的话，重进这一首之后
// 「加载更多」就永远按不动了（而第一页还在，界面上看不出任何异样）。
func TestLoadMoreCarriesTheOffsetThroughTheCache(t *testing.T) {
	dir := t.TempDir()
	a := New()
	t.Cleanup(a.Close)
	a.MountComments(comments.NewCache(dir))

	calls := &commentCalls{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		return bili.ArchiveStats{Aid: 1}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		if next == "" {
			return commentPage([]bili.Comment{{Rpid: 1, Message: "第一页"}}, "OFFSET-2", false), nil
		}
		return commentPage([]bili.Comment{{Rpid: 2, Message: "第二页"}}, "", true), nil
	}
	a.Queue().Append(Item{Source: "online", Type: "audio", Sid: "130191"})
	a.queue.selectAt(0)

	waitComments(t, a, 1, "第一页")

	// ⚠️ **落盘的那一份要带着偏移量**——否则重进这一首就翻不动了。
	e, hit := comments.NewCache(dir).Load(bili.CommentTypeAudio, 130191)
	if !hit {
		t.Fatal("第一页没落进缓存")
	}
	if e.NextOffset != "OFFSET-2" {
		t.Errorf("缓存里的偏移量 = %q，want OFFSET-2", e.NextOffset)
	}

	// 翻第二页带的是那份偏移量。
	if !a.LoadMoreComments() {
		t.Fatal("翻不了第二页")
	}
	waitComments(t, a, 2, "第二页")
	if got := calls.list[1].next; got != "OFFSET-2" {
		t.Errorf("翻页带的偏移量 = %q，want OFFSET-2", got)
	}
}

// TestTheRepliesUseTheSameObject 钉住「楼中楼问的是同一个对象」。
func TestTheRepliesUseTheSameObject(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := &commentCalls{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		calls.addArchive(bvid)
		return bili.ArchiveStats{Aid: 12345}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		return commentPage([]bili.Comment{{Rpid: 1, Count: 2}}, "", true), nil
	}
	a.commentsRepliesFetch = func(_ context.Context, typ, oid, root int64) ([]bili.Comment, error) {
		calls.addReply(typ, oid, root)
		return []bili.Comment{{Rpid: 9, Root: root, Message: "楼中楼"}}, nil
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)

	waitComments(t, a, 1, "主列表")
	a.CommentReplies(1)
	waitCommentsFor(t, commentsWaitTimeout, "楼中楼那一趟", func() bool { return calls.replyCount() >= 1 })

	got := calls.firstReply()
	if got.typ != bili.CommentTypeVideo || got.oid != 12345 {
		t.Errorf("楼中楼问的是 type=%d oid=%d，want 1 / 12345", got.typ, got.oid)
	}
	if got.root != 1 {
		t.Errorf("楼中楼的 root=%d want 1", got.root)
	}
	// 再问一次：缓存里有 ⇒ 不发第二趟。
	rs := a.CommentReplies(1)
	if len(rs) != 1 || rs[0].Message != "楼中楼" {
		t.Errorf("楼中楼读回来的是 %+v", rs)
	}
	if n := calls.replyCount(); n != 1 {
		t.Errorf("楼中楼发了 %d 趟，want 1", n)
	}
}

// TestNoCommentsIsNotAFailure 钉住「还没有人评论」和「问不出评论」是两回事。
//
// 混成一句话的话，用户看到「问不出评论」会以为歌没了，而歌好好地摆在那儿。
func TestNoCommentsIsNotAFailure(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		return bili.ArchiveStats{Aid: 1}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		// B 站回的是「这个对象在，只是没人评论」：code 0 + 空列表 + is_end。
		return bili.CommentPage{IsEnd: true, AllCount: 0}, bili.ErrCommentNoComments
	}
	a.Queue().Append(Item{Source: "online", Type: "audio", Sid: "130191"})
	a.queue.selectAt(0)

	a.Comments() // 触发那一趟
	waitCommentsFor(t, commentsWaitTimeout, "那一趟", func() bool {
		a.commentsMu.Lock()
		defer a.commentsMu.Unlock()
		return a.commentsLoaded
	})

	if a.CommentsErr() != nil {
		t.Errorf("「还没有人评论」被当成了失败：%v", a.CommentsErr())
	}
	if !a.CommentsNoComments() {
		t.Error("「还没有人评论」没有说清楚")
	}
}

// TestTheMVAsksAgainWhenTheAVIDCannotBeFetched 钉住「换 avid 失败之后能再问」。
//
// ⚠️ **这一条要防的是「一次失败就永远没有评论」**：换 avid 那一趟失败之后，
// 如果把它当成「问成功了、结果为空」，这一首在本次播放期间就永远没有评论了，
// 而界面上看不出任何异样（一个空列表，没有报错）。
//
// 而**评论请求一个都不该发**（票 #572：「取不到就当『没有评论』，不发请求」）。
func TestTheMVAsksAgainWhenTheAVIDCannotBeFetched(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := &commentCalls{}
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		calls.addArchive(bvid)
		return bili.ArchiveStats{}, errors.New("断网了")
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		return commentPage(nil, "", true), nil
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)

	a.Comments()
	waitCommentsFor(t, commentsWaitTimeout, "换 avid 那一趟", func() bool {
		return calls.archiveCount() >= 1
	})
	// ⚠️ **评论请求一个都不发**：oid 都不知道，发出去只会得到一个空列表。
	if got := calls.listCount(); got != 0 {
		t.Errorf("avid 没换到还发了 %d 趟评论请求，want 0", got)
	}
	// **不报错**：界面上那一句不该说「问不出评论」。
	if err := a.CommentsErr(); err != nil {
		t.Errorf("换 avid 失败被当成了评论失败：%v", err)
	}

	// 退避之内不再问。
	for range 20 {
		a.Comments()
	}
	if got := calls.archiveCount(); got != 1 {
		t.Fatalf("退避之内又换了 %d 回 avid，want 还是 1", got)
	}

	// 把「上一次发起」的时刻拨到退避窗口之前：下一趟之后会再问一次。
	a.commentsMu.Lock()
	a.commentsTriedAt = a.commentsTriedAt.Add(-2 * commentsRetryAfter)
	a.commentsMu.Unlock()

	a.Comments()
	waitCommentsFor(t, commentsWaitTimeout, "退避之后那一趟", func() bool {
		return calls.archiveCount() >= 2
	})
	if got := calls.archiveCount(); got != 2 {
		t.Errorf("过了退避窗口换了 %d 回 avid，want 2 —— 一次失败就再也不问了", got)
	}
}

// TestTheAVIDIsFetchedOnlyOnce 钉住「同一个 bvid 的 avid 只换一次」。
//
// 主列表那一趟和楼中楼那一趟可能同时问「这个 bvid 的 avid 是多少」——两趟都
// 发出去就是两次网络往返，而 B 站会当成两次独立的稿件详情请求。
func TestTheAVIDIsFetchedOnlyOnce(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := &commentCalls{}
	release := make(chan struct{})
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		calls.addArchive(bvid)
		<-release
		return bili.ArchiveStats{Aid: 12345}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		return commentPage([]bili.Comment{{Rpid: 1}}, "", true), nil
	}
	a.commentsRepliesFetch = func(_ context.Context, typ, oid, root int64) ([]bili.Comment, error) {
		return []bili.Comment{{Rpid: 9, Root: root}}, nil
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)

	// 主列表 + 楼中楼**同时**问。
	a.Comments()
	a.CommentReplies(1)
	waitCommentsFor(t, commentsWaitTimeout, "换 avid 那一趟", func() bool {
		return calls.archiveCount() >= 1
	})
	time.Sleep(50 * time.Millisecond)
	close(release)

	waitComments(t, a, 1, "主列表")
	if got := calls.archiveCount(); got != 1 {
		t.Errorf("换了 %d 回 avid，want 1 —— 两趟各发了一次", got)
	}
}

const commentsWaitTimeout = 5 * time.Second

// TestSwitchingSongsCancelsThePreviousCommentRequest 钉住「切歌会取消旧评论请求」。
//
// 旧请求即使最终返回也会被代次丢掉；但它还应该收到取消信号，避免慢的旧页
// 长时间占着请求槽位，快速切歌时把峰值越堆越高。
func TestSwitchingSongsCancelsThePreviousCommentRequest(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := &commentCalls{}
	oldStarted := make(chan struct{})
	oldCanceled := make(chan struct{})
	var startedOnce sync.Once
	var canceledOnce sync.Once
	a.commentsListFetch = func(ctx context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		if oid == 111 {
			startedOnce.Do(func() { close(oldStarted) })
			<-ctx.Done()
			canceledOnce.Do(func() { close(oldCanceled) })
			return bili.CommentPage{}, ctx.Err()
		}
		return commentPage([]bili.Comment{{Rpid: 2, Message: "这一首的"}}, "", true), nil
	}
	a.Queue().Reset(
		Item{Source: "online", Type: "audio", Sid: "111"},
		Item{Source: "online", Type: "audio", Sid: "222"},
	)

	a.Comments()
	waitCommentsFor(t, commentsWaitTimeout, "旧评论请求发出", func() bool {
		select {
		case <-oldStarted:
			return true
		default:
			return false
		}
	})

	a.Queue().selectAt(1)
	if got := a.Comments(); got != nil {
		t.Fatalf("切歌后旧评论还在界面上：%v", got)
	}
	waitComments(t, a, 1, "新歌评论")
	if got := a.Comments()[0].Message; got != "这一首的" {
		t.Fatalf("切歌后界面显示 %q，want 新歌评论", got)
	}
	select {
	case <-oldCanceled:
	case <-time.After(commentsWaitTimeout):
		t.Fatal("切歌后旧评论请求没有收到取消信号")
	}
	if got := calls.listCount(); got != 2 {
		t.Fatalf("评论请求发了 %d 趟，want 旧歌 1 趟 + 新歌 1 趟", got)
	}
}

// waitCommentsFor 是这一组内部那份「等条件」。
func waitCommentsFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等了 %s，%s", timeout, what)
}

// TestEnsureCommentsAsksOnceAndSaysItIsLoaded 钉住「界面那一半的出口」。
//
// 🔴 **它是界面层保障①（首次渲染检查）的落点**：界面只判「该不该问」
// （`internal/ui/comments.go` 的 `syncComments`），真正发出去的是这里——
// 铁律三：view 里不发网络请求。
//
// ⚠️ **重复调它不许多发一趟**：界面每一帧都判一次「还没拿到过第一页」，
// 而 `EnsureComments` 是幂等的（内部判过「问过了 / 正在问」，失败之后还有
// 15 秒退避）——不然就是每秒一趟。
func TestEnsureCommentsAsksOnceAndSaysItIsLoaded(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := &commentCalls{}
	release := make(chan struct{})
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		return bili.ArchiveStats{Aid: 12345}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		<-release
		return commentPage([]bili.Comment{{Rpid: 1, Message: "第一条"}}, "", true), nil
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)

	if a.CommentsLoaded() {
		t.Errorf("还没问就说「已经拿到过」了")
	}

	a.EnsureComments()
	waitCommentsFor(t, commentsWaitTimeout, "那一趟发出去了", func() bool {
		return calls.listCount() >= 1
	})

	// 在途的时候问 50 次（界面就是每一帧问一次）：还是 1 趟。
	for range 50 {
		a.EnsureComments()
	}
	if got := calls.listCount(); got != 1 {
		t.Errorf("在途的时候问了 50 次，发了 %d 趟，want 1", got)
	}

	close(release)
	waitComments(t, a, 1, "第一趟")

	// ⚠️ **拿到过第一页之后就不该再问第一页**：界面那一帧判的是
	// 「还没拿到过吗」——拿到过就不该再问。
	if !a.CommentsLoaded() {
		t.Errorf("第一页回来了还说「还没拿到过」")
	}
	for range 50 {
		a.EnsureComments()
	}
	if got := calls.listCount(); got != 1 {
		t.Errorf("拿到过之后又问了 50 次，总共发了 %d 趟，want 1", got)
	}
}

// TestCommentsLoadedIsTrueEvenWhenNobodyCommented 钉住「一条评论都没有也算
// 拿到过」。
//
// ⚠️ **「还没有人评论」是一份完整的答案**（B 站说这个对象在，只是没人评论），
// 不是「还没问过」——当成「还没问过」的话，界面会一遍一遍地问第一页。
func TestCommentsLoadedIsTrueEvenWhenNobodyCommented(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		return bili.ArchiveStats{Aid: 1}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		return bili.CommentPage{IsEnd: true, AllCount: 0}, bili.ErrCommentNoComments
	}
	a.Queue().Append(Item{Source: "online", Type: "audio", Sid: "130191"})
	a.queue.selectAt(0)

	a.EnsureComments()
	waitCommentsFor(t, commentsWaitTimeout, "那一趟", func() bool {
		return a.CommentsLoaded()
	})

	if !a.CommentsLoaded() {
		t.Errorf("「还没有人评论」没有算成「拿到过」")
	}
}

// TestEnsureCommentsDoesNotAskForALocalFile 钉住「本地文件一个请求都不发」。
//
// ⚠️ **本地音乐在 B 站上没有对应的对象**：问一个认不出来的 oid 的症状是
// 「静默拿到空列表」——而界面上的入口压根不摆（那一层已经挡住了），这一层
// 再挡一道。
func TestEnsureCommentsDoesNotAskForALocalFile(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := &commentCalls{}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		return commentPage(nil, "", true), nil
	}
	a.Queue().Append(Item{Source: "local", Type: "local", Path: "/tmp/x.mp3"})
	a.queue.selectAt(0)

	for range 20 {
		a.EnsureComments()
	}
	if got := calls.listCount(); got != 0 {
		t.Errorf("本地文件发了 %d 趟评论请求，want 0", got)
	}
	if a.CommentsLoaded() {
		t.Errorf("本地文件也说「已经拿到过」了")
	}
}

// TestTheReplyRequestGetsALiveContext 钉住「楼中楼那一趟拿到的 ctx 是活的」。
//
// 🔴 **这是一个真回归的护栏**（2026-10-29 的视觉走查发现）：`CommentReplies`
// 原来是 `go a.loadReplies(...)`，那一趟没有自己的 ctx；后来有人给它补了一个
// `context.WithCancel`，但补的写法是「`commentsCancel` 已经有人占着就把**新造的
// 那个**取消掉」——而主列表那一趟结束时**不会**把 `commentsCancel` 清空。
//
// 于是用户点开楼中楼时走的是 `else` 分支：刚造出来的 ctx 当场被 cancel，
// `loadReplies` 拿着一个已取消的上下文去发请求，**一个包都不会出去**。
// 症状是「点开回复没有任何反应，也没有报错」。
//
// ⚠️ **原来的替身签名是 `func(_ context.Context, …)`**，直接把 ctx 丢掉——
// 所以 `TestTheRepliesUseTheSameObject` 一直是绿的，盖住了这个 bug。
// 这一条**必须**读 ctx 的状态，不然它也是一条哑的护栏。
func TestTheReplyRequestGetsALiveContext(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	// 主列表那一趟要先跑完：正是它把 `commentsCancel` 占住了，
	// 复现的时序必须是「主列表已经拿到 → 再点楼中楼」。
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		return bili.ArchiveStats{Aid: 12345}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		return commentPage([]bili.Comment{{Rpid: 1, Count: 2}}, "", true), nil
	}

	ctxErr := make(chan error, 1)
	a.commentsRepliesFetch = func(ctx context.Context, typ, oid, root int64) ([]bili.Comment, error) {
		// 发请求之前先看 ctx：已取消的话这一趟根本不会出网。
		ctxErr <- ctx.Err()
		return nil, ctx.Err()
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)

	waitComments(t, a, 1, "主列表")
	a.CommentReplies(1)

	select {
	case err := <-ctxErr:
		if err != nil {
			t.Fatalf("楼中楼那一趟拿到的 ctx 已经结束（%v）——这一趟不会发出去，用户点开回复没有任何反应", err)
		}
	case <-time.After(commentsWaitTimeout):
		t.Fatal("楼中楼那一趟压根没被调用")
	}
}

// TestTheReplyRequestSurvivesUntilItsOwnResult 钉住「楼中楼那一趟不会被
// **另一条**楼中楼提前掐掉」。
//
// 同一个槽位上连着点开两条回复时，第一条那一趟不该被第二条取消：两条问的是
// 不同的 root，各自都要拿到自己的结果。取消只该发生在**换歌**（槽位变了）那一下。
func TestTheReplyRequestSurvivesUntilItsOwnResult(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		return bili.ArchiveStats{Aid: 12345}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		return commentPage([]bili.Comment{
			{Rpid: 1, Count: 2}, {Rpid: 2, Count: 3},
		}, "", true), nil
	}

	release := make(chan struct{})
	results := make(chan int64, 4)
	a.commentsRepliesFetch = func(ctx context.Context, typ, oid, root int64) ([]bili.Comment, error) {
		// 两条都先挂着，等测试放行——这样第二条来得及把第一条掐掉。
		select {
		case <-release:
		case <-ctx.Done():
			results <- -root
			return nil, ctx.Err()
		}
		results <- root
		return []bili.Comment{{Rpid: root * 10, Root: root, Message: "楼中楼"}}, nil
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)

	waitComments(t, a, 2, "主列表")
	a.CommentReplies(1)
	a.CommentReplies(2)
	close(release)

	// 两条都要正常回来（负数是「被取消了」）。
	var got []int64
	for range 2 {
		select {
		case root := <-results:
			got = append(got, root)
		case <-time.After(commentsWaitTimeout):
			t.Fatalf("只回来了 %d 条楼中楼，want 2", len(got))
		}
	}
	for _, root := range got {
		if root < 0 {
			t.Errorf("root %d 那一趟被取消了——两条楼中楼问的是不同的人，不该互相掐", -root)
		}
	}
}
