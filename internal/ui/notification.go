// 系统通知：什么时候该通知、点了通知去哪。
//
// # 这一层是「活跃态」的事实来源
//
// 「用户现在在不在这个应用里」这件事，**不能**从应用层问，也**不能**从下载那条
// 后台线上问——那两头都拿不到窗口的状态（后台那条线要是直接读界面状态，就是
// 跨 goroutine 读 UI 框架，违反铁律一，而且读到的那个值还不一定是最新的）。
//
// 所以活跃态是**从每扇窗的焦点状态推出来的**，而每扇窗的焦点状态早就有了：
// `internal/ui/presence.go` 的 `windowPresence.focused` 就是它（`watchWindow`
// 装的那几个回调改的正是它）。
//
// # 为什么不用 `App.OnDidBecomeActive` / `App.OnDidResignActive`
//
// 框架有那两个回调，但**在 v0.6.4 只有 macOS 后端触发**
// （`mygo@v0.6.4/internal/darwin/app.go:334-338`），Windows / Linux 没有
// 对应的调用。所以拿它当跨平台的判据，那两个平台上的「用户切走了」根本不会被
// 看见——而那恰恰是本票最要紧的那一档。
//
// ⚠️ **而 `OnActivate` 更不能拿来替代**：它是「点 Dock 图标把窗口叫回来」
// （`internal/ui/lifecycle.go` 里 `s.openWindow()` 那一支），跟「用户是不是在
// 这个应用里」是两件事——用户切到别的应用去**不会**触发它。
//
// # 判据只有一条：「有没有一扇窗有焦点」
//
// 票面说的是「没有任何聚焦窗口**或**窗口全部隐藏」，而这两条**合成一条**：
// 一扇窗看不见的时候它不可能有焦点（macOS 上 `Hide()` 会让它 resign key，
// 本机实测 2026-10-26），而「一扇都没有」自然落在「没有一扇有焦点」里。
// 所以判据是：
//
//	有焦点 → 用户在这个应用里 → 不发系统通知
//	没焦点 → 用户不在           → 发一条
//
// # 迷你播放器那一扇**也算**有焦点
//
// 它是 Panel（点它**不激活应用**，票 #162），可它在 macOS 上**照样能拿 key**
// ——本机实测（2026-10-26，真窗口，见 `probe_flow.log`）：
//
//	主窗收起来、迷你窗建出来 → panel.IsFocused() == true
//
// 而它一失焦（用户切到别的应用）就是 false。所以它**能**当「用户在不在」的
// 判据。
//
// ⚠️ **而这一条正是「迷你模式不弹通知」的那一半**：迷你开着的时候主窗是收起来
// 的（`openMiniPlayer`），如果迷你那一扇的焦点不算「用户在」，那么迷你模式下
// 每一次后台下载完成都会弹一条系统通知——而用户正看着迷你播放器在听歌。
//
// ⚠️ **它拿到焦点也不等于「用户激活了这个应用」**：用户在别的应用里点一下迷你
// 播放器的播放键，macOS 上那扇面板会拿到 key 而应用**仍然是不活动的**（实测，
// `probe_click.log`）。那一档我们判成「用户在」——代价是那一小段时间里少弹一条
// 通知，而反过来判的话就是「用户正看着迷你播放器却弹一条下载完成」，那是更糟的
// 一种打扰。
//
// # 「互相切换」的那一瞬：等一小会儿再看
//
// 主窗切到迷你窗时，**焦点不是同时换的**（实测顺序）：
//
//	main BLUR  →  main HIDE  →  panel FOCUS
//
// 中间有一个「两扇都没有焦点」的瞬间。而通知是**立刻**弹的（它不等下一帧）——
// 那一刻如果判成「用户不在」，用户点一下迷你播放器就会收到一条「下载完成」的
// 系统通知，而**他正看着迷你播放器**。
//
// 所以判定不是「看一眼就发」，而是「先看着，等一小会儿再看着」：离开得**那么快**
// 就回来（焦点在两扇窗之间移动），那一档就当没离开过（`notifyGrace`）。
package ui

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/platform"
)

// 通知 ID 的那几个前缀。
//
// ⚠️ **ID 必须是稳定的**（票面第 4 条「相同任务使用稳定 ID」）：框架那套
// `NewNotification` 拿同一个 ID 再发一次是**替换**那一条，而不是多出一条。所以
// 同一个下载任务重试两次也不会在通知中心里堆两条——而随机 ID 会。
const (
	// notifyDownloadPrefix 是下载完成那一条。
	//
	// ⚠️ **后面接的是下载任务自己的 ID**（`<bvid>|<cid>`），不是歌名——歌名
	// 会改（改文件名的后缀），而任务 ID 不会。拿歌名当 ID 的话，同一首歌改了
	// 名就会在通知中心里留下两条。
	notifyDownloadPrefix = "biu-download:"
	// notifyUpdatePrefix 是「有更新了」那一条。
	//
	// ⚠️ **后面接的是版本号**：同一条版本只该有一条通知，而换了一版就该是
	// 新的一条（用户要看的正是「哪一个版本」）。
	//
	// 更新那条与下载那条**共用这一整套出口**：同样的稳定 ID 规矩、同样的路由
	// （点它跳到设置页的更新那一段）、同样的静默降级。
	notifyUpdatePrefix = "biu-update:"
)

