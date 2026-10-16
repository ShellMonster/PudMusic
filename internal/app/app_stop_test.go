package app_test

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// `App.Stop` 的代次那道守卫：停掉之后，在途的在线流**不许**再把歌装回来。
//
// 症状很好认：用户在抽屉里删掉了正在放的那一首、或者按了「清空」，队列空了、
// 播放条空了，然后过一会儿**那首歌自己又放起来了**——而它已经不在队列里了。
// 那正是 Stop 要避免的「界面说的和它做的不一致」。
//
// 在途的那条流没法掐掉（取流 + 下载 + 起解码器都是后台 goroutine），唯一的
// 办法是让它的结果**过期**：App 每开一次在线流加一代，runOnline / failOnline /
// noteQuality 三处回来时都先对一遍（`online.go`）。Stop 不加这一代，它们就还对
// 得上——于是照样装上去。

func TestStopDiscardsAnOnlineLoadStillInFlight(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh320), 110836))

	// 把 CDN 那一段卡住：请求发出去了、handler 停在这儿不动，于是取流那条线
	// 真的停在「流还在建」的状态上。
	gate := make(chan struct{})
	var hits atomic.Int32
	released := false
	t.Cleanup(func() {
		if !released {
			close(gate)
		}
	})
	// hits 数请求打到了这里——**在卡住之前**数：卡在 serveMedia 前面的话，
	// fixture 自己那个计数器永远停在 0，等不到。
	f.on("/cdn/a.flac", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-gate
		f.serveMedia(w, r)
	})

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.PlayFrom(onlineItem("BV1", "1", "在线那一首")); err != nil {
		t.Fatal(err)
	}
	// 等到取流真的打到了 CDN。在那之前 Stop 停掉的是「还没开始取流」，那一条
	// 压根用不着代次。
	waitFor(t, 30*time.Second, "取流打到 CDN", func() bool { return hits.Load() > 0 })

	// 用户在抽屉里把这一首删掉了（队列里就这一首 → 停下来）。
	a.ClearQueue()

	// 放行：在途的那条流现在才建好，才有机会往 App 上装。
	released = true
	close(gate)

	// 给它落地的时间。落地之前状态当然是对的，所以这一步是「等它有机会犯」。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if st := a.Snapshot(); st.HasTrack || st.Title != "" {
			t.Fatalf("停掉 %.0fms 之后那首歌又放起来了：%+v",
				float64(3*time.Second-time.Until(deadline))/float64(time.Millisecond), st)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := a.Queue().Len(); n != 0 {
		t.Errorf("队列里又多了 %d 首", n)
	}
}

// TestStopOnAnIdleAppIsHarmless 钉「没在放的时候连着停几次也没事」。
//
// 「清空」「从列表删除」「直接 Stop」是三条会落到 Stop 上的路，用户连点几下
// 或者代码两条路都走一遍都很平常。它不许报错，也不许凭空改出什么状态。
func TestStopOnAnIdleAppIsHarmless(t *testing.T) {
	f := newStreamFixture(t)
	a := newOnlineApp(t, f, t.TempDir())

	a.ClearQueue()
	a.Stop()
	a.ClearQueue()
	a.Stop()

	if st := a.Snapshot(); st.HasTrack || st.Err != "" {
		t.Errorf("什么都没放的时候连着停，状态变了：%+v", st)
	}
	if n := a.Queue().Len(); n != 0 {
		t.Errorf("队列里有 %d 首", n)
	}
}
