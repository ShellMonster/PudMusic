// 把一条 fMP4（B 站 dash 的 `.m4s`）读成一张「采样表」。
//
// # 这一层只读懂源，一个字节的样本数据都不留在内存里
//
// 一条 30 分钟的 MV 视频流是**几百 MB**，五万多个采样。表里每个采样只记
// 「在源文件第几字节、多长、多久、是关键帧吗」，数据真要写进 mdat 的
// 时候再按偏移一段一段读过去（`copy.go`）。表本身是每个采样 32 字节，
// 五万个采样 1.6 MB —— 差着两个数量级。
//
// # 为什么自己扫 box 而不用 `mp4.DecodeFile`
//
// 库那个函数从 0 读到 EOF，而且会把 mdat 的数据整个读进内存：一条 dash
// 流喂进去就是几百 MB。阶段 2 就是因为这个否掉了它当播放侧的容器库。
//
// 这一层**扫顶层 box**：moov 和 moof 读进内存解（它们就是要解的），
// mdat 一眼都不看——采样在哪儿是 `trun` 说的事，跟 mdat 的内容无关。

package mux

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Eyevinn/mp4ff/bits"
	"github.com/Eyevinn/mp4ff/mp4"

	"github.com/ShellMonster/PudMusic/internal/perf"
)

// maxBoxBytes 是一个顶层 box 读进内存的上限。
//
// 只有 moov 和 moof 会进到这里，mdat 是跳过去的。64 MB 是对「moof 大到
// 不正常」的兜底：B 站的分片最长几十秒，一个 moof 里装不下这么多采样。
// 真的超过的话报出来，而不是悄悄吃掉几百 MB 内存。
const maxBoxBytes = 64 << 20

// maxSampleBytes 是单个 sample 允许的最大字节数。
//
// 采样大小来自文件的 sample table（`uint32`），而交织器会按它扩一块中转
// 缓冲——类型上界接近 4 GiB。B 站的关键帧实测最大是几 MB，所以 64 MiB 是
// 「正常撞不到、坏了就挡住」的那一档：不挡的话，一份坏文件能让后台交织
// goroutine 直接申请几百 MB 甚至更多。
const maxSampleBytes = 64 << 20

// gauge 是这一层写进诊断快照的字节表。
//
// ⚠️ **它是变量，不是直接调 `perf.ProcessGauges()`**：高水位一旦被推上去就
// 只增不减，同一个包里先跑的用例会把值抬到后面那些用例的期望之上——测试
// 于是「不管这里记没记都能过」。用变量之后，测试可以指向一份自己的账本，
// 断言这一次真的记了。
var gauge = perf.ProcessGauges()

// sampleRef 是一个采样在源流里的位置和时间戳。
//
// 时间戳都在**这条流自己的媒体时基**里（视频 90000、音频 48000，各是各的），
// 跨流比较要用 `movieTimescale` 那套换算。
type sampleRef struct {
	off  int64  // 数据在源文件里的绝对字节偏移
	size uint32 // 字节数
	dur  uint32 // 时长（媒体时基）
	cts  int32  // 呈现时间相对解码时间的偏移（B 帧重排就在这儿）
	sync bool   // 是不是关键帧（I 帧 / 音频里恒为 true）
	dts  uint64 // 解码时间，**已减掉这条流的起点**（所以第一个是 0）
}

