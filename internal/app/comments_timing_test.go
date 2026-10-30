package app

import (
	"context"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/service/comments"
)

// 评审指出的三个时序缺口（票 #572 第二轮）。
//
// 这三条盯的是**同一个对象的那一份结果**在三个时刻的先后：
//
//	零评论    「还没有人评论」是一份**完整的答案** ⇒ 要落缓存
//	刷新      刷新那一趟是**较新**的一趟 ⇒ 较早的那一趟不许盖掉它
//	换歌      旧的一趟在**可见状态上**被丢掉 ⇒ 它也不许盖掉盘上的新缓存
//
// ⚠️ 这一组走的是**真的 `comments.Cache`**（不是替身）：后两条盯的就是
// 「盘上那一份有没有被改回去」。
//
// 这一组**全走替身**，一个真请求都不发（AGENTS.md §6）。

// TestAnObjectWithNoCommentsIsCached 钉住「零评论的对象也落缓存」。
//
// 🔴 **B 站说「这个对象在，只是没人评论」是一份完整的答案**。不存的话每进一次
// 就要重新问一次——而验收要求「同一首歌第二次进是**瞬时的**（不发请求）」。
func TestAnObjectWithNoCommentsIsCached(t *testing.T) {
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
		// B 站回的是「这个对象在，只是没人评论」：code 0 + 空列表 + is_end。
		return bili.CommentPage{IsEnd: true, AllCount: 0}, bili.ErrCommentNoComments
	}
	a.Queue().Append(
		Item{Source: "online", Type: "audio", Sid: "111"},
		Item{Source: "online", Type: "audio", Sid: "222"},
	)
	a.queue.selectAt(0)

	// 第一首：问一次，确认「还没有人评论」。
	a.Comments()
	waitCommentsFor(t, commentsWaitTimeout, "第一首那一趟", func() bool {
		a.commentsMu.Lock()
		defer a.commentsMu.Unlock()
		return a.commentsLoaded
	})
	if !a.CommentsNoComments() {
		t.Error("「还没有人评论」没有说清楚")
	}
	// ⚠️ **零评论也要落盘**：不落的话第二次进来又要问一遍。
	waitCommentsFor(t, commentsWaitTimeout, "零评论落进缓存了", func() bool {
		e, ok := comments.NewCache(dir).Load(bili.CommentTypeAudio, 111)
		return ok && e.NoComments
	})

	// 换到第二首再换回来：A → B → A。
	a.queue.selectAt(1)
	a.Comments()
	waitCommentsFor(t, commentsWaitTimeout, "第二首那一趟", func() bool {
		a.commentsMu.Lock()
		defer a.commentsMu.Unlock()
		return a.commentsLoaded
	})
	a.queue.selectAt(0)

	// 再问一次：A 的结果应该**从缓存里**来（不发请求）。
	a.Comments()
	waitCommentsFor(t, commentsWaitTimeout, "A 那一趟（走缓存）", func() bool {
		a.commentsMu.Lock()
		defer a.commentsMu.Unlock()
		return a.commentsLoaded
	})
	// ⚠️ **A、B 各一次，共 2 趟**：A 的第二次是缓存命中，不发请求。
	if got := calls.listCount(); got != 2 {
		t.Errorf("总共发了 %d 趟请求，want 2 —— 零评论的对象没有落缓存", got)
	}
}

