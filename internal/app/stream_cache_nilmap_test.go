package app

// 「新建的应用也能往缓存里写」。
//
// ⚠️ **这一条盯的是一个真崩溃**（2026-10-30）：`New()` 只初始化了
// `streamCache` 那一个 map，而「放进来的先后」那个 `streamCacheOrder` 是 **nil
// map** —— 往 nil map 写会 panic（`assignment to entry in nil map`）。
//
// 症状很隐蔽：单跑每条测试都过，**整包跑**才炸——因为崩的是另一个 goroutine
// 里回调进来的 `refetch`（换流重试），而测试的 `t.Cleanup(a.Close)` 撞上那个
// 崩掉的 goroutine，整包就卡在 12 分钟超时上。
//
// ⚠️ **这一条用内部的出口**（`package app`）：要验的正是「那两栏都是 nil 的
// 时候，第一次写会不会炸」，而那个出口在界面层（`package app_test`）够不着。

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

func TestTheStreamCacheSurvivesAFreshApp(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	// 🔴 **先确认它真的是 nil**（`New()` 只初始化了另一个 map）：
	if a.streamCacheOrder != nil {
		t.Skip("`New()` 已经初始化了 streamCacheOrder，这一条要盯的那个坑不在了")
	}

	// 往缓存里写一条：走的是界面层那条路（`resolve` 写进去的）。
	a.mu.Lock()
	a.putStreamRefLocked("key", &bili.StreamRef{
		URLs:      []string{"https://f/x.flac"},
		ExpiresAt: time.Now().Add(time.Hour),
	})
	got := len(a.streamCache)
	a.mu.Unlock()
	if got != 1 {
		t.Errorf("写进去 %d 条，want 1", got)
	}
}
