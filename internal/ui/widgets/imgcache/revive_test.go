package imgcache

import (
	"image/color"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

// TestReleasedCoverStillHeldElsewhereRevivesWithoutPlaceholder 锁定：像素被放掉
// 的封面如果别处（正在播放、迷你窗口）还拿着那个 Bitmap，行重新挂载时必须当帧
// 就是 Ready 且是同一个 Bitmap。曾经出错的行为：一律退回占位，再重新解码一次，
// 那一格闪一两帧。
func TestReleasedCoverStillHeldElsewhereRevivesWithoutPlaceholder(t *testing.T) {
	body := releasePNG(t, 24, 24, color.RGBA{R: 200, G: 40, B: 40, A: 0xff})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(body) }))
	defer srv.Close()

	c := New(Options{ReleaseAfterFrames: 4})
	t.Cleanup(c.Close)
	url := srv.URL + "/held.png"

	now := time.Unix(1_700_000_000, 0)
	c.BeginFrame(now)
	held := waitState(t, c, url, 5*time.Second, loaded).Bitmap
	want := c.UsedBytes()

	advanceFrames(c, now, 5)
	if !releasedPixels(t, c, url) || c.UsedBytes() != 0 {
		t.Fatal("闲置之后条目应当已放掉像素、账归零")
	}

	s := c.Get(url)
	if !s.Ready() || s.Bitmap != held {
		t.Fatalf("别处还拿着位图时回看应当当帧复活：Ready=%v same=%v", s.Ready(), s.Bitmap == held)
	}
	if got := c.UsedBytes(); got != want {
		t.Errorf("复活后账上是 %d 字节，want %d", got, want)
	}
	c.lifeWG.Wait()
	if _, redec := c.ReleaseStats(); redec != 0 {
		t.Errorf("复活不应触发重新解码，实际 %d 次", redec)
	}
	runtime.KeepAlive(held)
}

// TestReleasedCoverNobodyHoldsStaysReleased 锁定 #673 的内存收益：没人拿着时，
// GC 之后位图真的被回收，回看走重新解码，不会被弱引用拖住。
func TestReleasedCoverNobodyHoldsStaysReleased(t *testing.T) {
	body := releasePNG(t, 24, 24, color.RGBA{R: 40, G: 200, B: 40, A: 0xff})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(body) }))
	defer srv.Close()

	c := New(Options{ReleaseAfterFrames: 4})
	t.Cleanup(c.Close)
	url := srv.URL + "/unheld.png"

	now := time.Unix(1_700_000_000, 0)
	c.BeginFrame(now)
	waitState(t, c, url, 5*time.Second, loaded)
	advanceFrames(c, now, 5)
	if c.UsedBytes() != 0 {
		t.Fatal("闲置之后账应归零")
	}
	for range 3 {
		runtime.GC()
	}
	waitState(t, c, url, 5*time.Second, loaded)
	if _, redec := c.ReleaseStats(); redec != 1 {
		t.Errorf("没人持有时应走一次重新解码，实际 %d", redec)
	}
}
