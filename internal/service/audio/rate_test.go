package audio

import (
	"io"
	"math"
	"strconv"
	"sync"
	"testing"
	"time"

	mygoaudio "github.com/egoist/mygo/audio"
)

// 变速器（WSOLA）的用例。
//
// # 为什么不靠听
//
// 「音高没变」这件事必须能被机器断掉，否则每次评审都得有人戴着耳机听一遍
// 2 倍速的人声，而「听着没变」和「确实没变」差着一整个八度。三条断言把它钉住：
//
//	TestUnityRateIsSampleForSample  1 倍速逐样本原样
//	TestRateKeepsThePitch           每一档的输出主频都还在原处
//	TestRateShortensAndLengthens     输出的长短跟着倍率走
//
// 「变速中 seek 准确」和「时钟跟着位置走」各有一条（见下面）。

// ── 测试用的解码器 ────────────────────────────────────────────────────────

// memDec 是一段全在内存里的解码器，用来看清楚这一层到底吐了什么。
type memDec struct {
	samples  []float32 // 交错的
	ch, rate int
	pos      int64
	seekErr  error
	reads    int
}

func (d *memDec) SampleRate() int { return d.rate }
func (d *memDec) Channels() int   { return d.ch }
func (d *memDec) Len() int64      { return int64(len(d.samples) / d.ch) }

func (d *memDec) Read(buf []float32) (int, error) {
	d.reads++
	left := d.Len() - d.pos
	if left <= 0 {
		return 0, io.EOF
	}
	frames := min(int64(cap(buf)/d.ch), left)
	copy(buf, d.samples[d.pos*int64(d.ch):(d.pos+frames)*int64(d.ch)])
	d.pos += frames
	return int(frames) * d.ch, nil
}

func (d *memDec) SetPosition(f int64) error {
	if d.seekErr != nil {
		return d.seekErr
	}
	d.pos = max(f, 0)
	return nil
}

// ── 信号 ──────────────────────────────────────────────────────────────────

// tone 造一段单音。频率单位是 Hz，长度单位是秒。
func tone(rate int, ch int, freq float64, seconds float64) []float32 {
	n := int(float64(rate)*seconds) * ch
	out := make([]float32, n)
	for i := 0; i < n; i += ch {
		v := float32(0.4 * math.Sin(2*math.Pi*freq*float64(i/ch)/float64(rate)))
		for c := 0; c < ch; c++ {
			out[i+c] = v
		}
	}
	return out
}

// swell 造一段**能量一路递增**的单音。
//
// 它是给「比的是点积还是余弦」准备的：点积最大的那一段就是能量最大的那一段，
// 于是一段渐强的音乐会把搜索点一路往后拽，1 倍速就不透明了。见 aNext 的注释。
func swell(rate int, ch int, freq float64, seconds float64) []float32 {
	n := int(float64(rate)*seconds) * ch
	out := make([]float32, n)
	for i := 0; i < n; i += ch {
		f := float64(i/ch) / float64(n/ch)
		v := float32((0.05 + 0.45*f) * math.Sin(2*math.Pi*freq*float64(i/ch)/float64(rate)))
		for c := 0; c < ch; c++ {
			out[i+c] = v
		}
	}
	return out
}

// gritty 往一段纯音上撒一点**确定性的**噪声。
//
// 干什么用：给「对位」类的用例准备素材。一段纯正弦的对齐是**有周期性歧义**的
// ——往右挪 22 个整周期（2940 帧）之后，它和原位逐样本只差浮点末位，相关性
// 一样是 0.9999999。于是「扫一遍找最像的位置」会落在歌里另一个一模一样的地方，
// 而代码其实是对的。加一点噪声（真实音乐里到处都是）之后对齐就唯一了。
//
// 噪声是定死的（线性同余），不用 math/rand 的全局种子：同一个用例每次跑出来
// 一样，红了能复现。
func gritty(in []float32) []float32 {
	out := append([]float32(nil), in...)
	seed := uint32(2463534242)
	for i := range out {
		seed = seed*1664525 + 1013904223
		out[i] += float32(float64(int32(seed>>8))/float64(1<<23))*0.05 - 0.025
	}
	return out
}

