//go:build e2e

package e2e

// 「我的关注」页头分组下拉的真窗口回归（票 #367 的那一档）。
//
// # 为什么要有这一族
//
// 这一票把左边 200 宽的分组栏收成页头右上角的一枚下拉，而界面层那一族
// （`internal/ui/follow_test.go` / `ticket213_test.go`）造的是**假外壳**——
// 它断不到两件事：
//
//  1. **下拉的弹层在真窗口上真的摆开**（弹层是**覆盖层**，无头 Tester 那一层
//     和真窗口的 hit chain / 覆盖层合成是两条路——`SelectBase` 的 `Popup` 走
//     `coreOverlay`，真窗口上它盖在内容区上头还是压在底下，只有真窗口验得了）。
//  2. **Esc 真的把弹层收掉**（`OverlayShortcut` 那条路在无头里是一条 `Key`
//     调用，在真窗口上是「覆盖层先吃这个键」的一整条分发）。
//
// # 怎么跑
//
//	BIU_RUN_MYGO_E2E=1 go test -tags e2e ./internal/e2e/ -run TestTheFollowGroupSelect
//
// ⚠️ **要一个真窗口**：它开一个窗口并驱动它，所以本机得能开窗口。
//
// ⚠️ **它跑的是整个应用**（不是 fixture）：要验的就是这个应用的那一枚下拉。
//
// ⚠️ **它只断「弹层开得了、Esc 收得掉」这两件窗级的事**——「哪一项摆什么
// 颜色、分组名长怎么截、三态怎么画」都归界面层那一族和视觉审查，这里不碰
// （理由同 `minicurtain_test.go` 顶上那一段：E2E 断机制和几何，不断像不像）。

import (
	"os"
	"testing"
	"time"
)

