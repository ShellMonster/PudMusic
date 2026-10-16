package localmusic

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// 标题是从**文件的标签块**里读的，不是从文件名猜的。
//
// # 为什么专门写一个标签读取器
//
// Biu 那边用的是 `music-metadata` 这个库（`local-music.ts:4`），我们没有。
// 但那一栏不能拿文件名去掉扩展名来凑——那是在说一件假话：用户按「标题」找歌，
// 找的就是文件里写着的那个名字。Biu 的 `toSafeTitle` 也是先标签后文件名
// （`local-music.ts:31-34`），我们照它这个优先级。
//
// # 读哪几种，为什么
//
// | 扩展名 | 读哪儿 | 为什么 |
// |---|---|---|
// | `.mp3` | ID3v2 的 `TIT2`，退回 ID3v1 的 `TAG` | 几乎所有 MP3 都带 |
// | `.m4a` | `moov/udta/meta/ilst/©nam` 的 `data` | iTunes 写的标准位置 |
// | `.flac` | `METADATA_BLOCK_VORBIS_COMMENT` 里的 `TITLE=` | FLAC 只有这一种 |
// | `.wav` | `LIST` → `INFO` → `INAM` | Windows 录音和转码工具写的 |
// | `.aiff/.aif/.aifc` | `FORM` 里的 `NAME` 块 | IFF 规范里的名字块 |
// | `.ogg` / `.wma` | 不读 | Ogg 的标签摊在页结构里、要在 Vorbis 注释里找；WMA 的 ASF 又是另一套。**宁可退回文件名，也不要给一个编出来的标题** |
//
// ⚠️ **没读到就是空串**，由 `trackOf` 退回文件名。这是刻意的：返回一个猜的
// 标题，比明说「这就是文件名」危险得多——用户会以为那是文件里写的。
//
// ⚠️ 这里读的**只有标题**。艺人、专辑、封面那些字段没读，也不打算读：
// 界面上那一页只有「标题」这一栏要它们（`docs/research/PLAN.md` 阶段 8.3），
// 为看不见的字段写解析器是白花的复杂度。

// headRead 是从文件头读多少字节来够找标签。
//
// 够绝大多数真实文件用：ID3v2 允许一个大到离谱的标签（几 MB 的封面），
// FLAC 的注释块也在头��，WAV 的 INFO 通常在 fmt 之后不远。真读到了边界之外
// 就是这一份读不出来 —— 退回文件名，不报错。
const headRead = 4 << 20 // 4 MiB

// Title 读文件里的标题标签。读不出来返回空串（调用方退回文件名）。
func Title(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return mp3Title(path)
	case ".m4a", ".m4s":
		return mp4Title(path)
	case ".flac":
		return flacTitle(readHeadOrNil(path))
	case ".wav":
		return riffTitle(path, false)
	case ".aiff", ".aif", ".aifc":
		return riffTitle(path, true)
	}
	return ""
}

// readHead 读文件开头的一小段。读不满就是读不满（文件本来就这么短），
// 不是错。
func readHead(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, headRead)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return buf[:n], nil
}

// cut 解一个四字节的类型名。少四个字节就返回空串。
func cut(b []byte, n int) string {
	if len(b) < n {
		return ""
	}
	return string(b[:n])
}

// ─────────────────────────────────────────────────────────────────────────────
// MP3：ID3v2 → ID3v1
// ─────────────────────────────────────────────────────────────────────────────

// mp3Title 先找 ID3v2 的 TIT2，没有再看文件末尾的 ID3v1。
//
// 两段都要，因为现实里两段都有：老工具写的 ID3v1 不会被后来写 ID3v2 的工具
// 清掉（B 站、网易云都只往前写）。而只有 ID3v1 的文件也多得是。
func mp3Title(path string) string {
	if head, err := readHead(path); err == nil {
		if s := id3v2Title(head); s != "" {
			return s
		}
	}
	return id3v1Title(path)
}

