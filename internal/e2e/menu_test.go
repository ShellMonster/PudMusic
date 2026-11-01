//go:build e2e

package e2e

// 真窗口上的菜单回归（票 #712）。
//
// # 为什么要有这一族
//
// 菜单是**系统画的**（macOS 的 NSMenu），它**不在窗口的画面里**——截不到也
// 量不到（见 `internal/ui/menu.go` 顶上那一段）。而界面层那一族测试断的是
// 「菜单树里有什么」，断不到两件事：
//
//  1. **菜单真的装上了、真的在菜单栏里**（`SetMenu` 的时机对不对、栏名对不对）。
//  2. **点了菜单项之后，状态真的变了**。
//
// 而这一族的独有价值在第 2 条：**菜单的加速键会先于窗口拿到那个键**
// （框架自己的 E2E：`mygo/internal/e2e/menu_shortcuts_windows_test.go:14`
// ——`Ctrl+,` 跑了菜单项，内容区一次都没收到）。这件事**只有真窗口验得了**。
//
// # 怎么跑
//
//	BIU_RUN_MYGO_E2E=1 go test -tags e2e ./internal/e2e/ -run TestTheApplicationMenu
//
// ⚠️ **要一个真窗口**：它开一个窗口并驱动它，所以本机得能开窗口。
//
// # ⚠️ 它跑的是**整个应用**（不是 fixture）
//
// 因为要验的就是**这个应用的菜单**——界面层那一族造的是一个假外壳，而菜单
// 是应用级的（`SetMenu` 设在 `mygo.App` 上）。所以这里跑的是根目录那个
// `main`（带 `e2e` 标签，它多 import 了 automation 插件）。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// menuAppName 是应用在菜单栏第一栏上写的名字。
//
// ⚠️ **第一栏的名字是应用名**（`RoleAppMenu` 的默认标签就是 `App.Name()`）。
//
// ⚠️ **而 `App.Name()` 在「源码直接编出来的二进制」上不是「布丁音乐」**：
// 框架的 `Name()`（`mygo/app.go:316`）先看 `packageInfo()`（那要**bundle**或
// `mygo build` 注入的 ldflags），拿不到就**回落到可执行文件的名字**——
// 而 E2E 编出来的那个叫 `biugo-e2e`。
//
// ⇒ 所以这里**从窗口标题反推**：真机上第一栏写着「布丁音乐」，而 E2E 里写着
// 「biugo-e2e」——**两个都要能用**，所以不写死，取窗口标题。

// TestTheApplicationMenu 断**应用菜单在真窗口上装上了，而且点了真的做事**。
//
// 三个子断言：
//
//  1. 菜单栏上**有那七栏**（按标题找栏——automation 的 `menu` 方法用
//     `indexOfItemWithTitle:` 找，找不到就报「no menu item」）。
//  2. 「播放」「歌曲」那两栏**在菜单栏上**——它们是**我们**写的（框架没有这两个
//     角色），而它们的栏名就是那一栏的 `Label`（`mygo/internal/darwin/menu.go:143`）。
//     **漏了 `Label` 就是一片空白**，而空白的栏在菜单栏上极难懂。
//  3. 点「设置…」**真的换到了设置页**——那一条要穿过「让开迷你 → 换路由 →
//     叫回主窗口」三步（`openSettingsFromMenu`），每一步都能出错。
func TestTheApplicationMenu(t *testing.T) {
	if os.Getenv("BIU_RUN_MYGO_E2E") != "1" {
		t.Skip("设置 BIU_RUN_MYGO_E2E=1 才运行真窗口 E2E")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("这一族只在 macOS 上跑：菜单栏是 macOS 的东西")
	}

	client, stop := startRealApp(t)
	defer stop()
	window := firstContentWindow(t, client)

	t.Run("每一栏都在菜单栏上", func(t *testing.T) {
		// ⚠️ **按标题找栏**：automation 的 `menu` 方法在**第一段路径**上就是
		// 在主菜单里找那一栏（`indexOfItemWithTitle:`）。
		//
		// ⚠️ **怎么问「有」而不「执行」**：路径给两段——第一段是栏名，第二段是
		// 一个**故意不存在**的项名。框架的 `TestPerformMenuItem`（`mygo/internal/darwin/testhooks.go:20`）
		// 找第一段找到之后会**下钻到那一栏的子菜单**，再找第二段——找不到就报
		// 「no menu item <第二段>」。而**栏本身不在的话**，报的是
		// 「no menu item <栏名>」。
		//
		// ⇒ 所以：**报错里说的是第二段 = 那一栏在**（只是第二段那个项不存在）；
		// **报错里说的是第一段 = 那一栏不在**。
		app := windowTitle(t, client, window)
		for _, column := range []string{app, "文件", "编辑", "播放", "歌曲", "显示", "窗口"} {
			err := automationCall(client, 100, "menu", map[string]any{
				"window": window,
				"path":   []string{column, "这一项故意不存在"},
			}, nil)
			if err == nil {
				t.Errorf("菜单栏上没有 %q 那一栏", column)
				continue
			}
			msg := err.Error()
			if strings.Contains(msg, "no menu item "+column) {
				t.Errorf("菜单栏上没有 %q 那一栏：%v", column, err)
			}
		}
	})

	t.Run("点设置换到了设置页", func(t *testing.T) {
		err := automationCall(client, 101, "menu", map[string]any{
			"window": window,
			"path":   []string{windowTitle(t, client, window), "设置…"},
		}, nil)
		if err != nil {
			t.Fatalf("点「设置…」：%v", err)
		}
		if !waitForTree(t, client, window, "常规设置", 20*time.Second) {
			t.Fatalf("点了「设置…」之后窗口里没有出现设置页的内容：\n%s", treeDump(t, client, window))
		}
	})
}

