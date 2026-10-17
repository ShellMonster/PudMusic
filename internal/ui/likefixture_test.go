// 点赞那一票的测试脚手架：一台会答赞 / 三连的假 B 站，加上一个挂着它的界面。
//
// 拆成单独一个文件是因为它已经比任何一个测试文件该有的样子长了（那边那条规矩是
// 800 行），而它本身**没有一条断言**——断言都在 `like_test.go`（手势）和
// `like_star_test.go`（星标）那边。
package ui

import (
	"encoding/json"
	"image"
	"image/draw"
	"net/http"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// ---- 给那台假 B 站补上点赞那三个接口 ----
//
// 路由挂在 `biliFixture.serve` 那边（那是所有 B 站接口的唯一一处），而这里放的是
// 「这一票要的答案」和量它们的几个小工具。

// onLike 装上赞 / 三连那三个接口的答案。
func (f *biliFixture) onLike(relLiked, relFaved, tripleFav bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.relLiked, f.relFaved = relLiked, relFaved
	f.tripleBody = writeBiliLikeJSON(map[string]any{
		"like": true, "coin": true, "fav": tripleFav, "multiply": 1,
	})
}

// setTriple 换掉三连那一趟的回包。
func (f *biliFixture) setTriple(data map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tripleBody = writeBiliLikeJSON(data)
}

// failTriple 把三连那一趟换成失败（没登录）。
func (f *biliFixture) failTriple() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tripleBody = `{"code":-101,"message":"账号未登录","ttl":1}`
}

// gate 卡住某一条路径的响应，直到 release 那条路径。
//
// ⚠️ 用的是 `gates` 那张表（原来只用来卡搜索的），键是路径而不是关键词——
// 那一趟 `serveSearch` 会去表里按关键词取，两边互不相干。
func (f *biliFixture) gate(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gates[path] = make(chan struct{})
}

// release 放掉某一条路径。
func (f *biliFixture) releasePath(path string) {
	f.mu.Lock()
	ch := f.gates[path]
	delete(f.gates, path)
	f.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

func (f *biliFixture) serveArchiveRelation(w http.ResponseWriter) {
	f.mu.Lock()
	liked, faved := f.relLiked, f.relFaved
	ch := f.gates["/x/web-interface/archive/relation"]
	f.mu.Unlock()

	if ch != nil {
		select {
		case <-ch:
		case <-time.After(gateWait):
		}
	}
	_, _ = w.Write([]byte(writeBiliLikeJSON(map[string]any{"like": liked, "favorite": faved})))
}

func (f *biliFixture) serveArchiveLike(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	f.likePosts++
	f.lastUp = r.PostForm.Get("up")
	f.mu.Unlock()
	_, _ = w.Write([]byte(`{"code":0,"message":"0","ttl":1}`))
}

func (f *biliFixture) serveArchiveTriple(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	f.triplePosts++
	body := f.tripleBody
	if body == "" {
		body = `{"code":0,"message":"0","ttl":1,"data":{"like":true,"coin":true,"fav":true,"multiply":1}}`
	}
	f.mu.Unlock()
	_, _ = w.Write([]byte(body))
}

// likePostsCount 是赞被叫了几次。
func (f *biliFixture) likePostsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.likePosts
}

// triplePostCount 是三连被叫了几次。
func (f *biliFixture) triplePostCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.triplePosts
}

// lastLikeUp 是最近一次赞带的 up。
func (f *biliFixture) lastLikeUp() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastUp
}

