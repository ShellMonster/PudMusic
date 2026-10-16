package app_test

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// 「音质切换后立即生效，无需重启」。
//
// # 这条断的是什么
//
// 不是「偏好记下来了」（那是设置那一层的事），而是**正在放的那首歌**：换档
// 之后要换一条流、跳回原来的**播放位置**接着播，中途不重启应用、不切歌。
//
// # 为什么断「位置不许倒退」而不是「有没有报错」
//
// 从头播一遍这个 bug 看起来**完全正常**：歌在响、没有红字、进度条也在走，
// 只有拿位置去量才发现回到 0。所以采样从切换**之前**就开始，断的是
// 「相邻两次采样之间位置不许倒退」。
//
// 采样在它自己的 goroutine 里跑，切片只归它所有，收尾用一个 channel 交回来
// ——共享变量 + 一个 bool 收尾是最容易写出数据竞争的那种写法（`-race` 一跑
// 就红，而且红的还是测试而不是产品代码）。

// twoTierDash 造一份「有 320K 也有 128K」的 dash，两条流指向同一段字节。
//
// 指向同一段是刻意的：这里要验的是「换档之后接线对不对」，不是「B 站换了
// 一段字节」——后者是 `internal/service/audio` 那边换流机制自己的事。
//
// ⚠️ 带宽要按档位给（B 站给的 bandwidth 是这次编码的实际码率，档位越高越大）。
// 给反了的话「最好的那一条」就变成 128K 了，自动档名正言顺地挑中它——
// 看起来像选档坏了，其实是这个 fixture 自己写反了。
func twoTierDash(mediaURL string) map[string]any {
	return map[string]any{
		"duration": 1,
		"audio": []any{
			map[string]any{
				"id": int(bili.QualityHigh320), "bandwidth": 320000,
				"codecs": "flac", "mimeType": "audio/mp4", "baseUrl": mediaURL,
			},
			map[string]any{
				"id": int(bili.QualityMid128), "bandwidth": 128000,
				"codecs": "flac", "mimeType": "audio/mp4", "baseUrl": mediaURL,
			},
		},
	}
}

// TestSwitchingQualitySwapsTheStreamWithoutRestarting 盯「换音质立刻生效」。
func TestSwitchingQualitySwapsTheStreamWithoutRestarting(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(twoTierDash(cdnURL(f.URL, "a.flac", 2*time.Hour)))

	a := newOnlineApp(t, f, t.TempDir())
	it := onlineItem("BV1", "1", "换档的那首")
	if err := a.Enqueue(it); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "换档的那首")
	if got := a.Snapshot().Quality; got != "320K" {
		t.Fatalf("一开始的徽章 = %q，want 320K（自动档取最好的那一档）", got)
	}
	waitFor(t, 30*time.Second, "播放位置动起来", func() bool {
		return a.Snapshot().Position > 700*time.Millisecond
	})

	stop := samplePosition(a)
	before := a.Snapshot()
	a.SetAudioQuality(bili.AudioMedium)

	waitFor(t, 30*time.Second, "徽章换到 128K", func() bool {
		return a.Snapshot().Quality == "128K"
	})
	// 换流是后台 goroutine 做的：徽章变了之后播放器可能还没换上，
	// 给它一点时间跑完。
	time.Sleep(2 * time.Second)

	if n := f.playCalls.Load(); n < 2 {
		t.Errorf("换档之后没有重新取流（接口只打了 %d 次）", n)
	}
	if a.Snapshot().Title != "换档的那首" {
		t.Errorf("换档把歌换掉了：%q", a.Snapshot().Title)
	}
	if a.AudioQuality() != bili.AudioMedium {
		t.Errorf("偏好 = %v，want AudioMedium", a.AudioQuality())
	}
	assertNoBacktrack(t, stop(), before.Position)
}

