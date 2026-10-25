// 收藏夹写操作（新建 / 修改 / 封面 / 全量列表 / 增删内容）的测试。
//
// ⚠️ 和 collection_test.go 顶上那句话一样：这里没有一个断言在证明「B 站真的
// 这么返回」。它证明的是**我们发出去的形状**（参数名、隐私值、空值怎么处理、
// 打到哪个主机上）和**我们读回来的字段**。真接口的兼容性只能靠真机回归。

package bili

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// ── 拉全量 ─────────────────────────────────────────────────────────────────

// TestCreatedFoldersAsksForAHundred 拉收藏夹列表时一页要 100 条。
//
// 这一条是「分页拉全量」能被实现的前提：翻页的步长就是它，界面那边照抄这个
// 常量（见 `app.LoadCreatedFolders`），所以它变了一定要两边一起变。
func TestCreatedFoldersAsksForAHundred(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathFolderCreated, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("ps"); got != "100" {
			t.Errorf("ps = %q，want 100", got)
		}
		if got := r.URL.Query().Get("pn"); got != "2" {
			t.Errorf("pn = %q，want 2（传 0 要夹到 1）", got)
		}
		if got := r.URL.Query().Get("up_mid"); got != "900" {
			t.Errorf("up_mid = %q，want 900", got)
		}
		// ⚠️ rid 必须显式给 0。不给的话 B 站按自己的记忆补一个值，会把一部分
		// 收藏夹悄悄藏起来，而界面上看不出任何异样。
		if got := r.URL.Query().Get("rid"); got != "77" {
			t.Errorf("rid = %q，want 77", got)
		}
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"count": 2, "has_more": false,
			"list": []map[string]any{
				{"id": 1, "title": "默认收藏夹", "attr": 0, "media_count": 3, "fav_state": 1,
					"cover": "http://i0.hdslb.com/bfs/album/one.jpg"},
				{"id": 2, "title": "私密的那个", "attr": 3, "media_count": 0, "fav_state": 0,
					"cover": "http://i0.hdslb.com/bfs/album/two.jpg"},
			},
		}))
	})

	got, hasMore, err := f.client(t, Options{}).CreatedFolders(context.Background(), 900, 77, 2)
	if err != nil {
		t.Fatal(err)
	}
	if hasMore {
		t.Error("has_more = true，want false")
	}
	// ⚠️ attr 第 0 位是私密、第 1 位是「**不是**默认收藏夹」（B 站存的是取反
	// 过的那个意思）。所以 attr=0 是「公开、而且是默认收藏夹」，attr=3 才是
	// 「私密、不是默认」。当枚举读的话「默认收藏夹」两个字整个读反，而默认
	// 收藏夹是绝大多数人的第一个。
	//
	// ⚠️ **封面也在这儿**（票 #270）：它是这个接口本来就给的一格，侧栏那一行
	// 要画封面，而每行另发一次请求就是 N 次网络往返。
	want := []CreatedFolder{
		{ID: 1, Title: "默认收藏夹", Cover: "http://i0.hdslb.com/bfs/album/one.jpg",
			Private: false, Default: true, MediaCount: 3, Faved: true},
		{ID: 2, Title: "私密的那个", Cover: "http://i0.hdslb.com/bfs/album/two.jpg",
			Private: true, Default: false, MediaCount: 0, Faved: false},
	}
	if len(got) != len(want) {
		t.Fatalf("拿到 %d 个收藏夹，want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 个 = %+v，want %+v", i, got[i], want[i])
		}
	}
}

// TestCreatedFoldersRefusesNobody 收藏夹主人 id 不对就报错，不发请求。
//
// 0 号用户的收藏夹列表 B 站会回一堆别人的东西或者干脆 -400，而两种结果在
// 界面上都表现为「收藏夹选择弹窗是空的」。
func TestCreatedFoldersRefusesNobody(t *testing.T) {
	f := newFakeBili(t)
	if _, _, err := f.client(t, Options{}).CreatedFolders(context.Background(), 0, 1, 1); err == nil {
		t.Fatal("up_mid=0 也发出去了，应该报错")
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("发出了 %d 个请求，want 0", n)
	}
}

// ── 新建 / 修改 ────────────────────────────────────────────────────────────

// TestCreateFolderSendsThePrivacyTheOtherWayRound 新建时私密是 1、公开是 0。
//
// ⚠️ **这是整张票里最容易写反的一个值**。界面上是「公开 / 私密」两个说法，
// B 站的 `privacy` 是 0 公开 1 私密——而隐私在它的 `attr` 里又是「第 0 位是
// 私密」。三样东西两个方向，写反了不会报错，用户建出来的是私密收藏夹而他
// 明明选了公开。
func TestCreateFolderSendsThePrivacyTheOtherWayRound(t *testing.T) {
	for _, tc := range []struct {
		name   string
		public bool
		want   string
	}{
		{"公开", true, "0"},
		{"私密", false, "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeBili(t)
			f.on(pathFolderAdd, func(w http.ResponseWriter, r *http.Request) {
				if got := r.PostForm.Get("privacy"); got != tc.want {
					t.Errorf("privacy = %q，want %q", got, tc.want)
				}
				if got := r.PostForm.Get("title"); got != "通勤路上" {
					t.Errorf("title = %q", got)
				}
				writeJSON(w, jsonData(t, 0, "0", map[string]any{"id": 555, "title": "通勤路上"}))
			})

			got, err := f.client(t, Options{}).CreateFolder(context.Background(), FolderDraft{
				Title: " 通勤路上 ", Privacy: boolToPrivacy(tc.public),
			})
			if err != nil {
				t.Fatal(err)
			}
			if got.ID != 555 {
				t.Errorf("新收藏夹 id = %d，want 555", got.ID)
			}
		})
	}
}

