package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// 导出
// ─────────────────────────────────────────────────────────────────────────────

// 导出使用当前设置的裸 JSON，凭据不会被带出。

// 凭据一件都不进导出。
//
// Cookie / SESSDATA / bili_jct / refresh_token / 带签名的 URL **压根不在设置里**
// ——凭据在另一个文件里（macOS 是 `credentials/session.json`，Windows 是
// `credentials.dpapi`）。这一条靠的是那个布局事实，所以断言的是「导出的 JSON 里
// grep 不到这些词」，而不是「某一句 if 拦住了它们」。哪天有人把 cookie 加进
// `Settings`，这一条会红。
func TestTheExportCarriesNoCredentialOfAnyKind(t *testing.T) {
	s := DefaultSettings("")
	s.ProxySettings = ProxySettings{
		Type: ProxyHTTP, Host: "proxy.example.com", Port: intp(8080),
		Username: "alice", Password: "hunter2",
	}

	raw := exportJSON(t, s)
	text := string(raw)

	// 一、代理密码本身不在。
	if strings.Contains(text, "hunter2") {
		t.Fatalf("代理密码进了导出：%s", text)
	}
	// 二、代理配置的其余几格**在**——换台机器接着用是想要的，代理配置仍需保留结构，不能整段删除。
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	if _, ok := top["proxySettings"]; !ok {
		t.Fatalf("代理配置整个没了：%s", text)
	}
	if !strings.Contains(string(top["proxySettings"]), "proxy.example.com") {
		t.Fatalf("代理地址没导出（那不是秘密）：%s", top["proxySettings"])
	}
	var proxy map[string]any
	if err := json.Unmarshal(top["proxySettings"], &proxy); err != nil {
		t.Fatal(err)
	}
	if pw, present := proxy["password"]; !present || pw != "" {
		t.Fatalf("代理密码不是空串而是被整个删掉了：Biu 那边的类型是必填：%s", top["proxySettings"])
	}
	// 三、这一类词一个都不许出现（大小写都算）。
	for _, secret := range []string{
		"SESSDATA", "bili_jct", "DedeUserID", "refresh_token", "Cookie",
		"deadline=", "sign=", "sig=",
	} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(secret)) {
			t.Fatalf("导出里有 %q：%s", secret, text)
		}
	}
}

