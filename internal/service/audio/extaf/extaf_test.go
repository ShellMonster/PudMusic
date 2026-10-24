//go:build darwin

package extaf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
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
	path := testAAC(t)
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
	d := openPath(t, testAAC(t))

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
	d := openPath(t, testAAC(t))

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
	d := openPath(t, testAAC(t))

	for _, at := range []int64{-1, d.Len() + 1} {
		if err := d.SetPosition(at); !errors.Is(err, ErrSeekRange) {
			t.Errorf("SetPosition(%d) = %v，want ErrSeekRange", at, err)
		}
	}
}

// TestSeekIsExact 确认跳过去解出来的确实是从那一帧开始的。
func TestSeekIsExact(t *testing.T) {
	d := openPath(t, testAAC(t))

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
//
// # 为什么只转六秒
//
// 这一条要的是「这条打开路径通不通」，不是「解出来的数字对不对」——数字由上面
// 那条拿 60 秒素材对着 `verify-streaming.md` 的黄金值断。六秒是 26 万多帧，
// 「能不能打开并解出帧」这件事到这里已经看得很清楚了，而 `ffmpeg` 转 60 秒加
// 解码 60 秒是这一整个包里最贵的一笔（实测十几秒，`-race` 下更贵）。
//
// 顺带还多走了一条路：`-t` 切在编码器的 priming 之后，末帧多半是不完整的，
// 所以「碰到半个帧不能崩」也被顺带验了。
func TestRawADTS(t *testing.T) {
	wav := testLongWAV(t)
	src := filepath.Join(filepath.Dir(wav), "raw.aac")
	if err := run("ffmpeg", "-v", "error", "-y", "-i", wav, "-t", "6",
		"-c:a", "aac", "-b:a", "192k", "-f", "adts", src); err != nil {
		if errors.Is(err, errNoTool) {
			t.Skipf("造不出裸 ADTS 素材（需要 ffmpeg）: %v", err)
		}
		t.Fatalf("造裸 ADTS 素材: %v", err)
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
	d := openPath(t, testAAC(t))
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

// ---- 总长不足 8192 字节的文件 ----
//
// # 现象
//
// AudioToolbox 探容器时**第一次就要 8192 字节**，而整个文件不足 8192。
// 字节源于是交出「没凑齐就到了头」（io.ReadFull 给的是 io.ErrUnexpectedEOF），
// 以前那被当成了硬错误：Decoder.Read 一读到它就返回，**一个样本都解不出来**，
// 界面上是「这歌放不出来」（#312）。
//
// 受影响的是**总长**不足 8192 的那一种东西：秒开片段、缓存碎片、测试素材。
// 「歌短」**不受影响**——文件总长够就行（短前奏没关系）。
//
// # 为什么短读不是错
//
// AudioToolbox 的读回调只有一个出口：「这次要到了几个字节」。它把「少于
// 我要的」当成流到头了——所以短读正是要告诉它的话。见 `source.readAt`。

// TestAShortFileStillDecodes 盯「总长不足 8192 字节的完整文件能解出来」。
//
// 素材是自己写的 WAV（不转系统素材）：这条要验的是「文件总长够不够」，
// 素材必须**恰好**短到能触发那个 8192 字节的探测读，而且每次都一样。
// 自己写字节就没有 afconvert / ffmpeg 的版本浮动，也不用在没装转换器的
// 机器上跳过——这一条要一直绿着。
func TestAShortFileStillDecodes(t *testing.T) {
	path, want := tinyWAVIn(t, 8000, 400)

	// 素材本身必须短到能触发那个探测读，否则这条测的就不是这一格。
	if st, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if st.Size() >= 8192 {
		t.Fatalf("素材有 %d 字节，不小于 8192，这条用例测不到目标", st.Size())
	}

	d := openPath(t, path)
	if d.SampleRate() != 8000 {
		t.Errorf("SampleRate() = %d, want 8000", d.SampleRate())
	}
	if d.Channels() != 1 {
		t.Errorf("Channels() = %d, want 1", d.Channels())
	}
	if d.Len() != int64(len(want)) {
		t.Errorf("Len() = %d, want %d", d.Len(), len(want))
	}

	frames, rms, peak := decodeAll(t, d)
	if frames != int64(len(want)) {
		t.Fatalf("解出 %d 帧，文件里只有 %d 帧", frames, len(want))
	}
	if peak <= 0 {
		t.Error("解出来的是一片静音")
	}
	if rms <= 0 {
		t.Error("RMS 是 0，解出来的不是声音")
	}
}

// TestAShortFileKeepsItsSamples 盯短文件解出来的**内容**。
//
// 帧数对不等于内容对：末尾短读处理错了的时候有可能凑出一个「帧数正好、
// 内容是零」的假象。这一条把每一个样本都和文件里写的比一遍。
func TestAShortFileKeepsItsSamples(t *testing.T) {
	path, want := tinyWAVIn(t, 8000, 400)
	d := openPath(t, path)

	buf := make([]float32, len(want))
	got := 0
	for got < len(buf) {
		n, err := d.Read(buf[got:])
		got += n
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("第 %d 个样本之后: %v", got, err)
		}
		if n == 0 {
			t.Fatalf("第 %d 个样本: 一帧没要到又没有错", got)
		}
	}
	if got != len(want) {
		t.Fatalf("读出 %d 个样本，want %d", got, len(want))
	}
	for i, v := range buf {
		if math.Abs(float64(v-want[i])) > 1e-6 {
			t.Fatalf("第 %d 个样本是 %v，文件里是 %v", i, v, want[i])
		}
	}
}

// TestAShortFileEndsAtEOF 盯短文件的另外两半。
//
// 一半是「读完仍然是干净的 EOF」——末尾短读被归到「读完」那一边之后，
// 不能把真的读完也弄丢（那样歌永远放不完）。
//
// 另一半是「字节源身上没有记下错」：末尾短读以前是被当成硬错误记下去的，
// 那一条正是这个 bug 的病根。
func TestAShortFileEndsAtEOF(t *testing.T) {
	path, want := tinyWAVIn(t, 8000, 400)
	d := openPath(t, path)

	buf := make([]float32, 512)
	var got int
	for {
		n, err := d.Read(buf)
		got += n
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("第 %d 个样本之后: %v", got, err)
		}
		if n == 0 {
			t.Fatalf("第 %d 个样本: 一帧没要到又没有错", got)
		}
	}
	if got != len(want) {
		t.Errorf("一共解出 %d 个样本，want %d", got, len(want))
	}

	// 读完了再读还是 EOF，不该崩、不该报错。
	if n, err := d.Read(buf); n != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("读完再读 = (%d, %v)，want (0, EOF)", n, err)
	}

	if err := d.readSourceErr(); err != nil {
		t.Errorf("字节源记下了一个错: %v（末尾短读不算错）", err)
	}
}

