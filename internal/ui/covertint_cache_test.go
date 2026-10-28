package ui

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/covertint"
)

// 取到的色**按封面 URL 记下来**：同一个 URL 第二次要色**不再发网络请求**。
//
// # 为什么
//
// `coverTint` 从前只记「当前这一张封面的色」——切走再切回同一个歌单，
// `t.url` 变了（新的一页是新的一份 `collectionPage`），于是要重新下载、
// 重新解码、重新取色一遍。用户看到的是「光晕又闪了一次」。
//
// 现在按 URL 记：切回来直接就有。
func TestTheTintIsRememberedPerCover(t *testing.T) {
	var hits atomic.Int64
	tintClientFor(t, &hits)

	var tint coverTint
	const url = "https://i0.hdslb.com/bfs/archive/cached.jpg"

	// 第一趟：没有缓存，要走网络。
	hue, sat := tint.sync(url, func() {})
	waitTint(t, &tint)
	hue, sat = tint.sync(url, func() {})
	if sat <= 0 {
		t.Fatalf("第一趟没取到色：hue=%v sat=%v", hue, sat)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("取色发了 %d 次网络请求，want 1", got)
	}

	// 第二趟：**同一个 URL** 不该再发请求。
	// 换一个封面再换回来（模拟切走再切回）。
	tint.sync("https://i0.hdslb.com/bfs/archive/other.jpg", func() {})
	waitTint(t, &tint)
	tint.sync(url, func() {})
	if got := hits.Load(); got != 2 {
		t.Fatalf("切走再切回之后总共发了 %d 次网络请求，want 2（同一个 URL 不该重下）", got)
	}
}

// 缓存的色**要能跨过「换成别的封面」这一段**：也就是缓存是一张表，不是一个格子。
func TestTheTintCacheKeepsMoreThanOneCover(t *testing.T) {
	var hits atomic.Int64
	tintClientFor(t, &hits)

	var tint coverTint
	const a = "https://i0.hdslb.com/bfs/archive/a.jpg"
	const b = "https://i0.hdslb.com/bfs/archive/b.jpg"

	tint.sync(a, func() {})
	waitTint(t, &tint)
	tint.sync(b, func() {})
	waitTint(t, &tint)
	tint.sync(a, func() {})
	tint.sync(b, func() {})

	// a、b 各下一次，之后都命中缓存。
	if got := hits.Load(); got != 2 {
		t.Fatalf("两个封面一共发了 %d 次网络请求，want 2", got)
	}
}

// 缓存**不该无限长**：那是内存泄漏。
func TestTheTintCacheIsBounded(t *testing.T) {
	var hits atomic.Int64
	tintClientFor(t, &hits)

	var tint coverTint
	// 灌比上限更多的封面。
	for i := range 64 {
		tint.sync(urlFor(i), func() {})
		waitTint(t, &tint)
	}
	if n := tint.cacheSize(); n > tintCacheLimit {
		t.Fatalf("缓存里有 %d 条，超过上限 %d", n, tintCacheLimit)
	}
}

// 缓存是**按 URL 存的**：换个 URL 就是另一个条目，不会串色。
func TestTheTintCacheIsKeyedByTheURL(t *testing.T) {
	var hits atomic.Int64
	tintClientFor(t, &hits)

	var tint coverTint
	tint.sync("https://i0.hdslb.com/bfs/archive/red.jpg", func() {})
	waitTint(t, &tint)
	tint.sync("https://i0.hdslb.com/bfs/archive/green.jpg", func() {})
	waitTint(t, &tint)

	redHue, _, _ := tint.cached("https://i0.hdslb.com/bfs/archive/red.jpg")
	greenHue, _, _ := tint.cached("https://i0.hdslb.com/bfs/archive/green.jpg")
	if redHue == greenHue {
		t.Fatalf("两个不同封面的色相一样（%v）——缓存串了", redHue)
	}
}

