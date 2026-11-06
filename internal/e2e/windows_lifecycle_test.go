//go:build e2e

package e2e

// Windows 上的生命周期回归：启动、单实例、退出。
//
// # 为什么要有这一族
//
// Linux 那一族（`linux_lifecycle_test.go` 的 `TestLinuxLifecycle`）验的是
// 「起得来 / 挡得住第二个 / 退得干净」——而 Windows 上有**自己的一份坑**：
//
//   - **退出入口不一样**：Windows 的托盘是通知区图标（NotifyIcon），菜单
//     由系统画；窗口上**也没有**菜单栏（`installMenu` 只在 macOS 装）。
//     所以「退出程序」那一条同样要走 E2E 钩子（`automation_e2e.go` 的
//     `serveTrayHook` → `E2EActivateTrayItem`），和 Linux 一个道理。
//   - **第二个实例**：Windows 上没有 Apple Event，第二个进程的命令行
//     全靠单实例锁转交（`internal/platform/single_instance.go`）——
//     这一条和 Linux 形状一样，但走的那把锁是 Windows 那一半
//     （`single_instance_windows.go`）。
//
// ⚠️ **验不到的那一半**：「资源管理器真的把 `.m4a` 关联给我们」要装过
// NSIS 安装包才成立——而那一半在 `windows_fileassoc_test.go` 里也只验到
// 「命令行递进来应用放」。这里验的是**进程进出**：起得来、第二个退、
// 点了退出真退。
//
// # 怎么跑
//
//	BIU_RUN_MYGO_E2E=1 go test -tags e2e ./internal/e2e/ -run TestWindowsLifecycle
//
// ⚠️ **要一个真窗口**：它开一个窗口并驱动它，所以本机得能开窗口。

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// TestWindowsLifecycle 钉住 Windows 上「起得来 / 挡得住第二个 / 退得干净」。
func TestWindowsLifecycle(t *testing.T) {
	if os.Getenv("BIU_RUN_MYGO_E2E") != "1" {
		t.Skip("设置 BIU_RUN_MYGO_E2E=1 才运行真窗口 E2E")
	}
	if runtime.GOOS != "windows" {
		t.Skip("这一族只在 Windows 上跑：验的是 Windows 的启动/单实例/退出")
	}

	client, stop := startRealApp(t)
	defer stop()

	t.Run("内容窗口起得来", func(t *testing.T) {
		window := firstContentWindow(t, client)
		// 「布丁音乐」那一栏在每个路由页的顶栏上。
		if !waitForTree(t, client, window, "布丁音乐", 20*time.Second) {
			t.Fatalf("窗口里没有画出界面：\n%s", treeDump(t, client, window))
		}
	})

	t.Run("第二个实例递命令行后自己退出", func(t *testing.T) {
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
		// 第一个实例还活着。
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
		switch got := trayHookCall(t, "tray.enabled tray.quit"); got {
		case "enabled":
		case "missing":
			t.Fatal("托盘菜单里没有 tray.quit：托盘建不出来或菜单里没有退出项")
		default:
			t.Fatalf("tray.quit 是 %q（want enabled）：退出项该是能点的", got)
		}
		if got := trayHookCall(t, "tray.click tray.quit"); got != "ok" {
			t.Fatalf("点托盘退出项：%q（want ok）", got)
		}
		if !waitForProcessGone(t, client, 20*time.Second) {
			t.Fatal("点了托盘「退出程序」之后进程还活着（automation socket 还在应答）")
		}
	})
}
