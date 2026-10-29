package imgcache

// 票 #673：长期没人要的封面**释放像素**，需要时重新解回来。
//
// # 为什么不是 `ui.Bitmap.ReleasePixels`
//
// `mygo/ui/bitmap.go` 的 `ReleasePixels` **只对 `ui.DecodeBitmap` 造的 bitmap
// 有效**：它要有一个 `decode` 闭包把像素再造出来，而 `DecodeBitmap` 为了那个
// 闭包会把**整份压缩字节**留在 bitmap 上。我们这个包**故意不用** `DecodeBitmap`
// （见 `scale.go` 的注释：一张封面几百 KB，缓存里几百张就是几十 MB），走的是
// `ui.NewBitmap` —— 而 `NewBitmap` 造的 bitmap 没有 `decode`，`ReleasePixels`
// 在它上面**什么都不做**（`bitmap.go`："A bitmap NewBitmap made … has nothing
// else to make them from and keeps them"）。
//
// 所以这里的「释放」是：**把条目上那个 `*ui.Bitmap` 整个放掉**（没人再引用，
// GC 回收像素和 mipmap），条目本身留着记「下次要重新解码」。重新解的时候走
// 原来那条路：先问磁盘缓存（生产上 `Options.Dir` 一直开着），盘上没有才重新下
// ——解出来还是按尺寸档位缩过的那一份，像素和第一次一样。
//
// # 判据
//
//   - 连续 N 帧没人请求（`onScreen` 的同一套帧号）才释放；
//   - 画面上正画着的那几张（`visibleFrames` 内）**不许**释放；
//   - 释放之后 LRU 的字节账同步归零，重新解回来再记上；
//   - 回看时画面像素和第一次一致，而且**不再打图床**（盘上那份还在）。

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// releasePNG 编一张纯色 PNG，当「一眼能认出来是哪一张」的封面用。
func releasePNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// advanceFrames 让缓存走 n 帧（每帧一个新的时刻，`BeginFrame` 才会计数）。
func advanceFrames(c *Cache, now time.Time, n int) time.Time {
	for range n {
		now = now.Add(16 * time.Millisecond)
		c.BeginFrame(now)
	}
	return now
}

// entryOf 报某个地址的条目（没有就 nil）。
func entryOf(c *Cache, url string) *entry {
	v, ok := c.entries.Load(url)
	if !ok {
		return nil
	}
	return v.(*entry)
}

