//go:build e2e

package e2e

// 刘海窗的真窗口回归。
//
// # 为什么单独要这一族
//
// 界面层那一族（`internal/ui/notch_test.go`）造的是**假外壳**：它把
// `viewNotch` 画进无头 Tester，量的是那一帧里 `notchTarget` 想画多大——
// 它断不到两件事：
//
//  1. **真窗口真的建出来、真的钉在屏顶正中**（`raiseAboveMenuBar` /
//    `pinToTop` 只走原生那一侧——无头里压根没有 NSWindow）。
//  2. **它的窗级真的比菜单栏高**（`setLevel:` 是原生侧那一个消息，
//    `windows` 那条结果里的 `level` 是系统报回来的数）。
//
// # 怎么跑
//
//	BIU_RUN_MYGO_E2E=1 go test -tags e2e ./internal/e2e/ -run TestTheNotchWindow
//
// ⚠️ **要一个真窗口、要一块屏**：它开主窗口和刘海窗，量的是 `bounds` 和
// `level`。
//
// ⚠️ **它跑的是整个应用**（不是 fixture）：要验的就是这个应用的刘海窗。
//
// # 这一族断什么、不断什么
//
// **断**：
//
//   - 菜单「窗口 ▸ 显示/隐藏刘海」把那一扇窗开出来（`windows` 里找得到）；
//   - 那一扇是**屏顶正中**的窗：y = 0（贴顶）、x 居中；
//   - 窗级 > `NSMainMenuWindowLevel`（24）——不然菜单栏一盖它就挡住了；
//   - 它在**屏幕上沿那一条**——这是「刘海窗」和「随便一个悬浮窗」的分界。
//
// **不断**：指针 hover 展开/收起。`input` 事件喂的是窗内坐标，而刘海的
// 触发区是**屏幕坐标**里「刘海那一块」——`mygo.Screen.CursorScreenPoint`
// 读的是真鼠标位置，automation 的 `input` 改不了它。展开那个动画只有真
// 鼠标走一遍才看得见，归 `docs/agents/visual-review.md` 的活。

import (
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// notchWinTitle 是刘海窗在 `windows` 结果里的标题（`NotchWindowOptions.Title`）。
//
// ⚠️ **它是前缀**：`notchLevelTitle` 会把原生窗级编进标题尾里
// （「布丁音乐刘海 level:27」），自动化协议没有 level 字段，这是把窗级
// 问出来的通道。
const notchWinTitle = "布丁音乐刘海"

// notchWinInfo 是 `windows` 里一扇窗要的那几个字段。
type notchWinInfo struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Kind    string `json:"kind"`
	Visible bool   `json:"visible"`
	Bounds  struct {
		X, Y, Width, Height float64
	} `json:"bounds"`
}

