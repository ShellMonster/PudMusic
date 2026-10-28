// 「我的关注」那一页的测试。
//
// 假 B 站、壳、等状态的几个工具在 upspace_test.go / dynamicfeed_test.go 里
// （同一个包，共用）。
//
// 这一组盯的是**页面上那些「两句话不能混」的地方**。这一页最容易出的错就是把
// 「没登录」说成「没关注任何人」——两者在界面上都是一片空白，而用户该做的事
// 完全不同：前者去登录，后者去关注。
package ui

import (
	"image/color"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 假接口
// ─────────────────────────────────────────────────────────────────────────────

// uiFollowUser 造一个 B 站给的关注对象。
//
// ⚠️ **`face` 那个主机是打不通的**（`https://f/…`，和 `dynamicfeed_test.go:28`、
// `upspace_test.go` 的头图同一个约定），**不要**改成真的 `i0.hdslb.com`：
//
// 这一页画的是一整屏头像，而 `Shell.New` 的封面缓存用的是**真的** HTTP 客户端
// （`imgcache.Options{}` 零值 → 15 秒超时、同时在途 6 个）。地址填成真的图床时，
// 这一页每张卡都会去**发一个真请求**，而在网络被掐住的那台机器上每一次都要等到
// 超时才完——`TestTheGridIsAVirtualGrid` 一张就有 40 个不同的地址，一整个用例
// 能从 1 秒拖到十几分钟，而症状是「`go test` 卡住不动」，和「这一页画得不对」
// 一点关系都没有。
//
// 顺带一提：地址里也不该有中文和空格（`name` 是「第一个 UP」那种）。`http.NewRequest`
// 对带空格的地址会报错，而报错的那一路量到的和「头像是 B 站给的」毫无关系。
func uiFollowUser(mid int64, name string) map[string]any {
	return uiFollowUserWithFace(mid, name, "https://f/face-"+strconv.FormatInt(mid, 10)+".jpg")
}

// uiFollowUserWithFace 是头像地址被指定的那一个。
//
// ⚠️ **量的是网格的用例请把地址给成同一个**（见 `uiFollowUserRange`）：每个不同的
// 地址都会在封面缓存里开一次下载，而缓存去解析一个打不通的主机同样要花时间
// （DNS 超时）。40 个不同地址就是 40 次解析，而这一组用例要量的事情和图片毫无
// 关系。
func uiFollowUserWithFace(mid int64, name, face string) map[string]any {
	return map[string]any{
		"mid": mid, "uname": name, "sign": name + " 的签名",
		"face":            face,
		"official_verify": map[string]any{"type": 0, "desc": name + " 的认证"},
	}
}

// uiFollowTags 给分组那一列装一份返回。groups 是 tagid → 名字。
func uiFollowTags(b *uiExplore, groups map[int]any) {
	b.on("/x/relation/tags", func(w http.ResponseWriter, r *http.Request) {
		list := make([]any, 0, len(groups))
		for id, name := range groups {
			list = append(list, map[string]any{
				"tagid": id, "name": name, "count": 1, "tip": "",
			})
		}
		uiExploreJSON(w, 0, list)
	})
}

// uiFollowList 给「全部关注」那一格装一份返回。
func uiFollowList(b *uiExplore, total int, users []any) {
	b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"total": total, "list": users})
	})
}

// uiFollowSearch 给搜索那一支装一份返回。命中 name 的给 users，不给空。
func uiFollowSearch(b *uiExplore, hit string, users []any) {
	b.on("/x/relation/followings/search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != hit {
			uiExploreJSON(w, 0, map[string]any{"total": 0, "list": []any{}})
			return
		}
		uiExploreJSON(w, 0, map[string]any{"total": len(users), "list": users})
	})
}

// uiFollowTag 给分组明细装一份返回（⚠️ B 站这一支的 data 是一个**数组**）。
func uiFollowTag(b *uiExplore, tagID int, users []any) {
	b.on("/x/relation/tag", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("tagid") != strconv.Itoa(tagID) {
			uiExploreJSON(w, 0, []any{})
			return
		}
		uiExploreJSON(w, 0, users)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 这一页
// ─────────────────────────────────────────────────────────────────────────────

// TestTheFollowPageShowsTheFollowedPeople 是这一页的主干：左边分组、右边网格。
func TestTheFollowPageShowsTheFollowedPeople(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{-10: "特别关注", 0: "默认分组", 777: "常听的"})
	uiFollowList(b, 3, []any{
		uiFollowUser(11, "第一个 UP"), uiFollowUser(12, "第二个 UP"),
		uiFollowUser(13, "第三个 UP"),
	})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 777, []any{uiFollowUser(77, "常听的那个人")})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()

	f := s.followPageOf()
	waitFrames(t, tt, "关注的人到了", func() bool { return len(f.users) == 3 && f.tagStarted })
	tt.Frame()

	for _, want := range []string{"第一个 UP", "第二个 UP", "第三个 UP"} {
		if !showsText(tt, want) {
			t.Errorf("关注的 %q 没画出来，文字有 %q", want, tt.Texts())
		}
	}
	// 左边的分组 Tab：一个都不能少。⚠️ **-10 和 0 是 B 站固定给的**，少了
	// 「特别关注」在网页版明明有。
	//
	// ⚠️ 名字后面那个括号也在断言里：上游 `index.tsx:238` 是
	// `${tag.name}${tag.count ? `(${tag.count})` : ""}`，而 0 的那一档不画括号
	// ——差的就是这一处，而空括号在界面上像加载坏了。
	for _, want := range []string{"全部关注(3)", "特别关注(1)", "默认分组(1)", "常听的(1)"} {
		if !showsText(tt, want) {
			t.Errorf("左边没有 %q 那一格，文字有 %q", want, tt.Texts())
		}
	}
	// 签名和认证也要画出来（它们是这张卡上除头像之外仅有的两行字）。
	if !showsText(tt, "第一个 UP 的签名") {
		t.Errorf("签名没画出来，文字有 %q", tt.Texts())
	}
	if !showsText(tt, "第一个 UP 的认证") {
		t.Errorf("认证没画出来，文字有 %q", tt.Texts())
	}
	// 切到某一格时左上角要跟着换名字（上游 index.tsx:206 那一支）。
	if !showsText(tt, "全部关注") {
		t.Errorf("左上角没画「全部关注」，文字有 %q", tt.Texts())
	}
}

// TestNotLoggedInSaysSoAndSendsNothing 是这一页最要紧的一条。
//
// 这四个接口全都要 Cookie，没登录时它们一律回 -101——而 -101 在这一层和「网络
// 断了」一模一样。所以：① 先判登录态，② **一个请求都不发**，③ 界面上说的是
// 「还没登录」而不是「还没关注任何人」。
func TestNotLoggedInSaysSoAndSendsNothing(t *testing.T) {
	b := newUIExplore(t)
	s, tt, _ := newSpaceShell(t, b, false, "")
	s.navigate(followPath)
	tt.Frame()
	tt.Frame()

	if !showsText(tt, followNotLoggedIn.Title) {
		t.Errorf("没登录时该说「%s」，文字有 %q", followNotLoggedIn.Title, tt.Texts())
	}
	// ⚠️ **不能是「没关注任何人」那一句**：用户登录完发现还是空的，会以为是
	// 登录坏了。
	if showsText(tt, lookup(followPath).Empty.Title) {
		t.Errorf("没登录时说成了「%s」——登录完还是空的，用户会以为是登录坏了，文字有 %q",
			lookup(followPath).Empty.Title, tt.Texts())
	}
	for _, p := range []string{
		"/x/relation/tags", "/x/relation/tag",
		"/x/relation/followings", "/x/relation/followings/search",
	} {
		if b.hit(p) {
			t.Errorf("没登录还去问了 %s（那个接口会回 -101）", p)
		}
	}
	// 还得给一条路走。
	if !showsText(tt, "去登录") {
		t.Errorf("没登录时没给「去登录」那一步，文字有 %q", tt.Texts())
	}
}