// releasedPixels 报某个地址的条目**现在**是不是「像素已经放掉、记录还在」。
//
// ⚠️ 用它代替「刚 Get 一定不是 Ready」那类断言：重新解码跑在后台 worker 上，
// 一次 `Get` 之内它可能已经解完了——那样的断言会在快机器上偶发。
func releasedPixels(t *testing.T, c *Cache, url string) bool {
	t.Helper()
	e := entryOf(c, url)
	if e == nil {
		t.Fatalf("%s 的条目已经不在 entries 里了", url)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.bm != nil {
		t.Fatalf("%s 还挂着位图——像素没放掉", url)
	}
	return e.released
}

// TestIdleCoversReleaseTheirPixels 是这一票的主线：连续 N 帧没人要 ⇒ 像素释放；
// 再要的时候重新解码、恢复 Ready，尺寸和画面都对。
func TestIdleCoversReleaseTheirPixels(t *testing.T) {
	body := releasePNG(t, 64, 64, color.RGBA{R: 200, G: 40, B: 40, A: 0xff})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	const idle = 8
	c := New(Options{ReleaseAfterFrames: idle})
	t.Cleanup(c.Close)
	url := srv.URL + "/a.png"

	now := time.Unix(1_700_000_000, 0)
	c.BeginFrame(now)
	st := waitState(t, c, url, 5*time.Second, loaded)
	if used := c.UsedBytes(); used != bytesOf(st.Bitmap) {
		t.Fatalf("刚解出来记账是 %d 字节，want %d", used, bytesOf(st.Bitmap))
	}

	// idle-1 帧：还不够，账上的字节一分不少。
	//
	// ⚠️ **这一段中间不许再 `Get`**：`Get` 会推进 `drawn`（「最后一次被要
	// 是在第几帧」），那就等于把闲置计时清零——测的就不是「连续没人要」了。
	now = advanceFrames(c, now, idle-1)
	if used := c.UsedBytes(); used == 0 {
		t.Fatalf("才 %d 帧没人要就把像素放了（阈值是 %d）", idle-1, idle)
	}

	// 第 idle 帧：释放。
	now = advanceFrames(c, now, 1)
	if used := c.UsedBytes(); used != 0 {
		t.Fatalf("连续 %d 帧没人要之后账上还占着 %d 字节——像素释放了账没清", idle, used)
	}
	if !releasedPixels(t, c, url) {
		t.Fatal("账清了，但条目没记成「已放掉」")
	}

	// 重新解回来：Ready、尺寸一样、计数一次。
	back := waitState(t, c, url, 5*time.Second, loaded)
	if w, h := back.Bitmap.Size(); w != 64 || h != 64 {
		t.Errorf("重新解回来是 %dx%d，want 64x64", w, h)
	}
	if used, want := c.UsedBytes(), bytesOf(back.Bitmap); used != want {
		t.Errorf("重新解回来记账是 %d 字节，want %d", used, want)
	}
	if got := c.redecodes.Load(); got != 1 {
		t.Errorf("重新解码计数是 %d，want 1", got)
	}

	// 释放/重解之后 LRU 的账必须自洽：used == 表里所有条目 bytes 之和。
	c.mu.Lock()
	var sum int64
	for _, el := range c.lru.byURL {
		sum += el.Value.(*entry).bytes
	}
	used := c.lru.used
	c.mu.Unlock()
	if used != sum {
		t.Errorf("LRU used=%d，表里各条 bytes 之和=%d——账和表对不上", used, sum)
	}

	// 再闲置一次：还会再释放一遍（策略不是一次性的）。
	advanceFrames(c, now, idle+1)
	if used := c.UsedBytes(); used != 0 {
		t.Errorf("第二次闲置之后账上还占着 %d 字节", used)
	}
}

// TestCoversOnScreenAreNeverReleased 钉住「显示中的不许释放」：即使阈值比
// `visibleFrames` 还小，`onScreen` 那道也要自己挡住。
func TestCoversOnScreenAreNeverReleased(t *testing.T) {
	body := releasePNG(t, 48, 48, color.RGBA{R: 20, G: 200, B: 40, A: 0xff})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	// 阈值 2 < visibleFrames(4)：只靠帧差会误伤「刚画过两帧」的那几张。
	c := New(Options{ReleaseAfterFrames: 2})
	t.Cleanup(c.Close)
	url := srv.URL + "/on-screen.png"

	now := time.Unix(1_700_000_000, 0)
	c.BeginFrame(now)
	waitState(t, c, url, 5*time.Second, loaded)

	// 每 3 帧要一次：帧差会 ≥ 阈值，但仍在 visibleFrames 之内。
	for i := range 30 {
		now = now.Add(16 * time.Millisecond)
		c.BeginFrame(now)
		if i%3 == 0 {
			if s := c.Get(url); !s.Ready() {
				t.Fatalf("第 %d 帧：画面上正画着的封面被释放了（Loading=%v Err=%v）",
					i, s.Loading, s.Err)
			}
		}
	}
	if used := c.UsedBytes(); used == 0 {
		t.Error("显示中的封面一次都没被要，账却归零了")
	}
	if got := c.redecodes.Load(); got != 0 {
		t.Errorf("显示中的封面被重新解码了 %d 次", got)
	}
}

// TestNothingIsReleasedWithoutBeginFrame 钉住兼容性：没调过 `BeginFrame`
// 的调用方（库自己的老测试、还没接上帧号的别处）行为不变。
func TestNothingIsReleasedWithoutBeginFrame(t *testing.T) {
	body := releasePNG(t, 32, 32, color.RGBA{R: 1, G: 2, B: 3, A: 0xff})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	c := New(Options{ReleaseAfterFrames: 2})
	t.Cleanup(c.Close)
	url := srv.URL + "/no-frames.png"
	waitState(t, c, url, 5*time.Second, loaded)

	for range 50 {
		if s := c.Get(url); !s.Ready() {
			t.Fatalf("没有帧号的时候封面被释放了：Loading=%v Err=%v", s.Loading, s.Err)
		}
	}
	if used := c.UsedBytes(); used == 0 {
		t.Error("没有帧号的时候账被清了")
	}
}

// TestStoppingFramesStopsReleasing 钉住「没有帧就不判」：窗口不再画帧的时候
// 缓存**不**释放。
//
// 这不是漏掉的一半，而是**故意的**（理由见 `release.go` 文件头）：没有帧就
// 没有「这一帧画着谁」的信息，那时候放掉像素可能放的正是用户眼前那张图——
// 一下一帧它就变成占位了。
func TestStoppingFramesStopsReleasing(t *testing.T) {
	body := releasePNG(t, 32, 32, color.RGBA{R: 5, G: 6, B: 7, A: 0xff})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	c := New(Options{ReleaseAfterFrames: 4})
	t.Cleanup(c.Close)
	url := srv.URL + "/stopped.png"

	now := time.Unix(1_700_000_000, 0)
	c.BeginFrame(now)
	waitState(t, c, url, 5*time.Second, loaded)

	// 帧停了：墙钟过去多久都不放。
	time.Sleep(50 * time.Millisecond)
	if used := c.UsedBytes(); used == 0 {
		t.Fatal("一帧都没走就把像素放了——判据变成了墙钟")
	}
	// 走够帧（`visibleFrames` + 阈值：画面上那一档也占着帧）就恢复判据。
	advanceFrames(c, now, visibleFrames+5)
	if used := c.UsedBytes(); used != 0 {
		t.Errorf("帧恢复之后账上还占着 %d 字节", used)
	}
}

// TestCoverStillLoadingIsNotReleased 钉住「还在下的不许放」：那一档没有像素
// 可放，也不该被当成「闲置」处理掉记录。
func TestCoverStillLoadingIsNotReleased(t *testing.T) {
	release := make(chan struct{})
	var arrived atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		arrived.Add(1)
		<-release
		w.Write(releasePNG(t, 32, 32, color.RGBA{R: 9, G: 9, B: 9, A: 0xff}))
	}))
	defer srv.Close()
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})

	c := New(Options{ReleaseAfterFrames: 4})
	t.Cleanup(c.Close)
	url := srv.URL + "/in-flight.png"

	now := time.Unix(1_700_000_000, 0)
	c.BeginFrame(now)
	if s := c.Get(url); !s.Loading {
		t.Fatalf("第一帧应该是在下，拿到 %+v", s)
	}
	waitFor(t, 5*time.Second, "请求发出去了", func() bool { return arrived.Load() > 0 })

	// 闲置很久，但那一张还在下。
	advanceFrames(c, now, 20)
	if s := c.Get(url); !s.Loading {
		t.Fatalf("还在下的那一张被改成了 %+v", s)
	}
	if _, ok := c.entries.Load(url); !ok {
		t.Fatal("还在下的那一张从 entries 里被删掉了——下一帧会再下一次")
	}

	close(release)
	waitState(t, c, url, 5*time.Second, loaded)
	if got := c.redecodes.Load(); got != 0 {
		t.Errorf("在途加载被当成重新解码计了 %d 次", got)
	}
}

