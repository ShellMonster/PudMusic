// 在线音质覆盖：320K AAC 和无损 FLAC 在线路径都要能播，且解出来的采样
// 跟原始录音**逐个相同**。
//
// # 为什么逐样本，不看 RMS
//
// 「左右声道相同的信号听不出错、用 RMS 也测不出来」——`SPEC.md`
// 「Testing Decisions」里明写了这一条。声道接错（所有左声道接完再接右声道）
// 在 RMS 上完全看不出来，在听感上也只是一点点怪。所以这一组拿
// `stereo16.wav`（FLAC 素材编码**之前**的那个 WAV）当判据，逐个采样比。
//
// # 判据从哪来
//
// WAV 是**原始录音**，FLAC 是无损编码——所以 FLAC 解出来的每一个采样都必须
// 等于 WAV 里那一个。不需要第二个解码器来达成一致。
//
// # 真网上验不到的那部分
//
// 真 B 站的无损流要大会员账号，本机没有；320K 那条真机验过
// （`reports/Biu_MyGo/14`）。这里验的是**解码侧**：字节从 CDN 来，和本地
// 文件走的是同一个 `openDecoder`。取流那一侧（怎么选中 320K / FLAC 那一档）
// 在 `internal/app/online_test.go` 与 `internal/service/bili`。

package audio

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	mygoaudio "github.com/egoist/mygo/audio"
)

// ─────────────────────────────────────────────────────────────────────────────
// fixture：一台按 Range 服务的假 CDN
// ─────────────────────────────────────────────────────────────────────────────

// fixture 读 flacdec 那份素材。用同一份的原因：逐样本比对要有一个判据，
// 而判据必须和素材是同一个录音。
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("flacdec", "testdata", name))
	if err != nil {
		t.Fatalf("读不到素材 %s：%v", name, err)
	}
	return data
}

// newByteCDN 起一台会 Range 的服务器，段内容就是 data。
func newByteCDN(t *testing.T, data []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveBytes(w, r, data, len(data))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newHalfByteCDN 起一台「只发前半截就卡住」的服务器。
//
// 卡住是故意的：它把「起播不必等下载完」从假设变成事实——解码器要是真的
// 需要整段文件才肯开口，这个 handler 就永远等不到测试来开门，第一个采样
// 也就永远出不来。Content-Length 照实写全长（下载确实还没完），所以源那边
// 就是「正在下」的状态，而不是「下到一半断了」。
//
// 返回的门要在测试**结束前**关掉，否则 `httptest.Server.Close` 会等这个
// 还在跑的 handler 收尾，一直等到超时。所以这里注册的最后一次 Cleanup：
// LIFO 让它最先跑。
func newHalfByteCDN(t *testing.T, data []byte) *httptest.Server {
	t.Helper()
	half := len(data) / 2
	gate := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		from := rangeStart(r)
		w.Header().Set("Content-Length", fmt.Sprint(int64(len(data))-from))
		if from > 0 {
			w.Header().Set("Content-Range",
				fmt.Sprintf("bytes %d-%d/%d", from, len(data)-1, len(data)))
			w.WriteHeader(http.StatusPartialContent)
		}
		if from < int64(half) {
			w.Write(data[from:half])
		}
		w.(http.Flusher).Flush()
		<-gate // 下半截永远不来，除非测试收尾时把门开了
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(gate) })
	return srv
}

// newOnlineSource 把 data 挂成一条「边下边播」的源，和生产路径同型。
func newOnlineSource(t *testing.T, srv *httptest.Server) *StreamSource {
	t.Helper()
	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL + "/stream.m4a"},
		Client:  srv.Client(),
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// decodeAll 把解码器剩下的采样全读出来。播放器那边是另一个 goroutine 在读、
// 读到的样本不外露，所以要验采样只能自己开一个解码器。
func decodeAll(t *testing.T, d mygoaudio.Decoder) []float32 {
	t.Helper()
	t.Cleanup(func() {
		if c, ok := d.(io.Closer); ok {
			c.Close()
		}
	})
	ch := d.Channels()
	buf := make([]float32, 4096*ch)
	var out []float32
	for {
		n, err := d.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			if err != io.EOF {
				t.Fatalf("解码中断：%v", err)
			}
			return out
		}
		if n == 0 {
			t.Fatal("解码器没有进展也没有报错")
		}
	}
}

