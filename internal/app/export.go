// 配置的导出与导入：形状照 Biu 的设置页，凭据一律不带。
//
// ## 形状
//
// 照 `biu/src/pages/settings/export-import.tsx`：
//
//   - **导出**：裸的 `appSettings`，**外面没有信封**。`export-import.tsx:19` 写的是
//     `JSON.stringify(settingStore?.appSettings ?? defaultAppSettings, null, 2)`。
//   - **导入**：逐个已知键去**顶层**取，然后 merge 进去（`:46-51`）。
//
// 与设置文件本身（`settings.json`，带 `schemaVersion` + `appSettings` 信封）是
// **两个形状**，别混：设置文件是程序自己维护的，带版本号好做迁移；导出的是给人
// 看、给人拷去另一台机器的裸文件。它是 Biu 设置页「导出配置」下载下来的那一形状，
// 所以用户在 Biu 里能直接导入我们导出的，反过来也一样。
//
// ## 凭据一件都不进导出
//
// 导出走**白名单**（只导 `knownSettingsKeys` 里的键），而不是「导全部再删几个」。
// 白名单多一层保证的是：文件里那些**我们不认识的键**（`Settings.extra`，Biu 或
// 未来某一版加的）一律不带出去——
//
//   - 一个我们不认识的键**可能**是凭据。照 `app-setting.d.ts` 逐字对过的这一版
//     没有，但「白名单」这个形状的保证不随 Biu 以后加什么而失效，而「黑名单」
//     会。
//   - 代理密码是 `proxySettings.password`，**在白名单里**（键名和 Biu 一致），
//     所以要单独抹掉。抹掉之后 `proxySettings` 里的其余几格（类型、主机、端口、
//     用户名）照导——代理地址不是秘密，用户换台机器接着用是想要的。
//
// Cookie / SESSDATA / bili_jct / refresh_token / 带签名的 URL 这一类**根本不在
// 设置里**：凭据存在系统钥匙串（`credentials.go` 的 `keychainStore`），不在用户
// 数据目录里的任何一份 JSON 中。所以它们不是「被排除了」，是「压根没有地方可导」。
// 这一条要有测试钉住（`export_test.go`），因为它靠的是「凭据不在这几个文件里」
// 这个布局事实，而不是某一句 `if`。
package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// ExportFilePrefix 是导出文件名的前缀，照 Biu 的
// `biu-settings-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`。
//
// 前缀也用 `biu-` 而不是 `mygo-`：这份文件丢进 Biu 那边是能直接导入的（键名一致），
// 文件名带个陌生的前缀不会影响任何东西，但会让用户在下载列表里认不出它是配置。
const ExportFilePrefix = "biu-settings-"

// ExportSettings 把一份设置写成 Biu 能导入的裸 JSON。path 是**完整的文件名**，
// 不是目录——调用方要弹文件框让用户自己选放哪（照 Biu 的行为：它也是让浏览器
// 下载到用户选的目录）。
func ExportSettings(path string, s Settings) error {
	b, err := marshalExport(s)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("app: 建导出目录：%w", err)
		}
	}
	// 原子写：导出是**覆盖同一个文件**（用户选了两次同一个名字），写到一半断电
	// 的话，用户原来那份配置也没了。
	return writeFileAtomic(path, b)
}

// marshalExport 把一份设置变成导出的那份 JSON。
//
// 三件事，顺序不能换：
//
//  1. 摊平成一张 map（`Settings.MarshalJSON` 会把 `extra` 也摊进来）。
//  2. **只留白名单里的键** —— 这一步顺手把 `extra` 全部丢掉。
//  3. 抹掉代理密码。
func marshalExport(s Settings) ([]byte, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, err
	}
	out := make(map[string]json.RawMessage, len(knownSettingsKeys))
	for k := range knownSettingsKeys {
		if v, ok := all[k]; ok {
			out[k] = v
		}
	}
	// 代理配置要在，但密码不能要。抹的方式是**把 password 这一格换成空串**而不是
	// 整个删掉 proxySettings：删掉的话 Biu 那边读到的 proxySettings 是 undefined，
	// 而它的类型是必填（`app-setting.d.ts`），界面上会渲染成一片空白。
	if ps, ok := out["proxySettings"]; ok {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(ps, &m); err == nil {
			m["password"] = json.RawMessage(`""`)
			out["proxySettings"], _ = json.Marshal(m)
		}
	}
	return json.MarshalIndent(out, "", "  ")
}

