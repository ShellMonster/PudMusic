package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// 这一组盯的是下载引擎的三条硬要求（`spec.md`「下载」一节）：
//
//	1. 10 MB 分块 + Range 并发 3 + 进度 500 ms 节流
//	2. 续传前校验资源身份；**服务器回 200 而不是 206 时必须中止**
//	3. 文件名净化到 255 字节
//
// ⚠️ 被 mock 掉的是「B 站什么时候让地址失效」，**不是**下载逻辑本身：
// 分块、Range、206 检查、并发上限、身份校验、续传、落盘全都在真的跑。
//
// ⚠️ 这台的 fake CDN 用 `byteAt` 现算正文，不在内存里存那一大段——
// 要测并发和续传就得用多块的资源（20 MB 起），存着的话光 fixture 就是
// 几十兆，而正文内容一个字节都不会少验。

// payloadByte 是第 i 个字节该是什么。variant 变的是「另一段内容」。
//
// **variant 是这一组测试里最重要的一行**：它让「资源身份对不上时能不能
// 拼出一个看起来正常的坏文件」变成一件可以断言的事——拼出来的文件
// 长度是对的，内容是 `variant0` 的后半段接 `variant1` 的前半段。
func payloadByte(variant, i int64) byte {
	return byte(i*7 + i/251 + variant*13)
}

const (
	bvid = "BV1xx411c7mD"
	cid  = "c1234567"
)

// cdn 是一台懂 Range 的假 CDN。开关都拧开了：可以不让它认 Range，可以
// 让它回一个错的 Content-Range，可以在发到一半时掐断。
type cdn struct {
	size    int64
	variant int64

	srv *httptest.Server

	mu          sync.Mutex
	inFlight    int
	maxInFlight int
	chunkReqs   []string // 每一发**分块请求**的 Range（探长度的不算）
	probes      int      // 探长度的请求数
	ignoreRange bool     // 回 200 而不是 206
	wrongRange  bool     // 回 206 但 Content-Range 指别处
	starTotal   bool     // 回 206 但 Content-Range 的总长写 `*`（不肯说长度）
	cutAt       int64    // >0：每次响应只给这么多字节就掐断
	broken      bool     // 一律 403（地址过期）
}

func newCDN(t *testing.T, size int64) *cdn {
	t.Helper()
	c := &cdn{size: size}
	c.srv = httptest.NewServer(c)
	t.Cleanup(c.srv.Close)
	return c
}

func (c *cdn) url() string { return c.srv.URL + "/audio.m4s?upsig=x&deadline=9999999999" }

func (c *cdn) track(delta int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inFlight += delta
	if c.inFlight > c.maxInFlight {
		c.maxInFlight = c.inFlight
	}
}