// drain 把一个解码器读到完，交错样本原样返回。
func drain(t *testing.T, d mygoaudio.Decoder, ch int) []float32 {
	t.Helper()
	buf := make([]float32, 512*ch)
	var out []float32
	for i := 0; i < 20000; i++ {
		n, err := d.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			return out
		}
		if n == 0 {
			// (0, nil) 在 MyGo 那边等于流到头了，会把一首好歌当场截断。
			t.Fatalf("读到 0 帧却没有报错（第 %d 次）", i)
		}
	}
	t.Fatal("读了两万次还没读完")
	return nil
}

// dominantFreq 是 out 中间那一段的主频（Hz）。
//
// 用 Goertzel 在 [lo, hi] 上一步步扫，不做 FFT：这里要的不是精确的谱，
// 是「主峰还在不在原地」——差半个八度（1.5 倍速的变调）一扫就分得开。
func dominantFreq(out []float32, rate int, ch int, lo, hi float64) float64 {
	const n = 16384
	if len(out) < n*ch {
		return 0
	}
	off := ((len(out)/ch - n) / 2) * ch
	seg := out[off : off+n*ch]

	best, bestMag := lo, 0.0
	for f := lo; f <= hi; f += 1 {
		mag := goertzel(seg, ch, rate, f)
		if mag > bestMag {
			best, bestMag = f, mag
		}
	}
	return best
}

// goertzel 是单个频点上的能量（已加汉宁窗，抑制旁瓣）。
func goertzel(seg []float32, ch, rate int, freq float64) float64 {
	const n = 16384
	w := 2 * math.Pi * freq / float64(rate)
	c := 2 * math.Cos(w)
	var s0, s1, s2, norm float64
	for i := 0; i < n; i++ {
		x := float64(seg[i*ch]) * (0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/n))
		s0 = x + c*s1 - s2
		s2, s1 = s1, s0
		norm += x * x
	}
	mag := s1*s1 + s2*s2 - c*s1*s2
	return mag / (norm + 1e-12)
}

// bestOffset 在 src 的 [want-span, want+span] 里找一段和 out 最像的位置。
func bestOffset(out, src []float32, ch, want, span int) int {
	best, bestScore := want, math.Inf(-1)
	for s := want - span; s <= want+span; s++ {
		if s < 0 || (s+len(out)/ch)*ch > len(src) {
			continue
		}
		var dot, energy float64
		seg := src[s*ch:]
		for i := 0; i < len(out); i += ch {
			x := float64(out[i])
			dot += x * float64(seg[i])
			energy += float64(seg[i]) * float64(seg[i])
		}
		score := dot / (math.Sqrt(energy) + 1e-12)
		if score > bestScore {
			best, bestScore = s, score
		}
	}
	return best
}

// ── 用例 ──────────────────────────────────────────────────────────────────

// TestUnityRateIsSampleForSample 盯「1 倍速就是原样」。
//
// 这不是洁癖：1 倍速是用户用得最多的那一档，也是从别的倍率切回来的落点。
// 只要这里有哪怕一点偏差，用户就会听到「按了一下 1.0x，声音轻微地跳了一下」。
//
// 用一段**能量一路递增**的信号：点积式的搜索会挑能量大的那一段，于是起点被拽走，
// 这一条立刻红。
func TestUnityRateIsSampleForSample(t *testing.T) {
	const rate = 44100
	src := swell(rate, 2, 220, 1.5)
	v := newVarispeed(&memDec{samples: src, ch: 2, rate: rate})

	got := drain(t, v, 2)

	// 头一跳是窗子的爬升段（没有上一帧和它重叠），尾两跳是按「淡出」铺的
	// （源里没有了，窗子也收到 0）——两端本来就该和源不一样，不在这条的
	// 断言范围里。
	const ch = 2
	head := v.hop * ch     // 开头爬升
	tail := 3 * v.hop * ch // 结尾淡出（源 + 冲尾那一跳）
	clean := len(src) - head - tail
	if clean <= 0 {
		t.Fatalf("信号太短：%d 帧", len(src)/ch)
	}
	if len(got) < head+clean {
		t.Fatalf("只吐了 %d 个样本，比要比的 %d 还少", len(got), head+clean)
	}
	for i := range clean {
		a, b := src[head+i], got[head+i]
		if d := math.Abs(float64(a - b)); d > 1e-6 {
			t.Fatalf("第 %d 个样本不一样：源 %g，出来 %g（差 %g）", head+i, a, b, d)
		}
	}
}