// TestTheFollowGroupSelect 断**页头那枚「选择关注分组」在真窗口上点得开、
// Esc 收得掉**。
//
// # 为什么判据是「那一枚 Label 在不在」而不是「弹层的某一项在不在」
//
// 弹层里的项是**动态**的（「全部关注(N)」/ 分组名 / 「还没有分组」/「正在
// 加载分组…」/「分组没拉出来」——哪一条出现取决于 B 站那一拉回什么），而
// E2E 起的是**真应用**：种的是假凭据（见 `seedFakeSession`），分组那一拉真
// 发出去真被拒，弹层里画的是「分组没拉出来」——那也是「弹层摆开了」。所以
// **开不开**的判据只用「弹层里有东西出现」，而**收没收掉**用「弹层独有的
// 那几句都不在了」。
//
// ⚠️ **「选择关注分组」那枚 Label 本身每一帧都在**（它是触发器的无障碍名，
// 弹层收着时也在），所以不能拿「它出现了」当弹层开了——得拿**弹层里独有的
// 那句话**（三态里的任何一句都算）。
func TestTheFollowGroupSelect(t *testing.T) {
	if os.Getenv("BIU_RUN_MYGO_E2E") != "1" {
		t.Skip("设置 BIU_RUN_MYGO_E2E=1 才运行真窗口 E2E")
	}

	// ⚠️ **要先种一份假登录态**：「我的关注」标了 `HideInMenuWhenLoggedOut`，
	// 没凭据时侧栏里根本没有它（见 `startRealAppWith` 那一段）。
	client, stop := startRealAppWith(t, true)
	defer stop()
	window := firstContentWindow(t, client)

	// ⚠️ **先把窗口催出一帧**：`act` 按 key 找人走的是**上一帧留下的元素树**
	// （`protocol.go` 的 act → `d.tree`），而 occluded 的窗一帧都没画过时那棵
	// 树是空的——问出来的「没有那一项」其实是「还没有树」。`pumpFrame` 逼一帧
	// 再走（理由同 `minicurtain_test.go` 那一处）。
	pumpFrame(t, client, window)

	// 先走到「我的关注」：侧栏那一项的 key 是 `side-item:/follow`
	//（`sidebar.go` 的 `side-item:` + `rt.Match`）。
	//
	// ⚠️ **用 `act` 的 press 按，不用 `input` 的 click**：侧栏那一项是整个
	// 可点的一块，`press` 走无障碍那条路——它比「点在某个坐标上」更贴近
	// 「用户按了这一项」，而且不随窗口尺寸和侧栏折叠态漂。
	//
	// ⚠️ **等的是「页头那一枚出现了」而不是「侧栏那一项被点了」**：换路由
	// 是异步的，那一帧可能还没画——`waitForTree` 是等「出现了」，不是
	// 「等了几帧」（`docs/agents/testing.md` 的判据）。
	// ⚠️ **按 id 走 `act` 的 press**：`side-item:` 那一格自己没进树（树里只有
	// 它里面那枚按钮的 Label），而 `Bounds` 在这个应用里全是零（accessibility
	// 不报位置），坐标点不着——所以只能按 **id**（`act` 认 id 不认 Label）。
	// 而那一枚按钮的 `actions` 里有 `press`（它是 `flagClickable` 的），
	// 按 id 发过去就真的是「按了这一项」。
	id, ok := e2eNodeIDByLabel(t, client, window, "我的关注")
	if !ok {
		t.Fatalf("侧栏里没有「我的关注」那一项：\n%s", treeDump(t, client, window))
	}
	err := automationCall(client, 500, "act", map[string]any{
		"window": window,
		"id":     id,
		"action": "press",
	}, nil)
	if err != nil {
		t.Fatalf("按侧栏「我的关注」：%v", err)
	}
	if !waitForTree(t, client, window, "选择关注分组", 20*time.Second) {
		t.Fatalf("进关注页之后没有「选择关注分组」那一枚：\n%s", treeDump(t, client, window))
	}

	// 点开下拉。
	//
	// ⚠️ **触发器也是按 id 发 press**：它是 `SelectBase` 的 Trigger（
	// `flagClickable`，`actions` 里有 `press`），挂在「选择关注分组」那个
	// Label 上（`follow.go` 的 `trigger.Label(...)`）——和侧栏那一项同一
	// 条路。
	if !followSelectOpen(t, client, window) {
		t.Fatalf("点开「选择关注分组」之后弹层没摆开：\n%s", treeDump(t, client, window))
	}

	// Esc 收掉它。
	//
	// ⚠️ **Esc 走 `input` 的 key**（`OverlayShortcut` 那条路），不走菜单：
	// 「收弹层」是覆盖层自己的事，不是应用菜单的事。
	err = automationCall(client, 501, "input", map[string]any{
		"window": window,
		"events": []map[string]any{
			{"type": "key", "key": "Escape"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("按 Esc：%v", err)
	}
	// 弹层里那句话没了才算收掉（等「消失」也要等，不能问一次就当收了）。
	if !waitForTreeGone(t, client, window, "选择关注分组", 15*time.Second, "全部关注", "正在加载分组", "分组没拉出来", "还没有分组") {
		t.Fatalf("按了 Esc 之后弹层没收掉：\n%s", treeDump(t, client, window))
	}
}

// followSelectOpen 点开页头那枚「选择关注分组」，等弹层里出现**任何一句
// 弹层独有的字**（三态里的哪一句都行——假凭据下分组那拉真发真败，画的是
// 「分组没拉出来」，那也算弹层摆开了）。
func followSelectOpen(t *testing.T, client *automationClient, window int) bool {
	t.Helper()
	id, ok := e2eNodeIDByLabel(t, client, window, "选择关注分组")
	if !ok {
		t.Logf("树里没有按得动的「选择关注分组」")
		return false
	}
	err := automationCall(client, 502, "act", map[string]any{
		"window": window,
		"id":     id,
		"action": "press",
	}, nil)
	if err != nil {
		t.Logf("按「选择关注分组」那一枚：%v", err)
		return false
	}
	// 弹层里独有的任何一句：三态里的哪一句都算「摆开了」。
	for _, want := range []string{"全部关注", "正在加载分组", "分组没拉出来", "还没有分组"} {
		if waitForTree(t, client, window, want, 8*time.Second) {
			return true
		}
	}
	return false
}

// e2eNode 是元素树里的一项（只取这一族用得上的那几个字段）。
type e2eNode struct {
	ID      any      `json:"id"`
	Parent  any      `json:"parent"`
	Label   string   `json:"label"`
	Actions []string `json:"actions"`
}

func hasE2EAction(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// e2eNodeIDByLabel 是「树里写着这个词、且按得动」的那一项的 id。
//
// ⚠️ **认 id 不认 Label**：`act` 只接 id 或 key，而「选择关注分组」「我的
// 关注」那几格自己没上 Key（`side-item:` 那格是容器，不进树）——Label 是
// 名字、key 是钥匙，名字对得上不等于钥匙开得了。按 id 是唯一能按到那一枚的
// 路。
//
// ⚠️ **为什么要挑 `actions` 里有 `press` 的**：Label 是画上去的字，字自己
// 也在树里占一项（`Text`），而**字不吃 press**——给它发 press 等于按了一张
// 图片。可点的是**外面那一格**（`flagClickable` 的那一个），它的 `actions`
// 里才有 `press`。
//
// ⚠️ **`Parent` 记的是父的 id，不是下标**（`protocol.go` 塞的是
// `t.Nodes[n.Parent].ID`）——往父链上找要**按 id 找父**，别把 `Parent`
// 当下标用。
func e2eNodeIDByLabel(t *testing.T, client *automationClient, window int, label string) (any, bool) {
	t.Helper()
	var tree struct {
		Nodes []e2eNode `json:"nodes"`
	}
	if err := automationCall(client, 503, "tree", map[string]any{"window": window}, &tree); err != nil {
		return nil, false
	}
	byID := make(map[any]int, len(tree.Nodes))
	for i, n := range tree.Nodes {
		byID[n.ID] = i
	}
	for _, n := range tree.Nodes {
		if n.Label != label {
			continue
		}
		// 理想情况：写着这个词的那一项自己就按得动（按钮把 Label 带在身上）。
		if hasE2EAction(n.Actions, "press") {
			return n.ID, true
		}
		// 不然它是一个 Text——往父链上找第一个有 press 的，那才是「按这里」。
		for p := n.Parent; p != nil; {
			pi, ok := byID[p]
			if !ok {
				break
			}
			pn := tree.Nodes[pi]
			if hasE2EAction(pn.Actions, "press") {
				return pn.ID, true
			}
			p = pn.Parent
		}
	}
	return nil, false
}

// waitForTreeGone 等那几句**弹层独有的字**都从树里消失（触发器自己的
// Label 不算——它收着弹层时也在）。
//
// ⚠️ **它和 `waitForTree` 是同一件事的反面**：Esc 是异步的，弹层收掉是
// 下一帧的事——问一次就断定收没收到，是「等了几帧」那种判据。
func waitForTreeGone(t *testing.T, client *automationClient, window int, keep string, timeout time.Duration, gone ...string) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var tree struct {
			Nodes []struct {
				Label string `json:"label"`
			} `json:"nodes"`
		}
		if err := automationCall(client, 504, "tree", map[string]any{"window": window}, &tree); err != nil {
			return false
		}
		labels := make(map[string]bool, len(tree.Nodes))
		for _, n := range tree.Nodes {
			labels[n.Label] = true
		}
		// 触发器自己还在（它是「收起那一格」，收着弹层时也在）。
		if !labels[keep] {
			return false
		}
		anyGone := false
		for _, g := range gone {
			if labels[g] {
				anyGone = true
				break
			}
		}
		if !anyGone {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}