func (c *cdn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.track(1)
	defer c.track(-1)

	c.mu.Lock()
	broken, ignore, wrong, star, cut, variant := c.broken, c.ignoreRange, c.wrongRange, c.starTotal, c.cutAt, c.variant
	c.mu.Unlock()

	if broken {
		http.Error(w, "expired", http.StatusForbidden)
		return
	}

	rng := r.Header.Get("Range")
	if rng == "" {
		// 不带 Range：整份。这是「B 站 CDN 认不认我们不管」的退路。
		w.Header().Set("Content-Length", strconv.FormatInt(c.size, 10))
		w.WriteHeader(http.StatusOK)
		c.write(w, variant, 0, c.size, cut)
		return
	}

	start, end, ok := parseRange(rng, c.size)
	if !ok {
		http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	// 探长度的那一发（`bytes=0-0`）不算分块请求。
	if rng == "bytes=0-0" {
		c.mu.Lock()
		c.probes++
		c.mu.Unlock()
		if ignore {
			// ⚠️ 不认 Range 的服务器对**任何**带 Range 的请求都回 200 +
			// 整个文件，探长度的那一发也不例外。曾经在这里写死
			// `Content-Length: 1`（好像只有分块请求会被无视），于是
			// 「问出来有多长」变成了 1 字节、整个任务退化成一个分块，
			// 而分块请求又长得跟探长度的一模一样 —— 测试自己把
			// 206 检查绕过去了，测的还是绿的。
			w.Header().Set("Content-Length", strconv.FormatInt(c.size, 10))
			w.WriteHeader(http.StatusOK)
			c.write(w, variant, 0, c.size, cut)
			return
		}
		if star {
			// RFC 7233 允许总长写成 `*`（「实例长度未知」）。而
			// `Content-Length` 说的是**这一发实际会发的几个字节**（1），
			// 不是整首歌——引擎要是拿它当总长就会规划出一个分块、下出
			// 一个 1 字节的「歌」。
			w.Header().Set("Content-Range", "bytes 0-0/*")
		} else {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-0/%d", c.size))
		}
		w.Header().Set("Content-Length", "1")
		w.WriteHeader(http.StatusPartialContent)
		c.write(w, variant, 0, 1, cut)
		return
	}

	c.mu.Lock()
	c.chunkReqs = append(c.chunkReqs, rng)
	c.mu.Unlock()

	// ⚠️ 这里就是「服务器不认 Range」的那一支：回 200，正文是**整个文件**。
	// B 站上游对这一支完全没有检查（`download-core.ts:395` 是全仓库唯一
	// 一处设 Range 的地方，grep `206` 零命中），而 `got` 默认只对非 2xx
	// 抛错——于是它被当成成功，整首歌的字节被盖进文件中间，长度还对得上。
	if ignore {
		w.Header().Set("Content-Length", strconv.FormatInt(c.size, 10))
		w.WriteHeader(http.StatusOK)
		c.write(w, variant, 0, c.size, cut)
		return
	}
	if wrong {
		// 有些 CDN 会把请求重定向到另一个分片上：状态码确实是 206，
		// 而给的不是我们要的那一段。
		start++
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, c.size))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(http.StatusPartialContent)
	c.write(w, variant, start, end+1, cut)
}

// write 把 [from, to) 写出去；cut > 0 时只给 cut 个字节就掐断连接。
func (c *cdn) write(w http.ResponseWriter, variant, from, to, cut int64) {
	if cut > 0 && from+cut < to {
		to = from + cut
	}
	buf := make([]byte, to-from)
	for i := range buf {
		buf[i] = payloadByte(variant, from+int64(i))
	}
	if _, err := w.Write(buf); err != nil {
		return
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	if cut > 0 {
		// 掐断。正文比 Content-Length 短，客户端读到的是 unexpected EOF。
		panic(http.ErrAbortHandler)
	}
}

// parseRange 从 `bytes=10-99` 里读出闭区间。
func parseRange(v string, size int64) (start, end int64, ok bool) {
	rest, ok := strings.CutPrefix(v, "bytes=")
	if !ok {
		return 0, 0, false
	}
	a, b, ok := strings.Cut(rest, "-")
	if !ok {
		return 0, 0, false
	}
	start, err := strconv.ParseInt(a, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	if b == "" {
		end = size - 1
	} else if end, err = strconv.ParseInt(b, 10, 64); err != nil {
		return 0, 0, false
	}
	if start < 0 || end >= size || start > end {
		return 0, 0, false
	}
	return start, end, true
}

func (c *cdn) requests() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.chunkReqs...)
}

func (c *cdn) peak() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.maxInFlight
}

func (c *cdn) set(fn func(*cdn)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(c)
}

// ---- 测试的架子 ----

// harness 是一套「起一个队列 + 一台假 CDN + 一个下载目录」。
type harness struct {
	t         *testing.T
	cdn       *cdn
	m         *Manager
	dir       string
	queueFile string
}

