// 点赞与一键三连的界面那一层：长按 2.5 秒、500 毫秒起进度环、已点赞时长按被
// 完全禁用、以及**长按之后补发的那一次 click 必须吞掉**。
//
// 走的是整条链：真的假 B 站（`biliFixture`）→ `App` 那一摊缓存 → 播放条上的星标
// 和封面 → 长按 → 请求真的发出去。所以这些断的是「用户按得住的那一下」，不是
// 「某个函数被调到了」。
//
// ⚠️ **全程假时钟**：2.5 秒和 500 毫秒都是 `s.clock` 上算的（`advanceLike` 把
// `s.clock` 和 Tester 自己那只一起推）。真等就是一条测试两秒半。
package ui

import (
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"net/http"
	"strconv"
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
	_, _ = w.Write([]byte(writeBiliLikeJSON(map[string]any{
		"like": liked, "favorite": faved,
	})))
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
	settleSearch(t, tt, "赞没赞问回来了", func() bool { return s.app.LikeAsked() })
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

// ---- 点赞星标 ----

// TestTheStarIsLitOnlyAfterTheServerSaysSo 钉住「点赞后播放条星标变亮」那一半。
//
// 「问回来之前」也断了一段：界面上显示的是「没赞」，而那**不是**用户已经知道的
// 事实（用户知道自己赞过）。锁屏解锁、切歌那一瞬，星标暗一下再亮是可接受的；
// 而**问都没问就显示「已赞」**是把一个我们猜的答案当事实说。
func TestTheStarIsLitOnlyAfterTheServerSaysSo(t *testing.T) {
	e := newLikeEnv(t, true, false, false)

	if _, ok := e.tt.Find("已赞"); !ok {
		t.Errorf("B 站说赞过了，星标却没亮，文字有 %q", e.tt.Texts())
	}
	if _, ok := e.tt.Find("赞"); ok {
		t.Errorf("赞过了还摆着「赞」这个键，文字有 %q", e.tt.Texts())
	}
}

// TestClickingTheStarLikesTheSong 断的是「点一下真的会发出去」。
//
// 星标是**两样东西**：画面上的一个字，和发出去的那一次 POST。第一版完全可以只做
// 前一样（把字换成「已赞」就完事），而用户点了没反应。
func TestClickingTheStarLikesTheSong(t *testing.T) {
	e := newLikeEnv(t, false, false, false)

	if _, ok := e.tt.Find("赞"); !ok {
		t.Fatalf("没赞的时候播放条上没有那个键，文字有 %q", e.tt.Texts())
	}
	if err := e.tt.Click("赞"); err != nil {
		t.Fatalf("点不了星标：%v（文字：%q）", err, e.tt.Texts())
	}

	settleSearch(t, e.tt, "赞发出去了", func() bool { return e.f.likePostsCount() > 0 })
	if got := e.f.lastLikeUp(); got != "1" {
		t.Errorf("发出去的 up 是 %q，want 1（1 是赞，2 是取消赞）", got)
	}
	// 乐观：点完那一帧就亮了，不用等网络。
	e.tt.Frame()
	if _, ok := e.tt.Find("已赞"); !ok {
		t.Errorf("点完星标还是暗的，文字有 %q", e.tt.Texts())
	}
}

// TestTheLikeRowFitsInThePlaybar 钉住「多摆一个赞字键，别的没被挤坏」。
//
// 🔴 两条都是 BMG-103 踩过的坑，加键之后要重新钉一遍：
//
//	歌名那一行被挤成竖条  左边那一块只有 `Grow(1)`，歌名和第二行吃的是**同一块**
//	                    宽度。而**挤没了之后界面上看不出坏了**——字还在那儿，只是
//	                    一个字一行（`playbar_meta_test.go` 的注释里有完整的事故）
//	键跑出窗口          三个键都是写死的宽度，最后一个跑出去会被 88 高的播放条裁掉
//
// 宽度取 760（比窗口下限 900 还窄）到 1200。UP 名给的是最长的那个（见 `likeItem`）。
func TestTheLikeRowFitsInThePlaybar(t *testing.T) {
	for _, width := range []int{760, 900, 1000, 1200} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			e := newLikeEnvSize(t, false, false, false, width)

			for _, label := range []string{"赞", "收藏", "下载"} {
				r, ok := e.tt.Find(label)
				if !ok {
					t.Fatalf("宽度 %d：%q 不在画面上，文字有 %q", width, label, e.tt.Texts())
				}
				if r.X < 0 || r.X+r.W > float32(width) {
					t.Errorf("宽度 %d：%q 画在 x=%.1f..%.1f，跑出窗口了",
						width, label, r.X, r.X+r.W)
				}
				if r.Y+float32(likeWinH-playbarHeight) < 0 || r.Y+r.H > float32(likeWinH) {
					t.Errorf("宽度 %d：%q 画在 y=%.1f..%.1f，跑出播放条了",
						width, label, r.Y, r.Y+r.H)
				}
			}

			title, ok := e.tt.Find("要赞的那一首")
			if !ok {
				t.Fatalf("宽度 %d：歌名不在画面上，文字有 %q", width, e.tt.Texts())
			}
			if title.W < 40 || title.W < title.H {
				t.Errorf("宽度 %d：歌名只剩 %.1f×%.1f，已经被挤成竖条了",
					width, title.W, title.H)
			}
		})
	}
}

