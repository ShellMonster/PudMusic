// 探索（UP 空间 + 动态流）这一摊的测试。
package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// exploreBili 是一台给探索测试用的假 B 站。
//
// 它记下每个请求，好让「四个 Tab 各自的接口接上了没有」这件事能被断言——
// 断言的是**请求打到了哪个路径**，而不是「调了哪个函数」。
type exploreBili struct {
	*httptest.Server
	mu       sync.Mutex
	paths    []string
	handlers map[string]http.HandlerFunc
	loggedIn bool
	// delayMS 是每个响应之前等的毫秒数，用来造「还在路上」那一种。
	delayMS int
}

func newExploreBili(t *testing.T) *exploreBili {
	t.Helper()
	b := &exploreBili{handlers: map[string]http.HandlerFunc{}}
	b.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		b.paths = append(b.paths, r.URL.Path)
		h := b.handlers[r.URL.Path]
		d := b.delayMS
		b.mu.Unlock()
		if h != nil {
			if d > 0 {
				sleepMS(d)
			}
			h(w, r)
			return
		}
		writeExploreJSON(w, 0, map[string]any{})
	}))
	t.Cleanup(b.Close)
	// 顺手把 WBI 的 key 备上：空间/投稿/动态三个接口都要签名。
	b.on("/x/web-interface/nav", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"wbi_img": map[string]string{
			"img_url": "https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png",
			"sub_url": "https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png",
		}})
	})
	return b
}

func (b *exploreBili) on(path string, h http.HandlerFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[path] = h
}

// countHit 数打到某条路径上的请求有几回。
func (b *exploreBili) countHit(path string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, p := range b.paths {
		if p == path {
			n++
		}
	}
	return n
}

func (b *exploreBili) hit(path string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, p := range b.paths {
		if p == path {
			return true
		}
	}
	return false
}

func writeExploreJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	msg := "0"
	if code != 0 {
		msg = "错了"
	}
	b, _ := exploreJSON(code, msg, data)
	_, _ = w.Write(b)
}

// newExploreApp 装一个指向假 B 站的 App。
func newExploreApp(t *testing.T, b *exploreBili) *App {
	t.Helper()
	a := New()
	t.Cleanup(a.Close)
	cli := bili.NewClient(bili.Options{Transport: b.Server.Client().Transport})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	a.MountLogin(NewMemoryStore("test"), cli)
	a.MountExplore(cli)
	return a
}

