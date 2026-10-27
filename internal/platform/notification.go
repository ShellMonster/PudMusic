// 系统通知（桌面通知）。
//
// # 这一层只做「把一句话交给系统」
//
// 它不认识业务，也不认识界面：给一个 ID、一句标题、一句正文，它就把那条通知
// 挂到系统的通知中心去；用户点了它，它把 ID 交回来；发不出去，它把原因交回来。
//
// 上层（`internal/ui`）负责**什么时候**该通知（用户不在应用里才通知），
// 而这一层只负责「怎么通知」。
//
// # 三件得在这一层垫掉的事
//
//  1. **它要一个跑起来的事件循环**：`mygo.NotificationsSupported` / `Show` 走
//     `needsApp`，而应用还没 ready 的时候那一问拿不到答案。于是这一层先问
//     `mygo.App.IsReady()`——没 ready 就报「不支持」，上层于是改成应用内提示
//     （票面第 5 条：不支持要静默降级，不许把事件丢掉）。
//  2. **第一次 Show 会等用户回答「允不允许通知」**（macOS）。那一次等待不能
//     占着下载那条后台线——这一层把它挪到自己的 goroutine 上去。
//  3. **通知发不出去的时候，事件不能丢**（票面第 5 条）：权限被拒 / 没打包 /
//     这台机器根本不支持——这三种都要**静默降级**成应用内的一句提示，而且
//     **第一次失败之后记住**，不再反复去请求（反复弹那个系统权限框是最招人烦
//     的一种做法）。
//
// # 「不支持」是怎么认出来的
//
// 框架给的是 `mygo.NotificationsSupported()`（macOS 上要求打包成 bundle），
// 而它**要一个 ready 的应用**。这一层在应用 ready 之前只能先按「还不知道」
// 记着，等 ready 之后问一次。
package platform

import (
	"errors"
	"log"
	"sync"

	"github.com/egoist/mygo"
)

// ErrNotificationsDenied 是「用户不允许发通知」。
//
// ⚠️ **它和 `mygo.ErrNotificationsDenied` 是同一个东西**（框架把平台那个直接
// 导出了），而这一层要用它把「权限被拒」和「别的错」分开：权限被拒是**用户
// 明确说的「不要」**，那种错不该重试，也不该反复去请求权限。
var ErrNotificationsDenied = mygo.ErrNotificationsDenied

// ErrNotificationsUnsupported 是「这台机器 / 这一次运行发不了通知」。
//
// ⚠️ **macOS 上没打包成 bundle 就是它**：`mygo.NotificationsSupported()` 在
// 那种情况下是 false，而 `mygo dev` / `go run` 跑出来的正是那种。
var ErrNotificationsUnsupported = errors.ErrUnsupported

// notificationSink 是「用户点了某一条通知」的那只手。
//
// 它由界面层装一次（`ui.MountNotifications`）。ID 是**我们自己给的**稳定 ID
// （见 `internal/ui/notification.go`），所以上层从 ID 就能认出「这一条是下载
// 完成」还是「那一条是更新」。
type notificationSink func(id string)

// notificationFailSink 是「通知没发出去」的那只手。
//
// ⚠️ **它是必需的，而不只是日志**：macOS 上「用户拒了权限」这件事只有
// `Show` 返回之后才知道（而 `Show` 是异步的），所以上层要靠它把这一档改成
// 应用内的一句提示——票面第 5 条的「不丢事件」说的就是它。
type notificationFailSink func(title, body string, err error)

