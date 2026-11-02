//go:build e2e

package e2e

// 换页收浮层的真窗口回归（评论抽屉换页不收那一票）。
//
// # 为什么要开真窗口
//
// 界面层那一族（`internal/ui/notfound_test.go` 的 `TestNavigating*`）造的是
// **假外壳**：它直接置 `s.drawerOpen = true` 等标志、再调 `s.router.Push`，
// 量的是「`syncRoute` 认出换页之后那几个标志收没收」。而这一条要验的是
// **整条用户路径**：播放条上点开抽屉 → 换一页 → 抽屉在**真窗口的无障碍树**
// 里真的不在了。
//
// # 为什么用**队列抽屉**而不是评论抽屉
//
// 评论键只在「当前这一首在 B 站上有对象」时才摆（`App.CommentsWorthAsking` —
// `internal/ui/comments_entry.go`）：本地文件压根没有那一枚。而命令行交进来
// 的文件一定是本地文件（`OnOpenFile` 那一条），UP 名那一条也要歌带
// `ArtistMid`——所以真窗口 E2E 里**评论抽屉开不出来**。
//
// 队列抽屉（`播放列表` 那一枚）**没有前提条件**：按下就 `s.drawerOpen = !`，
// 而它和评论抽屉是**同一副 Overlay 骨架**（`ui.Overlay` + 右侧滑入 +
// `Role(ui.RoleDialog)`），换页收它们的是**同一个出口**
// （`syncRoute` → `closeOverlaysForNav`）——用它验这条路径同样成立。
//
// ⚠️ **判据是无障碍树里有没有「播放列表」那个 dialog**：抽屉的面板挂着
// `Role(ui.RoleDialog).Label("播放列表")`（`queue_drawer.go`），它在树里是
// role=dialog 的节点；而播放条上那一枚键的无障碍名也是「播放列表」——
// **按 role 分**才不会把键当成抽屉。
//
// # 怎么跑
//
//	BIU_RUN_MYGO_E2E=1 go test -tags e2e ./internal/e2e/ -run TestNavigatingClosesTheQueueDrawer
//
// ⚠️ **要一个真窗口**：它开一个窗口并驱动它，所以本机得能开窗口。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// navNode 是无障碍树里这一项要的字段（id + 角色 + 名字 + key）。
type navNode struct {
	ID    string `json:"id"`
	Role  string `json:"role"`
	Label string `json:"label"`
	Key   any    `json:"key"`
}

// navTree 读窗口的元素树。
func navTree(t *testing.T, client *automationClient, window int) []navNode {
	t.Helper()
	var tree struct {
		Nodes []navNode `json:"nodes"`
	}
	if err := automationCall(client, 500, "tree", map[string]any{"window": window}, &tree); err != nil {
		t.Fatalf("读元素树：%v", err)
	}
	return tree.Nodes
}

// queueDialogInTree 报树上有没有「播放列表」那个 **dialog** 节点。
//
// ⚠️ **按 role 分**：播放条上那一枚键的无障碍名也是「播放列表」，而抽屉的
// 面板才是 `Role(ui.RoleDialog)`——只按 label 找会把键当成抽屉。
func queueDialogInTree(nodes []navNode) bool {
	for _, n := range nodes {
		if n.Role == "dialog" && n.Label == "播放列表" {
			return true
		}
	}
	return false
}

