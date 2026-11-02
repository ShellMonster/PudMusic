package ui

// 迷你播放器那块「歌词板」里**当前句居中**这一组。
//
// # 这一组量的是什么
//
// 歌词板是**独立的一张板**（卡片底下、隔着那条缝），而板里的歌词是
// 「**当前句在歌词区正中**」——上面那几句是历史、下面那几句是未来
// （`minicurtain_lyrics.go` 的文件头）。这一组量三样：
//
//  1. **当前句的中心 == 歌词板的中线**（差值在几像素之内）；
//  2. **当前句不在底边上**（它在板子正中，不在下沿）；
//  3. **下一句看得见**（居中排的语义：当前句上下都还有行）。
//
// ⚠️ **量的是画面上那个文字元素的位置**（`Tester.Find` 给的是元素在窗里的
// 框），不是哪个状态字段——用例要断的是**用户看得见的那件事**。
//
// ⚠️ **旧版量的是「贴着下沿排」**（票 #307 §②）：当前句的下沿压在下轴上、
// 下一句在视野之外。换「独立歌词板」之后那一套整个撤了——这一组是**新语义**
// 的判据，不是旧判据改个数。

import (
	"testing"
)

// miniCurtainBoardTop 是**歌词板的顶边**在窗里的 y（卡片 84 + 那条缝 10）。
const miniCurtainBoardTop = miniHeight + miniCurtainGap

// miniCurtainBoardMid 是**歌词区的中线**在窗里的 y（歌词板高 400，歌词区
// 占满整板——说明句和菜单钮是绝对定位的，不占它的高）。
const miniCurtainBoardMid = float32(miniCurtainBoardTop) + float32(miniCurtainH)/2

// TestTheMiniCurtainLyricsCentresTheCurrentLine 盯「当前句在歌词区正中」。
//
// 🔴 **判据一：当前句的中心压在歌词区的中线上。**
//
// ⚠️ **差值取 14**：那一行自己的上下各留 8（`lyricsLinePadY`），而「居中」
// 说的是**那一行**（它的框）——行的中心对区的中线，差一个像素都是偏的。
//
// 🔴 **判据二：当前句不在下半截。**
//
// ⚠️ **这一条是「居中」的正脸**：旧版「贴着下沿排」的话当前句在歌词板的
// 下沿上——而「中心在中线下面 30 像素以上」就证明它还在「贴下沿」那一档。
//
// 🔴 **判据三：下一句看得见。**
//
// ⚠️ **这一条是「居中」与「贴下沿」的分界**：贴下沿的语义是「当前句是最后
// 一句能看见的」（下一句在视野之外），而居中的语义是「上下都还有行」——
// 下一句**必须**画在歌词板里。
func TestTheMiniCurtainLyricsCentresTheCurrentLine(t *testing.T) {
	s, a := miniCurtainShell(t)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停：位置停在 0，于是当前句是第一句
	a.PutLyricsFrom("", "[00:00.00]第一句\n[00:05.00]第二句\n[00:10.00]第三句\n", "")
	tt := miniCurtainTester(s)
	tt.Frame()

	clickCurtainCover(t, tt)
	settleCurtain(t, tt, s, true)

	cur, ok := tt.Find("第一句")
	if !ok {
		t.Fatalf("画面上找不到当前句，文字有 %q", tt.Texts())
	}
	centre := cur.Y + cur.H/2
	if centre > miniCurtainBoardMid+14 || centre < miniCurtainBoardMid-14 {
		t.Errorf("当前句的中心在 y=%.1f，want ≈ %.1f（歌词区的中线）："+
			"它没有居中（差 %.1f 像素）",
			centre, miniCurtainBoardMid, centre-miniCurtainBoardMid)
	}
	// 🔴 **当前句不在下半截**：歌词板顶边到中线是 200，而当前句的中心必须在
	// 那条中线**附近**——贴着下沿的话它会在 470 上下（板底 − 半行高）。
	if centre > miniCurtainBoardMid+40 {
		t.Errorf("当前句的中心在 y=%.1f，比歌词区的中线（%.1f）低了 %.1f 像素："+
			"它还在「贴着下沿排」那一档", centre, miniCurtainBoardMid, centre-miniCurtainBoardMid)
	}

	// 🔴 **下一句看得见**：居中排的语义是「当前句上下都还有行」——第二句
	// 必须画在歌词板里（它的顶边在板底之上）。
	boardBottom := float32(miniCurtainBoardTop + miniCurtainH)
	next, ok := tt.Find("第二句")
	if !ok {
		t.Fatalf("画面上找不到第二句，文字有 %q", tt.Texts())
	}
	if next.Y >= boardBottom {
		t.Errorf("第二句的顶边在 y=%.1f，want < %.0f（歌词板的底边）："+
			"它被推到视野外了——而「当前句居中」的话，当前句上下都还有行",
			next.Y, boardBottom)
	}
}