// MergeSettings 把一份配置文件并进 cur，返回并完之后的设置。
//
// 语义照 Biu 的导入（`export-import.tsx:46-53`）：**逐个已知键**去文件顶层取，
// **文件里有的键覆盖，没有的原样保留**。所以「导出一份只改过主题的配置再导
// 进来」不会把别的设置打回默认值。
//
// 两处刻意和 Biu 不同：
//
//   - **凭据不并**：文件里带的代理密码丢掉，保留 `cur` 现在这一个。
//   - **只并我们认识的键**：文件里那些不认识的键原样带走（存进 `extra`），跟着
//     `cur` 一起留着，下次写盘时还在——用户在 Biu 那边加的键不该因为在 MyGo
//     里导入了一次配置就没了。
func MergeSettings(cur Settings, raw []byte) (Settings, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return cur, fmt.Errorf("app: 配置不是 JSON：%w", err)
	}
	// 带信封的那种（别人给的是 Biu 的 electron-store 文件，不是设置页导出的那份）
	// 先把信封剥掉，否则所有键都埋在一层里，一个也取不到。
	if inner, ok := top["appSettings"]; ok {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(inner, &nested); err != nil {
			return cur, fmt.Errorf("app: 配置里的 appSettings 读不懂：%w", err)
		}
		top = nested
	}

	patch := make(map[string]json.RawMessage, len(top))
	for k, v := range top {
		patch[k] = v
	}
	// 代理密码：文件里那个丢掉（如果有的话），然后用 `cur` 现在的补回去。
	if ps, ok := patch["proxySettings"]; ok {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(ps, &m); err == nil {
			if _, has := m["password"]; has {
				m["password"] = json.RawMessage(`""`)
			}
			if curPw := cur.ProxySettings.Password; curPw != "" {
				m["password"] = mustMarshal(curPw)
			}
			patch["proxySettings"] = mustMarshal(m)
		}
	}

	base, err := json.Marshal(cur)
	if err != nil {
		return cur, err
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(base, &merged); err != nil {
		return cur, err
	}
	for k, v := range patch {
		merged[k] = v
	}
	b, err := json.Marshal(merged)
	if err != nil {
		return cur, err
	}
	var out Settings
	if err := json.Unmarshal(b, &out); err != nil {
		return cur, fmt.Errorf("app: 并完的配置读不懂：%w", err)
	}
	out.normalize()
	return out, nil
}

// ImportSettingsFile 从一份配置文件读出要并进 cur 的那份设置。
//
// 它不碰磁盘上正在用的设置——那是调用方的事（`SettingsStore.Update` 或
// `ApplySettings`）。这里只管「读 + 并」。
func ImportSettingsFile(path string, cur Settings) (Settings, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return cur, fmt.Errorf("app: 读配置 %s：%w", path, err)
	}
	return MergeSettings(cur, raw)
}

// mustMarshal 是 Marshal 的一个短写法：这些地方序列化的一定是 map / string /
// bool，失败的话是程序写错了而不是用户数据有问题，所以直接 panic ——
// 让它带着调用栈炸在这里，而不是把一个 error 一路传回 UI。
func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("app: 序列化导出片段失败：%v", err))
	}
	return b
}

// ExportFileName 给一份导出文件起名字：`biu-settings-2026-10-17T12-30-00.json`。
//
// 时间格式和 Biu 逐字一样（`toISOString().slice(0,19).replace(/[:T]/g,"-")`）：
// 文件名里带时间，用户导出三次就是三个文件，不会互相覆盖；冒号在 Windows 上
// 非法，所以换成横线。
func ExportFileName(now time.Time) string {
	return fmt.Sprintf("%s%s.json", ExportFilePrefix, now.Format("2006-01-02T15-04-05"))
}

// ExportableKeys 是这份导出会带出去的键，按字母排。
//
// 供界面层用（诊断信息里「导出带哪些键」）和测试用。返回的是一份拷贝，调用方
// 改它不影响下一次。
func ExportableKeys() []string {
	out := make([]string, 0, len(knownSettingsKeys))
	for k := range knownSettingsKeys {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
