package mfadec

// 拆帧那一层的测试。这一层**没有 Windows 也能跑**——它只碰 MP4 容器和
// ADTS 头，不碰 COM。所以它在本机（macOS）上就钉住了两件事：
//
//   - 两份素材（.m4a 和 .aac）装的是**同一批** AAC 帧——于是「用哪条路解」
//     得到的 PCM 必须一样，而这一条是后面那个逐样本比对的前提。
//   - `MF_MT_USER_DATA` 那 12 个字节的排布对不对。
//
// ⚠️ 这些是**前提**，不是结论。「解出来的 PCM 对不对」要在 Windows 真机上
// 才测得出来（`spike_windows_test.go`）。

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// readFixture 读 testdata 里的一个文件。
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("读不到素材 %s：%v", name, err)
	}
	return b
}

// TestTheTwoFixturesCarryTheSameAACFrames 断 .m4a 和 .aac 里是同一批帧。
//
// 素材是同一个 WAV 用同一套 ffmpeg 参数编出来的，只是一个装进 MP4、
// 一个装成裸 ADTS。所以 **AAC 帧本身必须逐字节相同**。
//
// 这条是后面那个「逐样本比对」的前提：两条解码路（裸 AAC 和 ADTS）解出来
// 的 PCM 应当一致——如果帧都不一样，那两条路解出来的差异就分不清是
// 「解码器的差异」还是「素材的差异」。
func TestTheTwoFixturesCarryTheSameAACFrames(t *testing.T) {
	m4aFrames := framesOfM4A(t, "aaclc-stereo.m4a")
	adtsFrames := framesOfADTS(t, "aaclc-stereo.aac")

	if len(m4aFrames) != len(adtsFrames) {
		t.Fatalf(".m4a 有 %d 帧，.aac 有 %d 帧", len(m4aFrames), len(adtsFrames))
	}
	if len(m4aFrames) == 0 {
		t.Fatal("一帧都没拆出来")
	}
	for i := range m4aFrames {
		// .aac 的帧**带** 7 字节的 ADTS 头，.m4a 的帧是裸的——比的是载荷。
		raw := adtsFrames[i]
		if len(raw) < 7 {
			t.Fatalf("第 %d 帧只有 %d 字节，装不下 ADTS 头", i, len(raw))
		}
		if !bytes.Equal(m4aFrames[i], raw[7:]) {
			t.Fatalf("第 %d 帧不一样：\n m4a: %d 字节 %x\n aac: %d 字节（去掉 7 字节头）%x",
				i, len(m4aFrames[i]), m4aFrames[i][:min(16, len(m4aFrames[i]))],
				len(raw)-7, raw[7:][:min(16, len(raw)-7)])
		}
	}
	t.Logf("两份素材的 %d 帧 AAC 载荷逐字节相同", len(m4aFrames))
}

// TestTheADTSFramesAreOneAACFrameEach 断 ADTS 的每一帧里恰好一个 AAC 帧。
//
// 文档说「For raw AAC, each input sample must contain exactly one full AAC
// compressed frame」——喂错帧数的话，MFT 会解出错的 PCM，而症状是
// 「听起来有点怪」，不是报错。
func TestTheADTSFramesAreOneAACFrameEach(t *testing.T) {
	frames := framesOfADTS(t, "aaclc-stereo.aac")
	if len(frames) == 0 {
		t.Fatal("一帧都没拆出来")
	}
	for i, f := range frames {
		info, err := ParseADTS(f)
		if err != nil {
			t.Fatalf("第 %d 帧不是 ADTS：%v", i, err)
		}
		if info.FrameLen != len(f) {
			t.Errorf("第 %d 帧的头说 %d 字节，实际拆出来 %d 字节", i, info.FrameLen, len(f))
		}
	}
	t.Logf("%d 帧 ADTS，头里的帧长和实际长度都对得上", len(frames))
}

// TestAscFromADTSMatchesTheFixture 断从 ADTS 头拼出来的 ASC 是对的。
//
// AAC-LC 的 ASC 是 2 字节：audioObjectType(5) = 2、samplingFrequencyIndex(4)、
// channelConfiguration(4)、三个 0。
func TestAscFromADTSMatchesTheFixture(t *testing.T) {
	head := readFixture(t, "aaclc-stereo.aac")
	info, err := ParseADTS(head)
	if err != nil {
		t.Fatal(err)
	}
	asc, err := ascFromADTS(info)
	if err != nil {
		t.Fatal(err)
	}
	if len(asc) != 2 {
		t.Fatalf("ASC %d 字节，AAC-LC 应该是 2 个", len(asc))
	}
	// 00010 0100 0010 000 = AAC-LC(2) / 44100(索引 4) / 立体声(2)
	want := []byte{0x12, 0x10}
	if !bytes.Equal(asc, want) {
		t.Errorf("ASC = %x，want %x", asc, want)
	}
	// 反过来读一遍：objectType 必须是 2（AAC-LC）。
	if got := asc[0] >> 3; got != 2 {
		t.Errorf("ASC 里的 audioObjectType = %d，want 2（AAC-LC）", got)
	}
}

