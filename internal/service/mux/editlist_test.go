// 编辑列表、畸形输入、取消这三组。
//
// 为什么单独一个文件：它们测的都是**同一件事的不同侧面**——「输入不是
// 那个最规整的形状时，交织还能不能做对」。而它们要造的那些素材
// （一条晚开始的流、每帧 cts 都偏一点的流、一个没有 stbl 的 moof、
// 一个不带 default-base-is-moof 的分片）都要 muxtest 支持，所以素材
// 那一半在 `muxtest/dash.go` 里。

package mux

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Eyevinn/mp4ff/bits"
	"github.com/Eyevinn/mp4ff/mp4"

	"github.com/ShellMonster/Biu_Go/internal/service/mux/muxtest"
)

// TestALateAudioStreamGetsAnEmptyEditInTheRightTimescale 钉住编辑列表。
//
// 场景：**音轨比视频轨晚开始一秒**（音轨开头有一段静音是真实情况）。
// 那条晚的轨要补一段**空编辑**（`elst` 里 `media_time = -1` 的那一项），
// 于是播放时它是「先静着，到点了再出声」。
//
// 🔴 这一条钉的是 `elst` 里两个字段的**单位不一样**，而写反了看不出来：
//
//	`segment_duration`  单位是**电影**时基（本例 90000）
//	`media_time`         单位是**这条轨自己的媒体**时基（本例 48000）
//
// 拿电影时基的数去填 `media_time`，音频轨就会被按 90000 解释那个 1 秒——
// 实际是 1.875 秒。症状：开头多出小半秒没声音，之后音画慢慢错开。
// 而两条轨首帧都是 0 的**常见**情况下这一段代码根本不执行，所以它躲得
// 过所有「能播」的验收。
func TestALateAudioStreamGetsAnEmptyEditInTheRightTimescale(t *testing.T) {
	dir := t.TempDir()
	// 视频 0 秒起、5 秒长；音频 1 秒起、4 秒长（到 5 秒结束）。
	spV := muxtest.VideoSpec(150)
	spA := muxtest.AudioSpecStartingLate(4, 1)
	video, audio := writePair(t, spV, spA)
	dst := filepath.Join(dir, "v.mp4")

	if _, err := Mux(context.Background(), video, audio, dst); err != nil {
		t.Fatalf("交织：%v", err)
	}
	out := decodeOut(t, dst)
	movieTS := out.Moov.Mvhd.Timescale

	at := trakByHandler(t, out, "soun")
	if at.Edts == nil || len(at.Edts.Elst) == 0 {
		t.Fatal("晚开始的音频轨没有编辑列表——播放时会从 0 就出声")
	}
	entries := at.Edts.Elst[0].Entries
	if len(entries) != 2 {
		t.Fatalf("编辑列表有 %d 项，要的是 2（一段空编辑 + 一段取媒体）", len(entries))
	}
	// 第一段：空编辑，时长 1 秒，单位是**电影**时基。
	wantDelay := uint64(movieTS) // 1 秒
	if entries[0].MediaTime != -1 {
		t.Errorf("第一项的 media_time 是 %d，要的是 -1（空编辑）", entries[0].MediaTime)
	}
	if entries[0].SegmentDuration != wantDelay {
		t.Errorf("空编辑的时长是 %d 个电影刻度，要的是 %d（1 秒）",
			entries[0].SegmentDuration, wantDelay)
	}
	// 第二段从媒体的 0 开始取。
	//
	// 晚的那一段**只**由第一项那个空编辑承担（每条源流的时间轴都已经
	// 各自归零了——媒体时间轴从 0 起是对的，而两条流 `tfdt` 的差是
	// `source.origin` 记着的，见那里）。所以 `media_time` 是 0。
	if entries[1].MediaTime != 0 {
		t.Errorf("第二项的 media_time 是 %d，要的是 0（媒体时间轴已归零，"+
			"那一秒在第一项的空编辑里）", entries[1].MediaTime)
	}

	// 电影时长 = 5 秒（视频那条的长度），音频被空编辑推后一秒后仍然到 5 秒。
	if got := movieSeconds(out.Moov.Mvhd.Duration, movieTS); got < 4.9 || got > 5.1 {
		t.Errorf("电影时长是 %.3f 秒，要的是 5 秒", got)
	}
}

