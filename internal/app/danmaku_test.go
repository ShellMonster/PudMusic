package app

// 弹幕这一摊的测试。
//
// 四件事各自要钉住：
//
//	问        换歌就重问，迟到的结果不许落到新歌上
//	防重入    界面每一帧问一次，只发一趟
//	缓存      7 天内再进是瞬时的（不发请求），过期了会重新拉
//	没有 cid  快照为空，一个请求都不发

import (
	"bytes"
	"compress/flate"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/service/danmaku"
)

// danmakuCid 是这些用例里用的分 P id。
const danmakuCid = "112233"

// putDanmakuCurrent 把「正在放的那一首」摆成某一集（**不真的取流**）。
//
// ⚠️ **两样都要摆**：队列当前项（`CurrentItem`）和 `pending`（「用户按的是哪一
// 首」，见 `playOnlineFrom`）。弹幕这一摊认的是**后者**——判据见
// `danmakuTarget` 的注释：队列当前项在「放本地文件」「停了」「删了当前曲」这
// 几种情况下还指着上一首在线的歌，照它认的话用户放本地文件时屏幕上滚的是上一首
// 的弹幕。
//
// ⚠️ **cid 也要给**：弹幕是按 cid 组织的，而没有 cid 的那一支是另一条用例
// （`TestNoCidMeansNoSnapshotAndNoRequest`）。
func putDanmakuCurrent(a *App, bvid, cid string) {
	it := Item{Source: "online", Type: "mv", Bvid: bvid, Cid: cid, Title: "一首"}
	a.Queue().Append(it)
	a.queue.selectAt(len(a.Queue().Items()) - 1)
	a.mu.Lock()
	a.pending = it
	a.mu.Unlock()
}

// askedCid 记下问过的 cid（按顺序）。
//
// ⚠️ 它是被后台那个 goroutine 写的，所以带锁（`-race` 会报）。
type askedCid struct {
	mu  sync.Mutex
	ids []string
}

func (a *askedCid) record(_ context.Context, cid string) ([]bili.Danmaku, error) {
	a.mu.Lock()
	a.ids = append(a.ids, cid)
	a.mu.Unlock()
	return []bili.Danmaku{{TimeMS: 1000, Text: "一条", ID: "1"}}, nil
}

func (a *askedCid) len() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.ids)
}

func (a *askedCid) all() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.ids...)
}

// waitDanmaku 轮询到 cond 成立（后台那一趟是 goroutine）。
func waitDanmaku(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等了 2 秒，%s 还没发生", what)
}

// ---- 问 ----

// TestTheDanmakuIsAskedPerSong 钉住「弹幕是**这一集**的事，换歌就重问」。
func TestTheDanmakuIsAskedPerSong(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	asked := &askedCid{}
	a.danmakuFetch = asked.record

	putDanmakuCurrent(a, "BVfirst", danmakuCid)
	a.Danmaku()
	waitDanmaku(t, "第一集问过了", func() bool { return asked.len() == 1 })

	putDanmakuCurrent(a, "BVsecond", danmakuCid)
	a.Danmaku()
	waitDanmaku(t, "第二集也问了", func() bool { return asked.len() == 2 })
	if got := asked.all(); got[0] != danmakuCid || got[1] != danmakuCid {
		t.Errorf("问的是 %v", got)
	}
}

// TestALateAnswerFromTheOldSongNeverLands 钉住代次闸门。
//
// 换歌那一瞬最容易出的错：上一首那一趟还在路上，回来的时候按「上一首」把答案
// 装上去——于是「我在听 B，屏幕上滚的是 A 的弹幕」。
//
// ⚠️ **等的是唤醒，不是 sleep**：装上状态之后第一件事就是叫醒界面，所以
// 「叫醒过 2 次」正是「两趟都处理完了」。
func TestALateDanmakuFromTheOldSongNeverLands(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	gate := make(chan struct{})
	var asked atomic.Int32
	a.danmakuFetch = func(_ context.Context, cid string) ([]bili.Danmaku, error) {
		n := asked.Add(1)
		if n == 1 {
			// 先记下「问过了」（不记的话，测试没法知道那一趟已经出发），
			// 再把第一集卡住。
			<-gate
		}
		return []bili.Danmaku{{TimeMS: 1, Text: cid, ID: cid}}, nil
	}
	var wakes atomic.Int32
	a.SetWake(func() { wakes.Add(1) })

	putDanmakuCurrent(a, "BVfirst", "111")
	a.Danmaku() // 第一趟，卡着
	waitDanmaku(t, "第一趟发出去了", func() bool { return asked.Load() == 1 })

	putDanmakuCurrent(a, "BVsecond", "222")
	a.Danmaku() // 第二趟，立刻回来
	waitDanmaku(t, "第二集问过了", func() bool {
		snap := a.Danmaku()
		return snap.Ready && len(snap.Items) == 1 && snap.Items[0].Text == "222"
	})

	// 放行第一趟：它回来的时候已经过时了，**必须被丢掉**。
	close(gate)
	waitDanmaku(t, "旧的那一趟处理完了", func() bool { return a.danmakuDropped.Load() == 1 })

	snap := a.Danmaku()
	if len(snap.Items) != 1 || snap.Items[0].Text != "222" {
		t.Errorf("旧歌的弹幕盖掉了新歌的：%+v", snap.Items)
	}
}

