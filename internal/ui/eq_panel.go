// 均衡器的自定义面板：十个竖滑杆 + 「另存为预设」。
//
// # 两个入口，一份面板
//
// 设置页「均衡器」那一行有「自定义」键（`pickEQ`，settings_page.go），满屏
// 播放器的偏好面板里有一行「均衡器」（`viewFullSettings`，
// fullplayer_settings.go）。两处弹的是**同一个面板**
// （`eqPanelContent`）——面板管的是 `app.Settings` 里那一组 EQ 字段，跟它
// 挂在哪个锚点上无关，所以不复制实现。
//
// # 存储模型（详注在 `Settings.EQCustomGains` 那一格上）
//
//   - 预设下拉照旧写 `EQPreset`（内置 ID）；自定义预设选上时
//     `EQCustomOn=true` + `EQCustomGains=那套的增益` + `EQCustomName=名字`；
//   - 拖动任何一段 → `EQCustomOn=true` + 写 `EQCustomGains`。若改完之后和
//     `EQCustomName` 指着的那套不一样，名字清掉（它就不再是那套了）。
//
// 生效的增益只有 `Settings.EQGains()` 一个出口，`MountSettings` /
// `UpdateSettings` 都从它取值——这里只管把设置写对，播放链路（`a.eqGains`
// → `Player.SetEQ`）自己跟上。
//
// # 键的分域
//
// 滑杆的身份带前缀（`scope` 参数）：设置页那份是 `eq:band:N`，满屏那份是
// `full:eq:band:N`。两个面板不会同帧共存（满屏开着时设置页不建），但浮层
// 根是两处共用的，**状态**会串——同 `lyricKnobIDs` 那一段的理由。
package ui

import (
	"strconv"
	"strings"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/service/audio"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	// eqSliderH 是每一段竖滑杆的高。比歌词字号那条（112）略高：十段并排，
	// 每一段都窄，高一点才好拖。
	eqSliderH = 128.0
	// eqPanelPad / eqPanelGap 是面板内边距和行距。
	eqPanelPad = space.Px16
	eqPanelGap = space.Px12
)

// eqBandLabels 是滑杆底下那十个频率标签，和 audio.EQBands 一一对应。
var eqBandLabels = [10]string{"31", "62", "125", "250", "500", "1k", "2k", "4k", "8k", "16k"}

// eqCustomValue 是下拉里「自定义（未存名）」那一项的值。存的自定义预设在
// 下拉里是 "custom:<名字>"（eqCustomPresetValue），永远不会和内置 ID 撞上。
const eqCustomValue = "custom"

// eqCustomPresetValue 是一套自定义预存在下拉里的值。
func eqCustomPresetValue(name string) string { return "custom:" + name }

// eqCurrentSelectValue 是这份设置在下拉里该亮的那一项。
func eqCurrentSelectValue(st app.Settings) string {
	if st.EQCustomOn {
		for _, p := range st.EQCustomPresets {
			if p.Name == st.EQCustomName {
				return eqCustomPresetValue(st.EQCustomName)
			}
		}
		return eqCustomValue
	}
	return audio.EQPresetByID(st.EQPreset).ID
}

// eqGainLabel 是一段增益的读数（「+3.5」「0」「-2」）。
func eqGainLabel(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if v > 0 {
		return "+" + s
	}
	return s
}

// eqSlider 是面板上的一段竖滑杆，画法和歌词那两条同一个（lyrics_knobs.go
// 的 `lyricSlider`）：轨道 4 宽、⌀20 的钮，往上加。
func eqSlider(c *ui.Context, key string, v *float64, ro bool) ui.Element {
	return lyricSlider(c, key, v, audio.EQGainMin, audio.EQGainMax, audio.EQGainStep,
		lyricSliderW, eqSliderH).Disabled(ro)
}

// eqGainPreview 是预设名后面那一条十段增益的预览：中线上下各一根小柱，
// 高度按 |gain| / 12 缩。全是 0 的时候只剩一条中线（= 关 / 平直）。
func eqGainPreview(c *ui.Context, gains [10]float64) ui.Element {
	t := c.Theme()
	return ui.Box(c).Grow(1).Height(16).Draw(func(p *ui.Painter, r ui.Rect) {
		mid := r.Y + r.H/2
		p.Fill(ui.Rect{X: r.X, Y: mid - 0.5, W: r.W, H: 1}, t.Border, 0)
		slot := r.W / float32(len(gains))
		for i, g := range gains {
			h := float32(g/audio.EQGainMax) * (r.H/2 - 1)
			if h < 0 {
				h = -h
			}
			if h < 1 {
				h = 1
			}
			bar := ui.Rect{
				X: r.X + float32(i)*slot + (slot-6)/2,
				W: 6,
			}
			if g >= 0 {
				bar.Y, bar.H = mid-h, h
			} else {
				bar.Y, bar.H = mid, h
			}
			p.Fill(bar, t.Accent, 1)
		}
	})
}