// notifier 是这一层的那份状态。
//
// ⚠️ **它有自己的锁，不在别的锁底下**：通知是**后台**那条线上的事（下载那条
// goroutine），而那一头可能正拿着别的锁。
var notifier struct {
	mu sync.Mutex

	// sink 是「用户点了通知」的那只手。nil = 没装（这一层不参与）。
	sink notificationSink

	// failSink 是「通知没发出去」的那只手。nil = 没装（那时候只有日志）。
	failSink notificationFailSink

	// supported 报「这台机器能不能发通知」。
	//
	// ⚠️ **它同时是「已经问过了没有」那个记号**：框架那个
	// `NotificationsSupported` 要一个 ready 的应用，问不到的时候它会 panic
	// （`needsApp`）。而「第一次失败之后记住，不再反复请求」这条规矩要的
	// 就是这个记号。
	//
	// supported 报「这台机器能不能发通知」。
	//
	// 🔴 **它是三态，而「还没问过」那一档必须由类型自己保证**（票 #559，
	// 2026-10-27 的真机验出来的）：这一份是**包级变量**，而如果它是一个裸
	// int，零值 0 就落在「问过了：发不了」那一档上——于是
	// `NotificationsSupported()` 一进来就「已经问过了」那一支返回 false，
	// **框架那一问一次都没问过**。
	//
	// ⚠️ **症状是静默的**：那一档报「不支持」，上层于是**静默降级**成应用内的
	// 一句提示（票面第 5 条那一档），而界面上看不出任何异样——于是「打包的
	// 应用在 macOS 上弹不出系统通知」这件事，在真机上跑出来是「应用内提示照
	// 旧出现，一切正常」。
	//
	// ⚠️ **而单元测试量不到它**：测试的 `resetNotifier` 显式把它设成「还没问过」
	// 那一档，于是零值那个坑被**测试自己**挡住了。
	//
	// ⚠️ **所以这里用的是一个自定义类型**，而它的**零值就是「还没问过」**：
	// 「忘了初始化」这件事于是**编译不出来也跑不出来**——int 的零值落在「发不
	// 了」那一档上，而这一类型的零值落在「还没问过」那一档上。
	supported supportState

	// denied 记着「用户已经说过不让发通知」。
	//
	// ⚠️ **它和 `supported` 分开**：不支持是「这台机器的事」（换个环境也许就
	// 能发了），被拒是「用户的事」（他刚在系统设置里关了也会再被拒，但**我们
	// 不该再去弹那个系统权限框**了）。
	denied bool
}

// supportState 是「能不能发系统通知」那三档。
//
// 🔴 **它的零值是「还没问过」**（票 #559，2026-10-27 的真机验出来的）：
// 这一份住在**包级变量**里，而裸 int 的零值会落在「问过了：发不了」那一档
// 上——于是「刚起来的进程」一次都不会去问框架，打包的 macOS 应用于是弹不
// 出系统通知（而它静默降级成应用内提示，界面上看不出任何异样）。
//
// ⚠️ **零值落在「还没问过」那一档，是因为那三档的顺序就是这么排的**：第一档
// 是「还没问过」，第二档是「发不了」，第三档是「能发」。
type supportState int

const (
	// supportUnknown 是「还没问过」（**零值**）。
	supportUnknown supportState = iota
	// supportNo 是「问过了：发不了」（没打包 / 这台机器不支持）。
	supportNo
	// supportYes 是「问过了：能发」。
	supportYes
)

// NotificationsSupported 报「这一次运行能不能发系统通知」。
//
// **必须在应用 ready 之后调**（框架那一问要一个跑起来的事件循环）。没 ready
// 的时候它报 false——那一档的处置是「静默降级成应用内提示」，而那时应用
// 还没开窗，本来也没有系统通知可发。
func NotificationsSupported() bool {
	notifier.mu.Lock()
	known := notifier.supported
	notifier.mu.Unlock()
	if known != supportUnknown {
		return known == supportYes
	}
	// 还没问过：应用 ready 了才问一次，然后把答案记下来。
	//
	// ⚠️ **这一步为什么先问 `IsReady`**：框架的 `NotificationsSupported` 头上
	// 有 `needsApp`，而它只在「**主线程**上、且应用还没 `Run` 过」的时候 panic。
	// 这一层是从后台那条线调的（不是主线程），所以不会 panic——但应用还没
	// ready 时那一问本来就答不出什么，而答不出的时候**记住它**就等于把这一
	// 轮永久判成「不支持」（应用启动之后才开始下载，那时它早就能发了）。
	if !mygo.App.IsReady() {
		return false
	}
	supported := mygo.NotificationsSupported()
	notifier.mu.Lock()
	notifier.supported = supportNo
	if supported {
		notifier.supported = supportYes
	}
	notifier.mu.Unlock()
	return supported
}

