package mfadec

// ADTS 头解析的测试。
//
// 这一层**没有 Windows 也能跑**，所以它在本机上就钉住了拆帧这件事——
// 而拆帧是整条路线的第一站：帧长错一位，后面全错位，而症状是
// 「解出来是噪声」，和「解码器不行」分不开。

import (
	"testing"
)

// TestTheADTSFrameLengthIsTheWholeFrame 断帧长那 13 bit 取对了。
//
// ⚠️ **这一条是踩过坑才写出来的。** 从 BMG-68 接手时，帧长是用
// `binary.BigEndian.Uint16(b[5:7]) & 0x1FFF` 取的——那 16 位里混着缓冲区
// fullness 的位，取出来是个**完全合法、却谁都不是**的数（素材的第一帧
// 取出来 8188，真值 326）。
//
// 判据选的是「按这个长度走完整个文件、**正好停在末尾**」而不是「和某个
// 魔数相等」：前者对**任何**一份 ADTS 文件都成立，而后者只对这一份成立。
func TestTheADTSFrameLengthIsTheWholeFrame(t *testing.T) {
	data := readFixture(t, "aaclc-stereo.aac")

	off := 0
	frames := 0
	for off < len(data) {
		if off+7 > len(data) {
			t.Fatalf("第 %d 帧：还剩 %d 字节，不够一个头", frames, len(data)-off)
		}
		info, err := ParseADTS(data[off:])
		if err != nil {
			t.Fatalf("第 %d 帧（偏移 %d）：%v", frames, off, err)
		}
		if info.FrameLen < 7 {
			t.Fatalf("第 %d 帧：帧长 %d，连头都装不下", frames, info.FrameLen)
		}
		off += info.FrameLen
		frames++
	}
	if off != len(data) {
		t.Errorf("按帧长走完停在 %d，文件一共 %d 字节——多出来 %d 字节",
			off, len(data), len(data)-off)
	}
	t.Logf("按帧长走完 %d 帧，正好停在文件末尾（%d 字节）", frames, off)
}

// TestTheFirstADTSFrameHasTheLengthTheHeaderSays 断第一帧的具体长度。
//
// 上面那条对**任何**文件都成立，这一条钉住这一份素材的具体数字：
// 真值 326 字节（= 7 字节头 + 319 字节载荷）。
func TestTheFirstADTSFrameHasTheLengthTheHeaderSays(t *testing.T) {
	data := readFixture(t, "aaclc-stereo.aac")
	info, err := ParseADTS(data)
	if err != nil {
		t.Fatal(err)
	}
	if info.FrameLen != 326 {
		t.Errorf("第一帧 %d 字节，want 326", info.FrameLen)
	}
	if info.HasCRC {
		t.Error("这份素材的 ADTS 头说带 CRC，但它没有 CRC")
	}
	if info.SampleRate != 44100 || info.Channels != 2 {
		t.Errorf("第一帧说 %d Hz / %d 声道，want 44100 / 2", info.SampleRate, info.Channels)
	}
}

// TestParseADTSRefusesTheWrongHeader 断不是 ADTS 的字节不认。
func TestParseADTSRefusesTheWrongHeader(t *testing.T) {
	for _, b := range [][]byte{
		{},
		{0xFF, 0xF1},
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		{0xFF, 0x00, 0x50, 0x80, 0x28, 0xDF, 0xFC},
	} {
		if _, err := ParseADTS(b); err == nil {
			t.Errorf("ParseADTS(%x) 没有报错", b)
		}
	}
}
