package audio

// 「B 站五档音质 × 两个平台」的覆盖表。
//
// # 这张表是**跑出来的**，不是推出来的
//
// 每一格都是**真的解了一遍**的结果：解得出来就写「能解」并且把采样率、
// 声道数、帧数打出来；解不出来就把**原因**打出来（错的那一句，不是「失败」
// 两个字）。
//
// 为什么值得单独一张表：B 站的五档**不是五种编码**——三档有损都是 AAC-LC
// （只有码率不同），第四档是 E-AC-3，第五档是 FLAC。而「同一个解码器在两个
// 平台上能不能解」这件事，**平台之间差别很大**：macOS 有 AudioToolbox，
// Windows 有 Media Foundation，两边能解的东西**不一样**。
//
// # 平台那一列怎么来的
//
// 这一条测试在两个平台上都跑：
//
//   - **macOS**：走 `extaf`（AudioToolbox）。
//   - **Windows**：走 `mfadec`（Media Foundation），FLAC 那一档落回 `flacdec`。
//
// 所以同一份代码在两个平台上跑出来的表**就是两个平台的那张表**——不需要
// 两套实现，也不需要「Windows 的结果等 CI 跑完再补」。
//
// # 素材
//
// 三档有损是 `mfadec/testdata/` 里按 B 站形状造的 MP4（`mp4a.40.2`，44100 Hz
// 立体声，只有码率不同）；E-AC-3 那一档是**故意解不了**的那一份；FLAC 那一档
// 复用 `flacdec/testdata/stereo16.mp4`（MP4 里的 FLAC，正是 B 站无损流的形状，
// 见 issue #252）。

import (
	"bytes"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// qualityLevel 是覆盖表的一行。
type qualityLevel struct {
	// name 是 B 站界面上那一档的名字。
	name string
	// codec 是那一档的编码。
	codec string
	// path 是素材相对于本包目录的路径。
	path string
	// expect 是「这一档**应该**能解」还是「划掉」。
	expect string
	// why 是划掉那一档的原因（expect == "划掉" 时有内容）。
	why string
}

// coverageLevels 是 B 站的五档。
//
// ⚠️ **三档有损的编码是一样的**（都是 AAC-LC，`mp4a.40.2`），只有码率不同
// （`internal/service/bili/quality.go`：30216 是 64K、30232 是 128K、30280 是
// 192K）。所以它们在「能不能解」这件事上是同一格——但**素材还是各做了一份**：
// 码率不同就意味着码流长度不同，而「不同码率的码流都能解」这件事本身是
// 要验的（码率低的时候 ADTS 头里的帧长字段会出现 0，那是另一条路）。
var coverageLevels = []qualityLevel{
	{
		name: "64K", codec: "AAC-LC (mp4a.40.2)",
		path:   filepath.Join("mfadec", "testdata", "aaclc-64k.m4a"),
		expect: "能解",
	},
	{
		name: "128K", codec: "AAC-LC (mp4a.40.2)",
		path:   filepath.Join("mfadec", "testdata", "aaclc-stereo.m4a"),
		expect: "能解",
	},
	{
		name: "192K", codec: "AAC-LC (mp4a.40.2)",
		path:   filepath.Join("mfadec", "testdata", "aaclc-192k.m4a"),
		expect: "能解",
	},
	{
		name: "杜比全景声", codec: "E-AC-3 (ec-3)",
		path:   filepath.Join("mfadec", "testdata", "eac3-stereo.m4a"),
		expect: "划掉",
		why:    "field-of-use（授权限制），而且 Win11 24H2 移除了系统自带的 AC-3 解码器",
	},
	{
		name: "Hi-Res 无损", codec: "FLAC",
		path:   filepath.Join("flacdec", "testdata", "stereo16.mp4"),
		expect: "能解",
	},
}

// TestTheAACOnlineStreamActuallyPlays 盯「**在线**的有损流放得出来」。
//
// 这一条和上面那张表不是一回事：表里量的是**本地文件**，这一条量的是**在线
// 字节**——B 站发下来的音频是分片 MP4（`.m4s`），走的是 `StreamSource` 那条
// 路，而它和本地文件不是同一条路（边下边读、随时可能换流）。
//
// ⚠️ **这一条在 Windows 上要等整个文件下完才开口**：`go-m4a` 拆分片 MP4
// 要先把每一个 `moof` 走一遍，而 `moof` 在文件的末尾——所以开口之前整个文件
// 已经下完了。macOS 上没有这个问题（AudioToolbox 按需读）。
//
// 所以这一条断的是「**放得出来、放出来的是真声音**」，不是「边下边播」——
// 后者在 Windows 上要另做一件事（见报告）。
func TestTheAACOnlineStreamActuallyPlays(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("mfadec", "testdata", "aaclc-stereo.m4a"))
	if err != nil {
		t.Fatalf("读不到素材：%v", err)
	}
	d, name, err := openDecoder(newOnlineSource(t, newByteCDN(t, data)))
	if err != nil {
		t.Fatalf("在线的有损流认不出容器：%v", err)
	}
	defer func() {
		if c, ok := d.(io.Closer); ok {
			c.Close()
		}
	}()
	t.Logf("容器名 = %q，%d Hz / %d 声道", name, d.SampleRate(), d.Channels())

	got := decodeAll(t, d)
	if len(got) < d.SampleRate() {
		t.Fatalf("只解出 %d 个采样（采样率 %d），一首 4 秒的歌不该这么短", len(got), d.SampleRate())
	}
	if silent(got) {
		t.Error("解出来全是 0：在线流「播放中」但一个声音也没有")
	}
	if lo, hi := span(got); hi-lo < 0.05 {
		t.Errorf("波形上下只差 %v，接近静音（%.4f ~ %.4f）", hi-lo, lo, hi)
	}
	t.Logf("解出来 %d 个采样（%.3f 秒）", len(got), float64(len(got)/d.Channels())/float64(d.SampleRate()))
}

