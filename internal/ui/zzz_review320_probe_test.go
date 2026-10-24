package ui

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 评审用临时探针（跑完就删）。

func probeBar2(t *testing.T, st *app.State, width, height int, dark bool) (*mygoui.Tester, *Shell) {
	t.Helper()
	var b *testShell
	b = newTestShell(t, at(width, height), systemIsDark(dark), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			mygoui.Column(c).Fill().Children(func() {
				mygoui.Row(c).Grow(1)
				s.viewPlayBar(c, *st)
			})
		}
	}))
	tt := b.tt
	tt.Frame()
	return tt, b.s
}

// TestProbeReview320 评审测量：出错那一副的每帧分配 + 真实那一副里那一句有多宽。
func TestProbeReview320(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  string
	}{
		{"没有错（预算那一副）", ""},
		{"有错（长句，预算那一副 + 错）", "B 站没有给这一条单独的音频流（歌单、活动那一种多半是这样）。在 B 站里打开它听，或者换一首放"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := budgetState()
			st.Err = tc.err
			_, tt := newShell(t, reduceMotion(), withView(func(s *Shell) func(*mygoui.Context) {
				return func(c *mygoui.Context) {
					s.applyFrameTheme(c)
					s.viewPlayBar(c, st)
				}
			}))
			got := measureFrames(t, tt, 100)
			t.Logf("%s：每帧 %.0f 次分配 / %.0f 字节", tc.name, got.allocs, got.bytes)
		})
	}
	t.Run("真实那一副里那一句有多宽", func(t *testing.T) {
		st := budgetState()
		st.Err = "B 站没有给这一条单独的音频流（歌单、活动那一种多半是这样）。在 B 站里打开它听，或者换一首放"
		st.CanRetry = true
		tt, _ := probeBar2(t, &st, 1200, 320, false)
		r, ok := tt.Find("打不开：" + st.Err)
		if !ok {
			t.Fatalf("找不到那一句错：%q", tt.Texts())
		}
		t.Logf("有封面 + 「重试」键时，那一句错的布局盒：x=%.1f w=%.1f（也就是大约 %.0f 个 12 号字）",
			r.X, r.W, r.W/12)
	})
}
