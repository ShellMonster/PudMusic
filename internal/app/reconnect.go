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
// # 谁点亮它、谁收掉它
//
//	点亮	字节源说「我要重试了」（`noteReconnect`）
//	点亮	应用要从播放位置重开（`beginReconnect`）
//	收掉	字节回来了（`noteReconnected`）
//	收掉	这一首已经在响了（`clearErrLocked` 那一路）
//	收掉	出错了（`setErrTextDetail`）——错的那句信息量更大
//
// ⚠️ **「收掉」比「点亮」多**：一档只会亮不会灭的状态比没有更坏——它会
// 让用户以为应用一直在重连，而实际上它早就放弃了。

package app

// noteReconnect 记下「正在重连」（第 attempt 次重试）。
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
	changed := !a.reconnecting || a.reconnectAttempt != attempt
	a.reconnecting = true
	a.reconnectAttempt = attempt
	wake := a.wake
	a.mu.Unlock()
	if changed && wake != nil {
		wake()
	}
}

// noteReconnected 收掉「正在重连」这一档：字节又回来了。
//
// ⚠️ **它不报「这一趟成功了」**：换流可能换到了另一条流（那要上层重新开
// 解码器并跳回播放位置），而这里只说明「连接恢复了，字节在来了」。
//
// 成功那一半由调用方管（装上新播放器 → `clearErrLocked`）。
func (a *App) noteReconnected(gen, token int) {
	a.mu.Lock()
	if token == 0 || a.onlineGen != gen || a.closed || a.reconnectToken != token {
		a.mu.Unlock()
		return
	}
	changed := a.reconnecting
	a.clearReconnectingLocked()
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
	changed := !a.reconnecting
	a.reconnecting = true
	// 这一档不是「第几次重试」——它是**换流重开**本身，次数是 0。
	a.reconnectAttempt = 0
	wake := a.wake
	a.mu.Unlock()
	if changed && wake != nil {
		wake()
	}
}

// clearReconnectingLocked 收掉「正在重连」这一档。**必须在持锁时调**。
func (a *App) clearReconnectingLocked() {
	a.reconnecting = false
	a.reconnectAttempt = 0
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
func (a *App) newReconnectToken() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reconnectSeq++
	a.reconnectToken = a.reconnectSeq
	return a.reconnectSeq
}