// TestReleasedEntriesStayBounded 钉住「放掉的那些条目有上限」：它们没有字节数，
// LRU 的字节上限挤不掉，所以单独有一道条数上限。
func TestReleasedEntriesStayBounded(t *testing.T) {
	const (
		n      = 12
		maxRel = 4
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(releasePNG(t, 24, 24, color.RGBA{R: 60, G: 60, B: 60, A: 0xff}))
	}))
	defer srv.Close()

	c := New(Options{ReleaseAfterFrames: 2, MaxReleased: maxRel})
	t.Cleanup(c.Close)

	now := time.Unix(1_700_000_000, 0)
	c.BeginFrame(now)
	urls := make([]string, n)
	for i := range urls {
		urls[i] = srv.URL + "/bounded" + strconv.Itoa(i) + ".png"
	}
	for _, u := range urls {
		waitState(t, c, u, 5*time.Second, loaded)
	}
	advanceFrames(c, now, 32)

	c.mu.Lock()
	released := len(c.released)
	c.mu.Unlock()
	if released > maxRel {
		t.Errorf("放掉的条目有 %d 条，上限是 %d", released, maxRel)
	}
	if used := c.UsedBytes(); used != 0 {
		t.Errorf("放掉之后账上还占着 %d 字节", used)
	}
	// 被忘掉的那些再要一次就是干净的首次加载（能正常出图）。
	if s := waitState(t, c, urls[0], 5*time.Second, loaded); !s.Ready() {
		t.Error("被忘掉的条目再要一次没有恢复")
	}
}