// tintClientFor 造一个「数着请求次数」的取色客户端。
//
// ⚠️ **它装的是包级那个 `tintClient` 的位置**：生产里取色走
// `tintClient`（带凭据边界和代理），测试要换成假的——不然每一次测试都
// 会打真网络（AGENTS.md §6）。
func tintClientFor(t *testing.T, hits *atomic.Int64) *http.Client {
	t.Helper()
	orig := tintClient
	t.Cleanup(func() { tintClient = orig })
	tintClient = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		hits.Add(1)
		// ⚠️ **每个 URL 给一张不同的图**：否则「两个 URL 的色不一样」那条
		// 测的就不是缓存串没串，而是夹具本身（同一个图当然同一个色）。
		//
		// 颜色由 URL 里的最后一个字符定（`urlFor` 就是照这个造的）。
		// ⚠️ **要从「去掉裁剪参数」的地址取种子**：请求上的 Path 带着
		// `@96w_96h` 后缀，直接看末尾几个字符的话每个 URL 都一样。
		seed := uint8(40)
		if p := r.URL.Path; len(p) > 0 {
			for i := len(p) - 1; i >= 0; i-- {
				if p[i] == '.' {
					seed = uint8(p[i-1])
					break
				}
			}
		}
		img := image.NewRGBA(image.Rect(0, 0, 64, 36))
		for y := range 36 {
			for x := range 64 {
				img.Set(x, y, color.RGBA{seed, uint8(x * 4), uint8(y * 7), 255})
			}
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, nil); err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(&buf),
			Header:     make(http.Header),
		}, nil
	})}
	return tintClient
}

// roundTripperFunc 把函数当成 RoundTripper。
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// waitTint 等后台那一趟取色回来。
func waitTint(t *testing.T, tint *coverTint) {
	t.Helper()
	for range 2000 {
		if tint.idle() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("取色那一趟一直没回来")
}

// urlFor 造第 i 个不同的封面地址。
func urlFor(i int) string {
	return "https://i0.hdslb.com/bfs/archive/cover" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".jpg"
}

// 让 `covertint` 这个包在测试文件里被用到（取色的类型来自它）。
var _ = covertint.Hue{}
var _ = context.Background

// ��不到色的封面**也要记进缓存**。
//
// # 为什么
//
// 黑白封面、坏地址、加载失败——这些封面**永远取不到色**。不记的话，
// `sync` 每一帧都会为它再发一次网络请求（一次几百毫秒的往返），于是
// 「打开一个全是黑白封面的歌单」会把网络打满。
//
// 这条在实现里是 `fetch` 里那句「取不到色也记」——而它**很容易被当成
// 冗余的缓存写掉**（直觉是「没颜色，记它干嘛」），所以要有测试钉住。
func TestACoverWithoutColourIsStillRemembered(t *testing.T) {
	var hits atomic.Int64
	orig := tintClient
	t.Cleanup(func() { tintClient = orig })
	// 一张纯灰的图：取不出颜色（灰没有色相）。
	tintClient = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		hits.Add(1)
		img := image.NewRGBA(image.Rect(0, 0, 64, 36))
		for y := range 36 {
			for x := range 64 {
				img.Set(x, y, color.RGBA{128, 128, 128, 255})
			}
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, nil); err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(&buf),
			Header:     make(http.Header),
		}, nil
	})}

	var tint coverTint
	const url = "https://i0.hdslb.com/bfs/archive/grey.jpg"
	tint.sync(url, func() {})
	waitTint(t, &tint)

	// ⚠️ **要等后台那一趟落地**：取色是异步的，`sync` 立刻就返回了——所以
	// 「再要几次」之后不能马上查请求数（那还没涨），得等它们各自回来。
	//
	// 查的是**缓存里有没有这一条**：那是「下一次 `sync` 会不会重发」的
	// 直接判据。
	for range 5 {
		tint.sync(url, func() {})
	}
	waitTint(t, &tint)

	if _, _, ok := tint.cached(url); !ok {
		t.Fatalf("取不到色的封面没记进缓存——下一帧又要重发一次请求，" +
			"网络会被打满")
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("取不到色的封面发了 %d 次网络请求，want 1——"+
			"每一帧都会重发，网络会被打满", got)
	}
}
