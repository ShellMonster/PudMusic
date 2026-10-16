// 造输入：两条货真价实的 fMP4（`.m4s`）。
//
// 交织的测试（`mux` 包自己的，和 `download` 包里「下载一个视频」的
// 端到端那一条）都要用同一个造法，所以它在这个包里而不是某一份
// `_test.go` 里——**同一段造数据的代码抄两份，迟早会抄歪**，而抄歪了
// 表现是「测试绿着但没测到东西」。
//
// # 造出来的形状照着 B 站的来
//
//	视频：时基 90000、30 fps、一个 GOP 四个采样（解码顺序 I P B B、
//	      呈现顺序 I B B P → **中间两帧的 cts 是负的**，正好压到 ctts
//	      的 version 1 那条路上）、关键帧的字节数比别的帧大三倍
//	音频：时基 48000、一帧 1024
//	分片：每 30 个采样一个 `moof`+`mdat`，`tfhd` 带 default-base-is-moof
//
// 用 mp4ff 自己的 Encode 写出来，于是读的那一侧（`source.go` 自己扫
// box）和写的那一侧是**两个不同的实现**——两边同时错才会骗过测试。

// Package muxtest 造 B 站 dash 流的测试素材。
package muxtest

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"
)

// 时基和帧长。照 B 站 dash 的实际值。
const (
	// VideoTimescale 是视频轨的时基。
	VideoTimescale = uint32(90000)
	// AudioTimescale 是音频轨的时基。
	AudioTimescale = uint32(48000)
	// VideoDur 是视频一帧的时长（30 fps）。
	VideoDur = uint32(3000)
	// AudioDur 是一帧 AAC 的时长（1024 个采样）。
	AudioDur = uint32(1024)
	// GopLen 是多少个采样一个关键帧。
	GopLen = 4
	// PerFragment 是一个分片几个采样。
	PerFragment = 30
)

// Spec 是一条要造出来的流长什么样。
type Spec struct {
	video     bool
	timescale uint32
	dur       uint32
	samples   int
	// keyframeBytes >0 时关键帧给这么多字节（默认 3000）。
	//
	// 存在的理由：一个真视频的关键帧是几百 KB，而默认那 3000 字节**永远
	// 走不到**搬运代码里「采样比中转缓冲区大」那条分支——于是那条分支
	// 没人测，而它错起来是一个切片越界 panic。
	keyframeBytes uint32
}

// VideoSpec 造一条视频流：`samples` 个采样，30 fps。
func VideoSpec(samples int) Spec {
	return VideoSpecWithKeyframe(samples, 3000)
}

// VideoSpecWithKeyframe 是 `VideoSpec` 加一个「关键帧多大」。
func VideoSpecWithKeyframe(samples int, keyframeBytes uint32) Spec {
	return Spec{video: true, timescale: VideoTimescale, dur: VideoDur,
		samples: samples, keyframeBytes: keyframeBytes}
}

// AudioSpec 造一条音频流：`seconds` 秒的 AAC。
//
// AAC 一帧是 1024 个采样（44.1 kHz 那一档的时长），所以帧数由**时长**
// 给，不好看反。
func AudioSpec(seconds float64) Spec {
	return Spec{timescale: AudioTimescale, dur: AudioDur,
		samples: int(seconds * float64(AudioTimescale) / float64(AudioDur))}
}

// Video 报这是不是视频流。
func (s Spec) Video() bool { return s.video }

// Timescale 是这条流的时基。
func (s Spec) Timescale() uint32 { return s.timescale }

// Dur 是每个采样的时长（媒体时基）。
func (s Spec) Dur() uint32 { return s.dur }

// Samples 是采样个数。
func (s Spec) Samples() int { return s.samples }

// Seconds 是这条流有多长（秒）。
func (s Spec) Seconds() float64 {
	return float64(s.samples) * float64(s.dur) / float64(s.timescale)
}

// CtsOf 是第 i 个采样的呈现偏移。
//
// 视频用的是「I P B B」解码顺序配两个采样长的重排延迟（真实 AVC 就是
// 这样），于是中间那两个 B 帧的偏移是**负的**。音频没有重排，恒为 0。
//
// 这一列必须是一个**真的置换**：呈现顺序 I B B P 摊回到解码顺序
// I P B B 就是 dts 0/1/2/3 对应 PTS 0/3/1/2，也就是 cts 0/+2d/-d/-d。
// 排好序之后必须是 0, 1, 2, 3, …，一个不多一个不少。
func (s Spec) CtsOf(i int) int32 {
	if !s.video {
		return 0
	}
	switch i % GopLen {
	case 0: // I 帧：呈现时间就是解码时间
		return 0
	case 1: // P 帧：要等到两个采样之后才呈现
		return int32(2 * s.dur)
	default: // 两个 B 帧：参照的是「还没到」的那一帧
		return -int32(s.dur)
	}
}

// SyncOf 报第 i 个采样是不是关键帧。
func (s Spec) SyncOf(i int) bool { return s.video && i%GopLen == 0 }

