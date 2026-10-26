package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/service/download"
)

// 这一组盯的是「下载下完了 → 界面层」那一段接线（票 #559）。
//
// ⚠️ **它在应用层**：应用层那一句「《某某》下载完成」是念给读屏软件听的
// （见 `announce.go`），而系统通知那一段是**界面层**判完活跃态才发出去的。
// 这一组只管**应用层把这件事递出去了**——中间就靠 `SetDownloadNotifier`
// 那一行，删掉它编译照样过、别的用例照样绿。

// TestADownloadThatFinishedReachesTheNotifier 钉住「下载下完了会告诉界面层」。
//
// ⚠️ **这一段不许丢事件**（票面第 5 条）：而「丢」在这里是**静默**的——
// 界面层没收到，别的用例照样绿，而用户不在应用里时那首歌下完了他一点动静
// 都听不到。
func TestADownloadThatFinishedReachesTheNotifier(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	var gotID, gotTitle string
	calls := 0
	a.SetDownloadNotifier(func(taskID, title string) {
		calls++
		gotID, gotTitle = taskID, title
	})

	a.downloadFinished(download.Snapshot{ID: "BV1x|cid1", Title: "晴天", Status: download.StatusDone})

	if calls != 1 {
		t.Fatalf("界面层那一头被叫了 %d 次，want 1 次", calls)
	}
	if gotID != "BV1x|cid1" {
		t.Errorf("递过去的任务 ID 是 %q，want %q", gotID, "BV1x|cid1")
	}
	if gotTitle != "晴天" {
		t.Errorf("递过去的歌名是 %q，want %q", gotTitle, "晴天")
	}
}

// TestAFailedDownloadDoesNotNotify 钉住「下载**失败**不弹通知」。
//
// ⚠️ **失败不是「下载完成」**：用户不需要一条「下载失败」的系统通知——
// 而且那一档界面上的提示已经有了（读屏软件也念过了）。
func TestAFailedDownloadDoesNotNotify(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := 0
	a.SetDownloadNotifier(func(string, string) { calls++ })

	a.downloadFinished(download.Snapshot{ID: "BV1x|cid1", Title: "晴天", Status: download.StatusError, Err: "网断了"})

	if calls != 0 {
		t.Errorf("下载失败却通知了 %d 次，want 0 次", calls)
	}
}

// TestAPausedDownloadDoesNotNotify 钉住「暂停 / 取消不通知」。
//
// ⚠️ **暂停不是「下完了」**：用户还要接着下（见 `service/download/queue.go`
// 里 `runOne` 的那一支）。念一句「下载完成」是在撒谎，弹一条系统通知也是。
func TestAPausedDownloadDoesNotNotify(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	calls := 0
	a.SetDownloadNotifier(func(string, string) { calls++ })

	a.downloadFinished(download.Snapshot{ID: "BV1x|cid1", Title: "晴天", Status: download.StatusPaused})

	if calls != 0 {
		t.Errorf("暂停却通知了 %d 次，want 0 次", calls)
	}
}

// TestADownloadFinishedWithoutANotifierDoesNotPanic 钉住「界面层没装那一头
// 也不出事」。
//
// ⚠️ **无头测试里那一头常常是没装的**（界面层没起来）——而那一档不许 panic：
// panic 会把整个应用带走，而它带走的正是「下载下完了」那件事。
func TestADownloadFinishedWithoutANotifierDoesNotPanic(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	a.downloadFinished(download.Snapshot{ID: "BV1x|cid1", Title: "晴天", Status: download.StatusDone})
}

// TestTheDownloadIsStillReadOutWhenThereIsNoNotifier 钉住「系统通知那一头没
// 装的时候，那一句「下载完成」照样念」。
//
// ⚠️ **这是两条独立的线**：系统通知是给「用户不在应用里」用的，而读屏软件
// 念的那一句是给「读屏软件用户」用的。后者**不依赖**前者——所以系统通知发
// 不出去（没打包 / 权限被拒）的时候，那一句不能跟着丢。
func TestTheDownloadIsStillReadOutWhenThereIsNoNotifier(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	var said []string
	a.AddAnnounce(func(text string) { said = append(said, text) })

	a.downloadFinished(download.Snapshot{ID: "BV1x|cid1", Title: "晴天", Status: download.StatusDone})

	if len(said) != 1 || !strings.Contains(said[0], "晴天") {
		t.Errorf("念出去的是 %q，want 一句带歌名的「下载完成」", said)
	}
}

// TestABackgroundCheckTellsTheNotifier 钉住「后台查到了新版本会告诉界面层」。
//
// ⚠️ **这一段不许丢事件**（票面第 5 条）：而「丢」在这里是**静默**的——
// 界面层没收到，别的用例照样绿，而用户不在应用里时他一点动静都听不到。
//
// ⚠️ **而只有后台那一次才递**：用户自己按「检查更新」的话，他正盯着设置页
// 看着那句话，系统通知是多余的打扰（见 `update.go` 里 `check` 的注释）。
func TestABackgroundCheckTellsTheNotifier(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", &UpdateRelease{Version: "0.2.0"})

	var got []string
	f.app.SetUpdateNotifier(func(version string) { got = append(got, version) })

	// 后台那一次（user = false）。
	f.checkAndWait(t, false)

	if len(got) != 1 || got[0] != "0.2.0" {
		t.Fatalf("界面层那一头被叫了 %d 次（%v），want 1 次且版本是 0.2.0", len(got), got)
	}
}

