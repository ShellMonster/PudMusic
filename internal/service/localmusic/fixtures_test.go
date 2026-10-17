package localmusic

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// 测试用的音频文件**都是现搭的**：这一层要验的是「从文件的哪些字节里读标题」，
// 而不是一个真 MP3 的声音对不对。所以每个 fixture 只写标签块 + 几个字节的
// 填充，不写任何音频帧——真音频那一半由 `audio` 包的测试管（那边有真的
// testdata/tone.mp3 和 flacdec/testdata/*.flac）。
//
// 现搭的好处有两个：一是「标签在第几个字节」这件事完全在我们手里，
// 能专门造出「moov 在文件末尾」这种真实 mp4 里最常见的形状；二是加一种
// 编码只要加一个参数，不用去找一个带那种标签的歌。

// put 写一个文件，父目录会自己建。
func put(t *testing.T, path string, data []byte) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// ── MP3 ─────────────────────────────────────────────────────────────────────

// syncsafeBytes 把 n 写成 ID3 的同步安全整数（7 位一组）。
func syncsafeBytes(n int) []byte {
	return []byte{
		// 7 位一组：>>21 / >>14 / >>7 / >>0。写成 >>8 就错了一格，
		// 而且 n < 128 时恰好一样——所以只有长标签才暴露得出来。
		byte(n>>21) & 0x7f, byte(n>>14) & 0x7f, byte(n>>7) & 0x7f, byte(n) & 0x7f,
	}
}

// id3v2Tag 造一个 v2.4 的 ID3 标签，里面带一个 TIT2 标题帧。
//
// v4 的帧长是同步安全整数（v3 是普通大端）——这里只造 v4，v3 那条路由
// TestID3v3FramesUseAPlainSize 单独钉。
func id3v2Tag(major byte, title string, enc byte, syncsafeSizes bool) []byte {
	return id3v2Body(major, []byte(title), enc, syncsafeSizes)
}

// id3v2Body 是 id3v2Tag 的原字形：标题**已经是字节**。
//
// 编码 1（UTF-16）那条路必须这么造——UTF-16 的字节没法从 Go 字符串直接
// 变出来（BOM 和代理对是编码之后才有的）。
func id3v2Body(major byte, text []byte, enc byte, syncsafeSizes bool) []byte {
	body := append([]byte{enc}, text...)
	var sizeBytes []byte
	if syncsafeSizes {
		sizeBytes = syncsafeBytes(len(body))
	} else {
		sizeBytes = make([]byte, 4)
		binary.BigEndian.PutUint32(sizeBytes, uint32(len(body)))
	}
	frame := append([]byte("TIT2"), sizeBytes...)
	frame = append(frame, 0, 0) // 帧标志
	frame = append(frame, body...)

	tag := append([]byte("ID3"), major, 0, 0)
	tag = append(tag, syncsafeBytes(len(frame))...)
	return append(tag, frame...)
}

// mp3With 造一个带 ID3v2 标题的 .mp3。
func mp3With(t *testing.T, path, title string) string {
	return put(t, path, append(id3v2Tag(4, title, 3, true), 0xff, 0xfb, 0x90, 0x00))
}

// mp3WithID3v1 造一个只有 ID3v1 标题的 .mp3（末尾 128 字节那块）。
func mp3WithID3v1(t *testing.T, path, title string) string {
	var tag [128]byte
	copy(tag[:3], "TAG")
	copy(tag[3:33], title)
	data := append([]byte{0xff, 0xfb, 0x90, 0x00}, tag[:]...)
	return put(t, path, data)
}

// ── MP4 / M4A ───────────────────────────────────────────────────────────────

// atom 造一个 MP4 原子：4 字节大端长度 + 4 字节类型 + 内容。
func atom(typ string, payload ...[]byte) []byte {
	size := 8
	for _, p := range payload {
		size += len(p)
	}
	out := make([]byte, 8, size)
	binary.BigEndian.PutUint32(out[:4], uint32(size))
	copy(out[4:8], typ)
	for _, p := range payload {
		out = append(out, p...)
	}
	return out
}

// m4aWith 造一个带 `©nam` 标题的 .m4a。
//
// ftyp 之后可以插任意多个无关原子——真实 mp4 里 `moov` 常在文件末尾，
// mp4Title 必须能跳过前面的东西找到它。padding 就是用来逼这条路的。
func m4aWith(t *testing.T, path, title string, padding int) string {
	// ⚠️ 这里必须写 "\xa9nam" 而不是 "©nam"：MP4 的这个原子类型是
	// **4 字节** 0xA9 'n' 'a' 'm'，而 Go 源码里的 "©" 是 UTF-8 的两个字节。
	data := atom("\xa9nam", atom("data",
		[]byte{0, 0, 0, 1}, // 类型 1 = UTF-8 文本
		[]byte{0, 0, 0, 0}, // 区域码
		[]byte(title)))
	ilst := atom("ilst", data)
	meta := atom("meta", []byte{0, 0, 0, 0}, ilst) // meta 头后有 4 字节 version/flags
	moov := atom("moov", atom("udta", meta))
	out := atom("ftyp", []byte("M4A isom"))
	if padding > 0 {
		out = append(out, atom("free", make([]byte, padding))...)
	}
	return put(t, path, append(out, moov...))
}