// expect 把 WAV 里的整数采样换算成解码器该交出来的 float32。
//
// 除的是半个量程（1<<(bits-1)），这是 MyGo 自己的 WAV 解码器用的换算，
// 同一段录音的 WAV 和 FLAC 才落在同一个 float32 上。
func expect(v int32, bits int) float32 {
	return float32(v) / float32(int64(1)<<uint(bits-1))
}

// compareSampleForSample 逐个采样比，并说清第一个不一致在哪儿。
//
// ⚠️ 这里**不能**退化成 RMS：声道接错时 RMS 是一样的，两条声道相同时更是
// 连 RMS 都不用算就已经对了。报告里带上左右各是多少——那样一眼能看出
// 是不是「左右接反了」还是「某个采样算错了」。
func compareSampleForSample(t *testing.T, what string, channels int, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s：解出 %d 个采样，原始录音有 %d 个", what, len(got), len(want))
	}
	for i := range want {
		if got[i] == want[i] {
			continue
		}
		frame, side := i/channels, "左"
		if i%channels == 1 {
			side = "右"
		}
		t.Fatalf("%s：第 %d 帧的%s声道解出来是 %v，原始录音是 %v（逐样本比，不是 RMS）",
			what, frame, side, got[i], want[i])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 一套注册表
// ─────────────────────────────────────────────────────────────────────────────

// TestTheStreamAndTheFileGoThroughOneRegistry 盯「本地文件和在线流走同一套
// 格式注册，不是两套代码」。
//
// # 为什么两个入口都用公开的那两个
//
// 写这条的时候它对文件和对流都直接调 `openDecoder`——那样**抓不到**
// `Open` 里重新长出一句按扩展名分流的写法（回归测试：把「.flac 走
// `flacdec.OpenFile`」放回去，这条仍然是绿的）。现在它走 `Open` 与
// `OpenStream`：那是真正在生产里被调到的两个入口。
//
// # 为什么文件名一律叫 .flac
//
// 文件名是唯一能让「第二套认容器的方式」露馅的东西：只要 `Open` 里有一句
// 按后缀分流，一份**内容不是 FLAC**、名字却是 `.flac` 的文件就会在**本地
// 这一侧**被拒掉（或者走另一条路），而在线那侧照样放得出来。两个入口因此
// 给出不一样的结果——那正是「两套代码」。
//
// 逐样本那一半在 `TestLosslessFLACFromTheStreamIsSampleForSample`：
// `Player` 不外露解出来的采样，在这一层量采样就得绕开生产路径。
func TestTheStreamAndTheFileGoThroughOneRegistry(t *testing.T) {
	for _, c := range []struct{ name, file string }{
		{"裸 FLAC", "stereo16.flac"},
		{"MP4 里的 FLAC", "stereo16.mp4"},
		{"扩展名撒谎的（内容是 WAV）", "stereo16.wav"},
	} {
		t.Run(c.name, func(t *testing.T) {
			data := fixture(t, c.file)

			fromFile, err := Open(writeTemp(t, "同一段字节.flac", data))
			if err != nil {
				t.Fatalf("本地文件打不开：%v", err)
			}
			defer fromFile.Close()

			fromStream, err := OpenStream(newOnlineSource(t, newByteCDN(t, data)))
			if err != nil {
				t.Fatalf("同一段字节作为在线流打不开：%v", err)
			}
			defer fromStream.Close()

			if fromFile.Duration() != fromStream.Duration() {
				t.Errorf("本地时长 %v、在线时长 %v——同一段字节不该有两个结果",
					fromFile.Duration(), fromStream.Duration())
			}
			if fromFile.dec.SampleRate() != fromStream.dec.SampleRate() {
				t.Errorf("本地 %d Hz、在线 %d Hz", fromFile.dec.SampleRate(), fromStream.dec.SampleRate())
			}
			if fromFile.dec.Channels() != fromStream.dec.Channels() {
				t.Errorf("本地 %d 声道、在线 %d 声道",
					fromFile.dec.Channels(), fromStream.dec.Channels())
			}
			if fromFile.Duration() <= 0 {
				t.Errorf("时长 = %v，解码器没报出长度", fromFile.Duration())
			}
		})
	}
}

// TestContentDecidesTheContainerNotTheFileName 盯「按头几个字节认容器」。
//
// 一份 WAV 改名成 .flac，扩展名说谎。认头几个字节就照样放得出来（它本来就是
// 一份能放的 WAV），按扩展名认才会在这里放不出来。
//
// 这一条是上面那条的反面：反着写这条测试的时候最容易写成「改名的要被拒绝」，
// 那是**按扩展名认容器**的行为——曾经 `Open` 就是那么做的。
func TestContentDecidesTheContainerNotTheFileName(t *testing.T) {
	w := stereo16(t)
	wavBytes := buildWAV(w.samples, w.channels, w.rate)
	liar := writeTemp(t, "其实是wav.flac", wavBytes)

	if _, err := Open(liar); err != nil {
		t.Errorf("一份 WAV 改名成 .flac 就放不出来了（%v）——认容器认的是头几个字节，不是后缀", err)
	}
	// 同一段字节挂到 CDN 上，在线流也该认得出来。
	if _, err := OpenStream(newOnlineSource(t, newByteCDN(t, wavBytes))); err != nil {
		t.Errorf("同一份字节作为在线流放不出来：%v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 无损 FLAC：逐样本
// ─────────────────────────────────────────────────────────────────────────────

// TestLosslessFLACFromTheStreamIsSampleForSample 盯规格点名的那一条：
// **无损 FLAC 在线可播，解码结果逐样本一致**。
//
// 判据是 stereo16.wav——FLAC 素材编码**之前**的那个录音。无损的意思就是
// 解出来的每一个采样都等于原来那一个，所以这里不需要「参考解码器」，
// WAV 本身就是参考。
//
// 两种容器都要断，因为它们走的**不是同一个解码器**：裸 FLAC 认不出来、
// 落回 flacdec；MP4 里的 FLAC 在 macOS 上认成 MP4、走系统解码器，在**没有
// 系统解码器**的平台上（Windows）落回 flacdec。
//
// ⚠️ **B 站的无损流是哪一种已经实测过了**（2026-10-23，issue #252）：是
// **MP4 里的 FLAC**——头 16 字节是 `ftyp` 盒。素材与形状说明见
// `internal/service/bili/testdata/lossless/`。两种容器都还得是逐位准的，
// 因为本地裸 `.flac` 文件还要走另一条路。
//
// 先断素材的左右声道不同：这两个声道是不同信号，接错了才测得出来。
// 全程逐个采样比，不用 RMS（`SPEC.md`「Testing Decisions」）。
func TestLosslessFLACFromTheStreamIsSampleForSample(t *testing.T) {
	src := stereo16(t)
	for i := 0; i < len(src.samples); i += src.channels {
		if src.samples[i] == src.samples[i+1] {
			t.Fatalf("第 %d 帧的左右声道相同，这份素材测不出声道接错", i/src.channels)
		}
	}
	want := make([]float32, len(src.samples))
	for i, v := range src.samples {
		want[i] = expect(v, src.bits)
	}

	// MP4 里的 FLAC 走哪条路**要看平台**：macOS 上系统解码器（AudioToolbox）
	// 能用，走它（容器名 mp4）；其它平台上没有系统解码器，`openDecoder`
	// 落回 `flacdec`（容器名 flac）。这一条断言的就是这个分派。
	wantMP4 := "mp4"
	if runtime.GOOS != "darwin" {
		wantMP4 = "flac"
	}
	for _, c := range []struct{ file, wantDecoder string }{
		{"stereo16.flac", "flac"},
		{"stereo16.mp4", wantMP4},
	} {
		t.Run(c.file, func(t *testing.T) {
			d, name, err := openDecoder(newOnlineSource(t, newByteCDN(t, fixture(t, c.file))))
			if err != nil {
				t.Fatalf("无损流认不出容器：%v", err)
			}
			if name != c.wantDecoder {
				t.Errorf("容器名 = %q，want %q", name, c.wantDecoder)
			}
			if d.Channels() != src.channels || d.SampleRate() != src.rate {
				t.Errorf("格式 = %d Hz / %d 声道，原始录音是 %d Hz / %d 声道",
					d.SampleRate(), d.Channels(), src.rate, src.channels)
			}
			compareSampleForSample(t, "无损 FLAC 在线流", d.Channels(), decodeAll(t, d), want)
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 320K AAC：首次可听 ≤ 200 ms
// ─────────────────────────────────────────────────────────────────────────────

// firstAudibleBudget 是「下载等待之外的首次可听时间」的上限。
//
// 口径来自 `PLAN.md` 阶段 2：**首帧 < 首次播放的下载等待时间 + 200 ms**，
// 而不是「点击到出声 < 200 ms」这个孤立的绝对值——真网上 B 站的音频
// 60 秒才 1.2 MB，光下载就够这个数了。
const firstAudibleBudget = 200 * time.Millisecond

// TestAAC320FromTheStreamIsAudibleFast 盯「320K AAC 在线可播，首次可听
// 时间不超过 200 毫秒」。
//
// # 为什么 CDN 只发前半截
//
// 因为「播放中」和「等下载完才播放」在界面上长得一样：进度条都在走、都没
// 有红字。而验收标准要断的恰恰是**快**，那就得让「快」和「下载完」分得开。
// 服务器发到一半就卡住（`newHalfByteCDN`），于是：
//
//	解码器要是需要整段文件才肯开口 → 第一个采样永远出不来 → 这条红
//	解码器开了头就出声            → 第一个采样立刻就到
//
// # 量的是解码那一侧
//
// 从开口到交出第一个 PCM 采样，减去这段时间里等字节的全部时间
// （`netGauge` 记的）。统计口径照 `PLAN.md`：不含 MyGo 的输出层延迟，也不含
// 界面首次渲染。`PLAN.md` 的验收标准是「首帧 < 下载等待 + 200 ms」，
// 不是「点击到出声 < 200 ms」那个孤立的绝对值。
//
// # 为什么取三轮里最快的
//
// 和 `flacdec` 的 `TestDecodeSpeed` 同一个理由：`go test ./...` 是并行的，
// 测延迟时旁边可能有人在抢 CPU，「解码器变慢了」和「旁边有人在抢 CPU」
// 分不出来。这条断的是解码器的能力，不是调度。
func TestAAC320FromTheStreamIsAudibleFast(t *testing.T) {
	data := aac320(t)

	best, bestTotal := time.Duration(math.MaxInt64), time.Duration(0)
	for i := range 3 {
		g := &netGauge{r: newOnlineSource(t, newHalfByteCDN(t, data))}
		start := time.Now()
		d, name, err := openDecoder(g)
		if err != nil {
			t.Fatalf("第 %d 轮：320K 流认不出容器：%v", i+1, err)
		}
		if name != "mp4" {
			t.Errorf("容器名 = %q，want mp4（320K 走的是注册表里系统解码器那一条）", name)
		}
		firstSampleWithin(t, d, 30*time.Second)
		if c, ok := d.(io.Closer); ok {
			t.Cleanup(func() { c.Close() })
		}
		total := time.Since(start)
		spent := total - g.blockedTotal()
		if spent < best {
			best, bestTotal = spent, total
		}
	}

	t.Logf("首次可听：下载等待之外的解码时间 %v（含等待共 %v）", best.Round(time.Millisecond),
		bestTotal.Round(time.Millisecond))
	if best > firstAudibleBudget {
		t.Errorf("320K 在线流首次可听（不含下载等待）用了 %v，超过 %v",
			best.Round(time.Millisecond), firstAudibleBudget)
	}
}

// TestAAC320FromTheStreamActuallyDecodes 盯「在线的 320K 解出来是真声音」。
//
// 快是快，出不出声是另一件事：解码器一路顺利地交出 0 个采样也满足「快」。
// 所以这里数采样、看声道和采样率、看波形不是静音——静音的话播放条上照样
// 「正常播放中」，用户什么都听不见。
func TestAAC320FromTheStreamActuallyDecodes(t *testing.T) {
	data := aac320(t)
	d, _, err := openDecoder(newOnlineSource(t, newByteCDN(t, data)))
	if err != nil {
		t.Fatalf("320K 流认不出容器：%v", err)
	}
	if d.Channels() != 2 {
		t.Errorf("声道数 = %d，want 2（B 站的 320K 是立体声）", d.Channels())
	}
	got := decodeAll(t, d)
	if len(got) < d.SampleRate() {
		t.Fatalf("只解出 %d 个采样（采样率 %d），一首 1.5 秒的歌不该这么短", len(got), d.SampleRate())
	}
	if silent(got) {
		t.Error("解出来全是 0：在线流「播放中」但一个声音也没有")
	}
	if lo, hi := span(got); hi-lo < 0.05 {
		t.Errorf("波形上下只差 %v，接近静音（%.4f ~ %.4f）", hi-lo, lo, hi)
	}
}

// firstSampleWithin 一直读到解码器交出**第一个不是零的**采样，返回那一刻
// 的时刻。超过 d 还没等到就报错（不是挂着等 `go test` 自己的超时）。
//
// 为什么不是「第一次 Read 返回 n>0」：开头几个采样完全可能是 0（起播的
// 静音），那时候声音还没出来，量到的不是「首次可听」。
func firstSampleWithin(t *testing.T, d mygoaudio.Decoder, d0 time.Duration) time.Time {
	t.Helper()
	type found struct{ at time.Time }
	done := make(chan found, 1)
	go func() {
		buf := make([]float32, 2048*d.Channels())
		for {
			n, err := d.Read(buf)
			for _, v := range buf[:n] {
				if v != 0 {
					done <- found{time.Now()}
					return
				}
			}
			if err != nil {
				return
			}
			if n == 0 {
				return
			}
		}
	}()
	select {
	case f := <-done:
		return f.at
	case <-time.After(d0):
		t.Fatalf("%v 内没有等到第一个采样", d0)
		return time.Time{}
	}
}

func silent(s []float32) bool { lo, hi := span(s); return hi == 0 && lo == 0 }

func span(s []float32) (lo, hi float32) {
	lo, hi = s[0], s[0]
	for _, v := range s {
		lo, hi = min(lo, v), max(hi, v)
	}
	return lo, hi
}

// netGauge 包在字节源外面，量「等字节」这件事花了多久。
//
// 为什么单独包一层：`Player` 不外露解出来的采样，要量「第一个采样什么时候
// 出来」就只能自己开解码器；而那个解码器等的是源，源等的是网络——不把
// 网络那段单独摘出来，量到的就是「点击到出声」，那正是 `PLAN.md` 明确说
// 不能当成验收指标的那个数。
//
// ⚠️ 要加锁：extaf 的读回调跑在 AudioToolbox 自己的线程上，而 `blocked`
// 是测试 goroutine 读的。没有互斥的话 `-race` 一跑就红，红的还是测试。
type netGauge struct {
	r io.ReadSeeker

	mu     sync.Mutex
	waited time.Duration
}

func (g *netGauge) Read(p []byte) (int, error) {
	start := time.Now()
	n, err := g.r.Read(p)
	g.mu.Lock()
	g.waited += time.Since(start)
	g.mu.Unlock()
	return n, err
}

func (g *netGauge) Seek(off int64, whence int) (int64, error) { return g.r.Seek(off, whence) }

// blockedTotal 是「等字节」的总时间。
func (g *netGauge) blockedTotal() time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.waited
}

// aac320 用系统自带的 afconvert 造一份 320K 立体声 AAC（MP4 容器）素材。
//
// 为什么现造而不是把二进制塞进仓库：B 站的 320K 流就是 AAC-in-MP4，
// 而 `afconvert` 是 macOS 自带的、造出来的东西和 `verify-streaming.md`
// §0 用的是同一条命令（那边就是拿它做的实测）。造不出来就跳过——
// 拿 FLAC 假装验过 AAC 比不验更糟。
func aac320(t *testing.T) []byte {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("AAC 那条路（AudioToolbox）只有 macOS 有")
	}
	if _, err := exec.LookPath("afconvert"); err != nil {
		t.Skip("这台机器上没有 afconvert，造不出 AAC 素材")
	}
	const ping = "/System/Library/Sounds/Ping.aiff"
	if _, err := os.Stat(ping); err != nil {
		t.Skipf("系统里没有 Ping.aiff：%v", err)
	}
	out := filepath.Join(t.TempDir(), "ping320.m4a")
	if o, err := exec.Command("afconvert", "-f", "m4af", "-d", "aac", "-b", "320000", "-c", "2",
		ping, out).CombinedOutput(); err != nil {
		t.Skipf("造不出 320K AAC 素材：%v\n%s", err, o)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	// 认一下容器：MP4 的第一个 box 是 `ftyp`。素材不对的话，后面的断言
	// 测的就不是 320K AAC 了。
	if len(data) < 12 || string(data[4:8]) != "ftyp" {
		t.Fatalf("造出来的素材不像一个 MP4（前 12 字节 % x）", data[:min(len(data), 12)])
	}
	return data
}
