// 「下载视频」在 app 层的接线。
//
// 这一组盯的是**只有 app 层知道**的几件事：
//
//	1. 取流要走 dash 的**一对**流，拿到的是同一个 cid 的两条
//	2. 拉字节用的 http.Client 跟在线播放是**同一个**（凭据边界）
//	3. 落盘的是交织完的单个 `.mp4`，两条源流不再留在下载目录里
//	4. 默认不带 `qn`（要哪一档是设置页的事，见 `resolveVideoDownload`）
//
// 下载引擎本身（分块、206、续传、交织的细节）在 `service/download` 的
// 包内测试里跑，那边能精确造出每一种形态。

package app_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Eyevinn/mp4ff/mp4"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
	"github.com/ShellMonster/Biu_Go/internal/service/mux/muxtest"
)

// dashPairFor 造一份带视频的 dash 段（一条视频 + 一条音频）。
//
// ⚠️ 两条流的地址要指向同一集：交织那一步会**逐条**查这个（见
// `resolveVideoDownload`），而真取错了的症状是「能播、播两分钟就没声」。
func dashPairFor(videoURL, audioURL string) map[string]any {
	return map[string]any{
		"duration": 2,
		"video": []any{map[string]any{
			"id": 80, "bandwidth": 2_000_000, "codecs": "avc1.640028",
			"mimeType": "video/mp4", "width": 1280, "height": 720,
			"frameRate": "30", "baseUrl": videoURL,
		}},
		"audio": []any{map[string]any{
			"id": 30280, "bandwidth": 320000, "codecs": "mp4a.40.2",
			"mimeType": "audio/mp4", "baseUrl": audioURL,
		}},
	}
}

// dashPairFixture 是一台已经能回答「取流 + 两条 CDN」的 fixture。
type dashPairFixture struct {
	f     *streamFixture
	video []byte
	audio []byte
}

func newDashPairFixture(t *testing.T) *dashPairFixture {
	t.Helper()
	f := newStreamFixture(t)
	// 源流是真的 fMP4——交织要读 `moov` 和一串 `moof`，编不出来。
	src := t.TempDir()
	// 视频流带负的 cts 偏移（B 帧重排 = 真 dash 的常态），让交织一定走
	// 「要补一段空编辑」那一支。理由见 download 包里同名的那条注释。
	vp, ap := muxtest.PairIn(t, src, muxtest.VideoSpec(120).WithCtsBias(-3000), muxtest.AudioSpec(4))
	d := &dashPairFixture{f: f, video: mustReadFile(t, vp), audio: mustReadFile(t, ap)}
	f.on("/cdn/video.m4s", rangeHandler(d.video))
	f.on("/cdn/audio.m4s", rangeHandler(d.audio))
	f.onPlayURL(dashPairFor(
		cdnURL(f.URL, "video.m4s", time.Hour),
		cdnURL(f.URL, "audio.m4s", time.Hour),
	))
	return d
}

func (d *dashPairFixture) app(t *testing.T, dlDir string) *app.App {
	t.Helper()
	return newDownloadApp(t, d.f, dlDir, filepath.Join(t.TempDir(), download.QueueFileName))
}