// notifyGrace 是「焦点在两扇窗之间移动」的宽限期。
//
// ⚠️ **它要盖住的是焦点搬家那一瞬**（见文件头）：主窗失焦到迷你窗拿到焦点
// 之间，实测有 35~60 毫秒（`probe_flow.log`），机器忙的时候更久。而它是**下限
// 而不是上限**——真正「用户切到别的应用去」的那一档不受它影响（那一档会一直
// 没有焦点，于是照发不误）。
//
// 400 毫秒的依据：它比实测的那 60 毫秒宽裕得多（机器忙时的抖动也在里面），
// 而它对用户是**察觉不到**的——通知晚 0.4 秒出现，看不出来。
const notifyGrace = 400 * time.Millisecond

// notifyGraceSchedule 是「等一小会儿再判一次」那个排程的插口。
//
// ⚠️ **默认是 `time.AfterFunc`**（生产上就是真等一小会儿）。测试把它换成
// 「把那一段排程记下来，由测试自己决定什么时候跑」——于是「宽限期过了之后
// 重新判一次」那一段是**可测的**，而不用拿固定毫秒当判据（AGENTS.md §2：
// 机器忙的时候那种判据会飘，而且飘的时候和被测的逻辑一点关系都没有）。
//
// ⚠️ **它带一把锁**：排程从**后台**那条线调（下载下完了那条 goroutine），
// 而测试换它是从另一条 goroutine 上写的。
var notifyGraceSchedule = struct {
	mu sync.Mutex
	fn func(d time.Duration, f func())
}{fn: func(d time.Duration, f func()) { time.AfterFunc(d, f) }}

// swapNotifyGraceSchedule 换掉那个排程，返回原来那一个。
//
// **只有测试用**：生产路上它一直是 `time.AfterFunc`。
func swapNotifyGraceSchedule(fn func(d time.Duration, f func())) func(d time.Duration, f func()) {
	notifyGraceSchedule.mu.Lock()
	defer notifyGraceSchedule.mu.Unlock()
	old := notifyGraceSchedule.fn
	notifyGraceSchedule.fn = fn
	return old
}

// afterNotifyGrace 排一段「等一小会儿再跑」。
func afterNotifyGrace(f func()) {
	notifyGraceSchedule.mu.Lock()
	fn := notifyGraceSchedule.fn
	notifyGraceSchedule.mu.Unlock()
	fn(notifyGrace, f)
}

// notifyDownloadID 是下载任务那条通知的 ID。
func notifyDownloadID(taskID string) string { return notifyDownloadPrefix + taskID }

// notifyUpdateID 是某一个版本那条通知的 ID。
func notifyUpdateID(version string) string { return notifyUpdatePrefix + version }

// focusSnapshot 是「此刻这几扇窗的焦点状态」那一份快照。
//
// ⚠️ **它是逐窗口攒出来的，不是某一扇窗的**：两个窗口共用一个 `App`，而活跃态
// 说的是「**任一**一扇窗有焦点」。
type focusSnapshot struct {
	// focused 是「有几扇窗现在有焦点」。
	focused int
	// windows 是登记进来的窗数（用来判「有没有窗」）。
	windows int
	// hidden 是「有几扇窗现在看不见」。
	//
	// ⚠️ **它是独立的一档**（票面第 1 条要求「任一窗口聚焦 / 全部窗口隐藏」两档
	// 都要有）：「全部隐藏」不再靠「隐藏必然先失焦」那个隐含顺序——**那一扇窗
	// 可以还带着焦点标志而已经被藏起来了**（`OnHide` 与 `OnBlur` 是两个回调，
	// 谁先到不由我们决定；而 macOS 上 `Hide()` 会让它 resign key，可那是系统
	// 行为，不是我们代码里的顺序保证）。
	//
	// 所以「用户在不在这几扇窗里」这一档**只认「有焦点**且**看得见」**的窗。
	hidden int
}

