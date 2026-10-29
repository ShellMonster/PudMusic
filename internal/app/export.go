// 配置的导出与导入：当前版本的裸设置 JSON，凭据一律不带。
//
// 导入按字段合并，文件中没有的字段保留当前设置；代理密码始终保留当前值。
package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// ExportFilePrefix 是导出文件名的前缀。
const ExportFilePrefix = "mygo-settings-"

// ExportSettings 把当前设置写成裸 JSON。path 是完整文件名。
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

// marshalExport 只导出当前设置结构里的字段，并清空代理密码。
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
	// 代理配置要在，但密码不能要。密码替换为空串，保留代理的其它设置。
	if ps, ok := out["proxySettings"]; ok {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(ps, &m); err == nil {
			m["password"] = json.RawMessage(`""`)
			out["proxySettings"], _ = json.Marshal(m)
		}
	}
	return json.MarshalIndent(out, "", "  ")
}

// MergeSettings 按字段把配置文件并入当前设置；文件缺少的字段保持当前值。
func MergeSettings(cur Settings, raw []byte) (Settings, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return cur, fmt.Errorf("app: 配置不是 JSON：%w", err)
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

// ExportFileName 给一份导出文件起名字：`mygo-settings-2026-10-17T12-30-00.json`。
//
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
