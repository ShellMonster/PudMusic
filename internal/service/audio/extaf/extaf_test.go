//go:build darwin

package extaf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// 素材是拿系统自带工具现造的，命令和 `docs/research/verify-streaming.md` §0
// 一模一样，所以这一份文件在同样的 macOS 上解出来的数字应该和那份报告里的
// 一致——报告里的数字就成了这里的黄金值。
//
// 造不出来（没有 afconvert）就跳过，不要让测试在没有转换器的机器上假失败。

const (
	// verify-streaming.md §3.1 对 media/long60-aac.m4a 的实测。
	goldenFrames = 2648680
	goldenRMS    = 0.01848
	goldenPeak   = 0.29830
)

func TestAACDecodesToTheDocumentedNumbers(t *testing.T) {
	path := makeTestAAC(t)

	d := openPath(t, path)

	if d.SampleRate() != 44100 {
		t.Errorf("SampleRate() = %d, want 44100", d.SampleRate())
	}
	if d.Channels() != 2 {
		t.Errorf("Channels() = %d, want 2", d.Channels())
	}
	if d.Len() != goldenFrames {
		t.Errorf("Len() = %d, want %d（'#frm' 要按 SInt64 读，用 uint32 读会静默拿到 0）", d.Len(), goldenFrames)
	}

	n, rms, peak := decodeAll(t, d)
	if n != goldenFrames {
		t.Errorf("解出 %d 帧，Len() 说有 %d 帧", n, goldenFrames)
	}
	if math.Abs(rms-goldenRMS) > 5e-4 {
		t.Errorf("RMS = %.5f，报告里是 %.5f", rms, goldenRMS)
	}
	if math.Abs(peak-goldenPeak) > 5e-3 {
		t.Errorf("峰值 = %.5f，报告里是 %.5f", peak, goldenPeak)
	}
}

// TestReadStopsExactlyAtLen 是「拖到文件末尾不崩、不假死」那一半：
// 读完最后一帧之后必须干净地返回 io.EOF，而不是一直返回 0 帧。
//
// 0 帧 + 状态码 0 有两种完全不同的来源：真的读完了，和 AudioBuffer 字段顺序
// 写错了（`verify-audio-playback.md` §5.2）。这条测试把两者分开：读满了
// Len() 帧之后才允许出现 EOF。
func TestReadStopsExactlyAtLen(t *testing.T) {
	d := openPath(t, makeTestAAC(t))

	buf := make([]float32, 2048*d.Channels())
	var frames int64
	for {
		n, err := d.Read(buf)
		frames += int64(n / d.Channels())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("第 %d 帧: %v", frames, err)
		}
		if n == 0 {
			t.Fatalf("第 %d 帧: 状态码 0 但一帧没解出来，这就是 AudioBuffer 字段顺序写错了", frames)
		}
	}
	if frames != d.Len() {
		t.Errorf("一共解出 %d 帧，Len() 是 %d", frames, d.Len())
	}

	// 读完之后再读还是 EOF，不该崩、不该报错。
	if n, err := d.Read(buf); n != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("读完再读 = (%d, %v)，want (0, EOF)", n, err)
	}
}

// TestSeekToEndEndsCleanly 是另外那一半：直接跳到最后一帧。
func TestSeekToEndEndsCleanly(t *testing.T) {
	d := openPath(t, makeTestAAC(t))

	if err := d.SetPosition(d.Len()); err != nil {
		t.Fatalf("SetPosition(Len()) = %v", err)
	}
	buf := make([]float32, 2048*d.Channels())
	if n, err := d.Read(buf); n != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("跳到末尾再读 = (%d, %v)，want (0, EOF)", n, err)
	}

	// 往回拖必须能接着播。
	if err := d.SetPosition(100000); err != nil {
		t.Fatalf("SetPosition(100000) = %v", err)
	}
	if n, err := d.Read(buf); err != nil || n == 0 {
		t.Errorf("跳回中间再读 = (%d, %v)，want 有样本且无错", n, err)
	}
}