// TestHeaacUserDataIsTheTwelveBytesAfterWAVEFORMATEX 断 `MF_MT_USER_DATA`
// 的**第一种**形状：去掉 WAVEFORMATEX 之后的那 12 字节 + ASC。
//
// SDK 的 `um/mmreg.h` 里 `HEAACWAVEINFO` 是 `WAVEFORMATEX`（18 字节）加上
// 12 字节，而微软的「AAC Decoder」页面说 `MF_MT_USER_DATA` 这一栏是
// **去掉 WAVEFORMATEX 之后**的那 12 字节，紧跟着 AudioSpecificConfig()。
//
// 抄错的症状是「解码器不认这个媒体类型」——一个说不清的 E_INVALIDARG。
func TestHeaacUserDataIsTheTwelveBytesAfterWAVEFORMATEX(t *testing.T) {
	asc := []byte{0x11, 0x90}
	got := heaacTail(aacPayloadRaw, asc)
	if len(got) != 12+len(asc) {
		t.Fatalf("MF_MT_USER_DATA %d 字节，应该是 12 + ASC(%d)", len(got), len(asc))
	}
	// wPayloadType
	if v := uint16(got[0]) | uint16(got[1])<<8; v != aacPayloadRaw {
		t.Errorf("wPayloadType = %d，want %d", v, aacPayloadRaw)
	}
	// wAudioProfileLevelIndication：0xFE = 「没指定」。
	if v := uint16(got[2]) | uint16(got[3])<<8; v != 0x00FE {
		t.Errorf("wAudioProfileLevelIndication = %#x，want 0x00FE", v)
	}
	// wStructType：0 = 后面跟的是 AudioSpecificConfig()。
	if v := uint16(got[4]) | uint16(got[5])<<8; v != 0 {
		t.Errorf("wStructType = %d，want 0", v)
	}
	// wReserved1 和 dwReserved2：SDK 说「set these to zero」。
	for i, b := range got[6:12] {
		if b != 0 {
			t.Errorf("保留字节 got[%d] = %#x，want 0", 6+i, b)
		}
	}
	// 紧跟着的是 ASC。
	if !bytes.Equal(got[12:], asc) {
		t.Errorf("ASC 部分 = %x，want %x", got[12:], asc)
	}
}

// TestHeaacFormatBlockIsTheWholeWAVEFORMAT 断 `MF_MT_USER_DATA` 的**第二种**
// 形状：整个 `HEAACWAVEFORMAT`（WAVEFORMATEX + 那 12 字节 + ASC）。
//
// `um/mmreg.h` 描述的格式块是这一种，而解码器是靠 `wfx.cbSize` 找 ASC 的
// 位置的（`dwASCLen = cbSize - sizeof(HEAACWAVEINFO) + sizeof(WAVEFORMATEX)`）——
// `cbSize` 就在 `WAVEFORMATEX` 里面。
//
// ⚠️ **两处对不上**：微软的 AAC Decoder 页面说这一栏是「去掉 WAVEFORMATEX 的
// 那一段」，`um/mmreg.h` 说的是整个格式块。哪一处对是**这台机器上的事实**，
// 所以两种都试（`inputVariants`），这里只把两种形状各自的字节钉住。
func TestHeaacFormatBlockIsTheWholeWAVEFORMAT(t *testing.T) {
	asc := []byte{0x11, 0x90}
	got := heaacFormatBlock(44100, 2, aacPayloadRaw, asc)
	if len(got) != 18+12+len(asc) {
		t.Fatalf("HEAACWAVEFORMAT %d 字节，应该是 18 + 12 + ASC(%d)", len(got), len(asc))
	}
	// WAVEFORMATEX：
	if v := uint16(got[0]) | uint16(got[1])<<8; v != waveFormatMPEGHEAAC {
		t.Errorf("wFormatTag = %#x，want %#x", v, waveFormatMPEGHEAAC)
	}
	if v := uint16(got[2]) | uint16(got[3])<<8; v != 2 {
		t.Errorf("nChannels = %d，want 2", v)
	}
	if v := uint32(got[4]) | uint32(got[5])<<8 | uint32(got[6])<<16 | uint32(got[7])<<24; v != 44100 {
		t.Errorf("nSamplesPerSec = %d，want 44100", v)
	}
	// nBlockAlign = 1，wBitsPerSample = 0（SDK 头文件里写死的）。
	if v := uint16(got[12]) | uint16(got[13])<<8; v != 1 {
		t.Errorf("nBlockAlign = %d，want 1", v)
	}
	// cbSize = 12 + ASC 的长度。
	if v := uint16(got[16]) | uint16(got[17])<<8; int(v) != 12+len(asc) {
		t.Errorf("cbSize = %d，want %d", v, 12+len(asc))
	}
	// 后面是那 12 字节 + ASC。
	if !bytes.Equal(got[18:], heaacTail(aacPayloadRaw, asc)) {
		t.Errorf("后面那截不是 HEAACWAVEINFO 的尾巴 + ASC")
	}
}

// framesOfM4A 拆 .m4a 里的裸 AAC 帧。
func framesOfM4A(t *testing.T, name string) [][]byte {
	t.Helper()
	src, err := openM4A(bytes.NewReader(readFixture(t, name)))
	if err != nil {
		t.Fatal(err)
	}
	var out [][]byte
	for {
		f, err := src.next()
		if err != nil {
			break
		}
		out = append(out, f)
	}
	return out
}

// framesOfADTS 拆 .aac 里的帧（**带** ADTS 头）。
func framesOfADTS(t *testing.T, name string) [][]byte {
	t.Helper()
	src, err := openADTS(bytes.NewReader(readFixture(t, name)))
	if err != nil {
		t.Fatal(err)
	}
	var out [][]byte
	for {
		f, err := src.next()
		if err != nil {
			break
		}
		out = append(out, f)
	}
	return out
}
