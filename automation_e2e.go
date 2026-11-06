//go:build e2e

package main

import (
	"bufio"
	"net"
	"os"
	"strings"

	"github.com/ShellMonster/PudMusic/internal/ui"
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
	go serveTrayHook()
}

// serveTrayHook 是 E2E 的「点托盘菜单项」通道。
//
// ⚠️ **为什么要这条缝**：Linux 上托盘菜单由面板进程画（AppIndicator /
// StatusNotifierItem），Xvfb 下没有真面板——automation 的 `menu` 方法只
// 认窗口的菜单栏，够不到它。这条 socket 让 `internal/e2e` 的测试在**进程
// 外**点那一项，走的仍是菜单项自己的 `Click`（`ui.E2EActivateTrayItem`）。
//
// ⚠️ **协议是一行一个命令**：
//
//	tray.enabled <id>   → 回 "enabled" / "disabled" / "missing"
//	tray.click <id>     → 回 "ok"（激活了）或 "missing"（菜单/项不在）
//
// socket 放在 automation socket 旁边（`$MYGO_AUTOMATION.tray`）——那一条
// 已经在临时目录里，生命周期跟着 E2E 走。
func serveTrayHook() {
	auto := os.Getenv("MYGO_AUTOMATION")
	if auto == "" {
		return
	}
	sock := auto + ".tray"
	os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handleTrayHook(conn)
	}
}

func handleTrayHook(conn net.Conn) {
	defer conn.Close()
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return
	}
	fields := strings.Fields(line)
	reply := "bad\n"
	if len(fields) == 1 && fields[0] == "handoff.args" {
		// 「第二个实例转交的命令行到没到」那条探针（`ui.E2ELaunchArgs`，
		// 由 `handleLaunchArgs` 记）：回一行空格分隔的 args，空则回 "-"。
		// 它就是分不清「handoff 断了」和「toast 没画出来」时那唯一的一眼。
		args := ui.E2ELaunchArgs()
		if len(args) == 0 {
			reply = "-\n"
		} else {
			reply = strings.Join(args, " ") + "\n"
		}
	} else if len(fields) == 1 && fields[0] == "playback.stop" {
		// 「把正在放的那一首停掉、文件句柄放掉」——Windows 上 E2E 的
		// `t.TempDir()` 删不掉一个正被播放器占着的文件，子测试末尾要先让
		// 应用把它放开（`ui.E2EStopPlayback` → `App.ClearTrack`）。
		ui.E2EStopPlayback()
		reply = "ok\n"
	} else if len(fields) == 2 {
		switch fields[0] {
		case "tray.enabled":
			present, enabled := ui.E2ETrayItemEnabled(fields[1])
			if !present {
				reply = "missing\n"
			} else if enabled {
				reply = "enabled\n"
			} else {
				reply = "disabled\n"
			}
		case "tray.click":
			if ui.E2EActivateTrayItem(fields[1]) {
				reply = "ok\n"
			} else {
				reply = "missing\n"
			}
		}
	}
	conn.Write([]byte(reply))
}