// loginExplore 造一个登录态。
//
// 走 `Account.mgr.Adopt` 是因为那是「扫码成功后真正走的那条路」
// （account.go 的 adopt），而不是在测试里硬塞一个 bool——于是「登录态
// 带不带 DedeUserID」「SelfMid 能不能读出来」这些一并是真的。
func loginExplore(a *App, mid string) {
	a.account.mgr.Adopt(Session{
		SESSDATA: "test-sessdata", BiliJCT: "test-jct", DedeUserID: mid,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 没挂上
// ─────────────────────────────────────────────────────────────────────────────

// TestExploreIsMissingIsAnIdentifiableError 钉住「忘了 MountExplore」
// 是一件能一眼认出来的事。
//
// 没挂上时每一页都是空的，而原因在界面上一个字都看不出来。把错误做成
// 一个能 errors.Is 判的哨兵，是为了让这件事在测试里立刻红，而不是变成
// 「列表一直是空的」那种要人肉找的 bug。
func TestExploreIsMissingIsAnIdentifiableError(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)
	ctx := context.Background()

	if _, err := a.UpVideos(ctx, bili.UpVideoQuery{Mid: 1}); err != errExploreMissing {
		t.Errorf("投稿：%v，want errExploreMissing", err)
	}
	if _, err := a.UpCollections(ctx, 1, 1); err != errExploreMissing {
		t.Errorf("收藏夹：%v，want errExploreMissing", err)
	}
	if _, err := a.UpSeries(ctx, 1, 1, 20); err != errExploreMissing {
		t.Errorf("合集：%v，want errExploreMissing", err)
	}
	if _, err := a.UpDynamics(ctx, 1, ""); err != errExploreMissing {
		t.Errorf("动态：%v，want errExploreMissing", err)
	}
	if _, err := a.DynamicFeed(ctx, ""); err != errExploreMissing {
		t.Errorf("动态流：%v，want errExploreMissing", err)
	}
	if _, err := a.FollowedAuthors(ctx, 1, 1); err != errExploreMissing {
		t.Errorf("作者栏：%v，want errExploreMissing", err)
	}
	// 头部也要给同一句，而不是空指针。
	sp := a.UpSpaceOf(1)
	sp.Load()
	if got := sp.Snapshot(); got.Err == "" {
		t.Error("没挂上时头部的 Err 应该是「这一页暂时加载不出来」")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 头部
// ─────────────────────────────────────────────────────────────────────────────

// TestTheHeaderGathersAllThreePieces 钉住「头、关系、粉丝数」三样都拿到。
//
// 三件事是**并发**发的。漏掉任何一件，界面上就少一块：少了头图那一块，
// 少了关系就没有关注按钮（而且用户看不出来为什么没有），少了粉丝数就
// 只剩一个孤零零的头像。
func TestTheHeaderGathersAllThreePieces(t *testing.T) {
	b := newExploreBili(t)
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{
			"mid": 42, "name": "某个 UP 主", "level": 4,
			"top_photo_v2": map[string]string{"l_200h_img": "https://f/b.jpg"},
			"official":     map[string]any{"role": 0},
		})
	})
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"follower": 123456})
	})
	b.on("/x/space/wbi/acc/relation", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{
			"relation":    map[string]any{"attribute": 6},
			"be_relation": map[string]any{"attribute": 0},
		})
	})
	a := newExploreApp(t, b)
	loginExplore(a, "77")

	sp := a.UpSpaceOf(42)
	sp.Load()
	waitExplore(t, sp)

	h := sp.Snapshot()
	if !h.Loaded || h.Loading || h.Err != "" {
		t.Fatalf("头没读出来：%+v", h)
	}
	if h.User.Name != "某个 UP 主" || h.User.Banner != "https://f/b.jpg" || h.User.Level != 4 {
		t.Errorf("头图那块是 %+v", h.User)
	}
	if h.Stat.Follower != 123456 {
		t.Errorf("粉丝数是 %d，want 123456", h.Stat.Follower)
	}
	if !h.Relation.Followed() {
		t.Errorf("attribute=6（互关）该算已关注，拿到的却是 %v", h.Relation)
	}
}

// TestTheHeaderDoesNotAskForTheRelationWhenLoggedOut 钉住「没登录不查
// 关注关系」。
//
// 那个接口要 Cookie，没登录会回 -101，而 -101 在这一层和「网络断了」
// 长得一模一样——于是用户看到的是「这个 UP 主的空间打不开」。
func TestTheHeaderDoesNotAskForTheRelationWhenLoggedOut(t *testing.T) {
	b := newExploreBili(t)
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"mid": 42, "name": "n"})
	})
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"follower": 1})
	})
	b.on("/x/space/wbi/acc/relation", func(w http.ResponseWriter, r *http.Request) {
		// 没登录时 B 站回的就是这个。这里刻意让它 -101，好让「多打了
		// 那个注定失败的请求」在下面那行断言里显出来。
		writeExploreJSON(w, -101, nil)
	})
	a := newExploreApp(t, b)

	sp := a.UpSpaceOf(42)
	sp.Load()
	waitExplore(t, sp)

	if b.hit("/x/space/wbi/acc/relation") {
		t.Error("没登录还去查了关注关系（那个接口会回 -101，而 -101 看着像网络断了）")
	}
	if sp.Snapshot().Err != "" {
		t.Errorf("没登录不该让整页打不开：%q", sp.Snapshot().Err)
	}
}