// writeBiliLikeJSON 把一段 data 包成 B 站那个信封。nil 时给一个失败（没登录）。
func writeBiliLikeJSON(data any) string {
	if data == nil {
		return `{"code":-101,"message":"账号未登录","ttl":1}`
	}
	b, err := json.Marshal(map[string]any{
		"code": 0, "message": "0", "ttl": 1, "data": data,
	})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// Biu 的那几个数，在这里**写死字面量**，不去用 like.go 里的同名常量。
//
// 为什么不用那边的常量：断言和实现都用它的话，常量一改两边一起跟着变，测试就
// 发现不了了——把 2500 改成 2000，期望也跟着变成 2000，照样全绿。而这两个数正是
// 本票要钉死的东西（SPEC 需求 69「长按 2.5 秒」、PLAN 6.10「500ms 起进度环」）。
const (
	biuLikeLongPressMS = 2500 // index.tsx:104 的 setTimeout(…, 2500)
	biuLikeRingDelayMS = 500  // index.tsx:100 的 setTimeout(…, 500)
	biuLikeRingSpanMS  = 2000 // index.css 的 animation: … 2s linear forwards
)

// likeWin 是测试窗口的尺寸（和 newShell 一样）。
const (
	likeWinW = 1200
	likeWinH = 800
)

// likeBvid 是这些用例在放的那一首。
const likeBvid = "BVlikeTest"

// likeEnv 是一台会答赞 / 三连的假 B 站加上一个挂着它的界面。
type likeEnv struct {
	s  *Shell
	tt *mygoui.Tester
	fc *fakeClock
	f  *biliFixture
}

// newLikeEnv 建一个**正在放 B 站 mv** 的界面，**假时钟**已经换上。
//
// relLiked / relFaved 是那台假 B 站报的「赞没赞、收藏没收藏」（换歌时问的那一次）。
// tripleFav 是三连那一趟的回包里「收藏成没成」。
func newLikeEnv(t *testing.T, relLiked, relFaved, tripleFav bool) *likeEnv {
	t.Helper()
	return newLikeEnvSize(t, relLiked, relFaved, tripleFav, likeWinW)
}

// newLikeEnvSize 是 newLikeEnv 加一个窗口宽度。
func newLikeEnvSize(t *testing.T, relLiked, relFaved, tripleFav bool, width int) *likeEnv {
	t.Helper()
	f := newBiliFixture(t, 1)
	f.onLike(relLiked, relFaved, tripleFav)
	return playTheLikeSong(t, f, width, true)
}

// newLikeEnvSlowRelation 是「赞没赞那一问卡着」的界面。
//
// 为什么要有这么一个变体：长按三连是**问过之后才开着的**（`likeArmed` 要
// `LikeAsked`），而「问一直没回来」是最容易让人以为功能坏掉的那种情形。
//
// ⚠️ 门要在**建界面之前**挂上：界面每一帧都在问 App，而 App 建好的那一趟就卡住了。
func newLikeEnvSlowRelation(t *testing.T) *likeEnv {
	t.Helper()
	f := newBiliFixture(t, 1)
	f.onLike(false, false, false)
	f.gate(pathArchiveRelation)
	return playTheLikeSong(t, f, likeWinW, false)
}

// pathArchiveRelation 是「赞没赞」那一问的路径（挂门用）。
const pathArchiveRelation = "/x/web-interface/archive/relation"

// playTheLikeSong 把 likeItem 放起来，挂上那台假 B 站，等它开起来。
//
// waitAsked 是「等不等赞没赞问回来」：卡住的那一种等不到（而用例要的正是那段）。
func playTheLikeSong(t *testing.T, f *biliFixture, width int, waitAsked bool) *likeEnv {
	t.Helper()
	s, tt := newShellWidth(t, width)
	// 🔴 在建 Tester **之前**换上（理由同 fullplayer_test.go 的 fullTester）：
	// Tester 建好就跑了一帧，那一帧已经按真时钟把按下那一刻记成墙钟了，
	// 换晚了 2.5 秒就成了 2.5 秒加一大截。
	fc := newFakeClock(time.Unix(1_700_000_000, 0))
	s.clock = fc
	tt.SetDark(true)

	s.app.MountStreaming(app.Streaming{
		Client: f.client(), Media: f.Client(), TempDir: t.TempDir(),
	})
	if err := s.app.Enqueue(likeItem()); err != nil {
		t.Fatalf("把在线的那首放进队列：%v", err)
	}
	settleSearch(t, tt, "在线那首开始播", func() bool { return s.app.Snapshot().HasTrack })

	// 赞没赞是**每一帧**问 App 要的，而问 App 会触发那一趟。等到问回来为止：
	// 没问回来之前长按是关着的（likeArmed，见 like.go），用例在那一段量到的
	// 东西没有意义。
	if waitAsked {
		settleSearch(t, tt, "赞没赞问回来了", func() bool { return s.app.LikeAsked() })
	} else if s.app.LikeAsked() {
		t.Fatal("赞没赞问回来了 —— 这一条要的是「一直没问回来」那一段")
	}
	tt.Frame()
	return &likeEnv{s: s, tt: tt, fc: fc, f: f}
}

// likeItem 是这些用例放的那一首。
//
// ⚠️ **UP 名给一个很长的**：第二行最坏的情况是「名字 + 赞 + 收藏 + 下载」。
// 名字短的时候第二行只剩三个键，而那一行到底还剩多少宽度就量不到了。
func likeItem() app.Item {
	return app.Item{
		Source: "online", Type: "mv", Bvid: likeBvid, Cid: "1",
		Title: "要赞的那一首", Artist: "一个名字很长的 UP 主名字", ArtistMid: 12345,
	}
}

// newShellWidth 建一个给定宽度的整窗外壳（和 `newShell` 一样，只是宽窄可变）。
//
// 为什么不来一个 `newShellAt(t, w, h)`：那一处的三行收尾（历史、快捷键、红点那个
// 定时器）漏掉就是一堆挂着的 goroutine，而它们会让后面那些按固定像素判主题的
// 测试随机红（`newShell` 的注释里有详细说明）。
func newShellWidth(t *testing.T, w int) (*Shell, *mygoui.Tester) {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)
	s := New(a)
	t.Cleanup(s.images.Close)
	t.Cleanup(func() { _ = s.hist.Close() })
	t.Cleanup(func() { _ = s.keys.Close() })
	t.Cleanup(s.feed.disarm)
	s.router.Transition = mygoui.TransitionNone
	return s, mygoui.NewTester(s.View, w, likeWinH)
}

