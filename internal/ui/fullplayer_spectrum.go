// 满屏上的频谱：解码器挂点（`internal/service/audio/tap.go`）算出来的条高，
// 在这一层画成一条线。
//
// ## 画法是框架 v0.6.3 的现成件
//
//	`chart.MinMax`        按像素列取最低/最高，窄峰不会被采样丢掉，
//	                     而且**写进调用方传进去的切片**——这一层存一份复用，
//	                     稳态一帧不分配。
//	`Painter.StrokePolyline`  画每帧都变的线。GPU 上每段画成圆角矩形，
//	                     不把整条线光栅化成掩码（`StrokePath` 那样做就慢）。
//
// 照抄的是框架 `docs/ui/chart.md`「Live data」那一段的形状（bx/by/points 存在
// view 的状态里，每帧复用）。
//
// ⚠️ 框架自己写明了一个差异：段与段重叠处，不透明线的抗锯齿边会比 `StrokePath`
// 稍粗一点。频谱条是不透明的，所以看得到。PLAN 阶段 6 说「要拿真机看一眼这个
// 稍粗能不能接受」——这一版没看，见收尾报告。
//
// ## 四个数在 `internal/service/spectrum` 那边，不在这一层
//
// 低频 60%、2 像素基线、`boost = 1 + i/barCount`、默认色 `#666`：
// 前三个是 `spectrum.Bars`，第四个是下面那个 `spectrumDefaultColor`。
// 这一层只负责把「高度比例」摆到屏幕上的哪个位置。

package ui

import (
	"math"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"
)

// spectrumDefaultColor 是频谱条默认的颜色。
//
// Biu 的默认值是字符串 `"currentColor"`，画的时候再判断一次是不是它、是就落到
// `#666`（`audio-waveform/index.tsx:110` 的
// `ctx.fillStyle = barColor === "currentColor" ? "#666" : barColor`）。我们在读
// 偏好那一步就把那次判断提前了（见 `app.FullPlayerPrefs`），于是这一层拿到的
// 要么是 `#666` 这个串、要么是用户挑的那个串。
const spectrumDefaultColor = "#666"

// spectrumBuffers 是频谱那一块复用的中间量。
//
// ⚠️ **`xs`/`ys` 和 `bx`/`by` 不能共用内存**：`chart.MinMax` 往 dst 里写的时候
// 还读 src，两块共用一块的话画出来的是自己改自己。
type spectrumBuffers struct {
	xs, ys []float32 // 采样点（比例）
	bx, by []float32 // MinMax 之后（比例）
	points [][2]float32
}

// fullSpectrumWidth 是频谱那一块多宽。
//
// Biu（`index.tsx:189`）：`Math.min(640, Math.max(400, Math.round(windowWidth * 0.5)))`。
func fullSpectrumWidth(c *ui.Context) float32 {
	winW, _ := c.Size()
	w := float32(math.Round(float64(winW * 0.5)))
	return min(max(w, fullSpectrumMinW), fullSpectrumMaxW)
}

// fullBarCount 是画几根条。
//
// Biu（`index.tsx:190`）：`Math.max(48, Math.min(128, Math.round(width / 7.5)))`。
func fullBarCount(width float32) int {
	n := int(math.Round(float64(width / fullSpectrumBarPx)))
	return min(max(n, fullSpectrumBarsLo), fullSpectrumBarsHi)
}

