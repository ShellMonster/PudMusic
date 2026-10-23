package audio

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/service/audio/extaf"
	"github.com/ShellMonster/Biu_Go/internal/service/audio/flacdec"

	mygoaudio "github.com/egoist/mygo/audio"
	m4a "github.com/tphakala/go-m4a"
)

// 这一组盯的是 `openDecoder` 在「平台解码器不可用」时的那条落回路。
//
// # 为什么要用注入而不是真的造一个没有解码器的平台
//
// Windows 上 `extaf.Open` 恒返回 `ErrUnsupported`，而 macOS 上它永远成功。
// 于是「平台不可用 → 落回 flacdec」这条路径**在能跑测试的这个平台上根本
// 走不到**——不注入的话，写出来的测试在 macOS 上是绿的，在 Windows 上才
// 第一次被验证，而 Windows 那条 CI 现在还没跑起来（见 `windows-aac-spike.yml`）。
//
// 所以这里把「平台解码器」换成一个测试自己给的开销函数（`platformDecoderOpen`）。
// 换掉的是**机制**，验的是**行为**：落回的那条路和真 Windows 上跑的是同一条。
//
// # 素材
//
// `stereo16.mp4` 是 flacdec 那份素材：MP4 容器里的 FLAC，**正是 B 站无损流
// 的形状**（`flacdec/mp4.go`：「This is the shape Bilibili's FLAC audio
// streams have」）。判据是 `stereo16.wav`——编码**之前**的那段录音，无损
// 意味着解出来的每一个采样都等于原来那一个，所以不需要第二个解码器。

// TestOpenDecoderFallsBackToFLACWhenThePlatformDecoderIsUnavailable 盯
// **Windows 上那一档**：平台解码器不可用时，MP4 里的 FLAC 要落回 flacdec。
//
// 断的是三件事，一件都不能少：
//
//  1. 不报错（改之前这里是 `ErrUnsupported` 直接返回，B 站在线音频一档都放不出来）；
//  2. 容器名是 `flac`（不是 `mp4`——落回走的是 flacdec 那条路）；
//  3. 解出来的采样逐个等于原始录音（落回不能只是「不报错」）。
func TestOpenDecoderFallsBackToFLACWhenThePlatformDecoderIsUnavailable(t *testing.T) {
	stubPlatformDecoder(t, func(io.ReadSeeker) (mygoaudio.Decoder, error) {
		return nil, extaf.ErrUnsupported
	})

	d, name, err := openDecoder(bytes.NewReader(fixture(t, "stereo16.mp4")))
	if err != nil {
		t.Fatalf("平台解码器不可用时没落回 flacdec：%v", err)
	}
	if name != "flac" {
		t.Errorf("容器名 = %q，want flac（落回走的是 flacdec 那条路）", name)
	}

	src := stereo16(t)
	if d.Channels() != src.channels || d.SampleRate() != src.rate {
		t.Errorf("格式 = %d Hz / %d 声道，原始录音是 %d Hz / %d 声道",
			d.SampleRate(), d.Channels(), src.rate, src.channels)
	}
	want := make([]float32, len(src.samples))
	for i, v := range src.samples {
		want[i] = expect(v, src.bits)
	}
	compareSampleForSample(t, "落回 flacdec 的 MP4-FLAC", d.Channels(), decodeAll(t, d), want)
}

// TestThePlatformDecoderIsUsedWhenItIsAvailable 盯反面：**平台解码器能用的时候
// 不落回**（macOS 的行为不许变）。
//
// 断的不只是容器名：还得是**平台那个解码器本身**。只看容器名的话，一个
// 「先试平台、失败了再悄悄落回」的实现也能过——而那正是「落回只发生在平台
// 明确不可用时」这条规矩被破坏时的样子。
func TestThePlatformDecoderIsUsedWhenItIsAvailable(t *testing.T) {
	fake := &platformFakeDecoder{}
	stubPlatformDecoder(t, func(io.ReadSeeker) (mygoaudio.Decoder, error) {
		return fake, nil
	})

	d, name, err := openDecoder(bytes.NewReader(fixture(t, "stereo16.mp4")))
	if err != nil {
		t.Fatalf("认不出容器：%v", err)
	}
	if name != "mp4" {
		t.Errorf("容器名 = %q，want mp4（平台解码器能用的时候不落回）", name)
	}
	if d != mygoaudio.Decoder(fake) {
		t.Errorf("用的不是平台解码器，而是 %T——平台解码器成功时不该落回 flacdec", d)
	}
}

