package mfadec

// 这一组断的是**解码器这一层**（`decoder.go`）的算术：采样怎么数、开头该丢
// 多少、跳位置之后落在哪。
//
// # 为什么这一组能在 macOS 上跑
//
// Media Foundation 只在 Windows 上有，所以这里用的是**假的解码器**（一个
// 按固定长度吐采样的替身）。换掉的是**解码器**，验的是**这一层的算术**——
// 而那些算术错了的症状是「播出来的东西错位几百毫秒」，在真机上极难看出来：
// 歌还是那么好听，只是慢了半拍。
//
// 真解码器那一半（Media Foundation 认不认得、逐样本对不对得上、COM apartment
// 稳不稳）在 `spike_windows_test.go` 里，那是 Windows 上的事。
//
// # 素材
//
// 用的是仓库里那两份：`aaclc-stereo.m4a`（MP4 家族，裸 AAC 帧）和
// `aaclc-stereo.aac`（裸 ADTS）。它们的来历见 `fixture_test.go`。

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// 替身
// ─────────────────────────────────────────────────────────────────────────────

// fakeUnitLen 是替身每一帧吐多少个采样（每声道）。
//
// 取 **1024**——AAC-LC 一帧的真实长度。取别的数的话，跳位置那套算术
// （丢掉一整帧把解码器的重叠状态热起来）就对不上了。
const fakeUnitLen = 1024

// fakeCodec 是「解码器」那一半的替身。
//
// 它每一帧吐 `perFrame` 个采样（每声道），采样值是**这一帧的序号**——所以
// 「解出来的第几个采样」是可以对着算的，而不是「看起来对」。
//
// # ⚠️ 它模仿了真解码器的**两个**关键行为
//
//  1. **解码器吞掉第一帧**。AAC 的 MDCT 是有重叠的，解码器输出的第一帧是
//     「重叠不完整」的冷启动帧——也就是**编码器的预热**，**它不是呈现出来的
//     音频的一部分**。所以喂 N 帧只交出 **N-1** 帧，而且交出来的第一帧是
//     **第 1 帧**（第 0 帧被吞了）。
//  2. **输出比输入慢一帧**：这一帧解出来的采样要等下一趟才交出去，所以
//     **最后一次 drain 才交出最后一帧**。
//
// 这两条合起来就是真机的行为（issue #128 的评审 + 仓库自己量过的数：原始输出
// 173 帧 = 177152 个采样，正好等于 ffmpeg 解同一份 .m4a 的结果）。
//
// 不模仿它们的话，「读完要 drain」和「不丢编码器延迟」那两条测试都是假的：
// 替身每一帧都有输出、而且从第 0 帧起算，于是永远轮不到 drain，而真解码器
// 少了那一帧的症状是「结尾少 1024 个采样」（17 毫秒，悄悄少掉）。
type fakeCodec struct {
	rate, chans int
	// perFrame 是每一帧吐多少个采样（每声道）。
	perFrame int
	// feeds 是喂过几帧。
	feeds int
	// produced 是已经**解出来**几帧（不等于交出去几帧）。
	produced int
	// held 是攥着还没交出去的那一帧（输出比输入慢一帧）。
	held []float32
	// drains 是 drain 过几次。
	drains int
	// closed 是关过几次。
	closed int
	// drained 是 drain 时额外吐出来的采样（默认空）。
	drained []float32
}

// Feed 吐**上一帧**的采样；第一帧被解码器吞掉。
func (c *fakeCodec) Feed(frame []byte) ([]float32, error) {
	c.feeds++
	cur := c.frameSamples(c.produced)
	c.produced++
	if c.produced == 1 {
		// ⚠️ **第一帧被吞掉**：它是 MDCT 重叠不完整的冷启动帧 = 编码器的预热。
		return nil, nil
	}
	// 输出比输入慢一帧：这一帧先攥着，下一趟再交出去。
	if c.held == nil {
		c.held = cur
		return nil, nil
	}
	out := c.held
	c.held = cur
	return out, nil
}

// frameSamples 造一帧采样：值都是这一帧的序号。
func (c *fakeCodec) frameSamples(index int) []float32 {
	out := make([]float32, c.perFrame*c.chans)
	for i := 0; i < c.perFrame; i++ {
		for ch := 0; ch < c.chans; ch++ {
			out[i*c.chans+ch] = float32(index)
		}
	}
	return out
}

