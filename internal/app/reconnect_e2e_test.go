package app_test

// 「断流 → 重连 → 恢复」那条链路（票 #315 的验收项）。
//
// # 这一组是**端到端**的
//
// 真的字节源、真的退避、真的应用层接线、真的 `State`。它盯的是
// 「字节源说重试 → 应用层知道 → `State.Reconnecting` 亮起来」这条**接线**。
//
// 🔴 **手工调 `noteReconnect` 的用例证明不了它**：那些用例绕过了「源说重试」
// 这一步——源那边到底报不报、应用层到底知不知道，只有走真的源才验得到。
//
// # ⚠️ **这一组守不住的是 `buildFromRef` 里那两个回调**
//
// `OnRetry` / `OnReconnected` 这两个回调**只做一件事：叫醒界面**（它们不碰
// 任何状态）。**把那两个回调整个拆掉，这一组照样全绿**——因为「正在重连」
// 这一档的状态**住在源那边**（`StreamSource.Reconnecting()`），应用只**问**
// 当前在管的那一条流（见 `reconnect.go` 的文件头）：叫不叫醒界面，这一档的
// 状态都不变。
//
// ⚠️ **所以别拿「拆掉那两个回调这一组会红」当证据**——那句话在这份文件头上
// 说过一次，而它是**错的**（票 #315 的评审查出来的）。这一组守的是**状态**；
// 「叫醒界面」那一半归界面层（放歌时界面每 50 毫秒自己醒一次，见 AGENTS.md
// §4 第 3 条）。
//
// 这一组用的全是 **HTTP fixture**（假 B 站 + 假 CDN）：**不打真 B 站**。

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// TestTheStreamBreakShowsUpAsReconnecting 盯「断流 → 界面上说正在重连」。
//
// CDN 下到一半把连接掐了 → 源要退避 + 换一条新地址 → **那几秒里界面上必须
// 有字**。以前那几秒是**完全静默**的（票 #315 的现象：歌停着、界面不动、
// 没有字）。
//
// 🔴 **这一句是整条链路的判据**：它断的是「源说了重试 → 应用层知道 → 状态
// 上有」，而这一层以前**一个断言都没有**。
func TestTheStreamBreakShowsUpAsReconnecting(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.failMediaAfter = len(f.media) / 3

	// 两条地址指向**同一段字节**：B 站换流给的就是同一段。
	f.onPlayURL(dashFor(cdnURL(f.URL, "v1.flac", time.Hour), int(bili.QualityHigh192), 110836))
	f.onPlayURL(dashFor(cdnURL(f.URL, "v2.flac", time.Hour), int(bili.QualityHigh192), 110836))

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "会被掐断的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "会被掐断的一首")

	waitFor(t, 15*time.Second, "播放位置动起来", func() bool {
		return a.Snapshot().Position > 0
	})
	waitFor(t, 15*time.Second, "下到断点", func() bool { return f.mediaHits.Load() >= 1 })
	before := a.Snapshot().Position
	close(f.mediaGate)

	// 🔴 **断流之后、退避期间，界面上必须说得出「正在重连」**。
	waitFor(t, 30*time.Second, "界面上出现「正在重连」", func() bool {
		return a.Snapshot().Reconnecting
	})

	// 恢复了：换流真的发生了（取流接口被打第二次），而且歌从断点接着放。
	waitFor(t, 30*time.Second, "换流之后继续往前播", func() bool {
		return f.playCalls.Load() >= 2 && a.Snapshot().Position > before
	})
	// 恢复之后那一行必须收掉：它说的是「还在救」，而歌已经在放了。
	waitFor(t, 30*time.Second, "「正在重连」收掉", func() bool {
		return !a.Snapshot().Reconnecting
	})
	if st := a.Snapshot(); st.Err != "" {
		t.Errorf("恢复之后播放条上有错：%s", st.Err)
	}
}

