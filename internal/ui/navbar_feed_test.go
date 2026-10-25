// 动态红点那一组：位置、点过之后不再冒出来、没登录时一个请求都不发。
//
// ⚠️ 这一组有几条走的是**真的**那一趟（后台 goroutine + 假 B 站），不是把问的
// 那一只换掉：红点坏掉的症状是「那个点不亮」，而「不亮」有三种原因——没问、
// 问了但没接上、接上了但那个点画在别处——只有真的跑一遍才分得开。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是本地假服务器。
package ui

import (
	"image/color"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// feedBaselineLog 记下每一趟问 B 站时带的基线。
type feedBaselineLog struct {
	mu sync.Mutex
	// seen 是每一趟的基线（字符串）。
	seen []string
}

// add 记一趟。
func (l *feedBaselineLog) add(baseline string) {
	l.mu.Lock()
	l.seen = append(l.seen, baseline)
	l.mu.Unlock()
}

// count 是问了几趟。
func (l *feedBaselineLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.seen)
}

// at 拿第 i 趟的基线。
func (l *feedBaselineLog) at(i int) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if i >= len(l.seen) {
		return ""
	}
	return l.seen[i]
}

// has 报「有没有哪一趟带着这个基线」。
func (l *feedBaselineLog) has(baseline string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.seen {
		if s == baseline {
			return true
		}
	}
	return false
}

// all 是问过的全部基线（给报错那句话用）。
func (l *feedBaselineLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.seen...)
}

// TestTheRedDotSitsOnTheTopRightOfTheIcon 验那个点**落在图标的右上角**。
//
// ⚠️ **这一条是这一票的要害**：那个点以前画在图标的**右边**——它是那一行里的
// 另一个孩子，中间隔着 4 像素。那是「旁边有个小圆点」，不是「右上角有个点」，
// 而用户看不见的就是它。
//
// 判据是**位置**：点的中心落在图标右上角往里 1.3 的地方（上游那一句
// `translate-x-1/3 -translate-y-1/3` 的结果，见 navbar_feed.go 的
// dynamicDotNudge）。
func TestTheRedDotSitsOnTheTopRightOfTheIcon(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	fakeClockOn(s, fakeClockStart)
	// 假问：同步回话，说有 3 条没看过的。
	s.feed.ask = func(done func(int, error)) { done(3, nil) }

	tt.Frame()
	icon, ok := tt.Find("动态")
	if !ok {
		t.Fatalf("顶栏上没有「动态」那一枚，文字有 %q", tt.Texts())
	}
	dot, ok := tt.Find("3 条新动态")
	if !ok {
		t.Fatalf("那个点没画出来，文字有 %q", tt.Texts())
	}

	wantX := icon.X + icon.W - dynamicDotNudge
	wantY := icon.Y + dynamicDotNudge
	gotX := dot.X + dot.W/2
	gotY := dot.Y + dot.H/2
	if absF32(gotX-wantX) > 2 || absF32(gotY-wantY) > 2 {
		t.Errorf("那个点的中心在 (%.1f, %.1f)，want (%.1f, %.1f)"+
			"（图标右上角往里 %.1f）",
			gotX, gotY, wantX, wantY, dynamicDotNudge)
	}

	// ⚠️ **还要看它真的画上去了**：位置对而没画（被盖住、画在窗口外），
	// 用户看见的还是「没有点」。
	//
	// 颜色是**主题里那一个 warning**（规矩见 semantic.go 第 3 条：图形色用
	// 主题的原值）——经典那一档是上游的 #f5a524，主题方案各有自己的一档。
	want := BuildTheme(s.app.SettingsSnapshot(), false).Warning
	got := tt.Image().RGBAAt(int(gotX), int(gotY))
	if wantA := (color.RGBA{R: want.R, G: want.G, B: want.B, A: 0xff}); got != wantA {
		t.Errorf("那个点的中心那个像素是 %v，want %v（主题里那一个 warning）", got, wantA)
	}
}