// eqPanelContent 是自定义均衡器面板的**内容**：十段竖滑杆，底下一行
// 「重置 / 预设名输入 / 存为预设 / 删除」。
//
// scope 是滑杆键的前缀（"" 或 "full:"，见文件头「键的分域」）。
func (s *Shell) eqPanelContent(c *ui.Context, st app.Settings, scope string, ro bool) {
	t := c.Theme()
	gains := st.EQGains()

	ui.Column(c).Padding(eqPanelPad).Gap(eqPanelGap).Children(func() {
		// 十段滑杆：上面是滑杆、中间是频率、下面是当前读数。
		ui.Row(c).Gap(space.Px8).Children(func() {
			for i := range gains {
				band := i
				ui.Column(c).AlignItems(ui.Center).Gap(space.Px4).Children(func() {
					v := gains[band]
					sl := eqSlider(c, scope+"eq:band:"+strconv.Itoa(band), &v, ro).
						Label("均衡器 " + strconv.FormatFloat(audio.EQBands[band], 'f', -1, 64) + " Hz")
					if sl.Changed() {
						// ⚠️ **整份写回**：设置里存的是 [10]float64 一份，
						// 拖一段就是「改这一个数、其余照旧」，不是逐段一格。
						s.applyEQBand(band, v)
					}
					ui.Text(c, eqBandLabels[band]).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)
					ui.Text(c, eqGainLabel(v)).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)
				})
			}
		})

		// 底行：重置 + 存预设（名字框 + 保存键）+ 删除当前预设。
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(space.Px8).Children(func() {
			if flatButton(c, "重置").Disabled(ro).Clicked() {
				s.applySetting("均衡器", func(cur *app.Settings) {
					cur.EQCustomOn = true
					cur.EQCustomGains = [10]float64{}
					cur.EQCustomName = ""
				})
			}
			ui.Box(c).Grow(1)
			ui.TextInput(c, &s.settings.eqName).Width(120).
				Label("均衡器预设名称").Placeholder("预设名称").Disabled(ro)
			name := strings.TrimSpace(s.settings.eqName)
			full := len(st.EQCustomPresets) >= audio.EQMaxCustomPresets && !eqHasCustomPreset(st, name)
			if flatButton(c, "存为预设").Disabled(ro || name == "" || full).Clicked() {
				s.saveEQPreset(st)
			}
			if st.EQCustomName != "" && eqHasCustomPreset(st, st.EQCustomName) {
				if flatIconButton(c, iconDeleteBin5, "删除均衡器预设", 16).Disabled(ro).Clicked() {
					s.deleteEQPreset(st.EQCustomName)
				}
			}
		})
	})
}

// eqHasCustomPreset 报这套名字的自定义预设存了没有。
func eqHasCustomPreset(st app.Settings, name string) bool {
	for _, p := range st.EQCustomPresets {
		if p.Name == name {
			return true
		}
	}
	return false
}

// applyEQBand 写「拖了第 band 段到 v」这一笔。
//
// 拖动的起点是「当前生效的那一份」：自定义开着用 `EQCustomGains`，关着就
// 从预设的增益起步——用户看着预览拖，改的是他听到的那份。
func (s *Shell) applyEQBand(band int, v float64) {
	s.applySetting("均衡器", func(cur *app.Settings) {
		var g [10]float64
		if !cur.EQCustomOn {
			g = audio.EQPresetByID(cur.EQPreset).Gains
		} else {
			g = cur.EQCustomGains
		}
		g[band] = v
		cur.EQCustomOn = true
		cur.EQCustomGains = g
		// 改完之后若和存着的那套不一样，这套就不再是它了（名字清掉，
		// 下拉回落到「自定义」）。
		if cur.EQCustomName != "" {
			matched := false
			for _, p := range cur.EQCustomPresets {
				if p.Name == cur.EQCustomName && p.Gains == g {
					matched = true
				}
			}
			if !matched {
				cur.EQCustomName = ""
			}
		}
	})
}

// saveEQPreset 把当前生效的增益存成一套命名预设（上限
// audio.EQMaxCustomPresets，同名覆盖）。
func (s *Shell) saveEQPreset(st app.Settings) {
	name := strings.TrimSpace(s.settings.eqName)
	if name == "" {
		return
	}
	// 不许和内置预设重名：下拉里两个「低音」没法分清哪个是用户自己的。
	for _, p := range audio.EQPresets {
		if p.Name == name || p.ID == name {
			s.addToast("这个名字是内置预设的，换一个", notice.Danger)
			return
		}
	}
	gains := st.EQGains()
	s.applySetting("均衡器", func(cur *app.Settings) {
		entry := audio.EQPreset{ID: eqCustomPresetValue(name), Name: name, Gains: gains}
		replaced := false
		for i, p := range cur.EQCustomPresets {
			if p.Name == name {
				cur.EQCustomPresets[i] = entry
				replaced = true
			}
		}
		if !replaced {
			cur.EQCustomPresets = append(cur.EQCustomPresets, entry)
		}
		cur.EQCustomOn = true
		cur.EQCustomGains = gains
		cur.EQCustomName = name
	})
	s.settings.eqName = ""
}

// deleteEQPreset 删掉这套名字的自定义预设；删掉的正好是当前那套时，
// 当前增益保留成未命名自定义（用户听到的声音不变，只是它不再叫那个名字）。
func (s *Shell) deleteEQPreset(name string) {
	s.applySetting("均衡器", func(cur *app.Settings) {
		kept := cur.EQCustomPresets[:0]
		for _, p := range cur.EQCustomPresets {
			if p.Name != name {
				kept = append(kept, p)
			}
		}
		cur.EQCustomPresets = kept
		if cur.EQCustomName == name {
			cur.EQCustomName = ""
		}
	})
}
