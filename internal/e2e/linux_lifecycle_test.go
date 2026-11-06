//go:build e2e

package e2e

// Linux 上的生命周期回归：启动、单实例、退出。
//
// # 为什么单独一族
//
// 这一票把 Linux 拉进首发目标（`mygo.json` 的 linux 段 + CI 的 Ubuntu job）。
// macOS 那一族（`menu_test.go` 的 `TestTheApplicationMenu`）验的是**那个平台
// 的菜单**；Linux 上要验的是另外三件事：
//
//  1. **起得来**：真窗口在 Xvfb 下开得出来，automation 连得上，内容窗口
//     里画得出界面（元素树里有东西）。
//  2. **单实例挡得住第二个**：起第二个进程，它该自己退出（命令行转交），
//     而第一个还活着。
//  3. **退得干净**：托盘菜单里「退出程序」那一项真的让进程退出来。
//
// # 退出那一档验的是哪条路
//
// ⚠️ **Linux 窗口上没有菜单栏**：`installMenu`（`internal/ui/menu.go`）
// 只在 macOS 上装——Windows / Linux 上框架默认一栏菜单都没有，多一条
// 反而是 Biu 没有的窗口布局。所以**不存在**「窗口菜单 → 退出」这条路，
// automation 的 `menu` 方法（`linux.TestActivateMenuItem`，只走
// `w.menubar`）在 Linux 上必报 "no menu bar"。
//
// 真正的退出入口是**托盘右键菜单的「退出程序」**（`internal/ui/tray.go`
// 的 `trayQuitID`，落在 `App.Quit()`）。而那份菜单是 AppIndicator
// （StatusNotifierItem）协议交给**面板进程**画的——Xvfb 下没有真面板，
// automation 够不到它。
//
// ⇒ 所以这一档走 **E2E 钩子**（`automation_e2e.go` 的 serveTrayHook +
// `internal/ui/e2e_hook.go` 的 `E2EActivateTrayItem`）：在真进程里、
// 主线程上，激活托盘菜单里 `tray.quit` 那一项——和用户在面板上点它是
// **同一个 Click、同一条 `App.Quit()`**。
//
// ⚠️ **验不到的那一半**：「面板真的把菜单画出来了、点得到」在 Xvfb
// 里没有面板可验，那一半不在这份测试里（也不在任何测试里——别把它
// 当成已覆盖）。验到的是：菜单项存在、能点、点了进程真退。
//
// ⚠️ **CI 上怎么跑**：`ubuntu-24.04` + Xvfb + `libgtk-3-0t64` +
// `libayatana-appindicator3-1`（见 `.github/workflows/linux.yml`）。
// 本机 macOS 上它会在 `GOOS != "linux"` 那一行 skip。
//
// # 怎么跑
//
//	BIU_RUN_MYGO_E2E=1 go test -tags e2e ./internal/e2e/ -run TestLinuxLifecycle
//
// ⚠️ **要一个真窗口**：它开一个窗口并驱动它，所以本机得能开窗口
// （Linux 下可以是 Xvfb）。

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestLinuxLifecycle 钉住 Linux 上「起得来 / 挡得住第二个 / 退得干净」。
func TestLinuxLifecycle(t *testing.T) {
	if os.Getenv("BIU_RUN_MYGO_E2E") != "1" {
		t.Skip("设置 BIU_RUN_MYGO_E2E=1 才运行真窗口 E2E")
	}
	if runtime.GOOS != "linux" {
		t.Skip("这一族只在 Linux 上跑：验的是 Linux 的启动/单实例/退出")
	}

	client, stop := startRealApp(t)
	defer stop()

	t.Run("内容窗口起得来", func(t *testing.T) {
		window := firstContentWindow(t, client)
		// ⚠️ **判据是「元素树里画得出东西」**：窗口起来了不等于界面上有内容
		// （GL 起不来、字体缺、panic 在半路上，都只剩一扇空窗）。
		// 「布丁音乐」那一栏在**每个路由页**的顶栏上，拿它当「真的画出来了」。
		if !waitForTree(t, client, window, "布丁音乐", 20*time.Second) {
			t.Fatalf("窗口里没有画出界面：\n%s", treeDump(t, client, window))
		}
	})

	t.Run("第二个实例递命令行后自己退出", func(t *testing.T) {
		// 再起一个**同一份二进制、同一份数据目录**的进程：单实例锁该把它挡
		// 回去——它把命令行递给第一个实例之后**自己退出**（`internal/platform`
		// 的 `Lock` 返回 false 的那一支）。
		//
		// ⚠️ **不能拿 `pudmusic-e2e` 那份 fixture 再起**：那一份的数据目录
		// 是 `startRealApp` 塞在 socketDir 里的，这里照同一份 env 再起一个，
		// 正是「同一把锁」的场景。
		// 第二个实例用**同一份已编好的二进制、同一份数据目录**——那才是
		// 「同一个应用起第二个进程」，重编或换目录验的都是另一件事。
		binary := appHandle.binary
		if binary == "" {
			t.Fatal("startRealApp 没记下二进制路径（appHandle.binary 是空的）")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary)
		cmd.Env = append(os.Environ(), "BIU_E2E_DATA_DIR="+appHandle.dataDir)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("第二个实例没有自己退出：%v\nstderr:\n%s", err, stderr.String())
		}
		// 第一个实例**还活着**（还能答 automation）——转交完命令行之后它
		// 该继续跑，而不是跟着一起退。
		var windows struct {
			Windows []struct {
				ID   int    `json:"id"`
				Kind string `json:"kind"`
			} `json:"windows"`
		}
		if err := automationCall(client, 300, "windows", nil, &windows); err != nil {
			t.Fatalf("第二个实例退出之后第一个实例不应跟着退：%v", err)
		}
	})

	t.Run("托盘菜单的退出项真的让进程退出来", func(t *testing.T) {
		// 先断「菜单里真有这一项、它能点」——把「没这项」和「点了没退」
		// 分开，不然挂了分不清是菜单形状坏了还是退出路径坏了。
		switch got := trayHookCall(t, "tray.enabled tray.quit"); got {
		case "enabled":
		case "missing":
			t.Fatal("托盘菜单里没有 tray.quit：托盘建不出来或菜单里没有退出项")
		default:
			t.Fatalf("tray.quit 是 %q（want enabled）：退出项该是能点的", got)
		}
		// 激活它——和用户在面板上点「退出程序」是同一个 Click、同一条
		// `App.Quit()`（OnBeforeQuit → 关窗口 → OnWillQuit 存设置 → 进程退）。
		if got := trayHookCall(t, "tray.click tray.quit"); got != "ok" {
			t.Fatalf("点托盘退出项：%q（want ok）", got)
		}
		// ⚠️ **判据是进程真的退**：`tray.click` 回了 "ok" 只说明「这项
		// 激活了」，不说明进程退了。
		if !waitForProcessGone(t, client, 20*time.Second) {
			t.Fatal("点了托盘「退出程序」之后进程还活着（automation socket 还在应答）")
		}
	})
}