// boolToPrivacy 是界面上那个「是不是公开」到 B 站 privacy 的映射。
func boolToPrivacy(public bool) int {
	if public {
		return 0
	}
	return 1
}

// TestCreateFolderNeedsAName 没起名字就不发请求。
//
// B 站那边带一个空标题建出来的是一个没名字的收藏夹，而用户界面上看到的输入框
// 明明是空的（他没打字），于是会以为是自己手滑删掉了字。
func TestCreateFolderNeedsAName(t *testing.T) {
	f := newFakeBili(t)
	c := f.client(t, Options{})
	if _, err := c.CreateFolder(context.Background(), FolderDraft{Title: "   "}); err == nil {
		t.Error("空标题也发出去了")
	}
	if _, err := c.EditFolder(context.Background(), 1, FolderDraft{Title: ""}); err == nil {
		t.Error("空标题的修改也发出去了")
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("发出了 %d 个请求，want 0", n)
	}
}

// TestEditFolderCarriesTheMediaID 改哪一个收藏夹靠 media_id。
func TestEditFolderCarriesTheMediaID(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathFolderEdit, func(w http.ResponseWriter, r *http.Request) {
		if got := r.PostForm.Get("media_id"); got != "12345" {
			t.Errorf("media_id = %q，want 12345", got)
		}
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"id": 12345, "cover": "https://i0/1.jpg"}))
	})
	got, err := f.client(t, Options{}).EditFolder(context.Background(), 12345, FolderDraft{
		Title: "新名字", Intro: "新的简介", Privacy: 1, Cover: "https://i0/1.jpg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Cover != "https://i0/1.jpg" {
		t.Errorf("回读的封面 = %q", got.Cover)
	}
}

// TestEmptyFieldsAreNotSent 空字段不发过去。
//
// ⚠️ **空 cover 的意思是「不换封面」**，不是「把封面清空」：Biu 那边靠
// `omitBy` 去掉空值，而我们去掉之后 B 站保留原来的那一张。反过来如果把
// `cover=` 也发出去，B 站存的是一个空串——用户明明没动封面，回来发现封面没了。
func TestEmptyFieldsAreNotSent(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathFolderEdit, func(w http.ResponseWriter, r *http.Request) {
		for _, k := range []string{"cover", "intro"} {
			if _, has := r.PostForm[k]; has {
				t.Errorf("%q 被发出去了（值 %q），空值不该发", k, r.PostForm.Get(k))
			}
		}
		// privacy 是 "0"，**非空**：它是 0 不是「没有」，去掉就变成默认公开了。
		if _, has := r.PostForm["privacy"]; !has {
			t.Error("privacy 没发过去")
		}
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"id": 1}))
	})
	if _, err := f.client(t, Options{}).EditFolder(context.Background(), 1, FolderDraft{
		Title: "只有名字", Privacy: 0,
	}); err != nil {
		t.Fatal(err)
	}
}

// TestWriteCallsCarryTheCSRFInThreePlaces 写接口的 csrf 三处都要有。
//
// 查询串、bili-jct 头、表单体。少任何一处 B 站都回 -111，而 -111 在真机上
// 有一半的概率被报成「风控」（见 client.go 的 ErrCSRF）。
func TestWriteCallsCarryTheCSRFInThreePlaces(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathFolderAdd, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("csrf"); got != "tok" {
			t.Errorf("查询串的 csrf = %q", got)
		}
		if got := r.PostForm.Get("csrf"); got != "tok" {
			t.Errorf("表单体的 csrf = %q", got)
		}
		if got := r.Header.Get(CSRFHeaderName); got != "tok" {
			t.Errorf("%s 头 = %q", CSRFHeaderName, got)
		}
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"id": 1}))
	})
	c := f.client(t, Options{CSRF: func() string { return "tok" }})
	if _, err := c.CreateFolder(context.Background(), FolderDraft{Title: "x"}); err != nil {
		t.Fatal(err)
	}
}

// ── 封面 ───────────────────────────────────────────────────────────────────

