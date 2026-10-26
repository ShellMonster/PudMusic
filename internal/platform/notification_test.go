package platform

import (
	"errors"
	"sync"
	"testing"
)

// 这一组盯的是「通知发不出去的时候怎么降级」那一层（票 #559 / 板子任务
// #297）。
//
// # 为什么不碰框架那套通知
//
// `mygo.NotificationsSupported` 要一个 ready 的应用，而无头测试里没有；而
// `mygo.NewNotification(...).Show()` 走的是主线程——在无头环境里发不出去。
//
// 所以这一组盯的是**这一层自己拿主意的那几处**：三态的记号（问过没有）、
// 「用户拒了就不再请求」、以及失败要交给上层。

// resetNotifier 把这一层的那份状态抹回初始。
//
// ⚠️ **每一组都要抹**：这一层是包级的（它管的是整个进程一份的「能不能发」
// 那个记号），而测试之间共用它就会串——上一条留下的「用户拒了」会让下一条
// 一条通知都发不出去。
func resetNotifier(t *testing.T) {
	t.Helper()
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	notifier.sink = nil
	notifier.failSink = nil
	notifier.supported = -1
	notifier.denied = false
}

// TestANotificationIsNotSupportedBeforeTheAppIsReady 钉住「应用还没 ready 时
// 不去问框架」。
//
// ⚠️ **问不得**：框架那一问头上有 `needsApp`，而它在「**主线程**上、且应用
// 还没 `Run` 过」的时候直接 panic。
//
// ⚠️ **而「记住答不上来」是错的**：那等于把这一轮永久判成「不支持」——应用
// 启动之后才开始下载，那时它早就能发了。
func TestANotificationIsNotSupportedBeforeTheAppIsReady(t *testing.T) {
	resetNotifier(t)

	if NotificationsSupported() {
		t.Error("应用还没 ready，却说能发系统通知 —— " +
			"而框架那一问在那一刻本来就答不出什么")
	}

	// ⚠️ **关键**：那一问不能被记成「不支持」——它得留着「还没问过」，
	// 好让应用 ready 之后还能再问一次。
	notifier.mu.Lock()
	known := notifier.supported
	notifier.mu.Unlock()
	if known != -1 {
		t.Errorf("应用还没 ready 就把答案记成了 %d，want -1（还没问过）—— "+
			"那等于把这一轮永久判成「不支持」", known)
	}
}

// TestTheSupportAnswerIsAskedOnlyOnce 钉住「问过一次就记住」。
//
// ⚠️ **反复问是最招人烦的一种做法**：macOS 上第一次问会弹系统的权限框，
// 而那一问不该每条通知都来一次。
func TestTheSupportAnswerIsAskedOnlyOnce(t *testing.T) {
	resetNotifier(t)

	// 假装应用 ready 了（这一层只在 ready 之后才问框架）。
	notifier.mu.Lock()
	notifier.supported = 1
	notifier.mu.Unlock()

	for i := 0; i < 3; i++ {
		if !NotificationsSupported() {
			t.Fatal("已经记成「能发」，却又说不能 —— 那说明它没记住")
		}
	}
}

// TestADeniedNotificationIsNeverAskedForAgain 钉住「用户说过「不要」之后，
// 不再请求」。
//
// ⚠️ **这是票面第 5 条的另一半**：「首次失败后记住，不反复请求」。
func TestADeniedNotificationIsNeverAskedForAgain(t *testing.T) {
	resetNotifier(t)

	notifier.mu.Lock()
	notifier.denied = true
	notifier.mu.Unlock()

	err := Notify("biu-download:x", "下载完成", "《晴天》下载完成")
	if !errors.Is(err, ErrNotificationsDenied) {
		t.Errorf("用户已经拒过了，却还是去请求，返回 %v", err)
	}
}

// TestTheFailureIsHandedToTheUpperLayer 钉住「发不出去的那一档交给上层」。
//
// ⚠️ **上层要靠它降级**：macOS 上「用户拒了权限」是**异步**的（它在 `Show`
// 返回之后才知道），而上层要做的正是「静默降级成应用内的一句提示」——
// 票面第 5 条的「不丢事件」。
func TestTheFailureIsHandedToTheUpperLayer(t *testing.T) {
	resetNotifier(t)

	var mu sync.Mutex
	var gotTitle, gotBody string
	var gotErr error
	OnNotificationFailure(func(title, body string, err error) {
		mu.Lock()
		defer mu.Unlock()
		gotTitle, gotBody, gotErr = title, body, err
	})

	reportFailure("下载完成", "《晴天》下载完成", ErrNotificationsDenied)

	mu.Lock()
	defer mu.Unlock()
	if gotTitle != "下载完成" || gotBody != "《晴天》下载完成" {
		t.Errorf("上层收到的是 %q / %q", gotTitle, gotBody)
	}
	if !errors.Is(gotErr, ErrNotificationsDenied) {
		t.Errorf("上层收到的错是 %v，want ErrNotificationsDenied", gotErr)
	}
}

// TestTheClickIsHandedToTheUpperLayer 钉住「点了通知之后交给上层」。
//
// ⚠️ **而那一次必须在 `App.Run` 之前装好**（框架的注释就写着这句话）：否则
// 「点通知把应用唤起来」那一次会漏掉——而那正是用户从通知中心回来的那条路。
func TestTheClickIsHandedToTheUpperLayer(t *testing.T) {
	resetNotifier(t)

	var mu sync.Mutex
	var got string
	OnNotificationClick(func(id string) {
		mu.Lock()
		defer mu.Unlock()
		got = id
	})

	// 框架那一层装上了没装上都行（这里只验「我们装的那一手接住了 ID」）。
	notifier.mu.Lock()
	sink := notifier.sink
	notifier.mu.Unlock()
	if sink == nil {
		t.Fatal("点了通知那一手没装上")
	}
	sink("biu-download:BV1x|cid1")

	mu.Lock()
	defer mu.Unlock()
	if got != "biu-download:BV1x|cid1" {
		t.Errorf("上层收到的是 %q", got)
	}
}
