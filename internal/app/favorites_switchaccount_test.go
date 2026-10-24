// 这一组盯的是**换账号**（issue #398）。
//
// # 漏掉的是哪条路
//
// 「在飞的那一趟回来之后不能把上一个账号的收藏夹填回侧栏」这条不变量，
// **只在「退登」那条路上成立**。而漏掉的是「**已登录时再扫一次码，登录成另一个
// 账号**」——于是：
//
//   - 世代（`Favorites.gen`）不动 → 旧账号那一趟**照样算数** → 它回来就把
//     **上一个账号的收藏夹**填回侧栏；
//   - 「正在刷」那个闸门还关着 → 新账号那一趟**一个请求都不发** → 新账号的
//     收藏夹**永远不来**。
//
// 结果是侧栏上是上一个账号的收藏夹，而新账号的也拉不到。
//
// # 做法
//
// 把 created/list 那一趟卡在飞 → 已登录时再扫一次码，扫出来的是另一个人 →
// 放行。回答按 `up_mid` 分，所以「侧栏上摆的是谁的歌单」是从**内容**上读出来
// 的，而不是从「请求发了几回」推出来的。
package app_test

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 换账号这一组用例里的两个账号，和它们各自的歌单名。
const (
	switchOldUID = "111"
	switchNewUID = "222"

	switchOldTitle = "旧账号的歌单"
	switchNewTitle = "新账号的歌单"
)

// switchFixture 装好「收藏夹那一趟分两次回答」的假 B 站。
//
// 第一趟（旧账号那一趟）卡在 gateOld 上，第二趟（新账号那一趟）卡在 gateNew 上。
// 放行顺序由测试自己控制——先放哪一个，就是造哪一种竞态。
//
// ⚠️ **回答按 up_mid 分**：问的是哪个账号，就回那个账号的歌单。这样「侧栏上摆的
// 是谁的歌单」是从内容上读出来的，而不是从「请求发了几回」推出来的。
func switchFixture(t *testing.T, f *biliFixture, reqs *atomic.Int64, gateOld, gateNew chan struct{}) {
	t.Helper()
	f.on(favListPath, func(w http.ResponseWriter, r *http.Request) {
		mid := r.URL.Query().Get("up_mid")
		switch reqs.Add(1) {
		case 1:
			<-gateOld
		case 2:
			<-gateNew
		}
		title := switchOldTitle
		if mid == switchNewUID {
			title = switchNewTitle
		}
		writeFixtureJSON(t, w, 0, "0", map[string]any{
			"count": 1, "has_more": false,
			"list": []map[string]any{{"id": 11, "title": title, "attr": 0}},
		})
	})
	onFavCollected(t, f, nil, nil, false)
}

// TestSwitchingAccountsPullsTheNewAccountsFavorites 钉「换账号之后新账号的收藏夹
// **真的被拉了**」。
//
// ⚠️ **判据是「真的发出去了一次请求」**：写成「侧栏上有收藏夹了」的话，哪天
// 那一趟被换成读缓存，照样全绿——而用户看到的还是空的。
//
// ⚠️ **旧账号那一趟卡在飞**：不卡的话它早就跑完了，「正在刷」那个闸门是关着的，
// 而这一趟要验的正是**闸门开着的时候**新账号那一趟发不发得出去。
func TestSwitchingAccountsPullsTheNewAccountsFavorites(t *testing.T) {
	f := newBiliFixture(t)
	var reqs atomic.Int64
	gateOld := make(chan struct{})
	gateNew := make(chan struct{})
	releaseOld := sync.OnceFunc(func() { close(gateOld) })
	releaseNew := sync.OnceFunc(func() { close(gateNew) })
	defer releaseOld()
	defer releaseNew()
	switchFixture(t, f, &reqs, gateOld, gateNew)

	a := favApp(t, f, favStore(t, switchOldUID))

	// 旧账号那一趟：卡在飞。
	a.AutoRefreshFavorites()
	waitFor(t, waitTimeout, "旧账号那一趟到了假服务器", func() bool { return reqs.Load() == 1 })

	// 换人：已登录的时候再扫一次码，扫出来的是**另一个人**。
	f.onScanAs(switchNewUID)
	f.onScans(0)
	a.BeginLogin()
	defer a.CancelLogin()
	waitFor(t, waitTimeout, "登录成另一个人", func() bool { return a.LoginState().Phase == app.LoginDone })

	// ⚠️ **新账号那一趟必须发得出去**：旧账号那一趟还在飞（「正在刷」还关着），
	// 而它要是被挡掉了，新账号的收藏夹就**永远不来**。
	waitFor(t, waitTimeout, "新账号那一趟到了假服务器", func() bool { return reqs.Load() == 2 })

	releaseOld()
	releaseNew()
	waitFor(t, waitTimeout, "新账号的收藏夹进侧栏了", func() bool {
		return len(a.SideFavorites(app.FavoritesCreated)) == 1
	})
	got := a.SideFavorites(app.FavoritesCreated)
	if got[0].Title != switchNewTitle {
		t.Errorf("侧栏上是 %q，want %q", got[0].Title, switchNewTitle)
	}
}