// idle 报「用户不在这几扇窗里」。
//
// ⚠️ **判据是「一扇窗都没有『有焦点**且**看得见』」**。
//
// ⚠️ **而「窗口全部隐藏」不再靠「隐藏必然先失焦」那个隐含顺序**：一扇藏起来的
// 窗**不该**算数，哪怕它的焦点标志还挂着。所以账上**同时记着「看得见吗」**，
// 而 `focused` 这一档只数「有焦点**且**看得见」的窗。
//
// ⚠️ **「看得见」这一位取的是 `presence.shown`**（`OnShow` / `OnHide` 改的那一
// 位），不是「看得见**且**看得着」那个合成位（`seen()`）——后者还含「聚焦」
// 和「迷你播放器不聚焦也算」两件事，用它的话这一档会自己套自己（见
// `syncFocusTracker` 的注释）。
//
// ⚠️ **而迷你播放器那一扇的焦点要算进来**（见文件头）：迷你模式下主窗是收起来
// 的，若不算它，用户正看着迷你播放器听歌时每下一次下载完成都会弹一条通知。
//
// ⚠️ **而「一扇窗都还没登记」不算「用户不在」**：那是应用刚起来、窗口还没建
// 出来的那一段（`watchWindow` 建好窗口才登记）。那一档发通知的话，用户看到的
// 是「应用刚打开就弹了一条下载完成」——而他正看着应用打开。
func (f focusSnapshot) idle() bool { return f.windows > 0 && f.focused == 0 }

// focusTracker 是「哪些窗在这儿、哪几扇有焦点」的那份账。
//
// ⚠️ **它有自己的锁**：窗口的焦点回调在主线程上，而「现在可以不弹通知了吗」
// 那个问题是从**后台**那条线（下载下完了）问的。一个 bool 跨线程读写，`-race`
// 会报，真机上是一个同极难复现的旧帧。
type focusTracker struct {
	mu sync.Mutex
	// windows 是登记进来的每一扇窗的状态，键是那个登记号。
	windows map[int]*trackedWindow
	// next 是下一个登记号。
	next int
}

// trackedWindow 是登记进来的那一扇窗。
type trackedWindow struct {
	// focused 是「这一扇现在有焦点」。
	focused bool
	// shown 是「这一扇现在看得见」。
	//
	// ⚠️ **它是独立的一位**（票面第 1 条要「全部窗口隐藏」那一档，而那一档
	// 不许靠「隐藏必然先失焦」那个隐含顺序）。`OnShow` / `OnHide` 改它。
	shown bool
}

// newFocusTracker 建一份空的账。
func newFocusTracker() *focusTracker {
	return &focusTracker{windows: map[int]*trackedWindow{}}
}

// add 登记一扇窗，返回它的登记号。
//
// focused 是「登记那一刻这一扇有没有焦点」——它得一开始就对，因为登记之后到第一
// 次状态变化之间可能已经有一份通知在判活跃态了（那一份读到「没焦点」就会判成
// 用户不在）。
//
// ⚠️ **shown 也是**：一扇窗刚建出来的时候它是看得见的（`newWindowPresence` 的
// 默认值就是「看得见」），而登记的那一刻如果它是藏着的，那本账从一开始就该说
// 「看不见」。
func (t *focusTracker) add(focused, shown bool) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.next++
	id := t.next
	t.windows[id] = &trackedWindow{focused: focused, shown: shown}
	return id
}

// remove 摘掉一扇窗（它关掉了）。
func (t *focusTracker) remove(id int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.windows, id)
}

// set 改一扇窗的状态。
//
// ⚠️ **两个位一起改**（focused 和 shown）：它们**都是**「这扇窗此刻的状态」，
// 而「用户在不在这几扇窗里」这一档要同时看它们。分开改的话，中间那一瞬的
// 快照会拿新的 focused 配旧的 shown——而那一瞬正好是「主窗收起来、迷你窗
// 建出来」那个切换（见文件头）。
func (t *focusTracker) set(id int, focused, shown bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	w := t.windows[id]
	if w == nil {
		return
	}
	w.focused = focused
	w.shown = shown
}

// snapshot 取一份此刻的快照。
func (t *focusTracker) snapshot() focusSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	var s focusSnapshot
	for _, w := range t.windows {
		s.windows++
		if !w.shown {
			// ⚠️ **藏起来的那一扇不算「有焦点」**：票面第 1 条要的「全部窗口隐藏」
			// 是一档**独立**的状态，不许靠「隐藏必然先失焦」那个隐含顺序
			// （`OnHide` 与 `OnBlur` 是两个回调，谁先到不由我们决定）。
			s.hidden++
			continue
		}
		if w.focused {
			s.focused++
		}
	}
	return s
}

// shownOf 报一扇窗此刻「看得见吗」。
func (t *focusTracker) shownOf(id int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	w := t.windows[id]
	if w == nil {
		return false
	}
	return w.shown
}

// focusedOf 报一扇窗此刻「有焦点吗」。
func (t *focusTracker) focusedOf(id int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	w := t.windows[id]
	if w == nil {
		return false
	}
	return w.focused
}

