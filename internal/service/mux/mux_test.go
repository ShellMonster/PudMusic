// 交织的断言。
//
// # 这里断的是**结果**，不是「有没有报错」
//
// 交织错了最常见的表现是「文件能打开、能播、只是慢慢漂」——报错的那一类
// 反而好查。所以每一条断言都在读**输出文件里真的有什么**：轨道数、每帧的
// DTS/PTS 顺序、`avcC`/`esds` 在不在、每个采样的字节是不是原来那些、
// chunk 偏移指到的地方是不是真的那些字节。
//
// 「两平台能播」留给真机，「交织对了」在这里。

package mux

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/Eyevinn/mp4ff/mp4"
)

// decodeOut 把交织出来的东西解回来。
func decodeOut(t *testing.T, path string) *mp4.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开产物：%v", err)
	}
	defer f.Close()
	out, err := mp4.DecodeFile(f)
	if err != nil {
		t.Fatalf("产物解不出来：%v", err)
	}
	return out
}

// trakByHandler 按 `hdlr` 找那条轨。
func trakByHandler(t *testing.T, f *mp4.File, handler string) *mp4.TrakBox {
	t.Helper()
	for _, trak := range f.Moov.Traks {
		if trak.Mdia != nil && trak.Mdia.Hdlr != nil && trak.Mdia.Hdlr.HandlerType == handler {
			return trak
		}
	}
	t.Fatalf("产物里没有 %s 轨", handler)
	return nil
}

// sampleTimes 把一条轨的采样读成 (解码时间, 呈现时间, 字节数) 三张表。
//
// 用 mp4ff 的 `GetSampleData` —— 它是**从 `stts`/`ctts`/`stsz`/`stsc`/`co64`
// 重新算**的，不是读我内存里那份表。所以它算对了，就说明我写进文件的那几张
// 表和文件里的采样数据是对得上的。
func sampleTimes(t *testing.T, trak *mp4.TrakBox) (dts, pts []uint64, size []uint32) {
	t.Helper()
	n := trak.GetNrSamples()
	// 采样号是 **1 起**的（`stsc` 的 `first_chunk` 和 `stsz` 的下标都是）。
	all, err := trak.GetSampleData(1, n)
	if err != nil {
		t.Fatalf("读 %d 个采样：%v", n, err)
	}
	if int(n) != len(all) {
		t.Fatalf("stsz 说有 %d 个采样，读回来是 %d 个", n, len(all))
	}
	var d uint64
	for _, s := range all {
		dts = append(dts, d)
		pts = append(pts, uint64(int64(d)+int64(s.CompositionTimeOffset)))
		size = append(size, s.Size)
		d += uint64(s.Dur)
	}
	return dts, pts, size
}

// ---- 主用例：一条正常的 MV ----

// TestMuxProducesTwoTracksThatCarryTheSourceVerbatim 钉住
// 「一个能播的 MP4，有两条轨，采样描述符是从源搬过去的」。
func TestMuxProducesTwoTracksThatCarryTheSourceVerbatim(t *testing.T) {
	dir := t.TempDir()
	spV, spA := videoSpec(120), audioSpec(4)
	video, audio := writePair(t, spV, spA)
	dst := filepath.Join(dir, "歌.mp4")

	res, err := Mux(video, audio, dst)
	if err != nil {
		t.Fatalf("交织：%v", err)
	}

	out := decodeOut(t, dst)

	// 两条轨，轨道号 1 / 2。
	//
	// 轨道号这一条不能省：两个源各自都是**独立的一条流**，B 站给它们的
	// 轨道号都是 1。不换的话输出里两条轨都叫 1，播放器只放第一条——
	// 而文件本身完全正常，不报错。
	if len(out.Moov.Traks) != 2 {
		t.Fatalf("产物里有 %d 条轨，要的是 2", len(out.Moov.Traks))
	}
	for i, trak := range out.Moov.Traks {
		if got := trak.Tkhd.TrackID; got != uint32(i+1) {
			t.Errorf("第 %d 条轨的轨道号是 %d，要的是 %d", i, got, i+1)
		}
	}

	// avcC / esds 原样在。
	vt := trakByHandler(t, out, "vide")
	if vt.Mdia.Minf.Stbl.Stsd.AvcX == nil || vt.Mdia.Minf.Stbl.Stsd.AvcX.AvcC == nil {
		t.Error("产物里视频轨的 avcC 不在了")
	}
	at := trakByHandler(t, out, "soun")
	if at.Mdia.Minf.Stbl.Stsd.Mp4a == nil {
		t.Error("产物里音频轨的 mp4a 不在了")
	} else if at.Mdia.Minf.Stbl.Stsd.Mp4a.Esds == nil {
		t.Error("产物里音频轨的 esds 不在了")
	}

	// 渐进式，不是分片式：moov 里不能有 mvex。
	if out.Moov.Mvex != nil {
		t.Error("产物的 moov 里还有 mvex——那是分片流的标记")
	}
	// 而且 moov 必须在 mdat 前面。
	assertMoovBeforeMdat(t, dst)

	if res.VideoSamples != spV.nrSamples {
		t.Errorf("视频采样数是 %d，源是 %d", res.VideoSamples, spV.nrSamples)
	}
	if res.AudioSamples != spA.nrSamples {
		t.Errorf("音频采样数是 %d，源是 %d", res.AudioSamples, spA.nrSamples)
	}
}

