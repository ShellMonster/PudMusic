package app_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

func TestTheTrackFadeIsOffByDefaultAndSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	first := volumeApp(t, path)
	if first.SettingsSnapshot().TrackFadeSeconds != 0 {
		t.Fatal("切歌淡入淡出默认应该是关（0）")
	}
	if _, err := first.UpdateSettings(func(s *app.Settings) { s.TrackFadeSeconds = 99 }); err != nil {
		t.Fatal(err)
	}
	if got := first.SettingsSnapshot().TrackFadeSeconds; got != app.MaxTrackFadeSeconds {
		t.Errorf("超过上限的值没被夹到 %d：%d", app.MaxTrackFadeSeconds, got)
	}
	if err := first.CloseSettings(); err != nil {
		t.Fatal(err)
	}
	if got := volumeApp(t, path).SettingsSnapshot().TrackFadeSeconds; got != app.MaxTrackFadeSeconds {
		t.Errorf("重启之后是 %d", got)
	}
}

func TestScrubbingNearTheEndWithAFadeOnKeepsPlayingWithoutErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	a := volumeApp(t, path)
	if _, err := a.UpdateSettings(func(s *app.Settings) { s.TrackFadeSeconds = 3 }); err != nil {
		t.Fatal(err)
	}
	if err := a.Open(longWav(t, "淡出.wav")); err != nil {
		t.Fatal(err)
	}
	a.ScrubEnd(0.93)
	time.Sleep(500 * time.Millisecond)
	if st := a.Snapshot(); st.Err != "" {
		t.Errorf("出错：%q", st.Err)
	}
	a.ScrubEnd(0.2)
	time.Sleep(300 * time.Millisecond)
	if st := a.Snapshot(); !st.Playing {
		t.Error("拖回中间之后没有继续放")
	}
}

func TestTheEQPresetSurvivesARestartAndUnknownOnesAreOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	first := volumeApp(t, path)
	if first.SettingsSnapshot().EQPreset != "" {
		t.Fatal("均衡器默认应该是关")
	}
	if _, err := first.UpdateSettings(func(s *app.Settings) { s.EQPreset = "rock" }); err != nil {
		t.Fatal(err)
	}
	if err := first.CloseSettings(); err != nil {
		t.Fatal(err)
	}
	second := volumeApp(t, path)
	if got := second.SettingsSnapshot().EQPreset; got != "rock" {
		t.Errorf("重启之后预设是 %q", got)
	}
	if _, err := second.UpdateSettings(func(s *app.Settings) { s.EQPreset = "没有这个" }); err != nil {
		t.Fatal(err)
	}
	if got := second.SettingsSnapshot().EQPreset; got != "" {
		t.Errorf("认不出的预设该落成空，得到 %q", got)
	}
}

func TestTheVideoQualityPreferenceSurvivesARestartAndRejectsUnknownTiers(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	first := volumeApp(t, path)
	if first.SettingsSnapshot().VideoQuality != 0 {
		t.Fatal("默认应该是 0（720P）")
	}
	if _, err := first.UpdateSettings(func(s *app.Settings) { s.VideoQuality = 80 }); err != nil {
		t.Fatal(err)
	}
	if _, err := first.UpdateSettings(func(s *app.Settings) { s.VideoQuality = 999 }); err != nil {
		t.Fatal(err)
	}
	if got := first.SettingsSnapshot().VideoQuality; got != 0 {
		t.Errorf("认不得的档没被清掉：%d", got)
	}
	_, _ = first.UpdateSettings(func(s *app.Settings) { s.VideoQuality = 80 })
	if err := first.CloseSettings(); err != nil {
		t.Fatal(err)
	}
	if got := volumeApp(t, path).SettingsSnapshot().VideoQuality; got != 80 {
		t.Errorf("重启之后是 %d，应该是 80", got)
	}
}
