package ui

// 「回顶按钮的门槛是**真实像素**」（票 #703 的 B）。
//
// # 为什么要有这一条
//
// 回顶那一枚的门槛是滚过 400 像素（`list.backTopPx`）。这个数有两个来源：
//
//	挂 ScrollState	`ui.ScrollState` 的 Y——**真实像素**
//	没挂（按行换算）	网格那一档的行高是**估的**（190，歌名折行就不准）
//
// 首页那三档原先为了「切 Tab 各自记位置」（票 #370）把 ScrollState 关掉了，
// 门槛退回按行换算。票 #370 的回归钉子
// （`TestEachHomeTabKeepsItsOwnScrollAndItsOwnBackToTop`）在内容块按档分了
// key（票 #368 的 `homeBodyKeys`）之后照样绿——各档的滚动状态本来就落在
// **不同的元素位置**上，互不抢——所以三档挂回 ScrollState，门槛回到真实像素。
//
// # 这一条怎么把两种实现分开
//
// 网格一行至少 140 的封面，滚 420 像素时画面上的顶行**最多到第 3 行**；按行
// 估算的那份（行高 190）算出来最多 380，**够不到 400 的门槛**——按钮不出。
// 真实像素的那份 420 > 400，按钮出。**按钮出不出，就是两种实现的分界**。
//
// ⚠️ **这个分界有一个前提**：估算常数（`gridRowHeight`，190）得小于 200——
// 两倍的常数要是够到了 400（比如行高估成 200+），两种实现在 420 像素都会出
// 按钮，这一条就悄悄失去区分度。改那个常数的时候记得回来动这一条。

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestTheHomeBackToTopThresholdIsRealPixels 滚过 420 真实像素，按钮必须出现。
//
// ⚠️ **走网格档**：列表那一档行高定死（64），按行换算本来就是准的，分不出
// 两种实现；网格那一档的 190 是估的——这一条专门钉它。
func TestTheHomeBackToTopThresholdIsRealPixels(t *testing.T) {
	b := newUIExplore(t)
	homePagedArchives(b, 3)
	_, tt, a := newHomeShell(t, b)

	// 切到网格档（设置页那一条路：UpdateSettings）。
	mountDisplay(t, a)
	setDisplay(t, a, tt, app.DisplayModeCard)

	waitFrames(t, tt, "第一页到了", func() bool {
		return showsText(tt, homePagedSong("音乐", "1", 1))
	})

	// 还没滚：按钮不在。
	if showsText(tt, backTopLabel) {
		t.Fatalf("还没滚就出现回顶按钮了")
	}

	// 滚 420 真实像素：过了 400 的门槛，按钮出。
	tt.Scroll(homeW/2, homeH/2, 0, 420)
	waitFrames(t, tt, "滚过 420 像素之后回顶按钮出现", func() bool {
		return showsText(tt, backTopLabel)
	})

	// 按它：回到顶上，按钮收起来。
	if err := tt.Click(backTopLabel); err != nil {
		t.Fatalf("点不到回顶按钮：%v", err)
	}
	waitFrames(t, tt, "回到顶上之后按钮消失", func() bool {
		return !showsText(tt, backTopLabel)
	})
	if got := homePagedPos(tt, "音乐"); len(got) == 0 || got[0].title != homePagedSong("音乐", "1", 1) {
		t.Errorf("回到顶上之后画着的是 %v，want 从第一首开始", titles(got))
	}
}
