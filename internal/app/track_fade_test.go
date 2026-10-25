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
