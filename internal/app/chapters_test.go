package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// 这一组测的是「进度条上那些章节点」的数据从哪来、什么时候来。
//
// 规矩和分集那一族（`fullplayer_prefs_test.go`）一样：界面每一帧调
// `Chapters()`，getter 顺手把没问过的那一趟问出去——所以这里量的都是
// **getter 被调之后发生了什么**，不牵扯任何界面。

// twoChapters 是两条像样的章节（时长和排序都合法）。
func twoChapters() []bili.Chapter {
	return []bili.Chapter{
		{From: 12 * time.Second, To: 90 * time.Second, Title: "前奏"},
		{From: 90 * time.Second, To: 200 * time.Second, Title: "正题"},
	}
}

// waitChapters 轮询到问到了 want 条章节。
func waitChapters(t *testing.T, a *App, want int, what string) []Chapter {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := a.Snapshot().Chapters; len(got) == want {
			return got
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等了 %d 条章节，want %d（%s）", len(a.Snapshot().Chapters), want, what)
	return nil
}

// TestTheCurrentTracksChaptersShowUp 盯「问到回来、换一首就忘掉」的主干。
//
// 键是 **bvid+cid**：章节是按集给的，多 P 的两集各是各的——只按 bvid 记的
// 话，切到第二集还挂着第一集的点，seek 吸附落下去就是错的时刻。
func TestTheCurrentTracksChaptersShowUp(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := &fetchCalls{}
	a.chaptersFetch = func(_ context.Context, bvid, cid string) ([]bili.Chapter, error) {
		calls.n.Add(1)
		if bvid == "BV1" && cid == "1" {
			return twoChapters(), nil
		}
		return nil, nil
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)

	got := waitChapters(t, a, 2, "第一集的章节")
	want := []Chapter{
		{At: 12 * time.Second, End: 90 * time.Second, Title: "前奏"},
		{At: 90 * time.Second, End: 200 * time.Second, Title: "正题"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 条 = %+v，want %+v", i, got[i], want[i])
		}
	}

	// 界面每一帧都调：问过之后不再问第二趟。
	for range 20 {
		_ = a.Snapshot().Chapters
	}
	if n := calls.got(); n != 1 {
		t.Errorf("问了 %d 趟，want 1", n)
	}

	// 换到没有章节的那一集：键变了，缓存当场清掉——**第一帧**就该是空的，
	// 不能等后台那一趟回来才清（那半秒里挂的还是上一集的点）。
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV2", Cid: "2"})
	a.queue.selectAt(1)
	if got := a.Snapshot().Chapters; got != nil {
		t.Fatalf("换集之后第一帧就问出了 %d 条章节——那还是上一集的", len(got))
	}
	calls.wait(t, 2, "第二集那一趟")
	for range 10 {
		_ = a.Snapshot().Chapters
	}
	if n := calls.got(); n != 2 {
		t.Errorf("问了 %d 趟，want 2（没有章节也是问过，不该再来第三趟）", n)
	}
	if got := a.Snapshot().Chapters; got != nil {
		t.Errorf("没有章节的那一集问出了 %+v", got)
	}

	// 切回第一集：键又变了，重新问一轮。
	a.queue.selectAt(0)
	got = waitChapters(t, a, 2, "切回来之后的章节")
	if len(got) != 2 {
		t.Fatalf("切回来之后章节没回来")
	}
}

// TestChaptersFailQuietlyAndComeBackAfterTheBackoff 盯「失败是静默的、退避之后
// 还会再问」。
//
// 和分集那边（`TestPartsAreAskedAgainAfterAFailure`）同一个坑：失败要是也算
// 「问过」，一次网络抖动之后这一集到播完都没有点。章节没有界面上能说失败
// 的地方（点不点是锦上添花的事，不弹错），所以判据就是「过退避窗口之后
// 真的又发了一趟、成功之后点真的回来了」。
func TestChaptersFailQuietlyAndComeBackAfterTheBackoff(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := &fetchCalls{}
	a.chaptersFetch = func(_ context.Context, _, _ string) ([]bili.Chapter, error) {
		calls.n.Add(1)
		if calls.n.Load() < 3 {
			return nil, errors.New("风控")
		}
		return twoChapters(), nil
	}
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	a.queue.selectAt(0)

	// 前两趟都失败：界面上什么都不出现，也没有任何错误可看（静默）。
	for i := 1; i <= 2; i++ {
		if got := a.Snapshot().Chapters; got != nil {
			t.Fatalf("第 %d 趟（失败）问出了 %+v", i, got)
		}
		calls.wait(t, int32(i), "失败那一趟")
		// 退避之内再调不发。
		_ = a.Snapshot().Chapters
		if n := calls.got(); n != int32(i) {
			t.Fatalf("退避之内发了 %d 趟，want %d", n, i)
		}
		// 把「上一次发起」拨到退避窗口之前，下一帧会再问一次。
		a.chaptersMu.Lock()
		a.chaptersTriedAt = a.chaptersTriedAt.Add(-2 * chaptersRetryAfter)
		a.chaptersMu.Unlock()
	}

	got := waitChapters(t, a, 2, "第三趟（成功）的章节")
	if len(got) != 2 {
		t.Fatalf("成功之后章节没回来")
	}
	for range 10 {
		_ = a.Snapshot().Chapters
	}
	if n := calls.got(); n != 3 {
		t.Errorf("成功之后又问了 %d 趟，want 3", n)
	}
}

// TestChaptersAreNotAskedForItemsWithoutAPart 盯「只有投稿条目才问」。
//
// 音乐库音频的时长和背后投稿对不上（把投稿的章节按到音频上，seek 落下去
// 就是错的时刻），本地文件更没有——这两路连问都不该问。
func TestChaptersAreNotAskedForItemsWithoutAPart(t *testing.T) {
	for name, it := range map[string]Item{
		"音乐库条目":       {Source: "online", Type: "audio", Sid: "1293494"},
		"投稿条目但没有 cid": {Source: "online", Type: "mv", Bvid: "BV1"},
		"本地文件":        {Source: "local", Path: "/tmp/x.flac"},
	} {
		t.Run(name, func(t *testing.T) {
			a := New()
			t.Cleanup(a.Close)
			calls := &fetchCalls{}
			a.chaptersFetch = func(_ context.Context, _, _ string) ([]bili.Chapter, error) {
				calls.n.Add(1)
				return twoChapters(), nil
			}
			a.Queue().Append(it)
			a.queue.selectAt(0)
			for range 5 {
				if got := a.Snapshot().Chapters; got != nil {
					t.Fatalf("问出了 %+v，want 空", got)
				}
			}
			if n := calls.got(); n != 0 {
				t.Errorf("发了 %d 趟请求，want 0", n)
			}
		})
	}
}
