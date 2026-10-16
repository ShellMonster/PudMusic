package imgcache

import "testing"

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
