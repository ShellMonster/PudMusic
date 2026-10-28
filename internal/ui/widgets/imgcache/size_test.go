package imgcache

// 这一组钉住「**按显示尺寸要图**」。
//
// # 为什么这件事值一条测试
//
// 一张封面在内存里占多少，只取决于**解出来的像素**（RGBA，每像素 4 字节），
// 和它在网上是 WebP 还是 JPEG 无关——压缩格式省的是传输和磁盘。
//
// 而我们的用法一直是「解原图（1920×1080 → 8.3MB）→ 缩到 768 → 扔掉原图」：
// 每张付一次 ~37ms 的「解全图」，只为留一张 768 的。而界面上绝大多数位置
// 根本用不到 768——列表行 48 DIP、播放条 52 DIP，在 Retina 上是 96~104 像素。
//
// 实测（本机真实缓存 794 张、均值 284KB）：
//
//	解原图 37.2ms · 缩放 1.9ms · JPEG 编码 2.4ms
//
// 所以「让图床给一张刚好够大的」是唯一同时省内存、省 CPU、省带宽的办法。
// Flutter 那份文档把同一件事写在 `cacheWidth` / `cacheHeight` 上（4K 图
// 30MB → 按显示尺寸解码 330KB，100 倍）。
//
// # 这一组量什么
//
// `coverPixelSize(w, h)`：界面给的**显示尺寸（DIP）**换成**该向图床要的像素**。
// 它必须是「显示尺寸 × 屏幕倍率」，而不是一个固定档位——固定档位是这一组
// 要修的那个错（768 对所有位置都太大）。

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestTheRequestedPixelSizeFollowsTheDisplaySize 钉住换算。
//
// 判据：请求的像素 **不小于** 显示像素（小了会模糊），也**不该远超**它
// （超了就是白占内存——这一组存在的全部理由）。
func TestTheRequestedPixelSizeFollowsTheDisplaySize(t *testing.T) {
	for _, tc := range []struct {
		name    string
		w, h    float32
		scale   float32
		wantMin int
		wantMax int
	}{
		// 列表行 48 DIP，Retina 2x：96 像素够了。
		{"列表行 48", 48, 48, 2, 96, 128},
		// 播放条 52 DIP。
		{"播放条 52", 52, 52, 2, 104, 136},
		// 歌单页 200×168。
		{"歌单页", 200, 168, 2, 336, 420},
		// 满屏 384×288：这一处本来就是 768 长边，所以它该拿到 768。
		{"满屏", 384, 288, 2, 576, 800},
		// 非 Retina（1x）：只按 1 倍要。
		{"列表行 1x", 48, 48, 1, 48, 64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotW, gotH := coverPixelSize(tc.w, tc.h, tc.scale)
			long := max(gotW, gotH)
			if long < tc.wantMin {
				t.Errorf("要 %dx%d 像素，长边 %d 小于显示需要的 %d——图会糊",
					gotW, gotH, long, tc.wantMin)
			}
			if long > tc.wantMax {
				t.Errorf("要 %dx%d 像素，长边 %d 超过 %d——"+
					"这一格只显示 %g×%g DIP，多要的像素全在内存里占着（每像素 4 字节）",
					gotW, gotH, long, tc.wantMax, tc.w, tc.h)
			}
		})
	}
}

// TestTheRequestedPixelSizeIsCapped 钉住上限：不管显示多大，都不要超过
// `maxStoredSide`——那是磁盘和内存两边共同的上限，一张 4K 的封面图床也不该给。
func TestTheRequestedPixelSizeIsCapped(t *testing.T) {
	w, h := coverPixelSize(2000, 1200, 2)
	if max(w, h) > maxStoredSide {
		t.Errorf("要 %dx%d 像素，超过 maxStoredSide（%d）——"+
			"超出的部分既不显示也白占内存", w, h, maxStoredSide)
	}
}

// TestTheSmallSizeIsSnappedToTheCDNTiers 钉住「向图床要的尺寸落在它认得的档上」。
//
// B 站图床按 `@96w_96h_1c.webp` 这种后缀裁剪。**不能每次都拼一个任意数字**：
// 那样两个只差一像素的显示尺寸（比如窗口缩放导致的 95 和 96）会变成两个不同的
// URL、两份缓存、两次下载——而它们画出来是同一张图。
//
// 所以按**固定档位**向上取整，档位之间的差至少 25%，避免「差一点点就多一份」。
func TestTheSmallSizeIsSnappedToTheCDNTiers(t *testing.T) {
	prev := 0
	for _, req := range []int{30, 60, 96, 128, 160, 200, 240, 300, 400, 500, 600, 768} {
		got := snapToTier(req)
		if got < req {
			t.Errorf("%d 被吸到 %d，比要的还小——图会糊", req, got)
		}
		if prev > 0 && got < prev {
			t.Errorf("档位不单调：%d → %d", prev, got)
		}
		prev = got
	}
	// 两个接近的值必须落到同一档（否则缓存里会有同一张图的多个副本）。
	if a, b := snapToTier(94), snapToTier(96); a != b {
		t.Errorf("94 和 96 落到不同档（%d / %d）——"+
			"这两个尺寸画出来是同一张图，却会各下一份、各占一份内存", a, b)
	}
}

