package pud

import "testing"

func TestCountBadgeWidthHasFallbackWhenTextCannotBeMeasured(t *testing.T) {
	if got, want := countBadgeWidth(0), float32(countBadgeFallbackTextW)+8; got != want {
		t.Fatalf("countBadgeWidth(0) = %.1f, want %.1f", got, want)
	}
	if got, want := countBadgeWidth(-1), float32(countBadgeFallbackTextW)+8; got != want {
		t.Fatalf("countBadgeWidth(-1) = %.1f, want %.1f", got, want)
	}
	if got, want := countBadgeWidth(14), float32(22); got != want {
		t.Fatalf("countBadgeWidth(14) = %.1f, want %.1f", got, want)
	}
}