// TestAShortCompressedFileStillDecodes 盯压缩容器那一条路。
//
// 上两条用的是自己写的 WAV（PCM，位深浅）。生产里中招的是**压缩**的容器：
// 秒开片段、缓存碎片多半是 AAC / MP4。两条路的探测读是同一处，所以这里
// 再验一次压缩的：素材由 ffmpeg 现切 0.3 秒，长度必然小于 8192。
//
// 帧数不写死值：不同 ffmpeg 版本的编码器延迟略有不同，写死会飘。真正的
// 判据是「解出来的帧数 == 文件自己说的帧数」，那一条对任何版本都成立。
func TestAShortCompressedFileStillDecodes(t *testing.T) {
	wav := testLongWAV(t)
	dir := filepath.Dir(wav)

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"short.aac", []string{"-c:a", "aac", "-b:a", "48k", "-f", "adts"}},
		{"short.m4a", []string{"-c:a", "aac", "-b:a", "48k"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := filepath.Join(dir, tc.name)
			args := append([]string{"-v", "error", "-y", "-i", wav, "-t", "0.3"}, tc.args...)
			if err := run("ffmpeg", append(args, src)...); err != nil {
				if errors.Is(err, errNoTool) {
					t.Skipf("造不出短素材（需要 ffmpeg）: %v", err)
				}
				t.Fatalf("造短素材: %v", err)
			}

			st, err := os.Stat(src)
			if err != nil {
				t.Fatal(err)
			}
			if st.Size() >= 8192 {
				t.Fatalf("素材有 %d 字节，不小于 8192，这条用例测不到目标", st.Size())
			}

			d := openPath(t, src)
			frames, rms, peak := decodeAll(t, d)
			if frames == 0 {
				t.Fatal("一个样本都没解出来")
			}
			if frames != d.Len() {
				t.Errorf("解出 %d 帧，Len() 说有 %d 帧", frames, d.Len())
			}
			if peak <= 0 || rms <= 0 {
				t.Errorf("解出来的是静音（rms=%.6f peak=%.6f）", rms, peak)
			}
			if err := d.readSourceErr(); err != nil {
				t.Errorf("字节源记下了一个错: %v（末尾短读不算错）", err)
			}
		})
	}
}

