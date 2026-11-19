package mobileui

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo/ui"
)

// TestMobileSettingsOpensSubpage checks that the native settings menu opens a subpage and returns to the menu.
func TestMobileSettingsOpensSubpage(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	if err := a.SetThemeMode(app.ThemeModeLight); err != nil {
		t.Fatal(err)
	}
	s := newShell(context.Background(), a)
	s.switchTab(2)
	s.push(route{kind: pageSettings, title: "设置"})
	tester := ui.NewTester(s.view, 390, 844)
	if dir := os.Getenv("BIU_SHOT_DIR"); dir != "" {
		file, err := os.Create(filepath.Join(dir, "mobile-light-settings.png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, tester.Image())
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("settings screenshot: encode = %v, close = %v", err, closeErr)
		}
	}
	if err := tester.Click("外观"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tester.Texts(), "深浅模式") {
		t.Fatalf("appearance settings did not open, got %q", tester.Texts())
	}
	if err := tester.Click("返回"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tester.Texts(), "播放与下载") {
		t.Fatalf("back did not return to settings, got %q", tester.Texts())
	}
}

// TestTabletSettingsKeepsTheIndexBesideTheDetail verifies that switching sections uses the master list and that Back returns to Settings.
func TestTabletSettingsKeepsTheIndexBesideTheDetail(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	s := newShell(t.Context(), a)
	s.switchTab(2)
	s.push(route{kind: pageSettings, title: "设置"})
	tester := ui.NewTester(s.view, 780, 844)
	for _, label := range []string{"外观", "网络代理"} {
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	index, foundIndex := tester.Find("播放与下载")
	detail, foundDetail := tester.Find("使用系统代理")
	if !foundIndex || !foundDetail || index.X+index.W > detail.X || detail.Y > index.Y+160 {
		t.Fatalf("settings list and detail did not appear side by side: %v, %v", index, detail)
	}
	mobileLibraryShot(t, tester, "tablet-settings.png")
	if err := tester.Click("返回"); err != nil {
		t.Fatal(err)
	}
	if _, found := tester.Find("使用系统代理"); found {
		t.Fatal("Back left the settings detail open")
	}
}

// TestMobileSettingsVisibilityPersists checks that hiding and restoring a My page entry changes the saved preference.
func TestMobileSettingsVisibilityPersists(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	if err := a.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	s := &shell{app: a}
	page := &route{kind: pageSetting, id: "menu"}
	tester := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().PaddingX(16).Children(func() { s.settings(c, page) })
	}, 390, 700)
	for _, hidden := range []bool{true, false} {
		if err := tester.Click("显示历史记录"); err != nil {
			t.Fatal(err)
		}
		if err := a.FlushSettings(); err != nil {
			t.Fatal(err)
		}
		settings, err := app.LoadSettings(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := slices.Contains(settings.HiddenMenuKeys, "/history"); got != hidden {
			t.Fatalf("saved history visibility hidden = %v, want %v", got, hidden)
		}
	}
}

// TestMobileSettingsThemePersists checks that choosing a theme updates both the running app and its saved settings.
func TestMobileSettingsThemePersists(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	if err := a.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	s := &shell{app: a}
	page := &route{kind: pageSetting, id: "appearance"}
	tester := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().PaddingX(16).Children(func() { s.settings(c, page) })
	}, 390, 700)
	if err := tester.Click("深色"); err != nil {
		t.Fatal(err)
	}
	if got := a.SettingsSnapshot().ThemeMode; got != app.ThemeModeDark {
		t.Fatalf("running theme = %q, want dark", got)
	}
	if err := a.FlushSettings(); err != nil {
		t.Fatal(err)
	}
	persisted, err := app.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ThemeMode != app.ThemeModeDark {
		t.Fatalf("saved theme = %q, want dark", persisted.ThemeMode)
	}
}

// TestMobileSettingsQualityChooser checks that the touch picker applies a playback quality preference.
func TestMobileSettingsQualityChooser(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	s := &shell{app: a}
	page := &route{kind: pageSetting, id: "general"}
	tester := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().PaddingX(16).Children(func() { s.settings(c, page) })
	}, 390, 800)
	for _, label := range []string{"选择默认音质", "标准"} {
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if got := a.SettingsSnapshot().AudioQuality; got != app.AudioQualityMedium {
		t.Fatalf("audio quality = %q, want standard", got)
	}
}

// TestMobileSettingsProxyRejectsIncompleteEndpoint checks that an invalid custom proxy leaves the current connection mode intact.
func TestMobileSettingsProxyRejectsIncompleteEndpoint(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	s := &shell{app: a}
	page := &route{kind: pageSetting, id: "proxy"}
	tester := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().PaddingX(16).Children(func() { s.settings(c, page) })
	}, 390, 800)
	for _, label := range []string{"自定义代理", "保存并测试"} {
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	for _, message := range []string{"请填写代理主机", "端口必须为 1–65535"} {
		if !slices.Contains(tester.Texts(), message) {
			t.Fatalf("missing validation message %q in %q", message, tester.Texts())
		}
	}
	if got := a.SettingsSnapshot().ProxySettings.Type; got != app.ProxyNone {
		t.Fatalf("proxy mode = %q after rejecting invalid custom proxy, want system", got)
	}
}
