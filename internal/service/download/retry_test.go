package download

// 「这一项失败之后**还能不能重试**」这一位（票 #346）。
//
// # 为什么这一层要有一��
//
// 下载记录页上那枚「重试」按下去是**立刻重新取一次流**——和播放条上那枚
// 「重试」是**同一个动作**。所以两处必须用**同一个判据**（`bili.Recoverable`
// 那张表，由 app 层装进来，见 `Options.CanRetry`）。
//
// 各判各的的话，风控失败的下载会**仍然**给一枚「重试」键：用户按一次撞一次，
// 按第二次再撞——和上一票（#316）在播放条上收掉的那一轮一模一样，只是换了个
// 界面。
//
// 这一层**不认识 B 站**，所以判据是上层装进来的函数；它只负责在失败的那一刻
// 把判据的结果记在任务上（`Task.noRetry`），好让记录页那一行知道该不该摆
// 那枚键。

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// errBlocked 是一个「再问一次也没用」的错。
//
// ⚠️ 它只是**一个形状**：这一层不认 B 站的任何一个哨兵（依赖单向，见
// `app.MountDownloads` 的注释），所以测试里用一个本地哨兵代替。
var errBlocked = errors.New("download: 这一趟被挡下了")

// newFailingQueue 起一个「取流一定失败」的队列。
//
// canRetry 传 nil 就是「没装判据」——那是这一版之前的行为（每一项出错都
// 给一枚「重试」）。
func newFailingQueue(t *testing.T, canRetry func(error) bool) *Manager {
	t.Helper()
	m, err := New(Options{
		Client: http.DefaultClient,
		Resolve: func(context.Context, Identity) (*Source, error) {
			return nil, errBlocked
		},
		CanRetry: canRetry,
	}, "")
	if err != nil {
		t.Fatalf("建队列：%v", err)
	}
	t.Cleanup(func() { closeManager(t, m, "") })
	return m
}

// TestTheFailureDecidesWhetherARetryIsOffered 盯判据接缝。
//
// 这一条钉的是**两半**：判据说「不能重试」时那一项就**没有**「重试」可给；
// 判据说「能重试」时**有**。而「能不能重试」这件事**只有判据说了算**——
// 这一层不许自己另判一套。
func TestTheFailureDecidesWhetherARetryIsOffered(t *testing.T) {
	for _, c := range []struct {
		what string
		// canRetry 是装进去的判据。nil = 没装（旧行为）。
		canRetry func(error) bool
		want     bool
	}{
		{
			what:    "判据说不能重试",
			canRetry: func(error) bool { return false },
			want:    false,
		},
		{
			what:    "判据说能重试",
			canRetry: func(error) bool { return true },
			want:    true,
		},
		{
			// 没装判据就按「能重试」算（旧行为）。
			what: "没装判据",
			want: true,
		},
	} {
		m := newFailingQueue(t, c.canRetry)
		snap, err := m.Enqueue(AddRequest{
			Bvid: bvid, Cid: cid, Title: "测试的歌", Dir: t.TempDir(), Ext: ".m4a",
		})
		if err != nil {
			t.Fatalf("%s：入队：%v", c.what, err)
		}
		got := waitStatus(t, m, snap.ID, StatusError)
		if got.CanRetry != c.want {
			t.Errorf("%s：CanRetry = %v，want %v（错是 %q）", c.what, got.CanRetry, c.want, got.Err)
		}
	}
}

// TestTheVerdictIsJudgedAgainOnEveryFailure 盯「判据是每失败一次判一次的」。
//
// ⚠️ **为什么这一条要单独钉**：判据是**这一轮失败之后**才判的。用户按了
// 「重试」之后，这一项又跑了一轮——那时候它会不会再失败是另一件事，而
// 「现在该不该给「重试」键」要由**这一轮**的结局说话。
//
// 所以判据给两个不同的答案时，第二次失败的结论要跟着**这一次**走，而不是
// 带着上一轮那个「不能重试」不变。
func TestTheVerdictIsJudgedAgainOnEveryFailure(t *testing.T) {
	var calls int
	m := newFailingQueue(t, func(error) bool {
		calls++
		return calls > 1 // 第一次失败说「不能重试」，第二次说「能重试」
	})
	snap, err := m.Enqueue(AddRequest{
		Bvid: bvid, Cid: cid, Title: "测试的歌", Dir: t.TempDir(), Ext: ".m4a",
	})
	if err != nil {
		t.Fatalf("入队：%v", err)
	}
	failed := waitStatus(t, m, snap.ID, StatusError)
	if failed.CanRetry {
		t.Fatal("第一次失败：判据说不能重试，而这一项说能重试")
	}

	// 按「重试」（`Resume` 是同一个动作）：它回到队列里再跑一轮，又失败了。
	if err := m.Resume(snap.ID); err != nil {
		t.Fatalf("重试：%v", err)
	}
	again := waitStatus(t, m, snap.ID, StatusError)
	if !again.CanRetry {
		t.Error("第二次失败：判据这一次说「能重试」，而这一项还带着上一轮那个「不能重试」")
	}
}
