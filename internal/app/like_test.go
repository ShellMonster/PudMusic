package app

// 赞 / 藏状态这一摊的测试。
//
// 三件事各自要钉住：
//
//	问        换歌就重问，迟到的结果不许落到新歌上
//	赞        乐观（本地先改）→ 失败改回去
//	三连      赞和藏**都**按 B 站的答案更新

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// likeServer 是一台会答「赞没赞」和「三连」的假 B 站。
//
// liked/faved 是它报的关系；tripleBody 是三连那一趟的回包（可以自己改成
// 「B 站只做成了一部分」）。
type likeServer struct {
	*httptest.Server
	relBody    string
	tripleBody string
	likeBody   string

	mu   sync.Mutex
	seen []string
}

func newLikeServer(t *testing.T) *likeServer {
	t.Helper()
	s := &likeServer{
		relBody:    `{"code":0,"message":"0","ttl":1,"data":{"like":false,"favorite":false}}`,
		tripleBody: `{"code":0,"message":"0","ttl":1,"data":{"like":true,"coin":true,"fav":true,"multiply":1}}`,
		likeBody:   `{"code":0,"message":"0","ttl":1}`,
	}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		s.mu.Lock()
		s.seen = append(s.seen, r.URL.Path)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/x/web-interface/archive/relation":
			_, _ = w.Write([]byte(s.relBody))
		case "/x/web-interface/archive/like":
			_, _ = w.Write([]byte(s.likeBody))
		case "/x/web-interface/archive/like/triple":
			_, _ = w.Write([]byte(s.tripleBody))
		case "/x/web-interface/view": // 三连要 bvid → avid
			_, _ = w.Write([]byte(`{"code":0,"message":"0","ttl":1,"data":{"aid":42}}`))
		default:
			_, _ = w.Write([]byte(`{"code":-404,"message":"没有这个接口","ttl":1}`))
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// likeAppOn 建一个挂着这台假 B 站的 App。
func likeAppOn(t *testing.T, s *likeServer) *App {
	t.Helper()
	a := New()
	t.Cleanup(a.Close)
	cli := bili.NewClient(bili.Options{
		Transport: s.Server.Client().Transport,
		CSRF:      func() string { return "the-jct" },
	})
	cli.TestOnlyPointHostsAtFixture(s.URL)
	a.MountStreaming(Streaming{Client: cli, Media: s.Server.Client(), TempDir: t.TempDir()})
	return a
}

// putCurrent 把队列当前项摆成某一首（**不真的放**）：这些用例要的是「当前是哪一首」，
// 而 `play` 会去真的取流，那不是这里要验的东西。
func putCurrent(a *App, bvid string) {
	a.Queue().Append(Item{Source: "online", Type: "mv", Bvid: bvid, Cid: "1", Title: "一首"})
	a.queue.selectAt(len(a.Queue().Items()) - 1)
}

// calls 是打给某一条路径的请求数。
func (s *likeServer) calls(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, p := range s.seen {
		if p == path {
			n++
		}
	}
	return n
}

// askedBvid 记下问过的 bvid（按顺序）。
//
// ⚠️ 它是被后台那个 goroutine 写的，所以带锁（`-race` 会报）。
type askedBvid struct {
	mu  sync.Mutex
	ids []string
}

func (a *askedBvid) record(_ context.Context, bvid string) (bili.ArchiveRelation, error) {
	a.mu.Lock()
	a.ids = append(a.ids, bvid)
	a.mu.Unlock()
	return bili.ArchiveRelation{}, nil
}

func (a *askedBvid) len() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.ids)
}

func (a *askedBvid) all() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.ids...)
}

// waitLike 轮询到 cond 成立（后台那一趟是 goroutine）。
func waitLike(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等了 2 秒，%s 还没发生", what)
}

// ---- 问 ----

// TestTheLikeStateIsAskedPerSong 钉住「赞没赞是**这一首**的事，换歌就重问」。
//
// 症状很好认：一首歌赞过了，换到下一首，星标还是亮的——于是那一首被「再赞一次」
// 或者长按三连被开着（那一次要花两枚币）。
func TestTheLikeStateIsAskedPerSong(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	asked := &askedBvid{}
	a.likeFetch = asked.record
	putCurrent(a, "BVfirst")
	a.LikeState()
	waitLike(t, "第一首问过了", func() bool { return asked.len() == 1 })

	putCurrent(a, "BVsecond")
	a.LikeState()
	waitLike(t, "第二首也问了", func() bool { return asked.len() == 2 })
	if got := asked.all(); got[0] != "BVfirst" || got[1] != "BVsecond" {
		t.Errorf("问的是 %q，want 先 BVfirst 后 BVsecond", got)
	}
}

