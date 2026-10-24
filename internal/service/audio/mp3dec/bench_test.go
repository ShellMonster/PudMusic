package mp3dec_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/audio/mp3dec"
)

// 解码速度决定「能不能边下边放」：一个追不上实时的解码器，歌开头会卡住。
//
// # 为什么这一族尤其要有人守着
//
// **MP3 是纯 Go 解的**（`tphakala/go-mp3`，MIT，vendor 在 `gomp3/` 下），
// 没有系统解码器兜底——`extaf`（macOS AudioToolbox）和 `mfadec`（Windows
// Media Foundation）走的是操作系统自己的解码器，那两个是硬件加速过的。
// 所以纯 Go 这一条路上，换库、改解码路径、或者 Go 版本变了导致它变慢，
// **不会有人知道**：症状是「点开歌卡一下」，不是任何一句报错。
//
// # 这一族和 flacdec 那一族一样，是「检查」不是「基准」
//
// 回归时它要**红**，所以判据写成断言（`minRatio`），不是 `t.Log` 出来的一个
// 没人看的数。基准（`BenchmarkDecode`）另有一份，只在 `-bench` 时跑。
//
// # 覆盖到哪儿，没覆盖到哪儿
//
// 仓库里现成的三份 MP3（`internal/service/audio/testdata/`）**全是单声道**，
// 所以这条判据只量**单声道**那条路：MPEG-1 Layer III（tone.mp3）和 MPEG-2.5
// Layer III（mpeg25.mp3）。立体声那条路（joint stereo / intensity stereo）
// 没有素材可量——要量它得新造一份素材，不在这一票的范围里。
//
// 第三份 `gapless.mp3` 也没用上：它存在是为了**按帧对齐**地钉 LAME gapless
// 的裁剪（见 `internal/service/audio/mp3_test.go`），而它的解码路径和
// tone.mp3 是同一条（MPEG-1、44.1 kHz），裁剪是在样本层面做的、不在解码循环
// 里——量速度量不到它。

// TestDecodeSpeed 钉住「MP3 解码快得跟得上播放」。
//
// 它把一份素材从头解一遍，报出「比实时的快多少倍」。
func TestDecodeSpeed(t *testing.T) {
	for _, c := range []struct {
		name     string
		minRatio float64
	}{
		// MPEG-1 Layer III，44.1 kHz 单声道，128 kbps——最常见的那一种。
		//
		// 判据 **40 倍**。开发机（Apple M4，darwin/arm64，Go 1.27.1，单核）
		// 上实测 **约 430 倍实时**。
		//
		// 这个数是拿**进程 CPU 时间**定的：CPU 时间不受旁边抢 CPU 的影响，
		// 连着十轮落在 415~449 之间；同一批测量里的墙钟是 202~349（那台
		// 机器当时 load 30~60，被抢过）。墙钟不可能比 CPU 时间更乐观——
		// 抢占只会让墙钟更难看——而墙钟的最好成绩（435）就落在 CPU 时间
		// 那个区间里。所以「430」是这台机器上解码器真实的速度，不是一台
		// 碰巧闲着的机器的运气。
		//
		// 为什么定在十分之一：播放器只要 **1 倍**实时就能放，40 倍是**灾难
		// 级**回归的线——解码慢了十倍以上，症状才是「点开歌卡住」。再往上
		// 定就只剩「数字好看」，而机器一忙就飘（实测那台机器上同一份素材
		// 连着八轮的倍率从 78 到 435，差五倍多）。
		//
		// （FLAC 那一族的判据是 20、实测 200~690 倍——量的是同一个东西。这一族
		// 实测 430 倍，所以判据是 40：**按各自实测值的十分之一定，不照抄别的
		// 族的数**。）
		{"tone.mp3", 40},
		// MPEG-2.5，11.025 kHz 单声道——这一族存在的理由（旧的解码器解不了
		// MPEG-2.5，见 mp3dec.go 顶上那一节）。
		//
		// 实测 **约 1670 倍实时**，比 tone.mp3 高三四倍。**不是**因为它解码得
		// 更快，而是因为 MPEG-2.5 每帧只有 576 个样本（MPEG-1 是 1152），
		// 采样率又只有 44100 的四分之一——一秒的音频的帧数少一半、每帧的活
		// 也少一半，于是「每秒能解出几秒音频」这个数自然大了四倍
		// （1670 / 427 ≈ 3.9，正好对上）。
		//
		// 判据同样定在十分之一：150 倍。
		{"mpeg25.mp3", 150},
	} {
		ratio, frames, rate, took := decodeOnce(t, c.name, speedRepeats())

		// ⚠️ 取**五轮里最快的那次**，不是一次就下结论。
		//
		// `go test ./...` 是并行的：本包在跑解码速度的时候，extaf 那边的
		// MP4 用例正在同一台机器上抢 CPU（它走系统解码器，很吃 CPU）。
		// 一次测量碰上这种抢占就红，而「解码器变慢了」和「旁边有人在抢 CPU」
		// 是两回事——分不出来，而分不出来的那一次会被当成回归去查半天。
		//
		// 五轮取最快不是把断言放松：解码器真的慢的话五轮都会慢。
		//
		// ⚠️ 这一族比 flacdec 那一族**更**需要它：现成的 MP3 素材只有
		// 0.5~1 秒，一轮解 40 遍也才几十毫秒，撞上一次抢占就掉一大截。
		// 实测那台机器（load 40~60 的窗口里）同一份素材八轮的倍率从 78 到
		// 435，差五倍多——一次测量根本分不出「慢了」和「被抢了」。
		//
		// ⚠️ `-race` 下**一轮都不多跑**。这一族在 `underRace` 时判据是空的
		// （下面那个 `&& !underRace`），取五轮最快是为了「够格去判」，没有判据
		// 的那几轮量出来的数谁也不看——而每轮是 40 次完整解码。
		if !underRace {
			for range 4 {
				if ratio >= c.minRatio {
					break // 已经够了，不用再跑
				}
				if r, _, _, tk := decodeOnce(t, c.name, speedRepeats()); r > ratio {
					ratio, took = r, tk
				}
			}
		}

		t.Logf("%s: %d repeats, %d frames of %d Hz in %v, %.0f times real time",
			c.name, speedRepeats(), frames, rate, took, ratio)
		if ratio < c.minRatio && !underRace {
			t.Errorf("%s decoded at %.0f times real time (五轮最快), want at least %.0f", c.name, ratio, c.minRatio)
		}
	}
}

