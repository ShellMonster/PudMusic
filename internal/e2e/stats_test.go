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
	"bytes"
	"encoding/base64"
	"image"
	"image/draw"
	"image/png"
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

	// 入口在**侧栏**那一项上（`side-item:/stats`）。
	//
	// ⚠️ **顶栏那一枚已经没了**：顶栏改版（`navbar.go`）把「听歌统计」图标删了，
	// 入口挪进侧栏（`router.go` 的 `InMenu: true`，见 `sideMenuItem` 的
	// `side-item:` + `rt.Match`）。侧栏那一项的无障碍名同样是「听歌统计」
	// （`Label(rt.Title)`），所以「找得到那一枚」这一步不用换字。
	if !waitForTree(t, client, window, "听歌统计", 20*time.Second) {
		t.Fatalf("侧栏上没有「听歌统计」那一项：\n%s", treeDump(t, client, window))
	}

	// 按它。
	//
	// ⚠️ **按 Key 按，不是按无障碍名按**：automation 的 `act` 只认 Key 或
	// id（见框架的 `plugins/automation/protocol.go`）。侧栏那一项的 Key 是
	// `side-item:/stats`（`sideMenuItem`：`"side-item:" + rt.Match`）。
	err := automationCall(client, 300, "act", map[string]any{
		"window": window,
		"key":    "side-item:/stats",
		"action": "press",
	}, nil)
	if err != nil {
		t.Fatalf("按侧栏那项「听歌统计」：%v", err)
	}

	// 那一页画出来了。
	//
	// ⚠️ **等的是「那一页自己的东西」而不是「听歌统计」这四个字**——侧栏那一项
	// 的**无障碍名**也是「听歌统计」，而它**在每一页都在**。用那四个字当判据
	// 的话，按完它立刻就成立——于是「有没有真的换页」根本验不到。
	//
	// ⚠️ **「还没有听歌记录」是这一页独有的**：第一次运行（还没听过歌）时
	// 画的就是它（见 stats_view.go 的空态分支）。
	if !waitForTree(t, client, window, "还没有听歌记录", 20*time.Second) {
		t.Fatalf("按了那一项之后没有进统计页：\n%s", treeDump(t, client, window))
	}
	// ⚠️ **页头那行小字也是这一页独有的**：「最近 7 天 · 只统计在本机的收听」
	// 只在统计页的 `PageHead` 里画（stats_view.go），空态也有它——它到了说明
	// 那一页的骨架真的画出来了。
	//
	// （日 / 周 / 月 / 全部那排 Tab 随统计页改版删了——原型只留最近 7 天，
	// 见 stats_view.go 顶上那段。）
	if !treeHasLabel(t, client, window, "最近 7 天 · 只统计在本机的收听") {
		t.Errorf("统计页上没有页头小字：\n%s", treeDump(t, client, window))
	}
}

