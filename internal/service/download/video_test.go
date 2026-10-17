// 「下载一个视频条目」这条路的端到端用例。
//
// # 这一组盯的是四件事
//
//	1. **两条流都下完 → 落一个 `.mp4`**（不是两条 `.m4s`，也不是一条
//	   有画面没声音的）。用户点的是「下载视频」，拿到的东西得能播。
//	2. **进度是两条流加起来**。记一条就报「100%」而其实还有一半没下，
//	   而那一刻盘上是个还没有声音的 MV。
//	3. **两条流各自续传**。视频那个流几百 MB，中途断网重来是常事。
//	4. **交织失败不留在盘上，而且说得出为什么**。记录页那一行的红字要
//	    是真话——「两条流对不上，差了 7 秒」而不是一句「下载失败」。
//
// 被 mock 掉的只有「B 站的 CDN 什么时候让地址失效」。分块、Range、206
// 检查、并发、续传、交织全都在真的跑：两条 dash 流是 `muxtest` 造出来的
// 真 fMP4，产出的 `.mp4` 会被解回来数轨道。

package download

import (
	"context"
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

	"github.com/Eyevinn/mp4ff/mp4"

	"github.com/ShellMonster/Biu_Go/internal/service/mux/muxtest"
)

// fileCDN 是一台按**文件**回答的假 CDN。
//
// 上一组用例里的那台 `cdn` 是现算正文的（`payloadByte`），而 dash 流
// 必须是货真价实的 fMP4——交织要读 `moov` 和一串 `moof`，编不出来。
// 所以这里老老实实把文件读进内存再切。
type fileCDN struct {
	name string
	body []byte

	srv *httptest.Server

	mu        sync.Mutex
	reqs      []string
	cutAt     int64 // >0：每次响应只给这么多字节就掐断（模拟下到一半）
	inFlight  int
	peakRatio int

	// gate 非 nil 时，每次响应在送正文**之前**先等它关上。
	//
	// 它存在是为了把下载**确定地**按在「请求发出去了、正文一个字节还没到」
	// 那一刻：靠「素材够大所以慢」来撞那个时机，在负载一高的机器上就是
	// 时好时坏（而它时好时坏的样子是「照样通过，看不出自己没测到东西」）。
	gate      chan struct{}
	blocked   int
	requests0 int
}

// hold 把这台 CDN 关上，直到 release。
func (c *fileCDN) hold() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gate = make(chan struct{})
}

// release 放行。
func (c *fileCDN) release() {
	c.mu.Lock()
	g := c.gate
	c.gate = nil
	c.mu.Unlock()
	if g != nil {
		close(g)
	}
}

// waiting 报有几发请求正卡在闸门上。
func (c *fileCDN) waiting() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.blocked
}

func newFileCDN(t *testing.T, name, path string) *fileCDN {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s：%v", path, err)
	}
	c := &fileCDN{name: name, body: b}
	c.srv = httptest.NewServer(c)
	t.Cleanup(c.srv.Close)
	return c
}

func (c *fileCDN) url() string {
	return c.srv.URL + "/" + c.name + ".m4s?upsig=x&deadline=9999999999"
}

func (c *fileCDN) requests() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.reqs...)
}

// cut 让这台 CDN 之后**每一次**响应只给这么多字节就掐断连接
// （模拟「下到一半断了」）。0 = 恢复正常。
func (c *fileCDN) cut(bytes int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cutAt = bytes
}

func (c *fileCDN) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	c.inFlight++
	if c.inFlight > c.peakRatio {
		c.peakRatio = c.inFlight
	}
	cut := c.cutAt
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.inFlight--
		c.mu.Unlock()
	}()

	rng := r.Header.Get("Range")
	if rng == "" {
		w.Header().Set("Content-Length", strconv.Itoa(len(c.body)))
		c.write(w, 0, int64(len(c.body)), cut)
		return
	}
	start, end, ok := parseRange(rng, int64(len(c.body)))
	if !ok {
		http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	c.mu.Lock()
	// 探长度的那一发（`bytes=0-0`）**不算分块请求**——和 `cdn` 那个
	// harness 一致。每一轮取流都要重新探一次（地址会过期、身份要再比
	// 一次），那是必然的，不该被「续传时重下了多少」这类断言算进去。
	if rng != "bytes=0-0" {
		c.reqs = append(c.reqs, rng)
	}
	c.mu.Unlock()

	c.mu.Lock()
	g := c.gate
	c.mu.Unlock()
	if g != nil {
		c.mu.Lock()
		c.blocked++
		c.mu.Unlock()
		select {
		case <-g:
		case <-r.Context().Done():
			// 被取消了：这一发一个正文都不给（而这一块因此没下全）。
			c.mu.Lock()
			c.blocked--
			c.mu.Unlock()
			return
		}
		c.mu.Lock()
		c.blocked--
		c.mu.Unlock()
	}

	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(c.body)))
	w.WriteHeader(http.StatusPartialContent)
	c.write(w, start, end+1, cut)
}