// focusTrackerRef 是界面层共享的那一份账。
//
// ⚠️ **它是包级的，而两个窗口共用它**：主窗口和迷你播放器是两个 `Shell`，而
// 「用户在不在这几扇窗里」问的是**全部**窗，所以它不能是逐窗口一份
// （逐窗口那份的话，迷你窗会以为主窗没焦点，而主窗也看不见迷你窗）。
//
// ⚠️ **而它自己不换**：它是包级的一个指针，而它**被后台那条线读**
// （`notifyDownloadDone` 判活跃态）。测试要换它（`newFocusProbe`）的话，
// 换的动作与后台那条线的读会撞上——所以换它的那个动作走
// `swapFocusTracker`，而它带一把锁。
var focusTrackerRef = newFocusTracker()

// focusTrackerMu 护着「换掉 `focusTrackerRef`」这一个动作。
//
// ⚠️ **它和 `focusTracker` 自己那把锁是两回事**：那把锁护的是「账的内容」，
// 而这一把护的是「账本身是谁」。混在一起的话，测试换账的时候正好有一条
// 后台通知在读它——`-race` 会报，而真机上是一个读到了半个旧账的判定。
var focusTrackerMu sync.Mutex

// tracker 拿当前那本账。
func tracker() *focusTracker {
	focusTrackerMu.Lock()
	defer focusTrackerMu.Unlock()
	return focusTrackerRef
}

// swapFocusTracker 换掉那本账，返回原来那一份。
//
// **只有测试用**：生产路上那本账从建立起就不换（它换了就等于把「用户在不
// 在应用里」这件事的历史抹掉，而窗口的登记号还在旧账上）。
func swapFocusTracker(t *focusTracker) *focusTracker {
	focusTrackerMu.Lock()
	defer focusTrackerMu.Unlock()
	old := focusTrackerRef
	focusTrackerRef = t
	return old
}

// notifyWindow 是「这一扇窗在账上的登记号」。
//
// 它挂在 `Shell` 上（逐窗口一份）。
type notifyWindow struct {
	// id 是登记号。0 = 没登记（还没接窗口）。
	id int
	// wasFocused 是「上一刻这一扇有没有焦点」。
	//
	// ⚠️ **它用来认出「刚拿到焦点」那一刻**（票面第 6 条：任一扇窗重新获得
	// 焦点时清掉通知）。`noteWindow` 每次状态变化都跑，所以光看「现在有焦点」
	// 分不出「刚拿到」和「一直有」。
	wasFocused bool
}

// syncFocusTracker 把这一扇窗此刻的状态同步到「活跃态」那本账上。
//
// ⚠️ **它在窗口的每一个状态变化之后调**（`noteWindow` 那一处）：那本账答的是
// 「现在能不能弹系统通知」，而通知是**立刻**弹的——它不等下一帧。
func (s *Shell) syncFocusTracker() {
	// 🔴 **而「用户回来了」也是那一句降级提示该收的时候**（票面第 5 条
	// 「不丢事件」）：那一句是系统通知发不出去时弹的，而它**一直粘着**，等的
	// 就是这一刻——用户回来时它已经摆在那儿了（他看得见），于是它可以按正常的
	// 两秒走（不然它会永远收不掉）。
	//
	// ⚠️ **它在主窗口那一档弹**（见 `notifyFallback`），而这一段是**逐窗窗**
	// 跑的——所以放的是**主窗口那一份**的提示（提示卡片是逐窗口一份的）。
	//
	// ⚠️ **而「放」的时机得是「用户真的看得见它」**：迷你模式下拿回焦点的
	// 可能是迷你窗，而主窗还收着——那时候主窗那一帧**不画**，提示等于没被看
	// 见。所以那一句要等**主窗自己回来**才放（见 `releaseStickyToasts`）。
	//
	// ⚠️ **它每一次状态变化都问一遍**（而不是只在「刚拿到焦点」那一档）：主窗
	// 从收着变成露出来这一下也是一次状态变化，而那一档正是迷你模式下用户
	// 回到主窗的时刻。
	//
	// ⚠️ **它排在「还没接窗口」那一档之前**：那一档是「这扇窗还没登记在账上」
	//（无头 Tester、或者窗口还没建出来），而那一句降级提示**照样要能收**——
	// 它挂在一个还没接窗口的外壳上也是常事（无头测试就是这一档）。
	releaseStickyToasts()

	if s.notify.id == 0 {
		// 还没接窗口（无头 Tester，或者窗口还没建出来）。
		return
	}
	focused := s.presence.focusedState()
	shown := s.presence.shownState()
	tracker().set(s.notify.id, focused, shown)

	// 🔴 **任一扇窗重新拿到焦点就把通知清掉**（票面第 6 条）：用户已经回到应用里
	// 看着下载记录页了，通知中心里还堆着「下载完成」就是多余的。
	//
	// ⚠️ **只在「刚拿到焦点」的那一刻清**：失焦的时候清是反的（用户刚要走）。
	// 而 `noteWindow` 每次状态变化都跑，所以这里要认准**从没有焦点变成有
	// 焦点**这一下。
	if focused && !s.notify.wasFocused {
		platform.ClearNotifications()
	}
	s.notify.wasFocused = focused
}

