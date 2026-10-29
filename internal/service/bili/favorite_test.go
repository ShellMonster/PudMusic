package bili

// 收藏那一层的测试。
//
// **形状来自上游**：`fav-folder-created-list-all.ts`（列表）、`fav-folder-deal.ts`
// （写）、`fav-resource.ts:168` 的 `/x/v3/fav/resource/ids`（这一首在哪些格子里）、
// `web-interface-view.ts`（bvid → aid）。fixture 照那些类型的字段名造。
//
// ⚠️ **没有任何一条断言是在证明「B 站现在真这么返回」**，那要靠真机回归——而收藏是
// **写操作**，会动用户真实的收藏夹，真机验证只能在测试账号自己的夹子里做（见这一票
// 的报告）。

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// favFixture 是一台会收藏的假 B 站。
//
// mid 是它报的登录用户；folders 是「我创建的」收藏夹；already 是「这一首已经在
// 哪些格子里」。
func favFixture(t *testing.T, mid int64, folders []map[string]any, folderContents []map[string]any, aid int64) *fakeBili {
	t.Helper()
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"isLogin": mid != 0,
			"mid":     mid,
			"uname":   "测试账号",
		}))
	})
	f.on(pathView, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"aid": aid, "bvid": r.URL.Query().Get("bvid")}))
	})
	f.on(pathFavFolders, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"count": len(folders), "list": folders}))
	})
	// ⚠️ 收的是**收藏夹 id**、返回的是**那一格里的内容**（上游
	// `fav-resource.ts:127-152`）。第一版的 fixture 照着「给定 rid 问它在哪些格子里」
	// 造，真接口回 -400，收藏在写之前就断了。
	f.on(pathFavMediaIDs, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"ids": folderContents}))
	})
	f.on(pathFavDeal, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", nil))
	})
	return f
}

// favClient 是带登录态和 CSRF 的客户端（收藏是写操作，两样都要）。
func favClient(t *testing.T, f *fakeBili) *Client {
	t.Helper()
	return f.client(t, Options{
		CSRF: func() string { return "the-jct-value" },
		Now:  func() time.Time { return time.Unix(1702204169, 0) },
	})
}

func favFolderItem(id int64, title string, count int) map[string]any {
	return map[string]any{"id": id, "title": title, "media_count": count, "attr": 0}
}

// TestFavoriteSongPicksTheFirstFolderAndWritesIt 断的是「放哪一格」和「写什么」。
//
// 请求的顺序就是实现里的顺序：nav（拿 mid）→ view（bvid 换 aid）→ list-all（挑
// 格子）→ resource/ids（判重复）→ deal（写）。**顺序本身是判据**：少了 mid 那一步就
// 挑不出格子，少了 ids 那一步就会重复收藏。
func TestFavoriteSongPicksTheFirstFolderAndWritesIt(t *testing.T) {
	f := favFixture(t, 9527,
		[]map[string]any{favFolderItem(100, "默认收藏夹", 12), favFolderItem(200, "第二格", 3)},
		nil, 4242)
	c := favClient(t, f)

	folder, err := c.FavoriteSong(context.Background(), FavTarget{Bvid: "BV1xx", Cid: "c1", Type: "mv"})
	if err != nil {
		t.Fatalf("收藏：%v", err)
	}
	if folder.ID != 100 || folder.Title != "默认收藏夹" {
		t.Errorf("收进了 %+v，want 第一格（默认收藏夹）", folder)
	}

	var deal recordedRequest
	for _, r := range f.requests() {
		if r.Path == pathFavDeal {
			deal = r
		}
	}
	if deal.Path == "" {
		t.Fatalf("没有发收藏的写请求：%+v", f.requests())
	}
	if got := deal.Form.Get("rid"); got != "4242" {
		t.Errorf("写请求的 rid 是 %q，want 4242（要的是 avid，不是 bvid）", got)
	}
	if got := deal.Form.Get("type"); got != "2" {
		t.Errorf("写请求的 type 是 %q，want 2（投稿）", got)
	}
	if got := deal.Form.Get("add_media_ids"); got != "100" {
		t.Errorf("放进了哪一格：%q，want 100", got)
	}
	// 写操作必须带 CSRF，两个地方都带（见 TestCSRFIsInjectedOnWriteCalls）。
	if got := deal.Query.Get("csrf"); got != "the-jct-value" {
		t.Errorf("csrf 参数是 %q", got)
	}
	if got := deal.Header.Get(CSRFHeaderName); got != "the-jct-value" {
		t.Errorf("csrf 头是 %q", got)
	}
}

