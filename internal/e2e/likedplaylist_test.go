//go:build e2e

package e2e

// 喜欢歌单绑定那一票的真窗口回归：**无障碍树**上播放条 / 满屏里那几枚键
// 在不在。
//
// # 为什么这一族要开真窗口
//
// 「赞」和「收藏」那两枚键（连同它们的数字角标）被拿掉了，「喜欢」入口
// 留下。这件事在**无障碍树**里才是「读屏用户听得到的那一份」——界面层
// 那一族（`internal/ui/likedplaylist_test.go`）断的是同一棵树的**布局
// 事实**（`tt.Find` 那几枚键在不在），而这里断的是**辅助技术问起时那一帧
// 描述出来的样子**：
//
//   - 「赞」「收藏」两枚键**不在树里**（无障碍名字里没有它们，角标数字
//     跟着没了——数字本来就是挂在键上的子节点）；
//   - 「喜欢」入口**在**（名字读得到）；
//   - 满屏那一屏另验一遍：它有自己的一套键（`full:thumb` / `full:heart`），
//     和播放条不是同一份代码，单断播放条会漏掉它。
//
// 无头窗口里没人问，那一帧压根不描述（`mygo/ui/access.go` 的
// `accessibilityOn`）——所以只有真窗口验得了「拿掉的是社交键、留下的是
// 入口」这件事**读屏用户听不听得出**。
//
// # 怎么跑
//
//	BIU_RUN_MYGO_E2E=1 go test -tags e2e ./internal/e2e/ -run TestThePlayerHasOnlyTheLikeEntry
//
// ⚠️ **它跑的是 `fixture_liked`**：那台 fixture 内置一台会答「我创建的
// 收藏夹」的假 B 站、种下「已绑定」那份设置、正在放一首可赞的 mv——
// 「喜欢」亮着、绑定已就位，播放条上该只有它。

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

// TestThePlayerHasOnlyTheLikeEntry 断真窗口上：
//
// 播放条的无障碍树里——「赞」「收藏」两枚**不在**、「喜欢」**在**；
// 满屏的无障碍树里——「赞」「收藏」两枚**不在**、「喜欢」**在**。
//
// 断的是**无障碍名字**：读屏用户听得见的那个名字才是「这枚键在不在」的
// 判据（图标对辅助技术不可见，`mygo/ui/svg.go`）。
func TestThePlayerHasOnlyTheLikeEntry(t *testing.T) {
	if os.Getenv("BIU_RUN_MYGO_E2E") != "1" {
		t.Skip("设置 BIU_RUN_MYGO_E2E=1 才运行真窗口 E2E")
	}

	client, window, stop := startLikedFixture(t)
	defer stop()

	// 「喜欢」在：fixture 里「赞没赞」那一问已经回了（relation 里 like=false）。
	if !waitForTree(t, client, window, "喜欢", 20*time.Second) {
		t.Fatalf("播放条上没有「喜欢」那一枚：\n%s", treeDump(t, client, window))
	}
	// 「赞」「收藏」两枚不在——连同它们的角标（角标是键的子节点，键没了它
	// 就一起没了）。等一小段再断言「不在」，给「万一它迟来」一个露脸的机会：
	// 直接断言会漏掉「键慢了几帧才出现」那一档。
	for _, gone := range []string{"赞", "收藏"} {
		if waitForTree(t, client, window, gone, 3*time.Second) {
			t.Fatalf("播放条上还留着 %q 那一枚（它该被拿走了）：\n%s", gone, treeDump(t, client, window))
		}
	}

	// 满屏：点播放条上那张封面（`openFull` 的无头版——`act` 按元素）。
	// 它的那套键（`full:thumb` / `full:heart`）和播放条不是同一份代码，
	// 单断播放条会漏掉它。
	openFullWindow(t, client, window)

	if !waitForTree(t, client, window, "喜欢", 20*time.Second) {
		t.Fatalf("满屏上没有「喜欢」那一枚：\n%s", treeDump(t, client, window))
	}
	for _, gone := range []string{"赞", "收藏"} {
		if waitForTree(t, client, window, gone, 3*time.Second) {
			t.Fatalf("满屏上还留着 %q 那一枚（它该被拿走了）：\n%s", gone, treeDump(t, client, window))
		}
	}
}

// startLikedFixture 编起 `fixture_liked`，等 automation socket 起来。
//
// 返回 automation 客户端、第一个内容窗口的 id，和收尾。
func startLikedFixture(t *testing.T) (*automationClient, int, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	socketDir, socket, err := makeAutomationSocket()
	if err != nil {
		cancel()
		t.Fatalf("创建自动化 socket：%v", err)
	}
	binary := filepath.Join(socketDir, "liked-fixture")
	var buildOut, buildErr bytes.Buffer
	build := exec.Command("go", "build", "-tags", "e2e", "-o", binary,
		"./internal/e2e/fixture_liked")
	build.Dir = repoRoot(t)
	build.Stdout = &buildOut
	build.Stderr = &buildErr
	if err := build.Run(); err != nil {
		cancel()
		_ = os.RemoveAll(socketDir)
		t.Fatalf("构建喜欢歌单 fixture：%v\nstdout:\n%s\nstderr:\n%s",
			err, buildOut.String(), buildErr.String())
	}

	cmd := exec.CommandContext(ctx, binary)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"MYGO_AUTOMATION="+socket,
		"BIU_E2E_DATA_DIR="+filepath.Join(socketDir, "data"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		_ = os.RemoveAll(socketDir)
		t.Fatalf("启动喜欢歌单 fixture：%v", err)
	}
	stop := func() {
		cancel()
		waitForFixtureExit(t, cmd)
		_ = os.RemoveAll(socketDir)
	}

	conn := waitForAutomationSocket(t, socket, cmd, &stdout, &stderr)
	client := &automationClient{conn: conn, enc: json.NewEncoder(conn), dec: bufio.NewReader(conn)}
	window := firstContentWindow(t, client)
	return client, window, stop
}

// openFullWindow 点开满屏播放器：按播放条上那张封面（`当前封面`那一项）。
//
// ⚠️ **按 id 按**：封面的无障碍名「当前封面」在树上有，但它不是一枚
// `act` 按得动的 `button`（它是一张照片）。`act` 的 press 对任何节点都是
// 「对它中心点按一下」——和 `tt.ClickAt` 在无头版里开满屏是同一路。
func openFullWindow(t *testing.T, client *automationClient, window int) {
	t.Helper()
	id := findNodeID(t, client, window, func(n navNode) bool {
		return n.Label == "当前封面"
	})
	if id == "" {
		t.Fatalf("树上找不到「当前封面」：\n%s", treeDump(t, client, window))
	}
	if err := automationCall(client, 610, "act", map[string]any{
		"window": window,
		"id":     id,
		"action": "press",
	}, nil); err != nil {
		t.Fatalf("按「当前封面」开满屏：%v", err)
	}
	// 满屏的判据是它自己的「收起满屏播放器」那一枚。
	if !waitForTree(t, client, window, "收起满屏播放器", 20*time.Second) {
		t.Fatalf("按了封面之后满屏没开：\n%s", treeDump(t, client, window))
	}
}
