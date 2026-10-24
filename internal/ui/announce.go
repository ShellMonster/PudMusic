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
// 按钮叫什么、那一行是谁），而播报给的是「**刚刚发生了什么**」。界面层里有 95
// 处 `.Label`（带参数的那些才是给读屏软件的名字；另有 7 处 `Label()` 是领域
// 方法，跟无障碍无关），把它们逐个改成播报的后果是读屏软件把一屏控件名字念一
// 遍，而**一件正在发生的事一句也没有**。这一票要的是后者。
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
//
// # 攒着的那几句是**有上限、有过期**的
//
// ⚠️ **看不见的那一扇窗不能一直攒**：迷你播放器开着的时候主窗口是收在后台的
// （两个窗口互斥），而用户切到别的应用去的时候主窗口也算「看不见」——那一扇窗
// 看不见的时候连帧都不画，攒着的话没人念，等用户回来时**一次性全念出来**：
// 攒了多久就念多久之前的旧事。
//
// 所以两件事同时做：**看不见的那一扇直接丢掉**（`announce` 里那一句），而
// 攒着的那几摊**有上限也有过期**（`announceLimit` / `announceTTL`）——后者兜住
// 迷你播放器那一种（它是「不聚焦也算看得见」的，摘不走）。
package ui

import (
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

const (
	// announceLimit 是同时最多攒几句。
	//
	// ⚠️ **和 `toastLimit` 一样是 3**：屏幕上最多摆 3 条提示，读屏软件那边也最多
	// 积 3 句——一次念一屏是最吵的一种吵。而**新来的把旧的挤掉**：留住的正好是
	// 「刚刚发生的那几件」。
	announceLimit = 3

	// announceTTL 是攒着的那句话最多留多久。
	//
	// ⚠️ **必须有**：用户切到别的应用去的那段时间里后台照样在发生着事，而那些
	// 事**回到应用时不该全部念一遍**。
	//
	// 10 秒：够覆盖「下一帧」（几毫秒）到「用户瞥了一眼别的窗口」（几秒）；而
	// 10 秒前的旧事，用户回到应用时看一眼画面就知道了。
	announceTTL = 10 * time.Second
)

// announceItem 是攒着的一句话，外加它被攒下的那一刻。
type announceItem struct {
	text string
	at   time.Time
}

// announceQueue 是攒着待播报的那些句子。
//
// ⚠️ **它有自己的锁**：push 来自后台那条线（下载下完了、点赞回来了），take 来自
// 界面线程。`Shell.toastMu` 是同一类东西、同一类理由（见 `toast.go`）。
type announceQueue struct {
	mu    sync.Mutex
	items []announceItem
}

// push 攒一句。
//
// ⚠️ **满了就把最旧的挤掉**：留最新那几句（刚刚发生的那几件），而不是留最旧的
// 那几句（那几句多半已经是几分钟前的旧事了）。
func (q *announceQueue) push(text string, now time.Time) {
	if text == "" {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, announceItem{text: text, at: now})
	if len(q.items) > announceLimit {
		q.items = q.items[len(q.items)-announceLimit:]
	}
}

// take 取走攒着的那些（**过期的不要**）。
//
// ⚠️ **取走就是不再有了**：这是「一句只念一遍」的那道闸（见文件头）。而**过期的
// 那几句就地扔掉**：用户切到别的应用待了半分钟，回来时那半分钟里的事已经不该
// 念了——**攒了多久就念多久之前的旧事**是最糟的一种糟。
func (q *announceQueue) take(now time.Time) []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return nil
	}
	var out []string
	// ⚠️ **念过的也要扔掉**（`kept[:0]` 那一句）：这个函数是「取走」——
	// 取走就是不再有了。留着它们的话，下一帧又念一遍（读屏软件把同一句话念
	// 一屏，是最吵的一种吵）。
	kept := q.items[:0]
	for _, it := range q.items {
		if now.Sub(it.at) > announceTTL {
			continue
		}
		kept = append(kept, it)
		out = append(out, it.text)
	}
	q.items = kept[:0]
	return out
}

// announce 攒一句待播报的话。
//
// **任何 goroutine 都能调**（后台那条线也调它），但它只是攒着——真正念出去的
// 地方是 `flushAnnouncements`，而那一个只在建视图的时候才有效。
//
// 🔴 **看不见的那一扇窗直接丢掉这一句**。
//
// 迷你播放器开着的时候主窗口是收在后台的（两个窗口互斥），而用户切到别的应用
// 去的时候主窗口也算「看不见」——那一扇窗看不见的时候连帧都不画，攒着的话
// 没人念，等用户回来时**一次性全念出来**。
//
// 而**同一句被两扇窗各念一遍**也是同一个毛病：看不见的那一扇收下了，回来的时候
// 又念一次（那一扇回来时是看得见的，于是那句就念出来了）。
//
// ⚠️ **判的是「用户现在看得到这一扇窗」**（`presence.seen`），也就是唤醒那一道
// 闸门用的是同一个判据（见 `wake.go` 的规则三）——两边说的必须是同一件事。
func (s *Shell) announce(text string) {
	if !s.presence.seen() {
		return
	}
	s.announcements.push(text, time.Now())
}

// flushAnnouncements 把攒下的那些念给读屏软件听。
//
// ⚠️ **只能在建视图的时候调**：帧外 `Context.Announce` 是静默无效的
// （`mygo/ui/frame.go:29-34`）。
//
// ⚠️ **一句只念一遍**：这里取走的是攒着的那一摊（`take` 是有破坏性的），所以
// 框架那一帧里第二遍、第三遍建视图时取到的是空的。
func (s *Shell) flushAnnouncements(c *ui.Context) {
	for _, text := range s.announcements.take(time.Now()) {
		c.Announce(text)
	}
}