// TestFavoriteSongGoesIntoTheFolderItWasToldTo 界面（将来的收藏夹选择框）指定了
// 哪一格就去哪一格——列都不列。
func TestFavoriteSongGoesIntoTheFolderItWasToldTo(t *testing.T) {
	f := favFixture(t, 9527,
		[]map[string]any{favFolderItem(100, "默认收藏夹", 0), favFolderItem(200, "我选的那格", 0)},
		nil, 4242)
	c := favClient(t, f)

	folder, err := c.FavoriteSong(context.Background(), FavTarget{Bvid: "BV1xx", Type: "mv", FolderID: 200})
	if err != nil {
		t.Fatalf("收藏：%v", err)
	}
	if folder.ID != 200 {
		t.Errorf("收进了 %+v，want 第 200 格", folder)
	}
	for _, r := range f.requests() {
		if r.Path == pathFavFolders {
			t.Error("指定了格子还在列收藏夹：多一次网络往返")
		}
	}
}

// TestFavoriteSongSaysSoWhenItIsAlreadyThere 去重：已经在那一格里就别再写。
//
// 判据是**没有发写请求**——B 站的 deal 重复放是幂等的（不会多出一条），所以光看
// 「成没成功」分辨不出来，而用户看到的是两句话。
func TestFavoriteSongSaysSoWhenItIsAlreadyThere(t *testing.T) {
	f := favFixture(t, 9527,
		[]map[string]any{favFolderItem(100, "默认收藏夹", 1), favFolderItem(200, "另一格", 0)},
		[]map[string]any{{"id": 4242, "type": 2}}, 4242)
	c := favClient(t, f)

	_, err := c.FavoriteSong(context.Background(), FavTarget{Bvid: "BV1xx", Type: "mv"})
	if !errors.Is(err, ErrAlreadyFavorited) {
		t.Fatalf("已经收藏过了，回的是 %v，want ErrAlreadyFavorited", err)
	}
	for _, r := range f.requests() {
		if r.Path == pathFavDeal {
			t.Error("已经收藏过了还发了一次写请求")
		}
	}
}

// TestFavoriteSongNeedsALoginAndAFolder 没登录 / 一个夹子都没有，各回各的哨兵。
func TestFavoriteSongNeedsALoginAndAFolder(t *testing.T) {
	t.Run("没登录", func(t *testing.T) {
		f := favFixture(t, 0, []map[string]any{favFolderItem(100, "x", 0)}, nil, 4242)
		_, err := favClient(t, f).FavoriteSong(context.Background(), FavTarget{Bvid: "BV1xx", Type: "mv"})
		if !errors.Is(err, ErrNotLoggedIn) {
			t.Fatalf("没登录回的是 %v，want ErrNotLoggedIn", err)
		}
	})
	t.Run("没有收藏夹", func(t *testing.T) {
		f := favFixture(t, 9527, nil, nil, 4242)
		_, err := favClient(t, f).FavoriteSong(context.Background(), FavTarget{Bvid: "BV1xx", Type: "mv"})
		if !errors.Is(err, ErrNoFavoriteFolder) {
			t.Fatalf("没有收藏夹回的是 %v，want ErrNoFavoriteFolder", err)
		}
	})
	t.Run("没有稿件 id", func(t *testing.T) {
		f := favFixture(t, 9527, []map[string]any{favFolderItem(100, "x", 0)}, nil, 4242)
		_, err := favClient(t, f).FavoriteSong(context.Background(), FavTarget{Type: "mv"})
		if !errors.Is(err, ErrNoVideo) {
			t.Fatalf("没有 bvid 回的是 %v，want ErrNoVideo", err)
		}
	})
	t.Run("缺音频 id", func(t *testing.T) {
		f := favFixture(t, 9527, []map[string]any{favFolderItem(100, "x", 0)}, nil, 4242)
		_, err := favClient(t, f).FavoriteSong(context.Background(), FavTarget{Type: "audio"})
		if !errors.Is(err, ErrNoAudio) {
			t.Fatalf("音乐库回的是 %v，want ErrNoAudio", err)
		}
	})
}

