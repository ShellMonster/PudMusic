//go:build e2e

package e2e

// 听歌统计那一页的真窗口回归。
//
// # 为什么要有这一族
//
// 界面层那一族造的是一个**假外壳**：它没有真的窗口，而顶栏那枚图标是**用
// `ui.Context` 画的**。所以「那枚图标在真窗口里点得动、点了真的换页」这件
// 事，只有真窗口验得了。
//
// 而这一条尤其要紧：**统计页的入口是顶栏那一枚图标**，而侧栏里没有它——
// 那枚图标要是按了没反应，用户**没有任何别的路**进这一页（而界面层那一族
// 验的就是它，按了没反应的话那一族会红——但真窗口上仍可能有差别，比如
// 那枚图标被别的元素盖住了）。
//
// # 怎么跑
//
//	BIU_RUN_MYGO_E2E=1 go test -tags e2e ./internal/e2e/ -run TestTheStatsPage
//
// ⚠️ **要一个真窗口**：它开一个窗口并驱动它，所以本机得能开窗口。
//
// ⚠️ **它跑的是整个应用**（不是 fixture）：要验的就是这个应用的那一枚图标。

import (
	"os"
	"testing"
	"time"
)

// TestTheStatsPage 断**顶栏那枚图标能进统计页，而且那一页画得出来**。
func TestTheStatsPage(t *testing.T) {
	if os.Getenv("BIU_RUN_MYGO_E2E") != "1" {
		t.Skip("设置 BIU_RUN_MYGO_E2E=1 才运行真窗口 E2E")
	}

	client, stop := startRealApp(t)
	defer stop()
	window := firstContentWindow(t, client)

	// 那一枚图标在顶栏上。
	//
	// ⚠️ **它按「无障碍名」找**（"听歌统计"）：那枚图标的画面上只有一个图形，
	// 而它的 `Label` 是给读屏软件和 automation 用的（见 navbar.go 那一段）。
	if !waitForTree(t, client, window, "听歌统计", 20*time.Second) {
		t.Fatalf("顶栏上没有「听歌统计」那一枚：\n%s", treeDump(t, client, window))
	}

	// 按它。
	//
	// ⚠️ **按 Key 按，不是按无障碍名按**：automation 的 `act` 只认 Key 或
	// id（见框架的 `plugins/automation/protocol.go`）。那一枚图标的 Key 是
	// "nav-stats"（见 navbar.go 那一处）。
	err := automationCall(client, 300, "act", map[string]any{
		"window": window,
		"key":    "nav-stats",
		"action": "press",
	}, nil)
	if err != nil {
		t.Fatalf("按顶栏那枚「听歌统计」：%v", err)
	}

	// 那一页画出来了。
	//
	// ⚠️ **等的是「那一页自己的东西」而不是「听歌统计」这四个字**——顶栏那一枚
	// 图标的**无障碍名**也是「听歌统计」，而它**在每一页都在**。用那四个字当
	// 判据的话，按完图标它立刻就成立——于是「有没有真的换页」根本验不到。
	//
	// ⚠️ **「还没有听歌记录」是这一页独有的**：第一次运行（还没听过歌）时
	// 画的就是它（见 stats_view.go 的 `viewStatsEmpty`）。
	if !waitForTree(t, client, window, "还没有听歌记录", 20*time.Second) {
		t.Fatalf("按了那一枚之后没有进统计页：\n%s", treeDump(t, client, window))
	}
	// 四档都在。
	for _, tab := range []string{"日", "周", "月", "全部"} {
		if !treeHasLabel(t, client, window, tab) {
			t.Errorf("统计页上没有 %q 那一档：\n%s", tab, treeDump(t, client, window))
		}
	}
}
