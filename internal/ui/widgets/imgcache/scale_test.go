package imgcache

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// 这一组是「封面在有图 ↔ 占位之间反复切换」那个 bug 的回归（issue #208）。
//
// # 根因（两半，缺一不可）
//
// 1. **存下来的那一张图太大**：B 站的封面是 1920×1080 的原图，解码之后
//    RGBA 加 mipmap 链是 11MB，而界面上最大的那一格是满屏播放器那张
//    （384 DIP）。一屏十几张就是 170MB，远超缓存的字节上限——于是 LRU 只能
//    把**正在显示着的**封面挤掉，挤掉的那一张下一帧就退回占位，重新下一次。
//    `scale.go` 把存下来的那一份缩到最长边 768 像素（一张 1.8MB）。
//
// 2. **LRU 的字节账会漏**：一条从「有人要它」到「解完码」之间被挤掉的条目，
//    解码回来之后还会被记一笔账，而那一笔**再也挤不掉**（表里已经没有它了）。
//    于是 `used` 只增不减，挤的循环最后把整张表清空还挤不下——之后**每来
//    一张图就把表里剩下的全挤掉**（实测 16 张封面下了 7000 多次）。
//
// # 判据
//
// **一屏封面（每一张都比缓存的字节上限小）连着画 N 帧之后，每一张都还是
// 就绪态，而且一张都没有重新下载过。**

