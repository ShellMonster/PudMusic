package app

// 「掐掉在途的字节源」只许掐**自己那一趟**的。
//
// # 这条盯的是真缺陷（2026-10-30 从 CI 日志里查出来的）
//
// CI 上 `TestOnlyTheLastTrackWinsWhenSwitchingFast` 偶发 30 秒超时，日志里有
// 这一行：
//
//	播放失败："第 5 首"（开播放器，段=下音频，代次 5）：audio: 字节源已关闭
//
// 第 5 首是**最新**的那一趟（代次 5 就是当时的 `onlineGen`），它自己的源却在
// 建到一半时被**别人**关掉了。
//
// 关它的是 `abortPendingSource`：那是「这一趟不要了，把它还在下的字节源掐掉」
// 用的，而它**无条件**关当前的 `pendingSrc` / `loadSrc`，不看它们属于哪一趟。
// 连点几首时，前几首的失败清理（超时闹钟、切歌、停止）就会把**新歌正在建的**
// 那条源一起关掉——用户看到的是「点了歌，报一句字节源已关闭」，而第 5 首永远
// 起不来，`title` 就一直停在「第 4 首」。
//
// ⚠️ **判据是 App 上那两栏**（`pendingSrc` / `loadSrc` 还指着谁），不是去读
// 字节源内部：源关没关只有它自己知道，而「还挂在栏上、代次对得上」正是
// 后面那条超时/重连路径能管到它的前提。

import (
	"testing"
	"time"
)

// TestAbortingAnOldLoadKeepsTheNewSourceOnTheBooks 钉住「旧的清理碰不到新的源」。
//
// 时序（和 CI 上那条一样，只是不用等 20 秒超时）：
//
//	第 1 趟建好源 A（代次 1）
//	第 2 趟开始（代次 2），源 B 挂上来
//	第 1 趟的清理跑起来（它的超时闹钟响了 / 用户按了停止）
//	→ 源 B **还挂在栏上**，而且仍然是第 2 趟的那一条
func TestAbortingAnOldLoadKeepsTheNewSourceOnTheBooks(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	a.mu.Lock()
	a.onlineGen = 1
	a.mu.Unlock()

	srcA := retryingSourceWith(t, 10*time.Second)
	if !a.setPendingSource(srcA, 1) {
		t.Fatal("源 A 没挂上（代次 1）")
	}

	// 第 2 趟开始：代次涨到 2，源 B 挂上来。
	a.mu.Lock()
	a.onlineGen = 2
	a.mu.Unlock()
	srcB := retryingSourceWith(t, 10*time.Second)
	if !a.setPendingSource(srcB, 2) {
		t.Fatal("源 B 没挂上（代次 2）")
	}

	// 第 1 趟的清理跑起来（按它的代次 1）。
	a.abortPendingSourceFor(1)

	a.mu.Lock()
	got, gotGen := a.pendingSrc, a.pendingSrcGen
	a.mu.Unlock()
	if got != srcB || gotGen != 2 {
		t.Errorf("旧一趟的清理把新一趟的源摘掉了（还挂着的是 %v，代次 %d）——"+
			"CI 上就是这么让第 5 首报「字节源已关闭」、然后一直起不来的", got != nil, gotGen)
	}
}

// TestAbortingTheCurrentLoadClosesItsSource 钉住「自己那一趟照样掐得掉」。
//
// ⚠️ **这一条是上一条的反面**：只按代次放行、把清理改成什么都不关的话，
// 超时的那一趟会一直下到天亮（那正是它当初被加上的理由）。
func TestAbortingTheCurrentLoadClosesItsSource(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	a.mu.Lock()
	a.onlineGen = 7
	a.mu.Unlock()

	src := retryingSourceWith(t, 10*time.Second)
	if !a.setPendingSource(src, 7) {
		t.Fatal("源没挂上")
	}
	a.abortPendingSourceFor(7)

	a.mu.Lock()
	got := a.pendingSrc
	a.mu.Unlock()
	if got != nil {
		t.Error("这一趟自己的源还挂在栏上——超时之后它会一直下到天亮")
	}
	// 而且它确实被关了：关过的源再读会报 `ErrSourceClosed`。
	if _, err := src.Read(make([]byte, 1)); err == nil {
		t.Error("这一趟自己的字节源没被关掉")
	}
}
