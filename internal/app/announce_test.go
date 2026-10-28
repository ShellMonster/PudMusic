package app

import (
	"strings"
	"sync"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/service/download"
)

// 这一组盯的是「状态变化被念出来」的那一半（无障碍，票 #321）。
//
// 应用层只做两件事：**攒一句人话**，**把它送到每一扇窗**。真正念出去的是界面层
// （`internal/ui/announce.go`），而那一头有一道硬约束：框架的 `Context.Announce`
// 只在**建视图的那一段**里有效，帧外调它是静默无效的——所以话必须先攒着。
//
// 这里验的是「攒和送」这两半：话攒对了、送到了每一扇窗、关掉的那一扇不再听到。

// announceCollector 收念出去的那些话。
type announceCollector struct {
	mu    sync.Mutex
	texts []string
}

// sink 是装给 App 的那一句。
func (c *announceCollector) sink(text string) {
	c.mu.Lock()
	c.texts = append(c.texts, text)
	c.mu.Unlock()
}

// take 取走收着的那些。
func (c *announceCollector) take() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.texts
	c.texts = nil
	return out
}

// newAnnounceApp 建一个只用来验播报的 App。
func newAnnounceApp(t *testing.T) *App {
	t.Helper()
	a := New()
	t.Cleanup(a.Close)
	return a
}

// TestAPlaybackFailureIsReadOut 钉住「播放失败」这一句。
//
// 🔴 **这一句是读屏软件用户唯一的线索**：歌停了，播放条上多了一行红字——而他
// 既看不见那一行，也没有耳朵去等歌响起来。
func TestAPlaybackFailureIsReadOut(t *testing.T) {
	a := newAnnounceApp(t)
	c := &announceCollector{}
	a.AddAnnounce(c.sink)

	a.setErrText("等网络好一些", true)

	got := c.take()
	if len(got) != 1 || got[0] != "播放失败：等网络好一些" {
		t.Fatalf("念出去的是 %q，want 一句「播放失败：等网络好一些」", got)
	}
}

// TestClearingTheErrorIsNotReadOut 钉住「空话不念」。
//
// `setErr("")` 是**把上一句错收掉**（见 online.go），不是又出了一句错。
// 念出去的话就是「播放失败：」——那比不念还糟。
func TestClearingTheErrorIsNotReadOut(t *testing.T) {
	a := newAnnounceApp(t)
	c := &announceCollector{}
	a.AddAnnounce(c.sink)

	a.setErrText("", false)

	if got := c.take(); len(got) != 0 {
		t.Fatalf("把错收掉也念了一句：%q", got)
	}
}

// TestALocalFileThatCannotBeOpenedIsReadOut 钉住本地文件打不开那一句。
//
// ⚠️ **本地文件那条路不经过 `setErrText`**（它自己写 `errText`，见 app.go 的
// `Open`），所以它得自己念——漏了它的话，本地文件打不开在读屏软件那里是**无声的**。
func TestALocalFileThatCannotBeOpenedIsReadOut(t *testing.T) {
	a := newAnnounceApp(t)
	c := &announceCollector{}
	a.AddAnnounce(c.sink)

	// 一个压根不存在的路径：`audio.Open` 一定失败。
	_ = a.Open("/biu-go/这个文件不存在.m4a")

	got := c.take()
	if len(got) != 1 || !strings.HasPrefix(got[0], "播放失败：") {
		t.Fatalf("念出去的是 %q，want 一句「播放失败：…」", got)
	}
}

// TestADownloadThatFinishedIsReadOut 钉住「下载完成」那一句。
//
// 🔴 **这一句之前根本没有**：下载下完了，下载记录页上那一列变成「下载完成」加
// 一枚绿勾——而用户多半在别的页，**他既看不见那个勾，也听不见任何动静**。
func TestADownloadThatFinishedIsReadOut(t *testing.T) {
	a := newAnnounceApp(t)
	c := &announceCollector{}
	a.AddAnnounce(c.sink)

	a.downloadFinished(download.Snapshot{Title: "晴天", Status: download.StatusDone})

	got := c.take()
	if len(got) != 1 || got[0] != "《晴天》下载完成" {
		t.Fatalf("念出去的是 %q，want 一句「《晴天》下载完成」", got)
	}
}

// TestTheAnnouncedErrorIsNotTheRawGoError 钉住「念出来的不是英文系统错」。
//
// 🔴 **读屏软件是逐字念的**：而 `err.Error()` 是
// "open /Users/x/a.m4a: no such file or directory"——用户听到的是一串他完全
// 不懂的英文路径。
func TestTheAnnouncedErrorIsNotTheRawGoError(t *testing.T) {
	a := newAnnounceApp(t)
	c := &announceCollector{}
	a.AddAnnounce(c.sink)

	_ = a.Open("/biu-go/这个文件不存在.m4a")

	got := c.take()
	if len(got) != 1 {
		t.Fatalf("念出去的是 %q", got)
	}
	if strings.Contains(got[0], "no such file") || strings.Contains(got[0], "/biu-go/") {
		t.Fatalf("念出去的是原始的 Go 错串：%q", got[0])
	}
	if !strings.Contains(got[0], "打不开这个文件") {
		t.Fatalf("念出去的是 %q，want 一句人话", got[0])
	}
}

