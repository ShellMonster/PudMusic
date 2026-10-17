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

		// ⚠️ 取**五轮里最快的那次**，不是一次就下结论。
		//
		// `go test ./...` 是并行的：本包在跑解码速度的时候，extaf 那边的
		// MP4 用例正在同一台机器上抢 CPU（它走系统解码器，很吃 CPU）。
		// 一次测量碰上这种抢占就红，而「解码器变慢了」和「旁边有人在抢 CPU」
		// 是两回事——分不出来，而分不出来的那一次会被当成回归去查半天。
		//
		// 五轮取最快不是把断言放松：解码器真的慢的话五轮都会慢。
		//
		// 轮数不是拍脑袋定的：`go test ./...` 并行跑包，extaf 那边走系统
		// 解码器的 MP4 用例在同一台机器上抢 CPU，而本包整个只跑几秒，
		// 三轮里一轮落在干净窗口的概率还不够——实测三轮仍有大约三分之一
		// 的整包跑会红。五轮之后没再见过。
		//
		// ⚠️ `-race` 下**一轮都不多跑**。这一族在 `underRace` 时判据是空的
		// （下面那个 `&& !underRace`），取五轮最快是为了「够格去判」，没有判据
		// 的那几轮量出来的数谁也不看——而每轮是一次完整解码，五个 fixture 就是
		// 二十五次。这曾经是 `-race` 下这个包最贵的一段开销，而它一个字都判不了。
		if !underRace {
			for range 4 {
				if ratio >= c.minRatio {
					break // 已经够了，不用再跑
				}
				if r, _, _, tk := decodeOnce(t, c.name); r > ratio {
					ratio, took = r, tk
				}
			}
		}

		t.Logf("%s: %d frames of %d Hz in %v, %.0f times real time", c.name, frames, rate, took, ratio)
		if ratio < c.minRatio && !underRace {
			t.Errorf("%s decoded at %.0f times real time (五轮最快), want at least %.0f", c.name, ratio, c.minRatio)
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
//
// ⚠️ 判据是**这次 seek 从文件里拿走了多少字节**，不是墙钟。
//
// 墙钟在这个仓库里当判据必然翻车：`go test ./...` 并行跑整包，`go test -race ./...`
// 还会给解码器加一两个数量级的开销，同一段代码在两种跑法下能差出一百倍。拿它当判据，
// 测的就不是 seek，是当天机器有多闲。上面 TestDecodeSpeed 那段注释记的是同一个坑的
// 第一现场（`underRace` 就是那儿想出来的，见 race_on_test.go）；这里是第二现场：这里
// 当初也写了 `underRace`，那只让 `-race` 不再翻红，机器一忙照样翻。
//
// 字节是确定的。一段音频压成多少字节由数据决定，同一个 fixture 在哪台机器上都是同
// 一个整数，机器快慢、CPU 忙不忙都动不了它。解码器变慢也不会让 seek 少读一个字节，
// 所以这把尺子量的仍然是 seek 自己的工作量：「一次 seek 碰了多少音频」既不是它慢的
// 原因，也几乎就是它慢的全部原因。
//
// 墙钟没有扔掉，只是挪到了只测不判的地方：下面的 BenchmarkSeek。
func TestSeekSpeed(t *testing.T) {
	const name = "testdata/stereo16.flac"
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	src := &countingReader{r: f, chunk: readChunk}
	d, err := flacdec.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	rate := int64(d.SampleRate())
	if d.Len() < 4*rate {
		t.Fatalf("the fixture is %d frames at %d Hz, too short to seek around in", d.Len(), rate)
	}
	buf := make([]float32, 1024*d.Channels())

	// 尺子：一秒的音频在这个 fixture 上压出来多少字节。native.go 把索引的间隔写死成
	// checkpointSeconds = 1 秒，并且写明「所以一次往回 seek 的代价上界是一秒的解码」——
	// 下面那道判据就是这句话的字节版本。
	interval := bytesForFrames(t, name, rate)

	// 第一次：seek 进谁都没播过的地方，停在结尾前一秒。索引是空的，只能从文件头一路
	// 走，走过的路顺手把索引填上，所以这一次贵，而且它贵得有道理——那段音频本来就得解。
	// 停在一秒前而不是两秒前，是为了让第二次 seek 落在索引已经盖住的地方：这个
	// fixture 只有四秒长，「结尾前两秒」恰好是整首歌的正中，往回 seek 就成了原地
	// seek，两次量的是同一件事。
	fresh := d.Len() - rate
	var start time.Time
	src.n, start = 0, time.Now()
	if err := d.SetPosition(fresh); err != nil {
		t.Fatal(err)
	}
	if n, err := d.Read(buf); n == 0 || (err != nil && err != io.EOF) {
		t.Fatalf("a seek into unplayed ground returned nothing: n=%d err=%v", n, err)
	}
	walked, took := src.n, time.Since(start)
	t.Logf("a seek into unplayed ground walked %d bytes to frame %d in %v", walked, fresh, took)

	// 走到目标就该停。这次 seek 只能读「从文件头到目标」那段音频的字节，而那段音频
	// 值多少字节，就是 fresh/rate 个 checkpoint 区间。这是均速外推，而外推给的是
	// 一个偏松的数（这个 fixture 上三段实际是 184320 字节，外推给 196608），所以这条
	// 判据只断「越过目标一大段」那种回归，判的是粗不是细；一路解到文件尾是四个区间，
	// 掉得下来。
	span := (fresh / rate) * interval
	if walked > span+2*readChunk {
		t.Errorf("a seek to frame %d read %d bytes, more than the %d that the first %d frames of the file cost: it walked past its target",
			fresh, walked, span, fresh)
	}

	// 第二次：往回 seek 到索引已经盖住的地方。索引每 checkpointSeconds 记一格，所以
	// 这一次最多重解一格，不该再从文件头起步。
	back := d.Len() / 2
	src.n, start = 0, time.Now()
	if err := d.SetPosition(back); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read(buf); err != nil {
		t.Fatal(err)
	}
	indexed, tookBack := src.n, time.Since(start)
	t.Logf("a backward seek to frame %d read %d bytes in %v (one checkpoint interval of audio costs %d)",
		back, indexed, tookBack, interval)

	// 判据：一秒的音频，外加字节数看不到的两处余量——walk 是按整帧解的，落点那一帧要
	// 整帧读进来（这个 fixture 上是 4608 帧，约一秒的十分之一），读前瞻又最多多一个
	// chunk。四分之一把两处都盖住了，还给「真的退回从文件头起步」那条回归留了够它
	// 掉下来的距离：那是两秒的音频。
	if indexed > interval+interval/4 {
		t.Errorf("a backward seek over indexed ground read %d bytes, more than the %d that one checkpoint interval of audio costs: it is walking from the file head again",
			indexed, interval)
	}
}

// readChunk is how much countingReader hands over at a time. It is also the slack
// the byte bounds above allow for: the decoder reads through a 64 KB buffer, so a
// byte count is only ever an upper bound on the audio a seek really touched.
const readChunk = 4096

// countingReader counts the bytes a decoder pulls out of the file. The chunk cap
// is the point: an uncapped count would say how big the decoder's read-ahead
// buffer is rather than how much of the stream a seek walked.
type countingReader struct {
	r     io.ReadSeeker
	chunk int
	n     int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	if len(p) > c.chunk {
		p = p[:c.chunk]
	}
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (c *countingReader) Seek(off int64, whence int) (int64, error) { return c.r.Seek(off, whence) }

// bytesForFrames decodes the first frames frames of a fixture from the start and
// reports how many bytes of the file that took: the price of that stretch of audio,
// which is what a seek has to pay to walk it. The metadata is read by Open, and
// the count starts after it, so what is measured is the frames.
//
// Keep the stretch short: the acceptance gate runs this under -race, where every
// frame of audio is instrumented, and a ruler that costs more to build than the
// thing it measures is a bad trade.
func bytesForFrames(t *testing.T, name string, frames int64) int64 {
	t.Helper()
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	src := &countingReader{r: f, chunk: readChunk}
	d, err := flacdec.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	src.n = 0

	buf := make([]float32, 1024*d.Channels())
	for got := int64(0); got < frames; {
		n, err := d.Read(buf)
		got += int64(n / d.Channels())
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decoding %d frames of %s: %d frames, %v", frames, name, got, err)
		}
	}
	return src.n
}

// BenchmarkSeek is the wall clock TestSeekSpeed no longer judges: a seek in
// seconds. A benchmark never runs under a plain `go test`, so a busy machine
// cannot turn a green run red; asking for the number means asking with -bench.
func BenchmarkSeek(b *testing.B) {
	const name = "testdata/stereo16.flac"

	// A backward seek over ground that has already been walked, which is the seek a
	// player makes when someone drags the progress bar back.
	b.Run("indexed", func(b *testing.B) {
		d, err := flacdec.OpenFile(name)
		if err != nil {
			b.Fatal(err)
		}
		defer d.Close()
		buf := make([]float32, 1024*d.Channels())
		// Warm the index before the timer starts: this case is about a seek over
		// walked ground, and a cold start would measure the whole prefix instead.
		seekAndRead(b, d, d.Len()-int64(d.SampleRate()), buf)
		targets := []int64{d.Len() / 2, d.Len() / 3, 2 * d.Len() / 3, d.Len() / 4}
		b.ResetTimer()
		for i := range b.N {
			seekAndRead(b, d, targets[i%len(targets)], buf)
		}
	})

	// A seek into ground nobody has played, which has to walk from the file head and
	// so costs the length of the prefix. A fresh decoder each time, or the first
	// iteration would leave an index behind for every one after it — which is also
	// why the number includes opening the file.
	b.Run("cold", func(b *testing.B) {
		probe, err := flacdec.OpenFile(name)
		if err != nil {
			b.Fatal(err)
		}
		target, ch := probe.Len()-int64(probe.SampleRate()), probe.Channels()
		probe.Close()
		buf := make([]float32, 1024*ch)
		b.ResetTimer()
		for range b.N {
			d, err := flacdec.OpenFile(name)
			if err != nil {
				b.Fatal(err)
			}
			seekAndRead(b, d, target, buf)
			d.Close()
		}
	})
}

// seekAndRead is what a player does with a seek: move the decoder, then pull the
// first chunk of audio. Both halves are counted, and the second one is not
// optional: a bare-FLAC walk is lazy, so SetPosition only files a seek and the
// frames it has to skip over are decoded by the Read that follows.
func seekAndRead(b *testing.B, d *flacdec.Decoder, target int64, buf []float32) {
	b.Helper()
	if err := d.SetPosition(target); err != nil {
		b.Fatal(err)
	}
	if _, err := d.Read(buf); err != nil {
		b.Fatal(err)
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
