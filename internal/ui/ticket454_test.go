// 票 #454 的验收：队列抽屉那一行的反馈，和「清空播放列表」之前那道确认。
//
// 这一票治的是三件事（依据是 BMG-246 的报告 §四 的第 40 / 41 / 43 条）：
//
//  1. **「正在放」那一行悬停时一点变化都没有**——它是全抽屉里唯一一行没有悬停
//     反馈的，而它是**可点的**（点了不重头播，因为 `app.PlayAt` 碰到「就是这一首」
//     直接返回）。用户分不清它是「不能点」还是「坏了」。
//  2. **普通行没有按下态**：按下去的画面 = 悬停的画面（扁平按钮有 16% 那一层，
//     行没有）。
//  3. **「清空」一键销毁整个队列 + 停掉播放**，没有二次确认、也没有事后消息。
//
// # 判据全是**画面上的东西**
//
// 这一票改的每一处都是颜色，所以判据量的是**像素**：同一行的同一块地方，
// 「没指着」「悬停」「按着」三档必须两两不同，而且按着那一档离「没指着」更远
// （深浅两套里「更深」的方向相反：浅色下更深 = 更暗，深色下更深 = 更亮，
// 所以判据比的是**离原色多远**，不是「哪个更暗」）。
//
// ⚠️ **量的是行底下那条内边距**（`row.Y + row.H - 4`）：歌名和副题都在上面，
// 量到的是行自己的底色，而文字被那一层反馈压过之后颜色会变——量到文字就分不清
// 是「行底变了」还是「字变了」。
package ui

import (
	"image/color"
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// queueHarness 造一副**指定深浅**的界面。
//
// ⚠️ **深浅两套都要跑**：这一票改的每一层都是「压一层半透明前景色」，而深浅两套
// 里那个前景色一个近黑一个近白——压出来的方向正好相反。只跑浅色的话，深色下
// 「按下去更深」这件事根本没人验过。
func queueHarness(t *testing.T, dark bool) *harness {
	t.Helper()
	b := newTestShell(t, at(winW, winH), systemIsDark(dark))
	return &harness{app: b.a, tt: b.tt, s: b.s}
}

// queueThemeName 是这一组用例的子测试名。
func queueThemeName(dark bool) string {
	if dark {
		return "深色"
	}
	return "浅色"
}

// rowBackdrop 是量一行「自己的底」的那一点：行底下那条内边距。
//
// ⚠️ **不要量歌名那一块**：那一层反馈是画在整行上的（连字一起压），量到字就
// 分不清是「底变了」还是「字变了」。
func rowBackdrop(row mygoui.Rect) (float32, float32) {
	return row.X + row.W/2, row.Y + row.H - 4
}

// queueRowRects 是抽屉里**头两行**在画面上的位置。
//
// ⚠️ **第一行不能靠歌名找**：它是「正在放」的那一行，而**播放条上也是同一首歌名**
// （真放着的时候），主布局先于浮层排版，`Find` 先撞上的是播放条上那几行字
// （`findRow` 遇到那种就说「找不到」——它要的就是抽屉里那一行）。
//
// 而第二行的歌名是唯一的（播放条上放的是头一首），所以从它出发：抽屉里那些行
// **一样宽、一样叠着**（行高定死 64，见 queueRowHeight），于是头一行就是第二行
// 往上挪一格。
func queueRowRects(h *harness, secondTitle string) (first, second mygoui.Rect, ok bool) {
	second, ok = findRow(h, secondTitle)
	if !ok {
		return mygoui.Rect{}, mygoui.Rect{}, false
	}
	first = mygoui.Rect{X: second.X, Y: second.Y - queueRowHeight, W: second.W, H: queueRowHeight}
	return first, second, true
}

// farFrom 是两个颜色离得多远（只比红通道）。
//
// ⚠️ **比「离原色多远」而不是「哪个更暗」**：深色下那一层压的是近白，压下去
// 反而更亮——「更深」在两套里的方向是反的。
func farFrom(a, b color.RGBA) int {
	d := int(a.R) - int(b.R)
	if d < 0 {
		return -d
	}
	return d
}

// TestThePlayingQueueRowChangesWhenHovered 钉「正在放的那一行悬停时看得见变化」。
//
// 改之前那一行是全抽屉里**唯一一行没有悬停反馈的**：`case state == queueRowCurrent`
// 排在 `case hovered` 前面，于是指针落在它上面时底色一点不变。
//
// ⚠️ **量的是「正在放」那一行，不是别的行**：别的行本来就有悬停底（`t.SurfaceHover`），
// 拿它们来量的话这条用例在改之前也是绿的。
func TestThePlayingQueueRowChangesWhenHovered(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(queueThemeName(dark), func(t *testing.T) {
			h := queueHarness(t, dark)
			dir := t.TempDir()
			// 头一首是正在放的那一首（`enqueue` 放的是第一个）。
			enqueue(t, h, localSong(t, dir, "正在放.flac"), localSong(t, dir, "别的.flac"))
			openDrawer(t, h)

			row, _, ok := queueRowRects(h, "别的")
			if !ok {
				t.Fatalf("抽屉里找不到那两行，文字有 %q", h.tt.Texts())
			}
			px, py := rowBackdrop(row)
			idle := pixelAt(h.tt.Image(), px, py)

			// 指针落到这一行上。
			h.tt.Move(row.X+row.W/2, row.Y+row.H/2)
			h.tt.Frame()
			hovered := pixelAt(h.tt.Image(), px, py)

			if hovered == idle {
				t.Errorf("正在放的那一行悬停时底色一点没变（%v）——"+
					"它是可点的（只是点了不重头播），用户分不出它是「不能点」还是「坏了」", hovered)
			}
		})
	}
}

