package pud

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestHoveringATrackRowDoesNotShiftTheDuration 锁住行尾键「平时透明但仍占位」（原型
// `.hide-idle`）：指针移上行之前和之后，时长那一格在同一个位置；平时藏着的「⋯」
// 不可点。
//
// 曾经出错的行为：行尾键用 `if hot {…}` 现建，悬停时「⋯」从右边冒出来，把左边
// 那一格往左挤了一格。行上那颗心已经拿掉了，所以拿一直画着的时长当锚点。
func TestHoveringATrackRowDoesNotShiftTheDuration(t *testing.T) {
	const duration = "3:47"
	const more = "更多操作：曲目行"
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Padding(20).Children(func() {
			TrackRow(c, "track", Track{Title: "曲目行", Duration: duration, Removable: true}, 900)
		})
	}, 1000, 200)
	tt.Frame()

	before, ok := tt.Find(duration)
	if !ok {
		t.Fatal("没悬停时找不到时长")
	}
	if _, ok := tt.Find(more); ok {
		t.Fatal("没悬停时「⋯」不该是一枚可点的键")
	}

	// 指针压到行里（序号那一格，离行尾键远）。
	tt.Move(before.X-600, before.Y+before.H/2)
	tt.Frame()
	if _, ok := tt.Find(more); !ok {
		t.Fatal("悬停后「⋯」没有出现")
	}
	after, ok := tt.Find(duration)
	if !ok {
		t.Fatal("悬停后找不到时长")
	}
	if after.X != before.X || after.Y != before.Y {
		t.Fatalf("悬停让时长挪了位：之前 %+v，之后 %+v", before, after)
	}
}
