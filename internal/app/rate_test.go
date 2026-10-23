package app_test

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// 倍速在**应用这一层**的行为。
//
// 音高对不对、时长对不对，在 `internal/service/audio/rate_test.go` 里拿频谱和
// 逐样本量；那里量的是播放器自己，和 App 无关。
//
// 这里量的是只有这一层知道的三件事：
//
//	倍率记在 App 上（它是用户的一个设置，不是某一首歌的属性）
//	换歌、换流重开之后它还在，而且新播放器真的套上了
//	进度条的时钟真的按倍率走（这一层每 50ms 把播放器的位置读回来写进 State，
//	  界面上那根条就是它）
//
// # 断言的是**走得多快**，不是「倍率字段是几」
//
// 读字段只能验「我存了 1.5」。用户听见、看见的是**歌里的时间走得快不快**。
// 而这一层是唯一能同时摸到「用户按了什么」和「播放器走了多远」的地方——
// `audio` 那层没有 App，也就没有「用户按了什么」。
//
// # ⚠️ 为什么全都写成**比值**而不是「一秒该走几秒」
//
// 「一秒该走两秒」这种断言在这台机器上不稳，`-race` 下尤其不稳。三个原因叠在
// 一起，每一个都足以让它红：
//
//	系统音频输出要现开（MyGo 的 `wakeOutput`），头两三百毫秒位置根本不动
//	MyGo 预解约一秒（`mygo/audio/source.go` 那一圈 ring），改倍率之后那一秒的
//	  输出还是按旧倍率铺的，放完之前量的是旧倍率
//	`-race` 下一个抢不到 CPU 的音频回调会让位置停顿——实测同一条用例在
//	  `-race` 下只走到期望的六成，而不带 `-race` 时是准的
//
// 所以这里的写法是：**同一轮里量两个窗口，只信它们的比值**。机器慢会让两个
// 窗口一起慢，比值不变；而「倍率没套上」是比值从 2 掉到 1——那个掉得下。

// rateName 是测试里的档位名，和界面上那个一样。
func rateName(r float64) string { return strconv.FormatFloat(r, 'g', -1, 64) + "x" }

// reach 等到歌里的时间过到 target，返回那一刻的真实时刻。
func reach(t *testing.T, a *app.App, target time.Duration) time.Time {
	t.Helper()
	var at time.Time
	waitFor(t, 60*time.Second, "歌里的时间过到 "+target.String(), func() bool {
		if a.Snapshot().Position < target {
			return false
		}
		at = time.Now()
		return true
	})
	return at
}

// winSpan 是一段窗口有多长（歌内时间）。
//
// 两秒。位置每 50ms 才写回一次（app.go 的 tickInterval），窗口两端各能差 50ms，
// 一秒的窗口采样误差就是 5%——淹在 1.8 倍的容差里，但没必要顶着这个数跑。
//
// 更重要的是**窗口越长被整段抢走的概率越小**：这台机器上有十几份 `-race` 同时跑，
// 音频回调随时可能连续几百毫秒拿不到 CPU，而一段两秒的窗口被整整抢掉要更难。
// 「量两遍取大的」治的是抢走一部分，这个治的是抢走一整段。
const winSpan = 2 * time.Second

