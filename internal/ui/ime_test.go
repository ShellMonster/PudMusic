package ui

import (
	"image"
	"image/draw"
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// 中文输入法。
//
// # 这一组量的是哪一段
//
// 「中文输入法能不能用」这件事，真机上才验得完——那要有人拿拼音敲一遍。
// 但它不是只有真机能验：拆开看，它由三段组成。
//
//	窗口告诉系统「我接收合成文本」  这一段错了，输入法根本不会来合成，
//	                              敲拼音进去的是一串字母。这是**最致命的一段**，
//	                              而它完全可以在无头环境里量。
//	合成串显示在框里            候选窗浮在框上面，用户看得到自己在拼什么
//	上屏 → 文本落进搜索框 → 提交  这一段是应用的活：Esc 收浮层、回车提交，
//	                              都是我们自己写的回调
//
// 合成事件用框架自带的 `Tester.Compose`（"as while typing in Chinese or
// Japanese"），走的是和真机同一条 `platform.TextComposition` 事件，
// 不是往输入框里塞字符串——塞字符串测的是「赋值」，不是「输入法」。
//
// ⚠️ 真机上仍然要人敲一遍（候选窗、切换输入法、输入源），
// 那一半是 BMG-43 报告里留给人工的那张单子。

// TestTheSearchBoxTellsTheSystemItTakesComposedText 量的是第一段，也是最要命的一段。
//
// 窗口要向系统报「我这儿可以合成」。报成「不接收输入法」的话，
// macOS 根本不会把 `setMarkedText:` 发过来：用户切到拼音输入法，
// 敲「zhongwen」，框里出现的是 `zhongwen` 这七个字母——
// 而界面上看不出任何异常，测试也照样绿（`Type` 走的是另一条路）。
func TestTheSearchBoxTellsTheSystemItTakesComposedText(t *testing.T) {
	_, tt := newShell(t)
	tt.SetFocused(true)
	tt.Frame()

	tt.Click(searchLabel)
	tt.Type("周")

	active, noInputMethod := tt.TextInputActive()
	if !active {
		t.Fatal("点了搜索框之后窗口仍然说「我不接收文本」——输入框没拿到键盘")
	}
	if noInputMethod {
		t.Error("窗口说「我关掉了输入法」：中文输入法的合成根本不会发过来，敲拼音会出来一串字母")
	}
	if _, composing := tt.TextCaret(); !composing {
		t.Error("输入框没有报出合成光标：输入法不知道该把候选窗浮在哪儿")
	}
}

// TestTypingInChineseComposesAndCommits 是合成那两段。
//
// 一次完整的拼音输入是：合成串（zhong）→ 换候选 → 上屏（中文）。
// 中间那一步（候选窗选字）是系统做的，应用这一侧只看得到两端，
// 所以这里量两端：合成串画在框里、上屏之后文本落进 `searchKeyword`。
func TestTypingInChineseComposesAndCommits(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	tt.Frame()
	tt.Click(searchLabel)
	empty := shotOf(tt)

	// 拼音还在候选窗里没上屏。
	tt.Compose("zhong", 5)
	tt.Frame()
	if s.searchKeyword != "" {
		t.Errorf("还没上屏，搜索框里已经有东西了：%q", s.searchKeyword)
	}
	// ⚠️ 这里量的是**画面**而不是状态：合成串是画在框里的字，`Texts()` 里
	// 没有它（无障碍读的是标签和值，不是输入框里正在合成的那串）。
	// 「拼音有没有出现在框里」正是用户看得见的那件事，所以量像素。
	if !shotDiffersIn(t, empty, tt, searchLabel) {
		t.Errorf("合成串没画在框里（框里那一块一个像素都没变）")
	}

	// 换了候选，按空格上屏。空串 = 结束合成，紧跟着的 Type 是上屏的内容。
	preedit := shotOf(tt)
	tt.Compose("", 0)
	tt.Type("中文")
	tt.Frame()
	if s.searchKeyword != "中文" {
		t.Errorf("上屏之后搜索框里是 %q，want 中文", s.searchKeyword)
	}
	if !shotDiffers(preedit, shotOf(tt)) {
		t.Error("上屏之后画面一个像素都没变：拼音还留在框里")
	}
}

// TestSearchingInChineseGoesThroughTheWholeWay 把中文一路送到搜索页。
//
// 这一条量的是**我们自己写的回调**（`submitSearch`）：输入法的合成一定会
// 以「上屏 + 回车」收尾，而回车那个键是我们自己接的 `field.Submitted()`。
// 接错的表现是「词输进去了、搜索页却是空的」。
func TestSearchingInChineseGoesThroughTheWholeWay(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	tt.Frame()
	tt.Click(searchLabel)

	tt.Compose("zhong", 5)
	tt.Compose("", 0)
	tt.Type("周杰伦")
	tt.Frame()
	tt.Key(0, mygoui.KeyEnter)

	if s.router.Path() != searchPath {
		t.Fatalf("回车之后在 %q，want %q", s.router.Path(), searchPath)
	}
	if got := s.searchKeyword; got != "周杰伦" {
		t.Errorf("搜索页上拿到的词是 %q，want 周杰伦", got)
	}
	// 搜过的词要进历史，否则下一次开这个框还是空的。
	if got := s.hist.items; len(got) == 0 || got[0].Value != "周杰伦" {
		t.Errorf("历史里没有「周杰伦」，有 %+v", got)
	}
}

// ---- 小工具：画面快照 ----

// shotOf 是这一帧的像素拷贝。
//
// 必须拷：Tester 的 Image() 给回来的是**下一帧会被重画**的那张图，
// 直接拿两张「前后图」去比，比的可能是同一块内存。
func shotOf(tt *mygoui.Tester) *image.RGBA {
	src := tt.Image()
	dst := image.NewRGBA(src.Rect)
	draw.Draw(dst, src.Rect, src, src.Rect.Min, draw.Src)
	return dst
}

// shotDiffers 报两张快照有没有任何不同。
func shotDiffers(a, b *image.RGBA) bool {
	return !a.Rect.Eq(b.Rect) || !imageEqual(a, b)
}

func imageEqual(a, b *image.RGBA) bool {
	for y := a.Rect.Min.Y; y < a.Rect.Max.Y; y++ {
		for x := a.Rect.Min.X; x < a.Rect.Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				return false
			}
		}
	}
	return true
}

// shotDiffersIn 报「那个名字的元素」那一块有没有变。
//
// 只看框内那一块：输入框一变，清空按钮也会跟着冒出来，那是窗口别处的变化，
// 混进来会让「拼音画出来了」这条永远为真。
func shotDiffersIn(t *testing.T, before *image.RGBA, tt *mygoui.Tester, name string) bool {
	t.Helper()
	rect, ok := tt.Find(name)
	if !ok {
		t.Fatalf("画面上找不到 %q", name)
	}
	after := shotOf(tt)
	for y := max(after.Rect.Min.Y, int(rect.Y)); y < min(after.Rect.Max.Y, int(rect.Y+rect.H)); y++ {
		for x := max(after.Rect.Min.X, int(rect.X)); x < min(after.Rect.Max.X, int(rect.X+rect.W)); x++ {
			if before.RGBAAt(x, y) != after.RGBAAt(x, y) {
				return true
			}
		}
	}
	return false
}