// movieSeconds 把电影时基下的刻度换算成秒。
func movieSeconds(dur uint64, ts uint32) float64 {
	return float64(dur) / float64(ts)
}

// TestTheEditListMediaTimeIsInTheTrackTimescale 钉住 `elst` 里两个字段的
// **单位不一样**。
//
//	`segment_duration`  单位是**电影**时基（本例 90000）
//	`media_time`         单位是**这条轨自己的媒体**时基（本例 48000）
//
// 拿电影时基的数去填 `media_time`，播放器会按 48000 去解释那个 1.875 秒，
// 于是这条轨的**开头**被多砍掉 0.875 秒——文件照样能播、能拖，只是声音
// 慢半拍而且之后一直对不上画面。
//
// 这一条是被评审逮出来的（第一版写的是 `t.mediaTime = start`，而 `start`
// 是电影时基下的数）。它躲过了所有「能播」的验收：`media_time` **只在
// 首帧不落在媒体时间 0 上的时候才非零**，而那种流要靠「每个采样的呈现
// 偏移都偏一点」才造得出来（真实流里那是编码器 priming，比如 AAC 的
// 编码延迟）。
func TestTheEditListMediaTimeIsInTheTrackTimescale(t *testing.T) {
	dir := t.TempDir()
	// 音频每一帧的呈现偏移都 +1024（编码器 priming 那种），于是它的
	// 首帧落在媒体时间 1024 上，`media_time` 那个字段才非零。
	const priming = 1024
	spA := muxtest.AudioSpec(4).WithCtsBias(priming)
	spV := muxtest.VideoSpec(120)
	video, audio := writePair(t, spV, spA)
	dst := filepath.Join(dir, "v.mp4")

	if _, err := Mux(context.Background(), video, audio, dst); err != nil {
		t.Fatalf("交织：%v", err)
	}
	out := decodeOut(t, dst)
	movieTS := out.Moov.Mvhd.Timescale

	at := trakByHandler(t, out, "soun")
	if at.Edts == nil || len(at.Edts.Elst) == 0 {
		t.Fatalf("音频轨没有编辑列表（首帧落在媒体时间 %d 上，应该有一项）", priming)
	}
	entries := at.Edts.Elst[0].Entries
	last := entries[len(entries)-1]

	if last.MediaTime != priming {
		wrongMovieTS := rescale(int64(priming), muxtest.AudioTimescale, movieTS)
		t.Errorf("media_time 是 %d，要的是 %d（媒体时基下的刻度）。"+
			"填 %d（电影时基下的同一个数）的话，播放器按媒体时基解释它，"+
			"这条轨的开头会被多砍掉 %g 秒——能播、能拖，只是声音慢半拍",
			last.MediaTime, priming, wrongMovieTS,
			float64(wrongMovieTS-priming)/float64(muxtest.AudioTimescale))
	}
}

