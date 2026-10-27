// 迷你播放器的**多显示器定位**那一组（票 #555）。
//
// # 这一组量的是什么
//
// 「迷你窗该在哪儿」被拆成了**纯函数**（`minidisplays.go`）：给它一堆显示器、
// 一个指针位置、一个窗的矩形，它给出一个合法的矩形。于是：
//
//   - 「从任意显示器打开迷你窗，首次落在指针所在显示器」⇒ 量纯函数；
//   - 「跨屏拖动后不会按主屏 WorkArea 错误夹边」⇒ 量纯函数 + 生产那一档
//     用的那个匹配器（`miniDisplayFor`）；
//   - 「显示器移除 / 改分辨率后窗口自动回到可用区域」⇒ 量**生产那条路**
//     （`miniOnDisplaysChanged` / `miniOnWindowMoved`），而它跑在**假窗**上。
//
// ⚠️ **真双屏 / 插拔 / 旋转只有真机能验**：那一档列在 `miniplayer_test.go`
// 末尾「真窗口才量得到」那一节里，**不拿这里的无头测试顶替**。

package ui

import (
	"testing"

	"github.com/egoist/mygo"
)

// miniTestDisplays 是「一块主屏 + 一块在左边的副屏」那一组。
//
// ⚠️ **副屏的坐标是负的**（它在主屏的左边）：框架那一侧的坐标是「以主屏
// 左上角为原点」的全局坐标（`mygo/internal/darwin/window.go:1377` 的
// `rectFromMac`），而第二块屏在主屏左边时它的 X 就是负的——**负坐标那一档
// 是这一票必须覆盖的**（它是最容易把「最近的那块屏」算错的一种摆法）。
func miniTestDisplays() []mygo.Display {
	return []mygo.Display{
		{
			ID: 1, Label: "主屏", Primary: true,
			Bounds:   mygo.Rectangle{X: 0, Y: 0, Width: 1920, Height: 1080},
			WorkArea: mygo.Rectangle{X: 0, Y: 25, Width: 1920, Height: 1055},
		},
		{
			ID: 2, Label: "副屏",
			Bounds:   mygo.Rectangle{X: -2560, Y: -200, Width: 2560, Height: 1440},
			WorkArea: mygo.Rectangle{X: -2560, Y: -200, Width: 2560, Height: 1400},
		},
	}
}

// miniRectInside 报 r 有没有整个落在 work 里面。
func miniRectInside(r, work mygo.Rectangle) bool {
	return r.X >= work.X && r.Y >= work.Y &&
		r.X+r.Width <= work.X+work.Width &&
		r.Y+r.Height <= work.Y+work.Height
}

// TestTheMiniWindowOpensOnTheDisplayUnderTheCursor 盯「第一次落在指针所在的
// 那块屏」。
//
// ⚠️ **这一条是这一票的头**：以前迷你窗第一次开出来停的是**主显示器**的
// 右上角——于是在第二块屏上点「迷你」的人，窗开在他**看不见**的那块屏上，
// 而症状和「点了没反应」一模一样。
func TestTheMiniWindowOpensOnTheDisplayUnderTheCursor(t *testing.T) {
	displays := miniTestDisplays()
	size := mygo.Rectangle{Width: miniWidth, Height: miniHeight}

	// ① 指针在主屏上 ⇒ 落在主屏的右上角。
	plan := miniPlanFirst(displays, mygo.Point{X: 900, Y: 400}, size)
	if plan.Display.ID != 1 {
		t.Fatalf("指针在主屏上，而窗落在了 ID=%d 那块屏上", plan.Display.ID)
	}
	primary := displays[0].WorkArea
	want := miniCornerRect(primary, size)
	if plan.Rect != want {
		t.Errorf("落在 %+v，want %+v（主屏工作区的右上角）", plan.Rect, want)
	}
	if !miniRectInside(plan.Rect, primary) {
		t.Errorf("窗有一半在工作区外面：%+v 不在 %+v 里", plan.Rect, primary)
	}

	// ② 指针在**副屏**上（负坐标）⇒ 落在副屏的右上角，**不是主屏**。
	plan = miniPlanFirst(displays, mygo.Point{X: -2000, Y: 100}, size)
	if plan.Display.ID != 2 {
		t.Fatalf("指针在副屏上，而窗落在了 ID=%d 那块屏上", plan.Display.ID)
	}
	secondary := displays[1].WorkArea
	if !miniRectInside(plan.Rect, secondary) {
		t.Errorf("窗落在 %+v，不在副屏的工作区 %+v 里", plan.Rect, secondary)
	}
	if plan.Rect.X >= 0 {
		t.Errorf("窗的 x=%d 不是负的：副屏在主屏左边，它的坐标是负的", plan.Rect.X)
	}
	// ⚠️ **工作区必须跟着那块屏走**：卷帘往哪儿长、长多长量的是它。
	if plan.Work != secondary {
		t.Errorf("工作区是 %+v，want %+v（副屏那一块）", plan.Work, secondary)
	}
}

