//go:build e2e

package e2e

// 迷你播放器「歌词卷帘」的真窗口回归。
//
// # 为什么单独要这一族
//
// 界面层那一族（`internal/ui/minicurtain_*_test.go`）造的是**假外壳**：它把
// `viewMiniCurtain` 画进无头 Tester，量的是那一帧的像素和 `fakeMiniWindow`
// 记下的 `SetSize`——它断不到三件事：
//
//  1. **真窗口的那一扇 Panel 真的建出来了、真的 320×84**（`MiniWindowOptions`
//     里的 `Panel` / `Frameless` / `Vibrancy` 只走原生那一侧——无头里压根没有）。
//  2. **点封面真的把窗变高**（要跨「hit chain → `miniCurtainToggle` →
//     `miniCurtainSetHeight` → `w.SetSize` → 原生窗口 resize」一整条链——界面层
//     那一条断到 `fake.SetSize` 为止，原生那一侧有没有跟上只有真窗口量得到）。
//  3. **菜单真的挂在「···」上、选「收起歌词」真的把窗收回去**（`.Menu` 的元素
//     菜单在无头 Tester 里能选，真窗口上还可能挂在别处）。
//
// # 怎么跑
//
//	BIU_RUN_MYGO_E2E=1 go test -tags e2e ./internal/e2e/ -run TestTheMiniLyricsCurtain
//
// ⚠️ **要一个真窗口、要一块屏**：它开主窗口和迷你窗，量的是窗的 `bounds` 和
// 画面上的无障碍名。
//
// ⚠️ **它跑的是整个应用**（不是 fixture）：要验的就是这个应用的迷你窗。
//
// # ⚠️ 这一族只断「窗级」和「面板级」那两件事
//
// **不断**「歌词画得像不像目标图」——那是 `docs/agents/visual-review.md` 的活
// （无头 Tester 的画布是不透明白底，alpha 断言量不到磨砂；真窗口的画面得由视觉
// 审查去看）。这里断的是**机制和几何**——窗建出来没有、高对不对、菜单在不在。

import (
	"math"
	"os"
	"runtime"
	"testing"
	"time"
)

// miniWinTitle 是迷你窗在 `windows` 那条结果里的标题（`MiniWindowOptions.Title`）。
//
// ⚠️ **为什么用标题认窗而不用「第二个 content」**：主窗口也是 `kind=content`，
// 而 windows 列表的顺序是 `mygo.Windows()` 的顺序——它跟着「建窗先后」走，
// 先建主窗再建迷你窗的时候是这样，重启之后就不一定（`StateKey` 那一档会把
// 迷你窗的位置记进盘，但建窗顺序还是代码里的顺序）。标题是这一扇窗自己的
// 名字，不会撞。
const miniWinTitle = "布丁音乐迷你播放器"

