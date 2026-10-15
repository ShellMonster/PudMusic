package audio

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// 这组测试盯的是「文件头 → 解码器」这条分派有没有接上，不是解码对不对
// （对不对由 extaf 自己的测试管）。素材用系统自带的 afconvert / ffmpeg 现造，
// 造不出来就跳过。

// TestOpenRoutesMP4ToTheSystemDecoder 确认 .m4a 真的走的是 ExtAudioFile 那条路。
func TestOpenRoutesMP4ToTheSystemDecoder(t *testing.T) {
	path := makeAAC(t, "m4a")

	p, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%s): %v", path, err)
	}
	defer p.Close()

	if got := p.Duration(); got <= 0 {
		t.Errorf("时长 = %v，.m4a 应该有时长", got)
	}
	if got := p.dec.SampleRate(); got != 44100 {
		t.Errorf("采样率 = %d，want 44100", got)
	}
}

// TestOpenRoutesRawADTS 盯的是裸 ADTS（没有容器的 .aac）——
// `verify-streaming.md` §1.1 说 macOS 打不开，本线程实测能开，
// 这条测试就是为了不让它哪天悄悄退化回去。
func TestOpenRoutesRawADTS(t *testing.T) {
	wav := makeTestWAV(t)
	adts := filepath.Join(t.TempDir(), "raw.aac")
	if err := run(t, "ffmpeg", "-v", "error", "-y", "-i", wav, "-c:a", "aac", "-b:a", "192k", "-f", "adts", adts); err != nil {
		t.Skipf("造不出裸 ADTS 素材（需要 ffmpeg）: %v", err)
	}

	p, err := Open(adts)
	if err != nil {
		t.Fatalf("裸 ADTS 打不开：%v", err)
	}
	defer p.Close()
	if got := p.Duration(); got <= 0 {
		t.Errorf("时长 = %v", got)
	}
}

// TestOpenStillFindsTheOldFormats 是回归：新注册的两个容器不能把原来能开的
// 格式顶掉（MyGo 的格式表是按注册顺序第一个匹配就赢）。
func TestOpenStillFindsTheOldFormats(t *testing.T) {
	mp3 := filepath.Join("testdata", "tone.mp3")
	p, err := Open(mp3)
	if err != nil {
		t.Fatalf("Open(%s): %v", mp3, err)
	}
	defer p.Close()
	if got := p.Duration(); got <= 0 {
		t.Errorf("mp3 的时长 = %v", got)
	}

	// 系统自带的 AIFF：不在 MyGo 的格式表里（我们自己注册的是 FORM????AIFF），
	// 但 ExtAudioFile 也能解。这里只确认它不会因为新注册而报错。
	ping := "/System/Library/Sounds/Ping.aiff"
	if _, err := os.Stat(ping); err != nil {
		t.Skip("这台机器上没有 Ping.aiff")
	}
	q, err := Open(ping)
	if err != nil {
		t.Fatalf("Open(%s): %v", ping, err)
	}
	defer q.Close()
	if got := q.Duration(); got <= 0 {
		t.Errorf("aiff 的时长 = %v", got)
	}
}

func TestOpenRejectsSomethingThatIsNotAudio(t *testing.T) {
	path := filepath.Join(t.TempDir(), "junk.bin")
	if err := os.WriteFile(path, []byte("this is not audio, it is a sentence with words in it"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Open(path)
	if p != nil {
		p.Close()
		t.Fatal("一段文字居然解出来了")
	}
	if err == nil {
		t.Fatal("Open 应该报错")
	}
}

// ---- 素材 ----

func makeAAC(t *testing.T, format string) string {
	t.Helper()
	if _, err := exec.LookPath("afconvert"); err != nil {
		t.Skip("这台机器上没有 afconvert")
	}
	path := filepath.Join(t.TempDir(), "tone."+format)
	if err := run(t, "afconvert", "-f", "m4af", "-d", "aac", "-b", "192000", makeTestWAV(t), path); err != nil {
		t.Skipf("造不出素材: %v", err)
	}
	return path
}

// makeTestWAV 用系统的 Ping.aiff 转一段两秒的 44.1kHz 立体声 WAV。
func makeTestWAV(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("afconvert"); err != nil {
		t.Skip("这台机器上没有 afconvert")
	}
	path := filepath.Join(t.TempDir(), "tone.wav")
	if err := run(t, "afconvert", "-f", "WAVE", "-d", "LEI16@44100", "-c", "2", "/System/Library/Sounds/Ping.aiff", path); err != nil {
		t.Skipf("系统里没有 Ping.aiff: %v", err)
	}
	return path
}

func run(t *testing.T, name string, args ...string) error {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return &cmdErr{name: name, out: string(out), err: err}
	}
	return nil
}

type cmdErr struct {
	name, out string
	err       error
}

func (e *cmdErr) Error() string { return e.name + ": " + e.err.Error() + ": " + e.out }
