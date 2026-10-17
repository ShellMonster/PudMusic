// 歌单这一摊的业务层测试。
//
// **package app 而不是 app_test**：这里要钉 `favoriteSettleDelay` 这个常量本身
// （它没有导出，而「等 500 毫秒」是这一票的验收点之一）。
//
// ⚠️ fixture 是假的 B 站，不是真网：它证明的是「我们等够了才去刷新」这件事，
// 不是「B 站真的过 500 毫秒才改状态」。后者只能靠真机验证。

package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// collectionFixture 是一台假 B 站：收藏夹元信息 + 内容列表 + 那几个写接口。
type collectionFixture struct {
	*httptest.Server

	mu sync.Mutex
	// faved 是这个容器此刻收藏了没有（folder/info 就照它回）。
	faved bool
	// infos 是被问过几次 folder/info ——「刷新了几次」就是这一项。
	infos int
	// medias 是内容列表会回的条目。
	medias []bili.Media
	// orders 记下每一次内容列表请求带的 order。
	orders []string
	// removed 记下每一次 batch-del 的 resources。
	removed []string
	// overrides 是按路径整段替换掉下面那张 switch 的默认处理：下载那一段要
	// 问「这个稿件有哪几集」（`/x/player/pagelist`），而那不是这一摊的接口。
	overrides map[string]http.HandlerFunc
}

func newCollectionFixture(t *testing.T) *collectionFixture {
	t.Helper()
	f := &collectionFixture{overrides: map[string]http.HandlerFunc{}}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ⚠️ overrides 先看，而且**不拿这一把锁**跑它：替换掉的那一段自己要
		// 能读 f（要读 medias 就得进锁，而锁在上面已经拿住了）。
		f.mu.Lock()
		h := f.overrides[r.URL.Path]
		f.mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}

		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/x/v3/fav/folder/info":
			f.infos++
			state := 0
			if f.faved {
				state = 1
			}
			writeCollectionJSON(t, w, 0, "0", map[string]any{
				"id": 42, "title": "通勤路上", "attr": 2, "media_count": len(f.medias),
				"fav_state": state,
				"upper":     map[string]any{"mid": 1, "name": "UP"},
			})
		case "/x/v3/fav/resource/list":
			f.orders = append(f.orders, r.URL.Query().Get("order"))
			writeCollectionJSON(t, w, 0, "0", map[string]any{
				"medias":   fixtureMediaJSON(f.medias),
				"has_more": false,
			})
		case "/x/v3/fav/folder/fav", "/x/v3/fav/folder/unfav":
			_ = r.ParseForm()
			f.faved = r.URL.Path == "/x/v3/fav/folder/fav"
			writeCollectionJSON(t, w, 0, "0", nil)
		case "/x/v3/fav/resource/batch-del":
			_ = r.ParseForm()
			f.removed = append(f.removed, r.PostForm.Get("resources"))
			writeCollectionJSON(t, w, 0, "0", nil)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// fixtureMediaJSON 把 medias 翻成 B 站那个形状。
func fixtureMediaJSON(in []bili.Media) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, m := range in {
		out = append(out, map[string]any{
			"id": m.ID, "type": m.Type, "title": m.Title, "bvid": m.Bvid,
			"cover": m.Cover, "duration": int(m.Duration / time.Second),
			"attr": 0, "upper": map[string]any{"name": m.Owner},
		})
	}
	return out
}