// TestTheAnnouncedDownloadErrorHasNoInternalPrefix 钉住「内部前缀不念出去」。
//
// "download: " 是**我们自己的说法**，用户听不懂；而读屏软件是逐字念的。
func TestTheAnnouncedDownloadErrorHasNoInternalPrefix(t *testing.T) {
	a := newAnnounceApp(t)
	c := &announceCollector{}
	a.AddAnnounce(c.sink)

	a.downloadFinished(download.Snapshot{
		Title: "晴天", Status: download.StatusError, Err: "download: 拉字节：连接被重置",
	})

	got := c.take()
	if len(got) != 1 {
		t.Fatalf("念出去的是 %q", got)
	}
	if strings.Contains(got[0], "download: ") {
		t.Fatalf("念出去的是带内部前缀的：%q", got[0])
	}
	if !strings.Contains(got[0], "连接被重置") {
		t.Fatalf("念出去的是 %q，want 那半句错还在（只是前缀没了）", got[0])
	}
}

// TestADownloadThatFailedIsReadOut 钉住「下载失败」那一句。
func TestADownloadThatFailedIsReadOut(t *testing.T) {
	a := newAnnounceApp(t)
	c := &announceCollector{}
	a.AddAnnounce(c.sink)

	a.downloadFinished(download.Snapshot{Title: "晴天", Status: download.StatusError, Err: "网断了"})

	got := c.take()
	if len(got) != 1 || got[0] != "《晴天》下载失败：网断了" {
		t.Fatalf("念出去的是 %q，want 一句「《晴天》下载失败：网断了」", got)
	}
}

// TestAPausedDownloadIsNotReadOut 钉住「暂停不算完了」。
//
// ⚠️ **暂停 / 取消不是「下完了」**：用户还要接着下，而念一句「下载完成」是在
// 说他没干过的那件事。
func TestAPausedDownloadIsNotReadOut(t *testing.T) {
	a := newAnnounceApp(t)
	c := &announceCollector{}
	a.AddAnnounce(c.sink)

	a.downloadFinished(download.Snapshot{Title: "晴天", Status: download.StatusPaused})

	if got := c.take(); len(got) != 0 {
		t.Fatalf("暂停也念了一句：%q", got)
	}
}

// TestEveryWindowHearsTheSameThing 钉住「挨个送」。
//
// 🔴 **两个窗口共用一个 App**（主窗口和迷你播放器），而「攒一份让界面来取」是
// **有破坏性**的：谁先画一帧谁就取走了，于是迷你开着的时候那一句可能被收起来的
// 主窗口吃掉，迷你窗口永远听不到。
func TestEveryWindowHearsTheSameThing(t *testing.T) {
	a := newAnnounceApp(t)
	main, mini := &announceCollector{}, &announceCollector{}
	a.AddAnnounce(main.sink)
	a.AddAnnounce(mini.sink)

	a.announce("下载完成")

	if got := main.take(); len(got) != 1 || got[0] != "下载完成" {
		t.Fatalf("主窗口听到的是 %q", got)
	}
	if got := mini.take(); len(got) != 1 || got[0] != "下载完成" {
		t.Fatalf("迷你窗口听到的是 %q", got)
	}
}

// TestAWindowThatClosedStopsHearing 钉住「关掉的那一扇不再听」。
//
// 「进迷你 / 退出迷你」来回十次之后那个列表就有十条，每发生一件事就多走十趟空转。
func TestAWindowThatClosedStopsHearing(t *testing.T) {
	a := newAnnounceApp(t)
	gone, alive := &announceCollector{}, &announceCollector{}
	remove := a.AddAnnounce(gone.sink)
	a.AddAnnounce(alive.sink)

	remove()
	a.announce("下载完成")

	if got := gone.take(); len(got) != 0 {
		t.Fatalf("关掉的那一扇还听着：%q", got)
	}
	if got := alive.take(); len(got) != 1 {
		t.Fatalf("还活着的那一扇听到的是 %q", got)
	}
}

// TestAnEmptyAnnouncementIsNotSent 钉住「空话不送」。
//
// 一句空话在读屏软件那里是「什么也没说」，而在别处它却像一句说过的话。
func TestAnEmptyAnnouncementIsNotSent(t *testing.T) {
	a := newAnnounceApp(t)
	c := &announceCollector{}
	a.AddAnnounce(c.sink)

	a.announce("")

	if got := c.take(); len(got) != 0 {
		t.Fatalf("空话也送了：%q", got)
	}
}