// TestEveryFrameAsksButOnlyOneGoesOut 钉住防重入。
//
// 界面每一帧都问一次，而那一趟是网络往返——不挡住的话一次播放能发几百个请求
// （B 站会风控，而症状是「怎么问不出弹幕」）。
func TestEveryFrameAsksButOnlyOneGoesOut(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	var asked atomic.Int32
	a.danmakuFetch = func(context.Context, string) ([]bili.Danmaku, error) {
		asked.Add(1)
		return []bili.Danmaku{{Text: "x", ID: "1"}}, nil
	}

	putDanmakuCurrent(a, "BV1", danmakuCid)
	for range 50 {
		a.Danmaku()
	}
	waitDanmaku(t, "那一趟回来了", func() bool { return a.Danmaku().Ready })
	// 再问 50 帧：已经答过了，不该再发。
	for range 50 {
		a.Danmaku()
	}
	if got := asked.Load(); got != 1 {
		t.Errorf("发了 %d 趟，want 1", got)
	}
}

// TestNoCidMeansNoSnapshotAndNoRequest 盯「没有 cid」那一支。
//
// ⚠️ **一个请求都不许发**：本地文件和音乐库条目都走这一支，而它们永远没有
// 弹幕——为一段压根不存在的东西发请求是这一层最不该犯的错。
func TestNoCidMeansNoSnapshotAndNoRequest(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	var asked atomic.Int32
	a.danmakuFetch = func(context.Context, string) ([]bili.Danmaku, error) {
		asked.Add(1)
		return nil, nil
	}

	// 本地文件。
	a.Queue().Append(LocalItem("/tmp/一首歌.mp3"))
	a.queue.selectAt(0)
	for range 10 {
		a.Danmaku()
	}
	if got := asked.Load(); got != 0 {
		t.Errorf("本地文件发了 %d 趟请求", got)
	}
	snap := a.Danmaku()
	if snap.Ready || len(snap.Items) != 0 {
		t.Errorf("本地文件的快照是 %+v，want 空的", snap)
	}
	if a.DanmakuWorthAsking() {
		t.Error("本地文件被判成「有弹幕可问」了")
	}

	// 在线条目但没有 cid（搜索结果那一类）。
	a2 := New()
	t.Cleanup(a2.Close)
	a2.danmakuFetch = func(context.Context, string) ([]bili.Danmaku, error) {
		asked.Add(1)
		return nil, nil
	}
	putDanmakuCurrent(a2, "BV1", "")
	for range 10 {
		a2.Danmaku()
	}
	if got := asked.Load(); got != 0 {
		t.Errorf("没有 cid 的在线条目发了 %d 趟请求", got)
	}
	if a2.DanmakuWorthAsking() {
		t.Error("没有 cid 的在线条目被判成「有弹幕可问」了")
	}
}

// TestTheSnapshotIsEmptyUntilTheAnswerArrives 盯「问的那一趟还在路上」那一档。
//
// ⚠️ **空的是「还没有弹幕」，不是「没有弹幕」**：界面要靠 `Ready` 分这两者。
func TestTheSnapshotIsEmptyUntilTheAnswerArrives(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	gate := make(chan struct{})
	a.danmakuFetch = func(context.Context, string) ([]bili.Danmaku, error) {
		<-gate
		return []bili.Danmaku{{Text: "x", ID: "1"}}, nil
	}
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	})

	putDanmakuCurrent(a, "BV1", danmakuCid)
	snap := a.Danmaku()
	if snap.Ready {
		t.Error("那一趟还在路上，却说「有答案了」")
	}
	if len(snap.Items) != 0 {
		t.Errorf("那一趟还在路上，却有 %d 条弹幕", len(snap.Items))
	}

	close(gate)
	waitDanmaku(t, "答案来了", func() bool { return a.Danmaku().Ready })
}

