package app

// 批量下载 / 异步下载这一段的接线测试。
//
// **package app 而不是 app_test**：这里要用 `collectionFixture`（`collection_test.go`
// 里那台假 B 站）——它是这个包私有的，而这一组要验的正是「拿那条内容列表的
// 结果去问分 P、再进队列」这条线，拆到外面就得再造一台一模一样的。
//
// ⚠️ fixture 是假的 B 站：它证明的是「我们问到了分 P、把它带进队列了」，不是
// 「B 站真的会让那一首下完」。后者是下载引擎的事（`service/download` 的包内
// 测试）。

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// downloadApp 建一个挂了歌单、取流和下载三样的 App，都指着那台假 B 站。
func downloadApp(t *testing.T, f *collectionFixture) *App {
	t.Helper()
	a := New()
	t.Cleanup(a.Close)

	if err := a.MountSettings(filepath.Join(t.TempDir(), SettingsFileName)); err != nil {
		t.Fatalf("挂设置：%v", err)
	}
	t.Cleanup(func() { _ = a.CloseSettings() })

	cli := clientOn(t, f)
	// 取流这一摊要**同一个**客户端（理由同 `ui.MountStreaming`）：cid 是问它
	// 要的，而凭据边界是同一套规则。
	//
	// ⚠️ Media 也要给：`streamingSnapshot` 缺 Media 就当没挂，而这一组要的
	// 恰恰是「挂了」那一支（补 cid 要问的是 `Client`，但快照先卡 Media）。
	a.MountStreaming(Streaming{Client: cli, Media: f.Server.Client(), Quality: bili.AudioHigh})
	if err := a.MountDownloads(Downloads{
		Media:       f.Server.Client(),
		QueueFile:   filepath.Join(t.TempDir(), "queue.json"),
		FallbackDir: t.TempDir(),
	}); err != nil {
		t.Fatalf("挂下载：%v", err)
	}
	// ⚠️ `Downloader: a`：**生产上挂的就是 App 自己**（`ui.MountCollections`）。
	// 照抄那一行，否则验的是「一个桩接上了」而不是「真的接上了」。
	a.MountCollections(CollectionRefs{Client: cli, Downloader: a})
	return a
}

// servePagelist 让那台假 B 站答得出「这个稿件有哪几集」。
//
// ⚠️ **t 要显式传进来**：替换掉的那一段是在 httptest 自己的 goroutine 上跑的，
// 而 fixture 自己没存 `t`（`newCollectionFixture` 的 `t.Helper()` 之外没留）。
func servePagelist(t *testing.T, f *collectionFixture, cids map[string]int64) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.overrides["/x/player/pagelist"] = func(w http.ResponseWriter, r *http.Request) {
		cid, ok := cids[r.URL.Query().Get("bvid")]
		if !ok {
			writeCollectionJSON(t, w, -404, "稿件不见了", nil)
			return
		}
		writeCollectionJSON(t, w, 0, "0", []map[string]any{
			{"cid": cid, "page": 1, "part": "P1", "duration": 10},
		})
	}
}

// queueIDs 是下载队列此刻那几项的身份（`<bvid>|<cid>`）。
func queueIDs(a *App) []string {
	out := make([]string, 0)
	for _, s := range a.DownloadSnapshots() {
		out = append(out, s.ID)
	}
	return out
}

// TestTheBatchDownloadAsksForThePartItIsMissing 批量下载**会自己补 cid**。
//
// 🔴 这是这一段存在的全部理由：收藏夹的内容列表接口**不给 cid**（`bili.Media`
// 里没有这个字段），而下载队列的判重（`<bvid>|<cid>`）和落盘名都要它。不补的话
// 界面上「批量下载」按下去每一首都是「还没取到分 P id」。
func TestTheBatchDownloadAsksForThePartItIsMissing(t *testing.T) {
	f := newCollectionFixture(t)
	servePagelist(t, f, map[string]int64{"BV1": 7001, "BV2": 7002})
	a := downloadApp(t, f)

	if err := a.DownloadMedias(t.Context(), testRef, []bili.Media{
		{ID: 1, Type: bili.MediaVideo, Bvid: "BV1", Title: "歌 一"},
		{ID: 2, Type: bili.MediaVideo, Bvid: "BV2", Title: "歌 二"},
	}); err != nil {
		t.Fatalf("批量下载：%v", err)
	}

	got := queueIDs(a)
	want := []string{"BV1|7001", "BV2|7002"}
	if !slices.Equal(got, want) {
		t.Fatalf("队列里是 %v，want %v——分 P 那一次没问到", got, want)
	}
}

