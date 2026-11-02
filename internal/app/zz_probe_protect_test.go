package app_test

// 应用层探针 2（2026-11-02 对抗性审查）：引用计数的配对。
//
// PROBE APP-2: **换档收养 → 保护计数**。
//
//	playFromByteCache：A 档 Protect（1 票）
//	restreamOnline（换到 B 档）：
//	    releaseByteCacheProtect(tr)   → A 档 0 票
//	    adoptPreviousTrack(tr)        → A 档 Protect（1 票，finisher）
//	    新档 B 装上 → B 档没有保护（走网络那一路不 protect）
//	→ A 档 finisher 收尸 → Unprotect → A 档 0 票 ✅
//
// PROBE APP-3: **finisher 被 cancelFinishersForCache 收掉之后**，
// drain 还会跑 removeFinisher → 又 Unprotect 一次 → 多解一票。
// 那时候新的一档已经 Protect 了一票 → 计数从 2 掉到 1 → 还在 → 没问题。
// 但如果顺序反过来（新的一档的 Protect 在 Unprotect 之后）→ 计数掉到 0 →
// 正在播的那一份裸奔。

import (
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

func TestProbeAppProtectCounting(t *testing.T) {
	media, err := os.ReadFile(flacFixturePath(t))
	if err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	f := newStreamFixture(t)
	f.media = media

	gateX, gateY := make(chan struct{}), make(chan struct{})
	f.on("/cdn/x.flac", gatedHalfHandler(media, new(atomic.Int32), gateX))
	f.on("/cdn/y.flac", gatedHalfHandler(media, new(atomic.Int32), gateY))
	f.onPlayURLForCid("1", twoPathDash(cdnURL(f.URL, "x.flac", time.Hour), cdnURL(f.URL, "y.flac", time.Hour)))

	cacheDir := t.TempDir()
	a := app.New()
	t.Cleanup(a.Close)
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountStreaming(app.Streaming{Client: cli, Media: f.Client(), TempDir: t.TempDir(), CacheDir: cacheDir})

	if err := a.PlayFrom(app.Item{Source: "online", Type: "mv", Bvid: "BV1A", Cid: "1", Title: "A"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, "A 开始出声", func() bool {
		return a.Snapshot().Title == "A"
	})
	// 换档 → 旧档被收养 → 旧档有 1 票保护。
	a.SetAudioQuality(bili.AudioMedium)
	waitFor(t, 30*time.Second, "换到 128K", func() bool {
		return a.Snapshot().Quality == "128K"
	})
	t.Logf("PROBE APP-2: 换档后快照 = %+v", a.Snapshot())

	// 放行两道闸 → 两条都补完 → 两个 finisher 收尸 → 各自 Unprotect。
	close(gateX)
	close(gateY)
	waitFor(t, 30*time.Second, "192K 补完", func() bool {
		return cacheCompleteFor(t, cacheDir, "online|mv|BV1A|1|", "q30280")
	})
	waitFor(t, 30*time.Second, "128K 补完", func() bool {
		return cacheCompleteFor(t, cacheDir, "online|mv|BV1A|1|", "q30232")
	})
	t.Logf("PROBE APP-2: 两条都补完了")
}