// tinyWAVIn 造一个总长不足 8192 字节的 WAV（16 位单声道 PCM），返回它的路径
// 和它里面每一个样本解出来应该是什么值。
//
// 返回的期望值是按「16 位有符号整数除以 32768」算的——那是 AudioToolbox 把
// 定点 PCM 转成 float32 的算法（客户端格式要的是 float32）。
func tinyWAVIn(t *testing.T, rate, samples int) (path string, want []float32) {
	t.Helper()
	var b bytes.Buffer
	dataLen := samples * 2
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+dataLen))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16)) // fmt 块的长度
	binary.Write(&b, binary.LittleEndian, uint16(1))  // PCM
	binary.Write(&b, binary.LittleEndian, uint16(1))  // 单声道
	binary.Write(&b, binary.LittleEndian, uint32(rate))
	binary.Write(&b, binary.LittleEndian, uint32(rate*2)) // 每秒字节数
	binary.Write(&b, binary.LittleEndian, uint16(2))      // 块对齐
	binary.Write(&b, binary.LittleEndian, uint16(16))     // 位深
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(dataLen))

	want = make([]float32, samples)
	for i := range samples {
		v := int16(20000 * math.Sin(2*math.Pi*440*float64(i)/float64(rate)))
		binary.Write(&b, binary.LittleEndian, v)
		want[i] = float32(v) / 32768
	}

	path = filepath.Join(t.TempDir(), "tiny.wav")
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, want
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

// ---- 素材：造一次，全包共用 ----
//
// # 为什么要共用
//
// 60 秒的素材要转两次外部工具（Ping.aiff → WAV → AAC），而这个包里六条测试
// 都要它。挨个造，就是把同一份文件转六遍：`-race` 下这个包光等 afconvert 就
// 要一分多钟，而解码本身只要几百毫秒——**九成的墙钟花在重复造同一份素材上**。
//
// 共用不改变任何一条测试验的东西：素材是确定性的（同一份 WAV 进 afconvert，
// 出的是同一份 AAC），测的是 `Decoder` 的行为，不是 afconvert 的行为。
//
// 共用的只是**素材**。「跳过」这个决定仍然是每条测试自己做的：造不出来时
// 这里记下原因（`errNoTool` 之类），由 testAAC / testLongWAV 判断是 skip
// 还是 fail——工具没装是 skip，工具装着却转坏了是真错误。
var (
	fixtureOnce sync.Once
	fixtureDir  string // 这两份素材所在的目录，TestMain 结束时删
	fixtureErr  error  // 造素材这一步本身的错（建不了临时目录之类）

	longWAVOnce sync.Once
	longWAVPath string
	longWAVErr  error

	aacOnce sync.Once
	aacPath string
	aacErr  error
)

// errNoTool 是「这台机器上少一个转换器」，调用方据此 skip。
//
// 单独一类是因为另一件事不该 skip：工具装着却转坏了，那是真错误。
var errNoTool = errors.New("缺少转换工具")

// testMain 造素材的共享目录用 os.MkdirTemp 而不是 t.TempDir——后者每条测试
// 结束就删，而素材要活过整个包。
func TestMain(m *testing.M) {
	code := m.Run()
	if fixtureDir != "" {
		os.RemoveAll(fixtureDir)
	}
	os.Exit(code)
}

// sharedDir 拿（或造）素材目录。
func sharedDir() (string, error) {
	fixtureOnce.Do(func() {
		dir, err := os.MkdirTemp("", "biu-extaf-fixture")
		if err != nil {
			fixtureErr = err
			return
		}
		fixtureDir = dir
	})
	return fixtureDir, fixtureErr
}

// testLongWAV 返回 60 秒的 44.1kHz 立体声 WAV 的路径，造一次。
func testLongWAV(t *testing.T) string {
	t.Helper()
	longWAVOnce.Do(func() { longWAVPath, longWAVErr = buildLongWAV() })
	if longWAVErr != nil {
		if errors.Is(longWAVErr, errNoTool) {
			t.Skipf("造不出 60 秒 WAV 素材: %v", longWAVErr)
		}
		t.Fatalf("造 60 秒 WAV 素材: %v", longWAVErr)
	}
	return longWAVPath
}

