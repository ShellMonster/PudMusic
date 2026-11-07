package ui

// 刘海展开卡自己的进度条与频谱画法。
//
// 刘海固定黑底、不随主题（DESIGN §0′.T5），而共用的 `knobHoverLook` 轨道取
// `rangeRail(t)`（主题的 line-strong）、填充取 `t.Text`——浅色主题下填充是
// #141518，压在黑底上看不见。这里单独一根：轨道 #3a3a3e、填充 #ecebe8
// （原型 update-notch.html:99-100 `.progress-track` / `.progress-track i`），
// 悬停 / 按下填充换主色、旋钮才出（和播放条那根同一个分工，票 #218）。
// 值、拖动、点击跳转、键盘、无障碍仍是框架 `SliderBase` 的。

import (
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

var (
	// notchRail 是刘海进度条没走的那一段（update-notch.html:99）。
	notchRail = ui.Hex("#3A3A3E")
	// notchFill 是走过的那一段平时的颜色（update-notch.html:100）。
	notchFill = ui.Hex("#ECEBE8")
)

const (
	notchTrackW    = 4  // 轨道粗（DESIGN §0′.T5「轨道 4」），悬停 / 按下长到 notchTrackHotW
	notchTrackHotW = 6  // 悬停 / 按下的轨道粗（同播放条那根 4 → 6）
	notchKnobD     = 16 // 旋钮直径（同播放条那根）

	notchSpecBarW   = 4 // 频谱柱宽（update-notch.html:96 `.playing-eq i { width: 4px }`）
	notchSpecGap    = 3 // 柱缝（:95 `.playing-eq { gap: 3px }`）
	notchSpecRadius = 3 // 柱圆角（:96 `border-radius: 3px`）
	// notchSpecW 是频谱那一格的宽：五根柱 + 四道缝。
	notchSpecW = notchSpecBars*notchSpecBarW + (notchSpecBars-1)*notchSpecGap
)

// notchSeekSlider 是刘海进度行那根滑块。live 的来历和约定同 `selfSliderLive`。
//
// 颜色不读主题的轨道 / 正文色（理由见文件头）；主色取全屏那份暗色令牌
// （`tok.OfFull`），浅色主题下也是深色档的主色。
func notchSeekSlider(c *ui.Context, value *float64, live sliderLive) ui.Element {
	t := c.Theme()
	accent := tok.OfFull(t).Accent
	sl := ui.SliderBase(c, value, 0, 1).FocusRing(false).PaddingX(notchKnobD / 2)

	frac := min(max(float32(*value), 0), 1)
	heldTarget := float32(0)
	if sl.Pressed() {
		heldTarget = 1
	}
	held := sl.Animate("held", heldTarget, 150*time.Millisecond)
	focus := sl.FocusVisible()
	hotTarget := float32(0)
	if sl.Hovered() || held > 0 || focus {
		hotTarget = 1
	}
	heat := sl.Animate(fillHeatKey, hotTarget, 150*time.Millisecond)

	sl.Draw(func(p *ui.Painter, r ui.Rect) {
		if live.frac != nil && !sl.Pressed() && !sl.IsDisabled() {
			frac = min(max(live.frac(), 0), 1)
			if live.visible == nil || live.visible() {
				p.AnimationFrame()
			}
		}
		hot := heat
		fill := notchFill.Mix(accent, hot)
		if sl.IsDisabled() {
			hot, fill = 0, notchRail
		}
		trackW := notchTrackW + (notchTrackHotW-notchTrackW)*hot
		cy := r.Y + r.H/2
		track := ui.Rect{X: r.X, Y: cy - trackW/2, W: r.W, H: trackW}
		cx := r.X + notchKnobD/2 + (r.W-notchKnobD)*frac
		p.Fill(track, notchRail, trackW/2)
		p.Fill(ui.Rect{X: track.X, Y: track.Y, W: cx - track.X, H: trackW}, fill, trackW/2)
		if !knobWanted(sl, held) && !focus {
			return
		}
		g := 1 + 0.35*held
		knob := ui.Rect{X: cx - notchKnobD*g/2, Y: cy - notchKnobD*g/2, W: notchKnobD * g, H: notchKnobD * g}
		paintKnob(p, t, knob, knob.H/2, accent, held, focus)
	})
	return sl
}

// paintNotchSpectrum 画刘海主行那几根频谱柱：固定柱宽 / 缝（不像满屏那排按格宽
// 比例分），没有谱时落到 `spectrumRestH`（同满屏那排）。圆角照原型 3，
// 但不超过柱宽一半（浏览器对 4 宽的柱子也是这么收的）。
func paintNotchSpectrum(p *ui.Painter, r ui.Rect, bars []float32, color ui.Color) {
	rad := min(float32(notchSpecRadius), notchSpecBarW/2)
	bottom := r.Y + r.H
	for i, v := range bars {
		h := max(v*r.H, spectrumRestH)
		p.Fill(ui.Rect{
			X: r.X + float32(i)*(notchSpecBarW+notchSpecGap),
			Y: bottom - h,
			W: notchSpecBarW,
			H: h,
		}, color, rad)
	}
}