// TestSwitchingAccountsThrowsAwayTheOldAccountsFavorites 钉「换账号之后旧账号的
// 收藏夹**填不进侧栏**」。
//
// 这一条是那条例外不变量的另一半（issue #398）：旧账号那一趟回来的时候带着的是
// **上一个账号**的收藏夹，填回侧栏就是把它泄露给刚登上的这个人。
//
// ⚠️ **三段都要看**：换完人侧栏上得是空的（旧账号那份已经清掉了）；旧的那一趟
// 回来之后**仍然是空的**（世代守卫挡住了）；新账号那一趟回来之后才是新账号的
// 歌单。
//
// ⚠️ **旧的那一趟不能把新账号那一趟的在飞状态收掉**：新账号那一趟还在飞，而
// 「正在刷新收藏夹…」那一行是用户**唯一**看得见的在飞证据。
func TestSwitchingAccountsThrowsAwayTheOldAccountsFavorites(t *testing.T) {
	f := newBiliFixture(t)
	var reqs atomic.Int64
	gateOld := make(chan struct{})
	gateNew := make(chan struct{})
	releaseOld := sync.OnceFunc(func() { close(gateOld) })
	releaseNew := sync.OnceFunc(func() { close(gateNew) })
	defer releaseOld()
	defer releaseNew()
	switchFixture(t, f, &reqs, gateOld, gateNew)

	a := favApp(t, f, favStore(t, switchOldUID))

	// 「叫醒界面」的那一支当「那一趟回来了」的信号：后台那一趟跑完会调它。
	var wakes atomic.Int64
	a.SetWake(func() { wakes.Add(1) })

	a.AutoRefreshFavorites()
	waitFor(t, waitTimeout, "旧账号那一趟到了假服务器", func() bool { return reqs.Load() == 1 })

	f.onScanAs(switchNewUID)
	f.onScans(0)
	a.BeginLogin()
	defer a.CancelLogin()
	waitFor(t, waitTimeout, "登录成另一个人", func() bool { return a.LoginState().Phase == app.LoginDone })
	waitFor(t, waitTimeout, "新账号那一趟到了假服务器", func() bool { return reqs.Load() == 2 })

	// 换完人之后，侧栏上**不许**还摆着旧账号的收藏夹。
	if got := a.SideFavorites(app.FavoritesCreated); len(got) != 0 {
		t.Errorf("换完人侧栏上还有 %d 条旧账号的收藏夹", len(got))
	}
	// ⚠️ **状态也要复位**：不清的话界面上那一句是「还没有收藏夹」，而真相是
	// 「这个账号的还没拉过」。
	if st := a.FavoriteState(app.FavoritesCreated); st.Loaded || st.Err != "" {
		t.Errorf("换完人状态是 %+v，应该复位成「没拉过」", st)
	}
	// 而新账号那一趟确实在飞——界面上该有一句「正在刷新收藏夹…」。
	if !a.FavoritesRefreshing() {
		t.Error("换完人之后「正在刷新收藏夹…」那一行不见了——而新账号那一趟还在飞")
	}

	// 放行旧账号那一趟：它回来之后**填不进去**。
	base := wakes.Load()
	releaseOld()
	waitFor(t, waitTimeout, "旧账号那一趟回来了", func() bool { return wakes.Load() > base })
	if got := a.SideFavorites(app.FavoritesCreated); len(got) != 0 {
		t.Errorf("旧账号那一趟回来之后侧栏上有 %d 条——那是上一个账号的收藏夹", len(got))
	}
	if st := a.FavoriteState(app.FavoritesCreated); st.Loaded {
		t.Error("旧账号那一趟回来之后状态说「拉到了」——那也是上一个账号的结论")
	}
	// 旧的那一趟不能把新账号那一趟的在飞状态收掉。
	if !a.FavoritesRefreshing() {
		t.Error("旧账号那一趟回来之后，「正在刷新收藏夹…」那一行不见了——" +
			"而新账号那一趟还在飞")
	}

	// 放行新账号那一趟：侧栏上才是新账号的歌单。
	releaseNew()
	waitFor(t, waitTimeout, "新账号的收藏夹进侧栏了", func() bool {
		return len(a.SideFavorites(app.FavoritesCreated)) == 1
	})
	got := a.SideFavorites(app.FavoritesCreated)
	if got[0].Title != switchNewTitle {
		t.Errorf("侧栏上是 %q，want %q", got[0].Title, switchNewTitle)
	}
}

