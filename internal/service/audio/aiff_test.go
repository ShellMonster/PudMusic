package audio

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	mygoaudio "github.com/egoist/mygo/audio"
)

// AIFF 解码器的判据是它自己写出来的素材：现造一个 AIFF，再解回来逐样本比对。
// FLAC 那套素材（stereo16.wav）拿来做数据源正好——那是 44.1kHz 立体声 16 位。

type wav struct {
	rate     int
	channels int
	bits     int
	samples  []int32 // 交错的
}

func readWAV(t *testing.T, path string) wav {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("读不到 %s：%v", path, err)
	}
	if string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		t.Fatalf("%s 不是 RIFF/WAVE", path)
	}
	var w wav
	pos := 12
	for pos+8 <= len(b) {
		id := string(b[pos : pos+4])
		n := int(binary.LittleEndian.Uint32(b[pos+4:]))
		body := b[pos+8 : pos+8+n]
		switch id {
		case "fmt ":
			w.channels = int(binary.LittleEndian.Uint16(body[2:]))
			w.rate = int(binary.LittleEndian.Uint32(body[4:]))
			w.bits = int(binary.LittleEndian.Uint16(body[14:]))
		case "data":
			width := w.bits / 8
			w.samples = make([]int32, n/width)
			for i := range w.samples {
				w.samples[i] = int32(int16(binary.LittleEndian.Uint16(body[i*width:])))
			}
		}
		pos += 8 + n + n&1
	}
	if w.bits != 16 {
		t.Fatalf("这份素材是 %d 位，本测试只按 16 位写 AIFF", w.bits)
	}
	return w
}

func stereo16(t *testing.T) wav {
	t.Helper()
	return readWAV(t, filepath.Join("flacdec", "testdata", "stereo16.wav"))
}

// putExtended80 把一个正整数写进 10 字节的 80 位 IEEE 扩展精度浮点。
//
// 采样率只有用这种格式才装得下 44100 这种整数。整数整除、不经过浮点，
// 因为 44100 * 2^48 超过 float64 的 53 位尾数，走浮点会算错低位。
func putExtended80(b []byte, v uint64) {
	m, shift := v, 0
	for m >= 1<<63 {
		m >>= 1
		shift++
	}
	binary.BigEndian.PutUint16(b[:2], uint16(16383+63-shift))
	binary.BigEndian.PutUint64(b[2:10], m)
}

// buildAIFF 把交错的 16 位样本写成一个 AIFF 文件。form 传 "AIFC" 时写压缩类型。
func buildAIFF(form string, samples []int32, channels, rate int, compression string) []byte {
	// COMM：声道数、帧数、样本位宽、80 位扩展精度的采样率。
	comm := make([]byte, 18)
	binary.BigEndian.PutUint16(comm[0:], uint16(channels))
	binary.BigEndian.PutUint32(comm[2:], uint32(len(samples)/channels))
	binary.BigEndian.PutUint16(comm[6:], 16)
	putExtended80(comm[8:18], uint64(rate))
	if form == "AIFC" {
		comm = append(comm, compression...)
		comm = append(comm, 0) // 压缩名的长度字节
	}

	// SSND：offset 与 blockSize 两个字段，之后才是样本。
	ssnd := make([]byte, 8+len(samples)*2)
	for i, s := range samples {
		binary.BigEndian.PutUint16(ssnd[8+i*2:], uint16(int16(s)))
	}

	body := []byte(form)
	body = append(body, chunk("COMM", comm)...)
	body = append(body, chunk("SSND", ssnd)...)

	out := make([]byte, 8, 8+len(body))
	copy(out, "FORM")
	binary.BigEndian.PutUint32(out[4:], uint32(len(body)))
	return append(out, body...)
}