// write 送 [start, end) 那几个字节；cut>0 时**总共**只送 cut 个就掐断。
//
// ⚠️ 「总共」不是「每次写 cut 个」：后者会把整个分块一字节一字节地送完，
// 一次连接中断也没发生过，而这条用例要测的恰恰是「连接断了」。
func (c *fileCDN) write(w http.ResponseWriter, start, end, cut int64) {
	const buf = 256 << 10
	sent := int64(0)
	off := start
	for off < end {
		if cut > 0 && sent >= cut {
			return // 掐断：后面的字节一个都不给
		}
		n := end - off
		if n > buf {
			n = buf
		}
		if cut > 0 && n > cut-sent {
			n = cut - sent
		}
		if _, err := w.Write(c.body[off : off+n]); err != nil {
			return
		}
		off += n
		sent += n
	}
}

// videoHarness 是一套会下视频的队列。
type videoHarness struct {
	t         *testing.T
	m         *Manager
	dir       string
	queueFile string
	video     *fileCDN
	audio     *fileCDN
}

func newVideoHarness(t *testing.T, spVideo, spAudio muxtest.Spec) *videoHarness {
	t.Helper()
	dir := t.TempDir()
	// 源流先造在一个**别的**目录里：它们不是下载产物，混进下载目录
	// 会让「盘上到底剩了什么」这条断言读不清。
	src := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("建源目录：%v", err)
	}
	// 视频流给一个**负的** cts 偏移（B 帧重排的形状，也是真 B 站 dash 的
	// 常态）。它让首帧的呈现时间落在媒体时间 0 **之前**，于是交织一定走
	// 「要补一段空编辑」那一支。
	//
	// ⚠️ 这一条不是装饰。没有它，这一组里每一条端到端用例交织出来的文件
	// 里 `edts` 盒数都是 0 —— 也就是**评审逮到的那个重复 `edts` 的 bug
	// （产物在 macOS 上没有画面）根本走不到**。真素材上有 B 帧重排、
	// 没有真素材上就没有，而���一版的素材恰好没有。
	spVideo = spVideo.WithCtsBias(-3000)
	vp, ap := muxtest.PairIn(t, src, spVideo, spAudio)
	video := newFileCDN(t, "video", vp)
	audio := newFileCDN(t, "audio", ap)

	h := &videoHarness{t: t, dir: dir, queueFile: filepath.Join(t.TempDir(), QueueFileName),
		video: video, audio: audio}
	m, err := New(Options{
		Client:  http.DefaultClient,
		Resolve: h.resolver(),
	}, h.queueFile)
	if err != nil {
		t.Fatalf("建队列：%v", err)
	}
	h.m = m
	t.Cleanup(func() { _ = m.Close("") })
	return h
}

// reopen 走真实的退出流程（先把队列落盘）再读回来。
func (h *videoHarness) reopen() *videoHarness {
	h.t.Helper()
	if err := h.m.Save(h.queueFile); err != nil {
		h.t.Fatalf("存队列：%v", err)
	}
	m, err := New(Options{Client: http.DefaultClient, Resolve: h.resolver()}, h.queueFile)
	if err != nil {
		h.t.Fatalf("重开：%v", err)
	}
	h.t.Cleanup(func() { _ = m.Close("") })
	h2 := &videoHarness{t: h.t, m: m, dir: h.dir, queueFile: h.queueFile,
		video: h.video, audio: h.audio}
	return h2
}

// resolver 是这套队列的取流：两条 dash 流。
func (h *videoHarness) resolver() Resolver {
	return func(context.Context, Identity) (*Source, error) {
		return &Source{
			Bvid: bvid, Cid: cid, Quality: 120, Label: "1080P",
			URLs:          []string{h.video.url()},
			CompanionURLs: []string{h.audio.url()},
		}, nil
	}
}

