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
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
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
	// ReadOnly 是「这个数据目录来自更新的版本」——**整个目录一个字节都不许写**。
	//
	// ⚠️ 这不只是升级那一半的事：**导入是另一条写这个目录的路**，而
	// `firstBiuImport` 的守门只有一个「目录里有没有我们自己的数据」。一个只装得下
	// `data-version.json` 的目录对那个守门是空的，于是用户换回旧版之后一启动，
	// Biu 的数据就被灌进了那个「一个字都不写回去」的目录。
	//
	// 所以这个判决从 `runUpgrade` 一路传到导入两条路（自动 / 设置页上的按钮），
	// 三处都先问它。
	ReadOnly bool

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
	rep.Imported = firstBiuImport(dir, rep.BiuDirs, upgradeErr, rep.ReadOnly)

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
		// ⚠️ `rep.ReadOnly = true` 是给**导入那两条路**看的。不能只把这句话写给
		// 用户看——「一个字都不写回去」这个承诺，导入那条路也得兑现。
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

	// 有一份来自更新版本：**那一份**只读，**其余照迁**。
	//
	// 不用「一份只读所以整轮都不迁」——那会让一份无关的文件（比如下载队列是未来的
	// 版本）把设置、快捷键、搜索历史三份**本来能升的**一起卡住，用户每次启动都看到
	// 「升级失败」。这里只跳过它。
	//
	// ⚠️ `rep.ReadOnly = true` 仍然要设：那一份一个字都不能写，而**导入那两条路**
	// （自动 / 设置页的按钮）也得兑现同一个承诺。
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

// firstBiuImport 第一次启动且本应用还没有任何数据时，自动从 Biu 导入一次。
//
// 三道门，每一道都拦得住「往一个不该写的数据目录里写」：
//
//  1. **目录来自更新的版本**（`readOnly`）——用户把应用换回了旧版。整个目录一个
//     字节都不该动，写进去的 v1 设置会毁掉他升级之后的设置。升级那一半已经判出
//     这一条（`runUpgrade` 的 `ErrUpgradeFromNewer`），**那个判决必须传到这里**：
//     它不传的话，守门只剩 `hasAnyOwnFile`，而一个只装得下 `data-version.json`
//     的目录对它是空的，于是导入照跑不误。
//  2. **升级失败**——那时的目录可能正处于「回滚了但标记说不清」的状态，在上面写
//     东西是给一个说不清的状态再加一层说不清。
//  3. **本应用已经有自己的数据**——用户已经用过一阵子了，就不要抢他的数据；他要
//     的话设置页上还有「从 Biu 导入」那个按钮。
//
// **找到第一个有东西的就停**，不往下试下一个目录。哪怕这一份是**部分**成功的
// （设置进来了、快捷键那份认不出形状），成果连同报告一起带回去——拿第二个目录
// 接着导，是拿一份正式版的数据去盖掉刚从正式版导进来的设置。
func firstBiuImport(dir string, dirs []string, upgradeErr error, readOnly bool) app.Imported {
	if readOnly || upgradeErr != nil {
		return app.Imported{}
	}
	if hasAnyOwnFile(dir) {
		return app.Imported{}
	}
	for _, d := range dirs {
		// **不看 err**：err 只是「这一项没带过来」，而已经带过来的那几样是要
		// 留下的。拿 err==nil 当判据的话，部分成功会被当成完全失败丢掉，然后
		// 循环继续——那是本函数原来最大的一个洞。
		r, _ := importFromBiu(d, dir)
		if r.Any() {
			return r
		}
	}
	return app.Imported{}
}

