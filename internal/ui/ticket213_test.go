// 票 #367 改版：「我的关注」左边那列分组收成页头右上角的一枚下拉。
//
// 这一票把那一列从「200 宽 × 全高的常驻栏」改成「页头上一枚『全部关注 ▾』下拉」：
// 选组是主动动作（分组是用户自己建的，想看有哪些分组是自己点开的），常驻一栏只为
// 这一件事占地——内容区现在一行顶到窗口右缘。
//
// # 判据是**画面上的像素**和**弹层里摆出来的字**
//
// 原来钉在左栏上的那几条（「全部关注」和分组分得开、长名字截断不换行、三态都在），
// 收成下拉之后一样都不能少——只是它们现在活在一个**弹层**里：断言前先点开触发器
// （`tt.Click("选择关注分组")`），不然弹层里的字根本不在画面上。
//
// ⚠️ **深浅两套都要跑**：高亮那一档（实心主色底）在浅色下和底色差得很远、在深色下
// 只差十几个通道——只看浅色的话深色下那一层根本验不出来。
package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"image"
	"image/color"
	"net/http"
	"strconv"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// followTagSpec 是一个分组（id / 名字 / 人数）。
type followTagSpec struct {
	id    int
	name  string
	count int
}

// followPaneTags 是这一组测试用的那一份分组（顺序固定）。
//
// ⚠️ **有一个名字特别长**：分组名是用户自己起的，而「太长怎么办」是这一票要
// 定的事之一（截断？换行？）。
var followPaneTags = []followTagSpec{
	{id: -10, name: "特别关注", count: 1},
	{id: 0, name: "默认分组", count: 1},
	{id: 777, name: "常听的那些不太熟但是每首都想循环到会唱完整首的 UP 主", count: 1},
}

// uiFollowTagsOrdered 给分组那一列装一份**顺序固定**的返回。
//
// ⚠️ **`uiFollowTags`（`follow_test.go`）用的是 map**，而 map 的遍历顺序是随机的
// ——每一轮跑出来的分组次序都不一样。而这一票要断言「第一项和后面那几项分得
// 开」，那要一个**确定的顺序**。
func uiFollowTagsOrdered(b *uiExplore, tags []followTagSpec) {
	b.on("/x/relation/tags", func(w http.ResponseWriter, r *http.Request) {
		list := make([]any, 0, len(tags))
		for _, t := range tags {
			list = append(list, map[string]any{
				"tagid": t.id, "name": t.name, "count": t.count, "tip": "",
			})
		}
		uiExploreJSON(w, 0, list)
	})
}

// followPaneSettings 是这一组测试用的那份设置。
//
// ⚠️ **方案要显式写暖纸**：这一组量的是**暖纸**这一套，而经典那一套的 `sidebar`
// 就是页面底色（两个都是白）——有些判据在它下面恒真（2026-10-25 的评审抓出来
// 的那一处同形）。
func followPaneSettings() app.Settings {
	st := app.DefaultSettings("")
	st.Accent = app.AccentCinnabar
	return st
}

// followPaneShell 造一个停在「我的关注」那一页的壳（暖纸方案——默认那一套）。
//
// 分组用 `followPaneTags`（顺序固定），关注对象三个。
func followPaneShell(t *testing.T, dark bool) (*Shell, *mygoui.Tester) {
	t.Helper()
	b := newUIExplore(t)
	uiFollowTagsOrdered(b, followPaneTags)
	uiFollowList(b, 3, []any{
		uiFollowUser(11, "第一个 UP"), uiFollowUser(12, "第二个 UP"),
		uiFollowUser(13, "第三个 UP"),
	})
	uiFollowSearch(b, "", nil)
	s, tt, _ := newSpaceShell(t, b, true, "77", systemIsDark(dark),
		withSettings(followPaneSettings()))
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "分组到了", func() bool { return f.tagStarted && len(f.tags) == 3 })
	tt.Frame()
	return s, tt
}