// TestLoggedInWithNobodyFollowedSaysSomethingElse 钉住「没关注任何人」那一支
// **和没登录不是同一句**，而且说得出该做什么。
func TestLoggedInWithNobodyFollowedSaysSomethingElse(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	uiFollowList(b, 0, []any{})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()

	f := s.followPageOf()
	waitFrames(t, tt, "「全部关注」拉完了", func() bool { return f.pager.started })
	tt.Frame()

	if showsText(tt, followNotLoggedIn.Title) {
		t.Errorf("登录了还说「%s」", followNotLoggedIn.Title)
	}
	e := lookup(followPath).Empty
	if !showsText(tt, e.Title) || !showsText(tt, e.Title) {
		t.Errorf("登录了、一个人都没关注，该说 %q / %q，文字有 %q", e.Title, e.Hint, tt.Texts())
	}
	// ⚠️ 那个「还没关注任何人」的说法得**真的跟没登录那一支不同**：两句话一样
	// 的话，用户读完还是不知道该做什么，而这正是这一页要消掉的毛病。
	if followNotLoggedIn.Title == e.Title || followNotLoggedIn.Hint == e.Hint {
		t.Errorf("「没登录」和「没关注任何人」说的是同一句：%q", followNotLoggedIn.Title)
	}
}

// TestEachTagTabShowsThatTagsPeople 盯左边点一下换右边。
func TestEachTagTabShowsThatTagsPeople(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组", 777: "常听的"})
	uiFollowList(b, 1, []any{uiFollowUser(11, "全部关注里的那个人")})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 777, []any{uiFollowUser(77, "常听的那个人")})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "第一格到了", func() bool { return len(f.users) == 1 && f.tagStarted })
	tt.Frame()

	if err := tt.Click("常听的(1)"); err != nil {
		t.Fatalf("点不到「常听的」那一格：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	// ⚠️ 等的是**那一个人**，不是「有一个人」：切组之前画面上就已经有一个人了，
	// 只等个数的话会立刻就通过，而读到的是**上一组**那一份。
	waitFrames(t, tt, "换到那一组、人也换了", func() bool {
		return f.tab == followTagTab(777) && followIndexOfMid(f.users, 77) >= 0
	})
	tt.Frame()

	if !showsText(tt, "常听的那个人") {
		t.Errorf("那一组的人没画出来，文字有 %q", tt.Texts())
	}
	if showsText(tt, "全部关注里的那个人") {
		t.Errorf("换组之后上一组的人还在画面上，文字有 %q", tt.Texts())
	}
	// ⚠️ **左上角跟着换**：停在某一组的时候它该是那一组的名字（上游
	// index.tsx:206 那一支），而塌回「全部关注」的话用户以为自己在看全部。
	if !showsText(tt, "常听的") {
		t.Errorf("左上角没跟着换组，文字有 %q", tt.Texts())
	}
	// ⚠️ **搜索框只在「全部关注」那一格画**（上游 index.tsx:280）：B 站的搜索
	// 接口搜的是「我关注的人」的全集，搜不出「某一组里的谁」。
	if showsText(tt, followSearchLabel) {
		t.Errorf("停在一个分组上时搜索框不该在（搜不出这一组里的谁），文字有 %q", tt.Texts())
	}
}

// TestTheSearchBoxFindsAFollowedPerson 盯页内搜索真的走的是那个接口。
func TestTheSearchBoxFindsAFollowedPerson(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	uiFollowList(b, 1, []any{uiFollowUser(11, "全部关注里的那个人")})
	uiFollowSearch(b, "周杰伦", []any{uiFollowUser(99, "搜到的周杰伦")})
	uiFollowTag(b, 0, []any{})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "第一格到了", func() bool { return len(f.users) == 1 && f.tagStarted })
	tt.Frame()

	box, ok := tt.Find(followSearchLabel)
	if !ok {
		t.Fatalf("搜索框没画出来，文字有 %q", tt.Texts())
	}
	tt.ClickAt(box.X+box.W/2, box.Y+box.H/2)
	tt.Frame()
	tt.Type("周杰伦")
	tt.Frame()
	tt.TypeKey(0, mygoui.KeyEnter, "\r")
	tt.Frame()

	waitFrames(t, tt, "搜到了", func() bool { return len(f.users) == 1 && f.applied == "周杰伦" })
	tt.Frame()

	if !showsText(tt, "搜到的周杰伦") {
		t.Errorf("搜出来的人没画出来，文字有 %q", tt.Texts())
	}
	if got := b.lastQueryOf("/x/relation/followings/search").Get("name"); got != "周杰伦" {
		t.Errorf("搜索那一支发出去的词是 %q", got)
	}
	// ⚠️ **「全部关注(N)」那个 N 不能被搜到的结果顶掉**：搜「周杰伦」搜到 1 个，
	// 而那一格下面挂着的 N 说的是我关注了 1 个人——两件事，挂错了就是一句假话。
	if !showsText(tt, "全部关注(1)") {
		t.Errorf("「全部关注」那个 N 被搜索结果顶掉了，文字有 %q", tt.Texts())
	}
}

// TestASearchWithNoHitsSaysSomethingDifferent 钉住「搜了没搜到」不是
// 「没关注任何人」。
//
// 合成一句的话，用户会把「搜错了」当成「B 站没给他推荐」，于是再去搜一遍别的
// 词——而真正的问题是他记错了那个人的名字。
func TestASearchWithNoHitsSaysSomethingDifferent(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	uiFollowList(b, 1, []any{uiFollowUser(11, "某个 UP")})
	uiFollowSearch(b, "不存在的词", []any{})
	uiFollowTag(b, 0, []any{})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "第一格到了", func() bool { return len(f.users) == 1 && f.tagStarted })
	tt.Frame()

	box, _ := tt.Find(followSearchLabel)
	tt.ClickAt(box.X+box.W/2, box.Y+box.H/2)
	tt.Frame()
	tt.Type("不存在的词")
	tt.Frame()
	tt.TypeKey(0, mygoui.KeyEnter, "\r")
	tt.Frame()

	waitFrames(t, tt, "搜完了", func() bool { return f.applied == "不存在的词" && f.pager.started })
	tt.Frame()

	if !showsText(tt, followNoSearchResult.Title) {
		t.Errorf("搜了没搜到该说 %q，文字有 %q", followNoSearchResult.Title, tt.Texts())
	}
	if showsText(tt, lookup(followPath).Empty.Title) {
		t.Errorf("搜了没搜到却说成了「%s」——用户会把搜错了当成 B 站没给他关注，文字有 %q",
			lookup(followPath).Empty.Title, tt.Texts())
	}
}

// TestClickingACardGoesToThatSpace 盯整张卡点一下跳进那个 UP 的空间。
//
// 照上游 `follow-list/user-card.tsx:66` 的 `onPress={() => navigate(/user/${u.mid})}`。
func TestClickingACardGoesToThatSpace(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	uiFollowList(b, 1, []any{uiFollowUser(4242, "点我")})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "人到了", func() bool { return len(f.users) == 1 && f.tagStarted })
	tt.Frame()

	// ⚠️ **点签名，不点昵称**：昵称在这一格里出现两次（一次是行的无障碍名字
	// `st.Label`，一次是卡上那个 Text），而 `tt.Click` 找的是**第一个**——
	// 那是整行，按下去落在两卡之间的缝里。
	if err := tt.Click("点我 的签名"); err != nil {
		t.Fatalf("点不到那张卡：%v，文字有 %q", err, tt.Texts())
	}
	if got := s.router.Path(); got != "/user/4242" {
		t.Errorf("点了卡跳到了 %q，want /user/4242", got)
	}
}