// TestAFailedAskIsReportedAndDoesNotSpin 盯「问失败」那一档。
//
// ⚠️ **不许每秒一发**：界面每一帧都问一次，没有退避的话失败一次就是每秒一发
// （B 站会风控）。
func TestAFailedAskIsReportedAndDoesNotSpin(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	var asked atomic.Int32
	a.danmakuFetch = func(context.Context, string) ([]bili.Danmaku, error) {
		asked.Add(1)
		return nil, errors.New("问不出来")
	}

	putDanmakuCurrent(a, "BV1", danmakuCid)
	for range 20 {
		a.Danmaku()
	}
	waitDanmaku(t, "那一趟失败了", func() bool { return a.DanmakuErr() != nil })
	for range 20 {
		a.Danmaku()
	}
	if got := asked.Load(); got != 1 {
		t.Errorf("失败之后又发了 %d 趟（want 1：退避期内不该再问）", got)
	}
}

// ---- 缓存 ----

// TestTheSecondTimeIsInstant 盯「同一集第二次进是瞬时的」。
//
// ⚠️ **一个请求都不许发**：这一份 7 天之内都该直接读缓存。
func TestTheSecondTimeIsInstant(t *testing.T) {
	dir := t.TempDir()
	a := New()
	t.Cleanup(a.Close)
	a.MountDanmaku(danmaku.NewCache(dir))

	var asked atomic.Int32
	a.danmakuFetch = func(context.Context, string) ([]bili.Danmaku, error) {
		asked.Add(1)
		return []bili.Danmaku{{TimeMS: 42, Text: "缓存里那一条", ID: "1"}}, nil
	}

	putDanmakuCurrent(a, "BV1", danmakuCid)
	a.Danmaku()
	waitDanmaku(t, "第一趟回来了", func() bool { return a.Danmaku().Ready })
	// 落盘是**锁外**的，稍等它写完。
	waitDanmaku(t, "落盘了", func() bool {
		_, ok := danmaku.NewCache(dir).Load("BV1", danmakuCid)
		return ok
	})

	// 第二个 App（模拟「关掉再开」）：同一个缓存目录。
	b := New()
	t.Cleanup(b.Close)
	b.MountDanmaku(danmaku.NewCache(dir))
	b.danmakuFetch = func(context.Context, string) ([]bili.Danmaku, error) {
		asked.Add(1)
		return nil, errors.New("不该发请求")
	}
	putDanmakuCurrent(b, "BV1", danmakuCid)
	snap := b.Danmaku()
	waitDanmaku(t, "缓存里的那份出来了", func() bool { return b.Danmaku().Ready })
	snap = b.Danmaku()
	if len(snap.Items) != 1 || snap.Items[0].Text != "缓存里那一条" {
		t.Errorf("缓存里的是 %+v", snap.Items)
	}
	if got := asked.Load(); got != 1 {
		t.Errorf("第二次进发了 %d 趟请求，want 1（只有第一次那趟）", got)
	}
}

