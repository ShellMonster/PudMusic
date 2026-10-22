package ui

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"image/color"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	mygoui "github.com/egoist/mygo/ui"
)

// Toast 这一组盯的是 Biu 的硬要求：右下角、避开 88px 播放条、同时最多 3 条，
// 外加票 #180 的三件：**按语义分色**、**250ms 入场**、**尺寸与位置**
// （356 宽 / `p-3` / 14/500 / 抬 90 / 离右边 8）。
// 加上「切路由不残留」在 router_test.go 里。

// 短一点的存活时间，测试不用干等 4 秒。
func shortToasts(t *testing.T, d time.Duration) {
	t.Helper()
	old := toastLifetime
	toastLifetime = d
	t.Cleanup(func() { toastLifetime = old })
}

// settleToast 让入场动画走完（推钟 300ms）。
//
// ⚠️ **必须推钟**：入场是 250ms 的一段，而 Tester 的钟站着不动——不推的话那条
// 提示一直停在「刚出现」（几乎全透明），画面上什么都看不见。
//
// 推 300 而不是 250：多推一点，动画**准**走完（250ms 那一帧正好是终点，而
// 「正好」在判据里是另一件事）。
func settleToast(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
}

// toastCardOf 是那条提示那张卡片的框。
//
// ⚠️ **卡片的框是从文字那一块的框推出来的**：卡片比文字大四周各 12（`p-3`），
// 而 `tt.Find` 给的是**文字元素**的框（它的左边就是卡片的内边距那一层）。
func toastCardOf(text mygoui.Rect) mygoui.Rect {
	return mygoui.Rect{
		X: text.X - toastPad,
		Y: text.Y - toastPad,
		W: text.W + 2*toastPad,
		H: text.H + 2*toastPad,
	}
}

// toastSampleAt 是卡片上那个「采得到底色、又不落在字上」的点。
//
// ⚠️ **取卡片底边那一块的中间**：入场动画是**横着**滑的（从右边滑进来），
// 而左右两边都在滑动的范围里（卡片最右时右边滑出卡片 16 像素，最左时左边
// 滑出 16 像素）——中间那一块是稳的。
//
// ⚠️ **取底边那一条留白**（卡片下边往里 6 像素）：文字是**左对齐**的，短句的
// 右边空着——采到的是卡片底色，不是字的笔画。
func toastSampleAt(text mygoui.Rect) (int, int) {
	card := toastCardOf(text)
	return int(card.X + card.W/2), int(card.Y + card.H - toastPad/2)
}

// colorDistance 是两个颜色差多远（逐通道取绝对值再加起来）。
//
// 判「有没有更接近」用它，而不是逐通道比大小：淡入是**每一个通道**都往目标
// 靠，而目标色和底色的通道大小关系不一定一致（绿底上淡入白字，绿色的通道是
// 变**大**的）。
func colorDistance(a, b color.RGBA) int {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) + d(a.G, b.G) + d(a.B, b.B)
}

// toastRGBA 是那个十六进制颜色在画面上的样子（逐字相同就是它）。
//
// ⚠️ **画面上的像素是 `color.RGBA`，不是 `ui.Color`**：前者是采回来的，
// 后者是「打算画成什么」——两个混起来比，比的就不是用户看见的东西了。
func toastRGBA(hex string) color.RGBA {
	return rgbaOf(mygoui.Hex(hex))
}

// toastRectOf 按**前缀**找一条提示的文字框。
//
// ⚠️ **用前缀不用全文**：有些话的后半截是运行时才知道的（`bili.Friendly`
// 那一段），而 `tt.Find` 要的是逐字相同。
func toastRectOf(t *testing.T, tt *mygoui.Tester, prefix string) mygoui.Rect {
	t.Helper()
	for _, s := range tt.Texts() {
		if !strings.HasPrefix(s, prefix) {
			continue
		}
		r, ok := tt.Find(s)
		if !ok {
			t.Fatalf("画面上有「%s」却找不到它的框", s)
		}
		return r
	}
	t.Fatalf("画面上没有以 %q 开头的提示，文字有 %q", prefix, tt.Texts())
	return mygoui.Rect{}
}