// assertMoovBeforeMdat 钉住「moov 在最前面」。
//
// 这一条是**能不能播**的分界：moov 落在最后面的文件（faststart 没做的那种）
// 网络流播不了，而这一票写本地文件正是为了系统播放器直接能打开。
// `DecodeFile` 把两样都收进来，所以只能自己看字节顺序。
func assertMoovBeforeMdat(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读产物：%v", err)
	}
	moov := bytes.Index(b, []byte("moov"))
	mdat := bytes.Index(b, []byte("mdat"))
	if moov < 0 || mdat < 0 {
		t.Fatalf("产物里 moov 在第 %d 字节、mdat 在第 %d 字节", moov, mdat)
	}
	if moov > mdat {
		t.Errorf("moov 在第 %d 字节而 mdat 在第 %d 字节——moov 必须在前面", moov, mdat)
	}
}

// TestSampleTimesAreCarriedVerbatim 钉住每帧的 DTS/PTS 顺序。
//
// 这是「能播但不漂」和「漂了」之间唯一的区别，所以它是最要紧的一条断言：
// 每帧的时长、呈现偏移、字节数都要和源一模一样，呈现时间要**非递减**，
// 解码时间要严格递增。
func TestSampleTimesAreCarriedVerbatim(t *testing.T) {
	dir := t.TempDir()
	spV, spA := videoSpec(150), audioSpec(5)
	video, audio := writePair(t, spV, spA)
	dst := filepath.Join(dir, "v.mp4")

	if _, err := Mux(video, audio, dst); err != nil {
		t.Fatalf("交织：%v", err)
	}
	out := decodeOut(t, dst)

	for _, tc := range []struct {
		handler string
		sp      dashSpec
	}{
		{"vide", spV},
		{"soun", spA},
	} {
		trak := trakByHandler(t, out, tc.handler)
		if got := trak.Mdia.Mdhd.Timescale; got != tc.sp.ts {
			t.Errorf("%s 轨的时基是 %d，源是 %d（交织不该改时基）", tc.handler, got, tc.sp.ts)
		}
		dts, pts, sizes := sampleTimes(t, trak)
		if len(dts) != tc.sp.nrSamples {
			t.Errorf("%s 有 %d 个采样，源是 %d", tc.handler, len(dts), tc.sp.nrSamples)
			continue
		}
		var d uint64
		for i := range dts {
			if dts[i] != d {
				t.Fatalf("%s 第 %d 帧的 DTS 是 %d，逐帧累加应该是 %d", tc.handler, i, dts[i], d)
			}
			if want := uint64(int64(d) + int64(ctsOf(tc.sp, i))); pts[i] != want {
				t.Errorf("%s 第 %d 帧的 PTS 是 %d，要的是 %d", tc.handler, i, pts[i], want)
			}
			d += uint64(tc.sp.dur)
			if got := sizes[i]; got != sizeOf(tc.sp, i) {
				t.Errorf("%s 第 %d 帧是 %d 字节，源是 %d", tc.handler, i, got, sizeOf(tc.sp, i))
			}
		}

		// 呈现时间**在解码顺序里不是单调的**——B 帧重排就是干这个的
		// （解码顺序 I P B B，呈现顺序 I B B P）。所以断的不是
		// 「pts[i] ≥ pts[i-1]」，而是排好序之后这一列：
		//
		//	严格递增      = 没有两帧挤在同一时刻（多了一帧）
		//	每一项都是帧长的整数倍 = 时间轴没有错位（漂了）
		//	全部落在 [0, 帧数×帧长) = 没有跑到时间轴外面
		//
		// 三条加起来就等于「不多不少正好每帧一次」：帧数个互不相同的、
		// 落在那个范围里的帧长整数倍，只可能是 0, d, 2d, …。
		//
		// ⚠️ 不能直接断成「第 i 项必须等于 i×d」——最后一个 GOP 常常是
		// 残的（I 和 P 到了，两个 B 没到），那两个采样会排在后面。
		// 真实流就是这样，写死成相等会误报。
		shown := append([]uint64(nil), pts...)
		sort.Slice(shown, func(i, j int) bool { return shown[i] < shown[j] })
		frameLen := uint64(tc.sp.dur)
		// 成片时长，换算回这条轨自己的时基。末帧的呈现时间必须落在它
		// 里面——落在外面的话播放器会把它砍掉，而少两帧是看不出来的。
		// （`tkhd` 的时长是电影时基的，两条轨的时基还不一样，所以要换。）
		declared := trak.Tkhd.Duration * uint64(trak.Mdia.Mdhd.Timescale) / uint64(out.Moov.Mvhd.Timescale)
		for i, got := range shown {
			if got%frameLen != 0 {
				t.Errorf("%s 按呈现时间排序后第 %d 帧是 %d，不是帧长 %d 的整数倍"+
					"（时间轴错位了，表现就是慢慢漂）", tc.handler, i, got, frameLen)
			}
			if i > 0 && shown[i-1] >= got {
				t.Errorf("%s 有两帧落在同一时刻：%d 和 %d", tc.handler, shown[i-1], got)
			}
			if got+frameLen > declared {
				t.Errorf("%s 第 %d 帧的呈现时间是 %d+%d，跑出了成片时长 %d"+
					"（播放器会把它砍掉，看起来就是丢帧）",
					tc.handler, i, got, frameLen, declared)
			}
		}
	}

	// 负的呈现偏移必须落在 ctts 的 version 1 上。视频的 B 帧就是负的，
	// 而 version 0 的 ctts 装不下负数——版本写错的话播放器按无符号读，
	// 那一帧的呈现时间变成一个巨大的正数，音画从这里开始漂。
	vt := trakByHandler(t, out, "vide")
	if vt.Mdia.Minf.Stbl.Ctts == nil {
		t.Fatal("视频轨没有 ctts，可它有 B 帧重排")
	}
	if vt.Mdia.Minf.Stbl.Ctts.Version != 1 {
		t.Errorf("ctts 的 version 是 %d，而它装着负的呈现偏移，要的是 1", vt.Mdia.Minf.Stbl.Ctts.Version)
	}
}