// id3v2Title 从 ID3v2 标签里取标题帧。
func id3v2Title(b []byte) string {
	// 头 10 字节：'ID3' + 版本(2) + 标志(1) + 大小(4，同步安全整数)
	if len(b) < 10 || cut(b, 3) != "ID3" {
		return ""
	}
	major := b[3]
	flags := b[5]
	size := int(syncsafe(b[6:10]))
	// 头部之后有多余的整块（扩展头）时跳过去。
	start := 10
	if flags&0x40 != 0 && major >= 3 { // 扩展头
		if start+4 > len(b) {
			return ""
		}
		ext := int(binary.BigEndian.Uint32(b[start : start+4]))
		start += 4 + ext
	}
	end := min(start+size, len(b))

	// v2.2 的帧号是 3 字节，帧大小 3 字节；v2.3 是 4+4；v2.4 的帧大小
	// 变回同步安全整数。
	idLen, sizeLen := 4, 4
	if major == 2 {
		idLen, sizeLen = 3, 3
	}

	for p := start; p+idLen+sizeLen <= end; {
		id := string(b[p : p+idLen])
		var fsize int
		if major >= 4 {
			fsize = int(syncsafe(b[p+idLen : p+idLen+sizeLen]))
		} else {
			fsize = int(bigEndian(b[p+idLen:p+idLen+sizeLen], sizeLen))
		}
		p += idLen + sizeLen + 2 // 帧标志两字节
		if fsize <= 0 || p+fsize > end {
			break // 尺寸不合理：从这里往后都是不可信的，停下来
		}
		// TIT2 是 v2.3+ 的标题帧，v2.2 里它叫 TT2。
		if (major >= 3 && id == "TIT2") || (major == 2 && id == "TT2") {
			return decodeText(b[p : p+fsize])
		}
		p += fsize
	}
	return ""
}

// id3v1Title 从文件**末尾** 128 字节的 ID3v1 里取标题。
//
// ID3v1 的布局是死的：'TAG' 之后 30 字节标题、30 字节艺人、30 字节专辑、
// 4 字节年份、30 字节注释、1 字节音轨。末尾不足 128 字节的文件不可能有它。
func id3v1Title(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() < 128 {
		return ""
	}
	var buf [128]byte
	if _, err := f.ReadAt(buf[:], info.Size()-128); err != nil {
		return ""
	}
	if cut(buf[:], 3) != "TAG" {
		return ""
	}
	return trimNul(string(buf[3:33]))
}

// ─────────────────────────────────────────────────────────────────────────────
// MP4 / M4A：moov → udta → meta → ilst → ©nam → data
// ─────────────────────────────────────────────────────────────────────────────

// mp4Title 在 MP4 的原子树里找 `©nam`（iTunes 的标题原子）。
//
// ⚠️ **`moov` 常常在文件末尾**：B 站和各类转码工具为了边下边播都把它放在
// 最后。所以这一段用 `ReadAt` 按需跳读，而不是把整个文件读进内存——一个
// 100MB 的 m4a 只为了一个标题全读进来是荒唐的。
func mp4Title(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	var hdr [8]byte
	if _, err := f.ReadAt(hdr[:], 0); err != nil {
		return ""
	}
	// 正常 MP4 的第一个原子是 ftyp，从它后面开始找 moov。
	if cut(hdr[4:], 4) != "ftyp" {
		return ""
	}
	return mp4Name(f, 0, info.Size(), 0, 0)
}