// testAAC 返回和 verify-streaming.md §0 一样的 60 秒 AAC 素材的路径，造一次。
func testAAC(t *testing.T) string {
	t.Helper()
	aacOnce.Do(func() { aacPath, aacErr = buildAAC() })
	if aacErr != nil {
		if errors.Is(aacErr, errNoTool) {
			t.Skipf("造不出 AAC 素材: %v", aacErr)
		}
		t.Fatalf("造 AAC 素材: %v", aacErr)
	}
	return aacPath
}

// buildAAC 在共享目录里造 AAC 素材。
func buildAAC() (string, error) {
	dir, err := sharedDir()
	if err != nil {
		return "", err
	}
	wav, err := longWAVIn(dir)
	if err != nil {
		return "", err
	}
	out := filepath.Join(dir, "long60-aac.m4a")
	if err := run("afconvert", "-f", "m4af", "-d", "aac", "-b", "192000", wav, out); err != nil {
		return "", err
	}
	return out, nil
}

// buildLongWAV 在共享目录里造 60 秒 WAV 素材。
func buildLongWAV() (string, error) {
	dir, err := sharedDir()
	if err != nil {
		return "", err
	}
	return longWAVIn(dir)
}

// longWAVIn 把系统的 Ping.aiff 变成 60 秒的 44.1kHz 立体声 WAV。
// 步骤和 verify-streaming.md §0 一字不差，好让造出来的 AAC 和那份报告可比。
func longWAVIn(dir string) (string, error) {
	ping := filepath.Join(dir, "ping.wav")
	if err := run("afconvert", "-f", "WAVE", "-d", "LEI16@44100", "-c", "2", "/System/Library/Sounds/Ping.aiff", ping); err != nil {
		return "", err
	}

	src, err := os.ReadFile(ping)
	if err != nil {
		return "", err
	}
	// WAV 的数据块紧跟在 fmt 块后面，这里只处理 afconvert 产出的那一种版式。
	const dataTag = "data"
	i := bytes.Index(src[:min(len(src), 4096)], []byte(dataTag))
	if i < 0 {
		return "", errors.New("ping.wav 里找不到 data 块")
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
		return "", err
	}
	if err := patchWAVDataSize(out, uint32(len(pcm)*40)); err != nil {
		return "", err
	}
	return out, nil
}

// patchWAVDataSize 回填 RIFF 与 data 两处长度——它们只能事后知道。
func patchWAVDataSize(path string, size uint32) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b))-8) // RIFF 块长度
	i := bytes.Index(b[:min(len(b), 4096)], []byte("data"))
	binary.LittleEndian.PutUint32(b[i+4:], size)
	return os.WriteFile(path, b, 0o644)
}

func run(name string, args ...string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%w: %s: %w", errNoTool, name, err)
	}
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

// ---- 字节源中途死掉：不能被折成「读完」 ----
//
// # 为什么这一组要单独钉
//
// AudioToolbox 的读回调只有一个出口：「这次要到了几个字节」。读失败在回调里
// 只能变成 0，而 0 在它眼里和「干净地读到末尾」是**同一个东西**——本文件
// 的 `readAt` 以前把 `io.ReadFull` 的错误直接丢掉（`got, _ :=`），于是：
//
//	网络断了 → 回调交出 0 字节、状态码 0 → ExtAudioFileRead 说「没帧了」
//	         → Decoder.Read 返回 io.EOF → 歌「放完了」
//
// 表现就是「歌播到一半忽然放完了」，界面上什么提示都没有。
// `spec.md`「播放状态机」那一节说的「解码失败不能被当成播放结束」就是它。
// 本地文件读不出错，所以这条在本地永远测不出来——必须用一个会死的字节源。

// dyingSeeker 是一个「armed 之后读到某个偏移就报错」的字节源。
//
// 为什么要有 armed 这一下：`Open` 本身要读文件头（MP4 的话还要读尾部的
// moov），所以得先让它整个读一遍，再把源「掐掉」。不这样的话，
// AudioFileOpenWithCallbacks 在探类型的时候就拿到 -39，测不到我们要测的那段。
type dyingSeeker struct {
	r    *os.File
	size int64
	// mu 不是多余的：读回调跑在 AudioToolbox 自己的线程上，而 `armed` 是从
	// 测试的 goroutine 上置的。没有它的话，读回调很可能**一直看见旧值**
	// （实测就是这个：置了 armed 之后读回调还是照读不误，于是测试表现为
	// 「字节源死了却还是 EOF」——而那恰好就是这条用例要抓的 bug，
	// 查起来会把自己绕进去）。
	mu    sync.Mutex
	armed bool
	// budget 是「开完之后还准读几个字节」。到 0 之后每一次读都报错。
	//
	// 为什么用「还准读几个字节」而不是「从第几个字节起死」：AudioToolbox
	// 会在 Open 和头几次读里把文件**大段地读走**（它要探容器、还要拼 moov），
	// 按绝对偏移设死点的话，那一下早就越过了，测不到解码阶段。
	budget int64
	pos    int64
	read   int64
}

