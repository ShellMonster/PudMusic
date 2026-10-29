package bili

// 「我的关注」那一页那四个接口的这一组测试盯四件事：
//
//  1. **发出去的参数是上游那些数**（vmid / pn / ps=20 / name / tagid）。少一个的
//     症状是「页大小不对」或者「搜出来的是别的那个人的关注」——而那在界面上和
//     「B 站没给他关注」长得一模一样。
//  2. **两个调用同一个 `/x/relation/followings` 的地方，参数不一样**：作者栏要
//     `order_type=attention`，这一页不给（按关注顺序）。给错了不报错，只是名次
//     变了，没有任何症状能指出来。
//  3. **`/x/relation/tag` 的 `data` 是一个数组**，不是 `{"list": […]}`。照后者
//     解的话 `json.Unmarshal` 报的是一句「类型不匹配」，和「B 站改了形状」长得
//     一模一样。
//  4. **空页就是到底了**：B 站在页码超了的时候给的是空 list 而不是报错，按
//     「满不满」去推「还有没有」的话这一页会永远转圈。
//
// ⚠️ 这里**没有一个断言是在证明「B 站真的这么返回」**——fixture 是我们自己造的
// （见 client_test.go 顶上那一段）。真网对不对得上只能靠真机回归，见任务报告里
// 的「需要真网验，原因是 X」。

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// followUserFixture 造一个 B 站给的用户（字段名照上游那两个 TS 文件抄）。
//
// ⚠️ 头像故意给**协议相对**地址（`//i0.hdslb.com/…`）：那是 B 站在这一族接口上
// 真会给的形态，而它原样透出去的话界面上取图会失败（`search.go` 的
// normalizeImageURL 那一段记着同一个坑）。
//
// ⚠️ 这一层**只解不取**——它发请求打的是 `newFakeBili` 那台本地假服务器，头像
// 地址在这里只是一个**字符串**，从头到尾没有人去取它。所以这里写真的图床地址
// 是安全的（界面层那个 fixture 不然，见 `internal/ui/follow_test.go` 的
// `uiFollowUser`）。
func followUserFixture(mid int64, name string) map[string]any {
	return map[string]any{
		"mid": mid, "uname": name,
		"face": "//i0.hdslb.com/bfs/face/member-" + strconv.FormatInt(mid, 10) + ".jpg",
		"sign": name + " 的签名",
		"official_verify": map[string]any{
			"type": 0, "desc": name + " 的认证",
		},
	}
}

// onFollowList 给 `/x/relation/followings` 装一份返回。
func onFollowList(t *testing.T, f *fakeBili, total int, pages map[string][]any) {
	t.Helper()
	f.on(followingsPath, func(w http.ResponseWriter, r *http.Request) {
		pn := r.URL.Query().Get("pn")
		// 超过 total 之后 B 站给的是**空 list**（而不是报错），照它那样回：
		// 不这么造的话「还有没有」算错了在测试里看不出来。
		if pages[pn] == nil {
			pn = ""
		}
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"total": total, "list": pages[pn],
		}))
	})
}

// TestFollowTagsReadsTheTagList 盯分组那一列的解析。
func TestFollowTagsReadsTheTagList(t *testing.T) {
	f := newFakeBili(t)
	f.on(relationTagsPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", []any{
			map[string]any{"tagid": -10, "name": "特别关注", "count": 2, "tip": ""},
			map[string]any{"tagid": 0, "name": "默认分组", "count": 30, "tip": ""},
			map[string]any{"tagid": 12345, "name": "常听的", "count": 7, "tip": "自己建的"},
		}))
	})

	tags, err := f.client(t, Options{}).FollowTags(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 3 {
		t.Fatalf("读到 %d 个分组，want 3：%+v", len(tags), tags)
	}
	if tags[0].ID != -10 || tags[0].Name != "特别关注" {
		// ⚠️ **-10 和 0 是 B 站固定给的两个**，不是用户建的。少认一个的症状是
		// 「特别关注」那一格不见了，而它在网页版上明明有。
		t.Errorf("第一个分组读成了 %+v，want tagid=-10 特别关注", tags[0])
	}
	if tags[2].ID != 12345 || tags[2].Count != 7 || tags[2].Tip != "自己建的" {
		t.Errorf("用户自建的那个分组读成了 %+v", tags[2])
	}
}