// TestAFailedFirstPageSaysSoAndOffersARetry 盯「拉不到」不是「没关注」。
//
// ⚠️ **这一条钉住的是排序**：失败那一支必须排在空状态前面。反过来的话第一页
// 失败时界面上是「暂无内容」——一句话都不提失败，切走再切回来也不会重拉
// （`err != nil` 一直挡着），用户看到的是「我真的一个 UP 主都没关注」。
func TestAFailedFirstPageSaysSoAndOffersARetry(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, -101, nil)
	})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "拉失败了", func() bool { return f.pager.err != nil })
	tt.Frame()

	if showsText(tt, lookup(followPath).Empty.Title) {
		t.Errorf("拉失败了却画成了「%s」，文字有 %q", lookup(followPath).Empty.Title, tt.Texts())
	}
	if !showsText(tt, "这一块没拉出来") {
		t.Errorf("没说清是「拉不到」，文字有 %q", tt.Texts())
	}
	if !showsText(tt, "重试加载关注的人") {
		t.Errorf("没给重试，文字有 %q", tt.Texts())
	}

	// 按一下重试：B 站这次给得出东西了，而界面上要跟着换过来。
	uiFollowList(b, 1, []any{uiFollowUser(11, "重试之后那个人")})
	if err := tt.Click("重试加载关注的人"); err != nil {
		t.Fatalf("点不到重试：%v，文字有 %q", err, tt.Texts())
	}
	waitFrames(t, tt, "重试之后到了", func() bool { return len(f.users) == 1 })
	tt.Frame()
	if !showsText(tt, "重试之后那个人") {
		t.Errorf("重试之后人没画出来，文字有 %q", tt.Texts())
	}
}

// TestAFailureDoesNotRetryItselfForever 盯「失败之后不自动重发」。
//
// 那一段每帧都跑，而 `upPager.want()` 因为 `err != nil` 一直挡着。不挡的话实测
// 300 毫秒能发 138 次，界面停在「打不开」上还在刷网络。
func TestAFailureDoesNotRetryItselfForever(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, -101, nil)
	})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "拉失败了", func() bool { return f.pager.err != nil })

	before := b.countHit("/x/relation/followings")
	for range 60 {
		tt.Frame()
	}
	if after := b.countHit("/x/relation/followings"); after != before {
		t.Errorf("失败了之后又自己发了 %d 次请求（界面停在错上还在刷网络）", after-before)
	}
}

// TestSwitchingAccountsThrowsTheOldListAway 盯换账号整页作废。
//
// 左边列的是「我关注的人」、分组也是「我的分组」，两样都是**按账号**的。换账号后
// 留着上一份的话，用户在新账号下看的是旧账号的关注——而界面上没有任何东西提示
// 这一点（和 `dynamicFeed` 同一个理由）。
func TestSwitchingAccountsThrowsTheOldListAway(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	uiFollowList(b, 1, []any{uiFollowUser(11, "账号 77 关注的那个人")})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	s, tt, a := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "第一个账号的关注到了", func() bool { return len(f.users) == 1 })
	tt.Frame()
	if f.selfMid != 77 {
		t.Fatalf("这一页记的账号是 %d，want 77", f.selfMid)
	}

	// 换成一个什么都关注了没有的账号。
	a.TestOnlyAdoptLogin("88")
	uiFollowList(b, 0, []any{})
	tt.Frame()
	waitFrames(t, tt, "第二个账号的关注到了", func() bool {
		return f.selfMid == 88 && f.pager.started && len(f.users) == 0
	})
	tt.Frame()

	if showsText(tt, "账号 77 关注的那个人") {
		t.Errorf("换了账号还在看上一个账号的关注，文字有 %q", tt.Texts())
	}
	if !showsText(tt, lookup(followPath).Empty.Title) {
		t.Errorf("新账号一个关注都没有，该说 %q，文字有 %q", lookup(followPath).Empty.Title, tt.Texts())
	}
}

// TestTheGridIsAVirtualGrid 盯这一页真的是一个**虚拟**网格。
//
// ⚠️ 不是「画面上摆得下」的那种验：那一条在一个只有 3 张卡的 fixture 上照样绿。
// 这里给 40 张卡（两页）并滚到底，靠的是「滚到底之后最后一张卡画出来了」——
// 而 B 站 `pn=2` 那一页正是被这一条叫起来的。
func TestTheGridIsAVirtualGrid(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	pages := map[string][]any{
		"1": uiFollowUserRange(1, 20),
		"2": uiFollowUserRange(21, 20),
	}
	b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
		pn := r.URL.Query().Get("pn")
		if pages[pn] == nil {
			pn = ""
		}
		uiExploreJSON(w, 0, map[string]any{"total": 40, "list": pages[pn]})
	})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "第一页到了", func() bool { return len(f.users) == 20 })
	tt.Frame()

	if showsText(tt, "UP 21") {
		t.Fatalf("第二页还没滚到就已经画出来了——那说明不是虚拟的，文字有 %q", tt.Texts())
	}

	// ⚠️ 滚到底这件事**分两步**，而且两步都得等条件：先把哨兵滚进画面（第二页就是
	// 这么叫起来的），等第二页真的到了，**再滚一次**——行数翻倍之后滚动位置不会自己
	// 跟过去（`ScrollState` 记的是偏移量，不是「我盯着最后一行」）。合成一个固定的
	// 「滚 8 下」就是在赌第二页在那 8 下之内回来了，理由见 `scrollToEnd`。
	scrollFollowToEnd(t, tt, f)
	waitFrames(t, tt, "滚到底之后第二页也拉到了", func() bool { return len(f.users) == 40 })
	scrollFollowToEnd(t, tt, f)
	tt.Frame()

	// ⚠️ 断的是**末尾**：最后一屏是 33..40，所以问的是 UP 40（不是 UP 21——
	// 它在第二页的中段，滚到底时早滚过去了，量它等于量「屏幕能显示几行」，
	// 那不是虚拟不虚拟的事）。
	if !showsText(tt, "UP 40 的签名") {
		t.Errorf("滚到底之后末尾那几张没画出来，文字有 %q", tt.Texts())
	}
	// 而**虚拟**量的是这一条：40 张卡不可能同时在屏幕上，第一页开头那几张
	// 必须已经滚出画面。
	if showsText(tt, "UP 1 的签名") {
		t.Errorf("40 张卡同时在画面上——那不是虚拟网格，是一个普通列表，文字有 %q", tt.Texts())
	}
	if !f.pager.started || f.pager.hasMore {
		t.Errorf("40 个人 / 每页 20 = 两页，第二页之后不该说「还有」（%+v）", f.pager)
	}
}

// uiFollowUserRange 造 from 起的 count 个人，名字是「UP <序号>」。
//
// ⚠️ **头像地址全用同一个**：这一组用例量的是「40 张卡是不是一个虚拟网格」，
// 和图片一点关系都没有，而每个不同的地址都会在封面缓存里开一次下载
// （见 `uiFollowUserWithFace` 顶上那一段）。
func uiFollowUserRange(from, count int) []any {
	const sharedFace = "https://f/grid-card.jpg"
	out := make([]any, 0, count)
	for i := range count {
		out = append(out, uiFollowUserWithFace(int64(from+i), "UP "+strconv.Itoa(from+i), sharedFace))
	}
	return out
}

// 滚轮落在关注网格上的那一点（页面右边那一格）。落偏了滚的是侧栏或者顶栏。
const followScrollX, followScrollY float32 = 600, 400

