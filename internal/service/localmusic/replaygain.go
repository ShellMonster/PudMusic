package localmusic

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// 音量均衡要用的内嵌响度标签（ReplayGain）。读出来交给播放器当「已量好的
// 响度」，就不再等整首解码那十几秒（见 app/loudness_scan.go、
// audio/normalize.go 的 known 分支）。
//
// # 为什么单独写，不量进 tags.go
//
// tags.go 顶头写死了它的职责：「读的**只有标题**」。响度标签是另一件事——
// 它有三个字段、要解析数值、还有三种完全不同的容器格式，塞进去会把那份
// 「宁缺勿猜」的注释弄浑。所以这份解析器自立门户，只复用它的底层小工具
// （`readHead` / `le32` / `cut` / `oggPackets` 这些同包私有函数）。
//
// # 三种容器，三种写法
//
// | 容器 | 写法 | 字段 |
// |---|---|---|
// | `.flac` | Vorbis 注释块里的 `REPLAYGAIN_TRACK_GAIN=` | 同 `TITLE=` 一张表 |
// | `.mp3` | ID3v2 的 `TXXX`（描述串 `replaygain_track_gain`）或 `RVA2` 帧 | TXXX 优先 |
// | `.m4a`/`.m4s` | `ilst` 里的 `----` 自由原子：`mean=com.apple.iTunes` + `name=replaygain_track_gain` | iTunes 惯例 |
//
// ⚠️ 只取 **track** 那一对。`replaygain_album_gain` 是另一套口径（按整张专辑
// 压平），应用按首放，用专辑值会把本来就轻的那首再压一次。

// ReplayGain 读文件里内嵌的 ReplayGain 标签。
//
// 返回的 gainDB 是「按 ReplayGain 参考电平要调的 dB 数」——负值表示这首
// 比参考响，该压；正值表示轻，该抬。peak 是标签里写的峰值（0..1），标签
// 没写时是 0（「不知道」，调用方按「没有峰值上限」处理）。
//
// 没读到时 ok 为假——**不是错**，是这个文件本来就没写（绝大多数本地文件
// 都没写），调用方退回实时测量。
func ReplayGain(path string) (gainDB, peak float64, ok bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		gainDB, peak, ok = flacReplayGain(readHeadOrNil(path))
	case ".mp3":
		gainDB, peak, ok = mp3ReplayGain(path)
	case ".m4a", ".m4s", ".mp4":
		gainDB, peak, ok = mp4ReplayGain(path)
	}
	return
}

// rgNumber 解标签里的数字：「-7.68 dB」「+3.2」「0.988」都算。尾巴上的
// `dB` 和空白剥掉，剩下的交给 ParseFloat——解不出来就是 0 加「没读到」。
func rgNumber(s string) (float64, bool) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.ToUpper(strings.TrimSpace(s)), "DB"))
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}

// ─────────────────────────────────────────────────────────────────────────────
// FLAC：Vorbis 注释里的 REPLAYGAIN_TRACK_*
// ─────────────────────────────────────────────────────────────────────────────

// flacReplayGain 走 `flacTitle` 的同一条路找 Vorbis 注释块，取里面的
// REPLAYGAIN_TRACK_GAIN / REPLAYGAIN_TRACK_PEAK。
func flacReplayGain(b []byte) (gain, peak float64, ok bool) {
	if cut(b, 4) != "fLaC" {
		return 0, 0, false
	}
	for p := 4; p+4 <= len(b); {
		last := b[p]&0x80 != 0
		typ := b[p] & 0x7f
		size := int(uint(b[p+1])<<16 | uint(b[p+2])<<8 | uint(b[p+3]))
		p += 4
		if size < 0 || p+size > len(b) {
			return 0, 0, false
		}
		if typ == 4 {
			return vorbisReplayGain(b[p : p+size])
		}
		p += size
		if last {
			break
		}
	}
	return 0, 0, false
}