// toastInkColor 是那条提示上「字」的颜色。
//
// ⚠️ **取的是字形最深处那一个像素**：字是抗锯齿的，边缘那些像素是「字色和底色
// 的混合」，只有笔画最粗的地方才是字色本身——所以这里扫整块文字的框，取离
// 底色最远的那一个。
//
// ⚠️ **离底色最远**而不是「最深」：失败那一条是**白字**压在红底上，最深的是
// 底色自己。
func toastInkColor(t *testing.T, tt *mygoui.Tester, prefix string) color.RGBA {
	t.Helper()
	r := toastRectOf(t, tt, prefix)
	img := tt.Image()
	bg := toastColorAt(t, tt, prefix)
	best, bestDist := bg, 0
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			p := img.RGBAAt(x, y)
			if d := colorDistance(p, bg); d > bestDist {
				best, bestDist = p, d
			}
		}
	}
	return best
}

// toastColorAt 采一条提示那张卡片上的底色。
func toastColorAt(t *testing.T, tt *mygoui.Tester, prefix string) color.RGBA {
	t.Helper()
	r := toastRectOf(t, tt, prefix)
	x, y := toastSampleAt(r)
	return tt.Image().RGBAAt(x, y)
}

// TestToastSitsBottomRightAboveThePlayBar 验位置。
//
// 这两条是同一条要求的两半：右下角（Biu 的 `placement="bottom-right"`），并且
// 抬过播放条（`toastOffset={90}`）。
func TestToastSitsBottomRightAboveThePlayBar(t *testing.T) {
	s, tt := newShell(t)

	s.addToast("这是一条提示", notice.Plain)
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
//
// ⚠️ **要推钟让入场走完**：提示是淡进来的（250ms），刚出现那一帧它几乎是全
// 透明的——那一帧采到的是窗口底色，于是这一条会把「入场动画」误判成
// 「提示没画出来」。
func TestToastIsActuallyPainted(t *testing.T) {
	s, tt := newShell(t)

	s.addToast("看得见吗", notice.Plain)
	tt.Frame()
	settleToast(t, tt)

	card := toastColorAt(t, tt, "看得见吗")
	window := tt.Image().RGBAAt(4, 4)
	if card == window {
		t.Errorf("提示和窗口是同一个颜色（%v），画出来看不见", card)
	}
}

// TestAtMostThreeToasts 验同时最多 3 条，多出来的挤掉最旧的。
func TestAtMostThreeToasts(t *testing.T) {
	s, tt := newShell(t)
	shortToasts(t, time.Minute)

	for i := range 5 {
		s.addToast(string(rune('A'+i)), notice.Plain)
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

	s.addToast("一会儿就没了", notice.Plain)
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

	s.addToast("甲", notice.Plain)
	s.addToast("乙", notice.Plain)
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

	s.addToast("挡着点试试", notice.Plain)
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
		s.addToast(longToastText+strings.Repeat("·", i), notice.Plain)
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
	s.addToast(want, notice.Plain)
	tt.Frame()

	if !tt.HasText(strings.Trim(want[:5], "「")) {
		t.Errorf("提示没画出来，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 票 #180：语义色、尺寸与位置、入场
// ─────────────────────────────────────────────────────────────────────────────

// 上游那三个语义色（DESIGN.md §四）。
//
// ⚠️ **写死在这里，而不是从主题里取**：这一组要断的就是「和上游一致」，
// 从主题里取就变成自己跟自己比了。
const (
	toastUpstreamDanger  = "#f31260"
	toastUpstreamWarning = "#f5a524"
	toastUpstreamSuccess = "#17c964"
	// toastUpstreamPrimary 是主色（Biu 的绿，用户能在设置里换）。
	toastUpstreamPrimary = "#1ed760"
)

// TestTheToastTakesTheToneColor 验四种语义色画出来是**四个不同的颜色**。
//
// 这一条是票 #180 的头一条验收项。此前所有提示都是主色绿——「下载失败」也是
// 绿的，而用户是看颜色才知道结果的（规格 #163 的用户故事 74）。
//
// ⚠️ **深浅两套都要跑**：上游那三个语义色深浅同值，而**主色那一档**在深色
// 底下不一样（`buildTheme` 的 `applyAccent` 会推 hover / pressed 那一组）——
// 只跑浅色的话深色下那一面一次都没被验过。
//
// ⚠️ **一次只画一条**：同时最多 3 条（`toastLimit`），四条一起画的话最旧的
// 那条会被挤掉，采到的就少一个颜色。
func TestTheToastTakesTheToneColor(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := newShell(t, systemIsDark(dark))
			shortToasts(t, time.Minute)

			seen := map[string]bool{}
			for _, c := range []struct {
				tone notice.Tone
				want string
				ink  string
				what string
			}{
				// ⚠️ **字色也是这一档的一部分**（DESIGN.md §六「Toast」）：
				// 绿和橙上压的是**深色字**（白字压在那两个上只有 2.19:1 / 2.04:1，
				// 读不清），红上压的是**白字**（与确认框那颗红色的「确认」键一致），
				// 主色那一档的字色跟着主题走。
				{notice.Plain, toastUpstreamPrimary, "#18181b", "普通（主色）"},
				{notice.Success, toastUpstreamSuccess, "#18181b", "成功"},
				{notice.Warning, toastUpstreamWarning, "#18181b", "警告"},
				{notice.Danger, toastUpstreamDanger, "#ffffff", "失败"},
			} {
				text := "这一条是" + c.what
				s.addToast(text, c.tone)
				tt.Frame()
				settleToast(t, tt)

				got := toastColorAt(t, tt, text)
				if got != toastRGBA(c.want) {
					t.Errorf("%s那一条的卡片底色是 %v，want %s（上游的色相）", c.what, got, c.want)
				}
				if seen[c.want] {
					t.Errorf("%s和前面某一条是同一个颜色（%s）——四种语义色必须是四个颜色", c.what, c.want)
				}
				seen[c.want] = true

				// 字色：字形最深处那一个像素就是它（见 toastInkColor）。
				//
				// ⚠️ **主色那一档的字色跟着主题走**（DESIGN.md §六）：用户把主色
				// 换成深的就该配白字——所以这一档的期望值取自主题（`AccentText`），
				// 而那三个语义色的字色是**写死的**（绿和橙上压深色字、红上压白字）。
				wantInk := toastRGBA(c.ink)
				if c.tone == notice.Plain {
					wantInk = rgbaOf(BuildTheme(s.app.SettingsSnapshot(), dark).AccentText)
				}
				if ink := toastInkColor(t, tt, text); ink != wantInk {
					t.Errorf("%s那一条的字色是 %v，want %v（压在这个底色上的字色）", c.what, ink, wantInk)
				}

				// 收掉这一条，下一条从空屏开始（不然三条会叠起来）。
				s.clearToasts()
				tt.Frame()
			}
		})
	}
}

// TestAFailedDownloadIsNotGreen 钉住「下载失败不再是绿的」。
//
// 这一条是票 #180 点名的那一条：上游那 164 处调用是按调用给 `color` 的
// （danger 84 / success 58 / warning 22），而我们此前**一律主色**——于是
// 「下载没成」和「下载成了」在画面上是同一个绿。
//
// 判据落在**画面上那个像素**：失败那一条必须是危险色（红），成功那一条是
// 成功色（绿）。只断「addToast 传了什么参数」的话，改成别的颜色也照样绿。
func TestAFailedDownloadIsNotGreen(t *testing.T) {
	s, tt := newShell(t)
	shortToasts(t, time.Minute)

	s.sayDownloadDone("下载", errors.New("网络不通"))
	tt.Frame()
	settleToast(t, tt)

	failed := toastColorAt(t, tt, "下载没成：")
	if want := toastRGBA(toastUpstreamDanger); failed != want {
		t.Errorf("下载失败那一条的底色是 %v，want %s（失败是红的，不是绿的）", failed, toastUpstreamDanger)
	}

	// 成功那一条是绿的——两条不一样，才说明颜色是**跟着结果走**的。
	s.clearToasts()
	tt.Frame()
	s.sayDownloadDone("下载", nil)
	tt.Frame()
	settleToast(t, tt)

	ok := toastColorAt(t, tt, "已加进下载队列")
	if want := toastRGBA(toastUpstreamSuccess); ok != want {
		t.Errorf("下载成功那一条的底色是 %v，want %s（成功是绿的）", ok, toastUpstreamSuccess)
	}
}

// TestTheToastSlidesIn 验入场有**中间态**（推钟验）。
//
// 上游是 `appearance-in 250ms ease-out`（HeroUI Toast 的默认入场）。此前我们
// 的提示是「啪」地一下出现——而「啪」地出现的那一帧，界面是跳的。
//
// ⚠️ **判据是画面上那个像素**：提示是**淡**进来的，所以中途那一帧卡片是半
// 透明的——采到的颜色既不是窗口底色，也不是满色。只量「有没有画出来」的
// 测试量不出中间态（头尾两帧都是「画出来了」）。
//
// ⚠️ **推钟推 60ms 而不是 125ms**：ease-out 在一半处已经走了 87.5%（
// `1-(1-0.5)³`），那时候的颜色离满色只剩一截，容差不好定。60ms 处走了约
// 58%，离两头都还远，量得准。
func TestTheToastSlidesIn(t *testing.T) {
	s, tt := newShell(t)
	shortToasts(t, time.Minute)

	const text = "它是从右边滑进来的"
	s.addToast(text, notice.Plain)
	tt.Frame()

	r, ok := tt.Find(text)
	if !ok {
		t.Fatalf("提示没画出来，文字有 %q", tt.Texts())
	}
	x, y := toastSampleAt(r)
	full := toastRGBA(toastUpstreamPrimary)

	// 第一帧：刚出现，几乎全透明——采到的是它底下那一片（不是卡片底色）。
	start := tt.Image().RGBAAt(x, y)
	if start == full {
		t.Errorf("刚出现那一帧卡片已经是满色了（%v）——入场不是从零开始的", start)
	}
	// ⚠️ 动画在跑：这一帧之后界面还要帧（少了这一句，动画停在半路，
	// 而那一帧是几乎全透明的那一帧——提示就永远看不见了）。
	if !tt.FramePending() {
		t.Error("入场动画还没走完，界面却不要帧了——动画停在半路")
	}

	// 中间态：推 60ms，卡片是半透明的。
	tt.Advance(60 * time.Millisecond)
	tt.Frame()
	mid := tt.Image().RGBAAt(x, y)
	if mid == start {
		t.Errorf("入场 60ms 时颜色和第一帧一样（%v）——没有中间态", mid)
	}
	if mid == full {
		t.Errorf("入场 60ms 时卡片已经是满色了——中间态没量到")
	}
	// 中间态是**朝着卡片底色去的**：离满色比第一帧近。
	if colorDistance(mid, full) >= colorDistance(start, full) {
		t.Errorf("入场 60ms 时颜色没有更接近卡片底色（起 %v → 中 %v，满色 %v）", start, mid, full)
	}

	// 走完：满色。
	settleToast(t, tt)
	done := tt.Image().RGBAAt(x, y)
	if done != full {
		t.Errorf("入场走完之后卡片的颜色是 %v，want %s", done, toastUpstreamPrimary)
	}
}

// TestTheToastEnterHonoursReduceMotion 「减少动态效果」时提示一次到位。
//
// DESIGN.md 的「减少动态效果」那一节：所有自己写的过渡都要听系统偏好，为真
// 时直接到终点。这一条量的是**第一帧就是满色**（而不是淡入 halfway）。
func TestTheToastEnterHonoursReduceMotion(t *testing.T) {
	s, tt := newShell(t, withPrefs(mygoui.Preferences{ReduceMotion: true}))
	shortToasts(t, time.Minute)

	const text = "它不淡入"
	s.addToast(text, notice.Plain)
	tt.Frame()

	full := toastRGBA(toastUpstreamPrimary)
	if got := toastColorAt(t, tt, text); got != full {
		t.Errorf("减少动态效果时第一帧卡片的颜色是 %v，want %s（直接到终点）", got, toastUpstreamPrimary)
	}
}

// TestTheToastUsesTheUpstreamSize 钉住尺寸与位置（票 #180 的 G23）。
//
// 上游那一档（`@heroui/theme` 的 toast 与 `app.tsx:167-175` 的参数）：
//
//	宽 356 / `p-3`（12，四边一样）/ 字 14/500 / 抬过底部 90 / 离右边 4~8
//
// 此前是 320 / 12,16 / 13/400 / 抬 106（90+16）/ 离右边 16。
//
// 判据是**画面上量出来的框**：文字那一块的框是排版排出来的，卡片比它大四周
// 各 12（`p-3`），而卡片的位置就是「离右边 8、抬过底部 90」。
func TestTheToastUsesTheUpstreamSize(t *testing.T) {
	s, tt := newShell(t)
	shortToasts(t, time.Minute)

	const text = "这一条要量尺寸"
	s.addToast(text, notice.Plain)
	tt.Frame()
	settleToast(t, tt)

	r, ok := tt.Find(text)
	if !ok {
		t.Fatalf("提示没画出来，文字有 %q", tt.Texts())
	}
	card := toastCardOf(r)
	const w, h = 1200, 800

	if card.W != 356 {
		t.Errorf("卡片是 %v 宽，want 356（上游 `@heroui/theme` 的 toast）", card.W)
	}
	if right := w - (card.X + card.W); right < 4 || right > 8 {
		t.Errorf("卡片离右边 %v，want 4~8（上游那一档）", right)
	}
	if bottom := h - (card.Y + card.H); bottom != 90 {
		t.Errorf("卡片离底边 %v，want 90（Biu 的 toastOffset）", bottom)
	}
	// 内边距：文字的框往里退 12（四边一样，`p-3`）。
	if got := r.X - card.X; got != toastPad {
		t.Errorf("文字离卡片左边 %v，want %v（`p-3`）", got, toastPad)
	}
	if got := card.Y + card.H - (r.Y + r.H); got != toastPad {
		t.Errorf("文字离卡片下边 %v，want %v（`p-3`）", got, toastPad)
	}
	// 字号：文字盒高折算（`textBoxPerSize` 是那一族量出来的比值）。
	if got := r.H / textBoxPerSize; got < 13.5 || got > 14.5 {
		t.Errorf("折算出来的字号是 %.1f，want 14（上游 `text-sm`）", got)
	}
}

// TestTheToastTextUsesTheMediumWeight 钉住提示那行字是 **14/500**。
//
// 字重画不出盒子的差别（400 和 500 的行高一样，只有笔画粗细不同），所以这一
// 条只能钉代码：画提示那一句同时要有 14 的字号和 `weightMedium`。
//
// （500 那一档**真的画得比 400 粗**由 `typography_test.go` 的
// `TestTheMediumWeightIsDrawnHeavierThanNormal` 证明。）
func TestTheToastTextUsesTheMediumWeight(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "toast.go", nil, 0)
	if err != nil {
		t.Fatalf("解析 toast.go 失败：%v", err)
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		stmt, ok := n.(*ast.ExprStmt)
		if !ok {
			return true
		}
		has14, hasMedium := false, false
		ast.Inspect(stmt, func(m ast.Node) bool {
			call, ok := m.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch callName(call) {
			case "FontSize":
				if len(call.Args) == 1 && mentionsIdent(call.Args[0], "toastFont") {
					has14 = true
				}
			case "FontWeight":
				if len(call.Args) == 1 && mentionsIdent(call.Args[0], "weightMedium") {
					hasMedium = true
				}
			}
			return true
		})
		if has14 && hasMedium {
			found = true
		}
		return true
	})
	if !found {
		t.Error("toast.go 里没有一句同时写着 `FontSize(…toastFont…)` 和 " +
			"`FontWeight(weightMedium)`——提示那行字必须是 14/500")
	}
}

