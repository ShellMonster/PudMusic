// 收藏夹管理这一摊的业务层测试。
//
// **package app 而不是 app_test**：这一组要钉「翻页的步长就是 100」这个
// 不变式，而它没有导出。
//
// ⚠️ fixture 是假的 B 站，不是真网：它证明的是「我们一页 100 地翻到完」、
// 「结果只被取走一次」、「新建完侧栏立刻能看到」这些，**不是**「B 站真的
// 有那么多收藏夹」。

package app

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// folderFixture 是一台只认「收藏夹管理」那几个接口的假 B 站。
type folderFixture struct {
	*httptest.Server

	mu sync.Mutex
	// pages 是 created/list 会回的几页。**最后一页故意给不满**，
	// 逼翻页循环走「少于一步长」那一支（见 fetchCreatedFolders）。
	pages [][]map[string]any
	// ps 记下每一次请求的 ps，pn 记下 pn。
	ps []string
	pn []string
	// added / deleted 记下 deal 那一次放了什么、拿了什么。
	added   []string
	deleted []string
	// coverURL 是封面上传会回的地址。
	coverURL string
	// createTitle 是 add 接口回的新收藏夹名字（默认照请求里的 title 回，
	// 测试可以改成「B 站改过的名字」来验侧栏用的是接口那一份）。
	createTitle string
}

func newFolderFixture(t *testing.T) *folderFixture {
	t.Helper()
	f := &folderFixture{coverURL: "https://i0.hdslb.com/cover.jpg"}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/x/v3/fav/folder/created/list":
			page, _ := strconv.Atoi(r.URL.Query().Get("pn"))
			f.ps = append(f.ps, r.URL.Query().Get("ps"))
			f.pn = append(f.pn, r.URL.Query().Get("pn"))
			list := []map[string]any{}
			if page >= 1 && page <= len(f.pages) {
				list = f.pages[page-1]
			}
			writeCollectionJSON(t, w, 0, "0", map[string]any{
				"count": 200, "list": list, "has_more": page < len(f.pages),
			})
		case "/x/v3/fav/folder/add", "/x/v3/fav/folder/edit":
			_ = r.ParseForm()
			title := f.createTitle
			if title == "" {
				title = r.PostForm.Get("title")
			}
			id, _ := strconv.Atoi(r.PostForm.Get("media_id"))
			if id == 0 {
				id = 999
			}
			writeCollectionJSON(t, w, 0, "0", map[string]any{
				"id": id, "title": title, "mid": 7, "type": 11, "attr": 0,
			})
		case "/x/vu/web/cover/up":
			_ = r.ParseForm()
			writeCollectionJSON(t, w, 0, "0", map[string]any{"url": f.coverURL})
		case "/x/v3/fav/resource/deal":
			_ = r.ParseForm()
			f.added = append(f.added, r.PostForm.Get("add_media_ids"))
			f.deleted = append(f.deleted, r.PostForm.Get("del_media_ids"))
			writeCollectionJSON(t, w, 0, "0", nil)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// folderOf 造一个 created/list 会回的收藏夹。
func folderOf(id int, title string, faved bool) map[string]any {
	state := 0
	if faved {
		state = 1
	}
	return map[string]any{
		"id": id, "title": title, "attr": 2, "media_count": 3, "fav_state": state,
	}
}

// fullPage 造一页装满 100 条的（步长就是 100，见
// `bili.CreatedFolderPageSize`）。
func fullPage(from int) []map[string]any {
	out := make([]map[string]any, 0, bili.CreatedFolderPageSize)
	for i := range bili.CreatedFolderPageSize {
		out = append(out, folderOf(from+i, "收藏夹 "+strconv.Itoa(from+i), false))
	}
	return out
}

func (f *folderFixture) asks() (ps, pn []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ps...), append([]string(nil), f.pn...)
}

func (f *folderFixture) deals() (added, deleted []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.added...), append([]string(nil), f.deleted...)
}

