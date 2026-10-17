// 队列落盘：退出的时候把队列写下来，重开的时候读回去。
//
// # 为什么是原子写
//
// 队列文件是**唯一**一份「下到哪儿了」之外的东西——分块记账在里面。
// 写到一半断电的话，JSON 读不出来 → 整个队列丢 → 用户重启看到的是一个
// 空队列，而盘上还躺着一堆 `.part`。所以走「临时文件 → fsync → rename」，
// 跟设置那一份是同一套（`app/settings.go` 的 `writeSettingsAtomic`）。
//
// # 退出时把「正在下」改成「暂停」
//
// 跟 B 站上游一样（`electron/ipc/download/download-queue.ts:254`）：
// `downloading` / `waiting` 存成 `downloadPaused`。存成 running 没有意义
// ——重启之后**根本没有**那个在飞的 HTTP 请求，界面上会永远转圈。
//
// # 恢复时**不自动开下**
//
// B 站也是不自动续（`restoreQueue` 只把任务放回 map 里，用户得按「继续」）。
// 这一条在这里尤其重要：应用启动时网络可能还没起来、用户刚坐下、机器可能
// 根本没有网。一开窗就发几十个请求，表现为「一启动就卡」和一堆红字。
// 自动续是界面层的决定，不是这一层的行为。

package download

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// QueueFileName 是队列文件名。
const QueueFileName = "download-queue.json"

// QueueVersion 是队列文件的版本。
//
// 不是凑数的字段：以后换了记账方式（比如改分块大小），旧文件里的分块
// 下标就对不上新的分块划分了。认版本是为了**拒绝**一份读不懂的记录，
// 而不是硬读出一个「下到一半」的假象。
//
// 导出的理由和 `QueueFileName` 一样：这份文件的形状**就是**这一版的对外约定
// （用户自己会打开它看，别的层要写它、备份它、迁移它），「它第几版」是外面
// 的代码要问的第一个问题。
const QueueVersion = 1

// storeVersion 是这一版读的那一版。写代码时用 `QueueVersion`，这里留个别名是
// 为了让读代码的人一眼看出「读的那一版」和「对外说的那一版」是同一个数。
const storeVersion = QueueVersion

// queueFile 是落盘的那份 JSON。
type queueFile struct {
	Version int     `json:"version"`
	Tasks   []*Task `json:"tasks"`
}

// Save 把队列写到 path。**必须在 Manager 的锁外面调**（它自己会拿）。
//
// 写失败不 panic、不重试：磁盘满、目录被删了这类问题重试也是白转。
// 错误交回调用方，由它记日志——而队列在内存里还在，下次退出还能再试一次。
func (m *Manager) Save(path string) error {
	tasks := m.persistable()

	b, err := json.MarshalIndent(queueFile{Version: storeVersion, Tasks: tasks}, "", "  ")
	if err != nil {
		return fmt.Errorf("download: 序列队列：%w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("download: 建队列目录：%w", err)
	}
	return writeAtomic(path, b)
}

// Persistable 拷一份能落盘的任务列表，把在飞的改成暂停。
//
// 导出的（`persistable` 私有）是同一件事，供**外部**把内存里这一份并进队列文件：
// 队列文件会话期间只在退出时写一次，所以任何一次「换掉这个 Manager」的动作
// 都必须先把内存里的任务落到盘上——只 `Close("")` 的话，本次会话刚加进来的歌
// 会随旧 Manager 一起没了，而退出时新 Manager 落的是它自己那份（从陈旧文件
// Restore 来的），**用户点的歌从队列文件里被抹掉**。见 `app.MountDownloads`。
func (m *Manager) Persistable() []*Task { return m.persistable() }

// persistable 拷一份能落盘的任务列表，把在飞的改成暂停。
func (m *Manager) persistable() []*Task {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]*Task, 0, len(m.order))
	for _, id := range m.order {
		t := m.tasks[id]
		if t == nil {
			continue
		}
		cp := *t
		if cp.Status.inflight() {
			cp.Status = StatusPaused
		}
		out = append(out, &cp)
	}
	return out
}