// TestTheSizedURLCarriesTheRequest 钉住「按尺寸拼出来的地址」长什么样。
//
// 判据是**能被图床认出来的那一串**：`@<宽>w_<高>h_1c.webp`。实测（本机 curl
// 打 i0.hdslb.com）96 / 160 / 200 / 400 都有 200 响应——图床接受任意尺寸，
// 但**不会放大**（给一张 200×200 的原图要 400，回来的还是 200）。
//
// ⚠️ **原样返回的三种情况**（和 `CoverURL` 同一条规矩）：
//   - 不是 B 站图床（`/bfs/`）：别的图床没有这套裁剪参数，拼上去就 404；
//   - 已经带过 `@` 的：服务层可能已经拼过一档（头像就是），再拼一层是坏地址；
//   - 尺寸是 0/负：调用方量不出显示尺寸，那时候要原图而不是要一个 1×1。
func TestTheSizedURLCarriesTheRequest(t *testing.T) {
	const bfs = "https://i0.hdslb.com/bfs/archive/abc.jpg"
	for _, tc := range []struct {
		name string
		url  string
		w, h int
		want string
	}{
		{"普通的封面", bfs, 96, 96, bfs + "@96w_96h_1c.webp"},
		{"长图", bfs, 200, 168, bfs + "@200w_168h_1c.webp"},
		{"已经是 https 的", bfs, 128, 128, bfs + "@128w_128h_1c.webp"},
		{"已经带过参数", bfs + "@672w_378h_1c.webp", 96, 96, bfs + "@672w_378h_1c.webp"},
		{"不是 B 站图床", "https://example.com/a.jpg", 96, 96, "https://example.com/a.jpg"},
		{"尺寸为 0", bfs, 0, 0, bfs},
		{"协议相对", "//i0.hdslb.com/bfs/a.jpg", 96, 96, "https://i0.hdslb.com/bfs/a.jpg@96w_96h_1c.webp"},
		{"空串", "", 96, 96, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sizedURL(tc.url, tc.w, tc.h); got != tc.want {
				t.Errorf("sizedURL(%q, %d, %d)\n  得到 %q\n  想要 %q", tc.url, tc.w, tc.h, got, tc.want)
			}
		})
	}
}

// TestSizedCoversAreTheirOwnCacheEntry 钉住「同一个封面、两个尺寸 = 两格缓存」。
//
// 这是「按用途要图」能成立的前提：列表行要 96、歌单页要 400，它们**必须**是
// 两个不同的 key——否则先来的那个尺寸会把另一个挤掉，界面上一会儿糊一会儿清。
//
// ⚠️ 反过来也要成立：**同一个尺寸请求两次要是同一格**（不然一屏十几行同名
// 封面会各下一份）。
func TestSizedCoversAreTheirOwnCacheEntry(t *testing.T) {
	base := "https://i0.hdslb.com/bfs/archive/x.jpg"
	small := sizedURL(base, 96, 96)
	big := sizedURL(base, 400, 400)
	if small == big {
		t.Fatalf("两个尺寸拼出了同一个地址 %q——它们会互相挤掉", small)
	}
	if again := sizedURL(base, 96, 96); again != small {
		t.Errorf("同一个尺寸两次拼出不同地址：%q vs %q", again, small)
	}
}

// TestASmallCellAsksForASmallImage 钉住这一票的**全部价值**：小格子向图床要小图。
//
// # 它量的是「请求了什么」
//
// 起一个假图床，记下它收到的路径。摆一帧 48×48 的封面（列表行那一档），
// 然后看服务端收到的是不是 `@96w_96h_1c.webp`（48 DIP × 2 倍屏）。
//
// # 为什么这一条比「尺寸函数对不对」更硬
//
// 换算函数（`coverPixelSize`）测过了，但**它有没有真的接到请求上**是另一件事：
// `drawCover` 里少写一句 `GetSized`、或者 `SetScale` 没被喂，换算再对也不会
// 影响任何一次下载——而这种断裂在画面上看不出来（图还是出来了，只是大）。
func TestASmallCellAsksForASmallImage(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "image/png")
		w.Write(coverPNGForTest(t, 8, 8))
	}))
	defer srv.Close()

	c := New(Options{})
	t.Cleanup(c.Close)
	c.SetScale(2) // Retina

	// 48 DIP 的正方格子：Retina 上该要 96 像素。
	url := srv.URL + "/bfs/archive/cover.jpg"
	_ = c.GetSized(url, 48, 48)
	waitState(t, c, sizedURL(url, 96, 96), 10*time.Second, loaded)

	mu.Lock()
	defer mu.Unlock()
	if len(asked) == 0 {
		t.Fatal("一次请求都没发出去")
	}
	want := "/bfs/archive/cover.jpg@96w_96h_1c.webp"
	if asked[0] != want {
		t.Errorf("48 DIP 的格子请求了 %q\n  想要 %q\n"+
			"（以前这里要的是原图——768 长边那一份，47 倍的像素）", asked[0], want)
	}
}

