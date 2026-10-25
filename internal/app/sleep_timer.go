package app

import "time"

// 睡眠定时器：到点（或这一首放完）自动停下来。
//
// 它是一次性的：触发之后自己清掉，不落盘——重启之后没有定时器，睡着了的人
// 不会因为昨晚的设置在明早的第一首歌上被关掉。

// SleepTimerSteps 是界面上给的「过多久停」的几档。
var SleepTimerSteps = []time.Duration{15 * time.Minute, 30 * time.Minute, 45 * time.Minute, 60 * time.Minute}

// SetSleepTimer 设「过 d 之后暂停」。d <= 0 等于取消。会盖掉已有的定时器
// （包括「放完这一首」那一档）。
func (a *App) SetSleepTimer(d time.Duration) {
	if d <= 0 {
		a.CancelSleepTimer()
		return
	}
	a.mu.Lock()
	a.stopSleepLocked()
	at := time.Now().Add(d)
	a.sleepAt = at
	a.sleepStop = a.after(d, func() {
		a.mu.Lock()
		if !a.sleepAt.Equal(at) {
			a.mu.Unlock()
			return
		}
		a.sleepAt, a.sleepStop = time.Time{}, nil
		a.mu.Unlock()
		a.Pause()
	})
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// SleepAfterTrack 设「放完这一首就停」。会盖掉按时长的那一档。
func (a *App) SleepAfterTrack() {
	a.mu.Lock()
	a.stopSleepLocked()
	a.sleepAfterTrack = true
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// CancelSleepTimer 取消定时器（两种都取消）。
func (a *App) CancelSleepTimer() {
	a.mu.Lock()
	a.stopSleepLocked()
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
	}
}

func (a *App) stopSleepLocked() {
	if a.sleepStop != nil {
		a.sleepStop()
	}
	a.sleepAt, a.sleepStop, a.sleepAfterTrack = time.Time{}, nil, false
}

// takeSleepAfterTrack 在一首放完时问一次：要不要在这儿停。要停就顺手清掉标志。
func (a *App) takeSleepAfterTrack() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	was := a.sleepAfterTrack
	a.sleepAfterTrack = false
	return was
}
