// 迷你播放器的**多显示器定位**（票 #555）。
//
// # 这一票修的是什么
//
// 迷你播放器第一次出现时，它停在**主显示器**的右上角
// （`ensureMiniWindow` 里那句 `Screen.PrimaryDisplay().WorkArea`）——
// 而用户是从**他此刻盯着的那块屏**上点进来的。于是「在第二块屏上的人打开
// 迷你播放器，窗却开在另一块屏上」——而那块屏上的人根本没看见它。
//
// 另外两处：
//
//   - `StateKey` 记住的位置**恢复回来之后不再判显示器**：那块屏要是被拔了
//     （或者改了分辨率），窗就留在一个**谁都看不见**的地方。
//   - 跨屏拖动之后，卷帘按哪块屏的工作区算——原来按的是主屏（见
//     `miniWindowOf.WorkArea` 那一段的注释）。
//
// # 定位逻辑是**纯函数**，问系统是**薄薄一层**
//
// 「该放哪儿」这件事是纯逻辑：给它一堆显示器、一个指针位置、一个窗的矩形，
// 它给出一个合法的矩形。⇒ 无头那一档量得到（`minidisplays_test.go`），
// 而「真双屏 / 插拔 / 旋转」只有真机能验（收尾说明里单列，**不拿无头测试
// 顶替**）。
//
// ⚠️ **坐标是一套「以主屏左上角为原点」的全局坐标**：第二块屏在主屏左边时
// 它的 X 是负的，在上面时 Y 是负的（框架那一侧已经把 Cocoa 的坐标翻过来了，
// `mygo/internal/darwin/window.go:1377` 的 `rectFromMac`）。
//
// ⚠️ **别拿「哪块屏最大」当判据**：一块屏可以**完全在另一块屏的下面**
// （竖着放的那台笔记本 + 一台横着的外接显示器），而「哪块离指针近」和
// 「哪块面积大」是两件事，而这一票要的是前者。
package ui

import "github.com/egoist/mygo"

// miniMargin 是迷你窗离工作区边的那一道。
//
// ⚠️ **12 是窗口自己那道边距**（`miniCorner` 里原本那个写死的数）：窗停在
// 右上角时它离工作区顶边 12，而卷帘底边离工作区底边也留 12（见
// `minicurtain.go` 的 `miniCurtainMargin`）——两头一样。
const miniMargin = 12

// miniPlacement 是「迷你窗该在哪儿」那一次规划的**结果**。
//
// ⚠️ **三样是一件事**：窗在哪儿（`Rect`）、按哪块工作区算边界（`Work`）、
// 落在哪块屏上（`Display`）。而 `Work` **必须跟着 `Display` 走**——卷帘往
// 哪边长、长多长，量的是**窗现在所在那块屏**的工作区，而不是主屏的。
type miniPlacement struct {
	// Rect 是窗该在哪儿（屏幕坐标）。
	Rect mygo.Rectangle
	// Work 是那一块工作区（卷帘按它算展开方向和边界）。
	Work mygo.Rectangle
	// Display 是窗落在哪块屏上（**零值 = 「一块屏都没有」**）。
	Display mygo.Display
}