// newHarness 起一套。size 是这一首有多少字节。
//
// quality/variant 给的是 CDN 那一头的内容，B 站给的「另一段流」就是靠
// variant 模拟的——资源身份那一组用例靠它把「拼出来的坏文件」造出来。
func newHarness(t *testing.T, size int64, quality int) *harness {
	t.Helper()
	cdn := newCDN(t, size)
	dir := t.TempDir()

	// queueFile 一上来就有值：`Save("")` 会写到进程的工作目录里去，
	// 而那不是测试该做的事。
	h := &harness{t: t, cdn: cdn, dir: dir, queueFile: filepath.Join(t.TempDir(), QueueFileName)}
	m, err := New(Options{
		Client: http.DefaultClient,
		Resolve: func(context.Context, Identity) (*Source, error) {
			return &Source{
				Bvid: bvid, Cid: cid, Quality: quality,
				Label: "320K", URLs: []string{cdn.url()},
			}, nil
		},
	}, "")
	if err != nil {
		t.Fatalf("建队列：%v", err)
	}
	h.m = m
	t.Cleanup(func() { closeManager(t, m, dir) })
	return h
}

// closeManager 关掉队列管理器，并且把下载目录**自己**删掉（dir 是空串就不删）。
//
// ## 为什么目录要自己删
//
// `t.TempDir()` 的清理（`RemoveAll`）是在**别的清理都跑完之后**才做的，而在那
// 之前 `Manager.Close` 已经把还在跑的 `run` **丢在后台**了——那是故意的：取流方
// 不理 ctx 的话，「关掉应用」就退不出去了（见 queue.go 的 `runOne` 里那段「不能
// 无限等 `run` 返回」）。那个被丢下的 goroutine **可能还在往 `.part` 里写**，于是
// `RemoveAll` 删到一半、目录里又多了东西——症状是
// 「TempDir RemoveAll cleanup: directory not empty」，而**它和被测的逻辑毫无
// 关系**（实测连跑 300 次红 3 次）。
//
// 自己删就躲开了那一下：删不掉（目录不空）就等一会儿再删，直到删干净；删掉之后
// `t.TempDir()` 的清理再跑一遍 `RemoveAll`——路径已经没了，它什么也不做，也不
// 会报错。
func closeManager(t *testing.T, m *Manager, dir string) {
	t.Helper()
	_ = m.Close("")
	if dir == "" {
		return
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := os.RemoveAll(dir)
		if err == nil || time.Now().After(deadline) {
			return
		}
		// 那个被丢下的 goroutine 还在写：等它收尾。
		time.Sleep(5 * time.Millisecond)
	}
}

// enqueue 入队一首并等它落到 want 这个状态。
func (h *harness) enqueue(want Status) Snapshot {
	h.t.Helper()
	snap, err := h.m.Enqueue(AddRequest{Bvid: bvid, Cid: cid, Title: "测试的歌", Dir: h.dir, Ext: ".m4a"})
	if err != nil {
		h.t.Fatalf("入队：%v", err)
	}
	return h.wait(snap.ID, want)
}

func (h *harness) wait(id string, want Status) Snapshot {
	h.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		s, ok := h.m.Get(id)
		if ok && s.Status == want {
			return s
		}
		time.Sleep(2 * time.Millisecond)
	}
	s, _ := h.m.Get(id)
	h.t.Fatalf("等不到状态 %q，现在 %q（%d/%d 字节，%s）", want, s.Status, s.Done, s.Total, s.Err)
	return s
}

// assertFile 断磁盘上的内容一字节不多一字节不少。
func assertFile(t *testing.T, path string, variant int64, size int64) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s：%v", path, err)
	}
	if int64(len(b)) != size {
		t.Fatalf("%s 是 %d 字节，期望 %d", filepath.Base(path), len(b), size)
	}
	for i := range b {
		if want := payloadByte(variant, int64(i)); b[i] != want {
			t.Fatalf("%s 的第 %d 个字节是 %d，期望 %d —— 内容被写错位置了",
				filepath.Base(path), i, b[i], want)
		}
	}
}

// ---- 一、下完之后一字节不少 ----

