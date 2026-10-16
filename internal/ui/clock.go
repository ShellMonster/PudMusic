// 时钟：界面层所有「等一会儿」都从这里走。
//
// 为什么要有这一层：SPEC 的 Testing Decisions 把**假时钟**列为每个阶段都要留下
// 的回归证据——「404 页 3 秒后自动跳首页」这种行为，测试里不能真睡 3 秒（一条
// 测试干等 3 秒，一跑就是好几秒，慢的机器上还会随机地挂）。把「等」收在一个
// 接口后面，测试换一个 Advance 就到点的假时钟，被测的行为一个字都不用改。
//
// ⚠️ 用它的回调有一条硬规矩：**不许写 Shell 的字段**。定时器跑在后台 goroutine
// 上（真时钟）或测试 goroutine 上（假时钟），而界面状态是主线程单点写入的
// （铁律一）。两边同时动同一个字，`-race` 就会报。所以回调只调
// `wakeHandle.invalidate`（它自己带锁），「到没到点」由 view 拿时钟现算。
package ui

import (
	"sync"
	"time"
)

// clockSource 是界面层要的时钟的全部：现在几点，以及每 d 醒一次。
//
// 名字前面那个 clock 是给不掉的：playbar.go 里已经有一个 `clock(d)`，是写
// m:ss 的那个函数（同一个包里同名会打架）。
type clockSource interface {
	// Now 是此刻。倒计时、提示到点没到都拿它和记下的时刻比。
	Now() time.Time
	// EveryFunc 排一个每 d 醒一次的回调，返回的 func 停掉它。
	//
	// 用「每 d 一次」而不是「d 之后一次」，是因为倒计时要一秒一秒地重画数字，
	// 每一跳都得叫醒界面。
	EveryFunc(d time.Duration, f func()) (stop func())
}

// realClock 是真时钟，界面上跑的就是它。
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// EveryFunc 起一个 ticker。⚠️ 停掉的时候光 `t.Stop()` 不够：`t.C` 不会关，
// 那个 goroutine 会永远卡在 `for range t.C` 上。得再给一条退出的路。
func (realClock) EveryFunc(d time.Duration, f func()) func() {
	t := time.NewTicker(d)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-t.C:
				f()
			case <-done:
				t.Stop()
				return
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

// fakeClock 是不真等的时钟：Advance 推多少它就走多少，中途到点的回调按先后
// **同步**跑完。所以测试里「3 秒后回首页」是一行 Advance，不是一行 time.Sleep。
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	waits []*fakeWait
}

// fakeWait 是一个还没到点的等待。
type fakeWait struct {
	// at 是它下一次醒的时刻。
	at time.Time
	// every 大于 0 就是每 every 醒一次的那一种，醒一次往后挪一个 every。
	every time.Duration
	fn    func()
	dead  bool
}

// newFakeClock 建一个从 t 开始的假时钟。t 由测试自己定，用一个整数时间就够。
func newFakeClock(t time.Time) *fakeClock { return &fakeClock{now: t} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) EveryFunc(d time.Duration, f func()) func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := &fakeWait{at: c.now.Add(d), every: d, fn: f}
	c.waits = append(c.waits, w)
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		w.dead = true
	}
}

// Advance 把假时钟往前推 d，把这中间到点的回调按先后跑完。
//
// 跑到回调那一刻，Now() 就是它到点的时刻（不是推完之后的时刻）：定时器里
// 「已经过去几秒」的判断因此和真时钟一致。
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	target := c.now.Add(d)
	c.mu.Unlock()

	for {
		c.mu.Lock()
		var due *fakeWait
		for _, w := range c.waits {
			if w.dead || w.at.After(target) {
				continue
			}
			if due == nil || w.at.Before(due.at) {
				due = w
			}
		}
		if due == nil {
			c.now = target
			c.mu.Unlock()
			return
		}
		if due.at.After(c.now) {
			c.now = due.at
		}
		if due.every > 0 {
			due.at = c.now.Add(due.every)
		} else {
			due.dead = true
		}
		fn := due.fn
		c.mu.Unlock()
		// 回调在锁外跑：它自己可能要碰这个时钟（重新排一次），拿着锁调进去
		// 就是自锁。
		fn()
	}
}
