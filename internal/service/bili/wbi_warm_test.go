package bili

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// 这一组盯「**预热签名密钥**」那一条（票 #478 的「首次打开特有」那一项）。
//
// # 它解决的是什么
//
// 密钥**只缓存在内存里**（两小时，见 `wbiKeyTTL`），所以**每一次冷启动，
// 第一次签名请求都要多付一次 `nav` 往返**。而用户抱怨的正是「首次打开
// 取流慢」——那多出来的一次往返，正好落在「第一次点歌」的那一刻。
//
// 修法是启动的时候在后台先把它取回来（`app.StartWarmUp` 调它）。

// TestWarmWBIKeyFetchesTheKeyAheadOfTime 盯「预热真的把密钥取回来了」。
//
// 🔴 **判据是「那个 nav 请求发过了」**：预热的价值全在「**在第一次签名
// 请求之前**就把密钥拿到手」——而那正是 nav 请求发出去这件事。
func TestWarmWBIKeyFetchesTheKeyAheadOfTime(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"wbi_img": map[string]string{
				"img_url": "https://i0.hdslb.com/bfs/wbi/" + sampleImgKey + ".png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/" + sampleSubKey + ".png",
			},
		}))
	})
	c := f.client(t, Options{
		Now: func() time.Time { return time.Unix(1702204169, 0) },
	})

	// 🔴 **预热之前**：一个签名请求都还没有，那一次 nav 往返还在后面。
	if n := len(f.requests()); n != 0 {
		t.Fatalf("还没预热就已经发了 %d 个请求", n)
	}
	if err := c.WarmWBIKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(f.requests()); n != 1 {
		t.Fatalf("预热之后应该正好发了一个 nav 请求，却发了 %d 个", n)
	}

	// 🔴 **预热之后**：签名请求**不再**要那一次往返——密钥已经在手边了。
	// 而这正是预热的全部价值：第一次取流回到**一次**往返。
	if _, err := c.signWBI(context.Background(), map[string][]string{"foo": {"114"}}); err != nil {
		t.Fatal(err)
	}
	if n := len(f.requests()); n != 1 {
		t.Errorf("预热之后签名又去取了一遍密钥（nav 请求 %d 个）——预热白做了", n)
	}
}

// TestWarmWBIKeyDoesNotSendASecondRequestWhenTheKeyIsThere 盯「**已经取过
// 的密钥不会被再取一遍**」。
//
// ⚠️ **并发重取会直接撞上 -412 风控**：B 站的 nav 接口对短时间内的重复
// 请求很敏感。而预热和第一次取流**很可能同时在跑**（预热是后台的，
// 用户点歌是前台）——所以「已经在取」的时候后来的人要**等**那一次，
// 而不是各发各的。
func TestWarmWBIKeyDoesNotSendASecondRequestWhenTheKeyIsThere(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"wbi_img": map[string]string{
				"img_url": "https://i0.hdslb.com/bfs/wbi/" + sampleImgKey + ".png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/" + sampleSubKey + ".png",
			},
		}))
	})
	c := f.client(t, Options{
		Now: func() time.Time { return time.Unix(1702204169, 0) },
	})

	for range 3 {
		if err := c.WarmWBIKey(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(f.requests()); n != 1 {
		t.Errorf("预热了三遍却发了 %d 个 nav 请求——密钥是缓存着的", n)
	}
}

// TestWarmWBIKeySaysSoWhenItFails 盯「**预热失败要说得出来**」。
//
// ⚠️ **失败不是「没有 key」**：它是一个错。而调用方（`app.StartWarmUp`）
// 只记一条日志——**真正要签名的时候 `wbiKey` 会自己再取一次**，所以
// 这里的失败**不致命**，但它**不许被吞掉**（吞掉的话，启动时那次失败
// 就没有人知道了）。
func TestWarmWBIKeySaysSoWhenItFails(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "这台机器不给你", http.StatusInternalServerError)
	})
	c := f.client(t, Options{
		Now: func() time.Time { return time.Unix(1702204169, 0) },
	})

	if err := c.WarmWBIKey(context.Background()); err == nil {
		t.Error("预热失败了却没报错——那一句失败就没人知道了")
	}
}