// TestAPressedQueueRowLooksDeeperThanAHoveredOne 钉「普通行按下时和悬停时不一样」。
//
// 改之前行只有悬停底（`t.SurfaceHover`），**按下去的画面 = 悬停的画面**——
// 而扁平按钮是有 16% 那一层的（`widgets/flat` 的 `feel`）。
//
// 三档都要量：「没指着」「悬停」「按着」两两不同，而且按着那一档离「没指着」
// 更远（16% 比 8% 深）。
func TestAPressedQueueRowLooksDeeperThanAHoveredOne(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(queueThemeName(dark), func(t *testing.T) {
			h := queueHarness(t, dark)
			enqueue(t, h, copiesOf(t, 2)...)
			openDrawer(t, h)

			// 量第二行：头一行是「正在放」那一行，它有自己那层淡主色底。
			row, ok := findRow(h, "b")
			if !ok {
				t.Fatalf("抽屉里找不到第二行，文字有 %q", h.tt.Texts())
			}
			px, py := rowBackdrop(row)
			idle := pixelAt(h.tt.Image(), px, py)

			h.tt.Move(row.X+row.W/2, row.Y+row.H/2)
			h.tt.Frame()
			hovered := pixelAt(h.tt.Image(), px, py)

			// 按下去（**不抬手**——抬手就是点了，而点了会跳去放那一首）。
			h.tt.Press(row.X+row.W/2, row.Y+row.H/2)
			h.tt.Frame()
			pressed := pixelAt(h.tt.Image(), px, py)

			if hovered == idle {
				t.Fatalf("悬停时这一行一点没变（%v）——它本来就该有悬停底", hovered)
			}
			if pressed == hovered {
				t.Errorf("按下去和悬停时一模一样（%v）——行没有按下态，"+
					"「点得动」和「点得动的东西」在按下的那一瞬间长得一样", pressed)
			}
			if farFrom(pressed, idle) <= farFrom(hovered, idle) {
				t.Errorf("按下去那一档不比悬停深（悬停离原色 %d，按下离原色 %d）——"+
					"按下那一层是 16%%，悬停是 8%%，按下去得更深才对",
					farFrom(hovered, idle), farFrom(pressed, idle))
			}
		})
	}
}

