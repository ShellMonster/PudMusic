package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
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

// longToastText 是长成好几行的一句提示——真机上弹的就是这种（见
// syncGlobalShortcuts 那一句）。
const longToastText = "有几个全局快捷键没登记上：播放/暂停（和应用内那条重了）、上一首（和应用内那条重了）、下一首（和应用内那条重了）、音量加（和应用内那条重了）。应用内那一列照常用。"

// TestTheToastCardItselfDoesNotBlockClicks 验**卡片本身**也不吃点击。
//
// 上一条量的是「浮层容器空的地方透传」，而提示抬过播放条之后，卡片压在的**正是**
// 页面右下角那一块可点的东西。症状是「刚改完一个设置，右下角弹一句，两秒之内那个
// 按钮点不动」，而提示两秒后自己就没了——于是它看起来像那个按钮偶尔失灵，
// 最容易被当成界面坏了。
//
// 踩出来的经过：快捷键设置页最后那几行的 ×（清空那一格）就在那个位置上，而
// `syncGlobalShortcuts` 会在改完设置之后弹一句「全局快捷键没登记上」。于是
// `TestTheClearButtonInEveryRecorderCellActuallyClears` 一旦遇上那句提示就红。
//
// ⚠️ **逐格先量「点在不在卡片底下」**：不在的那几格量不到这一半，硬点它们只是白点。
// 而一格都不在底下的话这条就等于没量——所以那一条是 `t.Fatal` 而不是跳过（跳过的
// 测试没人会去读）。
func TestTheToastCardItselfDoesNotBlockClicks(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "快捷键设置")
	shortToasts(t, time.Minute)

	// 摆满 `toastLimit` 条，而且**用真的那种长句子**：短句子只占一行，卡片矮，三条
	// 叠起来还在表格下面——那就量不到任何东西。而真机上弹的那一串折了好几行，
	// 卡片高到三条叠起来正好压住表格最后那几行（这就是它在真机上出问题的那一档）。
	for i := range toastLimit {
		s.addToast(longToastText + strings.Repeat("·", i))
	}
	tt.Frame()
	// `Find` 给的是**文字**那一块的框，而卡片比文字大（上下各 12 的内边距）。所以
	// 这里拿三条文字的**并集**当「被压住的那一片」，宁可少算几条也别把没被压住的
	// 那几条算进来——那几条点得动是本来就该有的，不算证据。
	stack := mygoui.Rect{}
	for i := range toastLimit {
		r, found := tt.Find(longToastText + strings.Repeat("·", i))
		if !found {
			t.Fatalf("第 %d 条提示没画出来，文字有 %q", i, tt.Texts())
		}
		stack = unionRect(stack, r)
	}

	under := 0
	for _, item := range defaultShortcutSettings().Shortcuts {
		label := "清空「" + item.Name + "」的快捷键"
		r, found := tt.Find(label)
		if !found {
			t.Fatalf("没有「%s」那个 ×，文字有 %q", label, tt.Texts())
		}
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		if cx < stack.X || cx > stack.X+stack.W || cy < stack.Y || cy > stack.Y+stack.H {
			continue // 这一格不在卡片底下
		}
		under++
		tt.ClickAt(cx, cy)
		tt.Frame()
		if it, _ := findShortcut(item.ID, s.keys.snapshot().Shortcuts); it.Shortcut != "" {
			t.Errorf("卡片压着的那一格点不动：%s 还是 %q（卡片把点击吃了）", item.Name, it.Shortcut)
		}
		// 摆回默认值，好让下一格有东西可清。
		if err := s.keys.set(shortcutSlot{id: item.ID}, item.Shortcut); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	if under == 0 {
		t.Fatalf("卡片底下一个 × 都没有（被压住的那一片在 %v，窗口 1200x800）："+
			"这一条量的是「卡片压着可点的东西」，一格都不在底下就等于没量", stack)
	}
}

// unionRect 把两个框并成一个（零值当空框）。
func unionRect(a, b mygoui.Rect) mygoui.Rect {
	if b.W == 0 || b.H == 0 {
		return a
	}
	if a.W == 0 || a.H == 0 {
		return b
	}
	x, y := min(a.X, b.X), min(a.Y, b.Y)
	r, bo := max(a.X+a.W, b.X+b.W), max(a.Y+a.H, b.Y+b.H)
	return mygoui.Rect{X: x, Y: y, W: r - x, H: bo - y}
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