func (h *videoHarness) enqueue(want Status) Snapshot {
	h.t.Helper()
	snap, err := h.m.Enqueue(AddRequest{
		Bvid: bvid, Cid: cid, Title: "测试的 MV", Dir: h.dir, Video: true,
	})
	if err != nil {
		h.t.Fatalf("入队：%v", err)
	}
	return h.wait(snap.ID, want)
}

func (h *videoHarness) wait(id string, want Status) Snapshot {
	h.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
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

// leftovers 列出下载目录里除最终产物以外的所有文件。
func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	got, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		t.Fatalf("列 %s：%v", dir, err)
	}
	return got
}

// ---- 一、能落一个能播的文件 ----

// TestAVideoDownloadLandsOnePlayableMP4 是这一票的正题。
func TestAVideoDownloadLandsOnePlayableMP4(t *testing.T) {
	h := newVideoHarness(t, muxtest.VideoSpec(300), muxtest.AudioSpec(10))
	snap := h.enqueue(StatusDone)

	// 落盘名必须是 `.mp4`，不是两条流的 `.m4s`。
	// `歌.m4s` 是最像样的错：文件名在、内容对（确实是 fMP4），而双击打不开。
	if got := filepath.Ext(snap.Dest); got != ".mp4" {
		t.Errorf("落盘的后缀是 %q，要的是 .mp4（两条源流都是 .m4s，"+
			"而落盘的不是任何一条流，是交织出来的那个）", got)
	}

	f, err := os.Open(snap.Dest)
	if err != nil {
		t.Fatalf("打开产物：%v", err)
	}
	defer f.Close()
	out, err := mp4.DecodeFile(f)
	if err != nil {
		t.Fatalf("产物解不出来（交织没写对）：%v", err)
	}

	// **两条轨**。只有一条的话就是一个「能打开、没声音」的 MV。
	if len(out.Moov.Traks) != 2 {
		t.Fatalf("产物里有 %d 条轨，要的是 2（视频 + 音频）", len(out.Moov.Traks))
	}
	var sawVideo, sawAudio bool
	for _, trak := range out.Moov.Traks {
		stsd := trak.Mdia.Minf.Stbl.Stsd
		switch trak.Mdia.Hdlr.HandlerType {
		case "vide":
			sawVideo = true
			if stsd.AvcX == nil || stsd.AvcX.AvcC == nil {
				t.Error("产物里视频轨的 avcC 不在了")
			}
		case "soun":
			sawAudio = true
			if stsd.Mp4a == nil || stsd.Mp4a.Esds == nil {
				t.Error("产物里音频轨的 esds 不在了")
			}
		}
	}
	if !sawVideo || !sawAudio {
		t.Errorf("产物里视频轨=%v 音频轨=%v，要的是两条都有", sawVideo, sawAudio)
	}

	// 采样要真的搬过去了，不是两个空壳。
	if n := out.Moov.Traks[0].GetNrSamples(); n == 0 {
		t.Error("第一条轨一个采样都没有")
	}

	// 每条轨最多一个 `edts`，而且**这一组素材一定会走出 `edts`**。
	//
	// 多一个的话 macOS 的 AVFoundation 会把**整条轨**丢掉：文件能打开、
	// 只有声音、拖到中间那一帧解不出来（评审拿真 AVFoundation 验的，
	// 见 `mux` 包的 `TestATrackCarriesExactlyOneEditList`）。
	//
	// ⚠️ 断的是 `Children` 里的**个数**而不是 `trak.Edts` 那个字段：
	// mp4ff 的解码器把后一个 `edts` 盖在那个字段上、**不报错**，所以
	// 断言「那个字段非空」在重复写两遍的时候照样通过。
	edtsBoxes := 0
	for _, trak := range out.Moov.Traks {
		n := 0
		for _, c := range trak.Children {
			if _, ok := c.(*mp4.EdtsBox); ok {
				n++
			}
		}
		if n > 1 {
			t.Errorf("%s 轨里有 %d 个 edts", trak.Mdia.Hdlr.HandlerType, n)
		}
		edtsBoxes += n
	}
	if edtsBoxes == 0 {
		t.Error("产物里一个 edts 都没有——素材是带 B 帧重排的（B 帧的呈现偏移是负的），" +
			"交织一定该补一段空编辑。没有它，这一组用例就永远走不到出 edts 的那一支")
	}

	// 盘上除了这个产物**一个别的都不该有**：两个半成品、两条源流、
	// 任何临时文件。
	if got := leftovers(t, h.dir); len(got) != 1 {
		t.Errorf("下载目录里有 %d 个东西，要的只有产物那一个：%v", len(got), got)
	}
}

