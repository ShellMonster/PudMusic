// 启动时的数据升级。**必须是最先跑的那一件。**
//
// # 为什么在所有 Mount 之前
//
// 四份文件（设置、快捷键、搜索历史、下载队列）各自的挂载都要读盘。升级必须在
// 它们**之前**做完，有两个理由：
//
//   - **备份与回滚需要「原文件还在」**。升级是「备份 → 就地改 → 全成才删备份」；
//     而挂载读盘时可能顺手改写（搜索历史重写格式、队列把 running 改成 paused）。
//     先挂载再升级，备份里存的就是「已经被挂载动过的」内容，回滚回去的回滚不到
//     用户原本的样子。
//   - **只读判断要在迁移之后**。一份来自更新的版本的文件，挂载时判出只读、
//     一个字都不写；迁移如果在挂载之后跑，它就已经把它按旧语义改过了。
package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/service/download"
	"github.com/egoist/mygo"
)

// MigrationReport 是这一次启动做/没做什么。
//
// 界面用它展示升级结果，以及在升级失败时弹一条 toast。
type MigrationReport struct {
	// Upgraded 报这一轮升级做了什么。
	//
	// 三种取值：
	//
	//   - "" —— 没有要升的（数据已经是最新，或第一次运行）
	//   - "已把设置、快捷键、搜索历史、下载队列升到 v1"
	//   - 一句错（`UpgradeErr` 非 nil），末尾带「原来的数据一个字节都没少」
	Upgraded string
	// UpgradeErr 是升级的错。非 nil 时 `Upgraded` 里那句话就是它。
	UpgradeErr error
	// UpgradedFiles 是这一轮真的动了哪几份文件（按文件名）。
	//
	// 「真的动了」= 备份里那份和现在这份**内容不同**。一份 v1 的文件迁到 v1
	// （链是空的）不算动过——报成动过了，用户会以为应用改了他的数据。
	UpgradedFiles []string
	// Rollback 是「这一轮回滚了，原文件恢复了」。用户该知道数据没被改。
	Rollback bool
	// ReadOnly 是「这个数据目录来自更新的版本」——整个目录一个字节都不许写。
	ReadOnly bool
}

// migration 是最近一次的报告。进程里通常只有一份。
var migration struct {
	mu sync.Mutex
	r  MigrationReport
}

// MountMigration 跑一次数据升级。**必须在所有 Mount 之前调。**
//
// 返回的 error 不阻止程序起来：用户照常用这个应用，设置存不住的提示另行展示。
func MountMigration() error {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("biu: 拿不到应用数据目录，这次没法升级：%v", err)
		return err
	}
	return mountMigrationAt(dir)
}

// mountMigrationAt 是启动迁移的可测试接缝；它只处理本应用自己的数据目录。
func mountMigrationAt(dir string) error {
	var rep MigrationReport
	upgradeErr := runUpgrade(dir, &rep)
	rep.UpgradeErr = upgradeErr

	migration.mu.Lock()
	migration.r = rep
	migration.mu.Unlock()

	if upgradeErr != nil {
		return fmt.Errorf("biu: 升级数据失败，原文件都留着了：%w", upgradeErr)
	}
	return nil
}

// LastMigration 取最近一次的报告。零值可用（= 什么都没做）。
func LastMigration() MigrationReport {
	migration.mu.Lock()
	defer migration.mu.Unlock()
	return migration.r
}

