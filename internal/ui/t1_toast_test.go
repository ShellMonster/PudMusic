package ui

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
)

// TestTheToastIsAnInvertedPillAboveThePlayBar 钉原型 `.toast` / `.toasts` 的版式：
// 水平居中、离播放条上沿 16、反色（正文色底）、内边距 10/16、前面一枚 16 的图标缝 8。
func TestTheToastIsAnInvertedPillAboveThePlayBar(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, systemIsDark(dark), at(1200, 800))
			shortToasts(t, time.Minute)
			const text = "已加入下一首播放"
			b.s.addToast(text, notice.Success)
			b.tt.Frame()
			b.tt.Advance(400 * time.Millisecond)
			b.tt.Frame()

			r, ok := b.tt.Find(text)
			if !ok {
				t.Fatalf("提示没画出来，文字有 %q", b.tt.Texts())
			}
			// 卡片 = 文字往左 16 + 8 + 16（内边距 + 缝 + 图标），往右 16。
			left, right := r.X-(16+8+16), r.X+r.W+16
			if mid := (left + right) / 2; mid < 598 || mid > 602 {
				t.Errorf("卡片中心在 x=%.1f，want 600（水平居中）", mid)
			}
			if bottom := 800 - (r.Y + r.H + 10); bottom < float32(tok.BarH+16)-1.5 || bottom > float32(tok.BarH+16)+1.5 {
				t.Errorf("卡片离窗口底边 %.1f，want %.0f（播放条 84 + 16）", bottom, tok.BarH+16)
			}
			p := tok.Of(b.s.theme.base)
			got := b.tt.Image().RGBAAt(int(left)+2, int(r.Y+r.H/2))
			if d := maxDiff(got, rgb(p.Text)); d > 6 {
				t.Errorf("卡片底色是 %v，want 正文色 %v（原型反色胶囊）", got, p.Text)
			}
		})
	}
}
