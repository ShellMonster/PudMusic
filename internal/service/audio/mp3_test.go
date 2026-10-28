// MP3：MPEG-2.5 能放、LAME gapless 生效、探测时长不再全文件扫帧头。
//
// # 这一票换掉了什么
//
// 换掉之前，MP3 走的是 MyGo 自带的 `mygo/audio/mp3`（解码器是
// `hajimehoshi/go-mp3`，Apache-2.0，**已归档**）。那个库有三件事做不对：
//
//  1. **不支持 MPEG-2.5**——它在自己的 `internal/frame/frame.go` 里明写着
//     遇到 MPEG-2.5 就报错。而 MPEG-2.5（8/11.025/12 kHz）是低码率语音的
//     常见格式，界面上 `.mp3` 写着「可放」，点进去却报「解不了」。
//  2. **完全不做 LAME gapless**——编码器延迟（1105 个样本）原样留在输出里。
//     症状是每首歌开头多一段静音、结尾多一段，而且**歌和下一首接不上**。
//  3. **`NewDecoder` 把整个文件扫一遍**才算出长度。那是本地音乐列表扫几百个
//     文件时的几百次全文件读。
//
// 现在换的是 `tphakala/go-mp3`（MIT），vendor 进
// `internal/service/audio/mp3dec/`（来路见那一带的 PROVENANCE.md）。
//
// # 判据是什么
//
// 这一组不查「解码器注册成功」那种东西——那只能证明注册表里有它。三条判据
// 各量一件**具体的事**：
//
//   - **MPEG-2.5**：解出来的**音高**得是 440 Hz（素材是一段 440 Hz 正弦）。
//     解码器返回静音 → 过零数 0 → 红；返回噪声 → 频率对不上 → 红；
//     采样率读错 → 按错的采样率折算 → 红。
//   - **LAME gapless**：解出来的**长度**必须正好等于原始素材的长度，而**不裁**
//     的话会多出 1237 个样本（编码器延迟 576 + 末尾填充 661）。这一条是拿
//     「裁过的」和「没裁过的」两个解码结果**对读**出来的，不是拿某一边的
//     说法对读。
//   - **不再全文件扫帧头**：数**这个解码器一共从文件里读了多少字节**。
//     素材是一份 1 MB 的 MP3，读了几十 KB 就该收工；全文件扫的话是 1 MB。
package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	mygoaudio "github.com/egoist/mygo/audio"

	"github.com/ShellMonster/PudMusic/internal/service/audio/mp3dec"
	gomp3 "github.com/ShellMonster/PudMusic/internal/service/audio/mp3dec/gomp3"
)

// 素材的规格。这几个数**不是从素材里读出来的**，是素材的规格——写在这里是
// 为了让「解出来的对不对」有一个不依赖被测代码的判据。
//
// # `mpeg25.mp3` 怎么来的
//
//	ffmpeg -f lavfi -i "sine=frequency=440:duration=1:sample_rate=11025" \
//	       -ac 1 -c:a libmp3lame -b:a 32k mpeg25.mp3
//
// 11025 Hz 落在 MPEG-2.5 那一档（8/11.025/12 kHz），所以这一份是 MPEG-2.5
// Layer III——**旧的解码器解不了它**。1 秒 = 11025 个样本。
const (
	mpeg25Fixture = "mpeg25.mp3"
	mpeg25Rate    = 11025
	mpeg25Tone    = 440.0
	// mpeg25Frames 是这份素材**裁掉编码器延迟之后**应该有的样本数
	// （1 秒 × 11025 Hz）。带 LAME 标签，延迟 576、填充 1071。
	mpeg25Frames = 11025
)

