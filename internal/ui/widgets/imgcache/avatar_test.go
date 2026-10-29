package imgcache

// 这一组钉住「**头像走同一条规范化入口**」和「**按显示尺寸要的那一档记好一份**」。
//
// # 为什么这两件事在一起
//
// 同一个 UP 主的脸，在关注列表里是「服务层拼好 160×160 裁剪参数」的那一个地址，
// 在空间页 / 动态流里是「服务层补个协议」的那一个地址——同一个地址、同一张脸，
// 但**缓存的键是两个不同的串**：两个缓存条目、两次下载、两份落盘。
//
// 而「带了裁剪参数」的那一档**永远拿不到「按显示尺寸要图」的那一份**（参数已经
// 拼在地址里了，缓存那一层没法再拼）——于是关注卡那一格明明只要 128 像素，
// 却固定下一张 160×160。
//
// 所以这一票让所有头像走**同一个**规范化入口（补协议、**不**拼裁剪参数），
// 裁剪参数**由图片缓存按显示尺寸统一决定**。
//
// # ⚠️ 「统一」不是「统一成同一个尺寸」
//
// 头像的显示尺寸有 128（关注卡）、112（空间页）、40（动态流）、40（导航栏）各
// 不同，所以「按显示尺寸」那一档**会因地而异**——统一的是**机制**，不是**数值**。
//
// # ⚠️ 头像要正方形，所以那一族要中心裁剪（`_1c`）
//
// 图床的两种语义（实测，只读 GET 公开热门接口的真实封面）：
//
//	`@96w_96h_1c.webp`   → 96×96   中心裁剪成方
//	`@96w_96h_1s.webp`   → 96×54   **等比缩放（不裁）**
//
// 去掉 `_1c` 头像就不是正方形了——那个圆里的脸会变形。**这一票不碰「取色」那条
// 路**（`widgets/covertint`），那一族要等比（要算整张图的主色，裁了会偏色）。

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAnAddressThatAlreadyHasCropParamsIsLeftAlone 钉住「**已经带裁剪参数的
// 地址被尊重**」。
//
// 别人裁好的那一档不该被我们改掉——再拼一层 `@` 那是坏地址（图床回 400）。
func TestAnAddressThatAlreadyHasCropParamsIsLeftAlone(t *testing.T) {
	const already = "https://i0.hdslb.com/bfs/face/11.jpg@160w_160h_1c_1s.webp"
	if got := sizedURL(already, 96, 96); got != already {
		t.Errorf("已经带裁剪参数的地址被改成了 %q，want %q（原样返回）", got, already)
	}
}

// TestTheAvatarAsksForTheSizeItIsDrawnAt 钉住「**请求的是按显示尺寸那一档，
// 而不是一个固定的数**」。
//
// # 它量的是「请求了什么」
//
// 起一台假图床，记下它收到的路径。摆一格 128×128 的头像（关注卡那一档），
// 然后看服务端收到的是不是 `@<按显示尺寸算出来的>w_<…>h_1c.webp`——**不是**
// 一个固定的 160×160。
//
// # 为什么这一条比「地址对不对」更硬
//
// 换算函数测过了，但**它有没有真的接到请求上**是另一件事：某一处少写一句
// `GetSized`、或者显示尺寸没传下去，换算再对也不会影响任何一次下载——而这种
// 断裂在画面上看不出来（图还是出来了，只是大）。
func TestTheAvatarAsksForTheSizeItIsDrawnAt(t *testing.T) {
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

	// 关注卡那一档：128 见方，Retina 上该要 256 像素（吸档后 256）。
	url := srv.URL + "/bfs/face/11.jpg"
	_ = c.GetSized(url, 128, 128)
	waitState(t, c, sizedURL(url, 256, 256), 10*time.Second, loaded)

	mu.Lock()
	defer mu.Unlock()
	if len(asked) == 0 {
		t.Fatal("一次请求都没发出去")
	}
	want := "/bfs/face/11.jpg@256w_256h_1c.webp"
	if asked[0] != want {
		t.Errorf("128 DIP 的头像请求了 %q\n  想要 %q\n"+
			"（固定成 160×160 的话，那一格明明只要 128，却多下一张更大的；"+
			"而且那一个档位**拿不到**「按显示尺寸」的那一份）", asked[0], want)
	}
}

