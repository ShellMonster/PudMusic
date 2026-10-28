// 票 #367：「我的关注」那一页左边那列分组，按当前的设计风格重做。
//
// 这一票只动**左边那一列**（`follow.go` 的 `viewFollowPane` / `viewFollowPaneRow`）：
// 底色、选中态、字号、内边距、三态。**布局骨架（左列 + 右网格）和右边那个网格
// 一个像素没动**。
//
// # 判据是**画面上的像素**，不是函数调用
//
// 「像一家人」只能看画面：底色是不是同一片、选中那一格是不是同一层淡底、字
// 是不是同一档。所以这一组量的是像素（`bandPixels` / `modeOf` / `farthestFrom`），
// 而断「有没有写死颜色」的那一条量的是**源码**（照 `shadow_test.go` 那一族的路子）。
//
// ⚠️ **深浅两套都要跑**：淡底那一层在浅色下和底色差得很远、在深色下只差十几个
// 通道——只看浅色的话深色下那一层根本验不出来。
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
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
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
// ——每一轮跑出来的分组次序都不一样。而这一票要断言「第一格和后面那几格分得
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
// ⚠️ **方案要显式写暖纸，不能靠 `app.DefaultSettings("")` 的默认**：这一组要量的是
// **暖纸**这一套，而**经典那一套的 `sidebar` 就是页面底色**（两个都是白）——于是
// 「这一列的底 == 方案的 `sidebar`」这条判据在经典底下**恒真**：把这一列的底整个
// 删掉，测试照样绿（2026-10-25 的评审抓出来的）。
//
// 暖纸那一套的 `sidebar`（#EFE9DE）和页面底色（#F8F4ED）**不是一个色**，判据
// 才立得住——`TestTheFollowPaneAndTheSidebarAreOneFamily` 里还有一条专门盯
// 「这一套方案里这两个色不是同一个」。
//
// （票 #406 之后这一包的 `TestMain` 不再把方案钉成经典，所以显式写这一行是
// **钉住这一组要量的那一套**，而不是「躲开默认」——两者的区别是：将来默认方案
// 再变，这一组仍然量暖纸。）
func followPaneSettings() app.Settings {
	st := app.DefaultSettings("")
	st.ThemePreset = app.ThemePresetWarmPaper
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

// TestTheFollowPaneAndTheSidebarAreOneFamily 钉住「这一列和侧栏是同一片」。
//
// 三条判据：
//
//  1. **底色是同一片**——侧栏那一栏和这一列都是方案的 `sidebar`（和内容区不同色）。
//     改之前这一列是页面底色，于是它和内容区是一体的，而旁边的侧栏是另一片：
//     两片导航区夹着一片内容区，中间那道界是内容区的边。
//  2. **这一套方案里 `sidebar` 和页面底色不是同一个色**——不然第一条**恒真**
//     （经典那一套的 `sidebar` 就是页面底色，见 `followPaneSettings` 的注释）。
//  3. **选中那一格是同一层淡底**——两列都用 `flatCurrent`（`accentSoft` 淡底 +
//     按那层底现算的字），所以两个色值逐字节相同。
func TestTheFollowPaneAndTheSidebarAreOneFamily(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := followPaneShell(t, dark)
			theme := BuildTheme(followPaneSettings(), dark)
			img := tt.Image()

			// 判据二：**这一套方案里，方案的 `sidebar` 和页面底色不是同一个色**。
			//
			// ⚠️ **这一条是前一条的前提**：两个色一样的话，「这一列的底 == 侧栏的底
			// == 方案的 `sidebar`」恒成立——把这一列的底整个删掉，测试也绿
			//（2026-10-25 的评审抓出来的）。
			sidebar := s.sidebarBackground(theme)
			if nearRGBA(rgbaOf(sidebar), rgbaOf(theme.Background), 3) {
				t.Fatalf("这一套方案里 sidebar（%v）和页面底色（%v）是同一个色——"+
					"「这一列的底是方案的 sidebar」这条判据在它下面恒真，换一套方案",
					sidebar, theme.Background)
			}

			// 判据一：**底色同一片**。
			//
			// ⚠️ **取样点都在两栏的空处**（侧栏 x=190、这一列 x=390，都在底下那
			// 一段空白里）：取在菜单项上会量到那一项的底，取在收藏夹上会量到别
			// 的东西。
			want := rgbaOf(sidebar)
			sidePx := pixelAt(img, 190, 650)
			panePx := pixelAt(img, 390, 650)
			if !nearRGBA(sidePx, want, 3) {
				t.Errorf("侧栏的底色是 %v，want %v", sidePx, want)
			}
			if !nearRGBA(panePx, want, 3) {
				t.Errorf("分组那一列的底色是 %v，want %v——它要和侧栏是同一片"+
					"（方案里的 `sidebar`）", panePx, want)
			}
			// 顺带钉住「内容区是另一片」：内容区的底是**页面底色**，不是 `sidebar`。
			bodyPx := pixelAt(img, 500, 650)
			if !nearRGBA(bodyPx, rgbaOf(theme.Background), 3) {
				t.Errorf("内容区的底色是 %v，want %v（页面底色）", bodyPx, theme.Background)
			}

			// 判据三：**选中那一格是同一层淡底**。
			sideCur, ok := tt.Find("我的关注")
			if !ok {
				t.Fatalf("侧栏上没有「我的关注」那一项，文字有 %q", tt.Texts())
			}
			paneCur, ok := tt.Find("全部关注(3)")
			if !ok {
				t.Fatalf("分组那一列上没有「全部关注(3)」那一格，文字有 %q", tt.Texts())
			}
			sideBg := modeOf(bandPixels(img, sideCur))
			paneBgRow := modeOf(bandPixels(img, paneCur))
			if !nearRGBA(sideBg, paneBgRow, 3) {
				t.Errorf("侧栏当前项的底色是 %v，分组那一列当前项的底色是 %v——"+
					"两列的选中态得是同一个数", sideBg, paneBgRow)
			}
			if !nearRGBA(paneBgRow, rgbaOf(flatCurrentBg(theme)), 3) {
				t.Errorf("分组那一列当前项的底色是 %v，want %v（accentSoft 淡底）",
					paneBgRow, flatCurrentBg(theme))
			}
		})
	}
}

