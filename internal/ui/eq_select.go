package ui

import (
	"strings"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/audio"
)

// applyEQSelect 是均衡器预设「选中某一项」的那一笔。设置页的「均衡器」入口已经删掉（原型里 EQ
// 在满屏播放器里，归满屏播放器那一边接），写设置的逻辑留在这里给它用：
//
//   - "custom"           → 只把自定义那一格开起来（增益是它自己落盘的那份）；
//   - "custom:<名字>"    → 自定义开着，增益抄那一套存的，名字记上；
//   - 其余（内置 ID）    → 照旧写 EQPreset，同时把自定义那一格关掉。
func (s *Shell) applyEQSelect(st app.Settings, v string) {
	switch {
	case v == eqCustomValue:
		s.applySetting("均衡器", func(cur *app.Settings) {
			cur.EQCustomOn = true
			cur.EQCustomName = ""
		})
	case strings.HasPrefix(v, "custom:"):
		name := strings.TrimPrefix(v, "custom:")
		s.applySetting("均衡器", func(cur *app.Settings) {
			for _, p := range cur.EQCustomPresets {
				if p.Name == name {
					cur.EQCustomOn = true
					cur.EQCustomGains = p.Gains
					cur.EQCustomName = p.Name
					return
				}
			}
		})
	default:
		id := audio.EQPresetByID(v).ID
		s.applySetting("均衡器", func(cur *app.Settings) {
			cur.EQPreset = id
			cur.EQCustomOn = false
			cur.EQCustomName = ""
		})
	}
}
