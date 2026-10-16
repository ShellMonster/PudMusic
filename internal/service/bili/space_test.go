// UP 空间那几个接口的测试。
//
// 和包里别处的 fixture 一样：**没有一个断言是在证明「B 站真的这么返回」**。
// 它证明的是协议形状（哪个路径、哪个参数、要不要签名）和失败形态的行为。
// 真 B 站的兼容性只能靠真机回归。
package bili

import (
	"context"
	"net/http"
	"testing"
)

// spaceClient 建一台会回 WBI key 的假 B 站，顺手把宿主指到它上面。
func spaceClient(t *testing.T) *fakeBili {
	t.Helper()
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"wbi_img": map[string]string{
				"img_url": "https://i0.hdslb.com/bfs/wbi/" + sampleImgKey + ".png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/" + sampleSubKey + ".png",
			},
		}))
	})
	return f
}

// ─────────────────────────────────────────────────────────────────────────────
// 关系值
// ─────────────────────────────────────────────────────────────────────────────

// TestRelationFollowedCoversBothFollowedAndMutual 是「已关注是**两个**值」
// 那一条的钉子。
//
// 判据不是「函数返回了什么」，是「互粉的 UP 主身上那个按钮写的是什么」——
// `Followed` 是那个按钮的唯一依据。
func TestRelationFollowedCoversBothFollowedAndMutual(t *testing.T) {
	cases := []struct {
		rel      Relation
		followed bool
		blocked  bool
		why      string
	}{
		{RelationNone, false, false, "未关注"},
		{RelationQuietFollowed, false, false, "悄悄关注（B 站已弃用）不是「已关注」"},
		{RelationFollowed, true, false, "单向已关注"},
		{RelationMutual, true, false, "互相关注 —— 少了这一格按钮永远回不来"},
		{RelationBlocked, false, true, "拉黑不是「已关注」"},
	}
	for _, c := range cases {
		if got := c.rel.Followed(); got != c.followed {
			t.Errorf("%s（%d）：Followed() 是 %v，want %v", c.why, int(c.rel), got, c.followed)
		}
		if got := c.rel.Blocked(); got != c.blocked {
			t.Errorf("%s（%d）：Blocked() 是 %v，want %v", c.why, int(c.rel), got, c.blocked)
		}
	}
}

// TestRelationMutualWouldBreakTheButtonIfLeftOut 是上面那条的反证：把互粉
// 从 Followed 里拿掉，按钮在互粉的 UP 主身上会一直写着「关注」。
func TestRelationMutualWouldBreakTheButtonIfLeftOut(t *testing.T) {
	// Biu 上游写的是 `[UserRelation.Followed, UserRelation.MutualFollowed].includes(...)`
	// （space-info.tsx:33）。这里把上游那两个值抄过来对照：抄错一位就是红的。
	biuFollows := func(a int) bool { return a == int(RelationFollowed) || a == int(RelationMutual) }
	for _, attr := range []int{0, 1, 2, 6, 128} {
		if got, want := Relation(attr).Followed(), biuFollows(attr); got != want {
			t.Errorf("attribute=%d：Followed() 是 %v，上游那一份是 %v", attr, got, want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 头部
// ─────────────────────────────────────────────────────────────────────────────

// TestSpaceAccReadsTheBannerTheNameAndTheLevel
func TestSpaceAccReadsTheBannerTheNameAndTheLevel(t *testing.T) {
	f := spaceClient(t)
	f.on(spaceAccPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"mid": 12345, "name": "某个 UP 主", "sign": "签名", "face": "https://f/face.jpg",
			"top_photo":    "https://f/orig.jpg",
			"top_photo_v2": map[string]string{"l_200h_img": "https://f/200h.jpg", "l_img": "https://f/full.jpg"},
			"level":        5,
			"official":     map[string]any{"role": 0, "title": ""},
		}))
	})

	u, err := f.client(t, Options{}).SpaceAcc(context.Background(), 12345)
	if err != nil {
		t.Fatal(err)
	}
	if u.Mid != 12345 || u.Name != "某个 UP 主" || u.Sign != "签名" {
		t.Errorf("读出来是 %+v", u)
	}
	if u.Level != 5 {
		t.Errorf("等级是 %d，want 5", u.Level)
	}
	// ⚠️ **头图取 200 高那一份，不取原图**：原图动辄好几 MB，而头图区在
	// 屏幕上就是 200 高。拿原图的直接后果是「点进一个 UP 主下好几张图」。
	if u.Banner != "https://f/200h.jpg" {
		t.Errorf("头图是 %q，want 200 高那一份（拿原图就是白下好几 MB）", u.Banner)
	}
	if u.Official != "" {
		t.Errorf("role=0 表示没认证，Official 应该是空串，got %q", u.Official)
	}
}

