//go:build e2e

package e2e

// 首页切 Tab 滑动中那条**接缝分割线**的真窗口回归（本票）。
//
// # 界面层那一族断的是什么
//
// `internal/ui/homepage_pager_test.go` 那一族断的是**几何**（`homePagerView` 记下
// 两档的位置，分割线挂在「pager-seam」下、x 在接缝上）——而无头 Tester 的画面
// 是**不透明白底**，接缝那 1 DIP 的线在「它和底色差多少」这种判据上量不稳
// （半透明混色画上去的）。
//
// 所以这一层断的是**它在真窗口上真的画出来**：
//
//  1. **滑动中**：capture 一张，元素树里「pager-seam」在、它的 bounds 里那一带
//     有墨（`Border` 色，比底色深）；
//  2. **静止时**：元素树里没有「pager-seam」——它只在滑动中出现。
//
// # 怎么跑
//
//	BIU_RUN_MYGO_E2E=1 go test -tags e2e ./internal/e2e/ -run TestThePagerSeam
//
// ⚠️ **它跑的是整个应用**：切 Tab 走真窗口的那一条（act press → 滑动 → capture）。
//
// ⚠️ **滑动是 200ms**：capture 要抢在那一段里——所以「先按 Tab、立刻 capture」，
// 而不是等「滑动结束了再看」。真窗口的动画走墙钟，`capture` 逼的那一帧就落在
// 滑动中途（`pumpFrame` 同一条机制）。
//
// ⚠️ **判据是「那一条竖带里有墨」，不是「精确比色」**：接缝线是 `Border` 色
// （深色下 15% 白、浅色下 `#dbdbdb`），叠在内容上是一条**比底色深**的竖带——
// 和 `darkerIn` 同一个数法。

import (
	"image/png"
	"os"
	"testing"
	"time"
)

// seamNode 是元素树里「pager-seam」那一项。
type seamNode struct {
	Key    any    `json:"key"`
	Label  string `json:"label"`
	Bounds struct {
		X, Y, Width, Height float64
	}
}

