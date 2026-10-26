package app

import (
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
