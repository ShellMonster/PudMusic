package lyrics

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveNeteaseReturnsWordTiming 是联网验收，默认跳过。
//
// 它护的是**请求参数**，不是解析：`yv` 少一次，接口就不返回 `yrc`，而 fixture
// 里的 JSON 永远带着它——离线测试看不出这个缺口。2026-11-12 就是这么漏掉一次，
// 线上逐字一份都拿不到。
//
// 只读公开接口，不带任何账号或 Cookie。打开方式：
//
//	PUDMUSIC_LIVE_LYRICS=1 go test ./internal/service/lyrics -run TestLiveNeteaseReturnsWordTiming -v
func TestLiveNeteaseReturnsWordTiming(t *testing.T) {
	if os.Getenv("PUDMUSIC_LIVE_LYRICS") == "" {
		t.Skip("设 PUDMUSIC_LIVE_LYRICS=1 才跑联网验收")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	n := NewNetease(nil)
	cands, err := n.Search(ctx, "孤勇者 陈奕迅")
	if err != nil {
		t.Fatalf("搜索失败：%v", err)
	}
	if len(cands) == 0 {
		t.Fatal("没搜到候选")
	}
	var lastErr error
	for _, c := range cands {
		doc, err := n.Preview(ctx, c)
		if err != nil {
			lastErr = err
			continue
		}
		if lines := ParseYRC(doc.YRC); len(lines) > 0 {
			if len(lines[0].Words) == 0 {
				t.Fatalf("逐字那份解析出来没有词：%+v", lines[0])
			}
			return
		}
	}
	t.Fatalf("搜到的候选都没有逐字那份（最后一次错误：%v）", lastErr)
}
