package app_test

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
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
// 第二个返回值是**抖动**：两遍之间差多少倍。它比「测出来的值是多少」更能说明
// 这一轮能不能拿来判——机器稳定地慢（两遍接近）时那个值是可用的，机器在抖
// （两遍差得远）时那个值什么也不是。见 measurable。
func secPerSong(t *testing.T, a *app.App, from, to time.Duration) (best, jitter float64) {
	t.Helper()
	span := to - from
	lo, hi := window(t, a, from, to), window(t, a, to, to+span)
	return max(lo, hi), hi / lo
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

// measurable 断「这一轮的数能不能拿来判倍率」。
//
// # 慢有两种，只有一种会让比值失效
//
// **慢而均匀**：所有窗口一起慢。实测原速 0.825 时，2 倍速窗口也是 0.825 的两倍——
// 比值照样是 2，**这一族照样测得出结果**。这种情况**不许判成测不了**，跳过一次
// 就是少验一遍。
//
// **慢到供不上**：机器每秒钟只能供出 0.375 秒的音频。这时 2 倍速套上了也是 0.375、
// 没套上也是 0.375（没套上时供的是 1.0，也一样供不上），**两者在这台机器上完全
// 无法区分**。这不是判据松紧的问题，是物理上没有信号可测。
//
// # 所以判据是**两段测量的抖动**，不是「测出来的值多小」
//
// 早先那版看的是「原速基准低于 0.85 就跳过」，那把**慢而均匀**的机器也跳过了：
// 实测到过原速 0.588、而同一个 2 倍速窗口走到了 2.141——机器明明供得上 2 倍速，
// 只是那一段原速窗口撞上了忙，于是比值成了 3.6，于是被当成「测不了」。那是白丢
// 一次验证，而且丢的那一次里代码其实是对的。
//
// 抖动直接回答「这两个数可不可信」：两遍接近 = 稳定，那个值就是可用的（哪怕它
// 整体偏小）；两遍差得远 = 机器在抖，那个值什么也不是。
//
// 四种情形各有归属：
//
//	比值对                          → 量到了，让它过
//	比值不对、抖得厉害              → 测不了，跳过（报出去）
//	比值不对、稳定、机器跟得上实时  → 真的错了，照红
//	比值不对、稳定、机器供不上      → 测不了，跳过（报出去）
func measurable(unity, got, target, jitter float64) bool {
	if jitter > jitteryJitter {
		// 机器在抖：基准和窗口这两个数**都**不是它们的值，比值对不对都不算数。
		return false
	}
	return near(got, unity*target, rateTol) || unity >= healthyUnity
}

// requireMeasurable 在「这一轮根本判不了」的时候跳过，跳之前先打一行出来。
//
// # 为什么跳过时必须打一行
//
// `t.Skip` 在 `go test` **不带 `-v`** 时和通过一样只报 `ok`——也就是「静默地不测」。
// 那正是这一族最不该有的结果：一个人 review 看到 ok 会以为验过了。测试进程的 stderr
// 总是会被 `go test` 原样转发，不带 `-v` 也看得见，所以这里先往 stderr 打一行。
//
// 这一族本来就要求机器跟得上实时播放（它量的就是「走得比实时快多少倍」），所以这
// 个跳过**不是新增的逃逸口**：改动前这里没有任何跳过路径，重负载下只会红成
// 「倍率没套上」——那是把机器的事说成代码的事，比不测更坏。
func requireMeasurable(t *testing.T, unity, got, target, jitter float64) {
	t.Helper()
	if measurable(unity, got, target, jitter) {
		return
	}
	why := "机器跟不上实时播放，原速只走到 " + fmt.Sprintf("%.3f", unity)
	if jitter > jitteryJitter {
		why = fmt.Sprintf("机器在抖（同一段量两遍差 %.2f 倍）", jitter)
	}
	fmt.Fprintf(os.Stderr,
		"BMG-44: %s 测不了——%s，%v 倍速窗口只走到 %.3f。"+
			"这种时候「套上了」和「没套上」量出来的数没法区分（要么都没供上，要么"+
			"两遍量出来根本不是一回事），物理上没有信号可测，所以跳过而不是报红。"+
			"等负载降下来重跑。\n",
		t.Name(), why, target, got)
	t.Skipf("这台机器量不准（%s；%v 倍速窗口 %.3f），倍率在这一刻判不了", why, target, got)
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
	requireMeasurable(t, one, two, 2, max(jTwo, jOne))
	if !near(two, 2*one, rateTol) {
		t.Errorf("2 倍速走 %.3f，原速走 %.3f——比值 %.2f，want 2"+
			"（进度条和耳朵对不上）", two, one, two/one)
	}

	// 0.5 倍速：同一套。
	a.SetRate(0.5)
	// 0.5 倍速的预解缓冲最长（rateGap(0.5) 是两秒歌内），加一秒余量让开三秒。
	half, jHalf := secPerSong(t, a, 16*time.Second, 16*time.Second+winSpan)
	one2, jOne2 := unityBaseline(t, a, 27*time.Second)
	requireMeasurable(t, max(one, one2), half, 0.5, max(jHalf, jOne2))
	// 分母取两段原速里**大的**那个：机器慢的时候多给一次机会，别因为其中一段
	// 慢就把比值压到容差外。
	base := max(one, one2)
	if !near(half, 0.5*base, rateTol) {
		t.Errorf("0.5 倍速走 %.3f，原速走 %.3f——比值 %.2f，want 0.5",
			half, base, half/base)
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
	requireMeasurable(t, one, two, 2, max(jTwo, jOne))
	if !near(two, 2*one, rateTol) {
		t.Errorf("先按的 2 倍速没套到新歌上：原速走 %.3f，2 倍速走 %.3f——比值 %.2f，want 2",
			one, two, two/one)
	}
	// 拨回 2 倍速之后仍然是 2 倍速——量基准那一拨没有把倍率留在 1。
	again, jAgain := secPerSong(t, a, 15*time.Second, 15*time.Second+winSpan)
	requireMeasurable(t, one, again, 2, max(jAgain, jOne))
	if !near(again, 2*one, rateTol) {
		t.Errorf("量完基准再拨回 2 倍速，只走到 %.3f（原速 %.3f）——倍率被量基准那次留下了",
			again, one)
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
	// 第二首上也量一段原速（unityBaseline 会量完拨回 2 倍速），紧挨着 after。
	unity2, j2 := unityBaseline(t, a, 2*time.Second)
	after, jA := secPerSong(t, a, 8*time.Second, 8*time.Second+winSpan)

	// ⚠️ 两个倍速窗口**各自除以自己的原速基准**之后再比，不直接比绝对值。
	//
	// 这两个窗口中间隔了一次切歌（换掉整个播放器），机器在这两段的忙闲可能差很远：
	// 实测有一次 after 1.2、before 1.9，绝对值差 1.6 倍（超容差，红），而两个窗口
	// 各自相对原速都是 2 倍——机器慢，不是倍率掉了。归一化之后比的是「切歌前走的
	// 相对速度和切歌后走的相对速度一样吗」，那才是这一条要问的。
	rBefore, rAfter := before/unity1, after/unity2

	requireMeasurable(t, unity1, before, 2, max(j1, jB))
	requireMeasurable(t, unity2, after, 2, max(j2, jA))
	if !near(rBefore, 2, rateTol) {
		t.Errorf("切歌之前就不对：原速走 %.3f，2 倍速走 %.3f——比值 %.2f，want 2",
			unity1, before, rBefore)
	}
	if !near(rAfter, rBefore, rateTol) {
		t.Errorf("切歌之后从 %.2f 掉到 %.2f（各自的原速基准分别是 %.3f 和 %.3f）"+
			"——新播放器没套上倍率", rBefore, rAfter, unity1, unity2)
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
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.wav", 2*time.Hour), int(bili.QualityHigh320), 110836))

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
	requireMeasurable(t, one, two, 2, max(jTwo, jOne))
	if !near(two, 2*one, rateTol) {
		t.Errorf("在线歌没套上 2 倍速：原速走 %.3f，2 倍速走 %.3f——比值 %.2f，want 2",
			one, two, two/one)
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

// TestOnlyThePhysicallyUntestableCaseIsSkipped 钉住 skip 的判据本身。
//
// 这一族跳过与否决定「这一次有没有真的验倍率」，所以判据必须写成能测的、
// 而且**只有物理上测不了**的那一种情况才放过。下面每一条的数字都是在这台机器上
// 真实见过的（`-race` + 满载，`go test` 跑整包时采集）。
//
// ⚠️ 最要紧的是中间那三条「机器健康却比值不对」：**必须**照红不误。放过它们等于
// 把真 bug 一起放过——那才是真的减少判别力。而「慢而均匀」（0.825、0.761 那两组）
// 同样不许放过：机器慢不等于测不了，比值照样成立。
func TestOnlyThePhysicallyUntestableCaseIsSkipped(t *testing.T) {
	for _, c := range []struct {
		name             string
		unity, got, want float64
		jitter           float64
		wantSkip         bool
	}{
		// 慢而均匀：比值对得上，测得出结果——不许跳过。
		{"慢而均匀的 2 倍速", 0.825, 1.65, 2, 1.05, false},
		{"慢而均匀的 0.5 倍速", 0.761, 0.381, 0.5, 1.05, false},
		// 机器健康但比值不对：是代码的错——照红，不许跳过。
		{"机器健康，倍率刚好没套上", 1.0, 1.0, 2, 1.05, false},
		{"机器健康，倍率反了", 1.0, 0.5, 2, 1.05, false},
		{"机器健康，比值差得远", 0.95, 1.1, 2, 1.05, false},
		// 实测踩到的：2 倍速窗口走到了 2.1（机器供得上），只有原速那段撞上了忙。
		// 这是「机器在抖」，不是「机器供不上」——抖得厉害就该判成测不了。
		{"基准撞上忙，抖动大", 0.588, 2.141, 2, 2.4, true},
		// 供不上：物理上没有信号可测，这是另一种该跳过的。
		{"供不上，套没套上都一样", 0.375, 0.375, 2, 1.05, true},
		{"供不上，但这一轮恰好比值对上了", 0.375, 0.75, 2, 1.05, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			skip := !measurable(c.unity, c.got, c.want, c.jitter)
			if skip != c.wantSkip {
				why := "判成测不了，会把一次真验跳过去"
				if c.wantSkip {
					why = "判成可测，机器测不准时会把噪声报成「倍率没套上」"
				}
				t.Errorf("unity=%v got=%v want=%v jitter=%v：%s（跳过了=%v，want %v）",
					c.unity, c.got, c.want, c.jitter, why, skip, c.wantSkip)
			}
		})
	}
}

// TestTheSkipIsNeverSilent 钉住「跳过时必须先打一行到 stderr」。
//
// `t.Skip` 在 `go test` **不带 `-v`** 时和通过一样只报 `ok`——一个人 review 看到
// ok 会以为这一族验过了。所以 `requireMeasurable` 跳过之前先往 stderr 打一行
// （测试进程的 stderr 总是被 `go test` 原样转发）。这行字是「不静默」的唯一保证，
// 它要是哪天被删了，这里立刻红。
func TestTheSkipIsNeverSilent(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w

	skipped := t.Run("机器测不准的那一轮", func(t *testing.T) {
		defer func() { os.Stderr = old }() // Skip 走的是 Goexit，defer 照样跑
		requireMeasurable(t, 0.375, 0.375, 2, 1.05)
		t.Error("这一轮该跳过（机器供不上，套没套上量出来一样），却没有跳")
	})
	os.Stderr = old
	_ = w.Close()

	if !skipped {
		t.Error("子测试没有以跳过结束")
	}
	line, _ := io.ReadAll(r)
	if len(bytes.TrimSpace(line)) == 0 {
		t.Error("跳过了，但 stderr 上一行字也没有——`go test` 不带 -v 时这就等于静默不测")
	}
	// 这行字要能让人自己判断：带实测值，也带「为什么」。
	for _, want := range []string{"0.375", "2 倍速", "测不了"} {
		if !bytes.Contains(line, []byte(want)) {
			t.Errorf("stderr 那行里没有 %q，拿不到实测值：%q", want, bytes.TrimSpace(line))
		}
	}
}