// scrollFollowToEnd 滚到关注网格的末尾，**等末尾那一行真的在画面上**。
//
// ⚠️ 判据是 `f.state.AtEnd()`——框架自己判「这一帧露出了末尾」用的那一个
// （`mygo/ui/list.go`：`hi == n-1 && end()-glide <= H+0.5`）。这一组用例断的正是
// 末尾那几张（`UP 40` / `UP 25` / `UP 39`），而末尾那一行露没露出来就是这些断言成立
// 的前提——把它当条件等，判据一个字没放宽。
//
// ⚠️ **下一次翻页到了之后要再调一次**：行数翻倍之后滚动位置停在旧的高度上，
// 末尾那几张还在画面下面。合成一个固定的「滚 8 下」是在赌第二页在那 8 下之内回来，
// 理由见 `scrollToEnd`。
func scrollFollowToEnd(t *testing.T, tt *mygoui.Tester, f *followPage) {
	t.Helper()
	scrollToEnd(t, tt, followScrollX, followScrollY, "关注网格的末尾", f.state.AtEnd)
}

// TestTheRowsAreKeyedByWhoNotByNumber 钉住铁律四。
//
// ⚠️ **两个一模一样的行身份**会让框架按 key 对位时那几行共用一份状态——Tester 里
// 当场 panic，真窗口里选中一格隔壁那格也亮着，而界面上看不出任何异样。而拿序号
// 当身份的话，刷新之后用户盯着的那张卡会整个换成旁边那一张。
//
// 这里验的是「行身份跟着这一行里的第一个人走」：换列数之后它必须换。
func TestTheRowsAreKeyedByWhoNotByNumber(t *testing.T) {
	f := &followPage{cols: 4, users: []bili.FollowUser{
		{Mid: 1}, {Mid: 2}, {Mid: 3}, {Mid: 4}, {Mid: 5},
	}}
	// 4 列：第 0 行是 1..4，第 1 行开头是 5。
	if got := followRowKey(f.users, 1, 4); got != "follow:5" {
		t.Errorf("第 1 行的身份是 %q，want follow:5（这一行第一个人的 mid）", got)
	}
	// 换列数之后行身份跟着换——它**不是**行号。
	if got := followRowKey(f.users, 1, 2); got != "follow:3" {
		t.Errorf("2 列下第 1 行的身份是 %q，want follow:3", got)
	}
	// 越界那一行（末尾哨兵）是另一种身份，和任何人的 mid 撞不上。
	if got := followRowKey(f.users, 9, 4); got == "follow:1" {
		t.Errorf("哨兵那一行的身份和第一个人的撞上了：%q", got)
	}
}

// TestTheTagAndAllTabsHaveDifferentIdentities 盯左边两格的身份不撞。
//
// B 站的分组 id **可正可负**（-10 特别关注、0 默认分组），而 mid 是正数。
func TestTheTagAndAllTabsHaveDifferentIdentities(t *testing.T) {
	if followTabAll == followTagTab(0) || followTabAll == followTagTab(-10) {
		t.Fatalf("「全部关注」和某个分组的身份撞上了：%q", followTabAll)
	}
	for _, id := range []int{-10, 0, 777} {
		got, ok := tagIDOf(followTagTab(id))
		if !ok || got != id {
			t.Errorf("tagIDOf(followTagTab(%d)) = %d, %v", id, got, ok)
		}
	}
	if _, ok := tagIDOf(followTabAll); ok {
		t.Error("「全部关注」被当成了某个分组")
	}
}

// TestAPageThatComesBackForTheWrongTabNeverLands 盯代次。
//
// 🔴 **这一条是 cancel 挡不住的**：请求已经回来了、正往信箱里投的时候 cancel 已经
// 来得及了。所以真正挡着的是代次——而代次这一段只有在**状态这一层**才验得到：
// 从假 B 站那一头去造「一个不理 ctx 的服务器」要另起一台 httptest，而那验的
// 是 net/http 而不是这一段。
func TestAPageThatComesBackForTheWrongTabNeverLands(t *testing.T) {
	s, _ := newShell(t)
	// 停在某一组上，代次 2，账号 77。
	f := &followPage{tab: followTagTab(777), gen: 2, selfMid: 77}

	// 三次「发出去之后情况变了」的回来，都不许落地。
	f.box = []followResult{
		// ① 换组之前发的那一页（代次 1、停在「全部关注」）。
		{which: followUsers, selfMid: 77, gen: 1, tab: followTabAll,
			users: []bili.FollowUser{{Mid: 11, Name: "上一组的那个人"}}},
		// ② 代次对、停的组对，可是**带着一个已经不生效的搜索词**回来的那一页。
		// （换词同样 bump 代次，所以这一条量的是关键词那一道：它是加在那儿的
		// 第二道，而加它的理由是「代次只认『发出去了几件事』」这件事会在将来
		// 有人复用的时候漏掉。）
		{which: followUsers, selfMid: 77, gen: 2, tab: followTagTab(777),
			keyword: "上一个词",
			users:   []bili.FollowUser{{Mid: 12, Name: "上一个词搜到的"}}},
		// ③ 换账号之前发的那一页。
		{which: followUsers, selfMid: 88, gen: 2, tab: followTagTab(777),
			users: []bili.FollowUser{{Mid: 13, Name: "上一个账号的"}}},
	}
	s.adoptFollow(f)

	if len(f.users) != 0 {
		t.Errorf("作废的那几页落地了：%+v", f.users)
	}
	// ⚠️ **`settle` 必须无条件调**：作废的那一页如果不解开闸门，这一格会永远
	// 停在「正在加载」，而 `want()` 因为 `loading` 一直挡着，连重试都点不到。
	if f.pager.loading {
		t.Error("作废的那一页没有把闸门解开——界面上会永远转圈")
	}
	if f.pager.started {
		t.Error("一个格子都没收到却标成了「拉过了」——空状态那一支永远画不出来")
	}
}

// TestAPageForTheTabWeAreOnDoesLand 是上面那条的另一半。
//
// 只测「作废的那几页没落地」的话，一个**什么都不收**的实现也绿。
func TestAPageForTheTabWeAreOnDoesLand(t *testing.T) {
	s, _ := newShell(t)
	f := &followPage{tab: followTagTab(777), gen: 2, selfMid: 77}
	f.box = []followResult{{
		which: followUsers, selfMid: 77, gen: 2, tab: followTagTab(777),
		users: []bili.FollowUser{{Mid: 77, Name: "这一组的人"}},
	}}
	s.adoptFollow(f)

	if len(f.users) != 1 || f.users[0].Mid != 77 {
		t.Fatalf("当前这一页没有落地：%+v", f.users)
	}
	if !f.pager.started {
		t.Error("收到了却没标成「拉过了」——下一帧又拉一次第一页")
	}
}

// TestTheLastPartialPageEndsThePaging 盯「还有没有」是拿**累计**算的。
//
// 🔴 **总数 25、每页 20**：第二页发回来 5 个。拿**这一页的 5 个**去比 25 是
// 「还有」，于是第三页也被拉了——而 B 站给的是空 list，界面上是末尾多一行转圈
// 转一帧才停。次数不多，但症状是「明明已经到底了还在转」。
//
// 而真正会卡住的是更糟的那一种：累计也还是判「还有」的话，一次滚动能连着发
// 好几页空请求（B 站风控不是摆设）。
func TestTheLastPartialPageEndsThePaging(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	pages := map[string][]any{
		"1": uiFollowUserRange(1, 20),
		"2": uiFollowUserRange(21, 5), // 25 = 20 + 5，最后一页不满
	}
	b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
		pn := r.URL.Query().Get("pn")
		if pages[pn] == nil {
			pn = ""
		}
		uiExploreJSON(w, 0, map[string]any{"total": 25, "list": pages[pn]})
	})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "第一页到了", func() bool { return len(f.users) == 20 })
	tt.Frame()

	// ⚠️ 和 `TestTheGridIsAVirtualGrid` 同一个形状：滚到底这一步只能等条件。
	// 这一条比那一条多一层——最后一页只有 5 个人，而断的又是末尾（`UP 25`），
	// 所以第二页到了之后必须**再滚一次**才看得见它。
	scrollFollowToEnd(t, tt, f)
	waitFrames(t, tt, "最后一页也到了", func() bool { return len(f.users) == 25 })
	tt.Frame()
	scrollFollowToEnd(t, tt, f)

	if f.pager.hasMore {
		t.Error("25 个人 / 每页 20 = 到了底，却还说「还有」——下一帧又去拉一个空页")
	}
	// ⚠️ 再画几十帧，一个多余的请求都不许有：末尾那个哨兵行还占着行高，所以
	// 「滚到底」这一条每帧都会命中，唯一的闸门就是 hasMore。
	before := b.countHit("/x/relation/followings")
	for range 60 {
		tt.Frame()
	}
	if after := b.countHit("/x/relation/followings"); after != before {
		t.Errorf("到底之后又多发了 %d 个请求（%d → %d）", after-before, before, after)
	}
	if !showsText(tt, "UP 25 的签名") {
		t.Errorf("末尾那一张没画出来，文字有 %q", tt.Texts())
	}
}