// TestTheMiniWindowKeepsTheWorkAreaOfTheScreenItIsOn 盯「跨屏拖动之后按**那
// 一块屏**的工作区算」。
//
// 🔴 **这一条是「按主屏 WorkArea 错误夹边」那一半**：框架那一版
// `mygo.Screen.DisplayMatching` 在「一块屏都不重叠」时返回的是**第一块**
// （也就是主屏）——而窗在第二块屏上的时候，卷帘于是按**主屏**的工作区算高
// 度，症状是「窗拖过去了，点封面却说什么也不发生（那儿放不下）」。
func TestTheMiniWindowKeepsTheWorkAreaOfTheScreenItIsOn(t *testing.T) {
	displays := miniTestDisplays()
	// 窗在副屏上（负坐标）。
	cur := mygo.Rectangle{X: -2400, Y: 300, Width: miniWidth, Height: miniHeight}

	plan := miniPlanKeep(displays, cur)
	if plan.Display.ID != 2 {
		t.Fatalf("窗在副屏上，而它被算到了 ID=%d 那块屏上", plan.Display.ID)
	}
	if plan.Work != displays[1].WorkArea {
		t.Errorf("工作区是 %+v，want %+v（副屏那一块，不是主屏）",
			plan.Work, displays[1].WorkArea)
	}
	if !miniRectInside(plan.Rect, displays[1].WorkArea) {
		t.Errorf("窗 %+v 不在副屏的工作区 %+v 里", plan.Rect, displays[1].WorkArea)
	}

	// 🔴 **这一句问的就是生产那一档问的那个函数**：
	// `miniWindowOf.WorkArea` 调的是 `miniWorkAreaOf(displays, bounds)`。
	// 所以这一条断的就是**生产路径上的那一步**——把它换回框架那一版
	// （`mygo.Screen.DisplayMatching`，一块屏都不重叠时返回主屏），这一条
	// 立刻红。
	if got := miniWorkAreaOf(displays, cur); got != displays[1].WorkArea {
		t.Errorf("生产那一档算出来的工作区是 %+v，want %+v（副屏那一块）",
			got, displays[1].WorkArea)
	}
}

// TestTheMiniWindowComesBackWhenItsDisplayIsGone 盯「那块屏没了之后窗回到
// 可见区域」。
//
// ⚠️ **判据是「窗看得见」，不是「窗挪了」**：挪多少不重要，重要的是它**在
// 某一块还在的屏的工作区里**——而「看得见」这件事用户一眼就知道（「迷你
// 播放器不见了」），「挪了 3 个像素」他不知道。
func TestTheMiniWindowComesBackWhenItsDisplayIsGone(t *testing.T) {
	displays := miniTestDisplays()
	// 窗原来在副屏上（负坐标），而副屏被拔掉了。
	cur := mygo.Rectangle{X: -2400, Y: 300, Width: miniWidth, Height: miniHeight}
	onlyPrimary := displays[:1]

	plan := miniPlanKeep(onlyPrimary, cur)
	if plan.Rect.Width == 0 {
		t.Fatal("没有算出任何位置")
	}
	if !miniRectInside(plan.Rect, displays[0].WorkArea) {
		t.Errorf("副屏被拔掉之后窗在 %+v，不在剩下那块屏的工作区 %+v 里",
			plan.Rect, displays[0].WorkArea)
	}

	// 改分辨率也是同一件事：那块屏还在，但工作区变小了 → 位置要夹回去。
	smaller := []mygo.Display{{
		ID: 1, Primary: true,
		Bounds:   mygo.Rectangle{X: 0, Y: 0, Width: 1280, Height: 800},
		WorkArea: mygo.Rectangle{X: 0, Y: 25, Width: 1280, Height: 775},
	}}
	cur = mygo.Rectangle{X: 1500, Y: 100, Width: miniWidth, Height: miniHeight}
	plan = miniPlanKeep(smaller, cur)
	if !miniRectInside(plan.Rect, smaller[0].WorkArea) {
		t.Errorf("那块屏改了分辨率之后窗在 %+v，不在新的工作区 %+v 里",
			plan.Rect, smaller[0].WorkArea)
	}
}