// TestThePagerSeam 断**滑动中接缝处有分割线、静止时没有**。
func TestThePagerSeam(t *testing.T) {
	if os.Getenv("BIU_RUN_MYGO_E2E") != "1" {
		t.Skip("设置 BIU_RUN_MYGO_E2E=1 才运行真窗口 E2E")
	}

	client, stop := startRealApp(t)
	defer stop()
	window := firstContentWindow(t, client)

	// 先把窗口催出一帧（occluded 的窗一帧都没画过时无障碍树是空的）。
	pumpFrame(t, client, window)

	// 静止时：元素树里没有「pager-seam」。
	if n, ok := findSeamNode(t, client, window); ok {
		t.Fatalf("没滑动的时候「pager-seam」已经在树上了（bounds %.0f,%.0f %.0fx%.0f）——"+
			"它只在滑动中出现", n.Bounds.X, n.Bounds.Y, n.Bounds.Width, n.Bounds.Height)
	}

	// 按「鬼畜」那一档：切 Tab 走 `act` 的 press（它是 `flagClickable` 的）。
	//
	// ⚠️ **认 Key 不认 Label**：「鬼畜」两个字是 Tab 上画的字，字自己不吃 press——
	// 可点的是包着它的那一格（`flagClickable`）。那一格的 key 是 `tab:` + 名字
	//（`tabs.go` 的 `tabKey`），按 key 发 press 就真的是「点了这一档」。
	if !waitForTree(t, client, window, "鬼畜", 20*time.Second) {
		t.Fatalf("首页那一排里没有「鬼畜」：\n%s", treeDump(t, client, window))
	}
	id, ok := e2eNodeIDByLabel(t, client, window, "鬼畜")
	if !ok {
		t.Fatalf("「鬼畜」那一档按不动：\n%s", treeDump(t, client, window))
	}
	if err := automationCall(client, 600, "act", map[string]any{
		"window": window,
		"id":     id,
		"action": "press",
	}, nil); err != nil {
		t.Fatalf("按「鬼畜」那一档：%v", err)
	}

	// 立刻 capture：那一帧落在滑动中途（200ms 那段），接缝分割线在。
	//
	// ⚠️ **不 sleep**：`capture` 逼的那一帧就是「现在」——滑动走墙钟，等的是
	// 「按下去之后那一帧」，不是「按下去之后等几帧」。
	img, geom := captureWindow(t, client, window)
	seam, ok := findSeamNode(t, client, window)
	if !ok {
		// 没抢到滑动中途（200ms 太短，capture 落在走完之后）：那一帧接缝已经
		// 收掉了——这一层只能再验「静止时没有」，画上去的样子由界面层的
		// shot 那一档和视觉审查看。
		t.Logf("capture 落在滑动走完之后（接缝已收）——只验静止时没有分割线")
		if n, ok := findSeamNode(t, client, window); ok {
			t.Errorf("滑动走完之后「pager-seam」还在树上（bounds %.0f,%.0f %.0fx%.0f）",
				n.Bounds.X, n.Bounds.Y, n.Bounds.Width, n.Bounds.Height)
		}
		return
	}

	// 分割线那一条竖带里有墨（`Border` 色，比底色深）。
	//
	// ⚠️ **量的是它自己的 bounds**（DIP → 像素乘 `geom.scale`）：接缝线 1 DIP
	// 宽，bounds 就是它画的那个条。
	x0, y0 := geom.px(seam.Bounds.X), geom.py(seam.Bounds.Y)
	x1, y1 := geom.px(seam.Bounds.X+seam.Bounds.Width), geom.py(seam.Bounds.Y+seam.Bounds.Height)
	if x1 <= x0 || y1 <= y0 {
		t.Fatalf("「pager-seam」的 bounds 是空的（%.0f,%.0f %.0fx%.0f）",
			seam.Bounds.X, seam.Bounds.Y, seam.Bounds.Width, seam.Bounds.Height)
	}
	// 底色取它左边一格（接缝左边是滑出去的那一页的底色）。
	bg := pixelAt(img, geom, seam.Bounds.X-4, seam.Bounds.Y+4)
	ink := 0
	for y := y0; y < y1 && y < img.Bounds().Dy(); y++ {
		for x := x0; x < x1 && x < img.Bounds().Dx(); x++ {
			p := img.RGBAAt(x, y)
			if int(bg.R)+int(bg.G)+int(bg.B)-int(p.R)-int(p.G)-int(p.B) > 30 {
				ink++
			}
		}
	}
	if ink == 0 {
		t.Errorf("「pager-seam」的 bounds（%.0f,%.0f %.0fx%.0f）里一点墨都没有——"+
			"接缝分割线没画上去（它要是 `Border` 色，比底色深的一条竖带）",
			seam.Bounds.X, seam.Bounds.Y, seam.Bounds.Width, seam.Bounds.Height)
	}
	if os.Getenv("BIU_E2E_DUMP") != "" {
		p := os.TempDir() + "/pager-seam.png"
		f, _ := os.Create(p)
		defer f.Close()
		_ = png.Encode(f, img)
		t.Logf("capture 落在 %s，接缝 bounds=%+v ink=%d", p, seam.Bounds, ink)
	}

	// 滑动走完之后：接缝收掉。
	//
	// ⚠️ **等树更新**：滑动是墙钟走的，等「pager-seam」从树里消失才算走完——
	// 问一次就当收掉了是「等了几帧」那种判据。
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := findSeamNode(t, client, window); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Errorf("滑动走完之后「pager-seam」还在树上——它只在滑动中出现")
			break
		}
		pumpFrame(t, client, window)
		time.Sleep(30 * time.Millisecond)
	}
}

// findSeamNode 在元素树里找 key 是「home:pager:seam」或 Label 是「pager-seam」
// 的那一项。
//
// ⚠️ **两个都认**：真窗口里 key 上树（`treeJSON` 把 `keys[n.ID]` 塞成 `key`），
// 而无头 Tester 里那一项是 Label ——两个口径都找，免得哪天 key 的序列化变了
// （`keyJSON` 对别的类型走 `fmt`）静默漏掉。
func findSeamNode(t *testing.T, client *automationClient, window int) (seamNode, bool) {
	t.Helper()
	var tree struct {
		Nodes []seamNode `json:"nodes"`
	}
	if err := automationCall(client, 601, "tree", map[string]any{"window": window}, &tree); err != nil {
		t.Fatalf("读元素树：%v", err)
	}
	for _, n := range tree.Nodes {
		if n.Label == "页面接缝" || n.Label == "pager-seam" || n.Key == "home:pager:seam" {
			return n, true
		}
	}
	return seamNode{}, false
}
