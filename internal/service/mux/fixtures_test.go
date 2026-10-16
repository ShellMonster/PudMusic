// 造输入：两条货真价实的 fMP4（`.m4s`）。
//
// 用 mp4ff 自己写出来，而不是手搓字节——这样测的确实是「库写出来的
// 分片流能不能被读懂」，而读代码那一侧和写代码那一侧是**两个不同的
// 实现**（一个走 mp4ff 的 Encode，一个走 `source.go` 自己扫 box）。
//
// # 造出来的形状照着 B 站的来
//
//	视频：时基 90000、30 fps、一个 GOP 四个采样（解码顺序 I P B B，
//	      呈现顺序 I B B P → **中间两帧的 cts 是负的**，正好压到 ctts
//	      的 version 1 那条路上）、关键帧的字节数比别的帧大三倍
//	音频：时基 48000、一帧 1024（44.1 kHz 那档的时长是 1024）
//	分片：每 N 个采样一个 `moof`+`mdat`，`tfhd` 带 default-base-is-moof
package mux

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
	videoTimescale = uint32(90000)
	audioTimescale = uint32(48000)
	videoDur       = uint32(3000) // 30 fps
	audioDur       = uint32(1024) // AAC 一帧
	gopLen         = 4            // 每 4 个采样一个关键帧（I P B B）
	perFragment    = 30           // 一个分片几个采样
)

// dashSpec 说一条要造出来的流长什么样。
type dashSpec struct {
	video     bool
	ts        uint32
	dur       uint32
	nrSamples int
}

// videoSpec 造一条视频流：nrSamples 个采样，30 fps。
func videoSpec(nrSamples int) dashSpec {
	return dashSpec{video: true, ts: videoTimescale, dur: videoDur, nrSamples: nrSamples}
}

// audioSpec 造一条音频流：nrSamples 帧 AAC。
//
// 每秒 48000/1024 = 46.875 帧，所以帧数由**时长**给，不好看反。
func audioSpec(seconds float64) dashSpec {
	return dashSpec{video: false, ts: audioTimescale, dur: audioDur, nrSamples: int(seconds * float64(audioTimescale) / float64(audioDur))}
}

// ctsOf 是第 i 个采样的呈现偏移。
//
// 视频用的是「I P B B」解码顺序配两个采样长的重排延迟（真实 AVC 就是这样），
// 于是中间那两个 B 帧的偏移是**负的**。音频没有重排，恒为 0。
//
// 这一列必须是一个**真的置换**：呈现顺序 I B B P 摊回到解码顺序
// I P B B 就是 dts 0/1/2/3 对应 PTS 0/3/1/2，也就是 cts 0/+2d/-d/-d。
// 排好序之后必须是 0,1,2,3,…，一个不多一个不少——测试断的就是这个。
func ctsOf(sp dashSpec, i int) int32 {
	if !sp.video {
		return 0
	}
	switch i % gopLen {
	case 0: // I 帧：呈现时间就是解码时间
		return 0
	case 1: // P 帧：要等到两个采样之后才呈现
		return int32(2 * sp.dur)
	default: // 两个 B 帧：参照的是「还没到」的那一帧
		return -int32(sp.dur)
	}
}

// syncOf 报第 i 个采样是不是关键帧。
func syncOf(sp dashSpec, i int) bool {
	return sp.video && i%gopLen == 0
}

// sizeOf 是第 i 个采样有多少字节。
//
// 关键帧给 3000 字节、别的帧 500 字节，所以 `stsz` 用不上「每帧一样大」
// 那条省事的写法，测试才真的验到了「逐个采样记大小」这条���路。
// 音频恒定 400 字节（走 uniform 那条）。
func sizeOf(sp dashSpec, i int) uint32 {
	if !sp.video {
		return 400
	}
	if syncOf(sp, i) {
		return 3000
	}
	return 500
}

// payloadOf 是第 i 个采样的字节。内容随 i 变，所以「搬错了某一个采样」
// 在测试里是看得见的，而不是「长度对得上就算了」。
func payloadOf(sp dashSpec, i int) []byte {
	n := sizeOf(sp, i)
	b := make([]byte, n)
	for k := range b {
		b[k] = byte(i*7 + k)
	}
	return b
}

// writeDash 把一条流写成 fMP4。
func writeDash(t *testing.T, path string, sp dashSpec) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("建 %s：%v", path, err)
	}
	defer f.Close()

	init := mp4.CreateEmptyInit()
	trak := init.AddEmptyTrack(sp.ts, mediaType(sp), "und")
	addSampleEntry(trak, sp)
	if err := init.Encode(f); err != nil {
		t.Fatalf("写初始化段：%v", err)
	}

	var dts uint64
	for seq, start := 1, 0; start < sp.nrSamples; seq, start = seq+1, start+perFragment {
		frag, err := mp4.CreateFragment(uint32(seq), trak.Tkhd.TrackID)
		if err != nil {
			t.Fatalf("造第 %d 个分片：%v", seq, err)
		}
		end := start + perFragment
		if end > sp.nrSamples {
			end = sp.nrSamples
		}
		for i := start; i < end; i++ {
			flags := uint32(mp4.NonSyncSampleFlags)
			if syncOf(sp, i) {
				flags = mp4.SyncSampleFlags
			}
			frag.AddFullSample(mp4.FullSample{
				Sample: mp4.Sample{
					Flags:                 flags,
					Dur:                   sp.dur,
					Size:                  sizeOf(sp, i),
					CompositionTimeOffset: ctsOf(sp, i),
				},
				DecodeTime: dts,
				Data:       payloadOf(sp, i),
			})
			dts += uint64(sp.dur)
		}
		if err := frag.Encode(f); err != nil {
			t.Fatalf("写第 %d 个分片：%v", seq, err)
		}
	}
}

// mediaType 是 `hdlr` 里的那四个字母。
func mediaType(sp dashSpec) string {
	if sp.video {
		return "vide"
	}
	return "soun"
}

// addSampleEntry 给一条轨的 `stsd` 装上采样描述符。
//
// **这两样（`avcC` / `esds`）就是交织要原样搬走的东西**，所以测试里
// 会再验一遍它们还在输出里、内容没变。
func addSampleEntry(trak *mp4.TrakBox, sp dashSpec) {
	stsd := trak.Mdia.Minf.Stbl.Stsd
	if sp.video {
		// 一份**真**的 SPS/PPS（High profile、1280x720）。不能用瞎编的
		// 字节：`CreateAvcC` 会去解 SPS，解不出来就报 EOF——于是造不出
		// 输入，交织那一侧就没人测了。NAL 单元的内容对交织无所谓，
		// **有没有它**才要紧（`checkCodec` 拦的就是「有 avc1 却没有 avcC」）。
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

// writePair 在临时目录里造一对流，返回两条路径和两条 spec。
func writePair(t *testing.T, spVideo, spAudio dashSpec) (videoPath, audioPath string) {
	t.Helper()
	dir := t.TempDir()
	videoPath = filepath.Join(dir, "video.m4s")
	audioPath = filepath.Join(dir, "audio.m4s")
	writeDash(t, videoPath, spVideo)
	writeDash(t, audioPath, spAudio)
	return videoPath, audioPath
}