// TestMuxStopsWhenTheContextIsCancelled 钉住「交织认取消」。
//
// 为什么这条要紧：交织一个几百 MB 的 MV 要读一遍再写一遍，那十几秒里用户
// 要能退出、要能删掉这一项。`Manager.Close` 会等后台那条线退出，而那条线
// 正卡在交织里——**不认 ctx 的话「退出应用」就变成「等交织完」**。
//
// 而且它不留垃圾：被取消之后那个产物路径上**不能**有文件（用户在自己的
// 下载目录里看到的是完整的一个文件）。
func TestMuxStopsWhenTheContextIsCancelled(t *testing.T) {
	dir := t.TempDir()
	// 多一点采样，让「搬数据」那一段真的有活可干。
	spV, spA := muxtest.VideoSpec(6000), muxtest.AudioSpec(200)
	video, audio := writePair(t, spV, spA)
	dst := filepath.Join(dir, "v.mp4")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 一上来就取消

	_, err := Mux(ctx, video, audio, dst)
	if err == nil {
		t.Fatal("已经取消的 ctx 交织居然成功了")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("报错不是 context.Canceled：%v", err)
	}
	if _, statErr := os.Stat(dst); statErr == nil {
		t.Error("被取消之后产物还留在盘上")
	}
}

// TestAMoovWithoutSampleTablesIsRefusedNotPanicked 钉住「残的 moov 要报错，
// 不能 panic」。
//
// 为什么这条要紧：交织跑在**后台 goroutine 上**，那里一个 panic 会一路冒到
// 下载队列的循环（`runOne` → `loop`，谁都不 recover）——表现是**所有下载
// 一起停住而界面上没有任何报错**。而 `.part` 是从网上下来的字节，一个解得开
// 的、但少了 `minf`/`stbl`/`stsd` 的 `moov` 是完全能收到的。
func TestAMoovWithoutSampleTablesIsRefusedNotPanicked(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "video.m4s")
	audio := filepath.Join(dir, "audio.m4s")
	muxtest.Write(t, audio, muxtest.AudioSpec(2))
	writeMoovWithoutSampleTables(t, video)

	// 不 panic 是第一位的：`defer recover` 兜住，兜住就直接判失败。
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("交织一个残的 moov 的时候 panic 了：%v"+
				"（后台 goroutine 上的 panic 会杀掉整个下载队列）", r)
		}
	}()
	_, err := Mux(context.Background(), video, audio, filepath.Join(dir, "v.mp4"))
	if err == nil {
		t.Fatal("一个没有采样表的 moov 也交织成功了")
	}
	if !strings.Contains(err.Error(), "minf") {
		t.Errorf("报错没说是缺哪样：%v", err)
	}
}

// writeMoovWithoutSampleTables 造一条「有 moov、但轨里没有 minf/stbl/stsd」
// 的流。
//
// mp4ff 的 `CreateEmptyTrak` 一定会把那几个盒子建齐，所以这条只能手搓：
// 只放 `tkhd` 和 `mdia`（`mdhd` + `hdlr`）——解得开，而 `stbl` 真的没有。
func writeMoovWithoutSampleTables(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("建 %s：%v", path, err)
	}
	defer f.Close()

	hdlr, err := mp4.CreateHdlr("vide")
	if err != nil {
		t.Fatalf("造 hdlr：%v", err)
	}
	mdia := &mp4.MdiaBox{}
	mdia.AddChild(&mp4.MdhdBox{Timescale: muxtest.VideoTimescale})
	mdia.AddChild(hdlr)

	trak := mp4.NewTrakBox()
	tkhd := mp4.CreateTkhd()
	tkhd.TrackID = 1
	trak.AddChild(tkhd)
	trak.AddChild(mdia)

	moov := mp4.NewMoovBox()
	mvhd := mp4.CreateMvhd()
	moov.AddChild(mvhd)
	moov.AddChild(trak)

	if err := mp4.CreateFtyp().Encode(f); err != nil {
		t.Fatalf("写 ftyp：%v", err)
	}
	if err := moov.Encode(f); err != nil {
		t.Fatalf("写 moov：%v", err)
	}
	// 再塞一段随机的分片字节，让文件不至于短到被当成「不像 dash 流」，
	// 从而真的走到那条缺 `minf` 的路上。
	if _, err := f.Write(make([]byte, 64)); err != nil {
		t.Fatalf("补字节：%v", err)
	}
}