func TestADownloadLandsEveryByteExactly(t *testing.T) {
	// 30 MB = 正好三个分块。**必须超过一块**：一块的话并发、写入位置、
	// 续传三样都测不到（一个分块时只有一个请求，峰值和「并发 3」无关）。
	const size = 3 * chunkSize
	h := newHarness(t, size, 30280)

	snap := h.enqueue(StatusDone)
	if snap.Done != size || snap.Total != size || snap.Percent != 100 {
		t.Fatalf("下完了但进度不对：%d/%d = %d%%", snap.Done, snap.Total, snap.Percent)
	}
	assertFile(t, snap.Dest, 0, size)

	// 半成品要清掉：用户在自己的下载目录里看到的应该是完整的一首，
	// 不会有一首「打不开的歌」混在里面。
	if _, err := os.Stat(snap.Dest + ".part"); !os.IsNotExist(err) {
		t.Fatalf("下完了还留着 .part")
	}
	// 每一块都真的用 Range 要过——没有哪一块是靠一次整体下载蒙混过关的。
	if got := len(h.cdn.requests()); got != 3 {
		t.Fatalf("发了 %d 发分块请求，3 个分块应该是 3 发：%v", got, h.cdn.requests())
	}
	if _, _, ok := parseRange(h.cdn.requests()[0], size); !ok {
		t.Fatalf("第一发的 Range 看不懂：%q", h.cdn.requests()[0])
	}
}

// 🔴🔴 这一票最要紧的一条。
//
// **服务器回 200 而不是 206 时必须中止**，因为照写会得到一个「大小对得上
// 但播一半就断」的坏文件：预分配把文件铺到 25 MB，正文再从 0 覆盖一遍，
// 长度还是 25 MB，歌能打开、播到第一个 10 MB 那里就断。
//
// B 站上游**完全没有这道检查**——它对这一支是当成功处理的。所以把
// 「检查那一句」拿掉，这条测试必须红。
func TestAServerThatIgnoresRangeIsAbortedNotSpliced(t *testing.T) {
	const size = 3*chunkSize + 5*1024*1024
	h := newHarness(t, size, 30280)
	h.cdn.set(func(c *cdn) { c.ignoreRange = true })

	snap := h.enqueue(StatusError)
	if !strings.Contains(snap.Err, "206") {
		t.Fatalf("报的不是「服务器不认续传」，是 %q", snap.Err)
	}

	// 🔴 断言的是「**那首歌**不存在」。半成品留着是对的——它是续传的状态，
	// 网络断了也要留着。名字后面那个 `.part` 保证它不会被当成一首歌。
	if _, err := os.Stat(snap.Dest); err == nil {
		t.Fatalf("服务器不认 Range，却还是落了盘——那就是拼出来的坏文件：%s", snap.Dest)
	}
	// 一块都不该记成功——记了就等于告诉下次「这块下过了」，而那一块里
	// 躺着的是整首歌的字节盖在中间。
	if got, _ := h.m.Get(snap.ID); got.Done != 0 {
		t.Fatalf("中止之后却记下了 %d 字节", got.Done)
	}
	// 把 206 那道检查拿掉，这条测试必须红：那时候每一发都「成功」，
	// 文件被铺到完整长度（大小对得上），然后被改名成真正的歌。
	// ⚠️ 断的是**内容**，不是「发了几发」——第一个失败会把其余的在飞
	// 请求取消掉，所以发几发本身就是竞态的。
}

// 回 206 但给的是别的段：一样不能写。
func TestAPartialReplyForTheWrongRangeIsAborted(t *testing.T) {
	const size = 2*chunkSize + 1024
	h := newHarness(t, size, 30280)
	h.cdn.set(func(c *cdn) { c.wrongRange = true })

	snap := h.enqueue(StatusError)
	if !strings.Contains(snap.Err, "Content-Range") {
		t.Fatalf("报的不是「范围不对」，是 %q", snap.Err)
	}
	if _, err := os.Stat(snap.Dest); err == nil {
		t.Fatalf("范围不对却落了盘：%s", snap.Dest)
	}
}