// TestFollowTagsWithNoGroupsIsNotAnError 盯「一个分组都没有」是正常状态。
//
// 那是一个刚注册、一个关注都没点的账号。B 站在这一档返回**空数组 + code 0**，
// 而把它当成错误的话界面上是「拉不到分组」——用户会以为网络坏了。
func TestFollowTagsWithNoGroupsIsNotAnError(t *testing.T) {
	f := newFakeBili(t)
	f.on(relationTagsPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", []any{}))
	})
	tags, err := f.client(t, Options{}).FollowTags(context.Background())
	if err != nil {
		t.Fatalf("一个分组都没有不该是错误：%v", err)
	}
	if len(tags) != 0 {
		t.Errorf("读出了 %d 个分组：%+v", len(tags), tags)
	}
}

// TestTheFollowPageSendsItsOwnParams 盯「全部关注」那一格发出去的参数。
func TestTheFollowPageSendsItsOwnParams(t *testing.T) {
	f := newFakeBili(t)
	onFollowList(t, f, 5, map[string][]any{
		"1": {followUserFixture(11, "第一个 UP"), followUserFixture(12, "第二个 UP")},
		"2": {followUserFixture(13, "第二页那个")},
	})

	pg, err := f.client(t, Options{}).FollowList(context.Background(), 4242, 2)
	if err != nil {
		t.Fatal(err)
	}

	got := lastRequestTo(t, f, followingsPath)
	for _, q := range []struct{ key, want string }{
		{"vmid", "4242"}, {"pn", "2"}, {"ps", "20"},
	} {
		if v := got.Query.Get(q.key); v != q.want {
			t.Errorf("%s = %q，want %q（查询串：%v）", q.key, v, q.want, got.Query)
		}
	}
	if pg.Total != 5 || len(pg.Users) != 1 {
		t.Errorf("这一页解成了 %+v", pg)
	}
}

// TestTheFollowPageAndTheAuthorPaneSortDifferently 是这一组最重要的一条。
//
// 两个地方打的是**同一个接口**，而参数不一样：
//
//   - 动态流左侧的作者栏要 `order_type=attention`（按最常访问排）。
//   - 这一页的「全部关注」**不给**（按关注顺序，`follow-list/index.tsx:89`
//     就是这么调的）。
//
// 给错了不报错、返回的 list 也照样能用——**症状只是名次变了**，而「名次变了」
// 在界面上和「B 站改算法了」长得一模一样。所以这条把两个调用摆在一起对照。
func TestTheFollowPageAndTheAuthorPaneSortDifferently(t *testing.T) {
	f := newFakeBili(t)
	onFollowList(t, f, 2, map[string][]any{
		"1": {followUserFixture(11, "甲")},
	})
	c := f.client(t, Options{})

	// 作者栏那一列（dynamicfeed.go 的 Followings）。
	if _, err := c.Followings(context.Background(), 4242, 1); err != nil {
		t.Fatal(err)
	}
	if got := lastRequestTo(t, f, followingsPath).Query.Get("order_type"); got != "attention" {
		t.Errorf("作者栏的 order_type = %q，want attention", got)
	}

	// 这一页的「全部关注」。
	if _, err := c.FollowList(context.Background(), 4242, 1); err != nil {
		t.Fatal(err)
	}
	if got := lastRequestTo(t, f, followingsPath).Query.Get("order_type"); got != "" {
		t.Errorf("「全部关注」多发了一个 order_type=%q——上游 index.tsx:89 没发，"+
			"症状只是名次悄悄变了", got)
	}
}