// vorbisReplayGain 在一段 Vorbis 注释里找 ReplayGain 那两个字段。
//
// 和 `vorbisTitle` 同一套遍历，但要的是两个键（标题只要一个），所以不
// 复用它——把那份单键函数改成多键反而更绕。
//
// ⚠️ **两个字段各算各的**：只有 gain 没有 peak 照样算「读到了」——peak
// 缺省按 0（不知道）处理，增益照样能用，只是抬的时候没有峰值上限可挡。
// 反过来只有 peak 没 gain 没用，当没读到。
func vorbisReplayGain(b []byte) (gain, peak float64, ok bool) {
	p := 0
	vendorLen := le32(b, p)
	p += 4 + vendorLen
	if vendorLen < 0 || p > len(b) {
		return 0, 0, false
	}
	count := le32(b, p)
	p += 4
	if count < 0 || count > 1<<20 {
		return 0, 0, false
	}
	for i := 0; i < count && p+4 <= len(b); i++ {
		n := le32(b, p)
		p += 4
		if n < 0 || p+n > len(b) {
			return 0, 0, false
		}
		field := string(b[p : p+n])
		p += n
		key, value, c := strings.Cut(field, "=")
		if !c {
			continue
		}
		// 键大小写不敏感（同 vorbisTitle 那段注释的理由），值留着原样。
		switch {
		case strings.EqualFold(key, "REPLAYGAIN_TRACK_GAIN"):
			gain, ok = rgNumber(value)
		case strings.EqualFold(key, "REPLAYGAIN_TRACK_PEAK"):
			peak, _ = rgNumber(value)
		}
	}
	return gain, peak, ok
}

// ─────────────────────────────────────────────────────────────────────────────
// MP3：ID3v2 的 TXXX / RVA2
// ─────────────────────────────────────────────────────────────────────────────

// mp3ReplayGain 先找 TXXX（foobar2000、mp3gain、MusicBrainz Picard 写的都是
// 这一种），没有再退 RVA2（规范里正经的那个，写它的工具少得多）。
func mp3ReplayGain(path string) (gain, peak float64, ok bool) {
	b, err := readHead(path)
	if err != nil {
		return 0, 0, false
	}
	if g, p, ok := id3TXXXReplayGain(b); ok {
		return g, p, true
	}
	return id3RVA2(b)
}

// id3Frames 把 ID3v2 的每一个帧（id + 正文）摊给 fn 看，fn 返回 true 就停。
//
// 帧头布局照 `id3v2Title` 那一份：v2.2 是 3+3 没有标志，v2.3 是 4+4+2，
// v2.4 的大小是同步安全整数。这里不收单个帧——ReplayGain 可能同时有
// TXXX gain 和 TXXX peak，要两个都看到才算数，所以是回调不是返回值。
func id3Frames(b []byte, fn func(id string, body []byte) bool) {
	if len(b) < 10 || cut(b, 3) != "ID3" {
		return
	}
	major := b[3]
	flags := b[5]
	size := int(syncsafe(b[6:10]))
	start := 10
	if flags&0x40 != 0 && major >= 3 {
		if start+4 > len(b) {
			return
		}
		ext := int(binary.BigEndian.Uint32(b[start : start+4]))
		start += 4 + ext
	}
	end := min(start+size, len(b))
	idLen, sizeLen, flagLen := 4, 4, 2
	if major == 2 {
		idLen, sizeLen, flagLen = 3, 3, 0
	}
	for p := start; p+idLen+sizeLen+flagLen <= end; {
		id := string(b[p : p+idLen])
		var fsize int
		if major >= 4 {
			fsize = int(syncsafe(b[p+idLen : p+idLen+sizeLen]))
		} else {
			fsize = int(bigEndian(b[p+idLen:p+idLen+sizeLen], sizeLen))
		}
		p += idLen + sizeLen + flagLen
		if fsize <= 0 || p+fsize > end {
			break
		}
		if fn(id, b[p:p+fsize]) {
			return
		}
		p += fsize
	}
}

