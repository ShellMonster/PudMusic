// 票 #197 的界面层证据：**点列表行不再整行刷实心主色底**，「正在播放」那一行
// 仍有 flat 的主色高亮。
//
// ⚠️ **这一组量的是外壳上的画面**，不是 `widgets/list` 那一层：那一层量的是控件自己
// 画的画面，而这一层量的是**页面上**那一行——中间隔着外壳、页边距、主题的装配。
// 症状是静默的：控件里画对了、页面上刷出来还是一整块绿，只有读像素才分得出来。
//
// ⚠️ **深浅两套都要量**：主色在两套主题下是同一个值，而「选中那一行」在两套下
// 长得完全不一样（浅色下是一块亮绿，深色下是一块更刺眼的绿）。
package ui

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"
)

// isAccentPixel 报这个像素是不是**实心主色**。
//
// ⚠️ **判的是「和主色几乎一模一样」**，不是「偏主色」：焦点环是半透明的主色压在行沿
// 上的，它偏主色；而实心主色底是**就是**那个主色。两者必须分得开，所以这里比的是
// 差值（通道差之和 ≤ 24）。
func isAccentPixel(p color.RGBA, accent ui.Color) bool {
	d := func(a, b uint8) int {
		if a > b {
			return int(a) - int(b)
		}
		return int(b) - int(a)
	}
	return d(p.R, accent.R)+d(p.G, accent.G)+d(p.B, accent.B) <= 24
}

// hoverTitle 把指针挪到 title 那一行上。
//
// ⚠️ **挪走是必要的**：指针压在哪一行，哪一行就带**悬停**那一档底色。拿悬停着的那
// 一行和另一行比颜色，比的是悬停不是选中。
func hoverTitle(t *testing.T, tt *ui.Tester, title string) {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", title, tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
}

// focusList 把键盘焦点走到列表上（Tab 若干次，直到列表自己那一圈焦点环出现）。
//
// anchor 是这一页上**列表里的某一行**的歌名：列表左沿就是那一行的左沿（行是撑满
// 列表宽度的），而框架画在列表上的那一圈环落在列表盒子外面 3 像素处。
//
// ⚠️ **不写死 Tab 几次**：焦点落在哪取决于这一屏上还有多少能接焦点的东西（顶栏、
// 侧栏、筛选条、每一行行尾那枚「⋯」），写死一个数的话，页面上一加东西就废了。
func focusList(t *testing.T, tt *ui.Tester, anchor string) {
	t.Helper()
	r, ok := tt.Find(anchor)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", anchor, tt.Texts())
	}
	x, y := int(r.X)-2, int(r.Y)+int(r.H)/2
	for range 40 {
		tt.Key(0, ui.KeyTab)
		tt.Frame()
		// 列表左沿外面那一列上有东西 → 列表有焦点（框架画在它外面 3 像素处）。
		if tt.Image().RGBAAt(x, y) != tt.Image().RGBAAt(x-8, y) {
			return
		}
	}
	t.Fatalf("Tab 了 40 次还没走到列表上")
}

// rowHasRing 报那一行的上沿有没有焦点环。
//
// 判据是**那个像素和这一行的底色不是一回事**：焦点环是主题里那个 Focus 色（半透明
// 的主色）压在行沿上的，而没有环的那一行上沿就是行自己的底色。
//
// ⚠️ **不看「离主色多近」**：焦点环是半透明的，它离底色比离主色近；而主色换一个
// 色相（绿的、黄的）那一套判据就飘了。这里量的是「和底色差了多少」。
//
// ⚠️ **行的上沿要**取整**（`round32`），不是截断**（`int`）：行的 Y 可能是小数
// （列表容器在页面里的位置是算出来的，票 #290 给稍后再看页头加了一枚 32 高的
// 按钮之后，那一页的行就落在 286.8 这样的位置上）。截断取到的是**行上面**那一
// 像素（上一行的底色），于是「没有环」的那一行也会被当成有环——判据就废了。
// 取整取到的是行自己的上沿：没有环时那两处是同一个底色，有环时上沿那一处是
// 环的那条线。
func rowHasRing(t *testing.T, tt *ui.Tester, title string) bool {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", title, tt.Texts())
	}
	x := int(r.X + r.W/2)
	y := round32(r.Y)
	if x < 0 || x >= 1200 || y < 0 || y+6 >= 800 {
		t.Fatalf("「%s」那一行在画面外", title)
	}
	// 这一行自己的底色：往下挪 6 像素，避开环，也还在行的内边距里（8）。
	bg := tt.Image().RGBAAt(x, y+6)
	return tt.Image().RGBAAt(x, y) != bg
}
