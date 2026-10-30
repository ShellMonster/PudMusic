package mfadec

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// 这一组断的是**素材本身**，不是解码器。
//
// 为什么要有：Windows 上那次 spike 要拿解出来的 PCM 和一份参考值逐样本比。
// 参考值如果是「跑一次算一下」，那比的就不是解码器而是噪声——素材被换掉、
// 编码参数被改过，两边一起变，结论还是绿的。所以参考值必须**钉在仓库里**，
// 素材一变就红。

// 素材的来历（`ffmpeg -version` 是本机 7.x，自带的原生 aac 编码器）：
//
//	ffmpeg -i flacdec/testdata/stereo16.wav -c:a aac -b:a 128k -ar 44100 -ac 2 \
//	       -profile:a aac_low -f mp4  aaclc-stereo.m4a
//	ffmpeg -i flacdec/testdata/stereo16.wav -c:a aac -b:a 128k -ar 44100 -ac 2 \
//	       -profile:a aac_low -f adts aaclc-stereo.aac
//	ffmpeg -i aaclc-stereo.m4a -c:a pcm_s16le -f wav aaclc-stereo.ref.wav
//
// 第三条是**另一个解码器**（ffmpeg 的）解出来的 PCM，它当参考。选它是因为
// 同一个解码器自己解自己只会证明「它和解码器一致」。
//
// ⚠️ `-profile:a aac_low` 是 **AAC-LC**：B 站绝大多数用户下的那一档。
// 素材里**没有** HE-AAC，而且这台机器上的 ffmpeg 原生编码器**造不出来**
// （`ffmpeg -h encoder=aac` 只有 aac_coder / aac_ms / aac_is / aac_pns /
// aac_tns / aac_pce 六个选项，没有 profile；要 HE-AAC 得 libfdk_aac）。
// 所以「Windows 上解不解得了 HE-AAC」这一条**没有素材可验**——
// 那是 spike 报告里明写的一个未验项，不是「验过没问题」。

// 参考 WAV 的实测值（2026-10-18，macOS + ffmpeg）。
//
// Windows runner 的 MFT 原始输出按完整 access unit 计为 178176 帧；产品层
// 对 MP4 再按编辑列表裁成 176400 帧，对裸 ADTS 保留 178176 帧。
const (
	refRate   = 44100
	refChans  = 2
	refFrames = 177152
	refRMS    = 0.262226
	refPeak   = 0.645752
)

// TestTheCommittedFixturesSayWhatTheyAre 断两个素材的头。
func TestTheCommittedFixturesSayWhatTheyAre(t *testing.T) {
	adts := readFile(t, "aaclc-stereo.aac")
	info, err := ParseADTS(adts)
	if err != nil {
		t.Fatalf("裸 ADTS 素材的头解不出来：%v", err)
	}
	t.Logf("aaclc-stereo.aac: 头里写的 profile=%v rate=%d ch=%d 第一帧 %d 字节 crc=%v",
		info.Profile, info.SampleRate, info.Channels, info.FrameLen, info.HasCRC)

	if info.SampleRate != refRate {
		t.Errorf("素材采样率 %d，want %d", info.SampleRate, refRate)
	}
	if info.Channels != refChans {
		t.Errorf("素材声道数 %d，want %d", info.Channels, refChans)
	}
	if info.FrameLen < 7 || info.FrameLen > len(adts) {
		t.Errorf("第一帧长 %d 字节，文件一共 %d 字节", info.FrameLen, len(adts))
	}

	// .m4a 是 MP4 容器，头两字节没有 ADTS 同步字——这不是错误，
	// 是「这一份要走 MP4 那条路」。写出来是因为「素材认错了容器」
	// 表现和「解码器解不了」一模一样，排查时要先排除它。
	m4a := readFile(t, "aaclc-stereo.m4a")
	if _, err := ParseADTS(m4a); err != ErrNotADTS {
		t.Errorf("把 .m4a 当裸 ADTS 解析得到 %v，应该报 ErrNotADTS", err)
	}
	if string(m4a[4:8]) != "ftyp" {
		t.Errorf("aaclc-stereo.m4a 的第 5~8 字节是 %q，不是 ftyp", m4a[4:8])
	}
}