// startRealApp 起一个带 automation 的真应用，返回 automation 客户端。
//
// ⚠️ **编的是根目录那个 main**，不是 `fixture/` 里的那个——要验的就是
// **这个应用的菜单**，而菜单是应用级的。
func startRealApp(t *testing.T) (*automationClient, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	socketDir, socket, err := makeAutomationSocket()
	if err != nil {
		cancel()
		t.Fatalf("创建自动化 socket：%v", err)
	}
	binary := filepath.Join(socketDir, "biugo-e2e")
	var buildOut, buildErr bytes.Buffer
	build := exec.Command("go", "build", "-tags", "e2e", "-o", binary, ".")
	build.Dir = repoRoot(t)
	build.Stdout = &buildOut
	build.Stderr = &buildErr
	if err := build.Run(); err != nil {
		cancel()
		os.RemoveAll(socketDir)
		t.Fatalf("构建真应用（带 e2e 标签）：%v\nstdout:\n%s\nstderr:\n%s", err, buildOut.String(), buildErr.String())
	}

	cmd := exec.CommandContext(ctx, binary)
	// ⚠️ **单独的一份数据目录**（`automation_e2e.go` 顶上那段说明了为什么）：
	// 应用有单实例锁，而锁在用户数据目录里——**用户机器上已经跑着这个应用
	// 时**（开发时几乎总是这样），E2E 起来的那一份会抢不到锁、把命令行递过去、
	// 然后自己退出。症状是「socket 文件在，但没人听」。
	cmd.Env = append(os.Environ(),
		"MYGO_AUTOMATION="+socket,
		"BIU_E2E_DATA_DIR="+filepath.Join(socketDir, "data"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		os.RemoveAll(socketDir)
		t.Fatalf("启动真应用：%v", err)
	}
	stop := func() {
		cancel()
		waitForFixtureExit(t, cmd)
		os.RemoveAll(socketDir)
	}

	conn := waitForAutomationSocket(t, socket, cmd, &stdout, &stderr)
	client := &automationClient{conn: conn, enc: json.NewEncoder(conn), dec: bufio.NewReader(conn)}
	return client, stop
}

// windowTitle 是那个内容窗口的标题。
//
// ⚠️ **第一栏（应用菜单）的名字就是窗口标题**（框架的 `RoleAppMenu` 用
// `App.Name()`，而窗口标题用的也是它）——所以从这里就能拿到那一栏的名字，
// **不用写死「布丁音乐」**。
//
// ⚠️ **为什么不能写死**：源码直接编出来的那个二进制叫 `biugo-e2e`，而框架的
// `App.Name()`（`mygo/app.go:316`）在拿不到 bundle 信息时**回落到可执行文件
// 的名字**——所以 E2E 里第一栏写着的是「biugo-e2e」，真机上才是「布丁音乐」。
func windowTitle(t *testing.T, client *automationClient, window int) string {
	t.Helper()
	var windows struct {
		Windows []struct {
			ID    int    `json:"id"`
			Kind  string `json:"kind"`
			Title string `json:"title"`
		} `json:"windows"`
	}
	if err := automationCall(client, 2, "windows", nil, &windows); err != nil {
		t.Fatalf("问窗口标题：%v", err)
	}
	for _, w := range windows.Windows {
		if w.ID == window {
			return w.Title
		}
	}
	t.Fatalf("窗口 %d 不在 %+v 里", window, windows.Windows)
	return ""
}

// firstContentWindow 问出那个内容窗口的 id。
//
// ⚠️ **要等窗口出来**：automation 插件的 `ready` 是在**它自己那个**
// `WhenReady` 回调里关上的，而**应用的窗口是在另一个 `WhenReady` 回调里
// 开的**（`internal/ui/shell_window.go` 的 `s.openWindow()`）——两个回调的
// 先后不保证。
//
// ⇒ 所以**等窗口出现**，别问一次就当没有（那不是「应用没开窗口」，是「问早了」）。
func firstContentWindow(t *testing.T, client *automationClient) int {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		var windows struct {
			Windows []struct {
				ID   int    `json:"id"`
				Kind string `json:"kind"`
			} `json:"windows"`
		}
		if err := automationCall(client, 1, "windows", nil, &windows); err == nil {
			for _, w := range windows.Windows {
				if w.Kind == "content" {
					return w.ID
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("等不到内容窗口：%+v", windows.Windows)
			return 0
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// waitForTree 等窗口里出现那个标签。
//
// ⚠️ **等的是「出现了」而不是「等了几帧」**：固定毫秒数是机器忙的时候最会飘的
// 判据（`docs/agents/testing.md`）。
func waitForTree(t *testing.T, client *automationClient, window int, label string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if treeHasLabel(t, client, window, label) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// treeHasLabel 报窗口的元素树里有没有那一项。
func treeHasLabel(t *testing.T, client *automationClient, window int, label string) bool {
	t.Helper()
	var tree struct {
		Nodes []struct {
			Key   any    `json:"key"`
			Label string `json:"label"`
		} `json:"nodes"`
	}
	if err := automationCall(client, 200, "tree", map[string]any{"window": window}, &tree); err != nil {
		return false
	}
	for _, n := range tree.Nodes {
		if n.Label == label {
			return true
		}
	}
	return false
}

// treeDump 把窗口的元素树打出来，失败时用。
func treeDump(t *testing.T, client *automationClient, window int) string {
	t.Helper()
	var tree struct {
		Nodes []struct {
			Key   any    `json:"key"`
			Label string `json:"label"`
		} `json:"nodes"`
	}
	if err := automationCall(client, 201, "tree", map[string]any{"window": window}, &tree); err != nil {
		return fmt.Sprintf("（读不到元素树：%v）", err)
	}
	var b strings.Builder
	for _, n := range tree.Nodes {
		fmt.Fprintf(&b, "%v=%q\n", n.Key, n.Label)
	}
	return b.String()
}

// automationCall 发一个 automation 请求，**返回错误而不是把它打死**。
//
// ⚠️ **为什么和 `callAutomation` 分开**：这一族要**故意**问出一个错（找一栏
// 故意不存在的项），而 `callAutomation` 遇错就 `t.Fatalf`——那样就没法问
// 「这一栏在不在」了。
func automationCall(client *automationClient, id int, method string, params any, out any) error {
	_ = client.conn.SetDeadline(time.Now().Add(20 * time.Second))
	request := map[string]any{"id": id, "method": method}
	if params != nil {
		request["params"] = params
	}
	if err := client.enc.Encode(request); err != nil {
		return fmt.Errorf("发送 automation %s：%w", method, err)
	}
	line, err := client.dec.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("读取 automation %s：%w", method, err)
	}
	var response automationResponse
	if err := json.Unmarshal(line, &response); err != nil {
		return fmt.Errorf("解析 automation %s：%w", method, err)
	}
	if response.Error != nil {
		return fmt.Errorf("automation %s：%s：%s", method, response.Error.Code, response.Error.Message)
	}
	if out != nil && len(response.Result) > 0 {
		if err := json.Unmarshal(response.Result, out); err != nil {
			return fmt.Errorf("解析 automation %s 结果：%w", method, err)
		}
	}
	return nil
}

// repoRoot 是仓库根目录。
//
// ⚠️ **`fixtureDir()` 返回的是 `internal/e2e/fixture`**，所以要往上退**三层**
// 才是仓库根（`internal/e2e/fixture` → `internal/e2e` → `internal` → 根）。
//
// ⚠️ **退错一层的症状很怪**：会去 `internal/` 底下找 `go build .`，而那儿的
// Go 文件在 `build` 标签之外是空的 ⇒ 报「no Go files in …/internal」——
// 一个和「菜单有没有问题」毫无关系的错。
func repoRoot(t *testing.T) string {
	t.Helper()
	dir := fixtureDir()
	return filepath.Dir(filepath.Dir(filepath.Dir(dir)))
}