// TestAnExpiredCacheIsFetchedAgain 盯「过期后会重新拉」。
func TestAnExpiredCacheIsFetchedAgain(t *testing.T) {
	dir := t.TempDir()
	c := danmaku.NewCache(dir)
	// 8 天前的那一份：过期了。
	if err := c.Save("BV1", danmakuCid, danmaku.Entry{
		Items:     []bili.Danmaku{{Text: "旧的那一份", ID: "old"}},
		FetchedAt: time.Now().Add(-8 * 24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	a := New()
	t.Cleanup(a.Close)
	a.MountDanmaku(c)
	var asked atomic.Int32
	a.danmakuFetch = func(context.Context, string) ([]bili.Danmaku, error) {
		asked.Add(1)
		return []bili.Danmaku{{Text: "新拉的那一份", ID: "new"}}, nil
	}

	putDanmakuCurrent(a, "BV1", danmakuCid)
	a.Danmaku()
	waitDanmaku(t, "重新拉回来了", func() bool { return a.Danmaku().Ready })
	if got := asked.Load(); got != 1 {
		t.Errorf("过期之后发了 %d 趟，want 1", got)
	}
	snap := a.Danmaku()
	if len(snap.Items) != 1 || snap.Items[0].Text != "新拉的那一份" {
		t.Errorf("拿到的是 %+v", snap.Items)
	}
}

// TestTheCacheFileIsNamedAfterTheBvidAndTheCid 钉住落在盘上的那个名字。
func TestTheCacheFileIsNamedAfterTheBvidAndTheCid(t *testing.T) {
	dir := t.TempDir()
	a := New()
	t.Cleanup(a.Close)
	a.MountDanmaku(danmaku.NewCache(dir))
	a.danmakuFetch = func(context.Context, string) ([]bili.Danmaku, error) {
		return []bili.Danmaku{{Text: "x", ID: "1"}}, nil
	}

	putDanmakuCurrent(a, "BV1xx411c7mD", danmakuCid)
	a.Danmaku()
	waitDanmaku(t, "落盘了", func() bool {
		_, err := os.Stat(filepath.Join(dir, "BV1xx411c7mD-"+danmakuCid+".json"))
		return err == nil
	})
}

// TestSwitchingPartsAsksTheNewPart 盯「换集」。
//
// ⚠️ **分集切换是**就地改队列项的 cid**（见 `App.PlayPart`），所以队列当前项的
// cid 变了而 bvid 没变——弹幕必须跟着换集换。
func TestSwitchingPartsAsksTheNewPart(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	asked := &askedCid{}
	a.danmakuFetch = asked.record

	putDanmakuCurrent(a, "BV1", "111")
	a.Danmaku()
	waitDanmaku(t, "第一集问过了", func() bool { return asked.len() == 1 })

	// 换集：bvid 不变，cid 变了。
	putDanmakuCurrent(a, "BV1", "222")
	a.Danmaku()
	waitDanmaku(t, "第二集也问了", func() bool { return asked.len() == 2 })
	if got := asked.all(); got[0] != "111" || got[1] != "222" {
		t.Errorf("问的是 %v，want 先 111 后 222", got)
	}
}

// TestALocalFileHasNoDanmakuEvenWhileTheQueueStillPointsAtTheLastSong 盯
// 「放的是本地文件时，屏幕上不许滚上一首的弹幕」。
//
// ⚠️ **本地文件不进队列**：队列当前的那一项还指着上一首在线的歌。照队列认的话，
// 用户放的是本地文件，屏幕上滚的却是上一首的弹幕——而那一首的封面根本不在
// 屏幕上。
func TestALocalFileHasNoDanmakuEvenWhileTheQueueStillPointsAtTheLastSong(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	var asked atomic.Int32
	a.danmakuFetch = func(context.Context, string) ([]bili.Danmaku, error) {
		asked.Add(1)
		return []bili.Danmaku{{Text: "上一首的弹幕", ID: "1"}}, nil
	}

	putDanmakuCurrent(a, "BV1", danmakuCid)
	a.Danmaku()
	waitDanmaku(t, "上一首问过了", func() bool { return a.Danmaku().Ready })

	// 放本地文件：队列还指着上一首，而「正在放的那一首」已经是本地文件了。
	a.mu.Lock()
	a.pending = Item{}
	a.path = "/tmp/一首歌.mp3"
	a.mu.Unlock()

	snap := a.Danmaku()
	if len(snap.Items) != 0 {
		t.Errorf("放本地文件时还有 %d 条弹幕：%+v", len(snap.Items), snap.Items)
	}
	if a.DanmakuWorthAsking() {
		t.Error("放本地文件时还说「有弹幕可问」")
	}
}

// ---- 生产那一档（真 `bili.Client`） ----

// TestTheProductionPathActuallyAsksBili 盯「生产那一档真能跑通」。
//
// ⚠️ **前面那些用例都替掉了那一趟**（`danmakuFetch`），而生产上那一趟是
// `s.Client.Danmaku`——它走的是**原始 deflate 的 XML** 那条路（不是 JSON），
// 而「那条路通不通」不是本层能保证的。这一条用一台假 B 站走完整的生产路径。
//
// ⚠️ **buvid 那一次也要备上**：弹幕接口带 `Buvid: true`，而 buvid 是从
// `x/frontend/finger/spi` 白拿的——那一趟没备的话请求照样发出去（只是多一次
// 404），而那一次 404 会让 `bili.Client` 把它当成一个错。
func TestTheProductionPathActuallyAsksBili(t *testing.T) {
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/frontend/finger/spi":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"b_3":"B3DM","b_4":"B4DM"}}`))
		case "/x/v1/dm/list.so":
			asked.Add(1)
			if got := r.URL.Query().Get("oid"); got != danmakuCid {
				t.Errorf("问的是 oid=%q，want %q", got, danmakuCid)
			}
			// 响应头照实写：B 站给的就是这两个（原始 deflate 的 XML）。
			var buf bytes.Buffer
			fw, err := flate.NewWriter(&buf, flate.DefaultCompression)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = fw.Write([]byte(`<i><d p="12.34500,1,25,16777215,1792814614,0,ce48882b,2229049562097370368,10">一条弹幕</d></i>`))
			_ = fw.Close()
			w.Header().Set("Content-Type", "text/xml")
			w.Header().Set("Content-Encoding", "deflate")
			_, _ = w.Write(buf.Bytes())
		default:
			http.Error(w, "没有这个接口", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	a := New()
	t.Cleanup(a.Close)
	cli := bili.NewClient(bili.Options{Transport: srv.Client().Transport})
	cli.TestOnlyPointHostsAtFixture(srv.URL)
	a.MountStreaming(Streaming{Client: cli, Media: srv.Client(), TempDir: t.TempDir()})

	putDanmakuCurrent(a, "BV1", danmakuCid)
	a.Danmaku()
	waitDanmaku(t, "弹幕到了", func() bool { return a.Danmaku().Ready })

	snap := a.Danmaku()
	if len(snap.Items) != 1 {
		t.Fatalf("拿到 %d 条，want 1：%+v", len(snap.Items), snap.Items)
	}
	it := snap.Items[0]
	if it.Text != "一条弹幕" {
		t.Errorf("正文是 %q，want 「一条弹幕」", it.Text)
	}
	if it.TimeMS != 12345 {
		t.Errorf("时间是 %d ms，want 12345", it.TimeMS)
	}
	if it.ID != "2229049562097370368" {
		t.Errorf("弹幕 id 是 %q", it.ID)
	}
	if it.Color != 16777215 || it.Mode != 1 || it.FontSize != 25 {
		t.Errorf("样式那一组是 %+v", it)
	}
	if got := asked.Load(); got != 1 {
		t.Errorf("问了几趟，want 1")
	}
}

// ---- 评审提的三条 ----

// TestReviewUnresolvedSameBVidShowsThePreviousPartsDanmaku 盯「同一个 bvid 的
// 新选择，cid 还没问出来的时候，不许拿上一分 P 的弹幕」。
//
// 🔴 **这是评审提的第一条 major**。
//
// 症状：用户在同一个投稿里换了集（B 站的搜索结果只给 bvid，cid 要问一次才
// 知道），而问出来之前，屏幕上滚的还是**上一分 P** 的弹幕——弹幕盖住封面的时候
// 本来就不好认，用户完全看不出发生了什么。
//
// ⚠️ **它是怎么发生的**：判据在 cid 缺了的时候去「正在放的那一条」（`a.online`）
// 借，而那里的 cid 是**上一分 P** 的。借的那一下把两个不同的集当成同一集了。
func TestReviewUnresolvedSameBVidShowsThePreviousPartsDanmaku(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	dir := t.TempDir()
	a.MountDanmaku(danmaku.NewCache(dir))
	a.danmakuFetch = func(_ context.Context, cid string) ([]bili.Danmaku, error) {
		return []bili.Danmaku{{Text: "part " + cid, ID: cid}}, nil
	}

	// 上一分 P：缓存里有 BV1/333。
	putDanmakuCurrent(a, "BV1", "333")
	a.Danmaku()
	waitDanmaku(t, "上一分 P 问过了", func() bool { return a.Danmaku().Ready })
	waitDanmaku(t, "上一分 P 落盘了", func() bool {
		_, ok := danmaku.NewCache(dir).Load("BV1", "333")
		return ok
	})

	// 「正在放的那一条」还带着**上一分 P** 的 cid（切歌那一瞬间它就是旧的，
	// 而新流要等取完了才装上）。
	a.mu.Lock()
	a.online = &onlineTrack{item: Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "333"}}
	a.mu.Unlock()

	// **同一个 bvid 的新选择**：cid 还没问出来（搜索结果那一类）。
	a.mu.Lock()
	a.pending = Item{Source: "online", Type: "mv", Bvid: "BV1"}
	a.mu.Unlock()

	snap := a.Danmaku()
	if len(snap.Items) != 0 {
		t.Errorf("cid 还没问出来，却端出了 %d 条弹幕：%+v", len(snap.Items), snap.Items)
	}
	if snap.Ready {
		t.Error("cid 还没问出来，却说「有答案了」")
	}
	if a.DanmakuWorthAsking() {
		t.Error("cid 还没问出来，却说「有弹幕可问」")
	}
}

// TestReviewAStaleSameKeyFetchOverwritesTheFreshCache 盯「同一个 key 上慢的那一趟
// 不许盖掉新写的那一份」。
//
// 🔴 **这是评审提的第二条 major**。
//
// 症状：重新进这一集，拿到的是**几分钟前**的那一批——而用户什么都没做。
//
// ⚠️ **可见状态是安全的，代次挡得住**：旧的那一趟回来时按代次丢掉，所以屏幕上
// 显示的是新的一批。**但盘上不是**：那一趟照样去写缓存，而且它**写得更晚**——于是
// 新的那份被旧的盖掉了。下次进这一集读到的就是旧的那一批。
//
// 这就是「代次只保护了可见状态，没有保护缓存写入」那个意思。
func TestReviewAStaleSameKeyFetchOverwritesTheFreshCache(t *testing.T) {
	dir := t.TempDir()
	a := New()
	t.Cleanup(a.Close)
	a.MountDanmaku(danmaku.NewCache(dir))

	first := make(chan struct{})
	var calls atomic.Int32
	a.danmakuFetch = func(_ context.Context, cid string) ([]bili.Danmaku, error) {
		n := calls.Add(1)
		switch n {
		case 1:
			// 第一趟：卡住，等它变成「慢的那一趟」。
			<-first
			return []bili.Danmaku{{Text: "stale", ID: "stale"}}, nil
		default:
			return []bili.Danmaku{{Text: "fresh", ID: "fresh"}}, nil
		}
	}

	putDanmakuCurrent(a, "BV1", "333")
	a.Danmaku() // 第一趟，卡着
	waitDanmaku(t, "第一趟发出去了", func() bool { return calls.Load() == 1 })

	// 换走（让第一趟变旧），再换回来：同一个 key，第二趟是新的。
	putDanmakuCurrent(a, "BV2", "222")
	a.Danmaku()
	waitDanmaku(t, "第二趟回来了", func() bool { return a.Danmaku().Ready })
	putDanmakuCurrent(a, "BV1", "333")
	a.Danmaku()
	waitDanmaku(t, "第三趟回来了", func() bool {
		s := a.Danmaku()
		return s.Ready && len(s.Items) == 1 && s.Items[0].Text == "fresh"
	})
	waitDanmaku(t, "新的一份落盘了", func() bool {
		e, ok := danmaku.NewCache(dir).Load("BV1", "333")
		return ok && len(e.Items) == 1 && e.Items[0].Text == "fresh"
	})

	// 放行第一趟（慢的那一趟）：它是旧的，**不许**盖掉新写的那份。
	close(first)
	waitDanmaku(t, "旧的那一趟处理完了", func() bool { return a.danmakuDropped.Load() >= 1 })

	// ⚠️ **等它写盘**：代次丢弃发生在**写盘之前**，所以「被丢弃了」不等于
	// 「写完了」——而这一条要量的正是「它有没有写」。
	//
	// 旧代码里那一趟会**无条件写盘**，于是盘上从 fresh 变成 stale；新代码里
	// 它被序号挡住，盘上一直是 fresh。所以这里轮询到**内容变了**为止（或者
	// 超时——超时就是「没写」，新代码走的就是这一支）。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		e, ok := danmaku.NewCache(dir).Load("BV1", "333")
		if ok && len(e.Items) == 1 && e.Items[0].Text == "stale" {
			t.Fatalf("慢的那一趟盖掉了新的一份：%+v", e.Items)
		}
		time.Sleep(10 * time.Millisecond)
	}

	e, ok := danmaku.NewCache(dir).Load("BV1", "333")
	if !ok {
		t.Fatal("缓存文件没了")
	}
	if len(e.Items) != 1 || e.Items[0].Text != "fresh" {
		t.Errorf("盘上留下的是 %+v，want fresh（慢的那一趟盖掉了新的一份）", e.Items)
	}
}

// TestReviewCloseStillWritesTheCache 盯「Close 返回之后，缓存不再被写」。
//
// ⚠️ **这是评审提的 minor**（不改也不挡路，但这一版顺手修了）。
//
// 那一版的注释写着「代次加一 ⇒ 它不会在应用退出之后还往盘上写一份缓存」——而
// 那句话**不成立**：代次只管可见状态，在途的那一趟照样写盘。
func TestReviewCloseStillWritesTheCache(t *testing.T) {
	dir := t.TempDir()
	a := New()

	gate := make(chan struct{})
	a.MountDanmaku(danmaku.NewCache(dir))
	a.danmakuFetch = func(context.Context, string) ([]bili.Danmaku, error) {
		<-gate
		return []bili.Danmaku{{Text: "after close", ID: "1"}}, nil
	}

	putDanmakuCurrent(a, "BV1", "333")
	a.Danmaku()                       // 那一趟卡着
	time.Sleep(50 * time.Millisecond) // 让它真的发出去了

	a.Close()
	close(gate)
	// 给它一点时间把那一趟走完（它应该什么都不做）。
	time.Sleep(200 * time.Millisecond)

	if _, ok := danmaku.NewCache(dir).Load("BV1", "333"); ok {
		t.Error("Close 之后缓存里多了一份「after close」")
	}
}

// TestTheSameKeyCanBeFetchedAgainAfterItWasWritten 盯「写过一次之后，同一个 key
// 还能再写一次」。
//
// ⚠️ **这是 per-key 序号的一个边界**：写完盘之后那条序号会被删掉（免得表一直
// 长），所以下一次发起的那一趟必须还能写——而它能写，靠的是「发起时把序号
// 记进表里」这一步。
//
// 症状（如果不成立）：这一集第一次存进去了，第二次再存就**存不进去**——而
// 「7 天内再进是瞬时的」那条验收就靠它。
func TestTheSameKeyCanBeFetchedAgainAfterItWasWritten(t *testing.T) {
	dir := t.TempDir()
	a := New()
	t.Cleanup(a.Close)
	a.MountDanmaku(danmaku.NewCache(dir))

	var calls atomic.Int32
	a.danmakuFetch = func(context.Context, string) ([]bili.Danmaku, error) {
		n := calls.Add(1)
		return []bili.Danmaku{{Text: "batch " + string(rune('0'+n)), ID: "id"}}, nil
	}

	putDanmakuCurrent(a, "BV1", "333")
	a.Danmaku()
	waitDanmaku(t, "第一次落盘了", func() bool {
		e, ok := danmaku.NewCache(dir).Load("BV1", "333")
		return ok && len(e.Items) == 1 && e.Items[0].Text == "batch 1"
	})

	// **同一个 App**：换走再换回来（缓存文件被删了，过期的那一种）。
	//
	// ⚠️ **不能停在同一集上再来一遍**：同一 key 不换集的话那一趟压根不会重新
	// 发起（`danmakuLoaded` 挡住了）——那是既有行为，不是这一层要管的事。
	//
	// ⚠️ **序号表是 per-key 的**（`danmakuSeqs`），而它记的是「这个 key 最后
	// 发起的那一趟」——所以这一条量的是「上一次写完之后（那条序号被删了），
	// 下一次发起的那一趟仍然写得下去」。
	if err := os.Remove(filepath.Join(dir, "BV1-333.json")); err != nil {
		t.Fatal(err)
	}
	putDanmakuCurrent(a, "BV2", "222")
	a.Danmaku()
	waitDanmaku(t, "中间那一集问过了", func() bool { return calls.Load() >= 2 })
	putDanmakuCurrent(a, "BV1", "333")
	a.Danmaku()
	waitDanmaku(t, "第二次落盘了", func() bool {
		e, ok := danmaku.NewCache(dir).Load("BV1", "333")
		return ok && len(e.Items) == 1 && e.Items[0].Text != "batch 1"
	})
}

// TestNotePendingCidOnlyTouchesTheSameSong 盯「记 cid」的那一句。
//
// 🔴 **它必须只碰同一个 bvid 的那一条**：切歌那一瞬间，`a.pending` 可能已经换成
// 新的一首，而这一句是**上一首**的取流回来的那时候写的——bvid 对不上就把上一首的
// cid 写到新那一首上，那新那一集就会拿上一集的弹幕。
//
// ⚠️ **而这一句是「搜索结果那一路有弹幕」的全部**：cid 是取流那一趟问出来的，
// 不记回 `pending` 的话，弹幕这一摊（它只认 `pending` 的 cid）就永远拿不到 cid。
func TestNotePendingCidOnlyTouchesTheSameSong(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	// 这一趟的代次（`New` 之后 `onlineGen` 还是 0）。
	a.mu.Lock()
	gen := a.onlineGen
	a.mu.Unlock()

	// 同一首：记上去。
	a.mu.Lock()
	a.pending = Item{Source: "online", Type: "mv", Bvid: "BV1"}
	a.mu.Unlock()
	a.notePendingCid(gen, "BV1", "777")
	a.mu.Lock()
	got := a.pending.Cid
	a.mu.Unlock()
	if got != "777" {
		t.Errorf("同一首的 cid 没记上：%q", got)
	}

	// 另一首：不许碰（那是上一首的取流回来的）。
	a.mu.Lock()
	a.pending = Item{Source: "online", Type: "mv", Bvid: "BV2"}
	a.mu.Unlock()
	a.notePendingCid(gen, "BV1", "888")
	a.mu.Lock()
	got = a.pending.Cid
	a.mu.Unlock()
	if got != "" {
		t.Errorf("上一首的 cid 写到了新那一首上：%q", got)
	}

	// 另一个 bvid 的那一趟：不许碰（那是**上一趟**的 cid，而那一趟已经过时了）。
	a.mu.Lock()
	a.pending = Item{Source: "online", Type: "mv", Bvid: "BV4"}
	a.mu.Unlock()
	a.notePendingCid(gen+1, "BV4", "999")
	a.mu.Lock()
	got = a.pending.Cid
	a.mu.Unlock()
	if got != "" {
		t.Errorf("过时的那一趟的 cid 写到了现在这一首上：%q", got)
	}

	// 空的一律不记。
	a.mu.Lock()
	a.pending = Item{Source: "online", Type: "mv", Bvid: "BV3"}
	a.mu.Unlock()
	a.notePendingCid(gen, "BV3", "")
	a.notePendingCid(gen, "", "999")
	a.mu.Lock()
	got = a.pending.Cid
	a.mu.Unlock()
	if got != "" {
		t.Errorf("空的 cid 被记上去了：%q", got)
	}
}

// TestTheInFlightRunOfAnotherPartDoesNotWriteItsCid 盯「同一个 bvid 的另一分 P
// 的在途那一趟，不许把自己的 cid 写到现在这一首上」。
//
// 🔴 **这是评审提的第一条 major 的另一半**。
//
// 评审提的那一条是「cid 还没问出来的时候不许拿上一分 P 的弹幕」——这一条是
// **「cid 问出来了，但问的是上一分 P」**。
//
// 症状一模一样：「换了集，屏幕上滚的还是上一集的弹幕」。
//
// ⚠️ **为什么 bvid 对得上还不够**：多 P 的投稿**bvid 是同一个**，分 P id 才是
// 区别。所以「按了第二集」之后 `pending` 的 bvid 还是那个 bvid——第一集那一趟
// 回来时 bvid 是对得上的，于是它把**第一集的 cid 写到第二集上**。
//
// ⇒ 判据得是**代次**（那一趟还属不属于「现在这一趟」），而不是 bvid。
func TestTheInFlightRunOfAnotherPartDoesNotWriteItsCid(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	a.mu.Lock()
	// 「用户按的是第一集」——那一趟的代次是 1。
	a.onlineGen++
	gen := a.onlineGen
	a.mu.Unlock()

	// 然后用户按了**同一个投稿的第二集**：代次加一，`pending` 换成第二集
	// （bvid 一样，cid 还没问出来）。
	a.mu.Lock()
	a.onlineGen++
	a.pending = Item{Source: "online", Type: "mv", Bvid: "BV1"}
	a.mu.Unlock()

	// **第一集**那一趟回来得晚——它带着自己的 cid（777），而它已经过时了。
	a.notePendingCid(gen, "BV1", "777")

	a.mu.Lock()
	got := a.pending.Cid
	a.mu.Unlock()
	if got != "" {
		t.Errorf("上一分 P 的 cid 被写到了现在这一集上：%q", got)
	}
}