// source 是一条输入流读出来的东西。
type source struct {
	path      string
	size      int64
	trackID   uint32
	timescale uint32
	video     bool
	// trak 是要**原样搬进输出**的那棵盒子树：采样描述符（`avcC` / `esds`）
	// 在它里面。交织只换容器，**不动编码参数**——解码器要的东西全在这儿。
	trak *mp4.TrakBox
	// samples 按 DTS 递增。
	samples []sampleRef
	// trex 是分片流里那些「这一段没写、按默认来」的值的出处。
	trex *mp4.TrexBox
	// nextDTS 是下一个分片的解码时间该从哪儿起。没有 tfdt 的分片接着它。
	nextDTS uint64
	// mediaDur 是全部采样的时长之和（媒体时基）。
	mediaDur uint64
	// firstPTS 是第一个**呈现**出来的采样出现在哪儿（媒体时基，**已减掉
	// `origin`**）。
	//
	// 它跟 0 不一定相等：B 帧重排会让头几个采样的 cts 是负的。而
	// `origin + firstPTS` 才是这条轨**在原时间轴上**的首帧位置——两条轨
	// 的差就是**音画差**，编辑列表靠它算（见 `build.layoutEdits`）。
	firstPTS int64
	// origin 是第一个采样的原始 DTS（第一个分片的 `tfdt`）。
	//
	// 为什么要留着：每条源流的时间轴都是**自己**从某个 `tfdt` 起的，而
	// 两条独立流的 `tfdt` 之差就是音画差。把每条各自归零（媒体时间轴从 0
	// 起，这本身是对的）会把那个差**抹掉**，于是「音轨比视频轨晚一秒」
	// 就变成了「两条轨同时开始」——编辑列表整个不写，播放器从 0 就出声。
	origin uint64
	// endDTS 是最后一个采样的 dts+dur（媒体时基）。
	endDTS uint64
	// endPTS 是最后一个采样**呈现完**的时刻（媒体时基）。
	//
	// 它跟 `endDTS` 不是一回事：B 帧重排让最后几帧的呈现时间落在
	// 「全部采样时长之和」之后。成片的时长要按它算——按 `mediaDur` 算的
	// 话，最后一个 GOP 里那两个 B 帧会落在成片时长**之外**，播放器要么
	// 把它们砍掉（少两帧，看着像丢帧）要么在结尾卡一下。
	endPTS int64
}

// readSource 读一条 dash 流。
func readSource(path string) (*source, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("mux: 打开 %s：%w", filepath.Base(path), err)
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("mux: 问 %s 有多大：%w", filepath.Base(path), err)
	}
	if st.Size() < 16 {
		return nil, fmt.Errorf("mux: %s 只有 %d 字节，不像一条 dash 流", filepath.Base(path), st.Size())
	}

	src := &source{path: path, size: st.Size()}
	r := bufio.NewReaderSize(f, 1<<20)
	var pos int64
	for pos < src.size {
		hdr, err := readBoxHeader(r)
		if err != nil {
			// 读满了一个 box 但没到头，而剩下的又不足 8 字节（连头都读不出来）
			// 或者根本就是零：**这不是「到这里为止」，这是文件截断了**。
			// 报错而不是收工——收工的话下面那句「一条 moov 都没有」会把
			// 截断说成「这不是 dash 流」，而用户真正的问题是「下了一半」。
			return nil, fmt.Errorf("mux: %s 在第 %d 字节处断了（%w）", filepath.Base(path), pos, err)
		}
		if int64(hdr.size) < int64(hdr.hdrLen) || pos+int64(hdr.size) > src.size {
			return nil, fmt.Errorf("mux: %s 里第 %d 字节那个 %s box 声明有 %d 字节，而文件只剩 %d",
				filepath.Base(path), pos, hdr.name, hdr.size, src.size-pos)
		}

		// moov / moof 会被整个读走（`readBox` 连头带身子一起读），
		// 别的 box 只需要跳过。**不能读完了再跳一遍**——那样位置会
		// 往前多跑一个 box 的长度，读到中间的样本数据里去，后面全是
		// 「某某 box 声明有 1296977744 字节」这种看不懂的报错。
		consumed := false
		switch hdr.name {
		case "moov":
			if src.trak != nil {
				return nil, fmt.Errorf("mux: %s 里有两个 moov", filepath.Base(path))
			}
			if err := src.readMoov(r, hdr); err != nil {
				return nil, err
			}
			consumed = true
		case "moof":
			if src.trak == nil {
				return nil, fmt.Errorf("mux: %s 里 %s 在 moov 之前", filepath.Base(path), hdr.name)
			}
			moof, err := readMoofBox(r, hdr)
			if err != nil {
				return nil, err
			}
			if err := src.readMoof(moof, pos); err != nil {
				return nil, err
			}
			consumed = true
		}

		if !consumed {
			// mdat 就是在这一步被跳掉的——一个字节都不读。
			body := int64(hdr.size) - int64(hdr.hdrLen)
			if _, err := r.Discard(int(body)); err != nil {
				return nil, fmt.Errorf("mux: 跳过 %s 里第 %d 字节那个 %s：%w", filepath.Base(path), pos, hdr.name, err)
			}
		}
		pos += int64(hdr.size)
	}

	if err := src.finish(); err != nil {
		return nil, fmt.Errorf("%s：%w", filepath.Base(path), err)
	}
	return src, nil
}