// TestTheRedDotDoesNotComeBackAfterReadingIt 验「点进去清掉」真的清得掉。
//
// 这一条走的是**真的**那一趟：假 B 站按基线回答——基线是 0 就说「有 3 条没看过
// 的」，基线不是 0 就说「没有」（用户看过了）。
//
// ⚠️ **它量的是「基线往后挪了」**：点一下之后基线记成「用户看的那一刻」，下一轮
// 问的就是那个基线，于是 B 站说 0——那个点不会又冒出来。
//
// （上游 Biu 传的一直是 0，所以它清掉之后五分钟又回来：用户明明看过了。）
func TestTheRedDotDoesNotComeBackAfterReadingIt(t *testing.T) {
	b := newUIExplore(t)
	var log feedBaselineLog
	b.on("/x/polymer/web-dynamic/v1/feed/all/update", func(w http.ResponseWriter, r *http.Request) {
		baseline := r.URL.Query().Get("update_baseline")
		log.add(baseline)
		n := 0
		if baseline == "0" {
			n = 3
		}
		uiExploreJSON(w, 0, map[string]any{"update_num": n})
	})
	// 设置挂着：基线要落盘，而落盘走的是设置存储（见 internal/app/dynamicseen.go）。
	s, tt, a := newSpaceShell(t, b, true, "77", withSettings(app.Settings{}))
	clock := fakeClockOn(s, fakeClockStart)

	waitFrames(t, tt, "那个点出来了", func() bool { return tt.HasText("3 条新动态") })

	if err := tt.Click("动态"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("3 条新动态") {
		t.Errorf("看完动态那个点还在，文字有 %q", tt.Texts())
	}

	// ⚠️ **基线必须往后挪**：它就是「用户看的那一刻」。
	//
	// 判据是**这一趟问的基线是不是那个新值**，而不是「问了几趟」——点之前发出
	// 去的那一趟还带着 0（它发出去的时候用户还没看），那是应该的。
	want := a.DynamicBaseline()
	if want == 0 {
		t.Fatalf("点过之后基线还是 0：那个点会又回来")
	}

	// 下一轮。
	clock.Advance(wantFeedInterval)
	waitFrames(t, tt, "下一轮带着新基线问出去了", func() bool {
		return log.has(strconv.FormatInt(want, 10)) && !s.feed.busy
	})

	if tt.HasText("3 条新动态") {
		t.Errorf("看完动态之后那个点又回来了，文字有 %q", tt.Texts())
	}
}

// TestTheRedDotComesBackWhenNewDynamicsArrive 验「看过之后又有新动态 → 点会再亮」。
//
// 这一条是上一条的**另一半**：上一条量的是「点过之后那个点不再回来」（没有新动态
// 的时候它不该回来），这一条量的是「**有**新动态的时候它要回来」——不然就成了
// 「点一次就永远不亮了」。
//
// ⚠️ 假 B 站**每一趟都说「有 2 条没看过的」**：第一趟让点亮起来；点过之后那一趟
// （基线已经往后挪了）说的还是 2 —— 那就是「用户看过之后又有新的了」。
func TestTheRedDotComesBackWhenNewDynamicsArrive(t *testing.T) {
	b := newUIExplore(t)
	var log feedBaselineLog
	b.on("/x/polymer/web-dynamic/v1/feed/all/update", func(w http.ResponseWriter, r *http.Request) {
		log.add(r.URL.Query().Get("update_baseline"))
		uiExploreJSON(w, 0, map[string]any{"update_num": 2})
	})
	s, tt, a := newSpaceShell(t, b, true, "77", withSettings(app.Settings{}))
	clock := fakeClockOn(s, fakeClockStart)

	waitFrames(t, tt, "那个点出来了", func() bool { return tt.HasText("2 条新动态") })

	if err := tt.Click("动态"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("2 条新动态") {
		t.Errorf("看完动态那个点还在，文字有 %q", tt.Texts())
	}
	want := a.DynamicBaseline()
	if want == 0 {
		t.Fatalf("点过之后基线还是 0")
	}

	// 下一轮：基线往后挪了，而 B 站说「又有 2 条」→ 那个点要再亮。
	clock.Advance(wantFeedInterval)
	waitFrames(t, tt, "那个点又亮了", func() bool { return tt.HasText("2 条新动态") })

	if !log.has(strconv.FormatInt(want, 10)) {
		t.Errorf("下一轮问的基线里没有那个新基线 %d（问过的：%v）", want, log.all())
	}
}

// TestNothingIsAskedWhenNobodyIsLoggedIn 验**没登录时一个请求都不发**。
//
// 动态流是「我关注的人在发什么」，没有登录态就没有这个列表；而 B 站对那个接口
// 回 -101——那个 -101 在别的层和「网络断了」长得一模一样。
func TestNothingIsAskedWhenNobodyIsLoggedIn(t *testing.T) {
	b := newUIExplore(t)
	var log feedBaselineLog
	b.on("/x/polymer/web-dynamic/v1/feed/all/update", func(w http.ResponseWriter, r *http.Request) {
		log.add(r.URL.Query().Get("update_baseline"))
		uiExploreJSON(w, 0, map[string]any{"update_num": 3})
	})
	_, tt, _ := newSpaceShell(t, b, false, "", withSettings(app.Settings{}))

	tt.Frame()
	tt.Frame()
	if got := log.count(); got != 0 {
		t.Errorf("没登录也问了 %d 次", got)
	}
	if tt.HasText("3 条新动态") {
		t.Errorf("没登录却有那个点，文字有 %q", tt.Texts())
	}
}

// TestAnAnswerThatComesBackAfterReadingIsIgnored 验「在途的那一趟」不会把点点亮。
//
// 场景：问出去了，答案还在路上，用户这时候点进动态页把点清掉了；然后答案回来
// （它说的是「用户还没看的时候」的数）——它要是被当真，点就又亮了，而用户明明
// 已经看过了。
func TestAnAnswerThatComesBackAfterReadingIsIgnored(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	fakeClockOn(s, fakeClockStart)

	// 假问：它把回调扣住，答案晚点才给。
	var pending func(int, error)
	s.feed.ask = func(done func(int, error)) { pending = done }

	tt.Frame()
	if pending == nil {
		t.Fatal("那一趟没有问出去")
	}

	// 用户点进动态页：点被清掉。
	if err := tt.Click("动态"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("3 条新动态") {
		t.Fatalf("点之前那个点就在，文字有 %q", tt.Texts())
	}

	// 答案这时候才回来——它说的是 3 条。
	pending(3, nil)
	tt.Frame()
	tt.Frame()
	if tt.HasText("3 条新动态") {
		t.Errorf("看过的动态又被点亮了，文字有 %q", tt.Texts())
	}
}

// TestTheQueryGoesOutRightAfterTheAppOpens 验「打开 app 就问一次」。
//
// ⚠️ **不是等五分钟**：Biu 那个 `useRequest` 挂上就发第一次
// （`user-feed/index.tsx:14`），我们这一版也是（`tick` 里零值的 `next` 就是
// 「马上」）。用户第一次打开就该看见那个点，而不是干等五分钟。
func TestTheQueryGoesOutRightAfterTheAppOpens(t *testing.T) {
	b := newUIExplore(t)
	var log feedBaselineLog
	b.on("/x/polymer/web-dynamic/v1/feed/all/update", func(w http.ResponseWriter, r *http.Request) {
		log.add(r.URL.Query().Get("update_baseline"))
		uiExploreJSON(w, 0, map[string]any{"update_num": 0})
	})
	_, tt, _ := newSpaceShell(t, b, true, "77", withSettings(app.Settings{}))

	waitFrames(t, tt, "第一趟问出去了", func() bool { return log.count() >= 1 })
	if got := log.at(0); got != "0" {
		t.Errorf("第一趟问的基线是 %q，want 0（一次都没看过 → 从头算）", got)
	}
}
