package app

// 这一份量的是「播放器的时钟每一拍要不要叫醒界面」。
//
// # 背景：两个时钟被接成了一根
//
// `tick` 每 `tickInterval`（50ms）醒一次，那是对的：位置要采样、切歌淡出要看
// 剩余时长、播放记录要按拍对账、上次听到哪儿要按节奏落盘。**这些都是内部
// 状态，没有一样需要屏幕跟着动。**
//
// 而它每一拍还会叫醒每一扇可见的窗口（`wakeAll` → 整棵树重建）。于是屏幕上
// 真在变的东西只有进度条的读数（一秒一变）和那根线的位置，代价是每秒 20 次
// 整窗重建。
//
// 上游 mygo-cloudmusic 的 `tickTimes` 是反过来的：
//
//	func (a *app) tickTimes(c *ui.Context) {
//	    if a.pl.Playing() && a.pl.Ready() {
//	        pos := a.pl.Position()
//	        c.After(time.Second - pos%time.Second + 10*time.Millisecond)
//	    }
//	}
//
// 只在下一次秒变化前要一帧。这一份要把同一个口径搬过来。
//
// # 这一条为什么是纯函数
//
// 「该不该叫醒」是**一次判断**：这一拍读到的位置和上一拍报出去的位置，读数
// 变了没有。它不需要窗口、不需要 ticker、不靠时间——于是它能在满载的机器上
// 一直给出同一个答案（AGENTS §2：不拿固定毫秒当判据）。

import (
	"testing"
	"time"
)

// TestTheWakeFollowsTheReadoutNotTheClock 钉住「唤醒跟着读数走」。
//
// 三档，按「屏幕上看得见什么」分：
//
//   - 同一秒里从 10.0s 走到 10.9s：读数还是「0:10」，**不该唤醒**；
//   - 跨过 11.0s：读数变成「0:11」，**该唤醒**；
//   - 不在放（暂停、停住）：位置不动，**不该唤醒**。
//
// ⚠️ **第三档是这条函数存在的理由之一**：暂停时 `live` 在 `tick` 里已经是假，
// 但那个 `live` 是 `wantPlay || playing`——用户按了暂停之后 `wantPlay` 落成
// false、`playing` 也是 false，于是本来就不叫。真正要挡的是**第二种**：一帧
// 里位置跨了整秒、而播放器其实已经停住（缓冲、卡死）——那时读数没变，却因为
// 「位置变了」叫了一帧。
func TestTheWakeFollowsTheReadoutNotTheClock(t *testing.T) {
	for _, tc := range []struct {
		name string
		prev time.Duration
		now  time.Duration
		want bool
	}{
		{"同一秒里挪了一点", 10*time.Second + 100*time.Millisecond, 10*time.Second + 950*time.Millisecond, false},
		{"跨过整秒", 10*time.Second + 900*time.Millisecond, 11*time.Second + 50*time.Millisecond, true},
		{"秒边界上挪动", 10 * time.Second, 10*time.Second + 50*time.Millisecond, false},
		{"位置没动", 42 * time.Second, 42 * time.Second, false},
		{"从零开始", 0, 0, false},
		{"从零跨到一秒", 0, 1050 * time.Millisecond, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := readoutMoved(tc.prev, tc.now); got != tc.want {
				t.Errorf("readoutMoved(%v, %v) = %v，want %v——"+
					"界面只该在**读数**（秒数）变的时候重建，"+
					"跟着播放器每一拍走是每秒 20 次整窗重建", tc.prev, tc.now, got, tc.want)
			}
		})
	}
}

// TestTheReadoutIsWhatThePlayBarShows 钉住 `readoutMoved` 那个判据和界面
// **真的画出来的那个数**是同一个。
//
// # 为什么要单独钉
//
// `readoutMoved` 判的是 `pos/time.Second` 变了没有，而界面画的是
// `clockOrDash(position)`（`internal/ui/playbar.go`）。两者只要有一边换了
// 口径（比如界面改成把毫秒显示出来、或者改成四舍五入），这一条就会红——
// 而那正是「界面该刷新的时候没刷」或「刷了但没变」的根。
//
// 这里的本分是**把口径写下来**：`clockOrDash` 是截断到秒（`0:10.9` 画成
// `0:10`），所以判据也必须是截断。
func TestTheReadoutIsWhatThePlayBarShows(t *testing.T) {
	// 10.999s 在界面上是「0:10」——和 10.0s 画出来一模一样。
	if got := readoutMoved(10*time.Second, 10999*time.Millisecond); got {
		t.Error("10.0s → 10.999s 报「读数变了」，而界面这两者都画成 0:10——" +
			"判据和界面的口径对不上，白刷的那一帧就是这么来的")
	}
	// 11.0s 才是「0:11」。
	if !readoutMoved(10999*time.Millisecond, 11*time.Second) {
		t.Error("10.999s → 11.0s 报「读数没变」，而界面从 0:10 变成 0:11——" +
			"这一帧不刷，用户看到的是卡住的进度")
	}
}