// TestTheMiniPlacementHandlesScaledAndRotatedDisplays 盯「缩放 / 旋转」。
//
// ⚠️ **判据是「窗落在那块屏的工作区里」**：缩放与旋转在框架那一侧已经被
// 换算成**逻辑坐标**的工作区了（`mygo/internal/darwin/system.go:154` 的
// `Displays()` 拿的是 `visibleFrame`），所以这一档要防的是「定位那一层把
// 缩放因子当成了尺寸的一部分」——那会让窗跑到工作区外面去。
func TestTheMiniPlacementHandlesScaledAndRotatedDisplays(t *testing.T) {
	// 一块缩放 2 倍、旋转 90 度的屏：工作区是竖着的（高 > 宽）。
	rotated := []mygo.Display{{
		ID: 7, Label: "竖着的屏", ScaleFactor: 2, Rotation: 90,
		Bounds:   mygo.Rectangle{X: 0, Y: 0, Width: 1200, Height: 1920},
		WorkArea: mygo.Rectangle{X: 0, Y: 0, Width: 1200, Height: 1920},
	}}
	size := mygo.Rectangle{Width: miniWidth, Height: miniHeight}
	plan := miniPlanFirst(rotated, mygo.Point{X: 600, Y: 900}, size)
	if plan.Rect.Width == 0 {
		t.Fatal("没有算出任何位置")
	}
	if !miniRectInside(plan.Rect, rotated[0].WorkArea) {
		t.Errorf("缩放 2 倍、旋转 90 度的屏上算出了 %+v，不在工作区 %+v 里",
			plan.Rect, rotated[0].WorkArea)
	}
	// ⚠️ **贴的是右边**：右上角那一档是「工作区里最不挡事的位置」，而它跟
	// 屏是横是竖没关系。
	if got := plan.Rect.X + plan.Rect.Width; got != rotated[0].WorkArea.X+rotated[0].WorkArea.Width-miniMargin {
		t.Errorf("右边距是 %d，want %d（贴边 + 一道 %d）",
			got, rotated[0].WorkArea.X+rotated[0].WorkArea.Width-miniMargin, miniMargin)
	}
}