// Drain 吐出攥着的最后一帧。
func (c *fakeCodec) Drain() ([]float32, error) {
	c.drains++
	if len(c.drained) > 0 {
		return c.drained, nil
	}
	if c.held == nil {
		return nil, nil
	}
	out := c.held
	c.held = nil
	return out, nil
}

func (c *fakeCodec) SampleRate() int { return c.rate }
func (c *fakeCodec) Channels() int   { return c.chans }
func (c *fakeCodec) Close() error    { c.closed++; return nil }

// ─────────────────────────────────────────────────────────────────────────────
// 工具
// ─────────────────────────────────────────────────────────────────────────────

// newTestDecoder 用一份素材和一只假解码器建一个 Decoder。
//
// 它绕开了 `Open`（那一条要真的解码器），但**其余每一行都是生产代码**：
// 帧源是 `openFrameSource` 认出来的那个，解码器的状态机是 `decoder.go` 那个。
func newTestDecoder(t *testing.T, name string, perFrame int) (*Decoder, *fakeCodec) {
	t.Helper()
	src, err := openFrameSource(bytes.NewReader(readFixture(t, name)))
	if err != nil {
		t.Fatalf("认容器 %s：%v", name, err)
	}
	codec := &fakeCodec{rate: src.rate, chans: src.chans, perFrame: perFrame}
	return &Decoder{
		src:   src,
		codec: codec,
		newCodec: func(*frameSource) (aacCodec, error) {
			return &fakeCodec{rate: src.rate, chans: src.chans, perFrame: perFrame}, nil
		},
		rate:  codec.SampleRate(),
		chans: codec.Channels(),
		total: src.total,
		// ⚠️ **不按容器的 `EncoderDelay` 丢**——解码器自己吞掉了那一帧
		// （见 `TestTheDecoderDoesNotDropTheEncoderDelayTwice`）。
		skip: 0,
	}, codec
}

