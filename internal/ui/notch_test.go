package ui

// 刘海窗的开合状态机与窗口开关。真窗那半边（NSWindow 配置、CGEvent tap）
// 只有真机能验，这里钉的是**不靠窗口也成立**的那一半：
//
//   - `openNotchPlayer`/`closeNotchPlayer` 对外壳的开/关；
//   - `notchTick` 那个「指针在不在触发区」的状态机（进出时间、300ms/350ms
//     两个宽限）；
//   - 「刘海那一侧的 `s.notch` 是 nil」这条边上，`toggleNotchPlayer` 会绕回
//     主窗口（同 `toggleMiniPlayer` 的票 #… 那个坑）。
//
// ⚠️ 「刘海窗的 `notchSt` 长在外壳自己身上」是这一组的地基（`notch.go`
// 里 `notchTick` 那一段注释）：tap 那边写、这里这边读，两件事一个外壳。

import (
	"testing"
	"time"
)

// notchShell 建一个刘海外壳 + 它的主窗外壳，不建真窗（无头 Tester 里
// `mygo.NewWindow` 会 panic——和 `newMiniShell` 同一个理由）。
func newNotchShell(t *testing.T) (*Shell, *Shell) {
	t.Helper()
	sh := newTestShell(t)
	main := sh.s
	n := main.newNotchShell()
	t.Cleanup(n.closePerWindow)
	return n, main
}

// TestNotchPlayerOpensAndClosesTheShell 钉「开 / 关那一个外壳」。
func TestNotchPlayerOpensAndClosesTheShell(t *testing.T) {
	main := newTestShell(t).s
	if main.notch != nil {
		t.Fatal("刚开始刘海不该开着")
	}
	n := main.newNotchShell()
	if main.notch != n || n.main != main {
		t.Fatal("刘海和主窗口没接上")
	}
	if !main.dropNotch() {
		t.Fatal("dropNotch 说没有刘海")
	}
	if main.notch != nil {
		t.Fatal("关掉之后主窗口还指着那个外壳")
	}
	// 再调一遍是空操作，不崩。
	if main.dropNotch() {
		t.Fatal("第二个 dropNotch 不该说开着")
	}
}

// TestToggleNotchPlayerFromTheNotchSide 钉「小窗那一侧按这个键会绕回主窗口」
// （同 `toggleMiniPlayer` 顶上那一段的理由：那一侧的 `s.notch` 是 nil，
// 照走下去就把刘海建在迷你外壳上——开出两扇）。
func TestToggleNotchPlayerFromTheNotchSide(t *testing.T) {
	main := newTestShell(t).s
	n := main.newNotchShell()
	t.Cleanup(n.closePerWindow)

	// 从刘海那一侧调：它自己的 `s.notch` 是 nil，要绕回 main。
	n.toggleNotchPlayer()
	if main.notch != nil {
		t.Fatal("从刘海那侧按「切刘海」不该又开一个")
	}
	// 主窗口那边再调一次，还能再开出来。
	main.toggleNotchPlayer()
	if main.notch == nil {
		t.Fatal("主窗口那边没把刘海开回来")
	}
}

// TestNotchTickOpensAfterTheHoverDelay 钉「进触发区 300ms 才展开」。
func TestNotchTickOpensAfterTheHoverDelay(t *testing.T) {
	n, _ := newNotchShell(t)
	fake := newFakeClock(time.Now())
	n.clock = fake

	n.notchSt.hasPointer = true
	n.notchSt.pointerIn = true
	n.notchSt.lastPointer = notchNSPoint{X: 100, Y: 10}
	n.notchSt.pointerAt = n.notchSt.lastPointer
	n.notchSt.inAt = fake.Now()

	// 还没到 300ms：收着。
	fake.Advance(100 * time.Millisecond)
	n.notchTick()
	if n.notchSt.state != notchClosed {
		t.Fatal("还没到 300ms 就开了")
	}
	// 够 300ms：开。
	fake.Advance(250 * time.Millisecond)
	n.notchTick()
	if n.notchSt.state != notchOpen {
		t.Fatal("停够 300ms 没展开")
	}
}

// TestNotchTickClosesAfterTheExitGrace 钉「离开 350ms 才收起」。
func TestNotchTickClosesAfterTheExitGrace(t *testing.T) {
	n, _ := newNotchShell(t)
	fake := newFakeClock(time.Now())
	n.clock = fake

	n.notchSt.state = notchOpen
	n.notchSt.hasPointer = true
	n.notchSt.pointerIn = false
	n.notchSt.lastPointer = notchNSPoint{X: 900, Y: 900}
	n.notchSt.pointerAt = n.notchSt.lastPointer
	n.notchSt.outAt = fake.Now()

	fake.Advance(100 * time.Millisecond)
	n.notchTick()
	if n.notchSt.state != notchOpen {
		t.Fatal("宽限没到就收起了")
	}
	fake.Advance(300 * time.Millisecond)
	n.notchTick()
	if n.notchSt.state != notchClosed {
		t.Fatal("走开 350ms 还没收起")
	}
}

// TestNotchTickResetsTheEntryOnMove 钉「在区里挪动会重算停够时间」。
func TestNotchTickResetsTheEntryOnMove(t *testing.T) {
	n, _ := newNotchShell(t)
	fake := newFakeClock(time.Now())
	n.clock = fake

	n.notchSt.hasPointer = true
	n.notchSt.pointerIn = true
	n.notchSt.pointerAt = notchNSPoint{X: 10, Y: 10}
	n.notchSt.inAt = fake.Now()

	// 区里挪一下：inAt 被重置，再走 299ms 也不开。
	n.notchSt.lastPointer = notchNSPoint{X: 50, Y: 12}
	fake.Advance(10 * time.Millisecond)
	n.notchTick()
	fake.Advance(299 * time.Millisecond)
	n.notchTick()
	if n.notchSt.state != notchClosed {
		t.Fatal("挪动之后按旧时刻开了")
	}
}