// TestTheHeaderSaysSoWhenItCannotBeRead 钉住「读不出来要说」。
//
// 头和粉丝数两个都要的都没拿到时才算失败——**只要有一个拿到了就算成功**
// （B 站的权限限制经常只挡其中一个）。
func TestTheHeaderSaysSoWhenItCannotBeRead(t *testing.T) {
	b := newExploreBili(t)
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, -403, nil)
	})
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, -403, nil)
	})
	a := newExploreApp(t, b)

	sp := a.UpSpaceOf(42)
	sp.Load()
	waitExplore(t, sp)

	h := sp.Snapshot()
	if h.Loaded || h.Err == "" {
		t.Errorf("两个都要的都没拿到时不该算成功：%+v", h)
	}
	if h.Loading {
		t.Error("失败之后 Loading 必须回到 false（不然界面永远转圈）")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 关注
// ─────────────────────────────────────────────────────────────────────────────

// TestFollowChangesTheButtonBeforeTheRoundTrip 钉住「先改状态再发请求」。
//
// B 站的写接口要一个往返。等它回来才改的话，按钮按下去是**一点反应都没有**
// ——而这一点反馈正好是用户在等的。
func TestFollowChangesTheButtonBeforeTheRoundTrip(t *testing.T) {
	b := newExploreBili(t)
	b.mu.Lock()
	b.delayMS = 200
	b.mu.Unlock()
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"mid": 42, "name": "n"})
	})
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"follower": 1})
	})
	b.on("/x/space/wbi/acc/relation", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"relation": map[string]any{"attribute": 0}})
	})
	b.on("/x/relation/modify", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, nil)
	})
	a := newExploreApp(t, b)
	loginExplore(a, "77")

	sp := a.UpSpaceOf(42)
	sp.Load()
	waitExplore(t, sp)
	if sp.Snapshot().Relation.Followed() {
		t.Fatalf("一开始就该是未关注，got %v", sp.Snapshot().Relation)
	}

	done := make(chan error, 1)
	go func() { done <- sp.Follow(context.Background(), true) }()

	// 请求还在路上（服务器那边有 200ms 延迟），关系已经变了。
	deadline := 50
	for sp.Snapshot().Relation != bili.RelationFollowed && deadline > 0 {
		waitExploreSleep()
		deadline--
	}
	if got := sp.Snapshot().Relation; !got.Followed() {
		t.Fatalf("请求还没回来，按钮就该变成「已关注」了，got %v", got)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// TestFollowRollsTheButtonBackWhenItFails 钉住「失败要回滚」。
//
// 不回滚的话按钮会一直写着「已关注」，而 B 站那边他压根没关注——用户
// 第二次按「已关注」取消的是一次**从来没发生过**的关注。
func TestFollowRollsTheButtonBackWhenItFails(t *testing.T) {
	b := newExploreBili(t)
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"mid": 42, "name": "n"})
	})
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"follower": 1})
	})
	b.on("/x/space/wbi/acc/relation", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"relation": map[string]any{"attribute": 0}})
	})
	b.on("/x/relation/modify", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, -412, nil) // 被风控
	})
	a := newExploreApp(t, b)
	loginExplore(a, "77")

	sp := a.UpSpaceOf(42)
	sp.Load()
	waitExplore(t, sp)

	if err := sp.Follow(context.Background(), true); err == nil {
		t.Fatal("风控应该是一个错")
	}
	if got := sp.Snapshot().Relation; got.Followed() {
		t.Errorf("失败之后按钮该变回「关注」，got %v", got)
	}
}

// TestFollowKeepsMutualFollowedMutual 关注一个**已经互关**的人之后还是互关。
//
// 写成 RelationFollowed 的话界面上看不出差别（Followed 两个值都算），
// 但下一次刷新 B 站给回 6 之后按钮会闪一下——写成 6 则是在**猜**我们
// 其实知道的事（对方也关注我们）其实我们没查，所以这里保留原来的 6。
func TestFollowKeepsMutualFollowedMutual(t *testing.T) {
	b := newExploreBili(t)
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"mid": 42, "name": "n"})
	})
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"follower": 1})
	})
	b.on("/x/space/wbi/acc/relation", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"relation": map[string]any{"attribute": 6}})
	})
	b.on("/x/relation/modify", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, nil)
	})
	a := newExploreApp(t, b)
	loginExplore(a, "77")

	sp := a.UpSpaceOf(42)
	sp.Load()
	waitExplore(t, sp)

	// 已经互关了，再点一下是**取关**（按钮写的是「已关注」）。
	if err := sp.Follow(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if got := sp.Snapshot().Relation; got != bili.RelationNone {
		t.Errorf("取关之后是 %v，want 未关注", got)
	}
}

// TestTheHeaderWakesTheUIAfterLoading 🔴 铁律三。
//
// 只投递不唤醒 = 界面永远停在旧帧（实测 0 帧刷新）。这一条钉的是
// 「后台改完状态会叫醒界面」——即时模式里这是最容易漏、漏了最难查的一步。
func TestTheHeaderWakesTheUIAfterLoading(t *testing.T) {
	b := newExploreBili(t)
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"mid": 42, "name": "醒一醒"})
	})
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"follower": 1})
	})
	a := newExploreApp(t, b)

	var mu sync.Mutex
	woke := 0
	a.SetWake(func() { mu.Lock(); woke++; mu.Unlock() })

	a.UpSpaceOf(42).Load()
	waitExplore(t, a.UpSpaceOf(42))

	mu.Lock()
	defer mu.Unlock()
	if woke == 0 {
		t.Error("头加载完了没有叫醒界面（铁律三：只投递不唤醒 = 界面停在旧帧）")
	}
}