// coverPNGForTest 造一张 w×h 的纯色 PNG。
func coverPNGForTest(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 200, 40, 40, 255
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestDraggingTheWindowDoesNotMakeNewURLs 钉住「窗口宽一点点变，URL 不该跟着变」。
//
// # 这条是评审挑出来的（PR #622）
//
// 第一版只吸**长边**，短边按比例连续算。而首页磁贴那一格的宽随窗口走、高固定
// 96——窗口拖一像素，短边就变一像素，URL 就换一个。实测 `cw` 从 257 走到 320
// 会产生 **48 个不同的 URL**：拖一次窗口就下一批新图、在盘上攒一批几乎一样的
// 文件、在内存里各占一块。
//
// 现在的做法是短边**跟比例、但量化到 1/32**（见 `coverPixelSize`）。
func TestDraggingTheWindowDoesNotMakeNewURLs(t *testing.T) {
	const base = "https://i0.hdslb.com/bfs/archive/y.jpg"
	seen := map[string]bool{}
	for cw := 257; cw <= 320; cw++ {
		// 首页磁贴：宽 = cw*0.5、高固定 96（`homepage_overview.go`）。
		w, h := coverPixelSize(float32(cw)*0.5, 96, 2)
		seen[sizedURL(base, w, h)] = true
	}
	if len(seen) > 5 {
		t.Errorf("窗口宽从 257 拖到 320（64 个像素），产生了 %d 个不同的 URL——"+
			"拖一次窗口就下一批新图、盘上攒一批几乎一样的文件（短边没量化）",
			len(seen))
	}
}

// TestTheAspectRatioIsKept 钉住「宽高比不能掰弯」。
//
// 两条边各吸各的档会把比例掰弯：400×336 分别吸到 400/400，图被拉伸两成。
// 这条守的是「长边吸档、短边按比例跟」那个做法。
func TestTheAspectRatioIsKept(t *testing.T) {
	for _, tc := range []struct{ w, h float32 }{
		{200, 168}, {672, 378}, {384, 288}, {48, 48},
	} {
		pw, ph := coverPixelSize(tc.w, tc.h, 2)
		want := float64(tc.w) / float64(tc.h)
		got := float64(pw) / float64(ph)
		if diff := got/want - 1; diff > 0.06 || diff < -0.06 {
			t.Errorf("%g×%g 要成 %d×%d，宽高比 %g → %g（偏了 %.0f%%）——图会被拉伸",
				tc.w, tc.h, pw, ph, want, got, diff*100)
		}
	}
}

// TestTheProbeKeyMatchesTheDrawnKey 钉住「探测 Ready 用的 key 和画的时候是同一个」。
//
// # 为什么这条必须有
//
// 「先探测有没有图，有才画」是这一屏的常见写法（UP 空间缩略图、首页磁贴的
// 插画）。如果探测用 `Get(url)`、画的时候用 `GetSized(url, w, h)`，**两张图
// 下两遍**：探测那份是原图，画的那份才是按尺寸的——同一张封面多一次网络、
// 多一次解码，而这一票要省的像素一份没省。
//
// 这是 PR #622 的评审挑出来的真回归，`upspace.go` 和 `homepage_overview.go`
// 两处都中过。这条测试守的是「有人把探测改回 `Get`」。
func TestTheProbeKeyMatchesTheDrawnKey(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "image/png")
		w.Write(coverPNGForTest(t, 8, 8))
	}))
	defer srv.Close()

	c := New(Options{})
	t.Cleanup(c.Close)
	c.SetScale(2)

	url := srv.URL + "/bfs/archive/probe.jpg"
	w, h := float32(200), float32(168)

	// 探测（就是 `upspace.go` 那一句）。
	_ = c.GetSized(url, w, h)
	// 等的是 `GetSized` 真正拼出来的那一个 key（200×168 DIP @2x → 400 长边，
	// 短边按比例量化），不是「测试里手写的 200×168」。
	pw, ph := coverPixelSize(w, h, 2)
	waitState(t, c, sizedURL(url, pw, ph), 10*time.Second, loaded)
	// 画（`States` → `drawCover`）。同一个尺寸，**不该再发第二次**。
	_ = c.GetSized(url, w, h)

	mu.Lock()
	n := len(asked)
	mu.Unlock()
	if n != 1 {
		t.Errorf("探测 + 绘制一共发了 %d 次请求（%v）——"+
			"同一个尺寸该只下一次；多出来的那一次说明探测和绘制用了不同的 key", n, asked)
	}
}