// TestThereIsNoStarForALocalSong 钉住「本地文件不摆星标」。
//
// B 站那三个接口一个都不收本地文件（它压根没有投稿）。摆一个点了没反应的键，
// 比不摆更让人以为它坏了——Biu 那边也是 `playItem.source !== "local"` 才摆。
func TestThereIsNoStarForALocalSong(t *testing.T) {
	s, tt := newShell(t)
	tt.SetDark(true)
	if err := s.app.Enqueue(app.LocalItem(flacPath(t))); err != nil {
		t.Fatalf("放本地文件：%v", err)
	}
	tt.Frame()
	if !s.app.Snapshot().HasTrack {
		t.Fatalf("歌没开起来：%q", s.app.Snapshot().Err)
	}
	tt.Frame()
	if _, ok := tt.Find("赞"); ok {
		t.Errorf("本地文件的播放条上摆着星标，文字有 %q", tt.Texts())
	}
}

// ---- 长按与进度环 ----

// TestTheRingStartsAfterHalfASecond 钉住「500 毫秒开始显示进度环」这个数。
//
// ⚠️ 断的是**画面**：环画出来了没有。数一个「点赞进度」那个元素在不在是不够的
// ——那一个只要被建出来就在（而它建不建和环画不画是两件事）。
func TestTheRingStartsAfterHalfASecond(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	cover, before := holdCover(t, e)

	// 499 毫秒：还没有环。封面外面那一圈一个像素都不许多出来。
	advanceLike(e, biuLikeRingDelayMS*time.Millisecond-time.Millisecond)
	if ringBuilt(e.tt) {
		t.Errorf("按住 %d 毫秒就开始画环了，Biu 是 %d 毫秒才开始画",
			biuLikeRingDelayMS-1, biuLikeRingDelayMS)
	}
	if n := ringPainted(e.tt, cover, before); n != 0 {
		t.Errorf("按住 %d 毫秒封面外面就多了 %d 个像素", biuLikeRingDelayMS-1, n)
	}

	// 再过 2 毫秒：环出来了（还差得远，走不完一圈）。
	advanceLike(e, 2*time.Millisecond)
	if !ringBuilt(e.tt) {
		t.Fatalf("按住 %d 毫秒了环还没出来，文字有 %q", biuLikeRingDelayMS, e.tt.Texts())
	}
	if n := ringPainted(e.tt, cover, before); n == 0 {
		t.Error("盒建出来了，封面外面却一个像素都没多 —— 环没真的画上去")
	}
	if e.f.triplePostCount() != 0 {
		t.Errorf("按住半秒就三连了（%d 次）", e.f.triplePostCount())
	}
}

