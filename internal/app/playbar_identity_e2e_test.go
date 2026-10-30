package app_test

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// TestPlaybarDoesNotLinkThePendingSongToThePreviousLocalTitle 是用户从 UP 投稿行点 B
// 时的端到端播放条回归：本地 A 仍在播放、B 的真实取流链路尚未完成时，
// 标题和 B 站链接不能来自两首不同的歌。
func TestPlaybarDoesNotLinkThePendingSongToThePreviousLocalTitle(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "b.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))

	gate := make(chan struct{})
	var hits atomic.Int32
	released := false
	t.Cleanup(func() {
		if !released {
			close(gate)
		}
	})
	f.on("/cdn/b.flac", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-gate
		f.serveMedia(w, r)
	})

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	if got := a.Snapshot().Title; got != "stereo16" {
		t.Fatalf("本地 A 没有装上：标题=%q", got)
	}

	// 这就是 UP 投稿行最终递交给 App 的完整播放项：投稿身份、分 P 和显示标题
	// 都来自同一行。
	if err := a.PlayFrom(onlineItem("BVB", "2", "在线 B")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, "B 的取流打到 CDN", func() bool { return hits.Load() > 0 })

	st := a.Snapshot()
	if !st.Loading {
		t.Fatalf("B 没有处于取流中：%+v", st)
	}
	if st.Title != "stereo16" {
		t.Errorf("B 取流期间播放条标题 = %q，want 本地 A", st.Title)
	}
	if st.BiliURL != "" {
		t.Errorf("B 取流期间播放条链接 = %q，不能把待播 B 挂到 A 的标题上", st.BiliURL)
	}

	released = true
	close(gate)
	playing(t, a, "在线 B")
	if got := a.Snapshot().BiliURL; got != "https://www.bilibili.com/video/BVB" {
		t.Errorf("B 装上后播放条链接 = %q，want B 的链接", got)
	}
}
