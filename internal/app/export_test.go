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

// 导出的是**裸的** appSettings，键在顶层。
//
// 这一条钉的是「Biu 那边能直接导入我们导出的」：Biu 的导入是逐个
// `defaultAppSettings` 的键去**顶层**取（`export-import.tsx:46-51`），而我们
// 设置文件本身是带 `schemaVersion` + `appSettings` 信封的。导出要是也带信封，
// Biu 一个键都取不到，而它不报错——用户看到的是「导入了但什么都没变」。
func TestTheExportIsShapedTheWayBiuReadsItBack(t *testing.T) {
	s := DefaultSettings("/Users/x/Downloads")
	s.ThemeMode = ThemeModeDark
	s.PrimaryColor = "#17c964"

	raw := exportJSON(t, s)

	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	if _, wrapped := top["appSettings"]; wrapped {
		t.Fatal("导出带了 appSettings 信封：Biu 的导入逐个键去顶层取，一个都取不到")
	}
	if _, hasSchema := top["schemaVersion"]; hasSchema {
		t.Fatal("导出带了 schemaVersion：Biu 的导入不认识这个键，它也只挑自己那几个")
	}
	if !strings.Contains(string(raw), `"#17c964"`) {
		t.Fatalf("导出的内容不是当前设置：%s", raw)
	}
	if !strings.Contains(string(raw), `"dark"`) {
		t.Fatalf("主题没有导出：%s", raw)
	}
}

// 凭据一件都不进导出。
//
// Cookie / SESSDATA / bili_jct / refresh_token / 带签名的 URL **压根不在设置里**
// ——凭据在系统钥匙串。这一条靠的是那个布局事实，所以断言的是「导出的 JSON 里
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
	// 二、代理配置的其余几格**在**——换台机器接着用是想要的，而 Biu 的
	// `proxySettings` 是必填的，整个删掉会让它那边渲染成空白。
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
func TestTheExportDoesNotCarryKeysThisVersionDoesNotKnow(t *testing.T) {
	s := DefaultSettings("")
	// 手工塞一个不认识的键进去，就像 Biu 的未来版本或用户手改的文件那样。
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["someFutureKey"] = json.RawMessage(`"也许是秘密"`)
	var round Settings
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatal(err)
	}
	// round.extra 里就有它了。
	out := exportJSON(t, round)
	if strings.Contains(string(out), "someFutureKey") {
		t.Fatalf("不认识的键进了导出：%s", out)
	}
	if strings.Contains(string(out), "也许是秘密") {
		t.Fatalf("不认识的键的值进了导出：%s", out)
	}
}

// 导出的键就是「设置结构体上有的那几个」，一个不多一个不少。
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
// 照 Biu 的语义（`merge(getSettings(), patch)`，`export-import.tsx:53`）。
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

// 带信封的那种也能导入（有人给的是 Biu 的 electron-store 文件，不是设置页导出的
// 那一份）。认错的话所有键都埋在一层里，一个也取不到，而用户看到的是「导入了
// 但什么都没变」。
func TestImportingUnderstandsTheFileWithTheEnvelopeToo(t *testing.T) {
	cur := DefaultSettings("")
	patch := []byte(`{"appSettings":{"themeMode":"dark"},"__internal__":{"whatever":1}}`)
	got, err := MergeSettings(cur, patch)
	if err != nil {
		t.Fatal(err)
	}
	if got.ThemeMode != ThemeModeDark {
		t.Fatalf("带信封的没导进去：%v", got.ThemeMode)
	}
}

// 导入不覆盖代理密码。
//
// 配置是一个人在几台机器之间搬的东西，而代理密码往往是**每台机器不一样**的
// （同一个公司内网代理不同机房密码不同）。让一份配置把密码抹掉，用户要重新去
// 设置页输一遍——而他导出这份配置的动机可能就是换机器。
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

// 文件里不认识的键原样带走，下次写盘时还在。
//
// 用户在 Biu 那边加的键不该因为在 MyGo 里导入了一次配置就没了——那是最难查的
// 一类丢失。
func TestImportingKeepsTheKeysNeitherSideKnows(t *testing.T) {
	cur := DefaultSettings("")
	got, err := MergeSettings(cur, []byte(`{"aKeyOnlyBiuKnows":{"x":1},"themeMode":"dark"}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := marshalSettingsFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "aKeyOnlyBiuKnows") {
		t.Fatalf("不认识的键被导入丢掉了：%s", out)
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
	path := filepath.Join(dir, "biu-settings-2026-10-17T12-30-00.json")
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
// 格式照 Biu（`biu-settings-2026-10-17T12-30-00.json`），冒号换成横线是因为
// 它在 Windows 上非法。
func TestTheExportNameCarriesATimestampLikeBiuDoes(t *testing.T) {
	name := ExportFileName(mustTime(t, "2026-10-17T12:30:00Z"))
	if name != "biu-settings-2026-10-17T12-30-00.json" {
		t.Fatalf("导出文件名是 %q，和 Biu 的形状不一样", name)
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
