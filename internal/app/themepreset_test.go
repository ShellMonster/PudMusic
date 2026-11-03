package app_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestThemePresetDefaultsToCobalt：默认是钴蓝。
func TestThemePresetDefaultsToCobalt(t *testing.T) {
	if got := app.DefaultSettings("").ThemePreset; got != app.ThemePresetCobalt {
		t.Fatalf("默认方案是 %q，想要钴蓝", got)
	}
}

// TestThemePresetLoad：认识的方案原样读回，不认识的、空的退回默认钴蓝而不是报错。
func TestThemePresetLoad(t *testing.T) {
	cases := []struct {
		raw  string
		want app.ThemePreset
	}{
		{`"warm-paper"`, app.ThemePresetWarmPaper},
		{`"dusk-amber"`, app.ThemePresetDuskAmber},
		{`"soft-peach"`, app.ThemePresetSoftPeach},
		{`"celadon"`, app.ThemePresetCeladon},
		{`"ink-paper"`, app.ThemePresetInkPaper},
		{`"sandalwood"`, app.ThemePresetSandalwood},
		{`"indigo-night"`, app.ThemePresetIndigoNight},
		{`"cobalt"`, app.ThemePresetCobalt},
		{`"classic"`, app.ThemePresetClassic},
		{`""`, app.ThemePresetCobalt},
		{`"neon"`, app.ThemePresetCobalt},
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