// ---- 二、进度是两条流加起来 ----

// TestAVideoDownloadCountsBothStreamsInItsProgress 钉住「总长 = 视频 + 音频」。
//
// 只记一条的后果很具体：视频流下完的那一刻进度条就报 100%，而盘上
// 那个 `.part` 交不出来（音频还在下）。用户看着「下完了」然后等了
// 十几秒，那十几秒里界面什么也不说。
func TestAVideoDownloadCountsBothStreamsInItsProgress(t *testing.T) {
	spV, spA := muxtest.VideoSpec(300), muxtest.AudioSpec(10)
	h := newVideoHarness(t, spV, spA)

	var maxSeen int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if s, ok := h.m.Get(TaskID(bvid, cid)); ok {
				h.m.mu.Lock()
				if s.Total > maxSeen {
					maxSeen = s.Total
				}
				h.m.mu.Unlock()
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	snap := h.enqueue(StatusDone)
	close(stop)
	wg.Wait()

	// 两条流都在盘上（这两条小，一次请求就下完了），所以总长就是两条之和。
	wantTotal := int64(len(h.video.body)) + int64(len(h.audio.body))
	if snap.Total != wantTotal {
		t.Errorf("总长是 %d，两条流加起来是 %d", snap.Total, wantTotal)
	}
	if snap.Done != snap.Total {
		t.Errorf("下完那一刻是 %d/%d 字节", snap.Done, snap.Total)
	}
	if snap.Percent != 100 {
		t.Errorf("下完那一刻进度是 %d%%", snap.Percent)
	}
}

// ---- 三、交织失败：真话 + 盘上干净 ----

// TestAFailedMuxLeavesNothingBehindAndSaysWhy 钉住「交织失败怎么办」。
//
// 这一票最要紧的一句在任务里：「交织失败的下载在记录页显示真实原因」。
// 所以断的是两件事：盘上**不能**留着一个半成品（用户在自己的下载目录里
// 看到的是完整的一个文件），而那一行的 `Err` **要说得清为什么**。
func TestAFailedMuxLeavesNothingBehindAndSaysWhy(t *testing.T) {
	// 视频 10 秒、音频 3 秒——差 7 秒，交织必须拒绝写。
	h := newVideoHarness(t, muxtest.VideoSpec(300), muxtest.AudioSpec(3))
	snap := h.enqueue(StatusError)

	if snap.Err == "" {
		t.Fatal("交织失败了却一个字都没说——记录页上那一行会是空白")
	}
	if !strings.Contains(snap.Err, "对不上") {
		t.Errorf("报错没说是两条流对不上：%q", snap.Err)
	}
	// ⚠️ 这里**不是**「目录要空」。两条源流是下载的成果，续传要靠它们
	// （`prepare` 认的是这些路径和分块记账），出错时留着是规矩——
	// 跟音频那一票一样，删不删由 `Remove` / `Clear` 说了算。
	// 真正要断的是：**成片那个位置上不能有东西**。落了一个交织失败的
	// `.mp4` 才是最糟的——它能打开，就是没声音。
	assertNoLandedFile(t, h.dir)
}

// TestAVideoWithATruncatedAudioStreamDoesNotLand 是上一条的另一种样子。
//
// 音频流只下到一半：`.part` 是**预分配**到总长的（`prepare` 的 `Truncate`），
// 所以文件大小看着是对的，里面有一段是零。不看内容就说「下完了」的
// 引擎会给用户一个播几秒就没声的 MV。
func TestAVideoWithATruncatedAudioStreamDoesNotLand(t *testing.T) {
	h := newVideoHarness(t, muxtest.VideoSpec(120), muxtest.AudioSpec(4))
	// 音频那条流掐断在第 1 个采样之前：分块请求会拿到 1 个字节然后断。
	h.audio.cut(1)
	snap := h.enqueue(StatusError)

	if snap.Err == "" {
		t.Error("音频流没下全，交织那一步一个字都没说")
	}
	assertNoLandedFile(t, h.dir)
}

// assertNoLandedFile 断盘上**没有成片**。
//
// 断的不是「目录空不空」：出错的视频任务要留着两条源流好续传
// （见 `runMux` 的说明）。要断的是那个 `.mp4`——落一个交织失败的
// `.mp4` 是最糟的结果，它能打开、能拖进度条，就是没声音。
func assertNoLandedFile(t *testing.T, dir string) {
	t.Helper()
	for _, p := range leftovers(t, dir) {
		if filepath.Ext(p) == ".mp4" {
			t.Errorf("交织失败了却落了一个成片：%s", filepath.Base(p))
		}
	}
}

// ---- 四、两条流各自续传 ----

// TestAVideoDownloadResumesBothStreams 钉住「两条流各自续传」。
//
// 视频那个流几百 MB，中途断网重来是常事。这里让音频那条掐断、停下来、
// 重开队列再点「继续」，然后断：
//
//	音频那条**重下的正好是它没下完的那些块**（不是整个重来）
//	最后仍然落出一个两条轨的 mp4
func TestAVideoDownloadResumesBothStreams(t *testing.T) {
	h := newVideoHarness(t, muxtest.VideoSpec(300), muxtest.AudioSpec(10))
	h.audio.cut(1)
	snap := h.enqueue(StatusError)
	if snap.Err == "" {
		t.Fatal("音频没下全却没报错")
	}

	// 掐断关掉，这一轮正常下。
	h.audio.cut(0)
	before := len(h.audio.requests())
	if err := h.m.Resume(snap.ID); err != nil {
		t.Fatalf("继续：%v", err)
	}
	h.wait(snap.ID, StatusDone)

	if got := len(h.audio.requests()) - before; got == 0 {
		t.Error("继续之后音频一条请求都没发")
	}
	f, err := os.Open(snap.Dest)
	if err != nil {
		t.Fatalf("打开产物：%v", err)
	}
	defer f.Close()
	out, err := mp4.DecodeFile(f)
	if err != nil {
		t.Fatalf("续传之后产物解不出来：%v", err)
	}
	if len(out.Moov.Traks) != 2 {
		t.Errorf("续传之后产物里有 %d 条轨", len(out.Moov.Traks))
	}
}

// ---- 五、删除要两个半成品都删掉 ----

// TestRemovingAVideoTaskDeletesEveryHalfProduct 钉住「删除不留孤儿」。
//
// 视频有**三个**半成品（两条源流 + 交织的产物），只删主那个的话另外两个
// 永远躺在下载目录里，而界面上再没有任何东西指向它（`destFor` 那段
// 注释里的四步孤儿场景）。
//
// ⚠️ 这条用例**必须让下载停在半路**（掐断音频那条流），否则它会在
// `Remove` 之前就下完——而删完的成片是**故意留着**的（「用户删的是队列
// 里这一项，不是硬盘上那首歌」，见 `Remove`），那时候盘上理应剩下
// 一个 `.mp4`，而断言会看起来像失败。
func TestRemovingAVideoTaskDeletesEveryHalfProduct(t *testing.T) {
	h := newVideoHarness(t, muxtest.VideoSpec(300), muxtest.AudioSpec(10))
	h.audio.cut(1) // 下不完：这一项会停在半路

	snap, err := h.m.Enqueue(AddRequest{Bvid: bvid, Cid: cid, Title: "待删的 MV", Dir: h.dir, Video: true})
	if err != nil {
		t.Fatalf("入队：%v", err)
	}
	// 等两条源流的半成品都建出来再删。
	waitUntil(t, func() bool {
		return exists(partPath(snap.Dest)) && exists(companionPartPath(snap.Dest))
	})
	if err := h.m.Remove(snap.ID); err != nil {
		t.Fatalf("删除：%v", err)
	}
	if got := leftovers(t, h.dir); len(got) != 0 {
		t.Errorf("删完之后下载目录里还剩 %d 个东西：%v", len(got), got)
	}
}

// ---- 六、重开应用时正好停在「交织中」 ----

// TestAMuxingTaskSurvivesARestartAndFinishesOnResume 钉住
// 「重开时状态是「交织中」的那一项」会变成什么。
//
// 交织一个几百 MB 的 MV 要十几秒，而用户在这十几秒里退出应用是常事。
// 那一刻盘上躺着的是：**两条流都在、都下全了、`.mp4` 还没出来**。
//
// 这一条断的是「继续之后要走多远」：
//
//	两条流在盘上、分块记账是齐的 → 探长度、对身份、拉字节全都不用重做，
//	直接走到交织那一步，几秒钟就出结果
//
// 而如果恢复时把「交织中」当成别的（比如当成 done），用户看到的是一行
// 「已完成」而盘上什么都没有；当成 queued 的话「继续」会从头再下几百兆。
func TestAMuxingTaskSurvivesARestartAndFinishesOnResume(t *testing.T) {
	spV, spA := muxtest.VideoSpec(300), muxtest.AudioSpec(10)
	dir := t.TempDir()
	src := t.TempDir()
	vp, ap := muxtest.PairIn(t, src, spV, spA)
	video := newFileCDN(t, "video", vp)
	audio := newFileCDN(t, "audio", ap)

	// 造出「正下到一半被退出打断」那一刻的盘面：两条 `.part` 都在，
	// 而且都**是全的**（下载引擎下完一条流就是整条下完才进交织）。
	dest := filepath.Join(dir, "测试的 MV.mp4")
	write := func(part string, body []byte) {
		f, err := os.Create(part)
		if err != nil {
			t.Fatalf("建 %s：%v", part, err)
		}
		defer f.Close()
		if _, err := f.Write(body); err != nil {
			t.Fatalf("写 %s：%v", part, err)
		}
	}
	write(partPath(dest), video.body)
	write(companionPartPath(dest), audio.body)

	task := &Task{
		ID:       TaskID(bvid, cid),
		Title:    "测试的 MV",
		Identity: Identity{Bvid: bvid, Cid: cid, Quality: 120, Video: true, Length: int64(len(video.body))},
		Ext:      videoExt,
		Dest:     dest,
		Video:    true,
		Chunks:   allChunks(len(video.body)),
		Companion: &Companion{
			Length: int64(len(audio.body)),
			Chunks: allChunks(len(audio.body)),
		},
		Status: StatusMuxing,
	}
	queueFile := filepath.Join(t.TempDir(), QueueFileName)

	resolve := func(_ context.Context, want Identity) (*Source, error) {
		src := &Source{
			Bvid: bvid, Cid: cid, Quality: 120, Label: "1080P",
			URLs: []string{video.url()},
		}
		if want.Video {
			src.CompanionURLs = []string{audio.url()}
		}
		return src, nil
	}
	m1, err := New(Options{Client: http.DefaultClient, Resolve: resolve}, queueFile)
	if err != nil {
		t.Fatalf("建队列：%v", err)
	}
	m1.mu.Lock()
	m1.tasks[task.ID] = task
	m1.order = []string{task.ID}
	m1.mu.Unlock()
	if err := m1.Save(queueFile); err != nil {
		t.Fatalf("存队列：%v", err)
	}
	if err := m1.Close(""); err != nil {
		t.Fatalf("关队列：%v", err)
	}

	// ⚠️ 队列文件里要真的写着「交织中」。
	//
	// `Save` 自己就会把「在飞的」一律落成暂停（`store.go` 的 `persistable`），
	// 所以直接存出来的那份已经是 paused 了——那样测的就不是 `Restore`
	// 对「交织中」的处理，而是「暂停」的往返。这一票要断的是前者：
	// 一份写着「交织中」的记录（换句话说，别的版本写出来的，或者
	// 退出时序卡在写盘之前）被读回来该变成什么。
	forceStatusInQueueFile(t, queueFile, string(StatusMuxing))

	// 重开。
	m2, err := New(Options{Client: http.DefaultClient, Resolve: resolve}, queueFile)
	if err != nil {
		t.Fatalf("重开：%v", err)
	}
	defer m2.Close("")
	got, ok := m2.Get(task.ID)
	if !ok {
		t.Fatal("重开之后队列里没有这一项")
	}
	if got.Status != StatusPaused {
		t.Fatalf("重开之后状态是 %q，要的是暂停——"+
			"那一轮的进程都没了，不可能是「交织中」", got.Status)
	}

	videoHits, audioHits := len(video.requests()), len(audio.requests())
	if err := m2.Resume(task.ID); err != nil {
		t.Fatalf("继续：%v", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if s, _ := m2.Get(task.ID); s.Status == StatusDone {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	final, _ := m2.Get(task.ID)
	if final.Status != StatusDone {
		t.Fatalf("继续之后状态是 %q，%s", final.Status, final.Err)
	}
	// 🔴 一条字节都**不许重下**：两条流在盘上、分块记账是齐的。
	if n := len(video.requests()) - videoHits; n != 0 {
		t.Errorf("继续之后又发了 %d 发视频流请求——那几百 MB 白下了", n)
	}
	if n := len(audio.requests()) - audioHits; n != 0 {
		t.Errorf("继续之后又发了 %d 发音频流请求", n)
	}
	f, err := os.Open(final.Dest)
	if err != nil {
		t.Fatalf("打开产物：%v", err)
	}
	defer f.Close()
	out, err := mp4.DecodeFile(f)
	if err != nil {
		t.Fatalf("产物解不出来：%v", err)
	}
	if len(out.Moov.Traks) != 2 {
		t.Errorf("产物里有 %d 条轨，要的是 2", len(out.Moov.Traks))
	}
}

// forceStatusInQueueFile 把队列文件里那一项的状态改成 want。
//
// 它是**直接改 JSON**，不走 `Save`——因为要测的正是 `Save` 之外那条路
// （见调用处那段注释）。
func forceStatusInQueueFile(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读队列文件：%v", err)
	}
	got := strings.Replace(string(b), `"status": "paused"`, `"status": "`+want+`"`, 1)
	if got == string(b) {
		t.Fatalf("队列文件里没找到 paused 那一项:\n%s", b)
	}
	if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
		t.Fatalf("写队列文件：%v", err)
	}
}

// TestTheMuxingStatusIsNeitherDoneNorPausable 钉住「交织中」这几个语义。
//
//	Running   = true  它在读写几百 MB 的盘，用户看到的应该是「还在动」
//	CanPause  = false 两条流已经在盘上了，没有「暂停到一半」这回事
//	CanResume = false 那一项没出错，按「继续」是空的
//
// 而 `Restore` 把它变成暂停这件事由上面那条用例钉着。
func TestTheMuxingStatusIsNeitherDoneNorPausable(t *testing.T) {
	if !StatusMuxing.Running() {
		t.Error("「交织中」不算 Running——那一刻网络空出来了，但它在动盘")
	}
	if StatusMuxing.CanPause() {
		t.Error("「交织中」可以暂停——两条流已经下完了，暂停没有可续的")
	}
	if StatusMuxing.CanResume() {
		t.Error("「交织中」可以继续——它没出错，按「继续」是空的")
	}
}

// allChunks 是一条流的全部分块下标。
func allChunks(size int) []int {
	n := totalChunks(int64(size))
	out := make([]int, 0, n)
	for i := range n {
		out = append(out, i)
	}
	return out
}

// ---- 七、交织失败时 `Dest` 不能被改掉 ----

// TestAFailedMuxKeepsTheHalfProductsWhereTheyAre 钉住「交织失败不挪 `Dest`」。
//
// `runMux` 落盘前要再看一眼那个位置上有没有别人的东西（`destToLand`），
// 而那一步**可能改 `Dest`**。交织失败时那两条半成品还留在**旧**路径上，
// `Dest` 却指到了新路径 —— 于是下一轮「重试」：
//
//	prepare 走 else 分支（账上有进度）→ os.Stat 新路径当然不存在
//	→ resetChunks → **两条流从零重下几百兆**
//
// 而旧路径上那两个几百 MB 成了**孤儿**：`Remove` / `Clear` 只认当前 `Dest`，
// 永远不会去删它们。用户看到的是「重试一次，又下了一遍，然后下载目录里
// 莫名其妙多出一份」。
func TestAFailedMuxKeepsTheHalfProductsWhereTheyAre(t *testing.T) {
	// 视频 10 秒、音频 3 秒：交织必然拒绝。
	h := newVideoHarness(t, muxtest.VideoSpec(300), muxtest.AudioSpec(3))
	snap := h.enqueue(StatusError)
	if snap.Err == "" {
		t.Fatal("交织失败了却一个字都没说")
	}

	// 造一个「那个位置上已经有别人的东西」的现场，逼 `destToLand` 让开。
	squatter := snap.Dest
	if err := os.WriteFile(squatter, []byte("别人的歌"), 0o644); err != nil {
		t.Fatalf("占位：%v", err)
	}
	before, ok := h.m.Get(snap.ID)
	if !ok {
		t.Fatal("队列里没有这一项")
	}
	if err := h.m.Resume(snap.ID); err != nil {
		t.Fatalf("重试：%v", err)
	}
	after := h.wait(snap.ID, StatusError)

	if after.Dest != before.Dest {
		t.Errorf("重试之后落盘名从 %q 变成了 %q，而那两条半成品还在旧路径上"+
			"——下一次重试会判成「半成品不见了」而从零重下几百兆，旧的那两个"+
			"又成了没人删的孤儿",
			before.Dest, after.Dest)
	}
	// 半成品要还在原来的位置上（重试失败不该删掉它们，那是续传的凭据）。
	for _, p := range []string{partPath(before.Dest), companionPartPath(before.Dest)} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("半成品 %s 不在了：%v", filepath.Base(p), err)
		}
	}
}

