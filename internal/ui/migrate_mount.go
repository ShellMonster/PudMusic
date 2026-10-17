// 启动时的数据升级，以及从 Biu 的首次导入。**必须是最先跑的那一件。**
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
//
// # 两件事，风险完全不同
//
// | | 从 Biu 首次导入 | MyGo N→N+1 升级 |
// |---|---|---|
// | 触发 | 这台机器上有 Biu 的数据，而**本应用一个数据文件都没有** | 总标记说数据还是旧版 |
// | 风险 | 低。只读别人的文件，写的是自己的目录 | 高。写坏的是用户自己的数据 |
// | 失败 | 逐项跳过并如实报告（`app.Imported.Notes`） | 整轮回滚，**原文件一个字节不少**，并报错 |
//
// 详见 `internal/app/upgrade.go`（事务那一半）和 `internal/app/biuimport.go`
// （导入那一半）的文件头。
//
// # 凭据不迁
//
// Biu 的 `user-login-info.json` 是加密的（conf 的 `encryptionKey`），我们不搬，
// 而是在报告里说一句「请重新登录」。见 `app.BiuScan.LoggedIn`。
package ui

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
	"github.com/egoist/mygo"
)

// MigrationReport 是这一次启动做了/没做什么。
//
// 界面拿它做两件事：设置页上把「导了些什么」如实摆出来，以及首帧弹一条 toast。
// **宁可不弹，也不要说错**——「已导入 0 条」和「没导入过」在用户看来是一回事，
// 而前者是错的。
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

	// Imported 是首次导入的结果。零值表示这次没导入过（已经导过了，或者这台
	// 机器上没有 Biu）。
	Imported app.Imported
	// BiuDirs 是这台机器上还找得到的 Biu 数据目录，设置页的「从 Biu 导入」用。
	BiuDirs []string
}

// migration 是最近一次的报告。进程里通常只有一份。
var migration struct {
	mu sync.Mutex
	r  MigrationReport
}

// MountMigration 跑一次数据升级与首次导入。**必须在所有 Mount 之前调。**
//
// 返回的 error 是「有东西没办成」——升级失败、导入有跳过的项。它**不阻止程序
// 起���**：拿不到目录就什么都不做，用户照常用这个应用（设置存不住这件事界面上会
// 另有提示，见 `reportSettingsError`）。
func MountMigration() error {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("biu: 拿不到应用数据目录，这次没法升级数据、也没法导入：%v", err)
		return err
	}

	var rep MigrationReport
	upgradeErr := runUpgrade(dir, &rep)
	rep.UpgradeErr = upgradeErr
	rep.BiuDirs = app.FindBiuDirs()
	rep.Imported = firstBiuImport(dir, rep.BiuDirs, upgradeErr)

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
		if errors.Is(err, app.ErrUpgradeFromNewer) {
			rep.Upgraded = "数据目录来自更新的版本，这一版一个字都不会写回去。"
			return nil
		}
		rep.Upgraded = err.Error()
		return err
	}
	if tx.Version() >= app.CurrentDataVersion && !tx.Pending() {
		rep.Upgraded = ""
		return nil
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

	var done []string
	for _, f := range files {
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

// firstBiuImport 第一次启动且本应用还没有任何数据时，自动从 Biu 导入一次。
//
// **只在「本应用一个数据文件都没有」时自动跑**。判据是「总标记、设置、快捷键、
// 搜索历史、下载队列，一个都不在」——用户已经用过一阵子了，就不���抢他的数据；
// 他要的话设置页上还有「从 Biu 导入」那个按钮。
//
// 升级失败时**不导入**：那时的目录可能正处于「回滚了但标记说不清」的状态，
// 在上面写东西是给一个说不清的状态再加一层说不清。
func firstBiuImport(dir string, dirs []string, upgradeErr error) app.Imported {
	if upgradeErr != nil {
		return app.Imported{}
	}
	if hasAnyOwnFile(dir) {
		return app.Imported{}
	}
	for _, d := range dirs {
		if r, err := importFromBiu(d, dir); err == nil && r.Any() {
			return r
		}
	}
	return app.Imported{}
}

// hasAnyOwnFile 报本应用的数据目录里是不是已经有我们自己的文件。
func hasAnyOwnFile(dir string) bool {
	for _, n := range []string{
		app.UpgradeFileName, app.SettingsFileName, shortcutFileName,
		searchHistoryFileName, download.QueueFileName,
	} {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			return true
		}
	}
	return false
}

// importFromBiu 从一个 Biu 目录导入，设置 / 下载记录 / 歌词缓存在 `app` 那边写，
// 快捷键在这一层写（那份存储的代码在这一层，见 `app.BiuScan.Shortcuts`）。
func importFromBiu(biuDir, dir string) (app.Imported, error) {
	scan, err := app.ScanBiu(biuDir)
	if err != nil || scan == nil {
		return app.Imported{}, err
	}
	r, importErr := scan.Import(dir)

	if scan.HasShortcuts {
		if err := importShortcuts(scan.Shortcuts, shortcutPath(dir)); err != nil {
			r.Notes = append(r.Notes, fmt.Sprintf("快捷键没能带过来：%v", err))
			return r, err
		}
		r.Shortcuts = true
	}
	return r, importErr
}

// importShortcuts 把 Biu 的快捷键文件写进我们自己的那份。
//
// 目标文件已经存在就先挪成 `.bak`：用户可能已经在这个应用里改过快捷键了。
func importShortcuts(raw []byte, path string) error {
	_, s, ok, err := probeShortcuts(raw)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("认不出是快捷键文件（顶层没有 shortcuts）")
	}
	from, err := probeSchemaVersion(raw)
	if err != nil {
		return err
	}
	if from > shortcutSchema {
		return fmt.Errorf("Biu 那份是 v%d 的，这一版只到 v%d", from, shortcutSchema)
	}
	if err := runShortcutMigrations(&s, from); err != nil {
		return err
	}
	// 补上文件里没有的那几条命令：加了新命令之后老用户的文件里没有它，
	// 不补的话设置页上那一条就整个消失（`mergeMissing`）。
	s = s.mergeMissing(defaultShortcutSettings())

	if _, err := os.Stat(path); err == nil {
		backupShortcuts(path)
	}
	b, err := marshalShortcutFile(s)
	if err != nil {
		return err
	}
	return writeShortcutsAtomic(path, b)
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
