package ui

import "testing"

// TestTransportButtonsHaveNoHoverTip 钉住「播放 / 暂停 / 上一首 / 下一首悬停不弹提示」。
//
// 这几枚天天用、图标本身表意清楚；Apple HIG：图标够清楚就别用提示教用户。
// 它们的无障碍名字照旧在（读屏用），所以判据是「悬停之后画面上同名的文字
// 没有变多」，而不是「名字不在」。
func TestTransportButtonsHaveNoHoverTip(t *testing.T) {
	h := newHarness(t)
	h.open(t)

	count := func(s string) int {
		n := 0
		for _, x := range h.tt.Texts() {
			if x == s {
				n++
			}
		}
		return n
	}
	for _, name := range []string{"暂停", "上一首", "下一首"} {
		before := count(name)
		hoverOn(t, h.tt, name)
		for i := 0; i < 3; i++ {
			h.tt.Frame()
		}
		if after := count(name); after != before {
			t.Errorf("悬停在「%s」上之后，画面上同名文字从 %d 变成 %d——它弹了提示", name, before, after)
		}
	}
}