// openFollowGroupSelect 点开页头右上角那枚分组下拉。
//
// ⚠️ **靠 Label 找**：触发器写的是当前那一格的名字（「全部关注」/ 某一组 /
// 「我的关注」），那一格换组就变，而 Label 不变（`openSelect` 同一个理由）。
//
// ⚠️ **不验「摆开之后画面上有什么」**：弹层里有三态（加载中 / 拉失败了 / 正常
// 名单），三态摆的东西互不相同——验「全部关注(N) 在不在」会把前两态挡在外面。
// 点开没点开由各调用方自己的断言兜底（它们都在找弹层里具体的字）。
func openFollowGroupSelect(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	if err := tt.Click("选择关注分组"); err != nil {
		t.Fatalf("点不到分组下拉：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
}

// TestTheFollowGroupSelectSeparatesAllFromTheGroups 钉住「全部关注(N)」和分组项
// 分得开——它们不是一伙的：前一个是「我关注的人」这个全集，后一个是「B 站上我
// 自己建的分组」。摆成同一个样子的时候用户分不清「这一格是全部」还是「这一格
// 也是一个分组」。
//
// 判据是**中间那道线**：它画在第一项和第一个分组之间，线两侧是同色（弹层面板
// 是页面底色），所以只能靠线本身分——而它得是**主题的分隔线**（`t.Border`），
// 不是随便一道什么线。
func TestTheFollowGroupSelectSeparatesAllFromTheGroups(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			_, tt := followPaneShell(t, dark)
			openFollowGroupSelect(t, tt)
			theme := BuildTheme(followPaneSettings(), dark)
			img := tt.Image()

			cur, ok := tt.Find("全部关注(3)")
			if !ok {
				t.Fatalf("分组下拉里没有「全部关注(3)」那一项，文字有 %q", tt.Texts())
			}
			first, ok := tt.Find("特别关注(1)")
			if !ok {
				t.Fatalf("分组下拉里没有「特别关注(1)」那一项，文字有 %q", tt.Texts())
			}
			second, ok := tt.Find("默认分组(1)")
			if !ok {
				t.Fatalf("分组下拉里没有「默认分组(1)」那一项，文字有 %q", tt.Texts())
			}

			paneBg := rgbaOf(theme.Background)
			// 线的颜色：主题的分隔线压在面板底上。
			line := rgbaOf(theme.Border.Over(rgbaToColor(paneBg)))
			// 高亮那一档的实心底——**它是这一带另一种「不是线」的色**，
			// 判据必须把它排除掉（理由见 findSeparator）。
			selBg := rgbaOf(theme.Accent)

			y := findSeparator(img, cur.X, cur.X+cur.W, cur.Y+cur.H, first.Y, paneBg, selBg, line)
			if y < 0 {
				t.Errorf("「全部关注(3)」和「特别关注(1)」之间没有分界——" +
					"那一项（全部关注）和后面那些（分组）不是一伙的")
				return
			}
			// ⚠️ **分界只在第一项和分组之间**：分组和分组之间**不该**有线，
			// 否则弹层看起来像一列表格。
			if y2 := findSeparator(img, first.X, first.X+first.W, first.Y+first.H, second.Y,
				paneBg, selBg, line); y2 >= 0 {
				t.Errorf("「特别关注(1)」和「默认分组(1)」之间也有一道线（y=%d）——"+
					"分组和分组之间不该有线", y2)
			}

			// 🔴 **另一条独立的判据：分界那一带比别处宽**。
			//
			// 第一项和第一个分组之间要留出分界线自己的上下留白（4 + 4），而分组
			// 和分组之间只有项与项之间那一点缝。所以前者的字距**明显更大**——线
			// 被删掉（或者被换成一段空隙）的话，这一条也会红。
			//
			// ⚠️ **量的是「字」之间的距离**：项之间那点缝在两处都有，只有
			// 分界那一带多出分界线自己的留白。
			gapFirst := first.Y - (cur.Y + cur.H)
			gapSecond := second.Y - (first.Y + first.H)
			if gapFirst < gapSecond+4 {
				t.Errorf("「全部关注(3)」和「特别关注(1)」之间只隔 %.1f，而两个分组"+
					"之间隔 %.1f——分界线自己的留白没了", gapFirst, gapSecond)
			}
		})
	}
}