// hasAnyOwnFile 报本应用的数据目录里是不是已经有**我们自己的数据**。
//
// ⚠️ **不算总控标记**（`data-version.json`）。`MountMigration` 是先升级、后判断要不要
// 导入的，而第一次运行时那一轮升级会**写下标记**（哪怕四份文件一个都不存在）——
// 把它算成「已有数据」的话，自动导入在第一次运行时永远被跳过，而那恰恰是它唯一
// 该跑的场景。
//
// 判据是那四份真实数据：设置、快捷键、搜索历史、下载队列。用户动过任何一样，
// 就说明他已经用了一阵子了。
func hasAnyOwnFile(dir string) bool {
	for _, n := range []string{
		app.SettingsFileName, shortcutFileName,
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
//
// 返回的 `Imported` **总是可用**：即使每一项都失败，它也有 `Dir` 和 `Notes`
// （「没带过来的原因」）。err 只在「一个字节都没写下去」时非空——
// `firstBiuImport` 用 `r.Any()` 而不是 err 判成功，正是靠这一点。
func importFromBiu(biuDir, dir string) (app.Imported, error) {
	scan, err := app.ScanBiu(biuDir)
	if err != nil {
		return app.Imported{}, err
	}
	if scan == nil {
		return app.Imported{}, nil
	}
	r, importErr := scan.Import(dir)

	if scan.HasShortcuts {
		if err := importShortcuts(scan.Shortcuts, shortcutPath(dir)); err != nil {
			if errors.Is(err, errShortcutsFromNewer) {
				r.Notes = append(r.Notes,
					"你现在的快捷键来自更新的版本，这一版一个字都不会写回去，所以快捷键没有带过来。")
			} else {
				r.Notes = append(r.Notes, fmt.Sprintf("快捷键没能带过来：%v", err))
			}
		} else {
			r.Shortcuts = true
		}
	}
	return r, importErr
}

// importShortcuts 把 Biu 的快捷键文件写进我们自己的那份。
//
// **目标那一份来自更新的版本时返回 `errShortcutsFromNewer` 并且不碰它**——理由和
// 设置那边一样（`app.importSettings`）：那个目录是「用户换回了旧版」的证据，
// 往里面写一份当前版本的快捷键会毁掉他升级之后的东西。
//
// 目标文件存在且是当前版本时先挪成 `.bak`：用户可能已经在这个应用里改过快捷键了。
func importShortcuts(raw []byte, path string) error {
	if from, err := probeSchemaVersionAt(path); err == nil && from > shortcutSchema {
		return fmt.Errorf("%w：%s 是 v%d 的，这一版只到 v%d",
			errShortcutsFromNewer, filepath.Base(path), from, shortcutSchema)
	}

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

// announceMigration 第一帧把「启动时做了什么」弹成一条 toast。**只弹一次。**
//
// 挂在 `viewMain` 的第一帧而不是 `MountMigration` 里：后者跑在 `main` 里、界面
// 还没建，而 toast 是界面的一部分。
//
// 只弹「**需要用户做事**」的那一类：
//
//   - 升级失败（「原来的数据一个字节都没少」）
//   - 自动导入带过来的东西里有需要他去处理的（重新登录、什么也没带过来）
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
	case rep.Imported.Any(), len(rep.Imported.Notes) > 0:
		// 🔴 **这里只弹最短的那一句，绝不弹完整报告。**
		//
		// toast 在右下角、抬过播放条 `playbarClearance`（90px），而它的高度随文字
		// 增长——一句话几十个字就会换行三四行，向上**盖住页面内容区**。实测一条完整
		// 的导入报告（`biuImportToast` 的那一整串）能把内容区压掉一多半，界面上
		// 变成「上面是半条消息、下面是残缺的页面」，而它在两秒后就消失，用户根本
		// 看不到发生了什么。
		//
		// 所以：首帧只说「有事」，**说什么、跳过了什么**一律留给设置页「配置」那一
		// 段——那里逐条列着，位置固定、不会被盖住、用户想看多久看多久。
		//
		// 「请重新登录」这一句仍然必须出现在首帧：用户不会主动去设置页。
		s.addToast(shortImportToast(rep.Imported), importTone(rep.Imported))
	}
}

// importTone 是「从 Biu 导入」那一句话的语气。
//
// ⚠️ **有要用户动手的事就是橙的**：导入成了（那是绿的），但「Biu 的登录信息
// 不迁，请在这个应用里重新登录一次」——那一件要用户**现在**就动手。给它一个
// 绿色，用户会以为这一页不用管了。
//
// 什么都没带过来也是橙的：用户按了导入，什么都没发生。
func importTone(r app.Imported) notice.Tone {
	if !r.Any() || len(r.Notes) > 0 {
		return notice.Warning
	}
	return notice.Success
}

// shortImportToast 是首帧弹的那一句：**短**，最多一行。
//
// 「短」是硬要求而不是风格偏好——见 `announceMigration` 的注释：toast 一旦换行就
// 会盖住页面内容区。完整清单在设置页上，不在这里。
func shortImportToast(r app.Imported) string {
	if !r.Any() {
		return "没从 Biu 那里带过来任何东西"
	}
	// 「要重新登录」是唯一一件用户**现在就得动手**的事，优先说它。
	for _, n := range r.Notes {
		if strings.Contains(n, "重新登录") {
			return "从 Biu 导入了一批数据；Biu 的登录信息不迁，请在这个应用里重新登录一次（设置页「配置」那一段里有完整清单）"
		}
	}
	return "从 Biu 导入了一批数据（设置页「配置」那一段里有完整清单）"
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