// TestRevisitAfterReleaseComesFromDisk 钉住「压缩源或磁盘缓存仍可支持重新显示」：
// 释放之后回看，从盘上重新解，不重新打图床。
func TestRevisitAfterReleaseComesFromDisk(t *testing.T) {
	var hits atomic.Int32
	body := releasePNG(t, 72, 72, color.RGBA{R: 90, G: 90, B: 220, A: 0xff})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	c := New(Options{Dir: dir, ReleaseAfterFrames: 4})
	t.Cleanup(c.Close)
	url := srv.URL + "/disk.png"

	now := time.Unix(1_700_000_000, 0)
	c.BeginFrame(now)
	waitState(t, c, url, 5*time.Second, loaded)

	name := c.diskName(url)
	if name == "" {
		t.Fatal("磁盘缓存没开起来，这条测不了「盘上那份」")
	}
	waitFor(t, 5*time.Second, "这张图写到盘上", func() bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	})

	advanceFrames(c, now, 5)
	if used := c.UsedBytes(); used != 0 {
		t.Fatalf("闲置之后账上还占着 %d 字节", used)
	}

	before := hits.Load()
	waitState(t, c, url, 5*time.Second, loaded)
	if got := hits.Load(); got != before {
		t.Errorf("回看时又打了 %d 次图床，应该直接读盘", got-before)
	}
	if got := c.redecodes.Load(); got != 1 {
		t.Errorf("重新解码计数是 %d，want 1", got)
	}
}

// TestReleasedCoverShowsThePlaceholderUntilItDecodes 钉住「释放之后那一格先画
// 什么」：**正在加载的占位**，不是失败态。
//
// 判据做成确定的：第二次请求在服务端**卡住**，于是「重新解码」这一档一直是活的
// ——`Get` 每次都必须报 Loading（而不是 Err）。放它回来之后再落到 Ready。
func TestReleasedCoverShowsThePlaceholderUntilItDecodes(t *testing.T) {
	body := releasePNG(t, 24, 24, color.RGBA{R: 0, G: 128, B: 128, A: 0xff})
	gate := make(chan struct{})
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) > 1 {
			<-gate // 第二次（重新解那次）卡住
		}
		w.Write(body)
	}))
	defer srv.Close()
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	})

	c := New(Options{ReleaseAfterFrames: 4})
	t.Cleanup(c.Close)
	url := srv.URL + "/slow-again.png"

	now := time.Unix(1_700_000_000, 0)
	c.BeginFrame(now)
	waitState(t, c, url, 5*time.Second, loaded)
	advanceFrames(c, now, 5)
	if used := c.UsedBytes(); used != 0 {
		t.Fatalf("闲置之后账上还占着 %d 字节", used)
	}

	// 第二次请求卡在服务端：这一刻是「正在重新解」。
	deadline := time.Now().Add(10 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		if s := c.Get(url); s.Err != nil || s.Ready() {
			t.Fatalf("重新解码期间报的不是占位：Ready=%v Err=%v", s.Ready(), s.Err)
		}
		time.Sleep(time.Millisecond)
	}
	if calls.Load() < 2 {
		t.Fatal("第二次请求一直没发出来，这条测不到「重新解码期间」")
	}
	for range 5 {
		if s := c.Get(url); !s.Loading || s.Ready() || s.Err != nil {
			t.Fatalf("重新解码期间那一格不是干净的占位：Ready=%v Loading=%v Err=%v",
				s.Ready(), s.Loading, s.Err)
		}
	}
	// 并发再要几次：不许再起第三条请求（同一时刻只有一条在飞）。
	//
	// ⚠️ **不用「睡一会儿再看请求数」当判据**：那既可能漏（goroutine 还没跑到
	// 服务端）也可能白等。这里等两件确定的事——这几次 `Get` 全部返回
	// （「要不要起下载」是同步决定的），以及 `lifeWG` 排空（它数着所有下载
	// goroutine，包括万一多起的那一条；多起的那一条会卡在同一个 gate 上，
	// 放开之后才退出）。两者都过去了，请求数才是最终值。
	var gets sync.WaitGroup
	for range 5 {
		gets.Add(1)
		go func() {
			defer gets.Done()
			c.Get(url)
		}()
	}
	gets.Wait()

	close(gate)
	waitState(t, c, url, 5*time.Second, loaded)
	c.lifeWG.Wait()
	if got := calls.Load(); got != 2 {
		t.Errorf("同一张被要了 %d 次（want 2：第一次 + 一次重新解码）", got)
	}
	if got := c.redecodes.Load(); got != 1 {
		t.Errorf("重新解码计数是 %d，want 1", got)
	}
}