func TestSetPositionRejectsOutOfRange(t *testing.T) {
	d := openPath(t, makeTestAAC(t))

	for _, at := range []int64{-1, d.Len() + 1} {
		if err := d.SetPosition(at); !errors.Is(err, ErrSeekRange) {
			t.Errorf("SetPosition(%d) = %v，want ErrSeekRange", at, err)
		}
	}
}

// TestSeekIsExact 确认跳过去解出来的确实是从那一帧开始的。
func TestSeekIsExact(t *testing.T) {
	d := openPath(t, makeTestAAC(t))

	head := read(t, d, 512)
	if err := d.SetPosition(100000); err != nil {
		t.Fatal(err)
	}
	mid := read(t, d, 512)
	if slices.Equal(head, mid) {
		t.Error("跳到第 100000 帧之后解出来的和开头一样，seek 没生效")
	}
	if allZero(mid) {
		t.Error("跳过去解到的是一片静音")
	}
}

// TestRawADTS 试裸 ADTS（没有容器的裸 AAC 码流，就是很多地方叫「.aac」的东西）。
//
// `verify-streaming.md` §1.1 记的是「macOS 这条路打不开裸 ADTS」，但那次用的是
// ExtAudioFileOpenURL。这条走的是 AudioFileOpenWithCallbacks，门槛不一样，
// 所以这里实测一次，把结论钉住——不管能开还是不能开，都要明确。
func TestRawADTS(t *testing.T) {
	dir := t.TempDir()
	wav := makeLongWAV(t, dir)
	src := filepath.Join(dir, "raw.aac")
	if err := run(t, "ffmpeg", "-v", "error", "-y", "-i", wav, "-c:a", "aac", "-b:a", "192k", "-f", "adts", src); err != nil {
		t.Skipf("造不出裸 ADTS 素材（需要 ffmpeg）: %v", err)
	}

	f, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d, err := Open(f)
	if err != nil {
		t.Logf("裸 ADTS 打不开：%v", err)
		return
	}
	defer d.Close()
	t.Logf("裸 ADTS 能解：rate=%d ch=%d len=%d", d.SampleRate(), d.Channels(), d.Len())
	frames, rms, peak := decodeAll(t, d)
	t.Logf("解出 %d 帧，rms=%.5f peak=%.5f", frames, rms, peak)
	if frames == 0 {
		t.Error("能打开却一帧都没解出来")
	}
}

func TestCloseTwiceIsSafe(t *testing.T) {
	d := openPath(t, makeTestAAC(t))
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Errorf("第二次 Close: %v", err)
	}
	if _, err := d.Read(make([]float32, 512)); !errors.Is(err, errClosed) {
		t.Errorf("关掉之后再 Read = %v，want errClosed", err)
	}
	if sourcesLeft() != 0 {
		t.Errorf("还有 %d 个字节源没注销", sourcesLeft())
	}
}

func TestOpenRejectsSomethingThatIsNotAudio(t *testing.T) {
	f, err := os.Open(filepath.Join(t.TempDir(), "x.bin"))
	if err != nil {
		f, err = os.Create(filepath.Join(t.TempDir(), "x.bin"))
		if err != nil {
			t.Fatal(err)
		}
	}
	f.Write([]byte("not audio at all, just some bytes"))
	f.Close()
	f2, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()
	if _, err := Open(f2); err == nil {
		t.Error("一段垃圾也解开了")
	}
}

// ---- 工具 ----

// openPath 打开一个本地文件解起来，测试结束时连句柄一起收掉。
//
// Open 不拥有传进去的那个 reader（产品里文件句柄归 player.go 的 closingDecoder
// 管），所以这里自己关。
func openPath(t *testing.T, path string) *Decoder {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d, err := Open(f)
	if err != nil {
		f.Close()
		t.Fatalf("Open(%s): %v", path, err)
	}
	t.Cleanup(func() {
		d.Close()
		f.Close()
	})
	return d
}

