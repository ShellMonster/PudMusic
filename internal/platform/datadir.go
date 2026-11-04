// 应用数据目录：从 `Biu MyGo` **自动迁移**到 `PudMusic`。
//
// ## 这件事为什么存在
//
// 应用 2026-10 从 `Biu MyGo` 改名成 `BiuGo`，后来又定名「布丁音乐」
// （`PudMusic`）。而 MyGo 的 `PathUserData` 是 `os.UserConfigDir()`
// **拼上应用名**（`mygo@v0.6.3/paths.go:90-93`）：
//
//	case PathUserData:
//	    dir, err := os.UserConfigDir()
//	    return filepath.Join(dir, App.Name()), true, err
//
// 所以数据目录的名字从来就不跟着显示名走——它由这个文件钉住。之前的选择是
// 「钉死在 `Biu MyGo` 上、不迁移」：项目没发过版，老目录只装在维护者本机和
// 少数内测机上，而搬目录的失败模式（搬一半、搬丢了）是静默的。代价是 Finder
// 里那个目录和应用名从此对不上，用户翻自己的数据时找不到「布丁音乐」。
//
// 现在名字定下来了，**值得把这个不一致一次性收掉**：让老目录跟着搬到
// `PudMusic`，新装的也直接落在 `PudMusic`。设置、搜索历史、快捷键、歌词缓存、
// 下载记录、界面状态、单实例锁和登录态（`credentials/session.json`）全在那个
// 目录里——**整体 `os.Rename`，一个字节都不挑**，搬过去的东西和用户离开它时
// 一模一样。
//
// ## 怎么迁
//
// 每次启动时按三个分支走一遍（顺序不能乱）：
//
//  1. **旧目录在、新目录不在** → `os.Rename` 把旧的整个搬过去，然后钉到新
//     路径。`os.Rename` 是**同分区原子**的：要么整个过去、要么原地不动，
//     不存在「搬了一半」的中间态——这正是当初「什么时候该搬」那一条里定下的
//     核心思路。
//  2. **新旧都在** → 不动旧目录，直接用新目录。这可能是「迁过一次之后又有
//     东西写回了旧目录」，也可能是用户手动放的——无论哪种，**旧的那份留着**，
//     不替用户做主去删或合并。
//  3. **旧的不在** → 全新安装，直接用新目录。
//
// ## 失败不静默
//
// `os.Rename` 会败的那几种（跨设备、权限不够、目录被占）落到用户身上绝不能是
// 「设置安静地回到出厂」——所以 rename 败了**退回旧目录**：照样钉、照样开，
// 但 `PinUserDataDir` 把错误**返回**给 bootstrap 记日志（「钉不住」那条），
// 而不是吞掉。下回启动旧目录还在、新目录还没被建出来，自然再试一次。
//
// 那个 bug——「迁失败之后用空目录开起来，用户以为数据丢了」——已经在
// `internal/app` 里犯过一次（`migrate.go`），别再犯第二次。
package platform

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// LegacyUserDataDirName 是应用数据目录的名字。
//
// 显示名是「布丁音乐」（`mygo.json` 的 `name`），目录名跟着它收成了
// `PudMusic`——理由见本文件顶上那一节：名字定下来之后，值得让 Finder 里
// 那个目录和应用对得上。
//
// ⚠️ 它是老用户数据的**落点**：改名必须走 `PinUserDataDir` 里那条
// 「旧目录 `os.Rename` 过来」的路，不能只改这个常量——那一下的代价是全体
// 老用户的设置、搜索历史、快捷键、歌词缓存、下载记录和登录态一起「消失」，
// 而症状是静默的：没有报错，只是界面回到出厂设置。
//
// 登录态也落在这个目录里（`credentials/session.json`，见
// `internal/app/credentials_file_darwin.go`），目录一换、登录态跟着「消失」
// ——症状同样是静默的：老用户升级之后掉登录。
//
// （旧版把登录态放在 macOS 钥匙串里，服务名 `com.biu-go.biu-mygo.login`。
// 那个服务名现在只用于**一次性迁移**，但同样不能改——改了迁移就读不到旧的那份，
// 表现还是「升级之后掉登录」。）
const LegacyUserDataDirName = "PudMusic"

// oldUserDataDirName 是迁移前的那个目录名。
//
// 只在这一个地方出现：`PinUserDataDir` 拿它去找老用户留下的目录。找得到且
// 新目录还没在，就把旧的整个 `os.Rename` 到 `LegacyUserDataDirName`——
// 它是**迁移的源头**，不是「还可以选的一个名字」。
const oldUserDataDirName = "Biu MyGo"

