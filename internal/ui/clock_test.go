package ui

import (
	"sync"
	"testing"
	"time"
)

// 假时钟自己也得有测试：404 的 3 秒全靠它，用一个跑不对的假时钟去验 3 秒，
// 等于什么都没验。

// TestFakeClockDoesNotWaitInRealTime 是这一组的题眼：假时钟推进 3 秒不花 3 秒。
//
// 真等的话这条测试自己就慢 3 秒，而且这正是它要防的那种写法。
func TestFakeClockDoesNotWaitInRealTime(t *testing.T) {
	c := newFakeClock(time.Unix(2026, 0))
	start := time.Now()
	c.Advance(3 * time.Second)
	if spent := time.Since(start); spent > time.Second {
		t.Errorf("Advance(3s) 真花了 %v", spent)
	}
	if got := c.Now(); !got.Equal(time.Unix(2029, 0)) {
		t.Errorf("推进之后是 %v，want 2029-01-01", got)
	}
}

// TestFakeClockFiresInDueOrder 验到点的回调按先后跑，且是在它自己那一刻跑的。
//
// 「在它自己那一刻」这条要紧：回调里要是读到推进之后的时刻，跨过两个到点的
// 回调时就会看到同一时刻，倒计时这类按时间差算的东西会算错。
func TestFakeClockFiresInDueOrder(t *testing.T) {
	start := time.Unix(2026, 0)
	c := newFakeClock(start)

	var got []string
	// 记成「相对起点过了几秒」，比格式化时钟时刻好读，也不受起点选得怎样影响。
	mark := func(what string) func() {
		return func() { got = append(got, what+":"+c.Now().Sub(start).String()) }
	}
	c.EveryFunc(3*time.Second, mark("三秒"))
	c.EveryFunc(time.Second, mark("一秒"))

	c.Advance(3 * time.Second)

	// 同一时刻到点的两个，按排的先后跑：三秒那个先排，所以它先。
	want := []string{"一秒:1s", "一秒:2s", "三秒:3s", "一秒:3s"}
	if len(got) != len(want) {
		t.Fatalf("跑了 %d 次，want %d：%q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 次是 %q，want %q（全部 %q）", i, got[i], want[i], got)
		}
	}
}

// TestFakeClockStopEndsIt 验 stop 之后就不再醒了。
//
// 少了这一条，离开 404 之后那个每秒一跳还在跑：白叫醒界面，而且用户早就走到
// 别的页上了，倒计时还在后台数着。
func TestFakeClockStopEndsIt(t *testing.T) {
	c := newFakeClock(time.Unix(2026, 0))

	n := 0
	stop := c.EveryFunc(time.Second, func() { n++ })
	c.Advance(2 * time.Second)
	if n != 2 {
		t.Fatalf("停之前跑了 %d 次，want 2", n)
	}

	stop()
	c.Advance(10 * time.Second)
	if n != 2 {
		t.Errorf("停掉之后还跑了 %d 次（want 还是 2）", n)
	}
}

// TestFakeClockStopTwiceIsHarmless 验重复 stop 不炸。
//
// disarmNotFound 会在离开 404 时调一次、到点跳首页时再调一次；真实时钟那边
// 是 `sync.Once`，这里也得扛得住。
func TestFakeClockStopTwiceIsHarmless(t *testing.T) {
	c := newFakeClock(time.Unix(2026, 0))
	stop := c.EveryFunc(time.Second, func() {})
	stop()
	stop()
	c.Advance(time.Second)
}

// TestRealClockEveryFuncWakesAndStops 验真时钟那一支也成立。
//
// 假时钟跑得再好，真时钟的 EveryFunc 写错了界面上一样是死的：那一跳只负责
// 叫醒界面（倒计时的数字要一秒一秒动），它不响，数字就永远停在 3。
func TestRealClockEveryFuncWakesAndStops(t *testing.T) {
	var mu sync.Mutex
	n := 0
	stop := realClock{}.EveryFunc(10*time.Millisecond, func() {
		mu.Lock()
		n++
		mu.Unlock()
	})

	waitFor := func(what string, atLeast int) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			got := n
			mu.Unlock()
			if got >= atLeast {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Fatalf("%s：等了 3 秒还没到 %d 次", what, atLeast)
	}
	waitFor("真时钟没醒", 3)
	stop()

	// 停掉之后睡一会儿，看它是不是真的不再醒——goroutine 还活着的话它还在数。
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	after := n
	mu.Unlock()

	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	later := n
	mu.Unlock()

	if later != after {
		t.Errorf("停掉之后还在醒：%d → %d", after, later)
	}
}