// `gapless.mp3` 的规格。
//
//	ffmpeg -f lavfi -i "sine=frequency=440:duration=0.375:sample_rate=44100" \
//	       -af "adelay=125,apad=pad_dur=0.125" -ac 1 -ar 44100 \
//	       -c:a pcm_s16le gapless-src.wav
//	ffmpeg -i gapless-src.wav -c:a libmp3lame -b:a 128k gapless.mp3
//
// 也就是「0.125 秒静音 + 0.375 秒 440 Hz 正弦」，44100 Hz 单声道。
// `gapless-src.wav` 有 **27563** 个样本，声音从第 **5513** 个样本开始。
//
// 这一份带 LAME gapless 标签（ffmpeg 写的 "Lavc" 扩展）：编码器延迟 **576**、
// 末尾填充 **661**。不裁的话会多出 576+661 = **1237** 个样本（头 1105 =
// 576+529，尾 132 = 661-529）。
const (
	gaplessFixture = "gapless.mp3"
	gaplessRate    = 44100
	// gaplessSourceFrames 是原始素材的样本数——**裁完之后**应该正好是这个数。
	gaplessSourceFrames = 27563
	// gaplessOnset 是原始素材里声音开始的位置（0.125 秒 × 44100）。
	// 裁对了的话，解出来的声音也从这里开始。
	gaplessOnset = 5513
	// gaplessTrimmed 是「不裁」时解出来的样本数（25 帧 × 1152）。
	// 拿它和 gaplessSourceFrames 对读，就量出了裁掉了多少。
	gaplessTrimmed = 28800
)

// TestMPEG25ReallyDecodes 钉住「MPEG-2.5 能放」。
//
// 走的是应用真正走的那条路（`openDecoder`）：本地文件和 B 站在线流认容器
// 认的都是它。少了 MP3 那一行注册，这里第一步就报「不支持这个音频格式」。
func TestMPEG25ReallyDecodes(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", mpeg25Fixture))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	d, name, err := openDecoder(f)
	if err != nil {
		t.Fatalf("解不了 MPEG-2.5：%v", err)
	}
	if name != "mp3" {
		t.Errorf("认出来的容器是 %q，want mp3", name)
	}
	if d.SampleRate() != mpeg25Rate {
		t.Errorf("采样率是 %d，want %d", d.SampleRate(), mpeg25Rate)
	}
	if d.Channels() != 1 {
		t.Errorf("声道数是 %d，want 1", d.Channels())
	}

	samples := readAllSamples(t, d)
	frames := len(samples) / d.Channels()
	if frames != mpeg25Frames {
		t.Errorf("解出 %d 帧，want %d（1 秒 × %d Hz）", frames, mpeg25Frames, mpeg25Rate)
	}

	// 音高：数过零。单声道，逐个样本看符号。
	zc := 0
	for i := 1; i < len(samples); i++ {
		if (samples[i-1] < 0) != (samples[i] < 0) {
			zc++
		}
	}
	if zc == 0 {
		t.Fatal("解出来的全是同一个符号（静音？），连一次过零都没有")
	}
	secs := float64(frames) / mpeg25Rate
	got := float64(zc) / 2 / secs
	if absf(got-mpeg25Tone) > 5 {
		t.Errorf("解出来的频率是 %v Hz，want %v Hz（素材是 %v Hz 的正弦）",
			got, mpeg25Tone, mpeg25Tone)
	}
}