// TestRateKeepsThePitch 是这一票的主用例：每一档的音高都还在原处。
//
// 变调的那条路（MyGo 的 SetSpeed）在 1.5 倍速会把 440Hz 推到 660Hz，
// 在 0.5 倍速推到 220Hz。这里逐档量输出的主峰，差一整个半音以上就红。
func TestRateKeepsThePitch(t *testing.T) {
	const (
		rate = 44100
		f0   = 440.0
	)
	src := tone(rate, 1, f0, 1.5)

	for _, r := range RateSteps {
		t.Run(rateLabel(r), func(t *testing.T) {
			v := newVarispeed(&memDec{samples: src, ch: 1, rate: rate})
			v.setRate(r)
			got := drain(t, v, 1)

			f := dominantFreq(got, rate, 1, 200, 900)
			// ±10Hz ≈ 0.4 个半音。窗长 16384 的分辨力是 2.7Hz，窗函数把主瓣
			// 撑到两三个格子，10Hz 是「主峰确实在那儿」的宽容量。
			if math.Abs(f-f0) > 10 {
				t.Errorf("主峰跑到 %.0f Hz 上去了（源是 %.0f）——这一档变调了", f, f0)
			}
			// 再明确断一次「它没有跑到变调该去的那儿」。
			if shifted := f0 * r; r != 1 && math.Abs(f-shifted) < 20 {
				t.Errorf("主峰 %.0f Hz 正好落在变调该去的 %.0f Hz 上", f, shifted)
			}
		})
	}
}

// TestRateShortensAndLengthens 盯输出的长短跟着倍率走。
//
// ⚠️ 方向很容易搞反，而且搞反了**几乎全绿**：2 倍速该让输出变**短**（同样长度
// 的源，两秒的歌在两倍速下只吐一秒的输出），写成乘就成了「两秒的源吐四秒」——
// 那不是 2 倍速，是 0.5 倍速，而音高仍然是对的。这一条就是钉这个方向的。
func TestRateShortensAndLengthens(t *testing.T) {
	const (
		rate = 44100
		n    = 44100 * 2 // 两秒
	)
	src := tone(rate, 2, 440, 2)

	for _, r := range RateSteps {
		t.Run(rateLabel(r), func(t *testing.T) {
			v := newVarispeed(&memDec{samples: src, ch: 2, rate: rate})
			v.setRate(r)
			frames := int64(len(drain(t, v, 2)) / 2)

			// 输出一跳走合成跳、源走合成跳×倍率，所以 输出:源 = 1:倍率。
			want := float64(n) / r
			tol := math.Max(float64(6*v.hop), want*0.02)
			if math.Abs(float64(frames)-want) > tol {
				t.Errorf("吐了 %d 帧，%.1f 倍速该是 %.0f 帧（容差 %.0f）",
					frames, r, want, tol)
			}
		})
	}
}

