//go:build e2e

package e2e

// 列表行下面那条进度条的**无障碍**回归（票 #703 的 C 项）。
//
// # 为什么这一族要开真窗口
//
// 这一条断的是**读屏软件读得到的那一份**：那一行在无障碍树里是一个**值多少的
// 进度条**，而且**有名字**。
//
// ⚠️ **无头 Tester 断不了这一层**：框架只在**辅助技术问起**时才描述那一帧
// （`mygo/ui/access.go` 的 `accessibilityOn`——真窗口里是系统的读屏软件问起，
// 无头窗口里没人问，那一帧就压根不描述）。所以这一族走 automation 插件的
// `tree` 方法：它读的是**同一份无障碍树**（`mygo/ui/automation.go` 的
// `AutomationTree`）。
//
// ⚠️ **而手搓那一版压根做不到**：两个 `ui.Box` 既没有角色也没有值——它们在
// 无障碍树里**根本不存在**（`accessRole` 对一个没标签、不可点的盒子返回 false）。
// 所以这一条是这一票真正的收益，也是它唯一的判据。
//
// # 怎么跑
//
//	BIU_RUN_MYGO_E2E=1 go test -tags e2e ./internal/e2e/ -run TestTheListProgressBar
//
// ⚠️ **要一个真窗口**：它开一个窗口并驱动它，所以本机得能开窗口。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestTheListProgressBarIsAProgressIndicatorWithAName 钉住「列表行下面那条进度条
// 在**无障碍树**里是一个**有名字、值多少的进度条**」。
//
// 三样都断：
//
//  1. **角色是 progress**：不是「一个盒子」，也不是「一段文字」。
//  2. **值是 0.5**：`Range(0, 1, value)` 给的那一份（歌听了 30 秒 / 全长 60 秒）。
//  3. **名字带着歌名**：光说「播放进度」的话，读屏软件在一屏十几行里读到的
//     是十几次同一句话，分不出是哪一首。
func TestTheListProgressBarIsAProgressIndicatorWithAName(t *testing.T) {
	if os.Getenv("BIU_RUN_MYGO_E2E") != "1" {
		t.Skip("设置 BIU_RUN_MYGO_E2E=1 才运行真窗口 E2E")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	socketDir, socket, err := makeAutomationSocket()
	if err != nil {
		t.Fatalf("创建自动化 socket：%v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })

	binary := filepath.Join(socketDir, "progress-fixture")
	var buildOut, buildErr bytes.Buffer
	build := exec.Command("go", "build", "-tags", "e2e", "-o", binary, "./internal/e2e/fixture_progress")
	build.Dir = repoRoot(t)
	build.Stdout = &buildOut
	build.Stderr = &buildErr
	if err := build.Run(); err != nil {
		t.Fatalf("构建进度条 fixture：%v\nstdout:\n%s\nstderr:\n%s", err, buildOut.String(), buildErr.String())
	}

	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = append(os.Environ(), "MYGO_AUTOMATION="+socket)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动进度条 fixture：%v", err)
	}
	defer func() {
		cancel()
		waitForFixtureExit(t, cmd)
	}()

	conn := waitForAutomationSocket(t, socket, cmd, &stdout, &stderr)
	defer conn.Close()
	client := &automationClient{conn: conn, enc: json.NewEncoder(conn), dec: bufio.NewReader(conn)}

	window := firstContentWindow(t, client)

	// ⚠️ **等那一项出现，而不是问一次就当没有**：树是**每帧重算**的，而这一帧
	// 要等列表把那一行建出来（虚拟列表按滚动位置建行）。
	var node progressNode
	deadline := time.Now().Add(20 * time.Second)
	for {
		tree := progressTree(t, client, window)
		for _, n := range tree {
			if n.Role == "progress" {
				node = n
				break
			}
		}
		if node.Label != "" || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if node.Label == "" {
		t.Fatalf("无障碍树里没有进度条：\n%s", treeDump(t, client, window))
	}

	if node.Role != "progress" {
		t.Errorf("那一项的角色是 %q，want progress", node.Role)
	}
	if node.Label != "「一首歌」的播放进度" {
		t.Errorf("那一项的名字是 %q，want「一首歌」的播放进度", node.Label)
	}
	if node.Now == nil || node.Min == nil || node.Max == nil {
		t.Fatalf("那一项没有值：min=%v max=%v now=%v", node.Min, node.Max, node.Now)
	}
	if *node.Min != 0 || *node.Max != 1 {
		t.Errorf("那一项的范围是 [%v, %v]，want [0, 1]", *node.Min, *node.Max)
	}
	if *node.Now < 0.49 || *node.Now > 0.51 {
		t.Errorf("那一项的值是 %v，want 0.5（听了 30 秒 / 全长 60 秒）", *node.Now)
	}
}

// progressNode 是无障碍树里的一项（进度条那一族的字段）。
type progressNode struct {
	Role  string   `json:"role"`
	Label string   `json:"label"`
	Min   *float64 `json:"min"`
	Max   *float64 `json:"max"`
	Now   *float64 `json:"now"`
}

// progressTree 读窗口的元素树。
func progressTree(t *testing.T, client *automationClient, window int) []progressNode {
	t.Helper()
	var tree struct {
		Nodes []progressNode `json:"nodes"`
	}
	if err := automationCall(client, 300, "tree", map[string]any{"window": window}, &tree); err != nil {
		t.Fatalf("读元素树：%v", err)
	}
	return tree.Nodes
}