// readAll 把解码器剩下的采样全读出来。
func readAll(t *testing.T, d *Decoder) []float32 {
	t.Helper()
	var out []float32
	buf := make([]float32, 4096*d.chans)
	for {
		n, err := d.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return out
			}
			t.Fatalf("读：%v", err)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 用例
// ─────────────────────────────────────────────────────────────────────────────

// TestTheDecoderDoesNotDropTheEncoderDelayTwice 断「编码器延迟**不能**按容器的
// `EncoderDelay` 再丢一遍」。
//
// # 为什么
//
// AAC 的 MDCT 是有重叠的，**解码器输出的第一帧是「重叠不完整」的冷启动帧**——
// 它就是编码器的预热，**不在呈现出来的音频里**。解码器自己会吞掉它，所以
// 解码器的输出**本来就从呈现音频的开头起**。
//
// 而 MP4 的编辑列表里还写着 `media_time = 1024`（同一个预热）。**那一条是给
// 「不知道有解码器会吞它」的解码器用的**——AudioToolbox 和 ffmpeg 都照它剪，
// 所以它们解出来的东西和参考 WAV 对得上。
//
// 我们的解码器（Media Foundation）自己已经吞了那一帧。**再按 `EncoderDelay`
// 丢一遍就是丢了两遍**：凭空砍掉 1024 个真实采样（23 毫秒），而且比容器说的
// 呈现长度少 272 个采样——**判据①的下界会因此红**。
//
// # 断的是什么
//
// 第一个交出来的采样必须**就是解码器交出来的第一帧**（替身给它打的序号是
// 1，因为第 0 帧被解码器吞了）。要是产品层又丢了一遍，这个序号会变成 2。
func TestTheDecoderDoesNotDropTheEncoderDelayTwice(t *testing.T) {
	for _, name := range []string{"aaclc-stereo.m4a", "aaclc-stereo.aac"} {
		t.Run(name, func(t *testing.T) {
			d, _ := newTestDecoder(t, name, fakeUnitLen)
			got := readAll(t, d)
			// 替身的第 0 帧被解码器吞掉了，所以交出来的第一帧是第 1 帧。
			const want = 1.0
			if got[0] != want {
				t.Errorf("第一个采样来自第 %v 帧，want %v——编码器延迟被丢了两遍",
					got[0], want)
			}
			t.Logf("%s：第一个采样来自第 %v 帧（解码器吞掉的是第 0 帧）", name, got[0])
		})
	}
}

// TestTheDecoderHandsOutEveryFrameItPromised 断「读出来长度和容器说的一致」。
//
// `Len()` 是**呈现出来的**长度，Read 交出去的采样必须和它对得上——差一帧
// 的话进度条上的时间和实际听到的对不上。
func TestTheDecoderHandsOutEveryFrameItPromised(t *testing.T) {
	for _, name := range []string{"aaclc-stereo.m4a", "aaclc-stereo.aac"} {
		t.Run(name, func(t *testing.T) {
			d, _ := newTestDecoder(t, name, fakeUnitLen)
			got := readAll(t, d)
			frames := len(got) / d.chans
			t.Logf("%s：容器说 %d 帧，读出来 %d 帧（%d Hz / %d 声道）",
				name, d.total, frames, d.rate, d.chans)
			if d.total >= 0 && int64(frames) != d.total {
				t.Errorf("读出来 %d 帧，容器说的是 %d 帧", frames, d.total)
			}
		})
	}
}

// TestTheDecoderDrainsAtTheEndOfTheStream 断「读完会去 drain」。
//
// ⚠️ **必须 drain**：AAC 解码器有 MDCT 的重叠状态，最后一帧是「和前一帧重叠
// 着」的，不 drain 它就一直不发——解出来的 PCM 少最后一帧。
func TestTheDecoderDrainsAtTheEndOfTheStream(t *testing.T) {
	d, codec := newTestDecoder(t, "aaclc-stereo.m4a", fakeUnitLen)
	readAll(t, d)
	if codec.drains != 1 {
		t.Errorf("drain 了 %d 次，want 1", codec.drains)
	}
}

// TestSeekingLandsExactlyWhereItWasAsked 断「跳位置落在哪」。
//
// 这是这一层最容易错的地方：跳到第 F 帧，交出来的应该正好是**从第 F 帧开始**
// 的那些采样。多一帧少一帧，肉耳听不出来（歌还是那首歌），但拖进度条之后
// 听到的位置和界面上写的差了一拍。
//
// 断法是**对比**：从头读一遍拿到总数 C，跳到第 F 帧再读一遍必须是 C-F。
// 这样就不用知道「正确解器会解出多少」——那本来就取决于解码器。
func TestSeekingLandsExactlyWhereItWasAsked(t *testing.T) {
	for _, name := range []string{"aaclc-stereo.m4a", "aaclc-stereo.aac"} {
		t.Run(name, func(t *testing.T) {
			d, _ := newTestDecoder(t, name, fakeUnitLen)
			all := readAll(t, d)
			total := len(all) / d.chans

			for _, frame := range []int64{1, 1000, 5000, int64(total) - 1} {
				if frame <= 0 || frame >= int64(total) {
					continue
				}
				d2, _ := newTestDecoder(t, name, fakeUnitLen)
				if err := d2.SetPosition(frame); err != nil {
					t.Fatalf("跳到第 %d 帧：%v", frame, err)
				}
				rest := readAll(t, d2)
				got := len(rest) / d2.chans
				want := total - int(frame)
				if got != want {
					t.Errorf("跳到第 %d 帧之后读出来 %d 帧，want %d（差 %d 帧）",
						frame, got, want, want-got)
				}
			}
		})
	}
}

// TestSeekingStartsWithTheRightSample 断「跳过去之后第一个采样就是那一帧的」。
//
// 上一条断的是**数量**，这一条断的是**内容**：跳到第 F 帧之后交出来的采样，
// 必须和「从头读、然后跳过前 F 帧」读到的是同一段——不多不少不少一帧。
//
// 替身给每一帧打上它的序号，所以「同一段」是可以逐个采样比出来的。
func TestSeekingStartsWithTheRightSample(t *testing.T) {
	const perFrame = fakeUnitLen

	d, _ := newTestDecoder(t, "aaclc-stereo.m4a", perFrame)
	all := readAll(t, d)
	chans := d.chans

	for _, frame := range []int64{1024, 2048, 4096} {
		d2, _ := newTestDecoder(t, "aaclc-stereo.m4a", perFrame)
		if err := d2.SetPosition(frame); err != nil {
			t.Fatalf("跳到第 %d 帧：%v", frame, err)
		}
		rest := readAll(t, d2)
		if len(rest) == 0 {
			t.Fatalf("跳到第 %d 帧之后什么也没读到", frame)
		}
		// 跳过去之后替身是从头数的，所以两边的序号差一个常数——
		// 那个常数就是「跳过去之后重新解的第一帧」。
		base := float32(all[int(frame)*chans])
		for i := range rest {
			want := all[int(frame)*chans+i] - base
			got := rest[i] - rest[0]
			if got != want {
				t.Fatalf("跳到第 %d 帧之后第 %d 个采样 = %v，want %v（落点偏了）",
					frame, i, got, want)
			}
		}
	}
}

// TestTheDecoderRefusesToSeekOutsideTheStream 断「跳到流外面要报错」。
func TestTheDecoderRefusesToSeekOutsideTheStream(t *testing.T) {
	d, _ := newTestDecoder(t, "aaclc-stereo.m4a", fakeUnitLen)
	if err := d.SetPosition(-1); !errors.Is(err, ErrSeekRange) {
		t.Errorf("跳到第 -1 帧：%v，want %v", err, ErrSeekRange)
	}
	if d.total >= 0 {
		if err := d.SetPosition(d.total + 1); !errors.Is(err, ErrSeekRange) {
			t.Errorf("跳到第 %d 帧：%v，want %v", d.total+1, err, ErrSeekRange)
		}
	}
}

// TestTheDecoderCountsHowLongTheADTSStreamIs 断「裸 ADTS 的长度数得出来」。
//
// ADTS 没有容器，长度只能靠扫一遍整个流数帧——而**数的那一趟不能把解码
// 弄坏**（扫完要把游标拨回去）。所以这一条既断长度，也断「数完之后照样
// 解得出来」。
//
// ⚠️ 这一条是有意义的：长度不知道的话，界面上的进度条**没有长度**
// （`Player.Duration()` 返回 0），而 `.aac` 文件在 Windows 上只有这一条路。
func TestTheDecoderCountsHowLongTheADTSStreamIs(t *testing.T) {
	d, _ := newTestDecoder(t, "aaclc-stereo.aac", fakeUnitLen)

	got := d.Len()
	t.Logf("裸 ADTS：数出来 %d 帧", got)
	if got <= 0 {
		t.Fatalf("裸 ADTS 的长度数不出来（%d）", got)
	}

	// 数完之后照样要解得出来——而且解出来的长度要和数出来的一致。
	pcm := readAll(t, d)
	frames := len(pcm) / d.chans
	t.Logf("数出来 %d 帧，解出来 %d 帧", got, frames)
	if int64(frames) != got {
		t.Errorf("数出来 %d 帧，解出来 %d 帧", got, frames)
	}
}

// TestTheContainerIsRecognizedByItsHeader 断「认容器按头几个字节，而且不认错」。
//
// ⚠️ **MP3 认成 ADTS 是最危险的一种认错**：两者都以 `FF` 开头，只断前 12 个
// 同步位的话 MP3 的帧头（`FF FB` / `FF FA` / `FF F3` …）也认得出来——而 MP3 的
// layer 位是 01/10/11，ADTS 必须是 00。
//
// 认错的后果不是报错，是**解出别的东西**：一个 MP3 被当 ADTS 拆，拆出来的
// 「帧」是垃圾，解出来是噪声。
func TestTheContainerIsRecognizedByItsHeader(t *testing.T) {
	// 真的素材：MP4 和 ADTS 各一份（认容器要用真头，不能手搓）。
	realM4A := readFixture(t, "aaclc-stereo.m4a")
	realADTS := readFixture(t, "aaclc-stereo.aac")

	for _, c := range []struct {
		name string
		head []byte
		want string
	}{
		{"MP4 家族（真素材）", realM4A[:16], "mp4"},
		{"裸 ADTS（真素材）", realADTS[:16], "adts"},
		{"分片 MP4（styp）", []byte{0x00, 0x00, 0x00, 0x20, 's', 't', 'y', 'p'}, "mp4"},
		{"MP3（MPEG-1 layer III）", []byte{0xFF, 0xFB, 0x90, 0x64, 0x00, 0x00, 0x00}, ""},
		{"MP3（MPEG-2 layer III）", []byte{0xFF, 0xF3, 0x90, 0x64, 0x00, 0x00, 0x00}, ""},
		{"MP3（MPEG-2.5 layer III）", []byte{0xFF, 0xE3, 0x90, 0x64, 0x00, 0x00, 0x00}, ""},
		{"一段文字", []byte("this is not audio"), ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := detectContainer(c.head)
			if got != c.want {
				t.Errorf("认出 %q，want %q", got, c.want)
			}
			t.Logf("%s → %q", c.name, got)
		})
	}
}