// TestSetPositionLandsWhereItWasAsked 盯「变速中 seek 准确」。
//
// 做法是让输出和源**对位**：扫一遍源，找出哪一段和输出长得最像，位置必须落在
// seek 到的那个地方附近。对位量是扫出来的，不写死——WSOLA 每跳都会在名义位置
// 附近挪一点，写死反而会把正常的行为当成错。
//
// 顺带也盯住了最容易写错的那处：seek 的坐标是**源帧**。若坐标搞成了输出帧，
// 2 倍速下 seek 到 30 秒会落在源的 15 秒处，这一条立刻红。
func TestSetPositionLandsWhereItWasAsked(t *testing.T) {
	const (
		rate = 44100
		f0   = 330
	)
	src := gritty(tone(rate, 1, f0, 6))

	for _, r := range []float64{0.5, 0.75, 1, 1.5, 2} {
		t.Run(rateLabel(r), func(t *testing.T) {
			seekTo := int64(rate * 3) // 第三秒
			v := newVarispeed(&memDec{samples: src, ch: 1, rate: rate})
			v.setRate(r)
			if err := v.SetPosition(seekTo); err != nil {
				t.Fatal(err)
			}
			got := drain(t, v, 1)

			// 只拿**第二跳**去对位（第一跳是窗子的爬升段）。
			//
			// 为什么不能拿更长的一段：WSOLA 每一跳都在名义位置附近挪最多一个搜索
			// 半径，攒得越久这一段就越「不平」，拿它去和一段干净的源做互相关，
			// 峰值会散开。拿长窗口量出来的偏差量的是抖动，不是 seek 准不准。
			win := got[v.hop : 2*v.hop]
			// 过了两跳，源也跟着走了两跳 × 倍率。
			want := int(seekTo) + int(math.Round(float64(v.hop)*r))
			got1 := bestOffset(win, src, 1, want, 4*v.hop)
			if d := got1 - want; d < -2*v.search || d > 2*v.search {
				t.Errorf("seek 到源第 %d 帧，%.2f 倍速下第二跳却对得上源第 %d 帧"+
					"（该是 %d，差 %d 帧）", seekTo, r, got1, want, d)
			}
		})
	}
}

// TestSourceAtWalksTheSong 盯「变速时钟与播放位置同步」。
//
// `SourceAt` 是歌内位置的算法：输入是 MyGo 那个播放器数的「输出帧」，
// 出来的是「源帧」。这条用例让它从头走到尾，中途必须一路单调、斜率是 1/倍率，
// 最后落在源的总长上。
//
// 斜率这一条最要紧：斜率错了就是「1.5 倍速下进度条走成原速」或者反过来——
// 两种都只有盯着进度条看很久才发现。
func TestSourceAtWalksTheSong(t *testing.T) {
	const (
		rate = 44100
		n    = 44100 * 3
	)
	src := tone(rate, 2, 440, 3)

	for _, r := range []float64{0.5, 1, 1.75, 2} {
		t.Run(rateLabel(r), func(t *testing.T) {
			v := newVarispeed(&memDec{samples: src, ch: 2, rate: rate})
			v.setRate(r)
			buf := make([]float32, 2048*2)

			var produced int64
			last := v.SourceAt(0)
			for {
				gotN, err := v.Read(buf)
				if gotN > 0 {
					produced += int64(gotN / 2)
					cur := v.SourceAt(produced - 1)
					if cur < last-1 {
						t.Fatalf("位置倒退了：第 %d 帧时 %d，上一次 %d", produced, cur, last)
					}
					// 走了 out 帧输出，源该走「out × 倍率」帧。
					step := float64(produced) * r
					if d := math.Abs(float64(cur) - step); d > float64(4*v.hop)+step*0.05 {
						t.Fatalf("产出 %d 帧输出，源走到第 %d 帧，该是 %.0f（差 %.0f）",
							produced, cur, step, d)
					}
					last = cur
				}
				if err != nil {
					break
				}
			}
			// 走到底，位置要落在源的总长上。
			if d := math.Abs(float64(last) - float64(n)); d > float64(6*v.hop) {
				t.Errorf("读完时位置在第 %.0f 帧，源一共有 %d 帧", float64(last), n)
			}
		})
	}
}

