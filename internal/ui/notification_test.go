package ui

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// 这一组盯的是「用户不在应用里的时候，后台下载完成要弹一条系统通知」
//（票 #559 / 板子任务 #297）。
//
// # 判据是「什么时候该弹」，不是「弹了没有」
//
// 真的系统通知在无头环境里发不出去（`mygo.NotificationsSupported` 要一个
// ready 的应用，而 `mygo dev` 那种运行在 macOS 上还没打包），所以这一组
// **不碰框架那套通知**，而是盯**这一票真正新加的那一层判定**：
//
//	「用户在不在这几扇窗里」这个判据（`focusTracker`）
//
// 而它错起来的样子是**静默**的：判错成「用户还在」→ 永远不弹通知；判错成
// 「用户不在」→ 用户正盯着应用呢，弹出来一条系统通知。所以它必须钉死。

// focusProbe 造一份「活跃态那本账」，让测试能直接摆出用户在不在这几扇窗里。
type focusProbe struct {
	t *testing.T
	// ids 是登记过的窗。
	ids []int
}

// newFocusProbe 建一份空的账，并保证测试结束时它回到空。
func newFocusProbe(t *testing.T) *focusProbe {
	t.Helper()
	old := swapFocusTracker(newFocusTracker())
	t.Cleanup(func() { swapFocusTracker(old) })
	return &focusProbe{t: t}
}

// add 登记一扇窗。
func (p *focusProbe) add(focused bool) int {
	p.t.Helper()
	id := tracker().add(focused)
	p.ids = append(p.ids, id)
	return id
}

// set 改一扇窗的状态。
func (p *focusProbe) set(id int, focused bool) {
	tracker().set(id, focused)
}

// TestTheFocusSnapshotSaysWhenTheUserIsAway 钉住「用户在不在这几扇窗里」。
//
// 那是这一票的新判定，而它错起来是静默的（见文件头）。
func TestTheFocusSnapshotSaysWhenTheUserIsAway(t *testing.T) {
	p := newFocusProbe(t)

	// 一扇窗，有焦点：用户正在用它。
	main := p.add(true)
	if tracker().snapshot().idle() {
		t.Error("主窗口有焦点，却说用户不在——那会让系统通知一直弹，" +
			"而用户正盯着应用")
	}

	// 用户切到别的应用去了：主窗口失焦（**还看得见**）。
	p.set(main, false)
	if !tracker().snapshot().idle() {
		t.Error("用户切走了，却说用户还在——那会让后台下载完成" +
			"一声不吭，而用户不在")
	}

	// 用户回到应用里。
	p.set(main, true)
	if tracker().snapshot().idle() {
		t.Error("用户回来了，却还说用户不在")
	}
}

// TestTheMiniPlayerCountsAsTheUserBeingHere 钉住「迷你播放器拿到焦点
// 算「用户在这个应用里」」。
//
// ⚠️ **这是这一票最容易错的一处**：迷你播放器是 Panel（点它**不激活应用**，
// 见 `miniplayer.go` 和票 #162），可它在 macOS 上**照样能拿 key**（实测
// 2026-10-26，真窗口：主窗收起来、迷你窗建出来 → `panel.IsFocused()==true`）。
//
// ⚠️ **而它必须算**：迷你模式下主窗是收起来的（`openMiniPlayer`），而用户在
// 迷你播放器里听歌——**他正看着这个应用**。不算它的话，每一次后台下载完成都
// 会弹一条系统通知，而用户正看着迷你播放器在听歌。
//
// ⚠️ **而「全部隐藏」那一档不需要另算**：一扇看不见的窗不可能有焦点（macOS 上
// `Hide()` 会让它 resign key），所以「一扇都没有焦点」已经盖住了它。
func TestTheMiniPlayerCountsAsTheUserBeingHere(t *testing.T) {
	p := newFocusProbe(t)

	main := p.add(false)
	mini := p.add(true)
	// 迷你模式下：主窗收在后台（失焦），迷你窗浮在桌面上（有焦点）。
	p.set(main, false)
	p.set(mini, true)

	snap := tracker().snapshot()
	if snap.focused != 1 {
		t.Errorf("迷你播放器拿到焦点却不算「用户在这个应用里」，focused=%d —— "+
			"而迷你模式下主窗是收起来的，那样每下一次下载完成都会弹一条通知，"+
			"而用户正看着迷你播放器在听歌", snap.focused)
	}
	if snap.idle() {
		t.Error("用户正看着迷你播放器，却说用户不在")
	}
}