// 🔴 并发就是 3，不能多也不能少。
//
// 多了会被 B 站的限流掐（真机上 429），少了慢三分之一。而且「三个并发
// 写同一个文件的不同位置」这件事只有真的并发才验得到——串行的话内容
// 永远是对的。
func TestChunkRequestsAreConcurrentAndCappedAtThree(t *testing.T) {
	// 四个分块：三个在飞、一个排队。四个的话第四个一定得等，峰值稳在 3。
	const size = 4*chunkSize + 7
	h := newHarness(t, size, 30280)

	snap := h.enqueue(StatusDone)
	assertFile(t, snap.Dest, 0, size)
	if got := h.cdn.peak(); got != concurrency {
		t.Fatalf("同时在飞的分块请求峰值是 %d，要的是 %d", got, concurrency)
	}
	if concurrency != 3 {
		t.Fatalf("并发数是 %d，任务要求是 3", concurrency)
	}
	if chunkSize != 10<<20 {
		t.Fatalf("分块是 %d 字节，任务要求是 10 MB", chunkSize)
	}
}

// 🔴 进度回调被节流到 500 毫秒一次。
//
// 字节数是**每收到一个 TCP 包**报一次的（一个任务每秒几千次）。不节流
// 的话界面每秒要重画几千次，画出来的是同一帧。
func TestProgressIsThrottledToTwiceASecond(t *testing.T) {
	h := newHarness(t, 1024, 30280)
	snap, err := h.m.Enqueue(AddRequest{Bvid: bvid, Cid: cid, Title: "歌", Dir: h.dir, Ext: ".m4a"})
	if err != nil {
		t.Fatal(err)
	}
	h.wait(snap.ID, StatusDone)

	// 下的时候已经报过好几次了，节流窗口是热的。先清掉再量这一轮。
	h.m.mu.Lock()
	h.m.lastAt[snap.ID] = time.Time{}
	h.m.mu.Unlock()

	var mu sync.Mutex
	calls := 0
	h.m.SetWake(func() {
		mu.Lock()
		calls++
		mu.Unlock()
	})

	const n = 5000
	for range n {
		h.m.noteProgress(snap.ID)
	}
	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 1 {
		t.Fatalf("节流窗口内报了 %d 次进度，要的是 1 次", got)
	}
	if progressInterval != 500*time.Millisecond {
		t.Fatalf("进度节流是 %v，任务要求 500 毫秒", progressInterval)
	}
}

// 文件名净化一路走到盘上。
func TestTheSanitizedNameIsWhatLandsOnDisk(t *testing.T) {
	const size = 1024
	h := newHarness(t, size, 30280)
	// B 站歌名里三种脏东西同时出现。
	const nasty = `<em class="keyword">残酷</em>物语: 陈绮贞`

	snap, err := h.m.Enqueue(AddRequest{Bvid: bvid, Cid: cid, Title: nasty, Dir: h.dir, Ext: ".m4a"})
	if err != nil {
		t.Fatal(err)
	}
	got := h.wait(snap.ID, StatusDone)

	if want := "残酷物语_ 陈绮贞.m4s"; filepath.Base(got.Dest) != want {
		t.Fatalf("落盘的名字是 %q，期望 %q", filepath.Base(got.Dest), want)
	}
	// 入队时算的是一个**临时**名（那时还没有地址，扩展名是猜的 `.m4a`）。
	// 净化出来的那个**词干**两处必须一样——变了就说明净化不是确定性的，
	// 而界面上的歌名会跟着闪一下。
	if stem := strings.TrimSuffix(filepath.Base(got.Dest), filepath.Ext(got.Dest)); stem != "残酷物语_ 陈绮贞" {
		t.Fatalf("落盘的词干是 %q", stem)
	}
	if stem := strings.TrimSuffix(filepath.Base(snap.Dest), filepath.Ext(snap.Dest)); stem != "残酷物语_ 陈绮贞" {
		t.Fatalf("入队时算的词干是 %q", stem)
	}
	if _, err := os.Stat(got.Dest); err != nil {
		t.Fatalf("按这个名字建不出来：%v", err)
	}
}