// Restore 从 path 读回队列。**必须在 Manager 的锁外面调**。
//
// 读不出队列**不是错误**：第一次运行时那个文件本来就不存在。所以这里
// 返回 nil、队列空着。只有「文件在、但内容读不懂」才当错——那种情况
// 用户会以为自己的下载记录弄丢了，得说一句话。
func (m *Manager) Restore(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 第一次运行，没有队列
		}
		return fmt.Errorf("download: 读队列：%w", err)
	}
	var f queueFile
	if err := json.Unmarshal(b, &f); err != nil {
		return fmt.Errorf("download: 队列文件读不懂（%s）：%w", filepath.Base(path), err)
	}
	if f.Version > storeVersion {
		// 更高的版本我们读不懂，**整份不碰**。往一份我们没有能力重写的文件上
		// 动手，比什么都不做糟得多。
		return fmt.Errorf("download: 队列文件是第 %d 版的，这一版认第 %d 版", f.Version, storeVersion)
	}
	// 更低（或没有 version 键）的走迁移链，见 `migrate.go` 的 queueMigrations。
	// ⚠️ 这一条不能省：一份 v0 文件曾在这里被当成「读不懂」，整个队列丢，
	// 而用户重启看到的是空队列、盘上却躺着一堆 `.part`。
	if err := runQueueMigrations(&f, f.Version); err != nil {
		return fmt.Errorf("%w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.order) > 0 {
		// 已经往队列里放过东西了（比如界面一上来就恢复了两次）：
		// 以第一次的为准，别把用户刚点的任务冲掉。
		return nil
	}
	for _, t := range f.Tasks {
		if !keepOnRestore(t) {
			continue
		}
		// ⚠️ **只把「在飞的」改成暂停**，不是一律改。
		//
		// `running` / `queued`：重启之后那个在飞的请求早就不在了，记成
		// running 会让界面上永远转圈。交织中（`muxing`）同理——那个交织器
		// 也随进程没了，而交织的进度这一版根本存不下来（`Snapshot.Percent`
		// 是字节进度），记成交织中就是**永远转圈且永远停在同一个数**。
		//
		// `done`：歌已经在盘上了。改成暂停的话，记录页上每一行都写着
		//「暂停」，而那一行上唯一能按的「继续」会把**已经下完的整首歌**
		//重新下一遍（`prepare` 看到 `.part` 已经不在了 → `resetChunks`）。
		// 下完了就是下完了，它不需要「继续」。
		//
		// `paused` / `error`：本来就不是在飞的，原样带回来。
		if t.Status.inflight() {
			t.Status = StatusPaused
		}
		t.Err = ""
		if m.tasks == nil {
			m.tasks = map[string]*Task{}
		}
		m.tasks[t.ID] = t
		m.order = append(m.order, t.ID)
	}
	return nil
}

// keepOnRestore 判一条记录要不要捡回来。
//
// 四种要扔的：
//
//	ID 为空      认不出是哪一首，而它是队列里**唯一**的身份
//	Dest 为空    不知道往哪儿下，而续传要往盘上写
//	分块下标越界 版本对得上但记账是坏的（见 `Task.Done`）
//	有伴流却没有伴流的长度 / 伴流的下标越界（见下面的注释）
func keepOnRestore(t *Task) bool {
	if t == nil || t.ID == "" || t.Dest == "" {
		return false
	}
	if !chunksInRange(t.Identity.Length, t.Chunks) {
		return false
	}
	// 视频任务：伴流也得是条**完整**的记录。
	//
	// 「有伴流但长度是 0」意味着「这一项知道自己是视频，却不知道音频
	// 有多长」——交织那一步一定会因为拿不到音频流而报错，而用户在
	// 界面上看到的是一个下到一半的视频任务和一句没头没尾的话。宁可
	// 整项丢掉：视频会重新下一遍，而丢一个任务用户看得见。
	if t.Companion != nil && !chunksInRange(t.Companion.Length, t.Companion.Chunks) {
		return false
	}
	return true
}

// chunksInRange 报记账里有没有越界的分块下标。
func chunksInRange(length int64, chunks []int) bool {
	if length <= 0 {
		return true
	}
	n := totalChunks(length)
	for _, i := range chunks {
		if i < 0 || i >= n {
			return false
		}
	}
	return true
}

// writeAtomic 原子写：临时文件 → fsync → rename。
func writeAtomic(path string, b []byte) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".download-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(name)
		}
	}()

	if _, err = tmp.Write(b); err != nil {
		return err
	}
	// fsync 的是数据。数据落盘了但目录项没有的话，断电之后可能还是旧文件。
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	// CreateTemp 建出来的是 0600。这份文件不是秘密（里面没有凭据），
	// 但它和设置放同一个目录，给成普通用户配置同样的权限。
	if err = os.Chmod(name, 0o644); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	return syncDir(dir)
}

// syncDir 把目录项本身刷到盘上。
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