// TestSpaceAccFallsBackToTheOldBannerField 钉住新旧两个头图字段。
// B 站在 `top_photo_v2` 之前只有 `top_photo`，而这一版照样要能画。
func TestSpaceAccFallsBackToTheOldBannerField(t *testing.T) {
	f := spaceClient(t)
	f.on(spaceAccPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"mid": 7, "name": "老 UP 主", "top_photo": "https://f/old.jpg",
			"official": map[string]any{"role": 0},
		}))
	})
	u, err := f.client(t, Options{}).SpaceAcc(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if u.Banner != "https://f/old.jpg" {
		t.Errorf("没有 top_photo_v2 时头图该退回 top_photo，got %q", u.Banner)
	}
}

// TestSpaceAccHasNoBannerIsNotAnError 钉住「大多数 UP 主没设头图」这一支：
// 那时 B 站给的是**空对象**，界面上要画一块纯色，而不是一句「加载失败」。
func TestSpaceAccHasNoBannerIsNotAnError(t *testing.T) {
	f := spaceClient(t)
	f.on(spaceAccPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"mid": 9, "name": "没有头图"}))
	})
	u, err := f.client(t, Options{}).SpaceAcc(context.Background(), 9)
	if err != nil {
		t.Fatalf("没有头图不该是错：%v", err)
	}
	if u.Banner != "" {
		t.Errorf("没头图时 Banner 应是空串，got %q", u.Banner)
	}
}

// TestSpaceAccIsSigned 钉住「这个接口要 WBI 签名」。
//
// 不带的症状是 -403「访问权限不足」，而那句话读起来像是「你没权限看这个人」
// ——于是人会去查 mid 是不是写错了，而真正的原因是少了一个 w_rid。
func TestSpaceAccIsSigned(t *testing.T) {
	f := spaceClient(t)
	f.on(spaceAccPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"mid": 1, "name": "x"}))
	})
	if _, err := f.client(t, Options{}).SpaceAcc(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	for _, r := range f.requests() {
		if r.Path != spaceAccPath {
			continue
		}
		if r.Query.Get("w_rid") == "" {
			t.Fatalf("空间详情没有签名（query=%v）", r.Query)
		}
		if r.Query.Get("mid") != "1" {
			t.Errorf("mid 传成了 %q", r.Query.Get("mid"))
		}
		return
	}
	t.Fatalf("根本没请求 %s", spaceAccPath)
}

// TestSpaceRelationReadsRelationNotTheReverse 钉住「relation 不是
// be_relation」。
//
// 两个都在返回里，认错的话「我关注了他」会显示成「他没关注我」——而那一格
// 是关注按钮在不在的唯一依据。
func TestSpaceRelationReadsRelationNotTheReverse(t *testing.T) {
	f := spaceClient(t)
	f.on(spaceRelationPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"relation":    map[string]any{"mid": 2, "attribute": 6},
			"be_relation": map[string]any{"mid": 2, "attribute": 0},
		}))
	})
	rel, err := f.client(t, Options{}).SpaceRelation(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if rel != RelationMutual {
		t.Errorf("读成了 %v（%d），want 互关（6）。读成 0 就是认成了 be_relation", rel, int(rel))
	}
}

// TestRelationStatToleratesTheUnloggedInShape 未登录时 B 站只给 follower、
// 不给 following。那两个数**都是 0，不是错**——当成错的话未登录用户根本
// 看不了任何 UP 主。
func TestRelationStatToleratesTheUnloggedInShape(t *testing.T) {
	f := spaceClient(t)
	f.on(relationStatPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"mid": 3, "follower": 12345}))
	})
	st, err := f.client(t, Options{}).RelationStatOf(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if st.Follower != 12345 || st.Following != 0 {
		t.Errorf("读出来是 %+v，want {12345 0}", *st)
	}
}