// TestTheBatchDownloadSaysWhatDidNotGetThrough 「下不掉的要说出来」。
//
// 合集和 B 站音乐库里的音频条目没有下载通道。**闷头跳过**的话界面上只会说
// 「下载任务已添加」，而用户勾了 5 首只下了 3 首——那种「少做了一半还说做完了」
// 比报错难查得多（同一个道理见 `ui/collectionbatch.go` 的 startCollectionDelete）。
func TestTheBatchDownloadSaysWhatDidNotGetThrough(t *testing.T) {
	f := newCollectionFixture(t)
	servePagelist(t, f, map[string]int64{"BV1": 7001})
	a := downloadApp(t, f)

	err := a.DownloadMedias(t.Context(), testRef, []bili.Media{
		{ID: 1, Type: bili.MediaVideo, Bvid: "BV1", Title: "歌 一"},
		// 合集（21）：没有音频流。
		{ID: 2, Type: 21, Title: "一个合集"},
	})
	if err == nil {
		t.Fatal("一首都没进去却当成功")
	}
	for _, want := range []string{"已加进 1 首", "另有 1 首"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("那句话 %q 里没有 %q", err.Error(), want)
		}
	}
}

// TestTheBatchDownloadSaysSoWhenNothingCanGoDown 一首都进不去时说得清是为什么。
func TestTheBatchDownloadSaysSoWhenNothingCanGoDown(t *testing.T) {
	f := newCollectionFixture(t)
	a := downloadApp(t, f)

	err := a.DownloadMedias(t.Context(), testRef, []bili.Media{
		{ID: 1, Type: bili.MediaAudio, Title: "音乐库那一首"},
	})
	if err == nil {
		t.Fatal("一条都下不了却当成功")
	}
	if !strings.Contains(err.Error(), "一首也没能加进下载队列") {
		t.Errorf("那句话 %q 没说出「一首都没进去」", err.Error())
	}
}

// TestDownloadItemGoesAndGetsThePart 界面上「下载音频」按钮走的那一条。
//
// 与 `TestTheBatchDownloadAsksForThePartItIsMissing` 的区别有两条，都单独钉：
// **它立刻返回**（界面线程上不能等网络），而**分 P 是它在后台问的**。
func TestDownloadItemGoesAndGetsThePart(t *testing.T) {
	f := newCollectionFixture(t)
	servePagelist(t, f, map[string]int64{"BV9": 9001})
	a := downloadApp(t, f)

	done := make(chan error, 1)
	a.DownloadItem(Item{Source: "online", Type: "mv", Bvid: "BV9", Title: "动态里那一首"},
		func(err error) { done <- err })

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("下载：%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("5 秒还没回来")
	}
	if got := queueIDs(a); len(got) != 1 || got[0] != "BV9|9001" {
		t.Errorf("队列里是 %v，want [BV9|9001]", got)
	}
}

// TestDownloadItemRefusesALocalFile 按钮按在本地文件上要有一句人话。
func TestDownloadItemRefusesALocalFile(t *testing.T) {
	f := newCollectionFixture(t)
	a := downloadApp(t, f)

	var got string
	a.DownloadItem(LocalItem("/tmp/一首歌.m4a"), func(err error) {
		if err != nil {
			got = err.Error()
		}
	})
	if !strings.Contains(got, "磁盘") {
		t.Errorf("本地文件那句话是 %q，没有说清它已经在磁盘上了", got)
	}
	if n := len(a.DownloadSnapshots()); n != 0 {
		t.Errorf("本地文件也进了下载队列（%d 项）", n)
	}
}

// TestDownloadingAMusicLibraryEntrySaysThereIsNoDownloadPath 音乐库那一行按下载。
//
// 🔴 **这一条是 BMG-50 带出来的回归**：那一票让 `playableOnline` 放行了 sid 条目
// （它现在真放得出来），而下载那一路是按**稿件 + 分 P** 取流的，音乐库条目只有
// auid —— 没有 bvid 也没有 cid。不在这里挡的话它会一路走到 `withCid` 去问一个
// 空稿件的分 P，问出来的是「问不出这一集是哪一集」，而那句话说的是
// 「等网络好一些」——**把一件永远不可能成功的事说成网络问题**：用户会去查网、
// 换网络、等它好，而真正要做的跟网络一点关系都没有。
//
// 批量那一早就这么拒了（`downloadableItem`），这里是同一个错在另一个出口。
// 两条必须说**同一句话**——它们在同一个界面上。
func TestDownloadingAMusicLibraryEntrySaysThereIsNoDownloadPath(t *testing.T) {
	for _, c := range []struct {
		what string
		run  func(a *App, it Item, done func(error))
	}{
		{"下载音频", func(a *App, it Item, done func(error)) { a.DownloadItem(it, done) }},
		{"下载视频", func(a *App, it Item, done func(error)) { a.DownloadItemVideo(it, done) }},
	} {
		f := newCollectionFixture(t)
		a := downloadApp(t, f)

		// ⚠️ **要等 `done` 被调，不能同步去读那个变量**：`downloadLater` 的活儿在
		// 后台 goroutine 上（补 cid 要问 B 站），同步读读到的是还没写进去的零值
		// ——那么这条用例在「闸门被删掉」的时候也会绿（它读到 nil 就当对了）。
		got := make(chan error, 1)
		c.run(a, Item{Source: "online", Type: "audio", Sid: "1293494", Title: "音乐库的一首"},
			func(err error) { got <- err })
		err := waitOneErr(t, got)

		if !errors.Is(err, ErrNoAudioDownload) {
			t.Errorf("%s：错是 %v，want ErrNoAudioDownload", c.what, err)
		}
		// 一件永远不可能成功的事，**不许说成网络问题**。
		if msg := bili.Friendly(err); strings.Contains(msg, "网络") || strings.Contains(msg, "问不出") {
			t.Errorf("%s：给用户的话是 %q，把一件下不了的事说成了网络问题", c.what, msg)
		}
		if n := len(a.DownloadSnapshots()); n != 0 {
			t.Errorf("%s：音乐库条目进了下载队列（%d 项）", c.what, n)
		}
	}
}