// mp4Name 在 [start, end) 这一层的原子里找 `©nam`，找到就解出里面的 data。
//
// depth 是递归层数上限。MP4 的原子树最多四层（moov/udta/meta/ilst），
// 给 8 是留足余量，同时挡住一个畸形文件把自己引成无限深。
func mp4Name(f *os.File, start, end int64, skip int64, depth int) string {
	if depth > 8 {
		return ""
	}
	// meta 原子在头之后还有 4 字节的 version/flags（不是 0 的话再多 4 字节）。
	// 由调用方按跳过的原子告诉这里。
	for p := start + skip; p+8 <= end; {
		var hdr [16]byte
		if _, err := f.ReadAt(hdr[:8], p); err != nil {
			return ""
		}
		size := int64(binary.BigEndian.Uint32(hdr[0:4]))
		typ := cut(hdr[4:], 4)
		body := p + 8
		switch size {
		case 0:
			size = end - p // 「一直到文件末尾」
		case 1:
			// 64 位长度：头后面再跟 8 字节。
			if _, err := f.ReadAt(hdr[:], p); err != nil {
				return ""
			}
			size = int64(binary.BigEndian.Uint64(hdr[8:16]))
			body = p + 16
		}
		if size < body-p || p+size > end {
			return "" // 尺寸不自洽 / 越界：这份文件我们读不懂
		}
		if typ == "\xa9nam" { // '©nam' 的 UTF-8 字节就是 0xA9 'n' 'a' 'm'
			return mp4Data(f, body, p+size)
		}
		// ⚠️ 往下走必须传 **body**（p+8），不是 p：传 p 的话这一层又把同一个
		// 容器读一遍，再往下递归同一个参数——一层套一层，直到 depth 用完返回
		// 空串。症状是「所有 m4a 都读不出标题」，而且栈看起来完全正常。
		switch typ {
		case "moov", "udta", "trak", "mdia", "ilst":
			// ilst 的子原子就是各个标签（©nam / ©ART / …）。
			if s := mp4Name(f, body, p+size, 0, depth+1); s != "" {
				return s
			}
		case "meta":
			// meta 的头之后多 4 字节 version/flags（QuickTime 老格式是 8 字节），
			// 两个偏移都试一遍。
			if s := mp4Name(f, body, p+size, 4, depth+1); s != "" {
				return s
			}
			if s := mp4Name(f, body, p+size, 8, depth+1); s != "" {
				return s
			}
		}
		p += size
	}
	return ""
}

// mp4Data 解 `©nam` 里面那个 `data` 原子：8 字节头之后是 4 字节类型
// （1 = UTF-8 文本）、4 字节区域码，然后才是字符串。
func mp4Data(f *os.File, start, end int64) string {
	var hdr [8]byte
	if _, err := f.ReadAt(hdr[:], start); err != nil {
		return ""
	}
	if cut(hdr[4:], 4) != "data" {
		return ""
	}
	size := int64(binary.BigEndian.Uint32(hdr[0:4]))
	body := start + 16 // 8 头 + 4 类型 + 4 区域码
	if size < 16 || body+size-16 > end {
		return ""
	}
	n := size - 16
	if n > 4096 {
		n = 4096 // 标题不该有这么长；截断而不是让一个畸形文件撑爆内存
	}
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, body); err != nil {
		return ""
	}
	return trimNul(string(buf))
}

// ─────────────────────────────────────────────────────────────────────────────
// FLAC：METADATA_BLOCK_VORBIS_COMMENT 里的 TITLE=
// ─────────────────────────────────────────────────────────────────────────────

// flacTitle 在 FLAC 的元信息块里找 Vorbis 注释中的 `TITLE=`。
//
// 结构是死的：`fLaC` 之后每个元信息块的头是一个字节（最高位 = 最后一块，
// 其余 7 位 = 块类型，4 = VORBIS_COMMENT）+ 3 字节长度。注释块的内容是
// 4 字节小端厂商串长度 + 厂商串 + 4 字节条目数 + 每条「4 字节长度 + 内容」。
func flacTitle(b []byte) string {
	if cut(b, 4) != "fLaC" {
		return ""
	}
	for p := 4; p+4 <= len(b); {
		last := b[p]&0x80 != 0
		typ := b[p] & 0x7f
		size := int(uint(b[p+1])<<16 | uint(b[p+2])<<8 | uint(b[p+3]))
		p += 4
		if size < 0 || p+size > len(b) {
			return ""
		}
		if typ == 4 {
			return vorbisTitle(b[p : p+size])
		}
		p += size
		if last {
			break
		}
	}
	return ""
}

