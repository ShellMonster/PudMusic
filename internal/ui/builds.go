package ui

// buildTally 记着「各个界面一共建了几遍」。
//
// # 它是干什么的
//
// 「一帧建两遍」是**静默**的坏处：画面上看着没差（两遍画的是同一块东西，
// 后建的盖住先建的），而帧成本翻倍——用户能感到的是「界面有点粘」。
//
// 已知的一处：播放列表抽屉在满屏播放器开着的时候**由满屏那一支建**
// （`viewFullPlayer` 末尾那一句），而 `viewMain` 必须让开。让开的那一句
// 一旦被删掉，**没有任何东西会报错**——两遍建出来的抽屉长得一模一样。
//
// # 为什么它住在生产代码里
//
// 「建了几遍」只能在**画它的那一处**数出来：谁画它谁才知道。而那一处就是
// 生产代码里的 `viewHome` / `viewPlayBar` / `viewQueueDrawer` /
// `viewFullPlayer`——把它们挪到测试里，就成了「测试里那一遍」，而要抓的
// 恰恰是**两个调用方都建了它**。
//
// 代价是每帧四次整数自增（可忽略），换来的是一条**会红的**测试——
// 见 framebudget_test.go。
//
// ⚠️ **它只增不减**：生产里没有人读它；测试在量之前自己清零（这一份是
// 挂在窗口上的，所以清零就是 `s.builds = buildTally{}`）。
type buildTally struct {
	// home 是首页那一次（`viewHome`）。
	home int
	// playbar 是播放条那一次（`viewPlayBar`）。
	playbar int
	// drawer 是播放列表抽屉那一次（`viewQueueDrawer`）。
	drawer int
	// full 是全屏播放器那一次（`viewFullPlayer`）。
	full int
}