// TestTheSecondReopenSaysItGaveUp 盯「第二次重开被挡掉」那一支。
//
// 一首歌只重开一次（`onlineTrack.reopening`）。第二次还不行就该**如实报错**。
//
// 🔴 **以前这里是直接 `return`**（票 #315 的评审复现过）：界面上「正在重连」
// 那一档**永远亮着**、一句红字也没有，而歌早就停了。用户看到的「正在重连」
// 是**假的**——它压根没有在重连，它是放弃了。
//
// 这条链路的形状：
//
//	第一条流断 → 换流换到了**另一条**（容器不同）→ 从播放位置重开
//	重开后的那条流又断 → 换流又换到了**另一条**（容器又不同）
//	→ 这一首已经重开过一次了，救不回来 → **必须报错**
//
// 🔴 **第二条流的断点要放行才掐**：立刻掐的话那一段字节未必到了客户端，
// 于是「断在哪儿」成了随机的数（`internal/service/audio` 的 `cutAfter` 那条
// 注释说的就是这件事）。
func TestTheSecondReopenSaysItGaveUp(t *testing.T) {
	f := newStreamFixture(t)
	data := readFlac(t)
	f.media = data
	f.failMediaAfter = len(data) / 3

	// 三条地址：容器在 .flac / .m4s 之间来回换，每换一次源就拒绝硬接
	// （`sameContainer` 判它们不是同一条）→ `ErrStreamChanged`。
	f.onPlayURL(dashFor(cdnURL(f.URL, "v1.flac", time.Hour), int(bili.QualityHigh192), 110836))
	f.onPlayURL(dashFor(cdnURL(f.URL, "v2.m4s", time.Hour), int(bili.QualityHigh192), 110836))
	f.onPlayURL(dashFor(cdnURL(f.URL, "v3.flac", time.Hour), int(bili.QualityHigh192), 110836))

	// 第二条流（重开之后那条）自己断一次，断点等 gate2 放行。
	gate2 := make(chan struct{})
	f.on("/cdn/v2.m4s", func(w http.ResponseWriter, r *http.Request) {
		f.mediaHits.Add(1)
		serveUntilCut(w, r, data, len(data)/3, gate2)
	})

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "换了两次的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "换了两次的一首")

	waitFor(t, 15*time.Second, "播放位置动起来", func() bool {
		return a.Snapshot().Position > 0
	})
	waitFor(t, 15*time.Second, "下到断点", func() bool { return f.mediaHits.Load() >= 1 })
	// 放行第一次掐断 → 源换流 → 换到了 v2.m4s → 容器不同 → 重开。
	close(f.mediaGate)

	// 等重开之后的那条流也开始下了。
	waitFor(t, 30*time.Second, "重开之后第二条流开始下", func() bool {
		return f.mediaHits.Load() >= 2
	})
	// 放行第二次掐断 → 源再换流 → 换到了 v3.flac → 容器又不同 → 这一首
	// 已经重开过一次了。
	close(gate2)

	// 🔴 **必须有一句能看懂的话**：用户看到的是「歌停了」，而界面上要么是
	// 一句红字，要么是「正在重连」——**不能是后者**（它压根没有在重连）。
	waitFor(t, 30*time.Second, "播放条上出现一句失败的话", func() bool {
		return a.Snapshot().Err != ""
	})
	st := a.Snapshot()
	if st.Reconnecting {
		t.Errorf("已经放弃了还说着「正在重连」：%+v", st)
	}
	if !strings.Contains(st.Err, "接不上") {
		t.Errorf("失败的那句话没说清是「接不上」：%q", st.Err)
	}
	if st.Playing {
		t.Errorf("歌已经停了，`Playing` 却还是真：%+v", st)
	}
}

// serveUntilCut 发 [from, cut) 这一段，然后等 gate 放行再掐断连接。
//
// ⚠️ **断点不是 handler 一写完就掐连接**：掐得太早的话，那一段字节未必到了
// 客户端（`internal/service/audio` 的 `cutAfter` 那条注释说的就是这件事），
// 于是「断在哪儿」成了一个随机的数。
//
// 带 Range 的后续请求（from >= cut）**正常发完**——那是重试那一次。
func serveUntilCut(w http.ResponseWriter, r *http.Request, data []byte, cut int, gate chan struct{}) {
	from := 0
	if h := r.Header.Get("Range"); strings.HasPrefix(h, "bytes=") {
		from, _ = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(h, "bytes="), "-"))
	}
	if from > len(data) {
		http.Error(w, "越界", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(data)-from))
	if from > 0 {
		w.Header().Set("Content-Range",
			fmt.Sprintf("bytes %d-%d/%d", from, len(data)-1, len(data)))
		w.WriteHeader(http.StatusPartialContent)
	}
	if from < cut {
		w.Write(data[from:cut])
		w.(http.Flusher).Flush()
		<-gate
		panic(http.ErrAbortHandler)
	}
	w.Write(data[from:])
}

