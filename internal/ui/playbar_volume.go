// 播放条上的音量：一枚静音图标 + 一根 96×16 的横向滑块（原型 `.vol`）。
//
//   - 图标是能按的：按一下静音、再按一下取消静音。
//   - 滑块平时只画轨道，悬停或按住时才出旋钮（`knobHoverLook`）。
//
// 旧版本是“悬停图标浮出一根竖滑块 + 百分比”的浮层，原型里没有，已连同它的收放计时器删除。
//
// ## 「静音」是它自己的一档，不是「音量 0」
//
// 播放器那边只有增益（`audio.Player.SetVolume`），应用层多了一格静音
// （`App.ToggleMute`）：静音的时候音量那一格一动不动——取消静音放回去的
// 就是它，落盘的也是它。合成一档（静音 = 音量 0）会踩一个落盘的坑（票 #311）：
// 音量一旦开始落盘，「静音后退出再启动，音量变成 0」就成了必然。
//
// 画面上：图标跟着静音那一格走（静音时画「取消静音」那张）；而滑块在静音时
// 显示 0，取消静音后回到用户设的那一档。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/egoist/mygo/ui"
)

// viewVolumeButton 画静音键和音量滑块。
func (s *Shell) viewVolumeButton(c *ui.Context, st app.State) {
	muted := st.Muted || st.Volume <= 0
	name, label := "vol", "静音"
	switch {
	case muted:
		name, label = "mute", "取消静音"
	case st.Volume <= 0.5:
		name = "vol-low"
	}
	if s.withTip(c, pud.IconBtn(c, "pb-mute", name, label, pud.IconOpts{}), label).Clicked() {
		s.toggleMute()
	}
	vol := st.Volume
	if st.Muted {
		vol = 0
	}
	sl := selfSlider(c, &vol, 0, 1, knobHoverLook).Label("音量").Width(96).Height(16).Shrink(0)
	if sl.Changed() {
		s.setVolume(vol)
	}
}

// setVolume 设音量。
//
// ⚠️ **「静音之前是多少」不用这一层记**：那是应用层的事（`App.SetVolume` 记的是
// 用户设的那一档，静音那一档是它自己的一格）。这一层以前有个 `volBeforeMute`
// ——它跟着窗口走，进程一退就没了，而音量是要落盘的（票 #311）。
func (s *Shell) setVolume(v float64) {
	s.app.SetVolume(v)
}

// toggleMute 静音 / 取消静音。
//
// 静音 = 按下静音键（音量那一格不动）；取消静音 = 放回用户设的那一档。
func (s *Shell) toggleMute() {
	s.app.ToggleMute()
}
