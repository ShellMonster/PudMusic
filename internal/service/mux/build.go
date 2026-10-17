// 把两张采样表拼成一个能播的 MP4（`moov` 在前、`mdat` 在后的渐进式文件）。
//
// # 输出是**渐进式**（moov-before-mdat），不是分片式
//
// 分片式（fMP4）能边下边写，但那是给网络流准备的：本地文件用分片式，
// 两个系统自带的播放器里有一个会拒绝。渐进式是「moov 在最前面、
// mdat 在后面」的老式布局，**每个**播放器都认。
//
// 代价是 `moov` 里的采样表必须**先**知道，所以要先扫一遍源（`source.go`）
// 才知道每个采样多大、多长、在哪儿。这一遍只读元数据不读数据，
// 五万个采样加起来 1.6 MB，扫一遍几百 MB 的源是顺序读，很快。
//
// # `moov` 的大小和 `mdat` 的位置互为因果，这里怎么解开
//
// `mdat` 装在 `moov` 后面，所以采样表里的字节偏移取决于 `moov` 有多大；
// 而 `moov` 里那几块采样表（`stsz` 每个采样 4 字节、`co64` 每个 chunk
// 8 字节）的**大小只跟采样数和 chunk 数有关，跟它们填的具体数值无关**。
//
// 所以顺序是死的：**先按条目数把 `moov` 搭起来**（偏移全填 0）→ 量它多大
// → 算出 `mdat` 的载荷从文件哪个字节开始 → 把真正的偏移填回去 → 写。
// 全程 `co64`（64 位偏移）而不是 `stco`，正是为了「填数值不改变大小」
// 这一条成立——`stco` 在 4 GB 以下更省，但那要来回试两遍才知道会不会
// 越界，而交织一个 30 分钟的 MV 越 4 GB 很容易。
//
// # 交织顺序：按时间窗交替，不是严格按 DTS 排序
//
// 真按 DTS 排，两个轨的采样会**逐帧交替**（30 fps 的视频和 43 帧每秒的
// AAC 一帧一帧换），30 分钟下来十几万个 chunk，`co64` 光偏移就 1 MB。
//
// 真机上的 muxer（ffmpeg 也是）按**时间窗**交替：取 0.5 秒，先把这个
// 窗里的视频采样连着写完，再把这个窗里的音频采样连着写完，然后进下一个
// 窗。chunk 数降到一秒两个。代价是文件里的字节顺序不是严格 DTS 递增
// ——这是正常的，播放器的采样表按轨索引，取数据是按偏移跳过去的，
// 不靠文件顺序。

package mux

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"sort"

	"github.com/Eyevinn/mp4ff/mp4"
)

// interleaveWindowPerSec 是交织的时间窗有多长（每秒几个窗）。
//
// 2 = 半秒一个窗。这是「chunk 数」和「seek 粒度」之间折中：窗越小，
// 拖进度条越准，而拖不准的代价是**用户每次拖都等一下**；窗越大，
// `co64`/`stsc` 越省。半秒在两头都不难看。
const interleaveWindowPerSec = 2

// copyBufSize 是往 mdat 里搬采样数据时用的中转缓冲区。
//
// 一个关键帧能有几百 KB，而一帧音频只有几百字节。不共用一块缓冲区的话
// 就会按最大的那个分配。
const copyBufSize = 256 << 10

// track 是输出里的一条轨：源的那棵树，加上交织之后才算得出来的几样东西。
type track struct {
	src     *source
	id      uint32
	movieTS uint32
	// delay 是这条轨比另一条晚开始多少（电影时基）。
	delay uint64
	// mediaTime 是编辑列表里的 `media_time`：从媒体时间的哪儿开始**呈现**。
	mediaTime int64
	// presDur 是这条轨的呈现时长（电影时基）。
	presDur uint64
	runs    []*run
}

// run 是一段连续的字节：同一条轨、连续的若干个采样。
//
// 每一段对应输出 `moov` 里的一个 chunk。
type run struct {
	track int // 第几条轨（track 数组的下标）
	first int // 从这条轨的第几个采样开始
	count int
	bytes int64
	rel   int64 // 相对 mdat 载荷起点的字节偏移
	chunk int   // 它是这条轨的第几个 chunk（写进 `co64` 时要按轨编号）
}