// TestTheADTSProfileFieldIsTheObjectTypeMinusOne 钉住 ADTS 头里 profile 那两位的
// 含义。
//
// ⚠️⚠️ **这一条原来是反的，而它自己还是绿的。**
//
// 原来这里断言「这份素材的 ADTS 头写的是 Main，可 ffprobe 说它是 LC」，并把结论
// 写成「ffmpeg 的 ADTS muxer 没把 profile 落到那两个 bit 上，所以不能信头里的
// profile」。
//
// **那个结论是错的。** ISO/IEC 13818-7 的 `adts_fixed_header()` 里那个
// `profile` 字段存的是「**MPEG-4 Audio Object Type 减一**」，ffmpeg 的
// `adts_header()` 也是这么解的（`object_type = get_bits(gb, 2) + 1`）。
//
// 所以素材的头是 `ff f1 50 80 …`：byte 2 的头两位是 `01` → object type
// **2** → **AAC-LC**——和 ffprobe 说的**完全一致**。是**这一层的解析少加了一**，
// 才把 AAC-LC 报成 Main，而那个「ffmpeg 没写对」的结论是照着错答案编出来的。
//
// 素材是 `ffmpeg -profile:a aac_low -f adts` 造出来的，ffprobe 说它的 profile
// 是 LC（真的就是 AAC-LC，B 站下发给普通用户的那一档）。
//
// 这条测试现在的作用是：**「头里的 profile」和「ffprobe 说的 profile」必须
// 一致**——不一致的话就是解析又错位了。
func TestTheADTSProfileFieldIsTheObjectTypeMinusOne(t *testing.T) {
	info, err := ParseADTS(readFile(t, "aaclc-stereo.aac"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Profile != ProfileLC {
		t.Errorf("这份素材的 ADTS 头解出 %v，want %v（ffprobe 说它是 LC）",
			info.Profile, ProfileLC)
	}
	t.Logf("ADTS 头里那两位是 object type 减一：解出来是 %v，和 ffprobe 的 profile=LC 一致", info.Profile)
}

// TestTheCommittedReferenceIsWhatItClaims 断参考 PCM 的那几个数。
//
// 这些数就是 Windows spike 拿去比的东西。它们在这儿被钉住，是为了让
// 「Windows 上解出来的和 macOS 上解出来的不一样」这句话有个**可复核的**
// 前提——前提本身也可能是错的。
func TestTheCommittedReferenceIsWhatItClaims(t *testing.T) {
	rate, chans, pcm := readReferenceWAV(t)

	if rate != refRate || chans != refChans {
		t.Errorf("参考 WAV 是 %d Hz / %d 声道，want %d / %d", rate, chans, refRate, refChans)
	}
	if got := len(pcm) / chans; got != refFrames {
		t.Errorf("参考 WAV 有 %d 帧，want %d", got, refFrames)
	}
	rms, peak := stats(pcm)
	t.Logf("参考 WAV: %d 帧 rms=%.6f peak=%.6f", len(pcm)/chans, rms, peak)
	if math.Abs(rms-refRMS) > 5e-4 {
		t.Errorf("参考 WAV 的 RMS = %.6f，want %.6f", rms, refRMS)
	}
	if math.Abs(peak-refPeak) > 5e-4 {
		t.Errorf("参考 WAV 的峰值 = %.6f，want %.6f", peak, refPeak)
	}
}

// stats 算一堆样本的 RMS 和峰值。
//
// 和 `extaf` 测试里那个同名函数是同一种口径（那边拿它和
// `verify-streaming.md` §3.1 的黄金值比），所以两边的数字可以直接对话。
func stats(pcm []float32) (rms, peak float64) {
	var sum float64
	for _, v := range pcm {
		sum += float64(v) * float64(v)
		if a := math.Abs(float64(v)); a > peak {
			peak = a
		}
	}
	if len(pcm) == 0 {
		return 0, 0
	}
	return math.Sqrt(sum / float64(len(pcm))), peak
}

// readFile 读 testdata 里的一个文件。
func readFile(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("读不到素材 %s：%v", name, err)
	}
	return b
}

// readReferenceWAV 把 16 bit PCM 的 RIFF/WAVE 读成 [-1,1] 的 float32 交织样本，
// 顺便报出采样率和声道数。
//
// ⚠️ 这里**只认 PCM 整数 16 位**（wFormatTag == 1）。素材就是 16 位的，
// 而 WAV 的格式种类多得数不清（float、 extensible、 μ-law……），
// 这里支持不全就会变成「素材坏了」的误判——那比解不了音频更让人白忙一场。
func readReferenceWAV(t *testing.T) (rate, chans int, pcm []float32) {
	t.Helper()
	b := readFile(t, "aaclc-stereo.ref.wav")
	if len(b) < 44 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		t.Fatal("参考文件不是 RIFF/WAVE")
	}

	var (
		format, bits uint16
		data         []byte
	)
	for i := 12; i+8 <= len(b); {
		id := string(b[i : i+4])
		size := int(binary.LittleEndian.Uint32(b[i+4 : i+8]))
		body := b[i+8 : i+8+size]
		switch id {
		case "fmt ":
			if len(body) < 16 {
				t.Fatalf("fmt 块只有 %d 字节", len(body))
			}
			format = binary.LittleEndian.Uint16(body[0:2])
			chans = int(binary.LittleEndian.Uint16(body[2:4]))
			rate = int(binary.LittleEndian.Uint32(body[4:8]))
			bits = binary.LittleEndian.Uint16(body[14:16])
		case "data":
			data = body
		}
		i += 8 + size
		if size%2 == 1 { // 块按偶数字节对齐
			i++
		}
	}
	if format != 1 {
		t.Fatalf("参考 WAV 的 wFormatTag 是 %d，只认 1（16 bit 整数 PCM）", format)
	}
	if bits != 16 {
		t.Fatalf("参考 WAV 是 %d 位，只认 16 位", bits)
	}
	if data == nil || chans == 0 {
		t.Fatal("参考 WAV 里没有 data 块")
	}

	pcm = make([]float32, len(data)/2)
	for i := range pcm {
		// 除 32768 而不是 32767：满量程 −32768 映射到 −1.0，
		// 而不是 −0.99997——两个解码器的输出在这里差 1 LSB 时，
		// 用 32768 分母差值是干净的 2/32768，用 32767 会差出 3e-5 的系统偏差。
		pcm[i] = float32(int16(binary.LittleEndian.Uint16(data[i*2:]))) / 32768.0
	}
	return rate, chans, pcm
}