// TestSourceAtSurvivesASeek 盯 seek 之后位置立刻就是 seek 到的那个地方。
//
// 对不上这一条的表现是：拖完进度条，进度条先弹回歌头，过几十毫秒才跳到
// 拖到的地方。
func TestSourceAtSurvivesASeek(t *testing.T) {
	const rate = 44100
	src := tone(rate, 1, 440, 4)
	v := newVarispeed(&memDec{samples: src, ch: 1, rate: rate})
	v.setRate(2)

	// 先铺掉一点，好让 outFrames 不是一个干净的开头。
	drain(t, v, 1)

	for _, at := range []int64{0, 12345, 44100 * 2, int64(len(src)) - 1} {
		if err := v.SetPosition(at); err != nil {
			t.Fatal(err)
		}
		if got := v.SourceAt(at); got != at {
			t.Errorf("seek 到源第 %d 帧，SourceAt 说的是第 %d 帧", at, got)
		}
	}
}

// TestSetRateMidStreamDoesNotJump 盯「改倍率时位置不跳」。
//
// 变速层接着当前这一跳往下铺，不清缓冲、不 seek，所以改倍率的那一下用户听不到
// 「跳了一格」。这条盯的就是它：改完之后 SourceAt 在原来的位置还连着。
func TestSetRateMidStreamDoesNotJump(t *testing.T) {
	const rate = 44100
	src := tone(rate, 1, 440, 6)
	v := newVarispeed(&memDec{samples: src, ch: 1, rate: rate})

	buf := make([]float32, 4096)
	if n, _ := v.Read(buf); n == 0 {
		t.Fatal("一帧都没铺出来")
	}
	before := v.SourceAt(int64(1024))

	v.setRate(2)
	after := v.SourceAt(1024)

	// 允许一帧的量化，不允许「跳了一个搜索窗」。
	slack := int64(2*v.search + v.hop)
	if d := after - before; d > slack || d < -slack {
		t.Errorf("改倍率之后位置从 %d 跳到了 %d", before, after)
	}
}

// TestChangingRateMidStreamNeverGoesBackwards 盯「换倍率」在算法层也站得住。
//
// 这一条钉的是一处会**直接崩**的地方：换倍率那一刻名义位置是从**当前实际位置**
// 接上的（`aNext` 里那段）。不重定原点的话，2 倍速切 0.5 倍速时新名义位置会
// 落到实际位置后面几百跳，于是分析位置被一路拽着往回走——
//
//	`emit` 里 `v.in[a-inFrom:]` 当场越界，解码 goroutine panic，
//	 整个进程跟着没（那一帧之后什么都没了，界面上表现为「点一下倍速应用就没了」）
//
// 崩溃只是最坏的一种。轻的时候是「刚才那一段被反复重放」，输入缓冲越涨越大。
//
// 走法刻意做成「快→慢」和「慢→快」都试一遍：两个方向的差值符号相反，只试一个
// 方向会漏掉另一半。
func TestChangingRateMidStreamNeverGoesBackwards(t *testing.T) {
	const rate = 44100
	src := gritty(tone(rate, 1, 440, 12))

	for _, step := range []struct {
		name string
		from []float64
	}{
		{"快到慢", []float64{2, 1.75, 1.5, 1.25, 1, 0.75, 0.5}},
		{"慢到快", []float64{0.5, 0.75, 1, 1.25, 1.5, 1.75, 2}},
		{"来回", []float64{2, 0.5, 2, 0.5, 1}},
	} {
		t.Run(step.name, func(t *testing.T) {
			v := newVarispeed(&memDec{samples: src, ch: 1, rate: rate})
			buf := make([]float32, 2048)

			// consumed 是**一共**读走多少输出帧。这一层是直接被读的，没有
			// MyGo 那个播放器在数帧，所以它的坐标就是「读走了多少」。
			var consumed int64
			last := v.SourceAt(0)
			for i, r := range step.from {
				v.setRate(r)
				for range 8 {
					n, err := v.Read(buf)
					consumed += int64(n)
					if n == 0 {
						break
					}
					if err != nil {
						t.Fatalf("第 %d 档（%.2f 倍速）就把源读完了", i, r)
					}
					at := v.SourceAt(consumed)
					if at < last-int64(v.search) {
						t.Fatalf("第 %d 档（%.2f 倍速）：源的位置从 %d 退到了 %d"+
							"——用户听到的是刚才那一段被重放", i, r, last, at)
					}
					last = at
				}
			}
			// 一路换完还能把剩下的读完，且不越界。
			drain(t, v, 1)
		})
	}
}