// findSeparator 找 y0~y1 之间那道横线，返回它在 y 上的位置；找不到是 -1。
//
// # 一行要算「那道线」，得同时满足三样
//
//  1. **不是面板的底色**（`bg`）——底色的行不是线；
//  2. **不是高亮那一档的实心底**（`sel`）——⚠️ **这一条是必需的**（2026-10-25
//     的评审抓出来的同形坑）：高亮那一格铺着实心底，而实心底离那道线**比离底色
//     还近**——只判「离线更近」的话，实心底自己会被当成线，于是**把线删掉测试
//     照样绿**。
//  3. **离那道线更近**（或者至少不比离底色远）——线的那一档压上去的。
//
// ⚠️ **判据是「更靠近线那一档」，不是「等于线那个色」**：那一行的位置是算出来
// 的，可能是半个像素，于是那一道线被摊到两行上，每一行都是线和底的混合。
// 「更靠近线」这个判据两种情形都过，而「等于」那种过不了。
func findSeparator(img *image.RGBA, x0, x1, y0, y1 float32, bg, sel, line color.RGBA) int {
	for y := int(y0); y < int(y1); y++ {
		run := 0
		for x := int(x0); x < int(x1); x++ {
			p := img.RGBAAt(x, y)
			if maxDiff(p, bg) < 6 {
				// 和底色一样 → 这一行上没有线。
				run = 0
				continue
			}
			if maxDiff(p, sel) < 6 {
				// 和高亮那一档的实心底一样 → 那是那一项的底，不是线。
				run = 0
				continue
			}
			if maxDiff(p, line) > maxDiff(p, bg) {
				run = 0
				continue
			}
			run++
			// ⚠️ **要一整段**：一两个像素是抗锯齿的碎屑，不是一道线。
			if run >= 8 {
				return y
			}
		}
	}
	return -1
}

// TestTheFollowGroupSelectTruncatesALongGroupName 钉住「分组名太长怎么办」。
//
// 答：**截断，不换行**——和侧栏收藏夹那一行、上游的 `truncate` 是同一个做法。
//
// 三条判据：
//
//  1. **那个名字一行放不下**（量出来比弹层能给的宽还长）——所以它**必须**被
//     截断，否则就该换行了。
//  2. **它就是一行**：那一项的字和旁边那些短名字的字一样高（换行的话会高出一截，
//     把下面那些分组整体顶下去）。
//  3. **它没有越过弹层的右缘**（弹层 `MaxWidth` 就是它的硬边界）。
func TestTheFollowGroupSelectTruncatesALongGroupName(t *testing.T) {
	// ⚠️ **名字后面还跟着人数**（`全部关注(3)` 那一档的写法）：量宽度和找那一项
	// 用的都是**画出来的那一句**。
	const long = "常听的那些不太熟但是每首都想循环到会唱完整首的 UP 主(1)"

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			_, tt := followPaneShell(t, dark)
			openFollowGroupSelect(t, tt)

			short, ok := tt.Find("特别关注(1)")
			if !ok {
				t.Fatalf("分组下拉里没有「特别关注(1)」那一项，文字有 %q", tt.Texts())
			}

			// 判据一：**那一句本身有多宽**比「弹层有多宽」重要。
			//
			// ⚠️ **量的是同一个字号**（弹层里那一项是 14，见 `follow.go`）。
			var measured float32
			mygoui.NewTester(func(c *mygoui.Context) {
				measured, _ = c.MeasureText(0, mygoui.Span{Text: long, Size: 14})
			}, 300, 60).Frame()
			if measured <= short.W {
				t.Skipf("那一句只有 %.1f 宽，这一项有 %.1f——放得下，这一票没测到"+
					"「太长怎么办」", measured, short.W)
			}

			// 判据二：**它就是一行**：字的高度和旁边那一个短名字一样。
			longBox, ok := tt.Find(long)
			if !ok {
				t.Fatalf("分组下拉里没有那个长名字，文字有 %q", tt.Texts())
			}
			if longBox.H > short.H+1 {
				t.Errorf("那个长名字占 %.1f 高，而短名字占 %.1f——它换行了，"+
					"下面那些分组会被顶下去", longBox.H, short.H)
			}

			// 判据三：**没有越过弹层的右缘**。
			//
			// ⚠️ **右缘是弹层里其它项的右缘**：弹层一整列对齐，最宽那一项的右缘
			// 就是弹层的内缘——长名字那一项不许伸出去。
			cur, ok := tt.Find("全部关注(3)")
			if !ok {
				t.Fatalf("分组下拉里没有「全部关注(3)」那一项，文字有 %q", tt.Texts())
			}
			if longBox.X+longBox.W > cur.X+cur.W+1 {
				t.Errorf("那个长名字伸到 x=%.1f，而「全部关注(3)」那一项到 x=%.1f——"+
					"它溢出弹层了", longBox.X+longBox.W, cur.X+cur.W)
			}
		})
	}
}