// TestTheReferenceHasNoSilence 断素材不是静音。
//
// 一份被剪坏、或者被换成了空轨的参考，RMS 会是 0 而测试照样绿——
// 「两边都是 0」的比较什么也没证明。这条把它钉住。
func TestTheReferenceHasNoSilence(t *testing.T) {
	_, _, pcm := readReferenceWAV(t)
	rms, peak := stats(pcm)
	if rms < 0.05 || peak < 0.3 {
		t.Fatalf("参考 WAV 几乎是静音：rms=%.6f peak=%.6f", rms, peak)
	}
}

// maxAbsDiff 是两条 PCM 之间的最大逐样本差。
//
// ⚠️ 长度不等时**只比公共的那一段**，并且由调用方自己判断这件事可不可以接受。
// 理由见文件头：两个解码器对编码器 priming 的剪法本来就不一样，帧数对不上
// 是已知且已记录的差，不是 bug。
func maxAbsDiff(a, b []float32) float64 {
	n := min(len(a), len(b))
	var worst float64
	for i := range n {
		if d := math.Abs(float64(a[i] - b[i])); d > worst {
			worst = d
		}
	}
	return worst
}

// TestMaxAbsDiffIsMeasuringSomething 断上面那个工具本身。
//
// 它会被用来判「MF 解的和参考差多少」，判据本身写错了（差值算反、或者漏了
// 某一路）的话，上面所有用它的断言都会**绿着说谎**。
func TestMaxAbsDiffIsMeasuringSomething(t *testing.T) {
	if got := maxAbsDiff([]float32{0, 1, -1}, []float32{0, 1, -1}); got != 0 {
		t.Errorf("两条一样的 PCM，最大差应该是 0，得到 %v", got)
	}
	if got := maxAbsDiff([]float32{0, 0.5, 0}, []float32{0, 0, 0}); got != 0.5 {
		t.Errorf("最大差 = %v，want 0.5", got)
	}
	// 长度不等时只比公共段，不越界 panic。
	if got := maxAbsDiff([]float32{1, 1}, []float32{1}); got != 0 {
		t.Errorf("长度不等时最大差 = %v，want 0", got)
	}
}
