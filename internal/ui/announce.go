// 状态变化的播报（无障碍）。
//
// # 这一层是「攒」和「念」的那一头
//
// 后台发生了一件事（下载下完了、歌放起来了、搜索出结果了），而**读屏软件用户
// 看不见那一幕**——他们没有余光去扫右下角那张卡片，也没有耳朵去等歌响起来。
// 所以这一件事得**被念出来**：框架的 `ui.Context.Announce` 就是干这个的
// （「asks screen readers to read text out once, after what they are reading,
// for news the keyboard focus does not bring, as a search done or a file
// saved」）。
//
// ⚠️ **和 `.Label(...)` 是两件事**：`.Label` 给的是「**这一枚是什么**」（那一枚
// 按钮叫什么、那一行是谁），而播报给的是「**刚刚发生了什么**」。全仓有 118 处
// `.Label`，把它们逐个改成播报的后果是读屏软件把一屏控件名字念一遍，而**一件
// 正在发生的事一句也没有**。这一票要的是后者。
//
// # 为什么中间要攒一层
//
// ⚠️ **`Announce` 只在「正在建视图」的那一段里有效**：框架的 `Context.Announce`
// 走 `build()`，而 `build()` 在**帧外**返回 nil（`mygo/ui/frame.go:29-34`）——
// 帧外调它是**静默无效**的：不报错、不打日志，什么也没发生。
//
// 而「刚刚发生了什么」多半是**后台那条线**知道的：下载是在后台下完的，取流是
// 在后台回来的。所以这里先攒着，等界面层下一次建视图的时候再念出去。
//
// 攒的那一摊是**带锁**的：后台那条线push，界面线程取，两边不是同一个 goroutine。
//
// # 一帧建好几遍视图，念一遍就够
//
// 框架一帧最多建三遍视图（`mygo/ui/runtime.go:524` 那个 `for pass := 0; pass < 3`），
// 而每一遍都会走一遍 `Shell.View`。所以**取走**必须是**有破坏性**的：第一遍取
// 走并念掉，后面那两遍取到的是空的——一句只念一遍。
package ui

import (
	"sync"

	"github.com/egoist/mygo/ui"
)

// announceQueue 是攒着待播报的那些句子。
//
// ⚠️ **它有自己的锁**：push 来自后台那条线（下载下完了、点赞回来了），take 来自
// 界面线程。`Shell.toastMu` 是同一类东西、同一类理由（见 `toast.go`）。
type announceQueue struct {
	mu    sync.Mutex
	texts []string
}

// push 攒一句。
func (q *announceQueue) push(text string) {
	if text == "" {
		return
	}
	q.mu.Lock()
	q.texts = append(q.texts, text)
	q.mu.Unlock()
}

// take 取走攒着的那些。
//
// ⚠️ **取走就是不再有了**：这是「一句只念一遍」的那道闸（见文件头）。
func (q *announceQueue) take() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.texts) == 0 {
		return nil
	}
	out := q.texts
	q.texts = nil
	return out
}

// announce 攒一句待播报的话。
//
// **任何 goroutine 都能调**（后台那条线也调它），但它只是攒着——真正念出去
// 的地方是 `flushAnnouncements`，而那一个只在建视图的时候才有效。
func (s *Shell) announce(text string) { s.announcements.push(text) }

// flushAnnouncements 把攒下的那些念给读屏软件听。
//
// ⚠️ **只能在建视图的时候调**：帧外 `Context.Announce` 是静默无效的
// （`mygo/ui/frame.go:29-34`）。
//
// ⚠️ **一句只念一遍**：这里取走的是攒着的那一摊（`take` 是有破坏性的），所以
// 框架那一帧里第二遍、第三遍建视图时取到的是空的。
func (s *Shell) flushAnnouncements(c *ui.Context) {
	for _, text := range s.announcements.take() {
		c.Announce(text)
	}
}
