package app_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// 后台时钟不许被在线流的下行速度拖着。
//
// # 这条钉的是什么
//
// 界面每一帧的第一件事是 `Snapshot()`，而 `Snapshot()` 要 `a.mu`；`a.mu` 又被
// 后台时钟拿着去问播放器位置。于是只要「问位置」这件事会卡，整个窗口就跟着卡。
//
// 问位置为什么会卡？`audio.Player.Position` 要过变速层那把锁，而那一层曾经
// **拿着锁去读解码器**——在线流的解码器读的就是 CDN。CDN 卡住 → 锁卡住 →
// 位置卡住 → 窗口卡住。而 `StreamSource` 的卡死判定是 20 秒
// （`stream_source.go` 的 `defaultStallTimeout`），线上最坏就是**窗口卡 20 秒**。
//
// 两处都要在：变速层读内层时不持锁（`rate.go` 的 `readInner`），后台时钟问位置
// 时不持 `a.mu`（`app.go` 的 `tick`）。少任何一处，下面这条都会红——实测：
// 退掉 `readInner` 红，退掉 `tick` 也红（那一处是放大器：锁本来还被 `a.mu`
// 传下去，`Position()` 一卡就是整个窗口一卡）。
//
// # 为什么这条必须走真的在线流
//
// 慢速本地文件**不会**触发（读本地文件不阻塞）；App 这一层也没有办法换掉自己
// 的播放器。于是这里用真链路：httptest 当 CDN，先给一大块，剩下的每一块都要
// 测试亲手放行——不放行它就一直停在那儿，正是线上「CDN 挪不动」的那一下。
//
// # 三条自证，缺一条这个量就没意义
//
// 这三条全是踩过坑之后加的。前两版都在「回退修复之后照样绿」，绿着比红着更误导人。
//
//	位置走过一截   音频输出刚开的那两三百毫秒位置也是**不动**的，那不是「卡住」
//	             那是「还没起步」——不先等过这一段，「停住了」会在开机那一刻误判
//	位置没怎么走   量的是 3 秒；源要是其实在喂，3 秒里位置就该走满 3 秒。走了超过
//	             一半就说明这一轮量的不是「CDN 冻住时的界面」，那个门槛也就没有
//	             意义——直接失败，不给出一个假的漂亮数字。
//	冻住是有限的   回归真的发生的时候界面不是变慢而是彻底卡住，而源是被本测试
//	             冻着的——不设上限的话这条用例会一路挂到 go test 的十分钟超时。
//	             所以冻 3 秒就自己放行：那 3 秒既是量，也是回归时的「卡住时长」。
func TestSnapshotIsNotStalledByAStuckStream(t *testing.T) {
	const (
		// measure 是量 Snapshot 的窗口。源在这段时间里是堵着的。
		measure = 3 * time.Second
		// freezeFor 是总闸：CDN 再过这么久自动放行。取得比「等位置走过一秒
		// + 基准 + 窗口」都宽，好让正常时它落在量完之后。
		freezeFor = 8 * time.Second
		// budget 是界面一帧的上限。源在动时实测是**微秒**级（见下面那行日志），
		// 而 CDN 卡死的判定是 20 秒。250ms 夹在中间偏得极远：比基线宽三个
		// 数量级，比「跟着卡」小两个数量级。机器慢也不至于把一把互斥锁的开销
		// 变成 250ms，而这条要挡的是「锁被一次几秒的网络读持着」那种整数量级
		// 的回归。
		budget = 250 * time.Millisecond
		// stallBudget 是「位置在这段窗口里最多允许走多少」的自证阈值。源要是
		// 其实在喂，3 秒里它该走 3 秒；源冻着的话，它最多把开头那一块剩下的
		// 放完（这里不到一秒）就停住。
		stallBudget = measure / 2
	)

	f := newStreamFixture(t)
	// 二十秒的 WAV，不是那份四秒的 FLAC：开头那一块要装得下「MyGo 那一圈预解
	// 缓冲」还多留一点，那份素材压缩得太狠（一整首歌才 250KB），掐出来的
	// 开头里塞得下整首歌——源压根没堵上，量了个寂寞。
	f.media = readWav(t, longWav(t, "在线的一首.wav"))

	// proceed 放行下一块；resume 到点自动放行，免得测试挂在半截。
	proceed := make(chan struct{})
	resume := make(chan struct{})
	f.on("/cdn/frozen.flac", func(w http.ResponseWriter, r *http.Request) {
		data := f.media
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		flusher, _ := w.(http.Flusher)

		// 开头这一块 = 1.53 秒的音频。`longWav` 是**单声道** 44100Hz 16 位，
		// 一秒 88200 字节——按双声道算会多算一倍，那一块里就够放三秒，
		// 量的时候源还在喂（这个坑也踩过：量出来「3 秒里位置走了 1.97 秒」，
		// 一查是这里按双声道算的）。
		//
		// 下限是 MyGo 那一圈预解缓冲（`mygo/audio/source.go` 的 ring，约一秒
		// 的解码量）：给少了进度条压根不会开始走，后面量的是一个没起步的播放器。
		// 余量 0.53 秒：进度条走到 1 秒之后还有半秒放完，然后就真的堵住了。
		const (
			prefix = 132 << 10
			chunk  = 8 << 10
		)
		w.Write(data[:prefix])
		if flusher != nil {
			flusher.Flush()
		}

		// 剩下的每一块都要有人放行。测试不放，它就一直停在这儿。
		for off := prefix; off < len(data); off += chunk {
			select {
			case <-proceed:
			case <-resume:
			}
			end := min(off+chunk, len(data))
			if _, err := w.Write(data[off:end]); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	})
	f.onPlayURL(dashFor(cdnURL(f.URL, "frozen.flac", 2*time.Hour),
		int(bili.QualityHigh192), 110836))

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1xx", "123", "在线的一首")); err != nil {
		t.Fatal(err)
	}
	defer close(proceed)

	playing(t, a, "在线的一首")

	// 整段测量挂在一个**总闸**上：再过 `freezeFor` 秒 CDN 自动放行。
	//
	// 正常时它落在测量窗口之后，一点不影响量；回归真的发生的时候（窗口整个
	// 冻住、什么都读不到）它保证一切都解得开——测试挂到 go test 的十分钟超时
	// 是最坏的一种失败：下一个人只会以为这机器慢。
	release := time.AfterFunc(freezeFor, func() { close(resume) })
	defer release.Stop()

	// 走完开头那一块（1.53 秒）之后源就堵住了，而进度条要走到 1 秒才算真的
	// 起步了——音频输出刚开的那两三百毫秒位置也是不动的，那不是「卡住」。
	waitFor(t, 30*time.Second, "进度条真的走过一秒", func() bool {
		return a.Snapshot().Position >= time.Second
	})

	// 基准：源还在动的时候（开头那一块还有得放）。
	base := worstSnapshot(t, a, 20, 0, 3*time.Second)

	from := a.Snapshot().Position
	worst := worstSnapshot(t, a, 60, measure/60, 3*measure)
	t.Logf("源在动时最慢一次 Snapshot = %v；CDN 冻住那 %v 里最慢 = %v", base, measure, worst)

	if worst > budget {
		t.Fatalf("CDN 冻住的时候，界面一帧最慢用了 %v（源在动时是 %v）——"+
			"窗口会被拖着一起卡住，而 StreamSource 的卡死判定是 20 秒", worst, base)
	}

	// 到这里为止「看起来是好的」。但「看起来好」也可能是因为源压根没堵上——
	// 这条用例的前两版就栽在这儿，回退修复之后照样绿。再自证一次：源要是其实
	// 在喂，这 %v 里位置就该走满 %v。
	if moved := a.Snapshot().Position - from; moved > stallBudget {
		t.Fatalf("源没有按预期堵住（%v 里位置走了 %v，而开头只喂了 1.53 秒）——"+
			"这一轮量到的不一定是「CDN 冻住时的界面」，下面那个 %v 的门槛也就没有意义了",
			measure, moved, budget)
	}
}

// worstSnapshot 量 n 次 Snapshot() 里最慢的那一次，两次之间歇 d。
//
// ⚠️ 必须带上限。回归真的发生的时候，界面**不是变慢而是彻底卡住**：`a.mu` 被
// 后台时钟拿着不放，而它放不放要看源什么时候恢复。源到点自动恢复，所以这个
// 上限平时够宽（量完 60 次绰绰有余），而回归时会变成一条清楚的失败而不是
// 一路挂到 go test 的十分钟超时。
func worstSnapshot(t *testing.T, a *app.App, n int, d, cap time.Duration) time.Duration {
	t.Helper()
	done := make(chan time.Duration, 1)
	go func() {
		var worst time.Duration
		for range n {
			if d > 0 {
				time.Sleep(d)
			}
			start := time.Now()
			_ = a.Snapshot()
			if el := time.Since(start); el > worst {
				worst = el
			}
		}
		done <- worst
	}()

	select {
	case el := <-done:
		return el
	case <-time.After(cap):
		t.Fatalf("界面 %v 之内连一次快照都拿不到——窗口整个卡住了"+
			"（后台时钟拿着 a.mu 去问一个堵着的源）", cap)
		return 0
	}
}
