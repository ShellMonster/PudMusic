package mobileui

import (
	"path/filepath"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo/ui"
)

// TestMobilePlayerKeepsEarlierTracksInQueue verifies that opening a list at the second song retains the first song as a selectable entry.
func TestMobilePlayerKeepsEarlierTracksInQueue(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	if err := a.SetThemeMode(app.ThemeModeLight); err != nil {
		t.Fatal(err)
	}
	a.TestOnlyNoAutoLyrics()
	a.SetVolume(0)
	tracks := []app.Item{
		{Source: app.SourceLocal, Title: "第一首", Path: filepath.Join("..", "service", "audio", "testdata", "tone.mp3")},
		{Source: app.SourceLocal, Title: "第二首", Path: filepath.Join("..", "service", "audio", "testdata", "gapless.mp3")},
	}
	if err := a.PlayFromIndex(tracks, 1); err != nil {
		t.Fatal(err)
	}
	a.Pause()
	s := newShell(t.Context(), a)
	s.switchTab(2)
	tester := ui.NewTester(s.view, 390, 844)
	if err := tester.Click("打开满屏播放器：第二首"); err != nil {
		t.Fatal(err)
	}
	mobileLibraryShot(t, tester, "mobile-light-player.png")
	artwork, ok := tester.Find("当前封面的操作")
	if !ok {
		t.Fatal("the artwork cannot be reached")
	}
	startX, startY := artwork.X+artwork.W*.75, artwork.Y+artwork.H/2
	tester.TouchDown(1, startX, startY)
	tester.TouchMove(1, startX-100, startY)
	tester.TouchUp(1, startX-100, startY)
	tester.Frame()
	if _, ok := tester.Find("歌词设置"); !ok {
		t.Fatal("swiping left on the artwork did not open lyrics")
	}
	for _, label := range []string{"封面", "播放列表", "第一首"} {
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if current := a.Queue().Snapshot().CurrentIndex(); current != 0 {
		t.Fatalf("playing queue index %d after selecting the first entry", current)
	}
	mobileLibraryShot(t, tester, "mobile-light-queue.png")
	if err := tester.Click("关闭播放列表"); err != nil {
		t.Fatal(err)
	}
	if _, found := tester.Find("收起满屏播放器"); !found {
		t.Fatal("closing the queue did not return to the full player")
	}
}
