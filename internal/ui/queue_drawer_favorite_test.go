package ui

// 抽屉行菜单里「收藏」和「下载视频」这两项的端到端证据。
//
// 这两条走的是**整条链**：真的菜单项 → 真的 app 口 → 真的服务 → 真的（假）B 站 /
// 真的下载管理器 → 画面上真的出现那句话。断言的是「用户按下去之后发生了什么」，不是
// 「某一层被调到了」。
//
// ⚠️ **收藏是写操作**：这些用例打的是一台假 B 站（`newBiliFixture`），不打真网。
// 真机验证要动用户真实的收藏夹，只能在测试账号自己的夹子里做——见这一票的报告。
//
// 这两条在 `package ui`（不是 `ui_test`）：要复用 `newBiliFixture` 那台假 B 站，它在
// 这个包里。

import (
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// favTester 装一个真的 App + Shell：在线播放的 client 指着假 B 站，下载也挂上。
func favTester(t *testing.T, f *biliFixture) (*app.App, *ui.Tester) {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)
	a.MountStreaming(app.Streaming{
		Client:  f.client(),
		Media:   f.Client(),
		TempDir: t.TempDir(),
	})
	dir := t.TempDir()
	if err := a.MountDownloads(app.Downloads{
		Media:       f.Client(),
		QueueFile:   filepath.Join(dir, "queue.json"),
		FallbackDir: dir,
	}); err != nil {
		t.Fatalf("挂下载：%v", err)
	}
	// ⚠️ 下载队列要在 App 关掉之前清空：`download` 的后台线在队列里还排着队时关
	// 应用会卡住（活锁，见 `internal/service/download` 的 loop）。这一组用例每次都
	// 清一次。
	t.Cleanup(a.ClearDownloads)

	s := New(a)
	t.Cleanup(s.images.Close)
	tt := ui.NewTester(s.View, 760, 460)
	tt.Frame()
	return a, tt
}

// openQueueWithOneSong 摆一个只有一首在线歌的队列，并把抽屉拉出来。
func openQueueWithOneSong(t *testing.T, a *app.App, tt *ui.Tester) {
	t.Helper()
	a.Queue().Append(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1xx", Cid: "c1", Title: "在线那一首",
	})
	tt.Frame()
	if err := tt.Click("播放列表"); err != nil {
		t.Fatalf("点不了播放列表：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	if !tt.HasText("定位当前播放") {
		t.Fatalf("抽屉没开（文字：%q）", tt.Texts())
	}
}

// pickMenu 右键那一行，再从菜单里选 label。
func pickMenu(t *testing.T, tt *ui.Tester, row, label string) {
	t.Helper()
	if err := tt.RightClick(row); err != nil {
		t.Fatalf("右键不了 %q：%v", row, err)
	}
	if err := tt.ChooseMenuItem(label); err != nil {
		t.Fatalf("选不了 %q（菜单是 %q）：%v", label, tt.Menu(), err)
	}
	tt.Frame()
}

// TestTheRowMenuReallyFavoritesTheSong 断的是「按了收藏之后，B 站那边真的多了一条」。
//
// 判据是**写请求真的发出去了、参数是对的**（rid 是 avid 不是 bvid、放进第一格、
// 带 csrf），加上画面上那句「已收藏到「默认收藏夹」」。只断那句话的话，一个
// 「收藏不了：xxx」的失败分支也能把它凑出来。
func TestTheRowMenuReallyFavoritesTheSong(t *testing.T) {
	f := newBiliFixture(t, 1)
	a, tt := favTester(t, f)
	openQueueWithOneSong(t, a, tt)

	pickMenu(t, tt, "在线那一首", "收藏")

	// 收藏是后台做的（要问 B 站四到五次），所以要等它落地。
	waitUI(t, tt, "画面上说收藏成功了", func() bool { return tt.HasText("已收藏到「默认收藏夹」") })

	f.mu.Lock()
	calls := append([]url.Values(nil), f.favoriteCalls...)
	f.mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("发了 %d 次收藏的写请求，want 1：%v", len(calls), calls)
	}
	if got := calls[0].Get("rid"); got != "4242" {
		t.Errorf("写的 rid 是 %q，want 4242（ avid）", got)
	}
	if got := calls[0].Get("add_media_ids"); got != "100" {
		t.Errorf("放进了哪一格：%q，want 100", got)
	}
	if got := calls[0].Get("type"); got != "2" {
		t.Errorf("type 是 %q，want 2（投稿）", got)
	}
}

// TestTheRowMenuSaysSoWhenTheSongIsAlreadyFavorited 去重：已经在那一格里就别再
// 写一次，并且**说清楚是「已经收藏过了」**——那和「连不上 B 站」对用户的意义完全
// 不同（前者说明刚才那一下没白按，后者说明该重试）。
func TestTheRowMenuSaysSoWhenTheSongIsAlreadyFavorited(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.mu.Lock()
	f.favoritedIn = []int64{fixtureFolderID} // 已经在默认收藏夹里
	f.mu.Unlock()

	a, tt := favTester(t, f)
	openQueueWithOneSong(t, a, tt)

	pickMenu(t, tt, "在线那一首", "收藏")
	waitUI(t, tt, "画面上说了已经收藏过", func() bool { return tt.HasText("已经收藏过了") })

	f.mu.Lock()
	n := len(f.favoriteCalls)
	f.mu.Unlock()
	if n != 0 {
		t.Errorf("已经收藏过了还发了 %d 次写请求", n)
	}
}

// TestTheRowMenuReallyDownloadsTheVideo 「下载视频」要真的造出一个**视频**任务。
//
// 判据是队列里有任务、而且它是视频（落盘目标是 `.mp4`）：音频任务和视频任务是两个
// 东西，只断「队列里有一个」的话，接错了也绿。
func TestTheRowMenuReallyDownloadsTheVideo(t *testing.T) {
	f := newBiliFixture(t, 1)
	a, tt := favTester(t, f)
	openQueueWithOneSong(t, a, tt)

	pickMenu(t, tt, "在线那一首", "下载视频")

	snaps := a.DownloadSnapshots()
	if len(snaps) != 1 {
		t.Fatalf("下载队列里有 %d 个任务，want 1", len(snaps))
	}
	if got := filepath.Ext(snaps[0].Dest); got != ".mp4" {
		t.Errorf("任务落在 %q，want .mp4 —— 接的是音频那一路", snaps[0].Dest)
	}
}

// waitUI 轮询到 cond 成立（后台那一层要一会儿）。
func waitUI(t *testing.T, tt *ui.Tester, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等了 10s，%s 还没发生（画面：%q）", what, tt.Texts())
}