// ---- 八、暂停再继续 / 退出重开：这一项还是「视频」 ----

// TestAVideoSurvivesAPauseAndAResume 钉住「暂停一下再继续」之后它还是
// 视频下载。
//
// 这条是被评审第一轮逮出来的（探针叫 `TestProbePauseThenResumeInOneSession`）。
//
// 症状：暂停 → 继续 → `download: 音频流：download: 没有能下的地址`，而那一行
// **再也不动了**——它不是「下得慢」，是再也下不完。
//
// 根因在**身份**上：`prepare` 每轮都要做资源身份校验（续传的前提，见
// `Identity`），校验完把新身份**整份**写回去。而这一项自己带着的
// 「我是视频」「用户点的是哪一档」不在源告诉我们的那几样里，于是被顺带抹成
// 零值——下一轮取流看到 `want.Video == false`，就去问了一条音频，
// 而音频那一侧当然没有伴流可下。
//
// ⚠️ 界面上看不出任何异常：状态是「出错」、红字是「没有能下的地址」，而
// 用户做的事只是「暂停了一下又点了继续」。
func TestAVideoSurvivesAPauseAndAResume(t *testing.T) {
	h := newVideoHarness(t, muxtest.VideoSpec(300), muxtest.AudioSpec(10))

	// 把 CDN 关上：请求会发出去、正文一个字节都不到，而队列停在「在下」。
	h.video.hold()
	snap, err := h.m.Enqueue(AddRequest{
		Bvid: bvid, Cid: cid, Title: "测试的 MV", Dir: h.dir, Video: true, VideoQ: 120,
	})
	if err != nil {
		t.Fatalf("入队：%v", err)
	}
	waitUntil(t, func() bool { return h.video.waiting() > 0 })

	if err := h.m.Pause(snap.ID); err != nil {
		t.Fatalf("暂停：%v", err)
	}
	h.video.release()
	paused := h.wait(snap.ID, StatusPaused)
	if paused.Percent >= 100 {
		t.Fatalf("暂停时已经下到 %d%%——这一条没走到「下了一半」那个状态", paused.Percent)
	}

	if err := h.m.Resume(snap.ID); err != nil {
		t.Fatalf("继续：%v", err)
	}
	done := h.wait(snap.ID, StatusDone)
	if got := filepath.Ext(done.Dest); got != ".mp4" {
		t.Errorf("暂停再继续之后落的后缀是 %q，要的是 .mp4", got)
	}
}