// runUpgrade 跑一轮多文件一致性事务。
//
// 四份文件各自的迁移是**同一件事的四种形状**（`app.MigrateSettingsFile`、
// `MigrateShortcutsFile`、`MigrateSearchHistoryFile`、`download.MigrateFile`），
// 事务本身在 `app` 那边——它不认识任何一份文件的内容，只管备份谁、失败了怎么
// 恢复。
func runUpgrade(dir string, rep *MigrationReport) error {
	tx, err := app.BeginUpgrade(dir)
	if err != nil {
		// 「来自更新的版本」不是失败，是用户把应用换回了旧版。这种情况下**一
		// 个文件都不碰**：事务压根没开，`rep.Upgraded` 记成一句话让用户看见。
		//
		// 这一份来自更新版本时，整个目录保持只读；其它文件继续照常升级。

		if errors.Is(err, app.ErrUpgradeFromNewer) {
			rep.ReadOnly = true
			rep.Upgraded = "数据目录来自更新的版本，这一版一个字都不会写回去。"
			return nil
		}
		rep.Upgraded = err.Error()
		return err
	}
	files := []struct {
		path    string
		migrate func(string) error
	}{
		{app.SettingsPath(dir), app.MigrateSettingsFile},
		{shortcutPath(dir), MigrateShortcutsFile},
		{searchHistoryPath(dir), MigrateSearchHistoryFile},
		{filepath.Join(dir, download.QueueFileName), download.MigrateFile},
	}

	// 🔴 **总标记说「已经是最新版」不等于四份文件都已经是最新版。**
	//
	// 总标记（`data-version.json`）是**整票**的版本号，各文件自己的 schema 是**它
	// 自己**的版本号——两者会不同步：用户手改过文件、别的工具写过、或者一份文件从
	// 一个更新的版本回来而总标记没跟着改。
	//
	// 所以这里**逐份核对**，而不是看到标记是最新版就早退。早退的话：一个目录里
	// 总标记是 1 而 `settings.json` 写着 `schemaVersion: 99`，这一轮升级会直接返回，
	// 那一份永远是更新的那一版，而**没有任何一层告诉用户**——界面只看到「设置来自
	// 更新版本」这一句从别处冒出来，而升级这边什么都不知道。
	upToDate := tx.Version() >= app.CurrentDataVersion && !tx.Pending()

	var done, newer []string
	for _, f := range files {
		// 这一份是不是比当前版本还新。**在 Stage 之前问**：它来自更新版本的话我们
		// 一个字节都不该碰，备份也不必（`Stage` 只是复制，可它会把「待处置」这个
		// 状态写进标记，而这一份我们压根不打算处置）。
		if isFromNewerVersion(f) {
			newer = append(newer, filepath.Base(f.path))
			continue
		}
		if upToDate {
			continue // 齐了，这一份不用迁
		}

		before, _ := os.ReadFile(f.path)
		bk, err := tx.Stage(f.path)
		if err != nil {
			rep.Rollback = tx.Rollback() == nil
			rep.Upgraded = fmt.Sprintf("升级到 v%d 失败：%v", app.CurrentDataVersion, err)
			return err
		}
		if err := f.migrate(f.path); err != nil {
			// 事务回滚把**所有**已经改过的文件恢复回去——不只这一份。升到一半
			// 停在不同版本上的目录，比「一次都没升」更难解释。
			rep.Rollback = tx.Rollback() == nil
			rep.Upgraded = fmt.Sprintf("升级到 v%d 失败：%v", app.CurrentDataVersion, err)
			rep.Upgraded += "。原来的数据一个字节都没少。"
			log.Printf("biu: 升级数据失败，已全部回滚：%v", err)
			return err
		}
		// 内容没变的（链是空的）不算「动了」。
		after, _ := os.ReadFile(f.path)
		if string(before) != string(after) {
			done = append(done, filepath.Base(f.path))
		}
		_ = bk // 备份不在这里处置：整轮成功才统一挪进快照目录（见 tx.Commit）
	}

	// 有一份来自更新版本时：那一份只读，其余照迁。
	if len(newer) > 0 {
		rep.ReadOnly = true
		rep.Upgraded = "这几份来自更新的版本，这一版一个字都不会写回去：" +
			joinNames(newer)
		// 总标记不动：它说的是「整票迁到了哪」，而有一份我们没有迁（也不该迁）。
		// 把它推上去等于宣称「全部都是 v1」，下一次启动就再也不会问这件事了。
		return nil
	}
	if upToDate {
		rep.Upgraded = ""
		return nil
	}

	if err := tx.Commit(); err != nil {
		rep.Rollback = tx.Rollback() == nil
		rep.Upgraded = fmt.Sprintf("升级到 v%d 没能收尾：%v", app.CurrentDataVersion, err)
		return err
	}
	rep.UpgradedFiles = done
	rep.Upgraded = fmt.Sprintf("数据已升到 v%d", app.CurrentDataVersion)
	if len(done) > 0 {
		rep.Upgraded += "（动了：" + joinNames(done) + "）"
	}
	return nil
}

// joinNames 把文件名列表连成一句人话。
func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	out := ""
	for i, n := range names {
		if i > 0 {
			out += "、"
		}
		out += n
	}
	return out
}

// announceMigration 第一帧把启动升级失败提示弹成一条 toast。**只弹一次。**
//
// 挂在 `viewMain` 的第一帧而不是 `MountMigration` 里：后者跑在 `main` 里、界面
// 还没建，而 toast 是界面的一部分。
//
// 只弹「需要用户做事」的那一类：升级失败（「原来的数据一个字节都没少」）。
//
// 一次成功的、顺顺利利的升级不弹——用户没要求知道「数据已是最新」，弹一条
// 「已升到 v1」是在打扰他。
//
// ⚠️ 界面线程调（`viewMain` 里），`addToast` 自己有锁。
func (s *Shell) announceMigration() {
	if s.migrationAnnounced {
		return
	}
	s.migrationAnnounced = true

	rep := LastMigration()
	switch {
	case rep.UpgradeErr != nil:
		s.addToast("数据升级失败："+rep.Upgraded, notice.Danger)

	}
}

// upgradeFile 是一次升级要处理的那一份文件：路径 + 它的迁移入口。
type upgradeFile struct {
	path    string
	migrate func(string) error
}

// isFromNewerVersion 报这一份是不是来自一个**比当前还新**的版本。
//
// ⚠️ 判据是「这一份自己说的版本号」，不是总标记。用户手改过文件、别的工具写过、
// 或者一份文件从一个更新的版本回来而总标记没跟着改——这三种情况下总标记都说「齐了」，
// 而这一份确实比我们新。
//
// 只判「读得出版本号且比当前大」。读不出来（Biu 直接导出的裸设置、没有
// `schemaVersion` 键）不算「更新」——那是「还没有」，不是「比我们新」。
func isFromNewerVersion(f struct {
	path    string
	migrate func(string) error
}) bool {
	raw, err := os.ReadFile(f.path)
	if err != nil {
		return false // 不存在 / 读不出来：都不构成「来自更新版本」
	}
	var probe struct {
		SchemaVersion *int `json:"schemaVersion"`
		Version       *int `json:"version"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false // 不是 JSON：留给迁移那几条路去报错
	}
	// 设置用 `schemaVersion`，队列用 `version`（`download/store.go` 的 `queueFile`）。
	// 两个都认，因为这一层不认识任何一份文件的内容。
	v := -1
	if probe.SchemaVersion != nil {
		v = *probe.SchemaVersion
	} else if probe.Version != nil {
		v = *probe.Version
	}
	// 队列自己的上限是它包的 `storeVersion`；其余三份走本包的
	// `CurrentDataVersion`（它们的当前版本号都是 1，两边相等，见 `BeginUpgrade`）。
	return v > app.CurrentDataVersion
}