// jpegBytes 编一张 w×h 的 JPEG，当封面用。
//
// ⚠️ **用 JPEG 而不是 PNG**：这一组要编的是 1280×720 的大图（比缓存里存
// 的那一份大得多），PNG 编一张那个尺寸要一秒多，而 JPEG 几十毫秒就完。
// 解码那一侧 JPEG 也更快——这一组要解十六张。
func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x80, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 60}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestTheSameCoverStaysReadyAcrossFrames 是 issue #208 的主判据。
//
// 一屏封面（这里十六张）在内存预算里装不下的时候，老的那一版会把**正在显示
// 着的**封面挤掉：挤掉的那一张下一帧就退回占位，重新下一次、再解一次码……
// 界面上就是封面反复闪。
//
// 现在钉住的是：**每一张都还是就绪态**（没有一张退回占位），而且**一张都
// 没有重新下载过**（重新下载就意味着它被挤掉过）。
func TestTheSameCoverStaysReadyAcrossFrames(t *testing.T) {
	var hits atomic.Int32
	body := jpegBytes(t, 1280, 720)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Write(body)
	}))
	defer srv.Close()

	// 缓存里存的那一份是缩过的（最长边 768，见 scale.go）：一张 1280×720
	// 的封面缩完是 768×432，RGBA 加 mipmap 链约 1.8MB。十六张是 28MB。
	//
	// ⚠️ **预算给 40MB**：够装这一屏（28MB）加上一点余量，而**装不下按原图
	// 存的那一份**（1280×720 一张 4.9MB，十六张 78MB）——所以这一条在
	// 「不缩」的那一版上一定是红的（它会把封面挤掉，然后重新下载）。
	c := New(Options{MaxBytes: 40 << 20})
	t.Cleanup(c.Close)

	const covers = 16
	urls := make([]string, covers)
	for i := range urls {
		urls[i] = srv.URL + "/cover" + strconv.Itoa(i) + ".jpg"
	}

	// 第一帧：这一屏的每一张都要一次（界面上就是这一屏的封面）。
	for _, u := range urls {
		c.Get(u)
	}
	waitFor(t, 15*time.Second, "这一屏的封面都到齐了", func() bool {
		for _, u := range urls {
			if !c.Get(u).Ready() {
				return false
			}
		}
		return true
	})

	// 连着画三十帧。每一帧都问一遍这一屏的每一张：它们都还在吗？
	//
	// ⚠️ **判据是「状态」，不是「下载次数」**：状态退回 Loading 就是界面上的
	// 「占位」，而下载次数涨了就是「它被挤掉过」——后者是前者的原因。
	for frame := range 30 {
		for _, u := range urls {
			if s := c.Get(u); !s.Ready() {
				t.Fatalf("第 %d 帧：%s 退回占位了（Loading=%v Err=%v）——\n"+
					"封面在有图和占位之间反复切换，就是这一条钉的（issue #208）。\n"+
					"（内存占用 %d 字节，下载 %d 次）",
					frame, u, s.Loading, s.Err, c.UsedBytes(), hits.Load())
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	if n := hits.Load(); n != covers {
		t.Errorf("这一屏的封面被下载了 %d 次，应该是 %d 次——"+
			"多出来的那几次是被挤掉之后重新下的", n, covers)
	}
}

// TestTheByteAccountDoesNotLeak 钉住 LRU 的字节账不再漏。
//
// 漏了的症状不是「内存多占一点」：表里已经没有的那一条，它的字节数还挂在
// `used` 上，而挤的循环挤不掉它——于是挤的循环最后把**整张表**清空还挤不下，
// 之后每来一张图就把表里剩下的全挤掉（封面反复闪的那一半根因）。
//
// 所以判据是：**折腾半天之后，`used` 仍然在上限之内**。
func TestTheByteAccountDoesNotLeak(t *testing.T) {
	// ⚠️ **这一组解四十张图**：所以图挑小一点的（800×450，比缓存里存的那一份
	// 略大，缩放那一支照样走）。这一条要的是「折腾四十次」，不是「大图」。
	body := jpegBytes(t, 800, 450)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	const budget = 8 << 20
	c := New(Options{MaxBytes: budget})
	t.Cleanup(c.Close)

	// 来来回回换四十个地址：每一个都要一次，然后立刻换成下一个。
	//
	// ⚠️ **换得比预算快**：每一张都比预算装得下的多（缩完 1.8MB，预算 8MB
	// 装得下四张），所以每来一张就要挤掉一张——挤掉的那一条**晚一点才回来**
	// （解码在后台），而它回来的时候已经不在表里了。老的那一版就在这一刻
	// 把它的字节数记进 `used`，而那一笔再也挤不掉。
	for i := range 40 {
		u := srv.URL + "/c" + strconv.Itoa(i) + ".jpg"
		c.Get(u)
		if s := c.Get(u); !s.Loading && !s.Ready() {
			t.Fatalf("第 %d 张的状态是 %+v，既不是占位也不是就绪", i, s)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// 等后台那几条解码都回来（它们回来的时候才会计账）。
	time.Sleep(500 * time.Millisecond)

	if used := c.UsedBytes(); used > budget {
		t.Errorf("内存账是 %d 字节，上限 %d——超了 %d 字节。\n"+
			"这是「账漏了」：表里已经没有的那一条，它的字节数还挂在账上，"+
			"而挤的循环挤不掉它，于是挤的循环最后把整张表清空还挤不下"+
			"（封面反复闪的那一半根因，issue #208）",
			used, budget, used-budget)
	}
}

// TestACoverThatIsStillLoadingIsNotEvicted 钉住「还在下的那一张不参与淘汰」。
//
// 0 字节的条目（还在下，或者下失败了）**不占预算**：把它挤掉对预算一分钱
// 都不省，只是把正在下的一次扔了——那一次请求被取消，下一帧有人再要它，
// 它又从零开始，而「从零开始」在界面上就是「退回占位」。
//
// 判据：**那一次下载只发生一次**（没有被取消重来）。
func TestACoverThatIsStillLoadingIsNotEvicted(t *testing.T) {
	body := jpegBytes(t, 1280, 720)
	release := make(chan struct{})
	var slowHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow.jpg" {
			slowHits.Add(1)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Write(body)
	}))
	defer srv.Close()

	// 预算只够一张（缩完 1.8MB，预算给 2MB）。
	c := New(Options{MaxBytes: 2 << 20})
	t.Cleanup(c.Close)

	slow := srv.URL + "/slow.jpg"

	// ① 先要那一张「还在下的」：它排在最久没用的位置上（表里最靠后）。
	c.Get(slow)
	waitFor(t, 10*time.Second, "那一次请求发出去了", func() bool { return slowHits.Load() > 0 })

	// ② 再要两张，它们下完就把预算占满。
	for _, p := range []string{"/a.jpg", "/b.jpg"} {
		c.Get(srv.URL + p)
	}
	waitState(t, c, srv.URL+"/a.jpg", 10*time.Second, loaded)
	waitState(t, c, srv.URL+"/b.jpg", 10*time.Second, loaded)

	// ③ 预算满了之后的下一帧：界面上每一格都要问一遍自己的图。
	for range 5 {
		c.Get(slow)
		time.Sleep(20 * time.Millisecond)
	}

	// ④ 让那一次请求回来。
	close(release)

	// ⑤ 它下完了，而且**只下了一次**。
	//
	// ⚠️ **老的那一版在这里会下两次**：挤的循环从最久没用的那头开始，而
	// 「还在下」的那一条在表里是 0 字节、排在最后——于是它先被挤掉（请求被
	// 取消），下一帧再要它就是**第二次**（界面上：退回占位又重来一次）。
	waitState(t, c, slow, 10*time.Second, loaded)
	if n := slowHits.Load(); n != 1 {
		t.Errorf("那一张被请求了 %d 次（应该是 1 次）——"+
			"还在下的那一张被挤掉之后请求被取消了，下一帧又重新下一次", n)
	}
}

// TestStoredCoversAreScaledDown 钉住「存进缓存的那一份是缩过的」。
//
// 这一条是根因的第一半：界面上最大的那一格是满屏播放器那张（384 DIP =
// 768 物理像素），而 B 站的封面是 1920×1080 的原图。不缩的话一张就是 11MB，
// 一屏十几张就是 170MB，缓存根本装不下（issue #208）。
func TestStoredCoversAreScaledDown(t *testing.T) {
	body := jpegBytes(t, 1280, 720)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	c := New(Options{})
	t.Cleanup(c.Close)

	s := waitState(t, c, srv.URL+"/cover.jpg", 10*time.Second, loaded)
	w, h := s.Bitmap.Size()
	if w != maxStoredSide || h != maxStoredSide*720/1280 {
		t.Errorf("存下来的那张是 %dx%d，want %dx%d（长边缩到 %d）",
			w, h, maxStoredSide, maxStoredSide*720/1280, maxStoredSide)
	}
	// 缩过的那一张在账上占的字节也少了：账是按存下来的那张算的。
	if used, want := c.UsedBytes(), int64(maxStoredSide*maxStoredSide*720/1280*4*4/3); used > want {
		t.Errorf("内存账是 %d 字节，比缩过之后该占的 %d 多——"+
			"账是按存下来的那一张算的（见 bytesOf）", used, want)
	}
}