// stallDec 是「第一次 Read 一直卡到 gate 被打开」的解码器。
//
// 它是这一票**最贵的一个回归**的复现器（见下面那条用例）：读内层解码器时如果
// 拿着位置锁，CDN 卡一下锁就跟着卡一下，而 `Position()` 要这把锁——再往上，
// `App.tick` 拿着 `a.mu` 调 `Position()`，界面每帧开头的 `Snapshot()` 也要
// `a.mu`。于是 CDN 卡 2 秒，整个窗口卡 2 秒；`StreamSource` 的卡死判定是 20 秒。
type stallDec struct {
	samples  []float32
	ch, rate int
	pos      int64

	entered chan struct{} // 读到哪一步了：它正在 Read 里卡着
	release chan struct{} // 打开它：放它出来
	once    sync.Once
}

func (d *stallDec) SampleRate() int { return d.rate }
func (d *stallDec) Channels() int   { return d.ch }
func (d *stallDec) Len() int64      { return int64(len(d.samples) / d.ch) }

func (d *stallDec) SetPosition(f int64) error { d.pos = max(f, 0); return nil }

func (d *stallDec) Read(buf []float32) (int, error) {
	d.once.Do(func() { close(d.entered); <-d.release })
	left := d.Len() - d.pos
	if left <= 0 {
		return 0, io.EOF
	}
	frames := min(int64(cap(buf)/d.ch), left)
	copy(buf, d.samples[d.pos*int64(d.ch):(d.pos+frames)*int64(d.ch)])
	d.pos += frames
	return int(frames) * d.ch, nil
}

// TestPositionDoesNotWaitOnTheDecoder 盯「源卡住时界面不会跟着卡住」。
//
// 这条钉的是 `rate.go` 的 `readInner`：读内层解码器**不持有位置锁**。
// 回归过一次：那时锁横跨 `v.inner.Read`，一个卡住的在线流会把整层锁住，
// `Position()` 于是跟着卡住，再往上 `App.tick` 是**持着 `a.mu`** 调 `Position()`
// 的，而界面每一帧的第一件事就是 `Snapshot()`（要 `a.mu`）——实测 CDN 卡 2 秒，
// 界面一帧卡 2.07 秒，而 `StreamSource` 的卡死判定是 20 秒。
//
// 走 `Player` 而不是直接量 `SourceAt`：那样连「上层真的会去问位置」这一段也
// 一起量到了——`Player.Position()` 就是拿 `SourceAt` 换算的。
func TestPositionDoesNotWaitOnTheDecoder(t *testing.T) {
	const rate = 44100
	src := gritty(tone(rate, 2, 440, 5))
	d := &stallDec{
		samples: src, ch: 2, rate: rate,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}

	// `New` 自己就起了解码 goroutine：它会读内层，于是卡在 entered 上。
	p := New(d)
	defer p.Close()

	select {
	case <-d.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("解码器压根没被读到")
	}

	// ⚠️ 这一段必须带看门狗。锁真的横跨读内层时，`Position()` 会**一直**等下去
	//（放解码器的信号在循环之后才发），直接量就会把整条用例挂到 go test 的
	// 十分钟超时——那不是「红」，那是「挂」，下一个人只会以为这机器慢。
	measure := func() time.Duration {
		done := make(chan time.Duration, 1)
		go func() {
			start := time.Now()
			for range 100 {
				_ = p.Position()
			}
			done <- time.Since(start)
		}()
		select {
		case el := <-done:
			return el
		case <-time.After(3 * time.Second):
			close(d.release) // 先放它出去，别把收尾一起挂住
			t.Fatal("源卡住的时候 Position() 一直拿不到锁——" +
				"界面每帧开头都要问一次位置，CDN 一卡整个窗口就跟着卡" +
				"（生产上的卡死判定是 20 秒）")
			return 0
		}
	}

	if el := measure(); el > 200*time.Millisecond {
		t.Errorf("源卡住的时候 100 次 Position() 花了 %v——"+
			"界面每帧开头都要问一次位置，CDN 一卡整个窗口就跟着卡"+
			"（生产上的卡死判定是 20 秒）", el)
	}

	close(d.release) // 放它出来，后面的收尾才走得完
}

