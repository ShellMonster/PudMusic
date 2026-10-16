package flacdec_test

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/audio/flacdec"
)

// Decode speed is what decides whether a FLAC track can be played while it is
// still downloading: a decoder that cannot keep up with real time cannot start
// instantly. The numbers below are for one core of the development machine, and
// the ratio that matters is seconds of audio per second of wall clock.

// TestDecodeSpeed is a check rather than a benchmark, so a regression shows up as
// a failing test. It decodes a whole fixture and reports how far ahead of real
// time it stayed.
func TestDecodeSpeed(t *testing.T) {
	for _, c := range []struct {
		name     string
		minRatio float64
	}{
		// A player needs to decode faster than the track plays or it runs dry.
		{"stereo16.flac", 20},
		{"stereo24.flac", 20},
		{"stereo16.mp4", 20},
		{"stereo24.mp4", 20},
		{"mono16.flac", 20},
	} {
		ratio, frames, rate, took := decodeOnce(t, c.name)

		// ⚠️ 取**三轮里最快的那次**，不是一次就下结论。
		//
		// `go test ./...` 是并行的：本包在跑解码速度的时候，extaf 那边的
		// MP4 用例正在同一台机器上抢 CPU（它走系统解码器，很吃 CPU）。
		// 一次测量碰上这种抢占就红，而「解码器变慢了」和「旁边有人在抢 CPU」
		// 是两回事——分不出来，而分不出来的那一次会被当成回归去查半天。
		//
		// 三轮取最快不是把断言放松：解码器真的慢的话三轮都会慢。
		for range 2 {
			if !underRace && ratio >= c.minRatio {
				break // 已经够了，不用再跑
			}
			if r, _, _, tk := decodeOnce(t, c.name); r > ratio {
				ratio, took = r, tk
			}
		}

		t.Logf("%s: %d frames of %d Hz in %v, %.0f times real time", c.name, frames, rate, took, ratio)
		if ratio < c.minRatio && !underRace {
			t.Errorf("%s decoded at %.0f times real time (三轮最快), want at least %.0f", c.name, ratio, c.minRatio)
		}
	}
}

// decodeOnce 把一个文件从头解一遍，返回「几倍实时」。
func decodeOnce(t *testing.T, name string) (ratio float64, frames int64, rate int, took time.Duration) {
	t.Helper()
	d, err := flacdec.OpenFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	ch := d.Channels()
	buf := make([]float32, 4096*ch)
	start := time.Now()
	for {
		n, err := d.Read(buf)
		frames += int64(n / ch)
		if err != nil {
			if err != io.EOF {
				t.Fatal(err)
			}
			break
		}
	}
	took = time.Since(start)
	rate = d.SampleRate()
	audio := time.Duration(frames) * time.Second / time.Duration(rate)
	return float64(audio) / float64(took), frames, rate, took
}

// TestSeekSpeed covers the other latency a player feels: how long a seek takes.
// The bare-FLAC path with no seek table walks the stream, so its cost depends on
// how much has been decoded already.
func TestSeekSpeed(t *testing.T) {
	d, err := flacdec.OpenFile("testdata/stereo16.flac")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	// A seek into ground nobody has played: the index has nothing, so the decoder
	// has to walk from the start. Two seconds short of the end, so that there is
	// audio on the other side to time the seek to.
	const from = 2 * 44100
	buf := make([]float32, 4096*d.Channels())
	start := time.Now()
	if err := d.SetPosition(d.Len() - from); err != nil {
		t.Fatal(err)
	}
	if n, err := d.Read(buf); err != nil && n == 0 {
		t.Fatalf("seek into unplayed ground returned nothing: %v", err)
	}
	first := time.Since(start)
	t.Logf("a seek into unplayed ground, with a walk behind it, took %v", first)
	// ⚠️ 阈值在 `-race` 下**必须**跳过，同一个文件上面那个 TestDecodeSpeed
	// 早就这么做了（`underRace`，见 race_on_test.go）。这里漏了，于是
	// `go test -race ./...` 每次都红——而一条一直红的验收门会逼着每个
	// 读它的人（包括自动评审）先花十几分钟去证明「不是我弄坏的」。
	if first > 5*time.Second && !underRace {
		t.Errorf("a first seek took %v, which a player cannot hide", first)
	}

	// The index is filled now, so a seek back is bounded by the checkpoint interval.
	start = time.Now()
	if err := d.SetPosition(d.Len() / 2); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read(buf); err != nil {
		t.Fatal(err)
	}
	second := time.Since(start)
	t.Logf("a seek back over an indexed stream took %v", second)
	if second > 500*time.Millisecond && !underRace {
		t.Errorf("a backward seek took %v", second)
	}
}

// BenchmarkDecode measures the decode itself, without the seek and truncate work
// the tests above do.
func BenchmarkDecode(b *testing.B) {
	for _, name := range []string{"stereo16.flac", "stereo24.flac", "stereo16.mp4"} {
		b.Run(name, func(b *testing.B) {
			data, err := readFileB(b, "testdata/"+name)
			if err != nil {
				b.Fatal(err)
			}
			d, err := flacdec.New(data)
			if err != nil {
				b.Fatal(err)
			}
			ch := d.Channels()
			buf := make([]float32, 4096*ch)
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := d.SetPosition(0); err != nil {
					b.Fatal(err)
				}
				for {
					_, err := d.Read(buf)
					if err != nil {
						break
					}
				}
			}
		})
	}
}

func readFileB(b *testing.B, path string) ([]byte, error) {
	b.Helper()
	return os.ReadFile(path)
}