// TestAFragmentWithoutTheMoofBaseFlagIsStillFound 钉住基准位置。
//
// ISO/IEC 14496-12 §8.8.7：`tfhd` 既没有 `base-data-offset`、也没有
// `default-base-is-moof` 时，数据基准是**这个 moof 后面的第一个字节**。
//
// ⚠️ 用 moof 的**起点**当基准错的量是整个 moof 的长度（几 KB），而那几 KB
// 是**解得开**的分片元数据——于是采样被换成了 `trun` 的字节。出来的文件
// 轨道数对、时长对、能播，播出来是花的。
//
// mp4ff 的 `CreateTfhd` 总是带 `default-base-is-moof`，所以这条素材要自己
// 把那个标志去掉、并把 `data_offset` 重新算成相对「moof 之后」。
func TestAFragmentWithoutTheMoofBaseFlagIsStillFound(t *testing.T) {
	dir := t.TempDir()
	sp := muxtest.VideoSpec(20)
	normal := filepath.Join(dir, "normal.m4s")
	muxtest.Write(t, normal, sp)

	patched := filepath.Join(dir, "patched.m4s")
	patchFragmentBaseFlag(t, normal, patched, sp)
	audio := filepath.Join(dir, "audio.m4s")
	muxtest.Write(t, audio, muxtest.AudioSpec(1))

	dst := filepath.Join(dir, "v.mp4")
	if _, err := Mux(context.Background(), patched, audio, dst); err != nil {
		t.Fatalf("交织：%v", err)
	}
	out := decodeOut(t, dst)
	vt := trakByHandler(t, out, "vide")
	offs := sampleOffsets(t, vt)
	if len(offs) != sp.Samples() {
		t.Fatalf("算出 %d 帧，源里有 %d 帧", len(offs), sp.Samples())
	}
	raw, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("读产物：%v", err)
	}
	for i := range sp.Samples() {
		want := sp.PayloadOf(i)
		got, ok := bytesAt(raw, offs[i], len(want))
		if !ok || !bytes.Equal(got, want) {
			t.Fatalf("第 %d 帧的字节不对（产物偏移 %d）——基准位置算错了，"+
				"读回来的是 moof 的元数据而不是样本数据", i, offs[i])
		}
	}
}

// patchFragmentBaseFlag 把一条流里每个 `tfhd` 的 `default-base-is-moof`
// 标志去掉，并把 `data_offset` 重新算成相对「moof 之后」。
func patchFragmentBaseFlag(t *testing.T, in, out string, sp streamSpec) {
	t.Helper()
	raw, err := os.ReadFile(in)
	if err != nil {
		t.Fatalf("读 %s：%v", in, err)
	}
	f, err := os.Create(out)
	if err != nil {
		t.Fatalf("建 %s：%v", out, err)
	}
	defer f.Close()

	var pos int64
	for pos < int64(len(raw)) {
		hdr, err := readBoxHeader(bytes.NewReader(raw[pos:]))
		if err != nil {
			t.Fatalf("读第 %d 个 box 的头：%v", pos, err)
		}
		if hdr.name != "moof" {
			if _, err := f.Write(raw[pos : pos+int64(hdr.size)]); err != nil {
				t.Fatalf("原样搬 %s：%v", hdr.name, err)
			}
			pos += int64(hdr.size)
			continue
		}
		// 解出 moof，把标志和偏移改掉，再写回去。
		body := raw[pos : pos+int64(hdr.size)]
		box, err := mp4.DecodeBoxSR(uint64(pos), bits.NewFixedSliceReader(body))
		if err != nil {
			t.Fatalf("解 moof：%v", err)
		}
		moof := box.(*mp4.MoofBox)
		moofSize := moof.Size()
		for _, traf := range moof.Trafs {
			traf.Tfhd.Flags &^= mp4.TfhdDefaultBaseIsMoofFlag
			for _, trun := range traf.Truns {
				if trun.HasDataOffset() {
					// 新的基准是 `moofStart + moofSize`，所以偏移要减掉
					// `moofSize` 才落到同样的绝对位置。
					trun.DataOffset -= int32(moofSize)
				}
			}
		}
		if err := moof.Encode(f); err != nil {
			t.Fatalf("写改过的 moof：%v", err)
		}
		pos += int64(hdr.size)
	}
}

