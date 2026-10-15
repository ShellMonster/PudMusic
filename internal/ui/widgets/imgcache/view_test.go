package imgcache

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// 这一组验的是缓存的**界面出口**：三种状态（占位 / 就绪 / 失败）画出来的
// 东西不一样。跑的是 MyGo 的 Tester，和真窗口一模一样的绘制，只是画到内存。

// TestCoverShowsPlaceholderThenImage 是 1.11 的验收：先占位，图来了自己换上去。
func TestCoverShowsPlaceholderThenImage(t *testing.T) {
	var arrived atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		arrived.Add(1)
		<-release
		w.Write(pngBytes(t, 40, 40))
	}))
	defer srv.Close()

	c := New(Options{})
	t.Cleanup(c.Close)
	url := srv.URL + "/cover.png"

	tt := ui.NewTester(func(ctx *ui.Context) { View(ctx, c, url, 200, 120) }, 320, 200)
	if !tt.HasText("载入中") {
		t.Fatalf("第一帧没有占位，文字有 %q", tt.Texts())
	}
	if tt.HasText("封面加载失败") {
		t.Fatalf("还没下完就画了失败态，文字有 %q", tt.Texts())
	}

	waitFor(t, 5*time.Second, "请求发出去了", func() bool { return arrived.Load() > 0 })
	close(release)

	waitFor(t, 5*time.Second, "图下完之后占位换成了图", func() bool {
		tt.Frame()
		return !tt.HasText("载入中")
	})
	if tt.HasText("封面加载失败") {
		t.Errorf("下完了却画了失败态，文字有 %q", tt.Texts())
	}
}

// TestCoverShowsTheFailedState 验失败态画的是「失败」，不是一直转圈。
func TestCoverShowsTheFailedState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "没了", http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(Options{})
	t.Cleanup(c.Close)
	url := srv.URL + "/404.png"

	tt := ui.NewTester(func(ctx *ui.Context) { View(ctx, c, url, 200, 120) }, 320, 200)
	waitFor(t, 5*time.Second, "画出失败态", func() bool {
		tt.Frame()
		return tt.HasText("封面加载失败")
	})
	if tt.HasText("载入中") {
		t.Errorf("已经失败了还在占位，文字有 %q", tt.Texts())
	}
}

// TestCoverWithoutURLIsNotAskedFor 验没有地址时不发请求。
func TestCoverWithoutURLIsNotAskedFor(t *testing.T) {
	c := New(Options{})
	t.Cleanup(c.Close)

	tt := ui.NewTester(func(ctx *ui.Context) { View(ctx, c, "", 200, 120) }, 320, 200)
	if tt.HasText("封面加载失败") {
		t.Errorf("空地址不该画失败态，文字有 %q", tt.Texts())
	}
	n := 0
	c.entries.Range(func(_, _ any) bool { n++; return true })
	if n != 0 {
		t.Errorf("空地址也在缓存里落了 %d 条", n)
	}
}
