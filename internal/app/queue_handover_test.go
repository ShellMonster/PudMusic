package app_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 删掉正在放的那首、而**下一首是在线的、并且那条流建不起来**的那一支。
//
// 在线流是后台 goroutine 建的（取流要几百毫秒的网络往返），所以「开下一首」那一刻
// 界面上还没有新歌——**还响着的是刚刚从队列里删掉的那首**。不先停掉它的话，用户
// 看到的是：队列里已经没有它了、播放条上还是它、抽屉把下一首高亮成正在放，三处各说
// 各话；而网络那边只会在几十毫秒后报一句「取流失败」。
//
// fixture 让 playurl 一律回 503，于是这一支是确定的，不是靠等。

func TestRemovingTheCurrentStopsItBeforeTheOnlineNextIsBuilt(t *testing.T) {
	f := newStreamFixture(t)
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "取流失败", http.StatusServiceUnavailable)
	})

	dir := t.TempDir()
	a := newOnlineApp(t, f, dir)
	local := app.LocalItem(flac(t))
	if err := a.Enqueue(local); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, "本地那一首开始播", func() bool {
		return a.Snapshot().HasTrack
	})

	// 队列后面接一个在线项，然后删掉正在放的那首（本地那个）。
	a.Queue().Append(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1", Cid: "1", Title: "在线那一首",
	})
	if err := a.RemoveAt(0); err != nil {
		t.Fatalf("删掉正在放的那首：%v", err)
	}

	// 在线那一首建不起来（503），等它报完。
	waitFor(t, 30*time.Second, "在线那条流报出失败", func() bool { return a.Snapshot().Err != "" })

	st := a.Snapshot()
	if st.HasTrack {
		t.Errorf("下一首建不起来，删掉的那首还在放：%+v", st)
	}
	if cur, ok := a.Queue().Current(); !ok || cur.Title != "在线那一首" {
		t.Errorf("队列的当前项是 %+v，want 那一首在线的", cur)
	}
}