// advanceLike 把两个钟都往前推（同 advanceFull）。
func advanceLike(e *likeEnv, d time.Duration) {
	e.fc.Advance(d)
	e.tt.Advance(d)
}

// coverAt 是播放条上那张封面在画面上的框（找「当前封面」那个）。
func coverAt(t *testing.T, tt *mygoui.Tester) mygoui.Rect {
	t.Helper()
	if r, ok := tt.Find("当前封面"); ok {
		return r
	}
	t.Fatalf("画面上找不到封面，文字有 %q", tt.Texts())
	return mygoui.Rect{}
}

// holdCover 按住封面，并且**一直按着**（不松手）。
//
// 返回的是**按下去那一刻**的那张封面框和那一帧的画面——后面几帧拿它当基线比。
//
// ⚠️ **一毫秒都不推进**：推进一下就等于按下时刻往前挪，那 2500 / 500 两条断言
// 量到的就不是 Biu 的那三个数了（第一版就在这里栽过：`holdCover` 推了 1ms，
// 于是「2499ms」实际上是 2500ms，三连在断言之前就发去了）。
func holdCover(t *testing.T, e *likeEnv) (mygoui.Rect, *image.RGBA) {
	t.Helper()
	x, y := coverPoint(t, e.tt)
	e.tt.Press(x, y)
	return coverAt(t, e.tt), cloneImage(e.tt.Image())
}

// coverPoint 是播放条上那张封面的中心（`Tester` 的那几个方法收两个数而不是一对，
// 所以这里多给一个 `coverPoint2` 免得每一处都写 `x, y :=`）。
func coverPoint(t *testing.T, tt *mygoui.Tester) (float32, float32) {
	t.Helper()
	r := coverAt(t, tt)
	return r.X + r.W/2, r.Y + r.H/2
}

// coverPoint2 是 coverPoint 的成对版。
//
// 为什么要有两个：`Tester` 的 `Press` / `Release` 收两个数，而 Go 不许把一个
// 多返回值直接摊进另一个的参数里（`f(g())` 只有 g 返回一个值时才行）。
func coverPoint2(t *testing.T, tt *mygoui.Tester) [2]float32 {
	r := coverAt(t, tt)
	return [2]float32{r.X + r.W/2, r.Y + r.H/2}
}

// cloneImage 拷一份画面出来（`Tester.Image` 下一帧就被覆盖了）。
func cloneImage(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Rect)
	draw.Draw(dst, src.Rect, src, src.Rect.Min, draw.Src)
	return dst
}