// findMiniWindow 把「布丁音乐迷你播放器」那一扇的窗口记录拿出来。
//
// ⚠️ **它可能还没出来**：`toggleMini` 是真异步（建窗 → attach → surface.Automate
// → `a.targets[w]`），而建完到进 `windows()` 之间是另一个事件循环的事——
// 所以这里**等**，不是问一次就当没有。
func findMiniWindow(t *testing.T, client *automationClient, timeout time.Duration) (int, bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var windows struct {
			Windows []struct {
				ID     int    `json:"id"`
				Title  string `json:"title"`
				Kind   string `json:"kind"`
				Bounds struct {
					X, Y, Width, Height float64
				} `json:"bounds"`
			} `json:"windows"`
		}
		if err := automationCall(client, 400, "windows", nil, &windows); err == nil {
			for _, w := range windows.Windows {
				if w.Title == miniWinTitle && w.Kind == "content" {
					return w.ID, true
				}
			}
		}
		if time.Now().After(deadline) {
			return 0, false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// windowBounds 拿一扇窗的 `bounds`（屏幕坐标）。
func windowBounds(t *testing.T, client *automationClient, id int) (x, y, w, h float64, ok bool) {
	t.Helper()
	var windows struct {
		Windows []struct {
			ID     int `json:"id"`
			Bounds struct {
				X, Y, Width, Height float64
			} `json:"bounds"`
		} `json:"windows"`
	}
	if err := automationCall(client, 401, "windows", nil, &windows); err != nil {
		return 0, 0, 0, 0, false
	}
	for _, win := range windows.Windows {
		if win.ID == id {
			return win.Bounds.X, win.Bounds.Y, win.Bounds.Width, win.Bounds.Height, true
		}
	}
	return 0, 0, 0, 0, false
}

// pumpFrame 调一次 `capture` 逼那一扇窗画一帧。
//
// ⚠️ **为什么必须靠它**：迷你窗是 `Panel`，E2E 那一轮里它多半落在别的窗口
// 底下 ⇒ macOS 的 `occlusionState` 报「看不见」⇒ 框架的 `holdFrame`（mygo
// `ui/runtime.go`）**把所有帧都按住**——动画的弹簧一步都不走，窗高永远停在
// 84。这是**环境**，不是 bug：桌面上这一扇看得见，帧照常出。
//
// ⚠️ **一次 capture 正好一帧**：`windowHost.capture` 在「held」时调
// `rt.runFrame()`——它不走 `holdFrame` 那一道闸（它要的就是「拍一张它现在
// 的样子」），于是**每调一次 capture，弹簧被推一步**。真窗口的弹簧走
// `c.Now()`（墙钟），不是步数——所以帧与帧之间要留出真时间，不然弹簧的
// `dt` 是 0，推 100 次高度也不动。
//
// ⚠️ **它顺带把 PNG 拿回来了**（占内存），所以别在热循环里猛调——16ms 一次
// 已经够弹簧按 60fps 走了。
func pumpFrame(t *testing.T, client *automationClient, id int) {
	t.Helper()
	var out struct {
		Width, Height int
	}
	if err := automationCall(client, 420, "capture", map[string]any{"window": id}, &out); err != nil {
		t.Logf("capture（逼帧）：%v", err)
	}
}

// pumpHeightIs 等一扇窗的高度到某个值，**等的时候一扇一扇地逼帧**——收起和
// 展开都走动画，而 occluded 的窗一帧都不出（见 `pumpFrame`）。
func pumpHeightIs(t *testing.T, client *automationClient, id int, want float64, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		pumpFrame(t, client, id)
		_, _, _, h, ok := windowBounds(t, client, id)
		if ok && math.Abs(h-want) < 1 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		// ⚠️ **留出真时间**：弹簧按墙钟积分，帧推得太密的话 `dt` 趋零，
		// 高度几乎不动（见 `pumpFrame`）。
		time.Sleep(16 * time.Millisecond)
	}
}

// TestTheMiniLyricsCurtain 断三件事：迷你窗建出来是 320×84、点封面之后窗变成
// 84 + 缝 + 400、选「收起歌词」之后窗回到 84。
//
// 判据是 `bounds.height`——那是**原生那一侧**报出来的数，不是 app 想让它多高。
// 界面上「高不对」和「根本没展开」在 `bounds.height` 上是两种错：一种差几
// 像素，一种根本不到 494——这条都要管。
func TestTheMiniLyricsCurtain(t *testing.T) {
	if os.Getenv("BIU_RUN_MYGO_E2E") != "1" {
		t.Skip("设置 BIU_RUN_MYGO_E2E=1 才运行真窗口 E2E")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("这一族只在 macOS 上跑：迷你窗是 panel")
	}

	client, stop := startRealApp(t)
	defer stop()
	main := firstContentWindow(t, client)

	// 打开迷你窗（走菜单那一栏，**不是**键盘——键盘的 ⌘⇧M 会跑到应用自己那一
	// 份快捷键，而「菜单项 → onShortcut("toggleMiniMode")」这一条是用户路径）。
	err := automationCall(client, 410, "menu", map[string]any{
		"window": main,
		"path":   []string{"窗口", "显示/隐藏迷你播放器"},
	}, nil)
	if err != nil {
		t.Fatalf("点「显示/隐藏迷你播放器」：%v", err)
	}

	// 那一扇窗建出来了，而且是 320×84。
	miniID, ok := findMiniWindow(t, client, 15*time.Second)
	if !ok {
		t.Fatal("点了「显示/隐藏迷你播放器」之后没有那扇窗")
	}
	_, _, w0, h0, ok := windowBounds(t, client, miniID)
	if !ok {
		t.Fatal("问不到迷你窗的 bounds")
	}
	if math.Abs(w0-320) > 1 || math.Abs(h0-84) > 1 {
		t.Errorf("迷你窗建出来不是 320×84，而是 %.0f×%.0f", w0, h0)
	}
	// ⚠️ **先逼一帧**：occluded 的窗在建出来之后可能一帧都没画过
	//（`holdFrame`），而无障碍树是**上一帧**留下的——act 按树上
	// 的 ID 找人，树是空的就按不动。
	pumpFrame(t, client, miniID)

	// 点封面那一列（左侧 84 宽、整列可点；封面在 (6,6) 起 72×72，点正中间）。
	//
	// ⚠️ **X/Y 是「这扇窗内容里的 DIP」**（`input` 把事件直接喂给 surface.Conn），
	// 不是屏幕坐标——而 bounds 是屏幕坐标。所以要拿 bounds 找窗、再在**窗内**
	// 算坐标。
	//
	// ⚠️ **点在 (42, 42)**：那一列是 84 宽、84 高，正中间正好落在封面图里——
	// 而触发器包的是**整列**（`miniCurtainCoverKey` 那一格），不是只有那张图。
	err = automationCall(client, 411, "input", map[string]any{
		"window": miniID,
		"events": []map[string]any{
			{"type": "click", "x": 42, "y": 42},
		},
	}, nil)
	if err != nil {
		t.Fatalf("点迷你窗的封面列：%v", err)
	}

	// 展开之后窗高是 84 + 10（缝） + 400（歌词板） = 494。
	//
	// ⚠️ **判据是窗高**——动画那一段时间窗高在变，等到它停在 494 才算展开到
	// 位（`waitForTree` 里「歌词菜单」会**先**出现，那时窗还在长）。
	//
	// ⚠️ **得一边等一边逼帧**：occluded 的窗一帧都不出（`pumpFrame` 顶上那
	// 一段），弹簧不走，窗高永远停在 84。
	if !pumpHeightIs(t, client, miniID, 494, 15*time.Second) {
		_, _, _, h, _ := windowBounds(t, client, miniID)
		t.Fatalf("点封面之后迷你窗没有长到 494：bounds.height = %.0f", h)
	}

	// 「···」那一枚在歌词板上。
	//
	// ⚠️ **它进的是无障碍树**，不是 macOS 菜单栏——`.Menu(...)` 是**元素菜单**
	// （框架画的弹出层），挂在那一枚按钮上，所以 tree 里得有它。
	//
	// ⚠️ **树是上一帧的**：窗停在 494 那一帧已经画过（`pumpHeightIs` 逼的），
	// 所以这一次读树不用再逼。
	if !waitForTree(t, client, miniID, "歌词菜单", 5*time.Second) {
		t.Fatalf("展开之后歌词板上没有「···」那一枚：\n%s", treeDump(t, client, miniID))
	}

	// 再点一次封面那一列 → 窗回 84。
	//
	// ⚠️ **为什么不走「···」→「收起歌词」那一档**：`Element.Menu` 弹的是
	// **原生 NSMenu**（`rt.host.popupMenu` → `popUpMenuPositioningItem:`）——
	// 它的项**不进**这扇窗的元素无障碍树，而 automation 的 `menu` 方法只走
	// **应用菜单栏**那一条路（`chooseMenuItem`），够不到这一块弹出的 NSMenu。
	// 所以「菜单开没开、那一项在不在」在 E2E 这一层**量不到**——那一档归
	// `internal/ui/minicurtain_test.go` 的无头用例（它能直接断言 `.Menu`
	// 的项）。
	//
	// ⚠️ **再点一次封面是同一条收起路**（票面：「Esc、再次点击封面、点收起
	// 键都能收」）——而它要的是**真指针事件**：`act` 的 press 只是把
	// `clicks++`，收起的判定（`Clicked()`）在下一帧才跑，而 occluded 的窗
	// 那一帧可能永远不来；`input` 的 click 把 `rt.input` 置真，`holdFrame`
	// 放行，**当帧**就消费这个点击。
	err = automationCall(client, 413, "input", map[string]any{
		"window": miniID,
		"events": []map[string]any{
			{"type": "click", "x": 42, "y": 42},
		},
	}, nil)
	if err != nil {
		t.Fatalf("再点一次封面列：%v", err)
	}
	if !pumpHeightIs(t, client, miniID, 84, 15*time.Second) {
		_, _, _, h, _ := windowBounds(t, client, miniID)
		t.Fatalf("再点封面之后迷你窗没有收回 84：bounds.height = %.0f", h)
	}
}