// TestTheADTSPathDerivesTheSameAudioSpecificConfig 断「ADTS 那一路自己拼出来的
// AudioSpecificConfig，和 MP4 那一路从容器里读到的**是同一份**」。
//
// # 为什么这条重要
//
// 两种容器装的是**同一段录音**（同一份 WAV 编出来的），所以它们的
// AudioSpecificConfig 必须**逐字节相同**——那是解码器的全部输入参数
// （采样率、声道数、AAC-LC）。
//
// 而这两条路的 ASC **来源完全不同**：MP4 那一路是容器里的 `esds` 盒
// （go-m4a 读出来的），ADTS 那一路是**我们自己从 ADTS 头里拼的**
// （`ascFromADTS`）。拼错了的话，ADTS 那一路会用错的参数去解码——
// 症状是「能出声，但不是那首歌」（采样率错 → 变调）。
//
// 这条**不需要真解码器**，所以在 macOS 上就能验 ✓。
func TestTheADTSPathDerivesTheSameAudioSpecificConfig(t *testing.T) {
	adtsSrc, err := openADTS(bytes.NewReader(readFixture(t, "aaclc-stereo.aac")))
	if err != nil {
		t.Fatalf("拆裸 ADTS：%v", err)
	}
	m4aSrc, err := openM4A(bytes.NewReader(readFixture(t, "aaclc-stereo.m4a")))
	if err != nil {
		t.Fatalf("拆 MP4：%v", err)
	}

	// ⚠️ **只比前两个字节**：MP4 的 ASC（`esds` 盒里的）在这份素材上是 **5
	// 字节**——`12 10` 后面还有 3 个字节的扩展信令。ADTS 的头里**没有**扩展
	// 那一段（ADTS 头的格式里放不下），所以我们拼出来的必然只有 2 字节。
	//
	// 那 3 个字节在这份素材上是**不起作用**的（ffprobe 报的是 profile=LC、
	// 44100 Hz，编码器没用 SBR/PS），所以少给它们不影响解码。
	// ⚠️ 但这是一件**要记着**的事：**HE-AAC（SBR）在 ADTS 那一路表达不出来**——
	// 那一档本项目没验过（素材造不出来）。
	if len(adtsSrc.asc) != 2 {
		t.Errorf("ADTS 拼出来的 ASC 是 %d 字节（% x），want 2", len(adtsSrc.asc), adtsSrc.asc)
	}
	if len(m4aSrc.asc) < 2 || !bytes.Equal(adtsSrc.asc, m4aSrc.asc[:2]) {
		t.Errorf("ADTS 拼出来的 ASC = % x，MP4 的头两个字节 = % x——核心配置必须一样",
			adtsSrc.asc, m4aSrc.asc)
	}
	t.Logf("ADTS 拼出来的 ASC = % x（%d 字节），MP4 的是 % x（%d 字节，多的 %d 字节是 ADTS 头表达不了的扩展信令）",
		adtsSrc.asc, len(adtsSrc.asc), m4aSrc.asc, len(m4aSrc.asc), len(m4aSrc.asc)-2)

	if adtsSrc.rate != m4aSrc.rate || adtsSrc.chans != m4aSrc.chans {
		t.Errorf("ADTS 说 %d Hz / %d 声道，MP4 说 %d Hz / %d 声道",
			adtsSrc.rate, adtsSrc.chans, m4aSrc.rate, m4aSrc.chans)
	}
	if adtsSrc.payloadType != aacPayloadADTS || m4aSrc.payloadType != aacPayloadRaw {
		t.Errorf("payloadType 不对：ADTS = %d，MP4 = %d", adtsSrc.payloadType, m4aSrc.payloadType)
	}
}