// TestTheRingIsFullJustAsTheTripleGoesOut 钉住「环转满 = 三连触发」。
//
// Biu 那三个数是互相咬着的：环从 500ms 画起、2s 转满，而 500+2000 = 2500。
// 把环的时长单独改了就对不上——环转满那一刻用户以为「成了」，而请求还没发出去。
func TestTheRingIsFullJustAsTheTripleGoesOut(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	cover, before := holdCover(t, e)

	// 2499 毫秒：环几乎满了，**但还没发**。
	advanceLike(e, (biuLikeLongPressMS-1)*time.Millisecond)
	if e.f.triplePostCount() != 0 {
		t.Fatalf("按住 %d 毫秒就三连了，Biu 是 %d 毫秒",
			biuLikeLongPressMS-1, biuLikeLongPressMS)
	}
	if !ringBuilt(e.tt) {
		t.Fatal("快到点了环却不见了")
	}
	full := ringPainted(e.tt, cover, before)

	// 再过 2 毫秒：满了，而且**同时**三连已经发出去了。
	advanceLike(e, 2*time.Millisecond)
	settleSearch(t, e.tt, "三连发出去了", func() bool { return e.f.triplePostCount() > 0 })
	e.tt.Frame()
	if ringBuilt(e.tt) {
		t.Error("三连都发出去了环还在 —— Biu 那边是同一句 setIsProgressing(false)")
	}
	// 而 2499 毫秒那一刻环真的转满了：转满那一刻用户以为「成了」，而那时候请求
	// 还**没**发出去（上面已经断过了）。
	if full == 0 {
		t.Error("快到点时封面外面一个像素都没多 —— 环一直没画上去")
	}
}

// TestTheRingGrowsWhileTheCoverIsHeld 钉住「环是跟着手指走的」，不是一步到位。
//
// 把进度写成常数 1 的话上面那两条照样全绿，而用户看到的是「按下去立刻满环」，
// 然后干等两秒半。
func TestTheRingGrowsWhileTheCoverIsHeld(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	cover, before := holdCover(t, e)

	advanceLike(e, (biuLikeRingDelayMS+biuLikeRingSpanMS/4)*time.Millisecond)
	quarter := ringPainted(e.tt, cover, before)

	advanceLike(e, biuLikeRingSpanMS/4*time.Millisecond)
	half := ringPainted(e.tt, cover, before)

	if quarter <= 0 || half <= quarter {
		t.Errorf("环画了 %d 个像素然后 %d 个——它没有随着按住的时间变长", quarter, half)
	}
}

// TestLettingGoEarlyDoesNothing 钉住「短按不是三连」。
//
// 而那一次短按**照旧是普通点击**（封面照旧开全屏）：Biu 那边短按赞键是「赞一下」，
// 我们这边短按封面是「开全屏」，两边都不该被长按那一套吞掉。
func TestLettingGoEarlyDoesNothing(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	cover, before := holdCover(t, e)

	advanceLike(e, biuLikeRingDelayMS*time.Millisecond+100*time.Millisecond)
	if !ringBuilt(e.tt) {
		t.Fatal("按住 600 毫秒环还没出来，前一条已经量过这一点了")
	}
	if ringPainted(e.tt, cover, before) == 0 {
		t.Fatal("按住 600 毫秒环建出来了却没画上去")
	}

	// 松手。⚠️ **这一松是一次普通点击**，封面照旧开全屏（Biu 那边短按赞键是赞一下，
	// 我们这边短按封面是开全屏）——所以下面**不能**拿画面比了：满屏一开整窗都变了。
	pt := coverPoint2(t, e.tt)
	e.tt.Release(pt[0], pt[1])
	e.tt.Frame()

	if got := e.f.triplePostCount(); got != 0 {
		t.Errorf("按住 %d 毫秒就三连了（%d 次），Biu 是 %d 毫秒",
			biuLikeRingDelayMS+100, got, biuLikeLongPressMS)
	}
	if ringBuilt(e.tt) {
		t.Error("手已经松开了环还在")
	}
}

