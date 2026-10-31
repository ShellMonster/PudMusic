// 把队列文件逐版本迁到当前版本。
//
// Restore 和 MergeIntoQueue 共用这一条链：读到旧文件时先在内存里完成迁移，
// 只有调用方确定要写回时才落盘。这样迁移失败或遇到未来版本都不会覆盖原文件。
package download

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func decodeQueueFile(data []byte) (queueFile, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return queueFile{}, err
	}
	if fields == nil {
		return queueFile{}, fmt.Errorf("download: 队列文件顶层不是对象")
	}
	tasksRaw, ok := fields["tasks"]
	if !ok || bytes.Equal(bytes.TrimSpace(tasksRaw), []byte("null")) {
		return queueFile{}, fmt.Errorf("download: 队列文件缺少 tasks")
	}
	var f queueFile
	if raw, ok := fields["version"]; ok {
		if err := json.Unmarshal(raw, &f.Version); err != nil {
			return queueFile{}, fmt.Errorf("download: 队列版本无效：%w", err)
		}
	}
	if err := json.Unmarshal(tasksRaw, &f.Tasks); err != nil {
		return queueFile{}, fmt.Errorf("download: 队列 tasks 无效：%w", err)
	}
	return f, nil
}

// queueMigrations 把 v 升到 v+1。key 是升级前的版本号。
//
// v0 是文件里没有 version 键的旧格式。当前 v0 与 v1 的任务结构相同，
// 所以迁移函数为空，但这条路必须保留，未来改格式时才能继续追加迁移。
var queueMigrations = map[int]func(*queueFile){
	0: func(*queueFile) {},
}

// runQueueMigrations 从 from 逐步升到当前版本。
func runQueueMigrations(f *queueFile, from int) error {
	for version := from; version < storeVersion; version++ {
		migrate, ok := queueMigrations[version]
		if !ok {
			return fmt.Errorf("download: 没有从队列 v%d 升到 v%d 的路", version, version+1)
		}
		migrate(f)
	}
	f.Version = storeVersion
	return nil
}