// TestTheFollowPaneSelectedRowIsReadable 钉住「选中那一格：淡底 + 读得出来的字」。
//
// 三条判据缺一不可：
//
//  1. **底是 `accentSoft` 淡底**，不是实心主色（§七「我们比上游好、不要改回去」
//     里那一条；实心主色那一档是「被选中的选项」，不是「当前在哪」）。
//  2. **字是按那层底现算的**（浅色下是压深到 ≥4.5:1 的那一档，深色下是主色）。
//  3. **量出来的对比度 ≥ 4.5:1**——前两条是「配方对不对」，这一条是「屏幕上读不
//     读得出来」。
//
// 另外：**没选中的是次要色**——这一列是第二层导航，选中那一格才是要看的那一格。
func TestTheFollowPaneSelectedRowIsReadable(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			_, tt := followPaneShell(t, dark)
			theme := BuildTheme(followPaneSettings(), dark)
			img := tt.Image()

			cur, ok := tt.Find("全部关注(3)")
			if !ok {
				t.Fatalf("分组那一列上没有「全部关注(3)」那一格，文字有 %q", tt.Texts())
			}
			sib, ok := tt.Find("特别关注(1)")
			if !ok {
				t.Fatalf("分组那一列上没有「特别关注(1)」那一格，文字有 %q", tt.Texts())
			}

			// 底：accentSoft 淡底。
			curBg := modeOf(bandPixels(img, cur))
			if !nearRGBA(curBg, rgbaOf(flatCurrentBg(theme)), 3) {
				t.Errorf("选中那一格的底色是 %v，want %v（accentSoft 淡底）",
					curBg, flatCurrentBg(theme))
			}
			// 字：按那层底现算的那一档。
			//
			// ⚠️ **容差是 24**：字是抗锯齿画的，量到的那个像素是字色和底色的混合。
			curInk := farthestFrom(img, cur, curBg)
			wantInk := flatCurrentText(theme, true)
			if !nearRGBA(curInk, rgbaOf(wantInk), 24) {
				t.Errorf("选中那一格的字色是 %v，want %v", curInk, wantInk)
			}
			// 对比度：屏幕上真的读得出来。
			if r := ink.Ratio(rgbaToColor(curInk), rgbaToColor(curBg)); r < 4.5 {
				t.Errorf("选中那一格的字压在底上只有 %.2f:1（< 4.5）", r)
			}

			// 没选中的那一格：**次要色**。
			//
			// ⚠️ **那一格没有底色**（没选中的行不铺底），所以它的「众数」是这一列
			// 的底色——字要从「离底最远的那一个像素」里量。
			sibBg := modeOf(bandPixels(img, sib))
			sibInk := farthestFrom(img, sib, sibBg)
			if !nearRGBA(sibInk, rgbaOf(theme.TextMuted), 24) {
				t.Errorf("没选中那一格的字色是 %v，want %v（次要色）",
					sibInk, theme.TextMuted)
			}
		})
	}
}

