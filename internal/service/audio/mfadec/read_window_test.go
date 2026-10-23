package mfadec

// 这一组钉的是「拆一条 MP4 要对源做多少次 Seek+Read」。
//
// # 为什么这件事值得钉一条测试
//
// 源是**在线流**：`internal/service/audio` 的 `StreamSource` 边下边播，字节
// 先落到本地缓存文件里，再从那儿读。所以一次 Seek+Read 落在**缓存文件**上
// ——不是网络往返，但仍然是「定位一次 + 读一次」的系统调用，而在机械盘、
// 网络盘、Safari 沙盒里，定位都不便宜。
//
// go-m4a v0.8.0 起，帧不再「一个 access unit 定位一次、读一次」，而是走一个
// **64 KiB 的读前窗口**：窗口里还有帧就直接交出去，窗口空了才去源上补一次。
// 一条 1414 帧的流，Seek+Read 从**上千次**降到**几十次**（v0.7.0 是 2839 次，
// v0.8.0 是 35 次——见下面两条用例的日志）。
//
// ⚠️ **这不是 go-m4a 的内部细节，是产品这一层的行为**：`openM4A` +
// `frameSource.next` 就是产品拆容器的那条路（Windows 上它喂给 Media
// Foundation；`internal/service/audio/flacdec` 的 MP4 那条路是同一套）。
// 哪天读窗口被去掉，这里会红——而症状是「在线流播起来卡」，
// **不报错、不崩、只是慢**。
//
// # 两种形状都要测，因为产品两种都遇到
//
//   - **普通封装**（`ftyp` + `moov` + `mdat`）：B 站音乐库下下来的 `.m4a`、
//     本地文件，是这一种。
//   - **分片**（初始化段 + `moof`/`mdat`）：**B 站在线流是这一种**——
//     实测的盒结构是 `ftyp → moov → sidx → (moof → mdat) × N`
//     （`internal/service/bili/testdata/lossless/README.md`）。分片的次数
//     和**片数**有关，所以它和普通封装不是同一个数量级，得分开钉。

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"testing"

	m4a "github.com/tphakala/go-m4a"
)

// seekCounter 数「对源做了多少次 Seek + Read」。
//
// ⚠️ 它数的是**调用次数**，不是读了多少字节——量的是「来回」而不是「流量」。
// 一条流从头到尾只读一遍字节、但每帧都重新定位一次，字节数一样而调用次数
// 差两个数量级，那才是要治的病。
type seekCounter struct {
	r     io.ReadSeeker
	seeks int
	reads int
}

func (c *seekCounter) Seek(off int64, whence int) (int64, error) {
	c.seeks++
	return c.r.Seek(off, whence)
}

func (c *seekCounter) Read(p []byte) (int, error) {
	c.reads++
	return c.r.Read(p)
}

// calls 是对源做的 Seek+Read 总数。
func (c *seekCounter) calls() int { return c.seeks + c.reads }

// memFile 是一个可写的内存文件（go-m4a 的 `NewWriter` 要 `io.WriteSeeker`，
// 因为 mdat 的长度是 Close 时回填的）。
type memFile struct {
	buf []byte
	pos int64
}

func (f *memFile) Write(p []byte) (int, error) {
	need := int(f.pos) + len(p)
	if need > len(f.buf) {
		grown := make([]byte, need)
		copy(grown, f.buf)
		f.buf = grown
	}
	copy(f.buf[f.pos:], p)
	f.pos += int64(len(p))
	return len(p), nil
}

func (f *memFile) Seek(off int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = off
	case io.SeekCurrent:
		abs = f.pos + off
	case io.SeekEnd:
		abs = int64(len(f.buf)) + off
	default:
		return 0, fmt.Errorf("memFile: 不认识的 whence %d", whence)
	}
	if abs < 0 {
		return 0, errors.New("memFile: 负偏移")
	}
	f.pos = abs
	return abs, nil
}

// aacASC 是 48 kHz 立体声 AAC-LC 的 AudioSpecificConfig。
//
// （audioObjectType = 2，采样率索引 3 = 48000，声道配置 2 = 立体声。）
var aacASC = []byte{0x11, 0x90}

// aacFrameBytes 是一帧载荷的字节数。
//
// ⚠️ **这个数是编的**，不是真的 AAC 码流——真帧的大小随内容变，而这里要的是
// 「一条形状已知的流」。它只用来让「帧数 → 字节数」这条线是线性的，好把
// 次数和帧数对起来看。真帧是 `mfadec/testdata` 里那两份素材（那两条走的是
// 逐样本比对的判据，不是次数）。
const aacFrameBytes = 600

// aacPayload 是第 i 帧的载荷：把序号写进去，于是「读回来的顺序对不对」
// 也能验——而光验「有几帧」的话，读串了也看不出来。
func aacPayload(i int) []byte {
	b := make([]byte, aacFrameBytes)
	binary.LittleEndian.PutUint64(b, uint64(i))
	return b
}

