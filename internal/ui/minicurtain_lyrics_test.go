package ui

// 迷你播放器那个「歌词卷帘」的**歌词贴着下沿排**这一组（票 #307 §②）。
//
// # 这一组量的是什么
//
// 票面 §②：「卷帘里的歌词**贴着下沿排**（下轴在上、歌词在下轴之上），这样
// 窗高变了，歌词跟着下沿走 ⇒ 窗高永远等于内容高。」
//
// 而「贴着下沿」是**可以量的**：**当前句的下沿**就压在**下轴的上沿**上。
// 这一组量三样：
//
//  1. **当前句的下沿 == 下轴的上沿**（差值在几像素之内）；
//  2. **当前句不在中间**（它在下半截）；
//  3. **下一句在视野之外**（下沿排的语义：当前句是**最后一句能看见的**）。
//
// ⚠️ **量的是画面上那个文字元素的位置**（`Tester.Find` 给的是元素在窗里的
// 框），不是哪个状态字段——用例要断的是**用户看得见的那件事**。

import (
	"testing"
)

// miniCurtainAxisTop 是**下轴的上沿**在窗里的 y（无头 Tester 的窗是
// 84 + 236 = 320 高，而下轴是 14 高、贴着窗的下沿）。
const miniCurtainAxisTop = miniHeight + miniCurtainH - miniCurtainAxisH

// TestTheMiniCurtainLyricsHugTheBottom 盯「歌词贴着下沿排」（票 #307 §②）。
//
// 🔴 **判据一：当前句的下沿就压在下轴的上沿上。**
//
// ⚠️ **差值取 14**：那一行自己的上下各留 8（`lyricsLinePadY`，抄上游的
// `py-2`），所以**字**的下沿比**那一行**的下沿高 8——而「贴着下沿」说的是
// **那一行**（它的框）。⇒ 判据是「字的下沿离下轴的上沿不超过 14」。
//
// 🔴 **判据二：当前句不在中间。**
//
// ⚠️ **这一条是票面 §② 的正脸**：「当前句仍在中间」就是评审抓的那一条。
func TestTheMiniCurtainLyricsHugTheBottom(t *testing.T) {
	s, a := miniCurtainShell(t)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停：位置停在 0，于是当前句是第一句
	a.PutLyricsFrom("", "[00:00.00]第一句\n[00:05.00]第二句\n", "")
	tt := miniCurtainTester(s)
	tt.Frame()

	clickCurtainCover(t, tt)
	settleCurtain(t, tt, s, true)

	cur, ok := tt.Find("第一句")
	if !ok {
		t.Fatalf("画面上找不到当前句，文字有 %q", tt.Texts())
	}
	bottom := cur.Y + cur.H
	if bottom > float32(miniCurtainAxisTop)+1 {
		t.Errorf("当前句的下沿在 y=%.1f，want ≤ %.0f（= 下轴的上沿）："+
			"它压到下轴上去了", bottom, float32(miniCurtainAxisTop))
	}
	if bottom < float32(miniCurtainAxisTop)-14 {
		t.Errorf("当前句的下沿在 y=%.1f，want ≥ %.0f（= 下轴的上沿 − 那一行自己的 8 像素下留白）："+
			"它没有贴着下沿（差 %.1f 像素）",
			bottom, float32(miniCurtainAxisTop)-14, float32(miniCurtainAxisTop)-bottom)
	}
	// 🔴 **当前句不在中间**：歌词区的中线是 (84 + 306)/2 = 195，而当前句的
	// 中心必须在那条中线**下面**。
	mid := float32(miniHeight+miniCurtainAxisTop) / 2
	if cur.Y+cur.H/2 <= mid {
		t.Errorf("当前句的中心在 y=%.1f，want > %.0f（歌词区的中线）："+
			"它还在中间（票面 §②：「歌词贴着下沿排」）",
			cur.Y+cur.H/2, mid)
	}

	// 🔴 **下一句在视野之外**：下沿排的语义是「当前句是最后一句能看见的」。
	next, ok := tt.Find("第二句")
	if !ok {
		t.Fatalf("画面上找不到第二句，文字有 %q", tt.Texts())
	}
	if next.Y+next.H <= float32(miniCurtainAxisTop) {
		t.Errorf("第二句的下沿在 y=%.1f，want > %.0f（= 下轴的上沿）："+
			"它还在视野里——而「贴着下沿排」的话，当前句是**最后一句能看见的**",
			next.Y+next.H, float32(miniCurtainAxisTop))
	}
}