func writeCollectionJSON(t *testing.T, w http.ResponseWriter, code int, msg string, data any) {
	t.Helper()
	body := map[string]any{"code": code, "message": msg, "ttl": 1, "data": data}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

// withFixture 把这个 App 的歌单这一摊指向那台假 B 站。
func withFixture(t *testing.T, a *App, f *collectionFixture, settle time.Duration) {
	t.Helper()
	transport := &CredentialTransport{
		Base:   f.Server.Client().Transport,
		Policy: DefaultInjectPolicy().AllowBiliHost(hostOf(f.URL)),
	}
	cli := bili.NewClient(bili.Options{
		Transport: transport,
		CSRF:      func() string { return "jct-fixture" },
	})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountCollections(CollectionRefs{Client: cli, Settle: settle})
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// waitFor 轮询到 cond 为真，最多等 d。
//
// ⚠️ 不用 `time.Sleep` 一次睡到底：后台那一趟什么时候跑完跟机器负载有关，
// 睡一个固定的时长要么白等，要么在慢机器上不够——后者表现为偶发的红。
// ⚠️ 它原来叫 `waitFor`，和 `localmusic_test.go` 里那个（#78 带来的、
// 参数顺序还是反的：`waitFor(t, cond, msg)`）同包同名。合并时 Git 看不出来
// （不同的文件），编译才报。
func waitUntil(t *testing.T, what string, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等了 %v，%s 还没发生", d, what)
}

var testRef = CollectionRef{ID: 42, Type: bili.CollectionFavorite}

// ── 挂没挂上 ───────────────────────────────────────────────────────────────

func TestCollectionsUnmountedIsNormal(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)
	if a.CollectionsMounted() {
		t.Error("还没挂就说挂上了")
	}
	if a.DownloaderMounted() {
		t.Error("下载服务还没有实现，DownloaderMounted 却说挂上了")
	}
	// 没挂也要能读：零值不是「忘了初始化」，是「还没接」。
	if got := a.Collection(testRef); got.Loaded {
		t.Errorf("没挂的时候 Collection 返回 Loaded=true：%+v", got)
	}
	if _, ok := a.TakeCollectionNotice(testRef); ok {
		t.Error("没挂的时候竟然取得到一句要显示的话")
	}
	if _, _, err := a.LoadCollectionPage(t.Context(), testRef, bili.SortByFavTime, 1); err == nil {
		t.Error("没挂的时候拉内容页报成功")
	}
}

// ── 元信息缓存 ─────────────────────────────────────────────────────────────

// TestCollectionCachesTheHeader 头图区每一帧都要读它，不能每一帧发一次请求。
func TestCollectionCachesTheHeader(t *testing.T) {
	f := newCollectionFixture(t)
	f.medias = []bili.Media{{ID: 1, Type: 2, Title: "歌 1"}}
	a := New()
	t.Cleanup(a.Close)
	withFixture(t, a, f, 0)

	a.LoadCollection(testRef)
	waitUntil(t, "元信息到缓存里", time.Second, func() bool { return a.Collection(testRef).Loaded })

	info := a.Collection(testRef)
	if info.Title != "通勤路上" || info.MediaCount != 1 || info.OwnerName != "UP" {
		t.Errorf("元信息不对：%+v", info)
	}
	if info.Ref != testRef {
		t.Errorf("Ref = %v，want %v", info.Ref, testRef)
	}
	// 再读十次也不能多发请求。
	for range 10 {
		_ = a.Collection(testRef)
	}
	f.mu.Lock()
	n := f.infos
	f.mu.Unlock()
	if n != 1 {
		t.Errorf("folder/info 被问了 %d 次，want 1", n)
	}
}

// TestAFailedHeaderLoadSaysSo 拉不到要说话，而不是让头图区一直空着。
func TestAFailedHeaderLoadSaysSo(t *testing.T) {
	f := newCollectionFixture(t)
	f.Close() // 服务器没了：请求一定失败
	a := New()
	t.Cleanup(a.Close)
	withFixture(t, a, f, 0)

	a.LoadCollection(testRef)
	n, ok := waitNotice(t, a, testRef)
	if !ok {
		t.Fatal("拉失败了却一句话都没说")
	}
	if !strings.Contains(n.Text, "打不开") {
		t.Errorf("说的是 %q，want 一句「打不开这个收藏夹」", n.Text)
	}
	if a.Collection(testRef).Loaded {
		t.Error("拉失败了缓存却标成 Loaded，头图区会画成「正在加载…」永远停在那儿")
	}
}

// TestNoticeIsTakenOnlyOnce 取走过就不会再说第二遍——不取的话每一帧都冒一次 Toast。
func TestNoticeIsTakenOnlyOnce(t *testing.T) {
	f := newCollectionFixture(t)
	f.Close()
	a := New()
	t.Cleanup(a.Close)
	withFixture(t, a, f, 0)

	a.LoadCollection(testRef)
	first, ok := waitNotice(t, a, testRef)
	if !ok {
		t.Fatal("没有要显示的话")
	}
	if _, again := a.TakeCollectionNotice(testRef); again {
		t.Error("同一句被取走了两次")
	}
	// 同一个错误再发生一次：ID 必须变，否则界面「取到新的一条才发 Toast」
	// 的判重会让第二遍静默——用户会以为第二次点击没生效。
	a.LoadCollection(testRef)
	second, ok := waitNotice(t, a, testRef)
	if !ok {
		t.Fatal("第二次也没有要显示的话")
	}
	if second.ID == first.ID {
		t.Errorf("两次的 ID 相同（%d），界面会把第二遍当成重复而静默", second.ID)
	}
	if second.Text != first.Text {
		t.Errorf("两次的话不一样：%q / %q", first.Text, second.Text)
	}
}

func waitNotice(t *testing.T, a *App, ref CollectionRef) (CollectionNotice, bool) {
	t.Helper()
	var got CollectionNotice
	var ok bool
	waitUntil(t, "取到要显示的那句话", time.Second, func() bool {
		got, ok = a.TakeCollectionNotice(ref)
		return ok
	})
	return got, ok
}

// ── 🔴 收藏之后那 500 毫秒 ─────────────────────────────────────────────────

// TestTheSettleDelayIsHalfASecond 钉住那个数本身。
//
// 这是验收点名的那一条，而它是一个**死数**：Biu 上游等 500ms
// （`pages/video-collection/favorites/index.tsx` 的 toggleFavorite）。
// 改成 0 的后果不是「更快」，是「星标会说一次谎」。
func TestTheSettleDelayIsHalfASecond(t *testing.T) {
	if favoriteSettleDelay != 500*time.Millisecond {
		t.Errorf("收藏之后的等待是 %v，want 500ms（Biu 上游那个数）", favoriteSettleDelay)
	}
}

// TestFavoritingDoesNotRefreshBeforeItSettles 服务器**立刻**就改状态了，而
// 缓存直到 settle 之后才更新。
//
// 这正是那个坑的形状：B 站的 `folder/info` 有一小段时间仍然报着旧状态，
// 不等就直接刷新，用户看到的是「点了收藏，界面上立刻又变回没收藏」。
// 这条测试钉的是**顺序**（先等，再刷新），不是等多久。
func TestFavoritingDoesNotRefreshBeforeItSettles(t *testing.T) {
	f := newCollectionFixture(t)
	a := New()
	t.Cleanup(a.Close)
	// settle 给得很大：后台那一趟此刻**一定**还没走完。
	withFixture(t, a, f, 30*time.Second)

	a.SetFavorite(testRef, true)
	// 写接口已经打过去了（B 站那边已经改好了），但缓存还是旧的。
	waitUntil(t, "收藏请求发出去", time.Second, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.faved
	})
	if a.Collection(testRef).Loaded && a.Collection(testRef).Faved {
		t.Fatal("还没到 settle 就把收藏状态刷新进去了：星标会说一次谎")
	}

	// settle 给 0 的同一件事：立刻就刷进来了。
	f2 := newCollectionFixture(t)
	a2 := New()
	t.Cleanup(a2.Close)
	withFixture(t, a2, f2, 0)
	a2.SetFavorite(testRef, true)
	waitUntil(t, "收藏状态刷新进缓存", time.Second, func() bool {
		return a2.Collection(testRef).Faved
	})
}