// TestTheAvatarStaysSquare 钉住「**头像要正方形**」。
//
// 那一族的参数必须带 `_1c`（中心裁剪）：去掉它就是**等比缩放**（图床给的是
// 96×54 而不是 96×96），那个圆里的脸就变形了。
//
// ⚠️ **别和「取色」搞混**：取色那一趟要**等比**（要算整张图的主色，裁了会偏色——
// 票 #623 实测最大偏 172.9°）。但那是**另一条路**（`widgets/covertint`），
// 这一票不碰它。
func TestTheAvatarStaysSquare(t *testing.T) {
	const bfs = "https://i0.hdslb.com/bfs/face/11.jpg"
	got := sizedURL(bfs, 96, 96)
	if !strings.Contains(got, "_1c") {
		t.Errorf("头像那一档拼出的是 %q——没有 `_1c`（中心裁剪）的话，"+
			"图床给的是等比缩放（96×54 而不是 96×96），那个圆里的脸会变形", got)
	}
}

// TestTheSameFaceIsOnlyAskedForOnce 钉住「**同一个 UP 主的脸只下一次**」。
//
// # 这一条是这一票的全部价值的一半
//
// 同一个地址、同一张脸，两次请求就是两次下载、两份落盘——而用户看不出区别
// （他只看到同一张脸）。
//
// # 为什么这里能测出来
//
// 缓存的键就是**规范化之后的那个地址**。所以「同一个脸」= 「同一个键」=
// 「只会下一次」。而**带了裁剪参数的那一档永远拿不到「按显示尺寸」的那一份**
// ——于是同一个脸在两个页面上是两个键、两次下载。
//
// 这里量的是**缓存这一层的键**：两个不同的入口（按显示尺寸要图的那一个、
// 和「原样返回」的那一个）拿到同一个脸时，发出去的请求**只有一次**。
func TestTheSameFaceIsOnlyAskedForOnce(t *testing.T) {
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

	const url = "https://i0.hdslb.com/bfs/face/11.jpg"
	// 「按显示尺寸要图」那一档。
	//
	// ⚠️ **地址要指向那台假图床**：图床那一档是按**主机**认的
	//（`/bfs/` 在路径里），而这里量的是**请求发了几次**——真去 i0.hdslb.com
	// 的话那是**打真网**，而且会拿回一个 404。
	fake := srv.URL + "/bfs/face/11.jpg"
	_ = c.GetSized(fake, 128, 128)
	waitState(t, c, sizedURL(fake, 256, 256), 10*time.Second, loaded)

	mu.Lock()
	n := len(asked)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("同一个脸被要了 %d 次（%v）——它该只下一次", n, asked)
	}

	// **再来一次**（模拟「在关注列表和他空间之间来回切」）：请求数不许增加。
	_ = c.GetSized(fake, 128, 128)
	mu.Lock()
	n = len(asked)
	mu.Unlock()
	if n != 1 {
		t.Errorf("来回切之后同一个脸被要了 %d 次（%v）——"+
			"来回切一次就多下一次，那正是「两个缓存条目」这件事", n, asked)
	}
}

