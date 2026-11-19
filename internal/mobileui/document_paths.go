package mobileui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ShellMonster/PudMusic/internal/app"
)

func restoreMobileImportPaths(application *app.App, base string) error {
	if base == "" {
		return nil
	}
	settings := application.SettingsSnapshot()
	settings.LocalMusicDirs = slices.Clone(settings.LocalMusicDirs)
	settings.Playback.Items = slices.Clone(settings.Playback.Items)
	changed := false
	rebase := func(path string) string {
		resolved := currentMobileImportPath(base, path)
		changed = changed || resolved != path
		return resolved
	}
	for index, root := range settings.LocalMusicDirs {
		settings.LocalMusicDirs[index] = rebase(root)
	}
	updateIdentity := func(identity string) string {
		if path, ok := strings.CutPrefix(identity, "local:"); ok {
			return "local:" + rebase(path)
		}
		return identity
	}
	for index := range settings.Playback.Items {
		settings.Playback.Items[index].ID = updateIdentity(settings.Playback.Items[index].ID)
	}
	settings.Playback.Current = updateIdentity(settings.Playback.Current)
	settings.PlaybackResume.TrackKey = updateIdentity(settings.PlaybackResume.TrackKey)
	if !changed {
		return nil
	}
	updated, err := application.UpdateSettings(func(next *app.Settings) {
		next.LocalMusicDirs = settings.LocalMusicDirs
		next.Playback = settings.Playback
		next.PlaybackResume = settings.PlaybackResume
	})
	if err == nil {
		application.SetLocalRoots(updated.LocalMusicDirs)
	}
	return err
}

func currentMobileImportPath(base, path string) string {
	if !filepath.IsAbs(path) {
		return path
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		return path
	}
	_, relative, ok := strings.Cut(filepath.Clean(path), "/Library/Application Support/"+filepath.Base(filepath.Dir(base))+"/"+localImportDirectory+"/")
	if !ok {
		return path
	}
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) > 2 || !strings.HasPrefix(parts[0], "audio-") {
		return path
	}
	candidate := filepath.Join(base, relative)
	if info, err := os.Stat(candidate); err == nil && (len(parts) == 1 && info.IsDir() || len(parts) == 2 && info.Mode().IsRegular()) {
		return candidate
	}
	return path
}
