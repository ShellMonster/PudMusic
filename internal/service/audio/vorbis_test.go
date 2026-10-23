// Ogg Vorbis：`.ogg` 真的能解出声音。
//
// # 这一票修的是「界面说谎」
//
// 本地音乐的扫描白名单里有 `.ogg`（照抄 Biu 上游那一份），所以界面上那一行
// 写着「可放」。而**解码器从来没注册过**——`mygo/audio/vorbis` 一直没被 import，
// 于是点进去报的是「不支持这个音频格式」。
//
// 「界面说可放、实际报不支持」是最坏的一种错法：用户看到的是**界面骗了他**，
// 而不是「这个文件坏了」。而它之所以能活这么久，是因为原来那条断言只查了
// `Playable()`（那张表上写着可放），**一个样本都没解过**——判据一路绿，
// 实际放不出来。
//
// # 判据是音高，不是「没报错」
//
// Ogg Vorbis 是**有损**编码，逐样本比对没有判据（FLAC 那套素材是 FLAC 的
// 判据，Vorbis 没有对应的一份无损原件）。能判的是**音高**：素材是一段
// 440 Hz 的正弦，解出来的采样数一遍过零就该还是 440 Hz。
//
// 这比「解出来了、没报错」强得多：
//
//   - 解码器返回**静音**（一串 0）→ 过零数是 0 → 频率算出来是 0 Hz → 红。
//   - 解码器返回**噪声** → 过零数乱跳 → 频率对不上 → 红。
//   - 解码器把声道数或采样率读错 → 频率按错误的采样率折算 → 对不上 → 红。
//
// 只有真的解出了那声 440 Hz，这条才绿。
//
// # 素材的来路
//
// `testdata/tone.ogg` 来自 MyGo 仓库自带的那一份（MIT，
// `mygo/audio/vorbis/testdata/tone.ogg`），和 `testdata/tone.mp3` 同一个来路：
// 0.5 秒的 440 Hz 正弦，22050 Hz，双声道。放在这儿是因为 Ogg 这一条路
// **需要一份真的 Ogg 文件**——现造的（`localmusic` 的那个 fixture 只有头，
// 没有音频数据）解不出任何东西。

package audio

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// oggFixture 是那份 Ogg 素材的路径（相对这个包的 testdata）。
const oggFixture = "tone.ogg"

// oggFixtureRate / oggFixtureTone 是素材自己写着的两个数：采样率和音高。
//
// ⚠️ 这两个数**不是从素材里读出来的**，是素材的**规格**——写在这里就是为了
// 让「解出来的对不对」有一个不依赖被测代码的判据。素材本身可以拿
// `ffprobe` 复核（0.5 秒 / 22050 Hz / 2 声道 / vorbis）。
const (
	oggFixtureRate = 22050
	oggFixtureTone = 440.0
)

// TestOggVorbisReallyDecodes 钉住「`.ogg` 真的能解出声音」。
//
// 走的是**应用真正走的那条路**：`openDecoder`（`player.go`）——本地文件和
// B 站在线流认容器认的都是它。少了 `mygo/audio/vorbis` 那一行 blank import，
// 这里第一步就报「不支持这个音频格式」。
func TestOggVorbisReallyDecodes(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", oggFixture))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	d, name, err := openDecoder(f)
	if err != nil {
		t.Fatalf("解不了 .ogg：%v", err)
	}
	if name != "vorbis" {
		t.Errorf("认出来的容器是 %q，want vorbis", name)
	}
	if d.SampleRate() != oggFixtureRate {
		t.Errorf("采样率是 %d，want %d", d.SampleRate(), oggFixtureRate)
	}
	if d.Channels() != 2 {
		t.Errorf("声道数是 %d，want 2", d.Channels())
	}
	// 时长那一栏（本地音乐列表里的「时长」）靠的就是 Len()。
	if d.Len() <= 0 {
		t.Errorf("解码器报不出长度（Len() = %d）", d.Len())
	}

	samples := readAllSamples(t, d)
	if len(samples) == 0 {
		t.Fatal("一个样本都没解出来")
	}
	frames := len(samples) / 2
	// 素材是 0.5 秒；Vorbis 会在头尾各补一点，所以给一个宽松的下界。
	if want := int64(oggFixtureRate / 2); frames < int(want) {
		t.Errorf("解出 %d 帧，素材是 %d 帧（0.5 秒）", frames, want)
	}

	// 音高：数左声道的过零。440 Hz 的正弦每两个采样过一次零，
	// 于是频率 = 过零数 ÷ 2 ÷ 秒数。
	zc := 0
	for i := 2; i < len(samples); i += 2 {
		if (samples[i-2] < 0) != (samples[i] < 0) {
			zc++
		}
	}
	if zc == 0 {
		t.Fatal("解出来的全是同一个符号（静音？），连一次过零都没有")
	}
	secs := float64(frames) / oggFixtureRate
	got := float64(zc) / 2 / secs
	if math.Abs(got-oggFixtureTone) > 5 {
		t.Errorf("解出来的频率是 %v Hz，want %v Hz（素材是 %v Hz 的正弦）",
			got, oggFixtureTone, oggFixtureTone)
	}
}

// TestTheOggDurationIsProbedFromItsOwnContainer 钉住本地音乐列表里「时长」
// 那一栏对 `.ogg` 也有值。
//
// `ProbeDuration` 问的是解码器「你一共多少帧」，**一个样本都不解**——所以
// 它能快，但前提是容器认得出来。曾经 `.ogg` 在这里直接报「不支持这个
// 音频格式」，界面上那一栏画的是「-」。
func TestTheOggDurationIsProbedFromItsOwnContainer(t *testing.T) {
	got, err := ProbeDuration(filepath.Join("testdata", oggFixture))
	if err != nil {
		t.Fatalf("读不出 .ogg 的时长：%v", err)
	}
	if got <= 0 {
		t.Errorf("时长是 %v，want 一个正数", got)
	}
	// 素材是 0.5 秒。给一个宽松的区间：Vorbis 的头尾填充不算进去。
	if got < 400*time.Millisecond || got > 600*time.Millisecond {
		t.Errorf("时长是 %v，want 0.5 秒左右", got)
	}
}