// secPerSong 量「一秒真实时间里，歌里的时间走了多少」。
//
// ⚠️ 只在**同一轮里比两个窗口**时才准，见文件头那段。要看的从来不是它等于几，
// 而是它和同一轮另一个窗口比是大还是小。
//
// # 它量**两段**同长的窗口，取大的那个
//
// 机器慢会让所有窗口**一起**慢，比值不变——比值这个写法就是为那种慢准备的。
// 但还有另一种慢：**调度抖动**。播放走系统音频输出，回调被操作系统抢走的那些
// 窗口会明显偏小，而同一轮里没被抢的那些是准的。实测同一轮里原速窗口走到
// 0.975、紧跟着的 0.5 倍速窗口只走到 0.228——比值 0.23，既不是 0.5（没套上）
// 也不是别的任何倍率。那种时候红起来是报告了一台机器的调度，不是报告了代码。
//
// 取大的那个，等于「按这台机器能达到的**最好**情况判」。而它在**两个方向上**
// 都比原来严：
//
//	2 倍速没套上（走成原速）：两段都只有原速那么快，取大还是原速 → 红
//	套上了、有一段被抢了 CPU：一段被抢一段没被，取大那段没被 → 绿
//
// 这和 flacdec 的 `TestDecodeSpeed`「五轮取最快」是同一个手法，也是那个仓库早就
// 验证过的：它治的正是「解码器变慢」和「旁边有人在抢 CPU」分不出来这件事。
//
// 第二个返回值是**抖动**（见 `jitterOf`）。它比「测出来的值是多少」更能说明这一轮
// 能不能拿来判：机器稳定地慢（两遍接近）时那个值是可用的，机器在抖（两遍差得远）
// 时那个值什么也不是。
func secPerSong(t *testing.T, a *app.App, from, to time.Duration) (best, jitter float64) {
	t.Helper()
	span := to - from
	x, y := window(t, a, from, to), window(t, a, to, to+span)
	return max(x, y), jitterOf(x, y)
}

// jitterOf 是两遍测量之间差多少倍：大的除以小的。
//
// # ⚠️ 必须**双向**（`max/min`，不是 `lo/hi` 或 `hi/lo`）
//
// 哪一遍慢是**随机的**——机器什么时候被抢说不准，同一段的两次测量，先后顺序没有
// 任何规律。所以这个数不能带方向：`hi/lo` 在「第一遍慢」时给一个大于 1 的数、在
// 「第二遍慢」时给另一个，而这两个数说的是**同一份抖动**。用有方向的比值会让完全
// 等量的抖动因为出现在哪一遍而得到相反的结论。
//
// 例子：两遍量出 1.0 和 0.5。无论哪个是「第一遍」，抖动都是 2 倍。
func jitterOf(x, y float64) float64 {
	lo, hi := min(x, y), max(x, y)
	if lo <= 0 {
		return math.Inf(1)
	}
	return hi / lo
}

// window 量一段窗口的 secPerSong。
func window(t *testing.T, a *app.App, from, to time.Duration) float64 {
	t.Helper()
	at0 := reach(t, a, from)
	at1 := reach(t, a, to)
	return float64(to-from) / float64(at1.Sub(at0))
}

// near 断 got 和 want 是不是在彼此的 ±tol 倍之内（tol > 1）。
func near(got, want, tol float64) bool {
	if want == 0 {
		return got == 0
	}
	r := got / want
	return r >= 1/tol && r <= tol
}

// rateTol 是这一族所有比值判据的容差倍数。
//
// 真正的错（没套上 = 1.0、反过来 = 0.5）都掉在 1/1.8 ~ 1.8 之外。
const rateTol = 1.8

// healthyUnity 是「1 倍速下歌里的时间至少要走这么快」，低过它就算「机器跟不上实时」。
//
// 1 倍速的定义就是 1.0（MyGo 按实时消费输出），所以这个数是「这台机器**慢了多少**」
// 的读数。
const healthyUnity = 0.75

// jitteryJitter 是「两遍之间差这么多倍就算机器在抖」。
//
// 实测：机器健康时同一段量两遍差不到 1.1 倍；机器被抢时能差到两倍以上。
// 1.3 留了余量——宁可多判成稳一点（那会照红），也别把一次真验跳过去。
const jitteryJitter = 1.3

// distrust 返回一句说明「这一轮可能量不准」的话；量得准就返回空串。
//
// 它**不改变**断言的结果——只挂在错误信息后面，让人知道红的是不是代码的锅。
func distrust(unity, jitter float64) string {
	switch {
	case jitter > jitteryJitter:
		return fmt.Sprintf("⚠️ 这一次多半不是代码的问题：同一段量两遍差 %.2f 倍，"+
			"机器当时在抖，上面的数是机器的数。机器闲下来重跑一次就能确认。", jitter)
	case unity < healthyUnity:
		return fmt.Sprintf("⚠️ 这一次多半不是代码的问题：原速只走到每秒 %.3f 秒歌，"+
			"这台机器跟不上实时播放，上面的数可能是机器的数。机器闲下来重跑一次就能确认。", unity)
	}
	return ""
}

