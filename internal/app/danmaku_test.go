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