// 同名的两首不互相盖掉。
func TestTwoSongsWithTheSameNameBothSurvive(t *testing.T) {
	const size = 1024
	cdn := newCDN(t, size)
	dir := t.TempDir()
	m, err := New(Options{
		Client: http.DefaultClient,
		Resolve: func(context.Context, Identity) (*Source, error) {
			return &Source{Bvid: bvid, Cid: cid, Quality: 30280, URLs: []string{cdn.url()}}, nil
		},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeManager(t, m, dir) })

	a, err := m.Enqueue(AddRequest{Bvid: bvid, Cid: "c1", Title: "同名", Dir: dir, Ext: ".m4a"})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, a.ID, StatusDone)

	b, err := m.Enqueue(AddRequest{Bvid: bvid, Cid: "c2", Title: "同名", Dir: dir, Ext: ".m4a"})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, b.ID, StatusDone)

	// ⚠️ 断的是**下完之后**的落盘名。入队时算的是临时名（扩展名还是猜的），
	// 真正的避让发生在开始下之前——那时才知道地址上真的后缀，也才知道
	// 第一首已经落在盘上了。
	fa := waitStatus(t, m, a.ID, StatusDone)
	fb := waitStatus(t, m, b.ID, StatusDone)
	if fa.Dest == fb.Dest {
		t.Fatalf("两首同名歌落到了同一个路径：%s", fa.Dest)
	}
	for _, p := range []string{fa.Dest, fb.Dest} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s 不在了：%v", p, err)
		}
	}
	if want := "同名 (1).m4s"; filepath.Base(fb.Dest) != want {
		t.Fatalf("第二首叫 %q，期望 %q", filepath.Base(fb.Dest), want)
	}
}

// 同一首重复点下载，队列里还是一个。
func TestTheSameSongIsNotQueuedTwice(t *testing.T) {
	h := newHarness(t, 1024, 30280)
	a, err := h.m.Enqueue(AddRequest{Bvid: bvid, Cid: cid, Title: "歌", Dir: h.dir, Ext: ".m4a"})
	if err != nil {
		t.Fatal(err)
	}
	h.wait(a.ID, StatusDone)
	b, err := h.m.Enqueue(AddRequest{Bvid: bvid, Cid: cid, Title: "歌", Dir: h.dir, Ext: ".m4a"})
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != a.ID {
		t.Fatalf("重复入队换了身份：%q → %q", a.ID, b.ID)
	}
	if got := h.m.Snapshots(); len(got) != 1 {
		t.Fatalf("同一首点了两次，队列里有 %d 项", len(got))
	}
}

// 删掉一项：队列里没有了，半成品也没了，**下完的那首歌还在**。
func TestRemoveTakesTheRecordAndThePartButKeepsTheSong(t *testing.T) {
	const size = 3 * chunkSize
	h := newHarness(t, size, 30280)
	h.cdn.set(func(c *cdn) { c.cutAt = 1 << 20 })

	snap := h.enqueue(StatusError)
	if _, err := os.Stat(snap.Dest + ".part"); err != nil {
		t.Fatalf("出错之后连 .part 都没留下，这条测试自己就不成立：%v", err)
	}
	if err := h.m.Remove(snap.ID); err != nil {
		t.Fatalf("删除：%v", err)
	}
	if _, err := os.Stat(snap.Dest + ".part"); !os.IsNotExist(err) {
		t.Fatalf("删了队列项，.part 还在下载目录里")
	}
	if _, ok := h.m.Get(snap.ID); ok {
		t.Fatalf("删了还能问出来")
	}
}