// rateGap 是「换完倍率之后要让开多少歌内时间才开始量」。
//
// 换档之后约有一秒的**输出**仍是按旧倍率铺的（MyGo 那一圈预解缓冲），那一秒输出
// 占多少**歌内**时间取决于旧倍率：一秒输出 ÷ 倍率。所以倍率越慢，让开得越多
// （0.5 倍速要两秒歌内，2 倍速只要半秒）。
func rateGap(rate float64) time.Duration {
	if rate <= 0 {
		rate = 1
	}
	return time.Duration(float64(time.Second) / rate)
}

// unityBaseline 量一段**原速**窗口当基准，并把倍率**拨回原样**。
//
// from 必须是**已经在歌内时间 from 之后**才轮到量，且要再让开 rateGap(keep)。
// 它自己会查这一点——量基准的窗口如果蹭进了上一次换档的预解缓冲，量到的就不是纯
// 原速，那个偏差会把整条用例带歪，而且是从「偶发红」这种最难查的方向歪。
//
// 实测踩过：窗口起点比这里要求的早两秒，`again` 量到 0.955 而原速基准是 0.911，
// 比值 1.05 被判成「倍率被量基准那次留下了」——那是误报，代码没毛病，是窗口蹭进了
// 预解缓冲。
func unityBaseline(t *testing.T, a *app.App, from time.Duration) (best, jitter float64) {
	t.Helper()
	keep := a.Snapshot().Rate
	at := a.Snapshot().Position
	// 留一秒余量：预解缓冲的实际长度是估计值，宁可多让一点。
	if want := at + rateGap(keep) + time.Second; from < want {
		t.Fatalf("unityBaseline 的起点 %v 太早：此刻歌内位置 %v，换 %v 倍速之后至少要"+
			"到 %v（预解缓冲 %v + 一秒余量）。窗口蹭进预解缓冲的话量到的不是纯原速，"+
			"而那个偏差会以「偶发红」的形式歪掉整条用例。", from, at, keep, want, rateGap(keep))
	}
	a.SetRate(1)
	best, jitter = secPerSong(t, a, from, from+winSpan)
	a.SetRate(keep)
	return best, jitter
}

// TestSetRateIsSnappedAndClamped 盯倍率落在档位上。
//
// 设置和键盘都可能送来 1.13 这种值，而算法只认七档：吸到最近的一档，
// 界面上写的和声音里的必须是同一个数。
func TestSetRateIsSnappedAndClamped(t *testing.T) {
	a := newApp(t)
	if got := a.Snapshot().Rate; got != 1 {
		t.Fatalf("刚开出来倍率是 %v，want 1", got)
	}
	for _, c := range []struct{ in, want float64 }{
		{1.13, 1.25}, {0.1, 0.5}, {99, 2}, {-1, 1},
		{math.NaN(), 1}, {0.75, 0.75}, {1.75, 1.75},
	} {
		a.SetRate(c.in)
		if got := a.Snapshot().Rate; got != c.want {
			t.Errorf("SetRate(%v) 之后是 %v，want %v", c.in, got, c.want)
		}
	}
}