// TestALongPressNeverOpensTheFullScreen 钉住那一条真机上踩过的坑。
//
// 🔴 **长按之后浏览器补发的那一次 click 必须吞掉**。Biu 那边漏了它，一次长按 =
// 三连 + 一次多余的赞；我们这边长按目标是封面，于是漏了它 = **凭空弹出全屏
// 播放器**——进度环刚转满，界面就换成满屏那一屏，而用户以为自己只是赞了一下。
//
// 这一条量的是**画面**：松手之后「关闭全屏」那个键必须不在。
func TestALongPressNeverOpensTheFullScreen(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	holdCover(t, e)

	advanceLike(e, biuLikeLongPressMS*time.Millisecond+200*time.Millisecond)
	e.tt.Release(coverPoint(t, e.tt))
	e.tt.Frame()

	settleSearch(t, e.tt, "三连发出去了", func() bool { return e.f.triplePostCount() > 0 })
	if e.tt.HasText("关闭全屏") {
		t.Error("长按三连完，全屏播放器自己弹出来了 —— 补发的那一次 click 没吞掉")
	}
	if e.s.full.open {
		t.Error("外壳上记着全屏开着")
	}
	// 而**下一次**普通点击必须照旧开全屏：吞标记是一次性的，
	// 一直吞下去的话封面就再也点不开了。
	e.tt.ClickAt(coverPoint(t, e.tt))
	if !e.tt.HasText("关闭全屏") {
		t.Errorf("吞标记把后面那一次正常点击也吞了，文字有 %q", e.tt.Texts())
	}
}

// TestAShortPressStillOpensTheFullScreen 是上一条的反面：确认吞的是**那一次**。
//
// 没有这一条的话，把 `swallowClick` 写成「永远吞」上面那条照样全绿——而界面上
// 封面就再也点不开了（一个看着好好的键，什么也不发生）。
func TestAShortPressStillOpensTheFullScreen(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	x, y := coverPoint(t, e.tt)

	e.tt.ClickAt(x, y)
	if !e.tt.HasText("关闭全屏") {
		t.Errorf("点一下封面全屏没开，文字有 %q", e.tt.Texts())
	}
}

// TestTheLongPressIsOffOnceTheSongIsLiked 钉住「已点赞时长按被完全禁用」。
//
// 照抄 Biu 的 `handleMouseDown`：`if (isThumb) return`——**整个不进状态机**。
// 于是既没有进度环（用户不会以为还能长按），也没有那一次三连（那一次要花两枚
// 币，而 B 站压根不会再给）。
//
// ⚠️ 顺带断「那一次 click 仍然是普通的 click」：禁用长按**不是**禁用封面。
func TestTheLongPressIsOffOnceTheSongIsLiked(t *testing.T) {
	e := newLikeEnv(t, true, false, false)
	cover, before := holdCover(t, e)

	// 按住三秒（比三连那一把还长半秒）：没有三连，也没有环。
	advanceLike(e, biuLikeLongPressMS*time.Millisecond+500*time.Millisecond)

	if got := e.f.triplePostCount(); got != 0 {
		t.Errorf("已经赞过了还发出去 %d 次三连 —— Biu 那边是整个不进状态机", got)
	}
	if ringBuilt(e.tt) {
		t.Error("已经赞过了还建进度环 —— 用户会以为还能长按")
	}
	if n := ringPainted(e.tt, cover, before); n != 0 {
		t.Errorf("已经赞过了封面外面还多了 %d 个像素", n)
	}

	// ⚠️ **松手那一下仍然是普通点击**：禁用长按不是禁用封面。
	pt := coverPoint2(t, e.tt)
	e.tt.Release(pt[0], pt[1])
	e.tt.Frame()
	if !e.tt.HasText("关闭全屏") {
		t.Errorf("禁用长按把普通点击也禁用了 —— 封面照旧该开全屏，文字有 %q", e.tt.Texts())
	}
}

// TestTheLongPressIsOffUntilTheServerAnswers 钉住「没问就不许长按」。
//
// 换歌之后头半秒界面上「没赞」是我们**猜**的，而猜错的方向是「其实赞过了还让
// 长按」——那一次三连白花两枚币。宁可这半秒按不动。
func TestTheLongPressIsOffUntilTheServerAnswers(t *testing.T) {
	f := newBiliFixture(t, 1)
	// 把关系那一问卡住，模拟「网络慢 / B 站慢」。
	f.gate("/x/web-interface/archive/relation")

	s, tt := newShell(t)
	fc := newFakeClock(time.Unix(1_700_000_000, 0))
	s.clock = fc
	tt.SetDark(true)
	s.app.MountStreaming(app.Streaming{
		Client: f.client(), Media: f.Client(), TempDir: t.TempDir(),
	})
	if err := s.app.Enqueue(likeItem()); err != nil {
		t.Fatal(err)
	}
	settleSearch(t, tt, "在线那首开始播", func() bool { return s.app.Snapshot().HasTrack })
	tt.Frame()

	e := &likeEnv{s: s, tt: tt, fc: fc, f: f}
	holdCover(t, e)
	pt := coverPoint2(t, e.tt)
	advanceLike(e, biuLikeLongPressMS*time.Millisecond+500*time.Millisecond)
	e.tt.Release(pt[0], pt[1])
	e.tt.Frame()

	if got := f.triplePostCount(); got != 0 {
		t.Errorf("还没问「赞没赞」就发了 %d 次三连 —— 猜错的方向是白花两枚币", got)
	}
	f.releasePath("/x/web-interface/archive/relation")
}