// TestReleasedCoverRefetchesTheSameSizedURL 钉住尺寸档位与 DPR：释放之后重新
// 取的还是同一个 key（同一个按档位拼出来的地址），不会退化成原图那一格。
func TestReleasedCoverRefetchesTheSameSizedURL(t *testing.T) {
	paths := &pathLog{}
	body := releasePNG(t, 300, 300, color.RGBA{R: 10, G: 120, B: 200, A: 0xff})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths.add(r.URL.Path)
		w.Write(body)
	}))
	defer srv.Close()

	c := New(Options{ReleaseAfterFrames: 4})
	t.Cleanup(c.Close)
	c.SetScale(2) // Retina：96 DIP 的格子要 200 像素那一档
	src := srv.URL + "/bfs/cover.jpg"

	now := time.Unix(1_700_000_000, 0)
	c.BeginFrame(now)
	st := waitStateSized(t, c, src, 96, 96, 5*time.Second)
	if w, h := st.Bitmap.Size(); w != 300 || h != 300 {
		t.Fatalf("解出来是 %dx%d", w, h)
	}
	first := paths.last()
	if !strings.Contains(first, "@200w_200h_1c.webp") {
		t.Fatalf("第一次要的是 %q，应该带 200 那一档", first)
	}

	advanceFrames(c, now, 5)
	waitStateSized(t, c, src, 96, 96, 5*time.Second)
	if got := paths.last(); got != first {
		t.Errorf("释放之后重新要的是 %q，第一次是 %q——尺寸档位变了", got, first)
	}
	if got := c.redecodes.Load(); got != 1 {
		t.Errorf("重新解码计数是 %d，want 1", got)
	}
}

// pathLog 记下服务端收到的请求路径（只给上面那条用）。
type pathLog struct {
	mu sync.Mutex
	ps []string
}

func (p *pathLog) add(s string) {
	p.mu.Lock()
	p.ps = append(p.ps, s)
	p.mu.Unlock()
}

func (p *pathLog) last() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.ps) == 0 {
		return ""
	}
	return p.ps[len(p.ps)-1]
}

// waitStateSized 轮询到某个尺寸档的图落到 Ready。
func waitStateSized(t *testing.T, c *Cache, url string, w, h float32, d time.Duration) State {
	t.Helper()
	deadline := time.Now().Add(d)
	var last State
	for time.Now().Before(deadline) {
		last = c.GetSized(url, w, h)
		if last.Ready() {
			return last
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("等了 %v，%s 还是 Loading=%v Err=%v", d, url, last.Loading, last.Err)
	return last
}

// TestReleaseKeepsTheFailurePath 钉住「释放之后重新解失败，照常落到失败态」。
func TestReleaseKeepsTheFailurePath(t *testing.T) {
	var serveOK atomic.Bool
	serveOK.Store(true)
	requestCount.Store(0)
	body := releasePNG(t, 40, 40, color.RGBA{R: 250, G: 200, B: 0, A: 0xff})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		if !serveOK.Load() {
			http.Error(w, "没了", http.StatusNotFound)
			return
		}
		w.Write(body)
	}))
	defer srv.Close()

	c := New(Options{ReleaseAfterFrames: 4})
	t.Cleanup(c.Close)
	url := srv.URL + "/gone.png"

	now := time.Unix(1_700_000_000, 0)
	c.BeginFrame(now)
	waitState(t, c, url, 5*time.Second, loaded)
	advanceFrames(c, now, 5)

	serveOK.Store(false)
	// 永久失败（404）：不重试，直接落失败态。
	if s := waitState(t, c, url, 5*time.Second, failed); s.Err == nil {
		t.Fatal("重新解失败之后没有报错")
	}
	if used := c.UsedBytes(); used != 0 {
		t.Errorf("失败的那张还记着 %d 字节", used)
	}
	// ⚠️ **失败之后不许每次 Get 都再打一遍图床**：`done` 立着，`reloadReleased`
	// 就不再起加载——一个 404 不该变成每次 Get 一次请求（重试由退避那条路管）。
	before := requestCount.Load()
	for range 5 {
		if s := c.Get(url); s.Err == nil {
			t.Fatalf("失败态被盖掉了：%+v", s)
		}
	}
	if got := requestCount.Load(); got != before {
		t.Errorf("失败之后又打了 %d 次图床——`done` 没挡住重新加载", got-before)
	}
}

