package audio

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// 解码错误的单独上报。
//
// # 为什么这一层值得单独一组用例
//
// MyGo 的流接口把解码失败**折成「读到文件尾」**（`mygo/audio/source.go`
// 的 `stream.decode`：任何 `err != nil` 都只置 `atEOF = true`，具体错误
// 不往外传）。它这么设计是对的——播放器只关心「还有没有样本」。
//
// 但对我们不行。`Playing()` 在暂停、停止、关闭、播完时**都是 false**
// （`mygo/audio/audio.go`），而「解码失败」也表现为 `Playing() == false`。
// 于是这三件事在它眼里一模一样：
//
//	歌播完了   → 该自动下一首
//	网络断了   → 该报错并提示换流
//	容器坏了   → 该说「这个文件解不了」
//
// `spec.md`「播放状态机」说的「解码失败不能被当成播放结束」就是这件事，
// `errorDecoder` 是它的实现。而**实现对了不代表接线对了**——所以这里
// 钉的是「真实原因真的冒出来了」，不是「代码里有那么个类型」。

// errDecode 是这一组里那只「解码坏了」的错。
var errDecode = errors.New("这一帧解不出来")

// fakeDecoder 是一只可控的解码器。
//
// 自己写而不是用真素材：真解码器要喂它一段对的字节才能走到失败那一步，
// 而「失败」本身反而是最难精确造出来的那个状态。
type fakeDecoder struct {
	mu      sync.Mutex
	samples int   // 还能读出多少采样（不是帧）
	readErr error // 读完之后返回什么
	seekErr error // SetPosition 返回什么
	closed  bool
}

func (d *fakeDecoder) SampleRate() int { return 48000 }
func (d *fakeDecoder) Channels() int   { return 2 }

func (d *fakeDecoder) Read(buf []float32) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.samples <= 0 {
		return 0, d.readErr
	}
	n := min(len(buf), d.samples)
	d.samples -= n
	return n, nil
}

func (d *fakeDecoder) SetPosition(int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.seekErr
}

func (d *fakeDecoder) Len() int64 { return -1 }

func (d *fakeDecoder) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}

func (d *fakeDecoder) wasClosed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed
}

// TestADecodeFailureIsNotAnEndOfStream 钉主断言。
//
// 解码器报错时 MyGo 会把它折成「读到文件尾」——所以**上层唯一能看到真实
// 原因的地方就是这一层**。漏了它，界面上的表现是「歌放完了」，而且没有
// 任何错误可以报。
func TestADecodeFailureIsNotAnEndOfStream(t *testing.T) {
	d := &fakeDecoder{samples: 512, readErr: errDecode}
	ed := &errorDecoder{Decoder: d}

	if _, err := ed.Read(make([]float32, 512)); err != nil {
		t.Fatalf("第一次读就报错了：%v", err)
	}
	if _, err := ed.Read(make([]float32, 512)); err == nil {
		t.Fatal("解码器该报错了却什么都没说")
	}

	err := ed.Err()
	if err == nil {
		t.Fatal("解码失败之后 Err() 是 nil——上层会以为歌放完了")
	}
	if !errors.Is(err, errDecode) {
		t.Errorf("err = %v，want %v（真实原因被折掉了）", err, errDecode)
	}
	// ⚠️ **最要紧的一条**：它绝不能变成 io.EOF。EOF 在上层是「歌放完了」。
	if errors.Is(err, io.EOF) {
		t.Fatal("解码失败被说成了「读到文件尾」——界面会自动下一首")
	}
}

// TestAFinishedStreamIsNotADecodeFailure 钉反过来的那一半。
//
// 放完了不是坏了。报上去一个错的话，播放条上会出现一句「出了点问题」，
// 而用户只是听完了一首歌。
func TestAFinishedStreamIsNotADecodeFailure(t *testing.T) {
	d := &fakeDecoder{samples: 512, readErr: io.EOF}
	ed := &errorDecoder{Decoder: d}

	for range 3 {
		_, _ = ed.Read(make([]float32, 512))
	}
	if err := ed.Err(); err != nil {
		t.Errorf("正常放完却报了 %v，want nil", err)
	}
}

