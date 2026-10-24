// 这一组盯的是**收藏夹自动拉**（issue #362）。
//
// 「自动」的重点不在「拉」，而在**拉的那两趟**：登录之后一趟、开机时已经
// 登录一趟。而**不多拉**——这是网络往返，不是每帧都能做的事（每帧拉的话，
// 一次打开应用就是几十次请求，B 站那边直接限速）。
//
// 另外一组盯的是**拉失败和「真的没有」在界面上必须分开**：那是这一票的
// 另一半——摆一句「还没有收藏夹」在拉失败的时候是撒谎。

package app_test

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// favListPath 是拉「我创建的收藏夹」那个接口的路径。
const favListPath = "/x/v3/fav/folder/created/list"

// favCollectedPath 是拉「我收藏的收藏夹」那个接口的路径。
const favCollectedPath = "/x/v3/fav/folder/collected/list"

// favHits 记下收藏夹列表接口被问了几回。
type favHits struct {
	mu sync.Mutex
	n  int
}

func (h *favHits) add() {
	h.mu.Lock()
	h.n++
	h.mu.Unlock()
}

func (h *favHits) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.n
}

// onFavList 装好「我创建的」那个接口的回答，并把它被问了几回记下来。
//
// list 是它回的那几条；fail 为 true 时回一个业务错误（-500），用来造
// 「拉失败」那一种。
//
// ⚠️ **「我收藏的」那个接口也一并装上**：刷新是两组一起刷的（见
// `app.RefreshFavorites`），而那个接口没装的话它会 404——于是每一趟刷新
// 都以「拉失败」收场，而测试要验的那些东西根本轮不到发生。
func onFavList(t *testing.T, f *biliFixture, hits *favHits, list []map[string]any, fail bool) {
	t.Helper()
	f.on(favListPath, func(w http.ResponseWriter, r *http.Request) {
		hits.add()
		if fail {
			writeFixtureJSON(t, w, -500, "服务器开小差了", nil)
			return
		}
		writeFixtureJSON(t, w, 0, "0", map[string]any{
			"count": len(list), "has_more": false, "list": list,
		})
	})
	onFavCollected(t, f, nil, nil, false)
}

// onFavCollected 装好「我收藏的」那个接口的回答。
func onFavCollected(t *testing.T, f *biliFixture, hits *favHits, list []map[string]any, fail bool) {
	t.Helper()
	f.on(favCollectedPath, func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.add()
		}
		if fail {
			writeFixtureJSON(t, w, -500, "服务器开小差了", nil)
			return
		}
		writeFixtureJSON(t, w, 0, "0", map[string]any{
			"count": len(list), "list": list,
		})
	})
}

// favApp 装一个「登录那一摊 + 收藏夹那一摊都指着这台假 B 站」的 App。
//
// ⚠️ **两个 Mount 用的必须是同一个客户端**：收藏夹那一摊拿到的就是登录那
// 一摊那个（见 `app.MountCollections` 的注释），自己再造一个的话登录态、
// 请求头、WBI 密钥就不是同一份了。
func favApp(t *testing.T, f *biliFixture, store app.Store) *app.App {
	t.Helper()
	a, cli := newTestAppAndClient(t, f, store, nil)
	a.MountCollections(app.CollectionRefs{Client: cli})
	return a
}