// releaseStickyToasts 把粘着的那一句降级提示放走（按正常的那两秒收掉）。
//
// 🔴 **它是票面第 5 条「不丢事件」的另一半**（票 #559）：系统通知发不出去时
// 降级成的那一句应用内提示**一直粘着**，等的就是「用户回来」——用户一回来它
// 就摆在他眼前，于是它可以按正常的那两秒走（不然它会永远赖在屏幕上）。
//
// ⚠️ **它放的是「主窗口那一份」**：提示卡片是逐窗口一份的，而那一句是主窗
// 弹的（见 `notifyFallback`）。放错了那一份，它就永远收不掉。
//
// ⚠️ **而「用户看得见它」是前提**：迷你模式下主窗收着，而收着的窗那一帧不画
// ——那时放走它，用户回来的时候它已经没了（那正是这一条要修的那个症状）。
func releaseStickyToasts() {
	s := currentMainShell()
	if s == nil {
		return
	}
	// ⚠️ **主窗得自己看得见**（`seen()` 是「看得见且看得着」——含「有焦点」和
	// 「迷你播放器不聚焦也算」那几层）。
	if !s.presence.seen() {
		return
	}
	s.unstickToasts()
}

// installNotification 装上系统通知这一整套。
//
// ⚠️ **它必须在 `mygo.App.Run` 之前调**：框架的 `App.OnNotificationClick`
// 要接住「点通知把应用唤起来」那一次——那是启动参数交付的一部分（框架的
// 注释就写着「Register it before Run to receive the one whose click launched
// the app」），装晚了就漏掉那一次。
//
// ⚠️ **装的是主窗口那个外壳的**：通知指向的是主窗里的下载记录页，而迷你播放器
// 画的是「正在放什么」。
func installNotification(s *Shell) {
	platform.OnNotificationClick(s.onNotificationClicked)
	// 「没发出去」那一头也在这儿装：macOS 上「用户拒了权限」是**异步**的
	// （它在 `Show` 返回之后才知道），只有它能把那一档交给界面层。
	platform.OnNotificationFailure(notifyFallback)
}

// onNotificationClicked 是「用户点了某一条通知」。
//
// ⚠️ **它要回主窗**：迷你播放器画的是「正在放什么」，而通知指向的是主窗里的
// 一页（下载记录 / 更新弹窗）。所以它走 `handleDeepLink` 那条路——**先让开
// 迷你**（理由见 `handleDeepLink` 的注释），再跳页，再叫回主窗。
//
// ⚠️ **而「叫回主窗」不能省**：点了通知说明用户想看那件事，而窗口可能收在
// 托盘里 / 藏在后台。
func (s *Shell) onNotificationClicked(id string) {
	switch {
	case strings.HasPrefix(id, notifyDownloadPrefix):
		s.mainShell().handleDeepLink(downloadListPath)
	case strings.HasPrefix(id, notifyUpdatePrefix):
		// 更新那一条去设置页的「关于应用」那一段——而那里有个弹窗。
		//
		// ⚠️ **弹窗是逐窗口一份的**（`s.update.wanted`，见 `shell.go`），所以
		// 打开它的是**主窗口那个外壳**。
		s.mainShell().openUpdateDialog()
		s.mainShell().handleDeepLink(settingsPath)
	default:
		// 不认识的 ID：什么都不做。**别猜**——猜错的话用户点一条通知却到了一个
		// 无关的页面上，而界面上看不出任何异样。
		log.Printf("biu: 点了一条认不出的通知：%q", id)
		return
	}
	s.mainShell().openWindow()
}

// mainShell 报「主窗口那个外壳」。
//
// ⚠️ **迷你窗口那一扇没有主窗口**（`NewMini` 的 `main` 可以是 nil，见
// `miniplayer.go`），所以这一档要判空。
func (s *Shell) mainShell() *Shell {
	if s.main != nil {
		return s.main
	}
	return s
}

// mainShellRef 是主窗口那个外壳。
//
// ⚠️ **它是包级的**：主窗口和迷你播放器是两个 `Shell`，而「弹哪一句」得由
// **主窗口**做（迷你播放器画的是「正在放什么」，而通知指向的是主窗里的
// 下载记录页）。
//
// ⚠️ **而它不参与判活跃态**：那一档问的是「全部窗」（见 `focusTrackerRef`），
// 所以它是**全局**的。逐窗口判的话，迷你窗会以为「主窗没焦点 ⇒ 用户不在」。
//
// ⚠️ **它被后台那条线读**（`notifyFallback` 在通知发不出去时弹那句应用内
// 提示，而那一档是异步的），所以它自己带一把锁。
var mainShellRef struct {
	mu sync.Mutex
	s  *Shell
}