// TestSampleBytesSurviveTheMove 钉住「搬过去的是原来那些字节」。
//
// 交织只动容器，**一个字节都不许改**。而这是唯一能验到「chunk 偏移算对了」
// 的办法：`GetSampleData` 是按 `co64` 里的偏移去文件里取的，取出来的
// 内容不对就是偏移算错了（而偏移算错的表现是「能播、播出来是花的」）。
func TestSampleBytesSurviveTheMove(t *testing.T) {
	dir := t.TempDir()
	spV, spA := videoSpec(90), audioSpec(3)
	video, audio := writePair(t, spV, spA)
	dst := filepath.Join(dir, "v.mp4")

	if _, err := Mux(video, audio, dst); err != nil {
		t.Fatalf("交织：%v", err)
	}
	out := decodeOut(t, dst)
	raw, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("读产物：%v", err)
	}

	for _, tc := range []struct {
		handler string
		sp      dashSpec
	}{{"vide", spV}, {"soun", spA}} {
		trak := trakByHandler(t, out, tc.handler)
		offs := sampleOffsets(t, trak)
		if len(offs) != tc.sp.nrSamples {
			t.Fatalf("%s 从采样表算出 %d 帧，源是 %d 帧", tc.handler, len(offs), tc.sp.nrSamples)
		}
		for i, off := range offs {
			want := payloadOf(tc.sp, i)
			got, ok := bytesAt(raw, off, len(want))
			if !ok || !bytes.Equal(got, want) {
				t.Errorf("%s 第 %d 帧的字节不对（产物偏移 %d）：\n got %x\nwant %x",
					tc.handler, i, off, got, want)
			}
		}
	}
}

