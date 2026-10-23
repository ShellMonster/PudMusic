package ui

import (
	"os"
	"strings"
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// 这一组量的是票 #245 的 E4：**方向键能进出内容区**。
//
// ## 断在哪里
//
// 列表**本身**是能键盘走的：点一行之后连按 ↓，焦点环落在第几行（UX 报告
// §E4 说这一条是好的，别改）。断的是「**进内容区**」这一段——Tab 能走到侧栏，
// 可方向键停在侧栏的按钮上时什么也不发生：按钮不吃方向键，而内容区那一块
// 在焦点链之外，方向键根本到不了它。
//
// 现在：侧栏按 ↓ 走进内容区，内容区的列表在顶上一行时按 ↑ 走回侧栏。

// focusCtx 记住**最后一遍 build 的 context**。
//
// ⚠️ **为什么需要它**：判「焦点现在在哪儿」要用框架的 `Handle.Focused(c)` 和
// `list.Focused(c, state)`，而它们都要一个 `*ui.Context`——Tester 不给。
// 所以这里把整窗的 view 包一层：包一层之前先记下 context，再照旧画。
type focusCtx struct {
	c *mygoui.Context
}

// opt 是装这一层包装的外壳选项。
func (f *focusCtx) opt() shellOpt {
	return withView(func(s *Shell) func(*mygoui.Context) {
		orig := s.View
		return func(c *mygoui.Context) {
			f.c = c
			orig(c)
		}
	})
}

// TestTheArrowKeysWalkBetweenTheSidebarAndTheContent 钉住「方向键能进出内容区」。
//
// 走的是一整圈：**Tab** 进列表 → **↑** 回侧栏 → **↓** 再进列表。
//
// ⚠️ **中间那一步（↑ 回侧栏）是这一条的关键**：只测「↓ 能进内容区」的话，
// 「↑ 把焦点踢出列表」这一半没人管——而那一半坏了的症状是「键盘用户走到列表
// 顶就再也回不去了」。
func TestTheArrowKeysWalkBetweenTheSidebarAndTheContent(t *testing.T) {
	f := newLibFake(t).withLater(1, 3, "第一首", "第二首", "第三首")
	fc := &focusCtx{}
	tt, s := libraryShellOpts(t, f, laterPath, fc.opt())
	// ⚠️ **窗口必须有焦点**，否则框架的 `Focused()` 恒为假（`rt.windowFocus()`），
	// 「焦点在哪」这一整套判据就全是假的。
	tt.SetFocused(true)
	waitFrames(t, tt, "把第一页拉回来", func() bool { return rowsShown(&s.laterPage) == 3 })

	state := &s.laterPage.rows.state
	// ⚠️ **问句柄，不问 `list.Focused`**：那一个走 `c.Resolve(state.Handle)`，
	// 而 `Resolve` 查的是「**这一遍 commit 的** arena」——在测试里拿到的那个
	// context 上它查不到（元素是逐遍重建的，句柄一跨遍就过期）。
	// 句柄自己的 `Focused(c)` 读的是引擎里那个焦点 id，稳。
	inList := func() bool { return state.Handle.Focused(fc.c) }
	inSide := func() bool { return s.sideAnchor.Focused(fc.c) }

	// ① 焦点落在侧栏的第一项上。
	//
	// ⚠️ **用 handle 请求焦点，而不是点它**：点侧栏那一项是**换页**（点了就跳走
	// 了），焦点随之落到新页面上去了——那样测的就不是「焦点在侧栏时按 ↓」了。
	// 框架的 `Handle.Focus` 正是「把焦点给这一项」的那件事（`sideAnchor` 就是
	// 界面层登记的那一枚，见 focus.go）。
	s.sideAnchor.Focus()
	tt.Frame()
	waitFrames(t, tt, "焦点落到侧栏的第一项上", inSide)

	// ② ↓：从侧栏走进内容区。
	tt.Key(0, mygoui.KeyDown)
	tt.Frame()
	waitFrames(t, tt, "焦点从侧栏走进内容区的列表", inList)
	if inSide() {
		t.Error("焦点进了内容区，侧栏上还留着焦点")
	}
	// 走进来就该落在第一行上：光标停在「还没选中任何一行」的话，用户得再按一下
	// 方向键才开始挪。
	if !rowHasRing(t, tt, "第一首") {
		t.Errorf("走进来之后光标没停在第一行上（文字有 %q）", tt.Texts())
	}

	// ③ ↑：列表已经在顶上一行 → 焦点回侧栏的第一项。
	tt.Key(0, mygoui.KeyUp)
	tt.Frame()
	waitFrames(t, tt, "焦点从列表回到侧栏", inSide)
	if inList() {
		t.Error("焦点回了侧栏，列表上还留着焦点")
	}
}

// TestTheContentPageRegistersItsOwnList 钉住「有内容的页登记了自己的列表」。
//
// 判据是 `s.contentFocus`：它是「这一帧内容区里登记的那一个列表」，为空就说明
// 这一页没有登记——侧栏按 ↓ 什么也不会发生（用户看到的是「按了没反应」）。
//
// ⚠️ **只在这一页上有数据时验**：一页**没有内容**的时候画的是空状态，列表压根
// 不建，也就没什么可登记的（那是对的：没有东西可走）。
func TestTheContentPageRegistersItsOwnList(t *testing.T) {
	f := newLibFake(t).withLater(1, 3, "第一首", "第二首", "第三首")
	tt, s := libraryShell(t, f, laterPath)
	waitFrames(t, tt, "把第一页拉回来", func() bool { return rowsShown(&s.laterPage) == 3 })
	if s.contentFocus == nil {
		t.Fatal("稍后再看这一页有列表，却没有登记——方向键进不去内容区")
	}
}

// TestTheListWiringCarriesTheFocusHandover 钉住「接线在」。
//
// 列表**自己**登记自己（见 `widgets/list` 的 `focus.go`），而登记靠的是接线里
// 的那两样。接线漏了一样，症状是「列表不登记自己」或者「到顶了焦点回不去侧栏」
// ——而这两样都只在真窗口里才看得出来。
func TestTheListWiringCarriesTheFocusHandover(t *testing.T) {
	s, _ := newShell(t)
	acts := s.listActions()
	if acts.Focus == nil {
		t.Error("listActions 没装 Focus——列表不会登记自己，方向键进不去内容区")
	}
	if acts.FocusOut == nil {
		t.Error("listActions 没装 FocusOut——列表在顶上一行时焦点回不去侧栏")
	}
}

// TestEveryPageThatDrawsItsOwnListRegistersIt 钉住「自己画列表的页面也登记了」。
//
// ⚠️ **这一条是静态检查**：有些页面的列表不是 `widgets/list` 那个包建的
// （`follow.go` 的分组栏和网格、`dynamicfeed.go` 的作者栏和动态栏、
// `upspace_tabs.go` 的那一张、`library_rows.go` 那一整份），所以登记也得自己
// 写一遍——**漏一遍的症状是「那一页的方向键进不去内容区」**，而用户只以为这个
// 应用有时候键盘不好使。
//
// 判据是**源文件里有没有那句登记**：漏写的话这里当场红。
func TestEveryPageThatDrawsItsOwnListRegistersIt(t *testing.T) {
	for _, name := range []string{
		"follow.go", "dynamicfeed.go", "upspace_tabs.go", "library_rows.go",
	} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读不到 %s：%v", name, err)
		}
		// 两个登记入口：`s.registerContentFocus`（页面自己画的列表）和
		// `acts.Focus`（`pagedRows` 那一整份，它拿不到 Shell）。
		if !strings.Contains(string(b), "registerContentFocus") &&
			!strings.Contains(string(b), "acts.Focus") {
			t.Errorf("%s 自己画了列表，却没有登记它——"+
				"那一页的方向键进不去内容区（见 focus.go）", name)
		}
	}
}
