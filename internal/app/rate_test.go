package app_test

import (
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

// secPerSong 量「一秒真实时间里，歌里的时间走了多少」。
//
// ⚠️ 只在**同一轮里比两个窗口**时才准，见文件头那段。要看的从来不是它等于几，
// 而是它和同一轮另一个窗口比是大还是小。
//
// from/to 之间的歌内时间就是窗口。窗口越短越容易被两端的采样误差吃掉：位置每
// 50ms 才写回一次（app.go 的 tickInterval），所以一秒的窗口两端各能差 50ms。
func secPerSong(t *testing.T, a *app.App, from, to time.Duration) float64 {
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

// realtimeFloor 是「1 倍速下，歌里的时间至少要走这么快」的下限。
//
// # 为什么这一族量倍率之前要先过这一关
//
// 下面几条量的是**比值**：同一首歌上量 1 倍速和另一个倍速的两个窗口，比值要是
// 那个倍率。机器慢会让两个窗口**一起**慢、比值不变——这正是这些断言写成比值
// 而不是「一秒该走几秒」的原因。
//
// 但慢到某个程度就不成立了。播放走系统音频输出，那条回调被操作系统整个饿着的
// 时候（这台机器上同时跑着好几份 `-race`，负载压到 300 以上），某些窗口拿得到
// CPU、某些拿不到，**比值就成了噪声**。实测同一轮里 0.5 倍速那个窗口只走到
// 0.086、原速那个走到 0.331——比值 0.26，既不是 0.5（没套上）也不是别的任何倍率。
// 那种时候报「倍率没套上」是把机器的事说成代码的事，下一个人会照着去改
// `App.tick`，改完还是红的。
//
// 0.6 是两头量的：1 倍速的定义就是 1.0，掉到 0.6 以下时比值里的机器噪声已经和要
// 量的东西一样大。真要是**1 倍速本身**走慢了，那该由另一条单独断（它是进度条
// 时钟的问题，不是倍率的问题）——混在这一族里只会让它既报不出倍率的问题、
// 又盖住时钟的问题。
const realtimeFloor = 0.6

// requireRealtime 量一段原速窗口，顺带确认这台机器放得动实时音频。
//
// 放不动就**跳过**——跳过说的是「这个环境测不了这条」，不是「这条过了」。
// 跳过的信息里带着实测值，等负载降下来重跑就行。
//
// 返回的是基准值本身，调用方拿它当比值的分母。
func requireRealtime(t *testing.T, a *app.App, from, to time.Duration) float64 {
	t.Helper()
	v := secPerSong(t, a, from, to)
	if v < realtimeFloor {
		t.Skipf("这台机器放不动实时音频：1 倍速下歌里的时间只走了每秒 %.3f"+
			"（下限 %.1f）。倍速这一族量的是两个窗口的比值，音频回调被饿着时比值"+
			"是噪声，报「倍率没套上」是把机器的事说成代码的事——等负载降下来再跑。",
			v, realtimeFloor)
	}
	return v
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

	// 基准：原速。等到三秒才开始量——开头那段是输出现开 + 预解缓冲，不算数。
	// requireRealtime 在这里兼做一次「机器放得动实时音频吗」的自检：它把这个
	// 基准值原样还回来，下面就拿它当分母。
	a.SetRate(1)
	one := requireRealtime(t, a, 3*time.Second, 5*time.Second)

	a.SetRate(2)
	// 换档之后要越过那一圈预解缓冲（≈1 秒的输出，仍是旧倍率铺的）才开始量。
	two := secPerSong(t, a, 9*time.Second, 11*time.Second)

	a.SetRate(0.5)
	half := secPerSong(t, a, 15*time.Second, 17*time.Second)

	// 容差 1.8 倍：`-race` 下音频回调被抢会让所有窗口一起变小，而比值不变；
	// 真正的错（没套上 = 1.0、反过来 = 0.5）都掉在这个范围之外。
	const tol = 1.8
	if !near(two, 2*one, tol) {
		t.Errorf("2 倍速走 %.3f，原速走 %.3f——比值 %.2f，want 2"+
			"（进度条和耳朵对不上）", two, one, two/one)
	}
	if !near(half, 0.5*one, tol) {
		t.Errorf("0.5 倍速走 %.3f，原速走 %.3f——比值 %.2f，want 0.5",
			half, one, half/one)
	}
}

// TestRateIsRememberedBeforeAnyTrackIsOpen 盯「先按倍速再点歌」。
//
// 用户完全可能先在上一首歌上按了 2 倍速，切到下一首。那一首得从第一秒起
// 就是 2 倍速——不是开了之后再由用户补按一次。
//
// 这里只断「明显快过原速」而不是断精确的 2 倍：这一条要验的是「倍率有没有送到
// 新播放器上」，精确值由上面那条钉（那边有原速当基准）。少了原速当基准，一台
// 慢一半的机器会把「套上了」也判成「没套上」。
func TestRateIsRememberedBeforeAnyTrackIsOpen(t *testing.T) {
	a := newApp(t)
	if st := a.Snapshot(); st.HasTrack || st.Rate != 1 {
		t.Fatalf("刚开出来就有歌了（%+v）", st)
	}

	// 先用第一首量一段原速当基准（见 requireRealtime）。这一步不碰这一条要验的
	// 东西：此刻倍率还是 1，「先按倍率再开歌」的走法在下面一步才正式开始。
	if err := a.Open(longWav(t, "量基准的一首.wav")); err != nil {
		t.Fatal(err)
	}
	requireRealtime(t, a, 3*time.Second, 5*time.Second)

	a.SetRate(2)
	if err := a.Open(longWav(t, "先按后放.wav")); err != nil {
		t.Fatal(err)
	}
	if got := a.Snapshot().Rate; got != 2 {
		t.Fatalf("开歌之后倍率变成了 %v，want 2", got)
	}
	if got := secPerSong(t, a, 3*time.Second, 5*time.Second); got < 1.3 {
		t.Errorf("先按的 2 倍速没套到新歌上：原速该是 1，实测 %.2f", got)
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

	// 先在原速上量一段，确认这台机器放得动实时音频（见 requireRealtime），再量
	// 倍速。窗口往后挪是因为原速那一段已经占掉了歌的前五秒。
	requireRealtime(t, a, 3*time.Second, 5*time.Second)

	a.SetRate(2)
	before := secPerSong(t, a, 7*time.Second, 9*time.Second)

	a.Next()
	playing(t, a, "第二首")
	if got := a.Snapshot().Rate; got != 2 {
		t.Fatalf("切到第二首之后倍率变成了 %v，want 2", got)
	}
	if pos := a.Snapshot().Position; pos > 2*time.Second {
		t.Errorf("第二首一上来位置就在 %v——上一首的进度接着数了", pos)
	}
	after := secPerSong(t, a, 3*time.Second, 5*time.Second)

	if before < 1.3 {
		t.Errorf("切歌之前就走得不够快：%.2f（原速是 1）", before)
	}
	if !near(after, before, 1.8) {
		t.Errorf("切歌之后从 %.2f 掉到 %.2f——新播放器没套上倍率", before, after)
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

	// 先在原速上把同一首流放一遍，确认这台机器放得动实时音频（见 requireRealtime）。
	if err := a.Enqueue(onlineItem("BV1xx", "123", "在线的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "在线的一首")
	requireRealtime(t, a, 3*time.Second, 5*time.Second)

	// 真正要验的那一遍：先按倍率再开这一首。
	a.SetRate(2)
	if err := a.Enqueue(onlineItem("BV1xx", "123", "在线的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "在线的一首")

	if got := a.Snapshot().Rate; got != 2 {
		t.Fatalf("在线歌的倍率是 %v，want 2", got)
	}
	if got := secPerSong(t, a, 3*time.Second, 5*time.Second); got < 1.3 {
		t.Errorf("在线歌没套上 2 倍速：原速该是 1，实测 %.2f", got)
	}
}

// longWav 造一段二十秒的单声道 WAV。
//
// 二十秒：倍率测试要在一首歌上量三个窗口（1x、2x、0.5x），光 0.5 倍速那一个的
// 过渡就要走掉五六秒歌。那份 4 秒的 FLAC 放不下（中途播完会被误判成
// 「慢得离谱」）。
//
// 用 WAV 而不是 FLAC 只是因为它是 16 位 PCM，写起来是一段几十行的字节拼装，
// 不必为它在 app 这一层也铺一条 FLAC 编码。
func longWav(t *testing.T, name string) string {
	t.Helper()
	const (
		rate = 44100
		secs = 20
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