// chunk 给一个 IFF chunk 补上长度字段和偶数字节的填充。
func chunk(id string, data []byte) []byte {
	out := make([]byte, 0, 8+len(data)+1)
	out = append(out, id...)
	out = append(out, byte(len(data)>>24), byte(len(data)>>16), byte(len(data)>>8), byte(len(data)))
	out = append(out, data...)
	if len(data)%2 == 1 {
		out = append(out, 0)
	}
	return out
}

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestAIFFDecodesBackToTheSameSamples 是 AIFF 的主用例：造一份，解回来逐样本比。
func TestAIFFDecodesBackToTheSameSamples(t *testing.T) {
	w := stereo16(t)
	path := writeTemp(t, "roundtrip.aiff", buildAIFF("AIFF", w.samples, w.channels, w.rate, ""))

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	d, format, err := mygoaudio.NewDecoder(f)
	if err != nil {
		t.Fatal(err)
	}
	if format != "aiff" {
		t.Errorf("认出来的格式是 %q，want aiff", format)
	}
	if d.SampleRate() != w.rate || d.Channels() != w.channels {
		t.Fatalf("是 %d Hz %d 声道，素材是 %d Hz %d 声道",
			d.SampleRate(), d.Channels(), w.rate, w.channels)
	}
	if want := int64(len(w.samples) / w.channels); d.Len() != want {
		t.Errorf("Len() 是 %d，want %d 帧", d.Len(), want)
	}

	got := readAllSamples(t, d)
	if len(got) != len(w.samples) {
		t.Fatalf("解出 %d 个样本，素材有 %d 个", len(got), len(w.samples))
	}
	for i := range got {
		want := float32(w.samples[i]) / (1 << 15)
		if got[i] != want {
			t.Fatalf("第 %d 个样本是 %v，want %v（这是大端序解错了还是字节宽度错了）", i, got[i], want)
		}
	}
}

// TestAIFFSeekLandsExactly 验跳位置：seek 到第 n 帧，读出来必须正好是素材的第 n 帧。
func TestAIFFSeekLandsExactly(t *testing.T) {
	w := stereo16(t)
	path := writeTemp(t, "seek.aiff", buildAIFF("AIFF", w.samples, w.channels, w.rate, ""))

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d, _, err := mygoaudio.NewDecoder(f)
	if err != nil {
		t.Fatal(err)
	}

	const frame = 1000
	if err := d.SetPosition(frame); err != nil {
		t.Fatal(err)
	}
	got := readAllSamples(t, d)
	start := frame * w.channels
	for i := range got {
		want := float32(w.samples[start+i]) / (1 << 15)
		if got[i] != want {
			t.Fatalf("seek 之后第 %d 个样本是 %v，want %v", i, got[i], want)
		}
	}

	// seek 到末尾是合法的位置，读出来应该是干净的 EOF。
	if err := d.SetPosition(d.Len()); err != nil {
		t.Errorf("seek 到末尾报错：%v", err)
	}
	if n, err := d.Read(make([]float32, 64*w.channels)); n != 0 || err != io.EOF {
		t.Errorf("末尾读到 n=%d err=%v，want 0 和 io.EOF", n, err)
	}
	// 越界要拒绝，而不是给一段别的数据。
	if err := d.SetPosition(d.Len() + 1); err == nil {
		t.Error("seek 越过末尾居然成功了")
	}
}

// TestAIFCRejectsCompressed 验压缩过的 AIFC 明确报错，而不是播一段噪声。
func TestAIFCRejectsCompressed(t *testing.T) {
	w := stereo16(t)
	path := writeTemp(t, "comp.aifc", buildAIFF("AIFC", w.samples, w.channels, w.rate, "alaw"))

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, _, err := mygoaudio.NewDecoder(f); err == nil {
		t.Fatal("alaw 压缩的 AIFC 居然解出来了")
	} else if !errors.Is(err, mygoaudio.ErrFormat) {
		t.Logf("报的错是 %v（不是 ErrFormat，是具体的格式错）", err)
	}
}

// TestExtended80RoundTrip 单独盯那个 80 位浮点：采样率算错就是变调。
func TestExtended80RoundTrip(t *testing.T) {
	for _, want := range []float64{8000, 22050, 44100, 48000, 96000, 192000} {
		b := make([]byte, 10)
		putExtended80(b, uint64(want))
		if got := extended80(b); math.Abs(got-want) > 1e-6 {
			t.Errorf("采样率 %v 写进去读出来是 %v", want, got)
		}
	}
}

func readAllSamples(t *testing.T, d mygoaudio.Decoder) []float32 {
	t.Helper()
	buf := make([]float32, 4096*d.Channels())
	var out []float32
	for {
		n, err := d.Read(buf)
		out = append(out, buf[:n]...)
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("解码出错：%v", err)
		}
		if n == 0 {
			t.Fatal("解码没有进展")
		}
	}
}