// TestTheLameGaplessTrimIsApplied 钉住「编码器延迟被裁掉了」。
//
// # 判据是**两个解码结果对读**，不是「解码器自己说它裁了」
//
// 两边走的是**同一个解码器**、**同一份文件**，唯一的差别是：对照组那份把
// LAME 扩展的 4 字节标识（"Lavc"）改成了不认识的东西，于是那个标签不被承认，
// 于是**不裁**。
//
// 一份文件、两条路，长度差就是裁掉的量——而那个量必须正好等于标签里写的
// 「延迟 + 填充」（576 + 661 = 1237）。
//
// 只有「裁过的那条路」的长度等于**原始素材**的长度（27563）才算数：
// 一个把延迟原样留下的解码器，长度会是 28800，比原始素材多 1237。
func TestTheLameGaplessTrimIsApplied(t *testing.T) {
	// 一边：应用真正走的那条路。
	f, err := os.Open(filepath.Join("testdata", gaplessFixture))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	d, _, err := openDecoder(f)
	if err != nil {
		t.Fatalf("解不了 gapless.mp3：%v", err)
	}
	trimmed := readAllSamples(t, d)
	trimmedFrames := len(trimmed) / d.Channels()
	if trimmedFrames != gaplessSourceFrames {
		t.Errorf("裁完解出 %d 帧，want %d（原始素材的样本数）——"+
			"编码器延迟没被裁掉的话会多出 %d 个",
			trimmedFrames, gaplessSourceFrames, gaplessTrimmed-gaplessSourceFrames)
	}

	// 另一边：同一份文件，只把 LAME 扩展的标识改坏，于是那个标签不被承认、
	// 不裁。对照组用的是**同一个解码器**——这样差出来的量就只可能是裁剪。
	raw, err := os.ReadFile(filepath.Join("testdata", gaplessFixture))
	if err != nil {
		t.Fatal(err)
	}
	off := lameMagicOffset(t, raw)
	copy(raw[off:off+4], "XXXX")
	rawFrames := decodeFrames(t, raw)
	if rawFrames != gaplessTrimmed {
		t.Errorf("不裁的对照组解出 %d 帧，want %d——对照组本身变了，"+
			"这条测试就量不出东西了", rawFrames, gaplessTrimmed)
	}

	// 两边对读：差就是裁掉的量。
	if got := gaplessTrimmed - trimmedFrames; got != 1237 {
		t.Errorf("裁掉了 %d 个样本，want 1237（编码器延迟 576 + 末尾填充 661）", got)
	}
}

// TestTheGaplessDecodeStartsWhereTheSourceStarts 钉住「裁完之后声音还在原来的位置」。
//
// 上一条量的是**长度**；这一条量的是**位置**。
//
// 素材是「0.125 秒静音 + 0.375 秒正弦」，声音从第 5513 个样本开始。裁对了，
// 解出来的声音也从第 5513 个样本开始；不裁（只裁长度不裁头，或者压根没裁），
// 声音会往前挪 1105 个样本（编码器延迟 576 + 解码器合成延迟 529）。
//
// 判据留了 ±64 个样本的宽：MP3 是有损编码，边界那一个帧窗里有多少能量
// 糊在一起是有讲究的，但**挪 1105 个样本**是量得出来的差别。
func TestTheGaplessDecodeStartsWhereTheSourceStarts(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", gaplessFixture))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	d, _, err := openDecoder(f)
	if err != nil {
		t.Fatalf("解不了 gapless.mp3：%v", err)
	}
	samples := readAllSamples(t, d)

	onset := onsetOf(samples)
	if onset < 0 {
		t.Fatal("解出来的全是静音，连一个超过阈值的样本都没有")
	}
	if absi(onset-gaplessOnset) > 64 {
		t.Errorf("声音从第 %d 个样本开始，want %d（原始素材的位置）——"+
			"差了 %d 个样本", onset, gaplessOnset, onset-gaplessOnset)
	}
}

// TestOpeningAnMp3DoesNotReadTheWholeFile 钉住「打开一个 MP3 不再全文件扫帧头」。
//
// # 判据是**数字节**
//
// MP3 的长度不在文件头里，所以「算出长度」要么扫一遍帧头（老做法），要么
// 问文件末尾在哪（现在的做法：一次 lseek）。这两种代价差着一个数量级，而
// 差别在**读了多少字节**上一眼看得见——所以这里数这个解码器一共从文件里
// 读了多少字节。
//
// 素材是现搭的：把 `gapless.mp3` 的音频帧重复 100 遍（约 1 MB）。**必须够大**
// ——解码器内部有个 64 KB 的缓冲，小文件一次就被读完了，数不出差别。
func TestOpeningAnMp3DoesNotReadTheWholeFile(t *testing.T) {
	big := writeRepeatedMP3(t)
	defer os.Remove(big)

	f, err := os.Open(big)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 512*1024 {
		t.Fatalf("素材只有 %d 字节，太小了：解码器内部有 64 KB 的缓冲，"+
			"这么小的文件一次就被读完，数不出「有没有全文件扫」", info.Size())
	}

	counting := &countingSource{r: f}
	d, _, err := openDecoder(counting)
	if err != nil {
		t.Fatalf("解不了：%v", err)
	}
	defer func() {
		if c, ok := d.(io.Closer); ok {
			c.Close()
		}
	}()

	if counting.read >= info.Size()/4 {
		t.Errorf("打开这个 MP3 读了 %d 字节，文件一共 %d 字节——"+
			"这是全文件扫帧头的量级（老解码器就是这么干的）",
			counting.read, info.Size())
	}
	t.Logf("打开一个 %d 字节的 MP3，一共读了 %d 字节（%.1f%%）",
		info.Size(), counting.read,
		100*float64(counting.read)/float64(info.Size()))
	if d.Len() <= 0 {
		t.Errorf("解码器报不出长度（Len() = %d）", d.Len())
	}
}

