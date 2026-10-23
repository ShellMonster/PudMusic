package audio

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/audio/extaf"

	mygoaudio "github.com/egoist/mygo/audio"
)

// 换歌不能漏东西，而且漏的**不止**文件描述符：

//   - 文件描述符：os.File。数 /dev/fd 能看见。
//   - extaf 的 AudioToolbox 句柄（AudioFileID / ExtAudioFileRef）：不是 fd，
//     是 AudioToolbox 里的一块内存，只有解码器的 Close 能放掉。
//
// 第二样曾经漏过：解码器被 closingDecoder 包着，只把文件收了进去，播放照常、
// fd 数也不涨，切到几百首才崩。下面两条分别盯住一样。
//
// ⚠️ **第二样只有 macOS 有**：AudioToolbox 是 macOS 那一层，Windows 上的
// 解码器是 Media Foundation，它不往进程里挂这种句柄。而数 fd 的办法
// （`/dev/fd`）在 Windows 上也没有——所以那边只剩「第一样」可盯，而它靠
// 另一个办法数（Win32 的 `GetProcessHandleCount`），那是另一件事，不归
// 这一票管。两条在 Windows 上都会**明确地跳过**，不是碰巧跳过。

// TestOwnedClosesTheDecoderAndItsFile 盯解码器和文件都要关，而且顺序要对
// （解码器关自己时还要用到底下的文件）。
func TestOwnedClosesTheDecoderAndItsFile(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "x"))
	if err != nil {
		t.Fatal(err)
	}
	d := &countingDecoder{file: f}
	c := owned(d, f)

	closer, ok := c.(io.Closer)
	if !ok {
		t.Fatal("owned() 的结果要实现 io.Closer，MyGo 靠这个关解码器")
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if d.closed != 1 {
		t.Errorf("解码器的 Close 被调了 %d 次，want 1", d.closed)
	}
	if d.sawFileClosed {
		t.Error("解码器被关之前文件已经关了，顺序反了")
	}
	if err := f.Close(); err == nil {
		t.Error("文件没有关掉")
	}
}

// TestOpenAnMP4ReleasesTheAudioToolboxHandle 走真实的 Open 路径：打开一个
// .m4a、关掉，extaf 那边的字节源必须回到 0。
//
// ⚠️ **这条只管 macOS**：它数的是 **AudioToolbox 的句柄**
// （AudioFileID / ExtAudioFileRef）——那是 macOS 那一层特有的东西，Windows
// 上根本没有这一层（它走 Media Foundation），数不到东西。
//
// 素材用仓库自带的那份 `.m4a`（不是现造的）：CI 的 Windows runner 没有
// afconvert，现造出来的东西在那边根本造不出来。
func TestOpenAnMP4ReleasesTheAudioToolboxHandle(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("这条断的是 AudioToolbox 的句柄，只有 macOS 有这一层")
	}
	path := filepath.Join("mfadec", "testdata", "aaclc-stereo.m4a")

	p, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := extaf.LiveSources(); got == 0 {
		t.Error("播放器开着的时候 extaf 一个字节源都没有，这条测试没验到东西")
	}
	p.Close()
	settleHandles(t)

	if got := extaf.LiveSources(); got != 0 {
		t.Errorf("关掉之后 extaf 还挂着 %d 个字节源，AudioToolbox 的句柄漏了", got)
	}
}

// TestSwitchingTracksLeaksNoFileHandles 盯文件描述符那一半：连着开合几十次
// 之后，进程打开的 fd 数不许涨。
//
// ⚠️ **数 fd 的办法是这台机器上的**（`/dev/fd`），所以在 Windows 上这条会
// 跳过——那里数 fd 要走 Win32 的 `GetProcessHandleCount`，那是另一件事，
// 不归这一票管。
func TestSwitchingTracksLeaksNoFileHandles(t *testing.T) {
	path := filepath.Join("mfadec", "testdata", "aaclc-stereo.m4a")

	// 先开一轮把系统的延迟分配（解码器第一次调用会起后台线程、打开它自己的
	// 文件）付掉，再开始数。
	p, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p.Close()
	settleHandles(t)

	before := openFDs(t)
	for range 30 {
		q, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		q.Close()
	}
	// 播放器是异步关的：MyGo 让解码 goroutine 自己去关解码器。等它一会儿。
	settleHandles(t)

	if after := openFDs(t); after > before {
		t.Errorf("开合了 30 次之后打开的 fd 从 %d 涨到 %d，漏了 %d 个", before, after, after-before)
	}
}

// countingDecoder 是个只记得住「关过没有」的解码器，顺带看一眼文件关没关。
type countingDecoder struct {
	file          *os.File
	closed        int
	sawFileClosed bool
}

func (d *countingDecoder) SampleRate() int { return 44100 }
func (d *countingDecoder) Channels() int   { return 2 }
func (d *countingDecoder) Len() int64      { return 44100 }

func (d *countingDecoder) Read([]float32) (int, error) {
	return 0, errors.New("这个解码器只是占位，不真的解")
}
func (d *countingDecoder) SetPosition(int64) error { return nil }

func (d *countingDecoder) Close() error {
	d.closed++
	// 文件已经被关了的话 Stat 会报 bad file descriptor。
	_, err := d.file.Stat()
	d.sawFileClosed = err != nil
	return nil
}

// openFDs 数这个进程当前打开着多少个文件描述符。
func openFDs(t *testing.T) int {
	t.Helper()
	ents, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Skipf("这台机器上读不到 /dev/fd：%v", err)
	}
	return len(ents)
}

// settleHandles 等后台把该关的关完。播放器的解码 goroutine 是异步的，
// Close 刚返回时句柄可能还在。
func settleHandles(t *testing.T) {
	t.Helper()
	time.Sleep(200 * time.Millisecond)
}

var (
	_ mygoaudio.Decoder = (*countingDecoder)(nil)
	_ io.Closer         = (*countingDecoder)(nil)
)
