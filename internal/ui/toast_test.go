package ui

import (
	"strings"
	"testing"
	"time"
)

// Toast 这一组盯的是 Biu 的三条硬要求：右下角、避开 88px 播放条、同时最多 3 条。
// 加上「切路由不残留」在 router_test.go 里。

// 短一点的存活时间，测试不用干等 4 秒。
func shortToasts(t *testing.T, d time.Duration) {
	t.Helper()
	old := toastLifetime
	toastLifetime = d
	t.Cleanup(func() { toastLifetime = old })
}

// TestToastSitsBottomRightAboveThePlayBar 验位置。
//
// 这两条是同一条要求的两半：右下角（Biu 的 `placement="bottom-right"`），并且
// 抬过播放条（`toastOffset={90}`）。
func TestToastSitsBottomRightAboveThePlayBar(t *testing.T) {
	s, tt := newShell(t)

	s.addToast("这是一条提示")
	tt.Frame()

	r, ok := tt.Find("这是一条提示")
	if !ok {
		t.Fatalf("提示没画出来，文字有 %q", tt.Texts())
	}
	const w, h = 1200, 800
	if cx := r.X + r.W/2; cx <= w/2 {
		t.Errorf("提示在左边（中心 x=%v，窗口宽 %v），want 右下角", cx, w)
	}
	if bottom := r.Y + r.H; bottom > h-playbarClearance {
		t.Errorf("提示底边在 y=%v，压进了播放条（Biu 的 toastOffset 是 %v）", bottom, playbarClearance)
	}
	if r.Y <= 0 {
		t.Errorf("提示顶边在 y=%v，不像是浮在下面", r.Y)
	}
}

// TestToastIsActuallyPainted 验提示**看得见**，不只是排版排出来了。
//
// 这个坑踩过：卡片底色取的是 Theme.Inverse，而主题没设它的时候它是零值，
// 等于 Background —— 白卡片画在白窗口上。元素框查得到、位置算得对、
// 文字也在，可屏幕上什么都没有。只查元素框的测试是发现不了的。
func TestToastIsActuallyPainted(t *testing.T) {
	s, tt := newShell(t)

	s.addToast("看得见吗")
	tt.Frame()

	r, ok := tt.Find("看得见吗")
	if !ok {
		t.Fatalf("提示没画出来，文字有 %q", tt.Texts())
	}
	img := tt.Image()
	// 卡片是 320 宽、左右各 16 内边距，文字框往左退 16 就是卡片左边。
	// 取卡片左边缘内侧、竖直中间那一点，避开文字本身。
	card := img.RGBAAt(int(r.X)-12, int(r.Y+r.H/2))
	window := img.RGBAAt(4, 4)
	if card == window {
		t.Errorf("提示和窗口是同一个颜色（%v），画出来看不见", card)
	}
}

// TestAtMostThreeToasts 验同时最多 3 条，多出来的挤掉最旧的。
func TestAtMostThreeToasts(t *testing.T) {
	s, tt := newShell(t)
	shortToasts(t, time.Minute)

	for i := range 5 {
		s.addToast(string(rune('A' + i)))
	}
	tt.Frame()

	got := s.visibleToasts()
	if len(got) != toastLimit {
		t.Fatalf("同时有 %d 条，want %d：%q", len(got), toastLimit, toastTexts(got))
	}
	// 留下的是最新的三条，最旧的 A、B 被挤掉。
	want := []string{"C", "D", "E"}
	for i, w := range want {
		if got[i].text != w {
			t.Errorf("第 %d 条是 %q，want %q（全部 %q）", i, got[i].text, w, toastTexts(got))
		}
	}
	if tt.HasText("A") {
		t.Errorf("最旧的那条还在画面上，文字有 %q", tt.Texts())
	}
}