// ---- 三连之后 ----

// TestTheTripleLightsTheStarAndTheFavorite 钉住「三连成功后收藏状态同步更新」。
//
// 长按那一把顺手收藏了这一首（B 站的回包里有 `fav` 那一栏），所以**两个键**都要
// 跟着变。只变星标的话，用户在三连之后完全看不出收藏夹里多了一首。
//
// 量的是**画面**：星标换了词（好量），而收藏那一键只有颜色变了
// （它是「收藏」两个字，一直都在那儿）——所以量的是那个键自己的框里有没有像素
// 变掉。
func TestTheTripleLightsTheStarAndTheFavorite(t *testing.T) {
	e := newLikeEnv(t, false, false, true)
	favBox, ok := e.tt.Find("收藏")
	if !ok {
		t.Fatalf("播放条上没有收藏那个键，文字有 %q", e.tt.Texts())
	}
	before := cloneImage(e.tt.Image())

	holdCover(t, e)
	pt := coverPoint2(t, e.tt)
	advanceLike(e, biuLikeLongPressMS*time.Millisecond+200*time.Millisecond)
	e.tt.Release(pt[0], pt[1])

	settleSearch(t, e.tt, "星标亮了", func() bool {
		e.tt.Frame()
		_, ok := e.tt.Find("已赞")
		return ok
	})
	if n := diffIn(favBox, before, e.tt.Image()); n == 0 {
		t.Error("三连收藏了这一首，而「收藏」那一键一个像素都没变")
	}
}

// TestAFailedTripleLeavesEverythingAlone 钉住「失败时什么都不改」。
//
// 三连失败时界面上要是留下一句「已赞」，用户会以为赞成功了；而他手上什么都没有。
func TestAFailedTripleLeavesEverythingAlone(t *testing.T) {
	e := newLikeEnv(t, false, false, true)
	e.f.failTriple()

	holdCover(t, e)
	x, y := coverPoint(t, e.tt)
	advanceLike(e, biuLikeLongPressMS*time.Millisecond+200*time.Millisecond)
	e.tt.Release(x, y)

	settleSearch(t, e.tt, "三连失败那句话弹出来了", func() bool {
		e.tt.Frame()
		return e.tt.HasText("一键三连没成功")
	})
	e.tt.Frame()
	if _, ok := e.tt.Find("已赞"); ok {
		t.Errorf("三连失败了星标还是亮的，文字有 %q", e.tt.Texts())
	}
}

// ---- 满屏上那张封面 ----

// TestTheFullScreenCoverTakesTheLongPressToo 钉住「接进播放条**与**全屏播放器」。
//
// 满屏上那一张是**另一个元素**（BMG-34 的共享元素转场让它有自己的框），所以
// 「两边都接上了」不是把同一个函数调两次就成立——满屏那张外面套了一层新的 Box，
// 长按要落在**它**上面。
func TestTheFullScreenCoverTakesTheLongPressToo(t *testing.T) {
	e := newLikeEnv(t, false, false, true)

	r := coverAt(t, e.tt)
	e.tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	if !e.tt.HasText("关闭全屏") {
		t.Fatalf("满屏没开，文字有 %q", e.tt.Texts())
	}
	e.tt.Frame()

	// 满屏上那张封面：找「点赞进度」那个元素是不行的（它要按住 500ms 才建），
	// 找的是**画着**的那一张——共享元素转场结束之后它落在满屏中央。
	big := fullCoverRect(t, e.tt)
	e.tt.Press(big.X+big.W/2, big.Y+big.H/2)
	advanceLike(e, biuLikeLongPressMS*time.Millisecond+200*time.Millisecond)

	settleSearch(t, e.tt, "满屏上的长按三连发出去了", func() bool { return e.f.triplePostCount() > 0 })
}