// TestModifyRelationCarriesTheCSRFAndTheAction 钉住写接口的三样东西：
// POST、csrf 参数、动作码。少一样都是 -111，而 -111 有时候 B 站报的是
// 「风控」，两者混在一起极难查。
func TestModifyRelationCarriesTheCSRFAndTheAction(t *testing.T) {
	f := spaceClient(t)
	// 方法记在这里而不去动夹具的 recordedRequest（那个是几个文件共用的）。
	var method string
	f.on(relationModifyPath, func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		writeJSON(w, jsonData(t, 0, "0", nil))
	})
	cli := f.client(t, Options{CSRF: func() string { return "token123" }})

	if err := cli.ModifyRelation(context.Background(), 42, ActionFollow); err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	last := reqs[len(reqs)-1]
	if method != http.MethodPost {
		t.Errorf("关注用的是 %s，want POST", method)
	}
	if last.Form.Get("fid") != "42" || last.Form.Get("act") != "1" {
		t.Errorf("表单是 %v，want fid=42 act=1", last.Form)
	}
	// CSRF 走的是**查询串**（`Do` 里 `q.Set("csrf", …)`）加上 `bili-jct` 头。
	// 两个都要有：只给头的话 B 站照样回 -111，而 -111 有时候报的是「风控」。
	if last.Query.Get("csrf") != "token123" || last.Header.Get(CSRFHeaderName) != "token123" {
		t.Errorf("csrf 没带上（query=%v header=%q）", last.Query, last.Header.Get(CSRFHeaderName))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 投稿
// ─────────────────────────────────────────────────────────────────────────────

// TestSpaceVideosReadsBothShapesOfDurationAndCid 钉住那两个「两套格式」
// 字段。B 站对同一条投稿时而给 `"3:20"` 时而给 `200`，时而给数字 cid 时而
// 给字符串——只按一种解，另一种就变成一张打不开的卡片。
func TestSpaceVideosReadsBothShapesOfDurationAndCid(t *testing.T) {
	f := spaceClient(t)
	f.on(spaceVideoPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"list": map[string]any{"vlist": []any{
				map[string]any{"bvid": "BV1a", "title": "文字格式", "length": "3:20", "cid": "555",
					"pic": "https://f/a.jpg", "play": 100},
				map[string]any{"bvid": "BV1b", "title": "数字格式", "length": 200, "cid": 666,
					"pic": "https://f/b.jpg", "play": 200},
			}},
			"page": map[string]any{"count": 2},
		}))
	})

	pg, err := f.client(t, Options{}).SpaceVideos(context.Background(), UpVideoQuery{Mid: 1, Page: 1, Size: 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Videos) != 2 {
		t.Fatalf("拿到 %d 条，want 2", len(pg.Videos))
	}
	if pg.Videos[0].Duration != "3:20" || pg.Videos[0].Cid != "555" {
		t.Errorf("文字格式那条读成 duration=%q cid=%q", pg.Videos[0].Duration, pg.Videos[0].Cid)
	}
	if pg.Videos[1].Duration != "200" || pg.Videos[1].Cid != "666" {
		t.Errorf("数字格式那条读成 duration=%q cid=%q", pg.Videos[1].Duration, pg.Videos[1].Cid)
	}
	if pg.HasMore {
		t.Error("两页都拿完了（2/2）却还说有下一页")
	}
}

// TestSpaceVideosStopsAtAnEmptyPage 钉住「空页一律收口」。
//
// B 站在 pn 超出范围时回的是空 vlist 而不是报错。照着「还没拿够 count」
// 继续翻的话，界面上就是永远转圈——而那正是「界面永远转圈」那条铁律
// 要消灭的东西。
func TestSpaceVideosStopsAtAnEmptyPage(t *testing.T) {
	f := spaceClient(t)
	f.on(spaceVideoPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"list": map[string]any{"vlist": nil},
			"page": map[string]any{"count": 999},
		}))
	})
	pg, err := f.client(t, Options{}).SpaceVideos(context.Background(), UpVideoQuery{Mid: 1, Page: 40, Size: 30})
	if err != nil {
		t.Fatal(err)
	}
	if pg.HasMore {
		t.Errorf("B 站给了一页空的却说还有下一页（count=%d），界面会永远转圈", pg.Total)
	}
}

// TestSpaceVideosToleratesANullVList 是上一条**换一种形态**再钉一遍：
// B 站回的是 `null` 而不是 `[]`，按数组解会让整个「投稿」Tab 变成一句
// 「返回读不懂」。
func TestSpaceVideosToleratesANullVList(t *testing.T) {
	f := spaceClient(t)
	f.on(spaceVideoPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"list": map[string]any{"vlist": nil},
			"page": map[string]any{"count": 0},
		}))
	})
	pg, err := f.client(t, Options{}).SpaceVideos(context.Background(), UpVideoQuery{Mid: 1})
	if err != nil {
		t.Fatalf("vlist 是 null 不该是错：%v", err)
	}
	if len(pg.Videos) != 0 || pg.HasMore {
		t.Errorf("空投稿该是空的，got %+v", pg)
	}
}