// TestTheMp3DurationIsProbedFromItsOwnContainer 钉住本地音乐列表里「时长」
// 那一栏对 MP3 也有值，而且**不需要解一个样本**。
//
// `ProbeDuration` 问的是解码器「你一共多少帧」。对 MP3 来说那是标签里写着的
// 帧数（或者量一遍音频字节范围算出来的），不是解出来的。
func TestTheMp3DurationIsProbedFromItsOwnContainer(t *testing.T) {
	for _, c := range []struct {
		file  string
		rate  int
		frame int64
	}{
		{mpeg25Fixture, mpeg25Rate, mpeg25Frames},
		{gaplessFixture, gaplessRate, gaplessSourceFrames},
	} {
		t.Run(c.file, func(t *testing.T) {
			got, err := ProbeDuration(filepath.Join("testdata", c.file))
			if err != nil {
				t.Fatalf("读不出时长：%v", err)
			}
			want := float64(c.frame) / float64(c.rate)
			secs := got.Seconds()
			if absf(secs-want) > 0.01 {
				t.Errorf("时长是 %v 秒，want %v 秒", secs, want)
			}
		})
	}
}

// TestTheMp3DecoderIsOurs 钉住「注册进注册表的是本项目的解码器」。
//
// MyGo 的注册表是「按注册顺序找第一条命中的」，而 MP3 有过两个解码器
// （MyGo 自带的 `hajimehoshi/go-mp3` 和本项目的）。两个都注册的话，认出
// 哪一个取决于 init 顺序——那不是能靠读代码定下来的东西。
//
// 所以这里直接问**解出来的那个解码器是谁**：是 `*mp3dec.Decoder` 才说明
// 本项目注册的那一条在生效。
func TestTheMp3DecoderIsOurs(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", mpeg25Fixture))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	d, name, err := openDecoder(f)
	if err != nil {
		t.Fatalf("解不了：%v", err)
	}
	if name != "mp3" {
		t.Errorf("认出来的容器是 %q，want mp3", name)
	}
	if _, ok := d.(*mp3dec.Decoder); !ok {
		t.Errorf("解出来的是 %T，want *mp3dec.Decoder——"+
			"注册表里生效的不是本项目那一条", d)
	}
}

// countingSource 是个数字节的字节源：Read 数它读了多少，Seek 不数。
type countingSource struct {
	r    io.ReadSeeker
	read int64
}

func (c *countingSource) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.read += int64(n)
	return n, err
}

func (c *countingSource) Seek(off int64, whence int) (int64, error) {
	return c.r.Seek(off, whence)
}

// Stat 转发给底下那个源。
//
// ⚠️ **不能省**：解码器靠「这个源能 stat 自己」判断「问它末尾在哪是不是
// 不花钱」（本地文件问一次就行，网络流得先把整个下完）。包装器把这个方法
// 藏起来的话，解码器会以为问不到末尾，于是连长度都报不出来——那时候红的
// 不是「读了多少字节」，而是「长度报不出来」，测的就不是那件事了。
func (c *countingSource) Stat() (os.FileInfo, error) {
	return c.r.(interface {
		Stat() (os.FileInfo, error)
	}).Stat()
}