// TestClearingTheQueueAsksFirst 钉「清空播放列表」之前要问一次。
//
// 改之前那一下是**一键销毁整个队列 + 停掉播放**（`app.ClearQueue` → `queue.Reset()`
// + `ClearTrack()`），没有二次确认、也没有任何事后消息——而本仓库对别的破坏性动作
// （删收藏夹、删下载记录、删本地文件）都有确认框。
//
// ⚠️ **确认框用的是仓库现成的那一道**（`confirmModal`，见 confirmmodal.go）：
// 形状、按钮档位、遮罩行为都跟别处一样——不是另写的一套。
func TestClearingTheQueueAsksFirst(t *testing.T) {
	h := newHarness(t)
	enqueue(t, h, copiesOf(t, 2)...)
	openDrawer(t, h)

	if err := h.tt.Click("清空"); err != nil {
		t.Fatalf("点不了「清空」：%v", err)
	}
	h.tt.Frame()

	wantText(t, h.tt, "清空播放列表吗？")
	if n := h.app.Queue().Len(); n != 2 {
		t.Fatalf("确认框还没点确定就把队列清掉了（剩 %d 首）", n)
	}
	if st := h.app.Snapshot(); !st.HasTrack {
		t.Error("确认框还没点确定就停掉了播放")
	}

	// 取消：什么也不发生。
	if err := h.tt.Click("取消"); err != nil {
		t.Fatalf("点不了「取消」：%v（文字：%q）", err, h.tt.Texts())
	}
	h.tt.Frame()
	dontWantText(t, h.tt, "清空播放列表吗？")
	if n := h.app.Queue().Len(); n != 2 {
		t.Errorf("取消了还是把队列清掉了（剩 %d 首）", n)
	}
	if st := h.app.Snapshot(); !st.HasTrack {
		t.Error("取消了还是停掉了播放")
	}

	// 再来一次，这次确定。
	if err := h.tt.Click("清空"); err != nil {
		t.Fatalf("点不了「清空」：%v", err)
	}
	h.tt.Frame()
	// ⚠️ **点的是「确认清空」而不是「清空」**：抽屉头上那枚图标键的无障碍名字
	// 也是「清空」，而它在树里排得更前——点「清空」会点到抽屉头那一枚上去。
	// 确认键的无障碍名字是「确认」+ 那两个字（confirmmodal.go 的 confirmButton）。
	if err := h.tt.Click("确认清空"); err != nil {
		t.Fatalf("点不了确认键：%v（文字：%q）", err, h.tt.Texts())
	}
	h.tt.Frame()

	if n := h.app.Queue().Len(); n != 0 {
		t.Errorf("确定之后队列还剩 %d 首", n)
	}
	if st := h.app.Snapshot(); st.HasTrack {
		t.Errorf("确定之后还在放：%+v", st)
	}
	if !h.tt.HasText(queueEmptyHint) {
		t.Errorf("清空之后抽屉里不是空状态，文字：%q", h.tt.Texts())
	}
	// 事后再有一句：确认框问的是「要不要」，「已经清掉了」是另一句话。
	wantText(t, h.tt, "已清空播放列表")
}

// TestThePlayingRowStillHasNoPlayTriangle 钉「正在放」那一行的封面**仍然不浮播放
// 三角**。
//
// 这一票给那一行补了悬停反馈，而那一层反馈**不是**「它可以放」的意思——它已经在
// 放了，封面浮一个「▶」是错的。
//
// ⚠️ 判据是**封面中心那一块有没有变暗**：浮三角那一层是 35% 黑的蒙版
// （`viewQueueCover`），而悬停反馈只压 8% 的前景色——深浅两套里那两者的差都
// 远大于 30 个亮度级，所以「有没有蒙版」和「有没有悬停底」分得开。
func TestThePlayingRowStillHasNoPlayTriangle(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(queueThemeName(dark), func(t *testing.T) {
			h := queueHarness(t, dark)
			dir := t.TempDir()
			enqueue(t, h, localSong(t, dir, "正在放.flac"), localSong(t, dir, "别的.flac"))
			openDrawer(t, h)

			row, _, ok := queueRowRects(h, "别的")
			if !ok {
				t.Fatalf("抽屉里找不到那两行，文字有 %q", h.tt.Texts())
			}
			// 封面在行内：左边 8 是封面左沿，封面 48 宽、竖着铺满 64-16。
			cx := row.X + queuePadX + queueCoverSize/2
			cy := row.Y + row.H/2

			idle := snapshot(h.tt)
			h.tt.Move(cx, cy)
			h.tt.Frame()
			hovered := snapshot(h.tt)

			const half = 16
			darkened := 0
			for y := int(cy) - half; y <= int(cy)+half; y++ {
				for x := int(cx) - half; x <= int(cx)+half; x++ {
					if glyphLuma(idle.RGBAAt(clampX(idle, x), clampY(idle, y)))-
						glyphLuma(hovered.RGBAAt(clampX(hovered, x), clampY(hovered, y))) > 30 {
						darkened++
					}
				}
			}
			if want := (2*half + 1) * (2*half + 1) / 2; darkened >= want {
				t.Errorf("悬停在封面 %.0f,%.0f 上之后有 %d 个像素暗了三成——"+
					"「正在放」那一行的封面不该浮播放三角（它已经在放了）", cx, cy, darkened)
			}
		})
	}
}
