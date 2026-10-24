package app_test

// 「能不能重试」这个判据在 app 层的接线（票 #346）。
//
// # 这一组盯什么
//
// 判据本身在 `internal/service/bili/retry.go` 那张表里（唯一的一处）。这一层
// 盯的是**两处界面都用它**：
//
//	播放条上那枚「重试」	`App.failOnline` / `App.failPlay` → `bili.Recoverable`
//	下载记录页上那枚「重试」	`download.Options.CanRetry` → 也是 `bili.Recoverable`
//
// 两处是**同一个动作**（再问一次 B 站），所以两处的答案必须一样。
//
// ⚠️ **全部走内存里的假 B 站**（`streamFixture`），一条真接口都不打。

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/download"
)

// TestARateLimitedFailureIsNotOfferedARetry 盯限流（-509）那一档。
//
// 🔴 **限流和风控是同一档**（票 #346）：界面上那枚「重试」是**立刻**再问一次
// （`App.Retry` 里没有退避），而限流要等的是 B 站的限流窗口过去——立刻再问
// 还是「太快了」，还是撞同一面墙。
//
// 上一票（#316）只把**风控**收窄了，限流留在「可重试」那一档，于是「点一次
// 撞一次」的循环原样留着。
func TestARateLimitedFailureIsNotOfferedARetry(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		writeStreamJSON(w, map[string]any{"code": -509, "message": "操作太快了", "data": nil})
	})
	a := newOnlineApp(t, f, t.TempDir())

	if err := a.PlayFrom(onlineItem("BV1MbZnYoEk1", "29165486316", "这一首")); err != nil {
		t.Fatalf("放不进去：%v", err)
	}
	msg := waitForErr(t, a)
	if !strings.Contains(msg, "太快") {
		t.Errorf("播放条上那句话是 %q，没有说清是「操作太快了」", msg)
	}
	if a.Snapshot().CanRetry {
		t.Error("给了一枚按下去只会再撞一次限流的「重试」")
	}
}

// TestATruncatedResponseIsOfferedARetry 盯「响应被截断」那一档。
//
// 🔴 **这一档是上一票收窄时带出来的副作用**（票 #346）：「响应不是 JSON」
// 曾经被归到 `ErrRiskControl` 里，于是被截断的响应（多半是网络抖了一下，
// 再问一次有可能好）跟着风控一起被判成不可重试，丢了「重试」键。
func TestATruncatedResponseIsOfferedARetry(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 一份传了一半的 JSON。
		_, _ = w.Write([]byte(`{"code":0,"message":"0","data":{"dash`))
	})
	a := newOnlineApp(t, f, t.TempDir())

	if err := a.PlayFrom(onlineItem("BV1MbZnYoEk1", "29165486316", "这一首")); err != nil {
		t.Fatalf("放不进去：%v", err)
	}
	msg := waitForErr(t, a)
	if !strings.Contains(msg, "一半") {
		t.Errorf("播放条上那句话是 %q，没有说清是「只回了一半」", msg)
	}
	if !a.Snapshot().CanRetry {
		t.Errorf("这句话是 %q，却没有给「重试」——被截断的响应多半是网络抖了一下", msg)
	}
}

// TestARiskControlDownloadIsNotOfferedARetry 盯下载记录页那一枚。
//
// 🔴 **这一条是这一票的主线**：下载记录页上的「重试」本来是**另一套机制**
// （不走 `bili.Recoverable`），于是风控失败的下载**仍然**给着一枚立刻重新
// 取流的「重试」键——同一类「点一次撞一次」的循环留在下载页上。
func TestARiskControlDownloadIsNotOfferedARetry(t *testing.T) {
	f := newStreamFixture(t)
	serveRanged(f, []byte("12345678"))
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		writeStreamJSON(w, map[string]any{"code": -352, "message": "风控", "data": nil})
	})
	a := newDownloadApp(t, f, t.TempDir(), filepath.Join(t.TempDir(), "queue.json"))

	snap, err := a.Download(onlineItem("BV1", "c1", "歌"))
	if err != nil {
		t.Fatalf("放不进去：%v", err)
	}
	waitFor(t, 20*time.Second, "任务出错", func() bool {
		g, ok := mustGet(a, snap.ID)
		return ok && g.Status == download.StatusError
	})
	got, _ := mustGet(a, snap.ID)
	if got.CanRetry {
		t.Error("风控失败的下载仍然给了一枚「重试」——那枚键是立刻重新取流，按下去就是再撞一次墙")
	}
}

// TestANetworkFailureInADownloadIsStillOfferedARetry 盯**另一半**。
//
// 🔴 **接上同一张表不等于把所有失败都判成不可重试**：网络断了、地址过期了
// ——再问一次就有可能好，那一枚「重试」是**有用**的，给了才是对的。
func TestANetworkFailureInADownloadIsStillOfferedARetry(t *testing.T) {
	f := newStreamFixture(t)
	serveRanged(f, []byte("12345678"))
	// 取流那一趟服务端炸了——这就是「没取到」那一类。
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	a := newDownloadApp(t, f, t.TempDir(), filepath.Join(t.TempDir(), "queue.json"))

	snap, err := a.Download(onlineItem("BV1", "c1", "歌"))
	if err != nil {
		t.Fatalf("放不进去：%v", err)
	}
	waitFor(t, 20*time.Second, "任务出错", func() bool {
		g, ok := mustGet(a, snap.ID)
		return ok && g.Status == download.StatusError
	})
	got, _ := mustGet(a, snap.ID)
	if !got.CanRetry {
		t.Errorf("这一次失败是 %q，却没有给「重试」——网络断了，再试一次就有可能好", got.Err)
	}
}