// waitForQueueDialog 等「播放列表」那个 dialog 节点出现（want=true）或消失
// （want=false）。
//
// ⚠️ **等的是「出现了/消失了」，不是「等了几帧」**：抽屉进出场是动画
// （面板滑进滑出那 200ms 里节点还在树上），固定毫秒数是机器忙的时候最会飘的
// 判据（`docs/agents/testing.md`）。
func waitForQueueDialog(t *testing.T, client *automationClient, window int, want bool, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if queueDialogInTree(navTree(t, client, window)) == want {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestNavigatingClosesTheQueueDrawer 断**真窗口上**：队列抽屉开着，换一次页，
// 抽屉收掉。
//
// 三段：
//
//  1. **命令行把一首 FLAC 交进来**（应用起来就放它——播放条上那一组键
//     有歌的时候才齐；队列那一枚本身没前提，带歌是顺带给「换页前后播放条
//     还在」一个更真的状态）；
//  2. **按播放条上那一枚「播放列表」** → 抽屉在树里出现
//     （role=dialog、label="播放列表"）；
//  3. **点应用菜单「设置…」**换页 → 那个 dialog 节点从树里消失。
//
// ⚠️ **换页走的是应用菜单「设置…」**（`openSettingsFromMenu` →
// `s.navigate("/settings")`），不是 UP 名：UP 名那一条要歌带
// `ArtistMid`，命令行交进来的本地文件没有 mid——而收浮层的是 `syncRoute`
// 那一处收口，**哪条换页路径都一样**（理由见 `internal/ui/notfound.go`）。
//
// ⚠️ **为什么是菜单而不是侧栏/顶栏**：抽屉开着的时候它的蒙版是**模态**的
// （`queue_drawer.go` 的 `back.Modal()`），框架的无障碍树在模态开着时**只给
// 模态自己和它上面的东西**（`mygo/ui/access.go` 的 accessTree：「What is
// behind a dialog is inert」）——侧栏、顶栏在树上**整个不在**，`act` 按不
// 到任何一枚页面上的键。而**应用菜单是系统画的、不走元素树**，它在模态
// 开着的时候照常点得——这也是**真人**唯一的换页道。
func TestNavigatingClosesTheQueueDrawer(t *testing.T) {
	if os.Getenv("BIU_RUN_MYGO_E2E") != "1" {
		t.Skip("设置 BIU_RUN_MYGO_E2E=1 才运行真窗口 E2E")
	}

	// 那一首 FLAC 在仓库里（解码器的测试素材），命令行交进来 = 应用起来就放它。
	flac := filepath.Join(repoRoot(t), "internal", "service", "audio", "flacdec", "testdata", "stereo16.flac")
	if _, err := os.Stat(flac); err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}

	client, stop := startRealAppWithArgs(t, flac)
	defer stop()
	window := firstContentWindow(t, client)

	// 播放条上那一枚「播放列表」要在。
	if !waitForTree(t, client, window, "播放列表", 20*time.Second) {
		t.Fatalf("播放条上没有「播放列表」那一枚：\n%s", treeDump(t, client, window))
	}

	// 点开抽屉。「播放列表」那枚键**没有 key**（`iconKey` 没挂 key——它就是
	// 一个普通的图标键），所以按**元素 id** 按：先在树上把它找出来。
	id := findNodeID(t, client, window, func(n navNode) bool {
		return n.Label == "播放列表" && n.Role == "button"
	})
	if id == "" {
		t.Fatalf("树上找不到「播放列表」那一枚：\n%s", treeDump(t, client, window))
	}
	if err := automationCall(client, 501, "act", map[string]any{
		"window": window,
		"id":     id,
		"action": "press",
	}, nil); err != nil {
		t.Fatalf("按播放条上那一枚「播放列表」：%v", err)
	}
	if !waitForQueueDialog(t, client, window, true, 20*time.Second) {
		t.Fatalf("按了「播放列表」之后抽屉没开：\n%s", treeDump(t, client, window))
	}

	// 换一页：应用菜单「设置…」。模态开着的时候页面上一个可点的节点都不在
	// 树上（见顶上那段「为什么是菜单」），菜单是系统画的、不走元素树。
	if err := automationCall(client, 502, "menu", map[string]any{
		"window": window,
		"path":   []string{windowTitle(t, client, window), "设置…"},
	}, nil); err != nil {
		t.Fatalf("点应用菜单「设置…」：%v", err)
	}
	if !waitForQueueDialog(t, client, window, false, 20*time.Second) {
		t.Fatalf("换页之后队列抽屉还开着：\n%s", navTreeDump(t, client, window))
	}
}

// navTreeDump 把窗口的元素树（id / role / key / label 全列出）打出来，失败时用。
func navTreeDump(t *testing.T, client *automationClient, window int) string {
	t.Helper()
	var b strings.Builder
	for _, n := range navTree(t, client, window) {
		fmt.Fprintf(&b, "id=%s role=%s key=%v label=%q\n", n.ID, n.Role, n.Key, n.Label)
	}
	return b.String()
}

// findNodeID 在树上找第一个满足 cond 的节点的 id（字符串形式）。
func findNodeID(t *testing.T, client *automationClient, window int, cond func(navNode) bool) string {
	t.Helper()
	var tree struct {
		Nodes []struct {
			ID    string `json:"id"`
			Role  string `json:"role"`
			Label string `json:"label"`
			Key   any    `json:"key"`
		} `json:"nodes"`
	}
	if err := automationCall(client, 503, "tree", map[string]any{"window": window}, &tree); err != nil {
		t.Fatalf("读元素树：%v", err)
	}
	for _, n := range tree.Nodes {
		if cond(navNode{Role: n.Role, Label: n.Label, Key: n.Key}) {
			return n.ID
		}
	}
	return ""
}
