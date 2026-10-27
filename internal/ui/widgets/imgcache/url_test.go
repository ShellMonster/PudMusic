package imgcache

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestCoverURLUsesWebP 盯的是那个会静默失败的改动：.avif → .webp。
//
// MyGo 的解码器只有 PNG/JPEG/GIF/WebP/BMP 五种，AVIF 会返回
// "image: unknown format"，封面就永远出不来。B 站图床的裁剪参数与输出格式
// 是解耦的，同一个 @672w_378h_1c 换成 .webp 一样 200（实测 32KB）。
func TestCoverURLUsesWebP(t *testing.T) {
	const raw = "//i0.hdslb.com/bfs/archive/76b01753.jpg"

	got := CoverURL(raw, ParamsCover)
	want := "https://i0.hdslb.com/bfs/archive/76b01753.jpg@672w_378h_1c.webp"
	if got != want {
		t.Errorf("CoverURL 拼出 %q，want %q", got, want)
	}
}

// TestCoverURLKeepsBiuParamsSeparate 盯 Biu 那三处参数各不相同这件事。
//
// `@160w_160h_1c_1s.webp`（头像）、`@472w_264h_1c_!web-dynamic.webp`（迷你播放器）、
// `@672w_378h_1c`（大封面）不能当成一条规则批量替换——Biu 原来就是这么写的。
func TestCoverURLKeepsBiuParamsSeparate(t *testing.T) {
	const raw = "https://i0.hdslb.com/bfs/archive/a.jpg"
	seen := map[string]bool{}
	for _, p := range []string{ParamsCover, ParamsAvatar, ParamsMini} {
		if p == "" || seen[p] {
			t.Errorf("参数 %q 重复或为空", p)
		}
		seen[p] = true
		if got := CoverURL(raw, p); got != raw+"@"+p {
			t.Errorf("CoverURL(%q) = %q", p, got)
		}
	}
}

// TestCoverURLFollowsBiurule 照抄 Biu src/components/image/index.tsx:18-20。
func TestCoverURLFollowsBiuRule(t *testing.T) {
	tests := []struct{ name, src, params, want string }{
		{"协议相对补 https", "//i0.hdslb.com/bfs/a.jpg", ParamsCover, "https://i0.hdslb.com/bfs/a.jpg@" + ParamsCover},
		{"https 原样", "https://i0.hdslb.com/bfs/a.jpg", ParamsCover, "https://i0.hdslb.com/bfs/a.jpg@" + ParamsCover},
		{"已经带参数的不再拼", "https://i0.hdslb.com/bfs/a.jpg@160w_160h_1c_1s.webp", ParamsCover, "https://i0.hdslb.com/bfs/a.jpg@160w_160h_1c_1s.webp"},
		{"不是图床的不拼", "https://example.com/a.jpg", ParamsCover, "https://example.com/a.jpg"},
		{"没有参数就是原样", "https://i0.hdslb.com/bfs/a.jpg", "", "https://i0.hdslb.com/bfs/a.jpg"},
		{"空地址", "", ParamsCover, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CoverURL(tc.src, tc.params); got != tc.want {
				t.Errorf("CoverURL(%q, %q) = %q，want %q", tc.src, tc.params, got, tc.want)
			}
		})
	}
}

// TestAbsoluteURLMakesProtocolRelativeAddressesFetchable 钉住下载那一层的兜底
// （2026-10-27）。
//
// 🔴 **它修的是一个「永远出不来」**：服务层给的是协议相对地址
// （`//i0.hdslb.com/bfs/face/…`）时，`http.NewRequest` 会报
// `unsupported protocol scheme ""`，而 `isPermanent` 把它判成**永久失败**
// ——一次都不重试，界面上那一格永远是灰的。
//
// 服务层现在逐接口补了协议，但那是**手写**的（漏一处就复发），所以下载前
// 再兜一道。判据是**幂等**：补过的不许被改坏（`https://…` 原样返回）。
func TestAbsoluteURLMakesProtocolRelativeAddressesFetchable(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"//i0.hdslb.com/bfs/face/x.jpg", "https://i0.hdslb.com/bfs/face/x.jpg"},
		{"https://i0.hdslb.com/bfs/face/x.jpg", "https://i0.hdslb.com/bfs/face/x.jpg"},
		{"http://i0.hdslb.com/bfs/face/x.jpg", "http://i0.hdslb.com/bfs/face/x.jpg"},
		{"https://i0.hdslb.com/bfs/a.jpg@160w_160h_1c_1s.webp", "https://i0.hdslb.com/bfs/a.jpg@160w_160h_1c_1s.webp"},
		{"", ""},
	} {
		if got := absoluteURL(c.in); got != c.want {
			t.Errorf("absoluteURL(%q) = %q，want %q", c.in, got, c.want)
		}
	}
}

// TestProtocolRelativeAddressesAreNotPermanentFailures 钉住那一档**不再是永久失败**。
//
// ⚠️ **这一条走真缓存**：起一个假图床，用一个协议相对的地址去取——补协议之后
// 它必须真的发出请求（而不是在 `NewRequest` 那一步就落到 `permanentError`）。
func TestProtocolRelativeAddressesAreNotPermanentFailures(t *testing.T) {
	var hits atomic.Int64
	// ⚠️ **必须是 TLS 的假服务器**：`absoluteURL` 补的是 **https**，明文服务器的话
	// 那一趟会在握手上失败——而那样测出来的是「https 打不通」，不是「协议补对了」。
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes(t, 2, 2))
	}))
	t.Cleanup(srv.Close)

	// 把图片地址写成协议相对的形状：`//127.0.0.1:PORT/x.png`。
	protoRel := "//" + strings.TrimPrefix(srv.URL, "https://") + "/x.png"

	// ⚠️ **不用假钟**：这一条量的是「请求有没有真的发出去」，而退避那条路
	// 才需要假钟（`retry_test.go` 的 fakeClock）。第一次尝试是立刻发的。
	// `srv.Client()` trust 那张自签证书；缓存用它去请求。
	c := New(Options{MaxInflight: 1, Client: srv.Client()})
	t.Cleanup(c.Close)
	c.AddWake(func() {})

	c.Get(protoRel)
	// ⚠️ **判据是「假图床收到了请求」**，不是「有没有被唤醒」：唤醒在别的路径
	// 上也会触发（盘上命中也叫醒），而这一条要的正是**网络那一趟真的发出去了**。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if hits.Load() > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("协议相对的地址三秒都没发出请求——它多半在 NewRequest 那一步就被判成" +
		"永久失败了（那种失败一次都不重试，界面上永远是一块灰）")
}
