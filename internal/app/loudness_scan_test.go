package app_test

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/audio"
)

// longWav 是 0.244 幅度的 440 Hz 单声道正弦：均方功率 0.0298，K 加权在 440 Hz 上
// 几乎是 0 dB，所以积分响度约 -0.691 + 10·log10(0.0298) = -15.95 LUFS。
func TestMeasureFileReadsARealWav(t *testing.T) {
	l, err := audio.MeasureFile(longWav(t, "响度.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(l.LUFS-(-15.95)) > 0.5 {
		t.Errorf("量到 %.2f LUFS，want ≈-15.95", l.LUFS)
	}
	if math.Abs(l.Peak-8000.0/32768) > 0.01 {
		t.Errorf("峰值 %.3f，want ≈%.3f", l.Peak, 8000.0/32768)
	}
}

func TestSwitchingNormalizeOnWhileALocalTrackPlaysKeepsItPlaying(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	a := volumeApp(t, path)
	if err := a.Open(longWav(t, "边放边开.wav")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateSettings(func(s *app.Settings) { s.NormalizeVolume = true }); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if st := a.Snapshot(); !st.Playing || st.Err != "" {
		t.Errorf("开了音量均衡之后：playing=%v err=%q", st.Playing, st.Err)
	}
}