// newDyingSeeker 建一个。size 是文件总长——`Seek(0, SeekEnd)` 要靠它，
// 而 `extaf.Open` 开口第一件事就是问长度。
func newDyingSeeker(f *os.File, size, budget int64) *dyingSeeker {
	return &dyingSeeker{r: f, size: size, budget: budget}
}

func (s *dyingSeeker) Read(p []byte) (int, error) {
	s.mu.Lock()
	if s.armed {
		if s.budget <= 0 {
			s.read += int64(len(p)) // 只是为了让失败信息里的字节数有意义
			s.mu.Unlock()
			return 0, errByteSourceDead
		}
		if int64(len(p)) > s.budget {
			p = p[:s.budget]
		}
	}
	n, err := s.r.Read(p)
	s.pos += int64(n)
	s.budget -= int64(n)
	s.read += int64(n)
	s.mu.Unlock()
	return n, err
}

// arm 是从测试的 goroutine 上调的那个开关。
func (s *dyingSeeker) arm() {
	s.mu.Lock()
	s.armed = true
	s.mu.Unlock()
}

func (s *dyingSeeker) Seek(off int64, whence int) (int64, error) {
	s.mu.Lock()
	switch whence {
	case io.SeekStart:
		s.pos = off
	case io.SeekCurrent:
		s.pos += off
	case io.SeekEnd:
		s.pos = s.size + off
	}
	pos := s.pos
	s.mu.Unlock()

	if _, err := s.r.Seek(pos, io.SeekStart); err != nil {
		return 0, err
	}
	return pos, nil
}

var errByteSourceDead = errors.New("测试用的字节源：网络断了")

// TestAReadFailureIsNotAnEndOfFile 盯「拉不动了」没有被折成「放完了」。
func TestAReadFailureIsNotAnEndOfFile(t *testing.T) {
	path := filepath.Join("..", "flacdec", "testdata", "stereo16.mp4")
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("找不到 MP4 测试素材：%v", err)
	}
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	// Open 期间整个文件都能读；开完之后只准再读 32 KB，之后每一次读都失败。
	src := newDyingSeeker(f, info.Size(), 32<<10)
	d, err := Open(src)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	src.arm()

	buf := make([]float32, 4096*d.Channels())
	var sawErr bool
	for range 4000 {
		n, err := d.Read(buf)
		if err != nil {
			// 关键：它得是**读失败**，不能是 io.EOF。
			if errors.Is(err, io.EOF) {
				t.Fatalf("字节源断了却被折成了「读完」——上层会以为歌放完了（第 %d 字节）", src.read)
			}
			if !errors.Is(err, errByteSourceDead) {
				t.Fatalf("报的错不是字节源那个：%v", err)
			}
			sawErr = true
			break
		}
		if n == 0 {
			t.Fatal("一帧没要到又没有错")
		}
	}
	if !sawErr {
		t.Fatal("读完了整首都没报错（素材太小或者死得太晚，这条用例没验到东西）")
	}
}

// TestAFinishedSourceStillEndsCleanly 盯反过来的那一半。
//
// 上一个用例把错误捞出来了，这个要保证**正常读完仍然是 io.EOF**——
// 不然「歌放完了」会被说成「歌断了」，用户按重试也没用。
func TestAFinishedSourceStillEndsCleanly(t *testing.T) {
	path := filepath.Join("..", "flacdec", "testdata", "stereo16.mp4")
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("找不到 MP4 测试素材：%v", err)
	}
	info, _ := f.Stat()

	src := newDyingSeeker(f, info.Size(), 1<<40) // 永远死不了
	d, err := Open(src)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	src.arm()

	buf := make([]float32, 4096*d.Channels())
	for range 200000 {
		n, err := d.Read(buf)
		if errors.Is(err, io.EOF) {
			return // 对了
		}
		if err != nil {
			t.Fatalf("正常读完却报了错：%v", err)
		}
		if n == 0 {
			t.Fatal("一帧没要到又没有错")
		}
	}
	t.Fatal("读完了一直不到头")
}
