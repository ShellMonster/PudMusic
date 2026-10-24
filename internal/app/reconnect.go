// 「正在重连」这一档：断流之后应用在做什么，以及界面上看得见的那句话。
//
// # 为什么要有这一档
//
// 断流之后有三件事同时发生，而以前**界面上一个字都没有**：
//
//	字节源在后台换地址 / 换流（`internal/service/audio` 的 `recover`）
//	应用在从播放位置重开一首（`refreshOnlineNow`）
//	用户对着一个不动的界面等
//
// 于是用户看到的是「歌停着、界面不动、没有字」，等多久都像应用死了
// （票 #315）。加上退避之后更糟：那 7 秒的等待原本是**完全静默**的。
//
// # 这一档和「正在取流」是两件事
//
//	Loading		这一首**还没放起来**（第一次点它）
//	Reconnecting	这一首**响过一半**，流断了，正在救
//
// 用户看到的差别很大，而两者的等待上限也不同：取流是 20 秒
// （`onlinePlayTimeout`），重连是「退避 7 秒 + 一次换流往返」。
//
// # 🔴 **这一档有两半，亮与灭各管各的**
//
// 这一档是**两件互不相干的事**的并集：
//
//	a.reconnecting	**字节源**正在重试（它在退避里等着，或者刚失败）
//	a.reopenInFlight	**应用**正在从播放位置重开（重新取流 + 开解码器）
//
// 两半各有各的亮和灭，**不能互相抹**：
//
//	🔴 **装上新播放器不许抹掉源那一半**。源在退避里等着的时候，播放器
//	装好了——可**字节还没回来**（那正是断流的样子：歌停着、位置不动）。
//	这时候把「正在重连」收掉，用户看到的就是「界面什么都不说、进度条不动」
//	——那恰恰是这一档要消灭的形态（票 #315 的评审复现过：源刚点亮这一档，
//	`runOnline` 装播放器时顺手清了，退避那一秒完全静默）。
//
//	🔴 **新的一段流顶上来的时候要抹掉旧那一半**：旧源要么被关了、要么被
//	换掉了，它的「正在重试」到此为止（`newReconnectToken`）。
//
//	🔴 **报错要抹掉两半**：一句错比一枚转圈信息量大（`setErrTextDetail`）。
//
// # 谁点亮它、谁收掉它
//
//	点亮	字节源说「我要重试了」（`noteReconnect`）
//	点亮	应用要从播放位置重开（`beginReconnect`）
//	收掉	字节回来了（`noteReconnected`）
//	收掉	重开这一趟结束了（`endReconnect`）
//	收掉	新的一段流顶上来（`newReconnectToken`）
//	收掉	出错了（`setErrTextDetail`）——错的那句信息量更大
//	收掉	换歌 / 停了 / 放本地文件了（`playOnline` / `ClearTrack` / `Open`）
//
// ⚠️ **「收掉」比「点亮」多**：一档只会亮不会灭的状态比没有更坏——它会
// 让用户以为应用一直在重连，而实际上它早就放弃了（票 #315 的评审复现过：
// 第二次重开被 `reopening` 挡掉时，那一档永远亮着，而一句红字也没有）。

package app

// setReconnectLocked 把「正在重连」这一档的三个值一起落。
//
// ⚠️ **三个值必须一起落**：只落 `reconnecting` 而留着 `reconnectAttempt`，
// 界面上就会说「第 0 次」——而 0 是「不是重试那一档」的记号。
//
// 返回「变了没有」——变了才要叫醒界面（没变就叫醒是白要一帧）。
//
// **必须在持锁时调**。
func (a *App) setReconnectLocked(reconnecting bool, attempt int, reopen bool) bool {
	changed := a.reconnecting != reconnecting ||
		a.reconnectAttempt != attempt ||
		a.reopenInFlight != reopen
	a.reconnecting = reconnecting
	a.reconnectAttempt = attempt
	a.reopenInFlight = reopen
	return changed
}

// noteReconnect 记下「字节源正在重试」（第 attempt 次）。
//
// 它是**字节源**回调上来的：源在退避开始之前就说了「我要重试」，所以
// 界面上从第一秒起就亮着「正在重连」。
//
// ⚠️ **代次和令牌都要对**：代次不对是「上一首歌的事」（切歌了），令牌
// 不对是「上一条流的事」（换音质 / 换流重开建了新的源，而旧源的回调迟到
// 了）。两种迟到都不能碰界面上的状态。
func (a *App) noteReconnect(gen, token, attempt int) {
	a.mu.Lock()
	if token == 0 || a.onlineGen != gen || a.closed || a.reconnectToken != token {
		a.mu.Unlock()
		return
	}
	changed := a.setReconnectLocked(true, attempt, a.reopenInFlight)
	wake := a.wake
	a.mu.Unlock()
	if changed && wake != nil {
		wake()
	}
}

