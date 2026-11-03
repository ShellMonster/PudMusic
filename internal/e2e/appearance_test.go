//go:build e2e

package e2e

// 皮肤页（/appearance）的真窗口回归：顶栏那枚 palette 图标能按、按了真的
// 换页。形状照 `stats_test.go` 那一族——同样是「顶栏图标是唯一入口」的页。
//
//	BIU_RUN_MYGO_E2E=1 go test -tags e2e ./internal/e2e/ -run TestTheAppearancePage

import (
	"os"
	"testing"
	"time"
)

// TestTheAppearancePage 断**顶栏那枚图标能进皮肤页，而且那一页画得出来**。
func TestTheAppearancePage(t *testing.T) {
	if os.Getenv("BIU_RUN_MYGO_E2E") != "1" {
		t.Skip("设置 BIU_RUN_MYGO_E2E=1 才运行真窗口 E2E")
	}

	client, stop := startRealApp(t)
	defer stop()
	window := firstContentWindow(t, client)

	// 那一枚图标在顶栏上（按无障碍名找；画面上只有图形）。
	if !waitForTree(t, client, window, "皮肤", 20*time.Second) {
		t.Fatalf("顶栏上没有「皮肤」那一枚：\n%s", treeDump(t, client, window))
	}

	// 按它（automation 的 `act` 按 Key 找）。
	if err := automationCall(client, 300, "act", map[string]any{
		"window": window,
		"key":    "nav-appearance",
		"action": "press",
	}, nil); err != nil {
		t.Fatalf("按顶栏那枚「皮肤」：%v", err)
	}

	// 那一页画出来了：判据是这一页独有的「主题方案」（「皮肤」两个字在顶栏
	// 那枚图标的无障碍名里每一页都有，当不了判据）。
	if !waitForTree(t, client, window, "主题方案", 20*time.Second) {
		t.Fatalf("按了那一枚之后没有进皮肤页：\n%s", treeDump(t, client, window))
	}
}