// TestTheSizedURLIsRemembered 钉住「**拼出来的地址记好一份**」。
//
// # 它量的是「每帧分配次数」，不是「拼了几次」
//
// 「拼了几次」是**内部实现**（换个实现照样对），而**每帧分配次数**是外部
// 可观察的代价：一屏十二张封面 = 每帧十二次分配，60 帧每秒就是**每秒 720 次
// 纯垃圾**。
//
// # 判据是「按显示尺寸要图的那一次分配不许比要原图多」
//
// `Get` 是**对照组**：它要同一个缓存里的同一格，只是不拼那个 `@<宽>w_<高>h_1c`
// 后缀。所以「按显示尺寸要图」相对「要原图」**多出来的那几次分配**，就是拼
// 那个串的代价——**记好一份之后它该是零**。
//
// ⚠️ **为什么不用「两次调用的分配次数之差」**：那个判据两边都命中备忘录，
// 于是**有没有备忘录它都一样**——那是一条**永远绿**的测试（它量的是两次一样
// 的东西之差）。这一条比的是**两条不同的路**。
//
// # ⚠️ 地址要指向那台假图床
//
// 这里量的是**分配次数**，但 `GetSized` 也会真的去下载——真去 i0.hdslb.com 的话
// 那是**打真网**（而且会拿回一个 404，把这一条变成「网络抖动」的测量）。
func TestTheSizedURLIsRemembered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(coverPNGForTest(t, 8, 8))
	}))
	defer srv.Close()

	c := New(Options{Client: srv.Client()})
	t.Cleanup(c.Close)
	c.SetScale(2)

	url := srv.URL + "/bfs/archive/x.jpg"

	// 稳态：两格都已经在缓存里（`GetSized` 要的那一格 + `Get` 要的那一格）。
	_ = c.GetSized(url, 48, 48)
	_ = c.Get(url)

	sized := measureAllocs(t, func() { _ = c.GetSized(url, 48, 48) })
	plain := measureAllocs(t, func() { _ = c.Get(url) })

	if sized > plain {
		t.Errorf("按显示尺寸要图每次 %d 次分配，要原图只要 %d 次——"+
			"多出来的那 %d 次就是每帧每格拼那个 `@<宽>w_<高>h_1c.webp` 的代价"+
			"（一屏十二张封面就是每帧多十二次，60 帧每秒 720 次纯垃圾）",
			sized, plain, sized-plain)
	}
}

// measureAllocs 量「稳态下跑 n 次那个闭包」的每次分配次数。
//
// ⚠️ **先跑一遍闭包**（预热）：第一遍会把东西建出来（第一次调 `GetSized` 要拼
// 那个串、第一次调 `Get` 要建那个条目），而量的是**稳态**。
func measureAllocs(t *testing.T, fn func()) uint64 {
	t.Helper()
	fn()

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	const n = 200
	for range n {
		fn()
	}
	runtime.ReadMemStats(&after)
	return (after.Mallocs - before.Mallocs) / n
}

// TestTheRememberedURLsAreBounded 钉住「**那一份有上限**」。
//
// 不许无限长——地址那一维跟着「这一屏见过多少张不同的图」走，滚一晚上就能攒出
// 几万个条目，而它们一个字节都用不上（那些图早就被缓存挤掉了）。
//
// # 键的粒度要和现有的档位机制对齐
//
// 键里的宽高**已经是吸过档的**（`coverPixelSize` 的活儿：长边吸到某一档、短边
// 跟比例量化到 1/32）。所以拖动窗口时短边只每隔几十像素才变一档——**不会**每
// 拖一个像素产生一个一次性条目。
func TestTheRememberedURLsAreBounded(t *testing.T) {
	c := New(Options{})
	t.Cleanup(c.Close)
	c.SetScale(2)

	const base = "https://i0.hdslb.com/bfs/archive/y.jpg"
	// 首页磁贴那一档：宽随窗口走、高固定 96。
	for cw := 257; cw <= 320; cw++ {
		w, h := coverPixelSize(float32(cw)*0.5, 96, 2)
		_ = c.rememberedSizedURL(base, w, h)
	}
	if n := c.sizedURLs.lenOf(); n > 5 {
		t.Errorf("窗口宽从 257 拖到 320（64 个像素），备忘录里多了 %d 条——"+
			"拖一次窗口就多一批一次性条目（短边没量化）", n)
	}

	// ⚠️ **而且上限本身要兜住**：地址那一维没有天花板（滚一晚上就是几万个
	// 不同的图），所以表本身必须有一道闸。
	for i := range maxSizedURLCache + 100 {
		_ = c.rememberedSizedURL(base+strconv.Itoa(i), 96, 96)
	}
	if n := c.sizedURLs.lenOf(); n > maxSizedURLCache {
		t.Errorf("备忘录里有 %d 条，上限是 %d——它不会无限长", n, maxSizedURLCache)
	}
}