// TestReadRejectsABufferTooSmallForAFrame 盯畸形缓冲是**报错**而不是 panic。
//
// MyGo 的约定是「buf 是整数个帧」，`stream.decode` 固定传
// `make([]float32, 2048*channels)`，所以这是**调用方的 bug**。但它不能变成
// panic：`buf[n:want]` 在一个零长切片上会当场越界，而那个 panic 发生在解码
// goroutine 上——整个进程跟着没。
//
// 也不能返回 (0, nil)：MyGo 那边读成「流到头了」（`mygo/audio/source.go` 的
// `if err != nil || n == 0`），一首好好的歌被当场截断。
func TestReadRejectsABufferTooSmallForAFrame(t *testing.T) {
	const rate = 44100
	src := gritty(tone(rate, 2, 440, 3))

	for _, size := range []int{0, 1} {
		v := newVarispeed(&memDec{samples: src, ch: 2, rate: rate})
		buf := make([]float32, size)
		n, err := v.Read(buf)
		if err == nil {
			t.Errorf("给了 %d 个元素的缓冲（两声道一帧要 2 个），却没报错：n=%d",
				size, n)
		}
		if n != 0 {
			t.Errorf("给了 %d 个元素的缓冲，却往里写了 %d 个样本", size, n)
		}
	}
}

// TestClampRateAndSnap 盯倍率的两端和档位。
//
// 设置文件是用户能手改的，`playbackRate: 1.13` 不该原样进到算法里；`0` 和
// `NaN` 更不该——倍率算错时宁可原速。
func TestClampRateAndSnap(t *testing.T) {
	for _, c := range []struct{ in, want float64 }{
		// 0、负数、NaN 落回原速而不是最慢那一档：倍率算错时宁可不动，
		// 也不能是「突然慢半速」。
		{-3, DefaultRate}, {0, DefaultRate}, {math.NaN(), DefaultRate},
		{0.1, MinRate}, {1, 1}, {2, 2}, {99, MaxRate},
	} {
		if got := ClampRate(c.in); got != c.want {
			t.Errorf("ClampRate(%v) = %v，要的是 %v", c.in, got, c.want)
		}
	}
	for _, c := range []struct{ in, want float64 }{
		{1.13, 1.25}, {0.6, 0.5}, {0.9, 1}, {2.4, 2},
		// 正中间（0.625）落在慢的那一档：宁可慢，不要快。
		{0.625, 0.5},
	} {
		if got := SnapRate(c.in); got != c.want {
			t.Errorf("SnapRate(%v) = %v，要的是 %v", c.in, got, c.want)
		}
	}
}

