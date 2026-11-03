package app_test

import (
	"path/filepath"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/audio"
)

// 自定义均衡器这一组：增益落盘、重启还在、存的预设有上限、越界的数被钳住。

// TestTheCustomEQGainsSurviveARestart 验「拖出来的那十段落盘，重启之后还
// 是那十段」——自定义开着的时候 `EQPreset` 那一格不该被清掉（它记的是
// 「自定义之前选的那套」，见 `Settings.EQCustomGains` 的注释）。
func TestTheCustomEQGainsSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	first := volumeApp(t, path)
	want := [10]float64{1, -2, 3, -4, 5, -6, 7, -8, 9, -10}
	if _, err := first.UpdateSettings(func(s *app.Settings) {
		s.EQPreset = "rock"
		s.EQCustomOn = true
		s.EQCustomGains = want
	}); err != nil {
		t.Fatal(err)
	}
	if err := first.CloseSettings(); err != nil {
		t.Fatal(err)
	}

	second := volumeApp(t, path)
	st := second.SettingsSnapshot()
	if !st.EQCustomOn || st.EQCustomGains != want {
		t.Errorf("重启之后自定义增益是 on=%v gains=%v", st.EQCustomOn, st.EQCustomGains)
	}
	if st.EQPreset != "rock" {
		t.Errorf("自定义开着的时候 EQPreset 被清了：%q", st.EQPreset)
	}
	if got := st.EQGains(); got != want {
		t.Errorf("生效的增益是 %v，该是 %v", got, want)
	}
}

// TestEQGainsResolvesCustomAndBuiltIn 验生效增益的那一条规矩：自定义开着
// 用 `EQCustomGains`，关着走 `EQPresetByID`——`MountSettings` /
// `UpdateSettings` 都从这一个出口取值。
func TestEQGainsResolvesCustomAndBuiltIn(t *testing.T) {
	s := app.DefaultSettings("")
	s.EQPreset = "bass"
	if got := s.EQGains(); got != audio.EQPresetByID("bass").Gains {
		t.Errorf("预设该是低音那套：%v", got)
	}
	custom := [10]float64{-1, -1, -1, -1, -1, -1, -1, -1, -1, -1}
	s.EQCustomOn = true
	s.EQCustomGains = custom
	if got := s.EQGains(); got != custom {
		t.Errorf("自定义开着该是自定义那份：%v", got)
	}
	// 全 0 = flat：这一份写进去，解码路就等于「关」（eqParams.flat）。
	s.EQCustomGains = [10]float64{}
	if got := s.EQGains(); !audio.EQFlat(got) {
		t.Errorf("全 0 该是 flat：%v", got)
	}
}

// TestTheCustomEQPresetsAreClampedAndCapped 验 normalize 兜的两条底：每段
// 增益钳进 ±12，存的预设裁到 8 套。
func TestTheCustomEQPresetsAreClampedAndCapped(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	a := volumeApp(t, path)
	if _, err := a.UpdateSettings(func(s *app.Settings) {
		s.EQCustomOn = true
		s.EQCustomGains = [10]float64{99, -99, 99, -99, 99, -99, 99, -99, 99, -99}
		for i := 0; i < 12; i++ {
			s.EQCustomPresets = append(s.EQCustomPresets, audio.EQPreset{
				Name:  "第" + string(rune('一'+i)) + "套",
				Gains: [10]float64{99},
			})
		}
	}); err != nil {
		t.Fatal(err)
	}
	st := a.SettingsSnapshot()
	for i, g := range st.EQCustomGains {
		want := 12.0
		if i%2 == 1 {
			want = -12
		}
		if g != want {
			t.Errorf("第 %d 段没钳进 ±12：%v", i, g)
		}
	}
	if len(st.EQCustomPresets) != audio.EQMaxCustomPresets {
		t.Errorf("存的预设该裁到 %d 套：%d", audio.EQMaxCustomPresets, len(st.EQCustomPresets))
	}
	for _, p := range st.EQCustomPresets {
		if p.Gains[0] != 12 {
			t.Errorf("存的预设 %q 增益没钳：%v", p.Name, p.Gains[0])
		}
	}
}