// vorbisTitle 从一段 Vorbis 注释里取 `TITLE=`。
//
// 键的大小写不敏感（规范说字段名是大写，但现实里有小写的），而 `TITLE`
// 不能按前缀找（`TITLESORT` 那类字段得排除）。**值的大小写要原样留着**。
func vorbisTitle(b []byte) string {
	p := 0
	vendorLen := le32(b, p)
	p += 4 + vendorLen
	if vendorLen < 0 || p > len(b) {
		return ""
	}
	count := le32(b, p)
	p += 4
	if count < 0 || count > 1<<20 {
		return ""
	}
	for i := 0; i < count && p+4 <= len(b); i++ {
		n := le32(b, p)
		p += 4
		if n < 0 || p+n > len(b) {
			return ""
		}
		field := string(b[p : p+n])
		p += n
		// ⚠️ 只把**键**折成小写。整条折掉的话值也跟着折了——
		// `TITLE=Hello World` 会变成 `hello world`（见
		// TestVorbisCommentKeepsTheValueCase）。
		key, value, ok := strings.Cut(field, "=")
		if !ok || !strings.EqualFold(key, "TITLE") {
			continue
		}
		return trimNul(value)
	}
	return ""
}

// ─────────────────────────────────────────────────────────────────────────────
// WAV / AIFF：块列表里的 NAME / INAM
// ─────────────────────────────────────────────────────────────────────────────

// riffTitle 在 RIFF（WAV，小端）和 IFF（AIFF，大端）的块列表里找标题块。
//
// 两种容器的块列表结构一样——4 字节类型 + 4 字节长度（只是大小端不同），
// 长度是奇数时后面还跟一个补齐字节——所以一个函数带一个开关就够。
//
// WAV 的标题在 `LIST` → `INFO` → `INAM`，AIFF 的直接在 `NAME`。
func riffTitle(path string, bigEndian bool) string {
	b, err := readHead(path)
	if err != nil {
		return ""
	}
	if bigEndian {
		if cut(b, 4) != "FORM" {
			return ""
		}
	} else if cut(b, 4) != "RIFF" || cut(b[8:], 4) != "WAVE" {
		return ""
	}
	u32 := littleEndianU32
	if bigEndian {
		u32 = binary.BigEndian.Uint32
	}
	// 前 8 字节是 'RIFF' + 总长；AIFF 是 'FORM' + 总长。块列表从 12 起。
	for p := 12; p+8 <= len(b); {
		typ := cut(b[p:], 4)
		size := int(u32(b[p+4 : p+8]))
		body := p + 8
		if size < 0 || body+size > len(b) {
			return ""
		}
		switch {
		case !bigEndian && typ == "LIST":
			if cut(b[body:], 4) != "INFO" {
				break
			}
			// INFO 里面又是一份块列表，从 body+4 起。
			if s := infoTitle(b[body+4:body+size], u32); s != "" {
				return s
			}
		case bigEndian && typ == "NAME":
			return trimNul(string(b[body : body+size]))
		}
		p = body + size
		if size%2 == 1 {
			p++ // 补齐字节
		}
	}
	return ""
}

// infoTitle 在 `LIST`/`INFO` 里找 `INAM`（名字）。
func infoTitle(b []byte, u32 func([]byte) uint32) string {
	for p := 0; p+8 <= len(b); {
		typ := cut(b[p:], 4)
		size := int(u32(b[p+4 : p+8]))
		body := p + 8
		if size < 0 || body+size > len(b) {
			return ""
		}
		if typ == "INAM" {
			return trimNul(string(b[body : body+size]))
		}
		p = body + size
		if size%2 == 1 {
			p++
		}
	}
	return ""
}

// ─────────────────────────────────────────────────────────────────────────────
// 小工具
// ─────────────────────────────────────────────────────────────────────────────

