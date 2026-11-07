package pud

import (
	"image"
	"image/color"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/egoist/mygo/ui"
)

// disabledScene 在一块纯色底上摆一枚键，返回 Tester。build 里自己决定怎么禁用。
//
// 量不透明度用纯黑底（好算）；量悬停用中灰底——默认浅色主题的 hover 是黑 4.5%，压在纯黑上
// 看不出来，那样「有没有悬停底」永远一样，测试就是空的。
func disabledScene(t *testing.T, bg ui.Color, build func(c *ui.Context)) *ui.Tester {
	t.Helper()
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(400, 200).Background(bg).Padding(40).Children(func() { build(c) })
	}, 400, 200)
	tt.Frame()
	return tt
}

// centerPixel 取一个矩形正中那一点的颜色。
func centerPixel(img *image.RGBA, r ui.Rect) color.RGBA {
	return img.RGBAAt(int(r.X+r.W/2), int(r.Y+r.H/2))
}

// fillPixel 取按钮底色：`Find` 按字找到的是**字**的框，往左退 10（按钮左右内边距 20 / 12）
// 就落在按钮自己的底上，不碰字形。
func fillPixel(img *image.RGBA, r ui.Rect) color.RGBA {
	return img.RGBAAt(int(r.X-10), int(r.Y+r.H/2))
}

// settle 推钟把悬停底的颜色过渡（`motion.Fade`，160ms）走完：框架的钟在测试里站着不动，
// 不推的话画出来的永远是过渡起点，有没有悬停底都一样，这条测试就形同虚设。
func settle(tt *ui.Tester) {
	tt.Frame()
	for i := 0; i < 8 && tt.FramePending(); i++ {
		tt.Advance(motion.Med)
		tt.Frame()
	}
}

var gray = ui.RGB(128, 128, 128)

func near(a, b uint8, tol int) bool {
	d := int(a) - int(b)
	return d >= -tol && d <= tol
}

// TestADisabledButtonDoesNotLightUpOnHover 锁住「禁用的键指针落上去不铺悬停底」（原型
// `:hover:not(:disabled)`）：禁用在建键时传（`ButtonDisabled` / `RoundButtonDisabled`）。
//
// 曾经出错的行为：悬停底是建键那一刻铺的，而调用方是建完才链 `.Disabled(true)`
// （collectionhead / collectionbatch 那几处），那一刻还没禁用——于是禁用的幽灵键悬停时照样亮出
// hover 底，看着像点得动。
func TestADisabledButtonDoesNotLightUpOnHover(t *testing.T) {
	for _, kind := range []struct {
		name string
		k    Kind
	}{{"幽灵", Ghost}, {"安静", Quiet}, {"主色", Primary}} {
		t.Run(kind.name, func(t *testing.T) {
			tt := disabledScene(t, gray, func(c *ui.Context) {
				ButtonDisabled(c, "off", kind.k, Std, "禁用的键", "", true)
			})
			r, ok := tt.Find("禁用的键")
			if !ok {
				t.Fatalf("没找到键，文字有 %q", tt.Texts())
			}
			idle := fillPixel(tt.Image(), r)
			tt.Move(r.X-10, r.Y+r.H/2)
			settle(tt)
			hot := fillPixel(tt.Image(), r)
			if idle != hot {
				t.Errorf("禁用的%s键悬停后底色从 %v 变成了 %v——禁用的键不该有悬停反馈", kind.name, idle, hot)
			}
		})
	}
	t.Run("圆键", func(t *testing.T) {
		tt := disabledScene(t, gray, func(c *ui.Context) {
			RoundButtonDisabled(c, "off-round", Ghost, Std, "more", "更多", true)
		})
		r, ok := tt.Find("更多")
		if !ok {
			t.Fatal("没找到圆键")
		}
		edge := func() color.RGBA { return tt.Image().RGBAAt(int(r.X+6), int(r.Y+r.H/2)) }
		idle := edge()
		tt.Move(r.X+r.W/2, r.Y+r.H/2)
		settle(tt)
		if hot := edge(); hot != idle {
			t.Errorf("禁用的圆键悬停后底色从 %v 变成了 %v", idle, hot)
		}
	})
}