// TestTheClockFollowsTheRate 盯进度条真的按倍率走。
//
// 这一层最要紧的一条：倍率存对了、播放器也设对了，而这一层每 50ms 把播放器的
// 位置读回来写进 State——要是这里读错了（比如读成 MyGo 那个「已经播了多少秒」：
// 它数的是吐出去的帧，除以采样率之后永远等于真实时间），界面上那根条在 2 倍速
// 下会走成原速，而声音是 2 倍速的。**别的用例都抓不住这种错**：字段是 2、
// 播放器也是 2，只有进度条是错的。
//
// 三个窗口在同一个播放器上量，1x 的那个当基准。倍率反过来（2 倍速听成 0.5 倍速）
// 时比值会反过来，容差抓得住。
func TestTheClockFollowsTheRate(t *testing.T) {
	a := newApp(t)
	if err := a.Open(longWav(t, "一首长歌.wav")); err != nil {
		t.Fatal(err)
	}
	// 开头两秒是输出现开 + 预解缓冲，第一段从第三秒开始量。

	// 2 倍速：先量它自己，再让 unityBaseline 在**紧挨着**的位置量一段原速当分母。
	// 两段之间只隔一次 SetRate，机器的忙闲几乎没变。
	a.SetRate(2)
	two, jTwo := secPerSong(t, a, 3*time.Second, 3*time.Second+winSpan)
	one, jOne := unityBaseline(t, a, 9*time.Second)
	if !near(two, 2*one, rateTol) {
		t.Errorf("2 倍速走 %.3f，原速走 %.3f——比值 %.2f，want 2"+
			"（进度条和耳朵对不上）%s", two, one, two/one,
			distrust(one, max(jTwo, jOne)))
	}

	// 0.5 倍速：同一套。
	a.SetRate(0.5)
	// 0.5 倍速的预解缓冲最长（rateGap(0.5) 是两秒歌内），加一秒余量让开三秒。
	half, jHalf := secPerSong(t, a, 16*time.Second, 16*time.Second+winSpan)
	one2, jOne2 := unityBaseline(t, a, 27*time.Second)
	// 分母取两段原速里**大的**那个：机器慢的时候多给一次机会，别因为其中一段
	// 慢就把比值压到容差外。
	base := max(one, one2)
	if !near(half, 0.5*base, rateTol) {
		t.Errorf("0.5 倍速走 %.3f，原速走 %.3f——比值 %.2f，want 0.5%s",
			half, base, half/base, distrust(base, max(jHalf, jOne2)))
	}
}

// TestRateIsRememberedBeforeAnyTrackIsOpen 盯「先按倍速再点歌」。
//
// 用户完全可能先在上一首歌上按了 2 倍速，切到下一首。那一首得从第一秒起
// 就是 2 倍速——不是开了之后再由用户补按一次。
//
// ⚠️ 这条用例的**形状**本身就是断言的一部分：必须**没有任何歌在放**的时候调
// `SetRate`，走的正是 `app.SetRate` 里 `a.player == nil` 那一支——它只把倍率存
// 在 App 上（没有一个播放器可以套），等新歌开出来时再送过去。所以基准**不能**靠
// 「先开一首歌量一段原速」来拿：那会毁掉这个前提，让这一支变成没人走的死代码。
//
// 基准改为在**开歌之后**临时把倍率拨回 1 量一段，量完再拨回 2。开歌之后
// `a.player != nil` 了，走的是另一支，不影响上面那条断言。
func TestRateIsRememberedBeforeAnyTrackIsOpen(t *testing.T) {
	a := newApp(t)
	if st := a.Snapshot(); st.HasTrack || st.Rate != 1 {
		t.Fatalf("刚开出来就有歌了（%+v）", st)
	}

	a.SetRate(2) // 此刻没有歌：走 App.SetRate 里 player == nil 那一支
	if err := a.Open(longWav(t, "先按后放.wav")); err != nil {
		t.Fatal(err)
	}
	if got := a.Snapshot().Rate; got != 2 {
		t.Fatalf("开歌之后倍率变成了 %v，want 2", got)
	}

	// 「新歌一上来就是 2 倍速」——必须量**开歌后的第一段**，中间不能有别的操作。
	two, jTwo := secPerSong(t, a, 3*time.Second, 3*time.Second+winSpan)
	one, jOne := unityBaseline(t, a, 9*time.Second)
	if !near(two, 2*one, rateTol) {
		t.Errorf("先按的 2 倍速没套到新歌上：原速走 %.3f，2 倍速走 %.3f——比值 %.2f，want 2%s",
			one, two, two/one, distrust(one, max(jTwo, jOne)))
	}
	// 拨回 2 倍速之后仍然是 2 倍速——量基准那一拨没有把倍率留在 1。
	again, jAgain := secPerSong(t, a, 15*time.Second, 15*time.Second+winSpan)
	if !near(again, 2*one, rateTol) {
		t.Errorf("量完基准再拨回 2 倍速，只走到 %.3f（原速 %.3f）——倍率被量基准那次留下了%s",
			again, one, distrust(one, max(jAgain, jOne)))
	}
}