// TestTheMiniPlacementFollowsTheCursorDisplayAfterARestore 盯「恢复出来的
// 旧位置怎么判」。
//
// 🔴 **判据是「指针所在那块屏说了算」**（票 #555 的验收：「从任意显示器打
// 开迷你窗，首次落在指针所在显示器」）：
//
//   - 旧位置**就在指针那块屏上** ⇒ 留着它（位置记忆是这个功能本身）；
//   - 旧位置**在别的屏上**（哪怕它看得见）⇒ 跟指针走；
//   - 旧位置**谁都看不见** ⇒ 跟指针走。
//
// ⚠️ **中间那一条是这一票最容易漏掉的一半**：框架的 `restoreWindowState`
// （mygo v0.6.4 的 `window_state.go:133-137`）在旧位置屏外时会把窗**改成
// 主屏居中**——而「旧位置在主屏上、指针在副屏上」这一档，旧位置**看得
// 见**，于是「看得见就留着」会把窗留在主屏上，而用户正盯着副屏 ⇒ 症状和
// 「点了没反应」一模一样。
func TestTheMiniPlacementFollowsTheCursorDisplayAfterARestore(t *testing.T) {
	displays := miniTestDisplays()
	size := mygo.Rectangle{Width: miniWidth, Height: miniHeight}

	// ① 旧位置就在**指针那块屏**上 ⇒ 留着它（**不**改成右上角）。
	cur := mygo.Rectangle{X: -2400, Y: 300, Width: miniWidth, Height: miniHeight}
	plan := miniPlanRestore(displays, cur, mygo.Point{X: -2000, Y: 100}, size)
	if plan.Display.ID != 2 {
		t.Fatalf("旧位置就在指针那块屏上，而它被算到了 ID=%d 那块屏上", plan.Display.ID)
	}
	if plan.Rect != cur {
		t.Errorf("旧位置就在指针那块屏上，却被挪到了 %+v（want %+v）："+
			"位置记忆是这个功能本身", plan.Rect, cur)
	}

	// ② 旧位置在**别的屏**上（看得见）⇒ 跟指针走。
	//
	// ⚠️ **这一条正是评审抓出来的那一条**：旧位置 (400,600) 在主屏上，而指
	// 针在副屏上——窗必须落到副屏去。
	other := mygo.Rectangle{X: 400, Y: 600, Width: miniWidth, Height: miniHeight}
	plan = miniPlanRestore(displays, other, mygo.Point{X: -2000, Y: 100}, size)
	if plan.Display.ID != 2 {
		t.Fatalf("旧位置在别的屏上，而窗落在了 ID=%d 那块屏上（指针在 ID=2）",
			plan.Display.ID)
	}
	if !miniRectInside(plan.Rect, displays[1].WorkArea) {
		t.Errorf("窗在 %+v，不在指针那块屏的工作区 %+v 里",
			plan.Rect, displays[1].WorkArea)
	}

	// ③ 旧位置在**一块不存在的屏**上 ⇒ 跟指针走，落在指针那块屏上。
	stale := mygo.Rectangle{X: 4000, Y: 300, Width: miniWidth, Height: miniHeight}
	plan = miniPlanRestore(displays, stale, mygo.Point{X: -2000, Y: 100}, size)
	if plan.Display.ID != 2 {
		t.Fatalf("旧位置已经没人看得见了，而窗落在了 ID=%d 那块屏上", plan.Display.ID)
	}
	if !miniRectInside(plan.Rect, displays[1].WorkArea) {
		t.Errorf("窗在 %+v，不在指针那块屏的工作区 %+v 里",
			plan.Rect, displays[1].WorkArea)
	}

	// ④ **框架那一档的兜底位置**（`restoreWindowState` 把屏外的旧位置改成
	// **主屏居中**）⇒ 照样跟指针走。
	//
	// ⚠️ **这是评审探针里量到的那一个数**：(255,112) 是 320×84 在 1920×1080
	// 的主屏上居中的位置——它**看得见**，于是「看得见就留着」会把它留在主屏。
	centered := mygo.Rectangle{X: 255, Y: 112, Width: miniWidth, Height: miniHeight}
	plan = miniPlanRestore(displays, centered, mygo.Point{X: -2000, Y: 400}, size)
	if plan.Display.ID != 2 {
		t.Fatalf("旧位置是框架居中过的 (%d,%d)，而窗落在了 ID=%d 那块屏上（指针在 ID=2）",
			centered.X, centered.Y, plan.Display.ID)
	}
	if !miniRectInside(plan.Rect, displays[1].WorkArea) {
		t.Errorf("窗在 %+v，不在指针那块屏的工作区 %+v 里",
			plan.Rect, displays[1].WorkArea)
	}
}