// build 交织两条流，写到 out。**每搬一个 chunk 认一次取消**（见 `Mux`）。
func build(ctx context.Context, out io.Writer, video, audio *source, movieTS uint32) ([]*track, error) {
	tracks := []*track{
		{src: video, id: 1, movieTS: movieTS},
		{src: audio, id: 2, movieTS: movieTS},
	}
	layoutEdits(tracks)
	ftyp := mp4.NewFtyp("isom", 512, []string{"isom", "iso2", "avc1", "mp41"})
	moov, err := buildMoov(tracks, movieTS)
	if err != nil {
		return nil, err
	}

	// `layoutRuns` 已经把字节偏移按交错顺序排好了，这里**只求和**。
	//
	// ⚠️ 千万别在这里按轨重排一遍偏移——`runs` 是**分轨记的**，按轨重排
	// 会把「视频一段、音频一段、视频一段」压回「所有视频在前、所有音频
	// 在后」。那个文件照样能播（采样表按轨索引，取数据按偏移跳），
	// 但播放时要等音频那条下完才出声——而交织的意义正好是**不用等**。
	payload := int64(0)
	for _, t := range tracks {
		for _, r := range t.runs {
			payload += r.bytes
		}
	}
	if err := checkCovers(tracks, payload); err != nil {
		return nil, err
	}
	mdatHdr := int64(8)
	if payload+mdatHdr > math.MaxUint32 {
		mdatHdr = 16
	}
	dataStart := int64(ftyp.Size()) + int64(moov.Size()) + mdatHdr
	assignChunkOffsets(tracks, uint64(dataStart))

	if err := ftyp.Encode(out); err != nil {
		return nil, fmt.Errorf("mux: 写 ftyp：%w", err)
	}
	if err := moov.Encode(out); err != nil {
		return nil, fmt.Errorf("mux: 写 moov：%w", err)
	}
	if err := mp4.EncodeHeaderWithSize("mdat", uint64(payload+mdatHdr), mdatHdr == 16, out); err != nil {
		return nil, fmt.Errorf("mux: 写 mdat 的头：%w", err)
	}
	if err := copySamples(ctx, out, tracks); err != nil {
		return nil, err
	}
	return tracks, nil
}

// layoutEdits 算每条轨的空编辑和编辑起点。
//
// **这一段就是「时间戳换算」和「音画同步」在文件里的落点。**
//
// 两条轨各有各的时基（视频 90000、音频 48000），而**呈现**时间要在同一个
// 时基下比：谁先出现、差多少。这一步把两条轨的首帧都换算到电影时基，
// 晚的那条补一段**空编辑**（`elst` 里 `media_time = -1` 的那一项），
// 于是播放时它就是「先静着，到点了再出声」。
//
// 绝大多数情况两条轨的首帧都是 0，空编辑是 0 字节——`edts` 整个不写。
func layoutEdits(tracks []*track) {
	// 首帧在**原时间轴**上的位置：归零之后每条轨都从 0 起，差就抹掉了
	// （见 `source.origin`）。
	startOf := func(t *track) int64 {
		return int64(t.src.origin) + t.src.firstPTS
	}
	base := int64(math.MaxInt64)
	for _, t := range tracks {
		if p := rescale(startOf(t), t.src.timescale, t.movieTS); p < base {
			base = p
		}
	}
	for _, t := range tracks {
		start := rescale(startOf(t), t.src.timescale, t.movieTS)
		// 呈现的范围是 [firstPTS, endPTS)。
		//
		// ⚠️ 右端用 `endPTS` 而不是 `mediaDur`：B 帧重排让最后几帧的
		// 呈现时间落在「所有采样时长之和」之后。按 `mediaDur` 收尾的话，
		// 结尾那个 GOP 的两个 B 帧落在成片时长**之外**——播放器会
		// 少放两帧（看着像丢帧）或者在结尾顿一下。
		from := t.src.firstPTS
		if from < 0 {
			from = 0
		}
		// 一个「所有采样时长都是 0」的源会走到 `endPTS < from`，而
		// `uint64(负数)` 是天文数字——`presDur` 跟着绕回成 0，
		// 于是写出来的 `mvhd`/`tkhd` 时长是 0 而采样表是满的：
		// **打开正常、轨道数正常、就是一片黑**。
		if t.src.endPTS < from {
			t.delay, t.mediaTime, t.presDur = 0, 0, 0
			continue
		}
		// 🔴 这两个时长的**单位不一样**，别混：
		//
		//	`delay` 是 `elst` 的 `segment_duration`，单位是**电影**时基
		//	`mediaTime` 是 `elst` 的 `media_time`，单位是**这条轨自己的媒体**时基
		//
		// 拿电影时基的数去填 `media_time` 的话，音频轨（48000）会被按
		// 90000 解释——**差 1.875 倍**。症状是「开头多出小半秒没声音，
		// 之后音画慢慢错开」，而两条轨首帧都是 0 的常见情况下这一段代码
		// 根本不执行，所以它躲过了所有「能播」的验收。
		pres := rescale(t.src.endPTS-from, t.src.timescale, t.movieTS)
		if start < 0 {
			// 首帧的呈现时间在 0 之前（B 帧重排的常见结果）：空编辑先垫上
			// 这段，之后整条媒体原样呈现。
			t.delay = uint64(-start)
			t.mediaTime = 0
		} else {
			t.delay = uint64(start - base)
			t.mediaTime = from // **媒体时基**，不是电影时基
		}
		t.presDur = t.delay + uint64(pres)
	}
}