// TestAnEmptyPageStillReportsTheTotal 盯「空页也要报总数」。
//
// B 站在页码超了的时候给的是**空 list 而不是报错**，而总数照样给。
//
// ⚠️ 界面上判「还有没有」靠的就是这个总数（拿累计加载数去比它）。空页把总数
// 一起吞掉的话，界面上那一次请求回来的是「一共 0 个人」——用户刚看过 40 个人的
// 列表，刷新一下变成空的，而界面上什么都说不出来。
//
// ⚠️ 而**总数不是 0 也不等于「还有」**：空页 + 累计 20 < 总数 40，光比数字会
// 永远转圈。所以判据是两者与在一起（`internal/ui/follow.go` 的 `adoptFollow`）。
func TestAnEmptyPageStillReportsTheTotal(t *testing.T) {
	f := newFakeBili(t)
	onFollowList(t, f, 40, map[string][]any{
		"1": {followUserFixture(11, "甲"), followUserFixture(12, "乙")},
		"2": {},
	})
	pg, err := f.client(t, Options{}).FollowList(context.Background(), 4242, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Users) != 0 {
		t.Fatalf("这一页有 %d 个人", len(pg.Users))
	}
	if pg.Total != 40 {
		t.Errorf("空页把总数也吞了：Total = %d，want 40", pg.Total)
	}
}

// TestNoMidMeansNotLoggedIn 盯「没有自己的 mid」不发请求。
//
// 这一页的四个接口全都要 Cookie，而没有登录态就没有 mid。发出去的话 B 站回
// -101，而 -101 在这一层和「网络断了」一模一样（`client.go` 的 ErrNotLoggedIn）。
func TestNoMidMeansNotLoggedIn(t *testing.T) {
	f := newFakeBili(t)
	onFollowList(t, f, 0, nil)
	c := f.client(t, Options{})

	if _, err := c.FollowList(context.Background(), 0, 1); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("没有 mid 时返回 %v，want ErrNotLoggedIn", err)
	}
	if _, err := c.SearchFollowings(context.Background(), 0, "甲", 1); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("搜索那一支返回 %v，want ErrNotLoggedIn", err)
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("没登录还是发了 %d 个请求", n)
	}
}

// TestSearchFollowingsSendsTheKeyword 盯搜索那一支的参数和路径。
func TestSearchFollowingsSendsTheKeyword(t *testing.T) {
	f := newFakeBili(t)
	f.on(relationFollowingsSearchPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"total": 1, "list": []any{followUserFixture(99, "搜到的人")},
		}))
	})

	pg, err := f.client(t, Options{}).SearchFollowings(context.Background(), 4242, "搜", 1)
	if err != nil {
		t.Fatal(err)
	}

	got := lastRequestTo(t, f, relationFollowingsSearchPath)
	if got.Query.Get("vmid") != "4242" || got.Query.Get("name") != "搜" || got.Query.Get("ps") != "20" {
		t.Errorf("搜索那一支发出去的查询串不对：%v", got.Query)
	}
	if len(pg.Users) != 1 || pg.Users[0].Name != "搜到的人" || pg.Total != 1 {
		t.Errorf("搜索结果解成了 %+v", pg)
	}
}

// TestFollowTagUsersDecodesABareArray 钉住 `/x/relation/tag` 的形状。
//
// ⚠️ **B 站这一支的 `data` 是一个数组**，不是另外三个接口那种
// `{"list": […], "total": N}`。照后者解的话 fixture 照着错的合同造也全绿，
// 而真机上 `json.Unmarshal` 报的是一句「类型不匹配」。
//
// 造一个「有错也有对」都验不到的形状是很容易的，所以这里先照**对的**造一遍，
// 再照**错的**造一遍：后者必须报错。
func TestFollowTagUsersDecodesABareArray(t *testing.T) {
	f := newFakeBili(t)
	f.on(relationTagPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", []any{
			followUserFixture(21, "这一组的人"), followUserFixture(22, "还有一个"),
		}))
	})
	got, err := f.client(t, Options{}).FollowTagUsers(context.Background(), 12345, 2)
	if err != nil {
		t.Fatalf("数组形状解不出来：%v", err)
	}
	if len(got) != 2 || got[0].Name != "这一组的人" {
		t.Fatalf("解成了 %+v", got)
	}

	q := lastRequestTo(t, f, relationTagPath).Query
	if q.Get("tagid") != "12345" || q.Get("pn") != "2" || q.Get("ps") != "20" {
		t.Errorf("分组明细发出去的查询串不对：%v", q)
	}
}