func decodeAll(t *testing.T, d *Decoder) (frames int64, rms, peak float64) {
	t.Helper()
	buf := make([]float32, 2048*d.Channels())
	var sum float64
	for {
		n, err := d.Read(buf)
		for _, v := range buf[:n] {
			sum += float64(v) * float64(v)
			if a := math.Abs(float64(v)); a > peak {
				peak = a
			}
		}
		frames += int64(n / d.Channels())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("第 %d 帧: %v", frames, err)
		}
	}
	return frames, math.Sqrt(sum / (float64(frames) * float64(d.Channels()))), peak
}

func read(t *testing.T, d *Decoder, frames int) []float32 {
	t.Helper()
	buf := make([]float32, frames*d.Channels())
	n, err := d.Read(buf)
	if err != nil {
		t.Fatalf("读开头: %v", err)
	}
	return buf[:n]
}

func allZero(s []float32) bool {
	for _, v := range s {
		if v != 0 {
			return false
		}
	}
	return true
}

func sourcesLeft() int {
	sourcesMu.Lock()
	defer sourcesMu.Unlock()
	return len(sources)
}

// makeTestAAC 造一份和 verify-streaming.md §0 一样的 60 秒素材，造好了返回路径。
func makeTestAAC(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("afconvert"); err != nil {
		t.Skip("这台机器上没有 afconvert，造不出 AAC 素材")
	}
	dir := t.TempDir()
	wav := makeLongWAV(t, dir)
	out := filepath.Join(dir, "long60-aac.m4a")
	if err := run(t, "afconvert", "-f", "m4af", "-d", "aac", "-b", "192000", wav, out); err != nil {
		t.Skipf("造不出 AAC 素材: %v", err)
	}
	return out
}

// makeLongWAV 把系统的 Ping.aiff 变成 60 秒的 44.1kHz 立体声 WAV。
// 步骤和 verify-streaming.md §0 一字不差，好让造出来的 AAC 和那份报告可比。
func makeLongWAV(t *testing.T, dir string) string {
	t.Helper()
	ping := filepath.Join(dir, "ping.wav")
	if err := run(t, "afconvert", "-f", "WAVE", "-d", "LEI16@44100", "-c", "2", "/System/Library/Sounds/Ping.aiff", ping); err != nil {
		t.Skipf("系统里没有 Ping.aiff: %v", err)
	}

	src, err := os.ReadFile(ping)
	if err != nil {
		t.Fatal(err)
	}
	// WAV 的数据块紧跟在 fmt 块后面，这里只处理 afconvert 产出的那一种版式。
	const dataTag = "data"
	i := bytes.Index(src[:min(len(src), 4096)], []byte(dataTag))
	if i < 0 {
		t.Fatal("ping.wav 里找不到 data 块")
	}
	pcm := src[i+8:] // 4 字节长度 + 4 字节 tag 已经跳过
	pcm = pcm[:len(pcm)-len(pcm)%4]

	var long bytes.Buffer
	long.Write(src[:i+8])
	// data 块的长度字段要改，别让它跟原文件一样短。
	long.Write(make([]byte, 4))
	for range 40 {
		long.Write(pcm)
	}
	out := filepath.Join(dir, "long60.wav")
	if err := os.WriteFile(out, long.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	patchWAVDataSize(t, out, uint32(len(pcm)*40))
	return out
}

// patchWAVDataSize 回填 RIFF 与 data 两处长度——它们只能事后知道。
func patchWAVDataSize(t *testing.T, path string, size uint32) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b))-8) // RIFF 块长度
	i := bytes.Index(b[:min(len(b), 4096)], []byte("data"))
	binary.LittleEndian.PutUint32(b[i+4:], size)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, name string, args ...string) error {
	t.Helper()
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return &execErr{name: name, out: string(out), err: err}
	}
	return nil
}

type execErr struct {
	name, out string
	err       error
}

func (e *execErr) Error() string { return e.name + ": " + e.err.Error() + ": " + e.out }
