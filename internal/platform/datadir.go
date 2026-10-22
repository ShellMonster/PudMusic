// 应用数据目录：显示名改了，**目录名不改**。
//
// ## 这件事为什么存在
//
// 应用 2026-10 从 `Biu MyGo` 改名成 `BiuGo`。而 MyGo 的 `PathUserData` 是
// `os.UserConfigDir()` **拼上应用名**（`mygo@v0.6.2/paths.go:90-93`）：
//
//	case PathUserData:
//	    dir, err := os.UserConfigDir()
//	    return filepath.Join(dir, App.Name()), true, err
//
// 所以照着那个默认值走，改名之后数据目录会从
//
//	~/Library/Application Support/Biu MyGo
//
// 变成
//
//	~/Library/Application Support/BiuGo
//
// 而老目录还在原地没人读——**设置、搜索历史、快捷键、歌词缓存、下载记录、
// 界面状态全都在那儿**。用户看到的症状是「升级之后我的东西全没了」，而那一行
// 里没有一个字写着「你的东西被搬走了」：老数据一个字节没坏，只是这个版本不再
// 去那个目录找了。窗口位置（`WindowOptions.StateKey` 落在同一个目录）、
// 单实例锁（`single_instance.go` 的 `lockStatePath`）和框架自己的窗口状态恢复
// 也都在这里，一起丢。
//
// ## 选的是「不迁移」，不是「迁移」
//
// 两条路都能让老用户的数据不丢，这里选了改动最小的那条：**把目录名钉死在旧名字
// 上**，不搬一次、也不在每次启动时去判断新旧目录谁是谁。理由有三条。
//
//  1. **搬目录是那种「错了就静默丢数据」的活。** 跨设备、配额满、目录只读、
//     搬一半断电——任何一种落到用户身上都是「数据分成两半」，而两半**都不完整**。
//     钉名字则**一个字节都不动**，老用户连一次启动的差别都感觉不到。
//  2. **本项目还没有对外发过版。** 老目录里装的是维护者本机和少数内测机的数据，
//     没有「成千上万个用户要迁」这件事要扛。真的发过版之后再迁，那时才有值得
//     写迁移链、值得处理「新旧目录同时存在」的规模。
//  3. **目录名和应用名从此不一致，这本身是好事**：目录名不再跟着显示名走，
//     **下一次改名也不会再丢一次数据**。这是一次性的不一致，换来的是一个不会
//     重复犯的错。
//
// 代价是明面上的：在 Finder 里那个目录叫 `Biu MyGo`，应用叫 BiuGo。
// `SnapshotReadme`（`internal/app/upgrade.go`）里教用户敲的那条路径因此仍然是
// `…/Application Support/Biu MyGo`——**那是给用户回滚用的，必须继续指向真实
// 存在的那个目录**。
//
// ## 什么时候该搬
//
// 哪天真要搬（发过版之后、攒了一批真实用户），做法是：把这个 `SetPath` 换成
// 「旧目录不在且新目录也不在时把旧的 `os.Rename` 过去」，并且**保留一个版本**
// 的回退——搬失败要落回旧目录并报出来，不是静默地用空目录开起来。
// 那个 bug 已经在 `internal/app` 里犯过一次（`migrate.go`），别再犯第二次。
package platform

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// LegacyUserDataDirName 是应用数据目录的名字，**故意和显示名不一样**。
//
// 显示名是 `BiuGo`（`mygo.json` 的 `name`），这里还是老名字——理由见本文件顶上
// 那一节，一句话就是：跟着显示名走，老用户升级之后设置、搜索历史、快捷键、
// 歌词缓存、下载记录和界面状态会一起「消失」。
//
// ⚠️ **不要「顺手」把它改成和应用名一致。** 那一下的代价是全体老用户的数据
// 消失，而症状是静默的：没有报错，只是界面回到出厂设置。
//
// 同时它也是钥匙串之外**唯一**不该跟着改名走的东西：登录态靠的是
// `internal/app/credentials_keychain_darwin.go` 里写死的服务名
// `com.biu-go.biu-mygo.login`，那个也不能动（动了老用户要重新登录）。
const LegacyUserDataDirName = "Biu MyGo"

// PinUserDataDir 把应用数据目录钉在 `LegacyUserDataDirName` 上（见本文件顶上）。
//
// 必须在**任何一次 `mygo.App.Path(mygo.PathUserData)` 之前**调，而且要在
// `mygo.App.Run()` 之前——单实例锁与退出回调同理。调用点是 `main()` 的第一句，
// 它排在 `app.New()` 与所有 `Mount*` 之前：数据升级（`ui.MountMigration`）和读
// 设置（`ui.MountSettings`）都要读盘，排在它们后面就已经晚了一步，而**晚一步
// 的症状正是「按第一次运行处理」**——读不到设置就安静地用默认值开起来，用户只
// 发现「设置一直存不住」，换台机器再丢一次。
//
// 拿不到用户配置目录时返回错误，让调用方记一条日志继续走：那时候框架自己会
// 用默认值（应用名拼出来的那个新目录），是个能用的退路，值得带着它开起来。
func PinUserDataDir() error {
	base, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("拿不到用户配置目录：%w", err)
	}
	dir := filepath.Join(base, LegacyUserDataDirName)
	// `Path()` 在有覆盖值时是**直接返回、不建目录**的（`paths.go:57-60`），所以
	// 全新安装时这个目录还不存在，得自己建。不建的话第一次写设置就会失败，
	// 而失败又被当成「目录拿不到」，界面提示的是另一件事。
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("建应用数据目录 %s：%w", dir, err)
	}
	mygo.App.SetPath(mygo.PathUserData, dir)
	return nil
}
