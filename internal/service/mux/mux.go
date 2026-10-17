// 音视频交织：把 B 站 dash 的两条流（视频一条、音频一条）拼成一个能播的
// MP4 文件。
//
// # 这一票不转码、不改编码参数
//
// 采样描述符（视频的 `avcC`、音频的 `esds`）是**从源搬过去的**，一个字节
// 没改。交织改的只有一件事：**把两条各自完整的时间轴放进同一个容器**。
// 所以它快（不碰像素）也省电（下载完了才跑一遍），代价是没法改画质——
// 那本来也不是它该干的。
//
// # 「能播」不证明「对」
//
// 最阴险的失败模式是**时间戳错了但文件照样能播**，只是音画慢慢漂移。
// 要连播三分钟以上才看得出来，而用户多半没有三分钟。所以：
//
//	交织顺序、每帧的 DTS/PTS、两条轨的时长差，各有一组断言测试
//	（`mux_test.go`），断言的是**结果**而不是「有没有报错」
//	真机验的时候两边首帧对不齐，这里会**拒绝写**而不是写出一个慢慢漂的文件
//
// # 写坏了不留在盘上
//
// 输出先写到 `<目标>.mp4.part`，全部写完才改名。写到一半失败（磁盘满、
// 用户删任务）留下的是那个 `.part`，而它跟下载的半成品同名同规矩
// （见 `download` 的 `partPath`）：用户在下载目录里看到的是完整的一首，
// 不会有一首「打不开的 MV」混在里面。

package mux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Eyevinn/mp4ff/mp4"
)

// Result 是交织的结果。给记录页看的——「这一条视频下成了吗」，
// 以及「两条轨差了多少」。
type Result struct {
	// VideoSamples / AudioSamples 是两条轨各有多少个采样。
	VideoSamples int
	AudioSamples int
	// VideoDuration / AudioDuration 是两条轨的呈现时长。
	VideoDuration time.Duration
	AudioDuration time.Duration
	// Drift 是两条轨时长之差的绝对值。
	//
	// 正常的 dash 配对是毫秒级。**它是记下来给人看的**，不是判据——
	// 判据在 `check`，超了会拒绝写。
	Drift time.Duration
	// Bytes 是写出去的文件大小。
	Bytes int64
	// Out 是落盘的最终路径。
	Out string
}

// maxDrift 是两条轨时长差允许多大。
//
// 取两秒。往下的理由：同一个 cid 的 dash 音视频本来就是同一条时间线裁
// 出来的，差值正常在毫秒级（几十毫秒的编解码器 padding）；**差到秒级，
// 要么是取到了别的分 P，要么是其中一条流下截了**——这两种都会交织出一
// 个「能播、但播两分钟就没声了」或者「从头到尾对不上」的文件。
//
// 往上的理由：真的存在那种开头有一小段没有声音的 MV。两秒够容下
// 「片头前两秒不出声」，而再宽就接到「取错流」那一档上去了。
//
// **宁可报错**：交织不出来用户看到一句能看懂的话；交织出一个慢慢没声的
// 文件，用户要过两分钟才发现，而他会以为是播放器的问题。
const maxDrift = 2 * time.Second