// renameFunc 是迁移用的那一下 rename。单测里换掉它来模拟「rename 必败」
// （跨设备、权限不够、目录被占）——那种坏法没法用临时目录稳定地造出来。
var renameFunc = os.Rename

// PinUserDataDir 把应用数据目录钉在 `LegacyUserDataDirName` 上（见本文件顶上）。
//
// 必须在**任何一次 `mygo.App.Path(mygo.PathUserData)` 之前**调，而且要在
// `mygo.App.Run()` 之前——单实例锁与退出回调同理。调用点是
// `bootstrap.Assemble` 的第一阶段，它排在 `app.New()` 与所有 `Mount*` 之前：数据升级
// （`ui.MountMigration`）和读设置（`ui.MountSettings`）都要读盘，排在它们后面就已经晚了一步，
// 而**晚一步的症状正是「按第一次运行处理」**——读不到设置就安静地用默认值开起来，用户只
// 发现「设置一直存不住」，换台机器再丢一次。
//
// 拿不到用户配置目录时返回错误，让调用方记一条日志继续走：那时候框架自己会
// 用默认值（应用名拼出来的那个新目录），是个能用的退路，值得带着它开起来。
//
// ⚠️ **已经有人钉过就别再钉**：调用方可以先用 `mygo.App.SetPath` 钉一个别的
// 目录，而这一句会**无条件覆盖**它。
//
// 那是谁在钉？**E2E 那一轮**（`automation_e2e.go`）——它把数据目录挪到临时目录，
// 好让 E2E 起的那一份**不和用户正在跑的那个应用抢单实例锁**
// （那把锁在数据目录里，见 `internal/platform/single_instance.go` 的
// `lockPath()`）。
//
// ⇒ 而「抢不到锁」的 symptoms 极怪：automation 的 socket **文件在**（插件的
// `init` 建了它），但**没人听**——连上去是「connection refused」，而应用那边
// **一个字都没说**（它把命令行递过去就正常退出了，退出码 0）。
func PinUserDataDir() error {
	// ⚠️ **先问一次「是不是已经钉过了」**：`mygo.App.Path` 在有覆盖值时直接
	// 返回它（`mygo/paths.go:53-59`），而没有覆盖值时它会返回框架默认的
	// 那个（应用名拼出来的）——**那不是「钉过了」**。
	//
	// ⇒ 所以判据是「框架默认那个」和「已经钉过的那个」不同。而框架默认那个
	// 是 `os.UserConfigDir()` 拼上应用名（`mygo/paths.go:90`）——**不是**
	// `LegacyUserDataDirName`（那是这个项目**故意**用的另一个名字，见本文件
	// 顶上）。⇒ 两者的差别正好就是「有没有被钉过」。
	if dir, err := mygo.App.Path(mygo.PathUserData); err == nil && filepath.Base(dir) != mygo.App.Name() {
		// 已经被钉过了（目录名的最后一段不是应用名）：尊重它，别覆盖。
		return nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("拿不到用户配置目录：%w", err)
	}
	dir := filepath.Join(base, LegacyUserDataDirName)
	legacy := filepath.Join(base, oldUserDataDirName)

	// 旧目录在、新目录不在 → 把旧的整个 rename 过来。`os.Rename` 是同分区
	// 原子的，要么整个过去、要么原地不动，没有「搬一半」的中间态。
	// 新旧都在时不动旧的：可能是迁过一次之后又有东西写回去，也可能是用户
	// 自己放的——旧的那份留着，不替用户做主。
	if _, err := os.Stat(legacy); err == nil {
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			if err := renameFunc(legacy, dir); err != nil {
				// 迁失败（跨设备、权限不够、目录被占）→ **退回旧目录**：
				// 钉到旧的上、照常开，但把错误**报出去**——让 bootstrap
				// 记一条「钉不住」的日志，而不是吞掉之后用空目录开起来
				// （`internal/app/migrate.go` 犯过那个错，别再犯）。
				mygo.App.SetPath(mygo.PathUserData, legacy)
				return fmt.Errorf("应用数据目录迁移失败（%s → %s），退回旧目录：%w",
					legacy, dir, err)
			}
			log.Printf("pudmusic: 应用数据目录已从 %s 迁到 %s", legacy, dir)
		}
	}

	// `Path()` 在有覆盖值时是**直接返回、不建目录**的（`paths.go:57-60`），所以
	// 全新安装时这个目录还不存在，得自己建。不建的话第一次写设置就会失败，
	// 而失败又被当成「目录拿不到」，界面提示的是另一件事。
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("建应用数据目录 %s：%w", dir, err)
	}
	mygo.App.SetPath(mygo.PathUserData, dir)
	return nil
}