// TestTheFirstFrameComesOutFast 盯「**开口到第一个采样**不超过 200 毫秒」。
//
// 口径和 `quality_test.go` 的 `firstAudibleBudget` 一样：**只量解码那一侧**，
// 不含下载等待——真网上 B 站的音频 60 秒才 1.2 MB，光下载就够这个数了。
//
// 这一条量的是**产品这一层**：从 `openDecoder` 到第一个 PCM 采样出来。
// 拆容器（go-m4a）、认容器、开解码器、解第一帧，全在里面。
//
// ⚠️ **机器忙的时候这个数会飘**：机器负载 100+ 的时候，一次文件解析就能吃掉
// 几十毫秒。所以这里取五轮里**最快**的那次（和 `quality_test.go` 的
// `TestAAC320FromTheStreamIsAudibleFast`、以及 `flacdec` 的 `TestDecodeSpeed`
// 同一个口径）：测的是**解码器的能力**，不是调度。
//
// 而最慢的那一次**照样打出来**——它通常是「第一次」的冷启动（AudioToolbox
// 要现加载转换器），那一段是用户真的会等到的，值得看见。
func TestTheFirstFrameComesOutFast(t *testing.T) {
	const budget = 200 * time.Millisecond

	data, err := os.ReadFile(aacFixturePath)
	if err != nil {
		t.Fatalf("读不到素材 %s：%v", aacFixturePath, err)
	}

	best, worst := time.Duration(math.MaxInt64), time.Duration(0)
	for i := range 5 {
		start := time.Now()
		d, _, err := openDecoder(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("第 %d 轮：认不出容器：%v", i+1, err)
		}
		buf := make([]float32, 4096*d.Channels())
		n, err := d.Read(buf)
		got := time.Since(start)
		if c, ok := d.(io.Closer); ok {
			c.Close()
		}
		if err != nil && err != io.EOF {
			t.Fatalf("第 %d 轮：读第一块：%v", i+1, err)
		}
		if n == 0 {
			t.Fatalf("第 %d 轮：一个采样都没解出来", i+1)
		}
		t.Logf("第 %d 轮：开口到第一个采样 %s（%d 个采样）", i+1, got.Round(time.Microsecond), n)
		if got < best {
			best = got
		}
		if got > worst {
			worst = got
		}
	}
	t.Logf("五轮里最快的一次：%s，最慢的一次：%s，预算 %s",
		best.Round(time.Microsecond), worst.Round(time.Microsecond), budget)
	if best > budget {
		t.Errorf("开口到第一个采样用了 %s，超过 %s", best.Round(time.Microsecond), budget)
	}
}

// TestTheBilibiliQualityCoverage 产出覆盖表。
//
// 断的是**「应该能解的那几档真的能解」**——解不出来就红，并且把错打出来。
// 「划掉」那一档解不出来**不算失败**：那正是它被划掉的原因，要做的是把
// 原因记下来。
func TestTheBilibiliQualityCoverage(t *testing.T) {
	platform := "macOS"
	if runtime.GOOS == "windows" {
		platform = "Windows"
	}
	t.Logf("平台：%s", platform)

	for _, level := range coverageLevels {
		t.Run(level.name, func(t *testing.T) {
			data, err := os.ReadFile(level.path)
			if err != nil {
				t.Fatalf("读不到素材 %s：%v", level.path, err)
			}
			d, name, err := openDecoder(bytes.NewReader(data))
			if err != nil {
				if level.expect == "划掉" {
					t.Logf("❌ %s（%s）：%v", level.name, level.codec, err)
					t.Logf("   划掉的理由：%s", level.why)
					return
				}
				t.Fatalf("❌ %s（%s）：%v——这一档应该能解", level.name, level.codec, err)
			}
			defer func() {
				if c, ok := d.(io.Closer); ok {
					c.Close()
				}
			}()

			// 真的解一点出来，确认不是「打开了但解不出东西」。
			buf := make([]float32, 4096*d.Channels())
			n, err := d.Read(buf)
			if err != nil && err != io.EOF {
				t.Fatalf("❌ %s（%s）：读第一块就失败了：%v", level.name, level.codec, err)
			}
			if n == 0 {
				t.Fatalf("❌ %s（%s）：打开了，但一个采样都读不出来", level.name, level.codec)
			}
			peak := 0.0
			for _, v := range buf[:n] {
				a := float64(v)
				if a < 0 {
					a = -a
				}
				if a > peak {
					peak = a
				}
			}
			if peak < 0.01 {
				t.Fatalf("❌ %s（%s）：解出来几乎是静音（峰值 %v）", level.name, level.codec, peak)
			}
			t.Logf("✅ %s（%s）：能解——容器 %s，%d Hz / %d 声道，首块 %d 个采样，峰值 %.6f",
				level.name, level.codec, name, d.SampleRate(), d.Channels(), n, peak)
		})
	}
}
