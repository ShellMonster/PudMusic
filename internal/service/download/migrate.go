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
// v0 是「文件里没有 `version` 键」的那一份。**它必须有这条路**：没有的话
// `Restore` 判它「版本对不上」就把整个队列丢（用户重启看到空队列，而盘上躺着一堆
// `.part`），而 `MigrateFile` 直接报错，会让启动那一轮升级整轮回滚。
//
// 这一条迁移是空的：`Task` 的结构从 v1 起一个字段都没动过（`task.go` 的注释里
// 记着「改记账方式要涨 `QueueVersion`」，也就是**那时**才往这张表里写真东西）。
// 链是空的，但它得在——链断一环的后果是「认不出的文件被按旧语义重写一遍」。
var queueMigrations = map[int]func(*queueFile){
	0: func(*queueFile) {},
}

// runQueueMigrations 从 from 走到 storeVersion。链断了一环就返回错。
//
// 走完之后**把 f.Version 推到 storeVersion**：迁完的那一份是我们自己的形状，
// 而「它是第几版」这件事记在文件里。忘了推的后果是下一次启动又把同一份文件
// 当 v0 迁一遍——链是空的那一轮看不出问题，但只要有人往里加了真的迁移，
// 那一次就会被跑两次。
func runQueueMigrations(f *queueFile, from int) error {
	for v := from; v < storeVersion; v++ {
		m, ok := queueMigrations[v]
		if !ok {
			return fmt.Errorf("download: 没有从队列 v%d 升到 v%d 的路", v, v+1)
		}
		m(f)
	}
	f.Version = storeVersion
	return nil
}

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

	if err := runQueueMigrations(&f, f.Version); err != nil {
		return fmt.Errorf("%w，原文件没动", err)
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
