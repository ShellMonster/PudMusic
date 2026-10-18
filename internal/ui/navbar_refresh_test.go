// 头像下拉里「刷新数据」那一项（issue #96 的第二条验收）。
//
// 那一项以前按下去只弹一句「还没有数据可刷新」——而收藏夹接口是有的。现在它
// 真的重新拉一次侧栏那些收藏夹：喂一个**会变**的数据源，点了之后断言画面上的
// 列表换了内容。
//
// 跑的是 MyGo 的 Tester：B 站是假的（`folderBili`，见 foldertest_test.go）。

package ui

import (
	"context"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// refreshShell 造一个「登录了 + 收藏夹那一摊指着假 B 站」的外壳。
//
// mid 是空串就是没登录——「刷新」在没登录时该说什么，是这一组要验的东西之一。
//
// ⚠️ **唤醒一定要装**（同 folderShell）：后台那一趟跑完要叫醒界面，而真窗口里
// 这一句在 `Shell.Attach` 里。测试不走 Attach。
func refreshShell(t *testing.T, f *folderBili, mid string) (*Shell, *mygoui.Tester) {
	t.Helper()
	s, tt := newShell(t, mounted(func(t *testing.T, a *app.App) {
		cli := fixtureClient(t, f)
		if mid != "" {
			store := app.NewMemoryStore("test")
			if err := store.Save(context.Background(),
				app.Session{SESSDATA: "sess", DedeUserID: mid}); err != nil {
				t.Fatal(err)
			}
			a.MountLogin(store, cli)
		}
		// ⚠️ **在 MountLogin 之后**再装一遍 cookie：它的 restore 会照着登录态
		// 重置一次 jar（见 app/account.go 的 restore），而这一份登录态里
		// 没有 Cookies——于是 DedeUserID 会被抹掉，而「我创建了哪些收藏夹」
		// 问的正是 up_mid，抹掉了那一趟只会拿到一句「还没登录」。
		if err := cli.SeedCookies([]*http.Cookie{
			{Name: "DedeUserID", Value: "7", Path: "/"},
		}); err != nil {
			t.Fatal(err)
		}
		a.MountCollections(app.CollectionRefs{Client: cli, Settle: 0})
	}))
	s.app.SetWake(s.wake.invalidate)
	return s, tt
}

// chooseRefresh 点开头像下拉，选「刷新数据」。
func chooseRefresh(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	tt.SetFocused(true)
	if err := tt.Click(avatarLabel); err != nil {
		t.Fatalf("点不到头像：%v，文字有 %q", err, tt.Texts())
	}
	if err := tt.ChooseMenuItem("刷新数据"); err != nil {
		t.Fatalf("下拉里选不了「刷新数据」：%v", err)
	}
}

// refreshList 是「拉回来的收藏夹在画面上了吗」。
func refreshList(tt *mygoui.Tester, want ...string) bool {
	for _, w := range want {
		if !tt.HasText(w) {
			return false
		}
	}
	return true
}

// TestRefreshingReallyPullsTheFoldersAgain 是这一票的第二条验收：
// 喂一个会变的数据源，点「刷新数据」，断言**画面上那一列换了内容**。
//
// ## 为什么断言画面而不是内存里那份
//
// 判据是「刷新有没有真的走到界面上」。写成「`SideFavorites` 返回了新值」的话，
// 哪天这一趟换了一个出口（比如换成推给某个对话框），界面照样停在旧内容，而这一条
// 照样全绿——那正是这一票要消掉的那一种错。
//
// ⚠️ **两头都断言**：旧的必须消失。只断言新的一条出现的话，「刷新」退化成
// 「往里加」照样能过，而用户看见的是一个越刷越长的收藏夹列表。
func TestRefreshingReallyPullsTheFoldersAgain(t *testing.T) {
	f := newFolderBili(t, nil)
	f.pages = [][]map[string]any{folderPage(1, 2)}

	_, tt := refreshShell(t, f, "7")
	tt.Frame()
	if refreshList(tt, "收藏夹 1", "收藏夹 2") {
		t.Fatalf("起点侧栏上就有收藏夹了（谁塞的？），文字有 %q", tt.Texts())
	}

	chooseRefresh(t, tt)
	waitFrames(t, tt, "第一次刷新之后侧栏上出现了刚拉回来的收藏夹", func() bool {
		return refreshList(tt, "收藏夹 1", "收藏夹 2")
	})
	// 顺带把「刷新完了有一句话」钉住——一句都不说的话，用户不知道到底成了没有。
	waitFrames(t, tt, "刷新完的那句话", func() bool {
		return tt.HasText("已刷新「我创建的」：2 个")
	})

	// 换一个**内容完全不同**的数据源，再刷一次。B 站网页上改收藏夹是这件事的
	// 真身：改名字、删掉、新建。
	f.lock()
	f.pages = [][]map[string]any{folderPage(900, 2)}
	f.unlock()

	chooseRefresh(t, tt)
	waitFrames(t, tt, "第二次刷新之后侧栏换成了新的一份", func() bool {
		return refreshList(tt, "收藏夹 900", "收藏夹 901")
	})
	for _, gone := range []string{"收藏夹 1", "收藏夹 2"} {
		if tt.HasText(gone) {
			t.Errorf("刷新之后旧的那一条「%s」还在，文字有 %q", gone, tt.Texts())
		}
	}
}

// TestRefreshingTwiceSendsOneRequestAndSaysItIsBusy 钉第四条验收：
// 刷新期间有进行中状态，连点不重复发。
//
// ## 为什么要在飞的时候钉
//
// 「连点两下只发一次」这句话在刷新**已经跑完之后**验不出来：那时候再点一次就是
// 第二次合法的刷新，两次请求是应该的。所以服务器必须卡住，让第二次点击发生在
// 第一趟还在飞的时候（`folderBili.listGate`，见它那一段的注释）。
//
// ## 判据是「发出去几次」，不是「界面上长什么样」
//
// 界面上那一行「正在刷新收藏夹…」管的是「用户看得见进行中」，而**连点不重复发**
// 是应用层那道闸门的事（`favorites_refresh.go` 的 `tryBeginRefresh`）。把两件事
// 分成两条判据，才不会因为其中一件坏掉而看不出另一件。
func TestRefreshingTwiceSendsOneRequestAndSaysItIsBusy(t *testing.T) {
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	// ⚠️ **必须放走**：测试收尾时 `httptest.Server.Close` 要等在途的请求回来，
	// 不放就是整个测试挂到 go test 的上限（defers 先于 t.Cleanup 跑，所以这一句
	// 一定排在 Close 前面）。
	defer release()

	f := newFolderBili(t, nil)
	f.listGate = gate
	f.pages = [][]map[string]any{folderPage(1, 2)}

	_, tt := refreshShell(t, f, "7")
	tt.Frame()

	chooseRefresh(t, tt)
	waitFrames(t, tt, "侧栏上出现那一行进行中", func() bool {
		return tt.HasText(refreshLine)
	})
	waitFor(t, "第一趟请求到了假服务器", func() bool { return f.arrivals() >= 1 })

	// 第二下。它必须被挡下来，而挡下来这件事也要说给用户听——
	// 而且说的是**另一句话**（侧栏那一行说的是「在飞」，这里说的是「你这一下
	// 没发出去、因为已经在刷了」）。
	chooseRefresh(t, tt)
	tt.Frame()
	if !tt.HasText(refreshLine) {
		t.Errorf("第二下之后进行中那一行不见了，文字有 %q", tt.Texts())
	}
	if !tt.HasText("已经在刷了，等这一趟完") {
		t.Errorf("第二下之后没有说为什么没反应，文字有 %q", tt.Texts())
	}

	release()
	// ⚠️ **先等那句话，再等那一行收掉**：提示只活两秒（`toastLifetime`），
	// 而那一行要等那条「已经在刷了」过期才真的从画面上消失——反过来等的话，
	// 那两秒会把要看的这句吃掉，实测 6 次里飘 1 次。
	waitFrames(t, tt, "刷完的那句话", func() bool {
		return tt.HasText("已刷新「我创建的」：2 个")
	})
	waitFrames(t, tt, "刷完之后进行中那一行收掉", func() bool {
		return !tt.HasText(refreshLine)
	})

	// ⚠️ **在跑完之后数**：第二下要是真发了请求，它在放走闸门的那一刻就已经到
	// 假服务器了（那台机器上一次往返几毫秒），而第一趟也已经回来了——所以此刻
	// 还在数，才不会把「还没来得及发」当成「没发」。
	if got := f.arrivals(); got != 1 {
		t.Errorf("连点两下发了 %d 次列表请求，want 1", got)
	}
	if _, pn := f.asks(); !slices.Equal(pn, []string{"1"}) {
		t.Errorf("请求的页码是 %v，want [1]", pn)
	}
}

// TestRefreshingWithoutALoginSaysSoAndSendsNothing 钉「没登录时这一项说什么」。
//
// 🔴 **不发请求**是这一条的一半，而且是更重要的那半：未登录去试登录后的接口，
// B 站一律回 -101，界面上看不出异样，可真机上那一片 401 噪声会让别的排查
// 全部跑偏（issue #91「权限」那一节写的就是这一条）。
func TestRefreshingWithoutALoginSaysSoAndSendsNothing(t *testing.T) {
	f := newFolderBili(t, nil)
	f.pages = [][]map[string]any{folderPage(1, 2)}

	_, tt := refreshShell(t, f, "")
	tt.Frame()

	chooseRefresh(t, tt)
	waitFrames(t, tt, "说清楚还没登录", func() bool {
		return tt.HasText("还没登录，登录之后才能刷新收藏夹")
	})
	if tt.HasText(refreshLine) {
		t.Errorf("没登录还进了在飞状态，文字有 %q", tt.Texts())
	}
	if _, pn := f.asks(); len(pn) != 0 {
		t.Errorf("没登录也发了 %d 次列表请求（页码 %v）：未登录的路径不该去试登录后的接口",
			len(pn), pn)
	}
}

// TestRefreshingWithoutTheFoldersMountedSaysSo 钉另一档拒绝：没挂上接口。
//
// 那一档在真机上基本不出现（客户端是 `main` 挂的），而它出错的症状是**按下去
// 什么都不发生**——所以照样得说一句话。
func TestRefreshingWithoutTheFoldersMountedSaysSo(t *testing.T) {
	_, tt := newLoggedInShell(t, "7")
	tt.Frame()

	chooseRefresh(t, tt)
	waitFrames(t, tt, "说清楚这一摊还没接上", func() bool {
		return tt.HasText("刷不了收藏夹：这一摊还没接上 B 站接口")
	})
}
