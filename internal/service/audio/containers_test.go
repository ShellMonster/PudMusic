package audio

import (
	"os"
	"path/filepath"
	"testing"
)

// 这组测试盯的是「文件头 → 解码器」这条分派有没有接上，不是解码对不对
// （对不对由 extaf 自己的测试管）。
//
// # 素材必须是仓库的一部分，不能「现造」
//
// 这一组以前用系统自带的 afconvert / ffmpeg 现造素材，造不出来就跳过。
// 那样在 **Windows 上整组都是空的**：CI 的 windows-2022 runner 既没有
// afconvert 也没有 ffmpeg，于是每一条都「跳过」——**绿是绿的，但一条也没
// 测到**。而「Windows 上 .m4a 能不能放」恰恰是这一组最该在 Windows 上答的
// 那个问题（Windows 的系统解码器是 Media Foundation，不是 AudioToolbox）。
//
// 所以素材改成仓库自带的那两份（`mfadec/testdata/`）：两个平台跑的是
// **同一段字节**，判据可比，也不依赖任何系统工具。

// TestOpenRoutesMP4ToTheSystemDecoder 确认 .m4a 真的走的是**系统解码器**
// 那条路。
//
// 两个平台的系统解码器不是一个东西，而**都是系统解码器**：
//
//   - macOS：AudioToolbox（`extaf`）。
//   - Windows：Media Foundation（`mfadec`）——Windows 自带的那个。
//
// 所以这条测试在两个平台上断的是同一件事：「.m4a 落到系统解码器上了，而且
// 它报得出时长和采样率」。
func TestOpenRoutesMP4ToTheSystemDecoder(t *testing.T) {
	path := filepath.Join("mfadec", "testdata", "aaclc-stereo.m4a")

	p, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%s): %v", path, err)
	}
	defer p.Close()

	if got := p.Duration(); got <= 0 {
		t.Errorf("时长 = %v，.m4a 应该有时长", got)
	}
	if got := p.dec.SampleRate(); got != 44100 {
		t.Errorf("采样率 = %d，want 44100", got)
	}
}

// TestOpenRoutesRawADTS 盯的是裸 ADTS（没有容器的 .aac）——
// `verify-streaming.md` §1.1 说 macOS 打不开，本线程实测能开，
// 这条测试就是为了不让它哪天悄悄退化回去。
//
// 两个平台各走各的系统解码器（macOS 是 AudioToolbox，Windows 是 Media
// Foundation），但**都得开得了裸 ADTS**。
func TestOpenRoutesRawADTS(t *testing.T) {
	path := filepath.Join("mfadec", "testdata", "aaclc-stereo.aac")

	p, err := Open(path)
	if err != nil {
		t.Fatalf("裸 ADTS 打不开：%v", err)
	}
	defer p.Close()
	if got := p.Duration(); got <= 0 {
		t.Errorf("时长 = %v", got)
	}
}

// TestOpenStillFindsTheOldFormats 是回归：新注册的两个容器不能把原来能开的
// 格式顶掉（MyGo 的格式表是按注册顺序第一个匹配就赢）。
func TestOpenStillFindsTheOldFormats(t *testing.T) {
	mp3 := filepath.Join("testdata", "tone.mp3")
	p, err := Open(mp3)
	if err != nil {
		t.Fatalf("Open(%s): %v", mp3, err)
	}
	defer p.Close()
	if got := p.Duration(); got <= 0 {
		t.Errorf("mp3 的时长 = %v", got)
	}

	// 系统自带的 AIFF：不在 MyGo 的格式表里（我们自己注册的是 FORM????AIFF），
	// 但 ExtAudioFile 也能解。这里只确认它不会因为新注册而报错。
	ping := "/System/Library/Sounds/Ping.aiff"
	if _, err := os.Stat(ping); err != nil {
		t.Skip("这台机器上没有 Ping.aiff")
	}
	q, err := Open(ping)
	if err != nil {
		t.Fatalf("Open(%s): %v", ping, err)
	}
	defer q.Close()
	if got := q.Duration(); got <= 0 {
		t.Errorf("aiff 的时长 = %v", got)
	}
}

func TestOpenRejectsSomethingThatIsNotAudio(t *testing.T) {
	path := filepath.Join(t.TempDir(), "junk.bin")
	if err := os.WriteFile(path, []byte("this is not audio, it is a sentence with words in it"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Open(path)
	if p != nil {
		p.Close()
		t.Fatal("一段文字居然解出来了")
	}
	if err == nil {
		t.Fatal("Open 应该报错")
	}
}

// 这一组没有「现造素材」那一步——素材是仓库自带的两份文件（见顶上那段）。
// 以前这里有 `makeAAC` / `makeTestWAV`（用 afconvert 和 ffmpeg 现造），它们
// 造出来的东西在 Windows 上根本造不出来，于是那一整组在 Windows 上全部跳过。
