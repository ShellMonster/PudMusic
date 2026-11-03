package localmusic

import (
	"math"
	"path/filepath"
	"testing"
)

// ReplayGain 标签解析：三种容器三条路，断言的是「标签里写的数原样出来」。
// 没有标签的文件（现实里的大多数）必须给 ok=false——不是错，是让调用方
// 退回实时测量。

// flacWithRG 造一个带 ReplayGain 字段的 .flac（同 flacWith 的造法，
// 注释里多塞两条键值）。
func flacWithRG(t *testing.T, path, gain, peak string) string {
	fields := [][]byte{
		[]byte("REPLAYGAIN_TRACK_GAIN=" + gain),
		[]byte("REPLAYGAIN_TRACK_PEAK=" + peak),
	}
	var comment []byte
	comment = leBytes(0)
	comment = append(comment, leBytes(uint32(len(fields)))...)
	for _, f := range fields {
		comment = append(comment, leBytes(uint32(len(f)))...)
		comment = append(comment, f...)
	}
	block := []byte{0x80 | 4}
	block = append(block, byte(len(comment)>>16), byte(len(comment)>>8), byte(len(comment)))
	return put(t, path, append(append([]byte("fLaC"), block...), comment...))
}

func TestFLACReplayGainReadsTrackGainAndPeak(t *testing.T) {
	dir := t.TempDir()
	path := flacWithRG(t, filepath.Join(dir, "歌.flac"), "-7.68 dB", "0.988")

	gain, peak, ok := ReplayGain(path)

	if !ok || math.Abs(gain-(-7.68)) > 0.01 || math.Abs(peak-0.988) > 0.001 {
		t.Errorf("读到 gain=%.2f peak=%.3f ok=%v，want -7.68 / 0.988 / true", gain, peak, ok)
	}
}

func TestFLACReplayGainIsAbsentWhenThereAreNoTags(t *testing.T) {
	dir := t.TempDir()
	path := flacWith(t, filepath.Join(dir, "只有标题.flac"), "标题")

	if _, _, ok := ReplayGain(path); ok {
		t.Error("没有 ReplayGain 标签的文件报成了有")
	}
}

// id3v2Frame 造一个通用 ID3v2.4 帧（4 字节 id + 同步安全长度 + 2 标志 + 正文）。
func id3v2Frame(id string, body []byte) []byte {
	frame := append([]byte(id), syncsafeBytes(len(body))...)
	frame = append(frame, 0, 0)
	return append(frame, body...)
}

// id3v2TagWith 造一个 ID3v2.4 标签，里面装给定的若干个帧。
func id3v2TagWith(frames ...[]byte) []byte {
	var body []byte
	for _, f := range frames {
		body = append(body, f...)
	}
	tag := append([]byte("ID3"), 4, 0, 0)
	tag = append(tag, syncsafeBytes(len(body))...)
	return append(tag, body...)
}

// txxxBody 造 TXXX 帧的正文：编码 3（UTF-8）+ 描述 + NUL + 值。
func txxxBody(desc, value string) []byte {
	return append(append([]byte{3}, []byte(desc)...), append([]byte{0}, []byte(value)...)...)
}

func TestMP3ReplayGainReadsTheTXXXFrames(t *testing.T) {
	dir := t.TempDir()
	tag := id3v2TagWith(
		id3v2Frame("TXXX", txxxBody("replaygain_track_gain", "-6.54 dB")),
		id3v2Frame("TXXX", txxxBody("replaygain_track_peak", "0.912")),
	)
	path := put(t, filepath.Join(dir, "歌.mp3"), append(tag, 0xff, 0xfb))

	gain, peak, ok := ReplayGain(path)

	if !ok || math.Abs(gain-(-6.54)) > 0.01 || math.Abs(peak-0.912) > 0.001 {
		t.Errorf("TXXX 读到 gain=%.2f peak=%.3f ok=%v，want -6.54 / 0.912 / true", gain, peak, ok)
	}
}

// TXXX 优先于 RVA2：两份都写时拿 TXXX 那份（foobar2000 只写 TXXX，mp3gain
// 两份都写——写两份的值理论上一致，不一致时 TXXX 是各工具的新版写法）。
func TestMP3ReplayGainPrefersTXXXOverRVA2(t *testing.T) {
	dir := t.TempDir()
	// RVA2: "track" + NUL + 声道类型(1) + 调整(2, 1/512 dB) + 峰值位数(1) + 峰值。
	// -3.00 dB = -1536/512 → 0xFA00；峰值 16 位 0x8000 → 0.5。
	rva2 := append([]byte("track"), 0)
	rva2 = append(rva2, 1, 0xFA, 0x00, 16, 0x80, 0x00)
	tag := id3v2TagWith(
		id3v2Frame("RVA2", rva2),
		id3v2Frame("TXXX", txxxBody("replaygain_track_gain", "-9.99")),
	)
	path := put(t, filepath.Join(dir, "歌.mp3"), append(tag, 0xff, 0xfb))

	gain, _, ok := ReplayGain(path)

	if !ok || math.Abs(gain-(-9.99)) > 0.01 {
		t.Errorf("两份都写时读到 gain=%.2f，want -9.99（TXXX 优先）", gain)
	}
}