// TestReleaseSurvivesATransientFailure 钉住「重新解码暂时失败、自动重试成功」
// 这条路上重新解码仍然计数（`released` 不能被一次失败抹掉）。
//
// 判据是**成功之后**计数涨了一次，而不是「失败期间计了零次」——后者会把
// 一次真的重新解码漏掉（文件头说这个计数只给测试看，但它要准）。
func TestReleaseSurvivesATransientFailure(t *testing.T) {
	// 第 2 次请求（也就是**放掉之后重新解那一次**）回 503，其余成功。
	var requests atomic.Int32
	body := releasePNG(t, 40, 40, color.RGBA{R: 11, G: 22, B: 33, A: 0xff})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 2 {
			http.Error(w, "抖了一下", http.StatusServiceUnavailable) // 5xx：暂时失败，会重试
			return
		}
		w.Write(body)
	}))
	defer srv.Close()

	fc := newFakeClock()
	c := newRetryCache(t, Options{ReleaseAfterFrames: 4}, fc)
	url := srv.URL + "/flaky.png"

	now := fc.nowTime()
	c.BeginFrame(now)
	waitState(t, c, url, 5*time.Second, loaded)
	advanceFrames(c, now, 5)
	if used := c.UsedBytes(); used != 0 {
		t.Fatalf("闲置之后账上还占着 %d 字节", used)
	}

	// 重新解那一次撞上 503：失败态 + 排了重试；`released` 要留着。
	if s := waitState(t, c, url, 5*time.Second, failed); s.Err == nil {
		t.Fatal("重新解失败之后没有报错")
	}
	// ⚠️ **等重试真的排上再推钟**：失败态先落到 `state()`，定时器是在它之后
	// 才排的——推早了那一次 Advance 什么都不触发（`retry_test.go` 的 `armed`）。
	waitFor(t, 5*time.Second, "重试排上了", func() bool { return armed(c, url) })
	fc.Advance(5 * time.Second)
	waitState(t, c, url, 5*time.Second, loaded)
	if got := c.redecodes.Load(); got != 1 {
		t.Errorf("重新解码计数是 %d，want 1——`released` 被一次暂时失败抹掉了", got)
	}
}

// requestCount 是上面那条用的请求计数器（每个测试自己清零）。
var requestCount atomic.Int32

