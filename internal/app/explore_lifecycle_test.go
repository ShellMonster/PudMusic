package app

import (
	"net/http"
	"sync/atomic"
	"testing"
)

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
