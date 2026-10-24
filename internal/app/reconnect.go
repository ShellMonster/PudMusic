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
// # 🔴 这一档的**两半**，亮与灭各管各的
//
// 这一档是**两件互不相干的事**的并集：
//
//	源那一半	**字节源**正在等字节回来（它在退避里等着）
//	应用那一半	**应用**正在从播放位置重开（重新取流 + 开解码器）
//
// 两半各有各的亮和灭，**不能互相抹**：
//
//	🔴 **装上新播放器不许抹掉源那一半**。源在退避里等着的时候，播放器
//	装好了——可**字节还没回来**（那正是断流的样子：歌停着、位置不动）。
//	这时候把「正在重连」收掉，用户看到的就是「界面什么都不说、进度条不动」
//	——那恰恰是这一档要消灭的形态（票 #315 的评审复现过：源刚点亮这一档，
//	`runOnline` 装播放器时顺手清了，退避那一秒完全静默）。
//
//	🔴 **报错要抹掉应用那一半**：一句错比一枚转圈信息量大，而它们占**同一
//	个位置**（播放条上只有一行宽）。
//
// # 🔴 源那一半**不住在这里**
//
// 它住在源那边（`audio.StreamSource.Reconnecting()`），应用只**问**当前在管
// 的那一两条流。为什么不能在这儿存一栏——票 #315 的评审复现过：
//
//	一条建出来又被丢掉（代次过期）的源把那一栏抢走 → 正在放的那首断流时，
//	它的重试被当成「别的流的事」丢掉 → 界面上「正在重连」**一次都不亮**
//
// 住在源那边就没有「抢」这回事：应用压根不读那条被丢掉的源的状态。
//
// 🔴 **但「当前在管」要问代次**（票 #378）：切歌之后 `a.online` 还是**旧
// 那一首**（新播放器要等取流完了才装上），而旧那条流可能正在重连——
// 那时候界面上「正在重连」亮着的是**旧那一首**的状态，它把这一趟该说的
// 「正在取流：新的一首」盖掉了。每一段流的状态各归各：代次不对的不问。
//
// # 谁点亮它、谁收掉它
//
//	点亮	源说「我要重试了」（`OnRetry` → 叫醒界面）
//	点亮	应用要从播放位置重开（`beginReconnect`）
//	收掉	字节回来了（`OnReconnected` → 叫醒界面）
//	收掉	源放弃 / 被关了（泵退出 → `Reconnecting()` 变成 false）
//	收掉	重开这一趟结束了（`endReconnect`）
//	收掉	出错了（`setErrTextDetail`）——错的那句信息量更大
//	收掉	换歌 / 停了 / 放本地文件了（`playOnline` / `ClearTrack` / `Open`）
//
// ⚠️ **「收掉」比「点亮」多**：一档只会亮不会灭的状态比没有更坏——它会
// 让用户以为应用一直在重连，而实际上它早就放弃了（票 #315 的评审复现过：
// 第二次重开被 `reopening` 挡掉时，那一档永远亮着，而一句红字也没有）。

package app

// reconnectingLocked 报「当前在管的那两条流里，有没有在等字节回来的」。
//
// **问的是源，不是这里存的一栏**——理由见文件头那一节。
//
// 问的是两条：
//
//	`pendingSrc`	正在建的那条（还没装上）
//	`online.source`	正在放的那条
//
// 🔴 **`pendingSrc` 要对代次**：切歌之后，上一首歌那条**还在建**的源会留在
// `pendingSrc` 上（它要到自己建完才发现代次过期）。那一段的「正在重试」
// 和用户眼前这一首没关系——不问代次的话，界面上会亮起一盏**别人的**指示灯。
//
// 🔴 **`online.source` 也要对代次**（票 #378）：切歌之后 `a.online` 还是
// **旧那一首**——新播放器要等取流完了才装上，而那之前旧的一直在放。
// 旧那条流**可能正在重连**，那时候界面上「正在重连」亮着的是**旧那一首**
// 的状态，它把这一趟该说的「正在取流：新的一首」盖掉了——用户看着像
// 「一直在取流」，而实际是旧歌的状态赖在新歌上面。
//
// ⚠️ **代次不对就不问**：那一段流的状态属于**别的**那一趟，而那一趟的
// 话已经在界面上说过了（或者是它自己的错）。
//
// **必须在持锁时调**（`a.mu`）。它会再去拿源的锁（`s.mu`）——那边不会反手
// 拿 `a.mu`（源的回调都在**不持** `s.mu` 的时候调），所以不会死锁。
func (a *App) reconnectingLocked() (attempt int, retrying bool) {
	if a.pendingSrc != nil && a.pendingSrcGen == a.onlineGen {
		if at, ok := a.pendingSrc.Reconnecting(); ok {
			return at, true
		}
	}
	if a.online != nil && a.online.source != nil && a.online.gen == a.onlineGen {
		if at, ok := a.online.source.Reconnecting(); ok {
			return at, true
		}
	}
	return 0, false
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
	changed := !a.reopenInFlight
	a.reopenInFlight = true
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
	changed := a.reopenInFlight
	a.reopenInFlight = false
	wake := a.wake
	a.mu.Unlock()
	if changed && wake != nil {
		wake()
	}
}

// clearReconnectLocked 收掉「正在重连」这一档的**应用那一半**。
//
// **必须在持锁时调**。用在「这一首已经不是刚才那一首了」的那些地方：
// 换歌、停了、放本地文件了。
//
// ⚠️ **源那一半不用管**：它住在源那边（`Reconnecting()`），应用只问当前在管
// 的那一条流——而**代次不对的那一条不算**（票 #378）：换歌之后问到的就是
// 新那一首的源，旧那一首的状态归它自己（见 `reconnectingLocked`）。
func (a *App) clearReconnectLocked() {
	a.reopenInFlight = false
}

// wakeUp 叫醒界面：状态变了，要一帧。
//
// ⚠️ **不持 `a.mu` 调**：它是给源的回调用的，而源的回调从任何 goroutine 过来
// （下载泵）。
func (a *App) wakeUp() {
	a.mu.Lock()
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
	}
}
