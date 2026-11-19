package mobileui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestMobileImportsSurviveASandboxMove checks that the restored queue opens the copied music at its new location.
func TestMobileImportsSurviveASandboxMove(t *testing.T) {
	application := app.New()
	t.Cleanup(application.Close)
	if err := application.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	application.TestOnlyNoAutoLyrics()
	relative := filepath.Join("Library", "Application Support", "PudMusic", localImportDirectory)
	sandbox := t.TempDir()
	base := filepath.Join(sandbox, "new", relative)
	root, err := copyMobileImport(base, filepath.Join("..", "service", "audio", "testdata", "tone.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	oldRoot := filepath.Join(sandbox, "old", relative, filepath.Base(root))
	oldID := "local:" + filepath.Join(oldRoot, "tone.mp3")
	unrelated := filepath.Join(sandbox, "other", "tone.mp3")
	if _, err := application.UpdateSettings(func(settings *app.Settings) {
		settings.LocalMusicDirs = []string{oldRoot, unrelated}
		settings.Playback = app.PlaybackSnapshot{Current: oldID, Items: []app.PlaybackEntry{{ID: oldID, Title: "Imported audio"}}}
		settings.PlaybackResume.TrackKey = oldID
	}); err != nil {
		t.Fatal(err)
	}
	if err := restoreMobileImportPaths(application, base); err != nil {
		t.Fatal(err)
	}
	if err := application.RestorePlayback(); err != nil {
		t.Fatal(err)
	}
	if state := application.Snapshot(); !state.HasTrack || state.Playing || state.Path != filepath.Join(root, "tone.mp3") {
		t.Fatalf("restored playback = %q, loaded = %t, playing = %t", state.Path, state.HasTrack, state.Playing)
	}
	settings := application.SettingsSnapshot()
	if settings.LocalMusicDirs[0] != root || settings.LocalMusicDirs[1] != unrelated || settings.PlaybackResume.TrackKey != "local:"+filepath.Join(root, "tone.mp3") {
		t.Fatalf("restored library = %v, resume = %q", settings.LocalMusicDirs, settings.PlaybackResume.TrackKey)
	}
	if _, err := os.Stat(oldRoot); !os.IsNotExist(err) {
		t.Fatalf("old sandbox unexpectedly recreated: %v", err)
	}
}