// TestOpenDecoderDoesNotFallBackWhenTheFileItselfIsBroken 盯「**文件坏了**
// 要照旧报错」那一条。
//
// 平台解码器报的不是「这个平台没有解码器」，而是「这个文件我解不了」——
// 那是**文件**的问题，落回 flacdec 只会把它说成「不像 FLAC」，把真错盖掉。
//
// 所以这里断的是错误**还是不是**平台解码器报的那一个：落回之后报出来的会是
// `flacdec.ErrFormat`（「不像 FLAC」），而这条测试要的是原原本本的那个错。
func TestOpenDecoderDoesNotFallBackWhenTheFileItselfIsBroken(t *testing.T) {
	broken := errors.New("extaf: AudioFileOpenWithCallbacks: OSStatus -50")
	stubPlatformDecoder(t, func(io.ReadSeeker) (mygoaudio.Decoder, error) {
		return nil, broken
	})

	_, _, err := openDecoder(bytes.NewReader(fixture(t, "stereo16.mp4")))
	if err == nil {
		t.Fatal("平台解码器说这个文件解不了，却报成功")
	}
	if !errors.Is(err, broken) {
		t.Errorf("错误 = %v，want %v（平台解码器报的那个错要原样出去）", err, broken)
	}
	if errors.Is(err, flacdec.ErrFormat) {
		t.Errorf("错误 = %v——落回 flacdec 了。「文件解不了」和「平台没解码器」是两回事", err)
	}
}