// readMoov 解初始化段，记下要搬的那棵树和 trex 的默认值。
func (s *source) readMoov(r io.Reader, hdr boxHeader) error {
	b, err := readBox(r, hdr)
	if err != nil {
		return err
	}
	moov, ok := b.(*mp4.MoovBox)
	if !ok {
		return fmt.Errorf("mux: %s 里那个 moov 读出来是 %T", filepath.Base(s.path), b)
	}
	// 一条 dash 流里**正好一条轨**。多了说明它不是我们要的那种输入，
	// 硬取第一条会交织出一条「有声音没画面」或者反过来的文件。
	if len(moov.Traks) != 1 {
		return fmt.Errorf("mux: %s 的 moov 里有 %d 条轨，dash 流应该只有一条", filepath.Base(s.path), len(moov.Traks))
	}
	trak := moov.Traks[0]
	if trak.Tkhd == nil || trak.Mdia == nil || trak.Mdia.Mdhd == nil || trak.Mdia.Hdlr == nil {
		return fmt.Errorf("mux: %s 的轨缺 tkhd / mdia / mdhd / hdlr", filepath.Base(s.path))
	}

	// ⚠️ 校验要一路到 `stsd`。只查 `tkhd`/`mdia`/`mdhd`/`hdlr` 的话，一个
	// 解得开而没有 `minf`/`stbl`/`stsd` 的 `moov` 能一路走到交织前那道
	// 关里那个解引用 `stsd` 的 switch 上——**panic**。而交织跑在后台
	// goroutine 上，那里 panic 会一路冒到下载队列的循环（`runOne` →
	// `loop` 谁都不 recover）：表现是**所有下载一起停住而界面上没有
	// 任何报错**。
	if trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil || trak.Mdia.Minf.Stbl.Stsd == nil {
		return fmt.Errorf("mux: %s 的轨没有 minf / stbl / stsd", filepath.Base(s.path))
	}

	// trex 是分片流里那些「这一段没写、按默认来」的值的出处。抽掉了它，
	// trun 里没写的字段就全是 0——那样每一帧的时长都会变成 0。
	if moov.Mvex == nil || len(moov.Mvex.Trexs) != 1 {
		return fmt.Errorf("mux: %s 的 moov 里没有 trex，这不是一条分片流", filepath.Base(s.path))
	}
	trex := moov.Mvex.Trexs[0]

	s.trak = trak
	s.trackID = trak.Tkhd.TrackID
	s.timescale = trak.Mdia.Mdhd.Timescale
	if s.timescale == 0 {
		return fmt.Errorf("mux: %s 的轨时基是 0", filepath.Base(s.path))
	}
	if trex.TrackID != s.trackID {
		return fmt.Errorf("mux: %s 的 trex 说的是轨 %d，而 moov 里的轨是 %d",
			filepath.Base(s.path), trex.TrackID, s.trackID)
	}
	s.trex = trex

	switch trak.Mdia.Hdlr.HandlerType {
	case "vide":
		s.video = true
	case "soun":
		s.video = false
	default:
		return fmt.Errorf("mux: %s 是一条 %s 轨的视频下载里没有的东西", filepath.Base(s.path), trak.Mdia.Hdlr.HandlerType)
	}
	return nil
}

// readMoof 解一个分片头，把这一片里的采样记进表。
func (s *source) readMoof(moof *mp4.MoofBox, moofStart int64) error {
	for _, traf := range moof.Trafs {
		if traf.Tfhd == nil || traf.Tfhd.TrackID != s.trackID || len(traf.Truns) == 0 {
			continue // 别轨（B 站一条流里只有一条，但别写成「假定没有」）
		}
		if err := s.addTraf(moofStart, moof, traf); err != nil {
			return err
		}
	}
	return nil
}