// speedRepeats 是一轮里把素材解多少遍。
//
// # 为什么不是一遍
//
// 仓库里现成的 MP3 素材只有 0.5~1 秒（它们是用来**断言内容**的——音高对不对、
// gapless 裁对没有——不是用来量吞吐的），解一遍只要一毫秒出头。那么短的
// 计时钟上，调度抖动就能盖过解码速度本身。一轮解 40 遍，墙钟落在几十到几百
// 毫秒量级，量出来的倍率才稳。
//
// # 为什么 `-race` 下只解一遍
//
// `-race` 慢一到两个数量级，那一轮要好几秒，而那个数谁也不看（判据在
// `underRace` 时是空的）。
func speedRepeats() int {
	if underRace {
		return 1
	}
	return 40
}

// decodeOnce 把一份素材从头解一遍，返回「几倍实时」。
//
// 一轮解 repeats 遍（理由见 speedRepeats）。计时**不包含**打开文件和构造
// 解码器：那两件事是「打开一个歌」的一次性开销，不是解码速度（flacdec
// 那一族的 decodeOnce 也是这么分的）。
func decodeOnce(t *testing.T, name string, repeats int) (ratio float64, frames int64, rate int, took time.Duration) {
	t.Helper()
	path := filepath.Join("..", "testdata", name)
	for range repeats {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		d, err := mp3dec.Open(f)
		if err != nil {
			f.Close()
			t.Fatal(err)
		}
		ch := d.Channels()
		buf := make([]float32, 4096*ch)
		start := time.Now()
		for {
			n, err := d.Read(buf)
			frames += int64(n / ch)
			if err != nil {
				if err != io.EOF {
					f.Close()
					t.Fatalf("解码 %s：%v", name, err)
				}
				break
			}
		}
		took += time.Since(start)
		rate = d.SampleRate()
		f.Close()
	}
	if rate <= 0 {
		t.Fatalf("%s 解出来的采样率是 %d，算不出「几倍实时」", name, rate)
	}
	audio := time.Duration(frames) * time.Second / time.Duration(rate)
	return float64(audio) / float64(took), frames, rate, took
}

// BenchmarkDecode 把解码本身量成一个基准。基准不会在普通的 `go test` 里跑，
// 所以机器忙的时候不会把一次绿的跑法弄红；要那个数就 `-bench` 来问。
//
// 计时里**不含**打开文件和构造解码器：那是「打开一个歌」的一次性开销。
func BenchmarkDecode(b *testing.B) {
	for _, name := range []string{"tone.mp3", "mpeg25.mp3"} {
		b.Run(name, func(b *testing.B) {
			path := filepath.Join("..", "testdata", name)
			for b.Loop() {
				b.StopTimer()
				f, err := os.Open(path)
				if err != nil {
					b.Fatal(err)
				}
				d, err := mp3dec.Open(f)
				if err != nil {
					f.Close()
					b.Fatal(err)
				}
				ch := d.Channels()
				buf := make([]float32, 4096*ch)
				b.StartTimer()

				for {
					if _, err := d.Read(buf); err != nil {
						break
					}
				}

				b.StopTimer()
				f.Close()
				b.StartTimer() // 下一轮 b.Loop() 要有钟在走
			}
		})
	}
}