// miniRectOverlap 是两个矩形重叠的面积（不重叠就是 0）。
func miniRectOverlap(a, b mygo.Rectangle) int {
	w := min(a.X+a.Width, b.X+b.Width) - max(a.X, b.X)
	h := min(a.Y+a.Height, b.Y+b.Height) - max(a.Y, b.Y)
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

// miniRectGap 是两个矩形之间最短的距离（**平方**，比较用够了；重叠就是 0）。
//
// ⚠️ **它量的是「屏幕坐标里两块地方挨得多近」**，而方向不分——所以它只
// 用来在「没有重叠」的时候挑一块**最近的**屏。
func miniRectGap(a, b mygo.Rectangle) int {
	dx := max(max(b.X-(a.X+a.Width), a.X-(b.X+b.Width)), 0)
	dy := max(max(b.Y-(a.Y+a.Height), a.Y-(b.Y+b.Height)), 0)
	return dx*dx + dy*dy
}

// miniDisplayNearestPoint 是离 p 最近的那块屏。
//
// ⚠️ **判据是「离指针多近」，不是「哪块最大」**：用户盯着哪块屏，指针就在
// 哪块屏上——而那块屏可能是小的那块（竖着放的笔记本 + 横着的显示器）。
func miniDisplayNearestPoint(displays []mygo.Display, p mygo.Point) mygo.Display {
	best := mygo.Display{}
	bestGap := 0
	found := false
	for _, d := range displays {
		gap := miniRectGap(mygo.Rectangle{X: p.X, Y: p.Y}, d.Bounds)
		if !found || gap < bestGap {
			best, bestGap, found = d, gap, true
		}
	}
	return best
}

// miniDisplayFor 是「这个矩形该算在哪块屏上」。
//
// 🔴 **它和框架那一版不一样，而那正是这一票的一个根因**：框架的
// `mygo.Screen.DisplayMatching` 在「一块屏都不重叠」时返回的是**第一块**
// （它的 `bestArea` 初始是 -1，于是任何面积 ≥ 0 的屏都「赢」）——而第一块
// 恰好是**主屏**。
//
// ⇒ 症状是「窗拖到第二块屏上，卷帘却按**主屏**的工作区算」——窗在屏幕
// 下面，卷帘说那儿放不下（主屏矮），于是**点封面什么也不发生**。
//
// ⇒ 这一版：**重叠最多的那块**；一块都不重叠时取**最近**的那块。
func miniDisplayFor(displays []mygo.Display, r mygo.Rectangle) mygo.Display {
	best := mygo.Display{}
	bestArea, bestGap := 0, 0
	found := false
	for _, d := range displays {
		area := miniRectOverlap(r, d.Bounds)
		gap := miniRectGap(r, d.Bounds)
		switch {
		case !found:
			best, bestArea, bestGap, found = d, area, gap, true
		case area > bestArea:
			best, bestArea, bestGap = d, area, gap
		case area == bestArea && gap < bestGap:
			best, bestGap = d, gap
		}
	}
	return best
}

// miniRectReachable 报 r 在 work 里露得够不够多。
//
// ⚠️ **判据和框架那一版一样**（`mygo/window_state.go` 的 `onDisplay`）：
// 横向至少露出 100（窗比 100 窄就按窗宽算）、纵向至少露出 50。
//
// ⚠️ **为什么不用「露一点就算」**：一块 320×84 的迷你窗，露 10 像素出来，
// 用户根本看不见它——而「看不见」和「窗没了」在界面上长得一模一样。
func miniRectReachable(r, work mygo.Rectangle) bool {
	if r.Width <= 0 || r.Height <= 0 || work.Width <= 0 || work.Height <= 0 {
		return false
	}
	w := min(work.X+work.Width, r.X+r.Width) - max(work.X, r.X)
	h := min(work.Y+work.Height, r.Y+r.Height) - max(work.Y, r.Y)
	return w >= min(100, r.Width) && h >= min(50, r.Height)
}

// miniVisibleOnAny 报 r 是不是还看得见（在**某一块**屏的工作区里露得够多）。
func miniVisibleOnAny(displays []mygo.Display, r mygo.Rectangle) bool {
	for _, d := range displays {
		if miniRectReachable(r, d.WorkArea) {
			return true
		}
	}
	return false
}

// miniWorkAreaOf 是「这一扇窗该按哪一块工作区算」。
//
// 🔴 **它就是生产那一档用的那个函数**（`miniWindowOf.WorkArea` 调的是它）——
// 所以无头那一档断它，就等于断生产那一步，而不是断一份「测试自己写的那
// 一份」。
//
// ⚠️ **它不用框架那一版**（`mygo.Screen.DisplayMatching`）：框架那一版在
// 「一块屏都不重叠」时返回的是**第一块**（也就是主屏）——窗在第二块屏上的
// 时候，卷帘于是按主屏的工作区算高度，症状是「窗拖过去了，点封面却说什么
// 也不发生（那儿放不下）」。
func miniWorkAreaOf(displays []mygo.Display, bounds mygo.Rectangle) mygo.Rectangle {
	if len(displays) == 0 {
		return mygo.Rectangle{}
	}
	return miniDisplayFor(displays, bounds).WorkArea
}

// miniClampInto 把 r 挪进 work（**大小一个字都不改**）。
//
// ⚠️ **大小不动**：迷你窗有 `MinWidth/MinHeight`（`MiniWindowOptions`），
// 缩到比 320×84 还小是**框架不允许**的——而工作区比窗还窄的时候（改了这
// 块屏的分辨率），正确的做法是**把左上角对齐到工作区的左上角**，而不是
// 把窗改小（改小的话窗控件会被裁掉）。
func miniClampInto(r mygo.Rectangle, work mygo.Rectangle) mygo.Rectangle {
	if work.Width <= 0 || work.Height <= 0 {
		return r
	}
	out := r
	if out.Width >= work.Width {
		out.X = work.X
	} else {
		out.X = min(max(out.X, work.X), work.X+work.Width-out.Width)
	}
	if out.Height >= work.Height {
		out.Y = work.Y
	} else {
		out.Y = min(max(out.Y, work.Y), work.Y+work.Height-out.Height)
	}
	return out
}

// miniCornerRect 是工作区**右上角**那个位置（贴边 + 一道 12）。
//
// ⚠️ **右上角不是随便挑的**：左上角被菜单栏占着、底部被程序坞占着，右上角
// 是工作区里最不挡事的一块（理由见 `miniCorner` 那段注释）。
func miniCornerRect(work mygo.Rectangle, size mygo.Rectangle) mygo.Rectangle {
	if work.Width <= 0 || work.Height <= 0 {
		return mygo.Rectangle{}
	}
	return mygo.Rectangle{
		X:      work.X + work.Width - size.Width - miniMargin,
		Y:      work.Y + miniMargin,
		Width:  size.Width,
		Height: size.Height,
	}
}

// miniPlanFirst 是**第一次**放置：窗落在**指针所在**那块屏的右上角。
//
// ⚠️ **指针是唯一的判据**：「用户在哪儿点的那一下」比「哪块屏是主屏」更
// 贴近用户想看到的东西——一块屏的用户点了迷你播放器，而窗开在另一块屏上，
// 在他看来就是「点了没反应」。
func miniPlanFirst(displays []mygo.Display, cursor mygo.Point, size mygo.Rectangle) miniPlacement {
	d := miniDisplayNearestPoint(displays, cursor)
	if d.WorkArea.Width <= 0 || d.WorkArea.Height <= 0 {
		return miniPlacement{}
	}
	return miniPlacement{
		Rect:    miniCornerRect(d.WorkArea, size),
		Work:    d.WorkArea,
		Display: d,
	}
}

// miniPlanKeep 是「留在现在这块屏上」：按**它现在所在那块屏**的工作区夹住。
//
// ⚠️ **那块屏是「重叠最多的那块」**（`miniDisplayFor`）——而一块都不重叠
// 的时候是**最近的那块**。⇒ 这一条同时管着两件事：
//
//   - 跨屏拖动之后，卷帘按**新那块屏**的工作区算（不是主屏）；
//   - 原来那块屏被拔了 / 改了分辨率之后，窗被夹回**还剩下的**那块屏。
func miniPlanKeep(displays []mygo.Display, cur mygo.Rectangle) miniPlacement {
	d := miniDisplayFor(displays, cur)
	if d.WorkArea.Width <= 0 || d.WorkArea.Height <= 0 {
		return miniPlacement{}
	}
	return miniPlacement{
		Rect:    miniClampInto(cur, d.WorkArea),
		Work:    d.WorkArea,
		Display: d,
	}
}

// miniPlanRestore 是「恢复了旧位置之后」再判一次。
//
// ⚠️ **旧位置还看得见 ⇒ 留着它**（位置记忆是这个功能本身，别把它抹掉）。
// ⚠️ **看不见了 ⇒ 跟指针走**（那块屏被拔了 / 改了分辨率，旧位置是个谁都
// 看不见的地方——而用户多半正盯着指针所在的那块屏）。
func miniPlanRestore(displays []mygo.Display, cur mygo.Rectangle, cursor mygo.Point, size mygo.Rectangle) miniPlacement {
	if miniVisibleOnAny(displays, cur) {
		return miniPlanKeep(displays, cur)
	}
	return miniPlanFirst(displays, cursor, size)
}

// miniDisplayList 是「这一刻的显示器列表」。
//
// ⚠️ **空列表 = 「问不到系统」**：`mygo.Screen` 头上有 `needsApp`
// （`mygo/app.go:128`），应用没 ready 的时候调它**直接 panic**——而无头
// Tester 里压根没有应用。所以那一档返回空列表（调用方自己兜底）。
func miniDisplayList() []mygo.Display {
	if !mygo.App.IsReady() {
		return nil
	}
	return mygo.Screen.Displays()
}

// miniCursorPoint 是「指针现在在哪儿」（屏幕坐标）。
//
// ⚠️ **零值 + false = 「问不到」**：理由同 `miniDisplayList`。
func miniCursorPoint() (mygo.Point, bool) {
	if !mygo.App.IsReady() {
		return mygo.Point{}, false
	}
	return mygo.Screen.CursorScreenPoint(), true
}

// miniScreen 是「问系统：这一刻有哪些屏，指针在哪儿」。
//
// ⚠️ **它给的是这个外壳那一问**（`Shell.miniDisplays` / `Shell.miniCursor`）
// ——而那两个是**给无头测试换掉的**：生产那一档问 `mygo.Screen`，而无头那一
// 档压根没有应用（`mygo.Screen` 头上有 `needsApp`，调它直接 panic）。
//
// ⇒ 于是「显示器变了之后窗被夹回去」这一整条**生产路径**在无头那一档也走得
// 通（测试把一块假的显示器列表摆进来，见 `minidisplays_test.go`）。
func (s *Shell) miniScreen() ([]mygo.Display, mygo.Point) {
	displays := miniDisplayList()
	if s.miniDisplays != nil {
		displays = s.miniDisplays()
	}
	cursor, ok := miniCursorPoint()
	if !ok {
		cursor = mygo.Point{}
	}
	if s.miniCursor != nil {
		if c, ok := s.miniCursor(); ok {
			cursor = c
		}
	}
	return displays, cursor
}

// miniReconcileDisplay 是「重新判一次位置」：算出该在哪儿，然后挪过去。
//
// ⚠️ **它不挪的时候就是「哪儿也不用去」**——每一次 `SetPosition` 都是一次
// 原生调用，而这一档跑在「显示器变了」和「窗建出来了」这两个时刻上。
func (s *Shell) miniReconcileDisplay(displays []mygo.Display, cursor mygo.Point) {
	w := s.miniWindow()
	if w == nil || w.IsDestroyed() || len(displays) == 0 {
		return
	}
	cur := w.Bounds()
	size := mygo.Rectangle{Width: cur.Width, Height: cur.Height}
	if size.Width <= 0 {
		size.Width = miniWidth
	}
	if size.Height <= 0 {
		size.Height = miniHeight
	}
	plan := miniPlanRestore(displays, cur, cursor, size)
	if plan.Rect.Width == 0 {
		return
	}
	if plan.Rect.X == cur.X && plan.Rect.Y == cur.Y {
		return
	}
	w.SetPosition(plan.Rect.X, plan.Rect.Y)
}

// miniReconcileDisplayNow 是「问系统，然后重新判一次位置」。
func (s *Shell) miniReconcileDisplayNow() {
	displays, cursor := s.miniScreen()
	s.miniReconcileDisplay(displays, cursor)
}

// miniOnDisplaysChanged 是系统告诉我们「显示器变了」那一档
// （插上 / 拔掉 / 改分辨率）。
//
// 🔴 **这一档是「窗不会掉到屏幕外」的那一半**：拔掉一块屏之后，窗原来那个
// 位置成了**没人看得见**的地方，而这一档把它夹回**还剩下的**那块屏的
// 工作区里。
//
// ⚠️ **改分辨率也在这一档里**：系统发的是同一个通知
// （`NSApplicationDidChangeScreenParametersNotification`），而工作区变了
// 之后原来那个位置可能已经超出工作区——一样要夹回去。
func (s *Shell) miniOnDisplaysChanged() {
	w := s.miniWindow()
	if w == nil || w.IsDestroyed() {
		return
	}
	displays, _ := s.miniScreen()
	if len(displays) == 0 {
		return
	}
	cur := w.Bounds()
	plan := miniPlanKeep(displays, cur)
	if plan.Rect.Width == 0 {
		return
	}
	if plan.Rect.X != cur.X || plan.Rect.Y != cur.Y {
		w.SetPosition(plan.Rect.X, plan.Rect.Y)
	}
	// ⚠️ **卷帘开着的话，按新的工作区重算它该有多高**：那块屏改了分辨率之
	// 后，原来那个高度放不下了——而卷帘是**透明的那一块**（多出来的地方照样
	// 吃点击，见 `minicurtain.go` 的文件头）。
	s.miniCurtainRetarget()
}

// miniOnWindowMoved 是「窗被挪动了」那一档。
//
// ⚠️ **它不跟用户对着干**：只有**一块屏都看不见它**的时候才把它拉回来
// （用户正拖到两块屏之间的缝里去了）。而「窗现在在哪块屏上」这件事不靠
// 这一档记——卷帘每次要算高度的时候都现问（`miniWindowOf.WorkArea`）。
func (s *Shell) miniOnWindowMoved() {
	w := s.miniWindow()
	if w == nil || w.IsDestroyed() {
		return
	}
	displays, _ := s.miniScreen()
	if len(displays) == 0 {
		return
	}
	cur := w.Bounds()
	if !miniVisibleOnAny(displays, cur) {
		plan := miniPlanKeep(displays, cur)
		if plan.Rect.Width > 0 {
			w.SetPosition(plan.Rect.X, plan.Rect.Y)
		}
	}
	// 卷帘开着的话，目标高度按**窗现在这块屏**的工作区重算。
	s.miniCurtainRetarget()
}

// miniCurtainRetarget 是「卷帘开着的话，按它现在这块屏重算该有多高」。
//
// ⚠️ **它只改目标，不动手**：改的是 `miniCurtain.target`，而窗高每帧跟着
// 动画走（`miniCurtainSyncHeight`）——于是卷帘是**滑**到新高度的，不是
// 突然跳过去的。
func (s *Shell) miniCurtainRetarget() {
	if !s.miniCurtain.open {
		return
	}
	s.miniCurtain.target = s.miniCurtainMaxHeight()
}