// TestSwitchingToATierThatIsAlreadyPlayingChangesNothing 盯「本来就是那一档，
// 别打断」。
//
// 用户从「自动」点「高音质」，而 B 站只给了 320K：拿回来的还是同一档。
// 这时候重建播放器就是白打断一首歌——歌会顿一下、进度条会跳一下，用户
// 什么都没换来。
//
// 判据是 **CDN 有没有被重新打一次**：重建播放器就要重新开一条字节源，
// 重新开源就会从 0 重新请求。什么都没换就不该有第二次请求。
func TestSwitchingToATierThatIsAlreadyPlayingChangesNothing(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	// 只有 320K 一档：自动档和高音质档拿到的都是它。
	f.onPlayURL(map[string]any{"duration": 1, "audio": []any{map[string]any{
		"id": int(bili.QualityHigh320), "bandwidth": 110836,
		"codecs": "flac", "mimeType": "audio/mp4", "baseUrl": cdnURL(f.URL, "a.flac", 2*time.Hour),
	}}})

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "本来就是那一档")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "本来就是那一档")
	waitFor(t, 30*time.Second, "歌放起来", func() bool { return a.Snapshot().Position > 500*time.Millisecond })

	// 先确认 CDN 已经被打过一次（字节源确实建起来了），再让它安顿一会儿：
	// fixture 一次就把整段发完，所以这里等的是「泵和播放器都落到稳态」。
	waitFor(t, 30*time.Second, "字节源开始下载", func() bool {
		return f.mediaHits.Load() >= 1 && a.Snapshot().Position > 0
	})
	time.Sleep(1500 * time.Millisecond)
	hits := f.mediaHits.Load()
	pos := a.Snapshot().Position

	a.SetAudioQuality(bili.AudioHigh)
	waitFor(t, 15*time.Second, "偏好已经改了", func() bool {
		return a.AudioQuality() == bili.AudioHigh
	})
	time.Sleep(2 * time.Second)

	if now := f.mediaHits.Load(); now > hits {
		t.Errorf("同一档却重建了播放链路：CDN 被多打了 %d 次", now-hits)
	}
	if got := a.Snapshot(); got.Position < pos {
		t.Errorf("位置从 %v 倒回 %v", pos.Round(time.Millisecond), got.Position.Round(time.Millisecond))
	}
	if got := a.Snapshot(); got.Err != "" {
		t.Errorf("播放条上冒出一句错：%s", got.Err)
	}
}

// TestA320KAACStreamFromCDNPlays 盯「320K AAC 在线可播」的那一整条链。
//
// 队列 → 取流 → CDN → 字节源 → 注册表 → 系统解码器 → 播放条徽章。
// 这里用的是**真的 AAC-in-MP4 素材**（`handles_test.go` 里那个 `m4a()` 现造
// 的），不是拿 FLAC 顶着：B 站的 320K 就是这个容器，而「本地放得了、在线放
// 不了」正是容器认错时最常见的表现。
//
// 首次可听时间的那个 200ms 断言在 `internal/service/audio`
// （`TestAAC320FromTheStreamIsAudibleFast`）——那一层能摘掉「等字节」的
// 时间，这一层摘不掉：取流是带签名的网络往返，那是规格明确不算进那个数里
// 的部分。
func TestA320KAACStreamFromCDNPlays(t *testing.T) {
	media, err := os.ReadFile(m4a(t))
	if err != nil {
		t.Fatal(err)
	}

	f := newStreamFixture(t)
	f.media = media
	f.onPlayURL(map[string]any{"duration": 1, "audio": []any{map[string]any{
		"id": int(bili.QualityHigh320), "bandwidth": 320000,
		"codecs": "mp4a.40.2", "mimeType": "audio/mp4",
		"baseUrl": cdnURL(f.URL, "a.m4a", 2*time.Hour),
	}}})

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "在线的 320K")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "在线的 320K")

	waitFor(t, 30*time.Second, "进度条往前走", func() bool {
		return a.Snapshot().Position > 0
	})
	st := a.Snapshot()
	if st.Err != "" {
		t.Errorf("播放条上有错：%s", st.Err)
	}
	if st.Quality != "320K" {
		t.Errorf("音质徽章 = %q，want 320K", st.Quality)
	}
	if st.Duration <= 0 {
		t.Errorf("时长 = %v，AAC 流应该报得出长度", st.Duration)
	}
}