// TestCancellingFavoritingGoesTheOtherWay 取消收藏那一侧同样要走那一段等待。
func TestCancellingFavoritingGoesTheOtherWay(t *testing.T) {
	f := newCollectionFixture(t)
	f.faved = true
	a := New()
	t.Cleanup(a.Close)
	withFixture(t, a, f, 0)

	a.SetFavorite(testRef, false)
	waitUntil(t, "收藏状态变成没收藏", time.Second, func() bool {
		return !a.Collection(testRef).Faved
	})
}

// TestTheFavoriteNoticeSaysWhichWayItWent 说清楚是收藏上了还是取消掉了。
func TestTheFavoriteNoticeSaysWhichWayItWent(t *testing.T) {
	f := newCollectionFixture(t)
	a := New()
	t.Cleanup(a.Close)
	withFixture(t, a, f, 0)

	a.SetFavorite(testRef, true)
	if n, _ := waitNotice(t, a, testRef); !strings.Contains(n.Text, "已收藏") {
		t.Errorf("说的是 %q", n.Text)
	}
	f.mu.Lock()
	f.faved = false
	f.mu.Unlock()
	a.SetFavorite(testRef, false)
	if n, _ := waitNotice(t, a, testRef); !strings.Contains(n.Text, "取消") {
		t.Errorf("说的是 %q", n.Text)
	}
}

