// 从另一份队列文件并入任务：保留当前队列顺序，并按任务 ID 去重。
//
// 这用于替换下载 Manager 时合并内存中的任务，避免当前会话已经加入的任务被覆盖。
package download

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ShellMonster/PudMusic/internal/service/diskcache"
)

// ErrQueueFromNewer 是「这份队列来自更新的版本，一个字都不许写回去」。
//
// 单独拎出来而不是只用一句错字符串：调用方要**区别**「对方比我们新（该只读）」和
// 「文件坏了 / 磁盘满了（该报错）」——前者要告诉用户「你换回旧版了」，后者要告诉
// 用户「出了别的问题」。合成一句的话，导入那边只能把两者都报成「没能带过来」。
//
// 和 `internal/app` 的 `ErrSettingsFromNewer`、`internal/ui` 的
// `errShortcutsReadOnly` 是同一件事的三份，各自在自己的存储里。
var ErrQueueFromNewer = errors.New("download: 队列来自更新的版本，本次只读不写")

// MergeIntoQueue 把 extra 并进 path 这份队列文件，按 ID 去重。
//
// 现有队列排在前面，extra 里没见过的追加在后（extra 内部保持给进来的顺序）。
// 同一首两边都有时以现有队列为准。
func MergeIntoQueue(path string, extra []*Task) error {
	return mergeIntoQueue(path, extra, false)
}

// MergeIntoQueuePreferExtra 合并队列时让 extra 中的快照覆盖同 ID 的磁盘记录。
// 这是换挂当前 Manager 时使用的语义：当前内存状态比上次落盘的快照更新。
func MergeIntoQueuePreferExtra(path string, extra []*Task) error {
	return mergeIntoQueue(path, extra, true)
}

func mergeIntoQueue(path string, extra []*Task, extraWins bool) error {
	if len(extra) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("download: 建队列目录：%w", err)
	}

	tasks := []*Task{}
	if b, err := os.ReadFile(path); err == nil {
		f, err := decodeQueueFile(b)
		if err != nil {
			return fmt.Errorf("download: 队列文件读不懂（%s），没有动它：%w", filepath.Base(path), err)
		}
		if f.Version > storeVersion {
			return fmt.Errorf("%w：文件是第 %d 版的，我们只到第 %d 版，没有动它",
				ErrQueueFromNewer, f.Version, storeVersion)
		}
		if err := runQueueMigrations(&f, f.Version); err != nil {
			return fmt.Errorf("%w：%s 没有动它", err, filepath.Base(path))
		}
		tasks = f.Tasks

	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("download: 读队列：%w", err)
	}

	if extraWins {
		byID := make(map[string]*Task, len(tasks)+len(extra))
		order := make([]string, 0, len(tasks)+len(extra))
		for _, t := range tasks {
			if t == nil || !keepOnRestore(t) || byID[t.ID] != nil {
				continue
			}
			byID[t.ID] = t
			order = append(order, t.ID)
		}
		for _, t := range extra {
			if !keepOnRestore(t) {
				continue
			}
			if byID[t.ID] == nil {
				order = append(order, t.ID)
			}
			byID[t.ID] = t
		}
		tasks = tasks[:0]
		for _, id := range order {
			tasks = append(tasks, byID[id])
		}
	}
	if extraWins {
		// Already merged with extra snapshots above.
	} else {
		have := make(map[string]bool, len(tasks))
		for _, t := range tasks {
			if t != nil && keepOnRestore(t) {
				have[t.ID] = true
			}
		}
		for _, t := range extra {
			if !keepOnRestore(t) || have[t.ID] {
				continue
			}
			tasks = append(tasks, t)
			have[t.ID] = true
		}
	}

	b, err := json.MarshalIndent(queueFile{Version: storeVersion, Tasks: tasks}, "", "  ")
	if err != nil {
		return fmt.Errorf("download: 序列队列：%w", err)
	}
	return diskcache.WriteAtomic(path, b)
}
