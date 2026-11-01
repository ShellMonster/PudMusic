//go:build e2e

package main

import (
	"os"

	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/plugins/automation"
)

// E2E 那一轮的应用数据目录：**单独的一份**。
//
// ⚠️ **为什么必须单独一份**：应用有**单实例锁**（`internal/ui/shell_window.go`
// 的 `platform.Lock()`）——而那把锁在**用户数据目录**里
// （`internal/platform/single_instance.go` 的 `lockPath()`）。
//
// ⇒ 所以**用户机器上已经跑着一个这个应用**时（开发时几乎总是这样），E2E 起
// 来的那一份会**抢不到锁**，于是它把命令行递给那个已经在跑的、然后**自己立刻
// 退出**。
//
// ⇒ 症状极怪：automation 的 socket **文件在**（插件的 `init` 建了它），但
// **没人听**——连上去是「connection refused」；而应用那边**一个字都没说**
// （它正常退出了，退出码 0）。
//
// ⇒ 所以这里把 `PathUserData` 挪到一个临时目录，E2E 那一轮就有自己的锁、
// 自己的设置、自己的凭据——**和用户正在跑的那个应用彻底分开**。
//
// ⚠️ **这个目录是谁给的**：`BIU_E2E_DATA_DIR` 环境变量（`internal/e2e` 的
// 测试起进程时设）。没设就**不动** `PathUserData`——那意味着「和用户正在跑
// 的那个应用抢锁」，而那只会让 E2E 跑不起来。
func init() {
	dir := os.Getenv("BIU_E2E_DATA_DIR")
	if dir == "" {
		return
	}
	mygo.App.SetPath(mygo.PathUserData, dir)
}