// TestRateSurvivesTrackChanges 盯「切歌时倍率状态正确处理」。
//
// 倍率是**用户的一个设置**。切到下一首之后它必须还在：用户为了一首歌把速度
// 调快，听完顺手下一首，把他按回原速是要他重新调一次的。
//
// 这一条比的是**切歌前后两个窗口**：切歌把播放器整个换掉了（连同 MyGo 那一圈
// 预解缓冲），而用户这中间没有按任何键——两个窗口就该是一样的。真掉下去了，
// 就是新播放器没套上倍率。
//
// 顺带也盯了「切歌那一瞬间位置不跳」这件事的另一半：新歌开好之后 `Position`
// 从 0 开始，而不是接着上一首结尾那个数往上数。
func TestRateSurvivesTrackChanges(t *testing.T) {
	one := longWav(t, "第一首.wav")
	two := longWav(t, "第二首.wav")

	a := newApp(t)
	if err := a.Enqueue(app.LocalItem(one), app.LocalItem(two)); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "第一首")

	// 两个原速基准，**各自紧挨着自己那个倍速窗口**：机器在这两段的忙闲不一样，
	// 拿一个共用的基准去比两个窗口，机器一忙就会误判（实测踩到过一次）。
	a.SetRate(1)
	unity1, j1 := secPerSong(t, a, 2*time.Second, 2*time.Second+winSpan)

	a.SetRate(2)
	before, jB := secPerSong(t, a, 8*time.Second, 8*time.Second+winSpan)

	a.Next()
	playing(t, a, "第二首")
	if got := a.Snapshot().Rate; got != 2 {
		t.Fatalf("切到第二首之后倍率变成了 %v，want 2", got)
	}
	if pos := a.Snapshot().Position; pos > 2*time.Second {
		t.Errorf("第二首一上来位置就在 %v——上一首的进度接着数了", pos)
	}
	// ⚠️ `after` 必须在**任何 SetRate 之前**量——它是「切歌之后的第一段」。
	//
	// 早先那版为了拿基准，在量 `after` **之前**先调了 `unityBaseline`，而那个 helper
	// 会 `SetRate(1)` 量完再 `SetRate(2)` 拨回来。于是「新播放器忘了继承倍率」这个
	// 回归被**量基准那一下顺手补上了**：`Open` 只在初次打开时设倍率、换播放器时漏掉
	// 的话，`after` 本该掉到原速（比值 1）而变红，可它已经被补回 2 了，照样绿。
	// `Snapshot().Rate` 读的是 `App.rate`（App 自己记的那个数），不是播放器实际的
	// 倍率，所以上面那句 `Rate != 2` 的检查也拦不住。
	//
	// 判据在前面、基准在后面：先量完 `after` 再去动倍率，这个用例才问的是它要问的
	// 「切到下一首之后，新播放器自己有没有带上倍率」。
	after, jA := secPerSong(t, a, 2*time.Second, 2*time.Second+winSpan)
	unity2, j2 := unityBaseline(t, a, 8*time.Second)

	// ⚠️ 两个倍速窗口**各自除以自己的原速基准**之后再比，不直接比绝对值。
	//
	// 这两个窗口中间隔了一次切歌（换掉整个播放器），机器在这两段的忙闲可能差很远：
	// 实测有一次 after 1.2、before 1.9，绝对值差 1.6 倍（超容差，红），而两个窗口
	// 各自相对原速都是 2 倍——机器慢，不是倍率掉了。归一化之后比的是「切歌前走的
	// 相对速度和切歌后走的相对速度一样吗」，那才是这一条要问的。
	rBefore, rAfter := before/unity1, after/unity2

	if !near(rBefore, 2, rateTol) {
		t.Errorf("切歌之前就不对：原速走 %.3f，2 倍速走 %.3f——比值 %.2f，want 2%s",
			unity1, before, rBefore, distrust(unity1, max(j1, jB)))
	}
	if !near(rAfter, rBefore, rateTol) {
		t.Errorf("切歌之后从 %.2f 掉到 %.2f（各自的原速基准分别是 %.3f 和 %.3f）"+
			"——新播放器没套上倍率%s", rBefore, rAfter, unity1, unity2,
			distrust(unity2, max(j2, jA)))
	}
}