// addTraf 把一个 traf 里的采样记进表。
func (s *source) addTraf(moofStart int64, moof *mp4.MoofBox, traf *mp4.TrafBox) error {
	tfhd := traf.Tfhd

	// 基准位置：这一片的数据算在哪儿起算。
	//
	// `default-base-is-moof` 是分片流里压倒性的写法（B 站就是它），
	// 基准是 moof 自己那个位置。
	//
	// 没有这个标志又没写 `base-data-offset` 的时候，**规范**
	// （ISO/IEC 14496-12 §8.8.7）说基准是**这个 moof 后面的第一个字节**，
	// 不是 moof 的起点——两者差着整个 moof 的长度（几 KB）。照 moof 起点
	// 算出来的每一个偏移都会落进 moof 的字节里，而那几 KB 是**解得开的**
	// 分片元数据：于是采样被换成了 `trun` 的内容，而出来的文件轨道数对、
	// 时长对、能播，播出来是花的。
	var moofSize uint64
	if moof != nil {
		moofSize = moof.Size()
	}
	base := uint64(moofStart) + moofSize
	if tfhd.Flags&mp4.TfhdDefaultBaseIsMoofFlag != 0 {
		base = uint64(moofStart)
	}
	if tfhd.HasBaseDataOffset() {
		base = tfhd.BaseDataOffset
	}

	// 这一片的解码时间从哪儿起。tfdt 是绝对值，不写就接着上一片。
	dts := s.nextDTS
	if traf.Tfdt != nil {
		dts = traf.Tfdt.BaseMediaDecodeTime()
	}
	if dts < s.nextDTS {
		return fmt.Errorf("mux: 有一段把解码时间说成了 %d，而前面已经写到 %d（时间戳倒退了）", dts, s.nextDTS)
	}

	off := base
	for _, trun := range traf.Truns {
		// trun 里没写的字段按 tfhd → trex 补齐。不补的话每一帧的
		// 时长/大小都是 0，而时长全是 0 的文件**能打开、能播、就是不动**。
		trun.AddSampleDefaultValues(tfhd, s.trex)
		if trun.HasDataOffset() {
			// ⚠️ `data_offset` 是**有符号**的，可以是负的（往 moof 前面指）。
			// `uint64(base) + uint64(负数)` 在 Go 里是**回绕**，得到一个天文
			// 数字；不挡的话下面那句「在文件里吗」会因为回绕后仍然很大而
			// 漏过去，最后在搬数据时变成一句看不懂的 EINVAL。
			if trun.DataOffset < 0 && uint64(-int64(trun.DataOffset)) > base {
				return fmt.Errorf("mux: 一个分片的数据偏移落在源文件之前（基准 %d，偏移 %d）",
					base, trun.DataOffset)
			}
			off = base + uint64(int64(trun.DataOffset))
			if off < base {
				return fmt.Errorf("mux: 一个分片的数据偏移算出来落在源文件之前")
			}
		}
		for _, smp := range trun.Samples {
			end := int64(off) + int64(smp.Size)
			if end > s.size {
				return fmt.Errorf("mux: 有一个采样在源文件第 %d 字节、共 %d 字节，而文件只有 %d 字节",
					int64(off), smp.Size, s.size)
			}
			s.samples = append(s.samples, sampleRef{
				off: int64(off), size: smp.Size, dur: smp.Dur,
				cts:  smp.CompositionTimeOffset,
				sync: mp4.IsSyncSampleFlags(smp.Flags),
				dts:  dts,
			})
			off = uint64(end)
			dts += uint64(smp.Dur)
		}
	}
	s.nextDTS = dts
	return nil
}