// TestTheFollowGroupSelectHasThreeStates 钉住弹层里的三态：
// **加载中 / 拉失败了 / 没有分组**。
//
// 三句**互不通用**：用户读到「还没有分组」时不知道该做什么，而这三种情形要做的
// 下一步完全不同（等 / 重试 / 去建分组）。
//
// ⚠️ **三句都在弹层里**：断言前都要先点开触发器——不然弹层根本没摆开，画面上
// 一句都没有。
func TestTheFollowGroupSelectHasThreeStates(t *testing.T) {
	t.Run("加载中", func(t *testing.T) {
		b := newUIExplore(t)
		gate := make(chan struct{})
		defer close(gate)
		b.on("/x/relation/tags", func(w http.ResponseWriter, r *http.Request) {
			<-gate
			uiExploreJSON(w, 0, []any{})
		})
		uiFollowList(b, 1, []any{uiFollowUser(11, "第一个 UP")})
		uiFollowSearch(b, "", nil)
		s, tt, _ := newSpaceShell(t, b, true, "77", withSettings(app.DefaultSettings("")))
		s.navigate(followPath)
		tt.Frame()
		homeFrame(tt)

		openFollowGroupSelect(t, tt)
		if !showsText(tt, "正在加载分组…") {
			t.Errorf("分组还在拉，弹层里却没有「正在加载分组…」，文字有 %q", tt.Texts())
		}
		if showsText(tt, "还没有分组") {
			t.Error("分组还在拉，弹层里却说了「还没有分组」——那两件事不一样")
		}
	})

	t.Run("拉失败了", func(t *testing.T) {
		b := newUIExplore(t)
		b.on("/x/relation/tags", func(w http.ResponseWriter, r *http.Request) {
			uiExploreJSON(w, -412, nil)
		})
		uiFollowList(b, 1, []any{uiFollowUser(11, "第一个 UP")})
		uiFollowSearch(b, "", nil)
		s, tt, _ := newSpaceShell(t, b, true, "77", withSettings(app.DefaultSettings("")))
		s.navigate(followPath)
		tt.Frame()
		f := s.followPageOf()
		waitFrames(t, tt, "分组拉失败了", func() bool { return f.tagErr != nil })
		homeFrame(tt)

		openFollowGroupSelect(t, tt)
		if !showsText(tt, "分组没拉出来") {
			t.Errorf("分组拉失败了，弹层里却没有说「分组没拉出来」，文字有 %q", tt.Texts())
		}
		if !showsText(tt, "重试拉分组") {
			t.Errorf("分组拉失败了，却没有「重试拉分组」那一枚，文字有 %q", tt.Texts())
		}
		if showsText(tt, "还没有分组") {
			t.Error("分组拉失败了，弹层里却说了「还没有分组」——用户会以为他真的没建过")
		}
	})

	t.Run("没有分组", func(t *testing.T) {
		b := newUIExplore(t)
		uiFollowTagsOrdered(b, nil)
		uiFollowList(b, 0, []any{})
		uiFollowSearch(b, "", nil)
		s, tt, _ := newSpaceShell(t, b, true, "77", withSettings(app.DefaultSettings("")))
		s.navigate(followPath)
		tt.Frame()
		f := s.followPageOf()
		waitFrames(t, tt, "分组到了（空的）", func() bool { return f.tagStarted })
		homeFrame(tt)

		openFollowGroupSelect(t, tt)
		if !showsText(tt, "还没有分组") {
			t.Errorf("一个分组都没有，弹层里却没有说「还没有分组」，文字有 %q", tt.Texts())
		}
		if showsText(tt, "正在加载分组…") {
			t.Error("分组已经拉到了（是空的），弹层里却还在说「正在加载分组…」")
		}
	})
}