// TestSwitchingBetweenOurOwnWindowsIsNotLeavingTheApp 钉住「主窗与迷你窗互相
// 切换不会被误判为离开应用」。
//
// ⚠️ **那一档的顺序是**：主窗失焦 → 主窗隐藏 → 迷你窗拿到焦点。
// 两个状态变化**不是同时**发生的——中间有一个「两扇都没有焦点」的瞬间。
//
// ⚠️ **而那一个瞬间就是坑**：通知是**立刻**弹的，它不等下一帧。所以那一刻
// 如果判成「用户不在」，用户点一下迷你播放器就会收到一条「下载完成」的系统
// 通知——而他正看着迷你播放器。
//
// ⚠️ **而这一档靠的不是「判得准」，是「等一等」**：那一瞬真的判成「不在」，
// 而宽限期（`notifyGrace`）过了之后再看一眼——那时候迷你窗已经有焦点了，
// 于是不发。
func TestSwitchingBetweenOurOwnWindowsIsNotLeavingTheApp(t *testing.T) {
	p := newFocusProbe(t)

	main := p.add(true)
	mini := p.add(false)
	p.set(main, true)
	p.set(mini, false)

	// 迷你窗拿到焦点、主窗失焦（真实顺序：主窗先失焦，迷你窗后拿焦点）。
	p.set(main, false)
	// ⚠️ **中间那一瞬间**：两扇都没有焦点。
	if !tracker().snapshot().idle() {
		t.Error("主窗刚失焦、迷你窗还没拿焦点的那一瞬间，不该说用户还在——")
	}
	p.set(mini, true)

	// ⚠️ **而「互相切换不被误判」的那一半靠的是宽限期**：用户点一下迷你播放器，
	// 焦点从主窗搬到迷你窗，而那一瞬的「没焦点」不该弹出一条通知。
	//
	// 这一条量的就是它：切换之后等过宽限期，那一条通知**不该出现**。
	got := captureNotify(t, func() {
		notifyDownloadDone("BV1x|cid1", "晴天")
	})
	time.Sleep(notifyGrace + 200*time.Millisecond)
	if got.len() != 0 {
		t.Errorf("主窗与迷你窗互相切换，却还是发了 %d 条系统通知（%v）——"+
			"而用户正看着迷你播放器", got.len(), got.items)
	}
}

// TestAllWindowsHiddenMeansTheUserIsAway 钉住「窗口全部隐藏」那一档。
//
// 用户把窗口都收起来了（关掉 / 按 ⌘H），那也是「用户不在」——可以弹通知。
func TestAllWindowsHiddenMeansTheUserIsAway(t *testing.T) {
	p := newFocusProbe(t)

	p.add(false)
	p.add(false)

	if !tracker().snapshot().idle() {
		t.Error("两扇窗都收起来了，却说用户还在——于是后台下载完成一声不吭")
	}
}

// TestNoWindowYetIsNotTheUserBeingAway 钉住「应用刚起来、窗口还没建出来」
// 那一档。
//
// ⚠️ **那一档发通知的话，用户看到的是「应用刚打开就弹了一条下载完成」**——
// 而他正看着应用打开。窗口是 `watchWindow` 建好之后才登记的（见 `presence.go`），
// 所以「一扇窗都没有登记」是「还没建出来」，不是「用户不在」。
func TestNoWindowYetIsNotTheUserBeingAway(t *testing.T) {
	newFocusProbe(t)

	if tracker().snapshot().idle() {
		t.Error("一扇窗都还没登记，却说用户不在 —— " +
			"那会让应用刚打开就弹出一条系统通知")
	}
}

