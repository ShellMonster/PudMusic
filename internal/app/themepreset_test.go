package app_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// TestThemePresetDefaultsToWarmPaper：默认是暖纸。
func TestThemePresetDefaultsToWarmPaper(t *testing.T) {
	if got := app.DefaultSettings("").ThemePreset; got != app.ThemePresetWarmPaper {
		t.Fatalf("默认方案是 %q，想要暖纸", got)
	}
}

// TestThemePresetLoad：认识的三套原样读回，不认识的、空的退回默认暖纸而不是报错。
func TestThemePresetLoad(t *testing.T) {
	cases := []struct {
		raw  string
		want app.ThemePreset
	}{
		{`"warm-paper"`, app.ThemePresetWarmPaper},
		{`"dusk-amber"`, app.ThemePresetDuskAmber},
		{`"soft-peach"`, app.ThemePresetSoftPeach},
		{`"classic"`, app.ThemePresetClassic},
		{`""`, app.ThemePresetWarmPaper},
		{`"neon"`, app.ThemePresetWarmPaper},
	}
	for _, c := range cases {
		path := filepath.Join(t.TempDir(), "settings.json")
		body := `{"schemaVersion": 1, "appSettings": {"themePreset": ` + c.raw + `}}`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := app.LoadSettings(path)
		if err != nil {
			t.Fatal(err)
		}
		if s.ThemePreset != c.want {
			t.Errorf("themePreset %s 读成 %q，想要 %q", c.raw, s.ThemePreset, c.want)
		}
	}
}