// 🔴 一边下着一边删 / 清空。
//
// 这一组是为了**并发**来的，不是因为「删完之后队列对不对」（那个上面已经
// 断过了）：下载跑在后台 goroutine 上，而 `Remove` / `Clear` / `Enqueue`
// 跑在调用方的 goroutine 上，两者同时改同一张任务表。
//
// 这一条用例**当不了竞态检测器**（`-race` 才拦得住），但它保证那两个动作
// 在「真的有一首在飞」的时候不会死锁、不会 panic、不会删不掉。
func TestRemovingAndClearingWhileADownloadIsInFlight(t *testing.T) {
	// 分块多、每块慢：让「在飞」这件事持续得够久。
	const size = 8 * chunkSize
	h := newHarness(t, size, 30280)
	h.cdn.set(func(c *cdn) { c.cutAt = 2 << 20 }) // 每块只给 2 MB 就掐断

	// 1) 下一首，跑到一半删掉它。
	a, err := h.m.Enqueue(AddRequest{Bvid: bvid, Cid: "c1", Title: "要删的", Dir: h.dir})
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool { return len(h.cdn.requests()) > 0 })
	if err := h.m.Remove(a.ID); err != nil {
		t.Fatalf("删掉正在下的那一项：%v", err)
	}
	if _, ok := h.m.Get(a.ID); ok {
		t.Fatalf("删了还能问出来")
	}

	// 2) 再下一首，跑到一半清空整个队列。
	b, err := h.m.Enqueue(AddRequest{Bvid: bvid, Cid: "c2", Title: "要清空的", Dir: h.dir})
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool { return len(h.cdn.requests()) > 1 })
	h.m.Clear()
	if got := h.m.Snapshots(); len(got) != 0 {
		t.Fatalf("清空之后队列里还有 %d 项", len(got))
	}

	// 3) 清空之后还能接着用：后台那条线不该已经退了。
	c, err := h.m.Enqueue(AddRequest{Bvid: bvid, Cid: "c3", Title: "还有歌能下", Dir: h.dir})
	if err != nil {
		t.Fatal(err)
	}
	h.cdn.set(func(cdn *cdn) { cdn.cutAt = 0 })
	if err := h.m.Resume(b.ID); err == nil {
		t.Fatalf("清空之后还能继续一个已经不在队列里的任务")
	}
	waitStatus(t, h.m, c.ID, StatusDone)

	// 队列里只剩这一首，而且它下完了。
	if got := h.m.Snapshots(); len(got) != 1 || got[0].ID != c.ID {
		t.Fatalf("队列是 %+v", got)
	}
	assertFile(t, mustGetDest(t, h.m, c.ID), 0, size)
}

func mustGetDest(t *testing.T, m *Manager, id string) string {
	t.Helper()
	s, ok := m.Get(id)
	if !ok {
		t.Fatalf("队列里没有 %s", id)
	}
	return s.Dest
}

// ---- 小工具 ----