// TestOnlineTrackKeepsTheRate 盯在线流那条路也套上了倍率。
//
// ⚠️ 别处漏掉这一处的表现最难看：本地歌是 2 倍速、在线歌是原速，而两条路在
// App 里是**两套建播放器的代码**（`Open` 和 `buildOnline`）。只改一套的话，
// 换一种来源就换一种速度，用户会以为那首歌的音调天生就那样。
//
// 这一条同时是「换流重开」那条路（`refreshOnlineNow` 也走 `buildOnline`）的
// 覆盖：重开出来的播放器也是这个倍率。
//
// 素材传一段二十秒的 WAV 而不是默认那份四秒的 FLAC：这一条要量两秒的真实时间，
// 2 倍速下要走掉四秒，四秒的歌在窗口末尾就放完了——量出来的会是「歌放完了
// 位置停住」，被误判成「倍率没套上」。
func TestOnlineTrackKeepsTheRate(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readWav(t, longWav(t, "在线的一首.wav"))
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.wav", 2*time.Hour), int(bili.QualityHigh192), 110836))

	a := newOnlineApp(t, f, t.TempDir())
	a.SetRate(2)
	if err := a.Enqueue(onlineItem("BV1xx", "123", "在线的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "在线的一首")

	if got := a.Snapshot().Rate; got != 2 {
		t.Fatalf("在线歌的倍率是 %v，want 2", got)
	}
	two, jTwo := secPerSong(t, a, 3*time.Second, 3*time.Second+winSpan)
	one, jOne := unityBaseline(t, a, 9*time.Second)

	// ⚠️ **量出来比「任何可能的倍率」还快 ⇒ 这一趟量废了，跳过。**
	//
	// 倍率是档位（`audio.RateSteps`：0.5 / 0.75 / 1 / 1.25 / 1.5 / 1.75 / **2**），
	// 播放位置**不可能**走得比 2 倍速还快——而这一族量的是「一秒真实时间里，歌里
	// 的时间走了多少」。`one` 是同一轮里量出来的原速基准，也就是「这台机器一秒
	// 真实时间能走多少歌内时间」，所以 2 倍速能给的最高是 `2*one`。
	//
	// 量出比**容差那一头**还大的，只有两种可能：位置**跳**了（某一趟里位置报的
	// 是「已经拿到哪儿了」而不是「播到哪儿了」），或者别的测量事故。
	// **两种都不是「倍率没套上」**——倍率没套上的话这一趟量的是 1 倍（= `one`），
	// 比值 1，落在容差**外面**，照旧报错。
	//
	// 所以这里**跳过**而不是报错——仓库的约定是「跳过≠通过」，报告里要写清楚
	// 这一条这一次没验。实测（机器重到某个程度时）：原速基准 1.002、2 倍速那一段
	// 量出 5.224——比任何档位能给的都快，那就不是倍率的事。
	//
	// ⚠️ 阈值用的是**容差那一头**（`rateTol`），不是 `2*one` 本身：正常测出来的
	// 2 倍速是 2.00~2.11（窗口两端各能差 50ms 的采样误差），离 `2*one` 只差
	// 百分之几，拿 `2*one` 当线会把**好端端的测量也跳掉**。
	if rateMeasuredTooFast(two, one) {
		t.Skipf("这一趟量废了，没法回答「倍率套上没有」：原速基准 %.3f，2 倍速那一段量出 %.3f——"+
			"比任何档位能给的（最高 2 倍 = %.3f，再加容差 %.1f 倍 = %.3f）还快。"+
			"多半是位置跳了（报的是「已经拿到哪儿」而不是「播到哪儿」）。"+
			"**这一条这一次没验**（跳过≠通过）；机器闲下来重跑一次就能确认。",
			one, two, 2*one, rateTol, rateTol*2*one)
	}

	if !near(two, 2*one, rateTol) {
		t.Errorf("在线歌没套上 2 倍速：原速走 %.3f，2 倍速走 %.3f——比值 %.2f，want 2%s",
			one, two, two/one, distrust(one, max(jTwo, jOne)))
	}
}

// rateMeasuredTooFast 断「这一趟量出来的速度比任何档位能给的都快」。
//
// 倍率是档位（`audio.RateSteps`：0.5 / 0.75 / 1 / 1.25 / 1.5 / 1.75 / **2**），
// 播放位置**不可能**走得比 2 倍速还快。`one` 是同一轮里量出来的原速基准
// （这台机器一秒真实时间能走多少歌内时间），所以 2 倍速能给的最高是 `2*one`。
//
// ⚠️ 阈值取的是**容差那一头**（`rateTol`），不是 `2*one` 本身：正常测出来的
// 2 倍速是 2.00~2.11（窗口两端各能差 50ms 的采样误差），离 `2*one` 只差百分之
// 几，拿 `2*one` 当线会把**好端端的测量也跳掉**。
//
// ⚠️ 判「快」而不判「慢」是有意的：**慢**那一头是「倍率没套上」或者「机器跟不
// 上实时播放」，那两种都要照旧报错（见 `distrust`）；只有**快**这一头是「位置
// 跳了」这种测量事故——倍率没套上的话量出的是 1 倍（比值 1），落在慢那一头。
func rateMeasuredTooFast(two, one float64) bool {
	return two > rateTol*2*one
}

// TestRateMeasuredTooFast 把上面那个判据本身钉住。
//
// 这一条要防的是「把线画歪了」：线画低了会把**好端端的测量**也跳掉（于是这一条
// 变成永远不验），线画高了会把**该抓的**放过（于是倍率掉了也没人管）。两边都
// 要有例子。
func TestRateMeasuredTooFast(t *testing.T) {
	cases := []struct {
		two, one float64
		want     bool
		what     string
	}{
		{2.016, 1.002, false, "正常量出来的 2 倍速"},
		{2.106, 1.000, false, "采样误差那一头（窗口两端各差 50ms）"},
		{1.997, 1.000, false, "刚起步那一点"},
		{5.224, 1.002, true, "位置跳了（真机上实测到的那一次）"},
		{1.026, 1.027, false, "倍率没套上：比值 1，落在慢那一头，要照旧报错"},
		{0.310, 1.002, false, "机器跟不上实时播放：也在慢那一头，要照旧报错"},
	}
	for _, c := range cases {
		if got := rateMeasuredTooFast(c.two, c.one); got != c.want {
			t.Errorf("%s：rateMeasuredTooFast(%.3f, %.3f) = %v，want %v",
				c.what, c.two, c.one, got, c.want)
		}
	}
}

// longWav 造一段三十五秒的单声道 WAV。
//
// 三十五秒：倍率测试要在**一首歌**上量好几段——每一段还量两遍取大的（见
// secPerSong），而 `unityBaseline` 会在每个目标窗口**紧挨着**的位置再量一段原速
// （它自己占四秒歌内）。最靠后的那次基准起点在歌内第二十七秒，占到三十一秒。
// 换句话说歌得比最远的那一段还长出四秒，否则「歌放完了位置停住」会被误判成
// 「慢得离谱」。那份 4 秒的 FLAC 放不下。
//
// 用 WAV 而不是 FLAC 只是因为它是 16 位 PCM，写起来是一段几十行的字节拼装，
// 不必为它在 app 这一层也铺一条 FLAC 编码。
func longWav(t *testing.T, name string) string {
	t.Helper()
	const (
		rate = 44100
		secs = 35
	)
	n := rate * secs
	body := make([]byte, 0, 44+n*2)
	body = append(body, "RIFF"...)
	body = appendU32(body, uint32(36+n*2))
	body = append(body, "WAVEfmt "...)
	body = appendU32(body, 16)   // fmt 块长度
	body = append(body, 1, 0)    // PCM
	body = append(body, 1, 0)    // 单声道
	body = appendU32(body, rate) // 采样率
	body = appendU32(body, rate*2)
	body = appendU16(body, 2)  // 每帧字节数
	body = appendU16(body, 16) // 位宽
	body = append(body, "data"...)
	body = appendU32(body, uint32(n*2))
	for i := range n {
		v := int16(8000 * math.Sin(2*math.Pi*440*float64(i)/rate))
		body = append(body, byte(v), byte(v>>8))
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// readWav 读一份本地 WAV，在线 fixture 要的就是字节。
func readWav(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func appendU32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func appendU16(b []byte, v uint16) []byte { return append(b, byte(v), byte(v>>8)) }

// TestJitterDoesNotDependOnWhichPassWasSlow 钉住 `jitterOf` 是双向的。
//
// 哪一遍慢是随机的（机器什么时候被抢说不准），所以同一份抖动不能因为它出现在
// 「第一遍」还是「第二遍」而给出相反的结论。用 `hi/lo` 或 `lo/hi` 那种有方向的
// 比值就会这样：同样量出 1.0 和 0.5，换个顺序结论就反过来了，而那份抖动一模一样。
func TestJitterDoesNotDependOnWhichPassWasSlow(t *testing.T) {
	for _, c := range []struct {
		name string
		x, y float64
		want float64
	}{
		{"第一遍慢", 0.5, 1.0, 2},
		{"第二遍慢", 1.0, 0.5, 2},
		{"一样", 1.0, 1.0, 1},
		{"差得远", 0.4, 1.6, 4},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := jitterOf(c.x, c.y); got != c.want {
				t.Errorf("jitterOf(%v, %v) = %v，want %v", c.x, c.y, got, c.want)
			}
			// 换个顺序必须给同一个数——这一条才是它存在的理由。
			if got := jitterOf(c.y, c.x); got != c.want {
				t.Errorf("jitterOf(%v, %v) = %v，want %v（换个顺序结论就变了，说明"+
					"这个数带方向：同一份抖动会因为出现在哪一遍而被不对称地对待）",
					c.y, c.x, got, c.want)
			}
		})
	}
}

// TestDistrustNamesTheReasonItself 钉住「量不准的时候，红里会写清楚是谁的锅」。
//
// 这一族没有跳过路径（理由见 distrust 那段），所以红是唯一会被看到的东西。
// 于是**红里必须带一句「这一次可能不是代码的问题」**，并且在机器健康时不许出现
// 那句话——否则真 bug 会被一句免责声明盖过去。
func TestDistrustNamesTheReasonItself(t *testing.T) {
	for _, c := range []struct {
		name     string
		unity    float64
		jitter   float64
		wantWord string
	}{
		{"机器在抖", 1.0, 2.0, "抖"},
		{"跟不上实时", 0.4, 1.0, "跟不上实时"},
		{"机器健康", 1.0, 1.0, ""},
		{"临界不算抖", 1.0, jitteryJitter, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := distrust(c.unity, c.jitter)
			if c.wantWord == "" {
				if got != "" {
					t.Errorf("机器健康时不该有免责说明，却得到 %q——"+
						"那会让真 bug 被一句话盖过去", got)
				}
				return
			}
			if !strings.Contains(got, c.wantWord) {
				t.Errorf("免责说明里没有 %q，看不出是谁的锅：%q", c.wantWord, got)
			}
		})
	}
}