// 🔴 一条完整的线：点「下载视频」→ 取 dash 那一对 → 两条都下完 → 交织 →
// 落一个能播的 `.mp4`。
func TestAVideoDownloadLandsAnMP4WithBothTracks(t *testing.T) {
	d := newDashPairFixture(t)
	dir := t.TempDir()
	a := d.app(t, dir)

	snap, err := a.DownloadVideo(onlineItem("BV1xx", "c123", "测试的 MV"), bili.VideoQualityUnknown)
	if err != nil {
		t.Fatalf("入队：%v", err)
	}
	done := waitDownloadDone(t, a, snap.ID)

	if got := filepath.Ext(done.Dest); got != ".mp4" {
		t.Errorf("落盘的后缀是 %q，要的是 .mp4（两条源流都是 .m4s，"+
			"而落盘的既不是任何一条流，也不是改个后缀就行的东西）", got)
	}
	out := decodeMP4(t, done.Dest)
	if len(out.Moov.Traks) != 2 {
		t.Fatalf("产物里有 %d 条轨，要的是 2", len(out.Moov.Traks))
	}
	var sawVideo, sawAudio bool
	for _, trak := range out.Moov.Traks {
		stsd := trak.Mdia.Minf.Stbl.Stsd
		switch trak.Mdia.Hdlr.HandlerType {
		case "vide":
			sawVideo = stsd.AvcX != nil && stsd.AvcX.AvcC != nil
		case "soun":
			sawAudio = stsd.Mp4a != nil && stsd.Mp4a.Esds != nil
		}
	}
	if !sawVideo || !sawAudio {
		t.Errorf("产物里视频轨带 avcC=%v、音频轨带 esds=%v，两样都要有", sawVideo, sawAudio)
	}
	// 每条轨最多一个 `edts`。多一个的话 macOS 的 AVFoundation 会把
	// **整条轨**丢掉（表现为「文件能打开、只有声音」，见 mux 那边的
	// `TestATrackCarriesExactlyOneEditList`）。
	for _, trak := range out.Moov.Traks {
		n := 0
		for _, c := range trak.Children {
			if _, ok := c.(*mp4.EdtsBox); ok {
				n++
			}
		}
		if n > 1 {
			t.Errorf("%s 轨里有 %d 个 edts，macOS 会因此把整条轨丢掉",
				trak.Mdia.Hdlr.HandlerType, n)
		}
	}
	if trakByHandler(t, out, "soun").Edts == nil {
		t.Error("音频轨没有编辑列表——素材本来是让它晚半秒起的，" +
			"这一条断言是为了确认这一支真的走到了")
	}

	// 两条源流不再留在下载目录里：它们是 fMP4，用户点开放不出来，
	// 留着就是凭空多占两倍盘（见 `download.runMux`）。
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读下载目录：%v", err)
	}
	if len(entries) != 1 {
		t.Errorf("下载目录里有 %d 个东西，要的只有成片那一个", len(entries))
	}
}

// TestDownloadingAudioAndVideoAreTheSameQueueEntry 钉住「同一首只有一个任务」。
//
// `Task.ID` 是 `<bvid>|<cid>`，与「要音频还是要视频」无关。所以同一首先
// 点「下载音频」再点「下载视频」，第二次拿到的是**同一项**。
//
// 这一条是**已知的行为**，不是理想状态：要让两条各自成一项，得把「要哪
// 一路」放进键里，而那会破坏「同一首歌重复点两次下载，用户要的是就一个」
// 那条更重要的性质（见 `download.Task.ID`）。解开它要一个显式的
// 「换成视频重下」之类的动作，是记录页那一票的事。
func TestDownloadingAudioAndVideoAreTheSameQueueEntry(t *testing.T) {
	d := newDashPairFixture(t)
	a := d.app(t, t.TempDir())
	item := onlineItem("BV1xx", "c123", "同一首")

	first, err := a.Download(item)
	if err != nil {
		t.Fatalf("入队（音频）：%v", err)
	}
	second, err := a.DownloadVideo(item, bili.VideoQualityUnknown)
	if err != nil {
		t.Fatalf("入队（视频）：%v", err)
	}
	if first.ID != second.ID {
		t.Errorf("两次入队拿到了两个任务：%q 和 %q", first.ID, second.ID)
	}
	if got := len(a.DownloadSnapshots()); got != 1 {
		t.Errorf("队列里有 %d 项，要的是 1 项", got)
	}
}