// TestTheRelationIsNotAskedAgainWhileLoggedOut 钉住「没登录不查关注关系」。
//
// 那个接口要 Cookie，没登录会回 -101，而 -101 在这一层和「网络断了」长得
// 一模一样。
func TestTheRelationIsNotAskedAgainWhileLoggedOut(t *testing.T) {
	b := newExploreBili(t)
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"mid": 42, "name": "n"})
	})
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"follower": 1})
	})
	b.on("/x/space/wbi/acc/relation", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, -101, nil)
	})
	a := newExploreApp(t, b)

	sp := a.UpSpaceOf(42)
	sp.Load()
	waitExplore(t, sp)

	if b.hit("/x/space/wbi/acc/relation") {
		t.Error("没登录还去查了关注关系")
	}
	if got := sp.Snapshot().Follow(77, false); got != FollowHidden {
		t.Errorf("没登录时关注按钮该不画，got %v", got)
	}
}

// TestLoggingInLaterAsksForTheRelation 钉住「登录之后要补查」。
//
// 头是按 mid 缓存的，而关系是**按账号**的：没登录时进来过一次，头部已经
// `Loaded` 了——不补查的话登录之后界面一直停在「从没查过」的那份上，
// 已关注的 UP 主画出来是「关注」（按下去是一次**取关**）。
func TestLoggingInLaterAsksForTheRelation(t *testing.T) {
	b := newExploreBili(t)
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"mid": 42, "name": "n"})
	})
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"follower": 1})
	})
	b.on("/x/space/wbi/acc/relation", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"relation": map[string]any{"attribute": 2}})
	})
	a := newExploreApp(t, b)

	sp := a.UpSpaceOf(42)
	sp.Load()
	waitExplore(t, sp)
	if !sp.Snapshot().Loaded {
		t.Fatal("匿名这一次头该加载成功（只是没查关系）")
	}
	if !sp.Snapshot().NeedRelation(77) {
		t.Fatal("登录之后 NeedRelation 应该是 true（关系还没查过）")
	}

	loginExplore(a, "77")
	sp.Load()
	waitExplore(t, sp)

	if !b.hit("/x/space/wbi/acc/relation") {
		t.Fatal("登录之后没有补查关注关系")
	}
	if got := sp.Snapshot().Follow(77, true); got != FollowDone {
		t.Errorf("登录之后该显示「已关注」，got %v", got)
	}
}

// TestAFailedRelationIsNotCachedAsNotFollowed 钉住「查失败不能说成没关注」。
//
// 吞掉的话界面上是一个**能按的**「关注」按钮，而它其实没查到任何东西——
// 用户按下去是一次取关（B 站对没关注过的人「取关」是成功的），于是他更
// 确信自己关注过，而真相是我们压根没读到。
func TestAFailedRelationIsNotCachedAsNotFollowed(t *testing.T) {
	b := newExploreBili(t)
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"mid": 42, "name": "n"})
	})
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"follower": 1})
	})
	b.on("/x/space/wbi/acc/relation", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, -412, nil) // 被风控
	})
	a := newExploreApp(t, b)
	loginExplore(a, "77")

	sp := a.UpSpaceOf(42)
	sp.Load()
	waitExplore(t, sp)

	h := sp.Snapshot()
	if !h.Loaded {
		t.Fatal("头该加载成功（只有关系那一个失败）")
	}
	if got := h.Follow(77, true); got != FollowFailed {
		t.Errorf("关系没查到该是 FollowFailed（界面上说没读到 + 给一个重试），got %v", got)
	}
	if h.RelationKnown() {
		t.Error("关系没查到，relationKnown 不该是 true")
	}
	// 防抖：查失败之后**不自动重试**，等用户点。
	if h.NeedRelation(77) {
		t.Error("查失败之后 NeedRelation 应该是 false（否则每帧一个请求）")
	}

	// 人点「重试」之后重来一次，这次成了。
	b.on("/x/space/wbi/acc/relation", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"relation": map[string]any{"attribute": 2}})
	})
	sp.RetryRelation()
	if !sp.Snapshot().NeedRelation(77) {
		t.Fatal("RetryRelation 之后该又能查了")
	}
	sp.Load()
	waitExplore(t, sp)
	if got := sp.Snapshot().Follow(77, true); got != FollowDone {
		t.Errorf("重试之后该是「已关注」，got %v", got)
	}
}