// id3TXXXReplayGain 在 TXXX 帧里找 `replaygain_track_gain` / `_peak`。
//
// TXXX 的正文是「编码字节 + 描述串 + NUL + 值」。描述串就是写标签的工具
// 起的名字，各家的写法一致：`replaygain_track_gain`（大小写不敏感）。
func id3TXXXReplayGain(b []byte) (gain, peak float64, ok bool) {
	id3Frames(b, func(id string, body []byte) bool {
		if id != "TXXX" {
			return false
		}
		desc, value, c := txxxFields(body)
		if !c {
			return false
		}
		switch {
		case strings.EqualFold(desc, "replaygain_track_gain"):
			gain, ok = rgNumber(value)
		case strings.EqualFold(desc, "replaygain_track_peak"):
			peak, _ = rgNumber(value)
		}
		return ok && peak > 0 // 两个都有了就不用往后翻了
	})
	return gain, peak, ok
}

// txxxFields 把 TXXX 正文拆成「描述」和「值」。
//
// 第一字节是文本编码（同 `decodeText`）：0/3 是单字节编码，NUL 就一个字节；
// 1/2 是 UTF-16，NUL 是两个零字节——按单字节找会把描述串切在半个字符中间。
func txxxFields(b []byte) (desc, value string, ok bool) {
	if len(b) < 2 {
		return "", "", false
	}
	enc := b[0]
	body := b[1:]
	if enc == 1 || enc == 2 {
		i := 0
		for ; i+1 < len(body); i += 2 {
			if body[i] == 0 && body[i+1] == 0 {
				break
			}
		}
		if i+1 >= len(body) {
			return "", "", false
		}
		return decodeUTF16(body[:i], enc == 1), decodeUTF16(body[i+2:], enc == 1), true
	}
	i := strings.IndexByte(string(body), 0)
	if i < 0 {
		return "", "", false
	}
	d := body[:i]
	v := body[i+1:]
	if enc == 0 {
		return latin1(d), latin1(v), true
	}
	return string(d), trimNul(string(v)), true
}

// id3RVA2 解 RVA2 帧里 identification 为 `track` 的那一段。
//
// RVA2 的布局（id3v2.4）：`<identification>` + NUL，之后每个声道一段
// 「声道类型(1) + 音量调整(2，有符号，单位 1/512 dB) + 峰值位数(1) +
// 峰值(位数/8 字节，大端)」。`track` 是整首，`album` 不要（理由见文件头）。
//
// ⚠️ **只认 `track`**：RVA2 允许一段里混着写好几个声道的调整，拿第一个
// 就够——多声道各自的调整在这里没有意义（这一层只做整首一个增益）。
func id3RVA2(b []byte) (gain, peak float64, ok bool) {
	id3Frames(b, func(id string, body []byte) bool {
		if id != "RVA2" {
			return false
		}
		i := 0
		for i < len(body) && body[i] != 0 {
			i++
		}
		if i >= len(body) || !strings.EqualFold(string(body[:i]), "track") {
			return false
		}
		p := i + 1
		// 声道段至少 4 字节（类型 + 调整 + 峰值位数）。
		if p+4 > len(body) {
			return true // 是 track 段但装不下一条声道，到此为止
		}
		p++ // 声道类型，不管是哪个声道都按整首用
		gain = float64(int16(binary.BigEndian.Uint16(body[p:p+2]))) / 512
		p += 2
		bits := int(body[p])
		p++
		nbytes := (bits + 7) / 8
		if nbytes > 0 && p+nbytes <= len(body) {
			var v uint64
			for _, c := range body[p : p+nbytes] {
				v = v<<8 | uint64(c)
			}
			peak = float64(v) / float64(uint64(1)<<uint(bits))
		}
		ok = true
		return true
	})
	return gain, peak, ok
}

// ─────────────────────────────────────────────────────────────────────────────
// M4A：ilst 里的 ---- 自由原子
// ─────────────────────────────────────────────────────────────────────────────