// checkCovers 验那些段**恰好**盖满 mdat 的载荷，一段不多一段不少。
//
// 这一条不是多余的洁癖：`mdat` 里少一个字节就是某个采样的数据被截掉，
// 多一个字节后面全部错位——而这两种的文件**都能打开**。偏移表只按 chunk
// 记，一段错了不查就没人会发现，直到播放时某一块变成花的。
func checkCovers(tracks []*track, payload int64) error {
	all := make([]*run, 0, len(tracks[0].runs)+len(tracks[1].runs))
	for _, t := range tracks {
		all = append(all, t.runs...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].rel < all[j].rel })
	at := int64(0)
	for _, r := range all {
		if r.rel != at {
			return fmt.Errorf("mux: 交织出来的字节中间有个洞或者重叠：第 %d 字节应该是 %d"+
				"（一段少了 %d 字节或者多了 %d 字节）", at, r.rel, at-r.rel, r.rel-at)
		}
		at += r.bytes
	}
	if at != payload {
		return fmt.Errorf("mux: 交织出来的是 %d 字节，而 mdat 预留了 %d 字节", at, payload)
	}
	return nil
}

// layoutRuns 决定 mdat 里的字节顺序。
//
// 见文件头「交织顺序」那一段：按时间窗交替，窗内先视频后音频。
func layoutRuns(tracks []*track) {
	window := int64(tracks[0].movieTS) / interleaveWindowPerSec
	if window <= 0 {
		window = 1
	}
	cursors := []int{0, 0}
	var rel int64
	for {
		head := int64(math.MaxInt64)
		for i, t := range tracks {
			if c := cursors[i]; c < len(t.src.samples) {
				if d := rescale(int64(t.src.samples[c].dts), t.src.timescale, t.movieTS); d < head {
					head = d
				}
			}
		}
		if head == math.MaxInt64 {
			return // 两条轨都写完了
		}
		limit := (head/window + 1) * window
		for i, t := range tracks {
			start := cursors[i]
			end := start
			var bytes int64
			for end < len(t.src.samples) {
				d := rescale(int64(t.src.samples[end].dts), t.src.timescale, t.movieTS)
				if d >= limit {
					break
				}
				bytes += int64(t.src.samples[end].size)
				end++
			}
			if end == start {
				continue
			}
			t.runs = append(t.runs, &run{
				track: i, first: start, count: end - start,
				bytes: bytes, rel: rel, chunk: len(t.runs),
			})
			rel += bytes
			cursors[i] = end
		}
	}
}

// buildMoov 搭输出的 `moov`。
//
// **采样描述符（`avcC` / `esds`）是从源搬过去的，一个字节没改**——
// 这一票不做转码、不改编码参数，解码器要的东西全在 `stsd` 里面。
func buildMoov(tracks []*track, movieTS uint32) (*mp4.MoovBox, error) {
	layoutRuns(tracks)

	mvhd := mp4.CreateMvhd()
	mvhd.Timescale = movieTS
	var movieDur uint64
	for _, t := range tracks {
		if t.presDur > movieDur {
			movieDur = t.presDur
		}
	}
	mvhd.Duration = movieDur
	if movieDur > math.MaxUint32 {
		mvhd.Version = 1
	}

	moov := mp4.NewMoovBox()
	moov.AddChild(mvhd)
	for _, t := range tracks {
		trak, err := buildTrak(t)
		if err != nil {
			return nil, err
		}
		moov.AddChild(trak)
	}
	mvhd.NextTrackID = uint32(len(tracks) + 1)
	return moov, nil
}