// TestTheStatsEntryHovers 断**真窗口里指针落到侧栏那一项上，悬停反馈真的生效**。
//
// # 它修的是什么
//
// 顶栏那一枚原来把 `.Key("nav-stats")` 链在 `iconKey(...)` **返回之后**——
// `Element.Key` 是**事后 rekey**（换元素 id），而悬停反馈在**构造时**就按旧
// 位置 id 去对命中链，于是真窗口里指针落上去既不换主色、也没有淡底。顶栏改版
// 之后那一枚挪进了**侧栏**（`side-item:/stats`），悬停是 `sideMenuItem` 自己
// 那一套（`b.Hovered()` → `p.Hover` 淡底 + `p.Text` 字色），同一份「落上去要变」
// 的验收仍然适用。
//
// 界面层那一族（`TestTheStatsEntryLightsUpOnHover`）断的是像素（主色），而**真
// 窗口里**这一条走 automation：先按 `key:"side-item:/stats"` 在 tree 里读出那一项的
// bounds，用 `input` 发 `pointerMove` 到它的中心，再 `capture` 前后两张 PNG
// 比对那一块——悬停生效的话那一块必然变（图形变主色、底下多一层淡底）。
//
// ⚠️ **比对的是那一枚自己的 bounds，不是全屏**：别处（比如指针移过的路径上）
// 也可能有像素变，那一枚自己变了才叫「悬停生效」。
//
// ⚠️ **要一个真窗口**：跑法和上面那条一样，
// `BIU_RUN_MYGO_E2E=1 go test -tags e2e ./internal/e2e/ -run TestTheStatsEntryHovers`。
func TestTheStatsEntryHovers(t *testing.T) {
	if os.Getenv("BIU_RUN_MYGO_E2E") != "1" {
		t.Skip("设置 BIU_RUN_MYGO_E2E=1 才运行真窗口 E2E")
	}

	client, stop := startRealApp(t)
	defer stop()
	window := firstContentWindow(t, client)

	// 那一项在侧栏上，按 Key 找到它并读出 bounds。
	if !waitForTree(t, client, window, "听歌统计", 20*time.Second) {
		t.Fatalf("侧栏上没有「听歌统计」那一项：\n%s", treeDump(t, client, window))
	}
	type node struct {
		Key    any `json:"key"`
		Bounds struct {
			X, Y, Width, Height float64
		} `json:"bounds"`
	}
	var tree struct {
		Nodes []node `json:"nodes"`
	}
	if err := automationCall(client, 301, "tree", map[string]any{"window": window}, &tree); err != nil {
		t.Fatalf("读元素树：%v", err)
	}
	var box *node
	for i := range tree.Nodes {
		if tree.Nodes[i].Key == "side-item:/stats" {
			box = &tree.Nodes[i]
			break
		}
	}
	if box == nil || box.Bounds.Width == 0 {
		t.Fatalf("元素树里没有 key=side-item:/stats 的那一项：\n%s", treeDump(t, client, window))
	}

	// ⚠️ **坐标是两个口径**：树的 `bounds` 是**内容区 DIP**，`capture` 的 PNG
	// 是**像素**——Retina 上 1 DIP = 2 像素，直接把 DIP 当像素用，比对矩形会
	// 落在那一枚右下方两倍处（P1，非 1× 屏上必然假红）。倍率从 `windows` 的
	// `contentBounds`（DIP）和 PNG 的宽（像素）算出来，和 counts_test.go 的
	// `captureGeom`/`captureWindow` 同一份换算。
	var wins struct {
		Windows []struct {
			ID            int `json:"id"`
			ContentBounds struct {
				Width float64 `json:"width"`
			} `json:"contentBounds"`
		} `json:"windows"`
	}
	if err := automationCall(client, 302, "windows", nil, &wins); err != nil {
		t.Fatalf("问窗口的 contentBounds：%v", err)
	}
	var dipW float64
	for _, w := range wins.Windows {
		if w.ID == window {
			dipW = w.ContentBounds.Width
		}
	}
	if dipW <= 0 {
		t.Fatalf("窗口 %d 的 contentBounds 问不到：%+v", window, wins.Windows)
	}

	// captureShot 截一张内容区 PNG，返回解码后的 RGBA 和「DIP → 像素」的倍率。
	//
	// ⚠️ **`img.(*image.RGBA)` 不许硬断言**：解码路径哪天换了底层类型（NRGBA、
	// Paletted），硬断言就是 panic 而不是报错。非 RGBA 时 `draw.Draw` 拷一份，
	// 和 counts_test.go 同款。
	captureShot := func() (*image.RGBA, float64) {
		var shot struct {
			PNG string `json:"png"`
		}
		if err := automationCall(client, 303, "capture", map[string]any{"window": window}, &shot); err != nil {
			t.Fatalf("截窗口：%v", err)
		}
		raw, err := base64.StdEncoding.DecodeString(shot.PNG)
		if err != nil {
			t.Fatalf("capture 的 PNG 不是 base64：%v", err)
		}
		img, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("capture 的 PNG 解不开：%v", err)
		}
		b := img.Bounds()
		rgba, ok := img.(*image.RGBA)
		if !ok {
			rgba = image.NewRGBA(b)
			draw.Draw(rgba, b, img, b.Min, draw.Src)
		}
		return rgba, float64(b.Dx()) / dipW
	}

	// 先把指针挪到一个与那一枚无关的地方，截一张「没悬停」的底图。
	if err := automationCall(client, 304, "input", map[string]any{
		"window": window,
		"events": []map[string]any{
			{"type": "pointerMove", "x": 40, "y": 400},
		},
	}, nil); err != nil {
		t.Fatalf("把指针挪开：%v", err)
	}
	_ = automationCall(client, 305, "idle", map[string]any{"window": window, "timeout": 2000}, nil)
	before, scale := captureShot()

	// 指针落到那一枚的中心，再截一张。
	cx := box.Bounds.X + box.Bounds.Width/2
	cy := box.Bounds.Y + box.Bounds.Height/2
	if err := automationCall(client, 306, "input", map[string]any{
		"window": window,
		"events": []map[string]any{
			{"type": "pointerMove", "x": cx, "y": cy},
		},
	}, nil); err != nil {
		t.Fatalf("把指针落到那一枚上：%v", err)
	}
	_ = automationCall(client, 307, "idle", map[string]any{"window": window, "timeout": 2000}, nil)
	after, _ := captureShot()

	// 那一枚的 bounds（乘上 scale 换成像素）里要有像素真的变了（悬停淡底 +
	// 图形换色，二者至少其一会把那一块改掉）。容差给 4 个通道级，躲开抗锯齿
	// 那一两档。
	x0, y0 := int(box.Bounds.X*scale), int(box.Bounds.Y*scale)
	x1, y1 := int((box.Bounds.X+box.Bounds.Width)*scale), int((box.Bounds.Y+box.Bounds.Height)*scale)
	changed := 0
	for y := y0; y < y1 && y < after.Rect.Max.Y; y++ {
		for x := x0; x < x1 && x < after.Rect.Max.X; x++ {
			a, b := before.RGBAAt(x, y), after.RGBAAt(x, y)
			d := func(p, q uint8) int {
				if p > q {
					return int(p) - int(q)
				}
				return int(q) - int(p)
			}
			if max(d(a.R, b.R), max(d(a.G, b.G), d(a.B, b.B))) > 4 {
				changed++
			}
		}
	}
	if changed == 0 {
		t.Errorf("指针落到 side-item:/stats（bounds %.0f,%.0f %.0fx%.0f）上之后，那一块一个像素都没变——"+
			"悬停反馈没生效",
			box.Bounds.X, box.Bounds.Y, box.Bounds.Width, box.Bounds.Height)
	}
}