// TestALateAnswerFromTheOldSongNeverLands 钉住代次闸门。
//
// 换歌那一瞬最容易出的错：上一首那一趟还在路上，回来的时候按「上一首」把答案
// 写进缓存——于是「我在听 B，界面上却是 A 的赞没赞」。
//
// ⚠️ **等的是唤醒，不是 sleep**：`finishLike` 落地之后第一件事就是叫醒界面，
// 所以「叫醒过 2 次」正是「两趟都处理完了」。直接 sleep 一段的话这条要么是
// 靠运气过的，要么是靠运气红的。
func TestALateAnswerFromTheOldSongNeverLands(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	gate := make(chan struct{})
	asked := &askedBvid{}
	a.likeFetch = func(ctx context.Context, bvid string) (bili.ArchiveRelation, error) {
		// 先记下「问过了」（不记的话，测试没法知道那一趟已经出发），
		// 再把第一首卡住。
		rec, _ := asked.record(ctx, bvid)
		if bvid == "BVfirst" {
			rec.Liked = true
			<-gate
		}
		return rec, nil
	}
	var wakes atomic.Int32
	a.SetWake(func() { wakes.Add(1) })

	putCurrent(a, "BVfirst")
	a.LikeState() // 第一趟，卡着
	waitLike(t, "第一趟发出去了", func() bool { return asked.len() == 1 })

	putCurrent(a, "BVsecond")
	a.LikeState() // 第二趟，立刻回来
	waitLike(t, "第二首问过了", a.LikeAsked)

	close(gate) // 上一首那一趟这才回来
	waitLike(t, "迟到那一趟也落地了", func() bool { return wakes.Load() >= 2 })

	if a.LikeState().Liked {
		t.Error("界面上是第二首，赞却是上一首那一趟给的 —— 迟到那一趟必须被丢掉")
	}
}

// TestALocalSongIsNotLikeable 钉住「本地文件不摆星标」。
//
// B 站那三个接口一个都不收本地文件（它压根没有投稿），而摆一个点了没反应的键
// 比不摆更让人以为它坏了。音乐库条目（`type: "audio"`）同理——那边给的是 sid。
func TestALocalSongIsNotLikeable(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	a.Queue().Append(Item{Source: "local", Type: "local", Path: "/tmp/本地的一首.flac", Title: "本地的"})
	a.queue.selectAt(0)

	if a.Likeable() {
		t.Error("本地文件被判成可赞了")
	}
	if rel := a.LikeState(); rel.Liked || rel.Favorited {
		t.Errorf("本地文件读出了关系 %+v", rel)
	}
	if !a.LikeAsked() {
		t.Error("不可赞的那一种被判成「还没问」——界面会用它把长按一直关着")
	}

	var asked atomic.Int32
	a.likeFetch = func(context.Context, string) (bili.ArchiveRelation, error) {
		asked.Add(1)
		return bili.ArchiveRelation{}, nil
	}
	for range 5 {
		a.LikeState()
	}
	if got := asked.Load(); got != 0 {
		t.Errorf("不可赞的那一种还发了 %d 趟请求", got)
	}
}

// ---- 赞 ----

// TestSetLikeFlipsTheStarBeforeTheServerAnswers 钉住乐观那一半。
//
// 这一半是**看得见的**：点一下星标，那一帧就该亮。不等网络回来的话一次往返
// 几百毫秒，用户会以为没按到，于是按第二次——两次请求，一次赞一次取消赞。
func TestSetLikeFlipsTheStarBeforeTheServerAnswers(t *testing.T) {
	s := newLikeServer(t)
	s.relBody = `{"code":0,"message":"0","ttl":1,"data":{"like":false,"favorite":false}}`
	a := likeAppOn(t, s)
	putCurrent(a, "BV1")

	// 先问过，于是缓存属于这一首（乐观那一支改的就是它）。
	a.LikeState()
	waitLike(t, "问过了", a.LikeAsked)

	var done error
	have := make(chan struct{})
	a.SetLike(true, func(err error) { done = err; close(have) })

	// 还没等网络：星标已经亮了。
	if !a.LikeState().Liked {
		t.Error("刚点下去星标还是暗的 —— 界面那一帧要等网络回来才变")
	}
	<-have
	if done != nil {
		t.Fatalf("赞失败：%v", done)
	}
	if got := s.calls("/x/web-interface/archive/like"); got != 1 {
		t.Errorf("赞发了 %d 次请求，want 1", got)
	}
}