// TestTheFollowPaneSeparatesAllFromTheGroups 钉住「全部关注(N)」和分组格分得开。
//
// 它们不是一伙的：前一个是「我关注的人」这个全集，后一个是「B 站上我自己建的
// 分组」。摆成同一个样子的时候用户分不清「这一格是全部」还是「这一格也是一个
// 分组」。
//
// 判据是**中间那道线**：它画在第一个分组那一行的上面，线两侧是同色（这一列
// 整列都是方案的 `sidebar`），所以只能靠线本身分——而它得是**主题的分隔线**
// （`t.Border`），不是随便一道什么线。
func TestTheFollowPaneSeparatesAllFromTheGroups(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := followPaneShell(t, dark)
			theme := BuildTheme(followPaneSettings(), dark)
			img := tt.Image()

			cur, ok := tt.Find("全部关注(3)")
			if !ok {
				t.Fatalf("分组那一列上没有「全部关注(3)」那一格，文字有 %q", tt.Texts())
			}
			first, ok := tt.Find("特别关注(1)")
			if !ok {
				t.Fatalf("分组那一列上没有「特别关注(1)」那一格，文字有 %q", tt.Texts())
			}
			second, ok := tt.Find("默认分组(1)")
			if !ok {
				t.Fatalf("分组那一列上没有「默认分组(1)」那一格，文字有 %q", tt.Texts())
			}

			paneBg := rgbaOf(s.sidebarBackground(theme))
			// 线的颜色：主题的分隔线压在这一列的底上。
			line := rgbaOf(theme.Border.Over(rgbaToColor(paneBg)))
			// 选中那一格（全部关注）的淡底——**它是这一带另一种「不是线」的色**，
			// 判据必须把它排除掉（理由见 findSeparator）。
			selBg := rgbaOf(flatCurrentBg(theme))

			y := findSeparator(img, cur.X, cur.X+cur.W, cur.Y+cur.H, first.Y, paneBg, selBg, line)
			if y < 0 {
				t.Errorf("「全部关注(3)」和「特别关注(1)」之间没有分界——" +
					"那一格（全部关注）和后面那些（分组）不是一伙的")
				return
			}
			// ⚠️ **分界只在第一格和分组之间**：分组和分组之间**不该**有线，
			// 否则那一列看起来像一列表格。
			if y2 := findSeparator(img, first.X, first.X+first.W, first.Y+first.H, second.Y,
				paneBg, selBg, line); y2 >= 0 {
				t.Errorf("「特别关注(1)」和「默认分组(1)」之间也有一道线（y=%d）——"+
					"分组和分组之间不该有线", y2)
			}

			// 🔴 **另一条独立的判据：分界那一带比别处宽**。
			//
			// 「全部关注」那一格和第一个分组之间要留出分界线自己的上下留白
			// （4 + 4），而分组和分组之间只有行与行之间那 2 的缝。所以前者的
			// 字距**明显更大**——线被删掉（或者被换成一段空隙）的话，这一条
			// 也会红。
			//
			// ⚠️ **量的是「字」之间的距离**：行之间那 2 的缝在两处都有，只有
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
//  1. **不是这一列的底色**（`bg`）——底色的行不是线；
//  2. **不是选中那一格的淡底**（`sel`）——⚠️ **这一条是必需的**（2026-10-25
//     的评审抓出来的）：「全部关注」那一格就铺着淡底，而暖纸浅色下那层淡底
//     （#E0F9E9）离那道线（#DBDBDB）**比离底色还近**——只判「离线更近」的
//     话，淡底自己会被当成线，于是**把线删掉测试照样绿**。
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
				// 和选中那一格的淡底一样 → 那是那一格的底，不是线。
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

// TestTheFollowPaneTruncatesALongGroupName 钉住「分组名太长怎么办」。
//
// 答：**截断，不换行**——和侧栏收藏夹那一行、上游的 `truncate` 是同一个做法。
//
// 三条判据：
//
//  1. **那个名字一行放不下**（量出来比这一列能给的宽还长）——所以它**必须**被
//     截断，否则就该换行了。
//  2. **它就是一行**：那一行的字和旁边那些短名字的字一样高（换行的话会高出一截，
//     把下面那些分组整体顶下去）。
//  3. **它没有越过这一列的右缘**（截断，不是让它溢出到内容区那边去）。
func TestTheFollowPaneTruncatesALongGroupName(t *testing.T) {
	// ⚠️ **名字后面还跟着人数**（`全部关注(3)` 那一档的写法）：量宽度和找那一格
	// 用的都是**画出来的那一句**。
	const long = "常听的那些不太熟但是每首都想循环到会唱完整首的 UP 主(1)"

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			_, tt := followPaneShell(t, dark)
			short, ok := tt.Find("特别关注(1)")
			if !ok {
				t.Fatalf("分组那一列上没有「特别关注(1)」那一格，文字有 %q", tt.Texts())
			}

			// 判据一：**那一句本身有多宽**比「那一格有多宽」重要。
			//
			// ⚠️ **量的是同一个字号**（界面上那一格是 14，见 `follow.go`）。
			var measured float32
			mygoui.NewTester(func(c *mygoui.Context) {
				measured, _ = c.MeasureText(0, mygoui.Span{Text: long, Size: 14})
			}, 300, 60).Frame()
			if measured <= short.W {
				t.Skipf("那一句只有 %.1f 宽，这一格有 %.1f——放得下，这一票没测到"+
					"「太长怎么办」", measured, short.W)
			}

			// 判据二：**它就是一行**：字的高度和旁边那一个短名字一样。
			longBox, ok := tt.Find(long)
			if !ok {
				t.Fatalf("分组那一列上没有那个长名字，文字有 %q", tt.Texts())
			}
			if longBox.H > short.H+1 {
				t.Errorf("那个长名字占 %.1f 高，而短名字占 %.1f——它换行了，"+
					"下面那些分组会被顶下去", longBox.H, short.H)
			}

			// 判据三：**没有越过这一列的右缘**。
			//
			// ⚠️ **右缘是「全部关注(3)」那一格的右缘加一点**：那一格是这一列里
			// 最宽的（它的底色铺满整列），而长名字那一格和它左对齐。
			cur, ok := tt.Find("全部关注(3)")
			if !ok {
				t.Fatalf("分组那一列上没有「全部关注(3)」那一格，文字有 %q", tt.Texts())
			}
			if longBox.X+longBox.W > cur.X+cur.W+1 {
				t.Errorf("那个长名字伸到 x=%.1f，而这一列到 x=%.1f——它溢出去了",
					longBox.X+longBox.W, cur.X+cur.W)
			}
		})
	}
}