// TestReleaseWithTwoUsersKeepsRetainClose 钉住双窗口语义：释放不影响
// Retain / Close 的引用计数，最后一个使用者放开才真的关。
func TestReleaseWithTwoUsersKeepsRetainClose(t *testing.T) {
	body := releasePNG(t, 36, 36, color.RGBA{R: 30, G: 30, B: 30, A: 0xff})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	c := New(Options{ReleaseAfterFrames: 4})
	c.Retain() // 迷你窗口借的那一份
	url := srv.URL + "/shared.png"

	now := time.Unix(1_700_000_000, 0)
	c.BeginFrame(now)
	waitState(t, c, url, 5*time.Second, loaded)
	advanceFrames(c, now, 5)

	c.Close() // 主窗口收尾：还有一份使用者，不许关
	waitState(t, c, url, 5*time.Second, loaded)
	if got := c.redecodes.Load(); got != 1 {
		t.Errorf("剩一份使用者时重新解码计数是 %d，want 1", got)
	}

	// 再闲置一轮：第二份使用者还在用，释放照旧生效（票 #673 和引用计数是
	// 两件事——释放**像素**不释放缓存）。
	advanceFrames(c, now, 5)
	if used := c.UsedBytes(); used != 0 {
		t.Fatalf("第二次闲置之后账上还占着 %d 字节", used)
	}
	v, ok := c.entries.Load(url)
	if !ok {
		t.Fatal("释放像素不该把条目从 entries 里删掉")
	}
	e := v.(*entry)
	e.mu.Lock()
	stillHasPixels, released := e.bm != nil, e.released
	e.mu.Unlock()
	if stillHasPixels || !released {
		t.Fatalf("第二次闲置之后 bm=%v released=%v，want 已经放掉", stillHasPixels, released)
	}

	// 还没解回来就关掉：这一格必须落成 ErrClosed，不许永远停在「加载中」。
	c.Close() // 最后一份：真的关
	if s := c.Get(url); s.Err == nil || s.Loading {
		t.Errorf("关掉之后重新要释放过的封面应该报 ErrClosed，拿到 Loading=%v Err=%v", s.Loading, s.Err)
	}
	if s := c.Get(srv.URL + "/after-close.png"); s.Err == nil {
		t.Errorf("关掉之后新地址还去下载了：%+v", s)
	}
	// 关掉之后不再起后台解码：帧号还在走也不会把谁解回来。
	advanceFrames(c, now, 20)
	e.mu.Lock()
	bm := e.bm
	e.mu.Unlock()
	if bm != nil {
		t.Errorf("关掉之后后台还在解：%p", bm)
	}
}

