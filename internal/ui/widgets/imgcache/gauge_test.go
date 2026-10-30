package imgcache

import (
	"image/color"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/perf"
)

// TestTheCacheReportsItsBytesToTheProcessGauges pins the measurement seam: the
// diagnostic snapshot must see how much the cover cache holds without perf
// importing the UI package, so the cache itself reports every change of its
// byte account.
func TestTheCacheReportsItsBytesToTheProcessGauges(t *testing.T) {
	before := perf.ProcessGauges().Snapshot().ImageCacheBytes

	body := pngBytes(t, 40, 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	c := New(Options{})
	url := srv.URL + "/gauged.png"
	waitState(t, c, url, 5*time.Second, loaded)

	used := c.UsedBytes()
	if used <= 0 {
		t.Fatalf("缓存解码完之后账上是 %d 字节", used)
	}
	if got, want := perf.ProcessGauges().Snapshot().ImageCacheBytes-before, used; got != want {
		t.Fatalf("进程字节表上的封面缓存是 %d，缓存自己的账是 %d", got, want)
	}

	c.Close()
	if got := perf.ProcessGauges().Snapshot().ImageCacheBytes - before; got != 0 {
		t.Fatalf("关掉缓存之后进程字节表上还剩 %d 字节", got)
	}
}

// TestReleasedPixelsLeaveTheProcessGauges pins the other direction: when a
// cover's pixels are dropped, the process-wide number has to go down too.
// A gauge that only ever grows would be a counter wearing a gauge's name.
func TestReleasedPixelsLeaveTheProcessGauges(t *testing.T) {
	before := perf.ProcessGauges().Snapshot().ImageCacheBytes

	var hits atomic.Int32
	body := releasePNG(t, 32, 32, color.RGBA{R: 1, G: 2, B: 3, A: 0xff})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Write(body)
	}))
	defer srv.Close()

	c := New(Options{ReleaseAfterFrames: 4})
	t.Cleanup(c.Close)
	url := srv.URL + "/gauged-release.png"

	now := time.Unix(1_700_000_000, 0)
	c.BeginFrame(now)
	waitState(t, c, url, 5*time.Second, loaded)
	if got := perf.ProcessGauges().Snapshot().ImageCacheBytes - before; got <= 0 {
		t.Fatalf("解出来之后进程字节表上没有这一张的占用（%d）", got)
	}

	advanceFrames(c, now, 5)
	if got := perf.ProcessGauges().Snapshot().ImageCacheBytes - before; got != 0 {
		t.Fatalf("像素放掉之后进程字节表上还剩 %d 字节", got)
	}
}
