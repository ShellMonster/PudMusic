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
	"testing"
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

	if err := h.tt.Click("清空播放列表"); err != nil {
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
	if err := h.tt.Click("清空播放列表"); err != nil {
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