// SizeOf 是第 i 个采样有多少字节。
//
// 关键帧给 3000 字节、别的帧 500 字节，所以 `stsz` 用不上「每帧一样大」
// 那条省事的写法，测试才真的验到了「逐个采样记大小」这一路。
// 音频恒定 400 字节（走 uniform 那条）。
func (s Spec) SizeOf(i int) uint32 {
	if !s.video {
		return 400
	}
	if s.SyncOf(i) {
		return s.keyframeBytes
	}
	return 500
}

// PayloadOf 是第 i 个采样的字节。内容随 i 变，所以「搬错了某一个采样」
// 在测试里是看得见的，而不是「长度对得上就算了」。
func (s Spec) PayloadOf(i int) []byte {
	n := s.SizeOf(i)
	b := make([]byte, n)
	for k := range b {
		b[k] = byte(i*7 + k)
	}
	return b
}

// Write 把一条流写成 fMP4。
func Write(tb testing.TB, path string, s Spec) {
	tb.Helper()
	f, err := os.Create(path)
	if err != nil {
		tb.Fatalf("建 %s：%v", path, err)
	}
	defer f.Close()

	init := mp4.CreateEmptyInit()
	trak := init.AddEmptyTrack(s.timescale, mediaType(s), "und")
	addSampleEntry(trak, s)
	if err := init.Encode(f); err != nil {
		tb.Fatalf("写初始化段：%v", err)
	}

	var dts uint64
	for seq, start := 1, 0; start < s.samples; seq, start = seq+1, start+PerFragment {
		frag, err := mp4.CreateFragment(uint32(seq), trak.Tkhd.TrackID)
		if err != nil {
			tb.Fatalf("造第 %d 个分片：%v", seq, err)
		}
		end := start + PerFragment
		if end > s.samples {
			end = s.samples
		}
		for i := start; i < end; i++ {
			flags := uint32(mp4.NonSyncSampleFlags)
			if s.SyncOf(i) {
				flags = mp4.SyncSampleFlags
			}
			frag.AddFullSample(mp4.FullSample{
				Sample: mp4.Sample{
					Flags:                 flags,
					Dur:                   s.dur,
					Size:                  s.SizeOf(i),
					CompositionTimeOffset: s.CtsOf(i),
				},
				DecodeTime: dts,
				Data:       s.PayloadOf(i),
			})
			dts += uint64(s.dur)
		}
		if err := frag.Encode(f); err != nil {
			tb.Fatalf("写第 %d 个分片：%v", seq, err)
		}
	}
}

// Pair 在一个临时目录里造一对流，返回两条路径。
//
// 目录由 tb 管（`TempDir`），用完自动清掉。
func Pair(tb testing.TB, video, audio Spec) (videoPath, audioPath string) {
	tb.Helper()
	dir := tb.TempDir()
	videoPath = filepath.Join(dir, "video.m4s")
	audioPath = filepath.Join(dir, "audio.m4s")
	Write(tb, videoPath, video)
	Write(tb, audioPath, audio)
	return videoPath, audioPath
}

// PairIn 在指定的目录里造一对流。目录不是测试自己开的（下载那一侧要看
// 它落在哪儿、会不会留半成品）。
func PairIn(tb testing.TB, dir string, video, audio Spec) (videoPath, audioPath string) {
	tb.Helper()
	videoPath = filepath.Join(dir, "video.m4s")
	audioPath = filepath.Join(dir, "audio.m4s")
	Write(tb, videoPath, video)
	Write(tb, audioPath, audio)
	return videoPath, audioPath
}

// mediaType 是 `hdlr` 里的那四个字母。
func mediaType(s Spec) string {
	if s.video {
		return "vide"
	}
	return "soun"
}

// addSampleEntry 给一条轨的 `stsd` 装上采样描述符。
//
// **这两样（`avcC` / `esds`）就是交织要原样搬走的东西**，所以测试里
// 会再验一遍它们还在输出里、内容没变。
func addSampleEntry(trak *mp4.TrakBox, s Spec) {
	stsd := trak.Mdia.Minf.Stbl.Stsd
	if s.video {
		// 一份**真**的 SPS/PPS（High profile、1280x720）。不能用瞎编的
		// 字节：`CreateAvcC` 会去解 SPS，解不出来就报 EOF——于是造不出
		// 输入，交织那一侧就没人测了。NAL 单元的内容对交织无所谓，
		// **有没有它**才要紧。
		sps, err := hex.DecodeString("67640020accac05005bb0169e0000003002000000c9c4c000432380008647c12401cb1c31380")
		if err != nil {
			panic(fmt.Sprintf("解 SPS：%v", err))
		}
		pps, err := hex.DecodeString("68e84332c8b0")
		if err != nil {
			panic(fmt.Sprintf("解 PPS：%v", err))
		}
		avcC, err := mp4.CreateAvcC([][]byte{sps}, [][]byte{pps}, true)
		if err != nil {
			panic(fmt.Sprintf("造 avcC：%v", err))
		}
		stsd.AddChild(mp4.CreateVisualSampleEntryBox("avc1", 1280, 720, avcC))
		return
	}
	// AudioSpecificConfig：AAC-LC、48 kHz、双声道（两字节，真解码器认）。
	asc := []byte{0x11, 0x90}
	stsd.AddChild(mp4.CreateAudioSampleEntryBox("mp4a", 2, 16, 48000, mp4.CreateEsdsBox(asc)))
}