// setMainShell 登记主窗口那个外壳。由 `Run` 在建好外壳之后调。
func setMainShell(s *Shell) {
	mainShellRef.mu.Lock()
	mainShellRef.s = s
	mainShellRef.mu.Unlock()
}

// mainShell 拿主窗口那个外壳（没有就是 nil）。
func currentMainShell() *Shell {
	mainShellRef.mu.Lock()
	defer mainShellRef.mu.Unlock()
	return mainShellRef.s
}

// swapMainShell 换掉主窗口那个外壳，返回原来那一个。
//
// **只有测试用**：生产路上它由 `Run` 装一次（见 `setMainShell`）。
func swapMainShell(s *Shell) *Shell {
	mainShellRef.mu.Lock()
	defer mainShellRef.mu.Unlock()
	old := mainShellRef.s
	mainShellRef.s = s
	return old
}

// releaseNotification 把这一扇窗从「活跃态」那本账上摘掉（它关掉了）。
//
// ⚠️ **不摘的话那本账会一直记着一扇已经没了的窗**：而那一扇如果「有焦点」
// （它关掉的时候确实还 focused），那本账就会永远说「用户还在这个应用里」——
// 于是**再也弹不出系统通知**，而界面上看不出任何异样。
//
// ⚠️ **它必须排在「关窗收尾」的那一串里**（`closePerWindow`），因为那是每一扇
// 窗都会走一遍的地方。
func (s *Shell) releaseNotification() {
	if s.notify.id == 0 {
		return
	}
	tracker().remove(s.notify.id)
	s.notify.id = 0
}

// MountNotifications 把系统通知接上应用层。**必须在开窗口之前调**。
//
// ⚠️ **它接的是「下载下完了」那一件**：应用层在后台那条线上知道了「某一项下
// 完了」，把它递到界面层这一头；界面层判完活跃态（用户在不在这几扇窗里），
// 决定弹不弹系统通知。
//
// ⚠️ **为什么要有这一层而不是让应用层直接发通知**：应用层**不认识界面**（它
// 的包注释第一条），而「用户在不在这几扇窗里」是**窗口的状态**——只有界面层
// 答得了。
func MountNotifications(a *app.App) {
	// ⚠️ **「判活跃态」那一头是**全局**的，不是逐窗口的**：那本账是整个进程
	// 一份（见 `focusTrackerRef`），而「用户在不在这几扇窗里」问的是全部窗。
	// 逐窗口的话，迷你窗会以为「主窗没焦点 ⇒ 用户不在」，而主窗也会以为
	// 「迷你窗没焦点 ⇒ 用户不在」——于是通知一会儿弹、一会儿不弹。
	//
	// ⚠️ **而「弹哪一句」是逐窗口的**：迷你播放器画的是「正在放什么」，而通知
	// 指向的是主窗里的下载记录页。所以真正去弹的那一句得由**主窗口那个外壳**
	// 来做（它才有那一页）。
	a.SetDownloadNotifier(notifyDownloadDone)
	// 更新那一条**走同一套出口**：同样的活跃态判定、同样的稳定 ID 规矩、
	// 同样的路由（点它跳到设置页的更新弹窗）。
	//
	// ⚠️ **而它和下载那一档的差别在「谁发起的」**：查更新是后台自动查的
	//（用户多半不在），而用户自己按「检查更新」的那一档不发（他正盯着看）。
	// 那一档的判据在应用层（见 `internal/app/update.go` 的 `check`）。
	//
	// 🔴 **而这里绑的是那个 App**（票 #559，2026-10-27 的评审）：「这个版本
	// 已经提醒过了」那一份**记在应用层**（`internal/app/update.go` 的
	// `claimUpdateNotification`），而**记下它的那一层还不知道用户在不在线**——
	// 那一档是这一层判的。于是这一层判完「用户在里面」（所以那条系统通知**没
	// 发出去**）就得**把那一份退回去**——不退回的话，用户在前台时那一次后台
	// 检查会把版本**永久**记成「说过了」（而它**根本没说过**），用户随后离开，
	// 下一次后台检查被挡住，那一条通知**永远发不出去**。
	//
	// ⚠️ **绑成闭包，而不是记一个包级的 App 指针**：那一份是**只写一次、
	// 只读一次**的（装的时候写，通知那条线上读），而闭包**天生**就是那样——
	// 它不需要锁，也不需要「谁来装、谁来卸」那一份额外的约定。而一个包级指针
	// 是**可变的全局状态**：换掉它的时候正好有一条后台线在读它。
	a.SetUpdateNotifier(func(version string) { notifyUpdateAvailable(a, version) })
}

