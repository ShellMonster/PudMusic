// 把队列文件迁到当前版本。
//
// 形状照 `MigrateSettingsFile`（`internal/app/migrate.go`）：给多文件一致性事务
// （`app/upgrade.go`）调的一个「会落盘」的迁移，和 `Restore` 那条读时迁移的路
// 共用**同一份链**。
//
// 队列的链现在是空的——`QueueVersion` 一直是 1，还没有一次结构改动。留着这张表
// 是为了第一次改动时有一条明着的地方可写：链断一环的后果是「认不出的文件被按旧
// 语义重写一遍」，而备份和回滚已经在事务里了，那时要做的只是往这张表里写一条。
package download

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// queueMigrations 把 v 升到 v+1。key 是**升级前**的版本号。
//
// v0 是「文件里没有 version 键」的那一份——包括用户手写的一份和我们读不出来的
// 一份。真出现时要迁什么，到时候在这里写。
var queueMigrations = map[int]func(*queueFile){}

// MigrateFile 把 path 这份队列文件迁到当前版本，就地写回。
//
// 文件不存在不是错误（第一次运行）。已经是当前版本时**一个字都不写**。
//
// 版本比当前**高**时返回错、不碰文件：那是未来的版本写的，我们无权处置。
// 事务（`app/upgrade.go`）看到错会回滚这一轮，于是整个数据目录保持原样。
func MigrateFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("download: 读队列 %s：%w", path, err)
	}

	var f queueFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("download: 队列文件读不懂（%s），原文件没动：%w",
			filepath.Base(path), err)
	}
	if f.Version > storeVersion {
		return fmt.Errorf("download: 队列文件是第 %d 版的，这一版认第 %d 版（%s），没有动它",
			f.Version, storeVersion, filepath.Base(path))
	}
	if f.Version == storeVersion {
		return nil
	}

	for v := f.Version; v < storeVersion; v++ {
		m, ok := queueMigrations[v]
		if !ok {
			return fmt.Errorf("download: 没有从队列 v%d 升到 v%d 的路，原文件没动", v, v+1)
		}
		m(&f)
	}

	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("download: 序列队列：%w", err)
	}
	if err := writeAtomic(path, b); err != nil {
		return fmt.Errorf("download: 写回队列 %s：%w", path, err)
	}
	return nil
}