// TestTheToastMessagesKeepTheirBrackets 钉住「提示那句话没缺字」。
//
// 这一票把 120 来处调用点逐条改写了一遍（加语气参数），而**改写的时候少打
// 一个字符，用户看到的就是半截话**——实测就发生过两处：「已添加「音乐，正在扫描」
// 和「已从列表里移除「音乐，文件一个没动」（右引号没了）。
//
// ⚠️ **那种错没有一处会报错**：编译得过、界面画得出来、字还在，只是断句是断的。
// 所以这里逐条看那些话：**「」成对**（圆括号、书名号同理）。
//
// 判据是 AST：凡是**最后一个参数是 `notice.XXX` 的那个调用**，它前面那个参数
// 就是「话」——那一串里的字符串字面量按顺序拼起来，括号必须成对。
// （拼的是**字面量**那一段：话里夹着变量（目录名、歌名）时，那一段不在判据里。）
func TestTheToastMessagesKeepTheirBrackets(t *testing.T) {
	// 这两个目录：话有的在界面层（`addToast` / `say`），有的在应用层
	// （`sayFolder` / `finishRefresh` 那一族——那边写的也是给用户看的话）。
	for _, dir := range []string{".", "../app"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, name, nil, 0)
			if err != nil {
				t.Fatalf("解析 %s 失败：%v", name, err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				idx := -1
				for i, a := range call.Args {
					sel, ok := a.(*ast.SelectorExpr)
					if !ok {
						continue
					}
					pkg, ok := sel.X.(*ast.Ident)
					if !ok || pkg.Name != "notice" {
						continue
					}
					switch sel.Sel.Name {
					case "Plain", "Success", "Warning", "Danger":
						idx = i
					}
				}
				if idx <= 0 {
					return true
				}
				var msg strings.Builder
				ast.Inspect(call.Args[idx-1], func(m ast.Node) bool {
					lit, ok := m.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						return true
					}
					v, err := strconv.Unquote(lit.Value)
					if err == nil {
						msg.WriteString(v)
					}
					return true
				})
				text := msg.String()
				if text == "" {
					return true
				}
				for _, pair := range []struct{ open, close string }{
					{"「", "」"}, {"（", "）"}, {"《", "》"},
				} {
					if strings.Count(text, pair.open) != strings.Count(text, pair.close) {
						pos := fset.Position(call.Pos())
						t.Errorf("%s:%d 的话「%s」里的 %s%s 不成对——"+
							"用户看到的是半截话，而这种错没有任何一处会报错",
							pos.Filename, pos.Line, text, pair.open, pair.close)
					}
				}
				return true
			})
		}
	}
}