// 只有 RVA2 没有 TXXX 时退 RVA2（老工具写的）。
func TestMP3ReplayGainFallsBackToRVA2(t *testing.T) {
	dir := t.TempDir()
	rva2 := append([]byte("track"), 0)
	rva2 = append(rva2, 1, 0xFA, 0x00, 16, 0x80, 0x00) // -3.00 dB / 0.5
	tag := id3v2TagWith(id3v2Frame("RVA2", rva2))
	path := put(t, filepath.Join(dir, "歌.mp3"), append(tag, 0xff, 0xfb))

	gain, peak, ok := ReplayGain(path)

	if !ok || math.Abs(gain-(-3.0)) > 0.01 || math.Abs(peak-0.5) > 0.01 {
		t.Errorf("RVA2 读到 gain=%.2f peak=%.3f ok=%v，want -3.00 / 0.5 / true", gain, peak, ok)
	}
}

// RVA2 的 album 段不认：那是整张专辑的口径，不是这一首的。
func TestMP3ReplayGainIgnoresTheAlbumRVA2(t *testing.T) {
	dir := t.TempDir()
	rva2 := append([]byte("album"), 0)
	rva2 = append(rva2, 1, 0xFA, 0x00, 16, 0x80, 0x00)
	tag := id3v2TagWith(id3v2Frame("RVA2", rva2))
	path := put(t, filepath.Join(dir, "歌.mp3"), append(tag, 0xff, 0xfb))

	if _, _, ok := ReplayGain(path); ok {
		t.Error("只有 album RVA2 的文件报成了有 track ReplayGain")
	}
}

// m4aWithRG 造一个带 iTunes 自由原子 ReplayGain 的 .m4a：
// `ilst` 下一个 `----`，里面 `mean`/`name`/`data` 三个子原子。
func m4aWithRG(t *testing.T, path, name, value string) string {
	freeform := atom("----",
		atom("mean", []byte{0, 0, 0, 0}, []byte("com.apple.iTunes")),
		atom("name", []byte{0, 0, 0, 0}, []byte(name)),
		atom("data", []byte{0, 0, 0, 1}, []byte{0, 0, 0, 0}, []byte(value)))
	ilst := atom("ilst", freeform)
	meta := atom("meta", []byte{0, 0, 0, 0}, ilst)
	moov := atom("moov", atom("udta", meta))
	out := atom("ftyp", []byte("M4A isom"))
	return put(t, path, append(out, moov...))
}

func TestM4AReplayGainReadsTheFreeformAtom(t *testing.T) {
	dir := t.TempDir()
	path := m4aWithRG(t, filepath.Join(dir, "歌.m4a"), "replaygain_track_gain", "-4.32")

	gain, _, ok := ReplayGain(path)

	if !ok || math.Abs(gain-(-4.32)) > 0.01 {
		t.Errorf("M4A 读到 gain=%.2f ok=%v，want -4.32 / true", gain, ok)
	}
}

// 不是音频后缀的文件根本不读（.txt 里写什么也没用）。
func TestReplayGainIgnoresUnknownExtensions(t *testing.T) {
	dir := t.TempDir()
	path := put(t, filepath.Join(dir, "歌.txt"), []byte("REPLAYGAIN_TRACK_GAIN=-5"))

	if _, _, ok := ReplayGain(path); ok {
		t.Error("非音频文件报成了有 ReplayGain")
	}
}

// 坏了的标签不算有：声称自己很大实际没那么多的文件，读不出就是读不出。
func TestReplayGainSurvivesATruncatedFLAC(t *testing.T) {
	dir := t.TempDir()
	// VORBIS_COMMENT 块头声称 256 字节，文件只有头。
	path := put(t, filepath.Join(dir, "断.flac"),
		append([]byte("fLaC"), 0x84, 0x01, 0x00, 0x00))

	if _, _, ok := ReplayGain(path); ok {
		t.Error("截断的 FLAC 报成了有 ReplayGain")
	}
}