// TestAVideoSurvivesASaveAndRestore 钉住「退出重开之后它还是视频下载」。
//
// 同一条根因的另一半（探针 `TestProbeIdentityVideoSurvivesSaveRestore`
// 量到的是「重开之后身份 Video=false」）。这一条走的是**落盘再读回**那条路，
// 而那一路上写的是 `Identity` 自己的字段——所以断的是「队列文件里那个
// 身份还记不记得自己是视频」。
func TestAVideoSurvivesASaveAndRestore(t *testing.T) {
	h := newVideoHarness(t, muxtest.VideoSpec(300), muxtest.AudioSpec(10))
	h.video.hold()
	snap, err := h.m.Enqueue(AddRequest{
		Bvid: bvid, Cid: cid, Title: "测试的 MV", Dir: h.dir, Video: true, VideoQ: 120,
	})
	if err != nil {
		t.Fatalf("入队：%v", err)
	}
	waitUntil(t, func() bool { return h.video.waiting() > 0 })
	if err := h.m.Pause(snap.ID); err != nil {
		t.Fatalf("暂停：%v", err)
	}
	h.video.release()
	h.wait(snap.ID, StatusPaused)

	if err := h.m.Save(h.queueFile); err != nil {
		t.Fatalf("存队列：%v", err)
	}
	h2 := h.reopen()

	got, ok := h2.m.Get(snap.ID)
	if !ok {
		t.Fatal("重开之后队列里没有这一项")
	}
	if got.Status != StatusPaused {
		t.Fatalf("重开之后状态是 %q，要的是暂停", got.Status)
	}
	if err := h2.m.Resume(snap.ID); err != nil {
		t.Fatalf("继续：%v", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if s, _ := h2.m.Get(snap.ID); s.Status == StatusDone || s.Status == StatusError {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	final, _ := h2.m.Get(snap.ID)
	if final.Status != StatusDone {
		t.Fatalf("重开再继续之后状态是 %q，%s", final.Status, final.Err)
	}
	if got := filepath.Ext(final.Dest); got != ".mp4" {
		t.Errorf("落的后缀是 %q，要的是 .mp4", got)
	}
}