// releaseUpdateReminder 把「这个版本已经提醒过了」那两份记账都退回去。
//
// 🔴 **它是票 #559 那一段的收尾**（2026-10-27 的评审）：**「说过了」这句话
// 只有在真的说过之后才成立**。而这一次**没有说**——通知没发出去（用户正盯着
// 应用），所以两份记账都得退：
//
//	界面层那一份  `notifiedVersions`——这一层自己记的
//	应用层那一份  `updater.notifiedVersion`——**那一层记它的时候还不知道用户
//	              在不在线**（那是在 `check` 里判完「用户自己按的」之后立刻
//	              记的，而「用户在不在线」是这一层判的）
//
// ⚠️ **只退界面层那一份就是这一条 bug**：用户在前台时那一次后台检查把版本
// 记死在应用层（而它**根本没说过**），用户随后离开，下一次后台检查被挡住——
// 那一条通知**永远发不出去**，而界面上看不出任何异样。
//
// ⚠️ **a 可能是 nil**：那是**没有装应用层**的那一档（界面层的用例直接调
// `notifyUpdateAvailable`）。那一档没有应用层那份账可退，退了就是空操作。
func releaseUpdateReminder(a *app.App, version string) {
	releaseNotifiedVersion(version)
	if a != nil {
		a.ReleaseUpdateNotification(version)
	}
}

// notifyUpdateAvailable 是「查到了新版本」那一段。
//
// ⚠️ **它从任何 goroutine 都能调**（应用层在检查更新的那条线上调它），而它读
// 的那本账是带锁的（见 `focusTracker`）——所以跨 goroutine 调它不违反铁律一。
//
// ⚠️ **a 是「那个 App」**：界面层判出「用户在里面」时要**把应用层那一份记账
// 退回来**（见 `releaseUpdateReminder`），而退的那一头在应用层。
func notifyUpdateAvailable(a *app.App, version string) {
	if version == "" {
		return
	}
	// 🔴 **同一个版本只提醒一次**（票面第 6 条「不重复刷通知」）。
	//
	// ⚠️ **应用层已经去重了一份**（`internal/app/update.go` 的
	// `claimUpdateNotification`），而**这里再去一次是双保险**：应用层那一份
	// 记的是「更新器说过这个版本了」，而这一层记的是「**这个进程**已经为这个
	// 版本发过通知了」。两层各自能独立成立——删掉任何一层，另一层还拦得住
	// 「同一个版本反复弹」。
	//
	// ⚠️ **而稳定 ID 不代替它**：框架那一次 `Show` 是真的会再弹一次（同一个 ID
	// 再 `Show` 是**重新弹**，不是「什么都不做」），而 ID 只决定通知中心里那一条
	// 被**替换**。所以「不再发」得自己记一份。
	if !claimNotifiedVersion(version) {
		return
	}
	if !tracker().snapshot().idle() {
		// ⚠️ **而「用户在里面」那一档要把这一格退回去**：通知没发出去，所以
		// 「这个版本已经提醒过了」这句话**不成立**——等用户下一次不在应用里
		// 时，它还得能再提醒一次。
		//
		// ⚠️ **这就是「提醒过」必须在「真发出去了」之后才记**的原因**：先记
		// 再发的话，用户在应用里时那一次就把它吃掉了，于是**他永远收不到**
		// 那一条通知。
		//
		// 🔴 **而要退的是**两份**：界面层那一份和**应用层那一份**——后者是
		// 在 `check` 里记的，而记它的时候还不知道用户在不在线（见
		// `releaseUpdateReminder` 的注释）。
		releaseUpdateReminder(a, version)
		return
	}
	// ⚠️ **和下载那一档一样等一小会儿再判**（理由见文件头）：焦点正在两扇窗
	// 之间搬家的时候，此刻「没有一扇有焦点」而用户**并没有离开**。
	afterNotifyGrace(func() {
		if !tracker().snapshot().idle() {
			releaseUpdateReminder(a, version)
			return
		}
		sendNotification(notifyUpdateID(version), "有更新了", "BiuGo "+version+" 可更新")
	})
}

// notifiedVersions 是「已经为哪些版本发过通知了」。
//
// ⚠️ **它带一把锁**：通知是**延后**发的（宽限期过了才发，那是在一条 goroutine
// 上），而「记一笔」和「退回去」都从别的 goroutine 上来。
var notifiedVersions = struct {
	mu sync.Mutex
	m  map[string]bool
}{m: map[string]bool{}}

// claimNotifiedVersion 记下「这个版本正在提醒」，并报「该不该提醒」。
func claimNotifiedVersion(version string) bool {
	notifiedVersions.mu.Lock()
	defer notifiedVersions.mu.Unlock()
	if notifiedVersions.m[version] {
		return false
	}
	notifiedVersions.m[version] = true
	return true
}

// releaseNotifiedVersion 把「正在提醒」退回去——那一次**没有真的提醒**。
func releaseNotifiedVersion(version string) {
	notifiedVersions.mu.Lock()
	delete(notifiedVersions.m, version)
	notifiedVersions.mu.Unlock()
}