func TestFollowTagUsersRejectsTheObjectShape(t *testing.T) {
	f := newFakeBili(t)
	// 故意造错的那一种：B 站在另外三个接口上都是这个形状。
	f.on(relationTagPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"list": []any{followUserFixture(21, "甲")}}))
	})
	if _, err := f.client(t, Options{}).FollowTagUsers(context.Background(), 1, 1); err == nil {
		t.Fatal("B 站的返回形状变了，这一层却当成功——界面上是「这一组还没有人」")
	}
}

// TestTheSpecialTagIsPassedThroughAsANumber 盯 -10 没被当成「没给」。
//
// B 站固定有两个不是用户建的分组：**-10 特别关注**、**0 默认分组**。而
// `if tagID != 0` 这种写法会把「默认分组」整个当成没给——于是默认分组里那些人
// 永远不出现，而界面上看不出是哪一步丢的。
func TestTheSpecialTagIsPassedThroughAsANumber(t *testing.T) {
	f := newFakeBili(t)
	f.on(relationTagPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", []any{followUserFixture(31, "特别关注的")}))
	})
	c := f.client(t, Options{})
	for _, tagID := range []int{-10, 0} {
		if _, err := c.FollowTagUsers(context.Background(), tagID, 1); err != nil {
			t.Fatalf("tagid=%d 报了错：%v", tagID, err)
		}
		if got := lastRequestTo(t, f, relationTagPath).Query.Get("tagid"); got != strconv.Itoa(tagID) {
			t.Errorf("tagid=%d 发出去的是 %q", tagID, got)
		}
	}
}

