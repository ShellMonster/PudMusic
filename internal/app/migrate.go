// 把一份设置文件迁到当前 schema，并按我们自己的信封写回去。
//
// # 这一层是给「多文件一致性事务」用的
//
// `settings.go` 里的 `LoadSettings` 已经会迁移（读的时候顺手走一遍链）。为什么
// 还要单独一个 `MigrateSettingsFile`：
//
//   - **备份与回滚**在事务里（`upgrade.go`），迁移必须是一个能被事务包住的、
//     **会落盘**的动作。`LoadSettings` 只读不写。
//   - **多文件一致性**要求四份文件一起升。启动时先统一升一遍，四份 store 随后
//     挂载时读到的已经是最新版，`LoadSettings` 那条路这一次什么也不用做。
//
// 两条路的**迁移链是同一份**（`migrations`），所以不会出现「导入时迁移了、启动时
// 又迁一次」这种两次走链的分裂——第二次是从最新版走的，链是空的。
//
// # 文件不存在不是错误
//
// 第一次运行、没有设置文件，这次升级对它无事可做。返回 nil，事务照样提交。
package app

import (
	"errors"
	"fmt"
	"os"
)

// MigrateSettingsFile 把 path 这份设置文件迁到当前 schema，就地写回。
//
// 三种结果：
//
//   - 文件不在 / 是 v0（Biu 导出的裸文件）—— 迁一遍写回去。
//   - 已经是当前版本 —— **一个字都不写**。重写一遍会让文件的 mtime 变了，而
//     有些备份工具按 mtime 判断「改过了」，平白让用户以为设置被动过。
//   - 来自更高的版本 —— 返回 `ErrSettingsFromNewer`，**不碰它**。
//
// 迁不动（链断了一环）时返回错，而且**原文件保持原样**：这一步的写发生在链
// 走完之后，链走不完就一个字都没写。
func MigrateSettingsFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("app: 读设置 %s：%w", path, err)
	}

	s, from, err := parseSettingsFile(raw)
	if err != nil {
		return fmt.Errorf("%w: %s (%v)", ErrSettingsCorrupt, path, err)
	}
	if from > SchemaVersion {
		return fmt.Errorf("%w: 文件是 v%d，我们只到 v%d（%s）",
			ErrSettingsFromNewer, from, SchemaVersion, path)
	}
	if from == SchemaVersion {
		return nil // 已经是最新版，一个字都不动
	}

	if err := applyMigrations(&s, from); err != nil {
		return fmt.Errorf("app: 升级设置 %s 失败，原文件没动：%w", path, err)
	}
	s.normalize()

	b, err := marshalSettingsFile(s)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, b); err != nil {
		return fmt.Errorf("app: 写回设置 %s：%w", path, err)
	}
	return nil
}
