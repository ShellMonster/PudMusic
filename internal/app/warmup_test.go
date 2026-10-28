package app_test

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// 这一组盯「**预热签名密钥**」在**应用这一层**（票 #478 的「首次打开
// 特有」那一项）。
//
// # 它解决的是什么
//
// B 站的取流接口要 WBI 签名，而密钥**只缓存在内存里**——所以**每一次
// 冷启动，第一次取流都要多付一次 `nav` 往返**。而用户抱怨的正是
// 「首次打开取流慢」：那多出来的一次往返，正好落在「第一次点歌」的那一刻。
//
// 修法是启动的时候**在后台**把它取回来（`App.StartWarmUp`）。
//
// ⚠️ **服务层那一半**（密钥取回来没有、缓存生效没有）在
// `internal/service/bili/wbi_warm_test.go`；这一组盯的是**应用这一层
// 真的会去调它**。

// TestTheWarmUpActuallyHappens 盯「**预热真的发生了**」。
//
// 🔴 **判据是「那个 nav 请求发过了」**：预热的价值全在「**在第一次
// 签名请求之前**就把密钥拿到手」——而那正是 nav 请求发出去这件事。
//
// ⚠️ **它是后台的**（`StartWarmUp` 立刻就回来），所以这一条要**等**
// 那个请求出现，而不是假设它已经发生过了。
func TestTheWarmUpActuallyHappens(t *testing.T) {
	f := newStreamFixture(t)
	var navCalls atomic.Int64
	f.on("/x/web-interface/nav", func(w http.ResponseWriter, r *http.Request) {
		navCalls.Add(1)
		writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"wbi_img": map[string]any{
				"img_url": "https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png",
			}}})
	})

	a := newOnlineApp(t, f, t.TempDir())

	// 🔴 **预热之前**：一个 nav 请求都还没有。
	if n := navCalls.Load(); n != 0 {
		t.Fatalf("还没预热就已经发了 %d 个 nav 请求", n)
	}

	a.StartWarmUp()

	waitFor(t, 15*time.Second, "预热把签名密钥取回来了", func() bool {
		return navCalls.Load() > 0
	})
}

// TestTheWarmUpDoesNotBlockTheCaller 盯「**预热不挡事**」。
//
// ⚠️ **预热是后台的**：`StartWarmUp` 立刻就回来。而它挡事的话，
// 「双击 → 窗口出来」那一段就多了一个网络往返——而那一段本来就是
// 这一票要缩短的。
func TestTheWarmUpDoesNotBlockTheCaller(t *testing.T) {
	f := newStreamFixture(t)
	// 这台 nav **不立刻回**——而预热不该等它。
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	f.on("/x/web-interface/nav", func(w http.ResponseWriter, r *http.Request) {
		<-block
		writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"wbi_img": map[string]any{
				"img_url": "https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png",
			}}})
	})

	a := newOnlineApp(t, f, t.TempDir())

	done := make(chan struct{})
	go func() {
		a.StartWarmUp()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("预热把调用方堵住了——它必须是后台的")
	}
}

// TestTheWarmUpSurvivesAFailingNav 盯「**预热失败不挡事**」。
//
// ⚠️ **失败不是「没有 key」**：真正要签名的时候 `wbiKey` 会自己再取一次
// （见 `internal/service/bili/wbi.go`）。所以预热失败**不致命**——
// 但它得**说得出来**（`StartWarmUp` 会记一条日志）。
//
// 🔴 **这一条造的是「预热那一次撞上了毛病」**：nav 第一次回 500（预热
// 撞上了），第二次就正常（真正要签名的时候它自己再取一次）。
//
// ⚠️ **别把它读成「nav 一直坏着歌也能放」**：那不成立——签名拿不到密钥
// 时那个请求**就是过不去**（`signWBI` 拿不到 key 会把错原样还回去），
// 而那是「B 站那头」的事，不是「预热失败把应用带挂了」。
func TestTheWarmUpSurvivesAFailingNav(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	var navCalls atomic.Int64
	f.on("/x/web-interface/nav", func(w http.ResponseWriter, r *http.Request) {
		// 🔴 **只有第一次坏**：那是「预热撞上了毛病」。
		if navCalls.Add(1) == 1 {
			http.Error(w, "这台机器不给你", http.StatusInternalServerError)
			return
		}
		writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"wbi_img": map[string]any{
				"img_url": "https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png",
			}}})
	})
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour),
		int(bili.QualityHigh192), 110836))

	a := newOnlineApp(t, f, t.TempDir())
	a.StartWarmUp()

	waitFor(t, 15*time.Second, "预热试过了（失败了也算试过了）", func() bool {
		return navCalls.Load() > 0
	})

	// 🔴 **预热失败之后，歌照样放起来**：那一次取流要签名，而签名要密钥——
	// 密钥取不到时它会**自己再取一次**，而那一次是好的。
	if err := a.Enqueue(onlineItem("BV1", "1", "预热失败的那一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "预热失败的那一首")
}