// TestAClosedWindowIsNotCounted 钉住「关掉的那一扇窗要从账上摘掉」。
//
// ⚠️ **不摘的话那本账会永远记着一扇已经没了的窗**：而它关掉的时候确实还
// focused着——于是**再也弹不出系统通知**，而界面上看不出任何异样。
func TestAClosedWindowIsNotCounted(t *testing.T) {
	p := newFocusProbe(t)

	main := p.add(true)
	if tracker().snapshot().idle() {
		t.Fatal("起点就不对：主窗口有焦点，却说用户不在")
	}

	// 关窗：摘掉。
	tracker().remove(main)

	snap := tracker().snapshot()
	if snap.windows != 0 {
		t.Errorf("关掉的那一扇窗还在账上，windows=%d，want 0", snap.windows)
	}
	if snap.focused != 0 {
		t.Errorf("关掉的那一扇窗还留着焦点，focused=%d，want 0 —— "+
			"于是再也弹不出系统通知", snap.focused)
	}
}

// TestDownloadDoneIsAnnouncedWhenTheUserIsAway 钉住「下载完了这一条通知
// 真的会发出去」。
//
// ⚠️ **这一条钉的是接线**：应用层那一头（`downloadFinished`）把这件事递过来
// 之后，界面层这一头真的调了 `platform.Notify`——不是调了别的什么。
func TestDownloadDoneIsAnnouncedWhenTheUserIsAway(t *testing.T) {
	p := newFocusProbe(t)
	main := p.add(false)
	p.set(main, false) // 用户不在

	got := captureNotify(t, func() {
		notifyDownloadDone("BV1x|cid1", "晴天")
	})
	// ⚠️ **要等过宽限期**：通知是延后发的（见文件头）。
	time.Sleep(notifyGrace + 200*time.Millisecond)
	if got.len() != 1 {
		t.Fatalf("通知发了 %d 条，want 1 条（%v）", got.len(), got.items)
	}
	if !strings.Contains(got.at(0).body, "晴天") {
		t.Errorf("通知里没有歌名：%q", got.at(0).body)
	}
	if !strings.HasPrefix(got.at(0).id, notifyDownloadPrefix) {
		t.Errorf("通知的 ID %q 不带下载那一段前缀", got.at(0).id)
	}
}

// TestDownloadDoneIsSilentWhileTheUserIsHere 钉住「用户正在用应用时不发系统
// 通知」。
//
// ⚠️ **这一档是「不打扰」**：用户正盯着下载记录页呢，再弹一条系统通知是
// 多余的——而界面上已经有那句话了（读屏软件也念过了）。
func TestDownloadDoneIsSilentWhileTheUserIsHere(t *testing.T) {
	p := newFocusProbe(t)
	main := p.add(true)
	p.set(main, true) // 用户在

	got := captureNotify(t, func() {
		notifyDownloadDone("BV1x|cid1", "晴天")
	})
	time.Sleep(notifyGrace + 200*time.Millisecond)
	if got.len() != 0 {
		t.Errorf("用户正盯着应用，却还是发了 %d 条系统通知（%v）", got.len(), got.items)
	}
}

// TestTheNotificationIDIsStable 钉住「同一个任务用稳定 ID」。
//
// ⚠️ **随机 ID 的话同一个下载任务重试两次就会在通知中心里堆两条**——而用户
// 只下了一首歌。
//
// ⚠️ **而 ID 里带的是任务 ID 而不是歌名**：歌名会改（改文件名的后缀），任务 ID
// 不会。
func TestTheNotificationIDIsStable(t *testing.T) {
	first := notifyDownloadID("BV1x|cid1")
	second := notifyDownloadID("BV1x|cid1")
	if first != second {
		t.Errorf("同一个任务的两次通知 ID 不一样：%q vs %q", first, second)
	}
	if notifyDownloadID("BV1x|cid1") == notifyDownloadID("BV1x|cid2") {
		t.Error("两个不同的任务拿到了同一个 ID —— 第二个会替换掉第一个")
	}
}

// TestTheNotificationGoesToTheDownloadList 钉住「点通知回主窗的下载记录页」。
//
// ⚠️ **这是接线那一条**：ID 认出来之后得真的跳到那一页——而不跳的话用户点了
// 通知什么也没发生，而界面上看不出任何异样。
func TestTheNotificationGoesToTheDownloadList(t *testing.T) {
	sh := newTestShell(t, withoutTester())
	s := sh.s

	s.onNotificationClicked(notifyDownloadID("BV1x|cid1"))

	if got := s.router.Path(); got != downloadListPath {
		t.Errorf("点了下载通知之后停在 %q，want %q", got, downloadListPath)
	}
}