// TestAskingForLosslessWithoutItKeepsPlaying 盯「要不到就别打断」。
//
// 点了「无损」而账号没有大会员：取流会失败。这时候**原来那条流要继续放**，
// 播放条上给出原因——而不是歌突然停了。
//
// 用户为无损付了钱，静默降级比报错更糟；而报错也不该以「歌停了」的形式出现，
// 那会让人以为是网络问题。
func TestAskingForLosslessWithoutItKeepsPlaying(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	// 没有 dash.flac 段：B 站只给了有损的。
	f.onPlayURL(twoTierDash(cdnURL(f.URL, "a.flac", 2*time.Hour)))

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "要不到无损的那首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "要不到无损的那首")
	waitFor(t, 30*time.Second, "播放位置动起来", func() bool {
		return a.Snapshot().Position > 300*time.Millisecond
	})

	a.SetAudioQuality(bili.AudioLossless)
	waitFor(t, 15*time.Second, "播放条上解释为什么没有无损", func() bool {
		return a.Snapshot().Err != ""
	})
	if got := a.Snapshot().Err; !strings.Contains(got, "无损") {
		t.Errorf("话里没说清是「要不到无损」：%q", got)
	}

	// 原来那条流还在放：位置继续往前走。
	before := a.Snapshot().Position
	waitFor(t, 30*time.Second, "原来那条流继续播", func() bool {
		return a.Snapshot().Position > before
	})
	if q := a.Snapshot().Quality; q != "320K" {
		t.Errorf("徽章变成了 %q——明明没换成无损，不该改徽章（也不该悄悄降级）", q)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 位置采样
// ─────────────────────────────────────────────────────────────────────────────

// samplePosition 开始每 50 ms 采一次播放位置。
//
// 采样窗口必须在触发换档**之前**就开始：反过来写（「重开之后采样，别掉回 0」）
// 会量到一个空窗口，而从头播一遍看起来完全正常。
func samplePosition(a *app.App) (stop func() []time.Duration) {
	ch := make(chan []time.Duration, 1)
	done := make(chan struct{})
	go func() {
		var out []time.Duration
		for {
			if p := a.Snapshot().Position; p > 0 {
				out = append(out, p)
			}
			select {
			case <-done:
				ch <- out
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}()
	var once sync.Once
	return func() []time.Duration {
		once.Do(func() { close(done) })
		return <-ch
	}
}

// assertNoBacktrack 断「位置不许倒退」。
//
// 相邻两次采样之间最多只走了 50ms，所以给 400ms 的余量（seek 落点按采样帧
// 算，会差几帧）。
func assertNoBacktrack(t *testing.T, samples []time.Duration, from time.Duration) {
	t.Helper()
	if len(samples) < 10 {
		t.Fatalf("只采到 %d 个位置，验不了什么", len(samples))
	}
	for i := 1; i < len(samples); i++ {
		if got, prev := samples[i], samples[i-1]; got < prev-400*time.Millisecond {
			t.Fatalf("第 %d 次采样位置从 %v 倒回 %v——这是「从头开始」，"+
				"不是「从原来的位置继续」", i,
				prev.Round(time.Millisecond), got.Round(time.Millisecond))
		}
	}
	if samples[0] < from-400*time.Millisecond {
		t.Errorf("开始采样时歌才放到 %v（切换前是 %v），采样窗口起点就不对",
			samples[0].Round(time.Millisecond), from.Round(time.Millisecond))
	}
}
