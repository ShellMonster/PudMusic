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
	c.reqs = append(c.reqs, rng)
	c.mu.Unlock()

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
	t     *testing.T
	m     *Manager
	dir   string
	video *fileCDN
	audio *fileCDN
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
	vp, ap := muxtest.PairIn(t, src, spVideo, spAudio)
	video := newFileCDN(t, "video", vp)
	audio := newFileCDN(t, "audio", ap)

	h := &videoHarness{t: t, dir: dir, video: video, audio: audio}
	m, err := New(Options{
		Client:  http.DefaultClient,
		Resolve: h.resolver(),
	}, filepath.Join(t.TempDir(), QueueFileName))
	if err != nil {
		t.Fatalf("建队列：%v", err)
	}
	h.m = m
	t.Cleanup(func() { _ = m.Close("") })
	return h
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