// TestTheUpdateNotificationGoesToTheSettings 钉住「点更新通知回设置页的更新
// 弹窗」。
//
// ⚠️ **而弹窗是逐窗口一份的**（`s.update.wanted`），所以打开它的必须是**主
// 窗口那个外壳**——通知指向的是主窗里的设置页，而迷你播放器画的是「正在放
// 什么」。
func TestTheUpdateNotificationGoesToTheSettings(t *testing.T) {
	sh := newTestShell(t, withoutTester())
	s := sh.s

	s.onNotificationClicked(notifyUpdateID("1.2.3"))

	if got := s.router.Path(); got != settingsPath {
		t.Errorf("点了更新通知之后停在 %q，want %q", got, settingsPath)
	}
	if !s.update.wanted {
		t.Error("点了更新通知却没有打开更新弹窗 —— 用户得自己再找一次")
	}
}

// TestAnUnknownNotificationGoesNowhere 钉住「不认识的 ID 不猜」。
//
// ⚠️ **猜错的话用户点一条通知却到了一个无关的页面上**——而界面上看不出任何
// 异样。
func TestAnUnknownNotificationGoesNowhere(t *testing.T) {
	sh := newTestShell(t, withoutTester())
	s := sh.s
	before := s.router.Path()

	s.onNotificationClicked("some-other-app:whatever")

	if got := s.router.Path(); got != before {
		t.Errorf("点了一条认不出的通知，页面却从 %q 跳到了 %q", before, got)
	}
}

// TestAFailedNotificationStillSaysItInTheApp 钉住「系统通知发不出去时，那件事
// 不丢」。
//
// ⚠️ **这是票面第 5 条**：权限被拒 / 没打包 / 不支持——那三种都要**静默降级**
// 成应用内的一句提示，而**应用内提示仍存在**。
func TestAFailedNotificationStillSaysItInTheApp(t *testing.T) {
	sh := newTestShell(t, withoutTester())
	s := sh.s

	old := swapMainShell(s)
	t.Cleanup(func() { swapMainShell(old) })

	notifyFallback("下载完成", "《晴天》下载完成", errFakeNotify)

	if len(s.toasts) == 0 {
		t.Error("系统通知发不出去，却连一句应用内提示都没有 —— " +
			"用户不会知道歌下完了")
	}
}

// errFakeNotify 是「系统通知发不出去」那一种错。
var errFakeNotify = errFake("系统通知用不了")

// errFake 是一个假错。
type errFake string

func (e errFake) Error() string { return string(e) }

// capturedNotify 是「发出去的那一条通知」。
type capturedNotify struct {
	id    string
	title string
	body  string
}

// captureNotify 把 `platform.Notify` 换成记东西的桩，跑 fn，把记下来的交回去.
//
// ⚠️ **它换的是「发通知」那一个动作，不是「判要不要发」那一个**——所以这一组
// 验的是「该发的时候真的发出去了」，而「不该发的时候没发」由上面那几条验。
//
// ⚠️ **它记的那一份是带锁的**：通知是**延后**发的（宽限期过了才发），所以读
// 它的时候可能正有另一条 goroutine 在写。
func captureNotify(t *testing.T, fn func()) *capturedList {
	t.Helper()
	list := &capturedList{}
	old := swapNotifyStub(func(id, title, body string) error {
		list.mu.Lock()
		defer list.mu.Unlock()
		list.items = append(list.items, capturedNotify{id: id, title: title, body: body})
		return nil
	})
	t.Cleanup(func() { swapNotifyStub(old) })
	fn()
	return list
}

// capturedList 是「记下来的那几条通知」。
type capturedList struct {
	mu    sync.Mutex
	items []capturedNotify
}

// len 报记了几条。
func (c *capturedList) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// at 取第 n 条。
func (c *capturedList) at(n int) capturedNotify {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.items[n]
}