// TestTheBreakDuringTheFirstLoadIsStillVisible 盯「**首次取流期间**断流」。
//
// 🔴 **这一条是评审复现的那个洞**：源刚点亮「正在重连」，`runOnline` 装上新
// 播放器时 `clearErrLocked` 顺手把它清了——于是退避那一秒**完全静默**，
// 而歌正卡着（位置不动、没有声音、界面上一个字都没有）。
//
// 退避之前这里是「瞬间重试」，用户无感；退避把它变成了一段**确定的静默**。
//
// 🔴 **断点给在 20%**：FLAC 的头在头 3.3% 里（`stereo16.flac` 那个 8 KB 的
// PADDING 块），所以 20% 足够**开口**（播放器装得上），而掐断之后源只能
// 等退避过去才有下一段。
//
// 🔴 **掐断不等门**：立刻掐的话，那一段字节未必到了客户端，而这一组要的
// 恰恰是「**播放器已经装上了、源正在退避**」那个窗口。
func TestTheBreakDuringTheFirstLoadIsStillVisible(t *testing.T) {
	f := newStreamFixture(t)
	data := readFlac(t)
	f.media = data

	// 第一次请求：发 20%，然后**立刻**掐断连接。
	gate := make(chan struct{})
	close(gate)
	f.on("/cdn/v1.flac", func(w http.ResponseWriter, r *http.Request) {
		f.mediaHits.Add(1)
		serveUntilCut(w, r, data, len(data)/5, gate)
	})
	f.onPlayURL(dashFor(cdnURL(f.URL, "v1.flac", time.Hour), int(bili.QualityHigh192), 110836))
	f.onPlayURL(dashFor(cdnURL(f.URL, "v2.flac", time.Hour), int(bili.QualityHigh192), 110836))

	a := newOnlineApp(t, f, t.TempDir())

	// 🔴 **采样要密**：旧行为下那一档只亮**不到 2ms**（源刚点亮就被装播放器
	// 那一下清了）——10ms 的采样会整个错过它，于是这条用例在旧行为下照样
	// 绿（评审复现过：2ms 采样、2653 条，一次都没看见）。
	lit := make(chan bool, 1)
	stop := make(chan struct{})
	go func() {
		seen := false
		for {
			select {
			case <-stop:
				lit <- seen
				return
			default:
			}
			if a.Snapshot().Reconnecting {
				seen = true
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	if err := a.Enqueue(onlineItem("BV1", "1", "刚开始就断的一首")); err != nil {
		t.Fatal(err)
	}

	// 🔴 **那一秒里界面上必须有字**：否则用户对着一个不动的界面等。
	waitForFine(t, 30*time.Second, "界面上出现「正在重连」", func() bool {
		return a.Snapshot().Reconnecting
	})

	// 恢复之后那一行必须收掉，而歌要接着放。
	waitFor(t, 30*time.Second, "「正在重连」收掉", func() bool {
		return !a.Snapshot().Reconnecting
	})
	waitFor(t, 30*time.Second, "歌从断点接着放", func() bool {
		return a.Snapshot().Position > 0
	})
	if st := a.Snapshot(); st.Err != "" {
		t.Errorf("恢复之后播放条上有错：%s", st.Err)
	}
	close(stop)
	if !<-lit {
		t.Error("采样里一次都没看见「正在重连」——那一档亮得太短，等于没有")
	}
}

// waitForFine 每 2ms 采一次，等一个条件成立。
//
// 🔴 **为什么不用现成的 `waitFor`**：它每 10ms 采一次，而要等的那一档在旧
// 行为下只亮**不到 2ms**（源刚点亮就被装播放器那一下清了）——10ms 的采样
// 会整个错过它，于是这条用例在旧行为下照样绿。
func waitForFine(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("等了 %v，%s 还没发生", d, what)
}

// TestAStaleSourceDoesNotStealTheReconnectState 盯「一条被丢掉的源抢不走状态」。
//
// 队列两首：「慢的那首」取流慢 400ms、「快的那首」正常。起播慢的那首
// （代次 1），100ms 后切到快的那首（代次 2，它先建源先装上）；慢的那首的
// 取流这时才回来 → 它建了一条源 → 代次对不上 → 那条源被丢掉。
//
// 🔴 **以前那条被丢掉的源会把「正在重连」抢走**（那时候「谁在重连」是
// 「建源时」发的一个令牌，谁最后建谁抢）：于是快的那首断流时，它的重试
// 被守卫丢掉，界面上「正在重连」**一次都不亮**——正是这一票要消灭的
// 「歌停着、界面不动、没有字」（票 #315 的评审复现过）。
//
// 现在那一档**住在源那边**，应用只问自己当前在管的那一条流——被丢掉的那条
// 压根没人问。
func TestAStaleSourceDoesNotStealTheReconnectState(t *testing.T) {
	f := newStreamFixture(t)
	data := readFlac(t)
	f.media = data

	// 「慢的那首」的那一次取流慢 400ms；快的那首那一次正常。
	var (
		mu           sync.Mutex
		playCalls    int
		slowReturned = make(chan struct{})
	)
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		playCalls++
		mine := playCalls
		mu.Unlock()
		if mine == 1 {
			time.Sleep(400 * time.Millisecond)
		}
		f.servePlayURL(w, r.URL.Query())
		if mine == 1 {
			// 响应写回去了：应用那边马上就要建源（然后发现代次过期，把它丢掉）。
			close(slowReturned)
		}
	})
	f.onPlayURL(dashFor(cdnURL(f.URL, "v1.flac", time.Hour), int(bili.QualityHigh192), 110836))
	f.onPlayURL(dashFor(cdnURL(f.URL, "v1.flac", time.Hour), int(bili.QualityHigh192), 110836))

	// CDN：发到 20% 就停住，等 gate 放行才掐断——这样断点才是确定的。
	gate := make(chan struct{})
	f.on("/cdn/v1.flac", func(w http.ResponseWriter, r *http.Request) {
		f.mediaHits.Add(1)
		serveUntilCut(w, r, data, len(data)/5, gate)
	})

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "慢的那首")); err != nil {
		t.Fatal(err)
	}
	// 慢的那首已经在取流了（playurl 的第一次调用），这时候切到快的那首。
	waitFor(t, 15*time.Second, "慢的那首开始取流", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return playCalls >= 1
	})
	if err := a.Enqueue(onlineItem("BV2", "2", "快的那首")); err != nil {
		t.Fatal(err)
	}
	if err := a.PlayAt(1); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "快的那首")

	// 等慢的那首的取流回来：它会建一条源，然后发现代次过期，把它丢掉。
	select {
	case <-slowReturned:
	case <-time.After(30 * time.Second):
		t.Fatal("慢的那首的取流一直没回来")
	}
	// 给它「建源 + 作废」的时间（响应写回去之后应用才建源）。
	time.Sleep(200 * time.Millisecond)

	// 现在掐断 CDN：快的那首的源会重试。
	close(gate)

	// 🔴 **界面上必须说得出「正在重连」**，而且说的是**快的那首**的这一次：
	// 那条被丢掉的源（慢的那首留下的）抢不走这一档。
	//
	// ⚠️ **「快的那首」不在了就停**：那首歌放完了、队列往下走了，而那一档跟
	// 它无关——再等下去只会等到**下一首**的那一次重连（那一次是好的，但它证明
	// 不了这一条）。
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		st := a.Snapshot()
		if st.Title != "快的那首" {
			break
		}
		if st.Reconnecting {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("快的那首断流之后，界面上一次都没说「正在重连」：%+v", a.Snapshot())
}
