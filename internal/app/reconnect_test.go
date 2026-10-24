package app

// 这一组盯的是「正在重连」这一档在**应用层**的接线（票 #315）。
//
// # 为什么这一档要有
//
// 断流之后应用在救这一首（换地址 / 换流 / 从播放位置重开），而这一档以前
// 界面上一个字都没有——用户看到的是「歌停着、界面不动、没有字」。
//
// 这一组钉四件事：
//
//	字节源说「我要重试了」→ 那一档亮起来，而且说得出是第几次
//	字节回来了 / 出错了 → 那一档收掉
//	**迟到的回调不许碰界面上的状态**（旧流的回调会擦掉新流的那一行）
//	代次不对的回调不许碰（那是上一首歌的事）

import (
	"errors"
	"testing"
)

// TestTheReconnectingFlagReachesTheSnapshot 盯「字节源说要重试 → 界面知道」。
func TestTheReconnectingFlagReachesTheSnapshot(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	if st := a.Snapshot(); st.Reconnecting {
		t.Fatalf("什么都没发生就说在重连：%+v", st)
	}

	token := a.newReconnectToken()
	a.noteReconnect(a.onlineGen, token, 2)

	st := a.Snapshot()
	if !st.Reconnecting {
		t.Errorf("字节源说要重试，界面上却没有「正在重连」：%+v", st)
	}
	if st.ReconnectAttempt != 2 {
		t.Errorf("第几次重试 = %d，want 2", st.ReconnectAttempt)
	}
}

// TestTheBytesComingBackTurnsTheReconnectingFlagOff 盯**成功那一半**。
//
// 「重连成功」在界面上就是「那一行不见了、歌接着响」——不另给一句提示
// （那是噪音：成功了就继续播，用户要的是歌）。
func TestTheBytesComingBackTurnsTheReconnectingFlagOff(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	token := a.newReconnectToken()
	a.noteReconnect(a.onlineGen, token, 1)
	a.noteReconnected(a.onlineGen, token)

	if st := a.Snapshot(); st.Reconnecting {
		t.Errorf("字节都回来了还说着「正在重连」：%+v", st)
	}
}

// TestAnErrorTurnsTheReconnectingFlagOff 盯**失败那一半**。
//
// 出错之后那一行必须收掉：错的那句和「正在重连」占同一个位置，而错的那句
// 信息量更大——用户要看到的是「为什么不行、还能怎么办」。
func TestAnErrorTurnsTheReconnectingFlagOff(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	token := a.newReconnectToken()
	a.noteReconnect(a.onlineGen, token, 1)
	a.setErrText("在线流拉不下来了", true)

	st := a.Snapshot()
	if st.Reconnecting {
		t.Errorf("已经报错了还说着「正在重连」：%+v", st)
	}
	if st.Err == "" {
		t.Error("那句话没上屏")
	}
}

// TestALateCallbackFromTheOldStreamDoesNotTouchTheNewOne 盯**迟到的回调**。
//
// 换音质 / 换流重开会建一条新的字节源，而旧源可能还在退避里等着——它的
// 「字节回来了」回调在这之后才到。不看令牌的话，旧流会把新流的「正在重连」
// 那一行擦掉：用户正等着重连，那一行却忽然没了。
func TestALateCallbackFromTheOldStreamDoesNotTouchTheNewOne(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	old := a.newReconnectToken()
	a.noteReconnect(a.onlineGen, old, 1)

	// 换了一条流：令牌换了。
	fresh := a.newReconnectToken()
	a.noteReconnect(a.onlineGen, fresh, 1)

	// 旧源的回调迟到了。
	a.noteReconnected(a.onlineGen, old)

	if st := a.Snapshot(); !st.Reconnecting {
		t.Errorf("旧流的回调把新流的「正在重连」擦掉了：%+v", st)
	}
}

// TestACallbackFromAnotherGenerationIsIgnored 盯**代次不对的回调**。
//
// 切歌之后，上一首歌的源回调才回来是常事——它不许碰界面上的状态。
func TestACallbackFromAnotherGenerationIsIgnored(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	token := a.newReconnectToken()
	a.noteReconnect(a.onlineGen+1, token, 1)

	if st := a.Snapshot(); st.Reconnecting {
		t.Errorf("上一代的回调把这一代的状态改了：%+v", st)
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

// TestANewStreamClearsTheOldOnesRetryState 盯「新的一段流顶上来」。
//
// 旧源要么被关了、要么被换掉了——它的「正在重试」到此为止。不收的话那一行
// 会一直亮着，而那一段流早就没人管了。
//
// ⚠️ **只收源那一半**：应用层那一半（「正在从播放位置重开」）正开着呢——
// `refreshOnlineNow` 是先 `beginReconnect` 再建这条流的。
func TestANewStreamClearsTheOldOnesRetryState(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	old := a.newReconnectToken()
	a.noteReconnect(a.onlineGen, old, 1)
	a.beginReconnect(a.onlineGen)

	// 新的一段流顶上来了。
	a.newReconnectToken()

	st := a.Snapshot()
	if st.ReconnectAttempt != 0 {
		t.Errorf("旧那一段的「正在重试」没收掉：%+v", st)
	}
	if !st.Reconnecting {
		t.Errorf("「正在从播放位置重开」那一半被顺手收掉了：%+v", st)
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

	token := a.newReconnectToken()
	a.noteReconnect(a.onlineGen, token, 1)

	// 装上新播放器走的就是这一句（`runOnline` / `restreamOnline` / 重开成功）。
	a.clearErrLocked()

	if st := a.Snapshot(); !st.Reconnecting {
		t.Errorf("清错的时候把「正在重连」也抹了：%+v", st)
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
	// 源刚刚报过「我要重试」——这一档正亮着。
	a.reconnecting = true
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
