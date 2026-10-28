package imgcache

// 这一组钉住「**量不出显示尺寸**」那一档在缓存层的统一规则（票 #630）。
//
// # 这一档为什么要有规则
//
// 「按显示尺寸要图」的入口是 `GetSized(url, w, h)`。有些调用点**量不出**自己
// 要显示多大（通栏那一格：它在布局里是 `FillWidth()`，而宽度要等父容器算完
// 才知道）。那些调用点传 0。
//
// 以前的规则是「给最小的那一档（64×64）」——那是**从「量不出来」推出「小」**，
// 而这个推理是错的：
//
//   - 布局还没算完的那些调用点，量不出来是因为**真的还没算出来**，那一次用
//     原图是对的（只是慢一点）；
//   - 通栏那一格量不出来，是因为**它是通栏**（要的就是整行那么宽），不是
//     「小」——按 64×64 要图等于把一张通栏横幅变成一张正方形缩略图，
//     **横幅的两端直接丢掉**（实测请求的是 `top_photo.jpg@64w_64h_1c.webp`）。
//
// 新规则：**量不出显示尺寸时按原图走**（不裁、不缩）。「不知道要多大」时
// 原图是唯一安全的选择——它至少是全的。
//
// ⚠️ **这条规则落在缓存层，不是每一处各自决定**：将来有人再传 0，会被同一条
// 规则接住，而不是各自选一个。

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// askedPaths 是一台记下「请求了什么」的假图床。
type askedPaths struct {
	mu    sync.Mutex
	paths []string
}

func (p *askedPaths) hit(path string) {
	p.mu.Lock()
	p.paths = append(p.paths, path)
	p.mu.Unlock()
}

// first 报第一趟请求的路径（还没有请求时是空串）。
func (p *askedPaths) first() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.paths) == 0 {
		return ""
	}
	return p.paths[0]
}

// count 报一共发了几趟请求。
func (p *askedPaths) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.paths)
}

// newAskedPathsServer 起一台假图床，记下它收到的路径。
func newAskedPathsServer(t *testing.T) (*askedPaths, string) {
	t.Helper()
	asked := &askedPaths{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.hit(r.URL.Path)
		w.Header().Set("Content-Type", "image/png")
		w.Write(coverPNGForTest(t, 8, 8))
	}))
	t.Cleanup(srv.Close)
	return asked, srv.URL
}

// waitAsked 等到假图床收到第一趟请求。
//
// ⚠️ **不能拿 `waitState` 等**：它调 `c.Get(url)`，而 `Get` **会自己起一趟
// 下载**——那趟下载的就是原图，于是 `asked[0]` 变成「谁先到谁算数」。
// 这不是理论上的竞态：第一遍跑就绿、第二遍跑就红（实测）。
//
// 这一条只等「请求发出去了」——判据是**请求了什么**，那一下就够。
func waitAsked(t *testing.T, asked *askedPaths, what string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if asked.count() > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等了 10 秒，%s：一次请求都没发出去", what)
}

// TestAnUnknownDisplaySizeAsksForTheOriginal 钉住「量不出显示尺寸 ⇒ 按原图走」。
//
// # 判据是「请求了什么」
//
// 起一台假图床，记下它收到的路径。传 0 尺寸去要图，那么服务端收到的必须是
// **原图**那个地址（不带 `@`）——而不是 `@64w_64h_1c.webp` 那一档。
//
// ⚠️ **为什么这条不能靠「`coverPixelSize(0,0)` 返回什么」来判**：那是实现
// 细节。这条量的是**真的发出去的那一趟请求**——「换算函数对但没接到请求上」
// 是这一族最常见的一种静默断裂（`TestASmallCellAsksForASmallImage` 的
// 注释记着同一件事）。
func TestAnUnknownDisplaySizeAsksForTheOriginal(t *testing.T) {
	asked, base := newAskedPathsServer(t)

	c := New(Options{})
	t.Cleanup(c.Close)
	c.SetScale(2) // Retina：倍率给不给都一样——量不出尺寸时不该有档位

	url := base + "/bfs/archive/banner.jpg"
	_ = c.GetSized(url, 0, 0)
	waitAsked(t, asked, "量不出显示尺寸时的那一趟")

	if got := asked.first(); got != "/bfs/archive/banner.jpg" {
		t.Errorf("量不出显示尺寸时请求了 %q\n  想要原图 %q\n"+
			"（以前这里要的是 @64w_64h_1c.webp 那一档——"+
			"一张通栏横幅被缩成正方形缩略图，两端直接丢掉）",
			got, "/bfs/archive/banner.jpg")
	}
}

// TestOnlyOneSideUnknownIsAlsoTheOriginal 钉住「**只有一边**量不出来」也是
// 原图。
//
// ⚠️ **为什么单列一条**：横幅那一格高度是固定的（132），宽度是通栏——于是
// 「宽量不出来、高知道」是最常发生的那一种。只在**两边都**为 0 时才退化成
// 原图的话，横幅这一格会掉进「宽 0、高 132」那条路——而那正是这一票要修的
// 那个洞（`coverPixelSize(0, 132, 2)` 以前给 64×64）。
func TestOnlyOneSideUnknownIsAlsoTheOriginal(t *testing.T) {
	asked, base := newAskedPathsServer(t)

	c := New(Options{})
	t.Cleanup(c.Close)
	c.SetScale(2)

	// 宽量不出来、高知道（横幅那一格的情形）。
	url := base + "/bfs/archive/banner.jpg"
	_ = c.GetSized(url, 0, 132)
	waitAsked(t, asked, "宽量不出来的那一趟")

	if got := asked.first(); got != "/bfs/archive/banner.jpg" {
		t.Errorf("宽量不出来时请求了 %q\n  想要原图 %q\n"+
			"（以前这里是 @64w_64h_1c.webp——横幅被缩成正方形缩略图）",
			got, "/bfs/archive/banner.jpg")
	}
}

// TestAKnownSizeStillAsksForThatSize 钉住「**量得出**的时候照旧按那一档要」。
//
// ⚠️ **这一条挡的是「把规则写得太宽」**：如果有人把「量不出尺寸 ⇒ 原图」
// 写成「一律原图」，那一条会红。
func TestAKnownSizeStillAsksForThatSize(t *testing.T) {
	asked, base := newAskedPathsServer(t)

	c := New(Options{})
	t.Cleanup(c.Close)
	c.SetScale(2)

	url := base + "/bfs/archive/cover.jpg"
	_ = c.GetSized(url, 48, 48)
	waitAsked(t, asked, "48 DIP 的格子那一趟")

	want := "/bfs/archive/cover.jpg@96w_96h_1c.webp"
	if got := asked.first(); got != want {
		t.Errorf("48 DIP 的格子请求了 %q\n  想要 %q", got, want)
	}
}