// ── FLAC ────────────────────────────────────────────────────────────────────

// flacWith 造一个带 Vorbis 注释标题的 .flac。
func flacWith(t *testing.T, path, title string) string {
	field := []byte("TITLE=" + title)
	var comment []byte
	comment = leBytes(0)                     // 厂商串长度 0
	comment = append(comment, leBytes(1)...) // 一条注释
	comment = append(comment, leBytes(uint32(len(field)))...)
	comment = append(comment, field...)

	block := []byte{0x80 | 4} // 最高位 = 最后一块；类型 4 = VORBIS_COMMENT
	block = append(block, byte(len(comment)>>16), byte(len(comment)>>8), byte(len(comment)))
	return put(t, path, append(append([]byte("fLaC"), block...), comment...))
}

// leBytes 把 n 写成 4 字节小端。
func leBytes(n uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, n)
	return b
}

// ── WAV / AIFF ──────────────────────────────────────────────────────────────

// wavWith 造一个带 `LIST`/`INFO`/`INAM` 标题的 .wav。
func wavWith(t *testing.T, path, title string) string {
	inam := append([]byte("INAM"), leBytes(uint32(len(title)))...)
	inam = append(inam, []byte(title)...)
	if len(title)%2 == 1 { // 块长度是奇数时要补一个字节
		inam = append(inam, 0)
	}
	info := append([]byte("INFO"), inam...)
	list := append([]byte("LIST"), leBytes(uint32(len(info)))...)
	list = append(list, info...)
	body := append([]byte("WAVE"), list...)
	riff := append([]byte("RIFF"), leBytes(uint32(len(body)))...)
	riff = append(riff, body...)
	return put(t, path, riff)
}

// aiffWith 造一个带 `NAME` 块标题的 .aiff。
func aiffWith(t *testing.T, path, title string) string {
	name := append([]byte("NAME"), beBytes(uint32(len(title)))...)
	name = append(name, []byte(title)...)
	if len(title)%2 == 1 {
		name = append(name, 0)
	}
	body := append([]byte("AIFF"), name...)
	form := append([]byte("FORM"), beBytes(uint32(len(body)))...)
	form = append(form, body...)
	return put(t, path, form)
}

// beBytes 把 n 写成 4 字节大端。
func beBytes(n uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, n)
	return b
}

// ── 扫描用的普通文件 ─────────────────────────────────────────────────────────

// songPath 在 dir 里造一个内容随便的 .mp3。
func songPath(t *testing.T, dir, name string) string {
	t.Helper()
	return put(t, filepath.Join(dir, name), []byte("not really audio"))
}

// titlesOf 把一份结果压成「标题列表」，断言时比逐个字段比短。
func titlesOf(ts []Track) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Title
	}
	return out
}

// pathsOf 同上，压成路径。
func pathsOf(ts []Track) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Path
	}
	return out
}

// ── Ogg ────────────────────────────────────────────────────────────────────

// oggWith 造一个带 Vorbis 注释标题的 .ogg。
//
// 手拼的是一个**真的** Ogg 页：27 字节页头 + 分段表 + 数据。页头里的校验和
// 我们不算（读取那一侧也不验它），其余每���字段都按规范摆。
func oggWith(t *testing.T, path, title string) string {
	// 包 0：识别头 \x01vorbis。
	id := append([]byte{1}, []byte("vorbis")...)
	// 包 1：注释头 \x03vorbis + 注释 + framing 位。
	field := []byte("TITLE=" + title)
	comment := leBytes(0)                    // 厂商串长度 0
	comment = append(comment, leBytes(1)...) // 一条注释
	comment = append(comment, leBytes(uint32(len(field)))...)
	comment = append(comment, field...)
	pkt1 := append([]byte("\x03vorbis"), comment...)
	pkt1 = append(pkt1, 1) // framing 位

	// 分段表：每段最多 255 字节，满 255 表示「这个包还没完」。
	var table, data []byte
	for _, pkt := range [][]byte{id, pkt1} {
		n := len(pkt)
		for n >= 255 {
			table = append(table, 255)
			n -= 255
		}
		table = append(table, byte(n))
		data = append(data, pkt...)
	}

	hdr := []byte("OggS")
	hdr = append(hdr, 0)                  // 版本
	hdr = append(hdr, 2)                  // 头类型：BOS（这一页是流的开头）
	hdr = append(hdr, make([]byte, 8)...) // granule
	hdr = append(hdr, make([]byte, 4)...) // 序列号
	hdr = append(hdr, make([]byte, 4)...) // 页序号
	hdr = append(hdr, make([]byte, 4)...) // 校验和
	hdr = append(hdr, byte(len(table)))
	hdr = append(hdr, table...)

	return put(t, path, append(hdr, data...))
}