// Mux 交织两条流，写到 dst。
//
// **可取消**（ctx）：交织一个几百 MB 的 MV 要读一遍再写一遍，那十几秒里
// 用户要能退出、要能删掉这一项。`Manager.Close` 会等后台那条线退出，
// 而那条线正卡在这里——不认 ctx 的话「退出应用」就变成「等交织完」。
// 每搬一段采样查一次（见 `build`），所以取消的响应是亚秒级的。
//
// videoPath / audioPath 是下好的两条 dash 流（磁盘上的 `.m4s`）。
// **dst 由调用方决定它是什么**：这一层直接写那个路径，写到一半失败会
// 把它删掉（那是一份没人要的垃圾），但**改名那一下不归它管**——
// 给它一个临时路径、由调用方改��最终名，是下载那一层的事（它还要在
// 改名之前再看一眼那个位置上有没有别人的东西，见 `download.destToLand`）。
func Mux(ctx context.Context, videoPath, audioPath, dst string) (*Result, error) {
	video, err := readSource(videoPath)
	if err != nil {
		return nil, err
	}
	audio, err := readSource(audioPath)
	if err != nil {
		return nil, err
	}
	if !video.video || audio.video {
		return nil, fmt.Errorf("mux: 传进来的两条流是「%s」和「%s」，要的是一条视频一条音频",
			kindOf(video), kindOf(audio))
	}
	if err := check(video, audio); err != nil {
		return nil, err
	}

	// 电影时基取两条轨里**细**的那个。换算时基是就近取整，
	// 越细的那个时基错得越少（90000 比 1000 细，取整误差小两个数量级）。
	movieTS := video.timescale
	if audio.timescale > movieTS {
		movieTS = audio.timescale
	}

	// 直接写到 dst（调用方给的是它自己的临时路径）。出错就把半成品删掉：
	// 它是坏的，而用户在下载目录里看到的是完整的一首。
	f, err := os.Create(dst)
	if err != nil {
		return nil, fmt.Errorf("mux: 建 %s：%w", filepath.Base(dst), err)
	}
	written := int64(0)
	tracks, err := build(ctx, &countingWriter{f: f, n: &written}, video, audio, movieTS)
	if cerr := f.Close(); err == nil && cerr != nil {
		err = fmt.Errorf("mux: 关 %s：%w", filepath.Base(dst), cerr)
	}
	if err != nil {
		_ = os.Remove(dst)
		return nil, err
	}

	res := &Result{
		VideoSamples:  len(video.samples),
		AudioSamples:  len(audio.samples),
		VideoDuration: durationOf(tracks[0]),
		AudioDuration: durationOf(tracks[1]),
		Bytes:         written,
		Out:           dst,
	}
	res.Drift = res.VideoDuration - res.AudioDuration
	if res.Drift < 0 {
		res.Drift = -res.Drift
	}
	return res, nil
}

// durationOf 把一条轨的呈现时长（电影时基）换成人能读的。
func durationOf(t *track) time.Duration {
	return ticksToDuration(t.presDur, t.movieTS)
}

// ticksToDuration 把电影时基下的刻度换成 `time.Duration`。
func ticksToDuration(ticks uint64, ts uint32) time.Duration {
	if ts == 0 {
		return 0
	}
	return time.Duration(float64(ticks) / float64(ts) * float64(time.Second))
}

// check 是交织之前的那道关：源配对不对就别写。
//
// 这一层的错误分两类，处置完全不同：
//
//	**源有问题**（取错流、下截了、编码不认识）→ 报错，一秒都不写
//	**结果会不对**（两条轨差太多）→ 报错，因为「慢慢漂」看不出来
//
// 所以这里每一条都是**拒绝写**，没有一条是「写了再说」。
func check(video, audio *source) error {
	if err := checkCodec(video); err != nil {
		return err
	}
	if err := checkCodec(audio); err != nil {
		return err
	}
	// 一个关键帧都没有的视频轨交出去是一个「打开是黑的」的文件。
	// 而它照样有正确的时长、正确的轨道数——不看 stss 根本发现不了。
	syncs := 0
	for _, s := range video.samples {
		if s.sync {
			syncs++
		}
	}
	if syncs == 0 {
		return fmt.Errorf("mux: 那条视频流里一个关键帧都没有——这不像是一条下全了的视频")
	}

	dv := ticksToDuration(video.mediaDur, video.timescale)
	da := ticksToDuration(audio.mediaDur, audio.timescale)
	drift := dv - da
	if drift < 0 {
		drift = -drift
	}
	if drift > maxDrift {
		return fmt.Errorf("mux: 两条流对不上：视频 %.1f 秒、音频 %.1f 秒，差了 %.1f 秒"+
			"（超过允许的 %s）——多半是取到了不同的一集，或者其中一条没下全",
			dv.Seconds(), da.Seconds(), drift.Seconds(), maxDrift)
	}
	return nil
}