// noteReconnected 收掉「字节源正在重试」这一半：字节又回来了。
//
// ⚠️ **它不报「这一趟成功了」**：换流可能换到了另一条流（那要上层重新开
// 解码器并跳回播放位置），而这里只说明「连接恢复了，字节在来了」。
//
// ⚠️ **只收源那一半**：应用层那一半（「正在从播放位置重开」）是另一件事，
// 它归 `endReconnect` 管。
func (a *App) noteReconnected(gen, token int) {
	a.mu.Lock()
	if token == 0 || a.onlineGen != gen || a.closed || a.reconnectToken != token {
		a.mu.Unlock()
		return
	}
	changed := a.setReconnectLocked(false, 0, a.reopenInFlight)
	wake := a.wake
	a.mu.Unlock()
	if changed && wake != nil {
		wake()
	}
}

// beginReconnect 记下「正在从原位置重开」。
//
// 这是**应用层**的重连：字节流换了另一条（`ErrStreamChanged`），接不上
// 原来的字节偏移，只能从**播放位置**重开。它同样要走一趟网络（重新取流
// + 开解码器），界面上一个字都没有的话用户看到的就是「歌停着、界面不动」。
func (a *App) beginReconnect(gen int) {
	a.mu.Lock()
	if a.onlineGen != gen || a.closed {
		a.mu.Unlock()
		return
	}
	changed := a.setReconnectLocked(a.reconnecting, a.reconnectAttempt, true)
	wake := a.wake
	a.mu.Unlock()
	if changed && wake != nil {
		wake()
	}
}

// endReconnect 收掉「正在从原位置重开」这一半：这一趟结束了。
//
// ⚠️ **代次要对**：代次变了说明用户已经切歌了，而**新**的那一趟可能正在
// 重连——把它的收掉就是「界面说正在重连，而它已经不重连了」。
func (a *App) endReconnect(gen int) {
	a.mu.Lock()
	if a.onlineGen != gen || a.closed {
		a.mu.Unlock()
		return
	}
	changed := a.setReconnectLocked(a.reconnecting, a.reconnectAttempt, false)
	wake := a.wake
	a.mu.Unlock()
	if changed && wake != nil {
		wake()
	}
}

// clearReconnectLocked 把「正在重连」这一档**整个**收掉（两半都收）。
//
// **必须在持锁时调**。用在「这一首已经不是刚才那一首了」的那些地方：
// 换歌、停了、放本地文件了。
//
// ⚠️ **那些地方本来就会叫醒界面**（它们各自都有一次 wake），所以这里不用
// 再报「变了没有」。
func (a *App) clearReconnectLocked() {
	a.setReconnectLocked(false, 0, false)
}

// newReconnectToken 给一条新的在线字节源发一个重连令牌。
//
// **每一段流一个**：旧源的回调（它可能还在退避里等着）回来的时候令牌已经
// 变了，就不再碰界面上的状态——不然「上一条流的恢复」会把「这一条流正在
// 重连」那一行擦掉。
//
// ⚠️ **令牌从 1 起**：0 是「没有令牌」，回调拿着 0 一律不认（`noteReconnect`
// / `noteReconnected` 里各判一次）——不然一个还没建过源的 App 会把第一个
// 回调当成自己的。
//
// 🔴 **顺手把源那一半收掉**：新的一段流顶上来了，**旧**那一段要么被关了、
// 要么被换掉了——它的「正在重试」到此为止。不收的话它会一直亮着，而那一段
// 流早就没人管了。
//
// ⚠️ **只收源那一半**：应用层那一半（「正在从播放位置重开」）正开着呢——
// `refreshOnlineNow` 是先 `beginReconnect` 再建这条流的。
func (a *App) newReconnectToken() int {
	a.mu.Lock()
	a.reconnectSeq++
	a.reconnectToken = a.reconnectSeq
	changed := a.setReconnectLocked(false, 0, a.reopenInFlight)
	wake := a.wake
	a.mu.Unlock()
	if changed && wake != nil {
		wake()
	}
	return a.reconnectSeq
}