// folderApp 装一个指向这台假 B 站的 App。
//
// ⚠️ **必须 SeedCookies 一个 DedeUserID**：拉「我创建的收藏夹」要 `up_mid`，
// 而我们是从 cookie 里读它的（见 `bili.Client.SelfMid` 的注释）。没装的话
// 这一层会走「还没登录」那一支——那一支另有测试。
func folderApp(t *testing.T, f *folderFixture) *App {
	t.Helper()
	a := New()
	t.Cleanup(a.Close)
	transport := &CredentialTransport{
		Base:   f.Server.Client().Transport,
		Policy: DefaultInjectPolicy().AllowBiliHost(hostOf(f.URL)),
	}
	cli := bili.NewClient(bili.Options{
		Transport: transport,
		CSRF:      func() string { return "jct" },
	})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	if err := cli.SeedCookies([]*http.Cookie{{Name: "DedeUserID", Value: "7", Path: "/"}}); err != nil {
		t.Fatal(err)
	}
	a.MountCollections(CollectionRefs{Client: cli, Settle: 0})
	return a
}

// ── 拉全量 ─────────────────────────────────────────────────────────────────

// TestLoadsEveryFolderOneHundredAtATime 一页 100，一直翻到翻完。
//
// 🔴 这一条钉的是**翻页的步长就是 `bili.CreatedFolderPageSize`**：界面那边
// 照抄这个常量，两处一旦漂了就是「翻到第二页时步长变了」——用户有 150 个
// 收藏夹时第 101 个开始会重复或者漏掉，而界面上完全看不出来。
func TestLoadsEveryFolderOneHundredAtATime(t *testing.T) {
	f := newFolderFixture(t)
	f.pages = [][]map[string]any{fullPage(1), {{"id": 201, "title": "最后一个", "attr": 2, "media_count": 1, "fav_state": 1}}}
	a := folderApp(t, f)

	a.LoadCreatedFolders(77)
	waitUntil(t, "两页都翻完", 2*time.Second, func() bool {
		list, loaded := a.CreatedFolderChoices(77)
		return loaded && len(list) == 101
	})

	list, loaded := a.CreatedFolderChoices(77)
	if !loaded {
		t.Fatal("翻完了却说没翻完")
	}
	if list[100].Title != "最后一个" {
		t.Errorf("最后一个收藏夹 = %q", list[100].Title)
	}
	// ⚠️ 最后一页不满，所以**不该再多翻一页**：多翻的那一页是空的，而 B 站
	// 偶尔会在最后一页上说 has_more，多翻一页的表现是列表里多出一行
	// 「空白的收藏夹」。
	ps, pn := f.asks()
	if len(ps) != 2 {
		t.Fatalf("发了 %d 次请求（%v），want 2", len(ps), pn)
	}
	for _, got := range ps {
		if got != "100" {
			t.Errorf("ps = %q，want 100", got)
		}
	}
	if pn[0] != "1" || pn[1] != "2" {
		t.Errorf("页码 = %v，want [1 2]", pn)
	}
}

// TestFoldersAreGroupedByTheContentAsked 换一首歌要重新拉一份。
//
// ⚠️ 选择弹窗问的是「这条内容在不在每个收藏夹里」，那个答案因内容而异。
// 只按 rid=0 存一份的话，第二首歌打开弹窗时看到的是上一首歌的勾选——
// 用户点确认之后 B 站会把一条 del_media_ids 发出去，把别的歌踢掉。
func TestFoldersAreGroupedByTheContentAsked(t *testing.T) {
	f := newFolderFixture(t)
	f.pages = [][]map[string]any{{folderOf(1, "在的", true), folderOf(2, "不在的", false)}}
	a := folderApp(t, f)

	a.LoadCreatedFolders(77)
	waitUntil(t, "第一份拉回来", 2*time.Second, func() bool {
		_, ok := a.CreatedFolderChoices(77)
		return ok
	})

	if list, _ := a.CreatedFolderChoices(78); list != nil {
		t.Errorf("问的是 78 却拿到了 77 那份：%v", list)
	}
	a.LoadCreatedFolders(78)
	waitUntil(t, "第二份拉回来", 2*time.Second, func() bool {
		_, ok := a.CreatedFolderChoices(78)
		return ok
	})
}

