// 快捷键和搜索历史这两份文件的迁移入口。
//
// ## 为什么单独一个文件
//
// 迁移的**动作**属于各自那一份（`shortcuts.go` 的 `shortcutMigrations`、
// `searchhistory.go` 的 `searchHistoryMigrations`），而**编排**——四份文件一起
// 升、中途断电能回滚——属于 `internal/app` 的事务（见 `app/upgrade.go`）。
// 这一份是中间那一层：把「读 → 认形状 → 走链 → 写回」这四步，对两份文件各做一遍。
//
// 两条路的**链是同一份**：挂载时的 `readShortcuts` / `readSearchHistory` 也会走
// 迁移链，只是它们只读不写。这里先升一遍，那两条路读到的已经是最新版。
//
// ⚠️ 一处和读盘路径的差别：读盘路径遇到「来自更新的版本」时**只读不写**（各 store
// 自己的 `ro` 标记），这里遇到同一个情况是**返回错**。区别在于谁来处置：
// 读盘路径有 store 能把标志举着，这里只有一次性的迁移，让它安静地返回 nil 会让
// 事务以为「升完了」，于是把总标记推到最新版——下次升级就再也不会碰这四份文件了。
package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// MigrateShortcutsFile 把 path 这份快捷键文件迁到当前 schema，就地写回。
func MigrateShortcutsFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("biu: 读快捷键 %s：%w", path, err)
	}

	from, s, ok, err := probeShortcuts(raw)
	if err != nil {
		return fmt.Errorf("biu: 快捷键文件 %s 读不懂，原文件没动：%w", path, err)
	}
	if !ok {
		return fmt.Errorf("biu: %s 认不出是快捷键文件（顶层没有 shortcuts），原文件没动", path)
	}
	if from > shortcutSchema {
		return fmt.Errorf("biu: 快捷键文件是 v%d 的，这一版认 v%d（%s），没有动它",
			from, shortcutSchema, path)
	}
	if from == shortcutSchema {
		return nil
	}
	if err := runShortcutMigrations(&s, from); err != nil {
		return fmt.Errorf("biu: 升级快捷键 %s 失败，原文件没动：%w", path, err)
	}

	b, err := json.Marshal(shortcutFile{SchemaVersion: shortcutSchema, shortcutSettings: s})
	if err != nil {
		return err
	}
	if err := writeShortcutsAtomic(path, b); err != nil {
		return fmt.Errorf("biu: 写回快捷键 %s：%w", path, err)
	}
	return nil
}

// MigrateSearchHistoryFile 把 path 这份搜索历史迁到当前 schema，就地写回。
func MigrateSearchHistoryFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("biu: 读搜索历史 %s：%w", path, err)
	}

	var f searchHistoryFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("biu: 搜索历史 %s 读不懂，原文件没动：%w", path, err)
	}
	if f.SchemaVersion > searchHistorySchema {
		return fmt.Errorf("biu: 搜索历史是 v%d 的，这一版认 v%d（%s），没有动它",
			f.SchemaVersion, searchHistorySchema, path)
	}
	if f.SchemaVersion == searchHistorySchema {
		return nil
	}
	for v := f.SchemaVersion; v < searchHistorySchema; v++ {
		m, ok := searchHistoryMigrations[v]
		if !ok {
			return fmt.Errorf("biu: 没有从搜索历史 v%d 升到 v%d 的路，原文件没动", v, v+1)
		}
		m(&f)
	}

	b, err := json.Marshal(searchHistoryFile{
		SchemaVersion: searchHistorySchema,
		Items:         f.Items,
	})
	if err != nil {
		return err
	}
	if err := writeSearchHistoryAtomic(path, b); err != nil {
		return fmt.Errorf("biu: 写回搜索历史 %s：%w", path, err)
	}
	return nil
}

// probeShortcuts 认一份快捷键文件的形状，返回「它是第几版」和「读出来的内容」。
//
// 和 `readShortcuts` 的前半段一模一样（判据只有一条：**顶层有没有 `shortcuts`**），
// 拆出来是为了让读盘和迁移共用一份判据——两处判据一旦不同步，某一份文件在读盘
// 时是「Biu 的快捷键」、在迁移时是「不是快捷键文件」，而这两种表现都安静。
func probeShortcuts(raw []byte) (from int, s shortcutSettings, ok bool, err error) {
	var probe struct {
		SchemaVersion *int            `json:"schemaVersion"`
		Shortcuts     *[]shortcutItem `json:"shortcuts"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return 0, shortcutSettings{}, false, err
	}
	if probe.Shortcuts == nil {
		// 「文件里没有 shortcuts 键」和「shortcuts 是个空数组」必须分开：前者
		// 是认不出是什么文件，后者是一份合法的空快捷键。
		return 0, shortcutSettings{}, false, nil
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, shortcutSettings{}, false, err
	}
	if probe.SchemaVersion != nil {
		from = *probe.SchemaVersion
	}
	return from, s, true, nil
}

// runShortcutMigrations 从 from 走到 shortcutSchema。链断了一环就返回错。
func runShortcutMigrations(s *shortcutSettings, from int) error {
	for v := from; v < shortcutSchema; v++ {
		m, ok := shortcutMigrations[v]
		if !ok {
			return fmt.Errorf("没有从快捷键 v%d 升到 v%d 的路", v, v+1)
		}
		m(s)
	}
	return nil
}

// marshalShortcutFile 按我们自己的 schema 把一份快捷键写成字节。
func marshalShortcutFile(s shortcutSettings) ([]byte, error) {
	return json.Marshal(shortcutFile{SchemaVersion: shortcutSchema, shortcutSettings: s})
}

// probeSchemaVersion 报一份快捷键文件自己说的版本。没有 `schemaVersion` 键就是
// 0——那是 Biu 直接导出的、以及 Biu 自己 electron-store 里躺着的那一份
// （`readShortcuts` 的注释）。
func probeSchemaVersion(raw []byte) (int, error) {
	var probe struct {
		SchemaVersion *int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return 0, err
	}
	if probe.SchemaVersion == nil {
		return 0, nil
	}
	return *probe.SchemaVersion, nil
}