// TestTheFollowGroupSelectShowsTheCurrentGroup 钉住「收起那一格写的是当前那一格
// 的名字」——停在「全部关注」写「全部关注」，停在某一组写那一组的名字（上游
// `index.tsx:206` 的 `<h1>` 同一个口径）。塌回「我的关注」的话，用户以为自己在
// 看全部，而画面上摆的是某一组的人。
func TestTheFollowGroupSelectShowsTheCurrentGroup(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{777: "常听的"})
	uiFollowList(b, 1, []any{uiFollowUser(11, "全部关注里的那个人")})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 777, []any{uiFollowUser(77, "常听的那个人")})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "第一格到了", func() bool { return len(f.users) == 1 && f.tagStarted })
	tt.Frame()

	// 停在「全部关注」：收起那一格写「全部关注」。
	if !showsText(tt, "全部关注") {
		t.Fatalf("收起那一格没写「全部关注」，文字有 %q", tt.Texts())
	}

	openFollowGroupSelect(t, tt)
	if err := tt.Click("常听的(1)"); err != nil {
		t.Fatalf("下拉里点不到「常听的」：%v，文字有 %q", err, tt.Texts())
	}
	waitFrames(t, tt, "换到那一组", func() bool { return f.tab == followTagTab(777) })
	tt.Frame()

	// 停在某一组：收起那一格写那一组的名字。
	if !showsText(tt, "常听的") {
		t.Errorf("换组之后收起那一格没跟着换，文字有 %q", tt.Texts())
	}
}

// TestTheFollowPaneHasNoHardcodedColorsOrRadii 钉住「颜色和圆角不许写死」。
//
// 这一页的底色、淡底、字色、圆角**全都是从主题现算的**：主题方案换了整套跟着
// 换（暖纸 / 暮光 / 柔雾 / 经典），而写死的色值只对其中一套是对的。
//
// 判据是**源码**：`follow.go` 不许出现颜色字面量，也不许给圆角写一个数。
//
// ⚠️ **认的是整份 `follow.go`**：右边那个网格（`viewFollowCard` 那些）也是从
// 主题取色的，而「取消关注」那一枚药丸的圆角是 **999**（药丸是形状，不跟主题走，
// 见 `followActionPillRadius` 的注释）。所以药丸那一处是**特许**的，其余一处都
// 不许写死。
func TestTheFollowPaneHasNoHardcodedColorsOrRadii(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "follow.go", nil, 0)
	if err != nil {
		t.Fatalf("解析 follow.go 失败：%v", err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "Hex", "RGB":
			t.Errorf("follow.go 里现调了 %s(...)——颜色要从主题取，"+
				"写死的色值只对一套主题是对的", sel.Sel.Name)
		case "Radius":
			// 药丸那一处是形状（999），不是主题圆角。
			if len(call.Args) == 1 {
				if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.INT {
					if lit.Value != strconv.Itoa(followActionPillRadius) {
						t.Errorf("follow.go 里现调了 Radius(%s)——圆角走主题的 `Radius`",
							lit.Value)
					}
				}
			}
		}
		return true
	})
}
