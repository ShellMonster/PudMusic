package app

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

func TestRemountExploreInvalidatesKeyedStateAndLateResults(t *testing.T) {
	oldB := newExploreBili(t)
	newB := newExploreBili(t)
	oldStarted := make(chan struct{})
	oldRelease := make(chan struct{})
	oldFinished := make(chan struct{})
	newFinished := make(chan struct{})
	oldB.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		close(oldStarted)
		<-oldRelease
		writeExploreJSON(w, 0, map[string]any{"mid": 42, "name": "旧客户端"})
		close(oldFinished)
	})
	newB.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"mid": 42, "name": "新客户端"})
		close(newFinished)
	})

	a := New()
	t.Cleanup(a.Close)
	oldCLI := newExploreClient(oldB)
	newCLI := newExploreClient(newB)
	a.MountLogin(NewMemoryStore("test"), oldCLI)
	a.MountExplore(oldCLI)
	var wakes atomic.Int32
	a.SetWake(func() { wakes.Add(1) })

	oldSpace := a.UpSpaceOf(42)
	oldSpace.Load()
	waitExploreSignal(t, oldStarted, "旧客户端请求发出")

	a.MountExplore(newCLI)
	newSpace := a.UpSpaceOf(42)
	if newSpace == oldSpace {
		t.Fatal("重挂载后复用了旧的 UP 空间状态")
	}
	if got := newSpace.Snapshot(); got.Loaded || got.Loading || got.User.Name != "" {
		t.Fatalf("新挂载继承了旧的 UP 空间状态：%+v", got)
	}

	newSpace.Load()
	waitExploreSignal(t, newFinished, "新客户端请求结束")
	waitExplore(t, newSpace)
	if got := newSpace.Snapshot().User.Name; got != "新客户端" {
		t.Fatalf("新客户端没有写入新的 UP 空间状态：%q", got)
	}
	wakesBeforeOldResult := wakes.Load()

	close(oldRelease)
	waitExploreSignal(t, oldFinished, "旧客户端迟到请求结束")
	if got := oldSpace.Snapshot(); got.Loaded || got.Loading || got.User.Name != "" {
		t.Fatalf("旧客户端迟到结果写回了已失效空间：%+v", got)
	}
	if got := wakes.Load(); got != wakesBeforeOldResult {
		t.Fatalf("旧客户端迟到结果唤醒了新挂载窗口 %d 次，want %d", got, wakesBeforeOldResult)
	}
}

func newExploreClient(b *exploreBili) *bili.Client {
	cli := bili.NewClient(bili.Options{Transport: b.Server.Client().Transport})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	return cli
}

func waitExploreSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("等待%s超时", what)
	}
}

func TestClosedAppDiscardsLateSpaceResult(t *testing.T) {
	b := newExploreBili(t)
	b.mu.Lock()
	b.delayMS = 100
	b.mu.Unlock()
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"mid": 42, "name": "迟到的 UP"})
	})
	a := newExploreApp(t, b)
	var wakes atomic.Int32
	a.SetWake(func() { wakes.Add(1) })
	sp := a.UpSpaceOf(42)
	sp.Load()
	a.Close()
	for i := 0; i < 100; i++ {
		waitExploreSleep()
	}
	if got := sp.Snapshot(); got.Loaded || got.User.Name != "" {
		t.Fatalf("关闭后的迟到结果写回旧空间：%+v", got)
	}
	if got := wakes.Load(); got != 0 {
		t.Fatalf("关闭后的迟到结果唤醒了窗口 %d 次", got)
	}
}