// TestSwitchingAccountsKeepsTheLogoutPathWorking 钉「退登那条路没被改坏」。
//
// ⚠️ **退登和换账号现在走同一个钩子**（见 `App.onAccountChanged`），而钩子里
// 有一件更要紧的事：**代号 +1**。少了它，退登时在飞的那一趟就会把上一个账号的
// 收藏夹填回侧栏——而退登那条路本来是对的（issue #96 起就有）。
//
// 这一条把两条路并排钉住：退登之后侧栏上是空的，旧的那一趟回来也填不进去。
func TestSwitchingAccountsKeepsTheLogoutPathWorking(t *testing.T) {
	f := newBiliFixture(t)
	var reqs atomic.Int64
	gateOld := make(chan struct{})
	releaseOld := sync.OnceFunc(func() { close(gateOld) })
	defer releaseOld()
	f.on(favListPath, func(w http.ResponseWriter, r *http.Request) {
		reqs.Add(1)
		<-gateOld
		writeFixtureJSON(t, w, 0, "0", map[string]any{
			"count": 1, "has_more": false,
			"list": []map[string]any{{"id": 11, "title": switchOldTitle, "attr": 0}},
		})
	})
	onFavCollected(t, f, nil, nil, false)

	a := favApp(t, f, favStore(t, switchOldUID))

	var wakes atomic.Int64
	a.SetWake(func() { wakes.Add(1) })

	a.AutoRefreshFavorites()
	waitFor(t, waitTimeout, "旧账号那一趟到了假服务器", func() bool { return reqs.Load() == 1 })

	if err := a.Logout(); err != nil {
		t.Fatal(err)
	}
	if got := len(a.SideFavorites(app.FavoritesCreated)); got != 0 {
		t.Fatalf("退登之后侧栏上还有 %d 条", got)
	}

	base := wakes.Load()
	releaseOld()
	waitFor(t, waitTimeout, "旧账号那一趟回来了", func() bool { return wakes.Load() > base })
	if got := len(a.SideFavorites(app.FavoritesCreated)); got != 0 {
		t.Errorf("退登之后那一趟回来，把上一个账号的收藏夹填回去了 %d 条", got)
	}
}