// TestFavFoldersNeedsAMidAndRefusesEmpty 没有 mid 就是没登录；一格都没有要回
// ErrNoFavoriteFolder 而不是空列表——调用方分不出「没有」和「问失败了」。
func TestFavFoldersNeedsAMidAndRefusesEmpty(t *testing.T) {
	f := newFakeBili(t)
	c := f.client(t, Options{})
	if _, err := c.FavFolders(context.Background(), 0); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("mid 是 0 时回的是 %v，want ErrNotLoggedIn", err)
	}
	f.on(pathFavFolders, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"count": 0, "list": []any{}}))
	})
	if _, err := c.FavFolders(context.Background(), 9527); !errors.Is(err, ErrNoFavoriteFolder) {
		t.Errorf("空列表回的是 %v，want ErrNoFavoriteFolder", err)
	}
}

// TestFavFoldersAsksForThatUsersFolders up_mid 得是**这个用户的** mid：问错人就是
// 别人的收藏夹，而收藏是写操作。
func TestFavFoldersAsksForThatUsersFolders(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathFavFolders, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"list": []any{favFolderItem(100, "默认收藏夹", 0)}}))
	})
	folders, err := f.client(t, Options{}).FavFolders(context.Background(), 9527)
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 1 || folders[0].ID != 100 {
		t.Fatalf("列出来的是 %+v", folders)
	}
	var got string
	for _, r := range f.requests() {
		if r.Path == pathFavFolders {
			got = r.Query.Get("up_mid")
		}
	}
	if got != strconv.FormatInt(9527, 10) {
		t.Errorf("请求里的 up_mid 是 %q，want 9527", got)
	}
}

// TestFavFolderIDsAsksWithTheFolderID 钉住这一问的**方向**：收的是**收藏夹 id**。
//
// 写反了（给定稿件 id、type，问「它在哪些格子里」）真接口回 `-400 请求错误`，收藏
// 在写之前就断了——而照着错合同造的 fixture 是全绿的。
func TestFavFolderIDsAsksWithTheFolderID(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathFavMediaIDs, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("media_id"); got == "" {
			t.Errorf("请求里没有 media_id：%v", r.URL.Query())
		}
		if r.URL.Query().Get("rid") != "" || r.URL.Query().Get("type") != "" {
			t.Errorf("请求里带了 rid/type，那不是这个接口的参数：%v", r.URL.Query())
		}
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"ids": []any{
			map[string]any{"id": 111, "type": FavTypeVideo, "bvid": "BVaaa", "bv_id": "BVaaa"},
			map[string]any{"id": 4242, "type": FavTypeVideo, "bvid": "BV1xx", "bv_id": "BV1xx"},
		}}))
	})
	ids, err := f.client(t, Options{}).FavFolderIDs(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if !ids[4242] || ids[111] != true {
		t.Fatalf("问出来的是 %v", ids)
	}
	if ids[999] {
		t.Error("多了一个没给过的 id")
	}
	// media_id 没有就不问（省一次往返，也不该问出一个空集合当「这一格是空的」）。
	f2 := newFakeBili(t)
	f2.on(pathFavMediaIDs, func(w http.ResponseWriter, r *http.Request) {
		t.Error("没有收藏夹 id 还去问了")
	})
	if ids, err := f2.client(t, Options{}).FavFolderIDs(context.Background(), 0); err != nil || ids != nil {
		t.Errorf("空 id 的结果是 %v / %v，want nil / nil", ids, err)
	}
}

// TestFavoriteSongStillWritesWhenTheDedupeCallFails 去重那一问是**只读**的，它失败
// 了不该让用户收藏不成：B 站的 deal 重复放是幂等的，写下去结果一样。
func TestFavoriteSongStillWritesWhenTheDedupeCallFails(t *testing.T) {
	f := favFixture(t, 9527, []map[string]any{favFolderItem(100, "默认收藏夹", 0)}, nil, 4242)
	f.on(pathFavMediaIDs, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, -400, "请求错误", nil))
	})
	if _, err := favClient(t, f).FavoriteSong(context.Background(), FavTarget{Bvid: "BV1xx", Type: "mv"}); err != nil {
		t.Fatalf("去重那一问失败就不收藏了：%v", err)
	}
	wrote := false
	for _, r := range f.requests() {
		if r.Path == pathFavDeal {
			wrote = true
		}
	}
	if !wrote {
		t.Error("没有发写请求")
	}
}