// TestAFailedSeekIsNotAnEndOfStream 盯 seek 失败。
//
// 拖进度条拖到一个坏位置之后，MyGo 一样把它折成「流到头了」，表现和
// 「歌放完了」完全一样。只截 `Read` 不截 `SetPosition` 的话，界面会安静
// 地停住，一个字都不说。
func TestAFailedSeekIsNotAnEndOfStream(t *testing.T) {
	d := &fakeDecoder{seekErr: errors.New("跳不动")}
	ed := &errorDecoder{Decoder: d}

	if err := ed.SetPosition(12345); err == nil {
		t.Fatal("SetPosition 该报错了却什么都没说")
	}
	err := ed.Err()
	if err == nil {
		t.Fatal("seek 失败之后 Err() 是 nil")
	}
	if !strings.Contains(err.Error(), "跳不动") {
		t.Errorf("err = %v，want 含「跳不动」", err)
	}
}

// TestOnlyTheFirstDecodeErrorIsKept 钉「一条错误只有一个赢家」。
//
// 同一个源上会先后报好几个错（下载断了、换了流、又断了）。只记**第一条**：
// 第一条才是用户最先撞上的那个，后面的多半是它的余波。
//
// 而上层每 50 ms 问一次，所以如果每次都换一条，界面上的解释就会反复跳变。
func TestOnlyTheFirstDecodeErrorIsKept(t *testing.T) {
	d := &fakeDecoder{readErr: errors.New("第一个错")}
	ed := &errorDecoder{Decoder: d}
	if _, err := ed.Read(make([]float32, 512)); err == nil {
		t.Fatal("没报错")
	}

	d.mu.Lock()
	d.readErr = errors.New("第二个错")
	d.mu.Unlock()

	if _, err := ed.Read(make([]float32, 512)); err == nil {
		t.Fatal("没报错")
	}
	// ⚠️ 先判 nil 再 `got.Error()`：不判的话一次回归会变成一个空指针
	// panic，把整个测试二进制带下去，看不出是哪一条错了。
	if got := ed.Err(); got == nil || !strings.Contains(got.Error(), "第一个错") {
		t.Errorf("err = %v，want 第一条（用户最先撞上的那个）", got)
	}
}

// TestTheDecodeErrorReachesThePlayer 盯这一层真的接在播放器上。
//
// 前几条验的是 `errorDecoder` 自己；这一条验的是**接线**：`New` 建出来的
// 播放器上，解码器的错确实会冒到 `Player.Err()`——而那正是
// `app.checkPlayback` 每 50 ms 问的那个。
func TestTheDecodeErrorReachesThePlayer(t *testing.T) {
	p := New(&fakeDecoder{samples: 0, readErr: errDecode})

	// MyGo 的解码 goroutine 一建就在读，所以这里是等，不是同步读。
	deadline := time.Now().Add(5 * time.Second)
	for p.Err() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	err := p.Err()
	if err == nil {
		t.Fatal("解码器的错没有冒到 Player.Err()")
	}
	if !errors.Is(err, errDecode) {
		t.Errorf("err = %v，want %v", err, errDecode)
	}
	p.Close()
}

// TestClosingTheDecoderClosesTheOneUnderneath 盯 Close 一路传下去。
//
// `Decoder` 是**接口**字段，方法不会被提升——不显式写 `Close`，
// MyGo 就再也关不掉内层的解码器（AudioToolbox 的句柄会一直留着：
// 播放照常、fd 数也不涨，切到几百首才崩）。这条钉的就是那个显式的转发。
func TestClosingTheDecoderClosesTheOneUnderneath(t *testing.T) {
	d := &fakeDecoder{}
	ed := &errorDecoder{Decoder: d}
	if err := ed.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !d.wasClosed() {
		t.Error("Close 没有转发给底下的解码器")
	}
}

// TestADecoderWithoutCloserSurvivesClose 盯没有 io.Closer 的解码器。
//
// 不是每种解码器都实现了 `io.Closer`。这时 `Close` 什么都不做，但**不能
// panic**——那是切歌时必经的一路。
func TestADecoderWithoutCloserSurvivesClose(t *testing.T) {
	ed := &errorDecoder{Decoder: closerlessDecoder{}}
	if err := ed.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// closerlessDecoder 刻意不实现 io.Closer。
type closerlessDecoder struct{}

func (closerlessDecoder) SampleRate() int             { return 48000 }
func (closerlessDecoder) Channels() int               { return 2 }
func (closerlessDecoder) Read([]float32) (int, error) { return 0, io.EOF }
func (closerlessDecoder) SetPosition(int64) error     { return nil }
func (closerlessDecoder) Len() int64                  { return 0 }