// ---- 小工具 ----

// ringBuilt 问「那个叫「点赞进度」的盒子建出来没有」。
//
// ⚠️ **这不等于「环画出来了」**（`TestTheRingIsReallyPainted` 量的是那个），
// 但它对「500ms / 2500ms 这两个数」是够用的：`viewLikeRing` 在 `progress <= 0`
// 时**连盒子都不建**（它第一句就 return），所以「盒子在」严格蕴含「这一帧要画环」。
// 拿它去量时间是准的。
func ringBuilt(tt *mygoui.Tester) bool {
	_, ok := tt.Find("点赞进度")
	return ok
}

// ringPainted 是「封面**外面那一圈**里比基线多出来多少像素」。
//
// ⚠️ **只数封面外面，不数封面里面，也不数整张画面**。两样都量不得：
//
//	封面里面  按下去会多出悬停浮层（半透明黑 + ⤢），松手它又消失——
//	          整块封面变一次就是四千多个像素，而那和环一点关系没有
//	整张画面  播放条上别的控件会跟着抖（抗锯齿、焦点圈）
//
// 环是画在封面**外面**的（Biu 的 `inset: -2px`），所以外面这一圈是干净的：
// 没有环的时候数出来**正好是 0**。
//
// ⚠️ 一个刚开始的环只有 4~5 个像素（进度 0.1% 是 1 段，一段 2×2），所以
// 「画出来了吗」判的是 `> 0` 而不是「够不够多」。
func ringPainted(tt *mygoui.Tester, cover mygoui.Rect, before *image.RGBA) int {
	now := tt.Image()
	band := inflate(cover, likeRingWidth*2+2)
	n := 0
	for y := max(int(band.Y), now.Rect.Min.Y); y < min(int(band.Y+band.H), now.Rect.Max.Y); y++ {
		for x := max(int(band.X), now.Rect.Min.X); x < min(int(band.X+band.W), now.Rect.Max.X); x++ {
			// 封面里面那一块不算：那儿归悬停浮层管。
			if x >= int(cover.X) && x < int(cover.X+cover.W) &&
				y >= int(cover.Y) && y < int(cover.Y+cover.H) {
				continue
			}
			if !samePixel(before.RGBAAt(x, y), now.RGBAAt(x, y)) {
				n++
			}
		}
	}
	return n
}

// inflate 是一个框往外长 d。
func inflate(r mygoui.Rect, d float32) mygoui.Rect {
	return mygoui.Rect{X: r.X - d, Y: r.Y - d, W: r.W + 2*d, H: r.H + 2*d}
}

// diffIn 是 box 那个框里有多少个像素不一样。
func diffIn(box mygoui.Rect, a, b *image.RGBA) int {
	img := a
	x0 := max(int(box.X), img.Rect.Min.X)
	y0 := max(int(box.Y), img.Rect.Min.Y)
	x1 := min(int(box.X+box.W), img.Rect.Max.X)
	y1 := min(int(box.Y+box.H), img.Rect.Max.Y)
	n := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if !samePixel(a.RGBAAt(x, y), b.RGBAAt(x, y)) {
				n++
			}
		}
	}
	return n
}

// samePixel 留一点余量（光栅化会有舍入）。
func samePixel(a, b color.RGBA) bool {
	const slack = 8
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= slack && d(a.G, b.G) <= slack && d(a.B, b.B) <= slack
}

// fullCoverRect 是满屏上那张封面在画面上的框。
//
// 它是共享元素转场的**终点**，所以按满屏那几个数算：宽 `min(宽*0.7, 高*0.48)`
// 夹在 260~520，高是宽的 0.75，居中。找元素不行——满屏上那个「当前封面」和
// 播放条上那个同名，而播放条这时候压根没建。
func fullCoverRect(t *testing.T, tt *mygoui.Tester) mygoui.Rect {
	t.Helper()
	winW, winH := float32(likeWinW), float32(likeWinH)
	w := min(winW*0.7, winH*0.48)
	w = min(max(w, 260), 520)
	h := w * 0.75
	return mygoui.Rect{X: (winW - w) / 2, Y: (winH - h) / 2, W: w, H: h}
}
