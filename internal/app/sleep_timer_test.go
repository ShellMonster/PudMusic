package app_test

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
)

func TestTheSleepTimerFiresOnceAndClears(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	var fire func()
	var stopped bool
	a.SetAfterForTest(func(d time.Duration, fn func()) func() {
		fire = fn
		return func() { stopped = true }
	})

	a.SetSleepTimer(30 * time.Minute)
	if a.Snapshot().SleepAt.IsZero() {
		t.Fatal("设了定时器，SleepAt 还是零值")
	}
	fire()
	if !a.Snapshot().SleepAt.IsZero() {
		t.Error("到点之后定时器没有清掉")
	}
	if stopped {
		t.Error("到点触发不该去 stop 自己")
	}
}

func TestTheSleepTimerKindsReplaceEachOther(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	var stopped int
	a.SetAfterForTest(func(time.Duration, func()) func() { return func() { stopped++ } })

	a.SetSleepTimer(15 * time.Minute)
	a.SleepAfterTrack()
	st := a.Snapshot()
	if !st.SleepAt.IsZero() || !st.SleepAfterTrack || stopped != 1 {
		t.Errorf("换成放完这首后：SleepAt=%v after=%v stopped=%d", st.SleepAt, st.SleepAfterTrack, stopped)
	}
	a.SetSleepTimer(15 * time.Minute)
	if a.Snapshot().SleepAfterTrack {
		t.Error("按时长的那档没有盖掉放完这首")
	}
	a.CancelSleepTimer()
	if st := a.Snapshot(); !st.SleepAt.IsZero() || st.SleepAfterTrack || stopped != 2 {
		t.Errorf("取消之后还有残留：%+v stopped=%d", st, stopped)
	}
}