// TestEveryEmptySentenceOnThisPageSaysWhyAndHow 把这一页自己那几句空状态过一遍
// 通用检查。
//
// ⚠️ **通用那条覆盖不到它们**：`TestEmptyStateCopySaysWhyAndHow` 跳过所有接上
// `builtPage` 的路由（理由是「那一页画的是它自己」），而这一页接上了。于是
// 「还没登录」「没搜到」「这个分组还没有人」这三句连同路由表里那一句就成了没人
// 看的一句——而这一页最容易出的错恰恰是这几句话互相串台。
func TestEveryEmptySentenceOnThisPageSaysWhyAndHow(t *testing.T) {
	all := map[string]emptyState{
		"没登录":     followNotLoggedIn,
		"没关注任何人":  lookup(followPath).Empty,
		"搜了没搜到":   followNoSearchResult,
		"这一组还没有人": followEmptyTag,
	}
	for what, e := range all {
		if strings.TrimSpace(e.Title) == "" {
			t.Errorf("%s 没说「这里为什么空」", what)
		}
		if strings.TrimSpace(e.Hint) == "" {
			t.Errorf("%s 没说「怎么才能不空」", what)
			continue
		}
		if e.Title == e.Hint {
			t.Errorf("%s 两句说的是同一件事：%q", what, e.Title)
		}
		if !hasActionCue(e.Hint) {
			t.Errorf("%s 的「怎么才能不空」%q 里没有动作词", what, e.Hint)
		}
	}
	// ⚠️ **四句的标题必须互不相同**：标题一样的话用户在界面上分不出「要我去登录」
	// 和「我要去关注」，而这两个的下一步完全相反。
	seen := map[string]string{}
	for what, e := range all {
		if other, dup := seen[e.Title]; dup {
			t.Errorf("「%s」和「%s」说的是同一个标题 %q", what, other, e.Title)
		}
		seen[e.Title] = what
	}
}

// TestTheseFourEndpointsMayCarryCredentials 钉住「凭据走已建好的边界」这条硬要求。
//
// 这四个接口**全都要 Cookie**（没登录一律 -101），而凭据是 `CredentialTransport`
// 按白名单逐跳判的。这里逐个路径过一遍那张白名单，是为了让「换个主机名就悄悄
// 拿不到登录态」这件事在**这一票**里就红，而不是等到用户扫码登录完打开关注页
// 发现是空的（而那句话和「网络断了」长得一模一样）。
//
// ⚠️ 顺带钉住反面：这三个主机一个都不在凭据这一档——把 Cookie 发给 CDN 是把账号
// 交给一台存静态文件的机器。
func TestTheseFourEndpointsMayCarryCredentials(t *testing.T) {
	p := app.DefaultInjectPolicy()
	for _, path := range []string{
		"/x/relation/tags", "/x/relation/tag",
		"/x/relation/followings", "/x/relation/followings/search",
	} {
		got := p.Classify("https://api.bilibili.com" + path)
		if got != app.CredBiliFull {
			t.Errorf("%s 判成了 %v，want bilibili-full——这四个接口没登录一律 -101", path, got)
		}
	}
	for _, u := range []string{
		"https://i0.hdslb.com/bfs/face/a.jpg",
		"https://api.biliapi.net/x/relation/tags",
	} {
		if got := p.Classify(u); got == app.CredBiliFull {
			t.Errorf("%s 拿到了完整凭据", u)
		}
	}
}

// TestAFailedTagListDoesNotRetryItselfForever 钉住左边那一列的**闸门**。
//
// 🔴 **这一条是评审抓出来的 critical**：那一段每帧都跑，而失败之后 `tagStarted`
// 仍是 false、`tagLoading` 也回到 false——于是每一帧都满足条件。真正让它转起来
// 的是**每一次失败都有 `s.wake.invalidate()`**（铁律三，见 `adoptFollow`）：
// 请求 → 失败 → 叫醒 → 要一帧 → 下一帧又发。用户什么都不用点。
//
// 实测（改之前）：假 B 站一直回 -412，60 帧里 `/x/relation/tags` 被打 186 次。
// 触发条件全是日常的：SESSDATA 过期（本地看着还登录着，B 站回 -101）、
// -412/-352 风控、网络抖一下。
//
// ⚠️ 右边那一格有同一道闸（`upPager.want()` 里的 `p.err != nil`），所以这一条
// 量的是**两边必须一样**——一边有闸一边没有，就是一个停不下来的请求循环。
func TestAFailedTagListDoesNotRetryItselfForever(t *testing.T) {
	b := newUIExplore(t)
	uiFollowList(b, 1, []any{uiFollowUser(11, "全部关注里的那个人")})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})
	// 分组这一支一直失败。
	b.on("/x/relation/tags", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, -412, nil)
	})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "分组拉失败了", func() bool { return f.tagErr != nil })

	// ⚠️ **再多画几十帧，一个重试都不许有**：唯一的出口是用户点「重试拉分组」。
	before := b.countHit("/x/relation/tags")
	for range 60 {
		tt.Frame()
	}
	if after := b.countHit("/x/relation/tags"); after != before {
		t.Errorf("分组拉失败了之后又自己发了 %d 次（%d → %d）——每一帧都满足"+
			"「没拉到」，而每次失败都要一帧新画面，于是自己转起来了",
			after-before, before, after)
	}
	// 失败之后要说得清，并且给一条路走。
	//
	// ⚠️ **这里要用 `tt.HasText`（子串），不能用 `showsText`（全等）**：那一行是
	// 「分组没拉出来：<原因>」，原因跟着错走，而全等匹配钉不住一个前缀。
	if !tt.HasText("分组没拉出来") {
		t.Errorf("没说清是分组没拉出来，文字有 %q", tt.Texts())
	}
	if !showsText(tt, "重试拉分组") {
		t.Errorf("没给重试，文字有 %q", tt.Texts())
	}
}

// TestAFailedTagListCanBeRetriedByHand 钉住「闸门关上之后，人还能再开一次」。
//
// 少了这后半句，上一条的修法就有一半可能走偏：把触发条件整个删掉的话失败之后
// **永远**重试不了了，而 -101（SESSDATA 过期）恰恰是最需要能重试的那一种。
func TestAFailedTagListCanBeRetriedByHand(t *testing.T) {
	b := newUIExplore(t)
	uiFollowList(b, 1, []any{uiFollowUser(11, "全部关注里的那个人")})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	failed := true
	b.on("/x/relation/tags", func(w http.ResponseWriter, r *http.Request) {
		if failed {
			uiExploreJSON(w, -412, nil)
			return
		}
		uiExploreJSON(w, 0, []any{
			map[string]any{"tagid": 0, "name": "重试之后有了", "count": 1, "tip": ""},
		})
	})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "分组拉失败了", func() bool { return f.tagErr != nil })

	failed = false // B 站这一次肯给了
	if err := tt.Click("重试拉分组"); err != nil {
		t.Fatalf("点不到重试：%v，文字有 %q", err, tt.Texts())
	}
	waitFrames(t, tt, "重试之后分组到了", func() bool { return f.tagStarted && f.tagErr == nil })
	tt.Frame()
	// ⚠️ 同样是子串：那一格的名字后面跟着 `(1)`（B 站的分组成员数）。
	if !tt.HasText("重试之后有了") {
		t.Errorf("重试之后分组没画出来，文字有 %q", tt.Texts())
	}
}