// TestUploadCoverGoesToTheMemberHost 封面上传打的是 member 那台。
//
// ⚠️ 别的接口都在 `api.bilibili.com` 上，只有这一个在 `member.bilibili.com`
// （Biu 给它单开了一个 `memberRequest`）。打到 api 上去会得到 -404，而 -404
// 的文案是「啥都没有」，排查时完全指不到真正的原因。
func TestUploadCoverGoesToTheMemberHost(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathCoverUpload, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.PostForm.Get("cover"), "data:image/jpeg;base64,") {
			t.Errorf("cover = %q", r.PostForm.Get("cover"))
		}
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"url": "https://i0.hdslb.com/c.jpg"}))
	})
	got, err := f.client(t, Options{}).UploadCover(context.Background(), "data:image/jpeg;base64,AAAA")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://i0.hdslb.com/c.jpg" {
		t.Errorf("回读的封面地址 = %q", got)
	}
}

// TestUploadCoverNeedsADataURL 光给 raw base64 要被挡住。
//
// B 站是从 `data:image/…;base64,` 那个前缀里认格式的，缺了它整张图会被当成
// 一串没意义的字符，而返回是「成功」加一个打不开的地址。
func TestUploadCoverNeedsADataURL(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathCoverUpload, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"url": "https://i0/c.jpg"}))
	})
	c := f.client(t, Options{})
	if _, err := c.UploadCover(context.Background(), "/9j/4AAQSkZJRgABA"); err == nil {
		t.Error("raw base64 也发出去了")
	}
	if _, err := c.UploadCover(context.Background(), "data:image/jpeg;base64,"); err != nil {
		t.Errorf("光有前缀的应该放行：%v", err)
	}
	if n := len(f.requests()); n != 1 {
		t.Errorf("发出了 %d 个请求，want 1（只有那一次该拦的没出去）", n)
	}
}

// ── 往里放东西 ──────────────────────────────────────────────────────────────

// TestDealFolderMediasSendsBothLists 放和拿分成两个逗号串。
func TestDealFolderMediasSendsBothLists(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathResourceDeal, func(w http.ResponseWriter, r *http.Request) {
		if got := r.PostForm.Get("add_media_ids"); got != "1,2" {
			t.Errorf("add_media_ids = %q，want 1,2", got)
		}
		if got := r.PostForm.Get("del_media_ids"); got != "9" {
			t.Errorf("del_media_ids = %q，want 9", got)
		}
		if got := r.PostForm.Get("type"); got != "12" {
			t.Errorf("type = %q，want 12", got)
		}
		// 这四个是 B 站网页端自己带的，少给不报错也不生效（见 folder.go）。
		for _, k := range []string{"platform", "ga", "gaia_source"} {
			if r.PostForm.Get(k) == "" {
				t.Errorf("%q 没发过去", k)
			}
		}
		writeJSON(w, jsonData(t, 0, "0", nil))
	})
	err := f.client(t, Options{}).DealFolderMedias(context.Background(), 77, MediaAudio, []int64{1, 2}, []int64{9})
	if err != nil {
		t.Fatal(err)
	}
}

// TestDealWithNothingIsANoOp 两个都是空就别发。
//
// 「原来就没勾、现在还是没勾」是最常见的点法，发一个空请求过去 B 站回 -400，
// 界面上是一句红字，而用户其实什么都没改。
func TestDealWithNothingIsANoOp(t *testing.T) {
	f := newFakeBili(t)
	c := f.client(t, Options{})
	if err := c.DealFolderMedias(context.Background(), 77, MediaVideo, nil, nil); err != nil {
		t.Fatal(err)
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("发出了 %d 个请求，want 0", n)
	}
}

// TestDeleteFolderSendsTheMediaIDsName 删收藏夹的参数名是 `media_ids`。
//
// ⚠️ 抄成 `media_id` 的话 B 站回 -400「参数错误」——它不认那个名字，而一个
// 只把收藏夹名字写错的事故症状是「点了删除，它说参数错误」。
func TestDeleteFolderSendsTheMediaIDsName(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathFolderDel, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", 0))
	})
	err := f.client(t, Options{CSRF: func() string { return "jct" }}).
		DeleteFolder(context.Background(), 1182306172)
	if err != nil {
		t.Fatal(err)
	}

	reqs := f.requests()
	if len(reqs) != 1 || reqs[0].Path != pathFolderDel {
		t.Fatalf("请求是 %+v，want 一次 %s", reqs, pathFolderDel)
	}
	if got := reqs[0].Form.Get("media_ids"); got != "1182306172" {
		t.Errorf("media_ids = %q，want 1182306172", got)
	}
	if got := reqs[0].Form.Get("media_id"); got != "" {
		t.Errorf("发了 media_id = %q，B 站不认这个名字", got)
	}
	if got := reqs[0].Form.Get("csrf"); got != "jct" {
		t.Errorf("csrf = %q，want jct", got)
	}
}

// TestDeleteFolderNeedsAnID 空的收藏夹 id 不该发请求。
func TestDeleteFolderNeedsAnID(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathFolderDel, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", 0))
	})
	if err := f.client(t, Options{}).DeleteFolder(context.Background(), 0); err == nil {
		t.Fatal("空 id 也当成功")
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("空 id 还是发了 %d 次请求", n)
	}
}