// writePart 在盘上造一段半成品。
func writePart(t *testing.T, c *cdn, path string, variant int64, size int64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1<<20)
	for off := int64(0); off < size; {
		n := int64(len(buf))
		if off+n > size {
			n = size - off
		}
		for i := int64(0); i < n; i++ {
			buf[i] = payloadByte(variant, off+i)
		}
		if _, err := f.Write(buf[:n]); err != nil {
			t.Fatal(err)
		}
		off += n
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// saveQueue 往队列文件里写一份。
func (h *harness) saveQueue(t *testing.T, tasks map[string]*Task) {
	t.Helper()
	list := make([]*Task, 0, len(tasks))
	for _, v := range tasks {
		list = append(list, v)
	}
	b, err := json.MarshalIndent(queueFile{Version: storeVersion, Tasks: list}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	h.queueFile = filepath.Join(h.dir, QueueFileName)
	if err := os.WriteFile(h.queueFile, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// restart 读**已经写好的**队列文件重新起一个队列。
//
// 不先存盘：调用方要断的是「读回来对不对」，而队列文件的内容是由
// `saveQueue`（造一个下到一半的世界）或上一个用例的 `Save` 定好的。
// 带路径地 Close 会把内存里那份覆盖回去，把那些精心造好的状态抹掉。
func (h *harness) restart() *harness {
	h.t.Helper()
	h.m.Close("")
	return h.reopen()
}

// reopen 走真实的退出流程（先把队列落盘）再读回来。
func (h *harness) reopen() *harness {
	h.t.Helper()
	h.m.Close(h.queueFile)
	h2 := &harness{t: h.t, cdn: h.cdn, dir: h.dir, queueFile: h.queueFile}
	m, err := New(Options{
		Client: http.DefaultClient,
		Resolve: func(context.Context, Identity) (*Source, error) {
			return &Source{
				Bvid: bvid, Cid: cid, Quality: 30280,
				Label: "320K", URLs: []string{h.cdn.url()},
			}, nil
		},
	}, h.queueFile)
	if err != nil {
		h.t.Fatalf("重启：%v", err)
	}
	h2.m = m
	h2.cdn.mu.Lock()
	h2.cdn.chunkReqs = nil // 上一轮的发数不算这一轮的
	h2.cdn.mu.Unlock()
	h.t.Cleanup(func() { closeManager(h.t, m, h.dir) })
	return h2
}

// settle 等请求数不再增长。
func settle(t *testing.T, c *cdn) {
	t.Helper()
	last, stable := -1, 0
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n := len(c.requests())
		if n == last {
			if stable++; stable >= 3 {
				return
			}
		} else {
			stable, last = 0, n
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("等不到条件")
}

func waitStatus(t *testing.T, m *Manager, id string, want Status) Snapshot {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if s, ok := m.Get(id); ok && s.Status == want {
			return s
		}
		time.Sleep(2 * time.Millisecond)
	}
	s, _ := m.Get(id)
	t.Fatalf("等不到状态 %q，现在 %q（%s）", want, s.Status, s.Err)
	return s
}

// 断的那些错误确实能拿 errors.Is 判出来。
func TestTheAbortErrorsAreIdentifiable(t *testing.T) {
	// 调用方要分清「服务器不认续传」（换备用地址也没用）和「这段 403 了」
	// （换个地址就成了）——两者的处置完全不同。
	for _, e := range []error{ErrRangeUnsupported, ErrRangeMismatch, ErrNoSource, ErrNotFound, ErrPaused} {
		wrapped := fmt.Errorf("外面套一层: %w", e)
		if !errors.Is(wrapped, e) {
			t.Fatalf("errors.Is 认不出 %v", e)
		}
	}
}

// 落盘的目录**不存在时会建**。
//
// ⚠️ **这一条是默认落点那条路的一半**：默认落点是「音乐」目录下的应用子目录
// （`~/Music/BiuGo`），而它**不一定已经存在**——不建的话第一次下载会失败，
// 而失败是 `os.OpenFile` 报的「no such file or directory」，用户看到的是一句
// 他看不懂的话，实际原因只是「那个文件夹还没建」。
//
// ⚠️ **它建在落盘的那一刻**，不是入队的时候：入队只是排个队，而那一首可能
// 最后被删掉、被暂停、被清空——那时候那个文件夹就不该被建出来。
func TestTheDownloadDirectoryIsCreatedWhenTheSongLands(t *testing.T) {
	const size = 1024
	h := newHarness(t, size, 30280)

	// 那个目录**故意不存在**。
	dir := filepath.Join(h.dir, "还没建", "BiuGo")
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("%q 本来就不该存在", dir)
	}

	snap, err := h.m.Enqueue(AddRequest{Bvid: bvid, Cid: cid, Title: "测试的歌", Dir: dir, Ext: ".m4a"})
	if err != nil {
		t.Fatal(err)
	}
	got := h.wait(snap.ID, StatusDone)

	if filepath.Dir(got.Dest) != dir {
		t.Fatalf("歌落到了 %q，期望 %q 下面", got.Dest, dir)
	}
	if fi, err := os.Stat(dir); err != nil {
		t.Fatalf("落盘的那个目录没建出来：%v", err)
	} else if !fi.IsDir() {
		t.Fatalf("%q 不是目录", dir)
	}
	if _, err := os.Stat(got.Dest); err != nil {
		t.Fatalf("歌没落到那儿：%v", err)
	}
}
