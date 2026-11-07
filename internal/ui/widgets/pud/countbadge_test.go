package pud

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

func rectsIntersect(a, b ui.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

// TestTheFullPlayerCountSitsOutsideTheGlyph 锁住「全屏播放器里透明底的计数角标不压在图标图形上」：
// 计数文字的矩形落在图形外侧右上角、和图形不相交，也不碰隔 8 的下一枚键的图形；命中区仍是
// 36×36，下一枚键的位置不因角标挪动。
//
// 曾经出错的行为：全屏里角标去掉了 side 底（app.js:1145），却还按普通模式的位置（right -4、
// top 1）摆，「1.2万」整段压在评论图标的线条上，读不出来。
func TestTheFullPlayerCountSitsOutsideTheGlyph(t *testing.T) {
	for _, count := range []string{"1.2万", "999+"} {
		t.Run(count, func(t *testing.T) {
			// 元素句柄出了这一帧就失效，所以在建的时候当场读 Bounds（上一帧的框）。
			var g, x ui.Rect
			countBadgeHook = func(glyph, text ui.Element) { g, x = glyph.Bounds(), text.Bounds() }
			defer func() { countBadgeHook = nil }()
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Row(c).Size(400, 200).Padding(40).Gap(8).Children(func() {
					WithFull(func() {
						IconBtn(c, "cnt", "comment", "评论", IconOpts{Count: count})
						IconBtn(c, "next", "queue", "队列", IconOpts{})
					})
				})
			}, 400, 200)
			tt.Frame()
			tt.Frame() // Bounds 是上一帧的
			if g.W == 0 || x.W == 0 {
				t.Fatalf("没量到：图形 %v，文字 %v", g, x)
			}
			if rectsIntersect(g, x) {
				t.Errorf("计数文字 %v 压在图形 %v 上", x, g)
			}
			if x.Y+x.H > g.Y || x.X+x.W/2 <= g.X+g.W/2 {
				t.Errorf("计数文字 %v 不在图形 %v 的外侧右上角", x, g)
			}
			btn, ok := tt.Find("评论")
			if !ok || btn.W != tok.IconHit || btn.H != tok.IconHit {
				t.Fatalf("命中区 %v，want 36×36", btn)
			}
			next, ok := tt.Find("队列")
			if !ok || next.X != btn.X+btn.W+8 || next.Y != btn.Y {
				t.Fatalf("下一枚键 %v 被挪了（评论键 %v，缝 8）", next, btn)
			}
			inset := (tok.IconHit - tok.Icon) / 2
			nextGlyph := ui.Rect{X: next.X + inset, Y: next.Y + inset, W: tok.Icon, H: tok.Icon}
			if rectsIntersect(nextGlyph, x) || x.X+x.W > nextGlyph.X {
				t.Errorf("计数文字 %v 伸到了下一枚键的图形 %v", x, nextGlyph)
			}
		})
	}
}