// TestBothTracksStartingBeforeZeroKeepTheirRealGap 钉住「两条轨的首帧都在
// 媒体时间 0 之前」。
//
// 这条是被评审第一轮逮出来的。真触发条件是**两条源流的首个呈现时间都落在
// 0 之前**——两条流各自的编码器 priming / B 帧重排都会这样，而
// `muxtest.WithCtsBias` 就是造它的。
//
// 症状：第一版那条分支写的是 `delay = -start`（没有减掉 `base`），于是
// 每一条轨都被挪到电影时间 0——**它们之间真实的先后被抹掉**：
//
//	两条差 12 ms  → 出来两条同时出现
//	差 416.7 ms   → 出来画面比声音早 416.7 ms（约 12.5 帧 @30fps）
//
// ⚠️ 它躲过了所有既有用例：「**只有一条**在 0 之前」是常见情形，那时
// `-start` 恰好等于正确答案，于是另一支永远走不到。
func TestBothTracksStartingBeforeZeroKeepTheirRealGap(t *testing.T) {
	// 首帧落在电影时间的哪儿 = 空编辑的长度 + 它自己首帧在媒体时间 0 之前
	// 的那一段（媒体 m → 电影 `D + m`）。
	//
	// ⚠️ **不是**「空编辑的长度」那一样：两条轨的首帧都在 0 之前时，两条
	// 轨的 `D` **本来就该相等**（都等于 `-base`），而它们首帧的先后是在
	// `D + firstPTS` 那里分开的。量错了这一样，这一条用例就会在坏的那个
	// 版本上照样通过。
	for _, tc := range []struct {
		name        string
		videoBias   int32   // 90000 时基
		audioBias   int32   // 48000 时基
		wantVideoMS float64 // 视频首帧在电影时间上的位置
		wantAudioMS float64 // 音频首帧在电影时间上的位置
	}{
		{"两条差 12 毫秒", -3000, -1024, 0, 12},
		{"音频早半秒", -3000, -24000, 466.7, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			spV := muxtest.VideoSpec(120).WithCtsBias(tc.videoBias)
			spA := muxtest.AudioSpec(4).WithCtsBias(tc.audioBias)
			video, audio := writePair(t, spV, spA)
			dst := filepath.Join(dir, "v.mp4")

			if _, err := Mux(context.Background(), video, audio, dst); err != nil {
				t.Fatalf("交织：%v", err)
			}
			out := decodeOut(t, dst)
			movieTS := out.Moov.Mvhd.Timescale

			first := make(map[string]float64, 2)
			for _, trak := range out.Moov.Traks {
				if trak.Edts == nil || len(trak.Edts.Elst) == 0 {
					t.Fatalf("%s 轨没有编辑列表——首帧在媒体时间 0 之前时"+
						"必须有一段空编辑", trak.Mdia.Hdlr.HandlerType)
				}
				d := trak.Edts.Elst[0].Entries[0].SegmentDuration
				src := spA
				if trak.Mdia.Hdlr.HandlerType == "vide" {
					src = spV
				}
				// 媒体 m → 电影 `D + rescale(m)`，m 是这一条的首帧呈现时间。
				lead := rescale(int64(src.CtsOf(0)), src.Timescale(), movieTS)
				first[trak.Mdia.Hdlr.HandlerType] =
					(float64(d) + float64(lead)) / float64(movieTS) * 1000
			}
			if math.Abs(first["vide"]-tc.wantVideoMS) > 1 {
				t.Errorf("视频首帧出现在 %.1f ms，真值是 %.1f ms（got %v）",
					first["vide"], tc.wantVideoMS, first)
			}
			if math.Abs(first["soun"]-tc.wantAudioMS) > 1 {
				t.Errorf("音频首帧出现在 %.1f ms，真值是 %.1f ms（got %v）"+
					"——两条轨首帧都在 0 之前时，它们之间真实的先后被抹掉了",
					first["soun"], tc.wantAudioMS, first)
			}
		})
	}
}