// TestTheFollowPaneHasThreeStates 钉住这一列的三态：**加载中 / 拉失败了 / 没有分组**。
//
// 三句**互不通用**：用户读到「还没有分组」时不知道该做什么，而这一列的三种
// 情形要做的下一步完全不同（等 / 重试 / 去建分组）。
func TestTheFollowPaneHasThreeStates(t *testing.T) {
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

		if !showsText(tt, "正在加载分组…") {
			t.Errorf("分组还在拉，这一列上却没有「正在加载分组…」，文字有 %q", tt.Texts())
		}
		if showsText(tt, "还没有分组") {
			t.Error("分组还在拉，这一列上却说了「还没有分组」——那两件事不一样")
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

		if !showsText(tt, "分组没拉出来") {
			t.Errorf("分组拉失败了，这一列上却没有说「分组没拉出来」，文字有 %q", tt.Texts())
		}
		if !showsText(tt, "重试拉分组") {
			t.Errorf("分组拉失败了，却没有「重试拉分组」那一枚，文字有 %q", tt.Texts())
		}
		if showsText(tt, "还没有分组") {
			t.Error("分组拉失败了，这一列上却说了「还没有分组」——用户会以为他真的没建过")
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

		if !showsText(tt, "还没有分组") {
			t.Errorf("一个分组都没有，这一列上却没有说「还没有分组」，文字有 %q", tt.Texts())
		}
		if showsText(tt, "正在加载分组…") {
			t.Error("分组已经拉到了（是空的），这一列上却还在说「正在加载分组…」")
		}
	})
}

// TestTheFollowPaneHasNoHardcodedColorsOrRadii 钉住「颜色和圆角不许写死」。
//
// 这一列的底色、淡底、字色、圆角**全都是从主题现算的**：主题方案换了整套跟着
// 换（暖纸 / 暮光 / 柔雾 / 经典），而写死的色值只对其中一套是对的。
//
// 判据是**源码**：这一列不许出现颜色字面量，也不许给圆角写一个数。
//
// ⚠️ **认的是「这一列」那几处**：整个 `follow.go` 都不许有——右边那个网格
// （`viewFollowCard` 那些）也是从主题取色的，而「取消关注」那一枚药丸的圆角
// 是 **999**（药丸是形状，不跟主题走，见 `followActionPillRadius` 的注释）。
// 所以药丸那一处是**特许**的，其余一处都不许写死。
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
