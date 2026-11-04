package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"image/color"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/notice"
	mygoui "github.com/egoist/mygo/ui"
)

// Toast 这一组盯的是 Biu 的硬要求：右下角、避开 76px 播放条、同时最多 3 条，
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

// 「深夜唱片行」的语义色（DESIGN.md §0′，深浅各一份）。
//
// ⚠️ **写死在这里，而不是从主题里取**：这一组要断的就是「和令牌表一致」，
// 从主题里取就变成自己跟自己比了。
func toastTone(dark bool) (primary, success, warning, danger string) {
	if dark {
		return "#ff5d3d", "#5bc287", "#f2b04a", "#f0716b"
	}
	return "#d2391b", "#2e7d4f", "#9a5b06", "#c93b3b"
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

// TestTheToastStaysWhileThePointerIsOnIt 钉住「悬停宽限」。
//
// 《Functional Motion》：「当鼠标移入时，所有 Message 将会展开」「悬停时不会
// 消失」——用户多半是照着这一条去按底下的那一行，它在指针还在上面的时候就消失
// 了，那句话就没用了。
//
// ⚠️ **到期走的是真时钟**（后台那条 goroutine 真睡），所以这一条**要真等**——
// 推钟推不动它（`toastEnterAt` 那一档才是引擎钟）。
func TestTheToastStaysWhileThePointerIsOnIt(t *testing.T) {
	s, tt := newShell(t)
	shortToasts(t, 300*time.Millisecond)

	s.addToast("指着它", notice.Plain)
	tt.Frame()
	settleToast(t, tt)
	r, ok := tt.Find("指着它")
	if !ok {
		t.Fatalf("提示没画出来，文字有 %q", tt.Texts())
	}

	// 指针压到这一条上。
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()

	// 等过它该到点的时刻（真时钟）。
	time.Sleep(600 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("指着它") {
		t.Fatalf("指针指着它，它却没了，文字有 %q", tt.Texts())
	}

	// 指针移开：它就该按原定的时刻收走。
	tt.Move(4, 4)
	tt.Frame()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(s.visibleToasts()) > 0 {
		time.Sleep(10 * time.Millisecond)
	}
	tt.Frame()
	if tt.HasText("指着它") {
		t.Errorf("指针移开之后它还赖在画面上，文字有 %q", tt.Texts())
	}
}

// TestTheToastGoesAwayEvenWithoutAPointer 断的是上一条的另一半。
//
// ⚠️ **别把「悬停宽限」做成「永远不消失」**：没有指针指着的时候，它到点就得走。
func TestTheToastGoesAwayEvenWithoutAPointer(t *testing.T) {
	s, tt := newShell(t)
	shortToasts(t, 300*time.Millisecond)

	s.addToast("没人指着我", notice.Plain)
	tt.Frame()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(s.visibleToasts()) > 0 {
		time.Sleep(10 * time.Millisecond)
	}
	tt.Frame()
	if tt.HasText("没人指着我") {
		t.Errorf("没人指着它，它却还赖着，文字有 %q", tt.Texts())
	}
}