// TestRateStepsCoverTheDocumentedRange 盯档位表本身。
//
// 0.5~2.0 和那七档是照 Biu 的 `PlayRate` 抄的（`biu/src/common/constants/audio.tsx:3`），
// 界面上的浮层和它是对着写的。哪天这里动了，界面上那七行得跟着动。
func TestRateStepsCoverTheDocumentedRange(t *testing.T) {
	want := []float64{0.5, 0.75, 1, 1.25, 1.5, 1.75, 2}
	if len(RateSteps) != len(want) {
		t.Fatalf("档位有 %d 个，Biu 是 %d 个", len(RateSteps), len(want))
	}
	for i, r := range want {
		if RateSteps[i] != r {
			t.Errorf("第 %d 档是 %v，Biu 是 %v", i, RateSteps[i], r)
		}
	}
}

// TestPlayerRateIsIndependentOfMyGoSpeed 盯「倍速没走 MyGo 的 SetSpeed」。
//
// `mygo/audio` 的 SetSpeed 是重采样，音高必然跟着走。走那条路的话，1.5 倍速
// 的人声就是一个降了半个调的尖嗓——这一票存在的全部理由就是绕开它。
//
// 这条直接钉住播放器那一层：`Player.Rate()` 和 MyGo 播放器的 `Speed()` 是
// 两件事，前者是我们自己做的保调变速。
func TestPlayerRateIsIndependentOfMyGoSpeed(t *testing.T) {
	const rate = 44100
	src := tone(rate, 1, 440, 2)
	p := New(&memDec{samples: src, ch: 1, rate: rate})
	defer p.Close()

	p.SetRate(1.5)
	if got := p.Rate(); got != 1.5 {
		t.Errorf("Rate() = %v，要的是 1.5", got)
	}
	if got := p.play.Speed(); got != 1 {
		t.Errorf("MyGo 播放器自己的 speed 是 %v——这条路上去了，音高会跟着变", got)
	}
	// 越界的倍率夹回来。
	p.SetRate(99)
	if got := p.Rate(); got != MaxRate {
		t.Errorf("SetRate(99) 之后 Rate() = %v，要的是 %v", got, MaxRate)
	}
}

// TestPlayerSeekIsExactAtAnyRate 盯「变速中 seek 准确」的播放器那一半。
//
// 直接比 Duration 和 seek 之后的 Position：歌长恒等于源长，seek 到哪儿
// Position 就在哪儿，两个都与倍率无关。
//
// Position 是从解码器那一侧算的（变速层还没来得及处理刚发的 seek 时会读到
// 旧值），所以这里给一个短超时去等，而不是断言第一帧就准。
func TestPlayerSeekIsExactAtAnyRate(t *testing.T) {
	const rate = 44100
	src := tone(rate, 1, 440, 10)
	want := 10 * time.Second

	for _, r := range RateSteps {
		t.Run(rateLabel(r), func(t *testing.T) {
			p := New(&memDec{samples: src, ch: 1, rate: rate})
			defer p.Close()
			p.SetRate(r)

			if d := p.Duration() - want; d > time.Millisecond || d < -time.Millisecond {
				t.Fatalf("%.2f 倍速下歌长是 %v，源是 %v", r, p.Duration(), want)
			}
			// 目标都在歌长之内：拖到末尾之外会被播放器夹回末尾，那是另一件事。
			for _, at := range []time.Duration{3 * time.Second, 7 * time.Second, 0} {
				p.Seek(at)
				deadline := time.Now().Add(2 * time.Second)
				for time.Now().Before(deadline) {
					if d := p.Position() - at; d < 200*time.Millisecond && d > -200*time.Millisecond {
						break
					}
					time.Sleep(5 * time.Millisecond)
				}
				if d := p.Position() - at; d > 300*time.Millisecond || d < -300*time.Millisecond {
					t.Errorf("%.2f 倍速下 seek 到 %v，位置在 %v", r, at, p.Position())
				}
			}
		})
	}
}

// rateLabel 是测试里用的档位名，和界面上那个一样（`1x` / `1.25x`）。
func rateLabel(r float64) string {
	return strconv.FormatFloat(r, 'g', -1, 64) + "x"
}
