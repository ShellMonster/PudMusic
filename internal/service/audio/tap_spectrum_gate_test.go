package audio

import "testing"

func TestDisabledSpectrumPassesAudioWithoutBuildingHistory(t *testing.T) {
	d := newFakeDec(48000, 2, 48000)
	tp := newTap(d)
	tp.SetSpectrumEnabled(false)
	feed(t, tp, 2048)
	if got := tp.At(0); got != nil {
		t.Fatal("disabled spectrum retained history")
	}
	if d.pos != 2048 {
		t.Fatalf("disabled spectrum changed audio progress: %d", d.pos)
	}
	tp.SetSpectrumEnabled(true)
	feed(t, tp, 2048)
	if got := tp.At(0); got != nil {
		t.Fatal("re-enabled spectrum restarted history at the disabled position")
	}
	if got := tp.At(2048); got == nil {
		t.Fatal("re-enabled spectrum did not retain history at the current source frame")
	}
}
