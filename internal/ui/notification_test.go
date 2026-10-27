package ui

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
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
//
// ⚠️ **默认「看得见」**：一扇刚建出来的窗是看得见的，而绝大多数用例摆的是
// 「焦点在不在」那一档。「全部隐藏」那一档用 `addHidden`。
func (p *focusProbe) add(focused bool) int {
	return p.addShown(focused, true)
}

// addHidden 登记一扇**看不见**的窗。
func (p *focusProbe) addHidden(focused bool) int {
	return p.addShown(focused, false)
}

// addShown 登记一扇窗，焦点与可见性都摆好。
func (p *focusProbe) addShown(focused, shown bool) int {
	p.t.Helper()
	id := tracker().add(focused, shown)
	p.ids = append(p.ids, id)
	return id
}

// set 改一扇窗的焦点状态（**看得见**不变）。
func (p *focusProbe) set(id int, focused bool) {
	p.t.Helper()
	tracker().set(id, focused, tracker().shownOf(id))
}

// setShown 改一扇窗的可见性（焦点不变）。
func (p *focusProbe) setShown(id int, shown bool) {
	p.t.Helper()
	tracker().set(id, tracker().focusedOf(id), shown)
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
//
// 🔴 **而「通知是在切换**之前**发起的」是这一条的关键**：
// 通知是**延后**发的（宽限期过了才发），所以发起它的那一刻「用户不在」是
// 真的——而宽限期里迷你窗拿到了焦点，于是那一次被宽限期挡下来。
//
// ⚠️ **反过来那一版证明不了什么**：如果先让迷你窗拿到焦点**再**发起通知，
// 那么发起那一刻就已经「用户在里面」了，函数当场就返回了——**宽限期那段代码
// 一次都没跑到**。而把宽限期那段代码删掉，那一版照样绿。所以那一版量不到它
// 声称要量的东西。
func TestSwitchingBetweenOurOwnWindowsIsNotLeavingTheApp(t *testing.T) {
	p := newFocusProbe(t)

	main := p.add(true)
	mini := p.add(false)
	p.set(main, true)
	p.set(mini, false)

	// ⚠️ **主窗先失焦**（真实顺序：`main BLUR` → `main HIDE` → `panel FOCUS`）。
	//
	// ⚠️ **而通知是在这个「两扇都没有焦点」的瞬间发起的**——那才是这一档
	// 真正要挡的情形：下载恰好在这个瞬间下完了。
	//
	// ⚠️ **顺序反了就量不到东西**：先发起通知的话，主窗还带着焦点，那一刻
	// 判成「用户在里面」，函数当场就返回了——**宽限期那段代码一次都没跑到**。
	// 而把宽限期那段代码删掉，那一版照样绿。
	p.set(main, false)
	if !tracker().snapshot().idle() {
		t.Fatal("主窗刚失焦、迷你窗还没拿焦点的那一瞬间，不该说用户还在——")
	}

	// ⚠️ **而「互相切换不被误判」的那一半靠的是宽限期**：用户点一下迷你播放器，
	// 焦点从主窗搬到迷你窗，而那一瞬的「没焦点」不该弹出一条通知。
	//
	// ⚠️ **通知在切换**之前**发起**：那一刻「用户不在」是真的（主窗刚失焦、
	// 迷你窗还没拿到焦点），所以它会进宽限期那一段——而宽限期里迷你窗拿到了
	// 焦点，于是它被挡下来。
	//
	// 这一条量的就是它：**发起之后、等过宽限期**，那一条通知不该出现。
	got := captureNotify(t, func() {
		notifyDownloadDone("BV1x|cid1", "晴天")
	})

	// 然后迷你窗拿到焦点（用户点了一下迷你播放器）——**在宽限期之内**。
	p.set(mini, true)

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

// TestAHiddenWindowDoesNotCountAsTheUserBeingHere 钉住「全部窗口隐藏」那一档
// **不靠「隐藏必然先失焦」那个隐含顺序**。
//
// 🔴 **这是票面第 1 条要的「独立」**：票面说的是「没有任何聚焦窗口**或**窗口
// 全部隐藏」——那两档是**两件事**，而这一档**不许**靠「隐藏的时候会先失焦」
// 那个隐含顺序（`OnHide` 与 `OnBlur` 是两个回调，谁先到不由我们决定；而 macOS
// 上 `Hide()` 让它 resign key 那是**系统行为**，不是我们代码里的顺序保证）。
//
// ⚠️ **而这一档错起来的样子是静默的**：判错成「用户还在」→ 那条系统通知
// **永远不弹**，而用户不在应用里、界面上一句话也没有。
func TestAHiddenWindowDoesNotCountAsTheUserBeingHere(t *testing.T) {
	p := newFocusProbe(t)

	// 一扇窗：它**还带着焦点标志**，而它**已经被藏起来了**。
	//
	// ⚠️ **这一档就是「隐藏没有先触发失焦」那一种**：焦点标志还挂着，而窗已经
	// 不在了。
	main := p.addShown(true, false)

	snap := tracker().snapshot()
	if snap.focused != 0 {
		t.Errorf("藏起来的那一扇窗还在算「有焦点」，focused=%d，want 0 —— "+
			"于是全部窗口隐藏时那一条系统通知永远不弹，而用户不在应用里",
			snap.focused)
	}
	if snap.hidden != 1 {
		t.Errorf("藏起来的那一扇窗没有算进「隐藏」，hidden=%d，want 1", snap.hidden)
	}
	if !snap.idle() {
		t.Error("全部窗口隐藏，却说用户还在——于是后台下载完成一声不吭")
	}

	// ⚠️ **而「藏起来之后又露出来」那一档要跟着变**：窗口回来了，它就又是
	// 「看得见**且**有焦点」→ 用户在里面。
	p.setShown(main, true)
	if tracker().snapshot().idle() {
		t.Error("窗口已经露出来了，却还说用户不在")
	}
}

// TestAHiddenWindowIsAwayEvenWhileItStillHasFocus 钉住「隐藏的那一扇窗不弹
// 通知」这一档**真的发得出去**。
//
// ⚠️ **上一条量的是「快照怎么说」，这一条量的是「通知真的没发」**：快照对了
// 而发通知那一段没看它，那就是两段各说各话。
func TestAHiddenWindowIsAwayEvenWhileItStillHasFocus(t *testing.T) {
	p := newFocusProbe(t)
	p.addShown(true, false) // 藏起来、还带着焦点标志

	got := captureNotify(t, func() {
		notifyDownloadDone("BV1x|cid1", "晴天")
	})
	time.Sleep(notifyGrace + 200*time.Millisecond)
	if got.len() != 1 {
		t.Fatalf("全部窗口隐藏，却只发了 %d 条系统通知，want 1 条（%v）", got.len(), got.items)
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

// TestAFailedNotificationStillSaysItWhenTheUserReturns 钉住「那一句应用内提示
// 在用户回来的时候**还在**」。
//
// 🔴 **这是票面第 5 条「不丢事件」的真判据**（票 #559）：系统通知发不出去
// （没打包 / 权限被拒 / 不支持）时降级成的那一句，用户**多半不在应用里**——
// 而普通提示只待两秒（`toastLifetime`，照 Biu 的 timeout 2000）。所以他切走
// 几分钟再回来时，那一句**早就过期了**，而他不知道歌下完了。
//
// ⚠️ **这一条量的是「过期之后还在不在」**：先等过提示的两秒（那是普通提示的
// 一辈子），再问「它还在不在」。
func TestAFailedNotificationStillSaysItWhenTheUserReturns(t *testing.T) {
	sh := newTestShell(t, withoutTester())
	s := sh.s

	old := swapMainShell(s)
	t.Cleanup(func() { swapMainShell(old) })

	notifyFallback("下载完成", "《晴天》下载完成", errFakeNotify)

	// 用户切到别的应用去，过了一会儿才回来。
	time.Sleep(toastLifetime + 300*time.Millisecond)

	if len(s.toasts) == 0 {
		t.Errorf("系统通知发不出去，那一句应用内提示在用户回来之前就过期了 —— " +
			"而用户不在应用里时，界面上一个字也没有，他不会知道歌下完了")
	}
	// 那一句话的内容还在（不只是「有一条」）。
	if len(s.toasts) > 0 && !strings.Contains(s.toasts[0].text, "晴天") {
		t.Errorf("留下来那一句是 %q，want 一句带歌名的", s.toasts[0].text)
	}
}

// TestTheStickyNoticeIsReleasedWhenTheUserComesBack 钉住「用户回来之后那一句
// 会被收掉」。
//
// 🔴 **它是上一条的另一半**（票 #559）：粘着的那一句等的就是「用户回来」——
// 而他回来了，它就**该按正常的那两秒走**。不然它会永远赖在屏幕上（一条收不
// 掉的提示比「提示过期了」更糟）。
func TestTheStickyNoticeIsReleasedWhenTheUserComesBack(t *testing.T) {
	sh := newTestShell(t, withoutTester())
	s := sh.s

	old := swapMainShell(s)
	t.Cleanup(func() { swapMainShell(old) })

	notifyFallback("下载完成", "《晴天》下载完成", errFakeNotify)
	if len(s.toasts) == 0 {
		t.Fatal("系统通知发不出去，可那句应用内提示压根没弹")
	}
	if !s.toasts[0].sticky {
		t.Error("那一句不是「粘着」的 —— 而用户不在应用里的时候它必须粘着")
	}

	// 用户回来了：这一扇窗重新拿到焦点（这一档由 `syncFocusTracker` 认）。
	s.noteWindow(func(p *windowPresence) { p.focused = true })

	if len(s.toasts) > 0 && s.toasts[0].sticky {
		t.Error("用户已经回来了，那一句还粘着 —— 它会永远赖在屏幕上")
	}
	// 放走之后它按正常的那两秒走：过一会儿它自己没了。
	time.Sleep(toastLifetime + 300*time.Millisecond)
	if len(s.toasts) != 0 {
		t.Errorf("用户回来之后过了 %v，那一句还摆着 %d 条 —— 它该按正常的那两秒收掉",
			toastLifetime, len(s.toasts))
	}
}

// TestTheStickyNoticeIsOnScreenWhenTheUserComesBack 钉住「用户回来时那一句
// **画在画面上**」。
//
// 🔴 **它是上一条之外的另一半**（票 #559）：上一条量的是「它被放走了」，而这一条
// 量的是「它在被放走**之前**确实摆在他眼前」。
//
// ⚠️ **而这一条量得到，是因为提示是逐窗口一份的**：主窗收着的时候那一帧**不画**
// （见 `presence.go`），所以那时候它虽然「在」，用户却看不见。
func TestTheStickyNoticeIsOnScreenWhenTheUserComesBack(t *testing.T) {
	sh := newTestShell(t, withoutTester())
	s := sh.s

	old := swapMainShell(s)
	t.Cleanup(func() { swapMainShell(old) })

	notifyFallback("下载完成", "《晴天》下载完成", errFakeNotify)

	// ⚠️ **先让主窗收起来**：那是「用户不在应用里」的那一档（迷你模式 / 收在
	// 后台），而那一档**不画**——所以那句话虽然还在，用户看不见它。
	s.noteWindow(func(p *windowPresence) { p.shown = false })
	s.presence.update(func(p *windowPresence) { p.focused = false })

	if len(s.toasts) == 0 {
		t.Fatal("那一句不见了 —— 而它必须等用户回来")
	}

	// 用户回到主窗：它露出来了（`OnShow`）。
	s.noteWindow(func(p *windowPresence) { p.shown = true })

	// ⚠️ **它得画出来**：提示卡片是 `PassThrough` 的（见 `viewToasts`），所以
	// 「它在那儿」靠的是它**画出来了**，而画出来的那一帧得是「主窗看得见」的
	// 那一帧。
	visible := s.visibleToasts()
	if len(visible) == 0 {
		t.Fatal("用户回到主窗，那一句却没画出来 —— 而他就是靠它知道歌下完了")
	}
	if !strings.Contains(visible[0].text, "晴天") {
		t.Errorf("画面上那一句是 %q，want 一句带歌名的", visible[0].text)
	}
}

// TestTheSameVersionIsNotNotifiedTwice 钉住「同一个版本只提醒一次」。
//
// 🔴 **这是票面第 6 条「不重复刷通知」**（票 #559）：后台检查**每小时**一次，
// 而**同一个新版本会被查到很多次**（用户一直不装它就一直查得到）——不记的话，
// 用户每小时都会收到一条一模一样的「有更新了」。
//
// ⚠️ **而稳定 ID 不代替它**：框架那一次 `Show` 是真的会再弹一次（同一个 ID 再
// `Show` 是**重新弹**，不是「什么都不做」），而 ID 只决定通知中心里那一条被
// **替换**。所以「不再发」得自己记一份。
func TestTheSameVersionIsNotNotifiedTwice(t *testing.T) {
	p := newFocusProbe(t)
	resetNotifiedVersions(t)
	p.add(false) // 用户不在

	got := captureNotify(t, func() {
		notifyUpdateAvailable(nil, "1.2.3")
	})
	time.Sleep(notifyGrace + 200*time.Millisecond)
	if got.len() != 1 {
		t.Fatalf("第一次提醒发了 %d 条，want 1 条（%v）", got.len(), got.items)
	}

	// 第二次查同一个版本（后台又查了一遍）。
	got2 := captureNotify(t, func() {
		notifyUpdateAvailable(nil, "1.2.3")
	})
	time.Sleep(notifyGrace + 200*time.Millisecond)
	if got2.len() != 0 {
		t.Errorf("同一个版本提醒了 %d 次，want 1 次 —— "+
			"而用户每小时都会收到一条一模一样的「有更新了」", got2.len())
	}
}

// TestANewVersionIsNotifiedAgain 钉住「换了版本就该再提醒一次」。
//
// ⚠️ **去重不许把「整个更新提醒」关掉**：用户要看的正是「哪一个版本」，而
// 出了新版本他就该知道。
func TestANewVersionIsNotifiedAgain(t *testing.T) {
	p := newFocusProbe(t)
	resetNotifiedVersions(t)
	p.add(false)

	got := captureNotify(t, func() {
		notifyUpdateAvailable(nil, "1.2.3")
	})
	time.Sleep(notifyGrace + 200*time.Millisecond)
	if got.len() != 1 {
		t.Fatalf("第一次提醒发了 %d 条，want 1 条（%v）", got.len(), got.items)
	}

	got2 := captureNotify(t, func() {
		notifyUpdateAvailable(nil, "1.3.0")
	})
	time.Sleep(notifyGrace + 200*time.Millisecond)
	if got2.len() != 1 {
		t.Errorf("换了版本却只提醒了 %d 次，want 1 次（%v）", got2.len(), got2.items)
	}
}

// TestAVersionFoundWhileTheUserIsHereIsNotifiedWhenTheyLeave 钉住「用户在前台
// 发现新版本，等他离开之后**仍然**能收到那一条通知」。
//
// 🔴 **这是票 #559 那一段的真判据**（2026-10-27 的评审）：
//
//	第一次后台检查（用户**在前台**）→ 不发通知（他正盯着应用）
//	用户随后**离开**
//	第二次后台检查 → **发出一条通知**
//
// ⚠️ **而这一条量的是「两层合起来」**：应用层记着「这个版本已经提醒过了」
// （`internal/app/update.go` 的 `claimUpdateNotification`），而界面层记着
// 「这个进程已经为这个版本发过通知了」（`notifiedVersions`）。**两层的语义
// 不一样**——应用层那一份是「更新器说过这个版本了」，而它记下的时候**还不知
// 道用户在不在线**（那一档是界面层判的）。
//
// ⚠️ **只退界面层那一份就是这一条 bug**：应用层那一份仍然记着「说过了」，
// 于是第二次后台检查被它挡住，那一条通知**永远发不出去**——而界面上看不出任
// 何异样。
func TestAVersionFoundWhileTheUserIsHereIsNotifiedWhenTheyLeave(t *testing.T) {
	p := newFocusProbe(t)
	resetNotifiedVersions(t)

	// ⚠️ **这一条必须走**真的后台检查**那一条路**（`App.StartAutoChecks` →
	// `updater.check(false)` → `claimUpdateNotification`），而**不是**直接调
	// `notifyUpdateAvailable`。
	//
	// ⚠️ **为什么**：应用层那一份记账（`updater.notifiedVersion`）是**在
	// `check` 里记的**——直接调 `notifyUpdateAvailable` 的话，那一份根本不存在，
	// 量到的就只有界面层那一半，而这一条量的是**两层合起来**的行为。
	//
	// ⚠️ **而这一层那一半也退**：只退界面层那一份的话，应用层那一份仍然记着
	// 「说过了」，第二次后台检查被它挡住——那一条通知**永远发不出去**。
	be := newFakeUpdate("0.1.0", &app.UpdateRelease{Version: "1.2.3"})
	a := app.New()
	t.Cleanup(a.Close)
	a.MountUpdates(be, filepath.Join(t.TempDir(), "app-update.json"))
	// ⚠️ **要装的是真的那一套接线**（`MountNotifications`）：它装上
	// `SetUpdateNotifier`——而**没有它的话应用层根本不会把「查到了新版本」递到
	// 界面层**，那一条通知压根不会有人去发。
	//
	// ⚠️ **而它绑的是那个 App**（`MountNotifications` 里装的是闭包）：那一层
	// 判出「用户在里面」时要**把应用层那一份记账退回来**，而退的那一头就是
	// 这个 App。
	MountNotifications(a)

	// 用户**在前台**：主窗有焦点。
	main := p.add(true)
	p.set(main, true)

	// 第一次后台检查：界面层判出「用户在里面」，那一条**没发出去**。
	got := captureNotify(t, func() {
		stop := a.StartAutoChecks()
		defer stop()
		waitFor(t, "后台那一次查到了新版本", func() bool { return a.UpdateSnapshot().Available() })
	})
	time.Sleep(notifyGrace + 200*time.Millisecond)
	if got.len() != 0 {
		t.Fatalf("用户正盯着应用，却还是发了 %d 条系统通知（%v）", got.len(), got.items)
	}

	// 用户随后离开：主窗失焦（**还看得见**）。
	p.set(main, false)

	// 第二次后台检查：**该发出一条**。
	got2 := captureNotify(t, func() {
		stop := a.StartAutoChecks()
		defer stop()
		waitFor(t, "后台那一次又查了一遍", func() bool { return be.checksDone() >= 2 })
	})
	time.Sleep(notifyGrace + 200*time.Millisecond)
	if got2.len() != 1 {
		t.Fatalf("用户离开之后同一个版本只发了 %d 条通知（%v），want 1 条 —— "+
			"而他在前台发现新版本时那一条没发出去，于是他永远不知道有新版",
			got2.len(), got2.items)
	}
	if !strings.Contains(got2.at(0).body, "1.2.3") {
		t.Errorf("通知里没有版本号：%q", got2.at(0).body)
	}
}

// errFakeNotify 是「系统通知发不出去」那一种错。
var errFakeNotify = errFake("系统通知用不了")

// errFake 是一个假错。
type errFake string

// resetNotifiedVersions 把「已经提醒过哪些版本」那份账清空，测试结束时还原。
//
// ⚠️ **它是包级的**：而上一条测试提醒过的版本会**留在里面**——于是这一条里
// 提醒同一个版本会当场被拦下来，而它量的是「换了版本就该再提醒一次」。
func resetNotifiedVersions(t *testing.T) {
	t.Helper()
	notifiedVersions.mu.Lock()
	old := notifiedVersions.m
	notifiedVersions.m = map[string]bool{}
	notifiedVersions.mu.Unlock()
	t.Cleanup(func() {
		notifiedVersions.mu.Lock()
		notifiedVersions.m = old
		notifiedVersions.mu.Unlock()
	})
}

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

// TestTheUpdateNotificationSwitchIsOnTheSettingsPage 钉住「那一格在设置页上」。
//
// 🔴 **这是票面第 3 条「更新提醒可复用同一基础设施但另开开关」**（票 #559）：
// 那一格必须**在界面上有一个地方能拨**——而它默认是**开**的，所以用户想关掉
// 它得先找到它。
//
// ⚠️ **而它得摆在「关于应用」那一段里**：「有更新了」是那一段的东西，而摆在
// 别处的话用户找不到它。
func TestTheUpdateNotificationSwitchIsOnTheSettingsPage(t *testing.T) {
	// ⚠️ **更新得挂上**：那一格在「关于应用」那一段里，而那一段整体是由
	// `settingsUpdate` 画的——它没挂更新时**整段都不摆**（理由见那里的注释）。
	gu := newFakeUpdate("9.9.9", nil)
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""), mounted(func(_ *testing.T, a *app.App) {
		a.MountUpdates(gu, filepath.Join(t.TempDir(), "app-update.json"))
	}))

	if !tt.HasText(updateNotificationTitle) {
		t.Fatalf("设置页上没有 %q 那一行，文字有 %q", updateNotificationTitle, tt.Texts())
	}
	// 它得在「关于应用」那一段里（用户找的是「有更新了」那一件事）。
	if !tt.HasText(updateSection) {
		t.Fatalf("设置页上没有 %q 那一段，文字有 %q", updateSection, tt.Texts())
	}
}

// TestTheUpdateNotificationSwitchActuallyTurnsItOff 钉住「那一格真的管住那条
// 通知」。
//
// 🔴 **上一条只量到「那一行在画面上」，而这一条量到「拨下去它真的存进去了」**
// （票 #559）：一格画在设置页上而按了不生效，是这一类开关最常见的错——而症状
// 是**完全静默**的：用户以为关掉了，而每小时还是收到一条「有更新了」。
func TestTheUpdateNotificationSwitchActuallyTurnsItOff(t *testing.T) {
	gu := newFakeUpdate("9.9.9", nil)
	tt, s, a, _ := shellOnSettings(t, app.DefaultSettings(""), mounted(func(_ *testing.T, a *app.App) {
		a.MountUpdates(gu, filepath.Join(t.TempDir(), "app-update.json"))
	}))

	if !a.SettingsSnapshot().UpdateNotificationEnabled() {
		t.Fatal("「更新提醒」那一格一开始就是关的 —— 而它默认是开的")
	}

	sw, ok := tt.Find("开关：" + updateNotificationTitle)
	if !ok {
		t.Fatalf("找不到 %q 那个开关，文字有 %q", updateNotificationTitle, tt.Texts())
	}
	// ⚠️ **它得先滚进可视区**：那一格在「关于应用」那一段里，而那一段在常规
	// 那一页的最末尾——而设置页是可滚的，屏幕外面那个旋钮点不到。
	sw = settingRect(t, tt, s, "开关："+updateNotificationTitle)
	tt.ClickAt(sw.X+sw.W/2, sw.Y+sw.H/2)
	tt.Frame()

	if a.SettingsSnapshot().UpdateNotificationEnabled() {
		t.Error("把「更新提醒」拨到关，可设置里它还是开着 —— " +
			"于是它管不住那条通知，而用户以为关掉了")
	}
}