// TestAFullyOverlappingPageStillLoadsTheNextOne 钉住「整页重叠」不把分页掐死。
//
// 🔴 **评审抓出来的 major**：`upPager.adopt(cursor, sent, hasMore)` 里的 `sent`
// 说的是「B 站发了几个、过滤**之前**」（`upspace.go` 那段注释记着同一个坑：一页
// 全被丢弃时留下 0 条，拿 0 去判就成了「到底了」）。
//
// 传「去重之后新收进来几个」是同一个坑的另一种形状：**B 站翻页整页重叠**时那
// 20 个人全是上一页那些、去重后一个不留，于是 `hasMore` 被判成「没有了」。
// 症状：Tab 上写着 (59)、画面上只有 20 张卡，而且**末尾哨兵那一行也不画了**
// ——于是连手动加载的入口都没有。
func TestAFullyOverlappingPageStillLoadsTheNextOne(t *testing.T) {
	b := newUIExplore(t)
	// 第 2 页**整页**和第 1 页重叠；第 3 页有 19 个新的。总数 59 —— 比三页还多，
	// 所以它一定还没到头。
	pages := map[string][]any{
		"1": uiFollowUserRange(1, 20),
		"2": uiFollowUserRange(1, 20), // 完全重叠
		"3": uiFollowUserRange(21, 19),
	}
	// 总数照**真实存在的人数**给 39（20 + 20 重叠 + 19 个新的）。写成 59 的话
	// fixture 自己不自洽：B 站不可能一共 59 个人而只发得出 39 个，于是翻页会
	// 多拉一个空的第 4 页——而那一条会把「hasMore 该不该是 false」量成别的事。
	b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
		pn := r.URL.Query().Get("pn")
		if pages[pn] == nil {
			pn = ""
		}
		uiExploreJSON(w, 0, map[string]any{"total": 39, "list": pages[pn]})
	})
	uiFollowTags(b, map[int]any{0: "默认分组"})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "第一页到了", func() bool { return len(f.users) == 20 })
	tt.Frame()

	// ⚠️ 这一条要滚**两次**：第二页整页重叠（一个人都添不上，行数不变），
	// 第三页那 19 个到了之后行数才第二次翻倍，于是末尾要再滚一次才看得见。
	scrollFollowToEnd(t, tt, f)
	// ⚠️ **等的条件是「累计 39」**：第二页整页重叠、一个人都添不上，
	// 而第三页那 19 个必须照样到。
	waitFrames(t, tt, "整页重叠之后第三页也到了", func() bool { return len(f.users) == 39 })
	tt.Frame()
	scrollFollowToEnd(t, tt, f)

	if !showsText(tt, "全部关注(39)") {
		t.Errorf("Tab 上的总数对不上，文字有 %q", tt.Texts())
	}
	// ⚠️ **断的是「末尾」**：滚到底之后画面上是 25..39，所以问的是 UP 39。
	// （问 UP 21 量到的是「屏幕能显示几行」，不是「分页有没有停」。）
	if !showsText(tt, "UP 39 的签名") {
		t.Errorf("整页重叠把分页掐死了：第三页那 19 个人没画出来，文字有 %q", tt.Texts())
	}
	// 39 / 39 —— 到头了，而且**是被累计数算出来**的，不是被「这一页一个人都没
	// 添上」算出来的。
	if f.pager.hasMore {
		t.Error("39 / 39 已经到底了，却还说「还有」")
	}
}

// TestSwitchingAccountsDoesNotKeepTheOldTotal 钉住「全部关注(N)」跟着账号走。
//
// 评审提的 trivial：`resetUsers` 原先不清 `f.total`，于是**新账号的第一页落地
// 之前**那个数一直挂在 Tab 上；而新账号的第一页要是恰好拉失败，它就一直挂着
// 上一个账号的关注数，界面上没有任何东西提示这一点。
func TestSwitchingAccountsDoesNotKeepTheOldTotal(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	uiFollowList(b, 7, []any{uiFollowUser(11, "账号 77 关注的那个人")})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	s, tt, a := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "第一个账号的关注到了", func() bool {
		return f.selfMid == 77 && f.total == 7
	})
	tt.Frame()
	if !showsText(tt, "全部关注(7)") {
		t.Fatalf("第一个账号的总数没画出来，文字有 %q", tt.Texts())
	}

	// 换分组：那个 N **不许动**——它说的是「这个账号关注了多少人」，跟着账号走而
	// 不跟着「停在哪个分组」走（Biu 那边换组它也还在，见 `index.tsx:224`）。
	if err := tt.Click("默认分组(1)"); err != nil {
		t.Fatalf("点不到分组：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	waitFrames(t, tt, "切到那一组", func() bool { return f.tab == followTagTab(0) })
	tt.Frame()
	if f.total != 7 {
		t.Errorf("换分组之后那个 N 变成了 %d——它跟着账号走，不跟着停在哪个分组走", f.total)
	}

	// 换账号：那个 N **必须掉下来**。
	a.TestOnlyAdoptLogin("88")
	uiFollowList(b, 0, []any{}) // 新账号一个关注都没有
	// ⚠️ **判据是「画面上那个 N 已经不掉着上一个账号的数了」**，而不是内部的
	// `f.total` / `pager.started`。
	//
	// 原来等的是 `f.selfMid == 88 && f.pager.started`、下一行断 `f.total != 0`：
	// `started` 只在 `adopt()`（那一趟成功）时置位，而换账号之后**那一趟偶发会被
	// 作废**（在途的请求被上一代次掐掉），于是它在机器一忙时就永远到不了、
	// `waitFrames` 超时——而换账号那一刻 `f.total` 其实已经是 0 了（见
	// `resetFollowIfAccountChanged`），功能是好的。这一条实测在整包连跑里飘过
	// 两次，**在未改动的 main 上也飘**，单跑 30 次里飘过 1 次。
	//
	// ⚠️ **判据没放宽，反而更严**：「全部关注(7)」那一栏正是 `f.total` 画出来的，
	// 而它换账号**之前**就在画面上——所以这个判据不可能提前成立，等到的就是
	// 「重画成新账号那个数」。末尾那两行断言照样断内部的 `f.total`。
	waitFrames(t, tt, "新账号的那个 N 掉下来了", func() bool {
		return f.selfMid == 88 && !showsText(tt, "全部关注(7)")
	})
	if f.total != 0 {
		t.Errorf("换账号之后那个 N 仍然是 %d——它是上一个账号的关注数", f.total)
	}
	if showsText(tt, "全部关注(7)") {
		t.Errorf("新账号的 Tab 上还挂着上一个账号的关注数，文字有 %q", tt.Texts())
	}
}

// TestTheFollowCardTypographyAndItsHoverBar 钉住关注卡的两处：
// 名字 18、签名 14（上游 `user-card.tsx:84-85` 的 `text-lg` / `text-sm`），
// 以及鼠标移上去才升起来的那条操作条（300ms，`translate-y-20` → 0）。
func TestTheFollowCardTypographyAndItsHoverBar(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	uiFollowList(b, 3, []any{
		uiFollowUser(11, "第一个 UP"), uiFollowUser(12, "第二个 UP"), uiFollowUser(13, "第三个 UP"),
	})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "人到了", func() bool { return len(f.users) == 3 && f.tagStarted })
	tt.Frame()

	// ① 字号。第二张卡的名字不是整行的无障碍名字（那是第一张卡的），所以
	// `Find` 拿到的就是名字那一段文字本身；签名一直是唯一的。
	//
	// 量的是**盒子高**：行高跟着字号走，18 点那行实测 21.2、14 点加 1.4 行高
	// 实测 19.6。取 20.5 / 19.0 两条线——旧值 15 / 12 分别是 17.7 / 16.8，
	// 离得够开。
	name, ok := tt.Find("第二个 UP")
	if !ok {
		t.Fatalf("画面上找不到名字，文字有 %q", tt.Texts())
	}
	if name.H < 20.5 {
		t.Errorf("名字那一段只有 %.1f 高，want ≥ 20.5（上游是 text-lg，18 点）", name.H)
	}
	sign, ok := tt.Find("第二个 UP 的签名")
	if !ok {
		t.Fatalf("画面上找不到签名，文字有 %q", tt.Texts())
	}
	if sign.H < 19 {
		t.Errorf("签名那一段只有 %.1f 高，want ≥ 19（上游是 text-sm，14 点）", sign.H)
	}

	// ② 操作条平时不在画面上。
	if _, ok := tt.Find("取消关注"); ok {
		t.Fatalf("还没 hover，操作条就已经在画面上了（文字有 %q）", tt.Texts())
	}
	av, ok := tt.Find(f.users[1].Face)
	if !ok {
		t.Fatalf("第二张卡的头像没画出来，文字有 %q", tt.Texts())
	}
	tt.Move(av.X+av.W/2, av.Y+av.H/2)
	tt.Frame()
	// 悬停的那一帧动画才刚开始，位置还藏在卡片下沿外。
	if _, ok := tt.Find("取消关注"); ok {
		t.Fatalf("刚移上去的那一帧操作条就已经到位了——300ms 上滑没走")
	}

	// 推钟：150ms 时已经在画面里，但还没升到终点；再走完 300ms 就到顶。
	tt.Advance(150 * time.Millisecond)
	tt.Frame()
	mid, ok := tt.Find("取消关注")
	if !ok {
		t.Fatalf("hover 150ms 之后操作条还没出现，文字有 %q", tt.Texts())
	}
	tt.Advance(250 * time.Millisecond)
	tt.Frame()
	done, ok := tt.Find("取消关注")
	if !ok {
		t.Fatalf("hover 400ms 之后操作条不见了，文字有 %q", tt.Texts())
	}
	if done.Y >= mid.Y {
		t.Errorf("操作条在 150ms 时 y=%.1f、走完之后 y=%.1f——没有往上滑", mid.Y, done.Y)
	}

	// ③ 移开就收回去（同一段 300ms 走完）。
	tt.Move(5, 5)
	tt.Frame()
	tt.Advance(400 * time.Millisecond)
	tt.Frame()
	if _, ok := tt.Find("取消关注"); ok {
		t.Errorf("指针移开之后操作条还留在画面上，文字有 %q", tt.Texts())
	}
}