// buildTrak 把源的那棵树改成能在渐进式文件里用的样子。
func buildTrak(t *track) (*mp4.TrakBox, error) {
	src := t.src
	if src.trak.Tkhd == nil || src.trak.Mdia == nil || src.trak.Mdia.Minf == nil ||
		src.trak.Mdia.Minf.Stbl == nil || src.trak.Mdia.Minf.Stbl.Stsd == nil {
		return nil, fmt.Errorf("mux: 那条 %s 轨缺 tkhd / mdia / minf / stbl / stsd", kindOf(src))
	}

	// 轨道号必须换。两个源各自都是**独立的一条流**，B 站给它们的轨道号
	// 都是 1；不换的话输出里两条轨都叫 1，播放器只会放第一条。
	tkhd := src.trak.Tkhd
	tkhd.TrackID = t.id
	tkhd.Duration = t.presDur
	if t.presDur > math.MaxUint32 {
		tkhd.Version = 1
	}
	mdhd := src.trak.Mdia.Mdhd
	mdhd.Duration = src.mediaDur
	if src.mediaDur > math.MaxUint32 {
		mdhd.Version = 1
	}

	fillSampleTables(src.trak.Mdia.Minf.Stbl, src.samples, t.runs)
	children := trimTrakChildren(src.trak, t)
	src.trak.Children = children
	return src.trak, nil
}

// trimTrakChildren 重排 `trak` 的子盒子，丢掉源带来的 `edts`。
//
// 源是分片流，它的 `moov` 里的 `elst` 有一项 `segment_duration = 0`
// （「剩下的全都要」）。渐进式文件的 `mvhd` 时长是真的，而那个 0 会被
// 播放器**照字面**理解——于是整条轨在成片里变成 0 长，一个画面都没有。
// 与其去修那个 0，不如整个丢掉：编辑列表是我们按对齐算出来重新写的
// （见 `layoutEdits`），源那一份已经没有意义了。
func trimTrakChildren(trak *mp4.TrakBox, t *track) []mp4.Box {
	var edts *mp4.EdtsBox
	if t.delay > 0 || t.mediaTime > 0 {
		elst := &mp4.ElstBox{}
		if t.delay > 0 {
			elst.Entries = append(elst.Entries, mp4.ElstEntry{
				SegmentDuration: t.delay, MediaTime: -1, MediaRateInteger: 1,
			})
		}
		if t.presDur > t.delay {
			elst.Entries = append(elst.Entries, mp4.ElstEntry{
				SegmentDuration:  t.presDur - t.delay,
				MediaTime:        t.mediaTime,
				MediaRateInteger: 1,
			})
		}
		if elst.Version == 0 && elstNeeds64(elst) {
			elst.Version = 1
		}
		edts = &mp4.EdtsBox{Elst: []*mp4.ElstBox{elst}, Children: []mp4.Box{elst}}
	}
	trak.Edts = edts

	out := make([]mp4.Box, 0, len(trak.Children)+1)
	for _, c := range trak.Children {
		if _, ok := c.(*mp4.EdtsBox); ok {
			continue
		}
		if edts != nil {
			// edts 惯例上紧跟在 tkhd 后面（ISO/IEC 14496-12 §8.3.1）。
			out = append(out, c)
			if _, ok := c.(*mp4.TkhdBox); ok {
				out = append(out, edts)
			}
			continue
		}
		out = append(out, c)
	}
	if edts != nil && len(out) > 0 {
		if _, ok := out[len(out)-1].(*mp4.EdtsBox); !ok {
			// 没有 tkhd 可挂（不该发生），挂在最后总比不写强。
			out = append(out, edts)
		}
	}
	return out
}

// elstNeeds64 报这条编辑列表的时长是不是装不进 32 位。
func elstNeeds64(elst *mp4.ElstBox) bool {
	for _, e := range elst.Entries {
		if e.SegmentDuration > math.MaxUint32 {
			return true
		}
	}
	return false
}