// TestArchiveIDAsksByBvid bvid → aid 是收藏接口的前提（见 favorite.go 的文件头）。
func TestArchiveIDAsksByBvid(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathView, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"aid": 4242, "bvid": "BV1xx"}))
	})
	aid, err := f.client(t, Options{}).ArchiveID(context.Background(), "BV1xx")
	if err != nil {
		t.Fatal(err)
	}
	if aid != 4242 {
		t.Errorf("aid 是 %d，want 4242", aid)
	}
	for _, r := range f.requests() {
		if r.Path == pathView && r.Query.Get("bvid") != "BV1xx" {
			t.Errorf("请求里的 bvid 是 %q", r.Query.Get("bvid"))
		}
	}
	// B 站说没有这个稿件（aid 是 0）要回 ErrNoVideo，别当成「aid 是 0」。
	f.on(pathView, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"aid": 0}))
	})
	if _, err := f.client(t, Options{}).ArchiveID(context.Background(), "没了"); !errors.Is(err, ErrNoVideo) {
		t.Errorf("aid 是 0 时回的是 %v，want ErrNoVideo", err)
	}
	if _, err := f.client(t, Options{}).ArchiveID(context.Background(), ""); !errors.Is(err, ErrNoVideo) {
		t.Errorf("空 bvid 回的是 %v，want ErrNoVideo", err)
	}
}

// TestArchiveStatsReadsTheReplyCount 钉住「一趟往返同时拿到 aid 和评论数」。
//
// 🔴 **为什么要把 `stat.reply` 一起读回来**：播放条上那枚「评论」键要显示评论数
// （票 #682），而这一趟（bvid → aid）本来就要发——顺带读一栏不花第二个往返。
//
// ⚠️ **`stat` 缺失 / `reply` 是 0 都是「没有这个数」，不是「零评论」**：B 站没给
// 那一栏时我们没资格说「这一首没人评论」。类型上两者都是 0（界面那一层的判据
// 也是「0 就不画数字」），所以这里只钉「不报错、回 0」。
func TestArchiveStatsReadsTheReplyCount(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathView, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"aid": 4242, "bvid": "BV1xx",
			"stat": map[string]any{"view": 100, "danmaku": 3, "reply": 1286, "like": 9},
		}))
	})
	st, err := f.client(t, Options{}).ArchiveStats(context.Background(), "BV1xx")
	if err != nil {
		t.Fatal(err)
	}
	if st.Aid != 4242 {
		t.Errorf("Aid 是 %d，want 4242", st.Aid)
	}
	if st.Reply != 1286 {
		t.Errorf("Reply 是 %d，want 1286——`stat.reply` 就是评论数", st.Reply)
	}

	// ⚠️ **`stat` 整段缺失**：回包里没有那一栏时不许报错，Reply 是 0。
	f.on(pathView, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"aid": 7}))
	})
	st, err = f.client(t, Options{}).ArchiveStats(context.Background(), "BV1noStat")
	if err != nil {
		t.Fatalf("stat 缺失时报错了：%v", err)
	}
	if st.Aid != 7 || st.Reply != 0 {
		t.Errorf("stat 缺失时拿到 %+v，want Aid=7 Reply=0", st)
	}

	// ⚠️ **bvid 空时当场返回，不发请求**（和 `ArchiveID` 的既有行为一字不差）。
	before := len(f.requests())
	if _, err := f.client(t, Options{}).ArchiveStats(context.Background(), ""); !errors.Is(err, ErrNoVideo) {
		t.Errorf("空 bvid 回的是 %v，want ErrNoVideo", err)
	}
	if after := len(f.requests()); after != before {
		t.Errorf("空 bvid 时发了 %d 趟请求——它该当场返回", after-before)
	}

	// aid 是 0 时是「没有这个稿件」，不是「aid 是 0」。
	f.on(pathView, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"aid": 0, "stat": map[string]any{"reply": 5}}))
	})
	if _, err := f.client(t, Options{}).ArchiveStats(context.Background(), "没了"); !errors.Is(err, ErrNoVideo) {
		t.Errorf("aid 是 0 时回的是 %v，want ErrNoVideo", err)
	}
}

// TestFriendlyTalksAboutFavoriteFailures 收藏的那两个哨兵要能被说成一句用户听得懂
// 的话。**不是所有收藏失败都值得重试**：「已经收藏过了」再按一次还是那句话，而
// 「收藏夹接口没登录」重试多少次都一样，用户该做的是重新登录。
func TestFriendlyTalksAboutFavoriteFailures(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{ErrNoFavoriteFolder, "收藏夹"},
		{ErrAlreadyFavorited, "已经收藏"},
	} {
		got := Friendly(c.err)
		if got == "" || !strings.Contains(got, c.want) {
			t.Errorf("Friendly(%v) = %q，want 一句里带 %q 的话", c.err, got, c.want)
		}
	}
}