// TestLongScrollReleasesOldCoversAndBringsThemBack 是 E2E 风格的一条：模拟滚过
// 200 张封面。
//
// 预算给得**远大于** 200 张的总和（64MiB），所以「最后账上回落」只可能来自
// **释放**，不是 LRU 挤掉的。判据：
//
//   - 峰值持有字节受控（远低于 200 张全留着）；
//   - **Go 堆**在滚完之后也回落（不只是账目数字，是真实的像素内存）；
//   - 回看第一张：从盘上重新解码、画面颜色和第一次一致、不再打图床；
//   - 重新解码次数可数（`redecodes`）。
//
// ⚠️ 这里量的是 **Go heap**，不是 RSS / GPU texture：后两者只能在真窗口里量
// （见 issue 的验收条目——那是发布前汇总时的事）；无头测试里能量、而且和
// 「像素放没放」直接对应的是 Go 堆里那几 MB 的 RGBA。
func TestLongScrollReleasesOldCoversAndBringsThemBack(t *testing.T) {
	const (
		covers  = 200
		side    = 200
		visible = 8
		idle    = 16
	)
	perCover := int64(side*side*4) * 4 / 3 // bytesOf 的口径：RGBA + 1/3 mipmap

	colors := make([]color.RGBA, covers)
	bodies := make([][]byte, covers)
	for i := range covers {
		colors[i] = color.RGBA{R: uint8(i * 7), G: uint8(255 - i), B: uint8(i * 3), A: 0xff}
		bodies[i] = releasePNG(t, side, side, colors[i])
	}
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		i, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".png"))
		if err != nil || i < 0 || i >= covers {
			http.NotFound(w, r)
			return
		}
		w.Write(bodies[i])
	}))
	defer srv.Close()

	urls := make([]string, covers)
	for i := range urls {
		urls[i] = srv.URL + "/" + strconv.Itoa(i) + ".png"
	}

	dir := t.TempDir()
	c := New(Options{
		Dir:                dir,
		MaxBytes:           64 << 20, // 200 张全留着也装得下：回落只能来自释放
		ReleaseAfterFrames: idle,
		MaxInflight:        8,
	})
	t.Cleanup(c.Close)

	now := time.Unix(1_700_000_000, 0)
	frame := func() {
		now = now.Add(16 * time.Millisecond)
		c.BeginFrame(now)
	}
	// 等这一屏都画出来（不算帧：Get 只是同一帧里的几遍 build）。
	waitVisible := func(first int) {
		deadline := time.Now().Add(20 * time.Second)
		for {
			ready := true
			for i := first; i < first+visible; i++ {
				if !c.Get(urls[i]).Ready() {
					ready = false
				}
			}
			if ready {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("等了 20s，第 %d..%d 张还没出来", first, first+visible-1)
			}
			time.Sleep(time.Millisecond)
		}
	}

	var peak int64
	observe := func() {
		if u := c.UsedBytes(); u > peak {
			peak = u
		}
	}

	heapInUse := func() int64 {
		var ms runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&ms)
		return int64(ms.HeapAlloc)
	}
	heapBefore := heapInUse()

	for first := 0; first+visible <= covers; first++ {
		frame() // 新的一帧：上一帧没再要的封面变老一帧
		waitVisible(first)
		observe()
	}

	// 200 张全留着 ≈ 42.7MB；阈值 16 + 一屏 8 ≈ 24 张 ≈ 5.1MB。
	if peak > 12<<20 {
		t.Errorf("峰值持有 %d 字节（%.1fMB），200 张全留着是 %.1fMB——释放没跟上滚动",
			peak, float64(peak)/(1<<20), float64(perCover*covers)/(1<<20))
	}
	if got := c.UsedBytes(); got > 8<<20 {
		t.Errorf("滚完之后还持有 %d 字节（%.1fMB），没有回落", got, float64(got)/(1<<20))
	}
	// Go 堆也要回落：账目数字清了、像素还挂着的话这里会露馅。
	//
	// 判据给得松（12MB）：测试进程里同时还有别的测试的残留、t.TempDir 的
	// 磁盘缓存账本、以及 GC 的碎片；这条要拦的是「200 张的像素（42.7MB）
	// 一张都没放」，不是精确到字节。
	heapAfter := heapInUse()
	t.Logf("滚过 %d 张之后：账目峰值 %.1fMB，回落至 %.1fMB；Go heap %+.1fMB",
		covers, float64(peak)/(1<<20), float64(c.UsedBytes())/(1<<20),
		float64(heapAfter-heapBefore)/(1<<20))
	if grew := heapAfter - heapBefore; grew > 12<<20 {
		t.Errorf("滚过 %d 张之后 Go heap 涨了 %.1fMB——像素没被回收（200 张全留着是 %.1fMB）",
			covers, float64(grew)/(1<<20), float64(perCover*covers)/(1<<20))
	}
	if got := hits.Load(); got != covers {
		t.Errorf("滚过 200 张一共下了 %d 次（want 200）", got)
	}
	if got := c.redecodes.Load(); got != 0 {
		t.Errorf("还没回看就重新解码了 %d 次", got)
	}

	// 回看第一张：先占位（重新解码），再恢复，颜色必须和第一次一致。
	name := c.diskName(urls[0])
	waitFor(t, 10*time.Second, "第一张写到盘上", func() bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	})
	before := hits.Load()
	redecodesBefore := c.redecodes.Load()
	frame()
	if !releasedPixels(t, c, urls[0]) {
		t.Fatal("刚释放的封面回看前还是「有像素」——它压根没被释放")
	}
	waitVisible(0)
	st := c.Get(urls[0])
	if !st.Ready() {
		t.Fatalf("回看之后没有恢复：Loading=%v Err=%v", st.Loading, st.Err)
	}
	if w, h := st.Bitmap.Size(); w != side || h != side {
		t.Errorf("回看解出来是 %dx%d，want %dx%d", w, h, side, side)
	}
	// 画一遍，量中间那个像素：重新解出来的画面颜色要对。
	tt := ui.NewTester(func(ctx *ui.Context) {
		ui.Image(ctx, st.Bitmap).Fit(ui.FillBox)
	}, side, side)
	if got, want := tt.Image().RGBAAt(side/2, side/2), colors[0]; got != want {
		t.Errorf("回看的画面中心是 %v，want %v——重新解出来的像素不对", got, want)
	}
	if got := hits.Load(); got != before {
		t.Errorf("回看又打了 %d 次图床（应该读盘）", got-before)
	}
	// 回看这一屏是 8 张（`waitVisible` 把它们一起要了），8 张都重新解码一次。
	if got := c.redecodes.Load() - redecodesBefore; got != visible {
		t.Errorf("回看这一屏重新解码了 %d 张，want %d", got, visible)
	}

	// 再闲置一段：账要回落到 0（回看解回来的那一份也放掉）。
	for range idle + 2 {
		frame()
	}
	if got := c.UsedBytes(); got != 0 {
		t.Errorf("再闲置 %d 帧之后账上还占着 %d 字节", idle+2, got)
	}
}