// TestBothDownloadLanesSayTheSameThingAboutMusicLibrary 两句话不许分叉。
//
// 同一页上两个出口说的是同一件事：一处改了字面量、另一处没改，用户对照着看就会
// 发现「同一个东西两句话」，而那比两句话都说错更难解释。
func TestBothDownloadLanesSayTheSameThingAboutMusicLibrary(t *testing.T) {
	f := newCollectionFixture(t)
	a := downloadApp(t, f)

	fromSingle := make(chan error, 1)
	a.DownloadItem(Item{Source: "online", Type: "audio", Sid: "1293494", Title: "音乐库的一首"},
		func(err error) { fromSingle <- err })

	_, err := a.downloadableItem(context.Background(), bili.Media{ID: 1293494, Type: bili.MediaAudio})
	single := waitOneErr(t, fromSingle)
	if !errors.Is(err, ErrNoAudioDownload) {
		t.Errorf("批量那一路的错是 %v，want ErrNoAudioDownload", err)
	}
	if err.Error() != single.Error() {
		t.Errorf("两句话分叉了：批量 %q / 单首 %q", err.Error(), single.Error())
	}
}

// waitOneErr 等下载那一趟把结果交回来。
//
// 理由见上面那条的注释：`done` 跑在后台 goroutine 上，同步读读到的是零值——
// 而「读到零值」在闸门被删掉的时候恰好长得像「对了」。
func waitOneErr(t *testing.T, ch chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("等了 10 秒，下载那一趟还没把结果交回来")
		return nil
	}
}

// TestDownloadItemRefusesWhenDownloadsAreNotMounted 没挂下载服务时说清楚。
func TestDownloadItemRefusesWhenDownloadsAreNotMounted(t *testing.T) {
	f := newCollectionFixture(t)
	a := New()
	t.Cleanup(a.Close)
	cli := clientOn(t, f)
	a.MountStreaming(Streaming{Client: cli, Media: f.Server.Client(), Quality: bili.AudioHigh})
	a.MountCollections(CollectionRefs{Client: cli, Downloader: a})

	var got string
	a.DownloadItem(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"}, func(err error) {
		if err != nil {
			got = err.Error()
		}
	})
	if got == "" {
		t.Fatal("没挂下载服务却什么都没说")
	}
}

// TestAnItemWithoutAPartStillCannotBeDownloadedSynchronously 搜索结果那一页
// **如实拒绝**。
//
// 这一条钉的是那个「别为了统一而让它假装能下」的取舍：搜索接口只给 bvid，
// 而 `App.Download` 是同步的（界面线程上一次网络往返都不许有），所以它**不去
// 问**分 P，直接拒绝。偷偷问了就等于把「多 P 的第三集」下成第一集还不报错。
func TestAnItemWithoutAPartStillCannotBeDownloadedSynchronously(t *testing.T) {
	f := newCollectionFixture(t)
	servePagelist(t, f, map[string]int64{"BV1": 7001})
	a := downloadApp(t, f)

	_, err := a.Download(Item{Source: "online", Type: "mv", Bvid: "BV1", Title: "搜出来的"})
	if err == nil {
		t.Fatal("没有 cid 却下成了")
	}
	if !strings.Contains(err.Error(), "分 P") {
		t.Errorf("那句话 %q 没有说清是缺分 P", err.Error())
	}
	if n := len(a.DownloadSnapshots()); n != 0 {
		t.Errorf("它还是进了下载队列（%d 项）", n)
	}
}

// TestAnInvalidEntryIsNotDownloaded 钉住「下载那一路也滤」。
//
// 🔴 **这一条是审查补上的**（票 #624 的评审第 3 条）：下载那一路原先只判
// 「有没有音频流」，而失效的条目照样能进下载队列——于是它先问一次分 P
// （`/x/player/pagelist`，一趟网络往返），再进队列让下载器去取流（**又一趟**
// 带签名的往返）。**两趟白跑**，而那一首的稿件已经被删了。
func TestAnInvalidEntryIsNotDownloaded(t *testing.T) {
	f := newCollectionFixture(t)
	a := downloadApp(t, f)

	_, err := a.downloadableItem(context.Background(), bili.Media{
		ID: 1293494, Type: bili.MediaVideo, Bvid: "BVdead", Title: "失效的那首", Attr: 1,
	})
	if err == nil {
		t.Fatal("失效的那一条被排进了下载队列")
	}
	var u *Unplayable
	if !errors.As(err, &u) || u.Kind != UnplayableInvalid {
		t.Errorf("拒它的理由是 %v，want 一个 UnplayableInvalid", err)
	}
}
