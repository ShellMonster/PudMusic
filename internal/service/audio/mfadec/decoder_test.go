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
// ⚠️ **它模仿了真解码器的一个关键行为**：喂一帧进去，出来的是**上一帧**的
// 采样——MDCT 的重叠状态要靠下一帧才补齐。所以喂 N 帧只交出 N-1 帧，
// **最后一次 drain 才交出最后一帧**。
//
// 不模仿这一条的话，「读完要 drain」那条测试就是假的：替身每一帧都有输出，
// 于是永远轮不到 drain，而真解码器少了那一帧的症状是「结尾少 1024 个采样」
// （17 毫秒，悄悄少掉）。
type fakeCodec struct {
	rate, chans int
	// perFrame 是每一帧吐多少个采样（每声道）。
	perFrame int
	// feeds 是喂过几帧。
	feeds int
	// produced 是已经**解出来**几帧（不等于交出去几帧）。
	produced int
	// drains 是 drain 过几次。
	drains int
	// closed 是关过几次。
	closed int
	// drained 是 drain 时吐出来的采样（默认空）。
	drained []float32
}

// Feed 吐**上一帧**的采样。
func (c *fakeCodec) Feed(frame []byte) ([]float32, error) {
	c.feeds++
	if c.produced == 0 {
		// 第一帧没有「上一帧」可交：解码器的重叠状态是冷的。
		c.produced++
		return nil, nil
	}
	out := c.frameSamples(c.produced - 1)
	c.produced++
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

// Drain 吐出攒着的最后一帧。
func (c *fakeCodec) Drain() ([]float32, error) {
	c.drains++
	if len(c.drained) > 0 {
		return c.drained, nil
	}
	if c.produced == 0 {
		return nil, nil
	}
	out := c.frameSamples(c.produced - 1)
	c.produced++
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
		skip:  src.delay,
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

// TestTheDecoderDropsTheContainersEncoderDelay 断「开头那几百个采样要丢掉」。
//
// 那是编码器的预热（MP4 的编辑列表把它记成 `EncoderDelay`），**不属于**呈现
// 出来的音频。不丢的话，开头会有一声「咔」——而且歌整体早了那一点。
//
// 断的是**丢掉的数量**：第一个交出来的采样必须正好落在 delay 之后的那一帧。
func TestTheDecoderDropsTheContainersEncoderDelay(t *testing.T) {
	for _, name := range []string{"aaclc-stereo.m4a", "aaclc-stereo.aac"} {
		t.Run(name, func(t *testing.T) {
			d, _ := newTestDecoder(t, name, fakeUnitLen)
			if d.src.delay == 0 {
				t.Skip("这份素材没有编码器延迟（裸 ADTS 没有容器，没有延迟可丢）")
			}
			got := readAll(t, d)
			// 第一个采样是原始的第 delay 个采样之后的那一个。
			want := float32(d.src.delay / fakeUnitLen)
			if got[0] != want {
				t.Errorf("第一个采样 = %v，want %v（编码器延迟 %d 没有被丢掉）",
					got[0], want, d.src.delay)
			}
			t.Logf("%s：编码器延迟 %d 个采样，丢掉之后第一个采样来自第 %v 帧",
				name, d.src.delay, got[0])
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
