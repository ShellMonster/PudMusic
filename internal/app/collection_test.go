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
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/notice"
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
	// cleaned 记下每一次 resource/clean 的 media_id（「清除失效内容」那一项）。
	cleaned []string
	// deleted 记下每一次 folder/del 的 media_ids（「删除收藏夹」那一项）。
	deleted []string
	// paths 记下每一个请求打到了哪条路径。「一个请求都不该发」那几条用例
	// 要的就是它——数某个写接口记了多少次不够，一个都不该有。
	paths []string
	// ownerMid 是容器创建者的 mid。默认 1（不是「我」，除非用例登录成 1）。
	ownerMid int64
	// attr 是头图区那个位。默认 2 = 公开、非默认收藏夹。
	attr int
	// overrides 是按路径整段替换掉下面那张 switch 的默认处理：下载那一段要
	// 问「这个稿件有哪几集」（`/x/player/pagelist`），而那不是这一摊的接口。
	overrides map[string]http.HandlerFunc
}

func newCollectionFixture(t *testing.T) *collectionFixture {
	t.Helper()
	f := &collectionFixture{overrides: map[string]http.HandlerFunc{}, ownerMid: 1, attr: 2}
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
		f.paths = append(f.paths, r.URL.Path)
		switch r.URL.Path {
		case "/x/v3/fav/folder/info":
			f.infos++
			state := 0
			if f.faved {
				state = 1
			}
			writeCollectionJSON(t, w, 0, "0", map[string]any{
				"id": 42, "title": "通勤路上", "attr": f.attr, "media_count": len(f.medias),
				"fav_state": state,
				"upper":     map[string]any{"mid": f.ownerMid, "name": "UP"},
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
		case "/x/v3/fav/resource/clean":
			_ = r.ParseForm()
			f.cleaned = append(f.cleaned, r.PostForm.Get("media_id"))
			writeCollectionJSON(t, w, 0, "0", 0)
		case "/x/v3/fav/folder/del":
			_ = r.ParseForm()
			f.deleted = append(f.deleted, r.PostForm.Get("media_ids"))
			writeCollectionJSON(t, w, 0, "0", 0)
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
	if _, _, err := a.LoadCollectionPage(t.Context(), testRef, bili.SortByFavTime, "", 1); err == nil {
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
		medias, hasMore, err := a.LoadCollectionPage(t.Context(), testRef, sort, "", 1)
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

	medias, _, err := a.LoadCollectionPage(t.Context(), testRef, bili.SortByFavTime, "", 1)
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

// ── ⋯ 菜单里的那三件 ───────────────────────────────────────────────────────

// folderAppWithSettings 造一个挂了真设置文件、指向那台假 B 站的 App。
//
// ⚠️ **一定要挂设置**：「删收藏夹」和「从侧栏移除」都要把那一行从侧栏里拿掉，
// 而那一半会写 `SideMenuCollectionOrder`（见 `forgetFavorite`）。没挂设置的
// App 上 `UpdateSettings` 回 `ErrSettingsNotMounted`，侧栏那一行会留在原地。
//
// ⚠️ **登录是装上的，不是读盘读来的**：`MountLogin` 挂一个内存 Store
// （`NewMemoryStore`，不碰钥匙串——那是本项目的硬规矩，见内部记忆），
// 随后 `TestOnlyAdoptLogin` 直接装一份进去。`CanDeleteCollection` 问的是
// `SelfMid()`，而它从登录态里读，不装的话永远是 0。
func folderAppWithSettings(t *testing.T, f *collectionFixture) *App {
	t.Helper()
	a := New()
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})
	if err := a.MountSettings(filepath.Join(t.TempDir(), SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	withFixture(t, a, f, 0)
	a.MountLogin(NewMemoryStore("test"), nil)
	return a
}

// TestOnlyYourOwnFolderOffersDeletion 「删 B 站上那个」只在三个条件同时成立时
// 才摆得出来。
//
// 三个条件各有各的理由，合成一条表是因为**每一支少一个都会让用户在界面上
// 按出一个注定失败的按钮**：
//
//   - 不是自己建的：B 站没有删别人收藏夹的接口，报回来的错是「权限不足」，
//     用户会以为自己的账号坏了。
//   - 是默认收藏夹：它同样删不掉（B 站发下来的那一个）。
//   - **登录了**：OwnerMid 为 0 是「这一次没给」，而没登录时 `SelfMid()` 也是 0
//     ——`0 == 0` 会让未登录的路径判出「可以删」。
func TestOnlyYourOwnFolderOffersDeletion(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ownerMid int64
		attr     int
		loggedIn bool
		want     bool
	}{
		{"自己建的非默认", 7, 2, true, true},
		{"别人建的", 900, 2, true, false},
		{"默认收藏夹", 7, 0, true, false},
		{"没登录", 0, 2, false, false},
		{"创建者没给（0）而登录了", 0, 2, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCollectionFixture(t)
			f.ownerMid, f.attr = tc.ownerMid, tc.attr
			a := folderAppWithSettings(t, f)
			if tc.loggedIn {
				a.TestOnlyAdoptLogin("7")
			}
			a.LoadCollection(testRef)
			waitUntil(t, "元信息拉回来", 2*time.Second, func() bool {
				return a.Collection(testRef).Loaded
			})

			if got := a.CanDeleteCollection(testRef); got != tc.want {
				t.Errorf("CanDeleteCollection = %v，want %v（OwnerMid=%d attr=%d 登录=%v）",
					got, tc.want, tc.ownerMid, tc.attr, tc.loggedIn)
			}
		})
	}
}

// TestTheHeaderInfoCarriesWhatDeletionJudgesOn 元信息里那两栏是判断的全部依据。
//
// 少了 OwnerMid 或 Default，`CanDeleteCollection` 会永远给出同一个答案，而那
// 个答案要么是「永远能删」要么是「永远不能删」——两者界面上都看不出来。
func TestTheHeaderInfoCarriesWhatDeletionJudgesOn(t *testing.T) {
	f := newCollectionFixture(t)
	f.ownerMid, f.attr = 900, 3 // 私密、非默认
	a := New()
	t.Cleanup(a.Close)
	withFixture(t, a, f, 0)

	a.LoadCollection(testRef)
	waitUntil(t, "元信息拉回来", 2*time.Second, func() bool {
		return a.Collection(testRef).Loaded
	})

	info := a.Collection(testRef)
	if info.OwnerMid != 900 {
		t.Errorf("OwnerMid = %d，want 900", info.OwnerMid)
	}
	if info.Default {
		t.Error("attr=3（私密、非默认）却读成「默认收藏夹」")
	}
}

// TestClearingInvalidAsksTheServerToDoIt 「清除失效内容」是一个请求。
//
// 🔴 这里量的是**发出去的那一个请求**，不是界面刷新：B 站返回的是「清干净了」
// 而不���「清掉了几个」，所以本地能验的只有「我们没有自己在本地挑一遍再删」
// ——那条路的症状是清完还剩几个，而剩下的那几个在界面上和别的失效条目一模一样。
func TestClearingInvalidAsksTheServerToDoIt(t *testing.T) {
	f := newCollectionFixture(t)
	a := New()
	t.Cleanup(a.Close)
	withFixture(t, a, f, 0)

	a.ClearInvalidMedias(testRef)
	waitUntil(t, "resource/clean 发出去", 2*time.Second, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.cleaned) > 0
	})

	f.mu.Lock()
	got := append([]string(nil), f.cleaned...)
	f.mu.Unlock()
	if len(got) != 1 || got[0] != "42" {
		t.Errorf("clean 的 media_id = %v，want [42]", got)
	}
}

// TestAFinishedClearTellsTheListToRedraw 清完之后列表要重来。
//
// ⚠️ 判据是 **notice 的 Kind**，不是「元信息被刷新了」：刷新元信息改的是头图区
// 那个「N 条视频」，而失效条目照样画在列表上（见 `bili.Media.Attr` 的注释）——
// 真正少东西的是列表，而它只能由界面线程 reload（铁律一）。所以后台递回来的
// 那一句必须**标明「容器变了」**，否则界面只会发一个 Toast，用户看着列表纹丝
// 不动。
func TestAFinishedClearTellsTheListToRedraw(t *testing.T) {
	f := newCollectionFixture(t)
	a := New()
	t.Cleanup(a.Close)
	withFixture(t, a, f, 0)

	var got CollectionNotice
	var has bool
	a.ClearInvalidMedias(testRef)
	// ⚠️ **取走式的接口不能在 waitUntil 里试**：试的那一次就已经把它消费掉了
	//（取过就没有了，见 `TakeCollectionNotice`），等完了再取拿到的是空。
	waitUntil(t, "清完了", 2*time.Second, func() bool {
		got, has = a.TakeCollectionNotice(testRef)
		return has
	})

	if got.Kind != NoticeCollectionChanged {
		t.Errorf("成功那一句的 Kind = %v，want NoticeCollectionChanged（界面才知道要重来列表）", got.Kind)
	}
	if got.Text == "" {
		t.Error("成功之后一句话都没有——用户按下去像是什么也没发生")
	}
}

// TestAFailedClearDoesNotTellTheListToRedraw 失败了不许让列表重来。
//
// 反过来的后果不是白刷一次：用户看到列表整个跳回顶上、勾选全没了，而那一批
// 歌一个都没删。
func TestAFailedClearDoesNotTellTheListToRedraw(t *testing.T) {
	f := newCollectionFixture(t)
	f.overrides["/x/v3/fav/resource/clean"] = func(w http.ResponseWriter, r *http.Request) {
		writeCollectionJSON(t, w, -400, "请求错误", nil)
	}
	a := New()
	t.Cleanup(a.Close)
	withFixture(t, a, f, 0)

	a.ClearInvalidMedias(testRef)
	var got CollectionNotice
	var has bool
	waitUntil(t, "失败那一句递回来了", 2*time.Second, func() bool {
		got, has = a.TakeCollectionNotice(testRef)
		return has
	})

	if got.Kind != NoticePlain {
		t.Errorf("失败那一句的 Kind = %v，want NoticePlain", got.Kind)
	}
	if !strings.Contains(got.Text, "清不掉") {
		t.Errorf("失败那一句 = %q，want 一句说清「清不掉」的", got.Text)
	}
}

// TestDeletingFromThePageGoesThroughTheSameCallAsTheSidebar 页面上删收藏夹和侧栏
// 删的是**同一个接口**。
//
// 这一条钉的是两个入口共用一条路（`deleteCreatedFolder`）。各写一遍的话，
// 「侧栏删得掉、页面上删不掉」是最难查的那一类错：两边测试各自都绿。
func TestDeletingFromThePageGoesThroughTheSameCallAsTheSidebar(t *testing.T) {
	f := newCollectionFixture(t)
	a := folderAppWithSettings(t, f)
	a.TestOnlyAdoptLogin("7")

	said := make(chan string, 1)
	wasOK := make(chan bool, 1)
	a.DeleteCollectionFolder(testRef, "通勤路上", func(msg string, ok bool, _ notice.Tone) {
		said <- msg
		wasOK <- ok
	})

	var gotMsg string
	select {
	case gotMsg = <-said:
	case <-time.After(3 * time.Second):
		t.Fatal("删收藏夹那一趟没回来")
	}

	f.mu.Lock()
	got := append([]string(nil), f.deleted...)
	f.mu.Unlock()
	if len(got) != 1 || got[0] != "42" {
		t.Errorf("folder/del 的 media_ids = %v，want [42]", got)
	}
	// 🔴 **成功必须是 ok=true**：界面靠这一位决定「要不要离开这一页」，而它判断
	// 错了的后果是「B 站拒绝删除，用户却被踢到一个空页面上」（见
	// `TestAFailedDeleteSaysSoAndLeavesTheRowThere`）。
	if !<-wasOK {
		t.Error("删成功了却报成失败")
	}
	if !strings.Contains(gotMsg, "通勤路上") {
		t.Errorf("说完的那句 = %q，没说清删的是哪一个", gotMsg)
	}
	// 成功之后那一行要从侧栏里拿掉——B 站上已经没有了，侧栏还列着就是错的。
	if n := len(a.SideFavorites(FavoritesCreated)); n != 0 {
		t.Errorf("侧栏里还剩 %d 行，want 0（B 站上已经删掉了）", n)
	}
}

// TestForgettingAFolderNeverTouchesBilibili 「从侧栏移除」一个请求都不发。
//
// 🔴 这一条是这个选项**存在的全部理由**：用户按下它的理由是「我不想删 B 站上
// 的那个，只是这儿别列了」。而它一旦发出任何一个写请求——哪怕 B 站那边回 -101
// ——用户就已经拿到了他明确不要的那个结果。
func TestForgettingAFolderNeverTouchesBilibili(t *testing.T) {
	f := newCollectionFixture(t)
	a := folderAppWithSettings(t, f)
	a.SetFavorites(FavoritesCreated, []Favorite{{ID: 42, Title: "通勤路上"}})

	var said string
	a.ForgetCollectionFolder(testRef, "通勤路上", func(msg string, _ notice.Tone) { said = msg })

	// ⚠️ **同步**跑完之后还要给它一点时间：万一它偷偷起了个 goroutine，
	// 这一条就绿了——而那正是要挡的那种实现。
	time.Sleep(50 * time.Millisecond)
	f.mu.Lock()
	paths := append([]string(nil), f.paths...)
	f.mu.Unlock()
	for _, p := range paths {
		if p == "/x/v3/fav/folder/del" || p == "/x/v3/fav/resource/clean" || p == "/x/v3/fav/resource/batch-del" {
			t.Errorf("「从侧栏移除」往 B 站发了 %q——它一个字节都不该发", p)
		}
	}
	if !strings.Contains(said, "B 站") {
		t.Errorf("说完的那句 = %q，要说清 B 站上那个还在", said)
	}
	if n := len(a.SideFavorites(FavoritesCreated)); n != 0 {
		t.Errorf("侧栏里还剩 %d 行，want 0", n)
	}
}

// TestAFailedDeleteSaysSoAndLeavesTheRowThere 删不掉：**说清**、报成失败、
// 侧栏那一行原样留着。
//
// 🔴 这一条钉的是失败路径，因为它的后果是**界面骗人**：歌单页删完之后当前就停在
// 这个容器上，所以界面拿到 `ok=false` 就会留在原地；而一旦这一位判断错了，用户
// 看到的是**一个空页面**——收藏夹在 B 站上好好地还在、侧栏里也还在。
//
// ⚠️ **`folder/del` 成功时那一行要被拿掉、失败时不许动**：拿错了的话下一次拉全量
// 时它（已经不存在的那个）又冒回原位。
func TestAFailedDeleteSaysSoAndLeavesTheRowThere(t *testing.T) {
	f := newCollectionFixture(t)
	f.overrides["/x/v3/fav/folder/del"] = func(w http.ResponseWriter, r *http.Request) {
		writeCollectionJSON(t, w, -400, "请求错误", nil)
	}
	a := folderAppWithSettings(t, f)
	a.SetFavorites(FavoritesCreated, []Favorite{{ID: 42, Title: "通勤路上"}})

	said := make(chan string, 1)
	wasOK := make(chan bool, 1)
	a.DeleteCollectionFolder(testRef, "通勤路上", func(msg string, ok bool, _ notice.Tone) {
		said <- msg
		wasOK <- ok
	})

	var msg string
	select {
	case msg = <-said:
	case <-time.After(3 * time.Second):
		t.Fatal("那一趟没回来")
	}
	if ok := <-wasOK; ok {
		t.Error("B 站拒绝了却报成成功——界面会以为收藏夹没了，把用户踢走")
	}
	if !strings.Contains(msg, "删不掉") {
		t.Errorf("失败那一句 = %q，want 一句说清「删不掉」的", msg)
	}
	if n := len(a.SideFavorites(FavoritesCreated)); n != 1 {
		t.Errorf("侧栏里还剩 %d 行，want 1（它没删掉，那一行就该留着）", n)
	}
}

// TestForgettingClearsTheRowOutOfWhicheverGroupItIsIn 「从侧栏移除」把那一行从
// **它所在的那一组**里拿掉。
//
// 🔴 这一条是被一条真缺陷逼出来的：那一项 ⋯ 菜单对**每一页**都摆得出来，而一个
// **别人建的**收藏夹在侧栏上是「我收藏的」那一组里的。写死「我创建的」的话，提示
// 说「已从侧栏移除」而那一行原封不动留在侧栏上——**那是一句谎话**，而谎话是这一层
// 最不该留的东西。
func TestForgettingClearsTheRowOutOfWhicheverGroupItIsIn(t *testing.T) {
	for _, g := range []FavoriteGroup{FavoritesCreated, FavoritesCollected} {
		t.Run(g.String(), func(t *testing.T) {
			f := newCollectionFixture(t)
			a := folderAppWithSettings(t, f)
			a.SetFavorites(g, []Favorite{{ID: 42, Title: "通勤路上", Type: 11}})

			var said string
			a.ForgetCollectionFolder(testRef, "通勤路上", func(msg string, _ notice.Tone) { said = msg })

			if n := len(a.SideFavorites(g)); n != 0 {
				t.Errorf("「%s」那一组里还剩 %d 行，want 0（说了已移除却还在）", g, n)
			}
			if said == "" {
				t.Error("一句话都没说——用户按下去像是什么也没发生")
			}
		})
	}
}

// TestForgettingTakesTheRowOutOfBothGroups 同一个 id 两组里都有时，**两组都拿掉**。
//
// 同一个 id 同时出现在两组，意味着那是他自己建的、而他也收藏了自己建的收藏夹。
// 用户按下这一项的理由是「我不想在侧栏看到它」——只清一组的话它会在另一组里接着
// 冒出来，而提示已经说了「已从侧栏移除」。
func TestForgettingTakesTheRowOutOfBothGroups(t *testing.T) {
	f := newCollectionFixture(t)
	a := folderAppWithSettings(t, f)
	a.SetFavorites(FavoritesCreated, []Favorite{{ID: 42, Title: "通勤路上", Type: 11}})
	a.SetFavorites(FavoritesCollected, []Favorite{{ID: 42, Title: "通勤路上", Type: 11}})

	a.ForgetCollectionFolder(testRef, "通勤路上", func(string, notice.Tone) {})

	for _, g := range []FavoriteGroup{FavoritesCreated, FavoritesCollected} {
		if n := len(a.SideFavorites(g)); n != 0 {
			t.Errorf("「%s」那一组里还剩 %d 行，want 0", g, n)
		}
	}
}