// TestAFailedLikePutsTheStarBack 钉住失败那一半。
//
// B 站说没登录的时候，界面上不能留着「已赞」——那是一句对用户撒谎的话（用户
// 会以为赞过了），而且下一次点它会发「取消赞」。
func TestAFailedLikePutsTheStarBack(t *testing.T) {
	s := newLikeServer(t)
	s.likeBody = `{"code":-101,"message":"账号未登录","ttl":1}`
	a := likeAppOn(t, s)
	putCurrent(a, "BV1")
	a.LikeState()
	waitLike(t, "问过了", a.LikeAsked)

	have := make(chan error, 1)
	a.SetLike(true, func(err error) { have <- err })

	err := <-have
	if err == nil {
		t.Fatal("B 站说没登录，界面上却说赞成了")
	}
	if a.LikeState().Liked {
		t.Error("赞失败了，星标还亮着 —— 那是一句对用户撒谎的话")
	}
}

// TestAFailedLikeOfTheNextSongDoesNotStealTheAnswer 钉住回滚的那一半。
//
// 失败的那几百毫秒里用户换了歌：那时候把上一首的答案写回新一首的缓存上，界面上
// 就是「我在听 B，A 的赞没赞状态糊在 B 上」。
func TestAFailedLikeOfTheNextSongDoesNotStealTheAnswer(t *testing.T) {
	s := newLikeServer(t)
	s.likeBody = `{"code":-101,"message":"账号未登录","ttl":1}`
	a := likeAppOn(t, s)
	putCurrent(a, "BVfirst")
	a.LikeState()
	waitLike(t, "第一首问过了", a.LikeAsked)

	have := make(chan error, 1)
	a.SetLike(true, func(err error) { have <- err })

	// 趁它在路上换歌。
	putCurrent(a, "BVsecond")
	a.LikeState()
	waitLike(t, "第二首问过了", a.LikeAsked)

	<-have
	if a.LikeState().Liked {
		t.Error("上一首失败的回滚把答案写到了新一首上")
	}
}

// ---- 三连 ----

// TestTheTripleUpdatesTheLikeAndTheFavorite 钉住「三连成功后收藏状态同步更新」。
//
// B 站那一趟顺手收藏了这一首，所以赞和藏**都**要按它的答案变。丢掉 `fav` 那一栏
// 的症状是：用户点开收藏夹发现没有这一首，而界面上写着收藏了。
func TestTheTripleUpdatesTheLikeAndTheFavorite(t *testing.T) {
	s := newLikeServer(t)
	a := likeAppOn(t, s)
	putCurrent(a, "BV1")
	a.LikeState()
	waitLike(t, "问过了", a.LikeAsked)

	have := make(chan error, 1)
	a.TripleLike(func(_ bili.TripleResult, err error) { have <- err })
	if err := <-have; err != nil {
		t.Fatalf("三连失败：%v", err)
	}

	rel := a.LikeState()
	if !rel.Liked {
		t.Error("三连之后赞那一栏没亮")
	}
	if !rel.Favorited {
		t.Error("三连之后收藏那一栏没跟着更新 —— B 站那一趟是收藏了的")
	}
	if !a.LikeAsked() {
		t.Error("三连之后被判成「还没问」——长按于是又开着了")
	}
}