// syncsafe 解 ID3 的「同步安全整数」：7 位一组，最高位恒为 0。
//
// 比普通大端小 128 倍，换来的是标签里不会出现假的长度——一个声称自己 2GB 的
// 标签不会让我们去读 2GB。返回 int（上限 256MB，够任何真实标签）。
func syncsafe(b []byte) uint32 {
	if len(b) < 4 {
		return 0
	}
	return uint32(b[0]&0x7f)<<21 | uint32(b[1]&0x7f)<<14 |
		uint32(b[2]&0x7f)<<7 | uint32(b[3]&0x7f)
}

// bigEndian 解 n 字节大端整数（n 是 3 或 4）。
func bigEndian(b []byte, n int) uint32 {
	var v uint32
	for _, c := range b[:min(n, len(b))] {
		v = v<<8 | uint32(c)
	}
	return v
}

// le32 解 4 字节小端整数。越界返回 -1（一律当「尺寸不可信」处理）。
func le32(b []byte, off int) int {
	if off < 0 || off+4 > len(b) {
		return -1
	}
	v := binary.LittleEndian.Uint32(b[off : off+4])
	if v > 1<<30 {
		return -1
	}
	return int(v)
}

func littleEndianU32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }

// decodeText 解 ID3 文本帧的内容。
//
// 第一字节是编码：0 = ISO-8859-1、1 = UTF-16 带 BOM、2 = UTF-16BE、
// 3 = UTF-8。后面还有别的（帧头里的压缩/分组标志）不在我们支持的范围内，
// 遇到就当没有标签。
func decodeText(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	switch b[0] {
	case 0:
		return latin1(b[1:])
	case 1:
		if len(b) < 3 {
			return ""
		}
		return decodeUTF16(b[1:], true)
	case 2:
		return decodeUTF16(b[1:], false)
	case 3:
		return string(b[1:])
	}
	return ""
}

// decodeUTF16 解 UTF-16。带 BOM 时看头两个字节判大小端。
//
// 标签里的歌名十有八九带一个 UTF-16 BOM（写标签的库默认行为），所以这一路
// 比 UTF-8 那条忙得多。
func decodeUTF16(b []byte, expectBOM bool) string {
	big := false
	if expectBOM && len(b) >= 2 {
		switch {
		case b[0] == 0xFF && b[1] == 0xFE:
			b, big = b[2:], false
		case b[0] == 0xFE && b[1] == 0xFF:
			b, big = b[2:], true
		}
	}
	if len(b) < 2 {
		return ""
	}
	if len(b)%2 == 1 {
		b = b[:len(b)-1] // 落单的那个字节不是字符
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		if big {
			units[i] = binary.BigEndian.Uint16(b[i*2:])
		} else {
			units[i] = binary.LittleEndian.Uint16(b[i*2:])
		}
	}
	return trimNul(string(utf16.Decode(units)))
}

// latin1 把 ISO-8859-1 变成 UTF-8。
//
// 直接 `string(b)` 出来的是一堆非法 UTF-8 字节，界面上画出来是乱码。
// 8859-1 的高位字节一对一对应 U+0080~U+00FF，逐个映射就行。
func latin1(b []byte) string {
	ascii := true
	for _, c := range b {
		if c >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return string(b)
	}
	rs := make([]rune, len(b))
	for i, c := range b {
		rs[i] = rune(c)
	}
	return string(rs)
}

// trimNul 去掉标签里那些填充用的 NUL、UTF-8 BOM 和两端空白。
//
// 固定长度字段（ID3v1 的 30 字节、FLAC/MP4 的定长 data）是用 NUL 填满的，
// 不去掉的话歌名后面挂着一串看不见的字符，复制粘贴出来就带上了。BOM 是
// UTF-16 标签被某个工具转成 UTF-8 之后留下的。
func trimNul(s string) string {
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(strings.TrimPrefix(s, "\ufeff"))
}