// finish 收尾：把时间轴挪到从 0 起，算出这条流的呈现范围。
func (s *source) finish() error {
	if len(s.samples) == 0 {
		return errors.New("一条采样都没有——这不像是一条下全了的 dash 流")
	}

	origin := s.samples[0].dts
	s.origin = origin
	s.firstPTS = int64(1) << 62
	for i := range s.samples {
		sp := &s.samples[i]
		sp.dts -= origin
		pts := int64(sp.dts) + int64(sp.cts)
		if pts < s.firstPTS {
			s.firstPTS = pts
		}
		if end := pts + int64(sp.dur); end > s.endPTS {
			s.endPTS = end
		}
		s.endDTS = sp.dts + uint64(sp.dur)
	}
	s.mediaDur = s.endDTS
	return nil
}

// readMoofBox 把一个 `moof` 读进内存解出来。
func readMoofBox(r io.Reader, hdr boxHeader) (*mp4.MoofBox, error) {
	b, err := readBox(r, hdr)
	if err != nil {
		return nil, err
	}
	moof, ok := b.(*mp4.MoofBox)
	if !ok {
		return nil, fmt.Errorf("mux: 那个 moof 读出来是 %T", b)
	}
	return moof, nil
}

// boxHeader 是一个顶层 box 的头。
//
// **自己读而不用 `mp4.DecodeHeader`** 是因为要解的那两个 box 得连头一起
// 交给 `DecodeBoxSR`（它自己会再读一次头），而 `DecodeHeader` 把那 8 字节
// 吃掉了、又不还回来。少一个字节，库就会把 moov 里面的第一个子 box
// （`mvhd`）当成那个 moov 读出来——**静默地读错一个盒子**。
type boxHeader struct {
	name   string
	size   uint64
	hdrLen int
	raw    []byte // 头本身，解的时候要连它一起给回去
}

// readBoxHeader 读一个 box 的头（8 或 16 字节）。
func readBoxHeader(r io.Reader) (boxHeader, error) {
	buf := make([]byte, boxHeaderLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return boxHeader{}, err
	}
	h := boxHeader{name: string(buf[4:8]), size: uint64(binary.BigEndian.Uint32(buf[:4])), hdrLen: boxHeaderLen}
	switch h.size {
	case 0:
		return boxHeader{}, fmt.Errorf("box 大小是 0，意思是「到文件末尾」，解不了")
	case 1:
		// 64 位大小在后面 8 字节里。只有 mdat 会这么写，而 mdat 我们不读。
		extra := make([]byte, 8)
		if _, err := io.ReadFull(r, extra); err != nil {
			return boxHeader{}, fmt.Errorf("读 64 位大小：%w", err)
		}
		h.size = binary.BigEndian.Uint64(extra)
		h.hdrLen = 16
		h.raw = append(buf, extra...)
		return h, nil
	}
	if h.size < uint64(h.hdrLen) {
		return boxHeader{}, fmt.Errorf("%s 的 box 大小 %d 比它自己的头 %d 还小", h.name, h.size, h.hdrLen)
	}
	h.raw = buf
	return h, nil
}

// boxHeaderLen 是 box 头最短的那一种（32 位大小 + 四字类型）。
const boxHeaderLen = 8

// readBox 把一个 box 连头带身子读进内存再解。
//
// 只给 moov / moof 用（见 `maxBoxBytes`）。`startPos` 传 0 是因为这块内存
// 里 box 就在第 0 字节——mp4ff 用它算嵌套 box 的绝对位置，而我们只要
// 相对关系。
func readBox(r io.Reader, hdr boxHeader) (mp4.Box, error) {
	// 最大的那个 box 进诊断快照（票 #668）：它是这一层唯一一次「按文件里
	// 声明的长度分配内存」，异常大的 moov/moof 在快照里就这一个数能看出来。
	gauge.NoteMP4BoxBytes(int64(hdr.size))
	if hdr.size > maxBoxBytes {
		return nil, fmt.Errorf("mux: %s box 有 %d 字节，比能解的 %d 还大", hdr.name, hdr.size, maxBoxBytes)
	}
	buf := make([]byte, hdr.size)
	copy(buf, hdr.raw)
	if _, err := io.ReadFull(r, buf[len(hdr.raw):]); err != nil {
		return nil, fmt.Errorf("mux: 读 %s 的 %d 字节：%w", hdr.name, hdr.size, err)
	}
	return mp4.DecodeBoxSR(0, bits.NewFixedSliceReader(buf))
}