// findNotchWindow 把「布丁音乐刘海」那一扇的窗口记录拿出来（等它出来）。
func findNotchWindow(t *testing.T, client *automationClient, timeout time.Duration) (notchWinInfo, bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var windows struct {
			Windows []notchWinInfo `json:"windows"`
		}
		if err := automationCall(client, 600, "windows", nil, &windows); err == nil {
			for _, w := range windows.Windows {
				if strings.HasPrefix(w.Title, notchWinTitle) {
					return w, true
				}
			}
		}
		if time.Now().After(deadline) {
			return notchWinInfo{}, false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// notchTitleLevel 从标题尾里把窗级解出来（「布丁音乐刘海 level:27」→27）。
// 解不到就是 `notchLevelTitle` 没写上——那本身就是一条失败。
func notchTitleLevel(title string) (int, bool) {
	i := strings.LastIndex(title, "level:")
	if i < 0 {
		return 0, false
	}
	lv, err := strconv.Atoi(strings.TrimSpace(title[i+len("level:"):]))
	return lv, err == nil
}

// TestTheNotchWindow 断四件事：菜单开得出来、窗在屏顶正中、窗级压过
// 菜单栏、收合时高是「刘海那么高」那一档而不是全尺寸。
func TestTheNotchWindow(t *testing.T) {
	if os.Getenv("BIU_RUN_MYGO_E2E") != "1" {
		t.Skip("设置 BIU_RUN_MYGO_E2E=1 才运行真窗口 E2E")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("这一族只在 macOS 上跑：刘海只在 MacBook 上")
	}

	client, stop := startRealApp(t)
	defer stop()
	main := firstContentWindow(t, client)

	// 打开刘海窗（走菜单那一栏，和迷你窗那条同一用户路径）。
	err := automationCall(client, 610, "menu", map[string]any{
		"window": main,
		"path":   []string{"窗口", "显示/隐藏刘海"},
	}, nil)
	if err != nil {
		t.Fatalf("点「显示/隐藏刘海」：%v", err)
	}

	notch, ok := findNotchWindow(t, client, 15*time.Second)
	if !ok {
		var windows struct {
			Windows []notchWinInfo `json:"windows"`
		}
		_ = automationCall(client, 612, "windows", nil, &windows)
		t.Fatalf("点了「显示/隐藏刘海」之后没有那扇窗，windows=%+v", windows.Windows)
	}

	x, y, w, h := notch.Bounds.X, notch.Bounds.Y, notch.Bounds.Width, notch.Bounds.Height

	// 窗级要压过菜单栏（NSMainMenuWindowLevel = 24）：不然菜单栏一盖它就挡。
	level, ok := notchTitleLevel(notch.Title)
	if !ok {
		t.Fatalf("刘海窗标题里没有窗级（want … level:N），title=%q", notch.Title)
	}
	if level <= 24 {
		t.Errorf("刘海窗的 level 是 %d，want > 24（NSMainMenuWindowLevel）", level)
	}

	// 它得贴在屏顶（y = 0）、且在屏幕正中那一段——屏宽的一半落在
	// 窗的中线上（bounds 是屏幕坐标、以主屏左上角为原点）。
	if math.Abs(y) > 2 {
		t.Errorf("刘海窗的顶边在 y=%.0f，want 0（贴屏顶）", y)
	}
	// x 居中：窗中线 = 屏宽 / 2（primary display 的 bounds.x 是 0）。
	// 拿主屏的宽来核对——刘海窗不该歪到一边去。
	var displays struct {
		Displays []struct {
			Primary bool `json:"primary"`
			Bounds  struct {
				X, Y, Width, Height float64
			} `json:"bounds"`
		} `json:"displays"`
	}
	if err := automationCall(client, 611, "displays", nil, &displays); err == nil {
		for _, d := range displays.Displays {
			if d.Primary {
				if cx := x + w/2; math.Abs(cx-d.Bounds.Width/2) > 60 {
					t.Errorf("刘海窗中线在 x=%.0f，want 屏宽一半 %.0f", cx, d.Bounds.Width/2)
				}
			}
		}
	}
	// 窗宽是展开卡 468 + 两侧余量 60，与 mygo-cloudmusic 的刘海规格一致。
	if math.Abs(w-528) > 1 {
		t.Errorf("刘海窗的宽是 %.0f，want 528", w)
	}
	// 高是展开卡深 210 加刘海和余量后的 280。⚠️ 收合时**窗高不变**——变的是画出来的形状
	//（`notchTarget`），而 bounds 量的是窗不是形状。
	if math.Abs(h-280) > 1 {
		t.Errorf("刘海窗的高是 %.0f，want 280", h)
	}

	// 刘海轮询必须在单个可停止的调度器里运行。连续让真实应用处理
	// automation 请求，能抓住「每次 tick 又起一个永久 ticker」导致的
	// 回调扇出：修复前应用会在这一段时间内逐渐失去响应。
	for i := 0; i < 20; i++ {
		var windows struct {
			Windows []notchWinInfo `json:"windows"`
		}
		if err := automationCall(client, 620+i, "windows", nil, &windows); err != nil {
			t.Fatalf("刘海轮询期间第 %d 次 windows 请求失败：%v", i+1, err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 再从菜单把它收起来：走「显示/隐藏刘海」那一栏的关（`toggleNotchPlayer`
	// → `notchSetEnabled(false)` → `win.Hide()`——刘海窗是收不掉的第三扇，
	// 菜单关掉走的是隐藏不是销毁，见 `notch.go` 顶上那一段）。断的是**窗
	// 真的看不见了**：`windows` 那一栏的 `visible` 是原生侧报回来的——只
	// 断言「再点时窗还在」验不到它藏起来，「`visible` 掉下来」才是用户
	// 看得见的收。收的同时轮询该停、指针监听该摘、频谱那一票该收
	// （`notchSetEnabled(false)` 那一摊）。
	err = automationCall(client, 640, "menu", map[string]any{
		"window": main,
		"path":   []string{"窗口", "显示/隐藏刘海"},
	}, nil)
	if err != nil {
		t.Fatalf("第二次点「显示/隐藏刘海」：%v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		var windows struct {
			Windows []notchWinInfo `json:"windows"`
		}
		_ = automationCall(client, 641, "windows", nil, &windows)
		hidden := false
		for _, w := range windows.Windows {
			if strings.HasPrefix(w.Title, notchWinTitle) && w.ID == notch.ID {
				if !w.Visible {
					hidden = true
				}
			}
		}
		if hidden {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("「显示/隐藏刘海」关掉之后窗还可见：%+v", windows.Windows)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 藏起来之后应用本身不能死（`notchSetEnabled(false)` 把轮询 / 指针
	// 监听 / 频谱那一票收干净才算完——漏一个的话这一问会撞上失常的应用），
	// 而且它还能被同一栏再开回来（开关是往返的，不是单向门）。
	err = automationCall(client, 643, "menu", map[string]any{
		"window": main,
		"path":   []string{"窗口", "显示/隐藏刘海"},
	}, nil)
	if err != nil {
		t.Fatalf("第三次点「显示/隐藏刘海」：%v", err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		var windows struct {
			Windows []notchWinInfo `json:"windows"`
		}
		_ = automationCall(client, 644, "windows", nil, &windows)
		back := false
		for _, w := range windows.Windows {
			if strings.HasPrefix(w.Title, notchWinTitle) && w.ID == notch.ID && w.Visible {
				back = true
			}
		}
		if back {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("再开一次之后刘海窗没回来：%+v", windows.Windows)
		}
		time.Sleep(100 * time.Millisecond)
	}

	var after struct {
		Windows []notchWinInfo `json:"windows"`
	}
	if err := automationCall(client, 642, "windows", nil, &after); err != nil {
		t.Fatalf("刘海窗关掉之后 automation 不应答了：%v", err)
	}
}