// TestAvatarAddressesAreReadyToFetch 盯头像地址在服务层就收拾好了。
//
// 一件事：**补协议**（B 站给的是 `//i0.hdslb.com/…`，缺协议会被当成普通路径去
// 读盘）。
//
// ⚠️ **裁剪参数不在这层拼了**（票 #634）：它由图片缓存按显示尺寸统一决定，
// 而这一层自己拼的话，别处（空间页 / 动态流 / 收藏夹）那一份就**拿不到**「按
// 显示尺寸」的那一档——同一个 UP 主的同一张脸于是成了两个缓存条目、两次下载。
func TestAvatarAddressesAreReadyToFetch(t *testing.T) {
	f := newFakeBili(t)
	f.on(relationTagsPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", []any{}))
	})
	onFollowList(t, f, 1, map[string][]any{"1": {followUserFixture(11, "甲")}})
	f.on(relationFollowingsSearchPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"total": 1, "list": []any{followUserFixture(12, "乙")},
		}))
	})
	f.on(relationTagPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", []any{followUserFixture(13, "丙")}))
	})
	c := f.client(t, Options{})

	pg, err := c.FollowList(context.Background(), 4242, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := pg.Users[0].Face; !strings.HasPrefix(got, "https://i0.hdslb.com/") ||
		strings.Contains(got, "@") {
		t.Errorf("「全部关注」的头像地址是 %q", got)
	}

	sp, err := c.SearchFollowings(context.Background(), 4242, "乙", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sp.Users[0].Face, "https://") {
		t.Errorf("搜索那一支的头像地址是 %q", sp.Users[0].Face)
	}

	tu, err := c.FollowTagUsers(context.Background(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tu[0].Face, "https://") {
		t.Errorf("分组明细那一支的头像地址是 %q", tu[0].Face)
	}
}

// TestTheOfficialLineComesFromDesc 盯认证那一行。
//
// `official_verify.type == -1` 是「没有认证」，而 B 站偶尔给一个 -1 却带着一句
// `desc`。照 type 判的话那一格画出来是一句空字符串，而界面上那个位置会留一段
// 空白——看起来像布局坏了。
func TestTheOfficialLineComesFromDesc(t *testing.T) {
	f := newFakeBili(t)
	weird := followUserFixture(11, "甲")
	weird["official_verify"] = map[string]any{"type": -1, "desc": "尽管 type 说没有"}
	none := followUserFixture(12, "乙")
	delete(none, "official_verify")
	onFollowList(t, f, 2, map[string][]any{"1": {weird, none}})

	pg, err := f.client(t, Options{}).FollowList(context.Background(), 4242, 1)
	if err != nil {
		t.Fatal(err)
	}
	if pg.Users[0].Official != "尽管 type 说没有" {
		t.Errorf("认证那一行是 %q", pg.Users[0].Official)
	}
	if pg.Users[1].Official != "" {
		t.Errorf("没有认证的人被安上了一行认证：%q", pg.Users[1].Official)
	}
}

// TestTheFollowPageNeverAsksForWBI 盯这几个接口**不签名**。
//
// 它们要的是 Cookie 而不是签名。签了名 B 站不会报错，但会在多传两个参数之外
// 拒掉一部分请求——而症状是「有时拉得到有时拉不到」，最难查的那一类。
func TestTheFollowPageNeverAsksForWBI(t *testing.T) {
	f := newFakeBili(t)
	f.on(relationTagsPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", []any{}))
	})
	onFollowList(t, f, 1, map[string][]any{"1": {followUserFixture(11, "甲")}})
	f.on(relationFollowingsSearchPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"total": 0, "list": []any{}}))
	})
	f.on(relationTagPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", []any{}))
	})
	c := f.client(t, Options{CSRF: func() string { return "the-jct" }})

	ctx := context.Background()
	if _, err := c.FollowTags(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.FollowList(ctx, 4242, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SearchFollowings(ctx, 4242, "甲", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := c.FollowTagUsers(ctx, 1, 1); err != nil {
		t.Fatal(err)
	}

	// 顺带钉住：四个都是 GET，而且**一个 csrf 都不许带**（带上了也不报错，
	// 但那是写接口的标记，混进来的话以后谁加一个 POST 会顺手抄错）。
	for _, req := range f.requests() {
		if req.Query.Get("w_rid") != "" {
			t.Errorf("%s 多发了一个 WBI 签名", req.Path)
		}
		if req.Query.Get("csrf") != "" {
			t.Errorf("%s 是一个读接口却带了 csrf", req.Path)
		}
		if req.Method != http.MethodGet {
			t.Errorf("%s 是 %s，四个接口全是 GET", req.Path, req.Method)
		}
	}
}

// TestAFailedFollowRequestSaysWhy 盯失败不是「没有关注」。
//
// 拉失败返回的是错，而「一条都没有」返回的是空列表 + nil。合成一句的话，用户
// 读到「暂无内容」会以为 B 站没给他关注——而界面上那个重试按钮也就没人按了。
func TestAFailedFollowRequestSaysWhy(t *testing.T) {
	f := newFakeBili(t)
	f.on(relationTagsPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, -101, "账号未登录", nil))
	})
	_, err := f.client(t, Options{}).FollowTags(context.Background())
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("返回 %v，want ErrNotLoggedIn", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != -101 {
		t.Errorf("不是一个带 -101 的 *APIError：%v", err)
	}
}

// TestTheFixtureNamesMatchThePathsTheyClaim 是一道**自检**。
//
// 四个接口的路径是照上游抄的死数。抄错一个字母，B 站会回 -403「访问权限不足」，
// 而那句话和「路径写错了」没有任何关系。这里把四条路径都摆出来，改的时候一眼
// 看得见改了哪一个。
func TestTheFixtureNamesMatchThePathsTheyClaim(t *testing.T) {
	for path, want := range map[string]string{
		relationTagsPath:             "/x/relation/tags",
		relationTagPath:              "/x/relation/tag",
		relationFollowingsSearchPath: "/x/relation/followings/search",
	} {
		if path != want {
			t.Errorf("路径是 %q，want %q", path, want)
		}
	}
	if followingsPath != "/x/relation/followings" {
		t.Errorf("followingsPath 是 %q（那个常量在 dynamic.go，改它要连作者栏一起改）", followingsPath)
	}
	// 页大小四处统一（Biu 的 PAGE_SIZE）。改一个不改另外三个的话，同一页里
	// 「全部关注」和某一组的大小会差着，而症状是「有的分组翻不满」。
	if followPageSize != 20 {
		t.Errorf("一页 %d 个，want 20（上游 PAGE_SIZE）", followPageSize)
	}
}
