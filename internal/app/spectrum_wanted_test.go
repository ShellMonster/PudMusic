package app

import (
	"path/filepath"
	"testing"
	"time"
)

func specFLAC(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac")
	return p
}

// TestSpectrumWantedGatesFFT 钉「没人在画频谱的时候 FFT 不跑」。
//
// FFT 的代价在解码 goroutine 上（每 ~10ms 一整窗），满屏没开、偏好关着、或者
// 窗口看不见的时候那一摊是纯烧电。启停由界面报上来的 `SetSpectrumWanted` 推，
// 这一条把「报没人看 → 立刻停，报有人看 → 偏好也开着才跑」钉死。
func TestSpectrumWantedGatesFFT(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)
	if err := a.Open(specFLAC(t)); err != nil {
		t.Fatalf("打不开测试素材：%v", err)
	}

	player := func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.player == nil {
			t.Fatal("播放器没装上")
		}
		return a.player.SpectrumEnabled()
	}

	// 新播放器起来时 spectrumWanted=false（没人报过「我在画」）：FFT 必须停着。
	waitForT(t, 2*time.Second, "装上的那一刻 FFT 还开着", func() bool { return !player() })

	// 界面说「我在画」且偏好默认开着：FFT 跑起来。
	a.SetSpectrumWanted(true)
	if !player() {
		t.Fatal("界面在画、偏好开着，FFT 却停着")
	}

	// 满屏关掉了：FFT 停。
	a.SetSpectrumWanted(false)
	if player() {
		t.Fatal("没人画了 FFT 还在跑")
	}
}

// TestSpectrumWantedRespectsPref 钉「偏好关着的时候，即使有人在画也不跑」。
func TestSpectrumWantedRespectsPref(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	prefs := DefaultFullPlayerPrefs()
	prefs.ShowSpectrum = false
	a.SetFullPlayerPrefs(prefs)

	if err := a.Open(specFLAC(t)); err != nil {
		t.Fatalf("打不开测试素材：%v", err)
	}
	a.SetSpectrumWanted(true)

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.player == nil {
		t.Fatal("播放器没装上")
	}
	if a.player.SpectrumEnabled() {
		t.Fatal("偏好关着、有人在画，FFT 却跑起来了")
	}
}

func waitForT(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s", what)
}
