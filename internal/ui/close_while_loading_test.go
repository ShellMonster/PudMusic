package ui

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 「封面还在下就关窗口」在**真外壳**上走一遍。
//
// 为什么 imgcache 那两条不够、还要这一条：它们量的是缓存自己（会不会卡住、
// 会不会还去叫醒窗口）。这一条量的是**收尾那一串**——
//
//	关窗口 → images.Close() → app.Close() → 之后又来了一帧
//
// 这一串是**在生产里连着调的**（shell.go 的 `w.OnClosed`），而 view 里 panic
// 会穿透 Cocoa 事件循环直接杀死进程（铁律三）。也就是说：收尾里任何一次
// 不合时宜的唤醒，任何一个已经失效的句柄被碰一下，症状都是**整个应用没了**
// ——不是「这一帧画歪了」。
//
// 所以这一条钉的是「不崩」，并且把三样东西分开验：收尾不卡、请求真被取消、
// 之后再画一帧不炸。

// TestClosingTheWindowWhileTheCoverIsLoading 盯这一条收尾链。
func TestClosingTheWindowWhileTheCoverIsLoading(t *testing.T) {
	arrived := make(chan struct{})
	gone := make(chan struct{})
	var once, once2 sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(arrived) })
		<-r.Context().Done() // 客户端取消 = 连接断 = 这里醒
		once2.Do(func() { close(gone) })
	}))
	// 收尾：先让服务端醒过来再关它，否则 Close 会等一个还在等的 handler。
	t.Cleanup(func() {
		select {
		case <-gone:
		default:
			close(gone)
		}
		srv.Close()
	})

	a := app.New()
	b := newTestShell(t, onApp(a), at(760, coverWinH),
		withView(func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) {
				s.wake.set(c) // 和 Shell.View 每帧开头做的事一样
				s.View(c)
			}
		}))
	s, tt := b.s, b.tt
	if err := a.Enqueue(app.Item{
		Source: "local", Type: "local", Path: flacPath(t),
		Title: "封面正下着的一首", Cover: srv.URL + "/cover.png",
	}); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停：不让播放器那个 50ms 的时钟来搅局
	// ⚠️ 必须画一帧，封面请求才发得出去：封面缓存是**被 view 要**的时候才开下载
	// 的（`imgcache.View` 拿着地址去 `Get`，那儿 LoadOrStore 之后起后台线）。
	// `mygo.NewTester` 造完画的那一帧那时候还没这首歌，所以这一帧是补上的。
	tt.Frame()

	select {
	case <-arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("封面请求一直没发出去，这条测的不是「下着的时候关窗口」")
	}

	// 走**真窗口关掉时的那一份收尾**（`Shell.Close`，窗口的 OnClosed 调的就是
	// 它）。这一条以前是照着 OnClosed 抄的三步，所以 OnClosed 改了它不会红——
	// 现在直接调同一份，OnClosed 改了这里就会红。
	done := make(chan struct{})
	go func() {
		s.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("关窗收尾卡住了：用户会觉得应用退不出去")
	}

	select {
	case <-gone:
	case <-time.After(10 * time.Second):
		t.Error("退出之后封面请求还挂着：靠超时收的，不是靠取消")
	}

	// 之后再画一帧：迟到的唤醒、失效的句柄、关掉的缓存，全在这一帧里碰。
	// 这里要是不炸，「迟到的结果打不开任何东西」这件事就有证据了。
	tt.Frame()
	if tt.HasText("封面加载失败") {
		t.Errorf("退出之后还画出了失败态，文字有 %q", tt.Texts())
	}
}