// TestTheTripleTellsTheTruthWhenBiliOnlyDidHalf 钉住「按 B 站的答案写」。
//
// 造一份「赞成功、投币被拒」的回包：界面上赞亮、收藏**不**亮。而三样都点亮的
// 版本会骗用户说「投出去了」——用户手上没有那一枚币。
func TestTheTripleTellsTheTruthWhenBiliOnlyDidHalf(t *testing.T) {
	s := newLikeServer(t)
	s.tripleBody = `{"code":0,"message":"0","ttl":1,"data":{"like":true,"coin":false,"fav":false,"multiply":1}}`
	a := likeAppOn(t, s)
	putCurrent(a, "BV1")
	a.LikeState()
	waitLike(t, "问过了", a.LikeAsked)

	have := make(chan error, 1)
	a.TripleLike(func(_ bili.TripleResult, err error) { have <- err })
	if err := <-have; err != nil {
		t.Fatalf("三连失败：%v", err)
	}

	rel := a.LikeState()
	if !rel.Liked {
		t.Error("B 站说赞成了，星标却没亮")
	}
	if rel.Favorited {
		t.Error("B 站说没收藏，界面却说收藏了")
	}
}

// TestTheTripleOnAFailureChangesNothing 钉住「失败时三样都不动」。
//
// 用户手上什么都没有，而界面上因为一次失败多出一个赞，是凭空造出来的。
func TestTheTripleOnAFailureChangesNothing(t *testing.T) {
	s := newLikeServer(t)
	s.tripleBody = `{"code":-101,"message":"账号未登录","ttl":1}`
	a := likeAppOn(t, s)
	putCurrent(a, "BV1")
	a.LikeState()
	waitLike(t, "问过了", a.LikeAsked)

	have := make(chan error, 1)
	a.TripleLike(func(_ bili.TripleResult, err error) { have <- err })
	if err := <-have; err == nil {
		t.Fatal("B 站说没登录，界面上却说三连成功了")
	}
	if rel := a.LikeState(); rel.Liked || rel.Favorited {
		t.Errorf("三连失败了，状态却变成了 %+v", rel)
	}
}

// TestTheTripleIsSentOnlyForASongThatCanTakeIt 钉住「本地文件发不出去」。
//
// 那一趟要是真发出去，B 站要么回参数错误、要么（在字段错的情况下）给别的稿件
// 记上一笔。宁可**在界面上**就说不行，也不要打一个注定失败的请求。
func TestTheTripleIsSentOnlyForASongThatCanTakeIt(t *testing.T) {
	s := newLikeServer(t)
	a := likeAppOn(t, s)
	a.Queue().Append(Item{Source: "local", Type: "local", Path: "/tmp/x.flac", Title: "本地的"})
	a.queue.selectAt(0)

	have := make(chan error, 1)
	a.TripleLike(func(_ bili.TripleResult, err error) { have <- err })
	if err := <-have; err == nil {
		t.Error("本地文件三连居然没报错")
	}
	if got := s.calls("/x/web-interface/archive/like/triple"); got != 0 {
		t.Errorf("本地文件也发了三连请求（%d 次）", got)
	}
}

// TestTheLikeIsAskedAgainAfterAFailure 钉住「问失败之后还能再问」。
//
// 和分集是同一条：失败也把「问过了」置上的话，一次网络抖动之后这一首在本次播放
// 期间永远不再问——于是长按三连**一直关着**（`LikeAsked` 恒 false），用户看到的是
// 「长按封面没反应」。
func TestTheLikeIsAskedAgainAfterAFailure(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	var n atomic.Int32
	a.likeFetch = func(context.Context, string) (bili.ArchiveRelation, error) {
		n.Add(1)
		return bili.ArchiveRelation{}, errors.New("风控")
	}
	putCurrent(a, "BV1")

	a.LikeState()
	waitLike(t, "第一趟", func() bool { return n.Load() == 1 })
	if a.LikeErr() == nil {
		t.Error("问失败了却没记下原因")
	}
	if a.LikeAsked() {
		t.Error("问失败了却被记成「问过了」")
	}

	// 退避之内：不发。
	for range 5 {
		a.LikeState()
	}
	if got := n.Load(); got != 1 {
		t.Fatalf("退避之内又发了 %d 趟，want 1", got)
	}

	// 把「上一次发起」拨到退避窗口之前：下一帧之后会再问一次。
	a.likeMu.Lock()
	a.likeTriedAt = a.likeTriedAt.Add(-2 * likeRetryAfter)
	a.likeMu.Unlock()

	a.LikeState()
	waitLike(t, "退避之后那一趟", func() bool { return n.Load() == 2 })
}
