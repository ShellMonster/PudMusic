package app

// 这一组盯的是「正在重连」这一档在**应用层**的接线（票 #315）。
//
// # 🔴 那一档的**源那一半住在源那边**
//
// `audio.StreamSource.Reconnecting()` 报「这条流是不是在等字节回来」，应用
// 只**问**当前在管的那一两条流（见 `reconnect.go`）。
//
// ⚠️ **为什么不能在这儿存一栏**（票 #315 的评审复现过）：一条**建出来又被
// 丢掉**（代次过期）的源会把那一栏抢走，于是正在放的那首断流时，界面上
// 「正在重连」**一次都不亮**——正是这一票要消灭的「歌停着、界面不动、没有
// 字」。这一组盯的就是「**应用问的是哪一条源**」。
//
// 这一组用的是**真的源**（真退避、真 HTTP fixture）：手工塞一栏进去的话，
// 证明不了「源的状态真的到了界面上」。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/audio"
)

// retryingSource 造一条「正在重试」的字节源（它一直下不动）。
//
// 真源、真退避——只是把退避调得很快（几毫秒），好让用例不慢。
func retryingSource(t *testing.T) *audio.StreamSource {
	t.Helper()
	return retryingSourceWith(t, 10*time.Millisecond)
}

// retryingSourceWith 造一条「正在重试」的字节源，退避给的是多久。
//
// ⚠️ **退避要够长**：源只在**退避里等着**的那一段是「正在重试」，退避一
// 完它就放弃（`MaxRefresh` 用完了）——退避只有几毫秒的话，那一档亮着的
// 时间只有那么一小会儿，断言就变成「赌那一瞬间正好采到」了。
func retryingSourceWith(t *testing.T, wait time.Duration) *audio.StreamSource {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "地址失效了", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	s, err := audio.NewStreamSource(audio.StreamSourceOptions{
		URLs:    []string{srv.URL},
		Client:  srv.Client(),
		TempDir: t.TempDir(),
		Backoff: []time.Duration{wait},
		Refetch: func(context.Context, int64) *audio.StreamReply {
			return &audio.StreamReply{URLs: []string{srv.URL}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	waitFor(t, func() bool {
		_, ok := s.Reconnecting()
		return ok
	}, "源开始重试")
	return s
}

// TestTheReconnectingFlagReachesTheSnapshot 盯「源在等字节 → 界面上那一档亮」。
//
// 这一条盯的是**接线**：源的状态要真的走到 `State` 上（不是应用里另存一栏）。
func TestTheReconnectingFlagReachesTheSnapshot(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	if st := a.Snapshot(); st.Reconnecting {
		t.Fatalf("什么都没发生就说在重连：%+v", st)
	}

	src := retryingSource(t)
	a.mu.Lock()
	a.pendingSrc = src
	a.pendingSrcGen = a.onlineGen
	a.mu.Unlock()

	st := a.Snapshot()
	if !st.Reconnecting {
		t.Errorf("源在等字节回来，界面上却没有「正在重连」：%+v", st)
	}
	if st.ReconnectAttempt < 1 {
		t.Errorf("第几次重试 = %d，want ≥1", st.ReconnectAttempt)
	}
}

// TestOnlyTheSourceTheAppCaresAboutCounts 盯「应用问的是哪一条源」。
//
// 🔴 **这一条是评审复现的那个洞**：一条**建出来又被丢掉**（代次过期）的源，
// 它重试的时候**不许**点亮界面上那一档——抢走了的话，正在放的那首断流时，
// 界面上「正在重连」一次都不亮。
//
// 判据是「应用只问当前在管的那一条」：被丢掉的那条既不是 `pendingSrc`，
// 也不是 `online.source`。
func TestOnlyTheSourceTheAppCaresAboutCounts(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	kept := retryingSource(t)
	dropped := retryingSource(t)

	a.mu.Lock()
	a.pendingSrc = kept
	a.pendingSrcGen = a.onlineGen
	a.mu.Unlock()

	// 被丢掉的那条也在重试——它不该影响界面上的那一档。
	if _, ok := dropped.Reconnecting(); !ok {
		t.Fatal("被丢掉的那条源没有在重试，这条用例就验不到东西了")
	}
	if st := a.Snapshot(); !st.Reconnecting {
		t.Errorf("在管的那条源在重试，界面上却没有「正在重连」：%+v", st)
	}
}

// TestAStalePendingSourceDoesNotCount 盯**代次不对的那条不算**。
//
// 切歌之后，上一首歌那条**还在建**的源会留在 `pendingSrc` 上（它要到自己
// 建完才发现代次过期）。那一段的「正在重试」和用户眼前这一首没关系。
func TestAStalePendingSourceDoesNotCount(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	src := retryingSource(t)
	a.mu.Lock()
	a.pendingSrc = src
	a.pendingSrcGen = a.onlineGen - 1 // 上一代留下的
	a.mu.Unlock()

	if st := a.Snapshot(); st.Reconnecting {
		t.Errorf("上一代留下的源把「正在重连」点亮了：%+v", st)
	}
}

// TestTheReopenFromThePlaybackPositionSaysSo 盯**应用层自己那一档重连**。
//
// 「换了另一条流，接不上原来的字节」的时候，应用要从**播放位置**重开一首。
// 那一趟要走一次网络往返（重新取流 + 开解码器），几百毫秒到几秒——界面上
// 原来一个字都没有。
func TestTheReopenFromThePlaybackPositionSaysSo(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	a.beginReconnect(a.onlineGen)

	st := a.Snapshot()
	if !st.Reconnecting {
		t.Errorf("从播放位置重开的那一趟，界面上没有「正在重连」：%+v", st)
	}
	if st.ReconnectAttempt != 0 {
		t.Errorf("重开不是「第几次重试」，want 0，got %d", st.ReconnectAttempt)
	}

	// 重开这一趟结束了 → 那一档收掉（`endReconnect`）。
	a.endReconnect(a.onlineGen)
	if st := a.Snapshot(); st.Reconnecting {
		t.Errorf("重开结束了还说着「正在重连」：%+v", st)
	}
}

// TestClearingTheErrorDoesNotClearTheReconnecting 盯「装上新播放器不许抹掉重连」。
//
// 🔴 **这一条是评审复现的那个洞**：源刚点亮「正在重连」，`runOnline` 装好
// 播放器时 `clearErrLocked` 顺手把它清了——退避那一秒就又变成**完全静默**
// 的了，而歌正卡着（位置不动、没有声音）。
//
// 端到端的那一条在 `reconnect_e2e_test.go`（`TestTheBreakDuringTheFirstLoad
// IsStillVisible`）；这一条把机制本身钉死，不依赖时序。
func TestClearingTheErrorDoesNotClearTheReconnecting(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	src := retryingSource(t)
	a.mu.Lock()
	a.pendingSrc = src
	a.pendingSrcGen = a.onlineGen
	a.mu.Unlock()

	// 装上新播放器走的就是这一句（`runOnline` / `restreamOnline` / 重开成功）。
	a.clearErrLocked()

	if st := a.Snapshot(); !st.Reconnecting {
		t.Errorf("清错的时候把「正在重连」也抹了：%+v", st)
	}
}

// TestTheOldSongsReconnectStateDoesNotStayOnTheNewSong 盯「切歌之后旧流的状态
// 不留在新歌上」（票 #378）。
//
// 切歌之后 `a.online` 还是**旧那一首**——新播放器要等取流完了才装上，而
// 那之前旧的一直在放。旧那条流**可能正在重连**，那时候界面上「正在重连」
// 亮着的是**旧那一首**的状态，它把这一趟该说的「正在取流：新的一首」
// 盖掉了——用户看着像「一直在取流」，而实际是旧歌的状态赖在新歌上面。
//
// 🔴 **这一条钉的是代次守卫**：每一段流的状态各归各——代次不对的那一条源，
// 应用压根不去问它（理由见 `reconnect.go` 的 `reconnectingLocked`）。
//
// 端到端的那一条在 `reconnect_e2e_test.go`；这一条把机制本身钉死，不依赖
// 时序。
func TestTheOldSongsReconnectStateDoesNotStayOnTheNewSong(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	// 旧那一首：它的流正在重连（退避给得长，这一档亮着的时间才够断言）。
	src := retryingSourceWith(t, 30*time.Second)
	a.mu.Lock()
	a.online = &onlineTrack{item: Item{Title: "旧的那首"}, source: src, gen: a.onlineGen}
	a.mu.Unlock()

	if st := a.Snapshot(); !st.Reconnecting {
		t.Fatalf("旧那一首在重连，界面上却没有「正在重连」：%+v", st)
	}

	// 切歌：新的一趟开始取流（这一段就是 `playOnlineFrom` 干的事）。
	a.mu.Lock()
	a.onlineGen++
	a.loading = true
	a.loadingTitle = "新的一首"
	a.mu.Unlock()

	st := a.Snapshot()
	if st.Reconnecting {
		t.Errorf("旧那一首的重连状态留在了新歌上：%+v", st)
	}
	if !st.Loading {
		t.Errorf("新的一趟没有说「正在取流」：%+v", st)
	}
	if st.LoadingTitle != "新的一首" {
		t.Errorf("在取哪一首说错了：%q", st.LoadingTitle)
	}
}

// TestTheOldSongsReconnectStateDoesNotStealTheNewOnesLine 盯「新的一首自己在
// 重连时，那一档说的是**它自己**」（票 #378 的另一半）。
//
// 切歌之后新的一首也要取流，而它自己那条流**也可能**在重连（取流的那一趟
// 里地址就失效了）。那时候界面上该说的是新的一首的重连状态——不是旧那一首
// 的，也不是没有。
//
// 🔴 **代次守卫不许把话说反**：问的是**当前这一趟**的那一条源。
func TestTheOldSongsReconnectStateDoesNotStealTheNewOnesLine(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	// 旧那一首：它的流正在重连。
	old := retryingSourceWith(t, 30*time.Second)
	a.mu.Lock()
	a.online = &onlineTrack{item: Item{Title: "旧的那首"}, source: old, gen: a.onlineGen}
	a.onlineGen++
	a.loading = true
	a.loadingTitle = "新的一首"
	// 新的一首正在建的那条源：它也在重连（这一趟自己的）。
	newSrc := retryingSourceWith(t, 30*time.Second)
	a.pendingSrc = newSrc
	a.pendingSrcGen = a.onlineGen
	a.mu.Unlock()

	st := a.Snapshot()
	if !st.Reconnecting {
		t.Errorf("新的一首自己在重连，界面上却没有「正在重连」：%+v", st)
	}
	if st.ReconnectAttempt < 1 {
		t.Errorf("第几次重试 = %d，want ≥1", st.ReconnectAttempt)
	}
}

// TestTheSecondReopenSaysItGaveUp 盯「第二次重开被挡掉」那一支。
//
// 一首歌只重开一次（`onlineTrack.reopening`）。第二次还不行就该**如实报错**。
//
// 🔴 **以前这里是直接 return**（票 #315 的评审复现过）：界面上「正在重连」
// 那一档**永远亮着**、一句红字也没有，而歌早就停了。用户看到的「正在重连」
// 是**假的**——它压根没有在重连，它是放弃了。
//
// 端到端的那一条在 `reconnect_e2e_test.go`（`TestTheSecondReopenSaysItGaveUp`）；
// 这一条把机制本身钉死，不依赖时序。
func TestTheSecondReopenSaysItGaveUp(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	a.mu.Lock()
	a.online = &onlineTrack{item: Item{Title: "换过两次的那一首"}, reopening: true}
	a.mu.Unlock()

	a.refreshOnlineNow(errors.New("audio: 换流换到了另一条流"))

	st := a.Snapshot()
	if st.Err == "" {
		t.Error("重开不了却一句话都没有——用户看到的是「歌停了、界面说正在重连」")
	}
	if st.Reconnecting {
		t.Errorf("已经放弃了还说着「正在重连」：%+v", st)
	}
}