// TestTheMiniWindowIsPulledBackWhenItLandsOnNoScreen 盯**生产那条路**：
// 窗被拖到一块屏都看不见的地方 ⇒ 把它拉回可见的工作区。
//
// ⚠️ **它走的是 `attachMiniWindowTo` 装的那个钩子**（不是测试另写的一个），
// 而那一个钩子调的正是生产那个函数（`miniOnWindowMoved`）。
func TestTheMiniWindowIsPulledBackWhenItLandsOnNoScreen(t *testing.T) {
	s, _ := newMiniShell(t, nil)
	displays := miniTestDisplays()
	fake := newFakeMiniWindow()
	// 窗在副屏上（负坐标）。
	fake.x, fake.y = -2400, 300
	s.attachMiniWindowTo(fake)
	// ⚠️ **显示器列表摆进来**：生产那一档问的是 `mygo.Screen`，而无头环境里
	// 压根没有应用（`mygo.Screen` 头上有 `needsApp`）。
	s.miniDisplays = func() []mygo.Display { return displays }

	// 拖到两块屏之间的缝里去了（谁都看不见它）。
	fake.move(5000, 2000)

	// ⚠️ **这一条断的是「拉回来了」**：删掉 `miniOnWindowMoved` 里那一句
	// `SetPosition`，下面那句 `miniVisibleOnAny` 就会红。
	if !miniVisibleOnAny(displays, fake.Bounds()) {
		t.Errorf("窗被拖到了谁都看不见的地方（%+v），而它没有被拉回来", fake.Bounds())
	}
	if !miniRectInside(fake.Bounds(), displays[0].WorkArea) {
		t.Errorf("窗在 %+v，不在任何一块屏的工作区里", fake.Bounds())
	}
}

// TestTheMiniWindowComesBackWhenItsDisplayIsRemoved 盯**生产那条路**：显示器
// 变了（那一块被拔掉）⇒ 窗自动回到可用区域。
//
// ⚠️ **它走的是 `miniOnDisplaysChanged`**——而生产上那一个是由
// `mygo.Screen.OnDisplaysChanged` 叫起来的（系统通知，无头环境里没有）。
func TestTheMiniWindowComesBackWhenItsDisplayIsRemoved(t *testing.T) {
	s, _ := newMiniShell(t, nil)
	displays := miniTestDisplays()
	fake := newFakeMiniWindow()
	// 窗在副屏上（负坐标）。
	fake.x, fake.y = -2400, 300
	s.attachMiniWindowTo(fake)

	// 副屏被拔掉了：系统通知到达的那一刻，列表里只剩主屏。
	s.miniDisplays = func() []mygo.Display { return displays[:1] }
	s.miniOnDisplaysChanged()

	if !miniRectInside(fake.Bounds(), displays[0].WorkArea) {
		t.Errorf("副屏被拔掉之后窗在 %+v，不在剩下那块屏的工作区 %+v 里",
			fake.Bounds(), displays[0].WorkArea)
	}
	if !miniVisibleOnAny(displays[:1], fake.Bounds()) {
		t.Errorf("副屏被拔掉之后窗还是看不见（%+v）", fake.Bounds())
	}
}