// aacMP4 造一条装着 n 帧 AAC 的 MP4（普通封装：`ftyp` + `moov` + `mdat`）。
func aacMP4(t *testing.T, n int) []byte {
	t.Helper()
	f := &memFile{}
	w, err := m4a.NewWriter(f, m4a.WriterConfig{
		SampleRate:   48000,
		Channels:     2,
		ASC:          aacASC,
		EncoderDelay: m4a.NoEdit,
	})
	if err != nil {
		t.Fatalf("建 MP4 写手：%v", err)
	}
	for i := range n {
		if err := w.WriteFrame(aacPayload(i)); err != nil {
			t.Fatalf("写第 %d 帧：%v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("收尾 MP4：%v", err)
	}
	return f.buf
}

// aacFragmentedMP4 造一条装着 n 帧 AAC 的**分片** MP4（初始化段 + 分片）。
//
// 每 framesPerSegment 帧收成一个分片——B 站的在线流就是这一种（一个 segment
// 里装着好几秒的音频）。
func aacFragmentedMP4(t *testing.T, n, framesPerSegment int) []byte {
	t.Helper()
	cfg := m4a.WriterConfig{
		SampleRate:   48000,
		Channels:     2,
		ASC:          aacASC,
		EncoderDelay: m4a.NoEdit,
	}
	data, err := m4a.InitSegment(cfg)
	if err != nil {
		t.Fatalf("造初始化段：%v", err)
	}
	fw, err := m4a.NewFragmentWriter(cfg)
	if err != nil {
		t.Fatalf("建分片写手：%v", err)
	}
	inSeg := 0
	for i := range n {
		if err := fw.WriteFrame(aacPayload(i)); err != nil {
			t.Fatalf("写第 %d 帧：%v", i, err)
		}
		inSeg++
		if inSeg == framesPerSegment {
			if data, err = fw.AppendSegment(data); err != nil {
				t.Fatalf("收第 %d 片：%v", i/framesPerSegment, err)
			}
			inSeg = 0
		}
	}
	if inSeg > 0 {
		if data, err = fw.AppendSegment(data); err != nil {
			t.Fatalf("收最后一片：%v", err)
		}
	}
	return data
}

// drainFrames 把一条流拆完，返回拆出来的帧和拆到哪儿出的错。
func drainFrames(src *frameSource) ([][]byte, error) {
	var out [][]byte
	for {
		f, err := src.next()
		if err != nil {
			return out, err
		}
		out = append(out, f)
	}
}

// checkFrames 断拆出来的帧数和内容都对。
//
// ⚠️ **内容也要断**：只断「有几帧」的话，一个「只读第一帧、后面都复读它」
// 的实现也能过——而那正是「少读了很多次」最容易长出来的样子。
func checkFrames(t *testing.T, got [][]byte, want int) {
	t.Helper()
	if len(got) != want {
		t.Fatalf("拆出 %d 帧，want %d", len(got), want)
	}
	for i, f := range got {
		if !bytes.Equal(f, aacPayload(i)) {
			t.Fatalf("第 %d 帧的内容不对（长度 %d，want %d）", i, len(f), aacFrameBytes)
		}
	}
}

// TestAPlainMP4IsNotReadOneSeekPerFrame 钉**普通封装**的那条路。
//
// 判据：读完一条 1414 帧的流，对源做的 Seek+Read **不超过帧数的 1/10**。
//
// 为什么是 1/10 而不是某个绝对数：读窗口的大小是 go-m4a 的实现细节，改了
// 也不该让这条红；而「每帧一次定位」和「一个窗口一次定位」之间差的是**两个
// 数量级**，1/10 落在这两者中间，两边都判得住。
//
// （实测：v0.7.0 是 2839 次，v0.8.0 是 33 次。）
func TestAPlainMP4IsNotReadOneSeekPerFrame(t *testing.T) {
	const frames = 1414
	data := aacMP4(t, frames)

	counter := &seekCounter{r: bytes.NewReader(data)}
	src, err := openM4A(counter)
	if err != nil {
		t.Fatalf("拆不开这条 MP4：%v", err)
	}
	got, err := drainFrames(src)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("拆帧出错：%v", err)
	}
	checkFrames(t, got, frames)

	limit := frames / 10
	t.Logf("%d 帧（%d 字节）：Seek %d 次 + Read %d 次 = %d 次，上限 %d",
		frames, len(data), counter.seeks, counter.reads, counter.calls(), limit)
	if counter.calls() > limit {
		t.Errorf("读完 %d 帧对源做了 %d 次 Seek+Read，超过上限 %d——"+
			"读前窗口没了，每帧都在重新定位"+
			"（go-m4a v0.7.0 是每帧一次定位：这条要 2839 次）",
			frames, counter.calls(), limit)
	}
}

// TestAFragmentedMP4IsNotReadOneSeekPerFrame 钉**分片**的那条路——
// **B 站在线流就是这一种**。
//
// 判据和上面那条同一个形状：不超过帧数的 1/10。
//
// ⚠️ 分片的次数和**片数**有关（每片至少要定位一次去读它的 `moof`），所以
// 这条比普通封装那条更容易顶到上限——这也是它要单立一条的原因：普通封装
// 那条绿了，不代表分片那条也绿。
//
// （实测：v0.7.0 是 3785 次，v0.8.0 是 53 次。）
func TestAFragmentedMP4IsNotReadOneSeekPerFrame(t *testing.T) {
	const frames = 4 * 468 // 四个分片，每片 468 帧（十秒 @48 kHz）
	const framesPerSegment = 468
	data := aacFragmentedMP4(t, frames, framesPerSegment)

	counter := &seekCounter{r: bytes.NewReader(data)}
	src, err := openM4A(counter)
	if err != nil {
		t.Fatalf("拆不开这条分片 MP4：%v", err)
	}
	got, err := drainFrames(src)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("拆帧出错：%v", err)
	}
	checkFrames(t, got, frames)

	limit := frames / 10
	t.Logf("%d 帧 / %d 片（%d 字节）：Seek %d 次 + Read %d 次 = %d 次，上限 %d",
		frames, frames/framesPerSegment, len(data),
		counter.seeks, counter.reads, counter.calls(), limit)
	if counter.calls() > limit {
		t.Errorf("读完 %d 帧对源做了 %d 次 Seek+Read，超过上限 %d——"+
			"读前窗口没了，每帧都在重新定位"+
			"（go-m4a v0.7.0 是每帧一次定位：这条要 3785 次）",
			frames, counter.calls(), limit)
	}
}