// ── 内容列表 ───────────────────────────────────────────────────────────────

// TestLoadCollectionPagePassesTheSortThrough 排序是接口排的，别在本地重排。
func TestLoadCollectionPagePassesTheSortThrough(t *testing.T) {
	f := newCollectionFixture(t)
	f.medias = []bili.Media{
		{ID: 1, Type: bili.MediaVideo, Title: "歌 1", Bvid: "BV1", Duration: 3 * time.Minute},
		{ID: 2, Type: bili.MediaAudio, Title: "歌 2"},
	}
	a := New()
	t.Cleanup(a.Close)
	withFixture(t, a, f, 0)

	for _, sort := range bili.CollectionSorts {
		medias, hasMore, err := a.LoadCollectionPage(t.Context(), testRef, sort, 1)
		if err != nil {
			t.Fatal(err)
		}
		if hasMore || len(medias) != 2 {
			t.Fatalf("%v: %d 条 hasMore=%v", sort.Label(), len(medias), hasMore)
		}
	}
	f.mu.Lock()
	got := append([]string(nil), f.orders...)
	f.mu.Unlock()
	want := []string{"mtime", "view", "pubtime"}
	if len(got) != len(want) {
		t.Fatalf("发了 %d 次请求，want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 次的 order = %q，want %q", i+1, got[i], want[i])
		}
	}
}

// TestDurationComesBackInSeconds 一首三分钟的歌要显示成 3:00。
func TestDurationComesBackInSeconds(t *testing.T) {
	f := newCollectionFixture(t)
	f.medias = []bili.Media{{ID: 1, Type: 2, Title: "歌", Duration: 180 * time.Second}}
	a := New()
	t.Cleanup(a.Close)
	withFixture(t, a, f, 0)

	medias, _, err := a.LoadCollectionPage(t.Context(), testRef, bili.SortByFavTime, 1)
	if err != nil {
		t.Fatal(err)
	}
	if medias[0].Duration != 180*time.Second {
		t.Errorf("时长 = %v，want 3m0s", medias[0].Duration)
	}
}

// ── 批量删除 ───────────────────────────────────────────────────────────────

// TestRemoveMediasSendsThemAll 一条都不能少。
func TestRemoveMediasSendsThemAll(t *testing.T) {
	f := newCollectionFixture(t)
	a := New()
	t.Cleanup(a.Close)
	withFixture(t, a, f, 0)

	medias := []bili.Media{{ID: 11, Type: 2}, {ID: 22, Type: 12}}
	if err := a.RemoveMedias(t.Context(), testRef, medias); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	got := append([]string(nil), f.removed...)
	f.mu.Unlock()
	if len(got) != 1 || got[0] != "11:2,22:12" {
		t.Errorf("batch-del 发出去 %v，want [11:2,22:12]", got)
	}
}