// TestTheVideoDownloadAsksForNoQualityByDefault 钉住「默认不带 `qn`」。
//
// 取流接口**不带** `qn` 时 B 站把所有档都返回来，由客户端挑；带了 `qn`
// 就是让服务端只给那一档。默认那个（0）必须是「不带」——而点名某一档时
// `qn` 要真的出现在请求上。
//
// ⚠️ 两条都断。只断「默认不带」的话，点名那一档忘了传 `qn` 测试照样绿
// （请求照样发出去、照样下到东西，只是画质不是用户要的那一档）。
func TestTheVideoDownloadAsksForNoQualityByDefault(t *testing.T) {
	for _, tc := range []struct {
		name string
		qn   bili.VideoQuality
		want string
	}{
		{"默认不带 qn", bili.VideoQualityUnknown, ""},
		{"点名的档要出现在请求里", bili.VideoQuality720, "64"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDashPairFixture(t)
			// 记下取流请求里的 qn。挂在 playURL 那条路上，所以它一定
			// 是在**取流真的发生**的时候被记下来的（不会读到还没发的）。
			var seen sync.Map
			d.f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
				seen.Store(r.URL.Query().Get("qn"), true)
				d.f.servePlayURL(w)
			})

			a := d.app(t, t.TempDir())
			snap, err := a.DownloadVideo(onlineItem("BV1xx", "c123", "MV"), tc.qn)
			if err != nil {
				t.Fatalf("入队：%v", err)
			}
			waitDownloadSettled(t, a, snap.ID)

			if _, ok := seen.Load(tc.want); !ok {
				t.Errorf("取流请求里的 qn 里没有 %q（实际记到的是 %v）",
					tc.want, seenKeys(&seen))
			}
			if tc.want == "" && len(seenKeys(&seen)) != 1 {
				t.Errorf("默认那次只该有一次取流，实际记到 %v", seenKeys(&seen))
			}
		})
	}
}

func seenKeys(m *sync.Map) []string {
	var out []string
	m.Range(func(k, _ any) bool {
		out = append(out, fmt.Sprint(k))
		return true
	})
	return out
}

// ---- 这一组共用的小工具 ----

// rangeHandler 把一段字节变成一台认 Range 的假 CDN。
func rangeHandler(data []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start, end := int64(0), int64(len(data)-1)
		if h := r.Header.Get("Range"); strings.HasPrefix(h, "bytes=") {
			a, b, _ := strings.Cut(strings.TrimPrefix(h, "bytes="), "-")
			start, _ = strconv.ParseInt(a, 10, 64)
			if b != "" {
				end, _ = strconv.ParseInt(b, 10, 64)
			}
		}
		if start > int64(len(data)) {
			http.Error(w, "越界", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if end > int64(len(data))-1 {
			end = int64(len(data)) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start : end+1])
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s：%v", path, err)
	}
	return b
}

// trakByHandler 按 `hdlr` 找那条轨。
func trakByHandler(t *testing.T, f *mp4.File, handler string) *mp4.TrakBox {
	t.Helper()
	for _, trak := range f.Moov.Traks {
		if trak.Mdia != nil && trak.Mdia.Hdlr != nil && trak.Mdia.Hdlr.HandlerType == handler {
			return trak
		}
	}
	t.Fatalf("产物里没有 %s 轨", handler)
	return nil
}

// decodeMP4 把产物解回来。
func decodeMP4(t *testing.T, path string) *mp4.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开 %s：%v", path, err)
	}
	defer f.Close()
	out, err := mp4.DecodeFile(f)
	if err != nil {
		t.Fatalf("产物解不出来：%v", err)
	}
	return out
}

// waitDownloadSettled 等一项不再「在飞」。
//
// ⚠️ 判的是 `Running()` 而不是「等于 running」：视频那两条流下完之后
// 还要交织十几秒，那一段的状态是 `muxing`——它同样是在飞，而它下完之后
// 才是 Done。
func waitDownloadSettled(t *testing.T, a *app.App, id string) download.Snapshot {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		s, ok := a.DownloadSnapshot(id)
		if ok && !s.Status.Running() && s.Status != download.StatusQueued {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	s, _ := a.DownloadSnapshot(id)
	t.Fatalf("等不到任务停下来，现在 %q（%d/%d 字节，%s）", s.Status, s.Done, s.Total, s.Err)
	return s
}

// waitDownloadDone 等一项下完。**出错就当场失败**——这一组要验的是
// 「能下出来」，出错等于没验到。
func waitDownloadDone(t *testing.T, a *app.App, id string) download.Snapshot {
	t.Helper()
	s := waitDownloadSettled(t, a, id)
	if s.Status != download.StatusDone {
		t.Fatalf("下载没成功：状态 %q，%s", s.Status, s.Err)
	}
	return s
}