// TestADisabledIconButtonDoesNotLightUpOnHover 同上，图标键那一份（`IconOpts.Disabled`，
// 如顶栏后退 / 前进、满屏上一首 / 下一首、队列「上移」）。
func TestADisabledIconButtonDoesNotLightUpOnHover(t *testing.T) {
	tt := disabledScene(t, gray, func(c *ui.Context) {
		IconBtn(c, "off-icon", "left", "后退", IconOpts{Disabled: true})
	})
	r, ok := tt.Find("后退")
	if !ok {
		t.Fatal("没找到图标键")
	}
	corner := func() color.RGBA { return tt.Image().RGBAAt(int(r.X+4), int(r.Y+r.H/2)) }
	idle := corner()
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	settle(tt)
	if hot := corner(); hot != idle {
		t.Errorf("禁用的图标键悬停后底色从 %v 变成了 %v", idle, hot)
	}
}

// TestHoverStillLightsAnEnabledKey 是上面两条的对照：同一个场景里可用的键悬停**会**变底——
// 不然「禁用的不变」可能只是因为悬停根本没推到（钟没走 / 坐标落空），那两条就是空的。
func TestHoverStillLightsAnEnabledKey(t *testing.T) {
	tt := disabledScene(t, gray, func(c *ui.Context) {
		ButtonDisabled(c, "on", Ghost, Std, "可用的键", "", false)
	})
	r, ok := tt.Find("可用的键")
	if !ok {
		t.Fatal("没找到键")
	}
	idle := fillPixel(tt.Image(), r)
	tt.Move(r.X-10, r.Y+r.H/2)
	settle(tt)
	if hot := fillPixel(tt.Image(), r); hot == idle {
		t.Errorf("可用的幽灵键悬停后底色没变（%v）：场景推不出悬停，禁用那两条测不出东西", idle)
	}
}

// TestDisabledOpacityMatchesThePrototype 锁住禁用态**画出来**的不透明度：图标键 35%、按钮 45%
// （原型 index.html:228 / 289）。
//
// 曾经出错的行为：IconBtn 自己给 Opacity(0.35)，框架对禁用元素又乘 0.5，实际只剩 0.175，
// 顶栏后退 / 前进禁用时几乎看不见。
func TestDisabledOpacityMatchesThePrototype(t *testing.T) {
	t.Run("图标键", func(t *testing.T) {
		white := ui.RGB(255, 255, 255)
		tt := disabledScene(t, ui.RGB(0, 0, 0), func(c *ui.Context) {
			IconBtn(c, "op-icon", "heart-on", "停", IconOpts{Size: 34, Color: white, Disabled: true})
		})
		r, ok := tt.Find("停")
		if !ok {
			t.Fatal("没找到图标键")
		}
		// 填满的心：取图形正中那一点，白 × 不透明度压在黑底上。
		got := centerPixel(tt.Image(), r)
		op := float32(iconBtnDisabledOpacity)
		want := uint8(255 * op)
		if !near(got.R, want, 6) {
			t.Errorf("禁用图标键实际亮度 %d，want ≈ %d（35%% 白压黑底）", got.R, want)
		}
	})
	t.Run("按钮", func(t *testing.T) {
		var accent ui.Color
		tt := disabledScene(t, ui.RGB(0, 0, 0), func(c *ui.Context) {
			accent = pal(c).Accent
			ButtonDisabled(c, "op-btn", Primary, Std, "禁用主键", "", true)
		})
		r, ok := tt.Find("禁用主键")
		if !ok {
			t.Fatal("没找到按钮")
		}
		got := fillPixel(tt.Image(), r)
		want := ui.RGB(0, 0, 0).Mix(accent, btnDisabledOpacity)
		if !near(got.R, want.R, 6) || !near(got.G, want.G, 6) || !near(got.B, want.B, 6) {
			t.Errorf("禁用主色键底色 %v，want ≈ %v（主色 45%% 压黑底）", got, want)
		}
	})
}