// TestSpaceVideosDropsEntriesWithoutABvid 没有 bvid 的条目点不开（取流接口、
// 跳转链接都靠它），留着就是一张点了没反应的行。
func TestSpaceVideosDropsEntriesWithoutABvid(t *testing.T) {
	f := spaceClient(t)
	f.on(spaceVideoPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"list": map[string]any{"vlist": []any{
				map[string]any{"title": "没有 bvid 的"},
			}},
			"page": map[string]any{"count": 1},
		}))
	})
	pg, err := f.client(t, Options{}).SpaceVideos(context.Background(), UpVideoQuery{Mid: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Videos) != 0 {
		t.Errorf("没有 bvid 的条目被留下了：%+v", pg.Videos)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 合集与收藏夹
// ─────────────────────────────────────────────────────────────────────────────

// TestSpaceSeriesJoinsBothLists 钉住「合集和系列是**一个接口里的两个列表**」。
//
// 只取其中一个的话，一半的 UP 主在「合集」Tab 上是空的——而界面上看不出
// 是「他没有合集」还是「我们漏了」。它俩的 id 是两个编号空间，type 必须带上。
func TestSpaceSeriesJoinsBothLists(t *testing.T) {
	f := spaceClient(t)
	f.on(spaceSeriesPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"items_lists": map[string]any{
				"seasons_list": []any{map[string]any{"meta": map[string]any{
					"season_id": 11, "name": "一个合集", "cover": "https://f/s.jpg", "total": 7,
				}}},
				"series_list": []any{map[string]any{"meta": map[string]any{
					"series_id": 22, "name": "一个系列", "cover": "https://f/r.jpg", "total": 3,
				}}},
				"page": map[string]any{"total": 2},
			},
		}))
	})

	pg, err := f.client(t, Options{}).SpaceSeries(context.Background(), 5, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Collections) != 2 {
		t.Fatalf("拿到 %d 个，want 2（合集 + 系列都在这一个接口里）", len(pg.Collections))
	}
	// ⚠️ **两个 id 都是 11 和 22 时不带 type 会撞**：收藏夹和合集是 B 站两个
	// 不同的编号空间，各自从 1 开始数。行身份见 ui/upspace_tabs.go 的 upRowKey。
	if pg.Collections[0].Type == pg.Collections[1].Type {
		t.Errorf("合集和系列的 type 一样（%d），行身份会撞在一起", pg.Collections[0].Type)
	}
	if pg.HasMore {
		t.Error("2/2 都拿完了却还说有下一页")
	}
}

// TestSpaceFavFoldersIsOneShot 钉住「收藏夹一次给全，没有翻页参数」。
//
// 返回里没有 HasMore 这一栏就是这一层的约定：调用方别给它加一页「还在加载」。
func TestSpaceFavFoldersIsOneShot(t *testing.T) {
	f := spaceClient(t)
	f.on(spaceFavPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"list": []any{
				map[string]any{"id": 100, "title": "第一个收藏夹", "media_count": 12},
				map[string]any{"id": 0, "title": "没有 id 的"},
			},
		}))
	})
	cols, err := f.client(t, Options{}).SpaceFavFolders(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 1 {
		t.Fatalf("拿到 %d 个，want 1（id 为 0 的那条被丢掉）", len(cols))
	}
	if cols[0].ID != 100 || cols[0].Title != "第一个收藏夹" || cols[0].Count != 12 {
		t.Errorf("读出来是 %+v", cols[0])
	}
	if cols[0].Type != collectionTypeFav {
		t.Errorf("收藏夹的 type 是 %d，want %d", cols[0].Type, collectionTypeFav)
	}
}

// TestSpaceEndpointsRefuseAnImpossibleMid 传 0 / 负数要去发请求的话，
// B 站会回一个看不懂的错，而真正的原因是参数不对。
func TestSpaceEndpointsRefuseAnImpossibleMid(t *testing.T) {
	c := NewClient(Options{})
	ctx := context.Background()
	if _, err := c.SpaceAcc(ctx, 0); err == nil {
		t.Error("mid=0 不该发出去")
	}
	if _, err := c.SpaceRelation(ctx, -1); err == nil {
		t.Error("mid=-1 不该发出去")
	}
	if _, err := c.RelationStatOf(ctx, 0); err == nil {
		t.Error("vmid=0 不该发出去")
	}
	if _, err := c.SpaceSeries(ctx, 0, 1, 20); err == nil {
		t.Error("mid=0 不该发出去")
	}
	if _, err := c.SpaceFavFolders(ctx, 0); err == nil {
		t.Error("mid=0 不该发出去")
	}
	if _, err := c.SpaceVideos(ctx, UpVideoQuery{Mid: 0}); err == nil {
		t.Error("mid=0 不该发出去")
	}
	if err := c.ModifyRelation(ctx, 0, ActionFollow); err == nil {
		t.Error("fid=0 不该发出去")
	}
}