// TestTheFollowPaneHasARightBorder 钉住左栏右缘那条分界线（上游
// `follow-list/index.tsx:204` 的 `border-r`）。
func TestTheFollowPaneHasARightBorder(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	uiFollowList(b, 1, []any{uiFollowUser(11, "一个人")})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "人到了", func() bool { return len(f.users) == 1 && f.tagStarted })
	tt.Frame()

	pageLeft := int(sideTargetWidth(s.app.SettingsSnapshot()))
	img := tt.Image()
	bx := pageLeft + followPaneW - 1
	if got, ref := img.RGBAAt(bx, 400), img.RGBAAt(bx-4, 400); got == ref {
		t.Errorf("左栏右缘 (%d,400) 是 %v，和左边四像素一样——那条分界线没画", bx, got)
	}
}

// TestUnfollowingFromTheCardRemovesIt 钉住操作条上那个「取消关注」真的能按：
// 写接口走后台、成功后那一格从画面上消失、左栏的人数跟着减一。
func TestUnfollowingFromTheCardRemovesIt(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "我自己", 0, 1)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	uiFollowList(b, 2, []any{uiFollowUser(11, "第一个 UP"), uiFollowUser(12, "第二个 UP")})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "人到了", func() bool { return len(f.users) == 2 && f.tagStarted })
	tt.Frame()

	av, ok := tt.Find(f.users[1].Face)
	if !ok {
		t.Fatalf("第二张卡的头像没画出来，文字有 %q", tt.Texts())
	}
	tt.Move(av.X+av.W/2, av.Y+av.H/2)
	tt.Frame()
	tt.Advance(400 * time.Millisecond)
	tt.Frame()
	if err := tt.Click("取消关注"); err != nil {
		t.Fatalf("点不到操作条上的取消关注：%v，文字有 %q", err, tt.Texts())
	}

	waitFrames(t, tt, "取关之后那一格没了", func() bool { return len(f.users) == 1 })
	tt.Frame()
	if showsText(tt, "第二个 UP") {
		t.Errorf("取关成功了，画面上还挂着那个人，文字有 %q", tt.Texts())
	}
	if !b.hit("/x/relation/modify") {
		t.Errorf("取关没有真的发出去（假 B 站没收到 /x/relation/modify）")
	}
	if !showsText(tt, "全部关注(1)") {
		t.Errorf("取关之后左栏的人数没跟着减一，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 那一行是「布局」，不是「条目」
// ─────────────────────────────────────────────────────────────────────────────

// TestTheFollowGridRowHasNoHoverSlab 钉住「我的关注」右栏那一块 522×233 的
// 灰板（第二轮审计的 L-30）。
//
// # 那块灰板是什么
//
// 框架给「指针压着的那一行」铺一层 `t.SurfaceHover`（`mygo/ui/list.go` 的
// styleFn：`w.bg = t.SurfaceHover`）。这一页的行是**一排卡**，它比卡宽——
// 卡右边空出来的那一块会整块变成悬停底（浅色 `#e9e9ec`、深色 `#323236`），
// 而那里**什么都没有**。上游那一块是纯白。
//
// 悬停反馈是**卡**的事（卡自己刷**卡片那一档**的悬停，见 `viewFollowCard`），
// 不是行的事。
//
// # 判据是「两帧一模一样」
//
// 指针停在卡右边的空白上，和指针停在左栏（离内容区远远的），画面必须**一个像素
// 都不差**。比「那个位置是什么颜色」稳：颜色要跟着主题和底色走，而「有没有变」
// 不用。
func TestTheFollowGridRowHasNoHoverSlab(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newUIExplore(t)
			uiFollowTags(b, map[int]any{0: "默认分组"})
			// 只有**一张**卡：那一行右边空出来一大块，灰板就露在那里。
			uiFollowList(b, 1, []any{uiFollowUser(11, "一个人")})
			uiFollowSearch(b, "", nil)
			uiFollowTag(b, 0, []any{})

			s, tt := shotSpaceShell(t, b, dark, true, "77")
			s.navigate(followPath)
			tt.Frame()
			f := s.followPageOf()
			waitFrames(t, tt, "人到了", func() bool { return len(f.users) == 1 && f.tagStarted })
			homeFrame(tt)

			av, ok := tt.Find(f.users[0].Face)
			if !ok {
				t.Fatalf("卡上的头像没画出来，文字有 %q", tt.Texts())
			}

			// ① 指针停在卡右边那块空白上（卡右边还留着那圈影，所以再往右点）。
			tt.Move(av.X+av.W+140, av.Y+av.H/2)
			homeFrame(tt)
			hovered := tt.Image()

			// ② 指针停在左栏（离内容区远远的）。
			tt.Move(250, 600)
			homeFrame(tt)
			away := tt.Image()

			// 卡右边那一整块必须一模一样。
			for y := int(av.Y); y < int(av.Y+av.H); y++ {
				for x := int(av.X + av.W); x < hovered.Bounds().Dx(); x++ {
					if hovered.At(x, y) != away.At(x, y) {
						t.Fatalf("指针停在卡右边的空白上，那一块就变了（%d,%d：%v → %v）——"+
							"框架那一层行悬停底漏出来了", x, y, away.At(x, y), hovered.At(x, y))
					}
				}
			}
		})
	}
}