// mp4ReplayGain 在 MP4 原子树里找 `ilst` 下的 `----`（freeform）原子。
//
// iTunes 系的 ReplayGain 写法：`----` 里面套三个小原子——`mean`（固定是
// `com.apple.iTunes`）、`name`（字段名 `replaygain_track_gain`）、`data`
// （值）。`----` 可以有好几个（每个自定义字段一个），要逐个看 name。
func mp4ReplayGain(path string) (gain, peak float64, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, 0, false
	}
	var hdr [8]byte
	if _, err := f.ReadAt(hdr[:], 0); err != nil || cut(hdr[4:], 4) != "ftyp" {
		return 0, 0, false
	}
	mp4Freeform(f, 0, info.Size(), 0, 0, func(name string, body []byte) {
		// name 带 8 字节原子头之后还有 4 字节 version/flags，data 那段
		// 已经剥好了（见 mp4Freeform）。
		switch {
		case strings.EqualFold(name, "replaygain_track_gain"):
			gain, ok = rgNumber(string(body))
		case strings.EqualFold(name, "replaygain_track_peak"):
			peak, _ = rgNumber(string(body))
		}
	})
	return gain, peak, ok
}

// mp4Freeform 遍历原子树，把每个 `----` 原子里 name/data 那一对摊给 fn。
//
// `----` 的正文又是一组原子：`mean` / `name` / `data`，每个都是
// 「4 字节长度 + 4 字节类型 + 4 字节 version/flags + 内容」。整个 `----`
// 不会大（一个字段），读到内存里按字节切，比 `mp4Name` 那种 ReadAt 跳读
// 直接。
func mp4Freeform(f *os.File, start, end int64, skip int64, depth int, fn func(name string, data []byte)) {
	if depth > 8 {
		return
	}
	for p := start + skip; p+8 <= end; {
		var hdr [16]byte
		if _, err := f.ReadAt(hdr[:8], p); err != nil {
			return
		}
		size := int64(binary.BigEndian.Uint32(hdr[0:4]))
		typ := cut(hdr[4:], 4)
		body := p + 8
		switch size {
		case 0:
			size = end - p
		case 1:
			if _, err := f.ReadAt(hdr[:], p); err != nil {
				return
			}
			size = int64(binary.BigEndian.Uint64(hdr[8:16]))
			body = p + 16
		}
		if size < body-p || p+size > end {
			return
		}
		switch typ {
		case "----":
			if name, data := freeformFields(f, body, p+size); name != "" {
				fn(name, data)
			}
		case "moov", "udta", "trak", "mdia", "ilst":
			mp4Freeform(f, body, p+size, 0, depth+1, fn)
		case "meta":
			// 同 mp4Name：meta 头后面有 version/flags，4 和 8 两个偏移都试。
			mp4Freeform(f, body, p+size, 4, depth+1, fn)
			mp4Freeform(f, body, p+size, 8, depth+1, fn)
		}
		p += size
	}
}

// freeformFields 解一个 `----` 原子正文里的 `name` 和 `data`。
//
// 返回的 name 是字段名（`replaygain_track_gain` 那串），data 是 `data`
// 原子的**内容**（8 字节类型/区域码之后的那段文本）。读不到给空串。
func freeformFields(f *os.File, start, end int64) (name string, data []byte) {
	n := end - start
	if n <= 0 || n > 64<<10 { // 一个字段不可能有 64KB，超了当畸形
		return "", nil
	}
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, start); err != nil {
		return "", nil
	}
	for p := 0; p+8 <= len(buf); {
		size := int(binary.BigEndian.Uint32(buf[p : p+4]))
		typ := cut(buf[p+4:], 4)
		if size < 12 || p+size > len(buf) {
			return "", nil
		}
		content := buf[p+12 : p+size] // 8 头 + 4 version/flags
		switch typ {
		case "name":
			name = trimNul(string(content))
		case "data":
			// data 比 mean/name 多 4 字节区域码，共 8 字节前置。
			if size < 16 {
				return "", nil
			}
			data = buf[p+16 : p+size]
		}
		p += size
	}
	return name, data
}
