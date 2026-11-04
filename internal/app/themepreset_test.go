package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestAccentDefaultsToCinnabar：新设置的强调色是朱砂（「深夜唱片行」§0′）。
func TestAccentDefaultsToCinnabar(t *testing.T) {
	if got := app.DefaultSettings("").Accent; got != app.AccentCinnabar {
		t.Fatalf("默认强调色是 %q，想要朱砂", got)
	}
}

// loadAppSettings 把 appSettings 那一段写进临时设置文件再读回来。
func loadAppSettings(t *testing.T, body string) app.Settings {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	doc := `{"schemaVersion": 1, "appSettings": {` + body + `}}`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := app.LoadSettings(path)
	if err != nil {
		t.Fatalf("读 %s 报错：%v", body, err)
	}
	return s
}

// TestAccentLoad：四种强调色原样读回；空的、不认识的落到朱砂，不报错。
func TestAccentLoad(t *testing.T) {
	cases := []struct {
		raw  string
		want app.Accent
	}{
		{`"cinnabar"`, app.AccentCinnabar},
		{`"indigo"`, app.AccentIndigo},
		{`"pine"`, app.AccentPine},
		{`"amber"`, app.AccentAmber},
		{`""`, app.AccentCinnabar},
		{`"neon"`, app.AccentCinnabar},
	}
	for _, c := range cases {
		if got := loadAppSettings(t, `"accent": `+c.raw).Accent; got != c.want {
			t.Errorf("accent %s 读成 %q，想要 %q", c.raw, got, c.want)
		}
	}
}

// TestOldThemeSettingsMigrateSilently：老用户的设置文件里还有旧方案、自定义颜色、
// 「定时」档——读进来不报错，旧字段丢掉，外观落到默认值（定时 → 跟随系统）。
func TestOldThemeSettingsMigrateSilently(t *testing.T) {
	for _, body := range []string{
		`"themePreset": "warm-paper", "primaryColor": "#1ed760", "backgroundColor": "#101010", "themeMode": "schedule"`,
		`"themePreset": "classic", "primaryColor": "红色", "backgroundColor": "#zzz", "themeMode": "dark"`,
		`"themePreset": 3, "primaryColor": null, "backgroundColor": ""`,
	} {
		s := loadAppSettings(t, body)
		if s.Accent != app.AccentCinnabar {
			t.Errorf("%s：强调色读成 %q，想要朱砂", body, s.Accent)
		}
	}
	if got := loadAppSettings(t, `"themeMode": "schedule"`).ThemeMode; got != app.ThemeModeSystem {
		t.Errorf("定时档读成 %q，想要跟随系统", got)
	}
	if got := loadAppSettings(t, `"themeMode": "dark", "themePreset": "ink-paper"`).ThemeMode; got != app.ThemeModeDark {
		t.Errorf("深色档跟着旧方案一起丢了：%q", got)
	}
}

// TestSavedSettingsDropOldThemeKeys：写回去的文件里不再有那三个旧键。
func TestSavedSettingsDropOldThemeKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	doc := `{"schemaVersion": 1, "appSettings": {"themePreset": "celadon", "primaryColor": "#ff0000", "backgroundColor": "#000000"}}`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := app.OpenSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Update(func(s *app.Settings) { s.Accent = app.AccentAmber }); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// backgroundColor 不在这里查：全屏播放器偏好里另有一个同名键（omitempty）。
	for _, key := range []string{`"themePreset"`, `"primaryColor"`} {
		if strings.Contains(string(b), key) {
			t.Errorf("写回去的设置里还有旧键 %s：\n%s", key, b)
		}
	}
	if !strings.Contains(string(b), `"accent": "amber"`) {
		t.Errorf("新的 accent 没写进去：\n%s", b)
	}
}