// checkCodec 报这条流的采样描述符是不是我们能搬的。
//
// `stsd` 是从源搬过去的，所以搬一个不认识的进去，得到的是一个**结构
// 完整、但没有解码器配置**的文件：播放器认得这个盒子（`avc1` / `mp4a`
// 它都认），于是不报错，只是播不出声音或者黑屏。这种错只有在这里拦。
func checkCodec(s *source) error {
	stsd := s.trak.Mdia.Minf.Stbl.Stsd
	want := "avc1"
	if !s.video {
		want = "mp4a"
	}
	name, ok := sampleEntryName(stsd)
	if !ok || name == "" {
		return fmt.Errorf("mux: 那条%s流的 stsd 里没有采样描述符", kindOf(s))
	}
	if s.video {
		if !visualHasConfig(stsd) {
			return fmt.Errorf("mux: 那条视频流是 %s，却没有解码器配置（%s）", name, want)
		}
		return nil
	}
	if name != "mp4a" && name != "fLaC" && name != "enca" {
		return fmt.Errorf("mux: 那条音频流是 %s，这个应用只下得动 mp4a（AAC）和 fLaC", name)
	}
	return nil
}

// sampleEntryName 报 `stsd` 里装的是哪一种采样描述符。
//
// mp4ff 解出来是一个个**具名**的字段（`AvcX` / `Mp4a` / …）而不是一张表，
// 所以这里是挨个问一遍。顺序里有视频的在前，所以一条装了两种描述符的流
// 会先报视频那个——对这一票来说正好（我们一条流一条轨）。
//
// ⚠️ 这里**不能**把它们塞进一个 `[]mp4.Box` 再 `if b != nil`：那样空的
// 字段会变成「装着 nil 指针的非 nil 接口」，判不出来，
// `Type()` 一调就 panic。挨个判具体类型才躲得开。
func sampleEntryName(stsd *mp4.StsdBox) (string, bool) {
	switch {
	case stsd.AvcX != nil:
		return stsd.AvcX.Type(), true
	case stsd.HvcX != nil:
		return stsd.HvcX.Type(), true
	case stsd.VvcX != nil:
		return stsd.VvcX.Type(), true
	case stsd.Av01 != nil:
		return stsd.Av01.Type(), true
	case stsd.VpXX != nil:
		return stsd.VpXX.Type(), true
	case stsd.Mp4v != nil:
		return stsd.Mp4v.Type(), true
	case stsd.Mp4a != nil:
		return stsd.Mp4a.Type(), true
	case stsd.Enca != nil:
		return stsd.Enca.Type(), true
	}
	for _, c := range stsd.Children {
		if _, ok := c.(mp4.Box); ok {
			return c.Type(), true
		}
	}
	return "", false
}

// visualHasConfig 报视频的采样描述符里有没有解码器要的那份配置。
//
// 少一份就是一个「结构完整、黑屏」的文件，而它不报错——所以在这里问。
func visualHasConfig(stsd *mp4.StsdBox) bool {
	if v := stsd.AvcX; v != nil {
		return v.AvcC != nil
	}
	if v := stsd.HvcX; v != nil {
		return v.HvcC != nil
	}
	if v := stsd.VvcX; v != nil {
		return v.VvcC != nil
	}
	if v := stsd.Av01; v != nil {
		return v.Av1C != nil
	}
	if v := stsd.VpXX; v != nil {
		return v.VppC != nil
	}
	return false
}

// countingWriter 记下写了多少字节。
type countingWriter struct {
	f *os.File
	n *int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.f.Write(p)
	*c.n += int64(n)
	return n, err
}

// ErrNotFound 保留给将来「交织的时候源已经不在了」那一层用。
var ErrNotFound = errors.New("mux: 找不到要交织的源流")
