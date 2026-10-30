package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

func TestPageSeamCapturesSharedSnapshotsAndScopedActions(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	s := New(a)
	t.Cleanup(s.Close)

	item := app.Item{Source: app.SourceOnline, Type: "mv", Bvid: "BVseam", Title: "页面 seam"}
	a.Queue().Reset(item)
	prefs := a.FullPlayerPrefs()
	prefs.ShowSpectrum = true
	a.SetFullPlayerPrefs(prefs)

	seam := s.pageSeam()
	if got := seam.Input.Playback.Title; got != a.Snapshot().Title {
		t.Fatalf("page seam playback title = %q, app snapshot title = %q", got, a.Snapshot().Title)
	}
	if !seam.Input.HasCurrent || seam.Input.Current.Identity() != item.Identity() {
		t.Fatalf("page seam current item = %+v, want %q", seam.Input.Current, item.Identity())
	}
	if seam.Input.Account != a.AccountSnapshot() {
		t.Fatalf("page seam account snapshot = %+v, app snapshot = %+v", seam.Input.Account, a.AccountSnapshot())
	}
	if !seam.Input.FullPlayerPrefs.ShowSpectrum {
		t.Fatal("page seam did not capture full-player preferences")
	}
	if seam.Input.Images != s.images {
		t.Fatal("page seam did not retain the Shell-owned image cache")
	}
	if seam.Input.Now == nil {
		t.Fatal("page seam omitted time input")
	}

	var navigated string
	seam.Actions.Navigate = func(path string) { navigated = path }
	seam.Actions.Navigate("/search")
	if navigated != "/search" {
		t.Fatalf("page seam navigation action = %q", navigated)
	}
}

func TestPageSeamActionsPreserveAppOwnership(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	s := New(a)
	t.Cleanup(s.Close)

	seam := s.pageSeam()
	if seam.Actions.SetFullPlayerPrefs == nil || seam.Actions.TogglePlayback == nil || seam.Actions.Wake == nil {
		t.Fatal("page seam did not expose the scoped app actions")
	}

	prefs := seam.Input.FullPlayerPrefs
	prefs.ShowLyrics = !prefs.ShowLyrics
	seam.Actions.SetFullPlayerPrefs(prefs)
	if got := a.FullPlayerPrefs().ShowLyrics; got != prefs.ShowLyrics {
		t.Fatalf("set preferences action = %v, want %v", got, prefs.ShowLyrics)
	}
}