// TestFolderListNeedsALogin 没登录就说没登录，不发请求。
//
// 拉的是「**我**创建的收藏夹」，没登录时 mid 是 0，而 B 站对 up_mid=0 会回
// 一堆别人的东西或者干脆 -400——两种结果在界面上都是「收藏夹是空的」。
func TestFolderListNeedsALogin(t *testing.T) {
	f := newFolderFixture(t)
	a := New()
	t.Cleanup(a.Close)
	transport := &CredentialTransport{
		Base:   f.Server.Client().Transport,
		Policy: DefaultInjectPolicy().AllowBiliHost(hostOf(f.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountCollections(CollectionRefs{Client: cli, Settle: 0})

	a.LoadCreatedFolders(77)
	res := waitFolderResult(t, a, 2*time.Second)
	if res.OK {
		t.Errorf("没登录却说成功了：%q", res.Text)
	}
	if _, ok := a.CreatedFolderChoices(77); ok {
		t.Error("没登录却拿到了一份收藏夹")
	}
	// ⚠️ 话里要带上「登录」两个字：用户看到一句「拉不到收藏夹列表」只会去
	// 检查网络，然后重试五次。
	if !strings.Contains(res.Text, "登录") {
		t.Errorf("没登录时的话是 %q，没提「登录」", res.Text)
	}
}

// ── 结果出口 ───────────────────────────────────────────────────────────────

// TestResultsAreTakenOnce 结果只能被取走一次。
//
// 🔴 这一条钉的是**「取走」而不是「读」**：读而不取的话同一句提示会在界面
// 每一帧都发一次 Toast，而弹窗的「提交中」会在第一帧就解除——用户看着弹窗
// 关掉了却以为没生效，又点一次提交，于是建出两个同名收藏夹。
func TestResultsAreTakenOnce(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)
	a.MountCollections(CollectionRefs{})

	// 没挂客户端时也取不到，而不是崩。
	if _, ok := a.TakeFolderResult(); ok {
		t.Error("没挂的时候竟然取得到结果")
	}
	if _, ok := a.TakeCoverURL(); ok {
		t.Error("没挂的时候竟然取得到封面地址")
	}

	f := newFolderFixture(t)
	a2 := folderApp(t, f)
	a2.DealFolderMedias(77, 2, []int64{1}, nil)
	res := waitFolderResult(t, a2, 2*time.Second)
	if !res.OK {
		t.Fatalf("改收藏夹失败了：%q", res.Text)
	}
	if _, ok := a2.TakeFolderResult(); ok {
		t.Error("同一条结果被取到了两次")
	}
}

// TestCoverURLIsItsOwnChannel 封面上传成功走的是另一个出口。
//
// ⚠️ 封面上传成功**不是**一句要说的提示，它是一份要给表单用的地址。混在
// Toast 那一个出口里的话，界面就得从每条结果里猜「这条是不是封面」——而
// 失败的提示和成功的结果长得一样。
func TestCoverURLIsItsOwnChannel(t *testing.T) {
	f := newFolderFixture(t)
	a := folderApp(t, f)

	a.UploadCover("data:image/jpeg;base64,AAAA")
	waitUntil(t, "封面地址回来", 2*time.Second, func() bool {
		_, ok := a.TakeCoverURL()
		return ok
	})
	// 第一次取走之后就没有了——下一张图会重新来一份。
	if _, ok := a.TakeCoverURL(); ok {
		t.Error("同一个封面地址被取到了两次")
	}
}

// ── 写 ─────────────────────────────────────────────────────────────────────

// TestCreatingAFolderPutsItInTheSidebar 建完之后侧栏立刻能看到它。
//
// 而且侧栏上写的是**接口回回来的名字**，不是用户敲的那个：B 站会改名字，
// 两边不一致的话用户会看到「弹窗里叫 A、侧栏里叫 B」直到下次刷新。
func TestCreatingAFolderPutsItInTheSidebar(t *testing.T) {
	f := newFolderFixture(t)
	f.createTitle = "B 站改过的名字"
	a := folderApp(t, f)

	a.CreateFolder(bili.FolderDraft{Title: "我敲的名字"})
	res := waitFolderResult(t, a, 2*time.Second)
	if !res.OK {
		t.Fatalf("建收藏夹失败了：%q", res.Text)
	}
	if res.Folder != 999 {
		t.Errorf("回执里的新 id = %d，want 999", res.Folder)
	}
	got := a.SideFavorites(FavoritesCreated)
	if len(got) != 1 {
		t.Fatalf("侧栏里有 %d 个，want 1", len(got))
	}
	if got[0].Title != "B 站改过的名字" {
		t.Errorf("侧栏上写的是 %q，want 接口回回来的那个", got[0].Title)
	}
}

// TestEditingKeepsTheSpotInTheSidebar 改名字不能把侧栏顺序冲掉。
//
// 侧栏顺序是用户拖出来的（落盘在设置里）。改一次名字就把它挪到末尾的话，
// 用户下一次打开侧栏会发现刚整理好的顺序白整理了。
func TestEditingKeepsTheSpotInTheSidebar(t *testing.T) {
	f := newFolderFixture(t)
	a := folderApp(t, f)
	a.SetFavorites(FavoritesCreated, []Favorite{
		{ID: 1, Title: "甲"}, {ID: 2, Title: "乙"}, {ID: 3, Title: "丙"},
	})

	f.mu.Lock()
	f.createTitle = "乙改了"
	f.mu.Unlock()
	a.EditFolder(2, bili.FolderDraft{Title: "乙改了"})
	if res := waitFolderResult(t, a, 2*time.Second); !res.OK {
		t.Fatalf("改收藏夹失败了：%q", res.Text)
	}

	got := a.SideFavorites(FavoritesCreated)
	want := []string{"甲", "乙改了", "丙"}
	for i := range want {
		if got[i].Title != want[i] {
			t.Fatalf("侧栏顺序 = %v，want %v", titles(got), want)
		}
	}
}

func titles(in []Favorite) []string {
	out := make([]string, 0, len(in))
	for _, f := range in {
		out = append(out, f.Title)
	}
	return out
}

// TestDealSaysWhichOfTheThreeThingsHappened 改完之后说的话分三档。
//
// ⚠️ 三句话对三件事，说错的话用户会以为自己的勾白勾了：Biu 也是这么分的
// （`components/favorites-select-modal/index.tsx:125-138`）。
func TestDealSaysWhichOfTheThreeThingsHappened(t *testing.T) {
	for _, tc := range []struct {
		name        string
		add, del    []int64
		wantText    string
		wantDeleted string
	}{
		{"只加", []int64{1}, nil, "已添加到收藏夹", ""},
		{"只拿", nil, []int64{1}, "已从收藏夹中移除", "1"},
		{"又加又拿", []int64{1}, []int64{2}, "修改成功", "2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFolderFixture(t)
			a := folderApp(t, f)
			a.DealFolderMedias(77, 2, tc.add, tc.del)
			res := waitFolderResult(t, a, 2*time.Second)
			if !res.OK {
				t.Fatalf("失败了：%q", res.Text)
			}
			if res.Text != tc.wantText {
				t.Errorf("那句话 = %q，want %q", res.Text, tc.wantText)
			}
			added, deleted := f.deals()
			if len(added) != 1 || len(deleted) != 1 {
				t.Fatalf("deal 收到了 %d 次放、%d 次拿", len(added), len(deleted))
			}
			if deleted[0] != tc.wantDeleted {
				t.Errorf("del_media_ids = %q，want %q", deleted[0], tc.wantDeleted)
			}
		})
	}
}

// waitFolderResult 等第一条后台结果回来。
func waitFolderResult(t *testing.T, a *App, d time.Duration) FolderResult {
	t.Helper()
	var res FolderResult
	waitUntil(t, "后台结果回来", d, func() bool {
		var ok bool
		res, ok = a.TakeFolderResult()
		return ok
	})
	return res
}
