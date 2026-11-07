package ui

import (
	"image"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	mygoui "github.com/egoist/mygo/ui"
)

// coverBox 在 text（「N 首」那一行）左边、上下各约一枚封面高的那一段里找出封面那一块的外框（和侧栏底差得开的像素的包围盒）。
func coverBox(img *image.RGBA, text mygoui.Rect, bg mygoui.Color) (minX, minY, maxX, maxY int) {
	minX, minY, maxX, maxY = 1<<30, 1<<30, -1, -1
	for y := int(text.Y) - 26; y < int(text.Y)+24; y++ {
		for x := int(text.X) - 50; x < int(text.X)-4; x++ {
			if distRGBA(img.RGBAAt(clampX(img, x), y), bg) > 40 {
				minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
			}
		}
	}
	return
}

// TestTheFavoriteFallbackCoverUsesR4 锁定原型 index.html:198 `.fav-cover { border-radius: var(--r-4) }`：
// 侧栏收藏夹一首封面都没有时画的默认封面也是圆角 4（曾经跟主题圆角 8，角上缺一大块）。
func TestTheFavoriteFallbackCoverUsesR4(t *testing.T) {
	s, _, tt, _ := favShell(t, 1, 0)
	homeFrame(tt)
	// 「N 首」和名字左对齐，就在封面右边 10（按名字找会先找到整行那枚键）。
	text, ok := tt.Find("0 首")
	if !ok {
		t.Fatalf("侧栏上没有收藏夹那一行的「0 首」，文字有 %q", tt.Texts())
	}
	img := tt.Image()
	bg := tok.Of(BuildTheme(s.app.SettingsSnapshot(), false)).Side
	minX, minY, maxX, maxY := coverBox(img, text, bg)
	if maxX-minX < 28 || maxY-minY < 28 {
		t.Fatalf("没量出 32 的默认封面：x %d..%d y %d..%d", minX, maxX, minY, maxY)
	}
	// 圆角 4 时离角 1 像素那一点在弧里面（是封面的色）；圆角 8 时它在弧外面（露出侧栏底）。
	if got := img.RGBAAt(minX+1, minY+1); distRGBA(got, bg) <= 40 {
		t.Errorf("默认封面左上角 (1,1) 是侧栏底 %v——圆角比 4 大", got)
	}
}

// TestTheNewFolderKeyHasNoHoverLook 锁定原型 index.html:180 `.side-label button`：「+」键没有
// 悬停样式（曾经悬停铺 hover 底、图形换正文色）。
func TestTheNewFolderKeyHasNoHoverLook(t *testing.T) {
	_, _, tt, _ := favShell(t, 1, 0)
	homeFrame(tt)
	r, ok := tt.Find("新建收藏夹")
	if !ok {
		t.Fatalf("侧栏上没有「新建收藏夹」键，文字有 %q", tt.Texts())
	}
	tt.Move(10, 700)
	tt.Frame()
	// 量键的下沿那一行（悬停提示在键上方，它的影子会压到上沿）。
	x, y := int(r.X+r.W/2)+6, int(r.Y+r.H)-3
	idle := tt.Image().RGBAAt(x, y)
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	hot := tt.Image().RGBAAt(x, y)
	if maxDiff(idle, hot) > 2 {
		t.Errorf("悬停「+」键时键里的底色从 %v 变成 %v——原型没有悬停底", idle, hot)
	}
}