// sampleOffsets 按 `stsc` + `co64` + `stsz` 算出每一帧在文件里的偏移。
//
// **自己算**而不是用库的现成函数，是为了这一步真的在读交织写的那三张表：
// 采样在文件里是**交错**着的，所以偏移必须由 chunk 起点加上段内逐帧累加
// 得出——顺��顺着读验不到任何东西。偏移错的表现是「能播、播出来是花的」。
func sampleOffsets(t *testing.T, trak *mp4.TrakBox) []int64 {
	t.Helper()
	stbl := trak.Mdia.Minf.Stbl
	if stbl.Co64 == nil {
		t.Fatalf("这条轨用的是 stco 而不是 co64")
	}
	sizes := make([]uint32, 0, stbl.Stsz.GetNrSamples())
	if stbl.Stsz.SampleUniformSize > 0 {
		for range stbl.Stsz.GetNrSamples() {
			sizes = append(sizes, stbl.Stsz.SampleUniformSize)
		}
	} else {
		sizes = stbl.Stsz.SampleSize
	}

	var out []int64
	next := 0
	entry := 0
	for chunk := range stbl.Co64.ChunkOffset {
		// stsc 的每一项管到下一项为止。
		for entry+1 < len(stbl.Stsc.Entries) &&
			stbl.Stsc.Entries[entry+1].FirstChunk <= uint32(chunk+1) {
			entry++
		}
		per := int(stbl.Stsc.Entries[entry].SamplesPerChunk)
		off := stbl.Co64.ChunkOffset[chunk]
		for k := 0; k < per && next < len(sizes); k++ {
			out = append(out, int64(off))
			off += uint64(sizes[next])
			next++
		}
	}
	if next != len(sizes) {
		t.Errorf("chunk 盖住了 %d 帧，而 stsz 里有 %d 帧", next, len(sizes))
	}
	return out
}

// bytesAt 从 buf 的 off 处取 n 字节，越界返回 false。
func bytesAt(buf []byte, off int64, n int) ([]byte, bool) {
	if off < 0 || off+int64(n) > int64(len(buf)) {
		return nil, false
	}
	return buf[off : off+int64(n)], true
}

// TestTracksAreInterleavedByTimeWindow 钉住交织顺序。
//
// 不是「每个 chunk 都是同一条轨」就完了——那是一条轨写完再写另一条，
// 播放时要等第二条轨下完才出声。这里断的是**真的按时间窗交替**：
// 文件里的字节顺序应该是一段视频、一段音频、一段视频、一段音频。
func TestTracksAreInterleavedByTimeWindow(t *testing.T) {
	dir := t.TempDir()
	spV, spA := videoSpec(300), audioSpec(10)
	video, audio := writePair(t, spV, spA)
	dst := filepath.Join(dir, "v.mp4")

	if _, err := Mux(video, audio, dst); err != nil {
		t.Fatalf("交织：%v", err)
	}
	out := decodeOut(t, dst)

	// 把两条轨的 chunk 按**文件里的字节偏移**排成一串，看有没有交替。
	type at struct {
		off  uint64
		kind string
	}
	var order []at
	for _, trak := range out.Moov.Traks {
		kind := "soun"
		if trak.Mdia.Hdlr.HandlerType == "vide" {
			kind = "vide"
		}
		for _, off := range chunkOffsets(trak) {
			order = append(order, at{off: off, kind: kind})
		}
	}
	sort.Slice(order, func(i, j int) bool { return order[i].off < order[j].off })
	seq := make([]string, len(order))
	for i, o := range order {
		seq[i] = o.kind
	}

	if len(seq) < 4 {
		t.Fatalf("只有 %d 个 chunk，交织没有发生", len(seq))
	}
	switches := 0
	for i := 1; i < len(seq); i++ {
		if seq[i] != seq[i-1] {
			switches++
		}
	}
	// 三百个采样 = 10 秒 = 20 个窗，窗内先视频后音频 → 至少 19 次切换。
	if switches < 10 {
		t.Errorf("轨道只在 chunk 之间切换了 %d 次（chunk 顺序 %v），"+
			"看起来是一条轨写完才写另一条——那样播放时要等另一条下完才出声",
			switches, seq)
	}
}

// chunkOffsets 把一条轨的 chunk 偏移按顺序取出来。
func chunkOffsets(trak *mp4.TrakBox) []uint64 {
	stbl := trak.Mdia.Minf.Stbl
	if stbl.Co64 == nil {
		return nil
	}
	return stbl.Co64.ChunkOffset
}

// ---- 两轨时长对不上 ----

// TestMuxRefusesStreamsThatDoNotLineUp 钉住「两条流差太多就不写」。
//
// 差的症状是「播两分钟就没声了」，而那种文件**能播**——所以必须在这里
// 拦住。断的是结果：产物一个都不该有。
func TestMuxRefusesStreamsThatDoNotLineUp(t *testing.T) {
	dir := t.TempDir()
	// 视频 10 秒，音频 3 秒——差 7 秒，远超允许的两秒。
	video, audio := writePair(t, videoSpec(300), audioSpec(3))
	dst := filepath.Join(dir, "v.mp4")

	_, err := Mux(video, audio, dst)
	if err == nil {
		t.Fatal("两条流差了 7 秒还是交织成功了")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("对不上")) {
		t.Errorf("报错没说是两条流对不上：%v", err)
	}
	if _, statErr := os.Stat(dst); statErr == nil {
		t.Error("交织失败却把产物留在盘上了")
	}
}