// fillSampleTables 把 `stbl` 的采样表换成按交错结果算出来的那几张。
//
// 只动 `stts`/`ctts`/`stsc`/`stsz`/`stss`/偏移表；`stsd` 原样留着
// （见 `buildMoov` 的说明）。
func fillSampleTables(stbl *mp4.StblBox, samples []sampleRef, runs []*run) {
	stts := &mp4.SttsBox{}
	var ctts *mp4.CttsBox
	var cttsCounts []uint32
	var cttsOffsets []int32
	anyCts := false
	cttsVersion := byte(0)

	stsz := &mp4.StszBox{SampleNumber: uint32(len(samples))}
	uniform := len(samples) > 0
	stss := &mp4.StssBox{}

	for i, s := range samples {
		if n := len(stts.SampleCount); n > 0 && stts.SampleTimeDelta[n-1] == s.dur {
			stts.SampleCount[n-1]++
		} else {
			stts.SampleCount = append(stts.SampleCount, 1)
			stts.SampleTimeDelta = append(stts.SampleTimeDelta, s.dur)
		}
		if s.cts != 0 {
			anyCts = true
		}
		if s.cts < 0 {
			cttsVersion = 1
		}
		if n := len(cttsOffsets); n > 0 && cttsOffsets[n-1] == s.cts {
			cttsCounts[n-1]++
		} else {
			cttsCounts = append(cttsCounts, 1)
			cttsOffsets = append(cttsOffsets, s.cts)
		}
		if s.size != samples[0].size || s.size == 0 {
			uniform = false
		}
		// ⚠️ `stss` 列的是**关键帧**。少写一个符号就变成「列的是非关键帧」，
		// 而那个文件**照样能从头顺下来播**——拖进度条时每一次 seek 都落在
		// 一个 P/B 帧上，出来的是花的或者直接不解码。顺放看不出来，所以
		// 这一条要单独断。
		if s.sync {
			stss.SampleNumber = append(stss.SampleNumber, uint32(i+1))
		}
	}
	if uniform {
		stsz.SampleUniformSize = samples[0].size
	} else {
		stsz.SampleSize = make([]uint32, len(samples))
		for i, s := range samples {
			stsz.SampleSize[i] = s.size
		}
	}
	if anyCts {
		ctts = &mp4.CttsBox{Version: cttsVersion}
		_ = ctts.AddSampleCountsAndOffset(cttsCounts, cttsOffsets)
	}
	// 两种情况都**不要** `stss` 这个盒子，而缺它就是这个意思：
	//
	//	全是关键帧（或者一个关键帧都没有）→ 没有 seek 点要记，
	//	                                 缺 `stss` 正是这句话
	//	写成 `entry_count = 0` 却是**反过来的**意思：「一个同步采样都没有」
	//
	// 后者对音频轨是常态（AAC 每一帧都能独立解码，源里不给同步标志），
	// 而它会让播放器认为这条轨一个 seek 点都没有——于是**拖进度条拖不动**。
	if len(stss.SampleNumber) == 0 || len(stss.SampleNumber) == len(samples) {
		stss = nil
	}

	stsc, co64 := buildChunkTables(runs)

	stbl.Stts = stts
	stbl.Ctts = ctts
	stbl.Stsc = stsc
	stbl.Stsz = stsz
	stbl.Stss = stss
	stbl.Stco = nil
	stbl.Co64 = co64
	stbl.Sdtp = nil
	stbl.Sbgp, stbl.Sbgps = nil, nil
	stbl.Sgpd, stbl.Sgpds = nil, nil
	stbl.Subs, stbl.Saio, stbl.Saiz = nil, nil, nil

	children := []mp4.Box{stbl.Stsd, stts}
	if ctts != nil {
		children = append(children, ctts)
	}
	children = append(children, stsc, stsz)
	if stss != nil {
		children = append(children, stss)
	}
	children = append(children, co64)
	stbl.Children = children
}

// buildChunkTables 按 run 列表造 `stsc` 和 `co64`。
//
// 偏移先填 0：它们取决于 `mdat` 在哪儿，而那要等 `moov` 量出来才知道
// （见文件头）。`co64` 每项固定 8 字节，所以回头填真值不会让 `moov` 变大。
func buildChunkTables(runs []*run) (*mp4.StscBox, *mp4.Co64Box) {
	stsc := &mp4.StscBox{}
	co64 := &mp4.Co64Box{ChunkOffset: make([]uint64, len(runs))}
	for i, r := range runs {
		if n := len(stsc.Entries); n > 0 && stsc.Entries[n-1].SamplesPerChunk == uint32(r.count) {
			continue
		}
		// 采样描述符下标永远是 1：输出文件里每条轨只有一个采样描述。
		_ = stsc.AddEntry(uint32(i+1), uint32(r.count), 1)
	}
	return stsc, co64
}

