// 启动时的数据升级：schema 号 + 迁移链 + 迁移前备份 + 多文件一致性 + 降级快照。
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
// # 四件原子写管不了的事
//
// 临时文件 + rename 只保证「写到一半被断电」不会留下半个文件。它不管 schema
// 版本、迁移失败怎么办、版本回退、多文件一致性。这四件在 `internal/app/upgrade.go`
// （事务那一半）与各份文件自己的迁移链里，见那两个文件的头。
//
// # 这一票的范围
//
// 这里只有「MyGo N→N+1 升级」。**从 Biu 首次导入**是同一件工作的另一半
// （`Biu 那边迁过来的数据也得落到这一套 schema 上」），它在另一张票里，
// 接缝就是这里的 `MountMigration`——它跑完升级之后要不要接着导入，由那边决定。
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

// MigrationReport 是这一次启动升级做了什么。
//
// 界面拿它做两件事：设置页上把「升了什么」如实摆出来，以及首帧弹一条 toast。
// **宁可不弹，也不要说错**——「已升到 v1」和「没升过」在用户看来是一回事，
// 而前者是错的。
type MigrationReport struct {
	// Upgraded 报这一轮升级做了什么。
	//
	// 三种取值：
	//
	//   - "" —— 没有要升的（数据已经是最新，或第一次运行）
	//   - "数据已升到 v1（动了：settings.json、shortcuts.json）"
	//   - 一句错（`UpgradeErr` 非 nil），末尾带「原来的数据一个字节都没少」
	Upgraded string
	// UpgradeErr 是升级的错。非 nil 时 `Upgraded` 里那句话就是它。
	UpgradeErr error
	// UpgradedFiles 是这一轮真的动了哪几份文件（按文件名）。
	//
	// 「真的动了」= 备份里那份和现在那份**内容不同**。一份 v1 的文件迁到 v1
	// （链是空的）不算动过——报成动过了，用户会以为应用改了他的数据。
	UpgradedFiles []string
	// Rollback 是「这一轮回滚了，原文件恢复了」。用户该知道数据没被改。
	Rollback bool
	// ReadOnly 是「这个数据目录来自更新的版本」——**整个目录一个字节都不许写**。
	//
	// ⚠️ 这不只是升级这一半的事：**从 Biu 导入是另一条写这个目录的路**，而它的
	// 守门只有一个「目录里有没有我们自己的数据」。一个只装得下 `data-version.json`
	// 的目录对那个守门是空的，于是用户换回旧版之后一启动，Biu 的数据就被灌进了
	// 那个「一个字都不写回去」的目录。所以这个判决必须交给调用方。
	ReadOnly bool
}

// migration 是最近一次的报告。进程里通常只有一份。
var migration struct {
	mu sync.Mutex
	r  MigrationReport
}

// MountMigration 跑一次数据升级。**必须在所有 Mount 之前调。**
//
// 返回的 error 是「有东西没办成」。它**不阻止程序起来**：拿不到目录就什么都不做，
// 用户照常用这个应用（设置存不住这件事界面上会另有提示，见 `reportSettingsError`）。
func MountMigration() error {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("biu: 拿不到应用数据目录，这次没法升级数据：%v", err)
		return err
	}

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
		// ⚠️ `rep.ReadOnly = true` 是给**别的写这个目录的路**看的（从 Biu 导入）。
		// 不能只把这句话写给用户看——「一个字都不写回去」这个承诺，那些路也得兑现。
		if errors.Is(err, app.ErrUpgradeFromNewer) {
			rep.ReadOnly = true
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

// announceMigration 第一帧把「升级做了什么」弹成一条 toast。**只弹一次。**
//
// 挂在 `viewMain` 的第一帧而不是 `MountMigration` 里：后者跑在 `main` 里、界面
// 还没建，而 toast 是界面的一部分。
//
// 只弹**用户需要知道**的那两类：升级失败（「原来的数据一个字节都没少」），以及
// 数据目录来自更新的版本（这一版不会写它）。一次顺顺利利的升级**不弹**——用户
// 没要求知道「数据已是最新」，弹一条「已升到 v1」是在打扰他，而真出事时那条提示
// 混在一堆无害提示里就不显眼了。
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
		s.addToast("数据升级失败：" + rep.Upgraded)
	case rep.ReadOnly:
		s.addToast("数据目录来自更新的版本，这一版一个字都不会写回去")
	}
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