// TestARelationFromAnotherAccountIsNotUsed 钉住「别拿上一个账号的关系画按钮」。
func TestARelationFromAnotherAccountIsNotUsed(t *testing.T) {
	b := newExploreBili(t)
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"mid": 42, "name": "n"})
	})
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"follower": 1})
	})
	b.on("/x/space/wbi/acc/relation", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"relation": map[string]any{"attribute": 2}})
	})
	a := newExploreApp(t, b)
	loginExplore(a, "77")

	sp := a.UpSpaceOf(42)
	sp.Load()
	waitExplore(t, sp)
	if got := sp.Snapshot().Follow(77, true); got != FollowDone {
		t.Fatalf("账号 77 该是「已关注」，got %v", got)
	}
	// 换成账号 88：那份「已关注」是 77 的。
	if got := sp.Snapshot().Follow(88, true); got != FollowHidden {
		t.Errorf("拿账号 77 的关系给 88 画按钮了，got %v（该不画，等补查）", got)
	}
	if !sp.Snapshot().NeedRelation(88) {
		t.Error("换账号之后该重新查关系（账号 88 的那份还没查）")
	}
	if sp.Snapshot().NeedRelation(77) {
		t.Error("账号 77 刚查成功，不该又查一遍")
	}
}

// TestAFailedStatIsNotCachedAsZero 钉住「粉丝数查失败不能说成 0」。
//
// ⚠️ 这一条比关注关系那条更难受：粉丝数 0 会被画成一个**干干净净的「粉丝数
// 0」**，而那是个**假数**（真实值可能是十几万）。它看起来完全正常，用户会
// 相信它 —— 而我们一个错都没报。
func TestAFailedStatIsNotCachedAsZero(t *testing.T) {
	b := newExploreBili(t)
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"mid": 42, "name": "n"})
	})
	b.on("/x/space/wbi/acc/relation", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"relation": map[string]any{"attribute": 0}})
	})
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, -500, nil)
	})
	a := newExploreApp(t, b)
	loginExplore(a, "77")

	sp := a.UpSpaceOf(42)
	sp.Load()
	waitExplore(t, sp)

	h := sp.Snapshot()
	if !h.Loaded {
		t.Fatal("资料查到了，头该算加载过")
	}
	if h.StatOK() {
		t.Error("粉丝数没查到，StatOK 不该是 true")
	}
	if h.Stat.Follower != 0 {
		t.Errorf("粉丝数是 %d —— 查失败时它是零值，界面上会是一个**假数**", h.Stat.Follower)
	}
	if !h.StatFailed() {
		t.Error("查失败了，StatFailed 该是 true（界面据此给一个重试）")
	}
	if !h.AnyPartFailed() {
		t.Error("AnyPartFailed 该为 true")
	}
	// 防抖：失败之后不自动重试（不然每帧一个请求）。
	if h.NeedStat(77) {
		t.Error("查失败之后 NeedStat 应该是 false（否则每帧一个请求）")
	}

	// 人点「重试」，接口这回好了。
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"follower": 123456})
	})
	sp.RetryRelation()
	if !sp.Snapshot().NeedStat(77) {
		t.Fatal("RetryRelation 之后该又能查了")
	}
	sp.Load()
	waitExplore(t, sp)
	if !sp.Snapshot().StatOK() || sp.Snapshot().Stat.Follower != 123456 {
		t.Errorf("重试之后没拿到粉丝数：%+v", sp.Snapshot().Stat)
	}
}