// decodeFrames 用**本项目的解码器**把一份 MP3 解完，返回它解出多少个样本
// （每声道一个算一个）。
func decodeFrames(t *testing.T, data []byte) int {
	t.Helper()
	d, err := mp3dec.Open(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("解不了：%v", err)
	}
	samples := readAllSamples(t, d)
	return len(samples) / d.Channels()
}

// lameMagicOffset 找出 LAME 扩展在文件里的位置（那 4 字节标识的起点）。
//
// LAME 扩展紧跟在 Xing/Info 标签的字段后面：第一帧是 4 字节的 "Xing" 或
// "Info"，然后 4 字节的标志位，再后面是标志位指到的那些字段（帧数、字节数、
// TOC、质量），字段之后就是 LAME 扩展的头 4 字节（"LAME"/"Lavc"/"Lavf"）。
//
// 改坏那 4 个字节，标签就不被承认了——于是解码器**不裁**。这是对照组。
func lameMagicOffset(t *testing.T, data []byte) int {
	t.Helper()
	for _, tag := range [][]byte{[]byte("Xing"), []byte("Info")} {
		i := bytes.Index(data, tag)
		if i < 0 {
			continue
		}
		flags := binary.BigEndian.Uint32(data[i+4 : i+8])
		p := i + 8
		if flags&1 != 0 {
			p += 4 // 帧数
		}
		if flags&2 != 0 {
			p += 4 // 字节数
		}
		if flags&4 != 0 {
			p += 100 // TOC
		}
		if flags&8 != 0 {
			p += 4 // 质量
		}
		return p
	}
	t.Fatal("文件里没有 Xing/Info 标签，这一组对照组就没法搭了")
	return 0
}

// writeRepeatedMP3 搭一份约 1 MB 的 MP3：把 `gapless.mp3` 的**音频帧**重复
// 100 遍。
//
// 为什么不用素材本身：它只有 10 KB，而解码器内部有个 64 KB 的缓冲——这么小
// 的文件一次就被读完了，「有没有全文件扫」根本数不出来。
//
// 为什么把第一帧（带 Info/LAME 标签的那一帧）去掉：重复它会得到一份「有好
// 多个 Info 标签」的怪文件，帧数对不上。这一份要的就是**朴素的 CBR 音频帧
// 串**——那正是「没有标签、只能靠量字节算长度」的那种形状。
func writeRepeatedMP3(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", gaplessFixture))
	if err != nil {
		t.Fatal(err)
	}
	frames := mp3Frames(t, data)
	if len(frames) < 3 {
		t.Fatalf("素材只拆出 %d 帧，太少了", len(frames))
	}
	audio := frames[1:] // 第 0 帧是 Info/LAME 标签帧

	var buf []byte
	for i := 0; i < 100; i++ {
		for _, f := range audio {
			buf = append(buf, f...)
		}
	}
	path := filepath.Join(t.TempDir(), "big.mp3")
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// mp3Frames 把一个 MP3 拆成帧（按字节切）。
func mp3Frames(t *testing.T, data []byte) [][]byte {
	t.Helper()
	d := gomp3.NewDecoder()
	buf := make([]float32, 1152*2)
	var frames [][]byte
	pos := 0
	for pos < len(data) {
		_, info, err := d.DecodeFrame(data[pos:], buf)
		if err != nil && !errors.Is(err, gomp3.ErrUnsupported) {
			t.Fatalf("拆第 %d 字节处的一帧：%v", pos, err)
		}
		if info.FrameBytes == 0 {
			break
		}
		frames = append(frames, data[pos:pos+info.FrameBytes])
		pos += info.FrameBytes
	}
	return frames
}

// onsetOf 找第一个超过阈值的样本（下标）。全都在阈值以下是 -1。
func onsetOf(samples []float32) int {
	const threshold = 0.01 // 幅度 1 是满刻度，0.01 是 -40 dB
	for i, v := range samples {
		if v > threshold || v < -threshold {
			return i
		}
	}
	return -1
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func absi(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// 编译期钉住：mp3dec 实现的确实是 MyGo 的解码器接口。
var _ mygoaudio.Decoder = (*mp3dec.Decoder)(nil)