// favStore 造一个「已经登录了」的凭据仓库。
//
// ⚠️ **Cookie 那一栏不能省**：收藏夹列表接口问的是 `up_mid`，而那一层从
// **cookie** 里读它（见 `bili.Client.SelfMid` 的注释）。只存 SESSDATA 和
// DedeUserID 两个字段的话，客户端那边读出来的 mid 是 0，于是那一趟会说
// 「还没登录」——而真相是登录态好好的。
func favStore(t *testing.T, mid string) app.Store {
	t.Helper()
	store := app.NewMemoryStore("test")
	err := store.Save(context.Background(), app.Session{
		SESSDATA: "sess-from-fixture", DedeUserID: mid,
		Cookies: []app.Cookie{
			{Name: "SESSDATA", Value: "sess-from-fixture", Path: "/"},
			{Name: "DedeUserID", Value: mid, Path: "/"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// TestLoggingInPullsTheFavorites 钉住「登录之后自动拉一次」。
//
// 这一条是这一票的第一条验收：登录之后侧栏上那组收藏夹就该自己出现，
// 不该逼用户先去头像下拉里按「刷新数据」。
//
// ⚠️ **判据是「真的发出去了一次请求」**：写成「侧栏上有收藏夹了」的话，
// 哪天那一趟被换成读缓存，照样全绿——而用户看到的还是空的。
func TestLoggingInPullsTheFavorites(t *testing.T) {
	f := newBiliFixture(t)
	f.onScans(86101, 0)
	hits := &favHits{}
	onFavList(t, f, hits, []map[string]any{
		{"id": 11, "title": "通勤路上", "attr": 0, "media_count": 2},
	}, false)

	a := favApp(t, f, app.NewMemoryStore("test"))
	a.BeginLogin()
	defer a.CancelLogin()

	waitFor(t, waitTimeout, "登录成功", func() bool { return a.LoginState().Phase == app.LoginDone })
	waitFor(t, waitTimeout, "登录之后自动拉了一次收藏夹", func() bool { return hits.count() == 1 })

	// 而且拉到的东西真的进了侧栏那一份。
	waitFor(t, waitTimeout, "侧栏上有了收藏夹", func() bool {
		return len(a.SideFavorites(app.FavoritesCreated)) == 1
	})
	if got := a.SideFavorites(app.FavoritesCreated)[0].Title; got != "通勤路上" {
		t.Errorf("拉回来的那一条叫 %q，want 通勤路上", got)
	}
}

// TestAlreadyLoggedInPullsTheFavoritesAtStartup 钉住「开机时已经登录也拉一次」。
//
// 这一条是这一票的第二条验收：已经登录的用户打开应用就该看见自己的收藏夹，
// 不该先看到一片空的。
//
// ⚠️ **它模拟的是开机那一趟**（`ui.Run` 的 `WhenReady` 里那一行
// `AutoRefreshFavorites`，见 `internal/ui/shell.go`）。
func TestAlreadyLoggedInPullsTheFavoritesAtStartup(t *testing.T) {
	f := newBiliFixture(t)
	hits := &favHits{}
	onFavList(t, f, hits, []map[string]any{
		{"id": 11, "title": "通勤路上", "attr": 0, "media_count": 2},
	}, false)

	a := favApp(t, f, favStore(t, "7"))
	if !a.LoggedIn() {
		t.Fatal("凭据读回来了，但 App 说没登录")
	}

	a.AutoRefreshFavorites()

	waitFor(t, waitTimeout, "开机自动拉了一次收藏夹", func() bool { return hits.count() == 1 })
	waitFor(t, waitTimeout, "侧栏上有了收藏夹", func() bool {
		return len(a.SideFavorites(app.FavoritesCreated)) == 1
	})
}

// TestAutoRefreshSendsNothingWhenLoggedOut 钉「没登录时一个请求都不发」。
//
// 未登录去试登录后的接口，B 站一律回 -101——那一片 401 噪声会把别的排查
// 全带偏（issue #91「权限」那一节写的就是这一条）。
//
// ⚠️ **判据是「一个请求都没发」**：只断「没报错」的话，一次 -101 也算
// 「没报错」。
func TestAutoRefreshSendsNothingWhenLoggedOut(t *testing.T) {
	f := newBiliFixture(t)
	hits := &favHits{}
	onFavList(t, f, hits, nil, false)

	a := favApp(t, f, app.NewMemoryStore("test"))
	a.AutoRefreshFavorites()

	// ⚠️ **这一句是同步判**：没登录时 `AutoRefreshFavorites` 直接返回，
	// 一个 goroutine 都没起——所以「现在一个请求都没有」是确定的，
	// 不需要等。
	if got := hits.count(); got != 0 {
		t.Errorf("没登录也发了 %d 次收藏夹列表请求", got)
	}
}

// TestAutoRefreshSendsOnlyOneRequest 钉「一次自动拉只发一次请求」。
//
// 这一条是「不许每帧都拉」那条规矩的**反面判据**：自动拉是一个**事件**
// （登录 / 开机），不是一个每一帧都问一次的状态。多发一次的症状是用户
// 什么都没干，B 站那边先把他限速了。
func TestAutoRefreshSendsOnlyOneRequest(t *testing.T) {
	f := newBiliFixture(t)
	hits := &favHits{}
	onFavList(t, f, hits, []map[string]any{
		{"id": 11, "title": "通勤路上", "attr": 0, "media_count": 2},
	}, false)

	a := favApp(t, f, favStore(t, "7"))
	a.AutoRefreshFavorites()

	waitFor(t, waitTimeout, "自动拉跑完了", func() bool { return hits.count() >= 1 })
	// 跑完之后再等一小会儿，确认它**没有**自己在跑第二趟。
	waitFor(t, waitTimeout, "自动拉停下来了", func() bool {
		return !a.FavoritesRefreshing()
	})
	if got := hits.count(); got != 1 {
		t.Errorf("自动拉发了 %d 次请求，want 1", got)
	}
}

// TestAFailedFetchIsNotAnEmptyList 钉「拉失败」和「真的没有」在状态上是两回事。
//
// 这一条是这一票的第三句验收：**拉失败 ≠ 真的没有收藏夹**。
//
// ⚠️ **判据在状态上，不在界面上**：界面上那一句由界面层翻（见
// `ui.favEmptyText`），而这一层要保证的是「失败被记下来了」——记不下来
// 的话，界面只能照着空列表说「还没有收藏夹」，那是在撒谎。
func TestAFailedFetchIsNotAnEmptyList(t *testing.T) {
	f := newBiliFixture(t)
	hits := &favHits{}
	onFavList(t, f, hits, nil, true)

	a := favApp(t, f, favStore(t, "7"))
	a.AutoRefreshFavorites()

	waitFor(t, waitTimeout, "拉失败被记下来了", func() bool {
		return a.FavoriteState(app.FavoritesCreated).Err != ""
	})
	st := a.FavoriteState(app.FavoritesCreated)
	if st.Loaded {
		t.Error("拉失败了还说「拉到了」——界面上会摆一句「还没有收藏夹」，那是撒谎")
	}
	if len(a.SideFavorites(app.FavoritesCreated)) != 0 {
		t.Errorf("拉失败之后侧栏上有 %d 条", len(a.SideFavorites(app.FavoritesCreated)))
	}
}

// TestLoggingOutClearsTheFavorites 钉**退登之后侧栏那两组被清掉**。
//
// ⚠️ **它们是上一个账号的**：退登之后还摆着的话，换人登录那几秒里新用户看到
// 的是别人的收藏夹名。
//
// ⚠️ **状态也要复位**：不清的话下一次登录之后那一句是「还没有收藏夹」，
// 而真相是「这个账号的还没拉过」。
func TestLoggingOutClearsTheFavorites(t *testing.T) {
	f := newBiliFixture(t)
	hits := &favHits{}
	onFavList(t, f, hits, []map[string]any{
		{"id": 11, "title": "通勤路上", "attr": 0, "media_count": 2},
	}, false)

	a := favApp(t, f, favStore(t, "7"))
	a.AutoRefreshFavorites()
	waitFor(t, waitTimeout, "收藏夹拉回来了", func() bool {
		return len(a.SideFavorites(app.FavoritesCreated)) == 1
	})

	if err := a.Logout(); err != nil {
		t.Fatal(err)
	}
	if got := len(a.SideFavorites(app.FavoritesCreated)); got != 0 {
		t.Errorf("退登之后「我创建的」还有 %d 条", got)
	}
	if got := len(a.SideFavorites(app.FavoritesCollected)); got != 0 {
		t.Errorf("退登之后「我收藏的」还有 %d 条", got)
	}
	st := a.FavoriteState(app.FavoritesCreated)
	if st.Loaded || st.Err != "" {
		t.Errorf("退登之后状态是 %+v，应该复位成「没拉过」", st)
	}
}

// TestTheCollectedFoldersShowUpInTheSidebar 钉住「我收藏的」那一组有源了。
//
// 这一条是这一票的后一半：B 站「我收藏了别人的哪些收藏夹」那个接口**是有的**
// （`collected/list`），而它以前在本项目里只有测试填过——于是侧栏上那一组
// 永远空着，而用户看到的是一个摆在那儿、永远不动的分组。
//
// ⚠️ **断言的是那一组真的有条目**：写成「刷新成功」的话，接没接上那个接口
// 都一样——接口 404 时刷新照样「成功」（那一组拉失败不影响另一组）。
func TestTheCollectedFoldersShowUpInTheSidebar(t *testing.T) {
	f := newBiliFixture(t)
	hits := &favHits{}
	onFavList(t, f, hits, []map[string]any{
		{"id": 11, "title": "通勤路上", "attr": 0, "media_count": 2},
	}, false)
	// 「我收藏的」这一组：一条收藏夹、一条合集。
	onFavCollected(t, f, hits, []map[string]any{
		{"id": 900, "title": "入站必刷", "mid": 77, "type": 11, "state": 0},
		{"id": 901, "title": "合集 甲", "mid": 78, "type": 21, "state": 0},
	}, false)

	a := favApp(t, f, favStore(t, "7"))
	a.AutoRefreshFavorites()

	waitFor(t, waitTimeout, "「我收藏的」那一组拉回来了", func() bool {
		return len(a.SideFavorites(app.FavoritesCollected)) >= 1
	})
	got := a.SideFavorites(app.FavoritesCollected)
	if got[0].Title != "入站必刷" || got[0].Mid != 77 {
		t.Errorf("第一条是 %+v，want 入站必刷（mid 77）", got[0])
	}
	// ⚠️ **主人的 mid 要带过来**：「我收藏的」那一组里的收藏夹全是别人的，
	// 而跳 `/collection/{id}?mid=` 要的就是主人的那个。
	if got[0].Mid != 77 {
		t.Errorf("mid 是 %d，want 77（收藏夹主人的）", got[0].Mid)
	}
}

// TestCollectedAlbumsAreNotPutInTheSidebar 钉**合集 / 系列不进侧栏**。
//
// ⚠️ **摆一行点不动的行比不摆更坏**：合集（21）/ 系列（31）的内容列表是
// **另一套接口**（`/x/polymer/web-space/seasons/season_info`、
// `/x/series/archives`），本项目没接——于是点进去那一页是空的（合集的 id
// 被发到了收藏夹那两个接口上），右键「播放」「取消收藏」走的也是同一条错接口。
//
// 所以这一组**只收收藏夹**（见 `collectedFavorites`）。
func TestCollectedAlbumsAreNotPutInTheSidebar(t *testing.T) {
	f := newBiliFixture(t)
	hits := &favHits{}
	onFavList(t, f, hits, nil, false)
	onFavCollected(t, f, hits, []map[string]any{
		{"id": 900, "title": "入站必刷", "mid": 77, "type": 11, "state": 0},
		{"id": 901, "title": "合集 甲", "mid": 78, "type": 21, "state": 0},
		{"id": 902, "title": "系列 乙", "mid": 79, "type": 31, "state": 0},
	}, false)

	a := favApp(t, f, favStore(t, "7"))
	a.AutoRefreshFavorites()

	waitFor(t, waitTimeout, "「我收藏的」那一组拉回来了", func() bool {
		return len(a.SideFavorites(app.FavoritesCollected)) >= 1
	})
	got := a.SideFavorites(app.FavoritesCollected)
	for _, f := range got {
		if f.Type != 11 {
			t.Errorf("侧栏上有一条类型是 %d 的（%q）——合集 / 系列的内容本项目"+
				"拉不出来，摆进去是死的路", f.Type, f.Title)
		}
	}
	if len(got) != 1 {
		t.Errorf("「我收藏的」那一组有 %d 条，want 1（只有那个收藏夹）", len(got))
	}
}

// TestARefreshThatComesBackAfterALogoutIsThrownAway 钉「退登时在飞的那一趟
// 回来之后**不能**把上一个账号的收藏夹填回去」。
//
// ⚠️ **这是这一票新加的自动拉带来的窗口**：刚开机 / 刚登录一两秒内退登，
// 那一趟就还在飞；而它带回来的是**上一个账号**的收藏夹——填回侧栏就是把它
// 泄露给下一个登录的人。
//
// 做法：把 created/list 那一趟**卡在飞** → 退登 → 放行 → 侧栏上必须仍然是
// 空的（而且状态也要复位成「没拉过」）。
func TestARefreshThatComesBackAfterALogoutIsThrownAway(t *testing.T) {
	f := newBiliFixture(t)
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	// ⚠️ **必须放走**：测试收尾时 `httptest.Server.Close` 要等在途的请求回来，
	// 不放就是整个测试挂到 go test 的上限。
	defer release()

	hits := &favHits{}
	f.on(favListPath, func(w http.ResponseWriter, r *http.Request) {
		hits.add()
		<-gate
		writeFixtureJSON(t, w, 0, "0", map[string]any{
			"count": 1, "has_more": false,
			"list": []map[string]any{{"id": 11, "title": "通勤路上", "attr": 0}},
		})
	})
	onFavCollected(t, f, nil, nil, false)

	a := favApp(t, f, favStore(t, "7"))

	// 「叫醒界面」的那一支当「那一趟回来了」的信号：后台那一趟跑完会调它。
	var wakes atomic.Int64
	a.SetWake(func() { wakes.Add(1) })

	a.AutoRefreshFavorites()
	waitFor(t, waitTimeout, "那一趟到了假服务器", func() bool { return hits.count() == 1 })

	// 退登：它把侧栏那两组清掉（**并且代号 +1**）。
	if err := a.Logout(); err != nil {
		t.Fatal(err)
	}
	if got := len(a.SideFavorites(app.FavoritesCreated)); got != 0 {
		t.Fatalf("退登之后侧栏上还有 %d 条", got)
	}
	// 退登自己也会叫醒界面（界面要重画成登出的样子），所以从这儿开始数。
	base := wakes.Load()

	// 放行那一趟。
	release()
	waitFor(t, waitTimeout, "那一趟回来了", func() bool { return wakes.Load() > base })

	if got := len(a.SideFavorites(app.FavoritesCreated)); got != 0 {
		t.Errorf("退登之后那一趟回来，把上一个账号的收藏夹填回去了 %d 条", got)
	}
	if st := a.FavoriteState(app.FavoritesCreated); st.Loaded || st.Err != "" {
		t.Errorf("退登之后状态是 %+v，应该复位成「没拉过」", st)
	}
}

// TestTheOldRefreshDoesNotStealTheNewAccountsRefreshState 钉「旧的那一趟回来
// 的时候不能把新账号那一趟的「正在刷新…」收掉」。
//
// 场景：退登 → 换个人登录 → 新账号的那一趟在飞 → 旧的那一趟这时候才回来。
// 旧的那一趟要是把 busy 收掉，新账号那一趟的「正在刷新收藏夹…」那一行
// 就凭空不见了——而那一行是用户**唯一**看得见的「在飞」证据。
//
// ⚠️ **代号是跟着那一趟走的**（见 `Favorites.gen`）：旧的那一趟带的是旧代号，
// 收尾那一下对不上就什么也不做。
func TestTheOldRefreshDoesNotStealTheNewAccountsRefreshState(t *testing.T) {
	f := newBiliFixture(t)
	// 两个闸门：第一个卡住旧的那一趟，第二个卡住新账号的那一趟。
	gateOld := make(chan struct{})
	gateNew := make(chan struct{})
	releaseOld := sync.OnceFunc(func() { close(gateOld) })
	releaseNew := sync.OnceFunc(func() { close(gateNew) })
	defer releaseOld()
	defer releaseNew()

	var reqs atomic.Int64
	f.on(favListPath, func(w http.ResponseWriter, r *http.Request) {
		switch reqs.Add(1) {
		case 1:
			<-gateOld
		case 2:
			<-gateNew
		}
		writeFixtureJSON(t, w, 0, "0", map[string]any{
			"count": 1, "has_more": false,
			"list": []map[string]any{{"id": 11, "title": "通勤路上", "attr": 0}},
		})
	})
	onFavCollected(t, f, nil, nil, false)

	a := favApp(t, f, favStore(t, "7"))

	var wakes atomic.Int64
	a.SetWake(func() { wakes.Add(1) })

	// 旧账号的那一趟。
	a.AutoRefreshFavorites()
	waitFor(t, waitTimeout, "旧的那一趟到了假服务器", func() bool { return reqs.Load() == 1 })

	// 退登，然后**换个人登录**：登录成功会自动拉一次（`AutoRefreshFavorites`）。
	if err := a.Logout(); err != nil {
		t.Fatal(err)
	}
	f.onScans(0)
	a.BeginLogin()
	defer a.CancelLogin()
	waitFor(t, waitTimeout, "登录成功", func() bool { return a.LoginState().Phase == app.LoginDone })
	waitFor(t, waitTimeout, "新账号的那一趟到了假服务器", func() bool { return reqs.Load() == 2 })

	// 放行旧的那一趟：它回来之后**不能**把新账号那一趟的 busy 收掉。
	base := wakes.Load()
	releaseOld()
	waitFor(t, waitTimeout, "旧的那一趟回来了", func() bool { return wakes.Load() > base })
	if !a.FavoritesRefreshing() {
		t.Error("旧的那一趟回来之后，「正在刷新收藏夹…」那一行不见了——" +
			"而新账号的那一趟还在飞")
	}

	// 放行新账号的那一趟：它跑完之后那一行才收掉。
	releaseNew()
	waitFor(t, waitTimeout, "新账号的那一趟回来了", func() bool { return !a.FavoritesRefreshing() })
}

// TestAFetchThatFoundNothingIsStillLoaded 钉「拉到了，一个都没有」这一档。
//
// 这是**第三档**，和「没拉到」必须分开：一个都没有是 B 站那边的结论，
// 而「没拉到」是我们自己的失败。前者该说「还没有收藏夹」，后者该说
// 「拉不到收藏夹，可以重试」。
func TestAFetchThatFoundNothingIsStillLoaded(t *testing.T) {
	f := newBiliFixture(t)
	hits := &favHits{}
	onFavList(t, f, hits, nil, false)

	a := favApp(t, f, favStore(t, "7"))
	a.AutoRefreshFavorites()

	waitFor(t, waitTimeout, "拉完了", func() bool {
		return a.FavoriteState(app.FavoritesCreated).Loaded
	})
	st := a.FavoriteState(app.FavoritesCreated)
	if st.Err != "" {
		t.Errorf("拉成功了却记着错：%q", st.Err)
	}
	if len(a.SideFavorites(app.FavoritesCreated)) != 0 {
		t.Errorf("拉回来 %d 条，want 0", len(a.SideFavorites(app.FavoritesCreated)))
	}
}