// TestMuxAcceptsTheUsualFewMillisecondsOfSkew 钉住「正常的那点错位放行」。
//
// dash 的音视频本来就差几十毫秒（编解码器的 padding）。把阈值定死在
// 「一点都不能差」上，就会把绝大多数正常的 MV 拦下来——而拦下来的报错
// 用户根本没法处理。
func TestMuxAcceptsTheUsualFewMillisecondsOfSkew(t *testing.T) {
	dir := t.TempDir()
	video, audio := writePair(t, videoSpec(300), audioSpec(10))
	dst := filepath.Join(dir, "v.mp4")

	res, err := Mux(video, audio, dst)
	if err != nil {
		t.Fatalf("交织：%v", err)
	}
	if res.Drift > 50*time.Millisecond {
		t.Errorf("两条流差 %s，比预期的几十毫秒大得多", res.Drift)
	}
	if res.VideoDuration <= 0 || res.AudioDuration <= 0 {
		t.Errorf("时长算出来不对：视频 %s、音频 %s", res.VideoDuration, res.AudioDuration)
	}
}

// ---- 输入不对的那几种 ----

// TestMuxRejectsTheWrongKindOfStreams 钉住「视频配视频」要报错。
//
// 传错流的后果是产出一个只有画面没有声音（或者反过来）的文件——
// 而它能播。所以必须在门口问一句。
func TestMuxRejectsTheWrongKindOfStreams(t *testing.T) {
	dir := t.TempDir()
	video, audio := writePair(t, videoSpec(60), audioSpec(2))
	dst := filepath.Join(dir, "v.mp4")

	if _, err := Mux(video, video, dst); err == nil {
		t.Error("两条视频流交织成功了")
	}
	if _, err := Mux(audio, audio, dst); err == nil {
		t.Error("两条音频流交织成功了")
	}
}

// TestMuxRejectsATruncatedStream 钉住「下了一半的流」要报错。
//
// 下载分块失败时队列会停在半路上，而 `.part` 的长度是**预分配**到总长的
// （见 `download.prepare`）——所以文件大小是对的，里面有一段是零。
// 不看内容只说「交织成功了」，用户拿到的就是一个播两秒就没声的 MV。
func TestMuxRejectsATruncatedStream(t *testing.T) {
	dir := t.TempDir()
	spV, spA := videoSpec(300), audioSpec(10)
	video, audio := writePair(t, spV, spA)
	// 砍掉视频流的尾巴：前面的 moof 都在，就是最后一片没了。
	cut(t, video, 0.6)

	dst := filepath.Join(dir, "v.mp4")
	if _, err := Mux(video, audio, dst); err == nil {
		t.Error("视频流缺了尾巴还是交织成功了")
	}
	// 写坏的那个路径不能留在盘上：调用方给的是一个临时路径，留下来
	// 就是一份没人认领的垃圾，而用户在下载目录里看到的是完整的一首。
	if _, statErr := os.Stat(dst); statErr == nil {
		t.Error("交织失败却把半成品留在盘上了")
	}
}

// cut 把一个文件截到原长的 pct。
func cut(t *testing.T, path string, pct float64) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("问 %s：%v", path, err)
	}
	if err := os.Truncate(path, int64(float64(st.Size())*pct)); err != nil {
		t.Fatalf("截 %s：%v", path, err)
	}
}

// ---- 短到装不下交错 ----

// TestMuxHandlesStreamsShorterThanOneWindow 钉住「比一个时间窗还短」。
//
// 窗口是半秒。比它还短的流在「这个窗里先视频后音频」的循环里只跑一趟，
// 而那一趟里有几条不成立的分支——这条用例就是走它们。
func TestMuxHandlesStreamsShorterThanOneWindow(t *testing.T) {
	dir := t.TempDir()
	spV, spA := videoSpec(4), audioSpec(0.1)
	video, audio := writePair(t, spV, spA)
	dst := filepath.Join(dir, "v.mp4")

	res, err := Mux(video, audio, dst)
	if err != nil {
		t.Fatalf("交织：%v", err)
	}
	if res.VideoSamples != 4 {
		t.Errorf("视频采样数是 %d，源是 4", res.VideoSamples)
	}
	out := decodeOut(t, dst)
	if len(out.Moov.Traks) != 2 {
		t.Errorf("产物里有 %d 条轨", len(out.Moov.Traks))
	}
}