// Notify 发一条系统通知。
//
// id 是**稳定 ID**：同一个 id 再发一次是**替换**那一条，而不是多出一条。所以
// 同一个下载任务重试两次也不会在通知中心里堆两条。
//
// ⚠️ **它不阻塞调用者**（票面第 5 条「不丢事件」的前提）：发通知要过主线程，
// 而 macOS 的第一次还要等用户回答「允不允许」——那一次等待不能占着下载那条
// 后台线。所以它在**自己的 goroutine** 上做，调用者立刻返回。
//
// 返回的错误只有「压根没发不出去」那几种（不支持 / 权限被拒），而且那几种
// 上层要**静默降级**。发得出去返回 nil。
//
// ⚠️ **macOS 上「用户拒了权限」是异步的**：它在 `Show` 返回之后才知道，而
// 那时 `Notify` 早就返回 nil 了。那一档走的是失败回调（`OnNotificationFailure`）。
func Notify(id, title, body string) error {
	notifier.mu.Lock()
	denied := notifier.denied
	notifier.mu.Unlock()
	if denied {
		// ⚠️ **不再请求**：用户已经说过「不要」。系统设置里他改了主意的话，
		// 下次应用重启时这一层才重新问。
		return ErrNotificationsDenied
	}
	if !NotificationsSupported() {
		return ErrNotificationsUnsupported
	}

	// ⚠️ **在后台那条线上发**：框架的 `Show` 会 `await` 系统的回调，而 macOS 的
	// 第一次还要等用户回答权限——在主线程上等会挡住整个界面，在下载那条线
	// 上等会挡住那一轮下载的收尾。两者都不行，所以它自己一条 goroutine。
	go func() {
		n := mygo.NewNotification(mygo.NotificationOptions{
			ID:    id,
			Title: title,
			Body:  body,
		})
		// ⚠️ **这里不装 `n.OnClick`**：框架的 `notificationClicked` 会**两处**
		// 都通知（先 `n.onClick` 那一份，再 `App.onNotificationClick` 那一份，
		// 见 `mygo/modules.go:992-1000`），而 `OnNotificationClick` 已经接住了
		// 那一次。两边都装的话**点一下会回调两遍**——而那一遍会把窗口叫回来
		// 两次、跳两次页。
		//
		// 而 `App.OnNotificationClick` 那一条是**必需**的：它还接得住「上一次
		// 运行留下的那一条」（点它把应用唤起来），那一遍没有本进程的
		// `Notification` 对象，`n.OnClick` 压根不会触发。
		if err := n.Show(); err != nil {
			notifier.mu.Lock()
			if errors.Is(err, mygo.ErrNotificationsDenied) {
				// ⚠️ **记住它**：用户说过「不要」了，之后每一条都不再请求。
				notifier.denied = true
			}
			notifier.mu.Unlock()
			log.Printf("biu: 系统通知发不出去：%v", err)
			reportFailure(title, body, err)
		}
	}()
	return nil
}

// reportFailure 把「没发出去」交给上层（它要改成应用内的一句提示）。
//
// ⚠️ **它不在锁底下调上层**：那一头要弹一句应用内提示（会碰界面状态），
// 而这一层拿着锁去调另一个包的代码就是当场死锁。
func reportFailure(title, body string, err error) {
	notifier.mu.Lock()
	sink := notifier.failSink
	notifier.mu.Unlock()
	if sink != nil {
		sink(title, body, err)
	}
}

// OnNotificationFailure 装上「通知没发出去」的那只手。
//
// ⚠️ **它不是可有可无的**：macOS 上「用户拒了权限」是异步的，只有它能把
// 那一档交给上层——而上层要做的事是「静默降级成应用内的一句提示」（票面第 5
// 条：权限拒绝 / 不支持时不丢事件，应用内提示仍存在）。
func OnNotificationFailure(fn func(title, body string, err error)) {
	notifier.mu.Lock()
	notifier.failSink = fn
	notifier.mu.Unlock()
}

// ClearNotifications 把本应用留在通知中心里的那些收掉。
//
// **任一扇应用窗口重新拿到焦点时就调它**（票面第 6 条）：用户已经回到应用里
// 看着下载记录页了，通知中心里还堆着「下载完成」就是多余的。
func ClearNotifications() {
	notifier.mu.Lock()
	known := notifier.supported
	notifier.mu.Unlock()
	if known != supportYes {
		// 没发过通知就不用清（清的那一句也要过主线程）。
		return
	}
	mygo.ClearNotifications()
}

// OnNotificationClick 装上「用户点了通知」的那只手。
//
// ⚠️ **框架的 `App.OnNotificationClick` 必须在 `App.Run` 之前装**
// （框架的注释就写着这句话：它要接住「点通知把应用唤起来」那一次，而那是
// 启动参数交付的一部分，早一步晚一步都会漏）。
func OnNotificationClick(fn func(id string)) {
	notifier.mu.Lock()
	notifier.sink = fn
	notifier.mu.Unlock()
	mygo.App.OnNotificationClick(func(id string) {
		notifier.mu.Lock()
		sink := notifier.sink
		notifier.mu.Unlock()
		if sink != nil {
			sink(id)
		}
	})
}