// viewFullSpectrum 画频谱。**没开就一个字都不建**。
//
// 「默认关着」是 Biu 的 `defaultSettings.showSpectrum: false`，用户得在偏好面板里
// 自己开（`viewFullSettings`）。关着的时候这里连那个区域都不占——不是在画一片黑。
func (s *Shell) viewFullSpectrum(c *ui.Context, st app.State, hidden bool) {
	if !s.app.FullPlayerPrefs().ShowSpectrum {
		return
	}
	winW, _ := c.Size()
	w := fullSpectrumWidth(c)
	h := float32(fullSpectrumHeight)
	barCount := fullBarCount(w)
	bars := s.app.SpectrumBars(barCount, h)
	color := spectrumColorOf(s.app.FullPlayerPrefs().SpectrumColor)

	// 频谱离底边多远跟着控制条走（Biu 的 `index.tsx:370`）：
	// 界面露着的时候压在控制条上方 12，藏起来的时候退到离底 24。
	bottom := float32(fullSpectrumEdge)
	if !hidden {
		bottom = fullControlsHeight + fullSpectrumGapY
	}

	area := ui.Box(c).Key("full-spectrum").
		Absolute().Left((winW - w) / 2).Right((winW - w) / 2).
		Bottom(bottom).Height(h).
		Role(ui.RoleImage).Label("频谱")

	area.Draw(func(p *ui.Painter, r ui.Rect) {
		s.paintSpectrum(p, r, bars, color)
		// 只在真的在放、而且**用户看得到这一扇窗**的时候要下一帧：暂停时条子
		// 不动，一直要帧就是白烧 CPU（而全屏开着的时候这一层每个 pointer 移动
		// 都在画）；窗口收在后台时同理（票 #156）。
		if st.Playing && s.presence.seen() {
			p.AnimationFrame()
		}
	})
}

// paintSpectrum 是频谱那一笔。
//
// 三步都照框架 `docs/ui/chart.md`「Live data」那一段：
// `MinMax` 降采样 → 比例换算成框里的点 → `StrokePolyline`。
//
// ⚠️ **那 2 像素的底排已经算在 `bars` 里了**（`spectrum.Bars` 把
// `2 / heightDip` 换成了比例）。所以 `viewFullSpectrum` 必须把**这一块的真实
// 高度**传下去 —— 传错了那底排就不是 2 像素（框高 40 时传成 80，底排就只剩
// 1 像素，静的时候画出来的是一条贴着边的线而不是一条看得见的排）。
func (s *Shell) paintSpectrum(p *ui.Painter, r ui.Rect, bars []float32, color ui.Color) {
	buf := &s.full.spec
	n := len(bars)
	if n < 2 {
		return
	}
	buf.xs, buf.ys = growFloats(buf.xs, n), growFloats(buf.ys, n)
	// xs 必须**不减**（MinMax 的要求），所以均匀铺 0~1。
	last := float32(n - 1)
	for i, v := range bars {
		buf.xs[i] = float32(i) / last
		buf.ys[i] = v
	}

	buf.bx, buf.by = chart.MinMax(buf.bx, buf.by, buf.xs, buf.ys,
		int(math.Ceil(float64(r.W*p.Scale()))))

	buf.points = buf.points[:0]
	for i, x := range buf.bx {
		buf.points = append(buf.points, [2]float32{chart.X(r, x), chart.Y(r, buf.by[i])})
	}
	// 线宽 = 一格的八成（Biu 的 `barGap = cell * 0.2; barWidth = cell - barGap`）。
	cell := r.W / float32(n)
	p.StrokePolyline(buf.points, cell*0.8, color)
}

// spectrumColorOf 是偏好里那个颜色串该画成什么颜色。
//
// 空串（= 走默认）和认不出来的串都落到 `#666`。认不出来还照画的话，界面上是
// 一条纯黑的频谱摆在深色背景上——看着像「没画出来」，而用户是在用着一个自己
// 挑过的颜色。
func spectrumColorOf(hex string) ui.Color {
	if hex != "" {
		if col, ok := parseHexColor(hex); ok {
			return col
		}
	}
	// 兜底走**常量**，不是再写一遍 #666：两处各写一份，改了一处另一处不跟着改，
	// 而这一票的全部意义就是「那四个数照抄」。
	col, _ := parseHexColor(spectrumDefaultColor)
	return col
}

// growFloats 把 s 弄到至少 n 长。
func growFloats(s []float32, n int) []float32 {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]float32, n)
}