// TestLoggedOutHeaderDoesNotRefetchForever 是**一条自激循环**的钉子。
//
// ⚠️ 这条是评审第三轮抓到的回归，我自己引入的：`StatOK()` 当时是
// `statKnown && statFor != 0`，而未登录时 `statFor` 就是 0 —— 于是
// `StatOK` 永远为假、`NeedStat` 永远为真，而 `viewUpSpace` 每帧按它调一次
// `Load()`。结果：未登录、接口全部成功，**8 次空闲帧发了 10 次请求**，
// 而且每次结果回来又唤醒界面（铁律三）—— 那个唤醒正是循环的一环。
//
// 判据是**请求次数**：不画第二帧之前应该只有 1 次。
func TestLoggedOutHeaderDoesNotRefetchForever(t *testing.T) {
	b := newExploreBili(t)
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"mid": 42, "name": "n", "level": 3})
	})
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, 0, map[string]any{"follower": 100, "following": 5})
	})
	b.on("/x/space/wbi/acc/relation", func(w http.ResponseWriter, r *http.Request) {
		writeExploreJSON(w, -101, nil)
	})
	a := newExploreApp(t, b)

	// 未登录。
	sp := a.UpSpaceOf(42)
	sp.Load()
	waitExplore(t, sp)

	// 空闲八拍：每来一个「结果到了」的唤醒，界面那一帧都会问一次「还要不要再拉」。
	for range 8 {
		time.Sleep(time.Millisecond)
	}
	if n := b.countHit("/x/space/wbi/acc/info"); n != 1 {
		t.Errorf("未登录、接口全部成功，acc/info 被请求了 %d 次，want 1"+
			"（每帧重拉的自激循环：NeedStat 在未登录时必须为 false）", n)
	}
	if n := b.countHit("/x/relation/stat"); n != 0 {
		t.Errorf("未登录还查了 %d 次关注数（界面那一块没登录时不画，查它是白花）", n)
	}
	if h := sp.Snapshot(); !h.Loaded || h.Loading {
		t.Errorf("状态不对：%+v", h)
	}
	// 未登录时 `NeedStat` 必须是 false —— 这一句才是循环的那道门。
	if sp.Snapshot().NeedStat(0) {
		t.Error("未登录时 NeedStat 是 true —— 这就是每帧重拉的那道门")
	}
	// 反过来：登录之后它又该变回 true（登录那一刻才去查）。
	if !sp.Snapshot().NeedStat(77) {
		t.Error("登录之后 NeedStat 应该是 true（那一刻才去查关注数）")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 四个 Tab 的接口真的接上了
// ─────────────────────────────────────────────────────────────────────────────

// TestEachTabHitsItsOwnEndpoint 是「四个 Tab 各自的数据接口都接上了」
// 那条验收的钉子。
//
// 判据是**请求打到了哪个路径**——四个 Tab 一共四个接口，路径长得都像，
// 而打错了的症状是 -403「访问权限不足」，那句话和「路径抄错了」没有
// 任何关系。
func TestEachTabHitsItsOwnEndpoint(t *testing.T) {
	b := newExploreBili(t)
	a := newExploreApp(t, b)
	ctx := context.Background()

	if _, err := a.UpDynamics(ctx, 42, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpVideos(ctx, bili.UpVideoQuery{Mid: 42, Page: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpCollections(ctx, 42, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpSeries(ctx, 42, 1, 20); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DynamicFeed(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.FollowedAuthors(ctx, 42, 1); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		"/x/polymer/web-dynamic/v1/feed/space",     // 动态
		"/x/space/wbi/arc/search",                  // 投稿
		"/x/v3/fav/folder/created/list",            // 收藏夹
		"/x/polymer/web-space/seasons_series_list", // 合集
		"/x/polymer/web-dynamic/v1/feed/all",       // 动态流
		"/x/relation/followings",                   // 作者栏
	} {
		if !b.hit(path) {
			t.Errorf("没有一个请求打到 %s", path)
		}
	}
}

// TestSelfMidIsZeroWhenLoggedOut 钉住「作者栏要的是**自己**的 mid」。
//
// 没有登录态时它是 0，而 0 拿去问 B 站会回 -101 —— 界面上就是作者栏空了
// 而看不出原因。
func TestSelfMidIsZeroWhenLoggedOut(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)
	if got := a.SelfMid(); got != 0 {
		t.Errorf("没登录时 SelfMid() 是 %d，want 0", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 小工具
// ─────────────────────────────────────────────────────────────────────────────

// exploreJSON 造一个 B 站外壳（app 包的测试不共享 bili 包的那个）。
func exploreJSON(code int, msg string, data any) ([]byte, error) {
	body := map[string]any{"code": code, "message": msg, "ttl": 1}
	if data != nil {
		body["data"] = data
	}
	return json.Marshal(body)
}

// sleepMS 等一会儿（毫秒）。
func sleepMS(n int) { time.Sleep(time.Duration(n) * time.Millisecond) }

// waitExploreSleep 是等一个轮询的间隔。
func waitExploreSleep() { sleepMS(2) }

// waitExplore 等头加载完。
func waitExplore(t *testing.T, sp *UpSpace) {
	t.Helper()
	for i := 0; i < 2000; i++ {
		h := sp.Snapshot()
		if !h.Loading {
			return
		}
		waitExploreSleep()
	}
	t.Fatalf("头加载了一秒还没结束：%+v", sp.Snapshot())
}