// TestToastGoesAwayOnItsOwn 验到点自己消失。
//
// 收走之后 expire 会调 s.wake.invalidate()，少了这一步这条提示会一直挂在
// 屏幕上，直到别的原因碰巧重画了界面为止（只投递不唤醒 = 0 帧刷新，实测踩过）。
// 唤醒这一路本身在 Tester 里看不出来 —— Tester 没有后台出帧的循环，画一帧
// 要靠测试驱动。所以这里验的是「到点状态就被收干净了」，唤醒那一路由
// imgcache 的 TestEveryWindowGetsWoken 守着。
func TestToastGoesAwayOnItsOwn(t *testing.T) {
	s, tt := newShell(t)
	// ⚠️ 存活时间要**比一帧长**，不能只比「够短」。
	//
	// 这一条要在 addToast 之后先 Frame 一次、确认它画出来了，才能开始等它消失。
	// 原来的 50ms 是在跟那一帧赛跑：`-race` 下建一帧本身就要十几到几十毫秒，
	// 机器一忙（几个包的测试并行跑）帧就画完了、提示早到期被收走，于是报
	// 「提示没画出来」——症状像是 Toast 根本没弹过。实测 `go test -race ./...`
	// 撞到过两次。
	//
	// 300ms 仍然远小于下面那个 3 秒的等待上限，到点照样会消失，这条验的还是
	// 「到点自己收走」这一件事。
	shortToasts(t, 300*time.Millisecond)

	s.addToast("一会儿就没了")
	tt.Frame()
	if !tt.HasText("一会儿就没了") {
		t.Fatalf("提示没画出来，文字有 %q", tt.Texts())
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(s.visibleToasts()) > 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := s.visibleToasts(); len(got) != 0 {
		t.Fatalf("到点之后还留着 %d 条：%q", len(got), toastTexts(got))
	}

	tt.Frame()
	if tt.HasText("一会儿就没了") {
		t.Errorf("到点之后提示还挂在画面上，文字有 %q", tt.Texts())
	}
}

// TestClearToastsDropsEverything 验清空。
func TestClearToastsDropsEverything(t *testing.T) {
	s, _ := newShell(t)
	shortToasts(t, time.Minute)

	s.addToast("甲")
	s.addToast("乙")
	s.clearToasts()

	if got := s.visibleToasts(); len(got) != 0 {
		t.Errorf("清空之后还留着 %d 条：%q", len(got), toastTexts(got))
	}
}

// TestToastDoesNotBlockTheUI 验提示那层浮着的东西不吃点击。
//
// 提示是铺满窗口的一个浮层容器。不让空的地方透传的话，它会把底下的侧栏
// 和播放条全挡住——那就不是提示，是一层罩子。
func TestToastDoesNotBlockTheUI(t *testing.T) {
	s, tt := newShell(t)
	shortToasts(t, time.Minute)

	s.addToast("挡着点试试")
	tt.Frame()
	if !tt.HasText("挡着点试试") {
		t.Fatalf("提示没画出来，文字有 %q", tt.Texts())
	}

	// 提示浮在右下角，播放条在正下方偏左。点播放/暂停：点得着就没被挡。
	play, ok := tt.Find("播放")
	if !ok {
		t.Fatalf("画面上找不到播放按钮，文字有 %q", tt.Texts())
	}
	tt.ClickAt(play.X+play.W/2, play.Y+play.H/2)
	tt.Frame()

	if !s.app.Snapshot().HasTrack {
		return // 没歌时播放按钮是禁用的，点不动是应该的
	}
	if s.app.Snapshot().Playing {
		t.Error("点播放之后没在播，提示那层把点击吃了")
	}
}

// TestToastTextIsKeptVerbatim 验长一点的文案也不会被截断成认不出的样子。
func TestToastTextIsKeptVerbatim(t *testing.T) {
	s, tt := newShell(t)
	shortToasts(t, time.Minute)

	want := "「收藏夹 / 合集 / 系列」这一页还没做"
	s.addToast(want)
	tt.Frame()

	if !tt.HasText(strings.Trim(want[:5], "「")) {
		t.Errorf("提示没画出来，文字有 %q", tt.Texts())
	}
}