// assignChunkOffsets 把每个 run 的真实字节偏移填进 `co64`。
//
// `co64` 的下标是**这条轨自己的** chunk 序号，不是全局的——每条轨的
// 偏移表是独立的，都从 0 数起。
func assignChunkOffsets(tracks []*track, dataStart uint64) {
	for _, t := range tracks {
		co64 := t.src.trak.Mdia.Minf.Stbl.Co64
		for _, r := range t.runs {
			co64.ChunkOffset[r.chunk] = dataStart + uint64(r.rel)
		}
	}
}

// copySamples 把采样数据按交错顺序搬进 `mdat`。
//
// 这是**唯一**长时间的一段（几百 MB 的读+写），所以取消在这里查。
// 一个 chunk 是半秒的内容（`interleaveWindowPerSec` 那个窗），于是取消
// 的响应时间就是「一个窗」的量级——亚秒。
func copySamples(ctx context.Context, w io.Writer, tracks []*track) error {
	files := make(map[int]*os.File, len(tracks))
	defer func() {
		for _, f := range files {
			_ = f.Close()
		}
	}()

	// run 是分轨记的，输出顺序却是时间窗交替（见 `layoutRuns`），
	// 所以按字节偏移重排一次就是文件里的顺序。
	all := make([]placed, 0, len(tracks[0].runs)+len(tracks[1].runs))
	for i, t := range tracks {
		for _, r := range t.runs {
			all = append(all, placed{off: r.rel, run: r, idx: i})
		}
	}
	sort.Slice(all, func(a, b int) bool { return all[a].off < all[b].off })

	buf := make([]byte, copyBufSize)
	for _, p := range all {
		if err := ctx.Err(); err != nil {
			return err
		}
		t := tracks[p.idx]
		f, ok := files[p.idx]
		if !ok {
			var err error
			if f, err = os.Open(t.src.path); err != nil {
				return fmt.Errorf("mux: 打开 %s：%w", t.src.path, err)
			}
			files[p.idx] = f
		}
		for k := 0; k < p.run.count; k++ {
			s := t.src.samples[p.run.first+k]
			if s.size == 0 {
				continue
			}
			// 一个关键帧能有几百 KB，超过中转缓冲区就临时扩一块。
			// 不扩的话这里是切片越界 panic，而交织跑在后台 goroutine 上。
			if int(s.size) > len(buf) {
				buf = make([]byte, s.size)
			}
			n, err := readFull(f, buf[:s.size], s.off)
			if err != nil {
				return fmt.Errorf("mux: 从 %s 读第 %d 字节那一个采样：%w", t.src.path, s.off, err)
			}
			if _, err := w.Write(buf[:n]); err != nil {
				return fmt.Errorf("mux: 写 mdat：%w", err)
			}
		}
	}
	return nil
}

// placed 是「文件里第几字节开始的那一段属于哪条轨」。
type placed struct {
	off int64
	run *run
	idx int
}

// readFull 从 off 处读满 len(buf[:size]) 字节。
func readFull(f *os.File, buf []byte, off int64) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := f.ReadAt(buf[n:], off+int64(n))
		n += m
		if err != nil {
			if n == len(buf) {
				break
			}
			return n, err
		}
	}
	return n, nil
}

// rescale 把一个时基下的数换算到另一个时基，四舍五入。
//
// 换的是**绝对时间戳**而不是逐段累加的差值，所以误差不会一段段往上叠：
// 整个时间轴最多偏半个时基（90000 的时基上是 5.5 微秒），而拖三分钟
// 也还是这个量级。真要是逐帧累加，同样的误差会攒成几十毫秒——那才是
// 「能播、就是慢慢漂移」那个阴险的失败模式。
func rescale(v int64, from, to uint32) int64 {
	if from == to || v == 0 {
		return v
	}
	n := int64(to) * v
	f := int64(from) / 2
	if n >= 0 {
		return (n + f) / int64(from)
	}
	return -((-n + f) / int64(from))
}

// kindOf 是给人看的「这是视频那条还是音频那条」。
func kindOf(s *source) string {
	if s.video {
		return "视频"
	}
	return "音频"
}
