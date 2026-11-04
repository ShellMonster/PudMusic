// 满屏上的频谱：解码器挂点（`internal/service/audio/tap.go`）算出来的条高，
// 在这一层画成一根根离散的柱。
//
// ## 画法照上游 Biu（`src/components/audio-waveform/index.tsx` 的 `draw()`）
//
//	computedBarWidth = width / barCount
//	barGap  = computedBarWidth * 0.2         // 缝
//	barWidth = computedBarWidth - barGap     // 柱宽 = 一格的八成
//	x  = i * computedBarWidth                // 第 i 根从第 i 格的头开始
//	barHeight = Math.max((value / 255) * height, 2)
//	y  = height - barHeight                 // 从底部往上长
//	ctx.roundRect(x, y, barWidth, barHeight, 2)   // 圆角 2
//
// ⚠️ **这里画的是「柱」，不是「线」**（票 #374，2026-10-25 改）：原来这一层
// 画的是 `Painter.StrokePolyline`——一条连续的线，而上游画的是 N 根离散的柱。
// 用户 2026-10-24 说的「真正的音乐的频谱图是柱状图的」就是这件事。
//
// 那一版折线还配了 `chart.MinMax`（按**像素列**取最低/最高），那是**折线**的
// 算法：折线里一条窄峰要靠「取最高」才看得到，而柱状图要的是**每一格自己的
// 值**——所以现在直接用 `spectrum.Bars` 给的那一格，不降采样。
//
// ## 框架这一层用不到 `chart` 那套
//
// `chart.MinMax` / `chart.X` / `chart.Y` / `Painter.StrokePolyline` 都是给
// 折线用的（框架 `docs/ui/chart.md`「Live data」那一段），柱状图用不上。
// 圆角矩形框架**有**：`Painter.Fill`（`ui/paint.go`），半径按 DIP 给。
//
// ## 四个数在 `internal/service/spectrum` 那边，不在这一层
//
// 低频 60%、2 像素基线、`boost = 1 + i/barCount`、默认色 `#666`：
// 前三个是 `spectrum.Bars`，第四个是下面那个 `spectrumDefaultColor`。
// 这一层只负责把「高度比例」摆到屏幕上的哪个位置。

package ui

import (
	"github.com/egoist/mygo/ui"
)

// spectrumDefaultColor 是频谱条默认的颜色。
//
// Biu 的默认值是字符串 `"currentColor"`，画的时候再判断一次是不是它、是就落到
// `#666`（`audio-waveform/index.tsx:110` 的
// `ctx.fillStyle = barColor === "currentColor" ? "#666" : barColor`）。我们在读
// 偏好那一步就把那次判断提前了（见 `app.FullPlayerPrefs`），于是这一层拿到的
// 要么是 `#666` 这个串、要么是用户挑的那个串。
const spectrumDefaultColor = "#666"

// spectrumBarRadius 是每根柱子的圆角（DIP）。
//
// 原型 `.spectrum rect { rx: 1 }`；框架的 `Painter.Fill` 收的也是圆角半径（DIP），对得上。
//
// ⚠️ **底排那 2 像素的柱子比这个半径还矮**：框架渲染的时候会把圆角缩到能放下的
// 尺寸（`scene.FitRadii`），所以画出来是两头圆的小墩子，而不是一个畸形的形状。
const spectrumBarRadius = 1

// spectrumRestH 是柱子最矮的高度（原型暂停时 `target = 3`）。
const spectrumRestH = 3

// spectrumBarRatio 是柱宽占一格的比例。
//
// 原型 `viewBox 0 0 640 40` 里每格 10 宽、柱 7 宽（`x = i*10+1, width = 7`），也就是**柱宽七成、缝三成**。
const spectrumBarRatio = 0.7

// paintSpectrum 是频谱那一笔：N 根离散的柱，**从底部往上长**。
//
// ⚠️ **每一根用的是它自己那一格的值**（`spectrum.Bars` 给的高度比例），不是
// 「整条曲线的最高/最低」——后者是折线那套（`chart.MinMax`）的算法：折线里
// 窄峰要靠「取最高」才看得到，而柱状图要的是每一格自己的高度。
//
// ⚠️ **那 2 像素的底排已经算在 `bars` 里了**（`spectrum.Bars` 把
// `2 / heightDip` 换成了比例）。所以 `viewFullSpectrum` 必须把**这一块的真实
// 高度**传下去 —— 传错了那底排就不是 2 像素（框高 40 时传成 80，底排就只剩
// 1 像素，静的时候画出来的是一条贴着边的线而不是一条看得见的排）。
//
// **每帧不分配**：柱子的框是栈上的几个数（`ui.Rect` 是值类型），而 `bars`
// 本身是 `App.SpectrumBars` 复用着的那一块（见 `app/fullplayer.go`）。
func paintSpectrum(p *ui.Painter, r ui.Rect, bars []float32, color ui.Color) {
	n := len(bars)
	if n == 0 {
		return
	}
	cell := r.W / float32(n)
	barW := cell * spectrumBarRatio
	bottom := r.Y + r.H
	for i, v := range bars {
		// 一格的高度比例 × 这一格多高 = 这根柱子多高。原型里没有谱（暂停 / 还没解出来）时柱子
		// 落到 3 像素的底排，而不是不画（`target = 3`）。
		h := max(v*r.H, spectrumRestH)
		p.Fill(ui.Rect{
			X: r.X + float32(i)*cell,
			Y: bottom - h,
			W: barW,
			H: h,
		}, color, spectrumBarRadius)
	}
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