// TestAnOlderRequestDoesNotOverwriteTheRefresh 钉住「刷新那一趟不会被在途的旧
// 那一趟盖掉」。
//
// 顺序是：先卡住首次加载那一趟 → 按刷新 → 刷新那一趟回来（界面上是新的一份）→
// 放行首次加载那一趟 → **界面必须还是新的一份**。
//
// 🔴 **代次挡不住这个**：代次是「换歌才加」，而刷新不换歌。
func TestAnOlderRequestDoesNotOverwriteTheRefresh(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := &commentCalls{}
	release := make(chan struct{})
	first := true
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		return bili.ArchiveStats{Aid: 12345}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		if first {
			first = false
			// ⚠️ **首次加载那一趟卡在途**：刷新要在它还在路上的时候发生。
			<-release
			return commentPage([]bili.Comment{{Rpid: 1, Message: "旧的"}}, "", true), nil
		}
		return commentPage([]bili.Comment{{Rpid: 1, Message: "刷新拉的"}}, "", true), nil
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)

	// 触发首次加载那一趟（它卡在途上）。
	a.Comments()
	waitCommentsFor(t, commentsWaitTimeout, "首次加载那一趟发出去了", func() bool {
		return calls.listCount() >= 1
	})

	// 按刷新：刷新那一趟会发出去（缓存被绕过）。
	a.RefreshComments()
	waitCommentsFor(t, commentsWaitTimeout, "刷新那一趟", func() bool {
		return calls.listCount() >= 2
	})
	waitCommentsFor(t, commentsWaitTimeout, "界面上是刷新拉的那一份", func() bool {
		got := a.Comments()
		return len(got) == 1 && got[0].Message == "刷新拉的"
	})

	// 放行首次加载那一趟（它是**较早**的那一趟）。
	close(release)
	time.Sleep(100 * time.Millisecond)

	got := a.Comments()
	if len(got) != 1 || got[0].Message != "刷新拉的" {
		t.Errorf("较早的那一趟回来之后，界面上摆的是 %+v，want 刷新拉的那一份 —— "+
			"旧请求盖掉了刷新结果", got)
	}
}

// TestAnOlderRequestDoesNotOverwriteTheCache 钉住「换歌后迟到的旧响应不许盖掉
// 盘上的新缓存」。
//
// 顺序是：A（旧那一趟卡在途）→ B → A（新的一趟回来并落盘）→ 放行 A 的**旧**那一趟
// → **盘上那份必须还是新的**。
//
// 🔴 **症状是「重进这一首，拿到的是几分钟前的那一份」**：用户看到的是「评论怎么
// 变了」，而他什么都没做。
func TestAnOlderRequestDoesNotOverwriteTheCache(t *testing.T) {
	dir := t.TempDir()
	a := New()
	t.Cleanup(a.Close)
	a.MountComments(comments.NewCache(dir))

	calls := &commentCalls{}
	releaseOld := make(chan struct{})
	releaseNew := make(chan struct{})
	// 🔴 **同一个对象（oid 111）**：A 和「新的 A」问的是同一条评论。
	//
	// ⚠️ **必须用同一个 oid**：缓存文件是按 `<type>-<oid>` 命名的，而这一条盯的
	// 就是「盘上那一份被改回去」。oid 不一样的话那是两个文件，改不到一起。
	a.commentsArchiveFetch = func(_ context.Context, bvid string) (bili.ArchiveStats, error) {
		return bili.ArchiveStats{Aid: 1}, nil
	}
	a.commentsListFetch = func(_ context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
		calls.addList(typ, oid, next)
		if oid == 111 {
			// ⚠️ **靠「第几趟」分两趟**：两趟问的是同一个 oid。
			switch calls.listCount() {
			case 1:
				<-releaseOld
				return commentPage([]bili.Comment{{Rpid: 1, Message: "旧A"}}, "", true), nil
			default:
				<-releaseNew
				return commentPage([]bili.Comment{{Rpid: 1, Message: "新A"}}, "", true), nil
			}
		}
		return commentPage([]bili.Comment{{Rpid: 2, Message: "B的"}}, "", true), nil
	}
	a.Queue().Append(
		Item{Source: "online", Type: "audio", Sid: "111"},
		Item{Source: "online", Type: "audio", Sid: "222"},
	)
	a.queue.selectAt(0)

	// 旧 A 那一趟发出去了（卡在途）。
	a.Comments()
	waitCommentsFor(t, commentsWaitTimeout, "旧 A 那一趟发出去了", func() bool {
		return calls.listCount() >= 1
	})

	// 换到 B。
	a.queue.selectAt(1)
	a.Comments()
	waitCommentsFor(t, commentsWaitTimeout, "B 那一趟", func() bool {
		a.commentsMu.Lock()
		defer a.commentsMu.Unlock()
		return a.commentsLoaded
	})

	// 换回 A：**新的一趟**（旧那一趟还在途）。
	a.queue.selectAt(0)
	a.Comments()
	waitCommentsFor(t, commentsWaitTimeout, "新 A 那一趟发出去了", func() bool {
		return calls.listCount() >= 2
	})
	// 放行新 A：它把结果落盘。
	close(releaseNew)
	waitCommentsFor(t, commentsWaitTimeout, "新 A 落进缓存了", func() bool {
		e, ok := comments.NewCache(dir).Load(bili.CommentTypeAudio, 111)
		return ok && len(e.Comments) == 1 && e.Comments[0].Message == "新A"
	})

	// 放行旧 A：它是**较早**的那一趟，不许盖掉盘上的新缓存。
	close(releaseOld)
	time.Sleep(100 * time.Millisecond)

	e, ok := comments.NewCache(dir).Load(bili.CommentTypeAudio, 111)
	if !ok {
		t.Fatal("盘上那一份不见了")
	}
	if len(e.Comments) != 1 || e.Comments[0].Message != "新A" {
		t.Errorf("盘上那一份被旧请求改成了 %+v，want 新A —— 迟到的旧响应盖掉了缓存",
			e.Comments)
	}
}

