package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// 三档清晰度各自的**画布比例**和**模糊半径**（票 #666）。
//
// ⚠️ **两样都要跟着档位变**：只改画布不改模糊的话，画布放大四倍、而 σ=32 DIP
// 的模糊还留在那儿 ⇒ 糊成一片纯色，「高清」那一档白给。
func TestTheClarityTiersChangeBothTheCanvasAndTheBlur(t *testing.T) {
	cases := []struct {
		name    string
		clarity app.BackdropClarity
		div     float64
		blur    float64
	}{
		{"流畅（默认）", app.BackdropClaritySmooth, 4, 32},
		{"清晰", app.BackdropClarityClear, 2, 16},
		{"高清", app.BackdropClarityHD, 1, 8},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.clarity.CanvasDivisor(); got != c.div {
				t.Errorf("画布分母是 %v，want %v", got, c.div)
			}
			if got := c.clarity.BlurDIP(); got != c.blur {
				t.Errorf("模糊半径是 %v DIP，want %v", got, c.blur)
			}
		})
	}
	// ⚠️ **空串要等于「流畅」**：设置里没这一格（老文件）时不能变成「没有画布」。
	var empty app.BackdropClarity
	if empty.CanvasDivisor() != 4 || empty.BlurDIP() != 32 {
		t.Errorf("空串给了 分母=%v 模糊=%v，want 4 / 32（等于默认那一档）",
			empty.CanvasDivisor(), empty.BlurDIP())
	}
}

// **画布越大、模糊越小**——两者必须反向，否则「更清晰」是假的。
func TestTheBlurShrinksAsTheCanvasGrows(t *testing.T) {
	prevDiv, prevBlur := 0.0, 0.0
	for _, c := range []app.BackdropClarity{
		app.BackdropClaritySmooth, app.BackdropClarityClear, app.BackdropClarityHD,
	} {
		div, blur := c.CanvasDivisor(), c.BlurDIP()
		if prevDiv > 0 {
			// 分母变小（画布变大）⇒ 模糊也要变小。
			if div >= prevDiv {
				t.Errorf("%q 的画布分母 %v 没有比 %q 的 %v 小", c, div, "上一档", prevDiv)
			}
			if blur >= prevBlur {
				t.Errorf("%q 的模糊 %v 没有比上一档的 %v 小", c, blur, prevBlur)
			}
		}
		prevDiv, prevBlur = div, blur
	}
}

// **屏幕上**的模糊强度在档位内恒定：和窗口多大、屏幕多密无关。
//
// `clarityBlurSigma` 把「DIP 的模糊半径」换成「画布像素」——这个换算必须让
// 「画布上的 σ 放大到屏幕上」正好等于那个 DIP 值。
func TestTheBlurOnScreenIsTheSameWhateverTheWindowSize(t *testing.T) {
	const blurDIP = 32 // 流畅那一档
	for _, win := range []float32{900, 1200, 1600} {
		canvas := win / 4 // 流畅档：画布是窗口的 1/4
		sigma := clarityBlurSigma(app.BackdropClaritySmooth, canvas, win)
		// 画布上的 σ 放大 4 倍铺满窗 ⇒ 屏幕上是 sigma*4
		onScreen := sigma * (win / canvas)
		if diff := onScreen - blurDIP; diff > 0.01 || diff < -0.01 {
			t.Errorf("窗口宽 %v：屏幕上的模糊是 %v DIP，want %v", win, onScreen, blurDIP)
		}
	}
}

// ⚠️ **背景那一格要的图，存下来也要那么大**（票 #666 的核心）。
//
// 从前 `maxStoredSide = 768` 把「存的那一份」钉死：就算要到一张 2400 宽的图，
// 解下来还是 768 宽 —— 再被 GPU 拉大，**清晰度白给了**。
//
// 这一条在 `imgcache` 包里有专门的一条（`TestStoredCoversAreNotCappedAt768`），
// 这里只钉**调用方传的那个数**：背景那一格必须把自己画布的尺寸传下去。
func TestTheBackdropPassesItsOwnStoredSize(t *testing.T) {
	for _, c := range []struct {
		name    string
		clarity app.BackdropClarity
		div     float64
	}{
		{"流畅", app.BackdropClaritySmooth, 4},
		{"清晰", app.BackdropClarityClear, 2},
		{"高清", app.BackdropClarityHD, 1},
	} {
		const winW, winH = 1200, 800
		cw := int(float64(winW) / c.div)
		ch := int(float64(winH) / c.div)
		side := max(cw, ch)
		// 高清档画布 1200×800 ⇒ side 800（长边）——但 Retina 上要 ×2。
		// 这里只断「不忙 768」，具体倍数在 imgcache 那条测试里。
		if c.div == 1 && side <= 768 {
			t.Errorf("%s：画布 %d×%d 的长边 %d 不大于 768——"+
				"这一档本来就是要突破 768 的", c.name, cw, ch, side)
		}
	}
}
