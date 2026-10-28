// 从别的来源并进队列文件：读一份现有的，加上新的一批，写回去。
//
// # 为什么不复用 `Restore`
//
// `Restore` 是「接管整个队列」：队列里已经有东西就整个不读（`if len(m.order) > 0`
// 那一段）。那是**启动**要的语义——别把用户刚点的任务冲掉。
//
// 导入要的语义正相反：用户在 MyGo 里已经下过几首，这时导入另一份队列，
// 他要的是「Biu 那几十条也进来」，不是「Biu 那几十条把我在 MyGo 下的顶掉」。
// 所以这里是**按 ID 去重的并集**：两边都有的以哪边为准见下，现有队列的顺序在前。
//
// # 为什么自己读而不是拿一个 Manager 来加载
//
// 导入发生在**挂载之前**（队列还没接管，Manager 还不存在）。为一个「改一份
// JSON 文件」的动作造一个 Manager 出来，等于把下载引擎整套装载进来只为调用两次
// 方法——而它一装载就会开 goroutine、开队列循环。
package download

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
// 同一首两边都有时**以现有队列为准**：那是用户在这个应用里刚点的状态，
// 拿一份几个月前的外来记录覆盖它是倒退。
//
// path 上的文件不存在不是错误，返回 nil：第一次导入，目标目录里本来就只有
// Biu 带来的那些。文件在但读不懂、或者版本对不上，就**不碰它**、返回错——静默
// 清空一份下到一半的队列，是这一类工具最不该做的事（`Restore` 的文件头写着同一
// 句话）。
func MergeIntoQueue(path string, extra []*Task) error {
	if len(extra) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("download: 建队列目录：%w", err)
	}

	tasks := []*Task{}
	if b, err := os.ReadFile(path); err == nil {
		var f queueFile
		if err := json.Unmarshal(b, &f); err != nil {
			return fmt.Errorf("download: 队列文件读不懂（%s），没有动它：%w", filepath.Base(path), err)
		}
		if f.Version != storeVersion {
			return fmt.Errorf("download: 队列文件是第 %d 版的，我们只到第 %d 版，没有动它",
				f.Version, storeVersion)
		}
		tasks = f.Tasks
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("download: 读队列：%w", err)
	}

	have := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		if t != nil && keepOnRestore(t) {
			have[t.ID] = true
		}
	}
	for _, t := range extra {
		// 和 `Restore` 同一个门槛：认不出是哪一首、或者不知道往哪儿写的，
		// 捞回来也是界面上按不动的死行。
		if !keepOnRestore(t) || have[t.ID] {
			continue
		}
		tasks = append(tasks, t)
		have[t.ID] = true
	}

	b, err := json.MarshalIndent(queueFile{Version: storeVersion, Tasks: tasks}, "", "  ")
	if err != nil {
		return fmt.Errorf("download: 序列队列：%w", err)
	}
	return writeAtomic(path, b)
}