// TestACheckTheUserStartedDoesNotTellTheNotifier 钉住「用户自己按的那一次
// 不通知」。
//
// ⚠️ **用户正盯着设置页**：他刚按了「检查更新」，屏幕上正在出结果——
// 再弹一条系统通知是纯粹的打扰。
func TestACheckTheUserStartedDoesNotTellTheNotifier(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", &UpdateRelease{Version: "0.2.0"})

	calls := 0
	f.app.SetUpdateNotifier(func(string) { calls++ })

	f.checkAndWait(t, true)

	if calls != 0 {
		t.Errorf("用户自己按的那一次却通知了 %d 次，want 0 次", calls)
	}
}

// TestAnUpToDateCheckDoesNotTellTheNotifier 钉住「已是最新不通知」。
func TestAnUpToDateCheckDoesNotTellTheNotifier(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", nil)

	calls := 0
	f.app.SetUpdateNotifier(func(string) { calls++ })

	f.checkAndWait(t, false)

	if calls != 0 {
		t.Errorf("已经是最新了却通知了 %d 次，want 0 次", calls)
	}
}

// TestTwoBackgroundChecksDoNotNotifyTheSameVersionTwice 钉住「同一个版本只
// 提醒一次」。
//
// 🔴 **这是票面第 6 条「不重复刷通知」**（票 #559）：后台检查**每小时**一次，
// 而**同一个新版本会被查到很多次**（用户一直不装它就一直查得到）——不记的话，
// 用户每小时都会收到一条一模一样的「有更新了」。
//
// ⚠️ **而稳定 ID 不代替它**：框架那一次 `Show` 是真的会再弹一次（同一个 ID 再
// `Show` 是**重新弹**，不是「什么都不做」），而 ID 只决定通知中心里那一条被
// **替换**。所以「不再发」得自己记一份。
func TestTwoBackgroundChecksDoNotNotifyTheSameVersionTwice(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", &UpdateRelease{Version: "0.2.0"})

	var got []string
	f.app.SetUpdateNotifier(func(version string) { got = append(got, version) })

	// 后台查了两次，同一个版本（用户一直不装它，而检查每小时一次）。
	f.checkAndWait(t, false)
	f.checkAndWait(t, false)

	if len(got) != 1 {
		t.Fatalf("同一个版本通知了 %d 次（%v），want 1 次 —— "+
			"而用户每小时都会收到一条一模一样的「有更新了」", len(got), got)
	}
}

// TestANewVersionIsNotifiedAgain 钉住「换了版本就该再提醒一次」。
//
// ⚠️ **去重不许把「整个更新提醒」关掉**：用户要看的正是「哪一个版本」，而
// 出了新版本他就该知道。
func TestANewVersionIsNotifiedAgain(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", &UpdateRelease{Version: "0.2.0"})

	var got []string
	f.app.SetUpdateNotifier(func(version string) { got = append(got, version) })

	f.checkAndWait(t, false)
	if len(got) != 1 {
		t.Fatalf("第一次提醒了 %d 次（%v），want 1 次", len(got), got)
	}

	// 出了新版本：改掉假后端答的版本号，再查一次。
	f.be.mu.Lock()
	f.be.rel = &UpdateRelease{Version: "0.3.0"}
	f.be.mu.Unlock()
	f.checkAndWait(t, false)

	if len(got) != 2 {
		t.Errorf("换了版本却一共只提醒了 %d 次（%v），want 2 次", len(got), got)
	}
	if got[1] != "0.3.0" {
		t.Errorf("第二次提醒的是 %q，want %q", got[1], "0.3.0")
	}
}

// TestTheUpdateNotificationSwitchTurnsItOff 钉住「用户关了那一格之后，后台检查
// 就不再发通知」。
//
// 🔴 **这是票面第 3 条「更新提醒可复用同一基础设施但另开开关」**（票 #559）：
// 那一格**必须真的管住那条通知**——而它默认是**开**的，所以用户明确关掉之后
// 它得立刻生效。
func TestTheUpdateNotificationSwitchTurnsItOff(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", &UpdateRelease{Version: "0.2.0"})

	// 挂上设置（那一格在设置文件里，而更新器读的是**当前那一份**）。
	if err := f.app.MountSettings(filepath.Join(t.TempDir(), SettingsFileName)); err != nil {
		t.Fatal(err)
	}

	// 用户把那一格关了。
	if _, err := f.app.UpdateSettings(func(s *Settings) {
		off := false
		s.UpdateNotification = &off
	}); err != nil {
		t.Fatal(err)
	}

	calls := 0
	f.app.SetUpdateNotifier(func(string) { calls++ })

	f.checkAndWait(t, false)

	if calls != 0 {
		t.Errorf("用户把「更新提醒」关了，后台检查却还是通知了 %d 次，want 0 次", calls)
	}
}

// TestTheUpdateNotificationSwitchIsOnByDefault 钉住「那一格默认是开的」。
//
// ⚠️ **而「老版本写的设置文件」也按开算**：那一格是 `*bool`（理由见
// `Settings.UpdateNotification`），`nil` = 「没设过」→ 默认开。
func TestTheUpdateNotificationSwitchIsOnByDefault(t *testing.T) {
	if !DefaultSettings("").UpdateNotificationEnabled() {
		t.Error("「更新提醒」那一格默认是关的 —— 而系统通知是低频的好事")
	}
	// 老版本写的设置文件：那一格不存在（nil）→ 仍然按开算。
	s := Settings{}
	if !s.UpdateNotificationEnabled() {
		t.Error("「更新提醒」那一格不存在（老版本写的设置文件）时按关算 —— " +
			"而老用户升级上来不该平白多一条通知的打扰")
	}
	// 用户明确关了 → 关。
	off := false
	s.UpdateNotification = &off
	if s.UpdateNotificationEnabled() {
		t.Error("用户明确关了「更新提醒」，却还按开算")
	}
}
