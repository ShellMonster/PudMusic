// 「只看提到歌名的评论」那一枚开关在页头里的**摆布**（票 #574）。
//
// # 为什么要量
//
// 视觉审查不许目测（`docs/agents/visual-review.md`：《间距要量，不要目测》）。
// 这一族量的三样：
//
//	右沿  开关的右边缘贴着内容区的右沿（`pagePadX` = 16）
//	缝    「只看提到歌名的评论」和开关之间是 6（`space.Px6`）
//	行内  「共 N 条」和那一句之间是 12（页头第二行自己的 `Gap`）
package ui

import "testing"

// TestTheMentionFilterSwitchSitsAtTheRightEdge 断「那一枚的摆布」。
//
// 🔴 **三个数都是量出来的**（`Find` 给的是元素在窗口里的框，单位是 DIP）：
//
//	右沿  开关的右边缘 = 窗口宽 − `pagePadX`（16）= 内容区的右沿
//	缝    「只看提到歌名的评论」的右边缘到开关的左边缘 = 6（`space.Px6`）
//	行内  「共 N 条」的右边缘到那一句的左边缘 = 12（页头第二行自己的 `Gap`）
//
// ⚠️ **留 1 像素的余量**：字宽是浮点排版的结果（那一行里还有别的东西），
// 逐位相等不是「对」的证据（理由同 `backtotop_test.go` 那一段）。
func TestTheMentionFilterSwitchSitsAtTheRightEdge(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	b.mainRaw = commentFixtureWithNoMore(t, "reply-main.json")
	const win = 1400
	_, tt := newCommentsShell(t, b, mentionItem(), win, 1000)

	sw, ok := tt.Find("开关：只看提到歌名的评论")
	if !ok {
		t.Fatalf("画面上找不到那枚开关，文字有 %q", tt.Texts())
	}
	label, ok := tt.Find("只看提到歌名的评论")
	if !ok {
		t.Fatalf("画面上找不到开关旁边那一句，文字有 %q", tt.Texts())
	}
	count, ok := tt.Find("共 388 条")
	if !ok {
		t.Fatalf("画面上找不到「共 N 条」，文字有 %q", tt.Texts())
	}
	// 右沿：开关的右边缘贴着内容区的右沿。
	if got, want := sw.X+sw.W, float32(win)-pagePadX; got < want-1 || got > want+1 {
		t.Errorf("开关的右边缘是 %v，内容区的右沿是 %v——差了就说明那一枚没有贴在右沿上", got, want)
	}
	// 缝：那一句和开关之间是 6。
	if got := sw.X - (label.X + label.W); got < 5 || got > 7 {
		t.Errorf("「只看提到歌名的评论」和开关之间的缝是 %v，want 6", got)
	}
	// 行内：「共 N 条」和那一句之间是 12（页头第二行自己的 `Gap`）。
	if got := label.X - (count.X + count.W); got < 11 || got > 13 {
		t.Errorf("「共 N 条」和「只看提到歌名的评论」之间的缝是 %v，want 12", got)
	}
	// 上下：那一句和开关在同一条基线上（`AlignItems(Center)`）。
	if got := (sw.Y + sw.H/2) - (label.Y + label.H/2); got < -1 || got > 1 {
		t.Errorf("开关和那一句差了 %v 的竖直中线——它们不在同一条基线上", got)
	}
}