// TestOpenDecoderSaysBothWhenThePlatformIsUnavailableAndTheFileIsNotFLAC 盯
// **两条路径分得清**：平台没有解码器，而这个文件也不是 FLAC。
//
// 这种情况下两句都要在：一句说「这台机器没有系统解码器」，一句说「这也不像
// FLAC」。只报一句的话，界面上会出现「文件坏了」这种**其实不是**的提示——
// 而按那条提示去查文件，永远查不出问题。
func TestOpenDecoderSaysBothWhenThePlatformIsUnavailableAndTheFileIsNotFLAC(t *testing.T) {
	stubPlatformDecoder(t, func(io.ReadSeeker) (mygoaudio.Decoder, error) {
		return nil, extaf.ErrUnsupported
	})

	_, _, err := openDecoder(bytes.NewReader(buildAACMP4(t)))
	if err == nil {
		t.Fatal("平台没有解码器、文件也不是 FLAC，却报成功")
	}
	if !errors.Is(err, extaf.ErrUnsupported) {
		t.Errorf("错误 = %v，丢了「这个平台没有系统解码器」这一半", err)
	}
	if !errors.Is(err, flacdec.ErrFormat) {
		t.Errorf("错误 = %v，丢了「这也不像 FLAC」这一半", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Media Foundation 那条分支（Windows 上解 AAC 的那条路）
// ─────────────────────────────────────────────────────────────────────────────

// TestOpenDecoderUsesMediaFoundationWhenThePlatformDecoderIsUnavailable 盯
// **Windows 上有损那一档**：平台解码器不可用时，MP4 里的 AAC 要交给 Media
// Foundation。
//
// 这是 Windows 发布阻塞项的那一条路（issue #253）：接不上它，Windows 上
// B 站有损流（64K / 128K / 192K）**一档都放不出来**。
//
// 断的是两件事：
//
//  1. 走的**确实是** Media Foundation 那一个（不是碰巧也返回了一个解码器）；
//  2. **平台解码器能用的时候不走它**（macOS 的行为不许变）。
func TestOpenDecoderUsesMediaFoundationWhenThePlatformDecoderIsUnavailable(t *testing.T) {
	stubPlatformDecoder(t, func(io.ReadSeeker) (mygoaudio.Decoder, error) {
		return nil, extaf.ErrUnsupported
	})
	fake := &platformFakeDecoder{}
	stubMediaFoundation(t, func(io.ReadSeeker) (mygoaudio.Decoder, error) {
		return fake, nil
	})

	d, name, err := openDecoder(bytes.NewReader(buildAACMP4(t)))
	if err != nil {
		t.Fatalf("平台解码器不可用时没走 Media Foundation：%v", err)
	}
	if name != "aac" {
		t.Errorf("容器名 = %q，want aac（走的是 Media Foundation 那条路）", name)
	}
	if d != mygoaudio.Decoder(fake) {
		t.Errorf("用的不是 Media Foundation，而是 %T", d)
	}
}

// TestOpenDecoderDoesNotUseMediaFoundationWhenThePlatformDecoderIsAvailable 盯
// **反面**：macOS 上平台解码器能解码，就**不许**绕道 Media Foundation。
//
// 这条不许有例外：Media Foundation 只在 Windows 上有，而 macOS 上 AudioToolbox
// 什么都解得了。绕过去的话，macOS 的行为就变了（而 macOS 是这个仓库唯一能
// 跑全套测试的平台——行为在那儿变了，别处就没人知道了）。
func TestOpenDecoderDoesNotUseMediaFoundationWhenThePlatformDecoderIsAvailable(t *testing.T) {
	fake := &platformFakeDecoder{}
	stubPlatformDecoder(t, func(io.ReadSeeker) (mygoaudio.Decoder, error) {
		return fake, nil
	})
	stubMediaFoundation(t, func(io.ReadSeeker) (mygoaudio.Decoder, error) {
		t.Error("平台解码器明明能用，却还是去问了 Media Foundation")
		return nil, errors.New("不该走到这里")
	})

	d, name, err := openDecoder(bytes.NewReader(buildAACMP4(t)))
	if err != nil {
		t.Fatalf("认不出容器：%v", err)
	}
	if name != "mp4" {
		t.Errorf("容器名 = %q，want mp4（平台解码器能用的时候不绕道）", name)
	}
	if d != mygoaudio.Decoder(fake) {
		t.Errorf("用的不是平台解码器，而是 %T", d)
	}
}

// TestOpenDecoderFallsBackToFLACWhenMediaFoundationCannotDecodeTheFile 盯
// **Media Foundation 认不出来之后还要落回 flacdec**。
//
// Windows 上 MP4 家族里装的不一定是 AAC：B 站的无损流是「MP4 里的 FLAC」。
// 那一种 Media Foundation 认不出来（它只解 AAC），而**落回 flacdec 之后要照
// 旧能放**——不然 Windows 上连无损那一档都保不住。
func TestOpenDecoderFallsBackToFLACWhenMediaFoundationCannotDecodeTheFile(t *testing.T) {
	stubPlatformDecoder(t, func(io.ReadSeeker) (mygoaudio.Decoder, error) {
		return nil, extaf.ErrUnsupported
	})
	stubMediaFoundation(t, func(io.ReadSeeker) (mygoaudio.Decoder, error) {
		return nil, errors.New("mfadec: 这个 MP4 里不是 AAC（是 flac）")
	})

	d, name, err := openDecoder(bytes.NewReader(fixture(t, "stereo16.mp4")))
	if err != nil {
		t.Fatalf("Media Foundation 认不出 MP4 里的 FLAC，却没有落回 flacdec：%v", err)
	}
	if name != "flac" {
		t.Errorf("容器名 = %q，want flac（落回走的是 flacdec 那条路）", name)
	}
	src := stereo16(t)
	if d.Channels() != src.channels || d.SampleRate() != src.rate {
		t.Errorf("格式 = %d Hz / %d 声道，原始录音是 %d Hz / %d 声道",
			d.SampleRate(), d.Channels(), src.rate, src.channels)
	}
}

// TestOpenDecoderSaysWhyMediaFoundationCouldNotDecodeIt 盯「**Media Foundation
// 的真实原因要进最终那句错**」。
//
// Windows 上**有**系统解码器（Media Foundation），只是它解不了这一个文件
// （E-AC-3、ALAC 都是这种）。所以最终那句错里如果只有「这台机器没有系统
// 解码器」，用户看到的是一句**假话**——而真实原因（「这个 MP4 里不是 AAC」）
// 才是他能拿去做决定的。
//
// 这一条断的是**那句错里有没有真话**。
func TestOpenDecoderSaysWhyMediaFoundationCouldNotDecodeIt(t *testing.T) {
	stubPlatformDecoder(t, func(io.ReadSeeker) (mygoaudio.Decoder, error) {
		return nil, extaf.ErrUnsupported
	})
	// 真实原因长什么样，就用什么形状——别拿一个假的字符串糊弄过去。
	mfErr := errors.New("mfadec: 这个 MP4 里不是 AAC（是 ec-3）")
	stubMediaFoundation(t, func(io.ReadSeeker) (mygoaudio.Decoder, error) {
		return nil, mfErr
	})

	// 素材要挑一个**两个解码器都认不出来**的：`stereo16.mp4` 里装的是 FLAC，
	// 落回 flacdec 之后是能放的（那正是另一条测试要断的）。
	_, _, err := openDecoder(bytes.NewReader(buildAACMP4(t)))
	if err == nil {
		t.Fatal("两个解码器都认不出来，却报成功")
	}
	if !errors.Is(err, mfErr) {
		t.Errorf("错误 = %v，丢了 Media Foundation 的真实原因（%v）", err, mfErr)
	}
	if !errors.Is(err, extaf.ErrUnsupported) {
		t.Errorf("错误 = %v，丢了「没有 AudioToolbox」这一半", err)
	}
	if !errors.Is(err, flacdec.ErrFormat) {
		t.Errorf("错误 = %v，丢了「这也不像 FLAC」这一半", err)
	}
	t.Logf("最终那句错：%v", err)
}

// ─────────────────────────────────────────────────────────────────────────────
// 注入点
// ─────────────────────────────────────────────────────────────────────────────

// stubPlatformDecoder 把「平台解码器」换成测试自己给的，测试结束自动还原。
//
// ⚠️ 换掉的是**包级变量**：所以替换它的测试不能 `t.Parallel`——同包其它测试
// 会看见换掉之后的值（Go 在一个包内默认串行跑测试函数，所以不并行就没事）。
func stubPlatformDecoder(t *testing.T, open func(io.ReadSeeker) (mygoaudio.Decoder, error)) {
	t.Helper()
	old := platformDecoderOpen
	platformDecoderOpen = open
	t.Cleanup(func() { platformDecoderOpen = old })
}

// stubMediaFoundation 把「Media Foundation」换成测试自己给的，测试结束自动还原。
//
// ⚠️ 和 `stubPlatformDecoder` 一样：换掉的是包级变量，所以不能 `t.Parallel`。
func stubMediaFoundation(t *testing.T, open func(io.ReadSeeker) (mygoaudio.Decoder, error)) {
	t.Helper()
	old := mediaFoundationDecoderOpen
	mediaFoundationDecoderOpen = open
	t.Cleanup(func() { mediaFoundationDecoderOpen = old })
}

// platformFakeDecoder 是「平台解码器成功」时的占位解码器。
//
// 它只用来证明「用的确实是平台那一个」，不解任何东西——真解码由 extaf 自己
// 的测试管。
type platformFakeDecoder struct{}

func (platformFakeDecoder) SampleRate() int             { return 44100 }
func (platformFakeDecoder) Channels() int               { return 2 }
func (platformFakeDecoder) Read([]float32) (int, error) { return 0, io.EOF }
func (platformFakeDecoder) SetPosition(int64) error     { return nil }
func (platformFakeDecoder) Len() int64                  { return 0 }

// buildAACMP4 muxes 一小段 AAC-LC 轨道，装在 MP4 容器里——B 站有损流的那种
// 形状。go-m4a 写的。
//
// 用它是因为它是「认得出容器、但里面不是 FLAC」的那一类：格式表会把它认成
// mp4 交给平台解码器，平台解码器不可用时落回 flacdec，而 flacdec 会明确说
// 「这条轨道是 AAC，不是 FLAC」。
//
// ⚠️ 用的是 go-m4a 的**根包**，不是 `aacm4a` 子包——后者链进 LGPL-2.1 的
// `go-aac`，引它就破坏「一行 LGPL 都不用」那条不变量
// （`internal/licensing` 的 TestNoDependencyIsCopyleft 盯着）。
func buildAACMP4(t *testing.T) []byte {
	t.Helper()
	cfg := m4a.WriterConfig{SampleRate: 48000, Channels: 2, ASC: []byte{0x11, 0x90}, EncoderDelay: m4a.NoEdit}
	data, err := m4a.InitSegment(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fw, err := m4a.NewFragmentWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := fw.WriteFrame(make([]byte, 1024)); err != nil {
			t.Fatal(err)
		}
		if data, err = fw.AppendSegment(data); err != nil {
			t.Fatal(err)
		}
	}
	return data
}