// trayHookCall 往 E2E 钩子 socket（`$MYGO_AUTOMATION.tray`，见
// `automation_e2e.go` 的 serveTrayHook）发一行命令，拿一行回答。
//
// ⚠️ **socket 路径就是 automation socket 加 ".tray"**：`startRealApp` 把
// automation socket 存在 `appHandle.automationSocket`，两边规则一致。
func trayHookCall(t *testing.T, command string) string {
	t.Helper()
	sock := appHandle.automationSocket + ".tray"
	conn, err := net.DialTimeout("unix", sock, 5*time.Second)
	if err != nil {
		t.Fatalf("连托盘钩子 socket %s：%v", sock, err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.Write([]byte(command + "\n")); err != nil {
		t.Fatalf("发 %q：%v", command, err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("读 %q 的回答：%v", command, err)
	}
	return strings.TrimSpace(line)
}

// waitForProcessGone 等 automation socket **不再应答**——进程退出去之后
// socket 那端就没有人听了，下一次 `windows` 调用会拿到 EOF/超时。
//
// ⚠️ **判据是「答不上了」而不是「等了一会儿」**：固定毫秒数是机器忙的时候
// 最会飘的判据（`docs/agents/testing.md`）。
func waitForProcessGone(t *testing.T, client *automationClient, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var windows struct {
			Windows []struct {
				ID int `json:"id"`
			} `json:"windows"`
		}
		if err := automationCall(client, 500, "windows", nil, &windows); err != nil {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}
