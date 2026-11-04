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