// errStreamDied 是「在线流拉不下来了」那类错误的形状。
//
// ⚠️ 这里**不 import** `internal/service/audio`（那是父包，父包 import 这一包，
// 反过来就成环了）。所以用自己的一份等价物——判据要的是「真实原因还在错误
// 链里」这个**性质**，不是那个具体的 sentinel。
var errStreamDied = errors.New("这一条流拉不下来了")

// dyingSource 读到 limit 字节之后开始报错——模拟在线流中途断掉。
type dyingSource struct {
	r     io.ReadSeeker
	limit int64
	read  int64
}

func (d *dyingSource) Seek(off int64, whence int) (int64, error) {
	return d.r.Seek(off, whence)
}

func (d *dyingSource) Read(p []byte) (int, error) {
	if d.read >= d.limit {
		return 0, errStreamDied
	}
	if int64(len(p)) > d.limit-d.read {
		p = p[:d.limit-d.read]
	}
	n, err := d.r.Read(p)
	d.read += int64(n)
	return n, err
}

// TestAStreamThatDiesKeepsItsRealReason 钉「**流中途拉不动了**」时，
// 拆帧报出来的错**保住了真实原因**。
//
// # 为什么这一条要紧
//
// v0.7.0 把「源读不动了」一律说成 `go-m4a: corrupt container`——**真实原因
// 被吞掉了**。而上游 `internal/service/bili/friendly.go` 靠
// `errors.Is(err, audio.ErrStreamDead)` 把「网络断了」翻译成
// 「连不上 B 站这条音频流了（网络或地址的问题，不是歌坏了）」；原因被吞了，
// 用户看到的就是一句**假话**——歌没坏，是网断了。
//
// v0.8.0 起，读帧失败时错误是**包着底层原因**的，于是 `errors.Is` 能穿过去。
//
// ⚠️ **这一条还钉住另一件事**：出错**不能**是 `io.EOF`。
// `mfadec/decoder.go` 的 `fill()` 认的是 `errors.Is(err, io.EOF)`——它把
// io.EOF 当成「流完了」，于是会 drain 一下然后**安静地停住**。一个拉不动的
// 流要是报成 io.EOF，症状就是「歌放了一半不响了，界面上什么错都没有」——
// 和「文件坏了」长得一模一样，而查起来完全不是那回事。
func TestAStreamThatDiesKeepsItsRealReason(t *testing.T) {
	const frames = 1414
	data := aacMP4(t, frames)

	// 断在一半：够 `NewReader` 把 `moov` 读完，不够把整条流读完。
	src := &dyingSource{r: bytes.NewReader(data), limit: int64(len(data)) / 2}
	fs, err := openM4A(src)
	if err != nil {
		// 断得太早的话，打开就失败了。那也是一个「真实原因还在」的场合。
		if !errors.Is(err, errStreamDied) {
			t.Fatalf("打开就失败，而错误里没有真实原因：%v", err)
		}
		if errors.Is(err, io.EOF) {
			t.Fatalf("打开失败却报成 io.EOF：%v", err)
		}
		return
	}

	_, err = drainFrames(fs)
	if err == nil {
		t.Fatal("流拉不动了，却报成功")
	}
	t.Logf("拆到一半断掉的错误：%v", err)
	if !errors.Is(err, errStreamDied) {
		t.Errorf("错误 = %v，**没有**包着真实原因"+
			"（v0.7.0 会把它说成「go-m4a: corrupt container」，"+
			"于是上层把「网络断了」说成「歌坏了」）", err)
	}
	if errors.Is(err, io.EOF) {
		t.Errorf("错误 = %v，报成了 io.EOF——"+
			"上层会当成「流完了」安静地停住，而它其实是拉不动了", err)
	}
}