// TestTheMiniWindowIsReconciledAfterItIsBuilt 盯「窗建出来之后重新判一次」。
//
// ⚠️ **这是 `StateKey` 那一半**：框架会把记住的位置**盖掉**建窗时给的
// 坐标（`mygo/window_state.go` 的 `restoreWindowState`），而那一块屏要是已经
// 被拔了，恢复出来的就是一个**谁都看不见**的地方；那一块屏**还在**的话，恢
// 复出来的位置就在**别的屏**上——从副屏打开的人看不见它。于是建出来之后要
// 按**指针所在那块屏**再判一次（`ensureMiniWindow` 里那句
// `miniReconcileDisplayNow`）。
//
// ⚠️ **它走的是生产那个函数**（`miniReconcileDisplay`），不是测试另写的一个。
func TestTheMiniWindowIsReconciledAfterItIsBuilt(t *testing.T) {
	s, _ := newMiniShell(t, nil)
	displays := miniTestDisplays()
	fake := newFakeMiniWindow()
	// 窗被恢复到了一个 nobody 看得见的地方（那块屏已经被拔了）。
	fake.x, fake.y = 4000, 300
	s.attachMiniWindowTo(fake)

	// 指针在副屏上 ⇒ 窗该跟过去。
	s.miniReconcileDisplay(displays, mygo.Point{X: -2000, Y: 100})

	if !miniRectInside(fake.Bounds(), displays[1].WorkArea) {
		t.Errorf("窗在 %+v，不在指针那块屏的工作区 %+v 里",
			fake.Bounds(), displays[1].WorkArea)
	}
	// 🔴 **旧位置在「别的屏」上 ⇒ 也得跟指针走**（这一条是评审抓出来的那
	// 一半）：旧位置 (400,600) 在**主屏**上，而指针在**副屏**上——窗留在主
	// 屏的话，从副屏打开的人压根看不见它。
	fake.x, fake.y = 400, 600
	s.miniReconcileDisplay(displays, mygo.Point{X: -2000, Y: 100})
	if !miniRectInside(fake.Bounds(), displays[1].WorkArea) {
		t.Errorf("旧位置在主屏上、指针在副屏上，而窗还在 %+v（不在指针那块屏的"+
			"工作区 %+v 里）：从副屏打开的人看不见它",
			fake.Bounds(), displays[1].WorkArea)
	}

	// ⚠️ **旧位置在「指针那块屏」上 ⇒ 不许动**（位置记忆是这个功能本身）。
	fake.x, fake.y = -2400, 300
	s.miniReconcileDisplay(displays, mygo.Point{X: -2000, Y: 100})
	if fake.x != -2400 || fake.y != 300 {
		t.Errorf("旧位置就在指针那块屏上，却被挪到了 (%d,%d)：位置记忆是这个功能本身",
			fake.x, fake.y)
	}
}

// TestTheMiniCurtainUsesTheScreenItIsOn 盯「卷帘按**窗所在那块屏**的工作区
// 算高度」。
//
// ⚠️ **判据是「同一扇窗，在两块屏上算出两个不同的结果」**：窗停在两块屏的
// **同一个高度**（都靠近工作区的底边），而副屏更高——于是按**主屏**的工作
// 区算卷帘放不下（点封面什么也不发生），按**副屏**的那一块算就放得下。
//
// 🔴 **这一条正是「按主屏 WorkArea 错误夹边」那一半**：框架那一版
// `mygo.Screen.DisplayMatching` 对「一块屏都不重叠」的窗返回的是**第一块**
// （主屏）——而窗在副屏上时，那正是它算出来的那一块。
func TestTheMiniCurtainUsesTheScreenItIsOn(t *testing.T) {
	displays := miniTestDisplays()
	// 窗停在两块屏的同一个高度（都靠近工作区底边）。
	cur := mygo.Rectangle{X: -2400, Y: 896, Width: miniWidth, Height: miniHeight}

	// ⚠️ **用的是生产那一档用的那个函数**（`miniWindowOf.WorkArea` 调的就是
	// 它），而不是另写一份——这样「生产那一步算错了」这一种回归会被这一条
	// 抓住。
	work := miniWorkAreaOf(displays, cur)
	if work != displays[1].WorkArea {
		t.Fatalf("生产那一档算出来的工作区是 %+v，want %+v（副屏那一块）",
			work, displays[1].WorkArea)
	}
	// 按副屏那块工作区算：窗底到工作区底边还剩 1200 − 980 − 12 = 208 ⇒
	// 放得下（120 是下限，236 是上限）。
	h := miniCurtainHeight(work, cur)
	if h < miniCurtainMinH {
		t.Errorf("按副屏的工作区算卷帘高度是 %d，want >= %d（那儿放得下）",
			h, miniCurtainMinH)
	}
	// 🔴 **反过来**：按**主屏**的工作区算，同一个高度放不下（主屏矮 345）。
	// ⇒ 这一条证明「算的是哪块屏」是有分别的——按错了屏，卷帘就**再也不展开**
	// （点封面什么也不发生，而界面上看不出任何异样）。
	if hPrimary := miniCurtainHeight(displays[0].WorkArea, cur); hPrimary != 0 {
		t.Errorf("按主屏的工作区算居然放得下（%d）：这一条就证明不了「算的是哪块屏」",
			hPrimary)
	}
}