// TestTheNewTripStillWritesTheCacheAfterComingBack 钉住「A → B → A 之后，新的 A
// 那一趟**照样**落盘」。
//
// 🔴 **这是上面那条守卫的反面**：如果「较早的那一趟不许写盘」的实现顺手把**新的
// 那一趟**也挡掉了，那么换回 A 之后界面上是对的（A 的新结果摆在那儿），而**盘上
// 那一份是空的**——于是下次进 A 又要重新问一遍（那就不是「第二次进是瞬时的」）。
func TestTheNewTripStillWritesTheCacheAfterComingBack(t *testing.T) {
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
		if oid == 111 {
			return commentPage([]bili.Comment{{Rpid: 1, Message: "A的"}}, "", true), nil
		}
		return commentPage([]bili.Comment{{Rpid: 2, Message: "B的"}}, "", true), nil
	}
	a.Queue().Append(
		Item{Source: "online", Type: "audio", Sid: "111"},
		Item{Source: "online", Type: "audio", Sid: "222"},
	)
	a.queue.selectAt(0)

	a.Comments()
	waitCommentsFor(t, commentsWaitTimeout, "A 那一趟", func() bool {
		a.commentsMu.Lock()
		defer a.commentsMu.Unlock()
		return a.commentsLoaded
	})
	waitCommentsFor(t, commentsWaitTimeout, "A 落进缓存了", func() bool {
		_, ok := comments.NewCache(dir).Load(bili.CommentTypeAudio, 111)
		return ok
	})

	// B → A。
	a.queue.selectAt(1)
	a.Comments()
	waitCommentsFor(t, commentsWaitTimeout, "B 那一趟", func() bool {
		a.commentsMu.Lock()
		defer a.commentsMu.Unlock()
		return a.commentsLoaded
	})
	a.queue.selectAt(0)
	a.Comments()
	waitCommentsFor(t, commentsWaitTimeout, "换回 A 那一趟", func() bool {
		a.commentsMu.Lock()
		defer a.commentsMu.Unlock()
		return a.commentsLoaded
	})

	// ⚠️ **盘上那一份必须还在**：换回 A 之后新 A 那一趟落过盘。
	waitCommentsFor(t, commentsWaitTimeout, "换回 A 之后盘上那一份还在", func() bool {
		e, ok := comments.NewCache(dir).Load(bili.CommentTypeAudio, 111)
		return ok && len(e.Comments) == 1 && e.Comments[0].Message == "A的"
	})
}