// 导出走白名单：设置里那些我们不认识的键一个都不带出去。
//
// 「白名单」比「导全部再删几个」多一层保证的是这个：不认识的键**可能**是凭据
// （这一版没有，但白名单的形状不随 Biu 以后加什么而失效）。
func TestTheExportCarriesExactlyTheKeysTheSettingsHave(t *testing.T) {
	raw := exportJSON(t, DefaultSettings(""))
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	want := ExportableKeys()
	if len(top) != len(want) {
		t.Fatalf("导出 %d 个键，设置结构体上 %d 个：\n导出 %v", len(top), len(want), keysOf(top))
	}
	for _, k := range want {
		if _, ok := top[k]; !ok {
			t.Fatalf("设置里的 %q 没有导出", k)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 导入
// ─────────────────────────────────────────────────────────────────────────────

// 导入是**逐键 merge**：文件里没有的键原样保留。
//
// 「导一份只改过主题的配置」不该把别的设置打回默认值。
func TestImportingOnlyChangesTheKeysTheFileMentions(t *testing.T) {
	cur := DefaultSettings("/Users/x/Downloads")
	cur.ThemeMode = ThemeModeLight
	cur.BorderRadius = 16
	cur.SideMenuCollapsed = true

	patch := []byte(`{"themeMode":"dark"}`)
	got, err := MergeSettings(cur, patch)
	if err != nil {
		t.Fatal(err)
	}
	if got.ThemeMode != ThemeModeDark {
		t.Fatalf("文件里改了主题，没生效：%v", got.ThemeMode)
	}
	if got.BorderRadius != 16 {
		t.Fatalf("文件里没提圆角，圆角被重置了：%v", got.BorderRadius)
	}
	if !got.SideMenuCollapsed {
		t.Fatalf("文件里没提侧栏折叠，侧栏折叠被重置了：%v", got.SideMenuCollapsed)
	}
	if got.DownloadPath != "/Users/x/Downloads" {
		t.Fatalf("文件里没提下载目录，下载目录被重置了：%q", got.DownloadPath)
	}
}

func TestImportingNeverOverwritesTheProxyPassword(t *testing.T) {
	cur := DefaultSettings("")
	cur.ProxySettings = ProxySettings{Type: ProxyHTTP, Host: "a", Password: "原来的密码"}

	// 文件里带着一个（从别的机器导出的）密码。
	got, err := MergeSettings(cur, []byte(`{"proxySettings":{"type":"socks5","host":"b","password":"别的机器的"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.ProxySettings.Password != "原来的密码" {
		t.Fatalf("导入把代理密码换成了文件里那个：%q", got.ProxySettings.Password)
	}
	// 其余几格照常并进来——不然「换台机器接着用」这件事就没了。
	if got.ProxySettings.Type != ProxySOCKS5 || got.ProxySettings.Host != "b" {
		t.Fatalf("代理的其余几格没并进来：%+v", got.ProxySettings)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 往返
// ─────────────────────────────────────────────────────────────────────────────

// 导出 → 导入 回到同一份设置（除了**故意**不带的那几样）。
//
// 这是「用户换个应用打开还是原样」那条承诺的直接检验。
func TestExportingAndImportingBackGetsYouTheSameSettings(t *testing.T) {
	s := DefaultSettings("/Users/x/Downloads")
	s.ThemeMode = ThemeModeDark
	s.ThemeMode = ThemeModeLight
	s.AudioQuality = AudioQualityLossless
	s.FontFamily = "PingFang SC"
	s.LocalMusicDirs = []string{"/Users/x/Music"}
	s.HiddenMenuKeys = []string{"我的"}
	s.SideMenuCollectionOrder.Collected = []int64{9, 8, 7}
	s.ProxySettings = ProxySettings{Type: ProxySOCKS5, Host: "h", Port: intp(1080),
		Username: "u", Password: "不带走"}

	dir := t.TempDir()
	path := filepath.Join(dir, "mygo-settings-2026-10-17T12-30-00.json")
	if err := ExportSettings(path, s); err != nil {
		t.Fatal(err)
	}

	// 回到一份**默认**设置上导——真导入时用户当前有什么是不确定的。
	got, err := ImportSettingsFile(path, DefaultSettings(""))
	if err != nil {
		t.Fatal(err)
	}
	if got.ThemeMode != s.ThemeMode || got.AudioQuality != s.AudioQuality ||
		got.FontFamily != s.FontFamily || got.BorderRadius != s.BorderRadius {
		t.Fatalf("往返之后设置不一样了：\n%+v\n%+v", got, s)
	}
	if len(got.LocalMusicDirs) != 1 || got.LocalMusicDirs[0] != "/Users/x/Music" {
		t.Fatalf("本地音乐目录没往返过去：%v", got.LocalMusicDirs)
	}
	if len(got.SideMenuCollectionOrder.Collected) != 3 {
		t.Fatalf("收藏排序没往返过去：%v", got.SideMenuCollectionOrder.Collected)
	}
	// 唯一该不一样的就是代理密码。
	if got.ProxySettings.Password != "" {
		t.Fatalf("代理密码跟着往返过来了：%q", got.ProxySettings.Password)
	}

	// 反过来再验一次：一份带着密码的设置被导出去、再导回来，密码也不会
	// 从**当前的**设置上被抹掉（往返的第一份就够钉住这一条，这里钉的是
	// 「导出 → 导入」这条路上中间那一段也没漏）。
	if err := ExportSettings(path, DefaultSettings("")); err != nil {
		t.Fatal(err)
	}
}

// 导出文件名带时间戳：用户导出三次是三个文件，不会互相覆盖。
//
// 文件名带时间戳，冒号换成横线以兼容 Windows。
func TestTheExportNameCarriesATimestampLikeBiuDoes(t *testing.T) {
	name := ExportFileName(mustTime(t, "2026-10-17T12:30:00Z"))
	if name != "mygo-settings-2026-10-17T12-30-00.json" {
		t.Fatalf("导出文件名是 %q，和 当前格式不一样", name)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 小工具
// ─────────────────────────────────────────────────────────────────────────────

func exportJSON(t *testing.T, s Settings) []byte {
	t.Helper()
	b, err := marshalExport(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func intp(n int) *int { return &n }

// mustReadFile 读一个文件，测试专用。
func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s：%v", path, err)
	}
	return string(b)
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