// TestRemoveNothingIsANoOp 「没勾任何东西就点删除」根本不该发请求。
func TestRemoveNothingIsANoOp(t *testing.T) {
	f := newCollectionFixture(t)
	a := New()
	t.Cleanup(a.Close)
	withFixture(t, a, f, 0)
	if err := a.RemoveMedias(t.Context(), testRef, nil); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	n := len(f.removed)
	f.mu.Unlock()
	if n != 0 {
		t.Errorf("发了 %d 次删除请求，want 0", n)
	}
}

// ── 批量下载（#29 的接口） ──────────────────────────────────────────────────

// TestDownloadSaysItIsNotWiredYet **不假装成功**：假装的话界面上会弹一句
// 「下载任务已添加」，而用户等一小时什么也不会发生。
func TestDownloadSaysItIsNotWiredYet(t *testing.T) {
	f := newCollectionFixture(t)
	a := New()
	t.Cleanup(a.Close)
	withFixture(t, a, f, 0)

	err := a.DownloadMedias(t.Context(), testRef, []bili.Media{{ID: 1, Type: 2}})
	if !DownloaderMissing(err) {
		t.Fatalf("err = %v，want「下载服务还没接上」", err)
	}
	if err == nil {
		t.Fatal("下载服务还没有实现，却报成功")
	}
}

// fakeDownloader 是给「接上了」那条路用的桩。
type fakeDownloader struct {
	got    []bili.Media
	folder CollectionRef
	err    error
}

func (d *fakeDownloader) EnqueueDownload(_ context.Context, folder CollectionRef, medias []bili.Media) error {
	d.got, d.folder = medias, folder
	return d.err
}

// TestDownloadHandsTheBatchToTheService 接线定了：传过去的是**勾上的那几首**，
// 而且带得上它们来自哪个容器（下载任务要在界面上写「来自哪个收藏夹」）。
func TestDownloadHandsTheBatchToTheService(t *testing.T) {
	f := newCollectionFixture(t)
	a := New()
	t.Cleanup(a.Close)
	withFixture(t, a, f, 0)

	d := &fakeDownloader{}
	a.MountCollections(CollectionRefs{
		Client:     clientOn(t, f),
		Settle:     0,
		Downloader: d,
	})
	if !a.DownloaderMounted() {
		t.Fatal("装了 Downloader 却说没挂上")
	}
	want := []bili.Media{{ID: 1, Type: 2}, {ID: 2, Type: 12}}
	if err := a.DownloadMedias(t.Context(), testRef, want); err != nil {
		t.Fatal(err)
	}
	if len(d.got) != 2 || d.got[0].ID != 1 || d.got[1].ID != 2 {
		t.Errorf("交过去的是 %v", d.got)
	}
	if d.folder != testRef {
		t.Errorf("folder = %v，want %v", d.folder, testRef)
	}
}

// TestDownloadPassesTheServiceErrorUp 服务那边报错要原样传上去。
func TestDownloadPassesTheServiceErrorUp(t *testing.T) {
	f := newCollectionFixture(t)
	a := New()
	t.Cleanup(a.Close)
	d := &fakeDownloader{err: errDownloaderMissing}
	a.MountCollections(CollectionRefs{Client: clientOn(t, f), Downloader: d})
	if err := a.DownloadMedias(t.Context(), testRef, []bili.Media{{ID: 1, Type: 2}}); err == nil {
		t.Fatal("服务报错了却当成功")
	}
}

// clientOn 建一个指向那台 fixture 的客户端。
func clientOn(t *testing.T, f *collectionFixture) *bili.Client {
	t.Helper()
	transport := &CredentialTransport{
		Base:   f.Server.Client().Transport,
		Policy: DefaultInjectPolicy().AllowBiliHost(hostOf(f.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport, CSRF: func() string { return "jct" }})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	return cli
}