// TestTheFollowSentinelRowHasNoHoverSlab 钉住末尾那一行（哨兵）也一样。
//
// ⚠️ **哨兵那一行也是布局**：它装的是一句「继续往下滚」或者「加载失败：…」，
// 而框架那一层行悬停底会铺满整行——上游那一块是纯白。
//
// # 怎么让哨兵那一行**停下来**
//
// 第 1 页给 1 个人而总数写 2，于是「还有更多」、末尾有哨兵；第 2 页**失败**，
// 于是 `f.pager.err` 有值、闸门关上（`upPager.want` 那一段），哨兵停在
// 「加载失败：…」等重试，不再自己拉——画面于是是稳的，可以逐像素比。
func TestTheFollowSentinelRowHasNoHoverSlab(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newUIExplore(t)
			uiFollowTags(b, map[int]any{0: "默认分组"})
			b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("pn") == "1" {
					uiExploreJSON(w, 0, map[string]any{"total": 2,
						"list": []any{uiFollowUser(11, "一个人")}})
					return
				}
				uiExploreJSON(w, -101, nil)
			})
			uiFollowSearch(b, "", nil)
			uiFollowTag(b, 0, []any{})

			s, tt := shotSpaceShell(t, b, dark, true, "77")
			s.navigate(followPath)
			tt.Frame()
			f := s.followPageOf()
			waitFrames(t, tt, "第二页拉失败了", func() bool { return f.pager.err != nil })
			homeFrame(tt)

			btn, ok := tt.Find("重试加载关注的人")
			if !ok {
				t.Fatalf("哨兵那一行没画出来，文字有 %q", tt.Texts())
			}

			// ① 指针停在哨兵那一行靠右的空白上（那一行居中摆一句，两边是空的）。
			tt.Move(1100, btn.Y+btn.H/2)
			homeFrame(tt)
			hovered := tt.Image()

			// ② 指针停在左栏。
			tt.Move(250, 600)
			homeFrame(tt)
			away := tt.Image()

			// 哨兵那一行的右半边必须一模一样。
			for y := int(btn.Y) - 6; y < int(btn.Y+btn.H)+6; y++ {
				for x := 900; x < hovered.Bounds().Dx(); x++ {
					if hovered.At(x, y) != away.At(x, y) {
						t.Fatalf("指针停在哨兵那一行上，那一块就变了（%d,%d：%v → %v）——"+
							"框架那一层行悬停底漏出来了", x, y, away.At(x, y), hovered.At(x, y))
					}
				}
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 那张卡看得出是一张卡（票 #244）
// ─────────────────────────────────────────────────────────────────────────────

// TestTheFollowCardSitsOnItsOwnBackground 钉住「我的关注」右栏那些 UP 主
// **看得出是一张一张的方块小卡片**。
//
// # 三条判据
//
//  1. **卡片底 ≠ 页面底**：卡片底走**卡片那一档**（`Shell.cardBackground`）——
//     那一档和页面底差得开，那一片才看得出是一张一张的卡。原来那张卡取的是
//     `t.Background`（和页面底同一个底），于是整片 UP 主看着是**一整块**。
//  2. **悬停那一档从卡片自己的底出发**：常态和悬停在**同一条色阶**上（悬停是
//     卡片底往文字色推一档），不是跳到控件那一层去。
//  3. **推的量是「一档」**：对着上游 `content1 → content2`（zinc 的一格）——
//     推成两档就跳得太远了。
//
// # 为什么要量像素
//
// 卡片底是**画出来**的，代码里写的那个色和画面上那个色中间隔着主题的装配
// （方案 / 深浅 / 用户挑的底色）。量像素量的是**画面上真正画出来的那个色**。
//
// ⚠️ **量的是头像左边那一小块**：卡里的内容是**居中**的（`AlignItems(Center)`），
// 所以头像左边那一块除了底色什么也没有——而头像的**圆**在那一列上，圆外的那
// 一圈还是卡片的底。
func TestTheFollowCardSitsOnItsOwnBackground(t *testing.T) {
	for _, preset := range []app.ThemePreset{app.ThemePresetClassic, app.ThemePresetWarmPaper} {
		for _, dark := range []bool{false, true} {
			shade := "浅色"
			if dark {
				shade = "深色"
			}
			t.Run(string(preset)+"-"+shade, func(t *testing.T) {
				st := app.DefaultSettings("")
				st.ThemePreset = preset
				st.ThemeMode = app.ThemeModeLight
				if dark {
					st.ThemeMode = app.ThemeModeDark
				}
				b := newUIExplore(t)
				uiFollowTags(b, map[int]any{0: "默认分组"})
				uiFollowList(b, 2, []any{
					uiFollowUser(11, "第一个 UP"), uiFollowUser(12, "第二个 UP"),
				})
				uiFollowSearch(b, "", nil)
				uiFollowTag(b, 0, []any{})

				s, tt, _ := newSpaceShell(t, b, true, "77", withSettings(st))
				s.navigate(followPath)
				tt.Frame()
				f := s.followPageOf()
				waitFrames(t, tt, "人到了", func() bool { return len(f.users) == 2 && f.tagStarted })
				tt.Frame()

				th := s.theme.base
				av, ok := tt.Find(f.users[1].Face)
				if !ok {
					t.Fatalf("第二张卡的头像没画出来，文字有 %q", tt.Texts())
				}
				px, py := int(av.X-20), int(av.Y+av.H/2)
				resting := tt.Image().RGBAAt(px, py)

				// ① 卡片底 = 卡片那一档，且 ≠ 页面底。
				if want := toRGBA(s.cardBackground(th)); resting != want {
					t.Fatalf("卡片的底色是 %v，该是卡片那一档 %v", resting, want)
				}
				if resting == toRGBA(th.Background) {
					t.Fatalf("卡片的底色和页面底色一样（%v）——整片 UP 主会看着连成一块", resting)
				}

				// ② 悬停那一档：看得见、不是控件那一层、是从卡片底推的。
				tt.Move(av.X+av.W/2, av.Y+av.H/2)
				tt.Frame()
				hovered := tt.Image().RGBAAt(px, py)
				if hovered == resting {
					t.Fatalf("指针压在卡上，底色没变（%v）——悬停看不出来", hovered)
				}
				if hovered == toRGBA(th.Surface) {
					t.Errorf("悬停那一档是控件那一层 %v——常态和悬停不在同一条色阶上", hovered)
				}

				// ③ 推的量是「一档」：离卡片底近，离文字色还远（上游 content1 →
				// content2 是 zinc 的一格，也就是百分之几）。
				step := colorGap(hovered, resting)
				full := colorGap(toRGBA(th.Text), resting)
				if full == 0 {
					t.Fatalf("卡片底和文字色一个色（%v）", resting)
				}
				if step*12 > full {
					t.Errorf("悬停那一档推了 %v（离卡片底），而卡片底到文字色一共 %v——"+
						"推得比上游 content1 → content2 那一档远", step, full)
				}
			})
		}
	}
}

// colorGap 是两个颜色差多少（三个通道的平均差）。
//
// ⚠️ **不用亮度差**：亮度差会把「差一点点」和「差很多」混在一起（深色下尤
// 其如此），而这一条要判的是「有没有推一档」。
func colorGap(a, b color.RGBA) int {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return (d(a.R, b.R) + d(a.G, b.G) + d(a.B, b.B)) / 3
}