// TestATrackCarriesExactlyOneEditList 钉住「一条轨最多一个 `edts`」。
//
// 这条是被评审第三轮逮出来的（`review/duplicate-edts-macos-cannot-play.txt`），
// 而且它是最重的一个：**产物在 macOS 上根本没有画面**。
//
// # 症状
//
// AVFoundation（= QuickTime 的引擎）读同一个文件：
//
//	交织器的产物：playable=true tracks=1  ← 只剩音频，视频轨整条被丢掉
//	              decodedFrame@3s: Cannot Open
//	对照组（只删掉多出来的那个 edts）：tracks=2，3 秒处那一帧解得出来
//
// **差的就是那 48 个字节**。而这不是素材的运气：另外两个产物也一样，
// 其中一条**连打开都打不开**（`playable=false tracks=0`）。
//
// # 根因
//
// `trimTrakChildren` 把 `edts` 插在 `tkhd` 后面（惯例位置），然后又问
// 「最后一项是不是 `edts`」——而最后一项是 `mdia`，于是又追加了一次。
//
// # 为什么测试抓不到
//
// mp4ff 的解码器**不报错**：它把后一个 `edts` 盖在 `TrakBox.Edts` 那个
// 字段上，而测试断言的一律是 `trak.Edts.Elst[0]`——**正好是被盖掉的那个
// 指针**。所以「写了两遍」这件事在测试里完全不可见。
//
// 所以这条断言数的是 `trak.Children` 里 `*mp4.EdtsBox` 的**个数**，
// 而不是「那个字段是不是非空」。
func TestATrackCarriesExactlyOneEditList(t *testing.T) {
	dir := t.TempDir()
	// 用「音轨晚半秒起」那种素材：它一定走出 `edts` 那一支，而两条轨
	// 首帧**都**在 0 之前的那种是上一条用例管的事。
	spV := muxtest.VideoSpec(120)
	spA := muxtest.AudioSpecStartingLate(4, 0.5)
	video, audio := writePair(t, spV, spA)
	dst := filepath.Join(dir, "v.mp4")

	if _, err := Mux(context.Background(), video, audio, dst); err != nil {
		t.Fatalf("交织：%v", err)
	}
	out := decodeOut(t, dst)

	for _, trak := range out.Moov.Traks {
		n := 0
		for _, c := range trak.Children {
			if _, ok := c.(*mp4.EdtsBox); ok {
				n++
			}
		}
		if n > 1 {
			var names []string
			for _, c := range trak.Children {
				names = append(names, c.Type())
			}
			t.Errorf("%s 轨里有 %d 个 edts（%v）——ISO/IEC 14496-12 规定最多一个，"+
				"而 macOS 的 AVFoundation 遇到两个就把**整条轨**丢掉"+
				"（表现为「文件能打开、只有声音、拖到中间解不出那一帧」）",
				trak.Mdia.Hdlr.HandlerType, n, names)
		}
		if n == 1 {
			// 惯例上紧跟在 tkhd 后面（§8.3.1）。
			idx := -1
			for i, c := range trak.Children {
				if c.Type() == "edts" {
					idx = i
				}
			}
			if trak.Children[max(idx-1, 0)].Type() != "tkhd" {
				t.Errorf("%s 轨的 edts 排在 %d，而它前面是 %s（惯例上应该在 tkhd 后面）",
					trak.Mdia.Hdlr.HandlerType, idx, trak.Children[idx-1].Type())
			}
		}
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