// TestTheMiniWindowFitsTheNewWorkAreaWhenTheDisplayShrinks 盯「那块屏变小
// 之后，**展开着的**窗回到可用区域」。
//
// 🔴 **这一条是评审抓出来的那一条（major）**：原来那一档只把窗**挪**回工作
// 区，而窗**还是那么高**——多出来的那一部分是**透明**的，而透明的那一块照
// 样吃点击（`minicurtain.go` 的文件头第 1 条）——于是窗挡在屏幕外，底下那
// 些应用的按钮点不到。
//
// ⚠️ **它走的是生产那条路**（`miniOnDisplaysChanged`），跑在**假窗**上。
func TestTheMiniWindowFitsTheNewWorkAreaWhenTheDisplayShrinks(t *testing.T) {
	s, _ := newMiniShell(t, nil)
	fake := newFakeMiniWindow()
	// 窗展开着（84 + 236 = 320 高），停在工作区顶上那 12。
	fake.x, fake.y = 0, miniMargin
	fake.height = miniHeight + miniCurtainH
	s.attachMiniWindowTo(fake)
	s.miniCurtain.open = true
	s.miniCurtain.target = miniCurtainH
	s.miniCurtain.setHeight = miniHeight + miniCurtainH

	// 那块屏的工作区变成 200 高（改了分辨率）。
	next := mygo.Rectangle{X: 0, Y: 0, Width: 1920, Height: 200}
	fake.work = next
	s.miniDisplays = func() []mygo.Display {
		return []mygo.Display{{ID: 1, Primary: true, Bounds: next, WorkArea: next}}
	}
	s.miniOnDisplaysChanged()

	if !miniRectInside(fake.Bounds(), next) {
		t.Errorf("那块屏的工作区变成 200 高之后，窗还在 %+v，不在工作区 %+v 里："+
			"多出来的那一块是透明的，照样吃点击",
			fake.Bounds(), next)
	}
	if s.miniCurtain.open {
		t.Errorf("窗比工作区还高，而卷帘还开着：它该收起来（窗高得回到 %d）",
			miniHeight)
	}
}

// TestTheMiniCurtainRetargetsFromTheCardBottom 盯「改了分辨率之后，卷帘的
// 目标高度按**卡片底下**重算」。
//
// 🔴 **这一条是评审抓出来的那一条（major）的另一半**：目标高度原来量的是
// **窗底下**——而窗展开着的时候窗底就是「卡片底 + 已经长出来的那截卷帘」
// ⇒ 量到的是「卷帘自己已经占掉的那一块」⇒ 目标高度**一级一级往下掉**（屏
// 幕上显示高度一变，卷帘就越缩越短，最后缩成 0）。
//
// ⚠️ **判据是「工作区 500 高 ⇒ 目标还是 236」**：卡片底在 y=96，500 - 96 -
// 12 = 392 ⇒ 够 ⇒ 目标就是上限那 236；而按窗底（y=332）量的话只剩 156。
func TestTheMiniCurtainRetargetsFromTheCardBottom(t *testing.T) {
	for _, workHeight := range []int{500, 450} {
		s, _ := newMiniShell(t, nil)
		fake := newFakeMiniWindow()
		fake.x, fake.y = 0, miniMargin
		fake.height = miniHeight + miniCurtainH
		next := mygo.Rectangle{X: 0, Y: 0, Width: 1920, Height: workHeight}
		fake.work = next
		s.attachMiniWindowTo(fake)
		s.miniCurtain.open = true
		s.miniCurtain.target = miniCurtainH
		s.miniCurtain.setHeight = miniHeight + miniCurtainH
		s.miniDisplays = func() []mygo.Display {
			return []mygo.Display{{ID: 1, Primary: true, Bounds: next, WorkArea: next}}
		}

		s.miniOnDisplaysChanged()

		if got := s.miniCurtain.target; got != miniCurtainH {
			t.Errorf("工作区 %d 高：卷帘的目标高度是 %d，want %d"+
				"（量的是**卡片底下**到工作区底边，不是窗底下——"+
				"后者会一级一级往下掉）",
				workHeight, got, miniCurtainH)
		}
	}
}