// notifyStub 是「发通知」那个动作的插口。
//
// ⚠️ **默认是空**：nil 的时候走真的 `platform.Notify`（生产）。
// 测试把它换成记东西的桩，于是那些用例不用起一个真应用就能验「该发的时候
// 真的发了」。
//
// ⚠️ **它带一把锁**：通知是**延后**发的（宽限期过了才发，那是在一条
// goroutine 上），而测试换它也是从另一条 goroutine 上写的——一个不加锁的
// 包级函数指针，`-race` 会报，而真机上是一个「刚写了一半的函数指针」。
//
// 它返回错误，是为了能验「发不出去时降级成应用内提示」那一档（票面第 5 条）。
var notifyStub struct {
	mu sync.Mutex
	fn func(id, title, body string) error
}

// swapNotifyStub 换掉那个插口，返回原来那一个。
//
// **只有测试用**：生产路上那个插口一直是空的（真的通知走 `platform.Notify`）。
func swapNotifyStub(fn func(id, title, body string) error) func(id, title, body string) error {
	notifyStub.mu.Lock()
	defer notifyStub.mu.Unlock()
	old := notifyStub.fn
	notifyStub.fn = fn
	return old
}

// sendNotification 发一条系统通知（真的，或者桩）。
func sendNotification(id, title, body string) {
	notifyStub.mu.Lock()
	stub := notifyStub.fn
	notifyStub.mu.Unlock()
	if stub != nil {
		if err := stub(id, title, body); err != nil {
			notifyFallback(title, body, err)
		}
		return
	}
	if err := platform.Notify(id, title, body); err != nil {
		notifyFallback(title, body, err)
	}
}

// notifyDownloadDone 是「某一首下载完了」那一段。
//
// ⚠️ **它从任何 goroutine 都能调**（应用层在下载那条后台线上调它），而它读
// 的那本账是带锁的（见 `focusTracker`）——所以跨 goroutine 调它不违反铁律一。
func notifyDownloadDone(taskID, title string) {
	if taskID == "" {
		return
	}
	// 用户还在应用里：不发系统通知（他看得见那一条变化，界面上也有）。
	//
	// ⚠️ **这一档不是「跳过」，是「换一个出口」**：界面上已经有那句话了
	// （`downloadFinished` 念的那一句），而系统通知是给**用户不在**时用的。
	if !tracker().snapshot().idle() {
		return
	}
	// ⚠️ **等一小会儿再判一次**（理由见文件头）：焦点正在两扇窗之间搬家的时候，
	// 此刻「没有一扇有焦点」而用户**并没有离开**。
	//
	// ⚠️ **而它是延后而不是丢掉**：宽限期过了之后重新看一眼——用户真的走了才
	// 发，用户回来了就不发（那一档他已经看得见界面上的变化了）。
	afterNotifyGrace(func() {
		if !tracker().snapshot().idle() {
			return
		}
		sendNotification(notifyDownloadID(taskID), "下载完成", "《"+title+"》下载完成")
	})
}

// notifyFallback 是「系统通知发不出去」时的那句应用内提示。
//
// ⚠️ **它不许丢事件**（票面第 5 条）：权限被拒 / 没打包 / 不支持——这三种都
// 要让用户知道「歌下完了」，只是用应用内那句话而不是系统通知。
//
// ⚠️ **而「记住，不再反复请求」是平台层的事**（见 `platform.Notify`）：
// 这一层只管把这一次说清楚。
func notifyFallback(title, body string, err error) {
	log.Printf("biu: 系统通知用不了，改用应用内提示（%s）：%v", title, err)
	// 这一句由**主窗口那个外壳**弹（提示卡片是逐窗口的，见 `toast.go`）——
	// 迷你播放器画的是「正在放什么」，它没有放提示卡片的地方。
	//
	// 🔴 **而它是「粘着」的那一种**（票面第 5 条「不丢事件」）：用户多半**不在
	// 应用里**（系统通知发不出去的那一档多半就是他不在的时候），而普通提示只待
	// 两秒（`toastLifetime`，照 Biu 的 timeout 2000）——于是他回来的时候那一句
	// **早就没了**，而他不知道歌下完了。
	//
	// ⚠️ **它等的是「用户回来」**：那一档由 `syncFocusTracker` 放它走（见那里
	// 的注释）——用户一回来它就摆在他眼前，然后按正常的两秒收掉。
	//
	// ⚠️ **而主窗口可能正收着**（迷你模式）：提示是**逐窗口**的（`toast.go`），
	// 而主窗口收着的时候它那一帧不画——所以那一句要等主窗口回来才看得见。
	// 而**下载记录页上那一列「下载完成」是常驻的**——那才是这一档真正不会丢的
	// 证据。
	if s := currentMainShell(); s != nil {
		s.addStickyToast(body, notice.Success)
	}
}
